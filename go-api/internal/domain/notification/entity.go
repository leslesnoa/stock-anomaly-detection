package notification

import "time"

type Notification struct {
	UserID              *string
	StockCode           string
	AnomalyScore        float64
	AIReport            string
	TechnicalIndicators string
	SlackSent           bool
	NotifiedAt          time.Time
}
