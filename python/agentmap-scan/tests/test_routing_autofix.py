"""Tests for agentmap.scanner: routing(), hardcoded_sites(), apply_autofix()."""
from __future__ import annotations

import os

import pytest

from agentmap.scanner import (
    Finding,
    apply_autofix,
    hardcoded_sites,
    routing,
    scan,
)
from agentmap.signatures import CALL, ENDPOINT, MODEL, PROVIDERS_BY_ID

DEFAULT_PROXY = "http://localhost:4242"

IS_POSIX = os.name == "posix"
IS_ROOT = IS_POSIX and hasattr(os, "geteuid") and os.geteuid() == 0


def F(provider: str, kind: str = ENDPOINT, path: str = "x.py", line: int = 1) -> Finding:
    """Build a synthetic Finding for a registered provider."""
    return Finding(
        path=path,
        line=line,
        provider=provider,
        provider_name=PROVIDERS_BY_ID[provider].name,
        kind=kind,
        snippet="",
    )


# ── routing() ─────────────────────────────────────────────────────────────────

def test_routing_anthropic_env_is_proxy_exactly():
    plan = routing([F("anthropic")])
    assert plan["env"] == {"ANTHROPIC_BASE_URL": DEFAULT_PROXY}
    assert plan["auto"] == ["anthropic"]
    assert plan["manual"] == []


def test_routing_openai_env_has_openai_suffix():
    plan = routing([F("openai")])
    assert plan["env"] == {"OPENAI_BASE_URL": f"{DEFAULT_PROXY}/openai"}
    assert plan["auto"] == ["openai"]
    assert plan["manual"] == []


@pytest.mark.parametrize("pid", ["deepseek", "groq", "mistral", "together"])
def test_routing_openai_compatible_rides_openai_base_url(pid):
    assert PROVIDERS_BY_ID[pid].openai_compatible  # precondition on registry
    plan = routing([F(pid)])
    assert plan["env"] == {"OPENAI_BASE_URL": f"{DEFAULT_PROXY}/openai"}
    assert plan["auto"] == [pid]
    assert plan["manual"] == []


@pytest.mark.parametrize("pid", ["google_gemini", "cohere", "replicate", "langchain"])
def test_routing_non_compatible_lands_in_manual_with_no_env(pid):
    assert not PROVIDERS_BY_ID[pid].openai_compatible  # precondition on registry
    plan = routing([F(pid)])
    assert plan["env"] == {}
    assert plan["auto"] == []
    assert plan["manual"] == [pid]


def test_routing_custom_proxy_propagates():
    proxy = "https://gateway.example.internal:9099"
    plan = routing([F("anthropic"), F("openai"), F("cohere")], proxy=proxy)
    assert plan["proxy"] == proxy
    assert plan["env"]["ANTHROPIC_BASE_URL"] == proxy
    assert plan["env"]["OPENAI_BASE_URL"] == f"{proxy}/openai"


def test_routing_empty_findings_gives_empty_plan():
    plan = routing([])
    assert plan == {"env": {}, "auto": [], "manual": [], "proxy": DEFAULT_PROXY}


# ── hardcoded_sites() ─────────────────────────────────────────────────────────

def test_hardcoded_sites_only_endpoint_findings_of_auto_routable_providers():
    findings = [
        F("openai", ENDPOINT),        # auto-routable endpoint -> kept
        F("anthropic", ENDPOINT),     # auto-routable endpoint -> kept
        F("deepseek", ENDPOINT),      # openai-compatible endpoint -> kept
        F("openai", CALL),            # call site, not a hardcoded URL -> dropped
        F("anthropic", MODEL),        # model literal -> dropped
        F("google_gemini", ENDPOINT), # not auto-routable -> dropped
        F("cohere", ENDPOINT),        # not auto-routable -> dropped
    ]
    kept = hardcoded_sites(findings)
    assert [(f.provider, f.kind) for f in kept] == [
        ("openai", ENDPOINT),
        ("anthropic", ENDPOINT),
        ("deepseek", ENDPOINT),
    ]


# ── apply_autofix() ───────────────────────────────────────────────────────────

def _write_hardcoded_openai(tmp_path, name="hard.py"):
    p = tmp_path / name
    p.write_text(
        'client = OpenAI(base_url="https://api.openai.com/v1", api_key=key)\n',
        encoding="utf-8",
    )
    return p


def test_autofix_rewrites_hardcoded_openai_url_in_place(tmp_path):
    p = _write_hardcoded_openai(tmp_path)
    findings = scan(tmp_path)
    edits = apply_autofix(findings)
    body = p.read_text(encoding="utf-8")
    assert "api.openai.com" not in body
    assert f'base_url="{DEFAULT_PROXY}/openai"' in body
    assert edits == [(str(p), 1, body.strip())]


def test_autofix_never_rewrites_md_files(tmp_path):
    md = tmp_path / "docs.md"
    original = "Point your client at https://api.openai.com/v1 to get started.\n"
    md.write_text(original, encoding="utf-8")
    # Scan the .md directly as a single file (directory walks skip .md entirely),
    # so we get real ENDPOINT findings that autofix must still refuse to touch.
    findings = scan(md)
    assert any(f.kind == ENDPOINT and f.provider == "openai" for f in findings)
    edits = apply_autofix(findings)
    assert edits == []
    assert md.read_text(encoding="utf-8") == original


def test_autofix_twice_is_noop_second_time(tmp_path):
    p = _write_hardcoded_openai(tmp_path)
    findings = scan(tmp_path)
    first = apply_autofix(findings)
    assert first
    after_first = p.read_bytes()
    second = apply_autofix(findings)  # same stale findings, already-fixed file
    assert second == []
    assert p.read_bytes() == after_first


def test_autofix_skips_file_deleted_after_scan(tmp_path):
    p = _write_hardcoded_openai(tmp_path)
    findings = scan(tmp_path)
    assert findings
    p.unlink()
    edits = apply_autofix(findings)  # must not crash
    assert edits == []


@pytest.mark.skipif(not IS_POSIX, reason="chmod-based write denial is POSIX-only")
@pytest.mark.skipif(IS_ROOT, reason="root ignores file permission bits")
def test_autofix_skips_readonly_file(tmp_path):
    p = _write_hardcoded_openai(tmp_path)
    original = p.read_bytes()
    findings = scan(tmp_path)
    p.chmod(0o444)
    try:
        edits = apply_autofix(findings)  # write fails -> skip, no crash
        assert edits == []
        assert p.read_bytes() == original
    finally:
        p.chmod(0o644)


def test_autofix_skips_finding_line_beyond_eof(tmp_path):
    p = tmp_path / "svc.py"
    p.write_text(
        "import os\n"
        "import httpx\n"
        'BASE = "https://api.openai.com/v1"\n',
        encoding="utf-8",
    )
    findings = scan(tmp_path)
    assert any(f.line == 3 for f in findings)
    truncated = "import os\n"
    p.write_text(truncated, encoding="utf-8")  # file shrank after the scan
    edits = apply_autofix(findings)
    assert edits == []
    assert p.read_text(encoding="utf-8") == truncated


def test_autofix_changes_only_flagged_line(tmp_path):
    p = tmp_path / "app.py"
    lines = [
        "import os\n",
        "# talks to the completions backend\n",
        'BASE_URL = "https://api.openai.com/v1"\n',
        "TIMEOUT = 30\n",
        "def make_client():\n",
        "    return build(BASE_URL, TIMEOUT)\n",
    ]
    p.write_text("".join(lines), encoding="utf-8")
    findings = scan(tmp_path)
    edits = apply_autofix(findings)
    assert [(path, line) for path, line, _ in edits] == [(str(p), 3)]
    new_lines = p.read_text(encoding="utf-8").splitlines(keepends=True)
    assert len(new_lines) == len(lines)
    for i, (old, new) in enumerate(zip(lines, new_lines)):
        if i == 2:
            assert new == f'BASE_URL = "{DEFAULT_PROXY}/openai"\n'
        else:
            assert new == old  # byte-identical untouched lines
