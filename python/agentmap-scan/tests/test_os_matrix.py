"""Simulated OS/environment matrix for the agentmap scanner.

Everything runs on the host OS: Windows-style failures (WinError-flavoured
PermissionError/OSError from stat) are simulated via monkeypatching, and the
encoding/newline/filename cases exercise the scanner's cross-platform file
handling directly.
"""
from __future__ import annotations

import codecs
import os
from pathlib import Path

import pytest
from click.testing import CliRunner

from agentmap.cli import main as cli_main
from agentmap.scanner import scan

OPENAI_LINE = 'client = OpenAI(base_url="https://api.openai.com/v1")\n'

IS_POSIX = os.name == "posix"
IS_ROOT = IS_POSIX and hasattr(os, "geteuid") and os.geteuid() == 0


def _write_control(root: Path) -> Path:
    ctl = root / "app.py"
    ctl.write_text(OPENAI_LINE, encoding="utf-8")
    return ctl


def _patch_stat_raising(monkeypatch, needle: str, exc_factory):
    """Make os.stat (and therefore Path.stat) raise for paths containing needle."""
    real_stat = os.stat

    def fake_stat(path, *args, **kwargs):
        if needle in str(path):
            raise exc_factory()
        return real_stat(path, *args, **kwargs)

    monkeypatch.setattr(os, "stat", fake_stat)


# ── Windows-style crash simulation ────────────────────────────────────────────

def test_pnpm_access_denied_stat_survives(tmp_path, monkeypatch):
    """PermissionError(13, 'Access is denied') from stat on .pnpm paths (the
    classic Windows MAX_PATH / locked-cache failure) must not abort the scan."""
    _write_control(tmp_path)
    # ".pnpm" is NOT in _SKIP_DIRS (only ".pnpm-store" is), so the walk enters it.
    dep = tmp_path / "wild_cache" / ".pnpm" / "dep"
    dep.mkdir(parents=True)
    (dep / "index.js").write_text('fetch("https://api.openai.com/v1/x")\n', encoding="utf-8")

    _patch_stat_raising(monkeypatch, ".pnpm",
                        lambda: PermissionError(13, "Access is denied"))

    findings = scan(tmp_path)  # must not raise
    assert any(f.path.endswith("app.py") for f in findings), "control file missed"
    assert not any(".pnpm" in f.path for f in findings), "denied path was scanned"


def test_oserror_with_winerror_attributes_survives(tmp_path, monkeypatch):
    """OSError carrying Windows-style .winerror/.strerror attributes is still an
    OSError to the scanner's except clauses and must be swallowed."""
    _write_control(tmp_path)
    bad = tmp_path / "win_locked"
    bad.mkdir()
    (bad / "mod.py").write_text(OPENAI_LINE, encoding="utf-8")

    def make_winerror():
        e = OSError(22, "The filename, directory name, or volume label syntax is incorrect")
        e.winerror = 123  # ERROR_INVALID_NAME, as raised on Windows builds
        return e

    _patch_stat_raising(monkeypatch, "win_locked", make_winerror)

    findings = scan(tmp_path)  # must not raise
    assert any(f.path.endswith("app.py") for f in findings)
    assert not any("win_locked" in f.path for f in findings)


# ── File encodings ────────────────────────────────────────────────────────────

def test_utf8_bom_file_is_scanned(tmp_path):
    p = tmp_path / "bom.py"
    p.write_bytes(codecs.BOM_UTF8 + b"resp = client.chat.completions.create(model='gpt-4o')\n")
    findings = scan(tmp_path)
    assert [(f.line, f.provider) for f in findings if f.kind == "call"] == [(1, "openai")]


def test_utf16_le_file_skipped_without_crash(tmp_path):
    p = tmp_path / "wide.py"
    p.write_bytes(codecs.BOM_UTF16_LE
                  + 'x = "https://api.openai.com/v1"\n'.encode("utf-16-le"))
    findings = scan(tmp_path)  # strict-utf8 decode fails on the BOM → file skipped
    assert findings == []


def test_latin1_file_skipped_without_crash(tmp_path):
    p = tmp_path / "legacy.py"
    p.write_bytes("# café résumé: https://api.openai.com/v1\n".encode("latin-1"))
    findings = scan(tmp_path)
    assert findings == []


def test_crlf_line_numbers(tmp_path):
    p = tmp_path / "dos.py"
    p.write_bytes(b"import os\r\n"
                  b"x = 1\r\n"
                  b'r = post("https://api.anthropic.com/v1/messages")\r\n')
    findings = scan(tmp_path)
    assert len(findings) == 1
    assert (findings[0].provider, findings[0].line) == ("anthropic", 3)
    assert "\r" not in findings[0].snippet


def test_mixed_lf_crlf_line_numbers(tmp_path):
    p = tmp_path / "mixed.py"
    p.write_bytes(b"a = 1\n"
                  b"b = 2\r\n"
                  b"c = 3\n"
                  b'# hit https://api.groq.com/openai/v1\r\n')
    findings = scan(tmp_path)
    assert [(f.provider, f.line) for f in findings] == [("groq", 4)]


def test_binary_nul_bytes_with_py_extension_skipped(tmp_path):
    p = tmp_path / "fake.py"
    # NUL bytes plus invalid-UTF-8 bytes; endpoint text embedded so a naive
    # lossy decode would produce a false positive.
    p.write_bytes(b"\x00\x00\xff\xfe\x89PNG\r\n\x1a\n\x00api.openai.com\x00\xff")
    findings = scan(tmp_path)  # UnicodeDecodeError path — no crash
    assert findings == []


# ── Pathological content ──────────────────────────────────────────────────────

def test_very_long_single_line_snippet_capped(tmp_path):
    p = tmp_path / "minifiedish.js"
    p.write_text('var u="https://api.openai.com/v1";' + "x" * 100_000 + "\n",
                 encoding="utf-8")
    findings = scan(tmp_path)
    assert len(findings) == 1
    assert findings[0].line == 1
    assert len(findings[0].snippet) == 160


# ── Filenames ─────────────────────────────────────────────────────────────────

@pytest.mark.parametrize("name", [
    "my app.py",        # space
    "モデル呼び出し.py",   # unicode
    ".secretconfig",    # leading dot, no real suffix
])
def test_unusual_filenames_are_scanned(tmp_path, name):
    (tmp_path / name).write_text(OPENAI_LINE, encoding="utf-8")
    findings = scan(tmp_path)
    assert len(findings) == 1
    assert findings[0].provider == "openai"


@pytest.mark.parametrize("name", ["PIC.PNG", "notes.MD", "Doc.RST"])
def test_skip_ext_matching_is_case_insensitive(tmp_path, name):
    (tmp_path / name).write_text(OPENAI_LINE, encoding="utf-8")
    assert scan(tmp_path) == []


# ── POSIX-only permission denial (real chmod, not simulated) ──────────────────

@pytest.mark.skipif(not IS_POSIX, reason="chmod(0) denial is POSIX-only")
@pytest.mark.skipif(IS_ROOT, reason="root ignores file permission bits")
def test_unreadable_file_skipped_scan_survives(tmp_path):
    _write_control(tmp_path)
    locked = tmp_path / "locked.py"
    locked.write_text(OPENAI_LINE, encoding="utf-8")
    locked.chmod(0o000)
    try:
        findings = scan(tmp_path)
    finally:
        locked.chmod(0o644)  # let tmp_path cleanup succeed
    assert any(f.path.endswith("app.py") for f in findings)
    assert not any(f.path.endswith("locked.py") for f in findings)


# ── install: env file newlines ────────────────────────────────────────────────

def test_install_env_file_uses_lf_newlines(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()
    _write_control(repo)
    env_file = tmp_path / "routing.env"

    result = CliRunner().invoke(
        cli_main, ["install", str(repo), "--env-file", str(env_file)])
    assert result.exit_code == 0, result.output

    raw = env_file.read_bytes()
    assert b"\r" not in raw, "env file must use \\n newlines on every OS"
    assert raw.count(b"\n") >= 2  # header + at least one export line
    text = raw.decode("utf-8")
    assert "export OPENAI_BASE_URL=http://localhost:4242/openai\n" in text
