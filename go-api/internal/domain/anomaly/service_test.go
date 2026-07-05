package anomaly_test

import (
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectionService_Calculate(t *testing.T) {
	svc := anomaly.NewDetectionService()

	// 29件の交互データ（90, 110, 90, 110...）でhistoryを作る
	// mean≈99.65, stddev≈10
	makeAlternating := func(n int) []float64 {
		p := make([]float64, n)
		for i := range p {
			if i%2 == 0 {
				p[i] = 90
			} else {
				p[i] = 110
			}
		}
		return p
	}

	tests := []struct {
		name        string
		prices      []float64
		wantAnomaly bool
		wantZero    bool
		wantErr     bool
	}{
		{
			name:    "insufficient data returns error",
			prices:  []float64{100},
			wantErr: true,
		},
		{
			name:     "zero stddev returns 0",
			prices:   append(make([]float64, 29), 100), // 30 zeros + last 100 → all same
			wantZero: true,
		},
		{
			name:        "large spike above threshold is anomaly",
			prices:      append(makeAlternating(29), 130), // current=130, z≈3.0
			wantAnomaly: true,
		},
		{
			name:        "small variation is not anomaly",
			prices:      append(makeAlternating(29), 111), // current=111, z≈1.1
			wantAnomaly: false,
		},
		{
			name:        "large drop below mean is anomaly",
			prices:      append(makeAlternating(29), 70), // current=70, z≈-3.0
			wantAnomaly: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.Calculate(tt.prices)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantZero {
				assert.Equal(t, anomaly.ZScore(0), got)
				return
			}
			assert.Equal(t, tt.wantAnomaly, got.IsAnomaly(2.5),
				"z=%.3f, threshold=2.5", got)
		})
	}
}
