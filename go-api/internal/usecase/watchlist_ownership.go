package usecase

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

// ensureInWatchlist は userID の watchlist に code が無ければ watchlist.ErrNotFound を返す。
// データの存在範囲と認可範囲を一致させ、未登録銘柄への外部API呼び出し経路を作らないため。
func ensureInWatchlist(ctx context.Context, watchlists watchlist.Repository, userID string, code stock.StockCode) error {
	items, err := watchlists.FindByUserID(ctx, userID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.StockCode == code {
			return nil
		}
	}
	return watchlist.ErrNotFound
}
