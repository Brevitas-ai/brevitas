package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJavaScriptPreviewSyntaxHighlighting(t *testing.T) {
	highlighter := newPreviewHighlighter("eslint.config.mjs")
	line := highlighter.highlight(`import nextVitals from "eslint-config-next";`)
	for _, want := range []string{
		ansiSyntaxKeyword + "import" + ansiReset,
		ansiSyntaxKeyword + "from" + ansiReset,
		ansiSyntaxString + `"eslint-config-next"` + ansiReset,
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing highlighted token %q in %q", want, line)
		}
	}

	line = highlighter.highlight(`const result = defineConfig(42); // configured`)
	for _, color := range []string{ansiSyntaxKeyword, ansiSyntaxFunction, ansiSyntaxNumber, ansiSyntaxComment} {
		if !strings.Contains(line, color) {
			t.Fatalf("missing color %q in %q", color, line)
		}
	}
}

func TestPreviewSyntaxTracksMultilineComments(t *testing.T) {
	highlighter := newPreviewHighlighter("main.go")
	first := highlighter.highlight("/* start")
	second := highlighter.highlight("still commented */ return 2")
	if !strings.Contains(first, ansiSyntaxComment) || !strings.Contains(second, ansiSyntaxComment) {
		t.Fatalf("comment highlighting missing: %q / %q", first, second)
	}
	if !strings.Contains(second, ansiSyntaxKeyword+"return"+ansiReset) {
		t.Fatalf("highlighting did not resume after comment: %q", second)
	}
}

func TestFilePreviewAppliesSyntaxColors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.ts")
	if err := os.WriteFile(path, []byte("const answer = makeAnswer(42);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	preview := strings.Join(filePreview(path, 100, 5), "\n")
	for _, color := range []string{ansiSyntaxKeyword, ansiSyntaxFunction, ansiSyntaxNumber} {
		if !strings.Contains(preview, color) {
			t.Fatalf("preview missing syntax color %q: %q", color, preview)
		}
	}
}

func TestMarkdownPreviewHighlightsHeadingsAndInlineCode(t *testing.T) {
	highlighter := newPreviewHighlighter("README.md")
	if line := highlighter.highlight("# Setup"); !strings.Contains(line, ansiSyntaxKeyword) {
		t.Fatalf("heading was not highlighted: %q", line)
	}
	if line := highlighter.highlight("Run `bvx install repo` now."); !strings.Contains(line, ansiSyntaxString) {
		t.Fatalf("inline code was not highlighted: %q", line)
	}
}
