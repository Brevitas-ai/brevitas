# agentmap

Map every AI API call in a codebase — **offline, no LLM, no API keys**. Run one
command and get a webpage showing which files call which providers, what models
you use, and what each agent is responsible for.

```bash
pip install agentmap-scan
cd your-project
agentmap
```

A browser page opens with four sections:

- **Calls by file** — every AI API call site, `file:line`, provider-tagged.
- **Providers & models** — the distinct providers and model names in use, with counts.
- **Agents & responsibilities** — each agent's name and what its system prompt says it does.
- **Routing** — the env vars to send those calls through a gateway.

## How it works

Pure regex over your source — it matches provider **endpoint hosts**
(`api.openai.com`, `api.anthropic.com`, …) and **SDK call signatures**
(`.chat.completions.create`, `.messages.create`, …), so it finds calls in
Python, JS/TS, Go, Rust, Ruby, PHP, Java, shell/curl — not just the official
SDKs. Agent responsibilities come straight from the system prompts in your code,
trimmed to one line. Nothing leaves your machine.

Detects 20 providers: OpenAI, Anthropic, Google Gemini/Vertex, Azure OpenAI,
DeepSeek, Groq, xAI, Mistral, Cohere, Bedrock, Together, Fireworks, OpenRouter,
Perplexity, Replicate, Hugging Face, Ollama, LiteLLM, LangChain.

## Commands

```bash
agentmap [PATH]              # scan + open the report (default: current dir)
agentmap --no-open PATH      # scan, write the HTML, don't launch a browser
agentmap install PATH        # write .env.agentmap routing env vars
agentmap install PATH --auto # also rewrite hardcoded provider base_urls in place
```

## Routing → save money

`agentmap install` writes the env vars that point your OpenAI/Anthropic-compatible
calls at a gateway. Point them at [**Brevitas**](https://github.com/brevitas-ai)
to compress context losslessly and cut your token bill:

```bash
agentmap install . --target http://localhost:4242
source .env.agentmap
```

## Limitations (honest)

Agent detection is a heuristic. It reliably catches raw OpenAI/Anthropic message
dicts, `system=`/`system_prompt=`/`instructions=` assignments (incl. `X_system`,
`X_PROMPT` names), CrewAI `Agent(role=…)`, and `.agent("name")` calls. It does
**not** yet understand graph frameworks (LangGraph node graphs, AutoGen). PRs
adding per-framework matchers welcome.

## License

MIT.
