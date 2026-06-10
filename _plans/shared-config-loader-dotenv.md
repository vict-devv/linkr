# Plan: Shared Config Loader with Dotenv File Selection

## Context

All three services (`shortener-api`, `analytics-worker`, `stats-api`) currently load env vars inline in `main.go` using ad-hoc `envOr`/`mustEnv`/helper functions. This feature introduces a shared dotenv loader and per-service typed config structs to centralise that logic, improve environment management, and eliminate duplicated inline helpers.

---

## 1. New `shared/` Go module

**Create `shared/go.mod`:**
```
module github.com/linkr/shared

go 1.22.0

require github.com/joho/godotenv v1.5.1
```

**Create `shared/config/loader.go`:**

```go
package config

func Load(serviceRoot string, log *slog.Logger) error
func resolveEnvFile(env string) (string, error)  // unexported, pure — testable without subprocess
```

- `Load` reads `ENV`, falls back to `ENVIRONMENT`, defaults to `"local"`.
- Calls `resolveEnvFile(env)` to get the filename; if it returns an error, logs fatal and calls `os.Exit(1)`.
- Calls `godotenv.Load(filepath.Join(serviceRoot, filename))`.
- If `os.IsNotExist(err)` → `log.Warn(...)` + return `nil`.
- Any other error → return it (caller treats as fatal).
- No package-level mutable state.

**Create `shared/config/loader_test.go`:**
- `resolveEnvFile("local")` → `.env`
- `resolveEnvFile("dev")` → `.env.dev`
- `resolveEnvFile("prod")` → `.env.prod`
- `resolveEnvFile("unknown")` → returns error (tests mapping without subprocess)
- `Load` with a missing file → returns nil, warns
- `Load` with a valid temp `.env` file → loads the var into the process env

---

## 2. Per-service `internal/config` packages

Create `internal/config/config.go` in each service with a typed `Config` struct and `func Load(log *slog.Logger) Config` (exits on required-field absence, matching existing `mustEnv` behaviour).

**`shortener-api/internal/config/config.go`** — fields:
| Field | Env var | Default | Required |
|---|---|---|---|
| `DatabaseURL` | `DATABASE_URL` | — | yes |
| `RedisURL` | `REDIS_URL` | — | yes |
| `AMQPURL` | `AMQP_URL` | `amqp://guest:guest@localhost:5672/` | no |
| `Host` | `HOST` | `0.0.0.0` | no |
| `Port` | `PORT` | `8080` | no |
| `CacheTTL` | `CACHE_TTL` | `24h` | no (bad parse → warn + default) |

**`analytics-worker/internal/config/config.go`** — fields:
| Field | Env var | Default |
|---|---|---|
| `AMQPURL` | `AMQP_URL` | `amqp://guest:guest@localhost:5672/` |
| `AMQPPrefetch` | `AMQP_PREFETCH` | `10` |
| `MongoURI` | `MONGO_URI` | `mongodb://localhost:27017` |
| `MongoDB` | `MONGO_DB` | `analytics` |
| `HealthPort` | `HEALTH_PORT` | `8081` |
| `ShutdownTimeout` | `SHUTDOWN_TIMEOUT` | `15s` |

**`stats-api/internal/config/config.go`** — fields:
| Field | Env var | Default | Required |
|---|---|---|---|
| `MongoURI` | `MONGO_URI` | `mongodb://localhost:27017` | no |
| `MongoDB` | `MONGO_DB` | `analytics` | no |
| `MongoCollection` | `MONGO_COLLECTION` | `click_events` | no |
| `Port` | `PORT` | `8080` | no |
| `StatsWindowDays` | `STATS_WINDOW_DAYS` | `30` | must be > 0 |
| `TopReferrersLimit` | `TOP_REFERRERS_LIMIT` | `10` | must be > 0 |

Each service also gets `internal/config/config_test.go` covering:
- defaults applied when env vars absent
- required/positive-int validation triggers exit (via subprocess)

---

## 3. Updated `main.go` for each service

Replace the current pattern:
```go
log := slog.New(...)
dbURL := mustEnv("DATABASE_URL", log)
// ...
```
With:
```go
log := slog.New(...)
if err := sharedconfig.Load(".", log); err != nil {
    log.Error("failed to load env file", "error", err)
    os.Exit(1)
}
cfg := config.Load(log)
// use cfg.DatabaseURL, cfg.RedisURL, etc.
```

Remove the now-dead `envOr`, `mustEnv`, `parseDuration`, `parseIntOr`, `parseInt` helpers from all three `main.go` files.

The existing `handler.Config` structs in `shortener-api` and `stats-api` are populated from the new service-level `Config` (they are separate — `handler.Config` is the handler's view of config, while `internal/config.Config` is the full service config).

---

## 4. Service `go.mod` updates

Add to each service's `go.mod`:
```
require github.com/linkr/shared v0.0.0

replace github.com/linkr/shared => ../shared
```

Then `go mod tidy` in each service to pull in `godotenv` as an indirect dep and update `go.sum`.

---

## 5. Dotenv files and `.gitignore`

**Create per-service `.env.example`** (one per service, committed) listing all env vars with example/safe values — no real secrets. The root `.gitignore` already has `.env*` which also matches `.env.example`, so add a negation after it:
```
!.env.example
```

**Create per-service `.env`, `.env.dev`, `.env.prod`** with appropriate local/dev/prod values. These stay gitignored (by the existing `.env*` rule).

---

## Critical files

| File | Action |
|---|---|
| `shared/go.mod` | create |
| `shared/config/loader.go` | create |
| `shared/config/loader_test.go` | create |
| `shortener-api/go.mod` | add require+replace |
| `shortener-api/internal/config/config.go` | create |
| `shortener-api/internal/config/config_test.go` | create |
| `shortener-api/cmd/shortener-api/main.go` | update |
| `analytics-worker/go.mod` | add require+replace |
| `analytics-worker/internal/config/config.go` | create |
| `analytics-worker/internal/config/config_test.go` | create |
| `analytics-worker/cmd/analytics-worker/main.go` | update |
| `stats-api/go.mod` | add require+replace |
| `stats-api/internal/config/config.go` | create |
| `stats-api/internal/config/config_test.go` | create |
| `stats-api/cmd/stats-api/main.go` | update |
| `.gitignore` | add `!.env.example` |
| `{service}/.env.example` (×3) | create |
| `{service}/.env`, `.env.dev`, `.env.prod` (×9) | create (gitignored) |

---

## Verification

1. `cd shared && go test ./...` — loader tests pass.
2. `cd shortener-api && go build ./... && go test ./...` — no compile errors; existing integration tests pass.
3. `cd analytics-worker && go build ./... && go test ./...` — same.
4. `cd stats-api && go build ./... && go test ./...` — same.
5. Run `ENV=unknown go run ./cmd/shortener-api` — process exits with `"unknown ENV value: unknown; expected local|dev|prod"`.
6. Remove `.env` then run a service — process starts (warning logged), vars injected directly.
7. `git status` confirms only `.env.example` files are tracked, not `.env`/`.env.dev`/`.env.prod`.
