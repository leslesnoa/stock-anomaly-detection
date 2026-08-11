# Phase 3: Claude API連携・Slack通知 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 異常検知された銘柄について、Finnhubニュース取得・Pythonエンジンでのテクニカル指標計算・Claude APIでのAIレポート生成・Slack通知・通知履歴のPostgreSQL保存までを行うパイプラインをGo APIに実装する。

**Architecture:** `MonitorUsecase`が異常を検知したら新設の`AnalyzeAndNotifyUsecase`を呼び出す。`AnalyzeAndNotifyUsecase`はニュース取得（Finnhub gateway）・テクニカル指標計算（Python engine gateway）・AIレポート生成（Claude API gateway）・Slack通知（Slack gateway）を順に実行し、結果を`notifications`テーブル（Phase1で作成済み）に保存する。各外部依存はdomain層に定義したインターフェース経由でusecaseから参照する既存パターン（`stock.PriceFetcher`/`stock.PriceCache`と同様）を踏襲する。

**Tech Stack:** Go 1.24, `github.com/anthropics/anthropic-sdk-go`（Claude API公式Go SDK）, 標準ライブラリ`net/http`（Finnhub・Slack・Python engine呼び出し）, `github.com/jackc/pgx/v5`（通知履歴永続化）

## Global Constraints

- Goモジュール: `github.com/stock-anomaly-detection/go-api`（`go-api/`内で操作、go 1.24。Task 4でClaude公式Go SDK追加に伴いgo.mod・CIともに1.22→1.24へ更新済み）
- `go test -race -short ./...` は外部サービス不要（単体テスト。gatewayは`httptest`でモック）
- `go test -race ./...` は `DATABASE_URL` + `REDIS_URL` が必要（統合テスト）
- Clean Architecture依存ルール: `domain`は外部依存ゼロ、`usecase`は`domain`のみに依存、`interface`/`infrastructure`は`usecase`と`domain`に依存
- リポジトリ/フェッチャー等の外部依存インターフェースは`domain`パッケージ内に定義する
- Claude APIモデルは`claude-opus-5`をデフォルトとする（ユーザー明示指定なし）
- エラーハンドリング（設計書 §8 準拠）:
  - Pythonエンジンタイムアウト（30秒）→ エラーログ記録・Slack通知はスキップ
  - Slack Webhook失敗 → 最大3回リトライ・失敗時は`slack_sent=false`で`notifications`に保存
  - Claude API失敗 → リトライ1回・失敗時はテクニカル指標のみのSlack通知にフォールバック
- コミットメッセージ: Conventional Commits（`feat:`, `fix:`, `test:`など）

---

## ファイル構成（全タスク完了後）

```
go-api/
├── internal/
│   ├── domain/
│   │   ├── notification/
│   │   │   ├── entity.go         # Notification エンティティ（新規）
│   │   │   └── repository.go     # Repository インターフェース（新規）
│   │   ├── news/
│   │   │   └── fetcher.go        # Item, Fetcher インターフェース（新規）
│   │   ├── analysis/
│   │   │   └── analyzer.go       # Indicators, MACD, Bollinger, Analyzer インターフェース（新規）
│   │   ├── ai/
│   │   │   └── report_generator.go  # ReportGenerator インターフェース（新規）
│   │   └── notifier/
│   │       └── notifier.go       # Notifier インターフェース（新規）
│   ├── usecase/
│   │   ├── monitor_stocks.go     # 異常検知時にAnalyzeAndNotifyUsecaseを呼び出すよう修正
│   │   ├── monitor_stocks_test.go   # NewMonitorUsecase呼び出しを新シグネチャに修正
│   │   └── analyze_and_notify.go # オーケストレーションユースケース（新規）
│   ├── interface/gateway/
│   │   ├── news_client.go        # FinnhubClient（新規）
│   │   ├── python_engine_client.go  # PythonEngineClient（新規）
│   │   ├── claude_client.go      # ClaudeClient（新規）
│   │   └── slack_client.go       # SlackClient（新規）
│   └── infrastructure/persistence/
│       └── notification_repository.go  # PgNotificationRepository（新規）
├── go.mod                        # anthropic-sdk-go 依存追加
└── cmd/api/main.go               # DI配線・新規環境変数
```

---

### Task 1: Notification ドメイン + Postgres永続化

**Files:**
- Create: `go-api/internal/domain/notification/entity.go`
- Create: `go-api/internal/domain/notification/repository.go`
- Create: `go-api/internal/infrastructure/persistence/notification_repository.go`
- Test: `go-api/internal/infrastructure/persistence/notification_repository_test.go`

**Interfaces:**
- Produces: `notification.Notification{UserID *string, StockCode string, AnomalyScore float64, AIReport string, TechnicalIndicators string, SlackSent bool, NotifiedAt time.Time}`、`notification.Repository`インターフェース（`Save(ctx context.Context, n Notification) error`）、`persistence.NewPgNotificationRepository(conn *pgx.Conn) *PgNotificationRepository`

- [ ] **Step 1: ドメインエンティティとリポジトリインターフェースを作成**

`go-api/internal/domain/notification/entity.go`:
```go
package notification

import "time"

type Notification struct {
	UserID              *string
	StockCode           string
	AnomalyScore        float64
	AIReport            string
	TechnicalIndicators string
	SlackSent           bool
	NotifiedAt          time.Time
}
```

`go-api/internal/domain/notification/repository.go`:
```go
package notification

import "context"

type Repository interface {
	Save(ctx context.Context, n Notification) error
}
```

- [ ] **Step 2: 失敗する統合テストを書く**

`go-api/internal/infrastructure/persistence/notification_repository_test.go`:
```go
package persistence_test

import (
	"context"
	"os"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stretchr/testify/require"
)

func TestPgNotificationRepository_Save(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := persistence.Connect(ctx, databaseURL)
	require.NoError(t, err)
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, "TRUNCATE notifications CASCADE")
	require.NoError(t, err)

	repo := persistence.NewPgNotificationRepository(conn)
	n := notification.Notification{
		StockCode:           "7203",
		AnomalyScore:        3.2,
		AIReport:             "テストレポート",
		TechnicalIndicators: `{"rsi":65.5}`,
		SlackSent:           true,
	}
	err = repo.Save(ctx, n)
	require.NoError(t, err)

	var count int
	err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM notifications WHERE stock_code = $1", "7203").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}
```

- [ ] **Step 3: テストを実行して失敗を確認**

Run: `cd go-api && DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test ./internal/infrastructure/persistence/... -run TestPgNotificationRepository_Save -v`
Expected: FAIL（`persistence.NewPgNotificationRepository` が未定義）

- [ ] **Step 4: Postgres実装を書く**

`go-api/internal/infrastructure/persistence/notification_repository.go`:
```go
package persistence

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
)

type PgNotificationRepository struct {
	conn *pgx.Conn
}

func NewPgNotificationRepository(conn *pgx.Conn) *PgNotificationRepository {
	return &PgNotificationRepository{conn: conn}
}

func (r *PgNotificationRepository) Save(ctx context.Context, n notification.Notification) error {
	_, err := r.conn.Exec(ctx,
		`INSERT INTO notifications (user_id, stock_code, anomaly_score, ai_report, technical_indicators, slack_sent)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		n.UserID, n.StockCode, n.AnomalyScore, n.AIReport, n.TechnicalIndicators, n.SlackSent)
	return err
}
```

- [ ] **Step 5: テストを実行して成功を確認**

Run: `cd go-api && DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test ./internal/infrastructure/persistence/... -run TestPgNotificationRepository_Save -v`
Expected: PASS（DB未起動環境では`t.Skip`でスキップされることを確認: `go test -short ./internal/infrastructure/persistence/...`もPASSすること）

- [ ] **Step 6: コミット**

```bash
cd go-api
git add internal/domain/notification internal/infrastructure/persistence/notification_repository.go internal/infrastructure/persistence/notification_repository_test.go
git commit -m "feat: add notification domain and Postgres repository"
```

---

### Task 2: Finnhubニュース取得

**Files:**
- Create: `go-api/internal/domain/news/fetcher.go`
- Create: `go-api/internal/interface/gateway/news_client.go`
- Test: `go-api/internal/interface/gateway/news_client_test.go`

**Interfaces:**
- Consumes: `stock.StockCode`（`go-api/internal/domain/stock/value_object.go`で定義済み）
- Produces: `news.Item{Headline string, Summary string}`、`news.Fetcher`インターフェース（`FetchRecent(code stock.StockCode) ([]Item, error)`）、`gateway.NewFinnhubClient(apiKey string) *FinnhubClient`、`gateway.NewFinnhubClientWithBaseURL(apiKey, baseURL string) *FinnhubClient`（テスト用）

- [ ] **Step 1: ドメインインターフェースを作成**

`go-api/internal/domain/news/fetcher.go`:
```go
package news

import "github.com/stock-anomaly-detection/go-api/internal/domain/stock"

type Item struct {
	Headline string
	Summary  string
}

type Fetcher interface {
	FetchRecent(code stock.StockCode) ([]Item, error)
}
```

- [ ] **Step 2: 失敗するテストを書く**

`go-api/internal/interface/gateway/news_client_test.go`:
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

func TestFinnhubClient_FetchRecent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /company-news", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("X-Finnhub-Token"))
		assert.Equal(t, "7203.T", r.URL.Query().Get("symbol"))
		assert.NotEmpty(t, r.URL.Query().Get("from"))
		assert.NotEmpty(t, r.URL.Query().Get("to"))

		items := []map[string]string{
			{"headline": "トヨタ、新型EV発表", "summary": "トヨタ自動車が新型EVを発表した"},
			{"headline": "業績好調", "summary": "四半期決算は市場予想を上回った"},
		}
		require.NoError(t, json.NewEncoder(w).Encode(items))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewFinnhubClientWithBaseURL("test-key", srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchRecent(code)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "トヨタ、新型EV発表", got[0].Headline)
	assert.Equal(t, "トヨタ自動車が新型EVを発表した", got[0].Summary)
}

func TestFinnhubClient_FetchRecent_LimitsToTop5(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /company-news", func(w http.ResponseWriter, r *http.Request) {
		items := make([]map[string]string, 10)
		for i := range items {
			items[i] = map[string]string{"headline": "h", "summary": "s"}
		}
		require.NoError(t, json.NewEncoder(w).Encode(items))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewFinnhubClientWithBaseURL("test-key", srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchRecent(code)
	require.NoError(t, err)
	assert.Len(t, got, 5)
}
```

- [ ] **Step 3: テストを実行して失敗を確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestFinnhubClient -v`
Expected: FAIL（`gateway.NewFinnhubClientWithBaseURL`が未定義）

- [ ] **Step 4: Finnhubクライアントを実装**

`go-api/internal/interface/gateway/news_client.go`:
```go
package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

const newsLookbackDays = 7
const newsLimit = 5

type FinnhubClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewFinnhubClient(apiKey string) *FinnhubClient {
	return NewFinnhubClientWithBaseURL(apiKey, "https://finnhub.io/api/v1")
}

func NewFinnhubClientWithBaseURL(apiKey, baseURL string) *FinnhubClient {
	return &FinnhubClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *FinnhubClient) FetchRecent(code stock.StockCode) ([]news.Item, error) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -newsLookbackDays).Format("2006-01-02")
	to := now.Format("2006-01-02")
	symbol := fmt.Sprintf("%s.T", code)

	url := fmt.Sprintf("%s/company-news?symbol=%s&from=%s&to=%s", c.baseURL, symbol, from, to)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Finnhub-Token", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch news: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("finnhub api returned status %d", resp.StatusCode)
	}

	var raw []struct {
		Headline string `json:"headline"`
		Summary  string `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	items := make([]news.Item, 0, newsLimit)
	for i, r := range raw {
		if i >= newsLimit {
			break
		}
		items = append(items, news.Item{Headline: r.Headline, Summary: r.Summary})
	}
	return items, nil
}
```

- [ ] **Step 5: テストを実行して成功を確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestFinnhubClient -v`
Expected: PASS

- [ ] **Step 6: コミット**

```bash
cd go-api
git add internal/domain/news internal/interface/gateway/news_client.go internal/interface/gateway/news_client_test.go
git commit -m "feat: add Finnhub news client"
```

---

### Task 3: Python分析エンジンクライアント

**Files:**
- Create: `go-api/internal/domain/analysis/analyzer.go`
- Create: `go-api/internal/interface/gateway/python_engine_client.go`
- Test: `go-api/internal/interface/gateway/python_engine_client_test.go`

**Interfaces:**
- Consumes: `stock.StockCode`
- Produces: `analysis.Indicators{RSI *float64, MACD MACD, Bollinger Bollinger}`（`json`タグ付き: `rsi`/`macd`/`bollinger`）、`analysis.Analyzer`インターフェース（`Analyze(code stock.StockCode, zScore, currentPrice float64, prices []float64) (Indicators, error)`）、`gateway.NewPythonEngineClient(baseURL string) *PythonEngineClient`

- [ ] **Step 1: ドメインインターフェースを作成**

`go-api/internal/domain/analysis/analyzer.go`:
```go
package analysis

import "github.com/stock-anomaly-detection/go-api/internal/domain/stock"

type MACD struct {
	Line      *float64 `json:"line"`
	Signal    *float64 `json:"signal"`
	Histogram *float64 `json:"histogram"`
}

type Bollinger struct {
	Upper  *float64 `json:"upper"`
	Middle *float64 `json:"middle"`
	Lower  *float64 `json:"lower"`
}

type Indicators struct {
	RSI       *float64  `json:"rsi"`
	MACD      MACD      `json:"macd"`
	Bollinger Bollinger `json:"bollinger"`
}

type Analyzer interface {
	Analyze(code stock.StockCode, zScore, currentPrice float64, prices []float64) (Indicators, error)
}
```

- [ ] **Step 2: 失敗するテストを書く**

`go-api/internal/interface/gateway/python_engine_client_test.go`:
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

func TestPythonEngineClient_Analyze(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /analyze", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "7203", body["stock_code"])
		assert.Equal(t, 3.2, body["z_score"])
		assert.Equal(t, 3250.0, body["current_price"])

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"stock_code": "7203",
			"indicators": map[string]interface{}{
				"rsi": 65.3,
				"macd": map[string]interface{}{
					"line": 1.2, "signal": 0.9, "histogram": 0.3,
				},
				"bollinger": map[string]interface{}{
					"upper": 3300.0, "middle": 3200.0, "lower": 3100.0,
				},
			},
		}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	prices := make([]float64, 30)
	for i := range prices {
		prices[i] = 3000.0 + float64(i)
	}

	got, err := client.Analyze(code, 3.2, 3250.0, prices)
	require.NoError(t, err)
	require.NotNil(t, got.RSI)
	assert.InDelta(t, 65.3, *got.RSI, 0.001)
	require.NotNil(t, got.MACD.Line)
	assert.InDelta(t, 1.2, *got.MACD.Line, 0.001)
	require.NotNil(t, got.Bollinger.Upper)
	assert.InDelta(t, 3300.0, *got.Bollinger.Upper, 0.001)
}

func TestPythonEngineClient_Analyze_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /analyze", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	_, err := client.Analyze(code, 3.2, 3250.0, make([]float64, 30))
	require.Error(t, err)
}
```

- [ ] **Step 3: テストを実行して失敗を確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestPythonEngineClient -v`
Expected: FAIL（`gateway.NewPythonEngineClient`が未定義）

- [ ] **Step 4: Pythonエンジンクライアントを実装**

`go-api/internal/interface/gateway/python_engine_client.go`:
```go
package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type PythonEngineClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewPythonEngineClient(baseURL string) *PythonEngineClient {
	return &PythonEngineClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type analyzeRequest struct {
	StockCode    string    `json:"stock_code"`
	ZScore       float64   `json:"z_score"`
	CurrentPrice float64   `json:"current_price"`
	Prices       []float64 `json:"prices"`
}

type analyzeResponse struct {
	StockCode  string `json:"stock_code"`
	Indicators struct {
		RSI  *float64 `json:"rsi"`
		MACD struct {
			Line      *float64 `json:"line"`
			Signal    *float64 `json:"signal"`
			Histogram *float64 `json:"histogram"`
		} `json:"macd"`
		Bollinger struct {
			Upper  *float64 `json:"upper"`
			Middle *float64 `json:"middle"`
			Lower  *float64 `json:"lower"`
		} `json:"bollinger"`
	} `json:"indicators"`
}

func (c *PythonEngineClient) Analyze(code stock.StockCode, zScore, currentPrice float64, prices []float64) (analysis.Indicators, error) {
	reqBody, err := json.Marshal(analyzeRequest{
		StockCode:    code.String(),
		ZScore:       zScore,
		CurrentPrice: currentPrice,
		Prices:       prices,
	})
	if err != nil {
		return analysis.Indicators{}, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/analyze", bytes.NewReader(reqBody))
	if err != nil {
		return analysis.Indicators{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return analysis.Indicators{}, fmt.Errorf("call python engine: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return analysis.Indicators{}, fmt.Errorf("python engine returned status %d", resp.StatusCode)
	}

	var result analyzeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return analysis.Indicators{}, fmt.Errorf("decode response: %w", err)
	}

	return analysis.Indicators{
		RSI: result.Indicators.RSI,
		MACD: analysis.MACD{
			Line:      result.Indicators.MACD.Line,
			Signal:    result.Indicators.MACD.Signal,
			Histogram: result.Indicators.MACD.Histogram,
		},
		Bollinger: analysis.Bollinger{
			Upper:  result.Indicators.Bollinger.Upper,
			Middle: result.Indicators.Bollinger.Middle,
			Lower:  result.Indicators.Bollinger.Lower,
		},
	}, nil
}
```

- [ ] **Step 5: テストを実行して成功を確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestPythonEngineClient -v`
Expected: PASS

- [ ] **Step 6: コミット**

```bash
cd go-api
git add internal/domain/analysis internal/interface/gateway/python_engine_client.go internal/interface/gateway/python_engine_client_test.go
git commit -m "feat: add Python engine client"
```

---

### Task 4: Claude APIクライアント（AIレポート生成）

**Files:**
- Modify: `go-api/go.mod`（`github.com/anthropics/anthropic-sdk-go`追加）
- Create: `go-api/internal/domain/ai/report_generator.go`
- Create: `go-api/internal/interface/gateway/claude_client.go`
- Test: `go-api/internal/interface/gateway/claude_client_test.go`

**Interfaces:**
- Produces: `ai.ReportGenerator`インターフェース（`GenerateReport(prompt string) (string, error)`）、`gateway.NewClaudeClient(apiKey, model string) *ClaudeClient`、`gateway.NewClaudeClientWithBaseURL(apiKey, baseURL, model string) *ClaudeClient`（テスト用）

- [ ] **Step 1: 依存を追加**

```bash
cd go-api
go get github.com/anthropics/anthropic-sdk-go
```

- [ ] **Step 2: ドメインインターフェースを作成**

`go-api/internal/domain/ai/report_generator.go`:
```go
package ai

type ReportGenerator interface {
	GenerateReport(prompt string) (string, error)
}
```

- [ ] **Step 3: 失敗するテストを書く**

`go-api/internal/interface/gateway/claude_client_test.go`:
```go
package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/require"
)

func TestClaudeClient_GenerateReport(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_test", "type": "message", "role": "assistant",
			"model": "claude-opus-5",
			"content": []map[string]string{
				{"type": "text", "text": "出来高急増を伴う上昇であり、決算好感の可能性が高い。"},
			},
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
			"usage":         map[string]int{"input_tokens": 100, "output_tokens": 30},
		}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewClaudeClientWithBaseURL("test-api-key", srv.URL, "claude-opus-5")

	got, err := client.GenerateReport("銘柄コード：7203\n異常スコア：3.2σ\n分析してください。")
	require.NoError(t, err)
	require.Equal(t, "出来高急増を伴う上昇であり、決算好感の可能性が高い。", got)
}

func TestClaudeClient_GenerateReport_RetriesOnceThenFails(t *testing.T) {
	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewClaudeClientWithBaseURL("test-api-key", srv.URL, "claude-opus-5")

	_, err := client.GenerateReport("prompt")
	require.Error(t, err)
	require.Equal(t, 2, callCount, "1回失敗後にもう1回リトライするはず")
}
```

- [ ] **Step 4: テストを実行して失敗を確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestClaudeClient -v`
Expected: FAIL（`gateway.NewClaudeClientWithBaseURL`が未定義）

- [ ] **Step 5: Claude APIクライアントを実装**

`go-api/internal/interface/gateway/claude_client.go`:
```go
package gateway

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const claudeMaxTokens = 300

type ClaudeClient struct {
	client anthropic.Client
	model  string
}

func NewClaudeClient(apiKey, model string) *ClaudeClient {
	return &ClaudeClient{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
	}
}

func NewClaudeClientWithBaseURL(apiKey, baseURL, model string) *ClaudeClient {
	return &ClaudeClient{
		client: anthropic.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL)),
		model:  model,
	}
}

// GenerateReport はClaude APIでAI分析レポートを生成する。
// 失敗時は仕様どおり1回だけリトライする。
func (c *ClaudeClient) GenerateReport(prompt string) (string, error) {
	report, err := c.generate(prompt)
	if err != nil {
		report, err = c.generate(prompt)
	}
	if err != nil {
		return "", fmt.Errorf("claude api: %w", err)
	}
	return report, nil
}

func (c *ClaudeClient) generate(prompt string) (string, error) {
	message, err := c.client.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: claudeMaxTokens,
		Thinking:  anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return "", err
	}
	if message.StopReason == anthropic.StopReasonRefusal {
		return "", fmt.Errorf("claude refused the request")
	}
	for _, block := range message.Content {
		if text, ok := block.AsAny().(anthropic.TextBlock); ok {
			return text.Text, nil
		}
	}
	return "", fmt.Errorf("no text content in claude response")
}
```

- [ ] **Step 6: テストを実行して成功を確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestClaudeClient -v`
Expected: PASS

- [ ] **Step 7: コミット**

```bash
cd go-api
git add go.mod go.sum internal/domain/ai internal/interface/gateway/claude_client.go internal/interface/gateway/claude_client_test.go
git commit -m "feat: add Claude API client for AI report generation"
```

---

### Task 5: Slack通知クライアント

**Files:**
- Create: `go-api/internal/domain/notifier/notifier.go`
- Create: `go-api/internal/interface/gateway/slack_client.go`
- Test: `go-api/internal/interface/gateway/slack_client_test.go`

**Interfaces:**
- Produces: `notifier.Notifier`インターフェース（`Send(message string) error`）、`gateway.NewSlackClient(webhookURL string) *SlackClient`

- [ ] **Step 1: ドメインインターフェースを作成**

`go-api/internal/domain/notifier/notifier.go`:
```go
package notifier

type Notifier interface {
	Send(message string) error
}
```

- [ ] **Step 2: 失敗するテストを書く**

`go-api/internal/interface/gateway/slack_client_test.go`:
```go
package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlackClient_Send_Success(t *testing.T) {
	var received map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewSlackClient(srv.URL + "/webhook")
	err := client.Send("異常検知: 7203")
	require.NoError(t, err)
	assert.Equal(t, "異常検知: 7203", received["text"])
}

func TestSlackClient_Send_RetriesThreeTimesThenFails(t *testing.T) {
	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewSlackClient(srv.URL + "/webhook")
	err := client.Send("異常検知: 7203")
	require.Error(t, err)
	assert.Equal(t, 3, callCount)
}
```

- [ ] **Step 3: テストを実行して失敗を確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestSlackClient -v`
Expected: FAIL（`gateway.NewSlackClient`が未定義）

- [ ] **Step 4: Slackクライアントを実装**

`go-api/internal/interface/gateway/slack_client.go`:
```go
package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const slackMaxRetries = 3

type SlackClient struct {
	webhookURL string
	httpClient *http.Client
}

func NewSlackClient(webhookURL string) *SlackClient {
	return &SlackClient{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *SlackClient) Send(message string) error {
	body, err := json.Marshal(map[string]string{"text": message})
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt < slackMaxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, c.webhookURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		lastErr = fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("slack notification failed after %d attempts: %w", slackMaxRetries, lastErr)
}
```

- [ ] **Step 5: テストを実行して成功を確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestSlackClient -v`
Expected: PASS

- [ ] **Step 6: コミット**

```bash
cd go-api
git add internal/domain/notifier internal/interface/gateway/slack_client.go internal/interface/gateway/slack_client_test.go
git commit -m "feat: add Slack webhook client"
```

---

### Task 6: オーケストレーションユースケース（AnalyzeAndNotifyUsecase）

**Files:**
- Create: `go-api/internal/usecase/analyze_and_notify.go`
- Test: `go-api/internal/usecase/analyze_and_notify_test.go`

**Interfaces:**
- Consumes: `news.Fetcher`（Task 2）、`analysis.Analyzer`（Task 3）、`ai.ReportGenerator`（Task 4）、`notifier.Notifier`（Task 5）、`notification.Repository`（Task 1）
- Produces: `usecase.NewAnalyzeAndNotifyUsecase(newsFetcher news.Fetcher, analyzer analysis.Analyzer, reportGen ai.ReportGenerator, notif notifier.Notifier, notifications notification.Repository) *AnalyzeAndNotifyUsecase`、`(u *AnalyzeAndNotifyUsecase) Handle(ctx context.Context, code stock.StockCode, zScore, currentPrice float64, prices []float64) error`

- [ ] **Step 1: 失敗するテストを書く**

`go-api/internal/usecase/analyze_and_notify_test.go`:
```go
package usecase_test

import (
	"context"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockNewsFetcher struct{ mock.Mock }

func (m *MockNewsFetcher) FetchRecent(code stock.StockCode) ([]news.Item, error) {
	args := m.Called(code)
	return args.Get(0).([]news.Item), args.Error(1)
}

type MockAnalyzer struct{ mock.Mock }

func (m *MockAnalyzer) Analyze(code stock.StockCode, zScore, currentPrice float64, prices []float64) (analysis.Indicators, error) {
	args := m.Called(code, zScore, currentPrice, prices)
	return args.Get(0).(analysis.Indicators), args.Error(1)
}

type MockReportGenerator struct{ mock.Mock }

func (m *MockReportGenerator) GenerateReport(prompt string) (string, error) {
	args := m.Called(prompt)
	return args.String(0), args.Error(1)
}

type MockNotifier struct{ mock.Mock }

func (m *MockNotifier) Send(message string) error {
	return m.Called(message).Error(0)
}

type MockNotificationRepository struct{ mock.Mock }

func (m *MockNotificationRepository) Save(ctx context.Context, n notification.Notification) error {
	return m.Called(ctx, n).Error(0)
}

func rsi(v float64) *float64 { return &v }

func TestAnalyzeAndNotifyUsecase_Handle_HappyPath(t *testing.T) {
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	repo := &MockNotificationRepository{}

	code, _ := stock.NewStockCode("7203")
	indicators := analysis.Indicators{RSI: rsi(65.3)}

	newsFetcher.On("FetchRecent", code).Return([]news.Item{{Headline: "h", Summary: "s"}}, nil)
	analyzer.On("Analyze", code, 3.2, 3250.0, mock.Anything).Return(indicators, nil)
	reportGen.On("GenerateReport", mock.Anything).Return("AI分析結果", nil)
	notif.On("Send", mock.MatchedBy(func(msg string) bool { return msg != "" })).Return(nil)
	repo.On("Save", mock.Anything, mock.MatchedBy(func(n notification.Notification) bool {
		return n.SlackSent && n.StockCode == "7203"
	})).Return(nil)

	uc := usecase.NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, repo)
	err := uc.Handle(context.Background(), code, 3.2, 3250.0, []float64{3000, 3010, 3250})
	require.NoError(t, err)

	newsFetcher.AssertExpectations(t)
	analyzer.AssertExpectations(t)
	reportGen.AssertExpectations(t)
	notif.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestAnalyzeAndNotifyUsecase_Handle_AnalyzerFailure_SkipsNotification(t *testing.T) {
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	repo := &MockNotificationRepository{}

	code, _ := stock.NewStockCode("7203")
	newsFetcher.On("FetchRecent", code).Return([]news.Item{}, nil)
	analyzer.On("Analyze", code, 3.2, 3250.0, mock.Anything).Return(analysis.Indicators{}, assert.AnError)

	uc := usecase.NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, repo)
	err := uc.Handle(context.Background(), code, 3.2, 3250.0, []float64{3000, 3010, 3250})
	require.Error(t, err)

	reportGen.AssertNotCalled(t, "GenerateReport", mock.Anything)
	notif.AssertNotCalled(t, "Send", mock.Anything)
	repo.AssertNotCalled(t, "Save", mock.Anything, mock.Anything)
}

func TestAnalyzeAndNotifyUsecase_Handle_ClaudeFailure_FallsBackToIndicatorsOnly(t *testing.T) {
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	repo := &MockNotificationRepository{}

	code, _ := stock.NewStockCode("7203")
	indicators := analysis.Indicators{RSI: rsi(65.3)}

	newsFetcher.On("FetchRecent", code).Return([]news.Item{}, nil)
	analyzer.On("Analyze", code, 3.2, 3250.0, mock.Anything).Return(indicators, nil)
	reportGen.On("GenerateReport", mock.Anything).Return("", assert.AnError)
	notif.On("Send", mock.MatchedBy(func(msg string) bool {
		return msg != "" // フォールバックメッセージが送信される
	})).Return(nil)
	repo.On("Save", mock.Anything, mock.Anything).Return(nil)

	uc := usecase.NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, repo)
	err := uc.Handle(context.Background(), code, 3.2, 3250.0, []float64{3000, 3010, 3250})
	require.NoError(t, err)

	notif.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestAnalyzeAndNotifyUsecase_Handle_SlackFailure_SavesWithSlackSentFalse(t *testing.T) {
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	repo := &MockNotificationRepository{}

	code, _ := stock.NewStockCode("7203")
	indicators := analysis.Indicators{RSI: rsi(65.3)}

	newsFetcher.On("FetchRecent", code).Return([]news.Item{}, nil)
	analyzer.On("Analyze", code, 3.2, 3250.0, mock.Anything).Return(indicators, nil)
	reportGen.On("GenerateReport", mock.Anything).Return("AI分析結果", nil)
	notif.On("Send", mock.Anything).Return(assert.AnError)
	repo.On("Save", mock.Anything, mock.MatchedBy(func(n notification.Notification) bool {
		return !n.SlackSent
	})).Return(nil)

	uc := usecase.NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, repo)
	err := uc.Handle(context.Background(), code, 3.2, 3250.0, []float64{3000, 3010, 3250})
	require.NoError(t, err, "Slack失敗自体はHandleのエラーにしない")

	repo.AssertExpectations(t)
}
```

- [ ] **Step 2: テストを実行して失敗を確認**

Run: `cd go-api && go test ./internal/usecase/... -run TestAnalyzeAndNotifyUsecase -v`
Expected: FAIL（`usecase.NewAnalyzeAndNotifyUsecase`が未定義）

- [ ] **Step 3: オーケストレーションユースケースを実装**

`go-api/internal/usecase/analyze_and_notify.go`:
```go
package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/stock-anomaly-detection/go-api/internal/domain/ai"
	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notifier"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type AnalyzeAndNotifyUsecase struct {
	newsFetcher   news.Fetcher
	analyzer      analysis.Analyzer
	reportGen     ai.ReportGenerator
	notifier      notifier.Notifier
	notifications notification.Repository
}

func NewAnalyzeAndNotifyUsecase(
	newsFetcher news.Fetcher,
	analyzer analysis.Analyzer,
	reportGen ai.ReportGenerator,
	notif notifier.Notifier,
	notifications notification.Repository,
) *AnalyzeAndNotifyUsecase {
	return &AnalyzeAndNotifyUsecase{
		newsFetcher:   newsFetcher,
		analyzer:      analyzer,
		reportGen:     reportGen,
		notifier:      notif,
		notifications: notifications,
	}
}

// Handle は異常検知された銘柄のニュース取得・テクニカル指標計算・AIレポート生成・
// Slack通知・通知履歴保存を行う。Pythonエンジン呼び出しが失敗した場合のみエラーを返し、
// それ以外の失敗（ニュース取得・Claude API・Slack）はログに記録して処理を継続する。
func (u *AnalyzeAndNotifyUsecase) Handle(ctx context.Context, code stock.StockCode, zScore, currentPrice float64, prices []float64) error {
	items, err := u.newsFetcher.FetchRecent(code)
	if err != nil {
		log.Printf("WARN news fetch failed for %s: %v", code, err)
		items = nil
	}

	indicators, err := u.analyzer.Analyze(code, zScore, currentPrice, prices)
	if err != nil {
		log.Printf("ERROR python engine analyze failed for %s: %v (Slack通知をスキップ)", code, err)
		return fmt.Errorf("analyze indicators: %w", err)
	}

	var report string
	if aiReport, err := u.reportGen.GenerateReport(buildPrompt(code, zScore, currentPrice, indicators, items)); err != nil {
		log.Printf("WARN claude report generation failed for %s: %v (指標のみで通知)", code, err)
		report = buildIndicatorsOnlyMessage(code, zScore, currentPrice, indicators)
	} else {
		report = fmt.Sprintf("⚠️ 異常検知: %s (z=%.2fσ)\n%s", code, zScore, aiReport)
	}

	slackSent := true
	if err := u.notifier.Send(report); err != nil {
		log.Printf("ERROR slack notification failed for %s: %v", code, err)
		slackSent = false
	}

	indicatorsJSON, err := json.Marshal(indicators)
	if err != nil {
		return fmt.Errorf("marshal indicators: %w", err)
	}

	n := notification.Notification{
		StockCode:           code.String(),
		AnomalyScore:        zScore,
		AIReport:             report,
		TechnicalIndicators: string(indicatorsJSON),
		SlackSent:           slackSent,
	}
	if err := u.notifications.Save(ctx, n); err != nil {
		return fmt.Errorf("save notification: %w", err)
	}
	return nil
}

func buildPrompt(code stock.StockCode, zScore, currentPrice float64, indicators analysis.Indicators, items []news.Item) string {
	newsSummary := "関連ニュースなし"
	if len(items) > 0 {
		var sb strings.Builder
		for _, item := range items {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", item.Headline, item.Summary))
		}
		newsSummary = sb.String()
	}

	return fmt.Sprintf(
		"銘柄コード：%s\n異常スコア：%.2fσ（過去30日比）\n現在株価：%.1f円\nRSI：%s\nMACD：%s\n直近ニュース要約：\n%s\n上記を踏まえ異常の原因とリスクを200字以内で分析してください。",
		code, zScore, currentPrice, formatFloatPtr(indicators.RSI), formatMACD(indicators.MACD), newsSummary,
	)
}

func buildIndicatorsOnlyMessage(code stock.StockCode, zScore, currentPrice float64, indicators analysis.Indicators) string {
	return fmt.Sprintf(
		"⚠️ 異常検知: %s (z=%.2fσ)\n現在株価：%.1f円\nRSI：%s\nMACD：%s\n（AI分析レポートの生成に失敗したため、テクニカル指標のみ通知しています）",
		code, zScore, currentPrice, formatFloatPtr(indicators.RSI), formatMACD(indicators.MACD),
	)
}

func formatFloatPtr(v *float64) string {
	if v == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2f", *v)
}

func formatMACD(m analysis.MACD) string {
	return fmt.Sprintf("line=%s signal=%s histogram=%s", formatFloatPtr(m.Line), formatFloatPtr(m.Signal), formatFloatPtr(m.Histogram))
}
```

- [ ] **Step 4: テストを実行して成功を確認**

Run: `cd go-api && go test ./internal/usecase/... -run TestAnalyzeAndNotifyUsecase -v`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
cd go-api
git add internal/usecase/analyze_and_notify.go internal/usecase/analyze_and_notify_test.go
git commit -m "feat: add AnalyzeAndNotifyUsecase orchestrating news, indicators, AI report, and Slack notification"
```

---

### Task 7: MonitorUsecaseへの統合

**Files:**
- Modify: `go-api/internal/usecase/monitor_stocks.go`
- Modify: `go-api/internal/usecase/monitor_stocks_test.go`

**Interfaces:**
- Consumes: `*AnalyzeAndNotifyUsecase`（Task 6, `Handle(ctx, code, zScore, currentPrice float64, prices []float64) error`）
- Produces: `usecase.NewMonitorUsecase(fetcher stock.PriceFetcher, cache stock.PriceCache, detector *anomaly.DetectionService, threshold float64, notifyUsecase *AnalyzeAndNotifyUsecase) *MonitorUsecase`（シグネチャ変更）

- [ ] **Step 1: 既存テストのコンストラクタ呼び出しを新シグネチャに更新**

`go-api/internal/usecase/monitor_stocks_test.go`内の3箇所の`usecase.NewMonitorUsecase(fetcher, priceCache, svc, 2.5)`を`usecase.NewMonitorUsecase(fetcher, priceCache, svc, 2.5, nil)`に変更する（`TestMonitorUsecase_CheckStock_DetectsAnomaly`, `TestMonitorUsecase_CheckStock_InsufficientHistory`, `TestMonitorUsecase_CheckStock_SkipsDuplicateDate`の3関数）。

- [ ] **Step 2: テストを実行して失敗を確認**

Run: `cd go-api && go build ./... 2>&1 | head -20`
Expected: FAIL（`NewMonitorUsecase`の引数不足でコンパイルエラー。Step 1変更後は逆に`MonitorUsecase`側の未対応でコンパイルエラーになる）

- [ ] **Step 3: MonitorUsecaseに通知連携を追加**

`go-api/internal/usecase/monitor_stocks.go`の該当箇所を以下のように変更する。

`MonitorUsecase`構造体と`NewMonitorUsecase`:
```go
type MonitorUsecase struct {
	fetcher       stock.PriceFetcher
	cache         stock.PriceCache
	detector      *anomaly.DetectionService
	threshold     float64
	notifyUsecase *AnalyzeAndNotifyUsecase
}

func NewMonitorUsecase(
	fetcher stock.PriceFetcher,
	cache stock.PriceCache,
	detector *anomaly.DetectionService,
	threshold float64,
	notifyUsecase *AnalyzeAndNotifyUsecase,
) *MonitorUsecase {
	return &MonitorUsecase{
		fetcher:       fetcher,
		cache:         cache,
		detector:      detector,
		threshold:     threshold,
		notifyUsecase: notifyUsecase,
	}
}
```

`StartMonitoring`内の異常検知後の分岐（`case <-timer.C:`ブロック）を以下に置き換える:
```go
				case <-timer.C:
					detected, z, err := u.CheckStock(ctx, code)
					if err != nil {
						log.Printf("ERROR monitoring %s: %v", code, err)
						continue
					}
					if detected {
						log.Printf("ANOMALY %s z=%.2f (threshold=%.1f)", code, z, u.threshold)
						u.notify(ctx, code, z)
					}
```

新規メソッドを`CheckStock`の後に追加:
```go
// notify は異常検知後、AnalyzeAndNotifyUsecase が設定されていれば
// 直近の価格履歴を取得してAI分析・Slack通知パイプラインを起動する。
func (u *MonitorUsecase) notify(ctx context.Context, code stock.StockCode, z anomaly.ZScore) {
	if u.notifyUsecase == nil {
		return
	}
	prices, err := u.cache.GetHistory(code, historySize)
	if err != nil || len(prices) == 0 {
		log.Printf("ERROR fetch history for notify %s: %v", code, err)
		return
	}
	floatPrices := make([]float64, len(prices))
	for i, p := range prices {
		floatPrices[i] = float64(p)
	}
	currentPrice := floatPrices[len(floatPrices)-1]
	if err := u.notifyUsecase.Handle(ctx, code, float64(z), currentPrice, floatPrices); err != nil {
		log.Printf("ERROR analyze and notify %s: %v", code, err)
	}
}
```

- [ ] **Step 4: 全テストを実行して成功を確認**

Run: `cd go-api && go build ./... && go test -race -short ./...`
Expected: PASS（ビルド成功、全単体テストPASS）

- [ ] **Step 5: コミット**

```bash
cd go-api
git add internal/usecase/monitor_stocks.go internal/usecase/monitor_stocks_test.go
git commit -m "feat: wire AnalyzeAndNotifyUsecase into anomaly detection flow"
```

---

### Task 8: main.goへのDI配線と環境変数ドキュメント更新

**Files:**
- Modify: `go-api/cmd/api/main.go`
- Modify: `CLAUDE.md`（リポジトリルート）

**Interfaces:**
- Consumes: Task 1〜7で作成した全コンストラクタ

- [ ] **Step 1: main.goに環境変数読み込みとDI配線を追加**

`go-api/cmd/api/main.go`のimportに追加:
```go
	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/cache"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
```
（既存のimportに変更なし。以下は`func main()`内の変更点）

`jQuantsAPIKey := mustEnv("JQUANTS_API_KEY")`の直後に追加:
```go
	finnhubAPIKey := mustEnv("FINNHUB_API_KEY")
	anthropicAPIKey := mustEnv("ANTHROPIC_API_KEY")
	slackWebhookURL := mustEnv("SLACK_WEBHOOK_URL")
	pythonEngineURL := mustEnv("PYTHON_ENGINE_URL")

	claudeModel := "claude-opus-5"
	if m := os.Getenv("CLAUDE_MODEL"); m != "" {
		claudeModel = m
	}
```

`priceFetcher := gateway.NewJQuantsClient(jQuantsAPIKey)`の直後に追加:
```go
	newsClient := gateway.NewFinnhubClient(finnhubAPIKey)
	pythonEngineClient := gateway.NewPythonEngineClient(pythonEngineURL)
	claudeClient := gateway.NewClaudeClient(anthropicAPIKey, claudeModel)
	slackClient := gateway.NewSlackClient(slackWebhookURL)
	notificationRepo := persistence.NewPgNotificationRepository(conn)
	notifyUsecase := usecase.NewAnalyzeAndNotifyUsecase(newsClient, pythonEngineClient, claudeClient, slackClient, notificationRepo)
```

`monitor := usecase.NewMonitorUsecase(priceFetcher, priceCache, detector, threshold)`を以下に変更:
```go
	monitor := usecase.NewMonitorUsecase(priceFetcher, priceCache, detector, threshold, notifyUsecase)
```

- [ ] **Step 2: ビルドを確認**

Run: `cd go-api && go build ./...`
Expected: 成功（コンパイルエラーなし）

- [ ] **Step 3: CLAUDE.mdの環境変数セクションを更新**

`CLAUDE.md`の`## 環境変数（本番）`セクションを以下に置き換える:
```markdown
## 環境変数（本番）
DATABASE_URL, REDIS_URL, JQUANTS_API_KEY, STOCK_CODES（カンマ区切り4桁コード）, ANOMALY_THRESHOLD（デフォルト2.5）, FINNHUB_API_KEY, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）
```

`## 重要な設計決定`セクションの末尾に追記:
```markdown
- Phase 3: ニュース取得はGo側（`gateway.FinnhubClient`）で実施。異常検知時に `AnalyzeAndNotifyUsecase` が ニュース取得→Pythonエンジン(`/analyze`)→Claude API→Slack通知→`notifications`テーブル保存の順に実行する
- Claude APIはリトライ1回、失敗時はテクニカル指標のみのSlack通知にフォールバック。Slack通知は最大3回リトライ、失敗時も`slack_sent=false`で通知履歴を保存する
```

- [ ] **Step 4: 全テストとビルドを再確認**

Run: `cd go-api && go build ./... && go test -race -short ./...`
Expected: PASS

- [ ] **Step 5: コミット**

```bash
git add go-api/cmd/api/main.go CLAUDE.md
git commit -m "feat: wire Phase 3 gateways into main.go and document new env vars"
```

---

### Task 9: PR作成

- [ ] **Step 1: リモートにプッシュ**

```bash
git push -u origin feature/phase3-claude-slack-notification
```

- [ ] **Step 2: GitHub Actions（go-test.yml）がPASSすることを確認**

Run: `gh pr checks --watch` （プッシュ後、CI完了まで待機）
Expected: 全ジョブPASS

- [ ] **Step 3: PRを作成**

```bash
gh pr create --title "feat: Phase 3 - Claude API連携・Slack通知" --body "$(cat <<'EOF'
## Summary
- 異常検知時にFinnhubニュース取得→Pythonエンジンでのテクニカル指標計算→Claude APIでのAI分析レポート生成→Slack通知→`notifications`テーブル保存を行うパイプラインを追加
- Claude APIは`claude-opus-5`を使用（1回リトライ、失敗時は指標のみのフォールバック通知）
- Slack通知は最大3回リトライ、失敗時も`slack_sent=false`で履歴を保存
- 新規環境変数: `FINNHUB_API_KEY`, `ANTHROPIC_API_KEY`, `SLACK_WEBHOOK_URL`, `PYTHON_ENGINE_URL`, `CLAUDE_MODEL`

## Test plan
- [x] `go test -race -short ./...` 全PASS（ユニットテスト、gateway層は`httptest`でモック）
- [x] `go build ./...` 成功
- [ ] `go test -race ./...`（DATABASE_URL/REDIS_URL必要な統合テスト、CI上で確認）

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 4: PR URLをユーザーに報告**

---

## Summary

Phase 3として、異常検知後の「ニュース取得→テクニカル指標計算→AI分析レポート生成→Slack通知→履歴保存」パイプラインをGo API側に実装する。設計書§6のプロンプトテンプレートは、Go側で現状取得可能なデータ（銘柄コード・Zスコア・現在株価・RSI・MACD・ニュース要約）に合わせて簡略化している（銘柄名・前日比・出来高比は未実装のため含まない）。

## セルフレビュー

### スペックカバレッジ確認
- ニュース取得（Go側、Finnhub）: Task 2 ✓
- Pythonエンジン連携（`/analyze`呼び出し）: Task 3 ✓
- Claude API連携（AIレポート生成、200字以内指示、リトライ1回、失敗時フォールバック）: Task 4, Task 6 ✓
- Slack通知（最大3回リトライ、失敗時`slack_sent=false`）: Task 5, Task 6 ✓
- 通知履歴のPostgreSQL保存（`notifications`テーブル、Phase1で作成済み）: Task 1, Task 6 ✓
- Pythonエンジンタイムアウト時のSlack通知スキップ: Task 6（`Analyze`失敗時はエラーを返しSlack送信・保存をスキップ）✓
- MonitorUsecaseからの呼び出し配線: Task 7 ✓
- DI配線・環境変数: Task 8 ✓

### プレースホルダスキャン
各タスクのコードブロックは実装可能な完全なコードで記述しており、「TODO」「後で実装」等のプレースホルダは含まれていない。

### 型整合性確認
- `analysis.Analyzer.Analyze(code stock.StockCode, zScore, currentPrice float64, prices []float64) (Indicators, error)` — Task 3定義・Task 3実装（`PythonEngineClient.Analyze`）・Task 6使用箇所で一致
- `news.Fetcher.FetchRecent(code stock.StockCode) ([]Item, error)` — Task 2定義・実装・Task 6使用箇所で一致
- `ai.ReportGenerator.GenerateReport(prompt string) (string, error)` — Task 4定義・実装・Task 6使用箇所で一致
- `notifier.Notifier.Send(message string) error` — Task 5定義・実装・Task 6使用箇所で一致
- `notification.Repository.Save(ctx context.Context, n Notification) error` — Task 1定義・実装・Task 6使用箇所で一致
- `usecase.NewMonitorUsecase(...)` の新シグネチャ（`notifyUsecase *AnalyzeAndNotifyUsecase`追加）— Task 7で定義・既存テスト呼び出し元・Task 8のmain.go呼び出し元で一致
