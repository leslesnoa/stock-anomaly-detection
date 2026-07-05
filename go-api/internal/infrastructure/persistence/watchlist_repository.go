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
