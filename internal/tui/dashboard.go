package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
)

// allPaused reports whether every repo not being removed is paused: then the
// header offers Resume all. Repos being removed stay paused, so they do not count.
func (m Model) allPaused() bool {
	seen := false
	for _, r := range m.st.Repos {
		if r.Removing {
			continue
		}
		if !r.Paused {
			return false
		}
		seen = true
	}
	return seen
}

// dashButtons renders the Dashboard header's buttons.
func (m Model) dashButtons() string {
	g := m.groups
	g.pauseAll.Label = "Pause all"
	if m.allPaused() {
		g.pauseAll.Label = "Resume all"
	}
	g.add.SetDisabled(!m.connected)
	g.pauseAll.SetDisabled(!m.connected)
	if !m.connected && !m.onCard() {
		m.focusCard(m.focus) // a disabled button hands focus to the card the row keys follow
	}
	f := g.dash.FocusedID()
	return g.add.View(f == dashAdd, 0) + "  " + g.pauseAll.View(f == dashPauseAll, 0)
}

// cardTitle marks the focused card with the focus glyph.
func (m Model) cardTitle(id, title string) string {
	if m.groups.dash.FocusedID() == id {
		return "› " + title
	}
	return title
}

// focusCard focuses a Dashboard card and points the row keys at it.
func (m *Model) focusCard(p pane) {
	m.focus = p
	if p == paneRepos {
		m.groups.dash.Focus(dashRepos)
	} else {
		m.groups.dash.Focus(dashRunners)
	}
}

// dashKey handles tab, shift+tab and the header buttons' keys on the
// Dashboard. It reports false for keys the page and global keys handle.
func (m Model) dashKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	g := m.groups
	if ok, cmd := g.dash.Key(k); ok {
		return true, m, cmd
	}
	switch k.String() {
	case "tab":
		g.dash.Next()
	case "shift+tab":
		g.dash.Prev()
	default:
		return false, m, nil
	}
	switch g.dash.FocusedID() {
	case dashRepos:
		m.focus = paneRepos
	case dashRunners:
		m.focus = paneRunners
	}
	return true, m, nil
}

// onCard reports whether a Dashboard card, not a header button, has focus.
func (m Model) onCard() bool {
	id := m.groups.dash.FocusedID()
	return id != dashAdd && id != dashPauseAll
}

// statTiles renders the Dashboard's four stat tiles across w columns: boxed
// side by side, or on one line when compact.
func (m Model) statTiles(w int, compact bool) string {
	running, queued, active, paused := 0, 0, 0, 0
	for _, i := range m.st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	for _, r := range m.st.Repos {
		queued += r.Queued
		if r.Paused || r.Removing {
			paused++ // a repo being removed stays paused
		} else {
			active++
		}
	}
	limit := fmt.Sprint(m.st.GlobalMax)
	if m.st.Mode == config.ModeAll {
		limit = "∞"
	}
	diskStyle := sDim
	switch m.diskState() {
	case "warn":
		diskStyle = sAmber
	case "critical":
		diskStyle = sRed
	}
	tiles := []struct{ title, value string }{
		{"Running", fmt.Sprintf("%d / %s", running, limit)},
		{"Queued jobs", fmt.Sprint(queued)},
		{"Repositories", fmt.Sprintf("%d active · %d paused", active, paused)},
		{"Disk", diskStyle.Render(fmt.Sprintf("%d%%", m.st.DiskPct))},
	}
	if compact {
		var parts []string
		for _, t := range tiles {
			parts = append(parts, sDim.Render(t.title)+" "+sBold.Render(t.value))
		}
		return cell(" "+strings.Join(parts, "   "), w)
	}
	tw := (w - 3) / 4 // three one-column gaps
	var boxes []string
	for i, t := range tiles {
		bw := tw
		if i == len(tiles)-1 {
			bw = w - 3*tw - 3 // the last tile takes the remainder
		}
		if i > 0 {
			boxes = append(boxes, " ")
		}
		boxes = append(boxes, box(t.title, bw, []string{sBold.Render(t.value)}))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
}

func (m Model) reposLines(w int) []string {
	narrow := w < 80
	inner := w - 4
	hdr := fmt.Sprintf("  %-14s %-10s %-5s %-6s", "REPO", "STATE", "RUN", "QUEUE")
	if !narrow {
		hdr += " LAST JOB"
	}
	lines := []string{sDim.Render(hdr)}
	for i, r := range m.st.Repos {
		state, st := "active", "active"
		dot := "●"
		if r.Paused {
			state, st, dot = "paused", "paused", "◌"
		}
		if r.Removing {
			state, st, dot = "removing", "removing", "◌"
		}
		if r.Error != "" {
			state, st, dot = "error", "error", "✖"
		}
		queue := "–"
		if r.Queued > 0 {
			queue = sAmber.Render(fmt.Sprintf("⧗ %d", r.Queued))
		}
		sel := "  "
		if i == m.repoSel && m.focus == paneRepos {
			sel = "▸ "
		}
		line := sel + cell(r.Name, 14) + " " + stateStyle(st).Render(cell(dot+" "+state, 10)) + " " +
			cell(fmt.Sprintf("%d/%s", r.Active, maxText(r.Max)), 5) + " " + cell(queue, 6)
		if !narrow {
			last := sDim.Render("–")
			if j := r.LastJob; j != nil {
				icon, style := "✔", sGreen
				if j.Conclusion != "success" {
					icon, style = "✖", sRed
				}
				last = style.Render(icon) + fmt.Sprintf(" #%s %s  %s", j.RunNumber, j.JobName, sDim.Render(ago(m.now().Sub(j.FinishedAt))))
			}
			line += " " + last
		}
		if r.Error != "" && !narrow {
			line += "  " + sRed.Render(r.Error)
		}
		lines = append(lines, m.row(fmt.Sprintf("repo-%d", i), i == m.repoSel && m.focus == paneRepos, line, inner))
	}
	if len(m.st.Repos) == 0 {
		lines = append(lines, sDim.Render("  no repos configured — press a to add one"))
	}
	return lines
}

func (m Model) eventLines(n, w int) []string {
	inner := w - 4
	end := len(m.events) - m.eventScroll
	if end < 0 {
		end = 0
	}
	start := end - n
	if start < 0 {
		start = 0
	}
	var lines []string
	for _, e := range m.events[start:end] {
		icon, style := eventStyle(e.Level)
		lines = append(lines, cell(sDim.Render(e.Time.Local().Format("15:04:05"))+"  "+style.Render(icon)+" "+cell(e.Repo, 11)+" "+e.Msg, inner))
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

func (m Model) dashboard(w, h int) string {
	repoLines, runnerLines := m.reposLines(w), m.runnersLines(w)
	// When rows run short, the Activity card shrinks first (to 3 lines), then
	// the tiles collapse to one line; under 100 columns they are one line anyway.
	tilesH := 3
	if m.width < wideMin || tilesH+len(repoLines)+len(runnerLines)+4+5 > h {
		tilesH = 1
	}
	room := h - tilesH - 9 // two table borders plus an Activity card of at least 3 lines
	nRepo, nRun := len(repoLines), len(runnerLines)
	if nRepo+nRun > room {
		nRepo = min(nRepo, max(room/2, room-nRun))
		nRun = room - nRepo
	}
	tiles := m.statTiles(w, tilesH == 1)
	repos := box(m.cardTitle(dashRepos, "Repositories"), w, fit(repoLines, 1, m.repoSel, nRepo))
	runners := box(m.cardTitle(dashRunners, "Runners"), w, fit(runnerLines, 1, m.runnerSel, nRun))
	rest := h - tilesH - lipgloss.Height(repos) - lipgloss.Height(runners) - 2
	if w < 80 || rest < 3 {
		rest = 3
	}
	events := zone.Mark("events", box("Activity", w, m.eventLines(rest, w)))
	return strings.Join([]string{tiles, repos, runners, events}, "\n")
}
