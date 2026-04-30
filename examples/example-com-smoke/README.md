# example.com smoke scenario

Minimal end-to-end smoke for the `ui-agent` CLI. Hits a public, stable URL so
the run is reproducible without any auth or feature flags.

## Prerequisites

- Chrome or Chromium running with the DevTools port:
  ```
  /Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome \
      --remote-debugging-port=9222 --user-data-dir=/tmp/chrome-uiauto
  ```
- `bin/ui-agent` built (run `make build` from the repo root).

## Run

```bash
./scripts/run-example-smoke.sh
```

The script:

1. Confirms Chrome is reachable on `:9222`.
2. Launches `ui-agent demo` against `https://example.com` with the local
   scenario.
3. Saves results to `~/uiauto/tests/<scenario-id>_<timestamp>/`.

## Expected outcome

```
=== UIAuto Demo: Example.com smoke ===
ID: smoke-001 | Steps: 2 | Source: framework
...
=== Demo Complete ===
Results: 2/2 passed
```
