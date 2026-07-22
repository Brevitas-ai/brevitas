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
		"ACTIONS", "SELECTED ACTION", "Configure AI tools", "READY TO LAUNCH", "[a]",
		"↑/↓ NAVIGATE", "╭", "╯", "██████╗", ansiBrandBlue, ansiBrightCyan, ansiSelect,
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("TUI output missing %q", expected)
		}
	}
}

func TestWideHomeUsesAvailableTerminalSpace(t *testing.T) {
	var output bytes.Buffer
	renderHomeMenu(&output, 0, 100, 30)

	if got := strings.Count(output.String(), "\r\n"); got != 24 {
		t.Fatalf("wide home rendered %d completed rows, want 24", got)
	}
	for _, expected := range []string{
		"Connect repository", "bvx install repo", "Browse to a codebase", "Press", "Enter", "[r]",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("wide home missing %q", expected)
		}
	}
	if strings.Contains(output.String(), "Optimize every AI request") || strings.Contains(output.String(), "command center") {
		t.Fatal("wide home still contains the removed tagline")
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

func TestHomeMenuBackKeyKeepsDashboardOpen(t *testing.T) {
	var output bytes.Buffer
	args, handled, err := homeMenuWithKeys(
		bufio.NewReader(strings.NewReader("\x1b[D\x1b[B\r")),
		&output,
		func() (int, int) { return 100, 30 },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || strings.Join(args, " ") != "install ai" {
		t.Fatalf("selection after Back = %q, handled=%v", args, handled)
	}
}

func TestWaitForHomeKey(t *testing.T) {
	tests := []struct {
		name string
		keys string
		back bool
	}{
		{name: "enter", keys: "\r", back: true},
		{name: "back shortcut", keys: "b", back: true},
		{name: "home shortcut", keys: "h", back: true},
		{name: "left arrow", keys: "\x1b[D", back: true},
		{name: "backspace", keys: "\x7f", back: true},
		{name: "quit", keys: "q", back: false},
		{name: "control c", keys: "\x03", back: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			back, err := waitForHomeKey(bufio.NewReader(strings.NewReader(test.keys)))
			if err != nil {
				t.Fatal(err)
			}
			if back != test.back {
				t.Fatalf("back = %v, want %v", back, test.back)
			}
		})
	}
}

func TestHomeActionStartsOnCleanScreen(t *testing.T) {
	var output bytes.Buffer
	renderHomeActionScreen(&output)
	if got, want := output.String(), "\x1b[H\x1b[2J"; got != want {
		t.Fatalf("action screen reset = %q, want %q", got, want)
	}
}

func TestDashboardOwnsOneAlternateScreenUntilExit(t *testing.T) {
	var output bytes.Buffer
	enterAlternateScreen(&output)
	renderHomeActionScreen(&output)
	leaveAlternateScreen(&output)

	got := output.String()
	if strings.Count(got, "\x1b[?1049h") != 1 {
		t.Fatalf("dashboard entered the alternate screen more than once: %q", got)
	}
	if strings.Count(got, "\x1b[?1049l") != 1 {
		t.Fatalf("dashboard did not restore the normal terminal exactly once: %q", got)
	}
	if !strings.Contains(got, "\x1b[H\x1b[2J") {
		t.Fatalf("dashboard did not clear between views: %q", got)
	}
}

func TestSavingsCommandUsesDashboardView(t *testing.T) {
	if !isDashboardViewCommand("stats") {
		t.Fatal("stats should open in the dashboard view")
	}
	for _, command := range []string{"status", "doctor", "install"} {
		if isDashboardViewCommand(command) {
			t.Fatalf("%s unexpectedly opens in the dashboard view", command)
		}
	}
}

func TestSetupCommandsLandAtDashboard(t *testing.T) {
	for _, command := range []string{"install", "login"} {
		if !isDashboardLandingCommand(command) {
			t.Fatalf("%s should land at the dashboard", command)
		}
	}
	for _, command := range []string{"logout", "status", "stats"} {
		if isDashboardLandingCommand(command) {
			t.Fatalf("%s unexpectedly lands at the dashboard", command)
		}
	}
}

func TestParseHomeCommandLine(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "status", want: "status"},
		{input: "bvx status", want: "status"},
		{input: "BVX install '/tmp/My Project' --apply", want: "install|/tmp/My Project|--apply"},
		{input: `config set-port 9000`, want: "config|set-port|9000"},
		{input: "", want: ""},
	}
	for _, test := range tests {
		args, err := parseHomeCommandLine(test.input)
		if err != nil {
			t.Fatalf("parse %q: %v", test.input, err)
		}
		if got := strings.Join(args, "|"); got != test.want {
			t.Fatalf("parse %q = %q, want %q", test.input, got, test.want)
		}
	}
	for _, input := range []string{`status \`, `install "unfinished`} {
		if _, err := parseHomeCommandLine(input); err == nil {
			t.Fatalf("parse %q unexpectedly succeeded", input)
		}
	}
}

func TestCommandReferencePromptAcceptsOptionalBVXPrefix(t *testing.T) {
	var output bytes.Buffer
	app := &App{In: strings.NewReader("bvx status\n"), Out: &output}
	args, quit, err := app.promptHomeCommand()
	if err != nil {
		t.Fatal(err)
	}
	if quit || strings.Join(args, " ") != "status" {
		t.Fatalf("prompt returned args=%q, quit=%v", args, quit)
	}
	for _, expected := range []string{"RUN A COMMAND", "bvx ›", "Blank Enter returns Home"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("command prompt missing %q", expected)
		}
	}
}

func TestNarrowHomeExplainsHowToStart(t *testing.T) {
	var output bytes.Buffer
	renderHomeMenu(&output, 0, 50, 20)
	for _, expected := range []string{"BREVITAS", "HOME", "Start here", "[r]", "bvx install repo"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("narrow home missing %q: %q", expected, output.String())
		}
	}
	if strings.Contains(output.String(), ansiBrandBlue) {
		t.Fatal("narrow home unexpectedly rendered the large logo")
	}
}
