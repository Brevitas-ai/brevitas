"""
Minimalist static HTML report: calls-by-file, providers & models, agents &
responsibilities, and the routing plan. No backend, no JS — one self-contained file.
"""
from __future__ import annotations

import html as _html
import os
import tempfile
import webbrowser
from collections import Counter
from pathlib import Path

from .scanner import call_sites, hardcoded_sites, providers_found
from .signatures import MODEL, PROVIDERS_BY_ID

_CSS = """
* { box-sizing: border-box; }
body { margin:0; font:14px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace;
       background:#0b0f14; color:#d7dee8; }
.wrap { max-width:1000px; margin:0 auto; padding:32px 20px 64px; }
h1 { font-size:20px; margin:0 0 4px; color:#fff; }
.sub { color:#7c8794; margin:0 0 24px; }
.cards { display:grid; grid-template-columns:repeat(auto-fit,minmax(150px,1fr)); gap:12px; margin:0 0 28px; }
.card { background:#131a23; border:1px solid #1f2a37; border-radius:10px; padding:16px; }
.card .n { font-size:26px; font-weight:700; color:#fff; }
.card .l { color:#7c8794; font-size:12px; text-transform:uppercase; letter-spacing:.04em; }
h2 { font-size:13px; text-transform:uppercase; letter-spacing:.05em; color:#7c8794;
     border-bottom:1px solid #1f2a37; padding-bottom:8px; margin:32px 0 14px; }
.row { display:flex; align-items:center; gap:10px; margin:6px 0; }
.row .name { width:150px; color:#d7dee8; }
.bar { height:16px; background:#2563eb; border-radius:3px; min-width:2px; }
.bar.manual { background:#f59e0b; }
.row .c { color:#7c8794; width:70px; }
.tag { font-size:11px; padding:1px 7px; border-radius:99px; }
.tag.auto { background:#14351f; color:#4ade80; }
.tag.manual { background:#3a2a10; color:#f59e0b; }
pre { background:#0f151d; border:1px solid #1f2a37; border-radius:8px; padding:12px 14px;
      overflow-x:auto; color:#9fd0ff; }
code.f { color:#7c8794; }
.file { margin:10px 0 14px; }
.fp { color:#fff; font-weight:600; margin-bottom:2px; word-break:break-all; }
.agent { margin:8px 0; }
.agent .an { color:#fff; font-weight:600; }
.agent .ar { color:#9fb3c8; }
.agent .loc { color:#556; font-size:12px; }
.hard { color:#f87171; }
.empty { color:#7c8794; font-style:italic; }
.foot { margin-top:40px; color:#7c8794; font-size:13px; border-top:1px solid #1f2a37; padding-top:16px; }
a { color:#60a5fa; }
"""


def _esc(s) -> str:
    return _html.escape(str(s))


def _group_by_file(calls):
    """[(path, [(line, provider, snippet), …]), …] ordered by call count desc."""
    from collections import defaultdict
    d = defaultdict(list)
    for f in calls:
        d[f.path].append((f.line, f.provider, f.snippet))
    for v in d.values():
        v.sort()
    return sorted(d.items(), key=lambda kv: len(kv[1]), reverse=True)


def _models(findings):
    """[(provider_name, model, count), …] from MODEL-kind findings, most common first."""
    c = Counter((PROVIDERS_BY_ID[f.provider].name, f.snippet_model) for f in _model_findings(findings))
    return [(prov, model, n) for (prov, model), n in c.most_common()]


def _model_findings(findings):
    """Attach the matched model literal to each MODEL finding."""
    import re
    for f in findings:
        if f.kind != MODEL:
            continue
        spec = PROVIDERS_BY_ID[f.provider]
        pat = next((p for p in spec.patterns if p.kind == MODEL), None)
        if not pat:
            continue
        m = pat.regex.search(f.snippet)
        f.snippet_model = m.group(0) if m else "?"
        yield f


def build_html(path, findings, plan, agents, purposes) -> str:
    calls = call_sites(findings)
    counts = Counter(f.provider for f in calls)
    hard = hardcoded_sites(findings)
    mx = max(counts.values()) if counts else 1

    prov_rows = "".join(
        f'<div class="row"><span class="name">{_esc(pid)}</span>'
        f'<span class="bar {"" if pid in plan["auto"] else "manual"}" style="width:{int(n / mx * 300)}px"></span>'
        f'<span class="c">{n}</span>'
        f'<span class="tag {"auto" if pid in plan["auto"] else "manual"}">{"routed" if pid in plan["auto"] else "manual"}</span></div>'
        for pid, n in counts.most_common()) or '<div class="empty">no AI calls found</div>'

    model_rows = "".join(
        f'<div class="row"><span class="name">{_esc(prov)}</span>'
        f'<code class="f">{_esc(model)}</code><span class="c">({n})</span></div>'
        for prov, model, n in _models(findings)) or '<div class="empty">no model names detected</div>'

    files_html = "".join(
        f'<div class="file"><div class="fp">{_esc(fpath)}</div>' + "".join(
            f'<div class="row"><span class="c" style="width:52px">L{ln}</span>'
            f'<span class="tag {"auto" if pid in plan["auto"] else "manual"}">{_esc(pid)}</span> '
            f'<code class="f">{_esc(snip[:100])}</code></div>'
            for ln, pid, snip in items) + '</div>'
        for fpath, items in _group_by_file(calls)) or '<div class="empty">no AI calls found</div>'

    agents_html = "".join(
        f'<div class="agent"><span class="an">{_esc(a.name)}</span> — '
        f'<span class="ar">{_esc(a.role)}</span> '
        f'<span class="loc">{_esc(a.file)}:{a.line}</span></div>'
        for a in agents) or '<div class="empty">no agents detected (no system prompts found)</div>'

    purpose_html = "".join(
        f'<div class="row"><span class="name" style="width:220px">{_esc(Path(f).name)}</span>'
        f'<span class="ar">{_esc(role)}</span></div>'
        for f, role in purposes) or '<div class="empty">—</div>'

    env_block = "\n".join(f"export {k}={v}" for k, v in plan["env"].items()) \
        or "# no OpenAI/Anthropic-compatible calls found"
    manual = f'<p class="sub">Manual (own SDK, edit base_url): {_esc(", ".join(plan["manual"]))}</p>' if plan["manual"] else ""
    hard_html = ("".join(
        f'<div class="row"><span class="hard">{_esc(h.path)}:{h.line}</span> '
        f'<code class="f">{_esc(h.snippet[:90])}</code></div>' for h in hard[:15])
        or '<div class="empty">none — every call routes via env vars</div>')

    return f"""<!doctype html><html><head><meta charset="utf-8">
<title>agentmap — {_esc(path)}</title><style>{_CSS}</style></head><body><div class="wrap">
<h1>agentmap</h1><p class="sub">AI API calls in <code>{_esc(path)}</code></p>
<div class="cards">
  <div class="card"><div class="n">{sum(counts.values())}</div><div class="l">AI call sites</div></div>
  <div class="card"><div class="n">{len(counts)}</div><div class="l">providers</div></div>
  <div class="card"><div class="n">{len(agents)}</div><div class="l">agents</div></div>
  <div class="card"><div class="n">{len(hard)}</div><div class="l">hardcoded URLs</div></div>
</div>

<h2>Calls by file</h2>{files_html}

<h2>Providers &amp; models</h2>{prov_rows}
<div style="height:10px"></div>{model_rows}

<h2>Agents &amp; responsibilities</h2>{agents_html}
<div style="height:14px"></div>
<h2>Purpose per file</h2>{purpose_html}

<h2>Routing — set these to send calls through your gateway</h2>
{manual}<pre>{_esc(env_block)}</pre>
<h2>Hardcoded URLs (need --auto or a manual edit)</h2>{hard_html}

<div class="foot">Generated by <a href="https://github.com/">agentmap</a> ·
route these calls through <b>Brevitas</b> to compress context and cut token cost.</div>
</div></body></html>"""


def render_and_open(path, findings, plan, agents, purposes, open_browser=True) -> Path:
    doc = build_html(str(path), findings, plan, agents, purposes)
    out = Path(tempfile.gettempdir()) / "agentmap_report.html"
    try:
        out.write_text(doc, encoding="utf-8")
    except OSError:
        # fixed name unwritable (e.g. owned by another user on a shared box)
        fd, name = tempfile.mkstemp(prefix="agentmap_", suffix=".html")
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            fh.write(doc)
        out = Path(name)
    if open_browser:
        webbrowser.open(out.resolve().as_uri())  # as_uri handles Windows drive paths
    return out
