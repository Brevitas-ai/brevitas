"""Filesystem hazard scenarios for agentmap.scanner.scan / _walk.

All fixtures are built under tmp_path; no network, no timing dependence, and no
reliance on the repo's own findings. POSIX-only scenarios (chmod, symlinks,
geteuid) are guarded so the suite also passes on Windows.
"""
from __future__ import annotations

import os
import stat
import sys

import pytest

from agentmap.scanner import _MAX_BYTES, scan

# A line every scan below should detect (OpenAI endpoint, HIGH confidence).
AI_LINE = 'resp = requests.post("https://api.openai.com/v1/chat/completions")\n'

IS_POSIX = os.name == "posix"
IS_ROOT = hasattr(os, "geteuid") and os.geteuid() == 0

posix_only = pytest.mark.skipif(not IS_POSIX, reason="chmod/symlink semantics are POSIX-only")
not_root = pytest.mark.skipif(IS_ROOT, reason="root ignores file permission bits")
symlink_only = pytest.mark.skipif(
    os.name == "nt", reason="symlinks need elevated privileges on Windows"
)


def _paths(findings):
    return {f.path for f in findings}


# ── unreadable file / directory ───────────────────────────────────────────────

@posix_only
@not_root
def test_unreadable_file_is_skipped_not_fatal(tmp_path):
    good = tmp_path / "good.py"
    good.write_text(AI_LINE)
    bad = tmp_path / "bad.py"
    bad.write_text(AI_LINE)
    bad.chmod(0)
    try:
        findings = scan(tmp_path)
    finally:
        bad.chmod(stat.S_IRUSR | stat.S_IWUSR)
    assert str(good) in _paths(findings)
    assert str(bad) not in _paths(findings)


@posix_only
@not_root
def test_unreadable_directory_is_skipped_not_fatal(tmp_path):
    good = tmp_path / "good.py"
    good.write_text(AI_LINE)
    locked = tmp_path / "locked"
    locked.mkdir()
    (locked / "hidden.py").write_text(AI_LINE)
    locked.chmod(0)
    try:
        findings = scan(tmp_path)  # os.walk onerror swallows the PermissionError
    finally:
        locked.chmod(stat.S_IRWXU)
    assert str(good) in _paths(findings)
    assert not any("hidden.py" in p for p in _paths(findings))


# ── symlinks ──────────────────────────────────────────────────────────────────

@symlink_only
def test_broken_symlink_is_skipped(tmp_path):
    good = tmp_path / "good.py"
    good.write_text(AI_LINE)
    (tmp_path / "dangling.py").symlink_to(tmp_path / "does-not-exist.py")
    findings = scan(tmp_path)
    assert _paths(findings) == {str(good)}


@symlink_only
def test_symlink_loop_does_not_hang_or_crash(tmp_path):
    sub = tmp_path / "sub"
    sub.mkdir()
    good = sub / "good.py"
    good.write_text(AI_LINE)
    (sub / "loop").symlink_to(tmp_path)  # cycle back to the root
    (tmp_path / "self").symlink_to(tmp_path)  # self-referencing dir link
    findings = scan(tmp_path)  # followlinks=False → finite walk
    assert str(good) in _paths(findings)
    # the real file is found exactly once, never re-yielded through the loop
    assert sum(1 for f in findings if f.path == str(good)) == 1


# ── size limit ────────────────────────────────────────────────────────────────

def test_file_over_max_bytes_is_skipped(tmp_path):
    big = tmp_path / "big.py"
    padding = "#" * (_MAX_BYTES - len(AI_LINE))  # total = _MAX_BYTES + 1
    big.write_text(AI_LINE + padding + "\n")
    assert big.stat().st_size == _MAX_BYTES + 1
    assert scan(tmp_path) == []


def test_file_exactly_at_max_bytes_is_scanned(tmp_path):
    edge = tmp_path / "edge.py"
    padding = "#" * (_MAX_BYTES - len(AI_LINE) - 1)
    edge.write_text(AI_LINE + padding + "\n")
    assert edge.stat().st_size == _MAX_BYTES  # st_size > _MAX_BYTES is False
    findings = scan(tmp_path)
    assert [f.provider for f in findings] == ["openai"]
    assert findings[0].line == 1


# ── directory shapes ──────────────────────────────────────────────────────────

def test_deeply_nested_dirs_are_scanned(tmp_path):
    deep = tmp_path
    for i in range(50):
        deep = deep / f"d{i}"
    deep.mkdir(parents=True)
    target = deep / "leaf.py"
    target.write_text(AI_LINE)
    findings = scan(tmp_path)
    assert _paths(findings) == {str(target)}


def test_empty_dir_returns_empty_list(tmp_path):
    assert scan(tmp_path) == []


def test_scan_root_that_is_a_single_file(tmp_path):
    f = tmp_path / "only.py"
    f.write_text(AI_LINE)
    findings = scan(f)
    assert _paths(findings) == {str(f)}
    assert findings[0].provider == "openai"


def test_missing_root_raises_file_not_found(tmp_path):
    with pytest.raises(FileNotFoundError):
        scan(tmp_path / "no-such-dir")


# ── extension filtering ───────────────────────────────────────────────────────

def test_extensionless_file_with_ai_call_is_found(tmp_path):
    f = tmp_path / "Procfile"  # no suffix → never hits _SKIP_EXT
    f.write_text("web: curl https://api.anthropic.com/v1/messages\n")
    findings = scan(tmp_path)
    assert str(f) in _paths(findings)
    assert findings[0].provider == "anthropic"


@pytest.mark.parametrize(
    "name",
    [
        "bundle.min.js",   # explicit .min.js check
        "vendor.js.map",   # .map
        "README.md",       # prose docs
        "poetry.lock",     # .lock
        "logo.svg",        # binary-ish asset ext
    ],
)
def test_skip_extensions_are_never_scanned(tmp_path, name):
    (tmp_path / name).write_text(AI_LINE)
    (tmp_path / "keep.py").write_text(AI_LINE)
    findings = scan(tmp_path)
    assert _paths(findings) == {str(tmp_path / "keep.py")}


def test_skip_dirs_are_pruned(tmp_path):
    vendored = tmp_path / "node_modules" / "dep"
    vendored.mkdir(parents=True)
    (vendored / "index.js").write_text(AI_LINE)
    (tmp_path / "app.py").write_text(AI_LINE)
    findings = scan(tmp_path)
    assert _paths(findings) == {str(tmp_path / "app.py")}


# ── stat-time PermissionError (Windows WinError 5 on pnpm long paths) ─────────

def test_stat_permission_error_is_skipped_scan_continues(tmp_path, monkeypatch):
    """Simulate os.stat raising WinError 5 (access denied / long path) for one
    file mid-walk: that file is skipped, the rest of the scan still returns."""
    good = tmp_path / "good.py"
    good.write_text(AI_LINE)
    evil = tmp_path / "evil.py"
    evil.write_text(AI_LINE)

    real_stat = os.stat

    def fake_stat(path, *args, **kwargs):
        if os.path.basename(os.fspath(path)) == "evil.py":
            raise PermissionError(13, "Access is denied", os.fspath(path))
        return real_stat(path, *args, **kwargs)

    monkeypatch.setattr(os, "stat", fake_stat)
    findings = scan(tmp_path)
    assert str(good) in _paths(findings)
    assert str(evil) not in _paths(findings)


def test_is_file_permission_error_is_skipped(tmp_path, monkeypatch):
    """Path.is_file itself raising PermissionError must be caught by _walk."""
    from pathlib import Path

    good = tmp_path / "good.py"
    good.write_text(AI_LINE)
    evil = tmp_path / "evil.py"
    evil.write_text(AI_LINE)

    real_is_file = Path.is_file

    def fake_is_file(self, *args, **kwargs):
        if self.name == "evil.py":
            raise PermissionError(13, "Access is denied", str(self))
        return real_is_file(self, *args, **kwargs)

    monkeypatch.setattr(Path, "is_file", fake_is_file)
    findings = scan(tmp_path)
    assert str(good) in _paths(findings)
    assert str(evil) not in _paths(findings)
