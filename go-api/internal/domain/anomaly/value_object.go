package anomaly

type ZScore float64

func (z ZScore) IsAnomaly(threshold float64) bool {
	v := float64(z)
	if v < 0 {
		v = -v
	}
	return v >= threshold
}
