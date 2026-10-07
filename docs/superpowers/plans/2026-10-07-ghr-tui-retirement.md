# ghr TUI Retirement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete ghr's terminal UI, keep the CLI, set the web password from SSH (CLI and setup.sh), warn while it is unset, and close the four web UI gaps the TUI covered.

**Architecture:** The daemon gains `Auth.Set` and a socket-only `POST /web/set-password`; the CLI gains `ghr web set-password` and a status warning fed by a new `web_setup_required` status field; `internal/tui` and its libraries go. The React web UI gains status chips in the shell, Dashboard actions, a Runners log preview and Copy URL. homelab's setup.sh prompts for the web password on a first install.

**Tech Stack:** Go 1.26 (`golang.org/x/term`), React 19 + TanStack Query/Router + darkraise-ui, Vitest; bash for setup.sh.

**Spec:** docs/superpowers/specs/2026-10-07-ghr-tui-retirement-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 3 of 15 tasks are heavy (Tasks 1, 2, 6) and are delegated; the rest top out at total 4 (impl-sonnet-medium), raised to high because tasks are delegated.

**Plan review:** 2026-10-07 — dr-superpowers:judge-opus — executability 14 / coherence 17 / coverage 17 / assumptions 15 (round 1)

> codex off — plugin-not-enabled

## Global Constraints

- Code repo: github.com/darkraise/ghr. Tasks 1–14 run in the worktree `D:/Repositories/Personal/ghr-retire-tui` on branch `feat/retire-tui`, created from ghr master `8f93feb`. Paths in those tasks are relative to that worktree.
- Task 15 runs in homelab, worktree `D:/Repositories/Personal/homelab-retire-tui` on branch `feat/retire-tui` from homelab master. Its paths are relative to that worktree.
- Commits: `<type>(<scope>): <subject>`, subject ≤50 chars, imperative, no period; English only. Never `--no-verify`. Never push.
- Go checks: `go build ./... && go vet ./... && go test ./...` (run with `timeout 900`). `go test -race` does not run on this Windows box; CI runs it on Linux. After any go.mod change also run `GOOS=linux GOARCH=amd64 go vet ./...`.
- Web checks, from `web/`: if `web/node_modules` is missing run `npm ci` first; then `npm test`, `npm run lint` (already `--max-warnings 0`), `npm run build`.
- Run `gofmt -l .` before each Go commit; it must print nothing.
- Comments: none unless the why is non-obvious; never reference this task or the TUI.
- Password rules: 12 to 1024 bytes (`webui.MinPasswordLen`, `webui.MaxPasswordLen`); the daemon alone checks the length.
- Copy strings below are exact; tests assert them.
- The executing skill creates both worktrees (dr-superpowers:using-git-worktrees) before Task 1.
- Out of this plan: spec §8 rollout (release v0.1.15, setup.sh on the LXC, live checks). It needs the owner's push and is run after the branch is finished.
- Offline rule (spec §5.2): the Dashboard's mode, global max, repo max, Add, Pause/Resume and Remove controls are disabled while the daemon is unreachable; its runner row actions behave exactly as on the Runners page.

## Contracts

- **C1** `func (a *Auth) Set(password string) error` in `internal/webui/auth.go`. Returns `ErrPasswordLength` for a bad length without touching the file; otherwise writes a new hash, increments `gen`, clears every session.
- **C2** Socket-only route `POST /web/set-password` in `internal/daemon/web.go`. Body JSON `{"password": "<string>"}`, at most 16 KiB encoded. Replies: 204; 400 `{"error":"invalid request body"}`; 400 `{"error":"password must be 12 to 1024 bytes"}`; 500 `{"error":"<message>"}`. Success adds the `info` event `web password set from the command line; every browser was logged out`.
- **C3** `func (c *Client) SetWebPassword(ctx context.Context, password string) error` in `internal/api/client.go`, posting `{"password": ...}` to `/web/set-password`.
- **C4** `model.Status` field `WebSetupRequired bool \`json:"web_setup_required"\``; `daemon.Backend` field `WebSetupRequired func() bool` (nil reports false).
- **C5** `func cli(ctx context.Context, c *api.Client, args []string, stdin io.Reader, out, errOut io.Writer) error` in `cmd/ghr/cli.go`; `run` passes its stderr as `errOut`.
- **C6** CLI strings: success `web password set; every browser was logged out`; mismatch `passwords do not match`; prompts `New web password: ` then `Repeat: ` on stderr; usage error `usage: ghr web <reset-password|set-password>`; status warning `warning: the web UI has no password; the first visitor sets it. Run: ghr web set-password`.
- **C7** Usage lines in `cmd/ghr/main.go`: `  (no command)                    same as status, in a terminal` and `  web set-password                set the web UI password (prompts, or reads stdin)`; no `tui` line.
- **C8** `web/src/lib/status.ts` exports `running(status: Status): number` and `capText(status: Status): string`. `web/src/components/status-chips.tsx` exports `StatusChips()` (no props).
- **C9** `web/src/components/repo-actions.tsx` exports `RepoActionButtons({ repo, offline }: { repo: RepoStatus; offline: boolean })`: buttons named `Pause <name>` / `Resume <name>` and `Remove <name>`; toasts `paused <name>`, `resumed <name>`, `removing <name>`; confirmation `Remove repo <name>? Its running jobs finish first.`
- **C10** `web/src/components/stepper.tsx` exports `Stepper({ label, value, text, min, disabled, title, onChange })`; buttons named `Lower <label>` and `Raise <label>`.
- **C11** `RunnersTable({ status, actions, selected, onSelect }: { status: Status; actions: boolean; selected?: string | null; onSelect?: (id: string) => void })`.
- **C12** `LogView` gains `className?: string`, the height class, default `"h-[60vh]"`.
- **C13** `web/src/lib/clipboard.ts` exports `copyWithToast(text: string, label: string): Promise<void>`; success toast `copied <label>`.
- **C14** TS `Status` gains `web_setup_required?: boolean`.
- **C15** setup.sh: global `FIRST_INSTALL=0`; `install_config` sets `FIRST_INSTALL=1` when it writes the config; function `set_web_password`, run in `main` right after `wait_ready`; env `GHR_WEB_PASSWORD`.

## Assumptions (evidence)

- `golang.org/x/term` v0.46.0 is the latest release: `go list -m -versions golang.org/x/term` (2026-10-07) listed `v0.44.0 v0.45.0 v0.46.0`. It provides `IsTerminal(fd int)` and `ReadPassword(fd int)` on Linux and Windows.
- Only `internal/tui` imports the charmbracelet packages, `lrstanley/bubblezone` and `muesli/termenv`, besides `cmd/ghr/main.go`'s `charmbracelet/x/term` (grep of the ghr tree, 2026-10-07; Fable review #8).
- The Go client methods the TUI used stay: `internal/api/api_test.go`, `internal/api/update_test.go` and `internal/daemon/run_test.go` call them (grep, 2026-10-07: 23, 3 and 3 call sites).
- `cli` has one caller, `cmd/ghr/main.go:88` (grep, 2026-10-07).
- `fakeDaemon` (`cmd/ghr/cli_test.go:23-66`) answers any unknown path with 204 and records method, path and body, and replaces `newClient` without restoring it.
- darkraise-ui `TableRow` spreads its props onto `<tr>` and `Table` wraps `<table>` in a `div` (`node_modules/darkraise-ui/dist/chunk-RY7Z2GFK.js:7-41`, read 2026-10-07 in the ghr-web-polish worktree, same lockfile).
- The global MutationCache shows every failed mutation as an error toast (`web/src/query.ts:13-18`), so new mutations need no `onError`.
- `api.patchConfig(patch: ConfigPatch)` sends the object as the PATCH body (`web/src/api/client.ts:102`); `ConfigPatch` has `mode`, `global_max` and `repos` (`web/src/api/types.ts:314`).
- `login` refuses a session when `gen` changed under it (`internal/webui/auth.go:179`), which makes Task 1's race test deterministic only if `Set` bumps `gen` while holding the derivation slot, as `ChangePassword` does (`auth.go:257-284`).
- `tests/setup_test.sh` sources setup.sh and stubs `ghr`; all its assertions passed on 2026-10-07.
- No `writeSocketError` or `maxPasswordBody` exists in ghr (grep, 2026-10-07).

## Task index

1. Auth.Set
2. Socket route POST /web/set-password
3. Status field web_setup_required
4. Client SetWebPassword
5. Daemon wiring for the no-password warning
6. CLI ghr web set-password
7. Status warning line in the CLI
8. Remove the TUI
9. Status chips on every page
10. Shared repo action buttons
11. Dashboard actions
12. Runners log preview
13. Copy run URL
14. Reword TUI references
15. homelab: web password at install, config and README

---

### Task 1: Auth.Set

**Files:**
- Modify: `internal/webui/auth.go` (add `Set` after `ChangePassword`, before `Reset`)
- Modify: `internal/webui/password.go:35`
- Test: `internal/webui/auth_test.go`

**Interfaces:**
- Produces: C1.

**Items:** 4

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

Add `"io/fs"` to the import block of `internal/webui/auth_test.go`, then append:

```go
func TestSetReplacesThePassword(t *testing.T) {
	a, _ := newTestAuth(t)
	for _, bad := range []string{strings.Repeat("x", MinPasswordLen-1), strings.Repeat("x", MaxPasswordLen+1)} {
		if err := a.Set(bad); !errors.Is(err, ErrPasswordLength) {
			t.Fatalf("%d bytes: %v", len(bad), err)
		}
	}
	if _, err := os.Stat(a.path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a refused password wrote the file: %v", err)
	}
	if err := a.Set(pw); err != nil {
		t.Fatal(err)
	}
	if req, err := a.SetupRequired(); req || err != nil {
		t.Fatalf("after the first set: required %v err %v", req, err)
	}
	if err := a.Set("short"); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("short over an existing password: %v", err)
	}
	id, err := a.Login("10.0.0.1", pw)
	if err != nil {
		t.Fatal(err)
	}
	const next = "another long password"
	if err := a.Set(next); err != nil {
		t.Fatal(err)
	}
	if a.Valid(id) {
		t.Fatal("a session survived the new password")
	}
	if _, err := a.Login("10.0.0.2", pw); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("old password: %v", err)
	}
	if _, err := a.Login("10.0.0.3", next); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestSetRepairsAnUnreadableFile(t *testing.T) {
	a, _ := newTestAuth(t)
	os.WriteFile(a.path, []byte("garbage\n"), 0o600)
	if !strings.Contains(a.Check().Error(), "ghr web set-password") {
		t.Fatalf("the unreadable-file error should point at set-password: %v", a.Check())
	}
	if err := a.Set(pw); err != nil {
		t.Fatal(err)
	}
	if err := a.Check(); err != nil {
		t.Fatalf("after set: %v", err)
	}
	if _, err := a.Login("10.0.0.1", pw); err != nil {
		t.Fatal(err)
	}
}

// As with a password change, Set holds the derivation slot until the new
// gen is published, so a login queued behind it cannot keep a session.
func TestLoginRacingASetCreatesNoSession(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
	set := make(chan error, 1)
	go func() { set <- a.Set("another long secret") }()
	time.Sleep(50 * time.Millisecond)
	type result struct {
		id  string
		err error
	}
	done := make(chan result, 1)
	go func() {
		id, err := a.Login("10.0.0.1", pw)
		done <- result{id, err}
	}()
	time.Sleep(50 * time.Millisecond)
	<-a.sem
	if err := <-set; err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err == nil || a.Valid(r.id) {
		t.Fatalf("a login that read the old password outlived the set: id valid %v err %v", a.Valid(r.id), r.err)
	}
	<-a.sem
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/webui/ -run 'TestSet|TestLoginRacingASet' -count=1`
Expected: FAIL, `a.Set undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/webui/password.go`, replace line 35:

```go
var ErrUnreadable = errors.New("web password file is unreadable; run ghr web reset-password")
```

with:

```go
var ErrUnreadable = errors.New("web password file is unreadable; run ghr web set-password")
```

In `internal/webui/auth.go`, insert directly above the `// Reset forgets the password` comment:

```go
// Set replaces the password, or sets the first one, and ends every session.
// It never reads the old file, so it also replaces an unreadable one.
func (a *Auth) Set(password string) error {
	if !validLength(password) {
		return ErrPasswordLength
	}
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	var err error
	// The slot is held until the change is published, so a login queued
	// behind it sees the new gen.
	a.derive(func() {
		var h passwordHash
		if h, err = newHash(password, a.rand, a.iter); err != nil {
			return
		}
		if err = writeHash(a.path, h); err != nil {
			return
		}
		a.mu.Lock()
		a.gen++
		clear(a.sessions)
		a.mu.Unlock()
	})
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/webui/ -count=1`
Expected: PASS (all webui tests, including `TestUnreadableFile`, whose prefix check is unchanged).

- [ ] **Step 5: Commit**

```bash
gofmt -l . && git add internal/webui && git commit -m "feat(webui): set the web password directly"
```

### Task 2: Socket route POST /web/set-password

**Files:**
- Modify: `internal/daemon/web.go`
- Test: `internal/daemon/web_test.go`

**Interfaces:**
- Consumes: C1.
- Produces: C2.

**Items:** 4

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing test**

Replace the import block of `internal/daemon/web_test.go` with:

```go
import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/webui"
)
```

Append:

```go
func TestSetPasswordRoute(t *testing.T) {
	b, _, _ := newBackend(t)
	auth := webui.NewAuth(filepath.Join(t.TempDir(), "web-password"), time.Now, rand.Reader)
	id, err := auth.Setup("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	apiHandler := api.NewServer(b)

	web := httptest.NewServer(webui.Handler(auth, apiHandler, fstest.MapFS{}, nil))
	defer web.Close()
	req, _ := http.NewRequest(http.MethodPost, web.URL+"/api/web/set-password", strings.NewReader(`{"password":"over the network"}`))
	req.Header.Set("X-GHR", "1")
	req.AddCookie(&http.Cookie{Name: "ghr_session", Value: id})
	resp, err := web.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !auth.Valid(id) {
		t.Fatalf("over TCP: %d, session valid %v", resp.StatusCode, auth.Valid(id))
	}

	sock := httptest.NewServer(socketHandler(apiHandler, auth, b.Events))
	defer sock.Close()
	post := func(body string) (int, string) {
		t.Helper()
		resp, err := sock.Client().Post(sock.URL+"/web/set-password", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	for _, tc := range []struct{ name, body, want string }{
		{"not JSON", "password", "invalid request body"},
		{"trailing data", `{"password":"another long secret"}x`, "invalid request body"},
		{"over 16 KiB", `{"password":"` + strings.Repeat("a", 16<<10) + `"}`, "invalid request body"},
		{"too short", `{"password":"short"}`, "password must be 12 to 1024 bytes"},
		{"too long", `{"password":"` + strings.Repeat("a", webui.MaxPasswordLen+1) + `"}`, "password must be 12 to 1024 bytes"},
	} {
		code, body := post(tc.body)
		if code != http.StatusBadRequest || !strings.Contains(body, `"error":"`+tc.want+`"`) {
			t.Errorf("%s: %d %s", tc.name, code, body)
		}
	}
	if !auth.Valid(id) {
		t.Fatal("a refused request ended the session")
	}

	long := strings.Repeat("<", webui.MaxPasswordLen)
	escaped, _ := json.Marshal(map[string]string{"password": long})
	if code, body := post(string(escaped)); code != http.StatusNoContent {
		t.Fatalf("1024 bytes that escape to %d: %d %s", len(escaped), code, body)
	}
	if auth.Valid(id) {
		t.Fatal("the session survived the new password")
	}
	if _, err := auth.Login("10.0.0.1", long); err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	evs := b.Events.After(0)
	if len(evs) == 0 || evs[len(evs)-1].Level != "info" ||
		evs[len(evs)-1].Msg != "web password set from the command line; every browser was logged out" {
		t.Fatalf("events %+v", evs)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/daemon/ -run TestSetPasswordRoute -count=1`
Expected: FAIL. The socket requests get 404 or 405 instead of 400 and 204.

- [ ] **Step 3: Write the implementation**

Replace the whole of `internal/daemon/web.go` with:

```go
package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/webui"
)

// maxPasswordBody caps the set-password body. JSON escapes <, > and & as six
// bytes each, so a 1024-byte password can encode to over 6 KiB.
const maxPasswordBody = 16 << 10

// socketHandler serves the Unix socket: the control API plus routes that must
// stay unreachable from the web listener, which mounts the API handler alone.
func socketHandler(api http.Handler, auth *webui.Auth, ev *events.Ring) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /web/reset-password", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.Reset(); err != nil {
			writeSocketError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ev.Add("warn", "", "web password reset; the next visitor to the web UI sets a new one")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /web/set-password", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		// Unmarshal, unlike a Decoder, rejects data after the JSON value.
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPasswordBody))
		if err == nil {
			err = json.Unmarshal(data, &body)
		}
		if err != nil {
			writeSocketError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := auth.Set(body.Password); err != nil {
			code := http.StatusInternalServerError
			if errors.Is(err, webui.ErrPasswordLength) {
				code = http.StatusBadRequest
			}
			writeSocketError(w, code, err.Error())
			return
		}
		ev.Add("info", "", "web password set from the command line; every browser was logged out")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", api)
	return mux
}

func writeSocketError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/daemon/ -run 'TestSetPasswordRoute|TestResetRouteIsSocketOnly' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && git add internal/daemon/web.go internal/daemon/web_test.go && git commit -m "feat(daemon): add the socket set-password route"
```

### Task 3: Status field web_setup_required

**Files:**
- Modify: `internal/model/model.go:6-20` (the `Status` struct)
- Modify: `internal/daemon/backend.go` (the `Backend` struct, lines 67-92, and `Status`, line 114)
- Test: `internal/daemon/backend_test.go`

**Interfaces:**
- Produces: C4.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Append to `internal/daemon/backend_test.go`:

```go
func TestStatusReportsWebSetupRequired(t *testing.T) {
	b, _, _ := newBackend(t)
	if b.Status().WebSetupRequired {
		t.Fatal("without a web listener: want false")
	}
	required := true
	b.WebSetupRequired = func() bool { return required }
	if !b.Status().WebSetupRequired {
		t.Fatal("a listener with no password: want true")
	}
	required = false
	if b.Status().WebSetupRequired {
		t.Fatal("after a password is set: want false")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/daemon/ -run TestStatusReportsWebSetupRequired -count=1`
Expected: FAIL to compile, `b.WebSetupRequired undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/model/model.go`, inside `type Status struct`, after the `RunnerUpdate` field, add:

```go
	// WebSetupRequired is true while the web UI listens with no password set.
	WebSetupRequired bool `json:"web_setup_required"`
```

In `internal/daemon/backend.go`, inside `type Backend struct`, after the `WebApplied config.Web` field, add:

```go
	// WebSetupRequired reports whether the web listener runs with no password
	// set; nil, as without a listener, reports false.
	WebSetupRequired func() bool
```

Replace the one-line method

```go
func (b *Backend) Status() model.Status                { return b.M.Status() }
```

with:

```go
func (b *Backend) Status() model.Status {
	st := b.M.Status()
	if b.WebSetupRequired != nil {
		st.WebSetupRequired = b.WebSetupRequired()
	}
	return st
}
```

Then run `gofmt -w internal/daemon/backend.go internal/model/model.go`, which realigns the neighbouring one-line methods.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/daemon/ ./internal/model/ ./internal/api/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && git add internal/model/model.go internal/daemon/backend.go internal/daemon/backend_test.go && git commit -m "feat(daemon): report a web UI with no password"
```

### Task 4: Client SetWebPassword

**Files:**
- Modify: `internal/api/client.go` (after `ResetWebPassword`, line 276)
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: C2 (the route path and body).
- Produces: C3.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Add `"encoding/json"` and `"io"` to the import block of `internal/api/api_test.go`, then append:

```go
func TestSetWebPasswordPostsTheBody(t *testing.T) {
	var got, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.Path
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Second}}
	if err := c.SetWebPassword(context.Background(), "a <long> secret"); err != nil || got != "POST /web/set-password" {
		t.Fatalf("err %v request %q", err, got)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(body), &sent); err != nil || sent["password"] != "a <long> secret" {
		t.Fatalf("body %q: %v", body, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestSetWebPasswordPostsTheBody -count=1`
Expected: FAIL to compile, `c.SetWebPassword undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/api/client.go`, after `ResetWebPassword`:

```go
// SetWebPassword replaces the web UI password and ends its sessions. The
// route exists on the Unix socket only.
func (c *Client) SetWebPassword(ctx context.Context, password string) error {
	return c.call(ctx, http.MethodPost, "/web/set-password", jsonBody(map[string]string{"password": password}), nil)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/api/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && git add internal/api && git commit -m "feat(api): add the SetWebPassword client call"
```

### Task 5: Daemon wiring for the no-password warning

**Files:**
- Modify: `internal/daemon/run.go` (after the `b := &Backend{...}` literal, about line 233; after the `web UI listening` event, about line 256)
- Test: `internal/daemon/run_test.go` (`TestRunServesTheWebUI`, `TestRunWithoutTheWebListener`)

**Interfaces:**
- Consumes: C1 (indirectly through `auth`), C3, C4.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing assertions**

In `internal/daemon/run_test.go`, in `TestRunServesTheWebUI`, directly after the line

```go
	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })
```

insert:

```go
	setupRequired := func() bool {
		t.Helper()
		st, err := c.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return st.WebSetupRequired
	}
	if !setupRequired() {
		t.Fatal("a listener with no password must report web_setup_required")
	}
	waitFor(t, "the no-password warning", func() bool {
		evs, err := c.Events(context.Background(), 0)
		if err != nil {
			return false
		}
		for _, e := range evs {
			if e.Level == "warn" && strings.HasPrefix(e.Msg, "web UI on ") &&
				strings.HasSuffix(e.Msg, " has no password; run: ghr web set-password") {
				return true
			}
		}
		return false
	})
```

In the same test, directly after the `for _, ck := range resp.Cookies() { ... }` loop that sets `session`, insert:

```go
	if setupRequired() {
		t.Fatal("after setup: web_setup_required must be false")
	}
```

and directly after

```go
	if err := c.ResetWebPassword(context.Background()); err != nil {
		t.Fatal(err)
	}
```

insert:

```go
	if !setupRequired() {
		t.Fatal("after a reset: web_setup_required must be true")
	}
	if err := c.SetWebPassword(context.Background(), "set over the socket"); err != nil {
		t.Fatal(err)
	}
	if setupRequired() {
		t.Fatal("after SetWebPassword: web_setup_required must be false")
	}
```

In `TestRunWithoutTheWebListener`, directly before `if err := c.ResetWebPassword(context.Background()); err != nil {`, insert:

```go
	if st, err := c.Status(context.Background()); err != nil || st.WebSetupRequired {
		t.Fatalf("without a listener: web_setup_required %v err %v", st.WebSetupRequired, err)
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/daemon/ -run 'TestRunServesTheWebUI|TestRunWithoutTheWebListener' -count=1`
Expected: FAIL with `a listener with no password must report web_setup_required`.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/run.go`, directly after the closing `}` of the `b := &Backend{ ... }` literal (before `apiHandler := api.NewServer(b)`), insert:

```go
	if webLn != nil {
		b.WebSetupRequired = func() bool {
			// An unreadable file is reported by the start-up warning instead.
			req, err := auth.SetupRequired()
			return err == nil && req
		}
	}
```

Replace

```go
		ev.Add("info", "", "web UI listening on %s", webLn.Addr())
	}
```

with:

```go
		ev.Add("info", "", "web UI listening on %s", webLn.Addr())
		if b.WebSetupRequired() {
			ev.Add("warn", "", "web UI on %s has no password; run: ghr web set-password", webLn.Addr())
		}
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/daemon/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && git add internal/daemon/run.go internal/daemon/run_test.go && git commit -m "feat(daemon): warn while the web UI has no password"
```

### Task 6: CLI ghr web set-password

**Files:**
- Create: `cmd/ghr/password.go`
- Modify: `cmd/ghr/cli.go:54` (signature) and the `case "web":` block (lines 121-129)
- Modify: `cmd/ghr/main.go:88` (the `cli` call) and the `usage` const
- Modify: `go.mod`, `go.sum`
- Test: `cmd/ghr/cli_test.go`

**Interfaces:**
- Consumes: C3.
- Produces: C5, C6, the `web set-password` line of C7.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

- [ ] **Step 1: Add the dependency**

Run: `go get golang.org/x/term@v0.46.0`
Expected: go.mod gains `golang.org/x/term v0.46.0` (it may also raise `golang.org/x/sys`).

- [ ] **Step 2: Write the failing tests**

In `cmd/ghr/cli_test.go`, replace the whole of `TestWebResetPassword` with:

```go
func TestWebResetPassword(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, errOut := runCLI(t, "", "web", "reset-password")
	if code != 0 || !strings.Contains(out, "web password removed; the next visitor to the web UI sets a new one") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	last := (*reqs)[len(*reqs)-1]
	if last.method != "POST" || last.path != "/web/reset-password" {
		t.Fatalf("request %+v", last)
	}
	for _, args := range [][]string{{"web"}, {"web", "reset"}, {"web", "reset-password", "now"}} {
		code, _, errOut := runCLI(t, "", args...)
		if code != 2 || !strings.Contains(errOut, "usage: ghr web <reset-password|set-password>") {
			t.Errorf("%v: exit %d err %q", args, code, errOut)
		}
	}
	_, out, _ = runCLI(t, "", "help")
	for _, want := range []string{"web reset-password", "web set-password"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage lacks %q:\n%s", want, out)
		}
	}
}

// stubPasswordInput makes stdin look like a terminal, or not, and feeds the
// no-echo reads from answers.
func stubPasswordInput(t *testing.T, terminal bool, answers ...string) {
	t.Helper()
	oldTerm, oldRead := stdinIsTerminal, readPassword
	stdinIsTerminal = func() bool { return terminal }
	readPassword = func() ([]byte, error) {
		if len(answers) == 0 {
			return nil, io.EOF
		}
		a := answers[0]
		answers = answers[1:]
		return []byte(a), nil
	}
	t.Cleanup(func() { stdinIsTerminal, readPassword = oldTerm, oldRead })
}

func TestWebSetPasswordFromAPipe(t *testing.T) {
	reqs := fakeDaemon(t)
	stubPasswordInput(t, false)
	code, out, errOut := runCLI(t, " a long secret \r\n", "web", "set-password")
	if code != 0 || !strings.Contains(out, "web password set; every browser was logged out") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	last := (*reqs)[len(*reqs)-1]
	if last.method != "POST" || last.path != "/web/set-password" || last.body != `{"password":" a long secret "}` {
		t.Fatalf("request %+v", last)
	}
	runCLI(t, "secret one\n\n", "web", "set-password")
	if last := (*reqs)[len(*reqs)-1]; last.body != `{"password":"secret one\n"}` {
		t.Fatalf("only one line ending may be stripped: %+v", last)
	}
}

func TestWebSetPasswordPromptsTwice(t *testing.T) {
	reqs := fakeDaemon(t)
	stubPasswordInput(t, true, "a long secret", "a long secret")
	code, out, errOut := runCLI(t, "", "web", "set-password")
	if code != 0 || !strings.Contains(out, "web password set; every browser was logged out") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if errOut != "New web password: \nRepeat: \n" {
		t.Fatalf("prompts %q", errOut)
	}
	if last := (*reqs)[len(*reqs)-1]; last.path != "/web/set-password" || last.body != `{"password":"a long secret"}` {
		t.Fatalf("request %+v", last)
	}

	n := len(*reqs)
	stubPasswordInput(t, true, "a long secret", "a different one")
	code, _, errOut = runCLI(t, "", "web", "set-password")
	if code != 1 || !strings.Contains(errOut, "ghr: passwords do not match") || len(*reqs) != n {
		t.Fatalf("mismatch: exit %d err %q new requests %d", code, errOut, len(*reqs)-n)
	}
}

func TestSetPasswordPromptStopsWhenCancelled(t *testing.T) {
	stubPasswordInput(t, true, "a long secret", "a long secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var prompts bytes.Buffer
	if _, err := readNewPassword(ctx, strings.NewReader(""), &prompts); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if prompts.String() != "New web password: \n" {
		t.Fatalf("asked again after the cancel: %q", prompts.String())
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./cmd/ghr/ -run 'TestWeb|TestSetPassword' -count=1`
Expected: FAIL to compile, `undefined: stdinIsTerminal`.

- [ ] **Step 4: Write the implementation**

Create `cmd/ghr/password.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// stdinIsTerminal and readPassword are replaced in tests.
var (
	stdinIsTerminal = func() bool { return fdIsTerminal(os.Stdin.Fd()) }
	readPassword    = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }
)

// maxPipedPassword bounds a password read from a pipe; the daemon allows 1024 bytes.
const maxPipedPassword = 4 << 10

// readNewPassword asks twice without echo on a terminal, or reads one
// password from a pipe. The daemon checks the length.
func readNewPassword(ctx context.Context, stdin io.Reader, prompts io.Writer) (string, error) {
	if !stdinIsTerminal() {
		data, err := io.ReadAll(io.LimitReader(stdin, maxPipedPassword))
		if err != nil {
			return "", err
		}
		s := string(data)
		// Only the line ending goes: other whitespace may be part of the password.
		if strings.HasSuffix(s, "\r\n") {
			return s[:len(s)-2], nil
		}
		return strings.TrimSuffix(s, "\n"), nil
	}
	first, err := askPassword(ctx, prompts, "New web password: ")
	if err != nil {
		return "", err
	}
	second, err := askPassword(ctx, prompts, "Repeat: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("passwords do not match")
	}
	return first, nil
}

// askPassword reads one line without echo. Ctrl-C only cancels ctx, which
// the blocked read does not see, so ctx is checked once the read returns.
func askPassword(ctx context.Context, prompts io.Writer, prompt string) (string, error) {
	fmt.Fprint(prompts, prompt)
	b, err := readPassword()
	fmt.Fprintln(prompts)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(b), nil
}
```

In `cmd/ghr/cli.go`, change the signature on line 54 from

```go
func cli(ctx context.Context, c *api.Client, args []string, stdin io.Reader, out io.Writer) error {
```

to:

```go
func cli(ctx context.Context, c *api.Client, args []string, stdin io.Reader, out, errOut io.Writer) error {
```

and replace the whole `case "web":` block

```go
	case "web":
		if len(args) != 2 || args[1] != "reset-password" {
			return usageError("usage: ghr web reset-password")
		}
		if err := c.ResetWebPassword(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, "web password removed; the next visitor to the web UI sets a new one")
		return nil
```

with:

```go
	case "web":
		const webUsage = "usage: ghr web <reset-password|set-password>"
		if len(args) != 2 {
			return usageError(webUsage)
		}
		switch args[1] {
		case "reset-password":
			if err := c.ResetWebPassword(ctx); err != nil {
				return err
			}
			fmt.Fprintln(out, "web password removed; the next visitor to the web UI sets a new one")
			return nil
		case "set-password":
			pw, err := readNewPassword(ctx, stdin, errOut)
			if err != nil {
				return err
			}
			if err := c.SetWebPassword(ctx, pw); err != nil {
				return err
			}
			fmt.Fprintln(out, "web password set; every browser was logged out")
			return nil
		}
		return usageError(webUsage)
```

In `cmd/ghr/main.go`, change

```go
	if err := cli(ctx, newClient(), args, stdin, stdout); err != nil {
```

to:

```go
	if err := cli(ctx, newClient(), args, stdin, stdout, stderr); err != nil {
```

and in the `usage` const, replace the line

```
  web reset-password              forget the web UI password
```

with the two lines:

```
  web reset-password              forget the web UI password
  web set-password                set the web UI password (prompts, or reads stdin)
```

Then run `go mod tidy`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./cmd/ghr/ -count=1 && GOOS=linux GOARCH=amd64 go vet ./...`
Expected: PASS; no vet output.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && git add cmd/ghr go.mod go.sum && git commit -m "feat(cli): add ghr web set-password"
```

### Task 7: Status warning line in the CLI

**Files:**
- Modify: `cmd/ghr/cli.go` (`printStatus`, line 277)
- Test: `cmd/ghr/cli_test.go`

**Interfaces:**
- Consumes: C4, C6 (the warning string).

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing test**

Append to `cmd/ghr/cli_test.go`:

```go
func TestStatusWarnsWhileTheWebUIHasNoPassword(t *testing.T) {
	var out bytes.Buffer
	printStatus(&out, model.Status{Mode: "queue", GlobalMax: 1, WebSetupRequired: true})
	const warning = "warning: the web UI has no password; the first visitor sets it. Run: ghr web set-password\n"
	if !strings.HasPrefix(out.String(), warning) {
		t.Fatalf("output:\n%s", out.String())
	}
	out.Reset()
	printStatus(&out, model.Status{Mode: "queue", GlobalMax: 1})
	if strings.Contains(out.String(), "warning:") {
		t.Fatalf("warned with a password set:\n%s", out.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ghr/ -run TestStatusWarnsWhileTheWebUIHasNoPassword -count=1`
Expected: FAIL, output lacks the warning.

- [ ] **Step 3: Write the implementation**

In `cmd/ghr/cli.go`, make these the first statements of `func printStatus(out io.Writer, st model.Status) {`:

```go
	if st.WebSetupRequired {
		fmt.Fprintln(out, "warning: the web UI has no password; the first visitor sets it. Run: ghr web set-password")
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/ghr/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && git add cmd/ghr/cli.go cmd/ghr/cli_test.go && git commit -m "feat(cli): warn in status while web has no password"
```

### Task 8: Remove the TUI

**Files:**
- Delete: `internal/tui/` (whole directory)
- Modify: `cmd/ghr/main.go` (imports, test hooks, `openTUI`, `run`, `usage`)
- Modify: `cmd/ghr/main_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: C5, C7.
- Produces: C7 (final usage).

**Items:** 1, 2, 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `cmd/ghr/main_test.go`:

1. Replace the import block with:

```go
import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/api"
)
```

2. Delete `stubTUI`, `TestBareGhrOpensTUIOnTerminal` and `TestBareGhrPrintsUsageWithoutTerminal`, and the comment line above `TestStdioIsTerminalNeedsBoth` that ends "would never open the TUI." (replace that two-line comment with `// The production detector needs both stdin and stdout on a terminal.`).

3. Append:

```go
func TestBareGhrRunsStatusOnTerminal(t *testing.T) {
	reqs := fakeDaemon(t)
	stubTerminal(t, true)
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 0 || !strings.Contains(out.String(), "mode queue  global 1/3") {
		t.Fatalf("bare ghr: exit %d out %q err %q", code, out.String(), errb.String())
	}
	if last := (*reqs)[len(*reqs)-1]; last.path != "/status" {
		t.Fatalf("request %+v", last)
	}
}

func TestBareGhrReportsAnUnreachableDaemon(t *testing.T) {
	old := newClient
	t.Cleanup(func() { newClient = old })
	newClient = func() *api.Client { return api.NewUnixClient(filepath.Join(t.TempDir(), "missing.sock")) }
	stubTerminal(t, true)
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), "ghr: ghr daemon unreachable") {
		t.Fatalf("exit %d err %q", code, errb.String())
	}
}

func TestBareGhrPrintsUsageWithoutTerminal(t *testing.T) {
	stubTerminal(t, false)
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 2 || !strings.Contains(errb.String(), "usage: ghr") || out.Len() != 0 {
		t.Fatalf("exit %d out %q err %q", code, out.String(), errb.String())
	}
}

func TestTuiIsAnUnknownCommand(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"tui"}, strings.NewReader(""), &out, &errb); code != 2 || !strings.Contains(errb.String(), "ghr: unknown command tui") {
		t.Fatalf("exit %d err %q", code, errb.String())
	}
	out.Reset()
	run([]string{"help"}, strings.NewReader(""), &out, &errb)
	if strings.Contains(out.String(), "tui") || !strings.Contains(out.String(), "same as status, in a terminal") {
		t.Fatalf("usage:\n%s", out.String())
	}
}
```

- [ ] **Step 2: Do not run the new tests yet**

With the TUI still wired, bare `ghr` under a stubbed terminal and `ghr tui` would start the real bubbletea program inside `go test` and hang. The red state is the deleted `stubTUI`: the package does not compile until Step 3. Go straight to Step 3.

- [ ] **Step 3: Write the implementation**

Delete the TUI: `git rm -r -q internal/tui`

In `cmd/ghr/main.go`:

1. Replace the import block with:

```go
import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/daemon"
)
```

2. Replace

```go
// runTUI, isTerminal and fdIsTerminal are replaced in tests.
var (
	runTUI       = tui.Run
	isTerminal   = stdioIsTerminal
	fdIsTerminal = term.IsTerminal
)
```

with:

```go
// isTerminal and fdIsTerminal are replaced in tests.
var (
	isTerminal   = stdioIsTerminal
	fdIsTerminal = func(fd uintptr) bool { return term.IsTerminal(int(fd)) }
)
```

3. Delete the whole `openTUI` function and its comment.

4. In `run`, replace

```go
	if len(args) == 0 {
		// Scripts and pipes keep the old usage-and-exit-2 behaviour.
		if isTerminal() {
			return openTUI(stderr)
		}
		fmt.Fprint(stderr, usage)
		return 2
	}
```

with:

```go
	if len(args) == 0 {
		// Scripts and pipes keep the old usage-and-exit-2 behaviour.
		if !isTerminal() {
			fmt.Fprint(stderr, usage)
			return 2
		}
		args = []string{"status"}
	}
```

and delete

```go
	case "tui":
		return openTUI(stderr)
```

5. In the `usage` const, replace

```
  (no command)                    open the interactive dashboard
  daemon                          run the supervisor (systemd runs this)
  tui                             same as no command
```

with:

```
  (no command)                    same as status, in a terminal
  daemon                          run the supervisor (systemd runs this)
```

Then run `go mod tidy`.

- [ ] **Step 4: Run the checks**

Run: `go build ./... && go vet ./... && timeout 900 go test ./... -count=1 && GOOS=linux GOARCH=amd64 go vet ./...`
Expected: PASS.

Run: `grep -nE "charmbracelet|bubblezone|termenv" go.mod go.sum`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && git add -A cmd/ghr go.mod go.sum internal/tui && git commit -m "refactor(cli): remove the TUI"
```

### Task 9: Status chips on every page

**Files:**
- Create: `web/src/components/status-chips.tsx`
- Modify: `web/src/lib/status.ts`
- Modify: `web/src/components/shell.tsx`
- Modify: `web/src/pages/dashboard.tsx`
- Modify: `web/src/api/types.ts:70-83` (the `Status` interface)
- Test: `web/src/components/shell.test.tsx`

**Interfaces:**
- Produces: C8, C14.

**Items:** 3, 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Append inside `describe("shell", ...)` in `web/src/components/shell.test.tsx`:

```tsx
  it("shows the status chips on every page", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    expect(await screen.findByText("mode QUEUE")).toBeInTheDocument()
    expect(screen.getByText("runners 2/2")).toBeInTheDocument()
    expect(screen.getByText("api 4980")).toBeInTheDocument()
    expect(screen.getByText("disk 61%")).toBeInTheDocument()
    expect(screen.getByText("runner ↑ 2.338.0")).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/components/shell.test.tsx`
Expected: FAIL, `Unable to find an element with the text: mode QUEUE`.

- [ ] **Step 3: Write the implementation**

In `web/src/api/types.ts`, inside `export interface Status`, after `runner_update: RunnerUpdate`, add:

```ts
  web_setup_required?: boolean
```

In `web/src/lib/status.ts`, change the type import to

```ts
import type { RepoStatus, Status } from "@/api/types"
```

and append:

```ts
export function running(status: Status): number {
  return status.instances.filter((i) => i.state !== "cleaning").length
}

export function capText(status: Status): string {
  return status.mode === "all" ? "∞" : String(status.global_max)
}
```

Create `web/src/components/status-chips.tsx`:

```tsx
import { Badge, type BadgeVariant } from "darkraise-ui/components/badge"
import { useConfig, useStatus } from "@/api/hooks"
import { capText, running } from "@/lib/status"
import { useNow } from "@/lib/use-now"

const DAY = 86_400_000

function diskVariant(pct: number, highWater: number): BadgeVariant {
  if (pct >= 95) return "red"
  return pct >= highWater ? "amber" : "secondary"
}

export function StatusChips() {
  const status = useStatus()
  const config = useConfig()
  const now = useNow()
  const st = status.data
  if (!st) return null
  const highWater = config.data?.disk_high_water ?? 80
  const deadline = st.runner_update.deadline
  return (
    <div className="mb-4 flex flex-wrap gap-2">
      <Badge variant="outline">mode {st.mode.toUpperCase()}</Badge>
      <Badge variant="outline">
        runners {running(st)}/{capText(st)}
      </Badge>
      <Badge variant="outline">api {st.rate_remaining}</Badge>
      <Badge variant={diskVariant(st.disk_pct, highWater)}>disk {st.disk_pct}%</Badge>
      {st.degraded && <Badge variant="red">degraded: {st.degraded_reason}</Badge>}
      {deadline && <Badge variant={Date.parse(deadline) - now <= 7 * DAY ? "red" : "amber"}>runner ↑ {st.runner_update.latest}</Badge>}
    </div>
  )
}
```

In `web/src/components/shell.tsx`, add the import `import { StatusChips } from "@/components/status-chips"` after the `@/api/hooks` import, and replace

```tsx
      <Outlet />
```

with:

```tsx
      <StatusChips />
      <Outlet />
```

In `web/src/pages/dashboard.tsx`:

1. Delete the import line `import { Badge, type BadgeVariant } from "darkraise-ui/components/badge"`.
2. Change `import { keys, useConfig, useEvents, useMetrics, useStatus } from "@/api/hooks"` to `import { keys, useEvents, useMetrics, useStatus } from "@/api/hooks"`.
3. Change `import { repoState } from "@/lib/status"` to `import { capText, repoState, running } from "@/lib/status"`.
4. Delete the `DAY` const and the functions `running`, `capText`, `diskVariant` and `StatusChips`.
5. In `DashboardPage`, delete `const config = useConfig()` and the line `<StatusChips status={st} highWater={config.data?.disk_high_water ?? 80} now={now} />`.

- [ ] **Step 4: Run the checks**

Run (in `web/`): `npm test && npm run lint && npm run build`
Expected: all pass; the Dashboard's existing "shows the status chips" test still passes, now through the shell.

- [ ] **Step 5: Commit**

```bash
git add web/src && git commit -m "feat(web): show the status chips on every page"
```

### Task 10: Shared repo action buttons

**Files:**
- Create: `web/src/components/repo-actions.tsx`
- Modify: `web/src/pages/repositories.tsx` (whole file)

**Interfaces:**
- Produces: C9.

**Items:** 3, 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Confirm the existing tests cover the behaviour**

Run (in `web/`): `npx vitest run src/pages/repositories.test.tsx`
Expected: PASS. These tests ("locks a repo that is being removed", "removes a repo only after confirmation", the pause test) are the regression net for this refactor and must still pass unchanged after Step 3.

- [ ] **Step 2: Create the shared component**

Create `web/src/components/repo-actions.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { RepoStatus } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"

// Shared by the Dashboard and the Repositories page so the two cannot drift.
export function RepoActionButtons({ repo, offline }: { repo: RepoStatus; offline: boolean }) {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.status }),
      queryClient.invalidateQueries({ queryKey: keys.config }),
    ])
  const pause = useMutation({
    mutationFn: (resume: boolean) => (resume ? api.resumeRepo(repo.name) : api.pauseRepo(repo.name)),
    onSuccess: (_data, resume) => {
      toast.success(`${resume ? "resumed" : "paused"} ${repo.name}`)
    },
    onSettled: refresh,
  })
  const remove = useMutation({
    mutationFn: () => api.removeRepo(repo.name),
    onSuccess: () => {
      toast.success(`removing ${repo.name}`)
    },
    onSettled: refresh,
  })
  const locked = offline || Boolean(repo.removing)
  return (
    <>
      <Button
        size="sm"
        variant="secondary"
        aria-label={`${repo.paused ? "Resume" : "Pause"} ${repo.name}`}
        disabled={locked}
        onClick={() => pause.mutate(repo.paused)}
      >
        {repo.paused ? "Resume" : "Pause"}
      </Button>
      <Button
        size="sm"
        variant="destructive"
        aria-label={`Remove ${repo.name}`}
        disabled={locked}
        onClick={() =>
          setConfirm({
            title: `Remove repo ${repo.name}? Its running jobs finish first.`,
            action: "Remove",
            destructive: true,
            run: () => remove.mutate(),
          })
        }
      >
        Remove
      </Button>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}
```

- [ ] **Step 3: Use it on the Repositories page**

Replace the whole of `web/src/pages/repositories.tsx` with:

```tsx
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { EmptyState } from "darkraise-ui/components/empty-state"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { useConfig, useStatus } from "@/api/hooks"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { RepoActionButtons } from "@/components/repo-actions"
import { ActivitySummary, RepoSummary } from "@/components/repo-summary"
import { useNow } from "@/lib/use-now"

export function RepositoriesPage() {
  const status = useStatus()
  const config = useConfig()
  const now = useNow()
  const [adding, setAdding] = useState(false)

  const st = status.data
  if (!st) {
    return (
      <>
        <PageHeader title="Repositories" />
        <Spinner label="waiting for the daemon…" />
      </>
    )
  }
  const offline = status.isError
  const addButton = (
    <Button disabled={offline} onClick={() => setAdding(true)}>
      + Add repository
    </Button>
  )
  return (
    <>
      <PageHeader title="Repositories" actions={addButton} />
      {st.repos.length === 0 ? (
        <EmptyState title="No repositories yet" action={addButton} />
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {st.repos.map((r) => (
            <Card key={r.name}>
              <CardHeader>
                <CardTitle>{r.name}</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-3">
                <RepoSummary repo={r} now={now} />
                <ActivitySummary name={r.name} retention={config.data?.history_retention} now={now} />
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" variant="outline" asChild>
                    <Link to="/repositories/$name" params={{ name: r.name }} aria-label={`Manage ${r.name}`}>
                      Manage
                    </Link>
                  </Button>
                  <RepoActionButtons repo={r} offline={offline} />
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </>
  )
}
```

- [ ] **Step 4: Run the checks**

Run (in `web/`): `npm test && npm run lint && npm run build`
Expected: all pass, `src/pages/repositories.test.tsx` included, with no test edits.

- [ ] **Step 5: Commit**

```bash
git add web/src && git commit -m "refactor(web): share the repo pause and remove buttons"
```

### Task 11: Dashboard actions

**Files:**
- Create: `web/src/components/stepper.tsx`
- Modify: `web/src/pages/dashboard.tsx` (imports, `RepoTable`, `DashboardPage`)
- Test: `web/src/pages/dashboard.test.tsx`

**Interfaces:**
- Consumes: C8, C9.
- Produces: C10.

**Items:** 3, 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/dashboard.test.tsx`, change the expected empty-state text in "shows the empty states" from `"no repos configured — add one on the Repositories page"` to `"no repos configured"`, then append inside `describe("Dashboard page", ...)`:

```tsx
  it("switches the mode and steps the global max", async () => {
    const { calls } = mockApi(authedRoutes({ "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Switch to ALL" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH" && JSON.stringify(c.body) === '{"mode":"all"}')).toBe(true))
    await waitFor(() => expect(screen.getByRole("button", { name: "Raise global max" })).toBeEnabled())
    await user.click(screen.getByRole("button", { name: "Raise global max" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH" && JSON.stringify(c.body) === '{"global_max":3}')).toBe(true))
    expect((await screen.findAllByText("global max 3")).length).toBeGreaterThan(0)
  })

  it("lowers the global max no further than 1", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, global_max: 1 } }))
    renderApp("/")
    expect(await screen.findByRole("button", { name: "Lower global max" })).toBeDisabled()
  })

  it("steps a repo's max, except an unlimited one", async () => {
    const repos = fixtures.status.repos.map((r) => (r.name === "darkcloud" ? { ...r, max: 0 } : r))
    const { calls } = mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos }, "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/")
    expect(await screen.findByRole("button", { name: "Raise max for darkcloud" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Lower max for darkcloud" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Lower max for old-repo" })).toBeDisabled()
    await user.click(screen.getByRole("button", { name: "Raise max for darkmem" }))
    await waitFor(() =>
      expect(calls.some((c) => c.method === "PATCH" && JSON.stringify(c.body) === '{"repos":{"darkmem":{"max":3}}}')).toBe(true),
    )
  })

  it("adds, edits, pauses and removes repos, and acts on runners", async () => {
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/repos/available": fixtures.availableRepos,
        "POST /api/repos/darkmem/pause": () => noContent(),
        "DELETE /api/repos/darkmem": () => noContent(),
      }),
    )
    const { user } = renderApp("/")
    expect(await screen.findByRole("link", { name: "Edit darkmem" })).toHaveAttribute("href", "/repositories/darkmem")
    expect(screen.getByRole("button", { name: "Stop runner aaaaaa" })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Pause darkmem" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/repos/darkmem/pause")).toBe(true))
    await user.click(screen.getByRole("button", { name: "Remove darkmem" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)
    await user.click(ask.getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
    await user.click(screen.getByRole("button", { name: "+ Add repository" }))
    expect(await screen.findByRole("heading", { name: "Add repository" })).toBeInTheDocument()
  })
```

Change the first import line of the file to `import { screen, waitFor, within } from "@testing-library/react"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/dashboard.test.tsx`
Expected: FAIL, `Unable to find role="button" and name "Switch to ALL"`.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/stepper.tsx`:

```tsx
import { Button } from "darkraise-ui/components/button"

export function Stepper({
  label,
  value,
  text,
  min = 1,
  disabled = false,
  title,
  onChange,
}: {
  label: string
  value: number
  text?: string
  min?: number
  disabled?: boolean
  title?: string
  onChange: (next: number) => void
}) {
  return (
    <div className="inline-flex items-center gap-1" title={title}>
      <Button size="sm" variant="outline" aria-label={`Lower ${label}`} disabled={disabled || value <= min} onClick={() => onChange(value - 1)}>
        −
      </Button>
      <span className="min-w-6 text-center tabular-nums">{text ?? value}</span>
      <Button size="sm" variant="outline" aria-label={`Raise ${label}`} disabled={disabled} onClick={() => onChange(value + 1)}>
        +
      </Button>
    </div>
  )
}
```

In `web/src/pages/dashboard.tsx`, replace the whole import block with:

```tsx
import { useMutation, useQueryClient, type UseQueryResult } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Stat, StatLabel, StatValue } from "darkraise-ui/components/stat"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { PageHeader } from "darkraise-ui/layout"
import { useEffect, useRef, useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useEvents, useMetrics, useStatus } from "@/api/hooks"
import type { ConfigPatch, GhrEvent, HistoryEntry, Metrics, RepoStatus, Status } from "@/api/types"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { Glyph } from "@/components/glyph"
import { RepoActionButtons } from "@/components/repo-actions"
import { RunnersTable } from "@/components/runners-table"
import { Sparkline } from "@/components/sparkline"
import { StateBadge } from "@/components/state-badge"
import { Stepper } from "@/components/stepper"
import { ago, clock, fmtMem, maxText, series } from "@/lib/format"
import { capText, repoState, running } from "@/lib/status"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"
```

Replace the whole `RepoTable` function with:

```tsx
function RepoTable({
  repos,
  now,
  offline,
  busy,
  onMax,
}: {
  repos: RepoStatus[]
  now: number
  offline: boolean
  busy: boolean
  onMax: (name: string, max: number) => void
}) {
  if (repos.length === 0) {
    return <p className="py-6 text-center text-sm text-muted-foreground">no repos configured</p>
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Repo</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Run</TableHead>
          <TableHead>Queue</TableHead>
          <TableHead>Last job</TableHead>
          <TableHead className="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {repos.map((r) => (
          <TableRow key={r.name}>
            <TableCell>{r.name}</TableCell>
            <TableCell>
              <StateBadge state={repoState(r)} />
            </TableCell>
            <TableCell>
              {r.active}/{maxText(r.max)}
            </TableCell>
            <TableCell>
              {r.queued > 0 ? (
                <span className="text-amber-600">
                  <Glyph symbol="⧗" label="queued" /> {r.queued}
                </span>
              ) : (
                "–"
              )}
            </TableCell>
            <TableCell>
              {r.last_job ? <LastJob job={r.last_job} now={now} /> : "–"}
              {r.error && <span className="ml-2 text-destructive">{r.error}</span>}
            </TableCell>
            <TableCell>
              <div className="flex flex-wrap justify-end gap-2">
                <Stepper
                  label={`max for ${r.name}`}
                  value={r.max}
                  text={maxText(r.max)}
                  disabled={offline || busy || r.max === 0}
                  title={r.max === 0 ? "unlimited: change it on the repository page" : undefined}
                  onChange={(n) => onMax(r.name, n)}
                />
                <Button size="sm" variant="outline" asChild>
                  <Link to="/repositories/$name" params={{ name: r.name }} aria-label={`Edit ${r.name}`}>
                    Edit
                  </Link>
                </Button>
                <RepoActionButtons repo={r} offline={offline} />
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
```

Replace the whole `DashboardPage` function with:

```tsx
export function DashboardPage() {
  const status = useStatus()
  const metrics = useMetrics()
  const events = useEvents(status.data?.epoch)
  const now = useNow()
  const queryClient = useQueryClient()
  const [adding, setAdding] = useState(false)
  const toggle = useMutation({
    mutationFn: (resume: boolean) => (resume ? api.resumeAll() : api.pauseAll()),
    onSuccess: (_data, resume) => {
      toast.success(resume ? "resumed all repos" : "paused all repos (drain)")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })
  const patch = useMutation({
    mutationFn: ({ body }: { body: ConfigPatch; done: string }) => api.patchConfig(body),
    onSuccess: (_data, { done }) => {
      toast.success(done)
    },
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: keys.status }),
        queryClient.invalidateQueries({ queryKey: keys.config }),
      ]),
  })

  const st = status.data
  if (!st) {
    return (
      <>
        <PageHeader title="Dashboard" />
        <Spinner label="waiting for the daemon…" />
      </>
    )
  }
  const offline = status.isError
  const paused = allPaused(st.repos)
  const otherMode = st.mode === "all" ? "queue" : "all"
  return (
    <>
      <PageHeader
        title="Dashboard"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="outline"
              disabled={offline || patch.isPending}
              onClick={() => patch.mutate({ body: { mode: otherMode }, done: `mode ${otherMode}` })}
            >
              Switch to {otherMode.toUpperCase()}
            </Button>
            <span className="text-sm text-muted-foreground">global max</span>
            <Stepper
              label="global max"
              value={st.global_max}
              disabled={offline || patch.isPending}
              title="applies in queue mode"
              onChange={(n) => patch.mutate({ body: { global_max: n }, done: `global max ${n}` })}
            />
            <Button variant="secondary" disabled={offline || toggle.isPending} onClick={() => toggle.mutate(paused)}>
              {paused ? "Resume all" : "Pause all"}
            </Button>
          </div>
        }
      />
      <StatTiles status={st} metrics={metrics} />
      <div className="mb-4 grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between">
            <CardTitle>Repositories</CardTitle>
            <Button size="sm" disabled={offline} onClick={() => setAdding(true)}>
              + Add repository
            </Button>
          </CardHeader>
          <CardContent>
            <RepoTable
              repos={st.repos}
              now={now}
              offline={offline}
              busy={patch.isPending}
              onMax={(name, max) => patch.mutate({ body: { repos: { [name]: { max } } }, done: `${name} max ${max}` })}
            />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Runners</CardTitle>
          </CardHeader>
          <CardContent>
            <RunnersTable status={st} actions />
          </CardContent>
        </Card>
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Activity</CardTitle>
        </CardHeader>
        <CardContent>
          <ActivityFeed events={events} />
        </CardContent>
      </Card>
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </>
  )
}
```

- [ ] **Step 4: Run the checks**

Run (in `web/`): `npm test && npm run lint && npm run build`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src && git commit -m "feat(web): act on repos and runners from the Dashboard"
```

### Task 12: Runners log preview

**Files:**
- Modify: `web/src/components/log-view.tsx`
- Modify: `web/src/components/runners-table.tsx` (`RunnersTable` only)
- Modify: `web/src/pages/runners.tsx` (whole file)
- Test: `web/src/pages/runners.test.tsx`

**Interfaces:**
- Produces: C11, C12.

**Items:** 3, 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Append inside `describe("Runners page", ...)` in `web/src/pages/runners.test.tsx`:

```tsx
  it("previews the first runner's log", async () => {
    mockApi(authedRoutes({ "GET /api/runners/aaaaaa/log": fixtures.log }))
    renderApp("/runners")
    expect(await screen.findByText("Log preview — aaaaaa (following)")).toBeInTheDocument()
    expect(await screen.findByText(/Running job: test/)).toBeInTheDocument()
    const row = (await screen.findByRole("link", { name: "aaaaaa" })).closest("tr")
    expect(row).toHaveAttribute("aria-selected", "true")
  })

  it("switches the preview to a clicked row, but not on its buttons", async () => {
    mockApi(
      authedRoutes({
        "GET /api/runners/aaaaaa/log": fixtures.log,
        "GET /api/runners/bbbbbb/log": { data: "bbbbbb says hi\n", next: "x" },
      }),
    )
    const { user } = renderApp("/runners")
    await screen.findByText("Log preview — aaaaaa (following)")
    await user.click((await rowOf("bbbbbb")).getByRole("button", { name: "Stop runner bbbbbb" }))
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.getByText("Log preview — aaaaaa (following)")).toBeInTheDocument()
    await user.click((await rowOf("bbbbbb")).getByText("idle"))
    expect(await screen.findByText("Log preview — bbbbbb (following)")).toBeInTheDocument()
    expect(await screen.findByText(/bbbbbb says hi/)).toBeInTheDocument()
  })

  it("moves the selection with the arrow keys", async () => {
    mockApi(authedRoutes({ "GET /api/runners/aaaaaa/log": fixtures.log, "GET /api/runners/bbbbbb/log": fixtures.log }))
    const { user } = renderApp("/runners")
    await screen.findByText("Log preview — aaaaaa (following)")
    screen.getByLabelText("Runners table").focus()
    await user.keyboard("{ArrowDown}")
    expect(await screen.findByText("Log preview — bbbbbb (following)")).toBeInTheDocument()
  })

  it("keeps an ended runner's log and stops polling it", { timeout: 10_000 }, async () => {
    let gone = false
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/status": () => ({
          ...fixtures.status,
          instances: fixtures.status.instances.filter((i) => !(gone && i.id === "aaaaaa")),
        }),
        "GET /api/runners/aaaaaa/log": fixtures.log,
        "GET /api/runners/bbbbbb/log": fixtures.log,
      }),
    )
    renderApp("/runners")
    expect(await screen.findByText(/Running job: test/)).toBeInTheDocument()
    gone = true
    expect(await screen.findByText("Log preview — aaaaaa (ended)", {}, { timeout: 3000 })).toBeInTheDocument()
    expect(screen.getByText(/Running job: test/)).toBeInTheDocument()
    const polls = calls.filter((c) => c.path === "/api/runners/aaaaaa/log").length
    await new Promise((resolve) => setTimeout(resolve, 1500))
    expect(calls.filter((c) => c.path === "/api/runners/aaaaaa/log").length).toBe(polls)
  })

  it("says no runner is selected when none is up", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, instances: [], repos: [] } }))
    renderApp("/runners")
    expect(await screen.findByText("Log preview — no runner selected")).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/runners.test.tsx`
Expected: FAIL, `Unable to find an element with the text: Log preview — aaaaaa (following)`.

- [ ] **Step 3: Write the implementation**

In `web/src/components/log-view.tsx`, change the props to

```tsx
export function LogView({
  text,
  follow,
  onFollowChange,
  className = "h-[60vh]",
}: {
  text: string
  follow: boolean
  onFollowChange?: (follow: boolean) => void
  className?: string
}) {
```

and the `<pre>`'s className to:

```tsx
      className={`${className} overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-3 font-mono text-xs`}
```

In `web/src/components/runners-table.tsx`, change the React import to `import { useState, type KeyboardEvent, type MouseEvent } from "react"`, then replace the start of `RunnersTable` up to and including `<Table>` and the first `<TableBody>` row opening as follows. Replace

```tsx
export function RunnersTable({ status, actions }: { status: Status; actions: boolean }) {
```

with:

```tsx
export function RunnersTable({
  status,
  actions,
  selected,
  onSelect,
}: {
  status: Status
  actions: boolean
  selected?: string | null
  onSelect?: (id: string) => void
}) {
```

Directly after the `async function copy(id: string) { ... }` function, add:

```tsx
  const ids = status.instances.map((i) => i.id)
  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (!onSelect || ids.length === 0 || (e.key !== "ArrowDown" && e.key !== "ArrowUp")) return
    e.preventDefault()
    const at = selected ? ids.indexOf(selected) : -1
    const next = at === -1 ? 0 : e.key === "ArrowDown" ? Math.min(at + 1, ids.length - 1) : Math.max(at - 1, 0)
    const id = ids[next]
    if (id) onSelect(id)
  }
  function onRowClick(e: MouseEvent<HTMLTableRowElement>, id: string) {
    if (!onSelect || (e.target as HTMLElement).closest("a,button")) return
    onSelect(id)
  }
```

Wrap the `<Table> ... </Table>` element (not the `StopRunnerDialog`) in:

```tsx
      <div
        tabIndex={onSelect ? 0 : undefined}
        aria-label={onSelect ? "Runners table" : undefined}
        onKeyDown={onSelect ? onKeyDown : undefined}
      >
        <Table>
          ...unchanged...
        </Table>
      </div>
```

and change the instance row opening tag `<TableRow key={i.id}>` to:

```tsx
            <TableRow
              key={i.id}
              aria-selected={onSelect ? i.id === selected : undefined}
              className={onSelect ? `cursor-pointer ${i.id === selected ? "bg-muted" : ""}` : undefined}
              onClick={(e) => onRowClick(e, i.id)}
            >
```

Replace the whole of `web/src/pages/runners.tsx` with:

```tsx
import { Card, CardContent, CardHeader, CardTitle } from "darkraise-ui/components/card"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { useLogTail, useStatus } from "@/api/hooks"
import { LogView } from "@/components/log-view"
import { RunnersTable } from "@/components/runners-table"

export function RunnersPage() {
  const status = useStatus()
  const [picked, setPicked] = useState<string | null>(null)
  const [follow, setFollow] = useState(true)
  const instances = status.data?.instances ?? []
  const first = instances[0]?.id
  // Until the owner picks one, the first runner is the selection. A picked
  // runner that ends stays picked, so its last log stays readable.
  if (picked === null && first !== undefined) setPicked(first)
  const live = picked !== null && instances.some((i) => i.id === picked)
  const log = useLogTail(picked ?? "", live)
  const title = picked === null ? "Log preview — no runner selected" : `Log preview — ${picked} (${live ? "following" : "ended"})`
  return (
    <>
      <PageHeader title="Runners" />
      <Card>
        <CardContent className="max-h-[50vh] overflow-auto p-4">
          {status.data ? (
            <RunnersTable
              status={status.data}
              actions
              selected={live ? picked : null}
              onSelect={(id) => {
                setPicked(id)
                setFollow(true)
              }}
            />
          ) : (
            <Spinner label="waiting for the daemon…" />
          )}
        </CardContent>
      </Card>
      <Card className="mt-4">
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent>
          {picked !== null && <LogView text={log.data?.text ?? ""} follow={follow} onFollowChange={setFollow} className="h-[17.5rem]" />}
        </CardContent>
      </Card>
    </>
  )
}
```

`selected={live ? picked : null}` leaves no row highlighted for an ended runner, and the down arrow then selects the first row (`at === -1`).

- [ ] **Step 4: Run the checks**

Run (in `web/`): `npm test && npm run lint && npm run build`
Expected: all pass, the existing Runners and runner-detail tests included.

- [ ] **Step 5: Commit**

```bash
git add web/src && git commit -m "feat(web): preview the selected runner's log"
```

### Task 13: Copy run URL

**Files:**
- Modify: `web/src/lib/clipboard.ts`
- Modify: `web/src/components/runners-table.tsx` (the local `copy` function)
- Modify: `web/src/pages/history.tsx` (the last cell of each row)
- Modify: `web/src/pages/runner-detail.tsx` (the header actions)
- Test: `web/src/pages/history.test.tsx`, `web/src/pages/runner-detail.test.tsx`

**Interfaces:**
- Produces: C13.

**Items:** 3, 6

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 2 - spec 0 - coupling 0 - risk 0 = 2

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/history.test.tsx`, change the vitest import to `import { afterEach, describe, expect, it, vi } from "vitest"`, add below the imports:

```tsx
const secureContext = Object.getOwnPropertyDescriptor(window, "isSecureContext")

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  if (secureContext) Object.defineProperty(window, "isSecureContext", secureContext)
  else Reflect.deleteProperty(window, "isSecureContext")
})
```

and append inside `describe("History page", ...)`:

```tsx
  it("copies a run's URL", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    // userEvent.setup() (inside renderApp) installs its own clipboard stub, so the mock goes in after it.
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click(await screen.findByRole("button", { name: "Copy URL of build #41" }))
    expect(writeText).toHaveBeenCalledWith("https://github.com/darkraise/darkmem/actions/runs/101/job/1")
    expect((await screen.findAllByText("copied run URL")).length).toBeGreaterThan(0)
  })
```

In `web/src/pages/runner-detail.test.tsx`, change the vitest import to `import { afterEach, describe, expect, it, vi } from "vitest"`, add to the existing `afterEach` body:

```tsx
  Reflect.deleteProperty(navigator, "clipboard")
  Reflect.deleteProperty(window, "isSecureContext")
```

and append inside `describe("runner detail page", ...)`:

```tsx
  it("copies the run's URL", async () => {
    mockApi(detailRoutes())
    const { user } = renderApp("/runners/aaaaaa")
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click(await screen.findByRole("button", { name: "Copy URL" }))
    expect(writeText).toHaveBeenCalledWith("https://github.com/darkraise/darkmem/actions/runs/102/job/2")
    expect((await screen.findAllByText("copied run URL")).length).toBeGreaterThan(0)
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/history.test.tsx src/pages/runner-detail.test.tsx`
Expected: FAIL, no button named "Copy URL of build #41" / "Copy URL".

- [ ] **Step 3: Write the implementation**

Append to `web/src/lib/clipboard.ts`, and add the two imports at its top:

```ts
import { toast } from "darkraise-ui/components/sonner"
import { errorText } from "@/query"
```

```ts
export async function copyWithToast(text: string, label: string): Promise<void> {
  try {
    await copyText(text)
    toast.success(`copied ${label}`)
  } catch (err) {
    toast.error(errorText(err))
  }
}
```

In `web/src/components/runners-table.tsx`, delete the local `async function copy(id: string) { ... }`, change the Copy ID button's `onClick` to `() => void copyWithToast(i.id, i.id)`, change the clipboard import to `import { copyWithToast } from "@/lib/clipboard"`, and remove the `errorText` import (the deleted `copy` was its only user).

In `web/src/pages/history.tsx`, add the imports `import { Button } from "darkraise-ui/components/button"` and `import { copyWithToast } from "@/lib/clipboard"`, and replace the row's last cell

```tsx
                      <TableCell className="text-right">
                        {h.html_url && (
                          <a href={h.html_url} target="_blank" rel="noreferrer" className="text-sm hover:underline">
                            Open run
                          </a>
                        )}
                      </TableCell>
```

with:

```tsx
                      <TableCell className="text-right">
                        {url && (
                          <div className="flex items-center justify-end gap-2">
                            <a href={url} target="_blank" rel="noreferrer" className="text-sm hover:underline">
                              Open run
                            </a>
                            <Button
                              size="sm"
                              variant="ghost"
                              aria-label={`Copy URL of ${h.job_name} #${h.run_number}`}
                              onClick={() => void copyWithToast(url, "run URL")}
                            >
                              Copy URL
                            </Button>
                          </div>
                        )}
                      </TableCell>
```

and in the same `rows.map((h) => { ... })` callback, next to `const took = ...`, add `const url = h.html_url`.

In `web/src/pages/runner-detail.tsx`, add `import { copyWithToast } from "@/lib/clipboard"`, add `const runUrl = job?.html_url` next to `const job = inst?.job`, and replace

```tsx
            {job?.html_url && (
              <Button variant="outline" asChild>
                <a href={job.html_url} target="_blank" rel="noreferrer">
                  Open run
                </a>
              </Button>
            )}
```

with:

```tsx
            {runUrl && (
              <>
                <Button variant="outline" asChild>
                  <a href={runUrl} target="_blank" rel="noreferrer">
                    Open run
                  </a>
                </Button>
                <Button variant="outline" onClick={() => void copyWithToast(runUrl, "run URL")}>
                  Copy URL
                </Button>
              </>
            )}
```

- [ ] **Step 4: Run the checks**

Run (in `web/`): `npm test && npm run lint && npm run build`
Expected: all pass; the existing "copies a runner ID" test still finds `copied aaaaaa`.

- [ ] **Step 5: Commit**

```bash
git add web/src && git commit -m "feat(web): copy a run's URL"
```

### Task 14: Reword TUI references

**Files:**
- Modify: `internal/daemon/labelcheck.go:34`, `internal/daemon/backend.go` (the `stepsTTL` comment)
- Modify: `web/src/query.ts:11`, `web/src/api/hooks.ts:122`, `web/src/components/shell.tsx:31`, `web/src/components/toolchains-card.tsx:16-17`
- Modify: `web/src/lib/format.test.ts:25`, `web/src/lib/duration.test.ts:28`, `web/src/components/components.test.tsx:18`

**Interfaces:**
- None.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

- [ ] **Step 1: Make the replacements**

Each old text appears once; replace it exactly.

| File | Old | New |
|---|---|---|
| `internal/daemon/labelcheck.go` | `// the token changes; classification against labels happens in the TUI, so a` | `// the token changes; classification against labels happens in the web UI, so a` |
| `internal/daemon/backend.go` | `// however many browser tabs and TUIs poll its detail view.` | `// however many browser tabs poll its detail view.` |
| `web/src/query.ts` | `// Polls do not retry: the next poll is the retry, as in the TUI. A failed` | `// Polls do not retry: the next poll is the retry. A failed` |
| `web/src/api/hooks.ts` | `// Storage page poll every second, as the TUI's storageActive does.` | `// Storage page poll every second.` |
| `web/src/components/shell.tsx` | `// on every page as the TUI's do, and the Dashboard opens with them warm.` | `// on every page, and the Dashboard opens with them warm.` |
| `web/src/components/toolchains-card.tsx` | `// The daemon's popular preset (internal/toolchain/set.go); the TUI lists it` then the next line `// in the same words before queueing it.` | `// The daemon's popular preset (internal/toolchain/set.go), listed in the` then `// same words the confirmation shows before queueing it.` |
| `web/src/lib/format.test.ts` | `it("matches the TUI's buckets", () => {` | `it("buckets elapsed time", () => {` |
| `web/src/lib/duration.test.ts` | `it("names what is wrong, as the TUI does", () => {` | `it("names what is wrong", () => {` |
| `web/src/components/components.test.tsx` | `it("colours states as the TUI does", () => {` | `it("colours each state", () => {` |

- [ ] **Step 2: Check that no reference is left**

Run: `grep -rniw --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git --exclude=main_test.go -e tui . ; grep -rn --exclude-dir=node_modules --exclude-dir=.git "internal/tui" .`
Expected: no output from either. `cmd/ghr/main_test.go` is excluded on purpose: `TestTuiIsAnUnknownCommand` names `tui` to prove it is gone.

- [ ] **Step 3: Run the checks**

Run: `go build ./... && go vet ./... && timeout 900 go test ./internal/daemon/ -count=1`, then in `web/`: `npm test && npm run lint && npm run build`
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
gofmt -l . && git add internal web/src && git commit -m "docs: drop references to the removed TUI"
```

### Task 15: homelab: web password at install, config and README

**Files:**
- Modify: `github-runner/setup.sh` (header, globals, `install_config`, new `set_web_password`, `main`)
- Modify: `github-runner/config.example.yaml:14-16`
- Modify: `github-runner/README.md` (install, Use and Web UI sections)
- Test: `github-runner/tests/setup_test.sh`

**Interfaces:**
- Consumes: C6 (`ghr web set-password` reads stdin; failure prints `ghr: <message>` on stderr).
- Produces: C15.

**Items:** 4, 7

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `github-runner/tests/setup_test.sh`, insert this block directly above the line `# (c) Dist GC.`:

```bash
# (h) A first install sets the web password; an upgrade leaves it alone.
new_case h-web-password
ETC_DIR="$T/h-web-password/etc"
mkdir -p "$ETC_DIR"
first=$( (GHR_TOKEN=tok; set -e; install_config >/dev/null 2>&1; echo "$FIRST_INSTALL") )
check "h: writing config.yaml marks a first install" [ "$first" = 1 ]
again=$( (GHR_TOKEN=tok; set -e; install_config >/dev/null 2>&1; echo "$FIRST_INSTALL") )
check "h: an existing config.yaml is not a first install" [ "$again" = 0 ]

web_ghr() {
  ghr() {
    echo "$*" >> "$MARK_DIR/ghr-calls"
    cat > "$MARK_DIR/ghr-stdin"
    [ ! -f "$MARK_DIR/fail-web" ] || { echo "ghr: password must be 12 to 1024 bytes" >&2; return 1; }
    echo "web password set; every browser was logged out"
  }
}
out=$( (web_ghr; FIRST_INSTALL=0; GHR_WEB_PASSWORD='a long secret'; run set_web_password) </dev/null 2>&1)
check "h: an upgrade does not touch the web password" absent "$MARK_DIR/ghr-calls"
check "h: an upgrade prints nothing" [ -z "$out" ]

out=$( (web_ghr; FIRST_INSTALL=1; GHR_WEB_PASSWORD='a long secret'; run set_web_password) </dev/null 2>&1)
check "h: GHR_WEB_PASSWORD goes to ghr web set-password" [ "$(cat "$MARK_DIR/ghr-calls")" = "web set-password" ]
check "h: the password is piped without a newline" [ "$(wc -c < "$MARK_DIR/ghr-stdin")" -eq 13 ]
check "h: success is reported" contains "$out" "web password set"
check "h: no reminder after success" lacks "$out" "has no password"

rm -f "$MARK_DIR/ghr-calls"
touch "$MARK_DIR/fail-web"
out=$( (web_ghr; FIRST_INSTALL=1; GHR_WEB_PASSWORD='short'; run set_web_password) </dev/null 2>&1)
rc=$?
check "h: a refused password does not fail the install" [ "$rc" -eq 0 ]
check "h: the refusal is shown" contains "$out" "password must be 12 to 1024 bytes"
check "h: the reminder follows a refusal" contains "$out" "set it now with: ghr web set-password"

rm -f "$MARK_DIR/ghr-calls" "$MARK_DIR/fail-web"
out=$( (web_ghr; FIRST_INSTALL=1; GHR_WEB_PASSWORD=; run set_web_password) </dev/null 2>&1)
check "h: with no terminal and no GHR_WEB_PASSWORD nothing is sent" absent "$MARK_DIR/ghr-calls"
check "h: and the reminder is printed" contains "$out" "web UI on :8080 has no password; set it now with: ghr web set-password"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 120 bash github-runner/tests/setup_test.sh | grep -E "^FAIL|assertion"`
Expected: `FAIL h: ...` lines (first `FAIL h: writing config.yaml marks a first install`) and a non-zero count.

- [ ] **Step 3: Write the implementation**

In `github-runner/setup.sh`:

1. After the header line `#   GHR_TOKEN=... supplies the PAT for scripts that cannot answer the prompt.`, add:

```bash
#   GHR_WEB_PASSWORD=... sets the web UI password on a first install (else it asks; Enter skips).
```

2. After the line `SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"`, add:

```bash
FIRST_INSTALL=0
```

3. In `install_config`, directly after the line `log "wrote $ETC_DIR/config.yaml from config.example.yaml — review owner and repos"`, add:

```bash
    FIRST_INSTALL=1
```

4. Directly above the `gc_dist() {` function (and its comment, if it has one), add:

```bash
# A first install turns the web UI on (config.example.yaml); until a password
# is set, its first visitor chooses one.
set_web_password() {
  [ "$FIRST_INSTALL" = 1 ] || return 0
  local pw="${GHR_WEB_PASSWORD:-}" out
  if [ -z "$pw" ] && [ -t 0 ]; then
    read -rsp "Web UI password (12 to 1024 bytes; Enter to skip): " pw || true
    echo
  fi
  if [ -n "$pw" ]; then
    if out=$(printf '%s' "$pw" | ghr web set-password 2>&1); then
      log "$out"
      return 0
    fi
    warn "${out#ghr: }"
  fi
  log "web UI on :8080 has no password; set it now with: ghr web set-password"
}
```

5. In `main`, directly after the line `  wait_ready`, add `  set_web_password`.

In `github-runner/config.example.yaml`, replace

```yaml
# web:                        # browser UI; off until listen is set, then restart ghr
#   listen: 0.0.0.0:8080
#   hosts: [ghr.lan]          # names a proxy serves it under; IPs and localhost always work
```

with:

```yaml
web:                          # browser UI; setup.sh asks for its password, or run: ghr web set-password
  listen: 0.0.0.0:8080
  # hosts: [ghr.lan]          # names a proxy serves it under; IPs and localhost always work
```

In `github-runner/README.md`:

1. Replace the paragraph

```
On a first install it asks for the PAT without echoing it. Scripts that cannot answer the
prompt can pass the token in the `GHR_TOKEN` environment variable instead.
```

with:

```
On a first install it asks for the PAT without echoing it, then for the web UI password
the same way (press Enter to skip it). Scripts that cannot answer the prompts can pass
them in the `GHR_TOKEN` and `GHR_WEB_PASSWORD` environment variables instead.
```

2. In the Use code block, replace the line `ghr tui                       # dashboard (keyboard + mouse)` with `ghr                           # status, when run in a terminal`, and after the line `ghr token set < new-token.txt` add `ghr web set-password          # set the web UI password (prompts)`.

3. Delete the paragraph

```
While the TUI captures the mouse, select terminal text with Shift-drag (Option-drag in
iTerm2). Under tmux, mouse input requires `set -g mouse on`.
```

and its following blank line.

4. Replace `Edits made through the CLI or TUI rewrite `/etc/ghr/config.yaml`, dropping comments.` with `Edits made through the CLI or the web UI rewrite `/etc/ghr/config.yaml`, dropping comments.`

5. In `## Web UI`, replace the intro line and steps 1 and 2:

```
ghr can serve a browser UI with the same pages and actions as `ghr tui`. It stays off until `web.listen` is set.

1. Add a `web:` block to `/etc/ghr/config.yaml` (`config.example.yaml` has one, commented out), set `listen` (for example `0.0.0.0:8080`), and run `systemctl restart ghr`. A reload does not start or move the listener; it only warns `web settings changed; restart ghr to apply`.
2. Open `http://<lxc-ip>:8080` right away and choose the password (12 to 1024 bytes). Until a password is set, the first visitor to reach the port sets it. `ghr web reset-password` deletes the password and logs every browser out, which reopens that window, so log in again right after running it.
```

with:

```
ghr serves a browser UI with every page and action ghr offers. The example config turns it on at `0.0.0.0:8080`, and a first install asks for its password (`GHR_WEB_PASSWORD` for scripts).

1. If the install skipped the password, run `ghr web set-password` (12 to 1024 bytes) before putting the UI behind a proxy; `ghr status` warns until a password is set. Until then, the first visitor to reach the port sets it from the browser. `ghr web reset-password` deletes the password and logs every browser out, which reopens that window, so set a new one right after running it.
2. A config written before the web UI has no `web:` block: add one to `/etc/ghr/config.yaml` (see below), set `listen`, and run `systemctl restart ghr`. A reload does not start or move the listener; it only warns `web settings changed; restart ghr to apply`.
```

6. In step 4 of the same section, replace `and bind `listen` to a LAN-only address.` with `and bind `listen` to a LAN-only address; change it from `0.0.0.0` if the LXC has more than one network.`

- [ ] **Step 4: Run the checks**

Run: `bash -n github-runner/setup.sh && { ! command -v shellcheck >/dev/null || shellcheck github-runner/setup.sh; } && timeout 120 bash github-runner/tests/setup_test.sh | tail -2`
Expected: `all assertions passed`.

Run: `grep -n "tui\|TUI" github-runner/README.md github-runner/setup.sh github-runner/config.example.yaml`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add github-runner && git commit -m "feat(github-runner): set the web password at install"
```
