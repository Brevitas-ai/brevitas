package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type directoryShortcut struct {
	label string
	path  string
}

func (a *App) printRepositoryNavigatorHelp() {
	a.say("Choose a codebase with the guided directory navigator.\n")
	a.say("Usage:\n  bvx install repo [flags]\n")
	a.say("Navigator controls:")
	a.say("  Up/Down         move through folders and files")
	a.say("  Enter/Right     open a folder or confirm an action")
	a.say("  Left/Backspace  move to the parent folder")
	a.say("  p               toggle the file preview pane")
	a.say("  h               show or hide hidden entries")
	a.say("  s               return to starting locations")
	a.say("  q               cancel\n")
	a.say("Flags:")
	a.say("  --apply         route the codebase through Brevitas (writes .env.agentmap)")
	a.say("  --auto          with --apply, also rewrite hardcoded provider URLs")
	a.say("  --no-open       do not open the HTML report in a browser")
	a.say("  --target URL     gateway URL to route calls through")
	a.say("  -h, --help      show this help")
}

// selectRepository uses the full-screen arrow-key browser when stdin and
// stdout are terminals. Redirected input keeps the line-oriented browser so
// scripts and tests can still drive the command deterministically.
func (a *App) selectRepository() (string, bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("get current directory: %w", err)
	}
	home, _ := os.UserHomeDir()

	shortcuts := directoryShortcuts(cwd, home)
	if len(shortcuts) == 0 {
		return "", false, errors.New("no accessible starting directories")
	}
	if in, inOK := a.In.(*os.File); inOK {
		if out, outOK := a.Out.(*os.File); outOK && canUseArrowNavigator(in, out) {
			return a.browseDirectoriesTUI(in, out, shortcuts)
		}
	}

	reader := bufio.NewReader(a.In)
	return a.browseDirectories(reader, shortcuts)
}

func (a *App) browseDirectories(reader *bufio.Reader, shortcuts []directoryShortcut) (string, bool, error) {
	current := ""
	lastUsable := ""
	showHidden := false

	for {
		if current == "" {
			a.printDirectoryShortcuts(shortcuts)
			choice, err := readNavigatorLine(reader, "Choose a location: ", a.Out)
			if errors.Is(err, io.EOF) {
				return "", false, nil
			}
			if err != nil {
				return "", false, err
			}
			if isCancelChoice(choice) {
				return "", false, nil
			}

			index, ok := numberedChoice(choice, len(shortcuts))
			if !ok {
				a.say("Please enter a listed number or q to cancel.\n")
				continue
			}
			current = shortcuts[index].path
		}

		children, err := childDirectories(current, showHidden)
		if err != nil {
			a.warn("Cannot open %s: %v", current, err)
			if lastUsable != "" {
				current = lastUsable
			} else {
				current = ""
			}
			continue
		}
		lastUsable = current

		a.printDirectory(current, children, showHidden)
		choice, err := readNavigatorLine(reader, "Choose a folder or action: ", a.Out)
		if errors.Is(err, io.EOF) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}

		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "0", "select":
			confirmed, cancelled, err := confirmDirectory(reader, a.Out, current)
			if err != nil {
				return "", false, err
			}
			if cancelled {
				return "", false, nil
			}
			if confirmed {
				return current, true, nil
			}
		case "u", "up", "..":
			parent := filepath.Dir(current)
			if parent == current {
				a.say("Already at the filesystem root.\n")
			} else {
				current = parent
			}
		case "s", "start":
			current = ""
		case "h", "hidden":
			showHidden = !showHidden
		case "q", "quit", "cancel":
			return "", false, nil
		default:
			index, ok := numberedChoice(choice, len(children))
			if !ok {
				a.say("Please enter a listed number or action.\n")
				continue
			}
			current = children[index]
		}
	}
}

func (a *App) printDirectoryShortcuts(shortcuts []directoryShortcut) {
	a.say("Choose where your repository is located:\n")
	for i, shortcut := range shortcuts {
		fmt.Fprintf(a.Out, "  %d) %-20s %s\n", i+1, shortcut.label, shortcut.path)
	}
	fmt.Fprintln(a.Out, "  q) Cancel")
}

func (a *App) printDirectory(current string, children []string, showHidden bool) {
	a.say("\nBrowsing: %s\n", current)
	fmt.Fprintln(a.Out, "  0) Select this folder")
	for i, child := range children {
		fmt.Fprintf(a.Out, "  %d) %s/\n", i+1, filepath.Base(child))
	}
	fmt.Fprintln(a.Out, "  u) Up one level")
	fmt.Fprintln(a.Out, "  s) Start locations")
	if showHidden {
		fmt.Fprintln(a.Out, "  h) Hide hidden folders")
	} else {
		fmt.Fprintln(a.Out, "  h) Show hidden folders")
	}
	fmt.Fprintln(a.Out, "  q) Cancel")
}

func directoryShortcuts(cwd, home string) []directoryShortcut {
	candidates := []directoryShortcut{{label: "Current directory", path: cwd}}
	if home != "" {
		candidates = append(candidates,
			directoryShortcut{label: "Home", path: home},
			directoryShortcut{label: "Documents", path: filepath.Join(home, "Documents")},
			directoryShortcut{label: "Downloads", path: filepath.Join(home, "Downloads")},
			directoryShortcut{label: "Desktop", path: filepath.Join(home, "Desktop")},
			directoryShortcut{label: "GitHub", path: filepath.Join(home, "GitHub")},
			directoryShortcut{label: "GitHub (Documents)", path: filepath.Join(home, "Documents", "GitHub")},
		)
	}

	seen := make(map[string]bool)
	shortcuts := make([]directoryShortcut, 0, len(candidates))
	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate.path)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		key := abs
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if seen[key] || !isDirectory(abs) {
			continue
		}
		seen[key] = true
		candidate.path = abs
		shortcuts = append(shortcuts, candidate)
	}
	return shortcuts
}

func childDirectories(dir string, showHidden bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	children := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 && isDirectory(path) {
			children = append(children, path)
		}
	}
	sort.Slice(children, func(i, j int) bool {
		left := strings.ToLower(filepath.Base(children[i]))
		right := strings.ToLower(filepath.Base(children[j]))
		if left == right {
			return filepath.Base(children[i]) < filepath.Base(children[j])
		}
		return left < right
	})
	return children, nil
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func confirmDirectory(reader *bufio.Reader, out io.Writer, path string) (confirmed, cancelled bool, err error) {
	for {
		fmt.Fprintf(out, "\nUse this folder? %s [y/N]: ", path)
		choice, readErr := readNavigatorLine(reader, "", out)
		if errors.Is(readErr, io.EOF) {
			return false, true, nil
		}
		if readErr != nil {
			return false, false, readErr
		}
		switch strings.ToLower(strings.TrimSpace(choice)) {
		case "y", "yes":
			return true, false, nil
		case "", "n", "no":
			return false, false, nil
		case "q", "quit", "cancel":
			return false, true, nil
		default:
			fmt.Fprintln(out, "Please enter y or n (or q to cancel).")
		}
	}
}

func readNavigatorLine(reader *bufio.Reader, prompt string, out io.Writer) (string, error) {
	fmt.Fprint(out, prompt)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func numberedChoice(choice string, count int) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(choice))
	if err != nil || n < 1 || n > count {
		return 0, false
	}
	return n - 1, true
}

func isCancelChoice(choice string) bool {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "q", "quit", "cancel":
		return true
	default:
		return false
	}
}

func navigatorHelpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}
