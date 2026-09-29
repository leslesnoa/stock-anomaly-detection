package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
)

type stockChartUsecase interface {
	Handle(ctx context.Context, userID, stockCode string) (usecase.StockChart, error)
}

type StockHandler struct {
	charts stockChartUsecase
}

func NewStockHandler(charts stockChartUsecase) *StockHandler {
	return &StockHandler{charts: charts}
}

type priceResponse struct {
	Date  string  `json:"date"`
	Close float64 `json:"close"`
}

type alertBandResponse struct {
	Date  string  `json:"date"`
	Upper float64 `json:"upper"`
	Lower float64 `json:"lower"`
}

type forecastPointResponse struct {
	Step    int     `json:"step"`
	Center  float64 `json:"center"`
	Upper68 float64 `json:"upper_68"`
	Lower68 float64 `json:"lower_68"`
	Upper95 float64 `json:"upper_95"`
	Lower95 float64 `json:"lower_95"`
}

type directionModelResponse struct {
	Adopted                bool     `json:"adopted"`
	PredictedDirection     *string  `json:"predicted_direction"`
	HitRate                *float64 `json:"hit_rate"`
	BaselineHitRate        *float64 `json:"baseline_hit_rate"`
	PValue                 *float64 `json:"p_value"`
	IndependentSampleCount *int     `json:"independent_sample_count"`
	TrainedAt              *string  `json:"trained_at"`
}

type forecastResponse struct {
	Horizon        int                     `json:"horizon"`
	Points         []forecastPointResponse `json:"points"`
	DirectionModel directionModelResponse  `json:"direction_model"`
}

type notificationResponse struct {
	NotifiedAt   time.Time `json:"notified_at"`
	AnomalyScore float64   `json:"anomaly_score"`
	AIReport     string    `json:"ai_report"`
	SlackSent    bool      `json:"slack_sent"`
}

type stockChartResponse struct {
	StockCode     string                 `json:"stock_code"`
	CurrentPrice  *float64               `json:"current_price"`
	Prices        []priceResponse        `json:"prices"`
	AlertBand     []alertBandResponse    `json:"alert_band"`
	CurrentZScore *float64               `json:"current_z_score"`
	Forecast      *forecastResponse      `json:"forecast"`
	Notifications []notificationResponse `json:"notifications"`
}

func toStockChartResponse(c usecase.StockChart) stockChartResponse {
	prices := make([]priceResponse, len(c.Prices))
	for i, q := range c.Prices {
		prices[i] = priceResponse{Date: q.Date, Close: float64(q.Price)}
	}
	band := make([]alertBandResponse, len(c.AlertBand))
	for i, b := range c.AlertBand {
		band[i] = alertBandResponse{Date: b.Date, Upper: b.Upper, Lower: b.Lower}
	}
	notifications := make([]notificationResponse, len(c.Notifications))
	for i, n := range c.Notifications {
		notifications[i] = notificationResponse{
			NotifiedAt:   n.NotifiedAt,
			AnomalyScore: n.AnomalyScore,
			AIReport:     n.AIReport,
			SlackSent:    n.SlackSent,
		}
	}
	var fc *forecastResponse
	if c.Forecast != nil {
		points := make([]forecastPointResponse, len(c.Forecast.Points))
		for i, p := range c.Forecast.Points {
			points[i] = forecastPointResponse{
				Step: p.Step, Center: p.Center,
				Upper68: p.Upper68, Lower68: p.Lower68,
				Upper95: p.Upper95, Lower95: p.Lower95,
			}
		}
		fc = &forecastResponse{
			Horizon: c.Forecast.Horizon,
			Points:  points,
			DirectionModel: directionModelResponse{
				Adopted:                c.Forecast.DirectionModel.Adopted,
				PredictedDirection:     c.Forecast.DirectionModel.PredictedDirection,
				HitRate:                c.Forecast.DirectionModel.HitRate,
				BaselineHitRate:        c.Forecast.DirectionModel.BaselineHitRate,
				PValue:                 c.Forecast.DirectionModel.PValue,
				IndependentSampleCount: c.Forecast.DirectionModel.IndependentSampleCount,
				TrainedAt:              c.Forecast.DirectionModel.TrainedAt,
			},
		}
	}
	return stockChartResponse{
		StockCode:     c.StockCode,
		CurrentPrice:  c.CurrentPrice,
		Prices:        prices,
		AlertBand:     band,
		CurrentZScore: c.CurrentZScore,
		Forecast:      fc,
		Notifications: notifications,
	}
}

func (h *StockHandler) Chart(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	code := r.PathValue("code")
	chart, err := h.charts.Handle(r.Context(), userID, code)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, toStockChartResponse(chart))
	case errors.Is(err, stock.ErrInvalidStockCode):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, watchlist.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
