package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type JQuantsClient struct {
	baseURL  string
	email    string
	password string
	idToken  string
}

func NewJQuantsClient(email, password string) *JQuantsClient {
	return NewJQuantsClientWithBaseURL(email, password, "https://api.jquants.com")
}

func NewJQuantsClientWithBaseURL(email, password, baseURL string) *JQuantsClient {
	return &JQuantsClient{baseURL: baseURL, email: email, password: password}
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetch quotes: %w", err)
	}
	defer resp.Body.Close()

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
	if c.idToken != "" {
		return nil
	}

	body, _ := json.Marshal(map[string]string{
		"mailaddress": c.email,
		"password":    c.password,
	})
	resp, err := http.Post(c.baseURL+"/v1/token/auth_user", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var r1 struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r1); err != nil {
		return err
	}

	body, _ = json.Marshal(map[string]string{"refreshtoken": r1.RefreshToken})
	resp2, err := http.Post(c.baseURL+"/v1/token/auth_refresh", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	var r2 struct {
		IDToken string `json:"idToken"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&r2); err != nil {
		return err
	}

	c.idToken = r2.IDToken
	return nil
}
