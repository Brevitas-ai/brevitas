package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

type homeAction struct {
	label       string
	command     string
	description string
	details     []string
	args        []string
	icon        string
	color       string
}

var homeActions = []homeAction{
	{
		label: "Connect repository", command: "bvx install repo",
		description: "Browse to a codebase, authenticate, scan it, and connect it to Brevitas.",
		details:     []string{"Full-screen directory browser", "Safe API-key authorization", "Repository usage appears in your dashboard"},
		args:        []string{"install", "repo"}, icon: "▸", color: ansiCyan,
	},
	{
		label: "Configure AI tools", command: "bvx install ai",
		description: "Detect supported AI clients and route them through the local Brevitas proxy.",
		details:     []string{"Claude Code, Codex, Continue, Aider, and more", "Backs up every changed configuration", "Starts the proxy and optimizer services"},
		args:        []string{"install", "ai"}, icon: "◆", color: ansiPink,
	},
	{
		label: "System status", command: "bvx status",
		description: "Inspect services, Keychain authentication, optimizer health, and configured tools.",
		details:     []string{"Proxy and optimizer state", "Secure key availability", "Configured provider summary"},
		args:        []string{"status"}, icon: "●", color: ansiGreen,
	},
	{
		label: "Savings dashboard", command: "bvx stats",
		description: "See local request, caching, token, and verified savings metrics.",
		details:     []string{"Lossless cache activity", "Brevitas-attributed savings", "Prompt compression metrics"},
		args:        []string{"stats"}, icon: "▲", color: ansiOrange,
	},
	{
		label: "Run diagnostics", command: "bvx doctor",
		description: "Check the full installation and surface anything that needs attention.",
		details:     []string{"Service and proxy checks", "Network and engine checks", "AI-tool configuration validation"},
		args:        []string{"doctor"}, icon: "✦", color: ansiPurple,
	},
	{
		label: "AI tool compatibility", command: "bvx providers",
		description: "Review every supported AI tool and its detection or configuration state.",
		details:     []string{"Full and partial support", "Detected tools", "Manual configuration notes"},
		args:        []string{"providers"}, icon: "◇", color: ansiBlue,
	},
	{
		label: "Connect account", command: "bvx login",
		description: "Authorize this computer and store a revocable key in the OS credential manager.",
		details:     []string{"Browser-based approval", "No plaintext key files", "macOS Keychain / native credential store"},
		args:        []string{"login"}, icon: "●", color: ansiTeal,
	},
	{
		label: "Command reference", command: "bvx help",
		description: "Open the complete Brevitas command reference.",
		details:     []string{"Install and lifecycle commands", "Configuration commands", "Command-specific help"},
		args:        []string{"help"}, icon: "?", color: ansiYellow,
	},
	{
		label: "Quit", command: "q",
		description: "Leave Brevitas and return to your shell.",
		details:     []string{"No changes will be made"},
		icon:        "×", color: ansiRed,
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
		key, err := readTUIKey(reader)
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
	}
}

func renderHomeMenu(out io.Writer, cursor, width, height int) {
	if width >= 76 && height >= 18 {
		renderHomeMenuWide(out, cursor, width, height)
		return
	}
	renderHomeMenuStacked(out, cursor, width, height)
}

func renderHomeMenuWide(out io.Writer, cursor, width, height int) {
	fmt.Fprint(out, "\x1b[H\x1b[2J")
	fmt.Fprintf(out, "%s%s BREVITAS %s\x1b[K\r\n", ansiBold, ansiCyan, ansiReset)
	fmt.Fprintf(out, "%sChoose what you want Brevitas to do%s\x1b[K\r\n", ansiDim, ansiReset)
	fmt.Fprintf(out, "%s%s%s\r\n", ansiBlue, strings.Repeat("─", width), ansiReset)

	leftWidth := width * 42 / 100
	if leftWidth < 30 {
		leftWidth = 30
	}
	if leftWidth > 52 {
		leftWidth = 52
	}
	rightWidth := maxInt(20, width-leftWidth-3)
	contentHeight := maxInt(10, height-5)
	entryRows := contentHeight - 1
	start := 0
	if cursor >= entryRows {
		start = cursor - entryRows + 1
	}
	end := minInt(len(homeActions), start+entryRows)
	preview := homePreviewLines(homeActions[cursor], rightWidth, contentHeight-1)

	for row := 0; row < contentHeight; row++ {
		left, right := "", ""
		if row == 0 {
			left = ansiBold + ansiBlue + " ACTIONS" + ansiReset
			right = ansiBold + ansiMagenta + " PREVIEW" + ansiReset
		} else {
			index := start + row - 1
			if index < end {
				left = formatHomeAction(homeActions[index], index == cursor, leftWidth-1)
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
	footer := "↑/↓ navigate  Enter/→ launch  ←/Backspace/q quit"
	fmt.Fprintf(out, "%s%s%s\x1b[K", ansiDim, truncateText(footer, width-1), ansiReset)
}

func renderHomeMenuStacked(out io.Writer, cursor, width, height int) {
	fmt.Fprint(out, "\x1b[H\x1b[2J")
	fmt.Fprintf(out, "%s%s BREVITAS %s\r\n", ansiBold, ansiCyan, ansiReset)
	fmt.Fprintf(out, "%sChoose what you want Brevitas to do%s\r\n\r\n", ansiDim, ansiReset)
	visible := maxInt(5, height-12)
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
	fmt.Fprintf(out, "\r\n%s%s%s\r\n", ansiMagenta+ansiBold, truncateText(homeActions[cursor].command, width-1), ansiReset)
	fmt.Fprintf(out, "%s%s%s\r\n", ansiDim, truncateText(homeActions[cursor].description, width-1), ansiReset)
	fmt.Fprintf(out, "\r\n%s↑/↓ navigate  Enter launch  q quit%s", ansiDim, ansiReset)
}

func formatHomeAction(action homeAction, selected bool, width int) string {
	prefix, style := "  ", ""
	if selected {
		prefix, style = "> ", ansiSelect
	}
	label := truncateText(action.label, maxInt(10, width-7))
	return fmt.Sprintf("%s%s%s%s %s%s", style, prefix, action.color, action.icon, label, ansiReset)
}

func homePreviewLines(action homeAction, width, height int) []string {
	lines := []string{
		action.color + action.icon + " " + truncateText(action.label, maxInt(1, width-3)) + ansiReset,
		ansiCyan + ansiBold + truncateText(action.command, width) + ansiReset,
		"",
	}
	lines = append(lines, wrapTUIText(action.description, width)...)
	lines = append(lines, "", ansiBold+" WHAT HAPPENS"+ansiReset)
	for _, detail := range action.details {
		lines = append(lines, ansiGreen+"✓ "+ansiReset+truncateText(detail, maxInt(1, width-2)))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
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
		lines = append(lines, ansiDim+line+ansiReset)
		line = word
	}
	if line != "" {
		lines = append(lines, ansiDim+line+ansiReset)
	}
	return lines
}
