# Architecture

## Apps

Four Django apps, each with a narrow role:

| App | Responsibility |
|---|---|
| `control_plane_project/` | Settings, root URLs. Mounts the Django Ninja API at `/api/v1/`, Django admin at `/admin/`, web UI at `/`. |
| `authentication/` | Custom `User` model (email login, UUID PK), `ServiceClient` (S2S API-key holder + per-client encryption key), OAuth2/OIDC models, JWT issuance. |
| `config_management/` | `Application` → `Environment` (unique per app) → `ConfigEntry` (configs and secrets are the same model) / `FeatureFlag`, plus `ConfigEntryVersion` history and the `Activity` audit log. |
| `web_ui/` | Server-rendered CRUD (Bootstrap templates) for everything above, OAuth login/callback flow, read-only activity log. |
| `common/` | Shared utilities: Fernet encryption, JWT helpers, Django Ninja auth classes, password hashing, activity logging, rate limiting. |

`config_management`'s `ConfigService` / `FeatureFlagService` **get-or-create** the `Application`/`Environment` scope from `(service, environment)` string pairs rather than taking foreign keys directly — this is the shape both the Ninja API and the web UI call into.

## Two independent auth systems

1. **API** (`/api/v1/...`) — stateless.
   - `JWTAuth` / `JWTAdminAuth` (staff-only) read a `Bearer` token, issued by `POST /api/v1/auth/token`.
   - `APIKeyAuth` reads an `X-API-Key: <key_id>.<secret>` header and resolves a `ServiceClient`.
2. **Web UI** (`/...`) — standard Django session auth (`django.contrib.auth`), gated with `@login_required` / an `is_staff`-checking `admin_required` decorator.

Both paths operate on the same `authentication` / `config_management` models and services — a change to a service method typically needs checking both `*/api.py` and `web_ui/views.py` for callers.

## Encryption flow

1. **Write** — an admin creates a config/secret (`POST /api/v1/config/configs/upsert`, JWT auth, or the web UI). `ConfigService.upsert_config` encrypts the value with `MASTER_ENCRYPTION_KEY` before storing it. The API/UI never echo the plaintext back — secrets always render as `***ENCRYPTED***`.
2. **Read** — a service client calls `GET /api/v1/config/configs/list` with its `X-API-Key`. The server decrypts with the master key and **re-encrypts with that client's own `encryption_key`** (generated once, at client-creation time) before returning it.
3. **Client-side decrypt** — the client decrypts locally with the key it was given at creation time; see [API reference](./api.md#encryption-model) for a worked example.

Losing `MASTER_ENCRYPTION_KEY` makes every stored config/secret value permanently unrecoverable.

## Config history and rollback

Every `ConfigEntry` write path (API upsert, web UI create/clone/edit) calls `ConfigService.record_config_version`, which snapshots the entry's current *encrypted* value into `ConfigEntryVersion` (numbered per entry, starting at 1). Deleting a `ConfigEntry` cascades and deletes its version history — history isn't kept for deleted configs, and re-creating the same key later starts a fresh history at version 1.

`ConfigService.rollback_config` restores a prior version by calling `upsert_config` again (tagged `history_action="rollback"`) rather than mutating history in place, so the rollback itself becomes a new, auditable version. Secret values are never included in history responses — only that a version changed, when, and by whom.

## Caching

When `CACHE_ENABLED=true`, `ConfigService` / `FeatureFlagService` cache list responses scoped by service+environment (+ client key hash for configs). Invalidation is version-based: a per-scope counter (`config:scope-version:{service}:{environment}`) is bumped on any write instead of deleting keys directly, and read paths incorporate the current version into their cache key.

## Rate limiting

`common/middleware.py`'s `RateLimitMiddleware` throttles a fixed set of `(method, path)` routes — `POST /api/v1/auth/token`, `POST /api/v1/auth/register`, `POST /login/` — per client IP, via a fixed-window counter in a dedicated `'ratelimit'` cache alias (`common/ratelimit.py`). That cache alias always stores real counters regardless of the app-level `CACHE_ENABLED` flag: Redis when `CACHE_ENABLED`/`CACHE_BACKEND=redis`, otherwise an in-process `locmem` cache.

S2S API-key auth (`X-API-Key`, used by `/api/v1/config/configs/list`, `/api/v1/auth/s2s/ping`, and the feature-flags list endpoint) is separately throttled **on failed attempts only**, per client IP, via `S2S_AUTH_RATE_LIMIT` — a valid key is never throttled, only guessing.

See [Configuration](./configuration.md) for the relevant environment variables.
