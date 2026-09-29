package directionmodel

import (
	"context"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

// PriceSeries は学習用に日付付きの終値系列を1銘柄分まとめたもの。
type PriceSeries struct {
	StockCode stock.StockCode
	Quotes    []stock.Quote
}

// TrainingResult はwalk-forward検証の結果。観測性のためgo-api側のログに残す。
type TrainingResult struct {
	Adopted                bool
	HitRate                *float64
	BaselineHitRate        *float64
	PValue                 *float64
	IndependentSampleCount *int
}

// Trainer はプールした全銘柄の価格履歴から方向分類器を学習・walk-forward検証する。
// 実装は python-engine の POST /model/train を呼ぶ
// （docs/superpowers/specs/2026-09-29-chart-direction-classifier-design.md 参照）。
type Trainer interface {
	Train(ctx context.Context, series []PriceSeries) (TrainingResult, error)
}
