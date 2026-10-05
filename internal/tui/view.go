package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/tui/ui"
)

func dur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h, mi, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, mi)
	}
	return fmt.Sprintf("%dm%02ds", mi, s)
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func maxText(n int) string {
	if n == 0 {
		return "∞"
	}
	return fmt.Sprint(n)
}

func (m Model) View() string {
	w := max(m.width, 40)
	out := m.layout(w, m.pageBody)
	if m.overlay != ovNone {
		out = m.withOverlay(out, w)
	}
	return zone.Scan(out)
}

// pageBody renders the current page's content in w columns and h lines.
func (m Model) pageBody(w, h int) string {
	switch m.page {
	case pageRunners:
		return m.runnersPage(w, h)
	case pageHistory:
		return m.historyPage(w, h)
	case pageSettings:
		return m.settingsView(w, h)
	case pageDetail:
		return m.detailPage(w, h)
	}
	return m.dashboard(w, h)
}

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || (unicode.IsControl(r) && r != '\n' && r != '\t') {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func (m Model) row(id string, selected bool, s string, w int) string {
	s = cell(s, w)
	if selected {
		s = sSel.Render(ansi.Strip(s))
	}
	return zone.Mark(id, s)
}

// The selected row's buttons. Only the focused card's selected row shows
// them, so each zone appears once per frame.
const (
	rowPause  = "row/pause"
	rowRemove = "row/remove"
	rowLogs   = "row/logs"
	rowStop   = "row/stop"
)

// rowButtons renders buttons for the right end of a selected row.
func rowButtons(buttons ...*ui.Button) string {
	var parts []string
	for _, b := range buttons {
		parts = append(parts, b.View(false, 0))
	}
	return strings.Join(parts, "")
}

// selectedRow renders a selected row of width w: the text before the badge,
// the badge (kept in colour), the text after it, the row's buttons, then
// tail, the row's fixed right-hand columns, which keep their place under
// the header. The text gives way to the buttons, so its last column should
// be the flexible one.
func (m Model) selectedRow(id, before, badge, after, buttons, tail string, w int) string {
	bw, tw := ansi.StringWidth(buttons), ansi.StringWidth(tail)
	return zone.Mark(id, ui.BadgeRow(sSel, true, before, badge, after, w-bw-tw)) + buttons + sSel.Render(ansi.Strip(tail))
}

// stateBadge is the badge for a repo, runner, job or registration state.
func stateBadge(state string) string {
	kind := ui.BadgeMuted
	switch state {
	case "active", "online", "success", "matched", "ok":
		kind = ui.BadgeOK
	case "busy":
		kind = ui.BadgeBusy
	case "paused", "idle", "starting", "expires soon", "unverified":
		kind = ui.BadgeWarn
	case "error", "failure", "offline", "unmatched", "rejected":
		kind = ui.BadgeBad
	}
	return ui.Badge(state, kind)
}

// fit keeps the first hdr lines and scrolls the rest so that body line sel
// stays visible within n lines.
func fit(lines []string, hdr, sel, n int) []string {
	if len(lines) <= n {
		return lines
	}
	body, vis := lines[hdr:], max(n-hdr, 1)
	start := min(max(sel-vis+1, 0), len(body)-vis)
	return append(lines[:hdr:hdr], body[start:start+vis]...)
}
