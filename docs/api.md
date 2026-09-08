# API reference

Everything under `/api/v1/` is the app's only programmatic (non-browser) surface — creating
applications, environments, configs/secrets, feature flags, service clients, and viewing/rolling
back config history is web-UI-only. There is no JWT-based user/service auth API and no generic
CRUD API.

For worked examples in cURL, Python, Node.js/TypeScript, and Go, see the [Guides](guides/config-and-secrets.md).

## Config / secrets

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/api/v1/config/configs/list` | GET | `X-API-Key` | List configs/secrets for `?service=&environment=`, re-encrypted with the calling client's own key. Returns an array of per-entry objects (`id`/`service`/`environment`/`key`/`value`/`is_secret`/`type`). |
| `/api/v1/config/v2/configs/list` | GET | `X-API-Key` | Same scope/auth as v1, but returns a flat `{"KEY": "value", ...}` map instead of an array of objects — smaller payload for clients that only need values. Additive: v1 is unchanged. |

Details: [Config & Secrets guide](guides/config-and-secrets.md).

## Feature flags

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/api/v1/config/feature-flags` | GET | `X-API-Key` | List flags for `?service=&environment=`. |

Details: [Feature Flags guide](guides/feature-flags.md).

## Notifications

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/api/v1/notifications` | POST | `X-API-Key` | Create (queue) a notification on `email`/`sms`/`inapp`. |
| `/api/v1/notifications` | GET | `X-API-Key` | List notifications, filterable by `?channel=&status=`. |
| `/api/v1/notifications/:id` | GET | `X-API-Key` | Get a single notification by ID. |
| `/api/v1/notifications/sessions` | POST | `X-API-Key` | Mint a short-lived bearer token scoped to one `user_id`, for the two end-user endpoints below. |
| `/api/v1/notifications/sse/events` | GET | `Bearer <token>` | Stream that user's delivery events in real time (push, not persisted). |
| `/api/v1/notifications/inapp/unread` | GET | `Bearer <token>` | Fetch and mark-read that user's unread in-app notifications (pull, persisted). |

Details: [Notifications guide](guides/notifications.md), [Realtime events (SSE)](guides/sse.md),
[In-app inbox](guides/inapp-inbox.md).

## OIDC Identity Provider

Stateless, machine-facing endpoints for applications that log their users in via control-plane
(mounted at root, not under `/api/v1`, since these paths are conventionally root-level):

| Endpoint | Method | Auth | Purpose |
|---|---|---|---|
| `/.well-known/openid-configuration` | GET | none | OIDC discovery document. |
| `/.well-known/jwks.json` | GET | none | RS256 public signing key(s), for verifying ID tokens. |
| `/oauth2/token` | POST | `client_id`/`client_secret` (body) | Exchanges an authorization code for ID/access tokens. |
| `/oauth2/userinfo` | GET | `Authorization: Bearer <access_token>` | Returns claims for the token's subject. |

The browser-facing half, `GET/POST /oauth2/authorize`, is session-authenticated (web UI), not
listed here — see [OIDC Identity Provider setup](#setting-up-control-plane-as-an-oidc-identity-provider)
below.

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

## OAuth2 / OIDC — two independent, opposite directions

Don't confuse these — they share the word "OIDC" but nothing else:

- **control-plane's users logging in via an external IdP** (Google, GitHub, Okta, ...) — see
  [OAuth Providers: control-plane as relying party](#oauth-providers-control-plane-as-relying-party).
- **control-plane acting as the Identity Provider** for *other* applications' users — see
  [OIDC Identity Provider setup](#setting-up-control-plane-as-an-oidc-identity-provider).

### OAuth Providers: control-plane as relying party

The web UI supports signing in through an external OAuth2/OIDC provider instead of a local
password.

1. As an admin, go to **OAuth Providers** in the web UI and add one, with:
   - `client_id` / `client_secret` — from the provider's app registration.
   - `authorization_url`, `token_url`, and optionally `userinfo_url` (OIDC).
   - `scope` — space-separated, defaults to `openid email profile`.
   - `auto_create_users` — if enabled, a user is created automatically on first login via this
     provider (subject to the login policy's self-registration domain allow-list, if set).
2. Register the provider's callback URL with the provider itself:
   `http://<your-host>/oauth/callback/<provider-id>/`.
3. Users can now log in via **"Sign in with &lt;Provider&gt;"** on the login page
   (`/oauth/login/<provider-id>/`).

### Setting up control-plane as an OIDC Identity Provider

Any application can redirect its users to control-plane to log in via a standard
authorization-code flow, the same way it would with Google or Okta.

1. As an admin, go to **Service Clients → Create**, check **Enable as auth application**, and add:
   - One or more **redirect URIs** the application will send `code` back to.
   - Optionally, an **allowed groups** list — restricts login through this application to users in
     those Groups; empty allows any logged-in user.
   - **Require consent**, if the application's users should see a consent screen before the first
     token issuance.
2. Copy the client's `api_key_id` as the OIDC `client_id`, and its API key secret as the
   `client_secret` — same credential pair as the S2S API, reused as OIDC client credentials.
3. Point the application's OIDC library at:
   - Discovery: `http://<your-host>/.well-known/openid-configuration`
   - Authorize: `http://<your-host>/oauth2/authorize`
   - Token: `http://<your-host>/oauth2/token`
   - Userinfo: `http://<your-host>/oauth2/userinfo`
   - JWKS: `http://<your-host>/.well-known/jwks.json`

Tokens are RS256-signed with a per-install signing key generated on first use and stored encrypted
(`internal/model/auth.OIDCSigningKey`); rotate it by deleting that row (all previously issued
tokens become unverifiable — plan for an application restart/re-login).
