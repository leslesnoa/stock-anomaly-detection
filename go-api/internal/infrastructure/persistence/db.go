package persistence

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func Connect(ctx context.Context, databaseURL string) (*pgx.Conn, error) {
	return pgx.Connect(ctx, databaseURL)
}
