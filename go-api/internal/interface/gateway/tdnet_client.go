package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

const (
	tdnetLimit           = 5
	tdnetLookbackDays    = 7
	tdnetPubdateLayout   = "2006-01-02 15:04:05"
	tdnetDisclosureLimit = 100
)

// yanoshinのpubdateはタイムゾーン無しのJST。コンテナにtzdataが無くても動くよう FixedZone を使う。
var jst = time.FixedZone("JST", 9*60*60)

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
				Title   string `json:"title"`
				Pubdate string `json:"pubdate"`
			} `json:"Tdnet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -tdnetLookbackDays)

	items := make([]news.Item, 0, tdnetLimit)
	for _, r := range result.Items {
		if len(items) >= tdnetLimit {
			break
		}
		if pubdate, err := time.Parse(tdnetPubdateLayout, r.Tdnet.Pubdate); err == nil && pubdate.Before(cutoff) {
			continue
		}
		items = append(items, news.Item{Headline: r.Tdnet.Title, Summary: r.Tdnet.Title})
	}
	return items, nil
}

func (c *YanoshinTDnetClient) FetchDisclosures(ctx context.Context, code stock.StockCode, since time.Time) ([]news.Disclosure, error) {
	endpoint := fmt.Sprintf("%s/tdnet/list/%s.json?limit=%d", c.baseURL, code, tdnetDisclosureLimit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch tdnet disclosures: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yanoshin tdnet api returned status %d", resp.StatusCode)
	}

	var result struct {
		Items []struct {
			Tdnet struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				Pubdate     string `json:"pubdate"`
				DocumentURL string `json:"document_url"`
			} `json:"Tdnet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode tdnet disclosures: %w", err)
	}

	disclosures := []news.Disclosure{}
	for _, item := range result.Items {
		t := item.Tdnet
		if t.ID == "" || t.Title == "" {
			log.Printf("WARN skip tdnet item without id or title for %s", code)
			continue
		}
		publishedAt, err := time.ParseInLocation(tdnetPubdateLayout, t.Pubdate, jst)
		if err != nil {
			log.Printf("WARN skip tdnet item %s for %s: invalid pubdate %q", t.ID, code, t.Pubdate)
			continue
		}
		if publishedAt.Before(since) {
			continue
		}
		disclosures = append(disclosures, news.Disclosure{
			TdnetID:     t.ID,
			Title:       t.Title,
			URL:         sanitizeDocumentURL(t.DocumentURL, t.ID, code),
			PublishedAt: publishedAt,
		})
	}
	return disclosures, nil
}

// sanitizeDocumentURL は第三者サービス(yanoshin)由来の document_url をそのまま信用しない。
// url.Parse が成功しスキームが https の場合のみ残し、それ以外（javascript: スキームや http など）は
// 空文字にしてWARNを残す。記事自体は破棄しない（フロントエンドはURLが空ならタイトルのみ表示する）。
func sanitizeDocumentURL(raw, tdnetID string, code stock.StockCode) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" {
		log.Printf("WARN discard non-https document_url for tdnet item %s (%s): %q", tdnetID, code, raw)
		return ""
	}
	return raw
}
