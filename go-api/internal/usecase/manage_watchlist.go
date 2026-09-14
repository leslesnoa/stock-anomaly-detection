package usecase

import (
	"context"
	"errors"
	"log"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

const defaultAlertThreshold = 2.5

var ErrInvalidThreshold = errors.New("alert threshold must be positive")

type ManageWatchlistUsecase struct {
	watchlists watchlist.Repository
	backfiller *BackfillPriceHistoryUsecase
}

func NewManageWatchlistUsecase(watchlists watchlist.Repository, backfiller *BackfillPriceHistoryUsecase) *ManageWatchlistUsecase {
	return &ManageWatchlistUsecase{watchlists: watchlists, backfiller: backfiller}
}

// Add はwatchlistへ銘柄を登録し、backfillerが設定されていれば価格履歴の
// バックフィルを試みる。バックフィル失敗はログのみに留め、登録自体は成功として返す。
func (u *ManageWatchlistUsecase) Add(ctx context.Context, userID, rawStockCode string, threshold float64) (watchlist.Watchlist, error) {
	code, err := stock.NewStockCode(rawStockCode)
	if err != nil {
		return watchlist.Watchlist{}, err
	}
	if threshold == 0 {
		threshold = defaultAlertThreshold
	}
	w, err := u.watchlists.Create(ctx, watchlist.Watchlist{
		UserID:         userID,
		StockCode:      code,
		AlertThreshold: threshold,
	})
	if err != nil {
		return watchlist.Watchlist{}, err
	}
	if u.backfiller != nil {
		if err := u.backfiller.Run(code); err != nil {
			log.Printf("ERROR backfill price history %s: %v", code, err)
		}
	}
	return w, nil
}

func (u *ManageWatchlistUsecase) Remove(ctx context.Context, userID, watchlistID string) error {
	return u.watchlists.Delete(ctx, watchlistID, userID)
}

func (u *ManageWatchlistUsecase) UpdateThreshold(ctx context.Context, userID, watchlistID string, threshold float64) error {
	if threshold <= 0 {
		return ErrInvalidThreshold
	}
	return u.watchlists.UpdateThreshold(ctx, watchlistID, userID, threshold)
}

func (u *ManageWatchlistUsecase) List(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	return u.watchlists.FindByUserID(ctx, userID)
}
