package forecast

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

type Point struct {
	Step    int
	Center  float64
	Upper68 float64
	Lower68 float64
	Upper95 float64
	Lower95 float64
}

// DirectionModel はPhase 2のAI方向分類器の状態。Adoptedがfalseの場合、
// 他のフィールドはnilになりうる（未学習、またはwalk-forwardで既存の
// ドリフト中心線に対する優位性を示せなかった状態）。
// docs/superpowers/specs/2026-09-29-chart-direction-classifier-design.md 参照。
type DirectionModel struct {
	Adopted                bool
	PredictedDirection     *string
	HitRate                *float64
	BaselineHitRate        *float64
	PValue                 *float64
	IndependentSampleCount *int
	TrainedAt              *string
}

type Forecast struct {
	Horizon        int
	Points         []Point
	DirectionModel DirectionModel
}

// Forecaster は将来の価格レンジ（ドリフト＋ボラティリティ帯）を計算する。
// 点予測ではなく方向とレンジ帯を返す方針は
// docs/superpowers/specs/2026-09-24-stock-chart-forecast-design.md 参照。
type Forecaster interface {
	Forecast(ctx context.Context, code stock.StockCode, currentPrice float64, prices []float64) (Forecast, error)
}
