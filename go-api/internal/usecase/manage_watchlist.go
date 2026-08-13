package usecase

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

const defaultAlertThreshold = 2.5

type ManageWatchlistUsecase struct {
	watchlists watchlist.Repository
}

func NewManageWatchlistUsecase(watchlists watchlist.Repository) *ManageWatchlistUsecase {
	return &ManageWatchlistUsecase{watchlists: watchlists}
}

func (u *ManageWatchlistUsecase) Add(ctx context.Context, userID, rawStockCode string, threshold float64) (watchlist.Watchlist, error) {
	code, err := stock.NewStockCode(rawStockCode)
	if err != nil {
		return watchlist.Watchlist{}, err
	}
	if threshold == 0 {
		threshold = defaultAlertThreshold
	}
	return u.watchlists.Create(ctx, watchlist.Watchlist{
		UserID:         userID,
		StockCode:      code,
		AlertThreshold: threshold,
	})
}

func (u *ManageWatchlistUsecase) Remove(ctx context.Context, userID, watchlistID string) error {
	return u.watchlists.Delete(ctx, watchlistID, userID)
}

func (u *ManageWatchlistUsecase) UpdateThreshold(ctx context.Context, userID, watchlistID string, threshold float64) error {
	return u.watchlists.UpdateThreshold(ctx, watchlistID, userID, threshold)
}

func (u *ManageWatchlistUsecase) List(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	return u.watchlists.FindByUserID(ctx, userID)
}
