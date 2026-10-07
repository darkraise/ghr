# ghr Repositories page, management features and graphics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Repositories page that edits and manages each repo, expose token, registration, label-check and maintenance management in the TUI, and make the TUI graphical with badges, charts and gauges.

**Architecture:** Daemon first: the GitHub client records token metadata and lists recent runs and runner labels; new daemon endpoints serve token state, registrations, label-check observations, reload, prune and metrics, each with an `api.Client` method. Then `ui` primitives (badge, sparkline, gauge, masked field, multi-line row). Then the TUI: a shared `configPage` for two config pages fed by one config stream, the Repositories page, management cards, graphics, Help and goldens. Release and LXC acceptance last.

**Tech Stack:** Go 1.26 (go.mod), Bubble Tea v1, lipgloss v1, bubblezone v1, bubbles textinput; daemon HTTP over a Unix socket; GitHub REST API 2022-11-28.

**Spec:** docs/superpowers/specs/2026-10-05-ghr-repositories-management-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 14 of 29 tasks are heavy (not more than half), so the session implements the rest and delegates the heavy ones; effort is high because tasks are delegated.

**Plan review:** 2026-10-05 — dr-superpowers:judge-opus — executability 18 / coherence 18 / coverage 18 / assumptions 17 (round 3)

## Global Constraints

- Code repository: `D:/Repositories/Personal/ghr` (github.com/darkraise/ghr), branch `feat/repos-management` from master `9fa16fe`. Plan and spec live in `D:/Repositories/Personal/homelab`.
- Every command runs from the ghr repository root in Git Bash with an explicit `timeout`. Unit tests: `timeout 400 go test ./...`. CI also runs `gofmt -l .` (must print nothing), `go vet ./...` and `go test -race -count=1 -timeout 180s ./...`; `-race` cannot run on this Windows box (no cgo), so CI is its only run.
- TUI tests set `lipgloss.SetColorProfile(termenv.Ascii)` in `TestMain` (`internal/tui/tui_test.go:23`); goldens live under `internal/tui/testdata` and are regenerated with `go test ./internal/tui -run <Test> -update`. Every golden diff is read before committing.
- All daemon and GitHub text shown in the TUI passes through `clean()` (`internal/tui/view.go:71`). The daemon serves raw text.
- Token values never appear in events, logs, toasts, check errors or API responses.
- Comments: none unless the why is non-obvious; never reference this plan, a task or a review.
- Commits: `<type>(<scope>): <subject>`, subject ≤ 50 characters, imperative, no period. One commit per task unless a step says otherwise.
- Badge colours (spec §4): green+black text = active, online, success, matched, ok; blue+white = busy; amber+black = paused, idle, starting, expires soon, unverified; red+white = error, failure, offline, unmatched, rejected; grey+white = removing, cleaning, ghr, other, cancelled, unknown. ASCII profile renders `[TEXT]`, the same width as ` TEXT `.
- Page keys: 1 Dashboard, 2 Repositories, 3 Runners, 4 History, 5 Settings. Tab-row short names: Dash, Repo, Run, Hist, Set.
- Layout numbers: `wideMin = 100`; sidebar 16; Repositories list card 30 wide (26 inside: cursor 2, name 11, space, badge ≤ 10, space, dirty mark 1); one-column gap; panel rows use the narrow row form when the panel is under 70 columns.
- OS and architecture labels never offered by the label quick-add: `linux`, `windows`, `macos`, `x64`, `x86`, `arm`, `arm64`.
- Label check: 20 most recent runs, plus `queued`, `in_progress`, `waiting` runs, de-duplicated by run id; 60-second scan deadline; one scan per repo at a time; a new scan at most once a minute per repo.
- Metrics: 60 samples, one every 60 seconds on the sampler's own ticker; a gap over 90 seconds draws a space.
- Activity card: `GET /history?repo=<configured name>&limit=500`, last 7 days or the retention when shorter.

## Contracts

**C1 `internal/github` (Task 1, Task 2):**
```go
type Label struct{ Name string `json:"name"` }
type Runner struct { // existing fields kept; Labels added
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Status string  `json:"status"`
	Busy   bool    `json:"busy"`
	Labels []Label `json:"labels"`
}
type TokenMeta struct {
	Generation    uint64
	CheckedAt     time.Time  // zero: no call completed in this generation
	OK            bool       // the last verdict of this generation: a success (true) or an auth failure (false); other failures leave it
	RateRemaining *int
	RateLimit     *int
	RateReset     *time.Time
	ExpiresAt     *time.Time
}
func (c *Client) TokenMeta() TokenMeta
func (c *Client) ForgetCache()                       // existing; now also bumps Generation and clears the meta
func (c *Client) ListRecentRuns(ctx context.Context, repo string, n int) ([]Run, error)
```
A 403 whose JSON `message` contains `secondary rate limit` (case-insensitive) is `ErrRateLimit` with `RetryAt = now + 60s` (or `Retry-After` when present).

**C2 `internal/model` (Task 3; used by Tasks 4 to 10):**
```go
type MaintenanceStatus struct {
	Running      bool       `json:"running"`
	LastStarted  *time.Time `json:"last_started,omitempty"`
	LastFinished *time.Time `json:"last_finished,omitempty"`
	LastOutcome  string     `json:"last_outcome,omitempty"` // "ok", "errors", "interrupted"
}
// Status gains: Maintenance MaintenanceStatus `json:"maintenance"`
type TokenStatus struct {
	State         string     `json:"state"` // "ok", "rejected", "unverified"
	CheckedAt     *time.Time `json:"checked_at,omitempty"`
	Reason        string     `json:"reason,omitempty"`
	RateRemaining *int       `json:"rate_remaining,omitempty"`
	RateLimit     *int       `json:"rate_limit,omitempty"`
	RateReset     *time.Time `json:"rate_reset,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}
type Registration struct {
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	Status string   `json:"status"` // "online", "offline"
	Busy   bool     `json:"busy"`
	Labels []string `json:"labels"`
	GHR    bool     `json:"ghr"`
}
type LabelGroup struct {
	Labels   []string  `json:"labels"`   // lower-cased, sorted
	Jobs     []string  `json:"jobs"`     // up to three "workflow / job" names
	More     int       `json:"more"`     // further distinct names not listed
	Count    int       `json:"count"`
	LastSeen time.Time `json:"last_seen"`
}
type LabelCheck struct {
	State     string       `json:"state"` // "not_checked", "checking", "done"
	CheckedAt *time.Time   `json:"checked_at,omitempty"`
	Partial   bool         `json:"partial"`
	Error     string       `json:"error,omitempty"` // why the last scan stopped early, other than its deadline
	Groups    []LabelGroup `json:"groups"`
}
type MetricSample struct {
	At     time.Time `json:"at"`
	Live   int       `json:"live"`
	Queued int       `json:"queued"`
	CPU    *float64  `json:"cpu,omitempty"`
	Mem    *int64    `json:"mem,omitempty"`
}
type Metrics struct {
	Samples  []MetricSample `json:"samples"`
	CPU      *float64       `json:"cpu,omitempty"`
	MemUsed  *int64         `json:"mem_used,omitempty"`
	MemTotal *int64         `json:"mem_total,omitempty"`
	DiskPct  int            `json:"disk_pct"`
}
```

**C3 `internal/api` (Task 3, Tasks 5 to 10):** `api.Error` gains `RetryAt time.Time`; error bodies are `{"error": msg, "retry_at": RFC3339}` (`retry_at` omitted when zero); the client decodes `retry_at` into `Error.RetryAt`. `func FromGitHub(err error) error` maps a `*github.APIError` by kind: `ErrRateLimit` → 429 with `RetryAt`, `ErrAuth` → 403, `ErrNotFound` → 404, `ErrUnprocessable` → 409, any other → 502; a non-GitHub error is returned unchanged. Client methods:
```go
func (c *Client) Token(ctx context.Context) (model.TokenStatus, error)
func (c *Client) Registrations(ctx context.Context, repo string) ([]model.Registration, error)
func (c *Client) DeleteRegistration(ctx context.Context, repo string, id int64) error
func (c *Client) StartLabelCheck(ctx context.Context, repo string) error
func (c *Client) LabelCheck(ctx context.Context, repo string) (model.LabelCheck, error)
func (c *Client) Reload(ctx context.Context) ([]string, error)
func (c *Client) Prune(ctx context.Context) error
func (c *Client) Metrics(ctx context.Context) (model.Metrics, error)
```
Routes: `GET /token`, `GET /repos/{name}/registrations`, `DELETE /repos/{name}/registrations/{id}`, `POST /repos/{name}/label-check`, `GET /repos/{name}/label-check`, `POST /reload`, `POST /prune`, `GET /metrics`. Empty lists encode as `[]`.

**C4 `internal/runner` (Task 4):** `var ErrPruneRunning = errors.New("a prune is already running")`; `func (m *Manager) StartPrune() error` (returns `ErrPruneRunning` while one runs); `func (m *Manager) Maintenance() model.MaintenanceStatus`; `Manager.Status()` fills `Maintenance`. `func (m *Manager) Close()` cancels the maintenance context (called by the daemon on shutdown before `Wait`). Manual and automatic pruning share one reservation (`m.pruning` under `m.mu`, `reservePrune`/`releasePrune`); `StartPrune` returns `ErrPruneRunning` while either holds it.

**C5 `internal/daemon` (Task 5 to Task 9):** `func (b *Backend) Reload() ([]string, error)` (config and token; SIGHUP and `POST /reload` both call it; it calls `b.Wake()` on success and never ticks). Backend's `GitHub` interface gains `ListRunners`, `GetRunner`, `DeleteRunner`, `ListRuns`, `ListRecentRuns`, `TokenMeta`. Registration names in ghr's namespace match `^ghr-<repo>-[0-9a-f]{6}$` (repo compared case-insensitively). Task 7 adds `func (b *Backend) repoName(name string) (string, error)` (404 for an unknown repo) and `func (b *Backend) degradedErr() error` (503 while degraded). Task 8 adds `StartLabelCheck(repo string) error`, `LabelCheck(repo string) (model.LabelCheck, error)`, `forgetLabelCheck(name string)` (called by `AddRepo` and `FinalizeRemovals`) and `Close()` (cancels every scan; the daemon calls it on shutdown after `m.Close()`). Task 10 adds `Sampler *metrics.Sampler` and `Metrics() model.Metrics`.

**C6 `internal/metrics` (Task 10):**
```go
type Snapshot struct{ Live, Queued int }
type Sampler struct {
	Now     func() time.Time
	CPUStat string // default "/sys/fs/cgroup/cpu.stat"
	CPUMax  string // default "/sys/fs/cgroup/cpu.max"
	MemInfo string // default "/proc/meminfo"
	NumCPU  func() int
	Counts  func() Snapshot
	Disk    func() int
}
func NewSampler(counts func() Snapshot, disk func() int) *Sampler
func (s *Sampler) Sample()                       // takes one sample now
func (s *Sampler) Run(ctx context.Context, every time.Duration)
func (s *Sampler) Metrics() model.Metrics
```

**C7 `internal/tui/ui` (Tasks 11 to 13):**
```go
type BadgeKind int
const ( BadgeOK BadgeKind = iota; BadgeBusy; BadgeWarn; BadgeBad; BadgeMuted )
func Badge(text string, kind BadgeKind) string    // " TEXT " styled; "[TEXT]" under termenv.Ascii
func Sparkline(vals []*float64, limit float64, width int) string
func Gauge(used, total, width int) string         // moved from tui.gauge, same output
func BadgeRow(sel lipgloss.Style, selected bool, before, badge, after string, w int) string // Task 13: exactly w wide; after, then before, give way to the badge, which keeps its colours when selected
// Task 12: TextField gains Mask bool; var ErrMaskedInvalid error (a masked field's Err when Check fails)
// Task 12: Row gains Lines []string — logical read-only lines beside the label, wrapped by the row
// Task 12: const StackBelow = 44 — under this card inside width a row's label takes its own line
```

**C8 `internal/tui` pages (Tasks 14 to 19):** page constants in order `pageDashboard, pageRepos, pageRunners, pageHistory, pageSettings, pageDetail`; `pageNames = {"Dashboard", "Repositories", "Runners", "History", "Settings"}`. `type configPage struct` (Task 14) owns `form ui.Form`, `group ui.Group`, `scroll int`, `saving bool`, `alert []string`, `save, discard *ui.Button`; the Model owns `order *cfgOrder` (`type cfgOrder struct{ seq, shown int }`, a pointer because Model methods take value receivers), `func (m Model) nextCfgSeq() int`, `leaveFrom page`, and `func (m Model) configPage(p page) *configPage`. `savedMsg` and `refetchedMsg` gain `page page`. Task 13 adds `func stateBadge(state string) string` and changes the existing `func (m Model) selectedRow(id, s, buttons, tail string, w int) string` (`view.go:110`) to `func (m Model) selectedRow(id, before, badge, after, buttons, tail string, w int) string`. Task 15 adds `type reposPage struct` (held by pointer as `Model.repos`; selection `selected string`, `idx int`; buttons `add`, `pause`, `remove`; IDs `reposList`, `reposAdd`, `reposPause`, `reposRemove`; `reposListW = 30`), `repoIndex() int`, `selectedRepoStatus() *model.RepoStatus`, `moveRepo(d int)`, `repoState(model.RepoStatus) string`, `reposAction(id string)`. Task 16 adds repo field keys `repos/<name>/<field>` (fields `max`, `warm`, `labels`, `cleanup`) built by `reposKey(name, field string) string` and split by `splitReposKey`; `reposSpecs`, `saveRepos`, IDs `reposSave`/`reposDiscard`, `syncRepoControls()` (runs before every key, click and frame), `reposPanelLines(r, w) ([]string, map[string]ui.Range)` (every panel control has a range), `reposPanelSize(w, h) (int, int)`, `reposScrollToFocus()`, `runnersGet(name string) []string`, `reposPage.shownFocus`. Entering `pageRepos` fetches the config (Task 16) and, from Task 23, reloads the cards. Task 18 deletes the Settings `repoKey`.

**C9 `internal/tui` management (Tasks 20 to 26):** `fakeClient` (Task 20) gains `token`, `tokenErr`, `setTokenErr`, `regs`, `regsByRepo`, `regErr`, `reads`, `labels`, `labelsBy`, `labelErr`, `labelGetErr`, `warnings`, `reloadErr`, `pruneErr`, `metrics`, `metricsErr` (Task 26 adds `histErr`). `type manageState struct`, held as `Model.mg *manageState` (Task 21; later tasks add fields). `errText(err error) string` (Task 21) is a card's error text with `, try again after HH:MM` when the `api.Error` has `RetryAt`. Task 23 adds `repoSelected() tea.Cmd` (fetches every card of the selected repo; Tasks 24 and 26 extend it), `refreshRepoCards() tea.Cmd` (refetches when the selection no longer names `mg.cardsRepo`; called on list keys and clicks, on `statusMsg` while on the page, and on entering the page), `regCard(w) ([]string, map[string]ui.Range)`, `regDelID`, `reposRegRefresh`. Every card reply is applied only when its sequence number is the latest and its repo is the current selection. Task 24 adds `repoActionable() bool` (connected, not degraded, repo not removing; gates Check now, quick-add, Refresh and Delete), `lcCard(name, w) ([]string, map[string]ui.Range)`, `lcAddID(label)`/`lcAddLabel(id)` (hex-encoded label), `pollLabelCheck() tea.Cmd` (one read in flight). Task 26 adds `activityCard(w) ([]string, map[string]ui.Range)` and `reposActRetry`.

## Assumptions (evidence)

- The LXC exposes cgroup v2 `cpu.stat` with `usage_usec`, `cpu.max` reads `max 100000`, `memory.max` reads `max`, and lxcfs `/proc/meminfo` reports the container's 8 GB (ran over ssh, 2026-10-05).
- GitHub sends `GitHub-Authentication-Token-Expiration` on responses for tokens with an expiry ([docs](https://docs.github.com/en/rest/authentication/authenticating-to-the-rest-api), read 2026-10-05); value format `2026-12-31 23:59:59 UTC` — unverified, Task 1 parses leniently and Task 29 Step 2 prints the live header (only that header) and stops the release if its format is not one `parseExpiry` accepts.
- GitHub's runner listing returns `labels` as objects with `name` ([docs](https://docs.github.com/en/rest/actions/self-hosted-runners#list-self-hosted-runners-for-a-repository), read 2026-10-05).
- ghr names runners `ghr-<repo>-<id>` (`internal/runner/lifecycle.go:77`) and reconciliation owns that namespace (`internal/runner/adopt.go:136-158`).
- A 403 without `X-RateLimit-Remaining: 0` or `Retry-After` is classified `ErrAuth` today (`internal/github/client.go:239-259`).
- `sched.MatchLabels(job, effective)` is the daemon's demand predicate (`internal/runner/tick.go:124`) and returns true for an empty job label set (`internal/sched/sched.go:50-61`).
- The TUI's daemon calls time out after 5 s (`internal/tui/model.go:201-203`).
- Today's save-and-leave navigates even when an edit landed during the save (`internal/tui/settings.go` `refetched`, `if leaving { return m.goTo(m.leaveTo) }`).
- Selected rows strip all styling (`internal/tui/view.go:80-90,110-123`).
- Narrow layout (under 100 columns): the owner chose, on 2026-10-05, the stacked list card (a few rows tall, scrolling, above the panel) over spec §2's `Repo [ name ▾ ]` dropdown; spec §2 was updated to match. Tasks 15 and 16 implement the list card.
- `plan-lint` lists Task 29 among the delegated heavy tasks; the controlling session runs it anyway, because its Step 3 needs your human partner's approval and its Step 1 is the execution skill's own final review.
- Line numbers and code quoted from `internal/tui` reflect master `9fa16fe`; Tasks 13 to 28 each edit code that earlier tasks of this plan changed, so each task quotes the code as the earlier tasks leave it.

## Task index

1. GitHub client token metadata and secondary rate limits
2. GitHub client recent runs and runner labels
3. API error mapping and new model types
4. Maintenance prune in the runner manager
5. Reload and prune endpoints
6. Token endpoint
7. Registration endpoints
8. Label-check store
9. Label-check endpoints
10. Metrics sampler and endpoint
11. Badge, sparkline and gauge widgets
12. Masked text field and multi-line rows
13. Badges in existing tables
14. Shared config page and one config stream
15. Repositories page and navigation
16. Repositories editor and save
17. Leave guard for both config pages
18. Settings without repo cards
19. Dashboard Edit and Add repository
20. TUI client additions
21. Token section and replacement dialog
22. Maintenance section
23. Registrations card
24. Label check card
25. Dashboard metric tiles
26. Repositories Activity card
27. History bars and badges
28. Help in four columns
29. Release and LXC acceptance

---

### Task 1: GitHub client token metadata and secondary rate limits

**Files:**
- Modify: `internal/github/client.go` (`Client` struct, `ForgetCache`, `do`, `classify`)
- Test: `internal/github/client_test.go`

**Interfaces:**
- Consumes: none.
- Produces: C1 `TokenMeta`, `Client.TokenMeta()`, `ForgetCache` generation bump, secondary rate-limit classification.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 0: Branch**

Run (ghr root): `git switch -c feat/repos-management master && git log --oneline -1`
Expected: `9fa16fe fix(tui): complete help and footer key hints`.

- [ ] **Step 1: Write the failing tests** (append to `internal/github/client_test.go`; add `"errors"` and `"strconv"` to its imports; `"sync"` is already there)

```go
func TestTokenMetaRecorded(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Reset", "1798761600")
		w.Header().Set("GitHub-Authentication-Token-Expiration", "2026-12-31 23:59:59 UTC")
		fmt.Fprint(w, `{"full_name":"darkraise/darkcloud","private":true}`)
	})
	if m := c.TokenMeta(); !m.CheckedAt.IsZero() || m.RateLimit != nil {
		t.Fatalf("meta before any call: %+v", m)
	}
	if _, err := c.GetRepo(context.Background(), "darkcloud"); err != nil {
		t.Fatal(err)
	}
	m := c.TokenMeta()
	if !m.OK || m.CheckedAt.IsZero() || *m.RateRemaining != 4999 || *m.RateLimit != 5000 || m.RateReset.Unix() != 1798761600 {
		t.Fatalf("meta %+v", m)
	}
	if want := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC); m.ExpiresAt == nil || !m.ExpiresAt.Equal(want) {
		t.Fatalf("expires %v", m.ExpiresAt)
	}
	gen := m.Generation
	c.ForgetCache()
	if m := c.TokenMeta(); m.Generation != gen+1 || !m.CheckedAt.IsZero() || m.ExpiresAt != nil || c.RateRemaining() != -1 {
		t.Fatalf("after ForgetCache: %+v remaining %d", m, c.RateRemaining())
	}
}

func TestTokenMetaRejectedAndUnparsedExpiry(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("GitHub-Authentication-Token-Expiration", "soon")
		w.WriteHeader(401)
		fmt.Fprint(w, `{"message":"Bad credentials"}`)
	})
	if _, err := c.GetRepo(context.Background(), "darkcloud"); !IsKind(err, ErrAuth) {
		t.Fatalf("err %v", err)
	}
	if m := c.TokenMeta(); m.OK || m.CheckedAt.IsZero() || m.ExpiresAt != nil {
		t.Fatalf("meta %+v", m)
	}
}

// A response to a request sent with the previous token never updates the
// metadata of the new one.
func TestTokenMetaIgnoresOlderGeneration(t *testing.T) {
	release := make(chan struct{})
	arrived := make(chan struct{})
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-release
		w.Header().Set("X-RateLimit-Remaining", "10")
		fmt.Fprint(w, `{"full_name":"darkraise/darkcloud"}`)
	})
	done := make(chan error)
	go func() { _, err := c.GetRepo(context.Background(), "darkcloud"); done <- err }()
	<-arrived
	c.ForgetCache()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if m := c.TokenMeta(); !m.CheckedAt.IsZero() || m.RateRemaining != nil || c.RateRemaining() != -1 {
		t.Fatalf("old response recorded: %+v", m)
	}
}

func TestSecondaryRateLimitIsNotAuth(t *testing.T) {
	msg := `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, msg)
	})
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	c.Now = func() time.Time { return now }
	_, err := c.GetRepo(context.Background(), "darkcloud")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Kind != ErrRateLimit || !ae.RetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("err %#v", err)
	}
	if m := c.TokenMeta(); !m.CheckedAt.IsZero() {
		t.Fatalf("a rate limit says nothing about the token: %+v", m)
	}
	msg = `{"message":"Resource not accessible by personal access token"}`
	c.ForgetCache()
	if _, err := c.GetRepo(context.Background(), "darkcloud"); !IsKind(err, ErrAuth) {
		t.Fatalf("permission error: %v", err)
	}
}

// Only a success or an authentication failure is a verdict on the token; a
// server error, a 404 or a rate limit leaves the last verdict in place.
func TestTokenMetaOnlyCountsTokenVerdicts(t *testing.T) {
	status := 200
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(4000+status))
		w.WriteHeader(status)
		fmt.Fprint(w, `{"full_name":"darkraise/darkcloud","message":"x"}`)
	})
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	c.Now = func() time.Time { return now }
	if _, err := c.GetRepo(context.Background(), "darkcloud"); err != nil {
		t.Fatal(err)
	}
	first := c.TokenMeta()
	for _, status = range []int{500, 404} {
		now = now.Add(time.Minute)
		if _, err := c.GetRepo(context.Background(), "darkcloud"); err == nil {
			t.Fatalf("status %d: no error", status)
		}
		m := c.TokenMeta()
		if !m.OK || !m.CheckedAt.Equal(first.CheckedAt) || *m.RateRemaining != 4000+status {
			t.Fatalf("after %d: %+v", status, m)
		}
	}
	status = 401
	now = now.Add(time.Minute)
	c.GetRepo(context.Background(), "darkcloud")
	if m := c.TokenMeta(); m.OK || !m.CheckedAt.Equal(now) {
		t.Fatalf("after 401: %+v", m)
	}
}

// The daemon stores a new token before ForgetCache bumps the generation, so
// a request that read the old token just before a replacement must not be
// recorded against the new generation.
func TestTokenReplacedDuringRequest(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "10")
		fmt.Fprint(w, `{"full_name":"darkraise/darkcloud"}`)
	})
	cur, replaced := "old", false
	c.Token = func() string {
		tok := cur
		if !replaced {
			replaced = true
			cur = "new"
			c.ForgetCache()
		}
		return tok
	}
	if _, err := c.GetRepo(context.Background(), "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if m := c.TokenMeta(); m.Generation != 1 || !m.CheckedAt.IsZero() || m.RateRemaining != nil {
		t.Fatalf("old-token response recorded in the new generation: %+v", m)
	}
}

// A delayed response sent with the previous token restores neither a
// rate-limit suspension nor an ETag cache entry.
func TestOlderGenerationLeavesCacheAndSuspension(t *testing.T) {
	var mu sync.Mutex
	calls, inm := 0, ""
	release := make(chan struct{})
	arrived := make(chan struct{})
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		inm = r.Header.Get("If-None-Match")
		mu.Unlock()
		switch n {
		case 1:
			close(arrived)
			<-release
			w.Header().Set("ETag", `"v1"`)
			fmt.Fprint(w, `{"full_name":"darkraise/darkcloud"}`)
		case 2:
			fmt.Fprint(w, `{"full_name":"darkraise/darkcloud"}`)
		}
	})
	done := make(chan error)
	go func() { _, err := c.GetRepo(context.Background(), "darkcloud"); done <- err }()
	<-arrived
	c.ForgetCache()
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetRepo(context.Background(), "darkcloud"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if inm != "" {
		t.Fatalf("old-generation ETag reused: %q", inm)
	}
	mu.Unlock()

	release2 := make(chan struct{})
	arrived2 := make(chan struct{})
	c2, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		close(arrived2)
		<-release2
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "4102444800")
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
	})
	go func() { _, err := c2.GetRepo(context.Background(), "darkcloud"); done <- err }()
	<-arrived2
	c2.ForgetCache()
	close(release2)
	if err := <-done; !IsKind(err, ErrRateLimit) {
		t.Fatalf("err %v", err)
	}
	if until := c2.SuspendedUntil(); !until.IsZero() {
		t.Fatalf("old-generation rate limit suspended the new token until %v", until)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 120 go test ./internal/github -run 'TokenMeta|SecondaryRateLimit'`
Expected: build failure, `c.TokenMeta undefined`.

- [ ] **Step 3: Write the implementation** (`internal/github/client.go`)

Add the type after `JITConfig`:

```go
// TokenMeta is what GitHub's responses said about the current token. Each
// pointer is nil until a response carried that header.
type TokenMeta struct {
	Generation    uint64
	CheckedAt     time.Time
	OK            bool
	RateRemaining *int
	RateLimit     *int
	RateReset     *time.Time
	ExpiresAt     *time.Time
}
```

Add two fields to `Client`, after `retryAt    time.Time`:

```go
	gen        uint64 // bumped when the token changes; older responses are not recorded
	meta       TokenMeta
```

Replace `ForgetCache` and add `TokenMeta`, `record` and `parseExpiry`:

```go
// ForgetCache drops ETag state, any rate-limit suspension and the token
// metadata, used after the token changes.
func (c *Client) ForgetCache() {
	c.mu.Lock()
	c.cache = nil
	c.retryAt = time.Time{}
	c.gen++
	c.meta = TokenMeta{}
	c.remaining = -1
	c.mu.Unlock()
}

// TokenMeta returns the metadata recorded for the current token.
func (c *Client) TokenMeta() TokenMeta {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.meta
	m.Generation = c.gen
	return m
}

// record stores the response's rate and expiry headers, unless the token
// changed while the request was in flight. Only a success or an
// authentication failure is a verdict on the token; other failures leave
// CheckedAt and OK as they were.
func (c *Client) record(gen uint64, h http.Header, apiErr *APIError) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	if n, err := strconv.Atoi(h.Get("X-RateLimit-Remaining")); err == nil {
		c.remaining = n
		c.meta.RateRemaining = &n
	}
	if n, err := strconv.Atoi(h.Get("X-RateLimit-Limit")); err == nil {
		c.meta.RateLimit = &n
	}
	if n, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		t := time.Unix(n, 0)
		c.meta.RateReset = &t
	}
	if t, ok := parseExpiry(h.Get("GitHub-Authentication-Token-Expiration")); ok {
		c.meta.ExpiresAt = &t
	}
	switch {
	case apiErr == nil:
		c.meta.CheckedAt, c.meta.OK = c.Now(), true
	case apiErr.Kind == ErrAuth:
		c.meta.CheckedAt, c.meta.OK = c.Now(), false
	}
}

// parseExpiry reads GitHub's token expiry header, "2026-12-31 23:59:59 UTC".
func parseExpiry(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700", time.RFC3339} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
```

In `do`, capture the generation before the token is read. Replace:

```go
	req.Header.Set("Authorization", "Bearer "+c.Token())
```

with:

```go
	// SetToken and Reload store the new token before ForgetCache bumps the
	// generation, so reading the generation first means a request may carry
	// a newer token than its generation (its response is then dropped) but
	// never an older one.
	c.mu.Lock()
	gen := c.gen
	c.mu.Unlock()
	req.Header.Set("Authorization", "Bearer "+c.Token())
```

Replace the remaining-header block that follows `io.ReadAll`:

```go
	if rem := resp.Header.Get("X-RateLimit-Remaining"); rem != "" {
		if n, err := strconv.Atoi(rem); err == nil {
			c.mu.Lock()
			c.remaining = n
			c.mu.Unlock()
		}
	}
```

with:

```go
	var apiErr *APIError
	if resp.StatusCode >= 300 && !(resp.StatusCode == http.StatusNotModified && hasPrev) {
		apiErr = c.classify(resp, data)
	}
	c.record(gen, resp.Header, apiErr)
```

Further down, replace the error block and the ETag cache write:

```go
	if resp.StatusCode >= 300 {
		err := c.classify(resp, data)
		if err.Kind == ErrRateLimit {
			c.mu.Lock()
			if err.RetryAt.After(c.retryAt) {
				c.retryAt = err.RetryAt
			}
			c.mu.Unlock()
		}
		return nil, "", err
	}
	if method == http.MethodGet {
		if etag := resp.Header.Get("ETag"); etag != "" {
			c.mu.Lock()
			// Run and job URLs are unbounded over time; reset rather than track LRU.
			if c.cache == nil || len(c.cache) >= maxCacheEntries {
				c.cache = map[string]cached{}
			}
			c.cache[u] = cached{etag: etag, body: data, next: next}
			c.mu.Unlock()
		}
	}
```

with (a response from an older generation never writes client state):

```go
	if apiErr != nil {
		err := apiErr
		if err.Kind == ErrRateLimit {
			c.mu.Lock()
			if gen == c.gen && err.RetryAt.After(c.retryAt) {
				c.retryAt = err.RetryAt
			}
			c.mu.Unlock()
		}
		return nil, "", err
	}
	if method == http.MethodGet {
		if etag := resp.Header.Get("ETag"); etag != "" {
			c.mu.Lock()
			if gen == c.gen {
				// Run and job URLs are unbounded over time; reset rather than track LRU.
				if c.cache == nil || len(c.cache) >= maxCacheEntries {
					c.cache = map[string]cached{}
				}
				c.cache[u] = cached{etag: etag, body: data, next: next}
			}
			c.mu.Unlock()
		}
	}
```

In `classify`, inside the `403 || 429` case, replace the last two branches:

```go
		} else if resp.StatusCode == 429 {
			e.Kind = ErrRateLimit
			e.RetryAt = c.Now().Add(time.Minute)
		} else {
			e.Kind = ErrAuth
		}
```

with:

```go
		} else if resp.StatusCode == 429 || strings.Contains(strings.ToLower(msg.Message), "secondary rate limit") {
			e.Kind = ErrRateLimit
			e.RetryAt = c.Now().Add(time.Minute)
		} else {
			e.Kind = ErrAuth
		}
```

Add `"strings"` to the imports if it is not there.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 200 go test ./internal/github/ ./internal/runner/ ./internal/daemon/`
Expected: `ok` for all three; the existing rate-remaining and classification tests still pass.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/github
git add internal/github
git commit -m "feat(github): record token metadata per generation"
```

### Task 2: GitHub client recent runs and runner labels

**Files:**
- Modify: `internal/github/client.go` (`Runner`, new `Label`, new `ListRecentRuns`)
- Test: `internal/github/client_test.go`

**Interfaces:**
- Consumes: none.
- Produces: C1 `Label`, `Runner.Labels`, `ListRecentRuns`.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests** (append to `internal/github/client_test.go`)

```go
func TestListRecentRunsTakesOnePage(t *testing.T) {
	var base string
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, base, r.URL.RequestURI()))
		fmt.Fprint(w, `{"workflow_runs":[{"id":7,"status":"completed"},{"id":6,"status":"completed"}]}`)
	})
	base = c.BaseURL
	runs, err := c.ListRecentRuns(context.Background(), "darkcloud", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || len(f.requests) != 1 || !strings.HasSuffix(f.requests[0], "/repos/darkraise/darkcloud/actions/runs?per_page=20") {
		t.Fatalf("runs %+v requests %v", runs, f.requests)
	}
}

func TestRunnerLabelsDecoded(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"runners":[{"id":3,"name":"linux-1","status":"offline","busy":false,
			"labels":[{"id":1,"name":"self-hosted","type":"read-only"},{"id":9,"name":"darkcloud-linux","type":"custom"}]}]}`)
	})
	rs, err := c.ListRunners(context.Background(), "darkcloud")
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || len(rs[0].Labels) != 2 || rs[0].Labels[1].Name != "darkcloud-linux" {
		t.Fatalf("runners %+v", rs)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 120 go test ./internal/github -run 'RecentRuns|RunnerLabels'`
Expected: build failure, `c.ListRecentRuns undefined`.

- [ ] **Step 3: Write the implementation** (`internal/github/client.go`)

Replace the `Runner` type:

```go
type Label struct {
	Name string `json:"name"`
}

type Runner struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Status string  `json:"status"` // online | offline
	Busy   bool    `json:"busy"`
	Labels []Label `json:"labels"`
}
```

Add after `ListRuns`:

```go
// ListRecentRuns lists the repo's n most recent workflow runs of any status,
// one page only.
func (c *Client) ListRecentRuns(ctx context.Context, repo string, n int) ([]Run, error) {
	data, _, err := c.do(ctx, http.MethodGet, c.repoURL(repo, fmt.Sprintf("/actions/runs?per_page=%d", n)), nil)
	if err != nil {
		return nil, err
	}
	var page struct {
		WorkflowRuns []Run `json:"workflow_runs"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, err
	}
	return page.WorkflowRuns, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 200 go test ./internal/github/ ./internal/runner/`
Expected: `ok` for both.

- [ ] **Step 5: Commit**

```bash
git add internal/github
git commit -m "feat(github): list recent runs and runner labels"
```

### Task 3: API error mapping and new model types

**Files:**
- Modify: `internal/model/model.go` (add the C2 types; `Status` gains `Maintenance`)
- Modify: `internal/api/server.go` (`Error.RetryAt`, `FromGitHub`, `respond`)
- Modify: `internal/api/client.go` (`call` decodes `retry_at`)
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: none.
- Produces: C2 types; C3 `Error.RetryAt`, `FromGitHub`, the error body format.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test** (`internal/api/api_test.go`)

Add a field to `fakeBackend`: `killErr error`. Replace its `KillRunner`:

```go
func (f *fakeBackend) KillRunner(ctx context.Context, id string) error {
	f.killed = append(f.killed, id)
	return f.killErr
}
```

Append the test (add `"github.com/darkraise/ghr/internal/github"` to the imports):

```go
func TestGitHubErrorsMapToStatuses(t *testing.T) {
	c, b := setup(t)
	retry := time.Date(2026, 10, 5, 12, 1, 0, 0, time.UTC)
	for _, tc := range []struct {
		err    error
		status int
		retry  bool
	}{
		{&github.APIError{Status: 403, Kind: github.ErrRateLimit, Message: "rate", RetryAt: retry}, 429, true},
		{&github.APIError{Status: 401, Kind: github.ErrAuth, Message: "Bad credentials"}, 403, false},
		{&github.APIError{Status: 404, Kind: github.ErrNotFound, Message: "Not Found"}, 404, false},
		{&github.APIError{Status: 422, Kind: github.ErrUnprocessable, Message: "busy"}, 409, false},
		{&github.APIError{Status: 503, Kind: github.ErrServer, Message: "down"}, 502, false},
		{errors.New("plain"), 500, false},
		{Conflict("already"), 409, false},
	} {
		b.killErr = tc.err
		err := c.Kill(context.Background(), "aaaaaa")
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != tc.status || ae.RetryAt.Equal(retry) != tc.retry {
			t.Errorf("%v: got %#v", tc.err, err)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 120 go test ./internal/api -run GitHubErrorsMapToStatuses`
Expected: FAIL (`ae.RetryAt undefined` build error).

- [ ] **Step 3: Write the implementation**

`internal/model/model.go`: add to `Status`, after `Instances`:

```go
	Maintenance    MaintenanceStatus `json:"maintenance"`
```

and append the C2 types exactly as the Contracts section writes them (`MaintenanceStatus`, `TokenStatus`, `Registration`, `LabelGroup`, `LabelCheck`, `MetricSample`, `Metrics`), each with a one-line doc comment naming the endpoint that serves it.

`internal/api/server.go`: replace the `Error` type and add `FromGitHub` (add `"time"` and `"github.com/darkraise/ghr/internal/github"` to the imports):

```go
// Error carries an HTTP status to the client; RetryAt is set on 429.
type Error struct {
	Status  int
	Msg     string
	RetryAt time.Time
}

// FromGitHub gives a GitHub API error the status the daemon answers with.
// Other errors are returned unchanged.
func FromGitHub(err error) error {
	var ae *Error
	if errors.As(err, &ae) {
		return err
	}
	var ge *github.APIError
	if !errors.As(err, &ge) {
		return err
	}
	switch ge.Kind {
	case github.ErrRateLimit:
		return &Error{Status: http.StatusTooManyRequests, Msg: err.Error(), RetryAt: ge.RetryAt}
	case github.ErrAuth:
		return &Error{Status: http.StatusForbidden, Msg: err.Error()}
	case github.ErrNotFound:
		return &Error{Status: http.StatusNotFound, Msg: err.Error()}
	case github.ErrUnprocessable:
		return &Error{Status: http.StatusConflict, Msg: err.Error()}
	}
	return &Error{Status: http.StatusBadGateway, Msg: err.Error()}
}
```

Replace `respond`:

```go
func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		err = FromGitHub(err)
		status := http.StatusInternalServerError
		body := map[string]string{"error": err.Error()}
		var ae *Error
		if errors.As(err, &ae) {
			status = ae.Status
			if !ae.RetryAt.IsZero() {
				body["retry_at"] = ae.RetryAt.UTC().Format(time.RFC3339)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
		return
	}
	if v == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, v)
}
```

`internal/api/client.go`, in `call`, replace the error decoding:

```go
		var e struct {
			Error   string    `json:"error"`
			RetryAt time.Time `json:"retry_at"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return &Error{Status: resp.StatusCode, Msg: e.Error, RetryAt: e.RetryAt}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/api/ ./internal/daemon/ ./internal/tui/ ./cmd/...`
Expected: `ok` for every package.

- [ ] **Step 5: Commit**

```bash
git add internal/model internal/api
git commit -m "feat(api): map GitHub errors and carry retry time"
```

### Task 4: Maintenance prune in the runner manager

**Files:**
- Modify: `internal/runner/manager.go` (fields, `Init`, `Status`, new `StartPrune`, `Maintenance`, `Close`)
- Modify: `internal/runner/cleanup.go` (`prune` returns success and writes `lastPrune` under `mu`; new `forcedPrune`)
- Modify: `internal/runner/tick.go` (automatic pruning takes the same reservation as a manual prune)
- Test: `internal/runner/maintenance_test.go` (new), `internal/runner/fakes_test.go` (a `hold` hook that blocks fake Docker calls)

**Interfaces:**
- Consumes: C2 `MaintenanceStatus` (Task 3).
- Produces: C4.

**Items:** 4

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

The maintenance state describes manual prunes only. Manual and automatic pruning (the disk check and the 24-hour retention prune) share one reservation, `m.pruning` under `m.mu`: whichever takes it first runs, the other is refused (manual) or skipped until a later tick (automatic). The automatic retention prune keeps its own `lastPrune`, now read and written under `m.mu`.

- [ ] **Step 1: Add a blocking hook to the fake Docker** (`internal/runner/fakes_test.go`)

Add a field to `fakeDocker`, after `errs`:

```go
	hold      func(ctx context.Context, method string) error // may block a call; set before the manager runs
```

Add after `setErr`:

```go
func (f *fakeDocker) wait(ctx context.Context, method string) error {
	f.mu.Lock()
	hold := f.hold
	f.mu.Unlock()
	if hold == nil {
		return nil
	}
	return hold(ctx, method)
}
```

Replace the three prune methods (a manual prune now runs on its own goroutine, so `prunes` is appended under `f.mu`):

```go
func (f *fakeDocker) PruneBuildCacheOlderThan(ctx context.Context, h int) (string, error) {
	f.mu.Lock()
	f.prunes = append(f.prunes, fmt.Sprintf("until=%dh", h))
	f.mu.Unlock()
	if err := f.wait(ctx, "PruneBuildCacheOlderThan"); err != nil {
		return "", err
	}
	if err := f.err("PruneBuildCacheOlderThan"); err != nil {
		return "", err
	}
	return "1GB", nil
}
func (f *fakeDocker) PruneBuildCacheTo(ctx context.Context, keep string) (string, error) {
	f.mu.Lock()
	f.prunes = append(f.prunes, "keep="+keep)
	f.mu.Unlock()
	if err := f.wait(ctx, "PruneBuildCacheTo"); err != nil {
		return "", err
	}
	if err := f.err("PruneBuildCacheTo"); err != nil {
		return "", err
	}
	return "5GB", nil
}
func (f *fakeDocker) PruneDanglingImages(ctx context.Context) (string, error) {
	f.mu.Lock()
	f.prunes = append(f.prunes, "images")
	f.mu.Unlock()
	if err := f.wait(ctx, "PruneDanglingImages"); err != nil {
		return "", err
	}
	if err := f.err("PruneDanglingImages"); err != nil {
		return "", err
	}
	return "200MB", nil
}

func (f *fakeDocker) pruneList() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.prunes, ",")
}
```

- [ ] **Step 2: Write the failing tests** (`internal/runner/maintenance_test.go`)

```go
package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// holdOn blocks the named fake Docker method until release is closed or the
// call's context ends, signalling entered when it starts.
func holdOn(h *harness, method string) (entered, release chan struct{}) {
	entered, release = make(chan struct{}), make(chan struct{})
	h.docker.hold = func(ctx context.Context, m string) error {
		if m != method {
			return nil
		}
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return entered, release
}

func waitOrFail(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestStartPruneRunsForcedSequence(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{40} // below the high-water mark: a forced prune runs anyway
	if err := h.m.StartPrune(); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if got := h.docker.pruneList(); got != "keep="+h.cfg.BuildCacheKeep+",images" {
		t.Fatalf("prunes %s", got)
	}
	ms := h.m.Maintenance()
	if ms.Running || ms.LastOutcome != "ok" || ms.LastStarted == nil || ms.LastFinished == nil {
		t.Fatalf("maintenance %+v", ms)
	}
	if st := h.m.Status(); st.Maintenance.LastOutcome != "ok" {
		t.Fatalf("status maintenance %+v", st.Maintenance)
	}
	txt := h.eventText()
	at := -1
	for _, want := range []string{
		"prune: build cache to " + h.cfg.BuildCacheKeep + " freed 5GB",
		"prune: dangling images freed 200MB",
		"prune: history and logs past retention removed",
		"prune: disk 40% used",
		"prune finished",
	} {
		i := strings.Index(txt, want)
		if i <= at {
			t.Fatalf("%q missing or out of order in events:\n%s", want, txt)
		}
		at = i
	}
}

// A manual prune holds the reservation: a tick that would prune for disk
// space and for retention does neither until it ends.
func TestTickSkipsPruningDuringManualPrune(t *testing.T) {
	h := newHarness(t)
	h.m.mu.Lock()
	h.m.lastPrune = h.now.Add(-48 * time.Hour)
	h.m.mu.Unlock()
	entered, release := holdOn(h, "PruneBuildCacheTo")
	if err := h.m.StartPrune(); err != nil {
		t.Fatal(err)
	}
	waitOrFail(t, entered, "the manual prune")
	if err := h.m.StartPrune(); !errors.Is(err, ErrPruneRunning) {
		t.Fatalf("second StartPrune: %v", err)
	}
	h.docker.mu.Lock()
	h.docker.usage = []int{99} // above high-water: an unreserved disk check would prune
	h.docker.mu.Unlock()
	h.m.ticks = 0 // the next tick is tick 1, when checkDisk runs
	h.m.Tick(context.Background())
	if got := h.docker.pruneList(); got != "keep="+h.cfg.BuildCacheKeep {
		t.Fatalf("automatic pruning ran during a manual prune: %s", got)
	}
	h.m.mu.Lock()
	last := h.m.lastPrune
	h.m.mu.Unlock()
	if !last.Equal(h.now.Add(-48 * time.Hour)) {
		t.Fatal("the retention prune ran during a manual prune")
	}
	close(release)
	h.m.Wait()
	if ms := h.m.Maintenance(); ms.Running || ms.LastOutcome != "ok" {
		t.Fatalf("maintenance %+v", ms)
	}
}

// An automatic prune holds the reservation: StartPrune is refused until the
// tick ends, then accepted.
func TestStartPruneRefusedDuringAutomaticPrune(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{99, 99, 50}
	entered, release := holdOn(h, "PruneBuildCacheOlderThan")
	h.m.ticks = 0
	ticked := make(chan struct{})
	go func() {
		h.m.Tick(context.Background())
		close(ticked)
	}()
	waitOrFail(t, entered, "the automatic prune")
	if err := h.m.StartPrune(); !errors.Is(err, ErrPruneRunning) {
		t.Fatalf("StartPrune during an automatic prune: %v", err)
	}
	if ms := h.m.Maintenance(); ms.Running || ms.LastStarted != nil {
		t.Fatalf("a refused StartPrune changed the state: %+v", ms)
	}
	close(release)
	waitOrFail(t, ticked, "the tick")
	if err := h.m.StartPrune(); err != nil {
		t.Fatalf("StartPrune after the tick: %v", err)
	}
	h.m.Wait()
}

func TestPruneErrors(t *testing.T) {
	h := newHarness(t)
	h.docker.errs["PruneDanglingImages"] = errors.New("boom")
	if err := h.m.StartPrune(); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	ms := h.m.Maintenance()
	txt := h.eventText()
	if ms.LastOutcome != "errors" || !strings.Contains(txt, "prune: dangling images failed: boom") || !strings.Contains(txt, "prune finished with errors") {
		t.Fatalf("maintenance %+v events:\n%s", ms, txt)
	}
}

// Close cancels a Docker call already in flight; the prune then ends as
// interrupted, runs no further step, and Wait returns.
func TestCloseInterruptsRunningPrune(t *testing.T) {
	h := newHarness(t)
	entered, _ := holdOn(h, "PruneDanglingImages")
	if err := h.m.StartPrune(); err != nil {
		t.Fatal(err)
	}
	waitOrFail(t, entered, "the dangling-image prune")
	h.m.Close()
	waited := make(chan struct{})
	go func() {
		h.m.Wait()
		close(waited)
	}()
	waitOrFail(t, waited, "Wait after Close")
	ms := h.m.Maintenance()
	txt := h.eventText()
	if ms.Running || ms.LastOutcome != "interrupted" || !strings.Contains(txt, "prune interrupted by shutdown") {
		t.Fatalf("maintenance %+v events:\n%s", ms, txt)
	}
	if strings.Contains(txt, "history and logs past retention removed") || strings.Contains(txt, "prune: disk") {
		t.Fatalf("steps ran after the interruption:\n%s", txt)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 120 go test ./internal/runner -run 'Prune'`
Expected: build failure, `h.m.StartPrune undefined`.

- [ ] **Step 4: Write the implementation**

`internal/runner/manager.go`: add to `Manager`, after `lastPrune time.Time`:

```go
	pruning        bool                    // a manual or automatic prune holds the reservation; guarded by mu
	maint          model.MaintenanceStatus // manual prunes; guarded by mu
	maintCtx       context.Context         // cancelled by Close
	maintCancel    context.CancelFunc
```

In `Init`, after `m.lastPrune = m.Now()`:

```go
	m.maintCtx, m.maintCancel = context.WithCancel(context.Background())
```

In `Status`, add `Maintenance: m.maint,` to the `model.Status` literal.

Add (with `"errors"` imported):

```go
// ErrPruneRunning is returned by StartPrune while a prune runs.
var ErrPruneRunning = errors.New("a prune is already running")

// StartPrune starts a forced maintenance prune in the background. The caller
// returns before it finishes; Wait waits for it and Close interrupts it.
func (m *Manager) StartPrune() error {
	m.mu.Lock()
	if m.pruning {
		m.mu.Unlock()
		return ErrPruneRunning
	}
	now := m.Now()
	m.pruning = true
	m.maint.Running = true
	m.maint.LastStarted = &now
	ctx := m.maintCtx
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		m.forcedPrune(ctx)
	}()
	return nil
}

// Maintenance reports the manual prune state.
func (m *Manager) Maintenance() model.MaintenanceStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maint
}

// Close interrupts a running prune; the daemon calls it on shutdown before Wait.
func (m *Manager) Close() {
	if m.maintCancel != nil {
		m.maintCancel()
	}
}

// reservePrune takes the reservation shared by manual and automatic pruning,
// reporting false when a prune already holds it.
func (m *Manager) reservePrune() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pruning {
		return false
	}
	m.pruning = true
	return true
}

func (m *Manager) releasePrune() {
	m.mu.Lock()
	m.pruning = false
	m.mu.Unlock()
}
```

`internal/runner/cleanup.go`: change `prune` to report success and write `lastPrune` under the lock:

```go
// prune drops history lines and archived logs older than history_retention,
// reporting whether every step succeeded.
func (m *Manager) prune(cfg *config.Config, now time.Time) bool {
	ok := true
	cutoff := now.Add(-cfg.HistoryRetention.D())
	if err := m.History.Prune(cutoff); err != nil {
		ok = false
		m.Events.Add("warn", "", "history prune: %v", err)
	}
	entries, err := os.ReadDir(m.Paths.Logs)
	if err != nil && !os.IsNotExist(err) {
		ok = false
		m.Events.Add("warn", "", "log archive prune: %v", err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(filepath.Join(m.Paths.Logs, e.Name())); err != nil {
				ok = false
				m.Events.Add("warn", "", "log archive prune: %v", err)
			}
		}
	}
	m.mu.Lock()
	m.lastPrune = now
	m.mu.Unlock()
	return ok
}

// forcedPrune is a manual prune: build cache down to build_cache_keep,
// dangling images, history and logs past retention, then a fresh disk
// reading. Unlike checkDisk it ignores disk_high_water.
func (m *Manager) forcedPrune(ctx context.Context) {
	cfg := m.Config()
	failed := false
	step := func(name string, fn func() (string, error)) {
		if ctx.Err() != nil {
			return
		}
		freed, err := fn()
		if err != nil {
			failed = true
			m.Events.Add("warn", "", "prune: %s failed: %v", name, err)
			return
		}
		m.Events.Add("info", "", "prune: %s freed %s", name, freed)
	}
	step("build cache to "+cfg.BuildCacheKeep, func() (string, error) { return m.Docker.PruneBuildCacheTo(ctx, cfg.BuildCacheKeep) })
	step("dangling images", func() (string, error) { return m.Docker.PruneDanglingImages(ctx) })
	if ctx.Err() == nil {
		if m.prune(cfg, m.Now()) {
			m.Events.Add("info", "", "prune: history and logs past retention removed")
		} else {
			failed = true
		}
	}
	if ctx.Err() == nil {
		if pct, err := m.Docker.DataRootUsage(ctx); err != nil {
			failed = true
			m.Events.Add("warn", "", "prune: disk usage: %v", err)
		} else {
			m.setDisk(pct)
			m.Events.Add("info", "", "prune: disk %d%% used", pct)
		}
	}
	outcome, level, msg := "ok", "ok", "prune finished"
	switch {
	case ctx.Err() != nil:
		outcome, level, msg = "interrupted", "warn", "prune interrupted by shutdown"
	case failed:
		outcome, level, msg = "errors", "warn", "prune finished with errors"
	}
	now := m.Now()
	m.mu.Lock()
	m.pruning = false
	m.maint.Running = false
	m.maint.LastFinished = &now
	m.maint.LastOutcome = outcome
	m.mu.Unlock()
	m.Events.Add(level, "", "%s", msg)
}
```

`step` reports a failed step as a warning whatever the cause; a step cancelled by `Close` is then also covered by the final "prune interrupted by shutdown" event.

`internal/runner/tick.go`: replace the last two `if` blocks of `Tick`:

```go
	if m.ticks%10 == 1 {
		m.checkDisk(ctx, cfg)
	}
	if now.Sub(m.lastPrune) >= 24*time.Hour {
		m.prune(cfg, now)
	}
```

with:

```go
	diskDue := m.ticks%10 == 1
	m.mu.Lock()
	retentionDue := now.Sub(m.lastPrune) >= 24*time.Hour
	m.mu.Unlock()
	// Manual and automatic pruning share one reservation, so neither runs
	// Docker's prune commands or rewrites the history file under the other;
	// a skipped automatic prune runs on a later tick.
	if (diskDue || retentionDue) && m.reservePrune() {
		func() {
			defer m.releasePrune()
			if diskDue {
				m.checkDisk(ctx, cfg)
			}
			if retentionDue {
				m.prune(cfg, now)
			}
		}()
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/runner/ ./internal/daemon/`
Expected: `ok` for both, including the existing disk and prune tests.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/runner
git add internal/runner
git commit -m "feat(runner): run a forced maintenance prune"
```

### Task 5: Reload and prune endpoints

**Files:**
- Modify: `internal/daemon/backend.go` (`Manager` interface gains `StartPrune() error`; new `Reload`, `Prune`)
- Modify: `internal/daemon/run.go` (SIGHUP calls `b.Reload()`; shutdown calls `m.Close()` before `m.Wait()`)
- Modify: `internal/api/server.go` (`Backend` gains `Reload() ([]string, error)` and `Prune() error`; routes `POST /reload`, `POST /prune`)
- Modify: `internal/api/client.go` (`Reload`, `Prune`)
- Test: `internal/daemon/backend_test.go`, `internal/api/api_test.go`

**Interfaces:**
- Consumes: C4 `StartPrune`, `ErrPruneRunning`, `Close` (Task 4); C3 error mapping (Task 3).
- Produces: C3 `Reload`, `Prune` client methods and routes; C5 `Backend.Reload`.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

`internal/daemon/backend_test.go`: add to `fakeManager` the fields `pruneErr error` and `prunes int`, and the method:

```go
func (f *fakeManager) StartPrune() error {
	f.prunes++
	return f.pruneErr
}
```

Append:

```go
func TestReloadAppliesWakesAndRejects(t *testing.T) {
	b, m, gh := newBackend(t)
	woke := 0
	b.Wake = func() { woke++ }
	ws, err := b.Reload()
	if err != nil || ws == nil || woke != 1 || !gh.forgot || !m.cleared {
		t.Fatalf("reload: %v %v woke=%d forgot=%v cleared=%v", ws, err, woke, gh.forgot, m.cleared)
	}
	if err := os.WriteFile(b.Store.TokenPath, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); apiStatus(err) != 400 || woke != 1 {
		t.Fatalf("empty token: %v woke=%d", err, woke)
	}
	var txt strings.Builder
	for _, e := range b.Events.After(0) {
		txt.WriteString(e.Msg + "\n")
	}
	if !strings.Contains(txt.String(), "config and token reloaded") || !strings.Contains(txt.String(), "reload rejected") {
		t.Fatalf("events:\n%s", txt.String())
	}
}

func TestPruneStartsOrConflicts(t *testing.T) {
	b, m, _ := newBackend(t)
	if err := b.Prune(); err != nil || m.prunes != 1 {
		t.Fatalf("prune: %v %d", err, m.prunes)
	}
	m.pruneErr = runner.ErrPruneRunning
	if err := b.Prune(); apiStatus(err) != 409 {
		t.Fatalf("overlap: %v", err)
	}
}
```

`internal/api/api_test.go`: add to `fakeBackend` the field `pruneErr error` and:

```go
func (f *fakeBackend) Reload() ([]string, error) { return []string{"labels: duplicate"}, nil }
func (f *fakeBackend) Prune() error            { return f.pruneErr }
```

Append:

```go
func TestReloadAndPrune(t *testing.T) {
	c, b := setup(t)
	ws, err := c.Reload(context.Background())
	if err != nil || len(ws) != 1 || ws[0] != "labels: duplicate" {
		t.Fatalf("reload: %v %v", ws, err)
	}
	if err := c.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.pruneErr = Conflict("a prune is already running")
	var ae *Error
	if err := c.Prune(context.Background()); !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("overlap: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 200 go test ./internal/daemon ./internal/api -run 'Reload|Prune'`
Expected: build failures, `b.Reload undefined` and `c.Reload undefined`.

- [ ] **Step 3: Write the implementation**

`internal/daemon/backend.go`: add `StartPrune() error` to the `Manager` interface, and:

```go
// Reload re-reads config.yaml and the token file, as SIGHUP does. On any
// error the previous values stay active. It wakes the run loop, which ticks;
// it never ticks itself.
func (b *Backend) Reload() ([]string, error) {
	warnings, err := b.Store.Reload()
	if err != nil {
		b.Events.Add("error", "", "reload rejected, keeping previous config: %v", err)
		return nil, api.BadRequest(err.Error())
	}
	for _, w := range warnings {
		b.Events.Add("warn", "", "config: %s", w)
	}
	b.GH.ForgetCache()
	b.M.ClearDegraded()
	b.Events.Add("info", "", "config and token reloaded")
	if b.Wake != nil {
		b.Wake()
	}
	if warnings == nil {
		warnings = []string{}
	}
	return warnings, nil
}

// Prune starts a forced maintenance prune; its progress goes to the events.
func (b *Backend) Prune() error {
	err := b.M.StartPrune()
	if errors.Is(err, runner.ErrPruneRunning) {
		return api.Conflict(err.Error())
	}
	return err
}
```

`internal/daemon/run.go`: replace the whole `case <-reload:` body with:

```go
		case <-reload:
			b.Reload()
```

and in the `case <-ctx.Done():` body, insert `m.Close()` immediately before `done := make(chan struct{})`.

`internal/api/server.go`: add to the `Backend` interface:

```go
	Reload() ([]string, error)
	Prune() error
```

and register, next to `PUT /token`:

```go
	mux.HandleFunc("POST /reload", func(w http.ResponseWriter, r *http.Request) {
		ws, err := b.Reload()
		respond(w, ws, err)
	})
	mux.HandleFunc("POST /prune", func(w http.ResponseWriter, r *http.Request) {
		if err := b.Prune(); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
```

`internal/api/client.go`:

```go
// Reload makes the daemon re-read config.yaml and its token; it returns the config warnings.
func (c *Client) Reload(ctx context.Context) ([]string, error) {
	var ws []string
	err := c.call(ctx, http.MethodPost, "/reload", nil, &ws)
	return ws, err
}

// Prune starts a maintenance prune; its outcome arrives as events.
func (c *Client) Prune(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/prune", nil, nil)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ ./internal/api/ ./cmd/...`
Expected: `ok` for every package (the existing SIGHUP test in `run_test.go` still sees "config and token reloaded").

- [ ] **Step 5: Commit**

```bash
git add internal/daemon internal/api
git commit -m "feat(daemon): add reload and prune endpoints"
```

### Task 6: Token endpoint

**Files:**
- Modify: `internal/daemon/backend.go` (`GitHub` interface gains `TokenMeta() github.TokenMeta`; new `Token`)
- Modify: `internal/api/server.go` (`Backend` gains `Token() model.TokenStatus`; route `GET /token`)
- Modify: `internal/api/client.go` (`Token`)
- Test: `internal/daemon/backend_test.go`, `internal/api/api_test.go`

**Interfaces:**
- Consumes: C1 `TokenMeta` (Task 1); C2 `TokenStatus` (Task 3).
- Produces: C3 `GET /token`, `Client.Token`.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

`internal/daemon/backend_test.go`: add a field `degraded string` to `fakeManager` and change its `Status`:

```go
func (f *fakeManager) Status() model.Status {
	return model.Status{Instances: f.insts, Degraded: f.degraded != "", DegradedReason: f.degraded}
}
```

Add a field `meta github.TokenMeta` to `fakeGH` and:

```go
func (f *fakeGH) TokenMeta() github.TokenMeta { return f.meta }
```

Append:

```go
func TestTokenStates(t *testing.T) {
	b, m, gh := newBackend(t)
	if ts := b.Token(); ts.State != "unverified" || ts.CheckedAt != nil {
		t.Fatalf("fresh: %+v", ts)
	}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	exp := now.Add(80 * 24 * time.Hour)
	rem := 4800
	gh.meta = github.TokenMeta{CheckedAt: now, OK: true, RateRemaining: &rem, ExpiresAt: &exp}
	if ts := b.Token(); ts.State != "ok" || !ts.CheckedAt.Equal(now) || *ts.RateRemaining != 4800 || !ts.ExpiresAt.Equal(exp) {
		t.Fatalf("ok: %+v", ts)
	}
	m.degraded = "GitHub rejected the token"
	if ts := b.Token(); ts.State != "rejected" || ts.Reason != "GitHub rejected the token" {
		t.Fatalf("degraded: %+v", ts)
	}
	m.degraded = ""
	gh.meta.OK = false
	if ts := b.Token(); ts.State != "rejected" {
		t.Fatalf("last call rejected: %+v", ts)
	}
}
```

Add `"time"` to the test imports if missing.

`internal/api/api_test.go`: add two fields to `fakeBackend`, `tokenStatus model.TokenStatus` and `tokenCalls int`, and:

```go
func (f *fakeBackend) Token() model.TokenStatus {
	f.tokenCalls++
	return f.tokenStatus
}
```

Append (each call must reach the backend and carry what it returned at that moment):

```go
func TestTokenStatus(t *testing.T) {
	c, b := setup(t)
	exp := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	rem := 4800
	b.tokenStatus = model.TokenStatus{State: "ok", ExpiresAt: &exp, RateRemaining: &rem}
	ts, err := c.Token(context.Background())
	if err != nil || b.tokenCalls != 1 || ts.State != "ok" || ts.ExpiresAt == nil || !ts.ExpiresAt.Equal(exp) || *ts.RateRemaining != 4800 {
		t.Fatalf("%+v %v calls %d", ts, err, b.tokenCalls)
	}
	b.tokenStatus = model.TokenStatus{State: "rejected", Reason: "GitHub rejected the token"}
	ts, err = c.Token(context.Background())
	if err != nil || b.tokenCalls != 2 || ts.State != "rejected" || ts.Reason != "GitHub rejected the token" || ts.ExpiresAt != nil || ts.RateRemaining != nil || ts.CheckedAt != nil {
		t.Fatalf("second call %+v %v calls %d", ts, err, b.tokenCalls)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 200 go test ./internal/daemon ./internal/api -run 'Token'`
Expected: build failures, `b.Token undefined`, `c.Token undefined`.

- [ ] **Step 3: Write the implementation**

`internal/daemon/backend.go`: add `TokenMeta() github.TokenMeta` to the `GitHub` interface, and:

```go
// Token reports what GitHub's responses said about the token; never the token.
func (b *Backend) Token() model.TokenStatus {
	meta := b.GH.TokenMeta()
	ts := model.TokenStatus{State: "unverified", RateRemaining: meta.RateRemaining, RateLimit: meta.RateLimit,
		RateReset: meta.RateReset, ExpiresAt: meta.ExpiresAt}
	if !meta.CheckedAt.IsZero() {
		at := meta.CheckedAt
		ts.CheckedAt = &at
		ts.State = "ok"
		if !meta.OK {
			ts.State = "rejected"
		}
	}
	if st := b.M.Status(); st.Degraded {
		ts.State, ts.Reason = "rejected", st.DegradedReason
	}
	return ts
}
```

`internal/api/server.go`: add `Token() model.TokenStatus` to `Backend`, and register:

```go
	mux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Token()) })
```

`internal/api/client.go`:

```go
// Token reports the daemon's view of its GitHub token.
func (c *Client) Token(ctx context.Context) (model.TokenStatus, error) {
	var ts model.TokenStatus
	err := c.call(ctx, http.MethodGet, "/token", nil, &ts)
	return ts, err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ ./internal/api/ ./cmd/...`
Expected: `ok` for every package.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon internal/api
git commit -m "feat(daemon): report token state"
```

### Task 7: Registration endpoints

**Files:**
- Modify: `internal/daemon/backend.go` (`GitHub` interface gains `ListRunners`, `GetRunner`, `DeleteRunner`; helpers `repoName`, `degradedErr`, `ghrOwned`; `Registrations`, `DeleteRegistration`)
- Modify: `internal/api/server.go` (`Backend` gains both methods; routes)
- Modify: `internal/api/client.go` (`Registrations`, `DeleteRegistration`)
- Test: `internal/daemon/backend_test.go`, `internal/api/api_test.go`

**Interfaces:**
- Consumes: C1 `Runner.Labels` (Task 2); C2 `Registration`; C3 `FromGitHub` (Task 3).
- Produces: C3 registration routes and client methods; C5 namespace rule; the `repoName` and `degradedErr` helpers Task 8 reuses.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

`internal/daemon/backend_test.go`: add fields to `fakeGH`:

```go
	runners map[int64]github.Runner
	getErr  error
	deleted []int64
```

and methods:

```go
func (f *fakeGH) ListRunners(ctx context.Context, repo string) ([]github.Runner, error) {
	var out []github.Runner
	for _, id := range []int64{1, 2, 3, 4} {
		if r, ok := f.runners[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}
func (f *fakeGH) GetRunner(ctx context.Context, repo string, id int64) (*github.Runner, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	r, ok := f.runners[id]
	if !ok {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound}
	}
	return &r, nil
}
func (f *fakeGH) DeleteRunner(ctx context.Context, repo string, id int64) error {
	f.deleted = append(f.deleted, id)
	return nil
}
```

Append:

```go
func registrationGH(gh *fakeGH) {
	gh.runners = map[int64]github.Runner{
		1: {ID: 1, Name: "linux-1", Status: "offline", Labels: []github.Label{{Name: "self-hosted"}, {Name: "X64"}}},
		2: {ID: 2, Name: "ghr-darkcloud-aaaaaa", Status: "offline"},
		3: {ID: 3, Name: "laptop", Status: "online"},
		4: {ID: 4, Name: "build-box", Status: "offline", Busy: true},
	}
}

func TestRegistrationsListed(t *testing.T) {
	b, _, gh := newBackend(t)
	registrationGH(gh)
	rs, err := b.Registrations(context.Background(), "DarkCloud")
	if err != nil || len(rs) != 4 {
		t.Fatalf("%+v %v", rs, err)
	}
	if rs[0].GHR || !reflect.DeepEqual(rs[0].Labels, []string{"self-hosted", "X64"}) || !rs[1].GHR || rs[2].GHR {
		t.Fatalf("%+v", rs)
	}
	if _, err := b.Registrations(context.Background(), "nope"); apiStatus(err) != 404 {
		t.Fatalf("unknown repo: %v", err)
	}
}

func TestDeleteRegistrationRefusals(t *testing.T) {
	b, m, gh := newBackend(t)
	registrationGH(gh)
	ctx := context.Background()
	for id, want := range map[int64]int{2: 409, 3: 409, 4: 409} {
		if err := b.DeleteRegistration(ctx, "darkcloud", id); apiStatus(err) != want {
			t.Errorf("id %d: %v", id, err)
		}
	}
	if err := b.DeleteRegistration(ctx, "darkcloud", 99); err != nil {
		t.Fatalf("a runner already gone is deleted: %v", err)
	}
	if err := b.DeleteRegistration(ctx, "darkcloud", 1); err != nil || !reflect.DeepEqual(gh.deleted, []int64{1}) {
		t.Fatalf("offline foreign runner: %v %v", err, gh.deleted)
	}
	gh.getErr = errors.New("connection reset")
	if err := b.DeleteRegistration(ctx, "darkcloud", 1); apiStatus(err) != 502 || len(gh.deleted) != 1 {
		t.Fatalf("failed lookup must not delete: %v %v", err, gh.deleted)
	}
	gh.getErr = nil
	m.degraded = "GitHub rejected the token"
	if err := b.DeleteRegistration(ctx, "darkcloud", 1); apiStatus(err) != 503 {
		t.Fatalf("degraded: %v", err)
	}
}
```

`internal/api/api_test.go`: add to `fakeBackend` the fields `regs []model.Registration`, `regErr error`, `regRepos []string`, `deletedReg []string` and:

```go
func (f *fakeBackend) Registrations(ctx context.Context, repo string) ([]model.Registration, error) {
	f.regRepos = append(f.regRepos, repo)
	return f.regs, f.regErr
}
func (f *fakeBackend) DeleteRegistration(ctx context.Context, repo string, id int64) error {
	f.deletedReg = append(f.deletedReg, fmt.Sprintf("%s/%d", repo, id))
	return f.regErr
}
```

Add `"fmt"` and `"reflect"` to the test imports if missing.

Append (each call must reach the backend with its arguments, and carry the backend's current answer, empty list and error included):

```go
func TestRegistrationRoutes(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	b.regs = []model.Registration{{ID: 1, Name: "linux-1", Status: "offline", Labels: []string{"self-hosted"}}}
	rs, err := c.Registrations(ctx, "darkcloud")
	if err != nil || len(rs) != 1 || rs[0].Name != "linux-1" || !reflect.DeepEqual(rs[0].Labels, []string{"self-hosted"}) {
		t.Fatalf("%+v %v", rs, err)
	}
	b.regs = []model.Registration{}
	rs, err = c.Registrations(ctx, "darkmem")
	if err != nil || rs == nil || len(rs) != 0 {
		t.Fatalf("empty list: %#v %v", rs, err)
	}
	b.regErr = NotFound("unknown repo nope")
	var ae *Error
	if _, err := c.Registrations(ctx, "nope"); !errors.As(err, &ae) || ae.Status != 404 || ae.Msg != "unknown repo nope" {
		t.Fatalf("error: %v", err)
	}
	if !reflect.DeepEqual(b.regRepos, []string{"darkcloud", "darkmem", "nope"}) {
		t.Fatalf("backend saw %v", b.regRepos)
	}
	if err := c.DeleteRegistration(ctx, "darkcloud", 7); !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("delete error: %v", err)
	}
	b.regErr = nil
	if err := c.DeleteRegistration(ctx, "darkcloud", 1); err != nil || !reflect.DeepEqual(b.deletedReg, []string{"darkcloud/7", "darkcloud/1"}) {
		t.Fatalf("%v %v", err, b.deletedReg)
	}
	for _, bad := range []string{"abc", "0", "-3", "99999999999999999999"} {
		req, _ := http.NewRequest(http.MethodDelete, c.Base+"/repos/darkcloud/registrations/"+bad, nil)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("id %s: status %d", bad, resp.StatusCode)
		}
	}
	if len(b.deletedReg) != 2 {
		t.Fatalf("a bad id reached the backend: %v", b.deletedReg)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 200 go test ./internal/daemon ./internal/api -run 'Registration'`
Expected: build failures, `b.Registrations undefined`.

- [ ] **Step 3: Write the implementation**

`internal/daemon/backend.go`: add to the `GitHub` interface:

```go
	ListRunners(ctx context.Context, repo string) ([]github.Runner, error)
	GetRunner(ctx context.Context, repo string, id int64) (*github.Runner, error)
	DeleteRunner(ctx context.Context, repo string, id int64) error
```

Add (with `"regexp"` imported):

```go
var hexID = regexp.MustCompile(`^[0-9a-f]{6}$`)

// ghrOwned reports whether a registration name is in ghr's namespace,
// ghr-<repo>-<6 hex>. ghr registers a runner before it records the instance,
// so only the name can protect a runner that is still starting.
func ghrOwned(repo, name string) bool {
	prefix := "ghr-" + strings.ToLower(repo) + "-"
	n := strings.ToLower(name)
	return strings.HasPrefix(n, prefix) && hexID.MatchString(n[len(prefix):])
}

// repoName resolves a repo name case-insensitively to its configured spelling.
func (b *Backend) repoName(name string) (string, error) {
	r := b.Store.Config().Repo(name)
	if r == nil {
		return "", api.NotFound("unknown repo " + name)
	}
	return r.Name, nil
}

// degradedErr refuses calls to GitHub while it rejects the token.
func (b *Backend) degradedErr() error {
	if st := b.M.Status(); st.Degraded {
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: "GitHub is rejecting the token: " + st.DegradedReason}
	}
	return nil
}

// Registrations lists the runners GitHub has registered for the repo.
func (b *Backend) Registrations(ctx context.Context, repo string) ([]model.Registration, error) {
	name, err := b.repoName(repo)
	if err != nil {
		return nil, err
	}
	if err := b.degradedErr(); err != nil {
		return nil, err
	}
	rs, err := b.GH.ListRunners(ctx, name)
	if err != nil {
		return nil, err
	}
	out := []model.Registration{}
	for _, r := range rs {
		labels := []string{}
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		out = append(out, model.Registration{ID: r.ID, Name: r.Name, Status: r.Status, Busy: r.Busy,
			Labels: labels, GHR: ghrOwned(name, r.Name)})
	}
	return out, nil
}

// DeleteRegistration deletes an offline registration outside ghr's
// namespace. The runner is re-read first; GitHub's delete has no "only if
// offline" condition, so the check is the last observed state.
func (b *Backend) DeleteRegistration(ctx context.Context, repo string, id int64) error {
	name, err := b.repoName(repo)
	if err != nil {
		return err
	}
	if err := b.degradedErr(); err != nil {
		return err
	}
	r, err := b.GH.GetRunner(ctx, name, id)
	if github.IsKind(err, github.ErrNotFound) {
		return nil
	}
	if err != nil {
		return &api.Error{Status: http.StatusBadGateway, Msg: "could not check the runner before deleting it: " + err.Error()}
	}
	switch {
	case ghrOwned(name, r.Name):
		return api.Conflict(r.Name + " belongs to ghr, which removes its own registrations")
	case r.Busy:
		return api.Conflict(r.Name + " is running a job")
	case r.Status == "online":
		return api.Conflict(r.Name + " is online; only offline runners can be deleted")
	}
	if err := b.GH.DeleteRunner(ctx, name, id); err != nil {
		return err
	}
	b.Events.Add("info", name, "deleted runner registration %s", r.Name)
	return nil
}
```

Add `"net/http"` to the imports.

`internal/api/server.go`: add to `Backend`:

```go
	Registrations(ctx context.Context, repo string) ([]model.Registration, error)
	DeleteRegistration(ctx context.Context, repo string, id int64) error
```

and register:

```go
	mux.HandleFunc("GET /repos/{name}/registrations", func(w http.ResponseWriter, r *http.Request) {
		rs, err := b.Registrations(r.Context(), r.PathValue("name"))
		respond(w, rs, err)
	})
	mux.HandleFunc("DELETE /repos/{name}/registrations/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			respond(w, nil, BadRequest("runner id must be a positive integer"))
			return
		}
		respond(w, nil, b.DeleteRegistration(r.Context(), r.PathValue("name"), id))
	})
```

`internal/api/client.go`:

```go
// Registrations lists the runners GitHub has registered for repo.
func (c *Client) Registrations(ctx context.Context, repo string) ([]model.Registration, error) {
	var rs []model.Registration
	err := c.call(ctx, http.MethodGet, "/repos/"+url.PathEscape(repo)+"/registrations", nil, &rs)
	return rs, err
}

// DeleteRegistration deletes an offline runner registration outside ghr's namespace.
func (c *Client) DeleteRegistration(ctx context.Context, repo string, id int64) error {
	return c.call(ctx, http.MethodDelete, fmt.Sprintf("/repos/%s/registrations/%d", url.PathEscape(repo), id), nil, nil)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ ./internal/api/ ./cmd/...`
Expected: `ok` for every package.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon internal/api
git commit -m "feat(daemon): list and delete runner registrations"
```

### Task 8: Label-check store

**Files:**
- Create: `internal/daemon/labelcheck.go`
- Modify: `internal/daemon/backend.go` (`Backend` gains `Now func() time.Time` and `checks labelChecks`; `GitHub` interface gains `ListRuns`, `ListRecentRuns`; `AddRepo` and `FinalizeRemovals` forget a repo's label check)
- Modify: `internal/daemon/run.go` (shutdown calls `b.Close()`)
- Test: `internal/daemon/labelcheck_test.go` (new), `internal/daemon/backend_test.go` (fake)

**Interfaces:**
- Consumes: C1 `ListRecentRuns`, `TokenMeta` (Tasks 1, 2); C5 `repoName`, `degradedErr` (Task 7); C2 `LabelCheck`, `LabelGroup`.
- Produces: C5 `Backend.StartLabelCheck`, `Backend.LabelCheck`, `Backend.Close`.

**Items:** 4

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Extend the daemon test fake** (`internal/daemon/backend_test.go`)

Add fields to `fakeGH`:

```go
	recent    []github.Run
	runs      map[string][]github.Run
	jobs      map[int64][]github.Job
	jobErr    map[int64]error // returned with that run's jobs
	block     bool            // ListJobs waits for the context to end
	listErr   error           // returned by ListRecentRuns
	lmu       sync.Mutex      // guards the fields below, written by scan goroutines
	jobCalls  []int64
	inFlight  int // ListJobs calls currently blocked
	cancelled int // blocked ListJobs calls ended by cancellation (not the deadline)
```

Replace `ListJobs` (the fixed list stays the default for the existing steps tests), add the two run listings and a counter reader:

```go
func (f *fakeGH) ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error) {
	if f.block {
		f.lmu.Lock()
		f.inFlight++
		f.lmu.Unlock()
		<-ctx.Done()
		f.lmu.Lock()
		f.inFlight--
		if errors.Is(ctx.Err(), context.Canceled) {
			f.cancelled++
		}
		f.lmu.Unlock()
		return nil, ctx.Err()
	}
	f.lmu.Lock()
	f.jobCalls = append(f.jobCalls, runID)
	f.lmu.Unlock()
	if f.jobs != nil {
		return f.jobs[runID], f.jobErr[runID]
	}
	return []github.Job{
		{RunnerName: "someone-else", Steps: []github.Step{{Name: "x"}}},
		{RunnerName: "ghr-darkcloud-aaaaaa", Steps: []github.Step{{Number: 1, Name: "checkout", Status: "completed", Conclusion: "success"}}},
	}, nil
}
func (f *fakeGH) ListRecentRuns(ctx context.Context, repo string, n int) ([]github.Run, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.recent, nil
}
func (f *fakeGH) ListRuns(ctx context.Context, repo, status string) ([]github.Run, error) {
	return f.runs[status], nil
}

// scanCounts waits up to 3 s for cond over (in flight, cancelled), failing the test otherwise.
func (f *fakeGH) scanCounts(t *testing.T, cond func(inFlight, cancelled int) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		f.lmu.Lock()
		in, c := f.inFlight, f.cancelled
		f.lmu.Unlock()
		if cond(in, c) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("scan counts never matched: in flight %d, cancelled %d", in, c)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
```

Add `"sync"` and `"time"` to the test imports where missing.

- [ ] **Step 2: Write the failing tests** (`internal/daemon/labelcheck_test.go`)

```go
package daemon

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func labelGH(gh *fakeGH) {
	t0 := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	gh.recent = []github.Run{{ID: 1}, {ID: 2}}
	gh.runs = map[string][]github.Run{"queued": {{ID: 2}, {ID: 3}}}
	gh.jobs = map[int64][]github.Job{
		1: {{Name: "test", WorkflowName: "CI", Labels: []string{"self-hosted", "darkcloud-linux"}, CreatedAt: t0}},
		2: {{Name: "lint", WorkflowName: "CI", Labels: []string{"ubuntu-latest"}, CreatedAt: t0.Add(time.Hour)}},
		3: {
			{Name: "gpu", WorkflowName: "ML", Labels: []string{"Self-Hosted", "GPU"}, CreatedAt: t0.Add(2 * time.Hour)},
			{Name: "gpu", WorkflowName: "ML", Labels: []string{"gpu", "self-hosted"}, CreatedAt: t0.Add(30 * time.Minute)},
		},
	}
}

func waitLabelCheck(t *testing.T, b *Backend, repo string) model.LabelCheck {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		lc, err := b.LabelCheck(repo)
		if err != nil {
			t.Fatal(err)
		}
		if lc.State != "checking" {
			return lc
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("label check never finished")
	return model.LabelCheck{}
}

func TestLabelCheckGathersAndGroups(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	if lc, err := b.LabelCheck("darkcloud"); err != nil || lc.State != "not_checked" || lc.Groups == nil {
		t.Fatalf("before any check: %+v %v", lc, err)
	}
	if err := b.StartLabelCheck("DarkCloud"); err != nil {
		t.Fatal(err)
	}
	lc := waitLabelCheck(t, b, "darkcloud")
	if lc.State != "done" || lc.Partial || lc.CheckedAt == nil || len(lc.Groups) != 3 {
		t.Fatalf("%+v", lc)
	}
	if g := lc.Groups[0]; !reflect.DeepEqual(g.Labels, []string{"gpu", "self-hosted"}) || g.Count != 2 ||
		!reflect.DeepEqual(g.Jobs, []string{"ML / gpu"}) || !g.LastSeen.Equal(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("newest group %+v", g)
	}
	if !reflect.DeepEqual(gh.jobCalls, []int64{1, 2, 3}) {
		t.Fatalf("runs not de-duplicated: %v", gh.jobCalls)
	}
}

func TestLabelCheckThrottleAndTokenChange(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	waitLabelCheck(t, b, "darkcloud")
	var ae *api.Error
	if err := b.StartLabelCheck("darkcloud"); !errors.As(err, &ae) || ae.Status != 429 || !ae.RetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("within a minute: %v", err)
	}
	now = now.Add(time.Minute)
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatalf("after a minute: %v", err)
	}
	waitLabelCheck(t, b, "darkcloud")
	gh.meta.Generation++
	if lc, _ := b.LabelCheck("darkcloud"); lc.State != "not_checked" {
		t.Fatalf("a new token keeps no old results: %+v", lc)
	}
}

func TestLabelCheckDeadlineErrorsAndDegraded(t *testing.T) {
	b, m, gh := newBackend(t)
	labelGH(gh)
	gh.block = true
	old := labelScanDeadline
	labelScanDeadline = 300 * time.Millisecond
	t.Cleanup(func() { labelScanDeadline = old })
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatalf("a second start joins the running scan: %v", err)
	}
	if lc := waitLabelCheck(t, b, "darkcloud"); !lc.Partial || lc.Error != "" {
		t.Fatalf("deadline: %+v", lc)
	}

	b2, _, gh2 := newBackend(t)
	gh2.listErr = errors.New("connection reset")
	if err := b2.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	if lc := waitLabelCheck(t, b2, "darkcloud"); !lc.Partial || lc.Error != "connection reset" || len(lc.Groups) != 0 {
		t.Fatalf("listing error: %+v", lc)
	}

	m.degraded = "GitHub rejected the token"
	if _, err := b.LabelCheck("darkcloud"); apiStatus(err) != 503 {
		t.Fatalf("degraded read: %v", err)
	}
	if _, err := b.LabelCheck("nope"); apiStatus(err) != 404 {
		t.Fatalf("unknown repo: %v", err)
	}
}

// Jobs a failed pagination returned before its error are still grouped.
func TestLabelCheckKeepsJobsReturnedWithError(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	gh.jobErr = map[int64]error{1: context.DeadlineExceeded}
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	lc := waitLabelCheck(t, b, "darkcloud")
	if !lc.Partial || lc.Error != "" || len(lc.Groups) != 1 || !reflect.DeepEqual(lc.Groups[0].Labels, []string{"darkcloud-linux", "self-hosted"}) {
		t.Fatalf("%+v", lc)
	}
}

// A token change cancels the running scan before a new one may start, and
// Close cancels whatever runs at shutdown.
func TestLabelCheckTokenChangeCancelsScan(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	gh.block = true
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	gh.scanCounts(t, func(in, _ int) bool { return in == 1 })
	gh.meta.Generation++
	if lc, _ := b.LabelCheck("darkcloud"); lc.State != "not_checked" {
		t.Fatalf("old generation still shown: %+v", lc)
	}
	gh.scanCounts(t, func(in, c int) bool { return in == 0 && c == 1 })
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatalf("new token: %v", err)
	}
	gh.scanCounts(t, func(in, _ int) bool { return in == 1 })
	if lc, _ := b.LabelCheck("darkcloud"); lc.State != "checking" {
		t.Fatalf("%+v", lc)
	}
	b.Close()
	gh.scanCounts(t, func(in, c int) bool { return in == 0 && c == 2 })
	if err := b.StartLabelCheck("darkcloud"); apiStatus(err) != 503 {
		t.Fatalf("start after Close: %v", err)
	}
}

// Removing a repo drops its observations and throttle, cancels its scan, and
// a cancelled scan never publishes into the entry of the re-added repo.
func TestLabelCheckForgottenOnRemoveAndAdd(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	ctx := context.Background()
	gh.block = true
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	gh.scanCounts(t, func(in, _ int) bool { return in == 1 })
	if err := b.RemoveRepo("darkcloud"); err != nil {
		t.Fatal(err)
	}
	b.FinalizeRemovals()
	gh.scanCounts(t, func(in, c int) bool { return in == 0 && c == 1 })
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "darkcloud", Max: 1}); err != nil {
		t.Fatal(err)
	}
	if lc, err := b.LabelCheck("darkcloud"); err != nil || lc.State != "not_checked" {
		t.Fatalf("re-added repo shows old state: %+v %v", lc, err)
	}
	gh.block = false
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatalf("re-added repo is throttled: %v", err)
	}
	if lc := waitLabelCheck(t, b, "darkcloud"); lc.State != "done" || lc.Partial || len(lc.Groups) != 3 {
		t.Fatalf("%+v", lc)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 200 go test ./internal/daemon -run 'LabelCheck'`
Expected: build failure, `b.LabelCheck undefined`.

- [ ] **Step 4: Write the implementation**

`internal/daemon/backend.go`: add to the `GitHub` interface:

```go
	ListRuns(ctx context.Context, repo, status string) ([]github.Run, error)
	ListRecentRuns(ctx context.Context, repo string, n int) ([]github.Run, error)
```

add to `Backend`:

```go
	// Now is the clock for label-check throttling; nil means time.Now.
	Now    func() time.Time
	checks labelChecks
```

and (with `"time"` imported):

```go
func (b *Backend) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}
```

`internal/daemon/labelcheck.go`:

```go
package daemon

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// labelScanRuns is how many recent runs a label check reads, besides the
// queued, in-progress and waiting ones.
const labelScanRuns = 20

// labelScanDeadline bounds one scan; what was gathered by then is kept as partial.
var labelScanDeadline = 60 * time.Second

type labelEntry struct {
	running bool
	started time.Time
	result  *model.LabelCheck
	cancel  context.CancelFunc // set while running
}

// labelChecks holds each repo's last label scan, keyed by the lower-cased
// repo name. Results belong to one token generation and are dropped when
// the token changes; classification against labels happens in the TUI, so a
// config change never makes a stored result wrong.
type labelChecks struct {
	mu      sync.Mutex
	gen     uint64
	closed  bool
	entries map[string]*labelEntry
}

// drop cancels the entry's scan and forgets it; c.mu must be held. A
// cancelled scan finds its entry gone and publishes nothing.
func (c *labelChecks) drop(key string) {
	if e := c.entries[key]; e != nil {
		if e.cancel != nil {
			e.cancel()
		}
		delete(c.entries, key)
	}
}

// refresh drops everything from an older token and the entries of repos no
// longer configured; c.mu must be held.
func (c *labelChecks) refresh(gen uint64, cfg *config.Config) {
	if c.entries == nil {
		c.entries = map[string]*labelEntry{}
	}
	if gen != c.gen {
		for key := range c.entries {
			c.drop(key)
		}
		c.gen = gen
	}
	for key := range c.entries {
		if cfg.Repo(key) == nil {
			c.drop(key)
		}
	}
}

// forgetLabelCheck drops a repo's scan, results and throttle; called when the
// repo is added or finally removed, so a re-added repo starts clean.
func (b *Backend) forgetLabelCheck(name string) {
	c := &b.checks
	c.mu.Lock()
	c.drop(strings.ToLower(name))
	c.mu.Unlock()
}

// Close cancels every running label scan; the daemon calls it on shutdown.
func (b *Backend) Close() {
	c := &b.checks
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for key := range c.entries {
		c.drop(key)
	}
}

// StartLabelCheck starts a background scan of the repo's recent jobs. A
// scan already running is joined; a new one runs at most once a minute.
func (b *Backend) StartLabelCheck(repo string) error {
	name, err := b.repoName(repo)
	if err != nil {
		return err
	}
	if err := b.degradedErr(); err != nil {
		return err
	}
	gen := b.GH.TokenMeta().Generation
	key := strings.ToLower(name)
	now := b.now()
	c := &b.checks
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: "the daemon is shutting down"}
	}
	c.refresh(gen, b.Store.Config())
	e := c.entries[key]
	if e == nil {
		e = &labelEntry{}
		c.entries[key] = e
	}
	if e.running {
		return nil
	}
	if !e.started.IsZero() && now.Sub(e.started) < time.Minute {
		return &api.Error{Status: http.StatusTooManyRequests, Msg: "this repo was checked less than a minute ago",
			RetryAt: e.started.Add(time.Minute)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), labelScanDeadline)
	e.running, e.started, e.cancel = true, now, cancel
	go b.scanLabels(ctx, name, key, e)
	return nil
}

// scanLabels runs one scan and publishes it into e, unless e was dropped or
// replaced meanwhile (token change, repo removed or re-added, shutdown).
func (b *Backend) scanLabels(ctx context.Context, name, key string, e *labelEntry) {
	groups, err := gatherLabels(ctx, b.GH, name)
	at := b.now()
	res := &model.LabelCheck{State: "done", CheckedAt: &at, Groups: groups}
	if err != nil {
		res.Partial = true
		if !errors.Is(err, context.DeadlineExceeded) {
			res.Error = err.Error()
		}
	}
	c := &b.checks
	c.mu.Lock()
	defer c.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	if c.entries[key] != e {
		return
	}
	e.running, e.result = false, res
}

// LabelCheck returns the repo's last scan without starting one.
func (b *Backend) LabelCheck(repo string) (model.LabelCheck, error) {
	name, err := b.repoName(repo)
	if err != nil {
		return model.LabelCheck{}, err
	}
	if err := b.degradedErr(); err != nil {
		return model.LabelCheck{}, err
	}
	gen := b.GH.TokenMeta().Generation
	c := &b.checks
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refresh(gen, b.Store.Config())
	res := model.LabelCheck{State: "not_checked", Groups: []model.LabelGroup{}}
	e := c.entries[strings.ToLower(name)]
	if e == nil {
		return res, nil
	}
	if e.result != nil {
		res = *e.result
	}
	if e.running {
		res.State = "checking"
	}
	return res, nil
}

// gatherLabels reads the jobs of the repo's recent, queued, in-progress and
// waiting runs and groups them by the labels they request.
func gatherLabels(ctx context.Context, gh GitHub, repo string) ([]model.LabelGroup, error) {
	seen := map[int64]bool{}
	var runs []github.Run
	add := func(rs []github.Run) {
		for _, r := range rs {
			if !seen[r.ID] {
				seen[r.ID] = true
				runs = append(runs, r)
			}
		}
	}
	recent, err := gh.ListRecentRuns(ctx, repo, labelScanRuns)
	if err != nil {
		return []model.LabelGroup{}, err
	}
	add(recent)
	for _, status := range []string{"queued", "in_progress", "waiting"} {
		rs, err := gh.ListRuns(ctx, repo, status)
		if err != nil {
			return []model.LabelGroup{}, err
		}
		add(rs)
	}
	g := &labelGrouper{byKey: map[string]*model.LabelGroup{}, names: map[string]map[string]bool{}}
	for _, run := range runs {
		// A failed pagination still returns the jobs of the pages before it.
		jobs, err := gh.ListJobs(ctx, repo, run.ID)
		for _, j := range jobs {
			g.add(j)
		}
		if err != nil {
			return g.groups(), err
		}
	}
	return g.groups(), nil
}

type labelGrouper struct {
	byKey map[string]*model.LabelGroup
	names map[string]map[string]bool
	order []string
}

func (g *labelGrouper) add(j github.Job) {
	labels := make([]string, 0, len(j.Labels))
	for _, l := range j.Labels {
		labels = append(labels, strings.ToLower(strings.TrimSpace(l)))
	}
	sort.Strings(labels)
	key := strings.Join(labels, "\x00")
	grp := g.byKey[key]
	if grp == nil {
		grp = &model.LabelGroup{Labels: labels, Jobs: []string{}}
		g.byKey[key], g.names[key] = grp, map[string]bool{}
		g.order = append(g.order, key)
	}
	grp.Count++
	if j.CreatedAt.After(grp.LastSeen) {
		grp.LastSeen = j.CreatedAt
	}
	name := j.Name
	if j.WorkflowName != "" {
		name = j.WorkflowName + " / " + j.Name
	}
	if !g.names[key][name] {
		g.names[key][name] = true
		if len(grp.Jobs) < 3 {
			grp.Jobs = append(grp.Jobs, name)
		} else {
			grp.More++
		}
	}
}

// groups returns the groups, most recently seen first.
func (g *labelGrouper) groups() []model.LabelGroup {
	out := []model.LabelGroup{}
	for _, k := range g.order {
		out = append(out, *g.byKey[k])
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].LastSeen.After(out[b].LastSeen) })
	return out
}
```

`internal/daemon/backend.go`: in `AddRepo`, insert `b.forgetLabelCheck(name)` immediately before `b.Events.Add("info", name, "repo added")`; in `FinalizeRemovals`, change the `default:` branch to:

```go
		default:
			b.forgetLabelCheck(name)
			b.Events.Add("info", name, "repo removed")
```

`internal/daemon/run.go`: in the `case <-ctx.Done():` body, insert `b.Close()` immediately after the `m.Close()` that Task 5 added.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ ./cmd/...`
Expected: `ok` for both.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/daemon
git add internal/daemon
git commit -m "feat(daemon): gather workflow job labels per repo"
```

### Task 9: Label-check endpoints

**Files:**
- Modify: `internal/api/server.go` (`Backend` gains `StartLabelCheck`, `LabelCheck`; two routes)
- Modify: `internal/api/client.go` (`StartLabelCheck`, `LabelCheck`)
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: the Task 8 backend methods; C2 `LabelCheck`.
- Produces: C3 label-check routes and client methods.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing test** (`internal/api/api_test.go`)

Add to `fakeBackend` the fields `checked []string`, `startErr error`, `lc model.LabelCheck`, `lcErr error`, `lcRepos []string` and:

```go
func (f *fakeBackend) StartLabelCheck(repo string) error {
	f.checked = append(f.checked, repo)
	return f.startErr
}
func (f *fakeBackend) LabelCheck(repo string) (model.LabelCheck, error) {
	f.lcRepos = append(f.lcRepos, repo)
	return f.lc, f.lcErr
}
```

Append (each call must reach the backend with its repo and carry the backend's current answer, a throttle's retry time, an empty result and an error included):

```go
func TestLabelCheckRoutes(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	if err := c.StartLabelCheck(ctx, "dark cloud"); err != nil || !reflect.DeepEqual(b.checked, []string{"dark cloud"}) {
		t.Fatalf("%v %v", err, b.checked)
	}
	retry := time.Date(2026, 10, 5, 12, 1, 0, 0, time.UTC)
	b.startErr = &Error{Status: http.StatusTooManyRequests, Msg: "this repo was checked less than a minute ago", RetryAt: retry}
	var ae *Error
	if err := c.StartLabelCheck(ctx, "darkcloud"); !errors.As(err, &ae) || ae.Status != 429 || !ae.RetryAt.Equal(retry) || len(b.checked) != 2 {
		t.Fatalf("throttled start: %v %v", err, b.checked)
	}
	b.lc = model.LabelCheck{State: "done", Groups: []model.LabelGroup{{Labels: []string{"self-hosted"}, Count: 2}}}
	lc, err := c.LabelCheck(ctx, "darkcloud")
	if err != nil || lc.State != "done" || len(lc.Groups) != 1 || lc.Groups[0].Count != 2 {
		t.Fatalf("%+v %v", lc, err)
	}
	b.lc = model.LabelCheck{State: "not_checked", Groups: []model.LabelGroup{}}
	lc, err = c.LabelCheck(ctx, "darkmem")
	if err != nil || lc.State != "not_checked" || lc.Groups == nil || len(lc.Groups) != 0 {
		t.Fatalf("empty: %#v %v", lc, err)
	}
	b.lcErr = NotFound("unknown repo nope")
	if _, err := c.LabelCheck(ctx, "nope"); !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("error: %v", err)
	}
	if !reflect.DeepEqual(b.lcRepos, []string{"darkcloud", "darkmem", "nope"}) {
		t.Fatalf("backend saw %v", b.lcRepos)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 120 go test ./internal/api -run LabelCheckRoutes`
Expected: build failure, `c.StartLabelCheck undefined`.

- [ ] **Step 3: Write the implementation**

`internal/api/server.go`: add to `Backend`:

```go
	StartLabelCheck(repo string) error
	LabelCheck(repo string) (model.LabelCheck, error)
```

and register:

```go
	mux.HandleFunc("POST /repos/{name}/label-check", func(w http.ResponseWriter, r *http.Request) {
		if err := b.StartLabelCheck(r.PathValue("name")); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET /repos/{name}/label-check", func(w http.ResponseWriter, r *http.Request) {
		lc, err := b.LabelCheck(r.PathValue("name"))
		respond(w, lc, err)
	})
```

`internal/api/client.go`:

```go
// StartLabelCheck asks the daemon to scan repo's recent jobs in the background.
func (c *Client) StartLabelCheck(ctx context.Context, repo string) error {
	return c.call(ctx, http.MethodPost, "/repos/"+url.PathEscape(repo)+"/label-check", nil, nil)
}

// LabelCheck returns repo's last label scan; it never starts one.
func (c *Client) LabelCheck(ctx context.Context, repo string) (model.LabelCheck, error) {
	var lc model.LabelCheck
	err := c.call(ctx, http.MethodGet, "/repos/"+url.PathEscape(repo)+"/label-check", nil, &lc)
	return lc, err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/api/ ./internal/daemon/ ./cmd/...`
Expected: `ok` for every package.

- [ ] **Step 5: Commit**

```bash
git add internal/api
git commit -m "feat(api): serve label checks"
```

### Task 10: Metrics sampler and endpoint

**Files:**
- Create: `internal/metrics/metrics.go`
- Modify: `internal/daemon/backend.go` (field `Sampler *metrics.Sampler`; method `Metrics`)
- Modify: `internal/daemon/run.go` (build the sampler, run it, pass it to the backend)
- Modify: `internal/api/server.go` (`Backend` gains `Metrics() model.Metrics`; route `GET /metrics`), `internal/api/client.go` (`Metrics`)
- Test: `internal/metrics/metrics_test.go` (new), `internal/api/api_test.go`

**Interfaces:**
- Consumes: C2 `Metrics`, `MetricSample`.
- Produces: C6; C3 `GET /metrics`, `Client.Metrics`.

**Items:** 3, 4

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

`internal/metrics/metrics_test.go`:

```go
package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type host struct {
	dir string
	s   *Sampler
	now time.Time
}

func newHost(t *testing.T) *host {
	t.Helper()
	h := &host{dir: t.TempDir(), now: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	h.s = NewSampler(func() Snapshot { return Snapshot{Live: 2, Queued: 3} }, func() int { return 11 })
	h.s.Now = func() time.Time { return h.now }
	h.s.CPUStat = filepath.Join(h.dir, "cpu.stat")
	h.s.CPUMax = filepath.Join(h.dir, "cpu.max")
	h.s.MemInfo = filepath.Join(h.dir, "meminfo")
	h.s.NumCPU = func() int { return 2 }
	h.write(t, "cpu.max", "max 100000\n")
	h.write(t, "meminfo", "MemTotal:        8388608 kB\nMemFree:  100 kB\nMemAvailable:    8204288 kB\n")
	return h
}

func (h *host) write(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(h.dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSamplesCPUMemoryAndCounts(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 1000000\nuser_usec 1\n")
	h.s.Sample()
	m := h.s.Metrics()
	if len(m.Samples) != 1 || m.Samples[0].CPU != nil || m.Samples[0].Live != 2 || m.Samples[0].Queued != 3 {
		t.Fatalf("first sample %+v", m.Samples)
	}
	if *m.MemUsed != (8388608-8204288)*1024 || *m.MemTotal != 8388608*1024 || m.DiskPct != 11 {
		t.Fatalf("current %+v", m)
	}
	h.now = h.now.Add(time.Minute)
	h.write(t, "cpu.stat", "usage_usec 61000000\n") // 60 s of CPU over 60 s on 2 CPUs
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[1].CPU == nil || *m.Samples[1].CPU != 50 || *m.CPU != 50 {
		t.Fatalf("second sample %+v", m.Samples[1])
	}
	h.now = h.now.Add(time.Minute)
	h.write(t, "cpu.stat", "usage_usec 5\n") // the counter went backwards
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[2].CPU != nil {
		t.Fatal("a counter going backwards gives no CPU value")
	}
	os.Remove(filepath.Join(h.dir, "meminfo"))
	h.now = h.now.Add(time.Minute)
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[3].Mem != nil || m.MemUsed != nil {
		t.Fatal("a failed memory read gives no value")
	}
}

func TestQuotaAndRing(t *testing.T) {
	h := newHost(t)
	h.s.NumCPU = func() int { return 8 }
	h.write(t, "cpu.max", "200000 100000\n") // a quota of 2 CPUs
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Sample()
	for i := 1; i <= 64; i++ {
		h.now = h.now.Add(time.Minute)
		h.write(t, "cpu.stat", "usage_usec "+itoa(int64(i)*30000000)+"\n") // 30 s per minute on 2 CPUs
		h.s.Sample()
	}
	m := h.s.Metrics()
	if len(m.Samples) != 60 || *m.CPU != 25 {
		t.Fatalf("samples %d cpu %v", len(m.Samples), *m.CPU)
	}
	if want := time.Date(2026, 10, 5, 12, 5, 0, 0, time.UTC); !m.Samples[0].At.Equal(want) {
		t.Fatalf("oldest kept %v", m.Samples[0].At)
	}
}

// A failed quota read gives no CPU value rather than a percentage over the
// host's CPU count, and the next good read recovers.
func TestQuotaReadFailureAndRecovery(t *testing.T) {
	h := newHost(t)
	h.s.NumCPU = func() int { return 8 }
	h.write(t, "cpu.max", "200000 100000\n")
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Sample()
	os.Remove(filepath.Join(h.dir, "cpu.max"))
	h.now = h.now.Add(time.Minute)
	h.write(t, "cpu.stat", "usage_usec 30000000\n")
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[1].CPU != nil || m.CPU != nil {
		t.Fatalf("missing cpu.max: %+v", m.Samples[1])
	}
	h.write(t, "cpu.max", "garbage\n")
	h.now = h.now.Add(time.Minute)
	h.write(t, "cpu.stat", "usage_usec 60000000\n")
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[2].CPU != nil {
		t.Fatalf("unparseable cpu.max: %+v", m.Samples[2])
	}
	h.write(t, "cpu.max", "200000 100000\n")
	h.now = h.now.Add(time.Minute)
	h.write(t, "cpu.stat", "usage_usec 90000000\n")
	h.s.Sample()
	if m := h.s.Metrics(); m.Samples[3].CPU == nil || *m.Samples[3].CPU != 25 {
		t.Fatalf("recovered: %+v", m.Samples[3])
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
```

Add `"strconv"` to that file's imports.

`internal/api/api_test.go`: add to `fakeBackend` the fields `metrics model.Metrics` and `metricsCalls int`, and:

```go
func (f *fakeBackend) Metrics() model.Metrics {
	f.metricsCalls++
	return f.metrics
}
```

Append (each call must reach the backend and carry what it returned at that moment, an empty series included):

```go
func TestMetricsRoute(t *testing.T) {
	c, b := setup(t)
	cpu := 12.5
	b.metrics = model.Metrics{DiskPct: 11, CPU: &cpu, Samples: []model.MetricSample{{Live: 1, Queued: 2}}}
	m, err := c.Metrics(context.Background())
	if err != nil || b.metricsCalls != 1 || m.DiskPct != 11 || m.CPU == nil || *m.CPU != 12.5 || len(m.Samples) != 1 || m.Samples[0].Queued != 2 {
		t.Fatalf("%+v %v", m, err)
	}
	b.metrics = model.Metrics{DiskPct: 40, Samples: []model.MetricSample{}}
	m, err = c.Metrics(context.Background())
	if err != nil || b.metricsCalls != 2 || m.DiskPct != 40 || m.CPU != nil || m.Samples == nil || len(m.Samples) != 0 {
		t.Fatalf("second call %#v %v", m, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 200 go test ./internal/metrics ./internal/api`
Expected: build failures (`NewSampler undefined`, `c.Metrics undefined`).

- [ ] **Step 3: Write the implementation**

`internal/metrics/metrics.go`:

```go
// Package metrics keeps an hour of one-minute samples of the runner counts
// and the container's CPU and memory use.
package metrics

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// keep is how many samples the ring holds: an hour at one a minute.
const keep = 60

// Snapshot is the runner counts at one moment, as the Dashboard tiles show them.
type Snapshot struct{ Live, Queued int }

type Sampler struct {
	Now     func() time.Time
	CPUStat string
	CPUMax  string
	MemInfo string
	NumCPU  func() int
	Counts  func() Snapshot
	Disk    func() int

	mu      sync.Mutex
	samples []model.MetricSample
	cur     model.Metrics
	lastCPU int64
	lastAt  time.Time
	haveCPU bool
}

func NewSampler(counts func() Snapshot, disk func() int) *Sampler {
	return &Sampler{Now: time.Now, CPUStat: "/sys/fs/cgroup/cpu.stat", CPUMax: "/sys/fs/cgroup/cpu.max",
		MemInfo: "/proc/meminfo", NumCPU: runtime.NumCPU, Counts: counts, Disk: disk}
}

// Run samples at once and then every interval until ctx ends.
func (s *Sampler) Run(ctx context.Context, every time.Duration) {
	s.Sample()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sample()
		}
	}
}

// Sample records one sample now. CPU is the cgroup's usage growth since the
// previous sample over elapsed time and CPUs; the first sample, a failed
// read and a counter that went backwards give none.
func (s *Sampler) Sample() {
	now := s.Now()
	counts := s.Counts()
	usage, usageOK := readUsage(s.CPUStat)
	cpus, cpusOK := s.cpus()
	used, total, memOK := readMem(s.MemInfo)
	disk := s.Disk()

	smp := model.MetricSample{At: now, Live: counts.Live, Queued: counts.Queued}
	s.mu.Lock()
	defer s.mu.Unlock()
	if usageOK && cpusOK && s.haveCPU && usage >= s.lastCPU && now.After(s.lastAt) {
		pct := float64(usage-s.lastCPU) / (float64(now.Sub(s.lastAt).Microseconds()) * cpus) * 100
		pct = min(max(pct, 0), 100)
		smp.CPU = &pct
	}
	s.lastCPU, s.lastAt, s.haveCPU = usage, now, usageOK
	s.cur = model.Metrics{CPU: smp.CPU, DiskPct: disk}
	if memOK {
		smp.Mem = &used
		s.cur.MemUsed, s.cur.MemTotal = &used, &total
	}
	s.samples = append(s.samples, smp)
	if len(s.samples) > keep {
		s.samples = s.samples[len(s.samples)-keep:]
	}
}

// Metrics returns a copy of the samples and the current figures.
func (s *Sampler) Metrics() model.Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.cur
	m.Samples = append([]model.MetricSample{}, s.samples...)
	return m
}

// cpus is the cgroup's CPU quota, or the CPU count when cpu.max says "max".
// A failed or unparseable read reports false: guessing the CPU count would
// give a wrong percentage on a quota-limited host.
func (s *Sampler) cpus() (float64, bool) {
	data, err := os.ReadFile(s.CPUMax)
	if err != nil {
		return 0, false
	}
	f := strings.Fields(string(data))
	if len(f) != 2 {
		return 0, false
	}
	if f[0] == "max" {
		return float64(s.NumCPU()), true
	}
	q, err1 := strconv.ParseFloat(f[0], 64)
	p, err2 := strconv.ParseFloat(f[1], 64)
	if err1 != nil || err2 != nil || q <= 0 || p <= 0 {
		return 0, false
	}
	return q / p, true
}

func readUsage(path string) (int64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[0] == "usage_usec" {
			n, err := strconv.ParseInt(f[1], 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// readMem reads used (MemTotal − MemAvailable) and total memory in bytes.
// lxcfs reports the container's own figures in /proc/meminfo; the cgroup's
// memory.current would count page cache as used.
func readMem(path string) (used, total int64, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	tot, avail := int64(-1), int64(-1)
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			tot = n * 1024
		case "MemAvailable:":
			avail = n * 1024
		}
	}
	if tot < 0 || avail < 0 {
		return 0, 0, false
	}
	return tot - avail, tot, true
}
```

`internal/daemon/backend.go`: add the field `Sampler *metrics.Sampler` to `Backend` (import `"github.com/darkraise/ghr/internal/metrics"`) and:

```go
// Metrics returns the sampler's series and current host figures.
func (b *Backend) Metrics() model.Metrics {
	if b.Sampler == nil {
		return model.Metrics{Samples: []model.MetricSample{}}
	}
	return b.Sampler.Metrics()
}
```

`internal/daemon/run.go` (import `"github.com/darkraise/ghr/internal/metrics"`): immediately before `wake := make(chan struct{}, 1)`, add:

```go
	sampler := metrics.NewSampler(func() metrics.Snapshot {
		st := m.Status()
		var s metrics.Snapshot
		for _, i := range st.Instances {
			if i.State != "cleaning" {
				s.Live++
			}
		}
		for _, r := range st.Repos {
			s.Queued += r.Queued
		}
		return s
	}, func() int { return m.Status().DiskPct })
	go sampler.Run(ctx, time.Minute)
```

and add `Sampler: sampler,` to the `&Backend{...}` literal.

`internal/api/server.go`: add `Metrics() model.Metrics` to `Backend`, and:

```go
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Metrics()) })
```

`internal/api/client.go`:

```go
// Metrics returns the daemon's last hour of samples and current host figures.
func (c *Client) Metrics(ctx context.Context) (model.Metrics, error) {
	var m model.Metrics
	err := c.call(ctx, http.MethodGet, "/metrics", nil, &m)
	return m, err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/metrics/ ./internal/api/ ./internal/daemon/ ./cmd/...`
Expected: `ok` for every package.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal
git add internal/metrics internal/daemon internal/api
git commit -m "feat(daemon): sample runner counts, CPU and memory"
```

### Task 11: Badge, sparkline and gauge widgets

**Files:**
- Create: `internal/tui/ui/badge.go`, `internal/tui/ui/sparkline.go`
- Modify: `internal/tui/styles.go` (remove `gauge`), `internal/tui/shell.go` (call `ui.Gauge`)
- Test: `internal/tui/ui/badge_test.go` (new)

**Interfaces:**
- Consumes: none.
- Produces: C7 `BadgeKind`, `Badge`, `Sparkline`, `Gauge`.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests** (`internal/tui/ui/badge_test.go`)

```go
package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func withProfile(t *testing.T, p termenv.Profile) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func TestBadgeProfiles(t *testing.T) {
	withProfile(t, termenv.Ascii)
	if got := Badge("active", BadgeOK); got != "[ACTIVE]" {
		t.Fatalf("ascii badge %q", got)
	}
	withProfile(t, termenv.ANSI256)
	got := Badge("busy", BadgeBusy)
	if !strings.Contains(got, "\x1b[") || ansi.Strip(got) != " BUSY " || ansi.StringWidth(got) != len("[BUSY]") {
		t.Fatalf("colour badge %q", got)
	}
}

func TestSparkline(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	vals := []*float64{f(0), f(1), nil, f(4), f(8)}
	if got := Sparkline(vals, 8, 7); got != "  ▁▂ ▅█" {
		t.Fatalf("sparkline %q", got)
	}
	if got := Sparkline(vals, 8, 3); got != " ▅█" {
		t.Fatalf("cut to the newest %q", got)
	}
	if got := Sparkline(vals, 0, 2); got != "▁▁" {
		t.Fatalf("zero max %q", got)
	}
	if got := Gauge(5, 10, 4); got != "▕██░░▏" {
		t.Fatalf("gauge %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 120 go test ./internal/tui/ui -run 'Badge|Sparkline'`
Expected: build failure, `Badge undefined`.

- [ ] **Step 3: Write the implementation**

`internal/tui/ui/badge.go`:

```go
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type BadgeKind int

const (
	BadgeOK    BadgeKind = iota // active, online, success, matched, ok
	BadgeBusy                   // busy
	BadgeWarn                   // paused, idle, starting, expires soon, unverified
	BadgeBad                    // error, failure, offline, unmatched, rejected
	BadgeMuted                  // removing, cleaning, ghr, other, cancelled, unknown
)

var (
	black = lipgloss.Color("#000000")
	white = lipgloss.Color("#ffffff")
)

var badgeStyles = map[BadgeKind]lipgloss.Style{
	BadgeOK:    lipgloss.NewStyle().Bold(true).Foreground(black).Background(ColGreen),
	BadgeBusy:  lipgloss.NewStyle().Bold(true).Foreground(white).Background(ColAccent),
	BadgeWarn:  lipgloss.NewStyle().Bold(true).Foreground(black).Background(ColAmber),
	BadgeBad:   lipgloss.NewStyle().Bold(true).Foreground(white).Background(ColRed),
	BadgeMuted: lipgloss.NewStyle().Bold(true).Foreground(white).Background(ColDim),
}

// Badge renders a state as " TEXT " on a coloured block. Without colour it
// renders "[TEXT]", the same width, so layouts do not move.
func Badge(text string, kind BadgeKind) string {
	text = strings.ToUpper(text)
	if lipgloss.ColorProfile() == termenv.Ascii {
		return "[" + text + "]"
	}
	return badgeStyles[kind].Render(" " + text + " ")
}
```

`internal/tui/ui/sparkline.go`:

```go
package ui

import "strings"

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// Sparkline draws the newest width values scaled to limit, newest on the
// right. A nil value draws a space; fewer values than width are padded on
// the left.
func Sparkline(vals []*float64, limit float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", width-len(vals)))
	top := len(sparkRunes) - 1
	for _, v := range vals {
		if v == nil {
			b.WriteByte(' ')
			continue
		}
		i := 0
		if limit > 0 {
			i = int(*v/limit*float64(top) + 0.5)
		}
		b.WriteRune(sparkRunes[min(max(i, 0), top)])
	}
	return b.String()
}

// Gauge draws used out of total as a bar width cells wide.
func Gauge(used, total, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}
	filled := min(used*width/total, width)
	return "▕" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "▏"
}
```

`internal/tui/styles.go`: delete the `gauge` function and the `"strings"` import (`gauge` was its only user at `9fa16fe`; confirm with `grep -n 'strings\.' internal/tui/styles.go`, which must print nothing). `internal/tui/shell.go`: replace every `gauge(` call with `ui.Gauge(` (three calls in `chips`).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui/...`
Expected: `ok` for both packages; the goldens are unchanged.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "feat(tui): add badge, sparkline and gauge widgets"
```

### Task 12: Masked text field and multi-line rows

**Files:**
- Modify: `internal/tui/ui/textfield.go` (`Mask`)
- Modify: `internal/tui/ui/layout.go` (`Row.Lines`)
- Test: `internal/tui/ui/textfield_test.go`, `internal/tui/ui/layout_test.go`

**Interfaces:**
- Consumes: none.
- Produces: C7 `TextField.Mask`, `Row.Lines`.

**Items:** 2, 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/ui/textfield_test.go`:

```go
func TestMaskedFieldNeverShowsItsValue(t *testing.T) {
	f := NewTextField("t/token", 20)
	f.Mask = true
	f.Check = func(s string) error {
		if len(s) < 10 {
			return fmt.Errorf("token %q is too short", s)
		}
		return nil
	}
	typeText(f, "ghp_secret")
	views := []string{render(f.View(true, 40))}
	f.Update(key("enter")) // commit: no longer editing
	views = append(views, render(f.View(true, 40)), render(f.View(false, 40)))
	f.SetDisabled(true)
	views = append(views, render(f.View(false, 40)))
	f.SetDisabled(false)
	f.SetValue(Value{Text: "short"})
	views = append(views, render(f.View(false, 40)))
	for i, v := range views {
		if strings.Contains(v, "secret") || strings.Contains(v, "short") || strings.Contains(v, "ghp") {
			t.Fatalf("view %d shows the value: %q", i, v)
		}
	}
	if !strings.Contains(views[2], "••••••••••") {
		t.Fatalf("masked view %q", views[2])
	}
	if f.Value().Text != "short" {
		t.Fatal("masking changes only the view")
	}
}

// A masked field's check error never carries the value in any form: raw,
// quoted, escaped or transformed.
func TestMaskedFieldErrorIsGeneric(t *testing.T) {
	f := NewTextField("t/token", 20)
	f.Mask = true
	f.Check = func(s string) error {
		return fmt.Errorf("bad %s %q %s %s", s, s, strings.ToUpper(s), strings.ReplaceAll(s, "_", `\_`))
	}
	f.SetValue(Value{Text: "ghp_a\"b"})
	err := f.Err()
	if err == nil || err != ErrMaskedInvalid {
		t.Fatalf("Err() = %v", err)
	}
	v := render(f.View(false, 40))
	for _, s := range []string{err.Error(), v} {
		low := strings.ToLower(s)
		if strings.Contains(low, "ghp") || strings.Contains(s, `a\"b`) || strings.Contains(s, `a"b`) {
			t.Fatalf("value leaked: %q", s)
		}
	}
	if !strings.Contains(v, ErrMaskedInvalid.Error()) {
		t.Fatalf("view %q lacks the generic error", v)
	}
	f.Check = func(string) error { return nil }
	if f.Err() != nil {
		t.Fatal("a passing check gives no error")
	}
}
```

Add `"fmt"` and `"strings"` to that file's imports if missing.

Append to `internal/tui/ui/layout_test.go`:

```go
func TestRowLines(t *testing.T) {
	lines, _ := Render([]Section{{Rows: []Row{{Label: "Runners get", Lines: []string{"self-hosted linux x64", "homelab (global)"}}}}}, "", 60, true)
	if len(lines) != 2 || !strings.Contains(lines[0], "Runners get") || !strings.Contains(lines[0], "self-hosted linux x64") ||
		!strings.HasPrefix(lines[1], strings.Repeat(" ", LabelWidth+1)) || !strings.Contains(lines[1], "homelab (global)") {
		t.Fatalf("lines %q", lines)
	}
}

// Long Lines and a narrow-form description wrap to the space beside the
// label instead of being cut at the card edge; every word stays visible.
func TestRowLinesAndDescWrap(t *testing.T) {
	long := "self-hosted linux x64 darkcloud-linux gpu cuda-12 homelab (global) arm64-builder"
	desc := "container name prefixes removed after each job, matched by their start"
	lines, _ := Render([]Section{{Title: "Labels", Rows: []Row{
		{Label: "Runners get", Lines: []string{long, "runners already running keep their labels"}},
		{Label: "Prefixes", Items: []Widget{NewTextField("p", 10)}, Desc: desc},
	}}}, "", 52, false)
	text := strings.Join(lines, "\n")
	for _, word := range append(strings.Fields(long), strings.Fields(desc)...) {
		if !strings.Contains(text, word) {
			t.Errorf("word %q lost:\n%s", word, text)
		}
	}
	for i, l := range lines {
		if ansi.StringWidth(l) > 52 {
			t.Fatalf("line %d is %d wide: %q", i, ansi.StringWidth(l), l)
		}
	}
	if !strings.Contains(text, "keep their labels") {
		t.Fatalf("final note cut:\n%s", text)
	}
}

// Under StackBelow columns inside a card, the label takes its own line and
// the control gets the full width, so it is never cut.
func TestRowStacksWhenNarrow(t *testing.T) {
	s := NewStepper("m", 0, 99, 1)
	s.ZeroText = "∞"
	lines, _ := Render([]Section{{Title: "Capacity", Rows: []Row{{Label: "Max", Items: []Widget{s}, Desc: "once set, it stays explicit"}}}}, "", 40, false)
	if len(lines) < 4 || !strings.Contains(lines[1], "Max") || strings.Contains(lines[1], "[") {
		t.Fatalf("label not on its own line: %q", lines)
	}
	want := ansi.Strip(render(s.View(false, 34)))
	if !strings.Contains(ansi.Strip(render(lines[2])), strings.TrimSpace(want)) {
		t.Fatalf("control cut: %q, want %q", lines[2], want)
	}
	wide, _ := Render([]Section{{Title: "Capacity", Rows: []Row{{Label: "Max", Items: []Widget{s}}}}}, "", 60, false)
	if !strings.Contains(wide[1], "Max") || !strings.Contains(wide[1], "[") {
		t.Fatalf("a card 56 wide inside keeps the label column: %q", wide[1])
	}
}
```

Add `"strings"` and `"github.com/charmbracelet/x/ansi"` to that file's imports if missing.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 120 go test ./internal/tui/ui -run 'Masked|RowLines'`
Expected: build failure, `f.Mask undefined`, `unknown field Lines`.

- [ ] **Step 3: Write the implementation**

`internal/tui/ui/textfield.go`: add the field to `TextField`, after `Disabled bool`:

```go
	Mask     bool               // shows • for every character in every state; Err returns ErrMaskedInvalid
```

Replace `Err`, and add the error it returns for masked fields:

```go
// ErrMaskedInvalid is a masked field's check error. Check's own error may
// quote, escape or transform the value, so none of its text is shown.
var ErrMaskedInvalid = errors.New("not a valid value")

func (f *TextField) Err() error {
	if f.Check == nil {
		return nil
	}
	err := f.Check(f.Value().Text)
	if err != nil && f.Mask {
		return ErrMaskedInvalid
	}
	return err
}
```

Replace `View`:

```go
// View renders [ 5m         ] and, while the value fails Check, a line with the error.
func (f *TextField) View(focused bool, _ int) string {
	var text string
	if f.Mask {
		f.in.EchoMode, f.in.EchoCharacter = textinput.EchoPassword, '•'
	}
	switch {
	case f.editing:
		f.in.Width = f.Width - 1
		text = f.in.View()
	case f.Mask:
		text = strings.Repeat("•", utf8.RuneCountInString(f.in.Value()))
	default:
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

Add `"errors"` and `"unicode/utf8"` to the imports; `"strings"` is already there.

`internal/tui/ui/layout.go`: add to `Row`, after `Text string`:

```go
	Lines []string // read-only lines under the label, when Items and Text are empty
```

Add after `LabelWidth`:

```go
// StackBelow is the card inside width under which a row's label takes its
// own line and the controls start on the next one, indented two columns,
// so a control is never cut by the label column.
const StackBelow = 44
```

Replace `renderRow` (add `"github.com/charmbracelet/x/ansi"` to the imports):

```go
func renderRow(r Row, focused string, w int, wide bool) []string {
	var head []string
	pad := strings.Repeat(" ", LabelWidth+1)
	line := Cell(r.Label, LabelWidth) + " "
	if w < StackBelow {
		head, pad, line = []string{Cell(r.Label, w)}, "  ", "  "
	}
	ctlW := w - ansi.StringWidth(pad)
	wrap := func(s string) []string { return strings.Split(ansi.Wrap(s, max(ctlW-2, 1), ""), "\n") }
	var rest []string
	if len(r.Items) == 0 {
		if len(r.Lines) == 0 {
			line += "  " + r.Text
		} else {
			var all []string
			for _, l := range r.Lines {
				all = append(all, wrap(l)...)
			}
			line += "  " + all[0]
			for _, l := range all[1:] {
				rest = append(rest, "  "+l)
			}
		}
	}
	for i, it := range r.Items {
		lines := strings.Split(it.View(it.ID() == focused, ctlW), "\n")
		if i > 0 {
			line += " "
		} else {
			rest = append(rest, lines[1:]...)
		}
		line += lines[0]
	}
	if r.Dirty {
		line += " " + Amber.Render("●")
	}
	if r.Desc != "" && wide {
		line += "  " + Dim.Render(r.Desc)
	}
	out := append(head, line)
	for _, l := range rest {
		out = append(out, pad+l)
	}
	if r.Desc != "" && !wide {
		for _, l := range wrap(r.Desc) {
			out = append(out, pad+"  "+Dim.Render(l))
		}
	}
	if r.Err != "" {
		out = append(out, pad+"  "+Red.Render("✖ "+r.Err))
	}
	return out
}
```

A `Row.Lines` entry is wrapped by the row, so callers pass logical lines and never wrap them to a width themselves.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui/...`
Expected: `ok` for both packages. A golden may change only where a narrow-form Settings description used to be cut at the card edge and now wraps; read every golden diff, and regenerate with `-update` only for that change.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/ui
git commit -m "feat(tui): mask text fields and add multi-line rows"
```

### Task 13: Badges in existing tables

**Files:**
- Modify: `internal/tui/ui/badge.go` (`BadgeRow`), `internal/tui/view.go` (`selectedRow` takes parts; `stateBadge`)
- Modify: `internal/tui/dashboard.go` (`reposLines`), `internal/tui/runners.go` (`runnerLine`), `internal/tui/detail.go` (`detailSummary`), `internal/tui/history.go` (its `selectedRow` call only)
- Test: `internal/tui/ui/badge_test.go`, `internal/tui/tui_test.go`, `internal/tui/detail_test.go`, goldens under `internal/tui/testdata`

**Interfaces:**
- Consumes: C7 `Badge` (Task 11).
- Produces: C7 `BadgeRow`; `stateBadge(state string) string` in package `tui` (Tasks 15, 19, 20 use it).

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/ui/badge_test.go`:

```go
func TestBadgeRowKeepsBadgeColoursWhenSelected(t *testing.T) {
	withProfile(t, termenv.ANSI256)
	badge := Badge("busy", BadgeBusy)
	got := BadgeRow(Sel, true, "▸ a3f9c1 ", badge, " CI / e2e", 30)
	if !strings.Contains(got, badge) || ansi.StringWidth(got) != 30 {
		t.Fatalf("selected row %q (width %d)", got, ansi.StringWidth(got))
	}
	if plain := BadgeRow(Sel, false, "  a3f9c1 ", badge, " CI", 30); !strings.Contains(plain, badge) || ansi.StringWidth(plain) != 30 {
		t.Fatalf("plain row %q", plain)
	}
	// The text before the badge gives way so the badge keeps its place and colours.
	for _, sel := range []bool{true, false} {
		over := BadgeRow(Sel, sel, strings.Repeat("x", 28), badge, "tail", 30)
		if ansi.StringWidth(over) != 30 || !strings.Contains(over, badge) || !strings.Contains(over, "…") {
			t.Fatalf("overflowing row (selected %v) %q, %d wide", sel, over, ansi.StringWidth(over))
		}
	}
	// Narrower than the badge itself: the badge is cut, still in colour.
	if tiny := BadgeRow(Sel, true, "ab", badge, "", 3); ansi.StringWidth(tiny) != 3 || !strings.Contains(tiny, "\x1b[") {
		t.Fatalf("tiny row %q", tiny)
	}
}
```

Update the state assertions the badges replace: in `internal/tui/detail_test.go` change `"● busy"` to `"[BUSY]"` (two places); in `internal/tui/tui_test.go` change `"◌ paused"` to `"[PAUSED]"` and `"◌ removing"` to `"[REMOVING]"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 200 go test ./internal/tui/...`
Expected: `BadgeRow undefined` in `ui`; after a temporary stub the `tui` assertions above fail. (Skip the stub: the build failure is the expected failure.)

- [ ] **Step 3: Write the implementation**

`internal/tui/ui/badge.go` (add `"github.com/charmbracelet/x/ansi"` to the imports):

```go
// BadgeRow renders a table row exactly w columns wide: before, the badge,
// then after. A selected row gets sel on its text while the badge keeps its
// own colours. When the row is too narrow, after gives way first, then
// before is cut to leave room for the badge; only a row narrower than the
// badge itself cuts the badge.
func BadgeRow(sel lipgloss.Style, selected bool, before, badge, after string, w int) string {
	bw := ansi.StringWidth(badge)
	if bw >= w {
		return Cell(badge, w)
	}
	if ansi.StringWidth(before) > w-bw {
		before = Cell(before, w-bw)
	}
	after = Cell(after, w-bw-ansi.StringWidth(before))
	if !selected {
		return before + badge + after
	}
	return sel.Render(ansi.Strip(before)) + badge + sel.Render(ansi.Strip(after))
}
```

`internal/tui/view.go`: replace `selectedRow`:

```go
// selectedRow renders a selected row of width w: the text before the badge,
// the badge (kept in colour), the text after it, the row's buttons, then
// tail, the row's fixed right-hand columns, which keep their place under
// the header. The text gives way to the buttons, so its last column should
// be the flexible one.
func (m Model) selectedRow(id, before, badge, after, buttons, tail string, w int) string {
	bw, tw := ansi.StringWidth(buttons), ansi.StringWidth(tail)
	return zone.Mark(id, ui.BadgeRow(sSel, true, before, badge, after, w-bw-tw)) + buttons + sSel.Render(ansi.Strip(tail))
}

// stateBadge is the badge for a repo, runner, job or registration state.
func stateBadge(state string) string {
	kind := ui.BadgeMuted
	switch state {
	case "active", "online", "success", "matched", "ok":
		kind = ui.BadgeOK
	case "busy":
		kind = ui.BadgeBusy
	case "paused", "idle", "starting", "expires soon", "unverified":
		kind = ui.BadgeWarn
	case "error", "failure", "offline", "unmatched", "rejected":
		kind = ui.BadgeBad
	}
	return ui.Badge(state, kind)
}
```

`internal/tui/dashboard.go`, in `reposLines`: replace the state handling and line building. Replace from `state, st := "active", "active"` through the `lines = append(lines, m.row(fmt.Sprintf("repo-%d", i), false, line, inner))` line with:

```go
		state := "active"
		if r.Paused {
			state = "paused"
		}
		if r.Removing {
			state = "removing"
		}
		if r.Error != "" {
			state = "error"
		}
		queue := "–"
		if r.Queued > 0 {
			queue = sAmber.Render(fmt.Sprintf("⧗ %d", r.Queued))
		}
		selected := i == m.repoSel && m.repoFocus()
		sel := "  "
		if selected {
			sel = "▸ "
		}
		badge := stateBadge(state)
		before := sel + cell(r.Name, 14) + " "
		after := strings.Repeat(" ", max(10-ansi.StringWidth(badge), 0)) + " " +
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
			after += " " + last
		}
		if r.Error != "" && !narrow {
			after += "  " + sRed.Render(r.Error)
		}
		if selected {
			pause := ui.NewButton(rowPause, "Pause", ui.Secondary)
			if r.Paused {
				pause.Label = "Resume"
			}
			lines = append(lines, m.selectedRow(fmt.Sprintf("repo-%d", i), before, badge, after,
				rowButtons(pause, ui.NewButton(rowRemove, "Remove", ui.Danger)), "", inner))
			continue
		}
		lines = append(lines, zone.Mark(fmt.Sprintf("repo-%d", i), ui.BadgeRow(sSel, false, before, badge, after, inner)))
```

Add `"github.com/charmbracelet/x/ansi"` to `dashboard.go`'s imports if missing.

`internal/tui/runners.go`, in `runnerLine`: replace from `state := r.State` through the end of the function with:

```go
	state := r.State
	badge := stateBadge(state)
	if state == "busy" {
		badge = spinnerFrames[m.frame%len(spinnerFrames)] + " " + badge
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
	before := sel + cell(r.ID, 8) + " " + cell(r.Repo, 12) + " "
	after := strings.Repeat(" ", max(11-ansi.StringWidth(badge), 0)) + " " + cell(job, inner-50)
	tail := " " + cell(elapsed, 8)
	if selected {
		return m.selectedRow(fmt.Sprintf("runner-%d", i), before, badge, after,
			rowButtons(ui.NewButton(rowLogs, "Logs", ui.Secondary), ui.NewButton(rowStop, "Stop", ui.Danger)), tail, inner)
	}
	return zone.Mark(fmt.Sprintf("runner-%d", i), ui.BadgeRow(sSel, false, before, badge, after+tail, inner))
}
```

Add `"github.com/charmbracelet/x/ansi"` and `zone "github.com/lrstanley/bubblezone"` to `runners.go`'s imports if missing (`zone` is already imported there).

`internal/tui/detail.go`, in `detailSummary`, replace:

```go
	parts := []string{stateStyle(state).Render("● " + state), clean(r.Repo)}
```

with:

```go
	parts := []string{stateBadge(state), clean(r.Repo)}
```

`internal/tui/history.go`: change the `selectedRow` call to the new signature, with no badge yet (Task 24 adds it):

```go
			lines = append(lines, m.selectedRow(id, line, "", "", rowButtons(ui.NewButton(rowCopy, "Copy run URL", ui.Secondary)), tail, inner))
```

- [ ] **Step 4: Regenerate the goldens and run the tests**

Run: `timeout 300 go test ./internal/tui -run TestPagesGolden -update && git diff --stat -- internal/tui/testdata && timeout 300 go test ./internal/tui/...`
Expected: only the dashboard, runners and detail goldens change, each diff swapping `● active`/`◌ paused`/`○ idle`/the spinner-state text for `[ACTIVE]`, `[PAUSED]`, `[IDLE]`, `⣾ [BUSY]`; then `ok` for both packages. Read every golden diff before going on.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "feat(tui): show states as badges in tables"
```

### Task 14: Shared config page and one config stream

**Files:**
- Create: `internal/tui/configpage.go`
- Modify: `internal/tui/settings.go` (`settingsPage` embeds `configPage`; `seq`/`shown`/`nextSeq` removed; `saved`/`refetched` tagged by page; leave recheck)
- Modify: `internal/tui/model.go` (`order`, `leaveFrom`, `nextCfgSeq`, `fetchConfig`, the `configMsg` case), `internal/tui/dialogs.go` (`leave` records `leaveFrom`)
- Test: `internal/tui/settings_test.go`

**Interfaces:**
- Consumes: none.
- Produces: C8 `configPage`, `cfgOrder`, `nextCfgSeq`, `configPage(p)`, `leaveFrom`, page-tagged `savedMsg`/`refetchedMsg`.

**Items:** 5

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

Behaviour does not change except one fix: after a Save chosen in the unsaved-changes dialog, navigation happens only if nothing is still unsaved; an edit made while the save was in flight keeps the page and reopens the dialog.

- [ ] **Step 1: Write the failing test** (append to `internal/tui/settings_test.go`)

```go
// An edit made while a save from the unsaved-changes dialog is in flight
// keeps the page: leaving now would lose it.
func TestLeaveSaveStaysWhenEditedDuringSave(t *testing.T) {
	c := &fakeClient{}
	applyPoll(c)
	m := dirtySettings(t, c)
	if m = feed(m, key("2")); m.overlay != ovUnsaved {
		t.Fatalf("2: overlay %v", m.overlay)
	}
	upd, cmd := m.Update(key("enter")) // Save, the primary
	m = upd.(Model)
	set(m, setIdleTimeout, ui.Value{Text: "9m"})
	m, _ = pump(m, collect(cmd)...)
	if m.page != pageSettings || m.overlay != ovUnsaved {
		t.Fatalf("page %v overlay %v", m.page, m.overlay)
	}
	if f := m.settings.form.Field(setPollInterval); f.Dirty() {
		t.Fatal("the saved field should be reset")
	}
	if f := m.settings.form.Field(setIdleTimeout); !f.Dirty() {
		t.Fatal("the edit made during the save should stay")
	}
}
```

In the same file replace `m.settings.seq + 1` (in `TestUnsavedDialogClosesWhenNothingIsLeft`) with `m.order.seq + 1`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 200 go test ./internal/tui -run 'LeaveSaveStays|UnsavedDialogCloses'`
Expected: build failure on `m.order` (once it builds, `LeaveSaveStays` fails with `page 1 overlay 0`).

- [ ] **Step 3: Write the implementation**

`internal/tui/configpage.go`:

```go
package tui

import "github.com/darkraise/ghr/internal/tui/ui"

// configPage is what a page editing config.yaml owns: its form, focus,
// scroll, save state and rejected-save alert. Settings and Repositories
// each embed one; config responses are ordered once, by the Model.
type configPage struct {
	form          ui.Form
	group         ui.Group
	scroll        int
	saving        bool
	alert         []string // the daemon's messages from a rejected save
	save, discard *ui.Button
}

func newConfigPage(saveID, discardID string) configPage {
	return configPage{
		save:    ui.NewButton(saveID, "Save changes", ui.Primary),
		discard: ui.NewButton(discardID, "Discard", ui.Secondary),
	}
}

// cfgOrder numbers config requests. Responses can arrive out of order, so
// one answering an older request than the config already shown is dropped.
type cfgOrder struct{ seq, shown int }

func (m Model) nextCfgSeq() int {
	m.order.seq++
	return m.order.seq
}

// configPage returns the config page p, which must be pageSettings or pageRepos.
func (m Model) configPage(p page) *configPage {
	return &m.settings.configPage
}
```

`internal/tui/settings.go`: replace the `settingsPage` struct and `newSettingsPage`:

```go
// settingsPage is the Settings form. The Model holds it by pointer, so its
// controls keep their state while Bubble Tea copies the Model.
type settingsPage struct {
	configPage
	repos   []config.Repo         // the repos of the last loaded config, in order
	buttons map[string]*ui.Button // the repo cards' action buttons, by ID
}

func newSettingsPage() *settingsPage {
	return &settingsPage{configPage: newConfigPage(setSave, setDiscard)}
}
```

Add `page page` as the first field of both `savedMsg` and `refetchedMsg`. Delete `nextSeq`. In `loadConfig`, replace

```go
	s := m.settings
	if seq < s.shown {
		return
	}
	s.shown = seq
```

with

```go
	s := m.settings
	if seq < m.order.shown {
		return
	}
	m.order.shown = seq
```

In `saveSettings`, change the returned message to `savedMsg{pageSettings, sent, c.PatchConfig(cx, p)}`.

Replace `saved` and `refetched` with:

```go
// saved handles the patch's outcome. A rejection (a 4xx answer) keeps the
// edits and lists the daemon's messages; any other failure keeps them and
// says what went wrong in a toast. A success fetches the config to reset the
// saved fields; the save stays busy until that refetch is handled, so a
// second save cannot overlap it.
func (m Model) saved(msg savedMsg) (tea.Model, tea.Cmd) {
	s := m.configPage(msg.page)
	if msg.err != nil {
		s.saving = false
		if m.leaveFrom == msg.page {
			m.leaving = false // a failed save stays on the page
		}
		var ae *api.Error
		if !errors.As(msg.err, &ae) || ae.Status < 400 || ae.Status >= 500 {
			m.toast.Show("settings not saved: "+clean(msg.err.Error()), true, m.now())
			return m, nil
		}
		s.alert = strings.Split(clean(msg.err.Error()), "; ")
		m.toast.Show("settings not saved", true, m.now())
		return m, nil
	}
	m.toast.Show("Settings saved", false, m.now())
	c, seq, p := m.c, m.nextCfgSeq(), msg.page
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		var cfg config.Config
		if err := c.Config(cx, &cfg); err != nil {
			return refetchedMsg{page: p, seq: seq, sent: msg.sent, err: err}
		}
		return refetchedMsg{page: p, seq: seq, cfg: &cfg, sent: msg.sent}
	}
}

// refetched merges the config fetched after a save on msg.page. Every
// saved field of that page resets to its new base (a reset, not a merge),
// and a field whose new base differs from what was sent means the daemon
// ignored it.
//
// If the refetch fails, the save stands but cannot be checked: the edits stay
// as typed, a toast says so, and the next periodic refresh brings the form
// up to date.
//
// A save started from the unsaved-changes dialog leaves only from here, once
// the check has run, and only when nothing on the page is still unsaved: an
// edit made while the save was in flight asks again.
func (m Model) refetched(msg refetchedMsg) (tea.Model, tea.Cmd) {
	s := m.configPage(msg.page)
	s.saving = false
	leaving := m.leaving && m.leaveFrom == msg.page
	if leaving {
		m.leaving = false
	}
	if msg.err != nil {
		m.toast.Show("saved, but re-reading the config failed: "+clean(msg.err.Error()), true, m.now())
		return m, nil
	}
	// A newer refresh may already be shown; the reset and the check below
	// then run against it, which reflects the save just as well.
	m.loadConfig(msg.seq, msg.cfg)
	// Controls stay editable while a save is in flight, so a field edited
	// again since the patch was sent keeps its newer edit.
	var keys []string
	for k, v := range msg.sent {
		if f := s.form.Field(k); f != nil && ui.Equal(f.Kind, f.Input.Value(), v) {
			keys = append(keys, k)
		}
	}
	s.form.Reset(keys)
	for _, f := range s.form.Fields() {
		if v, ok := msg.sent[f.Key]; ok && !ui.Equal(f.Kind, f.Base, v) {
			m.toast.Show("daemon did not apply "+fieldName(f.Key)+"; is it older than this ghr?", true, m.now())
			return m, nil
		}
	}
	if leaving {
		if len(s.form.Dirty()) > 0 {
			return m.leave(m.leaveTo)
		}
		return m.goTo(m.leaveTo)
	}
	return m, nil
}
```

`internal/tui/model.go`: add to `Model`, next to `leaveTo`:

```go
	leaveFrom     page           // the config page the unsaved-changes dialog is for
	order         *cfgOrder      // config request numbering, shared by both config pages
```

In `New`, add `order: &cfgOrder{},` to the literal. In `fetchConfig`, replace `seq := m.settings.nextSeq()` with `seq := m.nextCfgSeq()`. Change the `configMsg` field comment to `// orders config responses; see cfgOrder`. In the `configMsg` case, replace `len(m.settings.form.Dirty()) == 0` with `len(m.configPage(m.leaveFrom).form.Dirty()) == 0`.

`internal/tui/dialogs.go`, in `leave`, replace `m.leaveTo = t` with:

```go
		m.leaveTo, m.leaveFrom = t, m.page
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui/...`
Expected: `ok` for both packages, the new test included; goldens unchanged.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "refactor(tui): share config page state and order" -m "Also keeps the page when an edit lands while a save from the
unsaved-changes dialog is in flight, instead of leaving over it."
```

### Task 15: Repositories page and navigation

**Files:**
- Create: `internal/tui/repos.go`
- Modify: `internal/tui/model.go` (page constants, `pageNames`, `Model.repos`, `New`), `internal/tui/input.go` (keys `1`-`5`; page key and mouse routing), `internal/tui/shell.go` (tab-row short names, `pageHeader`, `footerKeys`, `footerPress`), `internal/tui/view.go` (`pageBody`), `internal/tui/dialogs.go` (`pressed`; Help text `1-5`)
- Test: `internal/tui/repos_test.go` (new); page-key presses in every `internal/tui/*_test.go`; goldens under `internal/tui/testdata/TestPagesGolden`, `TestSettingsGolden`, `TestSettingsDropdownGolden`, `TestSettingsDialogGolden`

**Interfaces:**
- Consumes: C8 `configPage`, `newConfigPage` (Task 14); `stateBadge` (Task 13); `offline` (existing, `input.go`).
- Produces: C8 page constants and `reposPage`; `repoIndex`, `repoState`, `selectedRepoStatus` used by Tasks 16 to 23.

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Shift the page keys in the existing tests**

Every match of this command at commit `9fa16fe` is a page switch (35 lines):

```bash
grep -n 'key("[1-4]")\|keys("[1-4]"' internal/tui/*_test.go
```

In each matched call, map the page keys: `"2"` → `"3"`, `"3"` → `"4"`, `"4"` → `"5"`; `"1"` stays. Only the first argument of a `keys(...)` list is a page key, except `keys("1", "right", "down")` and `keys("1", "l")`, where only `"1"` is (unchanged). Also change `"1-4 switch page"` to `"1-5 switch page"` wherever a test asserts it.

The `run(t, m, ...)` helper and the local `down(first, n)` helper in `TestShortTerminalKeepsSelectionVisible` (`tui_test.go`) also send page keys. At `9fa16fe` these six lines are all of them:

```bash
grep -n 'run(t, [^)]*"[1-4]"\|down("[1-4]"' internal/tui/*_test.go
```

| Line (`tui_test.go`) | At `9fa16fe` | Change to |
|---|---|---|
| 503 | `run(t, m, "2", "tab")` (expects `pageRunners`) | `run(t, m, "3", "tab")` |
| 533 | `run(t, m, "2", "p", "+", "d")` (repo keys on the Runners page) | `run(t, m, "3", "p", "+", "d")` |
| 537 | `run(t, m, "4", "x")` (x on the Settings page) | `run(t, m, "5", "x")` |
| 873 | `down("1", 11)` | unchanged |
| 878 | `down("2", 9)` (the runners subtest) | `down("3", 9)` |
| 882 | `run(t, upd.(Model), "4")` (the settings subtest) | `run(t, upd.(Model), "5")` |

No test at `9fa16fe` types a digit into a text field through these helpers; if a match does (an Add-repo or Settings field is focused when the digit is sent), it is input, not a page key, and stays.

- [ ] **Step 2: Write the failing tests** (`internal/tui/repos_test.go`)

```go
package tui

import (
	"strings"
	"testing"
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
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/tui -run 'Repositories'`
Expected: build failure, `pageRepos undefined`.

- [ ] **Step 4: Write the implementation**

`internal/tui/model.go`: change the page constants and names:

```go
const (
	pageDashboard page = iota
	pageRepos
	pageRunners
	pageHistory
	pageSettings
	pageDetail // a runner's detail page; not in the sidebar
)

var pageNames = []string{"Dashboard", "Repositories", "Runners", "History", "Settings"}
```

Add `repos *reposPage` to `Model` after `settings *settingsPage`, and `repos: newReposPage(),` to the `New` literal.

`internal/tui/input.go`: in `press`, change `case "1", "2", "3", "4":` to `case "1", "2", "3", "4", "5":`. In `handleKey`, after the `pageSettings` block, add:

```go
	if m.page == pageRepos {
		if ok, mm, cmd := m.reposHandleKey(k); ok {
			return mm, cmd
		}
	}
```

In `handleMouse`, before the `pageDashboard` block, add:

```go
	if m.overlay == ovNone && m.page == pageRepos {
		if ok, mm, cmd := m.reposMouse(msg); ok {
			return mm, cmd
		}
	}
```

`internal/tui/shell.go`: in `tabRow`, set `short := []string{"Dash", "Repo", "Run", "Hist", "Set"}`. In `pageHeader`, add before the `pageDashboard` case:

```go
	case m.page == pageRepos:
		title, right = sBold.Render(pageNames[m.page]), m.repos.add.View(m.repos.group.FocusedID() == reposAdd, 0)
```

In `footerKeys`, add `case pageRepos: return m.reposFooterKeys()`. In `footerPress`, after the `pageDetail` block, add (the hints act directly, so a click never types into a focused field):

```go
	if m.page == pageRepos {
		switch k {
		case "a":
			return m.openAddRepo()
		case "p":
			return m.reposAction(reposPause)
		case "d":
			return m.reposAction(reposRemove)
		}
	}
```

`internal/tui/view.go`: in `pageBody`, add `case pageRepos: return m.reposView(w, h)`.

`internal/tui/dialogs.go`: in `pressed`, add before `default:`:

```go
	case reposAdd:
		return m.openAddRepo()
	case reposPause, reposRemove:
		return m.reposAction(id)
```

and in `helpGroups`/`helpText` change `"1-4"` to `"1-5"` and the note `"1-4 type into it"` to `"1-5 type into it"`.

`internal/tui/repos.go`:

```go
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	reposList   = "repos/list"   // the list card's tab stop
	reposAdd    = "repos/add"    // the header's Add repository button
	reposPause  = "repos/pause"  // the panel's Pause/Resume button
	reposRemove = "repos/remove" // the panel's Remove button

	reposListW = 30 // the list card's width in the wide layout
)

// reposPage is the Repositories page. The Model holds it by pointer, so its
// state survives Bubble Tea copying the Model.
type reposPage struct {
	configPage
	selected      string // the selected repo's name; selection follows the name across refreshes
	idx           int    // the selection's last position, used when its repo disappears
	add           *ui.Button
	pause, remove *ui.Button
}

func newReposPage() *reposPage {
	rp := &reposPage{
		configPage: newConfigPage("repos/save", "repos/discard"),
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

// syncRepoControls updates the panel buttons for the selected repo and sets
// the page's focus order: the header button, the list, then the panel.
func (m Model) syncRepoControls() {
	rp := m.repos
	items := []ui.Widget{rp.add, stop{reposList}}
	rp.add.SetDisabled(!m.connected)
	if r := m.selectedRepoStatus(); r != nil {
		rp.pause.Label = "Pause"
		if r.Paused {
			rp.pause.Label = "Resume"
		}
		for _, b := range []*ui.Button{rp.pause, rp.remove} {
			b.SetDisabled(!m.connected || r.Removing)
		}
		items = append(items, rp.pause, rp.remove)
	}
	rp.group.Set(items)
}

// reposList renders the list card, w columns wide and h lines tall.
func (m Model) reposListCard(w, h int) string {
	sel := m.repoIndex()
	focused := m.repos.group.FocusedID() == reposList
	var lines []string
	for i, r := range m.st.Repos {
		cursor := "  "
		if i == sel {
			cursor = "› "
		}
		badge := stateBadge(repoState(r))
		row := ui.BadgeRow(sSel, i == sel && focused, cursor+cell(clean(r.Name), 11)+" ", badge, "", w-4)
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

// reposPanel renders the selected repo's panel, w columns wide.
func (m Model) reposPanel(r model.RepoStatus, w int) []string {
	return strings.Split(box(clean(r.Name), w, m.repoSummary(r)), "\n")
}

// reposView renders the page in w columns and h lines: the list card and
// the panel side by side when wide, stacked otherwise.
func (m Model) reposView(w, h int) string {
	m.syncRepoControls()
	r := m.selectedRepoStatus()
	if r == nil {
		return box("Repos", min(w, reposListW), []string{sDim.Render("No repositories yet"), "",
			m.repos.add.View(m.repos.group.FocusedID() == reposAdd, 0)})
	}
	if m.width >= wideMin {
		panel := strings.Join(m.reposPanel(*r, w-reposListW-1), "\n")
		return lipgloss.JoinHorizontal(lipgloss.Top, m.reposListCard(reposListW, h), " ", panel)
	}
	list := m.reposListCard(w, min(len(m.st.Repos)+2, max(h/3, 3)))
	return list + "\n" + strings.Join(m.reposPanel(*r, w), "\n")
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui -run 'TestPagesGolden|TestSettingsGolden|TestSettingsDropdownGolden|TestSettingsDialogGolden' -update && git diff --stat -- internal/tui/testdata && timeout 300 go test ./internal/tui/...`
Expected: every page golden and the three Settings golden families change, and only in the sidebar and tab row (a fifth page, renumbered keys) and the footer's `1-5`; then `ok`. Read each golden diff; any other change is a bug to fix before committing.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "feat(tui): add the Repositories page"
```

### Task 16: Repositories editor and save

**Files:**
- Modify: `internal/tui/repos.go` (form specs, panel cards, Runners get, save, dirty marks, panel scroll)
- Modify: `internal/tui/settings.go` (`loadConfig` merges the repos form; `fieldName` reads `repos/` keys; `unsavedBar` and `alertBox` take a `*configPage`)
- Modify: `internal/tui/configpage.go` (`configPage(p)` returns the repos page), `internal/tui/dialogs.go` (`pressed`: Repositories Save and Discard), `internal/tui/input.go` (`switchPage` fetches the config for the Repositories page)
- Test: `internal/tui/repos_test.go`, goldens `internal/tui/testdata/TestRepositoriesGolden/{120,100}.golden` (new)

**Interfaces:**
- Consumes: C8 `reposPage`, `repoIndex`, `selectedRepoStatus`, `repoState` (Task 15); C7 `Row.Lines`, `StackBelow` (Task 12); C8 `configPage` (Task 14).
- Produces: C8 `reposKey`, `splitReposKey`, `reposSpecs`, `saveRepos`, `reposSave`/`reposDiscard`, `syncRepoControls`, `reposPanelLines`, `reposPanelSize`, `reposScrollToFocus`, `runnersGet`.

**Items:** 1, 2, 5

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

The Settings repo cards stay until Task 17. Their keys (`settings/repo/…`) differ from these (`repos/…`), so the two forms do not interfere.

- [ ] **Step 1: Write the failing tests** (append to `internal/tui/repos_test.go`; add imports `"fmt"`, `"reflect"`, `"github.com/charmbracelet/x/exp/golden"`, `"github.com/darkraise/ghr/internal/config"`, `"github.com/darkraise/ghr/internal/tui/ui"` — the golden import path is the one `tui_test.go` already uses)

```go
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
		for _, want := range []string{"[ − ]", "[ + ]", "darkcloud-linux", "dc-e2e-", "Runners get", "self-hosted", "homelab (global)", "keep their labels", "Pause", "Remove"} {
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/tui -run 'TestRepositories'`
Expected: build failure, `reposKey undefined`.

- [ ] **Step 3: Write the implementation**

`internal/tui/repos.go`: add `"github.com/darkraise/ghr/internal/config"` to the imports; add the IDs `reposSave = "repos/save"` and `reposDiscard = "repos/discard"` to the const block, and change `newReposPage` to use them: `configPage: newConfigPage(reposSave, reposDiscard),`. Then add:

```go
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
```

Replace Task 15's `syncRepoControls`, `reposListCard`, `reposPanel` and `reposView` with:

```go
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
```

In `reposHandleKey`, add before `case "a":`:

```go
	case "ctrl+s":
		mm, cmd := m.saveRepos()
		return true, mm, cmd
	case "pgup":
		_, ph := m.reposPanelSize(m.contentSize())
		rp.scroll = max(rp.scroll-ph/2, 0)
	case "pgdown":
		_, ph := m.reposPanelSize(m.contentSize())
		rp.scroll += ph / 2
```

The `a`, `p` and `d` cases apply only while no text or tag input is being edited: the focused control takes keys first (`rp.group.Key` above), so an editing field already swallows them; no extra check is needed. Focus moves need no scrolling code in the key and mouse handlers: `reposView` scrolls whenever the focus differs from the one it last showed. Add that field to `reposPage`, after `idx int`:

```go
	shownFocus    string // the focus reposView last scrolled to
```

`internal/tui/input.go`, in `switchPage`, add (entering the page refreshes the config, as Settings does):

```go
	case pageRepos:
		return m, m.fetchConfig()
```

In `reposMouse`, before the final `rp.group.Mouse`, add the wheel over the panel:

```go
	if msg.Action == tea.MouseActionPress && zone.Get("repos/panel").InBounds(msg) {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			rp.scroll = max(rp.scroll-3, 0)
			return true, m, nil
		case tea.MouseButtonWheelDown:
			rp.scroll += 3
			return true, m, nil
		}
	}
```

(place it before the existing list-wheel loop's `return false, m, nil`, so a wheel over the panel scrolls it and a wheel over the list moves the selection).

`internal/tui/settings.go`:
- In `loadConfig`, after the `if gone := s.load(c); …` block, add `m.repos.form.Merge(reposSpecs(c))`.
- In `fieldName`, add at the top:

```go
	if strings.HasPrefix(key, "repos/") {
		name, field := splitReposKey(key)
		if field == "cleanup" {
			field = "cleanup_name_prefixes"
		}
		return name + "." + field
	}
```

- Change `unsavedBar` to take the page and an optional name list:

```go
// unsavedBar is the sticky line shown while page cp has edits. Its text
// names them when names is set and shortens to fit w columns; the buttons
// are never cut.
func (m Model) unsavedBar(cp *configPage, w int, names string) string {
	n := len(cp.form.Dirty())
	if n == 0 {
		return ""
	}
	full := fmt.Sprintf("● %d unsaved changes", n)
	if n == 1 {
		full = "● 1 unsaved change"
	}
	focused := cp.group.FocusedID()
	buttons := "  " + cp.discard.View(focused == cp.discard.ID(), 0) + "  " + cp.save.View(focused == cp.save.ID(), 0)
	candidates := []string{full, fmt.Sprintf("● %d unsaved", n)}
	if names != "" {
		candidates = append([]string{full + " (" + names + ")"}, candidates...)
	}
	text := fmt.Sprintf("● %d", n)
	for _, t := range candidates {
		if ansi.StringWidth(t+buttons) <= w {
			text = t
			break
		}
	}
	return sAmber.Render(text) + buttons
}
```

  and update its Settings callers to `m.unsavedBar(&m.settings.configPage, w, "")`.
- Change `alertBox(w int)` to `alertBox(cp *configPage, w int)`, reading `cp.alert`, and update its Settings callers to `m.alertBox(&m.settings.configPage, w)`.

`internal/tui/configpage.go`: make `configPage` return the repos page:

```go
func (m Model) configPage(p page) *configPage {
	if p == pageRepos {
		return &m.repos.configPage
	}
	return &m.settings.configPage
}
```

`internal/tui/dialogs.go`, in `pressed`, add before `default:`:

```go
	case reposSave:
		return m.saveRepos()
	case reposDiscard:
		m.repos.form.Discard()
		m.repos.alert = nil
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui -run TestRepositoriesGolden -update && timeout 300 go test ./internal/tui/...`
Expected: two new goldens, `TestRepositoriesGolden/120.golden` and `100.golden`; read both (list card with the dirty mark on darkcloud, the panel with Capacity, Labels with the Runners get lines, Cleanup, and the unsaved bar). Then `ok` for both packages, other goldens unchanged.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "feat(tui): edit repositories on their page"
```

### Task 17: Leave guard for both config pages

**Files:**
- Modify: `internal/tui/dialogs.go` (`leave`, `pressed` leave buttons, the unsaved dialog text), `internal/tui/repos.go` (`saveConfig`)
- Test: `internal/tui/repos_test.go`

**Interfaces:**
- Consumes: C8 `configPage(p)`, `leaveFrom` (Task 14); `saveRepos` (Task 16).
- Produces: `func (m Model) saveConfig(p page) (tea.Model, tea.Cmd)`.

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test** (append to `internal/tui/repos_test.go`)

```go
func TestRepositoriesLeaveGuard(t *testing.T) {
	c := &fakeClient{}
	m := onRepos(t, c, 120, 40)
	repoInput(m, "darkmem", "max").SetValue(ui.Value{Num: 2, Set: true})
	if m = feed(m, key("1")); m.overlay != ovUnsaved || m.leaveFrom != pageRepos {
		t.Fatalf("1: overlay %v from %v", m.overlay, m.leaveFrom)
	}
	if v := m.View(); !strings.Contains(v, "You have 1 unsaved change on the Repositories page.") {
		t.Fatalf("dialog:\n%s", v)
	}
	m = click(t, m, btnLeaveDiscard)
	if m.page != pageDashboard || len(m.repos.form.Dirty()) != 0 {
		t.Fatalf("discard: page %v dirty %d", m.page, len(m.repos.form.Dirty()))
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
```

Add `"github.com/darkraise/ghr/internal/model"` to the test file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 200 go test ./internal/tui -run TestRepositoriesLeaveGuard`
Expected: FAIL at `1: overlay 0 from 0`.

- [ ] **Step 3: Write the implementation**

`internal/tui/repos.go`:

```go
// saveConfig saves the config page p.
func (m Model) saveConfig(p page) (tea.Model, tea.Cmd) {
	if p == pageRepos {
		return m.saveRepos()
	}
	return m.saveSettings()
}
```

`internal/tui/dialogs.go`: replace `leave`:

```go
// leave goes to t. Leaving a config page (Settings or Repositories) with
// unsaved changes first asks whether to save them, discard them or stay;
// ctrl+c never comes here.
func (m Model) leave(t leaveTarget) (tea.Model, tea.Cmd) {
	onConfig := m.page == pageSettings || m.page == pageRepos
	staying := !t.quit && t.page == m.page
	if onConfig && !staying {
		cp := m.configPage(m.page)
		if cp.saving {
			// The save decides: its result leaves or stays (see refetched).
			m.toast.Show("wait for the save to finish", true, m.now())
			return m, nil
		}
		if len(cp.form.Dirty()) > 0 {
			m.leaveTo, m.leaveFrom = t, m.page
			m.openDialog(ovUnsaved, btnLeaveStay, ui.NewButton(btnLeaveStay, "Stay", ui.Secondary),
				ui.NewButton(btnLeaveDiscard, "Discard", ui.Secondary), ui.NewButton(btnLeaveSave, "Save", ui.Primary))
			return m, nil
		}
	}
	return m.goTo(t)
}
```

In `pressed`, replace the `btnLeaveDiscard` and `btnLeaveSave` cases:

```go
	case btnLeaveDiscard:
		m.overlay = ovNone
		cp := m.configPage(m.leaveFrom)
		cp.form.Discard()
		cp.alert = nil
		return m.goTo(m.leaveTo)
	case btnLeaveSave:
		// Leave only once the save succeeds; refetched does the navigation.
		m.overlay = ovNone
		upd, cmd := m.saveConfig(m.leaveFrom)
		m = upd.(Model)
		m.leaving = cmd != nil
		return m, cmd
```

In `withOverlay`, replace the `ovUnsaved` case body:

```go
	case ovUnsaved:
		n := len(m.configPage(m.leaveFrom).form.Dirty())
		where := pageNames[m.leaveFrom]
		body := fmt.Sprintf("You have %d unsaved changes on the %s page.", n, where)
		if n == 1 {
			body = fmt.Sprintf("You have 1 unsaved change on the %s page.", where)
		}
		dialog = modal("Unsaved changes", body, m.dlgButtons, m.dlg.FocusedID(), w)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui/...`
Expected: `ok` for both packages; the Settings leave-guard tests still pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): guard leaving the Repositories page"
```

### Task 18: Settings without repo cards

**Files:**
- Modify: `internal/tui/settings.go`, `internal/tui/dialogs.go`
- Test: `internal/tui/settings_test.go`, `internal/tui/repos_test.go`, goldens under `internal/tui/testdata`

**Interfaces:**
- Consumes: Task 16's Repositories form (it now owns every repo field).
- Produces: a global-only Settings page.

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Move the removed-repo toast test** (append to `internal/tui/repos_test.go`)

```go
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
```

Add `zone "github.com/lrstanley/bubblezone"` to the test file's imports if missing.

- [ ] **Step 2: Update the Settings tests**

In `internal/tui/settings_test.go` delete these functions entirely (the Repositories tests of Tasks 15 to 17 cover their behaviour): `TestSettingsRepoDefaultFollowsEditedMode`, `TestSettingsRemovingRepoIsDisabled`, `TestSettingsRefreshMergesRepoChanges`, `TestSettingsRefreshScrollsMovedFocusIntoView`, `TestSettingsRepoActions`, `TestSettingsRepoActionsDisabledWhileRemoving`, `TestSettingsAddRepositoryButton`, `TestSettingsPageSwitchScrollsMovedFocusIntoView`.

In `TestSettingsShowsEverySettingButOwnerAsAControl`, delete the line `"darkcloud", "darkcloud-linux ✕", "dc-e2e- ✕", "darkmem", "darkagents (paused)",` and everything from `lines := settingsLines(m, 123)` to the end of the function body, then add before the closing brace:

```go
	if strings.Contains(v, "darkcloud") {
		t.Error("repo cards belong to the Repositories page")
	}
```

In `TestSettingsFocusOrderScrollsIntoView`, replace from `m = feed(m, key("shift+tab")) // wraps to the last control` to the end of the function with:

```go
	m = feed(m, key("shift+tab")) // wraps to the last control
	if got := m.settings.group.FocusedID(); got != setCPUQuota {
		t.Fatalf("wrap: %q", got)
	}
	v := m.View()
	if !strings.Contains(v, "› [ 200%") || lipgloss.Height(v) > 22 || m.settings.scroll == 0 {
		t.Fatalf("last control not scrolled into view (scroll %d):\n%s", m.settings.scroll, v)
	}
	m = feed(m, key("tab"))
	if v := m.View(); !strings.Contains(v, "Mode               › [ queue ▾ ]") || m.settings.scroll != 0 {
		t.Fatalf("first control not scrolled back into view:\n%s", v)
	}
}
```

Replace `TestSettingsSanitisesConfigText` with (the repo prefix assertion moved to `TestRepositoriesSanitisesConfigText`):

```go
func TestSettingsSanitisesConfigText(t *testing.T) {
	m := sampleModel(&fakeClient{}, 140, 40)
	// YAML's \e and \a escapes put real ESC and BEL bytes into the parsed config.
	m.cfg = parseConfig(t, `owner: "evil\e]0;pwned\a"
labels: ["bad\e[2Jlabel"]
`)
	m.settings.load(m.cfg)
	v := strings.Join(settingsLines(m, 123), "\n")
	if strings.ContainsAny(v, "\x1b\a") || !strings.Contains(v, "evil") || !strings.Contains(v, "badlabel") {
		t.Fatalf("config text not sanitised: %q", v)
	}
}
```

Replace `TestSettingsSavePatchHoldsOnlyDirtyFields` with:

```go
func TestSettingsSavePatchHoldsOnlyDirtyFields(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, setPollInterval, ui.Value{Text: "30s"})
	set(m, setLabels, ui.Value{List: []string{"homelab", "gpu"}})
	set(m, setMemoryMax, ui.Value{Text: "4G"})
	if v := m.View(); !strings.Contains(v, "● 3 unsaved changes") || !strings.Contains(v, "( Discard )") || !strings.Contains(v, "[ Save changes ]") {
		t.Fatalf("unsaved bar missing:\n%s", v)
	}
	m = feed(m, key("ctrl+s"))
	if len(c.patches) != 1 {
		t.Fatalf("patches %d", len(c.patches))
	}
	p := c.patches[0]
	if *p.PollInterval != "30s" || strings.Join(*p.Labels, ",") != "homelab,gpu" || *p.RunnerLimits.MemoryMax != "4G" || p.RunnerLimits.CPUQuota != nil {
		t.Fatalf("patch %+v", p)
	}
	if p.Mode != nil || p.GlobalMax != nil || p.StartTimeout != nil || p.IdleTimeout != nil || p.HistoryRetention != nil ||
		p.DiskHighWater != nil || p.BuildCacheKeep != nil || len(p.Repos) != 0 {
		t.Fatalf("patch carries clean fields: %+v", p)
	}
}
```

Replace `TestSettingsInAppChecksBlockSave` with:

```go
func TestSettingsInAppChecksBlockSave(t *testing.T) {
	c := &fakeClient{}
	m := onSettings(t, c, 120, 30)
	set(m, setIdleTimeout, ui.Value{Text: "soon"})
	m = feed(m, key("ctrl+s"))
	if v := m.View(); len(c.patches) != 0 || !strings.Contains(v, "fix the highlighted settings first") {
		t.Fatalf("patches %d:\n%s", len(c.patches), v)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/tui -run 'Settings|RemovedRepoToast'`
Expected: FAIL — Settings still shows `darkcloud` and still has repo fields in its patch focus order.

- [ ] **Step 4: Write the implementation** (`internal/tui/settings.go`, `internal/tui/dialogs.go`)

- Delete the `setAddRepo` constant and the Settings `repoKey` function.
- In `settingsSpecs`, delete the whole `for _, r := range c.Repos { … }` loop.
- Replace the `settingsPage` struct and `load`:

```go
// settingsPage is the Settings form of global settings. The Model holds it
// by pointer, so its controls keep their state while Bubble Tea copies the Model.
type settingsPage struct {
	configPage
}

// load merges a freshly fetched config into the form: clean fields take the
// new values and dirty ones keep their edits.
func (s *settingsPage) load(c *config.Config) { s.form.Merge(settingsSpecs(c)) }
```

- In `loadConfig`, replace the `if gone := s.load(c); len(gone) > 0 { … }` block (which sits before `m.cfg = c` moves; keep the order below) so the function reads:

```go
func (m *Model) loadConfig(seq int, c *config.Config) {
	s := m.settings
	if seq < m.order.shown {
		return
	}
	m.order.shown = seq
	var gone []string
	if m.cfg != nil {
		for _, r := range m.cfg.Repos {
			if c.Repo(r.Name) == nil {
				gone = append(gone, r.Name)
			}
		}
	}
	m.cfg = c
	focus := s.group.FocusedID()
	s.load(c)
	m.repos.form.Merge(reposSpecs(c))
	if len(gone) > 0 {
		m.toast.Show("repository "+strings.Join(gone, ", ")+" was removed", false, m.now())
	}
	if m.page == pageSettings && m.overlay == ovNone {
		if m.settingsSections(); s.group.FocusedID() != focus {
			m.scrollToFocus()
		}
	}
}
```

- Delete `button`, `repoAction` and `warmRow`, and the `"context"` import (`repoAction` was its only user at `9fa16fe`; `grep -n 'context\.' internal/tui/settings.go` must then print nothing).
- In `checkErrors`, delete the `for _, r := range s.repos { … }` loop and change the doc comment to "returns the in-app check failures by field key: a duration that does not parse or is below its floor."
- In `buildPatch`, delete the `if rest, ok := strings.CutPrefix(f.Key, "settings/repo/"); ok { … continue }` block.
- In `fieldName`, delete the `settings/repo/` branch.
- In `settingsSections`: delete the `queue` and `removing` variables and their loop; replace the per-field disable loop with `for _, f := range s.form.Fields() { f.Input.SetDisabled(!m.connected) }`; delete the `for _, r := range s.repos { … }` card loop, the `add := s.button(setAddRepo, …)` section, and the line `errs := s.checkErrors()` (the card loop's `warmRow` was its only reader).
- Then run `timeout 120 go vet ./internal/tui` and fix any "declared and not used" or unused import it reports in `settings.go` before going on.
- `internal/tui/dialogs.go`, in `pressed`: change `case setAddRepo, dashAdd:` to `case dashAdd:`, and delete the `default:` branch's `strings.HasPrefix(id, "settings/repo/")` block (keep `default:` empty or remove it).

- [ ] **Step 5: Regenerate the goldens and run the tests**

Run: `timeout 300 go test ./internal/tui -run 'Golden' -update && git diff --stat -- internal/tui/testdata && timeout 300 go test ./internal/tui/...`
Expected: only the Settings goldens change (repo cards gone; tab-row and sidebar lines already changed in Task 15); then `ok`. Read every golden diff.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "refactor(tui): move repo cards off Settings"
```

### Task 19: Dashboard Edit and Add repository

**Files:**
- Modify: `internal/tui/view.go` (`rowEdit`), `internal/tui/dashboard.go` (`reposLines` row buttons), `internal/tui/input.go` (`press` handles `e`; the row-button click map), `internal/tui/shell.go` (`newPageGroups` label; Dashboard footer)
- Test: `internal/tui/dashboard_test.go`, goldens

**Interfaces:**
- Consumes: Task 15's `reposPage` (`selected`, `group`, `reposList`).
- Produces: `rowEdit = "row/edit"`.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test** (append to `internal/tui/dashboard_test.go`)

```go
func TestDashboardEditOpensTheRepository(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 30), key("down")) // the Repositories card: darkmem
	if v := m.View(); !strings.Contains(v, "( Edit )") || !strings.Contains(v, "[ + Add repository ]") {
		t.Fatalf("dashboard:\n%s", v)
	}
	if m = feed(m, key("e")); m.page != pageRepos || m.repos.selected != "darkmem" {
		t.Fatalf("e: page %v selected %q", m.page, m.repos.selected)
	}
	m = feed(m, key("1"))
	if m = click(t, m, rowEdit); m.page != pageRepos || m.repos.selected != "darkmem" {
		t.Fatalf("click: page %v selected %q", m.page, m.repos.selected)
	}
}
```

The header button's label changes, so update the two existing assertions on it in `internal/tui/dashboard_test.go`:
- `TestDashboardTabOrder`: `"› [ + Add ]"` → `"› [ + Add repository ]"`.
- `TestDashboardHeaderButtons`: `"[ + Add ]"` → `"[ + Add repository ]"`.

(The `+ Add` in comments and failure messages may stay; `grep -n '"[^"]*+ Add \]' internal/tui/*_test.go` must print nothing afterwards.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 200 go test ./internal/tui -run 'TestDashboardEditOpensTheRepository|TestDashboardTabOrder|TestDashboardHeaderButtons'`
Expected: build failure, `rowEdit undefined`.

- [ ] **Step 3: Write the implementation**

`internal/tui/view.go`: add `rowEdit = "row/edit"` to the row-button const block.

`internal/tui/dashboard.go`, in `reposLines`, replace the selected row's buttons:

```go
			lines = append(lines, m.selectedRow(fmt.Sprintf("repo-%d", i), before, badge, after,
				rowButtons(ui.NewButton(rowEdit, "Edit", ui.Secondary), pause, ui.NewButton(rowRemove, "Remove", ui.Danger)), "", inner))
```

`internal/tui/input.go`: in `press`, add before `case "a":`:

```go
	case "e":
		if r := m.selectedRepo(); r != nil && m.repoFocus() {
			m.repos.selected = r.Name
			m.repos.group.Focus(reposList)
			return m.leave(leaveTarget{page: pageRepos})
		}
```

In `handleMouse`, add `rowEdit: "e"` to the row-button map.

`internal/tui/shell.go`: in `newPageGroups`, change the add button to `ui.NewButton(dashAdd, "+ Add repository", ui.Primary)`. In `footerKeys`' Dashboard card list, insert `{"e", "edit"}` after `{"p", "pause"}`.

- [ ] **Step 4: Regenerate the goldens and run the tests**

Run: `timeout 300 go test ./internal/tui -run TestPagesGolden -update && git diff --stat -- internal/tui/testdata && timeout 300 go test ./internal/tui/...`
Expected: dashboard goldens change (header button, `( Edit )` on the selected repo row, `e edit` in the footer); then `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): edit a repo from the Dashboard"
```

### Task 20: TUI client additions

**Files:**
- Modify: `internal/tui/model.go` (`Client` interface)
- Modify: `internal/tui/tui_test.go` (`fakeClient`)

**Interfaces:**
- Consumes: C3 client methods (Tasks 5 to 10).
- Produces: the TUI `Client` interface methods below and the `fakeClient` fields Tasks 21 to 26 set.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 0 = 1

- [ ] **Step 1: Extend the interface** (`internal/tui/model.go`, inside `type Client interface`)

```go
	Token(ctx context.Context) (model.TokenStatus, error)
	SetToken(ctx context.Context, token string) error
	Registrations(ctx context.Context, repo string) ([]model.Registration, error)
	DeleteRegistration(ctx context.Context, repo string, id int64) error
	StartLabelCheck(ctx context.Context, repo string) error
	LabelCheck(ctx context.Context, repo string) (model.LabelCheck, error)
	Reload(ctx context.Context) ([]string, error)
	Prune(ctx context.Context) error
	Metrics(ctx context.Context) (model.Metrics, error)
```

- [ ] **Step 2: Extend the fake** (`internal/tui/tui_test.go`)

Add fields to `fakeClient`:

```go
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
```

and methods (reads go to `reads`, not `calls`, so `actions()` keeps listing daemon actions only):

```go
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
```

- [ ] **Step 3: Run the tests**

Run: `timeout 300 go test ./internal/tui/... ./cmd/...`
Expected: `ok` (`cmd/ghr` passes `*api.Client`, which has every method since Tasks 5 to 10).

- [ ] **Step 4: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): add management calls to the client"
```

### Task 21: Token section and replacement dialog

**Files:**
- Create: `internal/tui/manage.go`
- Modify: `internal/tui/model.go` (`Model.mg`, `New`, `tokenMsg`/`tokenSetMsg` cases, tick refresh, `ovToken`), `internal/tui/input.go` (`switchPage`, key and mouse routing for `ovToken`), `internal/tui/dialogs.go` (`buttonOverlay`, `pressed`, `withOverlay`), `internal/tui/settings.go` (the GitHub token section)
- Test: `internal/tui/manage_test.go` (new), `internal/tui/settings_test.go` (focus-order wrap)

**Interfaces:**
- Consumes: Task 20's `Client.Token`, `Client.SetToken`; C7 `TextField.Mask` (Task 12); `stateBadge` (Task 13).
- Produces: `type manageState struct` held as `Model.mg *manageState` (Tasks 22 to 26 add fields); `overlay` value `ovToken`; button IDs `setReplaceToken = "settings/token"`, `tokOK = "token/ok"`, `tokCancel = "token/cancel"`.

**Items:** 4

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests** (`internal/tui/manage_test.go`)

```go
package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

func tokenStatus(days int) model.TokenStatus {
	exp := now.Add(time.Duration(days) * 24 * time.Hour)
	at := now.Add(-12 * time.Second)
	rem, lim := 4800, 5000
	return model.TokenStatus{State: "ok", ExpiresAt: &exp, CheckedAt: &at, RateRemaining: &rem, RateLimit: &lim}
}

func TestTokenSection(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87)}
	m := onSettings(t, c, 140, 60)
	v := m.View()
	for _, want := range []string{"GitHub token", "[OK]", "expires 2026-12-29 (87 days)", "4800 / 5000", "checked just now",
		"Replacing checks read access to the first repository only", "[ Replace token ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	c.token = tokenStatus(10)
	m = feed(m, tokenMsg{seq: m.mg.tokenSeq, ts: c.token})
	if v := m.View(); !strings.Contains(v, "[EXPIRES SOON]") {
		t.Fatalf("expiring:\n%s", v)
	}
	m = feed(m, tokenMsg{seq: m.mg.tokenSeq - 1, ts: model.TokenStatus{State: "rejected"}})
	if strings.Contains(m.View(), "[REJECTED]") {
		t.Fatal("a stale token reply was shown")
	}
}

// A failed token read stays visible with a Retry button until a retry succeeds.
func TestTokenSectionErrorRetries(t *testing.T) {
	c := &fakeClient{tokenErr: &api.Error{Status: 502, Msg: "daemon busy"}}
	m := onSettings(t, c, 140, 60)
	if v := m.View(); !strings.Contains(v, "✖ daemon busy") || !strings.Contains(v, "( Retry )") {
		t.Fatalf("error:\n%s", v)
	}
	c.tokenErr, c.token = nil, tokenStatus(87)
	m = click(t, m, setTokenRetry)
	if v := m.View(); !strings.Contains(v, "[OK]") || strings.Contains(v, "daemon busy") {
		t.Fatalf("after retry:\n%s", v)
	}
}

func TestTokenDialogMasksAndReplaces(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87)}
	m := onSettings(t, c, 140, 60)
	if m = click(t, m, setReplaceToken); m.overlay != ovToken {
		t.Fatalf("overlay %v", m.overlay)
	}
	m = feed(m, keys("g", "h", "p", "_", "n", "e", "w")...)
	for _, state := range []string{"editing", "blurred"} {
		if v := m.View(); strings.Contains(v, "ghp_new") || !strings.Contains(v, "•••••••") {
			t.Fatalf("%s: token visible or not masked:\n%s", state, v)
		}
		m = feed(m, key("tab"))
	}
	m.tok.group.Focus(tokOK)
	m = feed(m, key("enter"))
	if got := strings.Join(c.actions(), "|"); got != "set-token 7 chars" {
		t.Fatalf("actions %q", got)
	}
	if m.overlay != ovNone || !strings.Contains(m.View(), "GitHub token replaced") {
		t.Fatalf("after replace: overlay %v", m.overlay)
	}
}

func TestTokenDialogRejectionAndLateReply(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87), setTokenErr: &api.Error{Status: 400, Msg: "new token rejected: 401 Bad credentials"}}
	m := onSettings(t, c, 140, 60)
	m = click(t, m, setReplaceToken)
	m = feed(m, keys("b", "a", "d", "enter")...) // enter commits the field and moves focus to Cancel
	m.tok.group.Focus(tokOK)
	m = feed(m, key("enter"))
	if m.overlay != ovToken || m.tok == nil {
		t.Fatalf("rejection closed the dialog: overlay %v", m.overlay)
	}
	if v := m.View(); !strings.Contains(v, "new token rejected") || m.tok.field.Value().Text != "" {
		t.Fatalf("rejection: field %q\n%s", m.tok.field.Value().Text, v)
	}
	first := m.tok
	m = feed(m, key("esc"))
	m = click(t, m, setReplaceToken)
	upd, _ := m.Update(tokenSetMsg{d: first, err: nil}) // a late reply for the closed dialog
	m = upd.(Model)
	if m.overlay != ovToken || m.tok == first {
		t.Fatal("a late reply changed the new dialog")
	}
	var _ tea.Model = m
}

// While a replacement is in flight the field takes no input, and whatever
// the outcome no token text stays in the dialog.
func TestTokenDialogFieldLockedWhilePending(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87), setTokenErr: &api.Error{Status: 400, Msg: "new token rejected: 401 Bad credentials"}}
	m := click(t, onSettings(t, c, 140, 60), setReplaceToken)
	m = feed(m, keys("o", "l", "d")...)
	upd, cmd := m.pressed(tokOK) // the request is in flight until cmd runs
	m = upd.(Model)
	m.tok.group.Focus(tokField)
	m = feed(m, keys("n", "e", "w")...)
	if m.tok.field.Value().Text != "" || m.tok.field.Focusable() {
		t.Fatalf("field editable while pending: %q", m.tok.field.Value().Text)
	}
	m = feed(m, cmd())
	if m.overlay != ovToken || m.tok.field.Value().Text != "" || !m.tok.field.Focusable() || m.tok.busy {
		t.Fatalf("after rejection: overlay %v field %q busy %v", m.overlay, m.tok.field.Value().Text, m.tok != nil && m.tok.busy)
	}
}

// The dialog follows the connection: Replace is disabled and refuses while
// the daemon is unreachable, but a degraded daemon still accepts a token.
func TestTokenDialogFollowsConnection(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87)}
	m := click(t, onSettings(t, c, 140, 60), setReplaceToken)
	m = feed(m, keys("n", "e", "w")...)
	m.connected = false
	if m.View(); !m.tok.ok.Disabled {
		t.Fatal("Replace enabled while unreachable")
	}
	upd, cmd := m.pressed(tokOK)
	m = upd.(Model)
	if cmd != nil || len(c.actions()) != 0 || !strings.Contains(m.View(), "the daemon is unreachable") {
		t.Fatalf("replace sent while unreachable: %v", c.actions())
	}
	m.connected = true
	st := sampleStatus()
	st.Degraded, st.DegradedReason = true, "GitHub rejected the token"
	m = feed(m, statusMsg{st: st})
	if m.View(); m.tok.ok.Disabled {
		t.Fatal("Replace disabled while degraded")
	}
}
```

In `internal/tui/settings_test.go`, `TestSettingsFocusOrderScrollsIntoView`: change the wrap expectation from `setCPUQuota` to `setReplaceToken` and the view check from `"› [ 200%"` to `"› [ Replace token ]"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/tui -run 'Token'`
Expected: build failure, `tokenMsg undefined`.

- [ ] **Step 3: Write the implementation**

`internal/tui/manage.go`:

```go
package tui

import (
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/tui/ui"
)

const (
	setReplaceToken = "settings/token"
	setTokenRetry   = "settings/token/retry"
	tokField        = "token/field"
	tokOK           = "token/ok"
	tokCancel       = "token/cancel"
)

// manageState holds the management cards' daemon data and buttons. The
// Model keeps it by pointer so it survives Bubble Tea copying the Model.
type manageState struct {
	token    model.TokenStatus
	tokenErr string
	tokenSeq int
	replace  *ui.Button
	tokRetry *ui.Button
}

func newManageState() *manageState {
	return &manageState{
		replace:  ui.NewButton(setReplaceToken, "Replace token", ui.Primary),
		tokRetry: ui.NewButton(setTokenRetry, "Retry", ui.Secondary),
	}
}

type (
	tokenMsg struct {
		seq int
		ts  model.TokenStatus
		err error
	}
	// tokenSetMsg carries the dialog that sent it, so a late reply never
	// changes a dialog opened after it.
	tokenSetMsg struct {
		d   *tokenDialog
		err error
	}
)

func (m Model) fetchToken() tea.Cmd {
	m.mg.tokenSeq++
	seq, c := m.mg.tokenSeq, m.c
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		ts, err := c.Token(cx)
		return tokenMsg{seq, ts, err}
	}
}

func (m Model) gotToken(msg tokenMsg) {
	if msg.seq != m.mg.tokenSeq {
		return
	}
	if msg.err != nil {
		m.mg.tokenErr = errText(msg.err)
		return
	}
	m.mg.token, m.mg.tokenErr = msg.ts, ""
}

// errText is a daemon error as a card shows it, with the time a throttled
// call may be retried.
func errText(err error) string {
	s := clean(err.Error())
	var ae *api.Error
	if errors.As(err, &ae) && !ae.RetryAt.IsZero() {
		s += ", try again after " + ae.RetryAt.Local().Format("15:04")
	}
	return s
}

// tokenSection is the Settings card for the GitHub token. It never shows the token.
func (m Model) tokenSection() ui.Section {
	ts := m.mg.token
	state := ts.State
	if state == "" {
		state = "unverified"
	}
	badge := state
	exp := "expiry unknown"
	if ts.ExpiresAt != nil {
		left := ts.ExpiresAt.Sub(m.now())
		exp = fmt.Sprintf("expires %s (%d days)", ts.ExpiresAt.Local().Format("2006-01-02"), int(left.Hours()/24))
		if state == "ok" && left < 14*24*time.Hour {
			badge = "expires soon"
		}
	}
	status := stateBadge(badge) + "  " + exp
	if ts.CheckedAt != nil {
		status += sDim.Render("  · checked " + ago(m.now().Sub(*ts.CheckedAt)))
	}
	rate := "–"
	if ts.RateRemaining != nil {
		rate = fmt.Sprint(*ts.RateRemaining)
		if ts.RateLimit != nil {
			rate += fmt.Sprintf(" / %d", *ts.RateLimit)
		}
		if ts.RateReset != nil {
			rate += ", resets " + ts.RateReset.Local().Format("15:04")
		}
	}
	rows := []ui.Row{{Label: "Status", Text: status}, {Label: "Rate limit", Text: rate}}
	if ts.Reason != "" {
		rows = append(rows, ui.Row{Text: sRed.Render(clean(ts.Reason))})
	}
	if m.mg.tokenErr != "" {
		m.mg.tokRetry.SetDisabled(!m.connected)
		rows = append(rows, ui.Row{Text: sRed.Render("✖ " + m.mg.tokenErr)}, ui.Row{Items: []ui.Widget{m.mg.tokRetry}})
	}
	m.mg.replace.SetDisabled(!m.connected)
	rows = append(rows, ui.Row{Items: []ui.Widget{m.mg.replace}})
	return ui.Section{Title: "GitHub token",
		Note: "Replacing checks read access to the first repository only; registration permissions are checked when a runner is next started.",
		Rows: rows}
}

// tokenDialog is the Replace token dialog.
type tokenDialog struct {
	field      *ui.TextField
	ok, cancel *ui.Button
	group      ui.Group
	err        string
	busy       bool
}

func (m Model) openTokenDialog() (tea.Model, tea.Cmd) {
	d := &tokenDialog{
		field:  ui.NewTextField(tokField, 40),
		ok:     ui.NewButton(tokOK, "Replace", ui.Primary),
		cancel: ui.NewButton(tokCancel, "Cancel", ui.Secondary),
	}
	d.field.Mask = true
	d.group.Set([]ui.Widget{d.field, d.cancel, d.ok})
	m.tok, m.overlay = d, ovToken
	return m, nil
}

// syncToken sets the dialog's controls from its state and the connection:
// the field is locked while a replacement is in flight, and Replace needs a
// reachable daemon. A degraded daemon still takes a token; that is how a
// rejected one is fixed. It runs before every key, click and frame.
func (m Model) syncToken() {
	d := m.tok
	d.field.SetDisabled(d.busy)
	d.ok.SetDisabled(d.busy || !m.connected)
	d.group.Set([]ui.Widget{d.field, d.cancel, d.ok})
}

// tokenKey routes a key in the dialog: the focused control first (enter on
// a button presses it), then esc cancels, tab moves focus, enter replaces.
func (m Model) tokenKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.tok
	m.syncToken()
	if _, onButton := d.group.Focused().(*ui.Button); onButton && k.String() == "enter" {
		return m.pressed(d.group.FocusedID())
	}
	if ok, cmd := d.group.Key(k); ok {
		return m, cmd
	}
	switch k.String() {
	case "esc":
		return m.pressed(tokCancel)
	case "tab", "down":
		d.group.Next()
	case "shift+tab", "up":
		d.group.Prev()
	case "enter":
		return m.pressed(tokOK)
	}
	return m, nil
}

// submitToken sends the token and clears the field at once; only the
// request holds the value until it returns.
func (m Model) submitToken() (tea.Model, tea.Cmd) {
	d := m.tok
	if d == nil || d.busy || m.offline() {
		return m, nil
	}
	tok := d.field.Value().Text
	d.field.SetValue(ui.Value{})
	if tok == "" {
		d.err = "paste the new token first"
		return m, nil
	}
	d.busy, d.err = true, ""
	m.syncToken()
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return tokenSetMsg{d, c.SetToken(cx, tok)}
	}
}

func (m Model) tokenSet(msg tokenSetMsg) (tea.Model, tea.Cmd) {
	mine := m.overlay == ovToken && m.tok == msg.d
	if !mine {
		return m, nil
	}
	m.tok.field.SetValue(ui.Value{})
	if msg.err != nil {
		m.tok.busy, m.tok.err = false, clean(msg.err.Error())
		m.syncToken()
		return m, nil
	}
	m.overlay, m.tok = ovNone, nil
	m.toast.Show("GitHub token replaced", false, m.now())
	return m, m.fetchToken()
}

func (m Model) tokenView(w int) string {
	d := m.tok
	m.syncToken()
	f := d.group.FocusedID()
	body := "Paste a fine-grained token with Administration: read/write\nand Actions: read on every configured repository.\n\n" +
		d.field.View(f == tokField, 0)
	if d.err != "" {
		body += "\n" + lipgloss.NewStyle().Width(max(min(60, w-14), 20)).Render(sRed.Render("✖ "+d.err))
	}
	return modal("Replace GitHub token", body, []*ui.Button{d.cancel, d.ok}, f, w)
}
```

`internal/tui/model.go`:
- Add `ovToken` to the overlay constants, after `ovAddRepo`.
- Add to `Model`: `mg *manageState` (after `repos`) and `tok *tokenDialog // the open Replace token dialog` (after `add`); add `mg: newManageState(),` to `New`.
- In `Update`, add cases:

```go
	case tokenMsg:
		m.gotToken(msg)
		return m, nil
	case tokenSetMsg:
		return m.tokenSet(msg)
```

- In the `tickMsg` case, add after the history refresh: `if m.page == pageSettings && m.frame%slowPoll == 0 { cmds = append(cmds, m.fetchToken()) }`.

`internal/tui/input.go`:
- In `handleKey`'s overlay switch add `case ovToken: return m.tokenKey(k)`.
- In `handleMouse`, after the `ovAddRepo` block add:

```go
	if m.overlay == ovToken {
		m.syncToken()
		_, cmd := m.tok.group.Mouse(msg)
		return m, cmd
	}
```

- In `switchPage`, change `case pageSettings: return m, m.fetchConfig()` to `return m, tea.Batch(m.fetchConfig(), m.fetchToken())`.

`internal/tui/dialogs.go`:
- In `buttonOverlay`, add `case tokOK, tokCancel: return ovToken`.
- In `pressed`, add before `default:`:

```go
	case setReplaceToken:
		return m.openTokenDialog()
	case setTokenRetry:
		return m, m.fetchToken()
	case tokOK:
		return m.submitToken()
	case tokCancel:
		if m.tok != nil {
			m.tok.field.SetValue(ui.Value{})
		}
		m.overlay, m.tok = ovNone, nil
```

- In `withOverlay`, add `case ovToken: dialog = m.tokenView(w)`.

`internal/tui/settings.go`, in `settingsSections`, append the token card after the "Runner defaults" section: `secs = append(secs, m.tokenSection())` (before the widget loop that builds the focus order, so the Replace button joins it).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui -run 'Golden' -update && git diff --stat -- internal/tui/testdata && timeout 300 go test ./internal/tui/...`
Expected: the Settings goldens gain the GitHub token card; then `ok`. Read the golden diff and confirm no token value appears in it.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "feat(tui): show and replace the GitHub token"
```

### Task 22: Maintenance section

**Files:**
- Modify: `internal/tui/manage.go` (buttons, `maintenanceSection`, `reloadMsg`, `reloadConfig`), `internal/tui/settings.go` (append the section), `internal/tui/dialogs.go` (`pressed`), `internal/tui/model.go` (`reloadMsg` case)
- Test: `internal/tui/manage_test.go`, `internal/tui/settings_test.go` (focus-order wrap)

**Interfaces:**
- Consumes: Task 20's `Client.Reload`, `Client.Prune`; C2 `Status.Maintenance`; `manageState` (Task 21).
- Produces: button IDs `setReload = "settings/reload"`, `setPrune = "settings/prune"`.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests** (append to `internal/tui/manage_test.go`; its imports stay as Task 21 left them)

```go
func TestMaintenanceSection(t *testing.T) {
	c := &fakeClient{token: tokenStatus(87), warnings: []string{"labels: duplicate"}}
	m := onSettings(t, c, 140, 70)
	v := m.View()
	for _, want := range []string{"Maintenance", "61% used", "not pruned since the daemon started", "[ Reload config.yaml ]", "[ Prune now ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	m = click(t, m, setReload)
	if !strings.Contains(m.View(), "config reloaded (1 warning, see Activity)") {
		t.Fatalf("reload toast:\n%s", m.View())
	}
	m = click(t, m, setPrune)
	if got := strings.Join(c.actions(), "|"); got != "reload|prune" || !strings.Contains(m.View(), "prune started — see Activity") {
		t.Fatalf("actions %q\n%s", got, m.View())
	}
	st := sampleStatus()
	st.Maintenance.Running = true
	m = feed(m, statusMsg{st: st})
	if v := m.View(); !strings.Contains(v, "pruning…") || m.mg.prune.Focusable() {
		t.Fatalf("running prune:\n%s", v)
	}
	done := now.Add(-2 * time.Hour)
	st.Maintenance = model.MaintenanceStatus{LastFinished: &done, LastOutcome: "ok"}
	if v := feed(m, statusMsg{st: st}).View(); !strings.Contains(v, "last pruned 2h ago (ok)") {
		t.Fatalf("finished prune:\n%s", v)
	}
	c.reloadErr = &api.Error{Status: 400, Msg: "owner changed from darkraise to x: restart ghr to switch owners"}
	if v := click(t, m, setReload).View(); !strings.Contains(v, "reload rejected: owner changed") {
		t.Fatalf("rejected reload:\n%s", v)
	}
}
```

In `internal/tui/settings_test.go`, `TestSettingsFocusOrderScrollsIntoView`: change the wrap expectation from `setReplaceToken` to `setPrune` and the view check from `"› [ Replace token ]"` to `"› [ Prune now ]"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/tui -run 'Maintenance|FocusOrder'`
Expected: build failure, `setReload undefined`.

- [ ] **Step 3: Write the implementation**

`internal/tui/manage.go`: add the IDs `setReload = "settings/reload"` and `setPrune = "settings/prune"` to the const block; add fields `reload, prune *ui.Button` to `manageState` and create them in `newManageState`:

```go
		reload:  ui.NewButton(setReload, "Reload config.yaml", ui.Primary),
		prune:   ui.NewButton(setPrune, "Prune now", ui.Primary),
```

Add (with `"context"` imported):

```go
type reloadMsg struct {
	warnings []string
	err      error
}

func (m Model) reloadConfig() (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	c := m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		ws, err := c.Reload(cx)
		return reloadMsg{ws, err}
	}
}

func (m Model) reloaded(msg reloadMsg) (tea.Model, tea.Cmd) {
	switch n := len(msg.warnings); {
	case msg.err != nil:
		m.toast.Show("reload rejected: "+clean(msg.err.Error()), true, m.now())
		return m, nil
	case n == 0:
		m.toast.Show("config reloaded", false, m.now())
	case n == 1:
		m.toast.Show("config reloaded (1 warning, see Activity)", false, m.now())
	default:
		m.toast.Show(fmt.Sprintf("config reloaded (%d warnings, see Activity)", n), false, m.now())
	}
	return m, tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig(), m.fetchToken())
}

func (m Model) startPrune() (tea.Model, tea.Cmd) {
	if m.offline() {
		return m, nil
	}
	return m, m.action("prune started — see Activity", func(c context.Context) error { return m.c.Prune(c) })
}

// maintenanceSection is the Settings card for disk use, the last manual
// prune, Reload and Prune now.
func (m Model) maintenanceSection() ui.Section {
	ms := m.st.Maintenance
	last := "not pruned since the daemon started"
	switch {
	case ms.Running:
		last = "pruning…"
	case ms.LastFinished != nil:
		last = fmt.Sprintf("last pruned %s (%s)", ago(m.now().Sub(*ms.LastFinished)), ms.LastOutcome)
	}
	m.mg.reload.SetDisabled(!m.connected)
	m.mg.prune.SetDisabled(!m.connected || ms.Running)
	return ui.Section{Title: "Maintenance", Rows: []ui.Row{
		{Label: "Disk", Text: fmt.Sprintf("%d%% used", m.st.DiskPct)},
		{Label: "Prune", Text: last},
		{Items: []ui.Widget{m.mg.reload, m.mg.prune}},
	}}
}
```

`internal/tui/settings.go`, in `settingsSections`: append `m.maintenanceSection()` right after `m.tokenSection()`.

`internal/tui/dialogs.go`, in `pressed`, add before `default:`:

```go
	case setReload:
		return m.reloadConfig()
	case setPrune:
		return m.startPrune()
```

`internal/tui/model.go`, in `Update`: add `case reloadMsg: return m.reloaded(msg)`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui -run 'Golden' -update && git diff --stat -- internal/tui/testdata && timeout 300 go test ./internal/tui/...`
Expected: Settings goldens gain the Maintenance card; then `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): reload config and prune from Settings"
```

### Task 23: Registrations card

**Files:**
- Modify: `internal/tui/manage.go` (registration state, fetch, card, delete), `internal/tui/repos.go` (`repoSelected`; the card in the panel; its buttons in the focus order; selection changes fetch), `internal/tui/input.go` (`switchPage`), `internal/tui/dialogs.go` (`pressed`), `internal/tui/model.go` (`regsMsg`/`regDeletedMsg` cases)
- Test: `internal/tui/manage_test.go`

**Interfaces:**
- Consumes: Task 20's `Client.Registrations`, `Client.DeleteRegistration`; Task 16's panel (`reposPanelLines`, `syncRepoControls`); `stateBadge`.
- Produces: `func (m Model) repoSelected() tea.Cmd` (fetches the selected repo's cards; Tasks 24 and 26 add to it), `reposRegRefresh`, `regDelID`.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test** (append to `internal/tui/manage_test.go`)

```go
func TestRegistrationsCard(t *testing.T) {
	c := &fakeClient{regs: []model.Registration{
		{ID: 1, Name: "linux-1", Status: "offline", Labels: []string{"self-hosted", "X64"}},
		{ID: 2, Name: "ghr-darkcloud-aaaaaa", Status: "online", Busy: true, GHR: true},
		{ID: 3, Name: "laptop", Status: "online"},
	}}
	m := onRepos(t, c, 140, 80)
	v := m.View()
	for _, want := range []string{"GitHub registrations", "[OFFLINE]", "linux-1", "[BUSY] [GHR]", "ghr-darkcloud-aaaaaa", "[ONLINE]", "laptop", "( Refresh )"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if n := strings.Count(v, "[ Delete ]"); n != 1 {
		t.Fatalf("%d Delete buttons; only the offline foreign runner may have one", n)
	}
	m = click(t, m, regDelID(1))
	if m.overlay != ovConfirm || !strings.Contains(m.confirmText, "linux-1 from darkcloud") {
		t.Fatalf("confirm: %v %q", m.overlay, m.confirmText)
	}
	m = feed(m, key("enter")) // Yes
	if got := strings.Join(c.actions(), "|"); got != "del-reg darkcloud 1" || !strings.Contains(m.View(), "deleted linux-1") {
		t.Fatalf("actions %q", got)
	}
	m = feed(m, regsMsg{repo: "darkcloud", seq: m.mg.regSeq - 1})
	if !strings.Contains(m.View(), "linux-1") {
		t.Fatal("a stale listing replaced the current one")
	}
	c.regs = nil
	if v := click(t, m, reposRegRefresh).View(); !strings.Contains(v, "No runners registered") {
		t.Fatalf("empty:\n%s", v)
	}
	st := sampleStatus()
	st.Degraded, st.DegradedReason = true, "GitHub rejected the token"
	m = feed(m, statusMsg{st: st})
	if v := m.View(); !strings.Contains(v, "GitHub is rejecting the token") || m.mg.regRefresh.Focusable() {
		t.Fatalf("degraded:\n%s", v)
	}
}

// When a status refresh removes the selected repo, the selection falls back
// to its neighbour and the cards reload for it; the old rows never show
// under the new heading, and a late reply for the old repo is dropped.
func TestRegistrationsFollowSelectionChanges(t *testing.T) {
	c := &fakeClient{regsByRepo: map[string][]model.Registration{
		"darkcloud": {{ID: 1, Name: "linux-1", Status: "offline"}},
		"darkmem":   {{ID: 5, Name: "mem-box", Status: "online"}},
	}}
	m := onRepos(t, c, 140, 80)
	if v := m.View(); !strings.Contains(v, "linux-1") {
		t.Fatalf("darkcloud rows missing:\n%s", v)
	}
	st := sampleStatus()
	st.Repos = st.Repos[1:] // darkcloud removed: darkmem takes its place
	m = feed(m, statusMsg{st: st})
	v := m.View()
	if strings.Contains(v, "linux-1") || !strings.Contains(v, "mem-box") {
		t.Fatalf("cards did not follow the selection:\n%s", v)
	}
	// Later tasks add other card reads to the same batch; look at the
	// registration reads only.
	regReads := func() []string {
		var out []string
		for _, r := range c.reads {
			if strings.HasPrefix(r, "regs ") {
				out = append(out, r)
			}
		}
		return out
	}
	if got := strings.Join(regReads(), "|"); !strings.Contains(got, "regs darkcloud") || !strings.HasSuffix(got, "regs darkmem") {
		t.Fatalf("reads %q", got)
	}
	m = feed(m, regsMsg{repo: "darkcloud", seq: m.mg.regSeq, regs: []model.Registration{{ID: 9, Name: "stale"}}})
	if strings.Contains(m.View(), "stale") {
		t.Fatal("a reply for another repo was shown")
	}
	m = feed(m, key("down"))
	if rs := regReads(); rs[len(rs)-1] != "regs darkagents" {
		t.Fatalf("down did not fetch the new selection: %q", rs)
	}
}

// Every panel control has a line range, so focus on a short screen scrolls
// to the summary buttons and the registration buttons too.
func TestRegistrationButtonsScrollIntoView(t *testing.T) {
	c := &fakeClient{regs: []model.Registration{{ID: 1, Name: "linux-1", Status: "offline"}}}
	m := onRepos(t, c, 120, 16)
	for _, id := range []string{reposRegRefresh, regDelID(1), reposPause, reposRemove, reposRegRefresh} {
		m.repos.group.Focus(id)
		focusInView(t, m)
	}
}

// A throttled or failed listing shows when it may be retried.
func TestRegistrationsErrorShowsRetry(t *testing.T) {
	retry := now.Add(2 * time.Minute)
	c := &fakeClient{regErr: &api.Error{Status: 429, Msg: "GitHub rate limit", RetryAt: retry}}
	m := onRepos(t, c, 140, 80)
	if v := m.View(); !strings.Contains(v, "GitHub rate limit") || !strings.Contains(v, "try again after "+retry.Local().Format("15:04")) {
		t.Fatalf("retry time missing:\n%s", v)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 200 go test ./internal/tui -run 'TestRegistrations'`
Expected: build failure, `regDelID undefined`.

- [ ] **Step 3: Write the implementation**

`internal/tui/manage.go`: add `"strconv"` and `"strings"` to the imports (`errText`, from Task 21, already shows a throttled error's retry time), the IDs below to the const block, the fields to `manageState`, and their initialisation in `newManageState` (`regRefresh: ui.NewButton(reposRegRefresh, "Refresh", ui.Secondary), regDel: map[int64]*ui.Button{},`):

```go
	reposRegRefresh = "repos/reg/refresh"
```

```go
	cardsRepo  string // the repo the panel's cards were last fetched for
	regRepo    string // the repo regs belong to
	regs       []model.Registration
	regErr     string
	regSeq     int
	regLoaded  bool
	regRefresh *ui.Button
	regDel     map[int64]*ui.Button
```

Then:

```go
func regDelID(id int64) string { return fmt.Sprintf("repos/reg/del/%d", id) }

type (
	regsMsg struct {
		repo string
		seq  int
		regs []model.Registration
		err  error
	}
	regDeletedMsg struct {
		repo, name string
		err        error
	}
)

// deletable reports whether the daemon would delete g: offline, idle, and
// outside ghr's namespace.
func deletable(g model.Registration) bool { return !g.GHR && !g.Busy && g.Status == "offline" }

// fetchRegs lists the selected repo's registrations. A reply for an older
// request or another repo is dropped.
func (m Model) fetchRegs() tea.Cmd {
	r := m.selectedRepoStatus()
	if r == nil || m.st.Degraded {
		return nil
	}
	m.mg.regSeq++
	seq, repo, c := m.mg.regSeq, r.Name, m.c
	if !strings.EqualFold(m.mg.regRepo, repo) {
		m.mg.regRepo, m.mg.regs, m.mg.regErr, m.mg.regLoaded = repo, nil, "", false
	}
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		rs, err := c.Registrations(cx, repo)
		return regsMsg{repo, seq, rs, err}
	}
}

func (m Model) gotRegs(msg regsMsg) {
	r := m.selectedRepoStatus()
	if msg.seq != m.mg.regSeq || !strings.EqualFold(msg.repo, m.mg.regRepo) || r == nil || !strings.EqualFold(msg.repo, r.Name) {
		return
	}
	m.mg.regLoaded = true
	if msg.err != nil {
		m.mg.regErr = errText(msg.err)
		return
	}
	m.mg.regs, m.mg.regErr = msg.regs, ""
}

// regWidgets are the card's focusable buttons, in order.
func (m Model) regWidgets() []ui.Widget {
	off := !m.connected || m.st.Degraded
	m.mg.regRefresh.SetDisabled(off)
	ws := []ui.Widget{m.mg.regRefresh}
	for _, g := range m.mg.regs {
		if !deletable(g) {
			continue
		}
		b := m.mg.regDel[g.ID]
		if b == nil {
			b = ui.NewButton(regDelID(g.ID), "Delete", ui.Danger)
			m.mg.regDel[g.ID] = b
		}
		b.SetDisabled(off)
		ws = append(ws, b)
	}
	return ws
}

// regCard renders the GitHub registrations card w columns wide, with the
// line range of each of its buttons (counted from the card's top border).
func (m Model) regCard(w int) ([]string, map[string]ui.Range) {
	f := m.repos.group.FocusedID()
	ranges := map[string]ui.Range{}
	var lines []string
	mark := func(id string) { ranges[id] = ui.Range{Start: len(lines) + 1, End: len(lines) + 2} }
	switch {
	case m.st.Degraded:
		lines = append(lines, sRed.Render("GitHub is rejecting the token: "+clean(m.st.DegradedReason)))
	case m.mg.regErr != "":
		lines = append(lines, sRed.Render("✖ "+m.mg.regErr))
	case !m.mg.regLoaded:
		lines = append(lines, sDim.Render("loading…"))
	case len(m.mg.regs) == 0:
		lines = append(lines, sDim.Render("No runners registered. ghr starts single-use runners on demand (and keeps warm ones in all mode)."))
	}
	if !m.st.Degraded {
		for _, g := range m.mg.regs {
			state := g.Status
			if g.Busy {
				state = "busy"
			}
			line := stateBadge(state)
			if g.GHR {
				line += " " + ui.Badge("ghr", ui.BadgeMuted)
			}
			if b := m.mg.regDel[g.ID]; b != nil && deletable(g) {
				line += " " + b.View(f == b.ID(), 0)
				mark(b.ID())
			}
			line += " " + clean(g.Name) + "  " + sDim.Render(clean(strings.Join(g.Labels, " ")))
			lines = append(lines, line)
		}
	}
	mark(reposRegRefresh)
	lines = append(lines, m.mg.regRefresh.View(f == reposRegRefresh, 0))
	return strings.Split(box("GitHub registrations", w, lines), "\n"), ranges
}

// confirmDeleteReg asks before deleting the registration a Delete button
// names, capturing the repo and runner now.
func (m Model) confirmDeleteReg(id string) (tea.Model, tea.Cmd) {
	n, err := strconv.ParseInt(strings.TrimPrefix(id, "repos/reg/del/"), 10, 64)
	if err != nil || m.offline() {
		return m, nil
	}
	name := ""
	for _, g := range m.mg.regs {
		if g.ID == n {
			name = g.Name
		}
	}
	repo, c := m.mg.regRepo, m.c
	return m.openConfirm(fmt.Sprintf("Delete the runner registration %s from %s?", clean(name), clean(repo)), func() tea.Cmd {
		return func() tea.Msg {
			cx, cancel := ctx()
			defer cancel()
			return regDeletedMsg{repo, name, c.DeleteRegistration(cx, repo, n)}
		}
	})
}

func (m Model) regDeleted(msg regDeletedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.toast.Show("delete failed: "+clean(msg.err.Error()), true, m.now())
	} else {
		m.toast.Show("deleted "+clean(msg.name), false, m.now())
	}
	return m, m.fetchRegs() // a new request number drops any listing sent before the delete
}
```

`internal/tui/repos.go`:
- Add:

```go
// repoSelected fetches the selected repo's management cards.
func (m Model) repoSelected() tea.Cmd { return m.fetchRegs() }

// refreshRepoCards refetches the management cards when the selection no
// longer names the repo they were loaded for, and returns nil otherwise.
// Every path that can change the selection calls it: list keys and clicks,
// a status refresh that removed the selected repo, and entering the page.
func (m Model) refreshRepoCards() tea.Cmd {
	r := m.selectedRepoStatus()
	if r == nil || strings.EqualFold(r.Name, m.mg.cardsRepo) {
		return nil
	}
	m.mg.cardsRepo = r.Name
	return m.repoSelected()
}
```

- In `syncRepoControls`, after the loop that appends the repo inputs, append `items = append(items, m.regWidgets()...)`.
- Replace `reposPanelLines` (every control in the panel now has a line range, the summary's buttons and the cards' buttons included):

```go
// reposPanelLines renders the panel w columns wide with the line range of
// every control, for scrolling the focused one into view.
func (m Model) reposPanelLines(r model.RepoStatus, w int) ([]string, map[string]ui.Range) {
	out := strings.Split(box(clean(r.Name), w, m.repoSummary(r)), "\n")
	// The summary box's last inside line holds Pause and Remove.
	act := ui.Range{Start: len(out) - 2, End: len(out) - 1}
	ranges := map[string]ui.Range{reposPause: act, reposRemove: act}
	add := func(lines []string, rs map[string]ui.Range) {
		for k, rg := range rs {
			ranges[k] = ui.Range{Start: rg.Start + len(out), End: rg.End + len(out)}
		}
		out = append(out, lines...)
	}
	if m.cfg == nil || m.repoInputs(r.Name) == nil {
		add([]string{sDim.Render("loading…")}, nil)
	} else {
		add(ui.Render(m.repoSections(r, w), m.repos.group.FocusedID(), w, w >= 70))
	}
	add(m.regCard(w))
	return out, ranges
}
```

- In `reposHandleKey`, make the `up/k/down/j` case return the fetch: replace `m.moveRepo(d)` with `m.moveRepo(d)` followed by `return true, m, m.refreshRepoCards()`.
- In `reposMouse`, return `m.refreshRepoCards()` as the command where a click or the wheel over the list changes the selection.

`internal/tui/input.go`, in `switchPage`, replace Task 16's `case pageRepos: return m, m.fetchConfig()` with (entering the page always reloads the cards):

```go
	case pageRepos:
		m.mg.cardsRepo = ""
		return m, tea.Batch(m.fetchConfig(), m.refreshRepoCards())
```

`internal/tui/model.go`, in `Update`'s `statusMsg` case, immediately after `var cmds []tea.Cmd`, add:

```go
		if m.page == pageRepos {
			cmds = append(cmds, m.refreshRepoCards())
		}
```

`internal/tui/dialogs.go`, in `pressed`: add `case reposRegRefresh: return m, m.fetchRegs()` before `default:`, and inside `default:` add:

```go
		if strings.HasPrefix(id, "repos/reg/del/") {
			return m.confirmDeleteReg(id)
		}
```

`internal/tui/model.go`, in `Update`, add:

```go
	case regsMsg:
		m.gotRegs(msg)
		return m, nil
	case regDeletedMsg:
		return m.regDeleted(msg)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui -run TestRepositoriesGolden -update && git diff -- internal/tui/testdata/TestRepositoriesGolden && timeout 300 go test ./internal/tui/...`
Expected: the two `TestRepositoriesGolden` files change only by the GitHub registrations card appearing in the panel (whatever of it fits in 40 rows); read the diff, and treat any other change as a bug. Then `ok`.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "feat(tui): list and delete runner registrations"
```

### Task 24: Label check card

**Files:**
- Modify: `internal/tui/manage.go` (label-check state, fetch, start, classification, card, quick-add), `internal/tui/repos.go` (card placement, focus order, `repoSelected`), `internal/tui/dialogs.go` (`pressed`), `internal/tui/model.go` (`lcMsg`/`lcStartedMsg` cases; tick polling while checking)
- Test: `internal/tui/manage_test.go`

**Interfaces:**
- Consumes: Task 20's `Client.StartLabelCheck`, `Client.LabelCheck`; `reposKey`, `repoSelected` (Tasks 16, 23); `sched.MatchLabels`; `config.Config.EffectiveLabels`.
- Produces: `reposLCCheck = "repos/lc/check"`, `lcAddID(label string) string`.

**Items:** 2, 4

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test** (append to `internal/tui/manage_test.go`; add `"reflect"` to its imports if missing)

```go
func TestLabelCheckCard(t *testing.T) {
	seen := now.Add(-2 * time.Hour)
	at := now.Add(-3 * time.Minute)
	c := &fakeClient{labels: model.LabelCheck{State: "done", CheckedAt: &at, Groups: []model.LabelGroup{
		{Labels: []string{"darkcloud-linux", "self-hosted"}, Jobs: []string{"CI / test"}, Count: 4, LastSeen: seen},
		{Labels: []string{"gpu", "self-hosted"}, Jobs: []string{"ML / train"}, Count: 2, LastSeen: seen},
		{Labels: []string{"self-hosted", "windows"}, Jobs: []string{"Win / build"}, Count: 1, LastSeen: seen},
		{Labels: []string{"ubuntu-latest"}, Jobs: []string{"CI / lint"}, More: 2, Count: 5, LastSeen: seen},
		{Labels: []string{}, Jobs: []string{"Group / job"}, Count: 1, LastSeen: seen},
	}}}
	m := onRepos(t, c, 160, 120)
	v := m.View()
	for _, want := range []string{"Workflow labels", "Checked 3m ago", "[MATCHED]", "[UNMATCHED]", "missing gpu", "( + add gpu )",
		"needs a different OS or architecture", "[OTHER]", "+2 more", "no labels (runner group)", "does not install anything"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(v, "( + add windows )") {
		t.Fatal("OS and architecture labels are never offered")
	}
	m = click(t, m, lcAddID("gpu"))
	if got := repoInput(m, "darkcloud", "labels").Value().List; !reflect.DeepEqual(got, []string{"darkcloud-linux", "gpu"}) {
		t.Fatalf("quick add: %v", got)
	}
	if v := m.View(); strings.Count(v, "[MATCHED]") != 2 || !strings.Contains(v, "1 unsaved change (darkcloud)") {
		t.Fatalf("after quick add:\n%s", v)
	}
	m = click(t, m, reposLCCheck)
	if got := strings.Join(c.actions(), "|"); got != "label-check darkcloud" {
		t.Fatalf("actions %q", got)
	}
	retry := now.Add(time.Minute)
	c.labelErr = &api.Error{Status: 429, Msg: "this repo was checked less than a minute ago", RetryAt: retry}
	if v := click(t, m, reposLCCheck).View(); !strings.Contains(v, "checked less than a minute ago") || !strings.Contains(v, "try again after "+retry.Local().Format("15:04")) {
		t.Fatalf("throttled:\n%s", v)
	}
	c.labelErr, c.labelGetErr = nil, &api.Error{Status: 429, Msg: "GitHub rate limit", RetryAt: retry}
	m = feed(m, lcMsg{repo: "darkcloud", seq: m.mg.lcSeq, err: c.labelGetErr})
	if v := m.View(); !strings.Contains(v, "GitHub rate limit, try again after "+retry.Local().Format("15:04")) {
		t.Fatalf("fetch error:\n%s", v)
	}
}

func unmatchedGPU() model.LabelCheck {
	at := now.Add(-time.Minute)
	return model.LabelCheck{State: "done", CheckedAt: &at, Groups: []model.LabelGroup{
		{Labels: []string{"gpu", "self-hosted"}, Jobs: []string{"ML / train"}, Count: 2, LastSeen: at},
	}}
}

// Label-check data can arrive before the config; the card then lists the
// groups without classifying them, and classifies once the config loads.
func TestLabelCheckBeforeConfig(t *testing.T) {
	m := sampleModel(&fakeClient{}, 140, 80)
	m.page = pageRepos
	m.mg.lcRepo, m.mg.lcSeq = "darkcloud", 1
	m = feed(m, lcMsg{repo: "darkcloud", seq: 1, lc: unmatchedGPU()})
	if v := m.View(); !strings.Contains(v, "gpu, self-hosted") || strings.Contains(v, "[UNMATCHED]") {
		t.Fatalf("before config:\n%s", v)
	}
	m = feed(m, configMsg{seq: m.order.seq + 1, cfg: parseConfig(t, settingsYAML)})
	if v := m.View(); !strings.Contains(v, "[UNMATCHED]") || !strings.Contains(v, "+ add gpu") {
		t.Fatalf("after config:\n%s", v)
	}
}

// While a scan runs, at most one poll is in flight; a slow reply is still
// accepted rather than overtaken by the next tick's request.
func TestLabelCheckPollsOneAtATime(t *testing.T) {
	c := &fakeClient{labels: model.LabelCheck{State: "checking", Groups: []model.LabelGroup{}}}
	m := onRepos(t, c, 140, 80)
	first := m.pollLabelCheck()
	if first == nil {
		t.Fatal("no poll while checking")
	}
	if m.pollLabelCheck() != nil {
		t.Fatal("a second poll started while one is in flight")
	}
	c.labels = unmatchedGPU()
	m = feed(m, first())
	if m.mg.lc.State != "done" || m.mg.lcInFlight {
		t.Fatalf("slow reply dropped: state %q in flight %v", m.mg.lc.State, m.mg.lcInFlight)
	}
	// A reply carrying the latest number but another repo's data is dropped.
	m = feed(m, lcMsg{repo: "darkmem", seq: m.mg.lcSeq, lc: model.LabelCheck{State: "checking"}})
	if m.mg.lc.State != "done" {
		t.Fatalf("another repo's reply was applied: %q", m.mg.lc.State)
	}
}

// Check now, Delete, Refresh and the quick-add buttons are unavailable while
// GitHub rejects the token or the repo is being removed, and their actions
// refuse then too.
func TestManagementControlsFollowAvailability(t *testing.T) {
	c := &fakeClient{labels: unmatchedGPU(), regs: []model.Registration{{ID: 1, Name: "linux-1", Status: "offline"}}}
	m := onRepos(t, c, 140, 80)
	m.View()
	if m.mg.lcAdd["gpu"] == nil || m.mg.lcAdd["gpu"].Disabled {
		t.Fatal("quick add unavailable while everything is fine")
	}
	for _, mut := range []func(*model.Status){
		func(st *model.Status) { st.Degraded, st.DegradedReason = true, "GitHub rejected the token" },
		func(st *model.Status) { st.Repos[0].Removing = true },
	} {
		st := sampleStatus()
		mut(&st)
		mm := feed(m, statusMsg{st: st})
		mm.View()
		for _, b := range []*ui.Button{mm.mg.lcCheck, mm.mg.lcAdd["gpu"], mm.mg.regRefresh, mm.mg.regDel[1]} {
			if b != nil && !b.Disabled {
				t.Errorf("%s enabled (degraded %v)", b.ID(), st.Degraded)
			}
		}
		upd, _ := mm.pressed(lcAddID("gpu"))
		if got := repoInput(upd.(Model), "darkcloud", "labels").Value().List; len(got) != 1 {
			t.Errorf("quick add applied anyway: %v", got)
		}
		upd, cmd := mm.pressed(reposLCCheck)
		if cmd != nil || len(c.actions()) != 0 {
			t.Errorf("check started anyway: %v", c.actions())
		}
		_ = upd
	}
}

// GitHub label text never reaches the screen raw, and long label lists wrap
// so every quick-add button and the note stay whole on every width.
func TestLabelCheckSanitisesAndWraps(t *testing.T) {
	at := now.Add(-time.Minute)
	labels := []string{"self-hosted", "gpu\x1b]0;x\a", "cuda-12-runtime", "big-memory-box", "nvme-scratch-disk"}
	c := &fakeClient{labels: model.LabelCheck{State: "done", CheckedAt: &at, Groups: []model.LabelGroup{
		{Labels: labels, Jobs: []string{"ML / train-a-very-long-job-name"}, Count: 2, LastSeen: at},
	}}}
	for _, w := range []int{100, 99, 56, 40} {
		m := onRepos(t, c, w, 200)
		pw, _ := m.reposPanelSize(m.contentSize())
		r := m.selectedRepoStatus()
		lines, _ := m.reposPanelLines(*r, pw)
		v := zone.Scan(strings.Join(lines, "\n"))
		if strings.ContainsAny(v, "\x1b\a") {
			t.Fatalf("%d columns: raw control bytes: %q", w, v)
		}
		for _, want := range []string{"+ add gpu ", "+ add cuda-12-runtime", "+ add big-memory-box", "+ add nvme-scratch-disk", "on the runner."} {
			if !strings.Contains(v, want) {
				t.Errorf("%d columns: missing %q:\n%s", w, want, v)
			}
		}
		fits(t, "label check", m.View(), w, 200)
	}
	m := onRepos(t, c, 140, 80)
	m = click(t, m, lcAddID("gpu\x1b]0;x\a"))
	if got := repoInput(m, "darkcloud", "labels").Value().List; !reflect.DeepEqual(got, []string{"darkcloud-linux", "gpu\x1b]0;x\a"}) {
		t.Fatalf("the raw label is what gets added: %q", got)
	}
}
```

Add `zone "github.com/lrstanley/bubblezone"` and `"github.com/darkraise/ghr/internal/tui/ui"` to the imports of `manage_test.go` if missing. (`clean`, `internal/tui/view.go:71`, runs `ansi.Strip` first, so the whole OSC sequence goes and `gpu\x1b]0;x\a` shows as `gpu`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 200 go test ./internal/tui -run 'TestLabelCheck|TestManagementControlsFollowAvailability'`
Expected: build failure, `lcAddID undefined`.

- [ ] **Step 3: Write the implementation**

`internal/tui/manage.go`: add imports `"encoding/hex"`, `"github.com/charmbracelet/x/ansi"`, `"github.com/darkraise/ghr/internal/config"` and `"github.com/darkraise/ghr/internal/sched"`; add `reposLCCheck = "repos/lc/check"` to the const block; add to `manageState`:

```go
	lcRepo     string
	lc         model.LabelCheck
	lcErr      string
	lcSeq      int
	lcInFlight bool // a label-check read is outstanding; polls wait for it
	lcCheck    *ui.Button
	lcAdd      map[string]*ui.Button // by raw label
```

initialised in `newManageState` with `lcCheck: ui.NewButton(reposLCCheck, "Check now", ui.Secondary), lcAdd: map[string]*ui.Button{},`. Then:

```go
// lcAddID is a quick-add button's ID. Labels come from GitHub and may hold
// any byte, so the ID carries the label hex-encoded; lcAddLabel reverses it.
func lcAddID(label string) string { return "repos/lc/add/" + hex.EncodeToString([]byte(label)) }

func lcAddLabel(id string) (string, bool) {
	h, ok := strings.CutPrefix(id, "repos/lc/add/")
	if !ok {
		return "", false
	}
	b, err := hex.DecodeString(h)
	return string(b), err == nil
}

// repoActionable reports whether the selected repo's GitHub management
// controls may act: the daemon is reachable, GitHub accepts the token and
// the repo is not being removed.
func (m Model) repoActionable() bool {
	r := m.selectedRepoStatus()
	return r != nil && m.connected && !m.st.Degraded && !r.Removing
}

// osArchLabels name a machine's platform; adding one to a Linux x64 runner
// would only misroute jobs, so the quick fix never offers them.
var osArchLabels = map[string]bool{"linux": true, "windows": true, "macos": true, "x64": true, "x86": true, "arm": true, "arm64": true}

type (
	lcMsg struct {
		repo string
		seq  int
		lc   model.LabelCheck
		err  error
	}
	lcStartedMsg struct {
		repo string
		err  error
	}
)

// fetchLabelCheck reads the selected repo's last label scan; it never starts one.
func (m Model) fetchLabelCheck() tea.Cmd {
	r := m.selectedRepoStatus()
	if r == nil || m.st.Degraded {
		return nil
	}
	m.mg.lcSeq++
	m.mg.lcInFlight = true
	seq, repo, c := m.mg.lcSeq, r.Name, m.c
	if !strings.EqualFold(m.mg.lcRepo, repo) {
		m.mg.lcRepo, m.mg.lc, m.mg.lcErr = repo, model.LabelCheck{State: "not_checked"}, ""
	}
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		lc, err := c.LabelCheck(cx, repo)
		return lcMsg{repo, seq, lc, err}
	}
}

// pollLabelCheck re-reads a running scan on the Repositories page, one
// request at a time: a reply slower than the tick is waited for, not
// overtaken by a newer request whose number would drop it.
func (m Model) pollLabelCheck() tea.Cmd {
	if m.page != pageRepos || m.mg.lc.State != "checking" || m.mg.lcInFlight {
		return nil
	}
	return m.fetchLabelCheck()
}

func (m Model) gotLabelCheck(msg lcMsg) {
	if msg.seq != m.mg.lcSeq {
		return
	}
	m.mg.lcInFlight = false
	r := m.selectedRepoStatus()
	if !strings.EqualFold(msg.repo, m.mg.lcRepo) || r == nil || !strings.EqualFold(msg.repo, r.Name) {
		return
	}
	if msg.err != nil {
		m.mg.lcErr = errText(msg.err)
		return
	}
	m.mg.lc, m.mg.lcErr = msg.lc, ""
}

func (m Model) startLabelCheck() (tea.Model, tea.Cmd) {
	r := m.selectedRepoStatus()
	if r == nil || m.offline() || !m.repoActionable() {
		return m, nil
	}
	repo, c := r.Name, m.c
	return m, func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		return lcStartedMsg{repo, c.StartLabelCheck(cx, repo)}
	}
}

func (m Model) labelCheckStarted(msg lcStartedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.toast.Show("label check: "+errText(msg.err), true, m.now())
		return m, nil
	}
	if strings.EqualFold(msg.repo, m.mg.lcRepo) {
		m.mg.lc.State = "checking"
	}
	return m, m.fetchLabelCheck()
}

// effectiveLabels are the labels a newly started runner of repo name would
// carry with the draft repo labels; nil until the config has loaded.
func (m Model) effectiveLabels(name string) []string {
	if m.cfg == nil {
		return nil
	}
	r := m.cfg.Repo(name)
	if r == nil {
		return config.SystemLabels
	}
	draft := *r
	if f := m.repos.form.Field(reposKey(name, "labels")); f != nil {
		draft.Labels = f.Input.Value().List
	}
	return m.cfg.EffectiveLabels(draft)
}

// classify compares a job label group with the draft effective labels using
// the daemon's own predicate: matched (ghr takes these jobs), unmatched (they
// ask for self-hosted, with the labels missing) or other. Before the config
// has loaded there is nothing to compare with, and it reports "".
func (m Model) classify(name string, g model.LabelGroup) (kind string, missing []string) {
	if m.cfg == nil {
		return "", nil
	}
	if len(g.Labels) == 0 {
		return "other", nil
	}
	eff := m.effectiveLabels(name)
	if sched.MatchLabels(g.Labels, eff) {
		return "matched", nil
	}
	has := map[string]bool{}
	for _, l := range eff {
		has[strings.ToLower(l)] = true
	}
	self := false
	for _, l := range g.Labels {
		if l == "self-hosted" {
			self = true
		}
		if !has[l] {
			missing = append(missing, l)
		}
	}
	if !self {
		return "other", nil
	}
	return "unmatched", missing
}

// lcWidgets are the card's focusable buttons, in order.
func (m Model) lcWidgets(name string) []ui.Widget {
	off := !m.repoActionable()
	m.mg.lcCheck.SetDisabled(off || m.mg.lc.State == "checking")
	ws := []ui.Widget{m.mg.lcCheck}
	for _, l := range m.quickAdds(name) {
		b := m.mg.lcAdd[l]
		if b == nil {
			b = ui.NewButton(lcAddID(l), "+ add "+clean(l), ui.Secondary)
			m.mg.lcAdd[l] = b
		}
		b.SetDisabled(off)
		ws = append(ws, b)
	}
	return ws
}

// quickAdds are the custom labels the unmatched groups miss, in order.
func (m Model) quickAdds(name string) []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range m.mg.lc.Groups {
		if kind, missing := m.classify(name, g); kind == "unmatched" {
			for _, l := range missing {
				if !osArchLabels[l] && !seen[l] {
					seen[l] = true
					out = append(out, l)
				}
			}
		}
	}
	return out
}

// lcCard renders the Workflow labels card w columns wide, with the line
// range of each of its buttons. Text wraps to the card and every button has
// a line of its own, so nothing is cut on a narrow panel. Label and job
// names come from GitHub and pass through clean; the raw labels stay in the
// model for matching and adding.
func (m Model) lcCard(name string, w int) ([]string, map[string]ui.Range) {
	f := m.repos.group.FocusedID()
	lc := m.mg.lc
	inner := max(w-4, 1)
	ranges := map[string]ui.Range{}
	var lines []string
	plain := func(s ...string) string { return strings.Join(s, "") }
	text := func(s string, style func(...string) string) {
		for _, l := range strings.Split(ansi.Wrap(s, inner, ""), "\n") {
			lines = append(lines, style(l))
		}
	}
	shown := map[string]bool{}
	button := func(b *ui.Button, indent string) {
		if shown[b.ID()] {
			return
		}
		shown[b.ID()] = true
		ranges[b.ID()] = ui.Range{Start: len(lines) + 1, End: len(lines) + 2}
		lines = append(lines, indent+b.View(f == b.ID(), 0))
	}
	head := "Not checked yet"
	switch {
	case lc.State == "checking":
		head = "checking…"
	case lc.CheckedAt != nil:
		head = "Checked " + ago(m.now().Sub(*lc.CheckedAt))
		if lc.Partial {
			head += " · partial"
		}
	}
	text(head, sDim.Render)
	button(m.mg.lcCheck, "")
	if m.st.Degraded {
		text("GitHub is rejecting the token: "+clean(m.st.DegradedReason), sRed.Render)
	}
	if lc.Error != "" {
		text("✖ "+clean(lc.Error), sRed.Render)
	}
	if m.mg.lcErr != "" {
		text("✖ "+m.mg.lcErr, sRed.Render)
	}
	offered := false
	for _, g := range lc.Groups {
		kind, missing := m.classify(name, g)
		var shownLabels []string
		for _, l := range g.Labels {
			shownLabels = append(shownLabels, clean(l))
		}
		labels := strings.Join(shownLabels, ", ")
		if len(g.Labels) == 0 {
			labels = "no labels (runner group)"
		}
		jobs := clean(strings.Join(g.Jobs, ", "))
		if g.More > 0 {
			jobs += fmt.Sprintf(" +%d more", g.More)
		}
		badge := ""
		if kind != "" {
			badge = stateBadge(kind) + " "
		}
		text(badge+labels+"  "+jobs+sDim.Render(fmt.Sprintf(" · %d jobs · %s", g.Count, ago(m.now().Sub(g.LastSeen)))), plain)
		if kind != "unmatched" {
			continue
		}
		var shownMissing []string
		platform := false
		for _, l := range missing {
			shownMissing = append(shownMissing, clean(l))
			platform = platform || osArchLabels[l]
		}
		text("  missing "+strings.Join(shownMissing, ", "), plain)
		for _, l := range missing {
			if b := m.mg.lcAdd[l]; b != nil && !osArchLabels[l] {
				button(b, "    ")
				offered = true
			}
		}
		if platform {
			text("  needs a different OS or architecture", sAmber.Render)
		}
	}
	if offered {
		text("Adding a label changes which jobs ghr accepts; it does not install anything on the runner.", sDim.Render)
	}
	return strings.Split(box("Workflow labels", w, lines), "\n"), ranges
}

// addLabel puts label into the selected repo's labels as an unsaved edit,
// only while the repo's controls may act.
func (m Model) addLabel(label string) {
	if !m.repoActionable() {
		return
	}
	f := m.repos.form.Field(reposKey(m.mg.lcRepo, "labels"))
	if f == nil || !f.Input.Focusable() {
		return
	}
	v := f.Input.Value()
	for _, l := range v.List {
		if strings.EqualFold(l, label) {
			return
		}
	}
	v.List = append(append([]string{}, v.List...), label)
	f.Input.SetValue(v)
}
```

`internal/tui/repos.go`:
- `repoSelected` becomes `return tea.Batch(m.fetchRegs(), m.fetchLabelCheck())`.
- In `syncRepoControls`, insert `items = append(items, m.lcWidgets(r.Name)...)` immediately before `items = append(items, m.regWidgets()...)`.
- In `reposPanelLines`, insert `add(m.lcCard(r.Name, w))` immediately before `add(m.regCard(w))`.

`internal/tui/manage.go`: the registration controls follow the same rule. In `regWidgets`, change `off := !m.connected || m.st.Degraded` to `off := !m.repoActionable()`; in `confirmDeleteReg`, change `if err != nil || m.offline() {` to `if err != nil || m.offline() || !m.repoActionable() {`.

`internal/tui/dialogs.go`, in `pressed`: add `case reposLCCheck: return m.startLabelCheck()` before `default:`, and inside `default:`:

```go
		if label, ok := lcAddLabel(id); ok {
			if !m.offline() {
				m.addLabel(label)
			}
			return m, nil
		}
```

`internal/tui/model.go`:
- In `Update`, add:

```go
	case lcMsg:
		m.gotLabelCheck(msg)
		return m, nil
	case lcStartedMsg:
		return m.labelCheckStarted(msg)
```

- In the `tickMsg` case, add: `cmds = append(cmds, m.pollLabelCheck())` (it returns nil off the Repositories page, when no scan runs, and while a read is outstanding).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui -run TestRepositoriesGolden -update && git diff -- internal/tui/testdata/TestRepositoriesGolden && timeout 300 go test ./internal/tui/...`
Expected: the two `TestRepositoriesGolden` files change only by the Workflow labels card appearing in the panel (whatever of it fits in 40 rows); read the diff, and treat any other change as a bug. Then `ok`.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "feat(tui): check workflow labels per repo"
```

### Task 25: Dashboard metric tiles

**Files:**
- Modify: `internal/tui/manage.go` (metrics state and fetch), `internal/tui/dashboard.go` (`statTiles`, `series`, `seriesMax`, `fmtMem`; `tilesH` 3 → 4), `internal/tui/model.go` (`Init`, tick refresh, `metricsMsg` case)
- Test: `internal/tui/dashboard_test.go` (add `"errors"` to its imports if missing), goldens

**Interfaces:**
- Consumes: Task 20's `Client.Metrics`; C7 `Sparkline`, `Gauge` (Task 11); C2 `Metrics`.
- Produces: `func series(samples []model.MetricSample, pick func(model.MetricSample) *float64) []*float64` (Task 26 does not need it; it is local to the Dashboard).

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The tiles become Running, Queued jobs, CPU, Memory and Disk (the Repositories tile goes; its counts are on the Repositories page). Each tile keeps one content line: the value, then a sparkline or gauge in the room left, so the tile height and the short-screen collapse rules do not change.

- [ ] **Step 1: Replace the tile test** (`internal/tui/dashboard_test.go`; add imports `"time"` and `"github.com/darkraise/ghr/internal/model"`)

Replace `TestStatTiles` with:

```go
func sampleMetrics() model.Metrics {
	f := func(v float64) *float64 { return &v }
	cpu, used, total := 12.0, int64(180<<20), int64(8<<30)
	return model.Metrics{CPU: &cpu, MemUsed: &used, MemTotal: &total, DiskPct: 61, Samples: []model.MetricSample{
		{At: now.Add(-2 * time.Minute), Live: 1, Queued: 3, CPU: f(10)},
		{At: now.Add(-time.Minute), Live: 2, Queued: 1, CPU: f(30)},
	}}
}

func TestStatTiles(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	m.mg.metrics = sampleMetrics()
	v := m.View()
	for _, want := range []string{"╭─ Running ", "2 / 3", "▃▆", "╭─ Queued jobs ", "╭─ CPU ", "12%", "╭─ Memory ", "180M / 8.0G", "╭─ Disk ", "61%"} {
		if !strings.Contains(v, want) {
			t.Errorf("tiles missing %q", want)
		}
	}
	got := m.statTiles(100, true)
	for _, want := range []string{"Queued jobs 3", "CPU 12%", "Memory 180M / 8.0G"} {
		if !strings.Contains(got, want) {
			t.Errorf("compact tiles %q missing %q", got, want)
		}
	}
	m.st.Mode = config.ModeAll
	if got := m.statTiles(100, true); !strings.Contains(got, "Running 2 / ∞") {
		t.Errorf("all mode: %q", got)
	}
	m.mg.metrics = model.Metrics{}
	if v := m.View(); !strings.Contains(v, "╭─ CPU ") || !strings.Contains(v, "–") {
		t.Fatalf("no metrics yet:\n%s", v)
	}
}

// Every tile keeps its graphics at 100 and 120 columns: a value line, then
// a chart line; CPU has both a gauge and a sparkline.
func TestStatTilesKeepGraphics(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	m.mg.metrics = sampleMetrics()
	for _, w := range []int{84, 104} { // the content width at 100 and 120 columns
		lines := strings.Split(m.statTiles(w, false), "\n")
		if len(lines) != 4 {
			t.Fatalf("%d: tiles are %d lines, want 4", w, len(lines))
		}
		if n := strings.Count(lines[1], "▕") + strings.Count(lines[2], "▕"); n < 3 {
			t.Errorf("%d: %d gauges, want CPU, Memory and Disk:\n%s", w, n, strings.Join(lines, "\n"))
		}
		if !strings.ContainsAny(lines[2], "▁▂▃▄▅▆▇█") || !strings.Contains(lines[1], "12%") || !strings.Contains(lines[1], "180M / 8.0G") {
			t.Errorf("%d: tiles:\n%s", w, strings.Join(lines, "\n"))
		}
	}
}

// A failed metrics read shows in the CPU and Memory tiles and clears on the
// next good poll; a stale reply changes nothing.
func TestStatTilesMetricsError(t *testing.T) {
	m := sampleModel(&fakeClient{}, 120, 40)
	m.mg.metricsSeq = 3
	m = feed(m, metricsMsg{seq: 3, mt: sampleMetrics()})
	m = feed(m, metricsMsg{seq: 2, err: errors.New("stale")})
	if v := m.View(); strings.Contains(v, "stale") || !strings.Contains(v, "12%") {
		t.Fatalf("stale reply applied:\n%s", v)
	}
	m.mg.metricsSeq = 4
	m = feed(m, metricsMsg{seq: 4, err: errors.New("daemon busy")})
	if v := m.View(); !strings.Contains(v, "✖ daemon busy") || strings.Contains(v, "12%") {
		t.Fatalf("error not shown:\n%s", v)
	}
	if got := m.statTiles(100, true); !strings.Contains(got, "metrics ✖ daemon busy") {
		t.Fatalf("compact: %q", got)
	}
	m.mg.metricsSeq = 5
	m = feed(m, metricsMsg{seq: 5, mt: sampleMetrics()})
	if v := m.View(); strings.Contains(v, "daemon busy") || !strings.Contains(v, "12%") {
		t.Fatalf("not recovered:\n%s", v)
	}
}

func TestSeriesMarksGaps(t *testing.T) {
	s := []model.MetricSample{{At: now, Live: 1}, {At: now.Add(time.Minute), Live: 2}, {At: now.Add(4 * time.Minute), Live: 3}}
	got := series(s, func(x model.MetricSample) *float64 { v := float64(x.Live); return &v })
	if len(got) != 4 || got[2] != nil || *got[3] != 3 {
		t.Fatalf("series %v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 200 go test ./internal/tui -run 'StatTiles|SeriesMarksGaps'`
Expected: build failure, `m.mg.metrics undefined`.

- [ ] **Step 3: Write the implementation**

`internal/tui/manage.go`: add to `manageState` the fields `metrics model.Metrics`, `metricsErr string` and `metricsSeq int`, and:

```go
type metricsMsg struct {
	seq int
	mt  model.Metrics
	err error
}

func (m Model) fetchMetrics() tea.Cmd {
	m.mg.metricsSeq++
	seq, c := m.mg.metricsSeq, m.c
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		mt, err := c.Metrics(cx)
		return metricsMsg{seq, mt, err}
	}
}
```

`internal/tui/model.go`: `Init` returns `tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig(), m.fetchMetrics(), tick())`; in the `tickMsg` case, inside the existing `if m.frame%slowPoll == 0 || …` config block's neighbourhood add `if m.frame%slowPoll == 0 { cmds = append(cmds, m.fetchMetrics()) }`; in `Update` add:

```go
	case metricsMsg:
		// The tiles have no controls; a failed read shows in them and the
		// next slowPoll fetch is the retry.
		if msg.seq == m.mg.metricsSeq {
			if msg.err != nil {
				m.mg.metricsErr = errText(msg.err)
			} else {
				m.mg.metrics, m.mg.metricsErr = msg.mt, ""
			}
		}
		return m, nil
```

`internal/tui/dashboard.go`, in the Dashboard layout: change `tilesH := 3` to `tilesH := 4` (a tile is a border, a value line, a chart line and a border); the collapse test on the next line then uses the new height unchanged.

`internal/tui/dashboard.go`: add imports `"time"`, `"github.com/charmbracelet/x/ansi"` (if missing) and `"github.com/darkraise/ghr/internal/model"`; replace `statTiles` and add the helpers:

```go
// series turns samples into chart values; a gap of more than 90 seconds
// between samples (the daemon was down) draws a space.
func series(samples []model.MetricSample, pick func(model.MetricSample) *float64) []*float64 {
	var out []*float64
	for i, s := range samples {
		if i > 0 && s.At.Sub(samples[i-1].At) > 90*time.Second {
			out = append(out, nil)
		}
		out = append(out, pick(s))
	}
	return out
}

func seriesMax(vals []*float64) float64 {
	top := 0.0
	for _, v := range vals {
		if v != nil && *v > top {
			top = *v
		}
	}
	return top
}

func fmtMem(b int64) string {
	if b >= 1<<30 {
		return fmt.Sprintf("%.1fG", float64(b)/(1<<30))
	}
	return fmt.Sprintf("%dM", b>>20)
}

// statTiles renders the Dashboard's five stat tiles across w columns: boxed
// side by side with a chart after each value, or on one line when compact.
func (m Model) statTiles(w int, compact bool) string {
	running, queued := 0, 0
	for _, i := range m.st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	for _, r := range m.st.Repos {
		queued += r.Queued
	}
	mt := m.mg.metrics
	num := func(n int) *float64 { v := float64(n); return &v }
	live := series(mt.Samples, func(s model.MetricSample) *float64 { return num(s.Live) })
	queue := series(mt.Samples, func(s model.MetricSample) *float64 { return num(s.Queued) })
	cpus := series(mt.Samples, func(s model.MetricSample) *float64 { return s.CPU })
	limit, scale := fmt.Sprint(m.st.GlobalMax), float64(m.st.GlobalMax)
	if m.st.Mode == config.ModeAll {
		limit, scale = "∞", seriesMax(live)
	}
	cpu, mem := "–", "–"
	failed := m.mg.metricsErr != ""
	if mt.CPU != nil && !failed {
		cpu = fmt.Sprintf("%.0f%%", *mt.CPU)
	}
	if mt.MemUsed != nil && mt.MemTotal != nil && !failed {
		mem = fmtMem(*mt.MemUsed) + " / " + fmtMem(*mt.MemTotal)
	}
	diskStyle := sDim
	switch m.diskState() {
	case "warn":
		diskStyle = sAmber
	case "critical":
		diskStyle = sRed
	}
	gauge := func(used, total int, n int) string {
		if total <= 0 || n < 3 {
			return ""
		}
		return ui.Gauge(used, total, n-2)
	}
	errLine := func(n int) string { return sRed.Render(cell("✖ "+m.mg.metricsErr, n)) }
	// Each tile has a value line (value, then an inline chart in the room
	// left) and a chart line under it.
	tiles := []struct {
		title, value string
		inline       func(n int) string
		below        func(n int) string
	}{
		{"Running", fmt.Sprintf("%d / %s", running, limit), nil,
			func(n int) string { return sAccent.Render(ui.Sparkline(live, scale, n)) }},
		{"Queued jobs", fmt.Sprint(queued), nil,
			func(n int) string { return sAmber.Render(ui.Sparkline(queue, max(seriesMax(queue), 1), n)) }},
		{"CPU", cpu, func(n int) string {
			if mt.CPU == nil || failed {
				return ""
			}
			return gauge(int(*mt.CPU+0.5), 100, n)
		}, func(n int) string {
			if failed {
				return errLine(n)
			}
			return sAccent.Render(ui.Sparkline(cpus, 100, n))
		}},
		{"Memory", mem, nil, func(n int) string {
			if failed {
				return errLine(n)
			}
			if mt.MemUsed == nil || mt.MemTotal == nil {
				return ""
			}
			return gauge(int(*mt.MemUsed>>20), int(*mt.MemTotal>>20), n)
		}},
		{"Disk", diskStyle.Render(fmt.Sprintf("%d%%", m.st.DiskPct)), nil, func(n int) string {
			return diskStyle.Render(gauge(m.st.DiskPct, 100, n))
		}},
	}
	if compact {
		var parts []string
		for _, t := range tiles {
			parts = append(parts, sDim.Render(t.title)+" "+sBold.Render(t.value))
		}
		line := " " + strings.Join(parts, "   ")
		if failed {
			line += "   " + sRed.Render("metrics ✖ "+m.mg.metricsErr)
		}
		return cell(line, w)
	}
	tw := (w - 4) / 5 // four one-column gaps
	var boxes []string
	for i, t := range tiles {
		bw := tw
		if i == len(tiles)-1 {
			bw = w - 4*tw - 4 // the last tile takes the remainder
		}
		if i > 0 {
			boxes = append(boxes, " ")
		}
		line := sBold.Render(t.value)
		if room := bw - 4 - ansi.StringWidth(t.value) - 1; t.inline != nil && room >= 3 {
			line += " " + t.inline(room)
		}
		boxes = append(boxes, box(t.title, bw, []string{line, t.below(bw - 4)}))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
}
```

- [ ] **Step 4: Regenerate the goldens and run the tests**

Run: `timeout 300 go test ./internal/tui -run TestPagesGolden -update && git diff --stat -- internal/tui/testdata && timeout 300 go test ./internal/tui/...`
Expected: the dashboard goldens change in the tile row (Repositories tile replaced by CPU and Memory showing `–`, since the golden models have no metrics; each tile one line taller with its chart line) and the tables below it start one line lower; then `ok`, `TestStatTilesCollapse` included. If a short-screen test now collapses the tiles where it did not, that is the taller tiles: read the test and update its height expectation by one line.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "feat(tui): chart runners and host load on Dashboard"
```

### Task 26: Repositories Activity card

**Files:**
- Modify: `internal/tui/manage.go` (activity state, fetch, card, `resultStrip`), `internal/tui/repos.go` (`repoSelected`; card placement; Retry in the focus order), `internal/tui/model.go` (`actMsg` case; tick refresh), `internal/tui/dialogs.go` (`pressed`: Retry), `internal/tui/tui_test.go` (`fakeClient.histErr`)
- Test: `internal/tui/manage_test.go`

**Interfaces:**
- Consumes: the existing `Client.History`; `repoSelected` (Tasks 23, 24); `cleanEntry`, `dur`, `ago`.
- Produces: `resultStrip(hist []model.HistoryEntry, n int) string`.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test** (append to `internal/tui/manage_test.go`)

```go
func TestActivityCard(t *testing.T) {
	e := func(concl string, ago, d time.Duration) model.HistoryEntry {
		return model.HistoryEntry{Repo: "darkcloud", Conclusion: concl, FinishedAt: now.Add(-ago), StartedAt: now.Add(-ago - d)}
	}
	c := &fakeClient{hist: []model.HistoryEntry{ // newest first, as the daemon returns them
		e("success", time.Hour, 2*time.Minute), e("failure", 2*time.Hour, 4*time.Minute), e("cancelled", 3*time.Hour, time.Minute),
		e("unknown", 4*time.Hour, time.Minute), e("success", 9*24*time.Hour, time.Minute),
	}}
	m := onRepos(t, c, 160, 120)
	v := m.View()
	// The 9-day-old success is outside the window: neither counted nor drawn.
	for _, want := range []string{"Activity", "4 jobs · 33% success · avg 2m00s", "last 7 days", "?○✖■"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(v, "■?○✖■") {
		t.Error("the result strip draws a job outside the window")
	}
	if got := c.histReqs[len(c.histReqs)-1]; got != "darkcloud|" {
		t.Fatalf("history request %q", got)
	}
	m = feed(m, actMsg{repo: "darkcloud", seq: m.mg.actSeq - 1})
	if !strings.Contains(m.View(), "4 jobs") {
		t.Fatal("a stale reply replaced the card")
	}
	short := parseConfig(t, settingsYAML)
	short.HistoryRetention, _ = config.ParseDuration("3d")
	m = feed(m, configMsg{seq: m.order.seq + 1, cfg: short})
	if !strings.Contains(m.View(), "last 3d") {
		t.Fatal("the window follows a shorter retention")
	}
}

// Every conclusion GitHub reports counts toward the success rate; only a
// missing conclusion does not.
func TestActivityRateCountsEveryKnownConclusion(t *testing.T) {
	e := func(concl string, ago time.Duration) model.HistoryEntry {
		return model.HistoryEntry{Repo: "darkcloud", Conclusion: concl, FinishedAt: now.Add(-ago), StartedAt: now.Add(-ago - time.Minute)}
	}
	c := &fakeClient{hist: []model.HistoryEntry{
		e("success", time.Hour), e("neutral", 2*time.Hour), e("action_required", 3*time.Hour), e("unknown", 4*time.Hour), e("", 5*time.Hour),
	}}
	if v := onRepos(t, c, 160, 120).View(); !strings.Contains(v, "5 jobs · 33% success") {
		t.Fatalf("rate:\n%s", v)
	}
}

// A failed fetch stays visible in the card with a Retry button; retrying
// recovers the card alone.
func TestActivityErrorRetries(t *testing.T) {
	c := &fakeClient{histErr: errors.New("daemon busy")}
	m := onRepos(t, c, 160, 120)
	if v := m.View(); !strings.Contains(v, "✖ daemon busy") || !strings.Contains(v, "( Retry )") || !strings.Contains(v, "Workflow labels") {
		t.Fatalf("error:\n%s", v)
	}
	c.histErr = nil
	c.hist = []model.HistoryEntry{{Repo: "darkcloud", Conclusion: "success", FinishedAt: now.Add(-time.Hour), StartedAt: now.Add(-time.Hour - time.Minute)}}
	m = click(t, m, reposActRetry)
	if v := m.View(); !strings.Contains(v, "1 jobs · 100% success") || strings.Contains(v, "daemon busy") {
		t.Fatalf("after retry:\n%s", v)
	}
}
```

Add `"errors"` and `"github.com/darkraise/ghr/internal/config"` to the imports if missing. In `internal/tui/tui_test.go`, add a field `histErr error // returned by History when set` to `fakeClient`, and make `History` return `nil, f.histErr` first when it is set (after recording the request in `histReqs`).

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 200 go test ./internal/tui -run TestActivityCard`
Expected: build failure, `actMsg undefined`.

- [ ] **Step 3: Write the implementation**

`internal/tui/manage.go`: add `reposActRetry = "repos/act/retry"` to the const block; add to `manageState` the fields `actRepo string`, `act []model.HistoryEntry`, `actErr string`, `actSeq int`, `actLoaded bool`, `actRetry *ui.Button`, initialising `actRetry: ui.NewButton(reposActRetry, "Retry", ui.Secondary),` in `newManageState`; and:

```go
// actLimit bounds the Activity card's history request; the stats say "≥"
// when it is reached.
const actLimit = 500

type actMsg struct {
	repo string
	seq  int
	hist []model.HistoryEntry
	err  error
}

func (m Model) fetchActivity() tea.Cmd {
	r := m.selectedRepoStatus()
	if r == nil {
		return nil
	}
	m.mg.actSeq++
	seq, repo, c := m.mg.actSeq, r.Name, m.c
	if !strings.EqualFold(m.mg.actRepo, repo) {
		m.mg.actRepo, m.mg.act, m.mg.actErr, m.mg.actLoaded = repo, nil, "", false
	}
	return func() tea.Msg {
		cx, cancel := ctx()
		defer cancel()
		h, err := c.History(cx, repo, "", actLimit)
		return actMsg{repo, seq, h, err}
	}
}

func (m Model) gotActivity(msg actMsg) {
	r := m.selectedRepoStatus()
	if msg.seq != m.mg.actSeq || !strings.EqualFold(msg.repo, m.mg.actRepo) || r == nil || !strings.EqualFold(msg.repo, r.Name) {
		return
	}
	m.mg.actLoaded = true
	if msg.err != nil {
		m.mg.actErr = errText(msg.err)
		return
	}
	for i := range msg.hist {
		cleanEntry(&msg.hist[i])
	}
	m.mg.act, m.mg.actErr = msg.hist, ""
}

// resultStrip draws the newest n results oldest first; the symbols differ
// even without colour.
func resultStrip(hist []model.HistoryEntry, n int) string {
	if len(hist) > n {
		hist = hist[:n]
	}
	var b strings.Builder
	for i := len(hist) - 1; i >= 0; i-- {
		switch hist[i].Conclusion {
		case "success":
			b.WriteString(sGreen.Render("■"))
		case "failure":
			b.WriteString(sRed.Render("✖"))
		case "cancelled":
			b.WriteString(sDim.Render("○"))
		default:
			b.WriteString(sDim.Render("?"))
		}
	}
	if b.Len() == 0 {
		return sDim.Render("no finished jobs yet")
	}
	return b.String()
}

// activityCard renders the selected repo's recent jobs w columns wide: count,
// success rate over jobs with a known conclusion, average duration, and the
// last 20 results, all over one window: 7 days, or the retention when shorter.
func (m Model) activityCard(w int) ([]string, map[string]ui.Range) {
	window, label := 7*24*time.Hour, "last 7 days"
	if m.cfg != nil {
		if ret := m.cfg.HistoryRetention.D(); ret > 0 && ret < window {
			window, label = ret, "last "+m.cfg.HistoryRetention.String()
		}
	}
	ranges := map[string]ui.Range{}
	var lines []string
	switch {
	case m.mg.actErr != "":
		lines = append(lines, sRed.Render("✖ "+m.mg.actErr))
		ranges[reposActRetry] = ui.Range{Start: len(lines) + 1, End: len(lines) + 2}
		m.mg.actRetry.SetDisabled(!m.connected)
		lines = append(lines, m.mg.actRetry.View(m.repos.group.FocusedID() == reposActRetry, 0))
	case !m.mg.actLoaded:
		lines = append(lines, sDim.Render("loading…"))
	default:
		cut := m.now().Add(-window)
		var recent []model.HistoryEntry
		for _, e := range m.mg.act {
			if !e.FinishedAt.Before(cut) {
				recent = append(recent, e)
			}
		}
		jobs, ok, known := len(recent), 0, 0
		var total time.Duration
		for _, e := range recent {
			total += e.FinishedAt.Sub(e.StartedAt)
			// Every conclusion GitHub reports is known (success, failure,
			// cancelled, timed_out, neutral, action_required, skipped, …);
			// only a missing one is not.
			if e.Conclusion != "" && e.Conclusion != "unknown" {
				known++
				if e.Conclusion == "success" {
					ok++
				}
			}
		}
		rate, avg, more := "–", "–", ""
		if known > 0 {
			rate = fmt.Sprintf("%d%%", ok*100/known)
		}
		if jobs > 0 {
			avg = dur(total / time.Duration(jobs))
		}
		if len(m.mg.act) >= actLimit {
			more = "≥"
		}
		lines = append(lines, fmt.Sprintf("%s%d jobs · %s success · avg %s", more, jobs, rate, avg)+sDim.Render(" · "+label),
			resultStrip(recent, 20))
	}
	return strings.Split(box("Activity", w, lines), "\n"), ranges
}
```

`internal/tui/repos.go`:
- `repoSelected` becomes `return tea.Batch(m.fetchRegs(), m.fetchLabelCheck(), m.fetchActivity())`.
- In `reposPanelLines`, place the Activity card first among the cards: insert `add(m.activityCard(w))` immediately before `add(m.lcCard(r.Name, w))`.
- In `syncRepoControls`, insert immediately before `items = append(items, m.lcWidgets(r.Name)...)`:

```go
		if m.mg.actErr != "" {
			m.mg.actRetry.SetDisabled(!m.connected)
			items = append(items, m.mg.actRetry)
		}
```

`internal/tui/dialogs.go`, in `pressed`, add before `default:`: `case reposActRetry: return m, m.fetchActivity()`.

`internal/tui/model.go`:
- In `Update`: `case actMsg: m.gotActivity(msg); return m, nil`.
- In the `tickMsg` case: `if m.page == pageRepos && m.frame%slowPoll == 0 { cmds = append(cmds, m.fetchActivity()) }`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/tui -run TestRepositoriesGolden -update && git diff -- internal/tui/testdata/TestRepositoriesGolden && timeout 300 go test ./internal/tui/...`
Expected: the two `TestRepositoriesGolden` files change only by the Activity card appearing in the panel (whatever of it fits in 40 rows); read the diff, and treat any other change as a bug. Then `ok`.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "feat(tui): show recent activity per repo"
```

### Task 27: History bars and badges

**Files:**
- Modify: `internal/tui/history.go` (the table rows), `internal/tui/view.go` (`durBar`)
- Test: `internal/tui/history_test.go`, goldens

**Interfaces:**
- Consumes: `stateBadge`, `selectedRow`, `ui.BadgeRow` (Task 13).
- Produces: nothing other tasks use.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The fixed columns are `  FINISHED(16) REPO(12) RUN(7) ` (40 columns), the job column, then ` RESULT(11) DURATION(8)` (21 columns) and, when it fits, ` BAR`. While the job column would be under 14, columns give way in this order: the bar shrinks from 12 cells to 6, then goes, then REPO, then FINISHED, then DURATION. RUN, the job column and the result badge always stay; the job column takes what is left. At 120 and 80 columns this gives the same columns as before (80 drops only the bar), so those goldens change only in the badges and bars.

- [ ] **Step 1: Write the failing test** (append to `internal/tui/history_test.go`; add `"time"` and `"github.com/darkraise/ghr/internal/model"` to its imports if missing)

```go
func TestHistoryBarsAndBadges(t *testing.T) {
	h := func(concl string, d time.Duration) model.HistoryEntry {
		return model.HistoryEntry{Repo: "darkcloud", JobName: "e2e", RunNumber: "7", Conclusion: concl,
			StartedAt: now.Add(-time.Hour - d), FinishedAt: now.Add(-time.Hour)}
	}
	c := &fakeClient{hist: []model.HistoryEntry{h("success", 10*time.Minute), h("failure", 5*time.Minute), h("cancelled", 0)}}
	v := feed(sampleModel(c, 120, 30), key("4")).View()
	for _, want := range []string{"[SUCCESS]", "[FAILURE]", "[CANCELLED]", "████████████", "██████░░░░░░", "░░░░░░░░░░░░"} {
		if !strings.Contains(v, want) {
			t.Errorf("120 columns: missing %q", want)
		}
	}
	v = feed(sampleModel(c, 80, 30), key("4")).View()
	if !strings.Contains(v, "[FAILURE]") || strings.Contains(v, "░") {
		t.Fatalf("80 columns keep the badge and drop the bar:\n%s", v)
	}
	fits(t, "history", v, 80, 30)
	// Narrower, the leading columns give way; the badges of the selected
	// and the other rows, and the job names, stay.
	for _, w := range []int{56, 40} {
		v = feed(sampleModel(c, w, 30), key("4")).View()
		for _, want := range []string{"[SUCCESS]", "[FAILURE]", "[CANCELLED]", "e2e", "#7"} {
			if !strings.Contains(v, want) {
				t.Errorf("%d columns: missing %q:\n%s", w, want, v)
			}
		}
		fits(t, "history", v, w, 30)
	}
}
```

The result badge changes the selected row's text, so update `TestHistoryFilterBar` in the same file: change `"( Copy run URL ) success    2m00s"` to `"( Copy run URL ) [SUCCESS]   2m00s"` (the badge is 9 columns under the ASCII profile, padded to the 11-column RESULT field, then a space and the duration).

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 200 go test ./internal/tui -run TestHistoryBarsAndBadges`
Expected: FAIL, missing `[SUCCESS]`.

- [ ] **Step 3: Write the implementation**

`internal/tui/view.go`:

```go
// durBar draws d against the longest duration shown as a w-cell bar.
func durBar(d, longest time.Duration, w int) string {
	filled := 0
	if longest > 0 {
		filled = int(float64(w)*float64(d)/float64(longest) + 0.5)
	}
	filled = min(max(filled, 0), w)
	return strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
}
```

`internal/tui/history.go`: replace the block from `inner := w - 4` through the end of the row loop (the closing `}` of `for i := start; …`) with:

```go
	inner := w - 4
	showFinished, showRepo, showDur, barW := true, true, true, 12
	lead := func() int { // "  ", FINISHED, REPO, "RUN "
		n := 2 + 8
		if showFinished {
			n += 17
		}
		if showRepo {
			n += 13
		}
		return n
	}
	tail := func() int { // " RESULT", " DURATION", " BAR"
		n := 12
		if showDur {
			n += 9
		}
		if barW > 0 {
			n += barW + 1
		}
		return n
	}
	for _, giveWay := range []func(){
		func() { barW = 6 }, func() { barW = 0 }, func() { showRepo = false },
		func() { showFinished = false }, func() { showDur = false },
	} {
		if inner-lead()-tail() >= 14 {
			break
		}
		giveWay()
	}
	jobW := max(inner-lead()-tail(), 1)
	hdr := "  "
	if showFinished {
		hdr += fmt.Sprintf("%-16s ", "FINISHED")
	}
	if showRepo {
		hdr += fmt.Sprintf("%-12s ", "REPO")
	}
	hdr += fmt.Sprintf("%-7s %-*s %-11s", "RUN", jobW, "JOB", "RESULT")
	if showDur {
		hdr += " DURATION"
	}
	lines := []string{sDim.Render(hdr)}
	visible := max(h-len(bar)-3, 1)
	start := 0
	if m.histSel >= visible {
		start = m.histSel - visible + 1
	}
	var longest time.Duration
	for i := start; i < len(m.hist) && i < start+visible; i++ {
		longest = max(longest, m.hist[i].FinishedAt.Sub(m.hist[i].StartedAt))
	}
	for i := start; i < len(m.hist) && i < start+visible; i++ {
		e := m.hist[i]
		d := e.FinishedAt.Sub(e.StartedAt)
		line := "  "
		if showFinished {
			line += cell(e.FinishedAt.Local().Format("2006-01-02 15:04"), 16) + " "
		}
		if showRepo {
			line += cell(e.Repo, 12) + " "
		}
		line += cell("#"+e.RunNumber, 7) + " " + cell(e.JobName, jobW)
		badge := stateBadge(e.Conclusion)
		after := strings.Repeat(" ", max(11-ansi.StringWidth(badge), 0))
		if showDur {
			after += " " + cell(dur(d), 8)
		}
		if barW > 0 {
			after += " " + durBar(d, longest, barW)
		}
		id := fmt.Sprintf("hist-%d", i)
		if i == m.histSel {
			tailW := tail()
			lines = append(lines, m.selectedRow(id, line, "", "", rowButtons(ui.NewButton(rowCopy, "Copy run URL", ui.Secondary)), "", inner-tailW)+
				ui.BadgeRow(sSel, true, " ", badge, after, tailW))
		} else {
			lines = append(lines, zone.Mark(id, ui.BadgeRow(sSel, false, line+" ", badge, after, inner)))
		}
	}
```

Add `"time"` and `zone "github.com/lrstanley/bubblezone"` to `history.go`'s imports if missing.

- [ ] **Step 4: Regenerate the goldens and run the tests**

Run: `timeout 300 go test ./internal/tui -run TestPagesGolden -update && git diff --stat -- internal/tui/testdata && timeout 300 go test ./internal/tui/...`
Expected: the history goldens change in the RESULT column (badges) and gain bars at 120 columns; then `ok`, the narrow fit tests and `TestHistoryFilterBar` included.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/tui
git add internal/tui
git commit -m "feat(tui): add duration bars and result badges"
```

### Task 28: Help in four columns

**Files:**
- Modify: `internal/tui/dialogs.go` (`helpGroups`, `helpText`)
- Test: `internal/tui/dialogs_test.go`, goldens

**Interfaces:**
- Consumes: none.
- Produces: nothing other tasks use.

**Items:** 3, 5

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 0 - risk 1 = 1

No column may exceed ten rows (a title plus keys, a blank, a title plus keys), so the text stays at 14 lines and the dialog fits 22 rows. Four 23-column groups and three 2-column gaps need an inside of 98 columns: a screen of 104 or more. Narrower, the groups stack in one column and the dialog scrolls.

- [ ] **Step 1: Replace the fit test** (`internal/tui/dialogs_test.go`, replace `TestHelpGroupsFit`)

```go
// Help lists every group in four columns and fits a 22-row screen from 104
// columns; narrower, it stacks and scrolls.
func TestHelpGroupsFit(t *testing.T) {
	m := feed(sampleModel(&fakeClient{}, 120, 22), key("?"))
	v := m.View()
	for _, want := range []string{"Global", "Dashboard", "Repositories", "Runner rows", "Detail", "History", "Settings",
		"1-5", "add/remove/edit", "switch tab", "copy run URL", "takes digits", "set -g mouse on", "[ Close ]"} {
		if !strings.Contains(v, want) {
			t.Errorf("help missing %q", want)
		}
	}
	lines := strings.Split(ansi.Strip(v), "\n")
	for _, p := range [][2]string{{"Global", "Detail"}, {"Dashboard", "History"}, {"Repositories", "Settings"}} {
		top, below := lines[lineWith(lines, p[0])], lines[lineWith(lines, p[1])]
		if ansi.StringWidth(top[:strings.Index(top, p[0])]) != ansi.StringWidth(below[:strings.Index(below, p[1])]) {
			t.Errorf("%s is not under %s:\n%s", p[1], p[0], v)
		}
	}
	for _, w := range []int{104, 120} {
		v := feed(sampleModel(&fakeClient{}, w, 22), key("?")).View()
		if lipgloss.Height(v) > 22 || strings.Contains(v, "↑↓ scroll") {
			t.Errorf("width %d: help should fit 22 rows without scrolling:\n%s", w, v)
		}
		for i, line := range strings.Split(v, "\n") {
			if lipgloss.Width(line) > w {
				t.Fatalf("width %d: line %d is %d wide", w, i, lipgloss.Width(line))
			}
		}
	}
	if v := feed(sampleModel(&fakeClient{}, 103, 22), key("?")).View(); !strings.Contains(v, "↑↓ scroll") {
		t.Fatalf("at 103 columns help stacks and scrolls:\n%s", v)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 200 go test ./internal/tui -run TestHelpGroupsFit`
Expected: FAIL, missing `Repositories`.

- [ ] **Step 3: Write the implementation** (`internal/tui/dialogs.go`)

Replace `helpGroups` and its comment:

```go
// helpGroups in reading order. They render as four columns: Global over
// Detail, Dashboard over History, Repositories over Settings, then Runner
// rows. No column exceeds ten rows, so the dialog fits the 22-row minimum.
var helpGroups = []helpGroup{
	{"Global", [][2]string{{"1-5", "switch page"}, {"tab", "move focus"}, {"↑↓ j k", "move selection"}, {"? / q", "help / quit"}}},
	{"Dashboard", [][2]string{{"h / →", "repos / runners"}, {"p / P", "pause repo/all"}, {"+ - [ ]", "repo/global cap"}, {"m", "queue/all mode"}, {"a d e", "add/remove/edit"}}},
	{"Repositories", [][2]string{{"a / d", "add / remove"}, {"p", "pause / resume"}, {"ctrl+s", "save"}}},
	// The Runners page and the Dashboard's Runners card share these keys.
	{"Runner rows", [][2]string{{"enter", "open details"}, {"l / x", "log / stop"}, {"pgup/dn", "scroll the log"}}},
	{"Detail", [][2]string{{"← / →", "switch tab"}, {"x / esc", "stop / back"}, {"pgup/dn", "scroll the list"}}},
	{"History", [][2]string{{"r / c", "repo / result"}, {"enter", "copy run URL"}}},
	{"Settings", [][2]string{{"ctrl+s", "save"}, {"← / →", "choose / step"}, {"enter", "open / edit"}, {"esc", "close / stop"}}},
}
```

In `helpText`, replace the column layout (from `g := helpGroups` to the closing `}` of the `else` branch) with:

```go
	g := helpGroups
	var lines []string
	if inner < 4*helpColW+3*2 {
		lines = col(g...)
	} else {
		cols := [][]string{col(g[0], g[4]), col(g[1], g[5]), col(g[2], g[6]), col(g[3])}
		rows := 0
		for _, c := range cols {
			rows = max(rows, len(c))
		}
		for i := 0; i < rows; i++ {
			row := make([]string, len(cols))
			for c := range cols {
				if i < len(cols[c]) {
					row[c] = cols[c][i]
				}
				row[c] = cell(row[c], helpColW)
			}
			lines = append(lines, strings.TrimRight(strings.Join(row, "  "), " "))
		}
	}
```

Change the doc comment above `helpText` to "lists the keys by group in four columns, stacked into one when inner columns cannot hold four, followed by the notes on digits, the mouse and tmux."

- [ ] **Step 4: Regenerate the goldens and run the tests**

Run: `timeout 300 go test ./internal/tui -update -run 'Golden' && git diff --stat -- internal/tui/testdata && timeout 300 go test ./internal/tui/...`
Expected: `ok`, `TestHelpStacksWhenNarrow` and `TestHelpScrollsWhenShort` unchanged and passing. Read any golden diff.

- [ ] **Step 5: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): lay Help out in four columns"
```

### Task 29: Release and LXC acceptance

**Files:**
- Modify: `docs/superpowers/registers/2026-10-05-ghr-repositories-page.md` (homelab repo)

**Interfaces:**
- Consumes: every earlier task.
- Produces: the release and the register's final states.

**Items:** 1, 2, 3, 4, 5

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 0 - spec 1 - coupling 2 - risk 3 = 6

The controlling session runs this task itself, not a dispatched implementer: Step 3 needs your human partner's approval, and Step 1 is the execution skill's final review.

Every SSH command runs from Git Bash with `timeout` and `-o BatchMode=yes` against `root@192.168.0.99` with `-i ~/.ssh/ghr_lxc`. TUI checks run as scripts piped to `ssh … 'bash -s'`, each in its own tmux session named `accept-$$`, killed by an `EXIT` trap. Each script defines these helpers first (write the script file with the Write tool, not a heredoc; `\e` must reach the file as a backslash and `e`):

```bash
S=accept-$$
w() { timeout ${2:-15} bash -c "until tmux capture-pane -p -t $S | grep -qF -- \"$1\"; do sleep 0.3; done"; rc=$?; echo "found=$rc ($1)"; [ $rc -eq 0 ] || { tmux capture-pane -p -t $S; exit 1; }; }
click() { tmux send-keys -t $S -l "$(printf "\e[<0;%d;%dM\e[<0;%d;%dm" $1 $2 $1 $2)"; }
tmux new-session -d -s $S -x 120 -y 40 "NO_COLOR=1 ghr" || { echo "tmux could not create $S"; exit 1; }
trap "tmux kill-session -t $S" EXIT
w "[ACTIVE]" 30
```

`NO_COLOR=1` makes lipgloss pick the ASCII profile, so badges read `[TEXT]` as in the unit tests; the `w "[ACTIVE]"` line proves it at the start of every script (a Dashboard repo row carries that badge). A script that ends with `step=ok` passed every check. A key typed while a text field has focus goes into the field; leave such a field (tab or a click) before pressing a page key.

Write the scripts of Steps 5 to 7 as files `accept-a.sh`, `accept-b.sh`, `accept-c.sh`, `accept-d.sh` in the session scratchpad (each the helpers above, then the step's body), plus this wrapper `accept.sh`, which runs them in order and, on every exit, deletes the throwaway registration, restores `config.yaml` from the Step 2 backup and prints the checks Step 8 needs:

```bash
set -uo pipefail
L() { timeout "${T:-600}" ssh -i "$HOME/.ssh/ghr_lxc" -o BatchMode=yes root@192.168.0.99 "$@"; }
REG=""
finish() {
  rc=$?
  if [ -n "$REG" ]; then timeout 30 gh api -X DELETE "repos/darkraise/ghr-e2e/actions/runners/$REG" > /dev/null 2>&1; echo "fixture $REG deleted"; fi
  T=60 L 'cp /root/config.yaml.accept /etc/ghr/config.yaml && systemctl reload ghr && sleep 2 && sha256sum /etc/ghr/config.yaml; echo "accept sessions: $(tmux ls 2>/dev/null | grep -c accept-)"; ghr status | head -1'
  echo "acceptance exit=$rc"
}
trap finish EXIT
set -e
L 'bash -s' < accept-a.sh | tee a.log; grep -q step=ok a.log
L 'bash -s' < accept-b.sh | tee b.log; grep -q step=ok b.log
NAME=accept-stale-$RANDOM
REG=$(timeout 60 gh api -X POST repos/darkraise/ghr-e2e/actions/runners/generate-jitconfig -f name=$NAME -F runner_group_id=1 -f 'labels[]=self-hosted' -f work_folder=_work --jq .runner.id)
echo "name=$NAME id=$REG"
sed "s/NAME/$NAME/g" accept-c.sh | L 'bash -s' | tee c.log; grep -q step=ok c.log
[ "$(timeout 30 gh api repos/darkraise/ghr-e2e/actions/runners --jq '.runners[].name' | grep -c "$NAME")" = 0 ]
REG=""
timeout 30 gh workflow run single -R darkraise/ghr-e2e
for i in $(seq 60); do T=30 L 'ghr status | head -1' | grep -q 'global 1/' && break; sleep 5; done
L 'bash -s' < accept-d.sh | tee d.log; grep -q step=ok d.log
```

Run it from Git Bash in the scratchpad: `bash accept.sh 2>&1 | tee accept.log`.

- [ ] **Step 1: Final review before releasing**

Run the whole-branch final review exactly as the execution skill's Final Review section describes (`reference/final-review.md`) over `master..feat/repos-management`, apply its fix wave, and record its `Final review: clean …` line in the ledger. If it is not clean after one fix wave, stop and report the open findings.

- [ ] **Step 2: Pre-flight on the LXC**

```bash
timeout 60 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 'ghr version; ghr status | head -1; cp /etc/ghr/config.yaml /root/config.yaml.accept; sha256sum /etc/ghr/config.yaml; curl -sI -H "Authorization: Bearer $(cat /etc/ghr/token)" https://api.github.com/rate_limit | grep -i "^github-authentication-token-expiration:" || echo "no expiration header"'
```

Expected: `v0.1.6`, `global 0/` (no runner alive; otherwise wait), a checksum recorded as `CFG0`, and the raw `GitHub-Authentication-Token-Expiration` header line (only that header is printed; the token never leaves the LXC). Record the header's value format in the ledger: it settles the expiry-format assumption, and if it is neither `2006-01-02 15:04:05 MST` nor `2006-01-02 15:04:05 -0700` nor RFC 3339, stop and report, since `parseExpiry` would treat it as absent. The backup `/root/config.yaml.accept` is restored by the Step 5 wrapper.

- [ ] **Step 3: Ask for approval to release**

Ask your human partner, in one message: "The Repositories and management work passes all tests and its final review on `feat/repos-management` (commit `<sha>`). May I fast-forward `master`, push (CI publishes the next release), deploy it to the LXC with `GHR_VERSION` pinned, and run the acceptance there? It saves one label on ghr-e2e and restores config.yaml from a backup afterwards, runs one manual prune, creates and deletes one throwaway runner registration on ghr-e2e, and dispatches ghr-e2e's `single` workflow once." Wait for an explicit yes; without it, leave the register rows at `planned` and stop.

- [ ] **Step 4: Merge, push, wait for the release, deploy**

In `D:/Repositories/Personal/ghr`:

Run as one script (`bash release.sh`, written with the Write tool); it stops at the first failure and deploys only the release built from the approved commit:

```bash
set -euo pipefail
git switch master
git merge --ff-only feat/repos-management
SHA=$(git rev-parse HEAD); echo "sha=$SHA"
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
  TAG=$(git tag --points-at "$SHA" | grep '^v' | sort -V | tail -1 || true)
  [ -n "$TAG" ] && timeout 30 gh release view "$TAG" --json tagName > /dev/null 2>&1 && break
  TAG=""
  sleep 15
done
[ -n "$TAG" ] || { echo "no release tagged at $SHA"; exit 1; }
[ "$(git rev-list -n 1 "$TAG")" = "$SHA" ] || { echo "$TAG does not point at $SHA"; exit 1; }
echo "tag=$TAG"
timeout 590 ssh -i ~/.ssh/ghr_lxc -o BatchMode=yes root@192.168.0.99 "cd /root/github-runner && GHR_VERSION=$TAG timeout 570 bash setup.sh > /root/setup-$TAG.log 2>&1; echo rc=\$?; systemctl is-active ghr; ghr version"
```

Expected: `ci=ok`, `tag=v0.1.7`, `rc=0`, `active`, `v0.1.7`. Any other ending (the script exits non-zero before the ssh line) means stop and report; nothing was deployed.

- [ ] **Step 5: Dashboard, Repositories, label check, Help (tmux script A)**

Script body after the helpers:

```bash
w "╭─ CPU " 90; w "╭─ Memory "
# The sampler takes one sample at start and one a minute; CPU needs two.
for i in $(seq 30); do
  M=$(curl -s --unix-socket /run/ghr/ghr.sock http://ghr/metrics)
  echo "$M" | jq -e '(.samples | length) >= 2 and .cpu != null and .mem_used != null and .mem_total != null and ([.samples[] | select(.cpu != null)] | length) >= 1' > /dev/null && break
  sleep 10
done
echo "$M" | jq -c '{n: (.samples | length), cpu, mem_used, mem_total, disk_pct}'
echo "$M" | jq -e '.cpu != null and .mem_used != null' > /dev/null || { echo "metrics never filled"; exit 1; }
tmux capture-pane -p -t $S | grep -q "[▁▂▃▄▅▆▇█]" || { echo "no sparkline on the Dashboard"; tmux capture-pane -p -t $S; exit 1; }
tmux capture-pane -p -t $S | grep -qE "[0-9.]+[GM]" || { echo "no memory reading on the Dashboard"; exit 1; }
tmux send-keys -t $S e; w "Runners get"; w "darkcloud"
tmux send-keys -t $S Down Down Down; w "─ ghr-e2e "
tmux send-keys -t $S Tab Tab Tab Tab Tab Tab Tab Enter; w "Checked" 90; w "[MATCHED]"
tmux send-keys -t $S BTab BTab Enter; tmux send-keys -t $S -l accept-tmp; tmux send-keys -t $S Enter Escape
w "1 unsaved change (ghr-e2e)"; tmux send-keys -t $S C-s; w "Settings saved"; w "accept-tmp"
tmux send-keys -t $S Tab; tmux send-keys -t $S "?"; w "add/remove/edit"; w "Repositories"; tmux send-keys -t $S Escape
echo step=ok
```

Expected: `step=ok`. (The seven tabs go list → Pause → Remove → Max → Warm → Labels → Cleanup → Check now; two back-tabs reach Labels.)

- [ ] **Step 6: Settings token, Reload and Prune now (tmux script B)**

```bash
tmux send-keys -t $S 5; w "GitHub token"; w "expires"
tmux capture-pane -p -t $S | grep -E "Status|Rate limit"
tmux send-keys -t $S BTab BTab Enter; w "config reloaded"
tmux send-keys -t $S Tab Enter; w "prune started"
tmux send-keys -t $S 1; w "prune finished" 300
echo step=ok
```

Expected: `step=ok`; the printed Status line shows the live token badge and an expiry date consistent with the header Step 2 printed.

- [ ] **Step 7: Registrations (tmux scripts C and D)**

The wrapper creates the throwaway registration (`accept-stale-<random>`, labels `self-hosted`) and substitutes its name for `NAME`. Script C, after the helpers:

```bash
tmux send-keys -t $S 2 Down Down Down; w "─ ghr-e2e "; w "NAME" 30
ROW=$(tmux capture-pane -p -t $S | grep -n "NAME" | head -1 | cut -d: -f1)
COL=$(tmux capture-pane -p -t $S | sed -n "${ROW}p" | awk '{print index($0, "[ Delete ]")}')
[ "$COL" -gt 0 ] || { echo "no Delete on the fixture row"; exit 1; }
click $((COL + 2)) $ROW; w "Delete the runner registration NAME from ghr-e2e?"
tmux send-keys -t $S Enter; w "deleted NAME"
echo step=ok
```

The wrapper then confirms on GitHub that the registration is gone, dispatches ghr-e2e's `single` workflow, waits until `ghr status` shows `global 1/`, and runs script D, which checks the ghr row has no Delete:

```bash
tmux send-keys -t $S 2 Down Down Down; w "─ ghr-e2e "; w "[GHR]" 60
tmux capture-pane -p -t $S | grep "ghr-ghr-e2e-" | grep -q "Delete" && { echo "a ghr registration offers Delete"; exit 1; }
echo step=ok
```

(If the card loaded before the runner registered, press the card's Refresh: tab to it, or re-open the page with `1` then `2`.) Leave darkcloud's `linux-1` alone.

- [ ] **Step 8: Check the restore**

The wrapper's `finish` restored the config whatever happened. In `accept.log`, confirm: four `step=ok` lines, `acceptance exit=0`, the printed checksum equals `CFG0`, `accept sessions: 0`, and a status line. If the exit is not 0, the failed script's capture is above it: report it; the fixture is already deleted and the config restored.

- [ ] **Step 9: Record**

In `D:/Repositories/Personal/homelab`, with `R=D:/Repositories/Personal/darkraise-ai-plugins/plugins/dr-superpowers/scripts/register` and `F=docs/superpowers/registers/2026-10-05-ghr-repositories-page.md`, run for each row `ID` in `1 2 3 4 5`:

```bash
bash "$R" set "$F" ID done --note "ghr <TAG> on the LXC: <what accept.log showed for this row>; config.yaml restored byte-identical"
```

then `bash "$R" check "$F"` (it must report no errors), and commit `docs(registers): record ghr <TAG> repositories page`.

---
