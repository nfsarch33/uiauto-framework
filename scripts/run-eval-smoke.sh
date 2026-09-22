#!/usr/bin/env bash
# Deterministic eval smoke: brings up the integration stack (headless
# Chrome, browser-use lane, WireMock LLM + laya stubs, fixture pages),
# runs ui-agent eval against eval/suites/deterministic.yaml with the
# default rubric, and requires a PASS verdict. Exit 0 == rubric PASS.
set -euo pipefail

cd "$(dirname "$0")/.."

if command -v docker >/dev/null 2>&1; then
  COMPOSE="${COMPOSE:-docker compose}"
elif command -v podman-compose >/dev/null 2>&1; then
  COMPOSE="${COMPOSE:-podman-compose}"
else
  echo "ERROR: docker (or podman-compose) is required for the eval smoke." >&2
  exit 2
fi

COMPOSE_FILE="docker-compose.integration.yml"

cleanup() {
  echo "[eval-smoke] tearing stack down"
  $COMPOSE -f "$COMPOSE_FILE" down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "[eval-smoke] starting stack (builds the browser-use image on first run)"
$COMPOSE -f "$COMPOSE_FILE" up -d --build --remove-orphans

wait_for() {
  local name="$1" url="$2" tries="${3:-60}"
  for _ in $(seq 1 "$tries"); do
    if curl -fsS "$url" >/dev/null 2>&1; then return 0; fi
    sleep 2
  done
  echo "ERROR: $name never became healthy ($url)" >&2
  return 1
}

wait_for chrome "http://127.0.0.1:9333/json/version"
wait_for browser-use "http://127.0.0.1:8091/healthz" 90
wait_for laya-stub "http://127.0.0.1:8092/healthz"
wait_for fixtures "http://127.0.0.1:8018/form-flow/"

echo "[eval-smoke] building ui-agent"
make build

echo "[eval-smoke] running deterministic eval suite"
./bin/ui-agent eval \
  --suite eval/suites/deterministic.yaml \
  --rubric eval/rubrics/default.yaml \
  --browser-use http://127.0.0.1:8091 \
  --laya http://127.0.0.1:8092 \
  --chrome-debug http://127.0.0.1:9333 \
  --out eval-out
