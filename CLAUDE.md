# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**linkr** is a self-hosted URL shortening platform written in Go. It consists of two independent services, each with its own `go.mod`, that communicate via RabbitMQ:

- **shortener-api** — HTTP API for creating and resolving short URLs (Postgres + Redis)
- **analytics-worker** — AMQP consumer that records click events to MongoDB

## Common Commands

All commands must be run from within the respective service directory (`shortener-api/` or `analytics-worker/`), since each is an independent Go module.

```sh
# Build
go build ./...

# Run all tests
go test ./...

# Run tests in a specific package
go test ./tests/...

# Run a single test
go test ./tests/ -run TestShorten_ValidURL

# Static analysis
go vet ./...

# Run a service (example env vars — see README for full list)
DATABASE_URL=postgres://... REDIS_URL=localhost:6379 go run ./cmd/shortener-api
AMQP_URL=amqp://guest:guest@localhost:5672/ MONGO_URI=mongodb://localhost:27017 go run ./cmd/analytics-worker
```

Local dependencies via Docker (Postgres, Redis, RabbitMQ, MongoDB) are documented in the README.

## Architecture

### Service structure

Both services follow the same internal layout:

```
cmd/<service>/main.go      — wires dependencies and starts the process
internal/handler/          — HTTP handlers + router (shortener-api)
internal/consumer/         — AMQP consumer loop (analytics-worker)
internal/repo/             — storage interface + implementation
internal/cache/            — cache interface + Redis impl (shortener-api)
internal/publisher/        — AMQP publisher (shortener-api)
internal/middleware/       — HTTP middleware (logging)
internal/model/            — shared event types
tests/                     — black-box tests against the assembled router/consumer
```

### shortener-api

- Uses the Go 1.22 stdlib `net/http` mux with method+path patterns (`"GET /{code}"`).
- `NewRouter` in [shortener-api/internal/handler/routes.go](shortener-api/internal/handler/routes.go) assembles the mux and wires all dependencies through interfaces (`URLRepository`, `URLCache`, `EventPublisher`).
- On startup, `pgRepo.Migrate` applies the schema automatically — no migration tool needed.
- On redirect, a `redirect.clicked` event is published to RabbitMQ asynchronously; a publish failure does **not** affect the HTTP response.
- Redis is a read-through cache on top of Postgres. Cache TTL is configurable via `CACHE_TTL`.

### analytics-worker

- `AMQPConsumer` in [analytics-worker/internal/consumer/amqp.go](analytics-worker/internal/consumer/amqp.go) wraps connection, channel setup, and a `runLoop` that reconnects automatically with exponential backoff (up to 5 attempts, max 30s delay).
- `ProcessMessage` is exported to allow unit testing without a live broker.
- Consumes from exchange `redirects` (topic), routing key `redirect.clicked`, queue `analytics.clicks`.
- Invalid messages (bad JSON, missing `code`, bad timestamp) are nacked without requeue.

### Testing approach

Tests use hand-written fakes (not mocks) that implement the same interfaces as the real implementations. No test framework beyond stdlib `testing`. Tests in `tests/` build and exercise the assembled router or consumer directly via `httptest`.
