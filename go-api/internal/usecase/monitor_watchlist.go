package usecase

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

// RunWithDynamicWatchlist はDBのwatchlistテーブルから監視対象銘柄を定期的に読み込み、
// 銘柄セットが変化するたびに監視を再起動する。ctxがキャンセルされるまでブロックする。
func (u *MonitorUsecase) RunWithDynamicWatchlist(
	ctx context.Context,
	repo watchlist.Repository,
	hour, minute int,
	refreshInterval time.Duration,
) {
	var (
		currentCodes []stock.StockCode
		cancel       context.CancelFunc
		wg           sync.WaitGroup
	)

	restart := func(codes []stock.StockCode) {
		if cancel != nil {
			cancel()
			wg.Wait()
		}
		var childCtx context.Context
		childCtx, cancel = context.WithCancel(ctx)
		currentCodes = codes
		if len(codes) == 0 {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			u.startFn(childCtx, codes, hour, minute)
		}()
	}

	initial, err := repo.FindAllStockCodes(ctx)
	if err != nil {
		log.Printf("ERROR fetch watchlist codes: %v", err)
		initial = nil
	}
	restart(initial)

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if cancel != nil {
				cancel()
				wg.Wait()
			}
			return
		case <-ticker.C:
			codes, err := repo.FindAllStockCodes(ctx)
			if err != nil {
				log.Printf("ERROR fetch watchlist codes: %v", err)
				continue
			}
			if codesChanged(currentCodes, codes) {
				restart(codes)
			}
		}
	}
}

// codesChanged は2つの銘柄コード集合が（順序を無視して）異なるかを判定する。
func codesChanged(a, b []stock.StockCode) bool {
	if len(a) != len(b) {
		return true
	}
	sortedA := append([]stock.StockCode{}, a...)
	sortedB := append([]stock.StockCode{}, b...)
	sort.Slice(sortedA, func(i, j int) bool { return sortedA[i] < sortedA[j] })
	sort.Slice(sortedB, func(i, j int) bool { return sortedB[i] < sortedB[j] })
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return true
		}
	}
	return false
}
