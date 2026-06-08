package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/linkr/shortener-api/internal/cache"
	"github.com/linkr/shortener-api/internal/middleware"
	"github.com/linkr/shortener-api/internal/repo"
)

type Config struct {
	Host     string
	Port     string
	CacheTTL time.Duration
}

func NewRouter(cfg Config, r repo.URLRepository, c cache.URLCache, dbPing func(context.Context) error, cachePing func(context.Context) error, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler(dbPing, cachePing, log))
	mux.HandleFunc("POST /shorten", shortenHandler(cfg, r, c, log))
	mux.HandleFunc("GET /{code}", redirectHandler(r, c, cfg.CacheTTL, log))

	return middleware.Logging(log)(mux)
}
