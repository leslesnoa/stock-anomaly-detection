package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockWatchlistRepository struct{ mock.Mock }

func (m *mockWatchlistRepository) FindByUserID(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]watchlist.Watchlist), args.Error(1)
}
func (m *mockWatchlistRepository) FindAllStockCodes(ctx context.Context) ([]stock.StockCode, error) {
	args := m.Called(ctx)
	return args.Get(0).([]stock.StockCode), args.Error(1)
}
func (m *mockWatchlistRepository) Create(ctx context.Context, w watchlist.Watchlist) (watchlist.Watchlist, error) {
	args := m.Called(ctx, w)
	return args.Get(0).(watchlist.Watchlist), args.Error(1)
}
func (m *mockWatchlistRepository) Delete(ctx context.Context, id, userID string) error {
	return m.Called(ctx, id, userID).Error(0)
}
func (m *mockWatchlistRepository) UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error {
	return m.Called(ctx, id, userID, threshold).Error(0)
}

type MockNameFetcher struct{ mock.Mock }

func (m *MockNameFetcher) FetchCompanyName(code stock.StockCode) (string, error) {
	args := m.Called(code)
	return args.String(0), args.Error(1)
}

func TestManageWatchlistUsecase_Add_Success(t *testing.T) {
	repo := new(mockWatchlistRepository)
	code, _ := stock.NewStockCode("7203")
	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 3.0}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nil)
	result, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	require.Equal(t, "wl-1", result.ID)
}

func TestManageWatchlistUsecase_Add_DefaultThreshold(t *testing.T) {
	repo := new(mockWatchlistRepository)
	code, _ := stock.NewStockCode("7203")
	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 2.5}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 2.5}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nil)
	_, err := uc.Add(context.Background(), "user-1", "7203", 0)

	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestManageWatchlistUsecase_Add_InvalidStockCode(t *testing.T) {
	repo := new(mockWatchlistRepository)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nil)
	_, err := uc.Add(context.Background(), "user-1", "invalid", 2.5)

	require.Error(t, err)
	repo.AssertNotCalled(t, "Create")
}

func TestManageWatchlistUsecase_Remove(t *testing.T) {
	repo := new(mockWatchlistRepository)
	repo.On("Delete", mock.Anything, "wl-1", "user-1").Return(nil)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nil)
	err := uc.Remove(context.Background(), "user-1", "wl-1")

	require.NoError(t, err)
}

func TestManageWatchlistUsecase_UpdateThreshold(t *testing.T) {
	repo := new(mockWatchlistRepository)
	repo.On("UpdateThreshold", mock.Anything, "wl-1", "user-1", 4.0).Return(nil)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nil)
	err := uc.UpdateThreshold(context.Background(), "user-1", "wl-1", 4.0)

	require.NoError(t, err)
}

func TestManageWatchlistUsecase_List(t *testing.T) {
	repo := new(mockWatchlistRepository)
	repo.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{ID: "wl-1"}}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nil)
	result, err := uc.List(context.Background(), "user-1")

	require.NoError(t, err)
	require.Len(t, result, 1)
}

func TestManageWatchlistUsecase_Add_TriggersBackfill(t *testing.T) {
	repo := new(mockWatchlistRepository)
	fetcher := &MockPriceFetcher{}
	cache := &MockPriceCache{}
	code, _ := stock.NewStockCode("7203")

	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 3.0}, nil)
	cache.On("GetHistory", code, 1).Return([]stock.Price{}, nil)
	fetcher.On("FetchHistory", code, 30).Return([]stock.Quote{{Price: 3200.0, Date: "2026-07-06"}}, nil)
	cache.On("Push", code, stock.Price(3200.0)).Return(nil)
	cache.On("SetLastDate", code, "2026-07-06").Return(nil)

	backfiller := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
	uc := usecase.NewManageWatchlistUsecase(repo, backfiller, nil)
	_, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	cache.AssertExpectations(t)
	fetcher.AssertExpectations(t)
}

func TestManageWatchlistUsecase_Add_SucceedsEvenIfBackfillFails(t *testing.T) {
	repo := new(mockWatchlistRepository)
	fetcher := &MockPriceFetcher{}
	cache := &MockPriceCache{}
	code, _ := stock.NewStockCode("7203")

	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 3.0}, nil)
	cache.On("GetHistory", code, 1).Return([]stock.Price{}, nil)
	fetcher.On("FetchHistory", code, 30).Return([]stock.Quote{}, errors.New("yahoo finance unavailable"))

	backfiller := usecase.NewBackfillPriceHistoryUsecase(fetcher, cache)
	uc := usecase.NewManageWatchlistUsecase(repo, backfiller, nil)
	result, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	require.Equal(t, "wl-1", result.ID)
}

func TestManageWatchlistUsecase_Add_FetchesCompanyName(t *testing.T) {
	repo := new(mockWatchlistRepository)
	nameFetcher := new(MockNameFetcher)
	code, _ := stock.NewStockCode("7203")

	nameFetcher.On("FetchCompanyName", code).Return("Toyota Motor Corporation", nil)
	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, StockName: "Toyota Motor Corporation", AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, StockName: "Toyota Motor Corporation", AlertThreshold: 3.0}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nameFetcher)
	result, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	require.Equal(t, "Toyota Motor Corporation", result.StockName)
	repo.AssertExpectations(t)
	nameFetcher.AssertExpectations(t)
}

func TestManageWatchlistUsecase_Add_SucceedsEvenIfNameFetchFails(t *testing.T) {
	repo := new(mockWatchlistRepository)
	nameFetcher := new(MockNameFetcher)
	code, _ := stock.NewStockCode("7203")

	nameFetcher.On("FetchCompanyName", code).Return("", errors.New("yahoo finance unavailable"))
	repo.On("Create", mock.Anything, watchlist.Watchlist{UserID: "user-1", StockCode: code, StockName: "", AlertThreshold: 3.0}).
		Return(watchlist.Watchlist{ID: "wl-1", UserID: "user-1", StockCode: code, AlertThreshold: 3.0}, nil)

	uc := usecase.NewManageWatchlistUsecase(repo, nil, nameFetcher)
	result, err := uc.Add(context.Background(), "user-1", "7203", 3.0)

	require.NoError(t, err)
	require.Equal(t, "wl-1", result.ID)
}
