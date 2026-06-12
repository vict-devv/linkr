package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func NewHealthServer(port string, amqpAlive func() bool, mongoPing func(context.Context) error, log *slog.Logger) *http.Server {
	r := chi.NewRouter()
	r.Get("/health", healthHandler(amqpAlive, mongoPing, log))
	return &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}
}

func healthHandler(amqpAlive func() bool, mongoPing func(context.Context) error, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		amqpStatus := "up"
		if !amqpAlive() {
			amqpStatus = "down"
		}

		mongoStatus := "up"
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := mongoPing(pingCtx); err != nil {
			mongoStatus = "down"
			log.Warn("mongo ping failed", "error", err)
		}

		status := "ok"
		code := http.StatusOK
		if amqpStatus == "down" || mongoStatus == "down" {
			status = "degraded"
			code = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": status,
			"amqp":   amqpStatus,
			"mongo":  mongoStatus,
		})
	}
}
