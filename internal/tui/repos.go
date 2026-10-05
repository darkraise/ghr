package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	reposList    = "repos/list"    // the list card's tab stop
	reposAdd     = "repos/add"     // the header's Add repository button
	reposPause   = "repos/pause"   // the panel's Pause/Resume button
	reposRemove  = "repos/remove"  // the panel's Remove button
	reposSave    = "repos/save"    // the unsaved bar's Save button
	reposDiscard = "repos/discard" // the unsaved bar's Discard button

	reposListW = 30 // the list card's width in the wide layout
)

// reposPage is the Repositories page. The Model holds it by pointer, so its
// state survives Bubble Tea copying the Model.
type reposPage struct {
	configPage
	selected      string // the selected repo's name; selection follows the name across refreshes
	idx           int    // the selection's last position, used when its repo disappears
	shownFocus    string // the focus reposView last scrolled to
	add           *ui.Button
	pause, remove *ui.Button
}

func newReposPage() *reposPage {
	rp := &reposPage{
		configPage: newConfigPage(reposSave, reposDiscard),
		add:        ui.NewButton(reposAdd, "+ Add repository", ui.Primary),
		pause:      ui.NewButton(reposPause, "Pause", ui.Secondary),
		remove:     ui.NewButton(reposRemove, "Remove", ui.Danger),
	}
	rp.group.Set([]ui.Widget{rp.add, stop{reposList}})
	rp.group.Focus(reposList)
	return rp
}

// repoIndex resolves the selection by name, falling back to the repo now at
// the selection's last position when its repo has gone; -1 with no repos.
func (m Model) repoIndex() int {
	rp := m.repos
	for i, r := range m.st.Repos {
		if strings.EqualFold(r.Name, rp.selected) {
			rp.idx = i
			return i
		}
	}
	if len(m.st.Repos) == 0 {
		return -1
	}
	i := min(max(rp.idx, 0), len(m.st.Repos)-1)
	rp.selected, rp.idx = m.st.Repos[i].Name, i
	return i
}

func (m Model) selectedRepoStatus() *model.RepoStatus {
	if i := m.repoIndex(); i >= 0 {
		return &m.st.Repos[i]
	}
	return nil
}

func (m Model) moveRepo(d int) {
	if i := m.repoIndex(); i >= 0 {
		j := clamp(i+d, len(m.st.Repos))
		m.repos.selected, m.repos.idx = m.st.Repos[j].Name, j
	}
}

// repoState is the state a repo's badge shows.
func repoState(r model.RepoStatus) string {
	switch {
	case r.Error != "":
		return "error"
	case r.Removing:
		return "removing"
	case r.Paused:
		return "paused"
	}
	return "active"
}

// reposKey is a repo field's key (max, warm, labels or cleanup); it doubles
// as the control and zone ID.
func reposKey(name, field string) string { return ui.ZoneID("repos", name, field) }

// splitReposKey splits repos/<name>/<field>. The field never contains a
// slash, so the last one separates them.
func splitReposKey(key string) (name, field string) {
	rest := strings.TrimPrefix(key, "repos/")
	i := strings.LastIndex(rest, "/")
	return rest[:i], rest[i+1:]
}

// reposSpecs lists every repo's editable fields; a repo being removed is locked.
func reposSpecs(c *config.Config) []ui.Spec {
	var specs []ui.Spec
	for _, r := range c.Repos {
		maxKey, warmKey := reposKey(r.Name, "max"), reposKey(r.Name, "warm")
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
			listSpec(reposKey(r.Name, "labels"), r.Labels),
			listSpec(reposKey(r.Name, "cleanup"), r.CleanupNamePrefixes),
		}
		for i := range repo {
			repo[i].Locked = r.Removing
		}
		specs = append(specs, repo...)
	}
	return specs
}

// repoInputs are the selected repo's form inputs in panel order; nil until
// the config has loaded.
func (m Model) repoInputs(name string) []ui.Input {
	var out []ui.Input
	for _, f := range []string{"max", "warm", "labels", "cleanup"} {
		fl := m.repos.form.Field(reposKey(name, f))
		if fl == nil {
			return nil
		}
		out = append(out, fl.Input)
	}
	return out
}

// repoDirty reports whether the repo has unsaved edits.
func (m Model) repoDirty(name string) bool {
	for _, f := range m.repos.form.Dirty() {
		if n, _ := splitReposKey(f.Key); n == name {
			return true
		}
	}
	return false
}

// reposCheckErrors returns the in-app check failures: a warm count above
// the repo's explicit max.
func (m Model) reposCheckErrors() map[string]string {
	errs := map[string]string{}
	if m.cfg == nil {
		return errs
	}
	for _, r := range m.cfg.Repos {
		mx, wm := m.repos.form.Field(reposKey(r.Name, "max")), m.repos.form.Field(reposKey(r.Name, "warm"))
		if r.Removing || mx == nil || wm == nil {
			continue
		}
		warm := 1
		if v := wm.Input.Value(); v.Set {
			warm = v.Num
		}
		if v := mx.Input.Value(); v.Set && v.Num > 0 && warm > v.Num {
			errs[reposKey(r.Name, "warm")] = "warm must be <= max"
		}
	}
	return errs
}

// reposPatch turns the dirty repo fields into one config patch.
func (m Model) reposPatch() (model.ConfigPatch, map[string]ui.Value) {
	var p model.ConfigPatch
	sent := map[string]ui.Value{}
	for _, f := range m.repos.form.Dirty() {
		v := f.Input.Value()
		sent[f.Key] = v
		name, field := splitReposKey(f.Key)
		if p.Repos == nil {
			p.Repos = map[string]model.RepoPatch{}
		}
		rp := p.Repos[name]
		num, list := v.Num, append([]string{}, v.List...)
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
	}
	return p, sent
}

// saveRepos runs the in-app checks, then sends the dirty repo fields as one
// patch. While the daemon is unreachable it says so (the Save button is
// disabled then, but ctrl+s still reaches here).
func (m Model) saveRepos() (tea.Model, tea.Cmd) {
	rp := m.repos
	if m.cfg == nil || rp.saving || m.offline() {
		return m, nil
	}
	if len(m.reposCheckErrors()) > 0 {
		m.toast.Show("fix the highlighted settings first", true, m.now())
		return m, nil
	}
	p, sent := m.reposPatch()
	if len(sent) == 0 {
		return m, nil
	}
	rp.saving, rp.alert = true, nil
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return savedMsg{pageRepos, sent, c.PatchConfig(cx, p)}
	}
}

// runnersGet lists the labels a newly started runner for repo name carries
// once the edits are saved: the fixed system labels, the saved global labels
// (marked global) and the draft repo labels, normalised as the daemon does.
// The lines are logical; ui.Row wraps them to the panel.
func (m Model) runnersGet(name string) []string {
	r := m.cfg.Repo(name)
	if r == nil {
		return nil
	}
	draft := *r
	if f := m.repos.form.Field(reposKey(name, "labels")); f != nil {
		draft.Labels = f.Input.Value().List
	}
	global := map[string]bool{}
	for _, l := range m.cfg.CustomLabels(config.Repo{}) {
		global[l] = true
	}
	var custom []string
	for _, l := range m.cfg.CustomLabels(draft) {
		if global[l] {
			l += " (global)"
		}
		custom = append(custom, clean(l))
	}
	lines := []string{strings.Join(config.SystemLabels, " ")}
	if len(custom) > 0 {
		lines = append(lines, strings.Join(custom, "  "))
	}
	return append(lines, sDim.Render("runners already running keep their labels"))
}

// repoSections lays out the selected repo's editable cards in w columns.
func (m Model) repoSections(r model.RepoStatus, w int) []ui.Section {
	rp := m.repos
	field := func(f string) *ui.Field { return rp.form.Field(reposKey(r.Name, f)) }
	row := func(label, f, desc string) ui.Row {
		fl := field(f)
		return ui.Row{Label: label, Items: []ui.Widget{fl.Input}, Desc: desc, Dirty: fl.Dirty()}
	}
	warm := row("Warm", "warm", "applies in all mode")
	warm.Err = m.reposCheckErrors()[reposKey(r.Name, "warm")]
	return []ui.Section{
		{Title: "Capacity", Rows: []ui.Row{row("Max", "max", "once set, it stays explicit"), warm}},
		{Title: "Labels", Rows: []ui.Row{
			row("Repo labels", "labels", "added to this repo's runners"),
			{Label: "Runners get", Lines: m.runnersGet(r.Name)},
		}},
		{Title: "Cleanup", Rows: []ui.Row{row("Prefixes", "cleanup", "container name prefixes removed after each job")}},
	}
}

// syncRepoControls updates the selected repo's controls (disabled while the
// daemon is unreachable or the repo is being removed), gives every repo's
// Max stepper the default of the saved mode, and sets the page's focus
// order: header button, list, panel buttons, the repo's inputs, then Discard
// and Save while anything is dirty. It runs before every key, click and frame.
func (m Model) syncRepoControls() {
	rp := m.repos
	if m.cfg != nil {
		def, text := 1, ""
		if m.cfg.Mode == config.ModeAll {
			def, text = 0, "∞"
		}
		for _, r := range m.cfg.Repos {
			if f := rp.form.Field(reposKey(r.Name, "max")); f != nil {
				s := f.Input.(*ui.Stepper)
				s.Default, s.DefaultText = def, text
			}
		}
	}
	items := []ui.Widget{rp.add, stop{reposList}}
	rp.add.SetDisabled(!m.connected)
	if r := m.selectedRepoStatus(); r != nil {
		rp.pause.Label = "Pause"
		if r.Paused {
			rp.pause.Label = "Resume"
		}
		off := !m.connected || r.Removing
		for _, b := range []*ui.Button{rp.pause, rp.remove} {
			b.SetDisabled(off)
		}
		items = append(items, rp.pause, rp.remove)
		for _, in := range m.repoInputs(r.Name) {
			in.SetDisabled(off)
			items = append(items, in)
		}
	}
	rp.save.Label = "Save changes"
	if rp.saving {
		rp.save.Label = "Saving…"
	}
	rp.save.SetDisabled(rp.saving || !m.connected)
	rp.discard.SetDisabled(rp.saving || !m.connected)
	if len(rp.form.Dirty()) > 0 {
		items = append(items, rp.discard, rp.save)
	}
	rp.group.Set(items)
}

// reposListCard renders the list card, w columns wide and h lines tall.
func (m Model) reposListCard(w, h int) string {
	sel := m.repoIndex()
	focused := m.repos.group.FocusedID() == reposList
	var lines []string
	for i, r := range m.st.Repos {
		cursor := "  "
		if i == sel {
			cursor = "› "
		}
		after := ""
		if m.repoDirty(r.Name) {
			after = " " + sAmber.Render("●")
		}
		row := ui.BadgeRow(sSel, i == sel && focused, cursor+cell(clean(r.Name), 11)+" ", stateBadge(repoState(r)), after, w-4)
		lines = append(lines, zone.Mark(fmt.Sprintf("repos/row/%d", i), row))
	}
	title := "Repos"
	if focused {
		title = "› Repos"
	}
	return box(title, w, fit(lines, 0, max(sel, 0), max(h-2, 1)))
}

// repoSummary is the panel's first lines: state, counts, the last job and the actions.
func (m Model) repoSummary(r model.RepoStatus) []string {
	f := m.repos.group.FocusedID()
	counts := fmt.Sprintf("%d/%s running · %d queued", r.Active, maxText(r.Max), r.Queued)
	last := sDim.Render("no finished jobs yet")
	if j := r.LastJob; j != nil {
		icon, style := "✔", sGreen
		if j.Conclusion != "success" {
			icon, style = "✖", sRed
		}
		last = style.Render(icon) + fmt.Sprintf(" #%s %s · %s", j.RunNumber, j.JobName, ago(m.now().Sub(j.FinishedAt)))
	}
	out := []string{stateBadge(repoState(r)) + "  " + counts, last}
	if r.Removing {
		out = append(out, sAmber.Render("removing… running jobs finish first"))
	}
	if r.Error != "" {
		out = append(out, sRed.Render(r.Error))
	}
	return append(out, m.repos.pause.View(f == reposPause, 0)+"  "+m.repos.remove.View(f == reposRemove, 0))
}

// reposPanelLines renders the panel w columns wide with the line range of
// every control, for scrolling the focused one into view.
func (m Model) reposPanelLines(r model.RepoStatus, w int) ([]string, map[string]ui.Range) {
	top := strings.Split(box(clean(r.Name), w, m.repoSummary(r)), "\n")
	if m.cfg == nil || m.repoInputs(r.Name) == nil {
		return append(top, sDim.Render("loading…")), nil
	}
	lines, ranges := ui.Render(m.repoSections(r, w), m.repos.group.FocusedID(), w, w >= 70)
	for k, rg := range ranges {
		ranges[k] = ui.Range{Start: rg.Start + len(top), End: rg.End + len(top)}
	}
	return append(top, lines...), ranges
}

// reposNames is the unsaved bar's list of repos with edits.
func (m Model) reposNames() string {
	var names []string
	for _, r := range m.st.Repos {
		if m.repoDirty(r.Name) {
			names = append(names, clean(r.Name))
		}
	}
	return strings.Join(names, ", ")
}

// reposPanelSize is the panel's width and visible height on a w by h page.
func (m Model) reposPanelSize(w, h int) (int, int) {
	pw := w
	if m.width >= wideMin {
		pw = w - reposListW - 1
	} else {
		h -= min(len(m.st.Repos)+2, max(h/3, 3))
	}
	h -= len(m.alertBox(&m.repos.configPage, pw))
	if len(m.repos.form.Dirty()) > 0 {
		h--
	}
	return pw, max(h, 1)
}

func (m Model) reposScrollToFocus() {
	r := m.selectedRepoStatus()
	if r == nil {
		return
	}
	w, h := m.contentSize()
	pw, ph := m.reposPanelSize(w, h)
	if _, ranges := m.reposPanelLines(*r, pw); ranges != nil {
		if rg, ok := ranges[m.repos.group.FocusedID()]; ok {
			m.repos.scroll = ui.ScrollTo(m.repos.scroll, ph, rg)
		}
	}
}

// reposView renders the page in w columns and h lines: the list card and
// the panel side by side when wide, stacked otherwise.
func (m Model) reposView(w, h int) string {
	m.syncRepoControls()
	rp := m.repos
	r := m.selectedRepoStatus()
	if r == nil {
		return box("Repos", min(w, reposListW), []string{sDim.Render("No repositories yet"), "",
			rp.add.View(rp.group.FocusedID() == reposAdd, 0)})
	}
	// Focus moves by Tab, by a control's own enter-to-advance (Group.Key),
	// by a click, or by Group.Set when a refresh disables the focused
	// control; checking here, once per frame, catches every one of them.
	if f := rp.group.FocusedID(); f != rp.shownFocus {
		rp.shownFocus = f
		m.reposScrollToFocus()
	}
	pw, ph := m.reposPanelSize(w, h)
	lines, _ := m.reposPanelLines(*r, pw)
	rp.scroll = min(max(rp.scroll, 0), max(len(lines)-ph, 0))
	panel := append(m.alertBox(&rp.configPage, pw), lines[rp.scroll:min(rp.scroll+ph, len(lines))]...)
	body := strings.Join(panel, "\n")
	if m.width >= wideMin {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.reposListCard(reposListW, h), " ", zone.Mark("repos/panel", body))
	} else {
		body = m.reposListCard(w, min(len(m.st.Repos)+2, max(h/3, 3))) + "\n" + zone.Mark("repos/panel", body)
	}
	if bar := m.unsavedBar(&rp.configPage, w, m.reposNames()); bar != "" {
		body = fitLines(body, h-1) + "\n" + bar
	}
	return body
}

// reposHandleKey handles the page's keys. It reports false for keys that fall
// through to the global keys.
func (m Model) reposHandleKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	rp := m.repos
	m.syncRepoControls()
	if ok, cmd := rp.group.Key(k); ok {
		return true, m, cmd
	}
	switch k.String() {
	case "tab":
		rp.group.Next()
	case "shift+tab":
		rp.group.Prev()
	case "up", "k", "down", "j":
		if rp.group.FocusedID() == reposList {
			d := 1
			if k.String() == "up" || k.String() == "k" {
				d = -1
			}
			m.moveRepo(d)
		}
	case "ctrl+s":
		mm, cmd := m.saveRepos()
		return true, mm, cmd
	case "pgup":
		_, ph := m.reposPanelSize(m.contentSize())
		rp.scroll = max(rp.scroll-ph/2, 0)
	case "pgdown":
		_, ph := m.reposPanelSize(m.contentSize())
		rp.scroll += ph / 2
	case "a":
		mm, cmd := m.openAddRepo()
		return true, mm, cmd
	case "p":
		mm, cmd := m.reposAction(reposPause)
		return true, mm, cmd
	case "d":
		mm, cmd := m.reposAction(reposRemove)
		return true, mm, cmd
	default:
		return false, m, nil
	}
	return true, m, nil
}

// reposAction pauses, resumes or (after asking) removes the selected repo.
func (m Model) reposAction(id string) (tea.Model, tea.Cmd) {
	r := m.selectedRepoStatus()
	if r == nil || r.Removing || m.offline() {
		return m, nil
	}
	name := r.Name
	if id == reposRemove {
		return m.openConfirm(fmt.Sprintf("Remove repo %s? Its running jobs finish first.", name), func() tea.Cmd {
			return m.action("removing "+name, func(c context.Context) error { return m.c.RemoveRepo(c, name) })
		})
	}
	if r.Paused {
		return m, m.action("resumed "+name, func(c context.Context) error { return m.c.Resume(c, name) })
	}
	return m, m.action("paused "+name, func(c context.Context) error { return m.c.Pause(c, name) })
}

// reposMouse handles clicks on the list, the wheel over the list, and the
// page's buttons. It reports false for events the shell should handle.
func (m Model) reposMouse(msg tea.MouseMsg) (bool, tea.Model, tea.Cmd) {
	rp := m.repos
	m.syncRepoControls()
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		for i := range m.st.Repos {
			if zone.Get(fmt.Sprintf("repos/row/%d", i)).InBounds(msg) {
				d := 1
				if msg.Button == tea.MouseButtonWheelUp {
					d = -1
				}
				m.moveRepo(d)
				return true, m, nil
			}
		}
		if zone.Get("repos/panel").InBounds(msg) {
			if msg.Button == tea.MouseButtonWheelUp {
				rp.scroll = max(rp.scroll-3, 0)
			} else {
				rp.scroll += 3
			}
			return true, m, nil
		}
		return false, m, nil
	}
	if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
		for i, r := range m.st.Repos {
			if zone.Get(fmt.Sprintf("repos/row/%d", i)).InBounds(msg) {
				rp.selected, rp.idx = r.Name, i
				rp.group.Focus(reposList)
				return true, m, nil
			}
		}
	}
	ok, cmd := rp.group.Mouse(msg)
	return ok, m, cmd
}

func (m Model) reposFooterKeys() []footerKey {
	return []footerKey{{"up", "select"}, {"tab", "next"}, {"a", "add"}, {"p", "pause"}, {"d", "remove"}, {"?", "help"}, {"q", "quit"}}
}
