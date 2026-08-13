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

func TestPythonEngineClient_Analyze(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /analyze", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "7203", body["stock_code"])
		assert.Equal(t, 3.2, body["z_score"])
		assert.Equal(t, 3250.0, body["current_price"])

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"stock_code": "7203",
			"indicators": map[string]interface{}{
				"rsi": 65.3,
				"macd": map[string]interface{}{
					"line": 1.2, "signal": 0.9, "histogram": 0.3,
				},
				"bollinger": map[string]interface{}{
					"upper": 3300.0, "middle": 3200.0, "lower": 3100.0,
				},
			},
		}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	prices := make([]float64, 30)
	for i := range prices {
		prices[i] = 3000.0 + float64(i)
	}

	got, err := client.Analyze(code, 3.2, 3250.0, prices)
	require.NoError(t, err)
	require.NotNil(t, got.RSI)
	assert.InDelta(t, 65.3, *got.RSI, 0.001)
	require.NotNil(t, got.MACD.Line)
	assert.InDelta(t, 1.2, *got.MACD.Line, 0.001)
	require.NotNil(t, got.Bollinger.Upper)
	assert.InDelta(t, 3300.0, *got.Bollinger.Upper, 0.001)
}

func TestPythonEngineClient_Analyze_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /analyze", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	_, err := client.Analyze(code, 3.2, 3250.0, make([]float64, 30))
	require.Error(t, err)
}
