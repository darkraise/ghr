package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/tui/ui"
)

// The colour tokens live in package ui; these short names keep the page code readable.
var (
	sGreen  = ui.Green
	sAmber  = ui.Amber
	sRed    = ui.Red
	sDim    = ui.Dim
	sBold   = ui.Bold
	sAccent = ui.Accent
	sSel    = ui.Sel
	sBanner = ui.Banner
	sDialog = ui.Dialog
)

func cell(s string, w int) string { return ui.Cell(s, w) }

func box(title string, w int, lines []string) string { return ui.Box(title, w, lines) }

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
