# Spec for Chi Router Swap

branch: claude/feature/chi-router-swap

## Summary

Replace the Go stdlib `net/http` ServeMux with `github.com/go-chi/chi/v5` across all three services in the linkr monorepo (`shortener-api`, `analytics-worker`, `stats-api`). This is a pure mechanical refactor — no new endpoints, no behaviour changes, no new middleware.

## Functional Requirements

### shortener-api

- Replace the `net/http` `ServeMux` in `internal/handler/routes.go` with a `chi.Router`.
- Re-register all existing routes using chi method helpers:
  - `POST /shorten`
  - `GET /{code}`
  - `GET /health`
- Attach the existing request-logging middleware to the chi router via `r.Use(...)`, preserving the same structured `log/slog` output.
- Replace all `r.PathValue("code")` calls with `chi.URLParam(r, "code")`.
- Add `github.com/go-chi/chi/v5` to `shortener-api/go.mod` and `go.sum`.

### analytics-worker

- Replace the `net/http` `ServeMux` used by the internal health server with a `chi.Router`.
- Re-register `GET /health` using chi.
- Add `github.com/go-chi/chi/v5` to `analytics-worker/go.mod` and `go.sum`.

### stats-api

- Replace the `net/http` `ServeMux` in `internal/handler/routes.go` with a `chi.Router`.
- Re-register all existing routes using chi method helpers:
  - `GET /stats/{code}`
  - `GET /health`
- Attach any existing middleware to the chi router via `r.Use(...)`.
- Replace all `r.PathValue("code")` calls with `chi.URLParam(r, "code")`.
- Add `github.com/go-chi/chi/v5` to `stats-api/go.mod` and `go.sum`.

## Possible Edge Cases

- Chi uses `{code}` path parameter syntax which matches the stdlib 1.22 mux — verify the parameter name is consistent everywhere it is read.
- The `NewRouter` function signature in each service must remain unchanged so that `main.go` and test files require no modification.
- The health server in `analytics-worker` is started independently of the AMQP consumer loop; ensure the chi router is initialised before `ListenAndServe` is called.
- `go mod tidy` must be run in each service directory after adding the chi dependency to avoid stale `go.sum` entries.

## Acceptance Criteria

- All existing tests in each service pass without any modification to test files.
- `go build ./...` succeeds in every service directory.
- `go vet ./...` reports no issues in every service directory.
- No chi middleware packages (e.g. `chi/middleware`) are imported — only the core `chi` package.
- HTTP behaviour is identical to before the swap: same status codes, same response bodies, same path-parameter extraction.
- `github.com/go-chi/chi/v5` appears in `go.mod` and `go.sum` for each of the three services.

## Open Questions

- None — scope and constraints are fully defined.

## Testing Guidelines

No new test files are required. The existing test suites in each service's `tests/` directory cover the assembled router and must continue to pass as-is. After making changes, run the following in each service directory to confirm:

- `go test ./...` — all tests green
- `go build ./...` — clean build
- `go vet ./...` — no vet errors
