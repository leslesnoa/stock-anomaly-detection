package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/news"
	"github.com/stock-anomaly-detection/go-api/internal/domain/sentiment"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var sentimentNow = time.Date(2026, 9, 30, 6, 0, 0, 0, time.UTC)

type sentimentDeps struct {
	watchlists  *mockWatchlistRepository
	prices      *MockPriceRepository
	disclosures *MockDisclosureFetcher
	scorer      *MockSentimentScorer
	repo        *MockSentimentRepository
}

func newSentimentDeps(t *testing.T) sentimentDeps {
	t.Helper()
	code, _ := stock.NewStockCode("7203")
	watchlists := new(mockWatchlistRepository)
	watchlists.On("FindByUserID", mock.Anything, "user-1").Return([]watchlist.Watchlist{{StockCode: code}}, nil)
	return sentimentDeps{
		watchlists:  watchlists,
		prices:      new(MockPriceRepository),
		disclosures: new(MockDisclosureFetcher),
		scorer:      new(MockSentimentScorer),
		repo:        new(MockSentimentRepository),
	}
}

func (d sentimentDeps) usecase(waitTimeout time.Duration) *usecase.GetNewsSentimentUsecase {
	return usecase.NewGetNewsSentimentUsecase(d.watchlists, d.prices, d.disclosures, d.scorer, d.repo, anomaly.NewDetectionService(),
		usecase.NewsSentimentConfig{
			CacheTTL:       6 * time.Hour,
			WaitTimeout:    waitTimeout,
			RefreshTimeout: 5 * time.Second,
			Now:            func() time.Time { return sentimentNow },
		})
}

func labelPtr(l sentiment.Label) *sentiment.Label { return &l }
func intPtr(v int) *int                           { return &v }

func scoredArticle(id, tdnetID string) sentiment.Article {
	return sentiment.Article{ID: id, StockCode: "7203", TdnetID: tdnetID, Title: "t-" + tdnetID,
		PublishedAt: sentimentNow.AddDate(0, 0, -3), Sentiment: labelPtr(sentiment.Bullish), Confidence: intPtr(80)}
}

func unscoredArticle(id, tdnetID string) sentiment.Article {
	return sentiment.Article{ID: id, StockCode: "7203", TdnetID: tdnetID, Title: "t-" + tdnetID,
		PublishedAt: sentimentNow.AddDate(0, 0, -1)}
}

func TestGetNewsSentiment_FreshCacheSkipsRefresh(t *testing.T) {
	d := newSentimentDeps(t)
	cached := &sentiment.Snapshot{ID: "snap-1", StockCode: "7203", CheckedAt: sentimentNow.Add(-time.Hour)}
	articles := []sentiment.Article{scoredArticle("a", "1")}
	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(cached, nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", sentimentNow.AddDate(0, 0, -90)).Return(articles, nil)
	uc := d.usecase(2 * time.Second)

	got, err := uc.Handle(context.Background(), "user-1", "7203")
	require.NoError(t, err)
	assert.Equal(t, usecase.NewsSentimentReady, got.Status)
	assert.False(t, got.Stale)
	assert.Same(t, cached, got.Snapshot)
	assert.Equal(t, articles, got.Articles)
	d.disclosures.AssertNotCalled(t, "FetchDisclosures", mock.Anything, mock.Anything, mock.Anything)
	d.scorer.AssertNotCalled(t, "ScoreStock", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetNewsSentiment_ExpiredCacheWithSameInputOnlyTouches(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	articles := []sentiment.Article{scoredArticle("a", "1")}
	quotes := []stock.Quote{{Price: 1000, Date: "2026-09-28"}, {Price: 1010, Date: "2026-09-29"}}
	prevClose := 1010.0
	prev := &sentiment.Snapshot{ID: "snap-1", StockCode: "7203", InputFingerprint: sentiment.Fingerprint(articles, "2026-09-29"),
		BasePriceDate: "2026-09-29", BaseClose: &prevClose,
		CreatedAt: sentimentNow.Add(-30 * time.Hour), CheckedAt: sentimentNow.Add(-7 * time.Hour)}

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(prev, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).
		Return([]news.Disclosure{{TdnetID: "1", Title: "t-1", PublishedAt: sentimentNow.AddDate(0, 0, -3)}}, nil)
	d.repo.On("UpsertArticles", mock.Anything, mock.Anything).Return(nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", mock.Anything).Return(articles, nil)
	d.prices.On("FindRecent", mock.Anything, code, 30).Return(quotes, nil)
	d.repo.On("TouchSnapshot", mock.Anything, "snap-1", sentimentNow).Return(nil)
	uc := d.usecase(2 * time.Second)

	got, err := uc.Handle(context.Background(), "user-1", "7203")
	uc.Wait()
	require.NoError(t, err)
	assert.Equal(t, usecase.NewsSentimentReady, got.Status)
	assert.False(t, got.Stale)
	assert.Equal(t, "snap-1", got.Snapshot.ID)
	assert.Equal(t, sentimentNow, got.Snapshot.CheckedAt)
	assert.Equal(t, sentimentNow.Add(-7*time.Hour), prev.CheckedAt, "リポジトリから返されたスナップショットは書き換えない")
	assert.Equal(t, "2026-09-29", got.Snapshot.BasePriceDate, "touchだけの時は前回の基準日を保つ")
	assert.Equal(t, &prevClose, got.Snapshot.BaseClose)
	d.scorer.AssertNotCalled(t, "ScoreStock", mock.Anything, mock.Anything, mock.Anything)
	d.scorer.AssertNotCalled(t, "ScoreArticles", mock.Anything, mock.Anything)
	d.repo.AssertNotCalled(t, "InsertSnapshot", mock.Anything, mock.Anything)
}

func TestGetNewsSentiment_NewArticlesScoresOnlyUnscoredAndInsertsSnapshot(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	articles := []sentiment.Article{unscoredArticle("b", "2"), scoredArticle("a", "1")}
	quotes := buildQuotes(30)
	scores := sentiment.StockScores{Bullish: 72, Bearish: 18, Impact: 55, Confidence: 40, ShortTermUp: 58}

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, sentimentNow.AddDate(0, 0, -90)).Return([]news.Disclosure{
		{TdnetID: "2", Title: "t-2", URL: "https://example.com/2.pdf", PublishedAt: sentimentNow.AddDate(0, 0, -1)},
		{TdnetID: "1", Title: "t-1", URL: "https://example.com/1.pdf", PublishedAt: sentimentNow.AddDate(0, 0, -3)},
	}, nil)
	d.repo.On("UpsertArticles", mock.Anything, mock.MatchedBy(func(as []sentiment.Article) bool {
		return len(as) == 2 && as[0].StockCode == "7203" && as[0].TdnetID == "2" && as[0].URL == "https://example.com/2.pdf"
	})).Return(nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", sentimentNow.AddDate(0, 0, -90)).Return(articles, nil)
	d.scorer.On("ScoreArticles", mock.Anything, mock.MatchedBy(func(as []sentiment.Article) bool {
		return len(as) == 1 && as[0].TdnetID == "2"
	})).Return([]sentiment.ArticleJudgement{{Sentiment: sentiment.Bearish, Confidence: 66}}, nil)
	d.repo.On("SaveJudgements", mock.Anything, []string{"b"}, []sentiment.ArticleJudgement{{Sentiment: sentiment.Bearish, Confidence: 66}}, "claude", sentimentNow).Return(nil)
	d.prices.On("FindRecent", mock.Anything, code, 30).Return(quotes, nil)
	d.scorer.On("ScoreStock", mock.Anything, mock.MatchedBy(func(as []sentiment.Article) bool {
		return len(as) == 2 && as[0].Sentiment != nil && *as[0].Sentiment == sentiment.Bearish
	}), mock.MatchedBy(func(p sentiment.PriceContext) bool {
		return p.LatestDate == quotes[29].Date && p.Return5d != nil && p.Return20d != nil && p.ZScore != nil
	})).Return(scores, nil)
	d.repo.On("InsertSnapshot", mock.Anything, mock.MatchedBy(func(s sentiment.Snapshot) bool {
		return s.StockCode == "7203" && s.Scores != nil && *s.Scores == scores && s.ArticleCount == 2 &&
			s.ScoredBy == "claude" && s.InputFingerprint == sentiment.Fingerprint(articles, quotes[29].Date) &&
			s.BasePriceDate == quotes[29].Date && s.BaseClose != nil && *s.BaseClose == float64(quotes[29].Price) &&
			s.CreatedAt.Equal(sentimentNow) && s.CheckedAt.Equal(sentimentNow)
	})).Return("snap-new", nil)
	uc := d.usecase(2 * time.Second)

	got, err := uc.Handle(context.Background(), "user-1", "7203")
	uc.Wait()
	require.NoError(t, err)
	assert.Equal(t, usecase.NewsSentimentReady, got.Status)
	assert.Equal(t, "snap-new", got.Snapshot.ID)
	assert.Equal(t, scores, *got.Snapshot.Scores)
	d.repo.AssertExpectations(t)
	d.scorer.AssertExpectations(t)
}

func TestGetNewsSentiment_ArticleScoringFailureStillScoresStock(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	articles := []sentiment.Article{unscoredArticle("b", "2")}
	scores := sentiment.StockScores{Bullish: 10, Bearish: 10, Impact: 10, Confidence: 10, ShortTermUp: 50}

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).Return([]news.Disclosure{{TdnetID: "2", Title: "t-2"}}, nil)
	d.repo.On("UpsertArticles", mock.Anything, mock.Anything).Return(nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", mock.Anything).Return(articles, nil)
	d.scorer.On("ScoreArticles", mock.Anything, mock.Anything).Return(nil, errors.New("claude down"))
	d.prices.On("FindRecent", mock.Anything, code, 30).Return(buildQuotes(30), nil)
	d.scorer.On("ScoreStock", mock.Anything, mock.Anything, mock.Anything).Return(scores, nil)
	d.repo.On("InsertSnapshot", mock.Anything, mock.Anything).Return("snap-new", nil)
	uc := d.usecase(2 * time.Second)

	got, err := uc.Handle(context.Background(), "user-1", "7203")
	uc.Wait()
	require.NoError(t, err)
	assert.Equal(t, usecase.NewsSentimentReady, got.Status)
	d.repo.AssertNotCalled(t, "SaveJudgements", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestGetNewsSentiment_ManyUnscoredArticlesAreScoredInBatches(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	articles := make([]sentiment.Article, 25)
	for i := range articles {
		articles[i] = unscoredArticle(fmt.Sprintf("id%d", i), fmt.Sprintf("t%d", i))
	}
	firstChunkJudgements := make([]sentiment.ArticleJudgement, 20)
	for i := range firstChunkJudgements {
		firstChunkJudgements[i] = sentiment.ArticleJudgement{Sentiment: sentiment.Bullish, Confidence: 50}
	}
	secondChunkJudgements := make([]sentiment.ArticleJudgement, 5)
	for i := range secondChunkJudgements {
		secondChunkJudgements[i] = sentiment.ArticleJudgement{Sentiment: sentiment.Bearish, Confidence: 60}
	}
	firstChunkIDs := make([]string, 20)
	for i, a := range articles[:20] {
		firstChunkIDs[i] = a.ID
	}
	secondChunkIDs := make([]string, 5)
	for i, a := range articles[20:] {
		secondChunkIDs[i] = a.ID
	}
	scores := sentiment.StockScores{Bullish: 40, Bearish: 30, Impact: 20, Confidence: 10, ShortTermUp: 50}

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).Return([]news.Disclosure{}, nil)
	d.repo.On("UpsertArticles", mock.Anything, mock.Anything).Return(nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", mock.Anything).Return(articles, nil)
	d.scorer.On("ScoreArticles", mock.Anything, mock.MatchedBy(func(as []sentiment.Article) bool {
		return len(as) == 20
	})).Return(firstChunkJudgements, nil).Once()
	d.scorer.On("ScoreArticles", mock.Anything, mock.MatchedBy(func(as []sentiment.Article) bool {
		return len(as) == 5
	})).Return(secondChunkJudgements, nil).Once()
	d.repo.On("SaveJudgements", mock.Anything, firstChunkIDs, firstChunkJudgements, "claude", sentimentNow).Return(nil).Once()
	d.repo.On("SaveJudgements", mock.Anything, secondChunkIDs, secondChunkJudgements, "claude", sentimentNow).Return(nil).Once()
	d.prices.On("FindRecent", mock.Anything, code, 30).Return(buildQuotes(30), nil)
	d.scorer.On("ScoreStock", mock.Anything, mock.Anything, mock.Anything).Return(scores, nil)
	d.repo.On("InsertSnapshot", mock.Anything, mock.Anything).Return("snap-new", nil)
	uc := d.usecase(2 * time.Second)

	got, err := uc.Handle(context.Background(), "user-1", "7203")
	uc.Wait()
	require.NoError(t, err)
	assert.Equal(t, usecase.NewsSentimentReady, got.Status)
	d.scorer.AssertNumberOfCalls(t, "ScoreArticles", 2)
	d.repo.AssertNumberOfCalls(t, "SaveJudgements", 2)

	// 呼び出し順（20件のチャンクが先、5件のチャンクが後）を確認する。
	var scoreArticleCalls []mock.Call
	for _, c := range d.scorer.Calls {
		if c.Method == "ScoreArticles" {
			scoreArticleCalls = append(scoreArticleCalls, c)
		}
	}
	require.Len(t, scoreArticleCalls, 2)
	assert.Len(t, scoreArticleCalls[0].Arguments.Get(1).([]sentiment.Article), 20)
	assert.Len(t, scoreArticleCalls[1].Arguments.Get(1).([]sentiment.Article), 5)
}

func TestGetNewsSentiment_FirstChunkScoringFailureStillScoresSecondChunkAndStock(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	articles := make([]sentiment.Article, 25)
	for i := range articles {
		articles[i] = unscoredArticle(fmt.Sprintf("id%d", i), fmt.Sprintf("t%d", i))
	}
	secondChunkJudgements := make([]sentiment.ArticleJudgement, 5)
	for i := range secondChunkJudgements {
		secondChunkJudgements[i] = sentiment.ArticleJudgement{Sentiment: sentiment.Neutral, Confidence: 55}
	}
	secondChunkIDs := make([]string, 5)
	for i, a := range articles[20:] {
		secondChunkIDs[i] = a.ID
	}
	scores := sentiment.StockScores{Bullish: 10, Bearish: 10, Impact: 10, Confidence: 10, ShortTermUp: 50}

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).Return([]news.Disclosure{}, nil)
	d.repo.On("UpsertArticles", mock.Anything, mock.Anything).Return(nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", mock.Anything).Return(articles, nil)
	d.scorer.On("ScoreArticles", mock.Anything, mock.MatchedBy(func(as []sentiment.Article) bool {
		return len(as) == 20
	})).Return(nil, errors.New("claude down")).Once()
	d.scorer.On("ScoreArticles", mock.Anything, mock.MatchedBy(func(as []sentiment.Article) bool {
		return len(as) == 5
	})).Return(secondChunkJudgements, nil).Once()
	d.repo.On("SaveJudgements", mock.Anything, secondChunkIDs, secondChunkJudgements, "claude", sentimentNow).Return(nil).Once()
	d.prices.On("FindRecent", mock.Anything, code, 30).Return(buildQuotes(30), nil)
	d.scorer.On("ScoreStock", mock.Anything, mock.Anything, mock.Anything).Return(scores, nil)
	d.repo.On("InsertSnapshot", mock.Anything, mock.Anything).Return("snap-new", nil)
	uc := d.usecase(2 * time.Second)

	got, err := uc.Handle(context.Background(), "user-1", "7203")
	uc.Wait()
	require.NoError(t, err)
	assert.Equal(t, usecase.NewsSentimentReady, got.Status)
	d.scorer.AssertNumberOfCalls(t, "ScoreArticles", 2)
	d.repo.AssertNumberOfCalls(t, "SaveJudgements", 1)
	d.scorer.AssertCalled(t, "ScoreStock", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetNewsSentiment_ShortPriceHistoryPassesNilIndicators(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	articles := []sentiment.Article{scoredArticle("a", "1")}
	quotes := buildQuotes(3)

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).Return([]news.Disclosure{{TdnetID: "1", Title: "t-1"}}, nil)
	d.repo.On("UpsertArticles", mock.Anything, mock.Anything).Return(nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", mock.Anything).Return(articles, nil)
	d.prices.On("FindRecent", mock.Anything, code, 30).Return(quotes, nil)
	d.scorer.On("ScoreStock", mock.Anything, mock.Anything, sentiment.PriceContext{LatestDate: quotes[2].Date}).
		Return(sentiment.StockScores{Confidence: 5, ShortTermUp: 50}, nil)
	d.repo.On("InsertSnapshot", mock.Anything, mock.Anything).Return("snap-new", nil)
	uc := d.usecase(2 * time.Second)

	got, err := uc.Handle(context.Background(), "user-1", "7203")
	uc.Wait()
	require.NoError(t, err)
	assert.Equal(t, usecase.NewsSentimentReady, got.Status)
	d.scorer.AssertExpectations(t)
}

func TestGetNewsSentiment_NoArticlesInsertsEmptySnapshotWithoutAI(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).Return([]news.Disclosure{}, nil)
	d.repo.On("UpsertArticles", mock.Anything, mock.Anything).Return(nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", mock.Anything).Return([]sentiment.Article{}, nil)
	d.prices.On("FindRecent", mock.Anything, code, 30).Return(buildQuotes(30), nil)
	d.repo.On("InsertSnapshot", mock.Anything, mock.MatchedBy(func(s sentiment.Snapshot) bool {
		return s.Scores == nil && s.ArticleCount == 0
	})).Return("snap-empty", nil)
	uc := d.usecase(2 * time.Second)

	got, err := uc.Handle(context.Background(), "user-1", "7203")
	uc.Wait()
	require.NoError(t, err)
	assert.Equal(t, usecase.NewsSentimentReady, got.Status)
	assert.Nil(t, got.Snapshot.Scores)
	assert.Empty(t, got.Articles)
	d.scorer.AssertNotCalled(t, "ScoreArticles", mock.Anything, mock.Anything)
	d.scorer.AssertNotCalled(t, "ScoreStock", mock.Anything, mock.Anything, mock.Anything)
}

func TestGetNewsSentiment_RefreshFailureFallsBackToStaleSnapshot(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	old := &sentiment.Snapshot{ID: "snap-old", StockCode: "7203", InputFingerprint: "old", CheckedAt: sentimentNow.Add(-7 * time.Hour)}
	articles := []sentiment.Article{scoredArticle("a", "1")}

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(old, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).Return([]news.Disclosure{{TdnetID: "1", Title: "t-1"}}, nil)
	d.repo.On("UpsertArticles", mock.Anything, mock.Anything).Return(nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", mock.Anything).Return(articles, nil)
	d.prices.On("FindRecent", mock.Anything, code, 30).Return(buildQuotes(30), nil)
	d.scorer.On("ScoreStock", mock.Anything, mock.Anything, mock.Anything).Return(sentiment.StockScores{}, errors.New("claude down"))
	uc := d.usecase(2 * time.Second)

	got, err := uc.Handle(context.Background(), "user-1", "7203")
	uc.Wait()
	require.NoError(t, err)
	assert.Equal(t, usecase.NewsSentimentReady, got.Status)
	assert.True(t, got.Stale)
	assert.Same(t, old, got.Snapshot)
	assert.Equal(t, articles, got.Articles)
}

func TestGetNewsSentiment_SlowRefreshWithoutSnapshotReturnsPending(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	release := make(chan struct{})

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).
		Run(func(mock.Arguments) { <-release }).
		Return(nil, errors.New("tdnet timeout"))
	uc := d.usecase(50 * time.Millisecond)

	got, err := uc.Handle(context.Background(), "user-1", "7203")
	require.NoError(t, err)
	assert.Equal(t, usecase.NewsSentimentPending, got.Status)
	assert.Nil(t, got.Snapshot)
	assert.NotNil(t, got.Articles)
	assert.Empty(t, got.Articles)

	close(release)
	uc.Wait()
}

func TestGetNewsSentiment_FailedRefreshIsRetriedOnNextRequest(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).Return(nil, errors.New("tdnet down"))
	uc := d.usecase(2 * time.Second)

	for i := 0; i < 2; i++ {
		got, err := uc.Handle(context.Background(), "user-1", "7203")
		require.NoError(t, err)
		assert.Equal(t, usecase.NewsSentimentPending, got.Status)
	}
	uc.Wait()
	d.disclosures.AssertNumberOfCalls(t, "FetchDisclosures", 2)
}

func TestGetNewsSentiment_ConcurrentRequestsShareOneRefresh(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).
		Run(func(mock.Arguments) {
			once.Do(func() { close(started) })
			<-release
		}).
		Return(nil, errors.New("tdnet down"))
	uc := d.usecase(2 * time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := uc.Handle(context.Background(), "user-1", "7203")
			assert.NoError(t, err)
		}()
	}
	<-started
	// 2本目のHandleがsingleflightに合流するまでの猶予。合流までの処理はモック呼び出しのみで数μs。
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	uc.Wait()

	d.disclosures.AssertNumberOfCalls(t, "FetchDisclosures", 1)
}

func TestGetNewsSentiment_NotInWatchlist(t *testing.T) {
	d := newSentimentDeps(t)
	uc := d.usecase(2 * time.Second)

	_, err := uc.Handle(context.Background(), "user-1", "6758")
	require.ErrorIs(t, err, watchlist.ErrNotFound)
	d.repo.AssertNotCalled(t, "FindLatestSnapshot", mock.Anything, mock.Anything)
}

func TestGetNewsSentiment_InvalidStockCode(t *testing.T) {
	d := newSentimentDeps(t)
	uc := d.usecase(2 * time.Second)

	_, err := uc.Handle(context.Background(), "user-1", "abc")
	require.ErrorIs(t, err, stock.ErrInvalidStockCode)
}

func TestGetNewsSentiment_NoPricesSavesEmptyBasePrice(t *testing.T) {
	d := newSentimentDeps(t)
	code, _ := stock.NewStockCode("7203")
	articles := []sentiment.Article{scoredArticle("a", "1")}
	scores := sentiment.StockScores{Bullish: 50, Bearish: 50, Impact: 50, Confidence: 50, ShortTermUp: 50}

	d.repo.On("FindLatestSnapshot", mock.Anything, "7203").Return(nil, nil)
	d.disclosures.On("FetchDisclosures", mock.Anything, code, mock.Anything).Return([]news.Disclosure{}, nil)
	d.repo.On("UpsertArticles", mock.Anything, mock.Anything).Return(nil)
	d.repo.On("FindArticlesSince", mock.Anything, "7203", mock.Anything).Return(articles, nil)
	d.prices.On("FindRecent", mock.Anything, code, 30).Return([]stock.Quote{}, nil)
	d.scorer.On("ScoreStock", mock.Anything, mock.Anything, mock.Anything).Return(scores, nil)
	d.repo.On("InsertSnapshot", mock.Anything, mock.MatchedBy(func(s sentiment.Snapshot) bool {
		return s.BasePriceDate == "" && s.BaseClose == nil
	})).Return("snap-new", nil)
	uc := d.usecase(2 * time.Second)

	_, err := uc.Handle(context.Background(), "user-1", "7203")
	uc.Wait()
	require.NoError(t, err)
	d.repo.AssertExpectations(t)
}
