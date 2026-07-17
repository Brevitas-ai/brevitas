package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestThemeCoversSharedCLIElements(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	var output bytes.Buffer
	app := &App{Out: &output, Err: &output}

	app.page("Demo", "Subtitle")
	app.section("Runtime")
	app.ok("healthy")
	app.warn("attention")
	app.fail("offline")
	app.note("helpful context")
	app.command("bvx status", "inspect services")
	app.metric("Tokens saved", "12,400", ansiGreen)
	app.success("Complete")

	for _, expected := range []string{
		ansiCyan, ansiPink, ansiGreen, ansiYellow, ansiRed,
		"BVX", "RUNTIME", "bvx status", "Tokens saved", "Complete",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("themed output missing %q: %q", expected, output.String())
		}
	}
}

func TestNoColorDisablesThemeEscapes(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	app := &App{Out: &output, Err: &output}
	app.page("Demo", "Subtitle")
	app.ok("healthy")
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("NO_COLOR output contains ANSI escapes: %q", output.String())
	}
}
