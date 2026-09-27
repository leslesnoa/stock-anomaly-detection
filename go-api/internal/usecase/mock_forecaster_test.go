package usecase_test

import (
	"github.com/stock-anomaly-detection/go-api/internal/domain/forecast"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stretchr/testify/mock"
)

type MockForecaster struct{ mock.Mock }

func (m *MockForecaster) Forecast(code stock.StockCode, currentPrice float64, prices []float64) (forecast.Forecast, error) {
	args := m.Called(code, currentPrice, prices)
	return args.Get(0).(forecast.Forecast), args.Error(1)
}
