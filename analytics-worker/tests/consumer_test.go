package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/linkr/analytics-worker/internal/consumer"
	"github.com/linkr/analytics-worker/internal/repo"
)

func newNopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- fakes ---

type fakeRepo struct {
	mu          sync.Mutex
	events      []repo.ClickEvent
	returnError error
}

func (f *fakeRepo) Insert(_ context.Context, event repo.ClickEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.returnError != nil {
		return f.returnError
	}
	f.events = append(f.events, event)
	return nil
}

type fakeAck struct {
	mu            sync.Mutex
	acked         int
	nacked        int
	requeueOnNack bool
}

func (a *fakeAck) Ack(_ uint64, _ bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.acked++
	return nil
}

func (a *fakeAck) Nack(_ uint64, _ bool, requeue bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nacked++
	a.requeueOnNack = requeue
	return nil
}

func (a *fakeAck) Reject(_ uint64, _ bool) error { return nil }

// --- helpers ---

func newConsumer(r repo.ClickRepository) *consumer.AMQPConsumer {
	return consumer.NewAMQPConsumer("amqp://", 10, r, newNopLogger())
}

func makeDelivery(ack amqp.Acknowledger, body []byte) amqp.Delivery {
	return amqp.Delivery{
		Acknowledger: ack,
		Body:         body,
		Timestamp:    time.Now(),
	}
}

func makePayload(code, timestamp, referrer, ipHash string) []byte {
	b, _ := json.Marshal(map[string]string{
		"code":      code,
		"timestamp": timestamp,
		"referrer":  referrer,
		"ip_hash":   ipHash,
	})
	return b
}

// --- tests ---

func TestProcessMessage_Valid(t *testing.T) {
	r := &fakeRepo{}
	c := newConsumer(r)
	ack := &fakeAck{}

	ts := time.Now().UTC().Format(time.RFC3339)
	d := makeDelivery(ack, makePayload("abc123", ts, "https://example.com", "deadbeef"))
	c.ProcessMessage(d)

	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.acked != 1 {
		t.Fatalf("expected 1 ack, got %d", ack.acked)
	}
	if ack.nacked != 0 {
		t.Fatalf("expected 0 nacks, got %d", ack.nacked)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) != 1 {
		t.Fatalf("expected 1 inserted event, got %d", len(r.events))
	}
	if r.events[0].ReceivedAt.IsZero() {
		t.Fatal("expected ReceivedAt to be set by Insert")
	}
}

func TestProcessMessage_EmptyCode(t *testing.T) {
	r := &fakeRepo{}
	c := newConsumer(r)
	ack := &fakeAck{}

	ts := time.Now().UTC().Format(time.RFC3339)
	d := makeDelivery(ack, makePayload("", ts, "", ""))
	c.ProcessMessage(d)

	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.nacked != 1 {
		t.Fatalf("expected 1 nack, got %d", ack.nacked)
	}
	if ack.requeueOnNack {
		t.Fatal("expected requeue=false on validation nack")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) != 0 {
		t.Fatalf("expected no inserts, got %d", len(r.events))
	}
}

func TestProcessMessage_InvalidTimestamp(t *testing.T) {
	r := &fakeRepo{}
	c := newConsumer(r)
	ack := &fakeAck{}

	d := makeDelivery(ack, makePayload("abc123", "not-a-timestamp", "", ""))
	c.ProcessMessage(d)

	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.nacked != 1 {
		t.Fatalf("expected 1 nack, got %d", ack.nacked)
	}
	if ack.requeueOnNack {
		t.Fatal("expected requeue=false on validation nack")
	}
}

func TestProcessMessage_InsertError(t *testing.T) {
	r := &fakeRepo{returnError: errors.New("mongo down")}
	c := newConsumer(r)
	ack := &fakeAck{}

	ts := time.Now().UTC().Format(time.RFC3339)
	d := makeDelivery(ack, makePayload("abc123", ts, "", ""))
	c.ProcessMessage(d)

	ack.mu.Lock()
	defer ack.mu.Unlock()
	if ack.nacked != 1 {
		t.Fatalf("expected 1 nack, got %d", ack.nacked)
	}
	if ack.requeueOnNack {
		t.Fatal("expected requeue=false on insert nack")
	}
	if ack.acked != 0 {
		t.Fatalf("expected no acks, got %d", ack.acked)
	}
}
