package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/redis/go-redis/v9"
	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/domain/stock"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/cache"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	redisURL := mustEnv("REDIS_URL")
	jQuantsEmail := mustEnv("JQUANTS_EMAIL")
	jQuantsPassword := mustEnv("JQUANTS_PASSWORD")
	stockCodesRaw := mustEnv("STOCK_CODES")

	threshold := 2.5
	if t := os.Getenv("ANOMALY_THRESHOLD"); t != "" {
		var err error
		threshold, err = strconv.ParseFloat(t, 64)
		if err != nil {
			log.Fatalf("invalid ANOMALY_THRESHOLD: %v", err)
		}
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("invalid REDIS_URL: %v", err)
	}
	redisClient := redis.NewClient(opt)
	defer redisClient.Close()

	databaseURL := mustEnv("DATABASE_URL")
	conn, err := persistence.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer conn.Close(ctx)

	priceCache := cache.NewRedisPriceCache(redisClient)
	priceFetcher := gateway.NewJQuantsClient(jQuantsEmail, jQuantsPassword)
	detector := anomaly.NewDetectionService()
	monitor := usecase.NewMonitorUsecase(priceFetcher, priceCache, detector, threshold)

	var codes []stock.StockCode
	for _, s := range strings.Split(stockCodesRaw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		code, err := stock.NewStockCode(s)
		if err != nil {
			log.Printf("skip invalid stock code %q: %v", s, err)
			continue
		}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		log.Fatal("no valid stock codes in STOCK_CODES")
	}

	log.Printf("monitoring %d stocks (threshold=%.1fσ)", len(codes), threshold)
	monitor.StartMonitoring(ctx, codes)
	log.Println("monitoring stopped")
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env %s is not set", key)
	}
	return v
}
