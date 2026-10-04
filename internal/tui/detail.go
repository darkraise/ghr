package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	detailCopy = "detail/copy"
	detailStop = "detail/stop"
)

// openDetail shows the detail page of runner id, remembering the page to go
// back to.
func (m Model) openDetail(id string) (tea.Model, tea.Cmd) {
	inst := m.instance(id)
	if inst == nil {
		return m, nil
	}
	if m.page != pageDetail {
		m.detailFrom = m.page
	}
	m.page, m.detailID, m.detailSnap = pageDetail, id, *inst
	m.steps, m.containers, m.stepsErr, m.ctrsErr = nil, nil, "", ""
	m.groups.detail.Focus(detailStop)
	return m, tea.Batch(m.fetchSteps(), m.fetchContainers())
}

// closeDetail goes back to the page the detail page was opened from.
func (m Model) closeDetail() (tea.Model, tea.Cmd) { return m.switchPage(m.detailFrom) }

// detailButtons renders the header's buttons: Copy run URL when the run's
// URL is known, and Stop runner.
func (m Model) detailButtons() string {
	g := m.groups
	items := []ui.Widget{g.stopRunner}
	out := g.stopRunner.View(g.detail.FocusedID() == detailStop, 0)
	if j := m.detailSnap.Job; j != nil && j.HTMLURL != "" {
		items = append([]ui.Widget{g.copyURL}, items...)
		out = g.copyURL.View(g.detail.FocusedID() == detailCopy, 0) + "  " + out
	}
	g.stopRunner.SetDisabled(!m.connected)
	g.detail.Set(items)
	return out
}

// detailSummary is the line under the header: state, repo, job and run,
// elapsed time and start time.
func (m Model) detailSummary() string {
	r := m.detailSnap
	state := clean(r.State)
	parts := []string{stateStyle(state).Render("● " + state), clean(r.Repo)}
	start := r.Since
	if j := r.Job; j != nil {
		job := j.Name
		if j.RunNumber != "" {
			job += "  #" + j.RunNumber
		}
		parts = append(parts, job)
		if !j.StartedAt.IsZero() {
			start = j.StartedAt
		}
	}
	parts = append(parts, dur(m.now().Sub(start)), "started "+start.Local().Format("2006-01-02 15:04:05"))
	return strings.Join(parts, sDim.Render("  ·  "))
}

// detailPage renders the runner detail page in w columns and h lines.
func (m Model) detailPage(w, h int) string {
	lines := []string{m.detailSummary(), ""}
	lines = append(lines, sBold.Render("Steps"))
	if m.stepsErr != "" {
		lines = append(lines, sRed.Render(m.stepsErr))
	}
	if len(m.steps) == 0 {
		lines = append(lines, sDim.Render("no steps reported yet"))
	}
	lines = append(lines, m.stepLines()...)
	lines = append(lines, "", sBold.Render("Containers"))
	lines = append(lines, m.containerLines()...)
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:min(len(lines), h)], "\n")
}

func (m Model) stepLines() []string {
	var lines []string
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
	return lines
}

func (m Model) containerLines() []string {
	var lines []string
	if m.ctrsErr != "" {
		lines = append(lines, sRed.Render(m.ctrsErr))
	}
	if len(m.containers) == 0 {
		lines = append(lines, sDim.Render("none in this runner's compose projects"))
	}
	for _, c := range m.containers {
		lines = append(lines, stateStyle(c.State).Render(cell(c.State, 9))+" "+cell(c.Name, 28)+" "+cell(c.Image, 28)+" "+sDim.Render(c.Project))
	}
	return lines
}

// stopRunner stops runner id, asking first while it runs a job.
func (m Model) stopRunner(id string, busy bool) (tea.Model, tea.Cmd) {
	kill := func() tea.Cmd {
		return m.action("stopped "+id, func(c context.Context) error { return m.c.Kill(c, id) })
	}
	if busy {
		return m.openConfirm(fmt.Sprintf("Runner %s is running a job. Stop it?", id), kill)
	}
	return m, kill()
}

// detailKey handles the detail page's keys: esc goes back, x stops the
// runner, tab moves between the header buttons. It reports false for keys
// the global keys handle.
func (m Model) detailKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	m.detailButtons()
	if ok, cmd := m.groups.detail.Key(k); ok {
		return true, m, cmd
	}
	switch k.String() {
	case "esc":
		mm, cmd := m.closeDetail()
		return true, mm, cmd
	case "x":
		mm, cmd := m.stopRunner(m.detailID, m.detailSnap.State == "busy")
		return true, mm, cmd
	case "tab":
		m.groups.detail.Next()
	case "shift+tab":
		m.groups.detail.Prev()
	default:
		return false, m, nil
	}
	return true, m, nil
}

// detailPressed runs a detail header button.
func (m Model) detailPressed(id string) (tea.Model, tea.Cmd) {
	switch id {
	case detailCopy:
		if j := m.detailSnap.Job; j != nil && j.HTMLURL != "" {
			m.copyFn(j.HTMLURL)
			m.toast.Show("copied "+j.HTMLURL, false, m.now())
		}
	case detailStop:
		return m.stopRunner(m.detailID, m.detailSnap.State == "busy")
	}
	return m, nil
}
