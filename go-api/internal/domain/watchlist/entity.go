package watchlist

import "time"

type Watchlist struct {
	ID             string
	UserID         string
	StockCode      string
	AlertThreshold float64
	CreatedAt      time.Time
}
