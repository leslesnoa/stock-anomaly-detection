package anomaly

import "math"

type ZScore float64

func (z ZScore) IsAnomaly(threshold float64) bool {
	return math.Abs(float64(z)) >= threshold
}
