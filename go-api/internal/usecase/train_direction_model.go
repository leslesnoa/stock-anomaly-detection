package usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/directionmodel"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

// directionModelTrainTimeout はpython-engineの学習呼び出しに課す上限。
// ロジスティック回帰・全銘柄プールでも数百〜数千サンプル程度で、
// 既存の/forecast呼び出し（10秒）より重いが、数十秒あれば十分なはず。
const directionModelTrainTimeout = 60 * time.Second

// directionModelTrainRetryDelays は起動直後にpython-engineがまだ立ち上がっていない
// 場合のリトライ間隔。学習はモデル状態を持たない（失敗しても既存の統計手法に
// フォールバックするだけ）ため、BackfillPriceHistoryUsecaseほど強い自己回復は
// 不要だが、デプロイ直後の初回学習だけは取りこぼしたくないため数回リトライする。
var directionModelTrainRetryDelays = []time.Duration{1 * time.Minute, 5 * time.Minute, 15 * time.Minute}

type TrainDirectionModelUsecase struct {
	watchlists watchlist.Repository
	prices     stock.PriceRepository
	trainer    directionmodel.Trainer
}

func NewTrainDirectionModelUsecase(
	watchlists watchlist.Repository,
	prices stock.PriceRepository,
	trainer directionmodel.Trainer,
) *TrainDirectionModelUsecase {
	return &TrainDirectionModelUsecase{watchlists: watchlists, prices: prices, trainer: trainer}
}

// Run はwatchlist全銘柄の価格履歴を集めてpython-engineへ学習を依頼する。
// 履歴が1件も無い銘柄は除外する。全銘柄が対象外なら何もしない。
func (u *TrainDirectionModelUsecase) Run(ctx context.Context) error {
	codes, err := u.watchlists.FindAllStockCodes(ctx)
	if err != nil {
		return fmt.Errorf("fetch watchlist codes: %w", err)
	}

	var series []directionmodel.PriceSeries
	for _, code := range codes {
		quotes, err := u.prices.FindRecent(ctx, code, backfillDays)
		if err != nil {
			return fmt.Errorf("fetch price history %s: %w", code, err)
		}
		if len(quotes) == 0 {
			continue
		}
		series = append(series, directionmodel.PriceSeries{StockCode: code, Quotes: quotes})
	}
	if len(series) == 0 {
		return nil
	}

	trainCtx, cancel := context.WithTimeout(ctx, directionModelTrainTimeout)
	defer cancel()
	result, err := u.trainer.Train(trainCtx, series)
	if err != nil {
		return fmt.Errorf("train direction model: %w", err)
	}

	log.Printf(
		"direction model trained: adopted=%v hit_rate=%v baseline_hit_rate=%v p_value=%v samples=%v",
		result.Adopted, derefFloat(result.HitRate), derefFloat(result.BaselineHitRate),
		derefFloat(result.PValue), derefInt(result.IndependentSampleCount),
	)
	return nil
}

// RunWithRetry はRunが失敗した場合、directionModelTrainRetryDelaysの間隔でリトライする。
// python-engineがgo-apiより後に起動する場合のデプロイ順序問題に対応する。
// 全リトライ失敗時はログのみで次の定期サイクルへ委ねる（学習の失敗は既存機能を止めない）。
func (u *TrainDirectionModelUsecase) RunWithRetry(ctx context.Context) {
	if err := u.Run(ctx); err == nil {
		return
	} else {
		log.Printf("ERROR train direction model (attempt 1): %v", err)
	}

	for i, delay := range directionModelTrainRetryDelays {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if err := u.Run(ctx); err != nil {
			log.Printf("ERROR train direction model (attempt %d): %v", i+2, err)
			continue
		}
		return
	}
	log.Println("direction model training failed after all retries, will retry at next scheduled cycle")
}

// RunPeriodically はRunWithRetryを起動時に1回、以降interval間隔で実行し続ける。
// ctxがキャンセルされるまでブロックする。
func (u *TrainDirectionModelUsecase) RunPeriodically(ctx context.Context, interval time.Duration) {
	u.RunWithRetry(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			u.RunWithRetry(ctx)
		}
	}
}

func derefFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func derefInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
