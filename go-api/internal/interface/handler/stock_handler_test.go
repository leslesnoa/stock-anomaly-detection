package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/notification"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubStockChartUsecase struct {
	result usecase.StockChart
	err    error
}

func (s stubStockChartUsecase) Handle(ctx context.Context, userID, stockCode string) (usecase.StockChart, error) {
	return s.result, s.err
}

func TestStockHandler_Chart_Success(t *testing.T) {
	price := 1200.0
	z := 1.5
	h := handler.NewStockHandler(stubStockChartUsecase{result: usecase.StockChart{
		StockCode:     "7203",
		CurrentPrice:  &price,
		CurrentZScore: &z,
		Prices:        []stock.Quote{{Date: "2026-01-01", Price: 1200}},
		AlertBand:     []usecase.AlertBandPoint{},
		Notifications: []notification.Notification{},
	}})
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/stocks/7203/chart", nil), "user-1")
	req.SetPathValue("code", "7203")
	rec := httptest.NewRecorder()

	h.Chart(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, "7203", resp["stock_code"])
	assert.InDelta(t, 1200.0, resp["current_price"], 0.001)
	assert.InDelta(t, 1.5, resp["current_z_score"], 0.001)
	assert.Nil(t, resp["forecast"])
}

func TestStockHandler_Chart_InvalidStockCode(t *testing.T) {
	h := handler.NewStockHandler(stubStockChartUsecase{err: stock.ErrInvalidStockCode})
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/stocks/12/chart", nil), "user-1")
	req.SetPathValue("code", "12")
	rec := httptest.NewRecorder()

	h.Chart(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestStockHandler_Chart_NotInWatchlist(t *testing.T) {
	h := handler.NewStockHandler(stubStockChartUsecase{err: watchlist.ErrNotFound})
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/stocks/9999/chart", nil), "user-1")
	req.SetPathValue("code", "9999")
	rec := httptest.NewRecorder()

	h.Chart(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestStockHandler_Chart_Unauthorized(t *testing.T) {
	h := handler.NewStockHandler(stubStockChartUsecase{})
	req := httptest.NewRequest(http.MethodGet, "/stocks/7203/chart", nil)
	req.SetPathValue("code", "7203")
	rec := httptest.NewRecorder()

	h.Chart(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
