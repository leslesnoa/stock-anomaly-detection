package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/directionmodel"
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

func TestPythonEngineClient_Forecast(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /forecast", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "7203", body["stock_code"])
		assert.Equal(t, 3250.0, body["current_price"])

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"stock_code": "7203",
			"horizon":    20,
			"points": []map[string]interface{}{
				{"step": 1, "center": 3260.0, "upper_68": 3300.0, "lower_68": 3220.0, "upper_95": 3350.0, "lower_95": 3180.0},
			},
		}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	prices := make([]float64, 121)
	for i := range prices {
		prices[i] = 3000.0 + float64(i)
	}

	got, err := client.Forecast(context.Background(), code, 3250.0, prices)
	require.NoError(t, err)
	assert.Equal(t, 20, got.Horizon)
	require.Len(t, got.Points, 1)
	assert.Equal(t, 1, got.Points[0].Step)
	assert.InDelta(t, 3260.0, got.Points[0].Center, 0.001)
	assert.InDelta(t, 3220.0, got.Points[0].Lower68, 0.001)
}

func TestPythonEngineClient_Forecast_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /forecast", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	_, err := client.Forecast(context.Background(), code, 3250.0, make([]float64, 121))
	require.Error(t, err)
}

func TestPythonEngineClient_Train(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /model/train", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		stocks := body["stocks"].([]interface{})
		require.Len(t, stocks, 1)
		first := stocks[0].(map[string]interface{})
		assert.Equal(t, "7203", first["stock_code"])
		prices := first["prices"].([]interface{})
		require.Len(t, prices, 2)
		firstPrice := prices[0].(map[string]interface{})
		assert.Equal(t, "2026-07-06", firstPrice["date"])
		assert.Equal(t, 3200.0, firstPrice["close"])

		hitRate := 0.55
		pValue := 0.01
		samples := 120
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"adopted":                  true,
			"hit_rate":                 hitRate,
			"baseline_hit_rate":        0.50,
			"p_value":                  pValue,
			"independent_sample_count": samples,
			"trained_at":               "2026-09-29T00:00:00Z",
		}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	series := []directionmodel.PriceSeries{
		{
			StockCode: code,
			Quotes: []stock.Quote{
				{Price: 3200.0, Date: "2026-07-06"},
				{Price: 3250.0, Date: "2026-07-07"},
			},
		},
	}

	got, err := client.Train(context.Background(), series)
	require.NoError(t, err)
	assert.True(t, got.Adopted)
	require.NotNil(t, got.HitRate)
	assert.InDelta(t, 0.55, *got.HitRate, 0.001)
	require.NotNil(t, got.IndependentSampleCount)
	assert.Equal(t, 120, *got.IndependentSampleCount)
}

func TestPythonEngineClient_Train_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /model/train", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	_, err := client.Train(context.Background(), []directionmodel.PriceSeries{
		{StockCode: code, Quotes: []stock.Quote{{Price: 3200.0, Date: "2026-07-06"}}},
	})
	require.Error(t, err)
}

func TestPythonEngineClient_Forecast_IncludesDirectionModel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /forecast", func(w http.ResponseWriter, r *http.Request) {
		predictedDirection := "up"
		hitRate := 0.55
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"stock_code": "7203",
			"horizon":    20,
			"points":     []interface{}{},
			"direction_model": map[string]interface{}{
				"adopted":                  true,
				"predicted_direction":      predictedDirection,
				"hit_rate":                 hitRate,
				"baseline_hit_rate":        0.50,
				"p_value":                  0.01,
				"independent_sample_count": 120,
				"trained_at":               "2026-09-29T00:00:00Z",
			},
		}))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := gateway.NewPythonEngineClient(srv.URL)
	code, _ := stock.NewStockCode("7203")

	got, err := client.Forecast(context.Background(), code, 3300.0, make([]float64, 130))
	require.NoError(t, err)
	assert.True(t, got.DirectionModel.Adopted)
	require.NotNil(t, got.DirectionModel.PredictedDirection)
	assert.Equal(t, "up", *got.DirectionModel.PredictedDirection)
}
