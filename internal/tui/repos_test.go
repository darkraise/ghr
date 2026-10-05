package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/darkraise/ghr/internal/config"
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
