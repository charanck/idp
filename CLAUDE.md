# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go control plane for configuration/secret management and feature flags, with API-key
service-to-service (S2S) auth, OAuth2/OIDC login, and a server-rendered Bootstrap web UI. Configs
and secrets are the same model (`ConfigEntry` with `is_secret=true`) and are encrypted at rest with
a master key, then re-encrypted per-client on read. Built on [Echo](https://echo.labstack.com/)
(HTTP), [GORM](https://gorm.io/) over Postgres (query layer only — [goose](https://github.com/pressly/goose)
owns the schema via `internal/db/migrations/`), and [templ](https://templ.guide/) for
server-rendered HTML. See the Architecture section below for package-by-package detail.

There is no JWT-based user/service auth API and no Django-admin-style generic admin — both were
deliberately not carried over from an earlier Django implementation of this app. Service clients,
users, configs, secrets, and feature flags are all managed through the session-authenticated web
UI; the only programmatic API is S2S API-key auth for reading configs/flags.

## Commands

```bash
go build ./...          # build everything
go run ./cmd/server      # run the HTTP server
go run ./cmd/migrate-cutover [--dry-run]   # standalone schema-reconciliation/validation tool
make test                # go test -p 1 ./... (see Makefile for why -p 1 is required)
```

Regenerating templ views (only needed after editing a `.templ` file) requires the
[templ CLI](https://templ.guide/quick-start/installation):

```bash
go run github.com/a-h/templ/cmd/templ generate
```

Most tests are integration tests against a real Postgres instance (goose/GORM target
Postgres-specific types a mock or sqlite can't stand in for). Point `CP_TEST_DATABASE_URL` at a
throwaway Postgres database before running `make test`; without it, those tests `t.Skip()`
individually rather than failing:

```bash
docker run --rm -d -p 5433:5432 -e POSTGRES_PASSWORD=idp -e POSTGRES_USER=idp -e POSTGRES_DB=idp_test postgres:15.3-alpine
export CP_TEST_DATABASE_URL="host=localhost port=5433 dbname=idp_test user=idp password=idp sslmode=disable"
make test
```

Required env vars (see `docs/configuration.md` for the full list): `MASTER_ENCRYPTION_KEY` (no
auto-generated fallback — a missing key is a fatal error in every environment), `DB_*`
(Postgres — required, no SQLite support), `REDIS_URL` (Redis — required, not optional; sessions,
rate limiting, and caching are all Redis-backed with no fallback), `ADMIN_EMAIL`/`ADMIN_PASSWORD`
(provisioned/synced on every startup).

## Architecture

Package layout under `internal/`, each with a narrow role:

| Package | Responsibility |
|---|---|
| `appconfig` | Loads runtime configuration from environment variables. |
| `db` | Schema management. GORM is a query layer only; goose (`internal/db/migrations`) owns the schema. `Migrate` runs on every startup, guarded by a Postgres advisory lock. |
| `auth` | `User` (custom, email-based login, UUID PK, `ForcePasswordReset` flag), `ServiceClient` (S2S API-key holder + per-client Fernet `EncryptionKey`), OAuth2/OIDC models, and `AuthService`/`OAuthService` (authlib-equivalent authorization-code flow). |
| `config` | `Application` → `Environment` (unique per app) → `ConfigEntry` (unique per app+env+key; secrets are just `IsSecret=true` entries) and `FeatureFlag` (same app+env scoping, soft-deleted). `ConfigEntryVersion` is an immutable snapshot written on every create/update/delete/rollback of a `ConfigEntry`. `Activity` is an append-only audit log. `ConfigService`/`FeatureFlagService` both **get-or-create** the `Application`/`Environment` scope from `(service, environment)` string pairs rather than taking foreign keys directly — this is the shape both the API and the web UI call into. |
| `crypto` | Fernet master-key encryption (`EncryptForStorage`/`DecryptFromStorage`) + per-client re-encryption (`ReEncryptForClient`). |
| `security` | Password hashing. |
| `session` | Redis-backed signed-cookie sessions; flash messages and the CSRF token live in the same session blob. |
| `ratelimit` | Redis fixed-window limiter. |
| `activity` | Append-only audit log writer. |
| `cache` | Redis-backed, version-counter invalidation for `ConfigService`/`FeatureFlagService` list reads. |
| `api/http` | The S2S config/flag JSON API (`/api/v1/config/...`) — `APIKeyAuth` reads the `X-API-Key` header. |
| `web` | Session-authenticated CRUD handlers for everything: applications, environments, configs/secrets (incl. history/rollback), feature flags, users, service clients, OAuth providers, OAuth login/callback, activity log. Routes registered in `web/router.go`. |
| `observability` | Opt-in OTLP traces/metrics/logs, enabled only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set. |

`web/template/` holds the `.templ` sources and their generated `_templ.go` output (generated files
are committed, so a plain `go build` never needs the templ CLI). `web/static/` is served at
`/static`.

### Two auth systems

1. **API** (`/api/v1/config/...`): stateless. `APIKeyAuth` reads `X-API-Key: <key_id>.<secret>` and
   resolves a `ServiceClient` via `AuthService`'s API-key verification. This is the only
   programmatic auth surface in the app.
2. **Web UI** (`/...`): signed-cookie session auth (`internal/session`), gated by a login-required
   check and an admin-only (`IsStaff`) check for privileged pages.

Both operate on the same `auth`/`config` models and services — when changing a service method,
check both `api/http` and `web` for callers.

### Encryption flow

Admin writes a config/secret via the web UI → `ConfigService.UpsertConfig` encrypts with
`MASTER_ENCRYPTION_KEY` before storing. A service client reads via
`GET /api/v1/config/configs/list` (API-key auth) → the server decrypts with the master key and
**re-encrypts with that client's own `EncryptionKey`** before returning it; the client decrypts
locally with the key it was given at creation time. The web UI never displays decrypted secret
values back (`UpsertConfig` always returns `"***ENCRYPTED***"`).

### Caching

`ConfigService`/`FeatureFlagService` cache list responses in Redis, scoped by service+environment
(+ client key hash for configs). Cache invalidation is version-based: a per-scope version counter
(`config:scope-version:{service}:{environment}`) is bumped on any write instead of deleting keys
directly — read paths must incorporate the current version into their cache key.

### Config history and rollback

Every `ConfigEntry` write path (web UI create/clone/edit) calls `ConfigService.RecordConfigVersion`,
which snapshots the entry's current *encrypted* value into `ConfigEntryVersion` via a cascading FK
(`config_entry`), numbered per entry starting at 1. Deleting a `ConfigEntry` deletes its version
history with it (cascade) — history is not kept for deleted configs, and re-creating the same key
later starts a fresh history at version 1. `ConfigService.RollbackConfig` restores a prior version
by calling `UpsertConfig` again (tagged as a rollback action) rather than mutating history in
place — the rollback itself becomes a new, auditable version. Secret values are never included in
history responses, only that a version changed, when, and by whom.

### Rate limiting

A fixed-window limiter (`internal/ratelimit`, Redis-backed) throttles `POST /login/` per client IP
(`AUTH_RATE_LIMIT`/`AUTH_RATE_LIMIT_WINDOW_SECONDS`). S2S API-key auth (`X-API-Key`, used by both
endpoints under `/api/v1/config/...`) is separately throttled per client IP via
`S2S_AUTH_RATE_LIMIT` — **every request counts toward the window**, whether the key is valid or
not; there is no separate failed-only tracking.
