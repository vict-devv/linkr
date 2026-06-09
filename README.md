## Linkr - URL Shortener Platform

Linkr is a self-hosted URL shortening platform built in Go. It consists of three independent services that communicate via RabbitMQ and a shared MongoDB instance.

```
POST /shorten        ─►  shortener-api  ─► Postgres
GET  /{code}         ─►  shortener-api  ─► Redis (cache) ─► Postgres
                               │
                               └── RabbitMQ (redirect.clicked)
                                            │
                                      analytics-worker ─► MongoDB
                                                              │
GET /stats/{code}    ─►       stats-api  ◄─────────────────(read)
GET /health          ─►       stats-api
```

## Services

### shortener-api

HTTP API for creating and resolving short URLs. Stores mappings in Postgres with a Redis read-through cache. Publishes a `redirect.clicked` event to RabbitMQ on every successful redirect.

**Endpoints**

| Method | Path     | Description                              |
| ------ | -------- | ---------------------------------------- |
| POST   | /shorten | Create a short URL                       |
| GET    | /{code}  | Redirect to the original URL             |
| GET    | /health  | Liveness check (Postgres+Redis+RabbitMQ) |

**Environment variables**

| Variable       | Required | Default                              | Description                      |
| -------------- | -------- | ------------------------------------ | -------------------------------- |
| `DATABASE_URL` | yes      | —                                    | Postgres connection string       |
| `REDIS_URL`    | yes      | —                                    | Redis address (`host:port`)      |
| `AMQP_URL`     | no       | `amqp://guest:guest@localhost:5672/` | RabbitMQ connection URL          |
| `HOST`         | no       | `0.0.0.0`                            | Listen host                      |
| `PORT`         | no       | `8080`                               | Listen port                      |
| `CACHE_TTL`    | no       | `24h`                                | Redis cache TTL                  |

**Health endpoint**

`GET /health` returns 200 when Postgres, Redis, and RabbitMQ are all reachable:

```json
{"status":"ok","postgres":"up","redis":"up","amqp":"up"}
```

Returns 503 with `"status":"degraded"` if any dependency is down.

**Run**

```sh
cd shortener-api
DATABASE_URL=postgres://... REDIS_URL=localhost:6379 go run ./cmd/shortener-api
```

---

### analytics-worker

AMQP consumer that records click-through events emitted by `shortener-api`. Consumes from the `analytics.clicks` queue (exchange `redirects`, routing key `redirect.clicked`), validates each message, and persists it to MongoDB. Exposes a health endpoint for liveness checks.

**Environment variables**

| Variable           | Default                              | Description                  |
| ------------------ | ------------------------------------ | ---------------------------- |
| `AMQP_URL`         | `amqp://guest:guest@localhost:5672/` | RabbitMQ connection URL      |
| `AMQP_PREFETCH`    | `10`                                 | Consumer prefetch count      |
| `MONGO_URI`        | `mongodb://localhost:27017`          | MongoDB connection URI       |
| `MONGO_DB`         | `analytics`                          | MongoDB database name        |
| `HEALTH_PORT`      | `8081`                               | Port for the health endpoint |
| `SHUTDOWN_TIMEOUT` | `15s`                                | Graceful shutdown timeout    |

**Health endpoint**

`GET /health` returns 200 when both RabbitMQ and MongoDB are reachable:

```json
{"status":"ok","amqp":"up","mongo":"up"}
```

Returns 503 with `"status":"degraded"` if either dependency is down.

**Run**

```sh
cd analytics-worker
AMQP_URL=amqp://guest:guest@localhost:5672/ MONGO_URI=mongodb://localhost:27017 go run ./cmd/analytics-worker
```

---

### stats-api

Read-only HTTP API that exposes aggregated click analytics for short codes, intended for consumption by a dashboard. Reads from the same MongoDB instance written to by `analytics-worker` — performs no writes.

**Endpoints**

| Method | Path             | Description                                  |
| ------ | ---------------- | -------------------------------------------- |
| GET    | /stats/{code}    | Aggregated analytics for a short code        |
| GET    | /health          | Liveness check (MongoDB)                     |

**`GET /stats/{code}` response**

```json
{
  "code": "abc123",
  "total_clicks": 1042,
  "clicks_over_time": [
    { "date": "2026-05-11", "count": 0 },
    { "date": "2026-05-12", "count": 98 }
  ],
  "top_referrers": [
    { "referrer": "https://twitter.com", "count": 310 },
    { "referrer": "", "count": 205 }
  ]
}
```

- `clicks_over_time`: daily buckets for the last `STATS_WINDOW_DAYS` days, zero-filled for days with no clicks
- `top_referrers`: up to `TOP_REFERRERS_LIMIT` referrers by count, descending
- Returns 404 `{"error":"code not found"}` if no click events exist for the code

> **Prerequisite index** — the following compound index must exist on the `click_events` collection before the service handles production traffic:
> ```
> db.click_events.createIndex({ code: 1, timestamp: -1 })
> ```

**Environment variables**

| Variable               | Default                     | Description                                  |
| ---------------------- | --------------------------- | -------------------------------------------- |
| `PORT`                 | `8080`                      | Listen port                                  |
| `MONGO_URI`            | `mongodb://localhost:27017` | MongoDB connection URI                       |
| `MONGO_DB`             | `analytics`                 | MongoDB database name                        |
| `MONGO_COLLECTION`     | `click_events`              | MongoDB collection name                      |
| `STATS_WINDOW_DAYS`    | `30`                        | Lookback window for `clicks_over_time`       |
| `TOP_REFERRERS_LIMIT`  | `10`                        | Maximum referrers returned                   |

**Health endpoint**

`GET /health` returns 200 when MongoDB is reachable:

```json
{"status":"ok","mongo":"up"}
```

Returns 503 with `"status":"degraded"` if MongoDB is unreachable.

**Run**

```sh
cd stats-api
MONGO_URI=mongodb://localhost:27017 go run ./cmd/stats-api
```

---

## Development

```sh
# Build all packages (from each service directory)
go build ./...

# Run tests
go test ./...

# Static analysis
go vet ./...
```

**Local dependencies (Docker)**

```sh
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres postgres:16
docker run -d -p 6379:6379 redis:7
docker run -d -p 5672:5672 -p 15672:15672 rabbitmq:3-management
docker run -d -p 27017:27017 mongo:7
```
