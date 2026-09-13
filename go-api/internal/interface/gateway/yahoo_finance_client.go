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
	url := fmt.Sprintf("%s/v8/finance/chart/%s.T?range=5d&interval=1d", c.baseURL, code)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return stock.Quote{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return stock.Quote{}, fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return stock.Quote{}, fmt.Errorf("yahoo finance api returned status %d", resp.StatusCode)
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
		return stock.Quote{}, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Chart.Result) == 0 || len(result.Chart.Result[0].Indicators.Quote) == 0 {
		return stock.Quote{}, fmt.Errorf("no quote data for %s", code)
	}

	timestamps := result.Chart.Result[0].Timestamp
	closes := result.Chart.Result[0].Indicators.Quote[0].Close
	for i := len(closes) - 1; i >= 0; i-- {
		if closes[i] != nil {
			date := time.Unix(timestamps[i], 0).In(yahooJST).Format("2006-01-02")
			return stock.Quote{Price: stock.Price(*closes[i]), Date: date}, nil
		}
	}
	return stock.Quote{}, fmt.Errorf("no quote data for %s", code)
}
