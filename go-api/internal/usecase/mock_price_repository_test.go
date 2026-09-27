package usecase_test

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stretchr/testify/mock"
)

// MockPriceRepository は stock.PriceRepository のモック実装。
type MockPriceRepository struct{ mock.Mock }

func (m *MockPriceRepository) Save(ctx context.Context, code stock.StockCode, quote stock.Quote) error {
	return m.Called(ctx, code, quote).Error(0)
}

func (m *MockPriceRepository) SaveAll(ctx context.Context, code stock.StockCode, quotes []stock.Quote) error {
	return m.Called(ctx, code, quotes).Error(0)
}

func (m *MockPriceRepository) FindRecent(ctx context.Context, code stock.StockCode, n int) ([]stock.Quote, error) {
	args := m.Called(ctx, code, n)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]stock.Quote), args.Error(1)
}

func (m *MockPriceRepository) LatestDate(ctx context.Context, code stock.StockCode) (string, error) {
	args := m.Called(ctx, code)
	return args.String(0), args.Error(1)
}
