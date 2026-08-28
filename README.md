<p align="center">
  <img src="assets/logo.svg" width="72" height="72" alt="IDP logo">
</p>

<h1 align="center">IDP — Internal Developer Platform</h1>

<p align="center">
  A Go control plane for configuration &amp; secret management and feature flags, with API-key
  service-to-service (S2S) auth, OAuth2/OIDC login, and a server-rendered Bootstrap web UI.
</p>

<p align="center">
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="MIT License"></a>
  <img src="https://img.shields.io/badge/go-1.25%2B-00ADD8.svg" alt="Go 1.25+">
</p>

---

## Features

- **Configuration & secrets** scoped per application + environment, **encrypted at rest** and
  re-encrypted per service client on read — a client only ever sees ciphertext it can decrypt with
  its own key.
- **Feature flags**, per application + environment.
- **API-key (S2S) auth** for the config/flag API (`/api/v1/config/...`), and standard
  session auth for the web UI, gated by role.
- **OAuth2 / OIDC login** for the web UI.
- **Config version history & rollback** — every write is snapshotted; roll back to any prior
  version without losing the audit trail.
- **Append-only activity log** of who did what, from where.
- **Rate limiting** on the login endpoint and failed S2S API-key attempts.
- **Redis-backed caching** for read-heavy config/flag lookups.

📖 **Full documentation, including per-API guides in cURL/Python/Node.js/Go, lives in
[`docs/`](./docs).**

## Quick start

### Prerequisites

- Go 1.25+
- A Postgres database and a Redis instance (both required — see
  [Configuration](./docs/configuration.md))

### Run it

```bash
git clone https://github.com/charanck/idp.git
cd idp

cp .env.example .env
# edit .env: set MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD, DB_*, REDIS_URL
# generate a MASTER_ENCRYPTION_KEY with:
#   python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"

go run ./cmd/server
```

The server runs pending schema migrations and provisions/syncs the admin user (from
`ADMIN_EMAIL`/`ADMIN_PASSWORD`) automatically on startup — see
[Configuration](./docs/configuration.md#initial-admin-user) for details on changing it afterwards.

The app is now available at:

- **Web UI** — http://localhost:8000/
- **Config/flag S2S API** — http://localhost:8000/api/v1/config/...

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
make test
```

See [CONTRIBUTING.md](./CONTRIBUTING.md) for the full dev workflow.

## Documentation

| | |
|---|---|
| [Getting Started](./docs/getting-started.md) | Run it locally in a few minutes. |
| [Architecture](./docs/architecture.md) | Packages, auth systems, encryption flow, caching, config history/rollback. |
| [Configuration](./docs/configuration.md) | Every environment variable. |
| Guides | Worked examples (cURL/Python/Node.js/Go) for [config & secrets](./docs/guides/config-and-secrets.md), [feature flags](./docs/guides/feature-flags.md), [notifications](./docs/guides/notifications.md), [SSE](./docs/guides/sse.md), and the [in-app inbox](./docs/guides/inapp-inbox.md). |
| [API reference](./docs/api.md) | Full endpoint list, auth model, encryption model, OAuth2/OIDC setup. |
| [Deployment](./docs/deployment.md) | Docker, Docker Compose, production checklist, GHCR releases. |
| [Security](./docs/security.md) | Security model and vulnerability reporting. |

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](./CONTRIBUTING.md) for dev setup, testing, and
PR guidelines.

## License

[MIT](./LICENSE)
