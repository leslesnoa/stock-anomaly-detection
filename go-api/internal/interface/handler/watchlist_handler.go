package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
)

type watchlistUsecase interface {
	Add(ctx context.Context, userID, stockCode string, threshold float64) (watchlist.Watchlist, error)
	Remove(ctx context.Context, userID, watchlistID string) error
	UpdateThreshold(ctx context.Context, userID, watchlistID string, threshold float64) error
	List(ctx context.Context, userID string) ([]watchlist.Watchlist, error)
}

type WatchlistHandler struct {
	watchlists watchlistUsecase
}

func NewWatchlistHandler(watchlists watchlistUsecase) *WatchlistHandler {
	return &WatchlistHandler{watchlists: watchlists}
}

type watchlistItemResponse struct {
	ID             string  `json:"id"`
	StockCode      string  `json:"stock_code"`
	AlertThreshold float64 `json:"alert_threshold"`
}

func toWatchlistItemResponse(w watchlist.Watchlist) watchlistItemResponse {
	return watchlistItemResponse{ID: w.ID, StockCode: w.StockCode.String(), AlertThreshold: w.AlertThreshold}
}

func (h *WatchlistHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	items, err := h.watchlists.List(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	resp := make([]watchlistItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, toWatchlistItemResponse(item))
	}
	writeJSON(w, http.StatusOK, resp)
}

type addWatchlistRequest struct {
	StockCode      string  `json:"stock_code"`
	AlertThreshold float64 `json:"alert_threshold"`
}

func (h *WatchlistHandler) Add(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	var req addWatchlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := h.watchlists.Add(r.Context(), userID, req.StockCode, req.AlertThreshold)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, toWatchlistItemResponse(item))
	case errors.Is(err, watchlist.ErrAlreadyExists):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (h *WatchlistHandler) Remove(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	id := r.PathValue("id")
	err := h.watchlists.Remove(r.Context(), userID, id)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, watchlist.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

type updateThresholdRequest struct {
	AlertThreshold float64 `json:"alert_threshold"`
}

func (h *WatchlistHandler) UpdateThreshold(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	id := r.PathValue("id")
	var req updateThresholdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	err := h.watchlists.UpdateThreshold(r.Context(), userID, id, req.AlertThreshold)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, watchlist.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
