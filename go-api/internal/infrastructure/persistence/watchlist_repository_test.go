package persistence_test

import (
	"context"
	"os"
	"testing"

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
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, "TRUNCATE watchlist CASCADE")
	require.NoError(t, err)

	repo := persistence.NewPgWatchlistRepository(conn)
	watchlists, err := repo.FindByUserID(ctx, "00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.NotNil(t, watchlists)
	assert.IsType(t, []watchlist.Watchlist{}, watchlists)
}
