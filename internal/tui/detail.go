package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	detailCopy = "detail/copy"
	detailStop = "detail/stop"
	detailTabs = "detail/tabs"
)

// The detail page's tabs.
const (
	tabSteps = iota
	tabLog
	tabContainers
)

// tabStrip is the detail page's Steps / Log / Containers switcher. The page
// switches it with ←/→ whatever has focus, so it takes no keys itself.
type tabStrip struct {
	active int
}

var tabNames = []string{"Steps", "Log", "Containers"}

func tabZone(i int) string { return fmt.Sprintf("%s/%d", detailTabs, i) }

func (t *tabStrip) ID() string               { return detailTabs }
func (t *tabStrip) Focusable() bool          { return true }
func (t *tabStrip) TakesKey(tea.KeyMsg) bool { return false }
func (t *tabStrip) Capturing() bool          { return false }
func (t *tabStrip) Blur()                    {}
func (t *tabStrip) SetDisabled(bool)         {}

func (t *tabStrip) Hit(msg tea.MouseMsg) bool {
	for i := range tabNames {
		if zone.Get(tabZone(i)).InBounds(msg) {
			return true
		}
	}
	return false
}

func (t *tabStrip) Update(msg tea.Msg) (ui.Control, tea.Cmd) {
	if msg, ok := msg.(tea.MouseMsg); ok {
		for i := range tabNames {
			if zone.Get(tabZone(i)).InBounds(msg) {
				t.active = i
			}
		}
	}
	return t, nil
}

func (t *tabStrip) View(focused bool, _ int) string {
	out := "  "
	if focused {
		out = sAccent.Render("›") + " "
	}
	for i, name := range tabNames {
		label := sDim.Render("  " + name + "  ")
		if i == t.active {
			label = sAccent.Render("[ " + name + " ]")
		}
		out += zone.Mark(tabZone(i), label) + " "
	}
	return out
}

// openDetail shows the detail page of runner id on tab, remembering the page
// to go back to.
func (m Model) openDetail(id string, tab int) (tea.Model, tea.Cmd) {
	inst := m.instance(id)
	if inst == nil {
		return m, nil
	}
	if m.page != pageDetail {
		m.detailFrom = m.page
	}
	m.page, m.detailID, m.detailSnap = pageDetail, id, *inst
	m.steps, m.containers, m.stepsErr, m.ctrsErr, m.detailScroll = nil, nil, "", "", 0
	m.groups.tabs.active = tab
	m.detailButtons()
	m.groups.detail.Focus(detailTabs)
	follow := m.follow()
	return m, tea.Batch(m.fetchSteps(), m.fetchContainers(), follow)
}

// closeDetail goes back to the page the detail page was opened from.
func (m Model) closeDetail() (tea.Model, tea.Cmd) { return m.switchPage(m.detailFrom) }

// detailButtons renders the header's buttons: Copy run URL when the run's
// URL is known, and Stop runner.
func (m Model) detailButtons() string {
	g := m.groups
	items := []ui.Widget{g.stopRunner, g.tabs}
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

// detailTop is the part of the detail page above the active tab: the
// summary and the tab strip.
func (m Model) detailTop(w int) []string {
	g := m.groups
	return []string{m.detailSummary(), "", g.tabs.View(g.detail.FocusedID() == detailTabs, w), ""}
}

// detailList is the Steps or Containers tab's lines; nil on the Log tab.
func (m Model) detailList() []string {
	switch m.groups.tabs.active {
	case tabSteps:
		var body []string
		if m.stepsErr != "" {
			body = append(body, sRed.Render(m.stepsErr))
		}
		if len(m.steps) == 0 {
			body = append(body, sDim.Render("no steps reported yet"))
		}
		return append(body, m.stepLines()...)
	case tabContainers:
		return m.containerLines()
	}
	return nil
}

// scrollDetail moves the Steps or Containers list d lines, no further than
// its last line reaching the bottom of the page.
func (m *Model) scrollDetail(d int) {
	w, h := m.contentSize()
	limit := max(len(m.detailList())-max(h-len(m.detailTop(w)), 3), 0)
	m.detailScroll = min(max(m.detailScroll+d, 0), limit)
}

// detailPage renders the runner detail page in w columns and h lines: the
// summary, the tab strip and the active tab.
func (m Model) detailPage(w, h int) string {
	m.detailButtons()
	top := m.detailTop(w)
	bodyH := max(h-len(top), 3)
	var body []string
	if m.groups.tabs.active == tabLog {
		lines := m.logLines(bodyH - 2)
		for len(lines) < bodyH-2 {
			lines = append(lines, "")
		}
		body = strings.Split(zone.Mark("log", box("Log — following", w, lines)), "\n")
	} else {
		list := m.detailList()
		body = list[min(m.detailScroll, max(len(list)-bodyH, 0)):]
	}
	out := append(top, body[:min(len(body), bodyH)]...)
	return strings.Join(out, "\n")
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
	case "left", "right":
		n := len(tabNames)
		if k.String() == "left" {
			m.groups.tabs.active = (m.groups.tabs.active + n - 1) % n
		} else {
			m.groups.tabs.active = (m.groups.tabs.active + 1) % n
		}
		m.detailScroll = 0
		cmd := m.follow()
		return true, m, cmd
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
