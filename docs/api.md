# API reference

All endpoints are mounted under `/api/v1/`. Interactive Swagger docs are always available at
**`/api/v1/docs`** once the server is running — this page covers auth requirements and worked
examples; the exact request/response shapes are best explored there.

## Auth

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/auth/register` | POST | Admin JWT | Create a new user (`email`, `password`). New users are created `is_active=False` until an admin activates them. |
| `/auth/token` | POST | — | Exchange `username` (email) + `password` for a JWT (`access_token`). Rate-limited. |
| `/auth/me` | GET | User JWT | Current user's profile. |
| `/auth/s2s/clients` | POST | Admin JWT | Create a `ServiceClient`. Response includes `api_key` and `encryption_key` — **shown once**, cannot be retrieved again. |
| `/auth/s2s/ping` | GET | API key | Liveness check for S2S credentials. Failed attempts are rate-limited per IP (`S2S_AUTH_RATE_LIMIT`). |

## Config / secrets

Configs and secrets share one model (`ConfigEntry`); `is_secret=true` is what makes a value a
secret.

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/config/configs/upsert` | POST | Admin/user JWT | Create or update a config/secret for a `(service, environment, key)`. Response always shows `***ENCRYPTED***` for secret values. |
| `/config/configs/list` | GET | API key | List configs for `?service=&environment=`, re-encrypted with the calling client's own key. |
| `/config/configs/{id}/history` | GET | JWT | Version history for one entry. Secret values are never included, only that a version changed, when, and by whom. |
| `/config/configs/{id}/rollback` | POST | JWT | Restore a prior `version`. Recorded as a new version (`action=rollback`), not a rewrite of history. |

## Feature flags

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/config/feature-flags` | POST | JWT | Create a flag for `(service, environment, name)`. |
| `/config/feature-flags` | GET | JWT or API key | List flags for `?service=&environment=`. |
| `/config/feature-flags/{name}/toggle` | POST | JWT | Flip `is_enabled` for `?service=&environment=`. |

---

## Examples

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

The response includes `api_key` and `encryption_key` — both are shown once, at creation time.
Store them securely; they can't be retrieved again (the encryption key can be rotated later from
the client's detail page in the web UI if lost).

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

### View history and roll back a config/secret (admin JWT required)

```bash
curl http://localhost:8000/api/v1/config/configs/<config-id>/history \
  -H "Authorization: Bearer <admin-jwt>"

curl -X POST http://localhost:8000/api/v1/config/configs/<config-id>/rollback \
  -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{"version": 3}'
```

The same views are available in the web UI from a config's history icon.

---

## Encryption model

1. **Write** — an admin creates a config/secret via the API or web UI. The value is encrypted with
   `MASTER_ENCRYPTION_KEY` before it's stored; responses never echo the plaintext back.
2. **Read** — a service client calls `GET /config/configs/list` with its `X-API-Key`. The server
   decrypts with the master key and **re-encrypts with that client's own encryption key** (generated
   once, at client-creation time) before returning it.
3. **Client-side decrypt**:
   ```python
   from cryptography.fernet import Fernet

   fernet = Fernet(client_encryption_key.encode())
   decrypted = fernet.decrypt(encrypted_value.encode()).decode()
   ```

See [`examples/`](../examples) for runnable Python, Go, Node.js, and TypeScript clients that list
configs/secrets and feature flags and decrypt them locally. Full architecture is documented in
[Architecture](./architecture.md#encryption-flow).

## OAuth2 / OIDC login

The web UI supports signing in through an external OAuth2/OIDC provider (Google, GitHub, Okta, an
internal IdP, etc.) instead of a local password, via [authlib](https://authlib.org/)'s
authorization-code flow.

1. As an admin, go to **OAuth Providers** in the web UI and add one, with:
   - `client_id` / `client_secret` — from the provider's app registration.
   - `authorization_url`, `token_url`, and optionally `userinfo_url` (OIDC).
   - `scope` — space-separated, defaults to `openid email profile`.
   - `auto_create_users` — if enabled, a user is created automatically on first login via this
     provider.
2. Register the provider's callback URL with the provider itself:
   `http://<your-host>/oauth/callback/<provider-id>/`.
3. Users can now log in via **"Sign in with <Provider>"** on the login page
   (`/oauth/login/<provider-id>/`).
