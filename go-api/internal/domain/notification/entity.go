package notification

import (
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/analysis"
)

type Notification struct {
	UserID              *string
	StockCode           string
	AnomalyScore        float64
	AIReport            string
	TechnicalIndicators analysis.Indicators
	SlackSent           bool
	NotifiedAt          time.Time
}
