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

// MockPriceCacheForNotifyTest は stock.PriceCache のモック実装（notify用）
type MockPriceCacheForNotifyTest struct{ mock.Mock }

func (m *MockPriceCacheForNotifyTest) Push(code stock.StockCode, price stock.Price) error {
	return m.Called(code, price).Error(0)
}

func (m *MockPriceCacheForNotifyTest) GetHistory(code stock.StockCode, n int) ([]stock.Price, error) {
	args := m.Called(code, n)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]stock.Price), args.Error(1)
}

func (m *MockPriceCacheForNotifyTest) LastDate(code stock.StockCode) (string, error) {
	args := m.Called(code)
	return args.String(0), args.Error(1)
}

func (m *MockPriceCacheForNotifyTest) SetLastDate(code stock.StockCode, date string) error {
	return m.Called(code, date).Error(0)
}

// TestMonitorUsecase_Notify_WithNilNotifyUsecase は、notifyUsecase が nil の場合、
// notify が何もせずに正常に完了することを検証する。
func TestMonitorUsecase_Notify_WithNilNotifyUsecase(t *testing.T) {
	cache := &MockPriceCacheForNotifyTest{}
	detector := anomaly.NewDetectionService()
	fetcher := &mockNotifyFetcher{}

	code, _ := stock.NewStockCode("7203")

	// notifyUsecase を nil で作成
	monitor := NewMonitorUsecase(fetcher, cache, detector, 2.5, nil)

	// z-score を適当な値で notify を呼ぶ
	z := anomaly.ZScore(3.0)

	// 何もセットアップせず呼ぶ - パニックせず正常に完了するはず
	monitor.notify(context.Background(), code, z)

	// ここで検証: cache には何も呼ばれないはず
	cache.AssertNotCalled(t, "GetHistory")
}

// TestMonitorUsecase_Notify_ExtractsCurrentPriceCorrectly は、
// キャッシュから取得した価格履歴から currentPrice が正しく抽出されることを検証する。
// 特に、末尾の要素が currentPrice であること。
func TestMonitorUsecase_Notify_ExtractsCurrentPriceCorrectly(t *testing.T) {
	cache := &MockPriceCacheForNotifyTest{}
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
	prices := []stock.Price{90, 110, 100, 120, 130}
	expectedCurrentPrice := 130.0 // 末尾要素
	floatPrices := []float64{90, 110, 100, 120, 130}

	// キャッシュから prices を返す
	cache.On("GetHistory", code, historySize).Return(prices, nil)

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
	monitor := NewMonitorUsecase(fetcher, cache, detector, 2.5, analyzeUsecase)

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

	cache.AssertCalled(t, "GetHistory", code, historySize)
}

// TestMonitorUsecase_Notify_HandlesEmptyHistoryGracefully は、
// キャッシュから価格履歴が取得できない場合、notify が
// gracefully に処理を終了することを検証する。
func TestMonitorUsecase_Notify_HandlesEmptyHistoryGracefully(t *testing.T) {
	cache := &MockPriceCacheForNotifyTest{}
	detector := anomaly.NewDetectionService()
	fetcher := &mockNotifyFetcher{}

	code, _ := stock.NewStockCode("7203")

	// AnalyzeAndNotifyUsecase のモック
	newsFetcher := &MockNewsFetcher{}
	analyzer := &MockAnalyzer{}
	reportGen := &MockReportGenerator{}
	notif := &MockNotifier{}
	notificationsRepo := &MockNotificationRepository{}

	// GetHistory が空のスライスを返す
	cache.On("GetHistory", code, historySize).Return([]stock.Price{}, nil)

	// Handle は呼ばれないので期待値を設定しない

	// AnalyzeAndNotifyUsecase を作成
	analyzeUsecase := NewAnalyzeAndNotifyUsecase(newsFetcher, analyzer, reportGen, notif, notificationsRepo)

	// MonitorUsecase に AnalyzeAndNotifyUsecase を渡す
	monitor := NewMonitorUsecase(fetcher, cache, detector, 2.5, analyzeUsecase)

	// notify を呼ぶ - パニックせずに完了するはず
	z := anomaly.ZScore(3.0)
	monitor.notify(context.Background(), code, z)

	// 検証: Analyzer.Analyze が呼ばれないこと
	analyzer.AssertNotCalled(t, "Analyze")

	// GetHistory は呼ばれたが、中身が空なので Handle には進まない
	cache.AssertCalled(t, "GetHistory", code, historySize)
}

// TestMonitorUsecase_Notify_HandleCallWithCorrectZScore は、
// z-score が正しく Handle に渡されることを検証する。
func TestMonitorUsecase_Notify_HandleCallWithCorrectZScore(t *testing.T) {
	cache := &MockPriceCacheForNotifyTest{}
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
	prices := []stock.Price{100, 105, 110}
	floatPrices := []float64{100, 105, 110}
	expectedCurrentPrice := 110.0
	expectedZScore := 2.5 // テスト用の z-score

	// キャッシュから prices を返す
	cache.On("GetHistory", code, historySize).Return(prices, nil)

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
	monitor := NewMonitorUsecase(fetcher, cache, detector, 2.5, analyzeUsecase)

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

// historySize は monitor_stocks.go で定義されている定数と同じ
// ここでは参照のために定数を参照しているが、実際には 30
