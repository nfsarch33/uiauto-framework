#!/usr/bin/env python3
"""Unit rows for the done-shape coercion (sanctioned by the nightly
regression after the done-shape residual survived two nights).

Run: python3 -m unittest discover -s containers/browser-use -p 'server_test.py'
"""

import asyncio
import json
import unittest
from types import SimpleNamespace

# server.py imports browser_use at module scope; the tests only need the
# pure helpers and the wrapper classes, so import the module with the
# dependency stubbed rather than requiring the full container image.
import importlib.util
import sys
import types

_stub = types.ModuleType("browser_use")
_stub_llm = types.ModuleType("browser_use.llm")
class _FakeChatOpenAI:  # noqa: D401 - minimal stand-in for the real base
    def __init__(self, **kwargs):
        self.kwargs = kwargs
        self.stub_client = None

    def get_client(self):
        return self.stub_client
class _FakeAgent:  # noqa: D401 - the run path is exercised in the container
    pass
class _FakeBrowser:
    pass
sys.modules.setdefault("browser_use", _stub)
sys.modules.setdefault("browser_use.llm", _stub_llm)
_stub.Agent = _FakeAgent
_stub.Browser = _FakeBrowser
_stub_llm.ChatOpenAI = _FakeChatOpenAI

# server.py reads the installed browser-use version at import; the host
# has no browser-use, so the metadata lookup is stubbed to the pinned one.
_meta = types.ModuleType("importlib.metadata")
_meta.version = lambda name: "0.13.10"
sys.modules["importlib.metadata"] = _meta

_spec = importlib.util.spec_from_file_location(
    "bu_server", __file__.rsplit("/", 1)[0] + "/server.py"
)
server = importlib.util.module_from_spec(_spec)
try:
    _spec.loader.exec_module(server)
except Exception as exc:  # pragma: no cover - import drift guard
    raise SystemExit(f"server.py no longer imports standalone: {exc}")


def completion(content):
    """An OpenAI-shaped completion response with one choice."""
    return SimpleNamespace(
        choices=[SimpleNamespace(message=SimpleNamespace(content=content), finish_reason="stop")]
    )


class DoneText(unittest.TestCase):
    def test_input_dict_collapses_to_its_string_values(self):
        # The exact nightly shape: done(input: {index, value}).
        self.assertEqual(
            server._done_text({"input": {"index": 889, "value": "Ada Lovelace"}}),
            "Ada Lovelace",
        )

    def test_instruction_string_becomes_the_text(self):
        self.assertEqual(server._done_text({"instruction": "Extract the heading"}), "Extract the heading")

    def test_existing_text_wins_and_blank_extras_are_dropped(self):
        self.assertEqual(server._done_text({"text": "the answer", "note": ""}), "the answer")

    def test_non_dict_non_string_is_none_untouched(self):
        self.assertIsNone(server._done_text(7))


class CoerceDoneShape(unittest.TestCase):
    RAW = (
        '{"thinking": "about it", "action": ['
        '{"done": {"input": {"index": 889, "value": "Ada Lovelace"}}}]}'
    )

    def test_done_extra_collapses_to_text(self):
        got = json.loads(server.coerce_done_shape(self.RAW))
        self.assertEqual(got["action"][0]["done"], {"text": "Ada Lovelace"})
        self.assertEqual(got["thinking"], "about it")

    def test_non_done_actions_and_clean_done_are_untouched(self):
        raw = '{"action": [{"click": {"index": 5}}, {"done": {"text": "ok"}}]}'
        self.assertEqual(server.coerce_done_shape(raw), raw)

    def test_no_done_means_no_reparse(self):
        raw = '{"action": [{"click": {"index": 5}}]}'
        self.assertEqual(server.coerce_done_shape(raw), raw)

    def test_unparseable_text_passes_through_verbatim(self):
        raw = "the model rambled and never produced JSON"
        self.assertEqual(server.coerce_done_shape(raw), raw)

    def test_action_not_a_list_passes_through(self):
        raw = '{"action": {"done": {"input": 1}}}'
        self.assertEqual(server.coerce_done_shape(raw), raw)


class CoercingCompletions(unittest.TestCase):
    async def _run(self, content):
        inner = SimpleNamespace(
            create=lambda **kw: _Async(content),
        )
        shim = server._CoercingCompletions(inner)
        return await shim.create(model="m", messages=[])

    def test_create_rewrites_the_done_shape_in_flight(self):
        import asyncio

        raw = '{"action": [{"done": {"instruction": "Extract the heading"}}]}'
        resp = asyncio.run(self._run(raw))
        self.assertEqual(
            resp.choices[0].message.content,
            '{"action": [{"done": {"text": "Extract the heading"}}]}',
        )

    def test_create_survives_a_broken_response_object(self):
        import asyncio

        class Boom:
            choices = None

        async def broken(**kw):
            raise RuntimeError("provider down")

        shim = server._CoercingCompletions(SimpleNamespace(create=broken))
        with self.assertRaises(RuntimeError):
            asyncio.run(shim.create())

    def test_other_attributes_reach_the_inner_resource(self):
        inner = SimpleNamespace(create=None, model="m")
        self.assertEqual(server._CoercingCompletions(inner).model, "m")


class WiringThroughTheRealPath(unittest.TestCase):
    """The joined chain the library actually calls:
    get_client().chat.completions.create — the r1 wrapper sat on
    client.completions and never fired (the nightly proved it)."""

    def _llm_with_stub(self, content):
        import asyncio
        from types import SimpleNamespace

        async def create(**kw):
            return completion(content)

        completions = SimpleNamespace(create=create)
        chat = SimpleNamespace(completions=completions)
        client = SimpleNamespace(chat=chat, completions=SimpleNamespace(create=create))
        llm = server.DoneShapeChatOpenAI(model="m", base_url="http://127.0.0.1:1", api_key="k")
        llm.stub_client = client
        return llm

    def test_chat_completions_create_coerces_the_nightly_shape(self):
        import asyncio

        llm = self._llm_with_stub(
            '{"action": [{"done": {"input": {"index": 889, "value": "Ada Lovelace"}}}]}'
        )
        resp = asyncio.run(llm.get_client().chat.completions.create(model="m", messages=[]))
        self.assertEqual(
            resp.choices[0].message.content,
            '{"action": [{"done": {"text": "Ada Lovelace"}}]}',
        )

    def test_honest_failure_passes_through_the_path(self):
        import asyncio

        raw = '{"action": [{"done": {"text": "no", "success": false}}]}'
        llm = self._llm_with_stub(raw)
        resp = asyncio.run(llm.get_client().chat.completions.create(model="m", messages=[]))
        self.assertEqual(resp.choices[0].message.content, raw)


class LLMUsesSubclass(unittest.TestCase):
    def test_llm_returns_the_coercing_subclass(self):
        import os

        os.environ.setdefault("BU_LLM_BASE_URL", "http://127.0.0.1:1")
        os.environ.setdefault("BU_LLM_MODEL", "test-model")
        llm = server._llm("http://127.0.0.1:2", "other-model")
        self.assertIsInstance(llm, server.DoneShapeChatOpenAI)
        self.assertEqual(llm.kwargs["model"], "other-model")


class _Async:
    def __init__(self, content):
        self._content = content

    def __await__(self):
        async def coro():
            return completion(self._content)

        return coro().__await__()



def _cdp_server():
    """A CDP endpoint that answers /json/version, like a live Chrome."""
    import http.server
    import threading

    class H(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path == "/json/version":
                body = b'{"Browser": "Chrome/120.0.0.0"}'
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            else:
                self.send_response(404)
                self.end_headers()

        def log_message(self, *a):
            pass

    srv = http.server.HTTPServer(("127.0.0.1", 0), H)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv


def _closed_port():
    """A port with nothing behind it: the CDP endpoint is gone."""
    import socket

    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


class HealthzProbesCDP(unittest.TestCase):
    """/healthz must answer ok=false when the attached Chrome is gone.

    A service that answers ok=true while the CDP endpoint is down lets a
    run score 3/30 and the textfile stay green. The probe
    is the CDP /json/version fetch, over the same endpoint the browser
    attaches to.
    """

    def test_probe_is_true_when_the_cdp_answers(self):
        srv = _cdp_server()
        try:
            url = "http://127.0.0.1:%d" % srv.server_address[1]
            self.assertTrue(server._cdp_reachable(url))
        finally:
            srv.shutdown()
            srv.server_close()

    def test_probe_is_false_when_the_cdp_is_down(self):
        self.assertFalse(server._cdp_reachable("http://127.0.0.1:%d" % _closed_port()))

    def test_probe_is_false_when_no_cdp_is_configured(self):
        self.assertFalse(server._cdp_reachable(""))

    def test_health_payload_reports_not_ok_when_the_probe_fails(self):
        payload = server._health_payload("http://127.0.0.1:%d" % _closed_port())
        self.assertFalse(payload["ok"])
        self.assertFalse(payload["cdp_reachable"])
        self.assertTrue(payload["cdp_configured"])

    def test_health_payload_reports_ok_when_the_probe_answers(self):
        srv = _cdp_server()
        try:
            url = "http://127.0.0.1:%d" % srv.server_address[1]
            payload = server._health_payload(url)
            self.assertTrue(payload["ok"])
            self.assertTrue(payload["cdp_reachable"])
        finally:
            srv.shutdown()
            srv.server_close()


class HealthzEndpointReflectsCDP(unittest.TestCase):
    """The ENDPOINT, not just the helper: /healthz must answer ok=false
    when the attached Chrome is gone.

    The old handler returned a hardcoded ok=true, so the nightly read a
    green lane off a dead CDP (2026-10-13) and the verdict gauge stayed
    green for two nights. Mutant this kills: the handler bypassing the
    probe (a body built without _health_payload)."""

    def _serve(self):
        import http.server
        import threading

        srv = http.server.ThreadingHTTPServer(("127.0.0.1", 0), server.Handler)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        return srv

    def _healthz(self, port):
        import urllib.request

        with urllib.request.urlopen("http://127.0.0.1:%d/healthz" % port, timeout=5) as resp:
            return json.loads(resp.read().decode())

    def test_healthz_reports_not_ok_when_the_cdp_is_down(self):
        old_cdp = server.CDP_URL
        server.CDP_URL = "http://127.0.0.1:%d" % _closed_port()
        srv = self._serve()
        try:
            body = self._healthz(srv.server_address[1])
        finally:
            srv.shutdown()
            srv.server_close()
            server.CDP_URL = old_cdp
        self.assertFalse(body["ok"])
        self.assertFalse(body["cdp_reachable"])
        self.assertTrue(body["cdp_configured"])

    def test_healthz_reports_ok_when_the_cdp_answers(self):
        cdp = _cdp_server()
        old_cdp = server.CDP_URL
        server.CDP_URL = "http://127.0.0.1:%d" % cdp.server_address[1]
        srv = self._serve()
        try:
            body = self._healthz(srv.server_address[1])
        finally:
            srv.shutdown()
            srv.server_close()
            server.CDP_URL = old_cdp
            cdp.shutdown()
            cdp.server_close()
        self.assertTrue(body["ok"])
        self.assertTrue(body["cdp_reachable"])


if __name__ == "__main__":
    unittest.main()


class ReviewerRound1DeclaredFields(unittest.TestCase):
    """Round-1 blocker rows: the coercion must keep every field DoneAction
    declares (text, success, files_to_display) and only collapse the
    UNDECLARED extras into text — otherwise done(text, success=false) is
    rewritten into a validating success and the nightly counts a failed
    run as a pass."""

    def test_all_declared_keys_pass_through_unchanged(self):
        raw = '{"action": [{"done": {"text": "no", "success": false}}]}'
        self.assertEqual(server.coerce_done_shape(raw), raw)

    def test_declared_success_survives_an_extras_collapse(self):
        got = json.loads(
            server.coerce_done_shape(
                '{"action": [{"done": {"input": {"value": "Ada Lovelace"}, "success": false}}]}'
            )
        )
        self.assertEqual(got["action"][0]["done"]["text"], "Ada Lovelace")
        self.assertIs(got["action"][0]["done"]["success"], False)

    def test_files_to_display_survives_too(self):
        got = json.loads(
            server.coerce_done_shape(
                '{"action": [{"done": {"files_to_display": ["/a.png"], "note": "chart attached"}}]}'
            )
        )
        self.assertEqual(got["action"][0]["done"]["files_to_display"], ["/a.png"])
        self.assertEqual(got["action"][0]["done"]["text"], "chart attached")


class _FakeBrowserKwargs:
    """A browser stand-in that takes the constructor keywords the service
    passes (the module stub takes none)."""

    def __init__(self, **kwargs):
        self.kwargs = kwargs


class _HangingAgent:
    """An agent whose run never returns - the shape of a wedged LLM call or a
    CDP target that stopped answering."""

    def __init__(self, **kwargs):
        self.kwargs = kwargs
        self.history = SimpleNamespace(urls=lambda: ["https://example.com/", "about:blank"])

    async def run(self):
        await asyncio.sleep(3600)


class BoundedRun(unittest.TestCase):
    """A run that outruns its budget must answer with a bounded error that
    still carries the pages the agent saw. A caller's deadline firing leaves
    no body at all, and a run with no body has no page history to grade."""

    def _bounded_run(self, request):
        saved = (server.Agent, server.Browser, server.CDP_URL, server.LLM_BASE_URL, server.LLM_MODEL)
        server.Agent = _HangingAgent
        server.Browser = _FakeBrowserKwargs
        server.CDP_URL = "http://127.0.0.1:9222"
        server.LLM_BASE_URL = "http://llm.test"
        server.LLM_MODEL = "test-model"
        try:
            # The row's own bound: without the service's, the call hangs and
            # the suite hangs with it.
            return asyncio.run(asyncio.wait_for(server._run(request), timeout=5))
        finally:
            (server.Agent, server.Browser, server.CDP_URL,
             server.LLM_BASE_URL, server.LLM_MODEL) = saved

    def test_a_hanging_run_answers_with_a_bounded_error_and_the_page_history(self):
        result = self._bounded_run({"task": "t", "timeout_s": 1})
        self.assertFalse(result["ok"])
        self.assertTrue(any("budget" in e for e in result["errors"]), result["errors"])
        self.assertEqual(result["urls"], ["https://example.com/"])

    def test_the_service_bounds_a_run_that_names_no_budget(self):
        saved = getattr(server, "RUN_TIMEOUT_S", 150)
        server.RUN_TIMEOUT_S = 1
        try:
            result = self._bounded_run({"task": "t"})
        finally:
            server.RUN_TIMEOUT_S = saved
        self.assertFalse(result["ok"])
        self.assertTrue(any("budget" in e for e in result["errors"]), result["errors"])


class DoneShapeCounterRows(unittest.TestCase):
    """The done-shape coercion must be countable.

    The coercion is silent: a run that fired it looks exactly like a run
    that did not. The nightly read "5 of 30 records still log
    DoneActionModel errors" out of a 30/30 pass and had no way to tell
    whether the coercion had fixed those records or whether the errors
    were still there. These counters are that missing read: the done()
    shapes the coercion saw, the ones it rewrote, and the ones it could
    not fix at all.
    """

    def _delta(self, fn):
        before = server.DONE_SHAPE_COUNTER.snapshot()
        fn()
        after = server.DONE_SHAPE_COUNTER.snapshot()
        return {k: after[k] - before[k] for k in after}

    def test_a_nightly_shape_is_counted_as_coerced(self):
        delta = self._delta(
            lambda: server.coerce_done_shape(
                '{"action": [{"done": {"input": {"index": 889, "value": "Ada Lovelace"}}}]}'
            )
        )
        self.assertEqual(delta["seen"], 1)
        self.assertEqual(delta["coerced"], 1)
        self.assertEqual(delta["uncoerced"], 0)

    def test_a_clean_done_is_seen_but_not_coerced(self):
        delta = self._delta(
            lambda: server.coerce_done_shape('{"action": [{"done": {"text": "ok"}}]}')
        )
        self.assertEqual(delta["seen"], 1)
        self.assertEqual(delta["coerced"], 0)
        self.assertEqual(delta["uncoerced"], 0)

    def test_the_20261012_repro_shapes_are_accepted_and_counted(self):
        # The three shapes the 2026-10-12 report recorded as
        # "45 validation errors for AgentOutput action.0.DoneActionModel.*".
        for payload in (
            '{"action": [{"done": {"input": {"index": 889, "value": "Ada Lovelace"}}}]}',
            '{"action": [{"done": {"search_page": {"query": "Example Domain"}}}]}',
            '{"action": [{"done": {"extract": {"prompt": "What is the heading?"}}}]}',
        ):
            with self.subTest(payload=payload):
                before = server.DONE_SHAPE_COUNTER.snapshot()
                out = json.loads(server.coerce_done_shape(payload))
                after = server.DONE_SHAPE_COUNTER.snapshot()
                done = out["action"][0]["done"]
                self.assertLessEqual(set(done.keys()), {"text", "success", "files_to_display"})
                self.assertEqual(after["coerced"], before["coerced"] + 1)
                self.assertEqual(after["uncoerced"], before["uncoerced"])

    def test_a_done_the_coercion_cannot_fix_is_counted_as_uncoerced(self):
        delta = self._delta(lambda: server.coerce_done_shape('{"action": [{"done": 7}]}'))
        self.assertEqual(delta["seen"], 1)
        self.assertEqual(delta["coerced"], 0)
        self.assertEqual(delta["uncoerced"], 1)

    def test_the_health_payload_carries_the_counter(self):
        payload = server._health_payload("")
        self.assertIn("done_shape", payload)
        for key in ("seen", "coerced", "uncoerced"):
            self.assertIsInstance(payload["done_shape"][key], int)


class ActionShapeCoercionRows(unittest.TestCase):
    """The action shapes the 2026-10-12 night run recorded, coerced.

    The night run's validation errors hold the failing action elements as
    library's own validation error reports them: an action the model
    named, carrying the field under a name that action does not declare.
    The coercion renames the field and leaves the action alone.
    """

    # Every action element the run recorded and the library rejected, and
    # the shape each one coerces into. Ten of them, across the three
    # element indices and the four values the run typed. The indices and
    # the values are the ones the run used.
    REPORTED = (
        (
            {"input": {"index": 135, "value": "Test User"}},
            {"input": {"index": 135, "text": "Test User"}},
        ),
        (
            {"input": {"index": 194, "value": "Ada Lovelace"}},
            {"input": {"index": 194, "text": "Ada Lovelace"}},
        ),
        (
            {"input": {"index": 194, "value": "Ada Lovelace", "submit": False}},
            {"input": {"index": 194, "text": "Ada Lovelace", "submit": False}},
        ),
        (
            {"input": {"index": 241, "value": "Test User"}},
            {"input": {"index": 241, "text": "Test User"}},
        ),
        (
            {"input": {"index": 241, "value": "UIAuto Tester"}},
            {"input": {"index": 241, "text": "UIAuto Tester"}},
        ),
        (
            {"input": {"index": 194, "value": "Test User"}},
            {"input": {"index": 194, "text": "Test User"}},
        ),
        (
            {"input": {"index": 241, "value": "Ada Lovelace"}},
            {"input": {"index": 241, "text": "Ada Lovelace"}},
        ),
        (
            {"extract": {"prompt": "What is the main heading on this page?"}},
            {"extract": {"query": "What is the main heading on this page?"}},
        ),
        (
            {"search_page": {"query": "Example Domain"}},
            {"search_page": {"pattern": "Example Domain"}},
        ),
        (
            {"text": "Confirmed: The draft is published. DRAFT NOT LIVE"},
            # Prose the model wrote instead of an action: the wrapper is added and the
            # action did NOT succeed, so success is stated false rather than left to
            # DoneAction's default of true.
            {"done": {"text": "Confirmed: The draft is published. DRAFT NOT LIVE", "success": False}},
        ),
    )

    def _coerced(self, element):
        raw = json.dumps({"action": [element]})
        return json.loads(server.coerce_done_shape(raw))["action"][0]

    def test_every_shape_the_report_recorded_is_coerced(self):
        for element, expected in self.REPORTED:
            with self.subTest(element=element):
                self.assertEqual(self._coerced(element), expected)

    def test_the_input_value_shape_is_renamed_to_text(self):
        self.assertEqual(
            self._coerced({"input": {"index": 194, "value": "Ada Lovelace"}}),
            {"input": {"index": 194, "text": "Ada Lovelace"}},
        )

    def test_the_input_submit_shape_keeps_submit(self):
        self.assertEqual(
            self._coerced({"input": {"index": 194, "value": "Ada Lovelace", "submit": False}}),
            {"input": {"index": 194, "text": "Ada Lovelace", "submit": False}},
        )

    def test_the_extract_prompt_shape_is_renamed_to_query(self):
        self.assertEqual(
            self._coerced({"extract": {"prompt": "What is the heading?"}}),
            {"extract": {"query": "What is the heading?"}},
        )

    def test_the_search_page_query_shape_stays_a_page_search(self):
        got = self._coerced({"search_page": {"query": "Example Domain"}})
        self.assertEqual(got, {"search_page": {"pattern": "Example Domain"}})
        # The action the model chose is not swapped for another one: a web
        # search would drive the browser out to a search engine.
        self.assertNotIn("search", got)

    def test_the_unwrapped_done_shape_is_wrapped(self):
        self.assertEqual(
            self._coerced({"text": "Confirmed: DRAFT NOT LIVE"}),
            # success=false: an unwrapped done() whose success is unstated would
            # default to true and end the run as a success.
            {"done": {"text": "Confirmed: DRAFT NOT LIVE", "success": False}},
        )

    def test_a_bare_string_action_becomes_a_done(self):
        self.assertEqual(
            self._coerced("click the submit button"),
            # success=false, for the same reason: prose is not a success.
            {"done": {"text": "click the submit button", "success": False}},
        )

    def test_a_clean_action_is_left_exactly_as_it_came(self):
        for element in (
            {"click": {"index": 5}},
            {"search": {"query": "Example Domain"}},
            {"search_page": {"pattern": "Example Domain"}},
            {"screenshot": {}},
            {"done": {"text": "ok"}},
        ):
            with self.subTest(element=element):
                raw = json.dumps({"action": [element]})
                self.assertEqual(server.coerce_done_shape(raw), raw)

    def test_an_action_missing_a_field_the_coercion_cannot_name_is_left_alone(self):
        raw = json.dumps({"action": [{"input": {"index": 5}}]})
        self.assertEqual(server.coerce_done_shape(raw), raw)


class ActionShapeCounterRows(unittest.TestCase):
    """The shape coercion must be countable.

    The coercion is silent: a run that fired it looks exactly like a run
    that did not, so a nightly reading "5 of 30 records still log
    DoneActionModel errors" could not tell whether the coercion had fixed
    those records or whether the errors were still there. The counter
    covers every action element the coercion inspects, so a shape the
    coercion fixed and a shape it could not fix are two different
    numbers.
    """

    def _delta(self, fn):
        before = server.DONE_SHAPE_COUNTER.snapshot()
        fn()
        after = server.DONE_SHAPE_COUNTER.snapshot()
        return {k: after[k] - before[k] for k in after}

    def test_a_coerced_shape_is_counted_as_coerced(self):
        delta = self._delta(
            lambda: server.coerce_done_shape('{"action": [{"input": {"index": 1, "value": "x"}}]}')
        )
        self.assertEqual(delta["seen"], 1)
        self.assertEqual(delta["coerced"], 1)
        self.assertEqual(delta["uncoerced"], 0)

    def test_an_action_the_coercion_cannot_fix_is_counted_as_uncoerced(self):
        delta = self._delta(
            lambda: server.coerce_done_shape('{"action": [{"not_an_action": {}}]}')
        )
        self.assertEqual(delta["seen"], 1)
        self.assertEqual(delta["coerced"], 0)
        self.assertEqual(delta["uncoerced"], 1)

    def test_the_counter_counts_every_action_element_it_inspects(self):
        delta = self._delta(
            lambda: server.coerce_done_shape(
                '{"action": [{"click": {"index": 5}}, {"screenshot": {}}]}'
            )
        )
        self.assertEqual(delta["seen"], 2)
        self.assertEqual(delta["coerced"], 0)
        self.assertEqual(delta["uncoerced"], 0)


class PinnedLibraryAgreement(unittest.TestCase):
    """The action table and the coerced shapes must agree with the
    pinned library itself.

    These rows need browser_use importable. Where it is not, they skip
    -- and the PR body carries the run that did have it.
    """

    def _real_library(self):
        """Import the real browser_use, with the stub put back after."""
        stub = sys.modules.get("browser_use")
        for name in [
            m for m in list(sys.modules) if m == "browser_use" or m.startswith("browser_use.")
        ]:
            del sys.modules[name]
        try:
            from browser_use.tools.service import Tools
        except Exception as exc:  # pragma: no cover - the dev host has no browser_use
            sys.modules["browser_use"] = stub
            self.skipTest("browser_use is not importable here: %s" % exc)

        def restore():
            for name in [
                m for m in list(sys.modules) if m == "browser_use" or m.startswith("browser_use.")
            ]:
                del sys.modules[name]
            sys.modules["browser_use"] = stub

        return Tools, restore

    def test_the_action_table_matches_the_installed_library(self):
        Tools, restore = self._real_library()
        try:
            registry = Tools().registry
            declared = {
                name: frozenset(
                    field
                    for field, info in action.param_model.model_fields.items()
                    if info.is_required()
                )
                for name, action in registry.registry.actions.items()
            }
        finally:
            restore()
        self.assertEqual(
            {name: frozenset(fields) for name, fields in server.ACTION_REQUIRED_FIELDS.items()},
            declared,
        )

    def test_the_coerced_shapes_validate_against_the_installed_library(self):
        Tools, restore = self._real_library()
        try:
            from browser_use.agent.views import AgentOutput

            model = AgentOutput.type_with_custom_actions(Tools().registry.create_action_model())
        finally:
            restore()
        for element in (
            {"input": {"index": 194, "value": "Ada Lovelace"}},
            {"input": {"index": 194, "value": "Ada Lovelace", "submit": False}},
            {"extract": {"prompt": "What is the main heading on this page?"}},
            {"search_page": {"query": "Example Domain"}},
            {"text": "Confirmed: The draft is published. DRAFT NOT LIVE"},
            "click the submit button",
        ):
            with self.subTest(element=element):
                raw = json.dumps({"action": [element]})
                model.model_validate_json(server.coerce_done_shape(raw))


class CoercedProseIsNotASuccess(unittest.TestCase):
    """Prose the model wrote instead of an action must not end the run as a
    success.

    The pinned library's DoneAction declares `success`, and it DEFAULTS to
    true. A coercion that builds a done() without that field therefore turns
    model prose ("DRAFT NOT LIVE") into a successful run - the fail-open the
    coercion's own docstring says cannot happen. These rows assert that both
    coerced prose shapes carry success=false.
    """

    def _done(self, raw):
        got = json.loads(server.coerce_done_shape(raw))
        return got["action"][0]["done"]

    def test_a_bare_string_action_is_coerced_to_a_failed_done(self):
        done = self._done('{"action": ["DRAFT NOT LIVE"]}')
        self.assertEqual(done["text"], "DRAFT NOT LIVE")
        self.assertIs(done["success"], False)

    def test_an_unwrapped_text_action_is_coerced_to_a_failed_done(self):
        done = self._done('{"action": [{"text": "DRAFT NOT LIVE"}]}')
        self.assertEqual(done["text"], "DRAFT NOT LIVE")
        self.assertIs(done["success"], False)
