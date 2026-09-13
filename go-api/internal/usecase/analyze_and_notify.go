package usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/stock-anomaly-detection/go-api/internal/domain/ai"
	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/notifier"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type AnalyzeAndNotifyUsecase struct {
	newsFetcher   news.Fetcher
	analyzer      analysis.Analyzer
	reportGen     ai.ReportGenerator
	notifier      notifier.Notifier
	notifications notification.Repository
}

func NewAnalyzeAndNotifyUsecase(
	newsFetcher news.Fetcher,
	analyzer analysis.Analyzer,
	reportGen ai.ReportGenerator,
	notif notifier.Notifier,
	notifications notification.Repository,
) *AnalyzeAndNotifyUsecase {
	return &AnalyzeAndNotifyUsecase{
		newsFetcher:   newsFetcher,
		analyzer:      analyzer,
		reportGen:     reportGen,
		notifier:      notif,
		notifications: notifications,
	}
}

// Handle は異常検知された銘柄のニュース取得・テクニカル指標計算・AIレポート生成・
// Slack通知・通知履歴保存を行う。Pythonエンジン呼び出しが失敗した場合のみエラーを返し、
// それ以外の失敗（ニュース取得・Claude API・Slack）はログに記録して処理を継続する。
func (u *AnalyzeAndNotifyUsecase) Handle(ctx context.Context, code stock.StockCode, zScore, currentPrice float64, prices []float64) error {
	items, err := u.newsFetcher.FetchRecent(code)
	if err != nil {
		log.Printf("WARN news fetch failed for %s: %v", code, err)
		items = nil
	}

	indicators, err := u.analyzer.Analyze(code, zScore, currentPrice, prices)
	if err != nil {
		log.Printf("ERROR python engine analyze failed for %s: %v (Slack通知をスキップ)", code, err)
		return fmt.Errorf("analyze indicators: %w", err)
	}

	var report string
	if aiReport, err := u.reportGen.GenerateReport(buildPrompt(code, zScore, currentPrice, indicators, items)); err != nil {
		log.Printf("WARN claude report generation failed for %s: %v (指標のみで通知)", code, err)
		report = buildIndicatorsOnlyMessage(code, zScore, currentPrice, indicators)
	} else {
		report = fmt.Sprintf("⚠️ 異常検知: %s (z=%.2fσ)\n%s", code, zScore, aiReport)
	}

	slackSent := true
	if err := u.notifier.Send(report); err != nil {
		log.Printf("ERROR slack notification failed for %s: %v", code, err)
		slackSent = false
	}

	n := notification.Notification{
		StockCode:           code.String(),
		AnomalyScore:        zScore,
		AIReport:            report,
		TechnicalIndicators: indicators,
		SlackSent:           slackSent,
	}
	if err := u.notifications.Save(ctx, n); err != nil {
		return fmt.Errorf("save notification: %w", err)
	}
	return nil
}

func buildPrompt(code stock.StockCode, zScore, currentPrice float64, indicators analysis.Indicators, items []news.Item) string {
	newsSummary := "関連ニュースなし"
	if len(items) > 0 {
		var sb strings.Builder
		for _, item := range items {
			if item.Summary == "" || item.Summary == item.Headline {
				fmt.Fprintf(&sb, "- %s\n", item.Headline)
			} else {
				fmt.Fprintf(&sb, "- %s: %s\n", item.Headline, item.Summary)
			}
		}
		newsSummary = sb.String()
	}

	return fmt.Sprintf(
		"銘柄コード：%s\n異常スコア：%.2fσ（過去30日比）\n現在株価：%.1f円\nRSI：%s\nMACD：%s\n直近ニュース要約：\n%s\n上記を踏まえ異常の原因とリスクを200字以内で分析してください。",
		code, zScore, currentPrice, formatFloatPtr(indicators.RSI), formatMACD(indicators.MACD), newsSummary,
	)
}

func buildIndicatorsOnlyMessage(code stock.StockCode, zScore, currentPrice float64, indicators analysis.Indicators) string {
	return fmt.Sprintf(
		"⚠️ 異常検知: %s (z=%.2fσ)\n現在株価：%.1f円\nRSI：%s\nMACD：%s\n（AI分析レポートの生成に失敗したため、テクニカル指標のみ通知しています）",
		code, zScore, currentPrice, formatFloatPtr(indicators.RSI), formatMACD(indicators.MACD),
	)
}

func formatFloatPtr(v *float64) string {
	if v == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2f", *v)
}

func formatMACD(m analysis.MACD) string {
	return fmt.Sprintf("line=%s signal=%s histogram=%s", formatFloatPtr(m.Line), formatFloatPtr(m.Signal), formatFloatPtr(m.Histogram))
}
