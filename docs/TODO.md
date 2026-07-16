# Roadmap / TODO

## Codebase scanner (`bvx install <repo>`) — integrated ✅

`bvx install <repo>` now shells out to the **`agentmap-scan`** package
(`agentmap`), which maps every AI API call in a codebase offline (no keys, no
LLM) and can route those calls through a gateway:

```
bvx install repo                   # choose a codebase with the directory navigator
bvx install <repo>                 # scan + open the AI-call map (agentmap scan)
bvx install <repo> --apply         # also route calls through Brevitas (agentmap install)
bvx install <repo> --apply --auto  # also rewrite hardcoded provider URLs in place
```

`--apply` writes `.env.agentmap` with `OPENAI_BASE_URL=<proxy>/openai`,
`ANTHROPIC_BASE_URL=<proxy>`, etc. The proxy rewrites the `/openai/*`,
`/anthropic/*`, `/google/*` namespaces to the providers' real paths and
forwards with the codebase's own key, optimizing tokens in between.

### Remaining TODO

- **Hardcoded URLs**: `agentmap install` reports call sites with hardcoded
  provider URLs that env vars can't override; `--auto` rewrites them. Surface a
  clearer summary of what `--auto` changed.
- **Google routing**: verify `/google/*` namespace matches what agentmap emits
  for Gemini once a Google call site is available to test against.
- **Per-agent tracking**: agentmap reports agents; wire its report into
  `bvx stats` so per-agent token savings show up after routing.
- **Version pinning**: `agentmap-scan==0.1.0` — bump and re-test as it evolves.
