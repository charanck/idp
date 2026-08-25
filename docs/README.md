# Documentation

The hosted, browsable version of these docs is at **<https://charanck.github.io/idp/>**
(built with [MkDocs Material](https://squidfunk.github.io/mkdocs-material/) via
[`.github/workflows/docs.yml`](../.github/workflows/docs.yml) on every push to `master`).

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

## Working on these docs

The site is built with [MkDocs Material](https://squidfunk.github.io/mkdocs-material/) from
`mkdocs.yml` at the repo root. To preview locally:

```bash
pip install mkdocs-material
mkdocs serve
```
