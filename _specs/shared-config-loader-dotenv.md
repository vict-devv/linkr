# Spec for Shared Config Loader with Dotenv File Selection

branch: claude/feature/shared-config-loader-dotenv

## Summary

Introduce a shared `config` package consumed by all three services (`shortener-api`, `analytics-worker`, `stats-api`) that selects and loads the correct `.env` file based on the `ENV` environment variable, then exposes a typed config struct per service. Each service's `main.go` is updated to call the loader before reading any environment variables, and the local inline `envOr`/`mustEnv` helpers are replaced by struct field reads.

## Functional Requirements

### Shared loader (`shared/config/loader.go`)

- The package lives at path `shared/config` within the monorepo. Its Go module path must be resolvable by all three services (via `replace` directive or as a separate module added to each service's `go.mod`).
- Expose a single exported function:
  ```
  func Load(serviceRoot string) error
  ```
  `serviceRoot` is the directory in which to look for the `.env` file. Each service passes `"."` so each loads from its own root directory, not the monorepo root.
- `Load` reads `ENV` first; if empty, reads `ENVIRONMENT`; if still empty, defaults to `"local"`.
- `Load` maps the resolved value to a filename:
  - `"local"` → `.env`
  - `"dev"` → `.env.dev`
  - `"prod"` → `.env.prod`
  - any other value → log a fatal message in the format `"unknown ENV value: <value>; expected local|dev|prod"` and call `os.Exit(1)`.
- `Load` calls `godotenv.Load(<path>)` where `<path>` is `filepath.Join(serviceRoot, filename)`.
- If the file does not exist (`os.IsNotExist`), log a warning and return `nil` — this permits production deployments where env vars are injected directly without a `.env` file.
- Any other error from `godotenv.Load` is returned to the caller (who should treat it as fatal).
- `Load` must complete before any `os.Getenv` call in `main.go`.

### Per-service config structs

Each service gets a typed `Config` struct in its own `internal/config` package (e.g. `shortener-api/internal/config/config.go`). The struct holds all values previously read ad-hoc via `envOr`/`mustEnv`.

**shortener-api** fields:

- `DatabaseURL` string — required, no default (`DATABASE_URL`)
- `RedisURL` string — required, no default (`REDIS_URL`)
- `AMQPURL` string — default `"amqp://guest:guest@localhost:5672/"` (`AMQP_URL`)
- `Host` string — default `"0.0.0.0"` (`HOST`)
- `Port` string — default `"8080"` (`PORT`)
- `CacheTTL` `time.Duration` — default `24h` (`CACHE_TTL`)

**analytics-worker** fields:

- `AMQPURL` string — default `"amqp://guest:guest@localhost:5672/"` (`AMQP_URL`)
- `AMQPPrefetch` int — default `10` (`AMQP_PREFETCH`)
- `MongoURI` string — default `"mongodb://localhost:27017"` (`MONGO_URI`)
- `MongoDB` string — default `"analytics"` (`MONGO_DB`)
- `HealthPort` string — default `"8081"` (`HEALTH_PORT`)
- `ShutdownTimeout` `time.Duration` — default `15s` (`SHUTDOWN_TIMEOUT`)

**stats-api** fields:

- `MongoURI` string — default `"mongodb://localhost:27017"` (`MONGO_URI`)
- `MongoDB` string — default `"analytics"` (`MONGO_DB`)
- `MongoCollection` string — default `"click_events"` (`MONGO_COLLECTION`)
- `Port` string — default `"8080"` (`PORT`)
- `StatsWindowDays` int — required, must be > 0 (`STATS_WINDOW_DAYS`, default `"30"`)
- `TopReferrersLimit` int — required, must be > 0 (`TOP_REFERRERS_LIMIT`, default `"10"`)

Each struct has a `Load() (Config, error)` constructor (or equivalent) that reads from `os.Getenv` after the dotenv file has already been loaded. Required fields with no default should cause a fatal error if empty.

### `.env` files per service

Each service gets three dotenv files in its own root directory:

- `.env` (local)
- `.env.dev`
- `.env.prod`

Each file contains the full set of env vars for that service with values appropriate for the target environment (local defaults, dev endpoints, prod endpoints). All three files must be committed to the repository. `.env` is for local development and may contain plain-text secrets; add it to `.gitignore` if secrets are present, and commit only `.env.example` instead.

### Updated `main.go` for each service

1. Call `config.Load(".")` as the very first operation after creating the logger (or before).
2. Call the service-level `Config` constructor to produce a populated struct.
3. Replace all subsequent `envOr`/`mustEnv`/`os.Getenv` calls with reads from the struct.
4. Remove the now-redundant `envOr`/`mustEnv` helper functions from `main.go`.

## Possible Edge Cases

- `ENV` is set to a value not in the allowed set — must exit with a clear message, not silently fall back.
- `.env` file exists but is malformed — `godotenv.Load` returns an error; `main.go` should treat this as fatal.
- `.env` file is missing in production — loader logs a warning and continues; vars are expected to be injected by the runtime.
- A required field (e.g. `DATABASE_URL`) is absent from both the `.env` file and the process environment — the per-service config constructor must exit with a meaningful message identifying the missing key.
- `CACHE_TTL` or `SHUTDOWN_TIMEOUT` is set to an unparseable duration string — log and fall back to the default (current behaviour retained).
- `serviceRoot` is a relative path — `filepath.Join` handles this correctly; callers always pass `"."`.
- Multiple services running in the same process (not applicable here, but the package must not use package-level mutable state that would break concurrent calls).

## Acceptance Criteria

- `shared/config.Load(".")` selects `.env`, `.env.dev`, or `.env.prod` correctly based on `ENV`/`ENVIRONMENT`.
- Setting `ENV=unknown` causes `os.Exit(1)` with the prescribed message.
- When the selected `.env` file is absent, the process starts without error (only a warning is logged).
- Each service's `main.go` contains no inline `envOr`/`mustEnv`/direct-`os.Getenv` calls after the config struct is constructed.
- All three services still start and serve requests correctly after the refactor (verified by existing integration tests passing).
- `.env` files exist for all three services in all three environments.

## Open Questions

- Should the shared `config` package live in a `shared/` directory at the monorepo root as its own `go.mod`, or be inlined into one service and imported via `replace`? Decision will affect how the other two services reference it. It should have its own `go.mod`, the way it should be imported in the project must be consistent.
- Should `.env` (local) be committed or gitignored? If secrets appear there, gitignore and ship `.env.example` instead. All the envs files should be ignored except for the .env.example, this last one can be shipped for reference.
- Should `Load` accept a `*slog.Logger` to emit the "file not found" warning via structured logging, or use `log` stdlib for simplicity? All the logs should go through slog for consistency.

## Testing Guidelines

Create test files in the `tests/` folder (or `shared/config/` package tests) for the following cases, without going too heavy:

- `Load` with `ENV=local` loads the correct filename.
- `Load` with `ENV=dev` loads the correct filename.
- `Load` with `ENV=prod` loads the correct filename.
- `Load` with an unknown `ENV` value calls `os.Exit(1)` (test using a subprocess or by extracting the mapping logic into a pure, testable function).
- `Load` when the target file does not exist returns `nil` and logs a warning (no fatal).
- Per-service config constructor returns an error (or exits) when a required field is missing from the environment.
- Per-service config constructor populates defaults correctly when optional vars are absent.
