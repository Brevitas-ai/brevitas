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
		"██████╗", "Optimize every AI request without changing how you work", "START HERE",
		"bvx install repo", "bvx install ai", "bvx status", "bvx stats", "bvx help",
		ansiBrandBlue, ansiBrandShadow, ansiBrandWhite,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("home screen missing %q: %q", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "BVX") || strings.Contains(output.String(), "command center") {
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

func TestHomeBrandArtHasConsistentDimensions(t *testing.T) {
	for row, line := range homeBrandArt {
		if len([]rune(line)) != homeBrandWordWidth {
			t.Fatalf("brand art row %d has width %d, want %d", row, len([]rune(line)), homeBrandWordWidth)
		}
	}
	if len(homeBrandIconMask) != len(homeBrandArt)*2 {
		t.Fatalf("brand icon has %d mask rows, want %d", len(homeBrandIconMask), len(homeBrandArt)*2)
	}
	for row, line := range homeBrandIconMask {
		if len([]rune(line)) != homeBrandIconMaskWidth {
			t.Fatalf("brand icon row %d has width %d, want %d", row, len([]rune(line)), homeBrandIconMaskWidth)
		}
	}
	for row, line := range homeBrandLogo(false) {
		if len([]rune(line)) != homeBrandLogoWidth {
			t.Fatalf("combined brand row %d has width %d, want %d", row, len([]rune(line)), homeBrandLogoWidth)
		}
	}
}
