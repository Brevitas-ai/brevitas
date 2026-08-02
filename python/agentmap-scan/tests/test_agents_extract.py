"""Tests for agentmap.agents: extract_agents, purpose_by_file, _first_sentence."""
from __future__ import annotations

import os
import textwrap

import pytest

from agentmap.agents import Agent, _first_sentence, extract_agents, purpose_by_file


def _write(tmp_path, name, source):
    p = tmp_path / name
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(textwrap.dedent(source), encoding="utf-8")
    return p


# ── prompt-style matchers ──────────────────────────────────────────────────────

def test_dict_style_system_message(tmp_path):
    _write(tmp_path, "bot.py", '''
        def researcher(client):
            messages = [
                {"role": "system", "content": "You are a market research analyst. Analyze competitors."},
                {"role": "user", "content": user_input},
            ]
            return client.chat.completions.create(messages=messages)
    ''')
    agents = extract_agents(tmp_path)
    assert len(agents) == 1
    a = agents[0]
    assert a.name == "researcher"
    assert a.role == "You are a market research analyst."
    assert a.file.endswith("bot.py")
    assert a.line == 4  # 1-based; leading blank line from the fixture counts


def test_system_kwarg_uses_enclosing_def(tmp_path):
    _write(tmp_path, "support.py", '''
        def support_bot(client):
            return client.messages.create(system="You are a helpful support bot. Be brief.")
    ''')
    agents = extract_agents(tmp_path)
    assert [(a.name, a.role) for a in agents] == [
        ("support_bot", "You are a helpful support bot."),
    ]


def test_system_prompt_kwarg_uses_enclosing_class(tmp_path):
    _write(tmp_path, "summ.py", '''
        class Summarizer:
            system_prompt = "You summarize documents into bullet points. Keep them terse."
    ''')
    agents = extract_agents(tmp_path)
    assert [(a.name, a.role) for a in agents] == [
        ("Summarizer", "You summarize documents into bullet points."),
    ]


def test_instructions_kwarg(tmp_path):
    _write(tmp_path, "assist.py", '''
        def scheduler(api):
            return api.assistants.create(instructions="You schedule meetings without conflicts. Prefer mornings.")
    ''')
    agents = extract_agents(tmp_path)
    assert [(a.name, a.role) for a in agents] == [
        ("scheduler", "You schedule meetings without conflicts."),
    ]


def test_crewai_agent_role_with_name_kwarg(tmp_path):
    _write(tmp_path, "crew.py", '''
        def build_crew():
            critic = Agent(name="critic", role="You critique essays harshly but fairly. Cite examples.")
            return critic
    ''')
    agents = extract_agents(tmp_path)
    assert len(agents) == 1
    # Agent(name=...) on the prompt line wins over the enclosing def
    assert agents[0].name == "critic"
    assert agents[0].role == "You critique essays harshly but fairly."


def test_name_prefixed_system_variable(tmp_path):
    _write(tmp_path, "copy.py", '''
        copywriter_system = "You are a senior copywriter. Write punchy ad copy that converts."
    ''')
    agents = extract_agents(tmp_path)
    assert [(a.name, a.role) for a in agents] == [
        ("copywriter", "You are a senior copywriter."),
    ]


def test_triple_quoted_prompt(tmp_path):
    _write(tmp_path, "plan.py", '''
        def planner():
            system = """You plan trips carefully.
            Consider budget and weather before booking anything."""
            return system
    ''')
    agents = extract_agents(tmp_path)
    assert [(a.name, a.role) for a in agents] == [
        ("planner", "You plan trips carefully."),
    ]


# ── name resolution ────────────────────────────────────────────────────────────

def test_agent_kwarg_beats_enclosing_def(tmp_path):
    _write(tmp_path, "route.py", '''
        def route():
            return call(agent="dispatcher", system="You dispatch incoming requests to the right worker.")
    ''')
    agents = extract_agents(tmp_path)
    assert len(agents) == 1
    assert agents[0].name == "dispatcher"


def test_fallback_name_is_agent(tmp_path):
    _write(tmp_path, "cfg.py",
           'cfg = dict(system="You are a nameless background maintenance job runner.")\n')
    agents = extract_agents(tmp_path)
    assert len(agents) == 1
    assert agents[0].name == "agent"


def test_dedup_same_file_and_name_keeps_first(tmp_path):
    _write(tmp_path, "dup.py", '''
        def helper():
            a = create(system="You do the first thing which is plenty long.")
            b = create(system="You do the second thing which is plenty long.")
            return a, b
    ''')
    agents = extract_agents(tmp_path)
    assert len(agents) == 1
    assert agents[0].name == "helper"
    assert agents[0].role == "You do the first thing which is plenty long."


# ── filtering ──────────────────────────────────────────────────────────────────

@pytest.mark.parametrize("prompt", ["tiny.", "1234567", "  hi.  "])
def test_short_prompts_ignored(tmp_path, prompt):
    _write(tmp_path, "short.py", f'''
        def shorty():
            return create(system="{prompt}")
    ''')
    assert extract_agents(tmp_path) == []


def test_bare_message_role_values_not_prompts(tmp_path):
    _write(tmp_path, "chat.py", '''
        def talker():
            msgs = [{"role": "user", "content": user_text}]
            role = "assistant"
            other_role = "developer"
            return msgs, role, other_role
    ''')
    assert extract_agents(tmp_path) == []


@pytest.mark.parametrize("relpath", ["test_bot.py", "tests/bot.py", "helper_test.py"])
def test_test_files_excluded(tmp_path, relpath):
    _write(tmp_path, relpath, '''
        def fixture_agent():
            return create(system="You are only a fixture for testing purposes. Ignore.")
    ''')
    assert extract_agents(tmp_path) == []


@pytest.mark.skipif(os.name != "posix", reason="chmod-based denial is POSIX-only")
@pytest.mark.skipif(getattr(os, "geteuid", lambda: 1)() == 0,
                    reason="root ignores file permission bits")
def test_unreadable_file_skipped(tmp_path):
    _write(tmp_path, "good.py", '''
        def visible():
            return create(system="You are the readable agent in this directory now.")
    ''')
    bad = _write(tmp_path, "bad.py", '''
        def hidden():
            return create(system="You are unreadable and must be skipped entirely.")
    ''')
    bad.chmod(0o000)
    try:
        agents = extract_agents(tmp_path)
    finally:
        bad.chmod(0o644)
    assert [a.name for a in agents] == ["visible"]


# ── _first_sentence ────────────────────────────────────────────────────────────

@pytest.mark.parametrize("raw,expected", [
    ("First sentence. Second sentence.", "First sentence."),
    ("No terminator here", "No terminator here"),
    ("Multi\nline   text. Trailing tail.", "Multi line text."),
    ("Shout loudly! Then whisper.", "Shout loudly!"),
    ("Is it ready? Then ship it.", "Is it ready?"),
    ("Line one.\\nLine two.", "Line one."),  # literal backslash-n from source strings
])
def test_first_sentence(raw, expected):
    assert _first_sentence(raw) == expected


def test_first_sentence_caps_at_120_chars():
    long_sentence = "A" * 150 + ". Second."
    assert _first_sentence(long_sentence) == "A" * 120
    no_punct = "B" * 200
    assert _first_sentence(no_punct) == "B" * 120
    assert len(_first_sentence(no_punct)) == 120


# ── purpose_by_file & multi-agent files ───────────────────────────────────────

def test_purpose_by_file_first_agent_per_file_sorted():
    agents = [
        Agent(name="beta", role="Handles B things.", file="b.py", line=1),
        Agent(name="alpha", role="Handles A things.", file="a.py", line=3),
        Agent(name="beta2", role="Second in b, ignored.", file="b.py", line=9),
    ]
    assert purpose_by_file(agents) == [
        ("a.py", "Handles A things."),
        ("b.py", "Handles B things."),
    ]


def test_multiple_agents_in_one_file(tmp_path):
    _write(tmp_path, "multi.py", '''
        def researcher(client):
            messages = [
                {"role": "system", "content": "You are a market research analyst. Analyze competitors."},
            ]
            return client.chat.completions.create(messages=messages)

        copywriter_system = "You are a senior copywriter. Write punchy ad copy that converts."
    ''')
    agents = extract_agents(tmp_path)
    names = {a.name for a in agents}
    assert names == {"researcher", "copywriter"}
    # dict-style pattern is matched first, so the file's purpose is the researcher's
    purposes = purpose_by_file(agents)
    assert len(purposes) == 1
    assert purposes[0][0].endswith("multi.py")
    assert purposes[0][1] == "You are a market research analyst."
