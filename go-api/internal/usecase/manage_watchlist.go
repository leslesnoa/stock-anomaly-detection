package usecase

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

const defaultAlertThreshold = 2.5

var ErrInvalidThreshold = errors.New("alert threshold must be positive")

type ManageWatchlistUsecase struct {
	watchlists  watchlist.Repository
	backfiller  *BackfillPriceHistoryUsecase
	nameFetcher stock.NameFetcher
}

func NewManageWatchlistUsecase(watchlists watchlist.Repository, backfiller *BackfillPriceHistoryUsecase, nameFetcher stock.NameFetcher) *ManageWatchlistUsecase {
	return &ManageWatchlistUsecase{watchlists: watchlists, backfiller: backfiller, nameFetcher: nameFetcher}
}

// Add はwatchlistへ銘柄を登録する。nameFetcherが設定されていれば銘柄名の取得も試みる
// （失敗してもログのみに留め、空文字のまま登録を続行する）。backfillerが設定されて
// いれば価格履歴のバックフィルも試みる。バックフィル失敗もログのみに留め、登録自体は
// 成功として返す。
func (u *ManageWatchlistUsecase) Add(ctx context.Context, userID, rawStockCode string, threshold float64) (watchlist.Watchlist, error) {
	code, err := stock.NewStockCode(strings.ToUpper(strings.TrimSpace(rawStockCode)))
	if err != nil {
		return watchlist.Watchlist{}, err
	}
	if threshold == 0 {
		threshold = defaultAlertThreshold
	}

	var name string
	if u.nameFetcher != nil {
		name, err = u.nameFetcher.FetchCompanyName(code)
		if err != nil {
			log.Printf("ERROR fetch company name %s: %v", code, err)
			name = ""
		}
	}

	w, err := u.watchlists.Create(ctx, watchlist.Watchlist{
		UserID:         userID,
		StockCode:      code,
		StockName:      name,
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
