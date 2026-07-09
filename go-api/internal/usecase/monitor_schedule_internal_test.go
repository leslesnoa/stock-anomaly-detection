package usecase

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNextPollTime(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)

	cases := []struct {
		name      string
		now       time.Time
		hour, min int
		wantY     int
		wantM     time.Month
		wantD     int
		wantHH    int
		wantMM    int
	}{
		{"実行時刻前は当日", time.Date(2026, 7, 7, 10, 0, 0, 0, jst), 16, 0, 2026, 7, 7, 16, 0},
		{"実行時刻ちょうどは翌日", time.Date(2026, 7, 7, 16, 0, 0, 0, jst), 16, 0, 2026, 7, 8, 16, 0},
		{"実行時刻後は翌日", time.Date(2026, 7, 7, 18, 0, 0, 0, jst), 16, 0, 2026, 7, 8, 16, 0},
		{"深夜跨ぎ", time.Date(2026, 7, 7, 23, 30, 0, 0, jst), 0, 15, 2026, 7, 8, 0, 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextPollTime(tc.now, tc.hour, tc.min).In(jst)
			assert.Equal(t, tc.wantY, got.Year())
			assert.Equal(t, tc.wantM, got.Month())
			assert.Equal(t, tc.wantD, got.Day())
			assert.Equal(t, tc.wantHH, got.Hour())
			assert.Equal(t, tc.wantMM, got.Minute())
		})
	}
}
