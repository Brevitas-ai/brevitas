package cli

import (
	"fmt"
	"io"
	"strings"
)

const (
	ansiBrandBlue          = "\x1b[38;2;8;94;255m"
	ansiBrandBlueBG        = "\x1b[48;2;8;94;255m"
	ansiBrandShadow        = "\x1b[38;2;0;38;108m"
	ansiBrandShadowBG      = "\x1b[48;2;0;38;108m"
	ansiBrandWhite         = "\x1b[97m"
	homeBrandIconMaskWidth = 13
	homeBrandIconWidth     = homeBrandIconMaskWidth + 1
	homeBrandWordWidth     = 61
	homeBrandLogoWidth     = homeBrandIconWidth + 2 + homeBrandWordWidth
)

// homeBrandIconMask keeps twice as much vertical detail as the rendered icon.
// Each pair of mask rows is combined with upper/lower half-block characters.
var homeBrandIconMask = []string{
	"██         ██",
	"███        ██",
	" ██   █   ███",
	" ███ ███ ███ ",
	"  █████████  ",
	"   ███████   ",
	"   ███████   ",
	"  █████████  ",
	"  ██ ███ ███ ",
	" ███  █   ███",
	"███       ███",
	"██         ██",
}

// homeBrandArt uses a crisp six-row block face so the name stays legible at
// normal terminal sizes without depending on a particular font or glyph set.
var homeBrandArt = []string{
	"██████╗ ██████╗ ███████╗██╗   ██╗██╗████████╗ █████╗ ███████╗",
	"██╔══██╗██╔══██╗██╔════╝██║   ██║██║╚══██╔══╝██╔══██╗██╔════╝",
	"██████╔╝██████╔╝█████╗  ██║   ██║██║   ██║   ███████║███████╗",
	"██╔══██╗██╔══██╗██╔══╝  ╚██╗ ██╔╝██║   ██║   ██╔══██║╚════██║",
	"██████╔╝██║  ██║███████╗ ╚████╔╝ ██║   ██║   ██║  ██║███████║",
	"╚═════╝ ╚═╝  ╚═╝╚══════╝  ╚═══╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝╚══════╝",
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
	for _, line := range homeBrandLogo(color) {
		fmt.Fprintf(out, "  %s\n", line)
	}
	fmt.Fprintf(out, "\n  %sOptimize every AI request without changing how you work.%s\n\n", p.dim, p.reset)
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

func homeBrandLogo(color bool) []string {
	lines := make([]string, 0, len(homeBrandArt))
	for row, line := range homeBrandArt {
		icon := homeBrandIconRow(homeBrandIconMask[row*2], homeBrandIconMask[row*2+1], color)
		if color {
			line = ansiBrandWhite + ansiBold + line + ansiReset
		}
		lines = append(lines, icon+"  "+line)
	}
	return lines
}

func homeBrandIconRow(top, bottom string, color bool) string {
	var line strings.Builder
	topRunes, bottomRunes := []rune(top), []rune(bottom)
	if color {
		line.WriteString(ansiBold)
	}
	for column := 0; column < homeBrandIconWidth; column++ {
		topPixel := homeBrandIconPixel(topRunes, column)
		bottomPixel := homeBrandIconPixel(bottomRunes, column)
		line.WriteString(homeBrandIconCell(topPixel, bottomPixel, color))
	}
	if color {
		line.WriteString(ansiReset)
	}
	return line.String()
}

func homeBrandIconPixel(mask []rune, column int) byte {
	if column < homeBrandIconMaskWidth && mask[column] != ' ' {
		return 'B'
	}
	if column > 0 && column-1 < homeBrandIconMaskWidth && mask[column-1] != ' ' {
		return 'S'
	}
	return ' '
}

func homeBrandIconCell(top, bottom byte, color bool) string {
	if top == ' ' && bottom == ' ' {
		return " "
	}
	if !color {
		switch {
		case top == ' ':
			return "▄"
		case bottom == ' ':
			return "▀"
		default:
			return "█"
		}
	}
	if top == bottom {
		return homeBrandIconForeground(top) + "█" + ansiReset + ansiBold
	}
	if top == ' ' {
		return homeBrandIconForeground(bottom) + "▄" + ansiReset + ansiBold
	}
	if bottom == ' ' {
		return homeBrandIconForeground(top) + "▀" + ansiReset + ansiBold
	}
	return homeBrandIconForeground(top) + homeBrandIconBackground(bottom) + "▀" + ansiReset + ansiBold
}

func homeBrandIconForeground(pixel byte) string {
	if pixel == 'B' {
		return ansiBrandBlue
	}
	return ansiBrandShadow
}

func homeBrandIconBackground(pixel byte) string {
	if pixel == 'B' {
		return ansiBrandBlueBG
	}
	return ansiBrandShadowBG
}
