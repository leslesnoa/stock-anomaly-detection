package usecase

import (
	"context"
	"log"
	"math"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/forecast"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

// forecastMinPrices はPython engineの /forecast が要求する最低価格件数
// （推定ウィンドウ120営業日＋1）。これ未満の場合はPython engineを呼ばずforecastをnilにする。
const forecastMinPrices = 121

// forecastTimeout はPython engine呼び出しに独自に課す上限。go-apiのhttp.Server.WriteTimeout
// （main.goで30秒）より十分短くすることで、python-engineへの疎通が失敗して張り付いた場合でも
// WriteTimeoutが先に発火して未書き込みのままレスポンスが強制切断される事態を避け、
// 期限内にforecast抜きの縮退レスポンスを返せるようにする。
const forecastTimeout = 10 * time.Second

type AlertBandPoint struct {
	Date  string
	Upper float64
	Lower float64
}

type StockChart struct {
	StockCode     string
	CurrentPrice  *float64
	Prices        []stock.Quote
	AlertBand     []AlertBandPoint
	CurrentZScore *float64
	Forecast      *forecast.Forecast
	Notifications []notification.Notification
}

type GetStockChartUsecase struct {
	watchlists    watchlist.Repository
	prices        stock.PriceRepository
	detector      *anomaly.DetectionService
	forecaster    forecast.Forecaster
	notifications notification.Repository
	threshold     float64
}

func NewGetStockChartUsecase(
	watchlists watchlist.Repository,
	prices stock.PriceRepository,
	detector *anomaly.DetectionService,
	forecaster forecast.Forecaster,
	notifications notification.Repository,
	threshold float64,
) *GetStockChartUsecase {
	return &GetStockChartUsecase{
		watchlists:    watchlists,
		prices:        prices,
		detector:      detector,
		forecaster:    forecaster,
		notifications: notifications,
		threshold:     threshold,
	}
}

// Handle はuserIDのwatchlistにrawStockCodeが存在する場合のみチャートデータを返す。
// watchlistに無い銘柄は watchlist.ErrNotFound を返す（データの存在範囲と認可範囲を一致させるため、
// Yahooへの追加リクエスト経路を作らないため）。予測やzスコアに必要な件数の履歴が無い場合、
// または Python engine呼び出しが失敗した場合は該当フィールドを nil にするだけでエラーにはしない。
func (u *GetStockChartUsecase) Handle(ctx context.Context, userID, rawStockCode string) (StockChart, error) {
	code, err := stock.NewStockCode(rawStockCode)
	if err != nil {
		return StockChart{}, err
	}

	items, err := u.watchlists.FindByUserID(ctx, userID)
	if err != nil {
		return StockChart{}, err
	}
	owned := false
	for _, item := range items {
		if item.StockCode == code {
			owned = true
			break
		}
	}
	if !owned {
		return StockChart{}, watchlist.ErrNotFound
	}

	quotes, err := u.prices.FindRecent(ctx, code, backfillDays)
	if err != nil {
		return StockChart{}, err
	}

	chart := StockChart{
		StockCode: code.String(),
		Prices:    quotes,
		AlertBand: computeAlertBand(quotes, historySize, u.threshold),
	}

	if len(quotes) > 0 {
		current := float64(quotes[len(quotes)-1].Price)
		chart.CurrentPrice = &current
	}

	if len(quotes) >= historySize {
		floatPrices := make([]float64, historySize)
		for i, q := range quotes[len(quotes)-historySize:] {
			floatPrices[i] = float64(q.Price)
		}
		if z, err := u.detector.Calculate(floatPrices); err == nil {
			zf := float64(z)
			chart.CurrentZScore = &zf
		} else {
			log.Printf("WARN z-score calculation failed for %s: %v", code, err)
		}
	}

	if len(quotes) >= forecastMinPrices && chart.CurrentPrice != nil {
		floatPrices := make([]float64, len(quotes))
		for i, q := range quotes {
			floatPrices[i] = float64(q.Price)
		}
		forecastCtx, cancel := context.WithTimeout(ctx, forecastTimeout)
		f, err := u.forecaster.Forecast(forecastCtx, code, *chart.CurrentPrice, floatPrices)
		cancel()
		if err == nil {
			chart.Forecast = &f
		} else {
			log.Printf("WARN forecast failed for %s: %v", code, err)
		}
	}

	if notifications, err := u.notifications.FindByStockCode(ctx, code.String()); err == nil {
		chart.Notifications = notifications
	} else {
		log.Printf("WARN notification history fetch failed for %s: %v", code, err)
	}

	return chart, nil
}

// computeAlertBand は各時点について、その時点を「現在値」とみなしたときの
// anomaly.DetectionService.Calculate と同じ窓（末尾が現在値、先頭〜末尾-1が履歴）を使って
// アラート境界帯（履歴の平均±threshold×標準偏差）を計算する。window件に満たない
// 先頭部分は帯を計算できないため結果に含めない。anomaly/service.go 自体は変更しない。
func computeAlertBand(quotes []stock.Quote, window int, threshold float64) []AlertBandPoint {
	band := []AlertBandPoint{}
	for i := window - 1; i < len(quotes); i++ {
		history := quotes[i-window+1 : i]

		mean := 0.0
		for _, q := range history {
			mean += float64(q.Price)
		}
		mean /= float64(len(history))

		variance := 0.0
		for _, q := range history {
			diff := float64(q.Price) - mean
			variance += diff * diff
		}
		stddev := math.Sqrt(variance / float64(len(history)))

		band = append(band, AlertBandPoint{
			Date:  quotes[i].Date,
			Upper: mean + threshold*stddev,
			Lower: mean - threshold*stddev,
		})
	}
	return band
}
