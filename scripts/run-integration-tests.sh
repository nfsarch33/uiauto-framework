#!/usr/bin/env bash
# Brings up the integration test stack (Postgres + Chrome + OmniParser stub)
# defined in docker-compose.integration.yml, runs the full Go test suite (no
# `-short`, no `CI=1`), and tears the stack down. Prefer `make test-integration`
# which calls this script.
#
# Environment overrides:
#   COMPOSE         - docker-compose binary (default: detected automatically)
#   POSTGRES_URL    - DSN exported to the test process
#   REMOTE_DEBUG_URL - CDP URL exported to the test process
#   OMNIPARSER_URL  - OmniParser stub URL exported to the test process

set -euo pipefail

cd "$(dirname "$0")/.."

if command -v docker >/dev/null 2>&1; then
  COMPOSE="${COMPOSE:-docker compose}"
elif command -v podman-compose >/dev/null 2>&1; then
  COMPOSE="${COMPOSE:-podman-compose}"
else
  echo "ERROR: docker (or podman-compose) is required for integration tests." >&2
  exit 2
fi

COMPOSE_FILE="docker-compose.integration.yml"

cleanup() {
  echo "[integration] tearing stack down"
  $COMPOSE -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[integration] starting stack"
$COMPOSE -f "$COMPOSE_FILE" up -d --remove-orphans

# Wait for Postgres to accept connections.
for _ in $(seq 1 30); do
  if $COMPOSE -f "$COMPOSE_FILE" exec -T postgres pg_isready -U uiauto -d uiauto_test >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

# Wait for Chrome DevTools to respond.
for _ in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:9333/json/version >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

# Wait for the OmniParser stub.
for _ in $(seq 1 30); do
  if curl -fsS http://127.0.0.1:7861/probe/ >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

export POSTGRES_URL="${POSTGRES_URL:-postgres://uiauto:uiauto@127.0.0.1:5440/uiauto_test?sslmode=disable}"
export REMOTE_DEBUG_URL="${REMOTE_DEBUG_URL:-http://127.0.0.1:9333}"
export OMNIPARSER_URL="${OMNIPARSER_URL:-http://127.0.0.1:7861}"

echo "[integration] running go test (no -short)"
echo "  POSTGRES_URL=$POSTGRES_URL"
echo "  REMOTE_DEBUG_URL=$REMOTE_DEBUG_URL"
echo "  OMNIPARSER_URL=$OMNIPARSER_URL"

go test -count=1 -timeout 15m ./...
