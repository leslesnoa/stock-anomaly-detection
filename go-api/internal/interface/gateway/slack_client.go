package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const slackMaxRetries = 3

type SlackClient struct {
	webhookURL string
	httpClient *http.Client
}

func NewSlackClient(webhookURL string) *SlackClient {
	return &SlackClient{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *SlackClient) Send(message string) error {
	body, err := json.Marshal(map[string]string{"text": message})
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt < slackMaxRetries; attempt++ {
		req, err := http.NewRequest(http.MethodPost, c.webhookURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		lastErr = fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("slack notification failed after %d attempts: %w", slackMaxRetries, lastErr)
}
