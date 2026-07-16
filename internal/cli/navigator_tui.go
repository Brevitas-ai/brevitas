package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
	ansiTeal    = "\x1b[38;5;44m"
	ansiOrange  = "\x1b[38;5;208m"
	ansiPurple  = "\x1b[38;5;141m"
	ansiPink    = "\x1b[38;5;205m"
	ansiGray    = "\x1b[38;5;245m"
	ansiSelect  = "\x1b[48;5;236m\x1b[1m"
)

type tuiEntryKind int

const (
	tuiSelectFolder tuiEntryKind = iota
	tuiShortcut
	tuiDirectory
	tuiFile
)

type tuiEntry struct {
	kind  tuiEntryKind
	label string
	path  string
}

type tuiKey int

const (
	tuiKeyUnknown tuiKey = iota
	tuiKeyUp
	tuiKeyDown
	tuiKeyRight
	tuiKeyLeft
	tuiKeyEnter
	tuiKeyBack
	tuiKeyPreview
	tuiKeyHidden
	tuiKeyStart
	tuiKeyQuit
)

func canUseArrowNavigator(in, out *os.File) bool {
	return term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}

func (a *App) browseDirectoriesTUI(in, out *os.File, shortcuts []directoryShortcut) (string, bool, error) {
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return "", false, fmt.Errorf("enable arrow-key input: %w", err)
	}
	defer func() {
		_ = term.Restore(int(in.Fd()), state)
		fmt.Fprint(out, ansiReset+"\x1b[?25h\x1b[?1049l\r\n")
	}()
	fmt.Fprint(out, "\x1b[?1049h\x1b[?25l")

	size := func() (int, int) {
		width, height, sizeErr := term.GetSize(int(out.Fd()))
		if sizeErr != nil || width < 40 || height < 15 {
			return 100, 30
		}
		return width, height
	}
	return a.browseDirectoriesWithKeys(bufio.NewReader(in), out, shortcuts, size)
}

func (a *App) browseDirectoriesWithKeys(reader *bufio.Reader, out io.Writer, shortcuts []directoryShortcut, size func() (int, int)) (string, bool, error) {
	current := ""
	lastUsable := ""
	cursor := 0
	showHidden := false
	showPreview := true
	message := ""

	for {
		entries, err := tuiEntries(current, shortcuts, showHidden)
		if err != nil {
			message = fmt.Sprintf("Cannot open %s: %v", current, err)
			if lastUsable != "" {
				current = lastUsable
			} else {
				current = ""
			}
			cursor = 0
			continue
		}
		if current != "" {
			lastUsable = current
		}
		cursor = clampCursor(cursor, len(entries))
		width, height := size()
		renderNavigator(out, current, entries, cursor, showHidden, showPreview, message, width, height)
		message = ""

		key, keyErr := readTUIKey(reader)
		if errors.Is(keyErr, io.EOF) {
			return "", false, nil
		}
		if keyErr != nil {
			return "", false, keyErr
		}

		switch key {
		case tuiKeyUp:
			if cursor > 0 {
				cursor--
			}
		case tuiKeyDown:
			if cursor+1 < len(entries) {
				cursor++
			}
		case tuiKeyEnter, tuiKeyRight:
			entry := entries[cursor]
			switch entry.kind {
			case tuiShortcut, tuiDirectory:
				current = entry.path
				cursor = 0
			case tuiSelectFolder:
				confirmed, cancelled, confirmErr := confirmDirectoryTUI(reader, out, current, size)
				if confirmErr != nil {
					return "", false, confirmErr
				}
				if cancelled {
					return "", false, nil
				}
				if confirmed {
					return current, true, nil
				}
			case tuiFile:
				message = "Files are preview-only; choose 'Use this folder' to select the repository."
			}
		case tuiKeyLeft, tuiKeyBack:
			if current != "" {
				parent := filepath.Dir(current)
				if parent != current {
					current = parent
					cursor = 0
				} else {
					message = "Already at the filesystem root."
				}
			}
		case tuiKeyPreview:
			showPreview = !showPreview
		case tuiKeyHidden:
			showHidden = !showHidden
			cursor = 0
		case tuiKeyStart:
			current = ""
			cursor = 0
		case tuiKeyQuit:
			return "", false, nil
		}
	}
}

func tuiEntries(current string, shortcuts []directoryShortcut, showHidden bool) ([]tuiEntry, error) {
	if current == "" {
		entries := make([]tuiEntry, 0, len(shortcuts))
		for _, shortcut := range shortcuts {
			entries = append(entries, tuiEntry{kind: tuiShortcut, label: shortcut.label, path: shortcut.path})
		}
		return entries, nil
	}

	directoryEntries, err := os.ReadDir(current)
	if err != nil {
		return nil, err
	}
	entries := []tuiEntry{{kind: tuiSelectFolder, label: "Use this folder", path: current}}
	children := make([]tuiEntry, 0, len(directoryEntries))
	for _, entry := range directoryEntries {
		if !showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(current, entry.Name())
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 && isDirectory(path) {
			children = append(children, tuiEntry{kind: tuiDirectory, label: entry.Name(), path: path})
		} else {
			children = append(children, tuiEntry{kind: tuiFile, label: entry.Name(), path: path})
		}
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].kind != children[j].kind {
			return children[i].kind == tuiDirectory
		}
		left := strings.ToLower(children[i].label)
		right := strings.ToLower(children[j].label)
		if left == right {
			return children[i].label < children[j].label
		}
		return left < right
	})
	return append(entries, children...), nil
}

func renderNavigator(out io.Writer, current string, entries []tuiEntry, cursor int, showHidden, showPreview bool, message string, width, height int) {
	if showPreview && width >= 76 && height >= 18 {
		renderNavigatorWide(out, current, entries, cursor, showHidden, message, width, height)
		return
	}
	renderNavigatorStacked(out, current, entries, cursor, showHidden, showPreview, message, width, height)
}

func renderNavigatorStacked(out io.Writer, current string, entries []tuiEntry, cursor int, showHidden, showPreview bool, message string, width, height int) {
	fmt.Fprint(out, "\x1b[H\x1b[2J")
	fmt.Fprintf(out, "%s%s BVX Repository Navigator %s\r\n", ansiBold, ansiCyan, ansiReset)
	if current == "" {
		fmt.Fprintf(out, "%sChoose a starting location%s\r\n\r\n", ansiDim, ansiReset)
	} else {
		fmt.Fprintf(out, "%s%s%s\r\n\r\n", ansiDim, truncateText(sanitizeTerminalText(current), width-2), ansiReset)
	}

	previewHeight := 0
	if showPreview {
		previewHeight = 8
	}
	visible := height - previewHeight - 8
	if visible < 4 {
		visible = 4
	}
	start := 0
	if cursor >= visible {
		start = cursor - visible + 1
	}
	end := minInt(len(entries), start+visible)
	for i := start; i < end; i++ {
		renderTUIEntry(out, entries[i], i == cursor, current == "", width)
	}
	if end < len(entries) {
		fmt.Fprintf(out, "  %s… %d more%s\r\n", ansiDim, len(entries)-end, ansiReset)
	}
	for lines := end - start; lines < visible; lines++ {
		fmt.Fprint(out, "\r\n")
	}

	if showPreview && len(entries) > 0 {
		renderPreview(out, entries[cursor], showHidden, width)
	}
	if message != "" {
		fmt.Fprintf(out, "%s%s%s\r\n", ansiYellow, truncateText(sanitizeTerminalText(message), width-2), ansiReset)
	} else {
		fmt.Fprint(out, "\r\n")
	}
	fmt.Fprintf(out, "%s↑/↓ move  Enter/→ open  ←/Backspace up  p preview  h hidden  s shortcuts  q quit%s", ansiDim, ansiReset)
}

func renderNavigatorWide(out io.Writer, current string, entries []tuiEntry, cursor int, showHidden bool, message string, width, height int) {
	fmt.Fprint(out, "\x1b[H\x1b[2J")
	fmt.Fprintf(out, "%s%s BVX Repository Navigator %s\x1b[K\r\n", ansiBold, ansiCyan, ansiReset)
	location := "Choose a starting location"
	if current != "" {
		location = sanitizeTerminalText(current)
	}
	fmt.Fprintf(out, "%s%s%s\x1b[K\r\n", ansiDim, truncateText(location, width-2), ansiReset)
	fmt.Fprintf(out, "%s%s%s\r\n", ansiBlue, strings.Repeat("─", width), ansiReset)

	leftWidth := width * 42 / 100
	if leftWidth < 28 {
		leftWidth = 28
	}
	if leftWidth > 52 {
		leftWidth = 52
	}
	rightWidth := maxInt(20, width-leftWidth-3)
	contentHeight := maxInt(8, height-5)
	entryRows := contentHeight - 1
	start := 0
	if cursor >= entryRows {
		start = cursor - entryRows + 1
	}
	end := minInt(len(entries), start+entryRows)
	preview := widePreviewLines(entries[cursor], showHidden, rightWidth, contentHeight-1)

	for row := 0; row < contentHeight; row++ {
		left := ""
		right := ""
		if row == 0 {
			left = ansiBold + ansiBlue + " FILES" + ansiReset
			right = ansiBold + ansiMagenta + " PREVIEW" + ansiReset
		} else {
			entryIndex := start + row - 1
			if entryIndex < end {
				left = formatTUIEntry(entries[entryIndex], entryIndex == cursor, current == "", leftWidth-1)
			} else if entryIndex == end && end < len(entries) {
				left = fmt.Sprintf("  %s… %d more%s", ansiDim, len(entries)-end, ansiReset)
			}
			previewIndex := row - 1
			if previewIndex < len(preview) {
				right = preview[previewIndex]
			}
		}
		fmt.Fprint(out, left)
		fmt.Fprintf(out, "\x1b[%dG%s│%s %s\x1b[K\r\n", leftWidth+1, ansiBlue, ansiReset, right)
	}

	fmt.Fprintf(out, "%s%s%s\r\n", ansiBlue, strings.Repeat("─", width), ansiReset)
	footer := "↑/↓ move  Enter/→ open  ←/Backspace up  p preview  h hidden  s shortcuts  q quit"
	if message != "" {
		footer = message
		fmt.Fprint(out, ansiYellow)
	} else {
		fmt.Fprint(out, ansiDim)
	}
	fmt.Fprintf(out, "%s%s\x1b[K", truncateText(sanitizeTerminalText(footer), width-1), ansiReset)
}

func renderTUIEntry(out io.Writer, entry tuiEntry, selected, shortcut bool, width int) {
	fmt.Fprintf(out, "%s\r\n", formatTUIEntry(entry, selected, shortcut, width))
}

func formatTUIEntry(entry tuiEntry, selected, shortcut bool, width int) string {
	prefix := "  "
	style := ""
	if selected {
		prefix = "> "
		style = ansiSelect
	}
	icon, color := tuiEntryIcon(entry)
	label := sanitizeTerminalText(entry.label)
	if shortcut {
		label += "  " + sanitizeTerminalText(entry.path)
	}
	label = truncateText(label, maxInt(10, width-8))
	return fmt.Sprintf("%s%s%s%s %s%s", style, prefix, color, icon, label, ansiReset)
}

func widePreviewLines(entry tuiEntry, showHidden bool, width, height int) []string {
	icon, color := tuiEntryIcon(entry)
	name := entry.label
	if entry.kind == tuiSelectFolder {
		name = filepath.Base(entry.path)
	}
	lines := []string{
		color + icon + " " + truncateText(sanitizeTerminalText(name), maxInt(1, width-3)) + ansiReset,
		ansiDim + truncateText(sanitizeTerminalText(entry.path), width) + ansiReset,
		"",
	}
	details := previewLines(entry, showHidden, width, maxInt(1, height-len(lines)))
	lines = append(lines, details...)
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func tuiEntryIcon(entry tuiEntry) (string, string) {
	switch entry.kind {
	case tuiSelectFolder:
		return "✓", ansiGreen
	case tuiShortcut, tuiDirectory:
		return "▸", ansiBlue
	}

	base := strings.ToLower(filepath.Base(entry.path))
	switch base {
	case "makefile", "dockerfile", "containerfile", "justfile":
		return "◆", ansiOrange
	case "license", "license.md", "copying":
		return "◆", ansiYellow
	case "go.mod", "go.sum", "package.json", "package-lock.json", "pyproject.toml", "cargo.toml":
		return "◆", ansiTeal
	case ".gitignore", ".gitattributes", ".dockerignore", ".editorconfig":
		return "◆", ansiGray
	}

	switch strings.ToLower(filepath.Ext(entry.path)) {
	case ".go", ".py", ".rs", ".ts", ".tsx", ".js", ".jsx", ".java", ".c", ".cc", ".cpp", ".h":
		return "●", ansiCyan
	case ".md", ".mdx", ".txt", ".rst":
		return "●", ansiPink
	case ".json", ".yaml", ".yml", ".toml", ".xml", ".ini":
		return "●", ansiYellow
	case ".sh", ".zsh", ".bash", ".ps1", ".bat":
		return "●", ansiGreen
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp":
		return "●", ansiPurple
	case ".html", ".css", ".scss", ".vue", ".svelte":
		return "●", ansiMagenta
	case ".sql", ".db", ".sqlite":
		return "●", ansiOrange
	case ".lock", ".sum":
		return "●", ansiGray
	default:
		return "•", ansiTeal
	}
}

func renderPreview(out io.Writer, entry tuiEntry, showHidden bool, width int) {
	barWidth := maxInt(16, minInt(width-2, 72))
	label := " Preview "
	leftWidth := minInt(6, maxInt(1, barWidth-len(label)))
	rightWidth := maxInt(1, barWidth-leftWidth-len(label))
	fmt.Fprintf(out, "%s%s%s%s%s\r\n", ansiBlue, strings.Repeat("─", leftWidth), label, strings.Repeat("─", rightWidth), ansiReset)
	lines := previewLines(entry, showHidden, width-4, 5)
	for i := 0; i < 5; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		fmt.Fprintf(out, "  %s\r\n", line)
	}
}

func previewLines(entry tuiEntry, showHidden bool, width, maxLines int) []string {
	if entry.kind == tuiSelectFolder || entry.kind == tuiShortcut || entry.kind == tuiDirectory {
		return directoryPreview(entry.path, showHidden, width, maxLines)
	}
	return filePreview(entry.path, width, maxLines)
}

func directoryPreview(path string, showHidden bool, width, maxLines int) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return []string{ansiRed + truncateText(sanitizeTerminalText(err.Error()), width) + ansiReset}
	}
	folders, files := 0, 0
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		fullPath := filepath.Join(path, entry.Name())
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 && isDirectory(fullPath) {
			folders++
			name := truncateText(sanitizeTerminalText(entry.Name())+"/", maxInt(1, width-2))
			names = append(names, ansiBlue+"▸ "+name+ansiReset)
		} else {
			files++
			icon, color := tuiEntryIcon(tuiEntry{kind: tuiFile, path: fullPath})
			name := truncateText(sanitizeTerminalText(entry.Name()), maxInt(1, width-2))
			names = append(names, color+icon+" "+name+ansiReset)
		}
	}
	lines := []string{fmt.Sprintf("%s%d folders%s  %s%d files%s", ansiBlue, folders, ansiReset, ansiMagenta, files, ansiReset)}
	for _, name := range names {
		if len(lines) >= maxLines {
			break
		}
		lines = append(lines, name)
	}
	for len(lines) < maxLines {
		lines = append(lines, "")
	}
	return lines
}

func filePreview(path string, width, maxLines int) []string {
	info, err := os.Stat(path)
	if err != nil {
		return []string{ansiRed + truncateText(sanitizeTerminalText(err.Error()), width) + ansiReset}
	}
	metadata := fmt.Sprintf("%s%s%s  %s", ansiCyan, humanSize(info.Size()), ansiReset, sanitizeTerminalText(info.Mode().String()))
	file, err := os.Open(path)
	if err != nil {
		return []string{metadata, ansiRed + truncateText(sanitizeTerminalText(err.Error()), width) + ansiReset}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16*1024))
	if err != nil {
		return []string{metadata, ansiRed + truncateText(sanitizeTerminalText(err.Error()), width) + ansiReset}
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return []string{metadata, ansiDim + "Binary file — text preview unavailable" + ansiReset}
	}
	if len(data) == 0 {
		return []string{metadata, ansiDim + "Empty file" + ansiReset}
	}

	lines := []string{metadata}
	highlighter := newPreviewHighlighter(path)
	for i, line := range strings.Split(string(data), "\n") {
		if len(lines) >= maxLines {
			break
		}
		clean := truncateText(sanitizeTerminalText(strings.TrimRight(line, "\r")), maxInt(1, width-5))
		lines = append(lines, fmt.Sprintf("%s%2d%s  %s", ansiDim, i+1, ansiReset, highlighter.highlight(clean)))
	}
	for len(lines) < maxLines {
		lines = append(lines, "")
	}
	return lines
}

func confirmDirectoryTUI(reader *bufio.Reader, out io.Writer, path string, size func() (int, int)) (confirmed, cancelled bool, err error) {
	cursor := 0
	for {
		width, _ := size()
		fmt.Fprint(out, "\x1b[H\x1b[2J")
		fmt.Fprintf(out, "%s%s Use this repository? %s\r\n\r\n", ansiBold, ansiCyan, ansiReset)
		fmt.Fprintf(out, "%s%s%s\r\n\r\n", ansiDim, truncateText(sanitizeTerminalText(path), width-2), ansiReset)
		choices := []string{"Yes — scan this folder", "No — go back"}
		for i, choice := range choices {
			prefix, style := "  ", ""
			if i == cursor {
				prefix, style = "> ", ansiSelect
			}
			fmt.Fprintf(out, "%s%s%s%s\r\n", style, prefix, choice, ansiReset)
		}
		fmt.Fprintf(out, "\r\n%s↑/↓ choose  Enter confirm  ←/Backspace go back  q cancel%s", ansiDim, ansiReset)

		key, keyErr := readTUIKey(reader)
		if errors.Is(keyErr, io.EOF) {
			return false, true, nil
		}
		if keyErr != nil {
			return false, false, keyErr
		}
		switch key {
		case tuiKeyUp, tuiKeyDown:
			cursor = 1 - cursor
		case tuiKeyEnter, tuiKeyRight:
			return cursor == 0, false, nil
		case tuiKeyLeft, tuiKeyBack:
			return false, false, nil
		case tuiKeyQuit:
			return false, true, nil
		}
	}
}

func readTUIKey(reader *bufio.Reader) (tuiKey, error) {
	b, err := reader.ReadByte()
	if err != nil {
		return tuiKeyUnknown, err
	}
	switch b {
	case '\r', '\n':
		return tuiKeyEnter, nil
	case 3, 'q', 'Q':
		return tuiKeyQuit, nil
	case 8, 127:
		return tuiKeyBack, nil
	case 'p', 'P':
		return tuiKeyPreview, nil
	case 'h', 'H':
		return tuiKeyHidden, nil
	case 's', 'S':
		return tuiKeyStart, nil
	case 27:
		second, secondErr := reader.ReadByte()
		if secondErr != nil {
			return tuiKeyUnknown, secondErr
		}
		if second != '[' && second != 'O' {
			return tuiKeyUnknown, nil
		}
		third, thirdErr := reader.ReadByte()
		if thirdErr != nil {
			return tuiKeyUnknown, thirdErr
		}
		switch third {
		case 'A':
			return tuiKeyUp, nil
		case 'B':
			return tuiKeyDown, nil
		case 'C':
			return tuiKeyRight, nil
		case 'D':
			return tuiKeyLeft, nil
		}
	}
	return tuiKeyUnknown, nil
}

func sanitizeTerminalText(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return '�'
		default:
			return r
		}
	}, value)
}

func truncateText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func humanSize(size int64) string {
	const unit = int64(1024)
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, suffix := range units {
		value /= float64(unit)
		if value < float64(unit) || suffix == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%d B", size)
}

func clampCursor(cursor, length int) int {
	if length <= 0 || cursor < 0 {
		return 0
	}
	if cursor >= length {
		return length - 1
	}
	return cursor
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
