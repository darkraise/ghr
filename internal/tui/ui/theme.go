// Package ui holds the ghr TUI's colour tokens, its form controls (button,
// toggle, select, stepper, text field, tag list) and the focus, form, layout
// and toast helpers its pages are built from.
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	ColGreen  = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	ColAmber  = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	ColRed    = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	ColDim    = lipgloss.AdaptiveColor{Light: "#6e7781", Dark: "#7d8590"}
	ColAccent = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	ColSelBg  = lipgloss.AdaptiveColor{Light: "#ddf4ff", Dark: "#1f2a37"}

	Green  = lipgloss.NewStyle().Foreground(ColGreen)
	Amber  = lipgloss.NewStyle().Foreground(ColAmber)
	Red    = lipgloss.NewStyle().Foreground(ColRed)
	Dim    = lipgloss.NewStyle().Foreground(ColDim)
	Bold   = lipgloss.NewStyle().Bold(true)
	Accent = lipgloss.NewStyle().Foreground(ColAccent).Bold(true)
	Sel    = lipgloss.NewStyle().Background(ColSelBg).Bold(true)
	Banner = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(ColRed).Bold(true).Padding(0, 1)
	Dialog = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColAccent).Padding(1, 2)
)

// Cell truncates s to w columns and pads it to exactly w.
func Cell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// Box draws a rounded box of total width w with title in the top border.
func Box(title string, w int, lines []string) string {
	if w < 8 {
		w = 8
	}
	dashes := w - 5 - ansi.StringWidth(title)
	if dashes < 1 {
		title = ansi.Truncate(title, w-6, "…")
		dashes = w - 5 - ansi.StringWidth(title)
	}
	var b strings.Builder
	b.WriteString(Dim.Render("╭─ ") + Bold.Render(title) + Dim.Render(" "+strings.Repeat("─", dashes)+"╮"))
	for _, l := range lines {
		b.WriteString("\n" + Dim.Render("│") + " " + Cell(l, w-4) + " " + Dim.Render("│"))
	}
	b.WriteString("\n" + Dim.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return b.String()
}
