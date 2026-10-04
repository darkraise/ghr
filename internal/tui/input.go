package tui

import (
	"context"
	"fmt"
	"strconv"
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
	case ovConfirm:
		m.overlay = ovNone
		if key == "y" || key == "enter" {
			return m, m.confirmAction()
		}
		return m, nil
	case ovDetail, ovHelp:
		if key == "esc" || key == "q" || key == "enter" || key == "?" {
			m.overlay = ovNone
		}
		return m, nil
	}
	return m.press(key)
}

// press runs the action bound to key; mouse clicks on footer hints call it too.
func (m Model) press(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.overlay = ovHelp
	case "1", "2", "3", "4":
		return m.switchTab(tab(key[0] - '1'))
	case "tab":
		return m.switchTab((m.tab + 1) % 4)
	case "shift+tab":
		return m.switchTab((m.tab + 3) % 4)
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
			m.tab = tabRunners
			cmd := m.follow()
			return m, cmd
		}
	case "r":
		if m.tab == tabHistory {
			m.histRepo = m.cycle(m.histRepo, m.repoNames())
			return m, m.fetchHistory()
		}
	case "c":
		if m.tab == tabHistory {
			m.histConcl = m.cycle(m.histConcl, []string{"success", "failure", "cancelled"})
			return m, m.fetchHistory()
		}
	case "enter":
		return m.enter()
	}
	return m, nil
}

func (m Model) switchTab(t tab) (tea.Model, tea.Cmd) {
	m.tab = t
	switch t {
	case tabRunners:
		cmd := m.follow()
		return m, cmd
	case tabHistory:
		return m, m.fetchHistory()
	case tabConfig:
		return m, m.fetchConfig()
	}
	return m, nil
}

func (m *Model) move(d int) tea.Cmd {
	switch {
	case m.tab == tabHistory:
		m.histSel = clamp(m.histSel+d, len(m.hist))
	case m.tab == tabConfig:
		m.cfgSel = clamp(m.cfgSel+d, len(m.configFields()))
	case m.tab == tabRunners || m.focus == paneRunners:
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
		return nil // unlimited: change it in the Config tab
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

func (m Model) openConfirm(text string, action func() tea.Cmd) (tea.Model, tea.Cmd) {
	m.overlay, m.confirmText, m.confirmAction = ovConfirm, text, action
	return m, nil
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
	return m.tab == tabRunners || (m.tab == tabDashboard && m.focus == paneRunners)
}

func (m Model) repoFocus() bool {
	return m.tab == tabDashboard && m.focus == paneRepos
}

func (m Model) enter() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabHistory:
		if m.histSel < len(m.hist) {
			url := m.hist[m.histSel].HTMLURL
			if url == "" {
				m.flash, m.flashErr = "no run URL recorded", true
				return m, nil
			}
			m.copyFn(url)
			m.flash, m.flashErr = "copied "+url, false
		}
		return m, nil
	case tabConfig:
		fields := m.configFields()
		if m.cfgSel < len(fields) {
			f := fields[m.cfgSel]
			return m.openPrompt("Set "+f.label, f.value, func(v string) tea.Cmd {
				if v == f.value {
					return nil
				}
				p, err := f.apply(v)
				if err != nil {
					return func() tea.Msg { return doneMsg{err: err} }
				}
				return m.action(f.label+" = "+v, func(c context.Context) error { return m.c.PatchConfig(c, p) })
			})
		}
		return m, nil
	}
	if r := m.selectedRunner(); r != nil && (m.tab == tabRunners || m.focus == paneRunners) {
		m.overlay, m.detailID = ovDetail, r.ID
		m.steps, m.containers, m.stepsErr, m.ctrsErr = nil, nil, "", ""
		return m, tea.Batch(m.fetchSteps(), m.fetchContainers())
	}
	return m, nil
}

func intPtr(v string) (*int, error) {
	if v == "" || v == "∞" {
		z := 0
		return &z, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("not a number: %s", v)
	}
	return &n, nil
}

func listPtr(v string) *[]string {
	out := []string{}
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return &out
}

// configFields lists the editable settings of the Config tab.
func (m Model) configFields() []configField {
	c := m.cfg
	if c == nil {
		return nil
	}
	strPatch := func(set func(p *model.ConfigPatch, v *string)) func(string) (model.ConfigPatch, error) {
		return func(v string) (model.ConfigPatch, error) {
			var p model.ConfigPatch
			set(&p, &v)
			return p, nil
		}
	}
	fields := []configField{
		{"mode", c.Mode, strPatch(func(p *model.ConfigPatch, v *string) { p.Mode = v })},
		{"global_max", strconv.Itoa(c.GlobalMax), func(v string) (model.ConfigPatch, error) {
			n, err := intPtr(v)
			return model.ConfigPatch{GlobalMax: n}, err
		}},
		{"start_timeout", c.StartTimeout.String(), strPatch(func(p *model.ConfigPatch, v *string) { p.StartTimeout = v })},
		{"idle_timeout", c.IdleTimeout.String(), strPatch(func(p *model.ConfigPatch, v *string) { p.IdleTimeout = v })},
	}
	for _, r := range c.Repos {
		name := r.Name
		repoPatch := func(rp model.RepoPatch) model.ConfigPatch {
			return model.ConfigPatch{Repos: map[string]model.RepoPatch{name: rp}}
		}
		maxText := "∞"
		if eff := c.EffectiveMax(r); eff > 0 {
			maxText = strconv.Itoa(eff)
		}
		fields = append(fields,
			configField{name + ".max", maxText, func(v string) (model.ConfigPatch, error) {
				n, err := intPtr(v)
				return repoPatch(model.RepoPatch{Max: n}), err
			}},
			configField{name + ".warm", strconv.Itoa(c.EffectiveWarm(r)), func(v string) (model.ConfigPatch, error) {
				n, err := intPtr(v)
				return repoPatch(model.RepoPatch{Warm: n}), err
			}},
			configField{name + ".labels", strings.Join(r.Labels, ","), func(v string) (model.ConfigPatch, error) {
				return repoPatch(model.RepoPatch{Labels: listPtr(v)}), nil
			}},
			configField{name + ".cleanup_name_prefixes", strings.Join(r.CleanupNamePrefixes, ","), func(v string) (model.ConfigPatch, error) {
				return repoPatch(model.RepoPatch{CleanupNamePrefixes: listPtr(v)}), nil
			}},
		)
	}
	return fields
}

// footerKeys are the clickable hints shown in the footer, in order.
var footerKeys = []struct{ key, label string }{
	{"p", "pause"}, {"+", "repo cap"}, {"-", ""}, {"[", "global cap"}, {"]", ""}, {"m", "mode"},
	{"x", "kill"}, {"l", "logs"}, {"enter", "details"}, {"?", "help"}, {"q", "quit"},
}

// dialogButtons map each overlay button zone to the key it stands for.
var dialogButtons = []struct {
	zone string
	key  tea.KeyType
}{{"btn-ok", tea.KeyEnter}, {"btn-cancel", tea.KeyEsc}}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
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
	for i := range tabNames {
		if zone.Get(fmt.Sprintf("tab-%d", i)).InBounds(msg) {
			return m.switchTab(tab(i))
		}
	}
	for _, f := range footerKeys {
		if zone.Get("key-" + f.key).InBounds(msg) {
			return m.press(f.key)
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
	if i := hit("cfg", len(m.configFields())); i >= 0 {
		m.cfgSel = i
		return m.enter()
	}
	return m, nil
}
