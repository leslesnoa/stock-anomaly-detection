package usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

// backfillDays はバックフィルで取得する営業日数。約5年分。
// 監視に必要なのは historySize（30件）だけだが、チャート表示・ボラティリティ推定に
// 加えて、AI方向分類器（python-engine、direction_model.py）の学習に必要な
// 独立サンプル数（MIN_INDEPENDENT_SAMPLES=30、日付単位で数える）を確保するには
// 実測で約1200営業日分の履歴が要る（900日で32件、1200日で44件）。
// 2026-09-29以前は500日（約2年、AI方向分類器の学習には不足していた）だった。
const backfillDays = 1200

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

// Run はcodeの価格履歴がhistorySize件未満の場合のみ過去backfillDays件を取得して保存する。
// 既にhistorySize件以上の履歴が存在する場合（同一銘柄を別ユーザーが既に監視中、または
// 過去のバックフィルが正常に完了済み）は何もしない。
func (u *BackfillPriceHistoryUsecase) Run(ctx context.Context, code stock.StockCode) error {
	existing, err := u.prices.FindRecent(ctx, code, historySize)
	if err != nil {
		return fmt.Errorf("check existing history %s: %w", code, err)
	}
	// 監視ウィンドウ（historySize件）を満たしていない銘柄は取り直す。
	// 「1件でもあればスキップ」にすると、003適用前の起動やYahoo障害でバックフィルが
	// 失敗した直後に日次ポーリングが1件だけ書き込んだ場合、以降どの再起動でも
	// スキップされ続け、異常検知が約30営業日ぶん無言で止まる。
	// SaveAll は ON CONFLICT DO NOTHING なので、再取得しても既存行は壊れない。
	if len(existing) >= historySize {
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

// RunAll は与えられた銘柄を逐次バックフィルする。銘柄間に interval のウェイトを
// 挟むのは、Yahoo Finance が非公式APIでレート制限が明記されておらず、
// 起動直後に全銘柄分を一斉に投げるとIPブロックされるリスクがあるため。
//
// 個別銘柄の失敗はログに留めて次へ進む。1銘柄の取得失敗で
// 残り全銘柄の移行が止まる方が運用上まずい。
// ctx がキャンセルされたら即座に打ち切る。
func (u *BackfillPriceHistoryUsecase) RunAll(ctx context.Context, codes []stock.StockCode, interval time.Duration) {
	for i, code := range codes {
		if ctx.Err() != nil {
			return
		}
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
		if err := u.Run(ctx, code); err != nil {
			log.Printf("ERROR backfill price history %s: %v", code, err)
		}
	}
}
