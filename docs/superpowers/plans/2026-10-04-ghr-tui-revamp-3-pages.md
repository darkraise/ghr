# ghr TUI Revamp, Plan 3: The Remaining Pages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the Dashboard, Runners, runner detail and History pages and the Help dialog up to the web-style console Plan 2 started (stat tiles, header and row buttons, tab stops, a Runners page with a live log preview, a tabbed detail page, History filter selects, Help grouped by page), and release it as its own ghr version.

**Architecture:** Each page gets its own file in `internal/tui`: `dashboard.go`, `runners.go`, `detail.go` and `history.go`, which take over the page code still in `view.go`. A pointer-held `pageGroups` in `shell.go` gives the pages their focus groups, built from Plan 2's `ui.Group`, `ui.Button` and `ui.Select` plus two small widgets: `stop`, a tab stop for a table card, and `tabStrip`, the detail page's tabs. The detail popup becomes a page (`pageDetail`), and log polling follows whichever view shows a log. Buttons on the selected row are click targets that run the row's existing keys, so keys and clicks share one path.

**Tech Stack:** Go 1.26, Bubble Tea v1.3.10, lipgloss v1.1.0, bubbles v0.21.1, bubblezone v1.0.0, `charmbracelet/x/exp/golden` snapshots. No new modules.

**Spec:** docs/superpowers/specs/2026-10-04-ghr-tui-revamp-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 12 tasks, 2 heavy (Tasks 6 and 7, total 5) and delegated; the session implements the other 10 (highest total 4) and runs the whole-branch final review.

**Program:** docs/superpowers/specs/2026-10-04-ghr-tui-revamp-design.md — sub-project 3 of 3 — last

**Plan review:** 2026-10-04 — dr-superpowers:judge-opus — executability 17 / coherence 18 / coverage 16 / assumptions 17 (round 2)

## Global Constraints

- All code tasks run in `D:/Repositories/Personal/ghr` (module `github.com/darkraise/ghr`, `go 1.26`, remote `https://github.com/darkraise/ghr.git`). Start from `master` after Plan 2 (`docs/superpowers/plans/2026-10-04-ghr-tui-revamp-2-settings.md`) has been merged and released, on a feature branch `feat/pages-revamp`; never commit to `master` directly. Task 1 Step 1 checks that the starting code is the code this plan was written against.
- Every push to ghr's `master` publishes a release. Do not push, merge to `master`, tag or deploy without your human partner's explicit approval (Task 12 asks).
- Run every shell command in Git Bash (the Bash tool), never PowerShell: commands use POSIX redirection and `$?`.
- Copy every code block verbatim, including comments. Create or edit files with the Write or Edit tools, never shell heredocs. A "replace" block occurs exactly once in its file; replace it whole.
- Line endings are LF (`.gitattributes`: `* text=auto eol=lf`).
- Every `go test` carries `-count=1 -timeout 180s`. `-race` is unavailable on this Windows machine (no cgo).
- Before each commit: `gofmt -l cmd internal` prints nothing and `go vet ./...` passes.
- Commits: `<type>(<scope>): <subject>`, type one of feat|fix|docs|style|refactor|test|chore|perf, the whole first line ≤ 50 characters, imperative, English. Use each task's commit command as written.
- Do not upgrade Bubble Tea, lipgloss, bubbles or bubblezone, and add no module. No change to the daemon, its API or the CLI.
- Nothing is drawn over other content (spec, Decisions): an open dropdown pushes the table down, a modal replaces the whole screen. A zone ID never repeats within a frame, which is why only the focused card's selected row carries buttons.
- Text from the daemon or GitHub passes through `clean()` before it is rendered.
- Snapshot and view tests run under `termenv.Ascii`, so they assert glyphs (`›`, `●`, `▸`, `⚠`), never colours.

## Contracts

**From Plan 2** — the names below exist when this plan starts (Plan 2's Contracts section is their source); this plan uses them as stated here.
- **ui theme** (`internal/tui/ui/theme.go`, aliased in `internal/tui/styles.go`): styles `sGreen sAmber sRed sDim sBold sAccent sSel` (and `ui.Dialog`, a rounded border with padding 1 row and 2 columns); `cell(s string, w int) string` truncates with `…` and pads to w; `box(title string, w int, lines []string) string` draws a rounded card w wide with the title in its top border (`╭─ title ─…╮`); `stateStyle(state string) lipgloss.Style`; `spinnerFrames []string`.
- **ui controls** (`internal/tui/ui/control.go`, `button.go`, `select.go`):

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
  type Pressed struct{ ID string } // sent by a button; Model.Update hands it to pressed(id)
  ```

  Every control is a pointer that updates in place, and its `View` starts with a two-column focus marker: `› ` when focused, two spaces otherwise. `ui.NewButton(id, label string, kind ui.ButtonKind) *ui.Button` with kinds `ui.Primary` (renders `[ label ]`), `ui.Secondary` (`( label )`) and `ui.Danger` (`[ label ]` in red); fields `Label`, `Disabled`; `SetDisabled`; zone `id`; it takes `enter` and `space` and then sends `ui.Pressed{ID: id}`. `ui.NewSelect(id string, opts []ui.Option) *ui.Select` with `ui.Option{Value, Label, Desc string}`, the exported field `Options`, `Value() ui.Value` (`Text` is the selected option's `Value`), `SetValue(v ui.Value)` (selects the option whose `Value` is `v.Text`; an unknown value is ignored), `Open() bool`, `Selected() ui.Option`; zones `id` and `id/opt-N`; closed it renders `[ label ▾ ]` and takes `←`/`→` (cycle) and `enter`/`space` (open); open it renders one line per option below itself, takes every key but acts only on `↑`, `↓`, `enter` (pick) and `esc` (close), and takes every click.
- **ui focus** (`internal/tui/ui/focus.go`): `type Group struct` with `Set(items []Widget)` (keeps focus on the same ID while that widget is present and focusable, otherwise moves it to the next focusable widget), `Items() []Widget`, `FocusedID() string`, `Focused() Widget`, `Focus(id string) bool`, `Next()`, `Prev()` (both skip widgets that cannot take focus, and wrap), `Key(k tea.KeyMsg) (bool, tea.Cmd)` (gives `k` to the focused widget when it `TakesKey`), `Mouse(msg tea.MouseMsg) (bool, tea.Cmd)` (acts on left-button releases only: a capturing widget gets the click, otherwise the focusable widget whose `Hit` reports it takes focus and the click).
- **TUI pages and model** (`internal/tui/model.go`): `type page int` with `pageDashboard`, `pageRunners`, `pageHistory`, `pageSettings`; `pageNames = {"Dashboard", "Runners", "History", "Settings"}`; `Model.page`, `Model.focus pane` (`paneRepos`, `paneRunners`), `Model.connected bool`, `Model.st model.Status`, `Model.toast ui.Toast` (`Show(text string, isErr bool, now time.Time)`), `Model.copyFn func(string)`, `Model.now func() time.Time`, `Model.width`, `Model.height`; `func (m Model) switchPage(p page) (tea.Model, tea.Cmd)`; `selectedRunner() *model.InstanceStatus`; `instance(id string) *model.InstanceStatus`; `follow() tea.Cmd`, `fetchLog()`, `fetchSteps()`, `fetchContainers()`, `fetchHistory()`; `clean(s string) string` strips control characters and escape sequences.
- **TUI shell** (`internal/tui/shell.go`): `sidebarWidth = 16`, `wideMin = 100`; zones `nav/<page>` (`navZone(p page) string`), `nav/help`, `nav/quit`, `key-<key>`; `diskState() string` (`ok`, `warn`, `critical`); `pageHeader(w int) string` (the page title on the left, the page's buttons on the right); `type footerKey struct{ key, label string }`; `footerKeys() []footerKey`; `footerPress(k string) (tea.Model, tea.Cmd)` (navigation and control keys go through `handleKey`, letter keys straight to `press`); `keyMsg(k string) tea.KeyMsg`; `contentSize() (int, int)` (the content area's width and height below the page header); `layout(w int, body func(w, h int) string) string`.
- **TUI input** (`internal/tui/input.go`): `handleKey(k tea.KeyMsg)` (overlays first, then page handlers, then `press`), `press(key string)` (the page and global keys), `handleMouse(msg tea.MouseMsg)`, `move(d int) tea.Cmd`, `repoFocus() bool`, `runnerFocus() bool`, `enter()`.
- **TUI dialogs** (`internal/tui/dialogs.go`): `btnClose = "dialog/close"`; `openDialog(ov overlay, cancel string, buttons ...*ui.Button)`, `openConfirm(text string, action func() tea.Cmd)` (overlay `ovConfirm`), `openHelp()` (overlay `ovHelp`), `dialogKey(k)` (`enter` presses the primary button, `esc` cancel), `pressed(id string) (tea.Model, tea.Cmd)` (every `ui.Pressed` lands here), `modal(title, body string, buttons []*ui.Button, focused string, maxW int) string` (wraps a body wider than `maxW-6` columns), `withOverlay(base string, w int) string` (the dialog centred on a blank canvas `lipgloss.Height(base)` rows tall); `openAddRepo()` (overlay `ovAddRepo`); `leave(t leaveTarget)` with `leaveTarget{page page; quit bool}` (the unsaved-changes guard).
- **TUI test helpers** (`internal/tui/tui_test.go`, `settings_test.go`): `fakeClient` (records actions; `actions()`; fields `st *model.Status`, `steps`, `ctrs`, `calls`), `sampleStatus()` (repos darkcloud, darkmem and darkagents, the last paused; runners `a3f9c1` busy on darkcloud and `7be210` idle on darkmem), `newModel(c, w, h, st)`, `sampleModel(c, w, h)`, `feed(m, msgs...)`, `key(k)`, `keys(ks...)`, `run`, `ticks(m, n)`, `click(t, m, id)`, `zoneOf(t, m, id)`, `leftClick`, `now`, `slowPoll`, `lineWith(lines, s)`. `TestMain` sets the Ascii colour profile and `time.Local = time.UTC`.

**Dashboard** (`internal/tui/dashboard.go`, Task 1) — `func (m Model) statTiles(w int, compact bool) string`: four tiles, Running (`n / max`, or `n / ∞` in all mode, counting instances that are not cleaning), Queued jobs (the sum over repos), Repositories (`A active · P paused`; a repo being removed counts as paused) and Disk (styled by `diskState`). `func (m Model) dashboard(w, h int) string` draws the tiles boxed, or on one line when `m.width < wideMin` or the screen is too short, above the Repositories, Runners and Activity cards; `reposLines`, `eventLines` move here unchanged in behaviour.

**Page focus** (Tasks 2-3, extended by Tasks 6-9):
- `type stop struct{ id string }` in `shell.go`: a `ui.Widget` that is focusable but takes no keys and no clicks, used as a tab stop for a table card.
- `type pageGroups struct` in `shell.go`, held as `Model.groups *pageGroups` so focus survives Bubble Tea copying the Model, built by `newPageGroups()`. Its fields grow by task: `dash ui.Group` (Task 2); `add, pauseAll *ui.Button` (Task 3); `detail ui.Group; copyURL, stopRunner *ui.Button` (Task 6); `tabs *tabStrip` (Task 7); `back *ui.Button` (Task 8); `hist ui.Group; histRepo, histResult *ui.Select` (Task 9).
- Dashboard IDs `dashRepos = "dash/repos"`, `dashRunners = "dash/runners"` (Task 2), `dashAdd = "dash/add"`, `dashPauseAll = "dash/pauseall"` (Task 3). Tab order: `+ Add`, `Pause all`, the Repositories card, the Runners card; focus starts on Repositories.
- In `dashboard.go`: `func (m *Model) focusCard(p pane)` and `func (m Model) dashKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd)` (Task 2); `allPaused() bool` (every repo not being removed is paused), `dashButtons() string` (disables both buttons while unreachable and re-sets the group so a disabled button hands focus on), `cardTitle(id, title string) string` (prefixes `› ` on the focused card) and `onCard() bool` (Task 3). `repoFocus` and `runnerFocus` require `onCard()`, so row keys do nothing while a header button has focus; `move` ignores the arrows then, and the footer shows `enter press`, `tab next`, `? help`, `q quit`.

**Row buttons** (`internal/tui/view.go`, Task 4) — IDs `rowPause = "row/pause"`, `rowRemove = "row/remove"`, `rowLogs = "row/logs"`, `rowStop = "row/stop"` (Task 9 adds `rowCopy = "row/copy"` in `history.go`); `func rowButtons(buttons ...*ui.Button) string`; `func (m Model) selectedRow(id, s, buttons, tail string, w int) string` renders the row text `s` cut to make room, then the buttons, then `tail`, the row's fixed right-hand columns (the runner's elapsed time, History's result and duration; empty for repos), so those keep their place. Only the focused card's selected row (`repoFocus`/`runnerFocus`) carries buttons. `handleMouse` turns a click on a row button into its key through `press`: `p`, `d`, `l`, `x`, and `enter` for `rowCopy`.

**Pages** — `func (m Model) runnersPage(w, h int) string` in `runners.go` (Task 5; the table, its one focusable element, is always titled `› Runners` and keeps its natural height; a log preview box zoned `log` takes the rest, at least 3 lines, and reads `Log preview — no runner selected` when no runner is selected), `detailPage(w, h int)` in `detail.go` (Task 6), `historyPage(w, h int)` in `history.go` (Task 9). `pageBody` in `view.go` dispatches to them. From Task 11, `layout` passes every page body through `fitLines(s string, n int) string`, which pads or cuts it to the content height.

**Detail page** (`internal/tui/detail.go`, Task 6, extended by Tasks 7-8):
- `pageDetail` follows `pageSettings` in the `page` constants; it is not in `pageNames`, and `navPage() page` maps it to `pageRunners` for the sidebar and tab row. `ovDetail` goes.
- Model fields `detailFrom page` (where `esc` returns), `detailSnap model.InstanceStatus` (the runner as last seen in `/status`), from Task 7 `detailScroll int` (the first line shown of the Steps or Containers list), and from Task 8 `detailDone time.Time` (when the runner left `/status`; zero while it runs).
- IDs `detailCopy = "detail/copy"`, `detailStop = "detail/stop"`, `detailTabs = "detail/tabs"` (Task 7), `detailBack = "detail/back"` (Task 8).
- Methods: `openDetail(id string) (tea.Model, tea.Cmd)`, which Task 7 changes to `openDetail(id string, tab int)`; `closeDetail()`; `detailButtons() string` (also sets the focus order: Copy run URL when the run URL is known, Stop runner, Back to runners while finished, the tab strip); `detailSummary() string` (the runner ID, repo and state pass through `clean`); `stepLines()`, `containerLines() []string`; `stopRunner(id string, busy bool)` (asks first when busy); `detailKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd)`; `detailPressed(id string)`; from Task 7 `detailTop(w int) []string` (the lines above the active tab; Task 8 adds the finished alert), `detailList() []string` (the Steps or Containers lines; nil on Log) and `scrollDetail(d int)` (moves `detailScroll`, bounded by the list's length); `finished() bool` (Task 8). On the detail page every footer hint takes the key's path (`footerPress` → `handleKey`), so the footer's `x` asks to stop like the key.

**Detail tabs** (Task 7) — `type tabStrip struct{ active int }` (a `ui.Widget` that takes no keys; a click on a tab selects it); `tabSteps`, `tabLog`, `tabContainers` (0-2); `tabNames`; `tabZone(i int) string` → `"detail/tabs/<i>"`. `←`/`→` on the detail page switch tabs; a tab change by key or click resets `detailScroll` and calls `follow()`. `↑`/`↓`, the wheel and `pgup`/`pgdn` scroll the Steps and Containers lists. `func (m Model) logTarget() string` is the runner whose log a visible view shows: the selected runner on the Runners page, the detail page's runner on its Log tab, otherwise none; `follow()` and the tick's log polling use it.

**History filters** (`internal/tui/history.go`, Task 9) — IDs `histRepoSel = "hist/repo"`, `histResultSel = "hist/result"`, `histTable = "hist/table"`; `resultOptions`; `histFilters()` brings the selects up to date with `Model.histRepo`/`histConcl`, which stay the source of truth because `r` and `c` still cycle them (repo labels pass through `clean`); `histApply() tea.Cmd` adopts the selects' values and refetches; `histKey(k)`; `histFooterKeys() []footerKey` (an open filter: `up move`, `enter pick`, `esc close`; a closed one: `left prev`, `right next`, `enter open`, `tab next`; the table: `r repo`, `c result`, `enter copy URL`, `tab next`). Tab order: Repo, Result, the table; focus starts on the table, where `enter` copies the run URL; `↑`/`↓` move the table only while it has focus; `pgup`/`pgdn` move the selection 10 rows. The filter bar is cut to the page width.

**Help** (`internal/tui/dialogs.go`, Task 10) — `helpText(inner int) string` (six groups in three columns of 23, or one column below 73); `Model.helpScroll int`; `helpLines(w int) ([]string, int)` (the wrapped text and how many lines fit: all, or the room less one for a hint); `helpBody(w int) string` (a window with `↑↓ scroll · lines A-B of N` when Help is taller than the screen); while Help is open, `↑`/`↓`/`j`/`k`/`pgup`/`pgdn` scroll it.

**TUI test helpers** added here — `detailModel(t, c)` (Task 6, `detail_test.go`); `fakeClient.hist` (History returns the entries matching its filters) and `fakeClient.histReqs` (each request as `"repo|conclusion"`), `historyModel(t) (Model, *fakeClient)`, `lastReq(c)` (Task 9); `pageModels map[string]func(w int) Model`, `fits(t, name, v, w, h)` (Task 11, `tui_test.go`).

## Assumptions (evidence)

- Every task's code was written and tested in a scratch copy of ghr holding Plan 2's end state, on 2026-10-04. Each task's end state passed `go test ./...`, `go vet ./...` and `gofmt -l`, and a script that replays this plan's create and replace blocks from that state reproduced each task's files exactly (the snapshot files excepted: the tasks generate them with `-update`).
- That scratch state is Plan 2's plan replayed verbatim (Plan 2's own replay check reported no differences). If Plan 2's execution changed any file this plan edits, Task 1 Step 1 reports it and execution stops: unverified until Plan 2 has been executed, and Task 1 Step 1 verifies it.
- bubblezone v1.0.0 pairs markers by ID, so zones nest (`scanner.go:56-87`), and `Get` of an unknown ID returns nil with a nil-safe `InBounds` (`zoneinfo.go:22-37`); Plan 2's `settle` and `zoneOf` helpers make click tests wait for the current frame.
- `ansi.Truncate` keeps escape sequences past the cut (`x/ansi@v0.11.5/truncate.go:86-90`), so truncating detail lines, the History filter bar or a page header never orphans a zone marker. `fitLines` cuts content taller than the screen. The one view that can be taller is an open History Repo filter with more repos than rows: its option list is cut at the bottom row, because Plan 2's `ui.Select` has no scrolling viewport and this plan does not add one (`↑`/`↓` still move the cursor through every option). Task 11's test opens it with 23 repos at 80×22.
- `ui.Group.Mouse` passes only left-button releases to widgets (`internal/tui/ui/focus.go`, Plan 2 Task 7), so `tabStrip.Update` sees clicks only.
- The Help dialog's frame takes 8 rows (border 2, padding 2, title and blank 2, blank and buttons 2: `ui.Dialog` in `internal/tui/ui/theme.go`, `modal` in `dialogs.go`), so its body gets 14 lines on the 22-row minimum screen; Task 10's three columns fit in 73 columns, which the dialog has from 80 columns up, and below that Help scrolls.
- On the LXC (read over SSH, 2026-10-04): ghr v0.1.3, mode `queue`, `global_max: 2`, `poll_interval: 10s`, no runner alive while idle, repos darkcloud, darkmem, darkagents and `ghr-e2e` (`max: 3`); `ghr history` lists finished jobs of `ghr-e2e` and `ghr`. By the time this plan runs, Plan 2 will have deployed its own release there; Task 12 Step 2 re-reads the state.
- `darkraise/ghr-e2e` has a `single` workflow (`workflow_dispatch`, one job `one` on `[self-hosted, homelab]` that sleeps 30 s) (read with `gh api`, 2026-10-04). Its last run took 37 s (`ghr history`), so a dispatched run gives the acceptance a busy runner for about half a minute; the queue-mode daemon starts one runner for it within one poll interval.
- Task 12's tmux keystrokes and click positions were dry-run against the scratch copy on 2026-10-04 (a test feeding the same keys and clicks to the model at 120×40): every awaited text appeared, the sidebar's History row holds column 5 row 4, and the footer's `right next tab` holds column 30 row 40.
- ghr's `ci` workflow (`.github/workflows/ci.yml`) runs on pushes to `master` and creates the release with `--target "$GITHUB_SHA"`; `gh` 2.88.1 is installed here (2026-10-04) and supports `gh run list --workflow --commit`, `gh release list --json` and `gh workflow run -R`.
- dr-superpowers:executing-plans runs the whole-branch final review once every task is complete (`skills/executing-plans/SKILL.md`, Final Review), so Task 12, which must release only reviewed code, runs that review itself in Step 1 when the ledger has no `Final review: clean` line for the branch tip.
- plan-lint reports one ERROR on this plan: the Program line says `last` while register row 2 (Plan 2) is still `planned`. It clears when Plan 2's acceptance marks row 2 done, which Task 1 Step 1 requires before this plan starts; it is not a defect of this plan's text.
- The owner declined the Codex executor for this programme, so no task carries an `**Executor:**` line.
- Open for the owner (plan review round 2 kept it Important): `[ + Add ]` and `[ Pause all ]` sit on the Dashboard's page header line, where spec §1 puts each page's buttons, rather than inside the Repositories card's border as §3's "the header" could also mean (the reviewer notes `box` already draws its title in the top border, so the buttons could go there instead; that is a one-task change if the owner prefers it). Other interpretations, flagged for review: a row button is a click target, not a tab stop (owner's choice, 2026-10-04: tab stops are header buttons, cards and filters); on the detail page `↑`/`↓` scroll the active list (it has no selection); the detail page's `x` and Stop runner do nothing once the runner has finished; Back to runners goes to the Runners page, while `esc` returns to the page the detail was opened from; `l` acts only on a focused runner row, so on Settings it no longer opens the leave guard; History's `pgup`/`pgdn` move the selection 10 rows; Help's one-line descriptions and notes are this plan's wording.

## Task index

1. Dashboard stat tiles
2. Dashboard cards become tab stops
3. Dashboard header buttons and tab order
4. Buttons on the selected row
5. Runners page with a log preview
6. Runner detail page
7. Detail tabs and log following
8. Detail page after the runner finishes
9. History filter bar
10. Help grouped by page
11. Page snapshots and narrow screens
12. Release and LXC acceptance

---

### Task 1: Dashboard stat tiles

**Files:**
- Create: `internal/tui/dashboard.go` (`reposLines`, `eventLines` and `dashboard` move here from `view.go`)
- Modify: `internal/tui/view.go` (the three functions leave)
- Test: `internal/tui/dashboard_test.go`
- Modify (regenerated): `internal/tui/testdata/TestDashboardGolden/{120,80}.golden`

**Interfaces:**
- Consumes: Plan 2's **TUI shell** (`diskState`, `wideMin`) and **ui theme**
- Produces: **Dashboard** (`statTiles`, `dashboard`; see Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Check the starting point**

This plan's replace blocks were written against Plan 2's code exactly as Plan 2 states it. Run in `D:/Repositories/Personal/ghr`:

```bash
git checkout master && git status --short && git checkout -b feat/pages-revamp
bad=0
while read -r want path; do
  [ "$(git rev-parse "HEAD:$path" 2>/dev/null)" = "$want" ] || { echo "differs: $path"; bad=1; }
done <<'EOF'
e03af89f7b5ccf0e71277296358b1006b6db613f internal/tui/dialogs.go
089f75b9a596636132903796047414fd56d3df92 internal/tui/input.go
17027b07b70ee0c8d913467df3a4e2f4d08b7300 internal/tui/model.go
9f9631e044058cc20ff8beb904bd31c80fd61d9d internal/tui/settings_test.go
205fcebf5643efb8b0593e3a6cebdcedeb2f7593 internal/tui/shell.go
0bab472b7c9773dcb917961fcac8a0463d062bc2 internal/tui/shell_test.go
a5904d9ab4b7770829271f5eb6e15e83d51208b6 internal/tui/tui_test.go
56d189edc477ccae7df878d7a8f2ab459c771e52 internal/tui/view.go
fd460f590ac991bac8defb206c4e2c9f5933c515 internal/tui/testdata/TestDashboardGolden/120.golden
fd621afb0a1fe86e5214cdfd3dbbd1747ea0f59c internal/tui/testdata/TestDashboardGolden/80.golden
EOF
for f in dashboard detail history runners; do
  for p in "$f.go" "${f}_test.go"; do git cat-file -e "HEAD:internal/tui/$p" 2>/dev/null && { echo "exists: $p"; bad=1; }; done
done
git cat-file -e HEAD:internal/tui/dialogs_test.go 2>/dev/null && { echo "exists: dialogs_test.go"; bad=1; }
echo "start-check=$bad"
```

Expected: `git status` prints nothing, the branch `feat/pages-revamp` is created, and the last line is `start-check=0`. Any `differs:` or `exists:` line means Plan 2 was merged with changes this plan does not know about, so its replace blocks may not apply: stop and report the lines to your human partner; do not re-anchor blocks yourself.

- [ ] **Step 2: Write the failing tests**

Create `internal/tui/dashboard_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/config"
)

func TestStatTiles(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	v := m.View()
	for _, want := range []string{"╭─ Running ", "2 / 3", "╭─ Queued jobs ", "╭─ Repositories ", "2 active · 1 paused", "╭─ Disk ", "61%"} {
		if !strings.Contains(v, want) {
			t.Errorf("tiles missing %q", want)
		}
	}
	if got := m.statTiles(100, true); !strings.Contains(got, "Queued jobs 3") {
		t.Fatalf("queued is the sum over repos: %q", got)
	}
	m.st.Mode = config.ModeAll
	m.st.Repos[0].Removing, m.st.Repos[0].Paused = true, true
	got := m.statTiles(100, true)
	for _, want := range []string{"Running 2 / ∞", "Repositories 1 active · 2 paused"} {
		if !strings.Contains(got, want) {
			t.Errorf("compact tiles %q missing %q", got, want)
		}
	}
}

// Under 100 columns, or when rows run short, the tiles take one line.
func TestStatTilesCollapse(t *testing.T) {
	if v := sampleModel(&fakeClient{}, 80, 40).View(); strings.Contains(v, "╭─ Running ") || !strings.Contains(v, "Running 2 / 3") {
		t.Fatalf("tiles not compact under 100 columns:\n%s", v)
	}
	// The Activity card gives way first: at 23 rows the tiles are still boxed
	// and Activity is down to its 3 lines; one row fewer collapses the tiles.
	v := sampleModel(&fakeClient{}, 120, 23).View()
	lines := strings.Split(v, "\n")
	top := lineWith(lines, "╭─ Activity ")
	if !strings.Contains(v, "╭─ Running ") || top < 0 || !strings.Contains(lines[top+4], "╰") {
		t.Fatalf("at 23 rows the tiles should stay boxed with a 3-line Activity card:\n%s", v)
	}
	if v := sampleModel(&fakeClient{}, 120, 22).View(); strings.Contains(v, "╭─ Running ") || !strings.Contains(v, "Running 2 / 3") {
		t.Fatalf("tiles not compact on a short screen:\n%s", v)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `m.statTiles undefined (type Model has no field or method statTiles)`.

- [ ] **Step 4: Write the implementation**

Create `internal/tui/dashboard.go`:

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
)

// statTiles renders the Dashboard's four stat tiles across w columns: boxed
// side by side, or on one line when compact.
func (m Model) statTiles(w int, compact bool) string {
	running, queued, active, paused := 0, 0, 0, 0
	for _, i := range m.st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	for _, r := range m.st.Repos {
		queued += r.Queued
		if r.Paused || r.Removing {
			paused++ // a repo being removed stays paused
		} else {
			active++
		}
	}
	limit := fmt.Sprint(m.st.GlobalMax)
	if m.st.Mode == config.ModeAll {
		limit = "∞"
	}
	diskStyle := sDim
	switch m.diskState() {
	case "warn":
		diskStyle = sAmber
	case "critical":
		diskStyle = sRed
	}
	tiles := []struct{ title, value string }{
		{"Running", fmt.Sprintf("%d / %s", running, limit)},
		{"Queued jobs", fmt.Sprint(queued)},
		{"Repositories", fmt.Sprintf("%d active · %d paused", active, paused)},
		{"Disk", diskStyle.Render(fmt.Sprintf("%d%%", m.st.DiskPct))},
	}
	if compact {
		var parts []string
		for _, t := range tiles {
			parts = append(parts, sDim.Render(t.title)+" "+sBold.Render(t.value))
		}
		return cell(" "+strings.Join(parts, "   "), w)
	}
	tw := (w - 3) / 4 // three one-column gaps
	var boxes []string
	for i, t := range tiles {
		bw := tw
		if i == len(tiles)-1 {
			bw = w - 3*tw - 3 // the last tile takes the remainder
		}
		if i > 0 {
			boxes = append(boxes, " ")
		}
		boxes = append(boxes, box(t.title, bw, []string{sBold.Render(t.value)}))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
}

func (m Model) reposLines(w int) []string {
	narrow := w < 80
	inner := w - 4
	hdr := fmt.Sprintf("  %-14s %-10s %-5s %-6s", "REPO", "STATE", "RUN", "QUEUE")
	if !narrow {
		hdr += " LAST JOB"
	}
	lines := []string{sDim.Render(hdr)}
	for i, r := range m.st.Repos {
		state, st := "active", "active"
		dot := "●"
		if r.Paused {
			state, st, dot = "paused", "paused", "◌"
		}
		if r.Removing {
			state, st, dot = "removing", "removing", "◌"
		}
		if r.Error != "" {
			state, st, dot = "error", "error", "✖"
		}
		queue := "–"
		if r.Queued > 0 {
			queue = sAmber.Render(fmt.Sprintf("⧗ %d", r.Queued))
		}
		sel := "  "
		if i == m.repoSel && m.focus == paneRepos {
			sel = "▸ "
		}
		line := sel + cell(r.Name, 14) + " " + stateStyle(st).Render(cell(dot+" "+state, 10)) + " " +
			cell(fmt.Sprintf("%d/%s", r.Active, maxText(r.Max)), 5) + " " + cell(queue, 6)
		if !narrow {
			last := sDim.Render("–")
			if j := r.LastJob; j != nil {
				icon, style := "✔", sGreen
				if j.Conclusion != "success" {
					icon, style = "✖", sRed
				}
				last = style.Render(icon) + fmt.Sprintf(" #%s %s  %s", j.RunNumber, j.JobName, sDim.Render(ago(m.now().Sub(j.FinishedAt))))
			}
			line += " " + last
		}
		if r.Error != "" && !narrow {
			line += "  " + sRed.Render(r.Error)
		}
		lines = append(lines, m.row(fmt.Sprintf("repo-%d", i), i == m.repoSel && m.focus == paneRepos, line, inner))
	}
	if len(m.st.Repos) == 0 {
		lines = append(lines, sDim.Render("  no repos configured — press a to add one"))
	}
	return lines
}

func (m Model) eventLines(n, w int) []string {
	inner := w - 4
	end := len(m.events) - m.eventScroll
	if end < 0 {
		end = 0
	}
	start := end - n
	if start < 0 {
		start = 0
	}
	var lines []string
	for _, e := range m.events[start:end] {
		icon, style := eventStyle(e.Level)
		lines = append(lines, cell(sDim.Render(e.Time.Local().Format("15:04:05"))+"  "+style.Render(icon)+" "+cell(e.Repo, 11)+" "+e.Msg, inner))
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

func (m Model) dashboard(w, h int) string {
	repoLines, runnerLines := m.reposLines(w), m.runnersLines(w)
	// When rows run short, the Activity card shrinks first (to 3 lines), then
	// the tiles collapse to one line; under 100 columns they are one line anyway.
	tilesH := 3
	if m.width < wideMin || tilesH+len(repoLines)+len(runnerLines)+4+5 > h {
		tilesH = 1
	}
	room := h - tilesH - 9 // two table borders plus an Activity card of at least 3 lines
	nRepo, nRun := len(repoLines), len(runnerLines)
	if nRepo+nRun > room {
		nRepo = min(nRepo, max(room/2, room-nRun))
		nRun = room - nRepo
	}
	tiles := m.statTiles(w, tilesH == 1)
	repos := box("Repositories", w, fit(repoLines, 1, m.repoSel, nRepo))
	runners := box("Runners", w, fit(runnerLines, 1, m.runnerSel, nRun))
	rest := h - tilesH - lipgloss.Height(repos) - lipgloss.Height(runners) - 2
	if w < 80 || rest < 3 {
		rest = 3
	}
	events := zone.Mark("events", box("Activity", w, m.eventLines(rest, w)))
	return strings.Join([]string{tiles, repos, runners, events}, "\n")
}
```

In `internal/tui/view.go`, delete:

```go
func (m Model) reposLines(w int) []string {
	narrow := w < 80
	inner := w - 4
	hdr := fmt.Sprintf("  %-14s %-10s %-5s %-6s", "REPO", "STATE", "RUN", "QUEUE")
	if !narrow {
		hdr += " LAST JOB"
	}
	lines := []string{sDim.Render(hdr)}
	for i, r := range m.st.Repos {
		state, st := "active", "active"
		dot := "●"
		if r.Paused {
			state, st, dot = "paused", "paused", "◌"
		}
		if r.Removing {
			state, st, dot = "removing", "removing", "◌"
		}
		if r.Error != "" {
			state, st, dot = "error", "error", "✖"
		}
		queue := "–"
		if r.Queued > 0 {
			queue = sAmber.Render(fmt.Sprintf("⧗ %d", r.Queued))
		}
		sel := "  "
		if i == m.repoSel && m.focus == paneRepos {
			sel = "▸ "
		}
		line := sel + cell(r.Name, 14) + " " + stateStyle(st).Render(cell(dot+" "+state, 10)) + " " +
			cell(fmt.Sprintf("%d/%s", r.Active, maxText(r.Max)), 5) + " " + cell(queue, 6)
		if !narrow {
			last := sDim.Render("–")
			if j := r.LastJob; j != nil {
				icon, style := "✔", sGreen
				if j.Conclusion != "success" {
					icon, style = "✖", sRed
				}
				last = style.Render(icon) + fmt.Sprintf(" #%s %s  %s", j.RunNumber, j.JobName, sDim.Render(ago(m.now().Sub(j.FinishedAt))))
			}
			line += " " + last
		}
		if r.Error != "" && !narrow {
			line += "  " + sRed.Render(r.Error)
		}
		lines = append(lines, m.row(fmt.Sprintf("repo-%d", i), i == m.repoSel && m.focus == paneRepos, line, inner))
	}
	if len(m.st.Repos) == 0 {
		lines = append(lines, sDim.Render("  no repos configured — press a to add one"))
	}
	return lines
}

```

In `internal/tui/view.go`, delete:

```go
func (m Model) eventLines(n, w int) []string {
	inner := w - 4
	end := len(m.events) - m.eventScroll
	if end < 0 {
		end = 0
	}
	start := end - n
	if start < 0 {
		start = 0
	}
	var lines []string
	for _, e := range m.events[start:end] {
		icon, style := eventStyle(e.Level)
		lines = append(lines, cell(sDim.Render(e.Time.Local().Format("15:04:05"))+"  "+style.Render(icon)+" "+cell(e.Repo, 11)+" "+e.Msg, inner))
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

```

In `internal/tui/view.go`, delete:

```go
}

func (m Model) dashboard(w, h int) string {
	repoLines, runnerLines := m.reposLines(w), m.runnersLines(w)
	room := h - 9 // two table borders plus an Events box of at least 3 lines
	nRepo, nRun := len(repoLines), len(runnerLines)
	if nRepo+nRun > room {
		nRepo = min(nRepo, max(room/2, room-nRun))
		nRun = room - nRepo
	}
	repos := box("Repos", w, fit(repoLines, 1, m.repoSel, nRepo))
	runners := box("Runners", w, fit(runnerLines, 1, m.runnerSel, nRun))
	rest := h - lipgloss.Height(repos) - lipgloss.Height(runners) - 2
	if w < 80 || rest < 3 {
		rest = 3
	}
	events := zone.Mark("events", box("Events", w, m.eventLines(rest, w)))
	return strings.Join([]string{repos, runners, events}, "\n")
```

- [ ] **Step 5: Regenerate the snapshots**

Run: `go test ./internal/tui/ -run TestDashboardGolden -update -count=1 -timeout 180s`
Expected: the command exits 0 and writes both Dashboard snapshots. Open each file and compare it with the snapshot below (trailing spaces are trimmed here; the files keep them).

`internal/tui/testdata/TestDashboardGolden/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
▌ 1 Dashboard    Dashboard
  2 Runners      ╭─ Running ─────────────╮ ╭─ Queued jobs ─────────╮ ╭─ Repositories ────────╮ ╭─ Disk ────────────────╮
  3 History      │ 2 / 3                 │ │ 3                     │ │ 2 active · 1 paused   │ │ 61%                   │
  4 Settings     ╰───────────────────────╯ ╰───────────────────────╯ ╰───────────────────────╯ ╰───────────────────────╯
                 ╭─ Repositories ──────────────────────────────────────────────────────────────────────────────────────╮
                 │   REPO           STATE      RUN   QUEUE  LAST JOB                                                   │
                 │ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint  2m ago                                        │
                 │   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago                                 │
                 │   darkagents     ◌ paused   0/1   ⧗ 1    –                                                          │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Runners ───────────────────────────────────────────────────────────────────────────────────────────╮
                 │   ID       REPO         STATE       JOB                                               ELAPSED       │
                 │   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412                           12m04s        │
                 │   7be210   darkmem      ○ idle      –                                                 1m30s         │
                 │   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)                                      │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Activity ──────────────────────────────────────────────────────────────────────────────────────────╮
                 │ HH:MM:SS  ✔ darkcloud   #411 lint success 2m10s  cleanup: 3 ctrs                                    │
                 │ HH:MM:SS  ▶ darkcloud   spawned a3f9c1 (2.330.0)                                                    │
                 │ HH:MM:SS  ⚠             disk 81% > high-water 80% — pruned build cache                              │
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
 Running 2 / 3   Queued jobs 3   Repositories 2 active · 1 paused   Disk 61%
╭─ Repositories ───────────────────────────────────────────────────────────────╮
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
╭─ Activity ───────────────────────────────────────────────────────────────────╮
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

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run "TestStatTiles|TestDashboardGolden"`
Expected: PASS for `TestStatTiles`, `TestStatTilesCollapse` and both subtests of `TestDashboardGolden`.

- [ ] **Step 7: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/dashboard.go internal/tui/dashboard_test.go internal/tui/testdata/TestDashboardGolden/120.golden internal/tui/testdata/TestDashboardGolden/80.golden internal/tui/view.go
git commit -m "feat(tui): add dashboard stat tiles"
```

### Task 2: Dashboard cards become tab stops

**Files:**
- Modify: `internal/tui/shell.go` (`stop`, `pageGroups`, `newPageGroups`)
- Modify: `internal/tui/model.go` (the `groups` field)
- Modify: `internal/tui/dashboard.go` (`focusCard`, `dashKey`)
- Test: `internal/tui/dashboard_test.go`

**Interfaces:**
- Consumes: Plan 2's **ui focus** (`ui.Group`) and **ui controls** (`ui.Widget`)
- Produces: **Page focus** (`stop`, `pageGroups`, `Model.groups`, `focusCard`, `dashKey`; see Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

This task adds the focus model only; nothing calls `dashKey` until Task 3 routes keys to it, so the screen does not change yet.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/dashboard_test.go`, replace:

```go
		t.Fatalf("tiles not compact on a short screen:\n%s", v)
	}
}
```

with:

```go
		t.Fatalf("tiles not compact on a short screen:\n%s", v)
	}
}

// The cards are tab stops; focus and the row keys' pane move together.
func TestDashboardCardFocus(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	if m.groups.dash.FocusedID() != dashRepos || m.focus != paneRepos {
		t.Fatalf("start: focus %q pane %v", m.groups.dash.FocusedID(), m.focus)
	}
	ok, mm, _ := m.dashKey(key("tab"))
	if m = mm.(Model); !ok || m.groups.dash.FocusedID() != dashRunners || m.focus != paneRunners {
		t.Fatalf("tab: ok %v focus %q pane %v", ok, m.groups.dash.FocusedID(), m.focus)
	}
	if ok, _, _ = m.dashKey(key("p")); ok {
		t.Fatal("dashKey took a row key")
	}
	m.focusCard(paneRepos)
	if m.groups.dash.FocusedID() != dashRepos || m.focus != paneRepos {
		t.Fatalf("focusCard: focus %q pane %v", m.groups.dash.FocusedID(), m.focus)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `m.groups undefined (type Model has no field or method groups)`, `undefined: dashRepos`, `m.dashKey undefined` and `m.focusCard undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/shell.go`, replace:

```go
	zone "github.com/lrstanley/bubblezone"
)
```

with:

```go
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/tui/ui"
)
```

In `internal/tui/shell.go`, replace:

```go
func navZone(p page) string { return "nav/" + strings.ToLower(pageNames[p]) }

```

with:

```go
func navZone(p page) string { return "nav/" + strings.ToLower(pageNames[p]) }

// stop is a tab stop that is not a control, such as a table card: it takes
// no keys and no clicks of its own; the page acts on whatever has focus.
type stop struct{ id string }

func (s stop) ID() string                           { return s.id }
func (s stop) Update(tea.Msg) (ui.Control, tea.Cmd) { return s, nil }
func (s stop) View(bool, int) string                { return "" }
func (s stop) Focusable() bool                      { return true }
func (s stop) TakesKey(tea.KeyMsg) bool             { return false }
func (s stop) Capturing() bool                      { return false }
func (s stop) Hit(tea.MouseMsg) bool                { return false }
func (s stop) Blur()                                {}
func (s stop) SetDisabled(bool)                     {}

// pageGroups holds the focus groups of the pages other than Settings. The
// Model keeps it by pointer so focus survives Bubble Tea copying the Model.
type pageGroups struct {
	dash ui.Group
}

const (
	dashRepos   = "dash/repos"
	dashRunners = "dash/runners"
)

func newPageGroups() *pageGroups {
	g := &pageGroups{}
	g.dash.Set([]ui.Widget{stop{dashRepos}, stop{dashRunners}})
	g.dash.Focus(dashRepos)
	return g
}

```

In `internal/tui/model.go`, replace:

```go
	settings *settingsPage
	events   []model.Event
```

with:

```go
	settings *settingsPage
	groups   *pageGroups
	events   []model.Event
```

In `internal/tui/model.go`, replace:

```go
		c: c, now: time.Now, width: 120, height: 40, settings: newSettingsPage(),
```

with:

```go
		c: c, now: time.Now, width: 120, height: 40, settings: newSettingsPage(), groups: newPageGroups(),
```

In `internal/tui/dashboard.go`, replace:

```go
	"strings"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
```

with:

```go
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
```

In `internal/tui/dashboard.go`, replace:

```go
)

```

with:

```go
)

// focusCard focuses a Dashboard card and points the row keys at it.
func (m *Model) focusCard(p pane) {
	m.focus = p
	if p == paneRepos {
		m.groups.dash.Focus(dashRepos)
	} else {
		m.groups.dash.Focus(dashRunners)
	}
}

// dashKey handles tab, shift+tab and the header buttons' keys on the
// Dashboard. It reports false for keys the page and global keys handle.
func (m Model) dashKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	g := m.groups
	if ok, cmd := g.dash.Key(k); ok {
		return true, m, cmd
	}
	switch k.String() {
	case "tab":
		g.dash.Next()
	case "shift+tab":
		g.dash.Prev()
	default:
		return false, m, nil
	}
	switch g.dash.FocusedID() {
	case dashRepos:
		m.focus = paneRepos
	case dashRunners:
		m.focus = paneRunners
	}
	return true, m, nil
}

```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run TestDashboardCardFocus`
Expected: PASS.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/dashboard.go internal/tui/dashboard_test.go internal/tui/model.go internal/tui/shell.go
git commit -m "feat(tui): make dashboard cards tab stops"
```

### Task 3: Dashboard header buttons and tab order

**Files:**
- Modify: `internal/tui/dashboard.go`
- Modify: `internal/tui/shell.go` (`pageGroups`, `pageHeader`)
- Modify: `internal/tui/input.go` (key routing, `move`, `handleMouse`)
- Modify: `internal/tui/dialogs.go` (`pressed`)
- Test: `internal/tui/dashboard_test.go`
- Test: `internal/tui/tui_test.go` (`TestTabMovesFocusNotPage`)
- Modify (regenerated): `internal/tui/testdata/TestDashboardGolden/{120,80}.golden`

**Interfaces:**
- Consumes: **Page focus**, Plan 2's **TUI dialogs** (`pressed`, `openAddRepo`)
- Produces: `dashAdd`, `dashPauseAll`, `allPaused`, `onCard` (see Contracts, **Page focus**)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/tui/dashboard_test.go`, replace:

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

In `internal/tui/dashboard_test.go`, replace:

```go
		t.Fatalf("focusCard: focus %q pane %v", m.groups.dash.FocusedID(), m.focus)
	}
}
```

with:

```go
		t.Fatalf("focusCard: focus %q pane %v", m.groups.dash.FocusedID(), m.focus)
	}
}

// Tab cycles the Dashboard's tab stops: the two cards, then the header buttons.
func TestDashboardTabOrder(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 40)
	if v := m.View(); !strings.Contains(v, "╭─ › Repositories ") {
		t.Fatalf("Repositories card not focused at start:\n%s", v)
	}
	m = feed(m, key("tab"))
	if v := m.View(); m.focus != paneRunners || !strings.Contains(v, "╭─ › Runners ") {
		t.Fatalf("tab: focus %v\n%s", m.focus, v)
	}
	m = feed(m, key("tab"))
	if v := m.View(); !strings.Contains(v, "› [ + Add ]") {
		t.Fatalf("tab did not reach + Add:\n%s", v)
	}
	if m = feed(m, keys("p", "x")...); len(c.actions()) != 0 || m.overlay != ovNone {
		t.Fatalf("row keys acted while a header button had focus: %v", c.actions())
	}
	if m = feed(m, key("enter")); m.overlay != ovAddRepo {
		t.Fatal("enter on + Add did not open the dialog")
	}
	m = feed(m, keys("esc", "tab", " ")...) // Pause all
	if got := strings.Join(c.actions(), "|"); got != "pause-all" {
		t.Fatalf("actions %q", got)
	}
}

// While a header button has focus, the arrows move no table and the footer
// offers the button's keys; a button disabled under focus hands focus on.
func TestDashboardHeaderFocus(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), keys("tab", "tab")...) // + Add
	if m = feed(m, key("down")); m.repoSel != 0 || m.runnerSel != 0 {
		t.Fatalf("down moved repo %d runner %d", m.repoSel, m.runnerSel)
	}
	if v := m.View(); !strings.Contains(v, "enter press") || strings.Contains(v, "p pause") {
		t.Fatalf("footer does not follow the focused button:\n%s", v)
	}
	m = feed(m, statusMsg{err: errors.New("connection refused")})
	if v := m.View(); m.groups.dash.FocusedID() != dashRepos || !strings.Contains(v, "╭─ › Repositories") {
		t.Fatalf("focus stayed on a disabled button: %q", m.groups.dash.FocusedID())
	}
}

// The header offers Resume all once every repo not being removed is paused.
func TestDashboardHeaderButtons(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 40)
	if v := m.View(); !strings.Contains(v, "[ + Add ]") || !strings.Contains(v, "( Pause all )") {
		t.Fatalf("header buttons missing:\n%s", v)
	}
	if m = click(t, m, dashAdd); m.overlay != ovAddRepo {
		t.Fatal("clicking + Add did not open the dialog")
	}
	m = feed(m, key("esc"))
	st := sampleStatus()
	st.Repos[0].Paused, st.Repos[1].Paused = true, true
	st.Repos[2].Removing = true // still paused, and ignored
	m = feed(m, statusMsg{st: st})
	if v := m.View(); !strings.Contains(v, "( Resume all )") {
		t.Fatalf("Resume all not offered:\n%s", v)
	}
	m = click(t, m, dashPauseAll)
	if got := strings.Join(c.actions(), "|"); got != "resume-all" {
		t.Fatalf("actions %q", got)
	}
}
```

In `internal/tui/tui_test.go`, replace:

```go
	m = run(t, m, "tab")
	if m.focus != paneRepos {
		t.Fatalf("second tab: focus %v", m.focus)
```

with:

```go
	m = run(t, m, "tab", "tab", "tab") // + Add, Pause all, back to Repositories
	if m.focus != paneRepos || m.groups.dash.FocusedID() != dashRepos {
		t.Fatalf("tab cycle: focus %v on %q", m.focus, m.groups.dash.FocusedID())
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: dashAdd` and `undefined: dashPauseAll`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/dashboard.go`, replace:

```go
)

```

with:

```go
)

// allPaused reports whether every repo not being removed is paused: then the
// header offers Resume all. Repos being removed stay paused, so they do not count.
func (m Model) allPaused() bool {
	seen := false
	for _, r := range m.st.Repos {
		if r.Removing {
			continue
		}
		if !r.Paused {
			return false
		}
		seen = true
	}
	return seen
}

// dashButtons renders the Dashboard header's buttons.
func (m Model) dashButtons() string {
	g := m.groups
	g.pauseAll.Label = "Pause all"
	if m.allPaused() {
		g.pauseAll.Label = "Resume all"
	}
	g.add.SetDisabled(!m.connected)
	g.pauseAll.SetDisabled(!m.connected)
	g.dash.Set(g.dash.Items()) // a focused button that was just disabled hands focus on
	f := g.dash.FocusedID()
	return g.add.View(f == dashAdd, 0) + "  " + g.pauseAll.View(f == dashPauseAll, 0)
}

// cardTitle marks the focused card with the focus glyph.
func (m Model) cardTitle(id, title string) string {
	if m.groups.dash.FocusedID() == id {
		return "› " + title
	}
	return title
}

```

In `internal/tui/dashboard.go`, replace:

```go
	return true, m, nil
}
```

with:

```go
	return true, m, nil
}

// onCard reports whether a Dashboard card, not a header button, has focus.
func (m Model) onCard() bool {
	id := m.groups.dash.FocusedID()
	return id != dashAdd && id != dashPauseAll
}
```

In `internal/tui/dashboard.go`, replace:

```go
	repos := box("Repositories", w, fit(repoLines, 1, m.repoSel, nRepo))
	runners := box("Runners", w, fit(runnerLines, 1, m.runnerSel, nRun))
```

with:

```go
	repos := box(m.cardTitle(dashRepos, "Repositories"), w, fit(repoLines, 1, m.repoSel, nRepo))
	runners := box(m.cardTitle(dashRunners, "Runners"), w, fit(runnerLines, 1, m.runnerSel, nRun))
```

In `internal/tui/shell.go`, replace:

```go
	dash ui.Group
}

const (
	dashRepos   = "dash/repos"
	dashRunners = "dash/runners"
)

func newPageGroups() *pageGroups {
	g := &pageGroups{}
	g.dash.Set([]ui.Widget{stop{dashRepos}, stop{dashRunners}})
```

with:

```go
	dash          ui.Group
	add, pauseAll *ui.Button
}

const (
	dashAdd      = "dash/add"
	dashPauseAll = "dash/pauseall"
	dashRepos    = "dash/repos"
	dashRunners  = "dash/runners"
)

func newPageGroups() *pageGroups {
	g := &pageGroups{
		add:      ui.NewButton(dashAdd, "+ Add", ui.Primary),
		pauseAll: ui.NewButton(dashPauseAll, "Pause all", ui.Secondary),
	}
	g.dash.Set([]ui.Widget{g.add, g.pauseAll, stop{dashRepos}, stop{dashRunners}})
```

In `internal/tui/shell.go`, replace:

```go
	if m.page == pageSettings && !m.connected {
		right = sRed.Render("reconnecting")
```

with:

```go
	switch {
	case m.page == pageSettings && !m.connected:
		right = sRed.Render("reconnecting")
	case m.page == pageDashboard:
		right = m.dashButtons()
```

In `internal/tui/shell.go`, replace:

```go
		return m.settingsFooterKeys()
	}
```

with:

```go
		return m.settingsFooterKeys()
	}
	if !m.onCard() {
		return []footerKey{{"enter", "press"}, {"tab", "next"}, {"?", "help"}, {"q", "quit"}}
	}
```

In `internal/tui/input.go`, replace:

```go
	}
	return m.press(key)
```

with:

```go
	}
	if m.page == pageDashboard {
		if ok, mm, cmd := m.dashKey(k); ok {
			return mm, cmd
		}
	}
	return m.press(key)
```

In `internal/tui/input.go`, replace:

```go
	case "tab", "shift+tab":
		// tab moves focus within a page; on the Dashboard, between its two tables.
		if m.page == pageDashboard {
			m.focus = 1 - m.focus
		}
	case "left", "h":
		m.focus = paneRepos
	case "right":
		m.focus = paneRunners
```

with:

```go
	case "left", "h":
		m.focusCard(paneRepos)
	case "right":
		m.focusCard(paneRunners)
```

In `internal/tui/input.go`, replace:

```go
	case m.page == pageSettings:
```

with:

```go
	case m.page == pageSettings, m.page == pageDashboard && !m.onCard():
```

In `internal/tui/input.go`, replace:

```go
	all := len(m.st.Repos) > 0
	for _, r := range m.st.Repos {
		all = all && r.Paused
	}
	if all {
```

with:

```go
	if m.allPaused() {
```

In `internal/tui/input.go`, replace:

```go
	return m.page == pageRunners || (m.page == pageDashboard && m.focus == paneRunners)
}

func (m Model) repoFocus() bool {
	return m.page == pageDashboard && m.focus == paneRepos
```

with:

```go
	return m.page == pageRunners || (m.page == pageDashboard && m.onCard() && m.focus == paneRunners)
}

func (m Model) repoFocus() bool {
	return m.page == pageDashboard && m.onCard() && m.focus == paneRepos
```

In `internal/tui/input.go`, replace:

```go
		if ok, mm, cmd := m.settingsMouse(msg); ok {
			return mm, cmd
		}
	}
```

with:

```go
		if ok, mm, cmd := m.settingsMouse(msg); ok {
			return mm, cmd
		}
	}
	if m.overlay == ovNone && m.page == pageDashboard {
		if ok, cmd := m.groups.dash.Mouse(msg); ok {
			return m, cmd
		}
	}
```

In `internal/tui/input.go`, replace:

```go
		m.repoSel, m.focus = i, paneRepos
		return m, nil
	}
	if i := hit("runner", len(m.st.Instances)); i >= 0 {
		m.runnerSel, m.focus = i, paneRunners
```

with:

```go
		m.repoSel = i
		m.focusCard(paneRepos)
		return m, nil
	}
	if i := hit("runner", len(m.st.Instances)); i >= 0 {
		m.runnerSel = i
		m.focusCard(paneRunners)
```

In `internal/tui/dialogs.go`, replace:

```go
	case setAddRepo:
		return m.openAddRepo()
```

with:

```go
	case setAddRepo, dashAdd:
		return m.openAddRepo()
	case dashPauseAll:
		return m, m.togglePauseAll()
```

- [ ] **Step 4: Regenerate the snapshots**

Run: `go test ./internal/tui/ -run TestDashboardGolden -update -count=1 -timeout 180s`
Expected: the command exits 0 and writes both Dashboard snapshots. Open each file and compare it with the snapshot below (trailing spaces are trimmed here; the files keep them).

`internal/tui/testdata/TestDashboardGolden/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
▌ 1 Dashboard    Dashboard                                                                    [ + Add ]    ( Pause all )
  2 Runners      ╭─ Running ─────────────╮ ╭─ Queued jobs ─────────╮ ╭─ Repositories ────────╮ ╭─ Disk ────────────────╮
  3 History      │ 2 / 3                 │ │ 3                     │ │ 2 active · 1 paused   │ │ 61%                   │
  4 Settings     ╰───────────────────────╯ ╰───────────────────────╯ ╰───────────────────────╯ ╰───────────────────────╯
                 ╭─ › Repositories ────────────────────────────────────────────────────────────────────────────────────╮
                 │   REPO           STATE      RUN   QUEUE  LAST JOB                                                   │
                 │ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint  2m ago                                        │
                 │   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago                                 │
                 │   darkagents     ◌ paused   0/1   ⧗ 1    –                                                          │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Runners ───────────────────────────────────────────────────────────────────────────────────────────╮
                 │   ID       REPO         STATE       JOB                                               ELAPSED       │
                 │   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412                           12m04s        │
                 │   7be210   darkmem      ○ idle      –                                                 1m30s         │
                 │   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)                                      │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Activity ──────────────────────────────────────────────────────────────────────────────────────────╮
                 │ HH:MM:SS  ✔ darkcloud   #411 lint success 2m10s  cleanup: 3 ctrs                                    │
                 │ HH:MM:SS  ▶ darkcloud   spawned a3f9c1 (2.330.0)                                                    │
                 │ HH:MM:SS  ⚠             disk 81% > high-water 80% — pruned build cache                              │
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
Dashboard                                             [ + Add ]    ( Pause all )
 Running 2 / 3   Queued jobs 3   Repositories 2 active · 1 paused   Disk 61%
╭─ › Repositories ─────────────────────────────────────────────────────────────╮
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
╭─ Activity ───────────────────────────────────────────────────────────────────╮
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

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run "TestDashboardTabOrder|TestDashboardHeaderButtons|TestDashboardHeaderFocus|TestDashboardCardFocus|TestTabMovesFocusNotPage|TestDashboardGolden"`
Expected: PASS for all six tests, including both subtests of `TestDashboardGolden`.

- [ ] **Step 6: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/dashboard.go internal/tui/dashboard_test.go internal/tui/dialogs.go internal/tui/input.go internal/tui/shell.go internal/tui/testdata/TestDashboardGolden/120.golden internal/tui/testdata/TestDashboardGolden/80.golden internal/tui/tui_test.go
git commit -m "feat(tui): add dashboard header buttons"
```

### Task 4: Buttons on the selected row

**Files:**
- Modify: `internal/tui/view.go` (`rowButtons`, `selectedRow`, the `row/…` IDs, `runnerLine`, `runnersLines`)
- Modify: `internal/tui/dashboard.go` (`reposLines`)
- Modify: `internal/tui/input.go` (`handleMouse`)
- Test: `internal/tui/dashboard_test.go`
- Modify (regenerated): `internal/tui/testdata/TestDashboardGolden/{120,80}.golden`

**Interfaces:**
- Consumes: **Page focus** (`onCard`), Plan 2's **ui controls** (`ui.NewButton`)
- Produces: **Row buttons** (see Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

In `internal/tui/dashboard_test.go`, replace:

```go
	if got := strings.Join(c.actions(), "|"); got != "resume-all" {
		t.Fatalf("actions %q", got)
	}
}
```

with:

```go
	if got := strings.Join(c.actions(), "|"); got != "resume-all" {
		t.Fatalf("actions %q", got)
	}
}

// The focused card's selected row carries buttons that run the row keys.
// The selected row's buttons belong to the focused card: they go while a
// header button has focus, and a selected runner keeps its elapsed time.
func TestRowButtonsFollowFocus(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	m.focusCard(paneRunners)
	if v := m.View(); !strings.Contains(v, "( Logs )") || !strings.Contains(v, "[ Stop ] 12m04s") {
		t.Fatalf("selected runner row:\n%s", v)
	}
	if v := feed(m, key("tab")).View(); strings.Contains(v, "( Logs )") || strings.Contains(v, "( Pause )") {
		t.Fatalf("row buttons shown while + Add has focus:\n%s", v)
	}
}

func TestRowButtons(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 40)
	v := m.View()
	if !strings.Contains(v, "▸ darkcloud") || !strings.Contains(v, "( Pause )") || !strings.Contains(v, "[ Remove ]") || strings.Contains(v, "( Logs )") {
		t.Fatalf("repo row buttons:\n%s", v)
	}
	m = click(t, m, rowPause)
	if m = click(t, m, rowRemove); m.overlay != ovConfirm {
		t.Fatal("Remove did not ask first")
	}
	m = feed(m, key("esc"))
	m = feed(m, keys("down", "down")...) // darkagents is paused
	if v := m.View(); !strings.Contains(v, "( Resume )") {
		t.Fatalf("paused repo row:\n%s", v)
	}
	m = feed(m, key("right"))
	if v := m.View(); !strings.Contains(v, "( Logs )") || !strings.Contains(v, "[ Stop ]") || strings.Contains(v, "( Resume )") {
		t.Fatalf("runner row buttons:\n%s", v)
	}
	if m = click(t, m, rowStop); m.overlay != ovConfirm { // a3f9c1 is busy
		t.Fatal("Stop on a busy runner did not ask first")
	}
	m = feed(m, key("enter"))
	if got := strings.Join(c.actions(), "|"); got != "pause darkcloud|kill a3f9c1" {
		t.Fatalf("actions %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: rowPause`, `undefined: rowRemove` and `undefined: rowStop`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/view.go`, replace:

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

In `internal/tui/view.go`, replace:

```go
}

func (m Model) runnerLine(i int, r model.InstanceStatus, selected bool, inner int) string {
	state := r.State
```

with:

```go
}

// The selected row's buttons. Only the focused card's selected row shows
// them, so each zone appears once per frame.
const (
	rowPause  = "row/pause"
	rowRemove = "row/remove"
	rowLogs   = "row/logs"
	rowStop   = "row/stop"
)

// rowButtons renders buttons for the right end of a selected row.
func rowButtons(buttons ...*ui.Button) string {
	var parts []string
	for _, b := range buttons {
		parts = append(parts, b.View(false, 0))
	}
	return strings.Join(parts, "")
}

// selectedRow renders a selected row of width w: the text s, the row's
// buttons, then tail, the row's fixed right-hand columns, which keep their
// place under the header. s gives way to the buttons, so its last column
// should be the flexible one.
func (m Model) selectedRow(id, s, buttons, tail string, w int) string {
	bw, tw := ansi.StringWidth(buttons), ansi.StringWidth(tail)
	return zone.Mark(id, sSel.Render(ansi.Strip(cell(s, w-bw-tw)))) + buttons + sSel.Render(ansi.Strip(tail))
}

func (m Model) runnerLine(i int, r model.InstanceStatus, selected bool, inner int) string {
	state := r.State
```

In `internal/tui/view.go`, replace:

```go
		cell(job, inner-50) + " " + cell(elapsed, 8)
	return m.row(fmt.Sprintf("runner-%d", i), selected, line, inner)
```

with:

```go
		cell(job, inner-50)
	tail := " " + cell(elapsed, 8)
	if selected {
		return m.selectedRow(fmt.Sprintf("runner-%d", i), line,
			rowButtons(ui.NewButton(rowLogs, "Logs", ui.Secondary), ui.NewButton(rowStop, "Stop", ui.Danger)), tail, inner)
	}
	return m.row(fmt.Sprintf("runner-%d", i), false, line+tail, inner)
```

In `internal/tui/view.go`, replace:

```go
		selected := i == m.runnerSel && (m.focus == paneRunners || m.page == pageRunners)
```

with:

```go
		selected := i == m.runnerSel && m.runnerFocus()
```

In `internal/tui/dashboard.go`, replace:

```go
	"github.com/darkraise/ghr/internal/config"
)
```

with:

```go
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/tui/ui"
)
```

In `internal/tui/dashboard.go`, replace:

```go
		sel := "  "
		if i == m.repoSel && m.focus == paneRepos {
```

with:

```go
		selected := i == m.repoSel && m.repoFocus()
		sel := "  "
		if selected {
```

In `internal/tui/dashboard.go`, replace:

```go
		lines = append(lines, m.row(fmt.Sprintf("repo-%d", i), i == m.repoSel && m.focus == paneRepos, line, inner))
```

with:

```go
		if selected {
			pause := ui.NewButton(rowPause, "Pause", ui.Secondary)
			if r.Paused {
				pause.Label = "Resume"
			}
			lines = append(lines, m.selectedRow(fmt.Sprintf("repo-%d", i), line, rowButtons(pause, ui.NewButton(rowRemove, "Remove", ui.Danger)), "", inner))
			continue
		}
		lines = append(lines, m.row(fmt.Sprintf("repo-%d", i), false, line, inner))
```

In `internal/tui/input.go`, replace:

```go
	}
	hit := func(prefix string, n int) int {
```

with:

```go
	}
	// A row button runs its key, so the row keys and the buttons stay one path.
	for id, k := range map[string]string{rowPause: "p", rowRemove: "d", rowLogs: "l", rowStop: "x"} {
		if zone.Get(id).InBounds(msg) {
			return m.press(k)
		}
	}
	hit := func(prefix string, n int) int {
```

- [ ] **Step 4: Regenerate the snapshots**

Run: `go test ./internal/tui/ -run TestDashboardGolden -update -count=1 -timeout 180s`
Expected: the command exits 0 and writes both Dashboard snapshots. Open each file and compare it with the snapshot below (trailing spaces are trimmed here; the files keep them).

`internal/tui/testdata/TestDashboardGolden/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
▌ 1 Dashboard    Dashboard                                                                    [ + Add ]    ( Pause all )
  2 Runners      ╭─ Running ─────────────╮ ╭─ Queued jobs ─────────╮ ╭─ Repositories ────────╮ ╭─ Disk ────────────────╮
  3 History      │ 2 / 3                 │ │ 3                     │ │ 2 active · 1 paused   │ │ 61%                   │
  4 Settings     ╰───────────────────────╯ ╰───────────────────────╯ ╰───────────────────────╯ ╰───────────────────────╯
                 ╭─ › Repositories ────────────────────────────────────────────────────────────────────────────────────╮
                 │   REPO           STATE      RUN   QUEUE  LAST JOB                                                   │
                 │ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint  2m ago                  ( Pause )  [ Remove ] │
                 │   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago                                 │
                 │   darkagents     ◌ paused   0/1   ⧗ 1    –                                                          │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Runners ───────────────────────────────────────────────────────────────────────────────────────────╮
                 │   ID       REPO         STATE       JOB                                               ELAPSED       │
                 │   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412                           12m04s        │
                 │   7be210   darkmem      ○ idle      –                                                 1m30s         │
                 │   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)                                      │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Activity ──────────────────────────────────────────────────────────────────────────────────────────╮
                 │ HH:MM:SS  ✔ darkcloud   #411 lint success 2m10s  cleanup: 3 ctrs                                    │
                 │ HH:MM:SS  ▶ darkcloud   spawned a3f9c1 (2.330.0)                                                    │
                 │ HH:MM:SS  ⚠             disk 81% > high-water 80% — pruned build cache                              │
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
Dashboard                                             [ + Add ]    ( Pause all )
 Running 2 / 3   Queued jobs 3   Repositories 2 active · 1 paused   Disk 61%
╭─ › Repositories ─────────────────────────────────────────────────────────────╮
│   REPO           STATE      RUN   QUEUE  LAST JOB                            │
│ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint…  ( Pause )  [ Remove ] │
│   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago          │
│   darkagents     ◌ paused   0/1   ⧗ 1    –                                   │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Runners ────────────────────────────────────────────────────────────────────╮
│   ID       REPO         STATE       JOB                        ELAPSED       │
│   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412    12m04s        │
│   7be210   darkmem      ○ idle      –                          1m30s         │
│   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)               │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Activity ───────────────────────────────────────────────────────────────────╮
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

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run "TestRowButtons|TestDashboardGolden"`
Expected: PASS for `TestRowButtons`, `TestRowButtonsFollowFocus` and both subtests of `TestDashboardGolden`.

- [ ] **Step 6: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/dashboard.go internal/tui/dashboard_test.go internal/tui/input.go internal/tui/testdata/TestDashboardGolden/120.golden internal/tui/testdata/TestDashboardGolden/80.golden internal/tui/view.go
git commit -m "feat(tui): add buttons to the selected row"
```

### Task 5: Runners page with a log preview

**Files:**
- Create: `internal/tui/runners.go` (`runnerLine`, `runnersLines` move here from `view.go`)
- Modify: `internal/tui/view.go`
- Test: `internal/tui/runners_test.go`

**Interfaces:**
- Consumes: **Row buttons**
- Produces: `runnersPage` (see Contracts, **Pages**)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/runners_test.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/model"
)

func TestRunnersPage(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), key("2"))
	v := m.View()
	for _, want := range []string{"╭─ › Runners ", "▸ a3f9c1", "( Logs )", "[ Stop ]", "⧗ waiting", "Log preview — a3f9c1 (following)", `a3f9c1 after ""`} {
		if !strings.Contains(v, want) {
			t.Errorf("Runners page missing %q", want)
		}
	}
}

// On a short screen the log preview gives way first, down to 3 lines; then
// the table scrolls to keep the selection visible.
func TestRunnersPageShortScreen(t *testing.T) {
	st := sampleStatus()
	st.Instances = nil
	for i := 0; i < 20; i++ {
		st.Instances = append(st.Instances, model.InstanceStatus{ID: fmt.Sprintf("run%02d", i), Repo: "darkmem", State: "idle", Since: now})
	}
	m := feed(newModel(&fakeClient{}, 120, 22, st), key("2"))
	for i := 0; i < 19; i++ {
		m = feed(m, key("down"))
	}
	v := m.View()
	lines := strings.Split(v, "\n")
	logTop := lineWith(lines, "Log preview — run19")
	if logTop < 0 || !strings.Contains(v, "▸ run19") || lipgloss.Height(v) > 22 || !strings.Contains(lines[logTop+4], "╰") {
		t.Fatalf("preview not at 3 lines or selection hidden (%d lines):\n%s", lipgloss.Height(v), v)
	}
}

func TestRunnersPageLogScrolls(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), key("2"))
	for i := 0; i < 60; i++ {
		m.logText += fmt.Sprintf("line %02d\n", i)
	}
	z := zoneOf(t, m, "log")
	m = feed(m, tea.MouseMsg{X: z.StartX + 2, Y: z.StartY + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if m.logScroll != 3 {
		t.Fatalf("wheel: scroll %d", m.logScroll)
	}
	if m = feed(m, key("pgup")); m.logScroll != 13 || !strings.Contains(m.View(), "line 46") {
		t.Fatalf("pgup: scroll %d\n%s", m.logScroll, m.View())
	}
}

// Once every runner has gone, the preview no longer claims to follow one.
func TestRunnersPageWithoutRunners(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), key("2"))
	st := sampleStatus()
	st.Instances = nil
	v := feed(m, statusMsg{st: st}).View()
	if !strings.Contains(v, "Log preview — no runner selected") || strings.Contains(v, "a3f9c1 after") {
		t.Fatalf("stale preview:\n%s", v)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: `TestRunnersPage` fails with `Runners page missing "╭─ › Runners "` and `Runners page missing "Log preview — a3f9c1 (following)"` , `TestRunnersPageShortScreen` with `preview not at 3 lines or selection hidden`, and `TestRunnersPageWithoutRunners` with `stale preview`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/runners.go`:

```go
package tui

import (
	"fmt"
	"strings"

	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

func (m Model) runnerLine(i int, r model.InstanceStatus, selected bool, inner int) string {
	state := r.State
	icon := "○"
	switch state {
	case "busy":
		icon = spinnerFrames[m.frame%len(spinnerFrames)]
	case "starting":
		icon = "◔"
	case "cleaning":
		icon = "♻"
	}
	job, elapsed := sDim.Render("–"), dur(m.now().Sub(r.Since))
	if r.Job != nil {
		job = r.Job.Name
		if r.Job.RunNumber != "" {
			job += "  #" + r.Job.RunNumber
		}
		if !r.Job.StartedAt.IsZero() {
			elapsed = dur(m.now().Sub(r.Job.StartedAt))
		}
	}
	sel := "  "
	if selected {
		sel = "▸ "
	}
	line := sel + cell(r.ID, 8) + " " + cell(r.Repo, 12) + " " + stateStyle(state).Render(cell(icon+" "+state, 11)) + " " +
		cell(job, inner-50)
	tail := " " + cell(elapsed, 8)
	if selected {
		return m.selectedRow(fmt.Sprintf("runner-%d", i), line,
			rowButtons(ui.NewButton(rowLogs, "Logs", ui.Secondary), ui.NewButton(rowStop, "Stop", ui.Danger)), tail, inner)
	}
	return m.row(fmt.Sprintf("runner-%d", i), false, line+tail, inner)
}

func (m Model) runnersLines(w int) []string {
	inner := w - 4
	lines := []string{sDim.Render(fmt.Sprintf("  %-8s %-12s %-11s %-*s %s", "ID", "REPO", "STATE", inner-50, "JOB", "ELAPSED"))}
	for i, r := range m.st.Instances {
		selected := i == m.runnerSel && m.runnerFocus()
		lines = append(lines, m.runnerLine(i, r, selected, inner))
	}
	for _, r := range m.st.Repos {
		if r.Paused || r.Queued == 0 || r.Max == 0 || r.Active < r.Max {
			continue
		}
		lines = append(lines, sAmber.Render(fmt.Sprintf("  %-8s %-12s %-11s %d jobs queued (repo cap %d)", "–", r.Name, "⧗ waiting", r.Queued, r.Max)))
	}
	if len(m.st.Instances) == 0 {
		lines = append(lines, sDim.Render("  no runners — they start when jobs are queued"))
	}
	return lines
}

// runnersPage is the full-width runners table with a live log preview of the
// selected runner below it. When rows run short the preview gives way first,
// down to 3 lines; only then does the table scroll.
func (m Model) runnersPage(w, h int) string {
	all := m.runnersLines(w)
	logH := h - (len(all) + 2) - 2
	n := len(all)
	if logH < 3 {
		logH = 3
		n = max(h-2-logH-2, 2)
	}
	// The table is the page's one focusable element, so it is always marked.
	runners := box("› Runners", w, fit(all, 1, m.runnerSel, n))
	title := "Log preview — no runner selected"
	var lines []string
	// The log of a runner that has gone stays in logID; it is no longer followed.
	if m.logID != "" && m.selectedRunner() != nil {
		title = "Log preview — " + m.logID + " (following)"
		all := strings.Split(strings.TrimRight(m.logText, "\n"), "\n")
		end := max(len(all)-m.logScroll, 0)
		lines = all[max(end-logH, 0):end]
	}
	for len(lines) < logH {
		lines = append(lines, "")
	}
	return runners + "\n" + zone.Mark("log", box(title, w, lines))
}
```

In `internal/tui/view.go`, replace:

```go
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/model"
```

with:

```go
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

```

In `internal/tui/view.go`, replace:

```go
		return m.runnersTab(w, h)
```

with:

```go
		return m.runnersPage(w, h)
```

In `internal/tui/view.go`, delete:

```go
func (m Model) runnerLine(i int, r model.InstanceStatus, selected bool, inner int) string {
	state := r.State
	icon := "○"
	switch state {
	case "busy":
		icon = spinnerFrames[m.frame%len(spinnerFrames)]
	case "starting":
		icon = "◔"
	case "cleaning":
		icon = "♻"
	}
	job, elapsed := sDim.Render("–"), dur(m.now().Sub(r.Since))
	if r.Job != nil {
		job = r.Job.Name
		if r.Job.RunNumber != "" {
			job += "  #" + r.Job.RunNumber
		}
		if !r.Job.StartedAt.IsZero() {
			elapsed = dur(m.now().Sub(r.Job.StartedAt))
		}
	}
	sel := "  "
	if selected {
		sel = "▸ "
	}
	line := sel + cell(r.ID, 8) + " " + cell(r.Repo, 12) + " " + stateStyle(state).Render(cell(icon+" "+state, 11)) + " " +
		cell(job, inner-50)
	tail := " " + cell(elapsed, 8)
	if selected {
		return m.selectedRow(fmt.Sprintf("runner-%d", i), line,
			rowButtons(ui.NewButton(rowLogs, "Logs", ui.Secondary), ui.NewButton(rowStop, "Stop", ui.Danger)), tail, inner)
	}
	return m.row(fmt.Sprintf("runner-%d", i), false, line+tail, inner)
}

func (m Model) runnersLines(w int) []string {
	inner := w - 4
	lines := []string{sDim.Render(fmt.Sprintf("  %-8s %-12s %-11s %-*s %s", "ID", "REPO", "STATE", inner-50, "JOB", "ELAPSED"))}
	for i, r := range m.st.Instances {
		selected := i == m.runnerSel && m.runnerFocus()
		lines = append(lines, m.runnerLine(i, r, selected, inner))
	}
	for _, r := range m.st.Repos {
		if r.Paused || r.Queued == 0 || r.Max == 0 || r.Active < r.Max {
			continue
		}
		lines = append(lines, sAmber.Render(fmt.Sprintf("  %-8s %-12s %-11s %d jobs queued (repo cap %d)", "–", r.Name, "⧗ waiting", r.Queued, r.Max)))
	}
	if len(m.st.Instances) == 0 {
		lines = append(lines, sDim.Render("  no runners — they start when jobs are queued"))
	}
	return lines
}

```

In `internal/tui/view.go`, delete:

```go
}

func (m Model) runnersTab(w, h int) string {
	runners := box("Runners", w, fit(m.runnersLines(w), 1, m.runnerSel, max(h/2-2, 2)))
	logH := h - lipgloss.Height(runners) - 2
	if logH < 3 {
		logH = 3
	}
	title := "Log — no runner selected"
	var lines []string
	if m.logID != "" {
		title = "Log " + m.logID + " (following)"
		all := strings.Split(strings.TrimRight(m.logText, "\n"), "\n")
		end := len(all) - m.logScroll
		if end < 0 {
			end = 0
		}
		start := end - logH
		if start < 0 {
			start = 0
		}
		lines = all[start:end]
	}
	for len(lines) < logH {
		lines = append(lines, "")
	}
	return runners + "\n" + zone.Mark("log", box(title, w, lines))
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run TestRunnersPage`
Expected: PASS for `TestRunnersPage`, `TestRunnersPageShortScreen`, `TestRunnersPageLogScrolls` and `TestRunnersPageWithoutRunners`.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/runners.go internal/tui/runners_test.go internal/tui/view.go
git commit -m "feat(tui): add runners page with log preview"
```

### Task 6: Runner detail page

**Files:**
- Create: `internal/tui/detail.go`
- Modify: `internal/tui/model.go` (`pageDetail`; `ovDetail` goes)
- Modify: `internal/tui/shell.go` (`pageGroups`, `pageHeader`, `navPage`, footer, `footerPress`)
- Modify: `internal/tui/input.go`
- Modify: `internal/tui/dialogs.go` (`pressed`)
- Modify: `internal/tui/view.go` (the old detail overlay goes)
- Test: `internal/tui/detail_test.go`
- Test: `internal/tui/shell_test.go`
- Test: `internal/tui/tui_test.go`

**Interfaces:**
- Consumes: **Page focus**, **Row buttons**, Plan 2's **TUI dialogs** (`openConfirm`)
- Produces: **Detail page** (see Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

The detail popup (`ovDetail`) becomes a page of its own, `pageDetail`, which the sidebar and tab row show under Runners. `esc` returns to the page it was opened from.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/detail_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

// detailModel is the sample model on a3f9c1's detail page, opened from the
// Runners page; the run's URL is known.
func detailModel(t *testing.T, c *fakeClient) Model {
	t.Helper()
	st := sampleStatus()
	st.Instances[0].Job.HTMLURL = "https://github.com/darkraise/darkcloud/actions/runs/1"
	c.st = &st
	m := newModel(c, 120, 40, st)
	return feed(m, keys("2", "enter")...)
}

func TestDetailPage(t *testing.T) {
	m := detailModel(t, &fakeClient{})
	if m.page != pageDetail || m.detailID != "a3f9c1" {
		t.Fatalf("page %v id %q", m.page, m.detailID)
	}
	v := m.View()
	for _, want := range []string{"Runners › a3f9c1", "( Copy run URL )", "[ Stop runner ]", "● busy", "darkcloud",
		"CI / e2e-journeys  #412", "12m04s", "started 2026-10-03 13:52:56", "▌ 2 Runners", "esc back"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail page missing %q", want)
		}
	}
	if m = feed(m, key("esc")); m.page != pageRunners {
		t.Fatalf("esc went to page %v", m.page)
	}
}

func TestDetailButtons(t *testing.T) {
	c := &fakeClient{}
	m := detailModel(t, c)
	var copied string
	m.copyFn = func(s string) { copied = s }
	m = click(t, m, detailCopy)
	if copied != "https://github.com/darkraise/darkcloud/actions/runs/1" || !strings.Contains(m.View(), "✔ copied https://github.com") {
		t.Fatalf("copy: %q", copied)
	}
	if m = click(t, m, detailStop); m.overlay != ovConfirm { // a3f9c1 is busy
		t.Fatal("Stop runner on a busy runner did not ask first")
	}
	m = feed(m, key("enter"))
	if got := strings.Join(c.actions(), "|"); got != "kill a3f9c1" {
		t.Fatalf("actions %q", got)
	}
	m = feed(m, key("x")) // x stops from the keyboard too
	if m.overlay != ovConfirm {
		t.Fatal("x on the detail page did not ask")
	}
}

// Without a known run URL, the header offers only Stop runner.
func TestDetailWithoutURL(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	m.focus = paneRunners
	m = feed(m, key("enter"))
	if v := m.View(); strings.Contains(v, "Copy run URL") || !strings.Contains(v, "[ Stop runner ]") {
		t.Fatalf("header:\n%s", v)
	}
	if m = feed(m, key("esc")); m.page != pageDashboard {
		t.Fatalf("esc went to page %v", m.page)
	}
}

// The footer's x stops the runner like the key, and daemon text on the page
// is sanitized.
func TestDetailFooterAndCleanText(t *testing.T) {
	m := detailModel(t, &fakeClient{})
	if m = click(t, m, "key-x"); m.overlay != ovConfirm { // a3f9c1 is busy
		t.Fatal("the footer's x did not ask to stop the runner")
	}
	m = feed(m, key("esc"))
	m.detailSnap.Repo, m.detailSnap.State = "dark\x1b]0;pwned\x07cloud", "busy\x1b[2J"
	if v := m.View(); strings.Contains(v, "\x1b]0;") || strings.Contains(v, "\x1b[2J") || !strings.Contains(v, "darkcloud") {
		t.Fatalf("unsanitized detail text: %q", v)
	}
}
```

In `internal/tui/shell_test.go`, replace:

```go
	if m = click(t, m, "key-enter"); m.overlay != ovDetail {
```

with:

```go
	if m = click(t, m, "key-enter"); m.page != pageDetail {
```

In `internal/tui/tui_test.go`, replace:

```go
	lipgloss.SetColorProfile(termenv.Ascii)
	zone.NewGlobal()
```

with:

```go
	lipgloss.SetColorProfile(termenv.Ascii)
	time.Local = time.UTC // the views print local times; keep snapshots machine-independent
	zone.NewGlobal()
```

In `internal/tui/tui_test.go`, replace:

```go
	if upd.(Model).overlay != ovDetail {
		t.Fatal("double click did not open detail view")
```

with:

```go
	if upd.(Model).page != pageDetail {
		t.Fatal("double click did not open the detail page")
```

In `internal/tui/tui_test.go`, replace:

```go
	if m.overlay != ovDetail || strings.Join(c.calls, "|") != "containers a3f9c1" {
		t.Fatalf("overlay %v calls %v", m.overlay, c.calls)
```

with:

```go
	if m.page != pageDetail || strings.Join(c.calls, "|") != "containers a3f9c1" {
		t.Fatalf("page %v calls %v", m.page, c.calls)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: pageDetail`, `undefined: detailCopy`, `undefined: detailStop` and `m.detailSnap undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/detail.go`:

```go
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	detailCopy = "detail/copy"
	detailStop = "detail/stop"
)

// openDetail shows the detail page of runner id, remembering the page to go
// back to.
func (m Model) openDetail(id string) (tea.Model, tea.Cmd) {
	inst := m.instance(id)
	if inst == nil {
		return m, nil
	}
	if m.page != pageDetail {
		m.detailFrom = m.page
	}
	m.page, m.detailID, m.detailSnap = pageDetail, id, *inst
	m.steps, m.containers, m.stepsErr, m.ctrsErr = nil, nil, "", ""
	m.groups.detail.Focus(detailStop)
	return m, tea.Batch(m.fetchSteps(), m.fetchContainers())
}

// closeDetail goes back to the page the detail page was opened from.
func (m Model) closeDetail() (tea.Model, tea.Cmd) { return m.switchPage(m.detailFrom) }

// detailButtons renders the header's buttons: Copy run URL when the run's
// URL is known, and Stop runner.
func (m Model) detailButtons() string {
	g := m.groups
	items := []ui.Widget{g.stopRunner}
	out := g.stopRunner.View(g.detail.FocusedID() == detailStop, 0)
	if j := m.detailSnap.Job; j != nil && j.HTMLURL != "" {
		items = append([]ui.Widget{g.copyURL}, items...)
		out = g.copyURL.View(g.detail.FocusedID() == detailCopy, 0) + "  " + out
	}
	g.stopRunner.SetDisabled(!m.connected)
	g.detail.Set(items)
	return out
}

// detailSummary is the line under the header: state, repo, job and run,
// elapsed time and start time.
func (m Model) detailSummary() string {
	r := m.detailSnap
	state := clean(r.State)
	parts := []string{stateStyle(state).Render("● " + state), clean(r.Repo)}
	start := r.Since
	if j := r.Job; j != nil {
		job := j.Name
		if j.RunNumber != "" {
			job += "  #" + j.RunNumber
		}
		parts = append(parts, job)
		if !j.StartedAt.IsZero() {
			start = j.StartedAt
		}
	}
	parts = append(parts, dur(m.now().Sub(start)), "started "+start.Local().Format("2006-01-02 15:04:05"))
	return strings.Join(parts, sDim.Render("  ·  "))
}

// detailPage renders the runner detail page in w columns and h lines.
func (m Model) detailPage(w, h int) string {
	lines := []string{m.detailSummary(), ""}
	lines = append(lines, sBold.Render("Steps"))
	if m.stepsErr != "" {
		lines = append(lines, sRed.Render(m.stepsErr))
	}
	if len(m.steps) == 0 {
		lines = append(lines, sDim.Render("no steps reported yet"))
	}
	lines = append(lines, m.stepLines()...)
	lines = append(lines, "", sBold.Render("Containers"))
	lines = append(lines, m.containerLines()...)
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:min(len(lines), h)], "\n")
}

func (m Model) stepLines() []string {
	var lines []string
	for _, s := range m.steps {
		icon, style := "○", sDim
		switch {
		case s.Status == "in_progress":
			icon, style = spinnerFrames[m.frame%len(spinnerFrames)], sAccent
		case s.Conclusion == "success":
			icon, style = "✔", sGreen
		case s.Conclusion == "failure":
			icon, style = "✖", sRed
		case s.Conclusion == "skipped":
			icon = "–"
		}
		lines = append(lines, style.Render(icon)+" "+s.Name)
	}
	return lines
}

func (m Model) containerLines() []string {
	var lines []string
	if m.ctrsErr != "" {
		lines = append(lines, sRed.Render(m.ctrsErr))
	}
	if len(m.containers) == 0 {
		lines = append(lines, sDim.Render("none in this runner's compose projects"))
	}
	for _, c := range m.containers {
		lines = append(lines, stateStyle(c.State).Render(cell(c.State, 9))+" "+cell(c.Name, 28)+" "+cell(c.Image, 28)+" "+sDim.Render(c.Project))
	}
	return lines
}

// stopRunner stops runner id, asking first while it runs a job.
func (m Model) stopRunner(id string, busy bool) (tea.Model, tea.Cmd) {
	kill := func() tea.Cmd {
		return m.action("stopped "+id, func(c context.Context) error { return m.c.Kill(c, id) })
	}
	if busy {
		return m.openConfirm(fmt.Sprintf("Runner %s is running a job. Stop it?", id), kill)
	}
	return m, kill()
}

// detailKey handles the detail page's keys: esc goes back, x stops the
// runner, tab moves between the header buttons. It reports false for keys
// the global keys handle.
func (m Model) detailKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	m.detailButtons()
	if ok, cmd := m.groups.detail.Key(k); ok {
		return true, m, cmd
	}
	switch k.String() {
	case "esc":
		mm, cmd := m.closeDetail()
		return true, mm, cmd
	case "x":
		mm, cmd := m.stopRunner(m.detailID, m.detailSnap.State == "busy")
		return true, mm, cmd
	case "tab":
		m.groups.detail.Next()
	case "shift+tab":
		m.groups.detail.Prev()
	default:
		return false, m, nil
	}
	return true, m, nil
}

// detailPressed runs a detail header button.
func (m Model) detailPressed(id string) (tea.Model, tea.Cmd) {
	switch id {
	case detailCopy:
		if j := m.detailSnap.Job; j != nil && j.HTMLURL != "" {
			m.copyFn(j.HTMLURL)
			m.toast.Show("copied "+j.HTMLURL, false, m.now())
		}
	case detailStop:
		return m.stopRunner(m.detailID, m.detailSnap.State == "busy")
	}
	return m, nil
}
```

In `internal/tui/model.go`, replace:

```go
	pageSettings
)
```

with:

```go
	pageSettings
	pageDetail // a runner's detail page; not in the sidebar
)
```

In `internal/tui/model.go`, delete:

```go
	ovDetail
```

In `internal/tui/model.go`, replace:

```go
	detailID          string

```

with:

```go
	detailID          string
	detailFrom        page                 // the page esc returns to
	detailSnap        model.InstanceStatus // the runner as last seen in /status

```

In `internal/tui/model.go`, replace:

```go
		if m.overlay == ovDetail && m.frame%slowPoll == 0 && m.instance(m.detailID) != nil {
```

with:

```go
		if m.page == pageDetail && m.frame%slowPoll == 0 && m.instance(m.detailID) != nil {
```

In `internal/tui/model.go`, replace:

```go
		m.clampSelections()
		var cmds []tea.Cmd
```

with:

```go
		m.clampSelections()
		if inst := m.instance(m.detailID); inst != nil {
			m.detailSnap = *inst
		}
		var cmds []tea.Cmd
```

In `internal/tui/model.go`, replace:

```go
	case stepsMsg:
		if m.overlay == ovDetail && msg.id == m.detailID {
			if msg.err != nil {
```

with:

```go
	case stepsMsg:
		if m.page == pageDetail && msg.id == m.detailID {
			if msg.err != nil {
```

In `internal/tui/model.go`, replace:

```go
	case containersMsg:
		if m.overlay == ovDetail && msg.id == m.detailID {
			if msg.err != nil {
```

with:

```go
	case containersMsg:
		if m.page == pageDetail && msg.id == m.detailID {
			if msg.err != nil {
```

In `internal/tui/shell.go`, replace:

```go
	dash          ui.Group
	add, pauseAll *ui.Button
```

with:

```go
	dash                ui.Group
	add, pauseAll       *ui.Button
	detail              ui.Group
	copyURL, stopRunner *ui.Button
```

In `internal/tui/shell.go`, replace:

```go
		add:      ui.NewButton(dashAdd, "+ Add", ui.Primary),
		pauseAll: ui.NewButton(dashPauseAll, "Pause all", ui.Secondary),
```

with:

```go
		add:        ui.NewButton(dashAdd, "+ Add", ui.Primary),
		pauseAll:   ui.NewButton(dashPauseAll, "Pause all", ui.Secondary),
		copyURL:    ui.NewButton(detailCopy, "Copy run URL", ui.Secondary),
		stopRunner: ui.NewButton(detailStop, "Stop runner", ui.Danger),
```

In `internal/tui/shell.go`, replace:

```go
}

// sidebar is the wide layout's page list, h lines tall, with Help and Quit at the bottom.
func (m Model) sidebar(h int) string {
```

with:

```go
}

// navPage is the page the navigation marks as current: a runner's detail
// page belongs to Runners.
func (m Model) navPage() page {
	if m.page == pageDetail {
		return pageRunners
	}
	return m.page
}

// sidebar is the wide layout's page list, h lines tall, with Help and Quit at the bottom.
func (m Model) sidebar(h int) string {
```

In `internal/tui/shell.go`, replace:

```go
		label := fmt.Sprintf("%d %s", i+1, name)
		if page(i) == m.page {
			label = sAccent.Render("▌ " + label)
```

with:

```go
		label := fmt.Sprintf("%d %s", i+1, name)
		if page(i) == m.navPage() {
			label = sAccent.Render("▌ " + label)
```

In `internal/tui/shell.go`, replace:

```go
			if page(i) == m.page {
```

with:

```go
			if page(i) == m.navPage() {
```

In `internal/tui/shell.go`, replace:

```go
	title := sBold.Render(pageNames[m.page])
	right := ""
	switch {
	case m.page == pageSettings && !m.connected:
		right = sRed.Render("reconnecting")
	case m.page == pageDashboard:
		right = m.dashButtons()
```

with:

```go
	var title, right string
	switch {
	case m.page == pageDetail:
		title, right = sBold.Render("Runners › "+clean(m.detailID)), m.detailButtons()
	case m.page == pageSettings && !m.connected:
		title, right = sBold.Render(pageNames[m.page]), sRed.Render("reconnecting")
	case m.page == pageDashboard:
		title, right = sBold.Render(pageNames[m.page]), m.dashButtons()
	default:
		title = sBold.Render(pageNames[m.page])
```

In `internal/tui/shell.go`, replace:

```go
	switch m.page {
	case pageRunners:
```

with:

```go
	switch m.page {
	case pageDetail:
		return []footerKey{{"esc", "back"}, {"x", "stop"}, {"tab", "next"}, {"?", "help"}, {"q", "quit"}}
	case pageRunners:
```

In `internal/tui/shell.go`, replace:

```go
// on "q quit" never types a q into a focused text field.
func (m Model) footerPress(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "tab", "shift+tab", "ctrl+s", "enter", "esc", "up", "left", "right":
```

with:

```go
// on "q quit" never types a q into a focused text field. The detail page has
// no text fields and its own x, so every hint there takes the key's path.
func (m Model) footerPress(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "tab", "shift+tab", "ctrl+s", "enter", "esc", "up", "left", "right":
		return m.handleKey(keyMsg(k))
	}
	if m.page == pageDetail {
```

In `internal/tui/input.go`, replace:

```go
	case ovDetail:
		if key == "esc" || key == "q" || key == "enter" || key == "?" {
			m.overlay = ovNone
		}
		return m, nil
```

with:

```go
	}
	if m.page == pageDetail {
		if ok, mm, cmd := m.detailKey(k); ok {
			return mm, cmd
		}
```

In `internal/tui/input.go`, replace:

```go
			kill := func() tea.Cmd {
				return m.action("stopped "+id, func(c context.Context) error { return m.c.Kill(c, id) })
			}
			if busy {
				return m.openConfirm(fmt.Sprintf("Runner %s is running a job. Stop it?", id), kill)
			}
			return m, kill()
```

with:

```go
			return m.stopRunner(id, busy)
```

In `internal/tui/input.go`, replace:

```go
	if r := m.selectedRunner(); r != nil && (m.page == pageRunners || m.focus == paneRunners) {
		m.overlay, m.detailID = ovDetail, r.ID
		m.steps, m.containers, m.stepsErr, m.ctrsErr = nil, nil, "", ""
		return m, tea.Batch(m.fetchSteps(), m.fetchContainers())
	}
	return m, nil
}

// dialogButtons map each runner detail overlay button zone to the key it stands for.
var dialogButtons = []struct {
	zone string
	key  tea.KeyType
}{{"btn-ok", tea.KeyEnter}, {"btn-cancel", tea.KeyEsc}}
```

with:

```go
	if r := m.selectedRunner(); r != nil && m.runnerFocus() {
		return m.openDetail(r.ID)
	}
	return m, nil
}
```

In `internal/tui/input.go`, replace:

```go
	if m.overlay != ovNone {
		if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
			for _, b := range dialogButtons {
				if zone.Get(b.zone).InBounds(msg) {
					return m.handleKey(tea.KeyMsg{Type: b.key})
				}
			}
		}
		return m, nil
```

with:

```go
	if m.overlay == ovNone && m.page == pageDetail {
		m.detailButtons()
		if ok, cmd := m.groups.detail.Mouse(msg); ok {
			return m, cmd
		}
```

In `internal/tui/dialogs.go`, delete:

```go
	zone "github.com/lrstanley/bubblezone"
```

In `internal/tui/dialogs.go`, replace:

```go
		return m, m.togglePauseAll()
	case setSave:
```

with:

```go
		return m, m.togglePauseAll()
	case detailCopy, detailStop:
		return m.detailPressed(id)
	case setSave:
```

In `internal/tui/dialogs.go`, delete:

```go
// buttons renders the runner detail overlay's clickable buttons: ok runs
// enter, cancel runs esc.
func buttons(ok, cancel string) string {
	b := func(id, label string) string { return zone.Mark(id, sAccent.Render("[ "+label+" ]")) }
	if ok == "" {
		return b("btn-cancel", cancel)
	}
	return b("btn-ok", ok) + "  " + b("btn-cancel", cancel)
}

```

In `internal/tui/dialogs.go`, delete:

```go
	case ovDetail:
		dialog = sDialog.Render(m.detailBody())
```

In `internal/tui/view.go`, replace:

```go
		return m.settingsView(w, h)
	}
```

with:

```go
		return m.settingsView(w, h)
	case pageDetail:
		return m.detailPage(w, h)
	}
```

In `internal/tui/view.go`, replace:

```go
}

func (m Model) detailBody() string {
	r := m.instance(m.detailID)
	if r == nil {
		return "runner " + m.detailID + " has finished\n\n" + buttons("", "Close")
	}
	lines := []string{sBold.Render(fmt.Sprintf("Runner %s · %s · %s", r.ID, r.Repo, r.State))}
	if r.Job != nil {
		lines = append(lines, fmt.Sprintf("%s  #%s  %s", r.Job.Name, r.Job.RunNumber, r.Job.Workflow))
		if r.Job.HTMLURL != "" {
			lines = append(lines, sDim.Render(r.Job.HTMLURL))
		}
	}
	lines = append(lines, "", sBold.Render("Steps"))
	if m.stepsErr != "" {
		lines = append(lines, sRed.Render(m.stepsErr))
	}
	if len(m.steps) == 0 {
		lines = append(lines, sDim.Render("no steps reported yet"))
	}
	for _, s := range m.steps {
		icon, style := "○", sDim
		switch {
		case s.Status == "in_progress":
			icon, style = spinnerFrames[m.frame%len(spinnerFrames)], sAccent
		case s.Conclusion == "success":
			icon, style = "✔", sGreen
		case s.Conclusion == "failure":
			icon, style = "✖", sRed
		case s.Conclusion == "skipped":
			icon = "–"
		}
		lines = append(lines, style.Render(icon)+" "+s.Name)
	}
	lines = append(lines, "", sBold.Render("Containers"))
	if m.ctrsErr != "" {
		lines = append(lines, sRed.Render(m.ctrsErr))
	}
	if len(m.containers) == 0 {
		lines = append(lines, sDim.Render("none in this runner's compose projects"))
	}
	for _, c := range m.containers {
		lines = append(lines, stateStyle(c.State).Render(cell(c.State, 9))+" "+cell(c.Name, 28)+" "+cell(c.Image, 28)+" "+sDim.Render(c.Project))
	}
	lines = append(lines, "", buttons("", "Close")+"  "+sDim.Render("esc close"))
	return strings.Join(lines, "\n")
}
```

with:

```go
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run "TestDetail|TestMouseClickSelectsRunnerAndDoubleClickOpensDetail|TestFooterFollowsPageAndClicksRunKeys"`
Expected: PASS for `TestDetailPage`, `TestDetailButtons`, `TestDetailWithoutURL`, `TestDetailFooterAndCleanText`, `TestDetailShowsContainers`, `TestDetailStepsRefreshOnTicks`, `TestMouseClickSelectsRunnerAndDoubleClickOpensDetail` and `TestFooterFollowsPageAndClicksRunKeys`.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/detail.go internal/tui/detail_test.go internal/tui/dialogs.go internal/tui/input.go internal/tui/model.go internal/tui/shell.go internal/tui/shell_test.go internal/tui/tui_test.go internal/tui/view.go
git commit -m "feat(tui): turn runner details into a page"
```

### Task 7: Detail tabs and log following

**Files:**
- Modify: `internal/tui/detail.go` (`tabStrip`; `detailTop`, `detailList`, `scrollDetail`; tabs in `detailPage` and `detailKey`)
- Modify: `internal/tui/model.go` (`detailScroll`, `logTarget`, `follow`, the tick)
- Modify: `internal/tui/input.go` (`l`, `enter`, `move`, `pgup`/`pgdown`, detail clicks)
- Modify: `internal/tui/shell.go` (`pageGroups.tabs`, footer)
- Test: `internal/tui/detail_test.go`
- Test: `internal/tui/settings_test.go`
- Test: `internal/tui/tui_test.go`

**Interfaces:**
- Consumes: **Detail page**
- Produces: **Detail tabs** (see Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

The log is followed for whichever view shows one: the Runners page preview or the detail page's Log tab. `l` on a runner row now opens the detail page on its Log tab, so on Settings (which has no runner rows) it does nothing; `TestLogKeyIsGuarded` becomes `TestLogKeyIsInactiveOnSettings`.

- [ ] **Step 1: Write the failing tests**

In `internal/tui/detail_test.go`, replace:

```go
	"strings"
	"testing"
```

with:

```go
	"fmt"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/model"
```

In `internal/tui/detail_test.go`, replace:

```go
		t.Fatalf("unsanitized detail text: %q", v)
	}
}
```

with:

```go
		t.Fatalf("unsanitized detail text: %q", v)
	}
}

// ←/→ and clicks switch the tabs; only the Log tab follows the runner's log.
func TestDetailTabs(t *testing.T) {
	c := &fakeClient{steps: []model.Step{{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success"}},
		ctrs: []model.Container{{Name: "darkcloud-db-1", State: "running"}}}
	m := detailModel(t, c)
	m.logID, m.logText = "", ""
	v := m.View()
	if !strings.Contains(v, "[ Steps ]") || !strings.Contains(v, "✔ Set up job") || strings.Contains(v, "darkcloud-db-1") {
		t.Fatalf("Steps tab:\n%s", v)
	}
	if m = ticks(m, slowPoll); m.logID != "" {
		t.Fatalf("the Steps tab followed log %q", m.logID)
	}
	m = feed(m, key("right"))
	if m.groups.tabs.active != tabLog || m.logID != "a3f9c1" {
		t.Fatalf("right: tab %d log %q", m.groups.tabs.active, m.logID)
	}
	m.logText = "hello from the job\n"
	if v = m.View(); !strings.Contains(v, "Log — following") || !strings.Contains(v, "hello from the job") {
		t.Fatalf("Log tab:\n%s", v)
	}
	if m = click(t, m, tabZone(tabContainers)); m.groups.tabs.active != tabContainers || !strings.Contains(m.View(), "darkcloud-db-1") {
		t.Fatalf("click: tab %d\n%s", m.groups.tabs.active, m.View())
	}
	if m = feed(m, key("right")); m.groups.tabs.active != tabSteps {
		t.Fatalf("right wraps to tab %d", m.groups.tabs.active)
	}
	if m = feed(m, key("down"), key("down")); m.runnerSel != 0 || m.detailID != "a3f9c1" {
		t.Fatalf("down moved the hidden selection to %d", m.runnerSel)
	}
}

// l on a runner row opens its detail page on the Log tab; Tab reaches the
// tab strip after the header buttons.
func TestLogKeyOpensDetailLog(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), keys("2", "l")...)
	if m.page != pageDetail || m.groups.tabs.active != tabLog || m.logID != "a3f9c1" {
		t.Fatalf("page %v tab %d log %q", m.page, m.groups.tabs.active, m.logID)
	}
	if v := m.View(); !strings.Contains(v, "› ") || !strings.Contains(v, "right next tab") {
		t.Fatalf("tab strip not focused or footer missing:\n%s", v)
	}
	if m = feed(m, key("tab")); m.groups.detail.FocusedID() == detailTabs {
		t.Fatal("tab did not move focus off the tab strip")
	}
}

// A step list longer than the page scrolls with pgdn, pgup and the arrows,
// and starts at the top again on another tab.
func TestDetailListScrolls(t *testing.T) {
	c := &fakeClient{}
	for i := 1; i <= 60; i++ {
		c.steps = append(c.steps, model.Step{Number: i, Name: fmt.Sprintf("step %02d", i), Status: "completed", Conclusion: "success"})
	}
	m := detailModel(t, c)
	if v := m.View(); !strings.Contains(v, "step 01") || strings.Contains(v, "step 60") {
		t.Fatalf("top of the list:\n%s", v)
	}
	m = feed(m, keys("pgdown", "pgdown", "pgdown", "pgdown", "pgdown", "pgdown", "pgdown")...)
	if v := m.View(); strings.Contains(v, "step 01") || !strings.Contains(v, "step 60") {
		t.Fatalf("after pgdn:\n%s", v)
	}
	before := m.detailScroll
	if m = feed(m, key("pgup"), key("down")); m.detailScroll != before-9 {
		t.Fatalf("pgup then down: scroll %d, want %d", m.detailScroll, before-9)
	}
	if m = feed(m, key("right"), key("left")); m.detailScroll != 0 {
		t.Fatalf("tab change kept scroll %d", m.detailScroll)
	}
}

// Clicking the Log tab follows this runner's log at once, not the log of
// the runner followed before.
func TestDetailLogTabClickFollows(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 40), key("2")) // follows a3f9c1
	m = feed(m, keys("1", "right", "down")...)               // Dashboard, Runners card, 7be210
	if m = feed(m, key("enter")); m.detailID != "7be210" || m.logID != "a3f9c1" {
		t.Fatalf("detail %q log %q", m.detailID, m.logID)
	}
	if m = click(t, m, tabZone(tabLog)); m.logID != "7be210" {
		t.Fatalf("log tab click follows %q", m.logID)
	}
}
```

In `internal/tui/settings_test.go`, replace:

```go
// l (follow a runner's log) leaves Settings, so it is guarded too.
func TestLogKeyIsGuarded(t *testing.T) {
	m := dirtySettings(t, &fakeClient{})
	m.View()
	if m = feed(m, key("l")); m.overlay != ovUnsaved || m.leaveTo != (leaveTarget{page: pageRunners}) {
		t.Fatalf("l: overlay %v target %+v page %v", m.overlay, m.leaveTo, m.page)
```

with:

```go
// l acts on a runner row; Settings has none, so l leaves nothing unsaved behind.
func TestLogKeyIsInactiveOnSettings(t *testing.T) {
	m := dirtySettings(t, &fakeClient{})
	m.View()
	if m = feed(m, key("l")); m.overlay != ovNone || m.page != pageSettings {
		t.Fatalf("l: overlay %v page %v", m.overlay, m.page)
```

In `internal/tui/tui_test.go`, replace:

```go
	v := m.View()
	for _, want := range []string{"Containers", "running", "darkcloud-db-1", "postgres:17", "ghr-a3f9c1",
```

with:

```go
	v := feed(m, key("left")).View() // the Containers tab, wrapping round from Steps
	for _, want := range []string{"[ Containers ]", "running", "darkcloud-db-1", "postgres:17", "ghr-a3f9c1",
```

In `internal/tui/tui_test.go`, replace:

```go
	if !strings.Contains(v, "darkcloud-db-1") {
		t.Fatal("containers not refreshed")
	}
```

with:

```go
	if m = feed(m, key("left")); !strings.Contains(m.View(), "darkcloud-db-1") {
		t.Fatal("containers not refreshed")
	}
	m = feed(m, key("right")) // back to Steps
```

In `internal/tui/tui_test.go`, replace:

```go
	m = feed(m, keys("1", "l")...)
	if m.page != pageRunners || m.logID != "a3f9c1" {
		t.Fatalf("l: page %v log %q", m.page, m.logID)
```

with:

```go
	m = feed(m, keys("1", "l")...) // the Runners card kept focus from the click
	if m.page != pageDetail || m.groups.tabs.active != tabLog || m.logID != "a3f9c1" {
		t.Fatalf("l: page %v tab %d log %q", m.page, m.groups.tabs.active, m.logID)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `m.groups.tabs undefined (type *pageGroups has no field or method tabs)`, `undefined: tabLog`, `undefined: tabZone`, `undefined: tabContainers` and `undefined: tabSteps`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/detail.go`, replace:

```go
	tea "github.com/charmbracelet/bubbletea"

```

with:

```go
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

```

In `internal/tui/detail.go`, replace:

```go
)

// openDetail shows the detail page of runner id, remembering the page to go
// back to.
func (m Model) openDetail(id string) (tea.Model, tea.Cmd) {
```

with:

```go
	detailTabs = "detail/tabs"
)

// The detail page's tabs.
const (
	tabSteps = iota
	tabLog
	tabContainers
)

// tabStrip is the detail page's Steps / Log / Containers switcher. The page
// switches it with ←/→ whatever has focus, so it takes no keys itself.
type tabStrip struct {
	active int
}

var tabNames = []string{"Steps", "Log", "Containers"}

func tabZone(i int) string { return fmt.Sprintf("%s/%d", detailTabs, i) }

func (t *tabStrip) ID() string               { return detailTabs }
func (t *tabStrip) Focusable() bool          { return true }
func (t *tabStrip) TakesKey(tea.KeyMsg) bool { return false }
func (t *tabStrip) Capturing() bool          { return false }
func (t *tabStrip) Blur()                    {}
func (t *tabStrip) SetDisabled(bool)         {}

func (t *tabStrip) Hit(msg tea.MouseMsg) bool {
	for i := range tabNames {
		if zone.Get(tabZone(i)).InBounds(msg) {
			return true
		}
	}
	return false
}

func (t *tabStrip) Update(msg tea.Msg) (ui.Control, tea.Cmd) {
	if msg, ok := msg.(tea.MouseMsg); ok {
		for i := range tabNames {
			if zone.Get(tabZone(i)).InBounds(msg) {
				t.active = i
			}
		}
	}
	return t, nil
}

func (t *tabStrip) View(focused bool, _ int) string {
	out := "  "
	if focused {
		out = sAccent.Render("›") + " "
	}
	for i, name := range tabNames {
		label := sDim.Render("  " + name + "  ")
		if i == t.active {
			label = sAccent.Render("[ " + name + " ]")
		}
		out += zone.Mark(tabZone(i), label) + " "
	}
	return out
}

// openDetail shows the detail page of runner id on tab, remembering the page
// to go back to.
func (m Model) openDetail(id string, tab int) (tea.Model, tea.Cmd) {
```

In `internal/tui/detail.go`, replace:

```go
	m.steps, m.containers, m.stepsErr, m.ctrsErr = nil, nil, "", ""
	m.groups.detail.Focus(detailStop)
	return m, tea.Batch(m.fetchSteps(), m.fetchContainers())
```

with:

```go
	m.steps, m.containers, m.stepsErr, m.ctrsErr, m.detailScroll = nil, nil, "", "", 0
	m.groups.tabs.active = tab
	m.detailButtons()
	m.groups.detail.Focus(detailTabs)
	follow := m.follow()
	return m, tea.Batch(m.fetchSteps(), m.fetchContainers(), follow)
```

In `internal/tui/detail.go`, replace:

```go
	items := []ui.Widget{g.stopRunner}
```

with:

```go
	items := []ui.Widget{g.stopRunner, g.tabs}
```

In `internal/tui/detail.go`, replace:

```go
// detailPage renders the runner detail page in w columns and h lines.
func (m Model) detailPage(w, h int) string {
	lines := []string{m.detailSummary(), ""}
	lines = append(lines, sBold.Render("Steps"))
	if m.stepsErr != "" {
		lines = append(lines, sRed.Render(m.stepsErr))
	}
	if len(m.steps) == 0 {
		lines = append(lines, sDim.Render("no steps reported yet"))
	}
	lines = append(lines, m.stepLines()...)
	lines = append(lines, "", sBold.Render("Containers"))
	lines = append(lines, m.containerLines()...)
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines[:min(len(lines), h)], "\n")
```

with:

```go
// detailTop is the part of the detail page above the active tab: the
// summary and the tab strip.
func (m Model) detailTop(w int) []string {
	g := m.groups
	return []string{m.detailSummary(), "", g.tabs.View(g.detail.FocusedID() == detailTabs, w), ""}
}

// detailList is the Steps or Containers tab's lines; nil on the Log tab.
func (m Model) detailList() []string {
	switch m.groups.tabs.active {
	case tabSteps:
		var body []string
		if m.stepsErr != "" {
			body = append(body, sRed.Render(m.stepsErr))
		}
		if len(m.steps) == 0 {
			body = append(body, sDim.Render("no steps reported yet"))
		}
		return append(body, m.stepLines()...)
	case tabContainers:
		return m.containerLines()
	}
	return nil
}

// scrollDetail moves the Steps or Containers list d lines, no further than
// its last line reaching the bottom of the page.
func (m *Model) scrollDetail(d int) {
	w, h := m.contentSize()
	limit := max(len(m.detailList())-max(h-len(m.detailTop(w)), 3), 0)
	m.detailScroll = min(max(m.detailScroll+d, 0), limit)
}

// detailPage renders the runner detail page in w columns and h lines: the
// summary, the tab strip and the active tab.
func (m Model) detailPage(w, h int) string {
	m.detailButtons()
	top := m.detailTop(w)
	bodyH := max(h-len(top), 3)
	var body []string
	if m.groups.tabs.active == tabLog {
		all := strings.Split(strings.TrimRight(m.logText, "\n"), "\n")
		end := max(len(all)-m.logScroll, 0)
		lines := all[max(end-(bodyH-2), 0):end]
		for len(lines) < bodyH-2 {
			lines = append(lines, "")
		}
		body = strings.Split(zone.Mark("log", box("Log — following", w, lines)), "\n")
	} else {
		list := m.detailList()
		body = list[min(m.detailScroll, max(len(list)-bodyH, 0)):]
	}
	out := append(top, body[:min(len(body), bodyH)]...)
	return strings.Join(out, "\n")
```

In `internal/tui/detail.go`, replace:

```go
		return true, mm, cmd
	case "x":
```

with:

```go
		return true, mm, cmd
	case "left", "right":
		n := len(tabNames)
		if k.String() == "left" {
			m.groups.tabs.active = (m.groups.tabs.active + n - 1) % n
		} else {
			m.groups.tabs.active = (m.groups.tabs.active + 1) % n
		}
		m.detailScroll = 0
		cmd := m.follow()
		return true, m, cmd
	case "x":
```

In `internal/tui/model.go`, replace:

```go
	detailSnap        model.InstanceStatus // the runner as last seen in /status

```

with:

```go
	detailSnap        model.InstanceStatus // the runner as last seen in /status
	detailScroll      int                  // first line shown of the Steps or Containers list

```

In `internal/tui/model.go`, replace:

```go
// follow points the log pane of the Runners page at the selected runner,
// starting its log over when that is a different runner.
func (m *Model) follow() tea.Cmd {
	r := m.selectedRunner()
	if m.page != pageRunners || r == nil || r.ID == m.logID {
		return nil
	}
	m.logID, m.logText, m.logCursor, m.logScroll = r.ID, "", "", 0
```

with:

```go
// logTarget is the runner whose log a view on screen shows: the Runners
// page's preview follows the selection, the detail page's Log tab its runner
// (also after the runner finished: the daemon serves archived logs).
func (m Model) logTarget() string {
	switch {
	case m.page == pageRunners:
		if r := m.selectedRunner(); r != nil {
			return r.ID
		}
	case m.page == pageDetail && m.groups.tabs.active == tabLog:
		return m.detailID
	}
	return ""
}

// follow points the log at the runner a visible view shows, starting it over
// when that is a different runner. Only one log is followed at a time.
func (m *Model) follow() tea.Cmd {
	id := m.logTarget()
	if id == "" || id == m.logID {
		return nil
	}
	m.logID, m.logText, m.logCursor, m.logScroll = id, "", "", 0
```

In `internal/tui/model.go`, replace:

```go
		if m.page == pageRunners {
```

with:

```go
		if m.logTarget() != "" {
```

In `internal/tui/input.go`, replace:

```go
	case "pgdown":
		m.eventScroll = max(0, m.eventScroll-5)
		m.logScroll = max(0, m.logScroll-10)
```

with:

```go
		if m.page == pageDetail {
			m.scrollDetail(-10)
		}
	case "pgdown":
		m.eventScroll = max(0, m.eventScroll-5)
		m.logScroll = max(0, m.logScroll-10)
		if m.page == pageDetail {
			m.scrollDetail(10)
		}
```

In `internal/tui/input.go`, replace:

```go
		if m.selectedRunner() != nil {
			return m.leave(leaveTarget{page: pageRunners})
```

with:

```go
		if r := m.selectedRunner(); r != nil && m.runnerFocus() {
			return m.openDetail(r.ID, tabLog)
```

In `internal/tui/input.go`, replace:

```go
		m.histSel = clamp(m.histSel+d, len(m.hist))
	case m.page == pageSettings, m.page == pageDashboard && !m.onCard():
```

with:

```go
		m.histSel = clamp(m.histSel+d, len(m.hist))
	case m.page == pageDetail:
		m.scrollDetail(d)
	case m.page == pageSettings, m.page == pageDashboard && !m.onCard():
```

In `internal/tui/input.go`, replace:

```go
		return m.openDetail(r.ID)
```

with:

```go
		return m.openDetail(r.ID, tabSteps)
```

In `internal/tui/input.go`, replace:

```go
		if ok, cmd := m.groups.detail.Mouse(msg); ok {
			return m, cmd
```

with:

```go
		tab := m.groups.tabs.active
		if ok, cmd := m.groups.detail.Mouse(msg); ok {
			if m.groups.tabs.active != tab {
				m.detailScroll = 0
			}
			follow := m.follow() // a click on Log points the log at this runner at once
			return m, tea.Batch(cmd, follow)
```

In `internal/tui/shell.go`, replace:

```go
	copyURL, stopRunner *ui.Button
}
```

with:

```go
	copyURL, stopRunner *ui.Button
	tabs                *tabStrip
}
```

In `internal/tui/shell.go`, replace:

```go
		stopRunner: ui.NewButton(detailStop, "Stop runner", ui.Danger),
	}
```

with:

```go
		stopRunner: ui.NewButton(detailStop, "Stop runner", ui.Danger),
		tabs:       &tabStrip{},
	}
```

In `internal/tui/shell.go`, replace:

```go
		return []footerKey{{"esc", "back"}, {"x", "stop"}, {"tab", "next"}, {"?", "help"}, {"q", "quit"}}
```

with:

```go
		return []footerKey{{"esc", "back"}, {"left", "prev tab"}, {"right", "next tab"}, {"x", "stop"}, {"?", "help"}, {"q", "quit"}}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run "TestDetail|TestLogKey|TestRunnersTabFollowsSelection"`
Expected: PASS for `TestDetailTabs`, `TestDetailListScrolls`, `TestDetailLogTabClickFollows`, `TestLogKeyOpensDetailLog`, `TestLogKeyIsInactiveOnSettings`, `TestRunnersTabFollowsSelection` and the other `TestDetail…` tests.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/detail.go internal/tui/detail_test.go internal/tui/input.go internal/tui/model.go internal/tui/settings_test.go internal/tui/shell.go internal/tui/tui_test.go
git commit -m "feat(tui): add detail tabs and log following"
```

### Task 8: Detail page after the runner finishes

**Files:**
- Modify: `internal/tui/detail.go`
- Modify: `internal/tui/model.go` (`detailDone`, the status handler)
- Modify: `internal/tui/shell.go` (`pageGroups.back`)
- Modify: `internal/tui/dialogs.go` (`pressed`)
- Test: `internal/tui/detail_test.go`

**Interfaces:**
- Consumes: **Detail page**, **Detail tabs**
- Produces: `detailBack`, `finished` (see Contracts, **Detail page**)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/tui/detail_test.go`, replace:

```go
	"testing"

```

with:

```go
	"testing"
	"time"

```

In `internal/tui/detail_test.go`, replace:

```go
		t.Fatalf("log tab click follows %q", m.logID)
	}
}
```

with:

```go
		t.Fatalf("log tab click follows %q", m.logID)
	}
}

// When the runner leaves /status the page keeps its snapshot, freezes the
// elapsed time and offers Back to runners; only the Log tab keeps polling.
func TestDetailAfterRunnerFinishes(t *testing.T) {
	c := &fakeClient{}
	m := detailModel(t, c)
	c.st.Instances = c.st.Instances[1:] // a3f9c1 is gone
	m.now = func() time.Time { return now.Add(5 * time.Minute) }
	m = ticks(m, 1)
	m.now = func() time.Time { return now.Add(10 * time.Minute) }
	v := m.View()
	for _, want := range []string{"⚠ This runner has finished", "Back to runners", "● busy", "17m04s", "CI / e2e-journeys  #412"} {
		if !strings.Contains(v, want) {
			t.Errorf("finished detail page missing %q", want)
		}
	}
	c.calls = nil
	if m = ticks(m, slowPoll); strings.Contains(strings.Join(c.calls, "|"), "containers") {
		t.Fatalf("containers still polled: %v", c.calls)
	}
	c.calls = nil
	if m = ticks(feed(m, key("right")), 2); !strings.Contains(strings.Join(c.calls, "|"), "log a3f9c1") {
		t.Fatalf("the Log tab stopped polling: %v", c.calls)
	}
	if m = feed(m, key("x")); m.overlay != ovNone || strings.Contains(strings.Join(c.actions(), "|"), "kill") {
		t.Fatal("x tried to stop a finished runner")
	}
	if m = click(t, m, detailBack); m.page != pageRunners {
		t.Fatalf("Back to runners went to page %v", m.page)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: detailBack`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/detail.go`, replace:

```go
	"strings"

```

with:

```go
	"strings"
	"time"

```

In `internal/tui/detail.go`, replace:

```go
	detailTabs = "detail/tabs"
)
```

with:

```go
	detailTabs = "detail/tabs"
	detailBack = "detail/back"
)
```

In `internal/tui/detail.go`, replace:

```go
	m.page, m.detailID, m.detailSnap = pageDetail, id, *inst
```

with:

```go
	m.page, m.detailID, m.detailSnap, m.detailDone = pageDetail, id, *inst, time.Time{}
```

In `internal/tui/detail.go`, replace:

```go
// URL is known, and Stop runner.
func (m Model) detailButtons() string {
	g := m.groups
	items := []ui.Widget{g.stopRunner, g.tabs}
```

with:

```go
// URL is known, and Stop runner, disabled once the runner has finished. It
// also sets the page's focus order, which takes in Back to runners while the
// finished alert shows.
func (m Model) detailButtons() string {
	g := m.groups
	items := []ui.Widget{g.stopRunner}
```

In `internal/tui/detail.go`, replace:

```go
	g.stopRunner.SetDisabled(!m.connected)
	g.detail.Set(items)
	return out
}
```

with:

```go
	if m.finished() {
		items = append(items, g.back)
	}
	g.stopRunner.SetDisabled(!m.connected || m.finished())
	g.detail.Set(append(items, g.tabs))
	return out
}

// finished reports whether the detail page's runner has left /status.
func (m Model) finished() bool { return !m.detailDone.IsZero() }
```

In `internal/tui/detail.go`, replace:

```go
	parts = append(parts, dur(m.now().Sub(start)), "started "+start.Local().Format("2006-01-02 15:04:05"))
```

with:

```go
	end := m.now()
	if m.finished() {
		end = m.detailDone
	}
	parts = append(parts, dur(end.Sub(start)), "started "+start.Local().Format("2006-01-02 15:04:05"))
```

In `internal/tui/detail.go`, replace:

```go
// summary and the tab strip.
func (m Model) detailTop(w int) []string {
	g := m.groups
	return []string{m.detailSummary(), "", g.tabs.View(g.detail.FocusedID() == detailTabs, w), ""}
```

with:

```go
// summary, the finished alert once the runner has gone, and the tab strip.
func (m Model) detailTop(w int) []string {
	g := m.groups
	top := []string{m.detailSummary(), ""}
	if m.finished() {
		top = append(top, sAmber.Render("⚠ This runner has finished")+"   "+g.back.View(g.detail.FocusedID() == detailBack, 0), "")
	}
	return append(top, g.tabs.View(g.detail.FocusedID() == detailTabs, w), "")
```

In `internal/tui/detail.go`, replace:

```go
		mm, cmd := m.stopRunner(m.detailID, m.detailSnap.State == "busy")
```

with:

```go
		mm, cmd := m.detailPressed(detailStop)
```

In `internal/tui/detail.go`, replace:

```go
		return m.stopRunner(m.detailID, m.detailSnap.State == "busy")
```

with:

```go
		if !m.finished() {
			return m.stopRunner(m.detailID, m.detailSnap.State == "busy")
		}
	case detailBack:
		return m.switchPage(pageRunners)
```

In `internal/tui/model.go`, replace:

```go
	detailSnap        model.InstanceStatus // the runner as last seen in /status
	detailScroll      int                  // first line shown of the Steps or Containers list
```

with:

```go
	detailSnap        model.InstanceStatus // the runner as last seen in /status
	detailDone        time.Time            // when the runner left /status; zero while it runs
	detailScroll      int                  // first line shown of the Steps or Containers list
```

In `internal/tui/model.go`, replace:

```go
			m.detailSnap = *inst
		}
```

with:

```go
			m.detailSnap = *inst
		} else if m.page == pageDetail && !m.finished() {
			m.detailDone = m.now()
		}
```

In `internal/tui/shell.go`, replace:

```go
	copyURL, stopRunner *ui.Button
	tabs                *tabStrip
```

with:

```go
	copyURL, stopRunner *ui.Button
	back                *ui.Button
	tabs                *tabStrip
```

In `internal/tui/shell.go`, replace:

```go
		stopRunner: ui.NewButton(detailStop, "Stop runner", ui.Danger),
		tabs:       &tabStrip{},
```

with:

```go
		stopRunner: ui.NewButton(detailStop, "Stop runner", ui.Danger),
		back:       ui.NewButton(detailBack, "Back to runners", ui.Primary),
		tabs:       &tabStrip{},
```

In `internal/tui/dialogs.go`, replace:

```go
	case detailCopy, detailStop:
```

with:

```go
	case detailCopy, detailStop, detailBack:
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run TestDetailAfterRunnerFinishes`
Expected: PASS.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/detail.go internal/tui/detail_test.go internal/tui/dialogs.go internal/tui/model.go internal/tui/shell.go
git commit -m "feat(tui): show when a detail runner finishes"
```

### Task 9: History filter bar

**Files:**
- Create: `internal/tui/history.go` (`historyPage` replaces `historyTab` from `view.go`)
- Modify: `internal/tui/view.go`
- Modify: `internal/tui/shell.go` (`pageGroups`, footer)
- Modify: `internal/tui/input.go` (key routing, `move`, `pgup`/`pgdown`, `handleMouse`)
- Test: `internal/tui/history_test.go`
- Test: `internal/tui/tui_test.go` (`fakeClient.hist`, `fakeClient.histReqs`)

**Interfaces:**
- Consumes: **Page focus**, **Row buttons**, Plan 2's **ui controls** (`ui.Select`)
- Produces: **History filters** (see Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/history_test.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/model"
)

// historyModel is the sample model on the History page; its client holds two
// finished jobs and filters them as the daemon would.
func historyModel(t *testing.T) (Model, *fakeClient) {
	t.Helper()
	c := &fakeClient{hist: []model.HistoryEntry{
		{Repo: "darkcloud", RunNumber: "411", JobName: "lint", Conclusion: "success",
			StartedAt: now.Add(-4 * time.Minute), FinishedAt: now.Add(-2 * time.Minute), HTMLURL: "https://github.com/darkraise/darkcloud/actions/runs/411"},
		{Repo: "darkmem", RunNumber: "87", JobName: "build / test", Conclusion: "failure",
			StartedAt: now.Add(-70 * time.Minute), FinishedAt: now.Add(-time.Hour)},
	}}
	return feed(sampleModel(c, 120, 30), key("3")), c
}

func lastReq(c *fakeClient) string { return c.histReqs[len(c.histReqs)-1] }

func TestHistoryFilterBar(t *testing.T) {
	m, c := historyModel(t)
	v := m.View()
	for _, want := range []string{"Repo   [ all ▾ ]", "Result   [ all ▾ ]", "› History", "( Copy run URL ) success    2m00s", "#87", "tab next"} {
		if !strings.Contains(v, want) {
			t.Errorf("History page missing %q", want)
		}
	}
	// Tab wraps from the table to the Repo filter, which enter opens.
	m = feed(m, key("tab"), key("enter"), key("down"), key("enter"))
	if v = m.View(); lastReq(c) != "darkcloud|" || !strings.Contains(v, "› [ darkcloud ▾ ]") || strings.Contains(v, "#87") {
		t.Fatalf("repo filter: request %q\n%s", lastReq(c), v)
	}
	m = feed(m, key("tab"), key("right")) // a closed select cycles with →
	if lastReq(c) != "darkcloud|success" || !strings.Contains(m.View(), "#411") {
		t.Fatalf("result filter: request %q", lastReq(c))
	}
}

func TestHistoryFilterMouse(t *testing.T) {
	m, c := historyModel(t)
	if m = click(t, m, histResultSel); !m.groups.histResult.Open() {
		t.Fatal("a click did not open the Result filter")
	}
	m = click(t, m, histResultSel+"/opt-2")
	if v := m.View(); lastReq(c) != "|failure" || m.groups.histResult.Open() || !strings.Contains(v, "#87") || strings.Contains(v, "#411") {
		t.Fatalf("option click: request %q open %v\n%s", lastReq(c), m.groups.histResult.Open(), v)
	}
	// r and c still cycle, and the selects follow.
	m = feed(m, key("r"))
	if v := m.View(); lastReq(c) != "darkcloud|failure" || !strings.Contains(v, "[ darkcloud ▾ ]") || !strings.Contains(v, "[ failure ▾ ]") {
		t.Fatalf("r: request %q\n%s", lastReq(c), v)
	}
}

func TestHistoryCopyRunURL(t *testing.T) {
	m, _ := historyModel(t)
	var copied string
	m.copyFn = func(s string) { copied = s }
	if m = click(t, m, rowCopy); copied != "https://github.com/darkraise/darkcloud/actions/runs/411" {
		t.Fatalf("row button copied %q", copied)
	}
	copied = ""
	m = feed(m, key("down"), key("enter"))
	if copied != "" || !strings.Contains(m.View(), "no run URL recorded") {
		t.Fatalf("enter on a row without a URL copied %q", copied)
	}
	// With a filter focused, enter opens it instead of copying.
	m = feed(m, key("tab"), key("enter"))
	if !m.groups.histRepo.Open() || copied != "" {
		t.Fatal("enter on the Repo filter did not open it")
	}
}

// A focused filter owns the arrows and the footer; pgup and pgdn page the
// table whatever has focus.
func TestHistoryFocusKeys(t *testing.T) {
	m, c := historyModel(t)
	for i := 0; i < 30; i++ {
		c.hist = append(c.hist, model.HistoryEntry{Repo: "darkmem", RunNumber: fmt.Sprint(i), JobName: "test", Conclusion: "success", FinishedAt: now})
	}
	m = feed(m, key("r"), key("r"), key("r"), key("r")) // all repos again, refetched
	m = feed(m, key("tab"))                             // the Repo filter
	if m = feed(m, key("down")); m.histSel != 0 {
		t.Fatalf("down moved the table to %d while a filter had focus", m.histSel)
	}
	if v := m.View(); !strings.Contains(v, "enter open") || strings.Contains(v, "copy URL") {
		t.Fatalf("footer does not follow the filter:\n%s", v)
	}
	if m = feed(m, key("pgdown")); m.histSel != 10 {
		t.Fatalf("pgdn: selection %d", m.histSel)
	}
}

// A long repo name is cut to the width instead of widening the page.
func TestHistoryLongRepoName(t *testing.T) {
	st := sampleStatus()
	long := strings.Repeat("very-long-repository-name-", 4)
	st.Repos = append(st.Repos, model.RepoStatus{Name: long})
	m := feed(newModel(&fakeClient{}, 80, 30, st), key("3"))
	m.histRepo = long
	for i, line := range strings.Split(m.View(), "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line %d is %d wide: %q", i, lipgloss.Width(line), line)
		}
	}
}
```

In `internal/tui/tui_test.go`, replace:

```go
	onPatch  func(model.ConfigPatch) // applies a patch to cfg, as a daemon would
}
```

with:

```go
	onPatch  func(model.ConfigPatch) // applies a patch to cfg, as a daemon would
	hist     []model.HistoryEntry    // History returns the entries matching its filters
	histReqs []string                // each History request as "repo|conclusion"
}
```

In `internal/tui/tui_test.go`, replace:

```go
func (f *fakeClient) History(context.Context, string, string, int) ([]model.HistoryEntry, error) {
	return nil, nil
```

with:

```go
func (f *fakeClient) History(_ context.Context, repo, concl string, _ int) ([]model.HistoryEntry, error) {
	f.histReqs = append(f.histReqs, repo+"|"+concl)
	var out []model.HistoryEntry
	for _, e := range f.hist {
		if (repo == "" || e.Repo == repo) && (concl == "" || e.Conclusion == concl) {
			out = append(out, e)
		}
	}
	return out, nil
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `undefined: histResultSel`, `m.groups.histResult undefined`, `undefined: rowCopy` and `m.groups.histRepo undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/history.go`:

```go
package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	histRepoSel   = "hist/repo"
	histResultSel = "hist/result"
	histTable     = "hist/table"
	rowCopy       = "row/copy"
)

var resultOptions = []ui.Option{
	{Value: "", Label: "all"}, {Value: "success", Label: "success"},
	{Value: "failure", Label: "failure"}, {Value: "cancelled", Label: "cancelled"},
}

// histFilters brings the filter selects up to date with the model, which
// stays the source of truth because r and c change the filters too. The repo
// options follow the configured repos, keeping a filtered repo that has since
// been removed so the select still shows it.
func (m Model) histFilters() {
	g := m.groups
	names := m.repoNames()
	if m.histRepo != "" && !slices.Contains(names, m.histRepo) {
		names = append(names, m.histRepo)
	}
	opts := []ui.Option{{Value: "", Label: "all"}}
	for _, n := range names {
		opts = append(opts, ui.Option{Value: n, Label: clean(n)})
	}
	if !slices.Equal(g.histRepo.Options, opts) {
		g.histRepo = ui.NewSelect(histRepoSel, opts)
	}
	g.histRepo.SetValue(ui.Value{Text: m.histRepo})
	g.histResult.SetValue(ui.Value{Text: m.histConcl})
	g.hist.Set([]ui.Widget{g.histRepo, g.histResult, stop{histTable}})
}

// histApply adopts the selects' values as the filters, refetching the
// history when they changed.
func (m *Model) histApply() tea.Cmd {
	repo, concl := m.groups.histRepo.Value().Text, m.groups.histResult.Value().Text
	if repo == m.histRepo && concl == m.histConcl {
		return nil
	}
	m.histRepo, m.histConcl = repo, concl
	return m.fetchHistory()
}

// histFooterKeys are the footer hints for the focused History element.
func (m Model) histFooterKeys() []footerKey {
	if s, ok := m.groups.hist.Focused().(*ui.Select); ok {
		if s.Open() {
			return []footerKey{{"up", "move"}, {"enter", "pick"}, {"esc", "close"}}
		}
		return []footerKey{{"left", "prev"}, {"right", "next"}, {"enter", "open"}, {"tab", "next"}, {"?", "help"}, {"q", "quit"}}
	}
	return []footerKey{{"r", "repo"}, {"c", "result"}, {"enter", "copy URL"}, {"tab", "next"}, {"?", "help"}, {"q", "quit"}}
}

// histKey handles the History page's focus keys: tab and shift+tab move
// between the two filters and the table, and a focused filter takes its own
// keys. It reports false for keys the page and global keys handle.
func (m Model) histKey(k tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	m.histFilters()
	g := m.groups
	if ok, _ := g.hist.Key(k); ok {
		cmd := m.histApply()
		return true, m, cmd
	}
	switch k.String() {
	case "tab":
		g.hist.Next()
	case "shift+tab":
		g.hist.Prev()
	default:
		return false, m, nil
	}
	return true, m, nil
}

// historyPage renders the filter bar above the History table. An open
// dropdown's options push the table down, under their own select.
func (m Model) historyPage(w, h int) string {
	m.histFilters()
	g := m.groups
	f := g.hist.FocusedID()
	repo := strings.Split(g.histRepo.View(f == histRepoSel, 0), "\n")
	result := strings.Split(g.histResult.View(f == histResultSel, 0), "\n")
	left := sDim.Render("Repo ") + repo[0] + "    "
	bar := []string{left + sDim.Render("Result ") + result[0]}
	for _, l := range repo[1:] {
		bar = append(bar, "     "+l)
	}
	for _, l := range result[1:] {
		bar = append(bar, strings.Repeat(" ", ansi.StringWidth(left)+7)+l)
	}
	for i := range bar {
		bar[i] = ansi.Truncate(bar[i], w, "…") // a long repo name must not widen the page
	}
	bar = append(bar, "")

	inner := w - 4
	lines := []string{sDim.Render(fmt.Sprintf("  %-16s %-12s %-7s %-*s %-10s %s", "FINISHED", "REPO", "RUN", inner-62, "JOB", "RESULT", "DURATION"))}
	visible := max(h-len(bar)-3, 1)
	start := 0
	if m.histSel >= visible {
		start = m.histSel - visible + 1
	}
	for i := start; i < len(m.hist) && i < start+visible; i++ {
		e := m.hist[i]
		line := "  " + cell(e.FinishedAt.Local().Format("2006-01-02 15:04"), 16) + " " + cell(e.Repo, 12) + " " +
			cell("#"+e.RunNumber, 7) + " " + cell(e.JobName, inner-62)
		tail := " " + stateStyle(e.Conclusion).Render(cell(e.Conclusion, 10)) + " " + dur(e.FinishedAt.Sub(e.StartedAt))
		id := fmt.Sprintf("hist-%d", i)
		if i == m.histSel {
			lines = append(lines, m.selectedRow(id, line, rowButtons(ui.NewButton(rowCopy, "Copy run URL", ui.Secondary)), tail, inner))
		} else {
			lines = append(lines, m.row(id, false, line+tail, inner))
		}
	}
	if len(m.hist) == 0 {
		lines = append(lines, sDim.Render("  no finished jobs yet"))
	}
	title := "History"
	if f == histTable {
		title = "› History"
	}
	return strings.Join(bar, "\n") + "\n" + box(title, w, lines)
}
```

In `internal/tui/view.go`, replace:

```go
		return m.historyTab(w, h)
```

with:

```go
		return m.historyPage(w, h)
```

In `internal/tui/view.go`, replace:

```go
}

func (m Model) historyTab(w, h int) string {
	inner := w - 4
	filter := "repo: all"
	if m.histRepo != "" {
		filter = "repo: " + m.histRepo
	}
	if m.histConcl != "" {
		filter += "  result: " + m.histConcl
	} else {
		filter += "  result: all"
	}
	lines := []string{
		sDim.Render(filter + "   (r repo, c result, enter copies run URL)"),
		sDim.Render(fmt.Sprintf("  %-16s %-12s %-7s %-*s %-10s %s", "FINISHED", "REPO", "RUN", inner-62, "JOB", "RESULT", "DURATION")),
	}
	visible := h - 4
	start := 0
	if m.histSel >= visible {
		start = m.histSel - visible + 1
	}
	for i := start; i < len(m.hist) && i < start+visible; i++ {
		e := m.hist[i]
		line := "  " + cell(e.FinishedAt.Local().Format("2006-01-02 15:04"), 16) + " " + cell(e.Repo, 12) + " " +
			cell("#"+e.RunNumber, 7) + " " + cell(e.JobName, inner-62) + " " + stateStyle(e.Conclusion).Render(cell(e.Conclusion, 10)) + " " +
			dur(e.FinishedAt.Sub(e.StartedAt))
		lines = append(lines, m.row(fmt.Sprintf("hist-%d", i), i == m.histSel, line, inner))
	}
	if len(m.hist) == 0 {
		lines = append(lines, sDim.Render("  no finished jobs yet"))
	}
	return box("History", w, lines)
}
```

with:

```go
}
```

In `internal/tui/shell.go`, replace:

```go
	tabs                *tabStrip
}
```

with:

```go
	tabs                *tabStrip
	hist                ui.Group
	histRepo            *ui.Select
	histResult          *ui.Select
}
```

In `internal/tui/shell.go`, replace:

```go
	}
	g.dash.Set([]ui.Widget{g.add, g.pauseAll, stop{dashRepos}, stop{dashRunners}})
	g.dash.Focus(dashRepos)
```

with:

```go
		histRepo:   ui.NewSelect(histRepoSel, []ui.Option{{Value: "", Label: "all"}}),
		histResult: ui.NewSelect(histResultSel, resultOptions),
	}
	g.dash.Set([]ui.Widget{g.add, g.pauseAll, stop{dashRepos}, stop{dashRunners}})
	g.dash.Focus(dashRepos)
	g.hist.Set([]ui.Widget{g.histRepo, g.histResult, stop{histTable}})
	g.hist.Focus(histTable)
```

In `internal/tui/shell.go`, replace:

```go
		return []footerKey{{"r", "repo"}, {"c", "result"}, {"enter", "copy URL"}, {"?", "help"}, {"q", "quit"}}
```

with:

```go
		return m.histFooterKeys()
```

In `internal/tui/input.go`, replace:

```go
	}
	return m.press(key)
```

with:

```go
	}
	if m.page == pageHistory {
		if ok, mm, cmd := m.histKey(k); ok {
			return mm, cmd
		}
	}
	return m.press(key)
```

In `internal/tui/input.go`, replace:

```go
		}
	case "pgdown":
```

with:

```go
		}
		if m.page == pageHistory {
			m.histSel = clamp(m.histSel-10, len(m.hist))
		}
	case "pgdown":
```

In `internal/tui/input.go`, replace:

```go
			m.scrollDetail(10)
		}
```

with:

```go
			m.scrollDetail(10)
		}
		if m.page == pageHistory {
			m.histSel = clamp(m.histSel+10, len(m.hist))
		}
```

In `internal/tui/input.go`, replace:

```go
		m.histSel = clamp(m.histSel+d, len(m.hist))
```

with:

```go
		if m.groups.hist.FocusedID() == histTable {
			m.histSel = clamp(m.histSel+d, len(m.hist))
		}
```

In `internal/tui/input.go`, replace:

```go
	}
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
```

with:

```go
	}
	if m.overlay == ovNone && m.page == pageHistory {
		// Before the sidebar, so an open dropdown can swallow a click anywhere.
		m.histFilters()
		if ok, _ := m.groups.hist.Mouse(msg); ok {
			cmd := m.histApply()
			return m, cmd
		}
	}
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
```

In `internal/tui/input.go`, replace:

```go
	for id, k := range map[string]string{rowPause: "p", rowRemove: "d", rowLogs: "l", rowStop: "x"} {
```

with:

```go
	for id, k := range map[string]string{rowPause: "p", rowRemove: "d", rowLogs: "l", rowStop: "x", rowCopy: "enter"} {
```

In `internal/tui/input.go`, replace:

```go
		m.histSel = i
		return m, nil
```

with:

```go
		m.histSel = i
		m.groups.hist.Focus(histTable)
		return m, nil
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run TestHistory`
Expected: PASS for `TestHistoryFilterBar`, `TestHistoryFilterMouse`, `TestHistoryCopyRunURL`, `TestHistoryFocusKeys` and `TestHistoryLongRepoName`.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/history.go internal/tui/history_test.go internal/tui/input.go internal/tui/shell.go internal/tui/tui_test.go internal/tui/view.go
git commit -m "feat(tui): add history filter bar"
```

### Task 10: Help grouped by page

**Files:**
- Modify: `internal/tui/dialogs.go` (`helpText`, `helpLines`, `helpBody`, `openHelp`, `dialogKey`, `withOverlay`)
- Modify: `internal/tui/model.go` (`helpScroll`)
- Test: `internal/tui/dialogs_test.go`

**Interfaces:**
- Consumes: Plan 2's **TUI dialogs** (`modal`)
- Produces: none

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/dialogs_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Help lists every group and fits a 22-row screen at 80 columns and wider.
func TestHelpGroupsFit(t *testing.T) {
	for _, w := range []int{80, 120} {
		m := feed(sampleModel(&fakeClient{}, w, 22), key("?"))
		v := m.View()
		for _, want := range []string{"Global", "Dashboard", "Runners", "Detail", "History", "Settings",
			"switch tab", "copy run URL", "takes digits", "set -g mouse on", "[ Close ]"} {
			if !strings.Contains(v, want) {
				t.Errorf("width %d: help missing %q", w, want)
			}
		}
		lines := strings.Split(ansi.Strip(v), "\n")
		for _, p := range [][2]string{{"Dashboard", "History"}, {"Runners", "Settings"}} {
			top, below := lines[lineWith(lines, p[0])], lines[lineWith(lines, p[1])]
			if ansi.StringWidth(top[:strings.Index(top, p[0])]) != ansi.StringWidth(below[:strings.Index(below, p[1])]) {
				t.Errorf("width %d: %s is not under %s:\n%s", w, p[1], p[0], v)
			}
		}
		if h := lipgloss.Height(v); h > 22 {
			t.Errorf("width %d: help is %d rows on a 22-row screen:\n%s", w, h, v)
		}
		for i, line := range strings.Split(v, "\n") {
			if lipgloss.Width(line) > w {
				t.Fatalf("width %d: line %d is %d wide", w, i, lipgloss.Width(line))
			}
		}
	}
}

// Below three columns' width the groups stack into one column instead of
// wrapping mid-row.
func TestHelpStacksWhenNarrow(t *testing.T) {
	text := helpText(60)
	if !strings.Contains(text, "Global") || strings.Contains(text, "Global                   Dashboard") {
		t.Fatalf("narrow help did not stack:\n%s", text)
	}
	for _, line := range strings.Split(text, "\n")[:30] {
		if lipgloss.Width(line) > helpColW {
			t.Fatalf("stacked line %q is wider than one column", line)
		}
	}
}

// Too narrow for three columns, Help is taller than a 22-row screen: it shows
// what fits with a scroll hint, keeps Close in view, and scrolls to the rest.
func TestHelpScrollsWhenShort(t *testing.T) {
	for _, w := range []int{40, 56} {
		m := feed(sampleModel(&fakeClient{}, w, 22), key("?"))
		check := func(want, gone string) {
			t.Helper()
			v := m.View()
			if h := lipgloss.Height(v); h > 22 {
				t.Fatalf("width %d: help is %d rows:\n%s", w, h, v)
			}
			for i, line := range strings.Split(v, "\n") {
				if lipgloss.Width(line) > w {
					t.Fatalf("width %d: line %d is %d wide", w, i, lipgloss.Width(line))
				}
			}
			if !strings.Contains(v, want) || strings.Contains(v, gone) || !strings.Contains(v, "[ Close ]") || !strings.Contains(v, "↑↓ scroll") {
				t.Fatalf("width %d: want %q without %q:\n%s", w, want, gone, v)
			}
		}
		check("Global", "Shift-drag")
		m = feed(m, keys("pgdown", "pgdown", "pgdown", "pgdown", "pgdown")...)
		check("Shift-drag", "Global")
		if m = feed(m, key("esc")); m.overlay != ovNone {
			t.Fatalf("width %d: esc did not close help", w)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s`
Expected: FAIL to compile with `too many arguments in call to helpText` and `undefined: helpColW`.

- [ ] **Step 3: Write the implementation**

In `internal/tui/dialogs.go`, replace:

```go
func (m Model) openHelp() (tea.Model, tea.Cmd) {
	m.openDialog(ovHelp, btnClose, ui.NewButton(btnClose, "Close", ui.Primary))
```

with:

```go
func (m Model) openHelp() (tea.Model, tea.Cmd) {
	m.helpScroll = 0
	m.openDialog(ovHelp, btnClose, ui.NewButton(btnClose, "Close", ui.Primary))
```

In `internal/tui/dialogs.go`, replace:

```go
func (m Model) dialogKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
```

with:

```go
func (m Model) dialogKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.overlay == ovHelp {
		if d, ok := map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -10, "pgdown": 10}[k.String()]; ok {
			lines, n := m.helpLines(max(m.width, 40))
			m.helpScroll = min(max(m.helpScroll+d, 0), max(len(lines)-n, 0))
			return m, nil
		}
	}
	switch k.String() {
```

In `internal/tui/dialogs.go`, replace:

```go
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
```

with:

```go
type helpGroup struct {
	title string
	keys  [][2]string // key, what it does
}

// helpGroups in reading order. They render as three columns of two groups,
// Global over Detail, Dashboard over History and Runners over Settings, so
// the dialog fits the 22-row minimum screen.
var helpGroups = []helpGroup{
	{"Global", [][2]string{{"1-4", "switch page"}, {"tab", "move focus"}, {"↑↓ j k", "move selection"}, {"? / q", "help / quit"}}},
	{"Dashboard", [][2]string{{"h / →", "repos / runners"}, {"p / P", "pause repo/all"}, {"+ - [ ]", "repo/global cap"}, {"m", "queue/all mode"}, {"a / d", "add/remove repo"}}},
	{"Runners", [][2]string{{"enter", "open details"}, {"l / x", "log / stop"}, {"pgup/dn", "scroll the log"}}},
	{"Detail", [][2]string{{"← / →", "switch tab"}, {"x / esc", "stop / back"}}},
	{"History", [][2]string{{"r / c", "repo / result"}, {"enter", "copy run URL"}}},
	{"Settings", [][2]string{{"ctrl+s", "save"}, {"← / →", "choose / step"}, {"enter", "open / edit"}, {"esc", "close / stop"}}},
}

const helpColW = 23 // an 8-column key and a 15-column description

// helpText lists the keys by group in three columns, stacked into one when
// inner columns cannot hold three, followed by the notes on digits, the mouse
// and tmux.
func helpText(inner int) string {
	col := func(gs ...helpGroup) []string {
		var out []string
		for i, g := range gs {
			if i > 0 {
				out = append(out, "")
			}
			out = append(out, sAccent.Render(cell(g.title, helpColW)))
			for _, k := range g.keys {
				out = append(out, sBold.Render(cell(k[0], 8))+cell(k[1], helpColW-8))
			}
		}
		return out
	}
	g := helpGroups
	cols := [][]string{col(g[0], g[3]), col(g[1], g[4]), col(g[2], g[5])}
	var lines []string
	if inner < 3*helpColW+4 {
		lines = col(g...)
	} else {
		for i := 0; i < max(len(cols[0]), len(cols[1]), len(cols[2])); i++ {
			row := make([]string, 3)
			for c := range cols {
				if i < len(cols[c]) {
					row[c] = cols[c][i]
				}
				row[c] = cell(row[c], helpColW)
			}
			lines = append(lines, strings.TrimRight(strings.Join(row, "  "), " "))
		}
	}
	return strings.Join(append(lines, "",
		"A focused stepper takes digits: 1-4 type into it instead of switching.",
		"Mouse: click anything; double-click a runner; the wheel scrolls.",
		"Shift-drag selects text (Option-drag in iTerm2). tmux: set -g mouse on",
	), "\n")
}

// helpLines is the Help text wrapped for a w-column screen, and how many of
// its lines the dialog can show: all of them when they fit the height,
// otherwise one less than the room, leaving a line for the scroll hint.
func (m Model) helpLines(w int) ([]string, int) {
	inner := max(w-6, 20) // as modal computes it
	text := helpText(inner)
	if lipgloss.Width(text) > inner {
		text = lipgloss.NewStyle().Width(inner).Render(text)
	}
	lines := strings.Split(text, "\n")
	room := max(m.height-8, 3) // the dialog's border, padding, title and buttons take 8 rows
	if len(lines) <= room {
		return lines, len(lines)
	}
	return lines, room - 1
}

// helpBody is the part of the Help text that fits the screen, with a hint
// line when ↑/↓ or pgup/pgdn can scroll the rest into view.
func (m Model) helpBody(w int) string {
	lines, n := m.helpLines(w)
	if n == len(lines) {
		return strings.Join(lines, "\n")
	}
	start := min(m.helpScroll, len(lines)-n)
	hint := sDim.Render(fmt.Sprintf("↑↓ scroll · lines %d-%d of %d", start+1, start+n, len(lines)))
	return strings.Join(append(lines[start:start+n:start+n], hint), "\n")
```

In `internal/tui/dialogs.go`, replace:

```go
		dialog = modal("Keys", helpText(), m.dlgButtons, m.dlg.FocusedID(), w)
```

with:

```go
		dialog = modal("Keys", m.helpBody(w), m.dlgButtons, m.dlg.FocusedID(), w)
```

In `internal/tui/model.go`, replace:

```go
	confirmText   string
	confirmAction func() tea.Cmd
```

with:

```go
	confirmText   string
	helpScroll    int // first Help line shown when Help is taller than the screen
	confirmAction func() tea.Cmd
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run TestHelp`
Expected: PASS for `TestHelpGroupsFit`, `TestHelpStacksWhenNarrow`, `TestHelpScrollsWhenShort` and `TestHelpMentionsMouseNotes`.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/dialogs.go internal/tui/dialogs_test.go internal/tui/model.go
git commit -m "feat(tui): group help keys by page"
```

### Task 11: Page snapshots and narrow screens

**Files:**
- Modify: `internal/tui/detail.go` (`detailPage`)
- Modify: `internal/tui/shell.go` (`layout`, `fitLines`, `pageHeader`)
- Test: `internal/tui/tui_test.go` (`TestDashboardGolden` gives way)
- Delete: `internal/tui/testdata/TestDashboardGolden/`
- Create (generated): `internal/tui/testdata/TestPagesGolden/{dashboard,runners,detail,history}/{120,80}.golden`

**Interfaces:**
- Consumes: **Pages**, **Detail page**, **History filters**
- Produces: `pageModels` (see Contracts, **TUI test helpers**)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

In `internal/tui/tui_test.go`, replace:

```go
func TestDashboardGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			v := sampleModel(&fakeClient{}, w, 30).View()
			golden.RequireEqual(t, []byte(stripTimes(v)))
		})
	}
```

with:

```go
// pageModels builds the sample model on each page other than Settings, w
// columns wide.
var pageModels = map[string]func(w int) Model{
	"dashboard": func(w int) Model { return sampleModel(&fakeClient{}, w, 30) },
	"runners":   func(w int) Model { return feed(sampleModel(&fakeClient{}, w, 30), key("2")) },
	"detail": func(w int) Model {
		c := &fakeClient{steps: []model.Step{
			{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success"},
			{Number: 2, Name: "Run e2e journeys", Status: "in_progress"},
		}}
		st := sampleStatus()
		st.Instances[0].Job.HTMLURL = "https://github.com/darkraise/darkcloud/actions/runs/1"
		c.st = &st
		return feed(newModel(c, w, 30, st), keys("2", "enter")...)
	},
	"history": func(w int) Model {
		m := feed(sampleModel(&fakeClient{}, w, 30), key("3"))
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
	m := feed(newModel(&fakeClient{}, 80, 22, st), keys("3", "tab", "enter")...)
	if !m.groups.histRepo.Open() {
		t.Fatal("the Repo filter did not open")
	}
	fits(t, "history dropdown", m.View(), 80, 22)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -run "TestPagesGolden|TestPagesFitNarrow|TestDetailStatesFitNarrow|TestHistoryDropdownFitsShortScreen"`
Expected: `TestPagesGolden` fails to open `testdata/TestPagesGolden/<page>/<width>.golden` (not written yet), `TestPagesFitNarrow` fails with pages shorter than the screen (`dashboard at 56 columns: 22 rows, want 30`, `detail at 80 columns: 10 rows, want 30`, `history at 80 columns: 11 rows, want 30`) and detail lines too wide (`detail at 80 columns: line 3 is 91 wide`, `detail at 40 columns: line 2 is 54 wide`), and `TestDetailStatesFitNarrow` and `TestHistoryDropdownFitsShortScreen` fail the same ways.

- [ ] **Step 3: Write the implementation**

First delete the old Dashboard snapshots, which `TestPagesGolden` replaces (Step 1 removed `TestDashboardGolden`): run `git rm -r -q internal/tui/testdata/TestDashboardGolden`.

In `internal/tui/detail.go`, replace:

```go
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
```

with:

```go
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
```

In `internal/tui/detail.go`, replace:

```go
	out := append(top, body[:min(len(body), bodyH)]...)
	return strings.Join(out, "\n")
```

with:

```go
	out := append(top, body[:min(len(body), bodyH)]...)
	for i := range out {
		out[i] = ansi.Truncate(out[i], w, "…") // the summary, step names, errors and container lines can be long
	}
	return strings.Join(out, "\n")
```

In `internal/tui/shell.go`, replace:

```go
}

// pageHeader is the first line of the content area: the page title on the
// left and the page's status on the right.
```

with:

```go
}

// fitLines pads or cuts s to exactly n lines, so the footer stays on the
// bottom row and a dialog's canvas is the height of the screen.
func fitLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines[:n], "\n")
}

// pageHeader is the first line of the content area: the page title on the
// left and the page's status on the right.
```

In `internal/tui/shell.go`, replace:

```go
	gap := max(w-ansi.StringWidth(title)-ansi.StringWidth(right), 1)
	return title + strings.Repeat(" ", gap) + right
```

with:

```go
	// Too narrow: the title gives way first, then the buttons are cut.
	if over := ansi.StringWidth(title) + ansi.StringWidth(right) + 1 - w; over > 0 {
		title = ansi.Truncate(title, max(ansi.StringWidth(title)-over, 1), "…")
	}
	gap := max(w-ansi.StringWidth(title)-ansi.StringWidth(right), 1)
	return ansi.Truncate(title+strings.Repeat(" ", gap)+right, w, "…")
```

In `internal/tui/shell.go`, replace:

```go
	content := m.pageHeader(cw) + "\n" + body(cw, ch)
```

with:

```go
	content := m.pageHeader(cw) + "\n" + fitLines(body(cw, ch), ch)
```

- [ ] **Step 4: Regenerate the snapshots**

Run: `go test ./internal/tui/ -run TestPagesGolden -update -count=1 -timeout 180s`
Expected: the command exits 0 and writes eight snapshots, two per page. The two Dashboard files are identical to the deleted `TestDashboardGolden` ones. Open each file and compare it with the snapshot below (trailing spaces are trimmed here; the files keep them).

`internal/tui/testdata/TestPagesGolden/dashboard/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
▌ 1 Dashboard    Dashboard                                                                    [ + Add ]    ( Pause all )
  2 Runners      ╭─ Running ─────────────╮ ╭─ Queued jobs ─────────╮ ╭─ Repositories ────────╮ ╭─ Disk ────────────────╮
  3 History      │ 2 / 3                 │ │ 3                     │ │ 2 active · 1 paused   │ │ 61%                   │
  4 Settings     ╰───────────────────────╯ ╰───────────────────────╯ ╰───────────────────────╯ ╰───────────────────────╯
                 ╭─ › Repositories ────────────────────────────────────────────────────────────────────────────────────╮
                 │   REPO           STATE      RUN   QUEUE  LAST JOB                                                   │
                 │ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint  2m ago                  ( Pause )  [ Remove ] │
                 │   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago                                 │
                 │   darkagents     ◌ paused   0/1   ⧗ 1    –                                                          │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Runners ───────────────────────────────────────────────────────────────────────────────────────────╮
                 │   ID       REPO         STATE       JOB                                               ELAPSED       │
                 │   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412                           12m04s        │
                 │   7be210   darkmem      ○ idle      –                                                 1m30s         │
                 │   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)                                      │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Activity ──────────────────────────────────────────────────────────────────────────────────────────╮
                 │ HH:MM:SS  ✔ darkcloud   #411 lint success 2m10s  cleanup: 3 ctrs                                    │
                 │ HH:MM:SS  ▶ darkcloud   spawned a3f9c1 (2.330.0)                                                    │
                 │ HH:MM:SS  ⚠             disk 81% > high-water 80% — pruned build cache                              │
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

`internal/tui/testdata/TestPagesGolden/dashboard/80.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api 4800   disk ▕██████░░░░▏ 61%   ●
[ 1 Dashboard ]  2 Runners    3 History    4 Settings                    ? help
Dashboard                                             [ + Add ]    ( Pause all )
 Running 2 / 3   Queued jobs 3   Repositories 2 active · 1 paused   Disk 61%
╭─ › Repositories ─────────────────────────────────────────────────────────────╮
│   REPO           STATE      RUN   QUEUE  LAST JOB                            │
│ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint…  ( Pause )  [ Remove ] │
│   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago          │
│   darkagents     ◌ paused   0/1   ⧗ 1    –                                   │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Runners ────────────────────────────────────────────────────────────────────╮
│   ID       REPO         STATE       JOB                        ELAPSED       │
│   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412    12m04s        │
│   7be210   darkmem      ○ idle      –                          1m30s         │
│   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)               │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Activity ───────────────────────────────────────────────────────────────────╮
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

`internal/tui/testdata/TestPagesGolden/runners/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
  1 Dashboard    Runners
▌ 2 Runners      ╭─ › Runners ─────────────────────────────────────────────────────────────────────────────────────────╮
  3 History      │   ID       REPO         STATE       JOB                                               ELAPSED       │
  4 Settings     │ ▸ a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412          …  ( Logs )  [ Stop ] 12m04s   │
                 │   7be210   darkmem      ○ idle      –                                                 1m30s         │
                 │   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)                                      │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
                 ╭─ Log preview — a3f9c1 (following) ──────────────────────────────────────────────────────────────────╮
                 │ a3f9c1 after ""                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
                 │                                                                                                     │
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
 x kill  l logs  enter details  ? help  q quit
```

`internal/tui/testdata/TestPagesGolden/runners/80.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api 4800   disk ▕██████░░░░▏ 61%   ●
  1 Dashboard  [ 2 Runners ]  3 History    4 Settings                    ? help
Runners
╭─ › Runners ──────────────────────────────────────────────────────────────────╮
│   ID       REPO         STATE       JOB                        ELAPSED       │
│ ▸ a3f9c1   darkcloud    ⣾ busy      CI / e2e-j…  ( Logs )  [ Stop ] 12m04s   │
│   7be210   darkmem      ○ idle      –                          1m30s         │
│   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)               │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Log preview — a3f9c1 (following) ───────────────────────────────────────────╮
│ a3f9c1 after ""                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
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
 x kill  l logs  enter details  ? help  q quit
```

`internal/tui/testdata/TestPagesGolden/detail/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
  1 Dashboard    Runners › a3f9c1                                                    ( Copy run URL )    [ Stop runner ]
▌ 2 Runners      ● busy  ·  darkcloud  ·  CI / e2e-journeys  #412  ·  12m04s  ·  started 2026-10-03 13:52:56
  3 History
  4 Settings     › [ Steps ]   Log     Containers

                 ✔ Set up job
                 ⣾ Run e2e journeys



















  ? Help
  q Quit
 esc back  left prev tab  right next tab  x stop  ? help  q quit
```

`internal/tui/testdata/TestPagesGolden/detail/80.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api 4800   disk ▕██████░░░░▏ 61%   ●
  1 Dashboard  [ 2 Runners ]  3 History    4 Settings                    ? help
Runners › a3f9c1                             ( Copy run URL )    [ Stop runner ]
● busy  ·  darkcloud  ·  CI / e2e-journeys  #412  ·  12m04s  ·  started 2026-10…

› [ Steps ]   Log     Containers

✔ Set up job
⣾ Run e2e journeys




















 esc back  left prev tab  right next tab  x stop  ? help  q quit
```

`internal/tui/testdata/TestPagesGolden/history/120.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%   ● connected
  1 Dashboard    History
  2 Runners      Repo   [ all ▾ ]    Result   [ all ▾ ]
▌ 3 History
  4 Settings     ╭─ › History ─────────────────────────────────────────────────────────────────────────────────────────╮
                 │   FINISHED         REPO         RUN     JOB                                   RESULT     DURATION   │
                 │   2026-10-03 14:03 darkcloud    #411    lint                   …  ( Copy run URL ) success    2m00s │
                 │   2026-10-03 13:05 darkmem      #87     build / test                          failure    10m00s     │
                 ╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯


















  ? Help
  q Quit
 r repo  c result  enter copy URL  tab next  ? help  q quit
```

`internal/tui/testdata/TestPagesGolden/history/80.golden`:

```text
 ghr   mode ● QUEUE   runners ▕██░▏ 2/3   api 4800   disk ▕██████░░░░▏ 61%   ●
  1 Dashboard    2 Runners  [ 3 History ]  4 Settings                    ? help
History
Repo   [ all ▾ ]    Result   [ all ▾ ]

╭─ › History ──────────────────────────────────────────────────────────────────╮
│   FINISHED         REPO         RUN     JOB            RESULT     DURATION   │
│   2026-10-03 14:03 darkcloud    #411    …  ( Copy run URL ) success    2m00s │
│   2026-10-03 13:05 darkmem      #87     build / test   failure    10m00s     │
╰──────────────────────────────────────────────────────────────────────────────╯



















 r repo  c result  enter copy URL  tab next  ? help  q quit
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -count=1 -timeout 180s -v -run "TestPagesGolden|TestPagesFitNarrow|TestDetailStatesFitNarrow|TestHistoryDropdownFitsShortScreen"`
Expected: PASS for all four tests and the eight subtests of `TestPagesGolden`.

- [ ] **Step 6: Run the whole suite and the checks**

Run: `go test ./... -count=1 -timeout 180s && go vet ./... && gofmt -l cmd internal`
Expected: every package `ok`, `go vet` silent, `gofmt` prints nothing.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/detail.go internal/tui/shell.go internal/tui/tui_test.go internal/tui/testdata/TestPagesGolden
git commit -m "test(tui): snapshot pages and fit narrow screens"
```

### Task 12: Release and LXC acceptance

**Files:**
- None in ghr. Modify: `D:/Repositories/Personal/homelab/docs/superpowers/registers/2026-10-04-ghr-tui-revamp.md` (row 3 state)

**Interfaces:**
- Consumes: everything above, as merged from Tasks 1-11
- Produces: none

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 2 = 3

Every SSH command runs from Git Bash with `timeout` and `-o BatchMode=yes`. The TUI checks drive a real terminal on the LXC: `tmux`, keys through `send-keys`, and clicks as SGR mouse sequences written to the pane (`\e[<0;COL;ROWM` press, `…m` release; columns and rows count from 1). Each check starts its own 120×40 session, named `accept-<pid>` so it cannot touch anyone else's, stops at once if tmux cannot create it, and kills only that session when the script ends. Each script's helper `w` waits for text on the screen and exits the script at the first miss, so a script that ends with `step=ok` passed every check; anything else is a failure to report with the printed `found=` line. The acceptance changes no setting: it only reads, and dispatches one 30-second `ghr-e2e` job.

- [ ] **Step 1: Run the final review before releasing**

Only reviewed code may be released, and dr-superpowers:executing-plans reaches its whole-branch final review only after every task, this one included, is complete. This step runs in the controlling session, never in a dispatched implementer, because it may run the review and its fix wave. Run in `D:/Repositories/Personal/ghr`: `git rev-parse --short=7 feat/pages-revamp` (the tip) and `git rev-parse --short=7 $(git merge-base master feat/pages-revamp)` (the merge base); the ledger records seven-character hashes. Then read the ledger `D:/Repositories/Personal/homelab/.superpowers/sdd/2026-10-04-ghr-tui-revamp-3-pages/progress.md`. If it holds a `Final review: clean (commits <base>..<tip>…)` line whose tip is the branch tip, go on. Otherwise run the final review now, exactly as executing-plans' Final Review section describes (`reference/final-review.md`), over `<merge-base>..feat/pages-revamp`, apply its fix wave, and append its `Final review: clean …` line to the ledger. If it is not clean after its one fix wave, stop and report the open findings: do not ask for release approval. When execution later reaches the Final Review section after this task, that ledger line already records it.

- [ ] **Step 2: Pre-flight on the LXC**

Run:

```bash
timeout 30 gh release list -R darkraise/ghr --limit 1 --json tagName -q '.[0].tagName'
timeout 60 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 '
ghr version; ghr status | head -1; ghr history --limit 3
sha256sum /etc/ghr/config.yaml; command -v tmux'
```

Expected: the release tag and `ghr version` print the same tag, Plan 2's release (call it `PREV`); the status line shows `global 0/` (no runner alive); `ghr history` lists at least one finished job (Step 6 needs a History row); a checksum (record it as `CFG0`); a `tmux` path. If a runner is alive, wait until `ghr status` shows `global 0/` again before Step 7. If `ghr version` is not the latest release, stop and report: Plan 2's release is not deployed.

- [ ] **Step 3: Ask for approval to release**

Ask your human partner, in one message: "Plan 3 passes all tests and its final review on `feat/pages-revamp` (commit `<sha>`). May I fast-forward `master`, push (publishing a release), deploy that release to the LXC with `GHR_VERSION` pinned, and run the TUI acceptance there? The acceptance changes no setting; it dispatches the `single` workflow of `darkraise/ghr-e2e` once (one 30-second job on the LXC's runners) to watch a live runner on the detail page." Wait for an explicit yes. If the answer is no, stop here: leave register row 3 at `planned` and report that Task 12 awaits approval.

- [ ] **Step 4: Merge, push, and wait for this commit's release**

Run in `D:/Repositories/Personal/ghr`:

```bash
git checkout master && git merge --ff-only feat/pages-revamp && SHA=$(git rev-parse HEAD) && echo "sha=$SHA"
timeout 120 git push origin master
RUN=""; for i in $(seq 20); do RUN=$(timeout 30 gh run list --workflow ci --commit "$SHA" --limit 1 --json databaseId -q '.[0].databaseId'); [ -n "$RUN" ] && break; sleep 3; done; echo "run=$RUN"
timeout 600 gh run watch "$RUN" --exit-status --interval 15 > /dev/null; echo "ci exit=$?"
timeout 60 git fetch --tags origin
TAG=$(timeout 30 gh release list --limit 1 --json tagName -q '.[0].tagName'); echo "tag=$TAG"
[ "$(git rev-list -n 1 "$TAG")" = "$SHA" ] && echo tag-on-sha=yes || echo tag-on-sha=NO
```

Expected: a non-empty `run=`, `ci exit=0`, a `tag=` one patch above `PREV`, and `tag-on-sha=yes`. Call the tag `TAG` below. If CI fails or the tag is not on `SHA`, stop and report; do not deploy.

- [ ] **Step 5: Deploy the pinned release**

Run, replacing `TAG`:

```bash
timeout 590 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'cd /root/github-runner && GHR_VERSION=TAG timeout 570 bash setup.sh > /root/setup-TAG.log 2>&1; echo rc=$?; tail -8 /root/setup-TAG.log; systemctl is-active ghr; ghr version'
```

Expected: `rc=0`, a log line `installing ghr (TAG)`, a status table listing the four repos, `active`, and `TAG`.

- [ ] **Step 6: Dashboard, History and Help**

Run:

```bash
timeout 120 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 '
w() { timeout 15 bash -c "until tmux capture-pane -p -t $S | grep -qF -- \"$1\"; do sleep 0.3; done"; rc=$?; echo "found=$rc ($1)"; [ $rc -eq 0 ] || exit 1; }
click() { tmux send-keys -t $S -l "$(printf "\e[<0;%d;%dM\e[<0;%d;%dm" $1 $2 $1 $2)"; }
S=accept-$$
tmux new-session -d -s $S -x 120 -y 40 ghr || { echo "tmux could not create $S"; exit 1; }
trap "tmux kill-session -t $S" EXIT
w "1 Dashboard"
w "╭─ Running"; w "╭─ Queued jobs"; w " active · "; w "[ + Add ]"; w "all )"; w "╭─ › Repositories"
tmux send-keys -t $S Tab; w "╭─ › Runners"
tmux send-keys -t $S Tab; w "› [ + Add ]"
click 5 4; w "Repo   [ all ▾ ]"; w "╭─ › History"; w "( Copy run URL )"
tmux send-keys -t $S c; w "Result   [ success ▾ ]"
tmux send-keys -t $S "?"; w "Global"; w "switch tab"; w "copy run URL"
tmux send-keys -t $S Escape
timeout 10 bash -c "while tmux capture-pane -p -t $S | grep -q \"switch tab\"; do sleep 0.3; done" || { echo "help did not close"; exit 1; }
echo step=ok'
```

Expected: every `found=0`, then `step=ok`. The Dashboard shows the four stat tiles, the header buttons (`( Pause all )`, or `( Resume all )` if every repo is paused) and the focused Repositories card; two tabs reach `+ Add`. A click on the sidebar's History row opens History with its filter bar and the selected row's Copy run URL button; `c` sets the Result filter to success. `?` opens Help, grouped by page, and `esc` closes it.

- [ ] **Step 7: Runners page and the detail page with a live runner**

Run in Git Bash here, then at once the script:

```bash
timeout 30 gh workflow run single -R darkraise/ghr-e2e && echo dispatched=yes
timeout 300 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 '
w() { timeout ${2:-15} bash -c "until tmux capture-pane -p -t $S | grep -qF -- \"$1\"; do sleep 0.3; done"; rc=$?; echo "found=$rc ($1)"; [ $rc -eq 0 ] || exit 1; }
click() { tmux send-keys -t $S -l "$(printf "\e[<0;%d;%dM\e[<0;%d;%dm" $1 $2 $1 $2)"; }
S=accept-$$
tmux new-session -d -s $S -x 120 -y 40 ghr || { echo "tmux could not create $S"; exit 1; }
trap "tmux kill-session -t $S" EXIT
w "1 Dashboard"
tmux send-keys -t $S 2; w "Log preview"
w " busy " 150; w "( Logs )"; w "(following)"
tmux send-keys -t $S Enter; w "Runners › "; w "› [ Steps ]"; w "Set up job" 60
click 30 40; w "[ Log ]"; w "Log — following"
w "This runner has finished" 180; w "Back to runners"
tmux send-keys -t $S BTab Enter; w "Log preview — no runner selected" 30
echo step=ok'
```

Expected: `dispatched=yes`, every `found=0`, then `step=ok`. The Runners page shows the job's runner turning busy with its row buttons and a following log preview; `enter` opens its detail page on the Steps tab, where GitHub's first step appears; a click on the footer's `right next tab` (column 30 of the bottom row) switches to the Log tab. When the job ends and the runner leaves `/status`, the page says it has finished; shift+tab focuses Back to runners (Stop runner is disabled) and `enter` returns to the Runners page, whose preview no longer follows a runner. If `found=1 ( busy )`, check `gh run list -R darkraise/ghr-e2e --limit 1` and `ghr status` and report what they show.

- [ ] **Step 8: Confirm nothing changed**

Run:

```bash
timeout 60 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'sha256sum /etc/ghr/config.yaml; ghr status | head -1; ghr history --limit 1'
```

Expected: the checksum equals `CFG0`, the status line shows `global 0/`, and the newest history row is `ghr-e2e … one success`. If the checksum differs, report it with `diff` against `/root/config-before-plan2.yaml` if that file still exists; do not edit the config.

- [ ] **Step 9: Record the result**

Run in `D:/Repositories/Personal/homelab`, replacing `TAG`:

```bash
bash /d/Repositories/Personal/darkraise-ai-plugins/plugins/dr-superpowers/scripts/register set docs/superpowers/registers/2026-10-04-ghr-tui-revamp.md 3 done --note "ghr TAG on the LXC: Dashboard tiles, header buttons and card tab stops, History filter bar and row button, Help grouped by page, Runners log preview, and a live ghr-e2e runner on the tabbed detail page through to the finished alert, driven in tmux (keys and mouse); config.yaml unchanged"
git add docs/superpowers/registers/2026-10-04-ghr-tui-revamp.md
git commit -m "docs(github-runner): record ghr TAG pages"
```

Expected: `register` exits 0 and the commit succeeds. Do not push homelab.
