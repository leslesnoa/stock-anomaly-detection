package watchlist

import (
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type Watchlist struct {
	ID             string
	UserID         string
	StockCode      stock.StockCode
	AlertThreshold float64
	CreatedAt      time.Time
}
