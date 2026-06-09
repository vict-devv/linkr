package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/linkr/stats-api/internal/handler"
	"github.com/linkr/stats-api/internal/repo"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	mongoURI := envOr("MONGO_URI", "mongodb://localhost:27017")
	mongoDB := envOr("MONGO_DB", "analytics")
	mongoColl := envOr("MONGO_COLLECTION", "click_events")
	port := envOr("PORT", "8080")
	windowDays := parseInt(envOr("STATS_WINDOW_DAYS", "30"), "STATS_WINDOW_DAYS", log)
	topLimit := parseInt(envOr("TOP_REFERRERS_LIMIT", "10"), "TOP_REFERRERS_LIMIT", log)

	ctx := context.Background()

	mongoRepo, err := repo.NewMongoStatsRepo(ctx, mongoURI, mongoDB, mongoColl, log)
	if err != nil {
		log.Error("failed to connect to mongodb", "error", err)
		os.Exit(1)
	}

	cfg := handler.Config{
		Port:              port,
		StatsWindowDays:   windowDays,
		TopReferrersLimit: topLimit,
	}

	router := handler.NewRouter(cfg, mongoRepo, mongoRepo.Ping, log)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		log.Info("starting server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-sigCtx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("http server shutdown error", "error", err)
	}
	if err := mongoRepo.Close(shutdownCtx); err != nil {
		log.Warn("mongo close error", "error", err)
	}
	log.Info("shutdown complete")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseInt(s, name string, log *slog.Logger) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		log.Error("invalid environment variable", "key", name, "value", s)
		os.Exit(1)
	}
	return n
}
