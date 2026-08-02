# IDP client examples

Runnable examples showing how to fetch and decrypt configs/secrets, and read feature flags, from
each of a few languages. Both read endpoints accept the same service client `X-API-Key`, so a
single credential drives both functions:

- **`getDecryptedConfigs` / `get_decrypted_configs` / `GetDecryptedConfigs`** — lists every
  config/secret for a `service`+`environment` via `GET /api/v1/config/configs/list`, decrypts each
  value with the calling service client's Fernet `encryption_key`, and returns them as a
  `{ KEY: value }` map.
- **`getFeatureFlags` / `get_feature_flags` / `GetFeatureFlags`** — lists every feature flag for a
  `service`+`environment` via `GET /api/v1/config/feature-flags` and returns them as a
  `{ name: enabled }` map. (This endpoint also accepts a user JWT as an alternative, for
  admin/internal tooling - the examples here just use the API key.)

| Language | File |
|---|---|
| Python | [python/idp_client.py](./python/idp_client.py) |
| Go | [go/main.go](./go/main.go) |
| Node.js | [nodejs/idp-client.js](./nodejs/idp-client.js) |
| TypeScript | [typescript/idp-client.ts](./typescript/idp-client.ts) |

## Prerequisites

An admin JWT (`POST /api/v1/auth/token`) to create a service client:

```bash
curl -X POST http://localhost:8000/api/v1/auth/s2s/clients \
  -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-service"}'
```

The response's `api_key` and `encryption_key` are shown once — save them; they're what every
example above needs. (`api_key` is stored hashed server-side and can't be recovered later;
`encryption_key` can be viewed and rotated from that client's detail page in the web UI if you
lose it.)

See [docs/api.md](../docs/api.md) for the full API reference and encryption model.
