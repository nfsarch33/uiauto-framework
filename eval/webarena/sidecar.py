#!/usr/bin/env python3
"""BrowserGym sidecar for the browsergym-webarena executor (uiauto-framework).

Arena rung 2 (WebArena-Lite). Same JSON contract as the miniwob sidecar
(GOAL + flattened AXTREE in, bid action out, gym reward back); the HTTP
handler and observation renderer are imported from the miniwob module so
the two rungs share one reviewed implementation. This file only swaps
the task registry (browsergym-webarenalite) and the default port.

The agent loop lives in the Go executor; this process never calls a
model. Task ids are BrowserGym names ("webarenalite.0" ->
"browsergym/webarenalite.0"). BrowserGym performs the site login itself
(WebArenaInstance.ui_login) from the WA_* environment variables.

Requires: pip install browsergym-webarenalite webarena && playwright install chromium
Run:      python3 eval/webarena/sidecar.py [--port 8094]
"""

from __future__ import annotations

import argparse
import importlib.util
import sys
from http.server import HTTPServer
from pathlib import Path


def _load_miniwob_module():
    """Reuse the miniwob rung's HTTP handler and obs renderer (goal +
    bid axtree, decorative-first pruning). Both files are named
    sidecar.py, so the import goes by file path under its own module
    name — a sys.path insert would make `import sidecar` resolve to
    THIS file and recurse.
    """
    path = Path(__file__).resolve().parent.parent / "miniwob" / "sidecar.py"
    spec = importlib.util.spec_from_file_location("miniwob_sidecar", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


_miniwob = _load_miniwob_module()
Handler = _miniwob.Handler
render_obs = _miniwob.render_obs


def make_gym():
    try:
        import gymnasium as gym
        import browsergym.webarenalite  # noqa: F401  (registers the tasks)
    except ImportError as e:  # pragma: no cover - environment drift guard
        print(f"sidecar: missing dependency: {e}", file=sys.stderr)
        print("sidecar: pip install browsergym-webarenalite webarena && playwright install chromium", file=sys.stderr)
        sys.exit(2)
    return gym


def normalize_task(task: str) -> str:
    """Suite YAML carries the short id ("webarenalite.0"); the gym
    registers it under "browsergym/webarenalite.0"."""
    if not task.startswith("browsergym/"):
        return "browsergym/" + task
    return task


class Sidecar:
    def __init__(self) -> None:
        self.gym = make_gym()
        self.env = None
        self.task = None

    def start(self, task: str) -> dict:
        self.close()
        task = normalize_task(task)
        env = self.gym.make(task)
        obs, _ = env.reset()
        self.env, self.task = env, task
        return {"obs": render_obs(obs), "task": task}

    def step(self, action: str) -> dict:
        if self.env is None:
            raise RuntimeError("no task started")
        obs, reward, terminated, truncated, _ = self.env.step(action)
        return {"obs": render_obs(obs), "reward": float(reward), "done": bool(terminated or truncated)}

    def close(self) -> None:
        if self.env is not None:
            try:
                self.env.close()
            except Exception:  # noqa: BLE001 - close must never break the loop
                pass
            self.env = None
            self.task = None


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=8094)
    args = ap.parse_args()
    Handler.sidecar = Sidecar()
    # Single-threaded on purpose: playwright objects are thread-affine,
    # and the Go executor drives one task at a time anyway.
    server = HTTPServer(("127.0.0.1", args.port), Handler)
    print(f"sidecar: BrowserGym webarena-lite sidecar on 127.0.0.1:{args.port}", file=sys.stderr)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        Handler.sidecar.close()


if __name__ == "__main__":
    main()
