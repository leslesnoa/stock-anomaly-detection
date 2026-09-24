package usecase

import (
	"context"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockNewsFetcher は news.Fetcher のモック実装
type MockNewsFetcher struct{ mock.Mock }

func (m *MockNewsFetcher) FetchRecent(code stock.StockCode) ([]news.Item, error) {
	args := m.Called(code)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]news.Item), args.Error(1)
}

// MockAnalyzer は analysis.Analyzer のモック実装
type MockAnalyzer struct{ mock.Mock }

func (m *MockAnalyzer) Analyze(code stock.StockCode, zScore, currentPrice float64, prices []float64) (analysis.Indicators, error) {
	args := m.Called(code, zScore, currentPrice, prices)
	return args.Get(0).(analysis.Indicators), args.Error(1)
}

// MockReportGenerator は ai.ReportGenerator のモック実装
type MockReportGenerator struct{ mock.Mock }

func (m *MockReportGenerator) GenerateReport(prompt string) (string, error) {
	args := m.Called(prompt)
	return args.String(0), args.Error(1)
}

// MockNotifier は notifier.Notifier のモック実装
type MockNotifier struct{ mock.Mock }

func (m *MockNotifier) Send(message string) error {
	return m.Called(message).Error(0)
}

// MockNotificationRepository は notification.Repository のモック実装
type MockNotificationRepository struct{ mock.Mock }

func (m *MockNotificationRepository) Save(ctx context.Context, n notification.Notification) error {
	return m.Called(ctx, n).Error(0)
}

// MockPriceRepositoryForNotifyTest は stock.PriceRepository のモック実装（notify用）。
// monitor_notify_test.go は内部テストパッケージ（package usecase）のため、
// usecase_test 側の MockPriceRepository を使い回せず別途定義している。
type MockPriceRepositoryForNotifyTest struct{ mock.Mock }

func (m *MockPriceRepositoryForNotifyTest) Save(ctx context.Context, code stock.StockCode, quote stock.Quote) error {
	return m.Called(ctx, code, quote).Error(0)
}

func (m *MockPriceRepositoryForNotifyTest) SaveAll(ctx context.Context, code stock.StockCode, quotes []stock.Quote) error {
	return m.Called(ctx, code, quotes).Error(0)
}

func (m *MockPriceRepositoryForNotifyTest) FindRecent(ctx context.Context, code stock.StockCode, n int) ([]stock.Quote, error) {
	args := m.Called(ctx, code, n)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]stock.Quote), args.Error(1)
}

func (m *MockPriceRepositoryForNotifyTest) LatestDate(ctx context.Context, code stock.StockCode) (string, error) {
	args := m.Called(ctx, code)
	return args.String(0), args.Error(1)
}

// TestMonitorUsecase_Notify_WithNilNotifyUsecase は、notifyUsecase が nil の場合、
// notify が何もせずに正常に完了することを検証する。
func TestMonitorUsecase_Notify_WithNilNotifyUsecase(t *testing.T) {
	prices := &MockPriceRepositoryForNotifyTest{}
	detector := anomaly.NewDetectionService()
	fetcher := &mockNotifyFetcher{}

	code, _ := stock.NewStockCode("7203")

	// notifyUsecase を nil で作成
	monitor := NewMonitorUsecase(fetcher, prices, detector, 2.5, nil)

	// z-score を適当な値で notify を呼ぶ
	z := anomaly.ZScore(3.0)

	// 何もセットアップせず呼ぶ - パニックせず正常に完了するはず
	monitor.notify(context.Background(), code, z)

	// ここで検証: リポジトリには何も呼ばれないはず
	prices.AssertNotCalled(t, "FindRecent", mock.Anything, mock.Anything, mock.Anything)
}

// TestMonitorUsecase_Notify_ExtractsCurrentPriceCorrectly は、
// 価格リポジトリから取得した価格履歴から currentPrice が正しく抽出されることを検証する。
// 特に、末尾の要素が currentPrice であること。
func TestMonitorUsecase_Notify_ExtractsCurrentPriceCorrectly(t *testing.T) {
	prices := &MockPriceRepositoryForNotifyTest{}
	detector := anomaly.NewDetectionService()
	fetcher := &mockNotifyFetcher{}

	code, _ := stock.NewStockCode("7203")

	// AnalyzeAndNotifyUsecase のモック
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	notificationsRepo := &MockNotificationRepository{}

	// 価格データ: 最後の要素が 130.0
	quotes := []stock.Quote{
		{Price: 90, Date: "2026-07-01"},
		{Price: 110, Date: "2026-07-02"},
		{Price: 100, Date: "2026-07-03"},
		{Price: 120, Date: "2026-07-06"},
		{Price: 130, Date: "2026-07-07"},
	}
	expectedCurrentPrice := 130.0 // 末尾要素
	floatPrices := []float64{90, 110, 100, 120, 130}

	prices.On("FindRecent", mock.Anything, code, historySize).Return(quotes, nil)

	// AnalyzeAndNotifyUsecase.Handle が currentPrice=130.0 で呼ばれることを期待
	newsFetcher.On("FetchRecent", code).Return(nil, nil)
	analyzer.On("Analyze", code, 3.0, expectedCurrentPrice, floatPrices).Return(
		analysis.Indicators{RSI: nil, MACD: analysis.MACD{}, Bollinger: analysis.Bollinger{}}, nil)
	reportGen.On("GenerateReport", mock.AnythingOfType("string")).Return("test report", nil)
	notif.On("Send", mock.AnythingOfType("string")).Return(nil)
	notificationsRepo.On("Save", mock.Anything, mock.AnythingOfType("notification.Notification")).Return(nil)

	// AnalyzeAndNotifyUsecase を作成
	analyzeUsecase := NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, notificationsRepo)

	// MonitorUsecase に AnalyzeAndNotifyUsecase を渡す
	monitor := NewMonitorUsecase(fetcher, prices, detector, 2.5, analyzeUsecase)

	// notify を呼ぶ
	z := anomaly.ZScore(3.0)
	monitor.notify(context.Background(), code, z)

	// 検証: Analyzer.Analyze が正しい引数で呼ばれたことを確認
	// 特に currentPrice と prices スライスが正しいこと
	analyzer.AssertCalled(t, "Analyze", code, 3.0, expectedCurrentPrice, floatPrices)

	// より詳細な検証: prices スライスの末尾が currentPrice と同じであること
	calls := analyzer.Calls
	require.Len(t, calls, 1)
	callArgs := calls[0].Arguments

	// callArgs[3] が prices スライス
	passedPrices := callArgs[3].([]float64)
	require.Len(t, passedPrices, 5)
	assert.Equal(t, expectedCurrentPrice, passedPrices[len(passedPrices)-1], "末尾の価格が currentPrice と同じであること")

	prices.AssertCalled(t, "FindRecent", mock.Anything, code, historySize)
}

// TestMonitorUsecase_Notify_HandlesEmptyHistoryGracefully は、
// 価格リポジトリから価格履歴が取得できない場合、notify が
// gracefully に処理を終了することを検証する。
func TestMonitorUsecase_Notify_HandlesEmptyHistoryGracefully(t *testing.T) {
	prices := &MockPriceRepositoryForNotifyTest{}
	detector := anomaly.NewDetectionService()
	fetcher := &mockNotifyFetcher{}

	code, _ := stock.NewStockCode("7203")

	// AnalyzeAndNotifyUsecase のモック
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	notificationsRepo := &MockNotificationRepository{}

	// FindRecent が空のスライスを返す
	prices.On("FindRecent", mock.Anything, code, historySize).Return([]stock.Quote{}, nil)

	// Handle は呼ばれないので期待値を設定しない

	// AnalyzeAndNotifyUsecase を作成
	analyzeUsecase := NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, notificationsRepo)

	// MonitorUsecase に AnalyzeAndNotifyUsecase を渡す
	monitor := NewMonitorUsecase(fetcher, prices, detector, 2.5, analyzeUsecase)

	// notify を呼ぶ - パニックせずに完了するはず
	z := anomaly.ZScore(3.0)
	monitor.notify(context.Background(), code, z)

	// 検証: Analyzer.Analyze が呼ばれないこと
	analyzer.AssertNotCalled(t, "Analyze")

	// FindRecent は呼ばれたが、中身が空なので Handle には進まない
	prices.AssertCalled(t, "FindRecent", mock.Anything, code, historySize)
}

// TestMonitorUsecase_Notify_HandleCallWithCorrectZScore は、
// z-score が正しく Handle に渡されることを検証する。
func TestMonitorUsecase_Notify_HandleCallWithCorrectZScore(t *testing.T) {
	prices := &MockPriceRepositoryForNotifyTest{}
	detector := anomaly.NewDetectionService()
	fetcher := &mockNotifyFetcher{}

	code, _ := stock.NewStockCode("7203")

	// AnalyzeAndNotifyUsecase のモック
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	notificationsRepo := &MockNotificationRepository{}

	// 価格データ
	quotes := []stock.Quote{
		{Price: 100, Date: "2026-07-06"},
		{Price: 105, Date: "2026-07-07"},
		{Price: 110, Date: "2026-07-08"},
	}
	floatPrices := []float64{100, 105, 110}
	expectedCurrentPrice := 110.0
	expectedZScore := 2.5 // テスト用の z-score

	prices.On("FindRecent", mock.Anything, code, historySize).Return(quotes, nil)

	// Analyzer.Analyze が expectedZScore で呼ばれることを期待
	newsFetcher.On("FetchRecent", code).Return(nil, nil)
	analyzer.On("Analyze", code, expectedZScore, expectedCurrentPrice, floatPrices).Return(
		analysis.Indicators{RSI: nil, MACD: analysis.MACD{}, Bollinger: analysis.Bollinger{}}, nil)
	reportGen.On("GenerateReport", mock.AnythingOfType("string")).Return("test report", nil)
	notif.On("Send", mock.AnythingOfType("string")).Return(nil)
	notificationsRepo.On("Save", mock.Anything, mock.AnythingOfType("notification.Notification")).Return(nil)

	// AnalyzeAndNotifyUsecase を作成
	analyzeUsecase := NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, notificationsRepo)

	// MonitorUsecase に AnalyzeAndNotifyUsecase を渡す
	monitor := NewMonitorUsecase(fetcher, prices, detector, 2.5, analyzeUsecase)

	// notify を呼ぶ
	z := anomaly.ZScore(expectedZScore)
	monitor.notify(context.Background(), code, z)

	// 検証: z-score が正しく Handle に渡されたこと
	analyzer.AssertCalled(t, "Analyze", code, expectedZScore, mock.Anything, mock.Anything)
}

// mockNotifyFetcher は PriceFetcher のシンプルなモック
type mockNotifyFetcher struct{}

func (m *mockNotifyFetcher) FetchLatest(code stock.StockCode) (stock.Quote, error) {
	return stock.Quote{}, nil
}

func (m *mockNotifyFetcher) FetchHistory(code stock.StockCode, days int) ([]stock.Quote, error) {
	return nil, nil
}

// historySize は monitor_stocks.go で定義されている定数と同じ
// ここでは参照のために定数を参照しているが、実際には 30
