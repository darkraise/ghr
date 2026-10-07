# ghr First-run Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a fresh ghr install be finished from setup.sh, the `ghr setup` CLI or a web wizard, with the daemon serving before it has an owner or a token, and revert the loopback-only password setup.

**Architecture:** The store tolerates a missing owner and token and gains an atomic `Configure`. A daemon-level `setup` type serves `GET /setup`, `POST /setup/github` and `POST /setup/finish` in front of the API in both phases; `Run` serves a setup-only handler until owner and token are valid, then runs today's start-up path in-process and swaps the handler. The CLI gains `ghr setup`, the web UI a `/setup` wizard behind the session, and setup.sh prompts for owner and token with Enter to skip.

**Tech Stack:** Go 1.26, React 19 + TanStack Router/Query + Vitest, bash.

**Spec:** docs/superpowers/specs/2026-10-07-ghr-first-run-setup-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 4 of 14 tasks are heavy (Tasks 1, 3, 5, 6: risk 3) and are delegated; the rest top out at total 4 (impl-sonnet-medium), raised to high because tasks are delegated.

**Plan review:** 2026-10-07 — dr-superpowers:judge-opus — executability 16 / coherence 17 / coverage 16 / assumptions 15 (round 1)

> codex off — plugin-not-enabled

## Global Constraints

- ghr: Tasks 1–11 run in the worktree `D:/Repositories/Personal/ghr-first-run` on branch `feat/first-run-setup`, created from ghr master `b9560e6`. Paths in those tasks are relative to that worktree.
- homelab: Tasks 12–13 run in the worktree `D:/Repositories/Personal/homelab-first-run` on branch `feat/first-run-setup` from homelab master. Paths are relative to that worktree.
- After Task 11 and after Task 13, each branch is finished with dr-superpowers:finishing-a-development-branch (fast-forward into master, worktree removed) once the owner says yes. Task 14 runs only after both.
- Commits: `<type>(<scope>): <subject>`, subject ≤50 chars, imperative, no period; English only. Never `--no-verify`. Every push waits for the owner's explicit yes in chat.
- Go checks, from the ghr worktree: `timeout 900 go build ./... && timeout 900 go vet ./... && timeout 900 go test ./...`, and `gofmt -l .` must print nothing before each Go commit. `go test -race` does not run on this Windows box; CI runs it on Linux.
- Web checks, from `web/`: if `node_modules` is missing run `timeout 600 npm ci` first; then `timeout 600 npm test`, `timeout 300 npm run lint`, `timeout 600 npm run build`. Lint runs with `--max-warnings 0`, and `react-refresh/only-export-components` warns on a `.tsx` file that exports a hook or an object next to a component.
- setup.sh checks, from `github-runner/`: `timeout 300 bash tests/setup_test.sh`, `bash -n setup.sh`, and `uvx --from shellcheck-py shellcheck setup.sh tests/setup_test.sh`.
- Comments: none unless the why is non-obvious; never reference this plan, a task, a register row or a review.
- Copy strings in Contracts are exact; tests assert them.
- Every outward step in Task 14 (merge, push, release, deploy, any change on an LXC) waits for the owner's explicit yes in chat.

## Contracts

- **C1 config** — `config.Validate` (`internal/config/config.go`) no longer reports an empty owner; every other rule is unchanged.
- **C2 store** (`internal/daemon/store.go`):
  - `var ErrConfigured = errors.New("already configured; the owner changes only by editing config.yaml and restarting ghr, and the token with: ghr token set")`
  - `type OwnerMismatchError struct{ Owner string }`, `Error()` = `config.yaml names owner <Owner>; edit config.yaml to change it`
  - `func (s *Store) Configured() bool` — non-empty trimmed owner and non-empty token.
  - `func (s *Store) Configure(owner, token string) error` — one `s.mu` hold: `ErrConfigured` if configured; `*OwnerMismatchError` if config names another owner (case-insensitive); writes the token (0600, temp + rename), then the owner when it differs, validated and saved.
  - `OpenStore`/`Reload` read a missing or empty-after-trim token file as no token while none is loaded; once a token is loaded, today's errors stand. The `owner changed` guard applies only when the current owner is non-empty.
- **C3 manager** — `runner.Manager` gains exported `Epoch string`; `Init` uses it when non-empty, else mints one as today.
- **C4 model** (`internal/model/model.go`):
  - `type SetupState struct { Configured bool "configured"; Starting bool "starting"; SetupPending bool "setup_pending"; ToolchainsPending bool "toolchains_pending"; Owner string "owner"; WebListen string "web_listen" }` (JSON names as quoted, no omitempty).
  - `type SetupGitHubRequest struct { Owner string "owner"; Token string "token" }`, `type SetupFinishRequest struct { Toolchains string "toolchains" }`.
  - `Status` gains `Unconfigured bool` `json:"unconfigured,omitempty"` and `SetupPending bool` `json:"setup_pending,omitempty"`.
- **C5 setup routes** (`internal/daemon/setup.go`): `type setup` with fields `store *Store`, `events *events.Ring`, `githubURL, setupPending, toolchainsPending, webListen, epoch string`, `webSetupRequired func() bool`, `ready func()`, `backend atomic.Pointer[Backend]`, `mu sync.Mutex` (serialises `POST /setup/github`); methods `routes(next http.Handler) http.Handler` and `unconfigured() http.Handler`; helpers `exists(path string) bool`, `writeJSON`. Routes and answers:
  - `GET /setup` → `model.SetupState`; `configured` = `backend` set; `starting` = not set and `store.Configured()`.
  - `POST /setup/github` → 204; 400 `invalid request body` / `owner must be a GitHub user or organisation name` / `token is required` / `config.yaml names owner <o>; edit config.yaml to change it` / `the token cannot see any repository of <owner>; grant it access to at least one` / `token rejected by GitHub: <err>` / `the token cannot see <owner>/<repo>`; 409 `ErrConfigured`'s text; 429 with `retry_at`; 502 `cannot reach GitHub: <err>`.
  - `POST /setup/finish` → 204 and event `first-run setup finished`; 400 `toolchains must be popular or none`; 409 `ghr is not configured yet; finish GitHub setup first`.
  - Setup phase: `GET /status` reduced (`unconfigured: true`), `GET /events`, anything else 503 `ghr is not configured yet; finish setup first`.
- **C6 daemon start** (`internal/daemon/run.go`): `Options` gains `SetupPendingPath`, `ToolchainsPendingPath` (defaults `/var/lib/ghr/setup-pending`, `/var/lib/ghr/toolchains-pending`); `Backend` gains `SetupPending func() bool`. Events: `ghr is not configured; finish setup at http://<this host>:<port>/setup or run: ghr setup github --owner <owner>` (without the web UI: `ghr is not configured; run: ghr setup github --owner <owner>`), `ghr configured for owner <owner>; starting`, `config and token reloaded`, `reload rejected, keeping previous config: <err>`, and today's `ghr daemon started (owner %s, mode %s)`.
- **C7 API client** (`internal/api/client.go`): `SetupState(ctx) (model.SetupState, error)` → `GET /setup`; `SetupGitHub(ctx, owner, token string) error` → `POST /setup/github`; `SetupFinish(ctx, toolchains string) error` → `POST /setup/finish`.
- **C8 CLI** (`cmd/ghr/setup.go`):
  - `ghr setup` prints `configured: yes|starting|no`, `wizard: pending|done`, `owner: <owner or ->`, `web: <web_listen or ->`, one per line.
  - `ghr setup github [--owner <owner>]` prints `GitHub owner and token set; ghr is running`; errors `ghr setup github needs --owner <owner>: config.yaml names none` (exit 2) and `owner and token saved, but ghr is still starting; check: journalctl -u ghr -n 50` (exit 1); prompt `GitHub token: `.
  - `ghr setup finish [--toolchains popular|none]` prints `first-run setup finished`.
  - usage `usage: ghr setup [github [--owner <owner>] | finish [--toolchains popular|none]]`.
  - `printStatus` while unconfigured: `warning: ghr is not configured. Run: ghr setup github --owner <owner>, or open the web UI`, then the password warning if set, nothing else.
- **C9 web client**: `SetupState` interface in `web/src/api/types.ts`; `Status` gains `unconfigured?: boolean`, `setup_pending?: boolean`; `api.setupState(signal?)`, `api.setupGitHub(owner, token)`, `api.setupFinish(toolchains: "popular" | "none")`; `keys.setup = ["setup"]`; `useSetupState(poll: boolean)` (polls every second while `poll` or the data says `starting`); `export const setupStart = { limitMs: 60_000 }` in `web/src/api/hooks.ts`.
- **C10 settings form**: `web/src/lib/settings-form.ts` exports `interface SettingsForm { config: Config | undefined; values: Values; changed: string[]; errors: Record<string, string>; offline: boolean; saving: boolean; rejection: string; set: (key: string) => (value: unknown) => void; save: () => Promise<boolean>; discard: () => void }` and `useSettingsForm(): SettingsForm`; `web/src/components/settings-form.tsx` exports `SettingsSections({ form }: { form: SettingsForm })`.
- **C11 wizard**: `SetupPage` in `web/src/pages/setup.tsx` at `/setup`, under layout route id `setup` (session required, no Shell). Texts: heading `Set up ghr`; steps `Password`, `GitHub`, `Repositories`, `Settings`, `Toolchains`, `Finish`; buttons `Save and start ghr`, `Next`, `Back`, `Skip the rest`, `Finish`, `Retry`, `Save settings`, `+ Add repository`; `Starting ghr…`; `ghr is still starting; check journalctl -u ghr on the host`; dialog `Save your changes?` with `Stay`, `Discard`, `Save`; switch `Popular set`; login hint `No password is set. Choose one to claim this ghr.`; Shell banner `First-run setup is not finished.` with link `Finish setup`.
- **C12 setup.sh**: functions `stdin_is_tty`, `setup_field <name>`, `configure_github`, `print_next`; `install_config` touches `$STATE_DIR/setup-pending`; messages `GHR_TOKEN is ignored without an owner; set GHR_OWNER`, `toolchains can be chosen in the setup wizard, or later with: ghr toolchain install --preset popular`, `finish setup in the browser: http://<host>:<port>/setup`, `finish setup with: ghr setup github --owner <owner>`, `end first-run setup with: ghr setup finish`.

## Assumptions (evidence)

- ghr master `b9560e6` equals `origin/master` (`git rev-list --count origin/master..master` → 0, 2026-10-07); v0.1.15 is the latest release, published 2026-10-07T10:19:37Z (`gh release list`), so the next push releases v0.1.16 (CI tags `latest + 1`, `.github/workflows/ci.yml:57-73`, read for the loopback plan 2026-10-07).
- The runner LXC runs v0.1.15 and `ghr status` warns that the web UI has no password (`ssh root@192.168.0.99 'ghr version; ghr status'`, 2026-10-07): the paused v0.1.15 rollout removed it and kept the owner's hash at `/root/web-password.keep` (`.superpowers/sdd/2026-10-07-ghr-web-setup-loopback/progress.md`, last line).
- `Run` serves only after `Adopt` and `Reconcile` today, and tears servers down only on ctx cancel (`internal/daemon/run.go:137-145, 186-191, 286-316`); `signal.Notify` is registered after start-up (`run.go:270-276`); the epoch is minted in `Manager.Init` (`internal/runner/manager.go:199`). Read 2026-10-07.
- `webui.Handler` mounts `/api/` behind `requireSession` (`internal/webui/handler.go:41`); `socketHandler` keeps the password routes outside the API handler (`internal/daemon/web.go:19-55`).
- GitHub client: `Authorization: Bearer <token>` (`internal/github/client.go:253`); `classify` maps 401 → `ErrAuth`, 403/429 with `X-RateLimit-Remaining: 0` → `ErrRateLimit` with `RetryAt` from `X-RateLimit-Reset`, other 403 → `ErrAuth`, 404 → `ErrNotFound`, 5xx → `ErrServer` (`client.go:315-354`); `APIError.Error()` is `github: <status> <message>` (`client.go:41`); no retry on 5xx (`grep -n Sleep client.go`, 2026-10-07); `ListUserRepos` pages through `/user/repos?per_page=100` (`internal/github/releases.go:115-127`).
- `checkToken` wraps the runner and run listing errors with `%w` (`internal/daemon/run.go:105-118`), so `errors.As` still finds the `*github.APIError`.
- `fakeStorage` in `internal/daemon/storage_test.go:18-48` records `preset <name>`; `Backend.InstallToolchain` maps storage errors (`internal/daemon/backend.go:554-580`).
- Renaming a file onto an existing directory fails on Windows and Linux: unverified — Task 3 verifies it (its failure tests depend on it).
- Web: `mockApi` answers an unmocked route with 404 (`web/src/test/api.ts:45`); router tests mock only `GET /auth/state` (`web/src/router.test.tsx:7-8`), so a setup-state read that fails must not redirect (spec §5). `AddRepoDialog` invalidates status and config after adding (`web/src/components/add-repo-dialog.tsx:87-88`). The config fixture has `global_max: 2` and repos `darkmem`, `darkcloud` (`web/src/api/fixtures/config.json`).
- darkraise-ui `AlertDialogContent` has role `alertdialog` (`web/src/pages/storage.test.tsx:71`, `runner-detail.test.tsx:40`) and `Switch` takes its name from a `Label htmlFor` (`web/src/components/add-repo-dialog.test.tsx:121`). Whether darkraise-ui's `CardTitle` renders a heading is unknown, so the wizard tests query its own step heading with `level: 2` (`ToolchainsCard` has a `Toolchains` title, `toolchains-card.tsx:54`).
- Deviations from spec §3, decided here: a configured start serves only after `start()` returns, as today (so `wait_ready` still waits for adoption, matching spec §6); and the full handler is stored before `backend`, so `GET /setup` never reports `configured` while the setup handler still serves.
- `preinstall_toolchains` applies `GHR_TOOLCHAINS=none` before asking `ghr setup`: an explicit none drops the pending preset even while unconfigured, because the owner asked for none.
- shellcheck runs here as `uvx --from shellcheck-py shellcheck` (loopback ledger, Task 3, 2026-10-07).
- setup_test.sh stubs `ghr`, `timeout` (drops its first argument) and `sleep`, and runs functions with `run` under errexit (`github-runner/tests/setup_test.sh:32-118`); test `ghr setup` with the default stub prints to stderr and fails, so `setup_field` must swallow that.
- Codex lane: `codex-gate usable=false reason=plugin-not-enabled` (2026-10-07), so no task carries an Executor line.

## Task index

1. Revert the loopback-only setup guard
2. Allow a config without an owner
3. First-run configure in the store
4. Manager takes its epoch from the daemon
5. Setup routes
6. Two-phase daemon start
7. ghr setup CLI and client
8. Web setup client
9. Share the settings form
10. Setup wizard page
11. Gate the app on setup state
12. setup.sh first-run prompts
13. Docs and superseded records
14. Rollout v0.1.16

---

### Task 1: Revert the loopback-only setup guard

**Files:**
- Modify: `internal/webui/auth.go` (error var block, `ErrSetupRemote` at line 31)
- Modify: `internal/webui/handler.go` (`fromLoopback` at 168-179, the guard in `setup` at 182-185, the `fail` case at 267-268)
- Test: `internal/webui/handler_test.go` (`TestSetupOnlyFromLoopback`, line 341 to the end)
- Modify: `web/src/pages/login.tsx` (setup hint, line 75)
- Test: `web/src/pages/login.test.tsx` (line 26)

**Interfaces:**
- Consumes: none
- Produces: C11 login hint

**Items:** 1, 2, 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 0 - risk 3 = 5

- [ ] **Step 1: Write the failing tests**

In `internal/webui/handler_test.go`, replace the whole `TestSetupOnlyFromLoopback` function (from `func TestSetupOnlyFromLoopback` to the end of the file) with:

```go
func TestSetupFromAnyAddress(t *testing.T) {
	for name, mod := range map[string]func(*http.Request){
		"LAN address":     nil,
		"X-Forwarded-For": func(r *http.Request) { r.Header.Set("X-Forwarded-For", "192.168.0.10") },
		"loopback":        local(),
	} {
		h, a, _ := newTestHandler(t)
		rec := do(h, http.MethodPost, "/auth/setup", body(pw), mod)
		if rec.Code != http.StatusNoContent || len(rec.Result().Cookies()) == 0 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		if req, err := a.SetupRequired(); req || err != nil {
			t.Errorf("%s: setup required %v err %v", name, req, err)
		}
	}
}
```

In `web/src/pages/login.test.tsx`, change the expected hint string to `"No password is set. Choose one to claim this ghr."`.

- [ ] **Step 2: Run them to verify they fail**

Run: `timeout 900 go test ./internal/webui/ -run TestSetupFromAnyAddress` — Expected: FAIL (`LAN address: 403` and `X-Forwarded-For: 403`).
Run from `web/`: `timeout 600 npx vitest run src/pages/login.test.tsx` — Expected: FAIL (hint text not found).

- [ ] **Step 3: Remove the guard and reword the hint**

- `internal/webui/auth.go`: delete the line `ErrSetupRemote    = errors.New("set the first password over SSH: ghr web set-password (or open this page through an SSH tunnel to localhost)")`. Keep `remoteAddr` and `ClientKey` exactly as they are.
- `internal/webui/handler.go`: delete the `fromLoopback` function with its three-line comment; delete these four lines at the top of `setup`:

```go
	if !fromLoopback(r) {
		h.fail(w, ErrSetupRemote)
		return
	}
```

  and delete these two lines from `fail`:

```go
	case errors.Is(err, ErrSetupRemote):
		writeError(w, http.StatusForbidden, err.Error())
```

- `web/src/pages/login.tsx`: replace `"No password is set. Run ghr web set-password over SSH, or set it here from the host itself."` with `"No password is set. Choose one to claim this ghr."`.

Then run `gofmt -l .` (expect nothing); remove an import only if `go build` reports it unused.

- [ ] **Step 4: Run the checks**

Run: Go checks (Global Constraints) — Expected: PASS. Run from `web/`: `timeout 600 npm test`, `timeout 300 npm run lint`, `timeout 600 npm run build` — Expected: PASS.
Then use the Grep tool on the worktree (excluding `web/node_modules`) for `ErrSetupRemote`, `fromLoopback` and `from the host itself` — Expected: no matches.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/auth.go internal/webui/handler.go internal/webui/handler_test.go web/src/pages/login.tsx web/src/pages/login.test.tsx
git commit -m "fix(webui): let the first visitor claim the password"
```

---

### Task 2: Allow a config without an owner

**Files:**
- Modify: `internal/config/config.go:282-284`
- Test: `internal/config/config_test.go:82` and a new test

**Interfaces:**
- Consumes: none
- Produces: C1

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`, delete the case line `"owner is required":            func(c *Config) { c.Owner = "" },` from `TestValidateErrors`, and add at the end of the file:

```go
func TestEmptyOwnerValidates(t *testing.T) {
	c, _, err := Parse([]byte(strings.Replace(sample, "owner: darkraise", `owner: ""`, 1)))
	if err != nil || c.Owner != "" {
		t.Fatalf("empty owner: err %v owner %q", err, c.Owner)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 900 go test ./internal/config/ -run TestEmptyOwnerValidates` — Expected: FAIL with `owner is required`.

- [ ] **Step 3: Remove the rule**

In `Validate` (`internal/config/config.go`), delete:

```go
	if strings.TrimSpace(c.Owner) == "" {
		errs = append(errs, "owner is required")
	}
```

If `strings` becomes unused, `go vet` says so; it is used elsewhere in the file today.

- [ ] **Step 4: Run the checks**

Run: Go checks — Expected: PASS. The daemon's `TestStoreUpdateReloadToken` still passes: its `owner: ""` reload used to fail validation and now fails the owner-change guard; both are errors, which is all it asserts.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): allow an empty owner"
```

---

### Task 3: First-run configure in the store

**Files:**
- Modify: `internal/daemon/store.go` (whole file below the imports)
- Test: `internal/daemon/store_test.go`

**Interfaces:**
- Consumes: C1
- Produces: C2

**Items:** 4, 5

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

Add to `internal/daemon/store_test.go` (add `errors`, `fmt`, `strconv`, `sync` and `sync/atomic` to its imports):

```go
// unconfiguredStore opens a store whose config names owner (possibly empty)
// and whose token file holds token, or is missing when token is nil.
func unconfiguredStore(t *testing.T, owner string, token *string) *Store {
	t.Helper()
	dir := t.TempDir()
	cfg := strings.Replace(cfgYAML, "owner: darkraise", "owner: "+strconv.Quote(owner), 1)
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600)
	if token != nil {
		os.WriteFile(filepath.Join(dir, "token"), []byte(*token), 0o600)
	}
	s, _, err := OpenStore(filepath.Join(dir, "config.yaml"), filepath.Join(dir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreOpensUnconfigured(t *testing.T) {
	empty := " \n"
	for name, s := range map[string]*Store{
		"no token file":    unconfiguredStore(t, "", nil),
		"empty token file": unconfiguredStore(t, "", &empty),
		"owner, no token":  unconfiguredStore(t, "darkraise", nil),
	} {
		if s.Configured() || s.Token() != "" {
			t.Errorf("%s: configured %v token %q", name, s.Configured(), s.Token())
		}
	}
	if !newStore(t).Configured() {
		t.Fatal("owner and token: not configured")
	}
}

func TestStoreReloadSetsTheFirstOwnerAndToken(t *testing.T) {
	s := unconfiguredStore(t, "", nil)
	os.WriteFile(s.ConfigPath, []byte(cfgYAML), 0o600)
	if _, err := s.Reload(); err != nil || s.Configured() || s.Config().Owner != "darkraise" {
		t.Fatalf("owner without a token: err %v configured %v", err, s.Configured())
	}
	os.WriteFile(s.TokenPath, []byte("tok1\n"), 0o600)
	if _, err := s.Reload(); err != nil || !s.Configured() || s.Token() != "tok1" {
		t.Fatalf("owner and token: err %v token %q", err, s.Token())
	}
	os.WriteFile(s.TokenPath, []byte("\n"), 0o600)
	if _, err := s.Reload(); err == nil || s.Token() != "tok1" {
		t.Fatalf("emptied token after a token: err %v token %q", err, s.Token())
	}
	os.Remove(s.TokenPath)
	if _, err := s.Reload(); err == nil || s.Token() != "tok1" {
		t.Fatalf("removed token after a token: err %v token %q", err, s.Token())
	}
}

func TestStoreConfigure(t *testing.T) {
	s := unconfiguredStore(t, "", nil)
	if err := s.Configure("DarkRaise", "tok1"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.TokenPath)
	if !s.Configured() || s.Token() != "tok1" || string(data) != "tok1\n" || s.Config().Owner != "DarkRaise" {
		t.Fatalf("after configure: owner %q token %q file %q", s.Config().Owner, s.Token(), data)
	}
	if c, _, err := config.Load(s.ConfigPath); err != nil || c.Owner != "DarkRaise" {
		t.Fatalf("saved config: err %v", err)
	}
	if err := s.Configure("DarkRaise", "tok2"); !errors.Is(err, ErrConfigured) || s.Token() != "tok1" {
		t.Fatalf("second configure: %v", err)
	}

	s = unconfiguredStore(t, "darkraise", nil)
	var mismatch *OwnerMismatchError
	err := s.Configure("someone-else", "tok1")
	if !errors.As(err, &mismatch) || mismatch.Owner != "darkraise" ||
		err.Error() != "config.yaml names owner darkraise; edit config.yaml to change it" {
		t.Fatalf("another owner: %v", err)
	}
	if _, err := os.Stat(s.TokenPath); !os.IsNotExist(err) {
		t.Fatal("a refused configure wrote the token")
	}
	if err := s.Configure("DarkRaise", "tok1"); err != nil || s.Config().Owner != "DarkRaise" {
		t.Fatalf("config's owner in another case: err %v owner %q", err, s.Config().Owner)
	}
}

func TestStoreConfigureWriteFailures(t *testing.T) {
	s := unconfiguredStore(t, "", nil)
	os.Mkdir(s.TokenPath, 0o755)
	if err := s.Configure("darkraise", "tok1"); err == nil || s.Configured() || s.Token() != "" || s.Config().Owner != "" {
		t.Fatalf("token write failure: err %v token %q owner %q", err, s.Token(), s.Config().Owner)
	}
	if data, _ := os.ReadFile(s.ConfigPath); strings.Contains(string(data), "darkraise") {
		t.Fatal("the owner was saved although the token write failed")
	}

	s = unconfiguredStore(t, "", nil)
	saved, _ := os.ReadFile(s.ConfigPath)
	os.Remove(s.ConfigPath)
	os.Mkdir(s.ConfigPath, 0o755)
	if err := s.Configure("darkraise", "tok1"); err == nil || s.Configured() || s.Token() != "tok1" {
		t.Fatalf("owner write failure: err %v configured %v token %q", err, s.Configured(), s.Token())
	}
	os.Remove(s.ConfigPath)
	os.WriteFile(s.ConfigPath, saved, 0o600)
	if err := s.Configure("darkraise", "tok2"); err != nil || !s.Configured() || s.Token() != "tok2" {
		t.Fatalf("retry: err %v token %q", err, s.Token())
	}
}

func TestStoreConfiguresOnce(t *testing.T) {
	s := unconfiguredStore(t, "", nil)
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Configure("darkraise", fmt.Sprintf("tok%d", i)) == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d configures succeeded", ok.Load())
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `timeout 900 go test ./internal/daemon/ -run 'TestStore'` — Expected: FAIL to compile (`Configured`, `Configure`, `ErrConfigured`, `OwnerMismatchError` undefined).

- [ ] **Step 3: Write the implementation**

Replace everything in `internal/daemon/store.go` from `// Store owns the live config and token.` to the end of the file with the code below, and add `"io/fs"` to its imports:

```go
// ErrConfigured refuses a first-run configure once ghr has an owner and a token.
var ErrConfigured = errors.New("already configured; the owner changes only by editing config.yaml and restarting ghr, and the token with: ghr token set")

// OwnerMismatchError refuses a first-run owner other than the one config.yaml names.
type OwnerMismatchError struct{ Owner string }

func (e *OwnerMismatchError) Error() string {
	return "config.yaml names owner " + e.Owner + "; edit config.yaml to change it"
}

// Store owns the live config and token. Writes are validated and saved atomically.
type Store struct {
	ConfigPath string
	TokenPath  string

	mu    sync.Mutex
	cfg   atomic.Pointer[config.Config]
	token atomic.Pointer[string]
}

func OpenStore(configPath, tokenPath string) (*Store, []string, error) {
	s := &Store{ConfigPath: configPath, TokenPath: tokenPath}
	warnings, err := s.Reload()
	return s, warnings, err
}

func (s *Store) Config() *config.Config { return s.cfg.Load() }

func (s *Store) Token() string {
	if t := s.token.Load(); t != nil {
		return *t
	}
	return ""
}

// Configured reports whether ghr has an owner and a token, the two things
// the manager cannot start without.
func (s *Store) Configured() bool {
	return strings.TrimSpace(s.Config().Owner) != "" && s.Token() != ""
}

// Reload re-reads config and token; on any error the previous values stay active.
// The owner cannot change while the daemon runs: the GitHub client and every
// live runner belong to the owner it started with. A first owner may be set.
func (s *Store) Reload() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, warnings, err := config.Load(s.ConfigPath)
	if err != nil {
		return nil, err
	}
	if cur := s.cfg.Load(); cur != nil && cur.Owner != "" && cur.Owner != cfg.Owner {
		return nil, fmt.Errorf("owner changed from %s to %s: restart ghr to switch owners", cur.Owner, cfg.Owner)
	}
	tok, err := s.readToken()
	if err != nil {
		return nil, err
	}
	s.cfg.Store(cfg)
	if tok != "" {
		s.token.Store(&tok)
	}
	return warnings, nil
}

// readToken returns the token file's trimmed content. Until a token has been
// loaded, a missing or empty file means "not configured yet"; after that it is
// an error, so a reload never drops a working token.
func (s *Store) readToken() (string, error) {
	loaded := s.token.Load() != nil
	data, err := os.ReadFile(s.TokenPath)
	if errors.Is(err, fs.ErrNotExist) && !loaded {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" && loaded {
		return "", errors.New("token file is empty")
	}
	return tok, nil
}

// Update applies fn to a copy of the config, validates, saves, then swaps it in.
func (s *Store) Update(fn func(c *config.Config) error) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.Config().Clone()
	if err := fn(c); err != nil {
		return nil, err
	}
	warnings, err := c.Validate()
	if err != nil {
		return nil, err
	}
	if err := config.Save(s.ConfigPath, c); err != nil {
		return nil, err
	}
	s.cfg.Store(c)
	return warnings, nil
}

// SetToken writes the token file (0600, atomic) and swaps it in.
func (s *Store) SetToken(tok string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeToken(tok)
}

// Configure sets the first owner and token. The check and both writes happen
// under one hold of s.mu, so a concurrent reload cannot configure another
// owner in between. The token is written first: if the owner write then
// fails, ghr stays unconfigured and a retry overwrites the token.
func (s *Store) Configure(owner, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Configured() {
		return ErrConfigured
	}
	cur := s.Config()
	if cur.Owner != "" && !strings.EqualFold(cur.Owner, owner) {
		return &OwnerMismatchError{Owner: cur.Owner}
	}
	if err := s.writeToken(token); err != nil {
		return err
	}
	if cur.Owner == owner {
		return nil
	}
	c := cur.Clone()
	c.Owner = owner
	if err := config.Save(s.ConfigPath, c); err != nil {
		return err
	}
	s.cfg.Store(c)
	return nil
}

// writeToken writes the token file (0600, temporary file and rename); the
// caller holds s.mu.
func (s *Store) writeToken(tok string) error {
	tmp, err := os.CreateTemp(filepath.Dir(s.TokenPath), ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(tok + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.TokenPath); err != nil {
		return err
	}
	s.token.Store(&tok)
	return nil
}
```

`config.Save` validates before writing, so `Configure` needs no separate `Validate` call.

- [ ] **Step 4: Run the checks**

Run: `timeout 900 go test ./internal/daemon/ -run 'TestStore'` — Expected: PASS. If `TestStoreConfigureWriteFailures` fails because a rename onto a directory succeeded on this OS, report it (the assumption is wrong) instead of changing the test's intent.
Run: Go checks — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/store.go internal/daemon/store_test.go
git commit -m "feat(daemon): add first-run store configure"
```

---

### Task 4: Manager takes its epoch from the daemon

**Files:**
- Modify: `internal/runner/manager.go` (`Manager` struct at 142-158, `Init` at 190-199)
- Test: `internal/runner/manager_test.go`

**Interfaces:**
- Consumes: none
- Produces: C3

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Add to `internal/runner/manager_test.go`:

```go
func TestInitKeepsAGivenEpoch(t *testing.T) {
	h := newHarness(t)
	h.m.Epoch = "given"
	if err := h.m.Init(); err != nil {
		t.Fatal(err)
	}
	if got := h.m.Status().Epoch; got != "given" {
		t.Fatalf("epoch %q", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `timeout 900 go test ./internal/runner/ -run TestInitKeepsAGivenEpoch` — Expected: FAIL to compile (`h.m.Epoch undefined`).

- [ ] **Step 3: Write the implementation**

In the `Manager` struct, after the `PruneDone func()` field and its comment, add:

```go
	// Epoch, when set, is the status epoch Init uses instead of minting one,
	// so a daemon that served before the manager started keeps one epoch.
	Epoch string
```

In `Init`, replace `m.epoch = strconv.FormatInt(m.Now().UnixNano(), 36)` with:

```go
	m.epoch = m.Epoch
	if m.epoch == "" {
		m.epoch = strconv.FormatInt(m.Now().UnixNano(), 36)
	}
```

- [ ] **Step 4: Run the checks**

Run: `timeout 900 go test ./internal/runner/` — Expected: PASS, including `TestStatusEpochChangesPerStart`. Then the Go checks — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/runner/manager.go internal/runner/manager_test.go
git commit -m "feat(runner): accept the epoch from the daemon"
```

---

### Task 5: Setup routes

**Files:**
- Modify: `internal/model/model.go` (`Status` at 6-22, new types)
- Create: `internal/daemon/setup.go`
- Test: `internal/daemon/setup_test.go`

**Interfaces:**
- Consumes: C2
- Produces: C4, C5

**Items:** 4, 5

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Add the model types**

In `internal/model/model.go`, add to `Status` after `WebSetupRequired`:

```go
	// Unconfigured is true while ghr has no owner or token; an older daemon
	// omits it, which reads as configured.
	Unconfigured bool `json:"unconfigured,omitempty"`
	// SetupPending is true until first-run setup is finished.
	SetupPending bool `json:"setup_pending,omitempty"`
```

and after the `Status` type:

```go
// SetupState is GET /setup. Configured means the full API serves; Starting
// means owner and token are saved and the manager is still starting.
type SetupState struct {
	Configured        bool   `json:"configured"`
	Starting          bool   `json:"starting"`
	SetupPending      bool   `json:"setup_pending"`
	ToolchainsPending bool   `json:"toolchains_pending"`
	Owner             string `json:"owner"`
	WebListen         string `json:"web_listen"`
}

type SetupGitHubRequest struct {
	Owner string `json:"owner"`
	Token string `json:"token"`
}

type SetupFinishRequest struct {
	Toolchains string `json:"toolchains"`
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/daemon/setup_test.go`:

```go
package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/model"
)

// fakeSetupGitHub answers the reads first-run setup and a configured start
// make. The bearer token picks the behaviour: "good" sees DarkRaise/Alpha,
// DarkRaise/darkmem and someone-else/x; "other" sees only someone-else/x;
// "bad" is rejected; "noadmin" cannot list runners; "norepo" cannot read the
// repository; "limited" is rate limited; "down" gets a 502.
func fakeSetupGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		p := r.URL.Path
		switch tok {
		case "bad":
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"message":"Bad credentials"}`)
			return
		case "limited":
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "4102444800")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
			return
		case "down":
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		switch {
		case p == "/user/repos":
			if tok == "other" {
				fmt.Fprint(w, `[{"name":"x","private":true,"owner":{"login":"someone-else"}}]`)
				return
			}
			fmt.Fprint(w, `[{"name":"darkmem","private":true,"owner":{"login":"DarkRaise"}},`+
				`{"name":"Alpha","private":true,"owner":{"login":"DarkRaise"}},`+
				`{"name":"x","private":true,"owner":{"login":"someone-else"}}]`)
		case strings.HasSuffix(p, "/actions/runners"):
			if tok == "noadmin" {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"message":"Resource not accessible by personal access token"}`)
				return
			}
			fmt.Fprint(w, `{"total_count":0,"runners":[]}`)
		case strings.HasSuffix(p, "/actions/runs"):
			fmt.Fprint(w, `{"total_count":0,"workflow_runs":[]}`)
		case strings.HasPrefix(p, "/repos/") && strings.Count(p, "/") == 3:
			if tok == "norepo" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"Not Found"}`)
				return
			}
			fmt.Fprintf(w, `{"name":%q,"private":true}`, p[strings.LastIndex(p, "/")+1:])
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestSetup builds a setup over a store whose config names owner (possibly
// empty) and that has no token; readies counts calls to ready.
func newTestSetup(t *testing.T, owner string) (*setup, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	cfg := strings.Replace(cfgYAML, "owner: darkraise", "owner: "+strconv.Quote(owner), 1)
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600)
	store, _, err := OpenStore(filepath.Join(dir, "config.yaml"), filepath.Join(dir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	readies := &atomic.Int32{}
	s := &setup{
		store: store, events: events.New(), githubURL: fakeSetupGitHub(t).URL,
		setupPending: filepath.Join(dir, "setup-pending"), toolchainsPending: filepath.Join(dir, "toolchains-pending"),
		webListen: "0.0.0.0:8080", epoch: "e1", ready: func() { readies.Add(1) },
	}
	return s, readies
}

func setupRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestSetupGitHubRefusals(t *testing.T) {
	const ownerMsg = "owner must be a GitHub user or organisation name"
	for _, c := range []struct {
		name, body string
		code       int
		want       string
	}{
		{"owner syntax", `{"owner":"-bad","token":"good"}`, 400, ownerMsg},
		{"owner length", `{"owner":"` + strings.Repeat("a", 40) + `","token":"good"}`, 400, ownerMsg},
		{"no owner anywhere", `{"token":"good"}`, 400, ownerMsg},
		{"empty token", `{"owner":"DarkRaise","token":"  "}`, 400, "token is required"},
		{"trailing data", `{"owner":"DarkRaise","token":"good"} x`, 400, "invalid request body"},
		{"no repo of the owner", `{"owner":"DarkRaise","token":"other"}`, 400,
			"the token cannot see any repository of DarkRaise; grant it access to at least one"},
		{"bad token", `{"owner":"DarkRaise","token":"bad"}`, 400, "token rejected by GitHub: github: 401 Bad credentials"},
		{"no runner access", `{"owner":"DarkRaise","token":"noadmin"}`, 400,
			"token rejected by GitHub: listing runners failed; the token needs Administration: read/write"},
		{"repo not visible", `{"owner":"DarkRaise","token":"norepo"}`, 400, "the token cannot see DarkRaise/Alpha"},
		{"rate limited", `{"owner":"DarkRaise","token":"limited"}`, 429, `"retry_at":"2100-01-01T00:00:00Z"`},
		{"GitHub down", `{"owner":"DarkRaise","token":"down"}`, 502, "cannot reach GitHub: github: 502"},
	} {
		s, readies := newTestSetup(t, "")
		rec := setupRequest(s.routes(s.unconfigured()), http.MethodPost, "/setup/github", c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
		if readies.Load() != 0 || s.store.Configured() {
			t.Errorf("%s: readies %d configured %v", c.name, readies.Load(), s.store.Configured())
		}
		if _, err := os.Stat(s.store.TokenPath); !os.IsNotExist(err) {
			t.Errorf("%s: the token was written", c.name)
		}
	}
}

func TestSetupGitHubConfigures(t *testing.T) {
	s, readies := newTestSetup(t, "")
	h := s.routes(s.unconfigured())
	if rec := setupRequest(h, http.MethodPost, "/setup/github", `{"owner":"darkraise","token":" good "}`); rec.Code != http.StatusNoContent {
		t.Fatalf("configure: %d %s", rec.Code, rec.Body.String())
	}
	data, _ := os.ReadFile(s.store.TokenPath)
	if readies.Load() != 1 || s.store.Config().Owner != "DarkRaise" || string(data) != "good\n" {
		t.Fatalf("readies %d owner %q token file %q", readies.Load(), s.store.Config().Owner, data)
	}
	var st model.SetupState
	json.Unmarshal(setupRequest(h, http.MethodGet, "/setup", "").Body.Bytes(), &st)
	if st != (model.SetupState{Starting: true, Owner: "DarkRaise", WebListen: "0.0.0.0:8080"}) {
		t.Fatalf("state %+v", st)
	}
	rec := setupRequest(h, http.MethodPost, "/setup/github", `{"owner":"DarkRaise","token":"good"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), ErrConfigured.Error()) || readies.Load() != 1 {
		t.Fatalf("second configure: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSetupGitHubUsesTheConfigOwner(t *testing.T) {
	s, readies := newTestSetup(t, "DarkRaise")
	h := s.routes(s.unconfigured())
	rec := setupRequest(h, http.MethodPost, "/setup/github", `{"owner":"someone-else","token":"good"}`)
	if rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "config.yaml names owner DarkRaise; edit config.yaml to change it") || readies.Load() != 0 {
		t.Fatalf("another owner: %d %s", rec.Code, rec.Body.String())
	}
	if rec := setupRequest(h, http.MethodPost, "/setup/github", `{"token":"good"}`); rec.Code != http.StatusNoContent || readies.Load() != 1 {
		t.Fatalf("config's owner: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSetupGitHubOneAtATime(t *testing.T) {
	s, readies := newTestSetup(t, "")
	h := s.routes(s.unconfigured())
	codes := make([]int, 8)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = setupRequest(h, http.MethodPost, "/setup/github", `{"owner":"DarkRaise","token":"good"}`).Code
		}()
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		switch c {
		case http.StatusNoContent:
			ok++
		case http.StatusConflict:
		default:
			t.Errorf("code %d", c)
		}
	}
	if ok != 1 || readies.Load() != 1 {
		t.Fatalf("codes %v readies %d", codes, readies.Load())
	}
}

func TestSetupPhase(t *testing.T) {
	s, _ := newTestSetup(t, "")
	s.webSetupRequired = func() bool { return true }
	os.WriteFile(s.setupPending, nil, 0o600)
	s.events.Add("warn", "", "ghr is not configured")
	h := s.routes(s.unconfigured())

	rec := setupRequest(h, http.MethodGet, "/status", "")
	var st model.Status
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &st) != nil || !st.Unconfigured || !st.SetupPending ||
		!st.WebSetupRequired || st.Epoch != "e1" || st.Mode != "queue" || st.GlobalMax != 2 ||
		!strings.Contains(rec.Body.String(), `"repos":[]`) || !strings.Contains(rec.Body.String(), `"instances":[]`) {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	if rec := setupRequest(h, http.MethodGet, "/events", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "ghr is not configured") {
		t.Fatalf("events: %d %s", rec.Code, rec.Body.String())
	}
	if rec := setupRequest(h, http.MethodGet, "/config", ""); rec.Code != http.StatusServiceUnavailable ||
		!strings.Contains(rec.Body.String(), "ghr is not configured yet; finish setup first") {
		t.Fatalf("config: %d %s", rec.Code, rec.Body.String())
	}
	if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"none"}`); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "ghr is not configured yet; finish GitHub setup first") {
		t.Fatalf("finish: %d %s", rec.Code, rec.Body.String())
	}
	var state model.SetupState
	json.Unmarshal(setupRequest(h, http.MethodGet, "/setup", "").Body.Bytes(), &state)
	if state != (model.SetupState{SetupPending: true, WebListen: "0.0.0.0:8080"}) {
		t.Fatalf("state %+v", state)
	}
}

func TestSetupFinish(t *testing.T) {
	s, _ := newTestSetup(t, "DarkRaise")
	space := &fakeStorage{}
	s.backend.Store(&Backend{Space: space, Events: s.events})
	h := s.routes(http.NotFoundHandler())
	if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"all"}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "toolchains must be popular or none") {
		t.Fatalf("bad value: %d %s", rec.Code, rec.Body.String())
	}
	os.WriteFile(s.setupPending, nil, 0o600)
	os.WriteFile(s.toolchainsPending, nil, 0o600)
	if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"popular"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("popular: %d %s", rec.Code, rec.Body.String())
	}
	if len(space.calls) != 1 || space.calls[0] != "preset popular" || exists(s.setupPending) || exists(s.toolchainsPending) {
		t.Fatalf("calls %v", space.calls)
	}
	if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"none"}`); rec.Code != http.StatusNoContent || len(space.calls) != 1 {
		t.Fatalf("none with the markers gone: %d calls %v", rec.Code, space.calls)
	}
	var state model.SetupState
	json.Unmarshal(setupRequest(h, http.MethodGet, "/setup", "").Body.Bytes(), &state)
	if !state.Configured || state.Starting {
		t.Fatalf("state with a backend: %+v", state)
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `timeout 900 go test ./internal/daemon/ -run 'TestSetup'` — Expected: FAIL to compile (`setup` undefined).

- [ ] **Step 4: Write the implementation**

Create `internal/daemon/setup.go`:

```go
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// maxSetupBody caps a setup request body; a token is far below it.
const maxSetupBody = 16 << 10

// githubLogin is a GitHub user or organisation name: letters, digits and
// single inner hyphens; its length (1 to 39) is checked separately.
var githubLogin = regexp.MustCompile(`^[A-Za-z0-9]+(-[A-Za-z0-9]+)*$`)

// setup serves first-run setup in front of the API, before ghr has an owner
// and a token and after, so the wizard and the CLI use one set of routes.
type setup struct {
	store             *Store
	events            *events.Ring
	githubURL         string
	setupPending      string
	toolchainsPending string
	webListen         string
	epoch             string
	// webSetupRequired reports a web listener with no password; nil without one.
	webSetupRequired func() bool
	// ready is called once owner and token are saved; Run then starts the manager.
	ready func()
	// backend is set just before the full API replaces the setup phase's handler.
	backend atomic.Pointer[Backend]
	mu      sync.Mutex
}

func (s *setup) routes(next http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /setup", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.state()) })
	mux.HandleFunc("POST /setup/github", s.github)
	mux.HandleFunc("POST /setup/finish", s.finish)
	mux.Handle("/", next)
	return mux
}

// unconfigured is the API before ghr has an owner and a token: a reduced
// status, the event feed, and 503 for everything else.
func (s *setup) unconfigured() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.status()) })
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		ev := s.events.After(after)
		if ev == nil {
			ev = []model.Event{}
		}
		writeJSON(w, ev)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeSocketError(w, http.StatusServiceUnavailable, "ghr is not configured yet; finish setup first")
	})
	return mux
}

func (s *setup) state() model.SetupState {
	serving := s.backend.Load() != nil
	return model.SetupState{
		Configured:        serving,
		Starting:          !serving && s.store.Configured(),
		SetupPending:      exists(s.setupPending),
		ToolchainsPending: exists(s.toolchainsPending),
		Owner:             s.store.Config().Owner,
		WebListen:         s.webListen,
	}
}

func (s *setup) status() model.Status {
	cfg := s.store.Config()
	return model.Status{
		Now: time.Now(), Epoch: s.epoch, Mode: cfg.Mode, GlobalMax: cfg.GlobalMax,
		Repos: []model.RepoStatus{}, Instances: []model.InstanceStatus{},
		WebSetupRequired: s.webSetupRequired != nil && s.webSetupRequired(),
		Unconfigured:     true,
		SetupPending:     exists(s.setupPending),
	}
}

func (s *setup) github(w http.ResponseWriter, r *http.Request) {
	var req model.SetupGitHubRequest
	if !decodeSetupBody(w, r, &req) {
		return
	}
	if err := s.configure(r.Context(), req.Owner, req.Token); err != nil {
		writeSetupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// configure validates owner and token against GitHub, saves them and starts
// the manager. One request at a time, so a second one gets the 409.
func (s *setup) configure(ctx context.Context, owner, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.store.Config().Owner
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = current
	}
	if len(owner) > 39 || !githubLogin.MatchString(owner) {
		return api.BadRequest("owner must be a GitHub user or organisation name")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return api.BadRequest("token is required")
	}
	if s.store.Configured() {
		return api.Conflict(ErrConfigured.Error())
	}
	if current != "" && !strings.EqualFold(current, owner) {
		return api.BadRequest((&OwnerMismatchError{Owner: current}).Error())
	}
	login, err := s.check(ctx, owner, token)
	if err != nil {
		return err
	}
	var mismatch *OwnerMismatchError
	switch err := s.store.Configure(login, token); {
	case errors.Is(err, ErrConfigured):
		return api.Conflict(err.Error())
	case errors.As(err, &mismatch):
		return api.BadRequest(err.Error())
	case err != nil:
		return err
	}
	s.ready()
	return nil
}

// check lists the repositories the token can see and runs checkToken on the
// first of owner's, returning owner's login as GitHub spells it.
func (s *setup) check(ctx context.Context, owner, token string) (string, error) {
	c := github.New(owner, func() string { return token })
	if s.githubURL != "" {
		c.BaseURL = s.githubURL
	}
	repos, err := c.ListUserRepos(ctx)
	if err != nil {
		return "", setupGitHubError(err, "")
	}
	var mine []github.UserRepo
	for _, r := range repos {
		if strings.EqualFold(r.Owner.Login, owner) {
			mine = append(mine, r)
		}
	}
	if len(mine) == 0 {
		return "", api.BadRequest(fmt.Sprintf("the token cannot see any repository of %s; grant it access to at least one", owner))
	}
	sort.Slice(mine, func(i, j int) bool { return strings.ToLower(mine[i].Name) < strings.ToLower(mine[j].Name) })
	first := mine[0]
	if err := checkToken(ctx, c, first.Name); err != nil {
		return "", setupGitHubError(err, owner+"/"+first.Name)
	}
	return first.Owner.Login, nil
}

// setupGitHubError maps a GitHub failure during setup. repo names the
// repository checkToken read, or is empty for the repository listing.
func setupGitHubError(err error, repo string) error {
	var ge *github.APIError
	if errors.As(err, &ge) {
		switch {
		case ge.Kind == github.ErrAuth:
			return api.BadRequest("token rejected by GitHub: " + err.Error())
		case ge.Kind == github.ErrNotFound && repo != "":
			return api.BadRequest("the token cannot see " + repo)
		case ge.Kind == github.ErrRateLimit:
			return &api.Error{Status: http.StatusTooManyRequests, Msg: err.Error(), RetryAt: ge.RetryAt}
		}
	}
	return &api.Error{Status: http.StatusBadGateway, Msg: "cannot reach GitHub: " + err.Error()}
}

func (s *setup) finish(w http.ResponseWriter, r *http.Request) {
	var req model.SetupFinishRequest
	if !decodeSetupBody(w, r, &req) {
		return
	}
	if req.Toolchains != "popular" && req.Toolchains != "none" {
		writeSocketError(w, http.StatusBadRequest, "toolchains must be popular or none")
		return
	}
	b := s.backend.Load()
	if b == nil {
		writeSocketError(w, http.StatusConflict, "ghr is not configured yet; finish GitHub setup first")
		return
	}
	if req.Toolchains == "popular" {
		if err := b.InstallToolchain(model.InstallRequest{Preset: "popular"}); err != nil {
			writeSetupError(w, err)
			return
		}
	}
	for _, p := range []string{s.toolchainsPending, s.setupPending} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			writeSetupError(w, err)
			return
		}
	}
	s.events.Add("info", "", "first-run setup finished")
	w.WriteHeader(http.StatusNoContent)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// decodeSetupBody reads a capped JSON body; Unmarshal, unlike a Decoder,
// rejects data after the JSON value.
func decodeSetupBody(w http.ResponseWriter, r *http.Request, v any) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSetupBody))
	if err == nil {
		err = json.Unmarshal(data, v)
	}
	if err != nil {
		writeSocketError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func writeSetupError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	body := map[string]string{"error": err.Error()}
	var ae *api.Error
	if errors.As(err, &ae) {
		status = ae.Status
		if !ae.RetryAt.IsZero() {
			body["retry_at"] = ae.RetryAt.UTC().Format(time.RFC3339)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
```

- [ ] **Step 5: Run the checks**

Run: `timeout 900 go test ./internal/daemon/ -run 'TestSetup'` — Expected: PASS. Then the Go checks — Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/model/model.go internal/daemon/setup.go internal/daemon/setup_test.go
git commit -m "feat(daemon): serve first-run setup routes"
```

---

### Task 6: Two-phase daemon start

**Files:**
- Modify: `internal/daemon/run.go` (imports; `Options` at 31-52; `DefaultOptions` at 60-79; `Run` from line 120 to the end)
- Modify: `internal/daemon/backend.go` (`Backend` struct at 67-95, `Status` at 117-123)
- Test: `internal/daemon/run_test.go` (imports, `testOptions` at 242-258, new tests)

**Interfaces:**
- Consumes: C2, C3, C4, C5
- Produces: C6

**Items:** 5

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/run_test.go`, add `"encoding/json"` to the imports, and in `testOptions` add these two fields to the returned `Options`:

```go
		SetupPendingPath: filepath.Join(dir, "setup-pending"), ToolchainsPendingPath: filepath.Join(dir, "toolchains-pending"),
```

Then add at the end of the file:

```go
// startRun runs the daemon until the test ends; done receives Run's result.
func startRun(t *testing.T, o Options) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() { done <- Run(ctx, o); close(finished) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("Run did not return")
		}
	})
	return cancel, done
}

// unconfiguredOptions is testOptions with no owner and no token file, a
// GitHub that answers first-run setup, and a reload channel the test sends on.
func unconfiguredOptions(t *testing.T, extra string) (Options, chan os.Signal) {
	t.Helper()
	o := testOptions(t, strings.Replace(cfgYAML, "owner: darkraise", `owner: ""`, 1)+extra)
	os.Remove(o.TokenPath)
	o.GitHubURL = fakeSetupGitHub(t).URL
	reload := make(chan os.Signal, 1)
	o.Reload = reload
	return o, reload
}

// socketCall is a raw request over the Unix socket.
func socketCall(socket, method, path, body string) (int, string, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, "http://ghr"+path, rd)
	resp, err := api.NewUnixClient(socket).HTTP.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), nil
}

func setupStateOver(socket string) (model.SetupState, bool) {
	code, body, err := socketCall(socket, http.MethodGet, "/setup", "")
	var st model.SetupState
	if err != nil || code != http.StatusOK || json.Unmarshal([]byte(body), &st) != nil {
		return st, false
	}
	return st, true
}

// webCall calls the web listener with the X-GHR header and an optional session.
func webCall(t *testing.T, addr, method, path, body, session string) (*http.Response, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, "http://"+addr+path, rd)
	req.Header.Set("X-GHR", "1")
	if session != "" {
		req.AddCookie(&http.Cookie{Name: "ghr_session", Value: session})
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func eventMsgs(t *testing.T, socket string) string {
	t.Helper()
	evs, err := api.NewUnixClient(socket).Events(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, e := range evs {
		msgs = append(msgs, e.Msg)
	}
	return strings.Join(msgs, "\n")
}

func TestRunConfiguresFromTheWebUI(t *testing.T) {
	addr := freeAddr(t)
	_, port, _ := net.SplitHostPort(addr)
	o, _ := unconfiguredOptions(t, "web:\n  listen: "+addr+"\n")
	cancel, done := startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	if st, _ := setupStateOver(o.Socket); st.Configured || st.Starting || st.Owner != "" || st.WebListen != addr {
		t.Fatalf("before configuring: %+v", st)
	}
	if resp, _ := webCall(t, addr, http.MethodGet, "/api/setup", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("setup state without a session: %d", resp.StatusCode)
	}
	if resp, _ := webCall(t, addr, http.MethodPost, "/api/setup/github", `{"owner":"darkraise","token":"good"}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("configure without a session: %d", resp.StatusCode)
	}
	resp, body := webCall(t, addr, http.MethodPost, "/auth/setup", `{"password":"correct horse battery"}`, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("claim: %d %s", resp.StatusCode, body)
	}
	var session string
	for _, ck := range resp.Cookies() {
		if ck.Name == "ghr_session" {
			session = ck.Value
		}
	}
	resp, body = webCall(t, addr, http.MethodGet, "/api/status", "", session)
	var before model.Status
	if resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &before) != nil || !before.Unconfigured || before.Epoch == "" {
		t.Fatalf("status before: %d %s", resp.StatusCode, body)
	}
	if resp, body := webCall(t, addr, http.MethodGet, "/api/config", "", session); resp.StatusCode != http.StatusServiceUnavailable ||
		!strings.Contains(body, "ghr is not configured yet; finish setup first") {
		t.Fatalf("config before: %d %s", resp.StatusCode, body)
	}
	if resp, body := webCall(t, addr, http.MethodPost, "/api/setup/github", `{"owner":"darkraise","token":"good"}`, session); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("configure: %d %s", resp.StatusCode, body)
	}
	waitFor(t, "the full API", func() bool { st, _ := setupStateOver(o.Socket); return st.Configured })
	resp, body = webCall(t, addr, http.MethodGet, "/api/status", "", session)
	var after model.Status
	if resp.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &after) != nil || after.Unconfigured || after.Epoch != before.Epoch {
		t.Fatalf("status after, same session: %d %s (epoch before %q)", resp.StatusCode, body, before.Epoch)
	}
	msgs := eventMsgs(t, o.Socket)
	for _, want := range []string{
		"ghr is not configured; finish setup at http://<this host>:" + port + "/setup or run: ghr setup github --owner <owner>",
		"ghr configured for owner DarkRaise; starting",
		"ghr daemon started (owner DarkRaise, mode queue)",
	} {
		if !strings.Contains(msgs, want) {
			t.Errorf("events lack %q:\n%s", want, msgs)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener outlived Run: %v", err)
	}
	l.Close()
}

func TestRunConfiguresOnReload(t *testing.T) {
	o, reload := unconfiguredOptions(t, "")
	startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	if code, body, _ := socketCall(o.Socket, http.MethodGet, "/config", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("config before: %d %s", code, body)
	}
	if !strings.Contains(eventMsgs(t, o.Socket), "ghr is not configured; run: ghr setup github --owner <owner>") {
		t.Fatal("no start-up warning without the web UI")
	}
	os.WriteFile(o.ConfigPath, []byte(cfgYAML), 0o600)
	reload <- os.Interrupt
	waitFor(t, "the owner-only reload", func() bool { return strings.Contains(eventMsgs(t, o.Socket), "config and token reloaded") })
	if st, _ := setupStateOver(o.Socket); st.Configured || st.Starting || st.Owner != "darkraise" {
		t.Fatalf("owner without a token: %+v", st)
	}
	os.WriteFile(o.TokenPath, []byte("good\n"), 0o600)
	reload <- os.Interrupt
	waitFor(t, "the full API", func() bool { st, _ := setupStateOver(o.Socket); return st.Configured })
	if st, err := api.NewUnixClient(o.Socket).Status(context.Background()); err != nil || st.Unconfigured {
		t.Fatalf("status after: %+v %v", st, err)
	}
}

func TestRunConfiguresOnceWhenReloadAndSetupRace(t *testing.T) {
	o, reload := unconfiguredOptions(t, "")
	startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	os.WriteFile(o.ConfigPath, []byte(cfgYAML), 0o600)
	os.WriteFile(o.TokenPath, []byte("good\n"), 0o600)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		socketCall(o.Socket, http.MethodPost, "/setup/github", `{"owner":"darkraise","token":"good"}`)
	}()
	reload <- os.Interrupt
	wg.Wait()
	waitFor(t, "the full API", func() bool { st, _ := setupStateOver(o.Socket); return st.Configured })
	if n := strings.Count(eventMsgs(t, o.Socket), "ghr daemon started"); n != 1 {
		t.Fatalf("the manager started %d times", n)
	}
}

func TestRunStopsWhileUnconfigured(t *testing.T) {
	addr := freeAddr(t)
	o, _ := unconfiguredOptions(t, "web:\n  listen: "+addr+"\n")
	cancel, done := startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	if err := api.NewUnixClient(o.Socket).SetWebPassword(context.Background(), "set over the socket"); err != nil {
		t.Fatalf("set-password while unconfigured: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener outlived Run: %v", err)
	}
	l.Close()
	if _, err := api.NewUnixClient(o.Socket).Status(context.Background()); err == nil {
		t.Fatal("the socket still answers")
	}
}

func TestRunClosesListenersWhenAConfiguredStartFails(t *testing.T) {
	addr := freeAddr(t)
	o, _ := unconfiguredOptions(t, "web:\n  listen: "+addr+"\n")
	if err := os.MkdirAll(o.HistoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	_, done := startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	if code, body, err := socketCall(o.Socket, http.MethodPost, "/setup/github", `{"owner":"darkraise","token":"good"}`); err != nil || code != http.StatusNoContent {
		t.Fatalf("configure: %d %s %v", code, body, err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a start with an unreadable history returned nil")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the failed start")
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener was left open: %v", err)
	}
	l.Close()
}

func TestRunReportsAPendingSetup(t *testing.T) {
	o := testOptions(t, cfgYAML)
	os.WriteFile(o.SetupPendingPath, nil, 0o600)
	startRun(t, o)
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket", func() bool { _, err := c.Status(context.Background()); return err == nil })
	if st, _ := c.Status(context.Background()); !st.SetupPending || st.Unconfigured {
		t.Fatalf("status %+v", st)
	}
	if st, _ := setupStateOver(o.Socket); !st.Configured || !st.SetupPending || st.Owner != "darkraise" {
		t.Fatalf("setup state %+v", st)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `timeout 900 go test ./internal/daemon/ -run 'TestRun'` — Expected: FAIL to compile (`SetupPendingPath` unknown field).

- [ ] **Step 3: Extend `Backend`**

In `internal/daemon/backend.go`, add to the `Backend` struct after `WebSetupRequired func() bool`:

```go
	// SetupPending reports an unfinished first-run setup; nil reports false.
	SetupPending func() bool
```

and in `Status`, before `return st`:

```go
	if b.SetupPending != nil {
		st.SetupPending = b.SetupPending()
	}
```

- [ ] **Step 4: Rewrite the start-up**

In `internal/daemon/run.go`:

- Imports: add `"strconv"`, `"sync/atomic"` and `"github.com/darkraise/ghr/internal/config"`.
- `Options`: after `WebPasswordPath string`, add:

```go
	// SetupPendingPath marks an unfinished first-run setup and
	// ToolchainsPendingPath an unmade toolchain choice; setup.sh creates both
	// on a first install.
	SetupPendingPath      string
	ToolchainsPendingPath string
```

- `DefaultOptions`: after `WebPasswordPath: "/etc/ghr/web-password",` add `SetupPendingPath: "/var/lib/ghr/setup-pending", ToolchainsPendingPath: "/var/lib/ghr/toolchains-pending",` (gofmt aligns it).
- Replace everything from `// Run starts the daemon and blocks until ctx is cancelled.` to the end of the file with:

```go
// swapHandler serves the handler stored last, so the setup phase hands over
// to the full API without rebinding a listener.
type swapHandler struct{ h atomic.Pointer[http.Handler] }

func (s *swapHandler) Store(h http.Handler) { s.h.Store(&h) }

func (s *swapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { (*s.h.Load()).ServeHTTP(w, r) }

// running is what a configured start builds.
type running struct {
	m       *runner.Manager
	space   *storage.Service
	b       *Backend
	sampled chan struct{}
	wake    chan struct{}
}

// start reads the owner once, then adopts and reconciles the runners already
// on the host before anything serves the full API.
func start(ctx context.Context, o Options, store *Store, ev *events.Ring, epoch string, webCfg config.Web, webSetupRequired func() bool) (*running, error) {
	owner := store.Config().Owner
	gh := github.New(owner, store.Token)
	if o.GitHubURL != "" {
		gh.BaseURL = o.GitHubURL
	}
	hist := &history.Store{Path: o.HistoryPath}
	m := &runner.Manager{
		Config: store.Config, GH: gh, SD: o.Systemd, Docker: o.Docker, Disk: o.Disk, Host: o.Host, Fetch: o.Fetch,
		Paths: o.Paths, Events: ev, History: hist, Now: time.Now, NewID: runner.RandomID, Epoch: epoch,
	}
	if m.SD == nil {
		m.SD = system.Systemd{Run: system.Exec}
	}
	if m.Docker == nil {
		m.Docker = system.Docker{Run: system.Exec}
	}
	if m.Disk == nil {
		m.Disk = system.Docker{Run: system.Exec}
	}
	if m.Host == nil {
		m.Host = system.Host{Run: system.Exec, Script: system.ExecGroup}
	}
	if m.Fetch == nil {
		m.Fetch = system.Download
	}
	if err := m.Init(); err != nil {
		return nil, err
	}
	if err := m.Adopt(ctx); err != nil {
		return nil, err
	}
	m.Reconcile(ctx, store.Config())

	tools := toolchain.New(toolchain.NewEnv(o.Paths.ToolCache, o.Paths.Home, runner.RunnerUser,
		system.ExecGroupFor(InstallTimeout), system.DownloadFor(InstallTimeout)))
	space := &storage.Service{Tools: tools, Docker: m.Disk, Home: o.Paths.Home, User: runner.RunnerUser,
		Busy: m.BusyCount, Events: ev}
	space.Start()
	m.PruneDone = space.Trigger

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
	sampled := make(chan struct{})
	go func() { sampler.Run(ctx, time.Minute); close(sampled) }()

	wake := make(chan struct{}, 1)
	b := &Backend{
		Store: store, M: m, GH: gh, Events: ev, Hist: hist, Space: space,
		CheckToken: func(ctx context.Context, token, repo string) error {
			c := github.New(owner, func() string { return token })
			c.BaseURL = gh.BaseURL
			return checkToken(ctx, c, repo)
		},
		Sampler:          sampler,
		WebApplied:       webCfg,
		WebSetupRequired: webSetupRequired,
		SetupPending:     func() bool { return exists(o.SetupPendingPath) },
		Wake: func() {
			select {
			case wake <- struct{}{}:
			default:
			}
		},
	}
	return &running{m: m, space: space, b: b, sampled: sampled, wake: wake}, nil
}

// notConfigured is the start-up warning while ghr has no owner or token.
func notConfigured(webLn net.Listener) string {
	const cli = "run: ghr setup github --owner <owner>"
	if webLn == nil {
		return "ghr is not configured; " + cli
	}
	_, port, _ := net.SplitHostPort(webLn.Addr().String())
	return "ghr is not configured; finish setup at http://<this host>:" + port + "/setup or " + cli
}

// Run starts the daemon and blocks until ctx is cancelled. Runner units keep running after it exits.
// Without an owner or a token it serves first-run setup until both are set,
// then starts the manager in the same process.
func Run(ctx context.Context, o Options) error {
	store, warnings, err := OpenStore(o.ConfigPath, o.TokenPath)
	if err != nil {
		return err
	}
	ln, err := listen(o.Socket)
	if err != nil {
		return err
	}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	webSrv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
	var webLn net.Listener
	served := false
	defer func() {
		if !served {
			ln.Close()
			if webLn != nil {
				webLn.Close()
			}
		}
	}()
	webCfg := store.Config().Web
	if webCfg.Listen != "" {
		if webLn, err = net.Listen("tcp", webCfg.Listen); err != nil {
			return fmt.Errorf("web listener: %w", err)
		}
	}
	auth := webui.NewAuth(o.WebPasswordPath, time.Now, rand.Reader)

	ev := events.New()
	for _, w := range warnings {
		ev.Add("warn", "", "config: %s", w)
	}
	if err := auth.Check(); err != nil {
		ev.Add("warn", "", "%v", err)
	}
	epoch := strconv.FormatInt(time.Now().UnixNano(), 36)

	reload := o.Reload
	if reload == nil {
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		defer signal.Stop(hup)
		reload = hup
	}

	var webSetupRequired func() bool
	webListen := ""
	if webLn != nil {
		webListen = webLn.Addr().String()
		webSetupRequired = func() bool {
			// An unreadable file is reported by the start-up warning instead.
			req, err := auth.SetupRequired()
			return err == nil && req
		}
	}
	ready := make(chan struct{})
	var readyOnce sync.Once
	su := &setup{
		store: store, events: ev, githubURL: o.GitHubURL,
		setupPending: o.SetupPendingPath, toolchainsPending: o.ToolchainsPendingPath,
		webListen: webListen, epoch: epoch, webSetupRequired: webSetupRequired,
		ready: func() { readyOnce.Do(func() { close(ready) }) },
	}
	handler := &swapHandler{}
	srv.Handler = socketHandler(handler, auth, ev)
	if webLn != nil {
		static, err := fs.Sub(web.Dist, "dist")
		if err != nil {
			return err
		}
		webSrv.Handler = webui.Handler(auth, handler, static, webCfg.Hosts)
	}
	serve := func() {
		served = true
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("api server: %v", err)
			}
		}()
		if webLn == nil {
			return
		}
		go func() {
			if err := webSrv.Serve(webLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("web server: %v", err)
			}
		}()
		ev.Add("info", "", "web UI listening on %s", webLn.Addr())
		if webSetupRequired() {
			ev.Add("warn", "", "web UI on %s has no password; run: ghr web set-password", webLn.Addr())
		}
	}
	// Every return after serve goes through teardown, so no path leaves the
	// socket or the web port bound.
	teardown := func() {
		var wg sync.WaitGroup
		for _, s := range []*http.Server{srv, webSrv} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.Shutdown(shutdownCtx); err != nil {
					// Shutdown leaves connections open when its deadline passes.
					s.Close()
				}
			}()
		}
		wg.Wait()
	}

	unconfigured := !store.Configured()
	if unconfigured {
		handler.Store(su.routes(su.unconfigured()))
		serve()
		ev.Add("warn", "", "%s", notConfigured(webLn))
	wait:
		for {
			select {
			case <-ctx.Done():
				teardown()
				return nil
			case <-ready:
				break wait
			case <-reload:
				ws, err := store.Reload()
				if err != nil {
					ev.Add("error", "", "reload rejected, keeping previous config: %v", err)
					continue
				}
				for _, w := range ws {
					ev.Add("warn", "", "config: %s", w)
				}
				ev.Add("info", "", "config and token reloaded")
				if store.Configured() {
					su.ready()
				}
			}
		}
	}

	rt, err := start(ctx, o, store, ev, epoch, webCfg, webSetupRequired)
	if err != nil {
		if served {
			teardown()
		}
		return err
	}
	defer rt.space.Close()
	// Handler first: GET /setup reports configured only once the full API serves.
	handler.Store(su.routes(api.NewServer(rt.b)))
	su.backend.Store(rt.b)
	if !served {
		serve()
	}
	owner := store.Config().Owner
	if unconfigured {
		ev.Add("info", "", "ghr configured for owner %s; starting", owner)
	}
	ev.Add("info", "", "ghr daemon started (owner %s, mode %s)", owner, store.Config().Mode)

	m := rt.m
	timer := time.NewTimer(0)
	defer timer.Stop()
	tick := func() {
		m.Tick(ctx)
		rt.b.FinalizeRemovals()
		timer.Reset(store.Config().PollInterval.D())
	}
	for {
		select {
		case <-ctx.Done():
			teardown()
			m.Close()
			rt.space.Close()
			rt.b.Close()
			<-rt.sampled
			done := make(chan struct{})
			go func() {
				m.Wait()
				rt.space.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(o.ShutdownWait):
				log.Printf("shutdown: cleanups still running after %s; they resume at the next start", o.ShutdownWait)
			}
			return nil
		case <-reload:
			rt.b.Reload()
		case <-rt.wake:
			tick()
		case <-timer.C:
			tick()
		}
	}
}
```

Compare the body of `start` with the lines it replaces (`run.go:161-241` before this task): apart from `Epoch: epoch`, the `WebSetupRequired`/`SetupPending` fields and returning instead of serving, it must be the same code. If the old ctx branch differs from the one above in anything but `teardown()` and the `rt.` prefixes, keep the old behaviour and record the difference in the task report.

- [ ] **Step 5: Run the checks**

Run: `timeout 900 go test ./internal/daemon/` — Expected: PASS, including the existing `TestRunServesTicksReloadsAndKeepsRunners`, `TestRunServesTheWebUI`, `TestRunClosesTheWebListenerWhenStartFails` and `TestRunWithoutTheWebListener`. Then the Go checks — Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/run.go internal/daemon/backend.go internal/daemon/run_test.go
git commit -m "feat(daemon): start unconfigured, then configure"
```

---

### Task 7: ghr setup CLI and client

**Files:**
- Modify: `internal/api/client.go` (after `SetWebPassword`, line 280)
- Create: `cmd/ghr/setup.go`
- Test: `cmd/ghr/setup_test.go`
- Modify: `cmd/ghr/cli.go` (`cli` switch at 55-150, `printStatus` at 292)
- Test: `cmd/ghr/cli_test.go` (new test)
- Modify: `cmd/ghr/main.go` (`usage`, after the `web set-password` line)

**Interfaces:**
- Consumes: C4, C5
- Produces: C7, C8

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `cmd/ghr/setup_test.go`:

```go
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

// fakeSetupDaemon answers GET /setup with states in turn (the last repeats),
// refuses POST /setup/github with the token "bad", and accepts the rest.
func fakeSetupDaemon(t *testing.T, states ...model.SetupState) *[]recorded {
	t.Helper()
	var (
		mu   sync.Mutex
		reqs []recorded
		next int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		reqs = append(reqs, recorded{r.Method, r.URL.RequestURI(), string(b)})
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/setup":
			json.NewEncoder(w).Encode(states[min(next, len(states)-1)])
			next++
		case r.URL.Path == "/setup/github" && strings.Contains(string(b), `"token":"bad"`):
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "token rejected by GitHub: github: 401 Bad credentials"})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	newClient = func() *api.Client { return &api.Client{Base: srv.URL, HTTP: srv.Client()} }
	return &reqs
}

func fastSetupPoll(t *testing.T, wait time.Duration) {
	t.Helper()
	oldPoll, oldWait := setupPoll, setupWait
	setupPoll, setupWait = time.Millisecond, wait
	t.Cleanup(func() { setupPoll, setupWait = oldPoll, oldWait })
}

func TestSetupPrintsTheState(t *testing.T) {
	for _, c := range []struct {
		st   model.SetupState
		want string
	}{
		{model.SetupState{Configured: true, Owner: "DarkRaise", WebListen: "0.0.0.0:8080"}, "configured: yes\nwizard: done\nowner: DarkRaise\nweb: 0.0.0.0:8080\n"},
		{model.SetupState{Starting: true, SetupPending: true, Owner: "DarkRaise"}, "configured: starting\nwizard: pending\nowner: DarkRaise\nweb: -\n"},
		{model.SetupState{SetupPending: true}, "configured: no\nwizard: pending\nowner: -\nweb: -\n"},
	} {
		fakeSetupDaemon(t, c.st)
		if code, out, errOut := runCLI(t, "", "setup"); code != 0 || out != c.want {
			t.Errorf("%+v: exit %d out %q err %q", c.st, code, out, errOut)
		}
	}
}

func TestSetupGitHubFromAPipe(t *testing.T) {
	fastSetupPoll(t, time.Second)
	reqs := fakeSetupDaemon(t, model.SetupState{Starting: true}, model.SetupState{Configured: true})
	stubPasswordInput(t, false)
	code, out, errOut := runCLI(t, " tok \n", "setup", "github", "--owner", "DarkRaise")
	if code != 0 || out != "GitHub owner and token set; ghr is running\n" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	first := (*reqs)[0]
	if first.method != "POST" || first.path != "/setup/github" || first.body != `{"owner":"DarkRaise","token":"tok"}` {
		t.Fatalf("request %+v", first)
	}
	if len(*reqs) != 3 {
		t.Fatalf("want one POST and two polls, got %+v", *reqs)
	}
}

func TestSetupGitHubPrompts(t *testing.T) {
	fastSetupPoll(t, time.Second)
	reqs := fakeSetupDaemon(t, model.SetupState{Configured: true})
	stubPasswordInput(t, true, " tok ")
	code, _, errOut := runCLI(t, "", "setup", "github", "--owner", "DarkRaise")
	if code != 0 || errOut != "GitHub token: \n" || (*reqs)[0].body != `{"owner":"DarkRaise","token":"tok"}` {
		t.Fatalf("exit %d err %q requests %+v", code, errOut, *reqs)
	}
}

func TestSetupGitHubUsesTheConfigOwner(t *testing.T) {
	fastSetupPoll(t, time.Second)
	reqs := fakeSetupDaemon(t, model.SetupState{Owner: "DarkRaise"}, model.SetupState{Configured: true})
	stubPasswordInput(t, false)
	if code, _, errOut := runCLI(t, "tok\n", "setup", "github"); code != 0 || (*reqs)[1].body != `{"owner":"","token":"tok"}` {
		t.Fatalf("exit %d err %q requests %+v", code, errOut, *reqs)
	}
	fakeSetupDaemon(t, model.SetupState{})
	code, _, errOut := runCLI(t, "tok\n", "setup", "github")
	if code != 2 || !strings.Contains(errOut, "ghr setup github needs --owner <owner>: config.yaml names none") {
		t.Fatalf("no owner anywhere: exit %d err %q", code, errOut)
	}
}

func TestSetupGitHubReportsTheDaemon(t *testing.T) {
	reqs := fakeSetupDaemon(t, model.SetupState{Configured: true})
	stubPasswordInput(t, false)
	code, _, errOut := runCLI(t, "bad\n", "setup", "github", "--owner", "DarkRaise")
	if code != 1 || !strings.Contains(errOut, "ghr: token rejected by GitHub: github: 401 Bad credentials") || len(*reqs) != 1 {
		t.Fatalf("exit %d err %q requests %d", code, errOut, len(*reqs))
	}
}

func TestSetupGitHubGivesUpWaiting(t *testing.T) {
	fastSetupPoll(t, 20*time.Millisecond)
	fakeSetupDaemon(t, model.SetupState{Starting: true})
	stubPasswordInput(t, false)
	code, _, errOut := runCLI(t, "tok\n", "setup", "github", "--owner", "DarkRaise")
	if code != 1 || !strings.Contains(errOut, "ghr: owner and token saved, but ghr is still starting; check: journalctl -u ghr -n 50") {
		t.Fatalf("exit %d err %q", code, errOut)
	}
}

func TestSetupFinishAndUsage(t *testing.T) {
	reqs := fakeSetupDaemon(t, model.SetupState{})
	code, out, _ := runCLI(t, "", "setup", "finish")
	if last := (*reqs)[len(*reqs)-1]; code != 0 || out != "first-run setup finished\n" || last.path != "/setup/finish" || last.body != `{"toolchains":"none"}` {
		t.Fatalf("exit %d out %q request %+v", code, out, last)
	}
	runCLI(t, "", "setup", "finish", "--toolchains", "popular")
	if last := (*reqs)[len(*reqs)-1]; last.body != `{"toolchains":"popular"}` {
		t.Fatalf("popular: %+v", last)
	}
	for _, args := range [][]string{
		{"setup", "finish", "--toolchains", "all"}, {"setup", "finish", "now"}, {"setup", "bogus"},
		{"setup", "github", "--owner", "x", "extra"},
	} {
		code, _, errOut := runCLI(t, "", args...)
		if code != 2 || !strings.Contains(errOut, "usage: ghr setup [github [--owner <owner>] | finish [--toolchains popular|none]]") {
			t.Errorf("%v: exit %d err %q", args, code, errOut)
		}
	}
	_, out, _ = runCLI(t, "", "help")
	for _, want := range []string{"setup github [--owner <owner>]", "setup finish [--toolchains popular|none]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage lacks %q:\n%s", want, out)
		}
	}
}
```

Add to `cmd/ghr/cli_test.go`:

```go
func TestStatusWhileUnconfigured(t *testing.T) {
	var out bytes.Buffer
	printStatus(&out, model.Status{Mode: "queue", GlobalMax: 2, Unconfigured: true, WebSetupRequired: true})
	want := "warning: ghr is not configured. Run: ghr setup github --owner <owner>, or open the web UI\n" +
		"warning: the web UI has no password. Run: ghr web set-password\n"
	if out.String() != want {
		t.Fatalf("output:\n%s", out.String())
	}
	out.Reset()
	printStatus(&out, model.Status{Mode: "queue", GlobalMax: 2})
	if !strings.HasPrefix(out.String(), "mode queue") {
		t.Fatalf("a status without the field must print as configured:\n%s", out.String())
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `timeout 900 go test ./cmd/ghr/` — Expected: FAIL to compile (`setupPoll` undefined).

- [ ] **Step 3: Add the client methods**

Append to `internal/api/client.go`:

```go
// SetupState reports how far first-run setup has got.
func (c *Client) SetupState(ctx context.Context) (model.SetupState, error) {
	var s model.SetupState
	err := c.call(ctx, http.MethodGet, "/setup", nil, &s)
	return s, err
}

// SetupGitHub saves the GitHub owner and token of an unconfigured daemon; an
// empty owner keeps the one config.yaml names.
func (c *Client) SetupGitHub(ctx context.Context, owner, token string) error {
	return c.call(ctx, http.MethodPost, "/setup/github", jsonBody(model.SetupGitHubRequest{Owner: owner, Token: token}), nil)
}

// SetupFinish ends first-run setup; toolchains is "popular" or "none".
func (c *Client) SetupFinish(ctx context.Context, toolchains string) error {
	return c.call(ctx, http.MethodPost, "/setup/finish", jsonBody(model.SetupFinishRequest{Toolchains: toolchains}), nil)
}
```

- [ ] **Step 4: Write the command**

Create `cmd/ghr/setup.go`:

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

// setupPoll and setupWait bound how long `setup github` waits for the daemon
// to serve once owner and token are saved; tests shorten them.
var (
	setupPoll = time.Second
	setupWait = 60 * time.Second
)

const setupUsage = "usage: ghr setup [github [--owner <owner>] | finish [--toolchains popular|none]]"

func setupCmd(ctx context.Context, c *api.Client, args []string, stdin io.Reader, out, errOut io.Writer) error {
	if len(args) == 0 {
		st, err := c.SetupState(ctx)
		if err != nil {
			return err
		}
		printSetupState(out, st)
		return nil
	}
	switch args[0] {
	case "github":
		fs := flag.NewFlagSet("setup github", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		owner := fs.String("owner", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return flagError(err)
		}
		if fs.NArg() > 0 {
			return usageError(setupUsage)
		}
		if *owner == "" {
			st, err := c.SetupState(ctx)
			if err != nil {
				return err
			}
			if st.Owner == "" {
				return usageError("ghr setup github needs --owner <owner>: config.yaml names none")
			}
		}
		token, err := readToken(ctx, stdin, errOut)
		if err != nil {
			return err
		}
		if err := c.SetupGitHub(ctx, *owner, token); err != nil {
			return err
		}
		if err := waitConfigured(ctx, c); err != nil {
			return err
		}
		fmt.Fprintln(out, "GitHub owner and token set; ghr is running")
		return nil
	case "finish":
		fs := flag.NewFlagSet("setup finish", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		toolchains := fs.String("toolchains", "none", "")
		if err := fs.Parse(args[1:]); err != nil {
			return flagError(err)
		}
		if fs.NArg() > 0 || (*toolchains != "popular" && *toolchains != "none") {
			return usageError(setupUsage)
		}
		if err := c.SetupFinish(ctx, *toolchains); err != nil {
			return err
		}
		fmt.Fprintln(out, "first-run setup finished")
		return nil
	}
	return usageError(setupUsage)
}

func printSetupState(out io.Writer, st model.SetupState) {
	configured := "no"
	switch {
	case st.Configured:
		configured = "yes"
	case st.Starting:
		configured = "starting"
	}
	wizard := "done"
	if st.SetupPending {
		wizard = "pending"
	}
	fmt.Fprintf(out, "configured: %s\nwizard: %s\nowner: %s\nweb: %s\n", configured, wizard, orDash(st.Owner), orDash(st.WebListen))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// readToken asks once without echo on a terminal, or reads a pipe; the token
// is trimmed either way.
func readToken(ctx context.Context, stdin io.Reader, prompts io.Writer) (string, error) {
	if !stdinIsTerminal() {
		data, err := io.ReadAll(io.LimitReader(stdin, 64*1024))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	tok, err := askPassword(ctx, prompts, "GitHub token: ")
	return strings.TrimSpace(tok), err
}

// waitConfigured polls until the full API serves. A connection error counts
// as still starting: systemd restarts a daemon whose start failed.
func waitConfigured(ctx context.Context, c *api.Client) error {
	deadline := time.Now().Add(setupWait)
	for {
		if st, err := c.SetupState(ctx); err == nil && st.Configured {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errors.New("owner and token saved, but ghr is still starting; check: journalctl -u ghr -n 50")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(setupPoll):
		}
	}
}
```

- [ ] **Step 5: Wire it in**

- `cmd/ghr/cli.go`, in `cli`'s switch, before `case "web":`, add:

```go
	case "setup":
		return setupCmd(ctx, c, args[1:], stdin, out, errOut)
```

- `printStatus`: insert as its first statements:

```go
	if st.Unconfigured {
		fmt.Fprintln(out, "warning: ghr is not configured. Run: ghr setup github --owner <owner>, or open the web UI")
		if st.WebSetupRequired {
			fmt.Fprintln(out, "warning: the web UI has no password. Run: ghr web set-password")
		}
		return
	}
```

- `cmd/ghr/main.go`, in `usage`, after the `  web set-password                set the web UI password (prompts, or reads stdin)` line, add these three lines (the description column starts at the 35th character, as in the lines around them):

```
  setup                           first-run state: configured, wizard, owner
  setup github [--owner <owner>]  set the GitHub owner and token (prompts, or reads stdin)
  setup finish [--toolchains popular|none]
```

- [ ] **Step 6: Run the checks**

Run: `timeout 900 go test ./cmd/ghr/ ./internal/api/` — Expected: PASS. Then the Go checks — Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/api/client.go cmd/ghr/setup.go cmd/ghr/setup_test.go cmd/ghr/cli.go cmd/ghr/cli_test.go cmd/ghr/main.go
git commit -m "feat(cli): add ghr setup"
```

---

### Task 8: Web setup client

**Files:**
- Modify: `web/src/api/types.ts` (`Status` at 70-84, new interface)
- Modify: `web/src/api/client.ts` (`api` object at 81-128, type imports at 1-20)
- Modify: `web/src/api/hooks.ts` (`keys` at 10-27, new hook)
- Test: `web/src/api/setup.test.ts`

**Interfaces:**
- Consumes: C4, C5 (wire shapes)
- Produces: C9

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/api/setup.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import { api } from "@/api/client"
import type { SetupState } from "@/api/types"
import { mockApi, noContent } from "@/test/api"

describe("setup client", () => {
  it("reads the setup state and posts GitHub and finish", async () => {
    const state: SetupState = {
      configured: false,
      starting: false,
      setup_pending: true,
      toolchains_pending: true,
      owner: "",
      web_listen: "0.0.0.0:8080",
    }
    const { calls } = mockApi({
      "GET /api/setup": state,
      "POST /api/setup/github": () => noContent(),
      "POST /api/setup/finish": () => noContent(),
    })
    expect(await api.setupState()).toEqual(state)
    await api.setupGitHub("DarkRaise", "tok")
    await api.setupFinish("popular")
    expect(calls.map((c) => [c.method, c.path, c.body])).toEqual([
      ["GET", "/api/setup", undefined],
      ["POST", "/api/setup/github", { owner: "DarkRaise", token: "tok" }],
      ["POST", "/api/setup/finish", { toolchains: "popular" }],
    ])
  })
})
```

- [ ] **Step 2: Run it to verify it fails**

Run from `web/`: `timeout 600 npx vitest run src/api/setup.test.ts` — Expected: FAIL (`api.setupState is not a function`; TypeScript errors on `SetupState`).

- [ ] **Step 3: Write the implementation**

`web/src/api/types.ts` — add to `Status` after `web_setup_required?: boolean`:

```ts
  unconfigured?: boolean
  setup_pending?: boolean
```

and after the `Status` interface:

```ts
export interface SetupState {
  configured: boolean
  starting: boolean
  setup_pending: boolean
  toolchains_pending: boolean
  owner: string
  web_listen: string
}
```

`web/src/api/client.ts` — add `SetupState,` to the type import list (alphabetical, after `Registration,`), and add to the `api` object after `changePassword`:

```ts
  setupState: (signal?: AbortSignal) => request<SetupState>("GET", "/api/setup", undefined, signal),
  setupGitHub: (owner: string, token: string) => send("POST", "/api/setup/github", { owner, token }),
  setupFinish: (toolchains: "popular" | "none") => send("POST", "/api/setup/finish", { toolchains }),
```

`web/src/api/hooks.ts` — add `setup: ["setup"] as const,` to `keys` after `auth`, and after `useStatus`:

```ts
// How long the wizard waits for ghr to start after saving owner and token;
// tests shorten it.
export const setupStart = { limitMs: 60_000 }

// Polls while the caller waits for ghr to start, or while the daemon says it
// is starting (a reload in the middle of it).
export function useSetupState(poll: boolean) {
  return useQuery({
    queryKey: keys.setup,
    queryFn: ({ signal }) => api.setupState(signal),
    refetchInterval: (q) => (poll || q.state.data?.starting ? POLL_FAST : false),
  })
}
```

- [ ] **Step 4: Run the checks**

Run the web checks (Global Constraints) — Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/api/hooks.ts web/src/api/setup.test.ts
git commit -m "feat(web): add the setup API client"
```

---

### Task 9: Share the settings form

**Files:**
- Create: `web/src/lib/settings-form.ts`
- Create: `web/src/components/settings-form.tsx`
- Modify: `web/src/pages/settings.tsx` (whole file)

**Interfaces:**
- Consumes: none
- Produces: C10

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The existing `web/src/pages/settings.test.tsx` is the test for this task: it must pass unchanged, before and after.

- [ ] **Step 1: Run the existing test as the baseline**

Run from `web/`: `timeout 600 npx vitest run src/pages/settings.test.tsx` — Expected: PASS.

- [ ] **Step 2: Create the hook**

Create `web/src/lib/settings-form.ts`. Its body is the non-JSX part of today's `settings.tsx`, moved verbatim, plus the state that `SettingsPage` held:

```ts
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { Config, ConfigPatch, RunnerLimitsPatch } from "@/api/types"
import { changedKeys, sameValue, settle, type Equal, type Values } from "@/lib/draft"
import { DAY_MS, durationError, sameDuration } from "@/lib/duration"
import { errorText } from "@/query"

const FLOORS: Record<string, number> = { poll_interval: 5000, start_timeout: 0, idle_timeout: 0, history_retention: DAY_MS }

function loadedValues(c: Config): Values {
  return {
    mode: c.mode,
    global_max: c.global_max,
    poll_interval: c.poll_interval,
    start_timeout: c.start_timeout,
    idle_timeout: c.idle_timeout,
    disk_high_water: c.disk_high_water,
    build_cache_keep: c.build_cache_keep,
    history_retention: c.history_retention,
    labels: c.labels ?? [],
    memory_max: c.runner_limits.memory_max,
    cpu_quota: c.runner_limits.cpu_quota,
  }
}

const equal: Equal = (key, a, b) => {
  if (key in FLOORS) return sameDuration(String(a), String(b))
  return typeof a === "string" && typeof b === "string" ? a.trim() === b.trim() : sameValue(a, b)
}

function fieldErrors(v: Values): Record<string, string> {
  const errors: Record<string, string> = {}
  for (const [key, floor] of Object.entries(FLOORS)) {
    const err = durationError(key, String(v[key] ?? ""), floor)
    if (err) errors[key] = err
  }
  const max = Number(v.global_max)
  if (!Number.isInteger(max) || max < 1) errors.global_max = "global_max must be >= 1"
  const high = Number(v.disk_high_water)
  if (!Number.isInteger(high) || high < 1 || high > 100) errors.disk_high_water = "disk_high_water must be 1..100"
  return errors
}

const isLimit = (key: string): key is keyof RunnerLimitsPatch => key === "memory_max" || key === "cpu_quota"

function toPatch(sent: Values): ConfigPatch {
  const patch: Record<string, unknown> = {}
  const limits: RunnerLimitsPatch = {}
  for (const [key, v] of Object.entries(sent)) {
    const value = typeof v === "string" ? v.trim() : v
    if (isLimit(key)) limits[key] = value as string
    else patch[key] = value
  }
  if (Object.keys(limits).length > 0) patch.runner_limits = limits
  return patch as ConfigPatch
}

// The daemon took the save, so until a read succeeds the cached config shows
// what was sent; the next poll replaces it with what the daemon holds.
function withPatch(c: Config, patch: ConfigPatch): Config {
  const { runner_limits: limits, ...rest } = patch
  return { ...c, ...rest, runner_limits: { ...c.runner_limits, ...limits } } as Config
}

const patchKey = (key: string) => (isLimit(key) ? `runner_limits.${key}` : key)

export interface SettingsForm {
  config: Config | undefined
  values: Values
  changed: string[]
  errors: Record<string, string>
  offline: boolean
  saving: boolean
  rejection: string
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
  const errors = config.data ? fieldErrors(values) : {}
  const offline = status.isError

  const set = (key: string) => (value: unknown) => setDraft((d) => ({ ...d, [key]: value }))

  function discard() {
    setDraft({})
    setRejection("")
  }

  async function save(): Promise<boolean> {
    if (changed.length === 0) return true
    if (saving) return false
    if (Object.keys(errors).length > 0) {
      toast.error("fix the highlighted settings first")
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
        toast.error("settings not saved")
      } else {
        toast.error(`settings not saved: ${errorText(err)}`)
      }
      return false
    }
    toast.success("Settings saved")
    try {
      const fresh = loadedValues(
        await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 }),
      )
      const missed = Object.keys(sent).find((key) => !equal(key, fresh[key], sent[key]))
      if (missed) toast.error(`daemon did not apply ${patchKey(missed)}; is it older than this ghr?`)
    } catch (err) {
      toast.error(`saved, but re-reading the config failed: ${errorText(err)}`)
      queryClient.setQueryData<Config>(keys.config, (c) => c && withPatch(c, toPatch(sent)))
    }
    setDraft((d) => settle(d, sent, equal))
    setSaving(false)
    return true
  }

  return { config: config.data, values, changed, errors, offline, saving, rejection, set, save, discard }
}
```

- [ ] **Step 3: Create the sections component**

Create `web/src/components/settings-form.tsx` (the JSX of today's settings page, reading from the form):

```tsx
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import type { ReactNode } from "react"
import { RejectedAlert } from "@/components/save-bar"
import { TagField } from "@/components/tag-field"
import type { SettingsForm } from "@/lib/settings-form"

const MODES: Record<string, string> = {
  queue: "start runners only for queued jobs, up to the global max",
  all: "keep warm runners per repo, up to each repo's max",
}

function Section({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <Card className="mb-4">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {note && <CardDescription>{note}</CardDescription>}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">{children}</CardContent>
    </Card>
  )
}

function Field({
  label,
  htmlFor,
  desc,
  changed,
  error,
  children,
}: {
  label: string
  htmlFor?: string
  desc: string
  changed?: boolean
  error?: string
  children: ReactNode
}) {
  return (
    <div className="grid gap-1 sm:grid-cols-[12rem_1fr] sm:items-start">
      <Label htmlFor={htmlFor} className="sm:pt-2">
        {label}
      </Label>
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          {children}
          {changed && (
            <span className="text-amber-600" title="changed">
              ●
            </span>
          )}
        </div>
        {error && <p className="text-sm text-destructive">✖ {error}</p>}
        <p className="text-xs text-muted-foreground">{desc}</p>
      </div>
    </div>
  )
}

export function SettingsSections({ form }: { form: SettingsForm }) {
  const { values, changed, errors, offline, set } = form
  const text = (key: string) => String(values[key] ?? "")
  const num = (key: string) => (Number.isNaN(values[key]) ? "" : String(values[key]))
  const toNum = (s: string) => (s === "" ? Number.NaN : Number(s))
  const is = (key: string) => changed.includes(key)

  function duration(key: string, label: string, desc: string) {
    return (
      <Field label={label} htmlFor={key} desc={desc} changed={is(key)} error={errors[key]}>
        <Input id={key} className="w-40" value={text(key)} disabled={offline} onChange={(e) => set(key)(e.target.value)} />
      </Field>
    )
  }

  function plain(key: string, label: string, desc: string) {
    return (
      <Field label={label} htmlFor={key} desc={desc} changed={is(key)}>
        <Input id={key} className="w-40" value={text(key)} disabled={offline} onChange={(e) => set(key)(e.target.value)} />
      </Field>
    )
  }

  return (
    <>
      {form.rejection && <RejectedAlert message={form.rejection} />}
      <Section title="General">
        <Field label="Mode" desc={MODES[text("mode")] ?? ""} changed={is("mode")}>
          <Select value={text("mode")} onValueChange={set("mode")} disabled={offline}>
            <SelectTrigger aria-label="Mode" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {Object.keys(MODES).map((m) => (
                <SelectItem key={m} value={m}>
                  {m}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label="Global max" htmlFor="global_max" desc="applies in queue mode" changed={is("global_max")} error={errors.global_max}>
          <Input
            id="global_max"
            type="number"
            min={1}
            max={99}
            step={1}
            className="w-24"
            value={num("global_max")}
            disabled={offline}
            onChange={(e) => set("global_max")(toNum(e.target.value))}
          />
        </Field>
        <Field label="Owner" desc="change in config.yaml and restart the daemon">
          <span className="font-mono text-sm">{form.config?.owner}</span>
        </Field>
      </Section>
      <Section title="Timing">
        {duration("poll_interval", "Poll interval", "how often GitHub is checked (at least 5s)")}
        {duration("start_timeout", "Start timeout", "a runner not online by then is replaced")}
        {duration("idle_timeout", "Idle timeout", "idle runners beyond warm stop after this")}
      </Section>
      <Section title="Disk and retention">
        <Field
          label="Disk high-water"
          htmlFor="disk_high_water"
          desc="disk use that triggers pruning"
          changed={is("disk_high_water")}
          error={errors.disk_high_water}
        >
          <Input
            id="disk_high_water"
            type="number"
            min={1}
            max={100}
            step={5}
            suffix="%"
            className="w-24"
            value={num("disk_high_water")}
            disabled={offline}
            onChange={(e) => set("disk_high_water")(toNum(e.target.value))}
          />
        </Field>
        {plain("build_cache_keep", "Build cache keep", "build cache kept when pruning, e.g. 20GB")}
        {duration("history_retention", "History retention", "history and logs older than this are removed (at least 1d)")}
      </Section>
      <Section title="Runner defaults" note="limits apply to newly started runners">
        <Field label="Global labels" desc="added to every runner" changed={is("labels")}>
          <TagField label="Global labels" value={values.labels as string[]} onChange={set("labels")} disabled={offline} />
        </Field>
        {plain("memory_max", "Memory max", "per runner, e.g. 6G, 50% or infinity")}
        {plain("cpu_quota", "CPU quota", "per runner, e.g. 200%")}
      </Section>
    </>
  )
}
```

- [ ] **Step 4: Slim the page**

Replace the whole of `web/src/pages/settings.tsx` with:

```tsx
import { PageHeader } from "darkraise-ui/layout"
import { AccountCard } from "@/components/account-card"
import { MaintenanceCard } from "@/components/maintenance-card"
import { SaveBar } from "@/components/save-bar"
import { SettingsSections } from "@/components/settings-form"
import { TokenCard } from "@/components/token-card"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { useSettingsForm } from "@/lib/settings-form"

export function SettingsPage() {
  const form = useSettingsForm()
  return (
    <>
      <PageHeader title="Settings" />
      {!form.config ? (
        <p className="text-sm text-muted-foreground">loading…</p>
      ) : (
        <>
          <SettingsSections form={form} />
          <div className="mb-4 grid gap-4 lg:grid-cols-2">
            <TokenCard />
            <MaintenanceCard />
            <AccountCard />
          </div>
          <SaveBar count={form.changed.length} saving={form.saving} disabled={form.offline} onSave={() => void form.save()} onDiscard={form.discard} />
          <UnsavedGuard count={form.changed.length} page="Settings" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
        </>
      )}
    </>
  )
}
```

- [ ] **Step 5: Run the checks**

Run from `web/`: `timeout 600 npx vitest run src/pages/settings.test.tsx` — Expected: PASS, unchanged test file. Then the web checks — Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/settings-form.ts web/src/components/settings-form.tsx web/src/pages/settings.tsx
git commit -m "refactor(web): share the settings form"
```

---

### Task 10: Setup wizard page

**Files:**
- Create: `web/src/pages/setup.tsx`
- Test: `web/src/pages/setup.test.tsx`
- Modify: `web/src/router.tsx` (session check, new layout and route)

**Interfaces:**
- Consumes: C9, C10
- Produces: C11 (`/setup` route, layout id `setup`, `requireSession`)

**Items:** 1, 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `web/src/pages/setup.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { setupStart } from "@/api/hooks"
import type { SetupState } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const base: SetupState = {
  configured: false,
  starting: false,
  setup_pending: true,
  toolchains_pending: true,
  owner: "",
  web_listen: "0.0.0.0:8080",
}

afterEach(() => {
  setupStart.limitMs = 60_000
})

async function submitGitHub(user: ReturnType<typeof renderApp>["user"], owner: string | null) {
  await screen.findByRole("heading", { name: "Set up ghr" })
  if (owner !== null) await user.type(screen.getByLabelText("GitHub owner"), owner)
  await user.type(screen.getByLabelText("Token"), "tok")
  await user.click(screen.getByRole("button", { name: "Save and start ghr" }))
}

describe("setup wizard", () => {
  it("saves owner and token, waits for ghr to start, then opens Repositories", { timeout: 15_000 }, async () => {
    let state = base
    let polls = 0
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/setup": () => {
          if (state.starting && ++polls >= 2) state = { ...state, starting: false, configured: true }
          return state
        },
        "POST /api/setup/github": () => {
          state = { ...state, starting: true, owner: "DarkRaise" }
          return noContent()
        },
      }),
    )
    const { user } = renderApp("/setup")
    await submitGitHub(user, "darkraise")
    expect(await screen.findByText("Starting ghr…")).toBeInTheDocument()
    expect(await screen.findByRole("heading", { name: "Repositories" }, { timeout: 10_000 })).toBeInTheDocument()
    expect(calls.find((c) => c.path === "/api/setup/github")?.body).toEqual({ owner: "darkraise", token: "tok" })
  })

  it("shows GitHub's refusal inline", async () => {
    mockApi(
      authedRoutes({
        "GET /api/setup": base,
        "POST /api/setup/github": () => json({ error: "token rejected by GitHub: github: 401 Bad credentials" }, 400),
      }),
    )
    const { user } = renderApp("/setup")
    await submitGitHub(user, "DarkRaise")
    expect(await screen.findByRole("alert")).toHaveTextContent("token rejected by GitHub: github: 401 Bad credentials")
  })

  it("keeps config.yaml's owner read-only and sends it empty", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/setup": { ...base, owner: "DarkRaise" }, "POST /api/setup/github": () => noContent() }))
    const { user } = renderApp("/setup")
    const owner = await screen.findByLabelText("GitHub owner")
    expect(owner).toHaveValue("DarkRaise")
    expect(owner).toHaveAttribute("readonly")
    await submitGitHub(user, null)
    await waitFor(() => expect(calls.find((c) => c.path === "/api/setup/github")?.body).toEqual({ owner: "", token: "tok" }))
  })

  it("says when ghr takes too long to start", async () => {
    setupStart.limitMs = 50
    let state = base
    mockApi(
      authedRoutes({
        "GET /api/setup": () => state,
        "POST /api/setup/github": () => {
          state = { ...state, starting: true }
          return noContent()
        },
      }),
    )
    const { user } = renderApp("/setup")
    await submitGitHub(user, "DarkRaise")
    expect(await screen.findByText("ghr is still starting; check journalctl -u ghr on the host")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument()
  })

  it("walks repositories, settings and toolchains, and finishes with the popular set", { timeout: 15_000 }, async () => {
    let finished: unknown
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true, owner: "DarkRaise" },
        "GET /api/storage": fixtures.storage,
        "PATCH /api/config": () => noContent(),
        "POST /api/setup/finish": ({ body }: { body: unknown }) => {
          finished = body
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/setup")
    expect(await screen.findByRole("heading", { name: "Repositories", level: 2 })).toBeInTheDocument()
    expect(screen.getByRole("list", { name: "Configured repositories" })).toHaveTextContent("darkmem")
    await user.click(screen.getByRole("button", { name: "Next" }))

    const max = await screen.findByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Next" }))
    const dialog = within(await screen.findByRole("alertdialog"))
    await user.click(dialog.getByRole("button", { name: "Save" }))

    expect(await screen.findByRole("heading", { name: "Toolchains", level: 2 })).toBeInTheDocument()
    expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ global_max: 3 })
    expect(screen.getByRole("switch", { name: "Popular set" })).toBeChecked()
    await user.click(screen.getByRole("button", { name: "Next" }))
    await user.click(await screen.findByRole("button", { name: "Finish" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(finished).toEqual({ toolchains: "popular" })
  })

  it("renders without the Shell and opens the Add repository dialog", async () => {
    mockApi(
      authedRoutes({ "GET /api/setup": { ...base, configured: true }, "GET /api/repos/available": fixtures.availableRepos }),
    )
    const { user } = renderApp("/setup")
    expect(await screen.findByRole("heading", { name: "Repositories", level: 2 })).toBeInTheDocument()
    expect(screen.queryByRole("link", { name: "Dashboard" })).toBeNull()
    await user.click(screen.getByRole("button", { name: "+ Add repository" }))
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
  })

  it("finishes without toolchains when the rest is skipped", async () => {
    let finished: unknown
    mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true },
        "POST /api/setup/finish": ({ body }: { body: unknown }) => {
          finished = body
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Skip the rest" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(finished).toEqual({ toolchains: "none" })
  })

  it("sends a visitor without a session to /login", async () => {
    mockApi({ "GET /auth/state": { setup_required: false, authenticated: false } })
    const { router } = renderApp("/setup")
    expect(await screen.findByRole("button", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })
})
```

- [ ] **Step 2: Run them to verify they fail**

Run from `web/`: `timeout 600 npx vitest run src/pages/setup.test.tsx` — Expected: FAIL (`/setup` is not a route; the page module is missing).

- [ ] **Step 3: Write the page**

Create `web/src/pages/setup.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "darkraise-ui/components/alert-dialog"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
import { useEffect, useState, type FormEvent, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, setupStart, useConfig, useSetupState, useStatus, useStorage } from "@/api/hooks"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { SettingsSections } from "@/components/settings-form"
import { ToolchainsCard } from "@/components/toolchains-card"
import { useSettingsForm } from "@/lib/settings-form"
import { errorText } from "@/query"

const STEPS = ["Password", "GitHub", "Repositories", "Settings", "Toolchains", "Finish"]

interface NavProps {
  onBack?: () => void
  onNext: () => void
  onSkip?: () => void
  nextLabel?: string
  busy?: boolean
}

function Nav({ onBack, onNext, onSkip, nextLabel = "Next", busy }: NavProps) {
  return (
    <div className="mt-6 flex flex-wrap items-center gap-2">
      {onBack && (
        <Button variant="outline" onClick={onBack}>
          Back
        </Button>
      )}
      <Button onClick={onNext} loading={busy}>
        {nextLabel}
      </Button>
      {onSkip && (
        <Button variant="ghost" onClick={onSkip}>
          Skip the rest
        </Button>
      )}
    </div>
  )
}

function GitHubStep({ owner, onSaved }: { owner: string; onSaved: () => void }) {
  const [name, setName] = useState(owner)
  const [token, setToken] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError("")
    setBusy(true)
    try {
      // An owner config.yaml already names goes empty: the daemon keeps it.
      await api.setupGitHub(owner ? "" : name.trim(), token)
      onSaved()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="flex max-w-md flex-col gap-4" onSubmit={(e) => void submit(e)}>
      <div className="flex flex-col gap-2">
        <Label htmlFor="owner">GitHub owner</Label>
        <Input id="owner" value={name} readOnly={owner !== ""} onChange={(e) => setName(e.target.value)} />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="token">Token</Label>
        <Input id="token" type="password" autoComplete="off" value={token} onChange={(e) => setToken(e.target.value)} />
        <p className="text-xs text-muted-foreground">
          A fine-grained PAT with access to the repositories ghr serves: Administration read/write, Actions read.
        </p>
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button type="submit" loading={busy}>
        Save and start ghr
      </Button>
    </form>
  )
}

function Starting({ timedOut, onRetry }: { timedOut: boolean; onRetry: () => void }) {
  if (!timedOut) {
    return (
      <p className="flex items-center gap-2 text-sm">
        <Spinner />
        <span>Starting ghr…</span>
      </p>
    )
  }
  return (
    <div className="flex flex-col items-start gap-3">
      <p role="alert" className="text-sm text-destructive">
        ghr is still starting; check journalctl -u ghr on the host
      </p>
      <Button variant="outline" onClick={onRetry}>
        Retry
      </Button>
    </div>
  )
}

function RepositoriesStep() {
  const config = useConfig()
  const [adding, setAdding] = useState(false)
  const repos = config.data?.repos ?? []
  return (
    <div className="flex flex-col items-start gap-3">
      {repos.length === 0 ? (
        <p className="text-sm text-muted-foreground">No repositories yet. You can also add them later on the Repositories page.</p>
      ) : (
        <ul className="list-inside list-disc text-sm" aria-label="Configured repositories">
          {repos.map((r) => (
            <li key={r.name}>{r.name}</li>
          ))}
        </ul>
      )}
      <Button variant="outline" onClick={() => setAdding(true)}>
        + Add repository
      </Button>
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </div>
  )
}

function SettingsStep({ nav }: { nav: NavProps }) {
  const form = useSettingsForm()
  const [asking, setAsking] = useState(false)
  if (!form.config) return <p className="text-sm text-muted-foreground">loading…</p>

  async function saveAndNext() {
    setAsking(false)
    if (await form.save()) nav.onNext()
  }

  function discardAndNext() {
    form.discard()
    setAsking(false)
    nav.onNext()
  }

  return (
    <>
      <SettingsSections form={form} />
      <Button
        variant="outline"
        disabled={form.changed.length === 0 || form.offline}
        loading={form.saving}
        onClick={() => void form.save()}
      >
        Save settings
      </Button>
      <Nav {...nav} onNext={() => (form.changed.length > 0 ? setAsking(true) : nav.onNext())} />
      <AlertDialog
        open={asking}
        onOpenChange={(open) => {
          if (!open) setAsking(false)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Save your changes?</AlertDialogTitle>
            <AlertDialogDescription>{form.changed.length} setting(s) are not saved yet.</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Stay</AlertDialogCancel>
            <Button variant="outline" onClick={discardAndNext}>
              Discard
            </Button>
            <AlertDialogAction onClick={() => void saveAndNext()}>Save</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function ToolchainsStep({ popular, onPopular }: { popular: boolean; onPopular: (on: boolean) => void }) {
  const status = useStatus()
  const storage = useStorage(status.data)
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        <Switch id="popular" checked={popular} onCheckedChange={onPopular} />
        <Label htmlFor="popular">Popular set</Label>
      </div>
      <p className="text-xs text-muted-foreground">Queues the popular toolchain set when you finish; it installs in the background.</p>
      {storage.data && <ToolchainsCard storage={storage.data} status={status.data} offline={status.isError} />}
    </div>
  )
}

export function SetupPage() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [startedAt, setStartedAt] = useState<number | null>(null)
  const [timedOut, setTimedOut] = useState(false)
  const state = useSetupState(startedAt !== null)
  const [step, setStep] = useState<number | null>(null)
  const [popular, setPopular] = useState<boolean | null>(null)
  const [finishing, setFinishing] = useState(false)
  const [finishError, setFinishError] = useState("")
  const configured = state.data?.configured ?? false

  useEffect(() => {
    if (startedAt === null) return
    const id = setTimeout(() => setTimedOut(true), setupStart.limitMs)
    return () => clearTimeout(id)
  }, [startedAt])

  useEffect(() => {
    if (startedAt === null || !configured) return
    setStartedAt(null)
    setTimedOut(false)
    setStep(2)
    void queryClient.invalidateQueries({ queryKey: keys.status })
    void queryClient.invalidateQueries({ queryKey: keys.config })
  }, [startedAt, configured, queryClient])

  if (!state.data) {
    if (state.isError) {
      return <p className="p-8 text-center text-sm text-destructive">cannot reach ghr: {errorText(state.error)}</p>
    }
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner />
      </div>
    )
  }
  const s = state.data
  const current = step ?? (s.configured ? 2 : 1)
  const usePopular = popular ?? s.toolchains_pending

  async function finish(toolchains: "popular" | "none") {
    setFinishing(true)
    setFinishError("")
    try {
      await api.setupFinish(toolchains)
      await queryClient.invalidateQueries({ queryKey: keys.setup })
      await queryClient.invalidateQueries({ queryKey: keys.status })
      await navigate({ to: "/" })
    } catch (err) {
      setFinishError(errorText(err))
    } finally {
      setFinishing(false)
    }
  }
  const skip = () => void finish("none")
  const go = (n: number) => () => setStep(n)

  let body: ReactNode
  switch (current) {
    case 1:
      body =
        startedAt !== null || s.starting ? (
          <Starting
            timedOut={timedOut}
            onRetry={() => {
              setTimedOut(false)
              setStartedAt(Date.now())
            }}
          />
        ) : (
          <GitHubStep owner={s.owner} onSaved={() => setStartedAt(Date.now())} />
        )
      break
    case 2:
      body = (
        <>
          <RepositoriesStep />
          <Nav onNext={go(3)} onSkip={skip} />
        </>
      )
      break
    case 3:
      body = <SettingsStep nav={{ onBack: go(2), onNext: go(4), onSkip: skip }} />
      break
    case 4:
      body = (
        <>
          <ToolchainsStep popular={usePopular} onPopular={setPopular} />
          <Nav onBack={go(3)} onNext={go(5)} onSkip={skip} />
        </>
      )
      break
    default:
      body = (
        <>
          <p className="text-sm">ghr is set up.{usePopular ? " Finishing queues the popular toolchain set." : ""}</p>
          <Nav onBack={go(4)} onNext={() => void finish(usePopular ? "popular" : "none")} nextLabel="Finish" busy={finishing} />
        </>
      )
  }

  return (
    <div className="mx-auto max-w-4xl p-4 sm:p-8">
      <h1 className="mb-1 text-2xl font-semibold">Set up ghr</h1>
      <p className="mb-6 text-sm text-muted-foreground">Each step saves as you go; this page stays open to you until you finish.</p>
      <ol className="mb-6 flex flex-wrap gap-x-4 gap-y-1 text-sm" aria-label="Setup steps">
        {STEPS.map((name, i) => {
          const done = i === 0 || (i === 1 && s.configured) || i < current
          return (
            <li
              key={name}
              aria-current={i === current ? "step" : undefined}
              className={i === current ? "font-semibold" : "text-muted-foreground"}
            >
              {done ? "✓" : `${i + 1}.`} {name}
            </li>
          )
        })}
      </ol>
      <section aria-labelledby="setup-step">
        <h2 id="setup-step" className="mb-4 text-lg font-medium">
          {STEPS[current]}
        </h2>
        {body}
        {finishError && (
          <p role="alert" className="mt-3 text-sm text-destructive">
            {finishError}
          </p>
        )}
      </section>
    </div>
  )
}
```

- [ ] **Step 4: Add the route**

In `web/src/router.tsx`:

- Import `SetupPage` from `./pages/setup`.
- Above `const rootRoute`, after `authState`, add:

```ts
async function requireSession(queryClient: QueryClient, href: string) {
  if (!(await authState(queryClient)).authenticated) throw redirect({ to: "/login", search: loginSearch(href) })
}
```

- Change `appRoute`'s `beforeLoad` to `beforeLoad: ({ context, location }) => requireSession(context.queryClient, location.href),`.
- After `appRoute`, add:

```ts
// No Shell: its config, metrics and event queries would only meet the setup
// phase's 503.
const setupLayout = createRoute({
  getParentRoute: () => rootRoute,
  id: "setup",
  beforeLoad: ({ context, location }) => requireSession(context.queryClient, location.href),
})

const setupRoute = createRoute({ getParentRoute: () => setupLayout, path: "/setup", component: SetupPage })
```

- In `routeTree`, add `setupLayout.addChildren([setupRoute]),` after `loginRoute,`.

- [ ] **Step 5: Run the checks**

Run from `web/`: `timeout 600 npx vitest run src/pages/setup.test.tsx` — Expected: PASS. If a test cannot find the role `alertdialog` or the switch named `Popular set`, the darkraise-ui assumption is wrong: fix the page (add `role`/`aria-label` attributes), not the test's intent, and record it. Then the web checks — Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/setup.tsx web/src/pages/setup.test.tsx web/src/router.tsx
git commit -m "feat(web): add the first-run setup wizard"
```

---

### Task 11: Gate the app on setup state

**Files:**
- Modify: `web/src/router.tsx` (`appRoute` and `setupLayout` `beforeLoad`)
- Modify: `web/src/components/shell.tsx` (banner)
- Test: `web/src/router.test.tsx`

**Interfaces:**
- Consumes: C9, C11
- Produces: none

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

- [ ] **Step 1: Write the failing tests**

Add to the `describe("routes", …)` block in `web/src/router.test.tsx` (import `fixtures` from `@/test/fixtures` next to `authedRoutes`):

```tsx
  const setupState = (over: Record<string, unknown>) => ({
    configured: true,
    starting: false,
    setup_pending: false,
    toolchains_pending: false,
    owner: "darkraise",
    web_listen: "0.0.0.0:8080",
    ...over,
  })

  it("sends an unconfigured daemon to /setup", async () => {
    mockApi(authedRoutes({ "GET /api/setup": setupState({ configured: false, owner: "" }) }))
    const router = renderAt("/runners")
    expect(await screen.findByRole("heading", { name: "Set up ghr" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/setup")
  })

  it("sends a finished setup from /setup to the dashboard", async () => {
    mockApi(authedRoutes({ "GET /api/setup": setupState({}) }))
    const router = renderAt("/setup")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it("shows a banner while setup is pending, linking to /setup", async () => {
    mockApi(
      authedRoutes({
        "GET /api/setup": setupState({ setup_pending: true }),
        "GET /api/status": { ...fixtures.status, setup_pending: true },
      }),
    )
    const view = renderApp("/")
    expect(await screen.findByText("First-run setup is not finished.")).toBeInTheDocument()
    await view.user.click(screen.getByRole("link", { name: "Finish setup" }))
    await waitFor(() => expect(view.router.state.location.pathname).toBe("/setup"))
  })

  it("loads the app when the setup state cannot be read", async () => {
    mockApi(authedRoutes({ "GET /api/history": [] }))
    renderAt("/history")
    expect(await screen.findByRole("heading", { name: "History" })).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run them to verify they fail**

Run from `web/`: `timeout 600 npx vitest run src/router.test.tsx` — Expected: FAIL on the first three new tests.

- [ ] **Step 3: Write the implementation**

In `web/src/router.tsx`, import `type SetupState` from `./api/types`, and after `requireSession` add:

```ts
// staleTime 0, like authState. A failed read does not redirect: the page
// loads, and the Shell's status poll or the wizard reports the daemon.
async function setupState(queryClient: QueryClient): Promise<SetupState | undefined> {
  try {
    return await queryClient.fetchQuery({ queryKey: keys.setup, queryFn: ({ signal }) => api.setupState(signal), staleTime: 0 })
  } catch {
    return undefined
  }
}
```

Replace `appRoute`'s `beforeLoad` with:

```ts
  beforeLoad: async ({ context, location }) => {
    await requireSession(context.queryClient, location.href)
    if ((await setupState(context.queryClient))?.configured === false) throw redirect({ to: "/setup" })
  },
```

and `setupLayout`'s with:

```ts
  beforeLoad: async ({ context, location }) => {
    await requireSession(context.queryClient, location.href)
    const state = await setupState(context.queryClient)
    if (state?.configured && !state.setup_pending) throw redirect({ to: "/" })
  },
```

In `web/src/components/shell.tsx`, add `Link` to the `@tanstack/react-router` import, and insert after the `degraded` alert:

```tsx
      {status.data?.setup_pending && (
        <Alert className="mb-4">
          <AlertTitle>First-run setup is not finished.</AlertTitle>
          <AlertDescription>
            <Link to="/setup" className="underline">
              Finish setup
            </Link>
          </AlertDescription>
        </Alert>
      )}
```

- [ ] **Step 4: Run the checks**

Run the web checks — Expected: PASS (the wizard tests of Task 10 still pass: their states keep `setup_pending: true`).

- [ ] **Step 5: Commit**

```bash
git add web/src/router.tsx web/src/components/shell.tsx web/src/router.test.tsx
git commit -m "feat(web): send unconfigured daemons to setup"
```

After this commit, run the whole Go and web check set once more on the branch, then finish the ghr branch (Global Constraints) with the owner's yes.

---

### Task 12: setup.sh first-run prompts

**Files:**
- Modify: `github-runner/setup.sh` (header 1-8; `install_config` at 146-164; `wait_ready` comment at 188-189; `set_web_password` comment at 208-209; `preinstall_toolchains` at 254-270; `main` at 272-289; new functions)
- Modify: `github-runner/config.example.yaml`
- Test: `github-runner/tests/setup_test.sh`

**Interfaces:**
- Consumes: C8
- Produces: C12

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

- [ ] **Step 1: Write the failing tests**

In `github-runner/tests/setup_test.sh`:

- Replace the whole `# (d) Token prompt at end of input…` case (from its comment line through `check "d: no token file written" absent "$ETC_DIR/token"`) with:

```bash
# (d) install_config never asks for the token; a first install marks setup pending.
new_case d-no-token-prompt
ETC_DIR="$T/d-no-token-prompt/etc"
mkdir -p "$ETC_DIR"
out=$(GHR_TOKEN='' run install_config </dev/null 2>&1)
rc=$?
check "d: install_config succeeds without a token" [ "$rc" -eq 0 ]
check "d: no token prompt" lacks "$out" "PAT"
check "d: no token file written" absent "$ETC_DIR/token"
check "d: a first install marks setup pending" [ -f "$STATE_DIR/setup-pending" ]
rm -f "$STATE_DIR/setup-pending"
run install_config >/dev/null 2>&1
check "d: an upgrade does not mark setup pending" absent "$STATE_DIR/setup-pending"
```

- In case `g-first-install`, change both `(GHR_TOKEN=tok run install_config)` to `(run install_config)`.
- In case `h-web-password`, change both `(GHR_TOKEN=tok; set -e; install_config` to `(set -e; install_config`.
- Insert before the line `# (c) Dist GC.`:

```bash
# setup_ghr answers `ghr setup` from the mark files configured, wizard, owner
# and web, and records `ghr setup github` and `ghr toolchain` calls.
setup_ghr() {
  ghr() {
    case "$1 ${2:-}" in
      "setup ")
        printf 'configured: %s\nwizard: %s\nowner: %s\nweb: %s\n' "$(cat "$MARK_DIR/configured")" \
          "$(cat "$MARK_DIR/wizard")" "$(cat "$MARK_DIR/owner")" "$(cat "$MARK_DIR/web")"
        ;;
      "setup github")
        echo "$*" >> "$MARK_DIR/ghr-calls"
        cat > "$MARK_DIR/ghr-stdin"
        [ ! -f "$MARK_DIR/fail-github" ] || { echo "ghr: token rejected by GitHub: github: 401 Bad credentials" >&2; return 1; }
        echo "GitHub owner and token set; ghr is running"
        ;;
      "toolchain install")
        echo "$*" >> "$MARK_DIR/ghr-calls"
        echo "queued — follow with: ghr storage"
        ;;
      *) return 1 ;;
    esac
  }
}
setup_marks() {
  echo "$1" > "$MARK_DIR/configured"
  echo "$2" > "$MARK_DIR/wizard"
  echo "$3" > "$MARK_DIR/owner"
  echo "$4" > "$MARK_DIR/web"
  rm -f "$MARK_DIR/ghr-calls" "$MARK_DIR/ghr-stdin"
}

# (i) configure_github asks for owner and token while ghr is not configured.
new_case i-configure
setup_marks no pending - 0.0.0.0:8080
out=$( (setup_ghr; GHR_OWNER=DarkRaise; GHR_TOKEN=tok; run configure_github) </dev/null 2>&1)
check "i: GHR_OWNER and GHR_TOKEN configure ghr" [ "$(cat "$MARK_DIR/ghr-calls")" = "setup github --owner DarkRaise" ]
check "i: the token is piped" [ "$(cat "$MARK_DIR/ghr-stdin")" = tok ]
check "i: without a newline" [ "$(wc -c < "$MARK_DIR/ghr-stdin")" -eq 3 ]
check "i: success is reported" contains "$out" "ghr is running"

setup_marks yes done DarkRaise 0.0.0.0:8080
out=$( (setup_ghr; GHR_OWNER=DarkRaise; GHR_TOKEN=tok; run configure_github) </dev/null 2>&1)
check "i: a configured ghr is left alone" absent "$MARK_DIR/ghr-calls"
check "i: and nothing is printed" [ -z "$out" ]

setup_marks no pending DarkRaise 0.0.0.0:8080
(setup_ghr; GHR_OWNER=; GHR_TOKEN=tok; run configure_github) </dev/null >/dev/null 2>&1
check "i: config.yaml's owner is used" [ "$(cat "$MARK_DIR/ghr-calls")" = "setup github --owner DarkRaise" ]

setup_marks no pending - 0.0.0.0:8080
(setup_ghr; GHR_OWNER=; GHR_TOKEN=; run configure_github) </dev/null >/dev/null 2>&1
check "i: without a terminal or env vars nothing is sent" absent "$MARK_DIR/ghr-calls"
out=$( (setup_ghr; GHR_OWNER=; GHR_TOKEN=tok; run configure_github) </dev/null 2>&1)
check "i: GHR_TOKEN without an owner warns" contains "$out" "GHR_TOKEN is ignored without an owner; set GHR_OWNER"
check "i: and sends nothing" absent "$MARK_DIR/ghr-calls"

touch "$MARK_DIR/fail-github"
out=$( (setup_ghr; GHR_OWNER=DarkRaise; GHR_TOKEN=bad; run configure_github) </dev/null 2>&1)
rc=$?
check "i: a refused token does not fail the install" [ "$rc" -eq 0 ]
check "i: the refusal is shown" contains "$out" "token rejected by GitHub"
rm -f "$MARK_DIR/fail-github"

setup_marks no pending - 0.0.0.0:8080
(setup_ghr; stdin_is_tty() { return 0; }; GHR_OWNER=; GHR_TOKEN=; run configure_github) <<<$'DarkRaise\ntok' >/dev/null 2>&1
check "i: the prompts configure ghr" [ "$(cat "$MARK_DIR/ghr-calls")" = "setup github --owner DarkRaise" ]
check "i: with the typed token" [ "$(cat "$MARK_DIR/ghr-stdin")" = tok ]
setup_marks no pending - 0.0.0.0:8080
(setup_ghr; stdin_is_tty() { return 0; }; GHR_OWNER=; GHR_TOKEN=; run configure_github) <<<'' >/dev/null 2>&1
check "i: Enter at the owner prompt skips" absent "$MARK_DIR/ghr-calls"
(setup_ghr; stdin_is_tty() { return 0; }; GHR_OWNER=; GHR_TOKEN=; run configure_github) <<<$'DarkRaise\n' >/dev/null 2>&1
check "i: Enter at the token prompt skips" absent "$MARK_DIR/ghr-calls"

# (j) Toolchains wait for GitHub to be configured.
new_case j-toolchains-unconfigured
setup_marks no pending - 0.0.0.0:8080
touch "$STATE_DIR/toolchains-pending"
out=$( (setup_ghr; GHR_TOOLCHAINS=; run preinstall_toolchains) 2>&1)
check "j: nothing is queued before GitHub is configured" absent "$MARK_DIR/ghr-calls"
check "j: the preset stays pending" [ -f "$STATE_DIR/toolchains-pending" ]
check "j: says where to choose toolchains" contains "$out" "toolchains can be chosen in the setup wizard, or later with: ghr toolchain install --preset popular"
setup_marks yes pending DarkRaise 0.0.0.0:8080
(setup_ghr; GHR_TOOLCHAINS=; run preinstall_toolchains) >/dev/null 2>&1
check "j: once configured the pending preset is queued" [ "$(cat "$MARK_DIR/ghr-calls")" = "toolchain install --preset popular" ]

# (k) print_next says what is left of first-run setup.
new_case k-print-next
hostname() { echo "192.168.0.99 fd00::99"; }
setup_marks no pending - 0.0.0.0:8080
out=$( (setup_ghr; run print_next) 2>&1)
check "k: unconfigured with the web UI points at the browser" contains "$out" "finish setup in the browser: http://192.168.0.99:8080/setup"
setup_marks yes pending DarkRaise 0.0.0.0:8080
out=$( (setup_ghr; run print_next) 2>&1)
check "k: a pending wizard points at the browser" contains "$out" "finish setup in the browser: http://192.168.0.99:8080/setup"
setup_marks no pending - -
out=$( (setup_ghr; run print_next) 2>&1)
check "k: without the web UI it names ghr setup github" contains "$out" "finish setup with: ghr setup github --owner <owner>"
setup_marks yes pending DarkRaise -
out=$( (setup_ghr; run print_next) 2>&1)
check "k: a pending wizard without the web UI names ghr setup finish" contains "$out" "end first-run setup with: ghr setup finish"
setup_marks yes done DarkRaise 0.0.0.0:8080
out=$( (setup_ghr; run print_next) 2>&1)
check "k: a finished setup prints nothing" [ -z "$out" ]
unset -f hostname
```

- [ ] **Step 2: Run them to verify they fail**

Run from `github-runner/`: `timeout 300 bash tests/setup_test.sh` — Expected: FAIL on the `d:`, `i:`, `j:` and `k:` assertions (the old install_config prompts; the new functions do not exist).

- [ ] **Step 3: Write the implementation**

In `github-runner/setup.sh`:

- Replace header lines 4-8 (from `#   bash setup.sh   (asks for the PAT, hidden, on first install)` to the `GHR_WEB_PASSWORD` line) with:

```bash
#   bash setup.sh   (asks for the GitHub owner, the PAT and the web password; Enter skips any)
# Optional: GHR_VERSION=v0.1.1 RUNNER_VERSION=2.337.0
#   GHR_TOOLCHAINS=popular|none (default: popular on a first install, none on an upgrade)
#   GHR_OWNER=... and GHR_TOKEN=... configure GitHub for scripts that cannot answer the
#     prompts; GHR_TOKEN is ignored without an owner. Whatever is skipped is finished in
#     the web UI at /setup, or with: ghr setup github --owner <owner>
#   GHR_WEB_PASSWORD=... sets the web UI password on a first install (else it asks; Enter skips).
```

- Replace `install_config` with:

```bash
install_config() {
  if [ ! -f "$ETC_DIR/config.yaml" ]; then
    install -m 0600 "$SCRIPT_DIR/config.example.yaml" "$ETC_DIR/config.yaml"
    log "wrote $ETC_DIR/config.yaml from config.example.yaml"
    FIRST_INSTALL=1
    # Files, not variables: a first install that dies before preinstall_toolchains
    # or the setup wizard must still find them when re-run.
    touch "$STATE_DIR/toolchains-pending" "$STATE_DIR/setup-pending"
  fi
}
```

- Replace the two comment lines above `wait_ready` with:

```bash
# Polls `ghr status` until the API answers. A configured daemon adopts its runners
# and reconciles with GitHub before it serves, so this can take several seconds.
```

- Replace the two comment lines above `set_web_password` with:

```bash
# A first install turns the web UI on (config.example.yaml); until a password
# is set, the first visitor to the web UI sets it.
```

- Add after `set_web_password`:

```bash
stdin_is_tty() { [ -t 0 ]; }

# Prints one field of `ghr setup` (configured, wizard, owner, web), or nothing
# when the daemon does not answer.
setup_field() {
  timeout 10 ghr setup 2>/dev/null | sed -n "s/^$1: //p" || true
}

# Asks for the GitHub owner and token while ghr is not configured, on every run,
# so an answer skipped or refused earlier can be given later. Enter skips either.
configure_github() {
  [ "$(setup_field configured)" = no ] || return 0
  local owner="${GHR_OWNER:-}" tok="${GHR_TOKEN:-}" out
  if [ -z "$owner" ]; then
    owner=$(setup_field owner)
    [ "$owner" != - ] || owner=
  fi
  if [ -z "$owner" ] && stdin_is_tty; then
    read -rp "GitHub owner (user or org; Enter to finish in the browser): " owner || true
  fi
  if [ -z "$owner" ]; then
    [ -z "$tok" ] || warn "GHR_TOKEN is ignored without an owner; set GHR_OWNER"
    return 0
  fi
  if [ -z "$tok" ] && stdin_is_tty; then
    read -rsp "GitHub fine-grained PAT (Administration: read/write, Actions: read; Enter to finish in the browser): " tok || true
    echo
  fi
  [ -n "$tok" ] || return 0
  if out=$(printf '%s' "$tok" | timeout 90 ghr setup github --owner "$owner" 2>&1); then
    log "$out"
  else
    warn "${out#ghr: }"
  fi
}

# Says what is left of first-run setup.
print_next() {
  local configured wizard web host
  configured=$(setup_field configured)
  [ -n "$configured" ] || return 0
  wizard=$(setup_field wizard)
  web=$(setup_field web)
  [ "$configured" != yes ] || [ "$wizard" = pending ] || return 0
  if [ -n "$web" ] && [ "$web" != - ]; then
    host=$(hostname -I 2>/dev/null | awk '{print $1}')
    log "finish setup in the browser: http://${host:-<this host>}:${web##*:}/setup"
  elif [ "$configured" != yes ]; then
    log "finish setup with: ghr setup github --owner <owner>"
  else
    log "end first-run setup with: ghr setup finish"
  fi
}
```

- In `preinstall_toolchains`, insert between the `if [ "$mode" != popular ]; then … fi` block and `if timeout 30 ghr toolchain install --preset popular; then`:

```bash
  case "$(setup_field configured)" in
    no | starting)
      log "toolchains can be chosen in the setup wizard, or later with: ghr toolchain install --preset popular"
      return 0
      ;;
  esac
```

- In `main`, make the end read:

```bash
  wait_ready
  configure_github
  set_web_password
  gc_dist
  preinstall_toolchains
  print_next
}
```

In `github-runner/config.example.yaml`, replace the `owner: darkraise` line with `owner: ""                   # GitHub user or org; set by setup.sh, ghr setup github, or the web UI`, the `web:` line's comment with `# browser UI; the first visitor sets its password (setup.sh can set it first)`, and the whole `repos:` block (from `repos:` to the end of the file) with `repos: []`.

- [ ] **Step 4: Run the checks**

Run from `github-runner/`: `timeout 300 bash tests/setup_test.sh` — Expected: `all assertions passed`. Then `bash -n setup.sh` and `uvx --from shellcheck-py shellcheck setup.sh tests/setup_test.sh` — Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add github-runner/setup.sh github-runner/config.example.yaml github-runner/tests/setup_test.sh
git commit -m "feat(github-runner): prompt for owner and token"
```

---

### Task 13: Docs and superseded records

**Files:**
- Modify: `github-runner/README.md` (Install 31-33, Use block 45-58, Web UI 83-85)
- Modify: `docs/superpowers/specs/2026-10-07-ghr-tui-retirement-design.md` (Status line 4, Goal line 11, Decisions bullet at 20, subsection at 71-82)
- Modify: `docs/superpowers/plans/2026-10-07-ghr-web-setup-loopback.md` (Task 1 and Task 4 headings)
- Modify: `docs/superpowers/registers/2026-10-07-ghr-tui-retirement.md` (rows 8, 9, 12)

**Interfaces:**
- Consumes: C8, C11, C12 (names and strings the docs quote)
- Produces: none

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 1 - coupling 0 - risk 0 = 3

- [ ] **Step 1: README**

In `github-runner/README.md`:

- Replace the paragraph `On a first install it asks for the PAT without echoing it, then for the web UI password … in the GHR_TOKEN and GHR_WEB_PASSWORD environment variables instead.` with:

```markdown
On a first install it asks for the GitHub owner (a user or org) and the PAT, the PAT
without echo, then for the web UI password; press Enter at any prompt to skip it.
Finish whatever you skipped in the browser at `http://<lxc>:8080/setup`: the first
visitor sets the web password, then enters the owner and PAT, adds repositories,
adjusts settings and chooses toolchains. Scripts can pass `GHR_OWNER`, `GHR_TOKEN` and
`GHR_WEB_PASSWORD` instead, then run `ghr setup finish`; `GHR_TOKEN` alone no longer
configures ghr.
```

- Append to the next paragraph (`README.md:35-36`, where the sentence wraps across two lines), after `It never overwrites \`/etc/ghr/config.yaml\` or \`/etc/ghr/token\`.`: ` While ghr is not configured, a re-run asks for the owner and PAT again.`
- In the Use block, after the `ghr web set-password` line, add:

```
ghr setup                     # first-run state; ghr setup github --owner <owner> sets owner and PAT
ghr setup finish              # end first-run setup without the browser
```

- Replace the Web UI intro line (`ghr serves a browser UI … (\`GHR_WEB_PASSWORD\` for scripts).`) with:

```markdown
ghr serves a browser UI with every page and action ghr offers. The example config turns it on at `0.0.0.0:8080`. Until a password is set, the first visitor to the port sets it, so set it at install (`GHR_WEB_PASSWORD` for scripts) or open the UI right after installing; the first-run wizard at `/setup` stays reachable until you finish it.
```

- Replace item 1 of the Web UI list with:

```markdown
1. `ghr status` warns while the UI has no password; `ghr web set-password` (12 to 1024 bytes) sets it over SSH. `ghr web reset-password` deletes the password and logs every browser out, after which the next visitor sets a new one, so set one right after running it.
```

- [ ] **Step 2: Superseded spec and plan**

In `docs/superpowers/specs/2026-10-07-ghr-tui-retirement-design.md`:

- Append to the end of the `**Status:**` line: `; the loopback-only first-visitor rule was withdrawn the same day by docs/superpowers/specs/2026-10-07-ghr-first-run-setup-design.md (owner ruling: the first visitor may claim the password)`.
- In the Goal paragraph, replace `the browser setup page that remains as a fallback answers only loopback clients.` with `the browser setup page remains as a fallback.`
- Replace the whole Decisions bullet that begins `- **The first-visitor setup page stays** as a fallback (owner's choice), **but only for loopback clients**` with:

```markdown
- **The first-visitor setup page stays** as a fallback (owner's choice). A loopback-only rule added the same day was withdrawn by docs/superpowers/specs/2026-10-07-ghr-first-run-setup-design.md.
```

- Delete the subsection from the line `### Loopback-only first-visitor setup` up to, not including, the line `## 3. Warning while the web UI has no password`.

In `docs/superpowers/plans/2026-10-07-ghr-web-setup-loopback.md`, add directly under the headings `### Task 1: Loopback-only setup guard` and `### Task 4: Rollout v0.1.15`:

```markdown
**Superseded:** by docs/superpowers/specs/2026-10-07-ghr-first-run-setup-design.md; the guard is reverted and the rollout continues as Task 14 of docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md.
```

- [ ] **Step 3: TUI-retirement register**

Verify row 9's acceptance first: Grep `github-runner/tests/setup_test.sh` for `unset SECONDS; SECONDS=0` (expect a match in case `b-still-starting`) and run `timeout 300 bash github-runner/tests/setup_test.sh` (expect `ok   b: polling is bounded by the timeout`). Then:

```bash
P=D:/Repositories/Personal/darkraise-ai-plugins/plugins/dr-superpowers
R=docs/superpowers/registers/2026-10-07-ghr-tui-retirement.md
bash $P/scripts/register set $R 12 done --note "Owner ruling 2026-10-07: the first web visitor may claim the password; the loopback guard is reverted by docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md"
bash $P/scripts/register set $R 9 done --note "SECONDS frozen in b-still-starting (homelab 2689225); exact 6 polls"
bash $P/scripts/register set $R 8 planned --assigned docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md --acceptance "v0.1.16 released and deployed; the owner's web password restored; ghr web set-password, ghr setup and the install prompts work on the LXC"
bash $P/scripts/register check $R
```

Expected: `register: 0 errors`.

- [ ] **Step 4: Commit**

```bash
git add github-runner/README.md docs/superpowers/specs/2026-10-07-ghr-tui-retirement-design.md docs/superpowers/plans/2026-10-07-ghr-web-setup-loopback.md docs/superpowers/registers/2026-10-07-ghr-tui-retirement.md
git commit -m "docs(github-runner): document first-run setup"
```

Then finish the homelab branch (Global Constraints) with the owner's yes.

---

### Task 14: Rollout v0.1.16

**Files:**
- Modify: `docs/superpowers/registers/2026-10-07-ghr-tui-retirement.md` (ghr master, row 8 state; the records moved from homelab)

**Interfaces:**
- Consumes: C6, C8, C11, C12 (asserted live)
- Produces: none

**Items:** 1, 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 0 - spec 1 - coupling 1 - risk 2 = 4

Every step below that changes something outside this machine waits for the owner's explicit yes. LXC commands run as `ssh -i ~/.ssh/ghr_lxc root@192.168.0.99 '<command>'`. Never print `/etc/ghr/web-password` or a token.

- [ ] **Step 1: Restore the owner's web password**

If `/root/web-password.keep` exists on the LXC (`test -f /root/web-password.keep && echo present`), ask the owner, then run `install -m 0600 /root/web-password.keep /etc/ghr/web-password && systemctl restart ghr`, and check `curl -s http://127.0.0.1:8080/auth/state` on the LXC reports `"setup_required":false`. Keep `/root/web-password.keep` until Step 5 passes.

- [ ] **Step 2: Release**

With the owner's yes, push ghr master. Check that CI passes and releases v0.1.16: `timeout 60 gh run list -R darkraise/ghr -L 1` until the run completes `success`, then `timeout 60 gh release view v0.1.16 -R darkraise/ghr`.

- [ ] **Step 3: Deploy**

Check `ghr status` on the LXC shows no busy runner. With the owner's yes, copy homelab `github-runner/setup.sh` and `github-runner/config.example.yaml` to `/root/github-runner/` on the LXC (`scp -i ~/.ssh/ghr_lxc`), then run `cd /root/github-runner && GHR_VERSION=v0.1.16 bash setup.sh </dev/null`. Expected: exit 0, no owner or token prompt (the install is configured), no `finish setup` line.

- [ ] **Step 4: Live checks on the runner LXC**

- `ghr version` → `v0.1.16`; `ghr setup` → `configured: yes`, `wizard: done`, `owner: darkraise`, `web: 0.0.0.0:8080`; bare `ghr` over `ssh -t` prints status with no warning line.
- From the dev box, `curl -s http://192.168.0.99:8080/auth/state` → `"setup_required":false`; the owner confirms they log in from the browser with their own password and that `/setup` sends them to the Dashboard.

- [ ] **Step 5: set-password sequence** (carried from register row 8)

With the owner's yes, on the LXC: `cp -p /etc/ghr/web-password /root/web-password.check`; pipe a throwaway of 12+ bytes to `ghr web set-password` (expect `web password set; every browser was logged out`); from the dev box, `POST http://192.168.0.99:8080/auth/login` with `-H 'X-GHR: 1'` and `{"password":"<throwaway>"}` → 204; then restore with `install -m 0600 /root/web-password.check /etc/ghr/web-password && systemctl restart ghr` and confirm `/auth/state` reports `setup_required:false`. Remove `/root/web-password.check` and, with the owner's yes, `/root/web-password.keep`.

- [ ] **Step 6: Fresh-install check on a throwaway LXC**

Ask the owner for a throwaway Debian 13 LXC (its IP and SSH access). If there is none, record `fresh-install check skipped: no throwaway LXC` in the ledger and go to Step 7. Otherwise, with the owner's yes:

1. Copy `github-runner/` to it and run `bash setup.sh` over `ssh -t`, the owner pressing Enter at every prompt. Expected: the last line is `finish setup in the browser: http://<ip>:8080/setup`.
2. The owner opens that URL, claims a throwaway password, enters the owner and a PAT, waits for `Starting ghr…` to pass, adds no repository (a repository would register runners for it on GitHub), skips settings, turns `Popular set` off, and finishes. Expected: the Dashboard, and `ghr setup` on that LXC reports `configured: yes`, `wizard: done`.
3. With the owner's yes, reset it: `systemctl stop ghr && rm -rf /etc/ghr /var/lib/ghr/setup-pending /var/lib/ghr/toolchains-pending`, then run `GHR_OWNER=<owner> GHR_TOKEN=<pat> GHR_WEB_PASSWORD=<throwaway> GHR_TOOLCHAINS=none bash setup.sh </dev/null` (the owner types the values). Expected: `GitHub owner and token set; ghr is running`, then `ghr setup` reports `configured: yes`, `wizard: pending`; a `POST /auth/login` with that password → 204; `ghr setup finish` → `first-run setup finished` and `wizard: done`.
4. The owner destroys the throwaway LXC.

- [ ] **Step 7: Record**

On ghr master: `bash $P/scripts/register set docs/superpowers/registers/2026-10-07-ghr-tui-retirement.md 8 done --note "<one line: v0.1.16 deployed; checks passed; fresh-install check done or skipped>"`, then `register check`, and commit with `docs(registers): record the v0.1.16 rollout`.
