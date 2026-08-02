"""Tests for agentmap.report — build_html and render_and_open."""
from __future__ import annotations

import html as _htmlmod
import re
import tempfile
import webbrowser
from pathlib import Path

import pytest

from agentmap.agents import Agent
from agentmap.report import build_html, render_and_open
from agentmap.scanner import Finding


# ── helpers ───────────────────────────────────────────────────────────────────

def mk_finding(path="src/app.py", line=1, provider="openai",
               provider_name="OpenAI", kind="call", snippet="x = 1"):
    return Finding(path=path, line=line, provider=provider,
                   provider_name=provider_name, kind=kind, snippet=snippet)


def mk_plan(env=None, auto=None, manual=None, proxy="http://localhost:4242"):
    return {"env": env or {}, "auto": auto or [], "manual": manual or [],
            "proxy": proxy}


def build(findings=(), plan=None, agents=(), purposes=(), path="repo"):
    return build_html(path, list(findings), plan or mk_plan(),
                      list(agents), list(purposes))


HOSTILE = "<script>alert(1)</script>"
HOSTILE_QUOTES = "\"><img src=x onerror='alert(1)'>"


# ── HTML escaping of hostile inputs ───────────────────────────────────────────

@pytest.mark.parametrize("payload", [HOSTILE, HOSTILE_QUOTES],
                         ids=["script-tag", "quote-breakout"])
def test_hostile_scan_path_is_escaped(payload):
    html = build(path=payload)
    assert payload not in html
    assert _htmlmod.escape(payload) in html


@pytest.mark.parametrize("payload", [HOSTILE, HOSTILE_QUOTES],
                         ids=["script-tag", "quote-breakout"])
def test_hostile_finding_path_and_snippet_are_escaped(payload):
    f = mk_finding(path=f"evil/{payload}.py", snippet=f"call({payload})")
    html = build(findings=[f], plan=mk_plan(auto=["openai"]))
    assert payload not in html
    assert _htmlmod.escape(payload) in html


@pytest.mark.parametrize("payload", [HOSTILE, HOSTILE_QUOTES],
                         ids=["script-tag", "quote-breakout"])
def test_hostile_agent_name_and_role_are_escaped(payload):
    agent = Agent(name=f"bot{payload}", role=f"You do {payload} things.",
                  file=f"agents/{payload}.py", line=7)
    html = build(agents=[agent])
    assert payload not in html
    assert _htmlmod.escape(payload) in html


def test_hostile_hardcoded_snippet_is_escaped():
    f = mk_finding(kind="endpoint",
                   snippet=f'url = "https://api.openai.com/{HOSTILE}"')
    html = build(findings=[f], plan=mk_plan(auto=["openai"]))
    assert HOSTILE not in html
    assert _htmlmod.escape(HOSTILE) in html


# ── empty findings ────────────────────────────────────────────────────────────

def test_empty_findings_renders_empty_states():
    html = build()
    assert html.count("no AI calls found") == 2       # providers + calls-by-file
    assert "no model names detected" in html
    assert "no agents detected" in html
    assert "# no OpenAI/Anthropic-compatible calls found" in html
    # hardcoded section empty state
    assert "none — every call routes via env vars" in html


# ── unicode / emoji ───────────────────────────────────────────────────────────

def test_unicode_and_emoji_paths_survive():
    path = "src/🤖/模型ラボ.py"
    f = mk_finding(path=path, snippet="résumé = client.chat.completions.create()")
    html = build(findings=[f], plan=mk_plan(auto=["openai"]), path="проект-🚀")
    assert path in html
    assert "проект-🚀" in html
    assert "résumé" in html


# ── counts cards ──────────────────────────────────────────────────────────────

def _cards(html):
    return dict(
        (label, int(n)) for n, label in
        re.findall(r'<div class="card"><div class="n">(\d+)</div>'
                   r'<div class="l">([^<]+)</div></div>', html)
    )


def test_counts_cards_match_findings():
    findings = [
        mk_finding(path="a.py", line=1, provider="openai", kind="call"),
        mk_finding(path="a.py", line=2, provider="openai", kind="endpoint",
                   snippet='base_url="https://api.openai.com/v1"'),
        mk_finding(path="b.py", line=3, provider="anthropic",
                   provider_name="Anthropic", kind="call"),
        mk_finding(path="b.py", line=4, provider="anthropic",
                   provider_name="Anthropic", kind="import"),  # not a call site
        mk_finding(path="c.py", line=5, provider="google_gemini",
                   provider_name="Google Gemini / Vertex", kind="endpoint",
                   snippet="generativelanguage.googleapis.com"),  # manual → not hardcoded
    ]
    agents = [Agent(name="researcher", role="You research.", file="a.py", line=1)]
    cards = _cards(build(findings=findings,
                         plan=mk_plan(auto=["openai", "anthropic"],
                                      manual=["google_gemini"]),
                         agents=agents))
    assert cards["AI call sites"] == 4     # 2 openai + 1 anthropic + 1 gemini
    assert cards["providers"] == 3
    assert cards["agents"] == 1
    assert cards["hardcoded URLs"] == 1    # only the auto-routable endpoint


def test_counts_cards_all_zero_when_empty():
    cards = _cards(build())
    assert cards == {"AI call sites": 0, "providers": 0,
                     "agents": 0, "hardcoded URLs": 0}


# ── hardcoded section ─────────────────────────────────────────────────────────

def test_hardcoded_section_lists_entries():
    f = mk_finding(path="svc/client.py", line=42, kind="endpoint",
                   snippet='base_url="https://api.openai.com/v1"')
    html = build(findings=[f], plan=mk_plan(auto=["openai"]))
    assert '<span class="hard">svc/client.py:42</span>' in html
    assert _htmlmod.escape('base_url="https://api.openai.com/v1"') in html


def test_hardcoded_section_caps_at_15_but_card_counts_all():
    findings = [
        mk_finding(path=f"f{i}.py", line=i + 1, kind="endpoint",
                   snippet="https://api.openai.com/v1")
        for i in range(20)
    ]
    html = build(findings=findings, plan=mk_plan(auto=["openai"]))
    assert html.count('class="hard"') == 15
    assert _cards(html)["hardcoded URLs"] == 20


# ── env block / routing ───────────────────────────────────────────────────────

def test_env_block_contains_export_lines():
    plan = mk_plan(env={"OPENAI_BASE_URL": "http://localhost:4242/openai",
                        "ANTHROPIC_BASE_URL": "http://localhost:4242"},
                   auto=["openai", "anthropic"])
    html = build(plan=plan)
    assert "export OPENAI_BASE_URL=http://localhost:4242/openai" in html
    assert "export ANTHROPIC_BASE_URL=http://localhost:4242" in html


def test_manual_providers_note_rendered():
    html = build(plan=mk_plan(manual=["google_gemini", "cohere"]))
    assert "Manual (own SDK, edit base_url): google_gemini, cohere" in html


# ── models section ────────────────────────────────────────────────────────────

def test_models_section_renders_model_findings():
    findings = [
        mk_finding(kind="model", snippet="model='gpt-4o', temperature=0"),
        mk_finding(path="b.py", line=2, kind="model",
                   snippet='model="gpt-4o"'),
        mk_finding(path="c.py", line=3, provider="anthropic",
                   provider_name="Anthropic", kind="model",
                   snippet="model: 'claude-3-5-sonnet'"),
    ]
    html = build(findings=findings)
    assert '<code class="f">gpt-4o</code><span class="c">(2)</span>' in html
    assert "claude-3-5-sonnet" in html
    assert ">Anthropic</span>" in html
    assert ">OpenAI</span>" in html
    assert "no model names detected" not in html


# ── render_and_open ───────────────────────────────────────────────────────────

def test_render_and_open_writes_file_without_browser(tmp_path, monkeypatch):
    monkeypatch.setattr(tempfile, "gettempdir", lambda: str(tmp_path))
    monkeypatch.setattr(webbrowser, "open", lambda *a, **k: pytest.fail(
        "webbrowser.open must not be called when open_browser=False"))
    out = render_and_open("repo", [], mk_plan(), [], [], open_browser=False)
    assert isinstance(out, Path)
    assert out == tmp_path / "agentmap_report.html"
    text = out.read_text(encoding="utf-8")
    assert "<h1>agentmap</h1>" in text
    assert "no AI calls found" in text


def test_render_and_open_falls_back_to_mkstemp(tmp_path, monkeypatch):
    # write_text is monkeypatched to raise (no chmod), so this is cross-platform.
    monkeypatch.setattr(tempfile, "gettempdir", lambda: str(tmp_path))
    real_write_text = Path.write_text

    def deny_fixed_name(self, *args, **kwargs):
        if self.name == "agentmap_report.html":
            raise OSError(13, "Permission denied")
        return real_write_text(self, *args, **kwargs)

    monkeypatch.setattr(Path, "write_text", deny_fixed_name)
    out = render_and_open("repo", [], mk_plan(), [], [], open_browser=False)
    assert out.name != "agentmap_report.html"
    assert out.name.startswith("agentmap_") and out.name.endswith(".html")
    assert out.parent == tmp_path
    assert "<h1>agentmap</h1>" in out.read_text(encoding="utf-8")


def test_render_and_open_passes_file_uri_to_browser(tmp_path, monkeypatch):
    monkeypatch.setattr(tempfile, "gettempdir", lambda: str(tmp_path))
    opened = []
    monkeypatch.setattr(webbrowser, "open", lambda url: opened.append(url) or True)
    out = render_and_open("repo", [], mk_plan(), [], [], open_browser=True)
    assert opened == [out.resolve().as_uri()]
    assert opened[0].startswith("file://")
    assert opened[0].endswith("agentmap_report.html")
