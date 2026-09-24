package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestBackfillPriceHistoryUsecase_RunAll_BackfillsEveryCode(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	ctx := context.Background()

	code7203, _ := stock.NewStockCode("7203")
	code9984, _ := stock.NewStockCode("9984")

	quotes7203 := []stock.Quote{{Price: 3200.0, Date: "2026-07-06"}}
	quotes9984 := []stock.Quote{{Price: 8000.0, Date: "2026-07-06"}}

	prices.On("FindRecent", ctx, code7203, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code7203, 500).Return(quotes7203, nil)
	prices.On("SaveAll", ctx, code7203, quotes7203).Return(nil)

	prices.On("FindRecent", ctx, code9984, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code9984, 500).Return(quotes9984, nil)
	prices.On("SaveAll", ctx, code9984, quotes9984).Return(nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	uc.RunAll(ctx, []stock.StockCode{code7203, code9984}, time.Millisecond)

	prices.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestBackfillPriceHistoryUsecase_RunAll_ContinuesAfterFailure(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}
	ctx := context.Background()

	code7203, _ := stock.NewStockCode("7203")
	code9984, _ := stock.NewStockCode("9984")

	quotes9984 := []stock.Quote{{Price: 8000.0, Date: "2026-07-06"}}

	// 7203 は失敗させる
	prices.On("FindRecent", ctx, code7203, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code7203, 500).Return([]stock.Quote{}, errors.New("yahoo finance unavailable"))
	// 9984 は成功する（前の銘柄の失敗で止まらないこと）
	prices.On("FindRecent", ctx, code9984, 1).Return([]stock.Quote{}, nil)
	fetcher.On("FetchHistory", code9984, 500).Return(quotes9984, nil)
	prices.On("SaveAll", ctx, code9984, quotes9984).Return(nil)

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	uc.RunAll(ctx, []stock.StockCode{code7203, code9984}, time.Millisecond)

	prices.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestBackfillPriceHistoryUsecase_RunAll_StopsWhenContextCancelled(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}

	code7203, _ := stock.NewStockCode("7203")
	code9984, _ := stock.NewStockCode("9984")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	uc.RunAll(ctx, []stock.StockCode{code7203, code9984}, time.Hour)

	// キャンセル済みなら1件目にも着手しない
	prices.AssertNotCalled(t, "FindRecent", mock.Anything, mock.Anything, mock.Anything)
	fetcher.AssertNotCalled(t, "FetchHistory", mock.Anything, mock.Anything)
}

func TestBackfillPriceHistoryUsecase_RunAll_EmptyCodes(t *testing.T) {
	fetcher := &MockPriceFetcher{}
	prices := &MockPriceRepository{}

	uc := usecase.NewBackfillPriceHistoryUsecase(fetcher, prices)
	uc.RunAll(context.Background(), nil, time.Millisecond)

	fetcher.AssertNotCalled(t, "FetchHistory", mock.Anything, mock.Anything)
}
