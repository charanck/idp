# Configuration

All configuration is via environment variables, loaded from `.env` in development (see
[`.env.example`](../.env.example) for a ready-to-copy template).

## Core Django

| Variable | Default | Notes |
|---|---|---|
| `DEBUG` | `True` | Set `False` in every non-local environment. |
| `DJANGO_SECRET_KEY` | insecure dev fallback | Set explicitly outside local dev. |
| `ALLOWED_HOSTS` | `localhost,127.0.0.1` | Comma-separated. |
| `CSRF_TRUSTED_ORIGINS` | *(empty)* | Comma-separated `scheme://host` origins allowed to POST; only needed behind HTTPS/a reverse proxy. |
| `LOG_LEVEL` | `DEBUG` (dev) / `INFO` (prod) | Root logger level. |
| `DJANGO_LOG_LEVEL` | `INFO` | `django.*` logger level. |

## JWT

| Variable | Default | Notes |
|---|---|---|
| `JWT_SECRET_KEY` | insecure dev fallback | Set explicitly outside local dev. |
| `JWT_EXPIRE_MINUTES` | `60` | Token lifetime. |

## Encryption

| Variable | Default | Notes |
|---|---|---|
| `MASTER_ENCRYPTION_KEY` | auto-generated | Encrypts every config/secret value at rest. **Must** be set explicitly outside local dev — if unset, a new key is generated on every restart and all previously stored data becomes unreadable. Generate with: `python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"`. Back it up somewhere durable; losing it makes stored data permanently unrecoverable. |

## Initial admin user

| Variable | Default | Notes |
|---|---|---|
| `ADMIN_EMAIL` | `admin@example.com` | Used to provision the first admin **once**, on first `migrate`. |
| `ADMIN_PASSWORD` | `changeme123` | Same — changing this later has no effect once the user exists; use the web UI or `manage.py setup_admin` instead. |

## Database

| Variable | Default | Notes |
|---|---|---|
| `DB_ENGINE` | `django.db.backends.sqlite3` | Use `django.db.backends.postgresql` for Docker/production. |
| `DB_NAME` | `db.sqlite3` | |
| `DB_USER` | — | Postgres only. |
| `DB_PASSWORD` | — | Postgres only. |
| `DB_HOST` | — | Postgres only. |
| `DB_PORT` | — | Postgres only. |

## Cache (optional)

| Variable | Default | Notes |
|---|---|---|
| `CACHE_ENABLED` | `false` | Enables caching of config/feature-flag list responses. |
| `CACHE_BACKEND` | `redis` | `redis` or `locmem`. |
| `CACHE_TIMEOUT` | `300` | Seconds. |
| `CACHE_KEY_PREFIX` | `idp` | |
| `REDIS_URL` | `redis://127.0.0.1:6379/1` | |

## Rate limiting

| Variable | Default | Notes |
|---|---|---|
| `AUTH_RATE_LIMIT` | `10` | Max requests per client IP per window, for `POST /api/v1/auth/token`, `POST /api/v1/auth/register`, `POST /login/`. |
| `AUTH_RATE_LIMIT_WINDOW_SECONDS` | `60` | Window size. |
| `S2S_AUTH_RATE_LIMIT` | `20` | Max **failed** S2S API-key attempts per client IP per window; valid keys are never throttled. |

Rate limiting always uses a real counter (Redis when `CACHE_ENABLED`/`CACHE_BACKEND=redis`, otherwise an in-process `locmem` cache) — it's independent of whether app-level caching is turned on.

See [Architecture](./architecture.md#rate-limiting) for how these are enforced.
