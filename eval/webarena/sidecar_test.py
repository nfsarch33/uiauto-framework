"""Unit tests for the webarena sidecar's gym-free surface.

Run: python3 -m unittest discover -s eval/webarena
"""

from __future__ import annotations

import json
import sys
import threading
import unittest
import urllib.request
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import sidecar as webarena_sidecar  # noqa: E402  (the module under test)

from sidecar import normalize_task  # noqa: E402


class NormalizeTask(unittest.TestCase):
    def test_short_id_gets_the_browsergym_prefix(self):
        self.assertEqual(normalize_task("webarenalite.0"), "browsergym/webarenalite.0")

    def test_prefixed_id_passes_through_untouched(self):
        self.assertEqual(normalize_task("browsergym/webarenalite.32"), "browsergym/webarenalite.32")


class StubSidecar:
    """Duck-typed stand-in: proves the shared miniwob handler drives any
    object with start/step/close, without importing a gym."""

    def start(self, task: str) -> dict:
        return {"obs": "GOAL: x", "task": normalize_task(task)}

    def step(self, action: str) -> dict:
        return {"obs": "y", "reward": 1.0, "done": True}

    def close(self) -> None:
        pass


class SharedHandlerWiring(unittest.TestCase):
    def test_start_and_step_through_the_imported_handler(self):
        webarena_sidecar.Handler.sidecar = StubSidecar()
        from http.server import HTTPServer

        server = HTTPServer(("127.0.0.1", 0), webarena_sidecar.Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            base = f"http://127.0.0.1:{server.server_port}"

            def post(path: str, body: dict) -> dict:
                req = urllib.request.Request(
                    base + path, data=json.dumps(body).encode(), headers={"Content-Type": "application/json"}
                )
                with urllib.request.urlopen(req, timeout=5) as resp:
                    return json.loads(resp.read())

            with urllib.request.urlopen(base + "/healthz", timeout=5) as resp:
                self.assertEqual(json.loads(resp.read()), {"ok": True})
            start = post("/start", {"task": "webarenalite.0"})
            self.assertEqual(start["task"], "browsergym/webarenalite.0")
            self.assertEqual(post("/step", {"action": "stop()"}), {"obs": "y", "reward": 1.0, "done": True})
        finally:
            server.shutdown()


if __name__ == "__main__":
    unittest.main()
