#!/usr/bin/env python3
"""Fake Buildkite Agent API for `buildkite-agent cache` e2e tests.

Serves the cache registry endpoints (registry lookup, peek, store, commit,
retrieve, expire, confirm) backed by an in-memory entry list with store
"local_file", plus GET /jobs/<id>/secrets for `buildkite-agent secret get`.

Usage: fake_registry.py <port-file> <request-log>
Binds an ephemeral port on 127.0.0.1 and writes it to <port-file>.
GET /control/entries returns the committed entries as JSON.
"""
import json
import re
import sys
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

ENTRIES = []  # committed entries: {target_paths, cache_key, blobs}
PENDING = {}  # upload_id -> entry awaiting commit
SECRETS = {"MY_SECRET": "s3cr3t-from-secret-get-XYZ"}
LOG = open(sys.argv[2], "a", buffering=1)


def addr(target_paths, key):
    return (tuple(target_paths), tuple(p["value"] for p in key))


def find(target_paths, key):
    a = addr(target_paths, key)
    for e in reversed(ENTRIES):
        if addr(e["target_paths"], e["cache_key"]) == a:
            return e
    return None


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def send(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        u = urlparse(self.path)
        LOG.write(f"GET {u.path}\n")
        if u.path == "/control/entries":
            return self.send(200, ENTRIES)
        if re.fullmatch(r"/v3/jobs/[^/]+/secrets", u.path):
            key = parse_qs(u.query)["key"][0]
            if key in SECRETS:
                return self.send(200, {"key": key, "value": SECRETS[key], "uuid": "u"})
            return self.send(404, {"message": "Secret not found"})
        m = re.fullmatch(r"/v3/cache_registries/([^/]+)", u.path)
        if m:
            return self.send(200, {"uuid": "r", "name": m.group(1), "store": "local_file"})
        self.send(404, {"message": "no route"})

    def do_PUT(self):
        self.do_POST()

    def do_POST(self):
        u = urlparse(self.path)
        n = int(self.headers.get("Content-Length") or 0)
        b = json.loads(self.rfile.read(n) or b"{}")
        m = re.fullmatch(r"/v3/cache_registries/([^/]+)/(\w+)", u.path)
        if not m:
            return self.send(404, {"message": "no route"})
        op = m.group(2)
        key = [f"{p['value'][:16]}{'' if p['mandatory'] else '?'}" for p in b.get("cache_key", [])]
        LOG.write(f"{op} target_paths={b.get('target_paths')} key={key}\n")

        if op == "peek":
            e = find(b["target_paths"], b["cache_key"])
            if e:
                return self.send(200, dict(e, store="local_file"))
            return self.send(404, {"message": "Cache entry not found"})
        if op == "store":
            upload_id = str(uuid.uuid4())
            PENDING[upload_id] = {k: b[k] for k in ("target_paths", "cache_key", "blobs")}
            return self.send(200, {"upload_id": upload_id, "multipart": False,
                                   "upload_instructions": [], "retention_days": 7})
        if op == "commit":
            e = PENDING.pop(b["upload_id"])
            a = addr(e["target_paths"], e["cache_key"])
            ENTRIES[:] = [x for x in ENTRIES if addr(x["target_paths"], x["cache_key"]) != a]
            ENTRIES.append(e)
            return self.send(200, {"message": "ok"})
        if op == "retrieve":
            tp, req_key = b["target_paths"], b["cache_key"]
            e = find(tp, req_key)
            if e:
                return self.send(200, dict(e, store="local_file", fallback=False, retention_days=7))
            # Fallback: same target_paths and key length, mandatory parts equal.
            for e in reversed(ENTRIES):
                if tuple(e["target_paths"]) != tuple(tp) or len(e["cache_key"]) != len(req_key):
                    continue
                if all(s["value"] == r["value"] for s, r in zip(e["cache_key"], req_key) if r["mandatory"]):
                    return self.send(200, dict(e, store="local_file", fallback=True, retention_days=7))
            return self.send(404, {"message": "Cache entry not found"})
        if op == "expire":
            a = addr(b["target_paths"], b["cache_key"])
            before = len(ENTRIES)
            ENTRIES[:] = [x for x in ENTRIES if addr(x["target_paths"], x["cache_key"]) != a]
            return self.send(200, {"message": "ok", "existed": len(ENTRIES) != before})
        if op == "confirm":
            return self.send(200, {"message": "ok"})
        self.send(404, {"message": "no route"})


server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
with open(sys.argv[1], "w") as f:
    f.write(str(server.server_address[1]))
server.serve_forever()
