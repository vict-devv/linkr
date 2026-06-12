package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/linkr/shortener-api/internal/cache"
	"github.com/linkr/shortener-api/internal/middleware"
	"github.com/linkr/shortener-api/internal/publisher"
	"github.com/linkr/shortener-api/internal/repo"
)

type Config struct {
	Host     string
	Port     string
	CacheTTL time.Duration
	APIKey   string
}

func NewRouter(cfg Config, r repo.URLRepository, c cache.URLCache, dbPing func(context.Context) error, cachePing func(context.Context) error, pub publisher.EventPublisher, amqpAlive func() bool, log *slog.Logger) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.Logging(log))

	router.Get("/health", healthHandler(dbPing, cachePing, amqpAlive, log))
	router.With(middleware.APIKeyAuth(cfg.APIKey)).Post("/shorten", shortenHandler(cfg, r, c, log))
	router.Get("/{code}", redirectHandler(r, c, cfg.CacheTTL, pub, log))

	return router
}
