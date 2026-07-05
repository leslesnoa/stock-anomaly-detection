# Phase 1: Go 株価取得・Z-score異常検知 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** J-Quants APIから東証株価を毎分取得し、goroutineで複数銘柄を並行監視してZ-scoreで異常を検知し、ログに記録するGoサービスを構築する。

**Architecture:** Clean Architecture（domain → usecase → interface/infrastructure）+ 実践的DDD。domain層は外部依存ゼロ。goroutine per stock code で並行監視し、Redisに直近30件の終値をキャッシュしてZ-scoreを計算する。GoがZ-score ≥ 閾値を検知したときのみログ出力する（Phase 3でSlack通知に差し替え）。

**Tech Stack:** Go 1.22, pgx/v5（PostgreSQL）, go-redis/v9（Redis）, testify v1.9, httptest（J-Quantsモック）

## Global Constraints

- Go バージョン: 1.22（`go 1.22` を go.mod に記載）
- モジュール名: `github.com/stock-anomaly-detection/go-api`（GitHub repo作成後に実際のusernameに変更）
- 作業ディレクトリ: `go-api/`（モノレポルート `/Users/watanabekeisuke/Documents/stock-anomaly-detection` から）
- コミットメッセージ: Conventional Commits形式（`feat:`, `test:`, `chore:`, `ci:` など）
- テスト: table driven tests（`tests := []struct{ ... }{}` パターン）
- integration test は `testing.Short()` でスキップ可能にする（`go test -short ./...` でunit onlyに）
- APIキー・接続情報: 環境変数で管理。コードへの直書き禁止
- デフォルトZ-score閾値: 2.5σ
- `time.Time` を扱う構造体のゼロ値は `time.Time{}`（`nil` ではない）

---

### Task 1: プロジェクト初期化

**Files:**
- Create: `go-api/go.mod`
- Create: `go-api/go.sum`
- Create: `go-api/cmd/api/main.go`
- Create: `go-api/.gitignore`
- Create: ディレクトリ骨格（`internal/domain/{stock,watchlist,anomaly}`, `internal/usecase`, `internal/interface/gateway`, `internal/infrastructure/{persistence,cache}`, `migrations/`）

**Interfaces:**
- Produces: Go module `github.com/stock-anomaly-detection/go-api`、ビルド可能な空のmain

- [ ] **Step 1: git初期化とディレクトリ作成**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git init
git add docs/ .claude/
git commit -m "chore: add design docs, adr, and claude project config"

mkdir -p go-api/cmd/api
mkdir -p go-api/internal/domain/{stock,watchlist,anomaly}
mkdir -p go-api/internal/{usecase,interface/gateway}
mkdir -p go-api/internal/infrastructure/{persistence,cache}
mkdir -p go-api/migrations
```

- [ ] **Step 2: Go module初期化と依存関係追加**

```bash
cd go-api
go mod init github.com/stock-anomaly-detection/go-api
go get github.com/jackc/pgx/v5@v5.6.0
go get github.com/redis/go-redis/v9@v9.6.1
go get github.com/stretchr/testify@v1.9.0
```

- [ ] **Step 3: 空のmain.goを作成**

`go-api/cmd/api/main.go`:
```go
package main

func main() {}
```

- [ ] **Step 4: .gitignoreを作成**

`go-api/.gitignore`:
```
*.env
.env*
```

- [ ] **Step 5: ビルド確認**

```bash
cd go-api && go build ./...
```
Expected: エラーなし

- [ ] **Step 6: コミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add go-api/
git commit -m "chore: initialize go-api module with clean architecture directory structure"
```

---

### Task 2: ドメイン値オブジェクト・エンティティ・インターフェース

**Files:**
- Create: `go-api/internal/domain/stock/value_object.go`
- Create: `go-api/internal/domain/stock/price_cache.go`
- Create: `go-api/internal/domain/stock/price_fetcher.go`
- Create: `go-api/internal/domain/anomaly/value_object.go`
- Create: `go-api/internal/domain/watchlist/entity.go`
- Create: `go-api/internal/domain/watchlist/repository.go`
- Test: `go-api/internal/domain/stock/value_object_test.go`
- Test: `go-api/internal/domain/anomaly/value_object_test.go`

**Interfaces:**
- Consumes: なし（外部依存ゼロ）
- Produces:
  - `stock.StockCode`（string型、空文字禁止）
  - `stock.Price`（float64）
  - `stock.PriceCache` interface: `Push(StockCode, Price) error`、`GetHistory(StockCode, n int) ([]Price, error)`
  - `stock.PriceFetcher` interface: `FetchLatest(StockCode) (Price, error)`
  - `anomaly.ZScore`（float64）、`anomaly.ZScore.IsAnomaly(threshold float64) bool`
  - `watchlist.Watchlist` struct（ID, UserID, StockCode, AlertThreshold, CreatedAt）
  - `watchlist.Repository` interface: `FindAll(ctx context.Context) ([]Watchlist, error)`

- [ ] **Step 1: stock値オブジェクトを作成**

`go-api/internal/domain/stock/value_object.go`:
```go
package stock

import "errors"

type StockCode string

func NewStockCode(code string) (StockCode, error) {
	if code == "" {
		return "", errors.New("stock code cannot be empty")
	}
	return StockCode(code), nil
}

func (s StockCode) String() string { return string(s) }

type Price float64
type Volume int64
```

- [ ] **Step 2: インターフェースを作成**

`go-api/internal/domain/stock/price_cache.go`:
```go
package stock

type PriceCache interface {
	Push(code StockCode, price Price) error
	GetHistory(code StockCode, n int) ([]Price, error)
}
```

`go-api/internal/domain/stock/price_fetcher.go`:
```go
package stock

type PriceFetcher interface {
	FetchLatest(code StockCode) (Price, error)
}
```

- [ ] **Step 3: anomaly値オブジェクトを作成**

`go-api/internal/domain/anomaly/value_object.go`:
```go
package anomaly

type ZScore float64

func (z ZScore) IsAnomaly(threshold float64) bool {
	v := float64(z)
	if v < 0 {
		v = -v
	}
	return v >= threshold
}
```

- [ ] **Step 4: watchlistエンティティとリポジトリインターフェースを作成**

`go-api/internal/domain/watchlist/entity.go`:
```go
package watchlist

import "time"

type Watchlist struct {
	ID             string
	UserID         string
	StockCode      string
	AlertThreshold float64
	CreatedAt      time.Time
}
```

`go-api/internal/domain/watchlist/repository.go`:
```go
package watchlist

import "context"

type Repository interface {
	FindAll(ctx context.Context) ([]Watchlist, error)
}
```

- [ ] **Step 5: domain値オブジェクトのテストを書く**

`go-api/internal/domain/stock/value_object_test.go`:
```go
package stock_test

import (
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStockCode(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{"valid 4-digit code", "7203", false},
		{"valid 5-digit code", "72030", false},
		{"empty code returns error", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stock.NewStockCode(tt.code)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, stock.StockCode(tt.code), got)
			assert.Equal(t, tt.code, got.String())
		})
	}
}
```

`go-api/internal/domain/anomaly/value_object_test.go`:
```go
package anomaly_test

import (
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stretchr/testify/assert"
)

func TestZScore_IsAnomaly(t *testing.T) {
	tests := []struct {
		name      string
		z         anomaly.ZScore
		threshold float64
		want      bool
	}{
		{"above threshold positive", 2.6, 2.5, true},
		{"below threshold positive", 2.4, 2.5, false},
		{"above threshold negative", -2.6, 2.5, true},
		{"below threshold negative", -2.4, 2.5, false},
		{"exactly at threshold", 2.5, 2.5, true},
		{"zero", 0, 2.5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.z.IsAnomaly(tt.threshold))
		})
	}
}
```

- [ ] **Step 6: テストを実行して確認**

```bash
cd go-api && go test ./internal/domain/...
```
Expected: PASS（全6件）

- [ ] **Step 7: コミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add go-api/internal/domain/
git commit -m "feat: add domain value objects, entities, and repository interfaces"
```

---

### Task 3: Z-scoreドメインサービス

**Files:**
- Create: `go-api/internal/domain/anomaly/service.go`
- Test: `go-api/internal/domain/anomaly/service_test.go`

**Interfaces:**
- Consumes: `anomaly.ZScore`
- Produces: `*anomaly.DetectionService`、`func (s *DetectionService) Calculate(prices []float64) (ZScore, error)`

- [ ] **Step 1: テストを先に書く（TDD）**

`go-api/internal/domain/anomaly/service_test.go`:
```go
package anomaly_test

import (
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectionService_Calculate(t *testing.T) {
	svc := anomaly.NewDetectionService()

	// 29件の交互データ（90, 110, 90, 110...）でhistoryを作る
	// mean≈99.65, stddev≈10
	makeAlternating := func(n int) []float64 {
		p := make([]float64, n)
		for i := range p {
			if i%2 == 0 {
				p[i] = 90
			} else {
				p[i] = 110
			}
		}
		return p
	}

	tests := []struct {
		name        string
		prices      []float64
		wantAnomaly bool
		wantZero    bool
		wantErr     bool
	}{
		{
			name:    "insufficient data returns error",
			prices:  []float64{100},
			wantErr: true,
		},
		{
			name:     "zero stddev returns 0",
			prices:   append(make([]float64, 29), 100), // 30 zeros + last 100 → all same
			wantZero: true,
		},
		{
			name:        "large spike above threshold is anomaly",
			prices:      append(makeAlternating(29), 130), // current=130, z≈3.0
			wantAnomaly: true,
		},
		{
			name:        "small variation is not anomaly",
			prices:      append(makeAlternating(29), 111), // current=111, z≈1.1
			wantAnomaly: false,
		},
		{
			name:        "large drop below mean is anomaly",
			prices:      append(makeAlternating(29), 70), // current=70, z≈-3.0
			wantAnomaly: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.Calculate(tt.prices)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantZero {
				assert.Equal(t, anomaly.ZScore(0), got)
				return
			}
			assert.Equal(t, tt.wantAnomaly, got.IsAnomaly(2.5),
				"z=%.3f, threshold=2.5", got)
		})
	}
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd go-api && go test ./internal/domain/anomaly/...
```
Expected: FAIL（service.go が存在しないためコンパイルエラー）

- [ ] **Step 3: Z-scoreサービスを実装**

`go-api/internal/domain/anomaly/service.go`:
```go
package anomaly

import (
	"errors"
	"math"
)

type DetectionService struct{}

func NewDetectionService() *DetectionService {
	return &DetectionService{}
}

// Calculate は prices の末尾を現在値、先頭〜末尾-1 を履歴として Z-score を計算する。
// 履歴の標準偏差が0の場合は 0 を返す（同一価格が続く状態）。
func (s *DetectionService) Calculate(prices []float64) (ZScore, error) {
	if len(prices) < 2 {
		return 0, errors.New("need at least 2 price data points")
	}
	current := prices[len(prices)-1]
	history := prices[:len(prices)-1]

	mean := calcMean(history)
	stddev := calcStddev(history, mean)
	if stddev == 0 {
		return 0, nil
	}
	return ZScore((current - mean) / stddev), nil
}

func calcMean(prices []float64) float64 {
	sum := 0.0
	for _, p := range prices {
		sum += p
	}
	return sum / float64(len(prices))
}

func calcStddev(prices []float64, mean float64) float64 {
	variance := 0.0
	for _, p := range prices {
		diff := p - mean
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(len(prices)))
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd go-api && go test ./internal/domain/anomaly/...
```
Expected: PASS（全5件）

- [ ] **Step 5: コミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add go-api/internal/domain/anomaly/
git commit -m "feat: implement z-score anomaly detection domain service"
```

---

### Task 4: DBマイグレーション + PostgreSQL接続

**Files:**
- Create: `go-api/migrations/001_initial_schema.sql`
- Create: `go-api/internal/infrastructure/persistence/db.go`
- Test: `go-api/internal/infrastructure/persistence/db_test.go`

**Interfaces:**
- Consumes: 環境変数 `DATABASE_URL`（例: `postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable`）
- Produces: `func Connect(ctx context.Context, databaseURL string) (*pgx.Conn, error)`

- [ ] **Step 1: マイグレーションSQLを作成**

`go-api/migrations/001_initial_schema.sql`:
```sql
CREATE TABLE IF NOT EXISTS users (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email             TEXT UNIQUE NOT NULL,
    password_hash     TEXT NOT NULL,
    slack_webhook_url TEXT,
    created_at        TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE IF NOT EXISTS watchlist (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID REFERENCES users(id) ON DELETE CASCADE,
    stock_code      TEXT NOT NULL,
    alert_threshold NUMERIC DEFAULT 2.5,
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE(user_id, stock_code)
);

CREATE TABLE IF NOT EXISTS notifications (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID REFERENCES users(id),
    stock_code           TEXT NOT NULL,
    anomaly_score        NUMERIC NOT NULL,
    ai_report            TEXT,
    technical_indicators JSONB,
    slack_sent           BOOLEAN DEFAULT false,
    notified_at          TIMESTAMPTZ DEFAULT now()
);
```

- [ ] **Step 2: DB接続ヘルパーを実装**

`go-api/internal/infrastructure/persistence/db.go`:
```go
package persistence

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func Connect(ctx context.Context, databaseURL string) (*pgx.Conn, error) {
	return pgx.Connect(ctx, databaseURL)
}
```

- [ ] **Step 3: integration テストを書く**

`go-api/internal/infrastructure/persistence/db_test.go`:
```go
package persistence_test

import (
	"context"
	"os"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stretchr/testify/require"
)

func TestConnect(t *testing.T) {
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

	var result int
	err = conn.QueryRow(ctx, "SELECT 1").Scan(&result)
	require.NoError(t, err)
	require.Equal(t, 1, result)
}
```

- [ ] **Step 4: -short フラグでスキップ確認**

```bash
cd go-api && go test -short ./internal/infrastructure/persistence/...
```
Expected: PASS（`--- SKIP: TestConnect (skipping integration test)` と表示）

- [ ] **Step 5: コミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add go-api/migrations/ go-api/internal/infrastructure/persistence/
git commit -m "feat: add db migrations and postgresql connection helper"
```

---

### Task 5: Watchlistリポジトリ（PostgreSQL実装）

**Files:**
- Create: `go-api/internal/infrastructure/persistence/watchlist_repository.go`
- Test: `go-api/internal/infrastructure/persistence/watchlist_repository_test.go`

**Interfaces:**
- Consumes: `watchlist.Repository` interface、`*pgx.Conn`
- Produces: `*PgWatchlistRepository`（`watchlist.Repository` を実装）
  - `func NewPgWatchlistRepository(conn *pgx.Conn) *PgWatchlistRepository`
  - `func (r *PgWatchlistRepository) FindAll(ctx context.Context) ([]watchlist.Watchlist, error)`

- [ ] **Step 1: テストを先に書く（TDD）**

`go-api/internal/infrastructure/persistence/watchlist_repository_test.go`:
```go
package persistence_test

import (
	"context"
	"os"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPgWatchlistRepository_FindAll_Empty(t *testing.T) {
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

	// マイグレーション適用
	migration, err := os.ReadFile("../../../migrations/001_initial_schema.sql")
	require.NoError(t, err)
	_, err = conn.Exec(ctx, string(migration))
	require.NoError(t, err)

	repo := persistence.NewPgWatchlistRepository(conn)
	watchlists, err := repo.FindAll(ctx)
	require.NoError(t, err)
	assert.NotNil(t, watchlists) // nil ではなく空スライスを返す
	assert.IsType(t, []watchlist.Watchlist{}, watchlists)
}
```

※ import に `"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"` を追加する。

- [ ] **Step 2: テストが -short でスキップされることを確認**

```bash
cd go-api && go test -short ./internal/infrastructure/persistence/...
```
Expected: PASS（SKIPメッセージ）

- [ ] **Step 3: リポジトリを実装**

`go-api/internal/infrastructure/persistence/watchlist_repository.go`:
```go
package persistence

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

type PgWatchlistRepository struct {
	conn *pgx.Conn
}

func NewPgWatchlistRepository(conn *pgx.Conn) *PgWatchlistRepository {
	return &PgWatchlistRepository{conn: conn}
}

func (r *PgWatchlistRepository) FindAll(ctx context.Context) ([]watchlist.Watchlist, error) {
	rows, err := r.conn.Query(ctx, `
		SELECT id, user_id, stock_code, alert_threshold, created_at
		FROM watchlist
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []watchlist.Watchlist{}
	for rows.Next() {
		var w watchlist.Watchlist
		if err := rows.Scan(&w.ID, &w.UserID, &w.StockCode, &w.AlertThreshold, &w.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}
```

- [ ] **Step 4: ビルド確認**

```bash
cd go-api && go build ./...
```
Expected: エラーなし

- [ ] **Step 5: コミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add go-api/internal/infrastructure/persistence/
git commit -m "feat: implement postgresql watchlist repository"
```

---

### Task 6: Redisキャッシュ実装（30件スライディングウィンドウ）

**Files:**
- Create: `go-api/internal/infrastructure/cache/redis_price_cache.go`
- Test: `go-api/internal/infrastructure/cache/redis_price_cache_test.go`

**Interfaces:**
- Consumes: `stock.PriceCache` interface、`*redis.Client`
- Produces: `*RedisPriceCache`（`stock.PriceCache` を実装）
  - `func NewRedisPriceCache(client *redis.Client) *RedisPriceCache`
  - `Push`: Redisリストにpushし、最新30件のみ保持
  - `GetHistory`: 指定件数の履歴を古い順で返す

- [ ] **Step 1: テストを先に書く（TDD）**

`go-api/internal/infrastructure/cache/redis_price_cache_test.go`:
```go
package cache_test

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisPriceCache_PushAndGetHistory(t *testing.T) {
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
	key := "price:7203"

	// テスト前にクリア
	client.Del(ctx, key)

	// Push 3件 → GetHistory(3) で古い順に3件返る
	require.NoError(t, c.Push(code, 3250.0))
	require.NoError(t, c.Push(code, 3260.0))
	require.NoError(t, c.Push(code, 3270.0))

	prices, err := c.GetHistory(code, 3)
	require.NoError(t, err)
	assert.Len(t, prices, 3)
	assert.Equal(t, stock.Price(3250.0), prices[0])
	assert.Equal(t, stock.Price(3260.0), prices[1])
	assert.Equal(t, stock.Price(3270.0), prices[2])
}

func TestRedisPriceCache_MaxHistory30(t *testing.T) {
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
	code, _ := stock.NewStockCode("9984")
	client.Del(ctx, "price:9984")

	// 35件push → 最新30件のみ残る
	for i := 0; i < 35; i++ {
		require.NoError(t, c.Push(code, stock.Price(float64(1000+i))))
	}

	prices, err := c.GetHistory(code, 30)
	require.NoError(t, err)
	assert.Len(t, prices, 30)
	assert.Equal(t, stock.Price(1005.0), prices[0]) // 35-30=5番目から始まる
	assert.Equal(t, stock.Price(1034.0), prices[29])
}
```

- [ ] **Step 2: -short でスキップ確認**

```bash
cd go-api && go test -short ./internal/infrastructure/cache/...
```
Expected: PASS（SKIPメッセージ）

- [ ] **Step 3: Redisキャッシュを実装**

`go-api/internal/infrastructure/cache/redis_price_cache.go`:
```go
package cache

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

const maxHistory = 30

type RedisPriceCache struct {
	client *redis.Client
}

func NewRedisPriceCache(client *redis.Client) *RedisPriceCache {
	return &RedisPriceCache{client: client}
}

func (c *RedisPriceCache) Push(code stock.StockCode, price stock.Price) error {
	ctx := context.Background()
	key := fmt.Sprintf("price:%s", code)
	pipe := c.client.Pipeline()
	pipe.RPush(ctx, key, strconv.FormatFloat(float64(price), 'f', -1, 64))
	pipe.LTrim(ctx, key, -maxHistory, -1)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RedisPriceCache) GetHistory(code stock.StockCode, n int) ([]stock.Price, error) {
	ctx := context.Background()
	key := fmt.Sprintf("price:%s", code)
	vals, err := c.client.LRange(ctx, key, -int64(n), -1).Result()
	if err != nil {
		return nil, err
	}
	prices := make([]stock.Price, 0, len(vals))
	for _, v := range vals {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("parse price %q: %w", v, err)
		}
		prices = append(prices, stock.Price(f))
	}
	return prices, nil
}
```

- [ ] **Step 4: ビルド確認**

```bash
cd go-api && go build ./...
```
Expected: エラーなし

- [ ] **Step 5: コミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add go-api/internal/infrastructure/cache/
git commit -m "feat: implement redis price cache with 30-entry sliding window"
```

---

### Task 7: J-Quantsクライアント

**Files:**
- Create: `go-api/internal/interface/gateway/jquants_client.go`
- Test: `go-api/internal/interface/gateway/jquants_client_test.go`

**Interfaces:**
- Consumes: `stock.PriceFetcher` interface
- Produces: `*JQuantsClient`（`stock.PriceFetcher` を実装）
  - `func NewJQuantsClient(email, password string) *JQuantsClient`
  - `func NewJQuantsClientWithBaseURL(email, password, baseURL string) *JQuantsClient`（テスト用）
  - `func (c *JQuantsClient) FetchLatest(code StockCode) (Price, error)`

**J-Quants API 認証フロー:**
1. `POST /v1/token/auth_user` → `{"refreshToken": "..."}`
2. `POST /v1/token/auth_refresh` → `{"idToken": "..."}` (有効期限24時間)
3. 以降のリクエストに `Authorization: Bearer <idToken>` を付与

**J-Quants 株価取得:**
- `GET /v1/prices/daily_quotes?code=<4桁コード>0`（末尾に0をつける）
- レスポンス: `{"daily_quotes": [{"Code": "72030", "Close": 3250.0, ...}]}`

- [ ] **Step 1: テストを先に書く（httptest使用）**

`go-api/internal/interface/gateway/jquants_client_test.go`:
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

func TestJQuantsClient_FetchLatest(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/token/auth_user", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"refreshToken": "test-refresh"})
	})
	mux.HandleFunc("POST /v1/token/auth_refresh", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"idToken": "test-id-token"})
	})
	mux.HandleFunc("GET /v1/prices/daily_quotes", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-id-token", r.Header.Get("Authorization"))
		assert.Equal(t, "72030", r.URL.Query().Get("code"))
		json.NewEncoder(w).Encode(map[string]interface{}{
			"daily_quotes": []map[string]interface{}{
				{"Code": "72030", "Close": 3250.0},
			},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewJQuantsClientWithBaseURL("test@example.com", "pass", srv.URL)
	code, _ := stock.NewStockCode("7203")

	price, err := client.FetchLatest(code)
	require.NoError(t, err)
	assert.Equal(t, stock.Price(3250.0), price)
}

func TestJQuantsClient_FetchLatest_NoData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/token/auth_user", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"refreshToken": "r"})
	})
	mux.HandleFunc("POST /v1/token/auth_refresh", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"idToken": "i"})
	})
	mux.HandleFunc("GET /v1/prices/daily_quotes", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"daily_quotes": []interface{}{}})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewJQuantsClientWithBaseURL("e", "p", srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchLatest(code)
	require.Error(t, err)
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd go-api && go test ./internal/interface/gateway/...
```
Expected: FAIL（パッケージが存在しない）

- [ ] **Step 3: J-Quantsクライアントを実装**

`go-api/internal/interface/gateway/jquants_client.go`:
```go
package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type JQuantsClient struct {
	baseURL  string
	email    string
	password string
	idToken  string
}

func NewJQuantsClient(email, password string) *JQuantsClient {
	return NewJQuantsClientWithBaseURL(email, password, "https://api.jquants.com")
}

func NewJQuantsClientWithBaseURL(email, password, baseURL string) *JQuantsClient {
	return &JQuantsClient{baseURL: baseURL, email: email, password: password}
}

func (c *JQuantsClient) FetchLatest(code stock.StockCode) (stock.Price, error) {
	if err := c.ensureToken(); err != nil {
		return 0, fmt.Errorf("j-quants auth: %w", err)
	}

	// J-Quantsは4桁コードに末尾0を付けた5桁で検索する
	url := fmt.Sprintf("%s/v1/prices/daily_quotes?code=%s0", c.baseURL, code)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.idToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		DailyQuotes []struct {
			Close float64 `json:"Close"`
		} `json:"daily_quotes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	if len(result.DailyQuotes) == 0 {
		return 0, fmt.Errorf("no quote data for %s", code)
	}
	return stock.Price(result.DailyQuotes[0].Close), nil
}

func (c *JQuantsClient) ensureToken() error {
	if c.idToken != "" {
		return nil
	}

	body, _ := json.Marshal(map[string]string{
		"mailaddress": c.email,
		"password":    c.password,
	})
	resp, err := http.Post(c.baseURL+"/v1/token/auth_user", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r1 struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r1); err != nil {
		return err
	}

	body, _ = json.Marshal(map[string]string{"refreshtoken": r1.RefreshToken})
	resp2, err := http.Post(c.baseURL+"/v1/token/auth_refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	var r2 struct {
		IDToken string `json:"idToken"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&r2); err != nil {
		return err
	}

	c.idToken = r2.IDToken
	return nil
}
```

- [ ] **Step 4: テストが通ることを確認**

```bash
cd go-api && go test ./internal/interface/gateway/...
```
Expected: PASS（2件）

- [ ] **Step 5: コミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add go-api/internal/interface/
git commit -m "feat: implement j-quants api client with token auth and httptest"
```

---

### Task 8: MonitorStocksユースケース

**Files:**
- Create: `go-api/internal/usecase/monitor_stocks.go`
- Test: `go-api/internal/usecase/monitor_stocks_test.go`

**Interfaces:**
- Consumes: `stock.PriceFetcher`、`stock.PriceCache`、`*anomaly.DetectionService`
- Produces:
  - `func NewMonitorUsecase(fetcher stock.PriceFetcher, cache stock.PriceCache, detector *anomaly.DetectionService, threshold float64) *MonitorUsecase`
  - `func (u *MonitorUsecase) CheckStock(ctx context.Context, code stock.StockCode) (detected bool, zScore anomaly.ZScore, err error)`
  - `func (u *MonitorUsecase) StartMonitoring(ctx context.Context, codes []stock.StockCode)`（goroutine per code）

- [ ] **Step 1: テストを先に書く（testify/mock使用）**

`go-api/internal/usecase/monitor_stocks_test.go`:
```go
package usecase_test

import (
	"context"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockPriceFetcher struct{ mock.Mock }

func (m *MockPriceFetcher) FetchLatest(code stock.StockCode) (stock.Price, error) {
	args := m.Called(code)
	return args.Get(0).(stock.Price), args.Error(1)
}

type MockPriceCache struct{ mock.Mock }

func (m *MockPriceCache) Push(code stock.StockCode, price stock.Price) error {
	return m.Called(code, price).Error(0)
}

func (m *MockPriceCache) GetHistory(code stock.StockCode, n int) ([]stock.Price, error) {
	args := m.Called(code, n)
	return args.Get(0).([]stock.Price), args.Error(1)
}

func TestMonitorUsecase_CheckStock_DetectsAnomaly(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	priceCache := &MockPriceCache{}

	code, _ := stock.NewStockCode("7203")

	// history: 29件 交互 90/110（mean≈99.65, stddev≈10）
	// current: 130（z≈3.0 → anomaly）
	history := make([]stock.Price, 29)
	for i := range history {
		if i%2 == 0 {
			history[i] = stock.Price(90)
		} else {
			history[i] = stock.Price(110)
		}
	}
	allPrices := append(history, stock.Price(130))

	fetcher.On("FetchLatest", code).Return(stock.Price(130.0), nil)
	priceCache.On("Push", code, stock.Price(130.0)).Return(nil)
	priceCache.On("GetHistory", code, 30).Return(allPrices, nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, priceCache, svc, 2.5)

	detected, zScore, err := uc.CheckStock(context.Background(), code)
	require.NoError(t, err)
	assert.True(t, detected, "expected anomaly detection")
	assert.True(t, zScore.IsAnomaly(2.5), "z=%.3f should be anomaly", zScore)

	fetcher.AssertExpectations(t)
	priceCache.AssertExpectations(t)
}

func TestMonitorUsecase_CheckStock_InsufficientHistory(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	priceCache := &MockPriceCache{}

	code, _ := stock.NewStockCode("6758")
	fetcher.On("FetchLatest", code).Return(stock.Price(5000.0), nil)
	priceCache.On("Push", code, stock.Price(5000.0)).Return(nil)
	// 30件未満（5件）を返す → 検知しない
	priceCache.On("GetHistory", code, 30).Return([]stock.Price{5000, 5010, 4990, 5005, 5000}, nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, priceCache, svc, 2.5)

	detected, _, err := uc.CheckStock(context.Background(), code)
	require.NoError(t, err)
	assert.False(t, detected, "insufficient history should not trigger detection")
}
```

- [ ] **Step 2: テストが失敗することを確認**

```bash
cd go-api && go test ./internal/usecase/...
```
Expected: FAIL（usecase パッケージが存在しない）

- [ ] **Step 3: MonitorUsecaseを実装**

`go-api/internal/usecase/monitor_stocks.go`:
```go
package usecase

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

const historySize = 30

type MonitorUsecase struct {
	fetcher   stock.PriceFetcher
	cache     stock.PriceCache
	detector  *anomaly.DetectionService
	threshold float64
}

func NewMonitorUsecase(
	fetcher stock.PriceFetcher,
	cache stock.PriceCache,
	detector *anomaly.DetectionService,
	threshold float64,
) *MonitorUsecase {
	return &MonitorUsecase{
		fetcher:   fetcher,
		cache:     cache,
		detector:  detector,
		threshold: threshold,
	}
}

// CheckStock は1銘柄の価格を取得・キャッシュし、Z-scoreを計算して異常判定する。
// 履歴が30件未満の場合は detected=false を返す（ウォームアップ期間）。
func (u *MonitorUsecase) CheckStock(ctx context.Context, code stock.StockCode) (bool, anomaly.ZScore, error) {
	price, err := u.fetcher.FetchLatest(code)
	if err != nil {
		return false, 0, fmt.Errorf("fetch %s: %w", code, err)
	}

	if err := u.cache.Push(code, price); err != nil {
		return false, 0, fmt.Errorf("cache push %s: %w", code, err)
	}

	prices, err := u.cache.GetHistory(code, historySize)
	if err != nil {
		return false, 0, fmt.Errorf("cache get %s: %w", code, err)
	}

	if len(prices) < historySize {
		return false, 0, nil
	}

	floatPrices := make([]float64, len(prices))
	for i, p := range prices {
		floatPrices[i] = float64(p)
	}

	z, err := u.detector.Calculate(floatPrices)
	if err != nil {
		return false, 0, fmt.Errorf("z-score %s: %w", code, err)
	}

	return z.IsAnomaly(u.threshold), z, nil
}

// StartMonitoring はgoroutineで全銘柄を並行監視する。
// ctx がキャンセルされるまでブロックする。
func (u *MonitorUsecase) StartMonitoring(ctx context.Context, codes []stock.StockCode) {
	var wg sync.WaitGroup
	for _, code := range codes {
		wg.Add(1)
		go func(code stock.StockCode) {
			defer wg.Done()
			ticker := time.NewTicker(1 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
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

- [ ] **Step 4: テストが通ることを確認**

```bash
cd go-api && go test ./internal/usecase/...
```
Expected: PASS（2件）

- [ ] **Step 5: コミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add go-api/internal/usecase/
git commit -m "feat: implement monitor stocks usecase with goroutine-per-stock"
```

---

### Task 9: メインサーバー（DI組み立て）

**Files:**
- Modify: `go-api/cmd/api/main.go`

**Interfaces:**
- Consumes: 全インフラ実装 + ユースケース
- Produces: 実行可能バイナリ

**必要な環境変数:**

| 変数名 | 説明 | 例 |
|-------|------|-----|
| `REDIS_URL` | Redis接続URL | `redis://localhost:6379` |
| `JQUANTS_EMAIL` | J-Quants登録メール | `user@example.com` |
| `JQUANTS_PASSWORD` | J-Quantsパスワード | `secret` |
| `STOCK_CODES` | 監視銘柄カンマ区切り | `7203,6758,9984` |
| `ALERT_THRESHOLD` | Z-score閾値（省略可・デフォルト2.5） | `2.5` |

- [ ] **Step 1: main.goを実装**

`go-api/cmd/api/main.go`:
```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/redis/go-redis/v9"
	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/cache"
	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	redisURL := mustEnv("REDIS_URL")
	jQuantsEmail := mustEnv("JQUANTS_EMAIL")
	jQuantsPassword := mustEnv("JQUANTS_PASSWORD")
	stockCodesRaw := mustEnv("STOCK_CODES")

	threshold := 2.5
	if t := os.Getenv("ALERT_THRESHOLD"); t != "" {
		var err error
		threshold, err = strconv.ParseFloat(t, 64)
		if err != nil {
			log.Fatalf("invalid ALERT_THRESHOLD: %v", err)
		}
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("invalid REDIS_URL: %v", err)
	}
	redisClient := redis.NewClient(opt)

	priceCache := cache.NewRedisPriceCache(redisClient)
	priceFetcher := gateway.NewJQuantsClient(jQuantsEmail, jQuantsPassword)
	detector := anomaly.NewDetectionService()
	monitor := usecase.NewMonitorUsecase(priceFetcher, priceCache, detector, threshold)

	var codes []stock.StockCode
	for _, s := range strings.Split(stockCodesRaw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		code, err := stock.NewStockCode(s)
		if err != nil {
			log.Printf("skip invalid stock code %q: %v", s, err)
			continue
		}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		log.Fatal("no valid stock codes in STOCK_CODES")
	}

	log.Printf("monitoring %d stocks (threshold=%.1fσ)", len(codes), threshold)
	monitor.StartMonitoring(ctx, codes)
	log.Println("monitoring stopped")
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env %s is not set", key)
	}
	return v
}
```

- [ ] **Step 2: ビルド確認**

```bash
cd go-api && go build ./...
```
Expected: エラーなし

- [ ] **Step 3: 全unitテスト確認**

```bash
cd go-api && go test -short ./...
```
Expected: PASS（全unitテストが通り、integrationテストはSKIP）

- [ ] **Step 4: コミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add go-api/cmd/
git commit -m "feat: wire up main.go with dependency injection and graceful shutdown"
```

---

### Task 10: GitHub Actions CI

**Files:**
- Create: `.github/workflows/go-test.yml`

**Interfaces:**
- Consumes: `push` / `pull_request` イベント（`go-api/**` 変更時のみ）
- Produces: unitテスト + integrationテストが自動実行される

- [ ] **Step 1: GitHub Actionsワークフローを作成**

`.github/workflows/go-test.yml`:
```yaml
name: Go Test

on:
  push:
    paths:
      - 'go-api/**'
  pull_request:
    paths:
      - 'go-api/**'

jobs:
  test:
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: stock_anomaly_test
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
        ports:
          - 5432:5432

      redis:
        image: redis:7
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
        ports:
          - 6379:6379

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
          cache: true
          cache-dependency-path: go-api/go.sum

      - name: Run unit tests
        working-directory: go-api
        run: go test -short ./...

      - name: Run integration tests
        working-directory: go-api
        run: go test ./...
        env:
          DATABASE_URL: postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable
          REDIS_URL: redis://localhost:6379
```

- [ ] **Step 2: ファイルを作成してコミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add .github/
git commit -m "ci: add github actions workflow for go tests with postgres and redis services"
```

---

## スペックカバレッジ確認

| 要件 | タスク |
|------|--------|
| goroutineで複数銘柄を並行監視 | Task 8 `StartMonitoring` |
| J-Quants APIで株価取得 | Task 7 |
| Redisに直近30件の終値をキャッシュ | Task 6 |
| Z-score 2.5σ事前フィルタリング | Task 3, 8 |
| PostgreSQLスキーマ作成 | Task 4 |
| Go Clean Architecture + 実践的DDD | Task 1〜9（全体構造） |
| table driven tests | Task 2, 3, 6, 7, 8 |
| testing.Short()でintegrationテストスキップ | Task 4, 5, 6 |
| GitHub Actions CI（postgres/redis services） | Task 10 |

**Phase 1スコープ外（後続フェーズ）:**
- Python分析エンジン呼び出し（HTTP同期）→ Phase 2
- RSI/MACD/ボリンジャーバンド計算 → Phase 2
- Finnhub News API → Phase 2
- Claude API + Slack通知 → Phase 3
- ユーザー管理・JWT認証・HTTPサーバー → Phase 4
- Dockerfileとデプロイ → Phase 5
