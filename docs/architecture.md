# Architecture

## Packages

Package layout under `internal/`, each with a narrow role:

| Package | Responsibility |
|---|---|
| `appconfig` | Loads runtime configuration from environment variables. |
| `db` | Schema management. GORM is a query layer only; [goose](https://github.com/pressly/goose) (`internal/db/migrations`) owns the schema. `Migrate` runs on every startup, guarded by a Postgres advisory lock. |
| `model` | Plain GORM structs + repository *interfaces*, split by domain (`model/auth`, `model/config`, `model/notification`, `model/dashboard`, `model/analytics`, `model/activity`) — no business logic. |
| `repository` | GORM implementations of the `model.*Repository` interfaces, one subpackage per domain. Services depend on the `model` interfaces, not this package, so they're swappable in tests. |
| `auth` | `User` (email login, UUID PK), `Group` (the access-control primitive — module permissions + Application allow-list, unioned via `ComputeEffectivePermissions`), `Policy` (singleton: password complexity, lockout, session idle timeout, SSO-only, login IP allow-list, self-registration domain allow-list), `ServiceClient` (S2S API-key holder + per-client Fernet encryption key; doubles as an OIDC `client_id`/`client_secret` when `IsAuthApplication`), and `AuthService`/`OAuthService`/`OIDCService`. |
| `config` | `Application` → `Environment` (unique per app) → `ConfigEntry` (configs and secrets are the same model) / `FeatureFlag`, plus `ConfigEntryVersion` history and the `Activity` audit log. |
| `notification` | Queues and delivers messages over `email`/`sms`/`inapp`; a DBOS-durable worker retries failed sends; an in-process SSE hub pushes realtime delivery events; short-lived Fernet tokens scope the two end-user endpoints. |
| `dashboard` | Aggregates counts + recent activity across `config`/`auth` for the web UI landing page. |
| `analytics` | Hourly DBOS-scheduled snapshots of dashboard counts + event counters into a rolling 7-day window, served through a short Redis cache. |
| `cleanup` | Monthly DBOS-scheduled pruning of notifications (90d), the activity log (180d), and expired OIDC authorization codes. |
| `crypto` | Fernet master-key encryption + per-client re-encryption. |
| `security` | Password hashing. |
| `session` | Redis-backed signed-cookie sessions; flash messages and the CSRF token live in the same session blob. |
| `ratelimit` | Redis fixed-window limiter. |
| `activity` | Append-only audit log writer. |
| `cache` | Redis-backed, version-counter invalidation for config/flag list reads. |
| `api/http` | The S2S JSON API (`/api/v1/config/...`, `/api/v1/notifications`) and the stateless half of the OIDC Identity Provider (`/.well-known/...`, `/oauth2/token`, `/oauth2/userinfo`). |
| `web` | Session-authenticated CRUD handlers for everything: applications, environments, configs/secrets (incl. history/rollback), feature flags, users, groups, service clients, OAuth providers, policies, branding, notification settings, forward-auth, activity log, dashboard. |

`config`'s `ConfigService` / `FeatureFlagService` **get-or-create** the `Application`/`Environment`
scope from `(service, environment)` string pairs rather than taking foreign keys directly — this
is the shape both the API and the web UI call into. `notification.Service` does the same for its
`service` field.

## Access control: Groups

Every `User` belongs to one or more `Group`s. Three built-in, non-deletable groups ship out of the
box:

| Group | Modules |
|---|---|
| **Admin** | every module |
| **Developer** | `dashboard`, `configs`, `flags` |
| **User** | none — an authenticated identity with no web UI access beyond its own profile page; the default for newly-created accounts |

Admins can also create custom groups with any subset of modules
(`applications`/`environments`/`configs`/`flags`/`service_clients`/`users`/`groups`/
`oauth_providers`/`policies`/`branding`/`notification_settings`/`activity_log`/`dashboard`) and,
optionally, an **Application allow-list** — a group with no allow-list can see/manage every
Application; one with an allow-list is scoped to just those. A user's *effective* permissions are
the union of all their groups' modules and allow-lists (`auth.ComputeEffectivePermissions`);
`web/authz.go`'s `ModuleRequired(module)` middleware gates every route in `web/router.go` on this.

`User.IsStaff`/`IsSuperuser` remain as DB columns for legacy/back-compat but are no longer read
for gating anywhere.

## Four auth surfaces

1. **S2S API** (`/api/v1/config/...`, `/api/v1/notifications`) — stateless. `APIKeyAuth` reads an
   `X-API-Key: <key_id>.<secret>` header and resolves a `ServiceClient`. The two end-user
   notification endpoints (SSE stream, in-app inbox) instead take a short-lived
   `Authorization: Bearer <token>` minted by `POST /api/v1/notifications/sessions`, since the
   caller there is the end user, not the service client.
2. **Web UI** (`/...`) — signed-cookie session auth, gated by a login-required check and
   per-module `ModuleRequired(module)` checks driven by the logged-in user's effective Group
   permissions.
3. **OIDC Identity Provider** (`/.well-known/...`, `/oauth2/...`) — a *relying-party* application
   (a `ServiceClient` with `IsAuthApplication` set) redirects its users to the
   session-authenticated `GET/POST /oauth2/authorize` (web UI), then exchanges the resulting code
   for RS256-signed tokens via the stateless `POST /oauth2/token` / `GET /oauth2/userinfo`. This is
   the reverse direction of #4 below: here, control-plane is the IdP for *other* apps.
4. **Forward-auth** (`GET /forward-auth/verify`) — lets a reverse proxy gate an entire third-party
   app behind control-plane's own login session, without that app implementing any auth itself. See
   [Configuration: Forward-auth Identity Provider](./configuration.md#forward-auth-identity-provider).

All four operate on the same `auth`/`config`/`notification` models and services — a change to a
service method typically needs checking `api/http` and `web` for callers.

Separately, the web UI itself supports **OAuth2/OIDC login** — control-plane's own users signing
in via an external IdP (Google, GitHub, Okta, ...) configured under **OAuth Providers**. That's
`OAuthService`, unrelated to `OIDCService` above (opposite direction of the relationship).

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

## Notifications: queue, workers, and realtime delivery

`POST /api/v1/notifications` queues a message on one of three channels (`email`/`sms`/`inapp`),
scoped to a `service` the same way configs/flags are. Delivery is asynchronous:

- **Worker** — `notification.TaskEnqueuer` starts a [DBOS](https://github.com/dbos-inc/dbos-transact-golang)
  durable workflow per notification (`SendWorkflow`), running inside `cmd/server` itself — no
  separate worker process to deploy. DBOS persists workflow progress to Postgres, so an in-flight
  send survives a restart and won't be double-delivered. Failed sends retry up to a fixed limit
  before the notification is marked `failed`.
- **Channel providers** (`internal/notification/provider/`) — `email` sends real mail over SMTP
  (configured per-environment under **Notification Settings**); `sms` is a validate-and-log
  skeleton with no real provider wired up yet; `inapp` "delivery" is just persisting the row.
- **Realtime fan-out** — an in-process `sse_hub.go` pub/sub pushes delivery events to
  `GET /api/v1/notifications/sse/events` subscribers as they happen (push, not persisted).
- **In-app inbox** — `GET /api/v1/notifications/inapp/unread` is the pull-based, persisted
  counterpart: fetching marks those rows read.

Both end-user endpoints authenticate with a short-lived Fernet bearer token minted by
`POST /api/v1/notifications/sessions`, scoped to one `user_id` — never the service client's own
`X-API-Key`. See the [Notifications](guides/notifications.md), [SSE](guides/sse.md), and
[In-app inbox](guides/inapp-inbox.md) guides.

## Dashboard and analytics

`dashboard.Service` computes the web UI landing page's live counts (applications, environments,
configs, secrets, active flags, service clients) directly from Postgres on each request.
`analytics.Service` layers trend data on top: an hourly DBOS-scheduled workflow snapshots those
same counts plus event counters into a rolling 7-day Postgres window, read back through a
10-minute Redis cache so the dashboard doesn't hit Postgres on every load.

## Scheduled cleanup

A monthly DBOS-scheduled workflow (`internal/cleanup`) prunes data that would otherwise grow
unbounded: notifications older than 90 days, activity-log entries older than 180 days, and expired
(single-use) OIDC authorization codes. Retention windows are fixed constants, not
environment-configurable — changing them is a deliberate code change, not a deploy-time flip.

## Caching

`ConfigService` / `FeatureFlagService` cache list responses in Redis, scoped by
service+environment (+ client key hash for configs). Invalidation is version-based: a per-scope
counter (`config:scope-version:{service}:{environment}`) is bumped on any write instead of deleting
keys directly, and read paths incorporate the current version into their cache key. `analytics`
uses a simple short-TTL cache for its recent-snapshots read.

## Rate limiting

A fixed-window limiter throttles `POST /login/` per client IP (`AUTH_RATE_LIMIT` /
`AUTH_RATE_LIMIT_WINDOW_SECONDS`), backed by Redis.

S2S API-key auth (`X-API-Key`, used by every endpoint under `/api/v1/config/...` and
`/api/v1/notifications`) is separately throttled per client IP via `S2S_AUTH_RATE_LIMIT` — **every
request counts toward the window**, whether the key is valid or not; there is no separate
failed-only tracking.

See [Configuration](./configuration.md) for the relevant environment variables.
