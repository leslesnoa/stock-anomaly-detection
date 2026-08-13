package analysis

import "github.com/stock-anomaly-detection/go-api/internal/domain/stock"

type MACD struct {
	Line      *float64 `json:"line"`
	Signal    *float64 `json:"signal"`
	Histogram *float64 `json:"histogram"`
}

type Bollinger struct {
	Upper  *float64 `json:"upper"`
	Middle *float64 `json:"middle"`
	Lower  *float64 `json:"lower"`
}

type Indicators struct {
	RSI       *float64  `json:"rsi"`
	MACD      MACD      `json:"macd"`
	Bollinger Bollinger `json:"bollinger"`
}

type Analyzer interface {
	Analyze(code stock.StockCode, zScore, currentPrice float64, prices []float64) (Indicators, error)
}
