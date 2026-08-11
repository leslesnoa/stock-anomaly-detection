package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlackClient_Send_Success(t *testing.T) {
	var received map[string]string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewSlackClient(srv.URL + "/webhook")
	err := client.Send("異常検知: 7203")
	require.NoError(t, err)
	assert.Equal(t, "異常検知: 7203", received["text"])
}

func TestSlackClient_Send_RetriesThreeTimesThenFails(t *testing.T) {
	callCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewSlackClient(srv.URL + "/webhook")
	err := client.Send("異常検知: 7203")
	require.Error(t, err)
	assert.Equal(t, 3, callCount)
}

func TestSlackClient_Send_TransportErrorDoesNotLeakWebhookURL(t *testing.T) {
	// Start a server and close it immediately so requests fail at the
	// transport level (connection refused), not with an HTTP status code.
	srv := httptest.NewServer(http.NewServeMux())
	webhookURL := srv.URL + "/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"
	srv.Close()

	client := gateway.NewSlackClient(webhookURL)
	err := client.Send("異常検知: 7203")
	require.Error(t, err)

	errMsg := err.Error()
	assert.NotContains(t, errMsg, webhookURL, "error must not leak the full webhook URL")
	assert.NotContains(t, errMsg, "/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX", "error must not leak the webhook secret path")
	assert.NotEmpty(t, errMsg)
}
