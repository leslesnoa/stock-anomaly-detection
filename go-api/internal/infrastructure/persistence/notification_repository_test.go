package persistence_test

import (
	"context"
	"os"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
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
	defer conn.Close()

	_, err = conn.Exec(ctx, "TRUNCATE notifications CASCADE")
	require.NoError(t, err)

	repo := persistence.NewPgNotificationRepository(conn)
	rsi := 65.5
	n := notification.Notification{
		StockCode:           "7203",
		AnomalyScore:        3.2,
		AIReport:            "テストレポート",
		TechnicalIndicators: analysis.Indicators{RSI: &rsi},
		SlackSent:           true,
	}
	err = repo.Save(ctx, n)
	require.NoError(t, err)

	var count int
	err = conn.QueryRow(ctx, "SELECT COUNT(*) FROM notifications WHERE stock_code = $1", "7203").Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestPgNotificationRepository_FindByStockCode(t *testing.T) {
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

	_, err = conn.Exec(ctx, "TRUNCATE notifications CASCADE")
	require.NoError(t, err)

	repo := persistence.NewPgNotificationRepository(conn)
	rsi := 65.5
	require.NoError(t, repo.Save(ctx, notification.Notification{
		StockCode:           "7203",
		AnomalyScore:        3.2,
		AIReport:            "テストレポート",
		TechnicalIndicators: analysis.Indicators{RSI: &rsi},
		SlackSent:           true,
	}))
	require.NoError(t, repo.Save(ctx, notification.Notification{
		StockCode:    "9984",
		AnomalyScore: 2.8,
	}))

	found, err := repo.FindByStockCode(ctx, "7203")
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "7203", found[0].StockCode)
	assert.Equal(t, "テストレポート", found[0].AIReport)
	require.NotNil(t, found[0].TechnicalIndicators.RSI)
	assert.InDelta(t, 65.5, *found[0].TechnicalIndicators.RSI, 0.001)
	assert.True(t, found[0].SlackSent)
	assert.False(t, found[0].NotifiedAt.IsZero())
}

func TestPgNotificationRepository_FindByStockCode_NoResults(t *testing.T) {
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

	_, err = conn.Exec(ctx, "TRUNCATE notifications CASCADE")
	require.NoError(t, err)

	repo := persistence.NewPgNotificationRepository(conn)
	found, err := repo.FindByStockCode(ctx, "0000")
	require.NoError(t, err)
	assert.Empty(t, found)
}
