# Yahoo Finance価格履歴バックフィル Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** watchlistに新規銘柄が追加された時、Yahoo Financeから過去の終値をまとめて取得しRedisの価格キャッシュへ一括投入することで、異常検知が有効になるまでのウォームアップ期間（`historySize=30`件のスライディングウィンドウが埋まるまで、実運用で約6週間）を実質ゼロにする。

**Architecture:** 既存の`stock.PriceFetcher`インターフェースに`FetchHistory(code, days) ([]Quote, error)`を追加し、`gateway.YahooFinanceClient`が`range=3mo`のchart APIで実装する。新設する`usecase.BackfillPriceHistoryUsecase`がその履歴を`stock.PriceCache`へ一括Pushし、`usecase.ManageWatchlistUsecase.Add`から呼び出す。既存のGo=I/O、Redis=キャッシュという責務分担は変更しない。

**Tech Stack:** Go 1.x、`net/http`（Yahoo Finance非公式chart API）、Redis（`go-redis/v9`）、testify（mock/require/assert）

**Spec:** 本プランに埋め込み（下記）。背景は次のmemoryファイルを参照:
- `project_followup_yahoo_finance_backfill.md`（本機能の動機・スコープ確認）
- `project_followup_yahoo_finance_rate_limit.md`（関連リスク、本プランでは対応しない）

### 埋め込みSpec

- **対象:** `ManageWatchlistUsecase.Add`が呼ばれ、DBへの`Create`が成功した直後。
- **冪等性:** 対象銘柄コードの価格履歴（`price:<code>`）が既に1件以上存在する場合はバックフィルをスキップする。同一銘柄コードは複数ユーザーが独立に監視対象へ追加できるため（`watchlist`テーブルの一意制約は`(user_id, stock_code)`単位）、既に他ユーザーが追加済みで履歴が積み上がっている銘柄を上書き・重複投入しないため。
- **取得件数:** Yahoo Financeのchart APIを`range=3mo`で呼び、null（未確定値）を除いた終値のうち直近`historySize`（＝30、既存定数を再利用）件だけを古い順で返す。上場から日が浅い銘柄などで実件数が30件に満たない場合はそのまま全件を使う。
- **失敗時の扱い:** バックフィル取得・Push失敗はログ出力のみに留め、`Add`自体は成功として返す（watchlistへの登録はDBへの`Create`が成功した時点で完了しているため、バックフィルは付加的な最適化として扱う）。既存の`MonitorUsecase.notify`の「`notifyUsecase == nil`ならスキップ」というnilガード方針を踏襲し、`ManageWatchlistUsecase`も`backfiller *BackfillPriceHistoryUsecase`がnilなら何もしない。
- **リトライなし:** 既存の`YahooFinanceClient`と同じ「リトライなし」方針を維持する（[[project_followup_yahoo_finance_rate_limit]]で将来検討）。
- **スコープ外:** レート制限対策、複数銘柄の一括並行取得、`FetchHistory`の`days`可変化によるAPI呼び出し先の動的選択（常に`range=3mo`固定）。

## Global Constraints

- `historySize`（usecaseパッケージの既存定数、値30）をバックフィル件数の基準として再利用する。新しい定数は追加しない。
- 新しい外部依存（ライブラリ・サービス）は追加しない。既存の`gateway.YahooFinanceClient`が叩いている`query1.finance.yahoo.com`のchart APIをそのまま使う。
- `go test -race -short ./...`が全てパスすること（`go-api/`ディレクトリから実行）。
- `go build ./...`が通ること（`go-api/`ディレクトリから実行）。

---

## Task 1: `stock.PriceFetcher`にFetchHistoryを追加し、YahooFinanceClientで実装する

**Files:**
- Modify: `go-api/internal/domain/stock/price_fetcher.go`
- Modify: `go-api/internal/interface/gateway/yahoo_finance_client.go`
- Modify: `go-api/internal/interface/gateway/yahoo_finance_client_test.go`
- Modify: `go-api/internal/usecase/monitor_stocks_test.go`（既存`MockPriceFetcher`に新メソッド追加、コンパイル維持のため）
- Modify: `go-api/internal/usecase/monitor_notify_test.go`（既存`mockNotifyFetcher`に新メソッド追加、コンパイル維持のため）

**Interfaces:**
- Consumes: なし（このタスクが起点）
- Produces: `stock.PriceFetcher.FetchHistory(code stock.StockCode, days int) ([]stock.Quote, error)` — Task 2以降の`BackfillPriceHistoryUsecase`が利用する。戻り値は古い順（昇順）の`stock.Quote`スライス。

- [ ] **Step 1: FetchHistoryの失敗するテストを書く**

`go-api/internal/interface/gateway/yahoo_finance_client_test.go`の末尾に追記:

```go
func TestYahooFinanceClient_FetchHistory(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Mozilla/5.0", r.Header.Get("User-Agent"))
		assert.Equal(t, "3mo", r.URL.Query().Get("range"))
		assert.Equal(t, "1d", r.URL.Query().Get("interval"))

		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						// 2026-07-06, 2026-07-07, 2026-07-08 (JST 15:00)
						"timestamp": []int64{1783317600, 1783404000, 1783490400},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3200.0, 3250.0, 3300.0}},
							},
						},
					},
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	quotes, err := client.FetchHistory(code, 30)
	require.NoError(t, err)
	require.Len(t, quotes, 3)
	assert.Equal(t, stock.Quote{Price: 3200.0, Date: "2026-07-06"}, quotes[0])
	assert.Equal(t, stock.Quote{Price: 3250.0, Date: "2026-07-07"}, quotes[1])
	assert.Equal(t, stock.Quote{Price: 3300.0, Date: "2026-07-08"}, quotes[2])
}

func TestYahooFinanceClient_FetchHistory_TrimsToRequestedDays(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						// 5トレーディングデイ分あるが、直近2件だけを要求する
						"timestamp": []int64{1783058400, 1783144800, 1783317600, 1783404000, 1783490400},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3100.0, 3150.0, 3200.0, 3250.0, 3300.0}},
							},
						},
					},
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	quotes, err := client.FetchHistory(code, 2)
	require.NoError(t, err)
	require.Len(t, quotes, 2)
	assert.Equal(t, stock.Quote{Price: 3250.0, Date: "2026-07-07"}, quotes[0])
	assert.Equal(t, stock.Quote{Price: 3300.0, Date: "2026-07-08"}, quotes[1])
}

func TestYahooFinanceClient_FetchHistory_SkipsNullCloses(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						"timestamp": []int64{1783317600, 1783404000, 1783490400},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3200.0, nil, 3300.0}},
							},
						},
					},
				},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	quotes, err := client.FetchHistory(code, 30)
	require.NoError(t, err)
	require.Len(t, quotes, 2)
	assert.Equal(t, stock.Quote{Price: 3200.0, Date: "2026-07-06"}, quotes[0])
	assert.Equal(t, stock.Quote{Price: 3300.0, Date: "2026-07-08"}, quotes[1])
}

func TestYahooFinanceClient_FetchHistory_NoData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/0000.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchHistory(code, 30)
	require.Error(t, err)
}
```

- [ ] **Step 2: テストを実行して失敗を確認する**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestYahooFinanceClient_FetchHistory -v`
Expected: FAIL（`client.FetchHistory undefined`のコンパイルエラー）

- [ ] **Step 3: `stock.PriceFetcher`にFetchHistoryを追加する**

`go-api/internal/domain/stock/price_fetcher.go`を次の内容に置き換える:

```go
package stock

type PriceFetcher interface {
	FetchLatest(code StockCode) (Quote, error)
	FetchHistory(code StockCode, days int) ([]Quote, error)
}
```

- [ ] **Step 4: `YahooFinanceClient`を共通ヘルパー抽出込みでリファクタリングし、FetchHistoryを実装する**

`go-api/internal/interface/gateway/yahoo_finance_client.go`を次の内容に置き換える（既存の`FetchLatest`の挙動は変えず、chart API呼び出し・JSONデコード部分を`fetchChart`として共通化する）:

```go
package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

var yahooJST = time.FixedZone("JST", 9*60*60)

type YahooFinanceClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewYahooFinanceClient() *YahooFinanceClient {
	return NewYahooFinanceClientWithBaseURL("https://query1.finance.yahoo.com")
}

func NewYahooFinanceClientWithBaseURL(baseURL string) *YahooFinanceClient {
	return &YahooFinanceClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *YahooFinanceClient) FetchLatest(code stock.StockCode) (stock.Quote, error) {
	timestamps, closes, err := c.fetchChart(code, "5d")
	if err != nil {
		return stock.Quote{}, err
	}
	for i := len(closes) - 1; i >= 0; i-- {
		if closes[i] != nil {
			date := time.Unix(timestamps[i], 0).In(yahooJST).Format("2006-01-02")
			return stock.Quote{Price: stock.Price(*closes[i]), Date: date}, nil
		}
	}
	return stock.Quote{}, fmt.Errorf("no quote data for %s", code)
}

// FetchHistory は直近3ヶ月分の日次終値を取得し、null（未確定値）を除いた上で
// 直近days件（トレーディングデイ換算）に絞って古い順で返す。
// 上場から日が浅い銘柄など実件数がdaysに満たない場合はそのまま全件を返す。
func (c *YahooFinanceClient) FetchHistory(code stock.StockCode, days int) ([]stock.Quote, error) {
	timestamps, closes, err := c.fetchChart(code, "3mo")
	if err != nil {
		return nil, err
	}
	quotes := make([]stock.Quote, 0, len(closes))
	for i, close := range closes {
		if close == nil {
			continue
		}
		date := time.Unix(timestamps[i], 0).In(yahooJST).Format("2006-01-02")
		quotes = append(quotes, stock.Quote{Price: stock.Price(*close), Date: date})
	}
	if len(quotes) > days {
		quotes = quotes[len(quotes)-days:]
	}
	return quotes, nil
}

// fetchChart はYahoo Finance chart APIを叩き、タイムスタンプと終値の配列を返す。
// タイムスタンプ数より終値数が多い場合は終値側を切り詰める
// （データ不整合によるindex out of range panicを防ぐため）。
func (c *YahooFinanceClient) fetchChart(code stock.StockCode, rangeParam string) ([]int64, []*float64, error) {
	url := fmt.Sprintf("%s/v8/finance/chart/%s.T?range=%s&interval=1d", c.baseURL, code, rangeParam)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("yahoo finance api returned status %d", resp.StatusCode)
	}

	var result struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Close []*float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Chart.Result) == 0 || len(result.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, nil, fmt.Errorf("no quote data for %s", code)
	}

	timestamps := result.Chart.Result[0].Timestamp
	closes := result.Chart.Result[0].Indicators.Quote[0].Close
	if len(timestamps) < len(closes) {
		closes = closes[:len(timestamps)]
	}
	return timestamps, closes, nil
}
```

補足: `fetchChart`は`closes`が`timestamps`より長い場合のみ切り詰める。`FetchHistory`は`closes`側をループして`timestamps[i]`を参照するため、`len(closes) <= len(timestamps)`が常に成り立ち、index out of rangeは起きない（`FetchLatest`と同じ安全性）。この境界条件は既存の`TestYahooFinanceClient_FetchLatest_TimestampShorterThanClose`で共通ヘルパー`fetchChart`ごと検証済みのため、`FetchHistory`用に同テストを複製しない。

- [ ] **Step 5: テストを実行して成功を確認する**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestYahooFinanceClient -v`
Expected: PASS（既存の`FetchLatest`系テストも含めて全て通ること）

- [ ] **Step 6: 既存モックにFetchHistoryを追加してコンパイルを通す**

`go-api/internal/usecase/monitor_stocks_test.go`の`MockPriceFetcher`定義（`FetchLatest`メソッドの直後）に追記:

```go
func (m *MockPriceFetcher) FetchHistory(code stock.StockCode, days int) ([]stock.Quote, error) {
	args := m.Called(code, days)
	return args.Get(0).([]stock.Quote), args.Error(1)
}
```

`go-api/internal/usecase/monitor_notify_test.go`の`mockNotifyFetcher`定義（`FetchLatest`メソッドの直後）に追記:

```go
func (m *mockNotifyFetcher) FetchHistory(code stock.StockCode, days int) ([]stock.Quote, error) {
	return nil, nil
}
```

- [ ] **Step 7: モジュール全体のビルドとテストを確認する**

Run: `cd go-api && go build ./... && go test -race -short ./...`
Expected: PASS（全パッケージでコンパイルエラー・テスト失敗がないこと）

- [ ] **Step 8: コミット**

```bash
git add go-api/internal/domain/stock/price_fetcher.go \
  go-api/internal/interface/gateway/yahoo_finance_client.go \
  go-api/internal/interface/gateway/yahoo_finance_client_test.go \
  go-api/internal/usecase/monitor_stocks_test.go \
  go-api/internal/usecase/monitor_notify_test.go
git commit -m "feat: add FetchHistory to PriceFetcher and YahooFinanceClient"
```

---

## Task 2: BackfillPriceHistoryUsecaseを新設する

**Files:**
- Create: `go-api/internal/usecase/backfill_price_history.go`
- Test: `go-api/internal/usecase/backfill_price_history_test.go`

**Interfaces:**
- Consumes: `stock.PriceFetcher.FetchHistory(code, days) ([]Quote, error)`（Task 1で追加）、`stock.PriceCache.GetHistory(code, n) ([]Price, error)` / `Push(code, price) error` / `SetLastDate(code, date) error`（既存）。テストではTask 1で拡張済みの`usecase_test`パッケージ共有モック`MockPriceFetcher`・`MockPriceCache`（`monitor_stocks_test.go`定義）を再利用する。
- Produces: `usecase.NewBackfillPriceHistoryUsecase(fetcher stock.PriceFetcher, cache stock.PriceCache) *BackfillPriceHistoryUsecase`、および`(*BackfillPriceHistoryUsecase).Run(code stock.StockCode) error`。Task 3で`ManageWatchlistUsecase`から呼び出す。

- [ ] **Step 1: 失敗するテストを書く**

`go-api/internal/usecase/backfill_price_history_test.go`を新規作成:

```go
package usecase_test

import (
	"errors"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBackfillPriceHistoryUsecase_Run_PushesHistoryWhenCacheEmpty(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	cache := &MockPriceCache{}
	code, _ := stock.NewStockCode("7203")

	quotes := []stock.Quote{
		{Price: 3200.0, Date: "2026-07-06"},
		{Price: 3250.0, Date: "2026-07-07"},
		{Price: 3300.0, Date: "2026-07-08"},
	}
	cache.On("GetHistory", code, 1).Return([]stock.Price{}, nil)
	fetcher.On("FetchHistory", code, 30).Return(quotes, nil)
	cache.On("Push", code, stock.Price(3200.0)).Return(nil)
	cache.On("Push", code, stock.Price(3250.0)).Return(nil)
	cache.On("Push", code, stock.Price(3300.0)).Return(nil)
	cache.On("SetLastDate", code, "2026-07-08").Return(nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
	err := uc.Run(code)

	require.NoError(t, err)
	cache.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestBackfillPriceHistoryUsecase_Run_SkipsWhenHistoryAlreadyExists(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	cache := &MockPriceCache{}
	code, _ := stock.NewStockCode("7203")

	cache.On("GetHistory", code, 1).Return([]stock.Price{3300.0}, nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
	err := uc.Run(code)

	require.NoError(t, err)
	fetcher.AssertNotCalled(t, "FetchHistory", mock.Anything, mock.Anything)
	cache.AssertNotCalled(t, "Push", mock.Anything, mock.Anything)
}

func TestBackfillPriceHistoryUsecase_Run_PropagatesFetchError(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	cache := &MockPriceCache{}
	code, _ := stock.NewStockCode("7203")

	cache.On("GetHistory", code, 1).Return([]stock.Price{}, nil)
	fetcher.On("FetchHistory", code, 30).Return([]stock.Quote{}, errors.New("fetch failed"))

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
	err := uc.Run(code)

	require.Error(t, err)
	cache.AssertNotCalled(t, "Push", mock.Anything, mock.Anything)
}
```

- [ ] **Step 2: テストを実行して失敗を確認する**

Run: `cd go-api && go test ./internal/usecase/... -run TestBackfillPriceHistoryUsecase -v`
Expected: FAIL（`usecase.NewBackfillPriceHistoryUsecase undefined`のコンパイルエラー）

- [ ] **Step 3: 最小実装を書く**

`go-api/internal/usecase/backfill_price_history.go`を新規作成:

```go
package usecase

import (
	"fmt"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

// BackfillPriceHistoryUsecase は新規watchlist追加時に、過去の値動きを
// Redisの価格キャッシュへ一括投入し、異常検知が有効になるまでの
// ウォームアップ期間（historySize件分の日次ポーリング待ち）を短縮する。
type BackfillPriceHistoryUsecase struct {
	fetcher stock.PriceFetcher
	cache   stock.PriceCache
}

func NewBackfillPriceHistoryUsecase(fetcher stock.PriceFetcher, cache stock.PriceCache) *BackfillPriceHistoryUsecase {
	return &BackfillPriceHistoryUsecase{fetcher: fetcher, cache: cache}
}

// Run はcodeの価格履歴が空の場合のみ過去historySize件を取得してキャッシュへ積む。
// 既に履歴が存在する場合（同一銘柄を別ユーザーが既に監視中）は何もしない。
func (u *BackfillPriceHistoryUsecase) Run(code stock.StockCode) error {
	existing, err := u.cache.GetHistory(code, 1)
	if err != nil {
		return fmt.Errorf("check existing history %s: %w", code, err)
	}
	if len(existing) > 0 {
		return nil
	}

	quotes, err := u.fetcher.FetchHistory(code, historySize)
	if err != nil {
		return fmt.Errorf("fetch history %s: %w", code, err)
	}
	for _, q := range quotes {
		if err := u.cache.Push(code, q.Price); err != nil {
			return fmt.Errorf("cache push %s: %w", code, err)
		}
	}
	if len(quotes) == 0 {
		return nil
	}
	if err := u.cache.SetLastDate(code, quotes[len(quotes)-1].Date); err != nil {
		return fmt.Errorf("set last date %s: %w", code, err)
	}
	return nil
}
```

- [ ] **Step 4: テストを実行して成功を確認する**

Run: `cd go-api && go test ./internal/usecase/... -run TestBackfillPriceHistoryUsecase -v`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
git add go-api/internal/usecase/backfill_price_history.go go-api/internal/usecase/backfill_price_history_test.go
git commit -m "feat: add BackfillPriceHistoryUsecase"
```

---

## Task 3: ManageWatchlistUsecase.AddからBackfillPriceHistoryUsecaseを呼び出す

**Files:**
- Modify: `go-api/internal/usecase/manage_watchlist.go`
- Modify: `go-api/internal/usecase/manage_watchlist_test.go`

**Interfaces:**
- Consumes: `usecase.NewBackfillPriceHistoryUsecase`と`(*BackfillPriceHistoryUsecase).Run(code) error`（Task 2で追加）
- Produces: `usecase.NewManageWatchlistUsecase(watchlists watchlist.Repository, backfiller *BackfillPriceHistoryUsecase) *ManageWatchlistUsecase`（コンストラクタのシグネチャ変更）。Task 4で`cmd/api/main.go`から呼び出す。

- [ ] **Step 1: 失敗するテストを書く**

`go-api/internal/usecase/manage_watchlist_test.go`のimport文に`"errors"`を追加する（既存のimportブロックを次のように変更）:

```go
import (
	"context"
	"errors"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)
```

既存の6つのテスト内にある`usecase.NewManageWatchlistUsecase(repo)`を全て`usecase.NewManageWatchlistUsecase(repo, nil)`に置き換える（`repo := new(mockWatchlistRepository)`の直後で呼ばれている箇所、`TestManageWatchlistUsecase_Add_Success`・`_Add_DefaultThreshold`・`_Add_InvalidStockCode`・`_Remove`・`_UpdateThreshold`・`_List`の6箇所）。

ファイル末尾に新しいテストを追記:

```go
func TestManageWatchlistUsecase_Add_TriggersBackfill(t *testing.T) {
	repo := new(mockWatchlistRepository)
	fetcher := &MockPriceFetcher{}
	cache := &MockPriceCache{}
	code, _ := stock.NewStockCode("7203")

	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 3.0}, nil)
	cache.On("GetHistory", code, 1).Return([]stock.Price{}, nil)
	fetcher.On("FetchHistory", code, 30).Return([]stock.Quote{{Price: 3200.0, Date: "2026-07-06"}}, nil)
	cache.On("Push", code, stock.Price(3200.0)).Return(nil)
	cache.On("SetLastDate", code, "2026-07-06").Return(nil)

	backfiller := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
	uc := usecase.NewManageWatchlistUsecase(repo, backfiller)
	_, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	cache.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestManageWatchlistUsecase_Add_SucceedsEvenIfBackfillFails(t *testing.T) {
	repo := new(mockWatchlistRepository)
	fetcher := &MockPriceFetcher{}
	cache := &MockPriceCache{}
	code, _ := stock.NewStockCode("7203")

	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 3.0}, nil)
	cache.On("GetHistory", code, 1).Return([]stock.Price{}, nil)
	fetcher.On("FetchHistory", code, 30).Return([]stock.Quote{}, errors.New("yahoo finance unavailable"))

	backfiller := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
	uc := usecase.NewManageWatchlistUsecase(repo, backfiller)
	result, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	require.Equal(t, "wl-1", result.ID)
}
```

- [ ] **Step 2: テストを実行して失敗を確認する**

Run: `cd go-api && go test ./internal/usecase/... -run TestManageWatchlistUsecase -v`
Expected: FAIL（`NewManageWatchlistUsecase`の引数個数不一致によるコンパイルエラー）

- [ ] **Step 3: ManageWatchlistUsecaseにbackfillerを組み込む**

`go-api/internal/usecase/manage_watchlist.go`を次の内容に置き換える:

```go
package usecase

import (
	"context"
	"errors"
	"log"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

const defaultAlertThreshold = 2.5

var ErrInvalidThreshold = errors.New("alert threshold must be positive")

type ManageWatchlistUsecase struct {
	watchlists watchlist.Repository
	backfiller *BackfillPriceHistoryUsecase
}

func NewManageWatchlistUsecase(watchlists watchlist.Repository, backfiller *BackfillPriceHistoryUsecase) *ManageWatchlistUsecase {
	return &ManageWatchlistUsecase{watchlists: watchlists, backfiller: backfiller}
}

// Add はwatchlistへ銘柄を登録し、backfillerが設定されていれば価格履歴の
// バックフィルを試みる。バックフィル失敗はログのみに留め、登録自体は成功として返す。
func (u *ManageWatchlistUsecase) Add(ctx context.Context, userID, rawStockCode string, threshold float64) (watchlist.Watchlist, error) {
	code, err := stock.NewStockCode(rawStockCode)
	if err != nil {
		return watchlist.Watchlist{}, err
	}
	if threshold == 0 {
		threshold = defaultAlertThreshold
	}
	w, err := u.watchlists.Create(ctx, watchlist.Watchlist{
		UserID:         userID,
		StockCode:      code,
		AlertThreshold: threshold,
	})
	if err != nil {
		return watchlist.Watchlist{}, err
	}
	if u.backfiller != nil {
		if err := u.backfiller.Run(code); err != nil {
			log.Printf("ERROR backfill price history %s: %v", code, err)
		}
	}
	return w, nil
}

func (u *ManageWatchlistUsecase) Remove(ctx context.Context, userID, watchlistID string) error {
	return u.watchlists.Delete(ctx, watchlistID, userID)
}

func (u *ManageWatchlistUsecase) UpdateThreshold(ctx context.Context, userID, watchlistID string, threshold float64) error {
	if threshold <= 0 {
		return ErrInvalidThreshold
	}
	return u.watchlists.UpdateThreshold(ctx, watchlistID, userID, threshold)
}

func (u *ManageWatchlistUsecase) List(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	return u.watchlists.FindByUserID(ctx, userID)
}
```

- [ ] **Step 4: テストを実行して成功を確認する**

Run: `cd go-api && go test ./internal/usecase/... -run TestManageWatchlistUsecase -v`
Expected: PASS（既存6テスト＋新規2テストの計8テスト全て通ること）

- [ ] **Step 5: コミット**

```bash
git add go-api/internal/usecase/manage_watchlist.go go-api/internal/usecase/manage_watchlist_test.go
git commit -m "feat: trigger price history backfill on watchlist add"
```

---

## Task 4: main.goへの配線とドキュメント更新

**Files:**
- Modify: `go-api/cmd/api/main.go`
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: `usecase.NewBackfillPriceHistoryUsecase`（Task 2）、`usecase.NewManageWatchlistUsecase`の新シグネチャ（Task 3）
- Produces: なし（最終配線）

- [ ] **Step 1: main.goでBackfillPriceHistoryUsecaseを構築し、ManageWatchlistUsecaseへ渡す**

`go-api/cmd/api/main.go`の該当箇所を次のように変更する。

変更前（Lines 78-96付近）:
```go
	priceCache := cache.NewRedisPriceCache(redisClient)
	priceFetcher := gateway.NewYahooFinanceClient()
	newsClient := gateway.NewYanoshinTDnetClient()
	pythonEngineClient := gateway.NewPythonEngineClient(pythonEngineURL)
	claudeClient := gateway.NewClaudeClient(anthropicAPIKey, claudeModel)
	slackClient := gateway.NewSlackClient(slackWebhookURL)
	notificationRepo := persistence.NewPgNotificationRepository(pool)
	notifyUsecase := usecase.NewAnalyzeAndNotifyUsecase(newsClient, pythonEngineClient, claudeClient, slackClient, notificationRepo)
	detector := anomaly.NewDetectionService()
	monitor := usecase.NewMonitorUsecase(priceFetcher, priceCache, detector, threshold, notifyUsecase)

	userRepo := persistence.NewPgUserRepository(pool)
	watchlistRepo := persistence.NewPgWatchlistRepository(pool)
	hasher := gateway.NewBcryptHasher()
	tokenService := gateway.NewJWTTokenService(jwtSecret)

	registerUsecase := usecase.NewRegisterUserUsecase(userRepo, hasher)
	loginUsecase := usecase.NewLoginUserUsecase(userRepo, hasher, tokenService)
	watchlistUsecase := usecase.NewManageWatchlistUsecase(watchlistRepo)
```

変更後:
```go
	priceCache := cache.NewRedisPriceCache(redisClient)
	priceFetcher := gateway.NewYahooFinanceClient()
	newsClient := gateway.NewYanoshinTDnetClient()
	pythonEngineClient := gateway.NewPythonEngineClient(pythonEngineURL)
	claudeClient := gateway.NewClaudeClient(anthropicAPIKey, claudeModel)
	slackClient := gateway.NewSlackClient(slackWebhookURL)
	notificationRepo := persistence.NewPgNotificationRepository(pool)
	notifyUsecase := usecase.NewAnalyzeAndNotifyUsecase(newsClient, pythonEngineClient, claudeClient, slackClient, notificationRepo)
	detector := anomaly.NewDetectionService()
	monitor := usecase.NewMonitorUsecase(priceFetcher, priceCache, detector, threshold, notifyUsecase)
	backfillUsecase := usecase.NewBackfillPriceHistoryUsecase(priceFetcher, priceCache)

	userRepo := persistence.NewPgUserRepository(pool)
	watchlistRepo := persistence.NewPgWatchlistRepository(pool)
	hasher := gateway.NewBcryptHasher()
	tokenService := gateway.NewJWTTokenService(jwtSecret)

	registerUsecase := usecase.NewRegisterUserUsecase(userRepo, hasher)
	loginUsecase := usecase.NewLoginUserUsecase(userRepo, hasher, tokenService)
	watchlistUsecase := usecase.NewManageWatchlistUsecase(watchlistRepo, backfillUsecase)
```

- [ ] **Step 2: ビルドを確認する**

Run: `cd go-api && go build ./...`
Expected: エラーなく成功すること

- [ ] **Step 3: CLAUDE.mdへ設計決定を追記する**

`CLAUDE.md`の「## 重要な設計決定」セクションにある株価取得の行の直後に、新しい行を追加する。

変更前:
```
- 株価取得: Go側（`gateway.YahooFinanceClient`）がYahoo Financeの非公式chart API `query1.finance.yahoo.com/v8/finance/chart/{4桁コード}.T` を認証不要で利用（`User-Agent`ヘッダーのみ必要）。非公式APIのためSLA・レート制限の明記なし。J-Quants API（無料プランで約12週間のデータ遅延あり）は2026-09-13に廃止した
```

変更後:
```
- 株価取得: Go側（`gateway.YahooFinanceClient`）がYahoo Financeの非公式chart API `query1.finance.yahoo.com/v8/finance/chart/{4桁コード}.T` を認証不要で利用（`User-Agent`ヘッダーのみ必要）。非公式APIのためSLA・レート制限の明記なし。J-Quants API（無料プランで約12週間のデータ遅延あり）は2026-09-13に廃止した
- watchlist追加時（`ManageWatchlistUsecase.Add`）は`usecase.BackfillPriceHistoryUsecase`が`YahooFinanceClient.FetchHistory`（`range=3mo`）経由で過去の終値を取得しRedisへ一括投入する。対象銘柄に既に価格履歴があればスキップ（冪等）。取得失敗はログのみでwatchlist登録自体は成功させる。異常検知が有効になるまでのウォームアップ期間（`historySize=30`件分）を短縮する目的
```

- [ ] **Step 4: モジュール全体のビルドとテストを最終確認する**

Run: `cd go-api && go build ./... && go test -race -short ./...`
Expected: PASS（全パッケージでコンパイルエラー・テスト失敗がないこと）

- [ ] **Step 5: コミット**

```bash
git add go-api/cmd/api/main.go CLAUDE.md
git commit -m "feat: wire price history backfill into main and document it"
```
