# ghr runner update warning, queued update and repository picker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Warn when the LXC's GitHub Actions runner falls behind GitHub's latest release, let the owner queue an update that ghr installs by itself once no job is running or queued, and replace the free-text name in Add repository with a picker of the owner's repositories.

**Architecture:** The GitHub client lists actions/runner releases and the token's repositories. The runner manager checks releases at start and daily, warns once per version, keeps a queued update in `runner-update.json`, and, on a tick where nothing is starting, busy or queued, installs the latest runner in Go (download, checksum, extract, dependency script, switch `dist/current`, remove old versions) behind fakeable host seams. The daemon serves the state in `/status` and adds `POST`/`DELETE /runner-update` and `GET /repos/available`; the CLI gains `ghr runner-update` and a status line, and the TUI a Runner line with queue and cancel buttons, a top-bar badge, and a new `ui.Picker` in the Add repository dialog.

**Tech Stack:** Go 1.26 (go.mod), Bubble Tea v1, lipgloss v1, bubblezone v1; daemon HTTP over a Unix socket; GitHub REST API 2022-11-28.

**Spec:** docs/superpowers/specs/2026-10-05-ghr-runner-update-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 6 of 14 tasks are heavy (not more than half), so the session implements the rest and delegates the heavy ones; effort is high because tasks are delegated.

**Plan review:** 2026-10-05 — dr-superpowers:judge-opus — executability 15 / coherence 17 / coverage 18 / assumptions 15 (round 2)

## Global Constraints

- Code repository: `D:/Repositories/Personal/ghr` (github.com/darkraise/ghr), branch `feat/runner-update` from master `fd39978`. The plan, spec, register and `github-runner/setup.sh` (Task 13) live in `D:/Repositories/Personal/homelab`.
- Every command runs from the ghr repository root in Git Bash with an explicit `timeout`; Task 13 works in the homelab repository and names its paths absolutely. Unit tests: `timeout 400 go test ./...`. CI also runs `gofmt -l .` (must print nothing; run it here as `timeout 120 gofmt -l .`), `go vet ./...` (`timeout 300 go vet ./...`) and `go test -race -count=1 -timeout 180s ./...`; `-race` cannot run on this Windows box (no cgo), so CI is its only run.
- Windows lets only privileged users create symlinks, so `TestSwitchLinkReplacesTheLink` skips here and CI (ubuntu) is its run of record; every other test runs here.
- Edit Go files with the Edit or Write tools. Bash heredocs and inline `python -` lose backslashes (`\n` becomes a newline).
- TUI tests set `lipgloss.SetColorProfile(termenv.Ascii)` in `TestMain` (`internal/tui/tui_test.go:24`), so badges render as `[TEXT]`; goldens live under `internal/tui/testdata` and are regenerated with `timeout 300 go test ./internal/tui -run <Test> -update`. Read every golden diff before committing.
- All daemon and GitHub text shown in the TUI passes through `clean()` (`internal/tui/view.go:73`); the daemon serves raw text.
- Comments: none unless the why is non-obvious; never reference this plan, a task or a review.
- Commits: `<type>(<scope>): <subject>`, subject ≤ 50 characters, imperative, no period. One commit per task unless a step says otherwise.
- Runner update numbers (spec §1): release check at daemon start, then every 24 hours, only while the API is allowed and not degraded; a failed check retries after 1 hour; the deadline is 30 days after the `published_at` of the earliest-published release newer than the installed version; one update is bounded by 10 minutes; the TUI calls a deadline 7 days away or past urgent; the picker shows 8 rows.
- On-disk layout (unchanged from `setup.sh`): `/opt/ghr/dist/<version>`, staging `/opt/ghr/dist/<version>.tmp`, symlink `/opt/ghr/dist/current`; state file `/var/lib/ghr/runner-update.json` holding `queued_at` and `warned_version`, written via temp file and rename. Download URL: `https://github.com/actions/runner/releases/download/v<ver>/actions-runner-linux-x64-<ver>.tar.gz`, sent without credentials.
- Event texts, verbatim: `warn` "runner 2.338.0 is available (installed 2.337.0); update by 2026-11-04"; `info` "runner update queued"; `info` "runner update cancelled"; `info` "runner update started"; `info` "runner already up to date (2.338.0)"; `ok` "runner updated to 2.338.0"; `error` "runner update failed: <step>: <reason>" (steps `check`, `checksum`, `download`, `extract`, `dependencies`, `install`, `switch`); `warn` "runner update interrupted by shutdown".
- HTTP answers: `POST /runner-update` 202 (queued or already queued), 409 up to date / running / no `dist/current`, 503 degraded or shutting down, GitHub errors mapped by `api.FromGitHub` (429 with `retry_at`); `DELETE /runner-update` 204 (also when nothing was queued), 409 while running; `GET /repos/available` 200 with a JSON list (`[]` when empty), 503 while degraded.

## Contracts

**C1 `internal/github` (Task 1):**
```go
type Release struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Body        string    `json:"body"`
}
func (c *Client) ListRunnerReleases(ctx context.Context) ([]Release, error) // GET /repos/actions/runner/releases?per_page=30; drafts and prereleases dropped; never nil on success
func (r Release) LinuxX64SHA256() (string, bool)                            // the <!-- BEGIN SHA linux-x64 --> marker setup.sh reads
type Version []int
func ParseVersion(s string) (Version, bool) // "2.338.0" or "v2.338.0"
func (v Version) Compare(o Version) int     // part by part, a missing part is 0
func (v Version) String() string
type Account struct{ Login string `json:"login"` }
type UserRepo struct {
	Name    string  `json:"name"`
	Private bool    `json:"private"`
	Owner   Account `json:"owner"`
}
func (c *Client) ListUserRepos(ctx context.Context) ([]UserRepo, error) // GET /user/repos?per_page=100, following Link
```

**C2 `internal/system` (Task 2):**
```go
func (h Host) Extract(ctx context.Context, tarball, dir string) error  // tar -xzf tarball -C dir
type Host struct {
	Run    Runner
	Script Runner // runs scripts that start children of their own; nil uses Run
}
func ExecGroup(ctx context.Context, name string, args ...string) ([]byte, error) // Exec in a process group of its own, killed whole on cancel (Unix)
func (h Host) RunScript(ctx context.Context, dir, path string) error   // runs filepath.Join(dir, path) with Script (or Run)
func (Host) ReadLink(path string) (string, error)                      // filepath.EvalSymlinks
func (Host) SwitchLink(target, path string) error                      // symlink path+".tmp" -> target, renamed over path
var DownloadTimeout = 10 * time.Minute
func Download(ctx context.Context, url, dst string) error              // no credentials; a non-200 answer is an error
```

**C3 `internal/runner` seams (Task 3):** `runner.GitHub` gains `ListRunnerReleases(ctx context.Context) ([]github.Release, error)`; `runner.Host` gains `Extract(ctx context.Context, tarball, dir string) error`, `RunScript(ctx context.Context, dir, path string) error`, `ReadLink(path string) (string, error)`, `SwitchLink(target, path string) error`; `Paths` gains `UpdateState string`; `Manager` gains `Fetch func(ctx context.Context, url, dst string) error`; `spawn` resolves `Paths.Dist` with `m.Host.ReadLink`. Test helpers (package `runner`): `(*fakeGH).setReleases(rels ...github.Release)`, `(*fakeHost).setErr(method string, err error)`, `(*fakeHost).hostCalls() string` (`"Extract 2.338.0.tmp,RunScript 2.338.0.tmp"`), `(*harness).linkDist(t, ver string) string` (returns the dist dir; `Paths.Dist` becomes `<dist>/current`, a fake link file). Task 4 adds `(*fakeGH).callsText() string` and `newEvents(h *harness) *events.Ring`.

**C4 `internal/model` (Tasks 4 and 8):**
```go
// Status gains: RunnerUpdate RunnerUpdate `json:"runner_update"`
type RunnerUpdate struct {
	Installed       string     `json:"installed,omitempty"`
	Latest          string     `json:"latest,omitempty"`
	LatestPublished *time.Time `json:"latest_published,omitempty"`
	Deadline        *time.Time `json:"deadline,omitempty"` // set only while a newer runner than Installed exists
	CheckedAt       *time.Time `json:"checked_at,omitempty"`
	CheckError      string     `json:"check_error,omitempty"`
	Queued          bool       `json:"queued,omitempty"`
	QueuedAt        *time.Time `json:"queued_at,omitempty"`
	Running         bool       `json:"running,omitempty"`
	LastOutcome     string     `json:"last_outcome,omitempty"` // "ok", "failed", "current"
	LastError       string     `json:"last_error,omitempty"`
	LastFinished    *time.Time `json:"last_finished,omitempty"`
}
type AvailableRepo struct { // Task 8
	Name       string `json:"name"`
	Private    bool   `json:"private"`
	Configured bool   `json:"configured"`
}
```

**C5 `internal/runner` update (Tasks 4 to 6):** exported:

```go
var ErrUpdateRunning = errors.New("a runner update is running")
var ErrNoDist = errors.New("runner version unknown (no dist/current): run setup.sh first")
type UpToDateError string // Error() is "runner <version> is already up to date"
func (m *Manager) QueueUpdate(ctx context.Context) error
func (m *Manager) CancelUpdate() error
// StartPrune returns ErrUpdateRunning while an update holds the maintenance reservation (Task 6).
```

Unexported, in `internal/runner/update.go` unless noted:

```go
type updateFile struct { // runner-update.json
	QueuedAt      *time.Time `json:"queued_at,omitempty"`
	WarnedVersion string     `json:"warned_version,omitempty"`
}
type updateState struct { // Manager.upd, guarded by mu; only storeUpdateFile changes file
	file         updateFile
	installed    string // "" when dist/current does not resolve
	latest       string
	latestPub    time.Time
	deadline     time.Time // zero unless a release newer than installed exists
	checkedAt    time.Time
	checkErr     string
	nextCheck    time.Time
	running      bool
	lastOutcome  string
	lastErr      string
	lastFinished time.Time
	gen          int // bumped when an install switches dist/current
}
type releaseFacts struct {
	latest   github.Release
	version  github.Version
	deadline time.Time
}
func factsFor(installed string, rels []github.Release) (releaseFacts, bool)
func (m *Manager) installedVersion() string
func (m *Manager) loadUpdate()                        // starts m.upd afresh from dist/current and the state file
func (m *Manager) storeUpdateFile(f updateFile) error // requires fileMu; writes the file, then m.upd.file
func (m *Manager) releaseCheckDue(now time.Time) bool
func (m *Manager) checkRelease(ctx context.Context) ([]github.Release, error) // drops its result when upd.gen moved meanwhile
func (m *Manager) warnUpdate(installed, latest string, deadline time.Time)
func (m *Manager) runnerUpdate() model.RunnerUpdate // requires mu
// Manager fields: upd updateState (guarded by mu), fileMu sync.Mutex (taken before mu).
```

Task 5 adds `internal/runner/install.go`: `stepError{step string; err error}`, `complete(dir string) bool`, `installLatest(ctx) (version, outcome string, err error)` (checks `ctx` before the switch; after it, publishes `installed`, `deadline` and `gen+1`), `stage`, `var removeAll = os.RemoveAll`, `removeOldVersions` (a failure is a warning), `clearQueue()` (removes the state file when it cannot rewrite it), `runUpdate(ctx)`. Task 6 adds `const updateTimeout = 10 * time.Minute`, `startUpdateIfFree(ctx context.Context, cfg *config.Config, now time.Time, demandOK bool)`, and in `lifecycle.go` `type idleStop int` (`idleStopped`, `idleBusy`, `idleKept`) returned by `stopIdleRunner(ctx, i instance, now) idleStop`; `gatherDemand` returns `bool` (every unpaused repo answered).

Test helpers: `release(ver string, days int, sum string) github.Release` and `var published` (Task 4, `update_test.go`); `fakeGH.hook func(method string)` (runs during every call, after it is recorded) and `(*harness).restart(t)` (a fresh Manager on the same fakes and paths, after `Init`) (Task 4, `fakes_test.go`); `fakeHost.onScript func()` (runs inside `RunScript`), `tarball`, `tarballSum()`, `queuedUpdate(t, h, sum) (dist string, fetched *[]string)`, `current(t, h) string`, `distEntries(t, dist) string` (Task 5).

**C6 `internal/daemon` and `internal/api` (Tasks 7 and 8):** routes `POST /runner-update`, `DELETE /runner-update`, `GET /repos/available`. `api.Backend` gains `QueueRunnerUpdate(ctx context.Context) error`, `CancelRunnerUpdate() error` (Task 7), `AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error)` (Task 8). `api.Client` gains `QueueRunnerUpdate(ctx context.Context) error`, `CancelRunnerUpdate(ctx context.Context) error` (Task 7), `AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error)` (Task 8). `daemon.Manager` gains `QueueUpdate(ctx context.Context) error` and `CancelUpdate() error`; `daemon.GitHub` gains `ListUserRepos(ctx context.Context) ([]github.UserRepo, error)` (Task 8). `daemon.Options` gains `Fetch func(ctx context.Context, url, dst string) error` (nil means `system.Download`); `DefaultOptions().Paths.UpdateState` is `/var/lib/ghr/runner-update.json`. Test fakes: `fakeManager.updateErr`, `fakeManager.updates`; `fakeGH.userRepos`, `fakeGH.repoErr` (daemon); `fakeBackend.updates`, `fakeBackend.updateErr`, `fakeBackend.avail`, `fakeBackend.availErr` (api).

**C7 `internal/tui` (Tasks 10 and 12):** `tui.Client` gains `QueueRunnerUpdate(ctx context.Context) error`, `CancelRunnerUpdate(ctx context.Context) error`, `AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error)`; `fakeClient` gains `updateErr error`, `avail []model.AvailableRepo`, `availErr error`, `availCalls int` and records `"runner-update"` / `"runner-update cancel"`. Button IDs `setRunnerQueue = "settings/runner/queue"`, `setRunnerCancel = "settings/runner/cancel"`; `manageState` gains `rnQueue`, `rnCancel *ui.Button`; `func (m Model) updateUrgent() bool`, `func (m Model) runnerRows() []ui.Row`, `func (m Model) runnerBadge() string`. Task 12: dialog IDs `addRepo = "add/repo"` (replaces `addName`), `addRetry = "add/retry"`; `const pickerRows = 8`; `type availMsg struct{ d *addRepoDialog; repos []model.AvailableRepo; err error }`; `func (m Model) fetchAvailable(d *addRepoDialog) tea.Cmd`, `gotAvailable(msg availMsg) (tea.Model, tea.Cmd)`, `retryAvailable() (tea.Model, tea.Cmd)`. Test helpers: `at(d time.Duration) *time.Time`, `available(days int) model.RunnerUpdate`, `withRunner(u model.RunnerUpdate) model.Status` (Task 10, `runner_update_test.go`).

**C8 `internal/tui/ui` (Task 11):**
```go
type PickOption struct {
	Label    string // what the filter matches and a pick returns
	Badge    string // shown after the label
	Disabled bool   // shown dim, with Note, and never picked
	Note     string
}
type Picker struct{ Options []PickOption; Rows int; Disabled bool /* unexported state */ }
func NewPicker(id string, rows int) *Picker
func (p *Picker) SetOptions(opts []PickOption) // keeps the pick while its label is offered and enabled
func (p *Picker) Picked() (PickOption, bool)
func (p *Picker) Filter() string
// Picker is a ui.Widget; it takes printable keys, backspace, up, down and enter; row zones are "<id>/row-<option index>".
// Rows may change between renders: the window moves back over the highlight.
```

## Assumptions (evidence)

- The LXC's `/opt/ghr/dist/current` resolves to `/opt/ghr/dist/2.337.0`, the only version there, under ghr v0.1.8; GitHub's latest actions/runner release is v2.337.0 (published 2026-08-26), and the five newest releases all carry the linux-x64 checksum marker (`ssh … readlink -f`, `gh api repos/actions/runner/releases`, both 2026-10-05).
- On 2026-10-05 the LXC token's `/user/repos` listed 71 repositories across two owners, 32 public, on one page (spec, Decisions).
- An unprivileged Windows user cannot create symlinks: `os.Symlink` fails with "A required privilege is not held by the client" (go run, 2026-10-05). So the runner tests' fake host keeps links as regular files holding the target path, and the real `SwitchLink` test skips here; it passed on Linux (test binary cross-compiled and run in WSL, 2026-10-05).
- `system.Host` already has a field named `Run` (`internal/system/system.go:297`), so the script method is `RunScript`, as the amended spec §1 "Host operations" names it.
- `setup.sh`'s `fetch_runner`, `install_runner` and `gc_dist` (homelab `github-runner/setup.sh:77-107,188-199`) are the steps ported; `current` is an absolute symlink renamed into place (`ln -sfn` then `mv -Tf`), the same atomic switch `SwitchLink` does.
- Owner ruling, 2026-10-05 (spec amended): only jobs ghr can serve block the update. "No queued job" means none matching ghr's labels in any unpaused configured repo, and every unpaused repo must answer this tick's poll. Paused repos are not polled (`internal/runner/tick.go:74-76`) and get no runners; demand holds only queued jobs matching ghr's labels (`tick.go:136`). The update also waits while the API is not allowed or ghr is degraded, and when the config changed during the poll.
- Owner ruling, 2026-10-05 (spec amended): `setup.sh` stops `ghr.service` before installing a runner, because the daemon installs queued updates into the same `dist` directory (Task 13). Runner units outlive the daemon, and `install_service` starts it again.
- A failed release check posts no runner event; a rate limit or auth failure is handled like any other GitHub call (`apiErr` posts its own event and pauses API use). `TestReleaseCheckRateLimitPostsNoRunnerEvent` pins this.
- Removing old versions runs after `current` switched, so a failure there is a `warn` event, not a failed update; the next update removes them.
- An update's idle runners are stopped one by one; one that took a job, or whose check or stop failed, keeps the update waiting for a later tick (`TestUpdateWaitsWhenAnIdleRunnerDoesNotStop`). This includes an idle runner whose GitHub registration is gone (404), as `stopIdle` already treats it; inference: such a runner process exits, and `refreshUnits` (`internal/runner/lifecycle.go:214`) cleans the instance up, so the deferral lasts only until then.
- `startUpdateIfFree` compares the config once, before stopping idle runners; a config change during those stops is not caught and is left to the update, which spawns nothing until it ends (round-2 Minor, accepted).
- New doc comments on unexported names (for example `updateTimeout`, `idleStop`) follow the existing code, which documents every declaration (`internal/runner/manager.go`, `internal/system/system.go`).
- Beyond the spec: while an update holds the shared maintenance reservation, a manual prune answers 409 "a runner update is running" rather than the misleading "a prune is already running".
- The TUI's Runner row is labelled "Runner", so its text drops the leading word "runner" of the spec's sample line; `ui.Row.Lines` wraps and collapses double spaces, so the line renders `2.337.0 → 2.338.0 [UPDATE AVAILABLE]` with the deadline on the next line. `ghr status` keeps the spec's `runner 2.337.0 …` form.
- Adding methods to an interface scores coupling 2, as in the earlier ghr plans. Two existing unexported signatures change: `gatherDemand` returns `bool` and `stopIdleRunner` returns `idleStop` (Task 6); both are package-internal, and Task 6 updates every caller.
- Every code block in Tasks 1 to 12 was built and its tests run on master `fd39978` during planning, on Windows and, for the runner, system, daemon, api, tui and cmd packages, on Linux (2026-10-05), with each mutation check a step names run and seen to fail. Task 13's edits, applied to a copy of homelab's `github-runner`, pass `tests/setup_test.sh`, and its checks fail without them (2026-10-05). Line numbers quoted are master `fd39978`; a task that edits a file an earlier task changed quotes the text it replaces, not a line number.
- `plan-lint` lists Task 14 among the delegated heavy tasks; the controlling session runs it anyway, because its deploy needs your human partner's approval and its first step is the execution skill's own final review.
- The LXC keeps a plain copy of homelab's `github-runner` at `/root/github-runner` (not a git checkout; its `setup.sh` matched homelab's sha256 on 2026-10-05), so Task 14 copies the new `setup.sh` there before running it.

## Task index

1. GitHub runner releases and user repositories
2. Host operations for installing a runner
3. Runner manager seams
4. Release check, warning and queue
5. Runner install steps
6. Start a queued update when ghr is free
7. Runner update endpoints
8. Available repositories endpoint
9. CLI runner-update and status line
10. Runner line in Settings and top-bar badge
11. Picker widget
12. Add repository picker
13. setup.sh stops ghr before installing the runner
14. Release and LXC acceptance

---

### Task 1: GitHub runner releases and user repositories

**Files:**
- Create: `internal/github/releases.go`
- Test: `internal/github/releases_test.go` (new)

**Interfaces:**
- Consumes: the existing `Client.do` and `Client.getAll` (`internal/github/client.go:231,371`) and the test helper `newClient` (`internal/github/client_test.go:24`).
- Produces: C1.

**Items:** 1, 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Create the branch**

```bash
cd /d/Repositories/Personal/ghr && git switch master && git switch -c feat/runner-update
```

Expected: `Switched to a new branch 'feat/runner-update'`. If the branch already exists, `git switch feat/runner-update` instead.

- [ ] **Step 2: Write the failing tests** (`internal/github/releases_test.go`)

```go
package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestListRunnerReleasesSkipsDraftsAndPrereleases(t *testing.T) {
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
			{"tag_name":"v2.339.0","draft":true,"published_at":"2026-10-04T10:00:00Z"},
			{"tag_name":"v2.339.0-rc1","prerelease":true,"published_at":"2026-10-03T10:00:00Z"},
			{"tag_name":"v2.338.0","published_at":"2026-10-02T10:00:00Z","body":"notes"}]`)
	})
	rels, err := c.ListRunnerReleases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].TagName != "v2.338.0" || rels[0].Body != "notes" || rels[0].PublishedAt.Day() != 2 {
		t.Fatalf("releases %+v", rels)
	}
	if got := f.requests[0]; got != "GET /repos/actions/runner/releases?per_page=30" {
		t.Fatalf("request %q", got)
	}
}

func TestParseVersionAndCompare(t *testing.T) {
	for _, s := range []string{"", "v", "2..1", "2.x.0", "2.-1.0", "2.+1.0", " "} {
		if _, ok := ParseVersion(s); ok {
			t.Errorf("%q parsed", s)
		}
	}
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"2.338.0", "v2.338.0", 0},
		{"2.338.0", "2.337.9", 1},
		{"2.9.0", "2.10.0", -1},
		{"2.338", "2.338.0", 0},
		{"2.338.1", "2.338", 1},
	} {
		a, okA := ParseVersion(c.a)
		b, okB := ParseVersion(c.b)
		if !okA || !okB {
			t.Fatalf("%q or %q did not parse", c.a, c.b)
		}
		if got := a.Compare(b); got != c.want {
			t.Errorf("%s vs %s = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if v, _ := ParseVersion("v2.338.0"); v.String() != "2.338.0" {
		t.Fatalf("String %q", v.String())
	}
}

func TestLinuxX64SHA256(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	r := Release{Body: "x\n<!-- BEGIN SHA linux-x64 -->" + sum + "<!-- END SHA linux-x64 -->\ny"}
	if got, ok := r.LinuxX64SHA256(); !ok || got != sum {
		t.Fatalf("got %q %v", got, ok)
	}
	for _, body := range []string{
		"",
		"<!-- BEGIN SHA linux-arm64 -->" + sum + "<!-- END SHA linux-arm64 -->",
		"<!-- BEGIN SHA linux-x64 -->" + strings.ToUpper(sum) + "<!-- END SHA linux-x64 -->",
		"<!-- BEGIN SHA linux-x64 -->abc<!-- END SHA linux-x64 -->",
	} {
		if got, ok := (Release{Body: body}).LinuxX64SHA256(); ok {
			t.Errorf("body %q gave %q", body, got)
		}
	}
}

func TestListUserReposPaginates(t *testing.T) {
	var base string
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/user/repos?per_page=100&page=2>; rel="next"`, base))
			fmt.Fprint(w, `[{"name":"darkcloud","private":true,"owner":{"login":"darkraise"}}]`)
			return
		}
		fmt.Fprint(w, `[{"name":"other","private":false,"owner":{"login":"someone"}}]`)
	})
	base = c.BaseURL
	rs, err := c.ListUserRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0] != (UserRepo{Name: "darkcloud", Private: true, Owner: Account{Login: "darkraise"}}) ||
		rs[1].Owner.Login != "someone" || rs[1].Private {
		t.Fatalf("repos %+v", rs)
	}
	if got := strings.Join(f.requests, "|"); got != "GET /user/repos?per_page=100|GET /user/repos?per_page=100&page=2" {
		t.Fatalf("requests %q", got)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 200 go test ./internal/github/`
Expected: FAIL to build with `undefined: ParseVersion` (and the other new names).

- [ ] **Step 4: Write the implementation** (`internal/github/releases.go`)

```go
package github

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Release is one release of actions/runner.
type Release struct {
	TagName     string    `json:"tag_name"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Body        string    `json:"body"`
}

// ListRunnerReleases lists the 30 newest actions/runner releases, one page,
// without drafts and prereleases.
func (c *Client) ListRunnerReleases(ctx context.Context) ([]Release, error) {
	data, _, err := c.do(ctx, http.MethodGet, c.BaseURL+"/repos/actions/runner/releases?per_page=30", nil)
	if err != nil {
		return nil, err
	}
	var all []Release
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	out := []Release{}
	for _, r := range all {
		if !r.Draft && !r.Prerelease {
			out = append(out, r)
		}
	}
	return out, nil
}

var linuxX64SHA = regexp.MustCompile(`<!-- BEGIN SHA linux-x64 -->([0-9a-f]{64})<!-- END SHA linux-x64 -->`)

// LinuxX64SHA256 is the linux-x64 tarball checksum the release notes carry,
// the marker setup.sh reads too.
func (r Release) LinuxX64SHA256() (string, bool) {
	m := linuxX64SHA.FindStringSubmatch(r.Body)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// Version is a dotted release version such as 2.338.0.
type Version []int

// ParseVersion reads "2.338.0" or "v2.338.0". Every part must be decimal digits.
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return nil, false
	}
	var v Version
	for _, p := range strings.Split(s, ".") {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		v = append(v, n)
	}
	return v, true
}

// Compare orders versions part by part; a missing part counts as 0.
func (v Version) Compare(o Version) int {
	for i := range max(len(v), len(o)) {
		a, b := 0, 0
		if i < len(v) {
			a = v[i]
		}
		if i < len(o) {
			b = o[i]
		}
		if a != b {
			return cmp.Compare(a, b)
		}
	}
	return 0
}

func (v Version) String() string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// Account is a repository owner.
type Account struct {
	Login string `json:"login"`
}

// UserRepo is one repository the token's user can access.
type UserRepo struct {
	Name    string  `json:"name"`
	Private bool    `json:"private"`
	Owner   Account `json:"owner"`
}

// ListUserRepos lists every repository the token can access, across owners.
func (c *Client) ListUserRepos(ctx context.Context) ([]UserRepo, error) {
	var out []UserRepo
	err := c.getAll(ctx, c.BaseURL+"/user/repos?per_page=100", func(b []byte) error {
		var page []UserRepo
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page...)
		return nil
	})
	return out, err
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 200 go test ./internal/github/ && timeout 120 gofmt -l . && timeout 300 go vet ./internal/github/`
Expected: `ok  github.com/darkraise/ghr/internal/github`, and nothing from `gofmt` or `vet`.

- [ ] **Step 6: Commit**

```bash
git add internal/github/releases.go internal/github/releases_test.go
git commit -m "feat(github): list runner releases and user repos"
```

### Task 2: Host operations for installing a runner

**Files:**
- Modify: `internal/system/system.go` (imports at lines 4-14; `Exec` lines 25-43; `Host` line 297; new methods and `Download` after `ChownR`, lines 305-308)
- Create: `internal/system/group_unix.go`, `internal/system/group_other.go`
- Modify: `internal/daemon/run.go:140` (the daemon's host runs scripts with `ExecGroup`)
- Test: `internal/system/host_test.go` (new), `internal/system/group_unix_test.go` (new)

**Interfaces:**
- Consumes: the existing `Host{Run Runner}` and the test helper `fake` (`internal/system/system_test.go:21`).
- Produces: C2.

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

`bin/installdependencies.sh` runs apt, which starts processes of its own. `exec.CommandContext` kills only the command it started, so the script runs under `ExecGroup`: in a process group of its own, which a cancel or timeout kills whole. The group code is Unix-only behind build tags; on Windows, where the tests also run, it does nothing.

- [ ] **Step 1: Write the failing tests** (`internal/system/host_test.go`)

```go
package system

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostExtractAndRunScriptArgs(t *testing.T) {
	run, calls := fake(nil)
	script, scripts := fake(nil)
	h := Host{Run: run, Script: script}
	if err := h.Extract(context.Background(), "/d/2.338.0.tmp/runner.tar.gz", "/d/2.338.0.tmp"); err != nil {
		t.Fatal(err)
	}
	if err := h.RunScript(context.Background(), "/d/2.338.0.tmp", "bin/installdependencies.sh"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0].String() != "tar -xzf /d/2.338.0.tmp/runner.tar.gz -C /d/2.338.0.tmp" {
		t.Fatalf("Run calls %v", *calls)
	}
	want := filepath.Join("/d/2.338.0.tmp", "bin/installdependencies.sh") + " "
	if len(*scripts) != 1 || (*scripts)[0].String() != want {
		t.Fatalf("Script calls %v, want %q", *scripts, want)
	}
	// Without a Script runner the script goes to Run.
	h.Script = nil
	if err := h.RunScript(context.Background(), "/d", "bin/x.sh"); err != nil || len(*calls) != 2 {
		t.Fatalf("fallback: %v %v", err, *calls)
	}
}

func TestHostRunScriptReportsFailure(t *testing.T) {
	run, _ := fake(map[string]string{filepath.Join("/d", "bin/installdependencies.sh"): "ERR:exit status 1"})
	if err := (Host{Run: run}).RunScript(context.Background(), "/d", "bin/installdependencies.sh"); err == nil {
		t.Fatal("a failed script must be an error")
	}
}

// SwitchLink needs symlinks, which Windows grants only to privileged users;
// CI runs it on Linux.
func TestSwitchLinkReplacesTheLink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(dir, filepath.Join(dir, "probe")); err != nil {
		t.Skip("symlinks unavailable here:", err)
	}
	old, next := filepath.Join(dir, "2.337.0"), filepath.Join(dir, "2.338.0")
	for _, d := range []string{old, next} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cur := filepath.Join(dir, "current")
	var h Host
	if err := h.SwitchLink(old, cur); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cur+".tmp", []byte("leftover"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.SwitchLink(next, cur); err != nil {
		t.Fatal(err)
	}
	got, err := h.ReadLink(cur)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(next)
	if got != want {
		t.Fatalf("current resolves to %s, want %s", got, want)
	}
	if _, err := os.Lstat(cur + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("current.tmp left behind: %v", err)
	}
}

func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("download sent credentials")
		}
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("tarball bytes"))
	}))
	t.Cleanup(srv.Close)
	dst := filepath.Join(t.TempDir(), "runner.tar.gz")
	if err := Download(context.Background(), srv.URL+"/ok", dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "tarball bytes" {
		t.Fatalf("saved %q", b)
	}
	err := Download(context.Background(), srv.URL+"/missing", dst)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("404: %v", err)
	}
}
```

`internal/system/group_unix_test.go` (built on Unix only):

```go
//go:build unix

package system

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A cancelled script takes the children it started with it.
func TestExecGroupKillsTheScriptsChildren(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := ExecGroup(ctx, "sh", "-c", "sleep 30 & echo $! > "+pidFile+"; wait"); err == nil {
		t.Fatal("a cancelled script must fail")
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("child %d outlived the cancelled script", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 200 go test ./internal/system/`
Expected: FAIL to build with `h.Extract undefined` (and the other new names).

- [ ] **Step 3: Write the implementation** (`internal/system/system.go`)

Replace the import block (lines 4-14) with:

```go
import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)
```

Replace the first lines of `Exec`:

```go
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
```

with:

```go
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	return execute(ctx, false, name, args)
}

// ExecGroup is Exec for a command that starts children of its own, such as a
// script running apt: cancelling it kills its whole process group, not only
// the command.
func ExecGroup(ctx context.Context, name string, args ...string) ([]byte, error) {
	return execute(ctx, true, name, args)
}

func execute(ctx context.Context, group bool, name string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if group {
		killGroup(cmd)
	}
	var stderr bytes.Buffer
```

(the rest of the old `Exec` body is now `execute`'s, unchanged). Replace `type Host struct{ Run Runner }` with:

```go
type Host struct {
	Run Runner
	// Script runs scripts that start children of their own; nil uses Run.
	Script Runner
}
```

Append after `ChownR` (the end of the file):

```go

// Extract unpacks a .tar.gz into dir.
func (h Host) Extract(ctx context.Context, tarball, dir string) error {
	_, err := h.Run(ctx, "tar", "-xzf", tarball, "-C", dir)
	return err
}

// RunScript runs the script at path inside dir, as setup.sh runs
// bin/installdependencies.sh.
func (h Host) RunScript(ctx context.Context, dir, path string) error {
	run := h.Script
	if run == nil {
		run = h.Run
	}
	_, err := run(ctx, filepath.Join(dir, path))
	return err
}

// ReadLink resolves path through every symlink.
func (Host) ReadLink(path string) (string, error) { return filepath.EvalSymlinks(path) }

// SwitchLink points the symlink at path to target with one rename, so path
// never goes missing; a leftover path+".tmp" is replaced.
func (Host) SwitchLink(target, path string) error {
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DownloadTimeout bounds one Download.
var DownloadTimeout = 10 * time.Minute

// Download saves url to dst, following redirects. It sends no credentials:
// it fetches public release assets.
func Download(ctx context.Context, url, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, DownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
```

Create `internal/system/group_unix.go`:

```go
//go:build unix

package system

import (
	"os/exec"
	"syscall"
)

// killGroup starts cmd in a process group of its own and makes cancelling it
// kill that whole group.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
```

Create `internal/system/group_other.go`:

```go
//go:build !unix

package system

import "os/exec"

// killGroup leaves cmd as it is: ghr runs on Linux, and no test elsewhere
// starts a script.
func killGroup(*exec.Cmd) {}
```

In `internal/daemon/run.go:140`, replace `m.Host = system.Host{Run: system.Exec}` with:

```go
		m.Host = system.Host{Run: system.Exec, Script: system.ExecGroup}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 200 go test ./internal/system/ ./internal/daemon/ -v -run 'Host|SwitchLink|Download|ExecGroup' && timeout 120 gofmt -l . && timeout 300 go vet ./...`
Expected: PASS for `TestHostExtractAndRunScriptArgs`, `TestHostRunScriptReportsFailure`, `TestDownload`; `TestSwitchLinkReplacesTheLink` reports SKIP on Windows, and `TestExecGroupKillsTheScriptsChildren` is not built there (CI on Linux runs both); nothing from `gofmt` or `vet`.

- [ ] **Step 5: Commit**

```bash
git add internal/system/system.go internal/system/host_test.go internal/system/group_unix.go internal/system/group_other.go internal/system/group_unix_test.go internal/daemon/run.go
git commit -m "feat(system): add runner install host operations"
```

### Task 3: Runner manager seams

**Files:**
- Modify: `internal/runner/manager.go` (`GitHub` interface lines 41-49, `Host` interface lines 72-75, `Paths` lines 78-86, `Manager` fields lines 132-133)
- Modify: `internal/runner/lifecycle.go:93` (`spawn` resolves `dist/current` through the host)
- Modify: `internal/daemon/run_test.go:69` (`dirHost` embeds the real host)
- Test: `internal/runner/fakes_test.go` (fake releases, fake host operations, `linkDist`), `internal/runner/lifecycle_test.go` (one new test)

**Interfaces:**
- Consumes: C1 `github.Release` (Task 1); C2 (Task 2): `system.Host` must implement the new `runner.Host` methods, which `daemon.Run` relies on.
- Produces: C3.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The update reads and switches `dist/current` through the host, and spawning must resolve it the same way. Tests cannot create symlinks on Windows, so the fake host stores a link as a regular file holding the target path.

- [ ] **Step 1: Extend the fakes** (`internal/runner/fakes_test.go`)

In `type fakeGH struct`, add after `remaining int`:

```go
	releases  []github.Release
```

Add after `GenerateJITConfig` (before `func (f *fakeGH) RateRemaining`):

```go
func (f *fakeGH) ListRunnerReleases(context.Context) ([]github.Release, error) {
	if err := f.call("ListRunnerReleases", ""); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases, nil
}

func (f *fakeGH) setReleases(rels ...github.Release) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases = rels
}
```

Replace the `fakeHost` comment and struct:

```go
// fakeHost copies by creating the destination with a run.sh, like a real dist.
type fakeHost struct {
	mu      sync.Mutex
	chowned []string
}
```

with:

```go
// fakeHost copies by creating the destination with a run.sh, like a real dist.
// Its links are regular files holding the target path, because Windows lets
// only privileged users create symlinks.
type fakeHost struct {
	mu      sync.Mutex
	chowned []string
	calls   []string         // Extract and RunScript calls, as "<method> <dir>"
	errs    map[string]error // by method name
}

func (f *fakeHost) err(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.errs[method]
}

func (f *fakeHost) setErr(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errs == nil {
		f.errs = map[string]error{}
	}
	f.errs[method] = err
}

func (f *fakeHost) record(method, dir string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+" "+filepath.Base(dir))
}

// Extract unpacks a runner: run.sh and its dependency script.
func (f *fakeHost) Extract(_ context.Context, _, dir string) error {
	f.record("Extract", dir)
	if err := f.err("Extract"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "installdependencies.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"), 0o755)
}

func (f *fakeHost) RunScript(_ context.Context, dir, _ string) error {
	f.record("RunScript", dir)
	return f.err("RunScript")
}

// ReadLink reads a link file; any other path resolves as on disk.
func (f *fakeHost) ReadLink(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return filepath.EvalSymlinks(path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	target := string(b)
	if _, err := os.Stat(target); err != nil {
		return "", err
	}
	return target, nil
}

func (f *fakeHost) SwitchLink(target, path string) error {
	if err := f.err("SwitchLink"); err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", []byte(target), 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func (f *fakeHost) hostCalls() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}
```

(The existing `CopyTree` and `ChownR` methods of `fakeHost` stay as they are.)

Append at the end of the file:

```go

// linkDist gives the harness setup.sh's layout: dist/<ver> holding run.sh,
// and dist/current a fake host link to it. It returns the dist dir.
func (h *harness) linkDist(t *testing.T, ver string) string {
	t.Helper()
	dist := filepath.Join(h.root, "dist")
	dir := filepath.Join(dist, ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	cur := filepath.Join(dist, "current")
	if err := h.host.SwitchLink(dir, cur); err != nil {
		t.Fatal(err)
	}
	h.m.Paths.Dist = cur
	return dist
}
```

- [ ] **Step 2: Write the failing test** (append to `internal/runner/lifecycle_test.go`)

```go

// spawn resolves dist/current through the host, so the version it records is
// the one current points at.
func TestSpawnResolvesDistThroughHost(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	if err := h.m.spawn(context.Background(), h.cfg, "darkcloud"); err != nil {
		t.Fatal(err)
	}
	var meta Meta
	if err := readJSON(filepath.Join(h.m.instanceDir("aaaaaa"), MetaFile), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.DistVersion != "2.337.0" {
		t.Fatalf("dist version %q", meta.DistVersion)
	}
	if !strings.Contains(h.eventText(), "spawned aaaaaa (2.337.0)") {
		t.Fatalf("events:\n%s", h.eventText())
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `timeout 300 go test ./internal/runner/ -run TestSpawnResolvesDistThroughHost`
Expected: FAIL: `dist version "current"` (spawn still follows the path itself, and the fake link is a regular file).

- [ ] **Step 4: Write the implementation**

In `internal/runner/manager.go`, add to the `GitHub` interface, before `RateRemaining() int`:

```go
	ListRunnerReleases(ctx context.Context) ([]github.Release, error)
```

Replace the `Host` interface with:

```go
type Host interface {
	CopyTree(ctx context.Context, src, dst string) error
	ChownR(ctx context.Context, path, user string) error
	Extract(ctx context.Context, tarball, dir string) error
	RunScript(ctx context.Context, dir, path string) error
	ReadLink(path string) (string, error)
	SwitchLink(target, path string) error
}
```

Add to `Paths`, after `Home      string`:

```go
	// UpdateState is runner-update.json; empty keeps the update queue in memory only.
	UpdateState string
```

Add to `Manager`, after `NewID   func() string` and before the blank line above `mu`:

```go
	// Fetch downloads url to dst; the daemon sets system.Download.
	Fetch func(ctx context.Context, url, dst string) error
```

In `internal/runner/lifecycle.go` (line 93), replace:

```go
	dist, err := filepath.EvalSymlinks(m.Paths.Dist)
```

with:

```go
	dist, err := m.Host.ReadLink(m.Paths.Dist)
```

In `internal/daemon/run_test.go` (line 69), replace:

```go
type dirHost struct{}
```

with:

```go
// dirHost creates instance dirs without cp; links, extraction and scripts
// are the real host's.
type dirHost struct{ system.Host }
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 400 go test ./... && timeout 120 gofmt -l . && timeout 300 go vet ./...`
Expected: every package `ok`, including `TestSpawnResolvesDistThroughHost`; nothing from `gofmt` or `vet`.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/manager.go internal/runner/lifecycle.go internal/runner/fakes_test.go internal/runner/lifecycle_test.go internal/daemon/run_test.go
git commit -m "refactor(runner): add host and release seams"
```

### Task 4: Release check, warning and queue

**Files:**
- Modify: `internal/model/model.go` (`Status` gains `RunnerUpdate`; new type after `Status`)
- Create: `internal/runner/update.go`
- Modify: `internal/runner/manager.go` (two fields at the end of `Manager`; `Init` loads the state; `Status` fills `RunnerUpdate`)
- Modify: `internal/runner/tick.go` (the daily check, after demand is gathered)
- Test: `internal/runner/update_test.go` (new), `internal/runner/fakes_test.go` (`fakeGH.hook`, `UpdateState` in the harness, `callsText`, `newEvents`, `restart`)

**Interfaces:**
- Consumes: C1 (Task 1), C3 (Task 3).
- Produces: C4 `RunnerUpdate`; C5's exported names and Task 4's unexported ones.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

`runner-update.json` holds only `queued_at` and `warned_version`; everything else is rebuilt by the next check. Every change to the persisted part takes `fileMu` first, then `mu`, writes the file, and only then keeps the new value in memory.

- [ ] **Step 1: Extend the harness** (`internal/runner/fakes_test.go`)

In `fakeGH`, after the `releases` field Task 3 added, add:

```go
	hook      func(method string) // runs during every call, after it is recorded
```

Replace `fakeGH.call`:

```go
func (f *fakeGH) call(method, repo string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+" "+repo)
	return f.errs[method+" "+repo]
}
```

with (the hook runs unlocked, so it may call the manager, which calls the fake again):

```go
func (f *fakeGH) call(method, repo string) error {
	f.mu.Lock()
	f.calls = append(f.calls, method+" "+repo)
	err, hook := f.errs[method+" "+repo], f.hook
	f.mu.Unlock()
	if hook != nil {
		hook(method)
	}
	return err
}
```

In `newHarness`, replace:

```go
			Hooks: "/opt/ghr/hooks", Home: "/home/ghrunner"},
```

with:

```go
			Hooks: "/opt/ghr/hooks", Home: "/home/ghrunner", UpdateState: filepath.Join(root, "runner-update.json")},
```

Append at the end of the file:

```go

func (f *fakeGH) callsText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}

// newEvents is a fresh event ring on the harness clock, as a restarted daemon has.
func newEvents(h *harness) *events.Ring {
	ev := events.New()
	ev.Now = func() time.Time { return h.now }
	return ev
}

// restart replaces the manager with a fresh one on the same files, as a
// daemon restart does.
func (h *harness) restart(t *testing.T) {
	t.Helper()
	old := h.m
	h.m = &Manager{Config: old.Config, GH: old.GH, SD: old.SD, Docker: old.Docker, Host: old.Host, Paths: old.Paths,
		Events: newEvents(h), History: old.History, Now: old.Now, NewID: old.NewID, Fetch: old.Fetch}
	if err := h.m.Init(); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Write the failing tests** (`internal/runner/update_test.go`)

```go
package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
)

var published = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// release is an actions/runner release published days after published.
func release(ver string, days int, sum string) github.Release {
	body := "release notes"
	if sum != "" {
		body += "\n<!-- BEGIN SHA linux-x64 -->" + sum + "<!-- END SHA linux-x64 -->"
	}
	return github.Release{TagName: "v" + ver, PublishedAt: published.AddDate(0, 0, days), Body: body}
}

func TestFactsFor(t *testing.T) {
	rels := []github.Release{release("2.339.0", 9, ""), release("2.338.0", 3, ""), release("2.337.0", 0, ""), {TagName: "nightly"}}
	for _, c := range []struct {
		installed, latest string
		deadline          time.Time
	}{
		{"2.339.0", "2.339.0", time.Time{}},                 // current
		{"2.338.0", "2.339.0", published.AddDate(0, 0, 39)}, // one newer
		{"2.336.0", "2.339.0", published.AddDate(0, 0, 30)}, // several newer: the earliest counts
		{"2.340.0", "2.339.0", time.Time{}},                 // installed newer than latest
		{"", "2.339.0", time.Time{}},                        // installed unknown
	} {
		f, ok := factsFor(c.installed, rels)
		if !ok || f.version.String() != c.latest || !f.deadline.Equal(c.deadline) {
			t.Errorf("installed %q: latest %s deadline %v ok %v; want %s %v", c.installed, f.version, f.deadline, ok, c.latest, c.deadline)
		}
	}
	if _, ok := factsFor("2.337.0", []github.Release{{TagName: "nightly"}}); ok {
		t.Fatal("a listing without version tags gave facts")
	}
}

func TestReleaseCheckWarnsOncePerVersion(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setReleases(release("2.338.0", 0, ""), release("2.337.0", -20, ""))
	h.m.Tick(context.Background())
	const warning = "warn  runner 2.338.0 is available (installed 2.337.0); update by 2026-10-31"
	if n := strings.Count(h.eventText(), warning); n != 1 {
		t.Fatalf("%d warnings:\n%s", n, h.eventText())
	}
	u := h.m.Status().RunnerUpdate
	if u.Installed != "2.337.0" || u.Latest != "2.338.0" || u.Deadline == nil || !u.Deadline.Equal(published.AddDate(0, 0, 30)) ||
		u.CheckedAt == nil || !u.CheckedAt.Equal(h.now) || u.LatestPublished == nil || u.CheckError != "" || u.Queued {
		t.Fatalf("status %+v", u)
	}

	h.now = h.now.Add(23 * time.Hour)
	h.m.Tick(context.Background())
	if n := strings.Count(h.gh.callsText(), "ListRunnerReleases"); n != 1 {
		t.Fatalf("checked again within a day: %s", h.gh.callsText())
	}
	h.now = h.now.Add(time.Hour)
	h.m.Tick(context.Background())
	if n := strings.Count(h.gh.callsText(), "ListRunnerReleases"); n != 2 || strings.Count(h.eventText(), warning) != 1 {
		t.Fatalf("daily check: %s\n%s", h.gh.callsText(), h.eventText())
	}

	// A restarted daemon checks at once and remembers the warned version.
	h.restart(t)
	h.m.Tick(context.Background())
	if n := strings.Count(h.gh.callsText(), "ListRunnerReleases"); n != 3 || strings.Contains(h.eventText(), "is available") {
		t.Fatalf("after a restart: %s\n%s", h.gh.callsText(), h.eventText())
	}
	h.gh.setReleases(release("2.339.0", 5, ""), release("2.338.0", 0, ""))
	h.now = h.now.Add(24 * time.Hour)
	h.m.Tick(context.Background())
	if !strings.Contains(h.eventText(), "runner 2.339.0 is available (installed 2.337.0); update by 2026-10-31") {
		t.Fatalf("no warning for the next version:\n%s", h.eventText())
	}
}

func TestReleaseCheckFailureRetriesAfterAnHour(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "bad gateway"})
	h.m.Tick(context.Background())
	u := h.m.Status().RunnerUpdate
	if u.CheckError != "github: 502 bad gateway" || u.CheckedAt != nil || u.Installed != "2.337.0" {
		t.Fatalf("status %+v", u)
	}
	if strings.Contains(h.eventText(), "runner") {
		t.Fatalf("a failed check posted:\n%s", h.eventText())
	}
	h.gh.setErr("ListRunnerReleases ", nil)
	h.gh.setReleases(release("2.337.0", 0, ""))
	h.now = h.now.Add(59 * time.Minute)
	h.m.Tick(context.Background())
	if u := h.m.Status().RunnerUpdate; u.CheckedAt != nil {
		t.Fatalf("retried before an hour: %+v", u)
	}
	h.now = h.now.Add(time.Minute)
	h.m.Tick(context.Background())
	if u := h.m.Status().RunnerUpdate; u.CheckedAt == nil || u.CheckError != "" || u.Latest != "2.337.0" || u.Deadline != nil {
		t.Fatalf("retry: %+v", u)
	}
}

func TestReleaseCheckWaitsWhileDegraded(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth})
	h.m.Tick(context.Background())
	if strings.Contains(h.gh.callsText(), "ListRunnerReleases") {
		t.Fatalf("checked while degraded: %s", h.gh.callsText())
	}
}

func TestReleaseCheckWithoutDistIsUnknown(t *testing.T) {
	h := newHarness(t)
	h.m.Paths.Dist = h.root + "/dist/current"
	h.gh.setReleases(release("2.338.0", 0, ""))
	h.m.Tick(context.Background())
	if u := h.m.Status().RunnerUpdate; u.Installed != "" || u.Latest != "2.338.0" || u.Deadline != nil {
		t.Fatalf("status %+v", u)
	}
	if strings.Contains(h.eventText(), "is available") {
		t.Fatalf("warned without an installed version:\n%s", h.eventText())
	}
	if err := h.m.QueueUpdate(context.Background()); !errors.Is(err, ErrNoDist) {
		t.Fatalf("queue without dist: %v", err)
	}
}

func TestQueueAndCancelUpdate(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setReleases(release("2.337.0", 0, ""))
	err := h.m.QueueUpdate(context.Background())
	var utd UpToDateError
	if !errors.As(err, &utd) || err.Error() != "runner 2.337.0 is already up to date" {
		t.Fatalf("current: %v", err)
	}

	h.gh.setReleases(release("2.338.0", 0, ""))
	if err := h.m.QueueUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.m.QueueUpdate(context.Background()); err != nil {
		t.Fatalf("queueing twice: %v", err)
	}
	if n := strings.Count(h.eventText(), "info  runner update queued"); n != 1 {
		t.Fatalf("%d queued events:\n%s", n, h.eventText())
	}
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.QueuedAt == nil || !u.QueuedAt.Equal(h.now) {
		t.Fatalf("status %+v", u)
	}

	// The queue survives a restart.
	h.restart(t)
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.QueuedAt == nil || !u.QueuedAt.Equal(h.now) {
		t.Fatalf("queue lost on restart: %+v", u)
	}

	if err := h.m.CancelUpdate(); err != nil {
		t.Fatal(err)
	}
	if err := h.m.CancelUpdate(); err != nil {
		t.Fatalf("cancelling nothing: %v", err)
	}
	if n := strings.Count(h.eventText(), "info  runner update cancelled"); n != 1 {
		t.Fatalf("%d cancelled events:\n%s", n, h.eventText())
	}
	data, err := os.ReadFile(h.m.Paths.UpdateState)
	if err != nil || strings.Contains(string(data), "queued_at") || !strings.Contains(string(data), `"warned_version":"2.338.0"`) {
		t.Fatalf("state file %s %v", data, err)
	}

	h.m.mu.Lock()
	h.m.upd.running = true
	h.m.mu.Unlock()
	if err := h.m.QueueUpdate(context.Background()); !errors.Is(err, ErrUpdateRunning) {
		t.Fatalf("queue while running: %v", err)
	}
	if err := h.m.CancelUpdate(); !errors.Is(err, ErrUpdateRunning) {
		t.Fatalf("cancel while running: %v", err)
	}
}

func TestQueueUpdatePassesGitHubErrors(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	limit := &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Minute)}
	h.gh.setErr("ListRunnerReleases ", limit)
	if err := h.m.QueueUpdate(context.Background()); !errors.Is(err, limit) {
		t.Fatalf("rate limit: %v", err)
	}
	if h.m.apiAllowed(h.now) {
		t.Fatal("a rate-limited check did not pause the API")
	}
	h.m.Close()
	if err := h.m.QueueUpdate(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
}

func TestUnreadableUpdateStateStartsEmpty(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(h.m.Paths.UpdateState, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.restart(t)
	if !strings.Contains(h.eventText(), "runner update state unreadable, starting empty") || h.m.Status().RunnerUpdate.Queued {
		t.Fatalf("events:\n%s", h.eventText())
	}
}

// A rate limit on the release check pauses the API as on every GitHub call;
// the check itself posts nothing.
func TestReleaseCheckRateLimitPostsNoRunnerEvent(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Hour)})
	h.m.Tick(context.Background())
	if h.m.apiAllowed(h.now) {
		t.Fatal("the rate limit did not pause the API")
	}
	if txt := h.eventText(); !strings.Contains(txt, "GitHub rate limit") || strings.Contains(txt, "runner") {
		t.Fatalf("events:\n%s", txt)
	}
}

// A check that was in flight while an install switched dist/current does
// not overwrite the install's facts.
func TestStaleReleaseCheckIsDropped(t *testing.T) {
	h := newHarness(t)
	h.linkDist(t, "2.337.0")
	h.gh.setReleases(release("2.338.0", 0, ""), release("2.337.0", -20, ""))
	h.gh.hook = func(method string) {
		if method != "ListRunnerReleases" {
			return
		}
		h.m.mu.Lock()
		h.m.upd.installed, h.m.upd.deadline, h.m.upd.gen = "2.338.0", time.Time{}, h.m.upd.gen+1
		h.m.mu.Unlock()
	}
	if _, err := h.m.checkRelease(context.Background()); err != nil {
		t.Fatal(err)
	}
	if u := h.m.Status().RunnerUpdate; u.Installed != "2.338.0" || u.Deadline != nil || u.CheckedAt != nil {
		t.Fatalf("a stale check overwrote the install: %+v", u)
	}
	if strings.Contains(h.eventText(), "is available") {
		t.Fatalf("a stale check warned:\n%s", h.eventText())
	}
}

// A failed check after dist/current changed forgets what the last check said
// about the version installed before.
func TestFailedCheckForgetsFactsOfAnotherVersion(t *testing.T) {
	for _, c := range []struct {
		name, installed string
		change          func(t *testing.T, h *harness, dist string)
	}{
		{"switched to the latest", "2.338.0", func(t *testing.T, h *harness, _ string) { h.linkDist(t, "2.338.0") }},
		{"current removed", "", func(t *testing.T, h *harness, dist string) {
			if err := os.Remove(filepath.Join(dist, "current")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			dist := h.linkDist(t, "2.337.0")
			h.gh.setReleases(release("2.338.0", 0, ""), release("2.337.0", -20, ""))
			h.m.Tick(context.Background())
			if u := h.m.Status().RunnerUpdate; u.Deadline == nil {
				t.Fatalf("no update found: %+v", u)
			}
			c.change(t, h, dist)
			h.gh.setErr("ListRunnerReleases ", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "bad gateway"})
			if _, err := h.m.checkRelease(context.Background()); err == nil {
				t.Fatal("the check should fail")
			}
			u := h.m.Status().RunnerUpdate
			if u.Installed != c.installed || u.Deadline != nil || u.Latest != "" || u.CheckedAt != nil || u.CheckError != "github: 502 bad gateway" {
				t.Fatalf("status %+v", u)
			}
		})
	}
}
```

The fake GitHub records the release call as `"ListRunnerReleases "` (method, a space, an empty repo), which is the key `setErr` takes. The deadline date in the warnings is formatted in local time; the fixtures publish at 12:00 UTC so the date is the same in every zone from UTC−11 to UTC+11.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/runner/`
Expected: FAIL to build with `undefined: factsFor` (and the other new names).

- [ ] **Step 4: Add the model type** (`internal/model/model.go`)

Replace the end of `Status`:

```go
	Maintenance    MaintenanceStatus `json:"maintenance"`
}
```

with:

```go
	Maintenance    MaintenanceStatus `json:"maintenance"`
	RunnerUpdate   RunnerUpdate      `json:"runner_update"`
}

// RunnerUpdate is the GitHub Actions runner's version state, served inside
// GET /status. Each field is absent while unknown.
type RunnerUpdate struct {
	Installed       string     `json:"installed,omitempty"`
	Latest          string     `json:"latest,omitempty"`
	LatestPublished *time.Time `json:"latest_published,omitempty"`
	// Deadline is set only while a newer runner than Installed exists.
	Deadline     *time.Time `json:"deadline,omitempty"`
	CheckedAt    *time.Time `json:"checked_at,omitempty"`
	CheckError   string     `json:"check_error,omitempty"`
	Queued       bool       `json:"queued,omitempty"`
	QueuedAt     *time.Time `json:"queued_at,omitempty"`
	Running      bool       `json:"running,omitempty"`
	LastOutcome  string     `json:"last_outcome,omitempty"` // "ok", "failed", "current"
	LastError    string     `json:"last_error,omitempty"`
	LastFinished *time.Time `json:"last_finished,omitempty"`
}
```

- [ ] **Step 5: Write the update state** (`internal/runner/update.go`)

```go
package runner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

const (
	releaseCheckEvery = 24 * time.Hour
	releaseCheckRetry = time.Hour
	// GitHub stops queueing jobs to a runner 30 days after a newer version
	// was made available.
	updateGrace = 30 * 24 * time.Hour
)

var (
	ErrUpdateRunning = errors.New("a runner update is running")
	ErrNoDist        = errors.New("runner version unknown (no dist/current): run setup.sh first")
	errNoRelease     = errors.New("no actions/runner release with a version tag")
)

// UpToDateError is QueueUpdate's answer when no newer runner exists; it holds
// the installed version.
type UpToDateError string

func (e UpToDateError) Error() string { return "runner " + string(e) + " is already up to date" }

// updateFile is the persisted part of the runner update state.
type updateFile struct {
	QueuedAt      *time.Time `json:"queued_at,omitempty"`
	WarnedVersion string     `json:"warned_version,omitempty"`
}

// updateState is the runner update's state; guarded by Manager.mu. Only
// storeUpdateFile changes file.
type updateState struct {
	file         updateFile
	installed    string // "" when dist/current does not resolve
	latest       string
	latestPub    time.Time
	deadline     time.Time // zero unless a release newer than installed exists
	checkedAt    time.Time
	checkErr     string
	nextCheck    time.Time
	running      bool
	lastOutcome  string
	lastErr      string
	lastFinished time.Time
	gen          int // bumped when an install switches dist/current
}

// releaseFacts is what a release listing says about an installed version.
type releaseFacts struct {
	latest   github.Release
	version  github.Version
	deadline time.Time
}

// factsFor picks the highest release and the update deadline: 30 days after
// the earliest-published release newer than installed. ok is false when no
// release has a version tag.
func factsFor(installed string, rels []github.Release) (releaseFacts, bool) {
	inst, instOK := github.ParseVersion(installed)
	var f releaseFacts
	found := false
	var firstNewer time.Time
	for _, r := range rels {
		v, ok := github.ParseVersion(r.TagName)
		if !ok {
			continue
		}
		if !found || v.Compare(f.version) > 0 {
			f.latest, f.version, found = r, v, true
		}
		if instOK && v.Compare(inst) > 0 && (firstNewer.IsZero() || r.PublishedAt.Before(firstNewer)) {
			firstNewer = r.PublishedAt
		}
	}
	if !firstNewer.IsZero() {
		f.deadline = firstNewer.Add(updateGrace)
	}
	return f, found
}

// installedVersion is the base name of the directory dist/current resolves
// to, or "" when it does not resolve.
func (m *Manager) installedVersion() string {
	p, err := m.Host.ReadLink(m.Paths.Dist)
	if err != nil {
		return ""
	}
	return filepath.Base(p)
}

// loadUpdate starts the update state afresh from dist/current and
// runner-update.json; a missing file is an empty state.
func (m *Manager) loadUpdate() {
	m.upd = updateState{installed: m.installedVersion()}
	if m.Paths.UpdateState == "" {
		return
	}
	var f updateFile
	if err := readJSON(m.Paths.UpdateState, &f); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			m.Events.Add("warn", "", "runner update state unreadable, starting empty: %v", err)
		}
		return
	}
	m.upd.file = f
}

// storeUpdateFile writes f and only then keeps it, so memory never claims
// what the file does not hold. m.fileMu must be held.
func (m *Manager) storeUpdateFile(f updateFile) error {
	if m.Paths.UpdateState != "" {
		if err := writeJSONAtomic(m.Paths.UpdateState, f); err != nil {
			return fmt.Errorf("save runner update state: %w", err)
		}
	}
	m.mu.Lock()
	m.upd.file = f
	m.mu.Unlock()
	return nil
}

func (m *Manager) releaseCheckDue(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !now.Before(m.upd.nextCheck)
}

// checkRelease lists the runner releases and records what they say about the
// installed version, warning once per newer version. A failure keeps the last
// good facts and retries after an hour.
func (m *Manager) checkRelease(ctx context.Context) ([]github.Release, error) {
	m.mu.Lock()
	gen := m.upd.gen
	m.mu.Unlock()
	installed := m.installedVersion()
	rels, err := m.GH.ListRunnerReleases(ctx)
	now := m.Now()
	f, found := factsFor(installed, rels)
	if err == nil && !found {
		err = errNoRelease
	}
	latest := f.version.String()
	m.mu.Lock()
	// An install that switched dist/current during the request has already
	// published newer facts than this check read.
	stale := m.upd.gen != gen
	switch {
	case stale:
	case err != nil:
		if installed != m.upd.installed {
			m.upd.latest, m.upd.latestPub, m.upd.deadline, m.upd.checkedAt = "", time.Time{}, time.Time{}, time.Time{}
		}
		m.upd.installed, m.upd.checkErr, m.upd.nextCheck = installed, err.Error(), now.Add(releaseCheckRetry)
	default:
		m.upd.installed, m.upd.latest, m.upd.latestPub, m.upd.deadline = installed, latest, f.latest.PublishedAt, f.deadline
		m.upd.checkedAt, m.upd.checkErr, m.upd.nextCheck = now, "", now.Add(releaseCheckEvery)
	}
	m.mu.Unlock()
	if err != nil {
		if github.IsKind(err, github.ErrAuth) || github.IsKind(err, github.ErrRateLimit) {
			m.apiErr("", err, now)
		}
		return nil, err
	}
	if !stale && !f.deadline.IsZero() {
		m.warnUpdate(installed, latest, f.deadline)
	}
	return rels, nil
}

// warnUpdate posts the update warning unless this version was already warned
// about, before or since the last restart.
func (m *Manager) warnUpdate(installed, latest string, deadline time.Time) {
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	m.mu.Lock()
	f := m.upd.file
	m.mu.Unlock()
	if f.WarnedVersion == latest {
		return
	}
	f.WarnedVersion = latest
	if err := m.storeUpdateFile(f); err != nil {
		m.Events.Add("warn", "", "%v", err)
	}
	m.Events.Add("warn", "", "runner %s is available (installed %s); update by %s", latest, installed, deadline.Local().Format(time.DateOnly))
}

// QueueUpdate checks GitHub for a newer runner and queues its install for the
// next time ghr is free. Queueing twice is not an error.
func (m *Manager) QueueUpdate(ctx context.Context) error {
	m.mu.Lock()
	closed, running := m.closed, m.upd.running
	m.mu.Unlock()
	switch {
	case closed:
		return ErrClosed
	case running:
		return ErrUpdateRunning
	}
	if _, err := m.checkRelease(ctx); err != nil {
		return err
	}
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	m.mu.Lock()
	installed, newer, running, f := m.upd.installed, !m.upd.deadline.IsZero(), m.upd.running, m.upd.file
	m.mu.Unlock()
	switch {
	case running:
		return ErrUpdateRunning
	case installed == "":
		return ErrNoDist
	case !newer:
		return UpToDateError(installed)
	case f.QueuedAt != nil:
		return nil
	}
	now := m.Now()
	f.QueuedAt = &now
	if err := m.storeUpdateFile(f); err != nil {
		return err
	}
	m.Events.Add("info", "", "runner update queued")
	return nil
}

// CancelUpdate drops a queued update; with none queued it does nothing.
func (m *Manager) CancelUpdate() error {
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	m.mu.Lock()
	running, f := m.upd.running, m.upd.file
	m.mu.Unlock()
	if running {
		return ErrUpdateRunning
	}
	if f.QueuedAt == nil {
		return nil
	}
	f.QueuedAt = nil
	if err := m.storeUpdateFile(f); err != nil {
		return err
	}
	m.Events.Add("info", "", "runner update cancelled")
	return nil
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// runnerUpdate renders the update state for /status; m.mu must be held.
func (m *Manager) runnerUpdate() model.RunnerUpdate {
	u := m.upd
	return model.RunnerUpdate{
		Installed: u.installed, Latest: u.latest, LatestPublished: timePtr(u.latestPub), Deadline: timePtr(u.deadline),
		CheckedAt: timePtr(u.checkedAt), CheckError: u.checkErr,
		Queued: u.file.QueuedAt != nil, QueuedAt: u.file.QueuedAt, Running: u.running,
		LastOutcome: u.lastOutcome, LastError: u.lastErr, LastFinished: timePtr(u.lastFinished),
	}
}
```

- [ ] **Step 6: Wire it into the manager and the tick**

In `internal/runner/manager.go`, replace the end of the `Manager` struct:

```go
	lastJob        map[string]model.HistoryEntry
	epoch          string
	wg             sync.WaitGroup
}
```

with:

```go
	lastJob        map[string]model.HistoryEntry
	epoch          string
	wg             sync.WaitGroup
	upd            updateState // guarded by mu
	// fileMu serialises changes to the persisted update state, so a queue,
	// a cancel, a warning and an update start never overwrite one another;
	// take it before mu.
	fileMu sync.Mutex
}
```

In `Init`, after `m.epoch = strconv.FormatInt(m.Now().UnixNano(), 36)`, add:

```go
	m.loadUpdate()
```

In `Status`, replace:

```go
		Maintenance: m.maint,
	}
```

with:

```go
		Maintenance: m.maint, RunnerUpdate: m.runnerUpdate(),
	}
```

In `internal/runner/tick.go` (`Tick`), replace:

```go
	if m.apiAllowed(now) {
		m.gatherDemand(ctx, cfg, now)
	}
```

with:

```go
	if m.apiAllowed(now) {
		m.gatherDemand(ctx, cfg, now)
	}
	if m.apiAllowed(now) && !m.isDegraded() && m.releaseCheckDue(now) {
		m.checkRelease(ctx)
	}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `timeout 400 go test ./... && timeout 120 gofmt -l . && timeout 300 go vet ./...`
Expected: every package `ok` (the existing runner tests now see a failed release check on their first tick, which posts nothing); nothing from `gofmt` or `vet`.

- [ ] **Step 8: Commit**

```bash
git add internal/model/model.go internal/runner/update.go internal/runner/manager.go internal/runner/tick.go internal/runner/update_test.go internal/runner/fakes_test.go
git commit -m "feat(runner): check releases, queue runner update"
```

### Task 5: Runner install steps

**Files:**
- Create: `internal/runner/install.go`
- Test: `internal/runner/install_test.go` (new), `internal/runner/fakes_test.go` (`fakeHost.onScript`)

**Interfaces:**
- Consumes: C3 (Task 3: `Host.Extract`, `RunScript`, `ReadLink`, `SwitchLink`, `Manager.Fetch`, the test fakes); C5 from Task 4 (`checkRelease`, `factsFor`, `storeUpdateFile`, `fileMu`, `upd`, `ErrNoDist`, and the test helper `release`).
- Produces: C5's Task 5 names (`installLatest`, `runUpdate`, `clearQueue`, `complete`, `stepError`) and the test helpers `tarball`, `tarballSum`, `queuedUpdate`, `current`, `distEntries`.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 3 = 5

This task writes the update itself; Task 6 decides when it runs. The steps follow `setup.sh`'s `install_runner` and `gc_dist`: a version directory is published only after its dependency script succeeded, and `current` moves only to a published directory, so `current` never points at an incomplete install. Tests call `runUpdate` directly with the manager's own context, which `Close` cancels.

- [ ] **Step 1: Let a test act while the dependency script runs** (`internal/runner/fakes_test.go`)

Replace the `fakeHost` struct's fields:

```go
	mu      sync.Mutex
	chowned []string
	calls   []string         // Extract and RunScript calls, as "<method> <dir>"
	errs    map[string]error // by method name
```

with:

```go
	mu       sync.Mutex
	chowned  []string
	calls    []string         // Extract and RunScript calls, as "<method> <dir>"
	errs     map[string]error // by method name
	onScript func()           // runs inside RunScript
```

Replace `fakeHost.RunScript`:

```go
func (f *fakeHost) RunScript(_ context.Context, dir, _ string) error {
	f.record("RunScript", dir)
	return f.err("RunScript")
}
```

with:

```go
func (f *fakeHost) RunScript(_ context.Context, dir, _ string) error {
	f.record("RunScript", dir)
	f.mu.Lock()
	hook := f.onScript
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return f.err("RunScript")
}
```

- [ ] **Step 2: Write the failing tests** (`internal/runner/install_test.go`)

```go
package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var tarball = []byte("actions-runner tarball")

func tarballSum() string {
	s := sha256.Sum256(tarball)
	return hex.EncodeToString(s[:])
}

// queuedUpdate installs 2.337.0, releases 2.338.0 whose notes carry sum, and
// queues the update. The returned list collects the URLs Fetch was asked for.
func queuedUpdate(t *testing.T, h *harness, sum string) (dist string, fetched *[]string) {
	t.Helper()
	dist = h.linkDist(t, "2.337.0")
	h.gh.setReleases(release("2.338.0", 0, sum), release("2.337.0", -20, ""))
	urls := []string{}
	h.m.Fetch = func(_ context.Context, url, dst string) error {
		urls = append(urls, url)
		return os.WriteFile(dst, tarball, 0o644)
	}
	if err := h.m.QueueUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return dist, &urls
}

func current(t *testing.T, h *harness) string {
	t.Helper()
	p, err := h.host.ReadLink(h.m.Paths.Dist)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Base(p)
}

func distEntries(t *testing.T, dist string) string {
	t.Helper()
	es, err := os.ReadDir(dist)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}

func TestUpdateInstallsAndSwitchesCurrent(t *testing.T) {
	h := newHarness(t)
	dist, fetched := queuedUpdate(t, h, tarballSum())
	h.m.runUpdate(h.m.maintCtx)
	if got := current(t, h); got != "2.338.0" {
		t.Fatalf("current is %s", got)
	}
	if got := distEntries(t, dist); got != "2.338.0,current" {
		t.Fatalf("dist holds %s; old versions and staging must go", got)
	}
	if !complete(filepath.Join(dist, "2.338.0")) {
		t.Fatal("2.338.0 is not a complete install")
	}
	if _, err := os.Stat(filepath.Join(dist, "2.338.0", "runner.tar.gz")); !os.IsNotExist(err) {
		t.Fatalf("tarball kept: %v", err)
	}
	want := "https://github.com/actions/runner/releases/download/v2.338.0/actions-runner-linux-x64-2.338.0.tar.gz"
	if len(*fetched) != 1 || (*fetched)[0] != want {
		t.Fatalf("fetched %v", *fetched)
	}
	if got := h.host.hostCalls(); got != "Extract 2.338.0.tmp,RunScript 2.338.0.tmp" {
		t.Fatalf("host calls %s", got)
	}
	if !strings.Contains(h.eventText(), "ok  runner updated to 2.338.0") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	u := h.m.Status().RunnerUpdate
	if u.Installed != "2.338.0" || u.Deadline != nil || u.Queued || u.LastOutcome != "ok" || u.LastError != "" ||
		u.LastFinished == nil || !u.LastFinished.Equal(h.now) {
		t.Fatalf("status %+v", u)
	}
	if data, _ := os.ReadFile(h.m.Paths.UpdateState); strings.Contains(string(data), "queued_at") {
		t.Fatalf("queue still on disk: %s", data)
	}
}

func TestUpdateFailuresLeaveCurrent(t *testing.T) {
	for _, c := range []struct {
		name, sum, want string
		setup           func(h *harness)
	}{
		{"checksum mismatch", strings.Repeat("0", 64), "checksum: SHA-256 mismatch", nil},
		{"no checksum", "", "checksum: no linux-x64 checksum in the v2.338.0 release notes", nil},
		{"download", tarballSum(), "download: connection reset", func(h *harness) {
			h.m.Fetch = func(context.Context, string, string) error { return errors.New("connection reset") }
		}},
		{"extract", tarballSum(), "extract: tar: corrupt", func(h *harness) { h.host.setErr("Extract", errors.New("tar: corrupt")) }},
		{"dependencies", tarballSum(), "dependencies: exit status 1", func(h *harness) { h.host.setErr("RunScript", errors.New("exit status 1")) }},
		{"switch", tarballSum(), "switch: read-only file system", func(h *harness) {
			h.host.setErr("SwitchLink", errors.New("read-only file system"))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			dist, _ := queuedUpdate(t, h, c.sum)
			if c.setup != nil {
				c.setup(h)
			}
			h.m.runUpdate(h.m.maintCtx)
			if got := current(t, h); got != "2.337.0" {
				t.Fatalf("current moved to %s", got)
			}
			if _, err := os.Stat(filepath.Join(dist, "2.338.0.tmp")); !os.IsNotExist(err) {
				t.Fatalf("staging kept: %v", err)
			}
			if !strings.Contains(h.eventText(), "error  runner update failed: "+c.want) {
				t.Fatalf("events:\n%s", h.eventText())
			}
			u := h.m.Status().RunnerUpdate
			if u.Queued || u.LastOutcome != "failed" || !strings.HasPrefix(u.LastError, c.want) || u.Installed != "2.337.0" {
				t.Fatalf("status %+v", u)
			}
		})
	}
}

func TestUpdateSkipsDownloadForCompleteVersion(t *testing.T) {
	h := newHarness(t)
	dist, fetched := queuedUpdate(t, h, tarballSum())
	ready := filepath.Join(dist, "2.338.0")
	if err := os.MkdirAll(ready, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ready, "run.sh"), []byte("#!/bin/sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.m.runUpdate(h.m.maintCtx)
	if current(t, h) != "2.338.0" || len(*fetched) != 0 || h.host.hostCalls() != "" {
		t.Fatalf("current %s fetched %v host %q", current(t, h), *fetched, h.host.hostCalls())
	}
}

func TestUpdateFinishesWhenAlreadyCurrent(t *testing.T) {
	h := newHarness(t)
	_, fetched := queuedUpdate(t, h, tarballSum())
	h.gh.setReleases(release("2.337.0", -20, ""))
	h.m.runUpdate(h.m.maintCtx)
	if len(*fetched) != 0 || current(t, h) != "2.337.0" || !strings.Contains(h.eventText(), "info  runner already up to date (2.337.0)") {
		t.Fatalf("fetched %v\n%s", *fetched, h.eventText())
	}
	if u := h.m.Status().RunnerUpdate; u.Queued || u.LastOutcome != "current" {
		t.Fatalf("status %+v", u)
	}
}

func TestUpdateRemovesLeftoverStaging(t *testing.T) {
	h := newHarness(t)
	dist, _ := queuedUpdate(t, h, tarballSum())
	if err := os.MkdirAll(filepath.Join(dist, "2.336.0.tmp", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.m.runUpdate(h.m.maintCtx)
	if got := distEntries(t, dist); got != "2.338.0,current" {
		t.Fatalf("dist holds %s", got)
	}
}

func TestUpdateShutdownKeepsQueue(t *testing.T) {
	h := newHarness(t)
	dist, _ := queuedUpdate(t, h, tarballSum())
	h.m.Fetch = func(ctx context.Context, _, _ string) error {
		h.m.Close()
		<-ctx.Done()
		return ctx.Err()
	}
	h.m.runUpdate(h.m.maintCtx)
	if !strings.Contains(h.eventText(), "warn  runner update interrupted by shutdown") || strings.Contains(h.eventText(), "failed") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.LastOutcome != "" {
		t.Fatalf("status %+v", u)
	}
	if current(t, h) != "2.337.0" {
		t.Fatal("current moved")
	}
	if _, err := os.Stat(filepath.Join(dist, "2.338.0.tmp")); !os.IsNotExist(err) {
		t.Fatalf("staging kept: %v", err)
	}
}

// A cancel that lands before current switches leaves the installed runner as
// it was and keeps the queue, whether a version was just staged or reused.
func TestUpdateCancelledBeforeTheSwitchKeepsCurrent(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, h *harness, dist string)
	}{
		{"after the dependency script", func(t *testing.T, h *harness, _ string) { h.host.onScript = h.m.Close }},
		{"reusing a complete version", func(t *testing.T, h *harness, dist string) {
			ready := filepath.Join(dist, "2.338.0")
			if err := os.MkdirAll(ready, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ready, "run.sh"), []byte("#!/bin/sh"), 0o755); err != nil {
				t.Fatal(err)
			}
			h.m.Close()
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			dist, _ := queuedUpdate(t, h, tarballSum())
			c.setup(t, h, dist)
			h.m.runUpdate(h.m.maintCtx)
			if got := current(t, h); got != "2.337.0" {
				t.Fatalf("current moved to %s", got)
			}
			if !strings.Contains(h.eventText(), "warn  runner update interrupted by shutdown") {
				t.Fatalf("events:\n%s", h.eventText())
			}
			if u := h.m.Status().RunnerUpdate; !u.Queued || u.LastOutcome != "" {
				t.Fatalf("status %+v", u)
			}
			if _, err := os.Stat(filepath.Join(dist, "2.338.0.tmp")); !os.IsNotExist(err) {
				t.Fatalf("staging kept: %v", err)
			}
		})
	}
}

// When the state file cannot be rewritten, the update still ends the queue
// for good: the file goes, so a restart does not update again.
func TestFailedQueueClearSurvivesRestart(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, "")
	if err := os.MkdirAll(h.m.Paths.UpdateState+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	h.m.runUpdate(h.m.maintCtx)
	if !strings.Contains(h.eventText(), "save runner update state") || !strings.Contains(h.eventText(), "runner update failed: checksum") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	if _, err := os.Stat(h.m.Paths.UpdateState); !os.IsNotExist(err) {
		t.Fatalf("state file kept: %v", err)
	}
	h.restart(t)
	if u := h.m.Status().RunnerUpdate; u.Queued {
		t.Fatalf("the update is queued again after a restart: %+v", u)
	}
}

// Old versions that cannot be removed after the switch leave a warning; the
// update itself succeeded.
func TestUpdateWarnsWhenOldVersionsStay(t *testing.T) {
	h := newHarness(t)
	dist, _ := queuedUpdate(t, h, tarballSum())
	removeAll = func(path string) error {
		if filepath.Base(path) == "2.330.0" {
			return errors.New("device busy")
		}
		return os.RemoveAll(path)
	}
	t.Cleanup(func() { removeAll = os.RemoveAll })
	h.m.runUpdate(h.m.maintCtx)
	txt := h.eventText()
	if current(t, h) != "2.338.0" || !strings.Contains(txt, "warn  runner update: remove old runner 2.330.0: device busy") ||
		!strings.Contains(txt, "ok  runner updated to 2.338.0") {
		t.Fatalf("current %s\n%s", current(t, h), txt)
	}
	if got := distEntries(t, dist); got != "2.330.0,2.338.0,current" {
		t.Fatalf("dist holds %s", got)
	}
	if u := h.m.Status().RunnerUpdate; u.LastOutcome != "ok" || u.Queued {
		t.Fatalf("status %+v", u)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/runner/`
Expected: FAIL to build with `h.m.runUpdate undefined` and `undefined: complete`.

- [ ] **Step 4: Write the implementation** (`internal/runner/install.go`)

```go
package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// stepError names the update step that failed.
type stepError struct {
	step string
	err  error
}

func (e *stepError) Error() string { return e.step + ": " + e.err.Error() }
func (e *stepError) Unwrap() error { return e.err }

const tarballURL = "https://github.com/actions/runner/releases/download/v%[1]s/actions-runner-linux-x64-%[1]s.tar.gz"

// complete reports whether dir is a finished install: setup.sh and the update
// publish a dist dir only after its dependency script succeeded, so an
// executable run.sh means the version is complete.
func complete(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, "run.sh"))
	// Windows has no execute bit; ghr runs on Linux, its tests on Windows too.
	return err == nil && fi.Mode().IsRegular() && (fi.Mode().Perm()&0o111 != 0 || runtime.GOOS == "windows")
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// installLatest installs the latest runner release and points dist/current
// at it. It returns the version now current and "ok", or "current" when the
// installed runner was already the latest. A failure is a *stepError and
// leaves dist/current untouched.
func (m *Manager) installLatest(ctx context.Context) (string, string, error) {
	rels, err := m.checkRelease(ctx)
	if err != nil {
		return "", "", &stepError{"check", err}
	}
	m.mu.Lock()
	installed, newer := m.upd.installed, !m.upd.deadline.IsZero()
	m.mu.Unlock()
	switch {
	case installed == "":
		return "", "", &stepError{"check", ErrNoDist}
	case !newer:
		return installed, "current", nil
	}
	f, _ := factsFor(installed, rels)
	ver := f.version.String()
	sum, ok := f.latest.LinuxX64SHA256()
	if !ok {
		return "", "", &stepError{"checksum", fmt.Errorf("no linux-x64 checksum in the v%s release notes", ver)}
	}
	distDir := filepath.Dir(m.Paths.Dist)
	dir := filepath.Join(distDir, ver)
	if !complete(dir) {
		if err := m.stage(ctx, distDir, ver, sum); err != nil {
			return "", "", err
		}
	}
	// Switching current commits the update; one cancelled before it leaves the
	// installed runner as it was.
	if err := ctx.Err(); err != nil {
		return "", "", &stepError{"switch", err}
	}
	if err := m.Host.SwitchLink(dir, m.Paths.Dist); err != nil {
		return "", "", &stepError{"switch", err}
	}
	nf, _ := factsFor(ver, rels)
	m.mu.Lock()
	m.upd.installed, m.upd.deadline, m.upd.gen = ver, nf.deadline, m.upd.gen+1
	m.mu.Unlock()
	m.removeOldVersions(distDir, ver)
	return ver, "ok", nil
}

// stage downloads, verifies, unpacks and prepares version ver in
// dist/<ver>.tmp, then renames it to dist/<ver>. Every failure removes the
// staging dir.
func (m *Manager) stage(ctx context.Context, distDir, ver, sum string) error {
	leftovers, _ := filepath.Glob(filepath.Join(distDir, "*.tmp"))
	for _, l := range leftovers {
		os.RemoveAll(l)
	}
	staging := filepath.Join(distDir, ver+".tmp")
	fail := func(step string, err error) error {
		os.RemoveAll(staging)
		return &stepError{step, err}
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return fail("download", err)
	}
	tarball := filepath.Join(staging, "runner.tar.gz")
	if err := m.Fetch(ctx, fmt.Sprintf(tarballURL, ver), tarball); err != nil {
		return fail("download", err)
	}
	got, err := fileSHA256(tarball)
	if err != nil {
		return fail("checksum", err)
	}
	if got != sum {
		return fail("checksum", fmt.Errorf("SHA-256 mismatch: the release notes say %s, the download is %s", sum, got))
	}
	if err := m.Host.Extract(ctx, tarball, staging); err != nil {
		return fail("extract", err)
	}
	if err := os.Remove(tarball); err != nil {
		return fail("extract", err)
	}
	if err := m.Host.RunScript(ctx, staging, "bin/installdependencies.sh"); err != nil {
		return fail("dependencies", err)
	}
	final := filepath.Join(distDir, ver)
	if err := os.RemoveAll(final); err != nil {
		return fail("install", err)
	}
	if err := os.Rename(staging, final); err != nil {
		return fail("install", err)
	}
	return nil
}

// removeAll is os.RemoveAll; tests replace it to make a removal fail.
var removeAll = os.RemoveAll

// removeOldVersions deletes every dist dir but ver. Live runners keep
// working: each instance copied the runner files when it was spawned. It runs
// after current switched, so a failure is a warning, not a failed update.
func (m *Manager) removeOldVersions(distDir, ver string) {
	entries, err := os.ReadDir(distDir)
	if err != nil {
		m.Events.Add("warn", "", "runner update: list %s: %v", distDir, err)
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ver {
			continue
		}
		if err := removeAll(filepath.Join(distDir, e.Name())); err != nil {
			m.Events.Add("warn", "", "runner update: remove old runner %s: %v", e.Name(), err)
		}
	}
}

// clearQueue drops the queued update. When the state cannot be rewritten the
// file is removed instead, so neither this daemon nor the next one repeats
// the update on its own; only the warned version is forgotten.
func (m *Manager) clearQueue() {
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	m.mu.Lock()
	f := m.upd.file
	m.mu.Unlock()
	f.QueuedAt = nil
	if err := m.storeUpdateFile(f); err != nil {
		m.Events.Add("warn", "", "%v", err)
		if rerr := os.Remove(m.Paths.UpdateState); rerr != nil && !os.IsNotExist(rerr) {
			m.Events.Add("warn", "", "remove runner update state: %v", rerr)
		}
		m.mu.Lock()
		m.upd.file = updateFile{}
		m.mu.Unlock()
	}
}

// runUpdate installs the latest runner and records the outcome. A failure
// clears the queue, so only the owner re-queues; a shutdown keeps it, so the
// update runs again after the restart.
func (m *Manager) runUpdate(ctx context.Context) {
	ver, outcome, err := m.installLatest(ctx)
	if err != nil && m.maintCtx.Err() != nil {
		m.Events.Add("warn", "", "runner update interrupted by shutdown")
		return
	}
	if err != nil {
		outcome = "failed"
	}
	m.clearQueue()
	now := m.Now()
	m.mu.Lock()
	m.upd.lastOutcome, m.upd.lastFinished, m.upd.lastErr = outcome, now, ""
	if err != nil {
		m.upd.lastErr = err.Error()
	}
	m.mu.Unlock()
	switch outcome {
	case "failed":
		m.Events.Add("error", "", "runner update failed: %v", err)
	case "current":
		m.Events.Add("info", "", "runner already up to date (%s)", ver)
	default:
		m.Events.Add("ok", "", "runner updated to %s", ver)
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/runner/ && timeout 120 gofmt -l . && timeout 300 go vet ./internal/runner/`
Expected: `ok  github.com/darkraise/ghr/internal/runner`; nothing from `gofmt` or `vet`.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/install.go internal/runner/install_test.go internal/runner/fakes_test.go
git commit -m "feat(runner): install the latest runner release"
```

### Task 6: Start a queued update when ghr is free

**Files:**
- Modify: `internal/runner/tick.go` (`Tick`, `gatherDemand` returns whether every unpaused repo answered, `spawnPlanned` spawns nothing while an update runs)
- Modify: `internal/runner/lifecycle.go` (`stopIdle` delegates to a new `stopIdleRunner` returning `idleStop`, lines 505-534)
- Modify: `internal/runner/manager.go` (`StartPrune` refuses during an update, lines 192-193 and 205-209)
- Modify: `internal/runner/update.go` (import `config` and `sched`; append `startUpdateIfFree`)
- Test: `internal/runner/update_start_test.go` (new)

**Interfaces:**
- Consumes: C5 from Tasks 4 and 5 (`runUpdate`, `fileMu`, `upd`, `ErrUpdateRunning`, test helpers `queuedUpdate`, `current`, `tarball`, `tarballSum`); existing `queuedRun` (`internal/runner/tick_test.go:14`) and `waitOrFail` (`internal/runner/maintenance_test.go:36`).
- Produces: C5's Task 6 names; `StartPrune` returns `ErrUpdateRunning` while an update runs.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 3 = 5

The decision runs on the tick goroutine after idle runners and start timeouts are handled and before spawning. It holds `fileMu` then `mu` while it checks the queue and the conditions and marks the update running, so a cancel can never slip between the check and the start. It then stops every idle runner under the update's own context; one that took a job, or could not be checked or stopped, means ghr was not free after all, so the reservation is released and the queued update waits for a later tick. A config change during the poll (`m.Config() != cfg`) also defers it, because the poll did not cover the new config. The update takes the maintenance reservation (`m.pruning`) that manual and automatic pruning share, runs on its own goroutine under `m.maintCtx` (which `Close` cancels) with a 10-minute timeout, and is counted in `m.wg` so `Wait` covers it.

- [ ] **Step 1: Write the failing tests** (`internal/runner/update_start_test.go`)

```go
package runner

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/sched"
)

// holdFetch makes Fetch block until release is called or its context ends,
// signalling entered when it starts; it then writes the tarball.
func holdFetch(t *testing.T, h *harness) (entered chan struct{}, release func()) {
	entered = make(chan struct{})
	released := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(released) }) }
	t.Cleanup(release)
	h.m.Fetch = func(ctx context.Context, _, dst string) error {
		close(entered)
		select {
		case <-released:
			return os.WriteFile(dst, tarball, 0o644)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return entered, release
}

// idleRunner spawns a darkmem runner and makes it idle and online.
func idleRunner(t *testing.T, h *harness) string {
	t.Helper()
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	id := "aaaaaa"
	h.gh.setRunner(h.m.insts[id].RunnerID, "online", false)
	h.m.setState(id, sched.Idle)
	return id
}

func jitCalls(h *harness) int {
	return strings.Count(h.gh.callsText(), "GenerateJITConfig")
}

func TestQueuedUpdateRunsWhenFree(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	id := idleRunner(t, h)
	h.m.Tick(context.Background())
	h.m.Wait()
	if current(t, h) != "2.338.0" {
		t.Fatalf("current %s\n%s", current(t, h), h.eventText())
	}
	if len(h.sd.stopped) != 1 || h.sd.stopped[0] != UnitPrefix+id {
		t.Fatalf("stopped %v; the idle runner must stop for the update", h.sd.stopped)
	}
	txt := h.eventText()
	stopped, started := strings.Index(txt, "stopped idle runner "+id), strings.Index(txt, "info  runner update started")
	if stopped < 0 || started < stopped || !strings.Contains(txt, "ok  runner updated to 2.338.0") {
		t.Fatalf("events:\n%s", txt)
	}
	if u := h.m.Status().RunnerUpdate; u.Running || u.Queued || u.LastOutcome != "ok" {
		t.Fatalf("status %+v", u)
	}
	if err := h.m.StartPrune(); err != nil {
		t.Fatalf("the update kept the maintenance reservation: %v", err)
	}
	h.m.Wait()
}

func TestQueuedUpdateWaitsUntilFree(t *testing.T) {
	for _, c := range []struct {
		name  string
		block func(t *testing.T, h *harness)
	}{
		{"starting runner", func(t *testing.T, h *harness) {
			if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
				t.Fatal(err)
			}
		}},
		{"busy runner", func(t *testing.T, h *harness) {
			id := idleRunner(t, h)
			h.m.setState(id, sched.Busy)
		}},
		{"queued job", func(t *testing.T, h *harness) {
			queuedRun(h, "darkmem", 7, github.Job{ID: 70, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
			h.cfg.GlobalMax = 0 // keep the job queued: nothing spawns for it
		}},
		{"demand poll failed", func(t *testing.T, h *harness) {
			h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer})
		}},
		{"rate limited", func(t *testing.T, h *harness) {
			h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Minute)})
		}},
		{"degraded", func(t *testing.T, h *harness) {
			h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth})
		}},
		{"prune running", func(t *testing.T, h *harness) {
			h.m.mu.Lock()
			h.m.pruning = true
			h.m.mu.Unlock()
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			_, fetched := queuedUpdate(t, h, tarballSum())
			c.block(t, h)
			h.m.Tick(context.Background())
			h.m.Wait()
			if strings.Contains(h.eventText(), "runner update started") || len(*fetched) != 0 {
				t.Fatalf("the update started:\n%s", h.eventText())
			}
			if u := h.m.Status().RunnerUpdate; !u.Queued || u.Running {
				t.Fatalf("status %+v", u)
			}
		})
	}
}

func TestNoSpawnWhileUpdating(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	entered, release := holdFetch(t, h)
	h.m.Tick(context.Background())
	waitOrFail(t, entered, "the update to download")
	if u := h.m.Status().RunnerUpdate; !u.Running {
		t.Fatalf("status %+v", u)
	}
	if err := h.m.StartPrune(); !errors.Is(err, ErrUpdateRunning) {
		t.Fatalf("prune during the update: %v", err)
	}
	if err := h.m.CancelUpdate(); !errors.Is(err, ErrUpdateRunning) {
		t.Fatalf("cancel during the update: %v", err)
	}
	queuedRun(h, "darkmem", 7, github.Job{ID: 70, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	if n := jitCalls(h); n != 0 {
		t.Fatalf("%d spawns while updating", n)
	}
	release()
	h.m.Wait()
	h.m.Tick(context.Background())
	if n := jitCalls(h); n != 1 {
		t.Fatalf("%d spawns after the update", n)
	}
	if !strings.Contains(h.eventText(), "spawned aaaaaa (2.338.0)") {
		t.Fatalf("the runner did not start on the new version:\n%s", h.eventText())
	}
}

func TestShutdownInterruptsTheUpdate(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	entered, _ := holdFetch(t, h)
	h.m.Tick(context.Background())
	waitOrFail(t, entered, "the update to download")
	h.m.Close()
	h.m.Wait()
	if !strings.Contains(h.eventText(), "runner update interrupted by shutdown") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	if u := h.m.Status().RunnerUpdate; !u.Queued || u.Running || current(t, h) != "2.337.0" {
		t.Fatalf("status %+v current %s", u, current(t, h))
	}
}

// An idle runner that takes a job, or cannot be checked or stopped, after the
// tick found ghr free means it was not free: the update waits for a later
// tick, keeping its queue and releasing the reservation.
func TestUpdateWaitsWhenAnIdleRunnerDoesNotStop(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(h *harness, id string)
	}{
		{"took a job", func(h *harness, id string) {
			rid := h.m.insts[id].RunnerID
			h.gh.hook = func(m string) {
				if m == "ListRuns" {
					h.gh.setRunner(rid, "online", true)
				}
			}
		}},
		{"check failed", func(h *harness, _ string) {
			h.gh.hook = func(m string) {
				if m == "ListRuns" {
					h.gh.setErr("GetRunner darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
				}
			}
		}},
		{"stop failed", func(h *harness, id string) { h.sd.stopErr[UnitPrefix+id] = errors.New("unit busy") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			_, fetched := queuedUpdate(t, h, tarballSum())
			id := idleRunner(t, h)
			c.setup(h, id)
			h.m.Tick(context.Background())
			h.m.Wait()
			if strings.Contains(h.eventText(), "runner update started") || len(*fetched) != 0 {
				t.Fatalf("the update started:\n%s", h.eventText())
			}
			if len(h.sd.stopped) != 0 {
				t.Fatalf("stopped %v", h.sd.stopped)
			}
			if u := h.m.Status().RunnerUpdate; !u.Queued || u.Running {
				t.Fatalf("status %+v", u)
			}
			if err := h.m.StartPrune(); err != nil {
				t.Fatalf("the reservation was kept: %v", err)
			}
			h.m.Wait()
		})
	}
}

// A config change during the demand poll defers the update to the next tick,
// whose poll covers the new config.
func TestConfigChangeDuringPollDefersTheUpdate(t *testing.T) {
	h := newHarness(t)
	_, fetched := queuedUpdate(t, h, tarballSum())
	h.gh.hook = func(m string) {
		if m != "ListRuns" {
			return
		}
		h.gh.mu.Lock()
		h.gh.hook = nil
		h.gh.mu.Unlock()
		c := *h.cfg
		h.cfg = &c
	}
	h.m.Tick(context.Background())
	h.m.Wait()
	if strings.Contains(h.eventText(), "runner update started") || len(*fetched) != 0 {
		t.Fatalf("the update started on a stale poll:\n%s", h.eventText())
	}
	h.m.Tick(context.Background())
	h.m.Wait()
	if current(t, h) != "2.338.0" {
		t.Fatalf("the next tick did not update:\n%s", h.eventText())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/runner/ -run 'QueuedUpdate|NoSpawn|ShutdownInterrupts|ConfigChange'`
Expected: FAIL: `TestQueuedUpdateRunsWhenFree` reports `current 2.337.0`, `TestNoSpawnWhileUpdating` and `TestShutdownInterruptsTheUpdate` report `timed out waiting for the update to download`, and `TestConfigChangeDuringPollDefersTheUpdate` reports `the next tick did not update` (nothing starts the update yet). `TestUpdateWaitsWhenAnIdleRunnerDoesNotStop` passes before the implementation, because nothing starts an update; Step 8's mutation check is what proves it.

- [ ] **Step 3: Make demand gathering report completeness and pause spawning** (`internal/runner/tick.go`)

In `Tick`, replace:

```go
	if m.apiAllowed(now) {
		m.gatherDemand(ctx, cfg, now)
	}
```

with:

```go
	demandOK := false
	if m.apiAllowed(now) {
		demandOK = m.gatherDemand(ctx, cfg, now)
	}
```

and replace:

```go
	m.stopStartTimedOut(ctx, cfg, now)
	if m.apiAllowed(now) && !m.isDegraded() {
		m.spawnPlanned(ctx, cfg, now)
	}
```

with:

```go
	m.stopStartTimedOut(ctx, cfg, now)
	m.startUpdateIfFree(cfg, now, demandOK)
	if m.apiAllowed(now) && !m.isDegraded() {
		m.spawnPlanned(ctx, cfg, now)
	}
```

Replace the `gatherDemand` comment and signature:

```go
// gatherDemand lists matching queued jobs per repo and confirms our in-progress
// jobs. Degraded clears only after a tick in which some repo answered and none
// failed authentication.
func (m *Manager) gatherDemand(ctx context.Context, cfg *config.Config, now time.Time) {
```

with:

```go
// gatherDemand lists matching queued jobs per repo and confirms our in-progress
// jobs. Degraded clears only after a tick in which some repo answered and none
// failed authentication. It reports whether every unpaused repo answered.
func (m *Manager) gatherDemand(ctx context.Context, cfg *config.Config, now time.Time) bool {
```

In its body, replace `anyOK, authFailed := false, false` with:

```go
	anyOK, authFailed, complete := false, false, true
```

replace:

```go
		if wait {
			continue
		}
		jobs, err := m.repoDemand(ctx, cfg, r, ours, now)
		if err != nil {
```

with:

```go
		if wait {
			complete = false
			continue
		}
		jobs, err := m.repoDemand(ctx, cfg, r, ours, now)
		if err != nil {
			complete = false
```

and replace its end:

```go
	if recovered {
		m.Events.Add("ok", "", "GitHub token accepted again")
	}
}
```

with:

```go
	if recovered {
		m.Events.Add("ok", "", "GitHub token accepted again")
	}
	return complete
}
```

Replace the `spawnPlanned` comment and first lines:

```go
// spawnPlanned spawns what sched.Plan decides. Repos in an error state are left
// out of planning (no warm spawns into a failing repo) until a demand poll
// succeeds. Spawning stops as soon as the live config differs from the one the
// plan used, so a pause or cap change made during polling is never overridden.
func (m *Manager) spawnPlanned(ctx context.Context, cfg *config.Config, now time.Time) {
	m.mu.Lock()
	demand := m.demand
```

with:

```go
// spawnPlanned spawns what sched.Plan decides, nothing while a runner update
// runs. Repos in an error state are left out of planning (no warm spawns into
// a failing repo) until a demand poll succeeds. Spawning stops as soon as the
// live config differs from the one the plan used, so a pause or cap change
// made during polling is never overridden.
func (m *Manager) spawnPlanned(ctx context.Context, cfg *config.Config, now time.Time) {
	m.mu.Lock()
	if m.upd.running {
		m.mu.Unlock()
		return
	}
	demand := m.demand
```

- [ ] **Step 4: Share the idle-stop path** (`internal/runner/lifecycle.go`)

Replace the whole `stopIdle` function (its comment included, lines 505-534) with:

```go
// stopIdle stops idle runners chosen by sched.IdleToStop.
func (m *Manager) stopIdle(ctx context.Context, cfg *config.Config, now time.Time) {
	byID := map[string]instance{}
	for _, i := range m.snapshot() {
		byID[i.ID] = i
	}
	for _, id := range sched.IdleToStop(cfg, m.schedInstances(cfg), now) {
		if m.stopIdleRunner(ctx, byID[id], now) == idleKept && !m.apiAllowed(now) {
			return
		}
	}
}

// idleStop is what stopIdleRunner did with an idle runner.
type idleStop int

const (
	idleStopped idleStop = iota
	idleBusy             // the runners API reported a job; it is busy now
	idleKept             // the check or the stop failed; it keeps running
)

// stopIdleRunner stops i only after the runners API affirms it is not busy;
// any error leaves it running.
func (m *Manager) stopIdleRunner(ctx context.Context, i instance, now time.Time) idleStop {
	r, err := m.GH.GetRunner(ctx, i.Repo, i.RunnerID)
	if err != nil {
		if !github.IsKind(err, github.ErrNotFound) {
			m.apiErr(i.Repo, err, now)
		}
		return idleKept
	}
	if r.Busy {
		m.setState(i.ID, sched.Busy)
		return idleBusy
	}
	if err := m.SD.Stop(ctx, UnitPrefix+i.ID); err != nil {
		m.Events.Add("warn", i.Repo, "stop idle %s: %v", i.ID, err)
		return idleKept
	}
	m.Events.Add("info", i.Repo, "stopped idle runner %s", i.ID)
	return idleStopped
}
```

- [ ] **Step 5: Refuse a manual prune during an update** (`internal/runner/manager.go`)

Replace:

```go
// ErrPruneRunning is returned by StartPrune while a prune runs.
```

with:

```go
// ErrPruneRunning is returned by StartPrune while a prune runs; a runner
// update holds the same reservation and gets ErrUpdateRunning instead.
```

In `StartPrune`, replace:

```go
	if m.pruning {
		m.mu.Unlock()
		return ErrPruneRunning
	}
	now := m.Now()
```

with:

```go
	if m.upd.running {
		m.mu.Unlock()
		return ErrUpdateRunning
	}
	if m.pruning {
		m.mu.Unlock()
		return ErrPruneRunning
	}
	now := m.Now()
```

- [ ] **Step 6: Start the update** (`internal/runner/update.go`)

Replace the import block's last three lines:

```go
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)
```

with:

```go
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)
```

Append at the end of the file:

```go

// updateTimeout bounds one runner update.
const updateTimeout = 10 * time.Minute

// startUpdateIfFree starts a queued runner update when ghr is free: no
// instance starting or busy, this tick's demand poll (made with cfg, still the
// live config) answered for every unpaused repo with no queued job, the API
// allowed and not degraded, and the maintenance reservation free. Every idle
// runner is stopped first and comes back on the new version; one that took a
// job, or could not be checked or stopped, means ghr is not free after all.
// Spawning waits until the update ends.
func (m *Manager) startUpdateIfFree(cfg *config.Config, now time.Time, demandOK bool) {
	if !demandOK || m.Config() != cfg || !m.apiAllowed(now) || m.isDegraded() {
		return
	}
	m.fileMu.Lock()
	m.mu.Lock()
	free := m.upd.file.QueuedAt != nil && !m.upd.running && !m.closed && !m.pruning
	for _, i := range m.insts {
		if i.State == sched.Starting || i.State == sched.Busy {
			free = false
		}
	}
	for _, jobs := range m.demand {
		if len(jobs) > 0 {
			free = false
		}
	}
	if !free {
		m.mu.Unlock()
		m.fileMu.Unlock()
		return
	}
	m.pruning, m.upd.running = true, true
	ctx, cancel := context.WithTimeout(m.maintCtx, updateTimeout)
	m.wg.Add(1)
	m.mu.Unlock()
	m.fileMu.Unlock()
	finish := func() {
		m.mu.Lock()
		m.pruning, m.upd.running = false, false
		m.mu.Unlock()
		cancel()
		m.wg.Done()
	}
	for _, i := range m.snapshot() {
		if i.State == sched.Idle && m.stopIdleRunner(ctx, i, now) != idleStopped {
			finish()
			return
		}
	}
	m.Events.Add("info", "", "runner update started")
	go func() {
		defer finish()
		m.runUpdate(ctx)
	}()
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `timeout 400 go test ./... && timeout 120 gofmt -l . && timeout 300 go vet ./...`
Expected: every package `ok`, including the six new tests and every subtest of `TestQueuedUpdateWaitsUntilFree` and `TestUpdateWaitsWhenAnIdleRunnerDoesNotStop`; nothing from `gofmt` or `vet`.

- [ ] **Step 8: Check that each blocker is load-bearing**

Temporarily change `if len(jobs) > 0 {` in `startUpdateIfFree` to `if len(jobs) > 99 {` and run `timeout 300 go test ./internal/runner/ -run TestQueuedUpdateWaitsUntilFree`: the `queued_job` subtest must FAIL. Restore the line, then temporarily change `return complete` in `gatherDemand` to `_ = complete` followed by `return true`: the `demand_poll_failed` subtest must FAIL. Restore it, then temporarily change `!= idleStopped {` in `startUpdateIfFree` to `== -1 {`: `TestUpdateWaitsWhenAnIdleRunnerDoesNotStop` must FAIL. Restore it, then temporarily delete `m.Config() != cfg || ` from its first condition: `TestConfigChangeDuringPollDefersTheUpdate` must FAIL. Restore it and run the package again: `ok`. Run `git diff --stat` to confirm only the intended files changed.

- [ ] **Step 9: Commit**

```bash
git add internal/runner/tick.go internal/runner/lifecycle.go internal/runner/manager.go internal/runner/update.go internal/runner/update_start_test.go
git commit -m "feat(runner): start a queued update when free"
```

### Task 7: Runner update endpoints

**Files:**
- Modify: `internal/daemon/backend.go` (`Manager` interface lines 23-31, `Prune` lines 403-413; append `QueueRunnerUpdate`, `CancelRunnerUpdate`)
- Modify: `internal/daemon/run.go` (`Options` seams lines 35-40, `DefaultOptions` paths lines 50-58, `Run` manager wiring lines 129-141)
- Modify: `internal/api/server.go` (`Backend` interface lines 56-78; routes after `POST /prune`, line 197)
- Modify: `internal/api/client.go` (append two methods)
- Test: `internal/daemon/update_test.go` (new), `internal/daemon/backend_test.go` (`fakeManager`), `internal/api/update_test.go` (new), `internal/api/api_test.go` (`fakeBackend`)

**Interfaces:**
- Consumes: C5 (`QueueUpdate`, `CancelUpdate`, `ErrUpdateRunning`, `ErrNoDist`, `UpToDateError`, `ErrClosed`), C2 `system.Download`, C3 `Manager.Fetch` and `Paths.UpdateState`; existing `Backend.degradedErr` (`internal/daemon/backend.go:455`), `api.FromGitHub`, `respond`.
- Produces: C6 for `/runner-update`.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

A successful queue wakes the run loop, so a free ghr starts the update on the next tick rather than a poll interval later.

- [ ] **Step 1: Extend the fakes**

In `internal/daemon/backend_test.go`, replace the `fakeManager` struct:

```go
type fakeManager struct {
	insts    []model.InstanceStatus
	cleared  bool
	killed   []string
	pruneErr error
	prunes   int
	degraded string
}
```

with:

```go
type fakeManager struct {
	insts     []model.InstanceStatus
	cleared   bool
	killed    []string
	pruneErr  error
	prunes    int
	degraded  string
	updateErr error // returned by QueueUpdate and CancelUpdate
	updates   []string
}

func (f *fakeManager) QueueUpdate(context.Context) error {
	f.updates = append(f.updates, "queue")
	return f.updateErr
}

func (f *fakeManager) CancelUpdate() error {
	f.updates = append(f.updates, "cancel")
	return f.updateErr
}
```

In `internal/api/api_test.go`, replace the end of the `fakeBackend` struct:

```go
	metrics      model.Metrics
	metricsCalls int
}
```

with:

```go
	metrics      model.Metrics
	metricsCalls int
	updates      []string
	updateErr    error
}

func (f *fakeBackend) QueueRunnerUpdate(context.Context) error {
	f.updates = append(f.updates, "queue")
	return f.updateErr
}

func (f *fakeBackend) CancelRunnerUpdate() error {
	f.updates = append(f.updates, "cancel")
	return f.updateErr
}
```

- [ ] **Step 2: Write the failing tests**

`internal/daemon/update_test.go`:

```go
package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/runner"
)

func TestQueueRunnerUpdate(t *testing.T) {
	b, m, _ := newBackend(t)
	woke := 0
	b.Wake = func() { woke++ }
	if err := b.QueueRunnerUpdate(context.Background()); err != nil || woke != 1 {
		t.Fatalf("queue: %v, woke %d", err, woke)
	}
	for _, c := range []struct {
		err  error
		want int
	}{
		{runner.UpToDateError("2.337.0"), 409},
		{runner.ErrUpdateRunning, 409},
		{runner.ErrNoDist, 409},
		{runner.ErrClosed, 503},
	} {
		m.updateErr = c.err
		err := b.QueueRunnerUpdate(context.Background())
		if apiStatus(err) != c.want || err.Error() != c.err.Error() {
			t.Errorf("%v: got %v (%d), want %d", c.err, err, apiStatus(err), c.want)
		}
	}
	limit := &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: time.Now().Add(time.Minute)}
	m.updateErr = limit
	if err := b.QueueRunnerUpdate(context.Background()); !errors.Is(err, limit) {
		t.Fatalf("a GitHub error must reach the API unchanged: %v", err)
	}
	if woke != 1 {
		t.Fatalf("a refused queue woke the loop")
	}

	calls := len(m.updates)
	m.degraded = "GitHub rejected the token"
	if err := b.QueueRunnerUpdate(context.Background()); apiStatus(err) != 503 || len(m.updates) != calls {
		t.Fatalf("degraded: %v, manager called %v", err, m.updates)
	}
}

func TestCancelRunnerUpdate(t *testing.T) {
	b, m, _ := newBackend(t)
	if err := b.CancelRunnerUpdate(); err != nil {
		t.Fatal(err)
	}
	m.updateErr = runner.ErrUpdateRunning
	if err := b.CancelRunnerUpdate(); apiStatus(err) != 409 {
		t.Fatalf("running: %v", err)
	}
	if got := strings.Join(m.updates, ","); got != "cancel,cancel" {
		t.Fatalf("calls %s", got)
	}
}

func TestPruneRefusedWhileUpdating(t *testing.T) {
	b, m, _ := newBackend(t)
	m.pruneErr = runner.ErrUpdateRunning
	err := b.Prune()
	var ae *api.Error
	if !errors.As(err, &ae) || ae.Status != 409 || ae.Msg != "a runner update is running" {
		t.Fatalf("prune: %v", err)
	}
}
```

`internal/api/update_test.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
)

func TestRunnerUpdateRoutes(t *testing.T) {
	c, b := setup(t)
	if err := c.QueueRunnerUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.CancelRunnerUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(b.updates, ","); got != "queue,cancel" {
		t.Fatalf("calls %s", got)
	}
	var ae *Error
	b.updateErr = Conflict("runner 2.337.0 is already up to date")
	if err := c.QueueRunnerUpdate(context.Background()); !errors.As(err, &ae) || ae.Status != 409 || ae.Msg != "runner 2.337.0 is already up to date" {
		t.Fatalf("up to date: %v", err)
	}
	if err := c.CancelRunnerUpdate(context.Background()); !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("cancel refused: %v", err)
	}
	retry := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	b.updateErr = &github.APIError{Status: 403, Kind: github.ErrRateLimit, Message: "API rate limit exceeded", RetryAt: retry}
	if err := c.QueueRunnerUpdate(context.Background()); !errors.As(err, &ae) || ae.Status != 429 || !ae.RetryAt.Equal(retry) {
		t.Fatalf("rate limit: %v", err)
	}
}

// The routes answer with the statuses the spec names, which the client alone
// cannot tell apart.
func TestRunnerUpdateStatusCodes(t *testing.T) {
	c, b := setup(t)
	do := func(method string) int {
		t.Helper()
		req, err := http.NewRequest(method, c.Base+"/runner-update", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, s := range []struct {
		method string
		err    error
		want   int
	}{
		{http.MethodPost, nil, 202},
		{http.MethodPost, nil, 202}, // already queued
		{http.MethodDelete, nil, 204},
		{http.MethodDelete, nil, 204}, // nothing queued
		{http.MethodPost, Conflict("runner 2.337.0 is already up to date"), 409},
		{http.MethodPost, Conflict("a runner update is running"), 409},
		{http.MethodDelete, Conflict("a runner update is running"), 409},
		{http.MethodPost, &Error{Status: http.StatusServiceUnavailable, Msg: "GitHub is rejecting the token"}, 503},
	} {
		b.updateErr = s.err
		if got := do(s.method); got != s.want {
			t.Errorf("%s with %v: %d, want %d", s.method, s.err, got, s.want)
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ ./internal/api/`
Expected: FAIL to build with `b.QueueRunnerUpdate undefined` (daemon) and `c.QueueRunnerUpdate undefined` (api).

- [ ] **Step 4: Write the backend** (`internal/daemon/backend.go`)

Add to the `Manager` interface, after `StartPrune() error`:

```go
	QueueUpdate(ctx context.Context) error
	CancelUpdate() error
```

In `Prune`, replace:

```go
	if errors.Is(err, runner.ErrPruneRunning) {
```

with:

```go
	if errors.Is(err, runner.ErrPruneRunning) || errors.Is(err, runner.ErrUpdateRunning) {
```

Append at the end of the file:

```go

// QueueRunnerUpdate checks GitHub for a newer runner and queues its install;
// the loop is woken so a free ghr starts it at once.
func (b *Backend) QueueRunnerUpdate(ctx context.Context) error {
	if err := b.degradedErr(); err != nil {
		return err
	}
	err := b.M.QueueUpdate(ctx)
	var current runner.UpToDateError
	switch {
	case err == nil:
		if b.Wake != nil {
			b.Wake()
		}
		return nil
	case errors.As(err, &current), errors.Is(err, runner.ErrUpdateRunning), errors.Is(err, runner.ErrNoDist):
		return api.Conflict(err.Error())
	case errors.Is(err, runner.ErrClosed):
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: err.Error()}
	}
	return err
}

// CancelRunnerUpdate drops a queued runner update.
func (b *Backend) CancelRunnerUpdate() error {
	err := b.M.CancelUpdate()
	if errors.Is(err, runner.ErrUpdateRunning) {
		return api.Conflict(err.Error())
	}
	return err
}
```

- [ ] **Step 5: Wire the daemon** (`internal/daemon/run.go`)

Replace the seams at the end of `Options`:

```go
	// Test seams: zero values use GitHub, systemd, Docker, the host and SIGHUP.
	GitHubURL string
	Systemd   runner.Systemd
	Docker    runner.Docker
	Host      runner.Host
	Reload    <-chan os.Signal
}
```

with:

```go
	// Test seams: zero values use GitHub, systemd, Docker, the host, an HTTP
	// download and SIGHUP.
	GitHubURL string
	Systemd   runner.Systemd
	Docker    runner.Docker
	Host      runner.Host
	Fetch     func(ctx context.Context, url, dst string) error
	Reload    <-chan os.Signal
}
```

In `DefaultOptions`, replace the `Paths` literal:

```go
		Paths: runner.Paths{
			Dist:      "/opt/ghr/dist/current",
			Instances: "/var/lib/ghr/instances",
			Logs:      "/var/lib/ghr/logs",
			Pending:   "/var/lib/ghr/pending",
			ToolCache: "/var/lib/ghr/toolcache",
			Hooks:     "/opt/ghr/hooks",
			Home:      "/home/" + runner.RunnerUser,
		},
```

with:

```go
		Paths: runner.Paths{
			Dist:        "/opt/ghr/dist/current",
			Instances:   "/var/lib/ghr/instances",
			Logs:        "/var/lib/ghr/logs",
			Pending:     "/var/lib/ghr/pending",
			ToolCache:   "/var/lib/ghr/toolcache",
			Hooks:       "/opt/ghr/hooks",
			Home:        "/home/" + runner.RunnerUser,
			UpdateState: "/var/lib/ghr/runner-update.json",
		},
```

In `Run`, replace:

```go
		Config: store.Config, GH: gh, SD: o.Systemd, Docker: o.Docker, Host: o.Host,
```

with:

```go
		Config: store.Config, GH: gh, SD: o.Systemd, Docker: o.Docker, Host: o.Host, Fetch: o.Fetch,
```

and replace:

```go
	if m.Host == nil {
		m.Host = system.Host{Run: system.Exec, Script: system.ExecGroup}
	}
```

with:

```go
	if m.Host == nil {
		m.Host = system.Host{Run: system.Exec, Script: system.ExecGroup}
	}
	if m.Fetch == nil {
		m.Fetch = system.Download
	}
```

- [ ] **Step 6: Add the routes and client methods**

In `internal/api/server.go`, add to the `Backend` interface, after `DeleteRegistration(ctx context.Context, repo string, id int64) error`:

```go
	QueueRunnerUpdate(ctx context.Context) error
	CancelRunnerUpdate() error
```

In `NewServer`, after the `POST /prune` handler and before `return mux`, add:

```go
	mux.HandleFunc("POST /runner-update", func(w http.ResponseWriter, r *http.Request) {
		if err := b.QueueRunnerUpdate(r.Context()); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("DELETE /runner-update", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.CancelRunnerUpdate())
	})
```

Append to `internal/api/client.go`:

```go

// QueueRunnerUpdate asks the daemon to install the latest runner once no job
// is running or queued.
func (c *Client) QueueRunnerUpdate(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/runner-update", nil, nil)
}

// CancelRunnerUpdate drops a queued runner update.
func (c *Client) CancelRunnerUpdate(ctx context.Context) error {
	return c.call(ctx, http.MethodDelete, "/runner-update", nil, nil)
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `timeout 400 go test ./... && timeout 120 gofmt -l . && timeout 300 go vet ./...`
Expected: every package `ok`; nothing from `gofmt` or `vet`.

- [ ] **Step 8: Commit**

```bash
git add internal/daemon/backend.go internal/daemon/run.go internal/daemon/update_test.go internal/daemon/backend_test.go internal/api/server.go internal/api/client.go internal/api/update_test.go internal/api/api_test.go
git commit -m "feat(api): queue and cancel runner updates"
```

### Task 8: Available repositories endpoint

**Files:**
- Modify: `internal/model/model.go` (append `AvailableRepo`)
- Modify: `internal/daemon/backend.go` (import `sort`; `GitHub` interface lines 34-44; append `AvailableRepos`)
- Modify: `internal/api/server.go` (`Backend` interface; the route before `DELETE /repos/{name}`, line 139)
- Modify: `internal/api/client.go` (append one method)
- Test: `internal/daemon/available_test.go` (new), `internal/daemon/backend_test.go` (`fakeGH`), `internal/api/update_test.go` (imports and one test), `internal/api/api_test.go` (`fakeBackend`)

**Interfaces:**
- Consumes: C1 `ListUserRepos`, `UserRepo`, `Account`; C6 from Task 7 (`internal/api/update_test.go`, `fakeBackend.updates` and `updateErr`); existing `Backend.degradedErr`, `config.Config.Repo` (case-insensitive, `internal/config/config.go:217`).
- Produces: C4 `AvailableRepo`; C6 for `/repos/available`.

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Extend the fakes**

In `internal/daemon/backend_test.go`, in the `fakeGH` struct, replace:

```go
	recent    []github.Run
```

with:

```go
	userRepos []github.UserRepo
	repoErr   error // returned by ListUserRepos
	recent    []github.Run
```

and after `func (f *fakeGH) TokenMeta() github.TokenMeta { return f.meta }` add:

```go

func (f *fakeGH) ListUserRepos(context.Context) ([]github.UserRepo, error) {
	return f.userRepos, f.repoErr
}
```

In `internal/api/api_test.go`, replace the end of the `fakeBackend` struct (as Task 7 left it):

```go
	updates      []string
	updateErr    error
}
```

with:

```go
	updates      []string
	updateErr    error
	avail        []model.AvailableRepo
	availErr     error
}

func (f *fakeBackend) AvailableRepos(context.Context) ([]model.AvailableRepo, error) {
	return f.avail, f.availErr
}
```

- [ ] **Step 2: Write the failing tests**

`internal/daemon/available_test.go`:

```go
package daemon

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func TestAvailableReposKeepsTheOwnersSorted(t *testing.T) {
	b, m, gh := newBackend(t)
	gh.userRepos = []github.UserRepo{
		{Name: "zeta", Private: true, Owner: github.Account{Login: "darkraise"}},
		{Name: "DarkCloud", Private: true, Owner: github.Account{Login: "DarkRaise"}},
		{Name: "elsewhere", Private: true, Owner: github.Account{Login: "someone"}},
		{Name: "booklore", Private: false, Owner: github.Account{Login: "darkraise"}},
	}
	got, err := b.AvailableRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []model.AvailableRepo{
		{Name: "booklore", Private: false},
		{Name: "DarkCloud", Private: true, Configured: true},
		{Name: "zeta", Private: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}

	gh.userRepos = nil
	if got, err := b.AvailableRepos(context.Background()); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("no repos: %#v %v", got, err)
	}
	gh.repoErr = &github.APIError{Status: 502, Kind: github.ErrServer}
	if _, err := b.AvailableRepos(context.Background()); !errors.Is(err, gh.repoErr) {
		t.Fatalf("GitHub error: %v", err)
	}
	m.degraded = "GitHub rejected the token"
	if _, err := b.AvailableRepos(context.Background()); apiStatus(err) != 503 {
		t.Fatalf("degraded: %v", err)
	}
}
```

In `internal/api/update_test.go`, replace the import block with:

```go
import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)
```

and append:

```go

func TestAvailableReposRoute(t *testing.T) {
	c, b := setup(t)
	b.avail = []model.AvailableRepo{{Name: "booklore"}, {Name: "darkcloud", Private: true, Configured: true}}
	got, err := c.AvailableRepos(context.Background())
	if err != nil || !reflect.DeepEqual(got, b.avail) {
		t.Fatalf("got %+v %v", got, err)
	}
	b.avail = nil
	if got, err := c.AvailableRepos(context.Background()); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty list must decode as []: %#v %v", got, err)
	}
	b.availErr = &Error{Status: 503, Msg: "GitHub is rejecting the token"}
	var ae *Error
	if _, err := c.AvailableRepos(context.Background()); !errors.As(err, &ae) || ae.Status != 503 {
		t.Fatalf("degraded: %v", err)
	}
}
```

The test backend in `internal/daemon` reads `cfgYAML` (`internal/daemon/store_test.go:12`): owner `darkraise`, repos `darkcloud` and `darkmem`.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ ./internal/api/`
Expected: FAIL to build with `undefined: model.AvailableRepo`.

- [ ] **Step 4: Write the implementation**

Append to `internal/model/model.go`:

```go

// AvailableRepo is a repository of the configured owner that the token can
// access, served by GET /repos/available.
type AvailableRepo struct {
	Name       string `json:"name"`
	Private    bool   `json:"private"`
	Configured bool   `json:"configured"`
}
```

In `internal/daemon/backend.go`, add `"sort"` to the imports, between `"regexp"` and `"strings"`. Add to the `GitHub` interface, after `ListRecentRuns(ctx context.Context, repo string, n int) ([]github.Run, error)`:

```go
	ListUserRepos(ctx context.Context) ([]github.UserRepo, error)
```

Append at the end of the file:

```go

// AvailableRepos lists the configured owner's repositories the token can
// access, by name ignoring case, marking those already configured. The token
// may also reach other owners' repositories; ghr cannot manage those.
func (b *Backend) AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error) {
	if err := b.degradedErr(); err != nil {
		return nil, err
	}
	rs, err := b.GH.ListUserRepos(ctx)
	if err != nil {
		return nil, err
	}
	cfg := b.Store.Config()
	out := []model.AvailableRepo{}
	for _, r := range rs {
		if strings.EqualFold(r.Owner.Login, cfg.Owner) {
			out = append(out, model.AvailableRepo{Name: r.Name, Private: r.Private, Configured: cfg.Repo(r.Name) != nil})
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}
```

In `internal/api/server.go`, add to the `Backend` interface, after `CancelRunnerUpdate() error`:

```go
	AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error)
```

In `NewServer`, immediately before the `DELETE /repos/{name}` handler, add:

```go
	mux.HandleFunc("GET /repos/available", func(w http.ResponseWriter, r *http.Request) {
		rs, err := b.AvailableRepos(r.Context())
		if rs == nil {
			rs = []model.AvailableRepo{}
		}
		respond(w, rs, err)
	})
```

Append to `internal/api/client.go`:

```go

// AvailableRepos lists the owner's repositories the token can access.
func (c *Client) AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error) {
	var rs []model.AvailableRepo
	err := c.call(ctx, http.MethodGet, "/repos/available", nil, &rs)
	return rs, err
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 400 go test ./... && timeout 120 gofmt -l . && timeout 300 go vet ./...`
Expected: every package `ok`; nothing from `gofmt` or `vet`.

- [ ] **Step 6: Commit**

```bash
git add internal/model/model.go internal/daemon/backend.go internal/daemon/available_test.go internal/daemon/backend_test.go internal/api/server.go internal/api/client.go internal/api/update_test.go internal/api/api_test.go
git commit -m "feat(api): list repositories the token can add"
```

### Task 9: CLI runner-update and status line

**Files:**
- Modify: `cmd/ghr/cli.go` (a `runner-update` case in `cli`, lines 89-91; the runner line in `printStatus`, line 249; append `runnerLine`, `daysLeft`)
- Modify: `cmd/ghr/main.go` (usage, line 114)
- Test: `cmd/ghr/update_test.go` (new)

**Interfaces:**
- Consumes: C4 `RunnerUpdate`; C6 `api.Client.QueueRunnerUpdate`, `CancelRunnerUpdate`; the test helpers `fakeDaemon` and `runCLI` (`cmd/ghr/cli_test.go:23,64`), whose default answer is 204.
- Produces: `func runnerLine(st model.Status) string`, `func daysLeft(deadline, now time.Time) string`.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests** (`cmd/ghr/update_test.go`)

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

func TestRunnerUpdateCommands(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, errOut := runCLI(t, "", "runner-update")
	if code != 0 || out != "runner update queued; it runs when no job is running or queued\n" {
		t.Fatalf("queue: exit %d out %q err %q", code, out, errOut)
	}
	if code, out, errOut := runCLI(t, "", "runner-update", "--cancel"); code != 0 || out != "" {
		t.Fatalf("cancel: exit %d out %q err %q", code, out, errOut)
	}
	if code, _, errOut := runCLI(t, "", "runner-update", "--now"); code != 2 || !strings.Contains(errOut, "usage: ghr runner-update [--cancel]") {
		t.Fatalf("bad flag: exit %d err %q", code, errOut)
	}
	var got []string
	for _, r := range *reqs {
		got = append(got, r.method+" "+r.path)
	}
	if strings.Join(got, "|") != "POST /runner-update|DELETE /runner-update" {
		t.Fatalf("requests %v", got)
	}
}

func TestRunnerUpdateReportsRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "runner 2.337.0 is already up to date"})
	}))
	t.Cleanup(srv.Close)
	newClient = func() *api.Client { return &api.Client{Base: srv.URL, HTTP: srv.Client()} }
	if code, _, errOut := runCLI(t, "", "runner-update"); code != 1 || errOut != "ghr: runner 2.337.0 is already up to date\n" {
		t.Fatalf("exit %d err %q", code, errOut)
	}
}

func TestRunnerLine(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	for _, c := range []struct {
		u    model.RunnerUpdate
		want string
	}{
		{model.RunnerUpdate{}, "runner version unknown (no dist/current)"},
		{model.RunnerUpdate{CheckError: "github: 502"}, "runner version unknown (no dist/current)  last check failed: github: 502"},
		{model.RunnerUpdate{Installed: "2.337.0"}, "runner 2.337.0  checking…"},
		{model.RunnerUpdate{Installed: "2.337.0", Latest: "2.337.0", CheckedAt: at(-2 * time.Hour)}, "runner 2.337.0  up to date (checked 2h0m0s ago)"},
		{model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", CheckedAt: at(0), Deadline: at(30 * 24 * time.Hour)},
			"runner 2.337.0 → 2.338.0  update available, update by " + now.Add(30*24*time.Hour).Local().Format(time.DateOnly) + " (30 days)"},
		{model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", CheckedAt: at(0), Deadline: at(-time.Hour), Queued: true},
			"runner 2.337.0 → 2.338.0  update available, update by " + now.Add(-time.Hour).Local().Format(time.DateOnly) + " (overdue)  queued: runs when no job is running or queued"},
		{model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", CheckedAt: at(0), Deadline: at(time.Hour), Queued: true, Running: true},
			"runner 2.337.0 → 2.338.0  update available, update by " + now.Add(time.Hour).Local().Format(time.DateOnly) + " (0 days)  updating"},
		{model.RunnerUpdate{Installed: "2.337.0", CheckError: "github: 502 bad gateway"}, "runner 2.337.0  last check failed: github: 502 bad gateway"},
	} {
		if got := runnerLine(model.Status{Now: now, RunnerUpdate: c.u}); got != c.want {
			t.Errorf("%+v:\n got %q\nwant %q", c.u, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 200 go test ./cmd/ghr/`
Expected: FAIL to build with `undefined: runnerLine`.

- [ ] **Step 3: Write the implementation**

In `cmd/ghr/cli.go` (`cli`), replace:

```go
	case "history":
		return historyCmd(ctx, c, args[1:], out)
	}
```

with:

```go
	case "history":
		return historyCmd(ctx, c, args[1:], out)
	case "runner-update":
		switch {
		case len(args) == 1:
			if err := c.QueueRunnerUpdate(ctx); err != nil {
				return err
			}
			fmt.Fprintln(out, "runner update queued; it runs when no job is running or queued")
			return nil
		case len(args) == 2 && args[1] == "--cancel":
			return c.CancelRunnerUpdate(ctx)
		}
		return usageError("usage: ghr runner-update [--cancel]")
	}
```

In `printStatus`, after:

```go
	fmt.Fprintf(out, "mode %s  global %s  api %d  disk %d%%\n", st.Mode, global, st.RateRemaining, st.DiskPct)
```

add:

```go
	fmt.Fprintln(out, runnerLine(st))
```

Append to `cmd/ghr/cli.go`:

```go

// runnerLine is the status line about the GitHub Actions runner version.
func runnerLine(st model.Status) string {
	u := st.RunnerUpdate
	if u.Installed == "" {
		s := "runner version unknown (no dist/current)"
		if u.CheckError != "" {
			s += "  last check failed: " + u.CheckError
		}
		return s
	}
	s := "runner " + u.Installed
	switch {
	case u.Deadline != nil:
		s += fmt.Sprintf(" → %s  update available, update by %s (%s)", u.Latest, u.Deadline.Local().Format(time.DateOnly), daysLeft(*u.Deadline, st.Now))
	case u.CheckedAt != nil:
		s += fmt.Sprintf("  up to date (checked %s ago)", st.Now.Sub(*u.CheckedAt).Round(time.Minute))
	case u.CheckError == "":
		s += "  checking…"
	}
	switch {
	case u.Running:
		s += "  updating"
	case u.Queued:
		s += "  queued: runs when no job is running or queued"
	}
	if u.CheckError != "" {
		s += "  last check failed: " + u.CheckError
	}
	return s
}

// daysLeft is the time to deadline in whole days, or "overdue".
func daysLeft(deadline, now time.Time) string {
	left := deadline.Sub(now)
	if left < 0 {
		return "overdue"
	}
	return fmt.Sprintf("%d days", int(left.Hours()/24))
}
```

In `cmd/ghr/main.go` (`usage`), after the line `  history [--repo r] [--conclusion c] [--limit n]`, add:

```
  runner-update [--cancel]        queue (or cancel) a runner update
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 200 go test ./cmd/ghr/ && timeout 120 gofmt -l . && timeout 300 go vet ./cmd/...`
Expected: `ok  github.com/darkraise/ghr/cmd/ghr` (the existing `TestStatusOutput` still passes with the extra line); nothing from `gofmt` or `vet`.

- [ ] **Step 5: Commit**

```bash
git add cmd/ghr/cli.go cmd/ghr/main.go cmd/ghr/update_test.go
git commit -m "feat(cli): add runner-update and a status line"
```

### Task 10: Runner line in Settings and top-bar badge

**Files:**
- Modify: `internal/tui/model.go` (`Client` interface lines 20-45; `cleanStatus` line 542)
- Modify: `internal/tui/manage.go` (button IDs lines 23-34, `manageState` lines 38-46, `newManageState` lines 78-89, `maintenanceSection` lines 334-352; new `updateUrgent`, `runnerRows`, `queueRunnerUpdate`, `cancelRunnerUpdate`)
- Modify: `internal/tui/dialogs.go` (`pressed`, after `case setPrune:`, line 352)
- Modify: `internal/tui/shell.go` (`chips` end, lines 123-125; new `runnerBadge`)
- Test: `internal/tui/tui_test.go` (`fakeClient` fields and three methods), `internal/tui/runner_update_test.go` (new), `internal/tui/testdata/TestSettingsRunnerUpdateGolden/{120,80}.golden` (new)

**Interfaces:**
- Consumes: C4 `RunnerUpdate`; C6 client method names; existing `ui.Badge`, `ui.Row.Lines`, `ago` (`internal/tui/view.go:28`), `Model.action`, `Model.offline`, test helpers `onSettings`, `click`, `feed`, `fits`, `sampleModel`, `sampleStatus`.
- Produces: C7's Task 10 names; the three `tui.Client` methods and `fakeClient` fields that Task 12 uses.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The Runner row's text uses `ui.Row.Lines`, which wraps each line to the card and collapses runs of spaces, so the line is built with single spaces and the deadline or queue note goes on its own line.

- [ ] **Step 1: Extend the fake client** (`internal/tui/tui_test.go`)

Replace the end of the `fakeClient` struct:

```go
	metrics     model.Metrics
	metricsErr  error // returned by Metrics
}
```

with:

```go
	metrics     model.Metrics
	metricsErr  error // returned by Metrics
	updateErr   error // returned by QueueRunnerUpdate and CancelRunnerUpdate
	avail       []model.AvailableRepo
	availErr    error // returned by AvailableRepos
	availCalls  int
}
```

Replace:

```go
func (f *fakeClient) Metrics(context.Context) (model.Metrics, error) { return f.metrics, f.metricsErr }
```

with:

```go
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
```

- [ ] **Step 2: Write the failing tests** (`internal/tui/runner_update_test.go`)

```go
package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/muesli/termenv"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

func at(d time.Duration) *time.Time {
	t := now.Add(d)
	return &t
}

// available is 2.337.0 installed with 2.338.0 due in days.
func available(days int) model.RunnerUpdate {
	return model.RunnerUpdate{Installed: "2.337.0", Latest: "2.338.0", CheckedAt: at(-time.Hour),
		Deadline: at(time.Duration(days) * 24 * time.Hour)}
}

func withRunner(u model.RunnerUpdate) model.Status {
	st := sampleStatus()
	st.RunnerUpdate = u
	return st
}

func TestRunnerLineStates(t *testing.T) {
	queued, running := available(30), available(30)
	queued.Queued = true
	running.Queued, running.Running = true, true
	for _, c := range []struct {
		name   string
		u      model.RunnerUpdate
		want   []string
		button string
	}{
		{"unknown", model.RunnerUpdate{}, []string{"version unknown (no dist/current)"}, ""},
		{"before the first check", model.RunnerUpdate{Installed: "2.337.0"}, []string{"2.337.0 checking…"}, ""},
		{"current", model.RunnerUpdate{Installed: "2.337.0", Latest: "2.337.0", CheckedAt: at(-2 * time.Hour)},
			[]string{"2.337.0 [UP TO DATE] checked 2h ago"}, ""},
		{"available", available(30), []string{"2.337.0 → 2.338.0 [UPDATE AVAILABLE]", "update by 2026-11-02 (30 days)"}, "[ Queue update ]"},
		{"overdue", available(-1), []string{"update by 2026-10-02 (overdue)"}, "[ Queue update ]"},
		{"queued", queued, []string{"2.337.0 → 2.338.0 [QUEUED]", "runs when no job is running or queued"}, "( Cancel queued update )"},
		{"running", running, []string{"2.337.0 → 2.338.0 [UPDATING]"}, ""},
		{"check failed", model.RunnerUpdate{Installed: "2.337.0", CheckError: "github: 502 bad gateway"},
			[]string{"2.337.0", "last check failed: github: 502 bad gateway"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := feed(onSettings(t, &fakeClient{}, 140, 80), statusMsg{st: withRunner(c.u)})
			v := m.View()
			for _, want := range append([]string{"Runner"}, c.want...) {
				if !strings.Contains(v, want) {
					t.Fatalf("missing %q:\n%s", want, v)
				}
			}
			for _, b := range []string{"[ Queue update ]", "( Cancel queued update )"} {
				if strings.Contains(v, b) != (b == c.button) {
					t.Errorf("button %s shown %v, want only %q", b, strings.Contains(v, b), c.button)
				}
			}
		})
	}
}

func TestRunnerUpdateButtons(t *testing.T) {
	c := &fakeClient{}
	m := feed(onSettings(t, c, 140, 80), statusMsg{st: withRunner(available(30))})
	m = click(t, m, setRunnerQueue)
	if got := strings.Join(c.actions(), "|"); got != "runner-update" || !strings.Contains(m.View(), "runner update queued") {
		t.Fatalf("queue: %q\n%s", got, m.View())
	}
	u := available(30)
	u.Queued = true
	m = feed(m, statusMsg{st: withRunner(u)})
	m = click(t, m, setRunnerCancel)
	if got := strings.Join(c.actions(), "|"); got != "runner-update|runner-update cancel" || !strings.Contains(m.View(), "queued runner update cancelled") {
		t.Fatalf("cancel: %q\n%s", got, m.View())
	}
	c.updateErr = errors.New("runner 2.338.0 is already up to date")
	m = feed(m, statusMsg{st: withRunner(available(30))})
	if m = click(t, m, setRunnerQueue); !strings.Contains(m.View(), "runner 2.338.0 is already up to date") {
		t.Fatalf("refusal not shown:\n%s", m.View())
	}

	m = feed(m, statusMsg{err: errors.New("connection refused")})
	m.View()
	if m.mg.rnQueue.Focusable() {
		t.Fatal("Queue update enabled while the daemon is unreachable")
	}
	m.st = withRunner(u)
	m.View()
	if m.mg.rnCancel.Focusable() {
		t.Fatal("Cancel queued update enabled while the daemon is unreachable")
	}
}

func TestTopBarRunnerBadge(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 30)
	if strings.Contains(m.topBar(200), "RUNNER") {
		t.Fatal("badge shown without an update")
	}
	m = feed(m, statusMsg{st: withRunner(available(30))})
	bar := m.topBar(200)
	if i, j := strings.Index(bar, "disk "), strings.Index(bar, "[RUNNER ↑]"); i < 0 || j < i {
		t.Fatalf("badge missing or before the disk gauge: %q", bar)
	}
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	for _, c := range []struct {
		days int
		kind ui.BadgeKind
	}{{30, ui.BadgeWarn}, {8, ui.BadgeWarn}, {7, ui.BadgeBad}, {-2, ui.BadgeBad}} {
		m.st = withRunner(available(c.days))
		if got := m.runnerBadge(); got != ui.Badge("runner ↑", c.kind) {
			t.Errorf("%d days: %q", c.days, got)
		}
	}
}

func TestStatusRunnerTextIsCleaned(t *testing.T) {
	u := available(30)
	u.CheckError = "bad\x1b]0;x\a gateway"
	m := feed(sampleModel(&fakeClient{}, 120, 30), statusMsg{st: withRunner(u)})
	if strings.ContainsAny(m.st.RunnerUpdate.CheckError, "\x1b\a") {
		t.Fatalf("check error kept control characters: %q", m.st.RunnerUpdate.CheckError)
	}
}

func TestSettingsRunnerUpdateGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := feed(onSettings(t, &fakeClient{}, w, 80), statusMsg{st: withRunner(available(30))})
			golden.RequireEqual(t, []byte(m.View()))
		})
	}
}

// The Runner line wraps rather than overflowing the card, down to 40 columns.
func TestRunnerLineFitsNarrow(t *testing.T) {
	for _, w := range []int{80, 56, 40} {
		m := feed(onSettings(t, &fakeClient{}, w, 80), statusMsg{st: withRunner(available(30))})
		fits(t, "settings", m.View(), w, 80)
	}
}
```

The TUI tests' clock is `now` = 2026-10-03 14:05 UTC with `time.Local` set to UTC (`internal/tui/tui_test.go:25-30`), so 30 days ahead is 2026-11-02.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/tui/`
Expected: FAIL to build with `undefined: setRunnerQueue` (and the other new names).

- [ ] **Step 4: Add the client methods and clean the new status text** (`internal/tui/model.go`)

Add to the `Client` interface, after `Metrics(ctx context.Context) (model.Metrics, error)`:

```go
	QueueRunnerUpdate(ctx context.Context) error
	CancelRunnerUpdate(ctx context.Context) error
	AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error)
```

In `cleanStatus`, after its first line `st.Mode, st.DegradedReason = clean(st.Mode), clean(st.DegradedReason)`, add:

```go
	u := &st.RunnerUpdate
	u.Installed, u.Latest, u.CheckError, u.LastError = clean(u.Installed), clean(u.Latest), clean(u.CheckError), clean(u.LastError)
```

- [ ] **Step 5: Add the Runner rows and buttons** (`internal/tui/manage.go`)

In the button ID `const` block, after `setPrune        = "settings/prune"`, add:

```go
	setRunnerQueue  = "settings/runner/queue"
	setRunnerCancel = "settings/runner/cancel"
```

In `manageState`, after `prune    *ui.Button`, add:

```go
	rnQueue  *ui.Button
	rnCancel *ui.Button
```

In `newManageState`, after `prune:      ui.NewButton(setPrune, "Prune now", ui.Primary),`, add:

```go
		rnQueue:    ui.NewButton(setRunnerQueue, "Queue update", ui.Primary),
		rnCancel:   ui.NewButton(setRunnerCancel, "Cancel queued update", ui.Secondary),
```

Replace the `maintenanceSection` comment's second line `// prune, Reload and Prune now.` with:

```go
// prune, the runner version, Reload and Prune now.
```

and replace the end of `maintenanceSection`:

```go
	return ui.Section{Title: "Maintenance", Rows: []ui.Row{
		{Label: "Disk", Text: fmt.Sprintf("%d%% used", m.st.DiskPct)},
		{Label: "Prune", Text: last},
		{Items: []ui.Widget{m.mg.reload, m.mg.prune}},
	}}
}
```

with:

```go
	rows := []ui.Row{
		{Label: "Disk", Text: fmt.Sprintf("%d%% used", m.st.DiskPct)},
		{Label: "Prune", Text: last},
	}
	rows = append(rows, m.runnerRows()...)
	rows = append(rows, ui.Row{Items: []ui.Widget{m.mg.reload, m.mg.prune}})
	return ui.Section{Title: "Maintenance", Rows: rows}
}

// updateUrgent reports whether the runner update deadline is 7 days away or past.
func (m Model) updateUrgent() bool {
	d := m.st.RunnerUpdate.Deadline
	return d != nil && d.Sub(m.now()) <= 7*24*time.Hour
}

// runnerRows are the Maintenance card's Runner line, with the deadline or
// queue note and a failed check under it, and the button that queues or
// cancels an update.
func (m Model) runnerRows() []ui.Row {
	u := m.st.RunnerUpdate
	versions := u.Installed + " → " + u.Latest + " "
	var lines []string
	switch {
	case u.Installed == "":
		lines = []string{sDim.Render("version unknown (no dist/current)")}
	case u.Running:
		lines = []string{versions + ui.Badge("updating", ui.BadgeBusy)}
	case u.Queued:
		lines = []string{versions + ui.Badge("queued", ui.BadgeWarn), "runs when no job is running or queued"}
	case u.Deadline != nil:
		kind := ui.BadgeWarn
		if m.updateUrgent() {
			kind = ui.BadgeBad
		}
		left := "overdue"
		if d := u.Deadline.Sub(m.now()); d >= 0 {
			left = fmt.Sprintf("%d days", int(d.Hours()/24))
		}
		lines = []string{versions + ui.Badge("update available", kind),
			fmt.Sprintf("update by %s (%s)", u.Deadline.Local().Format("2006-01-02"), left)}
	case u.CheckedAt != nil:
		lines = []string{u.Installed + " " + ui.Badge("up to date", ui.BadgeOK) + " checked " + ago(m.now().Sub(*u.CheckedAt))}
	case u.CheckError == "":
		lines = []string{u.Installed + " " + sDim.Render("checking…")}
	default:
		lines = []string{u.Installed}
	}
	if u.CheckError != "" {
		lines = append(lines, sDim.Render("last check failed: "+u.CheckError))
	}
	rows := []ui.Row{{Label: "Runner", Lines: lines}}
	m.mg.rnQueue.SetDisabled(!m.connected)
	m.mg.rnCancel.SetDisabled(!m.connected)
	switch {
	case u.Running:
	case u.Queued:
		rows = append(rows, ui.Row{Items: []ui.Widget{m.mg.rnCancel}})
	case u.Deadline != nil:
		rows = append(rows, ui.Row{Items: []ui.Widget{m.mg.rnQueue}})
	}
	return rows
}

func (m Model) queueRunnerUpdate() (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	c := m.c
	return m, m.action("runner update queued", func(cx context.Context) error { return c.QueueRunnerUpdate(cx) })
}

func (m Model) cancelRunnerUpdate() (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	c := m.c
	return m, m.action("queued runner update cancelled", func(cx context.Context) error { return c.CancelRunnerUpdate(cx) })
}
```

In `internal/tui/dialogs.go` (`pressed`), replace:

```go
	case setPrune:
		return m.startPrune()
```

with:

```go
	case setPrune:
		return m.startPrune()
	case setRunnerQueue:
		return m.queueRunnerUpdate()
	case setRunnerCancel:
		return m.cancelRunnerUpdate()
```

- [ ] **Step 6: Add the top-bar badge** (`internal/tui/shell.go`)

Replace the end of `chips`:

```go
	return " " + sAccent.Render("ghr") + "   mode " + sGreen.Render("● "+strings.ToUpper(m.st.Mode)) +
		"   runners " + runners + "   api " + api + "   disk " + disk + "   " + conn
}
```

with:

```go
	if b := m.runnerBadge(); b != "" {
		disk += "   " + b
	}
	return " " + sAccent.Render("ghr") + "   mode " + sGreen.Render("● "+strings.ToUpper(m.st.Mode)) +
		"   runners " + runners + "   api " + api + "   disk " + disk + "   " + conn
}

// runnerBadge is the top bar's sign that a newer runner is available: amber,
// red once the deadline is 7 days away or past.
func (m Model) runnerBadge() string {
	if m.st.RunnerUpdate.Deadline == nil {
		return ""
	}
	if m.updateUrgent() {
		return ui.Badge("runner ↑", ui.BadgeBad)
	}
	return ui.Badge("runner ↑", ui.BadgeWarn)
}
```

- [ ] **Step 7: Create the golden and read it**

Run: `timeout 300 go test ./internal/tui/ -run TestSettingsRunnerUpdateGolden -update`, then open `internal/tui/testdata/TestSettingsRunnerUpdateGolden/120.golden`. Its first line ends `disk ▕██████░░░░▏ 61%   [RUNNER ↑]   ● connected`, and its Maintenance card reads:

```
╭─ Maintenance ───────────────────────────────────────────────────────────────────────────────────────╮
│ Disk                 61% used                                                                       │
│ Prune                not pruned since the daemon started                                            │
│ Runner               2.337.0 → 2.338.0 [UPDATE AVAILABLE]                                           │
│                      update by 2026-11-02 (30 days)                                                 │
│                      [ Queue update ]                                                               │
│                      [ Reload config.yaml ]   [ Prune now ]                                         │
╰─────────────────────────────────────────────────────────────────────────────────────────────────────╯
```

In `80.golden` the same rows appear, the card 80 columns wide and nothing cut.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `timeout 400 go test ./... && timeout 120 gofmt -l . && timeout 300 go vet ./...`
Expected: every package `ok` (the existing Settings goldens are 30 rows tall and do not reach the Maintenance card, so they do not change); nothing from `gofmt` or `vet`.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/model.go internal/tui/manage.go internal/tui/dialogs.go internal/tui/shell.go internal/tui/tui_test.go internal/tui/runner_update_test.go internal/tui/testdata/TestSettingsRunnerUpdateGolden
git commit -m "feat(tui): show the runner version in Settings"
```

### Task 11: Picker widget

**Files:**
- Create: `internal/tui/ui/picker.go`
- Test: `internal/tui/ui/picker_test.go` (new)

**Interfaces:**
- Consumes: existing `ui` helpers `glyph`, `clicked`, `printable`, `Cell`, `ZoneID`, styles `Bold`, `Dim`, `Accent` (`internal/tui/ui/control.go`, `theme.go`); test helpers `key`, `render`, `clickAt`, `outside`, `typeText` (`internal/tui/ui/control_test.go`).
- Produces: C8, and `takeAdvance` so a pick moves focus on (`ui.Group.Key`, `internal/tui/ui/focus.go:105-110`).

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Every line `View` returns is exactly `width` columns wide (at least 16), except the empty-list line; the cursor stays on the highlighted option whether or not the picker has focus.

- [ ] **Step 1: Write the failing tests** (`internal/tui/ui/picker_test.go`)

```go
package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func repoPicker() *Picker {
	p := NewPicker("t/pick", 3)
	p.SetOptions([]PickOption{
		{Label: "booklore", Badge: "[PUBLIC]"},
		{Label: "darkcloud", Badge: "[PRIVATE]", Disabled: true, Note: "added"},
		{Label: "darkmem", Badge: "[PRIVATE]"},
		{Label: "immich", Badge: "[PRIVATE]"},
		{Label: "Kavita", Badge: "[PRIVATE]"},
	})
	return p
}

func picked(p *Picker) string {
	o, _ := p.Picked()
	return o.Label
}

func TestPickerView(t *testing.T) {
	p := repoPicker()
	want := strings.Join([]string{
		"› [ type to filter                       ]",
		"  ▸ booklore  [PUBLIC]                    ",
		"    darkcloud  [PRIVATE]  added           ",
		"    darkmem  [PRIVATE]                    ",
		"  2 more, ↑↓ to scroll                    ",
	}, "\n")
	if got := render(p.View(true, 42)); got != want {
		t.Fatalf("view:\n%s\nwant:\n%s", got, want)
	}
	narrow := render(p.View(false, 20))
	for i, l := range strings.Split(narrow, "\n") {
		if ansi.StringWidth(l) > 20 {
			t.Errorf("line %d is %d wide: %q", i, ansi.StringWidth(l), l)
		}
	}
	if !strings.Contains(narrow, "darkcloud  [PRI…") {
		t.Fatalf("a long row is not cut with an ellipsis:\n%s", narrow)
	}
	if got := render(NewPicker("t/empty", 3).View(false, 30)); !strings.Contains(got, "nothing to pick") {
		t.Fatalf("empty picker:\n%s", got)
	}
}

func TestPickerKeys(t *testing.T) {
	p := repoPicker()
	for _, k := range []string{"a", " ", "backspace", "up", "down", "enter"} {
		if !p.TakesKey(key(k)) {
			t.Errorf("%s not taken", k)
		}
	}
	for _, k := range []string{"tab", "shift+tab", "esc", "ctrl+s"} {
		if p.TakesKey(key(k)) {
			t.Errorf("%s taken; it belongs to the dialog", k)
		}
	}
	p.Update(key("down"))
	p.Update(key("enter"))
	if picked(p) != "" || p.takeAdvance() {
		t.Fatalf("a disabled option was picked: %q", picked(p))
	}
	p.Update(key("down"))
	p.Update(key("enter"))
	if picked(p) != "darkmem" || !p.takeAdvance() {
		t.Fatalf("enter picked %q", picked(p))
	}
	for range 5 {
		p.Update(key("down"))
	}
	v := render(p.View(true, 42))
	if !strings.Contains(v, "▸ Kavita") || strings.Contains(v, "booklore") || !strings.Contains(v, "2 more") {
		t.Fatalf("the window did not follow the highlight:\n%s", v)
	}
	p.Update(key("up"))
	p.Update(key("up"))
	p.Update(key("up"))
	if v := render(p.View(true, 42)); !strings.Contains(v, "▸ darkcloud") || strings.Contains(v, "booklore") {
		t.Fatalf("up scrolled wrong:\n%s", v)
	}
}

func TestPickerFilter(t *testing.T) {
	p := repoPicker()
	typeText(p, "DARK")
	v := render(p.View(true, 42))
	if p.Filter() != "DARK" || !strings.Contains(v, "[ DARK") || !strings.Contains(v, "darkcloud") || !strings.Contains(v, "darkmem") ||
		strings.Contains(v, "booklore") || strings.Contains(v, "more") {
		t.Fatalf("filter ignores case:\n%s", v)
	}
	p.Update(key("down"))
	p.Update(key("enter"))
	if picked(p) != "darkmem" {
		t.Fatalf("picked %q", picked(p))
	}
	typeText(p, "x")
	if v := render(p.View(true, 42)); !strings.Contains(v, "no match") {
		t.Fatalf("no match:\n%s", v)
	}
	p.Update(key("enter"))
	if picked(p) != "darkmem" {
		t.Fatal("enter on an empty list changed the pick")
	}
	p.Update(key("backspace"))
	p.Update(key("backspace"))
	if p.Filter() != "DAR" {
		t.Fatalf("backspace: %q", p.Filter())
	}
}

func TestPickerMouse(t *testing.T) {
	p := repoPicker()
	view := p.View(false, 42)
	if !p.Hit(clickAt(t, view, "t/pick/row-2")) || p.Hit(outside) {
		t.Fatal("hit testing")
	}
	p.Update(clickAt(t, view, "t/pick/row-1"))
	if picked(p) != "" {
		t.Fatal("a click picked a disabled option")
	}
	p.Update(clickAt(t, view, "t/pick/row-2"))
	if picked(p) != "darkmem" {
		t.Fatalf("click picked %q", picked(p))
	}
	wheel := func(b tea.MouseButton) tea.MouseMsg {
		return tea.MouseMsg{X: 3, Y: 1, Action: tea.MouseActionPress, Button: b}
	}
	for range 4 {
		p.Update(wheel(tea.MouseButtonWheelDown))
	}
	v := render(p.View(false, 42))
	if !strings.Contains(v, "darkmem") || !strings.Contains(v, "Kavita") || strings.Contains(v, "darkcloud") {
		t.Fatalf("wheel down stops at the last full window:\n%s", v)
	}
	p.Update(wheel(tea.MouseButtonWheelUp))
	if v := render(p.View(false, 42)); !strings.Contains(v, "darkcloud") || strings.Contains(v, "Kavita") {
		t.Fatalf("wheel up:\n%s", v)
	}
}

func TestPickerKeepsPickAcrossOptions(t *testing.T) {
	p := repoPicker()
	p.Update(key("enter"))
	p.SetOptions([]PickOption{{Label: "zeta"}, {Label: "booklore"}})
	if picked(p) != "booklore" {
		t.Fatalf("pick lost: %q", picked(p))
	}
	p.SetOptions([]PickOption{{Label: "booklore", Disabled: true}})
	if picked(p) != "" {
		t.Fatal("a pick survived becoming disabled")
	}
	p.SetDisabled(true)
	p.Update(key("a"))
	if p.Focusable() || p.Filter() != "" {
		t.Fatal("a disabled picker took a key")
	}
}

// A window that shrinks or grows keeps the highlight in view, so enter picks
// the row the user sees.
func TestPickerResizeKeepsTheHighlightShown(t *testing.T) {
	p := NewPicker("t/resize", 8)
	var opts []PickOption
	for i := range 10 {
		opts = append(opts, PickOption{Label: fmt.Sprintf("r%02d", i)})
	}
	p.SetOptions(opts)
	for range 7 {
		p.Update(key("down"))
	}
	p.Rows = 3
	if v := render(p.View(true, 30)); !strings.Contains(v, "▸ r07") || strings.Contains(v, "r04") {
		t.Fatalf("shrunk:\n%s", v)
	}
	p.Update(key("enter"))
	if picked(p) != "r07" {
		t.Fatalf("enter picked %q", picked(p))
	}
	p.Rows = 8
	if v := render(p.View(true, 30)); !strings.Contains(v, "▸ r07") || !strings.Contains(v, "r02") || !strings.Contains(v, "r09") {
		t.Fatalf("grown:\n%s", v)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 200 go test ./internal/tui/ui/`
Expected: FAIL to build with `undefined: NewPicker` (and `PickOption`, `Picker`).

- [ ] **Step 3: Write the implementation** (`internal/tui/ui/picker.go`)

```go
package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// PickOption is one choice of a Picker.
type PickOption struct {
	Label    string // what the filter matches and a pick returns
	Badge    string // shown after the label
	Disabled bool   // shown dim, with Note, and never picked
	Note     string
}

// Picker is a filter field over a window of Rows options: typing filters by
// label ignoring case, up and down move the highlight and scroll the window,
// enter or a click picks, the wheel scrolls.
type Picker struct {
	id       string
	Options  []PickOption
	Rows     int
	Disabled bool
	filter   string
	cursor   int // index into the filtered options
	top      int // first filtered option in the window
	picked   int // index into Options; -1 when nothing is picked
	advance  bool
}

func NewPicker(id string, rows int) *Picker { return &Picker{id: id, Rows: rows, picked: -1} }

func (p *Picker) ID() string         { return p.id }
func (p *Picker) Focusable() bool    { return !p.Disabled }
func (p *Picker) Capturing() bool    { return false }
func (p *Picker) Blur()              {}
func (p *Picker) SetDisabled(d bool) { p.Disabled = d }
func (p *Picker) Filter() string     { return p.filter }

// SetOptions replaces the options and resets the window. The pick survives
// when its label is still offered and enabled.
func (p *Picker) SetOptions(opts []PickOption) {
	label := ""
	if o, ok := p.Picked(); ok {
		label = o.Label
	}
	p.Options, p.picked, p.cursor, p.top = opts, -1, 0, 0
	for i, o := range opts {
		if label != "" && o.Label == label && !o.Disabled {
			p.picked = i
		}
	}
}

// Picked returns the picked option.
func (p *Picker) Picked() (PickOption, bool) {
	if p.picked < 0 || p.picked >= len(p.Options) {
		return PickOption{}, false
	}
	return p.Options[p.picked], true
}

// visible lists the indexes of the options whose label contains the filter.
func (p *Picker) visible() []int {
	f := strings.ToLower(p.filter)
	var out []int
	for i, o := range p.Options {
		if strings.Contains(strings.ToLower(o.Label), f) {
			out = append(out, i)
		}
	}
	return out
}

func (p *Picker) rowZone(i int) string { return ZoneID(p.id, fmt.Sprintf("row-%d", i)) }

// TakesKey: printable keys and backspace edit the filter; up, down and enter
// work the list. Tab and esc belong to the dialog.
func (p *Picker) TakesKey(k tea.KeyMsg) bool {
	if printable(k) {
		return true
	}
	switch k.String() {
	case "backspace", "up", "down", "enter":
		return true
	}
	return false
}

// shown returns the visible indexes and the window [from, to) over them. It
// first moves the window back over the highlight, which a change of Rows can
// leave outside it.
func (p *Picker) shown() (vis []int, from, to int) {
	vis = p.visible()
	p.cursor = min(max(p.cursor, 0), max(len(vis)-1, 0))
	p.top = min(p.top, max(len(vis)-p.Rows, 0))
	p.top = min(max(p.top, p.cursor-p.Rows+1, 0), p.cursor)
	return vis, p.top, min(p.top+p.Rows, len(vis))
}

func (p *Picker) Hit(msg tea.MouseMsg) bool {
	if zone.Get(p.id).InBounds(msg) {
		return true
	}
	vis, from, to := p.shown()
	for _, i := range vis[from:to] {
		if zone.Get(p.rowZone(i)).InBounds(msg) {
			return true
		}
	}
	return false
}

func (p *Picker) takeAdvance() bool {
	a := p.advance
	p.advance = false
	return a
}

// move shifts the highlight by d within n options, scrolling it into the window.
func (p *Picker) move(d, n int) {
	if n == 0 {
		return
	}
	p.cursor = min(max(p.cursor+d, 0), n-1)
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+p.Rows {
		p.top = p.cursor - p.Rows + 1
	}
}

// pick picks option i unless it is disabled; a pick asks to advance focus.
func (p *Picker) pick(i int) {
	if !p.Options[i].Disabled {
		p.picked, p.advance = i, true
	}
}

func (p *Picker) Update(msg tea.Msg) (Control, tea.Cmd) {
	p.advance = false
	if p.Disabled {
		return p, nil
	}
	vis, from, to := p.shown()
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeySpace:
			p.filter += " "
			p.cursor, p.top = 0, 0
		case printable(msg):
			p.filter += string(msg.Runes)
			p.cursor, p.top = 0, 0
		case msg.String() == "backspace":
			if r := []rune(p.filter); len(r) > 0 {
				p.filter = string(r[:len(r)-1])
				p.cursor, p.top = 0, 0
			}
		case msg.String() == "up":
			p.move(-1, len(vis))
		case msg.String() == "down":
			p.move(1, len(vis))
		case msg.String() == "enter":
			if p.cursor < len(vis) {
				p.pick(vis[p.cursor])
			}
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
			d := 1
			if msg.Button == tea.MouseButtonWheelUp {
				d = -1
			}
			p.top = min(max(p.top+d, 0), max(len(vis)-p.Rows, 0))
			p.cursor = min(max(p.cursor, p.top), p.top+p.Rows-1)
			return p, nil
		}
		if !clicked(msg) {
			return p, nil
		}
		for n := from; n < to; n++ {
			if zone.Get(p.rowZone(vis[n])).InBounds(msg) {
				p.cursor = n
				p.pick(vis[n])
			}
		}
	}
	return p, nil
}

// View renders the filter field and, below it, the window of options cut to
// width, with a line counting the options outside the window.
func (p *Picker) View(focused bool, width int) string {
	w := max(width, 16)
	style := Bold
	if p.Disabled {
		style = Dim
	}
	text := p.filter
	if text == "" {
		text = Dim.Render("type to filter")
	}
	lines := []string{glyph(focused) + zone.Mark(p.id, style.Render("[ ")+Cell(text, w-6)+style.Render(" ]"))}
	vis, from, to := p.shown()
	switch {
	case len(p.Options) == 0:
		lines = append(lines, "  "+Dim.Render("nothing to pick"))
	case len(vis) == 0:
		lines = append(lines, "  "+Dim.Render("no match"))
	}
	for n := from; n < to; n++ {
		o := p.Options[vis[n]]
		mark := "  "
		if n == p.cursor {
			mark = Accent.Render("▸ ")
		}
		text := o.Label
		if o.Disabled {
			text = Dim.Render(o.Label)
		}
		if o.Badge != "" {
			text += "  " + o.Badge
		}
		if o.Disabled && o.Note != "" {
			text += "  " + Dim.Render(o.Note)
		}
		lines = append(lines, "  "+zone.Mark(p.rowZone(vis[n]), Cell(mark+text, w-2)))
	}
	if more := len(vis) - (to - from); more > 0 {
		lines = append(lines, "  "+Cell(Dim.Render(fmt.Sprintf("%d more, ↑↓ to scroll", more)), w-2))
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 200 go test ./internal/tui/ui/ && timeout 120 gofmt -l . && timeout 300 go vet ./internal/tui/ui/`
Expected: `ok  github.com/darkraise/ghr/internal/tui/ui`; nothing from `gofmt` or `vet`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/ui/picker.go internal/tui/ui/picker_test.go
git commit -m "feat(ui): add a filterable picker widget"
```

### Task 12: Add repository picker

**Files:**
- Modify: `internal/tui/dialogs.go` (IDs lines 24-30; `addRepoDialog`, `addedMsg` lines 32-50; `openAddRepo`, `sync` lines 52-87; `addRepoKey` line 92; `submitAddRepo` lines 122-126; `addRepoView` lines 163-181; `buttonOverlay` line 290; `pressed` lines 328-329)
- Modify: `internal/tui/model.go` (`Update`, after `case addedMsg:`)
- Modify: `internal/tui/input.go` (`handleMouse`, the `ovAddRepo` branch, lines 330-333)
- Modify: `internal/tui/tui_test.go` (delete five Add-dialog tests that type a name; drop the unused `ui` import)
- Test: `internal/tui/addrepo_test.go` (new), `internal/tui/testdata/TestAddRepoDialogGolden/{120,80}.golden` (new), `internal/tui/testdata/TestSettingsDialogGolden/{120,80}.golden` (regenerated)

**Interfaces:**
- Consumes: C8 `ui.Picker` (Task 11); C7 `tui.Client.AvailableRepos` and the `fakeClient` fields `avail`, `availErr`, `availCalls` (Task 10); C4 `AvailableRepo`; existing `errText` (`internal/tui/manage.go`), `clean`, `ui.Badge`, test helpers `pump`, `lineWith` (`internal/tui/settings_test.go:58,589`), `zoneOf`, `click`, `collect`, `fits`.
- Produces: C7's Task 12 names.

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

The dialog asks the daemon for the list each time it opens; while it loads, the picker is not in the focus order and focus rests on Max, and when the list (or its error) arrives focus moves to the picker (or Retry) unless the user has moved it. Add stays disabled until a repository is picked. `addRepoKey` syncs the controls before routing each key, because keys read together arrive without a frame between them and a pick must enable Add before the next key. `ghr repo add <name>` in the CLI is unchanged.

- [ ] **Step 1: Remove the tests that type a name** (`internal/tui/tui_test.go`)

Delete these five functions, each with the comment above it: `TestAddRepoDialog`, `TestAddRepoDialogTwoEntersSubmit`, `TestAddRepoDialogErrorsStayInside`, `TestAddRepoLateReplyIgnoresNewDialog`, `TestAddRepoDialogFitsNarrowScreen` (they sit together, from `func TestAddRepoDialog(` to the end of `TestAddRepoDialogFitsNarrowScreen`, just before `// While the daemon is unreachable, a does not open the dialog and says why.`). Keep `TestAddRepoUnavailableWhileUnreachable` and `TestAddRepoEnterOnCancelCancels`. Then remove the line `"github.com/darkraise/ghr/internal/tui/ui"` from the file's imports; nothing else in the file uses it.

- [ ] **Step 2: Write the failing tests** (`internal/tui/addrepo_test.go`)

```go
package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

// ownerRepos is what the daemon offers: darkcloud is configured already.
func ownerRepos() []model.AvailableRepo {
	return []model.AvailableRepo{
		{Name: "booklore"},
		{Name: "darkcloud", Private: true, Configured: true},
		{Name: "immich", Private: true},
		{Name: "newrepo", Private: true},
		{Name: "site", Private: true},
	}
}

func TestAddRepoDialog(t *testing.T) {
	c := &fakeClient{avail: ownerRepos()}
	m := feed(sampleModel(c, 120, 30), key("a"))
	if m.overlay != ovAddRepo || c.availCalls != 1 {
		t.Fatalf("dialog: overlay %v, %d list calls", m.overlay, c.availCalls)
	}
	v := m.View()
	for _, want := range []string{"Add repository", "Repository", "pick one below", "› [ type to filter", "booklore  [PUBLIC]",
		"darkcloud  [PRIVATE]  added", "immich  [PRIVATE]", "[ − ] 1 [ + ] (default)", "Allow public repo",
		"self-hosted runners on a public repo can run anyone's code", "( Cancel )", "[ Add ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("dialog missing %q", want)
		}
	}
	if m.add.group.FocusedID() != addRepo || m.add.ok.Focusable() {
		t.Fatalf("focus %q, Add enabled %v; Add waits for a pick", m.add.group.FocusedID(), m.add.ok.Focusable())
	}
	// enter in the picker picks the highlighted repository and moves focus to Max.
	m = feed(m, keys("n", "e", "w", "enter", "+", "+", "tab", "enter", "g", "p", "u", "enter", "esc", "tab", " ", "tab", "tab", "enter")...)
	if got := strings.Join(c.actions(), "|"); got != "add newrepo gpu max=3 public=true" {
		t.Fatalf("actions %q", got)
	}
	if m.overlay != ovNone || !strings.Contains(m.View(), "✔ added newrepo") {
		t.Fatalf("after add: overlay %v", m.overlay)
	}
}

// Two enters in one read (a paste, or tmux send-keys Enter Enter): the first
// picks and moves to Max, the second adds the repo.
func TestAddRepoDialogTwoEntersSubmit(t *testing.T) {
	c := &fakeClient{avail: ownerRepos()}
	m := feed(sampleModel(c, 120, 30), key("a"))
	m, _ = pump(m, keys("n", "e", "w", "enter", "enter")...)
	if got := strings.Join(c.actions(), "|"); got != "add newrepo  max=- public=false" {
		t.Fatalf("actions %q", got)
	}
	if m.overlay != ovNone {
		t.Fatalf("overlay %v", m.overlay)
	}
}

func TestAddRepoDialogPicksWithTheMouse(t *testing.T) {
	c := &fakeClient{avail: ownerRepos()}
	m := feed(sampleModel(c, 120, 30), key("a"))
	m = click(t, m, "add/repo/row-1")
	if _, ok := m.add.picker.Picked(); ok {
		t.Fatal("a configured repository was picked")
	}
	m = click(t, m, "add/repo/row-2")
	v := m.View()
	lines := strings.Split(v, "\n")
	if row := strings.Join(strings.Fields(lines[lineWith(lines, "Repository")]), " "); !strings.Contains(row, "Repository immich") || !m.add.ok.Focusable() {
		t.Fatalf("picked row not shown, or Add still disabled:\n%s", v)
	}
	m = click(t, m, addOK)
	if got := strings.Join(c.actions(), "|"); got != "add immich  max=- public=false" {
		t.Fatalf("actions %q", got)
	}
}

// The wheel over the list scrolls it; elsewhere in the dialog it does nothing.
func TestAddRepoDialogWheelScrollsTheList(t *testing.T) {
	var many []model.AvailableRepo
	for i := range 12 {
		many = append(many, model.AvailableRepo{Name: fmt.Sprintf("repo%02d", i), Private: true})
	}
	m := feed(sampleModel(&fakeClient{avail: many}, 120, 30), key("a"))
	z := zoneOf(t, m, "add/repo/row-0")
	m = feed(m, tea.MouseMsg{X: z.StartX, Y: z.StartY, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if v := m.View(); strings.Contains(v, "repo00") || !strings.Contains(v, "repo08") {
		t.Fatalf("wheel did not scroll the list:\n%s", v)
	}
	m = feed(m, tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if v := m.View(); strings.Contains(v, "repo00") || m.overlay != ovAddRepo {
		t.Fatalf("a wheel outside the list moved it:\n%s", v)
	}
}

func TestAddRepoDialogLoadingAndRetry(t *testing.T) {
	c := &fakeClient{availErr: errors.New("GitHub is rejecting the token")}
	upd, cmd := sampleModel(c, 120, 30).Update(key("a"))
	m := upd.(Model)
	if v := m.View(); !strings.Contains(v, "loading repositories…") || m.add.ok.Focusable() {
		t.Fatalf("loading:\n%s", v)
	}
	m = feed(m, collect(cmd)...)
	v := m.View()
	if !strings.Contains(v, "✖ GitHub is rejecting the token") || !strings.Contains(v, "( Retry )") || m.add.ok.Focusable() {
		t.Fatalf("error:\n%s", v)
	}
	if m.add.group.FocusedID() != addRetry {
		t.Fatalf("focus %q", m.add.group.FocusedID())
	}
	c.availErr, c.avail = nil, ownerRepos()
	m = feed(m, key("enter"))
	if v := m.View(); c.availCalls != 2 || !strings.Contains(v, "booklore") || strings.Contains(v, "( Retry )") {
		t.Fatalf("retry: %d calls\n%s", c.availCalls, v)
	}
	if m.add.group.FocusedID() != addRepo {
		t.Fatalf("focus after retry %q", m.add.group.FocusedID())
	}
}

// A list reply for a dialog that was cancelled never fills the one opened after it.
func TestAddRepoLateListReplyDropped(t *testing.T) {
	c := &fakeClient{avail: ownerRepos()}
	upd, cmd := sampleModel(c, 120, 30).Update(key("a"))
	m := feed(upd.(Model), ui.Pressed{ID: addCancel})
	upd, _ = m.Update(key("a"))
	m = upd.(Model)
	fresh := m.add
	m = feed(m, collect(cmd)...)
	if m.add != fresh || !fresh.loading || len(fresh.picker.Options) != 0 {
		t.Fatalf("late list reached the new dialog: loading %v, %d options", fresh.loading, len(fresh.picker.Options))
	}
}

// Picking a public repository leaves Allow public off; the daemon refuses it.
func TestAddRepoPublicStillNeedsTheToggle(t *testing.T) {
	c := &fakeClient{avail: ownerRepos(), addErr: errors.New("booklore is public; self-hosted runners must only serve private repos (pass --allow-public to override)")}
	m := feed(sampleModel(c, 120, 30), key("a"))
	m = feed(m, keys("b", "o", "o", "k", "enter")...)
	m = feed(m, ui.Pressed{ID: addOK})
	if got := strings.Join(c.actions(), "|"); got != "add booklore  max=- public=false" {
		t.Fatalf("actions %q", got)
	}
	if v := m.View(); m.overlay != ovAddRepo || !strings.Contains(v, "✖ booklore is public;") {
		t.Fatalf("refusal not shown in the dialog:\n%s", v)
	}
}

func TestAddRepoDialogErrorsStayInside(t *testing.T) {
	// The daemon's own message (internal/daemon/backend.go), on an 80-column screen.
	c := &fakeClient{avail: ownerRepos(), addErr: errors.New("site is public; self-hosted runners must only serve private repos (pass --allow-public to override)")}
	m := feed(sampleModel(c, 80, 30), key("a"))
	if m = click(t, m, addCancel); m.overlay != ovNone {
		t.Fatal("cancel did not close")
	}
	m = feed(m, key("a"))
	m = feed(m, keys("s", "i", "t", "e", "enter")...)
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
}

// A reply to a dialog that was cancelled never touches the dialog opened after it.
func TestAddRepoLateReplyIgnoresNewDialog(t *testing.T) {
	c := &fakeClient{avail: ownerRepos(), addErr: errors.New("repo site is already configured")}
	m := feed(sampleModel(c, 120, 30), key("a"))
	m = feed(m, keys("s", "i", "t", "e", "enter")...)
	upd, cmd := m.Update(ui.Pressed{ID: addOK})
	m = feed(upd.(Model), ui.Pressed{ID: addCancel}, key("a"))
	fresh := m.add
	m = feed(m, collect(cmd)...)
	// The open dialog hides the toast line, so check the toast itself.
	if m.add != fresh || fresh.err != "" || fresh.busy || m.toast.Text != "repo site is already configured" {
		t.Fatalf("late reply reached the new dialog: err %q busy %v", fresh.err, fresh.busy)
	}
}

// The dialog fits the narrowest supported screen, long names cut short.
func TestAddRepoDialogFitsNarrowScreen(t *testing.T) {
	c := &fakeClient{avail: append(ownerRepos(), model.AvailableRepo{Name: strings.Repeat("a-very-long-repository-name-", 3), Private: true})}
	m := feed(sampleModel(c, 40, 30), key("a"))
	v := m.View()
	for i, line := range strings.Split(v, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("line %d is %d wide:\n%s", i, lipgloss.Width(line), v)
		}
	}
	if !strings.Contains(v, "Add repository") || !strings.Contains(v, "[ Add ]") || !strings.Contains(v, "a-very-long") {
		t.Fatalf("dialog incomplete:\n%s", v)
	}
}

func TestAddRepoDialogGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			m := feed(sampleModel(&fakeClient{avail: ownerRepos()}, w, 30), key("a"))
			golden.RequireEqual(t, []byte(m.View()))
		})
	}
}

// With many repositories the dialog still fits the 22-row minimum screen.
func TestAddRepoDialogFitsShortScreen(t *testing.T) {
	var many []model.AvailableRepo
	for i := range 20 {
		many = append(many, model.AvailableRepo{Name: fmt.Sprintf("repo%02d", i), Private: true})
	}
	for _, h := range []int{22, 30} {
		m := feed(sampleModel(&fakeClient{avail: many}, 120, h), key("a"))
		fits(t, "add repository", m.View(), 120, h)
		if v := m.View(); !strings.Contains(v, "more, ↑↓ to scroll") || !strings.Contains(v, "[ Add ]") {
			t.Fatalf("height %d:\n%s", h, v)
		}
	}
}

// On a narrow, short screen, with an error and many repositories, the list
// gives up rows so the dialog and its buttons still fit.
func TestAddRepoDialogFitsNarrowShortScreenWithAnError(t *testing.T) {
	var many []model.AvailableRepo
	for i := range 20 {
		many = append(many, model.AvailableRepo{Name: fmt.Sprintf("repo%02d", i), Private: true})
	}
	c := &fakeClient{avail: many, addErr: errors.New("token cannot see darkraise/repo00: add it to the PAT's repository access first")}
	m := feed(sampleModel(c, 40, 30), key("a"))
	m = feed(m, key("enter"), ui.Pressed{ID: addOK})
	for _, h := range []int{30, 28} {
		m = feed(m, tea.WindowSizeMsg{Width: 40, Height: h})
		v := m.View()
		fits(t, "add repository", v, 40, h)
		if !strings.Contains(v, "[ Add ]") || !strings.Contains(v, "✖ token cannot see") || !strings.Contains(v, "more, ↑↓ to scroll") {
			t.Fatalf("height %d:\n%s", h, v)
		}
	}
}
```

The fake client records an add as `add <name> <labels joined by ,> max=<n or -> public=<bool>`, so an add without labels has two spaces after the name.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/tui/`
Expected: FAIL to build with `undefined: addRepo` (and `addRetry`, `m.add.picker`).

- [ ] **Step 4: Rebuild the dialog** (`internal/tui/dialogs.go`)

Replace the Add-dialog IDs at the end of the `const` block:

```go
	addName   = "add/name"
	addMax    = "add/max"
	addLabels = "add/labels"
	addPublic = "add/public"
	addOK     = "add/ok"
	addCancel = "add/cancel"
)
```

with:

```go
	addRepo   = "add/repo"
	addRetry  = "add/retry"
	addMax    = "add/max"
	addLabels = "add/labels"
	addPublic = "add/public"
	addOK     = "add/ok"
	addCancel = "add/cancel"
)

// pickerRows is how many repositories the Add dialog's picker shows at once.
const pickerRows = 8
```

Replace the `addRepoDialog` type and the `addedMsg` type (from `// addRepoDialog is the Add repository form.` to the closing brace of `addedMsg`) with:

```go
// addRepoDialog is the Add repository form. The repository is picked from
// the ones the daemon lists as available.
type addRepoDialog struct {
	picker            *ui.Picker
	max               *ui.Stepper
	labels            *ui.TagList
	public            *ui.Toggle
	ok, cancel, retry *ui.Button
	group             ui.Group
	loading           bool   // the repository list is being fetched
	listErr           string // why the list could not be fetched
	err               string // the daemon's rejection, shown inside the dialog
	busy              bool
}

// addedMsg and availMsg carry the dialog that sent the request, so a late
// reply never changes a dialog opened after it.
type (
	addedMsg struct {
		d    *addRepoDialog
		name string
		err  error
	}
	availMsg struct {
		d     *addRepoDialog
		repos []model.AvailableRepo
		err   error
	}
)
```

In `openAddRepo`, replace the dialog literal:

```go
	d := &addRepoDialog{
		name:   ui.NewTextField(addName, 30),
		max:    ui.NewStepper(addMax, 0, 99, 1),
		labels: ui.NewTagList(addLabels),
		public: ui.NewToggle(addPublic, false),
		ok:     ui.NewButton(addOK, "Add", ui.Primary),
		cancel: ui.NewButton(addCancel, "Cancel", ui.Secondary),
	}
```

with:

```go
	d := &addRepoDialog{
		picker:  ui.NewPicker(addRepo, pickerRows),
		max:     ui.NewStepper(addMax, 0, 99, 1),
		labels:  ui.NewTagList(addLabels),
		public:  ui.NewToggle(addPublic, false),
		ok:      ui.NewButton(addOK, "Add", ui.Primary),
		cancel:  ui.NewButton(addCancel, "Cancel", ui.Secondary),
		retry:   ui.NewButton(addRetry, "Retry", ui.Secondary),
		loading: true,
	}
```

Replace the end of `openAddRepo` and the whole `sync` method:

```go
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
```

with:

```go
	d.sync(true)
	m.add, m.overlay = d, ovAddRepo
	return m, m.fetchAvailable(d)
}

// fetchAvailable asks the daemon which repositories d can offer.
func (m Model) fetchAvailable(d *addRepoDialog) tea.Cmd {
	c := m.c
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		rs, err := c.AvailableRepos(cx)
		return availMsg{d, rs, err}
	}
}

// gotAvailable fills the picker, or shows why the list failed. Focus moves
// to the picker, or to Retry, unless it has left Max since the dialog opened.
func (m Model) gotAvailable(msg availMsg) (tea.Model, tea.Cmd) {
	d := m.add
	if m.overlay != ovAddRepo || d != msg.d {
		return m, nil
	}
	d.loading = false
	if msg.err != nil {
		d.listErr = errText(msg.err)
	} else {
		d.listErr = ""
		opts := make([]ui.PickOption, len(msg.repos))
		for i, r := range msg.repos {
			vis := ui.Badge("private", ui.BadgeMuted)
			if !r.Private {
				vis = ui.Badge("public", ui.BadgeWarn)
			}
			opts[i] = ui.PickOption{Label: clean(r.Name), Badge: vis, Disabled: r.Configured, Note: "added"}
		}
		d.picker.SetOptions(opts)
	}
	first := d.group.FocusedID() == addMax
	d.sync(m.connected)
	if first {
		d.group.Focus(d.group.Items()[0].ID())
	}
	return m, nil
}

// retryAvailable fetches the repository list again after a failure.
func (m Model) retryAvailable() (tea.Model, tea.Cmd) {
	d := m.add
	if d == nil || d.loading || m.offline() {
		return m, nil
	}
	d.loading, d.listErr = true, ""
	d.sync(m.connected)
	return m, m.fetchAvailable(d)
}

// sync updates the Add button (disabled until a repository is picked, while
// a request is in flight or while the daemon is unreachable) and the focus
// order, so focus never rests on a disabled control.
func (d *addRepoDialog) sync(connected bool) {
	d.ok.Label = "Add"
	if d.busy {
		d.ok.Label = "Adding…"
	}
	_, picked := d.picker.Picked()
	d.ok.SetDisabled(d.busy || !connected || !picked)
	d.retry.SetDisabled(!connected)
	var ws []ui.Widget
	switch {
	case d.listErr != "":
		ws = append(ws, d.retry)
	case !d.loading:
		ws = append(ws, d.picker)
	}
	d.group.Set(append(ws, d.max, d.labels, d.public, d.cancel, d.ok))
}
```

In `addRepoKey`, replace its first line `d := m.add` with:

```go
	d := m.add
	// Keys read together arrive without a frame between them, so a pick
	// must enable Add before the next key is routed.
	d.sync(m.connected)
```

In `submitAddRepo`, replace:

```go
	name := d.name.Value().Text
	if name == "" {
		d.err = "name is required"
		return m, nil
	}
```

with:

```go
	o, ok := d.picker.Picked()
	if !ok {
		d.err = "pick a repository first"
		return m, nil
	}
	name := o.Label
```

Replace the whole `addRepoView` function (its comment included, lines 162-181) with:

```go
// addRepoView renders the dialog to fit a screen w columns wide.
func (m Model) addRepoView(w int) string {
	d := m.add
	d.sync(m.connected)
	// The rows stay inside the modal's inner width (w-6) down to 40 columns.
	rw := max(min(72, w-14), 26)
	// The list gives up rows, keeping three, until the dialog fits the screen.
	d.picker.Rows = pickerRows
	view := m.addRepoModal(w, rw)
	for d.picker.Rows > 3 && lipgloss.Height(view) > m.height {
		d.picker.Rows--
		view = m.addRepoModal(w, rw)
	}
	return view
}

// addRepoModal renders the Add repository dialog with the picker's Rows.
func (m Model) addRepoModal(w, rw int) string {
	d := m.add
	f := d.group.FocusedID()
	picked := sDim.Render("pick one below")
	if o, ok := d.picker.Picked(); ok {
		picked = sBold.Render(o.Label)
	}
	rows := []ui.Row{{Label: "Repository", Text: picked}}
	switch {
	case d.loading:
		rows = append(rows, ui.Row{Text: sDim.Render("loading repositories…")})
	case d.listErr != "":
		rows = append(rows, ui.Row{Lines: []string{sRed.Render("✖ " + d.listErr)}}, ui.Row{Items: []ui.Widget{d.retry}})
	default:
		rows = append(rows, ui.Row{Items: []ui.Widget{d.picker}})
	}
	rows = append(rows,
		ui.Row{Label: "Max", Items: []ui.Widget{d.max}},
		ui.Row{Label: "Labels", Items: []ui.Widget{d.labels}},
		ui.Row{Label: "Allow public repo", Items: []ui.Widget{d.public}},
	)
	lines, _ := ui.Render([]ui.Section{{Rows: rows}}, f, rw, false)
	body := strings.Join(lines, "\n") + "\n" + sAmber.Render("⚠ self-hosted runners on a public repo can run anyone's code")
	if d.err != "" {
		body += "\n" + lipgloss.NewStyle().Width(rw).Render(sRed.Render("✖ "+d.err))
	}
	return modal("Add repository", body, []*ui.Button{d.cancel, d.ok}, f, w)
}
```

In `buttonOverlay`, replace `case addOK, addCancel:` with:

```go
	case addOK, addCancel, addRetry:
```

In `pressed`, replace:

```go
	case addOK:
		return m.submitAddRepo()
```

with:

```go
	case addOK:
		return m.submitAddRepo()
	case addRetry:
		return m.retryAvailable()
```

- [ ] **Step 5: Route the list reply and the wheel**

In `internal/tui/model.go` (`Update`), replace:

```go
	case addedMsg:
		return m.added(msg)
```

with:

```go
	case addedMsg:
		return m.added(msg)
	case availMsg:
		return m.gotAvailable(msg)
```

In `internal/tui/input.go` (`handleMouse`), replace:

```go
	if m.overlay == ovAddRepo {
		_, cmd := m.add.group.Mouse(msg)
		return m, cmd
	}
```

with:

```go
	if m.overlay == ovAddRepo {
		m.add.sync(m.connected)
		wheel := msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown)
		if wheel && m.add.picker.Hit(msg) {
			m.add.picker.Update(msg)
			return m, nil
		}
		_, cmd := m.add.group.Mouse(msg)
		return m, cmd
	}
```

- [ ] **Step 6: Create and regenerate the goldens, and read them**

Run: `timeout 300 go test ./internal/tui/ -run 'TestAddRepoDialogGolden|TestSettingsDialogGolden' -update`, then read the four files. In `TestAddRepoDialogGolden/120.golden` the dialog reads:

```
╭────────────────────────────────────────────────────────────────────────────╮
│                                                                            │
│  Add repository                                                            │
│                                                                            │
│  Repository           pick one below                                       │
│                     › [ type to filter                                  ]  │
│                       ▸ booklore  [PUBLIC]                                 │
│                         darkcloud  [PRIVATE]  added                        │
│                         immich  [PRIVATE]                                  │
│                         newrepo  [PRIVATE]                                 │
│                         site  [PRIVATE]                                    │
│  Max                  [ − ] 1 [ + ] (default)                              │
│  Labels               + add                                                │
│  Allow public repo    [━○] Off                                             │
│  ⚠ self-hosted runners on a public repo can run anyone's code              │
│                                                                            │
│                                                     ( Cancel )    [ Add ]  │
│                                                                            │
╰────────────────────────────────────────────────────────────────────────────╯
```

`git diff` of `TestSettingsDialogGolden/*.golden` shows only the Name row and its description replaced by the Repository row, the filter field and `nothing to pick` (that test's fake client offers no repositories), with the dialog one row taller.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `timeout 400 go test ./... && timeout 120 gofmt -l . && timeout 300 go vet ./...`
Expected: every package `ok`, including the kept `TestAddRepoEnterOnCancelCancels` (four tabs from the picker reach Cancel); nothing from `gofmt` or `vet`.

- [ ] **Step 8: Check that the screen-height fit is load-bearing**

Temporarily change `for d.picker.Rows > 3 && lipgloss.Height(view) > m.height {` in `addRepoView` to `for false {` and run `timeout 300 go test ./internal/tui/ -run 'TestAddRepoDialogFits'`: `TestAddRepoDialogFitsShortScreen` and `TestAddRepoDialogFitsNarrowShortScreenWithAnError` must FAIL. Restore the line and run them again: `ok`.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/dialogs.go internal/tui/model.go internal/tui/input.go internal/tui/tui_test.go internal/tui/addrepo_test.go internal/tui/testdata/TestAddRepoDialogGolden internal/tui/testdata/TestSettingsDialogGolden
git commit -m "feat(tui): pick the repository to add from a list"
```

### Task 13: setup.sh stops ghr before installing the runner

**Files:**
- Modify: `github-runner/setup.sh` (homelab repo; new `stop_ghr` before `install_runner`, line 86; `main` lines 201-212)
- Modify: `github-runner/tests/setup_test.sh` (the `fetch_runner` and `systemctl` fakes, lines 69-89; checks after the `c-main-upgrade` case, line 201)
- Modify: `github-runner/README.md` (Install / upgrade and Use sections)

**Interfaces:**
- Consumes: none (it guards Task 5's install, which writes the same `/opt/ghr/dist`).
- Produces: none.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The daemon now installs queued runner updates into `/opt/ghr/dist`, the directory `setup.sh`'s `install_runner` and `gc_dist` write, so the two must not run at once. `setup.sh` stops `ghr.service` before installing the runner; runner units are separate systemd units and keep running, and `install_service` restarts the daemon as before. If a step after the stop fails, an exit trap starts the old daemon again, so a failed re-run leaves ghr running as it did before this change. The files are in `D:/Repositories/Personal/homelab`; every command below names them absolutely. The tests are offline (every external command is a shell function) and run in Git Bash.

- [ ] **Step 1: Write the failing checks** (`github-runner/tests/setup_test.sh`)

In the `fetch_runner` fake, replace:

```bash
fetch_runner() {
  bump fetches
  cp "$T/runner.tar.gz" "$2"
}
```

with:

```bash
fetch_runner() {
  bump fetches
  [ ! -f "$MARK_DIR/stopped" ] || touch "$MARK_DIR/fetched-after-stop"
  cp "$T/runner.tar.gz" "$2"
}
```

In the `systemctl` fake, replace:

```bash
systemctl() {
  [ "$1" = is-active ] || return 0
```

with:

```bash
systemctl() {
  [ "$1" != stop ] || { echo "$2" > "$MARK_DIR/stopped"; return 0; }
  [ "$1" != start ] || { echo "$2" > "$MARK_DIR/started"; return 0; }
  [ "$1" = is-active ] || return 0
```

After the line `check "c: current kept and points at the new version" [ "$(target_of_current)" = "$FIXTURE_VERSION" ]`, add:

```bash
check "c: ghr stopped before the runner download" [ -f "$MARK_DIR/fetched-after-stop" ]
check "c: the stopped unit is ghr.service" [ "$(cat "$MARK_DIR/stopped")" = ghr.service ]

new_case c-first-install
out=$( (systemctl() { return 5; }; run stop_ghr) 2>&1)
rc=$?
check "c: stop_ghr succeeds when ghr is not installed yet" [ "$rc" -eq 0 ]

new_case c-main-fetch-fails
old_version 2.336.0
ln -sfn "$DIST_DIR/2.336.0" "$DIST_DIR/current"
out=$( (fetch_runner() { return 1; }; run main) 2>&1)
rc=$?
check "c: main fails when the runner download fails" [ "$rc" -ne 0 ]
check "c: ghr started again after the failure" [ "$(cat "$MARK_DIR/started" 2>/dev/null)" = ghr.service ]
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 120 bash D:/Repositories/Personal/homelab/github-runner/tests/setup_test.sh; echo "exit=$?"`
Expected: `FAIL c: ghr stopped before the runner download`, `FAIL c: the stopped unit is ghr.service`, `FAIL c: stop_ghr succeeds when ghr is not installed yet` (`stop_ghr: command not found`), `FAIL c: ghr started again after the failure`, and `exit=1`.

- [ ] **Step 3: Write the implementation** (`github-runner/setup.sh`)

Before `install_runner() {`, add:

```bash
# The daemon installs queued runner updates into the same dist dir, so it must
# not run while this script installs one. Runner units outlive it, and
# install_service starts it again; on a first install there is no unit yet.
# If a later step fails, the exit trap starts the old daemon again.
stop_ghr() {
  systemctl stop ghr.service 2>/dev/null || true
  trap 'systemctl start ghr.service 2>/dev/null || true' EXIT
}

```

In `main`, replace:

```bash
  create_user_and_dirs
  install_runner
```

with:

```bash
  create_user_and_dirs
  stop_ghr
  install_runner
```

In `github-runner/README.md`, replace:

```markdown
`RUNNER_VERSION=2.337.0`. Prefer re-running while no jobs are running: a Docker upgrade
restarts the Docker daemon, which stops the containers of running jobs.
```

with:

```markdown
`RUNNER_VERSION=2.337.0`. Prefer re-running while no jobs are running: a Docker upgrade
restarts the Docker daemon, which stops the containers of running jobs. setup.sh stops ghr
while it installs the runner, so a queued runner update cannot run at the same time;
running runners are separate units and keep their jobs. If setup.sh fails after that, it
starts ghr again.
```

and in the Use block, after the line `ghr set mode all`, add:

```bash
ghr runner-update             # queue a runner update; it runs when no job is running or queued
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 120 bash D:/Repositories/Personal/homelab/github-runner/tests/setup_test.sh; echo "exit=$?"`
Expected: every line `ok`, no `FAIL`, and `exit=0`.

- [ ] **Step 5: Check that the stop is load-bearing**

Temporarily delete the `  stop_ghr` line from `main` and run the tests again: `FAIL c: ghr stopped before the runner download`, `FAIL c: the stopped unit is ghr.service` and `FAIL c: ghr started again after the failure`, `exit=1`. Restore it, then temporarily delete the `trap` line from `stop_ghr`: `FAIL c: ghr started again after the failure`, `exit=1`. Restore it, run the tests again (`exit=0`), and confirm with `git -C D:/Repositories/Personal/homelab diff --stat` that only the three files changed.

- [ ] **Step 6: Commit** (homelab repo)

```bash
git -C D:/Repositories/Personal/homelab add github-runner/setup.sh github-runner/tests/setup_test.sh github-runner/README.md
git -C D:/Repositories/Personal/homelab commit -m "fix(github-runner): stop ghr before installing runner"
```

### Task 14: Release and LXC acceptance

**Files:**
- Modify: `docs/superpowers/registers/2026-10-05-ghr-runner-update.md` (homelab repo)

**Interfaces:**
- Consumes: every earlier task.
- Produces: the release and the register's final states.

**Items:** 1, 2

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 0 - spec 1 - coupling 2 - risk 3 = 6

The controlling session runs this task itself, not a dispatched implementer: Step 3 needs your human partner's approval, and Step 1 is the execution skill's final review.

Every SSH command runs from Git Bash with `timeout` and `-o BatchMode=yes` against `root@192.168.0.99` with `-i ~/.ssh/ghr_lxc`. `<SCR>` below is the session's scratchpad directory, the absolute path the system prompt names; write every script there with the Write tool, not a heredoc (backslashes must reach the file), and run it by that absolute path. Each Bash call starts a fresh shell, so substitute every `<…>` value (`<SCR>`, `<APPROVED>`, `<EXPECT>`) literally into the command; never rely on a variable set in an earlier call.

- [ ] **Step 1: Final review before releasing**

Read `D:/Repositories/Personal/darkraise-ai-plugins/plugins/dr-superpowers/reference/final-review.md` and run the whole-branch final review it describes over `master..feat/runner-update` in `D:/Repositories/Personal/ghr`, together with Task 13's homelab commit (`git -C D:/Repositories/Personal/homelab log -1 --format=%H -- github-runner/setup.sh` names it; review it with `git -C D:/Repositories/Personal/homelab show <that sha>`). Apply its fix wave, and append its `Final review: clean …` line to the ledger `D:/Repositories/Personal/homelab/.superpowers/sdd/2026-10-05-ghr-runner-update/progress.md`. If it is not clean after one fix wave, stop and report the open findings.

- [ ] **Step 2: Pre-flight on the LXC**

```bash
timeout 60 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'ghr version; ghr status | head -1; readlink -f /opt/ghr/dist/current; ls /opt/ghr/dist; sha256sum /root/github-runner/setup.sh; curl -s --unix-socket /run/ghr/ghr.sock http://ghr/token | jq -r ".state + \" expires \" + (.expires_at // \"unknown\")"'
timeout 30 gh api 'repos/actions/runner/releases?per_page=3' --jq '.[] | select(.draft|not) | select(.prerelease|not) | .tag_name' | head -1
git -C D:/Repositories/Personal/ghr rev-parse feat/runner-update
```

Expected: `v0.1.8`, `global 0/` (no runner alive; otherwise wait), `/opt/ghr/dist/2.337.0`, `2.337.0` and `current`, the LXC's `setup.sh` checksum, the token's state and expiry, the latest runner tag, and the branch's head commit: call it `APPROVED`. If the token is rejected or expires before the acceptance can finish, stop and report.

- [ ] **Step 3: Ask for approval to release**

Ask your human partner, in one message: "The runner update and repository picker pass all tests and the final review on `feat/runner-update` at `<APPROVED>`, with homelab's `setup.sh` now stopping ghr while it installs the runner. May I fast-forward ghr `master` to exactly that commit, push it (CI publishes the next release), copy the new `setup.sh` to `/root/github-runner` on the LXC, deploy the release with `GHR_VERSION` pinned, and run the acceptance there? It reads `/status` and `/repos/available`, runs `ghr runner-update` once, and opens and cancels the Add repository dialog in a throwaway tmux session; it adds no repository. `setup.sh` installs GitHub's latest runner itself, so after the deploy the runner is normally current. If GitHub has a newer runner by then, may `ghr runner-update` queue it, so ghr installs it once no job is running or queued?" Also mention the LXC token's expiry from Step 2. Wait for an explicit yes to the release; record separately whether the runner update question got a yes (call it `UPDATE_OK`). Without a yes to the release, leave the register rows at `planned` and stop.

- [ ] **Step 4: Merge, push, wait for the release, deploy**

Write `<SCR>/release.sh`:

```bash
set -euo pipefail
APPROVED=$1
cd D:/Repositories/Personal/ghr
[ "$(git rev-parse feat/runner-update)" = "$APPROVED" ] || { echo "feat/runner-update moved since approval"; exit 1; }
git switch master
git merge --ff-only "$APPROVED"
SHA=$(git rev-parse HEAD)
[ "$SHA" = "$APPROVED" ] || { echo "master is $SHA, not the approved $APPROVED"; exit 1; }
echo "sha=$SHA"
timeout 120 git push origin "$SHA:refs/heads/master"
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
  TAG=$(git tag --points-at "$SHA" | grep '^v' | sort -V | tail -1 || true)
  [ -n "$TAG" ] && timeout 30 gh release view "$TAG" --json tagName > /dev/null 2>&1 && break
  TAG=""
  sleep 15
done
[ -n "$TAG" ] || { echo "no release tagged at $SHA"; exit 1; }
[ "$(git rev-list -n 1 "$TAG")" = "$SHA" ] || { echo "$TAG does not point at $SHA"; exit 1; }
echo "tag=$TAG"
SETUP=D:/Repositories/Personal/homelab/github-runner/setup.sh
timeout 60 scp -i ~/.ssh/ghr_lxc -o BatchMode=yes "$SETUP" root@192.168.0.99:/root/github-runner/setup.sh
WANT=$(sha256sum "$SETUP" | cut -d' ' -f1)
GOT=$(timeout 30 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'sha256sum /root/github-runner/setup.sh' | cut -d' ' -f1)
[ "$WANT" = "$GOT" ] || { echo "setup.sh copy differs on the LXC"; exit 1; }
echo "setup=copied"
timeout 590 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 "cd /root/github-runner && GHR_VERSION=$TAG timeout 570 bash setup.sh > /root/setup-$TAG.log 2>&1; echo rc=\$?; systemctl is-active ghr; ghr version"
```

Run: `timeout 1500 bash <SCR>/release.sh <APPROVED>`
Expected: `sha=<APPROVED>`, `ci=ok`, `tag=v0.1.9`, `setup=copied`, `rc=0`, `active`, `v0.1.9`. Any other ending means stop and report; nothing reached the LXC before the `scp` line, and the release was not deployed before the last line. A non-zero `rc` with `active` means `setup.sh` failed after stopping ghr and started the old daemon again; read `/root/setup-<tag>.log` on the LXC and report.

- [ ] **Step 5: Daemon state and CLI on the LXC**

First read the state the deployed daemon reports:

```bash
timeout 120 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'for i in $(seq 30); do curl -s --unix-socket /run/ghr/ghr.sock http://ghr/status | jq -e ".runner_update.checked_at" > /dev/null && break; sleep 2; done; curl -s --unix-socket /run/ghr/ghr.sock http://ghr/status | jq -c .runner_update; ghr status | sed -n 2p; curl -s --unix-socket /run/ghr/ghr.sock http://ghr/repos/available | jq -c "{n: length, configured: [.[] | select(.configured) | .name], public: [.[] | select(.private|not) | .name] | length}"; grep -c "^  - name:" /etc/ghr/config.yaml'
```

Expected: `runner_update` with `checked_at` set; the status line; a repository count well under 71 (only the configured owner's); `configured` naming exactly the repositories in `config.yaml` (its count matches the `grep -c`). Then branch on `runner_update`:

- **No `deadline`** (`installed` equals `latest`; the usual case): the status line reads `runner <ver>  up to date (checked …)`. Run `timeout 60 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'ghr runner-update; echo "exit=$?"'`. Expected: `ghr: runner <ver> is already up to date` and `exit=1`. Note `<EXPECT>` = `current`.
- **A `deadline`, and `UPDATE_OK` is yes:** the status line says `update available`. Run `timeout 60 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'ghr runner-update; echo "exit=$?"'`. Expected: `runner update queued; it runs when no job is running or queued` and `exit=0`. Then run `timeout 720 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'for i in $(seq 45); do ghr status | sed -n 2p | grep -q "up to date" && break; sleep 15; done; ghr status | sed -n 2p; readlink -f /opt/ghr/dist/current; ls /opt/ghr/dist'`. Expected: the status line reads `up to date`, `current` names the `latest` version, and `/opt/ghr/dist` holds only it and `current`. Note `<EXPECT>` = `current`.
- **A `deadline`, and `UPDATE_OK` is not yes:** run nothing that queues. Note `<EXPECT>` = `available`.

- [ ] **Step 6: TUI on the LXC (tmux script)**

Write `<SCR>/accept.sh`:

```bash
EXPECT=$1
S=accept-$$
# line_has <string>...: some line of stdin contains every string, literally.
line_has() {
  local l s ok
  while IFS= read -r l; do
    ok=1
    for s in "$@"; do [[ "$l" == *"$s"* ]] || ok=0; done
    [ "$ok" = 1 ] && return 0
  done
  return 1
}
# wait_for <seconds> <string>...: until one pane line contains every string.
wait_for() {
  local end=$((SECONDS + $1))
  shift
  until tmux capture-pane -p -t "$S" | line_has "$@"; do
    [ "$SECONDS" -lt "$end" ] || { echo "timeout waiting for: $*"; tmux capture-pane -p -t "$S"; exit 1; }
    sleep 0.3
  done
  echo "found: $*"
}
AVAIL=$(curl -s --unix-socket /run/ghr/ghr.sock http://ghr/repos/available)
# A private, unconfigured repository whose name no other name contains, ignoring
# case as the picker's filter does, so filtering by it leaves one row.
NAME=$(echo "$AVAIL" | jq -r '[.[].name | ascii_downcase] as $all | [.[] | select(.configured|not) | select(.private) | .name as $n | select([$all[] | select(contains($n | ascii_downcase))] | length == 1) | $n][0]')
ADDED=$(echo "$AVAIL" | jq -r '[.[] | select(.configured)][0].name')
echo "pick=$NAME added=$ADDED"
[ "$NAME" != null ] && [ "$ADDED" != null ] || { echo "no repository to pick, or none configured"; exit 1; }
tmux new-session -d -s "$S" -x 120 -y 40 "NO_COLOR=1 ghr" || { echo "tmux could not create $S"; exit 1; }
trap 'tmux kill-session -t "$S"' EXIT
wait_for 30 "[ACTIVE]"
if [ "$EXPECT" = current ]; then
  if tmux capture-pane -p -t "$S" | line_has "[RUNNER"; then echo "a runner badge shows while the runner is current"; exit 1; fi
else
  wait_for 15 "[RUNNER ↑]"
fi
tmux send-keys -t "$S" 5
wait_for 15 "GitHub token"
for i in 1 2 3 4; do tmux send-keys -t "$S" NPage; done
if [ "$EXPECT" = current ]; then wait_for 15 "Runner" "[UP TO DATE]"; else wait_for 15 "Runner" "[UPDATE AVAILABLE]"; fi
tmux send-keys -t "$S" a
wait_for 15 "Add repository"
wait_for 20 "type to filter"
tmux send-keys -t "$S" -l "$ADDED"
wait_for 15 "$ADDED" "added"
for i in $(seq ${#ADDED}); do tmux send-keys -t "$S" BSpace; done
wait_for 15 "type to filter"
tmux send-keys -t "$S" -l "$NAME"
tmux send-keys -t "$S" Enter
wait_for 15 "Repository" "$NAME"
tmux send-keys -t "$S" Escape
end=$((SECONDS + 15))
while tmux capture-pane -p -t "$S" | line_has "Add repository"; do
  [ "$SECONDS" -lt "$end" ] || { echo "dialog still open"; tmux capture-pane -p -t "$S"; exit 1; }
  sleep 0.3
done
echo step=ok
```

Run: `timeout 300 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'bash -s <EXPECT>' < <SCR>/accept.sh`, with `<EXPECT>` replaced by `current` or `available` from Step 5.
Expected: `step=ok`. `NO_COLOR=1` makes lipgloss use the ASCII profile, so badges read `[TEXT]` as in the unit tests; `wait_for 30 "[ACTIVE]"` proves it (a Dashboard repo row carries that badge). Typing the configured repository's name first puts its row on screen wherever it sorts, and its row carries the `added` note. Escape cancels the dialog, so nothing is added; confirm with `timeout 30 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'grep -c "^  - name:" /etc/ghr/config.yaml'`, which must print the same count as Step 5.

- [ ] **Step 7: Update the register and commit** (homelab repo)

From `D:/Repositories/Personal/homelab`, with `P=D:/Repositories/Personal/darkraise-ai-plugins/plugins/dr-superpowers`:

```bash
R=docs/superpowers/registers/2026-10-05-ghr-runner-update.md
timeout 30 bash $P/scripts/register set $R 1 done --note "<tag>: runner <ver> shown <up to date|update available> in ghr status and Settings; ghr runner-update answered <already up to date|queued, installed <ver>|not run: owner declined the update>"
timeout 30 bash $P/scripts/register set $R 2 done --note "<tag>: Add repository listed <n> of the owner's repositories, configured ones marked added; a pick showed in the dialog"
timeout 30 bash $P/scripts/register check $R
git add $R && git commit -m "docs(registers): record ghr runner update"
```

Fill each `<…>` from Steps 4 to 6. Row 1's acceptance needs `ghr runner-update` to have answered: in the `available` branch without `UPDATE_OK`, set row 1 to `verify` with the note "queue not exercised: owner declined the update" instead of `done`. If an acceptance check failed and the cause is not fixed in this session, set that row to `verify` with the failure as its note. Do not push the homelab repository without your human partner's yes.
