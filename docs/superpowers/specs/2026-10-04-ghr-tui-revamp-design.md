# ghr TUI revamp: admin-console layout and a real Settings form

**Date:** 2026-10-04
**Status:** approved in chat section by section; revised after Fable review (2026-10-04)
**Register:** docs/superpowers/registers/2026-10-04-ghr-tui-revamp.md
**Code:** github.com/darkraise/ghr (`internal/tui`, `internal/model`, `internal/daemon`, `cmd/ghr`)
**Builds on:** docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md (§TUI)

## Goal

Make the ghr terminal app feel like a web admin console: sidebar navigation, a status top bar, cards, real form controls and toasts. Replace the Config tab's free-text prompt with a Settings form that edits every setting in `config.yaml` except `owner`. Make bare `ghr` open the app.

## Decisions

- **Look:** an admin console (sidebar, top bar, cards, form controls, Save/Discard). Chosen over a GitHub-styled look and over keeping today's layout.
- **Settings scope:** every setting except `owner`, so the daemon's config patch API grows to cover the rest.
- **Implementation:** a small widget set built on the current stack (Bubble Tea v1, lipgloss v1, bubblezone v1).
  - `huh` was rejected because its forms are built to own the screen, which fights a persistent sidebar layout. (Its v0 line does run on Bubble Tea v1.)
  - Restyling in place without new structure was rejected because `input.go` and `view.go` would grow past what stays maintainable.
- **No compositing:** nothing is ever drawn over other content. Dropdowns open inline and push the content below them down. The toast has a reserved line. A modal replaces the whole screen, as `withOverlay` does today. The reason is that a bubblezone marker is one identical escape sequence at the start and end of its region, and `Scan` pairs them up (bubblezone `manager.go:166-177`). Cutting a column span out of a line orphans those markers and breaks the click regions on that line.
- **One toast at a time.** A newer toast replaces the current one.

## 1. Launch, shell and navigation

**Launching:**
- `ghr` with no arguments opens the TUI when stdin and stdout are both terminals.
- Otherwise it keeps today's behaviour (usage on stderr, exit code 2), so scripts are unaffected.
- `ghr tui` still works, and the usage text lists bare `ghr` first.
- `cmd/ghr/main.go` gains `var runTUI = tui.Run` and `var isTerminal = func() bool { ... }`. The latter checks the `os.Stdin` and `os.Stdout` file descriptors with `charmbracelet/x/term` (already in the module graph). Both are package variables so tests can replace them, following the existing `newClient` precedent.

**Wide layout (100 columns or more):**
- **Top bar:** one line of status chips: mode; runners against the global cap (a gauge); GitHub API remaining (a gauge); disk (a gauge, amber at or above `disk_high_water`, red at 95%); and connection state.
- **Alert line:** directly under the top bar, full width, shown only while the daemon is unreachable or degraded.
- **Toast line:** directly under that, full width, shown only while a toast is active (see Toasts).
- **Sidebar:** 16 columns wide, holding Dashboard, Runners, History and Settings, with Help and Quit at the bottom. The active page gets an accent bar plus a `▌` glyph.
- **Content area:** each page starts with a header line: the breadcrumb title on the left (for example `Runners › aaaaaa`) and that page's buttons on the right.
- **Footer:** a short, clickable list of the keys that apply to the focused page and control.

**Narrow layout (under 100 columns):**
- The sidebar is replaced by a single row of clickable page tabs under the top bar.
- The top-bar chips shed width in this order until the bar fits: the connection chip shrinks to its dot, then the API gauge bar is dropped (the number stays), then the disk gauge bar is dropped (the percentage stays), then the runners gauge bar is dropped.

**Height.** The minimum supported height is 22 rows. When rows run short, these collapse first, in order:
1. the Dashboard's Activity card (down to 3 lines);
2. the stat tiles, to a single line;
3. the Runners-page log preview (down to 3 lines).

**Toasts.** There is at most one toast, on the toast line: `✔ Settings saved` or `✖ <error>`, with a clickable `✕` at the right. A success clears after 4 seconds and an error after 10. A newer toast replaces the current one, and its text is truncated to the line width. Timing uses the existing one-second tick and the model's injectable `now`.

**Focus.**
- `tab` and `shift+tab` move focus between the focusable elements of the current page (table cards, buttons, form controls, tab strips). This replaces `tab` as the page switcher.
- The focused element is marked with a `›` glyph and accent styling. Snapshot tests run without colour, so the glyph is what they assert.

**Keys.** Precedence runs from the top down:
1. **An open modal** gets every key. `enter` triggers the focused button (focus opens on the primary) and `esc` triggers cancel (in the unsaved-changes dialog, cancel means Stay).
2. **An open dropdown:** `↑`/`↓` move, `enter` picks, `esc` closes. Every other key is ignored while it is open.
3. **A text field or tag input that is being edited** gets every printable key. `enter` commits the value and moves focus to the next control. `esc` stops editing and keeps the typed value. `tab` stops editing and moves focus.
4. **A focused control's own keys:**
   - A stepper takes `←`/`→`, `+`/`=`/`-`, digits and `backspace`. While a stepper has focus, digits therefore don't switch pages, and Help says so.
   - A closed select takes `←`/`→` to cycle and `enter`/`space` to open.
   - A toggle or button takes `enter`/`space`.
5. **Page keys:**
   - `↑`/`↓` and `j`/`k`: on list pages they move the focused table's selection. On Settings they move focus to the previous or next control.
   - `pgup`/`pgdn` scroll the page's scrollable region.
   - On the Dashboard, `←`/`h` and `→` focus the Repositories and Runners cards (today's behaviour).
   - On the runner detail page, `←`/`→` switch between the Steps, Log and Containers tabs.
   - `ctrl+s` saves on Settings.
6. **Global keys:** `1`–`4`, `p`, `P`, `x`, `l`, `m`, `[`/`]`, `+`/`=`/`-`, `a`, `d`, `r`, `c`, `?`, `q`, `ctrl+c`, `enter`, each keeping today's meaning on its page.
   - On the Settings page, `m`, `[`/`]` and `+`/`=`/`-` are inactive, because config edits there go through the form.

**`esc` backs out one level at a time:** stop editing a text input → close the dropdown → close the modal → leave the detail page. On other pages it does nothing.

**`ctrl+c`** quits immediately, even with unsaved Settings changes. It is the terminal's emergency exit. `q` is guarded (see §2).

## 2. Controls and the Settings page

### Controls (`internal/tui/ui`)

Every control implements one interface:

```go
type Control interface {
    Update(msg tea.Msg) (Control, tea.Cmd)
    View(focused bool, width int) string
    Focusable() bool // false when disabled
}
```

- Each control has a unique ID per frame, `<page>/<field>` (for example `settings/mode` or `settings/repo/darkmem/max`). Its clickable parts are marked with zone IDs derived from it (`settings/mode/opt-1`).
- A zone ID must never repeat within a frame, because a duplicate corrupts both regions.

| Control | View | Keys | Mouse |
|---|---|---|---|
| Select | `[ queue ▾ ]`. When open, the options render inline below it and push content down | See Keys in §1 | Click opens; clicking an option picks it; a click anywhere else closes the dropdown and is swallowed |
| Stepper | `[ − ] 2 [ + ]` | See Keys in §1 | Click − or + |
| Text field | `[ 5m        ]`, with an inline error line underneath | Editing starts on focus with any printable key, or on `enter` | Click focuses and starts editing |
| Tag list | `tag ✕  tag ✕  + add` | `+ add` opens an inline input; `enter` adds; `backspace` on an empty input removes the last tag | Click ✕ removes; click `+ add` |
| Toggle | `[●━] On` / `[━○] Off` | `space` or `enter` | Click |
| Button | primary `[ Save changes ]`, secondary `( Discard )`, danger `[ Remove ]` | `enter` or `space` | Click |

**Bounds and checks:**
- Global max runs 1–99.
- Disk high-water runs 1–100 in steps of 5, though typed values aren't restricted to multiples of 5.
- Repo max and warm run 0–99.
- Duration fields are checked with `config.ParseDuration` as you type.
- `warm ≤ max` is checked in the app when max is set explicitly above 0.
- Size, memory and CPU fields are checked by the daemon only.
- A disabled control is dimmed and can't take focus. If the focused control becomes disabled, focus moves to the next enabled one.

### Settings page

A single scrollable form, laid out as cards in this order. Each field has a one-line description. Under 100 columns the description goes on its own line below the control.

1. **General:**
   - Mode: a select between `queue` ("start runners only for queued jobs, up to the global max") and `all` ("keep warm runners per repo, up to each repo's max").
   - Global max: a stepper, described as "applies in queue mode".
   - Owner: read-only text, "change in config.yaml and restart the daemon".
2. **Timing:** Poll interval, Start timeout and Idle timeout, all duration fields.
3. **Disk and retention:** Disk high-water (a stepper, %), Build cache keep (text, for example `20GB`) and History retention (a duration).
4. **Runner defaults:** Global labels (a tag list), Memory max (text, for example `6G`) and CPU quota (text, for example `200%`). The limits carry the note "applies to newly started runners".
5. **Repositories:** one card per configured repo, holding:
   - Max (a stepper). When the repo's `max` isn't set, it shows the effective value with a `(default)` suffix: 1 in queue mode, ∞ in all mode.
   - Warm (a stepper, "applies in all mode"). It also shows `(default)` when unset.
   - Labels (a tag list).
   - Cleanup prefixes (a tag list).
   - `[ Pause ]` or `[ Resume ]`, and `[ Remove ]`.

   An untouched default sends nothing. Once a value is set, it stays explicit: `RepoPatch` cannot clear it back to unset, and the description says so. A repo that is being removed shows "removing…", and its controls are disabled. An `[ + Add repository ]` button sits below the repo cards.

**Scrolling.** The page scrolls with the wheel and `pgup`/`pgdn`. The form renderer returns the line range of every control along with the rendered page. Moving focus to a control scrolls it into view using that range, not bubblezone positions, which describe the previous frame.

### Form state and saving

- **Base and edit.** For each field, the form keeps a **base** (the value as last loaded) and an **edit**.
  - A field is dirty when its edit differs from its base.
  - Durations compare by parsed value, so `120s` equals `2m0s`. Lists compare element by element.
  - A dirty field shows a `●` glyph after its control.
- **Unsaved-changes bar.** While anything is dirty, a sticky bar at the bottom of the page shows `● N unsaved changes  ( Discard )  [ Save changes ]`. It is hidden otherwise.
- **Save** (the button or `ctrl+s`):
  1. Runs the in-app checks.
  2. Disables itself and shows `Saving…`.
  3. Sends one `PatchConfig` containing only the dirty fields of repos that still exist and are not being removed.

  The daemon validates the whole config and writes it, or rejects the patch with nothing written.
  - **On success:** a `Settings saved` toast, then the config is re-fetched. Every field that was in the patch resets its edit to the new base. This is a reset, not a merge. If a refetched value differs from the value that was sent (compared as above), a warning toast names the field: "daemon did not apply <field>; is it older than this ghr?".
  - **On rejection:** the edits stay. An alert at the top of the form lists the daemon's messages (split on `"; "`), and an error toast is shown.
- **Discard** resets every edit to its base.
- **Config refresh.** The app re-fetches the config every 5 ticks on every page, plus right away when the daemon's epoch changes (it restarted) and after every action. When a new config arrives:
  - Every non-dirty field takes the new value, and dirty fields keep their edits.
  - A field whose edit now equals the new base stops being dirty.
  - A repo that no longer exists drops its card and its edits, and a toast says so.
  - A newly added repo appears with its base and edit both set to the new values.
  - A repo now being removed loses its edits, and its controls are disabled.
- **Leave guard.** Leaving Settings with unsaved changes (by the sidebar or tab row, `1`–`4`, or `q`) opens a modal with **Save**, **Discard** and **Stay**. `esc` means Stay. The model remembers where you were going, a page or quit.
  - Save navigates or quits only after the save succeeds. On rejection you stay on Settings and see the alert.
  - `ctrl+c` is not guarded.
- **While the daemon is unreachable,** every Settings control and Save are disabled, and the header shows "reconnecting". Edits are kept. On reconnect, the config refresh rules apply.
- **Actions, not settings.** Pause, Resume, Remove and Add repository are immediate actions, not form fields, and work as they do today. Remove asks for confirmation.

**Add repository dialog.** It holds:
- Name (a text field, required).
- Max (a stepper showing `(default)` until touched; untouched sends no `max`).
- Labels (a tag list).
- "Allow public repository" (a toggle), with the warning "self-hosted runners on a public repo can run anyone's code".

It sends `AddRepo`. The daemon's errors (the PAT can't see the repo, the repo is public, the repo is a duplicate) appear inside the dialog, which stays open. The Add button is disabled while the request is in flight.

## 3. Pages

**Dashboard:**
- **Stat tiles:** four tiles, all computed from `/status` plus the config for the disk threshold:
  - Running: non-cleaning instances against the global max, or against ∞ in all mode.
  - Queued jobs: the sum over repos.
  - Repositories: active and paused counts.
  - Disk: amber at or above `disk_high_water`, red at 95%. If no config has loaded yet, 80% is used as the threshold.

  The tiles collapse to a single line under 100 columns or when height runs short.
- **Repositories card:** today's columns. The header holds `[ + Add ]` and `[ Pause all ]`, which becomes `[ Resume all ]` when every repo not being removed is paused. Repos being removed stay paused, so they are left out of that test. The selected row shows `[ Pause ]` (or `[ Resume ]`) and `[ Remove ]` on the right.
- **Runners card:** today's columns. The selected row shows `[ Logs ]` and `[ Stop ]`, and stopping a busy runner asks for confirmation. A double-click or `enter` opens the runner's detail page.
- **Activity card:** today's event feed, with the scroll wheel.

**Runners page.** A full-width runners table, including the "⧗ waiting (repo cap)" rows, with the same row buttons as the Dashboard. Below it, a log preview follows the selected runner, as today's log pane does. It scrolls with the wheel and `pgup`/`pgdn`.

**Runner detail page:**
- **Header:** `Runners › <id>`, with `[ Copy run URL ]` (shown when the URL is known) and `[ Stop runner ]` on the right.
- **Summary line:** the state badge, repo, job name and run number, elapsed time and start time.
- **Tabs:** a focusable tab strip with Steps, Log and Containers. You switch with a click, or `←`/`→` on this page.
  - **Steps:** today's step list. It polls every 5 ticks while the runner exists.
  - **Log:** follows the log through today's cursor logic, with the same 256 KiB cap. It scrolls with the wheel and `pgup`/`pgdn`.
  - **Containers:** today's container list. It polls every 5 ticks while the runner exists.
- **Opening:** `l` on any runner row opens this page on the Log tab. `enter` or a double-click opens it on Steps.
- **Log polling:** today it is gated on the Runners tab (`model.go:245,303`). It becomes driven by whichever view shows a log, either the Runners preview or the detail Log tab. Only one log is followed at a time.
- **Snapshot:** the page keeps its own copy of the runner's last `InstanceStatus`, refreshed on every status update.
- **When the runner disappears from `/status`:**
  - The snapshot stays and the elapsed time freezes.
  - An alert "This runner has finished" appears with `[ Back to runners ]`.
  - Steps and Containers polling stop. The Log tab keeps its cursor and keeps polling while it is open, because the daemon serves archived logs.

**History page.**
- A filter bar holds `Repo [ all ▾ ]` (all, then each configured repo) and `Result [ all ▾ ]` (all, success, failure, cancelled). `r` and `c` still cycle the filters.
- The table has today's columns. The selected row shows `[ Copy run URL ]`, which `enter` also triggers.
- Polling stays at today's slow rate.

**Dialogs.** Confirm, Add repository, unsaved changes and Help share one modal style: a bold title, the body, and buttons aligned right. Like today's `withOverlay`, the modal is placed on a blank canvas the size of the screen; the base view is not drawn behind it. Help lists the keys grouped by Global, Dashboard, Runners, Detail, History and Settings, notes that a focused stepper takes digits, keeps today's notes on the mouse and tmux, and has a Close button.

## 4. Daemon API

`model.ConfigPatch` gains these fields. All are optional pointers with `omitempty`.

| JSON | Go | Applied to |
|---|---|---|
| `poll_interval` | `*string` | `PollInterval` |
| `history_retention` | `*string` | `HistoryRetention` |
| `disk_high_water` | `*int` | `DiskHighWater` |
| `build_cache_keep` | `*string` | `BuildCacheKeep` |
| `labels` | `*[]string` | `Labels` |
| `runner_limits` | `*RunnerLimitsPatch{MemoryMax, CPUQuota *string}` | `RunnerLimits` |

- **Applying.** `Backend.PatchConfig` applies these inside the same `update` closure as today's fields. The two durations join the existing duration loop. That loop now prefixes a parse error with the field name, for example `poll_interval: invalid duration "x"`, which also applies to the existing start and idle timeouts.
- **Rejection.** An unparseable duration is rejected inside the closure. Out-of-range or malformed values are rejected by the existing `Config.Validate` through `Store.Update`, which clones, applies, validates, saves and swaps. Either way the client gets a 400 and nothing is written.
- **Compatibility.**
  - Older clients never send the new fields, and `decode` ignores what is absent.
  - A new TUI talking to an older daemon is caught by the save-time mismatch check in §2.
  - There's no change to the CLI (`ghr set`).
- **Labels.** Changing the global labels already marks the affected runners stale, through `stale` → `CustomLabels` (`internal/runner/manager.go:209-218`), so they're replaced just as they are after a repo label change.

## 5. Code structure

- **`internal/tui/ui/`:**
  - `control.go`: the `Control` interface and the ID helpers.
  - `theme.go`: the colour tokens and styles, moved out of `styles.go`.
  - One file per control: `select.go`, `stepper.go`, `textfield.go`, `taglist.go`, `toggle.go`, `button.go`.
  - `form.go`: base, edit and dirty state; comparison by field type; layout with per-control line ranges; scroll-into-view.
  - `toast.go`: the single toast with expiry.
  - A test file for each.
- **`internal/tui/`:**
  - `model.go`: polling and message handling. It gains the current page, focus, the detail snapshot, form state and the pending navigation target. It loses the per-tab view and input code.
  - `shell.go`: the top bar, alert and toast lines, sidebar or tab row, and footer.
  - `dashboard.go`, `runners.go`, `detail.go`, `history.go` and `settings.go`: one file per page.
  - `dialogs.go`: the modals.
  - `input.go`: key precedence and mouse dispatch.
  - `styles.go`: reduced to the table helpers (`cell`, `box`, `gauge`).
- **`cmd/ghr/main.go`:** `runTUI`, `isTerminal`, the bare-`ghr` launch and the usage text.
- **`internal/model/model.go`** and **`internal/daemon/backend.go`:** the patch fields and the duration error prefix from §4.

## 6. Error handling

- Daemon unreachable or degraded: the alert line, as today. Settings is disabled as described in §2.
- Action failures: an error toast. A rejected Settings save keeps the edits and shows an inline alert. Add repository errors stay in its dialog.
- All text that comes from the daemon or GitHub still passes through `clean()` before it's rendered.

## 7. Testing

The tests use the existing harness: `charmbracelet/x/exp/golden` snapshots with direct `Update`/`View` calls under `termenv.Ascii`, and mouse tests through render → `zone.Scan` → `waitZone`. teatest is not used. The `ui` package tests need a zone manager and the same Scan-and-wait step.

- **`ui` unit tests:**
  - Select: open, move, pick, cycle, close, and a swallowed outside click.
  - Stepper: bounds, typed digits, `=` as `+`, and `(default)` until touched.
  - Text field: editing starts and ends, `enter` advances focus, and the inline duration error.
  - Tag list: add, remove with ✕, and backspace on an empty input.
  - Toggle: flips.
  - Form: the dirty count; a patch that holds only dirty fields; durations compared by value; Discard; and the refresh merge, including a repo removed, a repo added and a repo now being removed.
  - Scroll into view.
  - Toast: expiry at 4 s and 10 s, replacement, and ✕.
- **TUI tests:**
  - Golden snapshots of each page (Dashboard, Runners, detail, History, Settings, Settings with a dropdown open, a Settings dialog) at 120 and 80 columns. These replace today's `TestDashboardGolden`.
  - Mouse: sidebar items, narrow tab-row items, row buttons, double-click into the detail page, select options, detail tabs and the wheel.
  - Keys: precedence (a focused stepper takes digits, a text field takes letters), `tab` focus order, `esc` backing out from an edited text field to the detail page, and `ctrl+s`.
  - Save: a saved `120s` is not dirty after the refetch. A rejected save keeps the edits and shows the alert. Save is disabled while in flight. The mismatch warning fires when the daemon ignores a field.
  - Leave guard: through `q`, a sidebar click, and `1`–`4`. Save-then-navigate succeeds, save-then-stay happens on rejection, and `esc` means Stay. `ctrl+c` quits unguarded.
  - The detail page after its runner vanishes.
  - Settings disabled while the daemon is unreachable.
  - "Resume all" ignores repos being removed.
- **Daemon tests:**
  - Each new patch field is applied and persisted.
  - Invalid values (`disk_high_water: 0`, a bad size, a bad CPU quota, a bad memory value, an unparseable duration) return 400 and leave `config.yaml` unchanged.
  - Duration errors name their field.
- **Command tests:** bare `ghr` with `isTerminal` true calls `runTUI`, and bare `ghr` with it false prints usage and exits 2.
- **On the LXC:** install the release and drive it in tmux with injected keys and mouse events, covering every page the plan delivers: a Settings save, a rejected save, and Add repository against `ghr-e2e`. The owner's check from a real SSH terminal stays in register rows 4–5 of 2026-10-03-ghr-runners.md.

## 8. Delivery

There are three plans. Each is merged and released on its own, and each covers its own part of §7.

1. **Daemon settings fields and bare `ghr`:** §4 plus the launch part of §1. This delivers register item 1 and gives plan 2 an API to call.
2. **Widgets, shell and Settings:** the `ui` package, `shell.go`, `settings.go`, the shared modal, the leave guard, and the toast. The other pages move into the new shell with their current content. This delivers register item 2 and the first half of item 3.
3. **The remaining pages:** Dashboard tiles and row buttons, the Runners page with its log preview, the detail page, History filters, and Help. This completes register item 3.

Each plan's work happens on a feature branch in the ghr repo. Every push to ghr's master publishes a release, so a branch merges only after its gates pass and the owner says yes. Then it is deployed with `GHR_VERSION=<tag>` pinned.

## Out of scope

- Editing `owner`.
- Clearing a repo's explicit max or warm back to unset.
- CLI `ghr set` for the new settings.
- History beyond today's 200-row limit.
- Theme switching.
- The deferred v2 visuals (sparklines, elapsed bars).
- Hover effects: mouse tracking reports motion only while a button is held.
