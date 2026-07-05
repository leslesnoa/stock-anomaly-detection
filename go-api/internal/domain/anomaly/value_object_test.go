package anomaly_test

import (
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stretchr/testify/assert"
)

func TestZScore_IsAnomaly(t *testing.T) {
	tests := []struct {
		name      string
		z         anomaly.ZScore
		threshold float64
		want      bool
	}{
		{"above threshold positive", 2.6, 2.5, true},
		{"below threshold positive", 2.4, 2.5, false},
		{"above threshold negative", -2.6, 2.5, true},
		{"below threshold negative", -2.4, 2.5, false},
		{"exactly at threshold", 2.5, 2.5, true},
		{"zero", 0, 2.5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.z.IsAnomaly(tt.threshold))
		})
	}
}
