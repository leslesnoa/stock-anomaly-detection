package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func (r *PgWatchlistRepository) Create(ctx context.Context, w watchlist.Watchlist) (watchlist.Watchlist, error) {
	err := r.conn.QueryRow(ctx,
		`INSERT INTO watchlist (user_id, stock_code, alert_threshold) VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		w.UserID, w.StockCode.String(), w.AlertThreshold,
	).Scan(&w.ID, &w.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return watchlist.Watchlist{}, watchlist.ErrAlreadyExists
		}
		return watchlist.Watchlist{}, err
	}
	return w, nil
}

func (r *PgWatchlistRepository) Delete(ctx context.Context, id, userID string) error {
	tag, err := r.conn.Exec(ctx,
		`DELETE FROM watchlist WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return watchlist.ErrNotFound
	}
	return nil
}

func (r *PgWatchlistRepository) UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error {
	tag, err := r.conn.Exec(ctx,
		`UPDATE watchlist SET alert_threshold = $1 WHERE id = $2 AND user_id = $3`,
		threshold, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return watchlist.ErrNotFound
	}
	return nil
}
