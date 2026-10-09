# ghr Actions View Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an Actions page that lists the active and recent GitHub workflow runs of every configured and watched repository, and let the owner watch repositories that get no runners.

**Architecture:** A `watch_repos` config list with `POST /watch` and `DELETE /watch/{name}`; a `GET /actions` endpoint that calls `ListRecentRuns` per covered repository (four at a time, detached from the request, 20 s deadline) behind a 15 s cache keyed on the config pointer; a React Actions page that filters the one response in the browser; a watched-repositories section on the Repositories page built on a `RepoPicker` extracted from the add-repository dialog.

**Tech Stack:** Go 1.26 standard library (`net/http`, `sync`, `context`), React 19 + TypeScript, TanStack Router and Query, darkraise-ui, Vitest + Testing Library, axe-core.

**Spec:** docs/superpowers/specs/2026-10-09-ghr-actions-view-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 13 of 14 tasks total 4 or less; Task 6 (concurrent fetch and cache, risk 3) is delegated.

**Plan review:** 2026-10-09 — dr-superpowers:judge-opus — executability 15 / coherence 18 / coverage 18 / assumptions 12 (round 2)

## Global Constraints

- Execution environment: the owner's Linux machine (bash, GNU coreutils `timeout`, Go 1.26, Node 24 from `web/.nvmrc`). Task 1 Step 0 checks it; on any other shell, translate `timeout N cmd` and `VAR=1 cmd` to that shell's equivalents.
- Work on a branch, never on `master`. Do not push `master`: CI turns every push to `master` into a release.
- Go checks, from the repository root: `gofmt -l internal cmd web/*.go` prints nothing; `go vet ./internal/... ./cmd/... ./web/` passes. Never run `go vet ./...` or `go test ./...`: they walk Go code under `web/node_modules`.
- Web checks, from `web/`: `npm test`, `npm run typecheck`, `npm run lint` (eslint with `--max-warnings 0`). Web tests run with `TZ=UTC` (`web/vite.config.ts:23`), and the app's clock follows the status fixture's `now`, 2026-10-03T14:05:00Z.
- Give every test command an explicit timeout (`timeout 300 …`).
- Spec 1 §1.7 and spec 2 §1.2 copy rules bind every page: no "·" separator chains, no dash characters in empty cells, no kit `Badge` import outside `src/components/tag-field.tsx` (`web/src/anti-slop.test.ts` enforces it), mono for IDs, numbers, times and branches, sentence-case toasts, icon buttons carry an `aria-label` plus a tooltip.
- English only. Comments only where the why is not obvious.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.
- Watched names are bare repository names under `owner`, compared case-insensitively everywhere.

## Contracts

**Go — config and watch API**

- `config.Config.WatchRepos []string` — yaml `watch_repos,omitempty`, json `watch_repos,omitempty`; entries are trimmed names with no `/` (Task 1).
- `func (c *config.Config) Watched(name string) bool` — case-insensitive membership (Task 1).
- `model.AvailableRepo` gains `Watched bool`, json `watched` (Task 2).
- `type WatchRequest struct { Name string }` in `internal/model`, json `name` (Task 2).
- `func (b *daemon.Backend) WatchRepo(ctx context.Context, name string) error` — 400 for an empty name; 400 "token cannot see {owner}/{name}: add it to the PAT's repository access first"; 409 "repo {name} is already configured"; 409 "repo {name} is already watched" (Task 2).
- `func (b *daemon.Backend) UnwatchRepo(name string) error` — 404 "repo {name} is not watched"; never calls GitHub (Task 2).
- `api.Backend` gains `WatchRepo(ctx context.Context, name string) error`, `UnwatchRepo(name string) error` (Task 3) and `Actions(ctx context.Context) (model.Actions, error)` (Task 7). Routes: `POST /watch` with body `model.WatchRequest`, `DELETE /watch/{name}`, `GET /actions`; each answers through `respond`, so a nil value is 204.

**Go — workflow runs**

- `github.Run` (Task 4), field, type, json: `ID int64 id`, `Name string name`, `DisplayTitle string display_title`, `RunNumber int64 run_number`, `HeadBranch string head_branch`, `Event string event`, `Actor Account actor`, `Status string status`, `Conclusion string conclusion`, `CreatedAt time.Time created_at`, `RunStartedAt *time.Time run_started_at`, `UpdatedAt time.Time updated_at`, `HTMLURL string html_url`. `github.Account{Login string}` (json `login`) already exists at `internal/github/releases.go:104`.
- `model.Actions` (Task 5): `FetchedAt time.Time fetched_at`, `Runs []ActionsRun runs`, `Repos []ActionsRepo repos`.
- `model.ActionsRun` (Task 5): `Repo string repo`, `ID int64 id`, `RunNumber int64 run_number`, `Workflow string workflow`, `Title string title`, `Branch string branch`, `Event string event`, `Actor string actor`, `Status string status`, `Conclusion string conclusion`, `StartedAt time.Time started_at`, `UpdatedAt time.Time updated_at`, `HTMLURL string html_url`, `GHR bool ghr`, `Watched bool watched`.
- `model.ActionsRepo` (Task 5): `Repo string repo`, `Watched bool watched`, `Error string error,omitempty`, `RetryAt *time.Time retry_at,omitempty`.
- `internal/daemon/actions.go` (Task 5): `const actionRunsPerRepo = 50`; `type coveredRepo struct { repo string; watched bool }`; `func coveredRepos(cfg *config.Config) []coveredRepo`; `func ghrRunKey(repo string, runID int64) string` (lower-case repo, `#`, decimal ID); `func actionsRun(repo string, watched bool, r github.Run, ours map[string]bool) model.ActionsRun`; `func actionsErr(err error, owner, repo string) (string, *time.Time)`; `func keepsRuns(err error) bool`; `func sortActions(rs []model.ActionsRun)` (not completed first, newest start first, then repository ignoring case, then ID).
- `internal/daemon/actions_test.go` (Task 5): `func testRun(id int64, status, conclusion string, start time.Time) github.Run` — Name "ci", DisplayTitle "run {id}", RunNumber id, CreatedAt and RunStartedAt = start, UpdatedAt = start plus one minute. Task 6 uses it.
- `internal/daemon/actions.go` (Task 6): `const actionsTTL = 15 * time.Second`; `const actionsWorkers = 4`; `var actionsDeadline = 20 * time.Second`; `type actionsCache struct`; field `Backend.actions actionsCache`; `func (b *Backend) Actions(ctx context.Context) (model.Actions, error)`; `func (b *Backend) ghrRuns() map[string]bool`.
- `internal/daemon/backend_test.go` `fakeGH` additions: `getCalls int` (Task 2); `recentBy`, `recentErr`, `recentWait`, `recentCalls`, `recentN`, `recentInFlight`, `recentPeak` (Task 6).

**Fixtures (Task 7)**

- `web/src/api/fixtures/actions.json`: `fetched_at` 2026-10-03T14:05:00Z; runs in endpoint order: id 300 docs #9 `queued`, watched, started 14:04, title "Publish the guide"; id 102 darkmem #42 `in_progress`, ghr, started 14:01, title "Fix the cache key"; id 101 darkmem #41 `completed`/`success`, ghr, 13:35 to 13:40, title "Bump the runner", branch "renovate/runner"; id 90 darkcloud #7 `completed`/`failure`, 2026-10-02 14:05 to 14:06:30, title "deploy". Repos: darkmem, darkcloud, docs (watched), old-docs (watched, error "GitHub rate limit; API calls are paused", `retry_at` 2026-10-03T14:30:00Z).
- `config.json` gains `watch_repos: ["docs", "old-docs"]`; `available-repos.json` is darkmem (private, configured), docs (public, watched), new-repo (private), each with `watched`.

**TypeScript**

- `web/src/api/types.ts` (Task 8): `Config.watch_repos?: string[]`; `AvailableRepo.watched: boolean`; `interface ActionsRun { repo: string; id: number; run_number: number; workflow: string; title: string; branch: string; event: string; actor: string; status: string; conclusion: string; started_at: string; updated_at: string; html_url: string; ghr: boolean; watched: boolean }`; `interface ActionsRepo { repo: string; watched: boolean; error?: string; retry_at?: string }`; `interface Actions { fetched_at: string; runs: ActionsRun[]; repos: ActionsRepo[] }`.
- `web/src/test/fixtures.ts` (Task 8): `fixtures.actions: Actions`; `authedRoutes()` serves `"GET /api/actions": fixtures.actions`.
- `web/src/lib/actions.ts` (Task 9): `STATUS_FILTERS` (`active` Active, `failed` Failed, `succeeded` Succeeded); `type ActionsStatus = "active" | "failed" | "succeeded"`; `interface ActionsSearch { repo?: string; status?: ActionsStatus }`; `parseActionsSearch(search: Record<string, unknown>): ActionsSearch`; `isActive(r: ActionsRun): boolean` (status is not `completed`); `isQueued(r: ActionsRun): boolean` (active and not `in_progress`); `statusWord(r: ActionsRun): string`; `RECENT_LIMIT = 100`; `splitRuns(runs: ActionsRun[], repo: string | undefined, status: ActionsStatus | undefined): { active: ActionsRun[]; recent: ActionsRun[] }`; `runMs(r: ActionsRun, now: number): number`; `startText(iso: string, now: number): string`; `actionsSummary(runs: ActionsRun[], repos: number, repo: string | undefined, fetchedAt: Date): string`; `repoErrorText(r: ActionsRepo): string`.
- `web/src/lib/history.ts` (Task 10): `interface DayGroupOf<T> { key: string; label: string; rows: T[] }`; `groupByDayOf<T>(rows: T[], at: (row: T) => string, now: number): DayGroupOf<T>[]`; `type DayGroup = DayGroupOf<HistoryEntry>`; `groupByDay(rows: HistoryEntry[], now: number): DayGroup[]` keeps its signature.
- `web/src/api/client.ts`: `api.actions(signal?: AbortSignal): Promise<Actions>` (Task 11); `api.watchRepo(name: string): Promise<void>` and `api.unwatchRepo(name: string): Promise<void>` (Task 13).
- `web/src/api/hooks.ts` (Task 11): `POLL_ACTIONS = 15_000`; `keys.actions = ["actions"] as const`; `useActions()`.
- `web/src/components/repo-picker.tsx` (Task 12): `RepoPicker({ picked, onPick, disableWatched }: { picked: string; onPick: (name: string) => void; disableWatched?: boolean })`.
- `web/src/components/page/section.tsx` (Task 13): `Section` gains optional `id?: string`.

## Assumptions (evidence)

- Execution runs on the owner's Linux machine: the owner said on 2026-10-09, "I will start the execution and continue the development on another linux machine". Earlier plans ran on the Windows dev box, where `go test -race` cannot run (`docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md:25`); Task 1 Step 0 checks the machine, and Task 6 Step 5 says what to do without the race detector.
- Borderline review score (assumptions 12, round 2): accepted, because the only open finding was the Linux evidence, now cited from the owner's instruction above.
- The store installs a new config pointer on every `Update` and `Reload` (`internal/daemon/store.go:75,116`). `Configure` replaces it only when the owner changes (`store.go:146-151`); it never touches `repos` or `watch_repos`, so the covered set, which is all the actions cache keys on, cannot change without a new pointer. `Store.Update` works on a clone (`store.go:105`) and `Config.Clone` round-trips YAML (`internal/config/config.go:218-228`), so `watch_repos` survives every other config write.
- `config.Parse` decodes with `KnownFields(true)` (`internal/config/config.go:163`): an older binary refuses `watch_repos`. Release notes are generated by CI (`.github/workflows/ci.yml` release job), so Task 1 puts the downgrade note in `README.md`'s Install / upgrade section, and the spec says so.
- `RunnerSteps` already detaches a shared fetch with `context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)` (`internal/daemon/backend.go:291-294`); Task 6 copies the pattern.
- `githubErr()` returns an `*api.Error` (503 when degraded, 429 with `RetryAt` when paused): `internal/daemon/backend.go:725-742`.
- The GitHub client returns `http.Client.Do` errors unwrapped (`internal/github/client.go:269-272`), so `errors.Is(err, context.DeadlineExceeded)` sees a deadline through `*url.Error`.
- The kit spinner animates with `animate-spin` and has no reduced-motion rule (`web/node_modules/darkraise-ui/dist/components/spinner.css`); the app's reduced-motion block is `web/src/styles/ghr-theme.css:321-331`. Task 14 adds the spinner to it.
- `useMediaQuery("(min-width: 768px)")` is false under the default test stub and follows `setViewport` (`web/src/test/media.ts`), so page tests render the narrow layout unless they call `setViewport(1280)`.
- TanStack Router's `location.hash` carries no leading `#`; Task 13 strips one anyway and scrolls the section itself, so it does not depend on the router's hash scrolling.
- `history.Store.Query` fails when `Path` is a directory: `readAll` opens it and the scanner's read fails (`internal/history/history.go:66-85`). Task 6's failed-history test relies on it; execution runs on Linux, where reading a directory returns EISDIR.
- Ruling (spec §3.4, amended 2026-10-09): the add-repository dialog keeps watched entries pickable, labelled "watched", because `AddRepo` takes a watched repository over; only the watch dialog disables them (`disableWatched`).
- Ruling: the watch dialog, like the add dialog, keeps its button enabled while the daemon is unreachable and shows the request's error; `web/src/components/add-repo-dialog.test.tsx` ("sends Add while the daemon is unreachable…") pins that behaviour for the add dialog. The Repositories page disables the buttons that open both dialogs.
- Ruling: an active status other than `in_progress` (`queued`, `waiting`, `pending`, `requested`, or any other GitHub adds) counts and shows as queued; only `in_progress` is running (spec §3.2).
- Ruling: the actions cache keeps each repository's last good runs as well as the last response (spec §2.2's rule needs per-repository rows); both are bounded by 50 runs per covered repository.
- `web/src/pages/history.test.tsx:51-53` drives the Repository combobox with `user.click`, so Task 11's tests can too. The app clock follows `/api/status`'s `now` (`web/src/lib/use-now.ts`), so a status mock that moves `now` moves every elapsed time.
- Ruling: when the history read fails, no run is marked `ghr`, even if pending records or live instances would mark it (spec §2.2).
- `plural()` appends "s" (`web/src/lib/format.ts`), which would give "repositorys"; `actionsSummary` spells the repository count itself.

## Task index

1. Watch list in the config
2. Watch and unwatch in the daemon
3. Watch routes
4. Workflow run fields
5. Actions model and run helpers
6. Fetch and cache workflow runs
7. Actions route and fixtures
8. Actions types and test fixtures
9. Actions view helpers
10. Day grouping for any row
11. The Actions page
12. Shared repository picker
13. Watched repositories section
14. Accessibility and reduced motion

---

### Task 1: Watch list in the config

**Files:**
- Modify: `internal/config/config.go` (the `Config` struct at lines 37-52, `Validate` after the repos loop ending near line 350, a new method after `Repo` at line 238)
- Modify: `README.md` (the "Install / upgrade" section)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: none
- Produces: `config.Config.WatchRepos`, `(*config.Config).Watched` (Contracts, Go)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 0: Check the environment**

Run: `uname -s && go version && node --version && git status -sb | head -1`
Expected: `Linux`, Go 1.26.x, Node v24.x, and a branch other than `master` (create one with `git switch -c feat/actions-view` if needed). If the machine is not Linux, stop and tell the owner before any task runs: Task 6's race check and the failed-history tests assume Linux.

- [ ] **Step 1: Write the failing tests**

In `internal/config/config_test.go`, add five entries to the `cases` map in `TestValidateErrors` (after the `"history_retention must be >= 1d"` entry):

```go
		"watch_repos must not contain an empty name":        func(c *Config) { c.WatchRepos = []string{" "} },
		"watch_repos:  docs  must not have surrounding spaces": func(c *Config) { c.WatchRepos = []string{" docs "} },
		"watch_repos: other/docs must be a repository name": func(c *Config) { c.WatchRepos = []string{"other/docs"} },
		"duplicate watched repo Docs":                        func(c *Config) { c.WatchRepos = []string{"docs", "Docs"} },
		"watch_repos: DarkMem is already a configured repo":  func(c *Config) { c.WatchRepos = []string{"DarkMem"} },
```

Append this test at the end of the file:

```go
func TestWatchReposRoundTripAndLookup(t *testing.T) {
	c, _, err := Parse([]byte(sample + "watch_repos: [docs]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Watched("DOCS") || c.Watched("darkmem") {
		t.Fatalf("Watched: %v", c.WatchRepos)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.WatchRepos) != 1 || back.WatchRepos[0] != "docs" {
		t.Fatalf("round trip: %v", back.WatchRepos)
	}
	if d := back.Clone(); len(d.WatchRepos) != 1 {
		t.Fatalf("clone lost watch_repos: %v", d.WatchRepos)
	}

	plain, _, _ := Parse([]byte(sample))
	data, _ := json.Marshal(plain)
	if strings.Contains(string(data), "watch_repos") {
		t.Fatalf("empty watch_repos marshalled: %s", data)
	}
	if err := Save(path, plain); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "watch_repos") {
		t.Fatalf("empty watch_repos saved:\n%s", raw)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 120 go test ./internal/config/`
Expected: FAIL to compile with `c.WatchRepos undefined` and `c.Watched undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, add the field to `Config` directly after `Repos`:

```go
	Repos            []Repo       `yaml:"repos" json:"repos"`
	// WatchRepos are repositories whose workflow runs the Actions page shows;
	// ghr runs no runners for them.
	WatchRepos []string `yaml:"watch_repos,omitempty" json:"watch_repos,omitempty"`
```

In `Validate`, directly after the closing brace of the `for _, r := range c.Repos { … }` loop (the loop that fills `seen`), insert:

```go
	watched := map[string]bool{}
	for _, w := range c.WatchRepos {
		name := strings.TrimSpace(w)
		if name == "" {
			errs = append(errs, "watch_repos must not contain an empty name")
			continue
		}
		if name != w {
			errs = append(errs, "watch_repos: "+w+" must not have surrounding spaces")
		}
		if strings.Contains(name, "/") {
			errs = append(errs, "watch_repos: "+w+" must be a repository name under owner, without a slash")
		}
		key := strings.ToLower(name)
		if watched[key] {
			errs = append(errs, "duplicate watched repo "+w+" (names are case-insensitive)")
		}
		watched[key] = true
		if seen[key] {
			errs = append(errs, "watch_repos: "+w+" is already a configured repo")
		}
	}
```

After the `Repo` method, add:

```go
// Watched reports whether name is in watch_repos, ignoring case.
func (c *Config) Watched(name string) bool {
	for _, w := range c.WatchRepos {
		if strings.EqualFold(w, name) {
			return true
		}
	}
	return false
}
```

Run `gofmt -w internal/config/config.go internal/config/config_test.go` (it realigns the struct tags and the map entries).

In `README.md`, at the end of the "Install / upgrade" section, add:

```markdown
**Downgrading.** A config that lists `watch_repos` (the repositories the web UI's Actions page watches) is refused by any ghr older than that page. Remove the `watch_repos` key from the config file before installing an older release.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 120 go test ./internal/config/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go README.md
git commit -m "feat(config): add the watch_repos list"
```

### Task 2: Watch and unwatch in the daemon

**Files:**
- Modify: `internal/model/model.go` (`AvailableRepo` at line 264, `AddRepoRequest` at line 180)
- Modify: `internal/daemon/backend.go` (`AddRepo` at line 444, `AvailableRepos` at line 836)
- Test: `internal/daemon/backend_test.go`

**Interfaces:**
- Consumes: `config.Config.WatchRepos`, `(*config.Config).Watched` (Contracts, Go)
- Produces: `model.AvailableRepo.Watched`, `model.WatchRequest`, `Backend.WatchRepo`, `Backend.UnwatchRepo` (Contracts, Go)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/daemon/backend_test.go` (the file already imports `context`, `strings`, `config`, `github`, `model`):

```go
func TestWatchRepo(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()
	for _, c := range []struct {
		name   string
		status int
		text   string
	}{
		{" ", 400, "required"},
		{"missing", 400, "repository access"},
		{"DarkCloud", 409, "already configured"},
	} {
		if err := b.WatchRepo(ctx, c.name); apiStatus(err) != c.status || !strings.Contains(err.Error(), c.text) {
			t.Errorf("WatchRepo(%q) = %v", c.name, err)
		}
	}
	if err := b.WatchRepo(ctx, "public"); err != nil {
		t.Fatal(err)
	}
	if err := b.WatchRepo(ctx, "Public"); apiStatus(err) != 409 || !strings.Contains(err.Error(), "already watched") {
		t.Fatalf("second watch: %v", err)
	}
	if got := b.Store.Config().WatchRepos; len(got) != 1 || got[0] != "public" {
		t.Fatalf("watch_repos %v", got)
	}
	if _, err := b.Store.Update(func(c *config.Config) error {
		r := c.Repo("darkmem")
		r.Paused, r.Removing = true, true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.WatchRepo(ctx, "darkmem"); apiStatus(err) != 409 {
		t.Fatalf("repo being removed: %v", err)
	}
}

func TestUnwatchRepoNeverAsksGitHub(t *testing.T) {
	b, _, gh := newBackend(t)
	if err := b.WatchRepo(context.Background(), "public"); err != nil {
		t.Fatal(err)
	}
	delete(gh.repos, "public")
	before := gh.getCalls
	if err := b.UnwatchRepo("nope"); apiStatus(err) != 404 {
		t.Fatalf("unknown: %v", err)
	}
	if err := b.UnwatchRepo("PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if got := b.Store.Config().WatchRepos; len(got) != 0 {
		t.Fatalf("watch_repos %v", got)
	}
	if gh.getCalls != before {
		t.Fatalf("unwatch called GetRepo %d times", gh.getCalls-before)
	}
}

func TestConcurrentUnwatchSucceedsOnce(t *testing.T) {
	b, _, _ := newBackend(t)
	if err := b.WatchRepo(context.Background(), "public"); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- b.UnwatchRepo("public") }()
	}
	first, second := <-errs, <-errs
	if (first == nil) == (second == nil) || (apiStatus(first) != 404 && apiStatus(second) != 404) {
		t.Fatalf("errors %v and %v, want one success and one 404", first, second)
	}
}

func TestAddRepoTakesOverAWatchedRepo(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()
	if err := b.WatchRepo(ctx, "newrepo"); err != nil {
		t.Fatal(err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "newrepo", Labels: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	c := b.Store.Config()
	if c.Repo("newrepo") == nil || len(c.WatchRepos) != 0 {
		t.Fatalf("repos %+v watch_repos %v", c.Repos, c.WatchRepos)
	}
}

func TestPatchConfigKeepsWatchRepos(t *testing.T) {
	b, _, _ := newBackend(t)
	if err := b.WatchRepo(context.Background(), "public"); err != nil {
		t.Fatal(err)
	}
	three := 3
	if err := b.PatchConfig(model.ConfigPatch{GlobalMax: &three}); err != nil {
		t.Fatal(err)
	}
	if got := b.Store.Config().WatchRepos; len(got) != 1 || got[0] != "public" {
		t.Fatalf("watch_repos %v", got)
	}
}

func TestAvailableReposMarksWatched(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.userRepos = []github.UserRepo{
		{Name: "public", Owner: github.Account{Login: "darkraise"}},
		{Name: "darkmem", Private: true, Owner: github.Account{Login: "darkraise"}},
	}
	if err := b.WatchRepo(context.Background(), "public"); err != nil {
		t.Fatal(err)
	}
	rs, err := b.AvailableRepos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []model.AvailableRepo{{Name: "darkmem", Private: true, Configured: true}, {Name: "public", Watched: true}}
	if len(rs) != 2 || rs[0] != want[0] || rs[1] != want[1] {
		t.Fatalf("available %+v", rs)
	}
}
```

In the same file, count `GetRepo` calls: add a field to `fakeGH` after `deleted []int64`:

```go
	getCalls int // GetRepo calls
```

and make `GetRepo` count them:

```go
func (f *fakeGH) GetRepo(ctx context.Context, repo string) (*github.Repository, error) {
	f.getCalls++
	r, ok := f.repos[repo]
	if !ok {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound}
	}
	return r, nil
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run 'Watch|TakesOver|KeepsWatch'`
Expected: FAIL to compile with `b.WatchRepo undefined` and `unknown field Watched`.

- [ ] **Step 3: Write the implementation**

In `internal/model/model.go`, add `Watched` to `AvailableRepo`:

```go
type AvailableRepo struct {
	Name       string `json:"name"`
	Private    bool   `json:"private"`
	Configured bool   `json:"configured"`
	Watched    bool   `json:"watched"`
}
```

and, directly after `AddRepoRequest`, add:

```go
// WatchRequest is POST /watch.
type WatchRequest struct {
	Name string `json:"name"`
}
```

In `internal/daemon/backend.go`, inside `AddRepo`'s `b.update` closure, remove the name from the watch list before appending the repository (the config would otherwise fail validation):

```go
	if err := b.update(func(c *config.Config) error {
		c.WatchRepos = slices.DeleteFunc(c.WatchRepos, func(w string) bool { return strings.EqualFold(w, name) })
		c.Repos = append(c.Repos, config.Repo{Name: name, Max: req.Max, Labels: req.Labels})
		return nil
	}); err != nil {
```

In `AvailableRepos`, set the new field:

```go
			out = append(out, model.AvailableRepo{Name: r.Name, Private: r.Private, Configured: cfg.Repo(r.Name) != nil, Watched: cfg.Watched(r.Name)})
```

Add these two methods directly after `AddRepo`:

```go
// WatchRepo adds name to watch_repos once the token can see it. Public
// repositories are allowed: no runner ever serves a watched one.
func (b *Backend) WatchRepo(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return api.BadRequest("repo name is required")
	}
	cfg := b.Store.Config()
	if cfg.Repo(name) != nil {
		return api.Conflict("repo " + name + " is already configured")
	}
	if cfg.Watched(name) {
		return api.Conflict("repo " + name + " is already watched")
	}
	_, err := b.GH.GetRepo(ctx, name)
	if github.IsKind(err, github.ErrNotFound) {
		return api.BadRequest(fmt.Sprintf("token cannot see %s/%s: add it to the PAT's repository access first", cfg.Owner, name))
	}
	if err != nil {
		return err
	}
	if err := b.update(func(c *config.Config) error {
		if c.Repo(name) != nil || c.Watched(name) {
			return api.Conflict("repo " + name + " is already configured or watched")
		}
		c.WatchRepos = append(c.WatchRepos, name)
		return nil
	}); err != nil {
		return err
	}
	b.Events.Add("info", name, "watching")
	return nil
}

// UnwatchRepo never calls GitHub, so a repository GitHub no longer shows can
// always be unwatched. The check runs inside the update so two concurrent
// removals cannot both succeed.
func (b *Backend) UnwatchRepo(name string) error {
	if err := b.update(func(c *config.Config) error {
		if !c.Watched(name) {
			return api.NotFound("repo " + name + " is not watched")
		}
		c.WatchRepos = slices.DeleteFunc(c.WatchRepos, func(w string) bool { return strings.EqualFold(w, name) })
		return nil
	}); err != nil {
		return err
	}
	b.Events.Add("info", name, "stopped watching")
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ ./internal/model/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "feat(daemon): watch and unwatch repositories"
```

### Task 3: Watch routes

**Files:**
- Modify: `internal/api/server.go` (the `Backend` interface at lines 60-93, routes after `DELETE /repos/{name}` near line 189)
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: `Backend.WatchRepo`, `Backend.UnwatchRepo`, `model.WatchRequest` (Contracts, Go)
- Produces: routes `POST /watch`, `DELETE /watch/{name}` (Contracts, Go)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing test**

In `internal/api/api_test.go`, add two fields to `fakeBackend` (after `storageCalls`):

```go
	watched       []string
	unwatched     []string
```

add the two methods after `AvailableRepos`:

```go
func (f *fakeBackend) WatchRepo(ctx context.Context, name string) error {
	f.watched = append(f.watched, name)
	return nil
}

func (f *fakeBackend) UnwatchRepo(name string) error {
	f.unwatched = append(f.unwatched, name)
	return nil
}
```

and append this test:

```go
func TestWatchRoutes(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	if err := c.call(ctx, http.MethodPost, "/watch", strings.NewReader(`{"name":"docs"}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := c.call(ctx, http.MethodDelete, "/watch/old-docs", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.watched, []string{"docs"}) || !reflect.DeepEqual(b.unwatched, []string{"old-docs"}) {
		t.Fatalf("watched %v unwatched %v", b.watched, b.unwatched)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 120 go test ./internal/api/ -run TestWatchRoutes`
Expected: FAIL: the request gets the router's 404, reported as "the running ghr daemon does not know POST /watch".

- [ ] **Step 3: Write the implementation**

In `internal/api/server.go`, add to the `Backend` interface after `RemoveRepo(name string) error`:

```go
	WatchRepo(ctx context.Context, name string) error
	UnwatchRepo(name string) error
```

and register the routes after the `DELETE /repos/{name}` handler:

```go
	mux.HandleFunc("POST /watch", func(w http.ResponseWriter, r *http.Request) {
		var req model.WatchRequest
		if err := decode(r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.WatchRepo(r.Context(), req.Name))
	})
	mux.HandleFunc("DELETE /watch/{name}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.UnwatchRepo(r.PathValue("name")))
	})
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/api/ ./internal/daemon/`
Expected: PASS (the daemon's `var _ api.Backend = (*Backend)(nil)` compiles because Task 2 added both methods).

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/api_test.go
git commit -m "feat(api): add the watch routes"
```

### Task 4: Workflow run fields

**Files:**
- Modify: `internal/github/client.go:43-46` (the `Run` struct)
- Test: `internal/github/client_test.go`

**Interfaces:**
- Consumes: none
- Produces: the `github.Run` fields (Contracts, Go)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Append to `internal/github/client_test.go`:

```go
func TestRunFieldsDecode(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"workflow_runs":[
			{"id":102,"name":"ci","display_title":"Fix the cache key","run_number":42,"head_branch":"master",
			 "event":"push","actor":{"login":"darkraise"},"status":"in_progress","conclusion":null,
			 "created_at":"2026-10-09T14:00:00Z","run_started_at":"2026-10-09T14:01:00Z",
			 "updated_at":"2026-10-09T14:04:00Z","html_url":"https://github.com/darkraise/darkmem/actions/runs/102"},
			{"id":101,"name":"ci","run_number":41,"status":"completed","conclusion":"success",
			 "created_at":"2026-10-09T13:00:00Z","updated_at":"2026-10-09T13:05:00Z"}]}`)
	})
	runs, err := c.ListRecentRuns(context.Background(), "darkmem", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs %+v", runs)
	}
	r := runs[0]
	start := time.Date(2026, 10, 9, 14, 1, 0, 0, time.UTC)
	if r.ID != 102 || r.Name != "ci" || r.DisplayTitle != "Fix the cache key" || r.RunNumber != 42 || r.HeadBranch != "master" ||
		r.Event != "push" || r.Actor.Login != "darkraise" || r.Status != "in_progress" || r.Conclusion != "" ||
		!r.CreatedAt.Equal(start.Add(-time.Minute)) || r.RunStartedAt == nil || !r.RunStartedAt.Equal(start) ||
		!r.UpdatedAt.Equal(start.Add(3*time.Minute)) || r.HTMLURL != "https://github.com/darkraise/darkmem/actions/runs/102" {
		t.Fatalf("run %+v", r)
	}
	if runs[1].RunStartedAt != nil || runs[1].Conclusion != "success" {
		t.Fatalf("run without run_started_at %+v", runs[1])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 120 go test ./internal/github/ -run TestRunFieldsDecode`
Expected: FAIL to compile with `r.Name undefined`.

- [ ] **Step 3: Write the implementation**

Replace the `Run` struct in `internal/github/client.go`:

```go
// Run is a workflow run. The tick and the label check read only ID; the
// Actions page reads the rest. GitHub may leave run_started_at out.
type Run struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	DisplayTitle string     `json:"display_title"`
	RunNumber    int64      `json:"run_number"`
	HeadBranch   string     `json:"head_branch"`
	Event        string     `json:"event"`
	Actor        Account    `json:"actor"`
	Status       string     `json:"status"`
	Conclusion   string     `json:"conclusion"`
	CreatedAt    time.Time  `json:"created_at"`
	RunStartedAt *time.Time `json:"run_started_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	HTMLURL      string     `json:"html_url"`
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/github/ ./internal/runner/ ./internal/daemon/`
Expected: PASS (the tick and the label check build `Run{ID: …, Status: …}` literals by field name, which still compile).

- [ ] **Step 5: Commit**

```bash
git add internal/github/client.go internal/github/client_test.go
git commit -m "feat(github): decode the workflow run fields"
```

### Task 5: Actions model and run helpers

**Files:**
- Modify: `internal/model/model.go` (append after `AvailableRepo`)
- Create: `internal/daemon/actions.go`
- Test: `internal/daemon/actions_test.go`

**Interfaces:**
- Consumes: `github.Run` fields (Task 4), `config.Config.WatchRepos` (Task 1)
- Produces: `model.Actions`, `model.ActionsRun`, `model.ActionsRepo`, and the Task 5 helpers in `internal/daemon/actions.go` (Contracts, Go)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/actions_test.go`:

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func testRun(id int64, status, conclusion string, start time.Time) github.Run {
	return github.Run{ID: id, Name: "ci", DisplayTitle: fmt.Sprintf("run %d", id), RunNumber: id, Status: status,
		Conclusion: conclusion, CreatedAt: start, RunStartedAt: &start, UpdatedAt: start.Add(time.Minute)}
}

func TestCoveredRepos(t *testing.T) {
	cfg := &config.Config{Repos: []config.Repo{{Name: "darkcloud", Paused: true}, {Name: "darkmem"}}, WatchRepos: []string{"docs"}}
	got := coveredRepos(cfg)
	want := []coveredRepo{{repo: "darkcloud"}, {repo: "darkmem"}, {repo: "docs", watched: true}}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("covered %+v", got)
	}
}

func TestActionsRunFallsBackToCreatedAtAndName(t *testing.T) {
	created := time.Date(2026, 10, 9, 13, 0, 0, 0, time.UTC)
	r := github.Run{ID: 7, Name: "deploy", RunNumber: 3, Status: "completed", Conclusion: "failure", CreatedAt: created,
		HeadBranch: "master", Event: "push", Actor: github.Account{Login: "darkraise"}, HTMLURL: "https://example/7"}
	got := actionsRun("darkmem", true, r, map[string]bool{ghrRunKey("DarkMem", 7): true})
	want := model.ActionsRun{Repo: "darkmem", ID: 7, RunNumber: 3, Workflow: "deploy", Title: "deploy", Branch: "master",
		Event: "push", Actor: "darkraise", Status: "completed", Conclusion: "failure", StartedAt: created,
		HTMLURL: "https://example/7", GHR: true, Watched: true}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	started := created.Add(time.Minute)
	r.RunStartedAt, r.DisplayTitle = &started, "Fix it"
	if got := actionsRun("darkmem", false, r, nil); !got.StartedAt.Equal(started) || got.Title != "Fix it" || got.GHR {
		t.Fatalf("with run_started_at %+v", got)
	}
}

func TestActionsErr(t *testing.T) {
	retry := time.Date(2026, 10, 9, 14, 30, 0, 0, time.UTC)
	for _, c := range []struct {
		err   error
		text  string
		retry *time.Time
		keeps bool
	}{
		{fmt.Errorf("get: %w", context.DeadlineExceeded), "GitHub did not answer in time", nil, false},
		{&github.APIError{Status: 404, Kind: github.ErrNotFound}, "token cannot see darkraise/docs: add it to the PAT's repository access first", nil, false},
		{&github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: retry}, "GitHub rate limit; API calls are paused", &retry, true},
		{&github.APIError{Status: 401, Kind: github.ErrAuth, Message: "Bad credentials"}, "GitHub rejected the token: github: 401 Bad credentials", nil, true},
		{&api.Error{Status: 429, Msg: "GitHub rate limit; API calls are paused", RetryAt: retry}, "GitHub rate limit; API calls are paused", &retry, true},
		{&api.Error{Status: 503, Msg: "GitHub is rejecting the token: 401"}, "GitHub is rejecting the token: 401", nil, true},
		{errors.New("connection reset"), "connection reset", nil, false},
	} {
		text, at := actionsErr(c.err, "darkraise", "docs")
		if text != c.text || (at == nil) != (c.retry == nil) || (at != nil && !at.Equal(*c.retry)) {
			t.Errorf("actionsErr(%v) = %q, %v", c.err, text, at)
		}
		if keepsRuns(c.err) != c.keeps {
			t.Errorf("keepsRuns(%v) = %v", c.err, !c.keeps)
		}
	}
}

func TestSortActions(t *testing.T) {
	t0 := time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)
	rs := []model.ActionsRun{
		{Repo: "b", ID: 1, Status: "completed", StartedAt: t0},
		{Repo: "a", ID: 2, Status: "queued", StartedAt: t0.Add(-time.Hour)},
		{Repo: "a", ID: 3, Status: "completed", StartedAt: t0},
		{Repo: "a", ID: 4, Status: "completed", StartedAt: t0.Add(time.Minute)},
		{Repo: "a", ID: 5, Status: "in_progress", StartedAt: t0},
		{Repo: "a", ID: 0, Status: "completed", StartedAt: t0},
		{Repo: "Zoo", ID: 6, Status: "completed", StartedAt: t0},
		{Repo: "alpha", ID: 7, Status: "completed", StartedAt: t0},
	}
	sortActions(rs)
	var ids []int64
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	if fmt.Sprint(ids) != "[5 2 4 0 3 7 1 6]" {
		t.Fatalf("order %v", ids)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run 'Covered|ActionsRun|ActionsErr|SortActions'`
Expected: FAIL to compile with `undefined: coveredRepos`.

- [ ] **Step 3: Write the implementation**

Append to `internal/model/model.go`:

```go
// Actions is GET /actions: the recent workflow runs of every configured and
// watched repository, whoever ran them.
type Actions struct {
	FetchedAt time.Time     `json:"fetched_at"`
	Runs      []ActionsRun  `json:"runs"`
	Repos     []ActionsRepo `json:"repos"`
}

type ActionsRun struct {
	Repo       string    `json:"repo"`
	ID         int64     `json:"id"`
	RunNumber  int64     `json:"run_number"`
	Workflow   string    `json:"workflow"`
	Title      string    `json:"title"`
	Branch     string    `json:"branch"`
	Event      string    `json:"event"`
	Actor      string    `json:"actor"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	HTMLURL    string    `json:"html_url"`
	GHR        bool      `json:"ghr"`
	Watched    bool      `json:"watched"`
}

// ActionsRepo is one covered repository; Error is set when its runs could
// not be read, and RetryAt when GitHub calls are paused.
type ActionsRepo struct {
	Repo    string     `json:"repo"`
	Watched bool       `json:"watched"`
	Error   string     `json:"error,omitempty"`
	RetryAt *time.Time `json:"retry_at,omitempty"`
}
```

Create `internal/daemon/actions.go`:

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

const actionRunsPerRepo = 50

type coveredRepo struct {
	repo    string
	watched bool
}

// coveredRepos lists the configured repositories, paused and removing ones
// included, then the watched ones, each in config order.
func coveredRepos(cfg *config.Config) []coveredRepo {
	out := make([]coveredRepo, 0, len(cfg.Repos)+len(cfg.WatchRepos))
	for _, r := range cfg.Repos {
		out = append(out, coveredRepo{repo: r.Name})
	}
	for _, w := range cfg.WatchRepos {
		out = append(out, coveredRepo{repo: w, watched: true})
	}
	return out
}

// ghrRunKey identifies a workflow run ghr's runners took part in.
func ghrRunKey(repo string, runID int64) string {
	return strings.ToLower(repo) + "#" + strconv.FormatInt(runID, 10)
}

// actionsRun converts a GitHub run. It starts at created_at when GitHub left
// run_started_at out, and its title falls back to the workflow name.
func actionsRun(repo string, watched bool, r github.Run, ours map[string]bool) model.ActionsRun {
	start := r.CreatedAt
	if r.RunStartedAt != nil && !r.RunStartedAt.IsZero() {
		start = *r.RunStartedAt
	}
	title := r.DisplayTitle
	if title == "" {
		title = r.Name
	}
	return model.ActionsRun{
		Repo: repo, ID: r.ID, RunNumber: r.RunNumber, Workflow: r.Name, Title: title, Branch: r.HeadBranch,
		Event: r.Event, Actor: r.Actor.Login, Status: r.Status, Conclusion: r.Conclusion,
		StartedAt: start, UpdatedAt: r.UpdatedAt, HTMLURL: r.HTMLURL,
		GHR: ours[ghrRunKey(repo, r.ID)], Watched: watched,
	}
}

// actionsErr is the text, and the retry time when calls are paused, that a
// repository whose runs could not be read shows.
func actionsErr(err error, owner, repo string) (string, *time.Time) {
	var ae *api.Error
	if errors.As(err, &ae) {
		if ae.RetryAt.IsZero() {
			return ae.Msg, nil
		}
		at := ae.RetryAt
		return ae.Msg, &at
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "GitHub did not answer in time", nil
	}
	var ge *github.APIError
	if errors.As(err, &ge) {
		switch ge.Kind {
		case github.ErrNotFound:
			return fmt.Sprintf("token cannot see %s/%s: add it to the PAT's repository access first", owner, repo), nil
		case github.ErrRateLimit:
			at := ge.RetryAt
			return "GitHub rate limit; API calls are paused", &at
		case github.ErrAuth:
			return "GitHub rejected the token: " + ge.Error(), nil
		}
	}
	return err.Error(), nil
}

// keepsRuns reports whether a repository whose call failed with err keeps
// the runs of its last successful call: a paused, rate-limited or rejected
// token says nothing about the runs themselves.
func keepsRuns(err error) bool {
	var ae *api.Error
	if errors.As(err, &ae) {
		return true
	}
	return github.IsKind(err, github.ErrRateLimit) || github.IsKind(err, github.ErrAuth)
}

// Ties go by repository ignoring case, then ID, so the order is stable
// across polls.
func sortActions(rs []model.ActionsRun) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if ad, bd := a.Status == "completed", b.Status == "completed"; ad != bd {
			return !ad
		}
		if !a.StartedAt.Equal(b.StartedAt) {
			return a.StartedAt.After(b.StartedAt)
		}
		if ar, br := strings.ToLower(a.Repo), strings.ToLower(b.Repo); ar != br {
			return ar < br
		}
		return a.ID < b.ID
	})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ ./internal/model/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/daemon/actions.go internal/daemon/actions_test.go
git commit -m "feat(daemon): add the workflow run helpers"
```

### Task 6: Fetch and cache workflow runs

**Files:**
- Modify: `internal/daemon/actions.go` (append)
- Modify: `internal/daemon/backend.go` (the `Backend` struct, after `activity   map[string]*activityEntry` at line 101)
- Modify: `internal/daemon/backend_test.go` (`fakeGH` at line 93 and its `ListRecentRuns` at line 162)
- Test: `internal/daemon/actions_test.go`

**Interfaces:**
- Consumes: the Task 5 helpers and `testRun`, `Backend.WatchRepo` (Task 2), `Manager.Pending()` (`internal/daemon/backend.go:41`), `fakeManager.onPending` and `fakeManager.paused` / `degraded` (`internal/daemon/backend_test.go`)
- Produces: `Backend.Actions`, `actionsDeadline`, `actionsTTL` (Contracts, Go)

**Items:** 3

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 3 = 6

- [ ] **Step 1: Extend the fake GitHub client**

In `internal/daemon/backend_test.go`, add these fields to `fakeGH` directly after `cancelled int …`:

```go
	recentBy       map[string][]github.Run // ListRecentRuns answers by repo, when set
	recentErr      map[string]error
	recentWait     chan struct{} // when set, ListRecentRuns waits for it or the context
	recentCalls    []string      // guarded by lmu
	recentN        []int         // the n of each call, guarded by lmu
	recentInFlight int           // guarded by lmu
	recentPeak     int           // guarded by lmu
```

and replace `ListRecentRuns`:

```go
func (f *fakeGH) ListRecentRuns(ctx context.Context, repo string, n int) ([]github.Run, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.recentBy == nil && f.recentErr == nil && f.recentWait == nil {
		return f.recent, nil
	}
	f.lmu.Lock()
	f.recentCalls = append(f.recentCalls, repo)
	f.recentN = append(f.recentN, n)
	f.recentInFlight++
	f.recentPeak = max(f.recentPeak, f.recentInFlight)
	f.lmu.Unlock()
	defer func() {
		f.lmu.Lock()
		f.recentInFlight--
		f.lmu.Unlock()
	}()
	if f.recentWait != nil {
		select {
		case <-f.recentWait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := f.recentErr[repo]; err != nil {
		return nil, err
	}
	return f.recentBy[repo], nil
}
```

- [ ] **Step 2: Write the failing tests**

Add `"sync/atomic"` and `"github.com/darkraise/ghr/internal/history"` to the imports of `internal/daemon/actions_test.go`, then append:

```go
func setActionsDeadline(t *testing.T, d time.Duration) {
	old := actionsDeadline
	actionsDeadline = d
	t.Cleanup(func() { actionsDeadline = old })
}

// frozenClock gives b a clock the test moves; atomic, because builds read it
// from other goroutines.
func frozenClock(b *Backend, start time.Time) *atomic.Int64 {
	var ns atomic.Int64
	ns.Store(start.UnixNano())
	b.Now = func() time.Time { return time.Unix(0, ns.Load()).UTC() }
	return &ns
}

func recentCalls(gh *fakeGH) int {
	gh.lmu.Lock()
	defer gh.lmu.Unlock()
	return len(gh.recentCalls)
}

func waitInFlight(t *testing.T, gh *fakeGH, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		gh.lmu.Lock()
		n := gh.recentInFlight
		gh.lmu.Unlock()
		if n == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d calls in flight, want %d", n, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func runIDs(a model.Actions) string {
	var ids []int64
	for _, r := range a.Runs {
		ids = append(ids, r.ID)
	}
	return fmt.Sprint(ids)
}

var actionsT0 = time.Date(2026, 10, 9, 14, 0, 0, 0, time.UTC)

func TestActionsMergesCoveredRepos(t *testing.T) {
	b, m, gh := newBackend(t)
	ctx := context.Background()
	if err := b.WatchRepo(ctx, "public"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Store.Update(func(c *config.Config) error { c.Repo("darkcloud").Paused = true; return nil }); err != nil {
		t.Fatal(err)
	}
	frozenClock(b, actionsT0)
	noStart := testRun(3, "completed", "failure", actionsT0.Add(-time.Hour))
	noStart.RunStartedAt = nil
	gh.recentBy = map[string][]github.Run{
		"darkcloud": {testRun(1, "completed", "success", actionsT0.Add(-10*time.Minute))},
		"darkmem":   {testRun(2, "in_progress", "", actionsT0.Add(-2*time.Minute)), noStart},
		"public":    {testRun(4, "completed", "success", actionsT0.Add(-5*time.Minute))},
	}
	if err := b.Hist.Append(model.HistoryEntry{ID: "h", Repo: "DarkMem", RunID: 3, Conclusion: "failure",
		StartedAt: actionsT0.Add(-time.Hour), FinishedAt: actionsT0.Add(-50 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	m.pending = []model.HistoryEntry{{ID: "p", Repo: "darkmem", RunID: 2}}
	m.insts = []model.InstanceStatus{{ID: "i", Repo: "DARKCLOUD", State: "busy", Job: &model.JobInfo{RunID: 1}}}

	a, err := b.Actions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if runIDs(a) != "[2 4 1 3]" {
		t.Fatalf("order %s", runIDs(a))
	}
	for _, r := range a.Runs {
		if r.GHR != (r.ID != 4) || r.Watched != (r.ID == 4) {
			t.Errorf("run %d ghr %v watched %v", r.ID, r.GHR, r.Watched)
		}
	}
	if !a.Runs[3].StartedAt.Equal(actionsT0.Add(-time.Hour)) || !a.FetchedAt.Equal(actionsT0) {
		t.Fatalf("start %v fetched %v", a.Runs[3].StartedAt, a.FetchedAt)
	}
	want := []model.ActionsRepo{{Repo: "darkcloud"}, {Repo: "darkmem"}, {Repo: "public", Watched: true}}
	if len(a.Repos) != 3 || a.Repos[0] != want[0] || a.Repos[1] != want[1] || a.Repos[2] != want[2] {
		t.Fatalf("repos %+v", a.Repos)
	}
	if fmt.Sprint(gh.recentN) != "[50 50 50]" {
		t.Fatalf("asked for %v runs", gh.recentN)
	}
}

func TestActionsMarkAJobFinalizedMidBuild(t *testing.T) {
	b, m, gh := newBackend(t)
	gh.recentBy = map[string][]github.Run{"darkmem": {testRun(1, "completed", "success", time.Now().Add(-time.Hour))}}
	m.onPending = func() {
		if err := b.Hist.Append(model.HistoryEntry{ID: "p", Repo: "darkmem", RunID: 1, Conclusion: "success",
			StartedAt: time.Now().Add(-time.Hour), FinishedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := b.Actions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Runs) != 1 || !a.Runs[0].GHR {
		t.Fatalf("runs %+v", a.Runs)
	}
}

func TestActionsFailedHistoryMarksNothing(t *testing.T) {
	b, m, gh := newBackend(t)
	b.Hist = &history.Store{Path: t.TempDir()} // a directory: reading it fails
	m.pending = []model.HistoryEntry{{ID: "p", Repo: "darkmem", RunID: 1}}
	gh.recentBy = map[string][]github.Run{"darkmem": {testRun(1, "completed", "success", time.Now().Add(-time.Hour))}}
	a, err := b.Actions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Runs) != 1 || a.Runs[0].GHR {
		t.Fatalf("runs %+v", a.Runs)
	}
}

func TestActionsRepoErrorKeepsTheOthers(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.recentBy = map[string][]github.Run{"darkmem": {testRun(2, "completed", "success", time.Now().Add(-time.Hour))}}
	gh.recentErr = map[string]error{"darkcloud": &github.APIError{Status: 404, Kind: github.ErrNotFound}}
	a, err := b.Actions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runIDs(a) != "[2]" || a.Repos[0].Error != "token cannot see darkraise/darkcloud: add it to the PAT's repository access first" || a.Repos[1].Error != "" {
		t.Fatalf("runs %s repos %+v", runIDs(a), a.Repos)
	}
}

func TestActionsDeadlineOutlivesTheRequest(t *testing.T) {
	setActionsDeadline(t, 50*time.Millisecond)
	b, _, gh := newBackend(t)
	gh.recentBy = map[string][]github.Run{}
	gh.recentWait = make(chan struct{}) // never closed
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a, err := b.Actions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Repos) != 2 {
		t.Fatalf("repos %+v", a.Repos)
	}
	for _, r := range a.Repos {
		if r.Error != "GitHub did not answer in time" {
			t.Fatalf("repo %+v", r)
		}
	}
}

func TestActionsCacheFollowsTTLAndConfig(t *testing.T) {
	b, _, gh := newBackend(t)
	ctx := context.Background()
	clock := frozenClock(b, actionsT0)
	gh.recentBy = map[string][]github.Run{}
	for i, step := range []struct {
		at   time.Duration
		want int
	}{{0, 2}, {actionsTTL - time.Millisecond, 2}, {actionsTTL, 4}} {
		clock.Store(actionsT0.Add(step.at).UnixNano())
		if _, err := b.Actions(ctx); err != nil || recentCalls(gh) != step.want {
			t.Fatalf("step %d: %d calls, want %d, err %v", i, recentCalls(gh), step.want, err)
		}
	}
	if err := b.WatchRepo(ctx, "public"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Actions(ctx); err != nil || recentCalls(gh) != 7 {
		t.Fatalf("after a config change: %d calls, err %v", recentCalls(gh), err)
	}
}

func TestActionsCallersShareOneBuild(t *testing.T) {
	b, _, gh := newBackend(t)
	clock := frozenClock(b, actionsT0)
	gh.recentBy = map[string][]github.Run{}
	gh.recentWait = make(chan struct{})
	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			b.Actions(context.Background())
			done <- struct{}{}
		}()
	}
	waitInFlight(t, gh, 2)
	clock.Store(actionsT0.Add(actionsTTL + time.Second).UnixNano()) // the build outlasts the TTL
	close(gh.recentWait)
	<-done
	<-done
	if n := recentCalls(gh); n != 2 {
		t.Fatalf("%d calls, want 2: the waiting caller rebuilt instead of sharing", n)
	}
}

func TestActionsDegradedTokenMakesNoCalls(t *testing.T) {
	b, m, gh := newBackend(t)
	gh.recentBy = map[string][]github.Run{}
	m.degraded = "401 Bad credentials"
	a, err := b.Actions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recentCalls(gh) != 0 || len(a.Repos) != 2 {
		t.Fatalf("calls %d repos %+v", recentCalls(gh), a.Repos)
	}
	for _, r := range a.Repos {
		if r.Error != "GitHub is rejecting the token: 401 Bad credentials" || r.RetryAt != nil {
			t.Fatalf("repo %+v", r)
		}
	}
}

func TestActionsKeepRunsWhileGitHubIsPaused(t *testing.T) {
	b, m, gh := newBackend(t)
	ctx := context.Background()
	clock := frozenClock(b, actionsT0)
	gh.recentBy = map[string][]github.Run{"darkmem": {testRun(1, "completed", "success", actionsT0.Add(-time.Hour))}}
	if err := b.Hist.Append(model.HistoryEntry{ID: "h", Repo: "darkmem", RunID: 1, Conclusion: "success",
		StartedAt: actionsT0.Add(-time.Hour), FinishedAt: actionsT0.Add(-50 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	first, err := b.Actions(ctx)
	if err != nil || len(first.Runs) != 1 || !first.Runs[0].GHR {
		t.Fatalf("first %+v err %v", first, err)
	}
	clock.Store(actionsT0.Add(time.Minute).UnixNano())
	m.paused = actionsT0.Add(10 * time.Minute)
	a, err := b.Actions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if recentCalls(gh) != 2 || runIDs(a) != "[1]" || !a.FetchedAt.Equal(first.FetchedAt) {
		t.Fatalf("calls %d runs %s fetched %v", recentCalls(gh), runIDs(a), a.FetchedAt)
	}
	for _, r := range a.Repos {
		if r.Error != "GitHub rate limit; API calls are paused" || r.RetryAt == nil || !r.RetryAt.Equal(m.paused) {
			t.Fatalf("repo %+v", r)
		}
	}

	// Kept runs are marked again on every build: a failed history read now
	// marks nothing.
	clock.Store(actionsT0.Add(2 * time.Minute).UnixNano())
	b.Hist = &history.Store{Path: t.TempDir()}
	if a, _ := b.Actions(ctx); runIDs(a) != "[1]" || a.Runs[0].GHR {
		t.Fatalf("kept run after a failed history read %+v", a.Runs)
	}

	// A config change drops the kept runs.
	if err := b.WatchRepo(ctx, "public"); err != nil {
		t.Fatal(err)
	}
	if a, _ := b.Actions(ctx); runIDs(a) != "[]" {
		t.Fatalf("kept runs survived a config change: %s", runIDs(a))
	}
}

func TestActionsKeepRunsOnARateLimitedCall(t *testing.T) {
	b, _, gh := newBackend(t)
	ctx := context.Background()
	clock := frozenClock(b, actionsT0)
	gh.recentBy = map[string][]github.Run{
		"darkmem":   {testRun(1, "completed", "success", actionsT0.Add(-time.Hour))},
		"darkcloud": {testRun(5, "completed", "success", actionsT0.Add(-2*time.Hour))},
	}
	if _, err := b.Actions(ctx); err != nil {
		t.Fatal(err)
	}
	t1 := actionsT0.Add(time.Minute)
	clock.Store(t1.UnixNano())
	gh.recentBy["darkcloud"] = []github.Run{testRun(2, "completed", "success", t1.Add(-30*time.Minute))}
	gh.recentErr = map[string]error{"darkmem": &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: t1.Add(5 * time.Minute)}}
	a, err := b.Actions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if runIDs(a) != "[2 1]" || a.Repos[1].Error != "GitHub rate limit; API calls are paused" || !a.FetchedAt.Equal(t1) {
		t.Fatalf("runs %s repos %+v fetched %v", runIDs(a), a.Repos, a.FetchedAt)
	}

	t2 := t1.Add(time.Minute)
	clock.Store(t2.UnixNano())
	notFound := &github.APIError{Status: 404, Kind: github.ErrNotFound}
	gh.recentErr = map[string]error{"darkmem": notFound, "darkcloud": notFound}
	a, _ = b.Actions(ctx)
	if runIDs(a) != "[]" || !a.FetchedAt.Equal(t2) {
		t.Fatalf("an all-404 build kept runs %s or its old time %v", runIDs(a), a.FetchedAt)
	}
}

func TestActionsRunsAtMostFourCallsAtOnce(t *testing.T) {
	b, _, gh := newBackend(t)
	ctx := context.Background()
	for _, name := range []string{"w1", "w2", "w3", "w4"} {
		gh.repos[name] = &github.Repository{Private: true}
		if err := b.WatchRepo(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	gh.recentBy = map[string][]github.Run{}
	gh.recentWait = make(chan struct{})
	done := make(chan model.Actions, 1)
	go func() {
		a, _ := b.Actions(ctx)
		done <- a
	}()
	waitInFlight(t, gh, 4)
	time.Sleep(50 * time.Millisecond)
	close(gh.recentWait)
	a := <-done
	gh.lmu.Lock()
	peak, calls := gh.recentPeak, len(gh.recentCalls)
	gh.lmu.Unlock()
	if peak != 4 || calls != 6 || len(a.Repos) != 6 {
		t.Fatalf("peak %d calls %d repos %d", peak, calls, len(a.Repos))
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run 'TestActions'`
Expected: FAIL to compile with `b.Actions undefined` and `undefined: actionsDeadline`.

- [ ] **Step 4: Write the implementation**

In `internal/daemon/backend.go`, add to the `Backend` struct directly after `activity   map[string]*activityEntry`:

```go

	actions actionsCache
```

Add `"sync"` to the imports of `internal/daemon/actions.go`, then append:

```go
const (
	actionsTTL     = 15 * time.Second
	actionsWorkers = 4
)

// actionsDeadline bounds one /actions build; tests shorten it.
var actionsDeadline = 20 * time.Second

// good keeps each repository's runs from its last successful call, keyed by
// lower-case name, so a paused, rate-limited or rejected repository keeps
// showing them; a config change starts it over.
type actionsCache struct {
	mu     sync.Mutex
	cfg    *config.Config
	at     time.Time
	ok     bool
	val    model.Actions
	good   map[string][]model.ActionsRun
	goodAt time.Time
}

// Actions shares one build between callers, so the fetch runs detached from
// any one request and the cache age counts from when the build finished.
func (b *Backend) Actions(ctx context.Context) (model.Actions, error) {
	c := &b.actions
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg := b.Store.Config()
	if c.ok && c.cfg == cfg && b.now().Sub(c.at) < actionsTTL {
		return c.val, nil
	}
	if c.cfg != cfg {
		c.good, c.goodAt = map[string][]model.ActionsRun{}, time.Time{}
	}

	covered := coveredRepos(cfg)
	runs := make([][]github.Run, len(covered))
	errs := make([]error, len(covered))
	if skip := b.githubErr(); skip != nil {
		for i := range covered {
			errs[i] = skip
		}
	} else {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), actionsDeadline)
		sem := make(chan struct{}, actionsWorkers)
		var wg sync.WaitGroup
		for i, r := range covered {
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-fctx.Done():
					errs[i] = fctx.Err()
					return
				}
				defer func() { <-sem }()
				runs[i], errs[i] = b.GH.ListRecentRuns(fctx, r.repo, actionRunsPerRepo)
			}()
		}
		wg.Wait()
		cancel()
	}

	ours := b.ghrRuns()
	now := b.now()
	out := model.Actions{FetchedAt: now, Runs: []model.ActionsRun{}, Repos: make([]model.ActionsRepo, 0, len(covered))}
	// fetched_at keeps the last good build's time only while every
	// repository is served from kept runs.
	fresh, allKept := false, len(covered) > 0
	for i, r := range covered {
		row := model.ActionsRepo{Repo: r.repo, Watched: r.watched}
		key := strings.ToLower(r.repo)
		if errs[i] == nil {
			fresh, allKept = true, false
			got := make([]model.ActionsRun, 0, len(runs[i]))
			for _, run := range runs[i] {
				got = append(got, actionsRun(r.repo, r.watched, run, ours))
			}
			c.good[key] = got
			out.Runs = append(out.Runs, got...)
		} else {
			row.Error, row.RetryAt = actionsErr(errs[i], cfg.Owner, r.repo)
			if keepsRuns(errs[i]) {
				for _, k := range c.good[key] {
					k.GHR = ours[ghrRunKey(k.Repo, k.ID)]
					out.Runs = append(out.Runs, k)
				}
			} else {
				allKept = false
				delete(c.good, key)
			}
		}
		out.Repos = append(out.Repos, row)
	}
	if fresh {
		c.goodAt = now
	} else if allKept && !c.goodAt.IsZero() {
		out.FetchedAt = c.goodAt
	}
	sortActions(out.Runs)
	c.cfg, c.at, c.ok, c.val = cfg, now, true, out
	return out, nil
}

// ghrRuns reads live instances, then pending records, then history: a job
// moves through them in that order, so one that moves mid-read is still seen.
// A failed history read marks nothing.
func (b *Backend) ghrRuns() map[string]bool {
	insts := b.M.Status().Instances
	pend := b.M.Pending()
	hist, err := b.Hist.Query("", "", time.Time{}, 0)
	ours := map[string]bool{}
	if err != nil {
		return ours
	}
	for _, i := range insts {
		if i.Job != nil && i.Job.RunID != 0 {
			ours[ghrRunKey(i.Repo, i.Job.RunID)] = true
		}
	}
	for _, p := range pend {
		if p.RunID != 0 {
			ours[ghrRunKey(p.Repo, p.RunID)] = true
		}
	}
	for _, h := range hist {
		if h.RunID != 0 {
			ours[ghrRunKey(h.Repo, h.RunID)] = true
		}
	}
	return ours
}
```

`go.mod` declares Go 1.26, so the goroutine closure's `i` and `r` are per-iteration variables.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test -race ./internal/daemon/`
Expected: PASS, with no race reports. If `-race` is unavailable (it needs cgo and a C compiler: install `gcc`, or set `CGO_ENABLED=1`), run `timeout 300 go test ./internal/daemon/` and record in the ledger that the race check is owed; CI runs `go test -race ./...` on every pull request and push to `master` (`.github/workflows/ci.yml`).

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/actions.go internal/daemon/actions_test.go internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "feat(daemon): fetch and cache workflow runs"
```

### Task 7: Actions route and fixtures

**Files:**
- Modify: `internal/api/server.go` (the `Backend` interface; a route after `GET /activity` near line 115)
- Modify: `internal/api/api_test.go`
- Modify: `web/fixtures_test.go` (`fixtureValues()`, the `config` entry at line 149 and `available-repos` at line 194)
- Generated: `web/src/api/fixtures/actions.json`, `config.json`, `available-repos.json`

**Interfaces:**
- Consumes: `Backend.Actions`, `model.Actions` (Tasks 5 and 6)
- Produces: route `GET /actions`, the fixtures (Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

In `internal/api/api_test.go`, add two fields to `fakeBackend`:

```go
	actions       model.Actions
	actionsCalls  int
```

the method:

```go
func (f *fakeBackend) Actions(context.Context) (model.Actions, error) {
	f.actionsCalls++
	return f.actions, nil
}
```

and the test:

```go
func TestActionsRoute(t *testing.T) {
	c, b := setup(t)
	b.actions = model.Actions{Runs: []model.ActionsRun{{Repo: "darkmem", ID: 7}}, Repos: []model.ActionsRepo{{Repo: "darkmem"}}}
	var got model.Actions
	if err := c.call(context.Background(), http.MethodGet, "/actions", nil, &got); err != nil {
		t.Fatal(err)
	}
	if b.actionsCalls != 1 || len(got.Runs) != 1 || got.Runs[0].ID != 7 || len(got.Repos) != 1 {
		t.Fatalf("calls %d actions %+v", b.actionsCalls, got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 120 go test ./internal/api/ -run TestActionsRoute`
Expected: FAIL: "the running ghr daemon does not know GET /actions".

- [ ] **Step 3: Write the route**

In `internal/api/server.go`, add to the `Backend` interface after the `Activity(…)` line:

```go
	Actions(ctx context.Context) (model.Actions, error)
```

and register after the `GET /activity` handler:

```go
	mux.HandleFunc("GET /actions", func(w http.ResponseWriter, r *http.Request) {
		a, err := b.Actions(r.Context())
		respond(w, a, err)
	})
```

Run: `timeout 300 go test ./internal/api/ ./internal/daemon/`
Expected: PASS

- [ ] **Step 4: Add the fixtures**

In `web/fixtures_test.go`, in the `config` entry add `WatchRepos` after `Repos`:

```go
			WatchRepos: []string{"docs", "old-docs"},
```

replace the `available-repos` entry:

```go
		"available-repos": []model.AvailableRepo{
			{Name: "darkmem", Private: true, Configured: true},
			{Name: "docs", Watched: true},
			{Name: "new-repo", Private: true},
		},
```

and add an `actions` entry to the map, its runs in the endpoint's order (not completed first, newest start first):

```go
		"actions": model.Actions{
			FetchedAt: fixtureTime,
			Runs: []model.ActionsRun{
				{Repo: "docs", ID: 300, RunNumber: 9, Workflow: "pages", Title: "Publish the guide", Branch: "main",
					Event: "push", Actor: "darkraise", Status: "queued", StartedAt: at(-time.Minute),
					UpdatedAt: at(-time.Minute), HTMLURL: "https://github.com/darkraise/docs/actions/runs/300", Watched: true},
				{Repo: "darkmem", ID: 102, RunNumber: 42, Workflow: "ci", Title: "Fix the cache key", Branch: "master",
					Event: "push", Actor: "darkraise", Status: "in_progress", StartedAt: at(-4 * time.Minute),
					UpdatedAt: at(-time.Minute), HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/102", GHR: true},
				{Repo: "darkmem", ID: 101, RunNumber: 41, Workflow: "ci", Title: "Bump the runner", Branch: "renovate/runner",
					Event: "pull_request", Actor: "renovate[bot]", Status: "completed", Conclusion: "success",
					StartedAt: at(-30 * time.Minute), UpdatedAt: at(-25 * time.Minute),
					HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/101", GHR: true},
				{Repo: "darkcloud", ID: 90, RunNumber: 7, Workflow: "deploy", Title: "deploy", Branch: "master",
					Event: "workflow_dispatch", Actor: "darkraise", Status: "completed", Conclusion: "failure",
					StartedAt: at(-24 * time.Hour), UpdatedAt: at(-24*time.Hour + 90*time.Second),
					HTMLURL: "https://github.com/darkraise/darkcloud/actions/runs/90"},
			},
			Repos: []model.ActionsRepo{
				{Repo: "darkmem"},
				{Repo: "darkcloud"},
				{Repo: "docs", Watched: true},
				{Repo: "old-docs", Watched: true, Error: "GitHub rate limit; API calls are paused", RetryAt: ptr(at(25 * time.Minute))},
			},
		},
```

Regenerate and check: `GHR_UPDATE_FIXTURES=1 timeout 120 go test ./web/` then `timeout 120 go test ./web/`
Expected: both PASS; `git status` shows `web/src/api/fixtures/actions.json` new and `config.json`, `available-repos.json` changed.

- [ ] **Step 5: Run the web suite**

Run, in `web/`: `timeout 600 npm test -- src/components/add-repo-dialog.test.tsx src/pages/settings.test.tsx src/pages/repositories.test.tsx`
Expected: PASS. No existing test counts the picker's options, so the new `docs` entry and `watch_repos` change nothing they assert.

- [ ] **Step 6: Commit**

```bash
git add internal/api/server.go internal/api/api_test.go web/fixtures_test.go web/src/api/fixtures
git commit -m "feat(api): serve the workflow runs"
```

### Task 8: Actions types and test fixtures

**Files:**
- Modify: `web/src/api/types.ts` (`Config` near line 168, `AvailableRepo` near line 219; append the Actions types)
- Modify: `web/src/test/fixtures.ts`
- Test: `web/src/api/types.test.ts`

**Interfaces:**
- Consumes: the generated fixtures (Task 7)
- Produces: the TypeScript types, `fixtures.actions`, the `GET /api/actions` default route (Contracts, TypeScript)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

In `web/src/api/types.test.ts`, add `import actionsJson from "./fixtures/actions.json"` and `import type { Actions } from "./types"` next to the existing imports (match the file's import style), then add inside its top-level `describe`:

```ts
  it("actions parse with runs, repos and the watch list", () => {
    const actions: Actions = actionsJson
    expect(actions.runs.map((r) => r.id)).toEqual([300, 102, 101, 90])
    expect(actions.runs.find((r) => r.id === 102)?.ghr).toBe(true)
    expect(actions.runs.find((r) => r.id === 300)?.watched).toBe(true)
    expect(actions.repos[3]).toEqual({
      repo: "old-docs",
      watched: true,
      error: "GitHub rate limit; API calls are paused",
      retry_at: "2026-10-03T14:30:00Z",
    })
    expect(actions.repos[0]).not.toHaveProperty("error")
  })
```

and extend the existing config or available-repos assertions, or add:

```ts
  it("config carries the watch list and available repos say what is watched", () => {
    expect(fixtures.config.watch_repos).toEqual(["docs", "old-docs"])
    expect(fixtures.availableRepos.map((r) => [r.name, r.watched])).toEqual([
      ["darkmem", false],
      ["docs", true],
      ["new-repo", false],
    ])
    expect(fixtures.actions.repos).toHaveLength(4)
  })
```

(import `fixtures` from `@/test/fixtures` if the file does not already).

- [ ] **Step 2: Run the test to verify it fails**

Run, in `web/`: `timeout 300 npx vitest run src/api/types.test.ts` and `timeout 300 npm run typecheck`
Expected: typecheck FAILS with `Module '"./types"' has no exported member 'Actions'` and `Property 'actions' does not exist`.

- [ ] **Step 3: Write the implementation**

In `web/src/api/types.ts`, add to `Config` after `repos: RepoConfig[] | null`:

```ts
  watch_repos?: string[]
```

add to `AvailableRepo`:

```ts
  watched: boolean
```

and append:

```ts
export interface ActionsRun {
  repo: string
  id: number
  run_number: number
  workflow: string
  title: string
  branch: string
  event: string
  actor: string
  status: string
  conclusion: string
  started_at: string
  updated_at: string
  html_url: string
  ghr: boolean
  watched: boolean
}

export interface ActionsRepo {
  repo: string
  watched: boolean
  error?: string
  retry_at?: string
}

export interface Actions {
  fetched_at: string
  runs: ActionsRun[]
  repos: ActionsRepo[]
}
```

In `web/src/test/fixtures.ts`, add `import actionsJson from "@/api/fixtures/actions.json"` at the top of the JSON imports, add `Actions` to the type import, add `const actions: Actions = actionsJson`, add `actions` to the `fixtures` object, and add this line to `authedRoutes` before `...over`:

```ts
    "GET /api/actions": fixtures.actions,
```

- [ ] **Step 4: Run the tests to verify they pass**

Run, in `web/`: `timeout 300 npx vitest run src/api/types.test.ts`, `timeout 300 npm run typecheck`, `timeout 300 npm run lint`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/api/types.ts web/src/api/types.test.ts web/src/test/fixtures.ts
git commit -m "feat(web): add the Actions types"
```

### Task 9: Actions view helpers

**Files:**
- Create: `web/src/lib/actions.ts`
- Test: `web/src/lib/actions.test.ts`

**Interfaces:**
- Consumes: `ActionsRun`, `ActionsRepo` (Task 8), `resultWord` (`web/src/lib/history.ts:74`)
- Produces: the `web/src/lib/actions.ts` exports (Contracts, TypeScript)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/actions.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { ActionsRun } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import {
  actionsSummary,
  isActive,
  isQueued,
  parseActionsSearch,
  RECENT_LIMIT,
  repoErrorText,
  runMs,
  splitRuns,
  startText,
  statusWord,
} from "./actions"

const now = Date.parse("2026-10-03T14:05:00Z")
const runs = fixtures.actions.runs
const byId = (id: number): ActionsRun => {
  const r = runs.find((x) => x.id === id)
  if (!r) throw new Error(`no run ${id}`)
  return r
}
const run = (over: Partial<ActionsRun>): ActionsRun => ({ ...byId(101), ...over })

describe("parseActionsSearch", () => {
  it("keeps known values and drops the rest", () => {
    expect(parseActionsSearch({ repo: "darkmem", status: "failed" })).toEqual({ repo: "darkmem", status: "failed" })
    expect(parseActionsSearch({ repo: "", status: "bogus", other: 1 })).toEqual({})
  })
})

describe("run states", () => {
  it.each([
    ["queued", true, true, "Queued"],
    ["waiting", true, true, "Queued"],
    ["pending", true, true, "Queued"],
    ["requested", true, true, "Queued"],
    ["action_required", true, true, "Queued"],
    ["in_progress", true, false, "Running"],
  ])("reads %s", (status, active, queued, word) => {
    const r = run({ status, conclusion: "" })
    expect([isActive(r), isQueued(r), statusWord(r)]).toEqual([active, queued, word])
  })

  it("words a completed run by its result", () => {
    expect(statusWord(run({ conclusion: "success" }))).toBe("Succeeded")
    expect(statusWord(run({ conclusion: "timed_out" }))).toBe("Failed")
    expect(statusWord(run({ conclusion: "skipped" }))).toBe("Skipped")
  })
})

describe("splitRuns", () => {
  it("splits active from recent and filters by repository and status", () => {
    expect(splitRuns(runs, undefined, undefined)).toEqual({ active: [byId(300), byId(102)], recent: [byId(101), byId(90)] })
    expect(splitRuns(runs, "DarkMem", undefined)).toEqual({ active: [byId(102)], recent: [byId(101)] })
    expect(splitRuns(runs, undefined, "active")).toEqual({ active: [byId(300), byId(102)], recent: [] })
    expect(splitRuns(runs, undefined, "failed")).toEqual({ active: [], recent: [byId(90)] })
    expect(splitRuns(runs, undefined, "succeeded")).toEqual({ active: [], recent: [byId(101)] })
  })

  it("filters before it caps the recent runs", () => {
    const many = Array.from({ length: 150 }, (_, i) => run({ id: i, conclusion: i < 120 ? "success" : "failure" }))
    expect(splitRuns(many, undefined, undefined).recent).toHaveLength(RECENT_LIMIT)
    expect(splitRuns(many, undefined, "failed").recent).toHaveLength(30)
  })
})

describe("times", () => {
  it("measures a running run to now and a finished one to its update", () => {
    expect(runMs(byId(102), now)).toBe(4 * 60_000)
    expect(runMs(byId(101), now)).toBe(5 * 60_000)
  })

  it("shows the day before today", () => {
    expect(startText("2026-10-03T13:35:00Z", now)).toBe("13:35")
    expect(startText("2026-10-02T14:05:00Z", now)).toBe("Oct 2 14:05")
  })
})

describe("text", () => {
  it("sums up the runs across repositories or in one", () => {
    const at = new Date("2026-10-03T14:05:00Z")
    expect(actionsSummary(runs, 4, undefined, at)).toBe("1 running, 1 queued across 4 repositories. Updated 14:05.")
    expect(actionsSummary(runs, 1, undefined, at)).toBe("1 running, 1 queued across 1 repository. Updated 14:05.")
    expect(actionsSummary(runs, 4, "darkcloud", at)).toBe("0 running, 0 queued in darkcloud. Updated 14:05.")
    const waiting = [...runs, run({ id: 1, status: "action_required", conclusion: "" })]
    expect(actionsSummary(waiting, 4, undefined, at)).toBe("1 running, 2 queued across 4 repositories. Updated 14:05.")
  })

  it("words a repository's error like errorText", () => {
    expect(repoErrorText(fixtures.actions.repos[3]!)).toBe("old-docs: GitHub rate limit; API calls are paused. Retry after 14:30")
    expect(repoErrorText({ repo: "x", watched: false, error: "GitHub did not answer in time" })).toBe("x: GitHub did not answer in time")
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run, in `web/`: `timeout 300 npx vitest run src/lib/actions.test.ts`
Expected: FAIL with `Failed to resolve import "./actions"`.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/actions.ts`:

```ts
import type { ActionsRepo, ActionsRun } from "@/api/types"
import { hhmm, monthDay } from "./format"
import { resultWord } from "./history"

export const STATUS_FILTERS = [
  { value: "active", label: "Active" },
  { value: "failed", label: "Failed" },
  { value: "succeeded", label: "Succeeded" },
] as const
export type ActionsStatus = (typeof STATUS_FILTERS)[number]["value"]

export interface ActionsSearch {
  repo?: string
  status?: ActionsStatus
}

// Unknown values are dropped, so the page falls back to its defaults; the
// page also drops a repository that is neither configured nor watched.
export function parseActionsSearch(search: Record<string, unknown>): ActionsSearch {
  const out: ActionsSearch = {}
  if (typeof search.repo === "string" && search.repo !== "") out.repo = search.repo
  const status = STATUS_FILTERS.find((s) => s.value === search.status)
  if (status) out.status = status.value
  return out
}

export const isActive = (r: ActionsRun): boolean => r.status !== "completed"
// Only in_progress is running; queued, waiting, pending, requested and any
// status GitHub adds later wait.
export const isQueued = (r: ActionsRun): boolean => isActive(r) && r.status !== "in_progress"

export function statusWord(r: ActionsRun): string {
  if (!isActive(r)) return resultWord(r.conclusion)
  return isQueued(r) ? "Queued" : "Running"
}

function matchesRepo(r: ActionsRun, repo: string | undefined): boolean {
  return !repo || r.repo.toLowerCase() === repo.toLowerCase()
}

// Failed and Succeeded follow resultWord, so the filter agrees with the icon.
function matchesStatus(r: ActionsRun, status: ActionsStatus | undefined): boolean {
  if (!status) return true
  if (status === "active") return isActive(r)
  if (isActive(r)) return false
  return resultWord(r.conclusion) === (status === "failed" ? "Failed" : "Succeeded")
}

export const RECENT_LIMIT = 100

// The filter applies before the cap, so Failed still finds failures that
// sit behind 100 successes.
export function splitRuns(
  runs: ActionsRun[],
  repo: string | undefined,
  status: ActionsStatus | undefined,
): { active: ActionsRun[]; recent: ActionsRun[] } {
  const kept = runs.filter((r) => matchesRepo(r, repo) && matchesStatus(r, status))
  return { active: kept.filter(isActive), recent: kept.filter((r) => !isActive(r)).slice(0, RECENT_LIMIT) }
}

export function runMs(r: ActionsRun, now: number): number {
  const end = isActive(r) ? now : Date.parse(r.updated_at)
  return Math.max(0, end - Date.parse(r.started_at))
}

export function startText(iso: string, now: number): string {
  const d = new Date(iso)
  const today = new Date(now)
  const sameDay = d.getFullYear() === today.getFullYear() && d.getMonth() === today.getMonth() && d.getDate() === today.getDate()
  return sameDay ? hhmm(d) : `${monthDay(d)} ${hhmm(d)}`
}

export function actionsSummary(runs: ActionsRun[], repos: number, repo: string | undefined, fetchedAt: Date): string {
  const shown = runs.filter((r) => matchesRepo(r, repo))
  const running = shown.filter((r) => r.status === "in_progress").length
  const queued = shown.filter(isQueued).length
  const where = repo ? `in ${repo}` : `across ${repos} ${repos === 1 ? "repository" : "repositories"}`
  return `${running} running, ${queued} queued ${where}. Updated ${hhmm(fetchedAt)}.`
}

export function repoErrorText(r: ActionsRepo): string {
  const text = `${r.repo}: ${r.error ?? ""}`
  return r.retry_at ? `${text.replace(/\.$/, "")}. Retry after ${hhmm(new Date(r.retry_at))}` : text
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run, in `web/`: `timeout 300 npx vitest run src/lib/actions.test.ts`, `timeout 300 npm run typecheck`, `timeout 300 npm run lint`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/actions.ts web/src/lib/actions.test.ts
git commit -m "feat(web): add the Actions view helpers"
```

### Task 10: Day grouping for any row

**Files:**
- Modify: `web/src/lib/history.ts:43-58` (`DayGroup` and `groupByDay`)
- Test: `web/src/lib/history.test.ts`

**Interfaces:**
- Consumes: none
- Produces: `DayGroupOf<T>`, `groupByDayOf` (Contracts, TypeScript); `groupByDay` keeps its signature

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

In `web/src/lib/history.test.ts`, add `groupByDayOf` to the import from `./history`, then add:

```ts
describe("groupByDayOf", () => {
  it("groups any rows by the day its accessor reads", () => {
    const now = Date.parse("2026-10-03T14:05:00Z")
    const rows = [
      { id: 1, at: "2026-10-03T13:00:00Z" },
      { id: 2, at: "2026-10-03T09:00:00Z" },
      { id: 3, at: "2026-10-02T23:00:00Z" },
    ]
    const groups = groupByDayOf(rows, (r) => r.at, now)
    expect(groups.map((g) => [g.label, g.rows.map((r) => r.id)])).toEqual([
      ["Today", [1, 2]],
      ["Yesterday", [3]],
    ])
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run, in `web/`: `timeout 300 npx vitest run src/lib/history.test.ts`
Expected: FAIL with `groupByDayOf is not a function` (or not exported).

- [ ] **Step 3: Write the implementation**

In `web/src/lib/history.ts`, replace the `DayGroup` interface and `groupByDay` function with:

```ts
export interface DayGroupOf<T> {
  key: string
  label: string
  rows: T[]
}

export type DayGroup = DayGroupOf<HistoryEntry>

export function groupByDayOf<T>(rows: T[], at: (row: T) => string, now: number): DayGroupOf<T>[] {
  const out: DayGroupOf<T>[] = []
  for (const r of rows) {
    const iso = at(r)
    const key = dayKey(new Date(iso))
    const last = out.at(-1)
    if (last?.key === key) last.rows.push(r)
    else out.push({ key, label: dayLabel(iso, now), rows: [r] })
  }
  return out
}

export function groupByDay(rows: HistoryEntry[], now: number): DayGroup[] {
  return groupByDayOf(rows, (r) => r.finished_at, now)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run, in `web/`: `timeout 300 npx vitest run src/lib/history.test.ts src/pages/history.test.tsx`, `timeout 300 npm run typecheck`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/history.ts web/src/lib/history.test.ts
git commit -m "refactor(web): group any rows by day"
```

### Task 11: The Actions page

**Files:**
- Create: `web/src/pages/actions.tsx`
- Test: `web/src/pages/actions.test.tsx`
- Modify: `web/src/router.tsx` (imports; a route after `historyRoute`; the route tree)
- Modify: `web/src/components/shell.tsx:6,27-29` (icon import and the Operate items)
- Modify: `web/src/api/client.ts` (the `api` object)
- Modify: `web/src/api/hooks.ts` (constants, `keys`, a new hook)

**Interfaces:**
- Consumes: the Task 8 types and fixtures, the Task 9 helpers, `groupByDayOf` (Task 10)
- Produces: `api.actions`, `POLL_ACTIONS`, `keys.actions`, `useActions` (Contracts, TypeScript)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/pages/actions.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { setViewport } from "@/test/media"
import { renderApp } from "@/test/render"

const region = (name: string) => within(screen.getByRole("region", { name }))

describe("Actions page", () => {
  it("sums up the runs and is listed in the nav", async () => {
    const { calls } = mockApi(authedRoutes())
    renderApp("/actions")
    expect(await screen.findByRole("heading", { name: "Actions" })).toBeInTheDocument()
    expect(await screen.findByText("1 running, 1 queued across 4 repositories. Updated 14:05.")).toBeInTheDocument()
    expect((await screen.findAllByRole("link", { name: /^Actions/ })).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.path === "/api/actions")).toBe(true)
  })

  it("shows the runs in progress with their elapsed time", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: "In progress" })
    const progress = region("In progress")
    const link = progress.getByRole("link", { name: "Fix the cache key, darkmem #42, opens in a new tab" })
    expect(link).toHaveAttribute("href", "https://github.com/darkraise/darkmem/actions/runs/102")
    expect(link).toHaveAttribute("target", "_blank")
    expect(progress.getByText("Running")).toBeInTheDocument()
    expect(progress.getByRole("img", { name: "Queued" })).toBeInTheDocument()
    expect(progress.getByText(/^4m0\ds$/)).toBeInTheDocument()
  })

  it("ticks the elapsed time of a running run with the daemon's clock", { timeout: 10_000 }, async () => {
    let polls = 0
    mockApi(
      authedRoutes({
        "GET /api/status": () => ({ ...fixtures.status, now: polls++ < 1 ? fixtures.status.now : "2026-10-03T14:06:00Z" }),
      }),
    )
    renderApp("/actions")
    await screen.findByRole("region", { name: "In progress" })
    expect(await region("In progress").findByText(/^5m0\ds$/)).toBeInTheDocument()
    const done = within(region("Recent").getByRole("link", { name: /^Bump the runner/ }).closest("tr") as HTMLElement)
    expect(done.getByText("5m00s")).toBeInTheDocument()
  })

  it("groups the recent runs by day with their result, duration and the ghr mark", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: "Recent" })
    const recent = region("Recent")
    expect(recent.getByText("Today")).toBeInTheDocument()
    expect(recent.getByText("Yesterday")).toBeInTheDocument()
    const row = within(recent.getByRole("link", { name: /^Bump the runner/ }).closest("tr") as HTMLElement)
    expect(row.getByRole("img", { name: "Succeeded" })).toBeInTheDocument()
    expect(row.getByText("5m00s")).toBeInTheDocument()
    expect(row.getByText("ghr")).toHaveClass("font-mono")
    expect(row.getByText("renovate/runner")).toHaveClass("font-mono")
    const old = within(recent.getByRole("link", { name: /^deploy/ }).closest("tr") as HTMLElement)
    expect(old.getByText("Oct 2 14:05")).toBeInTheDocument()
    expect(old.queryByText("ghr")).toBeNull()
  })

  it("lists each repository that could not be read", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    expect(await screen.findByText("old-docs: GitHub rate limit; API calls are paused. Retry after 14:30")).toBeInTheDocument()
  })

  it("filters by status in the URL", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/actions")
    await screen.findByRole("region", { name: "Recent" })
    await user.click(screen.getByRole("radio", { name: "Failed" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ status: "failed" }))
    expect(screen.queryByRole("region", { name: "In progress" })).toBeNull()
    expect(region("Recent").getByRole("link", { name: /^deploy/ })).toBeInTheDocument()
    expect(region("Recent").queryByRole("link", { name: /^Bump the runner/ })).toBeNull()
  })

  it.each([
    ["only finished runs", fixtures.actions.runs.slice(2)],
    ["no runs at all", []],
  ])("says nothing is running when Active finds nothing, with %s", async (_label, runs) => {
    mockApi(authedRoutes({ "GET /api/actions": { ...fixtures.actions, runs } }))
    renderApp("/actions?status=active")
    expect(await screen.findByText("Nothing is running or queued.")).toBeInTheDocument()
  })

  it("filters by a configured or watched repository", async () => {
    mockApi(authedRoutes())
    renderApp("/actions?repo=darkcloud")
    expect(await screen.findByText("0 running, 0 queued in darkcloud. Updated 14:05.")).toBeInTheDocument()
    expect(region("Recent").getByRole("link", { name: /^deploy/ })).toBeInTheDocument()
    expect(region("Recent").queryByRole("link", { name: /^Bump the runner/ })).toBeNull()
  })

  it("picks a watched repository from the select", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/actions")
    await screen.findByRole("region", { name: "Recent" })
    await user.click(screen.getByRole("combobox", { name: "Repository" }))
    const options = (await screen.findAllByRole("option")).map((o) => o.textContent)
    expect(options).toEqual(["All repositories", "darkmem", "darkcloud", "docs (watched)", "old-docs (watched)"])
    await user.click(screen.getByRole("option", { name: "docs (watched)" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ repo: "docs" }))
    expect(screen.getByText("0 running, 1 queued in docs. Updated 14:05.")).toBeInTheDocument()
    expect(region("In progress").getByRole("link", { name: /^Publish the guide/ })).toBeInTheDocument()
    expect(region("In progress").queryByRole("link", { name: /^Fix the cache key/ })).toBeNull()
    expect(screen.queryByRole("region", { name: "Recent" })).toBeNull()
  })

  it("drops values it does not know, from the page and the URL", async () => {
    mockApi(authedRoutes())
    const { router } = renderApp("/actions?repo=nope&status=bogus")
    expect(await screen.findByText("1 running, 1 queued across 4 repositories. Updated 14:05.")).toBeInTheDocument()
    await waitFor(() => expect(router.state.location.search).toEqual({}))
    expect(router.state.location.href).toBe("/actions")
  })

  it("offers a way back when the filters hide everything", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/actions?repo=docs&status=failed")
    expect(await screen.findByText("No runs match these filters.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Clear filters" }))
    await waitFor(() => expect(router.state.location.search).toEqual({}))
  })

  it("explains an empty page", async () => {
    mockApi(authedRoutes({ "GET /api/actions": { fetched_at: "2026-10-03T14:05:00Z", runs: [], repos: [] } }))
    renderApp("/actions")
    expect(await screen.findByText("No repositories are covered yet.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Add a repository" })).toHaveAttribute("href", "/repositories")
    expect(screen.getByRole("link", { name: "Watch a repository" })).toHaveAttribute("href", "/repositories#watched")
  })

  it("says when the covered repositories have no runs", async () => {
    mockApi(authedRoutes({ "GET /api/actions": { ...fixtures.actions, runs: [] } }))
    renderApp("/actions")
    expect(await screen.findByText("No workflow runs in the covered repositories yet.")).toBeInTheDocument()
  })

  it("gives duration and start their own columns from 768px", async () => {
    setViewport(1280)
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: "Recent" })
    expect(region("Recent").getByRole("columnheader", { name: "Duration" })).toBeInTheDocument()
  })

  it("keeps duration under the facts below 768px", async () => {
    mockApi(authedRoutes())
    renderApp("/actions")
    await screen.findByRole("region", { name: "Recent" })
    expect(region("Recent").queryByRole("columnheader", { name: "Duration" })).toBeNull()
    expect(region("Recent").getByText("5m00s")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run, in `web/`: `timeout 300 npx vitest run src/pages/actions.test.tsx`
Expected: FAIL: no heading "Actions" (the route does not exist, so the app shows its not-found page).

- [ ] **Step 3: Add the query**

In `web/src/api/client.ts`, add `Actions` to the type import from `./types`, and add to the `api` object after `activity`:

```ts
  actions: (signal?: AbortSignal) => request<Actions>("GET", "/api/actions", undefined, signal),
```

In `web/src/api/hooks.ts`, add after `POLL_SLOW`:

```ts
// GET /actions caches for 15 seconds, so polling faster would read the same build.
export const POLL_ACTIONS = 15_000
```

add to `keys` after `metrics`:

```ts
  actions: ["actions"] as const,
```

and add after `useMetrics`:

```ts
export function useActions() {
  return useQuery({ queryKey: keys.actions, queryFn: ({ signal }) => api.actions(signal), refetchInterval: POLL_ACTIONS })
}
```

- [ ] **Step 4: Write the page**

Create `web/src/pages/actions.tsx`:

```tsx
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import { PageHeader } from "darkraise-ui/layout"
import { Clock } from "lucide-react"
import { useEffect } from "react"
import { useActions, useConfig } from "@/api/hooks"
import type { ActionsRun } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { ResultIcon } from "@/components/result-icon"
import {
  actionsSummary,
  isActive,
  isQueued,
  repoErrorText,
  runMs,
  splitRuns,
  startText,
  STATUS_FILTERS,
  type ActionsSearch,
  type ActionsStatus,
} from "@/lib/actions"
import { dur } from "@/lib/format"
import { groupByDayOf } from "@/lib/history"
import { useMediaQuery } from "@/lib/use-media-query"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const ALL = "__all__"

function StatusIcon({ run }: { run: ActionsRun }) {
  if (!isActive(run)) return <ResultIcon conclusion={run.conclusion} />
  if (isQueued(run)) return <Clock role="img" aria-label="Queued" size={15} className="shrink-0 text-muted-foreground" />
  return <Spinner size="sm" label={<span className="sr-only">Running</span>} />
}

function RunRow({ run, now, wide }: { run: ActionsRun; now: number; wide: boolean }) {
  const took = dur(runMs(run, now))
  const start = startText(run.started_at, now)
  return (
    <TableRow>
      <TableCell className="w-8 align-top">
        <StatusIcon run={run} />
      </TableCell>
      <TableCell className="align-top">
        <a href={run.html_url} target="_blank" rel="noreferrer" className="font-medium hover:underline">
          {run.title}
          <span className="sr-only">{`, ${run.repo} #${run.run_number}, opens in a new tab`}</span>
        </a>
        <span className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
          <span>{run.workflow}</span>
          <span>{run.repo}</span>
          <span className="font-mono">#{run.run_number}</span>
          {run.branch && <span className="font-mono">{run.branch}</span>}
          {run.event && <span>{run.event}</span>}
          {run.actor && <span>{run.actor}</span>}
        </span>
        {!wide && (
          <span className="mt-1 flex flex-wrap gap-x-3 text-xs text-muted-foreground">
            <span className="font-mono">{took}</span>
            <span className="font-mono">{start}</span>
            {run.ghr && <span className="font-mono">ghr</span>}
          </span>
        )}
      </TableCell>
      {wide && (
        <>
          <TableCell className="text-right align-top font-mono">{took}</TableCell>
          <TableCell className="text-right align-top font-mono">{start}</TableCell>
          <TableCell className="text-right align-top font-mono text-muted-foreground">{run.ghr ? "ghr" : ""}</TableCell>
        </>
      )}
    </TableRow>
  )
}

function RunHead({ wide }: { wide: boolean }) {
  return (
    <TableHeader>
      <TableRow>
        <TableHead>
          <span className="sr-only">Status</span>
        </TableHead>
        <TableHead>Run</TableHead>
        {wide && (
          <>
            <TableHead className="text-right">Duration</TableHead>
            <TableHead className="text-right">Started</TableHead>
            <TableHead>
              <span className="sr-only">Runner</span>
            </TableHead>
          </>
        )}
      </TableRow>
    </TableHeader>
  )
}

export function ActionsPage() {
  const search = useSearch({ from: "/app/actions" })
  const navigate = useNavigate()
  const config = useConfig()
  const actions = useActions()
  const now = useNow()
  const wide = useMediaQuery("(min-width: 768px)")

  const configured = (config.data?.repos ?? []).map((r) => r.name)
  const watched = config.data?.watch_repos ?? []
  const names = [...configured, ...watched]
  const repo = names.find((n) => n.toLowerCase() === (search.repo ?? "").toLowerCase())
  const status: ActionsStatus | undefined = search.status

  // A repository that is neither configured nor watched leaves the URL once
  // the config has loaded, so the address bar matches the page.
  useEffect(() => {
    if (config.data && search.repo && !repo) void navigate({ to: "/actions", search: status ? { status } : {}, replace: true })
  }, [config.data, search.repo, repo, status, navigate])

  function update(next: { repo?: string; status?: ActionsStatus | "" }) {
    const merged = { repo: repo ?? "", status: status ?? "", ...next }
    const out: ActionsSearch = {}
    if (merged.repo) out.repo = merged.repo
    if (merged.status) out.status = merged.status
    void navigate({ to: "/actions", search: out })
  }

  const data = actions.data
  const { active, recent } = data ? splitRuns(data.runs, repo, status) : { active: [], recent: [] }
  const failed = data?.repos.filter((r) => r.error) ?? []
  const cols = wide ? 5 : 2

  let body
  if (actions.isError && !data) body = <ErrorLine onRetry={() => void actions.refetch()}>{errorText(actions.error)}</ErrorLine>
  else if (!data) body = <Spinner label="Loading" />
  else if (data.repos.length === 0) {
    body = (
      <div className="flex flex-col items-start gap-2">
        <p className="text-sm text-muted-foreground">No repositories are covered yet.</p>
        <div className="flex flex-wrap gap-3 text-sm">
          <Link to="/repositories" className="text-primary hover:underline">
            Add a repository
          </Link>
          <Link to="/repositories" hash="watched" className="text-primary hover:underline">
            Watch a repository
          </Link>
        </div>
      </div>
    )
  } else if (status !== "active" && data.runs.length === 0) {
    body = <p className="text-sm text-muted-foreground">No workflow runs in the covered repositories yet.</p>
  } else if (status !== "active" && active.length === 0 && recent.length === 0) {
    body = (
      <div className="flex flex-col items-start gap-2">
        <p className="text-sm text-muted-foreground">No runs match these filters.</p>
        <Button size="sm" variant="outline" onClick={() => update({ repo: "", status: "" })}>
          Clear filters
        </Button>
      </div>
    )
  } else {
    body = (
      <>
        {(active.length > 0 || status === "active") && (
          <Section title="In progress">
            {active.length === 0 ? (
              <p className="text-sm text-muted-foreground">Nothing is running or queued.</p>
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <RunHead wide={wide} />
                  <TableBody>
                    {active.map((r) => (
                      <RunRow key={`${r.repo}#${r.id}`} run={r} now={now} wide={wide} />
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </Section>
        )}
        {recent.length > 0 && (
          <Section title="Recent">
            <div className="overflow-x-auto">
              <Table>
                <RunHead wide={wide} />
                {groupByDayOf(recent, (r) => r.started_at, now).map((g) => (
                  <TableBody key={g.key}>
                    <TableRow>
                      <th scope="rowgroup" colSpan={cols} className="pt-4 pb-1 text-left text-sm font-medium text-muted-foreground">
                        {g.label}
                      </th>
                    </TableRow>
                    {g.rows.map((r) => (
                      <RunRow key={`${r.repo}#${r.id}`} run={r} now={now} wide={wide} />
                    ))}
                  </TableBody>
                ))}
              </Table>
            </div>
          </Section>
        )}
      </>
    )
  }

  const watchLink = (
    <Link to="/repositories" hash="watched" className="text-sm text-primary hover:underline">
      Watch more repositories
    </Link>
  )
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Actions"
        description={data ? actionsSummary(data.runs, data.repos.length, repo, new Date(data.fetched_at)) : undefined}
        actions={watchLink}
      />
      <div className="flex flex-wrap items-center gap-3">
        <Select value={repo ?? ALL} onValueChange={(v) => update({ repo: v === ALL ? "" : v })}>
          <SelectTrigger aria-label="Repository" className="w-56">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All repositories</SelectItem>
            {configured.map((name) => (
              <SelectItem key={name} value={name}>
                {name}
              </SelectItem>
            ))}
            {watched.map((name) => (
              <SelectItem key={name} value={name}>
                {`${name} (watched)`}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={status ?? ALL}
          aria-label="Status"
          onValueChange={(v) => update({ status: STATUS_FILTERS.find((s) => s.value === v)?.value ?? "" })}
        >
          <ToggleGroupItem value={ALL}>All</ToggleGroupItem>
          {STATUS_FILTERS.map((s) => (
            <ToggleGroupItem key={s.value} value={s.value}>
              {s.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      {failed.length > 0 && (
        <Alert variant="warning">
          <AlertTitle>Some repositories could not be read</AlertTitle>
          <AlertDescription>
            <ul className="flex flex-col gap-0.5">
              {failed.map((r) => (
                <li key={r.repo}>{repoErrorText(r)}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}
      {body}
    </div>
  )
}
```

- [ ] **Step 5: Register the route and the nav item**

In `web/src/router.tsx`, add the imports:

```ts
import { parseActionsSearch, type ActionsSearch } from "./lib/actions"
import { ActionsPage } from "./pages/actions"
```

add after `historyRoute`:

```ts
const actionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/actions",
  // As History: naming the keys keeps a value the parser dropped from coming back.
  validateSearch: (search: Record<string, unknown>): ActionsSearch => ({
    repo: undefined,
    status: undefined,
    ...parseActionsSearch(search),
  }),
  component: ActionsPage,
})
```

and add `actionsRoute,` to `appRoute.addChildren([…])` directly before `historyRoute,`.

In `web/src/components/shell.tsx`, add `Workflow` to the `lucide-react` import and insert the item between Runners and History:

```ts
        { label: "Actions", href: "/actions", icon: Workflow },
```

- [ ] **Step 6: Run the tests to verify they pass**

Run, in `web/`: `timeout 300 npx vitest run src/pages/actions.test.tsx src/components/shell.test.tsx src/router.test.tsx`, `timeout 300 npm run typecheck`, `timeout 300 npm run lint`
Expected: PASS. (`toEqual` ignores the `undefined` keys that `validateSearch` names, so `{ repo: undefined, status: undefined }` equals `{}`.)

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/actions.tsx web/src/pages/actions.test.tsx web/src/router.tsx web/src/components/shell.tsx web/src/api/client.ts web/src/api/hooks.ts
git commit -m "feat(web): add the Actions page"
```

### Task 12: Shared repository picker

**Files:**
- Create: `web/src/components/repo-picker.tsx`
- Modify: `web/src/components/add-repo-dialog.tsx`
- Test: `web/src/components/add-repo-dialog.test.tsx`

**Interfaces:**
- Consumes: `AvailableRepo.watched` (Task 8)
- Produces: `RepoPicker` (Contracts, TypeScript)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

In `web/src/components/add-repo-dialog.test.tsx`, add inside `describe("Add repository dialog", …)`:

```tsx
  it("keeps a watched repo pickable, since adding takes it over", async () => {
    mockApi(routes())
    const { dialog } = await open()
    expect(dialog.getByRole("option", { name: /docs/ })).toBeEnabled()
    expect(dialog.getByText("watched")).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run the test to verify it fails**

Run, in `web/`: `timeout 300 npx vitest run src/components/add-repo-dialog.test.tsx`
Expected: FAIL: unable to find the text "watched".

- [ ] **Step 3: Write the picker**

Create `web/src/components/repo-picker.tsx`:

```tsx
import { Input } from "darkraise-ui/components/input"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState, type KeyboardEvent } from "react"
import { useAvailableRepos } from "@/api/hooks"
import type { AvailableRepo } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { errorText } from "@/query"

const KEY_STEP: Record<string, (i: number, n: number) => number> = {
  ArrowDown: (i, n) => Math.min(n - 1, i + 1),
  ArrowUp: (i) => Math.max(0, i - 1),
  Home: () => 0,
  End: (_i, n) => n - 1,
}

function moveFocus(e: KeyboardEvent<HTMLDivElement>) {
  const step = KEY_STEP[e.key]
  if (!step) return
  e.preventDefault()
  const options = Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="option"]:not(:disabled)'))
  if (options.length === 0) return
  const at = options.findIndex((o) => o === document.activeElement)
  options[step(at, options.length)]?.focus()
}

// The add dialog keeps watched entries pickable, because adding a watched
// repository takes it over; the watch dialog disables them.
export function RepoPicker({
  picked,
  onPick,
  disableWatched = false,
}: {
  picked: string
  onPick: (name: string) => void
  disableWatched?: boolean
}) {
  const repos = useAvailableRepos(true)
  const [filter, setFilter] = useState("")
  const off = (r: AvailableRepo) => r.configured || (disableWatched && r.watched)
  const needle = filter.trim().toLowerCase()
  const items = (repos.data ?? []).filter((r) => r.name.toLowerCase().includes(needle))
  const tabStop = items.find((r) => r.name === picked && !off(r))?.name ?? items.find((r) => !off(r))?.name

  if (repos.isError) return <ErrorLine onRetry={() => void repos.refetch()}>{errorText(repos.error)}</ErrorLine>
  if (!repos.data) return <Spinner label="Loading repositories" />
  return (
    <div className="flex flex-col gap-2">
      <Input aria-label="Filter repositories" placeholder="Type to filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
      {repos.data.length === 0 && <p className="text-sm text-muted-foreground">Nothing to pick</p>}
      {repos.data.length > 0 && items.length === 0 && <p className="text-sm text-muted-foreground">No match</p>}
      {items.length > 0 && (
        <div role="listbox" aria-label="Repositories" className="max-h-64 overflow-auto rounded-md border" onKeyDown={moveFocus}>
          {items.map((r) => (
            <button
              key={r.name}
              type="button"
              role="option"
              aria-selected={picked === r.name}
              disabled={off(r)}
              tabIndex={r.name === tabStop ? 0 : -1}
              onClick={() => onPick(r.name)}
              className="flex w-full items-center gap-2 px-2 py-1 text-left text-sm hover:bg-muted focus-visible:bg-muted focus-visible:outline-none disabled:opacity-50 aria-selected:bg-muted"
            >
              <span>{r.name}</span>
              {!r.private && <span className="text-muted-foreground">public</span>}
              {r.configured && <span className="text-muted-foreground">added</span>}
              {!r.configured && r.watched && <span className="text-muted-foreground">watched</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 4: Use it in the add dialog**

In `web/src/components/add-repo-dialog.tsx`:
- in `AddRepoForm`, delete the line `const repos = useAvailableRepos(true)`, the `filter` state, the `needle`, `items` and `tabStop` constants, and the whole `let picker …` block;
- delete the module-level `KEY_STEP` and `moveFocus`;
- change the imports: drop `Spinner`, `useAvailableRepos` and `type KeyboardEvent` (keep `useState`), and add `import { RepoPicker } from "@/components/repo-picker"`;
- replace `{picker}` in the JSX with:

```tsx
        <RepoPicker picked={picked} onPick={setPicked} />
```

The hook import line becomes `import { keys, useStatus } from "@/api/hooks"`.

- [ ] **Step 5: Run the tests to verify they pass**

Run, in `web/`: `timeout 300 npx vitest run src/components/add-repo-dialog.test.tsx src/pages/repositories.test.tsx`, `timeout 300 npm run typecheck`, `timeout 300 npm run lint`
Expected: PASS, including every earlier add-dialog test (filtering, keyboard focus, disabled configured entries).

- [ ] **Step 6: Commit**

```bash
git add web/src/components/repo-picker.tsx web/src/components/add-repo-dialog.tsx web/src/components/add-repo-dialog.test.tsx
git commit -m "refactor(web): extract the repository picker"
```

### Task 13: Watched repositories section

**Files:**
- Create: `web/src/components/watch-repo-dialog.tsx`
- Modify: `web/src/pages/repositories.tsx`
- Modify: `web/src/components/page/section.tsx`
- Modify: `web/src/api/client.ts` (the `api` object)
- Test: `web/src/pages/repositories.test.tsx`

**Interfaces:**
- Consumes: `RepoPicker` (Task 12), `keys.actions` (Task 11), `Config.watch_repos` (Task 8)
- Produces: `api.watchRepo`, `api.unwatchRepo`, `Section`'s `id` prop (Contracts, TypeScript)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/repositories.test.tsx`, add `vi` to the `vitest` import and add inside `describe("Repositories page", …)`:

```tsx
  it("lists the watched repositories", async () => {
    mockApi(authedRoutes())
    renderApp("/repositories")
    const watched = within(await screen.findByRole("region", { name: "Watched repositories" }))
    expect(watched.getByText("Their workflow runs show on the Actions page. ghr runs no runners for them.")).toBeInTheDocument()
    expect(watched.getByText("docs")).toHaveClass("font-mono")
    expect(watched.getByRole("button", { name: "Stop watching old-docs" })).toBeEnabled()
    expect(document.getElementById("watched")).not.toBeNull()
  })

  it("stops watching a repository", async () => {
    const { calls } = mockApi(authedRoutes({ "DELETE /api/watch/old-docs": () => noContent() }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "Stop watching old-docs" }))
    expect((await screen.findAllByText("Stopped watching old-docs")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/watch/old-docs")).toBe(true)
  })

  it("watches a repository from the picker, with watched ones not pickable", async () => {
    const { calls } = mockApi(
      authedRoutes({ "GET /api/repos/available": fixtures.availableRepos, "POST /api/watch": () => noContent() }),
    )
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "Watch a repository" }))
    const dialog = within(await screen.findByRole("dialog"))
    expect(await dialog.findByRole("option", { name: /docs/ })).toBeDisabled()
    expect(dialog.getByRole("option", { name: /darkmem/ })).toBeDisabled()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Watch" }))
    expect((await screen.findAllByText("Watching new-repo")).length).toBeGreaterThan(0)
    expect(calls.find((c) => c.method === "POST" && c.path === "/api/watch")?.body).toEqual({ name: "new-repo" })
  })

  it("scrolls to the watched section from a link", async () => {
    const spy = vi.spyOn(Element.prototype, "scrollIntoView")
    mockApi(authedRoutes())
    renderApp("/repositories#watched")
    await screen.findByRole("region", { name: "Watched repositories" })
    await waitFor(() => expect(spy.mock.contexts).toContain(document.getElementById("watched")))
    spy.mockRestore()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run, in `web/`: `timeout 300 npx vitest run src/pages/repositories.test.tsx`
Expected: FAIL: no region "Watched repositories".

- [ ] **Step 3: Give Section an id and add the API calls**

Replace `web/src/components/page/section.tsx`:

```tsx
import { useId, type ReactNode } from "react"

export function Section({
  id,
  title,
  aside,
  children,
  className = "",
}: {
  id?: string
  title: string
  aside?: ReactNode
  children: ReactNode
  className?: string
}) {
  const headingId = useId()
  return (
    <section id={id} aria-labelledby={headingId} className={`min-w-0 rounded-[10px] border border-border bg-card p-4 ${className}`}>
      <div className="mb-3 flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 id={headingId} className="text-base font-semibold">
          {title}
        </h2>
        {aside}
      </div>
      {children}
    </section>
  )
}
```

In `web/src/api/client.ts`, add to the `api` object after `removeRepo`:

```ts
  watchRepo: (name: string) => send("POST", "/api/watch", { name }),
  unwatchRepo: (name: string) => send("DELETE", `/api/watch/${seg(name)}`),
```

- [ ] **Step 4: Write the dialog**

Create `web/src/components/watch-repo-dialog.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "darkraise-ui/components/dialog"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import { ErrorLine } from "@/components/page/error-line"
import { RepoPicker } from "@/components/repo-picker"
import { errorText } from "@/query"

export function WatchRepoDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose()
      }}
    >
      <DialogContent>{open && <WatchRepoForm onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function WatchRepoForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient()
  const [picked, setPicked] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function watch() {
    if (!picked) return
    setBusy(true)
    setError("")
    try {
      await api.watchRepo(picked)
    } catch (err) {
      setError(errorText(err))
      return
    } finally {
      setBusy(false)
    }
    toast.success(`Watching ${picked}`)
    void queryClient.invalidateQueries({ queryKey: keys.config })
    void queryClient.invalidateQueries({ queryKey: keys.actions })
    onClose()
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Watch a repository</DialogTitle>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">Its workflow runs show on the Actions page. ghr runs no runners for it.</p>
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium">Repository</span>
          {picked ? <strong>{picked}</strong> : <span className="text-sm text-muted-foreground">Pick one below</span>}
        </div>
        <RepoPicker picked={picked} onPick={setPicked} disableWatched />
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} disabled={!picked || busy} onClick={() => void watch()}>
          Watch
        </Button>
      </DialogFooter>
    </>
  )
}
```

- [ ] **Step 5: Add the section to the Repositories page**

Replace `web/src/pages/repositories.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useLocation, useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { PageHeader } from "darkraise-ui/layout"
import { X } from "lucide-react"
import { useEffect, useState } from "react"
import { api } from "@/api/client"
import { keys, useActivity, useConfig, useStatus } from "@/api/hooks"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { Section } from "@/components/page/section"
import { RepoTable } from "@/components/repo-table"
import { WatchRepoDialog } from "@/components/watch-repo-dialog"
import { reposSummary } from "@/lib/repos"
import { useNow } from "@/lib/use-now"

function WatchedSection({ names, offline, onWatch }: { names: string[]; offline: boolean; onWatch: () => void }) {
  const queryClient = useQueryClient()
  const remove = useMutation({
    mutationFn: (name: string) => api.unwatchRepo(name),
    onSuccess: (_data, name) => {
      toast.success(`Stopped watching ${name}`)
      void queryClient.invalidateQueries({ queryKey: keys.config })
      void queryClient.invalidateQueries({ queryKey: keys.actions })
    },
  })
  return (
    <Section
      id="watched"
      title="Watched repositories"
      aside={
        <Button size="sm" variant="outline" disabled={offline} onClick={onWatch}>
          Watch a repository
        </Button>
      }
    >
      <p className="text-sm text-muted-foreground">Their workflow runs show on the Actions page. ghr runs no runners for them.</p>
      {names.length > 0 && (
        <ul className="mt-3 flex flex-col divide-y divide-border">
          {names.map((name) => (
            <li key={name} className="flex items-center justify-between gap-2 py-1.5">
              <span className="font-mono text-sm">{name}</span>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label={`Stop watching ${name}`}
                    disabled={offline || remove.isPending}
                    onClick={() => remove.mutate(name)}
                  >
                    <X size={15} aria-hidden="true" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>Stop watching</TooltipContent>
              </Tooltip>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

export function RepositoriesPage() {
  const status = useStatus()
  const config = useConfig()
  const activity = useActivity("24h")
  const now = useNow()
  const navigate = useNavigate()
  const hash = useLocation({ select: (l) => l.hash })
  const [adding, setAdding] = useState(false)
  const [watching, setWatching] = useState(false)

  const st = status.data
  const ready = st !== undefined
  // Scrolled here rather than by the router, so a link to #watched works
  // whatever the router does with hashes.
  useEffect(() => {
    if (ready && hash.replace(/^#/, "") === "watched") document.getElementById("watched")?.scrollIntoView()
  }, [ready, hash])

  if (!st) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader title="Repositories" />
        <Spinner label="Waiting for the daemon" />
      </div>
    )
  }
  const offline = status.isError
  const add = (
    <Button disabled={offline} onClick={() => setAdding(true)}>
      Add repository
    </Button>
  )
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Repositories" description={reposSummary(st) || undefined} actions={add} />
      {st.repos.length === 0 ? (
        <div className="flex flex-col items-start gap-2">
          <p className="text-sm text-muted-foreground">No repositories yet</p>
          {add}
        </div>
      ) : (
        <Section title="Configured repositories">
          <div className="overflow-x-auto">
            <RepoTable
              repos={st.repos}
              activity={activity.data?.repos}
              now={now}
              offline={offline}
              onOpen={(name) => void navigate({ to: "/repositories/$name", params: { name } })}
              columns="full"
              config={config.data}
            />
          </div>
        </Section>
      )}
      <WatchedSection names={config.data?.watch_repos ?? []} offline={offline} onWatch={() => setWatching(true)} />
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
      <WatchRepoDialog open={watching} onClose={() => setWatching(false)} />
    </div>
  )
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run, in `web/`: `timeout 300 npx vitest run src/pages/repositories.test.tsx src/components/page/section.test.tsx src/components/add-repo-dialog.test.tsx`, `timeout 300 npm run typecheck`, `timeout 300 npm run lint`
Expected: PASS. The earlier test "waits for the daemon before the first status" still passes because the hook order did not change before the early return.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/watch-repo-dialog.tsx web/src/pages/repositories.tsx web/src/pages/repositories.test.tsx web/src/components/page/section.tsx web/src/api/client.ts
git commit -m "feat(web): manage watched repositories"
```

### Task 14: Accessibility and reduced motion

**Files:**
- Modify: `web/src/a11y.test.tsx` (a new `describe` after "History accessibility"; the "Repositories accessibility" case at lines 76-88)
- Modify: `web/src/styles/ghr-theme.css:321-331`
- Test: `web/src/styles/theme.test.ts`

**Interfaces:**
- Consumes: the Actions page (Task 11), the watched section (Task 13)
- Produces: none

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests**

In `web/src/styles/theme.test.ts`, extend the reduced-motion test:

```ts
  it("stops the pulse and the spinner when motion is reduced", () => {
    expect(css).toMatch(/\.ghr-pulse \{\s*animation: ghr-pulse 1\.8s ease-out infinite;/)
    expect(css).toMatch(/@media \(prefers-reduced-motion: reduce\) \{\s*\.ghr-pulse \{\s*animation: none;\s*\}\s*\.dr-spinner-circle \{\s*animation: none;/)
  })
```

(rename the existing `"stops the pulse when motion is reduced"` test rather than adding a second one).

In `web/src/a11y.test.tsx`, add after the History block:

```tsx
describe("Actions accessibility", () => {
  it.each([[1280], [390]])(
    "has no axe violations at %ipx",
    async (width) => {
      setViewport(width)
      mockApi(authedRoutes())
      renderApp("/actions")
      await screen.findByText("old-docs: GitHub rate limit; API calls are paused. Retry after 14:30")
      await screen.findByRole("link", { name: /^Bump the runner/ })
      await screen.findByRole("link", { name: /^Fix the cache key/ })
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
```

and in the "Repositories accessibility" case add, before the `violations()` assertion:

```tsx
      await screen.findByRole("region", { name: "Watched repositories" })
```

- [ ] **Step 2: Run the tests to verify the CSS test fails**

Run, in `web/`: `timeout 300 npx vitest run src/styles/theme.test.ts src/a11y.test.tsx`
Expected: the theme test FAILS (no `.dr-spinner-circle` rule); the accessibility cases PASS or report violations to fix in the page markup (never by disabling a rule).

- [ ] **Step 3: Write the CSS**

In `web/src/styles/ghr-theme.css`, inside `@media (prefers-reduced-motion: reduce)`, directly after the `.ghr-pulse` rule, add:

```css
  .dr-spinner-circle {
    animation: none;
  }
```

- [ ] **Step 4: Run the full checks**

Run, in `web/`: `timeout 900 npm test`, `timeout 300 npm run typecheck`, `timeout 300 npm run lint`
Run, from the repository root: `gofmt -l internal cmd web/*.go`, `timeout 300 go vet ./internal/... ./cmd/... ./web/`, `timeout 600 go test ./internal/... ./cmd/... ./web/`
Expected: all PASS. Known flakes (register row 26): `login.test` "sets the first password", `history.test` "500-job cap" and `settings.test` "saves only the changed fields" can time out at 5 s under load; rerun those files alone, and report them rather than raising timeouts here.

- [ ] **Step 5: Commit**

```bash
git add web/src/a11y.test.tsx web/src/styles/ghr-theme.css web/src/styles/theme.test.ts
git commit -m "test(web): cover the Actions page for accessibility"
```
