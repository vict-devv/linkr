package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linkr/stats-api/internal/handler"
	"github.com/linkr/stats-api/internal/repo"
)

func newNopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- fake ---

type fakeRepo struct {
	totalClicks int64
	overTime    []repo.ClicksOverTime
	referrers   []repo.TopReferrer
	err         error
}

func (f *fakeRepo) TotalClicks(_ context.Context, _ string) (int64, error) {
	return f.totalClicks, f.err
}

func (f *fakeRepo) ClicksOverTime(_ context.Context, _ string, _ int) ([]repo.ClicksOverTime, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.overTime, nil
}

func (f *fakeRepo) TopReferrers(_ context.Context, _ string, _ int) ([]repo.TopReferrer, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.referrers, nil
}

// --- helpers ---

const windowDays = 30

func newRouter(r *fakeRepo) http.Handler {
	return newRouterWithPing(r, func(_ context.Context) error { return nil })
}

func newRouterWithPing(r *fakeRepo, ping func(context.Context) error) http.Handler {
	cfg := handler.Config{
		Port:              "8080",
		StatsWindowDays:   windowDays,
		TopReferrersLimit: 10,
		APIKey:            "testkey",
	}
	return handler.NewRouter(cfg, r, ping, newNopLogger())
}

func get(router http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func getWith(router http.Handler, path, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// --- tests ---

func TestStats_HappyPath(t *testing.T) {
	r := &fakeRepo{
		totalClicks: 42,
		overTime: []repo.ClicksOverTime{
			{Date: "2026-05-11", Count: 10},
			{Date: "2026-05-12", Count: 32},
		},
		referrers: []repo.TopReferrer{
			{Referrer: "https://twitter.com", Count: 20},
			{Referrer: "", Count: 22},
		},
	}
	router := newRouter(r)

	w := getWith(router, "/stats/abc123", "Bearer testkey")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}

	var resp struct {
		Code           string                `json:"code"`
		TotalClicks    int64                 `json:"total_clicks"`
		ClicksOverTime []repo.ClicksOverTime `json:"clicks_over_time"`
		TopReferrers   []repo.TopReferrer    `json:"top_referrers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal("invalid JSON:", err)
	}

	if resp.Code != "abc123" {
		t.Errorf("expected code abc123, got %q", resp.Code)
	}
	if resp.TotalClicks != 42 {
		t.Errorf("expected total_clicks 42, got %d", resp.TotalClicks)
	}
	if len(resp.ClicksOverTime) != windowDays {
		t.Errorf("expected %d entries in clicks_over_time (zero-filled), got %d", windowDays, len(resp.ClicksOverTime))
	}
	if len(resp.TopReferrers) != 2 {
		t.Errorf("expected 2 top referrers, got %d", len(resp.TopReferrers))
	}
}

func TestStats_ZeroFillContinuous(t *testing.T) {
	r := &fakeRepo{
		totalClicks: 5,
		overTime:    []repo.ClicksOverTime{{Date: "2026-05-15", Count: 5}},
	}
	router := newRouter(r)

	w := getWith(router, "/stats/abc123", "Bearer testkey")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		ClicksOverTime []repo.ClicksOverTime `json:"clicks_over_time"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.ClicksOverTime) != windowDays {
		t.Fatalf("expected %d days, got %d", windowDays, len(resp.ClicksOverTime))
	}
	for i := 1; i < len(resp.ClicksOverTime); i++ {
		if resp.ClicksOverTime[i].Date <= resp.ClicksOverTime[i-1].Date {
			t.Errorf("dates not ascending: %s >= %s", resp.ClicksOverTime[i].Date, resp.ClicksOverTime[i-1].Date)
		}
	}
}

func TestStats_CodeNotFound(t *testing.T) {
	r := &fakeRepo{totalClicks: 0}
	router := newRouter(r)

	w := getWith(router, "/stats/nosuchcode", "Bearer testkey")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "code not found" {
		t.Errorf("expected 'code not found', got %q", resp["error"])
	}
}

func TestStats_RepoError(t *testing.T) {
	sentinel := errors.New("mongo: connection refused")
	r := &fakeRepo{err: sentinel}
	router := newRouter(r)

	w := getWith(router, "/stats/abc123", "Bearer testkey")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, sentinel.Error()) {
		t.Errorf("raw error message leaked to client: %s", body)
	}
	var resp map[string]string
	_ = json.NewDecoder(strings.NewReader(body)).Decode(&resp)
	if resp["error"] == "" {
		t.Error("expected non-empty error field")
	}
}

func TestHealth_MongoUp(t *testing.T) {
	r := &fakeRepo{}
	router := newRouterWithPing(r, func(_ context.Context) error { return nil })

	w := get(router, "/health")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %q", resp["status"])
	}
	if resp["mongo"] != "up" {
		t.Errorf("expected mongo up, got %q", resp["mongo"])
	}
}

func TestHealth_MongoDown(t *testing.T) {
	r := &fakeRepo{}
	router := newRouterWithPing(r, func(_ context.Context) error { return errors.New("mongo unreachable") })

	w := get(router, "/health")

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "degraded" {
		t.Errorf("expected status degraded, got %q", resp["status"])
	}
	if resp["mongo"] != "down" {
		t.Errorf("expected mongo down, got %q", resp["mongo"])
	}
}

func TestAuth_Stats(t *testing.T) {
	r := &fakeRepo{totalClicks: 1}
	router := newRouter(r)

	w := getWith(router, "/stats/abc123", "Bearer testkey")
	if w.Code != http.StatusOK {
		t.Errorf("valid key: expected 200, got %d", w.Code)
	}

	w = get(router, "/stats/abc123")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no auth header: expected 401, got %d", w.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "unauthorized" {
		t.Errorf("no auth header: expected error=unauthorized, got %q", resp["error"])
	}

	w = getWith(router, "/stats/abc123", "Token testkey")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong scheme: expected 401, got %d", w.Code)
	}

	w = getWith(router, "/stats/abc123", "Bearer wrongkey")
	if w.Code != http.StatusForbidden {
		t.Errorf("wrong key: expected 403, got %d", w.Code)
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "forbidden" {
		t.Errorf("wrong key: expected error=forbidden, got %q", resp["error"])
	}
}

func TestAuth_PublicRoutes(t *testing.T) {
	r := &fakeRepo{}
	router := newRouter(r)

	w := get(router, "/health")
	if w.Code != http.StatusOK {
		t.Errorf("/health: expected 200 without auth, got %d", w.Code)
	}
}

func TestLogging_FieldsPresent(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	r := &fakeRepo{totalClicks: 1}
	cfg := handler.Config{Port: "8080", StatsWindowDays: windowDays, TopReferrersLimit: 10, APIKey: "testkey"}
	router := handler.NewRouter(cfg, r, func(_ context.Context) error { return nil }, log)

	req := httptest.NewRequest(http.MethodGet, "/stats/abc123", nil)
	req.Header.Set("Authorization", "Bearer testkey")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	logged := buf.String()
	for _, key := range []string{"method", "path", "status", "latency_ms"} {
		if !strings.Contains(logged, key) {
			t.Errorf("expected log output to contain %q\nlog: %s", key, logged)
		}
	}
}
