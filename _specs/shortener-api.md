# Spec for shortener-api

branch: claude/feature/shortener-api

## Summary

A Go HTTP microservice (`shortener-api`) that shortens URLs. It accepts a long URL, generates a unique 6-character base62 short code, persists it to Postgres, and serves redirect lookups via Redis-backed caching. The service exposes three endpoints: shorten, redirect, and health.

## Functional Requirements

### Endpoints

- `POST /shorten`
  - Accepts JSON body `{ "url": "<long_url>" }`.
  - Validates that the URL is well-formed and uses `http` or `https`.
  - Generates a unique 6-char base62 short code.
  - Persists the mapping to Postgres.
  - Invalidates any existing Redis cache entry for that code.
  - Returns `{ "code": "<code>", "short_url": "http://<host>/<code>" }`.

- `GET /:code`
  - Checks Redis for the code first (cache hit → 302 redirect).
  - On cache miss, queries Postgres; if found, populates Redis with the configured TTL and redirects 302.
  - Returns 404 JSON error if the code is not found in either store.

- `GET /health`
  - Probes Postgres and Redis connectivity.
  - Returns `{ "status": "ok"|"degraded", "postgres": "up"|"down", "redis": "up"|"down" }`.
  - HTTP 200 when all dependencies are up; HTTP 503 when any are down.

### Repository layer (`internal/repo`)

- `URLRepository` interface with:
  - `Save(ctx context.Context, longURL, code string) error`
  - `Find(ctx context.Context, code string) (string, error)` — returns `("", repo.ErrNotFound)` when the code does not exist.
- `PostgresRepo` concrete implementation backed by `pgx/v5`.
- Database migration creates a single `urls` table: `code TEXT PRIMARY KEY`, `long_url TEXT NOT NULL`, `created_at TIMESTAMPTZ DEFAULT now()`.

### Cache layer (`internal/cache`)

- `URLCache` interface with:
  - `Get(ctx context.Context, code string) (string, error)` — returns `("", cache.ErrNotFound)` on miss.
  - `Set(ctx context.Context, code string, longURL string, ttl time.Duration) error`
  - `Delete(ctx context.Context, code string) error` — used by `POST /shorten` to invalidate stale entries.
- `RedisCache` concrete implementation backed by `go-redis/v9`.
- Default cache TTL: 24 hours, overridable via the `CACHE_TTL` environment variable.

### Logging

- Use `log/slog` with `slog.NewJSONHandler(os.Stdout, nil)` as the global logger.
- HTTP middleware logs each request: method, path, response status code, and latency.
- Cache hits and misses are logged at `DEBUG` level.
- Postgres errors and Redis errors are logged at `ERROR` level with the offending operation as a field.

### Project layout

```
shortener-api/
  cmd/shortener-api/   # main package, wires dependencies and starts HTTP server
  internal/
    repo/              # URLRepository interface + PostgresRepo
    cache/             # URLCache interface + RedisCache
    handler/           # HTTP handlers for /shorten, /:code, /health
    middleware/        # request-logging middleware
  tests/               # acceptance/integration tests
```

### Configuration (environment variables)

| Variable       | Default   | Description                    |
| -------------- | --------- | ------------------------------ |
| `HOST`         | `0.0.0.0` | Bind address                   |
| `PORT`         | `8080`    | Bind port                      |
| `DATABASE_URL` | —         | Postgres DSN (required)        |
| `REDIS_URL`    | —         | Redis address (required)       |
| `CACHE_TTL`    | `24h`     | Redis TTL for cached redirects |

## Possible Edge Cases

- Duplicate long URL submitted — a new code is generated each time (no deduplication).
- Code collision — retry with a freshly generated code until unique (Postgres `PRIMARY KEY` constraint is the guard).
- Malformed or non-HTTP(S) URLs in `POST /shorten` — return 400 with a descriptive error.
- Redis unavailable — degrade gracefully: skip cache on reads, skip cache population, still serve redirects from Postgres.
- Postgres unavailable at startup — fail fast with a logged fatal error.
- Very long URLs — enforce a maximum input length (e.g. 2048 chars) to prevent abuse.
- `GET /health` called while Postgres is restarting — should not panic; catch dial errors and mark `"postgres": "down"`.

## Acceptance Criteria

- `POST /shorten` with a valid URL returns 200, a 6-char alphanumeric code, and a well-formed `short_url`.
- `POST /shorten` with an invalid URL returns 400.
- `GET /:code` for an existing code returns 302 to the original URL.
- `GET /:code` for an unknown code returns 404.
- A second `GET /:code` for the same code is served from Redis (cache hit logged).
- `GET /health` returns 200 `{ "status": "ok", ... }` when both dependencies are reachable.
- `GET /health` returns 503 when either dependency is down.
- All HTTP requests produce a structured JSON log line including method, path, status, and latency.

## Open Questions

- Should `POST /shorten` deduplicate — i.e. return the same code if the long URL was already shortened? (Current spec: no deduplication.) No deduplication.
- Is a custom alias (`POST /shorten` with a caller-supplied code) in scope? No.
- Should expired Redis entries also be purged from Postgres, or is Postgres the permanent store? Postgres is the permant store, it will be useful for auditing in the future.
- What is the maximum allowed URL length? 2048.

## Testing Guidelines

Create test file(s) in the `./tests` folder. Cover the following cases without going too heavy:

- `POST /shorten` with a valid URL → 200, valid code and short_url in response.
- `POST /shorten` with a missing or malformed URL → 400.
- `GET /:code` cache miss path → Postgres lookup → 302, Redis populated.
- `GET /:code` cache hit path → Redis lookup → 302 (no Postgres query).
- `GET /:code` unknown code → 404.
- `GET /health` with both dependencies up → 200 `ok`.
- `GET /health` with Redis down → 503 `degraded`.
