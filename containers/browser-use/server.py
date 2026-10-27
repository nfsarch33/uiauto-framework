"""HTTP executor service for the browser-use container.

POST /run     {"task": ..., "url": ..., "max_steps": ..., "cdp_url": ...,
               "model": ..., "base_url": ..., "headless": ...,
               "timeout_s": ...} -> run result
GET  /healthz -> {"ok": <the attached browser answered>, "browser_use": "<version>",
                  "cdp_configured": bool, "cdp_reachable": bool,
                  "done_shape": {"seen": int, "coerced": int, "uncoerced": int}}

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
per-request endpoint overrides), BU_LLM_AGENT_HEADER / BU_LLM_AGENT_ID
(caller-identity header sent to the LLM endpoint; default
X-Helixon-Agent: browser-use; empty id disables it),
BU_RUN_TIMEOUT_S (the service's own bound on one run; default 150).
"""
import asyncio
import atexit
import json
import os
import socket
import subprocess
import threading
import time
import urllib.request
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
# The CDP probe timeout for /healthz. The endpoint is on the host loopback
# (or a socat bridge to it), so two seconds is generous; the health gate
# that waits for this service polls every two seconds.
CDP_HEALTH_TIMEOUT_S = float(os.environ.get("BU_CDP_HEALTH_TIMEOUT_S", "2.0"))
# Gateways that route or gate per calling agent identify the caller by this
# header; without it the OpenAI client's User-Agent is sniffed and this lane
# can be mistaken for another tool. Empty BU_LLM_AGENT_ID sends no header.
LLM_AGENT_HEADER = os.environ.get("BU_LLM_AGENT_HEADER", "X-Helixon-Agent")
LLM_AGENT_ID = os.environ.get("BU_LLM_AGENT_ID", "browser-use")
MAX_BODY_BYTES = 1 << 20
MAX_STEPS_LIMIT = 200
# The service's own bound on one run. A run that hangs (a wedged LLM
# call, a CDP target that stopped answering) must not hold the request
# open until the caller's deadline: the caller then gets no body at all,
# and a run with no body has no page history to grade.
try:
    RUN_TIMEOUT_S = float(os.environ.get("BU_RUN_TIMEOUT_S", "150"))
except (TypeError, ValueError):
    RUN_TIMEOUT_S = 150.0
# The nightly failure class (AgentOutput "Invalid JSON"): the model
# prefixes prose to the JSON object the library parses, and on the
# correction retry packs the final message into a forbidden `input`
# parameter on the done action (DoneActionModel rejects extra inputs).
# The instruction rides the TASK text because browser-use 0.13.10
# exposes no system-prompt extension point on Agent().
JSON_ONLY_SUFFIX = (
    "\n\nIMPORTANT: reply with ONLY the raw JSON object your system prompt asks for. "
    "No prose before or after it, no markdown fences. When you finish, call the done "
    "action with your final message as its text parameter — done(text: \"...\") — "
    "never as an input parameter."
)
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


# --- done-shape coercion ---------------------------------------------------
#
# The models keep emitting done(input: {...}) / done(instruction: '...')
# where the library's DoneActionModel permits ONLY text — pydantic answers
# "Extra inputs are not permitted" and the run dies on a 45-error
# validation. The nightly regression sanctioned this mapping after the
# shape survived the task-suffix fix: coerce the RAW completion before the
# library validates it, collapsing every done() extra into text. Every
# other action is untouched, and anything that fails to parse passes
# through so the library's own error path fires exactly as before.


def _done_text(params):
    """Collapse a done(...) parameter dict into the text string the
    action model wants: the text field if present, else the string
    values (nested dicts included, insertion order) joined with spaces,
    else a JSON rendering so the answer is never silently empty."""
    if isinstance(params, str):
        return params
    if not isinstance(params, dict):
        return None
    if isinstance(params.get("text"), str) and params["text"].strip():
        return params["text"]
    leaves = []

    def _walk(v):
        if isinstance(v, str) and v.strip():
            leaves.append(v)
        elif isinstance(v, dict):
            for x in v.values():
                _walk(x)

    for k, v in params.items():
        if k != "text":
            _walk(v)
    if leaves:
        return " ".join(leaves)
    return json.dumps(params, ensure_ascii=False)


# The fields the pinned library's DoneAction declares. Everything else in
# a done() call is an undeclared extra the action model would reject.
DONE_DECLARED_FIELDS = frozenset({"text", "success", "files_to_display"})

# The actions the pinned library declares, with the fields each one
# REQUIRES. Pinned to browser-use 0.13.10 -- the version the Containerfile
# bakes -- and cross-checked against the installed library by the unit
# suite, so a version bump that changes an action fails loudly instead of
# leaving the coercion a field behind.
#
# A field the payload does not carry is what the action model rejects
# ("Field required"). A field the payload carries under a different NAME
# is the mismatch the models actually produce.
ACTION_REQUIRED_FIELDS = {
    "click": ("index",),
    "close": ("tab_id",),
    "done": ("text",),
    "dropdown_options": ("index",),
    "evaluate": ("code",),
    "extract": ("query",),
    "find_elements": ("selector",),
    "find_text": ("text",),
    "go_back": (),
    "input": ("index", "text"),
    "navigate": ("url",),
    "read_file": ("file_name",),
    "replace_file": ("file_name", "old_str", "new_str"),
    "save_as_pdf": (),
    "screenshot": (),
    "scroll": (),
    "search": ("query",),
    "search_page": ("pattern",),
    "select_dropdown": ("index", "text"),
    "send_keys": ("keys",),
    "switch": ("tab_id",),
    "upload_file": ("index", "path"),
    "wait": (),
    "write_file": ("file_name", "content"),
}

# The field names the models emit where the action declares a different
# one. Measured from the 2026-10-12 night run's validation errors: input
# came back carrying `value`, extract carrying `prompt`, search_page
# carrying `query`. The coercion renames the FIELD and never changes the
# action the model chose -- search_page stays a page search, it does not
# become search, which would drive the browser out to a search engine.
ACTION_FIELD_ALIASES = {
    "input": {"value": "text"},
    "extract": {"prompt": "query"},
    "search_page": {"query": "pattern"},
}


def _coerce_done(done):
    """One done() parameter dict, coerced. Returns None when there is
    nothing to do (every key is declared, or nothing collapsible) so the
    caller leaves the original untouched; otherwise a dict that keeps
    every declared field verbatim and collapses the UNDECLARED extras
    into text — an existing non-empty text wins, success and
    files_to_display are never invented or dropped."""
    if isinstance(done, str):
        return {"text": done}
    if not isinstance(done, dict):
        return None
    undeclared = [k for k in done if k not in DONE_DECLARED_FIELDS]
    if not undeclared:
        return None
    out = {k: v for k, v in done.items() if k in DONE_DECLARED_FIELDS}
    if not (isinstance(out.get("text"), str) and out["text"].strip()):
        collapsed = _done_text({k: done[k] for k in undeclared})
        if collapsed:
            out["text"] = collapsed
    if "text" not in out:
        return None
    return out


def _coerce_element(elem):
    """One element of the model's action list.

    Returns (element, changed, unfixed):
      element  the element to put back into the list
      changed  True when the element was rewritten
      unfixed  True when the element is still a shape the library's
               action model rejects and the coercion could not rewrite

    The shapes handled here are the ones the 2026-10-12 night run
    recorded: the action the model named, carrying the field under the
    wrong name (input/value, extract/prompt, search_page/query), the done
    payload without its `done` wrapper, and a bare string where the model
    should have called done(). Every other element passes through
    untouched, so the library's own validation stays the authority on
    what is broken.
    """
    # A bare string where an action object belongs: the model answered
    # with prose instead of an action. The action model rejects a
    # non-object outright, and the honest reading of that prose is a
    # done() whose text is the prose.
    if isinstance(elem, str):
        # The model answered with prose instead of an action. DoneAction
        # declares `success` and it DEFAULTS TO TRUE, so a done() built
        # without that field would end the run as a SUCCESS - the fail-open
        # this coercion exists to prevent. The honest reading of prose is a
        # done() that did not succeed.
        return {"done": {"text": elem, "success": False}}, True, False
    if not isinstance(elem, dict):
        return elem, False, False

    if "done" in elem:
        done = elem["done"]
        coerced = _coerce_done(done)
        if coerced is None:
            # A done() the coercion could not rewrite. A params dict that
            # carries undeclared extras always collapses (the JSON
            # fallback keeps the text non-empty), so the residual is the
            # non-dict case: the action model rejects it and nothing here
            # can save it. Counted, never silently dropped.
            unfixed = not isinstance(done, dict) or any(
                k not in DONE_DECLARED_FIELDS for k in done
            )
            return elem, False, unfixed
        if coerced == done:
            # The done() itself is clean, but a key beside it is an extra
            # the action model rejects, and rewriting the done() alone
            # cannot fix that.
            return elem, False, len(elem) > 1
        out = dict(elem)
        out["done"] = coerced
        return out, True, len(elem) > 1

    if len(elem) != 1:
        # More than one action in one element: the model expects one
        # action per element and the action model rejects the extras.
        # There is no safe way to pick a winner, so it is left alone and
        # counted as unfixed.
        return elem, False, True

    name = next(iter(elem))
    payload = elem[name]
    if name not in ACTION_REQUIRED_FIELDS:
        # The done payload without its wrapper: the model answered
        # {"text": "..."} where it should have answered
        # {"done": {"text": "..."}}.
        if name == "text" and isinstance(payload, str):
            # The same reasoning as the bare-string case above: the wrapper is
            # added, but the action did not succeed and must not default to
            # saying that it did.
            return {"done": {"text": payload, "success": False}}, True, False
        return elem, False, True
    if not isinstance(payload, dict):
        return elem, False, True
    required = ACTION_REQUIRED_FIELDS[name]
    if all(f in payload for f in required):
        # Every field the action requires is there: the element is the
        # shape the action model wants and is left exactly as it came.
        return elem, False, False
    out = dict(payload)
    renamed = False
    for wrong, right in ACTION_FIELD_ALIASES.get(name, {}).items():
        if wrong in out and right not in out:
            out[right] = out.pop(wrong)
            renamed = True
    if all(f in out for f in required):
        return {name: out}, renamed, False
    # The action still misses a field the coercion has no name for.
    return elem, False, True


def coerce_done_shape(content):
    """Coerce the action shapes the model gets wrong inside a raw model
    completion. Only the action list changes. A done() keeps every field
    DoneAction declares (text, success, files_to_display) and collapses
    only its UNDECLARED extras into text, so an honest
    done(text, success=false) can never be rewritten into a validating
    success. An action the model named with the wrong FIELD name gets
    that field renamed; the action itself is never swapped for another
    one. Unparseable text returns unchanged (the library's own validation
    error is the honest result)."""
    if '"action"' not in content:
        return content
    try:
        parsed = json.loads(content)
    except (json.JSONDecodeError, ValueError):
        return content
    actions = parsed.get("action") if isinstance(parsed, dict) else None
    if not isinstance(actions, list):
        return content
    changed = False
    seen = coerced_count = uncoerced = 0
    for index, elem in enumerate(actions):
        seen += 1
        fixed, elem_changed, unfixed = _coerce_element(elem)
        if elem_changed:
            actions[index] = fixed
            coerced_count += 1
            changed = True
        if unfixed:
            uncoerced += 1
    DONE_SHAPE_COUNTER.record(seen=seen, coerced=coerced_count, uncoerced=uncoerced)
    if not changed:
        return content
    try:
        return json.dumps(parsed, ensure_ascii=False)
    except (TypeError, ValueError):
        return content


# --- done-shape counter ---------------------------------------------------
#
# The coercion above is silent. A run that fired it looks exactly like a
# run that did not, so the nightly read "5 of 30 records still log
# DoneActionModel errors" out of a 30/30 pass and had no way to tell
# whether the coercion had fixed those records or whether the errors
# were still there. These three counters are that missing read:
#
#   seen      action elements the coercion inspected
#   coerced   elements rewritten into a shape the action model accepts
#   uncoerced elements that still fail the library's validation after the
#             coercion ran -- the residual, and the only number that says
#             work remains
#
# A plain in-process counter: no new dependency, and it rides the
# existing /healthz body.


class _DoneShapeCounter:
    """Three counters for the done-shape coercion, safe for the
    threaded server (the stdlib server is threaded, so two runs can
    touch the counter at once)."""

    def __init__(self):
        self._lock = threading.Lock()
        self._seen = 0
        self._coerced = 0
        self._uncoerced = 0

    def record(self, seen=0, coerced=0, uncoerced=0):
        with self._lock:
            self._seen += seen
            self._coerced += coerced
            self._uncoerced += uncoerced

    def snapshot(self):
        with self._lock:
            return {
                "seen": self._seen,
                "coerced": self._coerced,
                "uncoerced": self._uncoerced,
            }


DONE_SHAPE_COUNTER = _DoneShapeCounter()


class _CoercingCompletions:
    """chat.completions shim: the completion text is coerced before the
    library's model_validate_json sees it. Every other attribute and
    call reaches the real resource untouched."""

    def __init__(self, inner):
        self._inner = inner

    async def create(self, *args, **kwargs):
        response = await self._inner.create(*args, **kwargs)
        try:
            for choice in response.choices or []:
                message = getattr(choice, "message", None)
                if message is not None and isinstance(getattr(message, "content", None), str):
                    message.content = coerce_done_shape(message.content)
        except Exception:  # noqa: BLE001 - coercion must never kill a run
            pass
        return response

    def __getattr__(self, name):
        return getattr(self._inner, name)


class _CoercingChat:
    """The CHAT resource shim: completions on it are coerced before the
    library's model_validate_json sees them; every other attribute and
    call reaches the real resource untouched. The wrapper must sit on
    client.CHAT — the library's path is get_client().chat.completions,
    so a client-level .completions wrapper is never in that chain (the
    wiring bug this class replaces)."""

    def __init__(self, inner):
        self._inner = inner
        self.completions = _CoercingCompletions(inner.completions)

    def __getattr__(self, name):
        return getattr(self._inner, name)


class _CoercingClient:
    """The CLIENT shim: hands out the wrapped chat resource."""

    def __init__(self, inner):
        self._inner = inner
        self.chat = _CoercingChat(inner.chat)

    def __getattr__(self, name):
        return getattr(self._inner, name)


class DoneShapeChatOpenAI(ChatOpenAI):
    """ChatOpenAI whose provider responses pass through the done-shape
    coercion. The structured-output request, usage accounting and every
    error path are the library's own — only the completion text is
    rewritten before validation. The wrap happens on the client returned
    by get_client(), at the CHAT level, which is exactly where
    ChatOpenAI.ainvoke's `self.get_client().chat.completions.create`
    call chain passes."""

    def get_client(self):
        return _CoercingClient(super().get_client())


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
    headers = {LLM_AGENT_HEADER: LLM_AGENT_ID} if LLM_AGENT_HEADER and LLM_AGENT_ID else None
    return DoneShapeChatOpenAI(model=name, base_url=base, api_key=api_key, temperature=0, default_headers=headers)


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


def _resolve_run_timeout(req):
    """The budget for one run: the caller's, when it names one, capped by
    the service's own. The caller knows how long it will wait; the cap keeps
    a request without a budget from holding the thread open."""
    requested = req.get("timeout_s")
    try:
        requested = float(requested)
    except (TypeError, ValueError):
        requested = 0.0
    if requested <= 0:
        return RUN_TIMEOUT_S
    return min(requested, RUN_TIMEOUT_S)


def _bounded_failure(agent, budget, started):
    """The answer when a run outruns its budget: a bounded error that
    still carries the pages the agent saw, so the caller can grade the
    navigation that did happen instead of finding an empty history."""
    history = getattr(agent, "history", None)
    urls = _distinct_urls(history)
    return {
        "ok": False,
        "final_result": "",
        "steps": len(_maybe_call(history, "agent_steps", []) or []),
        "duration_s": round(time.monotonic() - started, 3),
        "errors": [f"run exceeded the {budget:g}s budget and was stopped"],
        "urls": urls,
        "urls_visited": len(urls),
        "browser_use": BROWSER_USE_VERSION,
    }


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
    agent = Agent(task=task + JSON_ONLY_SUFFIX, llm=llm, browser=browser, max_steps=max_steps, **agent_kwargs)

    started = time.monotonic()
    budget = _resolve_run_timeout(req)
    try:
        history = await asyncio.wait_for(agent.run(), timeout=budget)
    except (asyncio.TimeoutError, TimeoutError):
        # The run outran its budget: stop it here and answer with the
        # pages it saw. Letting the caller's deadline fire instead
        # leaves the caller with no body and no page history at all.
        return _bounded_failure(agent, budget, started)
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


def _cdp_reachable(cdp_url, timeout=None):
    """Is the attached Chrome actually there? The CDP /json/version fetch,
    over the same endpoint the browser attaches to. A down CDP is the
    failure this probe exists for: /healthz used to answer ok=true while
    the CDP endpoint was gone, so a run scored 3/30 and the textfile
    stayed green for two nights."""
    if not cdp_url:
        return False
    if timeout is None:
        timeout = CDP_HEALTH_TIMEOUT_S
    try:
        url = _bridge_url(cdp_url.rstrip("/")) + "/json/version"
        with urllib.request.urlopen(url, timeout=timeout) as resp:
            return resp.status == 200
    except Exception:
        # Any failure to reach the endpoint is "not reachable": a refused
        # connection, a timeout, a dead bridge. A health probe never
        # raises.
        return False


def _health_payload(cdp_url=None):
    """The /healthz body. ok is the LANE's verdict, not the process's:
    the service is up and the browser it drives is not."""
    if cdp_url is None:
        cdp_url = CDP_URL
    reachable = _cdp_reachable(cdp_url)
    return {
        "ok": reachable,
        "browser_use": BROWSER_USE_VERSION,
        "cdp_configured": bool(cdp_url),
        "cdp_reachable": reachable,
        "done_shape": DONE_SHAPE_COUNTER.snapshot(),
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
            self._send(200, _health_payload())
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
