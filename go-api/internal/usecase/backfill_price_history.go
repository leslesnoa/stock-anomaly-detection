package usecase

import (
	"context"
	"fmt"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

// backfillDays はバックフィルで取得する営業日数。約2年分。
// 監視に必要なのは historySize（30件）だけだが、チャート表示と
// ボラティリティ推定に十分な長さの履歴を最初の1回で揃えておく。
const backfillDays = 500

// BackfillPriceHistoryUsecase は新規watchlist追加時に、過去の値動きを
// daily_prices へ一括投入する。異常検知が有効になるまでのウォームアップ期間
// （historySize件分の日次ポーリング待ち）を短縮し、同時にチャート描画に
// 必要な長期履歴も用意する。
type BackfillPriceHistoryUsecase struct {
	fetcher stock.PriceFetcher
	prices  stock.PriceRepository
}

func NewBackfillPriceHistoryUsecase(fetcher stock.PriceFetcher, prices stock.PriceRepository) *BackfillPriceHistoryUsecase {
	return &BackfillPriceHistoryUsecase{fetcher: fetcher, prices: prices}
}

// Run はcodeの価格履歴が空の場合のみ過去backfillDays件を取得して保存する。
// 既に履歴が存在する場合（同一銘柄を別ユーザーが既に監視中）は何もしない。
func (u *BackfillPriceHistoryUsecase) Run(ctx context.Context, code stock.StockCode) error {
	existing, err := u.prices.FindRecent(ctx, code, 1)
	if err != nil {
		return fmt.Errorf("check existing history %s: %w", code, err)
	}
	if len(existing) > 0 {
		return nil
	}

	quotes, err := u.fetcher.FetchHistory(code, backfillDays)
	if err != nil {
		return fmt.Errorf("fetch history %s: %w", code, err)
	}
	if err := u.prices.SaveAll(ctx, code, quotes); err != nil {
		return fmt.Errorf("save history %s: %w", code, err)
	}
	return nil
}
