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

Access control is Group-based: every `User` belongs to one or more `Group`s — built-in **Admin**
(every module), **Developer** (`dashboard`, `configs`, `flags`), and **User** (no module access
beyond their own profile; the default for new accounts) — plus admin-creatable custom groups, each
granting a coarse set of module permissions and an optional Application allow-list.
`User.IsStaff`/`IsSuperuser` remain as DB columns for legacy/back-compat but are no longer read for
gating. Service clients, users, groups, configs, secrets, feature flags, and notification settings
are all managed through the session-authenticated web UI.

Scheduled background work (notification delivery, hourly analytics snapshots, monthly data
cleanup) runs as [DBOS](https://github.com/dbos-inc/dbos-transact-golang) durable workflows inside
the same `cmd/server` process — no separate worker process or queue infra to deploy.

Beyond S2S API-key auth for reading configs/flags, this control-plane is also an OAuth2/OIDC
**Identity Provider** for other applications: an "auth application" (a `ServiceClient` with
`IsAuthApplication` set) redirects its users here to log in via a standard authorization-code
flow and gets back RS256-signed ID/access tokens plus a JWKS endpoint. This is unrelated to the
existing `OAuthProvider` feature below, which is the reverse direction (control-plane's own users
logging in via an external IdP like Google).

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
| `model` | Plain GORM structs + repository *interfaces* only, split by domain (`model/auth`, `model/config`, `model/notification`, `model/dashboard`, `model/analytics`, `model/activity`) — no business logic. `User`, `Group`, `Policy`, `ServiceClient`, `OIDCSigningKey`/`OIDCAuthorizationCode`, `Application`/`Environment`/`ConfigEntry`/`ConfigEntryVersion`/`FeatureFlag`, `Notification`/`ProviderSetting` all live here. |
| `repository` | GORM implementations of the `model.*Repository` interfaces, one subpackage per domain mirroring `model`'s split. Services depend on the `model` interfaces, not this package directly, so they're swappable in tests. |
| `auth` | `AuthService` (users, password/lockout policy enforcement, group membership), `Group`-permission logic (`ComputeEffectivePermissions` unions a user's groups' module permissions + Application allow-lists), `OAuthService` (control-plane-as-relying-party: logging control-plane's own users in via an external IdP), `OIDCService` (control-plane-as-Identity-Provider: RS256 JWKS, authorization codes, token issuance for other applications) — unrelated, independent flows sharing the same `ServiceClient` model (`IsAuthApplication` marks one as an OIDC `client_id`/`client_secret` holder). Built-in groups are **Admin** (every module), **Developer** (`dashboard`,`configs`,`flags`), **User** (no module access; default for new accounts). |
| `config` | `Application` → `Environment` (unique per app) → `ConfigEntry` (unique per app+env+key; secrets are just `IsSecret=true` entries) and `FeatureFlag` (same app+env scoping, soft-deleted). `ConfigEntryVersion` is an immutable snapshot written on every create/update/delete/rollback of a `ConfigEntry`. `ConfigService`/`FeatureFlagService` both **get-or-create** the `Application`/`Environment` scope from `(service, environment)` string pairs rather than taking foreign keys directly — this is the shape both the API and the web UI call into. |
| `notification` | `Service` (queue/list/get), `TaskEnqueuer`/`worker.go` (DBOS-workflow-backed send pipeline with retries across `email`/`sms`/`inapp` channel providers, `internal/notification/provider/`), `sse_hub.go` (in-process pub/sub for realtime delivery events), `token.go` (short-lived Fernet bearer tokens scoping an end user to the SSE/inbox endpoints), `ProviderSettingService` (per-channel settings, e.g. SMTP, edited in the web UI). Gated as a whole by the `Enabled` const in `flag.go`. |
| `dashboard` | Aggregates counts (`Application`/`Environment`/`ConfigEntry`/`FeatureFlag`/`ServiceClient`) and recent activity spanning `config` and `auth` for the web UI dashboard landing page. |
| `analytics` | Computes/serves the dashboard's trend data: an hourly DBOS-scheduled workflow snapshots `dashboard.Counts` + event counters into a rolling 7-day Postgres window, read back through a short Redis cache. |
| `cleanup` | A monthly DBOS-scheduled workflow pruning data that would otherwise grow unbounded: delivered/failed notifications (90d), the activity audit log (180d), expired OIDC authorization codes. |
| `crypto` | Fernet master-key encryption (`EncryptForStorage`/`DecryptFromStorage`) + per-client re-encryption (`ReEncryptForClient`). |
| `security` | Password hashing. |
| `session` | Redis-backed signed-cookie sessions; flash messages and the CSRF token live in the same session blob. |
| `ratelimit` | Redis fixed-window limiter. |
| `activity` | Append-only audit log writer. |
| `cache` | Redis-backed, version-counter invalidation for `ConfigService`/`FeatureFlagService` list reads. |
| `api/http` | The S2S config/flag/notification JSON API (`/api/v1/...`) and the stateless half of the OIDC IdP (`/.well-known/...`, `POST /oauth2/token`, `GET /oauth2/userinfo`). `APIKeyAuth`/`NotificationAPIKeyAuthMiddleware` read the `X-API-Key` header. |
| `web` | Session-authenticated CRUD handlers for everything: applications, environments, configs/secrets (incl. history/rollback), feature flags, users, groups, service clients, OAuth providers, OAuth login/callback, the browser half of `/oauth2/authorize`, policies, branding, notification settings, forward-auth, activity log, dashboard. Routes registered in `web/router.go`. |
| `observability` | Opt-in OTLP traces/metrics/logs, enabled only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set. |

`web/template/` holds the `.templ` sources and their generated `_templ.go` output (generated files
are committed, so a plain `go build` never needs the templ CLI). `web/static/` is served at
`/static`.

### Four auth surfaces

1. **S2S API** (`/api/v1/config/...`, `/api/v1/notifications`): stateless. `APIKeyAuth` /
   `NotificationAPIKeyAuthMiddleware` read `X-API-Key: <key_id>.<secret>` and resolve a
   `ServiceClient` via `AuthService`'s API-key verification. A client's
   `ServiceClientApplicationIDs` allow-list (empty = unrestricted), if non-empty, 404s config/flag
   reads for services outside its scope. The two end-user notification endpoints
   (`/api/v1/notifications/sse/events`, `/api/v1/notifications/inapp/unread`) instead take a
   short-lived `Authorization: Bearer <token>` minted by `POST /api/v1/notifications/sessions` —
   the caller is the end user, not the service client.
2. **Web UI** (`/...`): signed-cookie session auth (`internal/session`), gated by a login-required
   check and per-module `ModuleRequired(module)` checks driven by the logged-in user's effective
   Group permissions (see `web/authz.go`).
3. **OIDC Identity Provider** (`/.well-known/...`, `/oauth2/...`): a relying-party application
   redirects its users to the session-authenticated `GET/POST /oauth2/authorize` (in `web`), then
   exchanges the resulting code for tokens via the stateless `POST /oauth2/token` /
   `GET /oauth2/userinfo` (in `api/http`), both backed by `OIDCService`.
4. **Forward-auth** (`GET /forward-auth/verify`): stateless-ish — reads the same session cookie as
   the Web UI (via `authMW.LoadUser()`, not `LoginRequired()`, since a reverse proxy needs a plain
   401/302 rather than a redirect bounce) plus `X-Forwarded-*` headers, and gates a *third-party*
   app's whole origin behind control-plane login without that app implementing any auth itself —
   see `web/forwardauth_handler.go` and [Configuration](docs/configuration.md#forward-auth-identity-provider).

All four operate on the same `auth`/`config`/`notification` models and services — when changing a
service method, check `api/http` and `web` for callers.

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
