package cache

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
)

const maxHistory = 30

type RedisPriceCache struct {
	client *redis.Client
}

func NewRedisPriceCache(client *redis.Client) *RedisPriceCache {
	return &RedisPriceCache{client: client}
}

func (c *RedisPriceCache) Push(code stock.StockCode, price stock.Price) error {
	ctx := context.Background()
	key := fmt.Sprintf("price:%s", code)
	pipe := c.client.Pipeline()
	pipe.RPush(ctx, key, strconv.FormatFloat(float64(price), 'f', -1, 64))
	pipe.LTrim(ctx, key, -maxHistory, -1)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RedisPriceCache) GetHistory(code stock.StockCode, n int) ([]stock.Price, error) {
	ctx := context.Background()
	key := fmt.Sprintf("price:%s", code)
	vals, err := c.client.LRange(ctx, key, -int64(n), -1).Result()
	if err != nil {
		return nil, err
	}
	prices := make([]stock.Price, 0, len(vals))
	for _, v := range vals {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("parse price %q: %w", v, err)
		}
		prices = append(prices, stock.Price(f))
	}
	return prices, nil
}
