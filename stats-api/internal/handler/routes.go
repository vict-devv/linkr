package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/linkr/stats-api/internal/middleware"
	"github.com/linkr/stats-api/internal/repo"
)

type Config struct {
	Port              string
	StatsWindowDays   int
	TopReferrersLimit int
}

func NewRouter(cfg Config, r repo.StatsRepository, mongoPing func(context.Context) error, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats/{code}", statsHandler(cfg, r, log))
	mux.HandleFunc("GET /health", healthHandler(mongoPing, log))
	return middleware.Logging(log)(mux)
}
