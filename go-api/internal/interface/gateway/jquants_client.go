package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type JQuantsClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewJQuantsClient(apiKey string) *JQuantsClient {
	return NewJQuantsClientWithBaseURL(apiKey, "https://api.jquants.com")
}

func NewJQuantsClientWithBaseURL(apiKey, baseURL string) *JQuantsClient {
	return &JQuantsClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *JQuantsClient) FetchLatest(code stock.StockCode) (stock.Quote, error) {
	// J-Quantsは4桁コードに末尾0を付けた5桁で検索する
	url := fmt.Sprintf("%s/v2/equities/bars/daily?code=%s0", c.baseURL, code)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return stock.Quote{}, err
	}
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return stock.Quote{}, fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return stock.Quote{}, fmt.Errorf("jquants api returned status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			C    float64 `json:"C"`
			Date string  `json:"Date"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return stock.Quote{}, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Data) == 0 {
		return stock.Quote{}, fmt.Errorf("no quote data for %s", code)
	}

	// data[] の並び順に依存せず、Date が最新のバーを選ぶ（Finding 2）
	latest := result.Data[0]
	for _, d := range result.Data[1:] {
		if d.Date > latest.Date {
			latest = d
		}
	}
	return stock.Quote{Price: stock.Price(latest.C), Date: latest.Date}, nil
}
