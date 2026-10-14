#!/usr/bin/env bash
# Runs the e2e suite (./e2e) against a freshly built server on a clean slate:
# prunes the local Postgres/Redis containers *and their volumes*, starts them
# again, boots cmd/server against them, runs the tests, then tears it all down.
#
# Usage: scripts/e2e.sh [extra `go test` args]   (or: task e2e)
# Env overrides: E2E_PORT (default 8000), E2E_KEEP=1 to leave the containers up.
set -euo pipefail

cd "$(dirname "$0")/.."

COMPOSE=(docker compose -f docker-compose.local.yml)
PORT="${E2E_PORT:-8000}"
BASE_URL="http://localhost:${PORT}"
WORK_DIR="$(mktemp -d)"
SERVER_PID=""

cleanup() {
	status=$?
	[[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
	if [[ "$status" -ne 0 && -f "$WORK_DIR/server.log" ]]; then
		echo "--- server log (last 40 lines) ---" >&2
		tail -n 40 "$WORK_DIR/server.log" >&2
	fi
	[[ "${E2E_KEEP:-}" == "1" ]] || "${COMPOSE[@]}" down -v >/dev/null 2>&1 || true
	rm -rf "$WORK_DIR"
	exit "$status"
}
trap cleanup EXIT

if curl -s -o /dev/null "$BASE_URL/login/"; then
	echo "something is already listening on port ${PORT}; stop it or set E2E_PORT" >&2
	exit 1
fi

echo ">> pruning Postgres + Redis (containers and volumes)"
"${COMPOSE[@]}" down -v >/dev/null 2>&1 || true
"${COMPOSE[@]}" up -d --wait postgres redis

# A fresh key every run is safe because the DB is wiped above: values encrypted
# under a previous key (e.g. the OIDC signing key) would be undecryptable.
export MASTER_ENCRYPTION_KEY="$(openssl rand -base64 32 | tr '+/' '-_')"
export SESSION_SECRET="e2e-session-secret-$(openssl rand -hex 16)"
export ADMIN_EMAIL="${ADMIN_EMAIL:-admin@example.com}"
export ADMIN_PASSWORD="${ADMIN_PASSWORD:-changeme123}"
export DB_HOST=localhost DB_PORT=5432 DB_NAME=idp DB_USER=idp DB_PASSWORD=idp DB_SSLMODE=disable
export REDIS_URL="redis://127.0.0.1:6379/1"
export PORT ALLOWED_HOSTS="localhost,127.0.0.1"

echo ">> building and starting the server on ${BASE_URL}"
go build -o "$WORK_DIR/server" ./cmd/server
"$WORK_DIR/server" >"$WORK_DIR/server.log" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 60); do
	curl -s -o /dev/null "$BASE_URL/login/" && break
	kill -0 "$SERVER_PID" 2>/dev/null || { echo "server exited during startup" >&2; exit 1; }
	sleep 1
done
curl -s -o /dev/null "$BASE_URL/login/" || { echo "server did not become ready" >&2; exit 1; }

echo ">> running e2e tests"
CP_E2E_BASE_URL="$BASE_URL" go test -p 1 -count=1 ./e2e/... "$@"
