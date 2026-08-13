package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
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
	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	redisURL := mustEnv("REDIS_URL")
	jQuantsAPIKey := mustEnv("JQUANTS_API_KEY")
	stockCodesRaw := mustEnv("STOCK_CODES")
	finnhubAPIKey := mustEnv("FINNHUB_API_KEY")
	anthropicAPIKey := mustEnv("ANTHROPIC_API_KEY")
	slackWebhookURL := mustEnv("SLACK_WEBHOOK_URL")
	pythonEngineURL := mustEnv("PYTHON_ENGINE_URL")
	jwtSecret := mustEnv("JWT_SECRET")

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	claudeModel := "claude-opus-5"
	if m := os.Getenv("CLAUDE_MODEL"); m != "" {
		claudeModel = m
	}

	threshold := 2.5
	if t := os.Getenv("ANOMALY_THRESHOLD"); t != "" {
		var err error
		threshold, err = strconv.ParseFloat(t, 64)
		if err != nil {
			log.Fatalf("invalid ANOMALY_THRESHOLD: %v", err)
		}
	}

	pollHour, pollMinute := 16, 0
	if pt := os.Getenv("POLL_TIME"); pt != "" {
		var err error
		pollHour, pollMinute, err = parsePollTime(pt)
		if err != nil {
			log.Fatalf("invalid POLL_TIME: %v", err)
		}
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("invalid REDIS_URL: %v", err)
	}
	redisClient := redis.NewClient(opt)
	defer redisClient.Close()

	databaseURL := mustEnv("DATABASE_URL")
	pool, err := persistence.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	priceCache := cache.NewRedisPriceCache(redisClient)
	priceFetcher := gateway.NewJQuantsClient(jQuantsAPIKey)
	newsClient := gateway.NewFinnhubClient(finnhubAPIKey)
	pythonEngineClient := gateway.NewPythonEngineClient(pythonEngineURL)
	claudeClient := gateway.NewClaudeClient(anthropicAPIKey, claudeModel)
	slackClient := gateway.NewSlackClient(slackWebhookURL)
	notificationRepo := persistence.NewPgNotificationRepository(pool)
	notifyUsecase := usecase.NewAnalyzeAndNotifyUsecase(newsClient, pythonEngineClient, claudeClient, slackClient, notificationRepo)
	detector := anomaly.NewDetectionService()
	monitor := usecase.NewMonitorUsecase(priceFetcher, priceCache, detector, threshold, notifyUsecase)

	userRepo := persistence.NewPgUserRepository(pool)
	watchlistRepo := persistence.NewPgWatchlistRepository(pool)
	hasher := gateway.NewBcryptHasher()
	tokenService := gateway.NewJWTTokenService(jwtSecret)

	registerUsecase := usecase.NewRegisterUserUsecase(userRepo, hasher)
	loginUsecase := usecase.NewLoginUserUsecase(userRepo, hasher, tokenService)
	watchlistUsecase := usecase.NewManageWatchlistUsecase(watchlistRepo)

	authHandler := handler.NewAuthHandler(registerUsecase, loginUsecase)
	watchlistHandler := handler.NewWatchlistHandler(watchlistUsecase)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/register", authHandler.Register)
	mux.HandleFunc("POST /auth/login", authHandler.Login)
	mux.HandleFunc("GET /watchlist", handler.RequireAuth(tokenService, watchlistHandler.List))
	mux.HandleFunc("POST /watchlist", handler.RequireAuth(tokenService, watchlistHandler.Add))
	mux.HandleFunc("DELETE /watchlist/{id}", handler.RequireAuth(tokenService, watchlistHandler.Remove))
	mux.HandleFunc("PATCH /watchlist/{id}", handler.RequireAuth(tokenService, watchlistHandler.UpdateThreshold))

	go func() {
		log.Printf("http server listening on :%s", port)
		if err := http.ListenAndServe(":"+port, mux); err != nil {
			log.Printf("ERROR http server: %v", err)
		}
	}()

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

	log.Printf("monitoring %d stocks (threshold=%.1fσ, poll=%02d:%02d JST)", len(codes), threshold, pollHour, pollMinute)
	monitor.StartMonitoring(ctx, codes, pollHour, pollMinute)
	log.Println("monitoring stopped")
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env %s is not set", key)
	}
	return v
}

// parsePollTime は "HH:MM"（24時間表記）を時・分に分解する。
// %s で余剰入力を捕まえ、"16:0:0" のような不正形式を厳密に弾く。
func parsePollTime(s string) (int, int, error) {
	var hour, minute int
	var rest string
	n, _ := fmt.Sscanf(s, "%d:%d%s", &hour, &minute, &rest)
	if n < 2 || rest != "" {
		return 0, 0, fmt.Errorf("POLL_TIME must be HH:MM, got %q", s)
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("POLL_TIME out of range: %q", s)
	}
	return hour, minute, nil
}
