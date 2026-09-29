package usecase_test

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/directionmodel"
	"github.com/stretchr/testify/mock"
)

type MockDirectionModelTrainer struct{ mock.Mock }

func (m *MockDirectionModelTrainer) Train(ctx context.Context, series []directionmodel.PriceSeries) (directionmodel.TrainingResult, error) {
	args := m.Called(ctx, series)
	return args.Get(0).(directionmodel.TrainingResult), args.Error(1)
}
