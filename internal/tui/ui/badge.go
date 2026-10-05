package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
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
