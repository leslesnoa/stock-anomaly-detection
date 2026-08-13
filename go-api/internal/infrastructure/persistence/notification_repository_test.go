package persistence_test

import (
	"context"
	"os"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
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
