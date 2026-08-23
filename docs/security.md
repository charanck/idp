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
- **No public self-registration** — the `/register/` route always redirects to login; new user
  accounts can only be created by an existing admin from the web UI, which also controls whether
  the new account starts active.
- **No self privilege-escalation** — admin (`is_staff`) users cannot change their own
  `is_staff`/`is_active` flags through the web UI; role changes must come from a different admin.
- **Rate limiting** — brute-force/credential-stuffing protection on `POST /login/`, plus separate
  throttling of failed S2S API-key attempts. See [Configuration](./configuration.md#rate-limiting).

## Deployment hygiene

- Always set `MASTER_ENCRYPTION_KEY` and `SESSION_SECRET` explicitly outside local dev — the
  encryption key has no fallback at all (the server won't start without it), and the session
  secret falls back to an insecure default if unset.
- Postgres and Redis are required in every environment; set `DEBUG=false` in production. See
  [Deployment](./deployment.md#production-checklist).
- Keep `MASTER_ENCRYPTION_KEY` backed up somewhere durable and access-controlled outside the app —
  losing it makes all stored config/secret data permanently unrecoverable, and leaking it defeats
  the encryption-at-rest model entirely.
