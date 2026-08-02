<p align="center">
  <img src="web_ui/static/web_ui/img/logo.svg" width="72" height="72" alt="IDP logo">
</p>

<h1 align="center">IDP — Internal Developer Platform</h1>

<p align="center">
  A Django + Django Ninja control plane for configuration &amp; secret management and feature flags,
  with JWT user auth, API-key service-to-service (S2S) auth, OAuth2/OIDC login, and a
  server-rendered Bootstrap web UI.
</p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT License"></a>
  <img src="https://img.shields.io/badge/python-3.12%2B-blue.svg" alt="Python 3.12+">
  <img src="https://img.shields.io/badge/django-6.0-0C4B33.svg" alt="Django 6.0">
</p>

---

## Features

- **Configuration & secrets** scoped per application + environment, **encrypted at rest** and
  re-encrypted per service client on read — a client only ever sees ciphertext it can decrypt with
  its own key.
- **Feature flags**, per application + environment.
- **Two auth systems** — stateless JWT/API-key auth for the API (`/api/v1/...`), and standard
  session auth for the web UI, gated by role.
- **OAuth2 / OIDC login** for the web UI via [authlib](https://authlib.org/).
- **Config version history & rollback** — every write is snapshotted; roll back to any prior
  version without losing the audit trail.
- **Append-only activity log** of who did what, from where.
- **Rate limiting** on auth endpoints and failed S2S API-key attempts.
- **Optional Redis caching** for read-heavy config/flag lookups.

See [`docs/`](./docs) for the full architecture, API reference, configuration, deployment, and
security docs.

## Quick start

### Prerequisites

- Python 3.12+
- [uv](https://docs.astral.sh/uv/) — dependency manager used by this project

### Run it

```bash
git clone https://github.com/charanck/idp.git
cd idp
uv sync --group dev

cp .env.example .env
# edit .env: set DJANGO_SECRET_KEY, JWT_SECRET_KEY, MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD
# generate a MASTER_ENCRYPTION_KEY with:
#   uv run python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"

uv run python manage.py migrate
uv run python manage.py runserver
```

The admin user (from `ADMIN_EMAIL`/`ADMIN_PASSWORD`) is provisioned automatically on first
`migrate` — see [Configuration](./docs/configuration.md#initial-admin-user) for details on
changing it afterwards.

The app is now available at:

- **Web UI** — http://localhost:8000/
- **API docs (Swagger)** — http://localhost:8000/api/v1/docs
- **Django admin** — http://localhost:8000/admin/

### Or run it with Docker

```bash
cp .env.example .env
# then set MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD in .env
docker compose -f docker-compose.local.yml --env-file .env up --build
```

See [Deployment](./docs/deployment.md) for the production Docker image, Compose stack, and GHCR
releases.

## Running tests

```bash
uv run pytest
```

See [CONTRIBUTING.md](./CONTRIBUTING.md) for the full dev workflow.

## Client examples

Runnable examples for fetching + decrypting configs/secrets and reading feature flags from
Python, Go, Node.js, and TypeScript are in [`examples/`](./examples).

## Documentation

| | |
|---|---|
| [Architecture](./docs/architecture.md) | Apps, auth systems, encryption flow, caching, config history/rollback. |
| [API reference](./docs/api.md) | Endpoints, curl examples, encryption model, OAuth2/OIDC setup. |
| [Configuration](./docs/configuration.md) | Every environment variable. |
| [Deployment](./docs/deployment.md) | Docker, Docker Compose, production checklist, GHCR releases. |
| [Security](./docs/security.md) | Security model and vulnerability reporting. |

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](./CONTRIBUTING.md) for dev setup, testing, and
PR guidelines.

## License

[MIT](./LICENSE)
