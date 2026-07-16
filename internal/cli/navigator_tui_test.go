package cli

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTUIKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  tuiKey
	}{
		{name: "up", input: "\x1b[A", want: tuiKeyUp},
		{name: "down", input: "\x1b[B", want: tuiKeyDown},
		{name: "right", input: "\x1b[C", want: tuiKeyRight},
		{name: "left", input: "\x1b[D", want: tuiKeyLeft},
		{name: "enter", input: "\r", want: tuiKeyEnter},
		{name: "backspace", input: "\x7f", want: tuiKeyBack},
		{name: "preview", input: "p", want: tuiKeyPreview},
		{name: "hidden", input: "h", want: tuiKeyHidden},
		{name: "shortcuts", input: "s", want: tuiKeyStart},
		{name: "quit", input: "q", want: tuiKeyQuit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := readTUIKey(bufio.NewReader(strings.NewReader(test.input)))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("key = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTUIEntriesIncludesFoldersAndPreviewableFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.go", "README.md", ".secret"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := tuiEntries(root, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].kind != tuiSelectFolder || entries[1].kind != tuiDirectory {
		t.Fatalf("select action and directories should come first: %#v", entries)
	}
	for _, entry := range entries {
		if entry.label == ".secret" {
			t.Fatal("hidden file was shown before hidden entries were enabled")
		}
	}

	entries, err = tuiEntries(root, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	foundHidden := false
	for _, entry := range entries {
		foundHidden = foundHidden || entry.label == ".secret"
	}
	if !foundHidden {
		t.Fatal("hidden file was not shown after hidden entries were enabled")
	}
}

func TestFileNamesUseTypeSpecificColors(t *testing.T) {
	tests := []struct {
		name      string
		wantColor string
	}{
		{name: "main.go", wantColor: ansiCyan},
		{name: "README.md", wantColor: ansiPink},
		{name: "Makefile", wantColor: ansiOrange},
		{name: "LICENSE", wantColor: ansiYellow},
		{name: "go.mod", wantColor: ansiTeal},
		{name: "photo.png", wantColor: ansiPurple},
		{name: "unknown-file", wantColor: ansiTeal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := tuiEntry{kind: tuiFile, label: test.name, path: test.name}
			_, color := tuiEntryIcon(entry)
			if color != test.wantColor {
				t.Fatalf("color = %q, want %q", color, test.wantColor)
			}
			rendered := formatTUIEntry(entry, false, false, 80)
			if !strings.Contains(rendered, test.wantColor) || !strings.Contains(rendered, test.name) {
				t.Fatalf("rendered entry = %q", rendered)
			}
		})
	}
}

func TestArrowNavigatorSelectsNestedRepository(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Preview me\nA short description."), 0o644); err != nil {
		t.Fatal(err)
	}

	// Enter the starting shortcut, move from "Use this folder" to project,
	// enter it, choose "Use this folder", then confirm Yes.
	keys := "\r\x1b[B\r\r\r"
	var output bytes.Buffer
	app := &App{Out: &output, Err: &output}
	selected, ok, err := app.browseDirectoriesWithKeys(
		bufio.NewReader(strings.NewReader(keys)),
		&output,
		[]directoryShortcut{{label: "Test", path: root}},
		func() (int, int) { return 100, 30 },
	)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || selected != project {
		t.Fatalf("selection = %q, %v; want %q, true", selected, ok, project)
	}
	for _, want := range []string{"BVX Repository Navigator", "PREVIEW", "│", "↑/↓ move", "Use this repository?", ansiBlue} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q in TUI output", want)
		}
	}
}

func TestFilePreviewShowsTextAndRejectsTerminalControlCodes(t *testing.T) {
	root := t.TempDir()
	textPath := filepath.Join(root, "main.go")
	if err := os.WriteFile(textPath, []byte("package main\n\x1b[31munsafe terminal text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := strings.Join(filePreview(textPath, 80, 5), "\n")
	if !strings.Contains(lines, ansiSyntaxKeyword+"package"+ansiReset) || !strings.Contains(lines, "main") || !strings.Contains(lines, "unsafe terminal text") {
		t.Fatalf("text preview = %q", lines)
	}
	if strings.Contains(lines, "\x1b[31munsafe") {
		t.Fatalf("file-provided terminal escape was not sanitized: %q", lines)
	}

	binaryPath := filepath.Join(root, "image.bin")
	if err := os.WriteFile(binaryPath, []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	binaryPreview := strings.Join(filePreview(binaryPath, 80, 5), "\n")
	if !strings.Contains(binaryPreview, "Binary file") {
		t.Fatalf("binary preview = %q", binaryPreview)
	}
}

func TestPreviewToggleRemovesPreviewPane(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	app := &App{Out: &output, Err: &output}
	_, ok, err := app.browseDirectoriesWithKeys(
		bufio.NewReader(strings.NewReader("pq")),
		&output,
		[]directoryShortcut{{label: "Test", path: root}},
		func() (int, int) { return 100, 30 },
	)
	if err != nil || ok {
		t.Fatalf("result = %v, %v", ok, err)
	}
	if count := strings.Count(output.String(), "PREVIEW"); count != 1 {
		t.Fatalf("preview rendered %d times, want once before toggling off", count)
	}
}

func TestNarrowNavigatorFallsBackToStackedPreview(t *testing.T) {
	root := t.TempDir()
	entries := []tuiEntry{{kind: tuiDirectory, label: "project", path: root}}
	var output bytes.Buffer
	renderNavigator(&output, root, entries, 0, false, true, "", 70, 24)
	if !strings.Contains(output.String(), " Preview ") {
		t.Fatalf("stacked preview missing from narrow layout: %q", output.String())
	}
	if strings.Contains(output.String(), "│") {
		t.Fatal("narrow layout unexpectedly rendered the wide divider")
	}
}
