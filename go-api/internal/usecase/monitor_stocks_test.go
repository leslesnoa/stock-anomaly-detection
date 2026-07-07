package usecase_test

import (
	"context"
	"testing"

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

type MockPriceCache struct{ mock.Mock }

func (m *MockPriceCache) Push(code stock.StockCode, price stock.Price) error {
	return m.Called(code, price).Error(0)
}

func (m *MockPriceCache) GetHistory(code stock.StockCode, n int) ([]stock.Price, error) {
	args := m.Called(code, n)
	return args.Get(0).([]stock.Price), args.Error(1)
}

func (m *MockPriceCache) LastDate(code stock.StockCode) (string, error) {
	args := m.Called(code)
	return args.String(0), args.Error(1)
}

func (m *MockPriceCache) SetLastDate(code stock.StockCode, date string) error {
	return m.Called(code, date).Error(0)
}

func TestMonitorUsecase_CheckStock_DetectsAnomaly(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	priceCache := &MockPriceCache{}

	code, _ := stock.NewStockCode("7203")

	// history: 29件 交互 90/110（mean≈99.65, stddev≈10）
	// current: 130（z≈3.0 → anomaly）
	history := make([]stock.Price, 29)
	for i := range history {
		if i%2 == 0 {
			history[i] = stock.Price(90)
		} else {
			history[i] = stock.Price(110)
		}
	}
	allPrices := append(history, stock.Price(130))

	fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 130.0, Date: "2026-07-07"}, nil)
	priceCache.On("Push", code, stock.Price(130.0)).Return(nil)
	priceCache.On("GetHistory", code, 30).Return(allPrices, nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, priceCache, svc, 2.5)

	detected, zScore, err := uc.CheckStock(context.Background(), code)
	require.NoError(t, err)
	assert.True(t, detected, "expected anomaly detection")
	assert.True(t, zScore.IsAnomaly(2.5), "z=%.3f should be anomaly", zScore)

	fetcher.AssertExpectations(t)
	priceCache.AssertExpectations(t)
}

func TestMonitorUsecase_CheckStock_InsufficientHistory(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	priceCache := &MockPriceCache{}

	code, _ := stock.NewStockCode("6758")
	fetcher.On("FetchLatest", code).Return(stock.Quote{Price: 5000.0, Date: "2026-07-07"}, nil)
	priceCache.On("Push", code, stock.Price(5000.0)).Return(nil)
	// 30件未満（5件）を返す → 検知しない
	priceCache.On("GetHistory", code, 30).Return([]stock.Price{5000, 5010, 4990, 5005, 5000}, nil)

	svc := anomaly.NewDetectionService()
	uc := usecase.NewMonitorUsecase(fetcher, priceCache, svc, 2.5)

	detected, _, err := uc.CheckStock(context.Background(), code)
	require.NoError(t, err)
	assert.False(t, detected, "insufficient history should not trigger detection")
}
