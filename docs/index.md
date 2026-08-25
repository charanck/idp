# IDP — Internal Developer Platform

A Go control plane for configuration & secret management and feature flags, with API-key
service-to-service (S2S) auth, OAuth2/OIDC login, a notification system (email/SMS/WhatsApp/in-app,
with realtime SSE delivery), and a server-rendered web UI.

[:fontawesome-brands-github: View on GitHub](https://github.com/charanck/idp){ .md-button }
[Get started](getting-started.md){ .md-button .md-button--primary }

## What it does

- **Configuration & secrets** scoped per application + environment, **encrypted at rest** and
  re-encrypted per service client on read — a client only ever sees ciphertext it can decrypt with
  its own key. See [Config & Secrets](guides/config-and-secrets.md).
- **Feature flags**, per application + environment. See [Feature Flags](guides/feature-flags.md).
- **Notifications** across email, SMS, WhatsApp, and in-app channels, processed by a background
  worker with retries. See [Notifications](guides/notifications.md).
- **Realtime delivery events over SSE** and a **persisted, pull-based in-app inbox** for
  user-facing notifications. See [Realtime events](guides/sse.md) and [In-app inbox](guides/inapp-inbox.md).
- **API-key (S2S) auth** for every programmatic endpoint, and session auth (with OAuth2/OIDC
  support) for the web UI.
- **Config version history & rollback** — every write is snapshotted; roll back to any prior
  version without losing the audit trail.
- **Append-only activity log**, Redis-backed caching, and rate limiting on auth-sensitive
  endpoints.

## Where to go next

| | |
|---|---|
| [Getting Started](getting-started.md) | Run the server locally in a few minutes. |
| [Architecture](architecture.md) | Packages, auth systems, encryption flow, caching, config history/rollback. |
| [Configuration](configuration.md) | Every environment variable, with defaults. |
| [Guides](guides/config-and-secrets.md) | How to use each API from cURL, Python, Node.js/TypeScript, and Go. |
| [API Reference](api.md) | Full endpoint reference. |
| [Deployment](deployment.md) | Docker image, Docker Compose, production checklist. |
| [Security](security.md) | Security model and how to report a vulnerability. |

Everything except reading configs/secrets/feature-flags and the notification API is managed
through the session-authenticated web UI (applications, environments, configs, secrets, feature
flags, users, service clients, OAuth providers) — there's no generic REST CRUD API and no
JWT-based user/service auth surface by design.
