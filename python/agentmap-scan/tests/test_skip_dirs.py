"""Tests for skip-dir pruning in agentmap.scanner._walk / scan.

Covers: every entry in scanner._SKIP_DIRS is pruned; the original user-crash
layout (.nx/cache/.../node_modules/.pnpm/...) is never scanned nor even
stat-ed; a *file* named like a skip dir is still scanned; nested
skip-in-skip; skip dirs at repo root vs deep in the tree.
"""
from __future__ import annotations

import os
from pathlib import Path

import pytest

from agentmap.scanner import _SKIP_DIRS, scan

# An unambiguous OpenAI call site (matches the openai CALL pattern) plus a
# hardcoded endpoint (matches the openai ENDPOINT pattern).
OPENAI_CALL = 'resp = client.chat.completions.create(model="gpt-4o", messages=msgs)\n'
OPENAI_ENDPOINT = 'fetch("https://api.openai.com/v1/chat/completions")\n'


def _plant_control(root: Path) -> None:
    (root / "app.py").write_text(OPENAI_CALL, encoding="utf-8")


def _found_names(findings) -> set[str]:
    return {Path(f.path).name for f in findings}


def _paths_under(findings, subtree: Path) -> list[str]:
    prefix = str(subtree) + os.sep
    return [f.path for f in findings if f.path.startswith(prefix) or f.path == str(subtree)]


# ── every skip dir is pruned ─────────────────────────────────────────────────

@pytest.mark.parametrize("skipdir", sorted(_SKIP_DIRS))
def test_skip_dir_is_pruned(tmp_path: Path, skipdir: str) -> None:
    _plant_control(tmp_path)
    dep_dir = tmp_path / skipdir / "sub"
    dep_dir.mkdir(parents=True)
    (dep_dir / "dep.py").write_text(OPENAI_CALL + OPENAI_ENDPOINT, encoding="utf-8")

    findings = scan(tmp_path)

    assert "app.py" in _found_names(findings), "control file at repo root was not found"
    assert _paths_under(findings, tmp_path / skipdir) == [], (
        f"findings leaked from skipped dir {skipdir!r}"
    )
    assert all(f.provider == "openai" for f in findings)


# ── original user crash layout: pnpm/Nx cache tree ───────────────────────────

def test_nx_pnpm_cache_tree_never_scanned_and_never_stated(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    """The layout that crashed the original user: a deep Nx/pnpm cache tree.

    The tree must be pruned at `.nx` — no findings from it, and no file
    inside it may even be stat-ed (Path.stat is spied and raises
    PermissionError for anything under the tree; pruning means the spy is
    never triggered for those paths).
    """
    _plant_control(tmp_path)
    bad = (
        tmp_path / ".nx" / "cache" / "8f3a91c2d4" / "packages" / "web" / ".next"
        / "standalone" / "node_modules" / ".pnpm" / "node_modules" / "abs-svg-path"
    )
    bad.mkdir(parents=True)
    (bad / "index.js").write_text(OPENAI_ENDPOINT, encoding="utf-8")
    (bad / "package.json").write_text('{"name": "abs-svg-path"}\n', encoding="utf-8")

    tree_root = str(tmp_path / ".nx")
    stated_inside_tree: list[str] = []
    real_stat = Path.stat

    def spy_stat(self, *args, **kwargs):
        s = str(self)
        if s == tree_root or s.startswith(tree_root + os.sep):
            stated_inside_tree.append(s)
            raise PermissionError(13, "Access is denied", s)
        return real_stat(self, *args, **kwargs)

    monkeypatch.setattr(Path, "stat", spy_stat)

    findings = scan(tmp_path)

    assert stated_inside_tree == [], (
        f"paths inside the pruned .nx tree were stat-ed: {stated_inside_tree}"
    )
    assert _paths_under(findings, tmp_path / ".nx") == []
    assert "app.py" in _found_names(findings)


# ── a FILE named like a skip dir is still scanned ────────────────────────────

def test_file_named_node_modules_is_scanned(tmp_path: Path) -> None:
    (tmp_path / "node_modules").write_text(OPENAI_CALL, encoding="utf-8")

    findings = scan(tmp_path)

    assert any(
        Path(f.path).name == "node_modules" and f.provider == "openai"
        for f in findings
    ), "a regular file literally named 'node_modules' must still be scanned"


def test_file_named_vendor_is_scanned_alongside_vendor_dir(tmp_path: Path) -> None:
    # Both a skip-named file and a same-named-sibling situation: the file
    # "vendor.py" and file "vendor"-named file scan; the dir "vendor" does not.
    (tmp_path / "vendor").mkdir()
    (tmp_path / "vendor" / "dep.py").write_text(OPENAI_CALL, encoding="utf-8")
    (tmp_path / "src").mkdir()
    (tmp_path / "src" / "vendor").write_text(OPENAI_ENDPOINT, encoding="utf-8")

    findings = scan(tmp_path)

    assert _paths_under(findings, tmp_path / "vendor") == []
    assert any(f.path == str(tmp_path / "src" / "vendor") for f in findings)


# ── nested skip-in-skip ──────────────────────────────────────────────────────

def test_nested_skip_in_skip_is_pruned(tmp_path: Path) -> None:
    _plant_control(tmp_path)
    inner = tmp_path / "node_modules" / "some-pkg" / "dist" / "__pycache__"
    inner.mkdir(parents=True)
    (inner / "dep.py").write_text(OPENAI_CALL, encoding="utf-8")
    (tmp_path / "node_modules" / "some-pkg" / "index.js").write_text(
        OPENAI_ENDPOINT, encoding="utf-8"
    )

    findings = scan(tmp_path)

    assert _paths_under(findings, tmp_path / "node_modules") == []
    assert _found_names(findings) == {"app.py"}


def test_inner_skip_dir_inside_normal_dirs_is_pruned(tmp_path: Path) -> None:
    # Skip dir NOT at root: normal dirs above it, skip dir below.
    _plant_control(tmp_path)
    keep = tmp_path / "src" / "lib"
    keep.mkdir(parents=True)
    (keep / "util.py").write_text(OPENAI_CALL, encoding="utf-8")
    skipped = keep / "node_modules" / "sub"
    skipped.mkdir(parents=True)
    (skipped / "dep.py").write_text(OPENAI_CALL, encoding="utf-8")

    findings = scan(tmp_path)

    names = _found_names(findings)
    assert "app.py" in names, "control at root missing"
    assert "util.py" in names, "sibling file next to a deep skip dir missing"
    assert _paths_under(findings, keep / "node_modules") == []


# ── skip dir at root vs deep ─────────────────────────────────────────────────

@pytest.mark.parametrize("depth", ["root", "deep"])
def test_skip_dir_pruned_at_any_depth(tmp_path: Path, depth: str) -> None:
    _plant_control(tmp_path)
    base = tmp_path if depth == "root" else tmp_path / "a" / "b" / "c"
    target = base / ".turbo" / "sub"
    target.mkdir(parents=True)
    (target / "dep.py").write_text(OPENAI_CALL, encoding="utf-8")

    findings = scan(tmp_path)

    assert "app.py" in _found_names(findings)
    assert _paths_under(findings, base / ".turbo") == []


# ── unreadable dir is non-fatal (POSIX-only) ─────────────────────────────────

@pytest.mark.skipif(os.name != "posix", reason="chmod-based denial is POSIX-only")
@pytest.mark.skipif(
    os.name == "posix" and getattr(os, "geteuid", lambda: -1)() == 0,
    reason="root bypasses file modes",
)
def test_unreadable_dir_does_not_crash_scan(tmp_path: Path) -> None:
    _plant_control(tmp_path)
    locked = tmp_path / "locked"
    locked.mkdir()
    (locked / "dep.py").write_text(OPENAI_CALL, encoding="utf-8")
    locked.chmod(0)
    try:
        findings = scan(tmp_path)
    finally:
        locked.chmod(0o755)
    assert "app.py" in _found_names(findings)
