"""HTTP decision service for the laya container.

POST /predict  {"state": {...}, "questions": {...}} -> {"answers": {...}}
GET  /healthz  -> {"ok": true}

Loads the English checkpoint once at startup (weights are baked into the
image), so first request needs no network. Stdlib only — the service adds
zero Python dependencies beyond laya itself.
"""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import laya

AGENT = laya.load("convaiinnovations/laya")


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
            self._send(200, {"ok": True})
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/predict":
            self._send(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            req = json.loads(self.rfile.read(length) or b"{}")
            state = req.get("state") or {}
            questions = req.get("questions") or {}
            if not state or not questions:
                self._send(400, {"error": "state and questions are required"})
                return
            result = AGENT.predict(state, questions)
            self._send(200, {"answers": result["answers"]})
        except Exception as exc:  # noqa: BLE001 - service boundary
            self._send(500, {"error": str(exc)})

    def log_message(self, fmt, *args):  # quiet; stdout stays clean for logs
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
