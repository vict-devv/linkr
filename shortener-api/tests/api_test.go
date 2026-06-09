package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/linkr/shortener-api/internal/cache"
	"github.com/linkr/shortener-api/internal/handler"
	"github.com/linkr/shortener-api/internal/model"
	"github.com/linkr/shortener-api/internal/repo"
)

func newNopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- fakes ---

type fakeRepo struct {
	mu          sync.Mutex
	urls        map[string]string
	findCalled  int
	saveCalled  int
	returnError error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{urls: map[string]string{}} }

func (f *fakeRepo) Save(_ context.Context, longURL, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveCalled++
	if f.returnError != nil {
		return f.returnError
	}
	f.urls[code] = longURL
	return nil
}

func (f *fakeRepo) Find(_ context.Context, code string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.findCalled++
	v, ok := f.urls[code]
	if !ok {
		return "", repo.ErrNotFound
	}
	return v, nil
}

type fakeCache struct {
	mu         sync.Mutex
	data       map[string]string
	getCalled  int
	setCalled  int
	failGet    bool
	failPing   bool
}

func newFakeCache() *fakeCache { return &fakeCache{data: map[string]string{}} }

func (f *fakeCache) Get(_ context.Context, code string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalled++
	if f.failGet {
		return "", errors.New("redis down")
	}
	v, ok := f.data[code]
	if !ok {
		return "", cache.ErrNotFound
	}
	return v, nil
}

func (f *fakeCache) Set(_ context.Context, code string, longURL string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalled++
	f.data[code] = longURL
	return nil
}

func (f *fakeCache) Delete(_ context.Context, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, code)
	return nil
}

func (f *fakeCache) Ping(_ context.Context) error {
	if f.failPing {
		return errors.New("redis down")
	}
	return nil
}

type fakePublisher struct {
	mu          sync.Mutex
	returnError error
	publishedCh chan model.RedirectEvent
}

func newFakePublisher() *fakePublisher {
	return &fakePublisher{publishedCh: make(chan model.RedirectEvent, 1)}
}

func (f *fakePublisher) Publish(_ context.Context, event model.RedirectEvent) error {
	f.mu.Lock()
	err := f.returnError
	f.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case f.publishedCh <- event:
	default:
	}
	return nil
}

func (f *fakePublisher) Close() error { return nil }

// waitEvent waits for one published event with a short timeout.
func (f *fakePublisher) waitEvent(t *testing.T) model.RedirectEvent {
	t.Helper()
	select {
	case e := <-f.publishedCh:
		return e
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for published event")
		return model.RedirectEvent{}
	}
}

// --- helpers ---

func newRouter(r *fakeRepo, c *fakeCache) http.Handler {
	return newRouterFull(r, c, func(_ context.Context) error { return nil }, newFakePublisher(), func() bool { return true })
}

func newRouterWithPings(r *fakeRepo, c *fakeCache, dbPing func(context.Context) error) http.Handler {
	return newRouterFull(r, c, dbPing, newFakePublisher(), func() bool { return true })
}

func newRouterWithPublisher(r *fakeRepo, c *fakeCache, pub *fakePublisher) http.Handler {
	return newRouterFull(r, c, func(_ context.Context) error { return nil }, pub, func() bool { return true })
}

func newRouterWithAMQPAlive(r *fakeRepo, c *fakeCache, amqpAlive func() bool) http.Handler {
	return newRouterFull(r, c, func(_ context.Context) error { return nil }, newFakePublisher(), amqpAlive)
}

func newRouterFull(r *fakeRepo, c *fakeCache, dbPing func(context.Context) error, pub *fakePublisher, amqpAlive func() bool) http.Handler {
	cfg := handler.Config{Host: "localhost", Port: "8080", CacheTTL: time.Hour}
	return handler.NewRouter(cfg, r, c, dbPing, c.Ping, pub, amqpAlive, newNopLogger())
}

func post(router http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/shorten", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func get(router http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// --- tests ---

func TestShorten_ValidURL(t *testing.T) {
	r, c := newFakeRepo(), newFakeCache()
	router := newRouter(r, c)

	w := post(router, `{"url":"https://example.com/some/long/path"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal("invalid JSON response:", err)
	}
	code, ok := resp["code"]
	if !ok || len(code) != 6 {
		t.Fatalf("expected 6-char code, got %q", code)
	}
	shortURL, ok := resp["short_url"]
	if !ok || !strings.HasSuffix(shortURL, "/"+code) {
		t.Fatalf("expected short_url ending in /%s, got %q", code, shortURL)
	}
}

func TestShorten_MissingURL(t *testing.T) {
	router := newRouter(newFakeRepo(), newFakeCache())

	for _, body := range []string{`{}`, `{"url":""}`, `not-json`} {
		w := post(router, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %q: expected 400, got %d", body, w.Code)
		}
	}
}

func TestShorten_InvalidURL(t *testing.T) {
	router := newRouter(newFakeRepo(), newFakeCache())

	for _, u := range []string{
		`{"url":"ftp://example.com"}`,
		`{"url":"not-a-url"}`,
		`{"url":"//missing-scheme.com"}`,
	} {
		w := post(router, u)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %q: expected 400, got %d", u, w.Code)
		}
	}
}

func TestRedirect_CacheMiss(t *testing.T) {
	r, c := newFakeRepo(), newFakeCache()
	router := newRouter(r, c)

	// seed repo directly, bypass shorten endpoint
	_ = r.Save(context.Background(), "https://example.com", "abc123")
	r.saveCalled = 0
	r.findCalled = 0

	w := get(router, "/abc123")

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	if w.Header().Get("Location") != "https://example.com" {
		t.Fatalf("unexpected Location: %s", w.Header().Get("Location"))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.setCalled != 1 {
		t.Fatalf("expected cache to be populated once, setCalled=%d", c.setCalled)
	}
}

func TestRedirect_CacheHit(t *testing.T) {
	r, c := newFakeRepo(), newFakeCache()
	// pre-warm cache
	_ = c.Set(context.Background(), "xyz999", "https://cached.example.com", time.Hour)
	c.setCalled = 0
	router := newRouter(r, c)

	w := get(router, "/xyz999")

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	if r.findCalled != 0 {
		t.Fatalf("expected no DB call on cache hit, findCalled=%d", r.findCalled)
	}
}

func TestRedirect_NotFound(t *testing.T) {
	router := newRouter(newFakeRepo(), newFakeCache())

	w := get(router, "/doesnotexist")

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestRedirect_PublishesEvent(t *testing.T) {
	r, c := newFakeRepo(), newFakeCache()
	pub := newFakePublisher()
	router := newRouterWithPublisher(r, c, pub)

	_ = r.Save(context.Background(), "https://example.com", "abc123")

	req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
	req.RemoteAddr = "203.0.113.42:54321"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	event := pub.waitEvent(t)
	if event.Code != "abc123" {
		t.Fatalf("expected event.Code=abc123, got %q", event.Code)
	}
	if event.Timestamp.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}
}

func TestRedirect_PublishErrorDoesNotAffectResponse(t *testing.T) {
	r, c := newFakeRepo(), newFakeCache()
	pub := newFakePublisher()
	pub.returnError = errors.New("amqp down")
	router := newRouterWithPublisher(r, c, pub)

	_ = r.Save(context.Background(), "https://example.com", "abc123")

	w := get(router, "/abc123")

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302 even when publisher fails, got %d", w.Code)
	}
}

func TestRedirect_IPHashStripsPort(t *testing.T) {
	r, c := newFakeRepo(), newFakeCache()
	pub := newFakePublisher()
	router := newRouterWithPublisher(r, c, pub)

	_ = r.Save(context.Background(), "https://example.com", "abc123")

	req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
	req.RemoteAddr = "203.0.113.42:54321"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	event := pub.waitEvent(t)

	sum := sha256.Sum256([]byte("203.0.113.42"))
	want := hex.EncodeToString(sum[:])
	if event.IPHash != want {
		t.Fatalf("expected IPHash=%s, got %s", want, event.IPHash)
	}
}

func TestRedirect_ReferrerEmpty(t *testing.T) {
	r, c := newFakeRepo(), newFakeCache()
	pub := newFakePublisher()
	router := newRouterWithPublisher(r, c, pub)

	_ = r.Save(context.Background(), "https://example.com", "abc123")

	w := get(router, "/abc123")

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}

	event := pub.waitEvent(t)
	if event.Referrer != "" {
		t.Fatalf("expected empty referrer, got %q", event.Referrer)
	}
}

func TestHealth_AllUp(t *testing.T) {
	r, c := newFakeRepo(), newFakeCache()
	router := newRouter(r, c)

	w := get(router, "/health")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", resp["status"])
	}
	if resp["amqp"] != "up" {
		t.Fatalf("expected amqp up, got %q", resp["amqp"])
	}
}

func TestHealth_RedisDegraded(t *testing.T) {
	r, c := newFakeRepo(), newFakeCache()
	c.failPing = true
	router := newRouterWithPings(r, c, func(_ context.Context) error { return nil })

	w := get(router, "/health")

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "degraded" {
		t.Fatalf("expected status degraded, got %q", resp["status"])
	}
	if resp["redis"] != "down" {
		t.Fatalf("expected redis down, got %q", resp["redis"])
	}
}

func TestHealth_AMQPDown(t *testing.T) {
	r, c := newFakeRepo(), newFakeCache()
	router := newRouterWithAMQPAlive(r, c, func() bool { return false })

	w := get(router, "/health")

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "degraded" {
		t.Fatalf("expected status degraded, got %q", resp["status"])
	}
	if resp["amqp"] != "down" {
		t.Fatalf("expected amqp down, got %q", resp["amqp"])
	}
}
