package usecase_test

import (
	"errors"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBackfillPriceHistoryUsecase_Run_PushesHistoryWhenCacheEmpty(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	cache := &MockPriceCache{}
	code, _ := stock.NewStockCode("7203")

	quotes := []stock.Quote{
		{Price: 3200.0, Date: "2026-07-06"},
		{Price: 3250.0, Date: "2026-07-07"},
		{Price: 3300.0, Date: "2026-07-08"},
	}
	cache.On("GetHistory", code, 1).Return([]stock.Price{}, nil)
	fetcher.On("FetchHistory", code, 30).Return(quotes, nil)
	cache.On("Push", code, stock.Price(3200.0)).Return(nil)
	cache.On("Push", code, stock.Price(3250.0)).Return(nil)
	cache.On("Push", code, stock.Price(3300.0)).Return(nil)
	cache.On("SetLastDate", code, "2026-07-08").Return(nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
	err := uc.Run(code)

	require.NoError(t, err)
	cache.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestBackfillPriceHistoryUsecase_Run_SkipsWhenHistoryAlreadyExists(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	cache := &MockPriceCache{}
	code, _ := stock.NewStockCode("7203")

	cache.On("GetHistory", code, 1).Return([]stock.Price{3300.0}, nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
	err := uc.Run(code)

	require.NoError(t, err)
	fetcher.AssertNotCalled(t, "FetchHistory", mock.Anything, mock.Anything)
	cache.AssertNotCalled(t, "Push", mock.Anything, mock.Anything)
}

func TestBackfillPriceHistoryUsecase_Run_PropagatesFetchError(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	cache := &MockPriceCache{}
	code, _ := stock.NewStockCode("7203")

	cache.On("GetHistory", code, 1).Return([]stock.Price{}, nil)
	fetcher.On("FetchHistory", code, 30).Return([]stock.Quote{}, errors.New("fetch failed"))

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
	err := uc.Run(code)

	require.Error(t, err)
	cache.AssertNotCalled(t, "Push", mock.Anything, mock.Anything)
}
