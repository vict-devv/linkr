# Spec for Analytics Worker

branch: claude/feature/analytics-worker

## Summary

`analytics-worker` is a Go AMQP consumer service that records click-through analytics events emitted by `shortener-api` on every successful redirect. It consumes JSON messages from RabbitMQ, validates and persists them to MongoDB, and exposes a health endpoint for liveness checks. The service is designed for graceful shutdown and resilient reconnection under transient broker failures.

## Functional Requirements

### Message Contract

- Consume from exchange `redirects` (topic, durable), queue `analytics.clicks` (durable), routing key `redirect.clicked`
- Expected payload fields: `code` (string), `timestamp` (RFC3339 string), `referrer` (string, may be empty), `ip_hash` (SHA-256 hex string, opaque — hashed upstream by shortener-api)

### Consumer Loop (`internal/consumer`)

- Define interface `EventConsumer` with methods `Start(ctx context.Context) error` and `Stop() error`
- Concrete `AMQPConsumer` backed by `rabbitmq/amqp091-go`
- Per-message pipeline: unmarshal JSON → validate (non-empty `code`, parseable `timestamp`) → insert into MongoDB → ack
- On validation or write failure: nack with `requeue=false` to send to dead-letter; must not block subsequent messages
- Prefetch count defaults to 10, overridable via `AMQP_PREFETCH` env var
- Reconnect loop with exponential backoff: max 5 retries, base delay 1 s, cap 30 s; reconnect logic is fully encapsulated inside `AMQPConsumer` and invisible to `main.go`

### Repository Layer (`internal/repo`)

- Define interface `ClickRepository` with method `Insert(ctx context.Context, event ClickEvent) error`
- Concrete `MongoRepo` backed by `mongo-driver/v2`
- Target collection: `click_events`; write concern: majority
- `ClickEvent` struct fields: `Code`, `Timestamp`, `Referrer`, `IPHash`, `ReceivedAt` (set to `time.Now()` by the worker at receipt time)
- Compound index `{ code: 1, timestamp: -1 }` created on startup if absent

### Health Endpoint (`internal/handler`)

- `GET /health` served on a standalone `net/http` server, default port `:8081` (configurable via `HEALTH_PORT`)
- Response body: `{ "status": "ok", "amqp": "up|down", "mongo": "up|down" }`
- AMQP liveness: check that the connection and channel are not closed
- Mongo liveness: `Ping` with a 2 s timeout
- Returns HTTP 200 when both dependencies are up; HTTP 503 otherwise
- Error responses use the shape `{ "error": "<message>" }`
- No external router — standard `net/http` only

### Graceful Shutdown

- Listen for `SIGTERM` and `SIGINT` via `signal.NotifyContext`
- On signal: stop the AMQP consumer (allow in-flight messages to finish, accept no new deliveries), close the Mongo client, shut down the health HTTP server
- Hard timeout: 15 s; if shutdown is not complete by then, call `os.Exit(1)`
- Timeout configurable via `SHUTDOWN_TIMEOUT` env var

### Logging

- Use `log/slog` with `slog.NewJSONHandler(os.Stdout, nil)`, injected top-down from `main.go`
- Log each consumed message with fields: `code`, `timestamp`, `latency_ms` (time from message publish to worker receipt)
- Log reconnect attempts and outcomes at `Warn`/`Error` level
- Log nack events and the reason at `Warn` level
- Log shutdown lifecycle events (signal received, consumer stopped, Mongo closed, server stopped) at `Info` level

### Configuration (env vars)

| Variable           | Default                              |
| ------------------ | ------------------------------------ |
| `AMQP_URL`         | `amqp://guest:guest@localhost:5672/` |
| `AMQP_PREFETCH`    | `10`                                 |
| `MONGO_URI`        | `mongodb://localhost:27017`          |
| `MONGO_DB`         | `analytics`                          |
| `HEALTH_PORT`      | `8081`                               |
| `SHUTDOWN_TIMEOUT` | `15s`                                |

## Possible Edge Cases

- RabbitMQ broker unavailable at startup — consumer must not crash; backoff/retry applies
- Malformed JSON payload — must nack (dead-letter) without panicking or blocking
- MongoDB write timeout or transient failure — nack the message; do not retry inline
- Reconnect retry limit exceeded — log a fatal error and exit; do not spin indefinitely
- `ip_hash` is empty or an unexpected format — treat as opaque string and persist as-is (no validation)
- SIGTERM received while a batch of messages is mid-flight — in-flight messages must be acked/nacked before shutdown completes (within the hard timeout)
- Duplicate messages due to redelivery after a crash — no deduplication required; `ReceivedAt` differentiates records

## Acceptance Criteria

- Consumer successfully processes a well-formed message end-to-end: RabbitMQ → MongoDB insert → ack
- Invalid messages (missing `code`, unparseable `timestamp`) are nacked with `requeue=false` and do not block subsequent messages
- Worker reconnects automatically after a broker restart, up to 5 attempts with backoff
- `GET /health` returns 200 with correct JSON when AMQP and Mongo are reachable
- `GET /health` returns 503 when either dependency is unavailable
- Sending SIGTERM triggers graceful shutdown; in-flight messages are completed before the process exits
- Hard shutdown timeout of 15 s is enforced; process calls `os.Exit(1)` if exceeded
- All structured log lines include the fields documented in the Logging section
- Service starts and runs with no external router dependency (standard `net/http` only)

## Open Questions

- Should dead-lettered messages be retried via a separate DLQ consumer, or simply discarded? (Current spec: discard via `requeue=false`). Let's discard them for now.
- Is there a retention policy for `click_events` documents in MongoDB (TTL index)? Keep them for two week.
- Should `latency_ms` be derived from the AMQP message timestamp header or from the JSON `timestamp` field? From the AMQP messate timestamp header.
- Will `analytics-worker` need to expose Prometheus metrics in a future iteration? Yes.

## Testing Guidelines

Create test files under `./tests` (or alongside the package under `_test.go` files) covering the following cases without going too heavy:

- `AMQPConsumer`: valid message is unmarshalled, inserted, and acked
- `AMQPConsumer`: message with empty `code` is nacked with `requeue=false`
- `AMQPConsumer`: message with invalid `timestamp` is nacked with `requeue=false`
- `AMQPConsumer`: MongoDB write failure triggers nack, not a panic
- `MongoRepo.Insert`: inserts a `ClickEvent` document and sets `ReceivedAt`
- `MongoRepo` startup: compound index is created if absent (idempotent)
- Health handler: returns 200 with `"amqp":"up","mongo":"up"` when both dependencies are healthy
- Health handler: returns 503 when either dependency reports down
- Graceful shutdown: consumer stops accepting new deliveries after `Stop()` is called
