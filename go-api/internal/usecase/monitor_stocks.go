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

// jst は日本市場の実行時刻計算に使う固定タイムゾーン（夏時間なし）。
var jst = time.FixedZone("JST", 9*60*60)

// nextPollTime は now 以降で最初に来る JST hour:minute の時刻を返す。
// now がちょうど実行時刻の場合は翌日を返す。
func nextPollTime(now time.Time, hour, minute int) time.Time {
	n := now.In(jst)
	next := time.Date(n.Year(), n.Month(), n.Day(), hour, minute, 0, 0, jst)
	if !next.After(n) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

type MonitorUsecase struct {
	fetcher       stock.PriceFetcher
	cache         stock.PriceCache
	detector      *anomaly.DetectionService
	threshold     float64
	notifyUsecase *AnalyzeAndNotifyUsecase
	startFn       func(ctx context.Context, codes []stock.StockCode, hour, minute int)
}

func NewMonitorUsecase(
	fetcher stock.PriceFetcher,
	cache stock.PriceCache,
	detector *anomaly.DetectionService,
	threshold float64,
	notifyUsecase *AnalyzeAndNotifyUsecase,
) *MonitorUsecase {
	u := &MonitorUsecase{
		fetcher:       fetcher,
		cache:         cache,
		detector:      detector,
		threshold:     threshold,
		notifyUsecase: notifyUsecase,
	}
	u.startFn = u.StartMonitoring
	return u
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

// notify は異常検知後、AnalyzeAndNotifyUsecase が設定されていれば
// 直近の価格履歴を取得してAI分析・Slack通知パイプラインを起動する。
func (u *MonitorUsecase) notify(ctx context.Context, code stock.StockCode, z anomaly.ZScore) {
	if u.notifyUsecase == nil {
		return
	}
	prices, err := u.cache.GetHistory(code, historySize)
	if err != nil || len(prices) == 0 {
		log.Printf("ERROR fetch history for notify %s: %v", code, err)
		return
	}
	floatPrices := make([]float64, len(prices))
	for i, p := range prices {
		floatPrices[i] = float64(p)
	}
	currentPrice := floatPrices[len(floatPrices)-1]
	if err := u.notifyUsecase.Handle(ctx, code, float64(z), currentPrice, floatPrices); err != nil {
		log.Printf("ERROR analyze and notify %s: %v", code, err)
	}
}

// StartMonitoring はgoroutineで全銘柄を並行監視する。
// 各銘柄は毎日 JST hour:minute（大引け後の想定）に1回チェックする。
// ctx がキャンセルされるまでブロックする。
func (u *MonitorUsecase) StartMonitoring(ctx context.Context, codes []stock.StockCode, hour, minute int) {
	var wg sync.WaitGroup
	for _, code := range codes {
		wg.Add(1)
		go func(code stock.StockCode) {
			defer wg.Done()
			for {
				timer := time.NewTimer(time.Until(nextPollTime(time.Now(), hour, minute)))
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
					detected, z, err := u.CheckStock(ctx, code)
					if err != nil {
						log.Printf("ERROR monitoring %s: %v", code, err)
						continue
					}
					if detected {
						log.Printf("ANOMALY %s z=%.2f (threshold=%.1f)", code, z, u.threshold)
						u.notify(ctx, code, z)
					}
				}
			}
		}(code)
	}
	wg.Wait()
}
