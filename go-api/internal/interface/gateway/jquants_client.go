package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type JQuantsClient struct {
	baseURL    string
	email      string
	password   string
	idToken    string
	mu         sync.Mutex
	httpClient *http.Client
}

func NewJQuantsClient(email, password string) *JQuantsClient {
	return NewJQuantsClientWithBaseURL(email, password, "https://api.jquants.com")
}

func NewJQuantsClientWithBaseURL(email, password, baseURL string) *JQuantsClient {
	return &JQuantsClient{
		baseURL:    baseURL,
		email:      email,
		password:   password,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *JQuantsClient) FetchLatest(code stock.StockCode) (stock.Price, error) {
	if err := c.ensureToken(); err != nil {
		return 0, fmt.Errorf("j-quants auth: %w", err)
	}

	// J-Quantsは4桁コードに末尾0を付けた5桁で検索する
	url := fmt.Sprintf("%s/v1/prices/daily_quotes?code=%s0", c.baseURL, code)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.idToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("jquants api returned status %d", resp.StatusCode)
	}

	var result struct {
		DailyQuotes []struct {
			Close float64 `json:"Close"`
		} `json:"daily_quotes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}
	if len(result.DailyQuotes) == 0 {
		return 0, fmt.Errorf("no quote data for %s", code)
	}
	return stock.Price(result.DailyQuotes[0].Close), nil
}

func (c *JQuantsClient) ensureToken() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idToken != "" {
		return nil
	}

	body, _ := json.Marshal(map[string]string{
		"mailaddress": c.email,
		"password":    c.password,
	})
	req1, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/token/auth_user", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req1.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req1)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jquants api returned status %d", resp.StatusCode)
	}
	var r1 struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r1); err != nil {
		return err
	}

	body, _ = json.Marshal(map[string]string{"refreshToken": r1.RefreshToken})
	req2, err := http.NewRequest(http.MethodPost, c.baseURL+"/v1/token/auth_refresh", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := c.httpClient.Do(req2)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("jquants api returned status %d", resp2.StatusCode)
	}
	var r2 struct {
		IDToken string `json:"idToken"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&r2); err != nil {
		return err
	}

	c.idToken = r2.IDToken
	return nil
}
