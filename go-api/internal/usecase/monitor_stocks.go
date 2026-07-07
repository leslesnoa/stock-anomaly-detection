package usecase

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

const historySize = 30

type MonitorUsecase struct {
	fetcher   stock.PriceFetcher
	cache     stock.PriceCache
	detector  *anomaly.DetectionService
	threshold float64
}

func NewMonitorUsecase(
	fetcher stock.PriceFetcher,
	cache stock.PriceCache,
	detector *anomaly.DetectionService,
	threshold float64,
) *MonitorUsecase {
	return &MonitorUsecase{
		fetcher:   fetcher,
		cache:     cache,
		detector:  detector,
		threshold: threshold,
	}
}

// CheckStock は1銘柄の価格を取得・キャッシュし、Z-scoreを計算して異常判定する。
// 履歴が30件未満の場合は detected=false を返す（ウォームアップ期間）。
func (u *MonitorUsecase) CheckStock(ctx context.Context, code stock.StockCode) (bool, anomaly.ZScore, error) {
	quote, err := u.fetcher.FetchLatest(code)
	if err != nil {
		return false, 0, fmt.Errorf("fetch %s: %w", code, err)
	}

	lastDate, err := u.cache.LastDate(code)
	if err != nil {
		return false, 0, fmt.Errorf("last date %s: %w", code, err)
	}
	// 同一取引日は既に取り込み済み。重複を積むと標準偏差が歪むため Push しない。
	if quote.Date == lastDate {
		return false, 0, nil
	}

	if err := u.cache.Push(code, quote.Price); err != nil {
		return false, 0, fmt.Errorf("cache push %s: %w", code, err)
	}
	if err := u.cache.SetLastDate(code, quote.Date); err != nil {
		return false, 0, fmt.Errorf("set last date %s: %w", code, err)
	}

	prices, err := u.cache.GetHistory(code, historySize)
	if err != nil {
		return false, 0, fmt.Errorf("cache get %s: %w", code, err)
	}

	if len(prices) < historySize {
		return false, 0, nil
	}

	floatPrices := make([]float64, len(prices))
	for i, p := range prices {
		floatPrices[i] = float64(p)
	}

	z, err := u.detector.Calculate(floatPrices)
	if err != nil {
		return false, 0, fmt.Errorf("z-score %s: %w", code, err)
	}

	return z.IsAnomaly(u.threshold), z, nil
}

// StartMonitoring はgoroutineで全銘柄を並行監視する。
// ctx がキャンセルされるまでブロックする。
func (u *MonitorUsecase) StartMonitoring(ctx context.Context, codes []stock.StockCode) {
	var wg sync.WaitGroup
	for _, code := range codes {
		wg.Add(1)
		go func(code stock.StockCode) {
			defer wg.Done()
			ticker := time.NewTicker(1 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					detected, z, err := u.CheckStock(ctx, code)
					if err != nil {
						log.Printf("ERROR monitoring %s: %v", code, err)
						continue
					}
					if detected {
						log.Printf("ANOMALY %s z=%.2f (threshold=%.1f)", code, z, u.threshold)
					}
				}
			}
		}(code)
	}
	wg.Wait()
}
