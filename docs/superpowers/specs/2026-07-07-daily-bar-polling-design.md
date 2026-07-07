# 日足ポーリング化による異常検知の是正（Finding 3 / 2）

## 背景と問題

現行実装は `time.NewTicker(1 * time.Minute)` で**1分ごと**に J-Quants の日足エンドポイント
`/v2/equities/bars/daily` を叩き、取得した終値を Redis のスライディングウィンドウ（30枠）へ Push している。

日足終値は取引日内では変化しないため、1分ごとのポーリングでは**同一値が30枠を埋め尽くす**。
その結果 `anomaly.DetectionService.Calculate` の標準偏差が 0 となり、`Calculate` は `(0, nil)` を
返して `IsAnomaly` が常に `false` になる。**異常検知が実質機能しない**（Finding 3）。

加えて `FetchLatest` は `data[0]`（配列先頭）を返しており、レスポンスが範囲を返す場合に最新である
保証がない（Finding 2）。

## ゴール

- ポーリングを **1営業日1回**にし、30営業日ぶんの異なる日足終値でウィンドウを満たす。
- 取得タイミングは大引け（JST 15:30）以降の確定値を確実に取るため、**JST の特定時刻に毎日実行**。
- 土日祝に実行されても重複が混入しないよう、**取引日（Date）ベースの dedup** を行う。
- `FetchLatest` が **最新取引日**のバーを返すよう是正する（Finding 2）。

非ゴール（YAGNI）:
- 祝日カレンダー導入・平日判定による実行抑制（Date dedup で重複は防げるため不要）。
- 分足/リアルタイム価格対応（本設計は「日次の異常」を検知する方針A）。
- watchlist 機構の配線（現状デッドコード。本タスクのスコープ外）。

## 設計判断（確定事項）

1. **取得タイミング**: JST の特定時刻に毎日実行。
2. **実行時刻**: 環境変数 `POLL_TIME`（`HH:MM` 形式、JST 前提）で設定可能。デフォルト `16:00`。
3. **Finding 2 の是正**: レスポンス `data[]` の中で `Date`（`YYYY-MM-DD`）が最新のバーの終値 `C` を返す。
4. **重複対策**: Date dedup。直近に積んだ取引日を Redis に保持し、取得したバーの `Date` が同じなら Push しない。判定は usecase 層で行う。

## コンポーネントとデータフロー

### 値オブジェクト `stock.Quote`

`FetchLatest` の戻り値を価格のみから「終値＋取引日」の組に変更する。

```go
type Quote struct {
    Price Price
    Date  string // 取引日 YYYY-MM-DD（J-Quants の Date フィールド）
}
```

`PriceFetcher` インターフェース:

```go
type PriceFetcher interface {
    FetchLatest(code StockCode) (Quote, error)
}
```

### 1回の実行フロー（CheckStock）

```
① FetchLatest(code) → Quote{Price, Date}
     - jquants_client は data[] を走査し Date が最大（最新）のバーを選択（Finding 2 解決）
② cache.LastDate(code) と Quote.Date を比較
     ├─ 同じ  → 何もしない（dedup。detected=false を返す）
     └─ 違う  → cache.Push(code, Price) して cache.SetLastDate(code, Date)
③ GetHistory(30) → 30 件揃えば Z-score 計算 → IsAnomaly 判定
     - 30 件未満はウォームアップ期間として detected=false
```

### スケジューリング（StartMonitoring）

- 各銘柄の goroutine 内で「次の `POLL_TIME`（JST）までの待機時間」を計算し、`time.Timer` で待つ。
- 発火後は次の同時刻（24時間後）を再計算して待機するループ。相対 24h ticker ではなく**壁時計基準**にすることで、実行時刻がずれ続ける問題を防ぐ。
- `ctx.Done()` でグレースフルに終了。
- `POLL_TIME` のパースは main.go 起動時に行い、不正値は `log.Fatalf` で早期終了。

### キャッシュ層の追加メソッド

`PriceCache` インターフェースに直近取引日の取得・更新を追加する。

```go
type PriceCache interface {
    Push(code StockCode, price Price) error
    GetHistory(code StockCode, n int) ([]Price, error)
    LastDate(code StockCode) (string, error) // 未設定なら "" を返す
    SetLastDate(code StockCode, date string) error
}
```

Redis 実装は `lastdate:<4桁コード>` キーで取引日文字列を保持する。`Push` に dedup 判定を混ぜず、
責務を分離する。

## 環境変数

| 変数 | 説明 | デフォルト |
|------|------|-----------|
| `POLL_TIME` | 日次ポーリング実行時刻（JST, `HH:MM`） | `16:00` |

既存の `DATABASE_URL` / `REDIS_URL` / `JQUANTS_API_KEY` / `STOCK_CODES` / `ANOMALY_THRESHOLD` は変更なし。
`.env.example` に `POLL_TIME` を追記する（併せて Finding 1 のプレースホルダ復帰も別途対応）。

## エラーハンドリング

- `FetchLatest` 失敗・`Push`/`GetHistory` 失敗は従来どおり CheckStock がラップして返し、goroutine 側で
  ログ出力のうえ次回実行まで継続（1銘柄の失敗が他へ波及しない）。
- `data[]` が空なら従来どおり `no quote data` エラー。
- `POLL_TIME` 不正値は起動時 Fatal。

## テスト方針

- `-race` 必須、`httptest.NewServer` でモック（実 API 呼び出しなし）。
- **Finding 2**: `data[]` に複数日・順不同のバーを入れ、最新 `Date` の終値が返ることを検証。
- **dedup**: 同じ `Date` を2回返すモックで、履歴が1件しか増えないことを検証。
- **異常検知**: 30営業日ぶんの異なる終値を投入し、閾値超えで `detected=true` になることを検証。
- **スケジューリング**: 「次の POLL_TIME までの待機時間計算」を純関数に切り出し、境界（同日時刻前/後、日跨ぎ）を単体テスト。
- 既存テスト（`jquants_client_test.go` 等）は戻り値変更に追随して更新。

## 影響ファイル（見込み）

- `internal/domain/stock/value_object.go` — `Quote` 型追加
- `internal/domain/stock/price_fetcher.go` — 戻り値を `Quote` に変更
- `internal/domain/stock/price_cache.go` — `LastDate`/`SetLastDate` 追加
- `internal/interface/gateway/jquants_client.go` — `Date` パース＋最新バー選択
- `internal/infrastructure/cache/redis_price_cache.go` — `lastdate:` キー実装
- `internal/usecase/monitor_stocks.go` — dedup 判定、壁時計基準スケジューリング
- `cmd/api/main.go` — `POLL_TIME` 読み込み・パース
- 各 `_test.go` — 上記に追随
- `.env.example` — `POLL_TIME` 追記
</content>
</invoke>
