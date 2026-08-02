"""Detection matrix across providers and languages for agentmap.scanner.scan.

Every provider with an ENDPOINT pattern in agentmap.signatures gets a one-line
fixture in a non-Python language containing its real endpoint host. SDK CALL
patterns are exercised in Python and TypeScript styles. Negative cases cover
prose mentions, .md exclusion, multi-provider lines, per-line dedup, and
MODEL-kind detection.

All fixtures live under tmp_path; deterministic, no network. POSIX-only tests
are guarded so the suite also passes on Windows.
"""
from __future__ import annotations

import os

import pytest

from agentmap.scanner import providers_found, scan
from agentmap.signatures import ENDPOINT, PROVIDERS

IS_POSIX = os.name == "posix"
posix_only = pytest.mark.skipif(not IS_POSIX, reason="requires POSIX (chmod/symlink)")
not_root = pytest.mark.skipif(
    IS_POSIX and os.geteuid() == 0, reason="chmod-based denial is ineffective as root"
)


def _scan_one(tmp_path, filename: str, line: str):
    (tmp_path / filename).write_text(line + "\n", encoding="utf-8")
    return scan(tmp_path)


# ── ENDPOINT matrix: one real-host fixture per provider, all non-Python ───────
# (provider id, fixture filename, one source line in that language)
ENDPOINT_FIXTURES = [
    ("openai", "Client.java",
     'HttpRequest request = HttpRequest.newBuilder().uri(URI.create("https://api.openai.com/v1/chat/completions")).build();'),
    ("azure_openai", "azure.php",
     'curl_setopt($ch, CURLOPT_URL, "https://myres.openai.azure.com/openai/deployments/prod/chat/completions?api-version=2024-02-01");'),
    ("anthropic", "ask.sh",
     'curl https://api.anthropic.com/v1/messages -H "x-api-key: $ANTHROPIC_API_KEY" -d @body.json'),
    ("google_gemini", "gemini.rb",
     'uri = URI("https://generativelanguage.googleapis.com/v1beta/models")'),
    ("google_gemini", "vertex.sh",
     'curl https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/publishers/google/models'),
    ("deepseek", "main.go",
     'req, _ := http.NewRequest("POST", "https://api.deepseek.com/v1/chat/completions", body)'),
    ("groq", "groq.rs",
     'let res = client.post("https://api.groq.com/openai/v1/chat/completions").send()?;'),
    ("xai", "Grok.cs",
     'var req = new HttpRequestMessage(HttpMethod.Post, "https://api.x.ai/v1/chat/completions");'),
    ("mistral", "Mistral.kt",
     'val request = Request.Builder().url("https://api.mistral.ai/v1/chat/completions").build()'),
    ("cohere", "cohere.php",
     'curl_setopt($ch, CURLOPT_URL, "https://api.cohere.com/v2/chat");'),
    ("bedrock", "bedrock.sh",
     'curl https://bedrock-runtime.us-east-1.amazonaws.com/foundation-models -H "Authorization: $SIG"'),
    ("together", "together.rb",
     'uri = URI("https://api.together.xyz/v1/chat/completions")'),
    ("fireworks", "fireworks.ex",
     '{:ok, resp} = HTTPoison.post("https://api.fireworks.ai/inference/v1/chat/completions", body)'),
    ("openrouter", "router.js",
     'const res = await fetch("https://openrouter.ai/api/v1/chat/completions", { method: "POST" });'),
    ("perplexity", "pplx.sh",
     'curl https://api.perplexity.ai/chat/completions -d @payload.json'),
    ("replicate", "rep.go",
     'req, _ := http.NewRequest("POST", "https://api.replicate.com/v1/predictions", body)'),
    ("huggingface", "Hf.java",
     'HttpRequest req = HttpRequest.newBuilder(URI.create("https://router.huggingface.co/v1/chat/completions")).build();'),
    ("ollama", "local.sh",
     "curl http://localhost:11434/api/generate -d '{\"model\": \"llama3\"}'"),
]


def test_endpoint_matrix_covers_every_endpoint_provider():
    """Every provider that declares an ENDPOINT pattern is in the matrix above."""
    endpoint_ids = {p.id for p in PROVIDERS if any(pt.kind == ENDPOINT for pt in p.patterns)}
    covered = {pid for pid, _, _ in ENDPOINT_FIXTURES}
    assert endpoint_ids <= covered, f"missing fixtures for: {endpoint_ids - covered}"


@pytest.mark.parametrize(
    "pid,fname,line", ENDPOINT_FIXTURES, ids=[f"{p}-{f}" for p, f, _ in ENDPOINT_FIXTURES]
)
def test_endpoint_detected_in_non_python_source(tmp_path, pid, fname, line):
    findings = _scan_one(tmp_path, fname, line)
    assert pid in providers_found(findings)
    hits = [f for f in findings if f.provider == pid]
    assert any(f.kind == ENDPOINT for f in hits), hits
    assert all(f.line == 1 for f in hits)


# ── SDK CALL patterns: Python and TypeScript call styles ──────────────────────
CALL_FIXTURES = [
    ("openai", "app.py",
     'resp = client.chat.completions.create(model="gpt-4o", messages=msgs)'),
    ("openai", "app.ts",
     'const r = await openai.chat.completions.create({ model: "gpt-4o", messages });'),
    ("anthropic", "bot.py",
     'msg = client.messages.create(model="claude-sonnet-4-20250514", max_tokens=64, messages=msgs)'),
    ("anthropic", "bot.ts",
     'const stream = anthropic.messages.stream({ model: "claude-sonnet-4-20250514", max_tokens: 64 });'),
]


@pytest.mark.parametrize(
    "pid,fname,line", CALL_FIXTURES, ids=[f"{p}-{f}" for p, f, _ in CALL_FIXTURES]
)
def test_sdk_call_detected(tmp_path, pid, fname, line):
    findings = _scan_one(tmp_path, fname, line)
    hits = [f for f in findings if f.provider == pid]
    assert hits, findings
    # CALL outranks the model-id literal on the same line (per-line dedup keeps
    # the first, highest-signal pattern for a provider).
    assert hits[0].kind == "call"
    assert len(hits) == 1


# ── Negative cases ────────────────────────────────────────────────────────────

def test_prose_provider_names_do_not_match(tmp_path):
    findings = _scan_one(
        tmp_path, "notes.txt",
        "We compared OpenAI, Anthropic, Cohere and Mistral before picking a vendor.",
    )
    assert findings == []


def test_markdown_files_are_never_scanned(tmp_path):
    findings = _scan_one(
        tmp_path, "README.md",
        'curl https://api.openai.com/v1/chat/completions and client.messages.create(...)',
    )
    assert findings == []


def test_two_providers_on_one_line_yields_both(tmp_path):
    findings = _scan_one(
        tmp_path, "compare.sh",
        "curl https://api.openai.com/v1/models https://api.anthropic.com/v1/models",
    )
    assert sorted(f.provider for f in findings) == ["anthropic", "openai"]
    assert all(f.line == 1 and f.kind == ENDPOINT for f in findings)


def test_same_provider_twice_on_one_line_deduped(tmp_path):
    findings = _scan_one(
        tmp_path, "urls.go",
        'urls := []string{"https://api.openai.com/v1", "https://api.openai.com/v2"}',
    )
    assert len(findings) == 1
    assert findings[0].provider == "openai"


# ── MODEL-kind detection ──────────────────────────────────────────────────────

@pytest.mark.parametrize(
    "pid,fname,line",
    [
        ("anthropic", "cfg.py", 'MODEL = "claude-3-5-sonnet-20241022"'),
        ("google_gemini", "cfg.ts", 'const MODEL = "gemini-1.5-flash";'),
    ],
    ids=["anthropic-model", "gemini-model"],
)
def test_model_literal_detected_as_model_kind(tmp_path, pid, fname, line):
    findings = _scan_one(tmp_path, fname, line)
    hits = [f for f in findings if f.provider == pid]
    assert len(hits) == 1
    assert hits[0].kind == "model"


# ── POSIX-only filesystem hazards during a provider scan ──────────────────────

@posix_only
@not_root
def test_unreadable_file_skipped_scan_continues(tmp_path):
    (tmp_path / "ok.go").write_text(
        'req, _ := http.NewRequest("POST", "https://api.deepseek.com/v1/chat/completions", b)\n',
        encoding="utf-8",
    )
    secret = tmp_path / "secret.rb"
    secret.write_text('uri = URI("https://api.openai.com/v1")\n', encoding="utf-8")
    secret.chmod(0)
    try:
        found = providers_found(scan(tmp_path))
        assert "deepseek" in found
        assert "openai" not in found  # unreadable file silently skipped
    finally:
        secret.chmod(0o644)


@posix_only
def test_broken_symlink_skipped_scan_continues(tmp_path):
    (tmp_path / "dangling.py").symlink_to(tmp_path / "no-such-target.py")
    (tmp_path / "real.sh").write_text(
        "curl https://api.perplexity.ai/chat/completions\n", encoding="utf-8"
    )
    found = providers_found(scan(tmp_path))
    assert found == ["perplexity"]
