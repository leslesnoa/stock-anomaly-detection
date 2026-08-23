# 監視ループのDB駆動化 — 設計仕様

**ステータス**: 承認済み
**日付**: 2026-08-23

## 背景

Phase 4バックエンド（ユーザー管理・JWT認証・watchlist CRUD API）は、watchlistテーブルへの
読み書きAPIのみをスコープとし、既存の監視ループ（`usecase.MonitorUsecase.StartMonitoring`、
`main.go`）は環境変数 `STOCK_CODES` の固定銘柄リストを監視する現状のまま変更しなかった
（2026-08-13時点でmemoryに記録済みの既知フォローアップ）。

この結果、ユーザーがフロントエンドのwatchlist機能で銘柄を追加しても、実際の監視対象には
一切反映されない状態になっている。本タスクでは監視ループをDBの`watchlist`テーブル駆動に
切り替え、この機能ギャップを解消する。

## スコープ

**含む:**
- 監視対象銘柄をDBの`watchlist`テーブル（全ユーザー横断）から動的に取得する仕組み
- `STOCK_CODES`環境変数の完全廃止

**含まない（意図的に対象外）:**
- `Watchlist.AlertThreshold`（ユーザーごとの個別閾値）を異常検知ロジックに反映すること。
  異常検知の閾値は引き続き`ANOMALY_THRESHOLD`環境変数によるグローバル固定値を使う。
  「異常検知ロジックはシステム側が持つべきで、ユーザーが個別設定する必要はないのでは」という
  疑問がユーザーから提起されており、`AlertThreshold`フィールド・`PATCH /watchlist/{id}`
  APIそのものの要否は別タスクで検討する（本タスクでは既存のまま触れない）。
- ユーザーごとの個別通知先分岐（現状Slack Webhook 1本のグローバル通知のまま）

## アーキテクチャ

### 動的更新方式：一括再起動

銘柄リストに変化があった場合、差分（追加/削除された銘柄）だけを個別に扱うのではなく、
**現在動いている監視goroutine群を丸ごと停止し、新しいリストで丸ごと再起動する**、
という単純な方式を採る。

理由：
- 既存の`StartMonitoring`（銘柄ごとに1 goroutine + timer を立てて`ctx`キャンセルまで
  ブロックする設計）のロジックを一切変更せずに流用できる
- goroutineの生成・停止コストは軽量で、現状の銘柄数規模（数〜数十件）では実用上問題にならない
- Redisの価格履歴キャッシュはgoroutineのライフサイクルと独立して保持されるため、
  監視の一時停止・再開があっても履歴は失われない
- `nextPollTime`は絶対時刻ベースの計算のため、再起動しても次回ポーリング予定時刻はズレない

### データフロー

```
5分ごとに繰り返す:
  1. watchlist.Repository.FindAllStockCodes(ctx) で全ユーザーの重複除去済み銘柄コード一覧を取得
  2. 前回取得したリストと比較
  3. 変化がなければ何もしない
  4. 変化があれば:
     a. 現在の StartMonitoring 実行（とその中の全 per-code goroutine）を context cancel で停止
     b. 新しい銘柄リストで StartMonitoring を再度起動
```

## コンポーネント変更

### 1. `internal/domain/watchlist/repository.go`

`Repository`インターフェースに追加：

```go
FindAllStockCodes(ctx context.Context) ([]stock.StockCode, error)
```

全ユーザーのwatchlistから重複を除いた銘柄コード一覧を返す。

### 2. `internal/infrastructure/persistence/watchlist_repository.go`

`PgWatchlistRepository`に`FindAllStockCodes`を実装。
`SELECT DISTINCT stock_code FROM watchlist`相当のクエリで取得し、`stock.NewStockCode`で
バリデーションしてから返す（既存の`FindByUserID`と同じパターン）。

### 3. `internal/usecase/monitor_stocks.go`

`MonitorUsecase`に新メソッドを追加：

```go
func (u *MonitorUsecase) RunWithDynamicWatchlist(
    ctx context.Context,
    repo watchlist.Repository,
    hour, minute int,
    refreshInterval time.Duration,
)
```

- `refreshInterval`ごとに`repo.FindAllStockCodes`を呼び、前回のリストと比較
- リストが変化した場合のみ、現行の監視をcancelして`StartMonitoring`を新しいgoroutineとして再起動
- `repo.FindAllStockCodes`がエラーを返した場合は、直前のリストを維持したままログ出力のみ行い、
  監視を止めない（初回起動時に失敗した場合はリストが空のまま待機しつつ次回リフレッシュで再試行）
- `ctx`がキャンセルされたら、現行の監視をcancelしてリターン

既存の`StartMonitoring`・`CheckStock`のシグネチャ・ロジックは変更しない。

### 4. `cmd/api/main.go`

- `STOCK_CODES`の`mustEnv`呼び出しと、それをパースしていた一群のコード（`strings.Split`
  ループ、`codes`変数、`len(codes) == 0`のチェック）を削除
- `monitor.StartMonitoring(ctx, codes, pollHour, pollMinute)`の呼び出しを
  `monitor.RunWithDynamicWatchlist(ctx, watchlistRepo, pollHour, pollMinute, 5*time.Minute)`
  に置き換え
- `watchlistRepo`は既に`persistence.NewPgWatchlistRepository(pool)`で生成済み（Phase 4で追加）
  のものをそのまま使う

### 5. `CLAUDE.md`

環境変数一覧（本番）から`STOCK_CODES（カンマ区切り4桁コード）`の記述を削除。

## エラーハンドリング

- DB取得失敗時：直前のリストを維持し、エラーログのみ出力。監視は継続（既に動いている
  goroutineはそのまま動き続ける）
- 初回起動時にDB取得が失敗した場合：銘柄ゼロで起動し、待機しながら次回リフレッシュで再試行
- watchlistが空（誰も銘柄を登録していない）の場合：エラーにはせず、監視ゼロの状態でログ出力
  しつつ待機し続ける

## テスト方針

- `PgWatchlistRepository.FindAllStockCodes`：既存の`watchlist_repository_test.go`と同様、
  `testing.Short()`でスキップする統合テストとして追加（実DBでdistinct取得を検証）
- `MonitorUsecase.RunWithDynamicWatchlist`：既存の`mockWatchlistRepository`
  （`manage_watchlist_test.go`）に`FindAllStockCodes`を追加し、短い`refreshInterval`
  （数十ミリ秒程度）で複数サイクル回して以下を検証：
  1. リストが変化した時に監視が再起動されること
  2. 変化がない時は再起動しないこと
  3. DB取得失敗時は直前のリストを維持すること
  - `-race`必須（goroutineの並行制御を含むため）
- 既存の`StartMonitoring`・`CheckStock`・`nextPollTime`のテストは変更なし
