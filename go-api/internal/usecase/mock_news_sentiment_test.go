package usecase_test

import (
	"context"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/sentiment"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stretchr/testify/mock"
)

type MockDisclosureFetcher struct{ mock.Mock }

func (m *MockDisclosureFetcher) FetchDisclosures(ctx context.Context, code stock.StockCode, since time.Time) ([]news.Disclosure, error) {
	args := m.Called(ctx, code, since)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]news.Disclosure), args.Error(1)
}

type MockSentimentScorer struct{ mock.Mock }

func (m *MockSentimentScorer) Name() string { return "claude" }

func (m *MockSentimentScorer) ScoreArticles(ctx context.Context, articles []sentiment.Article) ([]sentiment.ArticleJudgement, error) {
	args := m.Called(ctx, articles)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]sentiment.ArticleJudgement), args.Error(1)
}

func (m *MockSentimentScorer) ScoreStock(ctx context.Context, articles []sentiment.Article, price sentiment.PriceContext) (sentiment.StockScores, error) {
	args := m.Called(ctx, articles, price)
	return args.Get(0).(sentiment.StockScores), args.Error(1)
}

type MockSentimentRepository struct{ mock.Mock }

func (m *MockSentimentRepository) UpsertArticles(ctx context.Context, articles []sentiment.Article) error {
	return m.Called(ctx, articles).Error(0)
}

func (m *MockSentimentRepository) SaveJudgements(ctx context.Context, ids []string, js []sentiment.ArticleJudgement, scoredBy string, at time.Time) error {
	return m.Called(ctx, ids, js, scoredBy, at).Error(0)
}

func (m *MockSentimentRepository) FindArticlesSince(ctx context.Context, stockCode string, since time.Time) ([]sentiment.Article, error) {
	args := m.Called(ctx, stockCode, since)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]sentiment.Article), args.Error(1)
}

func (m *MockSentimentRepository) FindLatestSnapshot(ctx context.Context, stockCode string) (*sentiment.Snapshot, error) {
	args := m.Called(ctx, stockCode)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sentiment.Snapshot), args.Error(1)
}

func (m *MockSentimentRepository) InsertSnapshot(ctx context.Context, s sentiment.Snapshot) (string, error) {
	args := m.Called(ctx, s)
	return args.String(0), args.Error(1)
}

func (m *MockSentimentRepository) TouchSnapshot(ctx context.Context, id string, checkedAt time.Time) error {
	return m.Called(ctx, id, checkedAt).Error(0)
}
