package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/linkr/shortener-api/internal/cache"
	"github.com/linkr/shortener-api/internal/handler"
	"github.com/linkr/shortener-api/internal/publisher"
	"github.com/linkr/shortener-api/internal/repo"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dbURL := mustEnv("DATABASE_URL", log)
	redisAddr := mustEnv("REDIS_URL", log)
	amqpURL := envOr("AMQP_URL", "amqp://guest:guest@localhost:5672/")
	host := envOr("HOST", "0.0.0.0")
	port := envOr("PORT", "8080")
	cacheTTL := parseDuration(envOr("CACHE_TTL", "24h"), log)

	ctx := context.Background()

	pgRepo, err := repo.NewPostgresRepo(ctx, dbURL)
	if err != nil {
		log.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	if err := pgRepo.Migrate(ctx); err != nil {
		log.Error("migration failed", "error", err)
		os.Exit(1)
	}

	redisCache := cache.NewRedisCache(redisAddr)

	pub := publisher.NewAMQPPublisher(amqpURL, log)
	pub.Connect()

	cfg := handler.Config{
		Host:     host,
		Port:     port,
		CacheTTL: cacheTTL,
	}

	router := handler.NewRouter(cfg, pgRepo, redisCache, pgRepo.Ping, redisCache.Ping, pub, pub.IsAlive, log)

	addr := host + ":" + port
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		log.Info("starting server", "addr", addr)
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
	if err := pub.Close(); err != nil {
		log.Warn("publisher close error", "error", err)
	}
	log.Info("shutdown complete")
}

func mustEnv(key string, log *slog.Logger) string {
	v := os.Getenv(key)
	if v == "" {
		log.Error("required environment variable not set", "key", key)
		os.Exit(1)
	}
	return v
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseDuration(s string, log *slog.Logger) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Error("invalid CACHE_TTL, using default 24h", "value", s, "error", err)
		return 24 * time.Hour
	}
	return d
}
