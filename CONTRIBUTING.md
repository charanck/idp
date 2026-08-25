# Contributing

Thanks for considering a contribution to IDP.

## Setup

```bash
git clone https://github.com/charanck/idp.git
cd idp/server
go build ./...

cp .env.example .env   # then set MASTER_ENCRYPTION_KEY, ADMIN_EMAIL, ADMIN_PASSWORD, DB_*, REDIS_URL
go run ./cmd/server
```

See the [README](./README.md#quick-start) for the full local setup, and [docs/](./docs) for
architecture, configuration, and API details.

## Running tests

Most tests are integration tests against a real Postgres instance:

```bash
docker run --rm -d -p 5433:5432 -e POSTGRES_PASSWORD=idp -e POSTGRES_USER=idp -e POSTGRES_DB=idp_test postgres:15.3-alpine
export CP_TEST_DATABASE_URL="host=localhost port=5433 dbname=idp_test user=idp password=idp sslmode=disable"
make test
```

Without `CP_TEST_DATABASE_URL` set, those tests `t.Skip()` individually rather than failing.

Please add or update tests for any behavior change, and make sure `make test` passes before
opening a PR.

## Making changes

- Keep changes scoped to what the PR is about — avoid drive-by refactors in unrelated code.
- The `api` package (`internal/api`) and the web UI (`internal/webui`) both call into the same
  `auth`/`config` services (`internal/auth`, `internal/config`) — if you change a service method's
  behavior, check both call sites.
- Schema changes go through a new [goose](https://github.com/pressly/goose) migration in
  `internal/db/migrations/` — GORM is a query layer only and doesn't own the schema.
- After editing a `.templ` file, regenerate views with
  `go run github.com/a-h/templ/cmd/templ generate` and commit the generated `_templ.go` output.
- See [`docs/architecture.md`](./docs/architecture.md) before touching the encryption flow, config
  history/rollback, or caching invalidation — each has a non-obvious invariant worth understanding
  first.
- Docs live in [`docs/`](./docs) as plain Markdown and are built with
  [MkDocs Material](https://squidfunk.github.io/mkdocs-material/) (`mkdocs.yml` at the repo root),
  auto-deployed to <https://charanck.github.io/idp/> on push to `master`
  ([`.github/workflows/docs.yml`](./.github/workflows/docs.yml)). Preview locally with
  `pip install mkdocs-material && mkdocs serve`.

## Pull requests

1. Fork the repo and create a branch off `master`.
2. Make your change, with tests.
3. Open a PR with a clear description of the change and why it's needed.

## Reporting security issues

Please don't open a public issue for security vulnerabilities — see
[`docs/security.md`](./docs/security.md#reporting-a-vulnerability) for how to report privately.
