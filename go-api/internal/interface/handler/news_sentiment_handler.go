package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
)

type newsSentimentUsecase interface {
	Handle(ctx context.Context, userID, stockCode string) (usecase.NewsSentiment, error)
}

type NewsSentimentHandler struct {
	sentiments newsSentimentUsecase
}

func NewNewsSentimentHandler(sentiments newsSentimentUsecase) *NewsSentimentHandler {
	return &NewsSentimentHandler{sentiments: sentiments}
}

type sentimentScoresResponse struct {
	Bullish                int `json:"bullish"`
	Bearish                int `json:"bearish"`
	Impact                 int `json:"impact"`
	Confidence             int `json:"confidence"`
	ShortTermUpProbability int `json:"short_term_up_probability"`
}

type newsArticleResponse struct {
	Title               string    `json:"title"`
	URL                 string    `json:"url"`
	PublishedAt         time.Time `json:"published_at"`
	Sentiment           *string   `json:"sentiment"`
	SentimentConfidence *int      `json:"sentiment_confidence"`
}

type newsSentimentResponse struct {
	Status   string                   `json:"status"`
	Stale    bool                     `json:"stale"`
	Scores   *sentimentScoresResponse `json:"scores"`
	ScoredBy *string                  `json:"scored_by"`
	ScoredAt *time.Time               `json:"scored_at"`
	Articles []newsArticleResponse    `json:"articles"`
}

func toNewsSentimentResponse(ns usecase.NewsSentiment) newsSentimentResponse {
	resp := newsSentimentResponse{
		Status:   string(ns.Status),
		Stale:    ns.Stale,
		Articles: make([]newsArticleResponse, len(ns.Articles)),
	}
	if s := ns.Snapshot; s != nil {
		scoredBy, scoredAt := s.ScoredBy, s.CreatedAt
		resp.ScoredBy = &scoredBy
		resp.ScoredAt = &scoredAt
		if s.Scores != nil {
			resp.Scores = &sentimentScoresResponse{
				Bullish: s.Scores.Bullish, Bearish: s.Scores.Bearish, Impact: s.Scores.Impact,
				Confidence: s.Scores.Confidence, ShortTermUpProbability: s.Scores.ShortTermUp,
			}
		}
	}
	for i, a := range ns.Articles {
		article := newsArticleResponse{
			Title: a.Title, URL: a.URL, PublishedAt: a.PublishedAt, SentimentConfidence: a.Confidence,
		}
		if a.Sentiment != nil {
			label := string(*a.Sentiment)
			article.Sentiment = &label
		}
		resp.Articles[i] = article
	}
	return resp
}

func (h *NewsSentimentHandler) NewsSentiment(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	result, err := h.sentiments.Handle(r.Context(), userID, r.PathValue("code"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, toNewsSentimentResponse(result))
	case errors.Is(err, stock.ErrInvalidStockCode):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, watchlist.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		log.Printf("ERROR news sentiment %s: %v", r.PathValue("code"), err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
