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

func TestJQuantsClient_FetchLatest(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/token/auth_user", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"refreshToken": "test-refresh"})
	})
	mux.HandleFunc("POST /v1/token/auth_refresh", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"idToken": "test-id-token"})
	})
	mux.HandleFunc("GET /v1/prices/daily_quotes", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-id-token", r.Header.Get("Authorization"))
		assert.Equal(t, "72030", r.URL.Query().Get("code"))
		json.NewEncoder(w).Encode(map[string]interface{}{
			"daily_quotes": []map[string]interface{}{
				{"Code": "72030", "Close": 3250.0},
			},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewJQuantsClientWithBaseURL("test@example.com", "pass", srv.URL)
	code, _ := stock.NewStockCode("7203")

	price, err := client.FetchLatest(code)
	require.NoError(t, err)
	assert.Equal(t, stock.Price(3250.0), price)
}

func TestJQuantsClient_FetchLatest_NoData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/token/auth_user", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"refreshToken": "r"})
	})
	mux.HandleFunc("POST /v1/token/auth_refresh", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"idToken": "i"})
	})
	mux.HandleFunc("GET /v1/prices/daily_quotes", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"daily_quotes": []interface{}{}})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewJQuantsClientWithBaseURL("e", "p", srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchLatest(code)
	require.Error(t, err)
}
