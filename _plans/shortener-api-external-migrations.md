# Plan: shortener-api External Migrations

## Context

`shortener-api` currently runs `CREATE TABLE IF NOT EXISTS urls (...)` inline at startup via
`PostgresRepo.Migrate()`. This couples schema management to the application binary, prevents
controlled rollbacks, and is inconsistent with how operators normally manage database schemas.
The goal is to externalise the DDL into versioned `golang-migrate` SQL files and add a seed
migration with well-known short codes, while adding a one-shot `migrate` service to
`docker-compose.yaml` so `docker compose up` still works out of the box.

---

## Files to Modify

### 1. `shortener-api/internal/repo/postgres.go`
Remove the `Migrate()` method entirely (lines 28-37). No other change — `Save`, `Find`, `Ping`,
`IsUniqueViolation` are untouched.

### 2. `shortener-api/cmd/shortener-api/main.go`
Remove the two-line block that calls `pgRepo.Migrate(ctx)` and handles its error (lines 38-41).
No other change — connection, cache, publisher, router, and server wiring are untouched.

### 3. `docker-compose.yaml`
Add a `shortener-api-migrate` one-shot service between the infrastructure services and
`shortener-api`. It uses the official `migrate/migrate` image, mounts the migrations directory,
and runs against the same Postgres instance:

```yaml
shortener-api-migrate:
  image: migrate/migrate
  volumes:
    - ./shortener-api/migrations:/migrations
  command: ["-path", "/migrations", "-database", "postgres://local:local@postgres:5432/linkr?sslmode=disable", "up"]
  depends_on:
    postgres:
      condition: service_healthy
  networks:
    - linkr
  restart: no
```

Update `shortener-api.depends_on` to add:
```yaml
shortener-api-migrate:
  condition: service_completed_successfully
```

### 4. `README.md`
Add a "Running migrations" subsection under the local development section documenting:
- Install the `migrate` CLI (link to `golang-migrate` releases)
- The command: `migrate -path shortener-api/migrations -database "$DATABASE_URL" up`
- A note that migrations must be applied before first start and after any schema change
- A note that `docker compose up` runs them automatically via the `shortener-api-migrate` service

---

## Files to Create

### `shortener-api/migrations/000001_create_urls_table.up.sql`
```sql
CREATE TABLE IF NOT EXISTS urls (
    code       TEXT PRIMARY KEY,
    long_url   TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);
```

### `shortener-api/migrations/000001_create_urls_table.down.sql`
```sql
DROP TABLE IF EXISTS urls;
```

### `shortener-api/migrations/000002_seed_urls.up.sql`
Insert well-known short codes using `ON CONFLICT (code) DO NOTHING` for idempotency:
- `google` → `https://www.google.com`
- `youtube` → `https://www.youtube.com`
- `github` → `https://www.github.com`
- `wikipedia` → `https://www.wikipedia.org`
- `reddit` → `https://www.reddit.com`

### `shortener-api/migrations/000002_seed_urls.down.sql`
```sql
DELETE FROM urls WHERE code IN ('google', 'youtube', 'github', 'wikipedia', 'reddit');
```

---

## Tests

The existing test suite in `shortener-api/tests/api_test.go` uses hand-written fakes and never
calls `Migrate()`, so all existing tests will continue to pass after the removal.

Add one new test to `api_test.go` that verifies a seeded code resolves correctly through the
redirect handler. Pre-populate `fakeRepo.urls` directly (the map is exported-accessible through
`newFakeRepo`) with `"google": "https://www.google.com"`, then assert `GET /google` returns
`302` to `https://www.google.com`. This confirms the redirect path works with seed data without
touching a real database.

---

## Verification

1. `cd shortener-api && go build ./... && go vet ./...` — must pass
2. `cd shortener-api && go test ./...` — all tests including the new one must pass
3. `docker compose up --build` — the `shortener-api-migrate` service must exit 0 before
   `shortener-api` starts
4. `curl -X POST localhost:8080/shorten -d '{"url":"https://example.com"}'` then
   `curl -L localhost:8080/<code>` — happy path still works
5. `curl -I localhost:8080/google` — must return `302` to `https://www.google.com` (seed data)
6. Manual rollback: `migrate -path shortener-api/migrations -database "$DATABASE_URL" down 1`
   removes seed rows; `down 2` drops the table
