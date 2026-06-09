package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/linkr/analytics-worker/internal/handler"
)

func newHealthServer(amqpAlive func() bool, mongoPing func(context.Context) error) *http.Server {
	return handler.NewHealthServer("8081", amqpAlive, mongoPing, newNopLogger())
}

func healthGet(srv *http.Server, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	srv.Handler.ServeHTTP(w, req)
	return w
}

func TestHealth_AllUp(t *testing.T) {
	srv := newHealthServer(
		func() bool { return true },
		func(_ context.Context) error { return nil },
	)

	w := healthGet(srv, "/health")

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
	if resp["mongo"] != "up" {
		t.Fatalf("expected mongo up, got %q", resp["mongo"])
	}
}

func TestHealth_AMQPDown(t *testing.T) {
	srv := newHealthServer(
		func() bool { return false },
		func(_ context.Context) error { return nil },
	)

	w := healthGet(srv, "/health")

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["amqp"] != "down" {
		t.Fatalf("expected amqp down, got %q", resp["amqp"])
	}
}

func TestHealth_MongoDown(t *testing.T) {
	srv := newHealthServer(
		func() bool { return true },
		func(_ context.Context) error { return errors.New("mongo timeout") },
	)

	w := healthGet(srv, "/health")

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["mongo"] != "down" {
		t.Fatalf("expected mongo down, got %q", resp["mongo"])
	}
}
