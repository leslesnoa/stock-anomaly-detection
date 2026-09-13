# Yanoshin TDnet ニュースクライアント乗り換え Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gateway.FinnhubClient`（東証銘柄のcompany-newsが常に403で失敗する）を、認証不要で動作確認済みの無料API `webapi.yanoshin.jp` のTDnet適時開示リストに差し替え、日本株の異常検知通知にニュース文脈を復活させる。

**Architecture:** 既存の `news.Fetcher` インターフェース（`FetchRecent(code stock.StockCode) ([]news.Item, error)`）はそのまま維持し、実装だけを差し替える。新規 `gateway.YanoshinTDnetClient` を追加し、`GET https://webapi.yanoshin.jp/webapi/tdnet/list/{4桁コード}.json?limit=5` を叩いてTDnet開示タイトルを取得、`news.Item{Headline, Summary}` にマッピングする（`Summary` はタイトルをそのまま流用。APIレスポンスに別立ての要約文がないため）。認証キー・環境変数は不要。`FinnhubClient` と `FINNHUB_API_KEY` は完全に削除する（併存させない）。

**Tech Stack:** Go 1.x、標準ライブラリ `net/http` + `encoding/json`（追加ライブラリ不要）、`net/http/httptest` によるモックテスト、testify（assert/require）。

**Spec:** メモリ `project_followup_jquants_tdnet_news_alternative.md`（本タスクの調査・採用決定の記録。正式なdesignドキュメントは作成しない軽量タスクのため、このメモリと本プランがspec相当）

## Global Constraints

- URLは2026-09-13にユーザーが実際に叩いて動作確認済みのパターンのみを使う: `https://webapi.yanoshin.jp/webapi/tdnet/list/{code}.json?limit={n}`（日付範囲指定など未検証のURLパターンは使わない — Finnhub問題と同じ「未検証エンドポイントで本番失敗」を繰り返さないため）
- `{code}` は4桁の証券コードをそのまま使う（`stock.StockCode.String()`）。5桁変換は不要（レスポンス内の`company_code`が5桁になるだけで、リクエストパスは4桁のまま）
- 認証ヘッダー不要（`x-api-key`も`X-Finnhub-Token`も付けない）
- 既存の`news.Fetcher`インターフェースのシグネチャを変更しない
- テストは`httptest.NewServer`でモックし、実APIを叩かない（CLAUDE.mdのテスト方針）
- `go test -race -short ./...` を都度実行して確認する

---

### Task 1: YanoshinTDnetClient の実装とテスト

**Files:**
- Create: `go-api/internal/interface/gateway/tdnet_client.go`
- Create: `go-api/internal/interface/gateway/tdnet_client_test.go`

**Interfaces:**
- Consumes: `news.Item{Headline string; Summary string}`、`news.Fetcher`インターフェース（`go-api/internal/domain/news/fetcher.go`）、`stock.StockCode`（`go-api/internal/domain/stock/stock.go`、`.String()`で4桁文字列を取得）
- Produces: `gateway.NewYanoshinTDnetClient() *YanoshinTDnetClient`（本番用コンストラクタ、引数なし）、`gateway.NewYanoshinTDnetClientWithBaseURL(baseURL string) *YanoshinTDnetClient`（テスト用）、`(*YanoshinTDnetClient).FetchRecent(code stock.StockCode) ([]news.Item, error)` — Task 3のmain.go配線で使用

- [ ] **Step 1: 失敗するテストを書く（基本ケース）**

`go-api/internal/interface/gateway/tdnet_client_test.go` を新規作成:

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

func TestYanoshinTDnetClient_FetchRecent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "5", r.URL.Query().Get("limit"))

		resp := map[string]any{
			"total_count": 2,
			"items": []map[string]any{
				{"Tdnet": map[string]string{
					"title":        "自己株式の取得状況に関するお知らせ",
					"company_code": "72030",
					"pubdate":      "2026-09-03 15:30:00",
				}},
				{"Tdnet": map[string]string{
					"title":        "業績予想の修正に関するお知らせ",
					"company_code": "72030",
					"pubdate":      "2026-09-01 10:00:00",
				}},
			},
		}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchRecent(code)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "自己株式の取得状況に関するお知らせ", got[0].Headline)
	assert.Equal(t, "自己株式の取得状況に関するお知らせ", got[0].Summary)
	assert.Equal(t, "業績予想の修正に関するお知らせ", got[1].Headline)
}

func TestYanoshinTDnetClient_FetchRecent_LimitsToTop5(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		items := make([]map[string]any, 10)
		for i := range items {
			items[i] = map[string]any{"Tdnet": map[string]string{"title": "h", "company_code": "72030"}}
		}
		resp := map[string]any{"total_count": 10, "items": items}
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchRecent(code)
	require.NoError(t, err)
	assert.Len(t, got, 5)
}

func TestYanoshinTDnetClient_FetchRecent_ErrorStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tdnet/list/7203.json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYanoshinTDnetClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("7203")

	_, err := client.FetchRecent(code)
	require.Error(t, err)
}
```

- [ ] **Step 2: テストを実行して失敗を確認する**

Run: `cd go-api && go test -race -short ./internal/interface/gateway/... -run TestYanoshinTDnetClient -v`
Expected: FAIL（`gateway.NewYanoshinTDnetClientWithBaseURL` が未定義のためコンパイルエラー）

- [ ] **Step 3: 最小実装を書く**

`go-api/internal/interface/gateway/tdnet_client.go` を新規作成:

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

const tdnetLimit = 5

type YanoshinTDnetClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewYanoshinTDnetClient() *YanoshinTDnetClient {
	return NewYanoshinTDnetClientWithBaseURL("https://webapi.yanoshin.jp/webapi")
}

func NewYanoshinTDnetClientWithBaseURL(baseURL string) *YanoshinTDnetClient {
	return &YanoshinTDnetClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *YanoshinTDnetClient) FetchRecent(code stock.StockCode) ([]news.Item, error) {
	url := fmt.Sprintf("%s/tdnet/list/%s.json?limit=%d", c.baseURL, code, tdnetLimit)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch tdnet: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yanoshin tdnet api returned status %d", resp.StatusCode)
	}

	var result struct {
		Items []struct {
			Tdnet struct {
				Title string `json:"title"`
			} `json:"Tdnet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	items := make([]news.Item, 0, tdnetLimit)
	for i, r := range result.Items {
		if i >= tdnetLimit {
			break
		}
		items = append(items, news.Item{Headline: r.Tdnet.Title, Summary: r.Tdnet.Title})
	}
	return items, nil
}
```

- [ ] **Step 4: テストを実行して成功を確認する**

Run: `cd go-api && go test -race -short ./internal/interface/gateway/... -run TestYanoshinTDnetClient -v`
Expected: PASS（3テストすべて）

- [ ] **Step 5: コミット**

```bash
git add go-api/internal/interface/gateway/tdnet_client.go go-api/internal/interface/gateway/tdnet_client_test.go
git commit -m "feat: add YanoshinTDnetClient as news.Fetcher implementation"
```

---

### Task 2: FinnhubClient の削除とmain.goの配線差し替え

**Files:**
- Delete: `go-api/internal/interface/gateway/news_client.go`
- Delete: `go-api/internal/interface/gateway/news_client_test.go`
- Modify: `go-api/cmd/api/main.go:32`（`finnhubAPIKey := mustEnv("FINNHUB_API_KEY")` の行を削除）
- Modify: `go-api/cmd/api/main.go:82`（`newsClient := gateway.NewFinnhubClient(finnhubAPIKey)` → `newsClient := gateway.NewYanoshinTDnetClient()`）

**Interfaces:**
- Consumes: Task 1で作成した `gateway.NewYanoshinTDnetClient() *YanoshinTDnetClient`（引数なし）
- Produces: なし（末端の配線変更）

- [ ] **Step 1: FinnhubClientとそのテストを削除する**

```bash
git rm go-api/internal/interface/gateway/news_client.go go-api/internal/interface/gateway/news_client_test.go
```

- [ ] **Step 2: main.goを編集する**

`go-api/cmd/api/main.go` の該当行を編集:

削除（32行目）:
```go
	finnhubAPIKey := mustEnv("FINNHUB_API_KEY")
```

変更前（82行目）:
```go
	newsClient := gateway.NewFinnhubClient(finnhubAPIKey)
```

変更後:
```go
	newsClient := gateway.NewYanoshinTDnetClient()
```

- [ ] **Step 3: ビルドと全体テストを実行して確認する**

Run: `cd go-api && go build ./... && go test -race -short ./...`
Expected: ビルド成功、全テストPASS（`news_client_test.go`削除により`TestFinnhubClient_*`は消え、`TestYanoshinTDnetClient_*`のみが残る）

- [ ] **Step 4: コミット**

```bash
git add go-api/cmd/api/main.go
git commit -m "refactor: replace FinnhubClient with YanoshinTDnetClient in main wiring"
```

---

### Task 3: CLAUDE.mdのドキュメント更新

**Files:**
- Modify: `CLAUDE.md`（環境変数リストと Phase 3 の記述）

**Interfaces:**
- Consumes: なし
- Produces: なし（ドキュメントのみ）

- [ ] **Step 1: 環境変数（本番）セクションから`FINNHUB_API_KEY`を削除する**

変更前:
```
## 環境変数（本番）
DATABASE_URL, REDIS_URL, JQUANTS_API_KEY, ANOMALY_THRESHOLD（デフォルト2.5）, FINNHUB_API_KEY, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）, JWT_SECRET, PORT（デフォルト8080）
```

変更後:
```
## 環境変数（本番）
DATABASE_URL, REDIS_URL, JQUANTS_API_KEY, ANOMALY_THRESHOLD（デフォルト2.5）, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）, JWT_SECRET, PORT（デフォルト8080）
```

- [ ] **Step 2: Phase 3の記述を`YanoshinTDnetClient`に更新する**

変更前:
```
- Phase 3: ニュース取得はGo側（`gateway.FinnhubClient`）で実施。異常検知時に `AnalyzeAndNotifyUsecase` が ニュース取得→Pythonエンジン(`/analyze`)→Claude API→Slack通知→`notifications`テーブル保存の順に実行する
```

変更後:
```
- Phase 3: ニュース取得はGo側（`gateway.YanoshinTDnetClient`、認証不要の無料TDnet開示情報API `webapi.yanoshin.jp` を利用。非公式サービスのためSLA・レート制限の明記なし）で実施。異常検知時に `AnalyzeAndNotifyUsecase` が ニュース取得→Pythonエンジン(`/analyze`)→Claude API→Slack通知→`notifications`テーブル保存の順に実行する
```

- [ ] **Step 3: コミット**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for YanoshinTDnetClient news source"
```

---

## Self-Review メモ

- **Spec coverage:** メモリ`project_followup_jquants_tdnet_news_alternative.md`の「How to apply」に記載された全項目（`YanoshinTDnetClient`新規作成、`news.Fetcher`実装、`main.go`の`NewFinnhubClient`差し替え、`FINNHUB_API_KEY`削除、`httptest.NewServer`パターンでのテスト）をTask 1〜2でカバー。CLAUDE.mdの環境変数リスト整合はTask 3で追加。
- **Placeholder scan:** 全ステップに実コード・実コマンドを記載済み。TBD等なし。
- **Type consistency:** `FetchRecent(code stock.StockCode) ([]news.Item, error)` のシグネチャはTask 1〜2で一貫。コンストラクタ名`NewYanoshinTDnetClient`/`NewYanoshinTDnetClientWithBaseURL`もTask間で一致。
- **スコープ外（意図的）:** `.env.example`の`FINNHUB_API_KEY`行削除はツールの権限設定により`.env*`ファイルへの読み書きがブロックされているため本プランでは扱わない。ユーザーに手動削除を依頼する。J-Quants公式TDnetアドオンへの切り替えは有償・未契約のため対象外（メモリ参照）。
