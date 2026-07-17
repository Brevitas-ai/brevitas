package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

func colorEnabled(out io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" || os.Getenv("CLICOLOR_FORCE") != "" {
		return true
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func (a *App) styled(style, value string) string {
	if !colorEnabled(a.Out) || style == "" {
		return value
	}
	return style + value + ansiReset
}

func (a *App) page(title, subtitle string) {
	line := strings.Repeat("─", 52)
	fmt.Fprintf(a.Out, "\n  %s %s\n", a.styled(ansiCyan+ansiBold, "BVX"), a.styled(ansiBold, title))
	fmt.Fprintf(a.Out, "  %s\n", a.styled(ansiBlue, line))
	if subtitle != "" {
		fmt.Fprintf(a.Out, "  %s\n", a.styled(ansiDim, subtitle))
	}
}

func (a *App) section(title string) {
	fmt.Fprintf(a.Out, "\n  %s %s\n", a.styled(ansiPink+ansiBold, "◆"), a.styled(ansiBold, strings.ToUpper(title)))
}

func (a *App) note(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(a.Out, "  %s %s\n", a.styled(ansiBlue, "→"), a.styled(ansiDim, message))
}

func (a *App) command(command, description string) {
	padded := fmt.Sprintf("%-28s", command)
	fmt.Fprintf(a.Out, "    %s %s\n", a.styled(ansiCyan+ansiBold, padded), a.styled(ansiDim, description))
}

func (a *App) metric(label, value string, accent string) {
	fmt.Fprintf(a.Out, "    %-28s %s\n", label, a.styled(accent+ansiBold, value))
}

func (a *App) success(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(a.Out, "\n  %s %s\n", a.styled(ansiGreen+ansiBold, "✓"), a.styled(ansiBold, message))
}
