package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBackfillPriceHistoryUsecase_Run_SavesHistoryWhenStoreEmpty(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	code, _ := stock.NewStockCode("7203")
	ctx := context.Background()

	quotes := []stock.Quote{
		{Price: 3200.0, Date: "2026-07-06"},
		{Price: 3250.0, Date: "2026-07-07"},
		{Price: 3300.0, Date: "2026-07-08"},
	}
	prices.On("FindRecent", ctx, code, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code, 500).Return(quotes, nil)
	prices.On("SaveAll", ctx, code, quotes).Return(nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	err := uc.Run(ctx, code)

	require.NoError(t, err)
	prices.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestBackfillPriceHistoryUsecase_Run_SkipsWhenHistoryAlreadyExists(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	code, _ := stock.NewStockCode("7203")
	ctx := context.Background()

	prices.On("FindRecent", ctx, code, 1).Return([]stock.Quote{{Price: 3300.0, Date: "2026-07-08"}}, nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	err := uc.Run(ctx, code)

	require.NoError(t, err)
	fetcher.AssertNotCalled(t, "FetchHistory", mock.Anything, mock.Anything)
	prices.AssertNotCalled(t, "SaveAll", mock.Anything, mock.Anything, mock.Anything)
}

func TestBackfillPriceHistoryUsecase_Run_PropagatesFetchError(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	code, _ := stock.NewStockCode("7203")
	ctx := context.Background()

	prices.On("FindRecent", ctx, code, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code, 500).Return([]stock.Quote{}, errors.New("fetch failed"))

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	err := uc.Run(ctx, code)

	require.Error(t, err)
	prices.AssertNotCalled(t, "SaveAll", mock.Anything, mock.Anything, mock.Anything)
}

func TestBackfillPriceHistoryUsecase_Run_PropagatesSaveError(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	code, _ := stock.NewStockCode("7203")
	ctx := context.Background()

	quotes := []stock.Quote{{Price: 3200.0, Date: "2026-07-06"}}
	prices.On("FindRecent", ctx, code, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code, 500).Return(quotes, nil)
	prices.On("SaveAll", ctx, code, quotes).Return(errors.New("db down"))

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	err := uc.Run(ctx, code)

	require.Error(t, err)
}
