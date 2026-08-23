# API reference

The config/flag read API is mounted under `/api/v1/config/`. It's intentionally the only
programmatic (non-browser) API this app exposes — everything else (creating applications,
environments, configs/secrets, feature flags, service clients, and viewing/rolling back config
history) is done through the session-authenticated web UI. There is no JWT-based user/service auth
API.

## Config / secrets

Configs and secrets share one model (`ConfigEntry`); `is_secret=true` is what makes a value a
secret.

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/config/configs/list` | GET | API key | List configs for `?service=&environment=`, re-encrypted with the calling client's own key. |

## Feature flags

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/config/feature-flags` | GET | API key | List flags for `?service=&environment=`. |

Both endpoints authenticate with `X-API-Key: <key_id>.<secret>`, issued when a service client is
created from the web UI (**Service Clients → Create**). Failed API-key attempts are rate-limited
per client IP (`S2S_AUTH_RATE_LIMIT`); a valid key is never throttled.

---

## Examples

### Read configs as a service client

```bash
curl "http://localhost:8000/api/v1/config/configs/list?service=my-app&environment=prod" \
  -H "X-API-Key: <key_id>.<secret>"
```

### Read feature flags as a service client

```bash
curl "http://localhost:8000/api/v1/config/feature-flags?service=my-app&environment=prod" \
  -H "X-API-Key: <key_id>.<secret>"
```

---

## Encryption model

1. **Write** — an admin creates a config/secret via the web UI. The value is encrypted with
   `MASTER_ENCRYPTION_KEY` before it's stored; the UI never echoes the plaintext back.
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

## Config history and rollback

Every config/secret write is snapshotted as an immutable version. From a config's detail page in
the web UI, open its history to see prior versions (secret values are never shown, only that a
version changed, when, and by whom) and roll back to any of them — a rollback is recorded as a new
version rather than rewriting history.

## OAuth2 / OIDC login

The web UI supports signing in through an external OAuth2/OIDC provider (Google, GitHub, Okta, an
internal IdP, etc.) instead of a local password.

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
