package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/linkr/shortener-api/internal/cache"
	"github.com/linkr/shortener-api/internal/model"
	"github.com/linkr/shortener-api/internal/publisher"
	"github.com/linkr/shortener-api/internal/repo"
)

func redirectHandler(r repo.URLRepository, c cache.URLCache, ttl time.Duration, pub publisher.EventPublisher, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		code := chi.URLParam(req, "code")
		ctx := req.Context()

		longURL, err := c.Get(ctx, code)
		if err == nil {
			log.Debug("cache hit", "code", code)
			fireRedirectEvent(pub, log, code, req)
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

		fireRedirectEvent(pub, log, code, req)
		http.Redirect(w, req, longURL, http.StatusFound)
	}
}

func fireRedirectEvent(pub publisher.EventPublisher, log *slog.Logger, code string, req *http.Request) {
	event := model.RedirectEvent{
		Code:      code,
		Timestamp: time.Now().UTC(),
		Referrer:  req.Referer(),
		IPHash:    hashIP(req.RemoteAddr),
	}
	go func() {
		if err := pub.Publish(context.Background(), event); err != nil {
			log.Warn("failed to publish redirect event", "code", code, "error", err)
		}
	}()
}

func hashIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		ip = remoteAddr
	}
	sum := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(sum[:])
}
