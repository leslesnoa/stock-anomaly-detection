package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubTokenVerifier is redefined here because auth_middleware_test.go now
// uses the internal `package handler` test package (Task 11 review fix),
// so its stubTokenVerifier is no longer visible from this external
// `package handler_test` file. Go's structural typing means this
// independently-defined type still satisfies handler.RequireAuth's
// unexported tokenVerifier interface as long as it implements
// VerifyToken(token string) (string, error).
type stubTokenVerifier struct {
	userID string
	err    error
}

func (s stubTokenVerifier) VerifyToken(token string) (string, error) {
	return s.userID, s.err
}

type stubWatchlistUsecase struct {
	addResult  watchlist.Watchlist
	addErr     error
	removeErr  error
	updateErr  error
	listResult []watchlist.Watchlist
	listErr    error
}

func (s stubWatchlistUsecase) Add(ctx context.Context, userID, stockCode string, threshold float64) (watchlist.Watchlist, error) {
	return s.addResult, s.addErr
}
func (s stubWatchlistUsecase) Remove(ctx context.Context, userID, watchlistID string) error {
	return s.removeErr
}
func (s stubWatchlistUsecase) UpdateThreshold(ctx context.Context, userID, watchlistID string, threshold float64) error {
	return s.updateErr
}
func (s stubWatchlistUsecase) List(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	return s.listResult, s.listErr
}

func withUserContext(req *http.Request, userID string) *http.Request {
	verifier := stubTokenVerifier{userID: userID}
	var captured *http.Request
	mw := handler.RequireAuth(verifier, func(w http.ResponseWriter, r *http.Request) { captured = r })
	req.Header.Set("Authorization", "Bearer any-token")
	mw(httptest.NewRecorder(), req)
	return captured
}

func TestWatchlistHandler_List(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{listResult: []watchlist.Watchlist{{ID: "wl-1", StockCode: code, AlertThreshold: 2.5}}})
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/watchlist", nil), "user-1")
	rec := httptest.NewRecorder()

	h.List(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp []map[string]interface{}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Len(t, resp, 1)
	assert.Equal(t, "7203", resp[0]["stock_code"])
}

func TestWatchlistHandler_Add_Success(t *testing.T) {
	code, _ := stock.NewStockCode("7203")
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{addResult: watchlist.Watchlist{ID: "wl-1", StockCode: code, AlertThreshold: 2.5}})
	body, _ := json.Marshal(map[string]interface{}{"stock_code": "7203", "alert_threshold": 2.5})
	req := withUserContext(httptest.NewRequest(http.MethodPost, "/watchlist", bytes.NewReader(body)), "user-1")
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestWatchlistHandler_Add_AlreadyExists(t *testing.T) {
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{addErr: watchlist.ErrAlreadyExists})
	body, _ := json.Marshal(map[string]interface{}{"stock_code": "7203", "alert_threshold": 2.5})
	req := withUserContext(httptest.NewRequest(http.MethodPost, "/watchlist", bytes.NewReader(body)), "user-1")
	rec := httptest.NewRecorder()

	h.Add(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestWatchlistHandler_Remove_NotFound(t *testing.T) {
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{removeErr: watchlist.ErrNotFound})
	req := withUserContext(httptest.NewRequest(http.MethodDelete, "/watchlist/wl-1", nil), "user-1")
	req.SetPathValue("id", "wl-1")
	rec := httptest.NewRecorder()

	h.Remove(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestWatchlistHandler_UpdateThreshold_Success(t *testing.T) {
	h := handler.NewWatchlistHandler(stubWatchlistUsecase{})
	body, _ := json.Marshal(map[string]interface{}{"alert_threshold": 4.0})
	req := withUserContext(httptest.NewRequest(http.MethodPatch, "/watchlist/wl-1", bytes.NewReader(body)), "user-1")
	req.SetPathValue("id", "wl-1")
	rec := httptest.NewRecorder()

	h.UpdateThreshold(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}
