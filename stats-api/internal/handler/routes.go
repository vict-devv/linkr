package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/linkr/stats-api/internal/middleware"
	"github.com/linkr/stats-api/internal/repo"
)

type Config struct {
	Port              string
	StatsWindowDays   int
	TopReferrersLimit int
	APIKey            string
}

func NewRouter(cfg Config, r repo.StatsRepository, mongoPing func(context.Context) error, log *slog.Logger) http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.Logging(log))
	router.With(middleware.APIKeyAuth(cfg.APIKey)).Get("/stats/{code}", statsHandler(cfg, r, log))
	router.Get("/health", healthHandler(mongoPing, log))
	return router
}
