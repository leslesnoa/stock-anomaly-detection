package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

const tdnetLimit = 5

type YanoshinTDnetClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewYanoshinTDnetClient() *YanoshinTDnetClient {
	return NewYanoshinTDnetClientWithBaseURL("https://webapi.yanoshin.jp/webapi")
}

func NewYanoshinTDnetClientWithBaseURL(baseURL string) *YanoshinTDnetClient {
	return &YanoshinTDnetClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *YanoshinTDnetClient) FetchRecent(code stock.StockCode) ([]news.Item, error) {
	url := fmt.Sprintf("%s/tdnet/list/%s.json?limit=%d", c.baseURL, code, tdnetLimit)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch tdnet: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yanoshin tdnet api returned status %d", resp.StatusCode)
	}

	var result struct {
		Items []struct {
			Tdnet struct {
				Title string `json:"title"`
			} `json:"Tdnet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	items := make([]news.Item, 0, tdnetLimit)
	for i, r := range result.Items {
		if i >= tdnetLimit {
			break
		}
		items = append(items, news.Item{Headline: r.Tdnet.Title, Summary: r.Tdnet.Title})
	}
	return items, nil
}
