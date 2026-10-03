package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/sentiment"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubNewsSentimentUsecase struct {
	result usecase.NewsSentiment
	err    error
}

func (s stubNewsSentimentUsecase) Handle(ctx context.Context, userID, stockCode string) (usecase.NewsSentiment, error) {
	return s.result, s.err
}

func serveNewsSentiment(t *testing.T, stub stubNewsSentimentUsecase) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	h := handler.NewNewsSentimentHandler(stub)
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/stocks/7203/news-sentiment", nil), "user-1")
	req.SetPathValue("code", "7203")
	rec := httptest.NewRecorder()
	h.NewsSentiment(rec, req)
	var body map[string]any
	if rec.Code == http.StatusOK {
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	}
	return rec, body
}

func TestNewsSentimentHandler_Ready(t *testing.T) {
	bullish := sentiment.Bullish
	confidence := 81
	createdAt := time.Date(2026, 9, 30, 6, 10, 0, 0, time.UTC)
	rec, body := serveNewsSentiment(t, stubNewsSentimentUsecase{result: usecase.NewsSentiment{
		Status: usecase.NewsSentimentReady,
		Stale:  true,
		Snapshot: &sentiment.Snapshot{
			Scores:    &sentiment.StockScores{Bullish: 72, Bearish: 18, Impact: 55, Confidence: 40, ShortTermUpProbability: 58},
			ScoredBy:  "claude",
			CreatedAt: createdAt,
			CheckedAt: createdAt.Add(time.Hour),
		},
		Articles: []sentiment.Article{
			{Title: "上方修正", URL: "https://example.com/a.pdf", PublishedAt: time.Date(2026, 9, 3, 6, 30, 0, 0, time.UTC), Sentiment: &bullish, SentimentConfidence: &confidence},
			{Title: "未判定", URL: "https://example.com/b.pdf", PublishedAt: time.Date(2026, 9, 2, 6, 30, 0, 0, time.UTC)},
		},
	}})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ready", body["status"])
	assert.Equal(t, true, body["stale"])
	assert.Equal(t, "claude", body["scored_by"])
	assert.Equal(t, "2026-09-30T06:10:00Z", body["scored_at"], "scored_at はスコアを計算した created_at")
	assert.Equal(t, map[string]any{
		"bullish": 72.0, "bearish": 18.0, "impact": 55.0, "confidence": 40.0, "short_term_up_probability": 58.0,
	}, body["scores"])

	articles := body["articles"].([]any)
	require.Len(t, articles, 2)
	first := articles[0].(map[string]any)
	assert.Equal(t, "上方修正", first["title"])
	assert.Equal(t, "https://example.com/a.pdf", first["url"])
	assert.Equal(t, "2026-09-03T06:30:00Z", first["published_at"])
	assert.Equal(t, "bullish", first["sentiment"])
	assert.Equal(t, 81.0, first["sentiment_confidence"])
	second := articles[1].(map[string]any)
	assert.Nil(t, second["sentiment"])
	assert.Nil(t, second["sentiment_confidence"])
}

func TestNewsSentimentHandler_ReadyWithoutScores(t *testing.T) {
	rec, body := serveNewsSentiment(t, stubNewsSentimentUsecase{result: usecase.NewsSentiment{
		Status:   usecase.NewsSentimentReady,
		Snapshot: &sentiment.Snapshot{ScoredBy: "claude", CreatedAt: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)},
		Articles: []sentiment.Article{},
	}})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, body["scores"])
	assert.Equal(t, []any{}, body["articles"])
}

func TestNewsSentimentHandler_Pending(t *testing.T) {
	rec, body := serveNewsSentiment(t, stubNewsSentimentUsecase{result: usecase.NewsSentiment{
		Status:   usecase.NewsSentimentPending,
		Articles: []sentiment.Article{},
	}})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "pending", body["status"])
	assert.Equal(t, false, body["stale"])
	assert.Nil(t, body["scores"])
	assert.Nil(t, body["scored_by"])
	assert.Nil(t, body["scored_at"])
	assert.Equal(t, []any{}, body["articles"])
}

func TestNewsSentimentHandler_Errors(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code int
	}{
		"invalid code":     {stock.ErrInvalidStockCode, http.StatusBadRequest},
		"not in watchlist": {watchlist.ErrNotFound, http.StatusNotFound},
		"internal":         {errors.New("db down"), http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			rec, _ := serveNewsSentiment(t, stubNewsSentimentUsecase{err: tc.err})
			assert.Equal(t, tc.code, rec.Code)
		})
	}
}

func TestNewsSentimentHandler_MissingUserContext(t *testing.T) {
	h := handler.NewNewsSentimentHandler(stubNewsSentimentUsecase{})
	req := httptest.NewRequest(http.MethodGet, "/stocks/7203/news-sentiment", nil)
	req.SetPathValue("code", "7203")
	rec := httptest.NewRecorder()
	h.NewsSentiment(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
