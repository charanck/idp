# Deployment

## Docker image

The [`Dockerfile`](../Dockerfile) is a multi-stage, production-ready build:

- **Builder stage** resolves dependencies with [`uv`](https://docs.astral.sh/uv/) into a venv —
  build tools never make it into the final image.
- **Runtime stage** copies only the venv + app code, runs as a non-root user, collects and
  compresses static assets at build time via [WhiteNoise](http://whitenoise.evans.io/) (no separate
  static file server needed), and serves the app with **gunicorn** (not Django's dev server).
- A `HEALTHCHECK` hits `/` with `curl`.
- `docker-entrypoint.sh` runs `manage.py migrate` before handing off to gunicorn as PID 1.

```bash
docker build -t idp:local .
docker run --rm -p 8000:8000 --env-file .env idp:local
```

> Migrations run automatically on every container start. That's fine for a single instance; if you
> scale to multiple replicas, move `migrate` to a separate release/init step to avoid concurrent
> migration races.

## Local stack with Docker Compose

```bash
cp .env.example .env
# then set MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD in .env
docker compose -f docker-compose.local.yml --env-file .env up --build
```

This builds the image from the repo's `Dockerfile` and brings up Postgres and Redis alongside the
app — everything needed to run IDP locally with no other setup. The app is available at
http://localhost:8000/ once the containers are healthy.

## Production checklist

- Set `MASTER_ENCRYPTION_KEY`, `DJANGO_SECRET_KEY`, and `JWT_SECRET_KEY` explicitly — never rely on
  the auto-generated dev fallback. Back up `MASTER_ENCRYPTION_KEY` somewhere durable; losing it
  makes all stored config/secret data permanently unrecoverable.
- Use PostgreSQL (`DB_ENGINE=django.db.backends.postgresql`), not SQLite.
- Set `DEBUG=False` and a real `ALLOWED_HOSTS`.
- If serving behind a TLS-terminating reverse proxy (nginx, Traefik, Dokploy, etc.), set
  `CSRF_TRUSTED_ORIGINS` to your public origin(s) — `SECURE_PROXY_SSL_HEADER` is already configured
  to trust `X-Forwarded-Proto`.
- Set `ADMIN_EMAIL` / `ADMIN_PASSWORD` for the first deploy only — see
  [Configuration](./configuration.md#initial-admin-user).

See [Configuration](./configuration.md) for the full environment variable reference.

## GHCR release publishing

A GitHub Actions workflow ([`.github/workflows/release-ghcr.yml`](../.github/workflows/release-ghcr.yml))
publishes a container image to GHCR when you push a tag matching `release-*` or `release/*`:

```bash
git tag release-1.0.0
git push origin release-1.0.0
```

Published as `ghcr.io/<owner>/<repo>:release-1.0.0` and `ghcr.io/<owner>/<repo>:latest`.
