package cache_test

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisPriceCache_PushAndGetHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL not set")
	}

	opt, err := redis.ParseURL(redisURL)
	require.NoError(t, err)
	client := redis.NewClient(opt)
	defer client.Close()

	ctx := context.Background()
	c := cache.NewRedisPriceCache(client)
	code, _ := stock.NewStockCode("7203")
	key := "price:7203"

	// テスト前にクリア
	client.Del(ctx, key)

	// Push 3件 → GetHistory(3) で古い順に3件返る
	require.NoError(t, c.Push(code, 3250.0))
	require.NoError(t, c.Push(code, 3260.0))
	require.NoError(t, c.Push(code, 3270.0))

	prices, err := c.GetHistory(code, 3)
	require.NoError(t, err)
	assert.Len(t, prices, 3)
	assert.Equal(t, stock.Price(3250.0), prices[0])
	assert.Equal(t, stock.Price(3260.0), prices[1])
	assert.Equal(t, stock.Price(3270.0), prices[2])
}

func TestRedisPriceCache_LastDate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL not set")
	}

	opt, err := redis.ParseURL(redisURL)
	require.NoError(t, err)
	client := redis.NewClient(opt)
	defer client.Close()

	ctx := context.Background()
	c := cache.NewRedisPriceCache(client)
	code, _ := stock.NewStockCode("7203")
	client.Del(ctx, "lastdate:7203")

	// 未設定なら空文字
	d, err := c.LastDate(code)
	require.NoError(t, err)
	assert.Equal(t, "", d)

	// セットして読み戻す
	require.NoError(t, c.SetLastDate(code, "2026-07-07"))
	d, err = c.LastDate(code)
	require.NoError(t, err)
	assert.Equal(t, "2026-07-07", d)
}

func TestRedisPriceCache_MaxHistory30(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL not set")
	}

	opt, err := redis.ParseURL(redisURL)
	require.NoError(t, err)
	client := redis.NewClient(opt)
	defer client.Close()

	ctx := context.Background()
	c := cache.NewRedisPriceCache(client)
	code, _ := stock.NewStockCode("9984")
	client.Del(ctx, "price:9984")

	// 35件push → 最新30件のみ残る
	for i := 0; i < 35; i++ {
		require.NoError(t, c.Push(code, stock.Price(float64(1000+i))))
	}

	prices, err := c.GetHistory(code, 30)
	require.NoError(t, err)
	assert.Len(t, prices, 30)
	assert.Equal(t, stock.Price(1005.0), prices[0]) // 35-30=5番目から始まる
	assert.Equal(t, stock.Price(1034.0), prices[29])
}
