package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/config"
)

func TestFlaggedCommandsRenderThemedHelpWithoutErrors(t *testing.T) {
	tests := [][]string{
		{"login", "--help"},
		{"install", "ai", "--help"},
		{"install", "./demo-repo", "--help"},
		{"install", "repo", "--help"},
		{"providers", "--help"},
		{"logs", "--help"},
		{"uninstall", "--help"},
		{"update", "--help"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var output bytes.Buffer
			app := &App{Cfg: config.Default(), In: strings.NewReader(""), Out: &output, Err: &output}
			if code := app.Run(context.Background(), args); code != 0 {
				t.Fatalf("exit code = %d, output:\n%s", code, output.String())
			}
			if !strings.Contains(output.String(), "USAGE") && !strings.Contains(output.String(), "Usage:") {
				t.Fatalf("help missing usage section:\n%s", output.String())
			}
			if strings.Contains(output.String(), "flag: help requested") {
				t.Fatalf("help was reported as an error:\n%s", output.String())
			}
		})
	}
}
