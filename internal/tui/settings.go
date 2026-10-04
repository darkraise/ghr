package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/tui/ui"
)

// Settings field keys. They double as control and zone IDs.
const (
	setMode             = "settings/mode"
	setGlobalMax        = "settings/global_max"
	setPollInterval     = "settings/poll_interval"
	setStartTimeout     = "settings/start_timeout"
	setIdleTimeout      = "settings/idle_timeout"
	setDiskHighWater    = "settings/disk_high_water"
	setBuildCacheKeep   = "settings/build_cache_keep"
	setHistoryRetention = "settings/history_retention"
	setLabels           = "settings/labels"
	setMemoryMax        = "settings/memory_max"
	setCPUQuota         = "settings/cpu_quota"
)

// repoKey is the key of a per-repo field: max, warm, labels or cleanup.
func repoKey(name, field string) string { return ui.ZoneID("settings", "repo", name, field) }

var modeOptions = []ui.Option{
	{Value: config.ModeQueue, Label: "queue", Desc: "start runners only for queued jobs, up to the global max"},
	{Value: config.ModeAll, Label: "all", Desc: "keep warm runners per repo, up to each repo's max"},
}

// settingsPage is the Settings form. The Model holds it by pointer, so its
// controls keep their state while Bubble Tea copies the Model.
type settingsPage struct {
	form   ui.Form
	group  ui.Group
	scroll int
	repos  []config.Repo // the repos of the last loaded config, in order
}

func newSettingsPage() *settingsPage { return &settingsPage{} }

func durationCheck(s string) error {
	_, err := config.ParseDuration(s)
	return err
}

func textSpec(key, v string, kind ui.Kind, width int, check func(string) error) ui.Spec {
	return ui.Spec{Key: key, Kind: kind, Base: ui.Value{Text: v}, New: func() ui.Input {
		f := ui.NewTextField(key, width)
		f.Check = check
		return f
	}}
}

// cleanList sanitises config strings for display, as clean does for daemon text.
func cleanList(v []string) []string {
	out := make([]string, len(v))
	for i, s := range v {
		out[i] = clean(s)
	}
	return out
}

func listSpec(key string, v []string) ui.Spec {
	return ui.Spec{Key: key, Kind: ui.KindList, Base: ui.Value{List: cleanList(v)},
		New: func() ui.Input { return ui.NewTagList(key) }}
}

func intSpec(key string, v *int, newStepper func() *ui.Stepper) ui.Spec {
	base := ui.Value{}
	if v != nil {
		base = ui.Value{Num: *v, Set: true}
	}
	return ui.Spec{Key: key, Kind: ui.KindInt, Base: base, New: func() ui.Input { return newStepper() }}
}

// settingsSpecs lists every editable setting of c except owner, in page order.
func settingsSpecs(c *config.Config) []ui.Spec {
	gm, disk := c.GlobalMax, c.DiskHighWater
	specs := []ui.Spec{
		{Key: setMode, Kind: ui.KindText, Base: ui.Value{Text: c.Mode},
			New: func() ui.Input { return ui.NewSelect(setMode, modeOptions) }},
		intSpec(setGlobalMax, &gm, func() *ui.Stepper { return ui.NewStepper(setGlobalMax, 1, 99, 1) }),
		textSpec(setPollInterval, c.PollInterval.String(), ui.KindDuration, 10, durationCheck),
		textSpec(setStartTimeout, c.StartTimeout.String(), ui.KindDuration, 10, durationCheck),
		textSpec(setIdleTimeout, c.IdleTimeout.String(), ui.KindDuration, 10, durationCheck),
		intSpec(setDiskHighWater, &disk, func() *ui.Stepper {
			s := ui.NewStepper(setDiskHighWater, 1, 100, 5)
			s.Suffix = "%"
			return s
		}),
		textSpec(setBuildCacheKeep, clean(c.BuildCacheKeep), ui.KindText, 10, nil),
		textSpec(setHistoryRetention, c.HistoryRetention.String(), ui.KindDuration, 10, durationCheck),
		listSpec(setLabels, c.Labels),
		textSpec(setMemoryMax, clean(c.RunnerLimits.MemoryMax), ui.KindText, 10, nil),
		textSpec(setCPUQuota, clean(c.RunnerLimits.CPUQuota), ui.KindText, 10, nil),
	}
	for _, r := range c.Repos {
		maxKey, warmKey := repoKey(r.Name, "max"), repoKey(r.Name, "warm")
		repo := []ui.Spec{
			intSpec(maxKey, r.Max, func() *ui.Stepper {
				s := ui.NewStepper(maxKey, 0, 99, 1)
				s.ZeroText = "∞"
				return s
			}),
			intSpec(warmKey, r.Warm, func() *ui.Stepper {
				s := ui.NewStepper(warmKey, 0, 99, 1)
				s.Default = 1
				return s
			}),
			listSpec(repoKey(r.Name, "labels"), r.Labels),
			listSpec(repoKey(r.Name, "cleanup"), r.CleanupNamePrefixes),
		}
		for i := range repo {
			repo[i].Locked = r.Removing
		}
		specs = append(specs, repo...)
	}
	return specs
}

// load merges a freshly fetched config into the form.
func (s *settingsPage) load(c *config.Config) {
	s.form.Merge(settingsSpecs(c))
	s.repos = append([]config.Repo{}, c.Repos...)
}

func (s *settingsPage) input(key string) ui.Input { return s.form.Field(key).Input }

func (s *settingsPage) row(label, key, desc string) ui.Row {
	f := s.form.Field(key)
	return ui.Row{Label: label, Items: []ui.Widget{f.Input}, Desc: desc, Dirty: f.Dirty()}
}

// settingsSections lays out the form as cards. It first brings the controls
// up to date with the rest of the model (disabled while the daemon is
// unreachable or the repo is being removed, repo defaults that follow the
// edited mode) and sets the focus order to the layout order.
func (m Model) settingsSections() []ui.Section {
	s := m.settings
	queue := s.input(setMode).Value().Text == config.ModeQueue
	removing := map[string]bool{}
	for _, r := range s.repos {
		removing[r.Name] = r.Removing
	}
	for _, f := range s.form.Fields() {
		off := !m.connected
		if rest, ok := strings.CutPrefix(f.Key, "settings/repo/"); ok {
			name, _, _ := strings.Cut(rest, "/")
			off = off || removing[name]
		}
		f.Input.SetDisabled(off)
	}

	secs := []ui.Section{
		{Title: "General", Rows: []ui.Row{
			s.row("Mode", setMode, s.input(setMode).(*ui.Select).Selected().Desc),
			s.row("Global max", setGlobalMax, "applies in queue mode"),
			{Label: "Owner", Text: clean(m.cfg.Owner), Desc: "change in config.yaml and restart the daemon"},
		}},
		{Title: "Timing", Rows: []ui.Row{
			s.row("Poll interval", setPollInterval, "how often GitHub is checked (at least 5s)"),
			s.row("Start timeout", setStartTimeout, "a runner not online by then is replaced"),
			s.row("Idle timeout", setIdleTimeout, "idle runners beyond warm stop after this"),
		}},
		{Title: "Disk and retention", Rows: []ui.Row{
			s.row("Disk high-water", setDiskHighWater, "disk use that triggers pruning"),
			s.row("Build cache keep", setBuildCacheKeep, "build cache kept when pruning, e.g. 20GB"),
			s.row("History retention", setHistoryRetention, "history and logs older than this are removed (at least 1d)"),
		}},
		{Title: "Runner defaults", Note: "limits apply to newly started runners", Rows: []ui.Row{
			s.row("Global labels", setLabels, "added to every runner"),
			s.row("Memory max", setMemoryMax, "per runner, e.g. 6G, 50% or infinity"),
			s.row("CPU quota", setCPUQuota, "per runner, e.g. 200%"),
		}},
	}
	for _, r := range s.repos {
		mx := s.input(repoKey(r.Name, "max")).(*ui.Stepper)
		mx.Default, mx.DefaultText = 1, ""
		if !queue {
			mx.Default, mx.DefaultText = 0, "∞"
		}
		title := clean(r.Name)
		card := ui.Section{Title: title, Rows: []ui.Row{
			s.row("Max", repoKey(r.Name, "max"), "once set, it stays explicit"),
			s.row("Warm", repoKey(r.Name, "warm"), "applies in all mode"),
			s.row("Labels", repoKey(r.Name, "labels"), "added to this repo's runners"),
			s.row("Cleanup prefixes", repoKey(r.Name, "cleanup"), "container name prefixes removed after each job"),
		}}
		switch {
		case r.Removing:
			card.Title, card.Note = title+" (removing…)", "removing… its running jobs finish first"
		case r.Paused:
			card.Title = title + " (paused)"
		}
		secs = append(secs, card)
	}

	var ws []ui.Widget
	for _, sec := range secs {
		for _, r := range sec.Rows {
			ws = append(ws, r.Items...)
		}
	}
	s.group.Set(ws)
	return secs
}

// settingsKey handles a key on the Settings page in precedence order: the
// focused control first, then the page keys. It reports false for keys that
// fall through to the global keys.
func (m Model) settingsKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	if m.cfg == nil {
		// Still loading: the config keys stay inactive here all the same.
		switch k.String() {
		case "m", "[", "]", "+", "=", "-":
			return true, m, nil
		}
		return false, m, nil
	}
	s := m.settings
	m.settingsSections()
	if ok, cmd := s.group.Key(k); ok {
		m.scrollToFocus()
		return true, m, cmd
	}
	switch k.String() {
	case "tab", "down", "j":
		s.group.Next()
	case "shift+tab", "up", "k":
		s.group.Prev()
	case "pgup":
		s.scroll = max(s.scroll-m.settingsBodyH()/2, 0)
		return true, m, nil
	case "pgdown":
		s.scroll += m.settingsBodyH() / 2
		return true, m, nil
	case "esc", "m", "[", "]", "+", "=", "-":
		// Nothing to back out of; config edits go through the form on this page.
		return true, m, nil
	default:
		return false, m, nil
	}
	m.scrollToFocus()
	return true, m, nil
}

// settingsMouse handles the wheel over the form and clicks on its controls.
// It reports false for events the shell should handle.
func (m Model) settingsMouse(msg tea.MouseMsg) (bool, tea.Model, tea.Cmd) {
	if m.cfg == nil {
		return false, m, nil
	}
	s := m.settings
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		if !zone.Get("settings/body").InBounds(msg) {
			return false, m, nil
		}
		if msg.Button == tea.MouseButtonWheelUp {
			s.scroll = max(s.scroll-3, 0)
		} else {
			s.scroll += 3
		}
		return true, m, nil
	}
	m.settingsSections()
	ok, cmd := s.group.Mouse(msg)
	return ok, m, cmd
}

// advance moves focus past the control that asked for it (enter in a text field).
func (m Model) advance(id string) (tea.Model, tea.Cmd) {
	if m.page == pageSettings && m.settings.group.FocusedID() == id {
		m.settings.group.Next()
		m.scrollToFocus()
	}
	return m, nil
}

// settingsFooterKeys are the footer hints for the focused control.
func (m Model) settingsFooterKeys() []footerKey {
	switch w := m.settings.group.Focused().(type) {
	case *ui.Select:
		if w.Open() {
			return []footerKey{{"up", "move"}, {"enter", "pick"}, {"esc", "close"}}
		}
	case *ui.TextField:
		if w.Editing() {
			return []footerKey{{"enter", "commit"}, {"esc", "stop editing"}, {"tab", "next"}}
		}
	case *ui.TagList:
		if w.Adding() {
			return []footerKey{{"enter", "add"}, {"esc", "done"}, {"tab", "next"}}
		}
	case *ui.Stepper:
		return []footerKey{{"left", "less"}, {"right", "more"}, {"tab", "next"}, {"ctrl+s", "save"}, {"?", "help"}, {"q", "quit"}}
	}
	return []footerKey{{"tab", "next"}, {"shift+tab", "previous"}, {"ctrl+s", "save"}, {"?", "help"}, {"q", "quit"}}
}

// settingsBodyH is the number of form lines the page shows at once.
func (m Model) settingsBodyH() int {
	_, h := m.contentSize()
	return h
}

// scrollToFocus scrolls the focused control into view. It uses the line
// ranges the layout returns, not zone positions, which describe the last frame.
func (m Model) scrollToFocus() {
	s := m.settings
	w, _ := m.contentSize()
	_, ranges := ui.Render(m.settingsSections(), s.group.FocusedID(), w, m.width >= wideMin)
	if r, ok := ranges[s.group.FocusedID()]; ok {
		s.scroll = ui.ScrollTo(s.scroll, m.settingsBodyH(), r)
	}
}

// settingsView renders the form scrolled to the page's offset in w columns and h lines.
func (m Model) settingsView(w, h int) string {
	if m.cfg == nil {
		return sDim.Render("loading…")
	}
	s := m.settings
	lines, _ := ui.Render(m.settingsSections(), s.group.FocusedID(), w, m.width >= wideMin)
	s.scroll = min(max(s.scroll, 0), max(len(lines)-h, 0))
	return zone.Mark("settings/body", strings.Join(lines[s.scroll:min(s.scroll+h, len(lines))], "\n"))
}
