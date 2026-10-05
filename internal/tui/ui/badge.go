package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

type BadgeKind int

const (
	BadgeOK    BadgeKind = iota // active, online, success, matched, ok
	BadgeBusy                   // busy
	BadgeWarn                   // paused, idle, starting, expires soon, unverified
	BadgeBad                    // error, failure, offline, unmatched, rejected
	BadgeMuted                  // removing, cleaning, ghr, other, cancelled, unknown
)

var (
	black = lipgloss.Color("#000000")
	white = lipgloss.Color("#ffffff")
)

var badgeStyles = map[BadgeKind]lipgloss.Style{
	BadgeOK:    lipgloss.NewStyle().Bold(true).Foreground(black).Background(ColGreen),
	BadgeBusy:  lipgloss.NewStyle().Bold(true).Foreground(white).Background(ColAccent),
	BadgeWarn:  lipgloss.NewStyle().Bold(true).Foreground(black).Background(ColAmber),
	BadgeBad:   lipgloss.NewStyle().Bold(true).Foreground(white).Background(ColRed),
	BadgeMuted: lipgloss.NewStyle().Bold(true).Foreground(white).Background(ColDim),
}

// Badge renders a state as " TEXT " on a coloured block. Without colour it
// renders "[TEXT]", the same width, so layouts do not move.
func Badge(text string, kind BadgeKind) string {
	text = strings.ToUpper(text)
	if lipgloss.ColorProfile() == termenv.Ascii {
		return "[" + text + "]"
	}
	return badgeStyles[kind].Render(" " + text + " ")
}

// BadgeRow renders a table row exactly w columns wide: before, the badge,
// then after. A selected row gets sel on its text while the badge keeps its
// own colours. When the row is too narrow, after gives way first, then
// before is cut to leave room for the badge; only a row narrower than the
// badge itself cuts the badge.
func BadgeRow(sel lipgloss.Style, selected bool, before, badge, after string, w int) string {
	bw := ansi.StringWidth(badge)
	if bw >= w {
		return Cell(badge, w)
	}
	if ansi.StringWidth(before) > w-bw {
		before = Cell(before, w-bw)
	}
	after = Cell(after, w-bw-ansi.StringWidth(before))
	if !selected {
		return before + badge + after
	}
	return sel.Render(ansi.Strip(before)) + badge + sel.Render(ansi.Strip(after))
}
