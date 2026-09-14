package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

var yahooJST = time.FixedZone("JST", 9*60*60)

type YahooFinanceClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewYahooFinanceClient() *YahooFinanceClient {
	return NewYahooFinanceClientWithBaseURL("https://query1.finance.yahoo.com")
}

func NewYahooFinanceClientWithBaseURL(baseURL string) *YahooFinanceClient {
	return &YahooFinanceClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *YahooFinanceClient) FetchLatest(code stock.StockCode) (stock.Quote, error) {
	timestamps, closes, err := c.fetchChart(code, "5d")
	if err != nil {
		return stock.Quote{}, err
	}
	for i := len(closes) - 1; i >= 0; i-- {
		if closes[i] != nil {
			date := time.Unix(timestamps[i], 0).In(yahooJST).Format("2006-01-02")
			return stock.Quote{Price: stock.Price(*closes[i]), Date: date}, nil
		}
	}
	return stock.Quote{}, fmt.Errorf("no quote data for %s", code)
}

// FetchHistory は直近3ヶ月分の日次終値を取得し、null（未確定値）を除いた上で
// 直近days件（トレーディングデイ換算）に絞って古い順で返す。
// 上場から日が浅い銘柄など実件数がdaysに満たない場合はそのまま全件を返す。
func (c *YahooFinanceClient) FetchHistory(code stock.StockCode, days int) ([]stock.Quote, error) {
	timestamps, closes, err := c.fetchChart(code, "3mo")
	if err != nil {
		return nil, err
	}
	quotes := make([]stock.Quote, 0, len(closes))
	for i, close := range closes {
		if close == nil {
			continue
		}
		date := time.Unix(timestamps[i], 0).In(yahooJST).Format("2006-01-02")
		quotes = append(quotes, stock.Quote{Price: stock.Price(*close), Date: date})
	}
	if len(quotes) > days {
		quotes = quotes[len(quotes)-days:]
	}
	return quotes, nil
}

// fetchChart はYahoo Finance chart APIを叩き、タイムスタンプと終値の配列を返す。
// タイムスタンプ数より終値数が多い場合は終値側を切り詰める
// （データ不整合によるindex out of range panicを防ぐため）。
func (c *YahooFinanceClient) fetchChart(code stock.StockCode, rangeParam string) ([]int64, []*float64, error) {
	url := fmt.Sprintf("%s/v8/finance/chart/%s.T?range=%s&interval=1d", c.baseURL, code, rangeParam)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("yahoo finance api returned status %d", resp.StatusCode)
	}

	var result struct {
		Chart struct {
			Result []struct {
				Timestamp  []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Close []*float64 `json:"close"`
					} `json:"quote"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Chart.Result) == 0 || len(result.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, nil, fmt.Errorf("no quote data for %s", code)
	}

	timestamps := result.Chart.Result[0].Timestamp
	closes := result.Chart.Result[0].Indicators.Quote[0].Close
	if len(timestamps) < len(closes) {
		closes = closes[:len(timestamps)]
	}
	return timestamps, closes, nil
}
