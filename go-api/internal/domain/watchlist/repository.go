package watchlist

import (
	"context"
	"errors"
)

var ErrAlreadyExists = errors.New("stock already in watchlist")
var ErrNotFound = errors.New("watchlist item not found")

type Repository interface {
	FindByUserID(ctx context.Context, userID string) ([]Watchlist, error)
	Create(ctx context.Context, w Watchlist) (Watchlist, error)
	Delete(ctx context.Context, id, userID string) error
	UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error
}
