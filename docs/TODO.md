# Roadmap / TODO

## Codebase scanner (`bvx install <repo>`)

The second install path exists in the CLI today but the scanner behind it is a
stub (`internal/codebase`). It currently:

- validates the repo path,
- explains what the scanner will do,
- runs `brevitas analyze` as an **interim preview** if `brevitas-systems` is
  installed.

### TODO — build the internal codebase scanner

1. **Build the scanner** (separate project) that statically scans a repository
   for LLM API call sites — OpenAI / Anthropic / Google SDKs **and** raw HTTP —
   and reports, per call site:
   - provider + model,
   - the env var / mechanism the API **key** is read from,
   - a recommended strategy (optimize vs lossless).
2. **Package it as a pip package** (working name `brevitas-codebase`).
3. **Wire it into `internal/codebase`**: replace `stubScanner` with an
   implementation that shells out to the pip package via the configured Python
   interpreter (the same way `internal/optimizer` locates `brevitas-systems`).
   The Go-facing `Scanner` interface and `Result`/`CallSite` types are already
   defined so the CLI won't change when the scanner lands.
4. **`--apply`**: have the scanner wire Brevitas into each call site (wrap
   clients / route base URLs) so the **brevitas token-efficiency model** sits
   between the agents and the model and reduces tokens on every provider call.

### End state

```
bvx install ai            # configure interactive AI coding tools (Claude, Codex, ...)
bvx install <repo>        # scan a codebase, find keys + call sites, wire Brevitas in
```

The scanner finds the keys and calls; the brevitas model pip goes in between to
reduce the tokens sent to any provider API.
