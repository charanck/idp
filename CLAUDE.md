# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Django + Django Ninja control plane for configuration/secret management and feature flags, with JWT user auth, API-key service-to-service (S2S) auth, OAuth2/OIDC login, and a server-rendered Bootstrap web UI. Configs and secrets are the same model (`ConfigEntry` with `is_secret=true`) and are encrypted at rest with a master key, then re-encrypted per-client on read.

## Commands

Dependency management uses `uv` (see `pyproject.toml` / `uv.lock`).

```bash
# Install deps (add --group dev for pytest)
uv sync --group dev

# Run the dev server
uv run python manage.py runserver

# Migrations
uv run python manage.py makemigrations
uv run python manage.py migrate

# Create/update the admin user (also runs automatically via ADMIN_EMAIL/ADMIN_PASSWORD in some setups)
uv run python manage.py setup_admin --email admin@example.com --password SecurePass123

# Tests (pytest-django is a dev dependency; no pytest.ini/DJANGO_SETTINGS_MODULE config exists yet,
# so set it explicitly, or use Django's own test runner instead)
DJANGO_SETTINGS_MODULE=control_plane_project.settings uv run pytest
uv run python manage.py test
uv run python manage.py test authentication         # single app
uv run python manage.py test config_management.tests.SomeTestCase   # single case, once tests exist
```

Note: `authentication/tests.py` and `config_management/tests.py` are currently empty stubs — there is no real test suite yet.

Required env vars (see `README.md` for the full list): `DJANGO_SECRET_KEY`, `MASTER_ENCRYPTION_KEY`, `JWT_SECRET_KEY`, `DATABASE_URL`-style `DB_*` vars, `ADMIN_EMAIL`. If `MASTER_ENCRYPTION_KEY` is unset, `control_plane_project/settings.py` auto-generates one at startup (dev only — data becomes unreadable across restarts). Optional caching is controlled by `CACHE_ENABLED`/`CACHE_BACKEND` (`redis` or `locmem`).

## Architecture

Four Django apps, each with a narrow role:

- **`control_plane_project/`** — settings, root `urls.py`. Mounts the Django Ninja `NinjaAPI` at `/api/v1/` (auth router at `/api/v1/auth/`, config router at `/api/v1/config/`), Django admin at `/admin/`, and `web_ui` at `/`.
- **`authentication/`** — `User` (custom `AUTH_USER_MODEL`, email-based login, UUID PK, `force_password_reset` flag), `ServiceClient` (S2S API-key holder + per-client Fernet `encryption_key`), and OAuth2/OIDC models (`OAuthProvider`, `OAuthUserToken` in `oauth_models.py`, imported at the bottom of `models.py` so migrations pick them up). `services.py` = `AuthService` (user registration/auth, service-client creation/API-key verification). `oauth_service.py` = `OAuthService` (authlib-based authorization-code flow). `api.py` exposes JWT/S2S endpoints under Ninja.
- **`config_management/`** — `Application` → `Environment` (unique per app) → `ConfigEntry` (unique per app+env+key; secrets are just `is_secret=True` entries) and `FeatureFlag` (same app+env scoping, soft-deleted via `deleted_at`). `Activity` is an append-only audit log (type/resource/resource_id/user_email/ip) written by `common/activity_logger.py`. `services.py` = `ConfigService` and `FeatureFlagService`, both **get-or-create** the `Application`/`Environment` scope from `(service, environment)` string pairs rather than taking foreign keys directly — this is the shape both the Ninja API and the web UI call into.
- **`web_ui/`** — server-rendered CRUD (Bootstrap templates in `web_ui/templates/web_ui/`) for everything above: users, service clients, applications, environments, configs/secrets, feature flags, OAuth providers, plus the OAuth login/callback flow and a read-only activity log. `views.py` is one large module gated by Django's session auth (`@login_required`) and a local `admin_required` decorator (`is_staff` check) — this is a separate auth path from the JWT/API-key auth used by the Ninja API.
- **`common/`** — shared, framework-adjacent utilities imported across apps: `encryption.py` (Fernet — `EncryptionService.encrypt_for_storage`/`decrypt_from_storage` use the master key; `re_encrypt_for_client` re-wraps for a client's own key), `jwt_utils.py` (encode/decode JWT, `type` claim distinguishes `user` vs `service` tokens), `authentication.py` (Ninja auth classes: `JWTAuth`, `JWTAdminAuth` (staff-only), `APIKeyAuth` for the `X-API-Key: <key_id>.<secret>` S2S header), `security.py` (password hashing wrappers), `activity_logger.py` (writes `Activity` rows; `log_create`/`log_update`/`log_delete`/`log_toggle`/`log_login`/`log_logout` helpers).

### Two independent auth systems

1. **Ninja API** (`/api/v1/...`): stateless. `JWTAuth`/`JWTAdminAuth` read a Bearer token (`common/jwt_utils.py`); `APIKeyAuth` reads `X-API-Key` and resolves a `ServiceClient` via `AuthService.authenticate_service_api_key`.
2. **Web UI** (`/...`): standard Django session auth (`django.contrib.auth`), gated with `@login_required` / `admin_required`.
Both end up operating on the same `authentication`/`config_management` models and services — when changing a service method, check both `*/api.py` and `web_ui/views.py` for callers.

### Encryption flow

Admin writes a config/secret via JWT-authenticated API or the web UI → `ConfigService.upsert_config` encrypts with `MASTER_ENCRYPTION_KEY` before storing. A service client reads via `GET /api/v1/config/configs/list` (API-key auth) → the server decrypts with the master key and **re-encrypts with that client's own `encryption_key`** before returning it; the client decrypts locally with the key it was given at creation time (`POST /api/v1/auth/s2s/clients`). The web UI never displays decrypted secret values back through the API (`upsert_config` always returns `"***ENCRYPTED***"`).

### Caching

When `CACHE_ENABLED=true`, `ConfigService`/`FeatureFlagService` cache list responses scoped by service+environment (+ client key hash for configs). Cache invalidation is version-based: a per-scope version counter (`config:scope-version:{service}:{environment}`) is bumped on any write instead of deleting keys directly — read paths must incorporate the current version into their cache key.
