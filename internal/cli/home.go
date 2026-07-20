package cli

import (
	"fmt"
	"io"
)

var homeLogo = []string{
	"██████╗ ██╗   ██╗██╗  ██╗",
	"██╔══██╗██║   ██║╚██╗██╔╝",
	"██████╔╝██║   ██║ ╚███╔╝ ",
	"██╔══██╗╚██╗ ██╔╝ ██╔██╗ ",
	"██████╔╝ ╚████╔╝ ██╔╝ ██╗",
	"╚═════╝   ╚═══╝  ╚═╝  ╚═╝",
}

type homePalette struct {
	reset, bold, dim, blue, cyan, green, orange, pink string
}

func (a *App) home() {
	renderHome(a.Out, colorEnabled(a.Out))
}

func renderHome(out io.Writer, color bool) {
	p := homePalette{}
	if color {
		p = homePalette{
			reset: ansiReset, bold: ansiBold, dim: ansiDim, blue: ansiBlue,
			cyan: ansiCyan, green: ansiGreen, orange: ansiOrange, pink: ansiPink,
		}
	}

	fmt.Fprintln(out)
	for index, line := range homeLogo {
		accent := p.blue
		if index >= len(homeLogo)/2 {
			accent = p.cyan
		}
		fmt.Fprintf(out, "  %s%s%s%s\n", p.bold, accent, line, p.reset)
	}
	fmt.Fprintf(out, "\n  %s%sBREVITAS%s  %sOptimize every AI request without changing how you work.%s\n\n", p.bold, p.pink, p.reset, p.dim, p.reset)
	fmt.Fprintf(out, "  %s%sSTART HERE%s\n", p.bold, p.pink, p.reset)
	homeOption(out, p, p.green, "bvx install repo", "Connect a repository")
	homeOption(out, p, p.cyan, "bvx install ai", "Configure detected AI tools")

	fmt.Fprintf(out, "\n  %s%sEXPLORE%s\n", p.bold, p.blue, p.reset)
	homeOption(out, p, p.orange, "bvx status", "Check services and integrations")
	homeOption(out, p, p.pink, "bvx stats", "See tokens and cost saved")
	homeOption(out, p, p.cyan, "bvx help", "Explore every command")
	fmt.Fprintf(out, "\n%s  New here? Start with `bvx install repo`.%s\n\n", p.dim, p.reset)
}

func homeOption(out io.Writer, p homePalette, color, command, description string) {
	fmt.Fprintf(out, "    %s◆%s  %-20s %s\n", color, p.reset, command, description)
}
