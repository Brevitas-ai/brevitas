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


# Prefer the task-aware compression router: it classifies the prompt (code /
# creative / summarize / ...), picks a per-task keep-rate, protects code blocks,
# and uses LLMLingua-2 for real token reduction when the optional extra is
# installed (pip install "brevitas-systems[promptopt]"). Without that extra it
# transparently falls back to lossless (whitespace only) — which is why savings
# are small until the extra is installed.
try:
    _router = brevitas.TaskCompressionRouter(protect_code=True)
except Exception:
    _router = None


# ── Lossless engine (levers 2 + b9 + retrieval) ───────────────────────────────
# The real optimization path used by the production Python proxy: one call to
# optimize_request() applies provider-native cache_control (lever 2), multi-agent
# shared-prefix promotion (b9), and retrieval — all lossless and guarded. We
# delegate to it for chat-shaped bodies ({"messages": [...]}); other formats
# (Gemini contents / Responses input / legacy prompt) fall back to text
# compression below. Import is guarded so an older brevitas-systems still runs.
try:
    from token_efficiency_model.lossless.engine import optimize_request as _optimize_request
    from token_efficiency_model.lossless.router import BrevitasRouter as _BrevitasRouter
    from token_efficiency_model.lossless.provider_cache import count_tokens as _count_tokens
except Exception:  # pragma: no cover
    _optimize_request = None
    _BrevitasRouter = None
    _count_tokens = None

# One router per (key_id, provider) — learns each session's repeat + cache behavior.
# Process-lifetime state (the server runs serve_forever), mirroring proxy.py.
_routers: dict = {}

# Lossless engine is on by default; BREVITAS_LOSSLESS=0 falls back to text compression.
_LOSSLESS_ON = os.environ.get("BREVITAS_LOSSLESS", "1") not in ("0", "false", "no")
# Lossy LLMLingua text compression as an EXTRA pass (prod doesn't use it). Off by default.
_TEXT_COMPRESS_ON = os.environ.get("BREVITAS_TEXT_COMPRESS", "0") not in ("0", "false", "no")


def _router_for(key_id: str, provider: str):
    k = f"{key_id}:{provider}"
    r = _routers.get(k)
    if r is None:
        r = _BrevitasRouter(provider=provider)
        _routers[k] = r
    return r


def _labels_from_headers(headers) -> tuple:
    """(pipeline, agent, run_id) from x-brevitas-* headers (case-insensitive)."""
    if not isinstance(headers, dict):
        return "", "", ""
    low = {str(k).lower(): v for k, v in headers.items()}
    return (low.get("x-brevitas-pipeline", "") or "",
            low.get("x-brevitas-agent", "") or "",
            low.get("x-brevitas-run-id", "") or "")


def _messages_tokens(body: dict) -> int:
    if _count_tokens is None or not isinstance(body, dict):
        return 0
    total = 0
    for m in body.get("messages", []) or []:
        c = m.get("content", "")
        if isinstance(c, str):
            total += _count_tokens(c)
        elif isinstance(c, list):
            for b in c:
                if isinstance(b, dict) and isinstance(b.get("text"), str):
                    total += _count_tokens(b["text"])
    sysv = body.get("system")
    if isinstance(sysv, str):
        total += _count_tokens(sysv)
    return total


# ── Response cache (lever 1, exact-hash) ──────────────────────────────────────
# Skip the upstream call on a byte-identical repeat. Exact-hash only on the bvx
# path (semantic_enabled=False) → zero wrong-answer risk. Namespaced per key_id
# so a cached answer is never served across tenants. Any failure disables it
# silently — the cache must NEVER break a request.
try:
    from brevitas.semantic_cache import SemanticCache as _SemanticCache
except Exception:  # pragma: no cover
    _SemanticCache = None

_CACHE_ON = os.environ.get("BREVITAS_CACHE_ENABLED", "true").lower() != "false"
_caches: dict = {}


def _cache_for(key_id: str):
    if _SemanticCache is None or not _CACHE_ON:
        return None
    c = _caches.get(key_id)
    if c is None:
        db = os.environ.get("BREVITAS_CACHE_DB") or None
        try:
            c = _SemanticCache(db_path=db, namespace=key_id, semantic_enabled=False)
        except TypeError:
            # Older brevitas-systems without namespace/semantic_enabled: fall back to
            # exact-hash-only via an unreachable similarity threshold (cosine <= 1.0).
            c = _SemanticCache(db_path=db, similarity_threshold=1.01)
        except Exception:
            c = None
        _caches[key_id] = c
    return c


def _usage_from_response(provider: str, response: dict) -> tuple:
    """(prompt_tokens, completion_tokens) from a provider response usage object."""
    u = (response or {}).get("usage", {}) or {}
    if provider == "anthropic":
        p = int(u.get("input_tokens", 0)) + int(u.get("cache_read_input_tokens", 0)) \
            + int(u.get("cache_creation_input_tokens", 0))
        return p, int(u.get("output_tokens", 0))
    return int(u.get("prompt_tokens", 0)), int(u.get("completion_tokens", 0))


def cache_lookup(provider: str, model: str, body: dict, key_id: str):
    """Return the stored provider response for a byte-identical repeat, else None."""
    cache = _cache_for(key_id)
    if cache is None or not isinstance(body, dict):
        return None
    try:
        hit = cache.lookup(body, provider, model)  # cacheable() gate is inside
    except Exception:
        return None
    if hit is None:
        return None
    return {"response": hit.response, "kind": getattr(hit, "kind", "exact")}


def cache_store(provider: str, model: str, body: dict, response: dict, key_id: str) -> None:
    cache = _cache_for(key_id)
    if cache is None or not isinstance(body, dict) or not isinstance(response, dict):
        return
    try:
        p, c = _usage_from_response(provider, response)
        cache.store(body, provider, model, response, prompt_tokens=p, completion_tokens=c)
    except Exception:
        pass  # cache store is best-effort; never fail the record path


def optimize_request_body(provider: str, body: dict, key_id: str, headers):
    """Delegate a chat-shaped body to the lossless engine (levers 2 + b9 + retrieval).

    Returns (new_body, savings_dict). Mutates a copy in place via optimize_request.
    Falls back to (None, None) when the engine is unavailable or the body has no
    messages, so the caller can use text compression instead."""
    if _optimize_request is None or _BrevitasRouter is None:
        return None, None
    if not isinstance(body, dict) or not body.get("messages"):
        return None, None
    pipeline, agent, _run = _labels_from_headers(headers)
    session_id = f"{key_id or 'local'}:{agent or pipeline or 'default'}"
    router = _router_for(key_id or "local", provider)
    before = _messages_tokens(body)
    meta = _optimize_request(body, provider, router, session_id,
                             pipeline=pipeline, agent=agent)
    after = _messages_tokens(body)
    strategy = (meta or {}).get("strategy", "cache_only")
    applied = [strategy]
    if (meta or {}).get("cache_breakpoints"):
        applied.append("native_cache")
    saved_pct = (before - after) / before * 100.0 if before > 0 else 0.0
    savings = {
        "tokens_before": before,
        "tokens_after": after,
        "saved_pct": round(saved_pct, 2),
        "lossy": False,
        "method": strategy,
    }
    return body, {"savings": savings, "applied": applied}


def _optimize_text(text: str):
    """Compress one string; return (new_text, before, after, lossy, method)."""
    if not text or not text.strip():
        return text, 0, 0, False, "noop"

    opt = None
    if _router is not None:
        try:
            opt = _router.route(text).optimization  # auto-classifies the task
        except Exception:
            opt = None
    if opt is None:
        opt = brevitas.optimize_prompt(text)  # lossless fallback

    return (
        opt.optimized,
        int(opt.tokens_before),
        int(opt.tokens_after),
        bool(opt.lossy),
        str(opt.method),
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

    # OpenAI Responses API: {"input": "..." | [{role, content:[{type,text}]}]}
    inp = body.get("input")
    if isinstance(inp, str):
        new, b, a, ly, mth = _optimize_text(inp)
        body = {**body, "input": new}
        total_before += b
        total_after += a
        lossy = lossy or ly
        method = mth
    elif isinstance(inp, list):
        new_input = []
        for item in inp:
            if isinstance(item, dict) and isinstance(item.get("content"), list):
                nc, b, a, ly, mth = _optimize_message_content(item["content"])
                item = {**item, "content": nc}
                total_before += b
                total_after += a
                lossy = lossy or ly
                method = mth
            new_input.append(item)
        body = {**body, "input": new_input}

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

    def _read_json(self):
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length) if length else b"{}"
        return json.loads(raw or b"{}")

    def do_POST(self):  # noqa: N802
        if self.path not in ("/v1/optimize", "/v1/record"):
            self._send(404, {"error": "not found"})
            return
        try:
            req = self._read_json()
        except json.JSONDecodeError as exc:
            self._send(400, {"error": f"bad json: {exc}"})
            return

        if self.path == "/v1/record":
            # Populate the response cache (+ later, usage reporting) from a
            # completed exchange. Best-effort — always ack.
            try:
                cache_store(req.get("provider", ""), req.get("model", ""),
                            req.get("body"), req.get("response"), req.get("key_id", ""))
            except Exception:
                pass
            self._send(200, {"ok": True})
            return

        provider = req.get("provider", "")
        body = req.get("body")
        key_id = req.get("key_id", "")
        headers = req.get("headers", {})

        # Lever 1: exact-hash response-cache lookup BEFORE any optimization. On a
        # hit the Go proxy replays cached_response and skips the upstream call.
        try:
            hit = cache_lookup(provider, req.get("model", ""), body, key_id)
        except Exception:
            hit = None
        if hit is not None:
            self._send(200, {"cache_hit": True, "cached_response": hit["response"],
                             "cache_kind": hit["kind"], "bypass": False})
            return

        try:
            # Preferred path: the lossless engine (native cache_control + b9 +
            # retrieval) for chat-shaped bodies. Returns None for other formats.
            applied = []
            engine_savings = None
            if _LOSSLESS_ON:
                new_body, eng = optimize_request_body(provider, body, key_id, headers)
                if new_body is not None:
                    body = new_body
                    applied = eng["applied"]
                    engine_savings = eng["savings"]

            # Text compression: the sole path for non-chat formats (Gemini/Responses/
            # legacy), or an optional extra pass when explicitly enabled.
            savings = engine_savings
            if engine_savings is None or _TEXT_COMPRESS_ON:
                new_body, tsav = optimize_body(provider, body)
                body = new_body
                if tsav and tsav["tokens_before"]:
                    if "lossless" not in applied:
                        applied.append("lossless")
                    # keep the larger reported token reduction
                    if savings is None or tsav["tokens_before"] >= savings["tokens_before"]:
                        savings = tsav
        except Exception as exc:  # fail open on the server side too
            self._send(200, {"body": req.get("body"), "applied": [], "bypass": True, "note": str(exc)})
            return

        self._send(200, {
            "body": body,
            "applied": applied,
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
