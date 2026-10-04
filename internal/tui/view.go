package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/model"
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
	w := m.width
	if w < 40 {
		w = 40
	}
	var parts []string
	parts = append(parts, m.header(w))
	if !m.connected {
		parts = append(parts, sBanner.Render("daemon unreachable: "+m.connErr+" — retrying"))
	} else if m.st.Degraded {
		parts = append(parts, sBanner.Render("DEGRADED: "+m.st.DegradedReason+" — no new runners"))
	}
	parts = append(parts, m.tabBar(w))
	used := lipgloss.Height(strings.Join(parts, "\n")) + 2 // footer + flash
	bodyH := m.height - used
	if bodyH < 6 {
		bodyH = 6
	}
	switch m.tab {
	case tabDashboard:
		parts = append(parts, m.dashboard(w, bodyH))
	case tabRunners:
		parts = append(parts, m.runnersTab(w, bodyH))
	case tabHistory:
		parts = append(parts, m.historyTab(w, bodyH))
	case tabConfig:
		parts = append(parts, m.configTab(w, bodyH))
	}
	parts = append(parts, m.flashLine(), m.footer(w))
	out := strings.Join(parts, "\n")
	if m.overlay != ovNone {
		out = m.withOverlay(out, w)
	}
	return zone.Scan(out)
}

func (m Model) header(w int) string {
	running := 0
	for _, i := range m.st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	mode := sGreen.Render("● " + strings.ToUpper(m.st.Mode))
	global := fmt.Sprintf("%s %d/%d", gauge(running, m.st.GlobalMax, 3), running, m.st.GlobalMax)
	if m.st.Mode == "all" {
		global = fmt.Sprintf("%d/∞", running)
	}
	api := fmt.Sprintf("%s %d", gauge(max(m.st.RateRemaining, 0), 5000, 10), m.st.RateRemaining)
	diskStyle := sDim
	if m.st.DiskPct >= 80 {
		diskStyle = sAmber
	}
	disk := diskStyle.Render(fmt.Sprintf("%s %d%%", gauge(m.st.DiskPct, 100, 10), m.st.DiskPct))
	line := fmt.Sprintf(" mode %s   global %s   api %s   disk %s", mode, global, api, disk)
	return box("ghr", w, []string{line})
}

func (m Model) tabBar(w int) string {
	var tabs []string
	for i, name := range tabNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if tab(i) == m.tab {
			label = sAccent.Render("[" + label + "]")
		} else {
			label = sDim.Render(" " + label + " ")
		}
		tabs = append(tabs, zone.Mark(fmt.Sprintf("tab-%d", i), label))
	}
	bar := strings.Join(tabs, "")
	help := sDim.Render("? help")
	gap := w - ansi.StringWidth(bar) - ansi.StringWidth(help) - 1
	if gap < 1 {
		gap = 1
	}
	return bar + strings.Repeat(" ", gap) + help
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

func (m Model) runnerLine(i int, r model.InstanceStatus, selected bool, inner int) string {
	state := r.State
	icon := "○"
	switch state {
	case "busy":
		icon = spinnerFrames[m.frame%len(spinnerFrames)]
	case "starting":
		icon = "◔"
	case "cleaning":
		icon = "♻"
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
	line := sel + cell(r.ID, 8) + " " + cell(r.Repo, 12) + " " + stateStyle(state).Render(cell(icon+" "+state, 11)) + " " +
		cell(job, inner-50) + " " + cell(elapsed, 8)
	return m.row(fmt.Sprintf("runner-%d", i), selected, line, inner)
}

func (m Model) runnersLines(w int) []string {
	inner := w - 4
	lines := []string{sDim.Render(fmt.Sprintf("  %-8s %-12s %-11s %-*s %s", "ID", "REPO", "STATE", inner-50, "JOB", "ELAPSED"))}
	for i, r := range m.st.Instances {
		selected := i == m.runnerSel && (m.focus == paneRunners || m.tab == tabRunners)
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

func (m Model) dashboard(w, h int) string {
	repoLines, runnerLines := m.reposLines(w), m.runnersLines(w)
	room := h - 9 // two table borders plus an Events box of at least 3 lines
	nRepo, nRun := len(repoLines), len(runnerLines)
	if nRepo+nRun > room {
		nRepo = min(nRepo, max(room/2, room-nRun))
		nRun = room - nRepo
	}
	repos := box("Repos", w, fit(repoLines, 1, m.repoSel, nRepo))
	runners := box("Runners", w, fit(runnerLines, 1, m.runnerSel, nRun))
	rest := h - lipgloss.Height(repos) - lipgloss.Height(runners) - 2
	if w < 80 || rest < 3 {
		rest = 3
	}
	events := zone.Mark("events", box("Events", w, m.eventLines(rest, w)))
	return strings.Join([]string{repos, runners, events}, "\n")
}

func (m Model) runnersTab(w, h int) string {
	runners := box("Runners", w, fit(m.runnersLines(w), 1, m.runnerSel, max(h/2-2, 2)))
	logH := h - lipgloss.Height(runners) - 2
	if logH < 3 {
		logH = 3
	}
	title := "Log — no runner selected"
	var lines []string
	if m.logID != "" {
		title = "Log " + m.logID + " (following)"
		all := strings.Split(strings.TrimRight(m.logText, "\n"), "\n")
		end := len(all) - m.logScroll
		if end < 0 {
			end = 0
		}
		start := end - logH
		if start < 0 {
			start = 0
		}
		lines = all[start:end]
	}
	for len(lines) < logH {
		lines = append(lines, "")
	}
	return runners + "\n" + zone.Mark("log", box(title, w, lines))
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

func (m Model) configTab(w, h int) string {
	inner := w - 4
	fields := m.configFields()
	lines := []string{sDim.Render("enter edits the selected setting; the daemon validates before saving")}
	if fields == nil {
		lines = append(lines, sDim.Render("loading…"))
	}
	for i, f := range fields {
		line := "  " + cell(f.label, 36) + " " + f.value
		lines = append(lines, m.row(fmt.Sprintf("cfg-%d", i), i == m.cfgSel, line, inner))
	}
	return box("Config", w, fit(lines, 1, m.cfgSel, h-2))
}

func (m Model) flashLine() string {
	if m.flash == "" {
		return ""
	}
	if m.flashErr {
		return sRed.Render(" ✖ " + m.flash)
	}
	return sGreen.Render(" ✔ " + m.flash)
}

func (m Model) footer(w int) string {
	var parts []string
	for _, f := range footerKeys {
		text := sAccent.Render(f.key)
		if f.label != "" {
			text += " " + sDim.Render(f.label)
		}
		parts = append(parts, zone.Mark("key-"+f.key, text))
	}
	return " " + ansi.Truncate(strings.Join(parts, "  "), w-1, "…")
}

// buttons renders the clickable dialog buttons: ok runs enter, cancel runs esc.
func buttons(ok, cancel string) string {
	b := func(id, label string) string { return zone.Mark(id, sAccent.Render("[ "+label+" ]")) }
	if ok == "" {
		return b("btn-cancel", cancel)
	}
	return b("btn-ok", ok) + "  " + b("btn-cancel", cancel)
}

func (m Model) withOverlay(base string, w int) string {
	var body string
	switch m.overlay {
	case ovConfirm:
		body = m.confirmText + "\n\n" + buttons("Yes", "No") + "\n" + sDim.Render("y confirm · any other key cancels")
	case ovPrompt:
		body = sBold.Render(m.promptLabel) + "\n\n" + m.prompt.View() + "\n\n" + buttons("Save", "Cancel") + "\n" +
			sDim.Render("enter save · esc cancel")
	case ovHelp:
		body = sBold.Render("Keys") + "\n\n" + strings.Join([]string{
			"1-4 / tab   switch tab            ↑↓ / j k   move selection",
			"h / →       focus repos / runners  p          pause/resume repo",
			"+ / -       repo cap               [ / ]      global cap",
			"m           toggle queue/all       P          pause/resume all",
			"a / d       add / remove repo      x          stop runner",
			"l           follow runner log      enter      details / edit / copy URL",
			"pgup/pgdn   scroll events and log  q          quit",
			"",
			"Mouse: click tabs, rows, footer keys and dialog buttons; double-click a runner; wheel scrolls.",
			"Selecting terminal text needs Shift-drag (Option-drag in iTerm2).",
			"Under tmux, mouse input requires `set -g mouse on`.",
		}, "\n") + "\n\n" + buttons("", "Close")
	case ovDetail:
		body = m.detailBody()
	}
	dialog := sDialog.Render(body)
	return lipgloss.Place(w, lipgloss.Height(base), lipgloss.Center, lipgloss.Center, dialog,
		lipgloss.WithWhitespaceChars(" "))
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
