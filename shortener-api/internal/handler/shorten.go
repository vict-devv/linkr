package handler

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"

	"github.com/linkr/shortener-api/internal/cache"
	"github.com/linkr/shortener-api/internal/repo"
)

const (
	base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	codeLen        = 6
	maxURLLen      = 2048
	maxRetries     = 3
)

func generateCode() (string, error) {
	b := make([]byte, codeLen)
	alphabetLen := big.NewInt(int64(len(base62Alphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, alphabetLen)
		if err != nil {
			return "", err
		}
		b[i] = base62Alphabet[n.Int64()]
	}
	return string(b), nil
}

func shortenHandler(cfg Config, r repo.URLRepository, c cache.URLCache, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or invalid url field"})
			return
		}

		if len(body.URL) > maxURLLen {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url exceeds maximum length"})
			return
		}

		parsed, err := url.ParseRequestURI(body.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url must be a valid http or https URL"})
			return
		}

		ctx := req.Context()
		var code string
		for i := range maxRetries {
			code, err = generateCode()
			if err != nil {
				log.Error("failed to generate code", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
				return
			}
			err = r.Save(ctx, body.URL, code)
			if err == nil {
				break
			}
			if repo.IsUniqueViolation(err) {
				if i == maxRetries-1 {
					log.Error("code collision after max retries", "error", err)
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
					return
				}
				continue
			}
			log.Error("failed to save url", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}

		if delErr := c.Delete(ctx, code); delErr != nil {
			log.Error("failed to invalidate cache", "code", code, "error", delErr)
		}

		host := req.Host
		if host == "" {
			host = fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"code":      code,
			"short_url": fmt.Sprintf("http://%s/%s", host, code),
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
