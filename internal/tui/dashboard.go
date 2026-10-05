package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
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

// series turns samples into chart values; a gap of more than 90 seconds
// between samples (the daemon was down) draws a space.
func series(samples []model.MetricSample, pick func(model.MetricSample) *float64) []*float64 {
	var out []*float64
	for i, s := range samples {
		if i > 0 && s.At.Sub(samples[i-1].At) > 90*time.Second {
			out = append(out, nil)
		}
		out = append(out, pick(s))
	}
	return out
}

func seriesMax(vals []*float64) float64 {
	top := 0.0
	for _, v := range vals {
		if v != nil && *v > top {
			top = *v
		}
	}
	return top
}

func fmtMem(b int64) string {
	if b >= 1<<30 {
		return fmt.Sprintf("%.1fG", float64(b)/(1<<30))
	}
	return fmt.Sprintf("%dM", b>>20)
}

// statTiles renders the Dashboard's five stat tiles across w columns: boxed
// side by side with a chart after each value, or on one line when compact.
func (m Model) statTiles(w int, compact bool) string {
	running, queued := 0, 0
	for _, i := range m.st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	for _, r := range m.st.Repos {
		queued += r.Queued
	}
	mt := m.mg.metrics
	num := func(n int) *float64 { v := float64(n); return &v }
	live := series(mt.Samples, func(s model.MetricSample) *float64 { return num(s.Live) })
	queue := series(mt.Samples, func(s model.MetricSample) *float64 { return num(s.Queued) })
	cpus := series(mt.Samples, func(s model.MetricSample) *float64 { return s.CPU })
	limit, scale := fmt.Sprint(m.st.GlobalMax), float64(m.st.GlobalMax)
	if m.st.Mode == config.ModeAll {
		limit, scale = "∞", seriesMax(live)
	}
	cpu, mem := "–", "–"
	failed := m.mg.metricsErr != ""
	if mt.CPU != nil && !failed {
		cpu = fmt.Sprintf("%.0f%%", *mt.CPU)
	}
	if mt.MemUsed != nil && mt.MemTotal != nil && !failed {
		mem = fmtMem(*mt.MemUsed) + " / " + fmtMem(*mt.MemTotal)
	}
	diskStyle := sDim
	switch m.diskState() {
	case "warn":
		diskStyle = sAmber
	case "critical":
		diskStyle = sRed
	}
	gauge := func(used, total int, n int) string {
		if total <= 0 || n < 3 {
			return ""
		}
		return ui.Gauge(used, total, n-2)
	}
	errLine := func(n int) string { return sRed.Render(cell("✖ "+m.mg.metricsErr, n)) }
	// Each tile has a value line (value, then an inline chart in the room
	// left) and a chart line under it.
	tiles := []struct {
		title, value string
		inline       func(n int) string
		below        func(n int) string
	}{
		{"Running", fmt.Sprintf("%d / %s", running, limit), nil,
			func(n int) string { return sAccent.Render(ui.Sparkline(live, scale, n)) }},
		{"Queued jobs", fmt.Sprint(queued), nil,
			func(n int) string { return sAmber.Render(ui.Sparkline(queue, max(seriesMax(queue), 1), n)) }},
		{"CPU", cpu, func(n int) string {
			if mt.CPU == nil || failed {
				return ""
			}
			return gauge(int(*mt.CPU+0.5), 100, n)
		}, func(n int) string {
			if failed {
				return errLine(n)
			}
			return sAccent.Render(ui.Sparkline(cpus, 100, n))
		}},
		{"Memory", mem, nil, func(n int) string {
			if failed {
				return errLine(n)
			}
			if mt.MemUsed == nil || mt.MemTotal == nil {
				return ""
			}
			return gauge(int(*mt.MemUsed>>20), int(*mt.MemTotal>>20), n)
		}},
		{"Disk", diskStyle.Render(fmt.Sprintf("%d%%", m.st.DiskPct)), nil, func(n int) string {
			return diskStyle.Render(gauge(m.st.DiskPct, 100, n))
		}},
	}
	if compact {
		var parts []string
		for _, t := range tiles {
			parts = append(parts, sDim.Render(t.title)+" "+sBold.Render(t.value))
		}
		line := " " + strings.Join(parts, "   ")
		if failed {
			line += "   " + sRed.Render("metrics ✖ "+m.mg.metricsErr)
		}
		return cell(line, w)
	}
	tw := (w - 4) / 5 // four one-column gaps
	var boxes []string
	for i, t := range tiles {
		bw := tw
		if i == len(tiles)-1 {
			bw = w - 4*tw - 4 // the last tile takes the remainder
		}
		if i > 0 {
			boxes = append(boxes, " ")
		}
		line := sBold.Render(t.value)
		if room := bw - 4 - ansi.StringWidth(t.value) - 1; t.inline != nil && room >= 3 {
			line += " " + t.inline(room)
		}
		boxes = append(boxes, box(t.title, bw, []string{line, t.below(bw - 4)}))
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
		state := "active"
		if r.Paused {
			state = "paused"
		}
		if r.Removing {
			state = "removing"
		}
		if r.Error != "" {
			state = "error"
		}
		queue := "–"
		if r.Queued > 0 {
			queue = sAmber.Render(fmt.Sprintf("⧗ %d", r.Queued))
		}
		selected := i == m.repoSel && m.repoFocus()
		sel := "  "
		if selected {
			sel = "▸ "
		}
		badge := stateBadge(state)
		before := sel + cell(r.Name, 14) + " "
		after := strings.Repeat(" ", max(10-ansi.StringWidth(badge), 0)) + " " +
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
			after += " " + last
		}
		if r.Error != "" && !narrow {
			after += "  " + sRed.Render(r.Error)
		}
		if selected {
			pause := ui.NewButton(rowPause, "Pause", ui.Secondary)
			if r.Paused {
				pause.Label = "Resume"
			}
			lines = append(lines, m.selectedRow(fmt.Sprintf("repo-%d", i), before, badge, after,
				rowButtons(ui.NewButton(rowEdit, "Edit", ui.Secondary), pause, ui.NewButton(rowRemove, "Remove", ui.Danger)), "", inner))
			continue
		}
		lines = append(lines, zone.Mark(fmt.Sprintf("repo-%d", i), ui.BadgeRow(sSel, false, before, badge, after, inner)))
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
	tilesH := 4
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
