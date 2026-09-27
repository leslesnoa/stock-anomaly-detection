package forecast

import "github.com/stock-anomaly-detection/go-api/internal/domain/stock"

type Point struct {
	Step    int
	Center  float64
	Upper68 float64
	Lower68 float64
	Upper95 float64
	Lower95 float64
}

type Forecast struct {
	Horizon int
	Points  []Point
}

// Forecaster は将来の価格レンジ（ドリフト＋ボラティリティ帯）を計算する。
// 点予測ではなく方向とレンジ帯を返す方針は
// docs/superpowers/specs/2026-09-24-stock-chart-forecast-design.md 参照。
type Forecaster interface {
	Forecast(code stock.StockCode, currentPrice float64, prices []float64) (Forecast, error)
}
