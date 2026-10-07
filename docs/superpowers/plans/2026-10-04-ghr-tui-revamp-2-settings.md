# ghr TUI Revamp, Plan 2: Widgets, Shell and Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the TUI's free-text Config tab with a Settings form of real controls (a choice list for mode, steppers, text fields, tag lists, toggles) that edits every setting except `owner`, inside a new admin-console shell (top bar, sidebar, toasts, shared dialogs), and release it as its own ghr version.

**Architecture:** A new package `internal/tui/ui` holds the colour tokens and six controls that share one `Control`/`Widget` contract, plus a focus group, form state (base, edit, dirty, merge), a card layout that reports each control's line range, and a single toast. `internal/tui` gains `shell.go` (top bar, alert and toast lines, sidebar or tab row, footer), `dialogs.go` (one modal style for Confirm, Help, unsaved changes and Add repository) and `settings.go` (the form, its key and mouse routing, save, refresh and leave guard). The Dashboard, Runners and History pages keep their content inside the new shell; Plan 3 revamps them.

**Tech Stack:** Go 1.26, Bubble Tea v1.3.10, lipgloss v1.1.0, bubbles v0.21.1 (`textinput`), bubblezone v1.0.0, `charmbracelet/x/exp/golden` snapshots. No new modules.

**Spec:** docs/superpowers/specs/2026-10-04-ghr-tui-revamp-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 23 tasks, 6 heavy (Tasks 11, 12, 15, 16, 17, 18 total 5) and delegated; the session implements the other 17 (highest total 4) and runs the whole-branch final review.

**Program:** docs/superpowers/specs/2026-10-04-ghr-tui-revamp-design.md — sub-project 2 of 3 — next: The remaining pages

**Plan review:** 2026-10-04 — dr-superpowers:judge-opus — executability 16 / coherence 17 / coverage 16 / assumptions 15 (round 2)

## Global Constraints

- All code tasks run in `D:/Repositories/Personal/ghr` (module `github.com/darkraise/ghr`, `go 1.26`, remote `https://github.com/darkraise/ghr.git`). Start from `master` at `f4ccc6d` (ghr v0.1.3, Plan 1 merged) on a feature branch `feat/settings-revamp`; never commit to `master` directly.
- Every push to ghr's `master` publishes a release. Do not push, merge to `master`, tag or deploy without your human partner's explicit approval (Task 23 asks).
- Run every shell command in Git Bash (the Bash tool), never PowerShell: commands use POSIX redirection and `$?`.
- Copy every code block verbatim, including comments. Create or edit files with the Write or Edit tools, never shell heredocs. A "replace" block occurs exactly once in its file; replace it whole.
- Line endings are LF (`.gitattributes`: `* text=auto eol=lf`).
- Every `go test` carries `-count=1 -timeout 180s`. `-race` is unavailable on this Windows machine (no cgo).
- Before each commit: `gofmt -l cmd internal` prints nothing and `go vet ./...` passes.
- Commits: `<type>(<scope>): <subject>`, type one of feat|fix|docs|style|refactor|test|chore|perf, the whole first line ≤ 50 characters, imperative, English. Use each task's commit command as written.
- Do not upgrade Bubble Tea, lipgloss, bubbles or bubblezone, and add no module.
- No change to the daemon API or the CLI's `ghr set` (Plan 1 delivered the patch API; spec §4 keeps the CLI as is). The one daemon-side change is Task 1's minimums in `Config.Validate`, which the owner ordered on 2026-10-04 when deferring it from Plan 1's review (floors chosen by the owner: 5s and 1d).
- Nothing is drawn over other content (spec, Decisions): dropdowns open inline and push content down, the toast has its own line, a modal replaces the whole screen. A zone ID never repeats within a frame.
- Text from the daemon or GitHub passes through `clean()` before it is rendered.
- Snapshot and view tests run under `termenv.Ascii`, so they assert glyphs (`›`, `●`, `✔`, `✖`), never colours.

## Contracts

**Patch API** (Plan 1, `internal/model/model.go`) — `model.ConfigPatch{Mode, GlobalMax, PollInterval, StartTimeout, IdleTimeout, HistoryRetention, DiskHighWater, BuildCacheKeep, Labels, RunnerLimits{MemoryMax, CPUQuota}, Repos map[string]RepoPatch{Max, Warm, Labels, CleanupNamePrefixes, Paused}}`, all pointers; nil means unchanged. A bad value returns a 400 whose message the client returns as the error text; `Config.Validate` joins several problems with `"; "`.

**Duration floors** (`internal/config/config.go`, Task 1) — `const MinPollInterval = 5 * time.Second` and `const MinHistoryRetention = 24 * time.Hour`. `Validate` adds `poll_interval must be >= 5s` and `history_retention must be >= 1d` for positive values below them. `PatchConfig` already reports the first bad duration in the order poll_interval, start_timeout, idle_timeout, history_retention; Task 1 pins it with a test.

**ui theme** (`internal/tui/ui/theme.go`, Task 2) — colours `ColGreen ColAmber ColRed ColDim ColAccent ColSelBg`; styles `Green Amber Red Dim Bold Accent Sel Banner Dialog`; `func Cell(s string, w int) string` (truncate with `…`, pad to w); `func Box(title string, w int, lines []string) string` (rounded card, title in the top border). `internal/tui/styles.go` keeps the short names `sGreen sAmber sRed sDim sBold sAccent sSel sBanner sDialog` as aliases and `cell`, `box` as wrappers, plus `stateStyle`, `eventStyle`, `spinnerFrames`, `gauge`.

**ui controls** (`internal/tui/ui`, Tasks 3-6):

```go
type Control interface {
	Update(msg tea.Msg) (Control, tea.Cmd)
	View(focused bool, width int) string
	Focusable() bool // false when disabled
}
type Widget interface {
	Control
	ID() string
	TakesKey(k tea.KeyMsg) bool // the focused widget consumes k
	Capturing() bool            // owns every click (an open dropdown)
	Hit(msg tea.MouseMsg) bool
	Blur()                      // focus left: stop editing, close a dropdown
	SetDisabled(disabled bool)
}
type Value struct { Text string; Num int; Set bool; List []string }
type Input interface { Widget; Value() Value; SetValue(v Value) }
type Pressed struct{ ID string } // sent by a button
type Advance struct{ ID string } // sent by enter in a text field
func ZoneID(parts ...string) string // joins with "/"
```

Unexported helpers in `control.go` that Tasks 4-7 use: `glyph(focused bool) string` (the two-column focus marker), `clicked(msg tea.MouseMsg) bool` (a left-button release), `printable(k tea.KeyMsg) bool` (runes without alt, or space), `editorKey(k tea.KeyMsg) bool` (a field being edited takes every key except `tab`, `shift+tab`, `up`, `down`, `pgup`, `pgdown`, `ctrl+s`, `ctrl+c`), `send(msg tea.Msg) tea.Cmd`.

Every control is a pointer that updates in place, and its `View` starts with `glyph`: `› ` when focused, two spaces otherwise. Constructors and zones: `NewButton(id, label string, kind ButtonKind) *Button` (`Primary`, `Secondary`, `Danger`; fields `Label`, `Kind`, `Disabled`; zone `id`; takes `enter` and `space`); `NewToggle(id string, on bool) *Toggle` (field `On`; zone `id`); `NewSelect(id string, opts []Option) *Select` with `Option{Value, Label, Desc string}`, `Selected() Option`, `Open() bool` (zones `id`, `id/opt-N`; closed it takes `←`/`→`/`enter`/`space`, open it takes every key but acts only on `↑`, `↓`, `enter`, `esc`); `NewStepper(id string, lo, hi, step int) *Stepper` with fields `Min Max Step Default DefaultText ZeroText Suffix Disabled` (zones `id`, `id/dec`, `id/inc`; `Value.Set == false` shows `Default` with ` (default)`); `NewTextField(id string, width int) *TextField` with fields `Width`, `Check func(string) error`, `Disabled`, and `Editing() bool`, `Err() error` (zone `id`; `enter` while editing sends `Advance`); `NewTagList(id string) *TagList` with `Adding() bool` (zones `id/tag-N`, `id/x-N`, `id/add`, `id/input`; `Value` includes the draft being typed; `esc`, blur or a focus change commit the draft; `enter` on an empty input closes it and sends `Advance`; tags wrap to the width).

**ui focus** (`internal/tui/ui/focus.go`, Task 7) — `type Group struct` with `Set(items []Widget)` (keeps focus by ID, else moves to the next focusable), `Items() []Widget`, `FocusedID() string`, `Focused() Widget`, `Focus(id string) bool`, `Next()`, `Prev()`, `Key(k tea.KeyMsg) (bool, tea.Cmd)`, `Mouse(msg tea.MouseMsg) (bool, tea.Cmd)`.

**ui toast** (`internal/tui/ui/toast.go`, Task 8) — `type Toast struct{ Text string; Err bool }` with `Show(text string, isErr bool, now time.Time)`, `Tick(now time.Time)`, `Close()`, `Active() bool`, `CloseZone() string` (`"toast/close"`), `View(width int) string`; `ToastOK = 4s`, `ToastError = 10s`.

**ui form** (`internal/tui/ui/form.go`, Task 9) — `type Kind` (`KindText`, `KindDuration`, `KindInt`, `KindList`); `func Equal(k Kind, a, b Value) bool`; `type Field struct{ Key string; Kind Kind; Base Value; Input Input }` with `Dirty() bool`; `type Spec struct{ Key string; Kind Kind; Base Value; New func() Input; Locked bool }`; `type Form struct` with `Fields() []*Field`, `Field(key string) *Field`, `Dirty() []*Field`, `Discard()`, `Reset(keys []string)`, `Merge(specs []Spec)`.

**ui layout** (`internal/tui/ui/layout.go`, Task 10) — `const LabelWidth = 18`; `type Row struct{ Label string; Items []Widget; Text, Desc, Err string; Dirty bool }`; `type Section struct{ Title, Note string; Rows []Row }` (untitled: no frame); `type Range struct{ Start, End int }`; `func Render(secs []Section, focused string, w int, wide bool) ([]string, map[string]Range)`; `func ScrollTo(off, h int, r Range) int` (one line of context).

**ui test helpers** (package `ui` tests) — `TestMain` (Ascii profile, global zone manager) in `theme_test.go`; in `control_test.go`: `key(k string) tea.KeyMsg`, `typeText(w Widget, s string)`, `render(view string) string`, `waitZone(t, id)`, `settle(t)`, `clickAt(t, view, id string) tea.MouseMsg`, `outside`, `msgOf(cmd tea.Cmd) tea.Msg`; `modeSelect()` in `select_test.go`.

**TUI pages** (`internal/tui`, Task 11) — `type page int` with `pageDashboard`, `pageRunners`, `pageHistory`, `pageSettings` and `pageNames = {"Dashboard", "Runners", "History", "Settings"}`; `Model.page`, `Model.toast ui.Toast`; `func (m Model) switchPage(p page) (tea.Model, tea.Cmd)`. `tab`/`shift+tab` move focus within a page.

**TUI test helpers** (`internal/tui/tui_test.go`) — existing `fakeClient`, `sampleStatus`, `newModel`, `sampleModel`, `key`, `run`, `collect`, `feed`, `keys`, `ticks`, `waitZone`, `leftClick`, `sampleConfig`; Task 11 adds `settle(t)`, `zoneOf(t, m, id) *zone.ZoneInfo` and changes `click(t, m, id)` to use them; Task 16 adds more names to `key`; Task 17 adds `fakeClient.patches`, `patchErr`, `onPatch`; Task 21 adds `fakeClient.addErr` and the `add NAME LABELS max=N public=B` record.

**TUI shell** (`internal/tui/shell.go`, Task 12) — `sidebarWidth = 16`, `wideMin = 100`; `navZone(p page) string` (`"nav/dashboard"` …), zones `nav/help`, `nav/quit`, `key-<key>`; methods `topBar(w)`, `chips(level int)`, `diskState() string` (`ok`, `warn`, `critical`), `alertLine(w)`, `sidebar(h)`, `tabRow(w)` (labels shrink to short names, then numbers, to fit), `pageHeader(w)`, `footerKeys() []footerKey`, `footer(w)`, `layout(w int, body func(w, h int) string) string`; `type footerKey struct{ key, label string }`; `keyMsg(k string) tea.KeyMsg`. Task 16 adds `contentSize() (int, int)` and `footerPress(k string)` (navigation and control keys go through `handleKey`, global letter keys straight to `press`), and the Settings footer comes from `settingsFooterKeys()`.

**TUI dialogs** (`internal/tui/dialogs.go`, Task 13) — `btnYes = "dialog/yes"`, `btnNo = "dialog/no"`, `btnClose = "dialog/close"`; `Model.dlg ui.Group`, `Model.dlgButtons []*ui.Button` (the last is the primary), `Model.dlgCancel string`; `openDialog(ov overlay, cancel string, buttons ...*ui.Button)`, `openConfirm(text string, action func() tea.Cmd)`, `openHelp()`, `dialogKey(k)` (`enter` presses the primary, `space` the focused button, `esc` cancel), `pressed(id string) (tea.Model, tea.Cmd)` (every `ui.Pressed` lands here), `modal(title, body string, buttons []*ui.Button, focused string, maxW int) string` (wraps a body wider than the screen), `withOverlay`.

**Settings page** (`internal/tui/settings.go`, Task 14 on) — keys `setMode = "settings/mode"`, `setGlobalMax`, `setPollInterval`, `setStartTimeout`, `setIdleTimeout`, `setDiskHighWater`, `setBuildCacheKeep`, `setHistoryRetention`, `setLabels`, `setMemoryMax`, `setCPUQuota` (each `"settings/<config.yaml name>"`); `repoKey(name, field string) string` → `"settings/repo/<name>/<field>"` for `max`, `warm`, `labels`, `cleanup`, and the action buttons `pause`, `remove` (Task 20); `setSave`, `setDiscard` (Task 17), `setAddRepo` (Task 22). `type settingsPage struct{ form ui.Form; group ui.Group; scroll int; repos []config.Repo; … }` held as `Model.settings *settingsPage`; `newSettingsPage()`, `cleanList(v []string) []string`, `settingsSpecs(c *config.Config) []ui.Spec` (config strings pass through `clean`), `(s) load(c) []string` (returns removed repos from Task 18), `(s) input(key) ui.Input`, `(m) settingsSections() []ui.Section`, `(m) settingsView(w, h int) string` (zone `settings/body`). Task 16: `settingsKey(k) (bool, tea.Model, tea.Cmd)`, `settingsMouse(msg) (bool, tea.Model, tea.Cmd)`, `advance(id)`, `settingsBodyH() int`, `scrollToFocus()`, `settingsFooterKeys() []footerKey`. Task 18: `configMsg` becomes `struct{ seq int; cfg *config.Config }`; `settingsPage.seq`, `settingsPage.shown`, `(s) nextSeq() int`; `(m *Model) loadConfig(seq int, c *config.Config)` (drops a response older than the config shown; scrolls a moved focus into view). Task 20: `(s) button(id, label, kind) *ui.Button`, `repoAction(id)`.

**Settings save** (Task 17) — `savedMsg{sent map[string]ui.Value; err error}`, `refetchedMsg{seq int; cfg *config.Config; sent map[string]ui.Value; err error}` (`seq` from Task 18); `settingsPage.saving` stays true from the patch until the refetch is handled; `(s) checkErrors() map[string]string`, `(s) buildPatch() (model.ConfigPatch, map[string]ui.Value)`, `fieldName(key) string`, `saveSettings()`, `saved(msg)`, `refetched(msg)`, `unsavedBar() string`, `alertBox(w) []string` (at most three messages plus `… and N more`).

**Leave guard** (Task 19) — overlay `ovUnsaved`; `type leaveTarget struct{ page page; quit bool }`; `Model.leaveTo`, `Model.leaving`; `leave(t leaveTarget)` (waits while a save is in flight), `goTo(t leaveTarget)`; buttons `btnLeaveSave = "unsaved/save"`, `btnLeaveDiscard = "unsaved/discard"`, `btnLeaveStay = "unsaved/stay"`. A save started from the dialog leaves only from `refetched`, after the mismatch check, and not when it finds a mismatch or the refetch fails. `l` goes through `leave`.

**Add repository dialog** (Task 21) — overlay `ovAddRepo`; `Model.add *addRepoDialog`; IDs `addName = "add/name"`, `addMax`, `addLabels`, `addPublic`, `addOK = "add/ok"`, `addCancel = "add/cancel"`; `addedMsg{d *addRepoDialog; name string; err error}` (only the dialog that sent it reacts); `openAddRepo()` (refuses while unreachable), `(d) sync(connected bool)`, `addRepoKey(k)`, `submitAddRepo()`, `added(msg)`, `addRepoView(w int)`.

**Settings test helpers** (`internal/tui/settings_test.go`) — `settingsYAML`, `parseConfig(t, y)`, `settingsModel(t, w, h)`, `settingsLines(m, w)`, `lineWith(lines, s)` (Task 14); `onSettings(t, c, w, h)`, `quits(cmd)` (Task 16); `set(m, key, v)` (Task 17); `dirtySettings(t, c)`, `pump(m, msgs...) (Model, bool)`, `applyPoll(c)` (Task 19). `fakeClient.cfgErr` (Task 17) makes `Config` fail.

## Assumptions (evidence)

- Every task's code was written and tested in a scratch copy of ghr at `f4ccc6d` on 2026-10-04. Each task's end state passed `go test ./...`, `go vet ./...` and `gofmt -l`, and a script that replays this plan's create and replace blocks from `f4ccc6d` reproduced those states exactly (the snapshot files excepted: the tasks generate them with `-update`).
- bubblezone v1.0.0 applies `Scan` results through a 200-slot buffered channel (`manager.go:43`) and drops zones from older frames per scan (`manager.go:205-225`), so a `Clear` can be undone by a late update from an older frame. The test helpers' `settle` waits for a sentinel zone first; without it a click test flaked once in the scratch runs. Markers nest because the scanner pairs them by ID (`scanner.go:56-87`); `Get` of an unknown ID returns nil and `InBounds` is nil-safe (`zoneinfo.go:22-37`).
- `ansi.Truncate` keeps escape sequences past the cut (`x/ansi@v0.11.5/truncate.go:86-90`), so `Cell` never orphans a zone marker.
- bubbles v0.21.1 `textinput` has `Prompt`, `CharLimit`, `Width`, `Cursor.SetMode(cursor.CursorStatic)`, `Focus`, `Blur`, `SetValue`, `CursorEnd` (`textinput/textinput.go:91-260`); its `Update` ignores keys while blurred.
- `golden.RequireEqual` rewrites snapshots under the `-update` flag (`x/exp/golden@v0.1.0/golden.go:16`).
- `TestRunServesTicksReloadsAndKeepsRunners` polled every 100ms; at `5s` it still passes in 0.07s because the daemon's first tick fires at once (`internal/daemon/run.go:180-186`, timer starts at 0). Measured 2026-10-04.
- `Validate` also runs when the daemon loads `config.yaml` (`internal/config/config.go:153`), so a config below a floor would stop the daemon after the upgrade. The LXC's `/etc/ghr/config.yaml` holds `poll_interval: 10s` and `history_retention: 30d` (read over SSH, 2026-10-04); Task 23 re-checks before deploying.
- The LXC runs ghr v0.1.3 with repos darkcloud, darkmem, darkagents and `ghr-e2e` (`max: 3`, no labels), and has `tmux` and `curl` (read over SSH, 2026-10-04). Adding `ghr-e2e` again returns `repo ghr-e2e is already configured` (`internal/daemon/backend.go:218`). The `ghr` unit reloads its config on `systemctl reload ghr` (`ExecReload=/bin/kill -HUP $MAINPID`, `github-runner/setup.sh:138`).
- ghr's `ci` workflow (`.github/workflows/ci.yml`) runs on pushes to `master` and creates the release with `--target "$GITHUB_SHA"`, so the release tag points at the pushed commit; the repo's other workflow, `e2e-waiting`, runs only on dispatch. `gh` 2.88.1 is installed here (2026-10-04) and supports `gh run list --workflow --commit` and `gh release list --json`.
- The LXC's `/etc/ghr/config.yaml` is already in the form the daemon writes (`config.Save` re-marshals the whole struct, `internal/config/config.go:169-193`): Plan 1's acceptance patched it through the daemon on 2026-10-04 (its Task 3 Step 5: `valid=204`, `restore=204`), and its lines 10-40 read with yaml.v3's four-space indent (read over SSH, 2026-10-04). So after Task 23's checks restore every value, the file is byte-identical to the pre-flight backup, which Step 9 checks with `diff`.
- The API client turns a non-2xx response into an error whose text is the daemon's `{"error"}` message (`internal/api/client.go:47-55`), and `Validate` joins problems with `"; "` (`internal/config/config.go:328-330`), so the rejection alert splits on `"; "`.
- The owner declined the Codex executor for this programme, so no task carries an `**Executor:**` line.
- Task 1 is not in the approved spec: the owner directed it on 2026-10-04 as the first task of Plan 2 (deferred from Plan 1's review) and chose the floors (5s, 1d).
- Interpretations, flagged for review: the first printable key in a text field replaces its value (like a web field selected on focus) and `enter` keeps it; a tag-list draft counts as part of the value and is committed by `esc`, `tab` or any focus change, so nothing typed is ever hidden; `enter` on a non-empty tag draft adds it and keeps the input open for the next tag (the spec's table says "enter adds"), and only `enter` on an empty input commits and moves focus, as spec §1 rule 3 says for fields; a confirmation still takes `y`/`n`. The one-line descriptions the spec did not give are this plan's wording.
- The real daemon's longest Add repository rejection is `<name> is public; self-hosted runners must only serve private repos (pass --allow-public to override)` (`internal/daemon/backend.go:228`); Task 21's test uses it at 80 columns.
- Out of this plan, in Plan 3: the Dashboard tiles and row buttons, the Runners page with its log preview, the runner detail page (the popup stays here), History filters, Help regrouping, the height-collapse rules, "Resume all" ignoring repos being removed, and snapshots of the other pages.

## Task index

1. Duration floors in Config.Validate
2. Colour tokens move to package ui
3. The ui control contract
4. Button and Toggle
5. Select and Stepper
6. TextField and TagList
7. Focus group
8. Toast
9. Form state
10. Form layout and scroll-into-view
11. Pages, toasts and tab focus
12. Admin console shell
13. Shared modal for Confirm and Help
14. Settings form view
15. Settings page replaces the Config tab
16. Settings keys, mouse and scrolling
17. Save and Discard
18. Config refresh and repo changes
19. Leave guard
20. Repo actions on Settings
21. Add repository dialog
22. Add repository button and Settings snapshots
23. Release and LXC acceptance

---

### Task 1: Duration floors in Config.Validate

**Files:**
- Modify: `internal/config/config.go` (constants after `ModeAll`; `Validate`, after the duration loop near lines 271-278)
- Test: `internal/config/config_test.go`
- Test: `internal/daemon/backend_test.go`
- Modify: `internal/daemon/run_test.go:152` (its 100ms poll is now below the floor)

**Interfaces:**
- Consumes: none
- Produces: **Duration floors** (see Contracts)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 2 = 3

- [ ] **Step 1: Write the failing tests**

In `internal/config/config_test.go`, replace:

```go
		"runner_limits.cpu_quota":                        func(c *Config) { c.RunnerLimits.CPUQuota = "2" },
	}
```

with:

```go
		"runner_limits.cpu_quota":                        func(c *Config) { c.RunnerLimits.CPUQuota = "2" },
		"poll_interval must be >= 5s":                    func(c *Config) { c.PollInterval = Duration(time.Millisecond) },
		"history_retention must be >= 1d":                func(c *Config) { c.HistoryRetention = Duration(time.Second) },
	}
```

In `internal/config/config_test.go`, replace:

```go
			t.Errorf("want error containing %q, got %v", want, err)
		}
	}
}
```

with:

```go
			t.Errorf("want error containing %q, got %v", want, err)
		}
	}
}

// The floors themselves are valid; only values below them are rejected.
func TestDurationFloorsAreInclusive(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c.PollInterval, c.HistoryRetention = Duration(MinPollInterval), Duration(MinHistoryRetention)
	if _, err := c.Validate(); err != nil {
		t.Fatalf("floor values rejected: %v", err)
	}
}
```

In `internal/daemon/backend_test.go`, replace:

```go
	zero, size, cpu, mem, dur := 0, "lots", "fast", "6 gigs", "soon"
	empty := []string{}
```

with:

```go
	zero, size, cpu, mem, dur := 0, "lots", "fast", "6 gigs", "soon"
	fast, brief := "1ms", "1s"
	empty := []string{}
```

In `internal/daemon/backend_test.go`, replace:

```go
		{"labels", model.ConfigPatch{Labels: &empty}, "needs at least one label"},
	}
```

with:

```go
		{"labels", model.ConfigPatch{Labels: &empty}, "needs at least one label"},
		{"poll floor", model.ConfigPatch{PollInterval: &fast}, "poll_interval must be >= 5s"},
		{"retention floor", model.ConfigPatch{HistoryRetention: &brief}, "history_retention must be >= 1d"},
	}
```

In `internal/daemon/backend_test.go`, replace:

```go
		})
	}
```

with:

```go
		})
	}
}

// With several bad durations, the first in the order poll_interval,
// start_timeout, idle_timeout, history_retention is the one reported.
func TestPatchConfigReportsFirstBadDuration(t *testing.T) {
	bad := "x"
	cases := []struct {
		p    model.ConfigPatch
		want string
	}{
		{model.ConfigPatch{PollInterval: &bad, StartTimeout: &bad, IdleTimeout: &bad, HistoryRetention: &bad}, "poll_interval: "},
		{model.ConfigPatch{StartTimeout: &bad, IdleTimeout: &bad, HistoryRetention: &bad}, "start_timeout: "},
		{model.ConfigPatch{IdleTimeout: &bad, HistoryRetention: &bad}, "idle_timeout: "},
		{model.ConfigPatch{HistoryRetention: &bad, StartTimeout: &bad}, "start_timeout: "},
	}
	b, _, _ := newBackend(t)
	for _, tc := range cases {
		// Repeated, because a map-ordered loop would pass a single run by luck.
		for i := 0; i < 20; i++ {
			err := b.PatchConfig(tc.p)
			if apiStatus(err) != 400 || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("err %v, want 400 starting %q", err, tc.want)
			}
		}
	}
```

In `internal/daemon/run_test.go`, replace:

```go
	cfg := strings.Replace(cfgYAML, "global_max: 2", "global_max: 2\npoll_interval: 100ms", 1)
```

with:

```go
	cfg := strings.Replace(cfgYAML, "global_max: 2", "global_max: 2\npoll_interval: 5s", 1)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/ ./internal/daemon/ -count=1 -timeout 180s`
Expected: `internal/config` fails to compile with `undefined: MinPollInterval` and `undefined: MinHistoryRetention`; in `internal/daemon` the `poll floor` and `retention floor` subtests of `TestPatchConfigRejectsInvalidSettings` fail (`want 400 containing "poll_interval must be >= 5s"`). `TestPatchConfigReportsFirstBadDuration` already passes: it pins the order Plan 1 introduced.

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, replace:

```go
	ModeAll   = "all"
)
```

with:

```go
	ModeAll   = "all"
)

// The floors below keep a one-field mistake from hammering the GitHub API
// (poll_interval) or wiping the job history (history_retention).
const (
	MinPollInterval     = 5 * time.Second
	MinHistoryRetention = 24 * time.Hour
)
```

In `internal/config/config.go`, replace:

```go
	}
	if c.DiskHighWater < 1 || c.DiskHighWater > 100 {
```

with:

```go
	}
	if c.PollInterval > 0 && c.PollInterval.D() < MinPollInterval {
		errs = append(errs, "poll_interval must be >= 5s")
	}
	if c.HistoryRetention > 0 && c.HistoryRetention.D() < MinHistoryRetention {
		errs = append(errs, "history_retention must be >= 1d")
	}
	if c.DiskHighWater < 1 || c.DiskHighWater > 100 {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/ ./internal/daemon/ -count=1 -timeout 180s -v -run 'TestValidateErrors|TestDurationFloorsAreInclusive|TestPatchConfigRejectsInvalidSettings|TestPatchConfigReportsFirstBadDuration|TestRunServesTicksReloadsAndKeepsRunners'`
Expected: PASS for all five tests, including the eleven subtests of `TestPatchConfigRejectsInvalidSettings`; `TestRunServesTicksReloadsAndKeepsRunners` still takes well under a second (its first tick is immediate).

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/daemon/backend_test.go internal/daemon/run_test.go
git commit -m "fix(config): floor poll_interval and retention"
```

### Task 2: Colour tokens move to package ui

**Files:**
- Create: `internal/tui/ui/theme.go`
- Test: `internal/tui/ui/theme_test.go`
- Modify: `internal/tui/styles.go` (whole file)

**Interfaces:**
- Consumes: none
- Produces: **ui theme** (see Contracts)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/ui/theme_test.go`:

```go
package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"github.com/muesli/termenv"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	zone.NewGlobal()
	os.Exit(m.Run())
}

func TestCellTruncatesAndPads(t *testing.T) {
	if got := Cell("abcdef", 4); got != "abc…" {
		t.Fatalf("truncate: %q", got)
	}
	if got := Cell("ab", 4); got != "ab  " {
		t.Fatalf("pad: %q", got)
	}
	if got := Cell("ab", 0); got != "" {
		t.Fatalf("zero width: %q", got)
	}
}

func TestBoxFrame(t *testing.T) {
	got := Box("Title", 14, []string{"one", "a line that is far too long"})
	want := strings.Join([]string{
		"╭─ Title ────╮",
		"│ one        │",
		"│ a line th… │",
		"╰────────────╯",
	}, "\n")
	if got != want {
		t.Fatalf("box:\n%s\nwant:\n%s", got, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: Cell` and `undefined: Box`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/ui/theme.go`:

```go
// Package ui holds the ghr TUI's colour tokens, its form controls (button,
// toggle, select, stepper, text field, tag list) and the focus, form, layout
// and toast helpers its pages are built from.
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	ColGreen  = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	ColAmber  = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	ColRed    = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	ColDim    = lipgloss.AdaptiveColor{Light: "#6e7781", Dark: "#7d8590"}
	ColAccent = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	ColSelBg  = lipgloss.AdaptiveColor{Light: "#ddf4ff", Dark: "#1f2a37"}

	Green  = lipgloss.NewStyle().Foreground(ColGreen)
	Amber  = lipgloss.NewStyle().Foreground(ColAmber)
	Red    = lipgloss.NewStyle().Foreground(ColRed)
	Dim    = lipgloss.NewStyle().Foreground(ColDim)
	Bold   = lipgloss.NewStyle().Bold(true)
	Accent = lipgloss.NewStyle().Foreground(ColAccent).Bold(true)
	Sel    = lipgloss.NewStyle().Background(ColSelBg).Bold(true)
	Banner = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(ColRed).Bold(true).Padding(0, 1)
	Dialog = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColAccent).Padding(1, 2)
)

// Cell truncates s to w columns and pads it to exactly w.
func Cell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// Box draws a rounded box of total width w with title in the top border.
func Box(title string, w int, lines []string) string {
	if w < 8 {
		w = 8
	}
	dashes := w - 5 - ansi.StringWidth(title)
	if dashes < 1 {
		title = ansi.Truncate(title, w-6, "…")
		dashes = w - 5 - ansi.StringWidth(title)
	}
	var b strings.Builder
	b.WriteString(Dim.Render("╭─ ") + Bold.Render(title) + Dim.Render(" "+strings.Repeat("─", dashes)+"╮"))
	for _, l := range lines {
		b.WriteString("\n" + Dim.Render("│") + " " + Cell(l, w-4) + " " + Dim.Render("│"))
	}
	b.WriteString("\n" + Dim.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return b.String()
}
```

Replace the whole of `internal/tui/styles.go` with:

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/tui/ui"
)

// The colour tokens live in package ui; these short names keep the page code readable.
var (
	sGreen  = ui.Green
	sAmber  = ui.Amber
	sRed    = ui.Red
	sDim    = ui.Dim
	sBold   = ui.Bold
	sAccent = ui.Accent
	sSel    = ui.Sel
	sBanner = ui.Banner
	sDialog = ui.Dialog
)

func cell(s string, w int) string { return ui.Cell(s, w) }

func box(title string, w int, lines []string) string { return ui.Box(title, w, lines) }

func stateStyle(state string) lipgloss.Style {
	switch state {
	case "busy", "active", "success", "running":
		return sGreen
	case "starting", "idle", "paused", "removing", "queued", "waiting", "cancelled", "warn":
		return sAmber
	case "failure", "error", "cleaning":
		return sRed
	}
	return sDim
}

func eventStyle(level string) (string, lipgloss.Style) {
	switch level {
	case "ok":
		return "✔", sGreen
	case "warn":
		return "⚠", sAmber
	case "error":
		return "✖", sRed
	}
	return "▶", sAccent
}

var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

func gauge(used, total, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}
	filled := used * width / total
	if filled > width {
		filled = width
	}
	return "▕" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "▏"
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -count=1 -timeout 180s`
Expected: both packages `ok`. `TestDashboardGolden` passes unchanged: the styles are the same values under new names.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/styles.go internal/tui/ui/theme.go internal/tui/ui/theme_test.go
git commit -m "refactor(tui): move colour tokens to package ui"
```

### Task 3: The ui control contract

**Files:**
- Create: `internal/tui/ui/control.go`
- Test: `internal/tui/ui/control_test.go`

**Interfaces:**
- Consumes: **ui theme**
- Produces: **ui controls** (`Control`, `Widget`, `Value`, `Input`, `Pressed`, `Advance`, `ZoneID`) and the **ui test helpers** (see Contracts)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/ui/control_test.go`:

```go
package ui

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// key builds the KeyMsg a terminal sends for k ("enter", "a", " ", "ctrl+s", …).
func key(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// typeText feeds each rune of s to w as a key.
func typeText(w Widget, s string) {
	for _, r := range s {
		w.Update(key(string(r)))
	}
}

// render scans view the way the TUI's View does, so its zones register.
func render(view string) string { return zone.Scan(view) }

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

var syncN int

// settle waits until every zone update already queued has been applied.
// bubblezone applies Scan results from a buffered channel, so without this a
// late update from an older frame can land after a Clear and hand back stale
// coordinates.
func settle(t *testing.T) {
	t.Helper()
	syncN++
	id := fmt.Sprintf("sync/%d", syncN)
	render(zone.Mark(id, "x"))
	waitZone(t, id)
}

// clickAt renders view, waits for zone id and returns a left click inside it.
func clickAt(t *testing.T, view, id string) tea.MouseMsg {
	t.Helper()
	settle(t)
	zone.Clear(id)
	render(view)
	z := waitZone(t, id)
	return tea.MouseMsg{X: z.StartX, Y: z.StartY, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
}

// outside is a click far from anything a test renders.
var outside = tea.MouseMsg{X: 500, Y: 500, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}

func msgOf(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestZoneID(t *testing.T) {
	for _, c := range []struct {
		parts []string
		want  string
	}{
		{[]string{"settings", "repo", "darkmem", "max"}, "settings/repo/darkmem/max"},
		{[]string{"settings", "mode", "opt-1"}, "settings/mode/opt-1"},
		{[]string{"toast"}, "toast"},
	} {
		if got := ZoneID(c.parts...); got != c.want {
			t.Errorf("ZoneID(%q) = %q, want %q", c.parts, got, c.want)
		}
	}
}

func TestEditorKeyLeavesFocusAndSaveKeys(t *testing.T) {
	for _, k := range []string{"tab", "shift+tab", "up", "down", "ctrl+s"} {
		if editorKey(key(k)) {
			t.Errorf("an edited field took %q", k)
		}
	}
	for _, k := range []string{"a", "q", "1", " ", "backspace", "left", "enter", "esc"} {
		if !editorKey(key(k)) {
			t.Errorf("an edited field let %q through", k)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: Widget`, `undefined: ZoneID` and `undefined: editorKey`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/ui/control.go`:

```go
package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Control is the contract every form control meets.
type Control interface {
	Update(msg tea.Msg) (Control, tea.Cmd)
	View(focused bool, width int) string
	Focusable() bool // false when disabled
}

// Widget is a Control as a page holds it: addressable by ID, focusable and
// hit-testable. The controls in this package are pointers that update in
// place, so Update returns the receiver.
type Widget interface {
	Control
	ID() string
	// TakesKey reports whether the focused widget consumes k. Keys it does not
	// take fall through to the page keys and then the global keys.
	TakesKey(k tea.KeyMsg) bool
	// Capturing is true while the widget owns every click (an open dropdown).
	Capturing() bool
	// Hit reports whether a mouse event falls on one of the widget's zones.
	Hit(msg tea.MouseMsg) bool
	// Blur is called when focus leaves the widget: stop editing, close a dropdown.
	Blur()
	SetDisabled(disabled bool)
}

// Value is a form field's value. Which members are used depends on the
// field's Kind (see form.go).
type Value struct {
	Text string
	Num  int
	Set  bool // a number field: false means unset, so the default applies
	List []string
}

// Input is a Widget that holds a form value.
type Input interface {
	Widget
	Value() Value
	SetValue(v Value)
}

// Pressed is sent when a button is activated.
type Pressed struct{ ID string }

// Advance asks the page to move focus past the widget (enter in a text field).
type Advance struct{ ID string }

// ZoneID joins ID parts with "/", for example ZoneID("settings", "mode", "opt-1").
func ZoneID(parts ...string) string { return strings.Join(parts, "/") }

// glyph is the two-column focus marker every control view starts with.
func glyph(focused bool) string {
	if focused {
		return Accent.Render("›") + " "
	}
	return "  "
}

// clicked reports a left-button release, the event the TUI treats as a click.
func clicked(msg tea.MouseMsg) bool {
	return msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft
}

// printable reports whether k is text a field should take.
func printable(k tea.KeyMsg) bool {
	return (k.Type == tea.KeyRunes && !k.Alt) || k.Type == tea.KeySpace
}

// editorKey reports whether a field that is being edited takes k: everything
// except the keys that move focus, scroll, save or quit.
func editorKey(k tea.KeyMsg) bool {
	switch k.String() {
	case "tab", "shift+tab", "up", "down", "pgup", "pgdown", "ctrl+s", "ctrl+c":
		return false
	}
	return true
}

func send(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s -v -run "TestZoneID|TestEditorKeyLeavesFocusAndSaveKeys"`
Expected: PASS for both tests.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/ui/control.go internal/tui/ui/control_test.go
git commit -m "feat(tui): add the ui control contract"
```

### Task 4: Button and Toggle

**Files:**
- Create: `internal/tui/ui/button.go`
- Create: `internal/tui/ui/toggle.go`
- Test: `internal/tui/ui/button_test.go`
- Test: `internal/tui/ui/toggle_test.go`

**Interfaces:**
- Consumes: **ui controls**, **ui test helpers**
- Produces: `Button`, `Toggle` (see Contracts, **ui controls**)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/ui/button_test.go`:

```go
package ui

import "testing"

func TestButtonPressAndDisabled(t *testing.T) {
	b := NewButton("t/save", "Save changes", Primary)
	if got := render(b.View(true, 40)); got != "› [ Save changes ]" {
		t.Fatalf("view %q", got)
	}
	if got := render(NewButton("t/x", "Discard", Secondary).View(false, 40)); got != "  ( Discard )" {
		t.Fatalf("secondary view %q", got)
	}
	for _, k := range []string{"enter", " "} {
		if _, cmd := b.Update(key(k)); msgOf(cmd) != (Pressed{"t/save"}) {
			t.Fatalf("%q did not press", k)
		}
	}
	if _, cmd := b.Update(clickAt(t, b.View(false, 40), "t/save")); msgOf(cmd) != (Pressed{"t/save"}) {
		t.Fatal("click did not press")
	}
	if _, cmd := b.Update(outside); cmd != nil {
		t.Fatal("a click elsewhere pressed")
	}
	b.SetDisabled(true)
	if _, cmd := b.Update(key("enter")); cmd != nil || b.Focusable() {
		t.Fatal("a disabled button pressed or stayed focusable")
	}
}
```

Create `internal/tui/ui/toggle_test.go`:

```go
package ui

import "testing"

func TestToggleFlips(t *testing.T) {
	tg := NewToggle("t/public", false)
	if got := render(tg.View(false, 40)); got != "  [━○] Off" {
		t.Fatalf("view %q", got)
	}
	tg.Update(key(" "))
	if !tg.On || render(tg.View(true, 40)) != "› [●━] On" {
		t.Fatalf("space: on %v", tg.On)
	}
	tg.Update(clickAt(t, tg.View(false, 40), "t/public"))
	if tg.On {
		t.Fatal("click did not flip")
	}
	tg.Update(key("enter"))
	if !tg.On {
		t.Fatal("enter did not flip")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: NewButton`, `undefined: Primary` and `undefined: NewToggle`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/ui/button.go`:

```go
package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

type ButtonKind int

const (
	Primary ButtonKind = iota
	Secondary
	Danger
)

// Button sends Pressed{ID} on enter, space or a click.
type Button struct {
	id       string
	Label    string
	Kind     ButtonKind
	Disabled bool
}

func NewButton(id, label string, kind ButtonKind) *Button {
	return &Button{id: id, Label: label, Kind: kind}
}

func (b *Button) ID() string                 { return b.id }
func (b *Button) Focusable() bool            { return !b.Disabled }
func (b *Button) Capturing() bool            { return false }
func (b *Button) Blur()                      {}
func (b *Button) SetDisabled(d bool)         { b.Disabled = d }
func (b *Button) Hit(msg tea.MouseMsg) bool  { return zone.Get(b.id).InBounds(msg) }
func (b *Button) TakesKey(k tea.KeyMsg) bool { return k.String() == "enter" || k.String() == " " }

func (b *Button) Update(msg tea.Msg) (Control, tea.Cmd) {
	if b.Disabled {
		return b, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if b.TakesKey(msg) {
			return b, send(Pressed{b.id})
		}
	case tea.MouseMsg:
		if clicked(msg) && b.Hit(msg) {
			return b, send(Pressed{b.id})
		}
	}
	return b, nil
}

// View renders [ Label ] (primary and danger) or ( Label ) (secondary).
func (b *Button) View(focused bool, _ int) string {
	text, style := "[ "+b.Label+" ]", Accent
	switch b.Kind {
	case Secondary:
		text, style = "( "+b.Label+" )", Bold
	case Danger:
		style = Red.Bold(true)
	}
	if b.Disabled {
		style = Dim
	}
	return glyph(focused) + zone.Mark(b.id, style.Render(text))
}
```

Create `internal/tui/ui/toggle.go`:

```go
package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// Toggle flips On with enter, space or a click.
type Toggle struct {
	id       string
	On       bool
	Disabled bool
}

func NewToggle(id string, on bool) *Toggle { return &Toggle{id: id, On: on} }

func (t *Toggle) ID() string                 { return t.id }
func (t *Toggle) Focusable() bool            { return !t.Disabled }
func (t *Toggle) Capturing() bool            { return false }
func (t *Toggle) Blur()                      {}
func (t *Toggle) SetDisabled(d bool)         { t.Disabled = d }
func (t *Toggle) Hit(msg tea.MouseMsg) bool  { return zone.Get(t.id).InBounds(msg) }
func (t *Toggle) TakesKey(k tea.KeyMsg) bool { return k.String() == "enter" || k.String() == " " }

func (t *Toggle) Update(msg tea.Msg) (Control, tea.Cmd) {
	if t.Disabled {
		return t, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if t.TakesKey(msg) {
			t.On = !t.On
		}
	case tea.MouseMsg:
		if clicked(msg) && t.Hit(msg) {
			t.On = !t.On
		}
	}
	return t, nil
}

// View renders [●━] On or [━○] Off.
func (t *Toggle) View(focused bool, _ int) string {
	text, style := "[━○] Off", Dim
	if t.On {
		text, style = "[●━] On", Green
	}
	if t.Disabled {
		style = Dim
	}
	return glyph(focused) + zone.Mark(t.id, style.Render(text))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s -v -run "TestButtonPressAndDisabled|TestToggleFlips"`
Expected: PASS for both tests.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/ui/button.go internal/tui/ui/button_test.go internal/tui/ui/toggle.go internal/tui/ui/toggle_test.go
git commit -m "feat(tui): add button and toggle controls"
```

### Task 5: Select and Stepper

**Files:**
- Create: `internal/tui/ui/select.go`
- Create: `internal/tui/ui/stepper.go`
- Test: `internal/tui/ui/select_test.go`
- Test: `internal/tui/ui/stepper_test.go`

**Interfaces:**
- Consumes: **ui controls**, **ui test helpers**
- Produces: `Select`, `Option`, `Stepper` (see Contracts, **ui controls**)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/ui/select_test.go`:

```go
package ui

import (
	"strings"
	"testing"
)

func modeSelect() *Select {
	s := NewSelect("t/mode", []Option{
		{Value: "queue", Label: "queue", Desc: "jobs first"},
		{Value: "all", Label: "all", Desc: "warm runners"},
	})
	s.SetValue(Value{Text: "queue"})
	return s
}

func TestSelectKeys(t *testing.T) {
	s := modeSelect()
	if got := render(s.View(true, 40)); got != "› [ queue ▾ ]" {
		t.Fatalf("closed view %q", got)
	}
	s.Update(key("right"))
	if s.Value().Text != "all" {
		t.Fatalf("right: %q", s.Value().Text)
	}
	s.Update(key("right")) // wraps
	if s.Value().Text != "queue" {
		t.Fatalf("right wrap: %q", s.Value().Text)
	}
	s.Update(key("left"))
	if s.Value().Text != "all" {
		t.Fatalf("left wrap: %q", s.Value().Text)
	}
	s.Update(key("enter"))
	if !s.Open() || !s.Capturing() {
		t.Fatal("enter did not open")
	}
	want := strings.Join([]string{"  [ all ▾ ]", "      queue  jobs first", "    ▸ all  warm runners"}, "\n")
	if got := render(s.View(false, 40)); got != want {
		t.Fatalf("open view:\n%s\nwant:\n%s", got, want)
	}
	if !s.TakesKey(key("q")) || !s.TakesKey(key("1")) {
		t.Fatal("an open dropdown let a key through")
	}
	for _, k := range []string{"q", "j", "k", " "} { // held and ignored while open
		s.Update(key(k))
	}
	if !s.Open() || s.Value().Text != "all" || !strings.Contains(render(s.View(false, 40)), "▸ all") {
		t.Fatalf("an ignored key changed the open dropdown: open %v value %q", s.Open(), s.Value().Text)
	}
	s.Update(key("up"))
	s.Update(key("enter"))
	if s.Open() || s.Value().Text != "queue" {
		t.Fatalf("pick: open %v value %q", s.Open(), s.Value().Text)
	}
	s.Update(key(" "))
	s.Update(key("down"))
	s.Update(key("esc"))
	if s.Open() || s.Value().Text != "queue" {
		t.Fatalf("esc: open %v value %q", s.Open(), s.Value().Text)
	}
	if s.TakesKey(key("q")) || s.TakesKey(key("down")) {
		t.Fatal("a closed select took a page key")
	}
}

func TestSelectMouse(t *testing.T) {
	s := modeSelect()
	s.Update(clickAt(t, s.View(false, 40), "t/mode"))
	if !s.Open() {
		t.Fatal("click did not open")
	}
	s.Update(clickAt(t, s.View(false, 40), "t/mode/opt-1"))
	if s.Open() || s.Value().Text != "all" {
		t.Fatalf("option click: open %v value %q", s.Open(), s.Value().Text)
	}
	s.Update(clickAt(t, s.View(false, 40), "t/mode"))
	if !s.Hit(clickAt(t, s.View(false, 40), "t/mode/opt-0")) {
		t.Fatal("an open option is not part of the select")
	}
	if s.Hit(outside) {
		t.Fatal("a click elsewhere hit the select")
	}
	s.Update(outside)
	if s.Open() || s.Value().Text != "all" {
		t.Fatalf("outside click: open %v value %q", s.Open(), s.Value().Text)
	}
}

func TestSelectBlurAndDisable(t *testing.T) {
	s := modeSelect()
	s.Update(key("enter"))
	s.Blur()
	if s.Open() {
		t.Fatal("blur left the dropdown open")
	}
	s.Update(key("enter"))
	s.SetDisabled(true)
	if s.Open() || s.Focusable() {
		t.Fatal("disabling left it open or focusable")
	}
	s.Update(key("right"))
	if s.Value().Text != "queue" {
		t.Fatal("a disabled select changed")
	}
}
```

Create `internal/tui/ui/stepper_test.go`:

```go
package ui

import "testing"

func TestStepperBoundsAndKeys(t *testing.T) {
	s := NewStepper("t/max", 1, 99, 1)
	s.SetValue(Value{Num: 2, Set: true})
	if got := render(s.View(true, 40)); got != "› [ − ] 2 [ + ]" {
		t.Fatalf("view %q", got)
	}
	for _, k := range []string{"+", "=", "right"} {
		s.Update(key(k))
	}
	if s.Value().Num != 5 {
		t.Fatalf("up keys: %d", s.Value().Num)
	}
	for i := 0; i < 10; i++ {
		s.Update(key("-"))
	}
	if s.Value().Num != 1 {
		t.Fatalf("lower bound: %d", s.Value().Num)
	}
	s.Update(key("left"))
	if s.Value().Num != 1 {
		t.Fatalf("left below min: %d", s.Value().Num)
	}
}

func TestStepperTypedDigits(t *testing.T) {
	s := NewStepper("t/max", 1, 99, 1)
	typeText(s, "12")
	if s.Value().Num != 12 {
		t.Fatalf("typed 12: %d", s.Value().Num)
	}
	typeText(s, "3") // 123 > 99 starts over
	if s.Value().Num != 3 {
		t.Fatalf("typed past max: %d", s.Value().Num)
	}
	typeText(s, "4")
	s.Update(key("backspace"))
	if s.Value().Num != 3 {
		t.Fatalf("backspace: %d", s.Value().Num)
	}
	s.Update(key("backspace"))
	if s.Value().Num != 1 {
		t.Fatalf("backspace to empty clamps to min: %d", s.Value().Num)
	}
	s.Blur()
	typeText(s, "7")
	if s.Value().Num != 7 {
		t.Fatalf("typing after blur starts fresh: %d", s.Value().Num)
	}
	if !s.TakesKey(key("4")) || s.TakesKey(key("q")) || s.TakesKey(key("down")) {
		t.Fatal("TakesKey")
	}
}

func TestStepperStepAndSuffix(t *testing.T) {
	s := NewStepper("t/disk", 1, 100, 5)
	s.Suffix = "%"
	s.SetValue(Value{Num: 80, Set: true})
	s.Update(key("+"))
	if render(s.View(false, 40)) != "  [ − ] 85% [ + ]" {
		t.Fatalf("view %q", render(s.View(false, 40)))
	}
	for i := 0; i < 5; i++ {
		s.Update(key("+"))
	}
	if s.Value().Num != 100 {
		t.Fatalf("upper bound: %d", s.Value().Num)
	}
	typeText(s, "83")
	if s.Value().Num != 83 {
		t.Fatalf("typed values need not be multiples of the step: %d", s.Value().Num)
	}
}

func TestStepperDefaultUntilTouched(t *testing.T) {
	s := NewStepper("t/repo-max", 0, 99, 1)
	s.ZeroText = "∞"
	s.SetValue(Value{})
	s.Default = 1
	if got := render(s.View(false, 40)); got != "  [ − ] 1 [ + ] (default)" {
		t.Fatalf("queue default %q", got)
	}
	s.Default, s.DefaultText = 0, "∞"
	if got := render(s.View(false, 40)); got != "  [ − ] ∞ [ + ] (default)" {
		t.Fatalf("all default %q", got)
	}
	s.Update(key("+"))
	if v := s.Value(); !v.Set || v.Num != 1 {
		t.Fatalf("first + from the default: %+v", v)
	}
	s.Update(key("-"))
	if got := render(s.View(false, 40)); got != "  [ − ] ∞ [ + ]" {
		t.Fatalf("explicit zero %q", got)
	}
}

func TestStepperMouse(t *testing.T) {
	s := NewStepper("t/max", 1, 99, 1)
	s.SetValue(Value{Num: 2, Set: true})
	s.Update(clickAt(t, s.View(false, 40), "t/max/inc"))
	s.Update(clickAt(t, s.View(false, 40), "t/max/inc"))
	s.Update(clickAt(t, s.View(false, 40), "t/max/dec"))
	if s.Value().Num != 3 {
		t.Fatalf("clicks: %d", s.Value().Num)
	}
	if !s.Hit(clickAt(t, s.View(false, 40), "t/max")) || s.Hit(outside) {
		t.Fatal("Hit")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: NewSelect`, `undefined: Option` and `undefined: NewStepper`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/ui/select.go`:

```go
package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

type Option struct {
	Value, Label, Desc string
}

// Select picks one of a fixed set of options. Its dropdown opens inline: the
// options render as extra lines below it and push the content below down.
type Select struct {
	id       string
	Options  []Option
	Disabled bool
	sel      int
	open     bool
	cursor   int
}

func NewSelect(id string, opts []Option) *Select { return &Select{id: id, Options: opts} }

func (s *Select) ID() string         { return s.id }
func (s *Select) Focusable() bool    { return !s.Disabled }
func (s *Select) Capturing() bool    { return s.open }
func (s *Select) Open() bool         { return s.open }
func (s *Select) Blur()              { s.open = false }
func (s *Select) SetDisabled(d bool) { s.Disabled = d; s.open = s.open && !d }
func (s *Select) Selected() Option   { return s.Options[s.sel] }
func (s *Select) Value() Value       { return Value{Text: s.Options[s.sel].Value} }

// SetValue selects the option whose Value is v.Text; an unknown value is ignored.
func (s *Select) SetValue(v Value) {
	for i, o := range s.Options {
		if o.Value == v.Text {
			s.sel = i
		}
	}
}

func (s *Select) optZone(i int) string { return ZoneID(s.id, fmt.Sprintf("opt-%d", i)) }

// TakesKey: an open dropdown takes every key; a closed one takes ←/→ to
// cycle and enter/space to open.
func (s *Select) TakesKey(k tea.KeyMsg) bool {
	if s.open {
		return true
	}
	switch k.String() {
	case "left", "right", "enter", " ":
		return true
	}
	return false
}

func (s *Select) Hit(msg tea.MouseMsg) bool {
	if zone.Get(s.id).InBounds(msg) {
		return true
	}
	for i := range s.Options {
		if s.open && zone.Get(s.optZone(i)).InBounds(msg) {
			return true
		}
	}
	return false
}

func (s *Select) Update(msg tea.Msg) (Control, tea.Cmd) {
	if s.Disabled {
		return s, nil
	}
	n := len(s.Options)
	switch msg := msg.(type) {
	case tea.KeyMsg:
		k := msg.String()
		if s.open {
			// Every other key is held and ignored while the dropdown is open.
			switch k {
			case "up":
				s.cursor = max(s.cursor-1, 0)
			case "down":
				s.cursor = min(s.cursor+1, n-1)
			case "enter":
				s.sel, s.open = s.cursor, false
			case "esc":
				s.open = false
			}
			return s, nil
		}
		switch k {
		case "left":
			s.sel = (s.sel + n - 1) % n
		case "right":
			s.sel = (s.sel + 1) % n
		case "enter", " ":
			s.open, s.cursor = true, s.sel
		}
	case tea.MouseMsg:
		if !clicked(msg) {
			return s, nil
		}
		if s.open {
			// Any click closes the dropdown; one on an option also picks it.
			for i := range s.Options {
				if zone.Get(s.optZone(i)).InBounds(msg) {
					s.sel = i
				}
			}
			s.open = false
			return s, nil
		}
		if zone.Get(s.id).InBounds(msg) {
			s.open, s.cursor = true, s.sel
		}
	}
	return s, nil
}

// View renders [ queue ▾ ], followed by one line per option while open.
func (s *Select) View(focused bool, _ int) string {
	style := Bold
	if s.Disabled {
		style = Dim
	}
	lines := []string{glyph(focused) + zone.Mark(s.id, style.Render("[ "+s.Selected().Label+" ▾ ]"))}
	if s.open {
		for i, o := range s.Options {
			mark, label := "  ", o.Label
			if i == s.cursor {
				mark, label = Accent.Render("▸ "), Accent.Render(o.Label)
			}
			text := mark + label
			if o.Desc != "" {
				text += "  " + Dim.Render(o.Desc)
			}
			lines = append(lines, "    "+zone.Mark(s.optZone(i), text))
		}
	}
	return strings.Join(lines, "\n")
}
```

Create `internal/tui/ui/stepper.go`:

```go
package ui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// Stepper edits a bounded integer with − and +, the arrow keys, or typed digits.
// While unset it shows Default with a "(default)" suffix; the first change sets it.
type Stepper struct {
	id             string
	Min, Max, Step int
	Default        int    // the effective value while unset
	DefaultText    string // shown instead of Default while unset, when not empty
	ZeroText       string // shown for 0 when not empty (∞ for an unlimited cap)
	Suffix         string // appended to the number ("%")
	Disabled       bool
	val            int
	set            bool
	typed          string
}

func NewStepper(id string, lo, hi, step int) *Stepper {
	return &Stepper{id: id, Min: lo, Max: hi, Step: step, val: lo, set: true}
}

func (s *Stepper) ID() string         { return s.id }
func (s *Stepper) Focusable() bool    { return !s.Disabled }
func (s *Stepper) Capturing() bool    { return false }
func (s *Stepper) Blur()              { s.typed = "" }
func (s *Stepper) SetDisabled(d bool) { s.Disabled = d }
func (s *Stepper) Value() Value       { return Value{Num: s.val, Set: s.set} }
func (s *Stepper) SetValue(v Value)   { s.val, s.set, s.typed = v.Num, v.Set, "" }

func (s *Stepper) Hit(msg tea.MouseMsg) bool { return zone.Get(s.id).InBounds(msg) }

func (s *Stepper) TakesKey(k tea.KeyMsg) bool {
	switch k.String() {
	case "left", "right", "+", "=", "-", "backspace":
		return true
	}
	return isDigit(k)
}

func isDigit(k tea.KeyMsg) bool {
	return k.Type == tea.KeyRunes && len(k.Runes) == 1 && k.Runes[0] >= '0' && k.Runes[0] <= '9'
}

func (s *Stepper) clamp(n int) int { return min(max(n, s.Min), s.Max) }

func (s *Stepper) bump(d int) {
	start := s.val
	if !s.set {
		start = s.Default
	}
	s.val, s.set, s.typed = s.clamp(start+d), true, ""
}

func (s *Stepper) Update(msg tea.Msg) (Control, tea.Cmd) {
	if s.Disabled {
		return s, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch k := msg.String(); {
		case k == "right" || k == "+" || k == "=":
			s.bump(s.Step)
		case k == "left" || k == "-":
			s.bump(-s.Step)
		case k == "backspace":
			if s.typed != "" {
				s.typed = s.typed[:len(s.typed)-1]
				n, _ := strconv.Atoi(s.typed) // "" parses as 0, which clamps to Min
				s.val, s.set = s.clamp(n), true
			}
		case isDigit(msg):
			// Typed digits build a number; one that would pass Max starts over.
			s.typed += k
			if n, _ := strconv.Atoi(s.typed); n > s.Max {
				s.typed = k
			}
			n, _ := strconv.Atoi(s.typed)
			s.val, s.set = s.clamp(n), true
		}
	case tea.MouseMsg:
		if !clicked(msg) {
			return s, nil
		}
		if zone.Get(ZoneID(s.id, "dec")).InBounds(msg) {
			s.bump(-s.Step)
		} else if zone.Get(ZoneID(s.id, "inc")).InBounds(msg) {
			s.bump(s.Step)
		}
	}
	return s, nil
}

func (s *Stepper) text() string {
	if !s.set && s.DefaultText != "" {
		return s.DefaultText
	}
	n := s.val
	if !s.set {
		n = s.Default
	}
	if n == 0 && s.ZeroText != "" {
		return s.ZeroText
	}
	return strconv.Itoa(n) + s.Suffix
}

// View renders [ − ] 2 [ + ], with " (default)" while unset.
func (s *Stepper) View(focused bool, _ int) string {
	style := Bold
	if s.Disabled {
		style = Dim
	}
	body := zone.Mark(ZoneID(s.id, "dec"), style.Render("[ − ]")) + " " + style.Render(s.text()) + " " +
		zone.Mark(ZoneID(s.id, "inc"), style.Render("[ + ]"))
	if !s.set {
		body += Dim.Render(" (default)")
	}
	return glyph(focused) + zone.Mark(s.id, body)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s -v -run "TestSelect|TestStepper"`
Expected: PASS for the three `TestSelect…` and five `TestStepper…` tests.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/ui/select.go internal/tui/ui/select_test.go internal/tui/ui/stepper.go internal/tui/ui/stepper_test.go
git commit -m "feat(tui): add select and stepper controls"
```

### Task 6: TextField and TagList

**Files:**
- Create: `internal/tui/ui/textfield.go`
- Create: `internal/tui/ui/taglist.go`
- Test: `internal/tui/ui/textfield_test.go`
- Test: `internal/tui/ui/taglist_test.go`

**Interfaces:**
- Consumes: **ui controls**, **ui test helpers**
- Produces: `TextField`, `TagList` (see Contracts, **ui controls**)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/ui/textfield_test.go`:

```go
package ui

import (
	"testing"

	"github.com/darkraise/ghr/internal/config"
)

func durationField() *TextField {
	f := NewTextField("t/poll", 8)
	f.Check = func(s string) error { _, err := config.ParseDuration(s); return err }
	f.SetValue(Value{Text: "10s"})
	return f
}

func TestTextFieldEditing(t *testing.T) {
	f := durationField()
	if got := render(f.View(true, 40)); got != "› [ 10s      ]" {
		t.Fatalf("view %q", got)
	}
	if !f.TakesKey(key("q")) || !f.TakesKey(key("enter")) || f.TakesKey(key("down")) || f.TakesKey(key("tab")) {
		t.Fatal("TakesKey while not editing")
	}
	typeText(f, "30s") // a printable key starts editing and replaces the value
	if !f.Editing() || f.Value().Text != "30s" {
		t.Fatalf("editing %v value %q", f.Editing(), f.Value().Text)
	}
	if got := render(f.View(true, 40)); got != "› [ 30s      ]" {
		t.Fatalf("editing view %q", got)
	}
	if f.TakesKey(key("tab")) || f.TakesKey(key("ctrl+s")) || !f.TakesKey(key("q")) || !f.TakesKey(key("left")) {
		t.Fatal("TakesKey while editing")
	}
	_, cmd := f.Update(key("enter"))
	if f.Editing() || msgOf(cmd) != (Advance{"t/poll"}) {
		t.Fatalf("enter: editing %v msg %v", f.Editing(), msgOf(cmd))
	}
	f.Update(key("enter")) // enter starts editing and keeps the value
	f.Update(key("backspace"))
	f.Update(key("m"))
	f.Update(key("esc"))
	if f.Editing() || f.Value().Text != "30m" {
		t.Fatalf("esc keeps the typed value: editing %v value %q", f.Editing(), f.Value().Text)
	}
	f.Update(key("enter"))
	f.Blur()
	if f.Editing() {
		t.Fatal("blur did not stop editing")
	}
}

func TestTextFieldInlineError(t *testing.T) {
	f := durationField()
	typeText(f, "soon")
	want := "  [ soon     ]\n  ✖ invalid duration \"soon\""
	if got := render(f.View(false, 40)); got != want {
		t.Fatalf("view:\n%s\nwant:\n%s", got, want)
	}
	if f.Err() == nil {
		t.Fatal("Err is nil")
	}
}

func TestTextFieldClickAndDisable(t *testing.T) {
	f := durationField()
	f.Update(clickAt(t, f.View(false, 40), "t/poll"))
	if !f.Editing() {
		t.Fatal("click did not start editing")
	}
	f.SetDisabled(true)
	if f.Editing() || f.Focusable() {
		t.Fatal("disabling left it editing or focusable")
	}
	f.Update(key("x"))
	if f.Value().Text != "10s" {
		t.Fatal("a disabled field changed")
	}
}
```

Create `internal/tui/ui/taglist_test.go`:

```go
package ui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTagListAddAndRemove(t *testing.T) {
	l := NewTagList("t/labels")
	l.SetValue(Value{List: []string{"homelab"}})
	if got := render(l.View(true, 60)); got != "› homelab ✕  + add" {
		t.Fatalf("view %q", got)
	}
	if !l.TakesKey(key("enter")) || l.TakesKey(key("q")) {
		t.Fatal("TakesKey while not adding")
	}
	l.Update(key("enter"))
	if !l.Adding() || !l.TakesKey(key("q")) {
		t.Fatal("enter did not open the input")
	}
	typeText(l, "gpu")
	l.Update(key("enter"))
	typeText(l, "homelab") // duplicates are dropped
	l.Update(key("enter"))
	if got := l.Value().List; !reflect.DeepEqual(got, []string{"homelab", "gpu"}) {
		t.Fatalf("tags %v", got)
	}
	l.Update(key("backspace")) // empty input: removes the last tag
	if got := l.Value().List; !reflect.DeepEqual(got, []string{"homelab"}) {
		t.Fatalf("backspace: %v", got)
	}
	_, cmd := l.Update(key("enter")) // enter on an empty input closes it and advances
	if l.Adding() || msgOf(cmd) != (Advance{"t/labels"}) {
		t.Fatalf("empty enter: adding %v msg %v", l.Adding(), msgOf(cmd))
	}
}

// A draft is part of the value while typed, and esc, tab or blur commit it.
func TestTagListDraftIsNeverHidden(t *testing.T) {
	l := NewTagList("t/labels")
	l.SetValue(Value{List: []string{"a"}})
	l.Update(key("enter"))
	typeText(l, "b")
	if got := l.Value().List; !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("draft not in the value: %v", got)
	}
	l.Update(key("esc"))
	if got := l.Value().List; l.Adding() || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("esc: adding %v tags %v", l.Adding(), got)
	}
	l.Update(key("enter"))
	typeText(l, "c")
	l.Blur() // tab or a click elsewhere
	if got := l.Value().List; l.Adding() || !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("blur: adding %v tags %v", l.Adding(), got)
	}
	l.Update(key("enter"))
	typeText(l, "d")
	l.SetValue(Value{List: []string{"a"}}) // Discard
	if got := l.Value().List; l.Adding() || !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("SetValue kept the draft: adding %v tags %v", l.Adding(), got)
	}
}

func TestTagListMouse(t *testing.T) {
	l := NewTagList("t/labels")
	l.SetValue(Value{List: []string{"a", "b", "c"}})
	l.Update(clickAt(t, l.View(false, 60), "t/labels/x-1"))
	if got := l.Value().List; !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("✕ click: %v", got)
	}
	l.Update(clickAt(t, l.View(false, 60), "t/labels/add"))
	if !l.Adding() {
		t.Fatal("+ add click did not open the input")
	}
	if !l.Hit(clickAt(t, l.View(false, 60), "t/labels/tag-0")) || !l.Hit(clickAt(t, l.View(false, 60), "t/labels/input")) || l.Hit(outside) {
		t.Fatal("Hit")
	}
	l.Update(key("esc"))
	if l.Adding() {
		t.Fatal("esc left the input open")
	}
}

// Long lists wrap within the width, and every ✕ and "+ add" stays clickable.
func TestTagListWrapsToWidth(t *testing.T) {
	l := NewTagList("t/prefixes")
	var tags []string
	for i := 0; i < 6; i++ {
		tags = append(tags, fmt.Sprintf("cleanup-prefix-%d-", i))
	}
	l.SetValue(Value{List: tags})
	view := render(l.View(true, 40))
	lines := strings.Split(view, "\n")
	if len(lines) < 3 {
		t.Fatalf("did not wrap:\n%s", view)
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > 40 {
			t.Fatalf("line wider than 40: %q", line)
		}
	}
	l.Update(clickAt(t, l.View(true, 40), "t/prefixes/x-5"))
	l.Update(clickAt(t, l.View(true, 40), "t/prefixes/add"))
	if got := l.Value().List; len(got) != 5 || !l.Adding() {
		t.Fatalf("clicks on wrapped lines: tags %v adding %v", got, l.Adding())
	}
}

func TestTagListValueIsACopy(t *testing.T) {
	src := []string{"a"}
	l := NewTagList("t/labels")
	l.SetValue(Value{List: src})
	l.Value().List[0] = "z"
	src[0] = "y"
	if got := l.Value().List; got[0] != "a" {
		t.Fatalf("tags share memory with a caller: %v", got)
	}
	l.SetDisabled(true)
	if got := render(l.View(false, 60)); got != "  a" {
		t.Fatalf("disabled view %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: NewTextField` and `undefined: NewTagList`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/ui/textfield.go`:

```go
package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// TextField is a one-line input. Editing starts on enter (keeping the value),
// on a printable key (replacing it) or on a click; enter commits and asks to
// advance focus, esc stops editing and keeps what was typed.
type TextField struct {
	id       string
	Width    int                // columns inside the brackets
	Check    func(string) error // inline error shown under the field while it fails
	Disabled bool
	in       textinput.Model
	editing  bool
}

func NewTextField(id string, width int) *TextField {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 200
	in.Cursor.SetMode(cursor.CursorStatic)
	return &TextField{id: id, Width: width, in: in}
}

func (f *TextField) ID() string                { return f.id }
func (f *TextField) Focusable() bool           { return !f.Disabled }
func (f *TextField) Capturing() bool           { return false }
func (f *TextField) Editing() bool             { return f.editing }
func (f *TextField) Hit(msg tea.MouseMsg) bool { return zone.Get(f.id).InBounds(msg) }
func (f *TextField) Value() Value              { return Value{Text: strings.TrimSpace(f.in.Value())} }

func (f *TextField) SetValue(v Value) {
	f.in.SetValue(v.Text)
	f.in.CursorEnd()
}

func (f *TextField) SetDisabled(d bool) {
	f.Disabled = d
	if d {
		f.Blur()
	}
}

func (f *TextField) Blur() {
	f.editing = false
	f.in.Blur()
}

// Err is the inline error for the current value, or nil.
func (f *TextField) Err() error {
	if f.Check == nil {
		return nil
	}
	return f.Check(f.Value().Text)
}

func (f *TextField) TakesKey(k tea.KeyMsg) bool {
	if f.editing {
		return editorKey(k)
	}
	return printable(k) || k.String() == "enter"
}

func (f *TextField) startEditing() {
	f.editing = true
	f.in.Focus()
	f.in.CursorEnd()
}

func (f *TextField) Update(msg tea.Msg) (Control, tea.Cmd) {
	if f.Disabled {
		return f, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !f.editing {
			switch {
			case msg.String() == "enter":
				f.startEditing()
			case printable(msg):
				f.startEditing()
				f.in.SetValue("")
				f.in, _ = f.in.Update(msg)
			}
			return f, nil
		}
		switch msg.String() {
		case "enter":
			f.Blur()
			return f, send(Advance{f.id})
		case "esc":
			f.Blur()
			return f, nil
		}
		var cmd tea.Cmd
		f.in, cmd = f.in.Update(msg)
		return f, cmd
	case tea.MouseMsg:
		if clicked(msg) && f.Hit(msg) && !f.editing {
			f.startEditing()
		}
	}
	return f, nil
}

// View renders [ 5m         ] and, while the value fails Check, a line with the error.
func (f *TextField) View(focused bool, _ int) string {
	var text string
	if f.editing {
		f.in.Width = f.Width - 1
		text = f.in.View()
	} else {
		text = f.in.Value()
	}
	style := Bold
	if f.Disabled {
		style = Dim
	}
	out := glyph(focused) + zone.Mark(f.id, style.Render("[ ")+Cell(text, f.Width)+style.Render(" ]"))
	if err := f.Err(); err != nil {
		out += "\n  " + Red.Render("✖ "+err.Error())
	}
	return out
}
```

Create `internal/tui/ui/taglist.go`:

```go
package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
)

// TagList edits a list of short strings: ✕ removes a tag, "+ add" (or enter)
// opens an inline input where enter adds the draft and backspace on an empty
// input removes the last tag. A draft is never hidden: Value includes it, and
// esc, tab or any other focus change commits it. Enter on an empty input
// closes it and asks to advance focus.
type TagList struct {
	id       string
	Disabled bool
	tags     []string
	adding   bool
	in       textinput.Model
}

func NewTagList(id string) *TagList {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 100
	in.Width = 16
	in.Cursor.SetMode(cursor.CursorStatic)
	return &TagList{id: id, in: in}
}

func (l *TagList) ID() string      { return l.id }
func (l *TagList) Focusable() bool { return !l.Disabled }
func (l *TagList) Capturing() bool { return false }
func (l *TagList) Adding() bool    { return l.adding }

// Value is the tags plus the draft being typed, if it is a new tag.
func (l *TagList) Value() Value {
	out := append([]string{}, l.tags...)
	if d := l.draft(); d != "" && !l.has(d) {
		out = append(out, d)
	}
	return Value{List: out}
}

// SetValue replaces the tags and drops any draft.
func (l *TagList) SetValue(v Value) {
	l.tags = append([]string{}, v.List...)
	l.adding = false
	l.in.SetValue("")
	l.in.Blur()
}

func (l *TagList) SetDisabled(d bool) {
	l.Disabled = d
	if d {
		l.Blur()
	}
}

// Blur commits the draft and closes the input.
func (l *TagList) Blur() {
	l.commit()
	l.adding = false
	l.in.Blur()
}

func (l *TagList) Hit(msg tea.MouseMsg) bool {
	ids := []string{ZoneID(l.id, "add"), ZoneID(l.id, "input")}
	for i := range l.tags {
		ids = append(ids, l.xZone(i), l.tagZone(i))
	}
	for _, id := range ids {
		if zone.Get(id).InBounds(msg) {
			return true
		}
	}
	return false
}

func (l *TagList) TakesKey(k tea.KeyMsg) bool {
	if l.adding {
		return editorKey(k)
	}
	return k.String() == "enter" || k.String() == " "
}

func (l *TagList) draft() string { return strings.TrimSpace(l.in.Value()) }

func (l *TagList) has(tag string) bool {
	for _, t := range l.tags {
		if t == tag {
			return true
		}
	}
	return false
}

func (l *TagList) commit() {
	if d := l.draft(); d != "" && !l.has(d) {
		l.tags = append(l.tags, d)
	}
	l.in.SetValue("")
}

func (l *TagList) startAdding() {
	l.adding = true
	l.in.SetValue("")
	l.in.Focus()
}

func (l *TagList) xZone(i int) string   { return ZoneID(l.id, fmt.Sprintf("x-%d", i)) }
func (l *TagList) tagZone(i int) string { return ZoneID(l.id, fmt.Sprintf("tag-%d", i)) }

func (l *TagList) Update(msg tea.Msg) (Control, tea.Cmd) {
	if l.Disabled {
		return l, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !l.adding {
			if l.TakesKey(msg) {
				l.startAdding()
			}
			return l, nil
		}
		switch msg.String() {
		case "enter":
			if l.draft() == "" {
				l.Blur()
				return l, send(Advance{l.id})
			}
			l.commit()
			return l, nil
		case "esc":
			l.Blur()
			return l, nil
		case "backspace":
			if l.in.Value() == "" {
				if n := len(l.tags); n > 0 {
					l.tags = l.tags[:n-1]
				}
				return l, nil
			}
		}
		var cmd tea.Cmd
		l.in, cmd = l.in.Update(msg)
		return l, cmd
	case tea.MouseMsg:
		if !clicked(msg) {
			return l, nil
		}
		for i := range l.tags {
			if zone.Get(l.xZone(i)).InBounds(msg) {
				l.tags = append(l.tags[:i:i], l.tags[i+1:]...)
				return l, nil
			}
		}
		if zone.Get(ZoneID(l.id, "add")).InBounds(msg) && !l.adding {
			l.startAdding()
		}
	}
	return l, nil
}

// View renders tag ✕  tag ✕  + add, wrapping onto further lines (indented
// past the focus marker) so every tag, ✕ and the input stay within width.
func (l *TagList) View(focused bool, width int) string {
	style := Bold
	if l.Disabled {
		style = Dim
	}
	var parts []string
	for i, t := range l.tags {
		p := zone.Mark(l.tagZone(i), style.Render(t))
		if !l.Disabled {
			p += " " + zone.Mark(l.xZone(i), Dim.Render("✕"))
		}
		parts = append(parts, p)
	}
	switch {
	case l.adding:
		parts = append(parts, zone.Mark(ZoneID(l.id, "input"), style.Render("[ ")+Cell(l.in.View(), 16)+style.Render(" ]")))
	case !l.Disabled:
		parts = append(parts, zone.Mark(ZoneID(l.id, "add"), Accent.Render("+ add")))
	case len(l.tags) == 0:
		parts = append(parts, Dim.Render("none"))
	}
	avail := max(width-2, 10)
	var lines []string
	line := ""
	for _, p := range parts {
		switch {
		case line == "":
			line = p
		case ansi.StringWidth(line)+2+ansi.StringWidth(p) <= avail:
			line += "  " + p
		default:
			lines = append(lines, line)
			line = p
		}
	}
	lines = append(lines, line)
	return glyph(focused) + strings.Join(lines, "\n  ")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s -v -run "TestTextField|TestTagList"`
Expected: PASS for the three `TestTextField…` and five `TestTagList…` tests.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/ui/taglist.go internal/tui/ui/taglist_test.go internal/tui/ui/textfield.go internal/tui/ui/textfield_test.go
git commit -m "feat(tui): add text field and tag list controls"
```

### Task 7: Focus group

**Files:**
- Create: `internal/tui/ui/focus.go`
- Test: `internal/tui/ui/focus_test.go`

**Interfaces:**
- Consumes: **ui controls**, **ui test helpers**
- Produces: `Group` (see Contracts, **ui focus**)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/ui/focus_test.go`:

```go
package ui

import (
	"strings"
	"testing"
)

func threeButtons() (*Button, *Button, *Button, *Group) {
	a, b, c := NewButton("g/a", "A", Primary), NewButton("g/b", "B", Primary), NewButton("g/c", "C", Primary)
	g := &Group{}
	g.Set([]Widget{a, b, c})
	return a, b, c, g
}

func TestGroupStartsOnFirstFocusableAndWraps(t *testing.T) {
	a, _, _, g := threeButtons()
	a.Disabled = true
	g.Set(g.Items())
	if g.FocusedID() != "g/b" {
		t.Fatalf("initial focus %q", g.FocusedID())
	}
	g.Next()
	g.Next() // skips the disabled a and wraps to b
	if g.FocusedID() != "g/b" {
		t.Fatalf("next wrap %q", g.FocusedID())
	}
	g.Prev()
	if g.FocusedID() != "g/c" {
		t.Fatalf("prev wrap %q", g.FocusedID())
	}
}

func TestGroupKeepsFocusByIDAndMovesOffDisabled(t *testing.T) {
	a, b, c, g := threeButtons()
	g.Focus("g/b")
	g.Set([]Widget{c, b, a}) // rebuilt in another order
	if g.FocusedID() != "g/b" {
		t.Fatalf("focus lost on rebuild: %q", g.FocusedID())
	}
	b.Disabled = true
	g.Set([]Widget{c, b, a})
	if g.FocusedID() != "g/a" {
		t.Fatalf("focus not moved to the next enabled widget: %q", g.FocusedID())
	}
	g.Set([]Widget{c, b})
	if g.FocusedID() != "g/c" {
		t.Fatalf("focus not moved when its widget vanished: %q", g.FocusedID())
	}
	g.Set(nil)
	if g.Focused() != nil {
		t.Fatal("an empty group has focus")
	}
}

func TestGroupBlursOnFocusChange(t *testing.T) {
	f := NewTextField("g/name", 10)
	b := NewButton("g/ok", "OK", Primary)
	g := &Group{}
	g.Set([]Widget{f, b})
	g.Key(key("enter"))
	if !f.Editing() {
		t.Fatal("enter did not reach the text field")
	}
	g.Next()
	if f.Editing() {
		t.Fatal("moving focus did not stop editing")
	}
}

func TestGroupKeyRouting(t *testing.T) {
	_, _, _, g := threeButtons()
	if ok, cmd := g.Key(key("enter")); !ok || msgOf(cmd) != (Pressed{"g/a"}) {
		t.Fatalf("enter: handled %v", ok)
	}
	if ok, _ := g.Key(key("q")); ok {
		t.Fatal("a button took q")
	}
}

func TestGroupMouseFocusesAndCaptures(t *testing.T) {
	s := modeSelect()
	b := NewButton("g/ok", "OK", Primary)
	g := &Group{}
	g.Set([]Widget{s, b})
	view := strings.Join([]string{s.View(false, 40), b.View(false, 40)}, "\n")
	ok, cmd := g.Mouse(clickAt(t, view, "g/ok"))
	if !ok || g.FocusedID() != "g/ok" || msgOf(cmd) != (Pressed{"g/ok"}) {
		t.Fatalf("click on the button: handled %v focus %q", ok, g.FocusedID())
	}
	g.Mouse(clickAt(t, view, "t/mode"))
	if !s.Open() || g.FocusedID() != "t/mode" {
		t.Fatal("click on the select did not focus and open it")
	}
	view = strings.Join([]string{s.View(false, 40), b.View(false, 40)}, "\n")
	ok, cmd = g.Mouse(clickAt(t, view, "g/ok"))
	if !ok || cmd != nil || s.Open() || g.FocusedID() != "t/mode" {
		t.Fatalf("an open dropdown did not swallow the click: handled %v open %v focus %q", ok, s.Open(), g.FocusedID())
	}
	if ok, _ := g.Mouse(outside); ok {
		t.Fatal("a click on nothing was handled")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: Group`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/ui/focus.go`:

```go
package ui

import tea "github.com/charmbracelet/bubbletea"

// Group is the ordered set of widgets on a page or dialog that focus moves
// between. It tracks focus by widget ID, so a page can rebuild its widget
// list every frame without losing the focused control.
type Group struct {
	items []Widget
	focus string
	at    int // index of the focused widget the last time it was found
}

// Set replaces the widgets. Focus stays on the same ID when it is still
// present and focusable; otherwise it moves to the next focusable widget.
func (g *Group) Set(items []Widget) {
	g.items = items
	for i, w := range items {
		if w.ID() == g.focus {
			g.at = i
		}
	}
	w := g.Focused()
	if w != nil && w.Focusable() {
		return
	}
	if w != nil {
		w.Blur()
	}
	g.focus = ""
	n := len(items)
	start := min(g.at, n-1)
	for i := 0; i < n; i++ {
		if w := items[(start+i)%n]; w.Focusable() {
			g.focus, g.at = w.ID(), (start+i)%n
			return
		}
	}
}

func (g *Group) Items() []Widget { return g.items }

func (g *Group) FocusedID() string { return g.focus }

// Focused returns the focused widget, or nil.
func (g *Group) Focused() Widget {
	for _, w := range g.items {
		if w.ID() == g.focus {
			return w
		}
	}
	return nil
}

// Focus moves focus to id when it names a focusable widget.
func (g *Group) Focus(id string) bool {
	for i, w := range g.items {
		if w.ID() == id && w.Focusable() {
			if old := g.Focused(); old != nil && old != w {
				old.Blur()
			}
			g.focus, g.at = id, i
			return true
		}
	}
	return false
}

func (g *Group) Next() { g.step(1) }
func (g *Group) Prev() { g.step(-1) }

// step moves focus d places, skipping widgets that cannot take focus, and wraps.
func (g *Group) step(d int) {
	n := len(g.items)
	if n == 0 {
		return
	}
	cur := -1
	for i, w := range g.items {
		if w.ID() == g.focus {
			cur = i
		}
	}
	if cur < 0 {
		cur = min(g.at, n-1)
		if d > 0 {
			cur--
		}
	}
	for i := 1; i <= n; i++ {
		if w := g.items[((cur+d*i)%n+n)%n]; w.Focusable() {
			g.Focus(w.ID())
			return
		}
	}
}

// Key gives k to the focused widget when it takes it.
func (g *Group) Key(k tea.KeyMsg) (bool, tea.Cmd) {
	w := g.Focused()
	if w == nil || !w.TakesKey(k) {
		return false, nil
	}
	_, cmd := w.Update(k)
	return true, cmd
}

// Mouse handles a click: a capturing widget gets every click; otherwise the
// widget under the pointer takes focus and the click. It reports false when
// the event was not a click on one of the group's widgets.
func (g *Group) Mouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	if !clicked(msg) {
		return false, nil
	}
	if w := g.Focused(); w != nil && w.Capturing() {
		_, cmd := w.Update(msg)
		return true, cmd
	}
	for _, w := range g.items {
		if w.Focusable() && w.Hit(msg) {
			g.Focus(w.ID())
			_, cmd := w.Update(msg)
			return true, cmd
		}
	}
	return false, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s -v -run TestGroup`
Expected: PASS for the five `TestGroup…` tests.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/ui/focus.go internal/tui/ui/focus_test.go
git commit -m "feat(tui): add focus group"
```

### Task 8: Toast

**Files:**
- Create: `internal/tui/ui/toast.go`
- Test: `internal/tui/ui/toast_test.go`

**Interfaces:**
- Consumes: **ui theme**, **ui test helpers**
- Produces: `Toast` (see Contracts, **ui toast**)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/ui/toast_test.go`:

```go
package ui

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func TestToastExpiry(t *testing.T) {
	var ts Toast
	ts.Show("Settings saved", false, t0)
	ts.Tick(t0.Add(3 * time.Second))
	if !ts.Active() {
		t.Fatal("success cleared before 4s")
	}
	ts.Tick(t0.Add(4 * time.Second))
	if ts.Active() {
		t.Fatal("success still shown at 4s")
	}
	ts.Show("boom", true, t0)
	ts.Tick(t0.Add(9 * time.Second))
	if !ts.Active() {
		t.Fatal("error cleared before 10s")
	}
	ts.Tick(t0.Add(10 * time.Second))
	if ts.Active() {
		t.Fatal("error still shown at 10s")
	}
}

func TestToastReplaceViewAndClose(t *testing.T) {
	var ts Toast
	if ts.View(30) != "" {
		t.Fatal("an inactive toast rendered")
	}
	ts.Show("first", false, t0)
	ts.Show("second failure", true, t0.Add(time.Second))
	if got := render(ts.View(30)); got != " ✖ second failure            ✕" {
		t.Fatalf("view %q", got)
	}
	ts.Tick(t0.Add(5 * time.Second)) // the replacement carries its own 10s
	if !ts.Active() {
		t.Fatal("the replacing error expired on the first toast's clock")
	}
	ts.Show("a very long message that will not fit", false, t0)
	if got := render(ts.View(20)); got != " ✔ a very long me… ✕" {
		t.Fatalf("truncated view %q", got)
	}
	click := clickAt(t, ts.View(20), ts.CloseZone())
	if click.X != 19 {
		t.Fatalf("✕ at column %d", click.X)
	}
	ts.Close()
	if ts.Active() {
		t.Fatal("close did not clear")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: Toast`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/ui/toast.go`:

```go
package ui

import (
	"time"

	zone "github.com/lrstanley/bubblezone"
)

const (
	ToastOK    = 4 * time.Second
	ToastError = 10 * time.Second
	toastClose = "toast/close"
)

// Toast is the single transient message on the toast line. A newer one
// replaces the current one.
type Toast struct {
	Text  string
	Err   bool
	until time.Time
}

// Show replaces the toast; it clears after ToastOK, or ToastError for an error.
func (t *Toast) Show(text string, isErr bool, now time.Time) {
	d := ToastOK
	if isErr {
		d = ToastError
	}
	t.Text, t.Err, t.until = text, isErr, now.Add(d)
}

// Tick clears the toast once its time is up.
func (t *Toast) Tick(now time.Time) {
	if t.Text != "" && !now.Before(t.until) {
		t.Close()
	}
}

func (t *Toast) Close() { t.Text, t.Err = "", false }

func (t *Toast) Active() bool { return t.Text != "" }

// CloseZone is the zone of the toast's ✕.
func (t *Toast) CloseZone() string { return toastClose }

// View renders the toast across width columns with a clickable ✕ at the
// right, or "" when no toast is active.
func (t *Toast) View(width int) string {
	if t.Text == "" {
		return ""
	}
	icon, style := "✔ ", Green
	if t.Err {
		icon, style = "✖ ", Red
	}
	return " " + style.Render(Cell(icon+t.Text, max(width-3, 1))) + " " + zone.Mark(toastClose, Dim.Render("✕"))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s -v -run TestToast`
Expected: PASS for `TestToastExpiry` and `TestToastReplaceViewAndClose`.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/ui/toast.go internal/tui/ui/toast_test.go
git commit -m "feat(tui): add single toast"
```

### Task 9: Form state

**Files:**
- Create: `internal/tui/ui/form.go`
- Test: `internal/tui/ui/form_test.go`

**Interfaces:**
- Consumes: **ui controls**
- Produces: `Kind`, `Equal`, `Field`, `Spec`, `Form` (see Contracts, **ui form**)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/ui/form_test.go`:

```go
package ui

import (
	"reflect"
	"testing"
)

func TestEqualByKind(t *testing.T) {
	cases := []struct {
		k    Kind
		a, b Value
		want bool
	}{
		{KindDuration, Value{Text: "120s"}, Value{Text: "2m0s"}, true},
		{KindDuration, Value{Text: "1d"}, Value{Text: "24h"}, true},
		{KindDuration, Value{Text: "soon"}, Value{Text: "2m"}, false},
		{KindDuration, Value{Text: "soon"}, Value{Text: "soon"}, true},
		{KindText, Value{Text: "6G"}, Value{Text: "6g"}, false},
		{KindInt, Value{Num: 1, Set: false}, Value{Num: 1, Set: true}, false},
		{KindInt, Value{Num: 3, Set: false}, Value{Num: 7, Set: false}, true},
		{KindInt, Value{Num: 2, Set: true}, Value{Num: 2, Set: true}, true},
		{KindList, Value{List: nil}, Value{List: []string{}}, true},
		{KindList, Value{List: []string{"a", "b"}}, Value{List: []string{"b", "a"}}, false},
	}
	for _, c := range cases {
		if got := Equal(c.k, c.a, c.b); got != c.want {
			t.Errorf("Equal(%v, %+v, %+v) = %v", c.k, c.a, c.b, got)
		}
	}
}

func testSpecs(poll string, labels []string, repos ...string) []Spec {
	out := []Spec{
		{Key: "poll", Kind: KindDuration, Base: Value{Text: poll}, New: func() Input { return NewTextField("poll", 8) }},
		{Key: "labels", Kind: KindList, Base: Value{List: labels}, New: func() Input { return NewTagList("labels") }},
	}
	for _, r := range repos {
		out = append(out, Spec{Key: r + "/max", Kind: KindInt, Base: Value{Num: 1, Set: true},
			New: func() Input { return NewStepper(r+"/max", 0, 99, 1) }})
	}
	return out
}

func fieldKeys(fs []*Field) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Key)
	}
	return out
}

func TestFormDirtyAndDiscard(t *testing.T) {
	var f Form
	f.Merge(testSpecs("10s", []string{"homelab"}, "darkmem"))
	if len(f.Dirty()) != 0 {
		t.Fatalf("fresh form dirty: %v", fieldKeys(f.Dirty()))
	}
	f.Field("poll").Input.SetValue(Value{Text: "10000ms"})
	if len(f.Dirty()) != 0 {
		t.Fatal("an equal duration counts as dirty")
	}
	f.Field("poll").Input.SetValue(Value{Text: "30s"})
	f.Field("darkmem/max").Input.SetValue(Value{Num: 2, Set: true})
	if got := fieldKeys(f.Dirty()); !reflect.DeepEqual(got, []string{"poll", "darkmem/max"}) {
		t.Fatalf("dirty %v", got)
	}
	f.Discard()
	if len(f.Dirty()) != 0 || f.Field("poll").Input.Value().Text != "10s" {
		t.Fatal("discard did not reset")
	}
}

func TestFormMergeKeepsEditsAndControls(t *testing.T) {
	var f Form
	f.Merge(testSpecs("10s", []string{"homelab"}, "darkmem", "darkcloud"))
	poll := f.Field("poll").Input
	poll.SetValue(Value{Text: "30s"})
	f.Field("labels").Input.SetValue(Value{List: []string{"homelab", "gpu"}})

	// The daemon now has poll 20s and labels [homelab gpu]; darkcloud is gone and darkagents is new.
	f.Merge(testSpecs("20s", []string{"homelab", "gpu"}, "darkmem", "darkagents"))
	if f.Field("poll").Input != poll {
		t.Fatal("merge replaced an existing control")
	}
	if got := f.Field("poll"); got.Base.Text != "20s" || got.Input.Value().Text != "30s" || !got.Dirty() {
		t.Fatalf("dirty field lost its edit: base %+v edit %q", got.Base, got.Input.Value().Text)
	}
	if f.Field("labels").Dirty() {
		t.Fatal("a field whose edit now equals the new base is still dirty")
	}
	if got := fieldKeys(f.Fields()); !reflect.DeepEqual(got, []string{"poll", "labels", "darkmem/max", "darkagents/max"}) {
		t.Fatalf("fields %v", got)
	}
	if v := f.Field("darkagents/max").Input.Value(); !reflect.DeepEqual(v, Value{Num: 1, Set: true}) {
		t.Fatalf("new field not set to its base: %+v", v)
	}
}

func TestFormMergeRefreshesCleanFieldsAndLockedDropsEdits(t *testing.T) {
	var f Form
	f.Merge(testSpecs("10s", nil, "darkmem"))
	f.Field("darkmem/max").Input.SetValue(Value{Num: 5, Set: true})
	next := testSpecs("15s", nil, "darkmem")
	next[2].Locked = true // darkmem is now being removed
	f.Merge(next)
	if v := f.Field("poll").Input.Value().Text; v != "15s" {
		t.Fatalf("clean field not refreshed: %q", v)
	}
	if f.Field("darkmem/max").Dirty() {
		t.Fatal("a locked field kept its edit")
	}
}

func TestFormReset(t *testing.T) {
	var f Form
	f.Merge(testSpecs("10s", nil))
	f.Field("poll").Input.SetValue(Value{Text: "30s"})
	f.Field("labels").Input.SetValue(Value{List: []string{"x"}})
	f.Reset([]string{"poll", "missing"})
	if f.Field("poll").Dirty() || !f.Field("labels").Dirty() {
		t.Fatal("Reset touched the wrong fields")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: Equal`, `undefined: Form` and `undefined: Spec`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/ui/form.go`:

```go
package ui

import "github.com/darkraise/ghr/internal/config"

// Kind says how a field's values compare.
type Kind int

const (
	KindText     Kind = iota // Text, compared exactly
	KindDuration             // Text, compared by parsed value: 120s equals 2m0s
	KindInt                  // Num and Set
	KindList                 // List, element by element; nil equals empty
)

// Equal compares two values of kind k.
func Equal(k Kind, a, b Value) bool {
	switch k {
	case KindDuration:
		da, errA := config.ParseDuration(a.Text)
		db, errB := config.ParseDuration(b.Text)
		if errA == nil && errB == nil {
			return da == db
		}
		return a.Text == b.Text
	case KindInt:
		return a.Set == b.Set && (!a.Set || a.Num == b.Num)
	case KindList:
		if len(a.List) != len(b.List) {
			return false
		}
		for i := range a.List {
			if a.List[i] != b.List[i] {
				return false
			}
		}
		return true
	}
	return a.Text == b.Text
}

// Field pairs a setting's base (its value as last loaded) with the Input
// holding its edit.
type Field struct {
	Key   string
	Kind  Kind
	Base  Value
	Input Input
}

func (f *Field) Dirty() bool { return !Equal(f.Kind, f.Base, f.Input.Value()) }

// Spec describes one field as the latest config has it.
type Spec struct {
	Key    string
	Kind   Kind
	Base   Value
	New    func() Input // builds the control the first time Key appears
	Locked bool         // drop any edit and show Base (a repo being removed)
}

// Form is the ordered set of fields of a settings page.
type Form struct {
	fields []*Field
}

func (f *Form) Fields() []*Field { return f.fields }

// Field returns the field with key, or nil.
func (f *Form) Field(key string) *Field {
	for _, fl := range f.fields {
		if fl.Key == key {
			return fl
		}
	}
	return nil
}

// Dirty returns the fields whose edit differs from their base, in order.
func (f *Form) Dirty() []*Field {
	var out []*Field
	for _, fl := range f.fields {
		if fl.Dirty() {
			out = append(out, fl)
		}
	}
	return out
}

// Discard resets every edit to its base.
func (f *Form) Discard() {
	for _, fl := range f.fields {
		fl.Input.SetValue(fl.Base)
	}
}

// Reset resets the edits of the listed keys to their bases.
func (f *Form) Reset(keys []string) {
	for _, k := range keys {
		if fl := f.Field(k); fl != nil {
			fl.Input.SetValue(fl.Base)
		}
	}
}

// Merge brings the form up to date with a freshly loaded config. Each field
// takes its new base; a dirty field keeps its edit unless its spec is Locked,
// and any other field shows the new base. Fields missing from specs are
// dropped and new ones are added. Existing controls are kept, so an open
// dropdown or a field being edited survives a refresh.
func (f *Form) Merge(specs []Spec) {
	out := make([]*Field, 0, len(specs))
	for _, s := range specs {
		fl := f.Field(s.Key)
		if fl == nil {
			fl = &Field{Key: s.Key, Kind: s.Kind, Base: s.Base, Input: s.New()}
			fl.Input.SetValue(s.Base)
			out = append(out, fl)
			continue
		}
		keep := fl.Dirty() && !s.Locked
		fl.Kind, fl.Base = s.Kind, s.Base
		if !keep {
			fl.Input.SetValue(s.Base)
		}
		out = append(out, fl)
	}
	f.fields = out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s -v -run "TestEqualByKind|TestForm"`
Expected: PASS for `TestEqualByKind` and the four `TestForm…` tests.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/ui/form.go internal/tui/ui/form_test.go
git commit -m "feat(tui): add form state with merge"
```

### Task 10: Form layout and scroll-into-view

**Files:**
- Create: `internal/tui/ui/layout.go`
- Test: `internal/tui/ui/layout_test.go`

**Interfaces:**
- Consumes: **ui controls**, `Select`, `Stepper`, `Button`
- Produces: `Row`, `Section`, `Range`, `Render`, `ScrollTo`, `LabelWidth` (see Contracts, **ui layout**)

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/ui/layout_test.go`:

```go
package ui

import (
	"reflect"
	"strings"
	"testing"
)

func layoutSections() ([]Section, *Select, *Stepper, *Button, *Button) {
	mode := modeSelect()
	max := NewStepper("t/max", 1, 99, 1)
	max.SetValue(Value{Num: 2, Set: true})
	pause, remove := NewButton("t/pause", "Pause", Primary), NewButton("t/remove", "Remove", Danger)
	return []Section{
		{Title: "General", Rows: []Row{
			{Label: "Mode", Items: []Widget{mode}, Desc: "jobs first"},
			{Label: "Global max", Items: []Widget{max}, Desc: "queue mode", Dirty: true},
			{Label: "Owner", Text: "darkraise"},
		}},
		{Title: "darkmem", Note: "removing…", Rows: []Row{
			{Items: []Widget{pause, remove}, Err: "warm must be <= max"},
		}},
	}, mode, max, pause, remove
}

func TestRenderWide(t *testing.T) {
	secs, _, _, _, _ := layoutSections()
	lines, ranges := Render(secs, "t/max", 64, true)
	want := []string{
		"╭─ General ────────────────────────────────────────────────────╮",
		"│ Mode                 [ queue ▾ ]  jobs first                 │",
		"│ Global max         › [ − ] 2 [ + ] ●  queue mode             │",
		"│ Owner                darkraise                               │",
		"╰──────────────────────────────────────────────────────────────╯",
		"╭─ darkmem ────────────────────────────────────────────────────╮",
		"│ removing…                                                    │",
		"│                      [ Pause ]   [ Remove ]                  │",
		"│                      ✖ warm must be <= max                   │",
		"╰──────────────────────────────────────────────────────────────╯",
	}
	if got := strings.Split(render(strings.Join(lines, "\n")), "\n"); !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	wantRanges := map[string]Range{"t/mode": {1, 2}, "t/max": {2, 3}, "t/pause": {7, 9}, "t/remove": {7, 9}}
	if !reflect.DeepEqual(ranges, wantRanges) {
		t.Fatalf("ranges %v", ranges)
	}
}

func TestRenderNarrowPutsDescriptionsBelowAndCountsOpenDropdown(t *testing.T) {
	secs, mode, _, _, _ := layoutSections()
	mode.Update(key("enter"))
	lines, ranges := Render(secs, "", 50, false)
	got := render(strings.Join(lines[:7], "\n"))
	want := strings.Join([]string{
		"╭─ General ──────────────────────────────────────╮",
		"│ Mode                 [ queue ▾ ]               │",
		"│                        ▸ queue  jobs first     │",
		"│                          all  warm runners     │",
		"│                      jobs first                │",
		"│ Global max           [ − ] 2 [ + ] ●           │",
		"│                      queue mode                │",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if ranges["t/mode"] != (Range{1, 5}) || ranges["t/max"] != (Range{5, 7}) {
		t.Fatalf("ranges %v", ranges)
	}
}

func TestRenderUntitledSectionHasNoFrame(t *testing.T) {
	add := NewButton("t/add", "+ Add repository", Primary)
	lines, ranges := Render([]Section{{Rows: []Row{{Items: []Widget{add}}}}}, "", 44, true)
	if got := render(strings.Join(lines, "\n")); got != "                     [ + Add repository ]   " {
		t.Fatalf("got %q", got)
	}
	if ranges["t/add"] != (Range{0, 1}) {
		t.Fatalf("ranges %v", ranges)
	}
}

func TestScrollTo(t *testing.T) {
	cases := []struct {
		off, h int
		r      Range
		want   int
	}{
		{0, 10, Range{3, 4}, 0},   // already visible
		{0, 10, Range{12, 14}, 5}, // below: bottom-align, one line of context
		{8, 10, Range{2, 3}, 1},   // above: top-align, one line of context
		{4, 10, Range{0, 1}, 0},   // above at the very top: no line before it
		{0, 3, Range{5, 10}, 4},   // taller than the view: top-align
	}
	for _, c := range cases {
		if got := ScrollTo(c.off, c.h, c.r); got != c.want {
			t.Errorf("ScrollTo(%d, %d, %v) = %d, want %d", c.off, c.h, c.r, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: Section`, `undefined: Render` and `undefined: ScrollTo`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/ui/layout.go`:

```go
package ui

import "strings"

// LabelWidth is the width of a form row's label column.
const LabelWidth = 18

// Row is one setting on a form card: a label, its widgets, a description.
type Row struct {
	Label string
	Items []Widget // left to right; only the first may span several lines
	Text  string   // shown read-only when Items is empty
	Desc  string   // after the control when wide, else on its own line below
	Err   string   // shown in red under the row
	Dirty bool     // adds a ● after the controls
}

// Section is a card of rows. An untitled section renders without a frame.
type Section struct {
	Title string
	Note  string // dim line at the top of the card
	Rows  []Row
}

// Range is the lines [Start, End) a widget occupies in a rendered form.
type Range struct{ Start, End int }

// Render lays sections out as cards of total width w and returns the lines
// with the line range of every widget, so a page can scroll a widget into
// view without waiting for the next frame's zones.
func Render(secs []Section, focused string, w int, wide bool) ([]string, map[string]Range) {
	var out []string
	ranges := map[string]Range{}
	for _, s := range secs {
		inner, top := w, len(out)
		if s.Title != "" {
			inner, top = w-4, top+1 // inside the frame, below its top border
		}
		var body []string
		if s.Note != "" {
			body = append(body, Dim.Render(s.Note))
		}
		for _, r := range s.Rows {
			start := len(body)
			body = append(body, renderRow(r, focused, inner, wide)...)
			for _, it := range r.Items {
				ranges[it.ID()] = Range{top + start, top + len(body)}
			}
		}
		if s.Title == "" {
			for _, l := range body {
				out = append(out, Cell(l, w))
			}
			continue
		}
		out = append(out, strings.Split(Box(s.Title, w, body), "\n")...)
	}
	return out, ranges
}

func renderRow(r Row, focused string, w int, wide bool) []string {
	pad := strings.Repeat(" ", LabelWidth+1)
	line := Cell(r.Label, LabelWidth) + " "
	var rest []string
	if len(r.Items) == 0 {
		line += "  " + r.Text
	}
	for i, it := range r.Items {
		lines := strings.Split(it.View(it.ID() == focused, w-LabelWidth-1), "\n")
		if i > 0 {
			line += " "
		} else {
			rest = lines[1:]
		}
		line += lines[0]
	}
	if r.Dirty {
		line += " " + Amber.Render("●")
	}
	if r.Desc != "" && wide {
		line += "  " + Dim.Render(r.Desc)
	}
	out := []string{line}
	for _, l := range rest {
		out = append(out, pad+l)
	}
	if r.Desc != "" && !wide {
		out = append(out, pad+"  "+Dim.Render(r.Desc))
	}
	if r.Err != "" {
		out = append(out, pad+"  "+Red.Render("✖ "+r.Err))
	}
	return out
}

// ScrollTo returns the scroll offset that brings r into a view of h lines,
// moving as little as possible from off but keeping one line of context (a
// card border or the neighbouring row) beyond it. A range taller than the
// view is aligned to its top. Callers clamp the result to the content.
func ScrollTo(off, h int, r Range) int {
	switch {
	case r.Start < off || r.End-r.Start >= h:
		return max(r.Start-1, 0)
	case r.End > off+h:
		return r.End + 1 - h
	}
	return off
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ui/ -count=1 -timeout 180s -v -run "TestRender|TestScrollTo"`
Expected: PASS for the three `TestRender…` tests and `TestScrollTo`.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/ui/layout.go internal/tui/ui/layout_test.go
git commit -m "feat(tui): add form layout and scroll-into-view"
```

### Task 11: Pages, toasts and tab focus

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/input.go`
- Modify: `internal/tui/view.go`
- Test: `internal/tui/tui_test.go`
- Modify (regenerated): `internal/tui/testdata/TestDashboardGolden/120.golden`, `.../80.golden`

**Interfaces:**
- Consumes: `Toast`
- Produces: **TUI pages** and **TUI test helpers** (see Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

The tabs become pages (Config is renamed Settings; its content changes in Task 15), a `ui.Toast` replaces the always-present flash line, and `tab` moves focus within a page instead of switching pages.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/tui_test.go`, replace:

```go
		return tea.KeyMsg{Type: tea.KeyRight}
	}
```

with:

```go
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
```

In `internal/tui/tui_test.go`, replace:

```go
// click renders m, waits for zone id and feeds a left click on it.
func click(t *testing.T, m Model, id string) Model {
	t.Helper()
	zone.Clear(id)
	m.View()
	z := waitZone(t, id)
	return feed(m, leftClick(z.StartX+1, z.StartY))
```

with:

```go
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
```

In `internal/tui/tui_test.go`, replace:

```go
		t.Fatal("flash line missing")
```

with:

```go
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
```

In `internal/tui/tui_test.go`, replace:

```go
		t.Fatalf("repo keys on the Runners tab: overlay %v actions %v", m.overlay, c.actions())
	}
	run(t, m, "4", "x")
	if len(c.actions()) != 0 {
		t.Fatalf("x on the Config tab: actions %v", c.actions())
```

with:

```go
		t.Fatalf("repo keys on the Runners page: overlay %v actions %v", m.overlay, c.actions())
	}
	run(t, m, "4", "x")
	if len(c.actions()) != 0 {
		t.Fatalf("x on the Settings page: actions %v", c.actions())
```

In `internal/tui/tui_test.go`, replace:

```go
	m.View()
	z := waitZone(t, "runner-1")
```

with:

```go
	z := zoneOf(t, m, "runner-1")
```

In `internal/tui/tui_test.go`, replace:

```go
	if m.tab != tabRunners || m.logID != "a3f9c1" {
		t.Fatalf("l: tab %v log %q", m.tab, m.logID)
```

with:

```go
	if m.page != pageRunners || m.logID != "a3f9c1" {
		t.Fatalf("l: page %v log %q", m.page, m.logID)
```

In `internal/tui/tui_test.go`, replace:

```go
	m.tab = tabConfig
```

with:

```go
	m.page = pageSettings
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with errors such as `m.page undefined`, `undefined: pageRunners` and `m.toast undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/model.go`, replace:

```go
	"github.com/darkraise/ghr/internal/model"
)
```

with:

```go
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)
```

In `internal/tui/model.go`, replace:

```go
type tab int

const (
	tabDashboard tab = iota
	tabRunners
	tabHistory
	tabConfig
)

var tabNames = []string{"Dashboard", "Runners", "History", "Config"}
```

with:

```go
type page int

const (
	pageDashboard page = iota
	pageRunners
	pageHistory
	pageSettings
)

var pageNames = []string{"Dashboard", "Runners", "History", "Settings"}
```

In `internal/tui/model.go`, replace:

```go
	tab           tab
```

with:

```go
	page          page
```

In `internal/tui/model.go`, replace:

```go
	flash     string
	flashErr  bool
```

with:

```go
	toast     ui.Toast
```

In `internal/tui/model.go`, replace:

```go
// follow points the log pane of the Runners tab at the selected runner,
// starting its log over when that is a different runner.
func (m *Model) follow() tea.Cmd {
	r := m.selectedRunner()
	if m.tab != tabRunners || r == nil || r.ID == m.logID {
```

with:

```go
// follow points the log pane of the Runners page at the selected runner,
// starting its log over when that is a different runner.
func (m *Model) follow() tea.Cmd {
	r := m.selectedRunner()
	if m.page != pageRunners || r == nil || r.ID == m.logID {
```

In `internal/tui/model.go`, replace:

```go
		cmds := []tea.Cmd{tick(), m.fetchStatus(), m.fetchEvents()}
		if m.tab == tabRunners {
			cmds = append(cmds, m.fetchLog())
		}
		if m.tab == tabHistory && m.frame%slowPoll == 0 {
```

with:

```go
		m.toast.Tick(m.now())
		cmds := []tea.Cmd{tick(), m.fetchStatus(), m.fetchEvents()}
		if m.page == pageRunners {
			cmds = append(cmds, m.fetchLog())
		}
		if m.page == pageHistory && m.frame%slowPoll == 0 {
```

In `internal/tui/model.go`, replace:

```go
		if m.tab == tabConfig && m.cfg == nil {
```

with:

```go
		if m.page == pageSettings && m.cfg == nil {
```

In `internal/tui/model.go`, replace:

```go
			m.flash, m.flashErr = clean(msg.err.Error()), true
		} else if msg.text != "" {
			m.flash, m.flashErr = msg.text, false
```

with:

```go
			m.toast.Show(clean(msg.err.Error()), true, m.now())
		} else if msg.text != "" {
			m.toast.Show(msg.text, false, m.now())
```

In `internal/tui/input.go`, replace:

```go
		return m.switchTab(tab(key[0] - '1'))
	case "tab":
		return m.switchTab((m.tab + 1) % 4)
	case "shift+tab":
		return m.switchTab((m.tab + 3) % 4)
```

with:

```go
		return m.switchPage(page(key[0] - '1'))
	case "tab", "shift+tab":
		// tab moves focus within a page; on the Dashboard, between its two tables.
		if m.page == pageDashboard {
			m.focus = 1 - m.focus
		}
```

In `internal/tui/input.go`, replace:

```go
			m.tab = tabRunners
```

with:

```go
			m.page = pageRunners
```

In `internal/tui/input.go`, replace:

```go
	case "r":
		if m.tab == tabHistory {
			m.histRepo = m.cycle(m.histRepo, m.repoNames())
```

with:

```go
	case "r":
		if m.page == pageHistory {
			m.histRepo = m.cycle(m.histRepo, m.repoNames())
```

In `internal/tui/input.go`, replace:

```go
	case "c":
		if m.tab == tabHistory {
			m.histConcl = m.cycle(m.histConcl, []string{"success", "failure", "cancelled"})
```

with:

```go
	case "c":
		if m.page == pageHistory {
			m.histConcl = m.cycle(m.histConcl, []string{"success", "failure", "cancelled"})
```

In `internal/tui/input.go`, replace:

```go
func (m Model) switchTab(t tab) (tea.Model, tea.Cmd) {
	m.tab = t
	switch t {
	case tabRunners:
		cmd := m.follow()
		return m, cmd
	case tabHistory:
		return m, m.fetchHistory()
	case tabConfig:
```

with:

```go
func (m Model) switchPage(p page) (tea.Model, tea.Cmd) {
	m.page = p
	switch p {
	case pageRunners:
		cmd := m.follow()
		return m, cmd
	case pageHistory:
		return m, m.fetchHistory()
	case pageSettings:
```

In `internal/tui/input.go`, replace:

```go
	case m.tab == tabHistory:
		m.histSel = clamp(m.histSel+d, len(m.hist))
	case m.tab == tabConfig:
		m.cfgSel = clamp(m.cfgSel+d, len(m.configFields()))
	case m.tab == tabRunners || m.focus == paneRunners:
```

with:

```go
	case m.page == pageHistory:
		m.histSel = clamp(m.histSel+d, len(m.hist))
	case m.page == pageSettings:
		m.cfgSel = clamp(m.cfgSel+d, len(m.configFields()))
	case m.page == pageRunners || m.focus == paneRunners:
```

In `internal/tui/input.go`, replace:

```go
		return nil // unlimited: change it in the Config tab
```

with:

```go
		return nil // unlimited: change it on the Settings page
```

In `internal/tui/input.go`, replace:

```go
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
```

with:

```go
	return m.page == pageRunners || (m.page == pageDashboard && m.focus == paneRunners)
}

func (m Model) repoFocus() bool {
	return m.page == pageDashboard && m.focus == paneRepos
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
	case pageSettings:
```

In `internal/tui/input.go`, replace:

```go
	if r := m.selectedRunner(); r != nil && (m.tab == tabRunners || m.focus == paneRunners) {
```

with:

```go
	if r := m.selectedRunner(); r != nil && (m.page == pageRunners || m.focus == paneRunners) {
```

In `internal/tui/input.go`, replace:

```go
// configFields lists the editable settings of the Config tab.
```

with:

```go
// configFields lists the editable settings of the Settings page.
```

In `internal/tui/input.go`, replace:

```go
	for i := range tabNames {
		if zone.Get(fmt.Sprintf("tab-%d", i)).InBounds(msg) {
			return m.switchTab(tab(i))
```

with:

```go
	if zone.Get(m.toast.CloseZone()).InBounds(msg) {
		m.toast.Close()
		return m, nil
	}
	for i := range pageNames {
		if zone.Get(fmt.Sprintf("tab-%d", i)).InBounds(msg) {
			return m.switchPage(page(i))
```

In `internal/tui/view.go`, replace:

```go
	parts = append(parts, m.tabBar(w))
	used := lipgloss.Height(strings.Join(parts, "\n")) + 2 // footer + flash
```

with:

```go
	if m.toast.Active() {
		parts = append(parts, m.toast.View(w))
	}
	parts = append(parts, m.tabBar(w))
	used := lipgloss.Height(strings.Join(parts, "\n")) + 1 // footer
```

In `internal/tui/view.go`, replace:

```go
	switch m.tab {
	case tabDashboard:
		parts = append(parts, m.dashboard(w, bodyH))
	case tabRunners:
		parts = append(parts, m.runnersTab(w, bodyH))
	case tabHistory:
		parts = append(parts, m.historyTab(w, bodyH))
	case tabConfig:
		parts = append(parts, m.configTab(w, bodyH))
	}
	parts = append(parts, m.flashLine(), m.footer(w))
```

with:

```go
	switch m.page {
	case pageDashboard:
		parts = append(parts, m.dashboard(w, bodyH))
	case pageRunners:
		parts = append(parts, m.runnersTab(w, bodyH))
	case pageHistory:
		parts = append(parts, m.historyTab(w, bodyH))
	case pageSettings:
		parts = append(parts, m.configTab(w, bodyH))
	}
	parts = append(parts, m.footer(w))
```

In `internal/tui/view.go`, replace:

```go
	for i, name := range tabNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if tab(i) == m.tab {
```

with:

```go
	for i, name := range pageNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if page(i) == m.page {
```

In `internal/tui/view.go`, replace:

```go
		selected := i == m.runnerSel && (m.focus == paneRunners || m.tab == tabRunners)
```

with:

```go
		selected := i == m.runnerSel && (m.focus == paneRunners || m.page == pageRunners)
```

In `internal/tui/view.go`, delete:

```go
func (m Model) flashLine() string {
	if m.flash == "" {
		return ""
	}
	if m.flashErr {
		return sRed.Render(" ✖ " + m.flash)
	}
	return sGreen.Render(" ✔ " + m.flash)
}

```

In `internal/tui/view.go`, replace:

```go
			"1-4 / tab   switch tab            ↑↓ / j k   move selection",
```

with:

```go
			"1-4         switch page           ↑↓ / j k   move selection",
			"tab         move focus",
```

- [ ] **Step 4: Regenerate the snapshots**

Run: `go test ./internal/tui/ -run TestDashboardGolden -update -count=1 -timeout 180s`
Expected: the command exits 0 and writes both dashboard snapshots. Open each file and compare it with the snapshot below (trailing spaces are trimmed here; the files keep them). Against the old snapshots, the only changes are `4 Config` becoming `4 Settings`, the blank flash line above the footer going away, and the Events card growing by that one line.

`internal/tui/testdata/TestDashboardGolden/120.golden`:

```text
╭─ ghr ────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│  mode ● QUEUE   global ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%                                     │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
[ 1 Dashboard ]  2 Runners    3 History    4 Settings                                                            ? help
╭─ Repos ──────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│   REPO           STATE      RUN   QUEUE  LAST JOB                                                                    │
│ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint  2m ago                                                         │
│   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago                                                  │
│   darkagents     ◌ paused   0/1   ⧗ 1    –                                                                           │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
╭─ Runners ────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│   ID       REPO         STATE       JOB                                                                ELAPSED       │
│   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412                                            12m04s        │
│   7be210   darkmem      ○ idle      –                                                                  1m30s         │
│   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)                                                       │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
╭─ Events ─────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ HH:MM:SS  ✔ darkcloud   #411 lint success 2m10s  cleanup: 3 ctrs                                                     │
│ HH:MM:SS  ▶ darkcloud   spawned a3f9c1 (2.330.0)                                                                     │
│ HH:MM:SS  ⚠             disk 81% > high-water 80% — pruned build cache                                               │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
 p pause  + repo cap  -  [ global cap  ]  m mode  x kill  l logs  enter details  ? help  q quit
```

`internal/tui/testdata/TestDashboardGolden/80.golden`:

```text
╭─ ghr ────────────────────────────────────────────────────────────────────────╮
│  mode ● QUEUE   global ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░… │
╰──────────────────────────────────────────────────────────────────────────────╯
[ 1 Dashboard ]  2 Runners    3 History    4 Settings                    ? help
╭─ Repos ──────────────────────────────────────────────────────────────────────╮
│   REPO           STATE      RUN   QUEUE  LAST JOB                            │
│ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint  2m ago                 │
│   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago          │
│   darkagents     ◌ paused   0/1   ⧗ 1    –                                   │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Runners ────────────────────────────────────────────────────────────────────╮
│   ID       REPO         STATE       JOB                        ELAPSED       │
│   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412    12m04s        │
│   7be210   darkmem      ○ idle      –                          1m30s         │
│   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)               │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Events ─────────────────────────────────────────────────────────────────────╮
│ HH:MM:SS  ✔ darkcloud   #411 lint success 2m10s  cleanup: 3 ctrs             │
│ HH:MM:SS  ▶ darkcloud   spawned a3f9c1 (2.330.0)                             │
│ HH:MM:SS  ⚠             disk 81% > high-water 80% — pruned build cache       │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
╰──────────────────────────────────────────────────────────────────────────────╯
 p pause  + repo cap  -  [ global cap  ]  m mode  x kill  l logs  enter details…
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run "TestToastLifecycle|TestTabMovesFocusNotPage|TestDashboard|TestKeyActions|TestRunnersTabFollowsSelection"`
Expected: PASS for all of them.

- [ ] **Step 6: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/input.go internal/tui/model.go internal/tui/testdata/TestDashboardGolden/120.golden internal/tui/testdata/TestDashboardGolden/80.golden internal/tui/tui_test.go internal/tui/view.go
git commit -m "feat(tui): pages, toasts and tab focus"
```

### Task 12: Admin console shell

**Files:**
- Create: `internal/tui/shell.go`
- Test: `internal/tui/shell_test.go`
- Modify: `internal/tui/view.go` (`View`; remove `header`, `tabBar`, `footer`)
- Modify: `internal/tui/input.go` (remove `footerKeys`; navigation clicks)
- Test: `internal/tui/tui_test.go`
- Modify (regenerated): `internal/tui/testdata/TestDashboardGolden/120.golden`, `.../80.golden`

**Interfaces:**
- Consumes: **TUI pages**, **ui theme**, `Toast`
- Produces: **TUI shell** (see Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

The boxed header and tab bar give way to a one-line top bar of status chips, a full-width alert line, the toast line, a 16-column sidebar (a tab row under 100 columns), a page header line and a per-page footer. The page bodies keep their current content.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/shell_test.go`:

```go
package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The chips shed detail in a fixed order as the bar narrows.
func TestTopBarShedsDetailInOrder(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	full := m.topBar(200)
	for _, want := range []string{"runners ▕", "api ▕", "disk ▕", "● connected"} {
		if !strings.Contains(full, want) {
			t.Fatalf("full bar missing %q: %q", want, full)
		}
	}
	steps := []struct{ gone, kept string }{
		{"connected", "api ▕"},  // 1: the connection chip shrinks to its dot
		{"api ▕", "disk ▕"},     // 2: the API gauge bar goes
		{"disk ▕", "runners ▕"}, // 3: the disk gauge bar goes
		{"runners ▕", "2/3"},    // 4: the runners gauge bar goes
	}
	for level, s := range steps {
		w := ansi.StringWidth(m.chips(level)) - 1 // one column too narrow for the previous level
		bar := m.topBar(w)
		if strings.Contains(bar, s.gone) || !strings.Contains(bar, s.kept) || ansi.StringWidth(bar) > w {
			t.Fatalf("level %d at %d columns: %q", level+1, w, bar)
		}
	}
	if bar := m.topBar(30); ansi.StringWidth(bar) > 30 {
		t.Fatalf("bar overflows 30 columns: %q", bar)
	}
}

func TestDiskStateUsesHighWater(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	for _, c := range []struct {
		pct, hw int
		want    string
	}{{61, 0, "ok"}, {80, 0, "warn"}, {61, 60, "warn"}, {59, 60, "ok"}, {95, 99, "critical"}} {
		m.st.DiskPct, m.cfg = c.pct, nil
		if c.hw > 0 {
			m.cfg = sampleConfig(t, "darkcloud")
			m.cfg.DiskHighWater = c.hw
		}
		if got := m.diskState(); got != c.want {
			t.Errorf("disk %d%% high-water %d: %s, want %s", c.pct, c.hw, got, c.want)
		}
	}
}

func TestSidebarAndTabRowNavigate(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	if v := m.View(); !strings.Contains(v, "▌ 1 Dashboard") || !strings.Contains(v, "? Help") {
		t.Fatalf("wide layout has no sidebar:\n%s", v)
	}
	if m = click(t, m, "nav/history"); m.page != pageHistory {
		t.Fatalf("sidebar click: page %v", m.page)
	}
	if m = click(t, m, "nav/help"); m.overlay != ovHelp {
		t.Fatal("sidebar Help did not open help")
	}
	m = feed(m, key("esc"))

	n := sampleModel(&fakeClient{}, 80, 30)
	if v := n.View(); strings.Contains(v, "▌ 1 Dashboard") || !strings.Contains(v, "[ 1 Dashboard ]") {
		t.Fatalf("narrow layout should use the tab row:\n%s", v)
	}
	if n = click(t, n, "nav/settings"); n.page != pageSettings {
		t.Fatalf("tab row click: page %v", n.page)
	}
}

// Down to the 40-column minimum, every frame line fits and the tab row
// shrinks its labels instead of overflowing.
func TestNarrowFramesFit(t *testing.T) {
	for _, w := range []int{99, 72, 56, 40} {
		m := sampleModel(&fakeClient{}, w, 30)
		for i, line := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(line) > w {
				t.Fatalf("width %d, line %d is %d wide: %q", w, i, lipgloss.Width(line), line)
			}
		}
		if row := m.tabRow(w); !strings.Contains(row, "? help") || !strings.Contains(row, "4") {
			t.Fatalf("width %d tab row %q", w, row)
		}
		if m = click(t, m, "nav/settings"); m.page != pageSettings {
			t.Fatalf("width %d: tab row click went to page %v", w, m.page)
		}
	}
}

func TestAlertLineAndSettingsHeader(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 30), statusMsg{err: errors.New("connection refused")})
	m.page = pageSettings
	v := m.View()
	for _, want := range []string{"daemon unreachable: connection refused — retrying", "○ reconnecting", "reconnecting"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if lipgloss.Width(strings.Split(v, "\n")[1]) != 120 {
		t.Error("the alert line is not full width")
	}
}

func TestFooterFollowsPageAndClicksRunKeys(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	if m = click(t, m, "key-x"); m.overlay != ovNone {
		t.Fatal("x on the Repos card opened something")
	}
	m.page = pageSettings
	if v := m.View(); !strings.Contains(v, "ctrl+s save") || strings.Contains(v, "x kill") {
		t.Fatalf("Settings footer:\n%s", v)
	}
	m.page = pageDashboard
	if m = click(t, m, "key-enter"); m.overlay != ovNone {
		t.Fatal("enter on the Repos card opened the detail view")
	}
	m.focus = paneRunners
	if m = click(t, m, "key-enter"); m.overlay != ovDetail {
		t.Fatal("a footer click did not run the key")
	}
}
```

In `internal/tui/tui_test.go`, replace:

```go
	for _, want := range []string{"mode ● QUEUE", "global", "2/3", "darkcloud", "⧗ 2", "#411 lint", "2m ago",
```

with:

```go
	for _, want := range []string{"mode ● QUEUE", "runners", "2/3", "darkcloud", "⧗ 2", "#411 lint", "2m ago",
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `m.topBar undefined`, `m.chips undefined` and `m.diskState undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/shell.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
)

const (
	sidebarWidth = 16
	wideMin      = 100 // below this many columns a tab row replaces the sidebar
)

func navZone(p page) string { return "nav/" + strings.ToLower(pageNames[p]) }

// topBar renders the status chips, shedding detail until they fit in w
// columns: the connection chip shrinks to its dot, then the API, disk and
// runners gauge bars are dropped in that order.
func (m Model) topBar(w int) string {
	for level := 0; level <= 4; level++ {
		if s := m.chips(level); ansi.StringWidth(s) <= w {
			return s
		}
	}
	return ansi.Truncate(m.chips(4), w, "…")
}

func (m Model) chips(level int) string {
	running := 0
	for _, i := range m.st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	runners := fmt.Sprintf("%d/%d", running, m.st.GlobalMax)
	if m.st.Mode == "all" {
		runners = fmt.Sprintf("%d/∞", running)
	} else if level < 4 {
		runners = gauge(running, m.st.GlobalMax, 3) + " " + runners
	}
	api := fmt.Sprint(m.st.RateRemaining)
	if level < 2 {
		api = gauge(max(m.st.RateRemaining, 0), 5000, 10) + " " + api
	}
	disk := fmt.Sprintf("%d%%", m.st.DiskPct)
	if level < 3 {
		disk = gauge(m.st.DiskPct, 100, 10) + " " + disk
	}
	diskStyle := sDim
	switch m.diskState() {
	case "warn":
		diskStyle = sAmber
	case "critical":
		diskStyle = sRed
	}
	disk = diskStyle.Render(disk)
	conn := sGreen.Render("● connected")
	if !m.connected {
		conn = sRed.Render("○ reconnecting")
	}
	if level >= 1 {
		conn = ansi.Truncate(conn, 1, "")
	}
	return " " + sAccent.Render("ghr") + "   mode " + sGreen.Render("● "+strings.ToUpper(m.st.Mode)) +
		"   runners " + runners + "   api " + api + "   disk " + disk + "   " + conn
}

// diskState is "critical" at 95% or more, "warn" at or above disk_high_water
// (80% until the config has loaded), else "ok".
func (m Model) diskState() string {
	threshold := 80
	if m.cfg != nil {
		threshold = m.cfg.DiskHighWater
	}
	switch {
	case m.st.DiskPct >= 95:
		return "critical"
	case m.st.DiskPct >= threshold:
		return "warn"
	}
	return "ok"
}

// alertLine is the full-width banner shown while the daemon is unreachable or degraded.
func (m Model) alertLine(w int) string {
	switch {
	case !m.connected:
		return sBanner.Width(w).Render(ansi.Truncate("daemon unreachable: "+m.connErr+" — retrying", w-2, "…"))
	case m.st.Degraded:
		return sBanner.Width(w).Render(ansi.Truncate("DEGRADED: "+m.st.DegradedReason+" — no new runners", w-2, "…"))
	}
	return ""
}

// sidebar is the wide layout's page list, h lines tall, with Help and Quit at the bottom.
func (m Model) sidebar(h int) string {
	var lines []string
	for i, name := range pageNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if page(i) == m.page {
			label = sAccent.Render("▌ " + label)
		} else {
			label = "  " + label
		}
		lines = append(lines, zone.Mark(navZone(page(i)), cell(label, sidebarWidth)))
	}
	bottom := []string{
		zone.Mark("nav/help", cell(sDim.Render("  ? Help"), sidebarWidth)),
		zone.Mark("nav/quit", cell(sDim.Render("  q Quit"), sidebarWidth)),
	}
	for len(lines)+len(bottom) < h {
		lines = append(lines, cell("", sidebarWidth))
	}
	return strings.Join(append(lines, bottom...), "\n")
}

// tabRow replaces the sidebar under wideMin columns.
// The labels shrink to short names, then to numbers, until the row fits.
func (m Model) tabRow(w int) string {
	short := []string{"Dash", "Run", "Hist", "Set"}
	var row string
	for form := 0; form < 3; form++ {
		var tabs []string
		for i, name := range pageNames {
			label := fmt.Sprintf(" %d %s ", i+1, name)
			switch form {
			case 1:
				label = fmt.Sprintf(" %d %s ", i+1, short[i])
			case 2:
				label = fmt.Sprintf(" %d ", i+1)
			}
			if page(i) == m.page {
				label = sAccent.Render("[" + label + "]")
			} else {
				label = sDim.Render(" " + label + " ")
			}
			tabs = append(tabs, zone.Mark(navZone(page(i)), label))
		}
		bar := strings.Join(tabs, "")
		help := zone.Mark("nav/help", sDim.Render("? help"))
		gap := w - ansi.StringWidth(bar) - ansi.StringWidth(help) - 1
		row = bar + strings.Repeat(" ", max(gap, 1)) + help
		if gap >= 1 {
			break
		}
	}
	return row
}

// pageHeader is the first line of the content area: the page title on the
// left and the page's status on the right.
func (m Model) pageHeader(w int) string {
	title := sBold.Render(pageNames[m.page])
	right := ""
	if m.page == pageSettings && !m.connected {
		right = sRed.Render("reconnecting")
	}
	gap := max(w-ansi.StringWidth(title)-ansi.StringWidth(right), 1)
	return title + strings.Repeat(" ", gap) + right
}

type footerKey struct{ key, label string }

// footerKeys are the clickable key hints for the current page.
func (m Model) footerKeys() []footerKey {
	switch m.page {
	case pageRunners:
		return []footerKey{{"x", "kill"}, {"l", "logs"}, {"enter", "details"}, {"?", "help"}, {"q", "quit"}}
	case pageHistory:
		return []footerKey{{"r", "repo"}, {"c", "result"}, {"enter", "copy URL"}, {"?", "help"}, {"q", "quit"}}
	case pageSettings:
		return []footerKey{{"tab", "next"}, {"shift+tab", "previous"}, {"ctrl+s", "save"}, {"?", "help"}, {"q", "quit"}}
	}
	return []footerKey{
		{"p", "pause"}, {"+", "repo cap"}, {"-", ""}, {"[", "global cap"}, {"]", ""}, {"m", "mode"},
		{"x", "kill"}, {"l", "logs"}, {"enter", "details"}, {"?", "help"}, {"q", "quit"},
	}
}

func (m Model) footer(w int) string {
	var parts []string
	for _, f := range m.footerKeys() {
		text := sAccent.Render(f.key)
		if f.label != "" {
			text += " " + sDim.Render(f.label)
		}
		parts = append(parts, zone.Mark("key-"+f.key, text))
	}
	return " " + ansi.Truncate(strings.Join(parts, "  "), w-1, "…")
}

// keyMsg is the KeyMsg for a footer hint, so a click runs the same path as the key.
func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// layout composes the shell around body, the current page's content.
func (m Model) layout(w int, body func(w, h int) string) string {
	top := []string{m.topBar(w)}
	if a := m.alertLine(w); a != "" {
		top = append(top, a)
	}
	if m.toast.Active() {
		top = append(top, m.toast.View(w))
	}
	wide := w >= wideMin
	if !wide {
		top = append(top, m.tabRow(w))
	}
	bodyH := max(m.height-len(top)-1, 7) // the footer takes the last line
	cw := w
	if wide {
		cw = w - sidebarWidth - 1
	}
	content := m.pageHeader(cw) + "\n" + body(cw, bodyH-1)
	if wide {
		content = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(bodyH), " ", content)
	}
	return strings.Join(append(top, content, m.footer(w)), "\n")
}
```

In `internal/tui/view.go`, replace:

```go
	w := m.width
	if w < 40 {
		w = 40
	}
	var parts []string
	parts = append(parts, m.header(w))
	if !m.connected {
		parts = append(parts, sBanner.Render("daemon unreachable: "+m.connErr+" — retrying"))
	} else if m.st.Degraded {
		parts = append(parts, sBanner.Render("DEGRADED: "+m.st.DegradedReason+" — no new runners"))
	}
	if m.toast.Active() {
		parts = append(parts, m.toast.View(w))
	}
	parts = append(parts, m.tabBar(w))
	used := lipgloss.Height(strings.Join(parts, "\n")) + 1 // footer
	bodyH := m.height - used
	if bodyH < 6 {
		bodyH = 6
	}
	switch m.page {
	case pageDashboard:
		parts = append(parts, m.dashboard(w, bodyH))
	case pageRunners:
		parts = append(parts, m.runnersTab(w, bodyH))
	case pageHistory:
		parts = append(parts, m.historyTab(w, bodyH))
	case pageSettings:
		parts = append(parts, m.configTab(w, bodyH))
	}
	parts = append(parts, m.footer(w))
	out := strings.Join(parts, "\n")
```

with:

```go
	w := max(m.width, 40)
	out := m.layout(w, m.pageBody)
```

In `internal/tui/view.go`, replace:

```go
func (m Model) header(w int) string {
	running := 0
	for _, i := range m.st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	mode := sGreen.Render("● " + strings.ToUpper(m.st.Mode))
	global := fmt.Sprintf("%s %d/%d", gauge(running, m.st.GlobalMax, 3), running, m.st.GlobalMax)
	if m.st.Mode == "all" {
		global = fmt.Sprintf("%d/∞", running)
	}
	api := fmt.Sprintf("%s %d", gauge(max(m.st.RateRemaining, 0), 5000, 10), m.st.RateRemaining)
	diskStyle := sDim
	if m.st.DiskPct >= 80 {
		diskStyle = sAmber
	}
	disk := diskStyle.Render(fmt.Sprintf("%s %d%%", gauge(m.st.DiskPct, 100, 10), m.st.DiskPct))
	line := fmt.Sprintf(" mode %s   global %s   api %s   disk %s", mode, global, api, disk)
	return box("ghr", w, []string{line})
}

func (m Model) tabBar(w int) string {
	var tabs []string
	for i, name := range pageNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if page(i) == m.page {
			label = sAccent.Render("[" + label + "]")
		} else {
			label = sDim.Render(" " + label + " ")
		}
		tabs = append(tabs, zone.Mark(fmt.Sprintf("tab-%d", i), label))
	}
	bar := strings.Join(tabs, "")
	help := sDim.Render("? help")
	gap := w - ansi.StringWidth(bar) - ansi.StringWidth(help) - 1
	if gap < 1 {
		gap = 1
	}
	return bar + strings.Repeat(" ", gap) + help
```

with:

```go
// pageBody renders the current page's content in w columns and h lines.
func (m Model) pageBody(w, h int) string {
	switch m.page {
	case pageRunners:
		return m.runnersTab(w, h)
	case pageHistory:
		return m.historyTab(w, h)
	case pageSettings:
		return m.configTab(w, h)
	}
	return m.dashboard(w, h)
```

In `internal/tui/view.go`, delete:

```go
func (m Model) footer(w int) string {
	var parts []string
	for _, f := range footerKeys {
		text := sAccent.Render(f.key)
		if f.label != "" {
			text += " " + sDim.Render(f.label)
		}
		parts = append(parts, zone.Mark("key-"+f.key, text))
	}
	return " " + ansi.Truncate(strings.Join(parts, "  "), w-1, "…")
}

```

In `internal/tui/input.go`, delete:

```go
// footerKeys are the clickable hints shown in the footer, in order.
var footerKeys = []struct{ key, label string }{
	{"p", "pause"}, {"+", "repo cap"}, {"-", ""}, {"[", "global cap"}, {"]", ""}, {"m", "mode"},
	{"x", "kill"}, {"l", "logs"}, {"enter", "details"}, {"?", "help"}, {"q", "quit"},
}

```

In `internal/tui/input.go`, replace:

```go
		if zone.Get(fmt.Sprintf("tab-%d", i)).InBounds(msg) {
			return m.switchPage(page(i))
		}
	}
	for _, f := range footerKeys {
		if zone.Get("key-" + f.key).InBounds(msg) {
			return m.press(f.key)
```

with:

```go
		if zone.Get(navZone(page(i))).InBounds(msg) {
			return m.switchPage(page(i))
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
			return m.handleKey(keyMsg(f.key))
```

- [ ] **Step 4: Regenerate the snapshots**

Run: `go test ./internal/tui/ -run TestDashboardGolden -update -count=1 -timeout 180s`
Expected: the command exits 0 and writes both dashboard snapshots. Open each file and compare it with the snapshot below (trailing spaces are trimmed here; the files keep them).

`internal/tui/testdata/TestDashboardGolden/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
▌ 1 Dashboard    Dashboard
  2 Runners      ╭─ Repos ─────────────────────────────────────────────────────────────────────────────────────────────╮
  3 History      │   REPO           STATE      RUN   QUEUE  LAST JOB                                                   │
  4 Settings     │ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint  2m ago                                        │
                 │   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago                                 │
                 │   darkagents     ◌ paused   0/1   ⧗ 1    –                                                          │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Runners ───────────────────────────────────────────────────────────────────────────────────────────╮
                 │   ID       REPO         STATE       JOB                                               ELAPSED       │
                 │   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412                           12m04s        │
                 │   7be210   darkmem      ○ idle      –                                                 1m30s         │
                 │   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)                                      │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Events ────────────────────────────────────────────────────────────────────────────────────────────╮
                 │ HH:MM:SS  ✔ darkcloud   #411 lint success 2m10s  cleanup: 3 ctrs                                    │
                 │ HH:MM:SS  ▶ darkcloud   spawned a3f9c1 (2.330.0)                                                    │
                 │ HH:MM:SS  ⚠             disk 81% > high-water 80% — pruned build cache                              │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
  ? Help         │                                                                                                     │
  q Quit         ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
 p pause  + repo cap  -  [ global cap  ]  m mode  x kill  l logs  enter details  ? help  q quit
```

`internal/tui/testdata/TestDashboardGolden/80.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api 4800   disk ▕██████░░░░▏ 61%   ●
[ 1 Dashboard ]  2 Runners    3 History    4 Settings                    ? help
Dashboard
╭─ Repos ──────────────────────────────────────────────────────────────────────╮
│   REPO           STATE      RUN   QUEUE  LAST JOB                            │
│ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint  2m ago                 │
│   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago          │
│   darkagents     ◌ paused   0/1   ⧗ 1    –                                   │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Runners ────────────────────────────────────────────────────────────────────╮
│   ID       REPO         STATE       JOB                        ELAPSED       │
│   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412    12m04s        │
│   7be210   darkmem      ○ idle      –                          1m30s         │
│   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)               │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Events ─────────────────────────────────────────────────────────────────────╮
│ HH:MM:SS  ✔ darkcloud   #411 lint success 2m10s  cleanup: 3 ctrs             │
│ HH:MM:SS  ▶ darkcloud   spawned a3f9c1 (2.330.0)                             │
│ HH:MM:SS  ⚠             disk 81% > high-water 80% — pruned build cache       │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
╰──────────────────────────────────────────────────────────────────────────────╯
 p pause  + repo cap  -  [ global cap  ]  m mode  x kill  l logs  enter details…
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run "TestTopBar|TestDiskState|TestSidebarAndTabRow|TestNarrowFramesFit|TestAlertLine|TestFooter|TestDashboard"`
Expected: PASS for all of them.

- [ ] **Step 6: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/input.go internal/tui/shell.go internal/tui/shell_test.go internal/tui/testdata/TestDashboardGolden/120.golden internal/tui/testdata/TestDashboardGolden/80.golden internal/tui/tui_test.go internal/tui/view.go
git commit -m "feat(tui): add the admin console shell"
```

### Task 13: Shared modal for Confirm and Help

**Files:**
- Create: `internal/tui/dialogs.go`
- Modify: `internal/tui/view.go` (remove `buttons` and `withOverlay`)
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/input.go`
- Test: `internal/tui/tui_test.go`

**Interfaces:**
- Consumes: `Button`, `Group`, **TUI shell**
- Produces: **TUI dialogs** (see Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

Confirm and Help render through one modal: a bold title, the body (wrapped to the screen), buttons on the right. As spec §1 says, `enter` presses the primary button; `space` presses the focused one. The runner detail popup keeps its current rendering until Plan 3 replaces it with a page.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/tui_test.go`, replace:

```go
	}
	m = click(t, m, "btn-ok")
	if m.overlay != ovNone || strings.Join(c.actions(), "|") != "kill a3f9c1" {
```

with:

```go
	}
	m = click(t, m, btnYes)
	if m.overlay != ovNone || strings.Join(c.actions(), "|") != "kill a3f9c1" {
```

In `internal/tui/tui_test.go`, replace:

```go
func TestHelpMentionsMouseNotes(t *testing.T) {
	v := run(t, sampleModel(&fakeClient{}, 120, 30), "?").View()
	for _, want := range []string{"Shift-drag", "Option-drag in iTerm2", "set -g mouse on"} {
```

with:

```go
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

// enter presses the primary button even when another button has focus.
func TestModalEnterPressesPrimary(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = feed(m, keys("x", "tab", "enter")...)
	if m.overlay != ovNone || strings.Join(c.actions(), "|") != "kill a3f9c1" {
		t.Fatalf("overlay %v actions %v", m.overlay, c.actions())
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: btnYes`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/dialogs.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	btnYes   = "dialog/yes"
	btnNo    = "dialog/no"
	btnClose = "dialog/close"
)

// openDialog shows overlay ov with buttons, right-aligned in the given order.
// Focus starts on the last button, the primary one; esc presses cancel.
func (m *Model) openDialog(ov overlay, cancel string, buttons ...*ui.Button) {
	m.overlay, m.dlgButtons, m.dlgCancel = ov, buttons, cancel
	ws := make([]ui.Widget, len(buttons))
	for i, b := range buttons {
		ws[i] = b
	}
	m.dlg = ui.Group{}
	m.dlg.Set(ws)
	m.dlg.Focus(buttons[len(buttons)-1].ID())
}

func (m Model) openConfirm(text string, action func() tea.Cmd) (tea.Model, tea.Cmd) {
	m.confirmText, m.confirmAction = text, action
	m.openDialog(ovConfirm, btnNo, ui.NewButton(btnNo, "No", ui.Secondary), ui.NewButton(btnYes, "Yes", ui.Primary))
	return m, nil
}

func (m Model) openHelp() (tea.Model, tea.Cmd) {
	m.openDialog(ovHelp, btnClose, ui.NewButton(btnClose, "Close", ui.Primary))
	return m, nil
}

// dialogKey handles a key while a button dialog is open: enter presses the
// primary (last) button, space presses the focused one, esc presses cancel,
// and tab or the arrow keys move between the buttons. A confirmation also
// takes y and n.
func (m Model) dialogKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		return m.pressed(m.dlgButtons[len(m.dlgButtons)-1].ID())
	case "esc":
		return m.pressed(m.dlgCancel)
	case "tab", "right", "l":
		m.dlg.Next()
	case "shift+tab", "left", "h":
		m.dlg.Prev()
	case "y":
		if m.overlay == ovConfirm {
			return m.pressed(btnYes)
		}
	case "n":
		if m.overlay == ovConfirm {
			return m.pressed(btnNo)
		}
	default:
		_, cmd := m.dlg.Key(k)
		return m, cmd
	}
	return m, nil
}

// pressed runs the action of the button with id.
func (m Model) pressed(id string) (tea.Model, tea.Cmd) {
	switch id {
	case btnYes:
		m.overlay = ovNone
		return m, m.confirmAction()
	case btnNo, btnClose:
		m.overlay = ovNone
	}
	return m, nil
}

// modal renders a dialog no wider than maxW: a bold title, the body (wrapped
// when too wide), and the buttons aligned right.
func modal(title, body string, buttons []*ui.Button, focused string, maxW int) string {
	inner := max(maxW-6, 20) // the dialog's border and padding take 6 columns
	if lipgloss.Width(body) > inner {
		body = lipgloss.NewStyle().Width(inner).Render(body)
	}
	var bs []string
	for _, b := range buttons {
		bs = append(bs, b.View(b.ID() == focused, 0))
	}
	row := strings.Join(bs, "  ")
	w := max(lipgloss.Width(title), lipgloss.Width(body), lipgloss.Width(row))
	row = strings.Repeat(" ", w-lipgloss.Width(row)) + row
	return sDialog.Render(sBold.Render(title) + "\n\n" + body + "\n\n" + row)
}

func helpText() string {
	return strings.Join([]string{
		"1-4         switch page           ↑↓ / j k   move selection",
		"tab         move focus             ctrl+s     save settings",
		"h / →       focus repos / runners  p          pause/resume repo",
		"+ / -       repo cap               [ / ]      global cap",
		"m           toggle queue/all       P          pause/resume all",
		"a / d       add / remove repo      x          stop runner",
		"l           follow runner log      enter      details / copy URL",
		"pgup/pgdn   scroll                 q          quit",
		"",
		"A focused number field takes digits, so 1-4 type into it instead of switching pages.",
		"Mouse: click the sidebar, rows, buttons and footer keys; double-click a runner; wheel scrolls.",
		"Selecting terminal text needs Shift-drag (Option-drag in iTerm2).",
		"Under tmux, mouse input requires `set -g mouse on`.",
	}, "\n")
}

// buttons renders the runner detail and prompt overlays' clickable buttons:
// ok runs enter, cancel runs esc.
func buttons(ok, cancel string) string {
	b := func(id, label string) string { return zone.Mark(id, sAccent.Render("[ "+label+" ]")) }
	if ok == "" {
		return b("btn-cancel", cancel)
	}
	return b("btn-ok", ok) + "  " + b("btn-cancel", cancel)
}

// withOverlay replaces the screen with the open dialog, centred on a blank
// canvas the size of base; base itself is not drawn behind it.
func (m Model) withOverlay(base string, w int) string {
	var dialog string
	switch m.overlay {
	case ovConfirm:
		dialog = modal("Confirm", m.confirmText, m.dlgButtons, m.dlg.FocusedID(), w)
	case ovHelp:
		dialog = modal("Keys", helpText(), m.dlgButtons, m.dlg.FocusedID(), w)
	case ovPrompt:
		dialog = sDialog.Render(sBold.Render(m.promptLabel) + "\n\n" + m.prompt.View() + "\n\n" + buttons("Save", "Cancel") + "\n" +
			sDim.Render("enter save · esc cancel"))
	case ovDetail:
		dialog = sDialog.Render(m.detailBody())
	}
	return lipgloss.Place(w, lipgloss.Height(base), lipgloss.Center, lipgloss.Center, dialog,
		lipgloss.WithWhitespaceChars(" "))
}
```

In `internal/tui/view.go`, delete:

```go
// buttons renders the clickable dialog buttons: ok runs enter, cancel runs esc.
func buttons(ok, cancel string) string {
	b := func(id, label string) string { return zone.Mark(id, sAccent.Render("[ "+label+" ]")) }
	if ok == "" {
		return b("btn-cancel", cancel)
	}
	return b("btn-ok", ok) + "  " + b("btn-cancel", cancel)
}

func (m Model) withOverlay(base string, w int) string {
	var body string
	switch m.overlay {
	case ovConfirm:
		body = m.confirmText + "\n\n" + buttons("Yes", "No") + "\n" + sDim.Render("y confirm · any other key cancels")
	case ovPrompt:
		body = sBold.Render(m.promptLabel) + "\n\n" + m.prompt.View() + "\n\n" + buttons("Save", "Cancel") + "\n" +
			sDim.Render("enter save · esc cancel")
	case ovHelp:
		body = sBold.Render("Keys") + "\n\n" + strings.Join([]string{
			"1-4         switch page           ↑↓ / j k   move selection",
			"tab         move focus",
			"h / →       focus repos / runners  p          pause/resume repo",
			"+ / -       repo cap               [ / ]      global cap",
			"m           toggle queue/all       P          pause/resume all",
			"a / d       add / remove repo      x          stop runner",
			"l           follow runner log      enter      details / edit / copy URL",
			"pgup/pgdn   scroll events and log  q          quit",
			"",
			"Mouse: click tabs, rows, footer keys and dialog buttons; double-click a runner; wheel scrolls.",
			"Selecting terminal text needs Shift-drag (Option-drag in iTerm2).",
			"Under tmux, mouse input requires `set -g mouse on`.",
		}, "\n") + "\n\n" + buttons("", "Close")
	case ovDetail:
		body = m.detailBody()
	}
	dialog := sDialog.Render(body)
	return lipgloss.Place(w, lipgloss.Height(base), lipgloss.Center, lipgloss.Center, dialog,
		lipgloss.WithWhitespaceChars(" "))
}

```

In `internal/tui/model.go`, replace:

```go
	confirmAction func() tea.Cmd
	prompt        textinput.Model
```

with:

```go
	confirmAction func() tea.Cmd
	dlg           ui.Group     // focus across the open dialog's buttons
	dlgButtons    []*ui.Button // the open dialog's buttons, in display order
	dlgCancel     string       // the button esc presses
	prompt        textinput.Model
```

In `internal/tui/model.go`, replace:

```go
		return m, tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig())
	case tea.KeyMsg:
```

with:

```go
		return m, tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig())
	case ui.Pressed:
		return m.pressed(msg.ID)
	case tea.KeyMsg:
```

In `internal/tui/input.go`, replace:

```go
	case ovConfirm:
		m.overlay = ovNone
		if key == "y" || key == "enter" {
			return m, m.confirmAction()
		}
		return m, nil
	case ovDetail, ovHelp:
```

with:

```go
	case ovConfirm, ovHelp:
		return m.dialogKey(k)
	case ovDetail:
```

In `internal/tui/input.go`, replace:

```go
		m.overlay = ovHelp
```

with:

```go
		return m.openHelp()
```

In `internal/tui/input.go`, delete:

```go
func (m Model) openConfirm(text string, action func() tea.Cmd) (tea.Model, tea.Cmd) {
	m.overlay, m.confirmText, m.confirmAction = ovConfirm, text, action
	return m, nil
}

```

In `internal/tui/input.go`, replace:

```go
// dialogButtons map each overlay button zone to the key it stands for.
```

with:

```go
// dialogButtons map each detail or prompt overlay button zone to the key it stands for.
```

In `internal/tui/input.go`, replace:

```go
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.overlay != ovNone {
```

with:

```go
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.overlay == ovConfirm || m.overlay == ovHelp {
		_, cmd := m.dlg.Mouse(msg)
		return m, cmd
	}
	if m.overlay != ovNone {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run "TestConfirmModal|TestModal|TestKillBusyRunnerNeedsConfirm|TestMouseOverlayButtons|TestHelpMentionsMouseNotes"`
Expected: PASS for all six.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/dialogs.go internal/tui/input.go internal/tui/model.go internal/tui/tui_test.go internal/tui/view.go
git commit -m "feat(tui): share one modal for confirm and help"
```

### Task 14: Settings form view

**Files:**
- Create: `internal/tui/settings.go`
- Test: `internal/tui/settings_test.go`
- Modify: `internal/tui/model.go` (the `settings` field)

**Interfaces:**
- Consumes: **ui controls**, **ui form**, **ui layout**, `Group`
- Produces: **Settings page** (field keys, `settingsPage`, `load`, `settingsSections`, `settingsView`) and the **settings test helpers** (see Contracts)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

This task builds the form and renders it; the page still shows the old Config list until Task 15 wires it in, so the tests call `settingsView` directly.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/settings_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/tui/ui"
)

const settingsYAML = `owner: darkraise
mode: queue
global_max: 3
labels: [homelab]
repos:
  - name: darkcloud
    max: 2
    labels: [darkcloud-linux]
    cleanup_name_prefixes: [dc-e2e-]
  - name: darkmem
  - name: darkagents
    paused: true
`

func parseConfig(t *testing.T, y string) *config.Config {
	t.Helper()
	cfg, _, err := config.Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// settingsModel is the sample model with settingsYAML loaded into the form,
// without going through the page wiring.
func settingsModel(t *testing.T, w, h int) Model {
	t.Helper()
	m := sampleModel(&fakeClient{}, w, h)
	m.cfg = parseConfig(t, settingsYAML)
	m.settings.load(m.cfg)
	return m
}

// settingsLines renders the whole form, unscrolled, as plain lines.
func settingsLines(m Model, w int) []string {
	return strings.Split(zone.Scan(m.settingsView(w, 1000)), "\n")
}

func lineWith(lines []string, s string) int {
	for i, l := range lines {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

func TestSettingsShowsEverySettingButOwnerAsAControl(t *testing.T) {
	m := settingsModel(t, 140, 40)
	v := strings.Join(settingsLines(m, 123), "\n")
	for _, want := range []string{
		"[ queue ▾ ]", "start runners only for queued jobs", "[ − ] 3 [ + ]", "applies in queue mode",
		"Owner", "darkraise", "change in config.yaml and restart the daemon",
		"[ 10s", "[ 2m0s", "[ 5m0s", "80%", "[ 20GB", "[ 30d", "homelab ✕", "[ 6G", "[ 200%",
		"limits apply to newly started runners",
		"darkcloud", "darkcloud-linux ✕", "dc-e2e- ✕", "darkmem", "darkagents (paused)",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(v, "› [ darkraise") || strings.Contains(v, "[ darkraise") {
		t.Error("owner is editable")
	}
	lines := settingsLines(m, 123)
	cloud, mem := lineWith(lines, "─ darkcloud "), lineWith(lines, "─ darkmem ")
	if !strings.Contains(lines[cloud+1], "[ − ] 2 [ + ]") || strings.Contains(lines[cloud+1], "(default)") {
		t.Errorf("explicit max: %q", lines[cloud+1])
	}
	if !strings.Contains(lines[mem+1], "[ − ] 1 [ + ] (default)") || !strings.Contains(lines[mem+2], "[ − ] 1 [ + ] (default)") {
		t.Errorf("default max and warm: %q / %q", lines[mem+1], lines[mem+2])
	}
}

func TestSettingsRepoDefaultFollowsEditedMode(t *testing.T) {
	m := settingsModel(t, 140, 40)
	m.settings.input(setMode).SetValue(ui.Value{Text: config.ModeAll})
	lines := settingsLines(m, 123)
	mem := lineWith(lines, "─ darkmem ")
	if !strings.Contains(lines[mem+1], "[ − ] ∞ [ + ] (default)") {
		t.Fatalf("all-mode default max: %q", lines[mem+1])
	}
}

func TestSettingsRemovingRepoIsDisabled(t *testing.T) {
	m := sampleModel(&fakeClient{}, 140, 40)
	m.cfg = parseConfig(t, strings.Replace(settingsYAML, "  - name: darkmem\n", "  - name: darkmem\n    paused: true\n    removing: true\n", 1))
	m.settings.load(m.cfg)
	v := strings.Join(settingsLines(m, 123), "\n")
	if !strings.Contains(v, "darkmem (removing…)") || !strings.Contains(v, "its running jobs finish first") {
		t.Fatalf("removing repo not marked:\n%s", v)
	}
	for _, f := range []string{"max", "warm", "labels", "cleanup"} {
		if m.settings.input(repoKey("darkmem", f)).Focusable() {
			t.Errorf("darkmem %s is focusable while removing", f)
		}
	}
	if !m.settings.input(repoKey("darkcloud", "max")).Focusable() {
		t.Error("another repo was disabled")
	}
}

func TestSettingsDisabledWhileUnreachableKeepsEdits(t *testing.T) {
	m := settingsModel(t, 140, 40)
	m.settings.input(setPollInterval).SetValue(ui.Value{Text: "30s"})
	m.connected = false
	m.settingsView(123, 40)
	for _, f := range m.settings.form.Fields() {
		if f.Input.Focusable() {
			t.Fatalf("%s is focusable while unreachable", f.Key)
		}
	}
	if m.settings.group.Focused() != nil {
		t.Fatal("something has focus while unreachable")
	}
	m.connected = true
	m.settingsView(123, 40)
	if got := m.settings.input(setPollInterval).Value().Text; got != "30s" || m.settings.group.FocusedID() != setMode {
		t.Fatalf("after reconnect: poll %q focus %q", got, m.settings.group.FocusedID())
	}
}

func TestSettingsNarrowPutsDescriptionsBelow(t *testing.T) {
	m := settingsModel(t, 80, 40)
	lines := settingsLines(m, 80)
	i := lineWith(lines, "[ queue ▾ ]")
	if strings.Contains(lines[i], "start runners") || !strings.Contains(lines[i+1], "start runners only") {
		t.Fatalf("narrow description placement:\n%s\n%s", lines[i], lines[i+1])
	}
}

// Config text is rendered through clean, like any other daemon text.
func TestSettingsSanitisesConfigText(t *testing.T) {
	m := sampleModel(&fakeClient{}, 140, 40)
	// YAML's \e and \a escapes put real ESC and BEL bytes into the parsed config.
	m.cfg = parseConfig(t, `owner: "evil\e]0;pwned\a"
labels: ["bad\e[2Jlabel"]
repos:
  - name: darkmem
    cleanup_name_prefixes: ["x\e]52;c;Zm9v\ay"]
`)
	m.settings.load(m.cfg)
	v := strings.Join(settingsLines(m, 123), "\n")
	if strings.ContainsAny(v, "\x1b\a") || !strings.Contains(v, "evil") || !strings.Contains(v, "badlabel") || !strings.Contains(v, "xy") {
		t.Fatalf("config text not sanitised: %q", v)
	}
}

func TestSettingsViewClampsScroll(t *testing.T) {
	m := settingsModel(t, 140, 40)
	all := settingsLines(m, 123)
	m.settings.scroll = 999
	v := strings.Split(zone.Scan(m.settingsView(123, 10)), "\n")
	if m.settings.scroll != len(all)-10 || v[9] != all[len(all)-1] {
		t.Fatalf("scroll %d of %d lines; last line %q", m.settings.scroll, len(all), v[9])
	}
	m.settings.scroll = -5
	m.settingsView(123, 10)
	if m.settings.scroll != 0 {
		t.Fatalf("negative scroll kept: %d", m.settings.scroll)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `m.settings undefined`, `undefined: setMode` and `undefined: repoKey`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/settings.go`:

```go
package tui

import (
	"strings"

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
```

In `internal/tui/model.go`, replace:

```go
	st      model.Status
	epoch   string
	events  []model.Event
	lastSeq int64
	hist    []model.HistoryEntry
	cfg     *config.Config
```

with:

```go
	st       model.Status
	epoch    string
	settings *settingsPage
	events   []model.Event
	lastSeq  int64
	hist     []model.HistoryEntry
	cfg      *config.Config
```

In `internal/tui/model.go`, replace:

```go
		c: c, now: time.Now, width: 120, height: 40, prompt: ti,
```

with:

```go
		c: c, now: time.Now, width: 120, height: 40, prompt: ti, settings: newSettingsPage(),
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run TestSettings`
Expected: PASS for the seven `TestSettings…` tests.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/settings.go internal/tui/settings_test.go
git commit -m "feat(tui): render the settings form"
```

### Task 15: Settings page replaces the Config tab

**Files:**
- Modify: `internal/tui/input.go` (remove the Config tab code)
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/view.go`
- Test: `internal/tui/tui_test.go`
- Create (generated): `internal/tui/testdata/TestSettingsGolden/120.golden`, `.../80.golden`

**Interfaces:**
- Consumes: **Settings page**
- Produces: Settings as the fourth page (see Contracts, **TUI pages**)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

In `internal/tui/tui_test.go`, delete:

```go
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

```

In `internal/tui/tui_test.go`, replace:

```go
func TestConfigTabRetriesLoad(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.page = pageSettings
	c.cfg = sampleConfig(t, "darkcloud")
	m = ticks(m, 1)
	v := m.View()
	if !strings.Contains(v, "darkcloud.max") || strings.Contains(v, "loading…") {
		t.Fatalf("config not loaded by the tick:\n%s", v)
```

with:

```go
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
	if !strings.Contains(v, "─ darkcloud ") || strings.Contains(v, "loading…") {
		t.Fatalf("config not loaded by the tick:\n%s", v)
	}
}

func TestSettingsGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			c := &fakeClient{cfg: parseConfig(t, settingsYAML)}
			m := feed(sampleModel(c, w, 30), key("4"))
			golden.RequireEqual(t, []byte(m.View()))
		})
```

In `internal/tui/tui_test.go`, replace:

```go
	t.Run("config", func(t *testing.T) {
		upd, _ := newModel(&fakeClient{}, 120, h, st).Update(configMsg(sampleConfig(t, repos[:6]...)))
		check(t, run(t, upd.(Model), down("4", 27)...), "repo05.cleanup_name_prefixes")
```

with:

```go
	t.Run("settings", func(t *testing.T) {
		upd, _ := newModel(&fakeClient{}, 120, h, st).Update(configMsg(sampleConfig(t, repos[:6]...)))
		check(t, run(t, upd.(Model), "4"), "[ queue ▾ ]")
```

In `internal/tui/tui_test.go`, replace:

```go
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
```

with:

```go
	}
	m = run(t, m, "x")
	m = click(t, m, btnNo)
	if m.overlay != ovNone || len(c.actions()) != 1 {
		t.Fatalf("No: overlay %v calls %v", m.overlay, c.actions())
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL: `TestSettingsRetriesLoad` (`config not loaded by the tick`) and `TestSettingsGolden` (no snapshot file yet).

- [ ] **Step 3: Write the implementation**

In `internal/tui/input.go`, delete:

```go
	"strconv"
```

In `internal/tui/input.go`, replace:

```go
		m.cfgSel = clamp(m.cfgSel+d, len(m.configFields()))
```

with:

```go
		return nil
```

In `internal/tui/input.go`, delete:

```go
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
```

In `internal/tui/input.go`, delete:

```go
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

// configFields lists the editable settings of the Settings page.
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
```

In `internal/tui/input.go`, delete:

```go
	if i := hit("cfg", len(m.configFields())); i >= 0 {
		m.cfgSel = i
		return m.enter()
	}
```

In `internal/tui/model.go`, replace:

```go
	repoSel, runnerSel, histSel, cfgSel int
	eventScroll, logScroll              int
	histRepo, histConcl                 string
```

with:

```go
	repoSel, runnerSel, histSel int
	eventScroll, logScroll      int
	histRepo, histConcl         string
```

In `internal/tui/model.go`, replace:

```go
		m.cfg = msg
		m.clampSelections()
		return m, nil
```

with:

```go
		m.cfg = msg
		m.settings.load(msg)
		return m, nil
```

In `internal/tui/model.go`, delete:

```go
	m.cfgSel = clamp(m.cfgSel, len(m.configFields()))
```

In `internal/tui/model.go`, replace:

```go
}

// configField is one editable row of the Config tab.
type configField struct {
	label string
	value string
	apply func(v string) (model.ConfigPatch, error)
}
```

with:

```go
}
```

In `internal/tui/view.go`, replace:

```go
		return m.configTab(w, h)
```

with:

```go
		return m.settingsView(w, h)
```

In `internal/tui/view.go`, delete:

```go
func (m Model) configTab(w, h int) string {
	inner := w - 4
	fields := m.configFields()
	lines := []string{sDim.Render("enter edits the selected setting; the daemon validates before saving")}
	if fields == nil {
		lines = append(lines, sDim.Render("loading…"))
	}
	for i, f := range fields {
		line := "  " + cell(f.label, 36) + " " + f.value
		lines = append(lines, m.row(fmt.Sprintf("cfg-%d", i), i == m.cfgSel, line, inner))
	}
	return box("Config", w, fit(lines, 1, m.cfgSel, h-2))
}

```

- [ ] **Step 4: Regenerate the snapshots**

Run: `go test ./internal/tui/ -run TestSettingsGolden -update -count=1 -timeout 180s`
Expected: the command exits 0 and writes the two Settings snapshots. Open each file and compare it with the snapshot below (trailing spaces are trimmed here; the files keep them).

`internal/tui/testdata/TestSettingsGolden/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
  1 Dashboard    Settings
  2 Runners      ╭─ General ───────────────────────────────────────────────────────────────────────────────────────────╮
  3 History      │ Mode               › [ queue ▾ ]  start runners only for queued jobs, up to the global max          │
▌ 4 Settings     │ Global max           [ − ] 3 [ + ]  applies in queue mode                                           │
                 │ Owner                darkraise  change in config.yaml and restart the daemon                        │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Timing ────────────────────────────────────────────────────────────────────────────────────────────╮
                 │ Poll interval        [ 10s        ]  how often GitHub is checked (at least 5s)                      │
                 │ Start timeout        [ 2m0s       ]  a runner not online by then is replaced                        │
                 │ Idle timeout         [ 5m0s       ]  idle runners beyond warm stop after this                       │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Disk and retention ────────────────────────────────────────────────────────────────────────────────╮
                 │ Disk high-water      [ − ] 80% [ + ]  disk use that triggers pruning                                │
                 │ Build cache keep     [ 20GB       ]  build cache kept when pruning, e.g. 20GB                       │
                 │ History retention    [ 30d        ]  history and logs older than this are removed (at least 1d)     │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Runner defaults ───────────────────────────────────────────────────────────────────────────────────╮
                 │ limits apply to newly started runners                                                               │
                 │ Global labels        homelab ✕  + add  added to every runner                                        │
                 │ Memory max           [ 6G         ]  per runner, e.g. 6G, 50% or infinity                           │
                 │ CPU quota            [ 200%       ]  per runner, e.g. 200%                                          │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ darkcloud ─────────────────────────────────────────────────────────────────────────────────────────╮
                 │ Max                  [ − ] 2 [ + ]  once set, it stays explicit                                     │
                 │ Warm                 [ − ] 1 [ + ] (default)  applies in all mode                                   │
                 │ Labels               darkcloud-linux ✕  + add  added to this repo's runners                         │
  ? Help         │ Cleanup prefixes     dc-e2e- ✕  + add  container name prefixes removed after each job               │
  q Quit         ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
 tab next  shift+tab previous  ctrl+s save  ? help  q quit
```

`internal/tui/testdata/TestSettingsGolden/80.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api 4800   disk ▕██████░░░░▏ 61%   ●
  1 Dashboard    2 Runners    3 History  [ 4 Settings ]                  ? help
Settings
╭─ General ────────────────────────────────────────────────────────────────────╮
│ Mode               › [ queue ▾ ]                                             │
│                      start runners only for queued jobs, up to the global m… │
│ Global max           [ − ] 3 [ + ]                                           │
│                      applies in queue mode                                   │
│ Owner                darkraise                                               │
│                      change in config.yaml and restart the daemon            │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Timing ─────────────────────────────────────────────────────────────────────╮
│ Poll interval        [ 10s        ]                                          │
│                      how often GitHub is checked (at least 5s)               │
│ Start timeout        [ 2m0s       ]                                          │
│                      a runner not online by then is replaced                 │
│ Idle timeout         [ 5m0s       ]                                          │
│                      idle runners beyond warm stop after this                │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Disk and retention ─────────────────────────────────────────────────────────╮
│ Disk high-water      [ − ] 80% [ + ]                                         │
│                      disk use that triggers pruning                          │
│ Build cache keep     [ 20GB       ]                                          │
│                      build cache kept when pruning, e.g. 20GB                │
│ History retention    [ 30d        ]                                          │
│                      history and logs older than this are removed (at least… │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Runner defaults ────────────────────────────────────────────────────────────╮
│ limits apply to newly started runners                                        │
 tab next  shift+tab previous  ctrl+s save  ? help  q quit
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: `ok`.

- [ ] **Step 6: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/input.go internal/tui/model.go internal/tui/testdata/TestSettingsGolden/120.golden internal/tui/testdata/TestSettingsGolden/80.golden internal/tui/tui_test.go internal/tui/view.go
git commit -m "feat(tui): replace the config tab with settings"
```

### Task 16: Settings keys, mouse and scrolling

**Files:**
- Modify: `internal/tui/settings.go`
- Modify: `internal/tui/shell.go` (`layout`; add `contentSize`)
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/input.go`
- Test: `internal/tui/settings_test.go`
- Test: `internal/tui/tui_test.go` (more keys in `key`)

**Interfaces:**
- Consumes: **Settings page**, `Group`, `ScrollTo`, **TUI shell**
- Produces: `settingsKey`, `settingsMouse`, `advance`, `settingsBodyH`, `scrollToFocus`, `settingsFooterKeys`, `contentSize`, `footerPress` (see Contracts)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

Key precedence from spec §1: an open dialog, then the focused control (an open dropdown or an edited text field takes everything it should), then the page keys, then the global keys. `ctrl+c` quits from anywhere. The footer follows the focused control, and a click on a global hint runs it directly instead of typing it into a field.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/settings_test.go`, replace:

```go
	"testing"

	zone "github.com/lrstanley/bubblezone"

```

with:

```go
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

```

In `internal/tui/settings_test.go`, replace:

```go
		t.Fatalf("negative scroll kept: %d", m.settings.scroll)
	}
}
```

with:

```go
		t.Fatalf("negative scroll kept: %d", m.settings.scroll)
	}
}

// onSettings is the sample model on the Settings page with settingsYAML loaded.
func onSettings(t *testing.T, c *fakeClient, w, h int) Model {
	t.Helper()
	c.cfg = parseConfig(t, settingsYAML)
	return feed(sampleModel(c, w, h), key("4"))
}

func quits(cmd tea.Cmd) bool {
	for _, msg := range collect(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

func TestSettingsFocusOrderScrollsIntoView(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 22)
	m.View()
	if got := m.settings.group.FocusedID(); got != setMode {
		t.Fatalf("initial focus %q", got)
	}
	m = feed(m, keys("j", "tab", "down")...) // j moves focus off a select; a text field would take it
	if got := m.settings.group.FocusedID(); got != setStartTimeout {
		t.Fatalf("after j, tab, down: %q", got)
	}
	m = feed(m, keys("up", "shift+tab", "k")...)
	if got := m.settings.group.FocusedID(); got != setMode {
		t.Fatalf("after up, shift+tab, k: %q", got)
	}
	m = feed(m, key("shift+tab")) // wraps to the last control
	if got := m.settings.group.FocusedID(); got != repoKey("darkagents", "cleanup") {
		t.Fatalf("wrap: %q", got)
	}
	v := m.View()
	if !strings.Contains(v, "Cleanup prefixes   › + add") || lipgloss.Height(v) > 22 || m.settings.scroll == 0 {
		t.Fatalf("last control not scrolled into view (scroll %d):\n%s", m.settings.scroll, v)
	}
	m = feed(m, key("tab"))
	if v := m.View(); !strings.Contains(v, "Mode               › [ queue ▾ ]") || m.settings.scroll != 0 {
		t.Fatalf("first control not scrolled back into view:\n%s", v)
	}
}

// A focused stepper takes digits, so they do not switch pages; a focused
// text field takes letters, so q types instead of quitting.
func TestSettingsKeyPrecedence(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	m = feed(m, keys("tab", "2")...)
	if m.page != pageSettings || m.settings.input(setGlobalMax).Value().Num != 2 {
		t.Fatalf("digit: page %v global max %+v", m.page, m.settings.input(setGlobalMax).Value())
	}
	m = feed(m, keys("tab", "q")...)
	f := m.settings.input(setPollInterval).(*ui.TextField)
	if !f.Editing() || f.Value().Text != "q" {
		t.Fatalf("letter: editing %v value %q", f.Editing(), f.Value().Text)
	}
	upd, cmd := m.Update(key("q"))
	if m = upd.(Model); quits(cmd) || f.Value().Text != "qq" {
		t.Fatal("q quit while a text field was being edited")
	}
	m = feed(m, keys("esc", "esc")...) // stop editing; then nothing to back out of
	if f.Editing() || m.page != pageSettings || f.Value().Text != "qq" {
		t.Fatalf("esc: editing %v page %v value %q", f.Editing(), m.page, f.Value().Text)
	}
	for _, k := range []string{"m", "]", "+", "-"} {
		m = feed(m, key(k))
	}
	if len(c.actions()) != 0 {
		t.Fatalf("config keys acted on the Settings page: %v", c.actions())
	}
}

func TestSettingsSelectOpensInlineAndHoldsKeys(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 30)
	m = feed(m, key("enter"))
	v := m.View()
	if !strings.Contains(v, "▸ queue  start runners only") || !strings.Contains(v, "all  keep warm runners") {
		t.Fatalf("dropdown not open inline:\n%s", v)
	}
	m = feed(m, keys("1", "q", "down", "enter")...)
	if m.page != pageSettings || m.settings.input(setMode).Value().Text != config.ModeAll {
		t.Fatalf("page %v mode %q", m.page, m.settings.input(setMode).Value().Text)
	}
	if v := m.View(); !strings.Contains(v, "Mode               › [ all ▾ ] ●") {
		t.Fatalf("picked mode not marked dirty:\n%s", v)
	}
}

func TestSettingsTextFieldEnterAdvances(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 30)
	m.View()
	m.settings.group.Focus(setPollInterval)
	m = feed(m, keys("enter", "backspace", "backspace", "backspace", "1", "5", "s", "enter")...)
	if got := m.settings.input(setPollInterval).Value().Text; got != "15s" {
		t.Fatalf("value %q", got)
	}
	if got := m.settings.group.FocusedID(); got != setStartTimeout {
		t.Fatalf("enter did not advance focus: %q", got)
	}
}

func TestSettingsMouse(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 30)
	m = click(t, m, setPollInterval)
	if f := m.settings.input(setPollInterval).(*ui.TextField); m.settings.group.FocusedID() != setPollInterval || !f.Editing() {
		t.Fatal("click did not focus and edit the field")
	}
	m = click(t, m, setMode)
	if !m.settings.input(setMode).(*ui.Select).Open() {
		t.Fatal("click did not open the select")
	}
	m = click(t, m, "nav/history") // swallowed by the open dropdown
	if m.page != pageSettings || m.settings.input(setMode).(*ui.Select).Open() {
		t.Fatalf("outside click: page %v", m.page)
	}
	z := zoneOf(t, m, "settings/body")
	m = feed(m, tea.MouseMsg{X: z.StartX + 2, Y: z.StartY + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.settings.scroll != 3 {
		t.Fatalf("wheel: scroll %d", m.settings.scroll)
	}
	m = feed(m, key("pgup"))
	if m.settings.scroll != 0 {
		t.Fatalf("pgup: scroll %d", m.settings.scroll)
	}
}

func TestCtrlCQuitsFromAnywhere(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	m.focus = paneRunners
	m = run(t, m, "x") // a confirmation is open
	if _, cmd := m.Update(key("ctrl+c")); !quits(cmd) {
		t.Fatal("ctrl+c did not quit from a dialog")
	}
	m = onSettings(t, &fakeClient{}, 120, 30)
	m = feed(m, keys("tab", "tab", "enter")...) // editing a text field
	if _, cmd := m.Update(key("ctrl+c")); !quits(cmd) {
		t.Fatal("ctrl+c did not quit while editing")
	}
}

// The footer follows the focused control, and a click on a global hint runs
// the key directly instead of typing it into a focused text field.
func TestSettingsFooterFollowsFocus(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 30)
	m.View()
	m.settings.group.Focus(setPollInterval)
	m = feed(m, key("enter"))
	if v := m.View(); !strings.Contains(v, "enter commit") || !strings.Contains(v, "esc stop editing") || strings.Contains(v, "q quit") {
		t.Fatalf("editing footer:\n%s", v)
	}
	m = click(t, m, "key-esc")
	f := m.settings.input(setPollInterval).(*ui.TextField)
	if f.Editing() {
		t.Fatal("the esc hint did not stop editing")
	}
	z := zoneOf(t, m, "key-q")
	upd, cmd := m.Update(leftClick(z.StartX, z.StartY))
	if !quits(cmd) || upd.(Model).settings.input(setPollInterval).Value().Text != "10s" {
		t.Fatal("the q hint typed into the field instead of quitting")
	}
	m.settings.group.Focus(setGlobalMax)
	if v := m.View(); !strings.Contains(v, "left less") || !strings.Contains(v, "right more") {
		t.Fatalf("stepper footer:\n%s", v)
	}
}

// Before the config loads, the config keys still do nothing on Settings.
func TestSettingsConfigKeysInactiveWhileLoading(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.page = pageSettings // no config fetched yet
	for _, k := range []string{"m", "]", "["} {
		m = feed(m, key(k))
	}
	if m.cfg != nil || len(c.actions()) != 0 {
		t.Fatalf("config keys acted while loading: %v", c.actions())
	}
}
```

In `internal/tui/tui_test.go`, replace:

```go
		return tea.KeyMsg{Type: tea.KeyTab}
	}
```

with:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL: `TestSettingsFocusOrderScrollsIntoView`, `TestSettingsKeyPrecedence`, `TestSettingsSelectOpensInlineAndHoldsKeys`, `TestSettingsTextFieldEnterAdvances`, `TestSettingsMouse`, `TestCtrlCQuitsFromAnywhere`, `TestSettingsFooterFollowsFocus` and `TestSettingsConfigKeysInactiveWhileLoading`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/settings.go`, replace:

```go
	"strings"

	zone "github.com/lrstanley/bubblezone"

```

with:

```go
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

```

In `internal/tui/settings.go`, replace:

```go
}

// settingsView renders the form scrolled to the page's offset in w columns and h lines.
func (m Model) settingsView(w, h int) string {
```

with:

```go
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
```

In `internal/tui/shell.go`, replace:

```go
		return []footerKey{{"tab", "next"}, {"shift+tab", "previous"}, {"ctrl+s", "save"}, {"?", "help"}, {"q", "quit"}}
```

with:

```go
		return m.settingsFooterKeys()
```

In `internal/tui/shell.go`, replace:

```go
// keyMsg is the KeyMsg for a footer hint, so a click runs the same path as the key.
```

with:

```go
// footerPress runs a clicked footer hint. Navigation and control keys take
// the same path as the key; the global letter keys run directly, so a click
// on "q quit" never types a q into a focused text field.
func (m Model) footerPress(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "tab", "shift+tab", "ctrl+s", "enter", "esc", "up", "left", "right":
		return m.handleKey(keyMsg(k))
	}
	return m.press(k)
}

// keyMsg is the KeyMsg for a footer hint.
```

In `internal/tui/shell.go`, replace:

```go
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
```

with:

```go
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
```

In `internal/tui/shell.go`, replace:

```go
	bodyH := max(m.height-len(top)-1, 7) // the footer takes the last line
	cw := w
	if wide {
		cw = w - sidebarWidth - 1
	}
	content := m.pageHeader(cw) + "\n" + body(cw, bodyH-1)
	if wide {
		content = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(bodyH), " ", content)
	}
	return strings.Join(append(top, content, m.footer(w)), "\n")
}
```

with:

```go
	cw, ch := m.contentSize()
	content := m.pageHeader(cw) + "\n" + body(cw, ch)
	if wide {
		content = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(ch+1), " ", content)
	}
	return strings.Join(append(top, content, m.footer(w)), "\n")
}

// contentSize is the width and height layout gives the current page's body:
// what is left after the top bar, the alert and toast lines, the tab row (or
// the sidebar), the page header and the footer.
func (m Model) contentSize() (int, int) {
	w := max(m.width, 40)
	rows := 2 // the top bar and the footer
	if m.alertLine(w) != "" {
		rows++
	}
	if m.toast.Active() {
		rows++
	}
	cw := w
	if w >= wideMin {
		cw = w - sidebarWidth - 1
	} else {
		rows++
	}
	return cw, max(m.height-rows, 7) - 1
}
```

In `internal/tui/model.go`, replace:

```go
		return m.pressed(msg.ID)
	case tea.KeyMsg:
```

with:

```go
		return m.pressed(msg.ID)
	case ui.Advance:
		return m.advance(msg.ID)
	case tea.KeyMsg:
```

In `internal/tui/input.go`, replace:

```go
	key := k.String()
	switch m.overlay {
```

with:

```go
	key := k.String()
	if key == "ctrl+c" {
		// The terminal's emergency exit: nothing guards it.
		return m, tea.Quit
	}
	switch m.overlay {
```

In `internal/tui/input.go`, replace:

```go
		}
		return m, nil
	}
	return m.press(key)
```

with:

```go
		}
		return m, nil
	}
	if m.page == pageSettings {
		if ok, mm, cmd := m.settingsKey(k); ok {
			return mm, cmd
		}
	}
	return m.press(key)
```

In `internal/tui/input.go`, replace:

```go
	}
	if m.overlay != ovNone {
```

with:

```go
	}
	if m.overlay == ovNone && m.page == pageSettings {
		// First, so an open dropdown can swallow a click anywhere, the sidebar included.
		if ok, mm, cmd := m.settingsMouse(msg); ok {
			return mm, cmd
		}
	}
	if m.overlay != ovNone {
```

In `internal/tui/input.go`, replace:

```go
			return m.handleKey(keyMsg(f.key))
```

with:

```go
			return m.footerPress(f.key)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: `ok`.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/input.go internal/tui/model.go internal/tui/settings.go internal/tui/settings_test.go internal/tui/shell.go internal/tui/tui_test.go
git commit -m "feat(tui): route keys and mouse on settings"
```

### Task 17: Save and Discard

**Files:**
- Modify: `internal/tui/settings.go`
- Modify: `internal/tui/dialogs.go` (`pressed`)
- Modify: `internal/tui/model.go` (`Update`)
- Test: `internal/tui/settings_test.go`
- Test: `internal/tui/tui_test.go` (`fakeClient` records patches)

**Interfaces:**
- Consumes: **Settings page**, `Form`, `Equal`, `Button`
- Produces: **Settings save** (see Contracts)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

In `internal/tui/settings_test.go`, replace:

```go
import (
	"strings"
```

with:

```go
import (
	"errors"
	"strings"
```

In `internal/tui/settings_test.go`, replace:

```go
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/tui/ui"
```

with:

```go
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
```

In `internal/tui/settings_test.go`, replace:

```go
		t.Fatalf("config keys acted while loading: %v", c.actions())
	}
}
```

with:

```go
		t.Fatalf("config keys acted while loading: %v", c.actions())
	}
}

func set(m Model, key string, v ui.Value) { m.settings.input(key).SetValue(v) }

func TestSettingsSavePatchHoldsOnlyDirtyFields(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	set(m, setLabels, ui.Value{List: []string{"homelab", "gpu"}})
	set(m, setMemoryMax, ui.Value{Text: "4G"})
	set(m, repoKey("darkmem", "max"), ui.Value{Num: 2, Set: true})
	set(m, repoKey("darkcloud", "cleanup"), ui.Value{List: []string{}})
	if v := m.View(); !strings.Contains(v, "● 5 unsaved changes") || !strings.Contains(v, "( Discard )") || !strings.Contains(v, "[ Save changes ]") {
		t.Fatalf("unsaved bar missing:\n%s", v)
	}
	m = feed(m, key("ctrl+s"))
	if len(c.patches) != 1 {
		t.Fatalf("patches %d", len(c.patches))
	}
	p := c.patches[0]
	if *p.PollInterval != "30s" || strings.Join(*p.Labels, ",") != "homelab,gpu" || *p.RunnerLimits.MemoryMax != "4G" ||
		p.RunnerLimits.CPUQuota != nil || *p.Repos["darkmem"].Max != 2 || len(*p.Repos["darkcloud"].CleanupNamePrefixes) != 0 {
		t.Fatalf("patch %+v", p)
	}
	if p.Mode != nil || p.GlobalMax != nil || p.StartTimeout != nil || p.IdleTimeout != nil || p.HistoryRetention != nil ||
		p.DiskHighWater != nil || p.BuildCacheKeep != nil || p.Repos["darkmem"].Warm != nil || p.Repos["darkcloud"].Max != nil || len(p.Repos) != 2 {
		t.Fatalf("patch carries clean fields: %+v", p)
	}
}

// A saved 120s comes back as 2m0s: the field resets to the new base and is
// not dirty, and no mismatch is reported.
func TestSettingsSavedDurationIsNotDirtyAfterRefetch(t *testing.T) {
	c := &fakeClient{}
	c.onPatch = func(p model.ConfigPatch) {
		if p.PollInterval != nil {
			d, _ := config.ParseDuration(*p.PollInterval)
			c.cfg.PollInterval = d
		}
	}
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "120s"})
	m = click(t, m, setSave)
	f := m.settings.form.Field(setPollInterval)
	if f.Dirty() || f.Input.Value().Text != "2m0s" {
		t.Fatalf("after save: dirty %v value %q", f.Dirty(), f.Input.Value().Text)
	}
	v := m.View()
	if !strings.Contains(v, "✔ Settings saved") || strings.Contains(v, "unsaved change") || strings.Contains(v, "did not apply") {
		t.Fatalf("after save:\n%s", v)
	}
}

func TestSettingsRejectedSaveKeepsEditsAndShowsAlert(t *testing.T) {
	c := &fakeClient{patchErr: errors.New("build_cache_keep must look like 20GB; runner_limits.cpu_quota must be a positive percentage such as 200%")}
	m := onSettings(t, c, 120, 30)
	set(m, setBuildCacheKeep, ui.Value{Text: "lots"})
	set(m, setCPUQuota, ui.Value{Text: "fast"})
	m = feed(m, key("ctrl+s"))
	v := m.View()
	for _, want := range []string{"Save rejected", "✖ build_cache_keep must look like 20GB", "✖ runner_limits.cpu_quota must be a positive",
		"✖ settings not saved", "● 2 unsaved changes"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if m.settings.input(setBuildCacheKeep).Value().Text != "lots" || m.settings.saving {
		t.Fatal("rejected save lost the edit or stayed saving")
	}
	m = click(t, m, setDiscard)
	if v := m.View(); strings.Contains(v, "Save rejected") || strings.Contains(v, "unsaved change") {
		t.Fatalf("discard left the alert or edits:\n%s", v)
	}
}

func TestSettingsSaveDisabledWhileInFlight(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	upd, cmd := m.Update(key("ctrl+s"))
	m = upd.(Model)
	if cmd == nil || !m.settings.saving || !strings.Contains(m.View(), "[ Saving… ]") {
		t.Fatal("save did not enter the saving state")
	}
	if _, again := m.Update(key("ctrl+s")); again != nil {
		t.Fatal("a second save was sent while one was in flight")
	}
	m = feed(m, collect(cmd)...)
	if m.settings.saving || len(c.patches) != 1 {
		t.Fatalf("saving %v patches %d", m.settings.saving, len(c.patches))
	}
}

func TestSettingsWarnsWhenDaemonIgnoresAField(t *testing.T) {
	c := &fakeClient{} // accepts the patch but applies nothing, like an older daemon
	m := onSettings(t, c, 120, 30)
	set(m, setHistoryRetention, ui.Value{Text: "14d"})
	m = feed(m, key("ctrl+s"))
	if v := m.View(); !strings.Contains(v, "daemon did not apply history_retention; is it older than this ghr?") {
		t.Fatalf("no mismatch warning:\n%s", v)
	}
}

func TestSettingsInAppChecksBlockSave(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, repoKey("darkcloud", "warm"), ui.Value{Num: 3, Set: true}) // darkcloud has max: 2
	m = feed(m, key("ctrl+s"))
	v := m.View()
	if len(c.patches) != 0 || !strings.Contains(v, "✖ warm must be <= max") || !strings.Contains(v, "fix the highlighted settings first") {
		t.Fatalf("patches %d:\n%s", len(c.patches), v)
	}
	set(m, repoKey("darkcloud", "warm"), ui.Value{Num: 2, Set: true})
	set(m, setIdleTimeout, ui.Value{Text: "soon"})
	if m = feed(m, key("ctrl+s")); len(c.patches) != 0 {
		t.Fatal("an unparseable duration was sent")
	}
}

// The save stays busy until the config fetched after it is handled.
func TestSettingsStaysBusyUntilRefetch(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	upd, cmd := m.Update(key("ctrl+s"))
	saved := collect(cmd) // the patch's outcome only
	upd, refetch := upd.Update(saved[0])
	m = upd.(Model)
	if !m.settings.saving || !strings.Contains(m.View(), "[ Saving… ]") {
		t.Fatal("not busy while the refetch is pending")
	}
	if _, again := m.Update(key("ctrl+s")); again != nil {
		t.Fatal("a second save overlapped the refetch")
	}
	m = feed(m, collect(refetch)...)
	if m.settings.saving || len(c.patches) != 1 || m.settings.form.Field(setPollInterval).Dirty() {
		t.Fatalf("after the refetch: saving %v patches %d", m.settings.saving, len(c.patches))
	}
}

func TestSettingsRefetchFailureIsReported(t *testing.T) {
	c := &fakeClient{}
	c.onPatch = func(model.ConfigPatch) { c.cfgErr = errors.New("connection refused") }
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	m = feed(m, key("ctrl+s"))
	if v := m.View(); m.settings.saving || !strings.Contains(v, "saved, but re-reading the config failed: connection refused") {
		t.Fatalf("saving %v:\n%s", m.settings.saving, v)
	}
}

// Many rejection messages cannot push the form and footer off a 22-row screen.
func TestSettingsAlertIsBounded(t *testing.T) {
	var msgs []string
	for i := 0; i < 6; i++ {
		msgs = append(msgs, "repo"+string(rune('0'+i))+": needs at least one label in labels or repo labels")
	}
	c := &fakeClient{patchErr: errors.New(strings.Join(msgs, "; "))}
	m := onSettings(t, c, 120, 22)
	set(m, setLabels, ui.Value{List: []string{}})
	m = feed(m, key("ctrl+s"))
	v := m.View()
	if !strings.Contains(v, "repo2: needs") || strings.Contains(v, "repo3: needs") || !strings.Contains(v, "… and 3 more") ||
		lipgloss.Height(v) > 22 || !strings.Contains(v, "q quit") {
		t.Fatalf("alert not bounded (%d lines):\n%s", lipgloss.Height(v), v)
	}
}
```

In `internal/tui/tui_test.go`, replace:

```go
	calls  []string
	st     *model.Status
	events []model.Event
	steps  []model.Step
	ctrs   []model.Container
	cfg    *config.Config
```

with:

```go
	calls    []string
	st       *model.Status
	events   []model.Event
	steps    []model.Step
	ctrs     []model.Container
	cfg      *config.Config
	patches  []model.ConfigPatch
	patchErr error                   // returned by PatchConfig when set
	cfgErr   error                   // returned by Config when set
	onPatch  func(model.ConfigPatch) // applies a patch to cfg, as a daemon would
```

In `internal/tui/tui_test.go`, replace:

```go
func (f *fakeClient) Config(_ context.Context, out any) error {
	if f.cfg != nil {
```

with:

```go
func (f *fakeClient) Config(_ context.Context, out any) error {
	if f.cfgErr != nil {
		return f.cfgErr
	}
	if f.cfg != nil {
```

In `internal/tui/tui_test.go`, replace:

```go
func (f *fakeClient) PatchConfig(_ context.Context, p model.ConfigPatch) error {
	switch {
```

with:

```go
func (f *fakeClient) PatchConfig(_ context.Context, p model.ConfigPatch) error {
	f.patches = append(f.patches, p)
	if f.patchErr != nil {
		return f.patchErr
	}
	if f.onPatch != nil {
		f.onPatch(p)
	}
	switch {
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: setSave` and `undefined: setDiscard`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/settings.go`, replace:

```go
import (
	"strings"
```

with:

```go
import (
	"fmt"
	"strings"
```

In `internal/tui/settings.go`, replace:

```go
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/tui/ui"
```

with:

```go
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
```

In `internal/tui/settings.go`, replace:

```go
	setCPUQuota         = "settings/cpu_quota"
)
```

with:

```go
	setCPUQuota         = "settings/cpu_quota"

	setSave    = "settings/save"
	setDiscard = "settings/discard"
)
```

In `internal/tui/settings.go`, replace:

```go
}

func newSettingsPage() *settingsPage { return &settingsPage{} }
```

with:

```go
	saving bool
	alert  []string // the daemon's messages from a rejected save

	save, discard *ui.Button
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
		cfg  *config.Config
		sent map[string]ui.Value
		err  error
	}
)
```

In `internal/tui/settings.go`, replace:

```go
}

// settingsSections lays out the form as cards. It first brings the controls
// up to date with the rest of the model (disabled while the daemon is
```

with:

```go
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
		s.saving = false
		s.alert = strings.Split(clean(msg.err.Error()), "; ")
		m.toast.Show("settings not saved", true, m.now())
		return m, nil
	}
	m.toast.Show("Settings saved", false, m.now())
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		var cfg config.Config
		if err := c.Config(cx, &cfg); err != nil {
			return refetchedMsg{sent: msg.sent, err: err}
		}
		return refetchedMsg{cfg: &cfg, sent: msg.sent}
	}
}

// refetched merges the config fetched after a save. Every saved field resets
// to its new base (a reset, not a merge), and a field whose new base differs
// from what was sent means the daemon ignored it.
//
// If the refetch fails, the save stands but cannot be checked: the edits stay
// as typed, a toast says so, and the next periodic refresh brings the form
// up to date.
func (m Model) refetched(msg refetchedMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	s.saving = false
	if msg.err != nil {
		m.toast.Show("saved, but re-reading the config failed: "+clean(msg.err.Error()), true, m.now())
		return m, nil
	}
	m.cfg = msg.cfg
	s.load(msg.cfg)
	var keys []string
	for k := range msg.sent {
		keys = append(keys, k)
	}
	s.form.Reset(keys)
	for _, f := range s.form.Fields() {
		if v, ok := msg.sent[f.Key]; ok && !ui.Equal(f.Kind, f.Base, v) {
			m.toast.Show("daemon did not apply "+fieldName(f.Key)+"; is it older than this ghr?", true, m.now())
			break
		}
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
```

In `internal/tui/settings.go`, replace:

```go
		f.Input.SetDisabled(off)
	}

	secs := []ui.Section{
```

with:

```go
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
```

In `internal/tui/settings.go`, replace:

```go
			s.row("Warm", repoKey(r.Name, "warm"), "applies in all mode"),
```

with:

```go
			s.warmRow(r.Name, errs),
```

In `internal/tui/settings.go`, replace:

```go
	s.group.Set(ws)
	return secs
```

with:

```go
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
```

In `internal/tui/settings.go`, replace:

```go
		s.group.Prev()
	case "pgup":
```

with:

```go
		s.group.Prev()
	case "ctrl+s":
		mm, cmd := m.saveSettings()
		return true, mm, cmd
	case "pgup":
```

In `internal/tui/settings.go`, replace:

```go
// settingsBodyH is the number of form lines the page shows at once.
func (m Model) settingsBodyH() int {
	_, h := m.contentSize()
	return h
```

with:

```go
// settingsBodyH is the number of form lines the page shows at once: the
// content height less the alert box and the unsaved-changes bar.
func (m Model) settingsBodyH() int {
	w, h := m.contentSize()
	h -= len(m.alertBox(w))
	if m.unsavedBar() != "" {
		h--
	}
	return max(h, 1)
```

In `internal/tui/settings.go`, replace:

```go
	s.scroll = min(max(s.scroll, 0), max(len(lines)-h, 0))
	return zone.Mark("settings/body", strings.Join(lines[s.scroll:min(s.scroll+h, len(lines))], "\n"))
```

with:

```go
	top, bar := m.alertBox(w), m.unsavedBar()
	bodyH := h - len(top)
	if bar != "" {
		bodyH--
	}
	bodyH = max(bodyH, 1)
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
```

In `internal/tui/dialogs.go`, replace:

```go
		m.overlay = ovNone
	}
```

with:

```go
		m.overlay = ovNone
	case setSave:
		return m.saveSettings()
	case setDiscard:
		m.settings.form.Discard()
		m.settings.alert = nil
	}
```

In `internal/tui/model.go`, replace:

```go
		return m, nil
	case doneMsg:
```

with:

```go
		return m, nil
	case savedMsg:
		return m.saved(msg)
	case refetchedMsg:
		return m.refetched(msg)
	case doneMsg:
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run TestSettings`
Expected: PASS for every `TestSettings…` test, including the nine save tests.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/dialogs.go internal/tui/model.go internal/tui/settings.go internal/tui/settings_test.go internal/tui/tui_test.go
git commit -m "feat(tui): save and discard settings"
```

### Task 18: Config refresh and repo changes

**Files:**
- Modify: `internal/tui/model.go` (tick, epoch change, `configMsg`, `fetchConfig`)
- Modify: `internal/tui/settings.go` (`load`, `nextSeq`, `loadConfig`, `saved`, `refetched`)
- Test: `internal/tui/settings_test.go`
- Test: `internal/tui/tui_test.go` (the `configMsg` literal)

**Interfaces:**
- Consumes: **Settings page**, **Settings save**
- Produces: `configMsg{seq, cfg}`, `nextSeq`, `loadConfig` (see Contracts, **Settings page**)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

Config responses are numbered, like the log and event responses already are, so one answering an older request never replaces a newer config.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/settings_test.go`, replace:

```go
		t.Fatalf("alert not bounded (%d lines):\n%s", lipgloss.Height(v), v)
	}
}
```

with:

```go
		t.Fatalf("alert not bounded (%d lines):\n%s", lipgloss.Height(v), v)
	}
}

func TestConfigRefreshesEveryFiveTicksOnEveryPage(t *testing.T) {
	c := &fakeClient{cfg: parseConfig(t, settingsYAML)}
	m := sampleModel(c, 120, 30) // on the Dashboard, no config loaded yet
	if m = ticks(m, slowPoll-1); m.cfg != nil {
		t.Fatal("config fetched before the fifth tick")
	}
	if m = ticks(m, 1); m.cfg == nil || m.cfg.Owner != "darkraise" {
		t.Fatal("config not fetched on the fifth tick")
	}
	c.cfg = parseConfig(t, strings.Replace(settingsYAML, "global_max: 3", "global_max: 4", 1))
	st := sampleStatus()
	st.Epoch = "e2" // the daemon restarted
	m = feed(m, statusMsg{st: st})
	if m.cfg.GlobalMax != 4 {
		t.Fatalf("config not re-fetched after a daemon restart: global max %d", m.cfg.GlobalMax)
	}
}

func TestSettingsRefreshMergesRepoChanges(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	set(m, repoKey("darkagents", "labels"), ui.Value{List: []string{"x"}})
	set(m, repoKey("darkmem", "max"), ui.Value{Num: 4, Set: true})
	// Meanwhile: idle_timeout changed by hand, darkmem removed, darkagents being removed, newrepo added.
	c.cfg = parseConfig(t, `owner: darkraise
mode: queue
global_max: 3
idle_timeout: 9m
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
	s := m.settings
	if got := s.input(setPollInterval).Value().Text; got != "30s" {
		t.Errorf("dirty field lost its edit: %q", got)
	}
	if got := s.input(setIdleTimeout).Value().Text; got != "9m0s" {
		t.Errorf("clean field not refreshed: %q", got)
	}
	if s.form.Field(repoKey("darkmem", "max")) != nil {
		t.Error("removed repo kept its fields")
	}
	if s.form.Field(repoKey("darkagents", "labels")).Dirty() {
		t.Error("repo being removed kept its edit")
	}
	if got := s.input(repoKey("newrepo", "max")).Value(); got.Num != 3 || !got.Set {
		t.Errorf("new repo max %+v", got)
	}
	if !strings.Contains(m.View(), "repository darkmem was removed") {
		t.Error("no toast for the removed repo")
	}
	form := strings.Join(settingsLines(m, 103), "\n")
	for _, want := range []string{"darkagents (removing…)", "─ newrepo "} {
		if !strings.Contains(form, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(form, "─ darkmem ") {
		t.Error("removed repo still has a card")
	}
	p, _ := s.buildPatch()
	if p.PollInterval == nil || len(p.Repos) != 0 {
		t.Errorf("patch after refresh: %+v", p)
	}
}

// A config response to an older request never replaces a newer one.
func TestStaleConfigResponseIsDropped(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	older, newer := m.fetchConfig(), m.fetchConfig()
	c.cfg = parseConfig(t, strings.Replace(settingsYAML, "global_max: 3", "global_max: 7", 1))
	fresh := newer()
	c.cfg = parseConfig(t, strings.Replace(settingsYAML, "global_max: 3", "global_max: 5", 1))
	stale := older()
	m = feed(m, fresh, stale)
	if m.cfg.GlobalMax != 7 || m.settings.input(setGlobalMax).Value().Num != 7 {
		t.Fatalf("stale response applied: global max %d", m.cfg.GlobalMax)
	}
}

// When a refresh disables the focused control, focus moves on and the new
// focus is scrolled into view.
func TestSettingsRefreshScrollsMovedFocusIntoView(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 22)
	m.View()
	m.settings.group.Focus(repoKey("darkcloud", "labels"))
	m.scrollToFocus()
	c.cfg = parseConfig(t, strings.Replace(settingsYAML, "  - name: darkcloud\n", "  - name: darkcloud\n    paused: true\n    removing: true\n", 1))
	m = ticks(m, slowPoll)
	if got := m.settings.group.FocusedID(); got != repoKey("darkmem", "max") {
		t.Fatalf("focus %q", got)
	}
	if v := m.View(); !strings.Contains(v, "Max                › [ − ]") {
		t.Fatalf("new focus not in view (scroll %d):\n%s", m.settings.scroll, v)
	}
}
```

In `internal/tui/tui_test.go`, replace:

```go
		upd, _ := newModel(&fakeClient{}, 120, h, st).Update(configMsg(sampleConfig(t, repos[:6]...)))
```

with:

```go
		upd, _ := newModel(&fakeClient{}, 120, h, st).Update(configMsg{cfg: sampleConfig(t, repos[:6]...)})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `invalid composite literal type configMsg` (the tests use the new message shape).

- [ ] **Step 3: Write the implementation**

In `internal/tui/model.go`, replace:

```go
	configMsg *config.Config
	doneMsg   struct {
```

with:

```go
	configMsg struct {
		seq int // orders config responses; see settingsPage.nextSeq
		cfg *config.Config
	}
	doneMsg struct {
```

In `internal/tui/model.go`, replace:

```go
func (m Model) fetchConfig() tea.Cmd {
	return func() tea.Msg {
```

with:

```go
func (m Model) fetchConfig() tea.Cmd {
	seq := m.settings.nextSeq()
	return func() tea.Msg {
```

In `internal/tui/model.go`, replace:

```go
		return configMsg(&cfg)
```

with:

```go
		return configMsg{seq, &cfg}
```

In `internal/tui/model.go`, replace:

```go
		if m.page == pageSettings && m.cfg == nil {
```

with:

```go
		if m.frame%slowPoll == 0 || (m.page == pageSettings && m.cfg == nil) {
```

In `internal/tui/model.go`, replace:

```go
			// The daemon restarted and its event sequence numbers started over.
			m.epoch, m.lastSeq, m.events, m.eventScroll = msg.st.Epoch, 0, nil, 0
			cmds = append(cmds, m.fetchEvents())
```

with:

```go
			// The daemon restarted: its event sequence numbers started over, and
			// it may have reloaded a hand-edited config.yaml.
			m.epoch, m.lastSeq, m.events, m.eventScroll = msg.st.Epoch, 0, nil, 0
			cmds = append(cmds, m.fetchEvents(), m.fetchConfig())
```

In `internal/tui/model.go`, replace:

```go
		m.cfg = msg
		m.settings.load(msg)
```

with:

```go
		m.loadConfig(msg.seq, msg.cfg)
```

In `internal/tui/settings.go`, replace:

```go
	repos  []config.Repo // the repos of the last loaded config, in order
	saving bool
```

with:

```go
	repos  []config.Repo // the repos of the last loaded config, in order
	seq    int           // the last config request number issued
	shown  int           // the request number of the config the form shows
	saving bool
```

In `internal/tui/settings.go`, replace:

```go
	refetchedMsg struct {
		cfg  *config.Config
```

with:

```go
	refetchedMsg struct {
		seq  int
		cfg  *config.Config
```

In `internal/tui/settings.go`, replace:

```go
// load merges a freshly fetched config into the form.
func (s *settingsPage) load(c *config.Config) {
	s.form.Merge(settingsSpecs(c))
	s.repos = append([]config.Repo{}, c.Repos...)
```

with:

```go
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
```

In `internal/tui/settings.go`, replace:

```go
	m.toast.Show("Settings saved", false, m.now())
	c := m.c
	return m, func() tea.Msg {
```

with:

```go
	m.toast.Show("Settings saved", false, m.now())
	c, seq := m.c, s.nextSeq()
	return m, func() tea.Msg {
```

In `internal/tui/settings.go`, replace:

```go
			return refetchedMsg{sent: msg.sent, err: err}
		}
		return refetchedMsg{cfg: &cfg, sent: msg.sent}
```

with:

```go
			return refetchedMsg{seq: seq, sent: msg.sent, err: err}
		}
		return refetchedMsg{seq: seq, cfg: &cfg, sent: msg.sent}
```

In `internal/tui/settings.go`, replace:

```go
	m.cfg = msg.cfg
	s.load(msg.cfg)
```

with:

```go
	// A newer refresh may already be shown; the reset and the check below
	// then run against it, which reflects the save just as well.
	m.loadConfig(msg.seq, msg.cfg)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: `ok`.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/settings.go internal/tui/settings_test.go internal/tui/tui_test.go
git commit -m "feat(tui): refresh settings from the daemon"
```

### Task 19: Leave guard

**Files:**
- Modify: `internal/tui/model.go` (`ovUnsaved`, `leaveTarget`, two fields)
- Modify: `internal/tui/input.go`
- Modify: `internal/tui/dialogs.go`
- Modify: `internal/tui/settings.go` (`saved`, `refetched`)
- Test: `internal/tui/settings_test.go`

**Interfaces:**
- Consumes: **TUI dialogs**, **Settings save**
- Produces: **Leave guard** (see Contracts)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/tui/settings_test.go`, replace:

```go
		t.Fatalf("new focus not in view (scroll %d):\n%s", m.settings.scroll, v)
	}
}
```

with:

```go
		t.Fatalf("new focus not in view (scroll %d):\n%s", m.settings.scroll, v)
	}
}

// dirtySettings is onSettings with one unsaved change.
func dirtySettings(t *testing.T, c *fakeClient) Model {
	t.Helper()
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	return m
}

func TestLeaveGuardAsksOnQDigitsAndSidebar(t *testing.T) {
	m := dirtySettings(t, &fakeClient{})
	upd, cmd := m.Update(key("q"))
	m = upd.(Model)
	if quits(cmd) || m.overlay != ovUnsaved {
		t.Fatalf("q with unsaved changes: overlay %v", m.overlay)
	}
	v := m.View()
	for _, want := range []string{"Unsaved changes", "You have 1 unsaved change on the Settings page.", "( Stay )    ( Discard )  › [ Save ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("dialog missing %q:\n%s", want, v)
		}
	}
	m = feed(m, key("esc")) // esc means Stay
	if m.overlay != ovNone || m.page != pageSettings || m.settings.input(setPollInterval).Value().Text != "30s" {
		t.Fatalf("esc: overlay %v page %v", m.overlay, m.page)
	}
	if m = feed(m, key("1")); m.overlay != ovUnsaved || m.leaveTo != (leaveTarget{page: pageDashboard}) {
		t.Fatalf("1: overlay %v target %+v", m.overlay, m.leaveTo)
	}
	m = click(t, m, btnLeaveStay)
	if m = click(t, m, "nav/history"); m.overlay != ovUnsaved || m.leaveTo != (leaveTarget{page: pageHistory}) {
		t.Fatalf("sidebar: overlay %v target %+v", m.overlay, m.leaveTo)
	}
	m = click(t, m, btnLeaveDiscard)
	if m.overlay != ovNone || m.page != pageHistory || len(m.settings.form.Dirty()) != 0 {
		t.Fatalf("discard: overlay %v page %v dirty %d", m.overlay, m.page, len(m.settings.form.Dirty()))
	}
}

func TestLeaveGuardSaveThenNavigate(t *testing.T) {
	c := &fakeClient{}
	applyPoll(c)
	m := feed(dirtySettings(t, c), keys("2", "enter")...) // enter presses Save, the primary
	if m.page != pageRunners || len(c.patches) != 1 || *c.patches[0].PollInterval != "30s" {
		t.Fatalf("page %v patches %d", m.page, len(c.patches))
	}
}

func TestLeaveGuardRejectedSaveStays(t *testing.T) {
	c := &fakeClient{patchErr: errors.New("poll_interval must be >= 5s")}
	m := feed(dirtySettings(t, c), keys("3", "enter")...)
	if m.page != pageSettings || !strings.Contains(m.View(), "✖ poll_interval must be >= 5s") || m.leaving {
		t.Fatalf("page %v leaving %v", m.page, m.leaving)
	}
}

// pump applies msgs and every message their commands produce, running each
// command exactly once, and reports whether any of them asked to quit.
func pump(m Model, msgs ...tea.Msg) (Model, bool) {
	quit := false
	for len(msgs) > 0 {
		msg := msgs[0]
		msgs = msgs[1:]
		if _, ok := msg.(tea.QuitMsg); ok {
			quit = true
			continue
		}
		upd, cmd := m.Update(msg)
		m = upd.(Model)
		msgs = append(msgs, collect(cmd)...)
	}
	return m, quit
}

// applyPoll makes the fake daemon apply a patched poll interval.
func applyPoll(c *fakeClient) {
	c.onPatch = func(p model.ConfigPatch) {
		if p.PollInterval != nil {
			d, _ := config.ParseDuration(*p.PollInterval)
			c.cfg.PollInterval = d
		}
	}
}

// Save, then quit: the quit comes only after the patch and the check of the
// refetched config.
func TestLeaveGuardSaveThenQuit(t *testing.T) {
	c := &fakeClient{}
	applyPoll(c)
	m, quit := pump(dirtySettings(t, c), key("q"), key("enter"))
	if !quit || len(c.patches) != 1 || m.settings.form.Field(setPollInterval).Dirty() || m.settings.saving {
		t.Fatalf("quit %v patches %d", quit, len(c.patches))
	}
}

// When the daemon ignores a saved field, the save does not leave: the
// warning stays on screen.
func TestLeaveGuardStaysWhenDaemonIgnoresAField(t *testing.T) {
	c := &fakeClient{} // accepts the patch but applies nothing
	m, quit := pump(dirtySettings(t, c), key("q"), key("enter"))
	if quit || m.page != pageSettings || !strings.Contains(m.View(), "daemon did not apply poll_interval") {
		t.Fatalf("quit %v page %v", quit, m.page)
	}
}

// While a save is in flight, leaving waits for it instead of opening a second guard.
func TestLeaveWaitsForSaveInFlight(t *testing.T) {
	m := dirtySettings(t, &fakeClient{})
	upd, _ := m.Update(key("ctrl+s"))
	upd, cmd := upd.Update(key("q"))
	m = upd.(Model)
	if quits(cmd) || m.overlay != ovNone || !strings.Contains(m.View(), "wait for the save to finish") {
		t.Fatalf("overlay %v", m.overlay)
	}
}

// l (follow a runner's log) leaves Settings, so it is guarded too.
func TestLogKeyIsGuarded(t *testing.T) {
	m := dirtySettings(t, &fakeClient{})
	m.View()
	if m = feed(m, key("l")); m.overlay != ovUnsaved || m.leaveTo != (leaveTarget{page: pageRunners}) {
		t.Fatalf("l: overlay %v target %+v page %v", m.overlay, m.leaveTo, m.page)
	}
}

func TestNoGuardWhenCleanOrOnCtrlC(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 30)
	if m = feed(m, key("1")); m.page != pageDashboard || m.overlay != ovNone {
		t.Fatalf("clean form guarded: page %v overlay %v", m.page, m.overlay)
	}
	m = dirtySettings(t, &fakeClient{})
	if _, cmd := m.Update(key("ctrl+c")); !quits(cmd) {
		t.Fatal("ctrl+c was guarded")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: ovUnsaved`, `undefined: leaveTarget` and `undefined: btnLeaveStay`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/model.go`, replace:

```go
	ovHelp
)

```

with:

```go
	ovHelp
	ovUnsaved
)

// leaveTarget is where the user was going when the unsaved-changes dialog opened.
type leaveTarget struct {
	page page
	quit bool
}

```

In `internal/tui/model.go`, replace:

```go
	dlgCancel     string       // the button esc presses
	prompt        textinput.Model
```

with:

```go
	dlgCancel     string       // the button esc presses
	leaveTo       leaveTarget  // where to go once unsaved Settings changes are handled
	leaving       bool         // a save started from the unsaved-changes dialog is in flight
	prompt        textinput.Model
```

In `internal/tui/input.go`, replace:

```go
	case ovConfirm, ovHelp:
```

with:

```go
	case ovConfirm, ovHelp, ovUnsaved:
```

In `internal/tui/input.go`, replace:

```go
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		return m.openHelp()
	case "1", "2", "3", "4":
		return m.switchPage(page(key[0] - '1'))
```

with:

```go
	case "q":
		return m.leave(leaveTarget{quit: true})
	case "?":
		return m.openHelp()
	case "1", "2", "3", "4":
		return m.leave(leaveTarget{page: page(key[0] - '1')})
```

In `internal/tui/input.go`, replace:

```go
			m.page = pageRunners
			cmd := m.follow()
			return m, cmd
```

with:

```go
			return m.leave(leaveTarget{page: pageRunners})
```

In `internal/tui/input.go`, replace:

```go
	if m.overlay == ovConfirm || m.overlay == ovHelp {
```

with:

```go
	if m.overlay == ovConfirm || m.overlay == ovHelp || m.overlay == ovUnsaved {
```

In `internal/tui/input.go`, replace:

```go
			return m.switchPage(page(i))
```

with:

```go
			return m.leave(leaveTarget{page: page(i)})
```

In `internal/tui/dialogs.go`, replace:

```go
import (
	"strings"
```

with:

```go
import (
	"fmt"
	"strings"
```

In `internal/tui/dialogs.go`, replace:

```go
	btnClose = "dialog/close"
)
```

with:

```go
	btnClose = "dialog/close"

	btnLeaveSave    = "unsaved/save"
	btnLeaveDiscard = "unsaved/discard"
	btnLeaveStay    = "unsaved/stay"
)
```

In `internal/tui/dialogs.go`, replace:

```go
	m.openDialog(ovConfirm, btnNo, ui.NewButton(btnNo, "No", ui.Secondary), ui.NewButton(btnYes, "Yes", ui.Primary))
	return m, nil
}

```

with:

```go
	m.openDialog(ovConfirm, btnNo, ui.NewButton(btnNo, "No", ui.Secondary), ui.NewButton(btnYes, "Yes", ui.Primary))
	return m, nil
}

// leave goes to t. Leaving Settings with unsaved changes first asks whether
// to save them, discard them or stay; ctrl+c never comes here.
func (m Model) leave(t leaveTarget) (tea.Model, tea.Cmd) {
	staying := !t.quit && t.page == pageSettings
	if m.page == pageSettings && !staying && m.settings.saving {
		// The save decides: its result leaves or stays (see refetched).
		m.toast.Show("wait for the save to finish", true, m.now())
		return m, nil
	}
	if m.page == pageSettings && !staying && len(m.settings.form.Dirty()) > 0 {
		m.leaveTo = t
		m.openDialog(ovUnsaved, btnLeaveStay, ui.NewButton(btnLeaveStay, "Stay", ui.Secondary),
			ui.NewButton(btnLeaveDiscard, "Discard", ui.Secondary), ui.NewButton(btnLeaveSave, "Save", ui.Primary))
		return m, nil
	}
	return m.goTo(t)
}

func (m Model) goTo(t leaveTarget) (tea.Model, tea.Cmd) {
	if t.quit {
		return m, tea.Quit
	}
	return m.switchPage(t.page)
}

```

In `internal/tui/dialogs.go`, replace:

```go
		m.overlay = ovNone
	case setSave:
```

with:

```go
		m.overlay = ovNone
	case btnLeaveStay:
		m.overlay = ovNone
	case btnLeaveDiscard:
		m.overlay = ovNone
		m.settings.form.Discard()
		m.settings.alert = nil
		return m.goTo(m.leaveTo)
	case btnLeaveSave:
		// Leave only once the save succeeds; saved() does the navigation.
		m.overlay = ovNone
		upd, cmd := m.saveSettings()
		m = upd.(Model)
		m.leaving = cmd != nil
		return m, cmd
	case setSave:
```

In `internal/tui/dialogs.go`, replace:

```go
		dialog = modal("Keys", helpText(), m.dlgButtons, m.dlg.FocusedID(), w)
	case ovPrompt:
```

with:

```go
		dialog = modal("Keys", helpText(), m.dlgButtons, m.dlg.FocusedID(), w)
	case ovUnsaved:
		n := len(m.settings.form.Dirty())
		body := fmt.Sprintf("You have %d unsaved changes on the Settings page.", n)
		if n == 1 {
			body = "You have 1 unsaved change on the Settings page."
		}
		dialog = modal("Unsaved changes", body, m.dlgButtons, m.dlg.FocusedID(), w)
	case ovPrompt:
```

In `internal/tui/settings.go`, replace:

```go
		s.saving = false
```

with:

```go
		s.saving, m.leaving = false, false // a rejected save stays on Settings
```

In `internal/tui/settings.go`, replace:

```go
func (m Model) refetched(msg refetchedMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	s.saving = false
```

with:

```go
//
// A save started from the unsaved-changes dialog leaves only from here, once
// the check has run: a failed refetch or an ignored field stays on Settings
// so the warning is seen.
func (m Model) refetched(msg refetchedMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	s.saving = false
	leaving := m.leaving
	m.leaving = false
```

In `internal/tui/settings.go`, replace:

```go
			break
		}
```

with:

```go
			return m, nil
		}
	}
	if leaving {
		return m.goTo(m.leaveTo)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run "TestLeaveGuard|TestNoGuard|TestLeaveWaits|TestLogKey"`
Expected: PASS for the five `TestLeaveGuard…` tests, `TestLeaveWaitsForSaveInFlight`, `TestLogKeyIsGuarded` and `TestNoGuardWhenCleanOrOnCtrlC`.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/dialogs.go internal/tui/input.go internal/tui/model.go internal/tui/settings.go internal/tui/settings_test.go
git commit -m "feat(tui): guard leaving unsaved settings"
```

### Task 20: Repo actions on Settings

**Files:**
- Modify: `internal/tui/settings.go`
- Modify: `internal/tui/dialogs.go` (`pressed`)
- Test: `internal/tui/settings_test.go`
- Modify (regenerated): `internal/tui/testdata/TestSettingsGolden/120.golden`

**Interfaces:**
- Consumes: **Settings page**, `openConfirm`
- Produces: `settingsPage.button`, `repoAction` (see Contracts, **Settings page**)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

In `internal/tui/settings_test.go`, replace:

```go
	if got := m.settings.group.FocusedID(); got != repoKey("darkagents", "cleanup") {
		t.Fatalf("wrap: %q", got)
	}
	v := m.View()
	if !strings.Contains(v, "Cleanup prefixes   › + add") || lipgloss.Height(v) > 22 || m.settings.scroll == 0 {
```

with:

```go
	if got := m.settings.group.FocusedID(); got != repoKey("darkagents", "remove") {
		t.Fatalf("wrap: %q", got)
	}
	v := m.View()
	if !strings.Contains(v, "[ Resume ] › [ Remove ]") || lipgloss.Height(v) > 22 || m.settings.scroll == 0 {
```

In `internal/tui/settings_test.go`, replace:

```go
		t.Fatal("ctrl+c was guarded")
	}
}
```

with:

```go
		t.Fatal("ctrl+c was guarded")
	}
}

func TestSettingsRepoActions(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 80)
	form := strings.Join(settingsLines(m, 103), "\n")
	if !strings.Contains(form, "[ Pause ]   [ Remove ]") || !strings.Contains(form, "[ Resume ]   [ Remove ]") {
		t.Fatalf("repo action buttons missing:\n%s", form)
	}
	m = click(t, m, repoKey("darkmem", "pause"))
	m = click(t, m, repoKey("darkagents", "pause")) // paused: the button resumes
	m = click(t, m, repoKey("darkcloud", "remove"))
	if m.overlay != ovConfirm || !strings.Contains(m.View(), "Remove repo darkcloud? Its running jobs finish first.") {
		t.Fatalf("remove did not ask: overlay %v", m.overlay)
	}
	m = feed(m, key("enter"))
	if got := strings.Join(c.actions(), "|"); got != "pause darkmem|resume darkagents|rm darkcloud" {
		t.Fatalf("actions %s", got)
	}
	if len(c.patches) != 0 {
		t.Fatal("a repo action went through the config patch")
	}
}

func TestSettingsRepoActionsDisabledWhileRemoving(t *testing.T) {
	c := &fakeClient{cfg: parseConfig(t, strings.Replace(settingsYAML, "  - name: darkmem\n", "  - name: darkmem\n    paused: true\n    removing: true\n", 1))}
	m := feed(sampleModel(c, 120, 80), key("4"))
	m.View()
	for _, act := range []string{"pause", "remove"} {
		if m.settings.buttons[repoKey("darkmem", act)].Focusable() {
			t.Errorf("darkmem %s is enabled while removing", act)
		}
	}
	if !m.settings.buttons[repoKey("darkcloud", "remove")].Focusable() {
		t.Error("another repo's button was disabled")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `m.settings.buttons undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/settings.go`, replace:

```go
import (
	"fmt"
```

with:

```go
import (
	"context"
	"fmt"
```

In `internal/tui/settings.go`, replace:

```go
	save, discard *ui.Button
}
```

with:

```go
	save, discard *ui.Button
	buttons       map[string]*ui.Button // the repo cards' action buttons, by ID
}
```

In `internal/tui/settings.go`, replace:

```go
func (s *settingsPage) input(key string) ui.Input { return s.form.Field(key).Input }

```

with:

```go
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

```

In `internal/tui/settings.go`, replace:

```go
		}
		title := clean(r.Name)
```

with:

```go
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
```

In `internal/tui/settings.go`, replace:

```go
			s.row("Cleanup prefixes", repoKey(r.Name, "cleanup"), "container name prefixes removed after each job"),
		}}
```

with:

```go
			s.row("Cleanup prefixes", repoKey(r.Name, "cleanup"), "container name prefixes removed after each job"),
			{Items: []ui.Widget{pause, remove}},
		}}
```

In `internal/tui/dialogs.go`, replace:

```go
		m.settings.alert = nil
	}
```

with:

```go
		m.settings.alert = nil
	default:
		if strings.HasPrefix(id, "settings/repo/") {
			return m.repoAction(id)
		}
	}
```

- [ ] **Step 4: Regenerate the snapshots**

Run: `go test ./internal/tui/ -run TestSettingsGolden -update -count=1 -timeout 180s`
Expected: the command exits 0 and writes the Settings snapshots; only `120.golden` changes. Open each file and compare it with the snapshot below (trailing spaces are trimmed here; the files keep them).

`internal/tui/testdata/TestSettingsGolden/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
  1 Dashboard    Settings
  2 Runners      ╭─ General ───────────────────────────────────────────────────────────────────────────────────────────╮
  3 History      │ Mode               › [ queue ▾ ]  start runners only for queued jobs, up to the global max          │
▌ 4 Settings     │ Global max           [ − ] 3 [ + ]  applies in queue mode                                           │
                 │ Owner                darkraise  change in config.yaml and restart the daemon                        │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Timing ────────────────────────────────────────────────────────────────────────────────────────────╮
                 │ Poll interval        [ 10s        ]  how often GitHub is checked (at least 5s)                      │
                 │ Start timeout        [ 2m0s       ]  a runner not online by then is replaced                        │
                 │ Idle timeout         [ 5m0s       ]  idle runners beyond warm stop after this                       │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Disk and retention ────────────────────────────────────────────────────────────────────────────────╮
                 │ Disk high-water      [ − ] 80% [ + ]  disk use that triggers pruning                                │
                 │ Build cache keep     [ 20GB       ]  build cache kept when pruning, e.g. 20GB                       │
                 │ History retention    [ 30d        ]  history and logs older than this are removed (at least 1d)     │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Runner defaults ───────────────────────────────────────────────────────────────────────────────────╮
                 │ limits apply to newly started runners                                                               │
                 │ Global labels        homelab ✕  + add  added to every runner                                        │
                 │ Memory max           [ 6G         ]  per runner, e.g. 6G, 50% or infinity                           │
                 │ CPU quota            [ 200%       ]  per runner, e.g. 200%                                          │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ darkcloud ─────────────────────────────────────────────────────────────────────────────────────────╮
                 │ Max                  [ − ] 2 [ + ]  once set, it stays explicit                                     │
                 │ Warm                 [ − ] 1 [ + ] (default)  applies in all mode                                   │
                 │ Labels               darkcloud-linux ✕  + add  added to this repo's runners                         │
  ? Help         │ Cleanup prefixes     dc-e2e- ✕  + add  container name prefixes removed after each job               │
  q Quit         │                      [ Pause ]   [ Remove ]                                                         │
 tab next  shift+tab previous  ctrl+s save  ? help  q quit
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: `ok`.

- [ ] **Step 6: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/dialogs.go internal/tui/settings.go internal/tui/settings_test.go internal/tui/testdata/TestSettingsGolden/120.golden
git commit -m "feat(tui): add repo actions to settings"
```

### Task 21: Add repository dialog

**Files:**
- Modify: `internal/tui/dialogs.go`
- Modify: `internal/tui/model.go` (remove the prompt; `ovAddRepo`, `add`)
- Modify: `internal/tui/input.go` (remove `openPrompt`)
- Modify: `internal/tui/settings.go` (`advance` also serves the dialog)
- Test: `internal/tui/tui_test.go`

**Interfaces:**
- Consumes: **ui controls**, `Group`, **ui layout**, **TUI dialogs**
- Produces: **Add repository dialog** (see Contracts)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

The dialog replaces the one-line "name [labels]" prompt everywhere: the global `a` key now opens it.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/tui_test.go`, replace:

```go
	"github.com/darkraise/ghr/internal/model"
)
```

with:

```go
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)
```

In `internal/tui/tui_test.go`, replace:

```go
	patches  []model.ConfigPatch
	patchErr error                   // returned by PatchConfig when set
```

with:

```go
	patches  []model.ConfigPatch
	addErr   error                   // returned by AddRepo when set
	patchErr error                   // returned by PatchConfig when set
```

In `internal/tui/tui_test.go`, replace:

```go
	return f.rec("add %s %s", r.Name, strings.Join(r.Labels, ","))
```

with:

```go
	max := "-"
	if r.Max != nil {
		max = fmt.Sprint(*r.Max)
	}
	f.rec("add %s %s max=%s public=%v", r.Name, strings.Join(r.Labels, ","), max, r.AllowPublic)
	return f.addErr
```

In `internal/tui/tui_test.go`, replace:

```go
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
```

with:

```go
func TestAddRepoDialog(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), key("a"))
	if m.overlay != ovAddRepo {
		t.Fatal("dialog not open")
	}
	v := m.View()
	for _, want := range []string{"Add repository", "Name", "› [ ", "[ − ] 1 [ + ] (default)", "Allow public repo",
		"self-hosted runners on a public repo can run anyone's code", "( Cancel )", "[ Add ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("dialog missing %q", want)
		}
	}
	// enter in the name field commits it and moves focus to Max.
	m = feed(m, keys("n", "e", "w", "r", "e", "p", "o", "enter", "+", "+", "tab", "enter", "g", "p", "u", "enter", "esc", "tab", " ", "tab", "tab", "enter")...)
	if got := strings.Join(c.actions(), "|"); got != "add newrepo gpu max=3 public=true" {
		t.Fatalf("actions %q", got)
	}
	if m.overlay != ovNone || !strings.Contains(m.View(), "✔ added newrepo") {
		t.Fatalf("after add: overlay %v", m.overlay)
	}
}

func TestAddRepoDialogErrorsStayInside(t *testing.T) {
	// The daemon's own message (internal/daemon/backend.go), on an 80-column screen.
	c := &fakeClient{addErr: errors.New("site is public; self-hosted runners must only serve private repos (pass --allow-public to override)")}
	m := feed(sampleModel(c, 80, 30), key("a"))
	m = click(t, m, addOK)
	if m.overlay != ovAddRepo || !strings.Contains(m.View(), "✖ name is required") || len(c.actions()) != 0 {
		t.Fatalf("empty name: overlay %v actions %v", m.overlay, c.actions())
	}
	m = click(t, m, addName)
	m = feed(m, keys("s", "i", "t", "e")...)
	upd, cmd := m.Update(ui.Pressed{ID: addOK})
	m = upd.(Model)
	if !strings.Contains(m.View(), "[ Adding… ]") || m.add.group.FocusedID() == addOK {
		t.Fatalf("Add not disabled while in flight, or still focused (%q)", m.add.group.FocusedID())
	}
	if _, again := m.Update(ui.Pressed{ID: addOK}); again != nil {
		t.Fatal("a second add was sent while one was in flight")
	}
	m = feed(m, collect(cmd)...)
	v := m.View()
	if m.overlay != ovAddRepo || !strings.Contains(v, "✖ site is public;") || !strings.Contains(v, "--allow-public") || m.add.busy {
		t.Fatalf("rejection: overlay %v\n%s", m.overlay, v)
	}
	for i, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line %d is %d wide:\n%s", i, lipgloss.Width(line), v)
		}
	}
	if got := strings.Join(c.actions(), "|"); got != "add site  max=- public=false" {
		t.Fatalf("untouched max sent: %q", got)
	}
	m = click(t, m, addCancel)
	if m.overlay != ovNone {
		t.Fatal("cancel did not close")
	}
}

// A reply to a dialog that was cancelled never touches the dialog opened after it.
func TestAddRepoLateReplyIgnoresNewDialog(t *testing.T) {
	c := &fakeClient{addErr: errors.New("repo site is already configured")}
	m := feed(sampleModel(c, 120, 30), key("a"))
	m = feed(m, keys("s", "i", "t", "e")...)
	upd, cmd := m.Update(ui.Pressed{ID: addOK})
	m = feed(upd.(Model), ui.Pressed{ID: addCancel}, key("a"))
	fresh := m.add
	m = feed(m, collect(cmd)...)
	// The open dialog hides the toast line, so check the toast itself.
	if m.add != fresh || fresh.err != "" || fresh.busy || m.toast.Text != "repo site is already configured" {
		t.Fatalf("late reply reached the new dialog: err %q busy %v", fresh.err, fresh.busy)
	}
}

// The dialog fits the narrowest supported screen without re-wrapping its rows.
func TestAddRepoDialogFitsNarrowScreen(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 40, 30), key("a"))
	v := m.View()
	for i, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("line %d is %d wide:\n%s", i, lipgloss.Width(line), v)
		}
	}
	if !strings.Contains(v, "Add repository") || !strings.Contains(v, "[ Add ]") {
		t.Fatalf("dialog incomplete:\n%s", v)
	}
}

// While the daemon is unreachable, a and the Add button both do nothing.
func TestAddRepoUnavailableWhileUnreachable(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 30), statusMsg{err: errors.New("connection refused")})
	if m = feed(m, key("a")); m.overlay == ovAddRepo || !strings.Contains(m.View(), "daemon is unreachable") {
		t.Fatalf("a opened the dialog while unreachable: overlay %v", m.overlay)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: ovAddRepo`, `undefined: addOK` and `undefined: addName`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/dialogs.go`, replace:

```go
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/tui/ui"
)
```

with:

```go
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)
```

In `internal/tui/dialogs.go`, replace:

```go
	btnLeaveStay    = "unsaved/stay"
)

```

with:

```go
	btnLeaveStay    = "unsaved/stay"

	addName   = "add/name"
	addMax    = "add/max"
	addLabels = "add/labels"
	addPublic = "add/public"
	addOK     = "add/ok"
	addCancel = "add/cancel"
)

// addRepoDialog is the Add repository form.
type addRepoDialog struct {
	name       *ui.TextField
	max        *ui.Stepper
	labels     *ui.TagList
	public     *ui.Toggle
	ok, cancel *ui.Button
	group      ui.Group
	err        string // the daemon's rejection, shown inside the dialog
	busy       bool
}

// addedMsg carries the dialog that sent the request, so a late reply never
// changes a dialog opened after it.
type addedMsg struct {
	d    *addRepoDialog
	name string
	err  error
}

func (m Model) openAddRepo() (tea.Model, tea.Cmd) {
	if !m.connected {
		m.toast.Show("the daemon is unreachable; add the repository once it reconnects", true, m.now())
		return m, nil
	}
	d := &addRepoDialog{
		name:   ui.NewTextField(addName, 30),
		max:    ui.NewStepper(addMax, 0, 99, 1),
		labels: ui.NewTagList(addLabels),
		public: ui.NewToggle(addPublic, false),
		ok:     ui.NewButton(addOK, "Add", ui.Primary),
		cancel: ui.NewButton(addCancel, "Cancel", ui.Secondary),
	}
	// Max shows the default a new repo gets until it is touched; untouched sends no max.
	d.max.ZeroText = "∞"
	d.max.SetValue(ui.Value{})
	d.max.Default = 1
	if m.st.Mode == config.ModeAll {
		d.max.Default, d.max.DefaultText = 0, "∞"
	}
	d.sync(true)
	m.add, m.overlay = d, ovAddRepo
	return m, nil
}

// sync updates the Add button (disabled while a request is in flight or the
// daemon is unreachable) and the focus order, so focus never rests on it
// while it is disabled.
func (d *addRepoDialog) sync(connected bool) {
	d.ok.Label = "Add"
	if d.busy {
		d.ok.Label = "Adding…"
	}
	d.ok.SetDisabled(d.busy || !connected)
	d.group.Set([]ui.Widget{d.name, d.max, d.labels, d.public, d.cancel, d.ok})
}

// addRepoKey routes a key in the Add repository dialog: the focused control
// first (but enter on a button is always the primary, Add), then esc
// cancels, tab and the arrows move focus, and enter adds.
func (m Model) addRepoKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.add
	if _, onButton := d.group.Focused().(*ui.Button); onButton && k.String() == "enter" {
		return m.pressed(addOK)
	}
	if ok, cmd := d.group.Key(k); ok {
		return m, cmd
	}
	switch k.String() {
	case "esc":
		return m.pressed(addCancel)
	case "tab", "down":
		d.group.Next()
	case "shift+tab", "up":
		d.group.Prev()
	case "enter":
		return m.pressed(addOK)
	}
	return m, nil
}

func (m Model) submitAddRepo() (tea.Model, tea.Cmd) {
	d := m.add
	if d == nil || d.busy {
		return m, nil
	}
	if !m.connected {
		d.err = "the daemon is unreachable"
		return m, nil
	}
	name := d.name.Value().Text
	if name == "" {
		d.err = "name is required"
		return m, nil
	}
	req := model.AddRepoRequest{Name: name, Labels: d.labels.Value().List, AllowPublic: d.public.On}
	if v := d.max.Value(); v.Set {
		n := v.Num
		req.Max = &n
	}
	d.busy, d.err = true, ""
	d.sync(m.connected)
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return addedMsg{d, name, c.AddRepo(cx, req)}
	}
}

// added closes the dialog on success. A rejection (the token cannot see the
// repo, it is public, it is a duplicate) stays inside the open dialog.
func (m Model) added(msg addedMsg) (tea.Model, tea.Cmd) {
	mine := m.overlay == ovAddRepo && m.add == msg.d
	if msg.err != nil {
		if mine {
			m.add.busy, m.add.err = false, clean(msg.err.Error())
			m.add.sync(m.connected)
			return m, nil
		}
		m.toast.Show(clean(msg.err.Error()), true, m.now())
		return m, nil
	}
	if mine {
		m.overlay, m.add = ovNone, nil
	}
	m.toast.Show("added "+msg.name, false, m.now())
	return m, tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig())
}

// addRepoView renders the dialog to fit a screen w columns wide.
func (m Model) addRepoView(w int) string {
	d := m.add
	d.sync(m.connected)
	// The rows stay inside the modal's inner width (w-6) down to 40 columns.
	rw := max(min(72, w-14), 26)
	d.name.Width = max(min(30, rw-24), 8)
	f := d.group.FocusedID()
	lines, _ := ui.Render([]ui.Section{{Rows: []ui.Row{
		{Label: "Name", Items: []ui.Widget{d.name}, Desc: "a repository of the owner"},
		{Label: "Max", Items: []ui.Widget{d.max}},
		{Label: "Labels", Items: []ui.Widget{d.labels}},
		{Label: "Allow public repo", Items: []ui.Widget{d.public}},
	}}}, f, rw, false)
	body := strings.Join(lines, "\n") + "\n" + sAmber.Render("⚠ self-hosted runners on a public repo can run anyone's code")
	if d.err != "" {
		body += "\n" + lipgloss.NewStyle().Width(rw).Render(sRed.Render("✖ "+d.err))
	}
	return modal("Add repository", body, []*ui.Button{d.cancel, d.ok}, f, w)
}

```

In `internal/tui/dialogs.go`, replace:

```go
		return m, cmd
	case setSave:
```

with:

```go
		return m, cmd
	case addCancel:
		m.overlay, m.add = ovNone, nil
	case addOK:
		return m.submitAddRepo()
	case setSave:
```

In `internal/tui/dialogs.go`, replace:

```go
// buttons renders the runner detail and prompt overlays' clickable buttons:
// ok runs enter, cancel runs esc.
```

with:

```go
// buttons renders the runner detail overlay's clickable buttons: ok runs
// enter, cancel runs esc.
```

In `internal/tui/dialogs.go`, replace:

```go
	case ovPrompt:
		dialog = sDialog.Render(sBold.Render(m.promptLabel) + "\n\n" + m.prompt.View() + "\n\n" + buttons("Save", "Cancel") + "\n" +
			sDim.Render("enter save · esc cancel"))
```

with:

```go
	case ovAddRepo:
		dialog = m.addRepoView(w)
```

In `internal/tui/model.go`, delete:

```go
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
```

In `internal/tui/model.go`, replace:

```go
	ovPrompt
	ovDetail
	ovHelp
	ovUnsaved
```

with:

```go
	ovDetail
	ovHelp
	ovUnsaved
	ovAddRepo
```

In `internal/tui/model.go`, replace:

```go
	dlg           ui.Group     // focus across the open dialog's buttons
	dlgButtons    []*ui.Button // the open dialog's buttons, in display order
	dlgCancel     string       // the button esc presses
	leaveTo       leaveTarget  // where to go once unsaved Settings changes are handled
	leaving       bool         // a save started from the unsaved-changes dialog is in flight
	prompt        textinput.Model
	promptLabel   string
	promptSubmit  func(string) tea.Cmd
```

with:

```go
	dlg           ui.Group       // focus across the open dialog's buttons
	dlgButtons    []*ui.Button   // the open dialog's buttons, in display order
	dlgCancel     string         // the button esc presses
	leaveTo       leaveTarget    // where to go once unsaved Settings changes are handled
	leaving       bool           // a save started from the unsaved-changes dialog is in flight
	add           *addRepoDialog // the open Add repository dialog
```

In `internal/tui/model.go`, replace:

```go
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Cursor.SetMode(cursor.CursorStatic)
	return Model{
		c: c, now: time.Now, width: 120, height: 40, prompt: ti, settings: newSettingsPage(),
```

with:

```go
	return Model{
		c: c, now: time.Now, width: 120, height: 40, settings: newSettingsPage(),
```

In `internal/tui/model.go`, replace:

```go
		return m.refetched(msg)
	case doneMsg:
```

with:

```go
		return m.refetched(msg)
	case addedMsg:
		return m.added(msg)
	case doneMsg:
```

In `internal/tui/model.go`, delete:

```go
	}
	if m.overlay == ovPrompt {
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
		return m, cmd
```

In `internal/tui/input.go`, delete:

```go
	"strings"
```

In `internal/tui/input.go`, replace:

```go
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
```

with:

```go
	case ovAddRepo:
		return m.addRepoKey(k)
```

In `internal/tui/input.go`, replace:

```go
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
```

with:

```go
		return m.openAddRepo()
```

In `internal/tui/input.go`, delete:

```go
func (m Model) openPrompt(label, value string, submit func(string) tea.Cmd) (tea.Model, tea.Cmd) {
	m.overlay, m.promptLabel, m.promptSubmit = ovPrompt, label, submit
	m.prompt.SetValue(value)
	m.prompt.CursorEnd()
	// Focus mutates m.prompt; Go leaves unspecified whether `return m, m.prompt.Focus()` copies m first.
	cmd := m.prompt.Focus()
	return m, cmd
}

```

In `internal/tui/input.go`, replace:

```go
// dialogButtons map each detail or prompt overlay button zone to the key it stands for.
```

with:

```go
// dialogButtons map each runner detail overlay button zone to the key it stands for.
```

In `internal/tui/input.go`, replace:

```go
		_, cmd := m.dlg.Mouse(msg)
		return m, cmd
```

with:

```go
		_, cmd := m.dlg.Mouse(msg)
		return m, cmd
	}
	if m.overlay == ovAddRepo {
		_, cmd := m.add.group.Mouse(msg)
		return m, cmd
```

In `internal/tui/settings.go`, replace:

```go
	if m.page == pageSettings && m.settings.group.FocusedID() == id {
```

with:

```go
	switch {
	case m.overlay == ovAddRepo && m.add != nil && m.add.group.FocusedID() == id:
		m.add.group.Next()
	case m.overlay == ovNone && m.page == pageSettings && m.settings.group.FocusedID() == id:
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run TestAddRepo`
Expected: PASS for `TestAddRepoDialog`, `TestAddRepoDialogErrorsStayInside`, `TestAddRepoLateReplyIgnoresNewDialog`, `TestAddRepoDialogFitsNarrowScreen` and `TestAddRepoUnavailableWhileUnreachable`.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/dialogs.go internal/tui/input.go internal/tui/model.go internal/tui/settings.go internal/tui/tui_test.go
git commit -m "feat(tui): add the add repository dialog"
```

### Task 22: Add repository button and Settings snapshots

**Files:**
- Modify: `internal/tui/settings.go`
- Modify: `internal/tui/dialogs.go` (`pressed`)
- Test: `internal/tui/settings_test.go`
- Create (generated): `internal/tui/testdata/TestSettingsDialogGolden/{120,80}.golden`, `internal/tui/testdata/TestSettingsDropdownGolden/{120,80}.golden`

**Interfaces:**
- Consumes: **Add repository dialog**, **Settings page**
- Produces: `setAddRepo` (see Contracts, **Settings page**)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests**

In `internal/tui/settings_test.go`, replace:

```go
	"errors"
	"strings"
```

with:

```go
	"errors"
	"fmt"
	"strings"
```

In `internal/tui/settings_test.go`, replace:

```go
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
```

with:

```go
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/golden"
	zone "github.com/lrstanley/bubblezone"
```

In `internal/tui/settings_test.go`, replace:

```go
	if got := m.settings.group.FocusedID(); got != repoKey("darkagents", "remove") {
		t.Fatalf("wrap: %q", got)
	}
	v := m.View()
	if !strings.Contains(v, "[ Resume ] › [ Remove ]") || lipgloss.Height(v) > 22 || m.settings.scroll == 0 {
```

with:

```go
	if got := m.settings.group.FocusedID(); got != setAddRepo {
		t.Fatalf("wrap: %q", got)
	}
	v := m.View()
	if !strings.Contains(v, "› [ + Add repository ]") || lipgloss.Height(v) > 22 || m.settings.scroll == 0 {
```

In `internal/tui/settings_test.go`, replace:

```go
		t.Error("another repo's button was disabled")
	}
}
```

with:

```go
		t.Error("another repo's button was disabled")
	}
}

func TestSettingsAddRepositoryButton(t *testing.T) {
	m := onSettings(t, &fakeClient{}, 120, 80)
	if form := strings.Join(settingsLines(m, 103), "\n"); !strings.Contains(form, "[ + Add repository ]") {
		t.Fatalf("no Add repository button:\n%s", form)
	}
	if m = click(t, m, setAddRepo); m.overlay != ovAddRepo {
		t.Fatalf("overlay %v", m.overlay)
	}
}

func TestSettingsDialogGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := feed(onSettings(t, &fakeClient{}, w, 30), key("a"))
			golden.RequireEqual(t, []byte(m.View()))
		})
	}
}

func TestSettingsDropdownGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := feed(onSettings(t, &fakeClient{}, w, 30), key("enter"))
			golden.RequireEqual(t, []byte(m.View()))
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: setAddRepo`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/settings.go`, replace:

```go
	setDiscard = "settings/discard"
)
```

with:

```go
	setDiscard = "settings/discard"
	setAddRepo = "settings/add_repo"
)
```

In `internal/tui/settings.go`, replace:

```go
	}

```

with:

```go
	}
	add := s.button(setAddRepo, "+ Add repository", ui.Primary)
	add.SetDisabled(!m.connected)
	secs = append(secs, ui.Section{Rows: []ui.Row{{Items: []ui.Widget{add}}}})

```

In `internal/tui/dialogs.go`, replace:

```go
		return m.submitAddRepo()
	case setSave:
```

with:

```go
		return m.submitAddRepo()
	case setAddRepo:
		return m.openAddRepo()
	case setSave:
```

- [ ] **Step 4: Regenerate the snapshots**

Run: `go test ./internal/tui/ -run "TestSettingsDialogGolden|TestSettingsDropdownGolden" -update -count=1 -timeout 180s`
Expected: the command exits 0 and writes four new snapshots. Open each file and compare it with the snapshot below (trailing spaces are trimmed here; the files keep them).

`internal/tui/testdata/TestSettingsDialogGolden/120.golden`:

```text








                     ╭────────────────────────────────────────────────────────────────────────────╮
                     │                                                                            │
                     │  Add repository                                                            │
                     │                                                                            │
                     │  Name               › [                                ]                   │
                     │                       a repository of the owner                            │
                     │  Max                  [ − ] 1 [ + ] (default)                              │
                     │  Labels               + add                                                │
                     │  Allow public repo    [━○] Off                                             │
                     │  ⚠ self-hosted runners on a public repo can run anyone's code              │
                     │                                                                            │
                     │                                                     ( Cancel )    [ Add ]  │
                     │                                                                            │
                     ╰────────────────────────────────────────────────────────────────────────────╯








```

`internal/tui/testdata/TestSettingsDialogGolden/80.golden`:

```text








    ╭──────────────────────────────────────────────────────────────────────╮
    │                                                                      │
    │  Add repository                                                      │
    │                                                                      │
    │  Name               › [                                ]             │
    │                       a repository of the owner                      │
    │  Max                  [ − ] 1 [ + ] (default)                        │
    │  Labels               + add                                          │
    │  Allow public repo    [━○] Off                                       │
    │  ⚠ self-hosted runners on a public repo can run anyone's code        │
    │                                                                      │
    │                                               ( Cancel )    [ Add ]  │
    │                                                                      │
    ╰──────────────────────────────────────────────────────────────────────╯








```

`internal/tui/testdata/TestSettingsDropdownGolden/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
  1 Dashboard    Settings
  2 Runners      ╭─ General ───────────────────────────────────────────────────────────────────────────────────────────╮
  3 History      │ Mode               › [ queue ▾ ]  start runners only for queued jobs, up to the global max          │
▌ 4 Settings     │                        ▸ queue  start runners only for queued jobs, up to the global max            │
                 │                          all  keep warm runners per repo, up to each repo's max                     │
                 │ Global max           [ − ] 3 [ + ]  applies in queue mode                                           │
                 │ Owner                darkraise  change in config.yaml and restart the daemon                        │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Timing ────────────────────────────────────────────────────────────────────────────────────────────╮
                 │ Poll interval        [ 10s        ]  how often GitHub is checked (at least 5s)                      │
                 │ Start timeout        [ 2m0s       ]  a runner not online by then is replaced                        │
                 │ Idle timeout         [ 5m0s       ]  idle runners beyond warm stop after this                       │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Disk and retention ────────────────────────────────────────────────────────────────────────────────╮
                 │ Disk high-water      [ − ] 80% [ + ]  disk use that triggers pruning                                │
                 │ Build cache keep     [ 20GB       ]  build cache kept when pruning, e.g. 20GB                       │
                 │ History retention    [ 30d        ]  history and logs older than this are removed (at least 1d)     │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Runner defaults ───────────────────────────────────────────────────────────────────────────────────╮
                 │ limits apply to newly started runners                                                               │
                 │ Global labels        homelab ✕  + add  added to every runner                                        │
                 │ Memory max           [ 6G         ]  per runner, e.g. 6G, 50% or infinity                           │
                 │ CPU quota            [ 200%       ]  per runner, e.g. 200%                                          │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ darkcloud ─────────────────────────────────────────────────────────────────────────────────────────╮
                 │ Max                  [ − ] 2 [ + ]  once set, it stays explicit                                     │
  ? Help         │ Warm                 [ − ] 1 [ + ] (default)  applies in all mode                                   │
  q Quit         │ Labels               darkcloud-linux ✕  + add  added to this repo's runners                         │
 up move  enter pick  esc close
```

`internal/tui/testdata/TestSettingsDropdownGolden/80.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api 4800   disk ▕██████░░░░▏ 61%   ●
  1 Dashboard    2 Runners    3 History  [ 4 Settings ]                  ? help
Settings
╭─ General ────────────────────────────────────────────────────────────────────╮
│ Mode               › [ queue ▾ ]                                             │
│                        ▸ queue  start runners only for queued jobs, up to t… │
│                          all  keep warm runners per repo, up to each repo's… │
│                      start runners only for queued jobs, up to the global m… │
│ Global max           [ − ] 3 [ + ]                                           │
│                      applies in queue mode                                   │
│ Owner                darkraise                                               │
│                      change in config.yaml and restart the daemon            │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Timing ─────────────────────────────────────────────────────────────────────╮
│ Poll interval        [ 10s        ]                                          │
│                      how often GitHub is checked (at least 5s)               │
│ Start timeout        [ 2m0s       ]                                          │
│                      a runner not online by then is replaced                 │
│ Idle timeout         [ 5m0s       ]                                          │
│                      idle runners beyond warm stop after this                │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Disk and retention ─────────────────────────────────────────────────────────╮
│ Disk high-water      [ − ] 80% [ + ]                                         │
│                      disk use that triggers pruning                          │
│ Build cache keep     [ 20GB       ]                                          │
│                      build cache kept when pruning, e.g. 20GB                │
│ History retention    [ 30d        ]                                          │
│                      history and logs older than this are removed (at least… │
╰──────────────────────────────────────────────────────────────────────────────╯
 up move  enter pick  esc close
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: `ok`.

- [ ] **Step 6: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/dialogs.go internal/tui/settings.go internal/tui/settings_test.go internal/tui/testdata/TestSettingsDialogGolden/120.golden internal/tui/testdata/TestSettingsDialogGolden/80.golden internal/tui/testdata/TestSettingsDropdownGolden/120.golden internal/tui/testdata/TestSettingsDropdownGolden/80.golden
git commit -m "feat(tui): add repository button on settings"
```
### Task 23: Release and LXC acceptance

**Files:**
- None in ghr. Modify: `D:/Repositories/Personal/homelab/docs/superpowers/registers/2026-10-04-ghr-tui-revamp.md` (row 2 state)

**Interfaces:**
- Consumes: everything above, as merged from Tasks 1-22
- Produces: none

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 2 = 3

Every SSH command runs from Git Bash with `timeout` and `-o BatchMode=yes`. The TUI checks drive a real terminal on the LXC: `tmux`, keys through `send-keys`, and clicks as SGR mouse sequences written to the pane (`\e[<0;COL;ROWM` press, `…m` release; columns and rows count from 1). Each check starts its own session `g`, which a trap kills when the script ends, and starts with focus on Mode. Each script's helper `w` waits for text on the screen and exits the script at the first miss, so a script that ends with `step=ok` passed every check; anything else is a failure to report with the printed `found=` line.

- [ ] **Step 1: Run the final review before releasing**

Only reviewed code may be released, and dr-superpowers:executing-plans reaches its whole-branch final review only after every task, this one included, is complete. This step runs in the controlling session, never in a dispatched implementer, because it may run the review and its fix wave. Run in `D:/Repositories/Personal/ghr`: `git rev-parse --short=7 feat/settings-revamp` (the tip) and `git rev-parse --short=7 $(git merge-base master feat/settings-revamp)` (the merge base); the ledger records seven-character hashes. Then read the ledger `D:/Repositories/Personal/homelab/.superpowers/sdd/2026-10-04-ghr-tui-revamp-2-settings/progress.md`. If it holds a `Final review: clean (commits <base>..<tip>…)` line whose tip is the branch tip, go on. Otherwise run the final review now, exactly as executing-plans' Final Review section describes (`reference/final-review.md`), over `<merge-base>..feat/settings-revamp`, apply its fix wave, and append its `Final review: clean …` line to the ledger. If it is not clean after its one fix wave, stop and report the open findings: do not ask for release approval. When execution later reaches the Final Review section after this task, that ledger line already records it.

- [ ] **Step 2: Pre-flight on the LXC**

Run:

```bash
timeout 60 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 '
cp /etc/ghr/config.yaml /root/config-before-plan2.yaml && echo backup=ok
grep -E "^(poll_interval|history_retention):" /etc/ghr/config.yaml
sed -n "/- name: ghr-e2e/,/- name:/p" /etc/ghr/config.yaml
ghr version'
```

Expected: `backup=ok`; `poll_interval` at least 5s and `history_retention` at least 1d (if not, stop: the release would refuse to load this config); the `ghr-e2e` block; `v0.1.3`. Record the poll interval as `POLL0` (it was `10s` on 2026-10-04). Step 8 removes and re-adds `ghr-e2e` through the dialog, which can set only a name and a max: if the `ghr-e2e` block holds anything besides `- name: ghr-e2e` and `max: <n>` (labels, warm, cleanup prefixes, paused), skip Step 8 and say so in the report. Record `<n>` as `MAX0` (it was `3`).

- [ ] **Step 3: Ask for approval to release**

Ask your human partner, in one message: "Plan 2 passes all tests and its final review on `feat/settings-revamp` (commit `<sha>`). May I fast-forward `master`, push (publishing a release), deploy that release to the LXC with `GHR_VERSION` pinned, and run the TUI acceptance there? The acceptance saves a poll interval of 15s and restores `POLL0`, and removes `ghr-e2e` then adds it back with `max: MAX0`; `/root/config-before-plan2.yaml` holds the config as it is now." Wait for an explicit yes. If the answer is no, stop here: leave register row 2 at `planned` and report that Task 23 awaits approval.

- [ ] **Step 4: Merge, push, and wait for this commit's release**

Run in `D:/Repositories/Personal/ghr`:

```bash
git checkout master && git merge --ff-only feat/settings-revamp && SHA=$(git rev-parse HEAD) && echo "sha=$SHA"
timeout 120 git push origin master
RUN=""; for i in $(seq 20); do RUN=$(timeout 30 gh run list --workflow ci --commit "$SHA" --limit 1 --json databaseId -q '.[0].databaseId'); [ -n "$RUN" ] && break; sleep 3; done; echo "run=$RUN"
timeout 600 gh run watch "$RUN" --exit-status --interval 15 > /dev/null; echo "ci exit=$?"
timeout 60 git fetch --tags origin
TAG=$(timeout 30 gh release list --limit 1 --json tagName -q '.[0].tagName'); echo "tag=$TAG"
[ "$(git rev-list -n 1 "$TAG")" = "$SHA" ] && echo tag-on-sha=yes || echo tag-on-sha=NO
```

Expected: a non-empty `run=`, `ci exit=0`, a `tag=` one patch above `v0.1.3` (for example `v0.1.4`), and `tag-on-sha=yes`. Call the tag `TAG` below. If CI fails or the tag is not on `SHA`, stop and report; do not deploy.

- [ ] **Step 5: Deploy the pinned release**

Run, replacing `TAG`:

```bash
timeout 590 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'cd /root/github-runner && GHR_VERSION=TAG timeout 570 bash setup.sh > /root/setup-TAG.log 2>&1; echo rc=$?; tail -8 /root/setup-TAG.log; systemctl is-active ghr; ghr version'
```

Expected: `rc=0`, a log line `installing ghr (TAG)`, a status table listing the four repos, `active`, and `TAG`.

**If any of Steps 6-9 fails after it changed the config,** restore it and report:

```bash
timeout 60 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'cp /root/config-before-plan2.yaml /etc/ghr/config.yaml && systemctl reload ghr && sleep 2 && diff /root/config-before-plan2.yaml /etc/ghr/config.yaml && echo restored=yes; ghr status | tail -5'
```

- [ ] **Step 6: Rejected save, then the leave guard discards it**

Run:

```bash
timeout 120 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 '
trap "tmux kill-session -t g 2>/dev/null" EXIT
w() { timeout 15 bash -c "until tmux capture-pane -p -t g | grep -qF -- \"$1\"; do sleep 0.3; done"; rc=$?; echo "found=$rc ($1)"; [ $rc -eq 0 ] || exit 1; }
click() { tmux send-keys -t g -l "$(printf "\e[<0;%d;%dM\e[<0;%d;%dm" $1 $2 $1 $2)"; }
before=$(sha256sum /etc/ghr/config.yaml)
tmux new-session -d -s g -x 120 -y 40 ghr; w "1 Dashboard"
click 5 5; w "─ General "
for i in $(seq 10); do tmux send-keys -t g Tab; done
tmux send-keys -t g f a s t Enter C-s; w "Save rejected"; w "runner_limits.cpu_quota must be a positive percentage"
[ "$(sha256sum /etc/ghr/config.yaml)" = "$before" ] || { echo "config changed"; exit 1; }
tmux send-keys -t g Tab Tab 1; w "Unsaved changes"
tmux send-keys -t g BTab Space; w "╭─ Repos "
tmux capture-pane -p -t g | grep -q "unsaved change" && { echo "edits survived"; exit 1; }
echo step=ok'
```

Expected: every `found=0`, then `step=ok`. This clicks Settings in the sidebar, tabs ten times to CPU quota, types a bad value, saves (`ctrl+s`), sees the rejection alert with `config.yaml` unchanged, then presses `1` from the darkcloud Labels tag list (which does not take digits): the leave guard opens, shift+tab focuses Discard and space presses it, and the Dashboard opens with no unsaved changes.

- [ ] **Step 7: Save a poll interval, then restore it**

Run, replacing `POLL0`:

```bash
timeout 120 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 '
trap "tmux kill-session -t g 2>/dev/null" EXIT
w() { timeout 15 bash -c "until tmux capture-pane -p -t g | grep -qF -- \"$1\"; do sleep 0.3; done"; rc=$?; echo "found=$rc ($1)"; [ $rc -eq 0 ] || exit 1; }
click() { tmux send-keys -t g -l "$(printf "\e[<0;%d;%dM\e[<0;%d;%dm" $1 $2 $1 $2)"; }
cfg() { timeout 10 bash -c "until grep -q \"^poll_interval: $1\$\" /etc/ghr/config.yaml; do sleep 0.3; done"; rc=$?; echo "config=$rc ($1)"; [ $rc -eq 0 ] || exit 1; }
tmux new-session -d -s g -x 120 -y 40 ghr; w "1 Dashboard"
click 5 5; w "─ General "
tmux send-keys -t g Tab Tab 1 5 s Enter; w "● 1 unsaved change"
tmux send-keys -t g C-s; w "Settings saved"; cfg 15s
timeout 10 bash -c "while tmux capture-pane -p -t g | grep -q \"unsaved change\"; do sleep 0.3; done" || { echo "still dirty after save"; exit 1; }
tmux send-keys -t g BTab; tmux send-keys -t g -l POLL0; tmux send-keys -t g Enter C-s; cfg POLL0
echo step=ok'
```

Expected: every `found=0` and `config=0`, then `step=ok`. After the first save the focus is on Start timeout, so shift+tab returns to Poll interval, and typing replaces its value.

- [ ] **Step 8: Add repository rejects a duplicate inside the dialog**

Run:

```bash
timeout 120 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 '
trap "tmux kill-session -t g 2>/dev/null" EXIT
w() { timeout 15 bash -c "until tmux capture-pane -p -t g | grep -qF -- \"$1\"; do sleep 0.3; done"; rc=$?; echo "found=$rc ($1)"; [ $rc -eq 0 ] || exit 1; }
click() { tmux send-keys -t g -l "$(printf "\e[<0;%d;%dM\e[<0;%d;%dm" $1 $2 $1 $2)"; }
tmux new-session -d -s g -x 120 -y 40 ghr; w "1 Dashboard"
click 5 5; w "─ General "
tmux send-keys -t g a; w "Allow public repo"
tmux send-keys -t g -l ghr-e2e; tmux send-keys -t g Enter Enter; w "repo ghr-e2e is already configured"; w "Allow public repo"
tmux send-keys -t g Escape
timeout 10 bash -c "while tmux capture-pane -p -t g | grep -q \"Allow public repo\"; do sleep 0.3; done" || { echo "dialog did not close"; exit 1; }
echo step=ok'
```

Expected: every `found=0`, then `step=ok`. `a` opens the dialog because Mode, a select, does not take letters. The name commits with `enter` (focus moves to Max) and the second `enter` presses Add; the daemon's rejection appears inside the still-open dialog; `esc` closes it.

- [ ] **Step 9: Remove ghr-e2e from Settings, then add it back through the dialog**

Skip this step if Step 2 found more than a name and a max in the `ghr-e2e` block. Run, replacing `MAX0`:

```bash
timeout 180 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 '
trap "tmux kill-session -t g 2>/dev/null" EXIT
w() { timeout 30 bash -c "until tmux capture-pane -p -t g | grep -qF -- \"$1\"; do sleep 0.3; done"; rc=$?; echo "found=$rc ($1)"; [ $rc -eq 0 ] || exit 1; }
click() { tmux send-keys -t g -l "$(printf "\e[<0;%d;%dM\e[<0;%d;%dm" $1 $2 $1 $2)"; }
tmux new-session -d -s g -x 120 -y 40 ghr; w "1 Dashboard"
click 5 5; w "─ General "
tmux send-keys -t g BTab BTab Enter; w "Remove repo ghr-e2e? Its running jobs finish first."
tmux send-keys -t g Enter
timeout 60 bash -c "while grep -q \"name: ghr-e2e\" /etc/ghr/config.yaml; do sleep 1; done" || { echo "not removed"; exit 1; }
timeout 20 bash -c "while tmux capture-pane -p -t g | grep -q \"─ ghr-e2e\"; do sleep 0.3; done" || { echo "card still shown"; exit 1; }
w "› [ + Add repository ]"
tmux send-keys -t g Enter; w "Allow public repo"
tmux send-keys -t g -l ghr-e2e; tmux send-keys -t g Enter; tmux send-keys -t g -l MAX0; tmux send-keys -t g Tab Tab Tab Enter
timeout 15 bash -c "until grep -q \"name: ghr-e2e\" /etc/ghr/config.yaml; do sleep 0.3; done" || { echo "not re-added"; exit 1; }
diff /root/config-before-plan2.yaml /etc/ghr/config.yaml || { echo "config differs from the backup"; exit 1; }
ghr status | grep ghr-e2e
echo step=ok'
```

Expected: every `found=0`, an empty `diff`, a `ghr-e2e … active 0/MAX0` line, then `step=ok`. Shift+tab twice from Mode wraps to `+ Add repository` and then to ghr-e2e's Remove (it is the last repo card); `enter` on the confirmation presses Yes. Once the daemon has finished the removal and the card has gone, focus rests on `+ Add repository`, whose `enter` opens the dialog. There the name commits with `enter`, `MAX0` is typed into the focused Max stepper, and three tabs (Labels, Allow public repo, Cancel) and `enter` press Add (in a dialog, `enter` on any button presses the primary). On any failure, run the restore command under Step 5.

- [ ] **Step 10: Record the result**

Run in `D:/Repositories/Personal/homelab`, replacing `TAG`:

```bash
bash /d/Repositories/Personal/darkraise-ai-plugins/plugins/dr-superpowers/scripts/register set docs/superpowers/registers/2026-10-04-ghr-tui-revamp.md 2 done --note "ghr TAG on the LXC: Settings form driven in tmux (mouse and keys): rejected save shows the alert, leave guard discards, poll_interval saved and restored, duplicate Add repository rejected inside the dialog, ghr-e2e removed and re-added; config.yaml identical to the pre-flight backup"
git add docs/superpowers/registers/2026-10-04-ghr-tui-revamp.md
git commit -m "docs(github-runner): record ghr TAG settings"
```

Expected: `register` exits 0 and the commit succeeds. If Step 9 was skipped, replace "ghr-e2e removed and re-added" in the note with "remove and re-add skipped: ghr-e2e has settings the dialog cannot set". Do not push homelab.
