package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/golden"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

func TestRepositoriesPageListsAndSelectsByName(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), key("2"))
	if m.page != pageRepos {
		t.Fatalf("page %v", m.page)
	}
	v := m.View()
	for _, want := range []string{"2 Repositories", "[ + Add repository ]", "darkcloud", "darkmem", "darkagents", "[ACTIVE]", "[PAUSED]", "1/1 running · 2 queued"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	m = feed(m, key("down"))
	if m.repos.selected != "darkmem" {
		t.Fatalf("selected %q", m.repos.selected)
	}
	st := sampleStatus()
	st.Repos = st.Repos[1:] // darkcloud removed: the selection stays on darkmem
	m = feed(m, statusMsg{st: st})
	if r := m.selectedRepoStatus(); r == nil || r.Name != "darkmem" {
		t.Fatalf("selection moved: %+v", r)
	}
	st.Repos = st.Repos[1:] // darkmem removed: the repo now at its position
	m = feed(m, statusMsg{st: st})
	if r := m.selectedRepoStatus(); r == nil || r.Name != "darkagents" {
		t.Fatalf("fallback: %+v", r)
	}
}

func TestRepositoriesPageActions(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), keys("2", "down", "p")...)
	if got := strings.Join(c.actions(), "|"); got != "pause darkmem" {
		t.Fatalf("p: %q", got)
	}
	if m = feed(m, key("d")); m.overlay != ovConfirm || !strings.Contains(m.confirmText, "Remove repo darkmem?") {
		t.Fatalf("d: overlay %v %q", m.overlay, m.confirmText)
	}
	m = feed(m, key("esc"), key("a"))
	if m.overlay != ovAddRepo {
		t.Fatalf("a: overlay %v", m.overlay)
	}
}

// The footer's p and d hints act on the selected repo, as the keys do.
func TestRepositoriesFooterHints(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), keys("2", "down")...)
	upd, cmd := m.footerPress("p")
	m = feed(upd.(Model), collect(cmd)...)
	if got := strings.Join(c.actions(), "|"); got != "pause darkmem" {
		t.Fatalf("p hint: %q", got)
	}
	upd, _ = m.footerPress("d")
	if mm := upd.(Model); mm.overlay != ovConfirm || !strings.Contains(mm.confirmText, "Remove repo darkmem?") {
		t.Fatalf("d hint: overlay %v %q", mm.overlay, mm.confirmText)
	}
	upd, _ = m.footerPress("a")
	if mm := upd.(Model); mm.overlay != ovAddRepo {
		t.Fatalf("a hint: overlay %v", mm.overlay)
	}
}

func TestRepositoriesPageEmptyAndNarrow(t *testing.T) {
	st := sampleStatus()
	st.Repos = nil
	m := feed(newModel(&fakeClient{}, 120, 30, st), key("2"))
	if v := m.View(); !strings.Contains(v, "No repositories yet") {
		t.Fatalf("empty:\n%s", v)
	}
	// Narrow: the list card stacks above the panel; the rows and buttons must
	// be on screen, not only within the width.
	for _, w := range []int{99, 72, 56, 40} {
		v := feed(sampleModel(&fakeClient{}, w, 30), key("2")).View()
		fits(t, "repositories", v, w, 30)
		for _, want := range []string{"darkcloud", "darkmem", "[ACTIVE]", "Pause", "Remove"} {
			if !strings.Contains(v, want) {
				t.Errorf("%d columns: missing %q:\n%s", w, want, v)
			}
		}
	}
}

// onRepos opens the Repositories page; entering it fetches the config, which
// feed delivers from c.cfg.
func onRepos(t *testing.T, c *fakeClient, w, h int) Model {
	t.Helper()
	c.cfg = parseConfig(t, settingsYAML)
	m := feed(sampleModel(c, w, h), key("2"))
	if m.cfg == nil || m.repos.form.Field(reposKey("darkcloud", "max")) == nil {
		t.Fatal("entering the Repositories page did not load the config")
	}
	return m
}

// focusInView fails unless the focused panel control lies inside the
// panel's visible lines after a frame.
func focusInView(t *testing.T, m Model) {
	t.Helper()
	m.View()
	r := m.selectedRepoStatus()
	pw, ph := m.reposPanelSize(m.contentSize())
	_, ranges := m.reposPanelLines(*r, pw)
	id := m.repos.group.FocusedID()
	rg, ok := ranges[id]
	if !ok {
		t.Fatalf("focus %q is not a panel control", id)
	}
	if rg.Start < m.repos.scroll || rg.End > m.repos.scroll+ph {
		t.Fatalf("focus %q at lines %d-%d, view %d-%d", id, rg.Start, rg.End, m.repos.scroll, m.repos.scroll+ph)
	}
}

// Every focus move scrolls the panel: Tab, a tag list's enter-to-advance,
// and focus moved between frames (as Group.Set does after a refresh).
func TestRepositoriesFocusMovesScrollIntoView(t *testing.T) {
	m := onRepos(t, &fakeClient{}, 120, 16)
	m.repos.group.Focus(reposKey("darkcloud", "max"))
	focusInView(t, m)
	for _, f := range []string{"warm", "labels", "cleanup"} {
		m = feed(m, key("tab"))
		if got := m.repos.group.FocusedID(); got != reposKey("darkcloud", f) {
			t.Fatalf("tab: focus %q, want %s", got, f)
		}
		focusInView(t, m)
	}
	m.repos.group.Focus(reposKey("darkcloud", "max"))
	focusInView(t, m)
	m.repos.group.Focus(reposKey("darkcloud", "labels"))
	m = feed(m, key("enter"), key("enter")) // open the input, then enter on it empty: advance
	if got := m.repos.group.FocusedID(); got != reposKey("darkcloud", "cleanup") {
		t.Fatalf("enter-advance: focus %q", got)
	}
	focusInView(t, m)
	m.repos.group.Focus(reposKey("darkcloud", "max"))
	focusInView(t, m)
}

// Save while the daemon is unreachable says so, as every other action does;
// the Save button itself is disabled.
func TestRepositoriesSaveWhileDisconnected(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 120, 40)
	repoInput(m, "darkcloud", "labels").SetValue(ui.Value{List: []string{"gpu"}})
	m.connected = false
	m.View()
	if !m.repos.save.Disabled {
		t.Fatal("Save is enabled while unreachable")
	}
	m = feed(m, key("ctrl+s"))
	if v := m.View(); len(c.patches) != 0 || !strings.Contains(v, "the daemon is unreachable") {
		t.Fatalf("patches %d:\n%s", len(c.patches), v)
	}
}

// Narrow and short screens keep every control of the selected repo whole;
// the panel scrolls rather than cutting them.
func TestRepositoriesNarrowShowsControls(t *testing.T) {
	for _, w := range []int{99, 72, 56, 40} {
		m := onRepos(t, &fakeClient{}, w, 90)
		v := m.View()
		fits(t, "repositories", v, w, 90)
		for _, want := range []string{"[ − ]", "[ + ]", "darkcloud-linux", "dc-e2e-", "Runners get", "self-hosted", "homelab (global)", "runners already running", "their labels", "Pause", "Remove"} {
			if !strings.Contains(v, want) {
				t.Errorf("%d columns: missing %q", w, want)
			}
		}
	}
	m := onRepos(t, &fakeClient{}, 40, 22)
	fits(t, "repositories short", m.View(), 40, 22)
	m = feed(m, key("pgdown"), key("pgdown"), key("pgdown"))
	if v := m.View(); !strings.Contains(v, "dc-e2e-") {
		t.Fatalf("40x22 scrolled to the end lacks the cleanup control:\n%s", v)
	}
}

func TestRepositoriesGolden(t *testing.T) {
	for _, w := range []int{120, 100} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := onRepos(t, &fakeClient{}, w, 40)
			repoInput(m, "darkcloud", "labels").SetValue(ui.Value{List: []string{"darkcloud-linux", "gpu"}})
			golden.RequireEqual(t, []byte(m.View()))
		})
	}
}

// Each page saves only its own form: saving Repositories sends no Settings
// field and leaves the Settings edit unsaved, and the other way round.
func TestConfigPagesSaveIndependently(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 120, 40)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	repoInput(m, "darkcloud", "labels").SetValue(ui.Value{List: []string{"gpu"}})
	m = feed(m, key("ctrl+s"))
	if len(c.patches) != 1 || c.patches[0].PollInterval != nil || len(c.patches[0].Repos) != 1 {
		t.Fatalf("Repositories save: %+v", c.patches)
	}
	if len(m.settings.form.Dirty()) != 1 {
		t.Fatal("the Settings edit was saved or dropped by the Repositories save")
	}
	repoInput(m, "darkcloud", "labels").SetValue(ui.Value{List: []string{"gpu", "cuda"}})
	m.page = pageSettings
	m = feed(m, key("ctrl+s"))
	if len(c.patches) != 2 || c.patches[1].PollInterval == nil || len(c.patches[1].Repos) != 0 {
		t.Fatalf("Settings save: %+v", c.patches[1])
	}
	if !m.repos.form.Field(reposKey("darkcloud", "labels")).Dirty() {
		t.Fatal("the Repositories edit was saved or dropped by the Settings save")
	}
}

func repoInput(m Model, name, field string) ui.Input {
	return m.repos.form.Field(reposKey(name, field)).Input
}

func TestRepositoriesEditAndSave(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 120, 40)
	repoInput(m, "darkcloud", "labels").SetValue(ui.Value{List: []string{"darkcloud-linux", "gpu"}})
	v := m.View()
	for _, want := range []string{"● 1 unsaved change (darkcloud)", "Runners get", "self-hosted linux x64", "homelab (global)", "gpu", "runners already running keep their labels"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	m = feed(m, key("down"), key("up")) // switching repos keeps the edit
	if !m.repos.form.Field(reposKey("darkcloud", "labels")).Dirty() {
		t.Fatal("the edit was lost when switching repos")
	}
	m = feed(m, key("ctrl+s"))
	if len(c.patches) != 1 {
		t.Fatalf("patches %d", len(c.patches))
	}
	p := c.patches[0]
	if len(p.Repos) != 1 || p.Repos["darkcloud"].Labels == nil || !reflect.DeepEqual(*p.Repos["darkcloud"].Labels, []string{"darkcloud-linux", "gpu"}) ||
		p.Repos["darkcloud"].Max != nil || p.GlobalMax != nil {
		t.Fatalf("patch %+v", p)
	}
}

func TestRepositoriesWarmCheckAndDefaults(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 120, 40)
	repoInput(m, "darkcloud", "warm").SetValue(ui.Value{Num: 3, Set: true}) // darkcloud has max: 2
	m = feed(m, key("ctrl+s"))
	if v := m.View(); len(c.patches) != 0 || !strings.Contains(v, "warm must be <= max") || !strings.Contains(v, "fix the highlighted settings first") {
		t.Fatalf("patches %d:\n%s", len(c.patches), v)
	}
	m.View()
	if s := repoInput(m, "darkmem", "max").(*ui.Stepper); s.Default != 1 || s.DefaultText != "" {
		t.Fatalf("queue mode default %d %q", s.Default, s.DefaultText)
	}
	all := parseConfig(t, settingsYAML)
	all.Mode = config.ModeAll
	m = feed(m, configMsg{seq: m.order.seq + 1, cfg: all})
	m.View()
	if s := repoInput(m, "darkmem", "max").(*ui.Stepper); s.Default != 0 || s.DefaultText != "∞" {
		t.Fatalf("all mode default %d %q", s.Default, s.DefaultText)
	}
}

func TestRepositoriesRemovingAndRefreshMerge(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 120, 40)
	repoInput(m, "darkcloud", "cleanup").SetValue(ui.Value{List: []string{"dc-e2e-", "tmp-"}})
	cfg := parseConfig(t, settingsYAML)
	two := 2
	cfg.Repo("darkmem").Max = &two
	cfg.Repo("darkagents").Removing = true
	m = feed(m, configMsg{seq: m.order.seq + 1, cfg: cfg})
	if got := repoInput(m, "darkcloud", "cleanup").Value().List; !reflect.DeepEqual(got, []string{"dc-e2e-", "tmp-"}) {
		t.Fatalf("edit lost on refresh: %v", got)
	}
	if got := repoInput(m, "darkmem", "max").Value(); !got.Set || got.Num != 2 {
		t.Fatalf("refresh not merged: %+v", got)
	}
	st := sampleStatus()
	st.Repos[2].Removing = true
	m = feed(m, statusMsg{st: st}, key("down"), key("down"))
	m.View()
	if repoInput(m, "darkagents", "labels").Focusable() {
		t.Fatal("a repo being removed must be read-only")
	}
}

func TestRepositoriesLeaveGuard(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 120, 40)
	c.patchErr = rejected("repos.darkmem.max must be <= global_max")
	repoInput(m, "darkmem", "max").SetValue(ui.Value{Num: 2, Set: true})
	if m = feed(m, key("ctrl+s")); m.repos.alert == nil {
		t.Fatal("the rejected save showed no alert")
	}
	c.patchErr, c.patches = nil, nil
	if m = feed(m, key("1")); m.overlay != ovUnsaved || m.leaveFrom != pageRepos {
		t.Fatalf("1: overlay %v from %v", m.overlay, m.leaveFrom)
	}
	if v := m.View(); !strings.Contains(v, "You have 1 unsaved change on the Repositories page.") {
		t.Fatalf("dialog:\n%s", v)
	}
	m = click(t, m, btnLeaveDiscard)
	if m.page != pageDashboard || len(m.repos.form.Dirty()) != 0 || m.overlay != ovNone || m.repos.alert != nil {
		t.Fatalf("discard: page %v dirty %d overlay %v alert %v", m.page, len(m.repos.form.Dirty()), m.overlay, m.repos.alert)
	}
	m = feed(m, key("2"))
	repoInput(m, "darkmem", "max").SetValue(ui.Value{Num: 2, Set: true})
	c.onPatch = func(p model.ConfigPatch) { // the fake daemon applies the max, so the refetch check passes
		if rp, ok := p.Repos["darkmem"]; ok && rp.Max != nil {
			n := *rp.Max
			c.cfg.Repo("darkmem").Max = &n
		}
	}
	m = feed(m, keys("4", "enter")...) // Save, the primary
	if len(c.patches) != 1 || *c.patches[0].Repos["darkmem"].Max != 2 {
		t.Fatalf("patches %+v", c.patches)
	}
	if m.page != pageHistory {
		t.Fatalf("after save: page %v", m.page)
	}
}

func TestRemovedRepoToast(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 120, 40)
	cfg := parseConfig(t, settingsYAML)
	cfg.Repos = cfg.Repos[:1] // darkmem and darkagents gone
	m = feed(m, configMsg{seq: m.order.seq + 1, cfg: cfg})
	if v := m.View(); !strings.Contains(v, "repository darkmem, darkagents was removed") {
		t.Fatalf("toast missing:\n%s", v)
	}
	if m.repos.form.Field(reposKey("darkmem", "max")) != nil {
		t.Fatal("a removed repo keeps its fields")
	}
}

// Repo fields moved here from Settings, and with them the check that
// control sequences in config text never reach the screen.
func TestRepositoriesSanitisesConfigText(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 140, 60)
	// YAML's \e and \a escapes put real ESC and BEL bytes into the parsed config.
	cfg := parseConfig(t, `owner: darkraise
labels: ["bad\e[2Jlabel"]
repos:
  - name: darkcloud
  - name: darkmem
    cleanup_name_prefixes: ["x\e]52;c;Zm9v\ay"]
  - name: darkagents
`)
	m = feed(m, configMsg{seq: m.order.seq + 1, cfg: cfg}, key("down"))
	w, h := m.contentSize()
	v := zone.Scan(m.reposView(w, h))
	if strings.ContainsAny(v, "\x1b\a") || !strings.Contains(v, "xy") || !strings.Contains(v, "badlabel (global)") {
		t.Fatalf("config text not sanitised: %q", v)
	}
}

// Clicking a list row selects it and focuses the list; the wheel over the
// list moves the selection.
func TestRepositoriesListMouse(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 30), key("2"))
	m.repos.group.Focus(reposAdd)
	z := zoneOf(t, m, "repos/row/2")
	m = feed(m, leftClick(z.StartX, z.StartY))
	if r := m.selectedRepoStatus(); r == nil || r.Name != "darkagents" || m.repos.group.FocusedID() != reposList {
		t.Fatalf("click: %+v focus %q", r, m.repos.group.FocusedID())
	}
	wheel := func(b tea.MouseButton) {
		z := zoneOf(t, m, "repos/row/0")
		m = feed(m, tea.MouseMsg{X: z.StartX, Y: z.StartY, Action: tea.MouseActionPress, Button: b})
	}
	if wheel(tea.MouseButtonWheelUp); m.selectedRepoStatus().Name != "darkmem" {
		t.Fatalf("wheel up: %q", m.selectedRepoStatus().Name)
	}
	if wheel(tea.MouseButtonWheelDown); m.selectedRepoStatus().Name != "darkagents" {
		t.Fatalf("wheel down: %q", m.selectedRepoStatus().Name)
	}
}

// A repo being removed, or an unreachable daemon, disables Pause and Remove,
// and pressing them anyway does nothing.
func TestRepositoriesActionsGuarded(t *testing.T) {
	st := sampleStatus()
	st.Repos[2].Removing = true
	c := &fakeClient{st: &st}
	m := feed(newModel(c, 120, 30, st), keys("2", "down", "down")...)
	m.View()
	if !m.repos.pause.Disabled || !m.repos.remove.Disabled {
		t.Fatal("controls enabled while the repo is being removed")
	}
	for _, id := range []string{reposPause, reposRemove} {
		upd, cmd := m.reposAction(id)
		if cmd != nil || upd.(Model).overlay != ovNone {
			t.Fatalf("%s acted on a repo being removed", id)
		}
	}
	m = feed(m, key("up"))
	m.connected = false
	m.View()
	if !m.repos.pause.Disabled || !m.repos.remove.Disabled {
		t.Fatal("controls enabled while unreachable")
	}
	upd, cmd := m.reposAction(reposPause)
	if m = upd.(Model); cmd != nil || len(c.actions()) != 0 || !strings.Contains(m.View(), "the daemon is unreachable") {
		t.Fatalf("offline pause: actions %v", c.actions())
	}
}

// The list emptying shows the empty state; when repos return, the selection
// comes back to the repo it named.
func TestRepositoriesListEmptiesAndRefills(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 30), keys("2", "down")...)
	st := sampleStatus()
	empty := st
	empty.Repos = nil
	m = feed(m, statusMsg{st: empty})
	if m.selectedRepoStatus() != nil || !strings.Contains(m.View(), "No repositories yet") {
		t.Fatal("empty list not shown")
	}
	m = feed(m, statusMsg{st: st})
	if r := m.selectedRepoStatus(); r == nil || r.Name != "darkmem" || !strings.Contains(m.View(), "Pause") {
		t.Fatalf("refill: %+v", r)
	}
}

// A refresh while editing: a removed repo loses its fields and edit with a
// toast, a repo being removed drops its edit, a new repo gets fields, and no
// dropped edit reaches the next patch.
func TestRepositoriesRefreshDropsGoneEditsAndAddsNewRepo(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 120, 40)
	repoInput(m, "darkagents", "labels").SetValue(ui.Value{List: []string{"x"}})
	repoInput(m, "darkmem", "max").SetValue(ui.Value{Num: 4, Set: true})
	c.cfg = parseConfig(t, `owner: darkraise
mode: queue
global_max: 3
labels: [homelab]
repos:
  - name: darkcloud
    max: 2
    labels: [darkcloud-linux]
    cleanup_name_prefixes: [dc-e2e-]
  - name: darkagents
    paused: true
    removing: true
  - name: newrepo
    max: 3
`)
	m = ticks(m, slowPoll)
	if m.repos.form.Field(reposKey("darkmem", "max")) != nil {
		t.Error("removed repo kept its fields")
	}
	if m.repos.form.Field(reposKey("darkagents", "labels")).Dirty() {
		t.Error("repo being removed kept its edit")
	}
	if got := repoInput(m, "newrepo", "max").Value(); got.Num != 3 || !got.Set {
		t.Errorf("new repo max %+v", got)
	}
	if v := m.View(); !strings.Contains(v, "repository darkmem was removed") || strings.Contains(v, "unsaved change") {
		t.Errorf("toast or unsaved bar wrong:\n%s", v)
	}
	if p, _ := m.reposPatch(); len(p.Repos) != 0 {
		t.Errorf("patch after refresh: %+v", p)
	}
}

// Leaving while a save is in flight waits for it instead of asking.
func TestRepositoriesLeaveWaitsForSaveInFlight(t *testing.T) {
	m := onRepos(t, &fakeClient{}, 120, 40)
	repoInput(m, "darkmem", "max").SetValue(ui.Value{Num: 2, Set: true})
	upd, _ := m.Update(key("ctrl+s"))
	upd, _ = upd.Update(key("1"))
	m = upd.(Model)
	if m.overlay != ovNone || m.page != pageRepos || !strings.Contains(m.View(), "wait for the save to finish") {
		t.Fatalf("overlay %v page %v", m.overlay, m.page)
	}
}
