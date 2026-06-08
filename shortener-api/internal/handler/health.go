package handler

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

func healthHandler(dbPing func(context.Context) error, cachePing func(context.Context) error, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()

		var (
			mu         sync.Mutex
			pgStatus   = "up"
			rdisStatus = "up"
			wg         sync.WaitGroup
		)

		probe := func(ping func(context.Context) error, dest *string) {
			defer wg.Done()
			if err := ping(ctx); err != nil {
				log.Error("health probe failed", "error", err)
				mu.Lock()
				*dest = "down"
				mu.Unlock()
			}
		}

		wg.Add(2)
		go probe(dbPing, &pgStatus)
		go probe(cachePing, &rdisStatus)
		wg.Wait()

		status := "ok"
		httpStatus := http.StatusOK
		if pgStatus == "down" || rdisStatus == "down" {
			status = "degraded"
			httpStatus = http.StatusServiceUnavailable
		}

		writeJSON(w, httpStatus, map[string]string{
			"status":   status,
			"postgres": pgStatus,
			"redis":    rdisStatus,
		})
	}
}
