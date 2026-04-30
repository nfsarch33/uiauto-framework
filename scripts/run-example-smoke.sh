#!/usr/bin/env bash
# Runs the example.com smoke scenario against a Chrome instance on :9222.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin/ui-agent"
SCENARIO="$REPO_ROOT/examples/example-com-smoke/scenario.json"

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

exec "$BIN" demo \
    --scenario "$SCENARIO" \
    --scenario-id "smoke-001" \
    --url "https://example.com" \
    --remote-debug-url "http://127.0.0.1:9222" \
    --step-delay 1s \
    --step-timeout 30s
