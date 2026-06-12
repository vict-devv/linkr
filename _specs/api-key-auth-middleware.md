# Spec for API Key Auth Middleware

branch: claude/feature/api-key-auth-middleware

## Summary

Add Bearer token authentication to protect write/read endpoints in both `shortener-api` and `stats-api`. Each service independently validates an API key supplied via the `Authorization: Bearer <token>` header. Public routes (health checks and redirect resolution) remain unauthenticated. Keys are configured per-service through their own `API_KEY` environment variables and loaded using the existing `mustEnv` pattern so a missing key is fatal on startup.

## Functional Requirements

- A new `APIKeyAuth(key string) func(http.Handler) http.Handler` middleware lives in `internal/middleware/auth.go` in both `shortener-api` and `stats-api`.
- The middleware reads the `Authorization` header and expects the value to follow the `Bearer <token>` scheme.
- If the `Authorization` header is absent or the scheme is not `Bearer`, the middleware responds with `401 {"error": "unauthorized"}` and stops request processing.
- If the scheme is correct but the token does not match the configured key, the middleware responds with `403 {"error": "forbidden"}` and stops request processing.
- Token comparison must use `crypto/subtle.ConstantTimeCompare` to prevent timing attacks.
- The middleware is applied **only** to protected routes — it must not wrap public routes:
  - `shortener-api` protected: `POST /shorten`
  - `shortener-api` public: `GET /{code}`, `GET /health`
  - `stats-api` protected: `GET /stats/{code}`
  - `stats-api` public: `GET /health`
- Each service's `Config` struct gains an `APIKey string` field populated from the `API_KEY` environment variable via the existing `mustEnv` helper (required, no default, process exits on missing).
- `API_KEY=<value>` is added to `.env.example`, `.env.dev`, and `.env.prod` for both services.
- Routes are wired in each service's `internal/handler/routes.go` to apply `APIKeyAuth` only to the protected route group or handler.

## Possible Edge Cases

- `Authorization` header present but completely empty value.
- `Authorization` header value is just `Bearer` with no trailing token.
- Token contains leading/trailing whitespace after splitting on space.
- `API_KEY` env var is set to an empty string — the existing `mustEnv` pattern should catch this and exit; verify it does.
- Multiple `Authorization` headers sent in one request — Go's `net/http` will return the first value; this is acceptable.
- Case sensitivity of the `Bearer` scheme prefix — spec requires exact case match.

## Acceptance Criteria

- `POST /shorten` with a valid `Authorization: Bearer <correct-key>` header returns `201` (or whatever the current success code is) as before.
- `POST /shorten` with no `Authorization` header returns `401 {"error": "unauthorized"}`.
- `POST /shorten` with `Authorization: Token abc` (wrong scheme) returns `401 {"error": "unauthorized"}`.
- `POST /shorten` with `Authorization: Bearer wrong-key` returns `403 {"error": "forbidden"}`.
- `GET /{code}` and `GET /health` on `shortener-api` return their normal responses without any `Authorization` header.
- `GET /stats/{code}` with a valid key returns stats as before.
- `GET /stats/{code}` with no key returns `401`; with wrong key returns `403`.
- `GET /health` on `stats-api` returns its normal response without any `Authorization` header.
- Starting either service without `API_KEY` set causes the process to exit immediately with a clear error.

## Open Questions

- Should `API_KEY` be allowed to differ between environments (e.g., a weak key in `.env.dev`)? Assumed yes — each env file gets a placeholder value. Yes.
- Is a 401 response required to include a `WWW-Authenticate` header per RFC 7235? Not specified; omit for now unless the team requires strict RFC compliance. No.

## Testing Guidelines

Create test file(s) in the `tests/` folder of each service covering the following cases — keep them black-box and use the existing `httptest` + hand-written fake pattern (no mocks, no external framework):

- Valid `Authorization: Bearer <correct-key>` on a protected route → request passes through and returns the expected success response.
- No `Authorization` header on a protected route → `401` with `{"error": "unauthorized"}`.
- `Authorization` header with wrong scheme (e.g., `Token abc`) on a protected route → `401` with `{"error": "unauthorized"}`.
- `Authorization: Bearer <wrong-key>` on a protected route → `403` with `{"error": "forbidden"}`.
- Public routes (`GET /{code}` and `GET /health` for shortener-api; `GET /health` for stats-api) return their normal responses with no `Authorization` header supplied.

## Docs Update

- Update the root `README.md` to document the `API_KEY` environment variable for both services.
- Check each service's `docs/` folder (if present) for any authentication or configuration reference that needs updating.
