package usecase

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/domain/watchlist"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodesChanged(t *testing.T) {
	code1, _ := stock.NewStockCode("7203")
	code2, _ := stock.NewStockCode("9984")

	cases := []struct {
		name string
		a, b []stock.StockCode
		want bool
	}{
		{"同じ順序", []stock.StockCode{code1, code2}, []stock.StockCode{code1, code2}, false},
		{"順序違いのみ", []stock.StockCode{code1, code2}, []stock.StockCode{code2, code1}, false},
		{"件数が違う", []stock.StockCode{code1}, []stock.StockCode{code1, code2}, true},
		{"内容が違う", []stock.StockCode{code1}, []stock.StockCode{code2}, true},
		{"両方空", []stock.StockCode{}, []stock.StockCode{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, codesChanged(tc.a, tc.b))
		})
	}
}

type fakeWatchlistRepo struct {
	mu      sync.Mutex
	seq     [][]stock.StockCode
	calls   int
	errFrom int // 1-indexed; 0 means never error
}

func (f *fakeWatchlistRepo) FindAllStockCodes(ctx context.Context) ([]stock.StockCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.errFrom > 0 && f.calls >= f.errFrom {
		return nil, errors.New("db error")
	}
	idx := f.calls - 1
	if idx >= len(f.seq) {
		idx = len(f.seq) - 1
	}
	return f.seq[idx], nil
}
func (f *fakeWatchlistRepo) FindByUserID(ctx context.Context, userID string) ([]watchlist.Watchlist, error) {
	return nil, nil
}
func (f *fakeWatchlistRepo) Create(ctx context.Context, w watchlist.Watchlist) (watchlist.Watchlist, error) {
	return watchlist.Watchlist{}, nil
}
func (f *fakeWatchlistRepo) Delete(ctx context.Context, id, userID string) error { return nil }
func (f *fakeWatchlistRepo) UpdateThreshold(ctx context.Context, id, userID string, threshold float64) error {
	return nil
}

type startCall struct {
	codes []stock.StockCode
}

func TestRunWithDynamicWatchlist_RestartsOnChange(t *testing.T) {
	code1, _ := stock.NewStockCode("7203")
	code2, _ := stock.NewStockCode("9984")

	repo := &fakeWatchlistRepo{seq: [][]stock.StockCode{
		{code1},        // initial fetch
		{code1},        // 変化なし
		{code1, code2}, // 変化あり -> restart
	}}

	var mu sync.Mutex
	var calls []startCall
	u := &MonitorUsecase{}
	u.startFn = func(ctx context.Context, codes []stock.StockCode, hour, minute int) {
		mu.Lock()
		calls = append(calls, startCall{codes: codes})
		mu.Unlock()
		<-ctx.Done()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	u.RunWithDynamicWatchlist(ctx, repo, 16, 0, 30*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(calls), 2)
	assert.Equal(t, []stock.StockCode{code1}, calls[0].codes)
	assert.Equal(t, []stock.StockCode{code1, code2}, calls[len(calls)-1].codes)
}

func TestRunWithDynamicWatchlist_KeepsListOnFetchError(t *testing.T) {
	code1, _ := stock.NewStockCode("7203")
	repo := &fakeWatchlistRepo{
		seq:     [][]stock.StockCode{{code1}},
		errFrom: 2, // 2回目以降のfetchは失敗させる
	}

	var mu sync.Mutex
	var calls []startCall
	u := &MonitorUsecase{}
	u.startFn = func(ctx context.Context, codes []stock.StockCode, hour, minute int) {
		mu.Lock()
		calls = append(calls, startCall{codes: codes})
		mu.Unlock()
		<-ctx.Done()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	u.RunWithDynamicWatchlist(ctx, repo, 16, 0, 30*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, calls, 1)
	assert.Equal(t, []stock.StockCode{code1}, calls[0].codes)
}

func TestRunWithDynamicWatchlist_ReturnsOnContextCancel(t *testing.T) {
	repo := &fakeWatchlistRepo{seq: [][]stock.StockCode{{}}}
	u := &MonitorUsecase{}
	u.startFn = func(ctx context.Context, codes []stock.StockCode, hour, minute int) {}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		u.RunWithDynamicWatchlist(ctx, repo, 16, 0, 10*time.Millisecond)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("RunWithDynamicWatchlist did not return after context cancel")
	}
}
