package persistence_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPgWatchlistRepository_FindByUserID_Empty(t *testing.T) {
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
	watchlists, err := repo.FindByUserID(ctx, "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.NotNil(t, watchlists)
	assert.IsType(t, []watchlist.Watchlist{}, watchlists)
}

func insertTestUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, email string) string {
	t.Helper()
	var id string
	err := conn.QueryRow(ctx,
		"INSERT INTO users (email, password_hash) VALUES ($1, 'hashed') RETURNING id",
		email,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

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

	created, err := repo.Create(ctx, watchlist.Watchlist{UserID: userID, StockCode: code, AlertThreshold: 3.0})
	require.NoError(t, err)
	assert.NotEmpty(t, created.ID)

	found, err := repo.FindByUserID(ctx, userID)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, code, found[0].StockCode)
	assert.Equal(t, 3.0, found[0].AlertThreshold)
}

func TestPgWatchlistRepository_Create_Duplicate(t *testing.T) {
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

	userID := insertTestUser(t, ctx, conn, "dup-owner@example.com")
	repo := persistence.NewPgWatchlistRepository(conn)
	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)

	_, err = repo.Create(ctx, watchlist.Watchlist{UserID: userID, StockCode: code, AlertThreshold: 2.5})
	require.NoError(t, err)
	_, err = repo.Create(ctx, watchlist.Watchlist{UserID: userID, StockCode: code, AlertThreshold: 2.5})
	require.ErrorIs(t, err, watchlist.ErrAlreadyExists)
}

func TestPgWatchlistRepository_Delete(t *testing.T) {
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

	ownerID := insertTestUser(t, ctx, conn, "delete-owner@example.com")
	otherID := insertTestUser(t, ctx, conn, "delete-other@example.com")
	repo := persistence.NewPgWatchlistRepository(conn)
	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)
	created, err := repo.Create(ctx, watchlist.Watchlist{UserID: ownerID, StockCode: code, AlertThreshold: 2.5})
	require.NoError(t, err)

	err = repo.Delete(ctx, created.ID, otherID)
	assert.ErrorIs(t, err, watchlist.ErrNotFound)

	err = repo.Delete(ctx, created.ID, ownerID)
	require.NoError(t, err)

	found, err := repo.FindByUserID(ctx, ownerID)
	require.NoError(t, err)
	assert.Empty(t, found)
}

func TestPgWatchlistRepository_UpdateThreshold(t *testing.T) {
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

	userID := insertTestUser(t, ctx, conn, "update-owner@example.com")
	repo := persistence.NewPgWatchlistRepository(conn)
	code, err := stock.NewStockCode("7203")
	require.NoError(t, err)
	created, err := repo.Create(ctx, watchlist.Watchlist{UserID: userID, StockCode: code, AlertThreshold: 2.5})
	require.NoError(t, err)

	err = repo.UpdateThreshold(ctx, created.ID, userID, 4.0)
	require.NoError(t, err)

	found, err := repo.FindByUserID(ctx, userID)
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, 4.0, found[0].AlertThreshold)
}
