package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

func healthHandler(mongoPing func(context.Context) error, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mongoStatus := "up"
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := mongoPing(pingCtx); err != nil {
			mongoStatus = "down"
			log.WarnContext(r.Context(), "mongo ping failed", "error", err)
		}

		status := "ok"
		code := http.StatusOK
		if mongoStatus == "down" {
			status = "degraded"
			code = http.StatusServiceUnavailable
		}

		writeJSON(w, code, map[string]string{
			"status": status,
			"mongo":  mongoStatus,
		})
	}
}
