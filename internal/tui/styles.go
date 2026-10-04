package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	colGreen  = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	colAmber  = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	colRed    = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	colDim    = lipgloss.AdaptiveColor{Light: "#6e7781", Dark: "#7d8590"}
	colAccent = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	colSelBg  = lipgloss.AdaptiveColor{Light: "#ddf4ff", Dark: "#1f2a37"}

	sGreen  = lipgloss.NewStyle().Foreground(colGreen)
	sAmber  = lipgloss.NewStyle().Foreground(colAmber)
	sRed    = lipgloss.NewStyle().Foreground(colRed)
	sDim    = lipgloss.NewStyle().Foreground(colDim)
	sBold   = lipgloss.NewStyle().Bold(true)
	sAccent = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	sSel    = lipgloss.NewStyle().Background(colSelBg).Bold(true)
	sBanner = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(colRed).Bold(true).Padding(0, 1)
	sDialog = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).Padding(1, 2)
)

// cell truncates s to w columns and pads it to exactly w.
func cell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// box draws a rounded box of total width w with title in the top border.
func box(title string, w int, lines []string) string {
	if w < 8 {
		w = 8
	}
	dashes := w - 5 - ansi.StringWidth(title)
	if dashes < 1 {
		title = ansi.Truncate(title, w-6, "…")
		dashes = w - 5 - ansi.StringWidth(title)
	}
	var b strings.Builder
	b.WriteString(sDim.Render("╭─ ") + sBold.Render(title) + sDim.Render(" "+strings.Repeat("─", dashes)+"╮"))
	for _, l := range lines {
		b.WriteString("\n" + sDim.Render("│") + " " + cell(l, w-4) + " " + sDim.Render("│"))
	}
	b.WriteString("\n" + sDim.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return b.String()
}

func stateStyle(state string) lipgloss.Style {
	switch state {
	case "busy", "active", "success", "running":
		return sGreen
	case "starting", "idle", "paused", "removing", "queued", "waiting", "cancelled", "warn":
		return sAmber
	case "failure", "error", "cleaning":
		return sRed
	}
	return sDim
}

func eventStyle(level string) (string, lipgloss.Style) {
	switch level {
	case "ok":
		return "✔", sGreen
	case "warn":
		return "⚠", sAmber
	case "error":
		return "✖", sRed
	}
	return "▶", sAccent
}

var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

func gauge(used, total, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}
	filled := used * width / total
	if filled > width {
		filled = width
	}
	return "▕" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "▏"
}
