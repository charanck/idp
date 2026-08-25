# API reference

Everything under `/api/v1/` is the app's only programmatic (non-browser) surface — creating
applications, environments, configs/secrets, feature flags, service clients, and viewing/rolling
back config history is web-UI-only. There is no JWT-based user/service auth API and no generic
CRUD API.

For worked examples in cURL, Python, Node.js/TypeScript, and Go, see the [Guides](guides/config-and-secrets.md).

## Config / secrets

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/api/v1/config/configs/list` | GET | `X-API-Key` | List configs/secrets for `?service=&environment=`, re-encrypted with the calling client's own key. |

Details: [Config & Secrets guide](guides/config-and-secrets.md).

## Feature flags

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/api/v1/config/feature-flags` | GET | `X-API-Key` | List flags for `?service=&environment=`. |

Details: [Feature Flags guide](guides/feature-flags.md).

## Notifications

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/api/v1/notifications` | POST | `X-API-Key` | Create (queue) a notification on `email`/`sms`/`whatsapp`/`inapp`. |
| `/api/v1/notifications` | GET | `X-API-Key` | List notifications, filterable by `?channel=&status=`. |
| `/api/v1/notifications/:id` | GET | `X-API-Key` | Get a single notification by ID. |
| `/api/v1/notifications/sessions` | POST | `X-API-Key` | Mint a short-lived bearer token scoped to one `user_id`, for the two end-user endpoints below. |
| `/api/v1/notifications/sse/events` | GET | `Bearer <token>` | Stream that user's delivery events in real time (push, not persisted). |
| `/api/v1/notifications/inapp/unread` | GET | `Bearer <token>` | Fetch and mark-read that user's unread in-app notifications (pull, persisted). |

Details: [Notifications guide](guides/notifications.md), [Realtime events (SSE)](guides/sse.md),
[In-app inbox](guides/inapp-inbox.md).

## Authentication

Two distinct credentials are used across these endpoints, never interchangeably:

- **`X-API-Key: <key_id>.<secret>`** — a service client's own credential, issued once at
  creation time (**Service Clients → Create** in the web UI). Used by a *service* calling the API
  on its own behalf: reading configs/flags, and creating/listing/getting notifications.
- **`Authorization: Bearer <token>`** — a short-lived token minted by
  `POST /api/v1/notifications/sessions`, scoped to one end user. Used when the *caller is that
  user* (or something acting on their behalf, e.g. a browser tab), not the service client — the
  SSE stream and the in-app inbox.

Failed `X-API-Key` attempts are rate-limited per client IP (`S2S_AUTH_RATE_LIMIT`) — see
[Configuration](configuration.md#rate-limiting).

## Encryption model

1. **Write** — an admin creates a config/secret via the web UI. The value is encrypted with
   `MASTER_ENCRYPTION_KEY` before it's stored; the UI never echoes the plaintext back.
2. **Read** — a service client calls `GET /api/v1/config/configs/list` with its `X-API-Key`. The
   server decrypts with the master key and **re-encrypts with that client's own encryption key**
   (generated once, at client-creation time) before returning it.
3. **Client-side decrypt**:
   ```python
   from cryptography.fernet import Fernet

   fernet = Fernet(client_encryption_key.encode())
   decrypted = fernet.decrypt(encrypted_value.encode()).decode()
   ```

See the [Config & Secrets guide](guides/config-and-secrets.md) for full client examples, and
[Architecture](architecture.md#encryption-flow) for how this fits together end to end.

## Config history and rollback

Every config/secret write is snapshotted as an immutable version. From a config's detail page in
the web UI, open its history to see prior versions (secret values are never shown, only that a
version changed, when, and by whom) and roll back to any of them — a rollback is recorded as a new
version rather than rewriting history. There is no S2S endpoint for this; it's web-UI-only.

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
3. Users can now log in via **"Sign in with &lt;Provider&gt;"** on the login page
   (`/oauth/login/<provider-id>/`).
