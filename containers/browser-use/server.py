"""HTTP executor service for the browser-use container.

POST /run     {"task": ..., "url": ..., "max_steps": ..., "cdp_url": ...,
               "model": ..., "base_url": ..., "headless": ...} -> run result
GET  /healthz -> {"ok": true, "browser_use": "<version>", "cdp_configured": bool}

Each request runs one browser-use Agent in its own event loop (the stdlib
http server is threaded; Agent.run is async). The browser is NEVER
launched here: the service attaches to an existing Chromium over CDP
(cdp_url from the request or BU_CDP_URL). The LLM is any OpenAI-compatible
endpoint (base_url + model + api key), so the lane runs against a gateway,
Ollama, or -- in CI -- a deterministic stub.

Environment: BU_CDP_URL (default CDP endpoint), BU_LLM_BASE_URL,
BU_LLM_MODEL, BU_LLM_API_KEY_ENV (name of the env var holding the key;
default BROWSER_USE_API_KEY).
"""
import asyncio
import json
import os
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib.metadata import version as pkg_version

from browser_use import Agent, Browser, BrowserConfig
from langchain_openai import ChatOpenAI

BROWSER_USE_VERSION = pkg_version("browser-use")
CDP_URL = os.environ.get("BU_CDP_URL", "")
LLM_BASE_URL = os.environ.get("BU_LLM_BASE_URL", "")
LLM_MODEL = os.environ.get("BU_LLM_MODEL", "")
LLM_API_KEY_ENV = os.environ.get("BU_LLM_API_KEY_ENV", "BROWSER_USE_API_KEY")


def _llm(base_url, model):
    base = base_url or LLM_BASE_URL
    name = model or LLM_MODEL
    if not base or not name:
        raise RuntimeError(
            "no LLM configured: pass base_url/model in the request or set "
            "BU_LLM_BASE_URL and BU_LLM_MODEL"
        )
    return ChatOpenAI(
        model=name,
        base_url=base,
        api_key=os.environ.get(LLM_API_KEY_ENV, "stub-key"),
        temperature=0,
    )


def _maybe_call(obj, name, default):
    """Read attr or zero-arg call defensively across browser-use versions."""
    val = getattr(obj, name, default)
    if callable(val):
        try:
            val = val()
        except Exception:  # noqa: BLE001 - version drift must not 500 the run
            return default
    return val if val is not None else default


async def _run(req):
    cdp = req.get("cdp_url") or CDP_URL
    if not cdp:
        raise RuntimeError(
            "no CDP endpoint: pass cdp_url in the request or set BU_CDP_URL "
            "(this service never launches a browser)"
        )
    task = req.get("task") or ""
    if not task:
        raise RuntimeError("task is required")

    browser = Browser(config=BrowserConfig(cdp_url=cdp, headless=bool(req.get("headless", True))))
    agent = Agent(
        task=task,
        llm=_llm(req.get("base_url"), req.get("model")),
        browser=browser,
        max_steps=int(req.get("max_steps", 25)),
    )
    started = time.monotonic()
    history = await agent.run()
    return {
        "ok": True,
        "final_result": _maybe_call(history, "final_result", "") or "",
        "steps": len(_maybe_call(history, "history", []) or []),
        "duration_s": round(time.monotonic() - started, 3),
        "errors": [str(e) for e in (_maybe_call(history, "errors", []) or [])],
        "urls_visited": len(_maybe_call(history, "urls", []) or []),
        "browser_use": BROWSER_USE_VERSION,
    }


class Handler(BaseHTTPRequestHandler):
    def _send(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            self._send(200, {
                "ok": True,
                "browser_use": BROWSER_USE_VERSION,
                "cdp_configured": bool(CDP_URL),
            })
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/run":
            self._send(404, {"error": "not found"})
            return
        try:
            length = min(int(self.headers.get("Content-Length", "0")), 1 << 20)
            req = json.loads(self.rfile.read(length) or b"{}")
            self._send(200, asyncio.run(_run(req)))
        except Exception as exc:  # noqa: BLE001 - service boundary
            self._send(500, {"ok": False, "error": str(exc)})

    def log_message(self, fmt, *args):  # quiet; stdout stays clean for logs
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
