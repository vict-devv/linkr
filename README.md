## Linkr - URL Shortener Platform

Linkr is a self-hosted URL shortening platform built in Go. It consists of two independent services that communicate via RabbitMQ.

```
POST /shorten  ─►  shortener-api  ─► Postgres
GET  /{code}   ─►  shortener-api  ─► Redis (cache) ─► Postgres
                        │
                        └── RabbitMQ (redirect.clicked)
                                │
                          analytics-worker ─► MongoDB
```

## Services

### shortener-api

HTTP API for creating and resolving short URLs. Stores mappings in Postgres with a Redis read-through cache. Publishes a `redirect.clicked` event to RabbitMQ on every successful redirect.

**Endpoints**

| Method | Path     | Description                     |
| ------ | -------- | ------------------------------- |
| POST   | /shorten | Create a short URL              |
| GET    | /{code}  | Redirect to the original URL    |
| GET    | /health  | Liveness check (Postgres+Redis) |

**Environment variables**

| Variable       | Required | Default   | Description                 |
| -------------- | -------- | --------- | --------------------------- |
| `DATABASE_URL` | yes      | —         | Postgres connection string  |
| `REDIS_URL`    | yes      | —         | Redis address (`host:port`) |
| `HOST`         | no       | `0.0.0.0` | Listen host                 |
| `PORT`         | no       | `8080`    | Listen port                 |
| `CACHE_TTL`    | no       | `24h`     | Redis cache TTL             |

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
