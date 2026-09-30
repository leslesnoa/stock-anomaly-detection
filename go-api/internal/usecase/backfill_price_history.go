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

// backfillCompletionMargin はスキップ判定でbackfillDaysちょうどではなく多少の欠け
// （上場から日が浅い銘柄、取引停止日、Yahoo側の集計誤差等）を許容するための下限
// マージン。実測ではrange=5yで約1221件返るためbackfillDays(1200)との差はわずか
// 約21件しかなく、マージンが無いと際どい銘柄が毎回再取得され続けてしまう。
const backfillCompletionMargin = 30

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

// Run はcodeの価格履歴がbackfillDays件（マージンを許容）未満の場合は過去backfillDays件を
// 取得して保存する。既に十分な履歴が存在する場合（同一銘柄を別ユーザーが既に監視中、または
// 過去のバックフィルが正常に完了済み）は何もしない。
func (u *BackfillPriceHistoryUsecase) Run(ctx context.Context, code stock.StockCode) error {
	existing, err := u.prices.FindRecent(ctx, code, backfillDays)
	if err != nil {
		return fmt.Errorf("check existing history %s: %w", code, err)
	}
	// 目標件数（backfillDays、マージン込み）を満たしていない銘柄は取り直す。「1件でも
	// あればスキップ」にすると、バックフィル失敗直後に日次ポーリングが1件だけ書き込んだ
	// 場合、以降どの再起動でもスキップされ続け履歴が増えなくなる。2026-09-29以前は
	// historySize（30件）を基準にしていたが、AI方向分類器の学習に必要な独立サンプル数を
	// 確保するためbackfillDays（1200件、マージンを引いた1170件以上でスキップ）基準に
	// 引き上げた。これにより、旧backfillDays=500時代に既にバックフィル済みだった既存
	// watchlist銘柄も、次回起動時に1200件まで再取得される。上場来データがマージン込みの
	// 閾値（1170営業日）に満たない銘柄は、取得できる全件を保存した状態のまま以降の起動でも
	// 再取得が走り続けるが（1銘柄あたり起動ごとにYahoo Finance呼び出し1回）、SaveAll が
	// ON CONFLICT DO NOTHINGのため実害はない。
	if len(existing) >= backfillDays-backfillCompletionMargin {
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
