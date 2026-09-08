# Configuration

All configuration is via environment variables, loaded from `.env` in development (see
[`.env.example`](https://github.com/charanck/idp/blob/master/.env.example) for a ready-to-copy
template).

## Core

| Variable | Default | Notes |
|---|---|---|
| `DEBUG` | `false` | Set `false` in every non-local environment. |
| `SESSION_SECRET` | insecure dev default | Signs session cookies and CSRF tokens. Set explicitly outside local dev. |
| `COOKIE_DOMAIN` | *(empty)* | Sets the session cookie's `Domain` attribute. Leave unset for today's exact-host-only behavior; set to a parent domain (e.g. `.example.com`) to share the session across subdomains a reverse proxy protects via [forward-auth](#forward-auth-identity-provider). |
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
| `S2S_AUTH_RATE_LIMIT` | `20` | Max S2S API-key requests per client IP per window — every request counts toward the limit, whether the key is valid or not. |

See [Architecture](./architecture.md#rate-limiting) for how these are enforced.

## Observability and logging

Structured JSON logging to stdout is always on, independent of OTLP export below — this is a
dev tool, so leveled, greppable logs shouldn't require a collector to be configured first.
Every HTTP request is logged (status `>=500` at `error`, `>=400` at `warn`, else `info`), and
`debug` level adds granular detail across auth/session/config/forward-auth flows (login
rejections and why, account lockouts, session idle-timeout destruction, S2S API-key auth
success, config-list cache hit/miss, and forward-auth allow/deny decisions) for local
debugging.

| Variable | Default | Notes |
|---|---|---|
| `LOG_LEVEL` | `debug` | Minimum level for stdout (and, if enabled, OTLP) logs: `debug`/`info`/`warn`/`error`. Set to `info` or `warn` for quieter production logs. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | *(unset)* | Setting this additionally enables OTLP traces/metrics/logs export to a collector; leaving it unset means logs still go to stdout, just without OTLP export. Read directly via `os.Getenv` at startup, before `.env` — if you're only setting it there, make sure it's picked up (see `appconfig.LoadDotEnv`); a real process env var always works. |

## Forward-auth Identity Provider

`GET /forward-auth/verify` lets a reverse proxy gate access to any other app using the
control-plane's own login session, without that app implementing its own auth. It reads the
session cookie set by the web UI and the standard forwarded-request headers
(`X-Forwarded-Host`, `X-Forwarded-Uri`/`X-Original-URL`, `X-Forwarded-Proto`), then:

1. Requires a valid, logged-in session — otherwise denies.
2. Resolves a `ServiceClient` from the forwarded `Host` header (configured under
   **Service Clients → Edit**: a comma/newline-separated list of hostnames, plus the
   **Is Proxy-Auth Enabled** toggle) and requires the logged-in user to be in that client's
   **Allowed Groups** (empty allow-list = any logged-in user) — otherwise denies. A host that
   isn't mapped to any client, or maps to an inactive/proxy-auth-disabled one, always denies
   (fails closed).
3. On success, responds `200` with identity headers: `X-Auth-Request-Email`,
   `X-Auth-Request-User` (user ID), `X-Auth-Request-Groups` (comma-separated group names) — the
   same header names `oauth2-proxy`/nginx `auth_request` setups conventionally expect.
4. On denial, responds `401` by default. Add `?redirect=1` to the verify URL (in the proxy config)
   to instead get a `302` to `/login/?next=<original-url>` — needed for proxies that forward the
   auth response verbatim (e.g. Traefik's `ForwardAuth`) rather than remapping non-2xx codes to a
   login page themselves.

Since the proxy calls `/forward-auth/verify` on a different subdomain per protected app, set
`COOKIE_DOMAIN` to a shared parent domain so the same session cookie is sent to both the
control-plane and every protected app.

**nginx (`auth_request`, remaps `401` via `error_page`):**

```nginx
location = /internal/forward-auth {
    internal;
    proxy_pass https://idp.example.com/forward-auth/verify;
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
    proxy_set_header X-Forwarded-Host $host;
    proxy_set_header X-Forwarded-Uri $request_uri;
    proxy_set_header X-Forwarded-Proto $scheme;
}

server {
    server_name app.example.com;

    error_page 401 = @error401;
    location @error401 {
        return 302 https://idp.example.com/login/?next=$scheme://$host$request_uri;
    }

    location / {
        auth_request /internal/forward-auth;
        auth_request_set $email $upstream_http_x_auth_request_email;
        proxy_set_header X-Auth-Request-Email $email;
        proxy_pass http://app-upstream;
    }
}
```

**Traefik (`ForwardAuth`, forwards the response verbatim — use `?redirect=1`):**

```yaml
http:
  middlewares:
    idp-forward-auth:
      forwardAuth:
        address: "https://idp.example.com/forward-auth/verify?redirect=1"
        authResponseHeaders:
          - X-Auth-Request-Email
          - X-Auth-Request-User
          - X-Auth-Request-Groups

  routers:
    app:
      rule: "Host(`app.example.com`)"
      middlewares:
        - idp-forward-auth
      service: app-upstream
```
