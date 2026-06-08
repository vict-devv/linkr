package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/linkr/shortener-api/internal/cache"
	"github.com/linkr/shortener-api/internal/repo"
)

func redirectHandler(r repo.URLRepository, c cache.URLCache, ttl time.Duration, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		code := req.PathValue("code")
		ctx := req.Context()

		longURL, err := c.Get(ctx, code)
		if err == nil {
			log.Debug("cache hit", "code", code)
			http.Redirect(w, req, longURL, http.StatusFound)
			return
		}
		if !errors.Is(err, cache.ErrNotFound) {
			log.Error("cache get failed", "code", code, "error", err)
		}

		longURL, err = r.Find(ctx, code)
		if errors.Is(err, repo.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if err != nil {
			log.Error("db find failed", "code", code, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}

		log.Debug("cache miss", "code", code)
		if setErr := c.Set(ctx, code, longURL, ttl); setErr != nil {
			log.Error("cache set failed", "code", code, "error", setErr)
		}

		http.Redirect(w, req, longURL, http.StatusFound)
	}
}
