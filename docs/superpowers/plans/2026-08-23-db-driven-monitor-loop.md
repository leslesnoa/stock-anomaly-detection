# 監視ループのDB駆動化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 監視ループが`STOCK_CODES`環境変数の固定銘柄リストではなく、DBの`watchlist`テーブル（全ユーザー横断）から動的に監視対象を取得するように切り替え、フロントエンドでwatchlistに追加した銘柄が実際に監視されるようにする。

**Architecture:** `MonitorUsecase`に新メソッド`RunWithDynamicWatchlist`を追加する。5分ごとに`watchlist.Repository.FindAllStockCodes`で全ユーザーの重複除去済み銘柄リストを取得し、前回と比較。変化があれば現行の監視（既存の`StartMonitoring`が起動しているper-code goroutine群）を丸ごとcontext cancelし、新しいリストで丸ごと再起動する（差分更新ではなく一括再起動方式）。既存の`StartMonitoring`・`CheckStock`のロジックは変更しない。

**Tech Stack:** Go 1.x, pgx/v5, testify (mock/assert/require)

**Spec:** `docs/superpowers/specs/2026-08-23-db-driven-monitor-loop-design.md`

## Global Constraints

- `Watchlist.AlertThreshold`は今回のタスクでは使用しない。異常検知の閾値は引き続き`ANOMALY_THRESHOLD`環境変数によるグローバル固定値（`MonitorUsecase.threshold`）を使う。
- 銘柄リストのリフレッシュ間隔は5分固定。環境変数化はしない。
- `STOCK_CODES`環境変数は完全廃止する。
- DB取得失敗時は直前のリストを維持し、エラーログのみ出力して監視を止めない。
- goroutineの並行制御を含むため、テストは`-race`フラグ必須。
- 全ての作業は`go-api/`ディレクトリを基準にした相対パスで行う。

---

### Task 1: `watchlist.Repository`にFindAllStockCodesを追加

**Files:**
- Modify: `go-api/internal/domain/watchlist/repository.go`
- Modify: `go-api/internal/infrastructure/persistence/watchlist_repository.go`
- Modify: `go-api/internal/infrastructure/persistence/watchlist_repository_test.go`
- Modify: `go-api/internal/usecase/manage_watchlist_test.go`（`mockWatchlistRepository`にメソッド追加、コンパイルを通すために必須）

**Interfaces:**
- Produces: `watchlist.Repository.FindAllStockCodes(ctx context.Context) ([]stock.StockCode, error)` — Task 2, 3が使う

- [ ] **Step 1: 失敗する統合テストを書く**

`go-api/internal/infrastructure/persistence/watchlist_repository_test.go`の末尾に追加：

```go
func TestPgWatchlistRepository_FindAllStockCodes(t *testing.T) {
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

	user1 := insertTestUser(t, ctx, conn, "codes-user1@example.com")
	user2 := insertTestUser(t, ctx, conn, "codes-user2@example.com")
	repo := persistence.NewPgWatchlistRepository(conn)

	code7203, err := stock.NewStockCode("7203")
	require.NoError(t, err)
	code9984, err := stock.NewStockCode("9984")
	require.NoError(t, err)

	// user1: 7203, user2: 7203（重複）と9984 を登録
	_, err = repo.Create(ctx, watchlist.Watchlist{UserID: user1, StockCode: code7203, AlertThreshold: 2.5})
	require.NoError(t, err)
	_, err = repo.Create(ctx, watchlist.Watchlist{UserID: user2, StockCode: code7203, AlertThreshold: 2.5})
	require.NoError(t, err)
	_, err = repo.Create(ctx, watchlist.Watchlist{UserID: user2, StockCode: code9984, AlertThreshold: 2.5})
	require.NoError(t, err)

	codes, err := repo.FindAllStockCodes(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []stock.StockCode{code7203, code9984}, codes)
}

func TestPgWatchlistRepository_FindAllStockCodes_Empty(t *testing.T) {
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
	_, err = conn.Exec(ctx, "TRUNCATE watchlist CASCADE")
	require.NoError(t, err)

	repo := persistence.NewPgWatchlistRepository(conn)
	codes, err := repo.FindAllStockCodes(ctx)
	require.NoError(t, err)
	assert.Empty(t, codes)
}
```

- [ ] **Step 2: テスト実行で失敗確認**

Run (`go-api/`ディレクトリで):
```bash
DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test ./internal/infrastructure/persistence/... -run TestPgWatchlistRepository_FindAllStockCodes -v
```
Expected: コンパイルエラー `repo.FindAllStockCodes undefined`

- [ ] **Step 3: domain interfaceにメソッドを追加**

`go-api/internal/domain/watchlist/repository.go`を編集し、importに`"github.com/stock-anomaly-detection/go-api/internal/domain/stock"`を追加、`Repository`インターフェースに以下を追加：

```go
type Repository interface {
	FindByUserID(ctx context.Context, userID string) ([]Watchlist, error)
	FindAllStockCodes(ctx context.Context) ([]stock.StockCode, error)
	Create(ctx context.Context, w Watchlist) (Watchlist, error)
	Delete(ctx context.Context, id, userID string) error
	UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error
}
```

- [ ] **Step 4: PgWatchlistRepositoryに実装を追加**

`go-api/internal/infrastructure/persistence/watchlist_repository.go`の末尾に追加：

```go
func (r *PgWatchlistRepository) FindAllStockCodes(ctx context.Context) ([]stock.StockCode, error) {
	rows, err := r.conn.Query(ctx, `SELECT DISTINCT stock_code FROM watchlist`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []stock.StockCode{}
	for rows.Next() {
		var rawCode string
		if err := rows.Scan(&rawCode); err != nil {
			return nil, err
		}
		sc, err := stock.NewStockCode(rawCode)
		if err != nil {
			return nil, fmt.Errorf("invalid stock_code in DB: %w", err)
		}
		result = append(result, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
```

- [ ] **Step 5: mockWatchlistRepositoryにメソッドを追加（コンパイルを通すため）**

`go-api/internal/usecase/manage_watchlist_test.go`の`mockWatchlistRepository`メソッド群に追加：

```go
func (m *mockWatchlistRepository) FindAllStockCodes(ctx context.Context) ([]stock.StockCode, error) {
	args := m.Called(ctx)
	return args.Get(0).([]stock.StockCode), args.Error(1)
}
```

- [ ] **Step 6: テスト実行で成功確認**

Run:
```bash
DATABASE_URL=postgres://postgres:postgres@localhost:5432/stock_anomaly_test?sslmode=disable go test ./internal/infrastructure/persistence/... -run TestPgWatchlistRepository_FindAllStockCodes -v
```
Expected: PASS（DATABASE_URLが未設定の環境ではSKIP）

- [ ] **Step 7: 全体の単体テストでリグレッションがないことを確認**

Run:
```bash
go test -race -short ./...
```
Expected: PASS（全パッケージ）

- [ ] **Step 8: コミット**

```bash
git add internal/domain/watchlist/repository.go \
        internal/infrastructure/persistence/watchlist_repository.go \
        internal/infrastructure/persistence/watchlist_repository_test.go \
        internal/usecase/manage_watchlist_test.go
git commit -m "feat: add FindAllStockCodes to watchlist repository"
```

---

### Task 2: MonitorUsecase.RunWithDynamicWatchlistの実装

**Files:**
- Modify: `go-api/internal/usecase/monitor_stocks.go`（`startFn`フィールド追加）
- Create: `go-api/internal/usecase/monitor_watchlist.go`
- Create: `go-api/internal/usecase/monitor_watchlist_internal_test.go`

**Interfaces:**
- Consumes: `watchlist.Repository.FindAllStockCodes(ctx) ([]stock.StockCode, error)`（Task 1で追加）、既存の`MonitorUsecase.StartMonitoring(ctx, codes []stock.StockCode, hour, minute int)`
- Produces: `MonitorUsecase.RunWithDynamicWatchlist(ctx, repo watchlist.Repository, hour, minute int, refreshInterval time.Duration)` — Task 3が使う

- [ ] **Step 1: MonitorUsecaseにテスト用フックフィールドを追加**

`go-api/internal/usecase/monitor_stocks.go`の`MonitorUsecase`構造体とコンストラクタを編集：

```go
type MonitorUsecase struct {
	fetcher       stock.PriceFetcher
	cache         stock.PriceCache
	detector      *anomaly.DetectionService
	threshold     float64
	notifyUsecase *AnalyzeAndNotifyUsecase
	startFn       func(ctx context.Context, codes []stock.StockCode, hour, minute int)
}

func NewMonitorUsecase(
	fetcher stock.PriceFetcher,
	cache stock.PriceCache,
	detector *anomaly.DetectionService,
	threshold float64,
	notifyUsecase *AnalyzeAndNotifyUsecase,
) *MonitorUsecase {
	u := &MonitorUsecase{
		fetcher:       fetcher,
		cache:         cache,
		detector:      detector,
		threshold:     threshold,
		notifyUsecase: notifyUsecase,
	}
	u.startFn = u.StartMonitoring
	return u
}
```

（`startFn`は本番では常に`u.StartMonitoring`を指す。内部テストでのみ差し替えてスパイとして使う。)

- [ ] **Step 2: 既存テストでリグレッションがないことを確認**

Run:
```bash
go test -race -short ./internal/usecase/...
```
Expected: PASS

- [ ] **Step 3: codesChangedヘルパーの失敗するテストを書く**

新規作成 `go-api/internal/usecase/monitor_watchlist_internal_test.go`：

```go
package usecase

import (
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stretchr/testify/assert"
)

func TestCodesChanged(t *testing.T) {
	code1, _ := stock.NewStockCode("7203")
	code2, _ := stock.NewStockCode("9984")

	cases := []struct {
		name string
		a, b []stock.StockCode
		want bool
	}{
		{"同じ順序", []stock.StockCode{code1, code2}, []stock.StockCode{code1, code2}, false},
		{"順序違いのみ", []stock.StockCode{code1, code2}, []stock.StockCode{code2, code1}, false},
		{"件数が違う", []stock.StockCode{code1}, []stock.StockCode{code1, code2}, true},
		{"内容が違う", []stock.StockCode{code1}, []stock.StockCode{code2}, true},
		{"両方空", []stock.StockCode{}, []stock.StockCode{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, codesChanged(tc.a, tc.b))
		})
	}
}
```

- [ ] **Step 4: テスト実行で失敗確認**

Run:
```bash
go test -race -short ./internal/usecase/... -run TestCodesChanged -v
```
Expected: コンパイルエラー `undefined: codesChanged`

- [ ] **Step 5: codesChangedを実装**

新規作成 `go-api/internal/usecase/monitor_watchlist.go`（ファイル冒頭部分）：

```go
package usecase

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

// codesChanged は2つの銘柄コード集合が（順序を無視して）異なるかを判定する。
func codesChanged(a, b []stock.StockCode) bool {
	if len(a) != len(b) {
		return true
	}
	sortedA := append([]stock.StockCode{}, a...)
	sortedB := append([]stock.StockCode{}, b...)
	sort.Slice(sortedA, func(i, j int) bool { return sortedA[i] < sortedA[j] })
	sort.Slice(sortedB, func(i, j int) bool { return sortedB[i] < sortedB[j] })
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return true
		}
	}
	return false
}
```

- [ ] **Step 6: テスト実行で成功確認**

Run:
```bash
go test -race -short ./internal/usecase/... -run TestCodesChanged -v
```
Expected: PASS（5ケース全て）

- [ ] **Step 7: RunWithDynamicWatchlistの失敗するテストを書く**

`go-api/internal/usecase/monitor_watchlist_internal_test.go`に追加（importに`"context"`, `"errors"`, `"sync"`, `"time"`, `"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"`, `"github.com/stretchr/testify/require"`を追加）：

```go
type fakeWatchlistRepo struct {
	mu      sync.Mutex
	seq     [][]stock.StockCode
	calls   int
	errFrom int // 1-indexed; 0 means never error
}

func (f *fakeWatchlistRepo) FindAllStockCodes(ctx context.Context) ([]stock.StockCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.errFrom > 0 && f.calls >= f.errFrom {
		return nil, errors.New("db error")
	}
	idx := f.calls - 1
	if idx >= len(f.seq) {
		idx = len(f.seq) - 1
	}
	return f.seq[idx], nil
}
func (f *fakeWatchlistRepo) FindByUserID(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	return nil, nil
}
func (f *fakeWatchlistRepo) Create(ctx context.Context, w watchlist.Watchlist) (watchlist.Watchlist, error) {
	return watchlist.Watchlist{}, nil
}
func (f *fakeWatchlistRepo) Delete(ctx context.Context, id, userID string) error { return nil }
func (f *fakeWatchlistRepo) UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error {
	return nil
}

type startCall struct {
	codes []stock.StockCode
}

func TestRunWithDynamicWatchlist_RestartsOnChange(t *testing.T) {
	code1, _ := stock.NewStockCode("7203")
	code2, _ := stock.NewStockCode("9984")

	repo := &fakeWatchlistRepo{seq: [][]stock.StockCode{
		{code1},        // initial fetch
		{code1},        // 変化なし
		{code1, code2}, // 変化あり -> restart
	}}

	var mu sync.Mutex
	var calls []startCall
	u := &MonitorUsecase{}
	u.startFn = func(ctx context.Context, codes []stock.StockCode, hour, minute int) {
		mu.Lock()
		calls = append(calls, startCall{codes: codes})
		mu.Unlock()
		<-ctx.Done()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	u.RunWithDynamicWatchlist(ctx, repo, 16, 0, 30*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(calls), 2)
	assert.Equal(t, []stock.StockCode{code1}, calls[0].codes)
	assert.Equal(t, []stock.StockCode{code1, code2}, calls[len(calls)-1].codes)
}

func TestRunWithDynamicWatchlist_KeepsListOnFetchError(t *testing.T) {
	code1, _ := stock.NewStockCode("7203")
	repo := &fakeWatchlistRepo{
		seq:     [][]stock.StockCode{{code1}},
		errFrom: 2, // 2回目以降のfetchは失敗させる
	}

	var mu sync.Mutex
	var calls []startCall
	u := &MonitorUsecase{}
	u.startFn = func(ctx context.Context, codes []stock.StockCode, hour, minute int) {
		mu.Lock()
		calls = append(calls, startCall{codes: codes})
		mu.Unlock()
		<-ctx.Done()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	u.RunWithDynamicWatchlist(ctx, repo, 16, 0, 30*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, calls, 1)
	assert.Equal(t, []stock.StockCode{code1}, calls[0].codes)
}

func TestRunWithDynamicWatchlist_ReturnsOnContextCancel(t *testing.T) {
	repo := &fakeWatchlistRepo{seq: [][]stock.StockCode{{}}}
	u := &MonitorUsecase{}
	u.startFn = func(ctx context.Context, codes []stock.StockCode, hour, minute int) {}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		u.RunWithDynamicWatchlist(ctx, repo, 16, 0, 10*time.Millisecond)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("RunWithDynamicWatchlist did not return after context cancel")
	}
}
```

- [ ] **Step 8: テスト実行で失敗確認**

Run:
```bash
go test -race -short ./internal/usecase/... -run TestRunWithDynamicWatchlist -v
```
Expected: コンパイルエラー `undefined: (*MonitorUsecase).RunWithDynamicWatchlist`

- [ ] **Step 9: RunWithDynamicWatchlistを実装**

`go-api/internal/usecase/monitor_watchlist.go`に追加（`codesChanged`関数の前に配置）：

```go
// RunWithDynamicWatchlist はDBのwatchlistテーブルから監視対象銘柄を定期的に読み込み、
// 銘柄セットが変化するたびに監視を再起動する。ctxがキャンセルされるまでブロックする。
func (u *MonitorUsecase) RunWithDynamicWatchlist(
	ctx context.Context,
	repo watchlist.Repository,
	hour, minute int,
	refreshInterval time.Duration,
) {
	var (
		currentCodes []stock.StockCode
		cancel       context.CancelFunc
		wg           sync.WaitGroup
	)

	restart := func(codes []stock.StockCode) {
		if cancel != nil {
			cancel()
			wg.Wait()
		}
		var childCtx context.Context
		childCtx, cancel = context.WithCancel(ctx)
		currentCodes = codes
		if len(codes) == 0 {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			u.startFn(childCtx, codes, hour, minute)
		}()
	}

	initial, err := repo.FindAllStockCodes(ctx)
	if err != nil {
		log.Printf("ERROR fetch watchlist codes: %v", err)
		initial = nil
	}
	restart(initial)

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if cancel != nil {
				cancel()
				wg.Wait()
			}
			return
		case <-ticker.C:
			codes, err := repo.FindAllStockCodes(ctx)
			if err != nil {
				log.Printf("ERROR fetch watchlist codes: %v", err)
				continue
			}
			if codesChanged(currentCodes, codes) {
				restart(codes)
			}
		}
	}
}
```

- [ ] **Step 10: テスト実行で成功確認**

Run:
```bash
go test -race -short ./internal/usecase/... -run TestRunWithDynamicWatchlist -v
```
Expected: PASS（3ケース全て）

- [ ] **Step 11: パッケージ全体のテストを実行**

Run:
```bash
go test -race -short ./...
```
Expected: PASS（全パッケージ）

- [ ] **Step 12: コミット**

```bash
git add internal/usecase/monitor_stocks.go \
        internal/usecase/monitor_watchlist.go \
        internal/usecase/monitor_watchlist_internal_test.go
git commit -m "feat: add MonitorUsecase.RunWithDynamicWatchlist"
```

---

### Task 3: main.goの配線をSTOCK_CODESからDB駆動に切り替え

**Files:**
- Modify: `go-api/cmd/api/main.go`

**Interfaces:**
- Consumes: `MonitorUsecase.RunWithDynamicWatchlist(ctx, repo, hour, minute, refreshInterval)`（Task 2で追加）、既存の`persistence.NewPgWatchlistRepository(pool)`（既にPhase 4で`watchlistRepo`として生成済み）

- [ ] **Step 1: STOCK_CODES関連コードを削除し、RunWithDynamicWatchlist呼び出しに置き換え**

`go-api/cmd/api/main.go`から以下を削除：
- `stockCodesRaw := mustEnv("STOCK_CODES")`（32行目）

以下のブロック（127〜146行目、`var codes []stock.StockCode`から`log.Println("monitoring stopped")`の手前まで）を置き換え：

削除対象：
```go
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

	log.Printf("monitoring %d stocks (threshold=%.1fσ, poll=%02d:%02d JST)", len(codes), threshold, pollHour, pollMinute)
	monitor.StartMonitoring(ctx, codes, pollHour, pollMinute)
	log.Println("monitoring stopped")
```

置き換え後：
```go
	log.Printf("monitoring watchlist stocks (threshold=%.1fσ, poll=%02d:%02d JST, refresh=%s)",
		threshold, pollHour, pollMinute, watchlistRefreshInterval)
	monitor.RunWithDynamicWatchlist(ctx, watchlistRepo, pollHour, pollMinute, watchlistRefreshInterval)
	log.Println("monitoring stopped")
```

`const watchlistRefreshInterval = 5 * time.Minute`を`func main()`の直前（パッケージレベル）に追加：

```go
const watchlistRefreshInterval = 5 * time.Minute

func main() {
```

importから`"strings"`と`"github.com/stock-anomaly-detection/go-api/internal/domain/stock"`を削除（他の箇所で使われていないため）。

- [ ] **Step 2: ビルド確認**

Run:
```bash
go build ./...
```
Expected: エラーなし（未使用importがあればここで検出される）

- [ ] **Step 3: 単体テスト実行**

Run:
```bash
go test -race -short ./...
```
Expected: PASS

- [ ] **Step 4: コミット**

```bash
git add cmd/api/main.go
git commit -m "feat: switch monitor loop to DB-driven watchlist"
```

---

### Task 4: CLAUDE.mdの更新と最終確認

**Files:**
- Modify: `CLAUDE.md`（リポジトリルート）

- [ ] **Step 1: 環境変数一覧からSTOCK_CODESを削除**

`## 環境変数（本番）`セクションの行：
```
DATABASE_URL, REDIS_URL, JQUANTS_API_KEY, STOCK_CODES（カンマ区切り4桁コード）, ANOMALY_THRESHOLD（デフォルト2.5）, FINNHUB_API_KEY, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）, JWT_SECRET, PORT（デフォルト8080）
```
を以下に置き換え：
```
DATABASE_URL, REDIS_URL, JQUANTS_API_KEY, ANOMALY_THRESHOLD（デフォルト2.5）, FINNHUB_API_KEY, ANTHROPIC_API_KEY, SLACK_WEBHOOK_URL, PYTHON_ENGINE_URL, CLAUDE_MODEL（デフォルト claude-opus-5）, JWT_SECRET, PORT（デフォルト8080）
```

- [ ] **Step 2: 重要な設計決定セクションに一行追加**

`## 重要な設計決定`セクション内、Phase 3の行の直後に追加：
```
- 監視対象銘柄はDBの`watchlist`テーブル（全ユーザー横断、重複除去）から動的に取得する。`MonitorUsecase.RunWithDynamicWatchlist`が5分間隔で再読込し、銘柄セットに変化があれば監視を再起動する（`STOCK_CODES`環境変数は廃止）。異常検知の閾値は`Watchlist.AlertThreshold`ではなく引き続き`ANOMALY_THRESHOLD`のグローバル固定値を使う
```

- [ ] **Step 3: 最終ビルド・テスト確認**

Run（`go-api/`ディレクトリで）:
```bash
go build ./... && go test -race -short ./...
```
Expected: 両方エラーなし・PASS

- [ ] **Step 4: コミット**

```bash
cd /Users/watanabekeisuke/Documents/stock-anomaly-detection
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for DB-driven monitor loop"
```
