# Getting Started

## Prerequisites

- Go 1.25+
- A Postgres database and a Redis instance — both are required, with no fallback (see
  [Configuration](configuration.md))

## Run it locally

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
`ADMIN_EMAIL`/`ADMIN_PASSWORD`) automatically on every startup — see
[Configuration](configuration.md#initial-admin-user) for details on changing it afterwards.

The app is now available at:

- **Web UI** — <http://localhost:8000/>
- **S2S JSON API** — <http://localhost:8000/api/v1/...>

## Or run it with Docker

```bash
cp .env.example .env
# then set MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD in .env
docker compose -f docker-compose.local.yml --env-file .env up --build
```

This builds the image and brings up Postgres and Redis alongside the app — everything needed to
run it with no other setup. See [Deployment](deployment.md) for the production image, Compose
stack, and GHCR releases.

## First steps in the web UI

1. Log in at `/` with `ADMIN_EMAIL` / `ADMIN_PASSWORD`.
2. Create an **Application** and an **Environment** under it.
3. Add a **Config** or **Secret** (`is_secret=true`) entry.
4. Create a **Service Client** (**Service Clients → Create**) — this is the only place its API
   key (`<key_id>.<secret>`) and its per-client encryption key are ever shown; copy them now.
5. Use that API key to read the config back over the S2S API — see
   [Config & Secrets](guides/config-and-secrets.md).

## Next

- [Guides](guides/config-and-secrets.md) — worked examples for every API, in cURL, Python,
  Node.js/TypeScript, and Go.
- [Architecture](architecture.md) — how the pieces fit together.
- [API Reference](api.md) — the full endpoint reference.
