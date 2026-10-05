#!/usr/bin/env python3
"""BrowserGym sidecar for the browsergym-miniwob executor (uiauto-framework).

Owns the gym environment (browser + programmatic reward); speaks a tiny
JSON contract to the Go harness:

  GET  /healthz              -> 200 ok
  POST /start  {"task": id}  -> {"obs": str, "task": id}
  POST /step   {"action": s} -> {"obs": str, "reward": float, "done": bool}
  POST /close  {}            -> 200

The agent loop lives in the Go executor; this process never calls a
model. Task ids are BrowserGym names ("miniwob.click-button"). Actions
are BrowserGym bid strings ("click('e12')"). Rewards come from the task
itself — no judge.

Requires: pip install "browsergym[miniwob]" && playwright install chromium
Run:      python3 eval/miniwob/sidecar.py [--port 8093]
"""

from __future__ import annotations

import argparse
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

MAX_OBS_CHARS = 8000  # bound the observation before it crosses the wire


def make_env_factory():
    try:
        import gymnasium as gym
        import browsergym.miniwob  # noqa: F401  (registers the tasks)
    except ImportError as e:  # pragma: no cover - environment drift guard
        print(f"sidecar: missing dependency: {e}", file=sys.stderr)
        print('sidecar: pip install "browsergym[miniwob]" && playwright install chromium', file=sys.stderr)
        sys.exit(2)
    return gym


class Sidecar:
    def __init__(self) -> None:
        self.gym = make_env_factory()
        self.env = None
        self.task = None

    def start(self, task: str) -> dict:
        self.close()
        # Suite YAML carries the short id ("miniwob.click-button"); the
        # gym registers it under "browsergym/miniwob.click-button".
        if not task.startswith("browsergym/"):
            task = "browsergym/" + task
        env = self.gym.make(task)
        obs, _ = env.reset()
        self.env, self.task = env, task
        return {"obs": render_obs(obs), "task": task}

    def step(self, action: str) -> dict:
        if self.env is None:
            raise RuntimeError("no task started")
        obs, reward, terminated, truncated, _ = self.env.step(action)
        done = bool(terminated or truncated)
        if done:
            # The episode is over; keep the page until /close so a late
            # observation is still readable, then reset on next /start.
            pass
        return {"obs": render_obs(obs), "reward": float(reward), "done": done}

    def close(self) -> None:
        if self.env is not None:
            try:
                self.env.close()
            except Exception:  # noqa: BLE001 - close must never break the loop
                pass
            self.env = None
            self.task = None


def _flatten_axtree(obj) -> str:
    """Minimal flattener for the CDP accessibility tree BrowserGym
    returns (browsergym 0.14.x ships no flatten_axtree_to_str): the
    node list carries `browsergym_id` (the bid the action space refers
    to), parentId and childIds. Emits readable, indented
    `[role] name (bid)` lines for visible nodes, bounded."""
    nodes = obj.get("nodes", []) if isinstance(obj, dict) else obj
    if not isinstance(nodes, list):
        return json.dumps(obj, ensure_ascii=False)
    by_id = {}
    for n in nodes:
        if isinstance(n, dict) and n.get("nodeId") is not None:
            by_id[n["nodeId"]] = n
    children: dict = {}
    for n in nodes:
        if isinstance(n, dict):
            children.setdefault(n.get("parentId"), []).append(n)
    lines: list[str] = []

    def emit(node: dict, depth: int) -> None:
        role = (node.get("role") or {}).get("value", "") or "node"
        name = (node.get("name") or {}).get("value", "") or ""
        bid = node.get("browsergym_id", "")
        value = node.get("value")
        if isinstance(value, dict):
            value = value.get("value", "")
        # An ignored node renders nothing but its VISIBLE children still
        # count — pruning at an ignored intermediate would hide leaves.
        if not node.get("ignored"):
            label = f"[{role}] {name!r}".replace("''", "")
            if value:
                label += f" value={value!r}"
            if bid:
                label += f" ({bid})"
            lines.append("  " * depth + label)
            depth += 1
        for child in children.get(node.get("nodeId"), []):
            emit(child, depth)

    for root in children.get(None, []):
        emit(root, 0)
    text = "\n".join(lines)
    if len(text) > MAX_OBS_CHARS:
        text = text[:MAX_OBS_CHARS] + "\n...[truncated]"
    return text


def render_obs(obs) -> str:
    """BrowserGym observations are dicts. The goal comes from chat/user
    message or `goal`; the axtree is flattened to bid-carrying lines.
    Bounded to MAX_OBS_CHARS on both paths."""
    if isinstance(obs, str):
        return obs[:MAX_OBS_CHARS]
    if not isinstance(obs, dict):
        return str(obs)[:MAX_OBS_CHARS]
    goal = obs.get("goal") or ""
    if not goal and isinstance(obs.get("chat_messages"), (list, tuple)):
        for m in obs["chat_messages"]:
            if isinstance(m, dict) and m.get("role") == "user" and m.get("message"):
                goal = str(m["message"])
                break
    tree_obj = obs.get("axtree_object")
    if tree_obj is not None:
        tree = _flatten_axtree(tree_obj)
    else:
        tree = obs.get("axtree") or ""
        if not isinstance(tree, str):
            tree = json.dumps(tree, ensure_ascii=False)
    text = f"GOAL:\n{goal}\n\nAXTREE:\n{tree}"
    if len(text) > MAX_OBS_CHARS:
        text = text[:MAX_OBS_CHARS] + "\n...[truncated]"
    return text


class Handler(BaseHTTPRequestHandler):
    sidecar: Sidecar = None  # set in main

    def _reply(self, code: int, payload: dict | None = None) -> None:
        body = json.dumps(payload or {}).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):  # noqa: N802 - http.server API
        if self.path == "/healthz":
            self._reply(200, {"ok": True})
        else:
            self._reply(404, {"error": "unknown route"})

    def do_POST(self):  # noqa: N802 - http.server API
        try:
            length = int(self.headers.get("Content-Length") or 0)
            req = json.loads(self.rfile.read(length) or b"{}")
        except (ValueError, json.JSONDecodeError):
            self._reply(400, {"error": "body must be JSON"})
            return
        try:
            if self.path == "/start":
                task = str(req.get("task") or "")
                if not task:
                    self._reply(400, {"error": "task is required"})
                    return
                self._reply(200, self.sidecar.start(task))
            elif self.path == "/step":
                action = str(req.get("action") or "")
                if not action:
                    self._reply(400, {"error": "action is required"})
                    return
                self._reply(200, self.sidecar.step(action))
            elif self.path == "/close":
                self.sidecar.close()
                self._reply(200, {"ok": True})
            else:
                self._reply(404, {"error": "unknown route"})
        except Exception as e:  # noqa: BLE001 - one bad task never kills the sidecar
            self._reply(400, {"error": f"{type(e).__name__}: {e}"})

    def log_message(self, fmt, *args):  # quiet: stderr stays parseable
        print(f"sidecar: {fmt % args}", file=sys.stderr)


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=8093)
    args = ap.parse_args()
    Handler.sidecar = Sidecar()
    # Single-threaded on purpose: playwright objects are thread-affine,
    # and the Go executor drives one task at a time anyway.
    server = HTTPServer(("127.0.0.1", args.port), Handler)
    print(f"sidecar: BrowserGym miniwob sidecar on 127.0.0.1:{args.port}", file=sys.stderr)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        Handler.sidecar.close()


if __name__ == "__main__":
    main()
