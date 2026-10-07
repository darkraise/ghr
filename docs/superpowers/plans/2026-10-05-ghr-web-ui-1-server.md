# ghr web UI, part 1: server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the ghr daemon an opt-in TCP listener that serves the embedded web UI and the existing control API behind password login, plus `ghr web reset-password`, a GitHub steps cache and a reload warning for `web.*` changes.

**Architecture:** A new package `internal/webui` holds the password file (PBKDF2), sessions, the login throttle (`Auth`), the request checks and `/auth/*` routes in front of the unchanged `api.NewServer` handler (`Handler`), and the single-page-app file server. A new package `web` embeds `web/dist`. The daemon always builds an `Auth`, wraps the socket handler with a socket-only reset route, and, when `web.listen` is set, binds the TCP listener before touching any runner and serves `webui.Handler` on it. The browser app itself is part 2 (`docs/superpowers/plans/2026-10-05-ghr-web-ui-2-frontend.md`, written after this plan ships); until then the listener serves a 503 "not built" page for app paths.

**Tech Stack:** Go 1.26 (go.mod), standard library `net/http`, `crypto/pbkdf2`, `embed`, `testing/fstest`; gopkg.in/yaml.v3.

**Spec:** docs/superpowers/specs/2026-10-05-ghr-web-ui-design.md

**Execution:** subagent — `claude --model sonnet --effort high` — 6 of 11 tasks are heavy (more than half): the password file, `Auth`, the request handler, the steps cache, the socket route and the daemon wiring all carry security or concurrency risk.

**Plan review:** 2026-10-05 — dr-superpowers:judge-opus — executability 18 / coherence 18 / coverage 18 / assumptions 17 (round 3)

## Global Constraints

- Code repository: `D:/Repositories/Personal/ghr` (github.com/darkraise/ghr), branch `feat/web-server` from master `6803b3e`. Never commit to master. The plan and spec live in `D:/Repositories/Personal/homelab`.
- Every command runs from the ghr repository root in Git Bash with an explicit `timeout`. Unit tests: `timeout 400 go test ./...`. CI also runs `gofmt -l .` (must print nothing; here `timeout 120 gofmt -l .`), `go vet ./...` (`timeout 300 go vet ./...`) and `go test -race -count=1 -timeout 180s ./...`; `-race` cannot run on this Windows box (no cgo), so CI is its only run.
- After editing a Go file, run `timeout 60 gofmt -w <file>`: struct fields gain new alignment when a field is added.
- Edit Go files with the Edit or Write tools. Bash heredocs and inline `python -` lose backslashes.
- Comments: none unless the why is non-obvious; never reference this plan, a task, the spec or a review.
- Commits: `<type>(<scope>): <subject>`, subject ≤ 50 characters, imperative, no period. One commit per task.
- Password rules (spec §1, §2): PBKDF2-HMAC-SHA256, 600000 iterations, 16-byte random salt, 32-byte key; file format `pbkdf2-sha256$<iterations>$<salt base64>$<hash base64>` (unpadded standard base64) plus a newline; file `/etc/ghr/web-password`, mode 0600, written via temp file in the same directory + rename. Passwords are 12 to 1024 bytes.
- Session and throttle numbers (spec §2): session ID 32 random bytes, base64url unpadded; fixed 30-day expiry; cookie `ghr_session`, `Path=/`, `HttpOnly`, `SameSite=Strict`, `Max-Age=2592000`, `Secure` when `r.TLS != nil` or `X-Forwarded-Proto` is `https`. 5 failed logins within any rolling 15-minute window lock that client out for 15 minutes; IPv4 counts per address, IPv6 per /64; at most 2 PBKDF2 derivations run at once.
- TCP server (spec §2): `ReadHeaderTimeout: 10s`, `ReadTimeout: 30s`, `WriteTimeout: 60s`, `IdleTimeout: 120s`, `MaxHeaderBytes: 64 << 10`; `/auth/*` bodies capped at 4 KB (`4 << 10`).
- Response texts, verbatim: 421 `unknown host; add it to web.hosts`; 403 `missing X-GHR header`; 403 `cross-origin request refused`; 401 `not logged in`; 401 `wrong password`; 400 `password must be 12 to 1024 bytes`; 400 `invalid request body`; 409 `a web password is already set`; 409 `no web password is set yet; set one first`; 500 `web password file is unreadable; run ghr web reset-password`; 503 `ghr web UI was not built into this binary`.
- Event and CLI texts, verbatim: reload warning `web settings changed; restart ghr to apply`; event `web password reset; the next visitor to the web UI sets a new one` (level `warn`); event `web UI listening on <addr>` (level `info`); CLI output `web password removed; the next visitor to the web UI sets a new one`.
- Content-Security-Policy, verbatim: `default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'`.

## Contracts

**C1 `internal/config` (Task 1):**
```go
type Config struct {
	// ...existing fields...
	Web Web `yaml:"web,omitempty" json:"web,omitzero"`
}
type Web struct {
	Listen string   `yaml:"listen,omitempty" json:"listen,omitempty"`
	Hosts  []string `yaml:"hosts,omitempty" json:"hosts,omitempty"`
}
```
Validation errors (substrings tests match): `web.listen must be host:port` and `web.hosts entries must be bare hostnames`.

**C2 `internal/webui` password file (Task 2), package-internal:**
```go
const DefaultIterations = 600000
const MinPasswordLen, MaxPasswordLen = 12, 1024
var ErrUnreadable = errors.New("web password file is unreadable; run ghr web reset-password")
type passwordHash struct{ iter int; salt, key []byte }
func newHash(password string, rand io.Reader, iter int) (passwordHash, error)
func (h passwordHash) String() string
func parseHash(s string) (passwordHash, error)          // malformed → errors.Is(err, ErrUnreadable)
func (h passwordHash) matches(password string) bool
func readHash(path string) (h passwordHash, ok bool, err error) // missing file → ok false, err nil
func writeHash(path string, h passwordHash) error
```

**C3 `internal/webui` Auth (Task 3):**
```go
func NewAuth(path string, now func() time.Time, rand io.Reader) *Auth
func (a *Auth) Check() error
func (a *Auth) SetupRequired() (bool, error)
func (a *Auth) Setup(password string) (sessionID string, err error)
func (a *Auth) Login(clientKey, password string) (sessionID string, err error)
func (a *Auth) Valid(sessionID string) bool
func (a *Auth) Logout(sessionID string)
func (a *Auth) ChangePassword(keep, current, next string) error
func (a *Auth) Reset() error
func ClientKey(remoteAddr string) string
var ErrPasswordLength, ErrPasswordSet, ErrSetupRequired, ErrWrongPassword error
type ThrottledError struct{ RetryAt time.Time }
const sessionTTL = 30 * 24 * time.Hour // package-internal; Task 5 reads it
const maxFailures = 5                  // package-internal; Task 5's tests read it
const lockout, failWindow = 15 * time.Minute, 15 * time.Minute
const maxDerivations = 2
```
Package-internal test seams Task 3 creates and Tasks 4 and 5 reuse: fields `a.iter`, `a.path`, `a.sem`, `a.mu`, `a.sessions`, `a.fails`, and in `auth_test.go` the helpers `newTestAuth(t) (*Auth, *clock)` (the clock has `now()` and `add(d)`) and the constant `pw = "correct horse battery"`. Throttle rule: 5 failures inside any rolling 15-minute window lock the client out for 15 minutes; a concurrent burst gets exactly 5 wrong-password answers, the rest `ThrottledError`.

**C4 `internal/webui` static files (Task 4):** `func staticHandler(fsys fs.FS) http.Handler`; in `static_test.go` the helpers `testFS() fstest.MapFS` (holding `index.html` = `<html>app</html>`, `theme-init.js`, `assets/app-1234.js`) and `get(h http.Handler, method, target string) *httptest.ResponseRecorder`.

**C5 `internal/webui` handler (Task 5):** `func Handler(a *Auth, api http.Handler, static fs.FS, hosts []string) http.Handler`; cookie name `ghr_session`.

**C6 `web` package (Task 8):** `package web`, `var Dist embed.FS` (`//go:embed all:dist`); `fs.Sub(web.Dist, "dist")` is the app root.

**C7 `internal/daemon` (Tasks 7, 9, 11):** `Backend.WebApplied config.Web` (Task 7); `func socketHandler(api http.Handler, auth *webui.Auth, ev *events.Ring) http.Handler` serving `POST /web/reset-password` (Task 9); `Options.WebPasswordPath string`, default `/etc/ghr/web-password` (Task 11).

**C8 `internal/api` client (Task 10):** `func (c *Client) ResetWebPassword(ctx context.Context) error` → `POST /web/reset-password`.

## Assumptions (evidence)

- `crypto/pbkdf2.Key[Hash hash.Hash](h func() Hash, password string, salt []byte, iter, keyLength int) ([]byte, error)` is in the standard library: `go doc crypto/pbkdf2.Key` on go1.26.1, 2026-10-05.
- `config.Save` marshals the whole `Config` and `Parse` uses `KnownFields(true)`: `internal/config/config.go:150,176-180`. yaml.v3 `omitempty` treats a struct whose exported fields are all zero (nil slice included) as empty; Task 1's `TestEmptyWebIsOmitted` verifies it.
- `api.decode` does not call `DisallowUnknownFields`, so a `web` key in a `PATCH /config` body is ignored: `internal/api/server.go:225-230`; Task 7's `TestPatchCannotChangeWeb` verifies it end to end. `PATCH /config` answers 204 on success through `respond(w, nil, ...)`: `internal/api/server.go:126-133,252-255`.
- `Backend.Reload` returns the config warnings the CLI and TUI show and turns each into a `config: <warning>` event: `internal/daemon/backend.go:386-406`.
- `Manager.Init` fails when the history file cannot be read (`internal/runner/manager.go:185-188`); a directory at `HistoryPath` makes the read fail — unverified on Windows; Task 11's `TestRunClosesTheWebListenerWhenStartFails` verifies it.
- The fake GitHub client in `internal/daemon/backend_test.go:75-130` records `ListJobs` calls in `jobCalls` under `lmu`, and returns `jobs[runID], jobErr[runID]` when `jobs` is non-nil; `fakeManager.RunnerRepoAndRun("aaaaaa")` answers run 55 (`backend_test.go:63-68`).
- Goroutines blocked sending on a full channel are served in arrival order (the runtime's `sendq` is a FIFO queue), which Task 3's `TestLoginRacingAPasswordChangeCreatesNoSession` relies on to let the queued change derive before the queued login.
- `http.Server.Shutdown` leaves active connections open when its context expires, so Task 11 calls `Close` after a failed `Shutdown` (https://pkg.go.dev/net/http#Server.Shutdown). No test holds a request open past the 5-second deadline: it would need a handler that blocks for longer than the shutdown bound, and the only handlers that wait are PBKDF2 derivations (about half a second each, at most 2 at once).
- Waiting for a derivation slot does not watch the request context: at most 2 derivations of about half a second each run at once, so a parked handler waits seconds, not indefinitely. `TestWriteHashReplacesAtomically` is probabilistic and runs only on CI (Linux).
- The start-failure cleanup is tested through `Init` (an unreadable history file); `Adopt` failures leave `Run` through the same deferred close, so one failure path covers both.
- `http.ServeContent` with a zero modtime sets no `Last-Modified` and never redirects; `http.FileServer`'s `/index.html` redirect and directory listings come from `serveFile`, which this plan does not use (Go net/http documentation).
- Go's `ServeMux` ranks `GET /auth/state` above `/auth/` because its method and path match a subset of the other's requests, so the two do not conflict (Go 1.22 routing enhancements, https://go.dev/blog/routing-enhancements).
- `http.Server.Shutdown` on a server that never served returns at once (Go net/http documentation).
- Tasks 1, 4, 7, 8 and 10 pass the external-executor lane gate, but the owner chose Claude only for this plan (2026-10-05), so no task carries an `**Executor:**` line.

## Task index

1. Config: web block
2. webui: password file
3. webui: Auth sessions and throttle
4. webui: static app files
5. webui: request handler
6. daemon: steps cache
7. daemon: reload warning for web settings
8. web: embed package
9. daemon: socket-only password reset route
10. CLI: ghr web reset-password
11. daemon: web listener wiring

---

### Task 1: Config: web block

**Files:**
- Modify: `internal/config/config.go` (imports; `Config` struct at lines 36-49; `Validate` before `if len(errs) > 0` at line 341)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: C1.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Add `"encoding/json"` to the import block of `internal/config/config_test.go`, then append:

```go
func TestWebBlockRoundTrips(t *testing.T) {
	c, _, err := Parse([]byte(sample + "web:\n  listen: 0.0.0.0:8080\n  hosts: [ghr.lan]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Web.Listen != "0.0.0.0:8080" || len(c.Web.Hosts) != 1 || c.Web.Hosts[0] != "ghr.lan" {
		t.Fatalf("web %+v", c.Web)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Web.Listen != "0.0.0.0:8080" || len(back.Web.Hosts) != 1 || back.Web.Hosts[0] != "ghr.lan" {
		t.Fatalf("round trip lost web: %+v", back.Web)
	}
	clone := c.Clone()
	if clone.Web.Listen != "0.0.0.0:8080" || len(clone.Web.Hosts) != 1 {
		t.Fatalf("clone lost web: %+v", clone.Web)
	}
}

func TestEmptyWebIsOmitted(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	js, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), `"web"`) {
		t.Fatalf("empty web serialised to JSON: %s", js)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "web:") {
		t.Fatalf("empty web saved to YAML:\n%s", data)
	}
}

func TestWebValidation(t *testing.T) {
	bad := []Web{
		{Listen: "0.0.0.0:http"},
		{Listen: "8080"},
		{Listen: "0.0.0.0:0"},
		{Listen: "0.0.0.0:70000"},
		{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan:8080"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{""}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"https://ghr.lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"ghr lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan\t"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"user@ghr.lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"-bad.lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"bad_name.lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{"ghr..lan"}},
		{Listen: "0.0.0.0:8080", Hosts: []string{strings.Repeat("a.", 127) + "lan"}},
	}
	for _, w := range bad {
		c, _, err := Parse([]byte(sample))
		if err != nil {
			t.Fatal(err)
		}
		c.Web = w
		if _, err := c.Validate(); err == nil || !strings.Contains(err.Error(), "web.") {
			t.Errorf("%+v: want a web error, got %v", w, err)
		}
	}
	good := []Web{
		{},
		{Listen: ":8080"},
		{Listen: "[::]:8080"},
		{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan", "GHR.example.com", "ghr-1", "localhost"}},
	}
	for _, w := range good {
		c, _, err := Parse([]byte(sample))
		if err != nil {
			t.Fatal(err)
		}
		c.Web = w
		if _, err := c.Validate(); err != nil {
			t.Errorf("%+v rejected: %v", w, err)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/config -run 'TestWebBlockRoundTrips|TestEmptyWebIsOmitted|TestWebValidation' -v`
Expected: FAIL to compile with `c.Web undefined` and `undefined: Web`.

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, add `"net"` to the import block. In the `var (` block that holds `sizeRe`, add:

```go
	// hostnameRe is RFC 1123: dot-separated labels of letters, digits and
	// inner hyphens, each 1 to 63 characters.
	hostnameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)*$`)
```

In the `Config` struct, add this field directly below `Repos`:

```go
	// Web is read at daemon start only; changing it needs a restart.
	Web Web `yaml:"web,omitempty" json:"web,omitzero"`
```

Directly below the `RunnerLimits` type, add:

```go
// Web is the browser UI's TCP listener. Hosts names the hostnames, besides IP
// literals and localhost, that a request's Host header may carry.
type Web struct {
	Listen string   `yaml:"listen,omitempty" json:"listen,omitempty"`
	Hosts  []string `yaml:"hosts,omitempty" json:"hosts,omitempty"`
}
```

In `Validate`, directly before `if len(errs) > 0 {`, add:

```go
	if c.Web.Listen != "" {
		_, port, err := net.SplitHostPort(c.Web.Listen)
		n, perr := strconv.Atoi(port)
		if err != nil || perr != nil || n < 1 || n > 65535 {
			errs = append(errs, "web.listen must be host:port with a port from 1 to 65535, such as 0.0.0.0:8080")
		}
	}
	for _, h := range c.Web.Hosts {
		if len(h) > 253 || !hostnameRe.MatchString(h) {
			errs = append(errs, "web.hosts entries must be bare hostnames such as ghr.lan")
		}
	}
```

Run `timeout 60 gofmt -w internal/config/config.go internal/config/config_test.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/config -v`
Expected: PASS, every test in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add the web listener block"
```

---

### Task 2: webui: password file

**Files:**
- Create: `internal/webui/password.go`
- Test: `internal/webui/password_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: C2.

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

Create `internal/webui/password_test.go`:

```go
package webui

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHashRoundTrip(t *testing.T) {
	h, err := newHash("correct horse battery", rand.Reader, 1000)
	if err != nil {
		t.Fatal(err)
	}
	s := h.String()
	if !strings.HasPrefix(s, "pbkdf2-sha256$1000$") || strings.Count(s, "$") != 3 {
		t.Fatalf("stored form %q", s)
	}
	back, err := parseHash(s)
	if err != nil {
		t.Fatal(err)
	}
	if !back.matches("correct horse battery") {
		t.Fatal("the right password does not match")
	}
	if back.matches("correct horse batterz") {
		t.Fatal("a wrong password matches")
	}
	other, _ := newHash("correct horse battery", rand.Reader, 1000)
	if other.String() == s {
		t.Fatal("two hashes of one password share a salt")
	}
}

func TestParseHashRejectsMalformed(t *testing.T) {
	key := strings.Repeat("A", 43)
	for _, s := range []string{
		"",
		"plain text",
		"pbkdf2-sha256$x$AAAAAAAAAAAAAAAAAAAAAA$" + key,
		"pbkdf2-sha256$0$AAAAAAAAAAAAAAAAAAAAAA$" + key,
		"md5$1000$AAAAAAAAAAAAAAAAAAAAAA$" + key,
		"pbkdf2-sha256$1000$AAAAAAAAAAAAAAAAAAAAAA$AAAA",
		"pbkdf2-sha256$1000$$" + key,
		"pbkdf2-sha256$1000$!!!$" + key,
		"pbkdf2-sha256$1000$AA$" + key,
		"pbkdf2-sha256$999$AAAAAAAAAAAAAAAAAAAAAA$" + key,
		"pbkdf2-sha256$10000001$AAAAAAAAAAAAAAAAAAAAAA$" + key,
	} {
		if _, err := parseHash(s); !errors.Is(err, ErrUnreadable) {
			t.Errorf("%q: want ErrUnreadable, got %v", s, err)
		}
	}
}

// A reader racing the writer must always see a complete hash: a truncating
// write would expose an empty or partial file between truncate and write.
func TestWriteHashReplacesAtomically(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses to rename over a file another handle has open; CI runs this on Linux")
	}
	path := filepath.Join(t.TempDir(), "web-password")
	first, _ := newHash("correct horse battery", rand.Reader, 1000)
	if err := writeHash(path, first); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	bad := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, ok, err := readHash(path); !ok || err != nil {
				select {
				case bad <- fmt.Sprintf("ok %v err %v", ok, err):
				default:
				}
			}
		}
	}()
	for range 200 {
		h, _ := newHash("correct horse battery", rand.Reader, 1000)
		if err := writeHash(path, h); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	<-done
	select {
	case b := <-bad:
		t.Fatalf("a reader saw an incomplete password file: %s", b)
	default:
	}
}

func TestWriteAndReadHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web-password")
	if _, ok, err := readHash(path); ok || err != nil {
		t.Fatalf("missing file: ok %v err %v", ok, err)
	}
	h, err := newHash("correct horse battery", rand.Reader, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeHash(path, h); err != nil {
		t.Fatal(err)
	}
	got, ok, err := readHash(path)
	if !ok || err != nil || !got.matches("correct horse battery") {
		t.Fatalf("read back: ok %v err %v", ok, err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v err %v", fi.Mode().Perm(), err)
		}
	}
	h2, _ := newHash("another long secret", rand.Reader, 1000)
	if err := writeHash(path, h2); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := readHash(path); !got.matches("another long secret") {
		t.Fatal("rewrite did not replace the hash")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
	os.WriteFile(path, []byte("garbage\n"), 0o600)
	if _, ok, err := readHash(path); ok || !errors.Is(err, ErrUnreadable) {
		t.Fatalf("garbage: ok %v err %v", ok, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/webui -v`
Expected: FAIL to compile with `undefined: newHash`.

- [ ] **Step 3: Write the implementation**

Create `internal/webui/password.go`:

```go
// Package webui serves the browser UI: password login, sessions, the request
// checks in front of the control API, and the embedded single-page app.
package webui

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	hashScheme = "pbkdf2-sha256"
	// DefaultIterations is OWASP's 2023 figure for PBKDF2-HMAC-SHA256.
	DefaultIterations = 600000
	saltLen           = 16
	keyLen            = 32
	// minIterations admits the 1000-iteration hashes tests write;
	// maxIterations stops a hand-edited file from making every login hang.
	minIterations = 1000
	maxIterations = 10000000
	MinPasswordLen    = 12
	MaxPasswordLen    = 1024
)

// ErrUnreadable means the password file exists but cannot be used.
var ErrUnreadable = errors.New("web password file is unreadable; run ghr web reset-password")

type passwordHash struct {
	iter int
	salt []byte
	key  []byte
}

func newHash(password string, rand io.Reader, iter int) (passwordHash, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand, salt); err != nil {
		return passwordHash{}, err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iter, keyLen)
	if err != nil {
		return passwordHash{}, err
	}
	return passwordHash{iter: iter, salt: salt, key: key}, nil
}

func (h passwordHash) String() string {
	enc := base64.RawStdEncoding
	return fmt.Sprintf("%s$%d$%s$%s", hashScheme, h.iter, enc.EncodeToString(h.salt), enc.EncodeToString(h.key))
}

func parseHash(s string) (passwordHash, error) {
	parts := strings.Split(strings.TrimSpace(s), "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return passwordHash{}, ErrUnreadable
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < minIterations || iter > maxIterations {
		return passwordHash{}, ErrUnreadable
	}
	enc := base64.RawStdEncoding
	salt, serr := enc.DecodeString(parts[2])
	key, kerr := enc.DecodeString(parts[3])
	if serr != nil || kerr != nil || len(salt) != saltLen || len(key) != keyLen {
		return passwordHash{}, ErrUnreadable
	}
	return passwordHash{iter: iter, salt: salt, key: key}, nil
}

func (h passwordHash) matches(password string) bool {
	key, err := pbkdf2.Key(sha256.New, password, h.salt, h.iter, len(h.key))
	return err == nil && subtle.ConstantTimeCompare(key, h.key) == 1
}

// readHash returns the stored hash; ok is false when no password is set.
func readHash(path string) (h passwordHash, ok bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return passwordHash{}, false, nil
	}
	if err != nil {
		return passwordHash{}, false, fmt.Errorf("%w (%v)", ErrUnreadable, err)
	}
	h, err = parseHash(string(data))
	if err != nil {
		return passwordHash{}, false, err
	}
	return h, true, nil
}

// writeHash replaces the password file atomically. os.CreateTemp creates the
// file with mode 0600, so the hash is never readable by other users.
func writeHash(path string, h passwordHash) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".web-password-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(h.String() + "\n"); err != nil {
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

Run `timeout 60 gofmt -w internal/webui/password.go internal/webui/password_test.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/webui -v`
Expected: PASS: `TestHashRoundTrip`, `TestParseHashRejectsMalformed`, `TestWriteHashReplacesAtomically` (skipped on Windows), `TestWriteAndReadHash`.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/password.go internal/webui/password_test.go
git commit -m "feat(webui): store the web password as PBKDF2"
```

---

### Task 3: webui: Auth sessions and throttle

**Files:**
- Create: `internal/webui/auth.go`
- Test: `internal/webui/auth_test.go`

**Interfaces:**
- Consumes: C2.
- Produces: C3, including the test helpers `newTestAuth`, `clock` and `pw` that Tasks 4 and 5 reuse.

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

Design notes the code below implements, so a reviewer can check them:
- A login holds one of the `maxDerivations` slots of `a.sem` from just before PBKDF2 until it returns. It checks the lockout three times: before reading the password file, once it holds a slot, and under `a.mu` right before recording the outcome. The last check is what stops a burst: requests already deriving when the fifth failure lands return `ThrottledError` instead of a result, so a burst yields exactly `maxFailures` wrong-password answers, and a correct password queued behind it cannot clear the lockout.
- Failures are a rolling window: each client keeps the times of its failures in the last 15 minutes, and the fifth one inside the window locks the client out for 15 minutes.
- `a.gen` counts password changes and resets. A login records it before reading the password file and publishes its session only if it is unchanged, so a login that verified the old password cannot create a session after a reset or change ended them all. `ChangePassword` writes the file and bumps `a.gen` while still holding its derivation slot.
- Expired sessions and stale failure counters are swept, under `a.mu`, on every setup and login attempt.

- [ ] **Step 1: Write the failing tests**

Create `internal/webui/auth_test.go`:

```go
package webui

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const pw = "correct horse battery"

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestAuth(t *testing.T) (*Auth, *clock) {
	t.Helper()
	c := &clock{t: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)}
	a := NewAuth(filepath.Join(t.TempDir(), "web-password"), c.now, rand.Reader)
	a.iter = 1000
	return a, c
}

func TestSetupOnce(t *testing.T) {
	a, _ := newTestAuth(t)
	if req, err := a.SetupRequired(); !req || err != nil {
		t.Fatalf("fresh: required %v err %v", req, err)
	}
	if _, err := a.Setup("short"); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("short: %v", err)
	}
	if _, err := a.Setup(strings.Repeat("x", MaxPasswordLen+1)); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("long: %v", err)
	}
	if _, err := a.Login("10.0.0.1", pw); !errors.Is(err, ErrSetupRequired) {
		t.Fatalf("login before setup: %v", err)
	}
	id, err := a.Setup(pw)
	if err != nil || !a.Valid(id) {
		t.Fatalf("setup: id valid %v err %v", a.Valid(id), err)
	}
	if req, _ := a.SetupRequired(); req {
		t.Fatal("setup still required after setup")
	}
	if _, err := a.Setup(pw); !errors.Is(err, ErrPasswordSet) {
		t.Fatalf("second setup: %v", err)
	}
}

func TestConcurrentSetupHasOneWinner(t *testing.T) {
	a, _ := newTestAuth(t)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := a.Setup(pw)
			errs <- err
		}()
	}
	e1, e2 := <-errs, <-errs
	if (e1 == nil) == (e2 == nil) || !(errors.Is(e1, ErrPasswordSet) || errors.Is(e2, ErrPasswordSet)) {
		t.Fatalf("want one success and one ErrPasswordSet, got %v and %v", e1, e2)
	}
}

func TestLoginThrottle(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	for i := range maxFailures {
		if _, err := a.Login("10.0.0.1", "wrong password!"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	var te *ThrottledError
	if _, err := a.Login("10.0.0.1", pw); !errors.As(err, &te) || !te.RetryAt.Equal(c.now().Add(lockout)) {
		t.Fatalf("sixth attempt: %v", err)
	}
	if _, err := a.Login("10.0.0.2", pw); err != nil {
		t.Fatalf("another client was throttled: %v", err)
	}
	c.add(lockout)
	if id, err := a.Login("10.0.0.1", pw); err != nil || !a.Valid(id) {
		t.Fatalf("after the lockout: %v", err)
	}
}

func TestFailuresUseARollingWindow(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	fail := func(n int) {
		for range n {
			if _, err := a.Login("10.0.0.1", "wrong password!"); !errors.Is(err, ErrWrongPassword) {
				t.Fatalf("at %v: %v", c.now(), err)
			}
		}
	}
	fail(1)
	c.add(14 * time.Minute)
	fail(3)
	c.add(2 * time.Minute)
	fail(2)
	var te *ThrottledError
	if _, err := a.Login("10.0.0.1", pw); !errors.As(err, &te) {
		t.Fatalf("five failures within the last 15 minutes did not lock the client out: %v", err)
	}
}

func TestBurstCannotExceedTheLimit(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 20)
	for range 20 {
		go func() {
			<-start
			_, err := a.Login("10.0.0.1", "wrong password!")
			results <- err
		}()
	}
	close(start)
	wrong, throttled := 0, 0
	for range 20 {
		var te *ThrottledError
		switch err := <-results; {
		case errors.Is(err, ErrWrongPassword):
			wrong++
		case errors.As(err, &te):
			throttled++
		default:
			t.Fatalf("unexpected %v", err)
		}
	}
	if wrong != maxFailures || throttled != 20-maxFailures {
		t.Fatalf("%d wrong-password answers and %d throttled, want %d and %d", wrong, throttled, maxFailures, 20-maxFailures)
	}
	var te *ThrottledError
	if _, err := a.Login("10.0.0.1", pw); !errors.As(err, &te) {
		t.Fatalf("the right password after the burst: %v", err)
	}
}

func TestSuccessClearsFailures(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	for range maxFailures - 1 {
		a.Login("10.0.0.1", "wrong password!")
	}
	if _, err := a.Login("10.0.0.1", pw); err != nil {
		t.Fatal(err)
	}
	for range maxFailures - 1 {
		a.Login("10.0.0.1", "wrong password!")
	}
	if _, err := a.Login("10.0.0.1", pw); err != nil {
		t.Fatalf("failures before the success still counted: %v", err)
	}
}

func TestFailuresExpireAndAreSwept(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	for range maxFailures - 1 {
		a.Login("10.0.0.1", "wrong password!")
	}
	c.add(failWindow)
	a.Login("10.0.0.9", "wrong password!")
	a.mu.Lock()
	_, stale := a.fails["10.0.0.1"]
	a.mu.Unlock()
	if stale {
		t.Fatal("an expired failure counter was not swept")
	}
	for range maxFailures - 1 {
		a.Login("10.0.0.1", "wrong password!")
	}
	if _, err := a.Login("10.0.0.1", pw); err != nil {
		t.Fatalf("expired failures still counted: %v", err)
	}
}

func TestSessionsExpireAndEnd(t *testing.T) {
	a, c := newTestAuth(t)
	id1, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := a.Login("10.0.0.1", pw)
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 || len(id1) != 43 {
		t.Fatalf("session ids %q %q", id1, id2)
	}
	a.Logout(id1)
	if a.Valid(id1) || !a.Valid(id2) {
		t.Fatalf("logout: id1 %v id2 %v", a.Valid(id1), a.Valid(id2))
	}
	if a.Valid("") || a.Valid("made-up") {
		t.Fatal("an unknown id is valid")
	}
	c.add(sessionTTL)
	if a.Valid(id2) {
		t.Fatal("a session outlived 30 days")
	}
}

func TestFailedLoginSweepsExpiredSessions(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	c.add(sessionTTL)
	a.Login("10.0.0.1", "wrong password!")
	a.mu.Lock()
	n := len(a.sessions)
	a.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d expired sessions left after a failed login", n)
	}
}

func TestChangePassword(t *testing.T) {
	a, _ := newTestAuth(t)
	keep, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.Login("10.0.0.1", pw)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ChangePassword(keep, "wrong password!", "another long secret"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong current: %v", err)
	}
	if !a.Valid(keep) || !a.Valid(other) {
		t.Fatal("a refused change ended sessions")
	}
	if err := a.ChangePassword(keep, pw, "short"); !errors.Is(err, ErrPasswordLength) {
		t.Fatalf("short new: %v", err)
	}
	if err := a.ChangePassword(keep, pw, "another long secret"); err != nil {
		t.Fatal(err)
	}
	if !a.Valid(keep) || a.Valid(other) {
		t.Fatalf("after change: keep %v other %v", a.Valid(keep), a.Valid(other))
	}
	if _, err := a.Login("10.0.0.2", pw); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("old password: %v", err)
	}
	if _, err := a.Login("10.0.0.2", "another long secret"); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestResetEndsEverything(t *testing.T) {
	a, _ := newTestAuth(t)
	id, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Reset(); err != nil {
		t.Fatal(err)
	}
	if a.Valid(id) {
		t.Fatal("a session survived the reset")
	}
	if req, err := a.SetupRequired(); !req || err != nil {
		t.Fatalf("after reset: required %v err %v", req, err)
	}
	if err := a.Reset(); err != nil {
		t.Fatalf("reset without a password: %v", err)
	}
}

// fillSlots takes every derivation slot, so the next login stops just before
// PBKDF2, after it has read the password file.
func fillSlots(a *Auth) {
	for range maxDerivations {
		a.sem <- struct{}{}
	}
}

func TestLoginRacingAResetCreatesNoSession(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
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
	if err := a.Reset(); err != nil {
		t.Fatal(err)
	}
	<-a.sem
	r := <-done
	if !errors.Is(r.err, ErrSetupRequired) || a.Valid(r.id) {
		t.Fatalf("a login that read the old password outlived the reset: id valid %v err %v", a.Valid(r.id), r.err)
	}
	<-a.sem
}

// Waiting senders on a channel are served first come, first served, so the
// change, queued first, takes the freed slot and publishes before releasing
// it to the login.
func TestLoginRacingAPasswordChangeCreatesNoSession(t *testing.T) {
	a, _ := newTestAuth(t)
	keep, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
	changed := make(chan error, 1)
	go func() { changed <- a.ChangePassword(keep, pw, "another long secret") }()
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
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err == nil || a.Valid(r.id) {
		t.Fatalf("a login that read the old password outlived the change: id valid %v err %v", a.Valid(r.id), r.err)
	}
	if !a.Valid(keep) {
		t.Fatal("the changing session was ended")
	}
	<-a.sem
}

func TestUnreadableFile(t *testing.T) {
	a, _ := newTestAuth(t)
	if err := a.Check(); err != nil {
		t.Fatalf("missing file: %v", err)
	}
	os.WriteFile(a.path, []byte("garbage\n"), 0o600)
	if err := a.Check(); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("check: %v", err)
	}
	if _, err := a.SetupRequired(); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("setup required: %v", err)
	}
	if _, err := a.Login("10.0.0.1", pw); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("login: %v", err)
	}
	if _, err := a.Setup(pw); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("setup: %v", err)
	}
}

func TestDerivationsAreBounded(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	fillSlots(a)
	done := make(chan error, 1)
	go func() {
		_, err := a.Login("10.0.0.1", pw)
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("a login derived a key while every slot was taken")
	case <-time.After(50 * time.Millisecond):
	}
	<-a.sem
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-a.sem
}

func TestClientKey(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.0.10:5555":           "192.168.0.10",
		"[2001:db8:1:2:aaaa::1]:5555": "2001:db8:1:2::/64",
		"[2001:db8:1:2:bbbb::9]:6000": "2001:db8:1:2::/64",
		"[::ffff:10.0.0.1]:80":        "10.0.0.1",
		"[fe80::1%eth0]:80":           "fe80::/64",
		"not an address":              "not an address",
	} {
		if got := ClientKey(in); got != want {
			t.Errorf("ClientKey(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/webui -v`
Expected: FAIL to compile with `undefined: NewAuth`.

- [ ] **Step 3: Write the implementation**

Create `internal/webui/auth.go`:

```go
package webui

import (
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"
)

const (
	sessionTTL  = 30 * 24 * time.Hour
	maxFailures = 5
	failWindow  = 15 * time.Minute
	lockout     = 15 * time.Minute
	// maxDerivations bounds concurrent PBKDF2 runs: each costs about half a
	// second of CPU, and the daemon's tick loop shares the machine.
	maxDerivations = 2
)

var (
	ErrPasswordLength = errors.New("password must be 12 to 1024 bytes")
	ErrPasswordSet    = errors.New("a web password is already set")
	ErrSetupRequired  = errors.New("no web password is set yet; set one first")
	ErrWrongPassword  = errors.New("wrong password")
)

// ThrottledError refuses a login from a client with too many recent failures.
type ThrottledError struct{ RetryAt time.Time }

func (e *ThrottledError) Error() string {
	return "too many failed logins; retry after " + e.RetryAt.UTC().Format(time.RFC3339)
}

type failures struct {
	times       []time.Time // failures within the last failWindow, oldest first
	lockedUntil time.Time
}

// Auth owns the web password file, the login sessions and the login throttle.
// Sessions live in memory, so a daemon restart ends them.
type Auth struct {
	path string
	now  func() time.Time
	rand io.Reader
	iter int
	sem  chan struct{}

	fileMu sync.Mutex // serialises password file changes
	mu     sync.Mutex // guards the fields below
	// gen changes whenever the password changes or is reset; a login that
	// read the file under an older gen must not publish a session.
	gen      uint64
	sessions map[string]time.Time
	fails    map[string]*failures
}

func NewAuth(path string, now func() time.Time, rand io.Reader) *Auth {
	return &Auth{
		path: path, now: now, rand: rand, iter: DefaultIterations,
		sem:      make(chan struct{}, maxDerivations),
		sessions: map[string]time.Time{},
		fails:    map[string]*failures{},
	}
}

func (a *Auth) derive(fn func()) {
	a.sem <- struct{}{}
	defer func() { <-a.sem }()
	fn()
}

// Check reports a password file that exists but cannot be used.
func (a *Auth) Check() error {
	_, _, err := readHash(a.path)
	return err
}

// SetupRequired reports whether no password has been set yet.
func (a *Auth) SetupRequired() (bool, error) {
	_, ok, err := readHash(a.path)
	if err != nil {
		return false, err
	}
	return !ok, nil
}

func validLength(p string) bool { return len(p) >= MinPasswordLen && len(p) <= MaxPasswordLen }

// Setup sets the first password and starts a session for the caller.
func (a *Auth) Setup(password string) (string, error) {
	if !validLength(password) {
		return "", ErrPasswordLength
	}
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	a.mu.Lock()
	a.sweepLocked(a.now())
	a.mu.Unlock()
	_, ok, err := readHash(a.path)
	if err != nil {
		return "", err
	}
	if ok {
		return "", ErrPasswordSet
	}
	var h passwordHash
	a.derive(func() { h, err = newHash(password, a.rand, a.iter) })
	if err != nil {
		return "", err
	}
	if err := writeHash(a.path, h); err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.newSessionLocked(a.now())
}

// Login checks password for the client named by key (see ClientKey) and
// starts a session.
func (a *Auth) Login(key, password string) (string, error) {
	a.mu.Lock()
	err := a.admitLocked(key, a.now())
	gen := a.gen
	a.mu.Unlock()
	if err != nil {
		return "", err
	}
	h, ok, err := readHash(a.path)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrSetupRequired
	}
	a.sem <- struct{}{}
	defer func() { <-a.sem }()
	a.mu.Lock()
	err = a.admitLocked(key, a.now())
	a.mu.Unlock()
	if err != nil {
		return "", err
	}
	match := h.matches(password)
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if err := a.admitLocked(key, now); err != nil {
		return "", err
	}
	if !match {
		f := a.fails[key]
		if f == nil {
			f = &failures{}
			a.fails[key] = f
		}
		f.times = append(f.times, now)
		if len(f.times) >= maxFailures {
			f.lockedUntil = now.Add(lockout)
		}
		return "", ErrWrongPassword
	}
	if a.gen != gen {
		if _, ok, _ := readHash(a.path); !ok {
			return "", ErrSetupRequired
		}
		return "", ErrWrongPassword
	}
	delete(a.fails, key)
	return a.newSessionLocked(now)
}

// admitLocked sweeps, then refuses a client that is locked out. a.mu is held.
func (a *Auth) admitLocked(key string, now time.Time) error {
	a.sweepLocked(now)
	if f := a.fails[key]; f != nil && now.Before(f.lockedUntil) {
		return &ThrottledError{RetryAt: f.lockedUntil}
	}
	return nil
}

// sweepLocked drops expired sessions, failures older than failWindow, and
// clients with neither recent failures nor a lockout. a.mu is held.
func (a *Auth) sweepLocked(now time.Time) {
	for k, exp := range a.sessions {
		if !now.Before(exp) {
			delete(a.sessions, k)
		}
	}
	for k, f := range a.fails {
		i := 0
		for i < len(f.times) && now.Sub(f.times[i]) >= failWindow {
			i++
		}
		f.times = f.times[i:]
		if len(f.times) == 0 && !now.Before(f.lockedUntil) {
			delete(a.fails, k)
		}
	}
}

// newSessionLocked starts a session expiring sessionTTL after now. a.mu is held.
func (a *Auth) newSessionLocked(now time.Time) (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(a.rand, b); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	a.sessions[id] = now.Add(sessionTTL)
	return id, nil
}

// Valid reports whether id names a live session.
func (a *Auth) Valid(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[id]
	return ok && a.now().Before(exp)
}

func (a *Auth) Logout(id string) {
	a.mu.Lock()
	delete(a.sessions, id)
	a.mu.Unlock()
}

// ChangePassword replaces the password and ends every session except keep.
func (a *Auth) ChangePassword(keep, current, next string) error {
	if !validLength(next) {
		return ErrPasswordLength
	}
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	h, ok, err := readHash(a.path)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSetupRequired
	}
	// The slot is held until the change is published, so a login queued
	// behind it sees the new gen.
	var match bool
	a.derive(func() {
		if match = h.matches(current); !match {
			return
		}
		var nh passwordHash
		if nh, err = newHash(next, a.rand, a.iter); err != nil {
			return
		}
		if err = writeHash(a.path, nh); err != nil {
			return
		}
		a.mu.Lock()
		a.gen++
		for k := range a.sessions {
			if k != keep {
				delete(a.sessions, k)
			}
		}
		a.mu.Unlock()
	})
	if !match {
		return ErrWrongPassword
	}
	return err
}

// Reset forgets the password and ends every session; the next visitor sets a
// new password.
func (a *Auth) Reset() error {
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	if err := os.Remove(a.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	a.mu.Lock()
	a.gen++
	clear(a.sessions)
	a.mu.Unlock()
	return nil
}

// ClientKey is the throttle key for a request's remote address: the IPv4
// address, or the IPv6 /64, since one IPv6 host can use a whole /64.
func ClientKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	ip = ip.Unmap().WithZone("")
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.String()
	}
	return ip.String()
}
```

Run `timeout 60 gofmt -w internal/webui/auth.go internal/webui/auth_test.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/webui -count=3 -v`
Expected: PASS three times over, including every test from Task 2; the race tests do not flake.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/auth.go internal/webui/auth_test.go
git commit -m "feat(webui): add web sessions and login throttle"
```

---

### Task 4: webui: static app files

**Files:**
- Create: `internal/webui/static.go`
- Test: `internal/webui/static_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: C4.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `internal/webui/static_test.go`:

```go
package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":         {Data: []byte("<html>app</html>")},
		"theme-init.js":      {Data: []byte("init()")},
		"assets/app-1234.js": {Data: []byte("app()")},
	}
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestStaticServesFiles(t *testing.T) {
	h := staticHandler(testFS())
	rec := get(h, http.MethodGet, "/assets/app-1234.js")
	if rec.Code != 200 || rec.Body.String() != "app()" {
		t.Fatalf("asset: %d %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache-control %q", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("asset content-type %q", ct)
	}
	rec = get(h, http.MethodGet, "/theme-init.js")
	if rec.Code != 200 || rec.Body.String() != "init()" || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("root file: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
}

func TestStaticFallsBackToIndex(t *testing.T) {
	h := staticHandler(testFS())
	for _, p := range []string{"/", "/runners/abc123", "/assets", "/assets/", "/index.html", "/../index.html", "/missing.js"} {
		rec := get(h, http.MethodGet, p)
		if rec.Code != 200 || rec.Body.String() != "<html>app</html>" {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: cache-control %q", p, cc)
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("%s: redirected to %q", p, loc)
		}
	}
}

func TestStaticMethods(t *testing.T) {
	h := staticHandler(testFS())
	if rec := get(h, http.MethodPost, "/"); rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("POST: %d allow %q", rec.Code, rec.Header().Get("Allow"))
	}
	if rec := get(h, http.MethodHead, "/"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD: %d body %d", rec.Code, rec.Body.Len())
	}
}

func TestStaticWithoutABuild(t *testing.T) {
	h := staticHandler(fstest.MapFS{
		".gitkeep":           {Data: []byte{}},
		"assets/app-1234.js": {Data: []byte("app()")},
	})
	for _, p := range []string{"/runners", "/", "/.gitkeep", "/assets/app-1234.js"} {
		rec := get(h, http.MethodGet, p)
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "ghr web UI was not built into this binary") {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/webui -run 'TestStatic' -v`
Expected: FAIL to compile with `undefined: staticHandler`.

- [ ] **Step 3: Write the implementation**

Create `internal/webui/static.go`:

```go
package webui

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// staticHandler serves the single-page app from fsys: a path naming a file
// gets that file, and any other path gets index.html so client routes survive
// a reload. Without index.html the build is absent or partial, and every path
// gets the 503. http.FileServer is not used because it lists directories and
// redirects /index.html to /.
func staticHandler(fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		index, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "ghr web UI was not built into this binary", http.StatusServiceUnavailable)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" {
			if data, err := fs.ReadFile(fsys, name); err == nil {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}
```

Run `timeout 60 gofmt -w internal/webui/static.go internal/webui/static_test.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/webui -v`
Expected: PASS, including every earlier webui test.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/static.go internal/webui/static_test.go
git commit -m "feat(webui): serve the embedded single-page app"
```

---

### Task 5: webui: request handler

**Files:**
- Create: `internal/webui/handler.go`
- Test: `internal/webui/handler_test.go`

**Interfaces:**
- Consumes: C2 (`ErrUnreadable`), C3 (`Auth` and its errors, `ClientKey`, `sessionTTL`, `maxFailures`; test helpers `newTestAuth`, `pw`), C4 (`staticHandler`; test helper `testFS`).
- Produces: C5.

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

Create `internal/webui/handler_test.go`:

```go
package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type fakeAPI struct{ paths []string }

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	if r.URL.Path == "/status" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
		return
	}
	http.NotFound(w, r)
}

func newTestHandler(t *testing.T) (http.Handler, *Auth, *fakeAPI) {
	t.Helper()
	a, _ := newTestAuth(t)
	api := &fakeAPI{}
	return Handler(a, api, testFS(), []string{"ghr.lan"}), a, api
}

// do sends a request as the browser app would: Host ghr.lan:8080 and the
// X-GHR header on the routes that need it. mod runs last and may undo either.
func do(h http.Handler, method, target, body string, mod func(*http.Request)) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, rd)
	req.Host = "ghr.lan:8080"
	req.RemoteAddr = "192.168.0.10:5555"
	if strings.HasPrefix(req.URL.Path, "/api/") || (method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/auth/")) {
		req.Header.Set("X-GHR", "1")
	}
	if mod != nil {
		mod(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func withCookie(id string) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: cookieName, Value: id}) }
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	t.Fatalf("no %s cookie in %v", cookieName, rec.Header())
	return nil
}

func body(pw string) string { return `{"password":"` + pw + `"}` }

func TestSetupThenAPI(t *testing.T) {
	h, _, api := newTestHandler(t)
	rec := do(h, http.MethodGet, "/auth/state", "", nil)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"authenticated":false,"setup_required":true}` {
		t.Fatalf("state: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(h, http.MethodPost, "/auth/setup", body(pw), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	c := sessionCookie(t, rec)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.MaxAge != 2592000 || c.Path != "/" || c.Secure {
		t.Fatalf("cookie %+v", c)
	}
	rec = do(h, http.MethodGet, "/auth/state", "", withCookie(c.Value))
	if strings.TrimSpace(rec.Body.String()) != `{"authenticated":true,"setup_required":false}` {
		t.Fatalf("state after setup: %s", rec.Body.String())
	}
	rec = do(h, http.MethodGet, "/api/status", "", withCookie(c.Value))
	if rec.Code != 200 || rec.Body.String() != `{"ok":true}` || len(api.paths) != 1 || api.paths[0] != "GET /status" {
		t.Fatalf("api: %d %q %v", rec.Code, rec.Body.String(), api.paths)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("api cache-control %q", cc)
	}
	if rec := do(h, http.MethodPost, "/auth/setup", body(pw), nil); rec.Code != http.StatusConflict {
		t.Fatalf("second setup: %d", rec.Code)
	}
}

func TestLoginResponses(t *testing.T) {
	h, a, _ := newTestHandler(t)
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), nil); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "no web password is set yet") {
		t.Fatalf("login before setup: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/setup", body("short"), nil); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "password must be 12 to 1024 bytes") {
		t.Fatalf("short setup: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	for range maxFailures {
		rec := do(h, http.MethodPost, "/auth/login", body("wrong password!"), nil)
		if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
			t.Fatalf("wrong password: %d %v", rec.Code, rec.Result().Cookies())
		}
	}
	rec := do(h, http.MethodPost, "/auth/login", body(pw), nil)
	var e struct {
		Error   string `json:"error"`
		RetryAt string `json:"retry_at"`
	}
	json.Unmarshal(rec.Body.Bytes(), &e)
	if _, err := time.Parse(time.RFC3339, e.RetryAt); rec.Code != http.StatusTooManyRequests || err != nil {
		t.Fatalf("throttled: %d %s", rec.Code, rec.Body.String())
	}
	other := do(h, http.MethodPost, "/auth/login", body(pw), func(r *http.Request) { r.RemoteAddr = "192.168.0.11:6000" })
	if other.Code != http.StatusNoContent {
		t.Fatalf("other client: %d", other.Code)
	}
	sessionCookie(t, other)
}

func TestSecureCookie(t *testing.T) {
	h, _, _ := newTestHandler(t)
	rec := do(h, http.MethodPost, "/auth/setup", body(pw), func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") })
	if c := sessionCookie(t, rec); !c.Secure {
		t.Fatal("cookie not Secure behind an HTTPS proxy")
	}
	rec = do(h, http.MethodPost, "https://ghr.lan/auth/login", body(pw), nil)
	if c := sessionCookie(t, rec); !c.Secure {
		t.Fatal("cookie not Secure over TLS")
	}
}

func TestRequestChecks(t *testing.T) {
	h, a, _ := newTestHandler(t)
	id, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	rec := do(h, http.MethodGet, "/auth/state", "", func(r *http.Request) { r.Host = "evil.example:8080" })
	if rec.Code != http.StatusMisdirectedRequest || !strings.Contains(rec.Body.String(), "unknown host; add it to web.hosts") {
		t.Fatalf("unknown host: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Security-Policy") != csp {
		t.Fatal("421 lacks the CSP")
	}
	if rec := do(h, http.MethodGet, "/api/status", "", func(r *http.Request) { r.Host = "evil.example" }); rec.Code != http.StatusMisdirectedRequest ||
		rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("421 on /api: %d cache-control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	for _, host := range []string{"192.168.0.99:8080", "[::1]:8080", "localhost:5173", "GHR.LAN", "ghr.lan"} {
		if rec := do(h, http.MethodGet, "/auth/state", "", func(r *http.Request) { r.Host = host }); rec.Code != 200 {
			t.Errorf("host %s: %d", host, rec.Code)
		}
	}
	noHeader := func(r *http.Request) { r.Header.Del("X-GHR") }
	if rec := do(h, http.MethodGet, "/api/status", "", noHeader); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "missing X-GHR header") {
		t.Fatalf("no header, no session: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), noHeader); rec.Code != http.StatusForbidden {
		t.Fatalf("login without the header: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/status", "", nil); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Body.String(), "not logged in") {
		t.Fatalf("no session: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie("made-up")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad session: %d", rec.Code)
	}
	foreign := func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), foreign); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "cross-origin request refused") {
		t.Fatalf("foreign origin: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), func(r *http.Request) { r.Header.Set("Origin", "null") }); rec.Code != http.StatusForbidden {
		t.Fatalf("null origin: %d", rec.Code)
	}
	otherPort := func(r *http.Request) { r.Header.Set("Origin", "https://ghr.lan:8443") }
	if rec := do(h, http.MethodPost, "/auth/login", body(pw), otherPort); rec.Code != http.StatusNoContent {
		t.Fatalf("same host, other port: %d %s", rec.Code, rec.Body.String())
	}
	big := body(strings.Repeat("x", authBodyLimit+1))
	if rec := do(h, http.MethodPost, "/auth/login", big, nil); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "invalid request body") {
		t.Fatalf("oversized body: %d %s", rec.Code, rec.Body.String())
	}
	padded := body(pw) + strings.Repeat(" ", authBodyLimit)
	if rec := do(h, http.MethodPost, "/auth/login", padded, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("valid JSON padded past the cap: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/auth/login", body(pw)+` {"password":"x"}`, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/auth/logout", strings.Repeat("x", authBodyLimit+1), withCookie(id)); rec.Code != http.StatusBadRequest || !a.Valid(id) {
		t.Fatalf("oversized logout: %d, session valid %v", rec.Code, a.Valid(id))
	}
	rec = do(h, http.MethodGet, "/", "", withCookie(id))
	if rec.Header().Get("Content-Security-Policy") != csp || rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		rec.Header().Get("Referrer-Policy") != "same-origin" {
		t.Fatalf("security headers %v", rec.Header())
	}
}

func TestRoutes(t *testing.T) {
	h, a, api := newTestHandler(t)
	id, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		rec := do(h, m, "/auth/nope", "", nil)
		if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s /auth/nope: %d %q", m, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	if rec := do(h, http.MethodPost, "/", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/runners/abc123", "", nil); rec.Code != 200 || rec.Body.String() != "<html>app</html>" {
		t.Fatalf("client route: %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/api/nope", "", withCookie(id)); rec.Code != http.StatusNotFound || rec.Body.String() == "<html>app</html>" {
		t.Fatalf("unknown api path: %d %q", rec.Code, rec.Body.String())
	}
	if api.paths[len(api.paths)-1] != "GET /nope" {
		t.Fatalf("api saw %v", api.paths)
	}
}

func TestLogoutAndPasswordChange(t *testing.T) {
	h, a, _ := newTestHandler(t)
	keep, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.Login("10.0.0.1", pw)
	if err != nil {
		t.Fatal(err)
	}
	change := func(id, current, next string) *httptest.ResponseRecorder {
		return do(h, http.MethodPost, "/auth/password", `{"current":"`+current+`","new":"`+next+`"}`, withCookie(id))
	}
	if rec := do(h, http.MethodPost, "/auth/password", `{"current":"x","new":"y"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("change without a session: %d", rec.Code)
	}
	if rec := change(keep, "wrong password!", "another long secret"); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Body.String(), "wrong password") {
		t.Fatalf("wrong current: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(keep)); rec.Code != 200 {
		t.Fatalf("a wrong current password ended the session: %d", rec.Code)
	}
	if rec := change(keep, pw, "another long secret"); rec.Code != http.StatusNoContent {
		t.Fatalf("change: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(other)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other session survived the change: %d", rec.Code)
	}
	rec := do(h, http.MethodPost, "/auth/logout", "", withCookie(keep))
	if rec.Code != http.StatusNoContent || sessionCookie(t, rec).MaxAge >= 0 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(keep)); rec.Code != http.StatusUnauthorized {
		t.Fatalf("session survived logout: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/auth/logout", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("logout without a session: %d", rec.Code)
	}
}

func TestUnreadablePasswordFile(t *testing.T) {
	h, a, _ := newTestHandler(t)
	os.WriteFile(a.path, []byte("garbage\n"), 0o600)
	for _, rec := range []*httptest.ResponseRecorder{
		do(h, http.MethodGet, "/auth/state", "", nil),
		do(h, http.MethodPost, "/auth/login", body(pw), nil),
		do(h, http.MethodPost, "/auth/setup", body(pw), nil),
	} {
		if rec.Code != http.StatusInternalServerError ||
			!strings.Contains(rec.Body.String(), "web password file is unreadable; run ghr web reset-password") {
			t.Errorf("%d %s", rec.Code, rec.Body.String())
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/webui -run 'TestSetupThenAPI|TestLoginResponses|TestSecureCookie|TestRequestChecks|TestRoutes|TestLogoutAndPasswordChange|TestUnreadablePasswordFile' -v`
Expected: FAIL to compile with `undefined: Handler`.

- [ ] **Step 3: Write the implementation**

Create `internal/webui/handler.go`:

```go
package webui

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	cookieName    = "ghr_session"
	authBodyLimit = 4 << 10
	csp           = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'"
)

type handler struct {
	auth  *Auth
	hosts []string
}

// Handler serves the web UI's TCP listener: /auth/* for login, the control
// API under /api/ for logged-in sessions, and the app from static.
func Handler(a *Auth, api http.Handler, static fs.FS, hosts []string) http.Handler {
	h := &handler{auth: a, hosts: hosts}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/state", h.state)
	mux.HandleFunc("POST /auth/setup", h.setup)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("POST /auth/password", h.password)
	mux.HandleFunc("/auth/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/api/", h.requireSession(http.StripPrefix("/api", api)))
	mux.Handle("/", staticHandler(static))
	return h.checks(mux)
}

// checks runs before routing. The Host allowlist stops DNS rebinding; the
// X-GHR header forces a CORS preflight on any cross-origin script, which ghr
// never approves; the Origin check refuses cross-site form posts. Every /auth
// POST body is read in full here, so the 4 KB cap holds for routes that
// ignore their body.
func (h *handler) checks(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", csp)
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "same-origin")
		api := strings.HasPrefix(r.URL.Path, "/api/")
		auth := strings.HasPrefix(r.URL.Path, "/auth/")
		if api || auth {
			hd.Set("Cache-Control", "no-store")
		}
		if !h.hostAllowed(r.Host) {
			hd.Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusMisdirectedRequest)
			io.WriteString(w, "unknown host; add it to web.hosts\n")
			return
		}
		if (api || (auth && r.Method == http.MethodPost)) && r.Header.Get("X-GHR") != "1" {
			writeError(w, http.StatusForbidden, "missing X-GHR header")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" && !sameHost(o, r.Host) {
				writeError(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
		}
		if auth && r.Method == http.MethodPost {
			data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, authBodyLimit))
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
		}
		next.ServeHTTP(w, r)
	})
}

func hostname(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
}

func (h *handler) hostAllowed(host string) bool {
	name := hostname(host)
	if _, err := netip.ParseAddr(name); err == nil {
		return true
	}
	if strings.EqualFold(name, "localhost") {
		return true
	}
	for _, a := range h.hosts {
		if strings.EqualFold(name, a) {
			return true
		}
	}
	return false
}

// sameHost compares hostnames only: nginx-proxy-manager forwards Host without
// the port the browser used.
func sameHost(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Hostname() != "" && strings.EqualFold(u.Hostname(), hostname(host))
}

func (h *handler) session(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil || !h.auth.Valid(c.Value) {
		return "", false
	}
	return c.Value, true
}

func (h *handler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := h.session(r); !ok {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) state(w http.ResponseWriter, r *http.Request) {
	setup, err := h.auth.SetupRequired()
	if err != nil {
		h.fail(w, err)
		return
	}
	_, ok := h.session(r)
	writeJSON(w, http.StatusOK, map[string]bool{"setup_required": setup, "authenticated": !setup && ok})
}

type passwordBody struct {
	Password string `json:"password"`
	Current  string `json:"current"`
	New      string `json:"new"`
}

// decodeBody parses the whole body, which checks has already capped;
// json.Unmarshal, unlike a Decoder, rejects data after the JSON value.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	data, err := io.ReadAll(r.Body)
	if err == nil {
		err = json.Unmarshal(data, v)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func (h *handler) setup(w http.ResponseWriter, r *http.Request) {
	var b passwordBody
	if !decodeBody(w, r, &b) {
		return
	}
	id, err := h.auth.Setup(b.Password)
	if err != nil {
		h.fail(w, err)
		return
	}
	setSession(w, r, id, int(sessionTTL/time.Second))
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var b passwordBody
	if !decodeBody(w, r, &b) {
		return
	}
	id, err := h.auth.Login(ClientKey(r.RemoteAddr), b.Password)
	if err != nil {
		h.fail(w, err)
		return
	}
	setSession(w, r, id, int(sessionTTL/time.Second))
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	id, ok := h.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	h.auth.Logout(id)
	setSession(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) password(w http.ResponseWriter, r *http.Request) {
	id, ok := h.session(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	var b passwordBody
	if !decodeBody(w, r, &b) {
		return
	}
	if err := h.auth.ChangePassword(id, b.Current, b.New); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setSession sets the session cookie; maxAge -1 deletes it. X-Forwarded-Proto
// is trusted here because it can only make the cookie stricter.
func setSession(w http.ResponseWriter, r *http.Request, id string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: id, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
}

func (h *handler) fail(w http.ResponseWriter, err error) {
	var te *ThrottledError
	switch {
	case errors.As(err, &te):
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": err.Error(), "retry_at": te.RetryAt.UTC().Format(time.RFC3339),
		})
	case errors.Is(err, ErrWrongPassword):
		writeError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrPasswordLength):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrPasswordSet), errors.Is(err, ErrSetupRequired):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrUnreadable):
		writeError(w, http.StatusInternalServerError, ErrUnreadable.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

Run `timeout 60 gofmt -w internal/webui/handler.go internal/webui/handler_test.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/webui -v`
Expected: PASS, every webui test.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/handler.go internal/webui/handler_test.go
git commit -m "feat(webui): guard the API behind web login"
```

---

### Task 6: daemon: steps cache

**Files:**
- Modify: `internal/daemon/backend.go` (imports; `Backend` struct at lines 51-67; `RunnerSteps` at lines 115-138)
- Test: `internal/daemon/backend_test.go` (the `fakeGH` struct at lines 75-96 and its `ListJobs` at lines 111-130; new tests appended)

**Interfaces:**
- Consumes: nothing.
- Produces: nothing other tasks use.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 3 = 4

Design notes the code below implements: an entry lives until it is 5 seconds old or its runner leaves `Status().Instances`, checked on every call before a cache hit; concurrent callers for one runner share one in-flight GitHub fetch; an entry's age starts when it is stored, not when its fetch began; errors are never stored. `RunnerSteps` calls `b.now()` once on entry and once when storing.

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/backend_test.go`, add two fields to the `fakeGH` struct, directly below `cancelled int`:

```go
	gate   chan struct{} // when set, ListJobs waits for it after recording the call
	onJobs func()        // when set, ListJobs calls it after recording the call
```

In `fakeGH.ListJobs`, replace

```go
	f.lmu.Lock()
	f.jobCalls = append(f.jobCalls, runID)
	f.lmu.Unlock()
```

with

```go
	f.lmu.Lock()
	f.jobCalls = append(f.jobCalls, runID)
	f.lmu.Unlock()
	if f.onJobs != nil {
		f.onJobs()
	}
	if f.gate != nil {
		<-f.gate
	}
```

Append to `internal/daemon/backend_test.go`:

```go
func jobCallCount(gh *fakeGH) int {
	gh.lmu.Lock()
	defer gh.lmu.Unlock()
	return len(gh.jobCalls)
}

func TestRunnerStepsAreCached(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	ctx := context.Background()
	for range 3 {
		if s, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil || len(s) != 1 || s[0].Name != "checkout" {
			t.Fatalf("steps %+v err %v", s, err)
		}
	}
	if n := jobCallCount(gh); n != 1 {
		t.Fatalf("%d GitHub calls inside the TTL, want 1", n)
	}
	now = now.Add(stepsTTL)
	if _, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if n := jobCallCount(gh); n != 2 {
		t.Fatalf("%d GitHub calls after the TTL, want 2", n)
	}
}

func TestRunnerStepsAgeFromStorage(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	gh.onJobs = func() { now = now.Add(stepsTTL) }
	ctx := context.Background()
	for range 2 {
		if _, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil {
			t.Fatal(err)
		}
	}
	if n := jobCallCount(gh); n != 1 {
		t.Fatalf("%d GitHub calls: a slow fetch stored an already expired entry", n)
	}
}

func TestRunnerStepsDropAGoneRunner(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	ctx := context.Background()
	if _, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	m.insts = nil
	b.RunnerSteps(ctx, "aaaaaa")
	if n := jobCallCount(gh); n != 2 {
		t.Fatalf("%d GitHub calls: a runner that left Status kept its cached steps", n)
	}
}

func TestRunnerStepsErrorsAreNotCached(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	gh.jobs = map[int64][]github.Job{}
	gh.jobErr = map[int64]error{55: errors.New("boom")}
	for range 2 {
		if _, err := b.RunnerSteps(context.Background(), "aaaaaa"); err == nil {
			t.Fatal("error swallowed")
		}
	}
	if n := jobCallCount(gh); n != 2 {
		t.Fatalf("%d GitHub calls, want 2: an error was cached", n)
	}
}

func TestRunnerStepsShareOneFetch(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	gh.gate = make(chan struct{})
	type result struct {
		steps []model.Step
		err   error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() {
			s, err := b.RunnerSteps(context.Background(), "aaaaaa")
			results <- result{s, err}
		}()
	}
	waitFor(t, "the first fetch", func() bool { return jobCallCount(gh) == 1 })
	time.Sleep(50 * time.Millisecond)
	close(gh.gate)
	for range 8 {
		r := <-results
		if r.err != nil || len(r.steps) != 1 || r.steps[0].Name != "checkout" {
			t.Fatalf("steps %+v err %v", r.steps, r.err)
		}
	}
	if n := jobCallCount(gh); n != 1 {
		t.Fatalf("%d GitHub calls for 8 concurrent callers, want 1", n)
	}
}
```

`waitFor` is the helper in `internal/daemon/run_test.go:140`, in the same package.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon -run 'TestRunnerSteps' -v`
Expected: FAIL to compile with `undefined: stepsTTL`.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/backend.go`, add `"sync"` to the import block. In the `Backend` struct, directly below `checks  labelChecks`, add:

```go
	stepsMu    sync.Mutex
	steps      map[string]stepsEntry
	stepsCalls map[string]*stepsCall
```

Rename the existing method `func (b *Backend) RunnerSteps(ctx context.Context, id string) ([]model.Step, error)` to `func (b *Backend) fetchSteps(ctx context.Context, id string) ([]model.Step, error)`, leaving its body unchanged. Directly above it, add:

```go
// stepsTTL bounds how often one runner's steps are fetched from GitHub,
// however many browser tabs and TUIs poll its detail view.
const stepsTTL = 5 * time.Second

type stepsEntry struct {
	at    time.Time
	steps []model.Step
}

// stepsCall is one GitHub fetch that concurrent callers for a runner share.
type stepsCall struct {
	done  chan struct{}
	steps []model.Step
	err   error
}

func (b *Backend) RunnerSteps(ctx context.Context, id string) ([]model.Step, error) {
	live := map[string]bool{}
	for _, in := range b.M.Status().Instances {
		live[in.ID] = true
	}
	now := b.now()
	b.stepsMu.Lock()
	for k, e := range b.steps {
		if !live[k] || now.Sub(e.at) >= stepsTTL {
			delete(b.steps, k)
		}
	}
	if e, ok := b.steps[id]; ok {
		b.stepsMu.Unlock()
		return e.steps, nil
	}
	if c, ok := b.stepsCalls[id]; ok {
		b.stepsMu.Unlock()
		select {
		case <-c.done:
			return c.steps, c.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c := &stepsCall{done: make(chan struct{})}
	if b.stepsCalls == nil {
		b.stepsCalls = map[string]*stepsCall{}
	}
	b.stepsCalls[id] = c
	b.stepsMu.Unlock()

	// Joined callers share this fetch, so one caller closing its tab must
	// not cancel it for the others.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	c.steps, c.err = b.fetchSteps(fctx, id)
	cancel()

	b.stepsMu.Lock()
	delete(b.stepsCalls, id)
	if c.err == nil && live[id] {
		if b.steps == nil {
			b.steps = map[string]stepsEntry{}
		}
		b.steps[id] = stepsEntry{at: b.now(), steps: c.steps}
	}
	b.stepsMu.Unlock()
	close(c.done)
	return c.steps, c.err
}
```

Run `timeout 60 gofmt -w internal/daemon/backend.go internal/daemon/backend_test.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 400 go test ./internal/daemon -count=3 -v`
Expected: PASS three times over, including the existing `TestPauseAllStepsKillToken` (its fake manager reports no instances, so nothing is cached there).

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "perf(daemon): cache job steps for five seconds"
```

---

### Task 7: daemon: reload warning for web settings

**Files:**
- Modify: `internal/daemon/backend.go` (imports; `Backend` struct; `Reload` at lines 386-406)
- Test: `internal/daemon/backend_test.go`

**Interfaces:**
- Consumes: C1 (`config.Web`).
- Produces: C7 `Backend.WebApplied`.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Add `"net/http"`, `"net/http/httptest"` and `"slices"` to the import block of `internal/daemon/backend_test.go`, then append:

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

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon -run 'TestReloadWarnsOnWebChange|TestPatchCannotChangeWeb' -v`
Expected: `TestReloadWarnsOnWebChange` FAILS with `changed listen: [] <nil>`; `TestPatchCannotChangeWeb` passes (it guards existing behaviour).

- [ ] **Step 3: Write the implementation**

In `internal/daemon/backend.go`, add `"slices"` to the import block. In the `Backend` struct, directly below `Sampler *metrics.Sampler`, add:

```go
	// WebApplied is the web block the running listener started with. Only a
	// restart applies a change, so a reload that changes it warns.
	WebApplied config.Web
```

In `Reload`, replace

```go
	for _, w := range warnings {
		b.Events.Add("warn", "", "config: %s", w)
	}
```

with

```go
	if w := b.Store.Config().Web; w.Listen != b.WebApplied.Listen || !slices.Equal(w.Hosts, b.WebApplied.Hosts) {
		warnings = append(warnings, "web settings changed; restart ghr to apply")
	}
	for _, w := range warnings {
		b.Events.Add("warn", "", "config: %s", w)
	}
```

Run `timeout 60 gofmt -w internal/daemon/backend.go internal/daemon/backend_test.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 400 go test ./internal/daemon -v`
Expected: PASS, every daemon test.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "feat(daemon): warn when a reload changes web settings"
```

---

### Task 8: web: embed package

**Files:**
- Create: `web/embed.go`
- Create: `web/dist/.gitkeep` (empty)
- Create: `web/.gitignore`
- Modify: `.gitignore` (the line `dist/`)

**Interfaces:**
- Consumes: nothing.
- Produces: C6.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Confirm the placeholder is ignored today**

Run: `mkdir -p web/dist && timeout 10 touch web/dist/.gitkeep && git check-ignore -v web/dist/.gitkeep; echo "exit=$?"`
Expected: `.gitignore:1:dist/	web/dist/.gitkeep` and `exit=0`: the root rule `dist/` would keep the placeholder out of git, and a fresh clone would then fail `go build` on an empty embed.

- [ ] **Step 2: Write the implementation**

Create `web/embed.go`:

```go
// Package web holds the browser UI's build output, embedded into the ghr
// binary.
package web

import "embed"

// Dist is web/dist: the Vite build, or only .gitkeep in a build without the UI.
//
//go:embed all:dist
var Dist embed.FS
```

`web/dist/.gitkeep` already exists, empty, from Step 1.

Create `web/.gitignore`:

```
dist/*
!dist/.gitkeep
```

In the root `.gitignore`, replace the line `dist/` with `/dist/`.

Run `timeout 60 gofmt -w web/embed.go`.

- [ ] **Step 3: Verify the build and the ignore rules**

Run: `timeout 300 go build ./... && timeout 300 go vet ./web`
Expected: no output, exit 0 (the embed pattern matches `.gitkeep`).

Run: `git check-ignore -q web/dist/.gitkeep; echo "exit=$?"`
Expected: `exit=1` (the placeholder is no longer ignored).

Run: `timeout 10 touch web/dist/index.html && git check-ignore -v web/dist/index.html; rm web/dist/index.html`
Expected: `web/.gitignore:1:dist/*	web/dist/index.html`.

Run: `git check-ignore -v dist/ghr_linux_amd64.tar.gz`
Expected: `.gitignore:1:/dist/	dist/ghr_linux_amd64.tar.gz`.

- [ ] **Step 4: Commit**

```bash
git add web/embed.go web/dist/.gitkeep web/.gitignore .gitignore
git commit -m "feat(web): embed the web UI build output"
```

---

### Task 9: daemon: socket-only password reset route

**Files:**
- Create: `internal/daemon/web.go`
- Test: `internal/daemon/web_test.go`

**Interfaces:**
- Consumes: C3 (`webui.NewAuth`, `Auth.Reset`, `Auth.Valid`, `Auth.Setup`, `Auth.SetupRequired`), C5 (`webui.Handler`).
- Produces: C7 `socketHandler`.

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test**

Create `internal/daemon/web_test.go`:

```go
package daemon

import (
	"bytes"
	"crypto/rand"
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

func TestResetRouteIsSocketOnly(t *testing.T) {
	b, _, _ := newBackend(t)
	auth := webui.NewAuth(filepath.Join(t.TempDir(), "web-password"), time.Now, rand.Reader)
	id, err := auth.Setup("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	apiHandler := api.NewServer(b)

	web := httptest.NewServer(webui.Handler(auth, apiHandler, fstest.MapFS{}, nil))
	defer web.Close()
	req, _ := http.NewRequest(http.MethodPost, web.URL+"/api/web/reset-password", bytes.NewReader(nil))
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
	resp, err = sock.Client().Post(sock.URL+"/web/reset-password", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("over the socket: %d", resp.StatusCode)
	}
	if auth.Valid(id) {
		t.Fatal("the session survived the reset")
	}
	if req, err := auth.SetupRequired(); !req || err != nil {
		t.Fatalf("after reset: required %v err %v", req, err)
	}
	evs := b.Events.After(0)
	if len(evs) == 0 || evs[len(evs)-1].Level != "warn" ||
		!strings.Contains(evs[len(evs)-1].Msg, "web password reset; the next visitor to the web UI sets a new one") {
		t.Fatalf("events %+v", evs)
	}

	resp, err = sock.Client().Get(sock.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the socket no longer serves the API: %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 300 go test ./internal/daemon -run TestResetRouteIsSocketOnly -v`
Expected: FAIL to compile with `undefined: socketHandler`.

- [ ] **Step 3: Write the implementation**

Create `internal/daemon/web.go`:

```go
package daemon

import (
	"encoding/json"
	"net/http"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/webui"
)

// socketHandler serves the Unix socket: the control API plus routes that must
// stay unreachable from the web listener, which mounts the API handler alone.
func socketHandler(api http.Handler, auth *webui.Auth, ev *events.Ring) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /web/reset-password", func(w http.ResponseWriter, r *http.Request) {
		if err := auth.Reset(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		ev.Add("warn", "", "web password reset; the next visitor to the web UI sets a new one")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", api)
	return mux
}
```

Run `timeout 60 gofmt -w internal/daemon/web.go internal/daemon/web_test.go`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `timeout 400 go test ./internal/daemon -v`
Expected: PASS, every daemon test.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/web.go internal/daemon/web_test.go
git commit -m "feat(daemon): reset the web password over the socket"
```

---

### Task 10: CLI: ghr web reset-password

**Files:**
- Modify: `internal/api/client.go` (append after `AvailableRepos`)
- Modify: `cmd/ghr/cli.go` (`cli` switch, before `return usageError("unknown command " + args[0])`)
- Modify: `cmd/ghr/main.go` (`usage` constant)
- Test: `internal/api/api_test.go`, `cmd/ghr/cli_test.go`

**Interfaces:**
- Consumes: the socket route `POST /web/reset-password` (C7, Task 9).
- Produces: C8.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/api/api_test.go`:

```go
func TestResetWebPasswordPostsToTheSocketRoute(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Second}}
	if err := c.ResetWebPassword(context.Background()); err != nil || got != "POST /web/reset-password" {
		t.Fatalf("err %v request %q", err, got)
	}
}
```

Append to `cmd/ghr/cli_test.go`:

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
		if code != 2 || !strings.Contains(errOut, "usage: ghr web reset-password") {
			t.Errorf("%v: exit %d err %q", args, code, errOut)
		}
	}
	_, out, _ = runCLI(t, "", "help")
	if !strings.Contains(out, "web reset-password") {
		t.Fatalf("usage lacks the command:\n%s", out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/api ./cmd/ghr -run 'TestResetWebPasswordPostsToTheSocketRoute|TestWebResetPassword' -v`
Expected: FAIL to compile with `c.ResetWebPassword undefined`.

- [ ] **Step 3: Write the implementation**

Append to `internal/api/client.go`:

```go
// ResetWebPassword forgets the web UI password and ends its sessions. The
// route exists on the Unix socket only.
func (c *Client) ResetWebPassword(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/web/reset-password", nil, nil)
}
```

In `cmd/ghr/cli.go`, in the `cli` switch, directly below the `case "runner-update":` block (before `return usageError("unknown command " + args[0])`), add:

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

In `cmd/ghr/main.go`, in the `usage` constant, directly below the line `  runner-update [--cancel]        queue (or cancel) a runner update`, add the line (the description starts in column 35, like its neighbours):

```
  web reset-password              forget the web UI password
```

Run `timeout 60 gofmt -w internal/api/client.go internal/api/api_test.go cmd/ghr/cli.go cmd/ghr/main.go cmd/ghr/cli_test.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 400 go test ./... && timeout 300 go vet ./... && timeout 120 gofmt -l .`
Expected: every package passes, vet prints nothing, gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/api/client.go internal/api/api_test.go cmd/ghr/cli.go cmd/ghr/main.go cmd/ghr/cli_test.go
git commit -m "feat(cli): add ghr web reset-password"
```

---

### Task 11: daemon: web listener wiring

**Files:**
- Modify: `internal/daemon/run.go` (imports; `Options` at lines 25-43; `DefaultOptions` at lines 45-63; `Run` at lines 105-235)
- Test: `internal/daemon/run_test.go`

**Interfaces:**
- Consumes: C1 (`config.Web`), C3 (`webui.NewAuth`, `Auth.Check`), C5 (`webui.Handler`), C6 (`web.Dist`), C7 (`Backend.WebApplied`, `socketHandler`), C8 (`Client.ResetWebPassword`, in the test).
- Produces: C7 `Options.WebPasswordPath`.

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

Add `"io"` and `"net"` to the import block of `internal/daemon/run_test.go`, then append:

```go
// freeAddr returns a loopback address nothing listens on; validation rejects
// port 0, so the daemon cannot pick its own.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func testOptions(t *testing.T, cfg string) Options {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(cfg), 0o600)
	os.WriteFile(filepath.Join(dir, "token"), []byte("tok\n"), 0o600)
	dist := filepath.Join(dir, "dist", "2.330.0")
	os.MkdirAll(dist, 0o755)
	return Options{
		ConfigPath: cfgPath, TokenPath: filepath.Join(dir, "token"), WebPasswordPath: filepath.Join(dir, "web-password"),
		Socket: filepath.Join(dir, "ghr.sock"), HistoryPath: filepath.Join(dir, "history.jsonl"),
		ShutdownWait: 5 * time.Second,
		Paths: runner.Paths{Dist: dist, Instances: filepath.Join(dir, "instances"), Logs: filepath.Join(dir, "logs"),
			Pending: filepath.Join(dir, "pending"), ToolCache: filepath.Join(dir, "toolcache"), Hooks: "/opt/ghr/hooks", Home: "/home/ghrunner"},
		GitHubURL: fakeGitHub(t).URL, Systemd: &recSD{}, Docker: nopDocker{}, Host: dirHost{}, Reload: make(chan os.Signal, 1),
	}
}

func TestRunServesTheWebUI(t *testing.T) {
	addr := freeAddr(t)
	webBlock := "web:\n  listen: " + addr + "\n"
	o := testOptions(t, cfgYAML+webBlock)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}()
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })

	hc := &http.Client{Timeout: 10 * time.Second}
	call := func(method, path, body, session string) (*http.Response, string) {
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
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}

	resp, body := call(http.MethodGet, "/auth/state", "", "")
	if resp.StatusCode != 200 || !strings.Contains(body, `"setup_required":true`) {
		t.Fatalf("state: %d %s", resp.StatusCode, body)
	}
	if resp, _ := call(http.MethodGet, "/", "", ""); resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("the app response lacks the CSP")
	}
	if resp, _ := call(http.MethodGet, "/api/status", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("api without a session: %d", resp.StatusCode)
	}
	resp, body = call(http.MethodPost, "/auth/setup", `{"password":"correct horse battery"}`, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setup: %d %s", resp.StatusCode, body)
	}
	var session string
	for _, ck := range resp.Cookies() {
		if ck.Name == "ghr_session" {
			session = ck.Value
		}
	}
	resp, body = call(http.MethodGet, "/api/status", "", session)
	if resp.StatusCode != 200 || !strings.Contains(body, `"epoch"`) {
		t.Fatalf("api with a session: %d %s", resp.StatusCode, body)
	}
	if resp, _ := call(http.MethodPost, "/api/web/reset-password", "", session); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("reset over TCP: %d", resp.StatusCode)
	}
	if err := c.ResetWebPassword(context.Background()); err != nil {
		t.Fatal(err)
	}
	if resp, _ := call(http.MethodGet, "/api/status", "", session); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session after reset: %d", resp.StatusCode)
	}

	os.WriteFile(o.ConfigPath, []byte(cfgYAML+"web:\n  listen: "+freeAddr(t)+"\n"), 0o600)
	if ws, err := c.Reload(context.Background()); err != nil || !slices.Contains(ws, "web settings changed; restart ghr to apply") {
		t.Fatalf("reload with a new listen: %v %v", ws, err)
	}
	if resp, _ := call(http.MethodGet, "/auth/state", "", ""); resp.StatusCode != 200 {
		t.Fatalf("the listener moved on reload: %d", resp.StatusCode)
	}
	os.WriteFile(o.ConfigPath, []byte(cfgYAML+webBlock), 0o600)
	if ws, err := c.Reload(context.Background()); err != nil || len(ws) != 0 {
		t.Fatalf("reload back to the bound listen: %v %v", ws, err)
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
	done <- nil
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener outlived Run: %v", err)
	}
	l.Close()
}

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

	o = testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n")
	if err := os.MkdirAll(o.HistoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), o)
	if err == nil || strings.Contains(err.Error(), "web listener") {
		t.Fatalf("history unreadable: %v", err)
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the web listener was left open: %v", err)
	}
	l.Close()
}

func TestRunWithoutTheWebListener(t *testing.T) {
	o := testOptions(t, cfgYAML)
	if err := os.WriteFile(o.WebPasswordPath, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}()
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })
	evs, err := c.Events(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	warned, listening := false, false
	for _, e := range evs {
		if e.Level == "warn" && strings.Contains(e.Msg, "web password file is unreadable") {
			warned = true
		}
		if strings.Contains(e.Msg, "web UI listening") {
			listening = true
		}
	}
	if !warned || listening {
		t.Fatalf("warned %v listening %v in %+v", warned, listening, evs)
	}
	if err := c.ResetWebPassword(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(o.WebPasswordPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("password file after reset: %v", err)
	}
}

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

Also add `"errors"`, `"io/fs"` and `"slices"` to the import block of `internal/daemon/run_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 400 go test ./internal/daemon -run 'TestRunServesTheWebUI|TestRunClosesTheWebListenerWhenStartFails|TestRunWithoutTheWebListener|TestRunFailsWhenTheWebPortIsTaken' -v`
Expected: FAIL to compile with `unknown field WebPasswordPath in struct literal of type Options`.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/run.go`:

1. Add to the import block: `"crypto/rand"`, `"io/fs"`, `"sync"`, `"github.com/darkraise/ghr/internal/webui"` and `"github.com/darkraise/ghr/web"`.

2. In `Options`, directly below `HistoryPath string`, add:

```go
	// WebPasswordPath holds the web UI's password hash.
	WebPasswordPath string
```

3. In `DefaultOptions`, directly below `HistoryPath:  "/var/lib/ghr/history.jsonl",`, add:

```go
		WebPasswordPath: "/etc/ghr/web-password",
```

4. In `Run`, replace

```go
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	served := false
	defer func() {
		if !served {
			ln.Close()
		}
	}()
```

with

```go
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
```

5. Directly below

```go
	for _, w := range warnings {
		ev.Add("warn", "", "config: %s", w)
	}
```

add (unconditionally: the password file matters to `ghr web reset-password` and to the next start with the listener on)

```go
	if err := auth.Check(); err != nil {
		ev.Add("warn", "", "web: %v", err)
	}
```

6. In the `b := &Backend{` literal, directly below `Sampler: sampler,`, add `WebApplied: webCfg,`.

7. Replace

```go
	srv.Handler = api.NewServer(b)
	served = true
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("api server: %v", err)
		}
	}()
```

with

```go
	apiHandler := api.NewServer(b)
	srv.Handler = socketHandler(apiHandler, auth, ev)
	if webLn != nil {
		static, err := fs.Sub(web.Dist, "dist")
		if err != nil {
			return err
		}
		webSrv.Handler = webui.Handler(auth, apiHandler, static, webCfg.Hosts)
	}
	served = true
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("api server: %v", err)
		}
	}()
	if webLn != nil {
		go func() {
			if err := webSrv.Serve(webLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("web server: %v", err)
			}
		}()
		ev.Add("info", "", "web UI listening on %s", webLn.Addr())
	}
```

8. In the `case <-ctx.Done():` branch, replace

```go
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			srv.Shutdown(shutdownCtx)
			cancel()
```

with

```go
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
```

Run `timeout 60 gofmt -w internal/daemon/run.go internal/daemon/run_test.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 400 go test ./internal/daemon -v`
Expected: PASS, including the existing `TestRunServesTicksReloadsAndKeepsRunners`.

Run: `timeout 400 go test ./... && timeout 300 go vet ./... && timeout 120 gofmt -l .`
Expected: every package passes, vet prints nothing, gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/run.go internal/daemon/run_test.go
git commit -m "feat(daemon): serve the web UI on web.listen"
```
