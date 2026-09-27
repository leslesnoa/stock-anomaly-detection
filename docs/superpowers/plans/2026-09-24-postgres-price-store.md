# Postgres価格ストア一本化（PR-A）実装計画

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redisの日付なし価格キャッシュを、日付を持つPostgresの`daily_prices`テーブルへ一本化し、バックフィル期間を約2年へ拡張した上で、起動時に既存watchlist銘柄を自動移行する。

**Architecture:** `stock.PriceCache`（日付なし・ctxなし）を`stock.PriceRepository`（`(stock_code, date)`複合キー・ctxあり）へ置き換える。実装は既存の`persistence.PgWatchlistRepository`と同じpgxpoolパターンに従い、一括投入は`pgx.Batch` + `ON CONFLICT DO NOTHING`で冪等にする。Redisには移行可能な日付情報が存在しないため、既存データは移行せず、起動時にYahoo Financeから全watchlist銘柄を再取得する。本PRは**外部から観測できる振る舞いを変えない**データ基盤の刷新であり、チャートAPIとUI（PR-B）は含まない。

**Tech Stack:** Go 1.24 / pgx v5 (`github.com/jackc/pgx/v5`, `pgxpool`) / PostgreSQL 16 / testify (`mock`, `assert`, `require`) / `net/http/httptest`

**Spec:** `docs/superpowers/specs/2026-09-24-stock-chart-forecast-design.md`

## Global Constraints

- Clean Architecture の依存方向 `infrastructure/usecase → domain` を厳守する。逆方向禁止。
- リポジトリインターフェースは domain パッケージ内に定義する（`internal/domain/stock/price_repository.go`）。
- コメントは日本語で書き、**なぜそうしたか**を書く。既存コードのコメント密度に合わせる。
- 単体テスト: `go test -race -short ./...`（外部サービス不要）。統合テスト: `go test -race ./...`（`DATABASE_URL` 必要）。`-race` は必須。
- 統合テストは `testing.Short()` および `DATABASE_URL` 未設定で `t.Skip` する（既存 `watchlist_repository_test.go` と同じ形）。
- Yahoo Finance クライアントのテストは `httptest.NewServer` でモックする。実APIを叩かない。
- **`stock.PriceFetcher`（`FetchLatest` / `FetchHistory`）に `ctx` を追加してはならない。** `POST /watchlist` の同期ブロッキング解消は独立タスクとして先送りすると決定済み（メモリ `followup-watchlist-add-blocking` 参照）。`ctx` を足すのはDB側の `PriceRepository` のみ。
- **`internal/domain/anomaly/service.go` を変更してはならない。** 価格水準ベースのZ-score計算は現状維持。本PRは振る舞い不変。
- `daily_prices` の主キーは `(stock_code, date)` の複合自然キー。CLAUDE.md の「UUID PK」規約に対する**意図的な逸脱**であり、冪等なUPSERTと日付レンジ走査のために必要。移行時にこの理由をCLAUDE.mdへ明記する。
- バックフィル日数は `backfillDays = 500`（約2年の営業日数）、監視ウィンドウは `historySize = 30`（変更なし）。
- 各タスク終了時点で `go build ./...` と `go test -race -short ./...` が必ず green であること。

---

## File Structure

| ファイル | 責務 |
|---|---|
| `go-api/migrations/003_create_daily_prices.sql` | `daily_prices` テーブル定義（新規） |
| `go-api/internal/domain/stock/price_repository.go` | 日次終値の永続化インターフェース（新規） |
| `go-api/internal/domain/stock/price_cache.go` | 旧インターフェース（Task 4で**削除**） |
| `go-api/internal/infrastructure/persistence/price_repository.go` | pgxpool実装（新規） |
| `go-api/internal/infrastructure/persistence/price_repository_test.go` | 統合テスト（新規） |
| `go-api/internal/infrastructure/cache/` | Redis実装パッケージ（Task 4で**ディレクトリごと削除**） |
| `go-api/internal/interface/gateway/yahoo_finance_client.go` | `FetchHistory` の取得レンジを必要日数から自動選択（変更） |
| `go-api/internal/usecase/backfill_price_history.go` | `PriceRepository` へ切替 + `RunAll` 追加（変更） |
| `go-api/internal/usecase/monitor_stocks.go` | `PriceRepository` へ切替（変更） |
| `go-api/internal/usecase/manage_watchlist.go` | `backfiller.Run(ctx, code)` へ変更（変更） |
| `go-api/internal/usecase/mock_price_repository_test.go` | `usecase_test` パッケージ用モック（新規） |
| `go-api/cmd/api/main.go` | 配線変更・Redis撤去・起動時バックフィル起動（変更） |
| `docker-compose.yml` / `.github/workflows/go-test.yml` / `.env.example` / `CLAUDE.md` / `docs/deployment/phase5-railway-vercel-setup.md` | Redis撤去・マイグレーション003追記（変更） |

---

### Task 1: `daily_prices` テーブルと `PgPriceRepository`

**Files:**
- Create: `go-api/migrations/003_create_daily_prices.sql`
- Create: `go-api/internal/domain/stock/price_repository.go`
- Create: `go-api/internal/infrastructure/persistence/price_repository.go`
- Test: `go-api/internal/infrastructure/persistence/price_repository_test.go`
- Modify: `.github/workflows/go-test.yml:52-57`（マイグレーションステップに003を追加）

**Interfaces:**
- Consumes: `stock.StockCode`, `stock.Price`, `stock.Quote`（`internal/domain/stock/value_object.go`）、`persistence.Connect(ctx, databaseURL) (*pgxpool.Pool, error)`（`internal/infrastructure/persistence/db.go`）
- Produces:
  - `stock.PriceRepository` インターフェース: `Save(ctx, code, quote) error` / `SaveAll(ctx, code, quotes) error` / `FindRecent(ctx, code, n) ([]stock.Quote, error)` / `LatestDate(ctx, code) (string, error)`
  - `persistence.NewPgPriceRepository(conn *pgxpool.Pool) *persistence.PgPriceRepository`
  - `FindRecent` は**古い順（日付昇順）**で最大n件を返す。0件なら空スライス（nilではない）。
  - `LatestDate` は0件のとき `("", nil)` を返す（エラーにしない）。

- [ ] **Step 1: マイグレーションSQLを作成する**

`go-api/migrations/003_create_daily_prices.sql`:

```sql
-- 日次終値の単一の正となるストア。チャート描画・統計的な予測帯の算出・
-- 異常検知の履歴ウィンドウのすべてがこのテーブルを参照する。
--
-- 主キーを (stock_code, date) の複合自然キーにしている理由:
--   1. 同一銘柄・同一取引日の重複投入を DB 制約で弾きたい（ON CONFLICT DO NOTHING による冪等なバックフィル）
--   2. 「銘柄を指定して日付降順に直近n件」がこの主キーインデックスだけで完結する
-- 他テーブルの UUID PK 規約に対する意図的な逸脱。
CREATE TABLE IF NOT EXISTS daily_prices (
    stock_code TEXT        NOT NULL,
    date       DATE        NOT NULL,
    close      NUMERIC     NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (stock_code, date)
);
```

- [ ] **Step 2: ローカルのPostgresへマイグレーションを適用する**

`docker-compose.yml` の初期化スクリプトは既存ボリュームには再適用されないため、手動で当てる。

```bash
docker compose up -d postgres
docker compose exec -T postgres psql -U postgres -d stock_anomaly -f /docker-entrypoint-initdb.d/003_create_daily_prices.sql
```

期待: `CREATE TABLE`

- [ ] **Step 3: CIのマイグレーションステップに003を追加する**

`.github/workflows/go-test.yml` の "Run database migration" ステップを以下に変更する（Task 1の統合テストがCIで通るために必須）:

```yaml
      - name: Run database migration
        run: |
          psql $DATABASE_URL -f go-api/migrations/001_initial_schema.sql
          psql $DATABASE_URL -f go-api/migrations/002_add_watchlist_stock_name.sql
          psql $DATABASE_URL -f go-api/migrations/003_create_daily_prices.sql
        env:
          DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable
```

- [ ] **Step 4: domainインターフェースを作成する**

`go-api/internal/domain/stock/price_repository.go`:

```go
package stock

import "context"

// PriceRepository は日次終値の永続化を担う。Quote.Date は "YYYY-MM-DD"（JST基準）。
//
// 旧 PriceCache と違い日付を保持するため、SetLastDate/LastDate の二重管理が不要になり、
// 「同一取引日を二重に積んで標準偏差を歪める」事故を DB の主キー制約で防げる。
// DB アクセスは呼び出し元のキャンセルに追従させたいので ctx を取る。
type PriceRepository interface {
	// Save は1件の終値を保存する。同一 (code, quote.Date) が既にあれば何もしない。
	Save(ctx context.Context, code StockCode, quote Quote) error
	// SaveAll は複数の終値をまとめて保存する。既存分は無視する（冪等）。
	SaveAll(ctx context.Context, code StockCode, quotes []Quote) error
	// FindRecent は直近n件の終値を日付の古い順で返す。該当なしなら空スライス。
	FindRecent(ctx context.Context, code StockCode, n int) ([]Quote, error)
	// LatestDate は保存済みの最新取引日を返す。1件も無ければ空文字を返す（エラーにしない）。
	LatestDate(ctx context.Context, code StockCode) (string, error)
}
```

- [ ] **Step 5: 失敗する統合テストを書く**

`go-api/internal/infrastructure/persistence/price_repository_test.go`:

```go
package persistence_test

import (
	"context"
	"os"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newPriceRepoForTest は daily_prices を空にした上でリポジトリを返す。
// 戻り値の cleanup は必ず defer で呼ぶこと。
func newPriceRepoForTest(t *testing.T) (*persistence.PgPriceRepository, context.Context, func()) {
	t.Helper()
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
	_, err = conn.Exec(ctx, "TRUNCATE daily_prices")
	require.NoError(t, err)
	return persistence.NewPgPriceRepository(conn), ctx, conn.Close
}

func TestPgPriceRepository_SaveAndFindRecent(t *testing.T) {
	repo, ctx, cleanup := newPriceRepoForTest(t)
	defer cleanup()

	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	require.NoError(t, repo.Save(ctx, code, stock.Quote{Price: 3200.0, Date: "2026-07-06"}))
	require.NoError(t, repo.Save(ctx, code, stock.Quote{Price: 3250.0, Date: "2026-07-07"}))

	quotes, err := repo.FindRecent(ctx, code, 10)
	require.NoError(t, err)
	require.Len(t, quotes, 2)
	assert.Equal(t, stock.Quote{Price: 3200.0, Date: "2026-07-06"}, quotes[0])
	assert.Equal(t, stock.Quote{Price: 3250.0, Date: "2026-07-07"}, quotes[1])
}

func TestPgPriceRepository_Save_SameDateIsIdempotent(t *testing.T) {
	repo, ctx, cleanup := newPriceRepoForTest(t)
	defer cleanup()

	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	require.NoError(t, repo.Save(ctx, code, stock.Quote{Price: 3200.0, Date: "2026-07-06"}))
	// 同じ取引日を再投入しても行は増えず、先に入った値が残る
	require.NoError(t, repo.Save(ctx, code, stock.Quote{Price: 9999.0, Date: "2026-07-06"}))

	quotes, err := repo.FindRecent(ctx, code, 10)
	require.NoError(t, err)
	require.Len(t, quotes, 1)
	assert.Equal(t, stock.Price(3200.0), quotes[0].Price)
}

func TestPgPriceRepository_SaveAll_IsIdempotent(t *testing.T) {
	repo, ctx, cleanup := newPriceRepoForTest(t)
	defer cleanup()

	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	quotes := []stock.Quote{
		{Price: 3200.0, Date: "2026-07-06"},
		{Price: 3250.0, Date: "2026-07-07"},
		{Price: 3300.0, Date: "2026-07-08"},
	}
	require.NoError(t, repo.SaveAll(ctx, code, quotes))
	require.NoError(t, repo.SaveAll(ctx, code, quotes))

	found, err := repo.FindRecent(ctx, code, 100)
	require.NoError(t, err)
	assert.Len(t, found, 3)
}

func TestPgPriceRepository_SaveAll_Empty(t *testing.T) {
	repo, ctx, cleanup := newPriceRepoForTest(t)
	defer cleanup()

	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	require.NoError(t, repo.SaveAll(ctx, code, nil))
	found, err := repo.FindRecent(ctx, code, 10)
	require.NoError(t, err)
	assert.Empty(t, found)
}

func TestPgPriceRepository_FindRecent_ReturnsNewestNInAscendingOrder(t *testing.T) {
	repo, ctx, cleanup := newPriceRepoForTest(t)
	defer cleanup()

	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	require.NoError(t, repo.SaveAll(ctx, code, []stock.Quote{
		{Price: 100.0, Date: "2026-07-01"},
		{Price: 200.0, Date: "2026-07-02"},
		{Price: 300.0, Date: "2026-07-03"},
		{Price: 400.0, Date: "2026-07-06"},
	}))

	quotes, err := repo.FindRecent(ctx, code, 2)
	require.NoError(t, err)
	require.Len(t, quotes, 2)
	// 新しい方から2件を取り、並びは古い順に戻して返す
	assert.Equal(t, "2026-07-03", quotes[0].Date)
	assert.Equal(t, "2026-07-06", quotes[1].Date)
}

func TestPgPriceRepository_FindRecent_IsolatesByStockCode(t *testing.T) {
	repo, ctx, cleanup := newPriceRepoForTest(t)
	defer cleanup()

	code7203, err := stock.NewStockCode("7203")
	require.NoError(t, err)
	code9984, err := stock.NewStockCode("9984")
	require.NoError(t, err)

	require.NoError(t, repo.Save(ctx, code7203, stock.Quote{Price: 3200.0, Date: "2026-07-06"}))
	require.NoError(t, repo.Save(ctx, code9984, stock.Quote{Price: 8000.0, Date: "2026-07-06"}))

	quotes, err := repo.FindRecent(ctx, code9984, 10)
	require.NoError(t, err)
	require.Len(t, quotes, 1)
	assert.Equal(t, stock.Price(8000.0), quotes[0].Price)
}

func TestPgPriceRepository_FindRecent_EmptyReturnsEmptySlice(t *testing.T) {
	repo, ctx, cleanup := newPriceRepoForTest(t)
	defer cleanup()

	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	quotes, err := repo.FindRecent(ctx, code, 30)
	require.NoError(t, err)
	assert.NotNil(t, quotes)
	assert.Empty(t, quotes)
}

func TestPgPriceRepository_LatestDate(t *testing.T) {
	repo, ctx, cleanup := newPriceRepoForTest(t)
	defer cleanup()

	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	require.NoError(t, repo.SaveAll(ctx, code, []stock.Quote{
		{Price: 100.0, Date: "2026-07-01"},
		{Price: 400.0, Date: "2026-07-06"},
		{Price: 200.0, Date: "2026-07-02"},
	}))

	latest, err := repo.LatestDate(ctx, code)
	require.NoError(t, err)
	assert.Equal(t, "2026-07-06", latest)
}

func TestPgPriceRepository_LatestDate_EmptyReturnsEmptyString(t *testing.T) {
	repo, ctx, cleanup := newPriceRepoForTest(t)
	defer cleanup()

	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	latest, err := repo.LatestDate(ctx, code)
	require.NoError(t, err)
	assert.Equal(t, "", latest)
}

func TestPgPriceRepository_Save_InvalidDateFormat(t *testing.T) {
	repo, ctx, cleanup := newPriceRepoForTest(t)
	defer cleanup()

	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	err = repo.Save(ctx, code, stock.Quote{Price: 3200.0, Date: "2026/07/06"})
	require.Error(t, err)
}
```

- [ ] **Step 6: テストを実行して失敗を確認する**

```bash
cd go-api && DATABASE_URL="postgres://postgres:postgres@localhost:5432/stock_anomaly?sslmode=disable" go test -race ./internal/infrastructure/persistence/ -run TestPgPriceRepository -v
```

期待: コンパイルエラー `undefined: persistence.NewPgPriceRepository`

- [ ] **Step 7: 実装を書く**

`go-api/internal/infrastructure/persistence/price_repository.go`:

```go
package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

// dateLayout は Quote.Date の文字列表現。Yahoo Finance クライアントが
// JST で組み立てるフォーマットと一致させる。
const dateLayout = "2006-01-02"

type PgPriceRepository struct {
	conn *pgxpool.Pool
}

func NewPgPriceRepository(conn *pgxpool.Pool) *PgPriceRepository {
	return &PgPriceRepository{conn: conn}
}

// insertPriceSQL は同一 (stock_code, date) の再投入を黙って捨てる。
// バックフィルと日次ポーリングが同じ取引日を取りに行っても行が重複しないようにするため。
const insertPriceSQL = `INSERT INTO daily_prices (stock_code, date, close)
VALUES ($1, $2, $3)
ON CONFLICT (stock_code, date) DO NOTHING`

func (r *PgPriceRepository) Save(ctx context.Context, code stock.StockCode, quote stock.Quote) error {
	d, err := time.Parse(dateLayout, quote.Date)
	if err != nil {
		return fmt.Errorf("parse date %q: %w", quote.Date, err)
	}
	_, err = r.conn.Exec(ctx, insertPriceSQL, code.String(), d, float64(quote.Price))
	if err != nil {
		return fmt.Errorf("save price %s: %w", code, err)
	}
	return nil
}

// SaveAll は pgx.Batch で1往復にまとめて投入する。2年分（約500件）を1件ずつ
// Exec すると往復回数がそのままレイテンシになるため。
func (r *PgPriceRepository) SaveAll(ctx context.Context, code stock.StockCode, quotes []stock.Quote) error {
	if len(quotes) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, q := range quotes {
		d, err := time.Parse(dateLayout, q.Date)
		if err != nil {
			return fmt.Errorf("parse date %q: %w", q.Date, err)
		}
		batch.Queue(insertPriceSQL, code.String(), d, float64(q.Price))
	}

	br := r.conn.SendBatch(ctx, batch)
	for range quotes {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return fmt.Errorf("save prices %s: %w", code, err)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("save prices %s: %w", code, err)
	}
	return nil
}

// FindRecent は日付降順でn件取ってから昇順に並べ直して返す。
// 呼び出し元（Z-score計算・チャート描画）はいずれも古い順の配列を前提にしている。
func (r *PgPriceRepository) FindRecent(ctx context.Context, code stock.StockCode, n int) ([]stock.Quote, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT date, close FROM (
		     SELECT date, close FROM daily_prices
		     WHERE stock_code = $1
		     ORDER BY date DESC
		     LIMIT $2
		 ) AS recent
		 ORDER BY date ASC`,
		code.String(), n)
	if err != nil {
		return nil, fmt.Errorf("find recent prices %s: %w", code, err)
	}
	defer rows.Close()

	quotes := []stock.Quote{}
	for rows.Next() {
		var d time.Time
		var closePrice float64
		if err := rows.Scan(&d, &closePrice); err != nil {
			return nil, fmt.Errorf("scan price %s: %w", code, err)
		}
		quotes = append(quotes, stock.Quote{Price: stock.Price(closePrice), Date: d.Format(dateLayout)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("find recent prices %s: %w", code, err)
	}
	return quotes, nil
}

// LatestDate は1件も無い場合を「まだ取り込んでいない」という正常状態として扱い、
// 空文字を返す。呼び出し元は空文字と実際の取引日を単純比較するだけで済む。
func (r *PgPriceRepository) LatestDate(ctx context.Context, code stock.StockCode) (string, error) {
	var d time.Time
	err := r.conn.QueryRow(ctx,
		`SELECT date FROM daily_prices WHERE stock_code = $1 ORDER BY date DESC LIMIT 1`,
		code.String()).Scan(&d)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("latest date %s: %w", code, err)
	}
	return d.Format(dateLayout), nil
}
```

- [ ] **Step 8: テストを実行して通ることを確認する**

```bash
cd go-api && DATABASE_URL="postgres://postgres:postgres@localhost:5432/stock_anomaly?sslmode=disable" go test -race ./internal/infrastructure/persistence/ -run TestPgPriceRepository -v
go build ./...
go test -race -short ./...
```

期待: すべて PASS

- [ ] **Step 9: コンパイル時にインターフェースを満たしていることを確認する**

`go-api/internal/infrastructure/persistence/price_repository.go` の `NewPgPriceRepository` の直前に以下を追加する:

```go
// コンパイル時にインターフェース適合を保証する。
var _ stock.PriceRepository = (*PgPriceRepository)(nil)
```

```bash
cd go-api && go build ./...
```

期待: 成功

- [ ] **Step 10: コミット**

```bash
git add go-api/migrations/003_create_daily_prices.sql \
        go-api/internal/domain/stock/price_repository.go \
        go-api/internal/infrastructure/persistence/price_repository.go \
        go-api/internal/infrastructure/persistence/price_repository_test.go \
        .github/workflows/go-test.yml
git commit -m "$(cat <<'EOF'
feat: add daily_prices table and PgPriceRepository

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: `FetchHistory` の取得レンジを必要日数から自動選択する

**Files:**
- Modify: `go-api/internal/interface/gateway/yahoo_finance_client.go:44-73`
- Test: `go-api/internal/interface/gateway/yahoo_finance_client_test.go`（末尾に追記）

**Interfaces:**
- Consumes: `stock.PriceFetcher.FetchHistory(code StockCode, days int) ([]Quote, error)`（**シグネチャは変更しない**）
- Produces: `FetchHistory` が `days` に応じて Yahoo の `range` を `3mo` / `1y` / `2y` から選ぶようになる。`days <= 60` → `3mo`、`days <= 250` → `1y`、それ以上 → `2y`。既存の呼び出し（`days=30`）の挙動は変わらない。

- [ ] **Step 1: 失敗するテストを書く**

`go-api/internal/interface/gateway/yahoo_finance_client_test.go` の末尾に追記する:

```go
func TestYahooFinanceClient_FetchHistory_SelectsRangeFromRequestedDays(t *testing.T) {
	tests := []struct {
		name          string
		days          int
		expectedRange string
	}{
		{name: "30営業日は3mo", days: 30, expectedRange: "3mo"},
		{name: "境界の60営業日は3mo", days: 60, expectedRange: "3mo"},
		{name: "61営業日は1y", days: 61, expectedRange: "1y"},
		{name: "境界の250営業日は1y", days: 250, expectedRange: "1y"},
		{name: "251営業日は2y", days: 251, expectedRange: "2y"},
		{name: "500営業日は2y", days: 500, expectedRange: "2y"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /v8/finance/chart/7203.T", func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.expectedRange, r.URL.Query().Get("range"))
				assert.Equal(t, "1d", r.URL.Query().Get("interval"))

				resp := map[string]any{
					"chart": map[string]any{
						"result": []map[string]any{
							{
								"timestamp": []int64{1783317600},
								"indicators": map[string]any{
									"quote": []map[string]any{
										{"close": []any{3200.0}},
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

			_, err := client.FetchHistory(code, tt.days)
			require.NoError(t, err)
		})
	}
}
```

- [ ] **Step 2: テストを実行して失敗を確認する**

```bash
cd go-api && go test -race -short ./internal/interface/gateway/ -run TestYahooFinanceClient_FetchHistory_SelectsRangeFromRequestedDays -v
```

期待: `days=61` 以降のサブテストが FAIL（`"1y"` を期待したが `"3mo"` が来る）

- [ ] **Step 3: 実装を書く**

`go-api/internal/interface/gateway/yahoo_finance_client.go` の `FetchHistory` のコメントとレンジ指定を差し替え、直後に `historyRange` を追加する:

```go
// FetchHistory は必要日数（days、トレーディングデイ換算）をカバーする期間の
// 日次終値を取得し、null（未確定値）および当日（JST）分を除いた上で
// 直近days件に絞って古い順で返す。
// 上場から日が浅い銘柄など実件数がdaysに満たない場合はそのまま全件を返す。
//
// 当日分を除外する理由: chart APIのclose値は取引時間中も逐次更新されるため、
// 当日分がnullでなくても、それは確定した終値ではなく取引中の速報値である
// 可能性がある。当日の確定終値は、その日の取引が引けた後に通常の16:00ポーリング
// （FetchLatest）が正しく取得するため、ここで速報値を混入させる必要はない。
func (c *YahooFinanceClient) FetchHistory(code stock.StockCode, days int) ([]stock.Quote, error) {
	timestamps, closes, _, err := c.fetchChart(code, historyRange(days))
	if err != nil {
		return nil, err
	}
	today := time.Now().In(yahooJST).Format("2006-01-02")
	quotes := make([]stock.Quote, 0, len(closes))
	for i, close := range closes {
		if close == nil {
			continue
		}
		date := time.Unix(timestamps[i], 0).In(yahooJST).Format("2006-01-02")
		if date == today {
			continue
		}
		quotes = append(quotes, stock.Quote{Price: stock.Price(*close), Date: date})
	}
	if len(quotes) > days {
		quotes = quotes[len(quotes)-days:]
	}
	return quotes, nil
}

// historyRange は必要な営業日数を満たす最小のYahoo chart APIレンジを選ぶ。
// 日本市場の営業日は年約245日なので、3mo≒60日・1y≒245日・2y≒490日として閾値を置く。
// 必要以上に長いレンジを常用するとレスポンスサイズとパース時間が無駄に増えるため、
// 用途（監視ウィンドウ30件 / チャート用2年）に応じて切り替える。
func historyRange(days int) string {
	switch {
	case days <= 60:
		return "3mo"
	case days <= 250:
		return "1y"
	default:
		return "2y"
	}
}
```

- [ ] **Step 4: テストを実行して通ることを確認する**

```bash
cd go-api && go test -race -short ./internal/interface/gateway/ -v
```

期待: 新規テストを含めすべて PASS（既存の `FetchHistory` テストは `days<=60` なので `3mo` のまま）

- [ ] **Step 5: コミット**

```bash
git add go-api/internal/interface/gateway/yahoo_finance_client.go \
        go-api/internal/interface/gateway/yahoo_finance_client_test.go
git commit -m "$(cat <<'EOF'
feat: select Yahoo chart range from requested history days

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 3: バックフィルを `PriceRepository`（2年分）へ切り替える

**Files:**
- Modify: `go-api/internal/usecase/backfill_price_history.go`（全面書き換え）
- Modify: `go-api/internal/usecase/manage_watchlist.go:60-64`
- Create: `go-api/internal/usecase/mock_price_repository_test.go`
- Test: `go-api/internal/usecase/backfill_price_history_test.go`（全面書き換え）
- Test: `go-api/internal/usecase/manage_watchlist_test.go:124-163`
- Modify: `go-api/cmd/api/main.go:78-88`

**Interfaces:**
- Consumes: `stock.PriceRepository`（Task 1）、`historyRange` による2年取得（Task 2）、`persistence.NewPgPriceRepository`（Task 1）
- Produces:
  - `usecase.NewBackfillPriceHistoryUsecase(fetcher stock.PriceFetcher, prices stock.PriceRepository) *BackfillPriceHistoryUsecase`（第2引数の型が変わる）
  - `(*BackfillPriceHistoryUsecase).Run(ctx context.Context, code stock.StockCode) error`（`ctx` が増える）
  - `usecase.backfillDays = 500`（パッケージ内定数）
  - `usecase_test` パッケージ用の `MockPriceRepository`（`Save` / `SaveAll` / `FindRecent` / `LatestDate`）
- 注意: このタスクの時点では `main.go` は Redis と Postgres の**両方**を構築する（monitor はまだ `priceCache` を使う）。Task 4 で Redis 側を消す。

- [ ] **Step 1: `usecase_test` 用のモックを作成する**

`go-api/internal/usecase/mock_price_repository_test.go`:

```go
package usecase_test

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stretchr/testify/mock"
)

// MockPriceRepository は stock.PriceRepository のモック実装。
type MockPriceRepository struct{ mock.Mock }

func (m *MockPriceRepository) Save(ctx context.Context, code stock.StockCode, quote stock.Quote) error {
	return m.Called(ctx, code, quote).Error(0)
}

func (m *MockPriceRepository) SaveAll(ctx context.Context, code stock.StockCode, quotes []stock.Quote) error {
	return m.Called(ctx, code, quotes).Error(0)
}

func (m *MockPriceRepository) FindRecent(ctx context.Context, code stock.StockCode, n int) ([]stock.Quote, error) {
	args := m.Called(ctx, code, n)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]stock.Quote), args.Error(1)
}

func (m *MockPriceRepository) LatestDate(ctx context.Context, code stock.StockCode) (string, error) {
	args := m.Called(ctx, code)
	return args.String(0), args.Error(1)
}
```

- [ ] **Step 2: 失敗するテストを書く**

`go-api/internal/usecase/backfill_price_history_test.go` を以下で**全面置換**する:

```go
package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBackfillPriceHistoryUsecase_Run_SavesHistoryWhenStoreEmpty(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	code, _ := stock.NewStockCode("7203")
	ctx := context.Background()

	quotes := []stock.Quote{
		{Price: 3200.0, Date: "2026-07-06"},
		{Price: 3250.0, Date: "2026-07-07"},
		{Price: 3300.0, Date: "2026-07-08"},
	}
	prices.On("FindRecent", ctx, code, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code, 500).Return(quotes, nil)
	prices.On("SaveAll", ctx, code, quotes).Return(nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	err := uc.Run(ctx, code)

	require.NoError(t, err)
	prices.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestBackfillPriceHistoryUsecase_Run_SkipsWhenHistoryAlreadyExists(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	code, _ := stock.NewStockCode("7203")
	ctx := context.Background()

	prices.On("FindRecent", ctx, code, 1).Return([]stock.Quote{{Price: 3300.0, Date: "2026-07-08"}}, nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	err := uc.Run(ctx, code)

	require.NoError(t, err)
	fetcher.AssertNotCalled(t, "FetchHistory", mock.Anything, mock.Anything)
	prices.AssertNotCalled(t, "SaveAll", mock.Anything, mock.Anything, mock.Anything)
}

func TestBackfillPriceHistoryUsecase_Run_PropagatesFetchError(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	code, _ := stock.NewStockCode("7203")
	ctx := context.Background()

	prices.On("FindRecent", ctx, code, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code, 500).Return([]stock.Quote{}, errors.New("fetch failed"))

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	err := uc.Run(ctx, code)

	require.Error(t, err)
	prices.AssertNotCalled(t, "SaveAll", mock.Anything, mock.Anything, mock.Anything)
}

func TestBackfillPriceHistoryUsecase_Run_PropagatesSaveError(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	code, _ := stock.NewStockCode("7203")
	ctx := context.Background()

	quotes := []stock.Quote{{Price: 3200.0, Date: "2026-07-06"}}
	prices.On("FindRecent", ctx, code, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code, 500).Return(quotes, nil)
	prices.On("SaveAll", ctx, code, quotes).Return(errors.New("db down"))

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	err := uc.Run(ctx, code)

	require.Error(t, err)
}
```

`go-api/internal/usecase/manage_watchlist_test.go` の `TestManageWatchlistUsecase_Add_TriggersBackfill` と `TestManageWatchlistUsecase_Add_SucceedsEvenIfBackfillFails` を以下で置換する:

```go
func TestManageWatchlistUsecase_Add_TriggersBackfill(t *testing.T) {
	repo := new(mockWatchlistRepository)
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	code, _ := stock.NewStockCode("7203")

	quotes := []stock.Quote{{Price: 3200.0, Date: "2026-07-06"}}
	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 3.0}, nil)
	prices.On("FindRecent", mock.Anything, code, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code, 500).Return(quotes, nil)
	prices.On("SaveAll", mock.Anything, code, quotes).Return(nil)

	backfiller := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	uc := usecase.NewManageWatchlistUsecase(repo, backfiller, nil)
	_, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	prices.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestManageWatchlistUsecase_Add_SucceedsEvenIfBackfillFails(t *testing.T) {
	repo := new(mockWatchlistRepository)
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	code, _ := stock.NewStockCode("7203")

	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 3.0}, nil)
	prices.On("FindRecent", mock.Anything, code, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code, 500).Return([]stock.Quote{}, errors.New("yahoo finance unavailable"))

	backfiller := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	uc := usecase.NewManageWatchlistUsecase(repo, backfiller, nil)
	result, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	require.Equal(t, "wl-1", result.ID)
}
```

- [ ] **Step 3: テストを実行して失敗を確認する**

```bash
cd go-api && go test -race -short ./internal/usecase/ 2>&1 | head -20
```

期待: コンパイルエラー（`NewBackfillPriceHistoryUsecase` の引数型不一致、`Run` の引数個数不一致）

- [ ] **Step 4: バックフィルユースケースを書き換える**

`go-api/internal/usecase/backfill_price_history.go` を以下で**全面置換**する:

```go
package usecase

import (
	"context"
	"fmt"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

// backfillDays はバックフィルで取得する営業日数。約2年分。
// 監視に必要なのは historySize（30件）だけだが、チャート表示と
// ボラティリティ推定に十分な長さの履歴を最初の1回で揃えておく。
const backfillDays = 500

// BackfillPriceHistoryUsecase は新規watchlist追加時に、過去の値動きを
// daily_prices へ一括投入する。異常検知が有効になるまでのウォームアップ期間
// （historySize件分の日次ポーリング待ち）を短縮し、同時にチャート描画に
// 必要な長期履歴も用意する。
type BackfillPriceHistoryUsecase struct {
	fetcher stock.PriceFetcher
	prices  stock.PriceRepository
}

func NewBackfillPriceHistoryUsecase(fetcher stock.PriceFetcher, prices stock.PriceRepository) *BackfillPriceHistoryUsecase {
	return &BackfillPriceHistoryUsecase{fetcher: fetcher, prices: prices}
}

// Run はcodeの価格履歴が空の場合のみ過去backfillDays件を取得して保存する。
// 既に履歴が存在する場合（同一銘柄を別ユーザーが既に監視中）は何もしない。
func (u *BackfillPriceHistoryUsecase) Run(ctx context.Context, code stock.StockCode) error {
	existing, err := u.prices.FindRecent(ctx, code, 1)
	if err != nil {
		return fmt.Errorf("check existing history %s: %w", code, err)
	}
	if len(existing) > 0 {
		return nil
	}

	quotes, err := u.fetcher.FetchHistory(code, backfillDays)
	if err != nil {
		return fmt.Errorf("fetch history %s: %w", code, err)
	}
	if err := u.prices.SaveAll(ctx, code, quotes); err != nil {
		return fmt.Errorf("save history %s: %w", code, err)
	}
	return nil
}
```

- [ ] **Step 5: 呼び出し元に `ctx` を渡す**

`go-api/internal/usecase/manage_watchlist.go` の `Add` 内のバックフィル呼び出しを変更する:

```go
	if u.backfiller != nil {
		if err := u.backfiller.Run(ctx, code); err != nil {
			log.Printf("ERROR backfill price history %s: %v", code, err)
		}
	}
```

- [ ] **Step 6: `main.go` の配線を更新する**

`go-api/cmd/api/main.go`、`priceCache := cache.NewRedisPriceCache(redisClient)` の直後に `priceRepo` を追加し、`backfillUsecase` の引数を差し替える。monitor は**このタスクではまだ `priceCache` のまま**にする（Task 4 で切り替える）:

```go
	priceCache := cache.NewRedisPriceCache(redisClient)
	priceRepo := persistence.NewPgPriceRepository(pool)
	priceFetcher := gateway.NewYahooFinanceClient()
```

```go
	monitor := usecase.NewMonitorUsecase(priceFetcher, priceCache, detector, threshold, notifyUsecase)
	backfillUsecase := usecase.NewBackfillPriceHistoryUsecase(priceFetcher, priceRepo)
```

- [ ] **Step 7: テストを実行して通ることを確認する**

```bash
cd go-api && go build ./... && go test -race -short ./...
```

期待: すべて PASS

- [ ] **Step 8: コミット**

```bash
git add go-api/internal/usecase/backfill_price_history.go \
        go-api/internal/usecase/backfill_price_history_test.go \
        go-api/internal/usecase/mock_price_repository_test.go \
        go-api/internal/usecase/manage_watchlist.go \
        go-api/internal/usecase/manage_watchlist_test.go \
        go-api/cmd/api/main.go
git commit -m "$(cat <<'EOF'
feat: back fill two years of prices into daily_prices

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 4: 監視ループを `PriceRepository` へ切り替え、Redisを撤去する

**Files:**
- Modify: `go-api/internal/usecase/monitor_stocks.go:30-122`
- Test: `go-api/internal/usecase/monitor_stocks_test.go`（`MockPriceCache` 型を削除し3テストを書き換え）
- Test: `go-api/internal/usecase/monitor_notify_test.go:58-80,82-201`（モックとテストを書き換え）
- Delete: `go-api/internal/domain/stock/price_cache.go`
- Delete: `go-api/internal/infrastructure/cache/`（ディレクトリごと）
- Modify: `go-api/cmd/api/main.go`（Redis撤去）
- Modify: `go-api/go.mod` / `go-api/go.sum`（`go mod tidy`）

**Interfaces:**
- Consumes: `stock.PriceRepository`（Task 1）、`MockPriceRepository`（Task 3、`usecase_test` パッケージ）
- Produces:
  - `usecase.NewMonitorUsecase(fetcher stock.PriceFetcher, prices stock.PriceRepository, detector *anomaly.DetectionService, threshold float64, notifyUsecase *AnalyzeAndNotifyUsecase) *MonitorUsecase`（第2引数の型が変わる）
  - `CheckStock` / `StartMonitoring` / `RunWithDynamicWatchlist` のシグネチャは**変わらない**
  - `stock.PriceCache` と `cache` パッケージが存在しなくなる
- 振る舞い上の唯一の差分: 旧実装は「日付が変わっていれば `Push` → `SetLastDate`」だったが、新実装は `Save` の `ON CONFLICT DO NOTHING` により重複投入が DB 側でも弾かれる（二重防御になる）。

- [ ] **Step 1: 失敗するテストを書く（`usecase_test` パッケージ側）**

`go-api/internal/usecase/monitor_stocks_test.go` から `MockPriceCache` 型定義（4メソッド）を**削除**し、3つのテストを以下で置換する:

```go
func TestMonitorUsecase_CheckStock_DetectsAnomaly(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	ctx := context.Background()

	code, _ := stock.NewStockCode("7203")

	// history: 29件 交互 90/110（mean≈99.65, stddev≈10）
	// current: 130（z≈3.0 → anomaly）
	allQuotes := make([]stock.Quote, 0, 30)
	for i := 0; i < 29; i++ {
		p := stock.Price(110)
		if i%2 == 0 {
			p = stock.Price(90)
		}
		allQuotes = append(allQuotes, stock.Quote{Price: p, Date: "2026-06-01"})
	}
	allQuotes = append(allQuotes, stock.Quote{Price: 130, Date: "2026-07-07"})

	fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 130.0, Date: "2026-07-07"}, nil)
	prices.On("LatestDate", ctx, code).Return("2026-07-06", nil)
	prices.On("Save", ctx, code, stock.Quote{Price: 130.0, Date: "2026-07-07"}).Return(nil)
	prices.On("FindRecent", ctx, code, 30).Return(allQuotes, nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, prices, svc, 2.5, nil)

	detected, zScore, err := uc.CheckStock(ctx, code)
	require.NoError(t, err)
	assert.True(t, detected, "expected anomaly detection")
	assert.True(t, zScore.IsAnomaly(2.5), "z=%.3f should be anomaly", zScore)

	fetcher.AssertExpectations(t)
	prices.AssertExpectations(t)
}

func TestMonitorUsecase_CheckStock_InsufficientHistory(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	ctx := context.Background()

	code, _ := stock.NewStockCode("6758")
	fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 5000.0, Date: "2026-07-07"}, nil)
	prices.On("LatestDate", ctx, code).Return("2026-07-06", nil)
	prices.On("Save", ctx, code, stock.Quote{Price: 5000.0, Date: "2026-07-07"}).Return(nil)
	// 30件未満（5件）を返す → 検知しない
	prices.On("FindRecent", ctx, code, 30).Return([]stock.Quote{
		{Price: 5000, Date: "2026-07-01"},
		{Price: 5010, Date: "2026-07-02"},
		{Price: 4990, Date: "2026-07-03"},
		{Price: 5005, Date: "2026-07-06"},
		{Price: 5000, Date: "2026-07-07"},
	}, nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, prices, svc, 2.5, nil)

	detected, _, err := uc.CheckStock(ctx, code)
	require.NoError(t, err)
	assert.False(t, detected, "insufficient history should not trigger detection")
}

func TestMonitorUsecase_CheckStock_SkipsDuplicateDate(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	ctx := context.Background()

	code, _ := stock.NewStockCode("7203")
	// 取得したバーの取引日が、保存済みの最新取引日と同じ
	fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 3250.0, Date: "2026-07-07"}, nil)
	prices.On("LatestDate", ctx, code).Return("2026-07-07", nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, prices, svc, 2.5, nil)

	detected, _, err := uc.CheckStock(ctx, code)
	require.NoError(t, err)
	assert.False(t, detected, "duplicate trading date must not be saved")

	// Save / FindRecent は呼ばれない
	prices.AssertNotCalled(t, "Save", mock.Anything, mock.Anything, mock.Anything)
	prices.AssertNotCalled(t, "FindRecent", mock.Anything, mock.Anything, mock.Anything)
	fetcher.AssertExpectations(t)
}
```

`monitor_stocks_test.go` の import に `"github.com/stretchr/testify/mock"` が残っていることを確認する（`MockPriceFetcher` の定義と `AssertNotCalled` で使う）。

- [ ] **Step 2: 失敗するテストを書く（`usecase` パッケージ側）**

`go-api/internal/usecase/monitor_notify_test.go` の `MockPriceCacheForNotifyTest` 型（58-80行）を以下で置換する:

```go
// MockPriceRepositoryForNotifyTest は stock.PriceRepository のモック実装（notify用）。
// monitor_notify_test.go は内部テストパッケージ（package usecase）のため、
// usecase_test 側の MockPriceRepository を使い回せず別途定義している。
type MockPriceRepositoryForNotifyTest struct{ mock.Mock }

func (m *MockPriceRepositoryForNotifyTest) Save(ctx context.Context, code stock.StockCode, quote stock.Quote) error {
	return m.Called(ctx, code, quote).Error(0)
}

func (m *MockPriceRepositoryForNotifyTest) SaveAll(ctx context.Context, code stock.StockCode, quotes []stock.Quote) error {
	return m.Called(ctx, code, quotes).Error(0)
}

func (m *MockPriceRepositoryForNotifyTest) FindRecent(ctx context.Context, code stock.StockCode, n int) ([]stock.Quote, error) {
	args := m.Called(ctx, code, n)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]stock.Quote), args.Error(1)
}

func (m *MockPriceRepositoryForNotifyTest) LatestDate(ctx context.Context, code stock.StockCode) (string, error) {
	args := m.Called(ctx, code)
	return args.String(0), args.Error(1)
}
```

同ファイル内の4テストを、`cache := &MockPriceCacheForNotifyTest{}` → `prices := &MockPriceRepositoryForNotifyTest{}` に置換し、期待値のセットアップと検証を以下のように変更する:

`TestMonitorUsecase_Notify_WithNilNotifyUsecase`:
```go
	prices := &MockPriceRepositoryForNotifyTest{}
	...
	monitor := NewMonitorUsecase(fetcher, prices, detector, 2.5, nil)
	...
	monitor.notify(context.Background(), code, z)

	// ここで検証: リポジトリには何も呼ばれないはず
	prices.AssertNotCalled(t, "FindRecent")
```

`TestMonitorUsecase_Notify_ExtractsCurrentPriceCorrectly`:
```go
	prices := &MockPriceRepositoryForNotifyTest{}
	...
	// 価格データ: 最後の要素が 130.0
	quotes := []stock.Quote{
		{Price: 90, Date: "2026-07-01"},
		{Price: 110, Date: "2026-07-02"},
		{Price: 100, Date: "2026-07-03"},
		{Price: 120, Date: "2026-07-06"},
		{Price: 130, Date: "2026-07-07"},
	}
	expectedCurrentPrice := 130.0 // 末尾要素
	floatPrices := []float64{90, 110, 100, 120, 130}

	prices.On("FindRecent", mock.Anything, code, historySize).Return(quotes, nil)
	...
	monitor := NewMonitorUsecase(fetcher, prices, detector, 2.5, analyzeUsecase)
	...
	prices.AssertCalled(t, "FindRecent", mock.Anything, code, historySize)
```
（`cache.AssertCalled(t, "GetHistory", code, historySize)` の2箇所をこの形に置き換える。それ以外のアサーションはそのまま）

`TestMonitorUsecase_Notify_HandlesEmptyHistoryGracefully`:
```go
	prices := &MockPriceRepositoryForNotifyTest{}
	...
	// FindRecent が空のスライスを返す
	prices.On("FindRecent", mock.Anything, code, historySize).Return([]stock.Quote{}, nil)
	...
	monitor := NewMonitorUsecase(fetcher, prices, detector, 2.5, analyzeUsecase)
	...
	analyzer.AssertNotCalled(t, "Analyze")
	prices.AssertCalled(t, "FindRecent", mock.Anything, code, historySize)
```

`TestMonitorUsecase_Notify_HandleCallWithCorrectZScore`:
```go
	prices := &MockPriceRepositoryForNotifyTest{}
	...
	quotes := []stock.Quote{
		{Price: 100, Date: "2026-07-06"},
		{Price: 105, Date: "2026-07-07"},
		{Price: 110, Date: "2026-07-08"},
	}
	floatPrices := []float64{100, 105, 110}
	expectedCurrentPrice := 110.0
	expectedZScore := 2.5 // テスト用の z-score

	prices.On("FindRecent", mock.Anything, code, historySize).Return(quotes, nil)
	...
	monitor := NewMonitorUsecase(fetcher, prices, detector, 2.5, analyzeUsecase)
```

- [ ] **Step 3: テストを実行して失敗を確認する**

```bash
cd go-api && go test -race -short ./internal/usecase/ 2>&1 | head -20
```

期待: コンパイルエラー（`NewMonitorUsecase` の引数型不一致）

- [ ] **Step 4: `MonitorUsecase` を書き換える**

`go-api/internal/usecase/monitor_stocks.go` の構造体・コンストラクタ・`CheckStock`・`notify` を以下に差し替える（`nextPollTime`・`StartMonitoring`・`historySize`・`jst` はそのまま）:

```go
type MonitorUsecase struct {
	fetcher       stock.PriceFetcher
	prices        stock.PriceRepository
	detector      *anomaly.DetectionService
	threshold     float64
	notifyUsecase *AnalyzeAndNotifyUsecase
	startFn       func(ctx context.Context, codes []stock.StockCode, hour, minute int)
}

func NewMonitorUsecase(
	fetcher stock.PriceFetcher,
	prices stock.PriceRepository,
	detector *anomaly.DetectionService,
	threshold float64,
	notifyUsecase *AnalyzeAndNotifyUsecase,
) *MonitorUsecase {
	u := &MonitorUsecase{
		fetcher:       fetcher,
		prices:        prices,
		detector:      detector,
		threshold:     threshold,
		notifyUsecase: notifyUsecase,
	}
	u.startFn = u.StartMonitoring
	return u
}

// CheckStock は1銘柄の価格を取得・保存し、Z-scoreを計算して異常判定する。
// 履歴が30件未満の場合は detected=false を返す（ウォームアップ期間）。
func (u *MonitorUsecase) CheckStock(ctx context.Context, code stock.StockCode) (bool, anomaly.ZScore, error) {
	quote, err := u.fetcher.FetchLatest(code)
	if err != nil {
		return false, 0, fmt.Errorf("fetch %s: %w", code, err)
	}

	latestDate, err := u.prices.LatestDate(ctx, code)
	if err != nil {
		return false, 0, fmt.Errorf("latest date %s: %w", code, err)
	}
	// 同一取引日は既に取り込み済み。重複を積むと標準偏差が歪むため保存しない。
	// （daily_prices の主キー制約でも弾かれるが、無駄なDB往復と再計算を避ける）
	if quote.Date == latestDate {
		return false, 0, nil
	}

	if err := u.prices.Save(ctx, code, quote); err != nil {
		return false, 0, fmt.Errorf("save price %s: %w", code, err)
	}

	quotes, err := u.prices.FindRecent(ctx, code, historySize)
	if err != nil {
		return false, 0, fmt.Errorf("find recent %s: %w", code, err)
	}

	if len(quotes) < historySize {
		return false, 0, nil
	}

	floatPrices := make([]float64, len(quotes))
	for i, q := range quotes {
		floatPrices[i] = float64(q.Price)
	}

	z, err := u.detector.Calculate(floatPrices)
	if err != nil {
		return false, 0, fmt.Errorf("z-score %s: %w", code, err)
	}

	return z.IsAnomaly(u.threshold), z, nil
}

// notify は異常検知後、AnalyzeAndNotifyUsecase が設定されていれば
// 直近の価格履歴を取得してAI分析・Slack通知パイプラインを起動する。
func (u *MonitorUsecase) notify(ctx context.Context, code stock.StockCode, z anomaly.ZScore) {
	if u.notifyUsecase == nil {
		return
	}
	quotes, err := u.prices.FindRecent(ctx, code, historySize)
	if err != nil || len(quotes) == 0 {
		log.Printf("ERROR fetch history for notify %s: %v", code, err)
		return
	}
	floatPrices := make([]float64, len(quotes))
	for i, q := range quotes {
		floatPrices[i] = float64(q.Price)
	}
	currentPrice := floatPrices[len(floatPrices)-1]
	if err := u.notifyUsecase.Handle(ctx, code, float64(z), currentPrice, floatPrices); err != nil {
		log.Printf("ERROR analyze and notify %s: %v", code, err)
	}
}
```

- [ ] **Step 5: 旧インターフェースとRedis実装を削除する**

```bash
cd go-api
rm internal/domain/stock/price_cache.go
rm -r internal/infrastructure/cache
```

- [ ] **Step 6: `main.go` からRedisを撤去する**

`go-api/cmd/api/main.go` を以下のように変更する:

1. import から `"github.com/redis/go-redis/v9"` と `".../internal/infrastructure/cache"` を削除する
2. `redisURL := mustEnv("REDIS_URL")`（30行目）を削除する
3. Redisクライアント構築ブロック（64-69行目）を削除する:

```go
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("invalid REDIS_URL: %v", err)
	}
	redisClient := redis.NewClient(opt)
	defer redisClient.Close()
```

4. `priceCache := cache.NewRedisPriceCache(redisClient)` を削除する
5. monitor の構築を `priceRepo` に差し替える:

```go
	monitor := usecase.NewMonitorUsecase(priceFetcher, priceRepo, detector, threshold, notifyUsecase)
```

6. `databaseURL := mustEnv("DATABASE_URL")` のブロックが `err` を `:=` で受けられるよう、`pool, err := persistence.Connect(ctx, databaseURL)` のままで良いことを確認する（`opt, err :=` の削除で `err` の初回宣言がここになる）

- [ ] **Step 7: 依存を整理する**

```bash
cd go-api && go mod tidy
```

期待: `go.mod` から `github.com/redis/go-redis/v9` が消える

- [ ] **Step 8: テストを実行して通ることを確認する**

```bash
cd go-api && go build ./... && go test -race -short ./...
DATABASE_URL="postgres://postgres:postgres@localhost:5432/stock_anomaly?sslmode=disable" go test -race ./...
```

期待: すべて PASS。`grep -rn "redis\|PriceCache" internal/ cmd/` が空であること。

- [ ] **Step 9: コミット**

```bash
git add -A go-api/
git commit -m "$(cat <<'EOF'
refactor: move price monitoring from Redis cache to Postgres store

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 5: 起動時に全watchlist銘柄を自動バックフィルする

**Files:**
- Modify: `go-api/internal/usecase/backfill_price_history.go`（`RunAll` を追加）
- Test: `go-api/internal/usecase/backfill_price_history_test.go`（末尾に追記）
- Modify: `go-api/cmd/api/main.go`

**Interfaces:**
- Consumes: `(*BackfillPriceHistoryUsecase).Run(ctx, code)`（Task 3）、`watchlist.Repository.FindAllStockCodes(ctx) ([]stock.StockCode, error)`（既存、`internal/domain/watchlist`）
- Produces: `(*BackfillPriceHistoryUsecase).RunAll(ctx context.Context, codes []stock.StockCode, interval time.Duration)`（戻り値なし。個別失敗はログに出して次へ進む）
- 移行方針: Redisの既存データには日付が無く `daily_prices` へ移せないため、起動時に全watchlist銘柄を Yahoo から再取得する。`Run` が「履歴が1件でもあればスキップ」なので、2回目以降の起動ではDBアクセス1回だけで即座に終わる。

- [ ] **Step 1: 失敗するテストを書く**

`go-api/internal/usecase/backfill_price_history_test.go` の末尾に追記する（import に `"time"` を追加）:

```go
func TestBackfillPriceHistoryUsecase_RunAll_BackfillsEveryCode(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	ctx := context.Background()

	code7203, _ := stock.NewStockCode("7203")
	code9984, _ := stock.NewStockCode("9984")

	quotes7203 := []stock.Quote{{Price: 3200.0, Date: "2026-07-06"}}
	quotes9984 := []stock.Quote{{Price: 8000.0, Date: "2026-07-06"}}

	prices.On("FindRecent", ctx, code7203, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code7203, 500).Return(quotes7203, nil)
	prices.On("SaveAll", ctx, code7203, quotes7203).Return(nil)

	prices.On("FindRecent", ctx, code9984, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code9984, 500).Return(quotes9984, nil)
	prices.On("SaveAll", ctx, code9984, quotes9984).Return(nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	uc.RunAll(ctx, []stock.StockCode{code7203, code9984}, time.Millisecond)

	prices.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestBackfillPriceHistoryUsecase_RunAll_ContinuesAfterFailure(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	ctx := context.Background()

	code7203, _ := stock.NewStockCode("7203")
	code9984, _ := stock.NewStockCode("9984")

	quotes9984 := []stock.Quote{{Price: 8000.0, Date: "2026-07-06"}}

	// 7203 は失敗させる
	prices.On("FindRecent", ctx, code7203, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code7203, 500).Return([]stock.Quote{}, errors.New("yahoo finance unavailable"))
	// 9984 は成功する（前の銘柄の失敗で止まらないこと）
	prices.On("FindRecent", ctx, code9984, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code9984, 500).Return(quotes9984, nil)
	prices.On("SaveAll", ctx, code9984, quotes9984).Return(nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	uc.RunAll(ctx, []stock.StockCode{code7203, code9984}, time.Millisecond)

	prices.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestBackfillPriceHistoryUsecase_RunAll_StopsWhenContextCancelled(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}

	code7203, _ := stock.NewStockCode("7203")
	code9984, _ := stock.NewStockCode("9984")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	uc.RunAll(ctx, []stock.StockCode{code7203, code9984}, time.Hour)

	// キャンセル済みなら1件目にも着手しない
	prices.AssertNotCalled(t, "FindRecent", mock.Anything, mock.Anything, mock.Anything)
	fetcher.AssertNotCalled(t, "FetchHistory", mock.Anything, mock.Anything)
}

func TestBackfillPriceHistoryUsecase_RunAll_EmptyCodes(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	uc.RunAll(context.Background(), nil, time.Millisecond)

	fetcher.AssertNotCalled(t, "FetchHistory", mock.Anything, mock.Anything)
}
```

- [ ] **Step 2: テストを実行して失敗を確認する**

```bash
cd go-api && go test -race -short ./internal/usecase/ -run TestBackfillPriceHistoryUsecase_RunAll 2>&1 | head -10
```

期待: コンパイルエラー `uc.RunAll undefined`

- [ ] **Step 3: `RunAll` を実装する**

`go-api/internal/usecase/backfill_price_history.go` の import に `"log"` と `"time"` を追加し、末尾に以下を追加する:

```go
// RunAll は与えられた銘柄を逐次バックフィルする。銘柄間に interval のウェイトを
// 挟むのは、Yahoo Finance が非公式APIでレート制限が明記されておらず、
// 起動直後に全銘柄分を一斉に投げるとIPブロックされるリスクがあるため。
//
// 個別銘柄の失敗はログに留めて次へ進む。1銘柄の取得失敗で
// 残り全銘柄の移行が止まる方が運用上まずい。
// ctx がキャンセルされたら即座に打ち切る。
func (u *BackfillPriceHistoryUsecase) RunAll(ctx context.Context, codes []stock.StockCode, interval time.Duration) {
	for i, code := range codes {
		if ctx.Err() != nil {
			return
		}
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
		if err := u.Run(ctx, code); err != nil {
			log.Printf("ERROR backfill price history %s: %v", code, err)
		}
	}
}
```

- [ ] **Step 4: テストを実行して通ることを確認する**

```bash
cd go-api && go test -race -short ./internal/usecase/ -v -run TestBackfillPriceHistoryUsecase
```

期待: すべて PASS

- [ ] **Step 5: `main.go` から起動時バックフィルを走らせる**

`go-api/cmd/api/main.go` の `watchlistRefreshInterval` 定数の下に定数を追加する:

```go
const watchlistRefreshInterval = 5 * time.Minute

// startupBackfillInterval は起動時バックフィルで銘柄間に挟むウェイト。
// Yahoo Finance へ一斉にリクエストを投げないための間隔。
const startupBackfillInterval = 2 * time.Second
```

`monitor.RunWithDynamicWatchlist(...)` の直前（`log.Printf("monitoring watchlist stocks...")` の前）に以下を挿入する:

```go
	// 起動時バックフィル: daily_prices に履歴が無い銘柄をYahooから取得して埋める。
	// 旧Redisキャッシュには日付が保存されていなかったため移行できず、既存銘柄は
	// ここで取り直す。履歴がある銘柄は Run がDB参照1回でスキップするので、
	// 2回目以降の起動では実質ノーオペレーションになる。
	// HTTPサーバーと監視ループを待たせないよう goroutine で回す。
	go func() {
		codes, err := watchlistRepo.FindAllStockCodes(ctx)
		if err != nil {
			log.Printf("ERROR fetch watchlist codes for startup backfill: %v", err)
			return
		}
		log.Printf("startup backfill: checking %d stocks", len(codes))
		backfillUsecase.RunAll(ctx, codes, startupBackfillInterval)
		log.Println("startup backfill finished")
	}()
```

- [ ] **Step 6: ビルドとテストを確認する**

```bash
cd go-api && go build ./... && go test -race -short ./...
```

期待: すべて PASS

- [ ] **Step 7: ローカルで起動時バックフィルを実機確認する**

```bash
docker compose up -d postgres
cd go-api && DATABASE_URL="postgres://postgres:postgres@localhost:5432/stock_anomaly?sslmode=disable" \
  ANTHROPIC_API_KEY=dummy SLACK_WEBHOOK_URL=http://localhost:9999 \
  PYTHON_ENGINE_URL=http://localhost:8000 JWT_SECRET=dummy \
  go run ./cmd/api
```

期待するログ: `startup backfill: checking N stocks` → （N>0なら）`startup backfill finished`

別ターミナルで行数を確認する:
```bash
docker compose exec -T postgres psql -U postgres -d stock_anomaly -c "SELECT stock_code, count(*), min(date), max(date) FROM daily_prices GROUP BY stock_code;"
```
期待: watchlist に銘柄があれば、その銘柄について約450〜500行、`min(date)` が約2年前

確認後 Ctrl+C で停止する。

- [ ] **Step 8: コミット**

```bash
git add go-api/internal/usecase/backfill_price_history.go \
        go-api/internal/usecase/backfill_price_history_test.go \
        go-api/cmd/api/main.go
git commit -m "$(cat <<'EOF'
feat: back fill all watchlist stocks on startup

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

---

### Task 6: インフラ設定とドキュメントからRedisを撤去する

**Files:**
- Modify: `docker-compose.yml:19-33`
- Modify: `.github/workflows/go-test.yml:29-37,59-64`
- Modify: `.env.example`
- Modify: `CLAUDE.md`
- Modify: `docs/deployment/phase5-railway-vercel-setup.md:18,30,92-93`

**Interfaces:**
- Consumes: Task 4 で `REDIS_URL` を読むコードが無くなっていること
- Produces: Redis を必要としない開発・CI・デプロイ手順。`DATABASE_URL` のみで統合テストが通る。

- [ ] **Step 1: `docker-compose.yml` からRedisサービスを削除する**

`redis` サービス定義（19-29行目）と `volumes` の `redis_data:`（33行目）を削除する。結果:

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: stock_anomaly
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./go-api/migrations:/docker-entrypoint-initdb.d
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      timeout: 5s
      retries: 5

volumes:
  postgres_data:
```

- [ ] **Step 2: CIワークフローからRedisを削除する**

`.github/workflows/go-test.yml` から `redis` サービス定義（29-37行目）を削除し、"Run integration tests" ステップの `REDIS_URL` 環境変数（64行目）を削除する。結果の該当部分:

```yaml
      - name: Run integration tests
        working-directory: go-api
        run: go test -race ./...
        env:
          DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable
```

（Task 1 で追加した `003_create_daily_prices.sql` のマイグレーション行はそのまま残す）

- [ ] **Step 3: `.env.example` から `REDIS_URL` を削除する**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
sed -i '' '/REDIS_URL/d' .env.example
grep -c REDIS_URL .env.example || echo "REDIS_URL removed"
```

期待: `REDIS_URL removed`

- [ ] **Step 4: `CLAUDE.md` を更新する**

以下の4箇所を書き換える。

「Goコマンド」節:
```markdown
- `go test -race ./...` — 統合テスト（DATABASE_URL 必要）
```

「重要な設計決定」節の Redis キーの行を、以下で置き換える:
```markdown
- 日次終値は Postgres の `daily_prices` テーブル（`PRIMARY KEY (stock_code, date)`、`close NUMERIC`）に保存する。他テーブルのUUID PK規約に対する意図的な逸脱で、理由は (1) 同一銘柄・同一取引日の重複投入を `ON CONFLICT DO NOTHING` で冪等に弾くため、(2)「銘柄指定で日付降順に直近n件」が主キーインデックスだけで完結するため。異常検知は直近30件（`historySize`）のウィンドウを `PriceRepository.FindRecent` で読む。Redisは2026-09-24に廃止した（日付を持てずチャート・予測の基盤にできなかったため）
```

watchlist追加時のバックフィルの行を、以下で置き換える:
```markdown
- watchlist追加時（`ManageWatchlistUsecase.Add`）は`usecase.BackfillPriceHistoryUsecase`が`YahooFinanceClient.FetchHistory`経由で過去約2年分（`backfillDays=500`営業日、`range=2y`）の終値を取得し`daily_prices`へ一括投入する。対象銘柄に既に価格履歴があればスキップ（冪等）。取得失敗はログのみでwatchlist登録自体は成功させる。さらにサーバー起動時に`BackfillPriceHistoryUsecase.RunAll`が全watchlist銘柄に対して同じ処理を2秒間隔で走らせる（goroutine、HTTPサーバーはブロックしない）
```

「GitHub Actions CI」節:
```markdown
## GitHub Actions CI
- postgres:16 サービス
- `DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable`
- マイグレーションは `go-api/migrations/*.sql` をファイル名指定で順に適用する。**新しいマイグレーションを追加したら `.github/workflows/go-test.yml` の "Run database migration" ステップにも追記すること**
```

「環境変数（本番）」節から `REDIS_URL, ` を削除する:
```markdown
DATABASE_URL, ANOMALY_THRESHOLD（デフォルト2.5）, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）, JWT_SECRET, PORT（デフォルト8080）
```

- [ ] **Step 5: デプロイ手順書を更新する**

`docs/deployment/phase5-railway-vercel-setup.md`:

1. 18行目 `3. プロジェクトに「Redis」プラグインを追加する` を削除する
2. 30行目を以下に変更する:
```markdown
5. 環境変数タブで以下を設定する（`DATABASE_URL`はPostgresプラグインの
   「Variable Reference」機能で自動注入されるものを使う。`PORT`はRailwayが自動注入するため設定不要）:
```
3. 92-93行目のマイグレーション適用に003を追記する:
```
\i go-api/migrations/001_initial_schema.sql
\i go-api/migrations/002_add_watchlist_stock_name.sql
\i go-api/migrations/003_create_daily_prices.sql
```
4. 手順書の末尾（または4節の直後）に、既存環境向けの注意を1行追加する:
```markdown
> **既にRedisプラグインを追加済みの環境について:** go-apiは2026-09-24以降Redisを一切参照しない。
> Railwayプロジェクトに残っているRedisプラグインと`REDIS_URL`の変数参照は削除してよい。
```

- [ ] **Step 6: Redis参照が残っていないことを確認する**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
grep -rn -i "redis" --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=plans --exclude=go.sum . | grep -v "docs/superpowers/specs/"
```

期待: 出力なし、または `docs/deployment/...` の「削除してよい」注記だけ
（`docs/superpowers/plans/` の過去の計画書は履歴なので変更しない）

- [ ] **Step 7: 一通り動くことを確認する**

```bash
docker compose down -v && docker compose up -d postgres
cd go-api && go build ./... && go test -race -short ./...
DATABASE_URL="postgres://postgres:postgres@localhost:5432/stock_anomaly?sslmode=disable" go test -race ./...
```

期待: すべて PASS（`docker compose down -v` でボリュームを消したので003を含む全マイグレーションが初期化スクリプトとして自動適用される）

- [ ] **Step 8: コミット**

```bash
git add docker-compose.yml .github/workflows/go-test.yml .env.example CLAUDE.md docs/deployment/phase5-railway-vercel-setup.md
git commit -m "$(cat <<'EOF'
chore: drop Redis from local, CI, and deployment setup

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
EOF
)"
```

- [ ] **Step 9: Push して Pull Request を作成する**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git push -u origin feature/postgres-price-store
gh pr create --title "feat: consolidate price storage into Postgres daily_prices" --body "$(cat <<'EOF'
## 概要

Redisの日付なし価格キャッシュを、日付を持つPostgresの`daily_prices`テーブルへ一本化した。株価チャートと統計的な予測帯（PR-B）のためのデータ基盤で、**外部から観測できる振る舞いは変えていない**。

設計の経緯は `docs/superpowers/specs/2026-09-24-stock-chart-forecast-design.md`、実装計画は `docs/superpowers/plans/2026-09-24-postgres-price-store.md` を参照。

## 変更点

- `daily_prices` テーブルを追加（`PRIMARY KEY (stock_code, date)`、マイグレーション003）
- `stock.PriceCache` → `stock.PriceRepository` へ置き換え（`ctx` 対応、`SetLastDate`/`LastDate` の二重管理を廃止）
- `PgPriceRepository` を追加（`pgx.Batch` + `ON CONFLICT DO NOTHING` による冪等な一括投入）
- バックフィル期間を約3ヶ月（30営業日）から約2年（500営業日）へ拡張。Yahoo chart APIの`range`は必要日数から自動選択する
- 起動時に全watchlist銘柄を自動バックフィルする（旧Redisデータは日付を持たず移行不能なため再取得）
- Redisをコード・`docker-compose.yml`・CI・`.env.example`・デプロイ手順から完全に撤去

## 意図的な設計判断

- **複合自然キー**: CLAUDE.mdのUUID PK規約から意図的に逸脱した。冪等なUPSERTと日付降順走査を主キーインデックスだけで賄うため
- **`stock.PriceFetcher` に `ctx` を足していない**: `POST /watchlist` の同期ブロッキング解消は独立タスクとして先送り済み。バックフィル件数が約8倍になるためブロッキング時間は悪化するが、本PRのスコープ外
- **`anomaly/service.go` は未変更**: 価格水準ベースのZ-score計算は現状のまま

## テスト

- `go test -race -short ./...` — PASS
- `go test -race ./...`（`DATABASE_URL` 設定） — PASS
- ローカル実機で起動時バックフィルを確認し、`daily_prices` に約2年分の行が入ることを検証済み

## デプロイ時の注意

- Railwayで `003_create_daily_prices.sql` の適用が必要
- Railwayの Redis プラグインと `REDIS_URL` の変数参照は削除してよい

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

---

## 自己レビュー

**1. Spec カバレッジ**

| Spec の要求 | 対応タスク |
|---|---|
| Postgresに日次価格テーブルを新設 | Task 1 |
| 2年保持 | Task 2（`range=2y`）+ Task 3（`backfillDays=500`） |
| Postgresに一本化（Redis廃止） | Task 4（コード）+ Task 6（インフラ） |
| 起動時に自動バックフィル | Task 5 |
| コード＋ローカル＋CIを同PRで撤去 | Task 4 + Task 6 |
| 異常検知の振る舞いを変えない | Global Constraints（`anomaly/service.go` 変更禁止）+ Task 4 の `historySize=30` 維持 |
| Python `/forecast` エンドポイント | **PR-B**（本計画のスコープ外） |
| `GET /stocks/{code}/chart` | **PR-B**（本計画のスコープ外） |
| 銘柄詳細ページ + Recharts | **PR-B**（本計画のスコープ外） |
| 予測帯（±1σ/±1.96σ、20営業日） | **PR-B**（本計画のスコープ外） |
| 異常検知の発火履歴マーカー | **PR-B**（本計画のスコープ外） |

PR-B のスコープはSpecの「PR分割」節どおり本計画から意図的に除外している。

**2. プレースホルダ確認**

「TBD」「後で実装」「Task N と同様」「適切なエラーハンドリングを追加」の類は無い。すべてのコードステップに実コードを記載済み。

**3. 型の整合性**

- `PriceRepository` の4メソッド名（`Save` / `SaveAll` / `FindRecent` / `LatestDate`）は Task 1・3・4・5 を通して一貫している
- `Run(ctx, code)` のシグネチャは Task 3 で確定し、Task 5 の `RunAll` から同じ形で呼ばれる
- `backfillDays = 500` は Task 3 で定義し、Task 2 のレンジ選択（`>250` → `2y`）・Task 3/5 のテスト期待値（`FetchHistory`, 500）で一致している
- `historySize = 30` は変更していない（Task 4 の `FindRecent(ctx, code, historySize)`）
- モックは `usecase_test`（Task 3、`MockPriceRepository`）と `usecase`（Task 4、`MockPriceRepositoryForNotifyTest`）で別々に定義する。`monitor_notify_test.go` が内部テストパッケージのため共有できない

**4. 各タスク終了時のビルド整合性**

- Task 3 終了時点では `main.go` が Redis(`priceCache`) と Postgres(`priceRepo`) の両方を構築し、monitor は `priceCache`・backfill は `priceRepo` を使う。`go build` は通る
- Task 4 で monitor を切り替えると `priceCache` が未使用になるため、**同じタスク内で** Redis の構築・import・`price_cache.go`・`cache` パッケージをまとめて削除する必要がある。部分的に消すとコンパイルが壊れる
