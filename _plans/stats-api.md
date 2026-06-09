# Plan: stats-api service

## Context

`shortener-api` creates/resolves short URLs; `analytics-worker` persists click events to MongoDB (`click_events` collection). `stats-api` is a new read-only microservice that reads from that same MongoDB instance and exposes aggregated click analytics over HTTP, intended for a React dashboard. Specified in `_specs/stats-api-service.md`.

## ⚠️ Collection Name Discrepancy

The spec says `MONGO_COLLECTION` defaults to `clicks`, but `analytics-worker/internal/repo/mongo.go:32` hard-codes the collection as `"click_events"`. Using `clicks` as the default would cause stats-api to return 404 for every code in production.

**Resolution in this plan**: default `MONGO_COLLECTION` to `click_events`.

---

## File Layout

New independent Go module at `stats-api/` — mirrors the structure of both existing services:

```
stats-api/
  go.mod
  cmd/stats-api/main.go
  internal/
    handler/
      routes.go       — NewRouter, assembles mux + middleware
      stats.go        — GET /stats/{code} handler
      health.go       — GET /health handler
    repo/
      repo.go         — StatsRepository interface + ClicksOverTime/TopReferrer types
      mongo.go        — MongoStatsRepo implementation
    middleware/
      logging.go      — identical pattern to shortener-api/internal/middleware/logging.go
  tests/
    api_test.go       — black-box tests via httptest + fake repo
```

---

## Implementation Steps

### 1. `go.mod`

Module: `github.com/linkr/stats-api` (matches the `github.com/linkr/<service>` pattern).

Dependencies:
- `go.mongodb.org/mongo-driver/v2 v2.0.0` — same version as analytics-worker
- `golang.org/x/sync` — for `errgroup.WithContext` in the stats handler

### 2. `internal/repo/repo.go`

Define types and interface exactly as specified:

```go
type ClicksOverTime struct { Date string `json:"date"`; Count int64 `json:"count"` }
type TopReferrer    struct { Referrer string `json:"referrer"`; Count int64 `json:"count"` }

type StatsRepository interface {
    TotalClicks(ctx context.Context, code string) (int64, error)
    ClicksOverTime(ctx context.Context, code string, days int) ([]ClicksOverTime, error)
    TopReferrers(ctx context.Context, code string, limit int) ([]TopReferrer, error)
}
```

### 3. `internal/repo/mongo.go` — `MongoStatsRepo`

Constructor `NewMongoStatsRepo(ctx, uri, dbName, collName string, log *slog.Logger) (*MongoStatsRepo, error)`:
- `mongo.Connect` → ping with 5s timeout → store `*mongo.Collection`
- No index creation (required index `{ code:1, timestamp:-1 }` is a prerequisite — documented in spec, not created in app code)
- `Ping(ctx) error` method — 2s timeout, same as analytics-worker
- `Close(ctx) error` method

Aggregation pipelines (all read-only):

**TotalClicks**: `coll.CountDocuments(ctx, bson.D{{Key:"code", Value:code}})`

**ClicksOverTime**: Pipeline:
```
$match: { code: code, timestamp: { $gte: <now - days*24h> } }
$group: { _id: { $dateToString: { format: "%Y-%m-%d", date: "$timestamp" } }, count: { $sum: 1 } }
$sort:  { _id: 1 }
```
Decode results into `[]ClicksOverTime` where `Date = _id`.

**TopReferrers**: Pipeline:
```
$match:  { code: code }
$group:  { _id: "$referrer", count: { $sum: 1 } }
$sort:   { count: -1 }
$limit:  limit
```
Decode results into `[]TopReferrer` where `Referrer = _id`.

Slow-query logging: record start time before each call; if elapsed > 200ms log at Warn with `code` and `query_type` fields. Lives in repo methods.

### 4. `internal/middleware/logging.go`

Exact same implementation as `shortener-api/internal/middleware/logging.go`:
- `responseWriter` wrapper captures status code
- `Logging(log *slog.Logger) func(http.Handler) http.Handler`
- Logs `method`, `path`, `status`, `latency_ms` at Info level after each request

### 5. `internal/handler/routes.go`

```go
type Config struct {
    Port              string
    StatsWindowDays   int
    TopReferrersLimit int
}

func NewRouter(cfg Config, repo repo.StatsRepository, mongoPing func(context.Context) error, log *slog.Logger) http.Handler
```

Registers:
- `GET /stats/{code}` → `statsHandler(cfg, repo, log)`
- `GET /health`       → `healthHandler(mongoPing, log)`

Wraps mux with `middleware.Logging(log)`.

### 6. `internal/handler/stats.go`

Flow:
1. Extract `code` from `r.PathValue("code")`
2. Launch three concurrent calls via `errgroup.WithContext`:
   - `repo.TotalClicks`
   - `repo.ClicksOverTime(..., cfg.StatsWindowDays)`
   - `repo.TopReferrers(..., cfg.TopReferrersLimit)`
3. Any error → log at Error (raw message never in response) → 500 `{"error":"internal server error"}`
4. `totalClicks == 0` → 404 `{"error":"code not found"}`
5. Zero-fill `clicks_over_time` in the handler: build `map[date]count` from repo results, iterate over all `StatsWindowDays` days from `(today - days + 1)` through today, emitting zero for missing dates
6. Write 200 with full JSON shape

### 7. `internal/handler/health.go`

- Ping MongoDB with 2s timeout
- 200 `{"status":"ok","mongo":"up"}` on success
- 503 `{"status":"degraded","mongo":"down"}` on failure (log Warn, no raw error in response)

### 8. `cmd/stats-api/main.go`

Helpers `mustEnv(key, log)` and `envOr(key, def)` — identical pattern to shortener-api.

Config:
| Env var               | Default                     |
|-----------------------|-----------------------------|
| `PORT`                | `8080`                      |
| `MONGO_URI`           | `mongodb://localhost:27017` |
| `MONGO_DB`            | `analytics`                 |
| `MONGO_COLLECTION`    | `click_events`              |
| `STATS_WINDOW_DAYS`   | `30`                        |
| `TOP_REFERRERS_LIMIT` | `10`                        |

`STATS_WINDOW_DAYS` and `TOP_REFERRERS_LIMIT` parsed via `strconv.Atoi`; log error + exit on invalid values.

Wiring:
1. Create `MongoStatsRepo` → exit on error
2. Build `handler.Config`
3. `http.Server` with `handler.NewRouter(...)`
4. Graceful shutdown on SIGTERM/SIGINT (15s timeout) → `repo.Close`

### 9. `tests/api_test.go`

**fakeRepo** implements `StatsRepository`:
- Fields: `totalClicks int64`, `overTime []repo.ClicksOverTime`, `referrers []repo.TopReferrer`, `err error`
- Returns configured data or injects error on all three methods

Test cases (stdlib `testing` + `net/http/httptest`):
- `TestStats_HappyPath` — seeded data, verify 200, shape, zero-filled day count == `StatsWindowDays`
- `TestStats_CodeNotFound` — `totalClicks=0`, verify 404 `{"error":"code not found"}`
- `TestStats_RepoError` — repo returns error, verify 500, body must not contain raw error text
- `TestHealth_MongoUp` — nil ping func, verify 200 `{"status":"ok","mongo":"up"}`
- `TestHealth_MongoDown` — ping returns error, verify 503 `{"status":"degraded","mongo":"down"}`
- `TestLogging_FieldsPresent` — verify slog output contains method/path/status/latency_ms keys

---

## Verification

```sh
cd stats-api

# Build & vet
go build ./...
go vet ./...

# Tests (no live infrastructure — all fakes)
go test ./...

# Manual smoke test (requires running MongoDB with click_events data)
MONGO_URI=mongodb://localhost:27017 go run ./cmd/stats-api
curl http://localhost:8080/health
curl http://localhost:8080/stats/<known-code>
```
