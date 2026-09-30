package persistence_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stock-anomaly-detection/go-api/internal/domain/sentiment"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSentimentRepo(t *testing.T) (*persistence.PgSentimentRepository, *pgxpool.Pool) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	conn, err := persistence.Connect(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	_, err = conn.Exec(ctx, "TRUNCATE news_articles, stock_sentiment_snapshots")
	require.NoError(t, err)
	return persistence.NewPgSentimentRepository(conn), conn
}

func TestPgSentimentRepository_UpsertArticles_IgnoresDuplicates(t *testing.T) {
	repo, conn := newSentimentRepo(t)
	ctx := context.Background()
	published := time.Date(2026, 9, 3, 6, 30, 0, 0, time.UTC)

	article := sentiment.Article{StockCode: "7203", TdnetID: "1279204", Title: "自己株式の取得状況に関するお知らせ", URL: "https://example.com/a.pdf", PublishedAt: published}
	require.NoError(t, repo.UpsertArticles(ctx, []sentiment.Article{article}))
	article.Title = "上書きされないこと"
	require.NoError(t, repo.UpsertArticles(ctx, []sentiment.Article{article}))
	require.NoError(t, repo.UpsertArticles(ctx, nil))

	var count int
	require.NoError(t, conn.QueryRow(ctx, "SELECT COUNT(*) FROM news_articles").Scan(&count))
	assert.Equal(t, 1, count)

	got, err := repo.FindArticlesSince(ctx, "7203", published.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "自己株式の取得状況に関するお知らせ", got[0].Title)
	assert.NotEmpty(t, got[0].ID)
	assert.Nil(t, got[0].Sentiment)
	assert.True(t, got[0].PublishedAt.Equal(published))
}

func TestPgSentimentRepository_FindArticlesSince_FiltersAndOrders(t *testing.T) {
	repo, _ := newSentimentRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.UpsertArticles(ctx, []sentiment.Article{
		{StockCode: "7203", TdnetID: "old", Title: "old", URL: "u", PublishedAt: base.AddDate(0, 0, -100)},
		{StockCode: "7203", TdnetID: "a", Title: "a", URL: "u", PublishedAt: base},
		{StockCode: "7203", TdnetID: "b", Title: "b", URL: "u", PublishedAt: base.AddDate(0, 0, 2)},
		{StockCode: "6758", TdnetID: "other", Title: "other", URL: "u", PublishedAt: base},
	}))

	got, err := repo.FindArticlesSince(ctx, "7203", base.AddDate(0, 0, -90))
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "b", got[0].TdnetID, "公開日の降順")
	assert.Equal(t, "a", got[1].TdnetID)

	empty, err := repo.FindArticlesSince(ctx, "9999", base)
	require.NoError(t, err)
	assert.NotNil(t, empty)
	assert.Empty(t, empty)
}

func TestPgSentimentRepository_SaveJudgements(t *testing.T) {
	repo, _ := newSentimentRepo(t)
	ctx := context.Background()
	published := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.UpsertArticles(ctx, []sentiment.Article{
		{StockCode: "7203", TdnetID: "1", Title: "t", URL: "u", PublishedAt: published},
	}))
	articles, err := repo.FindArticlesSince(ctx, "7203", published.Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, articles, 1)

	scoredAt := time.Date(2026, 9, 30, 6, 0, 0, 0, time.UTC)
	require.NoError(t, repo.SaveJudgements(ctx, []string{articles[0].ID},
		[]sentiment.ArticleJudgement{{Sentiment: sentiment.Bullish, Confidence: 81}}, "claude", scoredAt))

	got, err := repo.FindArticlesSince(ctx, "7203", published.Add(-time.Hour))
	require.NoError(t, err)
	require.NotNil(t, got[0].Sentiment)
	assert.Equal(t, sentiment.Bullish, *got[0].Sentiment)
	assert.Equal(t, 81, *got[0].Confidence)
	assert.Equal(t, "claude", *got[0].ScoredBy)
	assert.True(t, got[0].ScoredAt.Equal(scoredAt))

	require.Error(t, repo.SaveJudgements(ctx, []string{articles[0].ID}, nil, "claude", scoredAt), "件数不一致はエラー")
}

func TestPgSentimentRepository_Snapshots(t *testing.T) {
	repo, _ := newSentimentRepo(t)
	ctx := context.Background()

	none, err := repo.FindLatestSnapshot(ctx, "7203")
	require.NoError(t, err)
	assert.Nil(t, none)

	older := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	_, err = repo.InsertSnapshot(ctx, sentiment.Snapshot{StockCode: "7203", ArticleCount: 0, InputFingerprint: "fp-0", ScoredBy: "claude", CreatedAt: older, CheckedAt: older})
	require.NoError(t, err)

	newer := older.Add(7 * time.Hour)
	scores := sentiment.StockScores{Bullish: 72, Bearish: 18, Impact: 55, Confidence: 40, ShortTermUp: 58}
	id, err := repo.InsertSnapshot(ctx, sentiment.Snapshot{StockCode: "7203", Scores: &scores, ArticleCount: 3, InputFingerprint: "fp-1", ScoredBy: "claude", CreatedAt: newer, CheckedAt: newer})
	require.NoError(t, err)
	require.NotEmpty(t, id)

	latest, err := repo.FindLatestSnapshot(ctx, "7203")
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, id, latest.ID)
	require.NotNil(t, latest.Scores)
	assert.Equal(t, scores, *latest.Scores)
	assert.Equal(t, 3, latest.ArticleCount)
	assert.Equal(t, "fp-1", latest.InputFingerprint)

	checked := newer.Add(6 * time.Hour)
	require.NoError(t, repo.TouchSnapshot(ctx, id, checked))
	touched, err := repo.FindLatestSnapshot(ctx, "7203")
	require.NoError(t, err)
	assert.True(t, touched.CheckedAt.Equal(checked))
	assert.True(t, touched.CreatedAt.Equal(newer), "created_at は変わらない")

	require.Error(t, repo.TouchSnapshot(ctx, "00000000-0000-0000-0000-000000000000", checked), "存在しないidはエラー")
}

func TestPgSentimentRepository_SnapshotWithoutScores(t *testing.T) {
	repo, _ := newSentimentRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	_, err := repo.InsertSnapshot(ctx, sentiment.Snapshot{StockCode: "7203", InputFingerprint: "fp", ScoredBy: "claude", CreatedAt: now, CheckedAt: now})
	require.NoError(t, err)

	latest, err := repo.FindLatestSnapshot(ctx, "7203")
	require.NoError(t, err)
	assert.Nil(t, latest.Scores)
}
