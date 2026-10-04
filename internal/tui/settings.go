package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
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

	setSave    = "settings/save"
	setDiscard = "settings/discard"
	setAddRepo = "settings/add_repo"
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
	seq    int           // the last config request number issued
	shown  int           // the request number of the config the form shows
	saving bool
	alert  []string // the daemon's messages from a rejected save

	save, discard *ui.Button
	buttons       map[string]*ui.Button // the repo cards' action buttons, by ID
}

func newSettingsPage() *settingsPage {
	return &settingsPage{
		save:    ui.NewButton(setSave, "Save changes", ui.Primary),
		discard: ui.NewButton(setDiscard, "Discard", ui.Secondary),
	}
}

// Results of a save: the patch's outcome, then the config fetched after it.
type (
	savedMsg struct {
		sent map[string]ui.Value
		err  error
	}
	refetchedMsg struct {
		seq  int
		cfg  *config.Config
		sent map[string]ui.Value
		err  error
	}
)

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

// load merges a freshly fetched config into the form: clean fields take the
// new values, dirty ones keep their edits, a repo now being removed loses its
// edits, and a new repo starts at its config values. It returns the repos that
// no longer exist, whose cards and edits are dropped.
func (s *settingsPage) load(c *config.Config) []string {
	var gone []string
	for _, r := range s.repos {
		if c.Repo(r.Name) == nil {
			gone = append(gone, r.Name)
		}
	}
	s.form.Merge(settingsSpecs(c))
	s.repos = append([]config.Repo{}, c.Repos...)
	return gone
}

// nextSeq numbers a config request. Responses can arrive out of order, so
// one answering an older request than the config already shown is dropped.
func (s *settingsPage) nextSeq() int {
	s.seq++
	return s.seq
}

// loadConfig takes the config fetched by request seq, unless a newer one is
// already shown, and says which repos disappeared. A refresh can move focus
// (its control vanished or became disabled), so focus is scrolled into view.
func (m *Model) loadConfig(seq int, c *config.Config) {
	s := m.settings
	if seq < s.shown {
		return
	}
	s.shown = seq
	m.cfg = c
	focus := s.group.FocusedID()
	if gone := s.load(c); len(gone) > 0 {
		m.toast.Show("repository "+strings.Join(gone, ", ")+" was removed", false, m.now())
	}
	if m.page == pageSettings && m.overlay == ovNone {
		if m.settingsSections(); s.group.FocusedID() != focus {
			m.scrollToFocus()
		}
	}
}

func (s *settingsPage) input(key string) ui.Input { return s.form.Field(key).Input }

// button returns the action button id, creating it on first use.
func (s *settingsPage) button(id, label string, kind ui.ButtonKind) *ui.Button {
	if s.buttons == nil {
		s.buttons = map[string]*ui.Button{}
	}
	b := s.buttons[id]
	if b == nil {
		b = ui.NewButton(id, label, kind)
		s.buttons[id] = b
	}
	b.Label = label
	return b
}

// repoAction runs a repo card's Pause, Resume or Remove button. Pause and
// Resume act at once; Remove asks first. They are actions, not form fields.
func (m Model) repoAction(id string) (tea.Model, tea.Cmd) {
	rest := strings.TrimPrefix(id, "settings/repo/")
	name, act, _ := strings.Cut(rest, "/")
	var repo *config.Repo
	for i := range m.settings.repos {
		if m.settings.repos[i].Name == name {
			repo = &m.settings.repos[i]
		}
	}
	if repo == nil {
		return m, nil
	}
	switch act {
	case "pause":
		if repo.Paused {
			return m, m.action("resumed "+name, func(c context.Context) error { return m.c.Resume(c, name) })
		}
		return m, m.action("paused "+name, func(c context.Context) error { return m.c.Pause(c, name) })
	case "remove":
		return m.openConfirm(fmt.Sprintf("Remove repo %s? Its running jobs finish first.", name), func() tea.Cmd {
			return m.action("removing "+name, func(c context.Context) error { return m.c.RemoveRepo(c, name) })
		})
	}
	return m, nil
}

func (s *settingsPage) row(label, key, desc string) ui.Row {
	f := s.form.Field(key)
	return ui.Row{Label: label, Items: []ui.Widget{f.Input}, Desc: desc, Dirty: f.Dirty()}
}

// checkErrors returns the in-app check failures by field key: a duration that
// does not parse, and a repo's warm above its explicit max. Size, memory and
// CPU values are left to the daemon.
func (s *settingsPage) checkErrors() map[string]string {
	errs := map[string]string{}
	for _, f := range s.form.Fields() {
		if tf, ok := f.Input.(*ui.TextField); ok && tf.Err() != nil {
			errs[f.Key] = tf.Err().Error()
		}
	}
	for _, r := range s.repos {
		if r.Removing {
			continue
		}
		mx, wm := s.input(repoKey(r.Name, "max")).Value(), s.input(repoKey(r.Name, "warm")).Value()
		warm := 1
		if wm.Set {
			warm = wm.Num
		}
		if mx.Set && mx.Num > 0 && warm > mx.Num {
			errs[repoKey(r.Name, "warm")] = "warm must be <= max"
		}
	}
	return errs
}

// buildPatch turns the dirty fields into one ConfigPatch and returns the
// value sent for each key. A repo being removed has no dirty fields: the
// merge that marked it removing dropped its edits.
func (s *settingsPage) buildPatch() (model.ConfigPatch, map[string]ui.Value) {
	var p model.ConfigPatch
	sent := map[string]ui.Value{}
	for _, f := range s.form.Dirty() {
		v := f.Input.Value()
		text, num, list := v.Text, v.Num, append([]string{}, v.List...)
		sent[f.Key] = v
		if rest, ok := strings.CutPrefix(f.Key, "settings/repo/"); ok {
			name, field, _ := strings.Cut(rest, "/")
			if p.Repos == nil {
				p.Repos = map[string]model.RepoPatch{}
			}
			rp := p.Repos[name]
			switch field {
			case "max":
				rp.Max = &num
			case "warm":
				rp.Warm = &num
			case "labels":
				rp.Labels = &list
			case "cleanup":
				rp.CleanupNamePrefixes = &list
			}
			p.Repos[name] = rp
			continue
		}
		switch f.Key {
		case setMode:
			p.Mode = &text
		case setGlobalMax:
			p.GlobalMax = &num
		case setPollInterval:
			p.PollInterval = &text
		case setStartTimeout:
			p.StartTimeout = &text
		case setIdleTimeout:
			p.IdleTimeout = &text
		case setDiskHighWater:
			p.DiskHighWater = &num
		case setBuildCacheKeep:
			p.BuildCacheKeep = &text
		case setHistoryRetention:
			p.HistoryRetention = &text
		case setLabels:
			p.Labels = &list
		case setMemoryMax, setCPUQuota:
			if p.RunnerLimits == nil {
				p.RunnerLimits = &model.RunnerLimitsPatch{}
			}
			if f.Key == setMemoryMax {
				p.RunnerLimits.MemoryMax = &text
			} else {
				p.RunnerLimits.CPUQuota = &text
			}
		}
	}
	return p, sent
}

// fieldName is a field's name as config.yaml spells it, for messages.
func fieldName(key string) string {
	if rest, ok := strings.CutPrefix(key, "settings/repo/"); ok {
		name, field, _ := strings.Cut(rest, "/")
		if field == "cleanup" {
			field = "cleanup_name_prefixes"
		}
		return name + "." + field
	}
	return strings.TrimPrefix(key, "settings/")
}

// saveSettings runs the in-app checks, then sends the dirty fields as one patch.
func (m Model) saveSettings() (tea.Model, tea.Cmd) {
	s := m.settings
	if m.cfg == nil || s.saving || !m.connected {
		return m, nil
	}
	if len(s.checkErrors()) > 0 {
		m.toast.Show("fix the highlighted settings first", true, m.now())
		return m, nil
	}
	p, sent := s.buildPatch()
	if len(sent) == 0 {
		return m, nil
	}
	s.saving, s.alert = true, nil
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return savedMsg{sent, c.PatchConfig(cx, p)}
	}
}

// saved handles the patch's outcome. A rejection keeps the edits and lists
// the daemon's messages. A success fetches the config to reset the saved
// fields; the save stays busy until that refetch is handled, so a second
// save cannot overlap it.
func (m Model) saved(msg savedMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	if msg.err != nil {
		s.saving, m.leaving = false, false // a rejected save stays on Settings
		s.alert = strings.Split(clean(msg.err.Error()), "; ")
		m.toast.Show("settings not saved", true, m.now())
		return m, nil
	}
	m.toast.Show("Settings saved", false, m.now())
	c, seq := m.c, s.nextSeq()
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		var cfg config.Config
		if err := c.Config(cx, &cfg); err != nil {
			return refetchedMsg{seq: seq, sent: msg.sent, err: err}
		}
		return refetchedMsg{seq: seq, cfg: &cfg, sent: msg.sent}
	}
}

// refetched merges the config fetched after a save. Every saved field resets
// to its new base (a reset, not a merge), and a field whose new base differs
// from what was sent means the daemon ignored it.
//
// If the refetch fails, the save stands but cannot be checked: the edits stay
// as typed, a toast says so, and the next periodic refresh brings the form
// up to date.
//
// A save started from the unsaved-changes dialog leaves only from here, once
// the check has run: a failed refetch or an ignored field stays on Settings
// so the warning is seen.
func (m Model) refetched(msg refetchedMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	s.saving = false
	leaving := m.leaving
	m.leaving = false
	if msg.err != nil {
		m.toast.Show("saved, but re-reading the config failed: "+clean(msg.err.Error()), true, m.now())
		return m, nil
	}
	// A newer refresh may already be shown; the reset and the check below
	// then run against it, which reflects the save just as well.
	m.loadConfig(msg.seq, msg.cfg)
	// Controls stay editable while a save is in flight, so a field edited
	// again since the patch was sent keeps its newer edit.
	var keys []string
	for k, v := range msg.sent {
		if f := s.form.Field(k); f != nil && ui.Equal(f.Kind, f.Input.Value(), v) {
			keys = append(keys, k)
		}
	}
	s.form.Reset(keys)
	for _, f := range s.form.Fields() {
		if v, ok := msg.sent[f.Key]; ok && !ui.Equal(f.Kind, f.Base, v) {
			m.toast.Show("daemon did not apply "+fieldName(f.Key)+"; is it older than this ghr?", true, m.now())
			return m, nil
		}
	}
	if leaving {
		return m.goTo(m.leaveTo)
	}
	return m, nil
}

// unsavedBar is the sticky line shown while anything is dirty.
func (m Model) unsavedBar() string {
	s := m.settings
	n := len(s.form.Dirty())
	if n == 0 {
		return ""
	}
	text := fmt.Sprintf("● %d unsaved changes", n)
	if n == 1 {
		text = "● 1 unsaved change"
	}
	focused := s.group.FocusedID()
	return sAmber.Render(text) + "  " + s.discard.View(focused == setDiscard, 0) + "  " + s.save.View(focused == setSave, 0)
}

// alertBox lists the daemon's messages from a rejected save.
func (m Model) alertBox(w int) []string {
	s := m.settings
	if len(s.alert) == 0 {
		return nil
	}
	// At most three messages, so the form keeps room at the 22-row minimum.
	var lines []string
	for _, a := range s.alert[:min(len(s.alert), 3)] {
		lines = append(lines, sRed.Render("✖ "+a))
	}
	if n := len(s.alert) - 3; n > 0 {
		lines = append(lines, sRed.Render(fmt.Sprintf("… and %d more", n)))
	}
	return strings.Split(box("Save rejected", w, lines), "\n")
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
	s.save.Label = "Save changes"
	if s.saving {
		s.save.Label = "Saving…"
	}
	s.save.SetDisabled(s.saving || !m.connected)
	s.discard.SetDisabled(s.saving || !m.connected)
	errs := s.checkErrors()

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
		pauseLabel := "Pause"
		if r.Paused {
			pauseLabel = "Resume"
		}
		pause := s.button(repoKey(r.Name, "pause"), pauseLabel, ui.Primary)
		remove := s.button(repoKey(r.Name, "remove"), "Remove", ui.Danger)
		for _, b := range []*ui.Button{pause, remove} {
			b.SetDisabled(!m.connected || r.Removing)
		}
		title := clean(r.Name)
		card := ui.Section{Title: title, Rows: []ui.Row{
			s.row("Max", repoKey(r.Name, "max"), "once set, it stays explicit"),
			s.warmRow(r.Name, errs),
			s.row("Labels", repoKey(r.Name, "labels"), "added to this repo's runners"),
			s.row("Cleanup prefixes", repoKey(r.Name, "cleanup"), "container name prefixes removed after each job"),
			{Items: []ui.Widget{pause, remove}},
		}}
		switch {
		case r.Removing:
			card.Title, card.Note = title+" (removing…)", "removing… its running jobs finish first"
		case r.Paused:
			card.Title = title + " (paused)"
		}
		secs = append(secs, card)
	}
	add := s.button(setAddRepo, "+ Add repository", ui.Primary)
	add.SetDisabled(!m.connected)
	secs = append(secs, ui.Section{Rows: []ui.Row{{Items: []ui.Widget{add}}}})

	var ws []ui.Widget
	for _, sec := range secs {
		for _, r := range sec.Rows {
			ws = append(ws, r.Items...)
		}
	}
	if len(s.form.Dirty()) > 0 {
		ws = append(ws, s.discard, s.save)
	}
	s.group.Set(ws)
	return secs
}

func (s *settingsPage) warmRow(name string, errs map[string]string) ui.Row {
	r := s.row("Warm", repoKey(name, "warm"), "applies in all mode")
	r.Err = errs[repoKey(name, "warm")]
	return r
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
	case "ctrl+s":
		mm, cmd := m.saveSettings()
		return true, mm, cmd
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
	switch {
	case m.overlay == ovAddRepo && m.add != nil && m.add.group.FocusedID() == id:
		m.add.group.Next()
	case m.overlay == ovNone && m.page == pageSettings && m.settings.group.FocusedID() == id:
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

// settingsBodyH is the number of form lines the page shows at once: the
// content height less the alert box and the unsaved-changes bar.
func (m Model) settingsBodyH() int {
	w, h := m.contentSize()
	h -= len(m.alertBox(w))
	if m.unsavedBar() != "" {
		h--
	}
	return max(h, 1)
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
	focus := s.group.FocusedID()
	sections := m.settingsSections()
	lines, ranges := ui.Render(sections, s.group.FocusedID(), w, m.width >= wideMin)
	top, bar := m.alertBox(w), m.unsavedBar()
	bodyH := h - len(top)
	if bar != "" {
		bodyH--
	}
	bodyH = max(bodyH, 1)
	// Rendering moves focus off a control that a refresh disabled while the page was hidden.
	if f := s.group.FocusedID(); f != focus {
		if r, ok := ranges[f]; ok {
			s.scroll = ui.ScrollTo(s.scroll, bodyH, r)
		}
	}
	s.scroll = min(max(s.scroll, 0), max(len(lines)-bodyH, 0))
	body := lines[s.scroll:min(s.scroll+bodyH, len(lines))]
	out := append(top, zone.Mark("settings/body", strings.Join(body, "\n")))
	if bar != "" {
		// The bar sticks to the bottom of the page.
		for range bodyH - len(body) {
			out = append(out, "")
		}
		out = append(out, bar)
	}
	return strings.Join(out, "\n")
}
