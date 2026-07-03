#!/usr/bin/env python3
"""Brevitas optimizer adapter.

This is the *server half* of the contract in docs/PROTOCOL.md. The Go proxy
dials a local socket and POSTs each request to /v1/optimize; this adapter uses
the brevitas-systems package (the 0.9.5 lossless token-efficiency model) as the
brain and returns the optimized request body plus token-savings numbers.

It contains NO optimization logic of its own — it only marshals request bodies
into brevitas.optimize_prompt() and back. Run it with:

    python3 brevitas_optimizer.py --unix /path/to/brevitas.sock
    python3 brevitas_optimizer.py --tcp 127.0.0.1:8765
"""

from __future__ import annotations

import argparse
import json
import os
import socket
import socketserver
import sys
from http.server import BaseHTTPRequestHandler

try:
    import brevitas  # the brevitas-systems package
except Exception as exc:  # pragma: no cover
    sys.stderr.write(f"brevitas-systems not importable: {exc}\n")
    sys.exit(3)

VERSION = getattr(brevitas, "__version__", "0.9.5")


def _optimize_text(text: str):
    """Run one string through the lossless model; return (new_text, before, after, lossy, method)."""
    if not text or not text.strip():
        return text, 0, 0, False, "noop"
    r = brevitas.optimize_prompt(text)  # default rate => lossless
    return (
        r.optimized,
        int(r.tokens_before),
        int(r.tokens_after),
        bool(r.lossy),
        str(r.method),
    )


def _optimize_message_content(content):
    """OpenAI/Anthropic 'content' may be a string or a list of parts."""
    before = after = 0
    method = "lossless"
    lossy = False
    if isinstance(content, str):
        new, b, a, ly, m = _optimize_text(content)
        return new, b, a, ly, m
    if isinstance(content, list):
        out = []
        for part in content:
            if isinstance(part, dict) and isinstance(part.get("text"), str):
                new, b, a, ly, m = _optimize_text(part["text"])
                part = {**part, "text": new}
                before += b
                after += a
                lossy = lossy or ly
                method = m
            out.append(part)
        return out, before, after, lossy, method
    return content, 0, 0, False, "noop"


def optimize_body(provider: str, body: dict):
    """Optimize the user-visible text in a provider request body.

    Returns (new_body, savings_dict). Only touches message/prompt text — model,
    params, tools, and everything else pass through untouched.
    """
    total_before = total_after = 0
    lossy = False
    method = "lossless"

    if not isinstance(body, dict):
        return body, None

    # OpenAI- and Anthropic-style: {"messages": [{"role","content"}, ...]}
    msgs = body.get("messages")
    if isinstance(msgs, list):
        new_msgs = []
        for m in msgs:
            if isinstance(m, dict) and "content" in m:
                nc, b, a, ly, mth = _optimize_message_content(m["content"])
                m = {**m, "content": nc}
                total_before += b
                total_after += a
                lossy = lossy or ly
                method = mth
            new_msgs.append(m)
        body = {**body, "messages": new_msgs}

    # Google Gemini-style: {"contents": [{"parts": [{"text": ...}]}]}
    contents = body.get("contents")
    if isinstance(contents, list):
        new_contents = []
        for c in contents:
            if isinstance(c, dict) and isinstance(c.get("parts"), list):
                nparts, b, a, ly, mth = _optimize_message_content(c["parts"])
                c = {**c, "parts": nparts}
                total_before += b
                total_after += a
                lossy = lossy or ly
                method = mth
            new_contents.append(c)
        body = {**body, "contents": new_contents}

    # Legacy completions: {"prompt": "..."}
    if isinstance(body.get("prompt"), str):
        new, b, a, ly, mth = _optimize_text(body["prompt"])
        body = {**body, "prompt": new}
        total_before += b
        total_after += a
        lossy = lossy or ly
        method = mth

    saved_pct = 0.0
    if total_before > 0:
        saved_pct = (total_before - total_after) / total_before * 100.0

    savings = {
        "tokens_before": total_before,
        "tokens_after": total_after,
        "saved_pct": round(saved_pct, 2),
        "lossy": lossy,
        "method": method,
    }
    return body, savings


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _send(self, code: int, obj):
        payload = json.dumps(obj).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):  # noqa: N802
        if self.path == "/health":
            self._send(200, {"status": "ok"})
        elif self.path == "/version":
            self._send(200, {"version": VERSION})
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):  # noqa: N802
        if self.path != "/v1/optimize":
            self._send(404, {"error": "not found"})
            return
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length) if length else b"{}"
        try:
            req = json.loads(raw or b"{}")
        except json.JSONDecodeError as exc:
            self._send(400, {"error": f"bad json: {exc}"})
            return

        provider = req.get("provider", "")
        body = req.get("body")
        try:
            new_body, savings = optimize_body(provider, body)
        except Exception as exc:  # fail open on the server side too
            self._send(200, {"body": body, "applied": [], "bypass": True, "note": str(exc)})
            return

        self._send(200, {
            "body": new_body,
            "applied": ["lossless"] if savings and savings["tokens_before"] else [],
            "bypass": False,
            "savings": savings,
        })

    def log_message(self, *args):  # silence default stderr access log
        pass


class UnixHTTPServer(socketserver.ThreadingUnixStreamServer):
    allow_reuse_address = True

    def get_request(self):
        conn, _ = super().get_request()
        return conn, ("unix", 0)  # BaseHTTPRequestHandler wants an indexable addr


class TCPHTTPServer(socketserver.ThreadingTCPServer):
    allow_reuse_address = True


def main() -> int:
    ap = argparse.ArgumentParser(description="Brevitas optimizer adapter")
    g = ap.add_mutually_exclusive_group(required=True)
    g.add_argument("--unix", help="Unix socket path to listen on")
    g.add_argument("--tcp", help="host:port to listen on")
    args = ap.parse_args()

    if args.unix:
        path = args.unix
        os.makedirs(os.path.dirname(path), exist_ok=True)
        if os.path.exists(path):
            os.unlink(path)
        server = UnixHTTPServer(path, Handler)
        os.chmod(path, 0o600)
        sys.stderr.write(f"brevitas optimizer {VERSION} listening on unix:{path}\n")
    else:
        host, _, port = args.tcp.rpartition(":")
        server = TCPHTTPServer((host or "127.0.0.1", int(port)), Handler)
        sys.stderr.write(f"brevitas optimizer {VERSION} listening on tcp:{args.tcp}\n")

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
        if args.unix and os.path.exists(args.unix):
            os.unlink(args.unix)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
