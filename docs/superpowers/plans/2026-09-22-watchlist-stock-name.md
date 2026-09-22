# Watchlist 銘柄名表示 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** watchlist一覧で証券コードだけでなく銘柄名も表示できるようにする。

**Architecture:** `YahooFinanceClient`が既に叩いているchart APIのレスポンスに含まれる`meta.longName`/`meta.shortName`を新規パースし、`ManageWatchlistUsecase.Add`実行時（DB登録前）に銘柄名を取得して`watchlist.stock_name`カラムへ保存する。取得はバックフィル処理とは独立して常に実行し、失敗してもwatchlist登録自体はブロックしない。フロントエンドは`GET /watchlist`のレスポンスに含まれる`stock_name`をそのままテーブルに表示する。

**Tech Stack:** Go 1.x（`go-api/`）、Next.js/TypeScript（`frontend/`）、PostgreSQL、testify/mock（Go）、Vitest（フロントエンド）

**Spec:** ブレインストーミング（本セッション内、spec文書化なし。設計合意事項は本プランのGlobal Constraintsに集約）

## Global Constraints

- 永続化先は`watchlist`テーブルの新規カラム`stock_name TEXT`（NULL許容）。銘柄マスタテーブルは作らない
- 表示名は`meta.longName`優先、空なら`meta.shortName`、両方空なら空文字列（英語表記のみ、日本語ローカライズはしない）
- 既存のwatchlist行は`stock_name`がNULLのままでよい（遡及バックフィルはしない）。フロントエンドは値がない場合`-`で表示
- 銘柄名取得はバックフィル（`BackfillPriceHistoryUsecase`）に便乗させない。バックフィルは既存Redis履歴があると外部APIを呼ばずスキップするため、銘柄名取得は`ManageWatchlistUsecase.Add`から独立して常に呼ぶ
- 名前取得失敗時はログ出力のみ・空文字にフォールバックし、watchlist登録自体は成功させる（既存のバックフィル失敗時と同じ方針）
- CI（`.github/workflows/go-test.yml`）は現在`001_initial_schema.sql`のみ適用しているため、新規マイグレーションファイルも適用するよう更新する

---

### Task 1: DBマイグレーション追加とCI更新

**Files:**
- Create: `go-api/migrations/002_add_watchlist_stock_name.sql`
- Modify: `.github/workflows/go-test.yml:52-53`

**Interfaces:**
- Produces: `watchlist.stock_name`カラム（TEXT, NULL許容）— 以降のタスクがこのカラムを読み書きする

- [ ] **Step 1: マイグレーションファイルを作成**

`go-api/migrations/002_add_watchlist_stock_name.sql`:

```sql
ALTER TABLE watchlist ADD COLUMN IF NOT EXISTS stock_name TEXT;
```

- [ ] **Step 2: CIワークフローに新マイグレーションの適用を追加**

`.github/workflows/go-test.yml`の該当箇所（現在）:

```yaml
      - name: Run database migration
        run: psql $DATABASE_URL -f go-api/migrations/001_initial_schema.sql
        env:
          DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable
```

これを以下に変更する:

```yaml
      - name: Run database migration
        run: |
          psql $DATABASE_URL -f go-api/migrations/001_initial_schema.sql
          psql $DATABASE_URL -f go-api/migrations/002_add_watchlist_stock_name.sql
        env:
          DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable
```

- [ ] **Step 3: ローカルのDATABASE_URLが設定されていれば適用して確認（任意、未設定ならスキップ）**

Run: `psql "$DATABASE_URL" -f go-api/migrations/002_add_watchlist_stock_name.sql`
Expected: `ALTER TABLE`（`DATABASE_URL`未設定の場合はこのステップをスキップしてよい。CI側は次のタスク以降の統合テストで検証される）

- [ ] **Step 4: Commit**

```bash
git add go-api/migrations/002_add_watchlist_stock_name.sql .github/workflows/go-test.yml
git commit -m "feat: add stock_name column to watchlist table"
```

---

### Task 2: YahooFinanceClientに銘柄名取得を追加

**Files:**
- Modify: `go-api/internal/domain/stock/price_fetcher.go`
- Create: `go-api/internal/domain/stock/name_fetcher.go`
- Modify: `go-api/internal/interface/gateway/yahoo_finance_client.go`
- Test: `go-api/internal/interface/gateway/yahoo_finance_client_test.go`

**Interfaces:**
- Produces:
  - `stock.NameFetcher`インターフェース: `FetchCompanyName(code StockCode) (string, error)`
  - `(*gateway.YahooFinanceClient).FetchCompanyName(code stock.StockCode) (string, error)` — `YahooFinanceClient`が`stock.NameFetcher`を実装する
- Consumes: 既存の`(*YahooFinanceClient).fetchChart(code stock.StockCode, rangeParam string) ([]int64, []*float64, error)` — このシグネチャを拡張して名前も返すようにする

- [ ] **Step 1: 失敗するテストを書く（longName優先）**

`go-api/internal/interface/gateway/yahoo_finance_client_test.go`の末尾に追加:

```go
func TestYahooFinanceClient_FetchCompanyName_PrefersLongName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1d", r.URL.Query().Get("range"))
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						"meta": map[string]any{
							"longName":  "Toyota Motor Corporation",
							"shortName": "TOYOTA MOTOR CORP",
						},
						"timestamp": []int64{1783404000},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3250.0}},
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

	name, err := client.FetchCompanyName(code)
	require.NoError(t, err)
	assert.Equal(t, "Toyota Motor Corporation", name)
}

func TestYahooFinanceClient_FetchCompanyName_FallsBackToShortName(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						"meta": map[string]any{
							"shortName": "TOYOTA MOTOR CORP",
						},
						"timestamp": []int64{1783404000},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3250.0}},
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

	name, err := client.FetchCompanyName(code)
	require.NoError(t, err)
	assert.Equal(t, "TOYOTA MOTOR CORP", name)
}

func TestYahooFinanceClient_FetchCompanyName_EmptyWhenNoNameInMeta(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{
					{
						"meta":      map[string]any{},
						"timestamp": []int64{1783404000},
						"indicators": map[string]any{
							"quote": []map[string]any{
								{"close": []any{3250.0}},
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

	name, err := client.FetchCompanyName(code)
	require.NoError(t, err)
	assert.Equal(t, "", name)
}

func TestYahooFinanceClient_FetchCompanyName_ErrorStatus(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v8/finance/chart/0000.T", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewYahooFinanceClientWithBaseURL(srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchCompanyName(code)
	require.Error(t, err)
}
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestYahooFinanceClient_FetchCompanyName -v`
Expected: コンパイルエラー（`FetchCompanyName`未定義）

- [ ] **Step 3: `stock.NameFetcher`インターフェースを新設**

`go-api/internal/domain/stock/name_fetcher.go`（新規）:

```go
package stock

type NameFetcher interface {
	FetchCompanyName(code StockCode) (string, error)
}
```

- [ ] **Step 4: `fetchChart`を拡張して名前も返すようにし、`FetchCompanyName`を実装**

`go-api/internal/interface/gateway/yahoo_finance_client.go`を変更。

まず`fetchChart`のレスポンス用構造体に`Meta`を追加し、返り値に名前を加える。現在の`fetchChart`シグネチャ:

```go
func (c *YahooFinanceClient) fetchChart(code stock.StockCode, rangeParam string) ([]int64, []*float64, error) {
```

これを以下に変更する（既存の呼び出し元`FetchLatest`・`FetchHistory`は返り値が1つ増えるため、受け取り側を`_`で無視するよう修正が必要）:

```go
func (c *YahooFinanceClient) fetchChart(code stock.StockCode, rangeParam string) ([]int64, []*float64, string, error) {
	url := fmt.Sprintf("%s/v8/finance/chart/%s.T?range=%s&interval=1d", c.baseURL, code, rangeParam)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, "", fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, "", fmt.Errorf("yahoo finance api returned status %d", resp.StatusCode)
	}

	var result struct {
		Chart struct {
			Result []struct {
				Meta struct {
					LongName  string `json:"longName"`
					ShortName string `json:"shortName"`
				} `json:"meta"`
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
		return nil, nil, "", fmt.Errorf("decode response: %w", err)
	}
	if len(result.Chart.Result) == 0 || len(result.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, nil, "", fmt.Errorf("no quote data for %s", code)
	}

	r := result.Chart.Result[0]
	timestamps := r.Timestamp
	closes := r.Indicators.Quote[0].Close
	if len(timestamps) < len(closes) {
		closes = closes[:len(timestamps)]
	}
	name := r.Meta.LongName
	if name == "" {
		name = r.Meta.ShortName
	}
	return timestamps, closes, name, nil
}
```

`FetchLatest`の呼び出し箇所を修正:

```go
func (c *YahooFinanceClient) FetchLatest(code stock.StockCode) (stock.Quote, error) {
	timestamps, closes, _, err := c.fetchChart(code, "5d")
	if err != nil {
		return stock.Quote{}, err
	}
	...
```

`FetchHistory`の呼び出し箇所を修正:

```go
func (c *YahooFinanceClient) FetchHistory(code stock.StockCode, days int) ([]stock.Quote, error) {
	timestamps, closes, _, err := c.fetchChart(code, "3mo")
	if err != nil {
		return nil, err
	}
	...
```

新規メソッドを追加（`FetchHistory`の直後などに配置）:

```go
// FetchCompanyName は銘柄の英語表記の企業名を取得する。longNameを優先し、
// なければshortNameにフォールバックする。両方空の場合は空文字列を返す
// （エラーにはしない。呼び出し元でログのみ出して空文字のまま扱う想定）。
func (c *YahooFinanceClient) FetchCompanyName(code stock.StockCode) (string, error) {
	_, _, name, err := c.fetchChart(code, "1d")
	if err != nil {
		return "", err
	}
	return name, nil
}
```

- [ ] **Step 5: テストを再実行してパスすることを確認**

Run: `cd go-api && go test ./internal/interface/gateway/... -run TestYahooFinanceClient -v`
Expected: 全てPASS（既存の`FetchLatest`/`FetchHistory`系テストも壊れていないこと）

- [ ] **Step 6: パッケージ全体のビルド確認**

Run: `cd go-api && go build ./...`
Expected: エラーなし

- [ ] **Step 7: Commit**

```bash
git add go-api/internal/domain/stock/name_fetcher.go go-api/internal/interface/gateway/yahoo_finance_client.go go-api/internal/interface/gateway/yahoo_finance_client_test.go
git commit -m "feat: add FetchCompanyName to YahooFinanceClient"
```

---

### Task 3: `Watchlist`エンティティとリポジトリにstock_nameを反映

**Files:**
- Modify: `go-api/internal/domain/watchlist/entity.go`
- Modify: `go-api/internal/infrastructure/persistence/watchlist_repository.go`
- Test: `go-api/internal/infrastructure/persistence/watchlist_repository_test.go`

**Interfaces:**
- Consumes: `watchlist.Watchlist`構造体（Task 4で`StockName`フィールドを使う）
- Produces: `watchlist.Watchlist.StockName string`フィールド。`PgWatchlistRepository.Create`/`FindByUserID`が`stock_name`カラムを読み書きする

- [ ] **Step 1: `Watchlist`構造体に`StockName`を追加**

`go-api/internal/domain/watchlist/entity.go`:

```go
type Watchlist struct {
	ID             string
	UserID         string
	StockCode      stock.StockCode
	StockName      string
	AlertThreshold float64
	CreatedAt      time.Time
}
```

- [ ] **Step 2: 失敗する統合テストを書く（stock_nameの保存・取得ラウンドトリップ）**

`go-api/internal/infrastructure/persistence/watchlist_repository_test.go`の`TestPgWatchlistRepository_CreateAndFindByUserID`を以下に置き換える（`StockName`の検証を追加）:

```go
func TestPgWatchlistRepository_CreateAndFindByUserID(t *testing.T) {
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
	defer conn.Close()
	_, err = conn.Exec(ctx, "TRUNCATE watchlist, users CASCADE")
	require.NoError(t, err)

	userID := insertTestUser(t, ctx, conn, "watchlist-owner@example.com")
	repo := persistence.NewPgWatchlistRepository(conn)
	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	created, err := repo.Create(ctx, watchlist.Watchlist{UserID: userID, StockCode: code, StockName: "Toyota Motor Corporation", AlertThreshold: 3.0})
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)

	found, err := repo.FindByUserID(ctx, userID)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, code, found[0].StockCode)
	assert.Equal(t, "Toyota Motor Corporation", found[0].StockName)
	assert.Equal(t, 3.0, found[0].AlertThreshold)
}
```

同ファイルに新規テストを追加し、`StockName`が空文字（未設定）で登録された場合にNULLでも正しく空文字として読み出せることを確認する:

```go
func TestPgWatchlistRepository_CreateAndFindByUserID_EmptyStockName(t *testing.T) {
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
	defer conn.Close()
	_, err = conn.Exec(ctx, "TRUNCATE watchlist, users CASCADE")
	require.NoError(t, err)

	userID := insertTestUser(t, ctx, conn, "watchlist-noname@example.com")
	repo := persistence.NewPgWatchlistRepository(conn)
	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	_, err = repo.Create(ctx, watchlist.Watchlist{UserID: userID, StockCode: code, AlertThreshold: 2.5})
	require.NoError(t, err)

	found, err := repo.FindByUserID(ctx, userID)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "", found[0].StockName)
}
```

- [ ] **Step 3: テストが失敗することを確認（DATABASE_URL設定時のみ実行可能）**

Run: `cd go-api && DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test ./internal/infrastructure/persistence/... -run TestPgWatchlistRepository_CreateAndFindByUserID -v`
Expected: `assert.Equal(t, "Toyota Motor Corporation", found[0].StockName)`が空文字との不一致でFAIL（`DATABASE_URL`が未設定の環境ではこのステップ自体がSKIPされる。その場合はStep 6のビルド確認まで進めてよい）

- [ ] **Step 4: `Create`と`FindByUserID`を修正**

`go-api/internal/infrastructure/persistence/watchlist_repository.go`:

```go
func (r *PgWatchlistRepository) FindByUserID(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT id, user_id, stock_code, stock_name, alert_threshold, created_at FROM watchlist WHERE user_id = $1`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []watchlist.Watchlist{}
	for rows.Next() {
		var w watchlist.Watchlist
		var rawCode string
		var stockName sql.NullString
		if err := rows.Scan(&w.ID, &w.UserID, &rawCode, &stockName, &w.AlertThreshold, &w.CreatedAt); err != nil {
			return nil, err
		}
		sc, err := stock.NewStockCode(rawCode)
		if err != nil {
			return nil, fmt.Errorf("invalid stock_code in DB: %w", err)
		}
		w.StockCode = sc
		w.StockName = stockName.String
		result = append(result, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *PgWatchlistRepository) Create(ctx context.Context, w watchlist.Watchlist) (watchlist.Watchlist, error) {
	err := r.conn.QueryRow(ctx,
		`INSERT INTO watchlist (user_id, stock_code, stock_name, alert_threshold) VALUES ($1, $2, $3, $4)
		 RETURNING id, created_at`,
		w.UserID, w.StockCode.String(), nullIfEmpty(w.StockName), w.AlertThreshold,
	).Scan(&w.ID, &w.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return watchlist.Watchlist{}, watchlist.ErrAlreadyExists
		}
		return watchlist.Watchlist{}, err
	}
	return w, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
```

`import`に`"database/sql"`を追加する。

- [ ] **Step 5: テストを再実行してパスすることを確認（DATABASE_URL設定時のみ）**

Run: `cd go-api && DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test ./internal/infrastructure/persistence/... -v`
Expected: 全てPASS

- [ ] **Step 6: パッケージビルド確認**

Run: `cd go-api && go build ./... && go vet ./...`
Expected: エラーなし

- [ ] **Step 7: Commit**

```bash
git add go-api/internal/domain/watchlist/entity.go go-api/internal/infrastructure/persistence/watchlist_repository.go go-api/internal/infrastructure/persistence/watchlist_repository_test.go
git commit -m "feat: persist stock_name on watchlist rows"
```

---

### Task 4: `ManageWatchlistUsecase`で名前取得をAddに組み込む

**Files:**
- Modify: `go-api/internal/usecase/manage_watchlist.go`
- Test: `go-api/internal/usecase/manage_watchlist_test.go`

**Interfaces:**
- Consumes: `stock.NameFetcher`（Task 2で定義）, `watchlist.Watchlist.StockName`（Task 3で定義）
- Produces: `usecase.NewManageWatchlistUsecase(watchlists watchlist.Repository, backfiller *BackfillPriceHistoryUsecase, nameFetcher stock.NameFetcher) *ManageWatchlistUsecase` — コンストラクタが3引数になる（既存呼び出し元は全て更新が必要）

- [ ] **Step 1: 既存テストの呼び出し箇所を3引数に更新（先にコンパイルを通す）**

`go-api/internal/usecase/manage_watchlist_test.go`内の全ての`usecase.NewManageWatchlistUsecase(repo, nil)`および`usecase.NewManageWatchlistUsecase(repo, backfiller)`呼び出し（8箇所: `TestManageWatchlistUsecase_Add_Success`, `TestManageWatchlistUsecase_Add_DefaultThreshold`, `TestManageWatchlistUsecase_Add_InvalidStockCode`, `TestManageWatchlistUsecase_Remove`, `TestManageWatchlistUsecase_UpdateThreshold`, `TestManageWatchlistUsecase_List`, `TestManageWatchlistUsecase_Add_TriggersBackfill`, `TestManageWatchlistUsecase_Add_SucceedsEvenIfBackfillFails`）を、末尾に`nil`を追加した3引数呼び出しに置き換える。例:

```go
uc := usecase.NewManageWatchlistUsecase(repo, nil, nil)
```

```go
backfiller := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
uc := usecase.NewManageWatchlistUsecase(repo, backfiller, nil)
```

- [ ] **Step 2: 名前取得の失敗するテストを書く**

同ファイルに`MockNameFetcher`を追加（ファイル冒頭、`mockWatchlistRepository`の下あたり）:

```go
type MockNameFetcher struct{ mock.Mock }

func (m *MockNameFetcher) FetchCompanyName(code stock.StockCode) (string, error) {
	args := m.Called(code)
	return args.String(0), args.Error(1)
}
```

新規テストを追加:

```go
func TestManageWatchlistUsecase_Add_FetchesCompanyName(t *testing.T) {
	repo := new(mockWatchlistRepository)
	nameFetcher := new(MockNameFetcher)
	code, _ := stock.NewStockCode("7203")

	nameFetcher.On("FetchCompanyName", code).Return("Toyota Motor Corporation", nil)
	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, StockName: "Toyota Motor Corporation", AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, StockName: "Toyota Motor Corporation", AlertThreshold: 3.0}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nameFetcher)
	result, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	require.Equal(t, "Toyota Motor Corporation", result.StockName)
	repo.AssertExpectations(t)
	nameFetcher.AssertExpectations(t)
}

func TestManageWatchlistUsecase_Add_SucceedsEvenIfNameFetchFails(t *testing.T) {
	repo := new(mockWatchlistRepository)
	nameFetcher := new(MockNameFetcher)
	code, _ := stock.NewStockCode("7203")

	nameFetcher.On("FetchCompanyName", code).Return("", errors.New("yahoo finance unavailable"))
	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, StockName: "", AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 3.0}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nameFetcher)
	result, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	require.Equal(t, "wl-1", result.ID)
}
```

- [ ] **Step 3: テストが失敗することを確認**

Run: `cd go-api && go test ./internal/usecase/... -run TestManageWatchlistUsecase -v`
Expected: コンパイルエラー（コンストラクタが3引数を受け付けない）または`FetchCompanyName`未呼び出しでFAIL

- [ ] **Step 4: `ManageWatchlistUsecase`を実装**

`go-api/internal/usecase/manage_watchlist.go`:

```go
type ManageWatchlistUsecase struct {
	watchlists  watchlist.Repository
	backfiller  *BackfillPriceHistoryUsecase
	nameFetcher stock.NameFetcher
}

func NewManageWatchlistUsecase(watchlists watchlist.Repository, backfiller *BackfillPriceHistoryUsecase, nameFetcher stock.NameFetcher) *ManageWatchlistUsecase {
	return &ManageWatchlistUsecase{watchlists: watchlists, backfiller: backfiller, nameFetcher: nameFetcher}
}

// Add はwatchlistへ銘柄を登録する。nameFetcherが設定されていれば銘柄名の取得も試みる
// （失敗してもログのみに留め、空文字のまま登録を続行する）。backfillerが設定されて
// いれば価格履歴のバックフィルも試みる。バックフィル失敗もログのみに留め、登録自体は
// 成功として返す。
func (u *ManageWatchlistUsecase) Add(ctx context.Context, userID, rawStockCode string, threshold float64) (watchlist.Watchlist, error) {
	code, err := stock.NewStockCode(rawStockCode)
	if err != nil {
		return watchlist.Watchlist{}, err
	}
	if threshold == 0 {
		threshold = defaultAlertThreshold
	}

	var name string
	if u.nameFetcher != nil {
		name, err = u.nameFetcher.FetchCompanyName(code)
		if err != nil {
			log.Printf("ERROR fetch company name %s: %v", code, err)
			name = ""
		}
	}

	w, err := u.watchlists.Create(ctx, watchlist.Watchlist{
		UserID:         userID,
		StockCode:      code,
		StockName:      name,
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
```

（`Remove`・`UpdateThreshold`・`List`メソッドは変更なし）

- [ ] **Step 5: テストを再実行してパスすることを確認**

Run: `cd go-api && go test ./internal/usecase/... -run TestManageWatchlistUsecase -v`
Expected: 全てPASS

- [ ] **Step 6: usecaseパッケージ全体のテストとビルド確認**

Run: `cd go-api && go test -race -short ./internal/usecase/... && go build ./...`
Expected: 全てPASS、ビルドエラーなし

- [ ] **Step 7: Commit**

```bash
git add go-api/internal/usecase/manage_watchlist.go go-api/internal/usecase/manage_watchlist_test.go
git commit -m "feat: fetch company name when adding to watchlist"
```

---

### Task 5: main.goの配線とハンドラーレスポンスにstock_nameを追加

**Files:**
- Modify: `go-api/cmd/api/main.go`
- Modify: `go-api/internal/interface/handler/watchlist_handler.go`
- Test: `go-api/internal/interface/handler/watchlist_handler_test.go`

**Interfaces:**
- Consumes: `usecase.NewManageWatchlistUsecase(watchlists, backfiller, nameFetcher)`（Task 4）, `watchlist.Watchlist.StockName`（Task 3）
- Produces: `GET /watchlist`・`POST /watchlist`のJSONレスポンスに`stock_name`フィールドが含まれる

- [ ] **Step 1: main.goの配線を更新**

`go-api/cmd/api/main.go`の該当行:

```go
watchlistUsecase := usecase.NewManageWatchlistUsecase(watchlistRepo, backfillUsecase)
```

これを以下に変更（`priceFetcher`は既に`gateway.NewYahooFinanceClient()`が代入されており、`stock.NameFetcher`も実装しているのでそのまま渡す）:

```go
watchlistUsecase := usecase.NewManageWatchlistUsecase(watchlistRepo, backfillUsecase, priceFetcher)
```

- [ ] **Step 2: 失敗するハンドラーテストを書く**

`go-api/internal/interface/handler/watchlist_handler_test.go`の`TestWatchlistHandler_List`を以下に置き換える:

```go
func TestWatchlistHandler_List(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{listResult: []watchlist.Watchlist{{ID: "wl-1", StockCode: code, StockName: "Toyota Motor Corporation", AlertThreshold: 2.5}}})
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/watchlist", nil), "user-1")
	rec := httptest.NewRecorder()

	h.List(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp []map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Len(t, resp, 1)
	assert.Equal(t, "7203", resp[0]["stock_code"])
	assert.Equal(t, "Toyota Motor Corporation", resp[0]["stock_name"])
}
```

- [ ] **Step 3: テストが失敗することを確認**

Run: `cd go-api && go test ./internal/interface/handler/... -run TestWatchlistHandler_List -v`
Expected: `resp[0]["stock_name"]`が`nil`でFAIL

- [ ] **Step 4: レスポンス構造体を修正**

`go-api/internal/interface/handler/watchlist_handler.go`:

```go
type watchlistItemResponse struct {
	ID             string  `json:"id"`
	StockCode      string  `json:"stock_code"`
	StockName      string  `json:"stock_name"`
	AlertThreshold float64 `json:"alert_threshold"`
}

func toWatchlistItemResponse(w watchlist.Watchlist) watchlistItemResponse {
	return watchlistItemResponse{ID: w.ID, StockCode: w.StockCode.String(), StockName: w.StockName, AlertThreshold: w.AlertThreshold}
}
```

- [ ] **Step 5: テストを再実行してパスすることを確認**

Run: `cd go-api && go test ./internal/interface/handler/... -v`
Expected: 全てPASS

- [ ] **Step 6: go-apiディレクトリ全体の単体テストとビルド確認**

Run: `cd go-api && go test -race -short ./... && go build ./...`
Expected: 全てPASS、ビルドエラーなし

- [ ] **Step 7: Commit**

```bash
git add go-api/cmd/api/main.go go-api/internal/interface/handler/watchlist_handler.go go-api/internal/interface/handler/watchlist_handler_test.go
git commit -m "feat: expose stock_name in watchlist API response"
```

---

### Task 6: フロントエンドの型・テーブル表示を更新

**Files:**
- Modify: `frontend/lib/go-api-client.ts`
- Modify: `frontend/components/watchlist-table.tsx`
- Modify: `frontend/components/watchlist-table.test.tsx`
- Modify: `frontend/app/watchlist/actions.test.ts`

**Interfaces:**
- Consumes: `GET /watchlist`のJSONレスポンス（Task 5で`stock_name`フィールドを含むようになる）
- Produces: `WatchlistItem`型に`stock_name: string`。`<WatchlistTable>`が「銘柄名」列を表示する

- [ ] **Step 1: 失敗するテストを書く（テーブル表示）**

`frontend/components/watchlist-table.test.tsx`を以下に置き換える:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { WatchlistTable } from "./watchlist-table";

vi.mock("@/app/watchlist/actions", () => ({
  removeAction: vi.fn(),
}));

import * as actions from "@/app/watchlist/actions";

describe("WatchlistTable", () => {
  it("renders each watchlist item's stock code and name", () => {
    render(
      <WatchlistTable
        items={[
          {
            id: "1",
            stock_code: "7203",
            stock_name: "Toyota Motor Corporation",
            alert_threshold: 2.5,
          },
          {
            id: "2",
            stock_code: "9984",
            stock_name: "SoftBank Group Corp.",
            alert_threshold: 3.0,
          },
        ]}
      />,
    );

    expect(screen.getByText("7203")).toBeInTheDocument();
    expect(screen.getByText("Toyota Motor Corporation")).toBeInTheDocument();
    expect(screen.getByText("9984")).toBeInTheDocument();
    expect(screen.getByText("SoftBank Group Corp.")).toBeInTheDocument();
  });

  it("falls back to a dash when stock_name is empty", () => {
    render(
      <WatchlistTable
        items={[
          { id: "1", stock_code: "7203", stock_name: "", alert_threshold: 2.5 },
        ]}
      />,
    );

    expect(screen.getByText("-")).toBeInTheDocument();
  });

  it("shows an empty state when items is empty", () => {
    render(<WatchlistTable items={[]} />);

    expect(
      screen.getByText("監視銘柄がまだ登録されていません"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
});

describe("WatchlistTable row actions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("calls removeAction with the item id when the delete button is clicked", async () => {
    vi.mocked(actions.removeAction).mockResolvedValue({ ok: true });

    render(
      <WatchlistTable
        items={[
          {
            id: "1",
            stock_code: "7203",
            stock_name: "Toyota Motor Corporation",
            alert_threshold: 2.5,
          },
        ]}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "削除" }));

    await waitFor(() => {
      expect(actions.removeAction).toHaveBeenCalledWith("1");
    });
  });
});
```

`frontend/app/watchlist/actions.test.ts`の`it("returns ok on success", ...)`内（29行目付近）のリテラルも更新する:

```ts
    vi.mocked(goApiClient.addWatchlistItem).mockResolvedValue({
      ok: true,
      item: {
        id: "1",
        stock_code: "7203",
        stock_name: "Toyota Motor Corporation",
        alert_threshold: 2.5,
      },
    });
```

- [ ] **Step 2: テストが失敗することを確認**

Run: `cd frontend && npx vitest run components/watchlist-table.test.tsx`
Expected: TypeScriptの型エラー（`WatchlistItem`に`stock_name`が存在しない）、または表示アサーションのFAIL

- [ ] **Step 3: 型定義を更新**

`frontend/lib/go-api-client.ts`:

```ts
export type WatchlistItem = {
  id: string;
  stock_code: string;
  stock_name: string;
  alert_threshold: number;
};
```

- [ ] **Step 4: テーブルコンポーネントに銘柄名列を追加**

`frontend/components/watchlist-table.tsx`のテーブル部分を修正:

```tsx
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>証券コード</TableHead>
          <TableHead>銘柄名</TableHead>
          <TableHead />
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((item) => (
          <WatchlistRow key={item.id} item={item} />
        ))}
      </TableBody>
    </Table>
  );
}
```

`WatchlistRow`内の`<TableCell>{item.stock_code}</TableCell>`の直後に列を追加:

```tsx
    <TableRow>
      <TableCell>{item.stock_code}</TableCell>
      <TableCell>{item.stock_name || "-"}</TableCell>
      <TableCell className="text-right">
```

- [ ] **Step 5: テストを再実行してパスすることを確認**

Run: `cd frontend && npx vitest run components/watchlist-table.test.tsx app/watchlist/actions.test.ts`
Expected: 全てPASS

- [ ] **Step 6: フロントエンド全体のテスト・型チェック・ビルド確認**

Run: `cd frontend && npm test && npm run build`
Expected: 全てPASS、ビルドエラーなし（`npm run build`は`GO_API_URL`環境変数が必要な場合がある。[[project_followup_build_time_go_api_url]]参照。必要なら`GO_API_URL=http://localhost:8080 npm run build`で実行する）

- [ ] **Step 7: Commit**

```bash
git add frontend/lib/go-api-client.ts frontend/components/watchlist-table.tsx frontend/components/watchlist-table.test.tsx frontend/app/watchlist/actions.test.ts
git commit -m "feat: display stock name in watchlist table"
```

---

### Task 7: ローカルでのe2e動作確認とPR作成

**Files:** なし（動作確認とPR作成のみ）

- [ ] **Step 1: go-api全体のテストとビルドを最終確認**

Run: `cd go-api && go test -race -short ./... && go build ./...`
Expected: 全てPASS

- [ ] **Step 2: frontend全体のテスト・lint・ビルドを最終確認**

Run: `cd frontend && npm run lint && npm test && npm run build`
Expected: 全てPASS

- [ ] **Step 3: リモートにpushしてPull Requestを作成**

CLAUDE.mdの作業フローに従い、実装完了後は必ずリモートにpushしてPRを作成する。

```bash
git push -u origin feature/watchlist-stock-name
gh pr create --title "feat: watchlist一覧に銘柄名を表示" --body "$(cat <<'EOF'
## Summary
- watchlistに銘柄追加時、Yahoo Finance chart APIのmeta.longName/shortNameから銘柄名を取得しstock_nameカラムに保存
- GET /watchlistのレスポンスにstock_nameを追加し、フロントエンドのテーブルに銘柄名列を表示
- 名前取得はバックフィル処理とは独立して常に実行（バックフィルは既存Redis履歴があるとYahoo Financeを呼ばずスキップするため）。失敗してもwatchlist登録自体はブロックしない
- 既存行のstock_nameはNULLのままとし、フロントエンドは"-"でフォールバック表示

## Test plan
- [ ] `cd go-api && go test -race -short ./...`
- [ ] `cd go-api && go build ./...`
- [ ] `cd frontend && npm run lint && npm test && npm run build`
- [ ] CI（GitHub Actions）でgo-test.yml・frontend-test.ymlがグリーンであることを確認
EOF
)"
```

- [ ] **Step 4: PR作成後、URLを確認して完了**

Run: `gh pr view --web` は不要。`gh pr create`の出力にあるURLをユーザーに報告する。
