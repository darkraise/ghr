package tui

import (
	"context"
	"fmt"
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
	case ovAddRepo:
		return m.addRepoKey(k)
	case ovToken:
		return m.tokenKey(k)
	case ovConfirm, ovHelp, ovUnsaved:
		return m.dialogKey(k)
	}
	if m.page == pageDetail {
		if ok, mm, cmd := m.detailKey(k); ok {
			return mm, cmd
		}
	}
	if m.page == pageSettings {
		if ok, mm, cmd := m.settingsKey(k); ok {
			return mm, cmd
		}
	}
	if m.page == pageStorage {
		if ok, mm, cmd := m.storageKey(k); ok {
			return mm, cmd
		}
	}
	if m.page == pageRepos {
		if ok, mm, cmd := m.reposHandleKey(k); ok {
			return mm, cmd
		}
	}
	if m.page == pageDashboard {
		if ok, mm, cmd := m.dashKey(k); ok {
			return mm, cmd
		}
	}
	if m.page == pageHistory {
		if ok, mm, cmd := m.histKey(k); ok {
			return mm, cmd
		}
	}
	return m.press(key)
}

// press runs the action bound to key; mouse clicks on footer hints call it too.
func (m Model) press(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m.leave(leaveTarget{quit: true})
	case "?":
		return m.openHelp()
	case "1", "2", "3", "4", "5", "6":
		return m.leave(leaveTarget{page: page(key[0] - '1')})
	case "left", "h":
		m.focusCard(paneRepos)
	case "right":
		m.focusCard(paneRunners)
	case "up", "k", "down", "j":
		d := 1
		if key == "up" || key == "k" {
			d = -1
		}
		cmd := m.move(d)
		return m, cmd
	case "pgup":
		if m.page == pageDashboard {
			m.eventScroll += 5
		}
		if m.logTarget() != "" {
			m.logScroll += 10
		}
		if m.page == pageDetail {
			m.scrollDetail(-10)
		}
		if m.page == pageHistory {
			m.histSel = clamp(m.histSel-10, len(m.hist))
		}
	case "pgdown":
		if m.page == pageDashboard {
			m.eventScroll = max(0, m.eventScroll-5)
		}
		if m.logTarget() != "" {
			m.logScroll = max(0, m.logScroll-10)
		}
		if m.page == pageDetail {
			m.scrollDetail(10)
		}
		if m.page == pageHistory {
			m.histSel = clamp(m.histSel+10, len(m.hist))
		}
	case "p":
		if !m.repoFocus() || m.offline() {
			return m, nil
		}
		return m, m.togglePause()
	case "P":
		if m.offline() {
			return m, nil
		}
		return m, m.togglePauseAll()
	case "+", "=", "-":
		if !m.repoFocus() || m.offline() {
			return m, nil
		}
		return m, m.repoCap(key != "-")
	case "[", "]":
		if m.offline() {
			return m, nil
		}
		return m, m.globalCap(key == "]")
	case "m":
		if m.offline() {
			return m, nil
		}
		mode := config.ModeAll
		if m.st.Mode == config.ModeAll {
			mode = config.ModeQueue
		}
		return m, m.action("mode "+mode, func(c context.Context) error {
			return m.c.PatchConfig(c, model.ConfigPatch{Mode: &mode})
		})
	case "e":
		if r := m.selectedRepo(); r != nil && m.repoFocus() {
			m.repos.selected = r.Name
			m.repos.group.Focus(reposList)
			return m.leave(leaveTarget{page: pageRepos})
		}
	case "a":
		return m.openAddRepo()
	case "d":
		if r := m.selectedRepo(); r != nil && m.repoFocus() && !m.offline() {
			name := r.Name
			return m.openConfirm(fmt.Sprintf("Remove repo %s? Its running jobs finish first.", name), func() tea.Cmd {
				return m.action("removing "+name, func(c context.Context) error { return m.c.RemoveRepo(c, name) })
			})
		}
	case "x":
		if r := m.selectedRunner(); r != nil && m.runnerFocus() && !m.offline() {
			id, busy := r.ID, r.State == "busy"
			return m.stopRunner(id, busy)
		}
	case "l":
		if r := m.selectedRunner(); r != nil && m.runnerFocus() {
			return m.openDetail(r.ID, tabLog)
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

// offline reports whether the daemon is unreachable, saying so in a toast.
// The keys that change daemon state check it, as their disabled buttons would.
func (m *Model) offline() bool {
	if !m.connected {
		m.toast.Show("the daemon is unreachable; try again once it reconnects", true, m.now())
	}
	return !m.connected
}

func (m Model) switchPage(p page) (tea.Model, tea.Cmd) {
	m.page = p
	switch p {
	case pageRunners:
		cmd := m.follow()
		return m, cmd
	case pageHistory:
		return m, m.fetchHistory()
	case pageStorage:
		return m, m.fetchStorage()
	case pageSettings:
		return m, tea.Batch(m.fetchConfig(), m.fetchToken())
	case pageRepos:
		m.mg.cardsRepo = ""
		return m, tea.Batch(m.fetchConfig(), m.refreshRepoCards())
	}
	return m, nil
}

func (m *Model) move(d int) tea.Cmd {
	switch {
	case m.page == pageHistory:
		if m.groups.hist.FocusedID() == histTable {
			m.histSel = clamp(m.histSel+d, len(m.hist))
		}
	case m.page == pageDetail:
		m.scrollDetail(d)
	case m.page == pageSettings, m.page == pageStorage, m.page == pageDashboard && !m.onCard():
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
	if m.allPaused() {
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

func (m Model) runnerFocus() bool {
	return m.page == pageRunners || (m.page == pageDashboard && m.onCard() && m.focus == paneRunners)
}

func (m Model) repoFocus() bool {
	return m.page == pageDashboard && m.onCard() && m.focus == paneRepos
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
	case pageSettings, pageStorage:
		return m, nil
	}
	if r := m.selectedRunner(); r != nil && m.runnerFocus() {
		return m.openDetail(r.ID, tabSteps)
	}
	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.overlay == ovConfirm || m.overlay == ovHelp || m.overlay == ovUnsaved {
		_, cmd := m.dlg.Mouse(msg)
		return m, cmd
	}
	if m.overlay == ovAddRepo {
		m.add.sync(m.connected)
		wheel := msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown)
		if wheel && m.add.picker.Hit(msg) {
			m.add.picker.Update(msg)
			return m, nil
		}
		_, cmd := m.add.group.Mouse(msg)
		return m, cmd
	}
	if m.overlay == ovToken {
		m.syncToken()
		_, cmd := m.tok.group.Mouse(msg)
		return m, cmd
	}
	if m.overlay == ovNone && m.page == pageSettings {
		// First, so an open dropdown can swallow a click anywhere, the sidebar included.
		if ok, mm, cmd := m.settingsMouse(msg); ok {
			return mm, cmd
		}
	}
	if m.overlay == ovNone && m.page == pageRepos {
		if ok, mm, cmd := m.reposMouse(msg); ok {
			return mm, cmd
		}
	}
	if m.overlay == ovNone && m.page == pageStorage {
		if ok, mm, cmd := m.storageMouse(msg); ok {
			return mm, cmd
		}
	}
	if m.overlay == ovNone && m.page == pageDashboard {
		if ok, cmd := m.groups.dash.Mouse(msg); ok {
			return m, cmd
		}
	}
	if m.overlay == ovNone && m.page == pageDetail {
		m.detailButtons()
		tab := m.groups.tabs.active
		if ok, cmd := m.groups.detail.Mouse(msg); ok {
			if m.groups.tabs.active != tab {
				m.detailScroll = 0
			}
			follow := m.follow() // a click on Log points the log at this runner at once
			return m, tea.Batch(cmd, follow)
		}
	}
	if m.overlay == ovNone && m.page == pageHistory {
		// Before the sidebar, so an open dropdown can swallow a click anywhere.
		m.histFilters()
		if ok, _ := m.groups.hist.Mouse(msg); ok {
			cmd := m.histApply()
			return m, cmd
		}
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
			return m.leave(leaveTarget{page: page(i)})
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
	// A row button runs its key, so the row keys and the buttons stay one path.
	for id, k := range map[string]string{rowEdit: "e", rowPause: "p", rowRemove: "d", rowLogs: "l", rowStop: "x", rowCopy: "enter"} {
		if zone.Get(id).InBounds(msg) {
			return m.press(k)
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
		m.repoSel = i
		m.focusCard(paneRepos)
		return m, nil
	}
	if i := hit("runner", len(m.st.Instances)); i >= 0 {
		m.runnerSel = i
		m.focusCard(paneRunners)
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
		m.groups.hist.Focus(histTable)
		return m, nil
	}
	return m, nil
}
