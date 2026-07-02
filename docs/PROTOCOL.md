# brevitas-systems service protocol

The Brevitas Go proxy does **not** run optimization logic. It delegates to a
long-running `brevitas-systems` process over a local transport:

- **Unix socket** (macOS/Linux) — default, path `~/…/brevitas.sock`
- **Loopback TCP** (Windows) — default, `127.0.0.1:8765`

This avoids launching a Python interpreter per request (which would add
~100 ms+ of cold-start latency to every completion). The persistent process
keeps per-request overhead in the low-millisecond range.

The transport is configured in Brevitas's `config.json`:

```json
"optimizer": {
  "transport": "unix",
  "address": "/Users/you/Library/Application Support/Brevitas/brevitas.sock",
  "python_bin": "python3",
  "start_timeout": 20000000000,
  "call_timeout": 60000000000
}
```

## Endpoints

`brevitas-systems` must expose a minimal HTTP/1.1 server on the transport with
three endpoints.

### `GET /health`

Returns `200 OK` when ready. Any body is ignored.

### `GET /version`

```json
{ "version": "1.4.2" }
```

### `POST /v1/optimize`

Request (sent by the proxy):

```json
{
  "provider": "openai",              // "openai" | "anthropic" | "google"
  "model": "gpt-4o",
  "stream": false,
  "path": "/v1/chat/completions",
  "headers": { "Content-Type": "application/json" },
  "body": { "model": "gpt-4o", "messages": [ ... ] }
}
```

Response:

```json
{
  "body": { "model": "gpt-4o", "messages": [ ... ] },  // optimized request body to forward
  "headers": { "x-brevitas-optimized": "1" },           // optional header overrides
  "applied": ["prompt-compression", "context-pruning"], // for logging only
  "bypass": false                                        // if true, forward the original unchanged
}
```

## Failure behavior (fail-open)

If `brevitas-systems` is unreachable, times out, or returns a non-200 status,
the proxy logs a warning and **forwards the original, unmodified request**.
Optimization is best-effort; it must never break a user's coding session.

## What the proxy owns vs. what brevitas-systems owns

| Concern                        | Owner            |
| ------------------------------ | ---------------- |
| Provider detection & config    | Go installer     |
| API key storage                | Go installer     |
| HTTP proxy, streaming, retries | Go proxy         |
| Prompt/request optimization    | brevitas-systems |
| Upstream provider selection    | brevitas-systems |
