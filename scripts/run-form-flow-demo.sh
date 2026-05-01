#!/usr/bin/env bash
# Runs the local form-flow demo against a visible Chrome instance on :9222.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin/ui-agent"
EXAMPLE_DIR="$REPO_ROOT/examples/form-flow"
SCENARIO="$EXAMPLE_DIR/scenario.json"
PORT="${UIAUTO_FORM_FLOW_PORT:-8018}"
URL="http://127.0.0.1:${PORT}/"

if [ ! -x "$BIN" ]; then
    echo "ui-agent binary missing; run 'make build' first" >&2
    exit 2
fi

if ! curl -fsS "http://127.0.0.1:9222/json/version" >/dev/null 2>&1; then
    echo "Chrome not reachable on :9222." >&2
    echo "Launch with:" >&2
    echo '  google-chrome --remote-debugging-port=9222 --user-data-dir=/tmp/chrome-uiauto' >&2
    exit 3
fi

server_log="$(mktemp -t uiauto-form-flow.XXXXXX.log)"
python3 -m http.server "$PORT" --directory "$EXAMPLE_DIR" >"$server_log" 2>&1 &
server_pid=$!
trap 'kill "$server_pid" >/dev/null 2>&1 || true; rm -f "$server_log"' EXIT

for _ in {1..30}; do
    if curl -fsS "$URL" >/dev/null 2>&1; then
        break
    fi
    sleep 0.1
done

exec "$BIN" demo \
    --scenario "$SCENARIO" \
    --scenario-id "form-flow-001" \
    --url "$URL" \
    --remote-debug-url "http://127.0.0.1:9222" \
    --step-delay 1s \
    --step-timeout 30s
