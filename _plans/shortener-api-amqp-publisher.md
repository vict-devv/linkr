# Plan: shortener-api AMQP Publisher

## Context

`shortener-api` currently redirects `GET /:code` requests but does not emit any events. `analytics-worker` already consumes from exchange `redirects` (topic, durable), routing key `redirect.clicked`. This plan wires the two services together by adding a fire-and-forget AMQP publisher to `shortener-api` that fires a `RedirectEvent` on every successful redirect. Publish failures must never block or fail the redirect response.

Spec: `_specs/shortener-api-amqp-publisher.md`

---

## Implementation Steps (in dependency order)

### 1. `shortener-api/internal/model/event.go` — new file

Add `RedirectEvent` struct:

| Field | Type | JSON key |
|-------|------|----------|
| `Code` | `string` | `"code"` |
| `Timestamp` | `time.Time` | `"timestamp"` |
| `Referrer` | `string` | `"referrer"` |
| `IPHash` | `string` | `"ip_hash"` |

`Timestamp` uses `time.Time` — Go marshals it as RFC3339Nano, which analytics-worker already parses via `time.Parse(time.RFC3339, ...)`. Always assign `time.Now().UTC()` at the call site.

---

### 2. `shortener-api/go.mod` — amend

Add `github.com/rabbitmq/amqp091-go v1.10.0` (same version as analytics-worker).

Run `go mod tidy` from `shortener-api/` to populate `go.sum`.

---

### 3. `shortener-api/internal/publisher/publisher.go` — new file

**`EventPublisher` interface:**
- `Publish(ctx context.Context, event model.RedirectEvent) error`
- `Close() error`

**`AMQPPublisher` struct** — mirrors `analytics-worker/internal/consumer/amqp.go`:
- Fields: `url string`, `log *slog.Logger`, `conn *amqp.Connection`, `ch *amqp.Channel`, `mu sync.RWMutex` (needed because Publish and runLoop race on conn/ch), `stopCh chan struct{}`, `doneCh chan struct{}`, `once sync.Once`
- Constants (identical to analytics-worker): `maxRetries=5`, `baseDelay=1s`, `maxDelay=30s`
- Exchange/routing constants: `exchangeName="redirects"`, `routingKey="redirect.clicked"`

**`NewAMQPPublisher(url string, log *slog.Logger) *AMQPPublisher`**

**`Connect()`** — returns no error (non-fatal; service starts even if RabbitMQ is unreachable):
- Launches `runLoop()` goroutine immediately
- `IsAlive()` returns false until a connection is established; health reports `"amqp":"down"`

**`runLoop()`** goroutine:
1. Call `dial()` — if retries exhausted, log error and return (`defer close(doneCh)`)
2. Register `ch.NotifyClose(...)` (read-lock mu to access ch)
3. `select` on `stopCh` or channel-close notification
4. On disconnect: check `stopCh`, log warning, go back to step 1

**`dial()`** — identical backoff loop to analytics-worker consumer:
- `for attempt := range maxRetries`; first attempt has no delay
- Delay formula: `min(baseDelay * 2^(attempt-1), maxDelay)`
- Respects `stopCh` during backoff sleep (same pattern as consumer)
- Calls `tryConnect()` on each attempt

**`tryConnect()`**:
1. `amqp.Dial(url)` → error: return
2. `conn.Channel()` → error: close conn, return
3. `ch.ExchangeDeclare("redirects", "topic", durable=true, autoDelete=false, internal=false, noWait=false, nil)` → error: close ch+conn, return
4. Write-lock mu, assign `p.conn` and `p.ch`, unlock
5. Log "amqp connected", return nil

**`Publish(ctx, event)`**:
1. `json.Marshal(event)` → error: return
2. Read-lock mu, capture `ch` pointer, unlock immediately (do not hold lock across network I/O)
3. If `ch == nil`: return `errors.New("amqp not connected")`
4. `ch.PublishWithContext(ctx, "redirects", "redirect.clicked", false, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, Body: body})`

**`IsAlive() bool`** — read-lock mu; `conn != nil && !conn.IsClosed()`

**`Close()`** — `once.Do(close(stopCh))`, `<-doneCh`, write-lock mu, close ch then conn, log "amqp publisher stopped"

---

### 4. `shortener-api/internal/handler/redirect.go` — amend

New signature:
```go
func redirectHandler(r repo.URLRepository, c cache.URLCache, ttl time.Duration, pub publisher.EventPublisher, log *slog.Logger) http.HandlerFunc
```

Add unexported helper `hashIP(remoteAddr string) string`:
- `net.SplitHostPort(remoteAddr)` to strip port; on error fallback to raw `remoteAddr`
- `sha256.Sum256([]byte(ip))` → `hex.EncodeToString`

New imports: `context`, `crypto/sha256`, `encoding/hex`, `net`, `time`, `internal/model`, `internal/publisher`.

There are two `http.Redirect` call sites in the current handler (line 21 cache-hit path, line 44 DB-hit path). Before **each** one, launch a goroutine:
```
go func() {
    event := model.RedirectEvent{Code, Timestamp: time.Now().UTC(), Referrer: req.Referer(), IPHash: hashIP(req.RemoteAddr)}
    if err := pub.Publish(context.Background(), event); err != nil {
        log.Warn("failed to publish redirect event", "code", code, "error", err)
    }
}()
http.Redirect(w, req, longURL, http.StatusFound)
```

Use `context.Background()` — **not** `req.Context()` — because the request context is cancelled as soon as the redirect response is written.

---

### 5. `shortener-api/internal/handler/health.go` — amend

New signature:
```go
func healthHandler(dbPing func(context.Context) error, cachePing func(context.Context) error, amqpAlive func() bool, log *slog.Logger) http.HandlerFunc
```

After `wg.Wait()`, call `amqpAlive()` synchronously (pure boolean, no I/O):
```
amqpStatus := "up"
if !amqpAlive() { amqpStatus = "down" }
```

Updated degraded condition: `pgStatus == "down" || rdisStatus == "down" || amqpStatus == "down"`

Add `"amqp": amqpStatus` to the `writeJSON` response map.

---

### 6. `shortener-api/internal/handler/routes.go` — amend

New signature:
```go
func NewRouter(cfg Config, r repo.URLRepository, c cache.URLCache,
    dbPing func(context.Context) error, cachePing func(context.Context) error,
    pub publisher.EventPublisher, amqpAlive func() bool,
    log *slog.Logger) http.Handler
```

Update handler registrations to pass `pub` to `redirectHandler` and `amqpAlive` to `healthHandler`. Add import for `internal/publisher`.

---

### 7. `shortener-api/cmd/shortener-api/main.go` — amend

**New env var** (after existing vars):
```go
amqpURL := envOr("AMQP_URL", "amqp://guest:guest@localhost:5672/")
```

**Create and connect publisher** (after `redisCache := ...`):
```go
pub := publisher.NewAMQPPublisher(amqpURL, log)
pub.Connect()
```

**Update `NewRouter` call** to pass `pub` and `pub.IsAlive`.

**Replace bare `ListenAndServe`** with graceful shutdown (shortener-api has none today):
```
sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
defer stop()

go srv.ListenAndServe() // exit goroutine on ErrServerClosed

<-sigCtx.Done()

shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

srv.Shutdown(shutdownCtx)  // stop accepting requests first
pub.Close()                // then shut down publisher
```

Shutdown ordering: HTTP server before publisher — ensures no new goroutines are launched after the publisher closes.

New imports: `errors`, `os/signal`, `syscall`, `internal/publisher`.

---

### 8. `shortener-api/tests/api_test.go` — amend

**Add `fakePublisher`** with a buffered channel for synchronisation (avoids flaky `time.Sleep`):
```go
type fakePublisher struct {
    returnError error
    publishedCh chan model.RedirectEvent // buffered, cap 1
}
func (f *fakePublisher) Publish(_ context.Context, event model.RedirectEvent) error { ... }
func (f *fakePublisher) Close() error { return nil }
```

**Update `newRouter` and `newRouterWithPings`** — add default `fakePublisher` and `amqpAlive = func() bool { return true }`.

**New test cases:**

| Test | Scenario | Assert |
|------|----------|--------|
| `TestRedirect_PublishesEvent` | GET /:code, code exists | 302; read event from `publishedCh` (with timeout); `event.Code` correct |
| `TestRedirect_PublishErrorDoesNotAffectResponse` | `returnError` set | still 302 |
| `TestRedirect_IPHashStripsPort` | `RemoteAddr = "203.0.113.42:54321"` | `event.IPHash == sha256("203.0.113.42")` hex |
| `TestRedirect_ReferrerEmpty` | no `Referer` header | `event.Referrer == ""` |
| `TestHealth_AMQPDown` | `amqpAlive = func() bool { return false }` | 503, `"amqp":"down"` |
| `TestHealth_AllUp` (update existing) | `amqpAlive = true` | 200, add assert for `"amqp":"up"` |

New imports: `crypto/sha256`, `encoding/hex`, `internal/model`.

---

## Verification

```sh
cd shortener-api
go build ./...    # must compile clean
go vet ./...      # no vet issues
go test ./...     # all tests pass
```

End-to-end: start RabbitMQ + Postgres + Redis, run the service, `GET /:code`, confirm a JSON message on exchange `redirects` / routing key `redirect.clicked` with correct `code`, `timestamp`, `referrer`, and `ip_hash` fields.
