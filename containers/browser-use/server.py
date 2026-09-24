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
- The request Host header must be in the allowlist (loopback names and
  this service's compose names): a DNS-rebinding page served to the
  attached Chrome resolves its own name to this service, gets a
  same-origin request past the Content-Type guard via a matching Origin,
  and is stopped here instead.
- Request-supplied base_url/cdp_url are honoured only when
  BU_ALLOW_REQUEST_ENDPOINTS=1 (default off); refusals are 403, malformed
  input is 400 -- a policy refusal is a client error, not a fault.
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
# Hosts a request may legitimately arrive with: the loopback names used by
# published-port callers, and this service's compose service/container
# names used inside the stack. Everything else -- a DNS-rebinding name
# resolved to this address -- is refused.
ALLOWED_HOSTS = {"127.0.0.1", "localhost", "browser-use", "uiauto-browseruse"}


class Refused(Exception):
    """A policy refusal that maps to a 4xx, not the generic 500."""

    def __init__(self, status, message):
        super().__init__(message)
        self.status = status


def _llm(base_url, model):
    """Build the LLM client. The env key is attached only to the env
    default base_url; an (allowed) request-supplied endpoint never sees
    it."""
    base = base_url or LLM_BASE_URL
    name = model or LLM_MODEL
    if not base or not name:
        raise Refused(400, "no LLM configured: pass base_url/model in the request or set BU_LLM_BASE_URL and BU_LLM_MODEL")
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
# lock, verified ALIVE (the owning process, not just the port -- a dead
# socat's port can be rebound by anyone) before reuse, reaped with
# terminate()+wait() when stale, and the map is capped so a caller cycling
# endpoints cannot grow it without bound.

MAX_BRIDGES = 8
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


def _reap(proc):
    proc.terminate()
    try:
        proc.wait(timeout=2)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()


def _alive(entry):
    # The port alone proves nothing (another process may have rebound it);
    # the socat we spawned must still be running for the entry to count.
    return entry["proc"].poll() is None and _port_open(entry["port"])


def _bridge_url(cdp_url):
    parsed = urlparse(cdp_url)
    if parsed.hostname in ("127.0.0.1", "localhost"):
        return cdp_url
    target = f"{parsed.hostname}:{parsed.port or 80}"
    with _bridges_lock:
        entry = _bridges.get(target)
        if entry and _alive(entry):
            return f"http://127.0.0.1:{entry['port']}"
        if entry:
            _reap(entry["proc"])
            _bridges.pop(target, None)
        # Opportunistically drop dead entries, then enforce the cap.
        for t in [t for t, e in _bridges.items() if e["proc"].poll() is not None]:
            _reap(_bridges[t]["proc"])
            _bridges.pop(t, None)
        if len(_bridges) >= MAX_BRIDGES:
            raise RuntimeError(f"more than {MAX_BRIDGES} live CDP bridges; refusing to open another")
        port = _free_loopback_port()
        proc = _spawn_bridge(target, port)
        if not _port_open(port, timeout=5.0):
            _reap(proc)
            raise RuntimeError(f"CDP bridge to {target} did not become ready")
        _bridges[target] = {"port": port, "proc": proc}
        return f"http://127.0.0.1:{port}"


def _reap_bridges():
    with _bridges_lock:
        for entry in _bridges.values():
            try:
                _reap(entry["proc"])
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
        raise Refused(403, "request-supplied cdp_url is disabled (BU_ALLOW_REQUEST_ENDPOINTS!=1)")
    return cdp or CDP_URL


def _resolve_llm(req):
    base_url, model = req.get("base_url"), req.get("model")
    if (base_url or model) and not ALLOW_REQUEST_ENDPOINTS:
        raise Refused(403, "request-supplied base_url/model is disabled (BU_ALLOW_REQUEST_ENDPOINTS!=1)")
    return _llm(base_url, model)


def _resolve_max_steps(req):
    try:
        max_steps = int(req.get("max_steps", 25))
    except (TypeError, ValueError, OverflowError):
        # OverflowError covers JSON non-finite numbers (Infinity, NaN).
        raise Refused(400, "max_steps must be a finite integer") from None
    return max(1, min(max_steps, MAX_STEPS_LIMIT))


def _distinct_urls(history):
    """AgentHistoryList.urls() is one entry per history item, None-padded,
    about:blank included; the useful history is the distinct real pages,
    in first-visit order."""
    seen = set()
    urls = []
    for u in _maybe_call(history, "urls", []) or []:
        if not u or u == "about:blank" or u in seen:
            continue
        seen.add(u)
        urls.append(u)
    return urls


async def _run(req):
    # Policy and validation FIRST, in order of cheapest and most
    # safety-relevant: nothing below may attach to a browser, open a
    # bridge, or navigate before the request has been fully admitted.
    task = req.get("task") or ""
    if not task:
        raise Refused(400, "task is required")
    cdp = _resolve_cdp(req)
    llm = _resolve_llm(req)
    max_steps = _resolve_max_steps(req)

    browser = Browser(cdp_url=_bridge_url(cdp), headless=bool(req.get("headless", True)))

    # The request's url is honoured through the agent's initial_actions:
    # they run inside Agent.run() after the session has started. Calling
    # BrowserSession.navigate_to directly is a silent no-op on a session
    # that has not started (browser-use 0.13.10 returns early when no
    # target is focused), so navigation must NOT happen here.
    agent_kwargs = {}
    url = req.get("url") or ""
    if url:
        agent_kwargs["initial_actions"] = [{"navigate": {"url": url, "new_tab": False}}]
    agent = Agent(task=task, llm=llm, browser=browser, max_steps=max_steps, **agent_kwargs)

    started = time.monotonic()
    history = await agent.run()
    # errors() can yield a bare None when the run was clean; only real
    # errors belong in the history a caller grades on.
    urls = _distinct_urls(history)
    return {
        "ok": True,
        "final_result": _maybe_call(history, "final_result", "") or "",
        "steps": len(_maybe_call(history, "agent_steps", []) or _maybe_call(history, "history", []) or []),
        "duration_s": round(time.monotonic() - started, 3),
        "errors": [str(e) for e in (_maybe_call(history, "errors", []) or []) if e],
        "urls": urls,
        "urls_visited": len(urls),
        "browser_use": BROWSER_USE_VERSION,
    }


class Handler(BaseHTTPRequestHandler):
    # Bound the per-connection read window: a slow body writer must not
    # hold a thread forever (the server is threaded, not async).
    timeout = 30

    def _send(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _host_allowed(self):
        host = (self.headers.get("Host") or "").split(":")[0].strip().lower()
        return host in ALLOWED_HOSTS

    def do_GET(self):
        if not self._host_allowed():
            self._send(403, {"error": "Host not allowed"})
            return
        if self.path == "/healthz":
            self._send(200, {
                "ok": True,
                "browser_use": BROWSER_USE_VERSION,
                "cdp_configured": bool(CDP_URL),
            })
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        if not self._host_allowed():
            self._send(403, {"error": "Host not allowed"})
            return
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
        except Refused as exc:
            self._send(exc.status, {"ok": False, "error": str(exc)})
        except Exception as exc:  # noqa: BLE001 - service boundary
            self._send(500, {"ok": False, "error": str(exc)})

    def log_message(self, fmt, *args):  # quiet; stdout stays clean for logs
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
