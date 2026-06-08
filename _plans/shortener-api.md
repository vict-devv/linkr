# Plan: shortener-api service

## Context

`linkr` is a greenfield Go repo. This plan implements `shortener-api`, a URL-shortening HTTP microservice, from scratch as specified in `_specs/shortener-api.md`. The service uses Postgres for persistence and Redis for caching, with structured JSON logging via `log/slog`.

Open question resolved: max URL length = **2048 chars**.

---

## Directory structure to create

All code lives under `shortener-api/` (subdirectory in the repo root).

```
shortener-api/
  go.mod                            # module github.com/linkr/shortener-api, go 1.22
  cmd/shortener-api/
    main.go                         # wires deps, runs HTTP server
  internal/
    repo/
      repo.go                       # URLRepository interface + ErrNotFound
      postgres.go                   # PostgresRepo (pgx/v5) + Migrate() + Ping()
    cache/
      cache.go                      # URLCache interface + ErrNotFound
      redis.go                      # RedisCache (go-redis/v9) + Ping()
    handler/
      routes.go                     # NewRouter — wires mux, wraps with logging middleware
      shorten.go                    # POST /shorten handler + generateCode()
      redirect.go                   # GET /{code} handler
      health.go                     # GET /health handler
    middleware/
      logging.go                    # request-logging middleware + responseWriter wrapper
  tests/
    api_test.go                     # acceptance tests using in-memory fakes
```

---

## Implementation steps

### 1. `shortener-api/go.mod`

```
module github.com/linkr/shortener-api
go 1.22

require:
  github.com/jackc/pgx/v5
  github.com/redis/go-redis/v9
```

Run `go mod tidy` after writing all source files to populate `go.sum`.

---

### 2. `internal/repo/repo.go`

- `URLRepository` interface: `Save(ctx, longURL, code string) error`, `Find(ctx, code string) (string, error)`
- `var ErrNotFound = errors.New("url not found")`

---

### 3. `internal/repo/postgres.go`

- `PostgresRepo` struct holding a `*pgxpool.Pool`
- `NewPostgresRepo(ctx, dsn string) (*PostgresRepo, error)` — calls `pgxpool.New`, then `pool.Ping`; returns error on failure (startup fail-fast)
- `Migrate(ctx) error` — executes `CREATE TABLE IF NOT EXISTS urls (code TEXT PRIMARY KEY, long_url TEXT NOT NULL, created_at TIMESTAMPTZ DEFAULT now())`
- `Save`: `INSERT INTO urls (code, long_url) VALUES ($1, $2)` — caller detects unique-constraint violation via `pgconn.PgError` code `"23505"` to trigger retry
- `Find`: `SELECT long_url FROM urls WHERE code = $1`; maps `pgx.ErrNoRows` → `ErrNotFound`
- `Ping(ctx) error` — calls `pool.Ping(ctx)`; used by health handler

---

### 4. `internal/cache/cache.go`

- `URLCache` interface: `Get`, `Set`, `Delete` (signatures as per spec)
- `var ErrNotFound = errors.New("cache miss")`

---

### 5. `internal/cache/redis.go`

- `RedisCache` struct holding a `*redis.Client`
- `NewRedisCache(addr string) *RedisCache` — `redis.NewClient(&redis.Options{Addr: addr})`
- `Get`: maps `redis.Nil` → `cache.ErrNotFound`
- `Set`: `client.Set(ctx, code, longURL, ttl)`
- `Delete`: `client.Del(ctx, code)`
- `Ping(ctx) error` — `client.Ping(ctx).Err()`; used by health handler

---

### 6. `internal/middleware/logging.go`

- `responseWriter` struct wrapping `http.ResponseWriter`; overrides `WriteHeader` to capture status code (default 200)
- `Logging(log *slog.Logger) func(http.Handler) http.Handler` — records start time, calls `next.ServeHTTP`, logs `method`, `path`, `status`, `latency_ms` at `Info` level

---

### 7. `internal/handler/routes.go`

- `Config` struct: `Host`, `Port string`, `CacheTTL time.Duration`
- `NewRouter(cfg Config, r repo.URLRepository, c cache.URLCache, log *slog.Logger) http.Handler`
  - `http.NewServeMux()` with Go 1.22 pattern syntax
  - `mux.HandleFunc("POST /shorten", ...)`
  - `mux.HandleFunc("GET /{code}", ...)` — `/health` is more specific and wins over `/{code}`
  - `mux.HandleFunc("GET /health", ...)`
  - Wraps mux with `middleware.Logging`

---

### 8. `internal/handler/shorten.go`

- `generateCode() (string, error)` — private; uses `crypto/rand` + `math/big` to pick 6 chars from base62 alphabet `0-9A-Za-z`
- Handler logic:
  1. Decode JSON body; reject if `url` field is empty → 400
  2. Validate URL: `url.Parse`, scheme must be `http` or `https`, length ≤ 2048 → 400 on failure
  3. Loop up to 3 times: generate code → `repo.Save`; on `pgconn.PgError` code `23505` (unique violation), retry; any other error → 500
  4. `cache.Delete(ctx, code)` to invalidate any stale entry (log error but don't fail request)
  5. Build `short_url` from `r.Host` (or `HOST:PORT` env fallback)
  6. Return 200 JSON `{ "code": "...", "short_url": "..." }`

---

### 9. `internal/handler/redirect.go`

- Extract `r.PathValue("code")`
- `cache.Get` → hit: log DEBUG "cache hit", `http.Redirect` 302
- Miss: `repo.Find`
  - Found: `cache.Set` (log error if cache write fails, don't abort), log DEBUG "cache miss", redirect 302
  - `repo.ErrNotFound`: 404 JSON `{ "error": "not found" }`
  - Other DB error: log ERROR, 500

---

### 10. `internal/handler/health.go`

- Inline `pinger` interface: `Ping(ctx context.Context) error`
- Accept `dbPinger pinger` and `cachePinger pinger`
- Probe each concurrently (two goroutines) with a `context.WithTimeout` of 2 s
- Build response: `status = "ok"` if both up, else `"degraded"`; HTTP 200 or 503
- Return JSON `{ "status": "...", "postgres": "up|down", "redis": "up|down" }`

---

### 11. `cmd/shortener-api/main.go`

1. Create `slog.Logger` with `slog.NewJSONHandler(os.Stdout, nil)`
2. Read env: `DATABASE_URL` (required), `REDIS_URL` (required), `HOST` (default `0.0.0.0`), `PORT` (default `8080`), `CACHE_TTL` (default `24h`)
3. `repo.NewPostgresRepo` → fatal on error
4. `postgresRepo.Migrate` → fatal on error
5. `cache.NewRedisCache`
6. `handler.NewRouter(cfg, repo, cache, log)`
7. `http.Server{Addr: host:port, Handler: router}` → `log.Fatal(srv.ListenAndServe())`

---

### 12. `tests/api_test.go`

Use `httptest.NewRecorder` + `httptest.NewRequest`. Implement in-memory fakes:

- `fakeRepo`: `map[string]string`, tracks `saveCalled int`
- `fakeCache`: `map[string]string`, tracks `getCalled`, `setCalled int`

Test cases (per spec Testing Guidelines):
1. `POST /shorten` valid URL → 200, 6-char code, well-formed `short_url`
2. `POST /shorten` missing / malformed URL → 400
3. `GET /{code}` cache miss → Postgres lookup, `fakeCache.setCalled == 1`, 302
4. `GET /{code}` cache hit → no Postgres call (`fakeRepo.findCalled == 0`), 302
5. `GET /{code}` unknown code → 404
6. `GET /health` both up → 200 `{ "status": "ok" }`
7. `GET /health` Redis down → 503 `{ "status": "degraded" }`

---

## Verification

```sh
cd shortener-api
go mod tidy
go build ./...
go vet ./...
go test ./tests/...
```

For a live smoke test (requires Docker):
```sh
docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=pass postgres:16
docker run -d -p 6379:6379 redis:7
DATABASE_URL="postgres://postgres:pass@localhost:5432/postgres" \
REDIS_URL="localhost:6379" \
go run ./cmd/shortener-api
curl -s -X POST localhost:8080/shorten -d '{"url":"https://example.com"}' | jq
curl -v localhost:8080/<returned-code>
curl -s localhost:8080/health | jq
```
