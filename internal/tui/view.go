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
		return m.historyTab(w, h)
	case pageSettings:
		return m.settingsView(w, h)
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

// selectedRow renders a selected row of width w: the text s, the row's
// buttons, then tail, the row's fixed right-hand columns, which keep their
// place under the header. s gives way to the buttons, so its last column
// should be the flexible one.
func (m Model) selectedRow(id, s, buttons, tail string, w int) string {
	bw, tw := ansi.StringWidth(buttons), ansi.StringWidth(tail)
	return zone.Mark(id, sSel.Render(ansi.Strip(cell(s, w-bw-tw)))) + buttons + sSel.Render(ansi.Strip(tail))
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

func (m Model) historyTab(w, h int) string {
	inner := w - 4
	filter := "repo: all"
	if m.histRepo != "" {
		filter = "repo: " + m.histRepo
	}
	if m.histConcl != "" {
		filter += "  result: " + m.histConcl
	} else {
		filter += "  result: all"
	}
	lines := []string{
		sDim.Render(filter + "   (r repo, c result, enter copies run URL)"),
		sDim.Render(fmt.Sprintf("  %-16s %-12s %-7s %-*s %-10s %s", "FINISHED", "REPO", "RUN", inner-62, "JOB", "RESULT", "DURATION")),
	}
	visible := h - 4
	start := 0
	if m.histSel >= visible {
		start = m.histSel - visible + 1
	}
	for i := start; i < len(m.hist) && i < start+visible; i++ {
		e := m.hist[i]
		line := "  " + cell(e.FinishedAt.Local().Format("2006-01-02 15:04"), 16) + " " + cell(e.Repo, 12) + " " +
			cell("#"+e.RunNumber, 7) + " " + cell(e.JobName, inner-62) + " " + stateStyle(e.Conclusion).Render(cell(e.Conclusion, 10)) + " " +
			dur(e.FinishedAt.Sub(e.StartedAt))
		lines = append(lines, m.row(fmt.Sprintf("hist-%d", i), i == m.histSel, line, inner))
	}
	if len(m.hist) == 0 {
		lines = append(lines, sDim.Render("  no finished jobs yet"))
	}
	return box("History", w, lines)
}

func (m Model) detailBody() string {
	r := m.instance(m.detailID)
	if r == nil {
		return "runner " + m.detailID + " has finished\n\n" + buttons("", "Close")
	}
	lines := []string{sBold.Render(fmt.Sprintf("Runner %s · %s · %s", r.ID, r.Repo, r.State))}
	if r.Job != nil {
		lines = append(lines, fmt.Sprintf("%s  #%s  %s", r.Job.Name, r.Job.RunNumber, r.Job.Workflow))
		if r.Job.HTMLURL != "" {
			lines = append(lines, sDim.Render(r.Job.HTMLURL))
		}
	}
	lines = append(lines, "", sBold.Render("Steps"))
	if m.stepsErr != "" {
		lines = append(lines, sRed.Render(m.stepsErr))
	}
	if len(m.steps) == 0 {
		lines = append(lines, sDim.Render("no steps reported yet"))
	}
	for _, s := range m.steps {
		icon, style := "○", sDim
		switch {
		case s.Status == "in_progress":
			icon, style = spinnerFrames[m.frame%len(spinnerFrames)], sAccent
		case s.Conclusion == "success":
			icon, style = "✔", sGreen
		case s.Conclusion == "failure":
			icon, style = "✖", sRed
		case s.Conclusion == "skipped":
			icon = "–"
		}
		lines = append(lines, style.Render(icon)+" "+s.Name)
	}
	lines = append(lines, "", sBold.Render("Containers"))
	if m.ctrsErr != "" {
		lines = append(lines, sRed.Render(m.ctrsErr))
	}
	if len(m.containers) == 0 {
		lines = append(lines, sDim.Render("none in this runner's compose projects"))
	}
	for _, c := range m.containers {
		lines = append(lines, stateStyle(c.State).Render(cell(c.State, 9))+" "+cell(c.Name, 28)+" "+cell(c.Image, 28)+" "+sDim.Render(c.Project))
	}
	lines = append(lines, "", buttons("", "Close")+"  "+sDim.Render("esc close"))
	return strings.Join(lines, "\n")
}
