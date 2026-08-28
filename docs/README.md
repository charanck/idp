# Documentation

A Go control plane for configuration & secret management and feature flags, with API-key
service-to-service (S2S) auth, OAuth2/OIDC login, a notification system (email/SMS/WhatsApp/in-app,
with realtime SSE delivery), and a server-rendered web UI.

- **Configuration & secrets** scoped per application + environment, **encrypted at rest** and
  re-encrypted per service client on read — a client only ever sees ciphertext it can decrypt with
  its own key. See [Config & Secrets](./guides/config-and-secrets.md).
- **Feature flags**, per application + environment. See [Feature Flags](./guides/feature-flags.md).
- **Notifications** across email, SMS, WhatsApp, and in-app channels, processed by a background
  worker with retries. See [Notifications](./guides/notifications.md).
- **Realtime delivery events over SSE** and a **persisted, pull-based in-app inbox** for
  user-facing notifications. See [Realtime events](./guides/sse.md) and
  [In-app inbox](./guides/inapp-inbox.md).
- **API-key (S2S) auth** for every programmatic endpoint, and session auth (with OAuth2/OIDC
  support) for the web UI.
- **Config version history & rollback** — every write is snapshotted; roll back to any prior
  version without losing the audit trail.
- **Append-only activity log**, Redis-backed caching, and rate limiting on auth-sensitive
  endpoints.

## Where to go next

- **[Getting Started](./getting-started.md)** — run the server locally in a few minutes.
- **[Architecture](./architecture.md)** — apps, the two auth systems, encryption flow, config
  history/rollback, caching, rate limiting.
- **[Configuration](./configuration.md)** — every environment variable, with defaults.
- **Guides** — worked examples (cURL, Python, Node.js/TypeScript, Go) for every API:
  [Config & Secrets](./guides/config-and-secrets.md), [Feature Flags](./guides/feature-flags.md),
  [Notifications](./guides/notifications.md), [Realtime events (SSE)](./guides/sse.md),
  [In-app inbox](./guides/inapp-inbox.md).
- **[API reference](./api.md)** — full endpoint list, auth model, encryption model, OAuth2/OIDC setup.
- **[Deployment](./deployment.md)** — Docker image, Docker Compose, production checklist, GHCR
  release publishing.
- **[Security](./security.md)** — security model and how to report a vulnerability.

For contributing (dev setup, tests, PR process), see [`CONTRIBUTING.md`](../CONTRIBUTING.md).

Everything except reading configs/secrets/feature-flags and the notification API is managed
through the session-authenticated web UI (applications, environments, configs, secrets, feature
flags, users, service clients, OAuth providers) — there's no generic REST CRUD API and no
JWT-based user/service auth surface by design.
