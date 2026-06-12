# Plan: Chi Router Swap

## Context

The three linkr services (`shortener-api`, `analytics-worker`, `stats-api`) all use the Go 1.22 stdlib `net/http` ServeMux. This refactor replaces it with `github.com/go-chi/chi/v5` across all three services — no behaviour changes, no new endpoints, no new middleware. The goal is to standardise on chi as the router foundation for future work.

## Affected Files (8 total)

| File | Change |
|------|--------|
| `shortener-api/internal/handler/routes.go` | Swap mux → chi.Router; wire middleware via `r.Use()` |
| `shortener-api/internal/handler/redirect.go` | `r.PathValue("code")` → `chi.URLParam(r, "code")` |
| `shortener-api/go.mod` | Add `github.com/go-chi/chi/v5` |
| `analytics-worker/internal/handler/health.go` | Swap mux → chi.Router |
| `analytics-worker/go.mod` | Add `github.com/go-chi/chi/v5` |
| `stats-api/internal/handler/routes.go` | Swap mux → chi.Router; wire middleware via `r.Use()` |
| `stats-api/internal/handler/stats.go` | `r.PathValue("code")` → `chi.URLParam(r, "code")` |
| `stats-api/go.mod` | Add `github.com/go-chi/chi/v5` |

`go.sum` files are updated automatically by `go mod tidy` — do not edit them manually.

---

## Implementation Steps

### Step 1 — Add the chi dependency to each service

Run in each service directory (`shortener-api/`, `analytics-worker/`, `stats-api/`):

```
go get github.com/go-chi/chi/v5
go mod tidy
```

This populates `go.mod` and `go.sum`. Do all three before touching any `.go` files so the import is resolvable when editing.

---

### Step 2 — shortener-api

**`internal/handler/routes.go`**

Current pattern:
```go
mux := http.NewServeMux()
mux.HandleFunc("GET /health", healthHandler(...))
mux.HandleFunc("POST /shorten", shortenHandler(...))
mux.HandleFunc("GET /{code}", redirectHandler(...))
return middleware.Logging(log)(mux)
```

New pattern:
```go
r := chi.NewRouter()
r.Use(middleware.Logging(log))
r.Get("/health", healthHandler(...))
r.Post("/shorten", shortenHandler(...))
r.Get("/{code}", redirectHandler(...))
return r
```

- `middleware.Logging(log)` already returns `func(http.Handler) http.Handler` — the exact type `r.Use()` expects. No changes to the middleware file.
- `NewRouter` signature and return type (`http.Handler`) are unchanged.
- Add `"github.com/go-chi/chi/v5"` import; remove `"net/http"` if it becomes unused (it won't — handlers still use `http.ResponseWriter` / `*http.Request`).

**`internal/handler/redirect.go`**

Replace:
```go
code := req.PathValue("code")
```
With:
```go
code := chi.URLParam(req, "code")
```

Add `"github.com/go-chi/chi/v5"` import to this file.

---

### Step 3 — analytics-worker

**`internal/handler/health.go`**

Current pattern:
```go
mux := http.NewServeMux()
mux.HandleFunc("GET /health", healthHandler(amqpAlive, mongoPing, log))
return &http.Server{Addr: ":" + port, Handler: mux}
```

New pattern:
```go
r := chi.NewRouter()
r.Get("/health", healthHandler(amqpAlive, mongoPing, log))
return &http.Server{Addr: ":" + port, Handler: r}
```

- No middleware to wire. `NewHealthServer` signature and return type (`*http.Server`) are unchanged.

---

### Step 4 — stats-api

**`internal/handler/routes.go`**

Current pattern:
```go
mux := http.NewServeMux()
mux.HandleFunc("GET /stats/{code}", statsHandler(cfg, r, log))
mux.HandleFunc("GET /health", healthHandler(mongoPing, log))
return middleware.Logging(log)(mux)
```

New pattern:
```go
router := chi.NewRouter()
router.Use(middleware.Logging(log))
router.Get("/stats/{code}", statsHandler(cfg, r, log))
router.Get("/health", healthHandler(mongoPing, log))
return router
```

Note: the local variable `r` is already used for `repo.StatsRepository` — name the chi router `router` to avoid shadowing.

**`internal/handler/stats.go`**

Replace:
```go
code := req.PathValue("code")
```
With:
```go
code := chi.URLParam(req, "code")
```

Add `"github.com/go-chi/chi/v5"` import to this file.

---

## Key Invariants to Preserve

- `NewRouter` / `NewHealthServer` function signatures must not change (tests call them directly).
- Route path patterns use the same `{code}` syntax in chi as in stdlib 1.22 mux — no pattern string changes needed.
- `chi.Router` implements `http.Handler`, so all call sites that accept `http.Handler` continue to work without modification.
- Chi's trie-based router gives static paths (`/health`, `/shorten`) priority over parameterised paths (`/{code}`) regardless of registration order — same semantics as stdlib mux.
- Do not import `github.com/go-chi/chi/v5/middleware` — only the core `chi` package.

---

## Verification

Run the following in each service directory after all changes:

```sh
go build ./...       # must succeed
go vet ./...         # must report no issues
go test ./...        # all existing tests must pass, no modifications to test files
```

No test files should be modified. The test helpers (`newRouterFull`, `newRouter`, `newHealthServer`) call the same `handler.NewRouter` / `handler.NewHealthServer` functions and pass their results as `http.Handler` / `*http.Server` — the chi types satisfy these interfaces transparently.
