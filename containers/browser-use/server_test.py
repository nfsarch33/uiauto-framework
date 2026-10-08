#!/usr/bin/env python3
"""Unit rows for the done-shape coercion (sanctioned by the nightly
regression after the done-shape residual survived two nights).

Run: python3 -m unittest discover -s containers/browser-use -p 'server_test.py'
"""

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
