package news

import "github.com/stock-anomaly-detection/go-api/internal/domain/stock"

type Item struct {
	Headline string
	Summary  string
}

type Fetcher interface {
	FetchRecent(code stock.StockCode) ([]Item, error)
}
