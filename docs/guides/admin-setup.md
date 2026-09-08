# Admin Setup Guide

A tutorial for admins setting up a fresh install: securing the initial login, organizing users
into Groups, registering applications, issuing service-client API keys, wiring up
notifications/OAuth/OIDC, and the operational pages you'll come back to (activity log, dashboard).

Assumes the server is already running — see [Getting Started](../getting-started.md) if not.

## 1. Log in and secure the admin account

Log in at `http://localhost:8000/` with `ADMIN_EMAIL` / `ADMIN_PASSWORD`. First thing, change the
password from **Profile** (top-right) rather than leaving the `.env` default in place.

`ADMIN_EMAIL`/`ADMIN_PASSWORD` are re-synced on **every server startup** — if you change the
password only in `.env` and restart, it reverts. Change it in the web UI, and leave the env vars
matching what you actually want the account to be if you ever need to re-provision it.

## 2. Set a login policy

Go to **Policies** (admin-only). This is a single, instance-wide policy record covering:

- **Password rules** — minimum length, upper/lower/digit/symbol requirements, max age (days).
- **Account lockout** — max failed login attempts before a temporary lockout, and the lockout
  duration.
- **Session idle timeout** — minutes of inactivity before a session is force-destroyed.
- **Login IP allow-list** — restrict `POST /login/` to specific CIDRs; empty allows any IP.
- **SSO-only** — when on, password login is rejected outright and users must use an OAuth
  provider (set one up in step 10 first, or you'll lock yourself out).
- **Self-registration allowed domains** — moot today, since the `/register/` route is disabled at
  the handler level regardless of this setting; leave blank.

Reasonable defaults ship out of the box — tighten as needed before onboarding real users.

## 3. Create Groups for your team

Go to **Groups**. Three built-in groups always exist and can't be deleted:

| Group | Module access |
|---|---|
| **Admin** | everything |
| **Developer** | Dashboard, Configs, Flags |
| **User** | nothing beyond their own profile page — the default for new accounts |

For anything in between — someone who should manage Feature Flags but not Service Clients, say —
create a **custom group**: pick a name, check the modules it grants
(`applications`/`environments`/`configs`/`flags`/`service_clients`/`users`/`groups`/
`oauth_providers`/`policies`/`branding`/`notification_settings`/`activity_log`/`dashboard`), and
optionally restrict it to specific **Applications** (leave unrestricted to see every Application).
A user's actual permissions are the union of every group they're in.

## 4. Create users

Go to **Users → Create**. Set an email, a temporary password (or let them set one via forced
reset — see below), and assign one or more Groups. New users default into **User** (no access) if
you don't pick a group explicitly.

Useful per-user actions from the user list/detail page:

- **Force password reset** — the user must set a new password on next login.
- **Unlock** — clears an active lockout from the Policy's failed-attempt threshold, before it
  expires on its own.

Admins can't change their own group membership or active status — have a second admin do it if
you ever need to demote yourself.

## 5. Register an Application and Environment

Go to **Applications → Create** and name it after the service that'll own it (e.g. `orders-api`).
Then, under that application, **Environments → Create** one per deploy target (`dev`, `staging`,
`prod`, ...) — configs, secrets, and feature flags are all scoped to one `(application,
environment)` pair.

You don't strictly have to pre-create these: `ConfigService`/`FeatureFlagService` **get-or-create**
the Application/Environment from the `service`/`environment` strings a config or flag is written
against. Creating them explicitly upfront just lets you scope Groups/Service Clients to a
predictable Application name before anything's been written yet.

## 6. Add configs, secrets, and feature flags

From **Configs → Create**, add a key/value pair scoped to a service+environment; check
**This is a secret** for anything sensitive — secret values are encrypted at rest and the web UI
never displays them back once saved (`***ENCRYPTED***`). Every write is versioned; open a config's
detail page to see its history and roll back to a prior version.

**Flags → Create** works the same way, minus encryption — just a name, description, and an
on/off toggle scoped to a service+environment.

See [Config & Secrets](config-and-secrets.md) and [Feature Flags](feature-flags.md) for reading
these back programmatically.

## 7. Create a Service Client (S2S API access)

Go to **Service Clients → Create**. This is what an application's backend uses to call the
config/flag/notification API on its own behalf. Set:

- **Applications** — the config/flag scope this client can read from (empty = unrestricted).

On creation, the client's **API key** (`<key_id>.<secret>`) and **encryption key** are shown
**exactly once** — copy both immediately, there's no way to retrieve them again (rotate from the
client's detail page if lost). The application uses the API key as `X-API-Key` on every request,
and the encryption key to decrypt secret values the server re-encrypts for it on read — see
[Config & Secrets: encryption model](../api.md#encryption-model).

```bash
curl "http://localhost:8000/api/v1/config/configs/list?service=orders-api&environment=prod" \
  -H "X-API-Key: <key_id>.<secret>"
```

## 8. (Optional) Let a reverse proxy gate another app with this login

If you have another internal app that shouldn't implement its own auth, forward-auth lets a
reverse proxy (nginx, Traefik, oauth2-proxy-style) delegate to control-plane's session instead.

1. Create (or reuse) a Service Client, and on its edit page set **Domains** to the hostname(s) the
   proxy will forward for (e.g. `internal-tool.example.com`, one per line) and check
   **Enable as proxy-auth application**.
2. Set that client's **Allowed Groups** to whichever Groups should be let in (empty = any
   logged-in user).
3. Point your proxy's auth-request/forwardAuth config at `GET /forward-auth/verify` — full nginx
   and Traefik examples are in
   [Configuration: Forward-auth Identity Provider](../configuration.md#forward-auth-identity-provider).

This client's API key isn't used for forward-auth itself (there's no S2S call involved) — it's
just the vehicle for the Domains/Allowed-Groups settings. The same client can also double as an S2S
config/flag reader or an OIDC auth application if you want, or be dedicated solely to this.

## 9. (Optional) Let another application log its users in via control-plane (OIDC IdP)

For an application that should use control-plane as a full Identity Provider (its users log in
*through* control-plane, not just behind a proxy), create a Service Client with **Enable as auth
application** checked and:

- **Redirect URIs** — one per line, where the application expects the authorization code back.
- **Allowed Groups** — restrict login through this application to specific Groups (empty = any
  logged-in user).
- **Require consent screen** — show a consent screen before first token issuance.

The client's `api_key_id`/API-key-secret double as the OIDC `client_id`/`client_secret`. Point the
application's OIDC library at `/.well-known/openid-configuration` and you're done — full endpoint
list in [API reference: OIDC Identity Provider](../api.md#oidc-identity-provider).

## 10. (Optional) Let your users log in via Google/GitHub/Okta instead of a password

Opposite direction from step 9: this is *control-plane's own users* authenticating via an
external provider. Go to **OAuth Providers → Create** with the provider's `client_id`/
`client_secret`, authorization/token/userinfo URLs, and scope; register
`http://<your-host>/oauth/callback/<provider-id>/` as the callback URL with the provider. Enable
**Auto-create users** if first-time logins should provision an account automatically (gated by the
Policy's self-registration domain allow-list from step 2, if set). Full details in
[API reference: OAuth Providers](../api.md#oauth-providers-control-plane-as-relying-party).

## 11. (Optional) Turn on notifications

Notifications (email/SMS/in-app) are always available in the API; only **email** has a real
provider wired up, over SMTP. Go to **Notification Settings → Email → Edit** and set the SMTP
host, port, from address/name, TLS mode (`none`/`starttls`/`tls`), and credentials, then mark it
active. Once active, a service client can queue notifications:

```bash
curl -X POST "http://localhost:8000/api/v1/notifications" \
  -H "X-API-Key: <key_id>.<secret>" -H "Content-Type: application/json" \
  -d '{"service":"orders-api","channel":"email",
       "recipient":{"email":"user@example.com"},
       "content":{"subject":"Order shipped","body":"..."}}'
```

Delivery happens asynchronously via a durable background worker with retries — see
[Notifications](notifications.md), [Realtime events (SSE)](sse.md), and
[In-app inbox](inapp-inbox.md) for the full flow, including how end users (not the service client)
pull their own notifications.

## 12. (Optional) Branding

**Branding** lets you set a product name, logo URL, accent color, and login-page background image
— cosmetic only, shown across the web UI and login page.

## Day-to-day: Dashboard and Activity Log

- **Dashboard** — live counts (applications, environments, configs, secrets, active flags,
  clients) plus hourly trend snapshots going back 7 days.
- **Activity Log** — an append-only audit trail of every create/update/delete across the app: who,
  what, and when. Retained for 180 days, then pruned automatically by a monthly cleanup job.

## Next

- [Architecture](../architecture.md) — how all of this fits together under the hood.
- [Configuration](../configuration.md) — every environment variable.
- [Security](../security.md) — the security model this setup relies on.
