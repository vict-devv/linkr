# Plan: API Key Auth Middleware

## Context

Both `shortener-api` and `stats-api` currently have no authentication on any routes. The feature adds Bearer token auth to protect write/analytics endpoints (`POST /shorten` and `GET /stats/{code}`) while leaving health checks and the redirect (`GET /{code}`) public. Each service reads its key from `API_KEY` via a fatal env-var loader so misconfiguration fails fast at startup.

---

## Key findings from exploration

- Both services use `chi/v5` and apply middleware globally with `router.Use()`. Per-route middleware uses `router.With(mw).Method(...)`.
- `shortener-api/internal/config/config.go` already has `mustEnv(key)` — logs + `os.Exit(1)` on missing.
- `stats-api/internal/config/config.go` does **not** have `mustEnv`; it only has `envOr` (default fallback) and `parsePositiveInt`. A `mustEnv` helper must be added there too.
- Both services have a single existing middleware file: `internal/middleware/logging.go`.
- Test pattern: hand-written fakes implementing repo interfaces + `httptest.NewRecorder` + stdlib `testing`. No mock framework. Tests in `tests/api_test.go`.

---

## Implementation steps

### 1. `internal/middleware/auth.go` (identical logic in both services)

New file in `shortener-api/internal/middleware/` and `stats-api/internal/middleware/`.

- `APIKeyAuth(key string) func(http.Handler) http.Handler`
- Extract `Authorization` header. If empty or doesn't start with `"Bearer "` → write `401 {"error":"unauthorized"}` and return.
- Strip the `"Bearer "` prefix to get the token. Use `crypto/subtle.ConstantTimeCompare([]byte(token), []byte(key)) != 1` → write `403 {"error":"forbidden"}` and return.
- Otherwise call `next.ServeHTTP(w, r)`.
- Use `encoding/json` for the error response body (consistent with existing handlers).

Edge cases handled by this logic:
- Header absent → no `"Bearer "` prefix → 401.
- Header is just `"Bearer"` (no space/token) → no prefix match → 401.
- Correct prefix, empty token → ConstantTimeCompare fails → 403.

### 2. `internal/config/config.go` — both services

**shortener-api:** Add `APIKey string` to `Config`; populate with `mustEnv("API_KEY")` in `Load()`. No other changes needed.

**stats-api:** Add `mustEnv(key string, log *slog.Logger) string` helper (same pattern as shortener-api: log error + `os.Exit(1)`). Add `APIKey string` to `Config`; populate with `mustEnv("API_KEY", log)` in `Load()`.

### 3. `internal/handler/routes.go` — both services

Apply middleware per-route using chi's `router.With()` so public routes are unaffected:

**shortener-api** — protect only `POST /shorten`:
```
router.With(middleware.APIKeyAuth(cfg.APIKey)).Post("/shorten", shortenHandler(...))
router.Get("/health", healthHandler(...))
router.Get("/{code}", redirectHandler(...))
```

**stats-api** — protect only `GET /stats/{code}`:
```
router.With(middleware.APIKeyAuth(cfg.APIKey)).Get("/stats/{code}", statsHandler(...))
router.Get("/health", healthHandler(...))
```

The `NewRouter` signature in each service already receives `cfg` (as a `Config` value), so `cfg.APIKey` is available without changing function signatures.

### 4. `.env` files — both services

Add `API_KEY=<value>` to `.env.example`, `.env.dev`, and `.env.prod` in both `shortener-api/` and `stats-api/`. Use a placeholder like `changeme` in example/dev and leave blank in prod (consistent with how `DATABASE_URL` etc. are handled there).

### 5. Tests — both services

Add to `tests/api_test.go` in each service (or a new `tests/auth_test.go` — either works, prefer extending the existing file to keep `newRouter` helper in scope).

**shortener-api** — test against `POST /shorten`:
- Valid `Authorization: Bearer testkey` → 201 (passes through to fake repo).
- No `Authorization` header → 401 `{"error":"unauthorized"}`.
- Wrong scheme `Authorization: Token testkey` → 401 `{"error":"unauthorized"}`.
- Wrong key `Authorization: Bearer wrongkey` → 403 `{"error":"forbidden"}`.
- `GET /health` with no auth header → 200 (public route unaffected).
- `GET /{code}` with no auth header → normal redirect/404 (public route unaffected).

**stats-api** — test against `GET /stats/{code}`:
- Valid key → 200 with stats body.
- No auth header → 401.
- Wrong scheme → 401.
- Wrong key → 403.
- `GET /health` with no auth header → 200 (public route unaffected).

Each test constructs the router with a known test key (e.g., `"testkey"`) by passing it through the `Config` struct. No changes to `newRouter` helper are needed — just pass a config with `APIKey` set.

---

## Files to create / modify

| File | Action |
|---|---|
| `shortener-api/internal/middleware/auth.go` | Create |
| `stats-api/internal/middleware/auth.go` | Create |
| `shortener-api/internal/config/config.go` | Modify — add `APIKey`, use `mustEnv` |
| `stats-api/internal/config/config.go` | Modify — add `mustEnv` helper + `APIKey` |
| `shortener-api/internal/handler/routes.go` | Modify — wire `router.With(APIKeyAuth)` on `/shorten` |
| `stats-api/internal/handler/routes.go` | Modify — wire `router.With(APIKeyAuth)` on `/stats/{code}` |
| `shortener-api/.env.example`, `.env.dev`, `.env.prod` | Modify — add `API_KEY` |
| `stats-api/.env.example`, `.env.dev`, `.env.prod` | Modify — add `API_KEY` |
| `shortener-api/tests/api_test.go` | Modify — add auth test cases |
| `stats-api/tests/api_test.go` | Modify — add auth test cases |

---

## Verification

```sh
# In shortener-api/
go build ./...
go vet ./...
go test ./tests/...

# In stats-api/
go build ./...
go vet ./...
go test ./tests/...
```

All new and existing tests must pass. The auth tests exercise the 401/403/pass-through paths without a live database or broker (fakes handle the repo/cache/publisher layer).
