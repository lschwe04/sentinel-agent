#!/usr/bin/env python3
"""Small local SentinelCore demo hub; standard library only."""

import gzip
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    server_version = "SentinelCoreMockHub/1.0"

    def log_message(self, format, *args):
        print("[hub] " + format % args, flush=True)

    def send_json(self, status, payload):
        data = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)

        if self.path == "/api/v1/agent/enroll":
            self.send_json(200, {
                "agent_id": "demo-agent-001",
                "mTLS_shared_secret": "demo-only-secret",
                "status": "enrolled",
            })
            return

        if self.path != "/api/v1/metrics":
            self.send_json(404, {"error": "not found"})
            return

        if self.headers.get("Content-Encoding") != "gzip":
            self.send_json(415, {"error": "expected gzip"})
            return

        try:
            payload = json.loads(gzip.decompress(body))
        except (OSError, ValueError, json.JSONDecodeError) as exc:
            self.send_json(400, {"error": str(exc)})
            return

        print(f"[hub] received {len(payload)} metric(s): {json.dumps(payload)}", flush=True)
        self.send_json(202, {"accepted": len(payload)})

    def do_GET(self):
        if self.path == "/healthz":
            self.send_json(200, {"status": "ok"})
            return
        self.send_json(404, {"error": "not found"})


if __name__ == "__main__":
    server = ThreadingHTTPServer(("127.0.0.1", 8080), Handler)
    print("SentinelCore mock hub listening on http://127.0.0.1:8080", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.shutdown()
        server.server_close()
