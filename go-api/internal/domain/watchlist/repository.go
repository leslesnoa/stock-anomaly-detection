package watchlist

import "context"

type Repository interface {
	FindAll(ctx context.Context) ([]Watchlist, error)
}
