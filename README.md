<p align="center">
  <img src="web_ui/static/web_ui/img/logo.svg" width="72" height="72" alt="IDP logo">
</p>

<h1 align="center">IDP — Internal Developer Platform</h1>

<p align="center">
  A Django + Django Ninja control plane for configuration &amp; secret management and feature flags,
  with JWT user auth, API-key service-to-service (S2S) auth, OAuth2/OIDC login, and a
  server-rendered Bootstrap web UI.
</p>

---

## What it does

- **Configuration & secrets** — `ConfigEntry` records scoped per application + environment. Secrets
  are the same model with `is_secret=true`; values are **encrypted at rest** with a master key and
  **re-encrypted per service client** on read, so a client only ever sees ciphertext it can decrypt
  with its own key.
- **Feature flags** — toggle features per application + environment, with soft delete.
- **Two independent auth systems**:
  - **API** (`/api/v1/...`) — stateless, JWT bearer tokens for users/admins and `X-API-Key` for
    service-to-service calls.
  - **Web UI** (`/...`) — standard Django session auth, gated by `is_staff` for admin screens.
- **OAuth2 / OIDC login** — authorization-code flow via [authlib](https://authlib.org/) for signing
  into the web UI through an external identity provider.
- **Activity log** — append-only audit trail of who did what, from where.
- **Optional Redis (or in-memory) caching** for the read-heavy config/flag list endpoints, with
  version-based invalidation.

## Architecture

Four Django apps, each with a narrow role:

| App | Responsibility |
|---|---|
| `control_plane_project/` | Settings, root URLs. Mounts the Ninja API at `/api/v1/`, Django admin at `/admin/`, web UI at `/`. |
| `authentication/` | Custom `User` model, `ServiceClient` (S2S API keys), OAuth provider/token models, JWT issuance. |
| `config_management/` | `Application` → `Environment` → `ConfigEntry` / `FeatureFlag`, plus the `Activity` audit log. |
| `web_ui/` | Server-rendered CRUD for everything above (Bootstrap templates), OAuth login/callback, activity log view. |
| `common/` | Shared utilities: Fernet encryption, JWT helpers, Ninja auth classes, password hashing, activity logging. |

See [CLAUDE.md](./CLAUDE.md) for a deeper architectural walkthrough (encryption flow, caching
invalidation strategy, etc.).

---

## Quick start (local, no Docker)

### Prerequisites
- Python 3.12+
- [uv](https://docs.astral.sh/uv/) — dependency manager used by this project
- SQLite (bundled, default) or PostgreSQL if you want to match production

### 1. Clone and install dependencies

```bash
git clone <repository-url>
cd control-plane
uv sync --group dev
```

`uv sync` creates `.venv` and installs everything from `uv.lock`. `--group dev` adds `pytest` /
`pytest-django` for running tests.

### 2. Configure environment variables

```bash
cp .env.example .env
```

Then edit `.env`. At minimum, set:

- `DJANGO_SECRET_KEY`, `JWT_SECRET_KEY` — any long random strings for local dev.
- `MASTER_ENCRYPTION_KEY` — generate with:
  ```bash
  uv run python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"
  ```
  If left unset, a key is auto-generated at startup **for convenience only** — it changes every
  restart, so encrypted data becomes unreadable. Never leave this unset in production.
- `ADMIN_EMAIL` / `ADMIN_PASSWORD` — see [Admin user](#admin-user) below.

The full variable list, with defaults, is documented in [`.env.example`](./.env.example).

### 3. Run migrations and start the server

```bash
uv run python manage.py migrate
uv run python manage.py runserver
```

The admin user (if `ADMIN_EMAIL`/`ADMIN_PASSWORD` are set) is provisioned automatically as part of
the `authentication` app's migrations — no separate step needed.

The app is now available at:

- **Web UI** — http://localhost:8000/
- **API docs (Swagger)** — http://localhost:8000/api/v1/docs
- **Django admin** — http://localhost:8000/admin/

### Admin user

On first `migrate`, if `ADMIN_EMAIL` and `ADMIN_PASSWORD` are set, a superuser is created once with
`force_password_reset=True` (you'll be prompted to change the password on first login). This only
ever happens **once**: if a user with that email already exists, startup leaves it untouched —
changing `ADMIN_PASSWORD` in `.env` later and restarting will **not** reset the password. To
change admin credentials afterwards, use the web UI or:

```bash
uv run python manage.py setup_admin --email admin@example.com --password NewSecurePass123
```

---

## Running tests

```bash
DJANGO_SETTINGS_MODULE=control_plane_project.settings uv run pytest
# or
uv run python manage.py test
```

---

## Docker

### Local development (build from source)

```bash
cp .env.example .env
# then set MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD in .env
docker compose -f docker-compose.local.yml --env-file .env up --build
```

This builds the image from the repo's `Dockerfile`, and brings up Postgres and Redis alongside the
app — everything needed to run IDP locally with no other setup. The app is available at
http://localhost:8000/ once the containers are healthy.

> `docker-compose.yml` (no suffix) is a separate, pre-existing deployment config pointed at a
> specific staging host and the published GHCR image — don't use it for local development.

### Build/run the image directly

```bash
docker build -t idp:local .
docker run --rm -p 8000:8000 --env-file .env idp:local
```

The container runs migrations at startup and then starts Django on `0.0.0.0:8000`.

### GHCR release publishing

A GitHub Actions workflow publishes a container image to GHCR when you push a tag matching
`release-*` or `release/*`:

```bash
git tag release-1.0.0
git push origin release-1.0.0
```

Published as `ghcr.io/<owner>/<repo>:release-1.0.0` and `ghcr.io/<owner>/<repo>:latest`.

---

## Encryption model

1. **Write** — an admin creates a config/secret (`POST /api/v1/config/configs/upsert`, JWT auth, or via
   the web UI). The value is encrypted with `MASTER_ENCRYPTION_KEY` before it's stored; the API/UI
   response never echoes the plaintext back (secrets show as `***ENCRYPTED***`).
2. **Read** — a service client calls `GET /api/v1/config/configs/list` with its `X-API-Key`. The server
   decrypts with the master key and **re-encrypts with that client's own encryption key** (generated
   once, at client-creation time) before returning it.
3. **Client-side decrypt**:
   ```python
   from cryptography.fernet import Fernet

   fernet = Fernet(client_encryption_key.encode())
   decrypted = fernet.decrypt(encrypted_value.encode()).decode()
   ```

Losing `MASTER_ENCRYPTION_KEY` makes all stored config/secret values permanently unrecoverable —
back it up somewhere durable outside the app.

---

## API examples

### Get a user JWT

```bash
curl -X POST http://localhost:8000/api/v1/auth/token \
  -H "Content-Type: application/json" \
  -d '{"username": "admin@example.com", "password": "<password>"}'
```

### Create a service client (admin JWT required)

```bash
curl -X POST http://localhost:8000/api/v1/auth/s2s/clients \
  -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-service"}'
```

The response includes `api_key` and `encryption_key` — both are shown once, at creation time. Store
them securely; they can't be retrieved again.

### Write a config/secret (admin JWT required)

```bash
curl -X POST http://localhost:8000/api/v1/config/configs/upsert \
  -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{"service": "my-app", "environment": "prod", "key": "DB_PASSWORD", "value": "secret-value", "is_secret": true}'
```

### Read configs as a service client

```bash
curl "http://localhost:8000/api/v1/config/configs/list?service=my-app&environment=prod" \
  -H "X-API-Key: <key_id>.<secret>"
```

Full endpoint reference is available at `/api/v1/docs` once the server is running.

---

## Security notes

- Set `MASTER_ENCRYPTION_KEY`, `DJANGO_SECRET_KEY`, and `JWT_SECRET_KEY` explicitly in every
  non-local environment — never rely on the auto-generated dev fallback.
- Use PostgreSQL (not SQLite) and `DEBUG=False` in production.
- Admin (`is_staff`) users cannot change their own `is_staff`/`is_active` flags through the web UI —
  role changes must come from a different admin, to prevent self privilege-escalation.
- Service client API keys and encryption keys are shown once at creation and stored hashed/encrypted
  thereafter — there is no way to retrieve them again through the app.

---

## License

MIT
