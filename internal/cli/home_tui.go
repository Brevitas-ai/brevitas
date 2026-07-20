package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"golang.org/x/term"
)

type homeAction struct {
	label       string
	command     string
	description string
	args        []string
	icon        string
	color       string
	shortcut    rune
}

var homeActions = []homeAction{
	{
		label: "Connect repository", command: "bvx install repo",
		description: "Browse to a codebase, authenticate, scan it, and connect it to Brevitas.",
		args:        []string{"install", "repo"}, icon: "▸", color: ansiCyan, shortcut: 'r',
	},
	{
		label: "Configure AI tools", command: "bvx install ai",
		description: "Detect supported AI clients and route them through the local Brevitas proxy.",
		args:        []string{"install", "ai"}, icon: "◆", color: ansiPink, shortcut: 'a',
	},
	{
		label: "System status", command: "bvx status",
		description: "Inspect services, Keychain authentication, optimizer health, and configured tools.",
		args:        []string{"status"}, icon: "●", color: ansiGreen, shortcut: 's',
	},
	{
		label: "Savings dashboard", command: "bvx stats",
		description: "See local request, caching, token, and verified savings metrics.",
		args:        []string{"stats"}, icon: "▲", color: ansiOrange, shortcut: 'v',
	},
	{
		label: "Run diagnostics", command: "bvx doctor",
		description: "Check the full installation and surface anything that needs attention.",
		args:        []string{"doctor"}, icon: "✦", color: ansiPurple, shortcut: 'd',
	},
	{
		label: "AI tool compatibility", command: "bvx providers",
		description: "Review every supported AI tool and its detection or configuration state.",
		args:        []string{"providers"}, icon: "◇", color: ansiBlue, shortcut: 'p',
	},
	{
		label: "Connect account", command: "bvx login",
		description: "Authorize this computer and store a revocable key in the OS credential manager.",
		args:        []string{"login"}, icon: "●", color: ansiTeal, shortcut: 'l',
	},
	{
		label: "Command reference", command: "bvx help",
		description: "Open the complete Brevitas command reference.",
		args:        []string{"help"}, icon: "?", color: ansiYellow, shortcut: 'h',
	},
	{
		label: "Quit", command: "q",
		description: "Leave Brevitas and return to your shell.",
		icon:        "×", color: ansiRed, shortcut: 'q',
	},
}

// chooseHomeAction opens the full-screen launcher when stdin and stdout are
// terminals. handled is false only when the static, pipe-friendly home should
// be used instead.
func (a *App) chooseHomeAction() (args []string, handled bool, err error) {
	in, inOK := a.In.(*os.File)
	out, outOK := a.Out.(*os.File)
	if !inOK || !outOK || !canUseArrowNavigator(in, out) {
		return nil, false, nil
	}

	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, true, fmt.Errorf("enable command-center input: %w", err)
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
	return homeMenuWithKeys(bufio.NewReader(in), out, size)
}

func homeMenuWithKeys(reader *bufio.Reader, out io.Writer, size func() (int, int)) ([]string, bool, error) {
	cursor := 0
	for {
		width, height := size()
		renderHomeMenu(out, cursor, width, height)
		key, shortcut, err := readHomeKey(reader)
		if errors.Is(err, io.EOF) {
			return nil, true, nil
		}
		if err != nil {
			return nil, true, err
		}
		switch key {
		case tuiKeyUp:
			cursor = (cursor - 1 + len(homeActions)) % len(homeActions)
		case tuiKeyDown:
			cursor = (cursor + 1) % len(homeActions)
		case tuiKeyEnter, tuiKeyRight:
			return append([]string(nil), homeActions[cursor].args...), true, nil
		case tuiKeyQuit, tuiKeyLeft, tuiKeyBack:
			return nil, true, nil
		}
		if shortcut != 0 {
			if action, ok := homeActionForShortcut(shortcut); ok {
				return append([]string(nil), action.args...), true, nil
			}
		}
	}
}

func renderHomeMenu(out io.Writer, cursor, width, height int) {
	if width >= 52 && height >= 22 {
		renderHomeMenuWide(out, cursor, width, height)
		return
	}
	renderHomeMenuStacked(out, cursor, width, height)
}

func renderHomeMenuWide(out io.Writer, cursor, width, height int) {
	fmt.Fprint(out, "\x1b[H\x1b[2J")
	selected := homeActions[cursor]
	description := []string{}
	if height >= 24 {
		description = wrapTUIText(selected.description, minInt(64, width-4))
	}
	usedHeight := 22 + len(description)
	for row := 0; row < maxInt(0, (height-usedHeight)/2); row++ {
		fmt.Fprint(out, "\r\n")
	}
	for index, line := range homeLogo {
		color := ansiBlue
		if index >= len(homeLogo)/2 {
			color = ansiCyan
		}
		writeHomeCentered(out, width, color+ansiBold+line+ansiReset, len([]rune(line)))
	}
	writeHomeCentered(out, width, ansiPink+ansiBold+"BREVITAS"+ansiReset+ansiDim+"  Optimize every AI request."+ansiReset, 35)
	writeHomeCentered(out, width, ansiBlue+ansiBold+"START HERE"+ansiReset, 10)

	menuWidth := minInt(46, width-4)
	for index, action := range homeActions {
		if index == 2 {
			writeHomeCentered(out, width, ansiBlue+ansiBold+"EXPLORE"+ansiReset, 7)
		}
		row := formatHomeAction(action, index == cursor, menuWidth)
		writeHomeCentered(out, width, row, menuWidth)
	}

	fmt.Fprint(out, "\r\n")
	writeHomeCentered(out, width, selected.color+ansiBold+selected.command+ansiReset, len([]rune(selected.command)))
	for _, line := range description {
		writeHomeCentered(out, width, ansiDim+line+ansiReset, len([]rune(line)))
	}
	fmt.Fprint(out, "\r\n")
	footer := "↑/↓ navigate  Enter launch  shortcut keys shown at right  q quit"
	writeHomeCenteredFinal(out, width, ansiDim+truncateText(footer, width-2)+ansiReset, minInt(len([]rune(footer)), width-2))
}

func renderHomeMenuStacked(out io.Writer, cursor, width, height int) {
	fmt.Fprint(out, "\x1b[H\x1b[2J")
	fmt.Fprintf(out, "%s%s BVX %s  %sBrevitas home%s\r\n", ansiBold, ansiCyan, ansiReset, ansiDim, ansiReset)
	fmt.Fprintf(out, "%sStart here: choose what you want to set up or inspect.%s\r\n\r\n", ansiDim, ansiReset)
	visible := maxInt(4, height-9)
	start := 0
	if cursor >= visible {
		start = cursor - visible + 1
	}
	end := minInt(len(homeActions), start+visible)
	for index := start; index < end; index++ {
		fmt.Fprintf(out, "%s\r\n", formatHomeAction(homeActions[index], index == cursor, width-1))
	}
	for row := end - start; row < visible; row++ {
		fmt.Fprint(out, "\r\n")
	}
	fmt.Fprintf(out, "\r\n%s%s%s\r\n", homeActions[cursor].color+ansiBold, truncateText(homeActions[cursor].command, width-1), ansiReset)
	fmt.Fprintf(out, "%s%s%s\r\n", ansiDim, truncateText(homeActions[cursor].description, width-1), ansiReset)
	fmt.Fprintf(out, "\r\n%s↑/↓ navigate  Enter launch  press a shortcut to launch  q quit%s", ansiDim, ansiReset)
}

func formatHomeAction(action homeAction, selected bool, width int) string {
	if width < 12 {
		return truncateText(action.label, width)
	}
	prefix, style := "  ", ""
	if selected {
		prefix, style = "› ", ansiSelect
	}
	labelWidth := maxInt(4, width-8)
	label := truncateText(action.label, labelWidth)
	return fmt.Sprintf("%s%s%s%s %-*s %s[%c]%s", style, prefix, action.color, action.icon, labelWidth, label, ansiOrange, action.shortcut, ansiReset)
}

func writeHomeCentered(out io.Writer, width int, value string, visibleWidth int) {
	column := maxInt(1, (width-visibleWidth)/2+1)
	fmt.Fprintf(out, "\x1b[%dG%s\x1b[K\r\n", column, value)
}

func writeHomeCenteredFinal(out io.Writer, width int, value string, visibleWidth int) {
	column := maxInt(1, (width-visibleWidth)/2+1)
	fmt.Fprintf(out, "\x1b[%dG%s\x1b[K", column, value)
}

func homeActionForShortcut(shortcut rune) (homeAction, bool) {
	shortcut = unicode.ToLower(shortcut)
	for _, action := range homeActions {
		if action.shortcut == shortcut {
			return action, true
		}
	}
	return homeAction{}, false
}

func readHomeKey(reader *bufio.Reader) (tuiKey, rune, error) {
	b, err := reader.ReadByte()
	if err != nil {
		return tuiKeyUnknown, 0, err
	}
	switch b {
	case '\r', '\n':
		return tuiKeyEnter, 0, nil
	case 3:
		return tuiKeyQuit, 0, nil
	case 8, 127:
		return tuiKeyBack, 0, nil
	case 27:
		second, secondErr := reader.ReadByte()
		if secondErr != nil {
			return tuiKeyUnknown, 0, secondErr
		}
		if second != '[' && second != 'O' {
			return tuiKeyUnknown, 0, nil
		}
		third, thirdErr := reader.ReadByte()
		if thirdErr != nil {
			return tuiKeyUnknown, 0, thirdErr
		}
		switch third {
		case 'A':
			return tuiKeyUp, 0, nil
		case 'B':
			return tuiKeyDown, 0, nil
		case 'C':
			return tuiKeyRight, 0, nil
		case 'D':
			return tuiKeyLeft, 0, nil
		}
	}
	if unicode.IsLetter(rune(b)) {
		return tuiKeyUnknown, unicode.ToLower(rune(b)), nil
	}
	return tuiKeyUnknown, 0, nil
}

func wrapTUIText(value string, width int) []string {
	if width < 10 {
		return []string{truncateText(value, width)}
	}
	words := strings.Fields(value)
	lines := []string{}
	line := ""
	for _, word := range words {
		if line == "" {
			line = word
			continue
		}
		if len([]rune(line))+1+len([]rune(word)) <= width {
			line += " " + word
			continue
		}
		lines = append(lines, line)
		line = word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
