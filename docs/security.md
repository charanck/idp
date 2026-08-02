# Security

## Reporting a vulnerability

If you find a security issue, please email **charank804@gmail.com** instead of opening a public
issue. Include reproduction steps and impact; you'll get a response before any public disclosure.

## Model

- **Secrets at rest** — every config/secret value is encrypted with `MASTER_ENCRYPTION_KEY`
  (Fernet) before it touches the database. Reads re-encrypt per-client with that client's own key,
  so a compromised client only ever has ciphertext it can decrypt for itself — see
  [Architecture: encryption flow](./architecture.md#encryption-flow).
- **Credentials shown once** — service client `api_key` and `encryption_key` are returned only at
  creation time and stored hashed/encrypted thereafter; there is no way to retrieve them again
  through the app (the encryption key can be rotated from the client's detail page if lost).
- **New users are inactive by default** — accounts created via `POST /api/v1/auth/register` start
  `is_active=False` and require explicit admin activation before they can authenticate.
- **No self privilege-escalation** — admin (`is_staff`) users cannot change their own
  `is_staff`/`is_active` flags through the web UI; role changes must come from a different admin.
- **Rate limiting** — brute-force/credential-stuffing protection on `POST /api/v1/auth/token`,
  `POST /api/v1/auth/register`, and `POST /login/`, plus separate throttling of failed S2S API-key
  attempts. See [Configuration](./configuration.md#rate-limiting).

## Deployment hygiene

- Always set `MASTER_ENCRYPTION_KEY`, `DJANGO_SECRET_KEY`, and `JWT_SECRET_KEY` explicitly outside
  local dev — the auto-generated fallbacks are for convenience only and are not stable across
  restarts.
- Use PostgreSQL and `DEBUG=False` in production; see [Deployment](./deployment.md#production-checklist).
- Keep `MASTER_ENCRYPTION_KEY` backed up somewhere durable and access-controlled outside the app —
  losing it makes all stored config/secret data permanently unrecoverable, and leaking it defeats
  the encryption-at-rest model entirely.
