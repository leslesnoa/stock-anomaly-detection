# 日足ポーリング化による異常検知の是正 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 1分ポーリングで日足が重複し stddev=0 となり異常検知が機能しない問題を、1営業日1回・JST特定時刻実行＋Date dedup で是正する。

**Architecture:** `FetchLatest` の戻り値を `Price` から `Quote{Price, Date}` へ拡張し取引日を取得（Finding 2 も是正）。usecase 層で「直近に取り込んだ取引日」と比較して同一なら Push しない（dedup）。監視 goroutine は相対 ticker をやめ、毎回「次の JST 実行時刻」を壁時計基準で算出して `time.Timer` で待つ。

**Tech Stack:** Go, testify (mock/assert/require), redis/go-redis v9, httptest。

## Global Constraints

- Go モジュール: `github.com/stock-anomaly-detection/go-api`（`go-api/` 内で操作）。
- 全テストは `-race` 必須。単体テストは `go test -race -short ./...`（外部サービス不要）。
- 統合テストは `testing.Short()` または環境変数未設定でスキップ。
- J-Quants クライアントは `httptest.NewServer` でモック（実 API 呼び出しなし）。
- 依存方向: `infrastructure`/`usecase` → `domain`（逆方向禁止）。リポジトリ/ゲートウェイのインターフェースは domain に定義。
- Redis キー: 価格リストは `price:<4桁コード>`、直近取引日は `lastdate:<4桁コード>`。
- J-Quants API V2: `x-api-key` ヘッダー、`/v2/equities/bars/daily`、レスポンス `data[].C`（終値）・`data[].Date`（取引日 `YYYY-MM-DD`）。コードは4桁→末尾0付与の5桁。
- コミットメッセージ末尾に `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` を付ける。

## File Structure

- `go-api/internal/domain/stock/value_object.go` — `Quote` 型を追加
- `go-api/internal/domain/stock/price_fetcher.go` — `FetchLatest` の戻り値を `Quote` に変更
- `go-api/internal/domain/stock/price_cache.go` — `LastDate`/`SetLastDate` を追加
- `go-api/internal/interface/gateway/jquants_client.go` — `Date` をパースし最新取引日のバーを選択
- `go-api/internal/infrastructure/cache/redis_price_cache.go` — `lastdate:` キーの読み書き
- `go-api/internal/usecase/monitor_stocks.go` — dedup 判定、壁時計基準スケジューリング、`nextPollTime` 純関数
- `go-api/cmd/api/main.go` — `POLL_TIME` 読み込み・`parsePollTime` パース・配線
- 各 `_test.go` — 上記に追随
- `.env.example` — `POLL_TIME` 追記＋Finding 1（実APIキー）のプレースホルダ復帰

---

## Task 1: `Quote` 型導入と `FetchLatest` の Date 対応（Finding 2 是正）

**Files:**
- Modify: `go-api/internal/domain/stock/value_object.go`
- Modify: `go-api/internal/domain/stock/price_fetcher.go`
- Modify: `go-api/internal/interface/gateway/jquants_client.go`
- Modify: `go-api/internal/usecase/monitor_stocks.go:39-47`
- Test: `go-api/internal/interface/gateway/jquants_client_test.go`
- Test: `go-api/internal/usecase/monitor_stocks_test.go`

**Interfaces:**
- Produces: `stock.Quote{ Price stock.Price; Date string }`
- Produces: `stock.PriceFetcher.FetchLatest(code StockCode) (Quote, error)`
- Produces: `gateway.JQuantsClient.FetchLatest(code stock.StockCode) (stock.Quote, error)` — `data[]` の中で `Date` が最大（最新）のバーを返す

- [ ] **Step 1: `jquants_client_test.go` の既存テストを Quote 前提に更新し、最新Date選択テストを追加**

`go-api/internal/interface/gateway/jquants_client_test.go` を以下で置き換える（`writeJSON` はそのまま残す）:

```go
func TestJQuantsClient_FetchLatest(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v2/equities/bars/daily", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "72030", r.URL.Query().Get("code"))
		// 順不同・複数日: 最新の 2026-07-07 (3250) が選ばれるべき
		writeJSON(w, map[string]interface{}{
			"data": []map[string]interface{}{
				{"C": 3200.0, "Date": "2026-07-06"},
				{"C": 3250.0, "Date": "2026-07-07"},
				{"C": 3100.0, "Date": "2026-07-03"},
			},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewJQuantsClientWithBaseURL("test-api-key", srv.URL)
	code, _ := stock.NewStockCode("7203")

	quote, err := client.FetchLatest(code)
	require.NoError(t, err)
	assert.Equal(t, stock.Price(3250.0), quote.Price)
	assert.Equal(t, "2026-07-07", quote.Date)
}

func TestJQuantsClient_FetchLatest_NoData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/equities/bars/daily", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"data": []interface{}{}})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewJQuantsClientWithBaseURL("key", srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchLatest(code)
	require.Error(t, err)
}
```

- [ ] **Step 2: テストを実行してコンパイル失敗を確認**

Run（`go-api/` ディレクトリから）: `go build ./...`
Expected: FAIL（`quote.Price`/`quote.Date` 未定義、`FetchLatest` の戻り値不一致でコンパイルエラー）

- [ ] **Step 3: `Quote` 型を追加**

`go-api/internal/domain/stock/value_object.go` の末尾（`type Volume int64` の下）に追加:

```go
// Quote は1営業日の終値と取引日（YYYY-MM-DD）の組。
type Quote struct {
	Price Price
	Date  string
}
```

- [ ] **Step 4: `PriceFetcher` インターフェースを変更**

`go-api/internal/domain/stock/price_fetcher.go` を置き換える:

```go
package stock

type PriceFetcher interface {
	FetchLatest(code StockCode) (Quote, error)
}
```

- [ ] **Step 5: `jquants_client.go` を Date パース＋最新バー選択に変更**

`go-api/internal/interface/gateway/jquants_client.go` の `FetchLatest` を置き換える:

```go
func (c *JQuantsClient) FetchLatest(code stock.StockCode) (stock.Quote, error) {
	// J-Quantsは4桁コードに末尾0を付けた5桁で検索する
	url := fmt.Sprintf("%s/v2/equities/bars/daily?code=%s0", c.baseURL, code)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return stock.Quote{}, err
	}
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return stock.Quote{}, fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return stock.Quote{}, fmt.Errorf("jquants api returned status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			C    float64 `json:"C"`
			Date string  `json:"Date"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return stock.Quote{}, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Data) == 0 {
		return stock.Quote{}, fmt.Errorf("no quote data for %s", code)
	}

	// data[] の並び順に依存せず、Date が最新のバーを選ぶ（Finding 2）
	latest := result.Data[0]
	for _, d := range result.Data[1:] {
		if d.Date > latest.Date {
			latest = d
		}
	}
	return stock.Quote{Price: stock.Price(latest.C), Date: latest.Date}, nil
}
```

- [ ] **Step 6: `monitor_stocks.go` の呼び出し側を Quote に追従（dedup はまだ入れない）**

`go-api/internal/usecase/monitor_stocks.go:40-47` を置き換える:

```go
	quote, err := u.fetcher.FetchLatest(code)
	if err != nil {
		return false, 0, fmt.Errorf("fetch %s: %w", code, err)
	}

	if err := u.cache.Push(code, quote.Price); err != nil {
		return false, 0, fmt.Errorf("cache push %s: %w", code, err)
	}
```

- [ ] **Step 7: `monitor_stocks_test.go` の MockPriceFetcher を Quote 返却に更新**

`go-api/internal/usecase/monitor_stocks_test.go` の `MockPriceFetcher.FetchLatest` を置き換える:

```go
func (m *MockPriceFetcher) FetchLatest(code stock.StockCode) (stock.Quote, error) {
	args := m.Called(code)
	return args.Get(0).(stock.Quote), args.Error(1)
}
```

同ファイルの2箇所の `fetcher.On(...)` を置き換える:

```go
// TestMonitorUsecase_CheckStock_DetectsAnomaly 内
fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 130.0, Date: "2026-07-07"}, nil)
```

```go
// TestMonitorUsecase_CheckStock_InsufficientHistory 内
fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 5000.0, Date: "2026-07-07"}, nil)
```

- [ ] **Step 8: ビルドと単体テストを実行して通ることを確認**

Run（`go-api/` から）: `go build ./... && go test -race -short ./...`
Expected: PASS（gateway・usecase 含め全パッケージ ok）

- [ ] **Step 9: コミット**

```bash
git add go-api/internal/domain/stock/value_object.go \
        go-api/internal/domain/stock/price_fetcher.go \
        go-api/internal/interface/gateway/jquants_client.go \
        go-api/internal/interface/gateway/jquants_client_test.go \
        go-api/internal/usecase/monitor_stocks.go \
        go-api/internal/usecase/monitor_stocks_test.go
git commit -m "feat: return Quote with Date from FetchLatest, select latest bar (Finding 2)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: `PriceCache` に `LastDate`/`SetLastDate` を追加

**Files:**
- Modify: `go-api/internal/domain/stock/price_cache.go`
- Modify: `go-api/internal/infrastructure/cache/redis_price_cache.go`
- Test: `go-api/internal/infrastructure/cache/redis_price_cache_test.go`
- Modify: `go-api/internal/usecase/monitor_stocks_test.go`（MockPriceCache にメソッド追加）

**Interfaces:**
- Consumes: なし
- Produces: `stock.PriceCache.LastDate(code StockCode) (string, error)` — 未設定なら `("", nil)`
- Produces: `stock.PriceCache.SetLastDate(code StockCode, date string) error`

- [ ] **Step 1: 統合テストを追加（失敗させる）**

`go-api/internal/infrastructure/cache/redis_price_cache_test.go` の末尾に追加:

```go
func TestRedisPriceCache_LastDate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL not set")
	}

	opt, err := redis.ParseURL(redisURL)
	require.NoError(t, err)
	client := redis.NewClient(opt)
	defer client.Close()

	ctx := context.Background()
	c := cache.NewRedisPriceCache(client)
	code, _ := stock.NewStockCode("7203")
	client.Del(ctx, "lastdate:7203")

	// 未設定なら空文字
	d, err := c.LastDate(code)
	require.NoError(t, err)
	assert.Equal(t, "", d)

	// セットして読み戻す
	require.NoError(t, c.SetLastDate(code, "2026-07-07"))
	d, err = c.LastDate(code)
	require.NoError(t, err)
	assert.Equal(t, "2026-07-07", d)
}
```

- [ ] **Step 2: コンパイル失敗を確認**

Run（`go-api/` から）: `go build ./...`
Expected: FAIL（`c.LastDate`/`c.SetLastDate` 未定義）

- [ ] **Step 3: `PriceCache` インターフェースを拡張**

`go-api/internal/domain/stock/price_cache.go` を置き換える:

```go
package stock

type PriceCache interface {
	Push(code StockCode, price Price) error
	GetHistory(code StockCode, n int) ([]Price, error)
	LastDate(code StockCode) (string, error)
	SetLastDate(code StockCode, date string) error
}
```

- [ ] **Step 4: Redis 実装を追加**

`go-api/internal/infrastructure/cache/redis_price_cache.go` の末尾に追加:

```go
func (c *RedisPriceCache) LastDate(code stock.StockCode) (string, error) {
	ctx := context.Background()
	key := fmt.Sprintf("lastdate:%s", code)
	v, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

func (c *RedisPriceCache) SetLastDate(code stock.StockCode, date string) error {
	ctx := context.Background()
	key := fmt.Sprintf("lastdate:%s", code)
	return c.client.Set(ctx, key, date, 0).Err()
}
```

- [ ] **Step 5: MockPriceCache にメソッドを追加（コンパイル維持）**

`go-api/internal/usecase/monitor_stocks_test.go` の `MockPriceCache` の下（`GetHistory` メソッドの後）に追加:

```go
func (m *MockPriceCache) LastDate(code stock.StockCode) (string, error) {
	args := m.Called(code)
	return args.String(0), args.Error(1)
}

func (m *MockPriceCache) SetLastDate(code stock.StockCode, date string) error {
	return m.Called(code, date).Error(0)
}
```

- [ ] **Step 6: ビルドと単体テストを確認**

Run（`go-api/` から）: `go build ./... && go test -race -short ./...`
Expected: PASS（新規統合テストは `-short` でスキップ、既存テストは緑）

- [ ] **Step 7: コミット**

```bash
git add go-api/internal/domain/stock/price_cache.go \
        go-api/internal/infrastructure/cache/redis_price_cache.go \
        go-api/internal/infrastructure/cache/redis_price_cache_test.go \
        go-api/internal/usecase/monitor_stocks_test.go
git commit -m "feat: add LastDate/SetLastDate to PriceCache (lastdate: redis key)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: usecase の Date dedup 判定

**Files:**
- Modify: `go-api/internal/usecase/monitor_stocks.go:39-56`
- Test: `go-api/internal/usecase/monitor_stocks_test.go`

**Interfaces:**
- Consumes: `stock.Quote`, `PriceCache.LastDate`, `PriceCache.SetLastDate`
- Produces: `CheckStock` は取得バーの `Date` が直近取り込み済み取引日と同じなら `Push` せず `(false, 0, nil)` を返す

- [ ] **Step 1: dedup テストを追加（失敗させる）**

`go-api/internal/usecase/monitor_stocks_test.go` の末尾に追加:

```go
func TestMonitorUsecase_CheckStock_SkipsDuplicateDate(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	priceCache := &MockPriceCache{}

	code, _ := stock.NewStockCode("7203")
	// 取得したバーの取引日が、直近取り込み済みの取引日と同じ
	fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 3250.0, Date: "2026-07-07"}, nil)
	priceCache.On("LastDate", code).Return("2026-07-07", nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, priceCache, svc, 2.5)

	detected, _, err := uc.CheckStock(context.Background(), code)
	require.NoError(t, err)
	assert.False(t, detected, "duplicate trading date must not be pushed")

	// Push / SetLastDate / GetHistory は呼ばれない
	priceCache.AssertNotCalled(t, "Push", code, stock.Price(3250.0))
	priceCache.AssertNotCalled(t, "SetLastDate", code, "2026-07-07")
	priceCache.AssertNotCalled(t, "GetHistory", code, 30)
	fetcher.AssertExpectations(t)
}
```

- [ ] **Step 2: 既存テストに LastDate/SetLastDate の期待を追加**

同ファイルの `TestMonitorUsecase_CheckStock_DetectsAnomaly` と `TestMonitorUsecase_CheckStock_InsufficientHistory` の、各 `priceCache.On("Push", ...)` の直前に以下を追加（取引日が異なるので Push まで進む）:

```go
	priceCache.On("LastDate", code).Return("2026-07-06", nil)
	priceCache.On("SetLastDate", code, "2026-07-07").Return(nil)
```

- [ ] **Step 3: テスト実行で失敗を確認**

Run（`go-api/` から）: `go test -race -short -run TestMonitorUsecase_CheckStock_SkipsDuplicateDate ./internal/usecase/`
Expected: FAIL（現状 `LastDate` が呼ばれず、Push が実行されるため mock が予期しない呼び出しで失敗、または detected 判定不一致）

- [ ] **Step 4: `CheckStock` に dedup を実装**

`go-api/internal/usecase/monitor_stocks.go` の `CheckStock` の先頭部分（`quote, err := ...` から `Push` まで）を置き換える:

```go
	quote, err := u.fetcher.FetchLatest(code)
	if err != nil {
		return false, 0, fmt.Errorf("fetch %s: %w", code, err)
	}

	lastDate, err := u.cache.LastDate(code)
	if err != nil {
		return false, 0, fmt.Errorf("last date %s: %w", code, err)
	}
	// 同一取引日は既に取り込み済み。重複を積むと標準偏差が歪むため Push しない。
	if quote.Date == lastDate {
		return false, 0, nil
	}

	if err := u.cache.Push(code, quote.Price); err != nil {
		return false, 0, fmt.Errorf("cache push %s: %w", code, err)
	}
	if err := u.cache.SetLastDate(code, quote.Date); err != nil {
		return false, 0, fmt.Errorf("set last date %s: %w", code, err)
	}
```

- [ ] **Step 5: 単体テストを実行して通ることを確認**

Run（`go-api/` から）: `go test -race -short ./internal/usecase/`
Expected: PASS（3テストとも緑）

- [ ] **Step 6: コミット**

```bash
git add go-api/internal/usecase/monitor_stocks.go \
        go-api/internal/usecase/monitor_stocks_test.go
git commit -m "feat: skip duplicate trading date in CheckStock (dedup, fixes Finding 3)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: 壁時計基準スケジューリング（`nextPollTime` + Timer 化）

**Files:**
- Modify: `go-api/internal/usecase/monitor_stocks.go`
- Create: `go-api/internal/usecase/monitor_schedule_internal_test.go`
- Modify: `go-api/cmd/api/main.go:75`（一時的に固定値 16,0 を渡す。Task 5 で POLL_TIME に差し替え）

**Interfaces:**
- Produces: `nextPollTime(now time.Time, hour, minute int) time.Time`（package 内非公開、JST 基準）
- Produces: `MonitorUsecase.StartMonitoring(ctx context.Context, codes []stock.StockCode, hour, minute int)`

- [ ] **Step 1: `nextPollTime` の内部テストを作成（失敗させる）**

`go-api/internal/usecase/monitor_schedule_internal_test.go` を新規作成（`package usecase` — 非公開関数にアクセスするため内部テスト）:

```go
package usecase

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNextPollTime(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)

	cases := []struct {
		name       string
		now        time.Time
		hour, min  int
		wantY      int
		wantM      time.Month
		wantD      int
		wantHH     int
		wantMM     int
	}{
		{"実行時刻前は当日", time.Date(2026, 7, 7, 10, 0, 0, 0, jst), 16, 0, 2026, 7, 7, 16, 0},
		{"実行時刻ちょうどは翌日", time.Date(2026, 7, 7, 16, 0, 0, 0, jst), 16, 0, 2026, 7, 8, 16, 0},
		{"実行時刻後は翌日", time.Date(2026, 7, 7, 18, 0, 0, 0, jst), 16, 0, 2026, 7, 8, 16, 0},
		{"深夜跨ぎ", time.Date(2026, 7, 7, 23, 30, 0, 0, jst), 0, 15, 2026, 7, 8, 0, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextPollTime(tc.now, tc.hour, tc.min).In(jst)
			assert.Equal(t, tc.wantY, got.Year())
			assert.Equal(t, tc.wantM, got.Month())
			assert.Equal(t, tc.wantD, got.Day())
			assert.Equal(t, tc.wantHH, got.Hour())
			assert.Equal(t, tc.wantMM, got.Minute())
		})
	}
}
```

- [ ] **Step 2: コンパイル失敗を確認**

Run（`go-api/` から）: `go build ./...`
Expected: FAIL（`nextPollTime` 未定義）

- [ ] **Step 3: `nextPollTime` と JST ロケーションを追加**

`go-api/internal/usecase/monitor_stocks.go` の import に `"time"` があることを確認（既にあり）。`const historySize = 30` の下に追加:

```go
// jst は日本市場の実行時刻計算に使う固定タイムゾーン（夏時間なし）。
var jst = time.FixedZone("JST", 9*60*60)

// nextPollTime は now 以降で最初に来る JST hour:minute の時刻を返す。
// now がちょうど実行時刻の場合は翌日を返す。
func nextPollTime(now time.Time, hour, minute int) time.Time {
	n := now.In(jst)
	next := time.Date(n.Year(), n.Month(), n.Day(), hour, minute, 0, 0, jst)
	if !next.After(n) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
```

- [ ] **Step 4: `StartMonitoring` を Timer ベースに変更**

`go-api/internal/usecase/monitor_stocks.go` の `StartMonitoring` を置き換える:

```go
// StartMonitoring はgoroutineで全銘柄を並行監視する。
// 各銘柄は毎日 JST hour:minute（大引け後の想定）に1回チェックする。
// ctx がキャンセルされるまでブロックする。
func (u *MonitorUsecase) StartMonitoring(ctx context.Context, codes []stock.StockCode, hour, minute int) {
	var wg sync.WaitGroup
	for _, code := range codes {
		wg.Add(1)
		go func(code stock.StockCode) {
			defer wg.Done()
			for {
				timer := time.NewTimer(time.Until(nextPollTime(time.Now(), hour, minute)))
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
					detected, z, err := u.CheckStock(ctx, code)
					if err != nil {
						log.Printf("ERROR monitoring %s: %v", code, err)
						continue
					}
					if detected {
						log.Printf("ANOMALY %s z=%.2f (threshold=%.1f)", code, z, u.threshold)
					}
				}
			}
		}(code)
	}
	wg.Wait()
}
```

- [ ] **Step 5: main.go の呼び出しを一時的に固定値へ（ビルド維持）**

`go-api/cmd/api/main.go:75` を置き換える（Task 5 で `POLL_TIME` に差し替える）:

```go
	monitor.StartMonitoring(ctx, codes, 16, 0)
```

- [ ] **Step 6: ビルドと単体テストを確認**

Run（`go-api/` から）: `go build ./... && go test -race -short ./...`
Expected: PASS（`TestNextPollTime` の4ケース含め緑）

- [ ] **Step 7: コミット**

```bash
git add go-api/internal/usecase/monitor_stocks.go \
        go-api/internal/usecase/monitor_schedule_internal_test.go \
        go-api/cmd/api/main.go
git commit -m "feat: poll once daily at JST wall-clock time via Timer (fixes Finding 3)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: `POLL_TIME` 環境変数の配線と `.env.example` 是正

**Files:**
- Modify: `go-api/cmd/api/main.go`
- Create: `go-api/cmd/api/main_test.go`
- Modify: `.env.example`

**Interfaces:**
- Consumes: `MonitorUsecase.StartMonitoring(ctx, codes, hour, minute int)`
- Produces: `parsePollTime(s string) (hour, minute int, err error)`（package main、`"HH:MM"` をパース）

- [ ] **Step 1: `parsePollTime` のテストを作成（失敗させる）**

`go-api/cmd/api/main_test.go` を新規作成（`package main`）:

```go
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePollTime(t *testing.T) {
	h, m, err := parsePollTime("16:00")
	require.NoError(t, err)
	assert.Equal(t, 16, h)
	assert.Equal(t, 0, m)

	h, m, err = parsePollTime("09:30")
	require.NoError(t, err)
	assert.Equal(t, 9, h)
	assert.Equal(t, 30, m)
}

func TestParsePollTime_Invalid(t *testing.T) {
	for _, s := range []string{"", "16", "16:60", "24:00", "aa:bb", "16:0:0", "-1:00"} {
		_, _, err := parsePollTime(s)
		assert.Error(t, err, "input %q should be rejected", s)
	}
}
```

- [ ] **Step 2: コンパイル失敗を確認**

Run（`go-api/` から）: `go build ./...`
Expected: FAIL（`parsePollTime` 未定義）

- [ ] **Step 3: `parsePollTime` を実装し、main の import に `fmt` を追加**

`go-api/cmd/api/main.go` の import ブロックに `"fmt"` を追加（`"context"` の下など）。ファイル末尾（`mustEnv` の下）に追加:

```go
// parsePollTime は "HH:MM"（24時間表記）を時・分に分解する。
// %s で余剰入力を捕まえ、"16:0:0" のような不正形式を厳密に弾く。
func parsePollTime(s string) (int, int, error) {
	var hour, minute int
	var rest string
	n, _ := fmt.Sscanf(s, "%d:%d%s", &hour, &minute, &rest)
	if n < 2 || rest != "" {
		return 0, 0, fmt.Errorf("POLL_TIME must be HH:MM, got %q", s)
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("POLL_TIME out of range: %q", s)
	}
	return hour, minute, nil
}
```

> パース挙動: `"16:00"` は rest="" で通る。`"16:0:0"` は rest=":0" で弾かれる。`"16"` は n=1 で弾かれる。`"aa:bb"` は n=0 で弾かれる。`"16:60"`/`"24:00"`/`"-1:00"` は範囲チェックで弾かれる。

- [ ] **Step 4: main で `POLL_TIME` を読み込み StartMonitoring へ渡す**

`go-api/cmd/api/main.go` の `stockCodesRaw := mustEnv("STOCK_CODES")` の下に追加:

```go
	pollHour, pollMinute := 16, 0
	if pt := os.Getenv("POLL_TIME"); pt != "" {
		var err error
		pollHour, pollMinute, err = parsePollTime(pt)
		if err != nil {
			log.Fatalf("invalid POLL_TIME: %v", err)
		}
	}
```

`go-api/cmd/api/main.go` の `monitor.StartMonitoring(ctx, codes, 16, 0)` を置き換える:

```go
	log.Printf("monitoring %d stocks (threshold=%.1fσ, poll=%02d:%02d JST)", len(codes), threshold, pollHour, pollMinute)
	monitor.StartMonitoring(ctx, codes, pollHour, pollMinute)
```

> 併せて既存の `log.Printf("monitoring %d stocks (threshold=%.1fσ)", len(codes), threshold)` 行（変更前の74行目相当）を削除する（上の1行に統合済み）。

- [ ] **Step 5: `.env.example` を是正（POLL_TIME 追記＋実APIキーのプレースホルダ復帰）**

`.env.example` を以下の内容に置き換える（Finding 1: 実APIキーをプレースホルダに戻す）:

```
DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly?sslmode=disable
REDIS_URL=redis://localhost:6379
JQUANTS_API_KEY=your-jquants-api-key
STOCK_CODES=7203,6758,9984
ANOMALY_THRESHOLD=2.5
POLL_TIME=16:00
```

- [ ] **Step 6: ビルドと全単体テストを確認**

Run（`go-api/` から）: `go build ./... && go test -race -short ./...`
Expected: PASS（`parsePollTime` の2テスト含め全パッケージ緑）

- [ ] **Step 7: コミット**

```bash
git add go-api/cmd/api/main.go go-api/cmd/api/main_test.go .env.example
git commit -m "feat: wire POLL_TIME env var, restore .env.example placeholder (Finding 1)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- 取得タイミング（JST特定時刻・毎日）→ Task 4（`nextPollTime` + Timer）✓
- 実行時刻を環境変数 `POLL_TIME`（デフォルト16:00）→ Task 5 ✓
- Finding 2（最新 Date のバー選択）→ Task 1 Step 5 ✓
- Date dedup（usecase 層、Redis `lastdate:` 保持）→ Task 2（保持）+ Task 3（判定）✓
- `Quote` 値オブジェクト → Task 1 ✓
- `.env.example` に POLL_TIME 追記＋Finding 1 復帰 → Task 5 Step 5 ✓
- テスト（Finding 2 順不同・dedup・スケジューリング境界）→ Task 1/3/4 に配置 ✓
- 非ゴール（祝日カレンダー・分足・watchlist配線）→ プランに含めない ✓

**Placeholder scan:** TBD/TODO/「適切なエラー処理」等なし。全コードステップに実コードを記載 ✓

**Type consistency:**
- `stock.Quote{Price, Date}` — Task 1 で定義、Task 3 テストで同名フィールド使用 ✓
- `FetchLatest(code) (Quote, error)` — Task 1 定義、Mock（Task 1）と一致 ✓
- `LastDate(code) (string, error)` / `SetLastDate(code, date) error` — Task 2 定義、Mock（Task 2）・CheckStock（Task 3）で一致 ✓
- `StartMonitoring(ctx, codes, hour, minute int)` — Task 4 定義、main（Task 4 固定値→Task 5 変数）で一致 ✓
- `nextPollTime(now, hour, minute)` / `parsePollTime(s)` — 定義とテストで一致 ✓
</content>
