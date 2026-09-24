package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockPriceFetcher struct{ mock.Mock }

func (m *MockPriceFetcher) FetchLatest(code stock.StockCode) (stock.Quote, error) {
	args := m.Called(code)
	return args.Get(0).(stock.Quote), args.Error(1)
}

func (m *MockPriceFetcher) FetchHistory(code stock.StockCode, days int) ([]stock.Quote, error) {
	args := m.Called(code, days)
	return args.Get(0).([]stock.Quote), args.Error(1)
}

func TestMonitorUsecase_CheckStock_DetectsAnomaly(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	ctx := context.Background()

	code, _ := stock.NewStockCode("7203")

	// history: 29件 交互 90/110（mean≈99.65, stddev≈10）
	// current: 130（z≈3.0 → anomaly）
	allQuotes := make([]stock.Quote, 0, 30)
	historyStart := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 29; i++ {
		p := stock.Price(110)
		if i%2 == 0 {
			p = stock.Price(90)
		}
		date := historyStart.AddDate(0, 0, i).Format("2006-01-02")
		allQuotes = append(allQuotes, stock.Quote{Price: p, Date: date})
	}
	allQuotes = append(allQuotes, stock.Quote{Price: 130, Date: "2026-07-07"})

	fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 130.0, Date: "2026-07-07"}, nil)
	prices.On("LatestDate", ctx, code).Return("2026-07-06", nil)
	prices.On("Save", ctx, code, stock.Quote{Price: 130.0, Date: "2026-07-07"}).Return(nil)
	prices.On("FindRecent", ctx, code, 30).Return(allQuotes, nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, prices, svc, 2.5, nil)

	detected, zScore, err := uc.CheckStock(ctx, code)
	require.NoError(t, err)
	assert.True(t, detected, "expected anomaly detection")
	assert.True(t, zScore.IsAnomaly(2.5), "z=%.3f should be anomaly", zScore)

	fetcher.AssertExpectations(t)
	prices.AssertExpectations(t)
}

func TestMonitorUsecase_CheckStock_InsufficientHistory(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	ctx := context.Background()

	code, _ := stock.NewStockCode("6758")
	fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 5000.0, Date: "2026-07-07"}, nil)
	prices.On("LatestDate", ctx, code).Return("2026-07-06", nil)
	prices.On("Save", ctx, code, stock.Quote{Price: 5000.0, Date: "2026-07-07"}).Return(nil)
	// 30件未満（5件）を返す → 検知しない
	prices.On("FindRecent", ctx, code, 30).Return([]stock.Quote{
		{Price: 5000, Date: "2026-07-01"},
		{Price: 5010, Date: "2026-07-02"},
		{Price: 4990, Date: "2026-07-03"},
		{Price: 5005, Date: "2026-07-06"},
		{Price: 5000, Date: "2026-07-07"},
	}, nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, prices, svc, 2.5, nil)

	detected, _, err := uc.CheckStock(ctx, code)
	require.NoError(t, err)
	assert.False(t, detected, "insufficient history should not trigger detection")
}

func TestMonitorUsecase_CheckStock_SkipsDuplicateDate(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	ctx := context.Background()

	code, _ := stock.NewStockCode("7203")
	// 取得したバーの取引日が、保存済みの最新取引日と同じ
	fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 3250.0, Date: "2026-07-07"}, nil)
	prices.On("LatestDate", ctx, code).Return("2026-07-07", nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, prices, svc, 2.5, nil)

	detected, _, err := uc.CheckStock(ctx, code)
	require.NoError(t, err)
	assert.False(t, detected, "duplicate trading date must not be saved")

	// Save / FindRecent は呼ばれない
	prices.AssertNotCalled(t, "Save", mock.Anything, mock.Anything, mock.Anything)
	prices.AssertNotCalled(t, "FindRecent", mock.Anything, mock.Anything, mock.Anything)
	fetcher.AssertExpectations(t)
}
