"""
Code-only agent + responsibility extraction — no LLM, no API key.

For each source file we find system-prompt strings and the nearest agent name,
then tidy the prompt to its first sentence. This is a heuristic, not a parser.

ponytail: regex heuristic with a known ceiling — covers raw OpenAI/Anthropic
message dicts, `system=`/`system_prompt=`/`instructions=` assignments,
CrewAI `Agent(role=...)`, `brevitas.agent("name")`, and plain enclosing
functions/classes. It will miss graph-based frameworks (LangGraph node defs,
AutoGen). Upgrade path: add a per-framework matcher here as users report misses.

    python -m agentmap.agents        # runs the self-check
"""
from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path

from .scanner import _walk  # reuse the same file-walk (skips deps/binaries/docs)


# ── system-prompt matchers ─────────────────────────────────────────────────────
# Each yields the prompt text via group 'p'. Ordered most-specific first.
_QUOTE = r"""(?:"{3}(?P<p3>.*?)"{3}|'{3}(?P<p3b>.*?)'{3}|"(?P<p1>(?:\\.|[^"\\])*)"|'(?P<p1b>(?:\\.|[^'\\])*)')"""

_PROMPT_PATTERNS = [
    # {"role": "system", "content": "..."}
    re.compile(r"""role["']?\s*:\s*["']system["']\s*,\s*["']?content["']?\s*:\s*""" + _QUOTE,
               re.IGNORECASE | re.DOTALL),
    # system= / system_prompt= / instructions= / role=, with an optional name
    # prefix so `copywriter_system = "..."` / `EDITOR_PROMPT = "..."` also match.
    re.compile(r"""\b\w*(?:system_prompt|system|instructions|_prompt)\s*[:=]\s*""" + _QUOTE,
               re.IGNORECASE | re.DOTALL),
    re.compile(r"""\brole\s*[:=]\s*""" + _QUOTE, re.IGNORECASE | re.DOTALL),  # CrewAI Agent(role=)
]

# ── agent-name matchers (searched upward from the prompt) ───────────────────────
_NAME_PATTERNS = [
    re.compile(r"""\.agent\(\s*["'](\w+)["']"""),                 # brevitas.agent("x")
    re.compile(r"""Agent\([^)]*\bname\s*=\s*["'](\w+)["']"""),    # Agent(name="x")
    re.compile(r"""\bagent(?:_name)?\s*=\s*["'](\w+)["']"""),     # agent="x"
    re.compile(r"""\b(\w+?)_(?:system|sys|prompt|instructions)\b\s*[:=]""", re.IGNORECASE),  # copywriter_system=
    re.compile(r"""\bdef\s+(\w+)\s*\("""),                        # enclosing def
    re.compile(r"""\bclass\s+(\w+)\b"""),                         # enclosing class
]


# Bare message-role values that leak from {"role": "user"} etc. — not agents.
_MESSAGE_ROLES = {"system", "user", "assistant", "tool", "function", "developer", "model", "ai"}


def _is_test_file(path: Path) -> bool:
    n = path.name.lower()
    return (n.startswith("test_") or n.endswith("_test.py")
            or "test" in path.parts or "tests" in path.parts)


@dataclass
class Agent:
    name: str
    role: str          # tidied first sentence of the system prompt
    file: str
    line: int


def _first_sentence(text: str) -> str:
    t = re.sub(r"\s+", " ", (text or "").replace("\\n", " ")).strip()
    # split on sentence end or the first newline-ish boundary
    m = re.search(r"(.+?[.!?])(?:\s|$)", t)
    s = m.group(1) if m else t
    return s.strip()[:120]


def _prompt_text(m: re.Match) -> str:
    gd = m.groupdict()
    for g in ("p3", "p3b", "p1", "p1b"):
        if gd.get(g):
            return gd[g]
    return ""


def extract_agents(root: str | Path) -> list[Agent]:
    """Find (agent name → responsibility) pairs across a codebase, code-only."""
    agents: list[Agent] = []
    seen: set[tuple[str, str]] = set()   # (file, name) dedup

    for path in _walk(Path(root)):
        if _is_test_file(path):
            continue  # test fixtures aren't real agents
        try:
            text = path.read_text(encoding="utf-8", errors="strict")
        except (UnicodeDecodeError, OSError):
            continue
        lines = text.splitlines()

        for pat in _PROMPT_PATTERNS:
            for m in pat.finditer(text):
                prompt = _prompt_text(m)
                if not prompt or len(prompt.strip()) < 8:
                    continue
                if prompt.strip().lower() in _MESSAGE_ROLES:
                    continue  # a message-role value, not a system prompt
                line_no = text.count("\n", 0, m.start()) + 1
                name = _nearest_name(lines, line_no)
                key = (str(path), name)
                if key in seen:
                    continue
                seen.add(key)
                agents.append(Agent(name=name, role=_first_sentence(prompt),
                                    file=str(path), line=line_no))
    return agents


# Generic tokens that are never a useful agent name (they're the prompt keyword itself).
_GENERIC_NAMES = {"system", "agent", "prompt", "instructions", "role", "messages",
                  "content", "user", "assistant", "sys", "self", "cls"}


def _nearest_name(lines: list[str], line_no: int, look_back: int = 25) -> str:
    """First non-generic agent-name signal at/above line_no; else the enclosing def/class."""
    start = max(0, line_no - 1)
    for i in range(start, max(-1, start - look_back), -1):
        for pat in _NAME_PATTERNS:
            m = pat.search(lines[i]) if i < len(lines) else None
            if m and m.group(1).lower() not in _GENERIC_NAMES:
                return m.group(1)
    return "agent"


def purpose_by_file(agents: list[Agent]) -> list[tuple[str, str]]:
    """One purpose line per file = its first agent's role. [(file, role), …]."""
    out: dict[str, str] = {}
    for a in agents:
        out.setdefault(a.file, a.role)
    return sorted(out.items())


# ── self-check ──────────────────────────────────────────────────────────────────

def _selfcheck() -> None:
    import tempfile
    fixture = '''
def researcher(client):
    messages = [
        {"role": "system", "content": "You are a market research analyst. Analyze competitors."},
        {"role": "user", "content": user_input},
    ]
    return client.chat.completions.create(model="gpt-4o", messages=messages)

copywriter_system = "You are a senior copywriter. Write punchy ad copy that converts."
'''
    with tempfile.TemporaryDirectory() as d:
        (Path(d) / "agents.py").write_text(fixture)
        found = extract_agents(d)
        by_name = {a.name: a.role for a in found}
        assert "researcher" in by_name, by_name
        assert by_name["researcher"].startswith("You are a market research analyst"), by_name
        assert not by_name["researcher"].endswith("competitors."), "should stop at first sentence"
        assert any("copywriter" in n for n in by_name), by_name
        print("ok: agents", {k: v[:40] for k, v in by_name.items()})


if __name__ == "__main__":
    _selfcheck()
