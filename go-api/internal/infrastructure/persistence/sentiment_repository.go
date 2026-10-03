package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stock-anomaly-detection/go-api/internal/domain/sentiment"
)

type PgSentimentRepository struct {
	conn *pgxpool.Pool
}

func NewPgSentimentRepository(conn *pgxpool.Pool) *PgSentimentRepository {
	return &PgSentimentRepository{conn: conn}
}

func (r *PgSentimentRepository) InsertNewArticles(ctx context.Context, articles []sentiment.Article) error {
	if len(articles) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, a := range articles {
		batch.Queue(
			`INSERT INTO news_articles (stock_code, tdnet_id, title, url, published_at)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (stock_code, tdnet_id) DO NOTHING`,
			a.StockCode, a.TdnetID, a.Title, a.URL, a.PublishedAt)
	}
	if err := r.conn.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("upsert news articles: %w", err)
	}
	return nil
}

func (r *PgSentimentRepository) SaveJudgements(ctx context.Context, articleIDs []string, judgements []sentiment.ArticleJudgement, scoredBy string, at time.Time) error {
	if len(articleIDs) != len(judgements) {
		return fmt.Errorf("save judgements: %d ids but %d judgements", len(articleIDs), len(judgements))
	}
	if len(articleIDs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for i, id := range articleIDs {
		batch.Queue(
			`UPDATE news_articles
			 SET sentiment = $2, sentiment_confidence = $3, scored_by = $4, scored_at = $5
			 WHERE id = $1`,
			id, string(judgements[i].Sentiment), judgements[i].Confidence, scoredBy, at)
	}
	br := r.conn.SendBatch(ctx, batch)
	defer br.Close()
	for _, id := range articleIDs {
		tag, err := br.Exec()
		if err != nil {
			return fmt.Errorf("save judgements: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("save judgements: article %s not found", id)
		}
	}
	if err := br.Close(); err != nil {
		return fmt.Errorf("save judgements: %w", err)
	}
	return nil
}

func (r *PgSentimentRepository) FindArticlesSince(ctx context.Context, stockCode string, since time.Time) ([]sentiment.Article, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT id::text, stock_code, tdnet_id, title, url, published_at,
		        sentiment, sentiment_confidence, scored_by, scored_at
		 FROM news_articles
		 WHERE stock_code = $1 AND published_at >= $2
		 ORDER BY published_at DESC, tdnet_id DESC`,
		stockCode, since)
	if err != nil {
		return nil, fmt.Errorf("find news articles %s: %w", stockCode, err)
	}
	defer rows.Close()

	articles := []sentiment.Article{}
	for rows.Next() {
		var a sentiment.Article
		var label *string
		if err := rows.Scan(&a.ID, &a.StockCode, &a.TdnetID, &a.Title, &a.URL, &a.PublishedAt,
			&label, &a.SentimentConfidence, &a.ScoredBy, &a.ScoredAt); err != nil {
			return nil, fmt.Errorf("scan news article %s: %w", stockCode, err)
		}
		if label != nil {
			l := sentiment.Label(*label)
			a.Sentiment = &l
		}
		articles = append(articles, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("find news articles %s: %w", stockCode, err)
	}
	return articles, nil
}

func (r *PgSentimentRepository) FindLatestSnapshot(ctx context.Context, stockCode string) (*sentiment.Snapshot, error) {
	var s sentiment.Snapshot
	var bullish, bearish, impact, confidence, shortTermUp *int
	var basePriceDate *time.Time
	err := r.conn.QueryRow(ctx,
		`SELECT id::text, stock_code, bullish_score, bearish_score, impact_score, confidence_score,
		        short_term_up_probability, article_count, input_fingerprint, scored_by,
		        base_price_date, base_close::float8, created_at, checked_at
		 FROM stock_sentiment_snapshots
		 WHERE stock_code = $1
		 ORDER BY created_at DESC
		 LIMIT 1`,
		stockCode).Scan(&s.ID, &s.StockCode, &bullish, &bearish, &impact, &confidence, &shortTermUp,
		&s.ArticleCount, &s.InputFingerprint, &s.ScoredBy, &basePriceDate, &s.BaseClose, &s.CreatedAt, &s.CheckedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find latest sentiment snapshot %s: %w", stockCode, err)
	}
	if basePriceDate != nil {
		s.BasePriceDate = basePriceDate.Format(dateLayout)
	}
	if bullish != nil && bearish != nil && impact != nil && confidence != nil && shortTermUp != nil {
		s.Scores = &sentiment.StockScores{
			Bullish: *bullish, Bearish: *bearish, Impact: *impact,
			Confidence: *confidence, ShortTermUpProbability: *shortTermUp,
		}
	}
	return &s, nil
}

func (r *PgSentimentRepository) InsertSnapshot(ctx context.Context, s sentiment.Snapshot) (string, error) {
	var bullish, bearish, impact, confidence, shortTermUp *int
	if s.Scores != nil {
		bullish, bearish, impact = &s.Scores.Bullish, &s.Scores.Bearish, &s.Scores.Impact
		confidence, shortTermUp = &s.Scores.Confidence, &s.Scores.ShortTermUpProbability
	}
	var basePriceDate *time.Time
	if s.BasePriceDate != "" {
		d, err := time.Parse(dateLayout, s.BasePriceDate)
		if err != nil {
			return "", fmt.Errorf("insert sentiment snapshot %s: parse base price date: %w", s.StockCode, err)
		}
		basePriceDate = &d
	}
	var id string
	err := r.conn.QueryRow(ctx,
		`INSERT INTO stock_sentiment_snapshots
		   (stock_code, bullish_score, bearish_score, impact_score, confidence_score,
		    short_term_up_probability, article_count, input_fingerprint, scored_by,
		    base_price_date, base_close, created_at, checked_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 RETURNING id::text`,
		s.StockCode, bullish, bearish, impact, confidence, shortTermUp,
		s.ArticleCount, s.InputFingerprint, s.ScoredBy, basePriceDate, s.BaseClose, s.CreatedAt, s.CheckedAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert sentiment snapshot %s: %w", s.StockCode, err)
	}
	return id, nil
}

func (r *PgSentimentRepository) TouchSnapshot(ctx context.Context, id string, checkedAt time.Time) error {
	tag, err := r.conn.Exec(ctx,
		`UPDATE stock_sentiment_snapshots SET checked_at = $2 WHERE id = $1`, id, checkedAt)
	if err != nil {
		return fmt.Errorf("touch sentiment snapshot %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("touch sentiment snapshot %s: not found", id)
	}
	return nil
}
