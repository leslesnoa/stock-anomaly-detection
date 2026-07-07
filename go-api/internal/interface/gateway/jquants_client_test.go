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

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func TestJQuantsClient_FetchLatest(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v2/equities/bars/daily", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "72030", r.URL.Query().Get("code"))
		// 順不同・複数日: 最新の 2026-07-07 (3250) が選ばれるべき
		writeJSON(w, map[string]interface{}{
			"data": []map[string]interface{}{
				{"C": 3200.0, "Date": "2026-07-06"},
				{"C": 3250.0, "Date": "2026-07-07"},
				{"C": 3100.0, "Date": "2026-07-03"},
			},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewJQuantsClientWithBaseURL("test-api-key", srv.URL)
	code, _ := stock.NewStockCode("7203")

	quote, err := client.FetchLatest(code)
	require.NoError(t, err)
	assert.Equal(t, stock.Price(3250.0), quote.Price)
	assert.Equal(t, "2026-07-07", quote.Date)
}

func TestJQuantsClient_FetchLatest_NoData(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/equities/bars/daily", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"data": []interface{}{}})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewJQuantsClientWithBaseURL("key", srv.URL)
	code, _ := stock.NewStockCode("0000")
	_, err := client.FetchLatest(code)
	require.Error(t, err)
}
