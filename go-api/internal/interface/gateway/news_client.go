package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

const newsLookbackDays = 7
const newsLimit = 5

type FinnhubClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewFinnhubClient(apiKey string) *FinnhubClient {
	return NewFinnhubClientWithBaseURL(apiKey, "https://finnhub.io/api/v1")
}

func NewFinnhubClientWithBaseURL(apiKey, baseURL string) *FinnhubClient {
	return &FinnhubClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *FinnhubClient) FetchRecent(code stock.StockCode) ([]news.Item, error) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -newsLookbackDays).Format("2006-01-02")
	to := now.Format("2006-01-02")
	symbol := fmt.Sprintf("%s.T", code)

	url := fmt.Sprintf("%s/company-news?symbol=%s&from=%s&to=%s", c.baseURL, symbol, from, to)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Finnhub-Token", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch news: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("finnhub api returned status %d", resp.StatusCode)
	}

	var raw []struct {
		Headline string `json:"headline"`
		Summary  string `json:"summary"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	items := make([]news.Item, 0, newsLimit)
	for i, r := range raw {
		if i >= newsLimit {
			break
		}
		items = append(items, news.Item{Headline: r.Headline, Summary: r.Summary})
	}
	return items, nil
}
