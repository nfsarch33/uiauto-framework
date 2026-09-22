# browser-use executor lane (container)

[browser-use](https://github.com/browser-use/browser-use) (MIT) is the
LLM-driven executor: it takes a natural-language task, drives an existing
Chromium over CDP, and returns the run history (`final_result`, steps,
errors). In this framework the decision layer (containers/laya) picks
WHAT to do and this lane (or the chromedp lane) DOES it. The Go client
lives in `pkg/uiauto/browseruse`; the operator surface is
`ui-agent browser-use-run`.

## Endpoints

| Route | Method | Payload |
|---|---|---|
| `/run` | POST | `{"task": ..., "url"?: ..., "max_steps"?: 25, "cdp_url"?: ..., "model"?: ..., "base_url"?: ...}` → run result |
| `/healthz` | GET | `{"ok": true, "browser_use": "<version>", "cdp_configured": bool}` |

## Configuration (environment)

| Var | Meaning |
|---|---|
| `BU_CDP_URL` | Default CDP endpoint (e.g. `http://chrome:9222`). Required unless each request passes `cdp_url`. |
| `BU_LLM_BASE_URL` | OpenAI-compatible base URL (gateway, vLLM, Ollama `/v1`, or the CI stub). |
| `BU_LLM_MODEL` | Model name for that endpoint. |
| `BU_LLM_API_KEY_ENV` | Name of the env var holding the API key (default `BROWSER_USE_API_KEY`). |

## Constraints

- **Never launches a browser.** No Chromium binaries are installed in the
  image; the service attaches over CDP only (one shared browser session
  per host, per policy). A request without a reachable CDP endpoint fails
  loudly.
- **No authentication, loopback only.** Same constraint as the laya
  container: acceptable only while the published port is bound to
  loopback; a tailnet/LAN publication requires a bearer token first.
- **Pinned** `browser-use==0.13.10`; the transitive freeze is written to
  `/versions.txt` for build-to-build diffing.
- **CDP loopback bridge.** Chromium's DevTools endpoints reject non-localhost
  Host headers, and the websocket URL chromium advertises is only dialable
  from a loopback forwarder. The service therefore bridges the configured
  CDP endpoint to `127.0.0.1:80` internally (socat) and hands browser-use
  the loopback address — no external proxy, no browser launched.

## Build and run

    make browser-use-image
    podman run --rm -p 127.0.0.1:8091:8080 \
      -e BU_CDP_URL=http://host.containers.internal:9222 \
      -e BU_LLM_BASE_URL=http://host.containers.internal:11434/v1 \
      -e BU_LLM_MODEL=qwen3.5:14b \
      localhost/hlxn-browseruse:latest &

## Deterministic CI

The integration stack (`docker-compose.integration.yml`) pairs this
service with the headless Chrome container and a WireMock OpenAI-compatible
LLM stub (`test/fixtures/llm-stub`) so the e2e lane runs green in CI
without a real model. Live-LLM runs are env-gated
(`BROWSER_USE_E2E=1`) and documented in `docs/eval.md`.
