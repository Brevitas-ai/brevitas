package cli

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/config"
)

func TestDirectoryShortcutsFiltersAndDeduplicates(t *testing.T) {
	home := t.TempDir()
	for _, dir := range []string{"Documents", "Downloads", "Desktop", "GitHub", filepath.Join("Documents", "GitHub")} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cwd := filepath.Join(home, "Documents", "GitHub")

	shortcuts := directoryShortcuts(cwd, home)
	labels := make([]string, 0, len(shortcuts))
	for _, shortcut := range shortcuts {
		labels = append(labels, shortcut.label)
	}
	want := []string{"Current directory", "Home", "Documents", "Downloads", "Desktop", "GitHub"}
	if strings.Join(labels, ",") != strings.Join(want, ",") {
		t.Fatalf("labels = %v, want %v", labels, want)
	}
}

func TestChildDirectoriesSortsFiltersAndFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"zebra", "Alpha", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(root, "zebra"), filepath.Join(root, "linked")); err != nil {
			t.Fatal(err)
		}
	}

	visible, err := childDirectories(root, false)
	if err != nil {
		t.Fatal(err)
	}
	wantVisible := []string{"Alpha"}
	if runtime.GOOS != "windows" {
		wantVisible = append(wantVisible, "linked")
	}
	wantVisible = append(wantVisible, "zebra")
	if got := baseNames(visible); strings.Join(got, ",") != strings.Join(wantVisible, ",") {
		t.Fatalf("visible directories = %v, want %v", got, wantVisible)
	}

	all, err := childDirectories(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := baseNames(all); len(got) == 0 || got[0] != ".hidden" {
		t.Fatalf("directories with hidden enabled = %v", got)
	}
}

func TestBrowseDirectoriesSelectsNestedDirectory(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	app := &App{In: strings.NewReader("1\n1\n0\ny\n"), Out: &output, Err: &output}
	selected, ok, err := app.browseDirectories(bufio.NewReader(app.In), []directoryShortcut{{label: "Test", path: root}})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || selected != project {
		t.Fatalf("selection = %q, %v; want %q, true", selected, ok, project)
	}
	if !strings.Contains(output.String(), "Use this folder? "+project) {
		t.Fatalf("confirmation was not shown:\n%s", output.String())
	}
}

func TestBrowseDirectoriesCanGoUpAndDeclineConfirmation(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	input := strings.NewReader("1\n1\nu\n0\nn\nq\n")
	app := &App{In: input, Out: &output, Err: &output}
	selected, ok, err := app.browseDirectories(bufio.NewReader(input), []directoryShortcut{{label: "Test", path: root}})
	if err != nil {
		t.Fatal(err)
	}
	if ok || selected != "" {
		t.Fatalf("selection = %q, %v; want cancellation", selected, ok)
	}
	if strings.Count(output.String(), "Browsing: "+root) < 2 {
		t.Fatalf("expected navigation to return to root:\n%s", output.String())
	}
}

func TestBrowseDirectoriesTogglesHiddenDirectories(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, ".project")
	if err := os.Mkdir(hidden, 0o755); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	input := strings.NewReader("1\nh\n1\n0\ny\n")
	app := &App{In: input, Out: &output, Err: &output}
	selected, ok, err := app.browseDirectories(bufio.NewReader(input), []directoryShortcut{{label: "Test", path: root}})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || selected != hidden {
		t.Fatalf("selection = %q, %v; want %q, true", selected, ok, hidden)
	}
}

func TestBrowseDirectoriesRecoversFromInvalidChoicesAndReturnsToStart(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	input := strings.NewReader("invalid\n1\n99\ns\n1\n0\nmaybe\ny\n")
	app := &App{In: input, Out: &output, Err: &output}
	selected, ok, err := app.browseDirectories(bufio.NewReader(input), []directoryShortcut{{label: "Test", path: root}})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || selected != root {
		t.Fatalf("selection = %q, %v; want %q, true", selected, ok, root)
	}
	for _, message := range []string{
		"Please enter a listed number or q to cancel.",
		"Please enter a listed number or action.",
		"Please enter y or n (or q to cancel).",
	} {
		if !strings.Contains(output.String(), message) {
			t.Fatalf("missing %q in output:\n%s", message, output.String())
		}
	}
}

func TestBrowseDirectoriesHandlesMissingLocationAndEOF(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "removed")
	var output bytes.Buffer
	input := strings.NewReader("1\nq\n")
	app := &App{In: input, Out: &output, Err: &output}
	selected, ok, err := app.browseDirectories(bufio.NewReader(input), []directoryShortcut{{label: "Missing", path: missing}})
	if err != nil {
		t.Fatal(err)
	}
	if ok || selected != "" || !strings.Contains(output.String(), "Cannot open") {
		t.Fatalf("unexpected result %q, %v:\n%s", selected, ok, output.String())
	}

	output.Reset()
	input = strings.NewReader("")
	app.In = input
	selected, ok, err = app.browseDirectories(bufio.NewReader(input), []directoryShortcut{{label: "Test", path: t.TempDir()}})
	if err != nil || ok || selected != "" {
		t.Fatalf("EOF result = %q, %v, %v", selected, ok, err)
	}
}

func TestInstallRepoCancellationDoesNotStartScanner(t *testing.T) {
	var output bytes.Buffer
	app := &App{In: strings.NewReader("q\n"), Out: &output, Err: &output}
	if err := app.cmdInstall(t.Context(), []string{"repo", "--apply"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Installation cancelled.") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestInstallRepoHelpDoesNotOpenNavigator(t *testing.T) {
	var output bytes.Buffer
	app := &App{Cfg: config.Default(), In: strings.NewReader(""), Out: &output, Err: &output}
	if err := app.cmdInstall(t.Context(), []string{"repo", "--help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Usage:\n  bvx install repo [flags]") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestInstallDirectPathStillUsesCodebaseFlow(t *testing.T) {
	var output bytes.Buffer
	missing := filepath.Join(t.TempDir(), "missing")
	app := &App{Cfg: config.Default(), In: strings.NewReader(""), Out: &output, Err: &output}
	err := app.cmdInstall(t.Context(), []string{missing})
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("error = %v", err)
	}
}

func baseNames(paths []string) []string {
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	return names
}
