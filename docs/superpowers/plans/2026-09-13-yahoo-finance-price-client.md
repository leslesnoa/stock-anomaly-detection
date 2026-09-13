# Yahoo Finance価格取得クライアント移行 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gateway.JQuantsClient`（無料プランで約12週間のデータ遅延があり異常検知が機能しない）を、認証不要で近リアルタイムのデータが取得できるYahoo Finance非公式chart APIに差し替える。

**Architecture:** 既存の`stock.PriceFetcher`インターフェース（`FetchLatest(code stock.StockCode) (stock.Quote, error)`）はそのまま維持し、実装だけを差し替える。新規`gateway.YahooFinanceClient`を追加し、`GET https://query1.finance.yahoo.com/v8/finance/chart/{4桁コード}.T?range=5d&interval=1d`を叩いて直近の日次終値配列を取得、末尾から走査して最初に見つかった非null終値を「最新の確定した取引日」として`stock.Quote{Price, Date}`にマッピングする。認証不要（`User-Agent`ヘッダーのみ必要）。「Go=外部I/O集約、Python=純粋計算」という既存の責務境界を維持するため、Python側やyfinanceライブラリ、Vercel Serverless Functionは使わない。`gateway.JQuantsClient`と`JQUANTS_API_KEY`は完全に削除する（併存させない）。

**Tech Stack:** Go標準ライブラリ（`net/http` + `encoding/json` + `time`）のみ、追加ライブラリ不要。`net/http/httptest`によるモックテスト、testify（assert/require）。

**Spec:** 別途designドキュメントは作成しない軽量タスク。本プランの背景・調査結果・確定した設計判断は以下の通り（/grill-meセッションで確認済み）:
- 2026-09-13、実際にYahoo Financeのchart APIを叩いて動作確認済み: `7203.T`で直近2026-09-11（金曜、直近取引日）のデータを取得できた。J-Quants無料プランの`2026-06-19`（約12週間遅延）に対し明確な改善
- 存在しない銘柄コード（例: `0000.T`）はHTTP 404を返す。有効な銘柄コードはHTTP 200
- レスポンス形式: `{"chart":{"result":[{"timestamp":[...],"indicators":{"quote":[{"close":[...]}]}}]}}`。`close`配列は該当日のデータが未確定/休場等でnullになりうる
- バックフィル（新規watchlist追加時に過去30日分を一括取得してウォームアップ期間を短縮する機能）は明示的にスコープ外。フォローアップとしてmemoryに記録する（本プランのタスクには含まない）
- リトライは実装しない（既存`JQuantsClient`と同じ「リトライなし・エラーは翌日まで待つ」を踏襲）
- Yahoo Finance非公式APIのSLA・レート制限リスクはCLAUDE.mdに注記し、watchlist規模拡大時のリスクは別途memoryにフォローアップとして記録する（本プランのタスクには含まない）

## Global Constraints

- `stock.PriceFetcher`インターフェースのシグネチャを変更しない
- URLは動作確認済みの`https://query1.finance.yahoo.com/v8/finance/chart/{4桁コード}.T?range=5d&interval=1d`のみ使う
- リクエストヘッダーに`User-Agent: Mozilla/5.0`を設定する（未設定だとYahoo側にブロックされる可能性がある）
- `close`配列は末尾から走査し、最初に見つかった非null値を採用する（休場日・未確定当日のnullをスキップし、直近の確定済み取引日を選ぶ）
- テストは`httptest.NewServer`でモックし、実API呼び出しをしない
- `go test -race -short ./...`で確認する
- `gateway.JQuantsClient`・`gateway.JQuantsClientWithBaseURL`・`JQUANTS_API_KEY`は完全に削除し、フォールバックとして残さない

---

### Task 1: YahooFinanceClientの実装とテスト

**Files:**
- Create: `go-api/internal/interface/gateway/yahoo_finance_client.go`
- Create: `go-api/internal/interface/gateway/yahoo_finance_client_test.go`

**Interfaces:**
- Consumes: `stock.Quote{Price stock.Price; Date string}`、`stock.PriceFetcher`インターフェース（`go-api/internal/domain/stock/price_fetcher.go`）、`stock.StockCode`（`.String()`で4桁文字列を取得）
- Produces: `gateway.NewYahooFinanceClient() *YahooFinanceClient`（本番用コンストラクタ、引数なし）、`gateway.NewYahooFinanceClientWithBaseURL(baseURL string) *YahooFinanceClient`（テスト用）、`(*YahooFinanceClient).FetchLatest(code stock.StockCode) (stock.Quote, error)` — Task 2のmain.go配線で使用

- [ ] **Step 1: 失敗するテストを書く**

`go-api/internal/interface/gateway/yahoo_finance_client_test.go` を新規作成:

```go
package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYahooFinanceClient_FetchLatest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Mozilla/5.0", r.Header.Get("User-Agent"))
		assert.Equal(t, "5d", r.URL.Query().Get("range"))
		assert.Equal(t, "1d", r.URL.Query().Get("interval"))

		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						// 2026-07-06, 2026-07-07 (JST 15:00)
						"timestamp": []int64{1783317600, 1783404000},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3200.0, 3250.0}},
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

	quote, err := client.FetchLatest(code)
	require.NoError(t, err)
	assert.Equal(t, stock.Price(3250.0), quote.Price)
	assert.Equal(t, "2026-07-07", quote.Date)
}

func TestYahooFinanceClient_FetchLatest_SkipsTrailingNullClose(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						// 2026-07-06, 2026-07-07, 2026-07-08（当日分は未確定でnull）
						"timestamp": []int64{1783317600, 1783404000, 1783490400},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3200.0, 3250.0, nil}},
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

	quote, err := client.FetchLatest(code)
	require.NoError(t, err)
	assert.Equal(t, stock.Price(3250.0), quote.Price)
	assert.Equal(t, "2026-07-07", quote.Date)
}

func TestYahooFinanceClient_FetchLatest_NoData(t *testing.T) {
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
	_, err := client.FetchLatest(code)
	require.Error(t, err)
}

func TestYahooFinanceClient_FetchLatest_ErrorStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/0000.T", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchLatest(code)
	require.Error(t, err)
}
```

- [ ] **Step 2: テストを実行して失敗を確認する**

Run: `cd go-api && go test -race -short ./internal/interface/gateway/... -run TestYahooFinanceClient -v`
Expected: FAIL（`gateway.NewYahooFinanceClientWithBaseURL`が未定義のためコンパイルエラー）

- [ ] **Step 3: 最小実装を書く**

`go-api/internal/interface/gateway/yahoo_finance_client.go` を新規作成:

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
	url := fmt.Sprintf("%s/v8/finance/chart/%s.T?range=5d&interval=1d", c.baseURL, code)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return stock.Quote{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return stock.Quote{}, fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return stock.Quote{}, fmt.Errorf("yahoo finance api returned status %d", resp.StatusCode)
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
		return stock.Quote{}, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Chart.Result) == 0 || len(result.Chart.Result[0].Indicators.Quote) == 0 {
		return stock.Quote{}, fmt.Errorf("no quote data for %s", code)
	}

	timestamps := result.Chart.Result[0].Timestamp
	closes := result.Chart.Result[0].Indicators.Quote[0].Close
	for i := len(closes) - 1; i >= 0; i-- {
		if closes[i] != nil {
			date := time.Unix(timestamps[i], 0).In(yahooJST).Format("2006-01-02")
			return stock.Quote{Price: stock.Price(*closes[i]), Date: date}, nil
		}
	}
	return stock.Quote{}, fmt.Errorf("no quote data for %s", code)
}
```

- [ ] **Step 4: テストを実行して成功を確認する**

Run: `cd go-api && go test -race -short ./internal/interface/gateway/... -run TestYahooFinanceClient -v`
Expected: PASS（4テストすべて）

- [ ] **Step 5: コミット**

```bash
git add go-api/internal/interface/gateway/yahoo_finance_client.go go-api/internal/interface/gateway/yahoo_finance_client_test.go
git commit -m "feat: add YahooFinanceClient as stock.PriceFetcher implementation"
```

---

### Task 2: JQuantsClientの削除とmain.goの配線差し替え

**Files:**
- Delete: `go-api/internal/interface/gateway/jquants_client.go`
- Delete: `go-api/internal/interface/gateway/jquants_client_test.go`
- Modify: `go-api/cmd/api/main.go:31`（`jQuantsAPIKey := mustEnv("JQUANTS_API_KEY")`の行を削除）
- Modify: `go-api/cmd/api/main.go:80`（`priceFetcher := gateway.NewJQuantsClient(jQuantsAPIKey)` → `priceFetcher := gateway.NewYahooFinanceClient()`）

**Interfaces:**
- Consumes: Task 1で作成した`gateway.NewYahooFinanceClient() *YahooFinanceClient`（引数なし）
- Produces: なし（末端の配線変更）

- [ ] **Step 1: JQuantsClientとそのテストを削除する**

```bash
git rm go-api/internal/interface/gateway/jquants_client.go go-api/internal/interface/gateway/jquants_client_test.go
```

- [ ] **Step 2: main.goを編集する**

`go-api/cmd/api/main.go`の該当行を編集:

削除（31行目）:
```go
	jQuantsAPIKey := mustEnv("JQUANTS_API_KEY")
```

変更前（80行目）:
```go
	priceFetcher := gateway.NewJQuantsClient(jQuantsAPIKey)
```

変更後:
```go
	priceFetcher := gateway.NewYahooFinanceClient()
```

- [ ] **Step 3: ビルドと全体テストを実行して確認する**

Run: `cd go-api && go build ./... && go test -race -short ./...`
Expected: ビルド成功、全テストPASS（`jquants_client_test.go`削除により`TestJQuantsClient_*`は消え、`TestYahooFinanceClient_*`のみが残る）

- [ ] **Step 4: コミット**

```bash
git add go-api/cmd/api/main.go
git commit -m "refactor: replace JQuantsClient with YahooFinanceClient in main wiring"
```

---

### Task 3: CLAUDE.mdのドキュメント更新

**Files:**
- Modify: `CLAUDE.md`（重要な設計決定・テスト方針・環境変数リスト）

**Interfaces:**
- Consumes: なし
- Produces: なし（ドキュメントのみ）

- [ ] **Step 1: J-Quants関連の設計決定行をYahoo Financeの記述に置き換える**

変更前:
```
- J-Quants API V2: 認証は `x-api-key` ヘッダー、エンドポイント `/v2/equities/bars/daily`、レスポンス `data[].C`（終値）
- J-Quants APIコード: 4桁→5桁（末尾0追加）
```

変更後:
```
- 株価取得: Go側（`gateway.YahooFinanceClient`）がYahoo Financeの非公式chart API `query1.finance.yahoo.com/v8/finance/chart/{4桁コード}.T` を認証不要で利用（`User-Agent`ヘッダーのみ必要）。非公式APIのためSLA・レート制限の明記なし。J-Quants API（無料プランで約12週間のデータ遅延あり）は2026-09-13に廃止した
```

- [ ] **Step 2: テスト方針のJ-Quants言及をYahoo Financeに更新する**

変更前:
```
- J-Quantsクライアント: `httptest.NewServer`でモック（実API呼び出しなし）
```

変更後:
```
- Yahoo Financeクライアント: `httptest.NewServer`でモック（実API呼び出しなし）
```

- [ ] **Step 3: 環境変数（本番）リストから`JQUANTS_API_KEY`を削除する**

変更前:
```
## 環境変数（本番）
DATABASE_URL, REDIS_URL, JQUANTS_API_KEY, ANOMALY_THRESHOLD（デフォルト2.5）, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）, JWT_SECRET, PORT（デフォルト8080）
```

変更後:
```
## 環境変数（本番）
DATABASE_URL, REDIS_URL, ANOMALY_THRESHOLD（デフォルト2.5）, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）, JWT_SECRET, PORT（デフォルト8080）
```

- [ ] **Step 4: コミット**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for YahooFinanceClient price source"
```

---

## Self-Review メモ

- **Spec coverage:** /grill-meセッションで確定した設計判断（Go実装・yfinance不使用・J-Quants完全廃止・バックフィルスコープ外・リトライなし・CLAUDE.mdへの注記）を全てTask 1〜3でカバーしている。バックフィル機能とwatchlist規模拡大時のレート制限リスクは、合意通りコードタスクに含めず、実装完了後にmemoryへフォローアップとして記録する（プラン外の作業）。
- **Placeholder scan:** 全ステップに実コード・実コマンドを記載済み。TBD等なし。
- **Type consistency:** `FetchLatest(code stock.StockCode) (stock.Quote, error)`のシグネチャはTask 1〜2で一貫。コンストラクタ名`NewYahooFinanceClient`/`NewYahooFinanceClientWithBaseURL`もTask間で一致。
- **既存パターンとの整合性:** `gateway.YanoshinTDnetClient`（前回のFinnhub→TDnet移行）と同じ構造（2種のコンストラクタ、`httptest.NewServer`モック、完全削除方針、CLAUDE.md注記）を踏襲している。
