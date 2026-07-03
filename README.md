# Brevitas

**Brevitas is middleware that sits between your AI coding assistants and the
LLM provider, optimizing every request.**

```
AI Tool → Brevitas Local Proxy → brevitas-systems (optimization) → LLM Provider → Response
```

This repository is the **installer and integration framework** (written in Go).
It detects your AI tools, stores one API key securely, points every supported
tool at a local proxy, and runs that proxy as a background service.

The optimization logic lives entirely in the separate **`brevitas-systems`**
Python package. Brevitas never bundles or duplicates it — it talks to it over a
local socket (see [`docs/PROTOCOL.md`](docs/PROTOCOL.md)).

---

## Install

```sh
brew install bvx
bvx install
```

`bvx install` will:

1. Scan your system for supported AI tools
2. Ask once for your Brevitas API key
3. Store it in your OS credential store (Keychain / Credential Manager / Secret Service)
4. Rewrite each supported tool's **documented** config to use `http://127.0.0.1:8080`
5. Install and start a background service running the proxy
6. Run diagnostics and print a summary

You never edit a config file by hand, and every change is backed up.

```
Scanning system...

  ✓ Claude Code
  ✓ Codex CLI
  ✓ Continue
  ✓ Aider
  ⚠ Cursor (manual step required)
  ⚠ GitHub Copilot — Unsupported

Detected 4 configurable tool(s), 1 manual, 1 unsupported.

Enter Brevitas API key: ****************

Installing...

  ✓ API key stored in macOS Keychain
  ✓ Claude Code configured
  ✓ Codex CLI configured
  ✓ Continue configured
  ✓ Aider configured
  ✓ Background service installed
  ✓ Proxy started

Running diagnostics...

  ✓ Claude Code
  ✓ Codex CLI
  ✓ Continue
  ✓ Aider

Installation complete.
```

---

## Commands

| Command | Description |
| --- | --- |
| `bvx install` | Detect, configure, and start everything |
| `bvx uninstall [--purge]` | Restore all configs, remove the service (`--purge` also deletes the key) |
| `bvx status` | Proxy, service, key, and provider state |
| `bvx stats` | Cumulative token-savings metrics from the proxy |
| `bvx optimizer` | Run the brevitas-systems optimizer adapter (the brain) |
| `bvx providers [--detected]` | List every supported tool and its state |
| `bvx doctor` | Full diagnostics |
| `bvx repair` | Re-apply config and restart the service |
| `bvx start` / `stop` / `restart` | Control the background service |
| `bvx logs [-f]` | Print/follow proxy logs |
| `bvx config [set-port\|set-upstream\|set-python]` | View/edit config |
| `bvx login` / `logout` | Manage the stored API key |
| `bvx update [-y]` | Check/upgrade `brevitas-systems` |
| `bvx version` | Version info |

---

## Supported tools

Detection is **best effort**. Not every tool can be proxied — Brevitas never
patches binaries, injects code, installs MITM certificates, or works around
authentication. When a tool can't be redirected through documented
configuration, it is reported as **Partial** (needs a one-time manual step) or
**Unsupported**, with the reason shown.

| Tool | Support | How it's configured |
| --- | --- | --- |
| Claude Code | ✅ Full | `~/.claude/settings.json` (`ANTHROPIC_BASE_URL`) |
| Codex CLI | ✅ Full | `~/.codex/config.toml` model provider |
| Continue | ✅ Full | `~/.continue/config.json` model entry |
| Aider | ✅ Full | `~/.aider.conf.yml` (`openai-api-base`) |
| Goose | ✅ Full | `~/.config/goose/config.yaml` (`OPENAI_HOST`) |
| OpenCode | ✅ Full | `opencode.json` provider `baseURL` |
| VS Code OpenAI | ✅ Full | VS Code `settings.json` (`vscode-openai.baseUrl`) |
| Gemini CLI | ⚠️ Partial | `~/.gemini/.env` (`GOOGLE_GEMINI_BASE_URL`); new shell needed |
| Cursor | ⚠️ Partial | Base-URL override lives in encrypted app state — set in Settings |
| Cline | ⚠️ Partial | API config lives in encrypted VS Code state — set in Cline settings |
| Open WebUI | ⚠️ Partial | Configured via admin panel / `OPENAI_API_BASE_URL` |
| AnythingLLM | ⚠️ Partial | Configured via "Generic OpenAI" provider in-app |
| Jan | ⚠️ Partial | Add an OpenAI-compatible provider in-app |
| Windsurf | ❌ Unsupported | Routes through Codeium's authenticated servers; no endpoint override |
| GitHub Copilot | ❌ Unsupported | Hard-coded GitHub endpoints, server-bound tokens over protected TLS |
| LM Studio | ❌ Unsupported | A local inference *server* (upstream), not a client to redirect |
| Ollama | ❌ Unsupported | Runs models locally; makes no external provider calls |

### Why GitHub Copilot can't be proxied

Copilot is the canonical "unsupported" case, and it's unsupported *by design*:

- **Non-configurable endpoints** — the extensions hard-code
  `api.githubcopilot.com`; there is no documented base-URL setting.
- **Server-bound tokens** — the editor exchanges your GitHub OAuth token for a
  short-lived Copilot token that only GitHub's own endpoints accept, so
  forwarding the traffic elsewhere fails authentication.
- **Protected transport** — intercepting it would require a MITM root
  certificate or patching the extension. Brevitas does neither.

Brevitas therefore detects Copilot and clearly reports that direct proxying is
not available, instead of attempting a fragile or unsafe workaround.

---

## Seeing token savings

The proxy delegates optimization to the **brevitas-systems** package (the
lossless token-efficiency model) running as a local service. Wire it up:

```sh
pip install brevitas-systems     # the optimization brain
bvx optimizer               # runs the adapter that serves the socket the proxy dials
                                 # (auto-detects the Python that has brevitas)
bvx serve                   # or the background service — the proxy tools point at
```

Then send traffic through the proxy and watch savings accumulate:

```sh
bvx stats
#   Requests proxied     2
#   Requests optimized   2
#   Tokens before        31
#   Tokens after         19
#   Tokens saved         12 (38.7%)
```

Per-request savings are also logged (`bvx logs`). If the optimizer isn't
running, the proxy **fails open** — requests forward unchanged, nothing breaks,
and stats simply show 0 saved.

## How it works

### Provider registry

Every integration is its own package under
[`internal/providers/`](internal/providers) implementing a common interface:

```go
type Provider interface {
    Name() string
    DisplayName() string
    Support() Support
    Detect(ctx context.Context) bool
    Install(ctx context.Context) error
    Uninstall(ctx context.Context) error
    Validate(ctx context.Context) error
    Status(ctx context.Context) Status
}
```

The [`registry`](internal/providers/registry.go) discovers all providers and
runs detection in parallel.

### Safe configuration

Config changes go through a journaled writer
([`internal/provider/configio.go`](internal/provider/configio.go)):

- The original file (or its absence) is **backed up** before any change.
- Edits are **atomic** (temp file + rename).
- JSON files are **merged** — unrelated keys are never touched.
- Text files (TOML/YAML/dotenv) get a delimited **managed block** so user
  content and comments are preserved.
- `bvx uninstall` **restores** every file exactly.

### API key storage

One key, stored in the OS-native store — never in plaintext:

| OS | Backend |
| --- | --- |
| macOS | Keychain (`security`) |
| Windows | Credential Manager (`advapi32` Cred* APIs) |
| Linux | Secret Service (`secret-tool` / libsecret) |

### Proxy

The [`proxy`](internal/proxy) is a lightweight HTTP server that:

- Routes OpenAI-, Anthropic-, and Google-compatible requests by path/headers
- Optimizes each request via `brevitas-systems` (**fail-open** on any error)
- Forwards to the upstream with pooled connections, retries, and timeouts
- Streams responses (SSE / chunked) with immediate flushing

### Background service

| OS | Mechanism |
| --- | --- |
| macOS | launchd LaunchAgent |
| Linux | systemd `--user` unit |
| Windows | Task Scheduler logon task¹ |

¹ A raw SCM Windows Service would require the binary to implement the Service
Control Protocol (an external dependency and service-mode entrypoint). Task
Scheduler is a first-class, documented Windows mechanism that meets the same
start/stop/status/restart-on-failure requirements without binary shims.

---

## Development

```sh
make build      # build ./bin/bvx with version info
make test       # run all tests
make race       # tests with the race detector
make vet        # go vet
make cross      # build the release matrix (darwin/linux/windows)
```

The project is **pure standard library** (no third-party Go modules), so it
builds and cross-compiles offline and stays Homebrew-friendly.

### Layout

```
cmd/bvx/            # entrypoint
internal/
  cli/                   # commands (install, doctor, status, ...)
  config/                # Brevitas's own config + platform paths
  detect/                # cross-platform detection helpers
  keyring/               # OS credential stores (darwin/linux/windows)
  logging/               # slog wrapper
  optimizer/             # client for brevitas-systems + pip management
  provider/              # Provider interface, DI Env, journaled config writer
  providers/             # one package per tool + the registry
  proxy/                 # HTTP proxy: routing, optimize, forward, stream
  service/               # launchd / systemd / Task Scheduler managers
  version/               # build metadata
docs/PROTOCOL.md         # the brevitas-systems service contract
Formula/brevitas.rb      # Homebrew formula
```

---

## License

MIT — see [LICENSE](LICENSE).
