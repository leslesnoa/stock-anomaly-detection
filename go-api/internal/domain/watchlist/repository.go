package watchlist

import "context"

type Repository interface {
	FindByUserID(ctx context.Context, userID string) ([]Watchlist, error)
}
