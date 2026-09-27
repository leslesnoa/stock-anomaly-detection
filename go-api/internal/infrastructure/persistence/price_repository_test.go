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
