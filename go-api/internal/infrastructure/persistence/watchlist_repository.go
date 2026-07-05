package persistence

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

type PgWatchlistRepository struct {
	conn *pgx.Conn
}

func NewPgWatchlistRepository(conn *pgx.Conn) *PgWatchlistRepository {
	return &PgWatchlistRepository{conn: conn}
}

func (r *PgWatchlistRepository) FindByUserID(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT id, user_id, stock_code, alert_threshold, created_at FROM watchlist WHERE user_id = $1`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := []watchlist.Watchlist{}
	for rows.Next() {
		var w watchlist.Watchlist
		var rawCode string
		if err := rows.Scan(&w.ID, &w.UserID, &rawCode, &w.AlertThreshold, &w.CreatedAt); err != nil {
			return nil, err
		}
		sc, err := stock.NewStockCode(rawCode)
		if err != nil {
			return nil, fmt.Errorf("invalid stock_code in DB: %w", err)
		}
		w.StockCode = sc
		result = append(result, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
