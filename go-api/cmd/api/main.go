package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/stock-anomaly-detection/go-api/internal/domain/anomaly"
	"github.com/stock-anomaly-detection/go-api/internal/infrastructure/persistence"
	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stock-anomaly-detection/go-api/internal/interface/handler"
	"github.com/stock-anomaly-detection/go-api/internal/usecase"
	"github.com/stock-anomaly-detection/go-api/migrations"
)

const watchlistRefreshInterval = 5 * time.Minute

// startupBackfillInterval は起動時バックフィルで銘柄間に挟むウェイト。
// Yahoo Finance へ一斉にリクエストを投げないための間隔。
const startupBackfillInterval = 2 * time.Second

// directionModelRetrainDefaultInterval はAI方向分類器の再学習をどれくらいの
// 間隔で走らせるかのデフォルト値。DIRECTION_MODEL_RETRAIN_INTERVAL で上書き可能。
const directionModelRetrainDefaultInterval = 24 * time.Hour

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	directionModelRetrainInterval := directionModelRetrainDefaultInterval
	if v := os.Getenv("DIRECTION_MODEL_RETRAIN_INTERVAL"); v != "" {
		var err error
		directionModelRetrainInterval, err = time.ParseDuration(v)
		if err != nil {
			log.Fatalf("invalid DIRECTION_MODEL_RETRAIN_INTERVAL: %v", err)
		}
	}

	databaseURL := mustEnv("DATABASE_URL")
	if err := persistence.RunMigrations(databaseURL, migrations.FS); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}
	pool, err := persistence.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	priceRepo := persistence.NewPgPriceRepository(pool)
	priceFetcher := gateway.NewYahooFinanceClient()
	newsClient := gateway.NewYanoshinTDnetClient()
	pythonEngineClient := gateway.NewPythonEngineClient(pythonEngineURL)
	claudeClient := gateway.NewClaudeClient(anthropicAPIKey, claudeModel)
	sentimentScorer, err := newSentimentScorer(os.Getenv("SENTIMENT_SCORER"), anthropicAPIKey, claudeModel)
	if err != nil {
		log.Fatalf("invalid SENTIMENT_SCORER: %v", err)
	}
	slackClient := gateway.NewSlackClient(slackWebhookURL)
	notificationRepo := persistence.NewPgNotificationRepository(pool)
	notifyUsecase := usecase.NewAnalyzeAndNotifyUsecase(newsClient, pythonEngineClient, claudeClient, slackClient, notificationRepo)
	detector := anomaly.NewDetectionService()
	monitor := usecase.NewMonitorUsecase(priceFetcher, priceRepo, detector, threshold, notifyUsecase)
	backfillUsecase := usecase.NewBackfillPriceHistoryUsecase(priceFetcher, priceRepo)

	userRepo := persistence.NewPgUserRepository(pool)
	watchlistRepo := persistence.NewPgWatchlistRepository(pool)
	hasher := gateway.NewBcryptHasher()
	tokenService := gateway.NewJWTTokenService(jwtSecret)

	registerUsecase := usecase.NewRegisterUserUsecase(userRepo, hasher)
	loginUsecase := usecase.NewLoginUserUsecase(userRepo, hasher, tokenService)
	watchlistUsecase := usecase.NewManageWatchlistUsecase(watchlistRepo, backfillUsecase, priceFetcher)
	chartUsecase := usecase.NewGetStockChartUsecase(watchlistRepo, priceRepo, detector, pythonEngineClient, notificationRepo, threshold)
	directionModelUsecase := usecase.NewTrainDirectionModelUsecase(watchlistRepo, priceRepo, pythonEngineClient)
	sentimentRepo := persistence.NewPgSentimentRepository(pool)
	newsSentimentUsecase := usecase.NewGetNewsSentimentUsecase(watchlistRepo, priceRepo, newsClient, sentimentScorer, sentimentRepo, detector, usecase.DefaultNewsSentimentConfig())

	authHandler := handler.NewAuthHandler(registerUsecase, loginUsecase)
	watchlistHandler := handler.NewWatchlistHandler(watchlistUsecase)
	stockHandler := handler.NewStockHandler(chartUsecase)
	newsSentimentHandler := handler.NewNewsSentimentHandler(newsSentimentUsecase)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.Health)
	mux.HandleFunc("POST /auth/register", authHandler.Register)
	mux.HandleFunc("POST /auth/login", authHandler.Login)
	mux.HandleFunc("GET /watchlist", handler.RequireAuth(tokenService, watchlistHandler.List))
	mux.HandleFunc("POST /watchlist", handler.RequireAuth(tokenService, watchlistHandler.Add))
	mux.HandleFunc("DELETE /watchlist/{id}", handler.RequireAuth(tokenService, watchlistHandler.Remove))
	mux.HandleFunc("PATCH /watchlist/{id}", handler.RequireAuth(tokenService, watchlistHandler.UpdateThreshold))
	mux.HandleFunc("GET /stocks/{code}/chart", handler.RequireAuth(tokenService, stockHandler.Chart))
	mux.HandleFunc("GET /stocks/{code}/news-sentiment", handler.RequireAuth(tokenService, newsSentimentHandler.NewsSentiment))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		log.Printf("http server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	// 起動時バックフィル: daily_prices に十分な履歴（backfillDays件、マージン込み）が
	// 無い銘柄をYahooから取得して埋める。2026-09-24以前の旧Redisキャッシュには日付が
	// 保存されていなかったため移行できず、既存銘柄はここで取り直す。
	// この閾値はhistorySize（30件）ではなくbackfillDays（1200件、マージン込みで1170件）
	// 基準のため、この変更をデプロイした直後の最初の再起動では、既存watchlist銘柄の
	// ほとんど（旧backfillDays=500時代にバックフィル済みのもの）が再取得対象になり
	// ノーオペレーションにはならない。各銘柄が1200件（マージン込み）まで到達した
	// 以降の再起動でようやくDB参照1回でスキップされる、実質的なノーオペレーションに
	// 安定する。
	// HTTPサーバーと監視ループを待たせないよう goroutine で回す。
	go func() {
		codes, err := watchlistRepo.FindAllStockCodes(ctx)
		if err != nil {
			log.Printf("ERROR fetch watchlist codes for startup backfill: %v", err)
			return
		}
		log.Printf("startup backfill: checking %d stocks", len(codes))
		backfillUsecase.RunAll(ctx, codes, startupBackfillInterval)
		log.Println("startup backfill finished")
	}()

	// AI方向分類器の再学習を directionModelRetrainInterval 間隔で回す。
	// HTTPサーバーと監視ループを待たせないよう goroutine で回す。
	go func() {
		directionModelUsecase.RunPeriodically(ctx, directionModelRetrainInterval)
	}()

	log.Printf("monitoring watchlist stocks (threshold=%.1fσ, poll=%02d:%02d JST, refresh=%s)",
		threshold, pollHour, pollMinute, watchlistRefreshInterval)
	monitor.RunWithDynamicWatchlist(ctx, watchlistRepo, pollHour, pollMinute, watchlistRefreshInterval)
	log.Println("monitoring stopped")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("ERROR http server shutdown: %v", err)
	}
	newsSentimentUsecase.Wait()
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
