package anomaly

import (
	"errors"
	"math"
)

type DetectionService struct{}

func NewDetectionService() *DetectionService {
	return &DetectionService{}
}

// Calculate は prices の末尾を現在値、先頭〜末尾-1 を履歴として Z-score を計算する。
// 履歴の標準偏差が0の場合は 0 を返す（同一価格が続く状態）。
func (s *DetectionService) Calculate(prices []float64) (ZScore, error) {
	if len(prices) < 2 {
		return 0, errors.New("need at least 2 price data points")
	}
	current := prices[len(prices)-1]
	history := prices[:len(prices)-1]

	mean := calcMean(history)
	stddev := calcStddev(history, mean)
	if stddev == 0 {
		return 0, nil
	}
	return ZScore((current - mean) / stddev), nil
}

func calcMean(prices []float64) float64 {
	sum := 0.0
	for _, p := range prices {
		sum += p
	}
	return sum / float64(len(prices))
}

func calcStddev(prices []float64, mean float64) float64 {
	variance := 0.0
	for _, p := range prices {
		diff := p - mean
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(len(prices)))
}
