# 株価異常検知 × AI自動分析通知システム

## 作業フロー（厳守）
実装作業は必ず以下の順序で行うこと。順序を飛ばしてはいけない。

1. **ブランチ作成** — `git checkout -b feature/<task-name>`（mainで直接作業禁止）
2. **プラン作成** — `superpowers:writing-plans` スキルで実装計画を作成・承認を得る
3. **実装** — 承認済みプランに従って実装する
4. **Push & PR作成** — 実装完了後は必ずリモートにpushしてPull Requestを作成する（ローカルマージや保留のまま放置しない）

> ハーネスはmainブランチでのコードファイル編集を自動的にブロックします。

## プロジェクト構造
- モノレポ: `go-api/`（Goサーバー）、`python-engine/`（将来）、`frontend/`（将来）
- Goモジュール: `github.com/stock-anomaly-detection/go-api`（`go-api/`内で操作）
- Railway/Vercelへのデプロイ手順は`docs/deployment/phase5-railway-vercel-setup.md`参照

## Goコマンド（`go-api/`ディレクトリから実行）
- `go test -race -short ./...` — 単体テスト（外部サービス不要）
- `go test -race ./...` — 統合テスト（DATABASE_URL + REDIS_URL 必要）
- `go build ./...` — ビルド確認

## アーキテクチャ
- Clean Architecture + Pragmatic DDD（bounded context・domain eventsなし）
- 依存方向: `infrastructure/usecase` → `domain`（逆方向禁止）
- リポジトリインターフェースはdomainパッケージ内に定義

## 重要な設計決定
- DBスキーマ: UUID PK（`gen_random_uuid()`）、`int64`不使用
- `WatchlistRepository.FindByUserID(ctx, userID string)` — userIDはUUID文字列
- Redisキー: `price:<4桁コード>`、スライディングウィンドウ30件
- 株価取得: Go側（`gateway.YahooFinanceClient`）がYahoo Financeの非公式chart API `query1.finance.yahoo.com/v8/finance/chart/{4桁コード}.T` を認証不要で利用（`User-Agent`ヘッダーのみ必要）。非公式APIのためSLA・レート制限の明記なし。J-Quants API（無料プランで約12週間のデータ遅延あり）は2026-09-13に廃止した
- watchlist追加時（`ManageWatchlistUsecase.Add`）は`usecase.BackfillPriceHistoryUsecase`が`YahooFinanceClient.FetchHistory`（`range=3mo`）経由で過去の終値を取得しRedisへ一括投入する。対象銘柄に既に価格履歴があればスキップ（冪等）。取得失敗はログのみでwatchlist登録自体は成功させる。異常検知が有効になるまでのウォームアップ期間（`historySize=30`件分）を短縮する目的
- Phase 3: ニュース取得はGo側（`gateway.YanoshinTDnetClient`、認証不要の無料TDnet開示情報API `webapi.yanoshin.jp` を利用。非公式サービスのためSLA・レート制限の明記なし）で実施。異常検知時に `AnalyzeAndNotifyUsecase` が ニュース取得→Pythonエンジン(`/analyze`)→Claude API→Slack通知→`notifications`テーブル保存の順に実行する
- Claude APIはリトライ1回、失敗時はテクニカル指標のみのSlack通知にフォールバック。Slack通知は最大3回リトライ、失敗時も`slack_sent=false`で通知履歴を保存する
- 監視対象銘柄はDBの`watchlist`テーブル（全ユーザー横断、重複除去）から動的に取得する。`MonitorUsecase.RunWithDynamicWatchlist`が5分間隔で再読込し、銘柄セットに変化があれば監視を再起動する（`STOCK_CODES`環境変数は廃止）。異常検知の閾値は`Watchlist.AlertThreshold`ではなく引き続き`ANOMALY_THRESHOLD`のグローバル固定値を使う

## テスト方針
- 統合テスト: `testing.Short()` または環境変数未設定でスキップ
- Yahoo Financeクライアント: `httptest.NewServer`でモック（実API呼び出しなし）
- `-race`フラグ必須（並行安全性の確認のため）

## GitHub Actions CI
- postgres:16 + redis:7 サービス
- `DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable`
- `REDIS_URL: redis://localhost:6379`

## 環境変数（本番）
DATABASE_URL, REDIS_URL, ANOMALY_THRESHOLD（デフォルト2.5）, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）, JWT_SECRET, PORT（デフォルト8080）
