package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/golden"
	zone "github.com/lrstanley/bubblezone"
	"github.com/muesli/termenv"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	zone.NewGlobal()
	os.Exit(m.Run())
}

var now = time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)

// fakeClient records actions; reads return the configured fields (zero values
// mean the sample status and empty results).
type fakeClient struct {
	calls  []string
	st     *model.Status
	events []model.Event
	steps  []model.Step
	ctrs   []model.Container
	cfg    *config.Config
}

func (f *fakeClient) rec(s string, a ...any) error {
	f.calls = append(f.calls, fmt.Sprintf(s, a...))
	return nil
}
func (f *fakeClient) Status(context.Context) (model.Status, error) {
	if f.st != nil {
		return *f.st, nil
	}
	return sampleStatus(), nil
}
func (f *fakeClient) Events(_ context.Context, after int64) ([]model.Event, error) {
	var out []model.Event
	for _, e := range f.events {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out, nil
}

// Log returns one line naming the runner and cursor it was asked for.
func (f *fakeClient) Log(_ context.Context, id, cursor string) (model.LogChunk, error) {
	f.rec("log %s %q", id, cursor)
	return model.LogChunk{Data: fmt.Sprintf("%s after %q\n", id, cursor), Next: cursor + "+"}, nil
}
func (f *fakeClient) Steps(context.Context, string) ([]model.Step, error) { return f.steps, nil }
func (f *fakeClient) Containers(_ context.Context, id string) ([]model.Container, error) {
	f.rec("containers %s", id)
	return f.ctrs, nil
}
func (f *fakeClient) Config(_ context.Context, out any) error {
	if f.cfg != nil {
		*out.(*config.Config) = *f.cfg
	}
	return nil
}
func (f *fakeClient) History(context.Context, string, string, int) ([]model.HistoryEntry, error) {
	return nil, nil
}
func (f *fakeClient) PatchConfig(_ context.Context, p model.ConfigPatch) error {
	switch {
	case p.GlobalMax != nil:
		return f.rec("global_max=%d", *p.GlobalMax)
	case p.Mode != nil:
		return f.rec("mode=%s", *p.Mode)
	}
	for name, rp := range p.Repos {
		if rp.Max != nil {
			return f.rec("%s.max=%d", name, *rp.Max)
		}
		if rp.Labels != nil {
			return f.rec("%s.labels=%s", name, strings.Join(*rp.Labels, ","))
		}
	}
	return f.rec("patch")
}
func (f *fakeClient) AddRepo(_ context.Context, r model.AddRepoRequest) error {
	return f.rec("add %s %s", r.Name, strings.Join(r.Labels, ","))
}
func (f *fakeClient) RemoveRepo(_ context.Context, n string) error { return f.rec("rm %s", n) }
func (f *fakeClient) Pause(_ context.Context, n string) error      { return f.rec("pause %s", n) }
func (f *fakeClient) Resume(_ context.Context, n string) error     { return f.rec("resume %s", n) }
func (f *fakeClient) PauseAll(context.Context) error               { return f.rec("pause-all") }
func (f *fakeClient) ResumeAll(context.Context) error              { return f.rec("resume-all") }
func (f *fakeClient) Kill(_ context.Context, id string) error      { return f.rec("kill %s", id) }

// actions drops the read calls the fake records, leaving the daemon actions.
func (f *fakeClient) actions() []string {
	var out []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "log ") && !strings.HasPrefix(c, "containers ") {
			out = append(out, c)
		}
	}
	return out
}

func sampleStatus() model.Status {
	return model.Status{
		Now: now, Epoch: "e1", Mode: "queue", GlobalMax: 3, RateRemaining: 4800, DiskPct: 61,
		Repos: []model.RepoStatus{
			{Name: "darkcloud", Max: 1, Active: 1, Queued: 2,
				LastJob: &model.HistoryEntry{RunNumber: "411", JobName: "lint", Conclusion: "success", FinishedAt: now.Add(-2 * time.Minute)}},
			{Name: "darkmem", Max: 1, Active: 1,
				LastJob: &model.HistoryEntry{RunNumber: "87", JobName: "build / test", Conclusion: "failure", FinishedAt: now.Add(-time.Hour)}},
			{Name: "darkagents", Paused: true, Max: 1, Queued: 1},
		},
		Instances: []model.InstanceStatus{
			{ID: "a3f9c1", Repo: "darkcloud", State: "busy", Since: now.Add(-12 * time.Minute),
				Job: &model.JobInfo{Name: "CI / e2e-journeys", RunNumber: "412", StartedAt: now.Add(-12*time.Minute - 4*time.Second)}},
			{ID: "7be210", Repo: "darkmem", State: "idle", Since: now.Add(-90 * time.Second)},
		},
	}
}

func newModel(c Client, w, h int, st model.Status) Model {
	m := New(c)
	m.now = func() time.Time { return now }
	m.copyFn = func(string) {}
	upd, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	upd, _ = upd.Update(statusMsg{st: st})
	return upd.(Model)
}

func sampleModel(c Client, w, h int) Model {
	upd, _ := newModel(c, w, h, sampleStatus()).Update(eventsMsg{"e1", []model.Event{
		{Seq: 1, Time: now.Add(-3 * time.Minute).Local(), Level: "ok", Repo: "darkcloud", Msg: "#411 lint success 2m10s  cleanup: 3 ctrs"},
		{Seq: 2, Time: now.Add(-2 * time.Minute).Local(), Level: "info", Repo: "darkcloud", Msg: "spawned a3f9c1 (2.330.0)"},
		{Seq: 3, Time: now.Add(-time.Minute).Local(), Level: "warn", Msg: "disk 81% > high-water 80% — pruned build cache"},
	}})
	return upd.(Model)
}

// stripTimes removes the local clock column so goldens do not depend on the test machine's zone.
func stripTimes(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if i := strings.Index(l, ":0"); i >= 2 && strings.Count(l, ":") >= 2 && strings.Contains(l, "  ") {
			l = l[:i-2] + "HH:MM:SS" + l[i+6:]
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func TestDashboardGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			v := sampleModel(&fakeClient{}, w, 30).View()
			golden.RequireEqual(t, []byte(stripTimes(v)))
		})
	}
}

func TestDashboardContent(t *testing.T) {
	v := sampleModel(&fakeClient{}, 120, 30).View()
	for _, want := range []string{"mode ● QUEUE", "runners", "2/3", "darkcloud", "⧗ 2", "#411 lint", "2m ago",
		"◌ paused", "a3f9c1", "CI / e2e-journeys  #412", "12m04s", "2 jobs queued (repo cap 1)", "disk 81%", "x kill"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	narrow := sampleModel(&fakeClient{}, 72, 30).View()
	if strings.Contains(narrow, "LAST JOB") {
		t.Error("LAST JOB column should be hidden below 80 columns")
	}
}

func TestRemovingRepoState(t *testing.T) {
	st := sampleStatus()
	st.Repos[2].Removing = true
	if v := newModel(&fakeClient{}, 120, 30, st).View(); !strings.Contains(v, "◌ removing") {
		t.Fatal("removing state not shown")
	}
}

func key(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// run feeds keys and executes resulting commands (one level, ignoring follow-up fetches).
func run(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	var cur tea.Model = m
	for _, k := range keys {
		var cmd tea.Cmd
		cur, cmd = cur.Update(key(k))
		if cmd != nil {
			if done, ok := cmd().(doneMsg); ok {
				cur, _ = cur.Update(done)
			}
		}
	}
	return cur.(Model)
}

// collect runs cmd and returns the messages it produces, expanding batches.
// A command still running after a short wait (the 1 s tick) is dropped.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range batch {
				out = append(out, collect(c)...)
			}
			return out
		}
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// feed applies msg and, recursively, every message its commands produce.
func feed(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		upd, cmd := m.Update(msg)
		m = feed(upd.(Model), collect(cmd)...)
	}
	return m
}

func keys(ks ...string) []tea.Msg {
	var out []tea.Msg
	for _, k := range ks {
		out = append(out, key(k))
	}
	return out
}

func ticks(m Model, n int) Model {
	for i := 0; i < n; i++ {
		m = feed(m, tickMsg(now))
	}
	return m
}

func waitZone(t *testing.T, id string) *zone.ZoneInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if z := zone.Get(id); !z.IsZero() {
			return z
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("zone %s never registered", id)
	return nil
}

func leftClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
}

var syncN int

// settle waits until every zone update already queued has been applied.
// bubblezone applies Scan results from a buffered channel, so without this a
// late update from an older frame can land after a Clear and hand back stale
// coordinates.
func settle(t *testing.T) {
	t.Helper()
	syncN++
	id := fmt.Sprintf("sync/%d", syncN)
	zone.Scan(zone.Mark(id, "x"))
	waitZone(t, id)
}

// zoneOf renders m and returns zone id as that frame placed it.
func zoneOf(t *testing.T, m Model, id string) *zone.ZoneInfo {
	t.Helper()
	settle(t)
	zone.Clear(id)
	m.View()
	return waitZone(t, id)
}

// click renders m, waits for zone id and feeds a left click on it.
func click(t *testing.T, m Model, id string) Model {
	t.Helper()
	z := zoneOf(t, m, id)
	return feed(m, leftClick(z.StartX, z.StartY))
}

func TestKeyActions(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m = run(t, m, "p", "+", "]", "m")
	m = run(t, m, "down", "down", "p") // darkagents is paused → resume
	want := []string{"pause darkcloud", "darkcloud.max=2", "global_max=4", "mode=all", "resume darkagents"}
	if strings.Join(c.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %v", c.calls)
	}
	if !strings.Contains(m.View(), "resumed darkagents") {
		t.Fatal("toast missing")
	}
}

// A toast replaces the previous one and clears on the tick after it expires:
// 4 s for a success, 10 s for an error. Its ✕ closes it early.
func TestToastLifecycle(t *testing.T) {
	c := &fakeClient{}
	m := run(t, sampleModel(c, 120, 30), "p")
	if v := m.View(); !strings.Contains(v, "✔ paused darkcloud") {
		t.Fatalf("success toast missing:\n%s", v)
	}
	m.now = func() time.Time { return now.Add(4 * time.Second) }
	m = ticks(m, 1)
	if strings.Contains(m.View(), "paused darkcloud") {
		t.Fatal("success toast still shown after 4s")
	}
	m.toast.Show("boom", true, now)
	m.now = func() time.Time { return now.Add(9 * time.Second) }
	if m = ticks(m, 1); !strings.Contains(m.View(), "✖ boom") {
		t.Fatal("error toast cleared before 10s")
	}
	m = click(t, m, m.toast.CloseZone())
	if m.toast.Active() || strings.Contains(m.View(), "boom") {
		t.Fatal("✕ did not close the toast")
	}
}

// tab moves focus within the page instead of switching pages.
func TestTabMovesFocusNotPage(t *testing.T) {
	m := run(t, sampleModel(&fakeClient{}, 120, 30), "tab")
	if m.page != pageDashboard || m.focus != paneRunners {
		t.Fatalf("tab: page %v focus %v", m.page, m.focus)
	}
	m = run(t, m, "tab")
	if m.focus != paneRepos {
		t.Fatalf("second tab: focus %v", m.focus)
	}
	if m = run(t, m, "2", "tab"); m.page != pageRunners {
		t.Fatalf("tab on Runners switched to page %v", m.page)
	}
}

func TestKillBusyRunnerNeedsConfirm(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = run(t, m, "x")
	if m.overlay != ovConfirm || len(c.calls) != 0 {
		t.Fatalf("overlay %v calls %v", m.overlay, c.calls)
	}
	m = run(t, m, "y")
	if len(c.calls) != 1 || c.calls[0] != "kill a3f9c1" {
		t.Fatalf("calls %v", c.calls)
	}
	m = run(t, m, "down", "x") // idle runner: no confirmation
	if c.calls[1] != "kill 7be210" {
		t.Fatalf("calls %v", c.calls)
	}
}

func TestHiddenSelectionKeysDoNothing(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m = run(t, m, "x")
	if len(c.actions()) != 0 || m.overlay != ovNone {
		t.Fatalf("x on the Repos pane: overlay %v actions %v", m.overlay, c.actions())
	}
	m = run(t, m, "2", "p", "+", "d")
	if len(c.actions()) != 0 || m.overlay != ovNone {
		t.Fatalf("repo keys on the Runners page: overlay %v actions %v", m.overlay, c.actions())
	}
	run(t, m, "4", "x")
	if len(c.actions()) != 0 {
		t.Fatalf("x on the Settings page: actions %v", c.actions())
	}
}

func TestAddRepoPrompt(t *testing.T) {
	c := &fakeClient{}
	m := run(t, sampleModel(c, 120, 30), "a")
	if m.overlay != ovPrompt {
		t.Fatal("prompt not open")
	}
	for _, r := range "newrepo homelab,gpu" {
		m = run(t, m, string(r))
	}
	m = run(t, m, "enter")
	if len(c.calls) != 1 || c.calls[0] != "add newrepo homelab,gpu" {
		t.Fatalf("calls %v", c.calls)
	}
}

func sampleConfig(t *testing.T, repos ...string) *config.Config {
	t.Helper()
	y := "owner: o\nlabels: [homelab]\nrepos:\n"
	for _, r := range repos {
		y += "  - name: " + r + "\n"
	}
	cfg, _, err := config.Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestConfigTabEdit(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	upd, _ := m.Update(configMsg(sampleConfig(t, "darkcloud")))
	m = run(t, upd.(Model), "4")
	if !strings.Contains(m.View(), "darkcloud.cleanup_name_prefixes") {
		t.Fatal("config fields not rendered")
	}
	m = run(t, m, "down", "down", "down", "down", "down", "down", "enter") // darkcloud.labels
	if m.overlay != ovPrompt || !strings.Contains(m.promptLabel, "darkcloud.labels") {
		t.Fatalf("prompt %q", m.promptLabel)
	}
	m.prompt.SetValue("a, b")
	run(t, m, "enter")
	if len(c.calls) != 1 || c.calls[0] != "darkcloud.labels=a,b" {
		t.Fatalf("calls %v", c.calls)
	}
}

func TestConfigUnchangedSaveSendsNothing(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	upd, _ := m.Update(configMsg(sampleConfig(t, "darkcloud")))
	m = run(t, upd.(Model), "4", "down", "down", "down", "down") // darkcloud.max
	m = run(t, m, "enter", "enter")
	if len(c.actions()) != 0 {
		t.Fatalf("an unchanged save sent %v", c.actions())
	}
	m = run(t, m, "enter")
	m.prompt.SetValue("2")
	run(t, m, "enter")
	if strings.Join(c.actions(), "|") != "darkcloud.max=2" {
		t.Fatalf("actions %v", c.actions())
	}
}

func TestMouseClickSelectsRunnerAndDoubleClickOpensDetail(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	z := zoneOf(t, m, "runner-1")
	click := leftClick(z.StartX+2, z.StartY)
	upd, _ := m.Update(click)
	m = upd.(Model)
	if m.runnerSel != 1 || m.focus != paneRunners {
		t.Fatalf("sel %d focus %v", m.runnerSel, m.focus)
	}
	upd, _ = m.Update(click)
	if upd.(Model).overlay != ovDetail {
		t.Fatal("double click did not open detail view")
	}
}

func TestDetailShowsContainers(t *testing.T) {
	c := &fakeClient{ctrs: []model.Container{
		{ID: "c1", Name: "darkcloud-db-1", Image: "postgres:17", State: "running", Project: "ghr-a3f9c1"},
		{ID: "c2", Name: "darkcloud-web-1", Image: "nginx:1.27", State: "exited", Project: "ghr-a3f9c1-e2e"},
	}}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = feed(m, key("enter"))
	if m.overlay != ovDetail || strings.Join(c.calls, "|") != "containers a3f9c1" {
		t.Fatalf("overlay %v calls %v", m.overlay, c.calls)
	}
	v := m.View()
	for _, want := range []string{"Containers", "running", "darkcloud-db-1", "postgres:17", "ghr-a3f9c1",
		"exited", "darkcloud-web-1", "nginx:1.27", "ghr-a3f9c1-e2e"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail view missing %q", want)
		}
	}
}

func TestDetailStepsRefreshOnTicks(t *testing.T) {
	c := &fakeClient{steps: []model.Step{{Number: 1, Name: "Set up job", Status: "queued"}}}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = feed(m, key("enter"))
	if v := m.View(); !strings.Contains(v, "○ Set up job") {
		t.Fatalf("queued step not shown:\n%s", v)
	}
	c.steps = []model.Step{{Number: 1, Name: "Set up job", Status: "in_progress"}}
	c.ctrs = []model.Container{{Name: "darkcloud-db-1", State: "running"}}
	m = ticks(m, slowPoll)
	v := m.View()
	if !strings.Contains(v, spinnerFrames[m.frame%len(spinnerFrames)]+" Set up job") {
		t.Fatalf("running step not refreshed:\n%s", v)
	}
	if !strings.Contains(v, "darkcloud-db-1") {
		t.Fatal("containers not refreshed")
	}
	c.steps = []model.Step{{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success"}}
	m = ticks(m, slowPoll)
	if v := m.View(); !strings.Contains(v, "✔ Set up job") {
		t.Fatalf("completed step not refreshed:\n%s", v)
	}
}

func TestStaleAsyncResponsesAreDropped(t *testing.T) {
	c := &fakeClient{steps: []model.Step{{Name: "current step", Status: "queued"}}}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = feed(m, keys("down", "enter")...) // detail for 7be210
	m = feed(m, stepsMsg{id: "a3f9c1", steps: []model.Step{{Name: "stale step"}}})
	if v := m.View(); strings.Contains(v, "stale step") || !strings.Contains(v, "current step") {
		t.Fatalf("stale steps response applied:\n%s", v)
	}
	m = feed(m, key("esc"))

	m = feed(m, keys("2", "up")...) // follow a3f9c1
	if m.logText != "a3f9c1 after \"\"\n" || m.logCursor != "+" {
		t.Fatalf("log %q cursor %q", m.logText, m.logCursor)
	}
	m = feed(m, logMsg{gen: m.logGen, cursor: "", chunk: model.LogChunk{Data: "duplicate\n", Next: "+"}})
	if strings.Contains(m.logText, "duplicate") || m.logCursor != "+" {
		t.Fatalf("response for an old cursor appended: %q", m.logText)
	}

	oldGen := m.logGen
	upd, _ := m.Update(key("down")) // follow 7be210; its first request is left in flight
	m = upd.(Model)
	m = feed(m, logMsg{gen: oldGen, cursor: "", chunk: model.LogChunk{Data: "late a3f9c1\n", Next: "+"}})
	if m.logText != "" || !m.logBusy {
		t.Fatalf("response for the previous runner applied: %q busy %v", m.logText, m.logBusy)
	}
	before := len(c.calls)
	m = ticks(m, 2)
	if len(c.calls) != before {
		t.Fatalf("log polled while a request was in flight: %v", c.calls[before:])
	}

	m = feed(m, key("3"))
	m = feed(m, key("r")) // filter repo darkcloud
	m = feed(m, historyMsg{hist: []model.HistoryEntry{{Repo: "unfiltered", JobName: "stale"}}})
	if len(m.hist) != 0 {
		t.Fatalf("history response for the old filter applied: %v", m.hist)
	}
}

func TestRunnersTabFollowsSelection(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), key("2"))
	if v := m.View(); m.logID != "a3f9c1" || !strings.Contains(v, `a3f9c1 after ""`) {
		t.Fatalf("entering Runners did not follow the selection: %q\n%s", m.logID, v)
	}
	m = feed(m, key("down"))
	if v := m.View(); m.logID != "7be210" || !strings.Contains(v, `7be210 after ""`) || strings.Contains(v, "a3f9c1 after") {
		t.Fatalf("keyboard selection did not switch the log: %q\n%s", m.logID, v)
	}
	m = click(t, m, "runner-0")
	if v := m.View(); m.logID != "a3f9c1" || !strings.Contains(v, `a3f9c1 after ""`) || strings.Contains(v, "7be210 after") {
		t.Fatalf("mouse selection did not switch the log: %q\n%s", m.logID, v)
	}
	m = ticks(m, 1)
	if !strings.Contains(m.logText, `a3f9c1 after "+"`) {
		t.Fatalf("followed log not polled from its cursor: %q", m.logText)
	}
	m = feed(m, keys("1", "l")...)
	if m.page != pageRunners || m.logID != "a3f9c1" {
		t.Fatalf("l: page %v log %q", m.page, m.logID)
	}
}

func TestEventsResetOnDaemonRestart(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m = feed(m, eventsMsg{"e1", []model.Event{{Seq: 900, Time: now, Level: "info", Msg: "old daemon event"}}})
	m = feed(m, statusMsg{err: errors.New("connection refused")})
	if !strings.Contains(m.View(), "daemon unreachable") {
		t.Fatal("unreachable banner missing")
	}
	st := sampleStatus()
	st.Epoch = "e2"
	c.st = &st
	c.events = []model.Event{
		{Seq: 1, Time: now, Level: "info", Msg: "new daemon started"},
		{Seq: 2, Time: now, Level: "ok", Repo: "darkcloud", Msg: "adopted a3f9c1"},
	}
	m = ticks(m, 1)
	v := m.View()
	for _, want := range []string{"new daemon started", "adopted a3f9c1"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing new-epoch event %q", want)
		}
	}
	if strings.Contains(v, "old daemon event") || m.lastSeq != 2 {
		t.Fatalf("events of the previous daemon kept (lastSeq %d)", m.lastSeq)
	}
}

func TestConfigTabRetriesLoad(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.page = pageSettings
	c.cfg = sampleConfig(t, "darkcloud")
	m = ticks(m, 1)
	v := m.View()
	if !strings.Contains(v, "darkcloud.max") || strings.Contains(v, "loading…") {
		t.Fatalf("config not loaded by the tick:\n%s", v)
	}
}

func TestShortTerminalKeepsSelectionVisible(t *testing.T) {
	const h = 22
	st := sampleStatus()
	st.Repos, st.Instances = nil, nil
	var repos []string
	for i := 0; i < 12; i++ {
		st.Repos = append(st.Repos, model.RepoStatus{Name: fmt.Sprintf("repo%02d", i), Max: 1})
		repos = append(repos, fmt.Sprintf("repo%02d", i))
	}
	for i := 0; i < 10; i++ {
		st.Instances = append(st.Instances, model.InstanceStatus{ID: fmt.Sprintf("run%02d", i), Repo: "repo00", State: "idle", Since: now})
	}
	check := func(t *testing.T, m Model, want string) {
		t.Helper()
		v := m.View()
		if !strings.Contains(v, want) || !strings.Contains(v, "q quit") || lipgloss.Height(v) > h {
			t.Fatalf("want %q and the footer within %d lines, got %d:\n%s", want, h, lipgloss.Height(v), v)
		}
	}
	down := func(first string, n int) []string {
		out := []string{first}
		for i := 0; i < n; i++ {
			out = append(out, "down")
		}
		return out
	}
	t.Run("dashboard", func(t *testing.T) {
		m := run(t, newModel(&fakeClient{}, 120, h, st), down("1", 11)...)
		check(t, m, "▸ repo11")
		check(t, run(t, m, down("right", 9)...), "▸ run09")
	})
	t.Run("runners", func(t *testing.T) {
		check(t, run(t, newModel(&fakeClient{}, 120, h, st), down("2", 9)...), "▸ run09")
	})
	t.Run("config", func(t *testing.T) {
		upd, _ := newModel(&fakeClient{}, 120, h, st).Update(configMsg(sampleConfig(t, repos[:6]...)))
		check(t, run(t, upd.(Model), down("4", 27)...), "repo05.cleanup_name_prefixes")
	})
}

func TestMouseOverlayButtons(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = run(t, m, "x")
	m.View()
	m = feed(m, leftClick(0, 0))
	if m.overlay != ovConfirm {
		t.Fatal("a click outside the buttons closed the confirmation")
	}
	m = click(t, m, "btn-ok")
	if m.overlay != ovNone || strings.Join(c.actions(), "|") != "kill a3f9c1" {
		t.Fatalf("overlay %v calls %v", m.overlay, c.actions())
	}

	c.cfg = sampleConfig(t, "darkcloud")
	m = feed(m, key("4"))
	m = click(t, m, "cfg-1") // global_max
	if m.overlay != ovPrompt || !strings.Contains(m.promptLabel, "global_max") {
		t.Fatalf("overlay %v prompt %q", m.overlay, m.promptLabel)
	}
	m.prompt.SetValue("5")
	m = click(t, m, "btn-ok")
	if m.overlay != ovNone || strings.Join(c.actions(), "|") != "kill a3f9c1|global_max=5" {
		t.Fatalf("overlay %v calls %v", m.overlay, c.actions())
	}
	m = click(t, m, "cfg-1")
	m = click(t, m, "btn-cancel")
	if m.overlay != ovNone || len(c.actions()) != 2 {
		t.Fatalf("cancel: overlay %v calls %v", m.overlay, c.actions())
	}
}

func TestHelpMentionsMouseNotes(t *testing.T) {
	v := run(t, sampleModel(&fakeClient{}, 120, 30), "?").View()
	for _, want := range []string{"Shift-drag", "Option-drag in iTerm2", "set -g mouse on"} {
		if !strings.Contains(v, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestUntrustedTextIsSanitized(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 30), key("2"))
	m.logText, m.logBusy = "", false
	m = feed(m, logMsg{gen: m.logGen, cursor: m.logCursor, chunk: model.LogChunk{Data: "ok\x1b]52;c;ZXZpbA==\a\x1b[2Jdone\r\n", Next: "+"}})
	if !strings.HasSuffix(m.logText, "okdone\n") {
		t.Fatalf("log text %q", m.logText)
	}
	m = feed(m, eventsMsg{m.epoch, []model.Event{{Seq: 1000, Time: now, Level: "info", Msg: "evil\x1b]0;pwned\a"}}})
	v := feed(m, key("1")).View() // events are drawn on the Dashboard only
	if !strings.Contains(v, "evil") || strings.Contains(v, "\x1b]") || strings.Contains(v, "\a") {
		t.Fatalf("event text not sanitised: %q", v)
	}
	got := clean("a\x9bb\tc")
	if strings.ContainsRune(got, 0x9b) || strings.Contains(got, "\x9b") || strings.Contains(got, "\x1b") || !strings.Contains(got, "\t") {
		t.Fatalf("clean: %q", got)
	}
}
