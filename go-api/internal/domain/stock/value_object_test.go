package stock_test

import (
	"testing"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStockCode(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		wantErr bool
	}{
		{"valid 4-digit code", "7203", false},
		{"empty code returns error", "", true},
		{"5-digit code returns error", "72030", true},
		{"non-numeric code returns error", "ABCD", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stock.NewStockCode(tt.code)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, stock.StockCode(tt.code), got)
			assert.Equal(t, tt.code, got.String())
		})
	}
}
