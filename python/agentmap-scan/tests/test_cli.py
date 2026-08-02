"""CLI behavior tests for agentmap.cli via click.testing.CliRunner.

All filesystem work happens under pytest tmp dirs; webbrowser.open and
tempfile.gettempdir are monkeypatched so no browser launches and the HTML
report lands in a per-test directory.
"""
from __future__ import annotations

import os
import stat
import sys
import tempfile
import webbrowser
from pathlib import Path

import pytest
from click.testing import CliRunner

from agentmap import cli
from agentmap.cli import main

IS_POSIX = os.name == "posix"
IS_ROOT = IS_POSIX and hasattr(os, "geteuid") and os.geteuid() == 0

OPENAI_CALL = 'resp = client.chat.completions.create(model="gpt-4o", messages=msgs)\n'
ANTHROPIC_CALL = 'msg = client.messages.create(model="claude-sonnet-4", max_tokens=64)\n'
HARDCODED_OPENAI = 'client = OpenAI(base_url="https://api.openai.com/v1", api_key=key)\n'
NO_AI = 'def add(a, b):\n    return a + b\n'


@pytest.fixture(autouse=True)
def sandbox(monkeypatch, tmp_path_factory):
    """Safety net: never open a real browser, never write to the real temp dir."""
    opened: list[str] = []
    monkeypatch.setattr(webbrowser, "open", lambda url, *a, **k: opened.append(url) or True)
    report_dir = tmp_path_factory.mktemp("report_tmp")
    monkeypatch.setattr(tempfile, "gettempdir", lambda: str(report_dir))
    return opened


def combined_output(result) -> str:
    out = result.output
    try:
        out += result.stderr
    except (ValueError, AttributeError):
        pass  # stderr not captured separately on this click version
    return out


def make_project(tmp_path: Path, source: str, name: str = "app.py") -> Path:
    proj = tmp_path / "proj"
    proj.mkdir()
    (proj / name).write_text(source, encoding="utf-8")
    return proj


# ── scan ──────────────────────────────────────────────────────────────────────

@pytest.mark.parametrize(
    "source,provider",
    [(OPENAI_CALL, "openai"), (ANTHROPIC_CALL, "anthropic")],
    ids=["openai", "anthropic"],
)
def test_scan_planted_call_prints_count_and_report(tmp_path, source, provider):
    proj = make_project(tmp_path, source)
    result = CliRunner().invoke(main, ["scan", str(proj), "--no-open"])
    assert result.exit_code == 0, combined_output(result)
    out = combined_output(result)
    assert "1 AI call sites" in out
    assert provider in out
    assert "(routed)" in out
    assert "Report:" in out
    assert "agentmap_report.html" in out


def test_scan_reported_file_exists_and_no_browser(tmp_path, sandbox):
    proj = make_project(tmp_path, OPENAI_CALL)
    result = CliRunner().invoke(main, ["scan", str(proj), "--no-open"])
    assert result.exit_code == 0
    report = Path(tempfile.gettempdir()) / "agentmap_report.html"
    assert report.is_file()
    body = report.read_text(encoding="utf-8")
    assert "agentmap" in body
    assert "openai" in body.lower()
    assert sandbox == []  # --no-open really means no browser


def test_scan_empty_dir_exits_zero_with_zero_sites(tmp_path):
    empty = tmp_path / "empty"
    empty.mkdir()
    result = CliRunner().invoke(main, ["scan", str(empty), "--no-open"])
    assert result.exit_code == 0, combined_output(result)
    assert "0 AI call sites" in combined_output(result)


def test_scan_nonexistent_path_is_clean_click_error(tmp_path):
    missing = tmp_path / "does-not-exist"
    result = CliRunner().invoke(main, ["scan", str(missing), "--no-open"])
    out = combined_output(result)
    assert result.exit_code != 0
    assert "path does not exist" in out
    assert "Traceback" not in out  # ClickException, not a crash


def test_default_group_path_arg_falls_through_to_scan(tmp_path):
    proj = make_project(tmp_path, OPENAI_CALL)
    result = CliRunner().invoke(main, [str(proj), "--no-open"])
    assert result.exit_code == 0, combined_output(result)
    assert "1 AI call sites" in combined_output(result)


def test_bare_invocation_scans_cwd_and_opens_browser(tmp_path, monkeypatch, sandbox):
    proj = make_project(tmp_path, OPENAI_CALL)
    monkeypatch.chdir(proj)
    result = CliRunner().invoke(main, [])
    assert result.exit_code == 0, combined_output(result)
    assert "1 AI call sites" in combined_output(result)
    assert len(sandbox) == 1  # default is --open; our stub caught it
    assert sandbox[0].startswith("file:")


def test_scan_single_file_path(tmp_path):
    f = tmp_path / "solo.py"
    f.write_text(OPENAI_CALL, encoding="utf-8")
    result = CliRunner().invoke(main, ["scan", str(f), "--no-open"])
    assert result.exit_code == 0, combined_output(result)
    assert "1 AI call sites" in combined_output(result)


# ── install ───────────────────────────────────────────────────────────────────

def test_install_writes_env_file_with_base_urls(tmp_path):
    proj = make_project(tmp_path, OPENAI_CALL + ANTHROPIC_CALL)
    env_file = tmp_path / "routing.env"
    result = CliRunner().invoke(main, ["install", str(proj), "--env-file", str(env_file)])
    assert result.exit_code == 0, combined_output(result)
    body = env_file.read_text(encoding="utf-8")
    assert "export OPENAI_BASE_URL=http://localhost:4242/openai" in body
    assert "export ANTHROPIC_BASE_URL=http://localhost:4242" in body
    assert f"Wrote {env_file}" in combined_output(result)


def test_install_nothing_to_route(tmp_path):
    proj = make_project(tmp_path, NO_AI)
    env_file = tmp_path / "routing.env"
    result = CliRunner().invoke(main, ["install", str(proj), "--env-file", str(env_file)])
    assert result.exit_code == 0, combined_output(result)
    assert "Nothing to route" in combined_output(result)
    assert not env_file.exists()


def test_install_env_file_in_nonexistent_dir_is_clean_error(tmp_path):
    proj = make_project(tmp_path, OPENAI_CALL)
    env_file = tmp_path / "no_such_dir" / "deeper" / ".env"
    result = CliRunner().invoke(main, ["install", str(proj), "--env-file", str(env_file)])
    out = combined_output(result)
    assert result.exit_code != 0
    assert "cannot write" in out
    assert "Traceback" not in out


@pytest.mark.skipif(not IS_POSIX, reason="chmod-based write denial is POSIX-only")
@pytest.mark.skipif(IS_ROOT, reason="root ignores file permission bits")
def test_install_env_file_in_readonly_dir_is_clean_error(tmp_path):
    proj = make_project(tmp_path, OPENAI_CALL)
    ro = tmp_path / "readonly"
    ro.mkdir()
    ro.chmod(stat.S_IRUSR | stat.S_IXUSR)
    try:
        result = CliRunner().invoke(main, ["install", str(proj), "--env-file", str(ro / ".env")])
        out = combined_output(result)
        assert result.exit_code != 0
        assert "cannot write" in out
        assert "Traceback" not in out
    finally:
        ro.chmod(stat.S_IRWXU)  # let pytest clean tmp_path


def test_install_without_auto_flags_hardcoded_urls_untouched(tmp_path):
    proj = make_project(tmp_path, HARDCODED_OPENAI, name="hard.py")
    env_file = tmp_path / "routing.env"
    result = CliRunner().invoke(main, ["install", str(proj), "--env-file", str(env_file)])
    assert result.exit_code == 0, combined_output(result)
    assert "1 hardcoded URL(s)" in combined_output(result)
    assert "api.openai.com" in (proj / "hard.py").read_text(encoding="utf-8")


def test_install_auto_rewrites_hardcoded_url_on_disk(tmp_path):
    proj = make_project(tmp_path, HARDCODED_OPENAI, name="hard.py")
    env_file = tmp_path / "routing.env"
    result = CliRunner().invoke(
        main, ["install", str(proj), "--auto", "--env-file", str(env_file)]
    )
    assert result.exit_code == 0, combined_output(result)
    assert "Rewrote 1 hardcoded URL(s)" in combined_output(result)
    body = (proj / "hard.py").read_text(encoding="utf-8")
    assert "api.openai.com" not in body
    assert "http://localhost:4242/openai" in body


# ── _echo unicode degradation ─────────────────────────────────────────────────

def _ascii_only_echo(seen: list):
    def fake_echo(msg=""):
        s = str(msg)
        for i, ch in enumerate(s):
            if ord(ch) > 127:
                raise UnicodeEncodeError("charmap", s, i, i + 1, "maps to <undefined>")
        seen.append(s)
    return fake_echo


def test_echo_degrades_to_ascii(monkeypatch):
    seen: list[str] = []
    monkeypatch.setattr(cli, "_click_echo", _ascii_only_echo(seen))
    cli._echo("✓ Report: résultat")  # must not raise
    assert seen == ["? Report: r?sultat"]


def test_scan_survives_legacy_ascii_console(tmp_path, monkeypatch):
    seen: list[str] = []
    monkeypatch.setattr(cli, "_click_echo", _ascii_only_echo(seen))
    proj = make_project(tmp_path, OPENAI_CALL)
    result = CliRunner().invoke(
        main, ["scan", str(proj), "--no-open"], catch_exceptions=False
    )
    assert result.exit_code == 0
    joined = "\n".join(seen)
    assert "1 AI call sites" in joined
    assert "? Report:" in joined  # the checkmark degraded, output still landed
