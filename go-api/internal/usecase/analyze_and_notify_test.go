package usecase_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockNewsFetcher struct{ mock.Mock }

func (m *MockNewsFetcher) FetchRecent(code stock.StockCode) ([]news.Item, error) {
	args := m.Called(code)
	return args.Get(0).([]news.Item), args.Error(1)
}

type MockAnalyzer struct{ mock.Mock }

func (m *MockAnalyzer) Analyze(code stock.StockCode, zScore, currentPrice float64, prices []float64) (analysis.Indicators, error) {
	args := m.Called(code, zScore, currentPrice, prices)
	return args.Get(0).(analysis.Indicators), args.Error(1)
}

type MockReportGenerator struct{ mock.Mock }

func (m *MockReportGenerator) GenerateReport(prompt string) (string, error) {
	args := m.Called(prompt)
	return args.String(0), args.Error(1)
}

type MockNotifier struct{ mock.Mock }

func (m *MockNotifier) Send(message string) error {
	return m.Called(message).Error(0)
}

type MockNotificationRepository struct{ mock.Mock }

func (m *MockNotificationRepository) Save(ctx context.Context, n notification.Notification) error {
	return m.Called(ctx, n).Error(0)
}

func rsi(v float64) *float64 { return &v }

func TestAnalyzeAndNotifyUsecase_Handle_HappyPath(t *testing.T) {
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	repo := &MockNotificationRepository{}

	code, _ := stock.NewStockCode("7203")
	indicators := analysis.Indicators{RSI: rsi(65.3)}

	newsFetcher.On("FetchRecent", code).Return([]news.Item{{Headline: "h", Summary: "s"}}, nil)
	analyzer.On("Analyze", code, 3.2, 3250.0, mock.Anything).Return(indicators, nil)
	reportGen.On("GenerateReport", mock.Anything).Return("AI分析結果", nil)
	notif.On("Send", mock.MatchedBy(func(msg string) bool { return msg != "" })).Return(nil)
	repo.On("Save", mock.Anything, mock.MatchedBy(func(n notification.Notification) bool {
		return n.SlackSent && n.StockCode == "7203"
	})).Return(nil)

	uc := usecase.NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, repo)
	err := uc.Handle(context.Background(), code, 3.2, 3250.0, []float64{3000, 3010, 3250})
	require.NoError(t, err)

	newsFetcher.AssertExpectations(t)
	analyzer.AssertExpectations(t)
	reportGen.AssertExpectations(t)
	notif.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestAnalyzeAndNotifyUsecase_Handle_AnalyzerFailure_SkipsNotification(t *testing.T) {
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	repo := &MockNotificationRepository{}

	code, _ := stock.NewStockCode("7203")
	newsFetcher.On("FetchRecent", code).Return([]news.Item{}, nil)
	analyzer.On("Analyze", code, 3.2, 3250.0, mock.Anything).Return(analysis.Indicators{}, assert.AnError)

	uc := usecase.NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, repo)
	err := uc.Handle(context.Background(), code, 3.2, 3250.0, []float64{3000, 3010, 3250})
	require.Error(t, err)

	reportGen.AssertNotCalled(t, "GenerateReport", mock.Anything)
	notif.AssertNotCalled(t, "Send", mock.Anything)
	repo.AssertNotCalled(t, "Save", mock.Anything, mock.Anything)
}

func TestAnalyzeAndNotifyUsecase_Handle_ClaudeFailure_FallsBackToIndicatorsOnly(t *testing.T) {
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	repo := &MockNotificationRepository{}

	code, _ := stock.NewStockCode("7203")
	indicators := analysis.Indicators{RSI: rsi(65.3)}

	newsFetcher.On("FetchRecent", code).Return([]news.Item{}, nil)
	analyzer.On("Analyze", code, 3.2, 3250.0, mock.Anything).Return(indicators, nil)
	reportGen.On("GenerateReport", mock.Anything).Return("", assert.AnError)
	notif.On("Send", mock.MatchedBy(func(msg string) bool {
		// フォールバックメッセージには指標由来の内容（RSI値）が含まれ、
		// Claude失敗時の空文字列やAI分析結果テキストが紛れ込んでいないことを検証する。
		return strings.Contains(msg, "RSI") &&
			strings.Contains(msg, "65.30") &&
			strings.Contains(msg, "テクニカル指標のみ通知") &&
			!strings.Contains(msg, "AI分析結果")
	})).Return(nil)
	repo.On("Save", mock.Anything, mock.Anything).Return(nil)

	uc := usecase.NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, repo)
	err := uc.Handle(context.Background(), code, 3.2, 3250.0, []float64{3000, 3010, 3250})
	require.NoError(t, err)

	notif.AssertExpectations(t)
	repo.AssertExpectations(t)
}

func TestAnalyzeAndNotifyUsecase_Handle_SlackFailure_SavesWithSlackSentFalse(t *testing.T) {
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	repo := &MockNotificationRepository{}

	code, _ := stock.NewStockCode("7203")
	indicators := analysis.Indicators{RSI: rsi(65.3)}

	newsFetcher.On("FetchRecent", code).Return([]news.Item{}, nil)
	analyzer.On("Analyze", code, 3.2, 3250.0, mock.Anything).Return(indicators, nil)
	reportGen.On("GenerateReport", mock.Anything).Return("AI分析結果", nil)
	notif.On("Send", mock.Anything).Return(assert.AnError)
	repo.On("Save", mock.Anything, mock.MatchedBy(func(n notification.Notification) bool {
		return !n.SlackSent
	})).Return(nil)

	uc := usecase.NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, repo)
	err := uc.Handle(context.Background(), code, 3.2, 3250.0, []float64{3000, 3010, 3250})
	require.NoError(t, err, "Slack失敗自体はHandleのエラーにしない")

	repo.AssertExpectations(t)
}
