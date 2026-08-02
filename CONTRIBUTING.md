# Contributing

Thanks for considering a contribution to IDP.

## Setup

```bash
git clone https://github.com/charanck/idp.git
cd idp
uv sync --group dev
cp .env.example .env   # then set MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD
uv run python manage.py migrate
uv run python manage.py runserver
```

See the [README](./README.md#quick-start) for the full local setup, and [docs/](./docs) for
architecture, configuration, and API details.

## Running tests

```bash
uv run pytest
# or a single app / case
uv run python manage.py test authentication
uv run python manage.py test config_management.tests.SomeTestCase
```

`pytest.ini` points `DJANGO_SETTINGS_MODULE` at `control_plane_project.test_settings`, which
isolates the test database, cache, and crypto keys from your local `.env` — you can run `pytest`
directly without any extra setup. Tests live alongside each app (`authentication/tests/`,
`config_management/tests/`, `web_ui/tests/`, `common/tests/`).

Please add or update tests for any behavior change, and make sure `uv run pytest` passes before
opening a PR.

## Making changes

- Keep changes scoped to what the PR is about — avoid drive-by refactors in unrelated code.
- The API (`*/api.py`) and the web UI (`web_ui/views.py`) both call into the same
  `authentication`/`config_management` services (`services.py`) — if you change a service method's
  behavior, check both call sites.
- Run migrations for any model change (`uv run python manage.py makemigrations`) and commit them.
- See [`docs/architecture.md`](./docs/architecture.md) before touching the encryption flow, config
  history/rollback, or caching invalidation — each has a non-obvious invariant worth understanding
  first.

## Pull requests

1. Fork the repo and create a branch off `master`.
2. Make your change, with tests.
3. Open a PR with a clear description of the change and why it's needed.

## Reporting security issues

Please don't open a public issue for security vulnerabilities — see
[`docs/security.md`](./docs/security.md#reporting-a-vulnerability) for how to report privately.
