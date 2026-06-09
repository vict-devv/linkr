# Spec for Stats API Service

branch: claude/feature/stats-api-service

## Summary

A new read-only Go HTTP microservice (`stats-api`) that exposes aggregated click analytics for short codes stored in MongoDB by the `analytics-worker` service. It is intended to be consumed by a React dashboard. The service follows the same internal conventions as `shortener-api` and `analytics-worker` (stdlib `net/http`, `log/slog`, interface-driven repository layer, black-box tests).

## Functional Requirements

### Service layout

The service lives at `stats-api/` as an independent Go module and follows the same directory structure as the existing services:

```
stats-api/
  cmd/stats-api/main.go
  internal/handler/
  internal/repo/
  internal/middleware/
  tests/
```

### Endpoints

#### `GET /stats/{code}`

Returns aggregated analytics for the given short code.

Response body (200):
```json
{
  "code": "<short_code>",
  "total_clicks": 1042,
  "clicks_over_time": [
    { "date": "2026-06-01", "count": 120 },
    { "date": "2026-06-02", "count": 98 }
  ],
  "top_referrers": [
    { "referrer": "https://twitter.com", "count": 310 },
    { "referrer": "", "count": 205 }
  ]
}
```

- `clicks_over_time`: daily bucketed counts for the last `STATS_WINDOW_DAYS` days (default 30), in ascending date order. Days with zero clicks must still appear so the dashboard can render a continuous time series. Zero-filling is done in the handler after the repo call, not in the aggregation pipeline.
- `top_referrers`: top `TOP_REFERRERS_LIMIT` (default 10) referrers by count, descending. An empty string referrer is a valid value (direct/unknown traffic).
- The three repo calls (`TotalClicks`, `ClicksOverTime`, `TopReferrers`) are independent and must be executed concurrently using `errgroup.WithContext`.
- Returns `404 { "error": "code not found" }` if no documents exist for the code.
- All three aggregations run as separate MongoDB queries — no `$facet`.

#### `GET /health`

Returns the service and MongoDB connectivity status.

- `200 { "status": "ok", "mongo": "up" }` when MongoDB is reachable.
- `503 { "status": "degraded", "mongo": "down" }` when MongoDB ping fails.

### Repository layer (`internal/repo`)

#### Interface

```go
type StatsRepository interface {
    TotalClicks(ctx context.Context, code string) (int64, error)
    ClicksOverTime(ctx context.Context, code string, days int) ([]ClicksOverTime, error)
    TopReferrers(ctx context.Context, code string, limit int) ([]TopReferrer, error)
}
```

Supporting types:

```go
type ClicksOverTime struct {
    Date  string `json:"date"`  // "YYYY-MM-DD"
    Count int64  `json:"count"`
}

type TopReferrer struct {
    Referrer string `json:"referrer"`
    Count    int64  `json:"count"`
}
```

#### Concrete implementation: `MongoStatsRepo`

Backed by `go.mongodb.org/mongo-driver/v2`.

- `TotalClicks`: `CountDocuments` with filter `{ code: <code> }`.
- `ClicksOverTime`: aggregation pipeline — `$match` on `code` and `timestamp` within the date range, `$group` by `$dateToString` of `timestamp` (format `"%Y-%m-%d"`), `$sort` by `_id` ascending.
- `TopReferrers`: aggregation pipeline — `$match` on `code`, `$group` by `referrer` with `$sum: 1`, `$sort` descending, `$limit`.

The handler never imports or touches the MongoDB driver directly; all access goes through the `StatsRepository` interface.

**Required index (manual/migration step — do not create in app code):**
```
{ code: 1, timestamp: -1 }
```
This compound index must exist on the `clicks` collection before the service handles production traffic.

### Logging

- JSON structured logging via `log/slog` with `slog.NewJSONHandler(os.Stdout, nil)`.
- Request middleware logs: method, path, HTTP status code, and request latency.
- Slow queries (latency > 200 ms) are logged at `Warn` level with `code` and `query_type` fields.
- MongoDB errors are logged at `Error` level. Raw error messages must never be returned in HTTP responses.

### Configuration (env vars)

| Variable              | Default                       | Description                              |
|-----------------------|-------------------------------|------------------------------------------|
| `PORT`                | `8080`                        | HTTP listen port                         |
| `MONGO_URI`           | `mongodb://localhost:27017`   | MongoDB connection string                |
| `MONGO_DB`            | `analytics`                   | Database name                            |
| `MONGO_COLLECTION`    | `clicks`                      | Collection name                          |
| `STATS_WINDOW_DAYS`   | `30`                          | Lookback window for clicks-over-time     |
| `TOP_REFERRERS_LIMIT` | `10`                          | Max referrers returned                   |

### Constraints

- Standard library `net/http` only — no external router or framework.
- No writes to MongoDB under any circumstances.
- Error responses must be `{ "error": "<message>" }` JSON with the correct HTTP status code.
- Do not add a `docker-compose.yml` — MongoDB is already declared in the root compose used by `analytics-worker`.

## Possible Edge Cases

- Short code exists in `shortener-api` but has zero click events in MongoDB — must return 404, not an empty stats object.
- `clicks_over_time` aggregation returns fewer days than the window (sparse data) — zero-fill all missing days in the handler.
- MongoDB is temporarily unreachable while the service is running — `/health` returns 503; `/stats/{code}` returns 500 with a generic error message (no raw driver error leaked).
- `STATS_WINDOW_DAYS` or `TOP_REFERRERS_LIMIT` set to 0 or negative values — clamp or reject at startup with a clear log message.
- Very high click volumes causing slow aggregation queries — slow-query logging at Warn level provides observability without affecting the response.
- Concurrent repo calls with `errgroup`: if one fails, the others are cancelled and the handler returns 500.

## Acceptance Criteria

- `GET /stats/{code}` returns the correct JSON shape with all three fields populated for a code that has click data.
- `clicks_over_time` always contains exactly `STATS_WINDOW_DAYS` entries, with zero-filled gaps for days with no clicks.
- `top_referrers` returns at most `TOP_REFERRERS_LIMIT` entries, sorted by count descending.
- `GET /stats/{code}` returns `404 { "error": "code not found" }` for a code with no click documents.
- `GET /health` returns 200 when MongoDB is reachable and 503 when it is not.
- All three repo calls for a single `/stats/{code}` request are executed concurrently.
- No raw MongoDB error messages appear in any HTTP response body.
- All config env vars are respected, including defaults when unset.
- The service builds with `go build ./...` and passes `go vet ./...` with no errors.

## Open Questions

- Should `/stats/{code}` validate that the code exists in `shortener-api`'s Postgres, or rely solely on MongoDB click data? (Current spec: rely on MongoDB only — 0 documents = 404.)
- Should there be a per-request timeout for MongoDB queries, and if so what value? Consider making it a configurable env var (`MONGO_QUERY_TIMEOUT`, e.g. 5s default).
- Is CORS required on `/stats/{code}` for direct browser access from the React dashboard, or will requests be proxied?

## Testing Guidelines

Create test files under `stats-api/tests/` using only stdlib `testing` and hand-written fakes (no mock libraries), consistent with the existing services.

- A fake `StatsRepository` implementation that returns configurable canned data or errors.
- `GET /stats/{code}` — happy path: verify response shape, correct totals, and that `clicks_over_time` has exactly `STATS_WINDOW_DAYS` entries with zero-fills.
- `GET /stats/{code}` — code not found: verify 404 and `{ "error": "code not found" }`.
- `GET /stats/{code}` — repo error: verify 500 and that the response body does not contain raw error text.
- `GET /health` — MongoDB up: verify 200 and `{ "status": "ok", "mongo": "up" }`.
- `GET /health` — MongoDB down: verify 503 and `{ "status": "degraded", "mongo": "down" }`.
- Middleware: verify that requests are logged (method, path, status, latency fields present in output).
