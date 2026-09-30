package news

import (
	"context"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type Disclosure struct {
	TdnetID     string
	Title       string
	URL         string
	PublishedAt time.Time
}

type DisclosureFetcher interface {
	FetchDisclosures(ctx context.Context, code stock.StockCode, since time.Time) ([]Disclosure, error)
}
