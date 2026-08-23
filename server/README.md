# control-plane (Go)

A Go rewrite of the Django control plane in the repo root — same data model, same encryption
scheme, same env var names, same routes — running on [Echo](https://echo.labstack.com/),
[GORM](https://gorm.io/) over Postgres, [templ](https://templ.guide/) for server-rendered HTML,
and [goose](https://github.com/pressly/goose) for migrations. It is being built up incrementally
alongside the Django app; see [Migration status](#migration-status-vs-django) below for exactly
what is and isn't ported yet.

## Quick start

Requires Go 1.25+, a Postgres database, and a Redis instance (both mandatory here — see
[Differences from the Django app](#differences-from-the-django-app)).

```bash
cd server
go build ./...

# MASTER_ENCRYPTION_KEY must be a Fernet key - reuse the one from the repo root's .env
# (see ../README.md) so data stays readable across both implementations, or generate a
# fresh one: python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"
export MASTER_ENCRYPTION_KEY=...
export DB_HOST=localhost DB_PORT=5432 DB_NAME=idp DB_USER=idp DB_PASSWORD=idp
export REDIS_URL=redis://127.0.0.1:6379/1
export ADMIN_EMAIL=admin@example.com ADMIN_PASSWORD=changeme123

go run ./cmd/server
```

The server listens on `:8000` (override with `PORT`), runs pending goose migrations against
Postgres on startup (safe against an existing Django-managed schema — see
[`internal/db/db.go`](./internal/db/db.go)), and provisions/syncs the admin user from
`ADMIN_EMAIL`/`ADMIN_PASSWORD` exactly like `manage.py setup_admin`.

- **Web UI** — http://localhost:8000/
- **Config/flag S2S API** — http://localhost:8000/api/v1/config/...

Or via Docker Compose, from the repo root:

```bash
cp .env.example .env
# set MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD
docker compose -f server/docker-compose.local.yml --env-file .env up --build
```

This uses the same `.env` as the Django stack — see [Container config](#container-config).

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

### Tests

Most tests are integration tests against a real Postgres instance (goose/GORM target
Postgres-specific types a mock or sqlite can't stand in for). Point `CP_TEST_DATABASE_URL` at a
throwaway Postgres database before running `make test`; without it, those tests `t.Skip()`
individually rather than failing:

```bash
docker run --rm -d -p 5433:5432 -e POSTGRES_PASSWORD=idp -e POSTGRES_USER=idp -e POSTGRES_DB=idp_test postgres:15.3-alpine
export CP_TEST_DATABASE_URL="host=localhost port=5433 dbname=idp_test user=idp password=idp sslmode=disable"
make test
```

## Architecture

Package layout under `internal/`, each mirroring one piece of the Django app:

| Package | Mirrors | Notes |
|---|---|---|
| `appconfig` | `control_plane_project/settings.py` | Reads the *same* env var names as Django, so `.env` doesn't change. |
| `db` | `manage.py migrate` | GORM is a query layer only; [goose](./internal/db/migrations) owns the schema. `Migrate` runs on every startup, guarded by a Postgres advisory lock, and reconciles cleanly against a schema Django's own migrations already created (`CREATE TABLE IF NOT EXISTS`). |
| `auth` | `authentication/` | `User`, `ServiceClient`, OAuth models + `AuthService`/`OAuthService`. |
| `config` | `config_management/` | `Application`/`Environment`/`ConfigEntry`/`ConfigEntryVersion`/`FeatureFlag` + `ConfigService`/`FeatureFlagService`. |
| `crypto` | `common/encryption.py` | Fernet master-key encryption + per-client re-encryption. |
| `security` | `common/security.py` | Password hashing. |
| `session` | `django.contrib.sessions` + `.messages` | Redis-backed signed-cookie sessions; flash messages and the CSRF token live inside the same session blob. |
| `ratelimit` | `common/ratelimit.py` | Redis fixed-window limiter, same semantics as the Django middleware. |
| `activity` | `common/activity_logger.py` | Append-only audit log writer. |
| `cache` | Django's cache framework usage in `ConfigService`/`FeatureFlagService` | Redis-backed, version-counter invalidation (same scheme as the Python side). |
| `api` | `authentication/api.py` (config-related routes) | Ninja-equivalent JSON API. Only the S2S config/flag endpoints are ported so far — see below. |
| `webui` | `web_ui/views.py` + `urls.py` | Session-authenticated CRUD handlers; routes registered in [`router.go`](./internal/webui/router.go) 1:1 with `web_ui/urls.py`. |
| `observability` | *(new, no Django equivalent)* | Opt-in OTLP traces/metrics/logs, enabled only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set. |

`views/` holds the `.templ` sources and their generated `_templ.go` output (generated files are
committed, so a plain `go build` never needs the templ CLI). `static/` is served at `/static` and
is currently empty (see gaps below).

## Migration status vs Django

**Ported and route-for-route equivalent:**
- Full web UI (session auth, CSRF, flash messages) — applications, environments, configs/secrets
  (incl. history/rollback), feature flags, users, service clients, OAuth providers, OAuth
  login/callback flow, activity log.
- S2S config API: `GET /api/v1/config/configs/list`, `GET /api/v1/config/feature-flags`
  (`X-API-Key` auth, same encryption re-wrap on read, same rate limiting).
- Schema: one shared Postgres schema between the two implementations (see
  [`internal/db/migrations/00001_baseline.sql`](./internal/db/migrations/00001_baseline.sql)) —
  either app can run against a database the other created.

**Not ported yet — known gaps:**
- JWT user/service auth API (`/api/v1/auth/token`, `/register`, `/s2s/clients`, `JWTAuth`/
  `JWTAdminAuth`). Only API-key (S2S) auth exists in Go today; there is no `/api/v1/auth/` router
  mounted in [`cmd/server/main.go`](./cmd/server/main.go).
- Django admin (`/admin/`) has no equivalent — the web UI's admin-gated pages cover the same
  ground for this app's models, but there's no generic admin.
- `static/app.css` (the small stylesheet the Django templates load alongside CDN Bootstrap/
  Alpine) hasn't been carried over — `static/` is currently empty on the Go side, so custom
  styling is missing even though Bootstrap CSS itself loads from the CDN either way.
- SQLite is not supported — see below.

When adding a feature, check both `web_ui/views.py`/`authentication/api.py` (Django) and the
corresponding Go package for parity before considering it done.

## Differences from the Django app

- **Postgres is required.** The Django app can run on SQLite for local dev; the Go app's schema
  management (goose) and some query patterns are Postgres-specific, so `DB_*` must point at a real
  Postgres instance even for local development.
- **Redis is required**, not optional. Django's `CACHE_ENABLED=false` path degrades to no caching
  and an in-process rate limiter; the Go server dials Redis on startup and fails to boot if it's
  unreachable, since sessions, rate limiting, and caching are all Redis-backed with no fallback.
- **No auto-generated dev encryption key.** Django auto-generates a throwaway
  `MASTER_ENCRYPTION_KEY` at startup if one isn't set (dev convenience only). The Go server treats
  a missing key as a fatal config error in every environment.

## Container config

See [`docker-compose.local.yml`](./docker-compose.local.yml) and [`Dockerfile`](./Dockerfile) in
this directory — a drop-in alternative to the repo root's Django Dockerfile/compose file that
reads the exact same `.env`, listens on the same port, and stands up the same Postgres/Redis
sidecars. Point tooling at whichever compose file matches the implementation you want to run;
both can share one Postgres/Redis pair since they use the same schema.
