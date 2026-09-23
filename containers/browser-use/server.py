"""HTTP executor service for the browser-use container.

POST /run     {"task": ..., "url": ..., "max_steps": ..., "cdp_url": ...,
               "model": ..., "base_url": ..., "headless": ...} -> run result
GET  /healthz -> {"ok": true, "browser_use": "<version>", "cdp_configured": bool}

Each request runs one browser-use Agent in its own event loop (the stdlib
http server is threaded; Agent.run is async). The browser is NEVER
launched here: the service attaches to an existing Chromium over CDP
(cdp_url from BU_CDP_URL). The LLM is any OpenAI-compatible endpoint
(base_url + model + api key), so the lane runs against a gateway,
Ollama, or -- in CI -- a deterministic stub.

Security posture (loopback-published test infra):
- /run requires Content-Type: application/json and a sane Content-Length;
  a browser-driven cross-site request can set neither without a CORS
  preflight this server never answers, so pages visited by the attached
  Chrome cannot drive it.
- Request-supplied base_url/cdp_url are honoured only when
  BU_ALLOW_REQUEST_ENDPOINTS=1 (default off).
- The configured API key is only ever sent to the configured
  BU_LLM_BASE_URL -- never to a request-supplied endpoint.

Environment: BU_CDP_URL (default CDP endpoint), BU_LLM_BASE_URL,
BU_LLM_MODEL, BU_LLM_API_KEY_ENV (name of the env var holding the key;
default BROWSER_USE_API_KEY), BU_ALLOW_REQUEST_ENDPOINTS (=1 to allow
per-request endpoint overrides).
"""
import asyncio
import atexit
import json
import os
import socket
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib.metadata import version as pkg_version
from urllib.parse import urlparse

from browser_use import Agent, Browser
from browser_use.llm import ChatOpenAI

BROWSER_USE_VERSION = pkg_version("browser-use")
CDP_URL = os.environ.get("BU_CDP_URL", "")
LLM_BASE_URL = os.environ.get("BU_LLM_BASE_URL", "")
LLM_MODEL = os.environ.get("BU_LLM_MODEL", "")
LLM_API_KEY_ENV = os.environ.get("BU_LLM_API_KEY_ENV", "BROWSER_USE_API_KEY")
ALLOW_REQUEST_ENDPOINTS = os.environ.get("BU_ALLOW_REQUEST_ENDPOINTS", "") == "1"
MAX_BODY_BYTES = 1 << 20
MAX_STEPS_LIMIT = 200


def _llm(base_url, model):
    """Build the LLM client. The env key is attached only to the env
    default base_url; an (allowed) request-supplied endpoint never sees
    it."""
    base = base_url or LLM_BASE_URL
    name = model or LLM_MODEL
    if not base or not name:
        raise RuntimeError(
            "no LLM configured: pass base_url/model in the request or set "
            "BU_LLM_BASE_URL and BU_LLM_MODEL"
        )
    api_key = "stub-key"
    if base.rstrip("/") == LLM_BASE_URL.rstrip("/"):
        api_key = os.environ.get(LLM_API_KEY_ENV, api_key)
    return ChatOpenAI(model=name, base_url=base, api_key=api_key, temperature=0)


# --- CDP loopback bridges -------------------------------------------------
#
# Chromium's DevTools endpoints reject Host headers that are not localhost,
# and the websocket URL chromium advertises carries the Host header it saw,
# so the only address the CDP client can dial back is a loopback forwarder.
# One bridge per CDP target, on its own loopback port: created once under a
# lock, readiness-checked, reused by later requests for the same target,
# and all reaped at exit.

_bridges_lock = threading.Lock()
_bridges = {}  # "host:port" -> {"port": int, "proc": subprocess.Popen}


def _port_open(port, timeout=0.0):
    deadline = time.monotonic() + timeout
    while True:
        with socket.socket() as s:
            s.settimeout(0.2)
            if s.connect_ex(("127.0.0.1", port)) == 0:
                return True
        if time.monotonic() >= deadline:
            return False
        time.sleep(0.1)


def _free_loopback_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _spawn_bridge(target, port):
    return subprocess.Popen(
        ["socat", f"TCP-LISTEN:{port},fork,bind=127.0.0.1,reuseaddr", f"TCP:{target}"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )


def _bridge_url(cdp_url):
    parsed = urlparse(cdp_url)
    if parsed.hostname in ("127.0.0.1", "localhost"):
        return cdp_url
    target = f"{parsed.hostname}:{parsed.port or 80}"
    with _bridges_lock:
        entry = _bridges.get(target)
        if entry and _port_open(entry["port"]):
            return f"http://127.0.0.1:{entry['port']}"
        if entry:
            # Stale bridge (process died): clear it before rebuilding so a
            # second target can never silently ride the first one's port.
            try:
                entry["proc"].terminate()
            except OSError:
                pass
            _bridges.pop(target, None)
        port = _free_loopback_port()
        proc = _spawn_bridge(target, port)
        if not _port_open(port, timeout=5.0):
            proc.terminate()
            raise RuntimeError(f"CDP bridge to {target} did not become ready")
        _bridges[target] = {"port": port, "proc": proc}
        return f"http://127.0.0.1:{port}"


def _reap_bridges():
    with _bridges_lock:
        for entry in _bridges.values():
            try:
                entry["proc"].terminate()
            except OSError:
                pass
        _bridges.clear()


atexit.register(_reap_bridges)


def _maybe_call(obj, name, default):
    """Read attr or zero-arg call defensively across browser-use versions."""
    val = getattr(obj, name, default)
    if callable(val):
        try:
            val = val()
        except Exception:  # noqa: BLE001 - version drift must not 500 the run
            return default
    return val if val is not None else default


def _resolve_cdp(req):
    cdp = req.get("cdp_url")
    if cdp and cdp != CDP_URL and not ALLOW_REQUEST_ENDPOINTS:
        raise RuntimeError("request-supplied cdp_url is disabled (BU_ALLOW_REQUEST_ENDPOINTS!=1)")
    return cdp or CDP_URL


def _resolve_llm(req):
    base_url, model = req.get("base_url"), req.get("model")
    if (base_url or model) and not ALLOW_REQUEST_ENDPOINTS:
        raise RuntimeError("request-supplied base_url/model is disabled (BU_ALLOW_REQUEST_ENDPOINTS!=1)")
    return _llm(base_url, model)


async def _run(req):
    cdp = _resolve_cdp(req)
    if not cdp:
        raise RuntimeError(
            "no CDP endpoint: set BU_CDP_URL (this service never launches a browser)"
        )
    task = req.get("task") or ""
    if not task:
        raise RuntimeError("task is required")

    try:
        max_steps = int(req.get("max_steps", 25))
    except (TypeError, ValueError):
        raise RuntimeError("max_steps must be an integer") from None
    max_steps = max(1, min(max_steps, MAX_STEPS_LIMIT))

    # browser_use.Browser is the session (BrowserSession) in 0.13.x; it
    # connects over CDP via the loopback bridge and never launches a
    # browser of its own.
    browser = Browser(cdp_url=_bridge_url(cdp), headless=bool(req.get("headless", True)))

    agent_kwargs = {}
    url = req.get("url") or ""
    if url:
        # Deterministic pre-navigation: the request's url is honoured
        # BEFORE the LLM is consulted, so a dropped or broken navigation
        # is visible in the run history (urls_visited) rather than silent.
        navigate_to = getattr(browser, "navigate_to", None)
        if callable(navigate_to):
            await navigate_to(url, new_tab=False)
        else:
            # Older sessions: the registered action name is 'navigate'
            # (with url/new_tab params) -- 'go_to_url' does not exist in
            # 0.13.10's registry and raises a KeyError.
            agent_kwargs["initial_actions"] = [{"navigate": {"url": url, "new_tab": False}}]
    agent = Agent(task=task, llm=_resolve_llm(req), browser=browser, max_steps=max_steps, **agent_kwargs)

    started = time.monotonic()
    history = await agent.run()
    # errors() can yield a bare None when the run was clean; only real
    # errors belong in the history a caller grades on.
    return {
        "ok": True,
        "final_result": _maybe_call(history, "final_result", "") or "",
        "steps": len(_maybe_call(history, "agent_steps", []) or _maybe_call(history, "history", []) or []),
        "duration_s": round(time.monotonic() - started, 3),
        "errors": [str(e) for e in (_maybe_call(history, "errors", []) or []) if e],
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
        # CSRF guard: a browser form post carries a form Content-Type and
        # cannot set application/json cross-origin without a preflight this
        # server never answers.
        if not self.headers.get("Content-Type", "").startswith("application/json"):
            self._send(415, {"error": "Content-Type must be application/json"})
            return
        try:
            length = int(self.headers.get("Content-Length", ""))
        except ValueError:
            self._send(400, {"error": "Content-Length is required"})
            return
        if length < 0 or length > MAX_BODY_BYTES:
            self._send(400, {"error": f"Content-Length out of range [0, {MAX_BODY_BYTES}]"})
            return
        try:
            req = json.loads(self.rfile.read(length) or b"{}")
            self._send(200, asyncio.run(_run(req)))
        except Exception as exc:  # noqa: BLE001 - service boundary
            self._send(500, {"ok": False, "error": str(exc)})

    def log_message(self, fmt, *args):  # quiet; stdout stays clean for logs
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
