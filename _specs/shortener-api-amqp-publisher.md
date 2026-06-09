# Spec for Shortener API AMQP Publisher

branch: claude/feature/shortener-api-amqp-publisher

## Summary

Amend `shortener-api` to publish a `RedirectEvent` message to RabbitMQ on every successful `GET /:code` redirect. The service declares the `redirects` exchange (topic, durable) on startup and fires a fire-and-forget publish to routing key `redirect.clicked`. The message is consumed by the existing `analytics-worker` service. Publish failures must never affect the redirect response.

## Functional Requirements

### Publisher layer (`internal/publisher`)

- Define an `EventPublisher` interface exposing:
  - `Publish(ctx context.Context, event RedirectEvent) error`
  - `Close() error`
- Provide a concrete `AMQPPublisher` implementation backed by `rabbitmq/amqp091-go`.
- On `Connect()`, declare the exchange `redirects` as topic and durable. Declaration is idempotent and safe to call on every reconnect.
- Publish messages to routing key `redirect.clicked` with content-type `application/json` and persistent delivery mode.
- Implement a reconnect loop with exponential backoff (max 5 retries, base 1 s, cap 30 s) — identical policy to `analytics-worker`. The reconnect logic is entirely self-contained and transparent to callers.
- Expose `Close() error` for clean shutdown.

### Message payload (`internal/model`)

Add a `RedirectEvent` struct with the following fields:

| Field       | Type        | JSON key    | Description                                         |
| ----------- | ----------- | ----------- | --------------------------------------------------- |
| `Code`      | `string`    | `code`      | The short code that was resolved                    |
| `Timestamp` | `time.Time` | `timestamp` | UTC time at the point of redirect                   |
| `Referrer`  | `string`    | `referrer`  | Value of the HTTP `Referer` header; empty if absent |
| `IPHash`    | `string`    | `ip_hash`   | SHA-256 hex digest of the client IP (port stripped) |

IP hashing responsibility sits in `shortener-api`. The port must be stripped from `RemoteAddr` via `net.SplitHostPort` before hashing. `analytics-worker` treats `ip_hash` as an opaque string.

### Integration point — redirect handler (`internal/handler/redirect.go`)

- Inject `EventPublisher` into the redirect handler as a constructor argument.
- After a successful cache-hit or DB lookup, and just before issuing the 302 redirect:
  1. Build a `RedirectEvent` with the current code, UTC timestamp, referrer, and IP hash.
  2. Call `publisher.Publish(ctx, event)` in a goroutine (fire-and-forget).
  3. A publish error must never block or fail the redirect. Log at WARN level and continue.

### Health endpoint amendment (`internal/handler/health.go`)

- Add an `"amqp"` field to the health response with value `"up"` or `"down"`.
- Liveness check: confirm the `AMQPPublisher` connection and channel are not closed.
- The overall HTTP 200 response requires postgres, redis, **and** amqp all reporting `"up"`. Any dependency down returns HTTP 503 with `"status": "degraded"`.

Updated health response shape:

```json
{
  "status": "ok|degraded",
  "postgres": "up|down",
  "redis": "up|down",
  "amqp": "up|down"
}
```

### Graceful shutdown amendment (`main.go`)

- After the HTTP server stops accepting connections, call `publisher.Close()` before the process exits.
- Honour the existing shutdown timeout — no new timeout is introduced.

### Configuration additions

| Variable   | Default                              | Description         |
| ---------- | ------------------------------------ | ------------------- |
| `AMQP_URL` | `amqp://guest:guest@localhost:5672/` | RabbitMQ connection |

### Project layout amendment

```
shortener-api/
  internal/
    publisher/   # EventPublisher interface + AMQPPublisher
    model/       # RedirectEvent struct (and any shared types)
```

## Possible Edge Cases

- RabbitMQ unavailable at startup — `AMQPPublisher.Connect()` should retry with backoff; after max retries, the service may still start but health reports `"amqp": "down"`.
- Publish timeout — the goroutine must respect the context deadline and return without blocking the HTTP response path.
- RabbitMQ connection drops mid-flight — the reconnect loop picks it up; individual in-flight publishes may be lost (acceptable, fire-and-forget).
- `RemoteAddr` without a port (e.g. Unix socket path) — `net.SplitHostPort` returns an error; fall back to hashing the raw value.
- Empty `Referer` header — store as empty string, do not omit the field from the JSON payload.
- Exchange already declared by `analytics-worker` with the same parameters — declaration is idempotent, no conflict.

## Acceptance Criteria

- `GET /:code` for an existing code still returns 302 even when RabbitMQ is unreachable.
- A `RedirectEvent` JSON message with the correct code, timestamp, referrer, and ip_hash appears on the `redirects` exchange / routing key `redirect.clicked` after a successful redirect.
- `GET /health` returns 200 `{ "status": "ok", ..., "amqp": "up" }` when all three dependencies are reachable.
- `GET /health` returns 503 when RabbitMQ is down, with `"amqp": "down"`.
- Publish errors are logged at WARN level and do not appear in the HTTP response.
- `publisher.Close()` is called on graceful shutdown; no goroutine leaks remain after shutdown.

## Open Questions

- Should undeliverable messages (exchange unreachable after max retries) be buffered in-memory or dropped silently? Current spec: dropped silently — fire-and-forget, no local buffer. Dropped silently.
- Is publisher confirmation (RabbitMQ `confirm` mode) required for reliability, or is best-effort acceptable? Current spec: best-effort, no confirms. best-effort is acceptable.

## Testing Guidelines

Create test file(s) in the `./tests` folder. Cover the following cases without going too heavy:

- `GET /:code` succeeds and a `RedirectEvent` is published (mock `EventPublisher`).
- `GET /:code` succeeds even when `publisher.Publish` returns an error — response is still 302.
- `RedirectEvent` IP hash is the SHA-256 hex of the stripped IP (not including port).
- `RedirectEvent` referrer is empty string when no `Referer` header is present.
- `GET /health` with all dependencies up returns 200 with `"amqp": "up"`.
- `GET /health` with AMQP down returns 503 with `"amqp": "down"`.
- `AMQPPublisher.Connect()` retries on failure and succeeds when RabbitMQ becomes available within max retries.
