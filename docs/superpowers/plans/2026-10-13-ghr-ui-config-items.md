# Config Items in the UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `repos[].allow_public`, `web.listen` and `web.hosts` editable in the web UI, with save-time lockout checks, a start-time fallback for the web listener, and a Restart ghr action that applies the restart-only settings.

**Architecture:** The daemon gains three patch keys, a browser mark on web requests, save-time checks, a restart block in the status, `POST /restart` (runs `systemctl restart --no-block ghr.service` after flushing its 202), and a web listener that falls back to the last address a browser used instead of exiting. The web app gains a Serve when public switch on the repository page, a Web access group in Settings, restart and listener-error banners in the shell, and a restarting screen outside the app routes.

**Tech Stack:** Go 1.26 (standard library only); TypeScript 5.7, React 19, TanStack Router and Query 5, the darkraise-ui kit, Tailwind 4, Vitest 4 with Testing Library and jsdom, all already dependencies.

**Spec:** docs/superpowers/specs/2026-10-13-ghr-ui-config-items-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 7 of 16 tasks are heavy (1, 2, 3, 5, 7, 8, 9) and are delegated; the other 9 run in the session
**Plan review:** 2026-10-13 — dr-superpowers:judge-opus — executability 17 / coherence 18 / coverage 18 / assumptions 15 (round 2)

**Parallelism:** sequential — the owner chose sequential (spec), the web tasks consume the Go tasks' JSON, and the Go tasks share `internal/daemon/backend.go`, `run.go` and `internal/model/model.go`

## Global Constraints

- Commands run from the repository root. `/tmp` is a small tmpfs that is nearly full: before the first command of a session run `mkdir -p /root/.cache/ghr-plan-tmp`, and put `TMPDIR=/root/.cache/ghr-plan-tmp` in front of every `npm`, `npx` and Go command. Go is at `/usr/local/go/bin` and not on PATH: prefix every Go command with `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH`.
- Give every command a `timeout`: 300 seconds for one Go package or one web test file, 900 for a whole suite.
- A fresh worktree has no `web/node_modules`. Once, before the first web command: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 600 npm ci --prefer-offline --no-audit --no-fund`. `web/package-lock.json` must not change.
- While working on a task run only the tests its commands name; only the last task runs every suite. A web test that only times out is the loaded host: run it once more, alone, before believing it. Never start a dev server, a watcher or any background process.
- Before each Go commit: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` prints nothing, and `go vet ./internal/... ./cmd/...` passes. Before each web commit: `cd web && npx tsc --noEmit` and `cd web && npx eslint src --max-warnings 0` print nothing.
- `web/fixtures_test.go` (`TestTypeFixtures`) marshals Go values and compares them with `web/src/api/fixtures/*.json`. A task that changes a JSON shape those fixtures hold updates the fixture in the same task and runs `go test ./web/`.
- No new Go module and no new npm dependency.
- User-visible texts (labels, messages, events, errors) are part of the spec. Copy them exactly as the task gives them.
- When an existing test fails because it asserted behaviour this plan changes, the task says so and gives the corrected lines. Report any other existing-test failure in the task report; never change production code to make an old expectation pass.
- A task's "replace these lines" blocks were written against `master` at `d3e0974` plus the earlier tasks of this plan. When a block is not found exactly as quoted, find the same lines by their content, apply the same change, and say so in the task report; never skip the change.
- Code comments: only where the reason is not obvious from the code. No comment mentions a task, a register row or the spec.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no trailing period, no attribution or co-author lines.
- Never push, never write to GitHub, and never deploy to or restart anything on the runner LXC (192.168.0.99). The one exception is the last task: it deploys and restarts only after the owner's yes in its first step.
- English only.

## Contracts

Names one task adds and a later task uses. Go names are in package `model` (`internal/model/model.go`) unless another package is given.

- **`WebPatch`** (Task 1): `type WebPatch struct { Listen *string \`json:"listen,omitempty"\`; Hosts *[]string \`json:"hosts,omitempty"\` }`. `ConfigPatch` gains `Web *WebPatch \`json:"web,omitempty"\``; `RepoPatch` gains `AllowPublic *bool \`json:"allow_public,omitempty"\``.
- **`api.Backend.PatchConfig(ctx context.Context, p model.ConfigPatch) error`** (Task 1). Every handler that calls it passes `r.Context()`; `daemon.Backend.PatchConfig` has the same signature.
- **`api.WithBrowser(ctx context.Context, host string) context.Context` and `api.Browser(ctx context.Context) (host string, ok bool)`** (Task 2, `internal/api/browser.go`). `webui.Handler` marks every `/api/` request it forwards with the request's `Host` header. The connection's local address is read by the consumer from `http.LocalAddrContextKey`.
- **`daemon.Backend` seams** (Task 3): `InterfaceAddrs func() ([]net.Addr, error)` (nil means `net.InterfaceAddrs`) and `Bind func(addr string) error` (nil means `net.Listen("tcp", addr)` then `Close`). `WebApplied config.Web` (existing) holds the listen string the web listener actually serves (Task 8 sets it to the fallback when one is used).
- **Save-check messages** (Task 3), each starting with its field key and a colon: `web.listen: the web UI cannot turn itself off; clear it in config.yaml to turn the UI off`; `web.listen: web.listen must be host:port with a port from 1 to 65535, such as 0.0.0.0:8080`; `web.listen: you reached ghr on <L>; listening on <host> would lock this browser out`; `web.listen: <host> is not an address of this host`; `web.listen: <host:port> is not free: <reason>`; `web.hosts: you are using <name>; removing it would lock this browser out`.
- **`runner.(*Manager).Pruning() bool`** (Task 4): true while a prune of either kind holds `m.pruning` and `m.upd.running` is false. The `daemon.Manager` interface gains `Pruning() bool`; every fake manager in `internal/daemon` tests gains it.
- **`RepoStatus.Public *bool`** (Task 4), `json:"public,omitempty"`: GitHub's last reported visibility, nil until known.
- **`blockedText(repo)`** (Task 4): `<repo> is public; ghr serves only private repositories. Turn on Serve when public on its repository page to serve it`.
- **`RestartStatus` and `WebListenError`** (Task 5):
  ```go
  type RestartStatus struct {
  	Pending   []string `json:"pending,omitempty"`
  	BlockedBy []string `json:"blocked_by,omitempty"`
  	Available bool     `json:"available"`
  	WebListen string   `json:"web_listen,omitempty"`
  }
  type WebListenError struct {
  	Address   string `json:"address"`
  	Reason    string `json:"reason"`
  	ServingOn string `json:"serving_on,omitempty"`
  }
  ```
  `Status` gains `Restart RestartStatus \`json:"restart"\`` and `WebListenError *WebListenError \`json:"web_listen_error,omitempty"\``. Filled by `daemon.Backend.Status()` from new `Backend` fields `RestartAvailable bool` and `WebListenErr *model.WebListenError` (Task 5; Task 7 and Task 8 set them at start).
- **Pending keys and BlockedBy texts** (Task 5): pending keys `web.listen`, `web.hosts` in that order; blockers in this order: the current storage operation as `<verb> <target>` (the UI's `queueText` verbs, lower-cased: install → `installing`, remove → `removing`, clear → `clearing`; any other kind as itself), `<N> queued`, `pruning`, `updating the runner software`.
- **`daemon.Backend.RequestRestart(ctx context.Context) (run func(), err error)`** (Task 7) and **`api.Backend.RequestRestart`** with the same signature. It refuses with `api.Conflict("ghr is not running as ghr.service; restart it by hand")` or `api.Conflict("waits for: " + strings.Join(blockedBy, ", "))`, writes the event `restart requested` (`restart requested from the web UI` with the browser mark), and returns `run`, which calls the `Backend.Restart func(ctx context.Context) error` seam with `context.WithoutCancel(ctx)` and writes `restart failed: <err>` as an error event on failure. `POST /restart` calls it, writes 202, flushes, then calls `run`.
- **`daemon.Options`** (Tasks 7 and 8): `SelfUnit func(ctx context.Context) bool` (nil in tests means false; `DefaultOptions` sets the systemd check), `Restart func(ctx context.Context) error` (`DefaultOptions` runs `systemctl restart --no-block ghr.service`), `WebListenPath string` (`DefaultOptions`: `/var/lib/ghr/web-listen`; empty records nothing and offers no fallback).
- **`webui.Handler(a *Auth, api http.Handler, static fs.FS, hosts []string, onSession func())`** (Task 9): `onSession` (may be nil) runs on every request that passes `requireSession`, before the API handles it.
- **Restart event text** (Task 6): the event reads `config: web settings changed; restart ghr to apply` (the existing `config: ` prefix of config warnings); the reload response lists `web settings changed; restart ghr to apply`.
- **Web types** (Task 10, `web/src/api/types.ts`): `RestartStatus { pending?: string[]; blocked_by?: string[]; available: boolean; web_listen?: string }`, `WebListenError { address: string; reason: string; serving_on?: string }`, `Status.restart: RestartStatus`, `Status.web_listen_error?: WebListenError`, `WebPatch { listen?: string; hosts?: string[] }`, `ConfigPatch.web?: WebPatch`, `RepoConfig.allow_public?: boolean`, `RepoPatch.allow_public?: boolean`, `RepoStatus.public?: boolean`. Client: `api.restart(): Promise<void>` sends `POST /api/restart`.
- **`web/src/lib/restart.ts`** (Task 13): `restartPlan(page: { protocol: string; hostname: string; port: string; origin: string }, running: string | undefined, saved: string | undefined): { kind: "same" | "moved" | "proxy" | "off"; url: string; port?: string }`. `same`: web.listen not pending, url is the page origin. `off`: saved empty. `moved`: the page's port equals the running listen port; url is built per spec §3.3. `proxy`: otherwise; url is the page origin, `port` the new listen port.
- **`useRestart()`** (Task 13, `web/src/lib/use-restart.ts`): returns `{ ask(): void; confirm: Confirm | null; close(): void; busy: boolean; ready: boolean }`. Confirming calls `api.restart()`; on success it calls `queryClient.clear()` and `startRestarting({ url, same })`, where `same` is true when `url` is the page's own origin.
- **`startRestarting(r: { url: string; same: boolean }): void` and `useRestarting(): { url: string; same: boolean } | null`** (Task 13, `web/src/lib/restarting.ts`): a module store with a subscription (`useSyncExternalStore`). **`RestartScreen`** (Task 14, `web/src/components/restart-screen.tsx`) is rendered by `RootLayout` in `web/src/components/root-layout.tsx` in place of its outlet while `useRestarting()` is not null, so it sits outside `appRoute` and its guards.

- **Web helpers and components** (Tasks 12 to 14):
  - `splitListen(listen: string): { host: string; port: string } | null` and `isHostname(host: string): boolean` in `web/src/lib/listen.ts` (Task 12; Task 13 uses `splitListen`).
  - `SettingsSections` gains `webAccess?: boolean` (default false); only the Settings page passes it, so the setup wizard does not show Web access (Task 12). The group's id is `web-access`; Task 13 links `/settings#web-access`.
  - The settings form gains `rejectedFields: Record<string, string>` (Task 12).
  - `restartPlan`'s `saved` equals `running` when `web.listen` is not pending; `web/src/lib/restart.ts` exports the types `Page` and `RestartPlan` (Task 13).
  - `web/src/lib/restarting.ts` also exports `clearRestarting(): void` and `type Restarting = { url: string; same: boolean }`; `web/src/test/setup.ts` calls `clearRestarting()` after every test (Task 13).
  - `useRestart()` also returns `busy: boolean`, true while `POST /api/restart` is in flight, and `ready: boolean`, false until the config query has data and while its read fails; every Restart ghr button is disabled while `ready` is false (Task 13).
  - `web/src/components/restart-banner.tsx` exports `RestartControl({ restart, offline })`, `RestartBanner({ status, offline })` and `ListenErrorBanner({ error })` (Task 13; Task 14's app test clicks the banner's button).
- **`webPending(saved, applied config.Web) []string` and `(*daemon.Backend).blockedBy(st model.Status) []string`** (Task 5, `internal/daemon`): the pending keys and the blockers as Contracts word them. Task 6 uses `webPending`; Task 7 uses `blockedBy`.
- **Test helpers in `internal/api/api_test.go`** (Task 1): `fakeBackend.patchCtxs []context.Context` records the context of every `PatchConfig` call; `type ctxMark struct{}` is a context key the tests use to tag a request's context. Task 2 consumes both.
- **Test helpers in `internal/daemon`:**
  - `fakeManager.pruning bool` (Task 4, `backend_test.go`): the value `(*fakeManager).Pruning()` returns; Tasks 5 to 7 set it to make `pruning` a blocker.
  - `type restartManager struct { *fakeManager; update bool }` (Task 5, `restart_test.go`): its `Status()` is `fakeManager.Status()` with `RunnerUpdate.Running = update`.
  - `setWeb(t *testing.T, b *Backend, w config.Web)` (Task 5, `restart_test.go`): saves `w` as the config's web block through `b.update`, without the save checks, and fails the test on error.
  - `restartWarnings(b *Backend) int` (Task 6, `restart_test.go`): counts the `config: web settings changed; restart ghr to apply` warn events.
  - `recordRunner(calls *[]string, out string, err error) system.Runner` (Task 7, `restart_test.go`): records each command and answers with `out` and `err`.
  - `statusOver(t *testing.T, socket string) model.Status` (Task 8, `run_test.go`): waits up to 10 s for the daemon on `socket` to serve `GET /status` and returns it.
  - `checkConnBound(t *testing.T, addr string)` (Task 8, `run_test.go`): checks the web listener on `addr` limits one client to `maxWebConnsPerClient` connections.
- **`restart cancelled: <blockers> started`** (Task 7): the warn event `run` writes when work started between `RequestRestart`'s check and the restart; the seam is then not called.
- **`bindReason(err error) string`** (Task 3, `internal/daemon/webcheck.go`): the operating system's text of a bind error alone (`address already in use`). Task 8 uses it for `WebListenError.Reason`.
- **Tests that start `Run`** use short test and subtest names: with `TMPDIR=/root/.cache/ghr-plan-tmp` a long name pushes the control socket's path past the 108-byte limit for Unix sockets.

## Assumptions (evidence)

- Tasks 1 to 14 were applied in order, as written, to throwaway copies of `d3e0974` on 2026-10-13: every Step 2 failed as stated (or is marked as a guard that already passes), every Step 4 passed, and `go test ./internal/... ./cmd/... ./web/` passed after Task 9. After the first plan review the revised Tasks 6, 7, 8, 12, 13 and 14 were applied and run again the same day, with the same results: the Go packages passed after Task 9, and the web suite passed (96 files, 1,315 tests) after Task 14 with Task 5's fixture edit.
- The automatic disk prune sets only `m.pruning`, not `m.maint.Running` (`internal/runner/cleanup.go` `pruneDisk`, `internal/runner/manager.go` `StartPruneScope`), hence `Pruning()`.
- The change hub's `status` fingerprint hashes `b.M.Status` only (`internal/daemon/changes.go` `fingerprints`), hence Task 5 extends it.
- `api.Backend.PatchConfig` takes no context today (`internal/api/server.go` `Backend`).
- `TestTypeFixtures` (`web/fixtures_test.go`) compares Go output with `web/src/api/fixtures/*.json`; `status.json` and `status-degraded.json` gain `"restart": {"available": false}` in Task 5.
- The web CSP is `default-src 'self'` with no `connect-src` (`internal/webui/handler.go` `csp`): fetches stay on the page's origin, navigation is free.
- Sessions live in memory (`internal/webui/auth.go`), so a restart logs every browser out and only the new process answers `/auth/state` with `authenticated: false`.
- The service is `ghr.service` with `Restart=always` (`deploy/setup.sh` `install_service`).
- The runner LXC has `python3` and key-based SSH from the dev host (both used there on 2026-10-13); Task 16's deploy gate relies on them.
- Only `PatchConfig` and `Reload` change the `web` block while the daemon runs: the setup flow (`internal/daemon/setup.go`) reads the served address and never writes `web`, so `pendingMu` covers every path that moves the pending list.
- Out of scope: `restartPlan` treats an empty host, `0.0.0.0` and all-zero IPv6 group spellings as wildcards, but not the IPv4-mapped forms `::ffff:0.0.0.0` or `::0.0.0.0`; such a listen value shows a specific-IP URL in the confirmation.
- `RootLayout` is defined in `web/src/components/root-layout.tsx` and named by `web/src/router.tsx`.
- unverified — Task 16 verifies it: on the LXC, `systemctl show -p MainPID --value ghr.service` prints the daemon's PID, so `restart.available` is true and the banner's Restart ghr works.
- Texts the spec does not give, chosen by this plan: the client checks `Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080` and `<host> is not a bare hostname such as ghr.lan`; the repository group title `Visibility`; the banner link `Go to Web access`; two pending keys read `Restart needed: web.listen and web.hosts changed.`; the event keeps the `config: ` prefix of config warnings.
- `config.Validate`'s `web.hosts entries must be bare hostnames such as ghr.lan` has no `web.hosts: ` prefix, so it shows in the form's alert; the client check refuses such values first.

## Task index

1. Accept the new patch keys
2. Mark requests from the browser
3. Check web settings when saved
4. Report visibility and prunes from the runner manager
5. Report the restart state
6. Write the restart warning once per change
7. Restart through systemd
8. Fall back when the web address fails
9. Record the address a browser used
10. Add the web types and restart call
11. Add the Serve when public switch
12. Add the Web access settings
13. Add the restart banners and confirmation
14. Show a restarting screen
15. Document the new settings
16. Run every suite and check on the LXC

---

### Task 1: Accept the new patch keys

**Files:**
- Modify: `internal/model/model.go` (`RepoPatch`, new `WebPatch`, `ConfigPatch`)
- Modify: `internal/api/server.go` (`Backend.PatchConfig`, the `PATCH /config`, `POST /repos/{name}/pause` and `POST /repos/{name}/resume` handlers)
- Modify: `internal/daemon/backend.go` (`PatchConfig`)
- Test: `internal/api/api_test.go` (the `fakeBackend` fake, a new test)
- Test: `internal/daemon/backend_test.go` (every `b.PatchConfig` call, a new test, `TestPatchCannotChangeWeb` rewritten)
- Test: `internal/daemon/changes_test.go` (its one `b.PatchConfig` call and the `context` import)
- Test: `internal/runner/blocked_test.go` (a new test)

**Interfaces:**
- Consumes: nothing
- Produces: `WebPatch`, `ConfigPatch.Web`, `RepoPatch.AllowPublic` (Contracts: **`WebPatch`**); `api.Backend.PatchConfig(ctx context.Context, p model.ConfigPatch) error` and `daemon.Backend.PatchConfig` with the same signature (Contracts: **`api.Backend.PatchConfig`**). For Task 2 and Task 3: `fakeBackend.patchCtxs` and the `ctxMark` test key in `internal/api/api_test.go`.

**Items:** 2, 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

`PatchConfig` gains a context and applies `web.listen`, `web.hosts` and `repos.<name>.allow_public`. No save check runs yet; Task 3 adds them. `allow_public` needs nothing more than the config change: `updateThen` wakes the loop, and `blockedLocked` reads `AllowPublic` from the config of the tick. `TestPatchCannotChangeWeb` pinned that the web keys were ignored; it is rewritten to pin that they are saved.

The new tests that patch `web.listen` set `b.WebApplied.Listen` to the same port, so that the port test Task 3 adds makes no real bind in them.

- [ ] **Step 1: Write the failing tests**

Before the first command of the session run `mkdir -p /root/.cache/ghr-plan-tmp`.

In `internal/api/api_test.go`, in `type fakeBackend struct`, replace

```go
type fakeBackend struct {
	patches       []model.ConfigPatch
```

with

```go
type fakeBackend struct {
	patches       []model.ConfigPatch
	patchCtxs     []context.Context // the context each PatchConfig call got
```

Replace

```go
func (f *fakeBackend) PatchConfig(p model.ConfigPatch) error {
	if p.GlobalMax != nil && *p.GlobalMax < 1 {
```

with

```go
func (f *fakeBackend) PatchConfig(ctx context.Context, p model.ConfigPatch) error {
	f.patchCtxs = append(f.patchCtxs, ctx)
	if p.GlobalMax != nil && *p.GlobalMax < 1 {
```

Directly above `func TestMutations(t *testing.T) {` add

```go
type ctxMark struct{}

// The routes that patch the config hand the request's own context to the
// backend, which reads the browser mark from it.
func TestPatchRoutesPassTheRequestContext(t *testing.T) {
	b := &fakeBackend{}
	h := NewServer(b)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPatch, "/config", `{"global_max":3}`},
		{http.MethodPost, "/repos/darkmem/pause", ""},
		{http.MethodPost, "/repos/darkmem/resume", ""},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req = req.WithContext(context.WithValue(req.Context(), ctxMark{}, "marked"))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	if len(b.patchCtxs) != 3 {
		t.Fatalf("%d PatchConfig calls, want 3", len(b.patchCtxs))
	}
	for i, ctx := range b.patchCtxs {
		if ctx.Value(ctxMark{}) != "marked" {
			t.Fatalf("call %d did not get the request's context", i)
		}
	}
}
```

Change every `b.PatchConfig(` call in the daemon tests to pass a context first. The calls are at `internal/daemon/backend_test.go` lines 331, 339, 342, 353, 372, 390, 429, 460, 493, 709, 724, 753, 1168, 1533, 1631, 1665, 1687, 1695, 1705, 1794 and `internal/daemon/changes_test.go` line 115; each changes only by the inserted `context.Background(), `, for example `if err := b.PatchConfig(model.ConfigPatch{Mode: &bad}); apiStatus(err) != 400 {` becomes `if err := b.PatchConfig(context.Background(), model.ConfigPatch{Mode: &bad}); apiStatus(err) != 400 {`. Run:

```bash
sed -i 's/b\.PatchConfig(/b.PatchConfig(context.Background(), /' internal/daemon/backend_test.go internal/daemon/changes_test.go
grep -c 'b.PatchConfig(context.Background(), ' internal/daemon/backend_test.go internal/daemon/changes_test.go
```

Expected: `internal/daemon/backend_test.go:20` and `internal/daemon/changes_test.go:1`.

In `internal/daemon/changes_test.go` replace

```go
import (
	"errors"
```

with

```go
import (
	"context"
	"errors"
```

In `internal/daemon/backend_test.go`, directly above the line `// An invalid setting is a 400 that names the problem and leaves config.yaml untouched.`, add

```go
// web.listen and web.hosts are saved for the next start, and allow_public is
// saved and wakes the loop. The JSON keys clients send reach the fields.
func TestPatchConfigWebAndAllowPublic(t *testing.T) {
	b, _, _ := newBackend(t)
	b.WebApplied = config.Web{Listen: "0.0.0.0:8080"}
	wakes := 0
	b.Wake = func() { wakes++ }
	var p model.ConfigPatch
	body := `{"web":{"listen":"[::]:8080","hosts":["ghr.lan","runner.lan"]},"repos":{"darkmem":{"allow_public":true}}}`
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	if err := b.PatchConfig(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	want := config.Web{Listen: "[::]:8080", Hosts: []string{"ghr.lan", "runner.lan"}}
	c := b.Store.Config()
	if !reflect.DeepEqual(c.Web, want) || !c.Repo("darkmem").AllowPublic || c.Repo("darkcloud").AllowPublic || wakes != 1 {
		t.Fatalf("web %+v, allow_public darkmem %v darkcloud %v, wakes %d", c.Web, c.Repo("darkmem").AllowPublic, c.Repo("darkcloud").AllowPublic, wakes)
	}
	saved, _, err := config.Load(b.Store.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Web, want) || !saved.Repo("darkmem").AllowPublic {
		t.Fatalf("not persisted: web %+v, allow_public %v", saved.Web, saved.Repo("darkmem").AllowPublic)
	}
	// A patch without the keys leaves them alone; a false allow_public clears it.
	off := false
	if err := b.PatchConfig(context.Background(), model.ConfigPatch{Repos: map[string]model.RepoPatch{"darkmem": {AllowPublic: &off}}}); err != nil {
		t.Fatal(err)
	}
	c = b.Store.Config()
	if !reflect.DeepEqual(c.Web, want) || c.Repo("darkmem").AllowPublic || wakes != 2 {
		t.Fatalf("web %+v, allow_public %v, wakes %d", c.Web, c.Repo("darkmem").AllowPublic, wakes)
	}
	hosts := []string{}
	if err := b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Hosts: &hosts}}); err != nil {
		t.Fatal(err)
	}
	if c := b.Store.Config(); c.Web.Listen != "[::]:8080" || len(c.Web.Hosts) != 0 {
		t.Fatalf("web %+v, want the listen value kept and no hosts", c.Web)
	}
}
```

In the same file replace the whole of

```go
func TestPatchCannotChangeWeb(t *testing.T) {
	b, _, _ := newBackend(t)
	srv := httptest.NewServer(api.NewServer(b))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/config",
		strings.NewReader(`{"global_max":3,"web":{"listen":"0.0.0.0:9000"}}`))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("patch: %d", resp.StatusCode)
	}
	if c := b.Store.Config(); c.GlobalMax != 3 || c.Web.Listen != "" {
		t.Fatalf("global_max %d web %+v", c.GlobalMax, c.Web)
	}
}
```

with

```go
// PATCH /config saves the web keys; they apply at the next start. The new
// listen port is the one the listener holds, so no bind is tried.
func TestPatchChangesWebThroughTheAPI(t *testing.T) {
	b, _, _ := newBackend(t)
	b.WebApplied = config.Web{Listen: "0.0.0.0:9000"}
	srv := httptest.NewServer(api.NewServer(b))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/config",
		strings.NewReader(`{"global_max":3,"web":{"listen":"[::]:9000","hosts":["ghr.lan"]}}`))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("patch: %d", resp.StatusCode)
	}
	if c := b.Store.Config(); c.GlobalMax != 3 || c.Web.Listen != "[::]:9000" || !slices.Equal(c.Web.Hosts, []string{"ghr.lan"}) {
		t.Fatalf("global_max %d web %+v", c.GlobalMax, c.Web)
	}
}
```

In `internal/runner/blocked_test.go`, directly above the line `// spawnPlanned plans nothing for a repository GitHub has not answered for,`, add

```go
// A patch changes allow_public without a reload: only the config changes.
// The next tick serves the public repository, and taking allow_public away
// again stops its idle runner on the tick after.
func TestAllowPublicFromAPatchAppliesOnTheNextTick(t *testing.T) {
	h := newHarness(t)
	blockDarkmem(t, h)
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	tick := func() {
		h.now = h.now.Add(10 * time.Second)
		h.m.Tick(context.Background())
		h.m.Wait()
	}
	tick()
	if spawnsFor(h, "darkmem") != 0 {
		t.Fatalf("setup: %d spawns for a blocked repository", spawnsFor(h, "darkmem"))
	}
	allowed := h.cfg.Clone()
	allowed.Repo("darkmem").AllowPublic = true
	h.cfg = allowed
	tick()
	if spawnsFor(h, "darkmem") != 1 {
		t.Fatalf("allow_public on: %d spawns for darkmem, want 1\n%s", spawnsFor(h, "darkmem"), h.eventText())
	}
	h.gh.setRunner(101, "online", false)
	h.gh.setJobs(7)
	tick()
	if h.state("aaaaaa") != "idle" || len(h.sd.stopped) != 0 {
		t.Fatalf("setup: aaaaaa %s, stopped %v", h.state("aaaaaa"), h.sd.stopped)
	}
	denied := h.cfg.Clone()
	denied.Repo("darkmem").AllowPublic = false
	h.cfg = denied
	tick()
	if len(h.sd.stopped) != 1 || h.sd.stopped[0] != "ghr-runner-aaaaaa" {
		t.Fatalf("allow_public off: stopped %v, want the idle runner aaaaaa", h.sd.stopped)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/api/ ./internal/daemon/ ./internal/runner/`

Expected: `internal/api` and `internal/daemon` fail to build: `*fakeBackend does not implement Backend (wrong type for method PatchConfig)` and `too many arguments in call to b.PatchConfig`. `internal/runner` passes: `TestAllowPublicFromAPatchAppliesOnTheNextTick` pins what the manager already does with a config change, which this task relies on.

- [ ] **Step 3: Write the implementation**

In `internal/model/model.go` replace

```go
type RepoPatch struct {
	Max                 OptInt    `json:"max,omitzero"`
	Warm                OptInt    `json:"warm,omitzero"`
	Labels              *[]string `json:"labels,omitempty"`
	CleanupNamePrefixes *[]string `json:"cleanup_name_prefixes,omitempty"`
	Paused              *bool     `json:"paused,omitempty"`
}
```

with

```go
type RepoPatch struct {
	Max                 OptInt    `json:"max,omitzero"`
	Warm                OptInt    `json:"warm,omitzero"`
	Labels              *[]string `json:"labels,omitempty"`
	CleanupNamePrefixes *[]string `json:"cleanup_name_prefixes,omitempty"`
	Paused              *bool     `json:"paused,omitempty"`
	AllowPublic         *bool     `json:"allow_public,omitempty"`
}
```

Replace

```go
type ConfigPatch struct {
	Mode             *string              `json:"mode,omitempty"`
```

with

```go
// WebPatch changes the web listener's settings; they apply at the next start.
type WebPatch struct {
	Listen *string   `json:"listen,omitempty"`
	Hosts  *[]string `json:"hosts,omitempty"`
}

type ConfigPatch struct {
	Mode             *string              `json:"mode,omitempty"`
```

Replace

```go
	RunnerLimits     *RunnerLimitsPatch   `json:"runner_limits,omitempty"`
	Repos            map[string]RepoPatch `json:"repos,omitempty"`
```

with

```go
	RunnerLimits     *RunnerLimitsPatch   `json:"runner_limits,omitempty"`
	Web              *WebPatch            `json:"web,omitempty"`
	Repos            map[string]RepoPatch `json:"repos,omitempty"`
```

In `internal/api/server.go`, in `type Backend interface`, replace

```go
	PatchConfig(p model.ConfigPatch) error
```

with

```go
	PatchConfig(ctx context.Context, p model.ConfigPatch) error
```

In `NewServer` replace

```go
		respond(w, nil, b.PatchConfig(p))
```

with

```go
		respond(w, nil, b.PatchConfig(r.Context(), p))
```

and replace

```go
		respond(w, nil, b.PatchConfig(pausePatch(r.PathValue("name"), true)))
	})
	mux.HandleFunc("POST /repos/{name}/resume", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.PatchConfig(pausePatch(r.PathValue("name"), false)))
```

with

```go
		respond(w, nil, b.PatchConfig(r.Context(), pausePatch(r.PathValue("name"), true)))
	})
	mux.HandleFunc("POST /repos/{name}/resume", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.PatchConfig(r.Context(), pausePatch(r.PathValue("name"), false)))
```

In `internal/daemon/backend.go` replace

```go
func (b *Backend) PatchConfig(p model.ConfigPatch) error {
```

with

```go
func (b *Backend) PatchConfig(ctx context.Context, p model.ConfigPatch) error {
```

Inside it replace

```go
			if rl.CPUQuota != nil {
				c.RunnerLimits.CPUQuota = *rl.CPUQuota
			}
		}
```

with

```go
			if rl.CPUQuota != nil {
				c.RunnerLimits.CPUQuota = *rl.CPUQuota
			}
		}
		if w := p.Web; w != nil {
			if w.Listen != nil {
				c.Web.Listen = *w.Listen
			}
			if w.Hosts != nil {
				c.Web.Hosts = *w.Hosts
			}
		}
```

and replace

```go
			if rp.CleanupNamePrefixes != nil {
				r.CleanupNamePrefixes = *rp.CleanupNamePrefixes
			}
```

with

```go
			if rp.CleanupNamePrefixes != nil {
				r.CleanupNamePrefixes = *rp.CleanupNamePrefixes
			}
			if rp.AllowPublic != nil {
				r.AllowPublic = *rp.AllowPublic
			}
```

`ctx` is unused until Task 3. `cmd/ghr` calls the client's `PatchConfig`, which already takes a context, and needs no change.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 600 go test -count=1 ./internal/api/ ./internal/daemon/ ./internal/runner/ ./internal/model/ ./cmd/... ./web/`
Expected: PASS for every package.

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 gofmt -l internal/ cmd/` (prints nothing) and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...` (passes).

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/api/server.go internal/api/api_test.go internal/daemon/backend.go internal/daemon/backend_test.go internal/daemon/changes_test.go internal/runner/blocked_test.go
git commit -m "feat(daemon): accept web and allow_public patches"
```

### Task 2: Mark requests from the browser

**Files:**
- Create: `internal/api/browser.go`
- Modify: `internal/webui/handler.go` (import, `requireSession`)
- Test: `internal/api/browser_test.go` (new)
- Test: `internal/webui/handler_test.go` (import, a new test)

**Interfaces:**
- Consumes: Task 1's `fakeBackend.patchCtxs` and `ctxMark` in `internal/api/api_test.go`; `api.Backend.PatchConfig` taking `r.Context()` (Contracts: **`api.Backend.PatchConfig`**)
- Produces: `api.WithBrowser(ctx context.Context, host string) context.Context` and `api.Browser(ctx context.Context) (host string, ok bool)`; every `/api/` request `webui.Handler` forwards carries the mark with its `Host` header (Contracts: **`api.WithBrowser` and `api.Browser`**)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

The mark is set inside `requireSession`, on the context it already derives, so only a request with a valid session carries it. `http.StripPrefix` copies the request with its context, so the mark reaches the API handler.

- [ ] **Step 1: Write the failing tests**

Create `internal/api/browser_test.go`:

```go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The mark survives contexts derived from it and reaches the backend through
// the request's context; a request without it reads as one over the socket.
func TestBrowserMark(t *testing.T) {
	if host, ok := Browser(context.Background()); ok || host != "" {
		t.Fatalf("an unmarked context reads as %q, %v", host, ok)
	}
	ctx := WithBrowser(context.Background(), "ghr.lan:8080")
	derived, cancel := context.WithCancel(context.WithValue(ctx, ctxMark{}, "other"))
	defer cancel()
	if host, ok := Browser(derived); !ok || host != "ghr.lan:8080" {
		t.Fatalf("derived context: %q, %v", host, ok)
	}
	if host, ok := Browser(WithBrowser(context.Background(), "")); !ok || host != "" {
		t.Fatalf("a request without a Host is still the browser's: %q, %v", host, ok)
	}
	b := &fakeBackend{}
	req := httptest.NewRequest(http.MethodPatch, "/config", strings.NewReader(`{"global_max":3}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	NewServer(b).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || len(b.patchCtxs) != 1 {
		t.Fatalf("patch: %d %s, %d calls", rec.Code, rec.Body.String(), len(b.patchCtxs))
	}
	if host, ok := Browser(b.patchCtxs[0]); !ok || host != "ghr.lan:8080" {
		t.Fatalf("the backend saw %q, %v", host, ok)
	}
}
```

In `internal/webui/handler_test.go` replace

```go
	"testing"
	"time"
)
```

with

```go
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
)
```

`newTestHandler` names a local variable `api`; it shadows the package only inside that function and needs no change. Directly above `func TestRequestChecks(t *testing.T) {` add

```go
// Every /api/ request a session lets through reaches the API marked as the
// browser's, with the Host it sent, past the /api prefix strip. A request
// the session check refuses never reaches it.
func TestAPIRequestsCarryTheBrowserMark(t *testing.T) {
	a, _ := newTestAuth(t)
	var seen []string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, ok := api.Browser(r.Context())
		if !ok {
			host = "(no mark)"
		}
		seen = append(seen, r.Method+" "+r.URL.Path+" "+host)
		w.WriteHeader(http.StatusNoContent)
	})
	h := Handler(a, backend, testFS(), []string{"ghr.lan"})
	id, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(id)); rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	byIP := func(r *http.Request) {
		withCookie(id)(r)
		r.Host = "192.168.0.99:8080"
	}
	if rec := do(h, http.MethodPatch, "/api/config", `{}`, byIP); rec.Code != http.StatusNoContent {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPatch, "/api/config", `{}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without a session: %d", rec.Code)
	}
	want := []string{"GET /status ghr.lan:8080", "PATCH /config 192.168.0.99:8080"}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the API saw %q, want %q", seen, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/api/ ./internal/webui/`
Expected: both fail to build: `undefined: Browser` and `undefined: WithBrowser` in `internal/api/browser_test.go`, `undefined: api.Browser` in `internal/webui/handler_test.go`.

- [ ] **Step 3: Write the implementation**

Create `internal/api/browser.go`:

```go
package api

import "context"

type browserKey struct{}

// WithBrowser marks ctx as a request the web UI's listener forwarded from a
// browser, with the Host header the browser sent.
func WithBrowser(ctx context.Context, host string) context.Context {
	return context.WithValue(ctx, browserKey{}, host)
}

// Browser reports the Host header of a request the web UI forwarded; ok is
// false for a request over the socket.
func Browser(ctx context.Context) (host string, ok bool) {
	host, ok = ctx.Value(browserKey{}).(string)
	return host, ok
}
```

In `internal/webui/handler.go` replace

```go
	"sync"
	"time"
)
```

with

```go
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/api"
)
```

`Handler`'s parameter `api` and the local `api` in `checks` shadow the package inside those two functions only; `requireSession` sees the package. Replace

```go
// requireSession serves next only for a logged-in session, and cancels the
// request's context once that session ends: logout, a password change or
// reset, or expiry.
```

with

```go
// requireSession serves next only for a logged-in session, and cancels the
// request's context once that session ends: logout, a password change or
// reset, or expiry. The context it hands on carries the browser mark.
```

and inside `requireSession` replace

```go
		ctx, cancel := context.WithCancel(r.Context())
```

with

```go
		ctx, cancel := context.WithCancel(api.WithBrowser(r.Context(), r.Host))
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 600 go test -count=1 ./internal/api/ ./internal/webui/ ./internal/daemon/`
Expected: PASS for the three packages.

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 gofmt -l internal/ cmd/` (prints nothing) and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...` (passes).

- [ ] **Step 5: Commit**

```bash
git add internal/api/browser.go internal/api/browser_test.go internal/webui/handler.go internal/webui/handler_test.go
git commit -m "feat(webui): mark API requests from the browser"
```

### Task 3: Check web settings when saved

**Files:**
- Create: `internal/daemon/webcheck.go`
- Modify: `internal/daemon/backend.go` (import `net`, the `InterfaceAddrs` and `Bind` fields, `PatchConfig`)
- Test: `internal/daemon/webcheck_test.go` (new)

**Interfaces:**
- Consumes: `WebPatch` and `PatchConfig(ctx, …)` from Task 1; `api.WithBrowser` and `api.Browser` from Task 2 (Contracts: **`api.WithBrowser` and `api.Browser`**); `Backend.WebApplied` (existing, `internal/daemon/backend.go:108`)
- Produces: the `daemon.Backend` seams `InterfaceAddrs func() ([]net.Addr, error)` and `Bind func(addr string) error` (Contracts: **`daemon.Backend` seams**); the six save-check messages (Contracts: **Save-check messages**). Also, for Task 8 if it wants the same wording, `bindReason(err error) string` in `internal/daemon/webcheck.go`: the operating system's reason alone, such as `address already in use`.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 3 = 5

The checks of spec §2.1 run inside the patch function, in its order, on the keys the patch carries: checks 1 to 5 when it sets `web.listen`, check 6 when it sets `web.hosts`. A failure is a 400 and leaves `config.yaml` as it was. The connection's local address comes from `http.LocalAddrContextKey` in the context `PatchConfig` receives; the request's context carries it from `net/http`, through `webui`'s derived contexts and the `api` handler's `r.Context()`. The tests put a `*net.TCPAddr` under that key. A request with the browser mark but no local address (it cannot come from `net/http`) is checked as one over the socket.

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/webcheck_test.go`:

```go
package daemon

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// fromBrowser is the context of a request the web UI forwarded: the Host
// header the browser sent and the local address its connection reached.
func fromBrowser(host, local string) context.Context {
	ctx := api.WithBrowser(context.Background(), host)
	return context.WithValue(ctx, http.LocalAddrContextKey, net.TCPAddrFromAddrPort(netip.MustParseAddrPort(local)))
}

func ipNet(t *testing.T, cidr string) net.Addr {
	t.Helper()
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	n.IP = ip
	return n
}

// webCheckBackend has loopback, a LAN address on IPv4 and IPv6, a docker
// bridge and a second NIC, and a web listener on port 8080. Its binder finds
// port 9090 taken and records every address it is asked for.
func webCheckBackend(t *testing.T) (*Backend, *[]string) {
	t.Helper()
	b, _, _ := newBackend(t)
	b.WebApplied = config.Web{Listen: "0.0.0.0:8080"}
	addrs := []net.Addr{ipNet(t, "127.0.0.1/8"), ipNet(t, "::1/128"), ipNet(t, "192.168.0.10/24"),
		ipNet(t, "fd00::10/64"), ipNet(t, "172.17.0.1/16"), ipNet(t, "10.0.0.5/24")}
	b.InterfaceAddrs = func() ([]net.Addr, error) { return addrs, nil }
	binds := &[]string{}
	b.Bind = func(addr string) error {
		*binds = append(*binds, addr)
		if strings.HasSuffix(addr, ":9090") {
			return &net.OpError{Op: "listen", Net: "tcp", Err: &os.SyscallError{Syscall: "bind", Err: errors.New("address already in use")}}
		}
		return nil
	}
	return b, binds
}

func TestPatchConfigChecksWebSettings(t *testing.T) {
	lan := fromBrowser("192.168.0.10:8080", "192.168.0.10:8080")
	byName := fromBrowser("ghr.lan:8080", "192.168.0.10:8080")
	v6 := fromBrowser("[fd00::10]:8080", "[fd00::10]:8080")
	mapped := fromBrowser("192.168.0.10:8080", "[::ffff:192.168.0.10]:8080")
	proxy := fromBrowser("ghr.example.com", "127.0.0.1:8080")
	viaLocalhost := fromBrowser("localhost:8080", "127.0.0.1:8080")
	noLocal := api.WithBrowser(context.Background(), "ghr.lan:8080")
	socket := context.Background()
	listen := func(s string) *model.WebPatch { return &model.WebPatch{Listen: &s} }
	hosts := func(h ...string) *model.WebPatch {
		if h == nil {
			h = []string{}
		}
		return &model.WebPatch{Hosts: &h}
	}
	lockout := func(local, host string) string {
		return "web.listen: you reached ghr on " + local + "; listening on " + host + " would lock this browser out"
	}
	const (
		off      = "web.listen: the web UI cannot turn itself off; clear it in config.yaml to turn the UI off"
		shape    = "web.listen: web.listen must be host:port with a port from 1 to 65535, such as 0.0.0.0:8080"
		taken    = "web.listen: 0.0.0.0:9090 is not free: address already in use"
		lanUsing = "web.hosts: you are using ghr.lan; removing it would lock this browser out"
	)
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		web   *model.WebPatch
		want  string // "" accepts the patch
		binds []string
	}{
		{"empty listen from a browser", lan, listen(""), off, nil},
		{"empty listen over the socket", socket, listen(""), off, nil},
		{"no port", socket, listen("0.0.0.0"), shape, nil},
		{"port 0", lan, listen("0.0.0.0:0"), shape, nil},
		{"port 70000", socket, listen("0.0.0.0:70000"), shape, nil},
		{"a named port", lan, listen("0.0.0.0:http"), shape, nil},

		{"LAN browser: 0.0.0.0", lan, listen("0.0.0.0:8080"), "", nil},
		{"LAN browser: ::", lan, listen("[::]:8080"), "", nil},
		{"LAN browser: its own address", lan, listen("192.168.0.10:8080"), "", nil},
		{"LAN browser: an empty host", lan, listen(":8080"), "", nil},
		{"LAN browser: the docker bridge", lan, listen("172.17.0.1:8080"), lockout("192.168.0.10", "172.17.0.1"), nil},
		{"LAN browser: another NIC", lan, listen("10.0.0.5:8080"), lockout("192.168.0.10", "10.0.0.5"), nil},
		{"LAN browser: localhost", lan, listen("localhost:8080"), lockout("192.168.0.10", "localhost"), nil},
		{"LAN browser: a loopback address", lan, listen("127.0.0.1:8080"), lockout("192.168.0.10", "127.0.0.1"), nil},

		{"IPv6 browser: 0.0.0.0", v6, listen("0.0.0.0:8080"), lockout("fd00::10", "0.0.0.0"), nil},
		{"IPv6 browser: ::", v6, listen("[::]:8080"), "", nil},
		{"IPv6 browser: its own address", v6, listen("[fd00::10]:8080"), "", nil},
		{"IPv6 browser: an IPv4 address", v6, listen("192.168.0.10:8080"), lockout("fd00::10", "192.168.0.10"), nil},
		{"IPv4-mapped browser: 0.0.0.0", mapped, listen("0.0.0.0:8080"), "", nil},
		{"IPv4-mapped browser: its own address", mapped, listen("192.168.0.10:8080"), "", nil},

		{"loopback browser: localhost", proxy, listen("localhost:8080"), "", nil},
		{"loopback browser: 127.0.0.1", proxy, listen("127.0.0.1:8080"), "", nil},
		{"loopback browser: ::1", proxy, listen("[::1]:8080"), "", nil},
		{"loopback browser: 0.0.0.0", proxy, listen("0.0.0.0:8080"), "", nil},
		{"loopback browser: a LAN address", proxy, listen("192.168.0.10:8080"), lockout("127.0.0.1", "192.168.0.10"), nil},

		{"socket: an own address", socket, listen("10.0.0.5:8080"), "", nil},
		{"socket: an own IPv6 address", socket, listen("[fd00::10]:8080"), "", nil},
		{"socket: 0.0.0.0", socket, listen("0.0.0.0:8080"), "", nil},
		{"socket: ::", socket, listen("[::]:8080"), "", nil},
		{"socket: localhost", socket, listen("localhost:8080"), "", nil},
		{"socket: an empty host", socket, listen(":8080"), "", nil},
		{"socket: a foreign address", socket, listen("203.0.113.7:8080"), "web.listen: 203.0.113.7 is not an address of this host", nil},
		{"socket: a hostname", socket, listen("ghr.lan:8080"), "web.listen: ghr.lan is not an address of this host", nil},
		{"a browser without a local address is checked as the socket", noLocal, listen("203.0.113.7:8080"),
			"web.listen: 203.0.113.7 is not an address of this host", nil},

		{"a free port", lan, listen("0.0.0.0:9091"), "", []string{"0.0.0.0:9091"}},
		{"a taken port", lan, listen("0.0.0.0:9090"), taken, []string{"0.0.0.0:9090"}},
		{"the held port on another address", lan, listen("192.168.0.10:8080"), "", nil},
		{"a taken port over the socket", socket, listen("10.0.0.5:9090"), "web.listen: 10.0.0.5:9090 is not free: address already in use",
			[]string{"10.0.0.5:9090"}},
		{"a lockout is reported before the port", lan, listen("172.17.0.1:9090"), lockout("192.168.0.10", "172.17.0.1"), nil},

		{"hostname browser: its name kept in another case", byName, hosts("other.lan", "GHR.LAN"), "", nil},
		{"hostname browser: its name removed", byName, hosts("other.lan"), lanUsing, nil},
		{"hostname browser: every name removed", byName, hosts(), lanUsing, nil},
		{"hostname browser behind a proxy", proxy, hosts("ghr.lan"),
			"web.hosts: you are using ghr.example.com; removing it would lock this browser out", nil},
		{"IP browser: every name removed", lan, hosts(), "", nil},
		{"IPv6 browser: every name removed", v6, hosts(), "", nil},
		{"localhost browser: every name removed", viaLocalhost, hosts(), "", nil},
		{"socket: every name removed", socket, hosts(), "", nil},
		{"listen is checked before hosts", byName, &model.WebPatch{Listen: listen("172.17.0.1:8080").Listen, Hosts: hosts().Hosts},
			lockout("192.168.0.10", "172.17.0.1"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, binds := webCheckBackend(t)
			before, err := os.ReadFile(b.Store.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			err = b.PatchConfig(tc.ctx, model.ConfigPatch{Web: tc.web})
			if !slices.Equal(*binds, tc.binds) {
				t.Errorf("binds %q, want %q", *binds, tc.binds)
			}
			if tc.want == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if w := b.Store.Config().Web; tc.web.Listen != nil && w.Listen != *tc.web.Listen {
					t.Fatalf("listen %q not saved", w.Listen)
				}
				return
			}
			if apiStatus(err) != 400 || err.Error() != tc.want {
				t.Fatalf("err %v (status %d), want 400 %q", err, apiStatus(err), tc.want)
			}
			if msg := err.Error(); !strings.HasPrefix(msg, "web.listen: ") && !strings.HasPrefix(msg, "web.hosts: ") {
				t.Fatalf("%q does not start with its field key", msg)
			}
			after, err := os.ReadFile(b.Store.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("a refused patch changed config.yaml:\n%s", after)
			}
		})
	}
}

// Without the seams the checks list this host's own addresses and bind for
// real: a port held elsewhere is refused, and accepted once it is released.
func TestPatchConfigWebChecksUseTheHost(t *testing.T) {
	b, _, _ := newBackend(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := "127.0.0.1:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	err = b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Listen: &addr}})
	if want := "web.listen: " + addr + " is not free: address already in use"; err == nil || err.Error() != want {
		l.Close()
		t.Fatalf("err %v, want %q", err, want)
	}
	l.Close()
	if err := b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Listen: &addr}}); err != nil {
		t.Fatalf("a released port: %v", err)
	}
	foreign := "203.0.113.7:8080"
	err = b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Listen: &foreign}})
	if want := "web.listen: 203.0.113.7 is not an address of this host"; err == nil || err.Error() != want {
		t.Fatalf("err %v, want %q", err, want)
	}
}
```

`TestPatchConfigWebChecksUseTheHost` runs with the default seams: it holds a loopback port of its own, so the refusal does not depend on what else runs on the host, and expects the loopback address `127.0.0.1` among `net.InterfaceAddrs`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/daemon/`
Expected: build failure, `b.InterfaceAddrs undefined (type *Backend has no field or method InterfaceAddrs)` and `b.Bind undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/daemon/webcheck.go`:

```go
package daemon

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

// checkWeb refuses a web patch that would turn the UI off, that does not
// parse, that would lock out the browser that sent it, or whose address
// would not bind at the next start. It runs inside the store's update, so
// its network calls are made with Store.mu held; they are quick local ones.
func (b *Backend) checkWeb(ctx context.Context, w *model.WebPatch) error {
	browserHost, browser := api.Browser(ctx)
	if w.Listen != nil {
		if err := b.checkListen(ctx, *w.Listen, browser); err != nil {
			return err
		}
	}
	if w.Hosts != nil && browser {
		name := hostOf(browserHost)
		_, ipErr := netip.ParseAddr(name)
		named := ipErr != nil && !strings.EqualFold(name, "localhost")
		if named && !slices.ContainsFunc(*w.Hosts, func(h string) bool { return strings.EqualFold(h, name) }) {
			return api.BadRequest("web.hosts: you are using " + name + "; removing it would lock this browser out")
		}
	}
	return nil
}

func (b *Backend) checkListen(ctx context.Context, listen string, browser bool) error {
	if listen == "" {
		return api.BadRequest("web.listen: the web UI cannot turn itself off; clear it in config.yaml to turn the UI off")
	}
	host, port, ok := splitListen(listen)
	if !ok {
		return api.BadRequest("web.listen: web.listen must be host:port with a port from 1 to 65535, such as 0.0.0.0:8080")
	}
	// A browser's request always has its local address; without one the
	// request is checked as one over the socket.
	if local, ok := localIP(ctx); browser && ok {
		if !reachable(host, local) {
			return api.BadRequest("web.listen: you reached ghr on " + local.String() + "; listening on " + host + " would lock this browser out")
		}
	} else if !b.ownHost(host) {
		return api.BadRequest("web.listen: " + host + " is not an address of this host")
	}
	// The running listener holds its own port until the restart releases it.
	if _, held, ok := splitListen(b.WebApplied.Listen); ok && held == port {
		return nil
	}
	if err := b.bind(listen); err != nil {
		return api.BadRequest("web.listen: " + listen + " is not free: " + bindReason(err))
	}
	return nil
}

// splitListen parses a web.listen value as config.Validate does.
func splitListen(listen string) (host string, port int, ok bool) {
	host, p, err := net.SplitHostPort(listen)
	if err != nil {
		return "", 0, false
	}
	port, err = strconv.Atoi(p)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return host, port, true
}

// hostOf is the hostname of a Host header, without its port or brackets.
func hostOf(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
}

// localIP is the address the request's connection reached, unmapped, so a
// browser on IPv4 that reached a dual-stack listener reads as IPv4.
func localIP(ctx context.Context) (netip.Addr, bool) {
	ta, ok := ctx.Value(http.LocalAddrContextKey).(*net.TCPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	a, ok := netip.AddrFromSlice(ta.IP)
	return a.Unmap(), ok
}

// reachable reports whether a listener on host still accepts a browser whose
// connection reached local.
func reachable(host string, local netip.Addr) bool {
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return local.IsLoopback()
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	a = a.Unmap().WithZone("")
	switch {
	case a == netip.IPv6Unspecified(), a == local:
		return true
	case a == netip.IPv4Unspecified():
		return local.Is4()
	}
	return a.IsLoopback() && local.IsLoopback()
}

// ownHost reports whether host is a wildcard, localhost, or an address of
// one of this host's interfaces.
func (b *Backend) ownHost(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	a = a.Unmap().WithZone("")
	if a.IsUnspecified() {
		return true
	}
	list := b.InterfaceAddrs
	if list == nil {
		list = net.InterfaceAddrs
	}
	addrs, err := list()
	if err != nil {
		log.Printf("web.listen check: %v", err)
		return false
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if own, ok := netip.AddrFromSlice(ip); ok && own.Unmap() == a {
			return true
		}
	}
	return false
}

func (b *Backend) bind(addr string) error {
	if b.Bind != nil {
		return b.Bind(addr)
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return l.Close()
}

// bindReason is the operating system's reason, without net.Listen's
// "listen tcp <addr>: bind: " in front; the message names the address.
func bindReason(err error) string {
	var se *os.SyscallError
	if errors.As(err, &se) {
		return se.Err.Error()
	}
	var oe *net.OpError
	if errors.As(err, &oe) {
		return oe.Err.Error()
	}
	return err.Error()
}
```

In `internal/daemon/backend.go` replace

```go
	"log"
	"maps"
	"net/http"
```

with

```go
	"log"
	"maps"
	"net"
	"net/http"
```

In `type Backend struct` replace

```go
	WebApplied config.Web
```

with

```go
	WebApplied config.Web
	// InterfaceAddrs lists this host's addresses for the web.listen check;
	// nil means net.InterfaceAddrs.
	InterfaceAddrs func() ([]net.Addr, error)
	// Bind tries addr for the web.listen check and releases it at once; nil
	// means net.Listen and Close.
	Bind func(addr string) error
```

In `PatchConfig` replace

```go
		if w := p.Web; w != nil {
			if w.Listen != nil {
```

with

```go
		if w := p.Web; w != nil {
			if err := b.checkWeb(ctx, w); err != nil {
				return err
			}
			if w.Listen != nil {
```

`Run` sets neither seam, so the daemon uses `net.InterfaceAddrs` and a real bind.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 600 go test -count=1 ./internal/daemon/ ./internal/api/ ./internal/webui/`
Expected: PASS for the three packages. Task 1's `TestPatchConfigWebAndAllowPublic` and `TestPatchChangesWebThroughTheAPI` keep passing: they patch over the socket to `::` on the port in `WebApplied`, so no bind is made.

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 gofmt -l internal/ cmd/` (prints nothing) and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...` (passes).

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/webcheck.go internal/daemon/webcheck_test.go internal/daemon/backend.go
git commit -m "feat(daemon): check web settings when saved"
```

### Task 4: Report visibility and prunes from the runner manager

**Files:**
- Modify: `internal/model/model.go` (`RepoStatus`)
- Modify: `internal/runner/manager.go` (new `Pruning`, `Status`)
- Modify: `internal/runner/visibility.go` (`blockedText`)
- Modify: `internal/daemon/backend.go` (`Manager` interface)
- Test: `internal/daemon/backend_test.go` (`fakeManager`)
- Test: `internal/runner/blocked_test.go` (`darkmemBlocked`)
- Test: `internal/runner/maintenance_test.go` (a new test)
- Test: `internal/runner/visibility_test.go` (a new test)

**Interfaces:**
- Consumes: nothing
- Produces: `runner.(*Manager).Pruning() bool` and `daemon.Manager.Pruning() bool`, with `fakeManager.pruning` as its value in the daemon tests (Contracts: **`runner.(*Manager).Pruning() bool`**); `RepoStatus.Public *bool` (Contracts: **`RepoStatus.Public`**); the new `blockedText` (Contracts: **`blockedText(repo)`**)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

`RepoStatus.Public` comes from `visibilityLocked`, which counts a runs listing that said public as well as `GetRepo`'s answer, the same answer `blockedLocked` uses. `web/src/api/fixtures/status.json` and `status-degraded.json` need no change: `web/fixtures_test.go` builds their `RepoStatus` values without `Public`, and a nil pointer with `omitempty` adds no key. The only other pins of the old blocked text are in past specs and plans under `docs/superpowers/`, which stay as they were written.

- [ ] **Step 1: Write the failing tests**

In `internal/runner/blocked_test.go` replace

```go
const darkmemBlocked = "darkmem is public; ghr serves only private repositories. Set allow_public: true for it in config.yaml and reload to serve it"
```

with

```go
const darkmemBlocked = "darkmem is public; ghr serves only private repositories. Turn on Serve when public on its repository page to serve it"
```

In `internal/runner/maintenance_test.go`, directly above the line `// tickInBackground runs one tick and fails the test if it does not return:`, add

```go
// Pruning is true while a manual or an automatic prune runs, and false for a
// runner update, which holds the same reservation.
func TestPruningReportsBothKindsOfPrune(t *testing.T) {
	h := newHarness(t)
	if h.m.Pruning() {
		t.Fatal("pruning before any prune")
	}
	entered, release := holdOn(t, h, "PruneBuildCacheTo")
	if err := h.m.StartPrune(); err != nil {
		t.Fatal(err)
	}
	waitOrFail(t, entered, "the manual prune")
	if !h.m.Pruning() {
		t.Fatal("not pruning during a manual prune")
	}
	release()
	h.m.Wait()
	if h.m.Pruning() {
		t.Fatal("pruning after the manual prune")
	}

	auto := newHarness(t)
	auto.docker.usage = []int{99, 99, 50}
	entered, release = holdOn(t, auto, "PruneBuildCacheOlderThan")
	auto.m.ticks = 0 // the next tick is tick 1, a disk check
	tickInBackground(t, auto)
	waitOrFail(t, entered, "the automatic prune")
	if !auto.m.Pruning() || auto.m.Maintenance().Running {
		t.Fatalf("automatic prune: pruning %v, maintenance running %v", auto.m.Pruning(), auto.m.Maintenance().Running)
	}
	release()
	auto.m.Wait()
	if auto.m.Pruning() {
		t.Fatal("pruning after the automatic prune")
	}

	h.m.mu.Lock()
	h.m.pruning, h.m.upd.running = true, true // as a runner update's start sets them
	h.m.mu.Unlock()
	if h.m.Pruning() {
		t.Fatal("a runner update reads as a prune")
	}
	h.m.mu.Lock()
	h.m.pruning, h.m.upd.running = false, false
	h.m.mu.Unlock()
}
```

In `internal/runner/visibility_test.go`, directly above the line `// A replaced or reloaded token is the owner saying "look again": every answer`, add

```go
// The status carries the visibility GitHub last reported for each repository,
// nil until it has answered, whether or not the repository is blocked.
func TestStatusReportsVisibility(t *testing.T) {
	h := newHarness(t)
	h.gh.setPublic("darkmem", true)
	public := func(i int) *bool { return h.m.Status().Repos[i].Public }
	if public(0) != nil || public(1) != nil {
		t.Fatalf("before any read: darkcloud %v darkmem %v", public(0), public(1))
	}
	h.m.Tick(context.Background())
	if p := public(0); p == nil || *p {
		t.Fatalf("darkcloud: %v, want false", p)
	}
	if p := public(1); p == nil || !*p {
		t.Fatalf("darkmem: %v, want true", p)
	}
	allowed := h.cfg.Clone()
	allowed.Repo("darkmem").AllowPublic = true
	h.cfg = allowed
	if r := h.m.Status().Repos[1]; r.Public == nil || !*r.Public || r.Error != "" {
		t.Fatalf("served while public: public %v error %q", r.Public, r.Error)
	}
	h.gh.setPublic("darkmem", false)
	h.m.ClearDegraded()
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if p := public(1); p == nil || *p {
		t.Fatalf("darkmem after it turned private: %v, want false", p)
	}
}
```

In `internal/daemon/backend_test.go`, in `type fakeManager struct`, replace

```go
	lastPrune *model.LastPrune // returned by LastPrune
```

with

```go
	lastPrune *model.LastPrune // returned by LastPrune
	pruning   bool             // returned by Pruning
```

and replace

```go
func (f *fakeManager) LastPrune() *model.LastPrune { return f.lastPrune }
```

with

```go
func (f *fakeManager) LastPrune() *model.LastPrune { return f.lastPrune }

func (f *fakeManager) Pruning() bool { return f.pruning }
```

`fakeManager` is the only fake of `daemon.Manager`; `run.go` passes the real `*runner.Manager`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/runner/`
Expected: build failure, `h.m.Pruning undefined (type *Manager has no field or method Pruning, but does have field pruning)` and `r.Public undefined (type model.RepoStatus has no field or method Public)`.

- [ ] **Step 3: Write the implementation**

In `internal/model/model.go`, in `type RepoStatus struct`, replace

```go
	Error          string        `json:"error,omitempty"`
	LastJob        *HistoryEntry `json:"last_job,omitempty"`
}
```

with

```go
	Error          string        `json:"error,omitempty"`
	LastJob        *HistoryEntry `json:"last_job,omitempty"`
	// Public is the visibility GitHub last reported for the repository; nil
	// until GitHub has answered.
	Public *bool `json:"public,omitempty"`
}
```

In `internal/runner/manager.go` replace

```go
func (m *Manager) releasePrune() {
	m.mu.Lock()
	m.pruning = false
	m.mu.Unlock()
}
```

with

```go
func (m *Manager) releasePrune() {
	m.mu.Lock()
	m.pruning = false
	m.mu.Unlock()
}

// Pruning reports whether a manual or automatic prune is running. A runner
// update holds the same reservation and is not a prune.
func (m *Manager) Pruning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pruning && !m.upd.running
}
```

In `Status` replace

```go
		if m.blockedLocked(cfg, r) {
			rs.Error = blockedText(r.Name)
		}
```

with

```go
		if m.blockedLocked(cfg, r) {
			rs.Error = blockedText(r.Name)
		}
		if public, known := m.visibilityLocked(r.Name); known {
			rs.Public = &public
		}
```

In `internal/runner/visibility.go` replace

```go
	return repo + " is public; ghr serves only private repositories. Set allow_public: true for it in config.yaml and reload to serve it"
```

with

```go
	return repo + " is public; ghr serves only private repositories. Turn on Serve when public on its repository page to serve it"
```

In `internal/daemon/backend.go`, in `type Manager interface`, replace

```go
	LastPrune() *model.LastPrune
```

with

```go
	LastPrune() *model.LastPrune
	Pruning() bool
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 600 go test -count=1 ./internal/runner/ ./internal/daemon/ ./internal/model/ ./web/`
Expected: PASS for the four packages; `./web/` (`TestTypeFixtures`) passes with the fixtures unchanged.

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 gofmt -l internal/ cmd/` (prints nothing) and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...` (passes).

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/runner/manager.go internal/runner/visibility.go internal/daemon/backend.go internal/daemon/backend_test.go internal/runner/blocked_test.go internal/runner/maintenance_test.go internal/runner/visibility_test.go
git commit -m "feat(runner): report visibility and prunes"
```

### Task 5: Report the restart state

**Files:**
- Create: `internal/daemon/restart.go`
- Modify: `internal/model/model.go` (`Status`, new `RestartStatus`, `WebListenError`), `internal/daemon/backend.go` (`Backend` fields, `Status`), `internal/daemon/changes.go` (`fingerprints`), `web/src/api/fixtures/status.json`, `web/src/api/fixtures/status-degraded.json`
- Test: `internal/daemon/restart_test.go` (create)

**Interfaces:**
- Consumes: `runner.(*Manager).Pruning() bool` and the `daemon.Manager` method `Pruning() bool` (Task 4)
- Produces: `RestartStatus`, `WebListenError`, `Status.Restart`, `Status.WebListenError`, `Backend.RestartAvailable`, `Backend.WebListenErr`; pending keys and BlockedBy texts (Contracts). Private helpers later tasks call: `webPending(saved, applied config.Web) []string`, `(*Backend).blockedBy(st model.Status) []string`.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

`GET /status` gains the restart block: which saved web settings differ from the ones the listener applied, what daemon work refuses a restart now, whether this process can be restarted through systemd, and the listen string served. The `status` change topic hashes the block, so a change publishes at once. The block is computed by one helper, `restartStatus(st)`, which both `Status()` and `fingerprints()` call with the manager status they already hold, so the fingerprint still reads the manager's status once. The storage queue is read through `storageOps()`, which treats a panicking snapshot as an empty queue: `TestAPanickingTopicIsLeftOut` requires that a panicking storage snapshot leaves the `status` topic in place.

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/restart_test.go`:

```go
package daemon

import (
	"os"
	"slices"
	"testing"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// restartManager is fakeManager with a runner update that can be running;
// the prune flag is fakeManager's own pruning field.
type restartManager struct {
	*fakeManager
	update bool
}

func (r *restartManager) Status() model.Status {
	st := r.fakeManager.Status()
	st.RunnerUpdate.Running = r.update
	return st
}

func setWeb(t *testing.T, b *Backend, w config.Web) {
	t.Helper()
	if err := b.update(func(c *config.Config) error { c.Web = w; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestRestartPendingComparesTheSavedWebBlock(t *testing.T) {
	b, _, _ := newBackend(t)
	b.WebApplied = config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan", "GHR.example"}}
	for _, tc := range []struct {
		name string
		web  config.Web
		want []string
	}{
		{"the applied block", config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan", "GHR.example"}}, nil},
		{"hosts reordered, recased and repeated", config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.example", "GHR.LAN", "ghr.lan"}}, nil},
		{"a new listen", config.Web{Listen: "0.0.0.0:9090", Hosts: []string{"ghr.lan", "ghr.example"}}, []string{"web.listen"}},
		{"a host removed", config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan"}}, []string{"web.hosts"}},
		{"both", config.Web{Listen: "127.0.0.1:8080"}, []string{"web.listen", "web.hosts"}},
	} {
		setWeb(t, b, tc.web)
		st := b.Status()
		if !slices.Equal(st.Restart.Pending, tc.want) {
			t.Errorf("%s: pending %v, want %v", tc.name, st.Restart.Pending, tc.want)
		}
		if st.Restart.WebListen != "0.0.0.0:8080" {
			t.Errorf("%s: web_listen %q", tc.name, st.Restart.WebListen)
		}
	}

	setWeb(t, b, b.WebApplied)
	if err := os.WriteFile(b.Store.ConfigPath, []byte(cfgYAML+"web:\n  listen: 0.0.0.0:9090\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := b.Status().Restart.Pending; !slices.Equal(got, []string{"web.listen", "web.hosts"}) {
		t.Fatalf("after a hand edit and a reload: %v", got)
	}
}

func TestRestartBlockedBy(t *testing.T) {
	b, m, _ := newBackend(t)
	rm := &restartManager{fakeManager: m}
	space := &fakeStorage{}
	b.M, b.Space = rm, space
	if got := b.Status().Restart.BlockedBy; len(got) != 0 {
		t.Fatalf("an idle daemon is blocked by %v", got)
	}
	space.snap.Operations = model.Operations{Current: &model.Operation{Kind: "install", Target: "node 24"}, Queued: 1}
	m.pruning, rm.update = true, true
	want := []string{"installing node 24", "1 queued", "pruning", "updating the runner software"}
	if got := b.Status().Restart.BlockedBy; !slices.Equal(got, want) {
		t.Fatalf("everything at once: %v, want %v", got, want)
	}
	m.pruning, rm.update = false, false
	for kind, text := range map[string]string{"remove": "removing node 22", "clear": "clearing node 22", "measure": "measure node 22"} {
		space.snap.Operations = model.Operations{Current: &model.Operation{Kind: kind, Target: "node 22"}}
		if got := b.Status().Restart.BlockedBy; !slices.Equal(got, []string{text}) {
			t.Errorf("%s: %v, want %q", kind, got, text)
		}
	}
	space.snap.Operations = model.Operations{Queued: 2}
	if got := b.Status().Restart.BlockedBy; !slices.Equal(got, []string{"2 queued"}) {
		t.Fatalf("queued only: %v", got)
	}
	space.snap.Operations = model.Operations{}
	m.pruning = true
	if got := b.Status().Restart.BlockedBy; !slices.Equal(got, []string{"pruning"}) {
		t.Fatalf("a prune: %v", got)
	}
}

func TestRestartAvailabilityAndListenError(t *testing.T) {
	b, _, _ := newBackend(t)
	if st := b.Status(); st.Restart.Available || st.WebListenError != nil {
		t.Fatalf("by default: %+v %+v", st.Restart, st.WebListenError)
	}
	b.RestartAvailable = true
	b.WebListenErr = &model.WebListenError{Address: "10.0.0.5:8080", Reason: "cannot assign requested address", ServingOn: "0.0.0.0:8080"}
	st := b.Status()
	if !st.Restart.Available || st.WebListenError == nil || *st.WebListenError != *b.WebListenErr {
		t.Fatalf("set at start: %+v %+v", st.Restart, st.WebListenError)
	}
}

func TestRestartChangesPublishStatus(t *testing.T) {
	b, m, _ := newBackend(t)
	prev := b.fingerprints()
	m.pruning = true
	cur := b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("a prune changed %v", got)
	}
	b.WebListenErr = &model.WebListenError{Address: "10.0.0.5:8080", Reason: "cannot assign requested address"}
	prev, cur = cur, b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("a listen error changed %v", got)
	}
	b.RestartAvailable = true
	prev, cur = cur, b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("availability changed %v", got)
	}
	setWeb(t, b, config.Web{Listen: "0.0.0.0:9090"})
	prev, cur = cur, b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"config", "status"}) {
		t.Fatalf("a pending listen changed %v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/daemon/ -run 'TestRestart'`
Expected: FAIL, build failed, starting with `st.Restart undefined (type model.Status has no field or method Restart)` and `b.Status().Restart undefined (type model.Status has no field or method Restart)`, then `too many errors`.

- [ ] **Step 3: Write the implementation**

In `internal/model/model.go`, replace

```go
	// SetupPending is true until first-run setup is finished.
	SetupPending bool `json:"setup_pending,omitempty"`
}
```

with

```go
	// SetupPending is true until first-run setup is finished.
	SetupPending bool          `json:"setup_pending,omitempty"`
	Restart      RestartStatus `json:"restart"`
	// WebListenError is set when the configured web.listen failed to bind at
	// start.
	WebListenError *WebListenError `json:"web_listen_error,omitempty"`
}

type RestartStatus struct {
	// Pending names the settings whose saved value differs from the one the
	// running daemon applied: "web.listen", "web.hosts".
	Pending []string `json:"pending,omitempty"`
	// BlockedBy names the daemon work that refuses a restart now.
	BlockedBy []string `json:"blocked_by,omitempty"`
	// Available is false when this process is not ghr.service's main process.
	Available bool `json:"available"`
	// WebListen is the listen value the running web listener serves; empty
	// when none runs.
	WebListen string `json:"web_listen,omitempty"`
}

type WebListenError struct {
	Address   string `json:"address"`
	Reason    string `json:"reason"`
	ServingOn string `json:"serving_on,omitempty"`
}
```

In `internal/daemon/backend.go`, in `Backend`, replace

```go
	// WebApplied is the web block the running listener started with. Only a
	// restart applies a change, so a reload that changes it warns.
	WebApplied config.Web
```

with

```go
	// WebApplied is the web block the running listener started with; its
	// Listen is the string bound, empty when no listener runs. Only a
	// restart applies a change, so a reload that changes it warns.
	WebApplied config.Web
	// WebListenErr is set when web.listen failed to bind at start.
	WebListenErr *model.WebListenError
	// RestartAvailable is true when this process is ghr.service's main
	// process, so systemd can restart it.
	RestartAvailable bool
```

In `Status`, replace

```go
	if b.GH != nil {
		if l := b.GH.TokenMeta().RateLimit; l != nil {
			st.RateLimit = *l
		}
	}
	return st
}
func (b *Backend) EventsAfter(seq int64) []model.Event { return b.Events.After(seq) }
```

with

```go
	if b.GH != nil {
		if l := b.GH.TokenMeta().RateLimit; l != nil {
			st.RateLimit = *l
		}
	}
	st.Restart = b.restartStatus(st)
	st.WebListenError = b.WebListenErr
	return st
}
func (b *Backend) EventsAfter(seq int64) []model.Event { return b.Events.After(seq) }
```

In `internal/daemon/changes.go`, in `fingerprints`, replace

```go
		st := status()
		st.WebSetupRequired, st.SetupPending = b.setupFlags()
```

with

```go
		st := status()
		st.WebSetupRequired, st.SetupPending = b.setupFlags()
		st.Restart, st.WebListenError = b.restartStatus(st), b.WebListenErr
```

Create `internal/daemon/restart.go`:

```go
package daemon

import (
	"slices"
	"strconv"
	"strings"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// restartStatus is the status's restart block; st is the manager's status
// it is served with.
func (b *Backend) restartStatus(st model.Status) model.RestartStatus {
	return model.RestartStatus{
		Pending:   webPending(b.Store.Config().Web, b.WebApplied),
		BlockedBy: b.blockedBy(st),
		Available: b.RestartAvailable,
		WebListen: b.WebApplied.Listen,
	}
}

// webPending names the web settings whose saved value differs from the one
// the running listener applied. Hosts compare as sets ignoring case, as the
// listener matches them.
func webPending(saved, applied config.Web) []string {
	var out []string
	if saved.Listen != applied.Listen {
		out = append(out, "web.listen")
	}
	if !slices.Equal(hostSet(saved.Hosts), hostSet(applied.Hosts)) {
		out = append(out, "web.hosts")
	}
	return out
}

func hostSet(hosts []string) []string {
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, strings.ToLower(h))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// opVerbs words a storage operation as the UI's queue line does.
var opVerbs = map[string]string{"install": "installing", "remove": "removing", "clear": "clearing"}

// blockedBy names the daemon work a restart would cut, in the order the UI
// shows it. Running jobs are not on it: their units outlive the daemon, and
// the next start adopts them.
func (b *Backend) blockedBy(st model.Status) []string {
	var out []string
	ops := b.storageOps()
	if c := ops.Current; c != nil {
		verb, ok := opVerbs[c.Kind]
		if !ok {
			verb = c.Kind
		}
		out = append(out, verb+" "+c.Target)
	}
	if ops.Queued > 0 {
		out = append(out, strconv.Itoa(ops.Queued)+" queued")
	}
	if b.M.Pruning() {
		out = append(out, "pruning")
	}
	if st.RunnerUpdate.Running {
		out = append(out, "updating the runner software")
	}
	return out
}

// storageOps reads the storage queue. A snapshot that panics reads as an
// empty queue, so it cannot take the status, or its change topic, with it.
func (b *Backend) storageOps() (ops model.Operations) {
	defer func() { _ = recover() }()
	return b.Space.Snapshot().Operations
}
```

`restart` is not `omitempty`, so both status fixtures gain the block. In `web/src/api/fixtures/status.json` and in `web/src/api/fixtures/status-degraded.json`, replace the last two lines

```json
  "web_setup_required": false
}
```

with

```json
  "web_setup_required": false,
  "restart": {
    "available": false
  }
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go test -count=1 ./internal/daemon/ ./internal/model/ ./internal/api/ ./web/`
Expected: PASS for all four (`TestAPanickingTopicIsLeftOut` and `TestFingerprintsReadTheManagerStatusOnce` included). If `TestTypeFixtures` fails, compare the fixture by hand with the step 3 block; do not regenerate it with `GHR_UPDATE_FIXTURES=1` unless the only difference is the restart block.

Then `PATH=/usr/local/go/bin:$PATH gofmt -w internal/model/model.go internal/daemon/backend.go internal/daemon/changes.go internal/daemon/restart.go internal/daemon/restart_test.go`, then `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (prints nothing) and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go vet ./internal/... ./cmd/...` (passes).

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/daemon/backend.go internal/daemon/changes.go internal/daemon/restart.go internal/daemon/restart_test.go web/src/api/fixtures/status.json web/src/api/fixtures/status-degraded.json
git commit -m "feat(daemon): report restart state in the status"
```

### Task 6: Write the restart warning once per change

**Files:**
- Modify: `internal/daemon/backend.go` (`Backend` private fields, `Reload`, `PatchConfig`), `internal/daemon/restart.go`
- Test: `internal/daemon/backend_test.go` (`TestReloadWarnsOnWebChange`), `internal/daemon/restart_test.go`

**Interfaces:**
- Consumes: `webPending` (Task 5); `daemon.Backend.PatchConfig(ctx, p)`, `WebPatch` (Task 1); the `Backend.Bind` seam (Task 3)
- Produces: the restart event text `web settings changed; restart ghr to apply` (Contracts), written as `config: web settings changed; restart ghr to apply` (see Notes); `(*Backend).warnPending() []string`

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

Today `Reload` writes the restart warning as an event on every reload until the restart. After this task the reload's answer still lists it on every reload while `Restart.Pending` is not empty (`TestRunServesTheWebUI` keeps pinning that), but the event is written only when the pending list becomes non-empty or changes while non-empty. An empty list resets the record, so the warning returns with the next change. A successful `PatchConfig` with a `web` block follows the same rule. This closes rows 232 and 263 of `docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md`.

The pending list is read after the store's update lock is released. Two web changes could therefore commit and then be checked in the other order, and a transition would be lost. To prevent that, `Backend.pendingMu` is held across the whole of `Reload`, and across the whole of a `PatchConfig` that carries `web`, including the check and the event. The concurrency test uses the existing `Backend.Wake` seam, which `updateThen` calls after the commit and before the check, to hold the first patch at that point; no new seam is needed. The test passes deterministically with the lock. Its 200 ms wait only bounds how long an implementation without the lock gets to let the second patch overtake.

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/backend_test.go`, replace the whole of `TestReloadWarnsOnWebChange`:

```go
func TestReloadWarnsOnWebChange(t *testing.T) {
	b, _, _ := newBackend(t)
	write := func(extra string) {
		if err := os.WriteFile(b.Store.ConfigPath, []byte(cfgYAML+extra), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const warning = "web settings changed; restart ghr to apply"
	write("web:\n  listen: 0.0.0.0:8080\n")
	ws, err := b.Reload()
	if err != nil || !slices.Contains(ws, warning) {
		t.Fatalf("changed listen: %v %v", ws, err)
	}
	found := false
	for _, e := range b.Events.After(0) {
		if e.Level == "warn" && strings.Contains(e.Msg, warning) {
			found = true
		}
	}
	if !found {
		t.Fatal("no warn event for the web change")
	}
	write("web:\n  hosts: [ghr.lan]\n")
	if ws, err := b.Reload(); err != nil || !slices.Contains(ws, warning) {
		t.Fatalf("changed hosts: %v %v", ws, err)
	}
	write("")
	if ws, err := b.Reload(); err != nil || len(ws) != 0 {
		t.Fatalf("reverted web still warns: %v %v", ws, err)
	}
}
```

with

```go
// Every reload's answer lists the web warning while a web setting waits for
// a restart; the event is written only when the pending list changes, and
// again after it empties and returns.
func TestReloadWarnsOnWebChange(t *testing.T) {
	b, _, _ := newBackend(t)
	write := func(extra string) {
		if err := os.WriteFile(b.Store.ConfigPath, []byte(cfgYAML+extra), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const warning = "web settings changed; restart ghr to apply"
	reload := func(what string, wantWarning bool, wantEvents int) {
		t.Helper()
		ws, err := b.Reload()
		if err != nil || slices.Contains(ws, warning) != wantWarning {
			t.Fatalf("%s: warnings %v, err %v", what, ws, err)
		}
		if n := restartWarnings(b); n != wantEvents {
			t.Fatalf("%s: %d warning events, want %d", what, n, wantEvents)
		}
	}
	write("web:\n  listen: 0.0.0.0:8080\n")
	reload("changed listen", true, 1)
	reload("the same change again", true, 1)
	write("web:\n  listen: 0.0.0.0:8080\n  hosts: [ghr.lan]\n")
	reload("hosts changed too", true, 2)
	write("web:\n  hosts: [ghr.lan]\n")
	reload("listen reverted, hosts still changed", true, 3)
	write("")
	reload("reverted", false, 3)
	write("web:\n  listen: 0.0.0.0:8080\n")
	reload("the change made again", true, 4)
}
```

In `internal/daemon/restart_test.go`, replace the import block

```go
import (
	"os"
	"slices"
	"testing"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)
```

with

```go
import (
	"context"
	"os"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)
```

and append:

```go
// restartWarnings counts the restart warning's events.
func restartWarnings(b *Backend) int {
	n := 0
	for _, e := range b.Events.After(0) {
		if e.Level == "warn" && e.Msg == "config: "+restartWarning {
			n++
		}
	}
	return n
}

func TestPatchingWebSettingsWarnsOncePerChange(t *testing.T) {
	b, _, _ := newBackend(t)
	b.Bind = func(string) error { return nil }
	setWeb(t, b, config.Web{Listen: "0.0.0.0:8080"})
	b.WebApplied = b.Store.Config().Web
	patch := func(what string, w model.WebPatch, wantEvents int) {
		t.Helper()
		if err := b.PatchConfig(context.Background(), model.ConfigPatch{Web: &w}); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if n := restartWarnings(b); n != wantEvents {
			t.Fatalf("%s: %d warning events, want %d", what, n, wantEvents)
		}
	}
	moved, back := "0.0.0.0:9090", "0.0.0.0:8080"
	patch("a new listen", model.WebPatch{Listen: &moved}, 1)
	patch("the same listen again", model.WebPatch{Listen: &moved}, 1)
	patch("a host as well", model.WebPatch{Hosts: &[]string{"ghr.lan"}}, 2)
	patch("both reverted", model.WebPatch{Listen: &back, Hosts: &[]string{}}, 2)
	patch("the listen moved again", model.WebPatch{Listen: &moved}, 3)
}

// A web patch is checked for the restart warning before another web patch
// can commit. The first patch is held in Wake, after its commit and before
// its check; a second patch that reverts it must wait, or the first patch's
// transition is never written.
func TestConcurrentWebPatchesWarnInCommitOrder(t *testing.T) {
	b, _, _ := newBackend(t)
	b.Bind = func(string) error { return nil }
	setWeb(t, b, config.Web{Listen: "0.0.0.0:8080"})
	b.WebApplied = b.Store.Config().Web
	held, release := make(chan struct{}), make(chan struct{})
	var wakes atomic.Int32
	b.Wake = func() {
		if wakes.Add(1) == 1 {
			close(held)
			<-release
		}
	}
	moved, back := "0.0.0.0:9090", "0.0.0.0:8080"
	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		first <- b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Listen: &moved}})
	}()
	<-held
	go func() {
		second <- b.PatchConfig(context.Background(), model.ConfigPatch{Web: &model.WebPatch{Listen: &back}})
	}()
	select {
	case err := <-second:
		t.Fatalf("the second web patch finished while the first was unchecked: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	for _, done := range []chan error{first, second} {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if n := restartWarnings(b); n != 1 {
		t.Fatalf("%d warning events, want 1 for the first patch's change", n)
	}
	if got := b.Status().Restart.Pending; len(got) != 0 {
		t.Fatalf("pending after the revert: %v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/daemon/ -run 'TestReloadWarnsOnWebChange|TestPatchingWebSettingsWarnsOncePerChange|TestConcurrentWebPatchesWarnInCommitOrder'`
Expected: FAIL, build failed: `undefined: restartWarning`.

- [ ] **Step 3: Write the implementation**

Append to `internal/daemon/restart.go`:

```go
const restartWarning = "web settings changed; restart ghr to apply"

// warnPending writes the restart warning when the pending web settings
// become non-empty or change, and returns them. An empty list resets the
// record, so the warning returns with the next change. The caller holds
// b.pendingMu across the change it checks.
func (b *Backend) warnPending() []string {
	pending := webPending(b.Store.Config().Web, b.WebApplied)
	if len(pending) > 0 && !slices.Equal(pending, b.pendingWarned) {
		b.Events.Add("warn", "", "config: %s", restartWarning)
	}
	b.pendingWarned = pending
	return pending
}
```

In `internal/daemon/backend.go`, at the end of `Backend`, replace

```go
	actions actionsCache
	wfFiles wfCache
	details detailCache
	logs    logCache
}
```

with

```go
	actions actionsCache
	wfFiles wfCache
	details detailCache
	logs    logCache

	// pendingMu is held across a change that can move the pending web
	// settings (a reload, a web patch) and its restart warning check, so
	// each committed list is checked in commit order. pendingWarned is the
	// list the warning was last written for.
	pendingMu     sync.Mutex
	pendingWarned []string
}
```

In `Reload`, replace

```go
func (b *Backend) Reload() ([]string, error) {
	warnings, added, tokenChanged, err := b.Store.Reload()
```

with

```go
func (b *Backend) Reload() ([]string, error) {
	b.pendingMu.Lock()
	defer b.pendingMu.Unlock()
	warnings, added, tokenChanged, err := b.Store.Reload()
```

and replace

```go
	// The web warning is not the store's, so it is written on every reload
	// until the restart it asks for.
	if w := b.Store.Config().Web; w.Listen != b.WebApplied.Listen || !slices.Equal(w.Hosts, b.WebApplied.Hosts) {
		const restart = "web settings changed; restart ghr to apply"
		warnings, added = append(warnings, restart), append(added, restart)
	}
	for _, w := range added {
		b.Events.Add("warn", "", "config: %s", w)
	}
```

with

```go
	for _, w := range added {
		b.Events.Add("warn", "", "config: %s", w)
	}
	// The web warning is not the store's. The answer lists it on every reload
	// until the restart it asks for; its event is written once per change.
	if len(b.warnPending()) > 0 {
		warnings = append(warnings, restartWarning)
	}
```

In `PatchConfig`, replace

```go
func (b *Backend) PatchConfig(ctx context.Context, p model.ConfigPatch) error {
```

with

```go
func (b *Backend) PatchConfig(ctx context.Context, p model.ConfigPatch) error {
	if p.Web != nil {
		b.pendingMu.Lock()
		defer b.pendingMu.Unlock()
	}
```

and replace

```go
	for _, name := range cancelled {
		b.Events.Add("info", name, "repo resumed; removal cancelled")
	}
```

with

```go
	for _, name := range cancelled {
		b.Events.Add("info", name, "repo resumed; removal cancelled")
	}
	if p.Web != nil {
		b.warnPending()
	}
```

`slices` stays imported in `backend.go`: `updateThen` and `PatchConfig` use it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/daemon/`
Expected: PASS (`TestRunServesTheWebUI`, `TestReloadRepeatsNoWarning` included).

Then `PATH=/usr/local/go/bin:$PATH gofmt -w internal/daemon/backend.go internal/daemon/restart.go internal/daemon/backend_test.go internal/daemon/restart_test.go`, `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (prints nothing), and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go vet ./internal/... ./cmd/...` (passes).

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/backend.go internal/daemon/restart.go internal/daemon/backend_test.go internal/daemon/restart_test.go
git commit -m "fix(daemon): warn once per web settings change"
```

### Task 7: Restart through systemd

**Files:**
- Modify: `internal/daemon/restart.go`, `internal/daemon/backend.go` (`Backend.Restart`), `internal/daemon/run.go` (`Options`, `DefaultOptions`, `start`), `internal/api/server.go` (`Backend`, `POST /restart`), `internal/api/api_test.go` (`fakeBackend` fields)
- Test: `internal/daemon/restart_test.go`, `internal/daemon/run_test.go`, `internal/api/restart_test.go` (create)

**Interfaces:**
- Consumes: `(*Backend).blockedBy` (Task 5); `api.Browser`, `api.WithBrowser` (Task 2)
- Produces: `daemon.Backend.RequestRestart(ctx) (run func(), err error)`, `api.Backend.RequestRestart`, `Backend.Restart func(ctx context.Context) error`, `daemon.Options.SelfUnit`, `daemon.Options.Restart` (Contracts); `POST /restart`; the events `restart requested`, `restart requested from the web UI`, `restart failed: <err>` and `restart cancelled: <blockers> started`; private `selfUnitCheck(run system.Runner) func(ctx context.Context) bool` and `restartUnit(run system.Runner) func(ctx context.Context) error`

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

`POST /restart` refuses with 409 when this process is not ghr.service's main process or while daemon work blocks a restart. Otherwise it writes `restart requested` (`restart requested from the web UI` for a browser), answers 202, flushes the answer, and only then runs `systemctl restart --no-block ghr.service` through `system.Exec`, with a context the end of the request cannot cancel. A failure is an error event `restart failed: <err>`, and the daemon keeps running. Work can start between the check and the restart, for example a runner update or a storage operation. `run` therefore checks the blockers again just before it calls the seam. If any appeared, it does not restart and writes the warn event `restart cancelled: <blockers, joined by ", "> started`. Nothing is reserved; that was the owner's ruling. The systemd defaults are built over an injected `system.Runner` (`selfUnitCheck(run)`, `restartUnit(run)`), which `DefaultOptions` gives `system.Exec`, so tests can check the exact commands. The answer has no body. The web client already treats a 202 as success with no body (`web/src/api/client.ts:63-64`). Availability is worked out once at start through `Options.SelfUnit`. The default implementation checks `INVOCATION_ID` and then compares `systemctl show -p MainPID --value ghr.service` with this process's PID, under a 10-second timeout so a hung systemctl cannot hold the start.

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/restart_test.go`, replace the import block (as Task 6 left it) with

```go
import (
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/system"
)
```

and append:

```go
func TestRequestRestart(t *testing.T) {
	b, m, _ := newBackend(t)
	rm := &restartManager{fakeManager: m}
	b.M = rm
	var got []context.Context
	b.Restart = func(ctx context.Context) error { got = append(got, ctx); return nil }
	restartEvents := func() []string {
		var out []string
		for _, e := range b.Events.After(0) {
			if strings.HasPrefix(e.Msg, "restart ") {
				out = append(out, e.Level+" "+e.Msg)
			}
		}
		return out
	}

	if _, err := b.RequestRestart(context.Background()); apiStatus(err) != http.StatusConflict ||
		apiMsg(err) != "ghr is not running as ghr.service; restart it by hand" {
		t.Fatalf("not the unit's main process: %v", err)
	}
	b.RestartAvailable = true
	m.pruning, rm.update = true, true
	if _, err := b.RequestRestart(context.Background()); apiStatus(err) != http.StatusConflict ||
		apiMsg(err) != "waits for: pruning, updating the runner software" {
		t.Fatalf("blocked: %v", err)
	}
	if evs := restartEvents(); len(evs) != 0 || len(got) != 0 {
		t.Fatalf("a refused restart wrote %v and ran %d times", evs, len(got))
	}
	m.pruning, rm.update = false, false

	ctx, cancel := context.WithCancel(api.WithBrowser(context.Background(), "ghr.lan:8080"))
	run, err := b.RequestRestart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("the restart ran before run was called")
	}
	cancel()
	run()
	if len(got) != 1 {
		t.Fatalf("run restarted %d times", len(got))
	}
	if err := got[0].Err(); err != nil {
		t.Fatalf("the restart's context ended with the request: %v", err)
	}

	run, err = b.RequestRestart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run()
	b.Restart = func(context.Context) error {
		return errors.New("systemctl restart: exit status 5: Unit ghr.service not loaded.")
	}
	run, err = b.RequestRestart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	run()
	want := []string{
		"info restart requested from the web UI",
		"info restart requested",
		"info restart requested",
		"error restart failed: systemctl restart: exit status 5: Unit ghr.service not loaded.",
	}
	if evs := restartEvents(); !slices.Equal(evs, want) {
		t.Fatalf("events %q, want %q", evs, want)
	}
}

// Work that starts after the request was accepted cancels the restart: run
// checks again and does not call the seam.
func TestRestartCancelledWhenWorkStarts(t *testing.T) {
	b, m, _ := newBackend(t)
	b.RestartAvailable = true
	restarts := 0
	b.Restart = func(context.Context) error { restarts++; return nil }
	run, err := b.RequestRestart(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m.pruning = true
	b.Space = &fakeStorage{snap: model.Storage{Operations: model.Operations{Current: &model.Operation{Kind: "install", Target: "node 24"}}}}
	run()
	if restarts != 0 {
		t.Fatal("the restart ran while work had started")
	}
	evs := b.Events.After(0)
	last := evs[len(evs)-1]
	if last.Level != "warn" || last.Msg != "restart cancelled: installing node 24, pruning started" {
		t.Fatalf("last event %s %q", last.Level, last.Msg)
	}
}

// recordRunner is a system.Runner that records each command and answers
// with out and err.
func recordRunner(calls *[]string, out string, err error) system.Runner {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		return []byte(out), err
	}
}

func TestSelfUnitCheck(t *testing.T) {
	pid := strconv.Itoa(os.Getpid())
	var calls []string
	t.Setenv("INVOCATION_ID", "")
	if selfUnitCheck(recordRunner(&calls, pid+"\n", nil))(context.Background()) || len(calls) != 0 {
		t.Fatalf("without INVOCATION_ID: ran %q", calls)
	}
	t.Setenv("INVOCATION_ID", "0123abcd")
	for _, tc := range []struct {
		name, out string
		err       error
		want      bool
	}{
		{"this process", pid + "\n", nil, true},
		{"another process", "1\n", nil, false},
		{"no main process", "0\n", nil, false},
		{"a failed command", pid + "\n", errors.New("systemctl show: exit status 1: Unit ghr.service not loaded."), false},
	} {
		calls = nil
		got := selfUnitCheck(recordRunner(&calls, tc.out, tc.err))(context.Background())
		if got != tc.want || !slices.Equal(calls, []string{"systemctl show -p MainPID --value ghr.service"}) {
			t.Errorf("%s: %v after %q, want %v", tc.name, got, calls, tc.want)
		}
	}
}

func TestRestartUnit(t *testing.T) {
	var calls []string
	if err := restartUnit(recordRunner(&calls, "", nil))(context.Background()); err != nil ||
		!slices.Equal(calls, []string{"systemctl restart --no-block ghr.service"}) {
		t.Fatalf("ran %q: %v", calls, err)
	}
	failure := errors.New("systemctl restart: exit status 5: Unit ghr.service not loaded.")
	if err := restartUnit(recordRunner(&calls, "", failure))(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("the command's error came back as %v", err)
	}
	if o := DefaultOptions(); o.SelfUnit == nil || o.Restart == nil {
		t.Fatal("the default options lack the restart seams")
	}
}
```

In `internal/daemon/run_test.go`, directly above `func TestRunServesStorageAndSweepsTheToolCache`, add:

```go
func TestRunRestartsThroughTheSeam(t *testing.T) {
	o := testOptions(t, cfgYAML)
	restarted := make(chan error, 1)
	o.SelfUnit = func(context.Context) bool { return true }
	o.Restart = func(ctx context.Context) error {
		restarted <- ctx.Err()
		return nil
	}
	startRun(t, o)
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "a status that allows a restart", func() bool {
		st, err := c.Status(context.Background())
		return err == nil && st.Restart.Available && len(st.Restart.BlockedBy) == 0
	})
	code, body, err := socketCall(o.Socket, http.MethodPost, "/restart", "")
	if err != nil || code != http.StatusAccepted {
		t.Fatalf("restart: %d %s %v", code, body, err)
	}
	select {
	case err := <-restarted:
		if err != nil {
			t.Fatalf("the restart ran with an ended context: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the restart seam never ran")
	}
	if msgs := eventMsgs(t, o.Socket); !strings.Contains(msgs, "restart requested") {
		t.Fatalf("no restart event in %q", msgs)
	}
}
```

In `internal/api/api_test.go`, in `fakeBackend`, replace

```go
	subMu         sync.Mutex     // guards subs
	subs          []*changes.Sub // subscriptions Subscribe opened
}
```

with

```go
	subMu         sync.Mutex     // guards subs
	subs          []*changes.Sub // subscriptions Subscribe opened
	restartErr    error          // returned by RequestRestart
	restartRun    func()         // RequestRestart's run; nil runs nothing
	restarts      int            // RequestRestart calls
}
```

Create `internal/api/restart_test.go`:

```go
package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func (f *fakeBackend) RequestRestart(context.Context) (func(), error) {
	f.restarts++
	if f.restartErr != nil {
		return nil, f.restartErr
	}
	if f.restartRun == nil {
		return func() {}, nil
	}
	return f.restartRun, nil
}

// The 202 reaches the client before the restart runs: run waits for the
// client to have it, which it never would if the answer went out only when
// the handler returned.
func TestRestartAnswersBeforeItRuns(t *testing.T) {
	c, b := setup(t)
	answered, ran := make(chan struct{}), make(chan struct{})
	b.restartRun = func() {
		select {
		case <-answered:
		case <-time.After(3 * time.Second):
			t.Error("the restart ran before the 202 reached the client")
		}
		close(ran)
	}
	resp, err := c.HTTP.Post(c.Base+"/restart", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	close(answered)
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the restart never ran")
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted || b.restarts != 1 {
		t.Fatalf("status %d, %d requests", resp.StatusCode, b.restarts)
	}
}

func TestRestartRefused(t *testing.T) {
	c, b := setup(t)
	ran := false
	b.restartRun = func() { ran = true }
	for _, msg := range []string{
		"ghr is not running as ghr.service; restart it by hand",
		"waits for: installing node 24, 1 queued",
	} {
		b.restartErr = Conflict(msg)
		resp, err := c.HTTP.Post(c.Base+"/restart", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), msg) {
			t.Errorf("%s: %d %s", msg, resp.StatusCode, body)
		}
	}
	if ran {
		t.Fatal("a refused restart ran")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/api/ -run 'TestRestart'`
Expected: FAIL, because no route exists yet. `TestRestartAnswersBeforeItRuns` fails after its 5-second wait with `the restart never ran`. `TestRestartRefused` reports `ghr is not running as ghr.service; restart it by hand: 404 404 page not found` and the same for `waits for: installing node 24, 1 queued`.

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/daemon/ -run 'TestRequestRestart|TestRestartCancelledWhenWorkStarts|TestSelfUnitCheck|TestRestartUnit|TestRunRestartsThroughTheSeam'`
Expected: FAIL, build failed: `b.Restart undefined (type *Backend has no field or method Restart)`, `b.RequestRestart undefined (type *Backend has no field or method RequestRestart)`, then `too many errors`; `undefined: selfUnitCheck` and `undefined: restartUnit` sit behind them.

- [ ] **Step 3: Write the implementation**

In `internal/api/server.go`, in `Backend`, replace

```go
	Reload() ([]string, error)
```

with

```go
	Reload() ([]string, error)
	// RequestRestart refuses a restart that cannot run now; otherwise run
	// restarts ghr, and the caller answers its request first.
	RequestRestart(ctx context.Context) (run func(), err error)
```

In `NewServer`, replace

```go
	mux.HandleFunc("POST /reload", func(w http.ResponseWriter, r *http.Request) {
		ws, err := b.Reload()
		respond(w, ws, err)
	})
```

with

```go
	mux.HandleFunc("POST /reload", func(w http.ResponseWriter, r *http.Request) {
		ws, err := b.Reload()
		respond(w, ws, err)
	})
	mux.HandleFunc("POST /restart", func(w http.ResponseWriter, r *http.Request) {
		run, err := b.RequestRestart(r.Context())
		if err != nil {
			respond(w, nil, err)
			return
		}
		// The answer goes out before systemd is asked: the stop it queues
		// ends this process.
		w.WriteHeader(http.StatusAccepted)
		http.NewResponseController(w).Flush()
		run()
	})
```

In `internal/daemon/backend.go`, in `Backend`, replace

```go
	// RestartAvailable is true when this process is ghr.service's main
	// process, so systemd can restart it.
	RestartAvailable bool
```

with

```go
	// RestartAvailable is true when this process is ghr.service's main
	// process, so systemd can restart it.
	RestartAvailable bool
	// Restart asks systemd to restart ghr.service without waiting for it.
	Restart func(ctx context.Context) error
```

In `internal/daemon/restart.go`, replace the import block

```go
import (
	"slices"
	"strconv"
	"strings"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)
```

with

```go
import (
	"context"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/system"
)
```

and append:

```go
// selfUnit is the systemd unit a restart from the UI restarts.
const selfUnit = "ghr.service"

// selfUnitCheck is the default Options.SelfUnit: whether this process is
// selfUnit's main process, asked through run. INVOCATION_ID alone is not
// enough: every process a unit starts inherits it, a job's processes on a
// runner unit among them.
func selfUnitCheck(run system.Runner) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		if os.Getenv("INVOCATION_ID") == "" {
			return false
		}
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		out, err := run(ctx, "systemctl", "show", "-p", "MainPID", "--value", selfUnit)
		return err == nil && strings.TrimSpace(string(out)) == strconv.Itoa(os.Getpid())
	}
}

// restartUnit is the default Options.Restart: it queues a restart of
// selfUnit through run and returns without waiting for it.
func restartUnit(run system.Runner) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		_, err := run(ctx, "systemctl", "restart", "--no-block", selfUnit)
		return err
	}
}

// RequestRestart refuses a restart that cannot run now. Otherwise it writes
// the request's event and returns run, which asks systemd for the restart;
// the caller answers its request before calling run. Work that starts in
// between cancels the restart: nothing reserves the daemon for it.
func (b *Backend) RequestRestart(ctx context.Context) (run func(), err error) {
	if !b.RestartAvailable {
		return nil, api.Conflict("ghr is not running as ghr.service; restart it by hand")
	}
	if blocked := b.blockedBy(b.M.Status()); len(blocked) > 0 {
		return nil, api.Conflict("waits for: " + strings.Join(blocked, ", "))
	}
	if _, ok := api.Browser(ctx); ok {
		b.Events.Add("info", "", "restart requested from the web UI")
	} else {
		b.Events.Add("info", "", "restart requested")
	}
	// The request ends once it is answered; the restart must not end with it.
	detached := context.WithoutCancel(ctx)
	return func() {
		if blocked := b.blockedBy(b.M.Status()); len(blocked) > 0 {
			b.Events.Add("warn", "", "restart cancelled: %s started", strings.Join(blocked, ", "))
			return
		}
		if err := b.Restart(detached); err != nil {
			b.Events.Add("error", "", "restart failed: %v", err)
		}
	}, nil
}
```

In `internal/daemon/run.go`, in `Options`, replace

```go
	// ShutdownWait bounds how long shutdown waits for in-flight cleanups; an
	// unfinished cleanup resumes at the next start.
	ShutdownWait time.Duration
```

with

```go
	// ShutdownWait bounds how long shutdown waits for in-flight cleanups; an
	// unfinished cleanup resumes at the next start.
	ShutdownWait time.Duration
	// SelfUnit reports whether this process is ghr.service's main process,
	// which a restart from the UI needs; nil reports false.
	SelfUnit func(ctx context.Context) bool
	// Restart restarts ghr.service without waiting for it.
	Restart func(ctx context.Context) error
```

In `DefaultOptions`, replace

```go
		ShutdownWait:          30 * time.Second,
```

with

```go
		ShutdownWait:          30 * time.Second,
		SelfUnit:              selfUnitCheck(system.Exec),
		Restart:               restartUnit(system.Exec),
```

In `start`, in the `Backend` literal, replace

```go
		WebApplied:       webCfg,
```

with

```go
		WebApplied:       webCfg,
		RestartAvailable: o.SelfUnit != nil && o.SelfUnit(ctx),
		Restart:          o.Restart,
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go test -count=1 ./internal/api/ ./internal/daemon/`
Expected: PASS for both.

Then `PATH=/usr/local/go/bin:$PATH gofmt -w internal/api/server.go internal/api/api_test.go internal/api/restart_test.go internal/daemon/backend.go internal/daemon/restart.go internal/daemon/run.go internal/daemon/restart_test.go internal/daemon/run_test.go`, `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (prints nothing), and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go vet ./internal/... ./cmd/...` (passes).

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/api_test.go internal/api/restart_test.go internal/daemon/backend.go internal/daemon/restart.go internal/daemon/run.go internal/daemon/restart_test.go internal/daemon/run_test.go
git commit -m "feat(api): restart ghr through systemd"
```

### Task 8: Fall back when the web address fails

**Files:**
- Create: `internal/daemon/weblisten.go`
- Modify: `internal/daemon/run.go` (`Options`, `DefaultOptions`, `start`, `Run`)
- Test: `internal/daemon/weblisten_test.go` (create), `internal/daemon/run_test.go`

**Interfaces:**
- Consumes: `Backend.WebListenErr`, `model.WebListenError` (Task 5); the `start` `Backend` literal as Task 7 left it; `bindReason(err error) string` and `splitListen(listen string) (host string, port int, ok bool)` (Task 3, `internal/daemon/webcheck.go`, not in Contracts)
- Produces: `daemon.Options.WebListenPath` (Contracts); `WebApplied.Listen` holds the fallback when one is served (Contracts, `daemon.Backend` seams); `readWebListen(path string) string` for Task 9

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 3 = 5

A `web.listen` that fails to bind no longer ends `Run`. The daemon then binds the address recorded in `Options.WebListenPath`, if the file holds a different `host:port`. On success it serves the UI there through `limitConns`, sets `WebApplied.Listen` to that value, sets `WebListenErr`, and writes the error event `web.listen <addr>: <reason>; serving the web UI on <fallback>`. When no address works it starts without a web listener and writes `web.listen <addr>: <reason>; the web UI is off until it is fixed`. `<reason>` is Task 3's `bindReason(err)` (`internal/daemon/webcheck.go`): the operating system's text alone, such as `address already in use` or `cannot assign requested address`, without `listen tcp <addr>: bind: `. That matches the spec's banner example (§3.3) and the wording of the save check. The bind happens before the event ring exists, so `Run` keeps the failure and writes the event right after the config warnings. `notConfigured` and `SetupState.WebListen` already read the listener and the listen string, which now hold the address actually served. A file that cannot be read, is empty, or does not hold a `host:port` with a port from 1 to 65535 is treated as absent. It is parsed with Task 3's `splitListen` (`internal/daemon/webcheck.go`), which parses as `config.Validate` does for `web.listen`. `config` has no exported helper for that.

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/weblisten_test.go`:

```go
package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/darkraise/ghr/internal/model"
)

func TestListenWeb(t *testing.T) {
	file := func(v string) string {
		p := filepath.Join(t.TempDir(), "web-listen")
		if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	also, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer also.Close()
	busy, busy2 := taken.Addr().String(), also.Addr().String()

	free := freeAddr(t)
	ln, served, failed := listenWeb(free, "")
	if ln == nil || served != free || failed != nil {
		t.Fatalf("a free address: %v %q %+v", ln, served, failed)
	}
	ln.Close()

	fallback := freeAddr(t)
	ln, served, failed = listenWeb(busy, file(" "+fallback+"\n"))
	want := model.WebListenError{Address: busy, Reason: "address already in use", ServingOn: fallback}
	if ln == nil || served != fallback || failed == nil || *failed != want {
		t.Fatalf("the fallback: %v %q %+v", ln, served, failed)
	}
	ln.Close()

	off := model.WebListenError{Address: busy, Reason: "address already in use"}
	for name, path := range map[string]string{
		"no file":             "",
		"the failing address": file(busy),
		"a taken fallback":    file(busy2),
		"not an address":      file("garbage\n"),
		"port zero":           file("127.0.0.1:0"),
		"no port":             file("127.0.0.1:"),
		"a named port":        file("127.0.0.1:http"),
		"a port too high":     file("127.0.0.1:65536"),
		"an empty file":       file(""),
		"a missing file":      filepath.Join(t.TempDir(), "missing"),
		"a directory":         t.TempDir(),
	} {
		ln, served, failed := listenWeb(busy, path)
		if ln != nil || served != "" || failed == nil || *failed != off {
			if ln != nil {
				ln.Close()
			}
			t.Errorf("%s: %v %q %+v", name, ln, served, failed)
		}
	}
}
```

In `internal/daemon/run_test.go`, replace

```go
// Two runs establish the cleanup. With the web port taken and the history
// file unreadable, the port error wins, so the listener is bound before the
// manager starts. With the port free, start fails in the manager, and the
// port is free again afterwards, so that listener was closed.
func TestRunClosesTheWebListenerWhenStartFails(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := taken.Addr().String()
	o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	if err := os.MkdirAll(o.HistoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "web listener") {
		t.Fatalf("both broken: %v", err)
	}
	taken.Close()
```

with

```go
// Run closes what it bound when start fails. A taken web port no longer ends
// Run, so with the port taken and the history unreadable the history is what
// fails, and the control socket is closed. With the port free, or with the
// UI served on the fallback, the port is free again after the failure.
func TestRunClosesTheWebListenerWhenStartFails(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := taken.Addr().String()
	o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	if err := os.MkdirAll(o.HistoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), o); err == nil || strings.Contains(err.Error(), "web listener") {
		t.Fatalf("both broken: %v", err)
	}
	if conn, err := net.DialTimeout("unix", o.Socket, time.Second); err == nil {
		conn.Close()
		t.Fatal("the control socket was left serving")
	}
	taken.Close()
```

and, in the same test, replace

```go
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener was left open: %v", err)
	}
	l.Close()
}

// Run serves the web UI through the limited listener: a client that holds
```

with

```go
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener was left open: %v", err)
	}
	l.Close()

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	fallback := freeAddr(t)
	o = testOptions(t, cfgYAML+"web:\n  listen: "+busy.Addr().String()+"\n")
	o.WebListenPath = filepath.Join(t.TempDir(), "web-listen")
	if err := os.WriteFile(o.WebListenPath, []byte(fallback+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(o.HistoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), o); err == nil {
		t.Fatal("history unreadable, UI on the fallback: Run succeeded")
	}
	l, err = net.Listen("tcp", fallback)
	if err != nil {
		t.Fatalf("the fallback listener was left open: %v", err)
	}
	l.Close()
}

// Run serves the web UI through the limited listener: a client that holds
```

In `TestRunBoundsAWebClientsConnections`, replace

```go
func TestRunBoundsAWebClientsConnections(t *testing.T) {
	addr := freeAddr(t)
	startRun(t, testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n"))
	var held []net.Conn
```

with

```go
func TestRunBoundsAWebClientsConnections(t *testing.T) {
	addr := freeAddr(t)
	startRun(t, testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n"))
	checkConnBound(t, addr)
}

// checkConnBound checks that the web listener on addr closes one client's
// connection over maxWebConnsPerClient, and serves it again once one closes.
func checkConnBound(t *testing.T, addr string) {
	t.Helper()
	var held []net.Conn
```

The rest of that function's body stays as it is, now as the body of `checkConnBound`.

Replace the whole of `TestRunFailsWhenTheWebPortIsTaken`:

```go
func TestRunFailsWhenTheWebPortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	o := testOptions(t, cfgYAML+"web:\n  listen: "+taken.Addr().String()+"\n")
	if err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "web listener") {
		t.Fatalf("Run with the web port taken: %v", err)
	}
	if conn, err := net.DialTimeout("unix", o.Socket, time.Second); err == nil {
		conn.Close()
		t.Fatal("the control socket was left serving")
	}
}
```

with

```go
// statusOver waits for the socket to serve the status and returns it.
func statusOver(t *testing.T, socket string) model.Status {
	t.Helper()
	c := api.NewUnixClient(socket)
	var st model.Status
	waitFor(t, "the socket to serve status", func() bool {
		var err error
		st, err = c.Status(context.Background())
		return err == nil
	})
	return st
}

// With the web port taken and no address to fall back to, the daemon runs
// without the web UI, and the status and an event say why.
func TestRunWithoutTheWebUIWhenItsPortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	addr := taken.Addr().String()
	o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	startRun(t, o)
	st := statusOver(t, o.Socket)
	want := model.WebListenError{Address: addr, Reason: "address already in use"}
	if st.WebListenError == nil || *st.WebListenError != want {
		t.Fatalf("listen error %+v, want %+v", st.WebListenError, want)
	}
	if st.Restart.WebListen != "" || !slices.Equal(st.Restart.Pending, []string{"web.listen"}) || st.WebSetupRequired {
		t.Fatalf("restart %+v, web_setup_required %v", st.Restart, st.WebSetupRequired)
	}
	msgs := eventMsgs(t, o.Socket)
	if !strings.Contains(msgs, "web.listen "+addr+": address already in use; the web UI is off until it is fixed") ||
		strings.Contains(msgs, "web UI listening") {
		t.Fatalf("events %q", msgs)
	}
	if ss, ok := setupStateOver(o.Socket); !ok || ss.WebListen != "" {
		t.Fatalf("setup state %+v", ss)
	}
}

// A web.listen that fails to bind falls back to the address a browser last
// used, behind the same connection bound.
func TestRunFallsBackToTheLastWebAddress(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	addr, fallback := taken.Addr().String(), freeAddr(t)
	o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	o.WebListenPath = filepath.Join(t.TempDir(), "web-listen")
	if err := os.WriteFile(o.WebListenPath, []byte(fallback+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	startRun(t, o)
	st := statusOver(t, o.Socket)
	want := model.WebListenError{Address: addr, Reason: "address already in use", ServingOn: fallback}
	if st.WebListenError == nil || *st.WebListenError != want {
		t.Fatalf("listen error %+v, want %+v", st.WebListenError, want)
	}
	if st.Restart.WebListen != fallback || !slices.Equal(st.Restart.Pending, []string{"web.listen"}) {
		t.Fatalf("restart %+v", st.Restart)
	}
	if msgs := eventMsgs(t, o.Socket); !strings.Contains(msgs, "web.listen "+addr+": address already in use; serving the web UI on "+fallback) {
		t.Fatalf("events %q", msgs)
	}
	if ss, ok := setupStateOver(o.Socket); !ok || ss.WebListen != fallback {
		t.Fatalf("setup state %+v", ss)
	}
	checkConnBound(t, fallback)
}

// A web address file that cannot be read, or holds no address, offers no
// fallback. The names are short: the subtest's temporary directory holds the
// control socket, whose path may not exceed 108 bytes.
func TestRunIgnoresABadWebAddressFile(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	addr := taken.Addr().String()
	for name, prepare := range map[string]func(path string) error{
		"dir":     func(p string) error { return os.Mkdir(p, 0o755) },
		"garbage": func(p string) error { return os.WriteFile(p, []byte("garbage\n"), 0o600) },
	} {
		t.Run(name, func(t *testing.T) {
			o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
			o.WebListenPath = filepath.Join(t.TempDir(), "web-listen")
			if err := prepare(o.WebListenPath); err != nil {
				t.Fatal(err)
			}
			startRun(t, o)
			st := statusOver(t, o.Socket)
			want := model.WebListenError{Address: addr, Reason: "address already in use"}
			if st.WebListenError == nil || *st.WebListenError != want {
				t.Fatalf("listen error %+v, want %+v", st.WebListenError, want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/daemon/ -run 'TestListenWeb|TestRunClosesTheWebListenerWhenStartFails|TestRunBoundsAWebClientsConnections|TestRunWithoutTheWebUIWhenItsPortIsTaken|TestRunFallsBackToTheLastWebAddress|TestRunIgnoresABadWebAddressFile'`
Expected: FAIL, build failed: `o.WebListenPath undefined (type Options has no field or method WebListenPath)` in `run_test.go`, then `undefined: listenWeb` in `weblisten_test.go`.

- [ ] **Step 3: Write the implementation**

Create `internal/daemon/weblisten.go`:

```go
package daemon

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/darkraise/ghr/internal/model"
)

// listenWeb binds the configured web address. When that fails it binds the
// address recorded at path, the last one a browser used, so a bad web.listen
// cannot take the UI away. It returns the listener, nil when no address
// works, the listen string it serves, and the failure to report.
func listenWeb(addr, path string) (net.Listener, string, *model.WebListenError) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, addr, nil
	}
	failed := &model.WebListenError{Address: addr, Reason: bindReason(err)}
	if fallback := readWebListen(path); fallback != "" && fallback != addr {
		if ln, err := net.Listen("tcp", fallback); err == nil {
			failed.ServingOn = fallback
			return ln, fallback, failed
		}
	}
	return nil, "", failed
}

// readWebListen returns the address recorded at path, or "" when there is
// none: no path, a file that cannot be read, or a value config.Validate
// would refuse for web.listen.
func readWebListen(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(data))
	if _, _, ok := splitListen(v); !ok {
		return ""
	}
	return v
}

// webListenEvent is the error event for a web.listen that failed to bind.
func webListenEvent(e *model.WebListenError) string {
	if e.ServingOn != "" {
		return fmt.Sprintf("web.listen %s: %s; serving the web UI on %s", e.Address, e.Reason, e.ServingOn)
	}
	return fmt.Sprintf("web.listen %s: %s; the web UI is off until it is fixed", e.Address, e.Reason)
}
```

All remaining edits are in `internal/daemon/run.go`. Replace

```go
	"github.com/darkraise/ghr/internal/metrics"
	"github.com/darkraise/ghr/internal/runner"
```

with

```go
	"github.com/darkraise/ghr/internal/metrics"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/runner"
```

In `Options`, replace

```go
	// Restart restarts ghr.service without waiting for it.
	Restart func(ctx context.Context) error
```

with

```go
	// Restart restarts ghr.service without waiting for it.
	Restart func(ctx context.Context) error
	// WebListenPath records the last web.listen value a browser used, the
	// fallback when web.listen fails to bind; empty records nothing and
	// offers no fallback.
	WebListenPath string
```

In `DefaultOptions`, replace

```go
		Restart:               restartUnit(system.Exec),
```

with

```go
		Restart:               restartUnit(system.Exec),
		WebListenPath:         "/var/lib/ghr/web-listen",
```

Replace the `start` signature

```go
func start(ctx context.Context, o Options, store *Store, ev *events.Ring, hub *changes.Hub, epoch string, webCfg config.Web, webSetupRequired func() bool) (*running, error) {
```

with

```go
func start(ctx context.Context, o Options, store *Store, ev *events.Ring, hub *changes.Hub, epoch string, webCfg config.Web, webErr *model.WebListenError, webSetupRequired func() bool) (*running, error) {
```

In its `Backend` literal, replace

```go
		WebApplied:       webCfg,
		RestartAvailable: o.SelfUnit != nil && o.SelfUnit(ctx),
```

with

```go
		WebApplied:       webCfg,
		WebListenErr:     webErr,
		RestartAvailable: o.SelfUnit != nil && o.SelfUnit(ctx),
```

In `Run`, replace

```go
	webCfg := store.Config().Web
	if webCfg.Listen != "" {
		tcp, err := net.Listen("tcp", webCfg.Listen)
		if err != nil {
			return fmt.Errorf("web listener: %w", err)
		}
		webLn = limitConns(tcp, maxWebConns, maxWebConnsPerClient)
	}
```

with

```go
	// From here on webCfg.Listen is the address served, which a fallback
	// changes; empty when no listener runs.
	webCfg := store.Config().Web
	var webErr *model.WebListenError
	if webCfg.Listen != "" {
		var tcp net.Listener
		tcp, webCfg.Listen, webErr = listenWeb(webCfg.Listen, o.WebListenPath)
		if tcp != nil {
			webLn = limitConns(tcp, maxWebConns, maxWebConnsPerClient)
		}
	}
```

Replace

```go
	for _, w := range warnings {
		ev.Add("warn", "", "config: %s", w)
	}
	if err := auth.Check(); err != nil {
```

with

```go
	for _, w := range warnings {
		ev.Add("warn", "", "config: %s", w)
	}
	if webErr != nil {
		ev.Add("error", "", "%s", webListenEvent(webErr))
	}
	if err := auth.Check(); err != nil {
```

Replace

```go
		// A wildcard listener reports [::] on a dual-stack host; setup output uses the configured address.
		webListen = webCfg.Listen
```

with

```go
		// A wildcard listener reports [::] on a dual-stack host; setup output uses the listen string bound.
		webListen = webCfg.Listen
```

Replace

```go
	rt, err := start(ctx, o, store, ev, hub, epoch, webCfg, webSetupRequired)
```

with

```go
	rt, err := start(ctx, o, store, ev, hub, epoch, webCfg, webErr, webSetupRequired)
```

`fmt` stays imported: `listen` uses it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/daemon/`
Expected: PASS (`TestRunServesTheWebUI`, `TestRunConfiguresFromTheWebUI` and `TestRunWithoutTheWebListener` included).

Then `PATH=/usr/local/go/bin:$PATH gofmt -w internal/daemon/weblisten.go internal/daemon/weblisten_test.go internal/daemon/run.go internal/daemon/run_test.go`, `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (prints nothing), and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go vet ./internal/... ./cmd/...` (passes).

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/weblisten.go internal/daemon/weblisten_test.go internal/daemon/run.go internal/daemon/run_test.go
git commit -m "feat(daemon): fall back when web.listen fails"
```

### Task 9: Record the address a browser used

**Files:**
- Modify: `internal/webui/handler.go` (`handler`, `Handler`, `requireSession`), `internal/daemon/weblisten.go`, `internal/daemon/run.go` (`Run`)
- Test: `internal/webui/handler_test.go`, `internal/daemon/web_test.go`, `internal/daemon/weblisten_test.go`, `internal/daemon/run_test.go`

**Interfaces:**
- Consumes: `Options.WebListenPath`, `listenWeb`'s served string as `webCfg.Listen` (Task 8)
- Produces: `webui.Handler(a *Auth, api http.Handler, static fs.FS, hosts []string, onSession func())` (Contracts); `recordWebListen(path, listen string) func()`

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

A bind alone does not prove a browser can reach the address. `webui.Handler` gains `onSession`, which runs for every request that passes `requireSession`, before the API handles it. `Run` passes a recorder that writes the served listen string to `Options.WebListenPath` at the first such request, unless the file already holds it. The write is atomic: a temporary file in the same directory (mode 0600 from `os.CreateTemp`), then sync and rename, as `writeHash` in `internal/webui/password.go` does. The repository has no shared helper for this; the nearest ones are tied to their types (`Store.writeTokenFile`, `writeHash`, `runner.writeJSONAtomic`). Once a write succeeds, or the file is found to hold the value, the recorder does nothing more for the life of the process. A failed write is tried again on the next request and logged once. `sync.Once` would not retry, so the recorder uses a mutex and a flag that is set only on success. An empty `WebListenPath` gives a nil `onSession`.

- [ ] **Step 1: Write the failing tests**

In `internal/webui/handler_test.go`, replace

```go
	return Handler(a, api, testFS(), []string{"ghr.lan"}), a, api
```

with

```go
	return Handler(a, api, testFS(), []string{"ghr.lan"}, nil), a, api
```

and replace

```go
			h := Handler(a, api, testFS(), []string{"ghr.lan"})
```

with

```go
			h := Handler(a, api, testFS(), []string{"ghr.lan"}, nil)
```

In `TestAPIRequestsCarryTheBrowserMark` (added by Task 2), replace

```go
	h := Handler(a, backend, testFS(), []string{"ghr.lan"})
```

with

```go
	h := Handler(a, backend, testFS(), []string{"ghr.lan"}, nil)
```

Then run `rg -n 'Handler\(a' internal/webui/` and give any other call of `Handler` that still has four arguments a fifth argument, `nil`. Append to `internal/webui/handler_test.go`:

```go
func TestOnSessionRunsForLoggedInAPIRequests(t *testing.T) {
	a, _ := newTestAuth(t)
	id, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	h := Handler(a, &fakeAPI{}, testFS(), []string{"ghr.lan"}, func() { calls++ })
	for _, tc := range []struct {
		target string
		mod    func(*http.Request)
		want   int
	}{
		{"/api/status", nil, 0},
		{"/auth/state", withCookie(id), 0},
		{"/", withCookie(id), 0},
		{"/api/status", withCookie(id), 1},
		{"/api/status", withCookie(id), 2},
	} {
		do(h, http.MethodGet, tc.target, "", tc.mod)
		if calls != tc.want {
			t.Fatalf("GET %s: %d calls, want %d", tc.target, calls, tc.want)
		}
	}
}
```

In `internal/daemon/web_test.go`, replace both occurrences of

```go
	web := httptest.NewServer(webui.Handler(auth, apiHandler, fstest.MapFS{}, nil))
```

with

```go
	web := httptest.NewServer(webui.Handler(auth, apiHandler, fstest.MapFS{}, nil, nil))
```

In `internal/daemon/weblisten_test.go`, replace the import block

```go
import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/darkraise/ghr/internal/model"
)
```

with

```go
import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)
```

and append:

```go
func TestRecordWebListen(t *testing.T) {
	if recordWebListen("", "0.0.0.0:8080") != nil {
		t.Fatal("an empty path records")
	}
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "web-listen")
	record := recordWebListen(path, "0.0.0.0:8080")
	record()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a write into a missing directory: %v", err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	record()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "0.0.0.0:8080\n" {
		t.Fatalf("after a failed write: %q %v", data, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info.Mode(), err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	record()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("written twice in one process: %v", err)
	}
}

func TestRecordWebListenKeepsAFileThatHoldsTheValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web-listen")
	if err := os.WriteFile(path, []byte(" 0.0.0.0:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	recordWebListen(path, "0.0.0.0:8080")()
	if info, err := os.Stat(path); err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("the file was rewritten: %v %v", info.ModTime(), err)
	}
}
```

In `internal/daemon/run_test.go`, directly above `func TestRunServesStorageAndSweepsTheToolCache`, add:

```go
// The web address is recorded once a browser has used it with a session,
// not when it is bound or at a login.
func TestRunRecordsTheWebAddressABrowserUsed(t *testing.T) {
	addr := freeAddr(t)
	o := testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	o.WebListenPath = filepath.Join(t.TempDir(), "web-listen")
	startRun(t, o)
	statusOver(t, o.Socket)
	absent := func(when string) {
		t.Helper()
		if _, err := os.Stat(o.WebListenPath); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("recorded %s: %v", when, err)
		}
	}
	if resp, _ := webCall(t, addr, http.MethodGet, "/auth/state", "", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("state: %d", resp.StatusCode)
	}
	absent("after the bind alone")
	resp, body := webCall(t, addr, http.MethodPost, "/auth/setup", `{"password":"correct horse battery"}`, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	var session string
	for _, ck := range resp.Cookies() {
		if ck.Name == "ghr_session" {
			session = ck.Value
		}
	}
	absent("at the login")
	if resp, body := webCall(t, addr, http.MethodGet, "/api/status", "", session); resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d %s", resp.StatusCode, body)
	}
	if data, err := os.ReadFile(o.WebListenPath); err != nil || string(data) != addr+"\n" {
		t.Fatalf("recorded %q %v, want %q", data, err, addr+"\n")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/webui/ -run 'TestOnSessionRunsForLoggedInAPIRequests'`
Expected: FAIL, build failed: `too many arguments in call to Handler`.

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/daemon/ -run 'TestRecordWebListen|TestRunRecordsTheWebAddressABrowserUsed'`
Expected: FAIL, build failed, starting with `internal/daemon/web_test.go:29:81: too many arguments in call to webui.Handler` (the same at line 84); `undefined: recordWebListen` may sit behind them.

- [ ] **Step 3: Write the implementation**

In `internal/webui/handler.go`, replace

```go
type handler struct {
	auth  *Auth
	hosts []string
}

// Handler serves the web UI's TCP listener: /auth/* for login, the control
// API under /api/ for logged-in sessions, and the app from static.
func Handler(a *Auth, api http.Handler, static fs.FS, hosts []string) http.Handler {
	h := &handler{auth: a, hosts: hosts}
```

with

```go
type handler struct {
	auth  *Auth
	hosts []string
	// onSession runs for every request a valid session lets through; nil
	// runs nothing.
	onSession func()
}

// Handler serves the web UI's TCP listener: /auth/* for login, the control
// API under /api/ for logged-in sessions, and the app from static.
// onSession, when set, runs for every /api/ request a valid session lets
// through, before the API handles it.
func Handler(a *Auth, apiHandler http.Handler, static fs.FS, hosts []string, onSession func()) http.Handler {
	h := &handler{auth: a, hosts: hosts, onSession: onSession}
```

The parameter is renamed from `api` because it shadowed the `internal/api` package that Task 2 imports into this file. In the same function, replace

```go
	mux.Handle("/api/", h.requireSession(http.StripPrefix("/api", api)))
```

with

```go
	mux.Handle("/api/", h.requireSession(http.StripPrefix("/api", apiHandler)))
```

In `requireSession`, replace

```go
		id, ok := h.session(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
```

with

```go
		id, ok := h.session(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		if h.onSession != nil {
			h.onSession()
		}
```

In `internal/daemon/weblisten.go`, replace the import block

```go
import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/darkraise/ghr/internal/model"
)
```

with

```go
import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/darkraise/ghr/internal/model"
)
```

and append:

```go
// recordWebListen returns the web handler's onSession: it records listen at
// path once a browser has used it with a session, so the next start can
// fall back to an address known to work. An empty path records nothing.
func recordWebListen(path, listen string) func() {
	if path == "" {
		return nil
	}
	r := &webListenRecorder{path: path, listen: listen}
	return r.record
}

// webListenRecorder writes at most once per process. A failed write is
// tried again at the next request, so the flag is set only on success.
type webListenRecorder struct {
	path, listen string

	mu      sync.Mutex
	written bool
	warned  bool
}

func (r *webListenRecorder) record() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.written {
		return
	}
	if data, err := os.ReadFile(r.path); err == nil && strings.TrimSpace(string(data)) == r.listen {
		r.written = true
		return
	}
	if err := writeWebListen(r.path, r.listen); err != nil {
		if !r.warned {
			log.Printf("record the web address: %v", err)
			r.warned = true
		}
		return
	}
	r.written = true
}

// writeWebListen replaces path atomically. os.CreateTemp creates the file
// with mode 0600.
func writeWebListen(path, listen string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".web-listen-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(listen + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
```

In `internal/daemon/run.go`, replace

```go
		webSrv.Handler = webui.Handler(auth, handler, static, webCfg.Hosts)
```

with

```go
		webSrv.Handler = webui.Handler(auth, handler, static, webCfg.Hosts, recordWebListen(o.WebListenPath, webCfg.Listen))
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go test -count=1 ./internal/webui/ ./internal/daemon/`
Expected: PASS for both.

Then `PATH=/usr/local/go/bin:$PATH gofmt -w internal/webui/handler.go internal/webui/handler_test.go internal/daemon/weblisten.go internal/daemon/weblisten_test.go internal/daemon/run.go internal/daemon/run_test.go internal/daemon/web_test.go`, `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (prints nothing), and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go vet ./internal/... ./cmd/...` (passes).

- [ ] **Step 5: Commit**

```bash
git add internal/webui/handler.go internal/webui/handler_test.go internal/daemon/weblisten.go internal/daemon/weblisten_test.go internal/daemon/run.go internal/daemon/run_test.go internal/daemon/web_test.go
git commit -m "feat(daemon): record the address a browser used"
```

### Task 10: Add the web types and restart call

**Files:**
- Modify: `web/src/api/types.ts`
- Modify: `web/src/api/client.ts`
- Test: `web/src/api/client.test.ts`

**Interfaces:**
- Consumes: the JSON of **`RestartStatus` and `WebListenError`** (Task 5), **`RepoStatus.Public`** (Task 4), **`WebPatch`** and `RepoPatch.AllowPublic` (Task 1), `POST /restart` answering 202 (Task 7); `web/src/api/fixtures/status.json` and `status-degraded.json` already hold `"restart": {"available": false}` (Task 5 regenerated them)
- Produces: **Web types** (Contracts) exactly as listed there, and `api.restart(): Promise<void>`

**Items:** 1, 2, 3

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

`web/src/api/types.test.ts:48-49` and `web/src/test/fixtures.ts:43` assign `status.json` and `status-degraded.json` to `Status`, so once `restart` is a required field `tsc` checks that the Go side wrote it. No web fixture is edited here; later tasks give the restart block and the web settings through `mockApi` overrides.

- [ ] **Step 1: Write the failing test**

In `web/src/api/client.test.ts`, replace:

```ts
  it("escapes runner IDs and cursors", async () => {
    const { calls } = mockApi({ "GET /api/runners/a%2Fb/log": { data: "", next: "" } })
    await api.log("a/b", "x;y")
    expect(calls[0]?.search).toBe("?cursor=x%3By")
  })
})
```

with:

```ts
  it("escapes runner IDs and cursors", async () => {
    const { calls } = mockApi({ "GET /api/runners/a%2Fb/log": { data: "", next: "" } })
    await api.log("a/b", "x;y")
    expect(calls[0]?.search).toBe("?cursor=x%3By")
  })

  it("asks for a restart with an empty POST that carries X-GHR", async () => {
    const { calls, fetchMock } = mockApi({ "POST /api/restart": () => new Response(null, { status: 202 }) })
    await expect(api.restart()).resolves.toBeUndefined()
    expect(calls).toEqual([{ method: "POST", path: "/api/restart", search: "", body: undefined, contentType: undefined }])
    expect(new Headers(fetchMock.mock.calls[0]?.[1]?.headers).get("X-GHR")).toBe("1")
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/api/client.test.ts`
Expected: FAIL, one test (`asks for a restart with an empty POST that carries X-GHR`), with `TypeError: api.restart is not a function`

- [ ] **Step 3: Write the implementation**

In `web/src/api/types.ts`, replace:

```ts
  error?: string
  last_job?: HistoryEntry
}
```

with:

```ts
  error?: string
  last_job?: HistoryEntry
  // GitHub's last reported visibility; absent until GitHub has answered.
  public?: boolean
}
```

Replace:

```ts
export interface Status {
  now: string
```

with:

```ts
export interface RestartStatus {
  pending?: string[]
  blocked_by?: string[]
  available: boolean
  web_listen?: string
}

export interface WebListenError {
  address: string
  reason: string
  serving_on?: string
}

export interface Status {
  now: string
```

Replace:

```ts
  web_setup_required?: boolean
  unconfigured?: boolean
  setup_pending?: boolean
}
```

with:

```ts
  web_setup_required?: boolean
  unconfigured?: boolean
  setup_pending?: boolean
  restart: RestartStatus
  web_listen_error?: WebListenError
}
```

Replace:

```ts
  cleanup_name_prefixes?: string[]
  paused?: boolean
  removing?: boolean
}
```

with:

```ts
  cleanup_name_prefixes?: string[]
  paused?: boolean
  removing?: boolean
  allow_public?: boolean
}
```

Replace:

```ts
  cleanup_name_prefixes?: string[]
  paused?: boolean
}

export interface ConfigPatch {
```

with:

```ts
  cleanup_name_prefixes?: string[]
  paused?: boolean
  allow_public?: boolean
}

export interface WebPatch {
  listen?: string
  hosts?: string[]
}

export interface ConfigPatch {
```

Replace:

```ts
  runner_limits?: RunnerLimitsPatch
  repos?: Record<string, RepoPatch>
}
```

with:

```ts
  runner_limits?: RunnerLimitsPatch
  repos?: Record<string, RepoPatch>
  web?: WebPatch
}
```

In `web/src/api/client.ts`, replace:

```ts
  reload: () => request<string[]>("POST", "/api/reload"),
```

with:

```ts
  reload: () => request<string[]>("POST", "/api/reload"),
  restart: () => send("POST", "/api/restart"),
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/api/client.test.ts src/api/types.test.ts`
Expected: PASS (both files)

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit`
Expected: no output, exit status 0 (a fixture without `restart` fails here at `src/api/types.test.ts:48`; it means Task 5 did not regenerate the fixtures)

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: no output, exit status 0

- [ ] **Step 5: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/api/client.test.ts
git commit -m "feat(web): add restart and web settings types"
```

### Task 11: Add the Serve when public switch

**Files:**
- Modify: `web/src/pages/repository.tsx`
- Test: `web/src/pages/repository.test.tsx`

**Interfaces:**
- Consumes: **Web types** (Task 10: `RepoConfig.allow_public`, `RepoPatch.allow_public`, `RepoStatus.public`); `PATCH /api/config` accepting `repos.<name>.allow_public` (Task 1); `ConfirmDialog` and `Confirm` (`web/src/components/confirm-dialog.tsx`)
- Produces: nothing other tasks use

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The switch joins the repository form's draft as `allow_public`, so the save bar, the Changed marker, the rejection and `UnsavedGuard` treat it like the other fields. `save()` splits in two: `save()` asks first when the draft turns the switch on and returns a promise that settles when the question is answered; `send()` is the old save body. `UnsavedGuard` already awaits `onSave()` (`web/src/components/unsaved-guard.tsx:74`), so its Save asks the same question. The kit's `AlertDialogAction` runs `onClick` before it closes the dialog (`node_modules/darkraise-ui/dist/chunk-KHIZQVEE.js:279-282`), so Confirm resolves the promise with the save before `onClose` resolves it with `false`, which is then ignored.

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/repository.test.tsx`, replace:

```ts
function applyingRoutes() {
  let config: Config = fixtures.config
```

with:

```ts
function applyingRoutes(initial: Config = fixtures.config) {
  let config = initial
```

At the end of the file, add:

```tsx
const PUBLIC_HELP = "A job in a public repository can come from anyone's pull request and runs on this host."

const publicConfig: Config = {
  ...fixtures.config,
  repos: (fixtures.config.repos ?? []).map((r) => (r.name === "darkmem" ? { ...r, allow_public: true } : r)),
}

describe("Serve when public", () => {
  it("asks before serving a public repository and sends the whole save once confirmed", async () => {
    const { calls } = mockApi(applyingRoutes())
    const { user } = renderApp("/repositories/darkmem/settings")
    const toggle = await screen.findByRole("switch", { name: "Serve when public" })
    expect(toggle).not.toBeChecked()
    expect(screen.getByText(PUBLIC_HELP)).toBeInTheDocument()
    expect(screen.queryByText(/GitHub reports this repository public/)).toBeNull()
    const max = screen.getByLabelText("Max")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(toggle)
    expect(screen.getByText("2 unsaved changes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    const ask = await screen.findByRole("alertdialog", { name: "Serve darkmem while it is public?" })
    expect(within(ask).getByText(PUBLIC_HELP)).toBeInTheDocument()
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
    await user.click(within(ask).getByRole("button", { name: "Serve when public" }))
    await waitFor(() =>
      expect(calls.filter((c) => c.method === "PATCH").map((c) => c.body)).toEqual([{ repos: { darkmem: { max: 3, allow_public: true } } }]),
    )
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.getByRole("switch", { name: "Serve when public" })).toBeChecked()
    expect(screen.queryAllByText(/Daemon did not apply darkmem\.allow_public/)).toHaveLength(0)
  })

  it("sends nothing when the question is cancelled, and keeps the draft", async () => {
    const { calls } = mockApi(applyingRoutes())
    const { user } = renderApp("/repositories/darkmem/settings")
    await user.click(await screen.findByRole("switch", { name: "Serve when public" }))
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    const ask = await screen.findByRole("alertdialog", { name: "Serve darkmem while it is public?" })
    await user.click(within(ask).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    expect(screen.getByRole("switch", { name: "Serve when public" })).toBeChecked()
  })

  it("asks the same question when the guard saves on leaving", async () => {
    const { calls } = mockApi(applyingRoutes())
    const { user, router } = renderApp("/repositories/darkmem/settings")
    await user.click(await screen.findByRole("switch", { name: "Serve when public" }))
    await user.click(screen.getByRole("link", { name: /^Runners/ }))
    const guard = await screen.findByRole("alertdialog", { name: "Unsaved changes" })
    await user.click(within(guard).getByRole("button", { name: "Save" }))
    const ask = await screen.findByRole("alertdialog", { name: "Serve darkmem while it is public?" })
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
    await user.click(within(ask).getByRole("button", { name: "Serve when public" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners"))
    expect(calls.filter((c) => c.method === "PATCH").map((c) => c.body)).toEqual([{ repos: { darkmem: { allow_public: true } } }])
  })

  it("saves it off without asking", async () => {
    const { calls } = mockApi(applyingRoutes(publicConfig))
    const { user } = renderApp("/repositories/darkmem/settings")
    const toggle = await screen.findByRole("switch", { name: "Serve when public" })
    expect(toggle).toBeChecked()
    await user.click(toggle)
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ repos: { darkmem: { allow_public: false } } }))
    expect(screen.queryByRole("alertdialog")).toBeNull()
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.queryAllByText(/Daemon did not apply darkmem\.allow_public/)).toHaveLength(0)
  })

  it("notes when GitHub reports the repository public, and what turning it off does", async () => {
    const repos = fixtures.status.repos.map((r) => (r.name === "darkmem" ? { ...r, public: true } : r))
    mockApi(routes({ "GET /api/config": publicConfig, "GET /api/status": { ...fixtures.status, repos } }))
    const { user } = renderApp("/repositories/darkmem/settings")
    expect(await screen.findByText("GitHub reports this repository public.")).toBeInTheDocument()
    await user.click(screen.getByRole("switch", { name: "Serve when public" }))
    expect(screen.getByText("GitHub reports this repository public. Idle runners stop; running jobs finish.")).toBeInTheDocument()
    expect(within(screen.getByRole("region", { name: "Visibility" })).getByText("Changed")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/pages/repository.test.tsx`
Expected: FAIL, the five tests of `Serve when public`; the first four with `Unable to find role="switch" and name "Serve when public"`, the last with `Unable to find an element with the text: GitHub reports this repository public.` Every other test passes.

- [ ] **Step 3: Write the implementation**

In `web/src/pages/repository.tsx`, replace:

```ts
import { Spinner } from "darkraise-ui/components/spinner"
```

with:

```ts
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
```

Replace:

```ts
import { ConfirmDialog } from "@/components/confirm-dialog"
```

with:

```ts
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
```

Replace:

```ts
function repoValues(r: RepoConfig | undefined): Values {
  return { max: r?.max, warm: r?.warm, labels: r?.labels ?? [], cleanup_name_prefixes: r?.cleanup_name_prefixes ?? [] }
}
```

with:

```ts
function repoValues(r: RepoConfig | undefined): Values {
  return {
    max: r?.max,
    warm: r?.warm,
    labels: r?.labels ?? [],
    cleanup_name_prefixes: r?.cleanup_name_prefixes ?? [],
    allow_public: r?.allow_public ?? false,
  }
}

const PUBLIC_HELP = "A job in a public repository can come from anyone's pull request and runs on this host."

function publicNote(isPublic: boolean | undefined, on: boolean): string {
  if (!isPublic) return ""
  return on ? "GitHub reports this repository public." : "GitHub reports this repository public. Idle runners stop; running jobs finish."
}
```

Replace:

```ts
  const [rejection, setRejection] = useState("")

  const cfg = config.data
```

with:

```ts
  const [rejection, setRejection] = useState("")
  const [question, setQuestion] = useState<{ confirm: Confirm; cancel: () => void } | null>(null)

  const cfg = config.data
```

Replace:

```ts
  async function save(): Promise<boolean> {
    if (warmError) {
      toast.error("Fix the highlighted settings first")
      return false
    }
    const sent: Values = Object.fromEntries(changed.map((k) => [k, draft[k]]))
```

with:

```ts
  // Turning Serve when public on asks first, and the answer covers the whole
  // save: confirming sends every change, Cancel sends nothing and keeps the
  // draft. The promise settles with the answer, so UnsavedGuard waits for it.
  function save(): Promise<boolean> {
    if (warmError) {
      toast.error("Fix the highlighted settings first")
      return Promise.resolve(false)
    }
    if (!changed.includes("allow_public") || draft.allow_public !== true) return send()
    return new Promise((resolve) => {
      setQuestion({
        confirm: { title: `Serve ${name} while it is public?`, body: PUBLIC_HELP, action: "Serve when public", run: () => resolve(send()) },
        cancel: () => resolve(false),
      })
    })
  }

  function closeQuestion() {
    question?.cancel()
    setQuestion(null)
  }

  async function send(): Promise<boolean> {
    const sent: Values = Object.fromEntries(changed.map((k) => [k, draft[k]]))
```

Replace:

```ts
  const is = (key: string) => changed.includes(key)

  const base = `/repositories/${name}`
```

with:

```ts
  const is = (key: string) => changed.includes(key)
  const note = publicNote(repoStatus?.public, values.allow_public === true)

  const base = `/repositories/${name}`
```

Replace:

```tsx
            </FieldGroup>
            <FieldGroup id="labels" title="Labels">
```

with:

```tsx
            </FieldGroup>
            <FieldGroup id="visibility" title="Visibility">
              <Field label="Serve when public" htmlFor="repo-allow-public" help={PUBLIC_HELP} changed={is("allow_public")}>
                <Switch
                  id="repo-allow-public"
                  checked={values.allow_public === true}
                  disabled={locked}
                  onCheckedChange={(on) => setDraft((d) => ({ ...d, allow_public: on }))}
                />
                {note && <span className="text-xs text-warning">{note}</span>}
              </Field>
            </FieldGroup>
            <FieldGroup id="labels" title="Labels">
```

Replace:

```tsx
        onSave={save}
        onDiscard={discard}
      />
    </div>
  )
}
```

with:

```tsx
        onSave={save}
        onDiscard={discard}
      />
      <ConfirmDialog confirm={question?.confirm ?? null} onClose={closeQuestion} />
    </div>
  )
}
```

The draft's `allow_public` is a boolean, so `draft[k] ?? null` in `send()` keeps `false` as `false`, and the read-back check compares it through `repoValues`, which reads a missing `allow_public` as `false`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/pages/repository.test.tsx src/anti-slop.test.ts`
Expected: PASS (both files)

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit`
Expected: no output, exit status 0

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: no output, exit status 0

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/repository.tsx web/src/pages/repository.test.tsx
git commit -m "feat(web): add the Serve when public switch"
```

### Task 12: Add the Web access settings

**Files:**
- Create: `web/src/lib/listen.ts`
- Create: `web/src/lib/listen.test.ts`
- Modify: `web/src/lib/settings-form.ts` (whole file given)
- Modify: `web/src/components/settings-form.tsx`
- Modify: `web/src/pages/settings.tsx`
- Test: `web/src/pages/settings.test.tsx`
- Test: `web/src/pages/setup.test.tsx`

**Interfaces:**
- Consumes: **Web types** (Task 10: `Config.web`, `WebPatch`, `ConfigPatch.web`); `PATCH /api/config` accepting `web {listen, hosts}` and the **Save-check messages** (Task 3), each starting `web.listen: ` or `web.hosts: `
- Produces: `splitListen(listen: string): { host: string; port: string } | null` and `isHostname(host: string): boolean` in `web/src/lib/listen.ts` (Task 13 uses `splitListen`); `SettingsForm.rejectedFields: Record<string, string>`; `SettingsSections({ form, webAccess = false }: { form: SettingsForm; webAccess?: boolean })`; the Web access group's anchor id `web-access` (Task 13 links to `/settings#web-access`)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

The restart control under the group's fields (spec 3.2, last bullet) needs `useRestart` from Task 13, so Task 13 adds it to this group; this task adds the fields only.

`splitListen` mirrors the daemon's check exactly (`internal/config/config.go:569-575`: `net.SplitHostPort`, then `strconv.Atoi` on the port and a range of 1 to 65535). From Go's `SplitHostPort` (`/usr/local/go/src/net/ipsock.go`): the port starts after the last colon; a bracketed host must close with the first `]` right before that colon; an unbracketed host holds no colon; no `[` may follow the first character and no `]` may follow the host's closing bracket (none at all without brackets). From `strconv.Atoi`: an optional `+` or `-` sign, then decimal digits, leading zeros allowed. The host comes back without brackets and the port as its number, so `000080` reads `80`. `isHostname` uses the daemon's `hostnameRe` (`internal/config/config.go:158`) and its 253-character limit (`:577`).

A rejection whose message starts with `web.listen: ` or `web.hosts: ` is shown under that field with the prefix removed. The whole remainder is the field's message: the save-check messages contain `; ` themselves (`you reached ghr on <L>; listening on <host> ...`), so they are not split the way `RejectedAlert` splits. Any other rejection stays in the alert, including `config.Validate`'s `web.hosts entries must be bare hostnames such as ghr.lan`, which has no colon after the key. A server's field message is kept apart from the client's `errors`, because `save()` refuses while `errors` is not empty and a rejection must not block the next save.

The client checks a web field only when its value differs from the loaded one: a saved value belongs to the daemon, and a hand edit the client would flag must not block saving other fields. An emptied listen address is not flagged either: the daemon refuses it with its own message, which then shows under the field.

`SettingsSections` is also the setup wizard's settings step (`web/src/pages/setup.tsx:168`). The group shows only on Settings → General: `SettingsSections` gains `webAccess?: boolean`, default false, and only `GeneralSettings` passes it. The wizard's form still carries `web_listen` and `web_hosts` in its loaded values; with no field to change them they never enter its draft.

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/listen.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import { isHostname, splitListen } from "./listen"

describe("splitListen", () => {
  it.each([
    ["0.0.0.0:8080", { host: "0.0.0.0", port: "8080" }],
    [":8080", { host: "", port: "8080" }],
    ["[::]:9090", { host: "::", port: "9090" }],
    ["[fd00::5]:80", { host: "fd00::5", port: "80" }],
    ["localhost:1", { host: "localhost", port: "1" }],
    ["192.168.0.99:65535", { host: "192.168.0.99", port: "65535" }],
  ])("reads %s", (listen, want) => {
    expect(splitListen(listen)).toEqual(want)
  })

  // strconv.Atoi takes a sign and leading zeros.
  it.each([
    ["0.0.0.0:000080", { host: "0.0.0.0", port: "80" }],
    ["0.0.0.0:+8080", { host: "0.0.0.0", port: "8080" }],
    ["[]:80", { host: "", port: "80" }],
    ["[fe80::1%eth0]:80", { host: "fe80::1%eth0", port: "80" }],
  ])("reads %s as Go does", (listen, want) => {
    expect(splitListen(listen)).toEqual(want)
  })

  it.each([
    "",
    "0.0.0.0",
    "0.0.0.0:",
    "0.0.0.0:0",
    "0.0.0.0:65536",
    "0.0.0.0:-80",
    "0.0.0.0: 80",
    "0.0.0.0:http",
    "0.0.0.0:99999999999999999999",
    "::1:80",
    "[::1]",
    "[::1]80",
    "[::1:8080",
    "127.0.0.1]:8080",
    "[::1]:80:90",
    "[::1]x:80",
    "[[::1]:80",
    "[::1]]:80",
    "a[b:80",
  ])("refuses %j", (listen) => {
    expect(splitListen(listen)).toBeNull()
  })
})

describe("isHostname", () => {
  it.each(["ghr.lan", "ghr", "GHR.example.com", "a-b.c"])("takes %s", (host) => {
    expect(isHostname(host)).toBe(true)
  })

  it.each(["https://ghr.lan", "ghr.lan:8080", "-ghr.lan", "ghr..lan", "ghr_lan", "", `${"a".repeat(63)}.${"b".repeat(63)}.${"c".repeat(63)}.${"d".repeat(63)}`])(
    "refuses %j",
    (host) => {
      expect(isHostname(host)).toBe(false)
    },
  )
})
```

In `web/src/pages/settings.test.tsx`, replace:

```ts
      const { runner_limits, ...rest } = body as Partial<Config>
      config = { ...config, ...rest, runner_limits: { ...config.runner_limits, ...runner_limits } }
```

with:

```ts
      const { runner_limits, web, ...rest } = body as Partial<Config>
      config = { ...config, ...rest, runner_limits: { ...config.runner_limits, ...runner_limits }, web: { ...config.web, ...web } }
```

At the end of the file, add:

```tsx
describe("Web access", () => {
  const group = () => within(screen.getByRole("region", { name: "Web access" }))
  const reason = "you reached ghr on 192.168.0.99; listening on 172.17.0.1 would lock this browser out"

  it("shows the listen address and the allowed hostnames after Runner defaults", async () => {
    mockApi(routes())
    renderApp("/settings")
    const listen = await screen.findByLabelText("Listen address")
    expect(listen).toHaveValue("0.0.0.0:8080")
    expect(listen).toHaveClass("font-mono")
    expect(group().getByText("Changes apply after a restart.")).toBeInTheDocument()
    expect(group().getByRole("button", { name: "Remove ghr.lan" })).toBeInTheDocument()
    expect(group().getByText("IP addresses and localhost always work.")).toBeInTheDocument()
    expect(screen.queryByText("The web UI turns off at the next restart.")).toBeNull()
    const titles = screen.getAllByRole("region").map((r) => r.querySelector("h2")?.textContent)
    expect(titles.indexOf("Web access")).toBe(titles.indexOf("Runner defaults") + 1)
  })

  it("saves both under web", async () => {
    const { calls } = mockApi(applyingDaemon())
    const { user } = renderApp("/settings")
    const listen = await screen.findByLabelText("Listen address")
    await user.clear(listen)
    await user.type(listen, "0.0.0.0:9090")
    await user.type(screen.getByRole("textbox", { name: "Allowed hostnames" }), "ghr.home{Enter}")
    expect(screen.getByText("2 unsaved changes")).toBeInTheDocument()
    expect(group().getAllByText("Changed")).toHaveLength(2)
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() =>
      expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ web: { listen: "0.0.0.0:9090", hosts: ["ghr.lan", "ghr.home"] } }),
    )
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.queryByText(/Daemon did not apply web\./)).toBeNull()
  })

  it("checks the address and the hostnames before sending", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/settings")
    const listen = await screen.findByLabelText("Listen address")
    await user.clear(listen)
    await user.type(listen, "0.0.0.0")
    expect(group().getByText("Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080")).toBeInTheDocument()
    await user.type(listen, ":70000")
    expect(group().getByText("Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080")).toBeInTheDocument()
    await user.clear(listen)
    // user-event reads "[" as the start of a key name; "[[" types one.
    await user.type(listen, "[[::]:8081")
    expect(listen).toHaveValue("[::]:8081")
    expect(group().queryByText("Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080")).toBeNull()
    await user.type(screen.getByRole("textbox", { name: "Allowed hostnames" }), "https://ghr.lan{Enter}")
    expect(group().getByText("https://ghr.lan is not a bare hostname such as ghr.lan")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

  it("does not let a saved address it would flag block saving other fields", async () => {
    const { calls } = mockApi(applyingDaemon({ ...fixtures.config, web: { listen: "0.0.0.0:http", hosts: ["ghr.lan"] } }))
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    expect(screen.getByLabelText("Listen address")).toHaveValue("0.0.0.0:http")
    expect(group().queryByText("Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080")).toBeNull()
    await user.clear(poll)
    await user.type(poll, "15")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ poll_interval: "15s" }))
  })

  it("does not count reordered hostnames, in any case, as a change", async () => {
    mockApi(routes({ "GET /api/config": { ...fixtures.config, web: { listen: "0.0.0.0:8080", hosts: ["ghr.lan", "ghr.home"] } } }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Remove ghr.lan" }))
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    await user.type(screen.getByRole("textbox", { name: "Allowed hostnames" }), "GHR.LAN{Enter}")
    expect(group().getByRole("button", { name: "Remove GHR.LAN" })).toBeInTheDocument()
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("shows a web.listen refusal under its field, without the key", async () => {
    mockApi(routes({ "PATCH /api/config": () => json({ error: `web.listen: ${reason}` }, 400) }))
    const { user } = renderApp("/settings")
    const listen = await screen.findByLabelText("Listen address")
    await user.clear(listen)
    await user.type(listen, "172.17.0.1:8080")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await group().findByText(reason)).toHaveClass("text-destructive")
    expect(screen.queryByText("Save rejected")).toBeNull()
    expect(listen).toHaveValue("172.17.0.1:8080")
  })

  it("shows a web.hosts refusal under its field", async () => {
    mockApi(routes({ "PATCH /api/config": () => json({ error: "web.hosts: you are using ghr.lan; removing it would lock this browser out" }, 400) }))
    const { user } = renderApp("/settings")
    await user.click(await screen.findByRole("button", { name: "Remove ghr.lan" }))
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await group().findByText("you are using ghr.lan; removing it would lock this browser out")).toBeInTheDocument()
    expect(screen.queryByText("Save rejected")).toBeNull()
  })

  it("keeps any other refusal in the form's alert", async () => {
    mockApi(routes({ "PATCH /api/config": () => json({ error: "web.hosts entries must be bare hostnames such as ghr.lan" }, 400) }))
    const { user } = renderApp("/settings")
    await user.type(await screen.findByRole("textbox", { name: "Allowed hostnames" }), "ghr.home{Enter}")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect(await screen.findByText("Save rejected")).toBeInTheDocument()
    expect(screen.getByText("web.hosts entries must be bare hostnames such as ghr.lan")).toBeInTheDocument()
  })

  it("notes that an empty saved address turns the web UI off", async () => {
    mockApi(routes({ "GET /api/config": { ...fixtures.config, web: { hosts: ["ghr.lan"] } } }))
    renderApp("/settings")
    expect(await screen.findByLabelText("Listen address")).toHaveValue("")
    expect(group().getByText("The web UI turns off at the next restart.")).toBeInTheDocument()
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })
})
```

In `web/src/pages/setup.test.tsx`, replace:

```tsx
  it("renders without the Shell and opens the Add repository dialog", async () => {
```

with:

```tsx
  it("leaves Web access out of the settings step", async () => {
    mockApi(authedRoutes({ "GET /api/setup": { ...base, configured: true, owner: "DarkRaise" }, "GET /api/storage": fixtures.storage }))
    const { user } = renderApp("/setup")
    expect(await screen.findByRole("heading", { name: "Repositories", level: 2 })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Next" }))
    expect(await screen.findByLabelText("Max concurrent runners")).toBeInTheDocument()
    expect(screen.getByRole("region", { name: "Runner defaults" })).toBeInTheDocument()
    expect(screen.queryByRole("region", { name: "Web access" })).toBeNull()
    expect(screen.queryByLabelText("Listen address")).toBeNull()
  })

  it("renders without the Shell and opens the Add repository dialog", async () => {
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/lib/listen.test.ts src/pages/settings.test.tsx src/pages/setup.test.tsx`
Expected: FAIL. `src/lib/listen.test.ts` fails to load with `Failed to resolve import "./listen"`; in `src/pages/settings.test.tsx` the nine `Web access` tests fail, most with `Unable to find a label with the text of: Listen address`, and every other test passes. `src/pages/setup.test.tsx` passes, the new test included: it guards the group from appearing in the wizard once Step 3 adds it.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/listen.ts`:

```ts
// The daemon's checks on web.listen and web.hosts (internal/config), so the
// form can refuse a value before sending it.

const HOSTNAME = /^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$/

// Go's net.SplitHostPort then strconv.Atoi, as config.Validate runs them on
// web.listen. The host comes back without brackets, the port as its number.
export function splitListen(listen: string): { host: string; port: string } | null {
  const colon = listen.lastIndexOf(":")
  if (colon < 0) return null
  let host: string
  let afterOpen = 0
  let afterClose = 0
  if (listen.startsWith("[")) {
    const close = listen.indexOf("]")
    if (close < 0 || close + 1 !== colon) return null
    host = listen.slice(1, close)
    afterOpen = 1
    afterClose = close + 1
  } else {
    host = listen.slice(0, colon)
    if (host.includes(":")) return null
  }
  if (listen.slice(afterOpen).includes("[") || listen.slice(afterClose).includes("]")) return null
  const port = listen.slice(colon + 1)
  if (!/^[+-]?\d+$/.test(port)) return null
  const n = Number(port)
  return n >= 1 && n <= 65535 ? { host, port: String(n) } : null
}

export function isHostname(host: string): boolean {
  return host.length <= 253 && HOSTNAME.test(host)
}
```

Replace the whole of `web/src/lib/settings-form.ts` with:

```ts
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { Config, ConfigPatch, RunnerLimitsPatch, WebPatch } from "@/api/types"
import { changedKeys, sameValue, settle, type Equal, type Values } from "@/lib/draft"
import { isHostname, splitListen } from "@/lib/listen"
import { UNIT_SETTINGS, readSetting, writeSetting } from "@/lib/units"
import { errorText } from "@/query"

// The daemon's cap on global_max (config.MaxRunners); it refuses a larger value.
const MAX_RUNNERS = 99

function loadedValues(c: Config): Values {
  return {
    mode: c.mode,
    global_max: c.global_max,
    poll_interval: readSetting("poll_interval", c.poll_interval),
    start_timeout: readSetting("start_timeout", c.start_timeout),
    idle_timeout: readSetting("idle_timeout", c.idle_timeout),
    disk_high_water: c.disk_high_water,
    build_cache_keep: readSetting("build_cache_keep", c.build_cache_keep),
    history_retention: readSetting("history_retention", c.history_retention),
    labels: c.labels ?? [],
    memory_max: readSetting("memory_max", c.runner_limits.memory_max),
    cpu_quota: readSetting("cpu_quota", c.runner_limits.cpu_quota),
    web_listen: c.web?.listen ?? "",
    web_hosts: c.web?.hosts ?? [],
  }
}

// The daemon compares hostnames as a set, ignoring case, order and repeats.
function hostSet(hosts: unknown[]): Set<string> {
  return new Set(hosts.map((h) => String(h).toLowerCase()))
}

const equal: Equal = (key, a, b) => {
  if (key === "web_hosts" && Array.isArray(a) && Array.isArray(b)) {
    const x = hostSet(a)
    const y = hostSet(b)
    return x.size === y.size && [...x].every((h) => y.has(h))
  }
  if (typeof a === "number" && typeof b === "number") return Object.is(a, b)
  return typeof a === "string" && typeof b === "string" ? a.trim() === b.trim() : sameValue(a, b)
}

function fieldErrors(v: Values, loaded: Values): Record<string, string> {
  const errors: Record<string, string> = {}
  for (const [key, spec] of Object.entries(UNIT_SETTINGS)) {
    const value = v[key]
    const err = typeof value === "number" ? spec.error(value) : ""
    if (err) errors[key] = err
  }
  const max = Number(v.global_max)
  if (!Number.isInteger(max) || max < 1 || max > MAX_RUNNERS) errors.global_max = `Enter a whole number from 1 to ${MAX_RUNNERS}`
  const high = Number(v.disk_high_water)
  if (!Number.isInteger(high) || high < 1 || high > 100) errors.disk_high_water = "Enter a whole number from 1 to 100"
  // Only an edit is checked: a saved value is the daemon's, and a hand edit
  // this check would flag must not block saving other fields. An emptied
  // address is left to the daemon, which refuses it with its own reason.
  const listen = String(v.web_listen ?? "").trim()
  if (!equal("web_listen", v.web_listen, loaded.web_listen) && listen !== "" && !splitListen(listen)) {
    errors.web_listen = "Enter host:port with a port from 1 to 65535, such as 0.0.0.0:8080"
  }
  if (!equal("web_hosts", v.web_hosts, loaded.web_hosts)) {
    const bad = ((v.web_hosts as string[] | undefined) ?? []).find((h) => !isHostname(h))
    if (bad !== undefined) errors.web_hosts = `${bad} is not a bare hostname such as ghr.lan`
  }
  return errors
}

const isLimit = (key: string): key is keyof RunnerLimitsPatch => key === "memory_max" || key === "cpu_quota"

function toPatch(sent: Values): ConfigPatch {
  const patch: Record<string, unknown> = {}
  const limits: RunnerLimitsPatch = {}
  const web: WebPatch = {}
  for (const [key, v] of Object.entries(sent)) {
    const written = writeSetting(key, v)
    const value = typeof written === "string" ? written.trim() : written
    if (isLimit(key)) limits[key] = value as string
    else if (key === "web_listen") web.listen = value as string
    else if (key === "web_hosts") web.hosts = value as string[]
    else patch[key] = value
  }
  if (Object.keys(limits).length > 0) patch.runner_limits = limits
  if (Object.keys(web).length > 0) patch.web = web
  return patch as ConfigPatch
}

// The daemon took the save, so until a read succeeds the cached config shows
// what was sent; the next poll replaces it with what the daemon holds.
function withPatch(c: Config, patch: ConfigPatch): Config {
  const { runner_limits: limits, web, ...rest } = patch
  return { ...c, ...rest, runner_limits: { ...c.runner_limits, ...limits }, web: { ...c.web, ...web } } as Config
}

const PATCH_KEYS: Record<string, string> = {
  memory_max: "runner_limits.memory_max",
  cpu_quota: "runner_limits.cpu_quota",
  web_listen: "web.listen",
  web_hosts: "web.hosts",
}

const patchKey = (key: string) => PATCH_KEYS[key] ?? key

// The daemon starts a web setting's refusal with its key, so the form shows
// it under that field. The rest of the message is one reason, which may hold
// "; " itself, so it is not split like the alert's list.
const REJECTION_FIELDS: [prefix: string, key: string][] = [
  ["web.listen: ", "web_listen"],
  ["web.hosts: ", "web_hosts"],
]

function placeRejection(message: string): { fields: Record<string, string>; rest: string } {
  const hit = REJECTION_FIELDS.find(([prefix]) => message.startsWith(prefix))
  return hit ? { fields: { [hit[1]]: message.slice(hit[0].length) }, rest: "" } : { fields: {}, rest: message }
}

export interface SettingsForm {
  config: Config | undefined
  configError: string
  retryConfig: () => void
  values: Values
  changed: string[]
  errors: Record<string, string>
  offline: boolean
  saving: boolean
  rejection: string
  // A rejection placed under its field, by draft key.
  rejectedFields: Record<string, string>
  set: (key: string) => (value: unknown) => void
  save: () => Promise<boolean>
  discard: () => void
}

export function useSettingsForm(): SettingsForm {
  const config = useConfig()
  const status = useStatus()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Values>({})
  const [saving, setSaving] = useState(false)
  const [rejection, setRejection] = useState("")

  const loaded = config.data ? loadedValues(config.data) : {}
  const values: Values = { ...loaded, ...draft }
  const changed = config.data ? changedKeys(draft, loaded, equal) : []
  const errors = config.data ? fieldErrors(values, loaded) : {}
  const offline = status.isError
  const placed = placeRejection(rejection)

  const set = (key: string) => (value: unknown) => setDraft((d) => ({ ...d, [key]: value }))

  function discard() {
    setDraft({})
    setRejection("")
  }

  async function save(): Promise<boolean> {
    if (changed.length === 0) return true
    if (saving) return false
    if (Object.keys(errors).length > 0) {
      toast.error("Fix the highlighted settings first")
      return false
    }
    const sent = Object.fromEntries(changed.map((k) => [k, values[k]]))
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig(toPatch(sent))
    } catch (err) {
      setSaving(false)
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        setRejection(err.message)
        toast.error("Settings not saved")
      } else {
        toast.error(`Settings not saved: ${errorText(err)}`)
      }
      return false
    }
    toast.success("Settings saved")
    try {
      const fresh = loadedValues(
        await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 }),
      )
      const missed = Object.keys(sent).find((key) => !equal(key, fresh[key], sent[key]))
      if (missed) toast.error(`Daemon did not apply ${patchKey(missed)}; is it older than this ghr?`)
    } catch (err) {
      toast.error(`Saved, but re-reading the config failed: ${errorText(err)}`)
      queryClient.setQueryData<Config>(keys.config, (c) => c && withPatch(c, toPatch(sent)))
    }
    setDraft((d) => settle(d, sent, equal))
    setSaving(false)
    return true
  }

  return {
    config: config.data,
    configError: config.isError ? errorText(config.error) : "",
    retryConfig: () => void config.refetch(),
    values,
    changed,
    errors,
    offline,
    saving,
    rejection: placed.rest,
    rejectedFields: placed.fields,
    set,
    save,
    discard,
  }
}
```

In `web/src/components/settings-form.tsx`, replace:

```tsx
export function SettingsSections({ form }: { form: SettingsForm }) {
```

with:

```tsx
// webAccess: only Settings → General shows the web listener's settings; the
// setup wizard leaves them out.
export function SettingsSections({ form, webAccess = false }: { form: SettingsForm; webAccess?: boolean }) {
```

Replace:

```tsx
        {unitField("cpu_quota", "CPU quota", "Per runner. 100% is one CPU core.")}
      </FieldGroup>
    </div>
  )
}
```

with:

```tsx
        {unitField("cpu_quota", "CPU quota", "Per runner. 100% is one CPU core.")}
      </FieldGroup>
      {webAccess && (
        <FieldGroup id="web-access" title="Web access" note="Changes apply after a restart.">
          <Field
            label="Listen address"
            htmlFor="web_listen"
            help={form.config?.web?.listen ? undefined : "The web UI turns off at the next restart."}
            changed={is("web_listen")}
            error={errors.web_listen ?? form.rejectedFields.web_listen}
          >
            <Input
              id="web_listen"
              className={`${FIELD_WIDTH} font-mono`}
              value={text("web_listen")}
              disabled={offline}
              onChange={(e) => set("web_listen")(e.target.value)}
            />
          </Field>
          <Field
            label="Allowed hostnames"
            help="IP addresses and localhost always work."
            changed={is("web_hosts")}
            error={errors.web_hosts ?? form.rejectedFields.web_hosts}
          >
            <TagField label="Allowed hostnames" value={values.web_hosts as string[]} onChange={set("web_hosts")} disabled={offline} className={FIELD_WIDTH} />
          </Field>
        </FieldGroup>
      )}
    </div>
  )
}
```

In `web/src/pages/settings.tsx`, replace:

```tsx
            <SettingsSections form={form} />
```

with:

```tsx
            <SettingsSections form={form} webAccess />
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/lib/listen.test.ts src/pages/settings.test.tsx src/pages/setup.test.tsx src/anti-slop.test.ts`
Expected: PASS (all four files)

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/a11y.test.tsx -t "Settings accessibility"`
Expected: PASS (the new group adds no axe violation)

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit`
Expected: no output, exit status 0

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: no output, exit status 0

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/listen.ts web/src/lib/listen.test.ts web/src/lib/settings-form.ts web/src/components/settings-form.tsx web/src/pages/settings.tsx web/src/pages/settings.test.tsx web/src/pages/setup.test.tsx
git commit -m "feat(web): add the Web access settings"
```

### Task 13: Add the restart banners and confirmation

**Files:**
- Create: `web/src/lib/restart.ts`
- Create: `web/src/lib/restart.test.ts`
- Create: `web/src/lib/restarting.ts`
- Create: `web/src/lib/use-restart.ts`
- Create: `web/src/components/restart-banner.tsx`
- Create: `web/src/components/restart-banner.test.tsx`
- Modify: `web/src/components/shell.tsx`
- Modify: `web/src/components/settings-form.tsx`
- Modify: `web/src/pages/settings.tsx`
- Modify: `web/src/test/setup.ts`

**Interfaces:**
- Consumes: **Web types** and `api.restart()` (Task 10); `splitListen` and the `web-access` anchor (Task 12); `status.restart` and `status.web_listen_error` (Task 5); `POST /api/restart` answering 202 or 409 `{error}` (Task 7)
- Produces: **`web/src/lib/restart.ts`** `restartPlan` (Contracts) plus its exported types `Page` and `RestartPlan`; **`useRestart()`** (Contracts) with two more fields, `busy: boolean` and `ready: boolean` (false until the config has data, and while its read fails; `ask()` does nothing then); **`startRestarting`** and **`useRestarting`** (Contracts) plus `clearRestarting(): void` and the type `Restarting`; `RestartBanner`, `ListenErrorBanner` and `RestartControl` in `web/src/components/restart-banner.tsx`

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

`restartPlan` has no pending list in its signature, so the caller says whether `web.listen` is pending through `saved`: `useRestart` passes the saved `web.listen` (empty when the config has none) when `restart.pending` holds `web.listen` and the config has loaded, and otherwise passes `running`, which `restartPlan` reads as `same`.

Restart ghr stays disabled until the config query has data, and while it is in error: the confirmation's sentence about where the web UI goes needs the saved `web.listen`, and without it `ask()` would read `running` and drop the move warning. `ask()` itself refuses to open without a loaded config. The "Waits for:" line shows whenever `blocked_by` is not empty, also when the restart must be done by hand.

The banner sits in the Runner update card's slot in the Shell (`web/src/components/shell.tsx:151-155`) and copies its style (`web/src/components/update-card.tsx:46-53`). The listener-error banner takes the restart banner's place while `web_listen_error` is present. `RestartControl` is the part both the banner and the Web access group show: the text for an unavailable restart, the blocked line, the Restart ghr button and its confirmation.

On a 202 `useRestart` clears the query cache and calls `startRestarting`; the screen that reads it comes in Task 14, so until then the store only records the restart. The store is module state, so `web/src/test/setup.ts` clears it after every test.

The page's own address in jsdom is `http://localhost/` (`web/vite.config.ts:22`), so its port is the scheme's default, 80; the tests give `restart.web_listen` port 80 for a page that reached ghr directly.

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/restart.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import { restartPlan } from "./restart"

function page(origin: string) {
  const u = new URL(origin)
  return { protocol: u.protocol, hostname: u.hostname, port: u.port, origin: u.origin }
}

describe("restartPlan", () => {
  it("keeps the page's origin when web.listen is not pending", () => {
    expect(restartPlan(page("http://192.168.0.99:8080"), "0.0.0.0:8080", "0.0.0.0:8080")).toEqual({ kind: "same", url: "http://192.168.0.99:8080" })
  })

  it("turns the web UI off for an empty saved address", () => {
    expect(restartPlan(page("http://192.168.0.99:8080"), "0.0.0.0:8080", "")).toEqual({ kind: "off", url: "http://192.168.0.99:8080" })
  })

  it.each([
    ["a wildcard keeps the page's hostname", "http://ghr.lan:8080", "0.0.0.0:8080", "0.0.0.0:9090", "http://ghr.lan:9090"],
    ["an IPv6 wildcard keeps it", "http://ghr.lan:8080", "[::]:8080", "[::]:9090", "http://ghr.lan:9090"],
    ["an expanded IPv6 wildcard keeps it", "http://ghr.lan:8080", "0.0.0.0:8080", "[0:0:0:0:0:0:0:0]:9090", "http://ghr.lan:9090"],
    ["a shortened IPv6 wildcard keeps it", "http://ghr.lan:8080", "0.0.0.0:8080", "[0::0]:9090", "http://ghr.lan:9090"],
    ["an IPv4 wildcard keeps it", "http://ghr.lan:8080", "[::]:8080", "0.0.0.0:9090", "http://ghr.lan:9090"],
    ["an empty host keeps it", "http://ghr.lan:8080", ":8080", ":9090", "http://ghr.lan:9090"],
    ["localhost keeps it", "http://localhost:8080", "localhost:8080", "localhost:9090", "http://localhost:9090"],
    ["a specific IP replaces it", "http://ghr.lan:8080", "0.0.0.0:8080", "192.168.0.99:9090", "http://192.168.0.99:9090"],
    ["a specific IPv6 address replaces it", "http://ghr.lan:8080", "0.0.0.0:8080", "[fd00::5]:9090", "http://[fd00::5]:9090"],
    ["the scheme's default port is left out", "http://ghr.lan", "0.0.0.0:80", "192.168.0.99:80", "http://192.168.0.99"],
  ])("moves a page that reached ghr directly: %s", (_name, origin, running, saved, url) => {
    expect(restartPlan(page(origin), running, saved)).toEqual({ kind: "moved", url })
  })

  it("names the new port for a proxy in front, and keeps the page's origin", () => {
    expect(restartPlan(page("https://ghr.example.com"), "127.0.0.1:8080", "127.0.0.1:9090")).toEqual({
      kind: "proxy",
      url: "https://ghr.example.com",
      port: "9090",
    })
  })
})
```

Create `web/src/components/restart-banner.test.tsx`:

```tsx
import { renderHook, screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it, onTestFinished, vi } from "vitest"
import { keys } from "@/api/hooks"
import type { Config, RestartStatus, Status } from "@/api/types"
import { useRestarting } from "@/lib/restarting"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const BODY = "Running jobs keep running. The web UI is down for a few seconds and you will need to log in again."

function routes(restart: RestartStatus, over: Partial<Status> = {}, more: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/status": { ...fixtures.status, restart, ...over }, "GET /api/token": fixtures.token, ...more })
}

const withWeb = (web: Config["web"]): Config => ({ ...fixtures.config, web })

describe("restart banner", () => {
  it.each([
    [["web.listen"], "Restart needed: web.listen changed."],
    [["web.listen", "web.hosts"], "Restart needed: web.listen and web.hosts changed."],
  ])("names each pending setting (%j)", async (pending, title) => {
    mockApi(routes({ pending, available: true }))
    renderApp("/")
    const banner = await screen.findByRole("region", { name: title })
    expect(banner).toHaveClass("border-warning")
    expect(within(banner).getByRole("button", { name: "Restart ghr" })).toBeEnabled()
  })

  it("is absent while nothing is pending", async () => {
    mockApi(routes({ available: true }))
    const { queryClient } = renderApp("/")
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toBeDefined())
    expect(screen.queryByRole("region", { name: /^Restart needed/ })).toBeNull()
  })

  it("disables the button and says what the restart waits for", async () => {
    mockApi(routes({ pending: ["web.hosts"], blocked_by: ["installing node 24", "1 queued"], available: true }))
    renderApp("/")
    const banner = await screen.findByRole("region", { name: "Restart needed: web.hosts changed." })
    expect(within(banner).getByText("Waits for: installing node 24, 1 queued")).toBeInTheDocument()
    expect(within(banner).getByRole("button", { name: "Restart ghr" })).toBeDisabled()
  })

  it("says how to restart by hand when ghr is not ghr.service", async () => {
    mockApi(routes({ pending: ["web.hosts"], available: false }))
    renderApp("/")
    const banner = await screen.findByRole("region", { name: "Restart needed: web.hosts changed." })
    expect(within(banner).getByText("Restart ghr with systemctl restart ghr.")).toBeInTheDocument()
    expect(within(banner).queryByRole("button")).toBeNull()
  })

  it("says what the restart waits for even when it must be done by hand", async () => {
    mockApi(routes({ pending: ["web.hosts"], blocked_by: ["pruning"], available: false }))
    renderApp("/")
    const banner = await screen.findByRole("region", { name: "Restart needed: web.hosts changed." })
    expect(within(banner).getByText("Restart ghr with systemctl restart ghr.")).toBeInTheDocument()
    expect(within(banner).getByText("Waits for: pruning")).toBeInTheDocument()
    expect(within(banner).queryByRole("button")).toBeNull()
  })

  it("keeps Restart ghr disabled until the config has loaded", async () => {
    let release: () => void = () => {}
    const loaded = new Promise<void>((resolve) => {
      release = resolve
    })
    mockApi(
      routes({ pending: ["web.listen"], available: true, web_listen: "0.0.0.0:80" }, {}, {
        "GET /api/config": async () => {
          await loaded
          return withWeb({ listen: "0.0.0.0:9090" })
        },
      }),
    )
    renderApp("/")
    const banner = await screen.findByRole("region", { name: "Restart needed: web.listen changed." })
    expect(within(banner).getByRole("button", { name: "Restart ghr" })).toBeDisabled()
    release()
    await waitFor(() => expect(within(banner).getByRole("button", { name: "Restart ghr" })).toBeEnabled())
  })

  it("keeps Restart ghr disabled while the config cannot be read", async () => {
    mockApi(routes({ pending: ["web.listen"], available: true, web_listen: "0.0.0.0:80" }, {}, {
      "GET /api/config": () => json({ error: "config unreadable" }, 500),
    }))
    const { queryClient } = renderApp("/")
    const banner = await screen.findByRole("region", { name: "Restart needed: web.listen changed." })
    await waitFor(() => expect(queryClient.getQueryState(keys.config)?.status).toBe("error"))
    expect(within(banner).getByRole("button", { name: "Restart ghr" })).toBeDisabled()
  })

  it("shows the listener error in its place, with a link to Web access", async () => {
    const scroll = vi.spyOn(Element.prototype, "scrollIntoView")
    onTestFinished(() => scroll.mockRestore())
    const error = { address: "10.0.0.5:8080", reason: "cannot assign requested address", serving_on: "0.0.0.0:8080" }
    mockApi(routes({ pending: ["web.listen"], available: true }, { web_listen_error: error }))
    const { user, router } = renderApp("/")
    const banner = await screen.findByRole("region", {
      name: "ghr could not listen on 10.0.0.5:8080: cannot assign requested address. Serving on 0.0.0.0:8080.",
    })
    expect(banner).toHaveClass("border-destructive")
    expect(screen.queryByRole("region", { name: /^Restart needed/ })).toBeNull()
    await user.click(within(banner).getByRole("link", { name: "Go to Web access" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings"))
    expect(router.state.location.hash).toBe("web-access")
    await waitFor(() => expect(scroll.mock.contexts).toContain(document.getElementById("web-access")))
  })
})

describe("restart confirmation", () => {
  async function ask(restart: RestartStatus, config: Config, more: Record<string, unknown> = {}) {
    const { calls } = mockApi(routes(restart, {}, { "GET /api/config": config, "POST /api/restart": () => new Response(null, { status: 202 }), ...more }))
    const view = renderApp("/")
    await waitFor(() => expect(view.queryClient.getQueryData(keys.config)).toBeDefined())
    await view.user.click(await screen.findByRole("button", { name: "Restart ghr" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Restart ghr?" })
    return { ...view, calls, dialog }
  }

  it.each([
    ["a wildcard", "0.0.0.0:80", withWeb({ listen: "0.0.0.0:9090" }), `${BODY} The web UI moves to http://localhost:9090.`],
    ["a specific IP", "0.0.0.0:80", withWeb({ listen: "192.168.0.99:9090" }), `${BODY} The web UI moves to http://192.168.0.99:9090.`],
    ["a proxy in front", "127.0.0.1:8080", withWeb({ listen: "127.0.0.1:9090" }), `${BODY} If a proxy forwards to ghr, point it at port 9090.`],
    ["an empty listen", "0.0.0.0:80", withWeb({ hosts: ["ghr.lan"] }), `${BODY} The web UI turns off.`],
  ])("says where the web UI goes for %s", async (_name, running, config, text) => {
    const { dialog } = await ask({ pending: ["web.listen"], available: true, web_listen: running }, config)
    expect(within(dialog).getByText(text)).toBeInTheDocument()
    expect(within(dialog).getByRole("button", { name: "Restart" })).toBeInTheDocument()
  })

  it("adds nothing while web.listen is not pending", async () => {
    const { dialog } = await ask({ pending: ["web.hosts"], available: true, web_listen: "0.0.0.0:80" }, withWeb({ listen: "0.0.0.0:9090" }))
    expect(within(dialog).getByText(BODY)).toBeInTheDocument()
  })

  it("restarts once confirmed and records the new address", async () => {
    const { user, calls, dialog } = await ask({ pending: ["web.listen"], available: true, web_listen: "0.0.0.0:80" }, withWeb({ listen: "192.168.0.99:9090" }))
    expect(calls.some((c) => c.path === "/api/restart")).toBe(false)
    await user.click(within(dialog).getByRole("button", { name: "Restart" }))
    await waitFor(() => expect(calls.filter((c) => c.method === "POST" && c.path === "/api/restart")).toHaveLength(1))
    const { result } = renderHook(() => useRestarting())
    await waitFor(() => expect(result.current).toEqual({ url: "http://192.168.0.99:9090", same: false }))
  })

  it("keeps the page's origin when web.listen is not pending", async () => {
    const { user, dialog } = await ask({ pending: ["web.hosts"], available: true, web_listen: "0.0.0.0:80" }, fixtures.config)
    await user.click(within(dialog).getByRole("button", { name: "Restart" }))
    const { result } = renderHook(() => useRestarting())
    await waitFor(() => expect(result.current).toEqual({ url: "http://localhost", same: true }))
  })

  it("shows a refused restart and stays on the page", async () => {
    const { user, dialog } = await ask({ pending: ["web.hosts"], available: true }, fixtures.config, {
      "POST /api/restart": () => json({ error: "waits for: pruning" }, 409),
    })
    await user.click(within(dialog).getByRole("button", { name: "Restart" }))
    expect((await screen.findAllByText("waits for: pruning")).length).toBeGreaterThan(0)
    expect(renderHook(() => useRestarting()).result.current).toBeNull()
    expect(screen.getByRole("region", { name: "Restart needed: web.hosts changed." })).toBeInTheDocument()
  })
})

describe("Web access restart control", () => {
  it("shows Restart ghr under the fields while a setting is pending", async () => {
    mockApi(routes({ pending: ["web.listen"], available: true }))
    renderApp("/settings")
    const group = await screen.findByRole("region", { name: "Web access" })
    expect(await within(group).findByRole("button", { name: "Restart ghr" })).toBeEnabled()
  })

  it("has no restart control while nothing is pending", async () => {
    mockApi(routes({ available: true }))
    const { queryClient } = renderApp("/settings")
    const group = await screen.findByRole("region", { name: "Web access" })
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toBeDefined())
    expect(within(group).queryByRole("button", { name: "Restart ghr" })).toBeNull()
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/lib/restart.test.ts src/components/restart-banner.test.tsx`
Expected: FAIL, both files fail to load, with `Failed to resolve import "./restart"` and `Failed to resolve import "@/lib/restarting"`

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/restart.ts`:

```ts
import { splitListen } from "@/lib/listen"

export interface Page {
  protocol: string
  hostname: string
  port: string
  origin: string
}

export interface RestartPlan {
  kind: "same" | "moved" | "proxy" | "off"
  url: string
  port?: string
}

const IPV4 = /^\d{1,3}(\.\d{1,3}){3}$/

// An empty host, 0.0.0.0, or an IPv6 address whose groups are all zero in
// any spelling (::, 0::0, 0:0:0:0:0:0:0:0) listens on every address.
function unspecified(host: string): boolean {
  if (host === "") return true
  if (IPV4.test(host)) return host.split(".").every((part) => Number(part) === 0)
  return host.includes(":") && host.split(":").every((group) => /^0{0,4}$/.test(group))
}

function specificIP(host: string): boolean {
  return !unspecified(host) && (IPV4.test(host) || host.includes(":"))
}

// Where the web UI answers after a restart. running is the listen value the
// running listener serves; saved is the one the restart applies, equal to
// running when web.listen is not pending. A page whose port is the running
// listener's reached ghr directly; any other went through a proxy.
export function restartPlan(page: Page, running: string | undefined, saved: string | undefined): RestartPlan {
  if (saved === running) return { kind: "same", url: page.origin }
  const value = saved?.trim() ?? ""
  if (value === "") return { kind: "off", url: page.origin }
  const next = splitListen(value)
  // The daemon refuses an address it cannot parse, so none is pending.
  if (!next) return { kind: "same", url: page.origin }
  const pagePort = page.port || (page.protocol === "https:" ? "443" : "80")
  const now = running ? splitListen(running) : null
  if (now?.port !== pagePort) return { kind: "proxy", url: page.origin, port: next.port }
  const url = new URL(page.origin)
  if (specificIP(next.host)) url.hostname = next.host.includes(":") ? `[${next.host}]` : next.host
  url.port = next.port
  return { kind: "moved", url: url.origin }
}
```

Create `web/src/lib/restarting.ts`:

```ts
import { useSyncExternalStore } from "react"

export interface Restarting {
  url: string
  same: boolean
}

let current: Restarting | null = null
const listeners = new Set<() => void>()

function emit() {
  for (const fn of listeners) fn()
}

function subscribe(fn: () => void): () => void {
  listeners.add(fn)
  return () => {
    listeners.delete(fn)
  }
}

export function startRestarting(r: Restarting): void {
  current = r
  emit()
}

// Tests end a restart so the next test starts with the app.
export function clearRestarting(): void {
  current = null
  emit()
}

export function useRestarting(): Restarting | null {
  return useSyncExternalStore(subscribe, () => current)
}
```

Create `web/src/lib/use-restart.ts`:

```ts
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { useConfig, useStatus } from "@/api/hooks"
import type { Confirm } from "@/components/confirm-dialog"
import { restartPlan, type RestartPlan } from "@/lib/restart"
import { startRestarting } from "@/lib/restarting"
import { errorText } from "@/query"

const BODY = "Running jobs keep running. The web UI is down for a few seconds and you will need to log in again."

function planText(plan: RestartPlan): string {
  switch (plan.kind) {
    case "off":
      return "The web UI turns off."
    case "moved":
      return `The web UI moves to ${plan.url}.`
    case "proxy":
      return `If a proxy forwards to ghr, point it at port ${plan.port ?? ""}.`
    case "same":
      return ""
  }
}

// ready is false until the config has loaded, and while its last read
// failed: the confirmation names where the web UI goes, which needs the
// saved web.listen.
export function useRestart(): { ask(): void; confirm: Confirm | null; close(): void; busy: boolean; ready: boolean } {
  const status = useStatus()
  const config = useConfig()
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const [busy, setBusy] = useState(false)

  async function restart(url: string) {
    setBusy(true)
    try {
      await api.restart()
    } catch (err) {
      setBusy(false)
      toast.error(errorText(err))
      return
    }
    // The restart ends the session: a poll or the change stream would meet a
    // 401 and send the page to the login form before ghr is back.
    queryClient.clear()
    startRestarting({ url, same: url === window.location.origin })
  }

  const ready = config.data !== undefined && !config.isError

  function ask() {
    const cfg = config.data
    if (!cfg || config.isError) return
    const state = status.data?.restart
    const running = state?.web_listen
    const listenPending = state?.pending?.includes("web.listen") ?? false
    const saved = listenPending ? (cfg.web?.listen ?? "") : running
    const plan = restartPlan(window.location, running, saved)
    const extra = planText(plan)
    setConfirm({ title: "Restart ghr?", body: extra ? `${BODY} ${extra}` : BODY, action: "Restart", run: () => void restart(plan.url) })
  }

  return { ask, confirm, close: () => setConfirm(null), busy, ready }
}
```

Create `web/src/components/restart-banner.tsx`:

```tsx
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { TriangleAlert } from "lucide-react"
import { useId } from "react"
import type { RestartStatus, Status, WebListenError } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { useRestart } from "@/lib/use-restart"

const BANNER = "flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border bg-card px-4 py-2.5 text-sm text-card-foreground"

function namesText(names: string[]): string {
  return names.length > 1 ? `${names.slice(0, -1).join(", ")} and ${names.at(-1) ?? ""}` : (names[0] ?? "")
}

export function RestartControl({ restart, offline }: { restart: RestartStatus; offline: boolean }) {
  const { ask, confirm, close, busy, ready } = useRestart()
  const blocked = restart.blocked_by ?? []
  return (
    <>
      {!restart.available && <p>Restart ghr with systemctl restart ghr.</p>}
      {blocked.length > 0 && <p>{`Waits for: ${blocked.join(", ")}`}</p>}
      {restart.available && (
        <>
          <Button size="sm" disabled={!ready || blocked.length > 0 || offline || busy} onClick={ask}>
            Restart ghr
          </Button>
          <ConfirmDialog confirm={confirm} onClose={close} />
        </>
      )}
    </>
  )
}

export function RestartBanner({ status, offline }: { status: Status; offline: boolean }) {
  const titleId = useId()
  const pending = status.restart.pending ?? []
  if (pending.length === 0) return null
  return (
    <section aria-labelledby={titleId} className={`${BANNER} border-warning`}>
      <h2 id={titleId} className="flex items-center gap-1.5 font-semibold text-warning">
        <TriangleAlert size={15} aria-hidden="true" />
        {`Restart needed: ${namesText(pending)} changed.`}
      </h2>
      <RestartControl restart={status.restart} offline={offline} />
    </section>
  )
}

export function ListenErrorBanner({ error }: { error: WebListenError }) {
  const titleId = useId()
  const serving = error.serving_on ? ` Serving on ${error.serving_on}.` : ""
  return (
    <section aria-labelledby={titleId} className={`${BANNER} border-destructive`}>
      <h2 id={titleId} className="flex items-center gap-1.5 font-semibold text-destructive">
        <TriangleAlert size={15} aria-hidden="true" />
        {`ghr could not listen on ${error.address}: ${error.reason.replace(/\.$/, "")}.${serving}`}
      </h2>
      <Link to="/settings" hash="web-access" className="text-primary hover:underline">
        Go to Web access
      </Link>
    </section>
  )
}
```

In `web/src/components/shell.tsx`, replace:

```ts
import { ModeControl } from "@/components/mode-control"
```

with:

```ts
import { ModeControl } from "@/components/mode-control"
import { ListenErrorBanner, RestartBanner } from "@/components/restart-banner"
```

Replace:

```tsx
          <div className="mb-4 empty:hidden">
            <UpdateCard update={st.runner_update} offline={offline} />
          </div>
```

with:

```tsx
          <div className="mb-4 flex flex-col gap-2 empty:hidden">
            {st.web_listen_error ? <ListenErrorBanner error={st.web_listen_error} /> : <RestartBanner status={st} offline={offline} />}
            <UpdateCard update={st.runner_update} offline={offline} />
          </div>
```

In `web/src/components/settings-form.tsx`, replace:

```ts
import { Field, FieldGroup } from "@/components/page/field"
```

with:

```ts
import { useStatus } from "@/api/hooks"
import { Field, FieldGroup } from "@/components/page/field"
import { RestartControl } from "@/components/restart-banner"
```

Replace:

```ts
  const { values, changed, errors, offline, set } = form
```

with:

```ts
  const { values, changed, errors, offline, set } = form
  const restart = useStatus().data?.restart
```

Replace:

```tsx
            <TagField label="Allowed hostnames" value={values.web_hosts as string[]} onChange={set("web_hosts")} disabled={offline} className={FIELD_WIDTH} />
          </Field>
        </FieldGroup>
```

with:

```tsx
            <TagField label="Allowed hostnames" value={values.web_hosts as string[]} onChange={set("web_hosts")} disabled={offline} className={FIELD_WIDTH} />
          </Field>
          {restart?.pending?.length ? (
            <div className="flex flex-wrap items-center gap-x-3 gap-y-2 py-3 text-sm">
              <RestartControl restart={restart} offline={offline} />
            </div>
          ) : null}
        </FieldGroup>
```

In `web/src/pages/settings.tsx`, replace:

```ts
import { Link, useParams } from "@tanstack/react-router"
```

with:

```ts
import { Link, useLocation, useParams } from "@tanstack/react-router"
```

Replace:

```ts
import type { ReactNode } from "react"
```

with:

```ts
import { useEffect, type ReactNode } from "react"
```

Replace:

```tsx
function GeneralSettings() {
  const form = useSettingsForm()
  const c = form.config
  return (
```

with:

```tsx
function GeneralSettings() {
  const form = useSettingsForm()
  const c = form.config
  const hash = useLocation({ select: (l) => l.hash })
  const ready = c !== undefined
  // Scrolled here rather than by the router, as on Repositories: the group
  // exists only once the config has loaded.
  useEffect(() => {
    if (ready && hash.replace(/^#/, "") === "web-access") document.getElementById("web-access")?.scrollIntoView()
  }, [ready, hash])
  return (
```

In `web/src/test/setup.ts`, replace:

```ts
import { liveConfig } from "@/api/live"
```

with:

```ts
import { liveConfig } from "@/api/live"
import { clearRestarting } from "@/lib/restarting"
```

Replace:

```ts
afterEach(async () => {
  cleanup()
  await releaseQueryClients()
```

with:

```ts
afterEach(async () => {
  cleanup()
  clearRestarting()
  await releaseQueryClients()
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/lib/restart.test.ts src/components/restart-banner.test.tsx src/components/shell.test.tsx src/components/update-card.test.tsx src/pages/settings.test.tsx src/anti-slop.test.ts`
Expected: PASS (all six files; `src/lib/restart.test.ts` 13 tests)

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/a11y.test.tsx -t "Dashboard accessibility|Settings accessibility"`
Expected: PASS

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit`
Expected: no output, exit status 0

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: no output, exit status 0

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/restart.ts web/src/lib/restart.test.ts web/src/lib/restarting.ts web/src/lib/use-restart.ts web/src/components/restart-banner.tsx web/src/components/restart-banner.test.tsx web/src/components/shell.tsx web/src/components/settings-form.tsx web/src/pages/settings.tsx web/src/test/setup.ts
git commit -m "feat(web): add restart banners and confirmation"
```

### Task 14: Show a restarting screen

**Files:**
- Create: `web/src/components/restart-screen.tsx`
- Create: `web/src/components/restart-screen.test.tsx`
- Modify: `web/src/components/root-layout.tsx`

**Interfaces:**
- Consumes: **`startRestarting` / `useRestarting`** and `clearRestarting` (Task 13), **`useRestart()`** and `RestartBanner` (Task 13), `api.authState` (`web/src/api/client.ts:103`)
- Produces: **`RestartScreen`** (Contracts) as `RestartScreen({ url, same }: { url: string; same: boolean })`

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`RootLayout` is defined in `web/src/components/root-layout.tsx`; `web/src/router.tsx:51` only names it as the root route's component. While `useRestarting()` is not null it renders `RestartScreen` instead of its `Outlet`, so `appRoute`'s guards and the `Shell` unmount. The Shell holds every app poll and the change stream (`useMetrics`, `useEvents`, `useChangeStream` at `web/src/components/shell.tsx:55-57`, and the pages under its outlet); unmounting it runs `useChangeStream`'s cleanup, which aborts the stream (`web/src/api/use-change-stream.ts:80-83`). `api.authState` is `GET /auth/state`, outside `/api/`, so a failure there never calls the 401 handler (`web/src/api/client.ts:70`).

On the page's own origin the screen polls `GET /auth/state` every second until it answers `authenticated: false`. `systemctl restart --no-block` returns before systemd stops the old process, which may still answer for a moment with this browser's session (`authenticated: true`); sessions live in memory, so the new process answers `false`. An error or `authenticated: true` keeps polling. The poll waits for one attempt to settle before scheduling the next, so a slow answer never stacks requests. The 60-second line "ghr has not answered yet." is for the page's own origin only, where the page keeps polling; it links that address. For another origin the page loads `<url>/login` at 10 seconds and promises nothing more: if the new address does not answer, the browser shows its own error page (spec 3.3 as amended).

The app test turns the change stream on (`liveConfig.enabled`, restored with `setLive(false)` afterwards, as `web/src/api/use-change-stream.test.tsx:90-98` does) and answers `/api/changes` with an open stream built the way that file's `controlled()` (`:67`) builds one. It holds one `/api/status` request across the restart and answers it with a 401 afterwards: the mock ignores the abort that `queryClient.clear()` sends, which stands for any response that arrives late. That 401 runs the unauthorized handler, which clears the cache and navigates to `/login`; `RootLayout` keeps rendering the restarting screen whatever the route, so the login form never replaces it.

jsdom's `location` cannot be spied on, so the tests replace it with a copy holding a mock `assign`, as `web/src/components/run-artifacts.test.tsx:33` does; `vi.unstubAllGlobals` in `web/src/test/setup.ts` puts it back. The screen tests fake only `setTimeout` and `clearTimeout` and assert with `vi.waitFor`, which runs on real timers.

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/restart-screen.test.tsx`:

```tsx
import { act, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, onTestFinished, vi } from "vitest"
import { keys } from "@/api/hooks"
import { liveConfig, setLive } from "@/api/live"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"
import { RestartScreen } from "./restart-screen"

function stubLocation() {
  const assign = vi.fn()
  const { href, origin, protocol, host, hostname, port, pathname, search, hash } = window.location
  vi.stubGlobal("location", { href, origin, protocol, host, hostname, port, pathname, search, hash, assign })
  return assign
}

async function tick(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

const down = () => {
  throw new TypeError("Failed to fetch")
}

describe("RestartScreen", () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it("asks ghr every second on the page's origin until the new process answers logged out", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    // Down, then the old process with this session, then the new one.
    const answers = [down, () => ({ setup_required: false, authenticated: true }), () => ({ setup_required: false, authenticated: false })]
    let polls = 0
    const { calls } = mockApi({ "GET /auth/state": () => (answers[polls++] ?? down)() })
    const assign = stubLocation()
    render(<RestartScreen url="http://localhost" same />)
    expect(screen.getByText("Restarting ghr…")).toBeInTheDocument()
    await tick(999)
    expect(calls).toHaveLength(0)
    await tick(1)
    expect(calls.map((c) => c.path)).toEqual(["/auth/state"])
    await tick(1000)
    await vi.waitFor(() => expect(calls).toHaveLength(2))
    expect(assign).not.toHaveBeenCalled()
    await tick(1000)
    await vi.waitFor(() => expect(assign).toHaveBeenCalledWith("http://localhost/login"))
    expect(calls).toHaveLength(3)
  })

  it("waits 10 seconds before loading another origin, which it cannot ask", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    const { calls } = mockApi({})
    const assign = stubLocation()
    render(<RestartScreen url="http://192.168.0.99:9090" same={false} />)
    await tick(9_999)
    expect(assign).not.toHaveBeenCalled()
    await tick(1)
    expect(assign).toHaveBeenCalledWith("http://192.168.0.99:9090/login")
    expect(calls).toEqual([])
  })

  it("promises nothing after loading another origin", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    mockApi({})
    stubLocation()
    render(<RestartScreen url="http://192.168.0.99:9090" same={false} />)
    await tick(120_000)
    expect(screen.queryByText("ghr has not answered yet.")).toBeNull()
    expect(screen.queryByRole("link")).toBeNull()
  })

  it("keeps asking its own origin, and says so with a link after 60 seconds", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    const { calls } = mockApi({ "GET /auth/state": down })
    const assign = stubLocation()
    render(<RestartScreen url="http://localhost" same />)
    await tick(59_999)
    expect(screen.queryByText("ghr has not answered yet.")).toBeNull()
    await tick(1)
    expect(screen.getByText("ghr has not answered yet.")).toBeInTheDocument()
    expect(screen.getAllByRole("link").map((a) => a.getAttribute("href"))).toEqual(["http://localhost"])
    expect(calls.length).toBeGreaterThan(30)
    expect(assign).not.toHaveBeenCalled()
  })

  it("stops asking once it is gone", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] })
    const { calls } = mockApi({ "GET /auth/state": down })
    stubLocation()
    const { unmount } = render(<RestartScreen url="http://localhost" same />)
    await tick(1000)
    unmount()
    await tick(5000)
    expect(calls).toHaveLength(1)
  })
})

// An open change stream the test can greet, built as in use-change-stream.test.tsx.
function openStream() {
  let ctrl: ReadableStreamDefaultController<Uint8Array> | undefined
  const response = new Response(
    new ReadableStream<Uint8Array>({
      start(c) {
        ctrl = c
      },
    }),
    { status: 200 },
  )
  return { response, hello: () => ctrl?.enqueue(new TextEncoder().encode("event: hello\ndata: {}\n\n")) }
}

describe("restarting from the app", () => {
  it("replaces every page, closes the change stream, and keeps the screen when a late request meets a 401", async () => {
    liveConfig.enabled = true
    onTestFinished(() => {
      liveConfig.enabled = false
      setLive(false)
    })
    let restarted = false
    let holding = false
    let answerHeld: () => void = () => {}
    const held = new Promise<void>((resolve) => {
      answerHeld = resolve
    })
    const stream = openStream()
    let streams = 0
    const status = { ...fixtures.status, restart: { pending: ["web.hosts"], available: true } }
    const unauthorized = () => json({ error: "not logged in" }, 401)
    const after = (value: unknown) => () => (restarted ? unauthorized() : value)
    const { calls, fetchMock } = mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated: !restarted }),
        "GET /api/changes": () => (streams++ === 0 ? stream.response : json({ error: "gone" }, 503)),
        "GET /api/status": async () => {
          if (!holding) return restarted ? unauthorized() : status
          await held
          return unauthorized()
        },
        "GET /api/config": after(fixtures.config),
        "GET /api/metrics": after(fixtures.metrics),
        "GET /api/events": after(fixtures.events),
        "POST /api/restart": () => {
          restarted = true
          return new Response(null, { status: 202 })
        },
      }),
    )
    const { user, router, queryClient } = renderApp("/")
    const button = await screen.findByRole("button", { name: "Restart ghr" })
    await waitFor(() => expect(calls.some((c) => c.path === "/api/changes")).toBe(true))
    stream.hello()

    // A status read still in flight when the restart is accepted.
    holding = true
    const statusReads = calls.filter((c) => c.path === "/api/status").length
    void queryClient.refetchQueries({ queryKey: keys.status })
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/status")).toHaveLength(statusReads + 1))

    const assign = stubLocation()
    await waitFor(() => expect(button).toBeEnabled())
    await user.click(button)
    const dialog = await screen.findByRole("alertdialog", { name: "Restart ghr?" })
    await user.click(within(dialog).getByRole("button", { name: "Restart" }))
    expect(await screen.findByText("Restarting ghr…")).toBeInTheDocument()
    const shown = calls.length
    expect(screen.queryByRole("navigation", { name: "Pages" })).toBeNull()
    expect(queryClient.getQueryCache().getAll()).toEqual([])
    const streamCall = fetchMock.mock.calls.find(([input]) => String(input) === "/api/changes")
    expect(streamCall?.[1]?.signal?.aborted).toBe(true)

    answerHeld()
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(screen.getByText("Restarting ghr…")).toBeInTheDocument()
    expect(screen.queryByText("Log in to manage this runner host.")).toBeNull()

    await waitFor(() => expect(assign).toHaveBeenCalledWith("http://localhost/login"), { timeout: 3000 })
    expect(screen.getByText("Restarting ghr…")).toBeInTheDocument()
    expect(calls.slice(shown).filter((c) => c.path.startsWith("/api/"))).toEqual([])
    expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(1)
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/restart-screen.test.tsx`
Expected: FAIL, the file fails to load with `Failed to resolve import "./restart-screen"`

- [ ] **Step 3: Write the implementation**

Create `web/src/components/restart-screen.tsx`:

```tsx
import { Spinner } from "darkraise-ui/components/spinner"
import { useEffect, useState } from "react"
import { api } from "@/api/client"
import { Brand } from "@/components/brand"

const POLL_MS = 1000
// The CSP limits fetches to the page's own origin, so a new address cannot be
// asked; the page gives ghr this long and then loads it. If it does not
// answer there, the browser's own error page says so.
const MOVE_WAIT_MS = 10_000
// Only the page's own origin is polled, so only it can be late.
const LATE_MS = 60_000

export function RestartScreen({ url, same }: { url: string; same: boolean }) {
  const [late, setLate] = useState(false)

  useEffect(() => {
    if (!same) return
    const timer = window.setTimeout(() => setLate(true), LATE_MS)
    return () => window.clearTimeout(timer)
  }, [same])

  useEffect(() => {
    const login = () => window.location.assign(`${url}/login`)
    if (!same) {
      const timer = window.setTimeout(login, MOVE_WAIT_MS)
      return () => window.clearTimeout(timer)
    }
    const ctrl = new AbortController()
    let timer = 0
    const poll = () => {
      timer = window.setTimeout(() => {
        // The old process may answer for a moment, still holding this
        // session; sessions live in memory, so the new one answers logged out.
        api.authState(ctrl.signal).then((state) => (state.authenticated ? again() : login()), again)
      }, POLL_MS)
    }
    const again = () => {
      if (!ctrl.signal.aborted) poll()
    }
    poll()
    return () => {
      ctrl.abort()
      window.clearTimeout(timer)
    }
  }, [url, same])

  return (
    <main className="flex min-h-dvh flex-col items-center justify-center gap-4 bg-background p-6 text-center text-foreground">
      <Brand />
      <p className="flex items-center gap-2 text-sm">
        <Spinner />
        <span>Restarting ghr…</span>
      </p>
      {late && (
        <div className="flex flex-col items-center gap-2 text-sm">
          <p>ghr has not answered yet.</p>
          <a href={url} className="font-mono text-primary hover:underline">
            {url}
          </a>
        </div>
      )}
    </main>
  )
}
```

Replace the whole of `web/src/components/root-layout.tsx` with:

```tsx
import { Outlet } from "@tanstack/react-router"
import { RouterAdapterProvider } from "darkraise-ui/router"
import { RestartScreen } from "@/components/restart-screen"
import { routerAdapter } from "@/lib/router-adapter"
import { useRestarting } from "@/lib/restarting"

// The restarting screen takes the place of every route, outside the app
// route and its guards, so nothing the restart ends can redirect the page.
export function RootLayout() {
  const restarting = useRestarting()
  return (
    <RouterAdapterProvider value={routerAdapter}>
      {restarting ? <RestartScreen url={restarting.url} same={restarting.same} /> : <Outlet />}
    </RouterAdapterProvider>
  )
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/restart-screen.test.tsx src/components/restart-banner.test.tsx src/router.test.tsx src/app.test.tsx src/anti-slop.test.ts`
Expected: PASS (all five files; `src/components/restart-screen.test.tsx` 6 tests)

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit`
Expected: no output, exit status 0

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: no output, exit status 0

- [ ] **Step 5: Commit**

```bash
git add web/src/components/restart-screen.tsx web/src/components/restart-screen.test.tsx web/src/components/root-layout.tsx
git commit -m "feat(web): show a restarting screen"
```

### Task 15: Document the new settings

**Files:**
- Modify: `README.md` (the web UI list, items 2 and 3)
- Modify: `deploy/config.example.yaml` (the `web:` and `allow_public` comments)

**Interfaces:**
- Consumes: the behaviour of Tasks 3, 7, 8, 11 and 12 (texts only).
- Produces: nothing.

**Items:** 1, 2, 3

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

- [ ] **Step 1: Update the README's web UI list**

In `README.md`, replace this line:

```markdown
2. A config written before the web UI has no `web:` block: add one to `/etc/ghr/config.yaml` (see below), set `listen`, and run `systemctl restart ghr`. A reload does not start or move the listener; it only warns `web settings changed; restart ghr to apply`.
```

with:

```markdown
2. A config written before the web UI has no `web:` block: add one to `/etc/ghr/config.yaml` (see below), set `listen`, and run `systemctl restart ghr`. After that, Settings → Web access changes the listen address and hostnames, and the **Restart ghr** banner applies them. A reload does not start or move the listener; it only warns `web settings changed; restart ghr to apply`. The UI refuses an address the browser could not reach it on, and a restart waits while ghr installs, removes or clears toolchains and caches, has such operations queued, prunes, or updates the runner software; running jobs keep running. If the address fails at start, ghr serves the UI on the last address a browser used and shows why.
```

Replace this line:

```markdown
3. To reach ghr through nginx-proxy-manager under a name, add that name to `web.hosts` and restart. ghr answers only IP addresses, `localhost` and the listed names, which blocks DNS rebinding. The proxy must forward the original `Host` header, as nginx-proxy-manager does by default.
```

with:

```markdown
3. To reach ghr through nginx-proxy-manager under a name, add that name to `web.hosts` (Settings → Web access, or `config.yaml`) and restart. ghr answers only IP addresses, `localhost` and the listed names, which blocks DNS rebinding. The proxy must forward the original `Host` header, as nginx-proxy-manager does by default.
```

- [ ] **Step 2: Update the example config's comments**

In `deploy/config.example.yaml`, replace:

```yaml
web:                          # browser UI; the first visitor sets its password (setup.sh can set it first)
```

with:

```yaml
web:                          # browser UI; the first visitor sets its password (setup.sh can set it first); Settings → Web access edits it
```

and replace:

```yaml
#     allow_public: true      # required to serve a public repository; without it ghr starts no runner for one
```

with:

```yaml
#     allow_public: true      # required to serve a public repository; the repository page's Serve when public switch sets it
```

- [ ] **Step 3: Check that nothing else names the old wording**

Run: `rg -n "Set allow_public: true for it in config.yaml" --glob '!docs/**' .`
Expected: no output (Task 4 replaced the message; the docs keep their history).

- [ ] **Step 4: Commit**

```bash
git add README.md deploy/config.example.yaml
git commit -m "docs: say where the web settings are edited"
```

### Task 16: Run every suite and check on the LXC

**Files:**
- Modify: `docs/superpowers/registers/2026-10-13-ghr-ui-config-items.md` (Step 6)
- Modify: `docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md` (Step 6, rows 232 and 263)
- No code changes, unless a check below fails: then fix it in the task the failure belongs to and report it.

**Interfaces:**
- Consumes: everything above.
- Produces: the register's acceptance evidence.

**Items:** 1, 2, 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 2 = 3

- [ ] **Step 1: Ask the owner**

Ask: "All tasks are committed. May I build this branch, deploy it to the runner LXC (192.168.0.99) and restart ghr there to run the browser checks? Restarting logs every browser out; running jobs keep running." Wait for an explicit yes. Without it, skip Steps 4 and 5, still run Steps 2, 3 and 6, and in Step 6 record rows 1 to 3 as `verify`.

- [ ] **Step 2: Run the Go suite**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go test ./...`
Expected: every package `ok`. Then `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/ web/` prints nothing and `TMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./...` passes.

- [ ] **Step 3: Run the web suite**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 900 npx vitest run`, then `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit`, then `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`.
Expected: every test passes; tsc and eslint print nothing.

- [ ] **Step 4: Build and deploy (only after the yes)**

```bash
B=/root/.cache/ghr-plan-tmp; H=$(git rev-parse --short HEAD)
rm -rf $B/build && mkdir -p $B/build && git archive HEAD | tar -x -C $B/build
ln -sfn "$(git rev-parse --show-toplevel)/web/node_modules" $B/build/web/node_modules
(cd $B/build/web && TMPDIR=$B timeout 600 npm run -s build)
(cd $B/build && TMPDIR=$B GOTMPDIR=$B PATH=/usr/local/go/bin:$PATH CGO_ENABLED=0 timeout 600 go build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=config-items-$H" -o $B/ghr-config-items ./cmd/ghr)
timeout 30 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 'systemctl list-units --no-legend "ghr-runner-*" | grep -c . ; curl -s --max-time 5 --unix-socket /run/ghr/ghr.sock http://ghr/status | python3 -c "import json,sys; d=json.load(sys.stdin); print(\"busy\", sum(1 for i in d[\"instances\"] if i[\"state\"]==\"busy\")); print(\"listen\", d.get(\"restart\",{}).get(\"web_listen\",\"\"))"; grep -n "listen:" /etc/ghr/config.yaml'
```

A failing SSH command means the LXC is not reachable from this host: stop and report it. The output gives the number of runner units, the number of busy runners, and the current `web.listen` line; write the `listen:` value down, Step 5 restores it. Idle warm runners do not block the deploy (a restart adopts them); busy runners do: when `busy` is above 0, check again every two minutes for at most 30 minutes, then stop and ask the owner. When `busy` is 0:

```bash
OLD=$(timeout 30 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 ghr version)
timeout 120 scp -q -o BatchMode=yes -o ConnectTimeout=10 $B/ghr-config-items root@192.168.0.99:/usr/local/bin/ghr.new
timeout 120 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 "cp -p /usr/local/bin/ghr /usr/local/bin/ghr.$OLD.bak && mv /usr/local/bin/ghr.new /usr/local/bin/ghr && systemctl restart ghr && sleep 3 && systemctl is-active ghr && ghr version"
```

Expected: `active` and `config-items-<sha>`.

- [ ] **Step 5: Browser checks (spec §5.3)**

Ask the owner for the web UI password for this session (never store it), and which private repository to use in check 4 (default `ghr-e2e`; confirm it is private with `gh repo view darkraise/<repo> --json visibility -q .visibility`, which prints `PRIVATE`). Use a browser the session can drive (the Playwright MCP browser, or headless Chrome with `--no-sandbox` when running as root); if none is available, ask the owner to run the checks and report what they saw. Drive `http://192.168.0.99:8080` and record what happens for each:
1. Settings → Web access: add a hostname to Allowed hostnames, Save. The Restart needed banner names `web.hosts`. Restart ghr → the restarting screen → the login page. Log in; the banner is gone. Remove the hostname again and restart the same way.
2. Set Listen address to `0.0.0.0:9090`, Save, Restart ghr: the confirmation says "The web UI moves to http://192.168.0.99:9090."; after about 10 seconds the browser opens `http://192.168.0.99:9090/login`. Log in there, set it back to `0.0.0.0:8080`, restart, and log in on port 8080.
3. Type `172.17.0.1:8080` in Listen address and Save: the field shows "you reached ghr on 192.168.0.99; listening on 172.17.0.1 would lock this browser out" and nothing is saved.
4. Repository page of a private repository (for example `ghr-e2e`): turn Serve when public on, Save, confirm; turn it off and Save. Each save shows "Repositories saved" and `/etc/ghr/config.yaml` shows `allow_public: true`, then no `allow_public` line.
5. Over SSH, with `L` the `listen:` value written down in Step 4:

   ```bash
   timeout 60 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 "cp -p /etc/ghr/config.yaml /etc/ghr/config.yaml.check && sed -i 's|^\(  *listen:\).*|\1 10.255.255.1:8080|' /etc/ghr/config.yaml && grep -n 'listen:' /etc/ghr/config.yaml && systemctl restart ghr && sleep 3 && systemctl is-active ghr && cat /var/lib/ghr/web-listen"
   ```

   Expected: the grep shows `listen: 10.255.255.1:8080`, the service is `active`, and `web-listen` holds `L` (written by checks 1 and 2). Open `http://192.168.0.99:8080`: the UI is served there with the banner "ghr could not listen on 10.255.255.1:8080: cannot assign requested address. Serving on L." Then restore and confirm:

   ```bash
   timeout 60 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 "mv /etc/ghr/config.yaml.check /etc/ghr/config.yaml && grep -n 'listen:' /etc/ghr/config.yaml && systemctl restart ghr && sleep 3 && systemctl is-active ghr"
   ```

   Expected: the original `listen:` line and `active`; the banner is gone after logging in again.

- [ ] **Step 6: Record the results**

Run, from the repository root, for each row that passed its checks:

```bash
R=docs/superpowers/registers/2026-10-13-ghr-ui-config-items.md
REG="$(ls -d /root/.claude/plugins/cache/darkraise/dr-superpowers/*/ | sort -V | tail -1)scripts/register"
bash "$REG" set $R 1 done --note "LXC browser checks <date>, build config-items-<sha>: checks 1, 2 and 5 passed"
bash "$REG" set $R 2 done --note "LXC browser checks <date>, build config-items-<sha>: check 4 passed"
bash "$REG" set $R 3 done --note "LXC browser checks <date>, build config-items-<sha>: checks 1, 2, 3 and 5 passed"
```

Without the owner's yes in Step 1, or for a row whose checks did not all pass, record it as `verify` instead, for example:

```bash
bash "$REG" set $R 1 verify --note "LXC browser checks not run: no owner yes on <date>"
```

and the same for rows 2 and 3. Then close the two rows Task 6 answers:

```bash
C=docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md
bash "$REG" set $C 232 done --assigned docs/superpowers/plans/2026-10-13-ghr-ui-config-items.md --note "Task 6: the restart warning event is written once per change of the pending list"
bash "$REG" set $C 263 done --assigned docs/superpowers/plans/2026-10-13-ghr-ui-config-items.md --note "Task 6: the restart warning event is written once per change of the pending list"
```

 Commit the register: `git add docs/superpowers/registers/ && git commit -m "docs(register): record config items checks"`.
