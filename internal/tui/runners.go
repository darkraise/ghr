package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

func (m Model) runnerLine(i int, r model.InstanceStatus, selected bool, inner int) string {
	state := r.State
	badge := stateBadge(state)
	if state == "busy" {
		badge = spinnerFrames[m.frame%len(spinnerFrames)] + " " + badge
	}
	job, elapsed := sDim.Render("–"), dur(m.now().Sub(r.Since))
	if r.Job != nil {
		job = r.Job.Name
		if r.Job.RunNumber != "" {
			job += "  #" + r.Job.RunNumber
		}
		if !r.Job.StartedAt.IsZero() {
			elapsed = dur(m.now().Sub(r.Job.StartedAt))
		}
	}
	sel := "  "
	if selected {
		sel = "▸ "
	}
	before := sel + cell(r.ID, 8) + " " + cell(r.Repo, 12) + " "
	after := strings.Repeat(" ", max(11-ansi.StringWidth(badge), 0)) + " " + cell(job, inner-50)
	tail := " " + cell(elapsed, 8)
	if selected {
		return m.selectedRow(fmt.Sprintf("runner-%d", i), before, badge, after,
			rowButtons(ui.NewButton(rowLogs, "Logs", ui.Secondary), ui.NewButton(rowStop, "Stop", ui.Danger)), tail, inner)
	}
	return zone.Mark(fmt.Sprintf("runner-%d", i), ui.BadgeRow(sSel, false, before, badge, after+tail, inner))
}

func (m Model) runnersLines(w int) []string {
	inner := w - 4
	lines := []string{sDim.Render(fmt.Sprintf("  %-8s %-12s %-11s %-*s %s", "ID", "REPO", "STATE", inner-50, "JOB", "ELAPSED"))}
	for i, r := range m.st.Instances {
		selected := i == m.runnerSel && m.runnerFocus()
		lines = append(lines, m.runnerLine(i, r, selected, inner))
	}
	for _, r := range m.st.Repos {
		if r.Paused || r.Queued == 0 || r.Max == 0 || r.Active < r.Max {
			continue
		}
		lines = append(lines, sAmber.Render(fmt.Sprintf("  %-8s %-12s %-11s %d jobs queued (repo cap %d)", "–", r.Name, "⧗ waiting", r.Queued, r.Max)))
	}
	if len(m.st.Instances) == 0 {
		lines = append(lines, sDim.Render("  no runners — they start when jobs are queued"))
	}
	return lines
}

// runnersPage is the full-width runners table with a live log preview of the
// selected runner below it. When rows run short the preview gives way first,
// down to 3 lines; only then does the table scroll.
func (m Model) runnersPage(w, h int) string {
	all := m.runnersLines(w)
	logH := h - (len(all) + 2) - 2
	n := len(all)
	if logH < 3 {
		logH = 3
		n = max(h-2-logH-2, 2)
	}
	// The table is the page's one focusable element, so it is always marked.
	runners := box("› Runners", w, fit(all, 1, m.runnerSel, n))
	title := "Log preview — no runner selected"
	var lines []string
	// The log of a runner that has gone stays in logID; it is no longer followed.
	if m.logID != "" && m.selectedRunner() != nil {
		title = "Log preview — " + m.logID + " (following)"
		lines = m.logLines(logH)
	}
	for len(lines) < logH {
		lines = append(lines, "")
	}
	return runners + "\n" + zone.Mark("log", box(title, w, lines))
}

// logLines is up to n lines of the followed log, ending logScroll lines above
// its last line.
func (m Model) logLines(n int) []string {
	all := strings.Split(strings.TrimRight(m.logText, "\n"), "\n")
	end := max(len(all)-m.logScroll, 0)
	return all[max(end-n, 0):end]
}
