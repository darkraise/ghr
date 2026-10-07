# ghr toolchains 3: Storage page, release and LXC acceptance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the ghr TUI a Storage page (Docker disk with prune scopes, toolchains with Install and Remove, package caches with Clear, recent operations), move disk and prune off the Settings page, then release ghr and run the spec's LXC acceptance.

**Architecture:** A new `internal/tui/storage.go` holds a `storagePage` (the last `GET /storage` snapshot, its buttons and focus group) and renders four cards through a small card builder that records each button's line range for scrolling. The page slots in as page 5 (Settings moves to 6) through the per-page tables. The Install dialog lives in `internal/tui/install.go`, modelled on the Add repository dialog. Actions go through the confirmation dialog and `Model.action`, so their results arrive as `doneMsg` toasts; finished queued operations are announced from the snapshot.

**Tech Stack:** Go 1.26 (go.mod), Bubble Tea, lipgloss, bubblezone, `charmbracelet/x/exp/golden`; the `internal/tui/ui` widgets; plan 2's `api.Client` storage methods; bash, ssh and tmux for the acceptance.

**Spec:** docs/superpowers/specs/2026-10-06-ghr-toolchains-caches-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 1 of 8 tasks is heavy (Task 8: risk 3), so the session self-implements the other seven; the highest self-implemented total is 4 (impl-sonnet-medium), raised to `high` because Task 8 counts as delegated. Task 8 is not dispatched to an implementer: it needs your human partner's approval mid-task, so the controlling session runs it with the review the delegated path carries.

**Plan review:** 2026-10-06 — dr-superpowers:judge-opus — executability 16 / coherence 18 / coverage 18 / assumptions 14 (round 2)

## Global Constraints

- Code repository: `D:/Repositories/Personal/ghr` (github.com/darkraise/ghr). Work in a worktree at `D:/Repositories/Personal/ghr-toolchains-tui` on a new branch `feat/toolchains-tui` from master `4bbaf32`. The plan, spec and register live in `D:/Repositories/Personal/homelab`; Task 8 edits the register there and copies `github-runner/` to the LXC.
- This plan is part 3 of 3 for the spec: 1 installers (merged at `c6f5f0b`), 2 daemon, API, CLI and `setup.sh` (merged at `cd436df`, storage hardening follow-ups at `4bbaf32`), 3 TUI, release and LXC acceptance (this plan).
- Never push, never open a pull request, never commit `.superpowers/`, until Task 8's approval step. Pushing `master` publishes a release (`.github/workflows/ci.yml` release job).
- Every command runs from the ghr worktree root in Git Bash with an explicit `timeout` (git included), unless a step names another directory. Package tests: `timeout 300 go test ./internal/tui/...`. Before each commit also run `timeout 120 gofmt -l internal cmd` (must print nothing; fix with `gofmt -w <file>`) and `timeout 300 go vet ./...` (must print nothing).
- Edit Go files with the Edit or Write tools. Bash heredocs and inline `python -` lose backslashes.
- Comments: none unless the why is non-obvious; never reference this plan, a task, a review or a finding.
- Commits: `<type>(<scope>): <subject>`, subject ≤ 50 characters, imperative, no period, scope `tui`. One commit per task.
- TUI tests run with the ASCII colour profile and `time.Local = time.UTC` (`internal/tui/tui_test.go` `TestMain`); the fixed clock is `now` = 2026-10-03 14:05 UTC. Badges read `[TEXT]` in tests.
- Goldens live under `internal/tui/testdata` and are regenerated with `-update` only after reading why they changed; every golden diff is read before committing.
- Page order and keys (spec §6): Dashboard 1, Repositories 2, Runners 3, History 4, Storage 5, Settings 6. Tab-row short names: `Dash`, `Repo`, `Run`, `Hist`, `Stor`, `Set`.
- The Storage page polls `GET /storage` every 5 ticks (`slowPoll`), and every tick while an operation runs or waits or a measurement is in progress.
- Prune scopes, exactly: `standard`, `build-cache-keep`, `build-cache-all`, `dangling-images`, `unused-volumes`. The **Prune** button sends `POST /prune` (`Client.Prune`); the others `POST /prune/{scope}` (`Client.PruneScope`).
- While any runner's state is `busy`, Remove, Clear and **Unused volumes** stay enabled and show the hint `<what>: refused while N jobs run`.
- At under 100 columns (`wideMin`) the Docker table drops its reclaimable column and the package-cache rows drop their files column.

## Contracts

**C1 TUI client (Task 1):** `tui.Client` gains `Storage(ctx) (model.Storage, error)`, `RefreshStorage(ctx) error`, `AvailableToolchains(ctx, tool string) ([]model.ToolchainChoice, error)`, `InstallToolchain(ctx, tool, version string) error`, `InstallPreset(ctx, preset string) error`, `RemoveToolchain(ctx, tool, version string) error`, `ClearCache(ctx, name string) error`, `PruneScope(ctx, scope string) error` — the `*api.Client` methods of the same names. The test `fakeClient` gains fields `storage *model.Storage`, `storageErr error`, `storageN int`, `choices map[string][]model.ToolchainChoice`, `choicesErr error`, `availTools []string`, `installErr error`, `storeErr error`, and records actions as `refresh-storage`, `install <tool> <version>`, `install-preset <preset>`, `rm-toolchain <tool> <version>`, `clear <name>`, `prune <scope>` (plain `Prune` keeps recording `prune`). `sampleStorage() model.Storage` is the test snapshot (tui_test.go).

**C2 Storage page (Task 2):** `pageStorage` sits between `pageHistory` and `pageSettings`. `Model.store *storagePage`. Control IDs: `storeBody`, `storePrune`, `storeKeep`, `storeAll`, `storeDangling`, `storeVolumes`, `storeInstall`, `storePopular`, `storeRefresh`, and the prefixes `storeRmPrefix` (`storage/rm/` + `<tool>/<version>`) and `storeClearPrefix` (`storage/clear/` + cache name). `storagePage` fields: `group ui.Group`, `scroll int`, `shownFocus string`, `data model.Storage`, `loaded bool`, `err string`, `seq int`, `shown int`, `lastOp string`, buttons `prune, keep, all, dangling, volumes, install, popular, refresh *ui.Button`, maps `rm, clear map[string]*ui.Button`. `storageMsg{seq int; s model.Storage; err error}`. Functions: `(m Model) fetchStorage() tea.Cmd`, `(s *storagePage) active() bool`, `(m Model) gotStorage(storageMsg) (tea.Model, tea.Cmd)`, `(m *Model) announce(recent []model.Operation)`, `cleanStorage(model.Storage) model.Storage`, `(m Model) opsCard(w int) []string`, `(m Model) storageKey(tea.KeyMsg) (bool, tea.Model, tea.Cmd)`, `(m Model) storageMouse(tea.MouseMsg) (bool, tea.Model, tea.Cmd)`, `(m Model) storageFooterKeys() []footerKey`, `(m Model) syncStorage()`, `(m Model) storageLines(w int) ([]string, map[string]ui.Range)`, `(m Model) storageView(w, h int) string`, `(m Model) storagePressed(id string) (tea.Model, tea.Cmd, bool)`. Card builder: `newStoreCard() *storeCard` with `add(lines ...string)`, `right(text string, b *ui.Button, focused string, w int)`, `buttons(focused string, w int, bs ...*ui.Button)`, `box(title string, w int) ([]string, map[string]ui.Range)`. Test helper `onStorage(t, c *fakeClient, w, h int) Model` (storage_test.go).

**C3 Docker card (Task 3):** `(m Model) busyJobs() int`, `(m Model) refusedHint(what string) string`, `(m Model) dockerWidgets() []ui.Widget`, `(m Model) dockerCard(w int) ([]string, map[string]ui.Range)`. Test helper `withConfig(m Model, c *fakeClient) Model` loads a config with `disk_high_water: 80` and `build_cache_keep: 20GB` into the model and into `c`, so the refetch after every action keeps it (storage_test.go).

**C4 Toolchains and caches (Task 4):** `toolchainKey(t model.Toolchain) string` (`<tool>/<version>`), `(m Model) toolchainWidgets() []ui.Widget`, `(m Model) cacheWidgets() []ui.Widget`, `(m Model) toolchainsCard(w int) ([]string, map[string]ui.Range)`, `(m Model) cachesCard(w int) ([]string, map[string]ui.Range)`.

**C5 Install dialog (Task 5):** overlay `ovInstall`, `Model.inst *installDialog`, IDs `instTool`, `instPick`, `instRetry`, `instOK`, `instCancel`; `(m Model) openInstall() (tea.Model, tea.Cmd)`; messages `choicesMsg`, `installedMsg`. Test helper `nodeChoices() map[string][]model.ToolchainChoice` (install_test.go: node 24.9.0, 22.11.0 LTS, 22.10.0 LTS; python 3.13.7).

## Assumptions (evidence)

- The LXC (`root@192.168.0.99`, key `~/.ssh/ghr_lxc`) runs ghr v0.1.10, mode queue, no runner alive; its tool cache holds `dotnet` (SDKs 10.0.303 and 10.0.401), `go/1.26.8` and `node/20.20.2`, written by jobs' own `setup-*` steps; `docker system df` shows 68 local volumes (8.65 GB, 67 dangling) and an empty build cache; `/home/ghrunner` has `.nuget` and `.npm`; `tmux` and `jq` are installed; `/etc/os-release` `VERSION_ID="13"` (ssh, 2026-10-06).
- The newest ghr release is v0.1.10 (`gh release list -R darkraise/ghr`, 2026-10-06), so CI tags this plan's merge v0.1.11.
- `darkraise/ghr-e2e` is private; its workflows are `cap.yml`, `compose.yml`, `single.yml`, `testcontainers.yml`; `single.yml` runs `on: workflow_dispatch` with `runs-on: [self-hosted, homelab]` and sleeps 30 s (`gh api`, 2026-10-06).
- Latest action majors: setup-node v7.0.0, setup-python v7.0.0, setup-dotnet v6.0.0, setup-go v7.0.0, setup-java v6.0.1 (`gh api repos/actions/<a>/releases/latest`, 2026-10-06).
- `ui.Picker` asks to advance focus after a pick (`internal/tui/ui/picker.go:146-150`); `ui.Select` cycles with left/right while closed (`internal/tui/ui/select.go:73-124`); `api.Error.Error()` returns `Msg` (`internal/api/server.go:25`); `model.HumanBytes` prints `%.1f` with decimal units (`internal/model/storage.go`).
- Goldens regenerate with `go test ./internal/tui -run <Test> -update` (`docs/superpowers/plans/2026-10-05-ghr-repositories-management.md:21`).
- The TUI counts busy runners as instances whose state is `busy`; the daemon refuses on `BusyCount()`, which counts a runner from the moment its job hook writes `job.json`. unverified that both agree on the LXC — Task 8 Step 7 verifies it.
- The Codex executor lane is closed (`codex-gate usable=false reason=quota`, 2026-10-06), so no task carries an Executor line.

## Task index

1. TUI client storage methods
2. Storage page navigation and polling
3. Docker disk card and prune scopes
4. Toolchains and package caches cards
5. Install dialog
6. Settings without disk and prune
7. Storage goldens
8. Release and LXC acceptance

---

### Task 1: TUI client storage methods

**Files:**
- Modify: `internal/tui/model.go:20-48` (the `Client` interface)
- Modify: `internal/tui/tui_test.go` (the `fakeClient` struct and methods, `sampleStorage`)

**Interfaces:**
- Consumes: plan 2's `*api.Client` storage methods (`internal/api/client.go:229-270`).
- Produces: C1.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

First create the worktree:

```bash
cd /d/Repositories/Personal/ghr
timeout 60 git worktree add -b feat/toolchains-tui ../ghr-toolchains-tui 4bbaf32
cd ../ghr-toolchains-tui
```

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/tui_test.go`:

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 300 go test ./internal/tui/ -run TestClientServesStorage`
Expected: a build failure, `c.Storage undefined (type Client has no field or method Storage)`.

- [ ] **Step 3: Extend the interface and the fake**

In `internal/tui/model.go`, add to the `Client` interface, after `AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error)`:

```go
	Storage(ctx context.Context) (model.Storage, error)
	RefreshStorage(ctx context.Context) error
	AvailableToolchains(ctx context.Context, tool string) ([]model.ToolchainChoice, error)
	InstallToolchain(ctx context.Context, tool, version string) error
	InstallPreset(ctx context.Context, preset string) error
	RemoveToolchain(ctx context.Context, tool, version string) error
	ClearCache(ctx context.Context, name string) error
	PruneScope(ctx context.Context, scope string) error
```

In `internal/tui/tui_test.go`, add to the `fakeClient` struct, after the `availCalls  int` line:

```go
	storage     *model.Storage                     // Storage returns it; nil means sampleStorage()
	storageErr  error                              // returned by Storage when set
	storageN    int                                // Storage calls
	choices     map[string][]model.ToolchainChoice // AvailableToolchains answers per tool
	choicesErr  error                              // returned by AvailableToolchains when set
	availTools  []string                           // each AvailableToolchains request's tool
	installErr  error                              // returned by InstallToolchain and InstallPreset
	storeErr    error                              // returned by RefreshStorage, RemoveToolchain, ClearCache and PruneScope
```

Add these methods directly after the `AvailableRepos` method:

```go
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
```

Add directly after the `sampleStatus` function:

```go
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
```

- [ ] **Step 4: Run the tests**

Run: `timeout 300 go test ./internal/tui/... && timeout 300 go build ./...`
Expected: `ok` for `internal/tui` and `internal/tui/ui`; the build succeeds, which proves `*api.Client` still satisfies `tui.Client` (`cmd/ghr` passes it to `tui.Run`).

- [ ] **Step 5: Commit**

```bash
timeout 120 gofmt -l internal cmd; timeout 300 go vet ./...
timeout 60 git add internal/tui/model.go internal/tui/tui_test.go
timeout 60 git commit -m "feat(tui): add storage methods to the client"
```

---

### Task 2: Storage page navigation and polling

**Files:**
- Create: `internal/tui/storage.go`
- Create: `internal/tui/storage_test.go`
- Modify: `internal/tui/model.go` (page constants, `pageNames`, `Model`, `New`, tick, `Update`)
- Modify: `internal/tui/shell.go` (`tabRow` short names, `footerKeys`, `footerPress`)
- Modify: `internal/tui/input.go` (`handleKey`, `press`, `switchPage`, `move`, `enter`, `handleMouse`)
- Modify: `internal/tui/view.go` (`pageBody`)
- Modify: `internal/tui/dialogs.go` (`helpGroups`, `helpText`, `pressed`)
- Modify: `internal/tui/settings_test.go:136`, `internal/tui/tui_test.go:608,808,850`, `internal/tui/dialogs_test.go:18-26` (Settings moves to key 6), `internal/tui/shell_test.go:70` (six full-name tabs need 94 columns)
- Regenerate: every golden under `internal/tui/testdata`

**Interfaces:**
- Consumes: C1.
- Produces: C2.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The edits to `model.go`, `shell.go`, `input.go`, `view.go` and `dialogs.go` are one decision repeated: each per-page table gains the Storage entry.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/storage_test.go`:

```go
package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/model"
)

// onStorage opens the Storage page on the sample model, w by h.
func onStorage(t *testing.T, c *fakeClient, w, h int) Model {
	t.Helper()
	m := feed(sampleModel(c, w, h), key("5"))
	if m.page != pageStorage {
		t.Fatalf("key 5 went to page %v", m.page)
	}
	return m
}

func TestStorageNavigation(t *testing.T) {
	m := onStorage(t, &fakeClient{}, 120, 40)
	v := m.View()
	for _, want := range []string{"5 Storage", "6 Settings", "Recent operations", "13:55", "[REFUSED]",
		"refused: 1 jobs running", "installed node 22.11.0"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	if m = feed(m, key("6")); m.page != pageSettings {
		t.Fatalf("key 6 went to page %v", m.page)
	}
	if v := onStorage(t, &fakeClient{}, 80, 30).View(); !strings.Contains(v, "5 Stor") {
		t.Fatalf("tab row:\n%s", v)
	}
}

func TestStoragePollsEverySecondWhileBusy(t *testing.T) {
	c := &fakeClient{}
	m := onStorage(t, c, 120, 40)
	base := c.storageN
	if m = ticks(m, 4); c.storageN != base {
		t.Fatalf("an idle page polled on ticks 1-4: %d reads", c.storageN-base)
	}
	if m = ticks(m, 1); c.storageN != base+1 {
		t.Fatalf("an idle page did not poll on tick 5: %d reads", c.storageN-base)
	}
	st := sampleStorage()
	st.Operations.Current = &model.Operation{ID: "op3", Kind: "install", Target: "node 24", StartedAt: now, Progress: "extracting"}
	c.storage = &st
	m = feed(m, storageMsg{seq: m.store.seq, s: st})
	base = c.storageN
	if m = ticks(m, 3); c.storageN != base+3 {
		t.Fatalf("a busy page polled %d times in 3 ticks", c.storageN-base)
	}
}

func TestStorageAnnouncesOnlyNewlyFinishedOperations(t *testing.T) {
	m := onStorage(t, &fakeClient{}, 120, 40)
	if m.toast.Active() {
		t.Fatalf("toast on opening: %q", m.toast.Text)
	}
	st := sampleStorage()
	done := now
	st.Operations.Recent = append([]model.Operation{{ID: "op3", Kind: "clear", Target: "nuget", StartedAt: now,
		FinishedAt: &done, Outcome: "refused", Message: "refused: 2 jobs running"}}, st.Operations.Recent...)
	m = feed(m, storageMsg{seq: m.store.seq, s: st})
	if !m.toast.Active() || !m.toast.Err || m.toast.Text != "clear nuget: refused: 2 jobs running" {
		t.Fatalf("toast %q err=%v", m.toast.Text, m.toast.Err)
	}
	m.toast.Close()
	if m = feed(m, storageMsg{seq: m.store.seq, s: st}); m.toast.Active() {
		t.Fatal("the same operation was announced twice")
	}
}

func TestStorageKeepsTheNewestReply(t *testing.T) {
	m := onStorage(t, &fakeClient{}, 120, 40)
	old := sampleStorage()
	old.Operations.Recent = nil
	if m = feed(m, storageMsg{seq: m.store.shown - 1, s: old}); len(m.store.data.Operations.Recent) != 2 {
		t.Fatal("an older reply replaced the newer one")
	}
	m = feed(m, storageMsg{seq: m.store.shown, err: errors.New("connection refused")})
	if v := m.View(); !strings.Contains(v, "✖ connection refused") || !strings.Contains(v, "Recent operations") {
		t.Fatalf("a failed refresh should keep the last snapshot under the error:\n%s", v)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `timeout 300 go test ./internal/tui/ -run 'TestStorage'`
Expected: a build failure naming `pageStorage`, `storageMsg` or `m.store`.

- [ ] **Step 3: Create `internal/tui/storage.go`**

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

// Storage page control IDs. A Remove button's ID is storeRmPrefix plus
// <tool>/<version>; a Clear button's is storeClearPrefix plus the cache name.
const (
	storeBody        = "storage/body"
	storePrune       = "storage/prune"
	storeKeep        = "storage/prune/keep"
	storeAll         = "storage/prune/all"
	storeDangling    = "storage/prune/dangling"
	storeVolumes     = "storage/prune/volumes"
	storeInstall     = "storage/install"
	storePopular     = "storage/popular"
	storeRefresh     = "storage/refresh"
	storeRmPrefix    = "storage/rm/"
	storeClearPrefix = "storage/clear/"
)

// storagePage is the Storage page: the last GET /storage snapshot and the
// page's buttons. The Model holds it by pointer, so it survives Bubble Tea
// copying the Model.
type storagePage struct {
	group      ui.Group
	scroll     int
	shownFocus string // the focus storageView last scrolled to
	data       model.Storage
	loaded     bool
	err        string
	seq        int    // numbers Storage requests
	shown      int    // the newest request whose reply is shown
	lastOp     string // the newest finished operation already announced

	prune, keep, all, dangling, volumes *ui.Button
	install, popular, refresh           *ui.Button
	rm                                  map[string]*ui.Button // by toolchainKey
	clear                               map[string]*ui.Button // by cache name
}

func newStoragePage() *storagePage {
	return &storagePage{
		prune:    ui.NewButton(storePrune, "Prune", ui.Primary),
		keep:     ui.NewButton(storeKeep, "Build cache to keep", ui.Secondary),
		all:      ui.NewButton(storeAll, "All build cache", ui.Danger),
		dangling: ui.NewButton(storeDangling, "Dangling images", ui.Secondary),
		volumes:  ui.NewButton(storeVolumes, "Unused volumes", ui.Danger),
		install:  ui.NewButton(storeInstall, "Install…", ui.Primary),
		popular:  ui.NewButton(storePopular, "Install popular set", ui.Secondary),
		refresh:  ui.NewButton(storeRefresh, "Refresh", ui.Secondary),
		rm:       map[string]*ui.Button{},
		clear:    map[string]*ui.Button{},
	}
}

type storageMsg struct {
	seq int
	s   model.Storage
	err error
}

// fetchStorage asks for the snapshot. Replies can arrive out of order; one
// older than the reply shown is dropped.
func (m Model) fetchStorage() tea.Cmd {
	m.store.seq++
	seq, c := m.store.seq, m.c
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		s, err := c.Storage(cx)
		return storageMsg{seq, s, err}
	}
}

// active reports whether the page polls every tick: an operation runs or
// waits, or a measurement is in progress.
func (s *storagePage) active() bool {
	o := s.data.Operations
	return o.Current != nil || o.Queued > 0 || s.data.Measuring
}

// gotStorage shows a snapshot. A failed read keeps the last one and names
// the failure above it.
func (m Model) gotStorage(msg storageMsg) (tea.Model, tea.Cmd) {
	s := m.store
	if msg.seq < s.shown {
		return m, nil
	}
	s.shown = msg.seq
	if msg.err != nil {
		s.err = errText(msg.err)
		return m, nil
	}
	data := cleanStorage(msg.s)
	m.announce(data.Operations.Recent)
	s.data, s.loaded, s.err = data, true, ""
	return m, nil
}

// announce shows a toast for the newest operation that finished since the
// last snapshot. The first snapshot only records where the page starts, so
// opening it does not replay old results.
func (m *Model) announce(recent []model.Operation) {
	s := m.store
	if len(recent) == 0 || recent[0].ID == s.lastOp {
		return
	}
	o := recent[0]
	s.lastOp = o.ID
	if !s.loaded {
		return
	}
	text := o.Kind + " " + o.Target + ": " + o.Outcome
	if o.Message != "" {
		text = o.Kind + " " + o.Target + ": " + o.Message
	}
	m.toast.Show(text, o.Outcome != "ok" && o.Outcome != "skipped", m.now())
}

// cleanStorage sanitises the text fields of s in place; the slices come from
// a freshly decoded response nobody else holds.
func cleanStorage(s model.Storage) model.Storage {
	for i := range s.Toolchains {
		t := &s.Toolchains[i]
		t.Tool, t.Version, t.Arch, t.Path = clean(t.Tool), clean(t.Version), clean(t.Arch), clean(t.Path)
	}
	for i := range s.OtherToolCache {
		s.OtherToolCache[i].Name = clean(s.OtherToolCache[i].Name)
	}
	for i := range s.PackageCaches {
		c := &s.PackageCaches[i]
		c.Name, c.Label, c.Paths = clean(c.Name), clean(c.Label), cleanList(c.Paths)
	}
	for i := range s.Docker.Rows {
		s.Docker.Rows[i].Type = clean(s.Docker.Rows[i].Type)
	}
	for i := range s.Docker.BuildCacheTypes {
		s.Docker.BuildCacheTypes[i].Type = clean(s.Docker.BuildCacheTypes[i].Type)
	}
	s.MeasureError = clean(s.MeasureError)
	op := func(o *model.Operation) {
		o.ID, o.Kind, o.Target, o.Progress = clean(o.ID), clean(o.Kind), clean(o.Target), clean(o.Progress)
		o.Outcome, o.Message = clean(o.Outcome), clean(o.Message)
	}
	if s.Operations.Current != nil {
		op(s.Operations.Current)
	}
	for i := range s.Operations.Recent {
		op(&s.Operations.Recent[i])
	}
	if p := s.LastPrune; p != nil {
		p.Trigger, p.Scope, p.Outcome = clean(p.Trigger), clean(p.Scope), clean(p.Outcome)
		for i := range p.Steps {
			p.Steps[i].Name, p.Steps[i].Error = clean(p.Steps[i].Name), clean(p.Steps[i].Error)
		}
	}
	return s
}

// storeCard collects a card's inside lines and the line range of each of
// its buttons, counted from the card's top border.
type storeCard struct {
	lines  []string
	ranges map[string]ui.Range
}

func newStoreCard() *storeCard { return &storeCard{ranges: map[string]ui.Range{}} }

func (c *storeCard) add(lines ...string) { c.lines = append(c.lines, lines...) }

// right adds text with b at the right end of a w-column line.
func (c *storeCard) right(text string, b *ui.Button, focused string, w int) {
	v := b.View(b.ID() == focused, 0)
	c.ranges[b.ID()] = ui.Range{Start: len(c.lines) + 1, End: len(c.lines) + 2}
	c.add(cell(text, w-ansi.StringWidth(v)) + v)
}

// buttons lays bs out left to right in w columns, starting a new line when
// the next one would not fit.
func (c *storeCard) buttons(focused string, w int, bs ...*ui.Button) {
	start := len(c.lines)
	var rows []string
	for _, b := range bs {
		v := b.View(b.ID() == focused, 0)
		if n := len(rows); n > 0 && ansi.StringWidth(rows[n-1]+" "+v) <= w {
			rows[n-1] += " " + v
		} else {
			rows = append(rows, v)
		}
		c.ranges[b.ID()] = ui.Range{Start: start + len(rows), End: start + len(rows) + 1}
	}
	c.add(rows...)
}

func (c *storeCard) box(title string, w int) ([]string, map[string]ui.Range) {
	return strings.Split(box(title, w, c.lines), "\n"), c.ranges
}

// opsCard lists the last ten finished operations, newest first.
func (m Model) opsCard(w int) []string {
	c := newStoreCard()
	ops := m.store.data.Operations.Recent
	if len(ops) == 0 {
		c.add(sDim.Render("no operations since the daemon started"))
	}
	for _, o := range ops {
		at := o.StartedAt
		if o.FinishedAt != nil {
			at = *o.FinishedAt
		}
		c.add(at.Local().Format("15:04") + "  " + cell(o.Kind, 8) + cell(o.Target, 20) + opBadge(o.Outcome) + "  " + sDim.Render(o.Message))
	}
	lines, _ := c.box("Recent operations", w)
	return lines
}

func opBadge(outcome string) string {
	kind := ui.BadgeMuted
	switch outcome {
	case "ok":
		kind = ui.BadgeOK
	case "refused", "interrupted":
		kind = ui.BadgeWarn
	case "failed":
		kind = ui.BadgeBad
	}
	return ui.Badge(outcome, kind)
}

// syncStorage brings the page's buttons up to date with the model and sets
// the focus order to the layout order. It runs before every key, click and
// frame.
func (m Model) syncStorage() {
	var ws []ui.Widget
	m.store.group.Set(ws)
}

// storageLines renders the page's cards w columns wide, with the line range
// of every button.
func (m Model) storageLines(w int) ([]string, map[string]ui.Range) {
	var out []string
	ranges := map[string]ui.Range{}
	add := func(lines []string, rs map[string]ui.Range) {
		for k, r := range rs {
			ranges[k] = ui.Range{Start: r.Start + len(out), End: r.End + len(out)}
		}
		out = append(out, lines...)
	}
	add(m.opsCard(w), nil)
	return out, ranges
}

// storageView renders the page in w columns and h lines, scrolled so a
// newly focused button is in view.
func (m Model) storageView(w, h int) string {
	s := m.store
	m.syncStorage()
	if !s.loaded {
		if s.err != "" {
			return sRed.Render(cell("✖ "+s.err, w))
		}
		return sDim.Render("loading…")
	}
	var top []string
	if s.err != "" {
		top = append(top, sRed.Render(cell("✖ "+s.err, w)))
	}
	bodyH := max(h-len(top), 1)
	lines, ranges := m.storageLines(w)
	// Focus moves by tab, by a click, or by Group.Set when a refresh disables
	// the focused button; checking once per frame catches every one of them.
	if f := s.group.FocusedID(); f != s.shownFocus {
		s.shownFocus = f
		if r, ok := ranges[f]; ok {
			s.scroll = ui.ScrollTo(s.scroll, bodyH, r)
		}
	}
	s.scroll = min(max(s.scroll, 0), max(len(lines)-bodyH, 0))
	body := lines[s.scroll:min(s.scroll+bodyH, len(lines))]
	return strings.Join(append(top, zone.Mark(storeBody, strings.Join(body, "\n"))), "\n")
}

// storageKey handles a key on the Storage page: the focused button first,
// then focus and scrolling. It reports false for keys that fall through to
// the global keys.
func (m Model) storageKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	s := m.store
	m.syncStorage()
	if ok, cmd := s.group.Key(k); ok {
		return true, m, cmd
	}
	_, h := m.contentSize()
	switch k.String() {
	case "tab", "down", "j":
		s.group.Next()
	case "shift+tab", "up", "k":
		s.group.Prev()
	case "pgup":
		s.scroll = max(s.scroll-h/2, 0)
	case "pgdown":
		s.scroll += h / 2
	default:
		return false, m, nil
	}
	return true, m, nil
}

// storageMouse handles the wheel over the cards and clicks on the buttons.
// It reports false for events the shell should handle.
func (m Model) storageMouse(msg tea.MouseMsg) (bool, tea.Model, tea.Cmd) {
	s := m.store
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		if !zone.Get(storeBody).InBounds(msg) {
			return false, m, nil
		}
		if msg.Button == tea.MouseButtonWheelUp {
			s.scroll = max(s.scroll-3, 0)
		} else {
			s.scroll += 3
		}
		return true, m, nil
	}
	m.syncStorage()
	ok, cmd := s.group.Mouse(msg)
	return ok, m, cmd
}

func (m Model) storageFooterKeys() []footerKey {
	return []footerKey{{"tab", "next"}, {"enter", "press"}, {"pgdown", "scroll"}, {"?", "help"}, {"q", "quit"}}
}

// storagePressed runs a Storage page button; ok is false for any other ID.
func (m Model) storagePressed(id string) (tea.Model, tea.Cmd, bool) {
	return m, nil, false
}
```

- [ ] **Step 4: Slot the page into the per-page tables**

`internal/tui/model.go`:

1. In the `page` constants, between `pageHistory` and `pageSettings`, add the line `pageStorage`.
2. Replace `var pageNames = []string{"Dashboard", "Repositories", "Runners", "History", "Settings"}` with `var pageNames = []string{"Dashboard", "Repositories", "Runners", "History", "Storage", "Settings"}`.
3. In the `Model` struct, directly after the line `repos    *reposPage`, add `store    *storagePage`.
4. In `New`, directly after the line `mg:    newManageState(),`, add `store: newStoragePage(),`.
5. In `Update`'s `case tickMsg:` block, directly before its `return m, tea.Batch(cmds...)`, add:

```go
		if m.page == pageStorage && (m.frame%slowPoll == 0 || m.store.active()) {
			cmds = append(cmds, m.fetchStorage())
		}
```

6. In `Update`, directly before `case regDeletedMsg:`, add:

```go
	case storageMsg:
		return m.gotStorage(msg)
```

7. In `Update`'s `case doneMsg:` block, replace `return m, tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig())` with:

```go
		cmds := []tea.Cmd{m.fetchStatus(), m.fetchEvents(), m.fetchConfig()}
		if m.page == pageStorage {
			cmds = append(cmds, m.fetchStorage())
		}
		return m, tea.Batch(cmds...)
```

`internal/tui/shell.go`:

1. In `tabRow`, replace `short := []string{"Dash", "Repo", "Run", "Hist", "Set"}` with `short := []string{"Dash", "Repo", "Run", "Hist", "Stor", "Set"}`.
2. In `footerKeys`, directly after `case pageRepos:` and its `return m.reposFooterKeys()`, add:

```go
	case pageStorage:
		return m.storageFooterKeys()
```

3. In `footerPress`, replace the comment line `// on "q quit" never types a q into a focused text field. The detail page has` and the next line `// no text fields and its own x, so every hint there takes the key's path.` with `// on "q quit" never types a q into a focused text field. The detail and` and `// Storage pages have no text fields, so every hint there takes the key's path.`, and replace `if m.page == pageDetail {` with `if m.page == pageDetail || m.page == pageStorage {`.

`internal/tui/input.go`:

1. In `handleKey`, directly after the block `if m.page == pageSettings { … }`, add:

```go
	if m.page == pageStorage {
		if ok, mm, cmd := m.storageKey(k); ok {
			return mm, cmd
		}
	}
```

2. In `press`, replace `case "1", "2", "3", "4", "5":` with `case "1", "2", "3", "4", "5", "6":`.
3. In `switchPage`, directly after `case pageHistory:` and its `return m, m.fetchHistory()`, add:

```go
	case pageStorage:
		return m, m.fetchStorage()
```

4. In `move`, replace `case m.page == pageSettings, m.page == pageDashboard && !m.onCard():` with `case m.page == pageSettings, m.page == pageStorage, m.page == pageDashboard && !m.onCard():`.
5. In `enter`, replace `case pageSettings:` (the one followed by `return m, nil`) with `case pageSettings, pageStorage:`.
6. In `handleMouse`, directly after the block `if m.overlay == ovNone && m.page == pageRepos { … }`, add:

```go
	if m.overlay == ovNone && m.page == pageStorage {
		if ok, mm, cmd := m.storageMouse(msg); ok {
			return mm, cmd
		}
	}
```

`internal/tui/view.go`: in `pageBody`, directly before `case pageSettings:`, add:

```go
	case pageStorage:
		return m.storageView(w, h)
```

`internal/tui/dialogs.go`:

1. In `helpGroups`, replace `{"Global", [][2]string{{"1-5", "switch page"},` with `{"Global", [][2]string{{"1-6", "switch page"},`, and add as the last element, after the Settings group:

```go
	{"Storage", [][2]string{{"tab", "next action"}, {"enter", "press"}, {"pgup/dn", "scroll"}}},
```

2. Replace the `helpGroups` comment's second and third lines, `// Detail, Dashboard over History, Repositories over Settings, then Runner` and `// rows. No column exceeds ten rows, so the dialog fits the 22-row minimum.`, with `// Detail, Dashboard over History, Repositories over Settings, Runner rows` and `// over Storage. No column exceeds ten rows, so the dialog fits the 22-row minimum.`
3. In `helpText`, replace `cols := [][]string{col(g[0], g[4]), col(g[1], g[5]), col(g[2], g[6]), col(g[3])}` with `cols := [][]string{col(g[0], g[4]), col(g[1], g[5]), col(g[2], g[6]), col(g[3], g[7])}`, and replace `"A focused stepper takes digits: 1-5 type into it instead of switching.",` with `"A focused stepper takes digits: 1-6 type into it instead of switching.",`.
4. In `pressed`, make the first lines of the `default:` branch:

```go
	default:
		if mm, cmd, ok := m.storagePressed(id); ok {
			return mm, cmd
		}
```

(the existing `if label, ok := lcAddLabel(id); ok {` block follows unchanged).

- [ ] **Step 5: Move the tests that open Settings with key 5**

- `internal/tui/settings_test.go:136`: `return feed(sampleModel(c, w, h), key("5"))` → `return feed(sampleModel(c, w, h), key("6"))`.
- `internal/tui/tui_test.go:608`: `run(t, m, "5", "x")` → `run(t, m, "6", "x")`.
- `internal/tui/tui_test.go:808`: `m := feed(sampleModel(c, w, 30), key("5"))` → `m := feed(sampleModel(c, w, 30), key("6"))`.
- `internal/tui/tui_test.go:850`: `check(t, run(t, upd.(Model), "5"), "[ queue ▾ ]")` → `check(t, run(t, upd.(Model), "6"), "[ queue ▾ ]")`.
- `internal/tui/shell_test.go:70`: in `TestSidebarAndTabRowNavigate`, `n := sampleModel(&fakeClient{}, 90, 30)` → `n := sampleModel(&fakeClient{}, 96, 30)`. Six full-name tabs plus `? help` and a one-column gap need 94 columns, so at 90 the row falls back to short names; 96 keeps the narrow layout (under `wideMin`) with full names.
- `internal/tui/dialogs_test.go`: in `TestHelpGroupsFit`, replace `"1-5", "add/remove/edit",` with `"1-6", "Storage", "add/remove/edit",`, and replace `{{"Global", "Detail"}, {"Dashboard", "History"}, {"Repositories", "Settings"}}` with `{{"Global", "Detail"}, {"Dashboard", "History"}, {"Repositories", "Settings"}, {"Runner rows", "Storage"}}`.

- [ ] **Step 6: Run the new tests**

Run: `timeout 300 go test ./internal/tui/ -run 'TestStorage|TestHelp|TestSettings|TestKey|TestShortTerminal|TestSidebar|TestNarrow'`
Expected: every `TestStorage*`, `TestHelp*`, `TestSidebar*` and `TestNarrow*` test passes; the only failures are `TestSettingsGolden` and the other golden tests, whose sidebar and tab row changed.

- [ ] **Step 7: Regenerate the goldens and read every diff**

```bash
timeout 300 go test ./internal/tui/ -update
timeout 60 git diff --stat -- internal/tui/testdata
timeout 60 git diff -- internal/tui/testdata | grep '^[-+]' | grep -v '^+++\|^---' | sort | uniq -c | sort -rn | head -40
```

Expected: every changed line is a sidebar line (`5 Storage` inserted, `6 Settings` renumbered, one blank sidebar row fewer) or a tab row (`5 Stor`/`6 Set`, or the numbers form), and nothing else. If any other line changed, stop and report it instead of committing.

- [ ] **Step 8: Run the whole package**

Run: `timeout 300 go test ./internal/tui/...`
Expected: `ok` for both packages.

- [ ] **Step 9: Commit**

```bash
timeout 120 gofmt -l internal cmd; timeout 300 go vet ./...
timeout 60 git add internal/tui
timeout 60 git commit -m "feat(tui): add the Storage page"
```

---

### Task 3: Docker disk card and prune scopes

**Files:**
- Modify: `internal/tui/storage.go`
- Modify: `internal/tui/storage_test.go`

**Interfaces:**
- Consumes: C2.
- Produces: C3.

**Items:** 4, 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

In `internal/tui/storage_test.go`, add `"github.com/darkraise/ghr/internal/api"` and `"github.com/darkraise/ghr/internal/config"` to the imports, then append:

```go
// withConfig loads a config with the disk settings the Storage page shows.
// c serves it too: every action's doneMsg fetches the config again.
func withConfig(m Model, c *fakeClient) Model {
	c.cfg = &config.Config{Mode: "queue", DiskHighWater: 80, BuildCacheKeep: "20GB"}
	cfg := *c.cfg
	return feed(m, configMsg{seq: m.nextCfgSeq(), cfg: &cfg})
}

func TestStorageDockerCard(t *testing.T) {
	c := &fakeClient{}
	m := withConfig(onStorage(t, c, 120, 60), c)
	v := m.View()
	for _, want := range []string{"Docker disk", "61% used · prunes above 80%", "reclaimable", "Images", "6.6 GB", "5.6 GB",
		"Local Volumes", "8.7 GB", "Build Cache", "source.local", "86.0 MB",
		"auto · 13:07 · ok — build cache older than 72h 1.2 GB, dangling images 300.0 MB",
		"[ Prune ]", "( Build cache to 20GB )", "[ All build cache ]", "( Dangling images )", "[ Unused volumes ]",
		"Unused volumes: refused while 1 jobs run"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	narrow := &fakeClient{}
	if v := withConfig(onStorage(t, narrow, 80, 60), narrow).View(); strings.Contains(v, "reclaimable") {
		t.Fatalf("the reclaimable column shows at 80 columns:\n%s", v)
	}
	st := sampleStorage()
	failed := now.Add(-5 * time.Minute)
	st.LastPrune = &model.LastPrune{Trigger: "manual", Scope: "build-cache-all", StartedAt: failed, FinishedAt: &failed,
		Outcome: "errors", Steps: []model.PruneStep{{Name: "all build cache", Error: "docker: daemon busy"}}}
	m = feed(m, storageMsg{seq: m.store.seq, s: st})
	if v := m.View(); !strings.Contains(v, "manual build-cache-all · 14:00 · errors — all build cache: docker: daemon busy") {
		t.Fatalf("failed prune line:\n%s", v)
	}
	st.LastPrune = nil
	if v := feed(m, storageMsg{seq: m.store.seq, s: st}).View(); !strings.Contains(v, "no prune since start") {
		t.Fatalf("no prune:\n%s", v)
	}
}

func TestStoragePruneButtonsConfirmFirst(t *testing.T) {
	c := &fakeClient{}
	m := withConfig(onStorage(t, c, 120, 60), c)
	for _, id := range []string{storePrune, storeKeep, storeAll, storeDangling, storeVolumes} {
		if m = click(t, m, id); m.overlay != ovConfirm {
			t.Fatalf("%s: no confirmation", id)
		}
		m = feed(m, key("n"))
	}
	if len(c.actions()) != 0 {
		t.Fatalf("a declined prune ran: %v", c.actions())
	}
	if m = click(t, m, storeAll); !strings.Contains(m.confirmText, "Remove all build cache (up to 456.0 MB)?") {
		t.Fatalf("confirmation %q", m.confirmText)
	}
	m = feed(m, key("y"))
	if !strings.Contains(m.View(), "build-cache-all prune started") {
		t.Fatalf("toast:\n%s", m.View())
	}
	if m = click(t, m, storeKeep); !strings.Contains(m.confirmText, "Prune the build cache down to 20GB?") {
		t.Fatalf("confirmation %q", m.confirmText)
	}
	m = feed(m, key("y"))
	m = feed(click(t, m, storePrune), key("y"))
	if got := strings.Join(c.actions(), "|"); got != "prune build-cache-all|prune build-cache-keep|prune" {
		t.Fatalf("actions %q", got)
	}
	c.storeErr = &api.Error{Status: 409, Msg: "refused: 1 jobs running"}
	if m = click(t, m, storeVolumes); !strings.Contains(m.confirmText, "Remove every volume no container uses (up to 8.7 GB)?") {
		t.Fatalf("confirmation %q", m.confirmText)
	}
	if m = feed(m, key("y")); !m.toast.Err || !strings.Contains(m.toast.Text, "refused: 1 jobs running") {
		t.Fatalf("toast %q err=%v", m.toast.Text, m.toast.Err)
	}
	st := sampleStatus()
	st.Maintenance.Running = true
	m = feed(m, statusMsg{st: st})
	if v := m.View(); !strings.Contains(v, "pruning…") || m.store.prune.Focusable() || m.store.volumes.Focusable() {
		t.Fatalf("buttons stay enabled while a prune runs:\n%s", v)
	}
}
```

Also add `"time"` to the imports of `internal/tui/storage_test.go`.

- [ ] **Step 2: Run them to verify they fail**

Run: `timeout 300 go test ./internal/tui/ -run 'TestStorageDocker|TestStoragePrune'`
Expected: FAIL — the page has no Docker disk card (`missing "Docker disk"`) and `click` cannot find the `storage/prune` zone.

- [ ] **Step 3: Add the Docker card**

In `internal/tui/storage.go`, add `"context"` and `"fmt"` to the standard-library imports. In `syncStorage`, replace the line `var ws []ui.Widget` with `ws := m.dockerWidgets()`. In `storageLines`, replace the line `add(m.opsCard(w), nil)` with:

```go
	add(m.dockerCard(w))
	add(m.opsCard(w), nil)
```

Replace the whole `storagePressed` function with:

```go
// storagePressed runs a Storage page button; ok is false for any other ID.
func (m Model) storagePressed(id string) (tea.Model, tea.Cmd, bool) {
	var mm tea.Model
	var cmd tea.Cmd
	switch {
	case id == storePrune, id == storeKeep, id == storeAll, id == storeDangling, id == storeVolumes:
		mm, cmd = m.confirmPrune(id)
	default:
		return m, nil, false
	}
	return mm, cmd, true
}
```

Append to `internal/tui/storage.go`:

```go
// busyJobs counts the runners running a job: removals, clears and the
// unused-volumes prune are refused while any is.
func (m Model) busyJobs() int {
	n := 0
	for _, i := range m.st.Instances {
		if i.State == "busy" {
			n++
		}
	}
	return n
}

// refusedHint is the line an action refused while jobs run shows then.
func (m Model) refusedHint(what string) string {
	return sAmber.Render(fmt.Sprintf("%s: refused while %d jobs run", what, m.busyJobs()))
}

// dockerWidgets are the Docker card's prune buttons, disabled while the
// daemon is unreachable or a prune runs.
func (m Model) dockerWidgets() []ui.Widget {
	s := m.store
	keep := "keep"
	if m.cfg != nil {
		keep = clean(m.cfg.BuildCacheKeep)
	}
	s.keep.Label = "Build cache to " + keep
	var ws []ui.Widget
	for _, b := range []*ui.Button{s.prune, s.keep, s.all, s.dangling, s.volumes} {
		b.SetDisabled(!m.connected || m.st.Maintenance.Running)
		ws = append(ws, b)
	}
	return ws
}

// dockerCard renders the Docker disk card w columns wide: the disk bar
// against disk_high_water, Docker's disk table with a row per build cache
// type, the last prune and the prune buttons.
func (m Model) dockerCard(w int) ([]string, map[string]ui.Range) {
	s, d := m.store, m.store.data.Docker
	f, in, c := s.group.FocusedID(), w-4, newStoreCard()
	high := 80
	if m.cfg != nil {
		high = m.cfg.DiskHighWater
	}
	c.add(ui.Gauge(d.DiskPct, 100, 20) + fmt.Sprintf(" %d%% used · prunes above %d%%", d.DiskPct, high))
	full := m.width >= wideMin
	row := func(typ, count, active string, size, reclaimable int64) string {
		line := cell(typ, 16) + cell(count, 7) + cell(active, 8) + cell(model.HumanBytes(size), 11)
		if full {
			line += model.HumanBytes(reclaimable)
		}
		return line
	}
	head := cell("", 16) + cell("count", 7) + cell("active", 8) + cell("size", 11)
	if full {
		head += "reclaimable"
	}
	c.add(sDim.Render(head))
	for _, r := range d.Rows {
		c.add(row(r.Type, fmt.Sprint(r.Count), fmt.Sprint(r.Active), r.Bytes, r.Reclaimable))
		if r.Type == "Build Cache" {
			for _, t := range d.BuildCacheTypes {
				c.add(sDim.Render(row("  "+t.Type, fmt.Sprint(t.Count), "", t.Bytes, t.Reclaimable)))
			}
		}
	}
	c.add(m.lastPruneLines(in)...)
	c.buttons(f, in, s.prune, s.keep, s.all, s.dangling, s.volumes)
	if m.busyJobs() > 0 {
		c.add(m.refusedHint("Unused volumes"))
	}
	return c.box("Docker disk", w)
}

// lastPruneLines describe the newest prune in w columns, for example
// "auto · 14:05 · ok — build cache older than 72h 1.2 GB, dangling images
// 300.0 MB"; a failed step shows its error in red.
func (m Model) lastPruneLines(w int) []string {
	p := m.store.data.LastPrune
	switch {
	case m.st.Maintenance.Running || (p != nil && p.FinishedAt == nil):
		return []string{sAmber.Render("pruning…")}
	case p == nil:
		return []string{sDim.Render("no prune since start")}
	}
	head := p.Trigger
	if p.Trigger != "auto" {
		head += " " + p.Scope
	}
	line := head + " · " + p.FinishedAt.Local().Format("15:04") + " · " + p.Outcome
	var steps []string
	for _, st := range p.Steps {
		if st.Error != "" {
			steps = append(steps, sRed.Render(st.Name+": "+st.Error))
		} else {
			steps = append(steps, st.Name+" "+model.HumanBytes(st.Freed))
		}
	}
	if len(steps) > 0 {
		line += " — " + strings.Join(steps, ", ")
	}
	return ui.WrapWords(line, max(w, 1))
}

// confirmPrune asks before the prune a Docker card button names, saying
// what it removes.
func (m Model) confirmPrune(id string) (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	keep := "the build_cache_keep size"
	if m.cfg != nil {
		keep = clean(m.cfg.BuildCacheKeep)
	}
	d := m.store.data.Docker
	var scope, text string
	switch id {
	case storePrune:
		scope, text = "standard", "Prune now? Removes build cache beyond "+keep+", dangling images, and history and logs past retention."
	case storeKeep:
		scope, text = "build-cache-keep", "Prune the build cache down to "+keep+"?"
	case storeAll:
		scope, text = "build-cache-all", "Remove all build cache"+reclaim(d, "Build Cache")+"? The next builds start cold."
	case storeDangling:
		scope, text = "dangling-images", "Remove dangling images (untagged and used by no container)?"
	case storeVolumes:
		scope, text = "unused-volumes", "Remove every volume no container uses"+reclaim(d, "Local Volumes")+"? It is refused while jobs run."
	default:
		return m, nil
	}
	c := m.c
	return m.openConfirm(text, func() tea.Cmd {
		return m.action(scope+" prune started", func(cx context.Context) error {
			if scope == "standard" {
				return c.Prune(cx)
			}
			return c.PruneScope(cx, scope)
		})
	})
}

// reclaim is " (up to 1.2 GB)" for Docker's row typ, or "" when it frees nothing.
func reclaim(d model.DockerDisk, typ string) string {
	for _, r := range d.Rows {
		if r.Type == typ && r.Reclaimable > 0 {
			return " (up to " + model.HumanBytes(r.Reclaimable) + ")"
		}
	}
	return ""
}
```

- [ ] **Step 4: Run the tests**

Run: `timeout 300 go test ./internal/tui/...`
Expected: `ok` for both packages. The plain `Prune` button records `prune` (`Client.Prune`); the others record `prune <scope>`.

- [ ] **Step 5: Commit**

```bash
timeout 120 gofmt -l internal cmd; timeout 300 go vet ./...
timeout 60 git add internal/tui/storage.go internal/tui/storage_test.go
timeout 60 git commit -m "feat(tui): show Docker disk and prune scopes"
```

---

### Task 4: Toolchains and package caches cards

**Files:**
- Modify: `internal/tui/storage.go`
- Modify: `internal/tui/storage_test.go`

**Interfaces:**
- Consumes: C2, C3 (`busyJobs`, `refusedHint`).
- Produces: C4.

**Items:** 2, 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/storage_test.go`:

```go
func TestStorageToolchainsCard(t *testing.T) {
	m := onStorage(t, &fakeClient{}, 120, 80)
	v := m.View()
	for _, want := range []string{"Toolchains", "node    22.11.0", "182.4 MB", "installed 2026-10-01", "dotnet  10.0.100",
		"java    21.0.8+9", "PyPy", "98.0 MB", "other: a job's own setup step", "Remove: refused while 1 jobs run",
		"[ Install… ]", "( Install popular set )"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	st := sampleStorage()
	st.Operations.Current = &model.Operation{ID: "op3", Kind: "install", Target: "node 24.9.0", StartedAt: now, Progress: "extracting"}
	st.Operations.Queued = 2
	if v := feed(m, storageMsg{seq: m.store.seq, s: st}).View(); !strings.Contains(v, "installing node 24.9.0 — extracting (2 queued)") {
		t.Fatalf("queue line:\n%s", v)
	}
}

func TestStorageRemoveConfirmsWithTheSize(t *testing.T) {
	c := &fakeClient{}
	m := onStorage(t, c, 120, 80)
	m = click(t, m, storeRmPrefix+"dotnet/8.0.414")
	if !strings.Contains(m.confirmText, "Remove dotnet 8.0.414 (412.0 MB)?") ||
		!strings.Contains(m.confirmText, "also removes the 8.0 runtimes and packs") {
		t.Fatalf("confirmation %q", m.confirmText)
	}
	m = feed(m, key("y"))
	if m = click(t, m, storeRmPrefix+"node/22.11.0"); m.overlay != ovConfirm || strings.Contains(m.confirmText, "runtimes") {
		t.Fatalf("a node removal warns about .NET runtimes: %q", m.confirmText)
	}
	m = feed(m, key("y"))
	if got := strings.Join(c.actions(), "|"); got != "rm-toolchain dotnet 8.0.414|rm-toolchain node 22.11.0" {
		t.Fatalf("actions %q", got)
	}
	st := sampleStorage()
	st.Toolchains = append(st.Toolchains, model.Toolchain{Tool: "dotnet", Version: "8.0.120", Arch: "x64", Bytes: 400_000_000, InstalledAt: now})
	c.storage = &st
	m = feed(m, storageMsg{seq: m.store.seq, s: st})
	m = click(t, m, storeRmPrefix+"dotnet/8.0.414")
	if m.overlay != ovConfirm || !strings.Contains(m.confirmText, "Remove dotnet 8.0.414") || strings.Contains(m.confirmText, "runtimes") {
		t.Fatalf("one of two 8.0 SDKs: overlay %v, %q", m.overlay, m.confirmText)
	}
}

func TestStoragePopularSetListsTheNineEntries(t *testing.T) {
	c := &fakeClient{}
	m := click(t, onStorage(t, c, 120, 80), storePopular)
	if !strings.Contains(m.confirmText, "node 22, node 24, dotnet 8.0, dotnet 10.0, python 3.13, python 3.14, go latest, java 21, java 25") {
		t.Fatalf("confirmation %q", m.confirmText)
	}
	if m = feed(m, key("y")); strings.Join(c.actions(), "|") != "install-preset popular" || !strings.Contains(m.View(), "queued: popular set") {
		t.Fatalf("actions %v\n%s", c.actions(), m.View())
	}
}

func TestStoragePackageCachesCard(t *testing.T) {
	c := &fakeClient{}
	m := onStorage(t, c, 120, 80)
	v := m.View()
	for _, want := range []string{"Package caches", "NuGet", "/home/ghrunner/.nuget/packages", "1.2 GB", "18204 files",
		"written 2h ago", "pip         not present", "Clear: refused while 1 jobs run", "measured 14:02", "( Refresh )"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	if m.store.clear["pip"] != nil {
		t.Fatal("a cache that is not present has a Clear button")
	}
	if v := onStorage(t, &fakeClient{}, 80, 80).View(); strings.Contains(v, "18204 files") {
		t.Fatalf("the files column shows at 80 columns:\n%s", v)
	}
	if m = click(t, m, storeClearPrefix+"nuget"); m.confirmText != "Clear the NuGet cache (1.2 GB)? Jobs download what they need again." {
		t.Fatalf("confirmation %q", m.confirmText)
	}
	m = feed(m, key("y"))
	m = click(t, m, storeRefresh)
	if got := strings.Join(c.actions(), "|"); got != "clear nuget|refresh-storage" {
		t.Fatalf("actions %q", got)
	}
	st := sampleStorage()
	st.Measuring, st.MeasureError = true, "docker system df: Cannot connect to the Docker daemon"
	m = feed(m, storageMsg{seq: m.store.seq, s: st})
	if v := m.View(); !strings.Contains(v, "measuring…") || !strings.Contains(v, "✖ docker system df: Cannot connect") || m.store.refresh.Focusable() {
		t.Fatalf("measuring:\n%s", v)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `timeout 300 go test ./internal/tui/ -run 'TestStorageToolchains|TestStorageRemove|TestStoragePopular|TestStoragePackage'`
Expected: FAIL — `missing "Toolchains"`, and `click` cannot find the `storage/rm/…` zones.

- [ ] **Step 3: Add the two cards**

In `internal/tui/storage.go`, add `"github.com/darkraise/ghr/internal/toolchain"` to the module imports (between `internal/model` and `internal/tui/ui`). In `syncStorage`, replace the line `ws := m.dockerWidgets()` with:

```go
	ws := m.dockerWidgets()
	ws = append(ws, m.toolchainWidgets()...)
	ws = append(ws, m.cacheWidgets()...)
```

In `storageLines`, replace the line `add(m.dockerCard(w))` with:

```go
	add(m.dockerCard(w))
	add(m.toolchainsCard(w))
	add(m.cachesCard(w))
```

In `storagePressed`, directly before the line `default:`, add:

```go
	case id == storePopular:
		mm, cmd = m.confirmPopular()
	case id == storeRefresh:
		mm, cmd = m.refreshStorage()
	case strings.HasPrefix(id, storeRmPrefix):
		mm, cmd = m.confirmRemove(strings.TrimPrefix(id, storeRmPrefix))
	case strings.HasPrefix(id, storeClearPrefix):
		mm, cmd = m.confirmClear(strings.TrimPrefix(id, storeClearPrefix))
```

Append to `internal/tui/storage.go`:

```go
// toolchainKey names an installed version; its Remove button's ID ends with it.
func toolchainKey(t model.Toolchain) string { return t.Tool + "/" + t.Version }

// toolchainWidgets are one Remove per installed version, then Install… and
// Install popular set. Buttons of versions no longer installed are dropped.
func (m Model) toolchainWidgets() []ui.Widget {
	s := m.store
	var ws []ui.Widget
	live := map[string]bool{}
	for _, t := range s.data.Toolchains {
		k := toolchainKey(t)
		live[k] = true
		b := s.rm[k]
		if b == nil {
			b = ui.NewButton(storeRmPrefix+k, "Remove", ui.Danger)
			s.rm[k] = b
		}
		b.SetDisabled(!m.connected)
		ws = append(ws, b)
	}
	for k := range s.rm {
		if !live[k] {
			delete(s.rm, k)
		}
	}
	s.install.SetDisabled(!m.connected)
	s.popular.SetDisabled(!m.connected)
	return append(ws, s.install, s.popular)
}

// cacheWidgets are one Clear per present cache, then Refresh, which waits
// while a measurement runs.
func (m Model) cacheWidgets() []ui.Widget {
	s := m.store
	var ws []ui.Widget
	live := map[string]bool{}
	for _, pc := range s.data.PackageCaches {
		if !pc.Present {
			continue
		}
		live[pc.Name] = true
		b := s.clear[pc.Name]
		if b == nil {
			b = ui.NewButton(storeClearPrefix+pc.Name, "Clear", ui.Danger)
			s.clear[pc.Name] = b
		}
		b.SetDisabled(!m.connected)
		ws = append(ws, b)
	}
	for k := range s.clear {
		if !live[k] {
			delete(s.clear, k)
		}
	}
	s.refresh.SetDisabled(!m.connected || s.data.Measuring)
	return append(ws, s.refresh)
}

// toolchainsCard renders the Toolchains card w columns wide: a row per
// installed version with Remove, the folders no installer owns, what the
// queue is doing, and the install buttons.
func (m Model) toolchainsCard(w int) ([]string, map[string]ui.Range) {
	s := m.store
	f, in, c := s.group.FocusedID(), w-4, newStoreCard()
	if len(s.data.Toolchains) == 0 && len(s.data.OtherToolCache) == 0 {
		c.add(sDim.Render("the tool cache is empty"))
	}
	for _, t := range s.data.Toolchains {
		text := cell(t.Tool, 8) + cell(t.Version, 14) + cell(model.HumanBytes(t.Bytes), 11) +
			sDim.Render("installed "+t.InstalledAt.Local().Format("2006-01-02"))
		if b := s.rm[toolchainKey(t)]; b != nil {
			c.right(text, b, f, in)
		} else {
			c.add(text)
		}
	}
	for _, o := range s.data.OtherToolCache {
		c.add(cell(o.Name, 22) + cell(model.HumanBytes(o.Bytes), 11) + sDim.Render("other: a job's own setup step"))
	}
	if line := m.queueLine(); line != "" {
		c.add(line)
	}
	if m.busyJobs() > 0 {
		c.add(m.refusedHint("Remove"))
	}
	c.buttons(f, in, s.install, s.popular)
	return c.box("Toolchains", w)
}

// queueLine says what the operation queue is doing, for example
// "installing node 24.9.0 — extracting (2 queued)", or "" when it is idle.
func (m Model) queueLine() string {
	o := m.store.data.Operations
	if o.Current == nil {
		if o.Queued > 0 {
			return sAmber.Render(fmt.Sprintf("%d queued", o.Queued))
		}
		return ""
	}
	verb := map[string]string{"install": "installing", "remove": "removing", "clear": "clearing"}[o.Current.Kind]
	if verb == "" {
		verb = o.Current.Kind
	}
	line := verb + " " + o.Current.Target
	if o.Current.Progress != "" {
		line += " — " + o.Current.Progress
	}
	if o.Queued > 0 {
		line += fmt.Sprintf(" (%d queued)", o.Queued)
	}
	return sAmber.Render(line)
}

// cachesCard renders the Package caches card w columns wide: a row per
// cache with Clear, or "not present", and the measurement footer.
func (m Model) cachesCard(w int) ([]string, map[string]ui.Range) {
	s := m.store
	f, in, c := s.group.FocusedID(), w-4, newStoreCard()
	full := m.width >= wideMin
	for _, pc := range s.data.PackageCaches {
		if !pc.Present {
			c.add(cell(pc.Label, 12) + sDim.Render("not present"))
			continue
		}
		tail := cell(model.HumanBytes(pc.Bytes), 11)
		if full {
			tail += cell(fmt.Sprintf("%d files", pc.Files), 13)
		}
		last := "never written"
		if pc.LastWritten != nil {
			last = "written " + ago(m.now().Sub(*pc.LastWritten))
		}
		tail += cell(last, 18)
		b := s.clear[pc.Name]
		if b == nil {
			c.add(cell(pc.Label, 12) + tail)
			continue
		}
		pathW := max(in-12-ansi.StringWidth(tail)-ansi.StringWidth(b.View(false, 0))-1, 4)
		c.right(cell(pc.Label, 12)+sDim.Render(cell(strings.Join(pc.Paths, " "), pathW))+" "+tail, b, f, in)
	}
	if m.busyJobs() > 0 {
		c.add(m.refusedHint("Clear"))
	}
	status := sDim.Render("not measured yet")
	switch {
	case s.data.Measuring:
		status = sAmber.Render("measuring…")
	case s.data.MeasuredAt != nil:
		status = sDim.Render("measured " + s.data.MeasuredAt.Local().Format("15:04"))
	}
	c.right(status, s.refresh, f, ansi.StringWidth(status)+2+ansi.StringWidth(s.refresh.View(false, 0)))
	if s.data.MeasureError != "" {
		for _, l := range ui.WrapWords("✖ "+s.data.MeasureError, max(in, 1)) {
			c.add(sRed.Render(l))
		}
	}
	return c.box("Package caches", w)
}

// lastDotnetMajor is the SDK's major version when t is the only installed
// .NET SDK of that major, since removing it then also removes the major's
// runtimes and packs; otherwise "".
func (m Model) lastDotnetMajor(t model.Toolchain) string {
	if t.Tool != "dotnet" {
		return ""
	}
	major, _, _ := strings.Cut(t.Version, ".")
	for _, o := range m.store.data.Toolchains {
		if o.Tool == "dotnet" && o.Version != t.Version && strings.HasPrefix(o.Version, major+".") {
			return ""
		}
	}
	return major
}

// confirmRemove asks before removing the version k names (see toolchainKey).
func (m Model) confirmRemove(k string) (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	for _, t := range m.store.data.Toolchains {
		if toolchainKey(t) != k {
			continue
		}
		tool, version, c := t.Tool, t.Version, m.c
		text := fmt.Sprintf("Remove %s %s (%s)?", tool, version, model.HumanBytes(t.Bytes))
		if major := m.lastDotnetMajor(t); major != "" {
			text += fmt.Sprintf(" It is the last .NET %s SDK, so this also removes the %s.0 runtimes and packs.", major, major)
		}
		return m.openConfirm(text, func() tea.Cmd {
			return m.action("queued: remove "+tool+" "+version, func(cx context.Context) error {
				return c.RemoveToolchain(cx, tool, version)
			})
		})
	}
	return m, nil
}

// confirmClear asks before clearing the present cache name.
func (m Model) confirmClear(name string) (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	for _, pc := range m.store.data.PackageCaches {
		if pc.Name != name || !pc.Present {
			continue
		}
		label, c := pc.Label, m.c
		text := fmt.Sprintf("Clear the %s cache (%s)? Jobs download what they need again.", label, model.HumanBytes(pc.Bytes))
		return m.openConfirm(text, func() tea.Cmd {
			return m.action("queued: clear "+label, func(cx context.Context) error { return c.ClearCache(cx, name) })
		})
	}
	return m, nil
}

// confirmPopular asks before queueing the popular set, listing its entries.
func (m Model) confirmPopular() (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	var names []string
	for _, e := range toolchain.Popular {
		names = append(names, e.Tool+" "+e.Spec)
	}
	text := "Install the popular set? " + strings.Join(names, ", ") + ". Versions already installed are skipped."
	c := m.c
	return m.openConfirm(text, func() tea.Cmd {
		return m.action("queued: popular set", func(cx context.Context) error { return c.InstallPreset(cx, "popular") })
	})
}

// refreshStorage asks the daemon to measure now; a measurement already
// running answers with an error the toast shows.
func (m Model) refreshStorage() (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	c := m.c
	return m, m.action("measurement started", func(cx context.Context) error { return c.RefreshStorage(cx) })
}
```

- [ ] **Step 4: Run the tests**

Run: `timeout 300 go test ./internal/tui/...`
Expected: `ok` for both packages.

- [ ] **Step 5: Commit**

```bash
timeout 120 gofmt -l internal cmd; timeout 300 go vet ./...
timeout 60 git add internal/tui/storage.go internal/tui/storage_test.go
timeout 60 git commit -m "feat(tui): show toolchains and package caches"
```

---

### Task 5: Install dialog

**Files:**
- Create: `internal/tui/install.go`
- Create: `internal/tui/install_test.go`
- Modify: `internal/tui/model.go` (`overlay` constants, `Model`, `Update`)
- Modify: `internal/tui/input.go` (`handleKey`, `handleMouse`)
- Modify: `internal/tui/dialogs.go` (`buttonOverlay`, `pressed`, `withOverlay`)
- Modify: `internal/tui/storage.go` (`storagePressed`)

**Interfaces:**
- Consumes: C1 (`AvailableToolchains`, `InstallToolchain`), C2.
- Produces: C5.

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The edits to `model.go`, `input.go`, `dialogs.go` and `storage.go` are the routing entries every dialog has (compare `ovAddRepo`).

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/install_test.go`:

```go
package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/model"
)

func nodeChoices() map[string][]model.ToolchainChoice {
	return map[string][]model.ToolchainChoice{
		"node": {{Spec: "24.9.0", Version: "24.9.0"}, {Spec: "22.11.0", Version: "22.11.0", LTS: true},
			{Spec: "22.10.0", Version: "22.10.0", LTS: true}},
		"python": {{Spec: "3.13.7", Version: "3.13.7"}},
	}
}

func TestInstallDialogInstallsAPickedVersion(t *testing.T) {
	c := &fakeClient{choices: nodeChoices()}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	if m.overlay != ovInstall || strings.Join(c.availTools, ",") != "node" {
		t.Fatalf("overlay %v, fetched %v", m.overlay, c.availTools)
	}
	v := m.View()
	for _, want := range []string{"Install toolchain", "Node.js", "24.9.0", "22.11.0  [LTS]", "pick one below, or type a version"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
	m = feed(m, keys("tab", "2", "2", "enter")...) // the pick moves focus on to Cancel
	if v := m.View(); strings.Contains(v, "24.9.0") || !strings.Contains(v, "22.10.0") {
		t.Fatalf("filter 22:\n%s", v)
	}
	m = feed(m, keys("tab", "enter")...)
	if got := strings.Join(c.actions(), "|"); got != "install node 22.11.0" || m.overlay != ovNone {
		t.Fatalf("actions %q overlay %v", got, m.overlay)
	}
	if !strings.Contains(m.View(), "queued: install node 22.11.0") {
		t.Fatalf("toast:\n%s", m.View())
	}
}

func TestInstallDialogTakesATypedVersion(t *testing.T) {
	c := &fakeClient{choices: nodeChoices()}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	m = feed(m, keys("tab", "2", "4", "tab", "tab", "enter")...)
	if got := strings.Join(c.actions(), "|"); got != "install node 24" {
		t.Fatalf("actions %q", got)
	}
}

func TestInstallDialogFetchesTheChosenTool(t *testing.T) {
	c := &fakeClient{choices: nodeChoices()}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	m = feed(m, key("right"))
	if v := m.View(); strings.Join(c.availTools, ",") != "node,python" || !strings.Contains(v, "Python") || !strings.Contains(v, "3.13.7") {
		t.Fatalf("fetched %v:\n%s", c.availTools, v)
	}
	if m = feed(m, key("esc")); m.overlay != ovNone || m.inst != nil {
		t.Fatalf("esc left overlay %v", m.overlay)
	}
}

func TestInstallDialogErrorsStayInside(t *testing.T) {
	c := &fakeClient{choices: nodeChoices(), choicesErr: errors.New("go.dev: 503")}
	m := click(t, onStorage(t, c, 120, 40), storeInstall)
	if v := m.View(); !strings.Contains(v, "✖ go.dev: 503") || !strings.Contains(v, "( Retry )") {
		t.Fatalf("list error:\n%s", v)
	}
	c.choicesErr = nil
	m = click(t, m, instRetry)
	if v := m.View(); len(c.availTools) != 2 || !strings.Contains(v, "22.11.0") {
		t.Fatalf("retry fetched %v:\n%s", c.availTools, v)
	}
	c.installErr = errors.New("unknown tool")
	m.inst.group.Focus(instPick) // the retried list replaced Retry, so focus moved off it
	m = feed(m, keys("2", "2", "enter", "tab", "enter")...)
	if v := m.View(); m.overlay != ovInstall || !strings.Contains(v, "✖ unknown tool") {
		t.Fatalf("install error:\n%s", v)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `timeout 300 go test ./internal/tui/ -run TestInstallDialog`
Expected: a build failure naming `ovInstall`, `instRetry` or `m.inst`.

- [ ] **Step 3: Create `internal/tui/install.go`**

```go
package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	instTool   = "install/tool"
	instPick   = "install/version"
	instRetry  = "install/retry"
	instOK     = "install/ok"
	instCancel = "install/cancel"
)

// availTimeout bounds GET /toolchains/available: on a cold cache the daemon
// fetches the tool's release index first.
const availTimeout = 30 * time.Second

var toolOptions = []ui.Option{
	{Value: "node", Label: "Node.js"},
	{Value: "python", Label: "Python"},
	{Value: "go", Label: "Go"},
	{Value: "java", Label: "Java (Temurin)"},
	{Value: "dotnet", Label: ".NET SDK"},
}

// installDialog is the Install toolchain form: a tool, then a version picked
// from the ones the daemon offers or typed as is.
type installDialog struct {
	tool              *ui.Select
	picker            *ui.Picker
	ok, cancel, retry *ui.Button
	group             ui.Group
	shown             string // the tool whose versions the picker holds or is fetching
	choices           []model.ToolchainChoice
	loading           bool
	listErr           string // why the versions could not be fetched
	err               string // the daemon's rejection of the install
	busy              bool
}

// choicesMsg and installedMsg carry the dialog that sent the request, so a
// late reply never changes a dialog opened after it.
type (
	choicesMsg struct {
		d    *installDialog
		tool string
		cs   []model.ToolchainChoice
		err  error
	}
	installedMsg struct {
		d      *installDialog
		target string
		err    error
	}
)

func (m Model) openInstall() (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	d := &installDialog{
		tool:   ui.NewSelect(instTool, toolOptions),
		picker: ui.NewPicker(instPick, pickerRows),
		ok:     ui.NewButton(instOK, "Install", ui.Primary),
		cancel: ui.NewButton(instCancel, "Cancel", ui.Secondary),
		retry:  ui.NewButton(instRetry, "Retry", ui.Secondary),
	}
	m.inst, m.overlay = d, ovInstall
	return m, m.fetchChoices(d)
}

// fetchChoices loads the versions the selected tool offers into d.
func (m Model) fetchChoices(d *installDialog) tea.Cmd {
	tool := d.tool.Value().Text
	d.shown, d.loading, d.listErr, d.choices = tool, true, "", nil
	d.picker.SetOptions(nil)
	d.sync(m.connected)
	c := m.c
	return func() tea.Msg {
		cx, cancel := context.WithTimeout(context.Background(), availTimeout)
		defer cancel()
		cs, err := c.AvailableToolchains(cx, tool)
		return choicesMsg{d, tool, cs, err}
	}
}

func (m Model) gotChoices(msg choicesMsg) (tea.Model, tea.Cmd) {
	d := m.inst
	if m.overlay != ovInstall || d != msg.d || msg.tool != d.shown {
		return m, nil
	}
	d.loading = false
	if msg.err != nil {
		d.listErr = errText(msg.err)
	} else {
		d.choices = msg.cs
		opts := make([]ui.PickOption, len(msg.cs))
		for i, c := range msg.cs {
			opts[i] = ui.PickOption{Label: clean(c.Version)}
			if c.LTS {
				opts[i].Badge = ui.Badge("lts", ui.BadgeOK)
			}
		}
		d.picker.SetOptions(opts)
	}
	d.sync(m.connected)
	return m, nil
}

func (m Model) retryChoices() (tea.Model, tea.Cmd) {
	d := m.inst
	if d == nil || d.loading || m.offline() {
		return m, nil
	}
	return m, m.fetchChoices(d)
}

// target is the version Install sends: the picked choice's spec, else the
// filter as typed, so a partial version such as 22 installs as is.
func (d *installDialog) target() string {
	if o, ok := d.picker.Picked(); ok {
		for _, c := range d.choices {
			if clean(c.Version) == o.Label {
				return c.Spec
			}
		}
	}
	return strings.TrimSpace(d.picker.Filter())
}

// sync updates Install (disabled until there is a version, while a request
// is in flight or while the daemon is unreachable) and the focus order, so
// focus never rests on a disabled control.
func (d *installDialog) sync(connected bool) {
	d.ok.Label = "Install"
	if d.busy {
		d.ok.Label = "Queuing…"
	}
	d.ok.SetDisabled(d.busy || !connected || d.target() == "")
	d.retry.SetDisabled(!connected)
	d.tool.SetDisabled(d.busy)
	ws := []ui.Widget{d.tool}
	switch {
	case d.listErr != "":
		ws = append(ws, d.retry)
	case !d.loading:
		ws = append(ws, d.picker)
	}
	d.group.Set(append(ws, d.cancel, d.ok))
}

// installKey routes a key in the Install dialog: enter on a button presses
// it, then the focused control, then esc cancels, tab and the arrows move
// focus, and enter installs. A new tool fetches its versions.
func (m Model) installKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.inst
	d.sync(m.connected)
	if _, onButton := d.group.Focused().(*ui.Button); onButton && k.String() == "enter" {
		return m.pressed(d.group.FocusedID())
	}
	before := d.tool.Value().Text
	if ok, cmd := d.group.Key(k); ok {
		if d.tool.Value().Text != before {
			return m, tea.Batch(cmd, m.fetchChoices(d))
		}
		return m, cmd
	}
	switch k.String() {
	case "esc":
		return m.pressed(instCancel)
	case "tab", "down":
		d.group.Next()
	case "shift+tab", "up":
		d.group.Prev()
	case "enter":
		return m.pressed(instOK)
	}
	return m, nil
}

// installMouse handles the wheel over the version list and clicks in the dialog.
func (m Model) installMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	d := m.inst
	d.sync(m.connected)
	wheel := msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown)
	if wheel && d.picker.Hit(msg) {
		d.picker.Update(msg)
		return m, nil
	}
	before := d.tool.Value().Text
	_, cmd := d.group.Mouse(msg)
	if d.tool.Value().Text != before {
		return m, tea.Batch(cmd, m.fetchChoices(d))
	}
	return m, cmd
}

func (m Model) submitInstall() (tea.Model, tea.Cmd) {
	d := m.inst
	if d == nil || d.busy {
		return m, nil
	}
	if !m.connected {
		d.err = "the daemon is unreachable"
		return m, nil
	}
	spec := d.target()
	if spec == "" {
		d.err = "pick or type a version first"
		return m, nil
	}
	tool := d.tool.Value().Text
	d.busy, d.err = true, ""
	d.sync(m.connected)
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return installedMsg{d, tool + " " + spec, c.InstallToolchain(cx, tool, spec)}
	}
}

// installed closes the dialog once the install is queued. A rejection stays
// inside the open dialog.
func (m Model) installed(msg installedMsg) (tea.Model, tea.Cmd) {
	mine := m.overlay == ovInstall && m.inst == msg.d
	if msg.err != nil {
		if mine {
			m.inst.busy, m.inst.err = false, clean(msg.err.Error())
			m.inst.sync(m.connected)
			return m, nil
		}
		m.toast.Show(clean(msg.err.Error()), true, m.now())
		return m, nil
	}
	if mine {
		m.overlay, m.inst = ovNone, nil
	}
	m.toast.Show("queued: install "+msg.target, false, m.now())
	return m, m.fetchStorage()
}

// installView renders the dialog to fit a screen w columns wide, giving up
// version rows until it fits the height.
func (m Model) installView(w int) string {
	d := m.inst
	d.sync(m.connected)
	rw := max(min(72, w-14), 26)
	cursor, top := d.picker.Window()
	render := func(rows int) string {
		d.picker.Rows = rows
		d.picker.SetWindow(cursor, top)
		return m.installModal(w, rw)
	}
	rows := pickerRows
	view := render(rows)
	for rows > 1 && lipgloss.Height(view) > m.height {
		rows--
		view = render(rows)
	}
	return view
}

func (m Model) installModal(w, rw int) string {
	d := m.inst
	f := d.group.FocusedID()
	version := sDim.Render("pick one below, or type a version")
	if t := d.target(); t != "" {
		version = sBold.Render(t)
	}
	rows := []ui.Row{{Label: "Tool", Items: []ui.Widget{d.tool}}, {Label: "Version", Text: version}}
	switch {
	case d.loading:
		rows = append(rows, ui.Row{Text: sDim.Render("loading versions…")})
	case d.listErr != "":
		rows = append(rows, ui.Row{Lines: []string{sRed.Render("✖ " + d.listErr)}}, ui.Row{Items: []ui.Widget{d.retry}})
	default:
		rows = append(rows, ui.Row{Items: []ui.Widget{d.picker}})
	}
	lines, _ := ui.Render([]ui.Section{{Rows: rows}}, f, rw, false)
	body := strings.Join(lines, "\n")
	if d.err != "" {
		body += "\n" + lipgloss.NewStyle().Width(rw).Render(sRed.Render("✖ "+d.err))
	}
	return modal("Install toolchain", body, []*ui.Button{d.cancel, d.ok}, f, w)
}
```

- [ ] **Step 4: Route the dialog**

`internal/tui/model.go`:

1. In the `overlay` constants, after `ovToken`, add the line `ovInstall`.
2. In the `Model` struct, directly after the line `tok           *tokenDialog   // the open Replace token dialog`, add `inst          *installDialog // the open Install toolchain dialog`.
3. In `Update`, directly before `case reloadMsg:`, add:

```go
	case choicesMsg:
		return m.gotChoices(msg)
	case installedMsg:
		return m.installed(msg)
```

`internal/tui/input.go`:

1. In `handleKey`'s `switch m.overlay`, directly after `case ovToken:` and its `return m.tokenKey(k)`, add:

```go
	case ovInstall:
		return m.installKey(k)
```

2. In `handleMouse`, directly before the block `if m.overlay == ovToken {`, add:

```go
	if m.overlay == ovInstall {
		return m.installMouse(msg)
	}
```

`internal/tui/dialogs.go`:

1. In `buttonOverlay`, directly after `case tokOK, tokCancel:` and its `return ovToken`, add:

```go
	case instOK, instCancel, instRetry:
		return ovInstall
```

2. In `pressed`, directly after `case addRetry:` and its `return m.retryAvailable()`, add:

```go
	case instCancel:
		m.overlay, m.inst = ovNone, nil
	case instOK:
		return m.submitInstall()
	case instRetry:
		return m.retryChoices()
```

3. In `withOverlay`, directly after `case ovToken:` and its `dialog = m.tokenView(w)`, add:

```go
	case ovInstall:
		dialog = m.installView(w)
```

`internal/tui/storage.go`: in `storagePressed`, directly before the line `case id == storePopular:`, add:

```go
	case id == storeInstall:
		mm, cmd = m.openInstall()
```

- [ ] **Step 5: Run the tests**

Run: `timeout 300 go test ./internal/tui/...`
Expected: `ok` for both packages.

- [ ] **Step 6: Commit**

```bash
timeout 120 gofmt -l internal cmd; timeout 300 go vet ./...
timeout 60 git add internal/tui
timeout 60 git commit -m "feat(tui): add the Install toolchain dialog"
```

---

### Task 6: Settings without disk and prune

**Files:**
- Modify: `internal/tui/manage.go` (`setPrune`, `manageState.prune`, `newManageState`, `startPrune`, `maintenanceSection`)
- Modify: `internal/tui/dialogs.go` (`pressed`)
- Modify: `internal/tui/manage_test.go` (`TestMaintenanceSection`)
- Modify: `internal/tui/settings_test.go` (`TestSettingsFocusOrderScrollsIntoView`)
- Regenerate: the Settings goldens

**Interfaces:**
- Consumes: none.
- Produces: none.

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

- [ ] **Step 1: Change the tests**

In `internal/tui/manage_test.go`, in `TestMaintenanceSection`, replace everything from the line `for _, want := range []string{"Maintenance", "61% used", "not pruned since the daemon started", "[ Reload config.yaml ]", "[ Prune now ]"} {` through the line `t.Fatalf("finished prune:\n%s", v)` and the `}` after it with:

```go
	for _, want := range []string{"Maintenance", "[ Reload config.yaml ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, gone := range []string{"Prune now", "% used", "pruned"} {
		if strings.Contains(v, gone) {
			t.Errorf("%q belongs to the Storage page now", gone)
		}
	}
	m = click(t, m, setReload)
	if got := strings.Join(c.actions(), "|"); got != "reload" || !strings.Contains(m.View(), "config reloaded (1 warning, see Activity)") {
		t.Fatalf("actions %q\n%s", got, m.View())
	}
```

(the `c.reloadErr = &api.Error{…}` lines that follow stay).

In `internal/tui/settings_test.go`, in `TestSettingsFocusOrderScrollsIntoView`, replace `if got := m.settings.group.FocusedID(); got != setPrune {` with `if got := m.settings.group.FocusedID(); got != setReload {`, and `if !strings.Contains(v, "› [ Prune now ]") || lipgloss.Height(v) > 22 || m.settings.scroll == 0 {` with `if !strings.Contains(v, "› [ Reload config.yaml ]") || lipgloss.Height(v) > 22 || m.settings.scroll == 0 {`.

- [ ] **Step 2: Run them to verify they fail**

Run: `timeout 300 go test ./internal/tui/ -run 'TestMaintenanceSection|TestSettingsFocusOrder'`
Expected: FAIL — `"Prune now" belongs to the Storage page now`, and the wrap lands on `settings/prune`.

- [ ] **Step 3: Remove disk and prune from Settings**

`internal/tui/manage.go`:

1. Delete the line `setPrune        = "settings/prune"` from the constants.
2. Delete the line `prune    *ui.Button` from `manageState`, and the line `prune:      ui.NewButton(setPrune, "Prune now", ui.Primary),` from `newManageState`.
3. Delete the whole `startPrune` function.
4. Replace the whole `maintenanceSection` function (with its comment) with:

```go
// maintenanceSection is the Settings card for the runner version and
// Reload; disk use and pruning are on the Storage page.
func (m Model) maintenanceSection() ui.Section {
	m.mg.reload.SetDisabled(!m.connected)
	rows := append(m.runnerRows(), ui.Row{Items: []ui.Widget{m.mg.reload}})
	return ui.Section{Title: "Maintenance", Rows: rows}
}
```

`internal/tui/dialogs.go`: in `pressed`, delete the two lines `case setPrune:` and `return m.startPrune()`.

Run `timeout 120 gofmt -w internal/tui/manage.go` (the constant and field blocks realign).

- [ ] **Step 4: Regenerate the Settings goldens and read the diff**

```bash
timeout 300 go test ./internal/tui/ -run 'TestSettings' -update
timeout 60 git diff -- internal/tui/testdata
```

Expected: only the Maintenance card changed — its `Disk` and `Prune` rows are gone and `[ Prune now ]` left the button row — and the lines below it moved up. If anything else changed, stop and report it.

- [ ] **Step 5: Run the whole package**

Run: `timeout 300 go test ./internal/tui/...`
Expected: `ok` for both packages.

- [ ] **Step 6: Commit**

```bash
timeout 120 gofmt -l internal cmd; timeout 300 go vet ./...
timeout 60 git add internal/tui
timeout 60 git commit -m "feat(tui): move disk and prune off Settings"
```

---

### Task 7: Storage goldens

**Files:**
- Modify: `internal/tui/storage_test.go`
- Create: `internal/tui/testdata/TestStorageGolden/<state>/<width>.golden` (generated)

**Interfaces:**
- Consumes: C2, C3, C4, C5.
- Produces: none.

**Items:** 3, 4, 5

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the golden and fit tests**

In `internal/tui/storage_test.go`, add `"fmt"` and `"github.com/charmbracelet/x/exp/golden"` to the imports, then append:

```go
// idleStorage opens the Storage page, w by 60, with no runner busy.
func idleStorage(t *testing.T, c *fakeClient, w int) Model {
	t.Helper()
	st := sampleStatus()
	st.Instances[0].State, st.Instances[0].Job = "idle", nil
	c.st = &st
	return withConfig(feed(newModel(c, w, 60, st), key("5")), c)
}

// storageStates builds the Storage page in each state a golden records.
var storageStates = map[string]func(t *testing.T, w int) Model{
	"idle": func(t *testing.T, w int) Model { return idleStorage(t, &fakeClient{}, w) },
	"running": func(t *testing.T, w int) Model {
		st := sampleStorage()
		st.Operations.Current = &model.Operation{ID: "op3", Kind: "install", Target: "node 24.9.0", StartedAt: now, Progress: "extracting"}
		st.Operations.Queued = 2
		return idleStorage(t, &fakeClient{storage: &st}, w)
	},
	"busy": func(t *testing.T, w int) Model {
		c := &fakeClient{}
		return withConfig(feed(newModel(c, w, 60, sampleStatus()), key("5")), c)
	},
	"install": func(t *testing.T, w int) Model {
		return click(t, idleStorage(t, &fakeClient{choices: nodeChoices()}, w), storeInstall)
	},
	"confirm": func(t *testing.T, w int) Model {
		return click(t, idleStorage(t, &fakeClient{}, w), storeRmPrefix+"dotnet/8.0.414")
	},
}

func TestStorageGolden(t *testing.T) {
	for name, build := range storageStates {
		for _, w := range []int{120, 80} {
			t.Run(fmt.Sprintf("%s/%d", name, w), func(t *testing.T) {
				golden.RequireEqual(t, []byte(build(t, w).View()))
			})
		}
	}
}

// Every Storage state fills its screen exactly, down to the 40-column minimum.
func TestStorageFitsNarrow(t *testing.T) {
	for name, build := range storageStates {
		for _, w := range []int{80, 56, 40} {
			fits(t, "storage "+name, build(t, w).View(), w, 60)
		}
	}
}
```

- [ ] **Step 2: Run them to verify the goldens are missing**

Run: `timeout 300 go test ./internal/tui/ -run 'TestStorageGolden|TestStorageFitsNarrow'`
Expected: `TestStorageFitsNarrow` passes; `TestStorageGolden` fails because each `testdata/TestStorageGolden/<state>/<width>.golden` file does not exist.

- [ ] **Step 3: Generate the goldens and read every one**

```bash
timeout 300 go test ./internal/tui/ -run TestStorageGolden -update
ls internal/tui/testdata/TestStorageGolden/*/
```

Expected: ten files, `120.golden` and `80.golden` under each of `busy`, `confirm`, `idle`, `install`, `running`. Open each one and check:

- `idle/120`: the sidebar shows `5 Storage` marked current; the Docker disk card has the gauge line, the table with a `reclaimable` column, the `regular` and `source.local` rows under `Build Cache`, the `auto · 13:07 · ok` line and the five prune buttons; the Toolchains card lists node, both dotnet versions and java with Remove, then PyPy; Package caches lists NuGet and npm with Clear, then `pip` `not present`, then `measured 14:02` with Refresh; no `refused while` hint anywhere.
- `idle/80`: the same cards without the `reclaimable` and `files` columns, and the tab row in place of the sidebar.
- `busy/*`: the three `refused while 1 jobs run` hints (Unused volumes, Remove, Clear).
- `running/*`: `installing node 24.9.0 — extracting (2 queued)` in the Toolchains card.
- `install/*`: the Install toolchain dialog with `Node.js`, the version list and `22.11.0  [LTS]`.
- `confirm/*`: the confirmation `Remove dotnet 8.0.414 (412.0 MB)? It is the last .NET 8 SDK, so this also removes the 8.0 runtimes and packs.`

If a golden shows anything else, fix the code it exposes before committing.

- [ ] **Step 4: Run the whole package**

Run: `timeout 300 go test ./internal/tui/...`
Expected: `ok` for both packages.

- [ ] **Step 5: Commit**

```bash
timeout 120 gofmt -l internal cmd; timeout 300 go vet ./...
timeout 60 git add internal/tui
timeout 60 git commit -m "test(tui): add Storage page goldens"
```

---

### Task 8: Release and LXC acceptance

**Files:**
- Modify: `docs/superpowers/registers/2026-10-06-ghr-toolchains-caches.md` (homelab repo)

**Interfaces:**
- Consumes: every earlier task.
- Produces: the release and the register's final states.

**Items:** 1, 2, 3, 4, 5

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 0 - spec 1 - coupling 2 - risk 3 = 6

The controlling session runs this task itself, not a dispatched implementer: Step 3 needs your human partner's approval, and Step 1 is the execution skill's final review.

Every SSH command runs from Git Bash with `timeout` and `-o BatchMode=yes` against `root@192.168.0.99` with `-i ~/.ssh/ghr_lxc`, through this helper (define it in each script):

```bash
L() { timeout "${T:-600}" ssh -i "$HOME/.ssh/ghr_lxc" -o BatchMode=yes root@192.168.0.99 "$@"; }
```

The daemon's API answers on the LXC at `curl -s --unix-socket /run/ghr/ghr.sock http://ghr/<path>`. Write every script with the Write tool into the session scratchpad and run it from there.

- [ ] **Step 1: Final review before releasing**

Run the whole-branch final review exactly as the execution skill's Final Review section describes (`reference/final-review.md`) over `master..feat/toolchains-tui`, apply its fix wave, and record its `Final review: clean …` line in the ledger. If it is not clean after one fix wave, stop and report the open findings.

- [ ] **Step 2: Pre-flight on the LXC**

```bash
L() { timeout "${T:-600}" ssh -i "$HOME/.ssh/ghr_lxc" -o BatchMode=yes root@192.168.0.99 "$@"; }
T=60 L 'ghr version; ghr status | head -1; docker system df; docker volume ls -qf dangling=true | wc -l; ls /var/lib/ghr/toolcache; ls -d /home/ghrunner/.nuget/packages 2>&1; du -sh /home/ghrunner/.nuget/packages 2>&1'
```

Expected: `v0.1.10`, `global 0/` (no runner alive; otherwise wait), the Docker table, the dangling-volume count (67 on 2026-10-06), the tool cache folders, and whether `/home/ghrunner/.nuget/packages` exists. Record all of it in the ledger. If `.nuget/packages` is missing, Step 7 creates a small one first.

- [ ] **Step 3: Ask for approval to release and run the acceptance**

Ask your human partner, in one message: "The Storage page work passes all tests and its final review on `feat/toolchains-tui` (commit `<sha>`). May I fast-forward `master` and push it (CI publishes v0.1.11, which also carries the unreleased web UI server and toolchains daemon work), copy `github-runner/setup.sh` and `README.md` to the LXC, deploy v0.1.11 there with `GHR_VERSION` pinned, and run the acceptance? It installs the popular toolchain set (about nine downloads, a few GB), adds a `toolchains.yml` workflow to `darkraise/ghr-e2e` and dispatches it, dispatches `single` twice, builds one throwaway image to create build cache and then prunes all build cache, clears the NuGet cache (refused once while a job runs, then for real), and prunes unused volumes, which deletes the anonymous volumes no container uses (67 volumes, 8.65 GB on 2026-10-06)." Wait for an explicit yes; without it, leave the register rows at `planned` and stop.

- [ ] **Step 4: Merge, push, wait for the release, deploy**

Write `release.sh` and run it with `bash release.sh 2>&1 | tee release.log`; it stops at the first failure and deploys only the release built from the approved commit:

```bash
set -euo pipefail
L() { timeout "${T:-600}" ssh -i "$HOME/.ssh/ghr_lxc" -o BatchMode=yes root@192.168.0.99 "$@"; }
cd /d/Repositories/Personal/ghr
timeout 60 git switch master
timeout 60 git merge --ff-only feat/toolchains-tui
SHA=$(timeout 30 git rev-parse HEAD); echo "sha=$SHA"
timeout 120 git push origin master
RUN=""
for i in $(seq 20); do
  RUN=$(timeout 30 gh run list --workflow ci --commit "$SHA" --limit 1 --json databaseId -q '.[0].databaseId')
  [ -n "$RUN" ] && break
  sleep 3
done
[ -n "$RUN" ] || { echo "no CI run for $SHA"; exit 1; }
echo "run=$RUN"
timeout 600 gh run watch "$RUN" --exit-status --interval 15 > /dev/null
echo "ci=ok"
TAG=""
for i in $(seq 20); do
  timeout 60 git fetch --tags origin
  TAG=$(timeout 30 git tag --points-at "$SHA" | grep '^v' | sort -V | tail -1 || true)
  [ -n "$TAG" ] && timeout 30 gh release view "$TAG" --json tagName > /dev/null 2>&1 && break
  TAG=""
  sleep 15
done
[ -n "$TAG" ] || { echo "no release tagged at $SHA"; exit 1; }
[ "$(timeout 30 git rev-list -n 1 "$TAG")" = "$SHA" ] || { echo "$TAG does not point at $SHA"; exit 1; }
echo "tag=$TAG"
timeout 60 scp -i "$HOME/.ssh/ghr_lxc" -o BatchMode=yes /d/Repositories/Personal/homelab/github-runner/setup.sh /d/Repositories/Personal/homelab/github-runner/README.md root@192.168.0.99:/root/github-runner/
T=590 L "cd /root/github-runner && GHR_VERSION=$TAG timeout 570 bash setup.sh > /root/setup-$TAG.log 2>&1; echo rc=\$?; systemctl is-active ghr; ghr version; grep -c 'toolchain' /root/setup-$TAG.log || true"
```

Expected: `ci=ok`, `tag=v0.1.11`, `rc=0`, `active`, `v0.1.11`. The existing LXC is an upgrade, so `setup.sh` queues no toolchains (`GHR_TOOLCHAINS` defaults to `none`). Any other ending means stop and report; if the script exited before the `L` line, nothing was deployed.

- [ ] **Step 5: Pre-install the popular set (row 2)**

```bash
L() { timeout "${T:-600}" ssh -i "$HOME/.ssh/ghr_lxc" -o BatchMode=yes root@192.168.0.99 "$@"; }
T=60 L 'ghr toolchain install --preset popular'
T=3700 L 'for i in $(seq 360); do s=$(curl -s --unix-socket /run/ghr/ghr.sock http://ghr/storage); echo "$s" | jq -e ".operations.current == null and .operations.queued == 0" > /dev/null && break; sleep 10; done
echo "$s" | jq -r ".operations.recent[] | [.kind, .target, .outcome, .message] | @tsv"
ghr toolchain list
for py in /var/lib/ghr/toolcache/Python/*/x64; do LD_LIBRARY_PATH=$py/lib ldd $py/lib/python3.*/lib-dynload/*.so | grep "not found" && echo "MISSING in $py"; done; echo ldd-done'
```

Expected: `queued — follow with: ghr storage`; nine `install` lines (node 22, node 24, dotnet 8.0, dotnet 10.0, python 3.13, python 3.14, go latest, java 21, java 25), each `ok` or `skipped`, none `failed` or `interrupted`; `ghr toolchain list` shows a node 22.x, node 24.x, dotnet 8.0.x, dotnet 10.0.x, python 3.13.x, python 3.14.x, go, java 21 and java 25 version (the job-written node 20.20.2, go 1.26.8 and the dotnet 10.0.3xx/4xx SDKs may also be listed); `ldd-done` with no `MISSING` line. A missing library is added to the `setup.sh` apt line (spec §9 row 2), reported, and the acceptance stops.

- [ ] **Step 6: Cached toolchains in a workflow (row 1)**

Write `toolchains.yml` in the scratchpad:

```yaml
name: toolchains
on: workflow_dispatch
jobs:
  cached:
    runs-on: [self-hosted, homelab]
    steps:
      - uses: actions/setup-node@v7
        with:
          node-version: 22
      - uses: actions/setup-python@v7
        with:
          python-version: '3.13'
      - run: python -c "import ssl, sqlite3, ctypes, lzma, bz2, zlib; print('stdlib ok')"
      - uses: actions/setup-dotnet@v6
        with:
          dotnet-version: 8.0.x
      - uses: actions/setup-go@v7
        with:
          go-version: stable
          cache: false
      - uses: actions/setup-java@v6
        with:
          distribution: temurin
          java-version: 21
      - run: node --version && python --version && dotnet --version && go version && java -version
```

Then write and run `row1.sh`:

```bash
set -euo pipefail
SHA=$(timeout 30 gh api repos/darkraise/ghr-e2e/contents/.github/workflows/toolchains.yml --jq .sha 2>/dev/null) || SHA=""
timeout 60 gh api -X PUT repos/darkraise/ghr-e2e/contents/.github/workflows/toolchains.yml \
  -f message="ci: add toolchain cache check" -f content="$(base64 -w0 toolchains.yml)" ${SHA:+-f sha="$SHA"} --jq .commit.sha
sleep 5
timeout 30 gh workflow run toolchains -R darkraise/ghr-e2e
sleep 15
RUN=$(timeout 30 gh run list -R darkraise/ghr-e2e --workflow toolchains --limit 1 --json databaseId -q '.[0].databaseId')
echo "run=$RUN"
timeout 1500 gh run watch "$RUN" -R darkraise/ghr-e2e --exit-status --interval 15 > /dev/null && echo "job=ok"
timeout 60 gh run view "$RUN" -R darkraise/ghr-e2e --log > toolchains.log
grep -E "Found in cache|stdlib ok|Successfully set up CPython|already installed|Resolved Java|tool-cache" toolchains.log | cut -c1-200
if grep -iE "attempting to download|downloading|not found in (the )?local cache|acquiring" toolchains.log | cut -c1-200; then echo "DOWNLOADED"; else echo "no downloads"; fi
```

Expected: `job=ok`, the `stdlib ok` line, `Found in cache` lines for node and go, and `no downloads`. A `DOWNLOADED` ending names the action that missed the cache: report it with the log lines printed and stop.

- [ ] **Step 7: Storage page, All build cache, refusals and clears (rows 3, 4, 5)**

Write `accept-a.sh` (it runs on the LXC through `L 'bash -s' < accept-a.sh`; write it with the Write tool so `\e` reaches the file as a backslash and `e`):

```bash
S=accept-$$
w() { timeout ${2:-15} bash -c "until tmux capture-pane -p -t $S | grep -qF -- \"$1\"; do sleep 0.3; done"; rc=$?; echo "found=$rc ($1)"; [ $rc -eq 0 ] || { tmux capture-pane -p -t $S; exit 1; }; }
tmux new-session -d -s $S -x 120 -y 40 "NO_COLOR=1 ghr" || { echo "tmux could not create $S"; exit 1; }
trap "tmux kill-session -t $S" EXIT
w "[ACTIVE]" 30
tmux send-keys -t $S 5; w "Docker disk"; w "Build Cache"
tmux capture-pane -p -t $S | grep -A3 -E "used · prunes above|Images|Local Volumes|Build Cache"
tmux capture-pane -p -t $S > /tmp/accept-pane.txt; grep -q "pruning…" /tmp/accept-pane.txt && { echo "a prune is running; wait and re-run"; exit 1; }
tmux send-keys -t $S Tab Tab Enter; w "Remove all build cache"; tmux send-keys -t $S Enter
w "build-cache-all prune started"; w "manual build-cache-all" 120
tmux send-keys -t $S NPage; w "Package caches"; w "NuGet"
tmux capture-pane -p -t $S | grep -E "NuGet|npm|measured"
echo step=ok
```

(Focus starts on Prune, the first enabled button; two tabs reach All build cache. While a prune runs the prune buttons are disabled, hence the check before the keys. `NO_COLOR=1` makes lipgloss pick the ASCII profile, so badges read `[ACTIVE]` as in the unit tests; the repositories-management acceptance relied on the same.)

Then write and run `row345.sh` from the scratchpad:

```bash
set -uo pipefail
L() { timeout "${T:-600}" ssh -i "$HOME/.ssh/ghr_lxc" -o BatchMode=yes root@192.168.0.99 "$@"; }
API='curl -s --unix-socket /run/ghr/ghr.sock http://ghr'
finish() { rc=$?; T=60 L "docker rmi -f ghr-accept-cache busybox:1.37 > /dev/null 2>&1; echo \"accept sessions: \$(tmux ls 2>/dev/null | grep -c accept-)\"; ghr status | head -1"; echo "acceptance exit=$rc"; }
trap finish EXIT
set -e
# Build cache to prune, and a NuGet cache to clear.
T=300 L "printf 'FROM busybox:1.37\nRUN date > /stamp\n' | docker build -q -t ghr-accept-cache - && runuser -u ghrunner -- sh -c 'mkdir -p ~/.nuget/packages/ghr.accept && date > ~/.nuget/packages/ghr.accept/stamp' && ghr storage refresh"
T=120 L "for i in \$(seq 60); do $API/storage | jq -e '.measuring == false and (.docker.build_cache_types | length) > 0' > /dev/null && break; sleep 2; done; $API/storage | jq -c '{rows: .docker.rows, types: .docker.build_cache_types}'"
# Row 4: the Storage page and All build cache, through the TUI.
L 'bash -s' < accept-a.sh | tee a.log; grep -q step=ok a.log
T=60 L "$API/storage | jq -c '.last_prune | {trigger, scope, outcome, steps}'"
# Rows 3 and 5 while a job runs.
timeout 30 gh workflow run single -R darkraise/ghr-e2e
busy=0
for i in $(seq 60); do T=30 L "$API/status | jq -e '[.instances[] | select(.state == \"busy\")] | length > 0'" > /dev/null && { busy=1; break; }; sleep 3; done
[ "$busy" = 1 ] || { echo "no busy runner seen: nothing was cleared or pruned"; exit 1; }
T=60 L "$API/status | jq -c '[.instances[] | {id, state}]'; ghr cache clear nuget; ghr prune --scope unused-volumes; echo prune-rc=\$?"
T=120 L "for i in \$(seq 60); do $API/storage | jq -e '.operations.current == null and .operations.queued == 0' > /dev/null && break; sleep 2; done; $API/storage | jq -r '.operations.recent[0] | [.kind, .target, .outcome, .message] | @tsv'"
# Once no runner is busy, for real.
for i in $(seq 60); do T=30 L "$API/status | jq -e '[.instances[] | select(.state == \"busy\")] | length == 0'" > /dev/null && break; sleep 5; done
T=60 L "docker system df | grep 'Local Volumes'; ghr cache clear nuget"
T=120 L "for i in \$(seq 60); do $API/storage | jq -e '.operations.current == null and .operations.queued == 0' > /dev/null && break; sleep 2; done; $API/storage | jq -r '.operations.recent[0] | [.kind, .target, .outcome, .message] | @tsv'; ls -A /home/ghrunner/.nuget/packages | wc -l"
T=600 L "ghr prune --scope unused-volumes; for i in \$(seq 120); do $API/storage | jq -e '.last_prune.scope == \"unused-volumes\" and .last_prune.finished_at != null' > /dev/null && break; sleep 3; done; $API/storage | jq -c '.last_prune | {scope, outcome, steps}'; docker system df | grep 'Local Volumes'"
```

Run it: `bash row345.sh 2>&1 | tee row345.log`.

Expected, in order:
- the Docker rows and at least one build cache type after the throwaway build;
- `step=ok` from `accept-a.sh`, whose capture shows the disk line, the table, the build cache type rows indented under `Build Cache` (the types the jq line above printed), `manual build-cache-all`, then the NuGet and npm rows with sizes and `measured HH:MM`;
- `last_prune` with `"trigger":"manual"`, `"scope":"build-cache-all"`, `"outcome":"ok"` and a step whose `freed` is above 0;
- while the `single` job runs: an instance with `"state":"busy"` (this settles the busy-state assumption: the TUI's hint and the daemon's refusal agree), `queued — follow with: ghr storage` for the clear, the prune refused with `refused: 1 jobs running` and `prune-rc=` not 0, then the clear's operation `clear nuget refused refused: 1 jobs running`;
- once idle: `clear nuget ok cleared NuGet …` and `0` entries left in `.nuget/packages`;
- `last_prune` with `"scope":"unused-volumes"`, `"outcome":"ok"` and `freed` above 0, and a `Local Volumes` row whose count dropped from the one printed before;
- `accept sessions: 0`, a status line and `acceptance exit=0`.

If the script ends with `no busy runner seen`, nothing was cleared or pruned: re-run `row345.sh` whole (the build and the NuGet stamp are recreated, and All build cache is pruned again). If the clear while busy came back `ok` instead of `refused`, report it and stop before anything else runs by hand, since the refusal is what rows 3 and 5 check. Any other failure: report the log; the trap has removed the throwaway image and the tmux session.

- [ ] **Step 8: Record**

In `D:/Repositories/Personal/homelab`, with `R=D:/Repositories/Personal/darkraise-ai-plugins/plugins/dr-superpowers/scripts/register` and `F=docs/superpowers/registers/2026-10-06-ghr-toolchains-caches.md`, run for each row `ID` in `1 2 3 4 5`:

```bash
bash "$R" set "$F" ID done --note "ghr <TAG> on the LXC: <what the step's log showed for this row>"
```

then `bash "$R" check "$F"` (it must report no errors), and commit with `timeout 60 git add "$F" && timeout 60 git commit -m "docs(registers): record ghr <TAG> storage acceptance"`. From `D:/Repositories/Personal/ghr`, remove the worktree (`timeout 60 git worktree remove ../ghr-toolchains-tui`) and delete the merged branch (`timeout 60 git branch -d feat/toolchains-tui`).
