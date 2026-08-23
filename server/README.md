# control-plane (Go)

The control plane app: configuration/secret management and feature flags, running on
[Echo](https://echo.labstack.com/) (HTTP), [GORM](https://gorm.io/) over Postgres,
[templ](https://templ.guide/) for server-rendered HTML, and [goose](https://github.com/pressly/goose)
for migrations. See the root [`README.md`](../README.md) and [`docs/`](../docs) for
product-level docs (API reference, configuration, deployment, security); this file covers
Go-specific setup and package layout.

## Quick start

Requires Go 1.25+, a Postgres database, and a Redis instance (both mandatory — see
[Requirements](#requirements) below).

```bash
cd server
go build ./...

# MASTER_ENCRYPTION_KEY must be a Fernet key - generate one with:
# python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"
export MASTER_ENCRYPTION_KEY=...
export DB_HOST=localhost DB_PORT=5432 DB_NAME=idp DB_USER=idp DB_PASSWORD=idp
export REDIS_URL=redis://127.0.0.1:6379/1
export ADMIN_EMAIL=admin@example.com ADMIN_PASSWORD=changeme123

go run ./cmd/server
```

The server listens on `:8000` (override with `PORT`), runs pending goose migrations against
Postgres on startup (see [`internal/db/db.go`](./internal/db/db.go)), and provisions/syncs the
admin user from `ADMIN_EMAIL`/`ADMIN_PASSWORD`.

- **Web UI** — http://localhost:8000/
- **Config/flag S2S API** — http://localhost:8000/api/v1/config/...

Or via Docker Compose, from the repo root:

```bash
cp .env.example .env
# set MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD
docker compose -f server/docker-compose.local.yml --env-file .env up --build
```

See [Container config](#container-config) for details.

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

Package layout under `internal/`, each with a narrow role:

| Package | Notes |
|---|---|
| `appconfig` | Loads runtime configuration from environment variables. |
| `db` | GORM is a query layer only; [goose](./internal/db/migrations) owns the schema. `Migrate` runs on every startup, guarded by a Postgres advisory lock. |
| `auth` | `User`, `ServiceClient`, OAuth models + `AuthService`/`OAuthService`. |
| `config` | `Application`/`Environment`/`ConfigEntry`/`ConfigEntryVersion`/`FeatureFlag` + `ConfigService`/`FeatureFlagService`. |
| `crypto` | Fernet master-key encryption + per-client re-encryption. |
| `security` | Password hashing. |
| `session` | Redis-backed signed-cookie sessions; flash messages and the CSRF token live inside the same session blob. |
| `ratelimit` | Redis fixed-window limiter. |
| `activity` | Append-only audit log writer. |
| `cache` | Redis-backed, version-counter invalidation for `ConfigService`/`FeatureFlagService`. |
| `api` | The S2S config/flag JSON API — see [API reference](../docs/api.md). |
| `webui` | Session-authenticated CRUD handlers; routes registered in [`router.go`](./internal/webui/router.go). |
| `observability` | Opt-in OTLP traces/metrics/logs, enabled only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set. |

`views/` holds the `.templ` sources and their generated `_templ.go` output (generated files are
committed, so a plain `go build` never needs the templ CLI). `static/` is served at `/static`; it
has no custom stylesheet yet — Bootstrap/Alpine load from CDN in the meantime.

There is no JWT-based user/service auth API and no generic admin panel — both were deliberately
left out rather than ported from an earlier implementation of this app. Service-client creation,
user management, and everything else are handled by the web UI; the only programmatic API is
S2S API-key auth for config/flag reads.

## Requirements

- **Postgres is required** — no SQLite support. The schema management (goose) and some query
  patterns are Postgres-specific, so `DB_*` must point at a real Postgres instance even for local
  development.
- **Redis is required**, not optional. The server dials Redis on startup and fails to boot if it's
  unreachable, since sessions, rate limiting, and caching are all Redis-backed with no fallback.
- **No auto-generated dev encryption key.** A missing `MASTER_ENCRYPTION_KEY` is a fatal config
  error in every environment, including local dev.

## Container config

See [`docker-compose.local.yml`](./docker-compose.local.yml) and [`Dockerfile`](./Dockerfile) in
this directory. They read `.env` from the repo root and stand up Postgres + Redis sidecars
alongside the app — see [Deployment](../docs/deployment.md) for the full walkthrough.
