# Configuration

All configuration is via environment variables, loaded from `.env` in development (see
[`.env.example`](../.env.example) for a ready-to-copy template).

## Core

| Variable | Default | Notes |
|---|---|---|
| `DEBUG` | `false` | Set `false` in every non-local environment. |
| `SESSION_SECRET` | falls back to `DJANGO_SECRET_KEY`, then an insecure dev default | Signs session cookies and CSRF tokens. Set explicitly outside local dev. |
| `ALLOWED_HOSTS` | `localhost,127.0.0.1` | Comma-separated. |
| `CSRF_TRUSTED_ORIGINS` | *(empty)* | Comma-separated `scheme://host` origins allowed to POST; only needed behind HTTPS/a reverse proxy. |
| `PORT` | `8000` | HTTP listen port. |

## Encryption

| Variable | Default | Notes |
|---|---|---|
| `MASTER_ENCRYPTION_KEY` | *(none — required)* | Encrypts every config/secret value at rest. The server refuses to start if this is unset; there is no dev-only auto-generated fallback. Generate with: `python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"`. Back it up somewhere durable; losing it makes stored data permanently unrecoverable. |

## Initial admin user

| Variable | Default | Notes |
|---|---|---|
| `ADMIN_EMAIL` | `admin@example.com` | Provisioned/synced on **every** startup — the admin user always ends up matching these two values. |
| `ADMIN_PASSWORD` | `changeme123` | Same. Change it afterwards from the web UI (or by updating this env var and restarting). |

## Database

Postgres is required — there is no SQLite support.

| Variable | Default | Notes |
|---|---|---|
| `DB_HOST` | `localhost` | |
| `DB_PORT` | `5432` | |
| `DB_NAME` | `idp` | |
| `DB_USER` | `idp` | |
| `DB_PASSWORD` | `idp` | |
| `DB_SSLMODE` | `disable` | |

## Cache and sessions

Redis is required (not optional) — sessions, rate limiting, and caching all depend on it, and the
server fails to boot if it's unreachable.

| Variable | Default | Notes |
|---|---|---|
| `REDIS_URL` | `redis://127.0.0.1:6379/1` | |
| `CACHE_TIMEOUT` | `300` | Seconds. |
| `CACHE_KEY_PREFIX` | `control_plane` | |

## Rate limiting

| Variable | Default | Notes |
|---|---|---|
| `AUTH_RATE_LIMIT` | `10` | Max requests per client IP per window, for `POST /login/`. |
| `AUTH_RATE_LIMIT_WINDOW_SECONDS` | `60` | Window size. |
| `S2S_AUTH_RATE_LIMIT` | `20` | Max **failed** S2S API-key attempts per client IP per window; valid keys are never throttled. |

See [Architecture](./architecture.md#rate-limiting) for how these are enforced.

## Observability (optional)

| Variable | Default | Notes |
|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | *(unset)* | Setting this enables OTLP traces/metrics/logs export; leaving it unset disables observability entirely. |
