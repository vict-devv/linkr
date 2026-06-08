package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/linkr/shortener-api/internal/cache"
	"github.com/linkr/shortener-api/internal/handler"
	"github.com/linkr/shortener-api/internal/repo"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dbURL := mustEnv("DATABASE_URL", log)
	redisAddr := mustEnv("REDIS_URL", log)
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

	cfg := handler.Config{
		Host:     host,
		Port:     port,
		CacheTTL: cacheTTL,
	}

	router := handler.NewRouter(cfg, pgRepo, redisCache, pgRepo.Ping, redisCache.Ping, log)

	addr := host + ":" + port
	log.Info("starting server", "addr", addr)
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
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
