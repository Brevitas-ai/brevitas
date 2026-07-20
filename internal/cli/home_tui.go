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

// waitForHome keeps interactive dashboard sessions alive after an action has
// finished, while leaving direct commands such as `bvx status` non-interactive.
func (a *App) waitForHome() (back bool, err error) {
	in, inOK := a.In.(*os.File)
	out, outOK := a.Out.(*os.File)
	if !inOK || !outOK || !canUseArrowNavigator(in, out) {
		return false, nil
	}

	fmt.Fprintf(out, "\n%s  [ Enter ]  Back to Home     [ q ]  Quit%s", ansiBold+ansiCyan, ansiReset)
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return false, fmt.Errorf("enable return-home input: %w", err)
	}
	defer func() {
		_ = term.Restore(int(in.Fd()), state)
		fmt.Fprint(out, ansiReset+"\r\n")
	}()

	return waitForHomeKey(bufio.NewReader(in))
}

func waitForHomeKey(reader *bufio.Reader) (bool, error) {
	for {
		key, shortcut, err := readHomeKey(reader)
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		switch key {
		case tuiKeyEnter, tuiKeyLeft, tuiKeyBack:
			return true, nil
		case tuiKeyQuit:
			return false, nil
		}
		switch shortcut {
		case 'b', 'h':
			return true, nil
		case 'q':
			return false, nil
		}
	}
}

func renderHomeActionScreen(out io.Writer) {
	// The launcher uses the alternate buffer. Once an action is selected it
	// returns to the normal buffer, which must be cleared so completed actions
	// never stack above the newly selected page.
	fmt.Fprint(out, "\x1b[H\x1b[2J")
}

func (a *App) promptHomeCommand() (args []string, quit bool, err error) {
	fmt.Fprintf(a.Out, "\n  %s%s◆ RUN A COMMAND%s\n", ansiPink, ansiBold, ansiReset)
	fmt.Fprintf(a.Out, "  %sType a command below; the `bvx` prefix is optional.%s\n", ansiDim, ansiReset)
	fmt.Fprintf(a.Out, "  %sBlank Enter returns Home  •  q quits%s\n\n", ansiDim, ansiReset)

	for {
		line, promptErr := a.prompt("  " + ansiCyan + ansiBold + "bvx › " + ansiReset)
		if promptErr != nil {
			return nil, false, promptErr
		}
		if strings.TrimSpace(line) == "q" {
			return nil, true, nil
		}
		args, parseErr := parseHomeCommandLine(line)
		if parseErr != nil {
			fmt.Fprintf(a.Out, "  %s✗ %v%s\n", ansiRed, parseErr, ansiReset)
			continue
		}
		return args, false, nil
	}
}

func parseHomeCommandLine(line string) ([]string, error) {
	var args []string
	var token strings.Builder
	var quote rune
	escaped, started := false, false

	flush := func() {
		if started {
			args = append(args, token.String())
			token.Reset()
			started = false
		}
	}
	for _, char := range strings.TrimSpace(line) {
		if escaped {
			token.WriteRune(char)
			started, escaped = true, false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped, started = true, true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				token.WriteRune(char)
			}
			started = true
			continue
		}
		switch {
		case char == '\'' || char == '"':
			quote, started = char, true
		case unicode.IsSpace(char):
			flush()
		default:
			token.WriteRune(char)
			started = true
		}
	}
	if escaped {
		return nil, fmt.Errorf("command ends with an unfinished escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("command has an unterminated quote")
	}
	flush()
	if len(args) > 0 && strings.EqualFold(args[0], "bvx") {
		args = args[1:]
	}
	return args, nil
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
		case tuiKeyQuit:
			return nil, true, nil
		case tuiKeyLeft, tuiKeyBack:
			continue
		}
		if shortcut != 0 {
			if action, ok := homeActionForShortcut(shortcut); ok {
				return append([]string(nil), action.args...), true, nil
			}
		}
	}
}

func renderHomeMenu(out io.Writer, cursor, width, height int) {
	if width >= homeBrandLogoWidth+2 && height >= 22 {
		renderHomeMenuWide(out, cursor, width, height)
		return
	}
	renderHomeMenuStacked(out, cursor, width, height)
}

func renderHomeMenuWide(out io.Writer, cursor, width, height int) {
	fmt.Fprint(out, "\x1b[H\x1b[2J")
	selected := homeActions[cursor]
	frameWidth := minInt(width-2, 104)
	leftWidth := maxInt(30, (frameWidth-3)*42/100)
	rightWidth := frameWidth - leftWidth - 3
	bodyHeight := minInt(maxInt(len(homeActions)+2, height-9), 16)

	for _, line := range homeBrandLogo(true) {
		writeHomeCentered(out, width, line, homeBrandLogoWidth)
	}

	topBorder := ansiBlue + "╭" + homePanelBorderTitle("ACTIONS", leftWidth) +
		"┬" + homePanelBorderTitle("SELECTED ACTION", rightWidth) + "╮" + ansiReset
	writeHomeCentered(out, width, topBorder, frameWidth)

	leftRows := make([]homePanelLine, bodyHeight)
	leftStart := maxInt(0, (bodyHeight-len(homeActions))/2)
	for index, action := range homeActions {
		leftRows[leftStart+index] = homePanelLine{
			value: formatHomeAction(action, index == cursor, leftWidth-2),
			width: leftWidth - 2,
		}
	}

	rightRows := []homePanelLine{
		{},
		{
			value: selected.color + ansiBold + selected.icon + "  " + selected.label + ansiReset,
			width: len([]rune(selected.icon)) + 2 + len([]rune(selected.label)),
		},
		{
			value: ansiCyan + ansiBold + selected.command + ansiReset,
			width: len([]rune(selected.command)),
		},
		{},
	}
	for _, line := range wrapTUIText(selected.description, rightWidth-4) {
		rightRows = append(rightRows, homePanelLine{value: ansiDim + line + ansiReset, width: len([]rune(line))})
	}
	rightRows = append(rightRows,
		homePanelLine{},
		homePanelLine{value: ansiBlue + ansiBold + "READY TO LAUNCH" + ansiReset, width: 15},
		homePanelLine{
			value: fmt.Sprintf("Press %sEnter%s or %s[%c]%s", ansiBold, ansiReset, ansiOrange, selected.shortcut, ansiReset),
			width: 18,
		},
	)
	rightStart := maxInt(0, (bodyHeight-len(rightRows))/2)

	for row := 0; row < bodyHeight; row++ {
		left := homePanelPad(leftRows[row], leftWidth)
		right := strings.Repeat(" ", rightWidth)
		if panelRow := row - rightStart; panelRow >= 0 && panelRow < len(rightRows) {
			right = homePanelPad(rightRows[panelRow], rightWidth)
		}
		line := ansiBlue + "│" + ansiReset + left + ansiBlue + "│" + ansiReset + right + ansiBlue + "│" + ansiReset
		writeHomeCentered(out, width, line, frameWidth)
	}

	bottomBorder := ansiBlue + "╰" + strings.Repeat("─", leftWidth) +
		"┴" + strings.Repeat("─", rightWidth) + "╯" + ansiReset
	writeHomeCentered(out, width, bottomBorder, frameWidth)

	footer := "↑/↓ navigate  •  Enter launch  •  actions return Home  •  q quit"
	writeHomeCenteredFinal(out, width, ansiDim+truncateText(footer, width-2)+ansiReset, minInt(len([]rune(footer)), width-2))
}

type homePanelLine struct {
	value string
	width int
}

func homePanelBorderTitle(title string, width int) string {
	prefix := "─ " + title + " "
	if len([]rune(prefix)) > width {
		return strings.Repeat("─", width)
	}
	return prefix + strings.Repeat("─", width-len([]rune(prefix)))
}

func homePanelPad(line homePanelLine, width int) string {
	contentWidth := maxInt(0, width-2)
	value := line.value
	visibleWidth := line.width
	if visibleWidth > contentWidth {
		value = truncateText(value, contentWidth)
		visibleWidth = contentWidth
	}
	return " " + value + strings.Repeat(" ", maxInt(0, contentWidth-visibleWidth)) + " "
}

func renderHomeMenuStacked(out io.Writer, cursor, width, height int) {
	fmt.Fprint(out, "\x1b[H\x1b[2J")
	fmt.Fprintf(out, "%s%s brevitas %s  %shome%s\r\n", ansiBold, ansiCyan, ansiReset, ansiDim, ansiReset)
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
	fmt.Fprintf(out, "\r\n%s↑/↓ navigate  Enter launch  actions return Home  q quit%s", ansiDim, ansiReset)
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
