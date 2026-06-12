# Spec for shortener-api External Migrations

branch: claude/feature/shortener-api-external-migrations

## Summary

Replace the inline auto-migration in `shortener-api` with versioned SQL migration files managed
by the `golang-migrate` CLI. The service will no longer apply schema changes on startup; instead,
operators run migrations explicitly before deploying or starting the service.

## Functional Requirements

1. **Remove `Migrate()` from `internal/repo/postgres.go`** — the method that creates the `urls`
   table via `CREATE TABLE IF NOT EXISTS` must be deleted.

2. **Remove the `Migrate()` call from `cmd/shortener-api/main.go`** — the two lines that invoke
   `pgRepo.Migrate(ctx)` and handle its error must be removed.

3. **Create `shortener-api/migrations/` directory** containing four files following the
   `golang-migrate` naming convention:
   - `000001_create_urls_table.up.sql` — creates the `urls` table with columns `code TEXT PRIMARY
KEY`, `long_url TEXT NOT NULL`, and `created_at TIMESTAMPTZ DEFAULT now()`.
   - `000001_create_urls_table.down.sql` — contains `DROP TABLE IF EXISTS urls;`.
   - `000002_seed_urls.up.sql` — inserts a small set of well-known short-code rows for common
     websites (e.g. Google, YouTube, GitHub, Wikipedia, Reddit) using `INSERT INTO urls ... ON
CONFLICT (code) DO NOTHING` so the migration is safe to replay.
   - `000002_seed_urls.down.sql` — deletes the seeded rows by code so the seed can be rolled back
     cleanly without dropping user data.

4. **Update `README.md`** with a "Running migrations" section that documents the `migrate` CLI
   command for local development and explains that migrations must be run before starting the
   service.

## Possible Edge Cases

- A developer starts the service without first running migrations: the service will start
  successfully but fail at runtime when the first SQL query hits the missing `urls` table. The
  README documentation must make the prerequisite explicit.
- The `golang-migrate` CLI is not installed locally: the README should note where to obtain it
  (the project's install page) so developers are not blocked.
- Future schema changes must follow the same versioned file convention (`000003_...`, etc.) — the
  spec establishes this as the ongoing pattern.
- Seed data is development/demo convenience only; it must not be treated as required application
  state. Production operators may choose to skip or reverse the seed migration.

## Acceptance Criteria

- `internal/repo/postgres.go` no longer contains a `Migrate` method.
- `cmd/shortener-api/main.go` no longer calls `pgRepo.Migrate` or imports anything solely for
  migration purposes.
- `shortener-api/migrations/000001_create_urls_table.up.sql` exists and contains the correct DDL
  for the `urls` table.
- `shortener-api/migrations/000001_create_urls_table.down.sql` exists and contains
  `DROP TABLE IF EXISTS urls;`.
- Running `migrate -path shortener-api/migrations -database "$DATABASE_URL" up` against a fresh
  Postgres instance produces a working `urls` table populated with the seed rows.
- Running the down migration rolls back the seed rows, then drops the table cleanly.
- `shortener-api/migrations/000002_seed_urls.up.sql` exists and contains `INSERT ... ON CONFLICT
DO NOTHING` rows for at least Google, YouTube, GitHub, Wikipedia, and Reddit.
- `shortener-api/migrations/000002_seed_urls.down.sql` exists and removes only those seeded codes.
- Re-running `migrate up` on a database that already has the seed rows is a no-op (no duplicate
  key errors).
- `go build ./...` and `go vet ./...` pass inside `shortener-api/`.
- `README.md` documents the migration command under a clear heading.

## Open Questions

- Should `docker-compose.yml` include a one-shot migration container that runs before the
  `shortener-api` service starts, or is a manual step sufficient for local dev? Yes, include a one-shot migration container to seed the data.
- Do we need to document a CI step that runs migrations against the test database before the test suite, or do the existing tests avoid hitting the real schema? The existing test avoid hitting the real schema.

## Testing Guidelines

Create a test file(s) in the ./tests folder for the new feature, and create meaningful tests for
the following cases, without going too heavy:

- The assembled router starts without error when the `urls` table already exists (i.e., the
  service no longer depends on `Migrate()` being called).
- `POST /shorten` and `GET /{code}` still function correctly against a pre-migrated schema,
  confirming that removing `Migrate()` has no side-effects on the happy path.
- `GET /{code}` resolves one of the seeded short codes (e.g. `google`) to its expected long URL,
  confirming the seed data is queryable through the normal redirect flow.

## Docs Update

Check if any document-related files (README.md at the root project directory and/or any `.md`
file inside each service `docs/` folder if any) needs to be updated:

- **README.md** (root or `shortener-api/`) — add a "Running migrations" section with the
  `migrate` CLI command and a note that migrations must be applied before the first service start
  or after any schema change.
