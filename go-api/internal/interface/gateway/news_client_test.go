package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinnhubClient_FetchRecent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /company-news", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("X-Finnhub-Token"))
		assert.Equal(t, "7203.T", r.URL.Query().Get("symbol"))
		assert.NotEmpty(t, r.URL.Query().Get("from"))
		assert.NotEmpty(t, r.URL.Query().Get("to"))

		items := []map[string]string{
			{"headline": "トヨタ、新型EV発表", "summary": "トヨタ自動車が新型EVを発表した"},
			{"headline": "業績好調", "summary": "四半期決算は市場予想を上回った"},
		}
		require.NoError(t, json.NewEncoder(w).Encode(items))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewFinnhubClientWithBaseURL("test-key", srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchRecent(code)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "トヨタ、新型EV発表", got[0].Headline)
	assert.Equal(t, "トヨタ自動車が新型EVを発表した", got[0].Summary)
}

func TestFinnhubClient_FetchRecent_LimitsToTop5(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /company-news", func(w http.ResponseWriter, r *http.Request) {
		items := make([]map[string]string, 10)
		for i := range items {
			items[i] = map[string]string{"headline": "h", "summary": "s"}
		}
		require.NoError(t, json.NewEncoder(w).Encode(items))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewFinnhubClientWithBaseURL("test-key", srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.FetchRecent(code)
	require.NoError(t, err)
	assert.Len(t, got, 5)
}
