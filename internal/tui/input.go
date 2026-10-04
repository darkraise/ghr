package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

const doubleClick = 400 * time.Millisecond

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		// The terminal's emergency exit: nothing guards it.
		return m, tea.Quit
	}
	switch m.overlay {
	case ovPrompt:
		switch key {
		case "esc":
			m.overlay = ovNone
			return m, nil
		case "enter":
			m.overlay = ovNone
			return m, m.promptSubmit(strings.TrimSpace(m.prompt.Value()))
		}
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(k)
		return m, cmd
	case ovConfirm, ovHelp:
		return m.dialogKey(k)
	case ovDetail:
		if key == "esc" || key == "q" || key == "enter" || key == "?" {
			m.overlay = ovNone
		}
		return m, nil
	}
	if m.page == pageSettings {
		if ok, mm, cmd := m.settingsKey(k); ok {
			return mm, cmd
		}
	}
	return m.press(key)
}

// press runs the action bound to key; mouse clicks on footer hints call it too.
func (m Model) press(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		return m.openHelp()
	case "1", "2", "3", "4":
		return m.switchPage(page(key[0] - '1'))
	case "tab", "shift+tab":
		// tab moves focus within a page; on the Dashboard, between its two tables.
		if m.page == pageDashboard {
			m.focus = 1 - m.focus
		}
	case "left", "h":
		m.focus = paneRepos
	case "right":
		m.focus = paneRunners
	case "up", "k", "down", "j":
		d := 1
		if key == "up" || key == "k" {
			d = -1
		}
		cmd := m.move(d)
		return m, cmd
	case "pgup":
		m.eventScroll += 5
		m.logScroll += 10
	case "pgdown":
		m.eventScroll = max(0, m.eventScroll-5)
		m.logScroll = max(0, m.logScroll-10)
	case "p":
		if !m.repoFocus() {
			return m, nil
		}
		return m, m.togglePause()
	case "P":
		return m, m.togglePauseAll()
	case "+", "=", "-":
		if !m.repoFocus() {
			return m, nil
		}
		return m, m.repoCap(key != "-")
	case "[", "]":
		return m, m.globalCap(key == "]")
	case "m":
		mode := config.ModeAll
		if m.st.Mode == config.ModeAll {
			mode = config.ModeQueue
		}
		return m, m.action("mode "+mode, func(c context.Context) error {
			return m.c.PatchConfig(c, model.ConfigPatch{Mode: &mode})
		})
	case "a":
		return m.openPrompt("Add repo — name [label,label]", "", func(v string) tea.Cmd {
			f := strings.Fields(v)
			if len(f) == 0 {
				return nil
			}
			req := model.AddRepoRequest{Name: f[0]}
			if len(f) > 1 {
				req.Labels = strings.Split(f[1], ",")
			}
			return m.action("added "+f[0], func(c context.Context) error { return m.c.AddRepo(c, req) })
		})
	case "d":
		if r := m.selectedRepo(); r != nil && m.repoFocus() {
			name := r.Name
			return m.openConfirm(fmt.Sprintf("Remove repo %s? Its running jobs finish first.", name), func() tea.Cmd {
				return m.action("removing "+name, func(c context.Context) error { return m.c.RemoveRepo(c, name) })
			})
		}
	case "x":
		if r := m.selectedRunner(); r != nil && m.runnerFocus() {
			id, busy := r.ID, r.State == "busy"
			kill := func() tea.Cmd {
				return m.action("stopped "+id, func(c context.Context) error { return m.c.Kill(c, id) })
			}
			if busy {
				return m.openConfirm(fmt.Sprintf("Runner %s is running a job. Stop it?", id), kill)
			}
			return m, kill()
		}
	case "l":
		if m.selectedRunner() != nil {
			m.page = pageRunners
			cmd := m.follow()
			return m, cmd
		}
	case "r":
		if m.page == pageHistory {
			m.histRepo = m.cycle(m.histRepo, m.repoNames())
			return m, m.fetchHistory()
		}
	case "c":
		if m.page == pageHistory {
			m.histConcl = m.cycle(m.histConcl, []string{"success", "failure", "cancelled"})
			return m, m.fetchHistory()
		}
	case "enter":
		return m.enter()
	}
	return m, nil
}

func (m Model) switchPage(p page) (tea.Model, tea.Cmd) {
	m.page = p
	switch p {
	case pageRunners:
		cmd := m.follow()
		return m, cmd
	case pageHistory:
		return m, m.fetchHistory()
	case pageSettings:
		return m, m.fetchConfig()
	}
	return m, nil
}

func (m *Model) move(d int) tea.Cmd {
	switch {
	case m.page == pageHistory:
		m.histSel = clamp(m.histSel+d, len(m.hist))
	case m.page == pageSettings:
		return nil
	case m.page == pageRunners || m.focus == paneRunners:
		m.runnerSel = clamp(m.runnerSel+d, len(m.st.Instances))
		return m.follow()
	default:
		m.repoSel = clamp(m.repoSel+d, len(m.st.Repos))
	}
	return nil
}

func (m Model) repoNames() []string {
	var out []string
	for _, r := range m.st.Repos {
		out = append(out, r.Name)
	}
	return out
}

// cycle steps "" → opts[0] → … → "" (the empty value means no filter).
func (m Model) cycle(cur string, opts []string) string {
	for i, o := range opts {
		if o == cur {
			if i+1 < len(opts) {
				return opts[i+1]
			}
			return ""
		}
	}
	if len(opts) == 0 {
		return ""
	}
	return opts[0]
}

func (m Model) togglePause() tea.Cmd {
	r := m.selectedRepo()
	if r == nil {
		return nil
	}
	name, paused := r.Name, r.Paused
	if paused {
		return m.action("resumed "+name, func(c context.Context) error { return m.c.Resume(c, name) })
	}
	return m.action("paused "+name, func(c context.Context) error { return m.c.Pause(c, name) })
}

func (m Model) togglePauseAll() tea.Cmd {
	all := len(m.st.Repos) > 0
	for _, r := range m.st.Repos {
		all = all && r.Paused
	}
	if all {
		return m.action("resumed all repos", m.c.ResumeAll)
	}
	return m.action("paused all repos (drain)", m.c.PauseAll)
}

func (m Model) repoCap(up bool) tea.Cmd {
	r := m.selectedRepo()
	if r == nil || r.Max == 0 {
		return nil // unlimited: change it on the Settings page
	}
	n := r.Max - 1
	if up {
		n = r.Max + 1
	}
	if n < 1 {
		return nil
	}
	name := r.Name
	return m.action(fmt.Sprintf("%s max %d", name, n), func(c context.Context) error {
		return m.c.PatchConfig(c, model.ConfigPatch{Repos: map[string]model.RepoPatch{name: {Max: &n}}})
	})
}

func (m Model) globalCap(up bool) tea.Cmd {
	n := m.st.GlobalMax - 1
	if up {
		n = m.st.GlobalMax + 1
	}
	if n < 1 {
		return nil
	}
	return m.action(fmt.Sprintf("global max %d", n), func(c context.Context) error {
		return m.c.PatchConfig(c, model.ConfigPatch{GlobalMax: &n})
	})
}

func (m Model) openPrompt(label, value string, submit func(string) tea.Cmd) (tea.Model, tea.Cmd) {
	m.overlay, m.promptLabel, m.promptSubmit = ovPrompt, label, submit
	m.prompt.SetValue(value)
	m.prompt.CursorEnd()
	// Focus mutates m.prompt; Go leaves unspecified whether `return m, m.prompt.Focus()` copies m first.
	cmd := m.prompt.Focus()
	return m, cmd
}

func (m Model) runnerFocus() bool {
	return m.page == pageRunners || (m.page == pageDashboard && m.focus == paneRunners)
}

func (m Model) repoFocus() bool {
	return m.page == pageDashboard && m.focus == paneRepos
}

func (m Model) enter() (tea.Model, tea.Cmd) {
	switch m.page {
	case pageHistory:
		if m.histSel < len(m.hist) {
			url := m.hist[m.histSel].HTMLURL
			if url == "" {
				m.toast.Show("no run URL recorded", true, m.now())
				return m, nil
			}
			m.copyFn(url)
			m.toast.Show("copied "+url, false, m.now())
		}
		return m, nil
	case pageSettings:
		return m, nil
	}
	if r := m.selectedRunner(); r != nil && (m.page == pageRunners || m.focus == paneRunners) {
		m.overlay, m.detailID = ovDetail, r.ID
		m.steps, m.containers, m.stepsErr, m.ctrsErr = nil, nil, "", ""
		return m, tea.Batch(m.fetchSteps(), m.fetchContainers())
	}
	return m, nil
}

// dialogButtons map each detail or prompt overlay button zone to the key it stands for.
var dialogButtons = []struct {
	zone string
	key  tea.KeyType
}{{"btn-ok", tea.KeyEnter}, {"btn-cancel", tea.KeyEsc}}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.overlay == ovConfirm || m.overlay == ovHelp {
		_, cmd := m.dlg.Mouse(msg)
		return m, cmd
	}
	if m.overlay == ovNone && m.page == pageSettings {
		// First, so an open dropdown can swallow a click anywhere, the sidebar included.
		if ok, mm, cmd := m.settingsMouse(msg); ok {
			return mm, cmd
		}
	}
	if m.overlay != ovNone {
		if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
			for _, b := range dialogButtons {
				if zone.Get(b.zone).InBounds(msg) {
					return m.handleKey(tea.KeyMsg{Type: b.key})
				}
			}
		}
		return m, nil
	}
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		up := msg.Button == tea.MouseButtonWheelUp
		switch {
		case zone.Get("events").InBounds(msg):
			if up {
				m.eventScroll++
			} else {
				m.eventScroll = max(0, m.eventScroll-1)
			}
		case zone.Get("log").InBounds(msg):
			if up {
				m.logScroll += 3
			} else {
				m.logScroll = max(0, m.logScroll-3)
			}
		default:
			d := 1
			if up {
				d = -1
			}
			cmd := m.move(d)
			return m, cmd
		}
		return m, nil
	}
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if zone.Get(m.toast.CloseZone()).InBounds(msg) {
		m.toast.Close()
		return m, nil
	}
	for i := range pageNames {
		if zone.Get(navZone(page(i))).InBounds(msg) {
			return m.switchPage(page(i))
		}
	}
	if zone.Get("nav/help").InBounds(msg) {
		return m.press("?")
	}
	if zone.Get("nav/quit").InBounds(msg) {
		return m.press("q")
	}
	for _, f := range m.footerKeys() {
		if zone.Get("key-" + f.key).InBounds(msg) {
			return m.footerPress(f.key)
		}
	}
	hit := func(prefix string, n int) int {
		for i := 0; i < n; i++ {
			if zone.Get(fmt.Sprintf("%s-%d", prefix, i)).InBounds(msg) {
				return i
			}
		}
		return -1
	}
	if i := hit("repo", len(m.st.Repos)); i >= 0 {
		m.repoSel, m.focus = i, paneRepos
		return m, nil
	}
	if i := hit("runner", len(m.st.Instances)); i >= 0 {
		m.runnerSel, m.focus = i, paneRunners
		id := fmt.Sprintf("runner-%d", i)
		now := m.now()
		if m.lastClick == id && now.Sub(m.lastAt) < doubleClick {
			m.lastClick = ""
			return m.enter()
		}
		m.lastClick, m.lastAt = id, now
		cmd := m.follow()
		return m, cmd
	}
	if i := hit("hist", len(m.hist)); i >= 0 {
		m.histSel = i
		return m, nil
	}
	return m, nil
}
