# Plan: Analytics Worker Service

## Context

`shortener-api` already publishes a JSON event to RabbitMQ on every successful redirect but nothing consumes those events yet. This plan implements `analytics-worker` — a new, standalone Go service that consumes those events and persists them to MongoDB for analytics. It mirrors the conventions of `shortener-api` exactly (slog, interface+fake pattern, `tests/` folder, env-based config).

Resolved open questions from spec:
- Dead-lettered messages → discard via `requeue=false`, no separate DLQ consumer
- `click_events` TTL → 2 weeks (TTL index on `ReceivedAt`)
- `latency_ms` → computed from AMQP message timestamp header (`delivery.Timestamp`)
- Prometheus metrics → future iteration, out of scope here

---

## Files to Create

```
analytics-worker/
├── go.mod                               # module github.com/linkr/analytics-worker, go 1.22.0
├── go.sum
├── cmd/
│   └── analytics-worker/
│       └── main.go                      # wires deps, starts consumer + health server, shutdown
├── internal/
│   ├── consumer/
│   │   ├── consumer.go                  # EventConsumer interface
│   │   └── amqp.go                      # AMQPConsumer concrete impl + reconnect loop
│   ├── repo/
│   │   ├── repo.go                      # ClickRepository interface + ClickEvent struct
│   │   └── mongo.go                     # MongoRepo concrete impl
│   └── handler/
│       └── health.go                    # GET /health handler + NewHealthServer
└── tests/
    ├── consumer_test.go                 # AMQPConsumer unit tests via ProcessMessage
    └── health_test.go                   # Health handler HTTP tests
```

---

## Implementation Steps

### 1. `go.mod` + `go.sum`

```
module github.com/linkr/analytics-worker
go 1.22.0

require (
    github.com/rabbitmq/amqp091-go v1.10.0
    go.mongodb.org/mongo-driver/v2 v2.0.0
)
```

Run `go mod tidy` to generate `go.sum` and resolve exact transitive versions.

---

### 2. `internal/repo/repo.go` — Interface + ClickEvent

- `ClickEvent` struct: `Code string`, `Timestamp time.Time`, `Referrer string`, `IPHash string`, `ReceivedAt time.Time`
- `ClickRepository` interface: `Insert(ctx context.Context, event ClickEvent) error`

---

### 3. `internal/repo/mongo.go` — MongoRepo

- `MongoRepo` struct holds `*mongo.Client`, `*mongo.Collection`, `*slog.Logger`
- `NewMongoRepo(ctx, uri, dbName string, log) (*MongoRepo, error)`:
  1. `mongo.Connect` with the URI
  2. Ping (fatal if fails)
  3. Get collection `click_events` with `WriteConcern: majority`
  4. Call `ensureIndexes(ctx)` — creates compound `{ code: 1, timestamp: -1 }` and TTL index on `ReceivedAt` with `ExpireAfterSeconds: 14*24*3600`; both use `IndexView.CreateOne`
- `Insert(ctx, event)`: set `event.ReceivedAt = time.Now()` then `collection.InsertOne`
- `Ping(ctx)`: `client.Ping` with 2 s timeout
- `Close(ctx)`: `client.Disconnect`

---

### 4. `internal/consumer/consumer.go` — Interface

```go
type EventConsumer interface {
    Start(ctx context.Context) error
    Stop() error
}
```

---

### 5. `internal/consumer/amqp.go` — AMQPConsumer

**Struct fields:** `url string`, `prefetch int`, `repo repo.ClickRepository`, `log *slog.Logger`, `conn *amqp091.Connection`, `ch *amqp091.Channel`, `stopCh chan struct{}`, `doneCh chan struct{}`

**`NewAMQPConsumer(url string, prefetch int, repo, log) *AMQPConsumer`**

**`Start(ctx) error`**:
- Call `connect()` — returns error after max retries
- Start goroutine that ranges over deliveries and calls `ProcessMessage(d)`
- Watch `ch.NotifyClose(make(chan *amqp091.Error, 1))`: on channel close attempt `reconnect()` loop; if reconnect exhausted, log error and return

**`connect() error`** (also called by reconnect):
- Exponential backoff: up to 5 attempts, base 1 s, cap 30 s (`min(base * 2^attempt, 30s)`)
- `amqp091.Dial(url)` → `conn.Channel()` → declare exchange (`redirects`, topic, durable) → declare queue (`analytics.clicks`, durable) → `QueueBind(routing key: redirect.clicked)` → `Qos(prefetch, 0, false)` → `Consume(queue, consumer-tag, autoAck=false, ...)`
- Log each retry attempt at Warn, success at Info, exhaustion at Error

**`ProcessMessage(d amqp091.Delivery)`** (exported for tests):
- Unmarshal JSON body into local struct `{Code, Timestamp, Referrer, IPHash}`
- Validate: `Code` non-empty, `Timestamp` parseable as RFC3339
- On validation failure: `d.Nack(false, false)`, log Warn with reason, return
- Call `repo.Insert(ctx, event)`
- On insert failure: `d.Nack(false, false)`, log Warn, return
- On success: `d.Ack(false)`, log Info with `code`, `timestamp`, `latency_ms` (= `time.Since(d.Timestamp).Milliseconds()`)

**`Stop() error`**:
- Close `stopCh` to signal no new deliveries
- Wait for `doneCh` (goroutine signals when drain complete)
- Close channel and connection
- Log shutdown at Info

**`IsAlive() bool`**: returns true when `conn` and `ch` are non-nil and not closed (used by health handler).

**Reconnect loop is entirely internal to `AMQPConsumer`; `main.go` only sees `Start` / `Stop`.**

---

### 6. `internal/handler/health.go` — Health Server

- `NewHealthServer(port string, amqpAlive func() bool, mongoPing func(context.Context) error, log *slog.Logger) *http.Server`
- Single handler registered on `"GET /health"` using `http.NewServeMux()`
- On request:
  1. Check `amqpAlive()`
  2. Ping mongo with 2 s context timeout
  3. Build response: `{"status":"ok","amqp":"up|down","mongo":"up|down"}`
  4. Return 200 if both up, 503 otherwise
- Error responses: `{"error":"<message>"}` with appropriate status code

---

### 7. `cmd/analytics-worker/main.go`

Pattern mirrors `shortener-api/cmd/shortener-api/main.go`:

1. Init `slog.New(slog.NewJSONHandler(os.Stdout, nil))`
2. Load config with `mustEnv` / `envOr` / `parseDuration` helpers (same pattern as shortener-api)
3. `NewMongoRepo(ctx, mongoURI, mongoDB, log)` — fatal on error
4. `NewAMQPConsumer(amqpURL, prefetch, mongoRepo, log)`
5. `NewHealthServer(healthPort, consumer.IsAlive, mongoRepo.Ping, log)`
6. Launch `go consumer.Start(ctx)` and `go healthServer.ListenAndServe()`
7. `ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)`
8. Block on `<-ctx.Done()`
9. Log "shutdown signal received"
10. `shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)`
11. Ordered shutdown: `consumer.Stop()` → `mongoRepo.Close(shutdownCtx)` → `healthServer.Shutdown(shutdownCtx)`
12. Hard timeout enforced via `time.AfterFunc(shutdownTimeout, func() { os.Exit(1) })`

---

### 8. `tests/consumer_test.go`

Uses a `fakeRepo` implementing `ClickRepository` and a `fakeAcknowledger` implementing `amqp091.Acknowledger` (tracks Ack/Nack calls). Each test builds a synthetic `amqp091.Delivery` with the fake acknowledger and calls `consumer.ProcessMessage(d)` directly.

Tests:
- Valid message → acked, inserted, `ReceivedAt` set
- Empty `code` → nacked `requeue=false`, no insert
- Unparseable `timestamp` → nacked `requeue=false`, no insert
- `repo.Insert` returns error → nacked `requeue=false`

---

### 9. `tests/health_test.go`

Uses `httptest.NewRequest` + `httptest.NewRecorder`. Passes synthetic `amqpAlive` closures and `mongoPing` functions to `NewHealthServer`.

Tests:
- Both up → 200, `status:ok`, `amqp:up`, `mongo:up`
- AMQP down → 503, `amqp:down`
- Mongo ping fails → 503, `mongo:down`

---

## Verification

```sh
# Install deps
cd analytics-worker && go mod tidy

# Run tests
go test ./...

# Static analysis
go vet ./...

# Smoke test (requires running RabbitMQ + MongoDB):
docker run -d -p 5672:5672 -p 15672:15672 rabbitmq:3-management
docker run -d -p 27017:27017 mongo:7

AMQP_URL=amqp://guest:guest@localhost:5672/ \
MONGO_URI=mongodb://localhost:27017 \
go run ./cmd/analytics-worker

# Health check
curl http://localhost:8081/health
# Expected: {"status":"ok","amqp":"up","mongo":"up"}

# Publish a test message via RabbitMQ management UI or amqp client and confirm
# click_events document appears in MongoDB analytics.click_events collection
```
