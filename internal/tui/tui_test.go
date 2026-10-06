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
	time.Local = time.UTC // the views print local times; keep snapshots machine-independent
	zone.NewGlobal()
	os.Exit(m.Run())
}

var now = time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)

// fakeClient records actions; reads return the configured fields (zero values
// mean the sample status and empty results).
type fakeClient struct {
	calls       []string
	st          *model.Status
	events      []model.Event
	steps       []model.Step
	ctrs        []model.Container
	cfg         *config.Config
	patches     []model.ConfigPatch
	addErr      error                   // returned by AddRepo when set
	patchErr    error                   // returned by PatchConfig when set
	cfgErr      error                   // returned by Config when set
	onPatch     func(model.ConfigPatch) // applies a patch to cfg, as a daemon would
	hist        []model.HistoryEntry    // History returns the entries matching its filters
	histReqs    []string                // each History request as "repo|conclusion"
	histErr     error                   // returned by History when set
	token       model.TokenStatus
	tokenErr    error // returned by Token
	setTokenErr error
	regs        []model.Registration
	regsByRepo  map[string][]model.Registration // when set, Registrations answers per repo
	regErr      error
	reads       []string // each Registrations and LabelCheck request, as "regs <repo>" or "lc <repo>"
	labels      model.LabelCheck
	labelsBy    map[string]model.LabelCheck // when set, LabelCheck answers per repo
	labelErr    error                       // returned by StartLabelCheck
	labelGetErr error                       // returned by LabelCheck
	warnings    []string
	reloadErr   error
	pruneErr    error
	metrics     model.Metrics
	metricsErr  error // returned by Metrics
	updateErr   error // returned by QueueRunnerUpdate and CancelRunnerUpdate
	avail       []model.AvailableRepo
	availErr    error // returned by AvailableRepos
	availCalls  int
	storage     *model.Storage                     // Storage returns it; nil means sampleStorage()
	storageErr  error                              // returned by Storage when set
	storageN    int                                // Storage calls
	choices     map[string][]model.ToolchainChoice // AvailableToolchains answers per tool
	choicesErr  error                              // returned by AvailableToolchains when set
	availTools  []string                           // each AvailableToolchains request's tool
	installErr  error                              // returned by InstallToolchain and InstallPreset
	storeErr    error                              // returned by RefreshStorage, RemoveToolchain, ClearCache and PruneScope
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
	if f.cfgErr != nil {
		return f.cfgErr
	}
	if f.cfg != nil {
		*out.(*config.Config) = *f.cfg
	}
	return nil
}
func (f *fakeClient) History(_ context.Context, repo, concl string, _ int) ([]model.HistoryEntry, error) {
	f.histReqs = append(f.histReqs, repo+"|"+concl)
	if f.histErr != nil {
		return nil, f.histErr
	}
	var out []model.HistoryEntry
	for _, e := range f.hist {
		if (repo == "" || e.Repo == repo) && (concl == "" || e.Conclusion == concl) {
			out = append(out, e)
		}
	}
	return out, nil
}
func (f *fakeClient) PatchConfig(_ context.Context, p model.ConfigPatch) error {
	f.patches = append(f.patches, p)
	if f.patchErr != nil {
		return f.patchErr
	}
	if f.onPatch != nil {
		f.onPatch(p)
	}
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
	max := "-"
	if r.Max != nil {
		max = fmt.Sprint(*r.Max)
	}
	f.rec("add %s %s max=%s public=%v", r.Name, strings.Join(r.Labels, ","), max, r.AllowPublic)
	return f.addErr
}
func (f *fakeClient) RemoveRepo(_ context.Context, n string) error { return f.rec("rm %s", n) }
func (f *fakeClient) Pause(_ context.Context, n string) error      { return f.rec("pause %s", n) }
func (f *fakeClient) Resume(_ context.Context, n string) error     { return f.rec("resume %s", n) }
func (f *fakeClient) PauseAll(context.Context) error               { return f.rec("pause-all") }
func (f *fakeClient) ResumeAll(context.Context) error              { return f.rec("resume-all") }
func (f *fakeClient) Kill(_ context.Context, id string) error      { return f.rec("kill %s", id) }

func (f *fakeClient) Token(context.Context) (model.TokenStatus, error) { return f.token, f.tokenErr }
func (f *fakeClient) SetToken(_ context.Context, tok string) error {
	f.rec("set-token %d chars", len(tok))
	return f.setTokenErr
}
func (f *fakeClient) Registrations(_ context.Context, repo string) ([]model.Registration, error) {
	f.reads = append(f.reads, "regs "+repo)
	if f.regsByRepo != nil {
		return f.regsByRepo[repo], f.regErr
	}
	return f.regs, f.regErr
}
func (f *fakeClient) DeleteRegistration(_ context.Context, repo string, id int64) error {
	return f.rec("del-reg %s %d", repo, id)
}
func (f *fakeClient) StartLabelCheck(_ context.Context, repo string) error {
	f.rec("label-check %s", repo)
	return f.labelErr
}
func (f *fakeClient) LabelCheck(_ context.Context, repo string) (model.LabelCheck, error) {
	f.reads = append(f.reads, "lc "+repo)
	if f.labelsBy != nil {
		return f.labelsBy[repo], f.labelGetErr
	}
	return f.labels, f.labelGetErr
}
func (f *fakeClient) Reload(context.Context) ([]string, error) {
	f.rec("reload")
	return f.warnings, f.reloadErr
}
func (f *fakeClient) Prune(context.Context) error {
	f.rec("prune")
	return f.pruneErr
}
func (f *fakeClient) Metrics(context.Context) (model.Metrics, error) { return f.metrics, f.metricsErr }
func (f *fakeClient) QueueRunnerUpdate(context.Context) error {
	f.rec("runner-update")
	return f.updateErr
}
func (f *fakeClient) CancelRunnerUpdate(context.Context) error {
	f.rec("runner-update cancel")
	return f.updateErr
}
func (f *fakeClient) AvailableRepos(context.Context) ([]model.AvailableRepo, error) {
	f.availCalls++
	return f.avail, f.availErr
}
func (f *fakeClient) Storage(context.Context) (model.Storage, error) {
	f.storageN++
	if f.storageErr != nil {
		return model.Storage{}, f.storageErr
	}
	if f.storage != nil {
		return *f.storage, nil
	}
	return sampleStorage(), nil
}
func (f *fakeClient) RefreshStorage(context.Context) error {
	f.rec("refresh-storage")
	return f.storeErr
}
func (f *fakeClient) AvailableToolchains(_ context.Context, tool string) ([]model.ToolchainChoice, error) {
	f.availTools = append(f.availTools, tool)
	return f.choices[tool], f.choicesErr
}
func (f *fakeClient) InstallToolchain(_ context.Context, tool, version string) error {
	f.rec("install %s %s", tool, version)
	return f.installErr
}
func (f *fakeClient) InstallPreset(_ context.Context, preset string) error {
	f.rec("install-preset %s", preset)
	return f.installErr
}
func (f *fakeClient) RemoveToolchain(_ context.Context, tool, version string) error {
	f.rec("rm-toolchain %s %s", tool, version)
	return f.storeErr
}
func (f *fakeClient) ClearCache(_ context.Context, name string) error {
	f.rec("clear %s", name)
	return f.storeErr
}
func (f *fakeClient) PruneScope(_ context.Context, scope string) error {
	f.rec("prune %s", scope)
	return f.storeErr
}

func TestClientServesStorage(t *testing.T) {
	f := &fakeClient{}
	var c Client = f
	s, err := c.Storage(context.Background())
	if err != nil || len(s.Toolchains) != 4 || s.Docker.DiskPct != 61 {
		t.Fatalf("storage %+v %v", s, err)
	}
	if err := c.InstallToolchain(context.Background(), "node", "22"); err != nil {
		t.Fatal(err)
	}
	if err := c.PruneScope(context.Background(), "unused-volumes"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.actions(), "|"); got != "install node 22|prune unused-volumes" || f.storageN != 1 {
		t.Fatalf("actions %q, %d storage reads", got, f.storageN)
	}
}

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

// sampleStorage is a Storage snapshot with every card filled: two .NET
// majors (each SDK the last of its major), a folder no installer owns, a
// cache that is not present, and a finished automatic prune.
func sampleStorage() model.Storage {
	installed := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	measured, written := now.Add(-3*time.Minute), now.Add(-2*time.Hour)
	pruned, op1, op2 := now.Add(-58*time.Minute), now.Add(-30*time.Minute), now.Add(-10*time.Minute)
	return model.Storage{
		Toolchains: []model.Toolchain{
			{Tool: "node", Version: "22.11.0", Arch: "x64", Bytes: 182_400_000, InstalledAt: installed},
			{Tool: "dotnet", Version: "8.0.414", Arch: "x64", Bytes: 412_000_000, InstalledAt: installed},
			{Tool: "dotnet", Version: "10.0.100", Arch: "x64", Bytes: 455_000_000, InstalledAt: installed},
			{Tool: "java", Version: "21.0.8+9", Arch: "x64", Bytes: 195_000_000, InstalledAt: installed},
		},
		OtherToolCache: []model.Folder{{Name: "PyPy", Bytes: 98_000_000}},
		PackageCaches: []model.PackageCache{
			{Name: "nuget", Label: "NuGet", Paths: []string{"/home/ghrunner/.nuget/packages"}, Present: true,
				Bytes: 1_240_000_000, Files: 18_204, LastWritten: &written},
			{Name: "npm", Label: "npm", Paths: []string{"/home/ghrunner/.npm"}, Present: true,
				Bytes: 310_000_000, Files: 4_410, LastWritten: &written},
			{Name: "pip", Label: "pip", Paths: []string{"/home/ghrunner/.cache/pip"}},
		},
		Docker: model.DockerDisk{
			Rows: []model.DockerRow{
				{Type: "Images", Count: 9, Active: 2, Bytes: 6_571_000_000, Reclaimable: 5_627_000_000},
				{Type: "Containers", Count: 2, Active: 2, Bytes: 40_960},
				{Type: "Local Volumes", Count: 68, Active: 1, Bytes: 8_660_000_000, Reclaimable: 8_660_000_000},
				{Type: "Build Cache", Count: 12, Bytes: 1_686_000_000, Reclaimable: 456_000_000},
			},
			BuildCacheTypes: []model.BuildCacheType{
				{Type: "regular", Count: 10, Bytes: 1_600_000_000, Reclaimable: 400_000_000},
				{Type: "source.local", Count: 2, Bytes: 86_000_000, Reclaimable: 56_000_000},
			},
			DiskPct: 61,
		},
		MeasuredAt: &measured,
		Operations: model.Operations{Recent: []model.Operation{
			{ID: "op2", Kind: "clear", Target: "npm", StartedAt: op2, FinishedAt: &op2, Outcome: "refused", Message: "refused: 1 jobs running"},
			{ID: "op1", Kind: "install", Target: "node 22", StartedAt: op1, FinishedAt: &op1, Outcome: "ok", Message: "installed node 22.11.0"},
		}},
		LastPrune: &model.LastPrune{Trigger: "auto", Scope: "auto", StartedAt: pruned, FinishedAt: &pruned, Outcome: "ok",
			Steps: []model.PruneStep{{Name: "build cache older than 72h", Freed: 1_200_000_000}, {Name: "dangling images", Freed: 300_000_000}}},
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

// pageModels builds the sample model on each page other than Settings, w
// columns wide.
var pageModels = map[string]func(w int) Model{
	"dashboard": func(w int) Model { return sampleModel(&fakeClient{}, w, 30) },
	"runners":   func(w int) Model { return feed(sampleModel(&fakeClient{}, w, 30), key("3")) },
	"detail": func(w int) Model {
		c := &fakeClient{steps: []model.Step{
			{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success"},
			{Number: 2, Name: "Run e2e journeys", Status: "in_progress"},
		}}
		st := sampleStatus()
		st.Instances[0].Job.HTMLURL = "https://github.com/darkraise/darkcloud/actions/runs/1"
		c.st = &st
		return feed(newModel(c, w, 30, st), keys("3", "enter")...)
	},
	"history": func(w int) Model {
		m := feed(sampleModel(&fakeClient{}, w, 30), key("4"))
		m.hist = []model.HistoryEntry{
			{Repo: "darkcloud", RunNumber: "411", JobName: "lint", Conclusion: "success",
				StartedAt: now.Add(-4 * time.Minute), FinishedAt: now.Add(-2 * time.Minute)},
			{Repo: "darkmem", RunNumber: "87", JobName: "build / test", Conclusion: "failure",
				StartedAt: now.Add(-70 * time.Minute), FinishedAt: now.Add(-time.Hour)},
		}
		return m
	},
}

func TestPagesGolden(t *testing.T) {
	for name, build := range pageModels {
		for _, w := range []int{120, 80} {
			t.Run(fmt.Sprintf("%s/%d", name, w), func(t *testing.T) {
				golden.RequireEqual(t, []byte(stripTimes(build(w).View())))
			})
		}
	}
}

// fits reports a line of v wider than w, or v not exactly h rows tall.
func fits(t *testing.T, name string, v string, w, h int) {
	t.Helper()
	if got := lipgloss.Height(v); got != h {
		t.Errorf("%s at %d columns: %d rows, want %d", name, w, got, h)
	}
	for i, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > w {
			t.Errorf("%s at %d columns: line %d is %d wide: %q", name, w, i, lipgloss.Width(line), line)
		}
	}
}

// Every page fills its screen exactly, down to the 40-column minimum.
func TestPagesFitNarrow(t *testing.T) {
	for name, build := range pageModels {
		for _, w := range []int{80, 56, 40} {
			fits(t, name, build(w).View(), w, 30)
		}
	}
}

// Every state of the detail page fits: each tab with long text, an error,
// and the finished alert.
func TestDetailStatesFitNarrow(t *testing.T) {
	long := strings.Repeat("a-very-long-name-", 8)
	for _, w := range []int{80, 40} {
		m := pageModels["detail"](w)
		m.steps = append(m.steps, model.Step{Number: 3, Name: long, Status: "queued"})
		m.stepsErr = "steps: " + long
		m.containers = []model.Container{{Name: long, Image: long, State: "running", Project: long}}
		m.detailDone = now
		for tab := range tabNames {
			m.groups.tabs.active = tab
			fits(t, fmt.Sprintf("detail tab %d", tab), m.View(), w, 30)
		}
	}
}

// An open Repo filter with many repos stays within a short screen.
func TestHistoryDropdownFitsShortScreen(t *testing.T) {
	st := sampleStatus()
	for i := 0; i < 20; i++ {
		st.Repos = append(st.Repos, model.RepoStatus{Name: fmt.Sprintf("repository-number-%02d", i)})
	}
	m := feed(newModel(&fakeClient{}, 80, 22, st), keys("4", "tab", "enter")...)
	if !m.groups.histRepo.Open() {
		t.Fatal("the Repo filter did not open")
	}
	fits(t, "history dropdown", m.View(), 80, 22)
}

func TestDashboardContent(t *testing.T) {
	v := sampleModel(&fakeClient{}, 120, 30).View()
	for _, want := range []string{"mode ● QUEUE", "runners", "2/3", "darkcloud", "⧗ 2", "#411 lint", "2m ago",
		"[PAUSED]", "a3f9c1", "CI / e2e-journeys  #412", "12m04s", "2 jobs queued (repo cap 1)", "disk 81%", "x stop"} {
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
	if v := newModel(&fakeClient{}, 120, 30, st).View(); !strings.Contains(v, "[REMOVING]") {
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
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
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
	m = run(t, m, "tab", "tab", "tab") // + Add, Pause all, back to Repositories
	if m.focus != paneRepos || m.groups.dash.FocusedID() != dashRepos {
		t.Fatalf("tab cycle: focus %v on %q", m.focus, m.groups.dash.FocusedID())
	}
	if m = run(t, m, "3", "tab"); m.page != pageRunners {
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
	m = run(t, m, "3", "p", "+", "d")
	if len(c.actions()) != 0 || m.overlay != ovNone {
		t.Fatalf("repo keys on the Runners page: overlay %v actions %v", m.overlay, c.actions())
	}
	run(t, m, "6", "x")
	if len(c.actions()) != 0 {
		t.Fatalf("x on the Settings page: actions %v", c.actions())
	}
}

// While the daemon is unreachable, a does not open the dialog and says why.
func TestAddRepoUnavailableWhileUnreachable(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 30), statusMsg{err: errors.New("connection refused")})
	if m = feed(m, key("a")); m.overlay == ovAddRepo || !strings.Contains(m.View(), "daemon is unreachable") {
		t.Fatalf("a opened the dialog while unreachable: overlay %v", m.overlay)
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
	if upd.(Model).page != pageDetail {
		t.Fatal("double click did not open the detail page")
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
	if m.page != pageDetail || strings.Join(c.calls, "|") != "containers a3f9c1" {
		t.Fatalf("page %v calls %v", m.page, c.calls)
	}
	v := feed(m, key("left")).View() // the Containers tab, wrapping round from Steps
	for _, want := range []string{"[ Containers ]", "running", "darkcloud-db-1", "postgres:17", "ghr-a3f9c1",
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
	if m = feed(m, key("left")); !strings.Contains(m.View(), "darkcloud-db-1") {
		t.Fatal("containers not refreshed")
	}
	m = feed(m, key("right")) // back to Steps
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

	m = feed(m, keys("3", "up")...) // follow a3f9c1
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

	m = feed(m, key("4"))
	m = feed(m, key("r")) // filter repo darkcloud
	m = feed(m, historyMsg{hist: []model.HistoryEntry{{Repo: "unfiltered", JobName: "stale"}}})
	if len(m.hist) != 0 {
		t.Fatalf("history response for the old filter applied: %v", m.hist)
	}
}

func TestRunnersTabFollowsSelection(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), key("3"))
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
	m = feed(m, keys("1", "l")...) // the Runners card kept focus from the click
	if m.page != pageDetail || m.groups.tabs.active != tabLog || m.logID != "a3f9c1" {
		t.Fatalf("l: page %v tab %d log %q", m.page, m.groups.tabs.active, m.logID)
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

func TestSettingsRetriesLoad(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.page = pageSettings
	if v := m.View(); !strings.Contains(v, "loading…") {
		t.Fatalf("no loading state:\n%s", v)
	}
	c.cfg = sampleConfig(t, "darkcloud")
	m = ticks(m, 1)
	v := m.View()
	if !strings.Contains(v, "─ General ") || strings.Contains(v, "loading…") {
		t.Fatalf("config not loaded by the tick:\n%s", v)
	}
}

func TestSettingsGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			c := &fakeClient{cfg: parseConfig(t, settingsYAML)}
			m := feed(sampleModel(c, w, 30), key("6"))
			golden.RequireEqual(t, []byte(m.View()))
		})
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
		check(t, run(t, newModel(&fakeClient{}, 120, h, st), down("3", 9)...), "▸ run09")
	})
	t.Run("settings", func(t *testing.T) {
		upd, _ := newModel(&fakeClient{}, 120, h, st).Update(configMsg{cfg: sampleConfig(t, repos[:6]...)})
		check(t, run(t, upd.(Model), "6"), "[ queue ▾ ]")
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
	m = click(t, m, btnYes)
	if m.overlay != ovNone || strings.Join(c.actions(), "|") != "kill a3f9c1" {
		t.Fatalf("overlay %v calls %v", m.overlay, c.actions())
	}

	m = run(t, m, "x")
	m = click(t, m, btnNo)
	if m.overlay != ovNone || len(c.actions()) != 1 {
		t.Fatalf("No: overlay %v calls %v", m.overlay, c.actions())
	}
}

// Confirm and Help share one modal: a bold title, the body, buttons on the
// right. enter presses the focused button (Yes at first), esc presses No.
func TestConfirmModal(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = run(t, m, "x")
	v := m.View()
	for _, want := range []string{"Confirm", "Runner a3f9c1 is running a job. Stop it?", "( No )  › [ Yes ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("modal missing %q:\n%s", want, v)
		}
	}
	m = feed(m, key("q")) // not a dialog key: ignored, the dialog stays
	if m.overlay != ovConfirm {
		t.Fatal("an unrelated key closed the dialog")
	}
	m = feed(m, keys("tab", " ")...) // focus No, press it with space
	if m.overlay != ovNone || len(c.actions()) != 0 {
		t.Fatalf("No: overlay %v actions %v", m.overlay, c.actions())
	}
	m = feed(m, keys("x", "esc")...)
	if m.overlay != ovNone || len(c.actions()) != 0 {
		t.Fatalf("esc: overlay %v actions %v", m.overlay, c.actions())
	}
	m = feed(m, keys("x", "enter")...)
	if m.overlay != ovNone || strings.Join(c.actions(), "|") != "kill a3f9c1" {
		t.Fatalf("enter: overlay %v actions %v", m.overlay, c.actions())
	}
}

// enter presses the focused button; focus opens on the primary.
func TestModalEnterPressesFocused(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = feed(m, keys("x", "tab", "enter")...)
	if m.overlay != ovNone || len(c.actions()) != 0 {
		t.Fatalf("enter on No: overlay %v actions %v", m.overlay, c.actions())
	}
	m = feed(m, keys("x", "enter")...)
	if m.overlay != ovNone || strings.Join(c.actions(), "|") != "kill a3f9c1" {
		t.Fatalf("enter on Yes: overlay %v actions %v", m.overlay, c.actions())
	}
}

func TestAddRepoEnterOnCancelCancels(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), keys("a", "tab", "tab", "tab", "tab")...)
	if m.add == nil || m.add.group.FocusedID() != addCancel {
		t.Fatalf("focus not on Cancel: %v", m.add)
	}
	m = feed(m, key("enter"))
	if m.overlay != ovNone || len(c.actions()) != 0 {
		t.Fatalf("enter on Cancel: overlay %v actions %v", m.overlay, c.actions())
	}
}

// A long body wraps so the dialog and its buttons stay on an 80-column screen.
func TestModalWrapsToViewport(t *testing.T) {
	m := sampleModel(&fakeClient{}, 80, 30)
	upd, _ := m.openConfirm(strings.Repeat("a very long confirmation sentence ", 6), func() tea.Cmd { return nil })
	v := upd.(Model).View()
	for i, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line %d is %d wide", i, lipgloss.Width(line))
		}
	}
	if !strings.Contains(v, "[ Yes ]") {
		t.Fatalf("buttons lost:\n%s", v)
	}
}

func TestHelpMentionsMouseNotes(t *testing.T) {
	v := run(t, sampleModel(&fakeClient{}, 120, 30), "?").View()
	for _, want := range []string{"Shift-drag", "Option-drag in iTerm2", "set -g mouse on", "Close", "takes digits"} {
		if !strings.Contains(v, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestUntrustedTextIsSanitized(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 30), key("3"))
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

// Every status string a page renders is cleaned on arrival.
func TestStatusTextIsCleanedOnArrival(t *testing.T) {
	st := sampleStatus()
	st.Mode = "queue\x1b[31m"
	st.Repos[0].Name = "dark\x07cloud"
	i := &st.Instances[0]
	i.ID, i.Repo, i.RunnerName, i.State = "a3f9\x1b]0;x\x07c1", "dark\x00cloud", "ghr-\x1b[2Ja3f9c1", "bu\rsy"
	m := newModel(&fakeClient{}, 120, 30, st)
	got := []string{m.st.Mode, m.st.Repos[0].Name, m.st.Instances[0].ID, m.st.Instances[0].Repo,
		m.st.Instances[0].RunnerName, m.st.Instances[0].State}
	want := []string{"queue", "darkcloud", "a3f9c1", "darkcloud", "ghr-a3f9c1", "busy"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A refresh that removes an earlier row keeps the selection on the same
// runner and repo, so a row button never acts on the row that moved up.
func TestSelectionFollowsRunnerAndRepoAcrossRefresh(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	m.runnerSel, m.repoSel = 1, 1 // 7be210, darkmem
	st := sampleStatus()
	st.Instances = append(st.Instances[1:], model.InstanceStatus{ID: "c0ffee", Repo: "darkmem", State: "idle", Since: now})
	st.Repos = st.Repos[1:]
	m = feed(m, statusMsg{st: st})
	if r := m.selectedRunner(); r == nil || r.ID != "7be210" {
		t.Fatalf("runner selection moved: %+v", r)
	}
	if r := m.selectedRepo(); r == nil || r.Name != "darkmem" {
		t.Fatalf("repo selection moved: %+v", r)
	}
	st = sampleStatus()
	st.Instances = st.Instances[:1]
	m = feed(m, statusMsg{st: st}) // the selected runner has gone: the selection clamps
	if r := m.selectedRunner(); r == nil || r.ID != "a3f9c1" {
		t.Fatalf("runner selection after its runner left: %+v", r)
	}
}

// With the daemon unreachable, the keys that change its state send nothing
// and say why, as the disabled header buttons do.
func TestDaemonKeysWaitForConnection(t *testing.T) {
	const want = "the daemon is unreachable; try again once it reconnects"
	down := statusMsg{err: errors.New("connection refused")}
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), down)
	for _, k := range []string{"p", "P", "+", "-", "[", "]", "m", "d"} {
		m.toast.Text = ""
		if m = feed(m, key(k)); m.toast.Text != want || m.overlay != ovNone {
			t.Errorf("%s on the repos card: toast %q overlay %v", k, m.toast.Text, m.overlay)
		}
	}
	m.focusCard(paneRunners)
	m.toast.Text = ""
	if m = feed(m, key("x")); m.toast.Text != want || m.overlay != ovNone {
		t.Errorf("x on the runners card: toast %q overlay %v", m.toast.Text, m.overlay)
	}
	m = feed(detailModel(t, c), down)
	if m = feed(m, key("x")); m.toast.Text != want || m.overlay != ovNone {
		t.Errorf("x on the detail page: toast %q overlay %v", m.toast.Text, m.overlay)
	}
	if got := c.actions(); len(got) != 0 {
		t.Fatalf("actions %q", got)
	}
}
