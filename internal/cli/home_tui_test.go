package cli

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestHomeMenuLaunchesSelectedActionWithArrowKeys(t *testing.T) {
	var output bytes.Buffer
	args, handled, err := homeMenuWithKeys(
		bufio.NewReader(strings.NewReader("\x1b[B\r")),
		&output,
		func() (int, int) { return 100, 30 },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || strings.Join(args, " ") != "install ai" {
		t.Fatalf("selection = %q, handled=%v", args, handled)
	}
	for _, expected := range []string{
		"██████╗", "BREVITAS", "START HERE", "EXPLORE", "Configure AI tools", "[a]",
		"↑/↓ navigate", ansiSelect,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("TUI output missing %q", expected)
		}
	}
}

func TestHomeMenuLaunchesActionsWithShortcutKeys(t *testing.T) {
	for _, action := range homeActions {
		t.Run(action.label, func(t *testing.T) {
			var output bytes.Buffer
			args, handled, err := homeMenuWithKeys(
				bufio.NewReader(strings.NewReader(string(action.shortcut))),
				&output,
				func() (int, int) { return 100, 30 },
			)
			if err != nil {
				t.Fatal(err)
			}
			if !handled || strings.Join(args, " ") != strings.Join(action.args, " ") {
				t.Fatalf("shortcut %q dispatched %q, handled=%v; want %q", action.shortcut, args, handled, action.args)
			}
		})
	}
}

func TestHomeMenuQuitMakesNoSelection(t *testing.T) {
	var output bytes.Buffer
	args, handled, err := homeMenuWithKeys(
		bufio.NewReader(strings.NewReader("q")),
		&output,
		func() (int, int) { return 60, 18 },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || len(args) != 0 {
		t.Fatalf("quit selection = %q, handled=%v", args, handled)
	}
}

func TestEveryHomeActionDispatchesTheExpectedCommand(t *testing.T) {
	for index, action := range homeActions {
		t.Run(action.label, func(t *testing.T) {
			keys := strings.Repeat("\x1b[B", index) + "\r"
			var output bytes.Buffer
			args, handled, err := homeMenuWithKeys(
				bufio.NewReader(strings.NewReader(keys)),
				&output,
				func() (int, int) { return 100, 30 },
			)
			if err != nil {
				t.Fatal(err)
			}
			if !handled {
				t.Fatal("menu input was not handled")
			}
			if got, want := strings.Join(args, " "), strings.Join(action.args, " "); got != want {
				t.Fatalf("dispatched %q, want %q", got, want)
			}
			if !strings.Contains(output.String(), action.label) || !strings.Contains(output.String(), action.command) {
				t.Fatalf("selected action was not rendered: %s", action.label)
			}
		})
	}
}

func TestHomeMenuArrowNavigationWraps(t *testing.T) {
	var output bytes.Buffer
	args, handled, err := homeMenuWithKeys(
		bufio.NewReader(strings.NewReader("\x1b[A\x1b[B\r")),
		&output,
		func() (int, int) { return 100, 30 },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || strings.Join(args, " ") != "install repo" {
		t.Fatalf("wrapped selection = %q, handled=%v", args, handled)
	}
}

func TestNarrowHomeExplainsHowToStart(t *testing.T) {
	var output bytes.Buffer
	renderHomeMenu(&output, 0, 50, 20)
	for _, expected := range []string{"BVX", "Brevitas home", "Start here", "[r]", "bvx install repo"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("narrow home missing %q: %q", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "██████╗") {
		t.Fatal("narrow home unexpectedly rendered the large logo")
	}
}
