package watchlist

import "context"

type Repository interface {
	FindByUserID(ctx context.Context, userID int64) ([]Watchlist, error)
}
