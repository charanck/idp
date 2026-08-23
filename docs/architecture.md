# Architecture

## Packages

Package layout under `server/internal/`, each with a narrow role:

| Package | Responsibility |
|---|---|
| `appconfig` | Loads runtime configuration from environment variables. |
| `db` | Schema management. GORM is a query layer only; [goose](https://github.com/pressly/goose) (`internal/db/migrations`) owns the schema. `Migrate` runs on every startup, guarded by a Postgres advisory lock. |
| `auth` | `User` (email login, UUID PK), `ServiceClient` (S2S API-key holder + per-client encryption key), OAuth2/OIDC models, and `AuthService`/`OAuthService`. |
| `config` | `Application` → `Environment` (unique per app) → `ConfigEntry` (configs and secrets are the same model) / `FeatureFlag`, plus `ConfigEntryVersion` history and the `Activity` audit log. |
| `crypto` | Fernet master-key encryption + per-client re-encryption. |
| `security` | Password hashing. |
| `session` | Redis-backed signed-cookie sessions; flash messages and the CSRF token live in the same session blob. |
| `ratelimit` | Redis fixed-window limiter. |
| `activity` | Append-only audit log writer. |
| `cache` | Redis-backed, version-counter invalidation for config/flag list reads. |
| `api` | The S2S config/flag JSON API (`/api/v1/config/...`) — see [API reference](./api.md). |
| `webui` | Session-authenticated CRUD handlers for everything: applications, environments, configs/secrets (incl. history/rollback), feature flags, users, service clients, OAuth providers, OAuth login/callback, activity log. |

`config`'s `ConfigService` / `FeatureFlagService` **get-or-create** the `Application`/`Environment`
scope from `(service, environment)` string pairs rather than taking foreign keys directly — this
is the shape both the API and the web UI call into.

## Two auth systems

1. **API** (`/api/v1/config/...`) — stateless. `APIKeyAuth` reads an
   `X-API-Key: <key_id>.<secret>` header and resolves a `ServiceClient`. This is the only
   programmatic auth in the app; there is no JWT-based user/service token issuance.
2. **Web UI** (`/...`) — signed-cookie session auth, gated with a login-required check and an
   admin-only check for privileged pages (users, service clients, OAuth providers).

Both paths operate on the same `auth` / `config` models and services — a change to a service
method typically needs checking both `internal/api` and `internal/webui` for callers.

## Encryption flow

1. **Write** — an admin creates a config/secret via the web UI. `ConfigService.UpsertConfig`
   encrypts the value with `MASTER_ENCRYPTION_KEY` before storing it. The UI never echoes the
   plaintext back — secrets always render as `***ENCRYPTED***`.
2. **Read** — a service client calls `GET /api/v1/config/configs/list` with its `X-API-Key`. The
   server decrypts with the master key and **re-encrypts with that client's own `encryption_key`**
   (generated once, at client-creation time) before returning it.
3. **Client-side decrypt** — the client decrypts locally with the key it was given at creation
   time; see [API reference](./api.md#encryption-model) for a worked example.

Losing `MASTER_ENCRYPTION_KEY` makes every stored config/secret value permanently unrecoverable.

## Config history and rollback

Every `ConfigEntry` write path (create/clone/edit in the web UI) records the entry's current
*encrypted* value as a new `ConfigEntryVersion` (numbered per entry, starting at 1). Deleting a
`ConfigEntry` cascades and deletes its version history — history isn't kept for deleted configs,
and re-creating the same key later starts a fresh history at version 1.

Rolling back restores a prior version by writing a new config version rather than mutating history
in place, so the rollback itself becomes a new, auditable version. Secret values are never included
in history responses — only that a version changed, when, and by whom.

## Caching

`ConfigService` / `FeatureFlagService` cache list responses in Redis, scoped by
service+environment (+ client key hash for configs). Invalidation is version-based: a per-scope
counter (`config:scope-version:{service}:{environment}`) is bumped on any write instead of deleting
keys directly, and read paths incorporate the current version into their cache key.

## Rate limiting

A fixed-window limiter throttles `POST /login/` per client IP (`AUTH_RATE_LIMIT` /
`AUTH_RATE_LIMIT_WINDOW_SECONDS`), backed by Redis.

S2S API-key auth (`X-API-Key`, used by both endpoints under `/api/v1/config/...`) is separately
throttled **on failed attempts only**, per client IP, via `S2S_AUTH_RATE_LIMIT` — a valid key is
never throttled, only guessing.

See [Configuration](./configuration.md) for the relevant environment variables.
