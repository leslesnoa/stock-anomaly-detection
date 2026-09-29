package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/directionmodel"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestTrainDirectionModelUsecase_Run_Success(t *testing.T) {
	code7203, _ := stock.NewStockCode("7203")
	code6758, _ := stock.NewStockCode("6758")

	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindAllStockCodes", mock.Anything).Return([]stock.StockCode{code7203, code6758}, nil)

	quotes7203 := []stock.Quote{{Price: 3200, Date: "2026-07-06"}}
	quotes6758 := []stock.Quote{{Price: 1500, Date: "2026-07-06"}}
	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code7203, mock.Anything).Return(quotes7203, nil)
	prices.On("FindRecent", mock.Anything, code6758, mock.Anything).Return(quotes6758, nil)

	trainer := new(MockDirectionModelTrainer)
	trainer.On("Train", mock.Anything, mock.MatchedBy(func(series []directionmodel.PriceSeries) bool {
		return len(series) == 2
	})).Return(directionmodel.TrainingResult{Adopted: false}, nil)

	uc := usecase.NewTrainDirectionModelUsecase(watchlists, prices, trainer)
	err := uc.Run(context.Background())

	require.NoError(t, err)
	trainer.AssertExpectations(t)
}

func TestTrainDirectionModelUsecase_Run_SkipsWhenNoStocksHaveHistory(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindAllStockCodes", mock.Anything).Return([]stock.StockCode{code}, nil)

	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).Return([]stock.Quote{}, nil)

	trainer := new(MockDirectionModelTrainer)

	uc := usecase.NewTrainDirectionModelUsecase(watchlists, prices, trainer)
	err := uc.Run(context.Background())

	require.NoError(t, err)
	trainer.AssertNotCalled(t, "Train", mock.Anything, mock.Anything)
}

func TestTrainDirectionModelUsecase_Run_PropagatesTrainerError(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindAllStockCodes", mock.Anything).Return([]stock.StockCode{code}, nil)

	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).
		Return([]stock.Quote{{Price: 3200, Date: "2026-07-06"}}, nil)

	trainer := new(MockDirectionModelTrainer)
	trainer.On("Train", mock.Anything, mock.Anything).
		Return(directionmodel.TrainingResult{}, errors.New("engine unavailable"))

	uc := usecase.NewTrainDirectionModelUsecase(watchlists, prices, trainer)
	err := uc.Run(context.Background())

	require.Error(t, err)
}

func TestTrainDirectionModelUsecase_RunWithRetry_SucceedsOnFirstAttempt(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindAllStockCodes", mock.Anything).Return([]stock.StockCode{code}, nil)

	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).
		Return([]stock.Quote{{Price: 3200, Date: "2026-07-06"}}, nil)

	trainer := new(MockDirectionModelTrainer)
	trainer.On("Train", mock.Anything, mock.Anything).
		Return(directionmodel.TrainingResult{Adopted: false}, nil).Once()

	uc := usecase.NewTrainDirectionModelUsecase(watchlists, prices, trainer)

	done := make(chan struct{})
	go func() {
		uc.RunWithRetry(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithRetry should return immediately on first success without waiting")
	}
	trainer.AssertNumberOfCalls(t, "Train", 1)
}

func TestTrainDirectionModelUsecase_RunPeriodically_StopsOnContextCancel(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindAllStockCodes", mock.Anything).Return([]stock.StockCode{code}, nil)

	prices := new(MockPriceRepository)
	prices.On("FindRecent", mock.Anything, code, mock.Anything).
		Return([]stock.Quote{{Price: 3200, Date: "2026-07-06"}}, nil)

	trainer := new(MockDirectionModelTrainer)
	trainer.On("Train", mock.Anything, mock.Anything).
		Return(directionmodel.TrainingResult{Adopted: false}, nil)

	uc := usecase.NewTrainDirectionModelUsecase(watchlists, prices, trainer)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		uc.RunPeriodically(ctx, time.Hour)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunPeriodically should return promptly after context cancellation")
	}
}
