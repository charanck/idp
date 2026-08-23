# Deployment

## Docker image

The [`server/Dockerfile`](../server/Dockerfile) is a multi-stage, production-ready build:

- **Builder stage** compiles a static Go binary (`CGO_ENABLED=0`) — no runtime Go toolchain ships
  in the final image.
- **Runtime stage** copies only the binary + `static/` assets onto a slim Alpine base, runs as a
  non-root user, and serves the app directly (no separate app server needed — the binary is the
  server).
- A `HEALTHCHECK` hits `/` with `curl`.
- Schema migrations and admin user provisioning run automatically on startup, guarded by a
  Postgres advisory lock (see [`internal/db/db.go`](../server/internal/db/db.go)) — safe for
  multiple replicas starting concurrently.

```bash
cd server
docker build -t idp:local .
docker run --rm -p 8000:8000 --env-file ../.env idp:local
```

## Local stack with Docker Compose

```bash
cp .env.example .env
# then set MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD in .env
docker compose -f server/docker-compose.local.yml --env-file .env up --build
```

This builds the image from [`server/Dockerfile`](../server/Dockerfile) and brings up Postgres and
Redis alongside the app — everything needed to run IDP locally with no other setup. The app is
available at http://localhost:8000/ once the containers are healthy.

## Production checklist

- Set `MASTER_ENCRYPTION_KEY` and `SESSION_SECRET` explicitly — there is no auto-generated dev
  fallback for the encryption key, and the session secret defaults to an insecure value if unset.
  Back up `MASTER_ENCRYPTION_KEY` somewhere durable; losing it makes all stored config/secret data
  permanently unrecoverable.
- Postgres and Redis are both required in every environment, including production — there's no
  degraded/no-cache fallback mode.
- Set `DEBUG=false` and a real `ALLOWED_HOSTS`.
- If serving behind a TLS-terminating reverse proxy (nginx, Traefik, Dokploy, etc.), set
  `CSRF_TRUSTED_ORIGINS` to your public origin(s).
- Set `ADMIN_EMAIL` / `ADMIN_PASSWORD` — these are provisioned/synced on every startup, so keep
  them set (and change them via the web UI or by rotating the env vars and restarting).

See [Configuration](./configuration.md) for the full environment variable reference.

## GHCR release publishing

A GitHub Actions workflow ([`.github/workflows/release-ghcr.yml`](../.github/workflows/release-ghcr.yml))
publishes a container image to GHCR when you push a tag matching `release-*` or `release/*`:

```bash
git tag release-1.0.0
git push origin release-1.0.0
```

Published as `ghcr.io/<owner>/<repo>:release-1.0.0` and `ghcr.io/<owner>/<repo>:latest`, built from
[`server/Dockerfile`](../server/Dockerfile).
