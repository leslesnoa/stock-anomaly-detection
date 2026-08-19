package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
			return sanitizeURLError(err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = sanitizeURLError(err)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		lastErr = fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("slack notification failed after %d attempts: %w", slackMaxRetries, lastErr)
}

// sanitizeURLError strips the request URL from a *url.Error before it is
// stored or logged. The webhook URL is a bearer credential, and
// (*url.Error).Error() embeds the full URL by default (e.g. `Post
// "https://hooks.slack.com/services/...": dial tcp: ...`), which would leak
// the secret into application logs on any transient transport failure. The
// underlying cause is preserved so the error remains useful for debugging.
func sanitizeURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s <redacted-url>: %w", urlErr.Op, urlErr.Err)
	}
	return err
}
