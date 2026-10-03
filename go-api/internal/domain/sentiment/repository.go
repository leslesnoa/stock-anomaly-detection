package sentiment

import (
	"context"
	"time"
)

type Scorer interface {
	// Name は scored_by としてDBに保存されるため、固定の識別子を返すこと。
	Name() string
	// ScoreArticles は入力と同じ順・同じ件数の判定を返す。
	ScoreArticles(ctx context.Context, articles []Article) ([]ArticleJudgement, error)
	ScoreStock(ctx context.Context, articles []Article, price PriceContext) (StockScores, error)
}

type Repository interface {
	// InsertNewArticles は (stock_code, tdnet_id) が既にある記事を無視して保存する。
	InsertNewArticles(ctx context.Context, articles []Article) error
	SaveJudgements(ctx context.Context, articleIDs []string, judgements []ArticleJudgement, scoredBy string, at time.Time) error
	// FindArticlesSince は公開日の降順で返す。該当なしなら空スライス。
	FindArticlesSince(ctx context.Context, stockCode string, since time.Time) ([]Article, error)
	// FindLatestSnapshot は created_at が最新のスナップショットを返す。無ければ nil, nil。
	FindLatestSnapshot(ctx context.Context, stockCode string) (*Snapshot, error)
	// InsertSnapshot は採番された id を返す。
	InsertSnapshot(ctx context.Context, s Snapshot) (string, error)
	TouchSnapshot(ctx context.Context, id string, checkedAt time.Time) error
}
