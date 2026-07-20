package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHomeHighlightsFirstRunCommands(t *testing.T) {
	var output bytes.Buffer
	renderHome(&output, true)

	for _, expected := range []string{
		"██████╗", "BREVITAS", "Optimize every AI request without changing how you work", "START HERE",
		"bvx install repo", "bvx install ai", "bvx status", "bvx stats", "bvx help", ansiCyan,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("home screen missing %q: %q", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "BVX / BREVITAS") || strings.Contains(output.String(), "command center") {
		t.Fatalf("home screen contains retired branding: %q", output.String())
	}
}

func TestHomeCanRenderWithoutTerminalEscapes(t *testing.T) {
	var output bytes.Buffer
	renderHome(&output, false)
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("plain home screen contains ANSI escapes: %q", output.String())
	}
}
