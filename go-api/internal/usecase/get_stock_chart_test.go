package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/forecast"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func buildQuotes(n int) []stock.Quote {
	quotes := make([]stock.Quote, n)
	for i := 0; i < n; i++ {
		quotes[i] = stock.Quote{Price: stock.Price(1000 + float64(i)), Date: fmt.Sprintf("day-%03d", i)}
	}
	return quotes
}

func newChartUsecase(t *testing.T, watchlists *mockWatchlistRepository, prices *MockPriceRepository, forecaster *MockForecaster, notifications *MockNotificationRepository) *usecase.GetStockChartUsecase {
	t.Helper()
	return usecase.NewGetStockChartUsecase(watchlists, prices, anomaly.NewDetectionService(), forecaster, notifications, 2.5)
}

func TestGetStockChartUsecase_Success(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{StockCode: code}}, nil)

	quotes := buildQuotes(130)
	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).Return(quotes, nil)

	forecaster := new(MockForecaster)
	forecaster.On("Forecast", mock.Anything, code, mock.Anything, mock.Anything).
		Return(forecast.Forecast{Horizon: 20, Points: []forecast.Point{{Step: 1, Center: 1200}}}, nil)

	notifications := new(MockNotificationRepository)
	notifications.On("FindByStockCode", mock.Anything, "7203").
		Return([]notification.Notification{{StockCode: "7203", AnomalyScore: 3.1}}, nil)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	chart, err := uc.Handle(context.Background(), "user-1", "7203")

	require.NoError(t, err)
	assert.Equal(t, "7203", chart.StockCode)
	require.NotNil(t, chart.CurrentPrice)
	assert.InDelta(t, 1129.0, *chart.CurrentPrice, 0.001)

	// アラート帯: window=30なので index 29..129 の101件、先頭は history=quotes[0:29]（0..28の29整数）から計算
	require.Len(t, chart.AlertBand, 101)
	assert.Equal(t, "day-029", chart.AlertBand[0].Date)
	assert.InDelta(t, 1034.9165, chart.AlertBand[0].Upper, 0.01)
	assert.InDelta(t, 993.0835, chart.AlertBand[0].Lower, 0.01)

	require.NotNil(t, chart.CurrentZScore)
	assert.InDelta(t, 1.79284, float64(*chart.CurrentZScore), 0.01)

	require.NotNil(t, chart.Forecast)
	assert.Equal(t, 20, chart.Forecast.Horizon)
	require.Len(t, chart.Forecast.Points, 1)
	assert.InDelta(t, 1200.0, chart.Forecast.Points[0].Center, 0.001)

	require.Len(t, chart.Notifications, 1)
	assert.Equal(t, "7203", chart.Notifications[0].StockCode)
}

func TestGetStockChartUsecase_InsufficientHistory_ForecastAndZScoreNil(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{StockCode: code}}, nil)

	quotes := buildQuotes(20)
	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).Return(quotes, nil)

	// forecaster には期待を設定しない: 呼ばれたらtestifyのmockが未設定呼び出しとして失敗させる
	forecaster := new(MockForecaster)
	notifications := new(MockNotificationRepository)
	notifications.On("FindByStockCode", mock.Anything, "7203").Return([]notification.Notification{}, nil)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	chart, err := uc.Handle(context.Background(), "user-1", "7203")

	require.NoError(t, err)
	require.NotNil(t, chart.CurrentPrice)
	assert.Empty(t, chart.AlertBand)
	assert.Nil(t, chart.CurrentZScore)
	assert.Nil(t, chart.Forecast)
	forecaster.AssertNotCalled(t, "Forecast", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestGetStockChartUsecase_ForecasterError_ForecastNilButNoError(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{StockCode: code}}, nil)

	quotes := buildQuotes(130)
	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).Return(quotes, nil)

	forecaster := new(MockForecaster)
	forecaster.On("Forecast", mock.Anything, code, mock.Anything, mock.Anything).
		Return(forecast.Forecast{}, errors.New("python engine down"))

	notifications := new(MockNotificationRepository)
	notifications.On("FindByStockCode", mock.Anything, "7203").Return([]notification.Notification{}, nil)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	chart, err := uc.Handle(context.Background(), "user-1", "7203")

	require.NoError(t, err)
	assert.Nil(t, chart.Forecast)
	require.NotNil(t, chart.CurrentZScore)
}

func TestGetStockChartUsecase_NotInWatchlist_ReturnsNotFound(t *testing.T) {
	otherCode, _ := stock.NewStockCode("9984")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{StockCode: otherCode}}, nil)

	prices := new(MockPriceRepository)
	forecaster := new(MockForecaster)
	notifications := new(MockNotificationRepository)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	_, err := uc.Handle(context.Background(), "user-1", "7203")

	require.Error(t, err)
	assert.ErrorIs(t, err, watchlist.ErrNotFound)
	prices.AssertNotCalled(t, "FindRecent", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetStockChartUsecase_InvalidStockCode_ReturnsError(t *testing.T) {
	watchlists := new(mockWatchlistRepository)
	prices := new(MockPriceRepository)
	forecaster := new(MockForecaster)
	notifications := new(MockNotificationRepository)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	_, err := uc.Handle(context.Background(), "user-1", "12")

	require.Error(t, err)
	assert.ErrorIs(t, err, stock.ErrInvalidStockCode)
	watchlists.AssertNotCalled(t, "FindByUserID", mock.Anything, mock.Anything)
}

// TestGetStockChartUsecase_AlertBandConsistentWithDetector は
// computeAlertBand（get_stock_chart.go）が独自に再実装している平均・標準偏差の計算が、
// anomaly.DetectionService.Calculate と同じ窓（末尾30件、先頭29件が履歴）で
// 同じ統計量に一致し続けることを保証する回帰ガード。computeAlertBand自体は変更しない
// （anomaly/service.go同様、このテストのみでバインドする）。
func TestGetStockChartUsecase_AlertBandConsistentWithDetector(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{StockCode: code}}, nil)

	quotes := buildQuotes(35)
	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).Return(quotes, nil)

	// forecastMinPrices(121) 未満なので forecaster は呼ばれない
	forecaster := new(MockForecaster)
	notifications := new(MockNotificationRepository)
	notifications.On("FindByStockCode", mock.Anything, "7203").Return([]notification.Notification{}, nil)

	uc := newChartUsecase(t, watchlists, prices, forecaster, notifications)
	chart, err := uc.Handle(context.Background(), "user-1", "7203")
	require.NoError(t, err)
	require.NotEmpty(t, chart.AlertBand)

	// computeAlertBandの末尾要素と同じ窓（末尾30件、先頭29件が履歴）を
	// anomaly.DetectionService.Calculate に渡し、統計量が一致することを確認する。
	window := quotes[len(quotes)-30:]
	windowPrices := make([]float64, len(window))
	for i, q := range window {
		windowPrices[i] = float64(q.Price)
	}

	detector := anomaly.NewDetectionService()
	z, err := detector.Calculate(windowPrices)
	require.NoError(t, err)

	history := windowPrices[:len(windowPrices)-1]
	mean := 0.0
	for _, p := range history {
		mean += p
	}
	mean /= float64(len(history))

	variance := 0.0
	for _, p := range history {
		diff := p - mean
		variance += diff * diff
	}
	stddev := math.Sqrt(variance / float64(len(history)))

	current := windowPrices[len(windowPrices)-1]
	expectedZ := (current - mean) / stddev
	assert.InDelta(t, expectedZ, float64(z), 1e-9)

	last := chart.AlertBand[len(chart.AlertBand)-1]
	const threshold = 2.5 // newChartUsecase に渡している閾値と一致させる
	assert.InDelta(t, mean+threshold*stddev, last.Upper, 1e-9)
	assert.InDelta(t, mean-threshold*stddev, last.Lower, 1e-9)

	require.NotNil(t, chart.CurrentZScore)
	assert.InDelta(t, expectedZ, float64(*chart.CurrentZScore), 1e-9)
}
