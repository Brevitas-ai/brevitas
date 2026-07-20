package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoadingIndicatorUsesPlainTextForRedirectedOutput(t *testing.T) {
	var output bytes.Buffer
	app := &App{Out: &output}
	wantErr := errors.New("operation failed")

	err := app.withLoading("Scanning for AI tools…", func() error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("withLoading error = %v, want %v", err, wantErr)
	}
	if got := output.String(); got != "  … Scanning for AI tools…\n" {
		t.Fatalf("loading output = %q", got)
	}
}

func TestAnimatedLoadingIndicatorAdvancesAndRestoresTerminal(t *testing.T) {
	var output bytes.Buffer
	indicator := newLoadingIndicator(&output, "Installing optimizer…", true, time.Hour)
	indicator.render(1)
	indicator.Stop()
	indicator.Stop() // stopping twice must be safe

	got := output.String()
	for _, want := range []string{"\x1b[?25l", "⠋", "⠙", "Installing optimizer…", "\x1b[?25h"} {
		if !strings.Contains(got, want) {
			t.Fatalf("animated output missing %q: %q", want, got)
		}
	}
}
