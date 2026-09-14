package usecase

import (
	"fmt"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

// BackfillPriceHistoryUsecase は新規watchlist追加時に、過去の値動きを
// Redisの価格キャッシュへ一括投入し、異常検知が有効になるまでの
// ウォームアップ期間（historySize件分の日次ポーリング待ち）を短縮する。
type BackfillPriceHistoryUsecase struct {
	fetcher stock.PriceFetcher
	cache   stock.PriceCache
}

func NewBackfillPriceHistoryUsecase(fetcher stock.PriceFetcher, cache stock.PriceCache) *BackfillPriceHistoryUsecase {
	return &BackfillPriceHistoryUsecase{fetcher: fetcher, cache: cache}
}

// Run はcodeの価格履歴が空の場合のみ過去historySize件を取得してキャッシュへ積む。
// 既に履歴が存在する場合（同一銘柄を別ユーザーが既に監視中）は何もしない。
func (u *BackfillPriceHistoryUsecase) Run(code stock.StockCode) error {
	existing, err := u.cache.GetHistory(code, 1)
	if err != nil {
		return fmt.Errorf("check existing history %s: %w", code, err)
	}
	if len(existing) > 0 {
		return nil
	}

	quotes, err := u.fetcher.FetchHistory(code, historySize)
	if err != nil {
		return fmt.Errorf("fetch history %s: %w", code, err)
	}
	for _, q := range quotes {
		if err := u.cache.Push(code, q.Price); err != nil {
			return fmt.Errorf("cache push %s: %w", code, err)
		}
	}
	if len(quotes) == 0 {
		return nil
	}
	if err := u.cache.SetLastDate(code, quotes[len(quotes)-1].Date); err != nil {
		return fmt.Errorf("set last date %s: %w", code, err)
	}
	return nil
}
