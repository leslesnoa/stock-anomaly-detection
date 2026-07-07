# J-Quants API V1 → V2 Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** J-Quants API が V1 を廃止し V2 に移行したため、認証方式・エンドポイント・レスポンス形式をすべて V2 仕様に更新する。

**Architecture:** V1 は email+password → refreshToken → idToken の 2 段階認証だったが、V2 はダッシュボードで発行した API キーを `x-api-key` ヘッダーに付けるだけ。エンドポイントは `/v1/prices/daily_quotes` → `/v2/equities/bars/daily`、レスポンスは `daily_quotes[].Close` → `data[].C` に変わる。

**Tech Stack:** Go 1.22, net/http, httptest

## Global Constraints

- ベースURL: `https://api.jquants.com`（変更なし）
- 株価エンドポイント: `GET /v2/equities/bars/daily?code=<5桁コード>`
- 認証ヘッダー: `x-api-key: <APIキー>`（`Authorization: Bearer` は廃止）
- レスポンスの株価フィールド: `"data"` キー配列、終値は `"C"`
- 銘柄コードは引き続き 4 桁 → 末尾 0 付加で 5 桁（例: `7203` → `72030`）
- 環境変数: `JQUANTS_EMAIL` / `JQUANTS_PASSWORD` を廃止し `JQUANTS_API_KEY` を使用

---

### Task 1: JQuantsClient を V2 に更新する

**Files:**
- Modify: `go-api/internal/interface/gateway/jquants_client.go`
- Modify: `go-api/internal/interface/gateway/jquants_client_test.go`

**Interfaces:**
- Produces: `NewJQuantsClient(apiKey string) *JQuantsClient`
- Produces: `NewJQuantsClientWithBaseURL(apiKey, baseURL string) *JQuantsClient`
- Produces: `(*JQuantsClient).FetchLatest(code stock.StockCode) (stock.Price, error)` — シグネチャ変更なし

- [ ] **Step 1: テストを V2 形式に書き換える（先に壊れることを確認するため）**

`go-api/internal/interface/gateway/jquants_client_test.go` を以下に全置換する:

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

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func TestJQuantsClient_FetchLatest(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v2/equities/bars/daily", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "72030", r.URL.Query().Get("code"))
		writeJSON(w, map[string]interface{}{
			"data": []map[string]interface{}{
				{"C": 3250.0},
			},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewJQuantsClientWithBaseURL("test-api-key", srv.URL)
	code, _ := stock.NewStockCode("7203")

	price, err := client.FetchLatest(code)
	require.NoError(t, err)
	assert.Equal(t, stock.Price(3250.0), price)
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

- [ ] **Step 2: テストが失敗することを確認する**

```bash
cd go-api && go test -race -short ./internal/interface/gateway/...
```

期待: FAIL（`NewJQuantsClientWithBaseURL` のシグネチャが合わない）

- [ ] **Step 3: `jquants_client.go` を V2 実装に全置換する**

```go
package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type JQuantsClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewJQuantsClient(apiKey string) *JQuantsClient {
	return NewJQuantsClientWithBaseURL(apiKey, "https://api.jquants.com")
}

func NewJQuantsClientWithBaseURL(apiKey, baseURL string) *JQuantsClient {
	return &JQuantsClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *JQuantsClient) FetchLatest(code stock.StockCode) (stock.Price, error) {
	// J-Quantsは4桁コードに末尾0を付けた5桁で検索する
	url := fmt.Sprintf("%s/v2/equities/bars/daily?code=%s0", c.baseURL, code)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("jquants api returned status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			C float64 `json:"C"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Data) == 0 {
		return 0, fmt.Errorf("no quote data for %s", code)
	}
	return stock.Price(result.Data[0].C), nil
}
```

- [ ] **Step 4: テストが通ることを確認する**

```bash
cd go-api && go test -race -short ./internal/interface/gateway/...
```

期待:
```
ok  github.com/stock-anomaly-detection/go-api/internal/interface/gateway
```

- [ ] **Step 5: 全テストが通ることを確認する**

```bash
cd go-api && go test -race -short ./...
```

期待: すべて `ok` または `[no test files]`

- [ ] **Step 6: コミットする**

```bash
git add go-api/internal/interface/gateway/jquants_client.go \
        go-api/internal/interface/gateway/jquants_client_test.go
git commit -m "feat: migrate JQuantsClient to V2 API (x-api-key auth, /v2/equities/bars/daily)"
```

---

### Task 2: main.go・.env.example・CLAUDE.md を V2 対応に更新する

**Files:**
- Modify: `go-api/cmd/api/main.go`
- Modify: `.env.example`
- Modify: `CLAUDE.md`

**Interfaces:**
- Consumes: `gateway.NewJQuantsClient(apiKey string)` (Task 1 で変更済み)

- [ ] **Step 1: `main.go` の env var と JQuantsClient 生成を変更する**

`go-api/cmd/api/main.go` の以下の 3 行を変更する:

変更前:
```go
jQuantsEmail := mustEnv("JQUANTS_EMAIL")
jQuantsPassword := mustEnv("JQUANTS_PASSWORD")
```

変更後:
```go
jQuantsAPIKey := mustEnv("JQUANTS_API_KEY")
```

変更前:
```go
priceFetcher := gateway.NewJQuantsClient(jQuantsEmail, jQuantsPassword)
```

変更後:
```go
priceFetcher := gateway.NewJQuantsClient(jQuantsAPIKey)
```

- [ ] **Step 2: ビルドが通ることを確認する**

```bash
cd go-api && go build ./...
```

期待: エラーなし（exit 0）

- [ ] **Step 3: `.env.example` を更新する**

ファイル全体を以下に置換する:

```
DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly?sslmode=disable
REDIS_URL=redis://localhost:6379
JQUANTS_API_KEY=your-jquants-api-key
STOCK_CODES=7203,6758,9984
ANOMALY_THRESHOLD=2.5
```

※ API キーは J-Quants ダッシュボード（https://jpx-jquants.com/）→ 「APIキー」メニューから発行・確認できる。

- [ ] **Step 4: `CLAUDE.md` の J-Quants 関連記述を更新する**

`CLAUDE.md` の「## 環境変数（本番）」セクションを以下に変更する:

変更前:
```
DATABASE_URL, REDIS_URL, JQUANTS_EMAIL, JQUANTS_PASSWORD, STOCK_CODES（カンマ区切り4桁コード）, ANOMALY_THRESHOLD（デフォルト2.5）
```

変更後:
```
DATABASE_URL, REDIS_URL, JQUANTS_API_KEY, STOCK_CODES（カンマ区切り4桁コード）, ANOMALY_THRESHOLD（デフォルト2.5）
```

また「## 重要な設計決定」の J-Quants API 行を以下に変更する:

変更前:
```
- J-Quants APIコード: 4桁→5桁（末尾0追加）、JSON key: `"refreshToken"`（camelCase）
```

変更後:
```
- J-Quants API V2: 認証は `x-api-key` ヘッダー、エンドポイント `/v2/equities/bars/daily`、レスポンス `data[].C`（終値）
- J-Quants APIコード: 4桁→5桁（末尾0追加）
```

- [ ] **Step 5: 全テストとビルドが通ることを最終確認する**

```bash
cd go-api && go test -race -short ./... && go build ./...
```

期待: すべて `ok` / エラーなし

- [ ] **Step 6: コミットする**

```bash
git add go-api/cmd/api/main.go .env.example CLAUDE.md
git commit -m "feat: update env vars and docs for J-Quants V2 (JQUANTS_API_KEY)"
```
