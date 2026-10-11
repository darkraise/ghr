# Host Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close register rows 47 to 74 (except 52 and 63), 186, 203, 205 to 208, 210, 211, 213 and 214: a login lockout one LAN host cannot hold against another, CLI commands that say what really happened, a config loader that tolerates what it can repair, toolchain downloads pinned to their host, root-owned code that does not trust what a job can write, an installer that checks Docker's key and comes back with Docker, and a public repository blocked from the poll that shows it.

**Architecture:** Targeted fixes, package by package, and nothing is restructured. Five groups that share no production file (Task 3 also edits one test helper in the daemon package): the web server and CLI (`internal/webui`, `internal/api`, `cmd/ghr`), the config loader (`internal/config`), the toolchain installers and storage (`internal/toolchain`, `internal/storage`, the download client in `internal/system`), the installer, CI and README (`deploy/`, `.github/workflows/ci.yml`, `README.md`), and the daemon and runner manager (`internal/daemon`, `internal/runner`, `internal/metrics`, the `Run` type in `internal/github`).

**Tech Stack:** Go 1.26 (standard library plus `golang.org/x/term` and `gopkg.in/yaml.v3`, both already dependencies), bash for `deploy/setup.sh` and its test script, GitHub Actions YAML.

**Spec:** docs/superpowers/specs/2026-10-12-ghr-host-fixes-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 14 of 36 tasks are heavy (1, 3, 7, 11, 12, 14, 17, 19, 25, 26, 29, 31, 32, 33) and are delegated; the other 22 run in the session

**Plan review:** 2026-10-12 — dr-superpowers:judge-opus — executability 17 / coherence 18 / coverage 18 / assumptions 14 (round 2)

**Parallelism:** sequential — the security tasks must run alone, the fixes are small, and the dev host is the limit: `/tmp` is a nearly full tmpfs and the test suites time out under load

## Global Constraints

- Go is at `/usr/local/go/bin` and not on PATH: prefix every Go command with `PATH=/usr/local/go/bin:$PATH`. If the shell refuses that prefix, call `/usr/local/go/bin/go` and `/usr/local/go/bin/gofmt` by their absolute paths instead.
- `/tmp` is a small tmpfs that is nearly full. Before the first Go command of a session run `mkdir -p /root/.cache/ghr-plan-tmp`, and run every Go command with `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp` in front of it. A build that fails with `no space left on device` was run without them.
- Before each commit run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing) and the test command the task names.
- Go tests use the package's existing fakes and clocks. No new test calls `time.Sleep`. A `time.After` guard or a read deadline that only elapses when the test would otherwise hang is allowed.
- `deploy/setup.sh` installs Docker and system services: never run it. Its tests are `bash deploy/tests/setup_test.sh`, which sources the script and replaces its commands with fakes. A test run must never write under `/etc`: where a task begins by turning a path into a variable (`KEYRING_DIR`, `SYSTEMD_DIR`), do that step first, exactly as written, before any test is run.
- No write to GitHub (no registration, no run, no release) and no host change (no `systemctl`, no `apt`). Read-only `gh api` and `curl` GETs are fine.
- Nothing under `web/` changes. No new Go module dependency.
- Some agent shells refuse a command with a pipe or a `-run 'A|B'` pattern. Where a task gives a plain form beside such a command, use it; otherwise run the names one at a time.
- Error messages, event texts and CLI output are part of the spec. Copy them exactly as the task gives them.
- A task's "replace these lines" blocks were written against `master` at `8cf8ea2` plus the tasks before it in this plan. When a block to replace is not found exactly as quoted, find the same lines by their content, apply the same change, and say so in the task report; never skip the change.
- When an existing test fails because it asserted behaviour this plan changes, the task says so and gives the corrected lines. Report any other existing-test failure in the task report; never change production code to make an old expectation pass.
- Code comments: only where the reason is not obvious from the code. No comment mentions a task, a register row or a fix.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no trailing period, no attribution or co-author lines.
- Never deploy to the runner LXC (192.168.0.99).
- English only.

## Contracts

Names one task adds and a later task uses.

- Task 3: what a `Client` method in `internal/api/client.go` returns when it gets no answer, which Task 4's test pins as it reaches the terminal. The caller's context ended: that context's error itself, unwrapped (`context.Canceled` after a cancel). The call outlived its limit: a plain error, wrapping nothing, with the text `ghr daemon did not answer within <n>s; the request may still be running` (`<n>` is 30, or 100 for `Kill`). Anything else: `ghr daemon unreachable: <error>`, wrapping the cause. The first two also come back when the cut falls while a response is read, whatever its status; an `*api.Error` is returned only for an error response that arrived in full or ended on its own.
- Task 3: `internal/api/client.go` gains `callLimit` (30 s), `killLimit` (100 s) and `func (c *Client) callWithin(ctx context.Context, limit time.Duration, method, path string, body io.Reader, out any) error`. A later client method that needs more than 30 seconds calls `callWithin` with its own limit.
- Task 3: `api.NewUnixClient` returns a client whose `HTTP.Timeout` is 0. Code that calls `client.HTTP.Do` directly has no time limit unless its request carries a context deadline. `socketCall` in `internal/daemon/run_test.go`, the one such caller in the tree, gets a 30-second deadline in the same task.
- Task 3: the `"testing/synctest"` import in `internal/api/api_test.go`, used by Task 5.
- Task 4: `run` in `cmd/ghr/main.go` prints `ghr: interrupted` to stderr and exits 1 for any error for which `errors.Is(err, context.Canceled)` holds. Task 7 returns such an error from `askPassword`; `waitConfigured` in `cmd/ghr/setup.go` already does. Any later CLI task that returns a cancelled context's error gets this text.
- Task 4: `var interruptContext func() (context.Context, context.CancelFunc)` in `cmd/ghr/main.go`, the context `run` hands to `cli`. A later test that drives `run` and needs to cancel it replaces this variable.
- Task 4: the `"testing/synctest"` import in `cmd/ghr/main_test.go`.
- Task 5: `const killWriteWindow = 100 * time.Second` in `internal/api/server.go`. The kill route applies it only when the request's context holds an `*http.Server` (under `http.ServerContextKey`) whose `WriteTimeout` is above zero. A later route that needs a longer write deadline on the web listener and none on the socket can use the same test.
- Task 5: in `internal/api/api_test.go`, the field `onKill func()` of `fakeBackend`, the type `deadlineWriter`, and `servedBy(srv *http.Server, method, path string) *http.Request`, which builds a request as that server would hand it to a handler.
- Task 7: the rule that only `askPassword` changes the terminal's mode. `readPassword` (`var readPassword func() ([]byte, error)` in `cmd/ghr/password.go`) now only reads a line: its default is `readHidden(os.Stdin)`, and it must never change a terminal mode. No other code calls it.
- Task 7: `var hideInput func() (func(), error)` in `cmd/ghr/password.go`, which hides the terminal's input and returns the function that puts the terminal back; `func hideTerminal(f *os.File) (func(), error)` and `func readHidden(in io.Reader) ([]byte, error)`, its real parts; `var errInterrupted = errors.New("interrupted")`, returned for Ctrl-C or Ctrl-D typed at a prompt.
- Task 7: `stubPasswordInput` in `cmd/ghr/cli_test.go` now replaces `hideInput` too. A later test that calls `askPassword`, `readToken` or `readNewPassword` on the terminal path without `stubPasswordInput` must replace `hideInput` itself. `openPTY(t)` in `cmd/ghr/password_linux_test.go` returns both ends of a pseudo-terminal (Linux only).
- Task 7: the `"slices"`, `"sync"` and `"testing/synctest"` imports in `cmd/ghr/cli_test.go`.
- Task 1: `maxPrefixFailures = 50` and `func prefixKey(key string) (string, bool)` in `internal/webui/auth.go`; `ClientKey` now returns an IPv6 client's address, not its /64. The test helpers `v6`, `v6Prefix` and `failureCounters` are in `internal/webui/auth_test.go`.
- Task 2: `authBodyLimit` is `16 << 10`; `/auth` answers 405 `{"error":"method not allowed"}` with `Allow`, and 413 `{"error":"request body too large"}`.
- Task 8 adds `config.MaxLabelLength = 100` (exported constant, `internal/config/config.go`).
- Task 8 adds unexported helpers in `internal/config/config.go`: `(*Config).dropBadLabels() []string`, `labelFault(l string) string`, `labelProblem(labels []string) string`. No later task calls them.
- Task 8, `Validate` error texts (each is joined with the others by `; ` and sorted; only the first offender of each list is named; `<repo>` is the repository's name as written):
  - `labels: "a b" must not contain spaces or commas` (a comma or any Unicode whitespace)
  - `labels: " " must not be empty` (empty after trimming; the label is quoted as written)
  - `labels: "<label>" must be at most 100 characters` (counted in characters, not bytes)
  - `labels: "linux" is added by ghr` (any of `self-hosted`, `linux`, `x64`, ignoring case; the label is quoted as written)
  - `labels: "Docker" is listed twice` (names the later spelling; compared ignoring case after trimming)
  - the same five with the prefix `<repo>: ` for a repository's `labels`, e.g. `darkcloud: labels: "linux" is added by ghr`
- Task 8, `Parse` warning texts (one per dropped label, in this order: runner-cap warnings, then global labels, then each repository in file order, then `Validate`'s own warnings):
  - `labels: "a b" is not a valid label; ignored` (any label that fails the rules above, including a system label)
  - `labels: "docker" is listed twice; ignored` (a label equal ignoring case to an earlier one in the same list; the first one stays)
  - the same two with the prefix `<repo>: ` for a repository's `labels`, e.g. `darkcloud: labels: "x,y" is not a valid label; ignored`
- Task 8 changes `Parse`'s warning list: a load with an invalid label now returns warnings it did not before. The daemon records each warning as an event; a later task that remembers load warnings (spec 3.4) should expect these strings alongside `global_max 500 is above 99; using 99`.
- Task 9 adds no names.
- Task 10 adds unexported names in `internal/config/config.go`: `syncFile` (a package variable, `= (*os.File).Sync`, the one seam; tests replace it and restore it with `t.Cleanup`), `saveTarget(path string) (string, error)` and `maxLinkHops = 8`. No later task calls them. Task 10 also adds the test helpers `saveOwner`, `isSymlink` and `stubSync` to `config_test.go`; a later task that adds a test there may use them.
- Task 10 changes `Save`'s behaviour: a symbolic link at the path stays and the file at the end of the chain is written (created when missing); a loop, more than 8 links, or a missing directory on the way returns an error and writes nothing; a failing sync returns the error, keeps the old file and removes the temporary file.
- Task 11: `type Origin struct{ Dir, File string }` in `internal/toolchain/toolchain.go`. Exactly one field is set: `Dir` is a directory rule and must end in `/`; `File` is an exact-file rule. Node, Python, Java and Go pass `Origin{Dir: ...}`; .NET passes `Origin{File: e.Sources.DotnetScript}`.
- Task 11: `Env.Allow func(url string, from Origin) error` in `internal/toolchain/toolchain.go`. `NewEnv` sets it to `httpsFrom`; an `Env` built by hand must set it, or an install panics on the nil func. No caller of `NewEnv` changes.
- Task 11: `(*Env).fetch(ctx context.Context, from Origin, rawURL, dst string) error` in `internal/toolchain/layout.go`, the only caller of `Env.Fetch`. It returns `Allow`'s error as it is and wraps a `Fetch` error as `download: <err>`.
- Task 11: `(*Env).installArchive(ctx context.Context, toolDir string, from Origin, rel Release, progress func(string)) error` (new third parameter `from`).
- Task 11: `httpsFrom(rawURL string, from Origin) error` in `internal/toolchain/env.go`; the constants `nodeReleases` (`node.go`) and `pythonReleases` (`python.go`); `javaReleases(version string) string` (`java.go`).
- Task 11: the error text `refusing to download <url>`, returned unwrapped by every installer's `Install`.
- Task 12: `system.HTTPSRedirect(req *http.Request, via []*http.Request) error` in `internal/system/system.go`, an `http.Client.CheckRedirect`; its refusal reads `refusing a redirect to <scheme>://<host>`. Task 13 uses it. Also the unexported `downloadClient` in `internal/system`.
- Task 13: the unexported `getClient` in `internal/toolchain/env.go`; `NewEnv`'s `Extract` runs `tar --no-same-owner -xzf <archive> -C <dir>`.
- Task 17: `openOwned(path string) (*os.Root, error)` in `internal/toolchain/layout.go`.
- Task 18: `type inode struct{ dev, ino uint64 }` in `internal/storage/measure.go`; `hardLink(fi os.FileInfo) (inode, bool)` in `internal/storage/own_unix.go` and `own_other.go`.
- Task 13 uses `system.HTTPSRedirect` from Task 12. No other task of this chain uses a name from an earlier one. Beyond that the order matters because Tasks 13 to 17 edit files that Task 11 changes (`env.go`, `python.go`, `golang.go`, `java.go`, `layout.go`, `hardening_test.go`), and their quoted lines are those files as Task 11 leaves them.
- `KEYRING_DIR` (Task 19): variable in `deploy/setup.sh`, default `/etc/apt/keyrings`; `write_docker_source` writes `Signed-By: $KEYRING_DIR/docker.asc`; the test script sets it to `$T/keyrings` for the whole run and `new_case` to `$T/<case>/keyrings`, `DEFAULT_KEYRING_DIR` keeps the script's value, and `run` refuses to start a function while it is outside `$T`.
- `DOCKER_KEY_FINGERPRINT` (Task 19): variable in `deploy/setup.sh`, `9DC858229FC7DD38854AE2D88D81803C0EBFCD88`.
- `export_docker_key <dir>` and `install_docker_key` (Task 19): functions in `deploy/setup.sh`; the die message is `Docker apt key has an unexpected fingerprint`.
- `SYSTEMD_DIR` (Task 20): variable in `deploy/setup.sh`, default `/etc/systemd/system`; `install_service` writes `$SYSTEMD_DIR/ghr.service` and looks for `$SYSTEMD_DIR/docker.service.wants/ghr.service`. The test script sets it to `$T/systemd` for the whole run and `new_case` to `$T/<case>/systemd`, `DEFAULT_SYSTEMD_DIR` keeps the script's value, and `run` now checks both directories.
- `install_service` systemctl calls (Task 20), in this order: `--no-reload reenable ghr.service`; then `daemon-reload`, only when the unit file differed or the `docker.service.wants` link was missing; then `restart ghr.service`.
- `$STATE_DIR/web-password-pending` (Task 21): marker file created by `install_config`, removed by `set_web_password` after a password is set or after a read that succeeded and returned an empty line (not on end of input or a failed read).
- `FIRST_INSTALL` (removed by Task 21): nothing may read it afterwards.
- `main` order in `deploy/setup.sh` (Task 22): `install_runner`, `gc_dist`, `install_ghr`, `install_config`, `install_service`, ..., `set_web_password`, `preinstall_toolchains`, `print_next`.
- `daemon.SaveError` in `internal/daemon/store.go`: `type SaveError struct{ Err error }` with `Error() string` (`"save config: " + Err`) and `Unwrap() error`; `Store.Update` returns it when `config.Save` fails. Task 24. Task 25 quotes the line that builds it.
- The answer `500 could not save config.yaml` from `Backend.update`, built as `&api.Error{Status: http.StatusInternalServerError, Msg: "could not save config.yaml"}`; the log line `save config: <error>`. Task 24. Any web or CLI task that asserts on a save failure sees this status and text.
- `func (s *Store) Reload() (warnings, added []string, tokenChanged bool, err error)` in `internal/daemon/store.go`. Task 25. It has one more return value than before, `added`: the warnings of this load that were neither in the load before nor in the config saved since, worked out under the store's lock. Every caller is in `internal/daemon` and is changed by Task 25: `OpenStore`, `Backend.Reload`, the not-configured loop in `Run`, and ten calls in `store_test.go`. The store has no method that reads the remembered list; the field is `Store.warnings`.
- `internal/daemon/limitlistener.go`: `limitConns(l net.Listener, total, perClient int) *limitListener`, `clientKey(remote net.Addr) string`, `maxWebConns = 256`, `maxWebConnsPerClient = 64`. Task 26. In `Run`, `webLn` is now a `*limitListener` held as a `net.Listener`; connections the web server sees are `*limitConn`, which embeds `net.Conn`. A task that sets a write deadline through `http.ResponseController` on a web route (spec 1.2) is not affected: `SetWriteDeadline` is promoted from the embedded connection.
- Test helpers in package `daemon` tests: `textAddr`, `scriptedConn`, `scriptedListener`, `newScriptedListener`, `acceptLoop`, `endLoop`, `closeTwiceAtOnce` (`limitlistener_test.go`). Task 26. Names another drafter's test in `internal/daemon` must not reuse.
- `fakeManager.cleared` in `internal/daemon/backend_test.go` is an `atomic.Bool`: read it with `m.cleared.Load()`, set it with `m.cleared.Store(...)`. Task 25. A later test in `internal/daemon` that reads the flag must use `Load()`.
- The error text `could not check the token` and the method `(*Backend).checkTokenSafely`. Task 27.
- `internal/daemon/joblogs.go`: `logSettle = 2 * time.Minute`, `logSettleTTL = 15 * time.Second`, `completionTime(t *time.Time) time.Time`, the field `logJob.completedAt`, and `logMiss.until` in place of `logMiss.at`. Task 28.
- `internal/runner/manager.go`: `readJobJSON(path string, v any) error`, `maxJobFile = 1 << 20`. Task 29. Test helpers `mustReturn` and `mkfifo` in `internal/runner/jobfile_unix_test.go`.
- `internal/github/client.go`: `type RunRepository struct{ Private *bool }`, the field `Run.Repository RunRepository` (JSON `repository`), `func (r Run) RepoPublic() bool`. Task 30. Task 31 calls `RepoPublic` and its test builds a `RunRepository`.
- `func (m *Manager) sawPublic(repo string, now time.Time)` in `internal/runner/visibility.go`, placed directly above `blockedLocked`, and the field `listedAt time.Time` of the `visibility` struct. Task 31. Task 32 adds two fields to the same struct, replaces the end of `readVisibility` as Task 31 left it, and places its method directly above `sawPublic`. Test helper `listedRun` in `internal/runner/blocked_test.go`.
- `func (m *Manager) RecordVisibility(repo string, public bool)` on `*runner.Manager`, in `internal/runner/visibility.go`, placed directly above `sawPublic`; the fields `rev uint64` and `handed bool` of the `visibility` struct. Task 32. Task 33 calls it. Its contract: hand an answer over only for a repository that is being added, and before the config that holds it can be seen; the manager keeps the answer until the repository has shown up in the config once.
- `func (s *Store) UpdateThen(fn func(c *config.Config) error, saved func()) ([]string, error)` in `internal/daemon/store.go`: `Update` with a step that runs once `config.yaml` holds the change and before `Store.Config` shows it, with the store's lock held; `Update(fn)` is now `UpdateThen(fn, nil)`. `(*Backend).updateThen(fn, saved)` in `internal/daemon/backend.go` is the same for `update`. Task 33.
- `RecordVisibility(repo string, public bool)` as the last method of the `Manager` interface in `internal/daemon/backend.go`; `fakeManager` in `internal/daemon/backend_test.go` gains `RecordVisibility`, `vmu`, `visibility` and `onVisibility`. Task 33. Any other type that must satisfy `daemon.Manager` needs the method; at the base commit only `*runner.Manager` and `fakeManager` do.
- `func (m *Manager) prune(cfg *config.Config, now time.Time) (removed, ok bool)` in `internal/runner/cleanup.go` (one `bool` before). Task 34.

## Assumptions (evidence)

- Every task's code was carried out red-then-green in a drafter's worktree on local `master` (`8cf8ea2`, or `76066ff`, which differs only in docs), one commit per task except where a task says it makes two, in five chains that share no production file: Tasks 1 to 7, 8 to 10, 11 to 18, 19 to 23, 24 to 35. Each drafter checked its quoted blocks against the commit before each task. Evidence: the drafters' reports of 2026-10-12, recorded in docs/superpowers/notes/2026-10-12-ghr-host-fixes-plan-draft.md.
- The five chains merge without conflict. Evidence: on 2026-10-12 the five branches were merged into one worktree from `master`; `gofmt -l`, `go vet ./...`, `GOOS=windows go vet ./...`, `go test -race -count=1 -timeout 480s ./...` (22 packages) and `bash deploy/tests/setup_test.sh` passed there. The run was made on the final branches, after four tasks had been split in two and the findings of the first plan review had been fixed.
- `internal/daemon/run_test.go` is edited by Task 3 (the `socketCall` helper only) and by tasks of the last chain elsewhere in the file; the hunks do not overlap. Evidence: the merge above.
- GitHub's maximum length for a runner label is unverified and cannot be checked without registering a runner. The rule in Task 8 keeps the web form's 100 characters; a label GitHub refuses fails the spawn with GitHub's own error, as today. No task verifies it: spec section 7 records the decision.
- A config file whose only labels are system labels stops loading after Task 8, by the existing rule `needs at least one label in labels or repo labels`. This is an accepted consequence recorded in spec section 3.1; the owner was asked on 2026-10-12 and may reverse it.
- The three pinned actions in Task 23 are the commits their tags name, in the `actions` organisation's own repositories. Evidence, 2026-10-12: `gh api repos/actions/checkout/git/ref/tags/v4.4.0` gives `11d5960a326750d5838078e36cf38b85af677262`, `gh api repos/actions/setup-go/git/ref/tags/v5.6.0` gives `40f1582b2485089dde7abd97c1529aa768e1baff`, `gh api repos/actions/setup-node/git/ref/tags/v4.4.0` gives `49933ea5288caeca8642d1e84afbd3f7d6820020`, and `gh api repos/actions/<name>/commits/<sha>` answers each from that repository.
- With `Requires=` and `After=` alone a unit stays inactive after its required unit is stopped and started; also wanted by that unit, it starts again, and on a restart too. Evidence: two throwaway units on Debian 13 with systemd 257, 2026-10-12 (spec section 7).
- One score differs from what its drafter proposed. Task 11's coupling is 1, not 2: no later task uses `Env.Allow`, `Env.fetch` or `Origin`.
- Task 7 puts the password prompt's terminal in raw mode, with the consequences spec section 2.3 lists. The owner was told on 2026-10-12 and may choose per-system terminal code instead; the plan keeps raw mode until they say so. Nobody has typed at a real interactive terminal: a pseudo-terminal test proves the state is restored, and it skips where `/dev/ptmx` is missing.
- That `systemctl --no-reload reenable` makes no implicit reload is taken from systemd's documentation, not from a run on a host (Task 20's tests check the recorded commands). The live check in spec section 8 settles it.
- Entries below marked unverified that no task settles are accepted risks, not open work: the 90-second unit stop, the prompt at a real keyboard, `/dev/ptmx` on the CI runner, whether a runs listing's ETag changes with visibility, `--no-reload reenable` on a live systemd, `shellcheck`, and a permission error while `Save` resolves a link. Each is covered by the owner's live check on the runner LXC, which spec section 8 describes and which no task performs because a deploy needs the owner's yes.
- `ClientKey` is called in one place outside tests, `internal/webui/handler.go:228`, and `Auth.Login` is called outside the package only by `internal/daemon/web_test.go:134`, with an IPv4 key (`rg -n '\.Login\(|ClientKey\(' --glob '*.go'`, 2026-10-12). Both signatures are unchanged.
- An IPv4-mapped remote address is turned into its IPv4 address before it becomes a key: `remoteAddr` ends with `ip.Unmap().WithZone("")` (`internal/webui/auth.go:338` at `8cf8ea2`).
- `login` checks the lockout three times, at `internal/webui/auth.go:135`, `:155` and `:164` at `8cf8ea2`, each under `a.mu`, and reads the clock right before each. Task 1's `TestALockLandingMidLoginRefusesIt` relies on exactly these three clock reads.
- `failWindow` and `lockout` are both 15 minutes (`internal/webui/auth.go:19-20`), so a lock ends at the moment its last counted failure leaves the window, and `sweepLocked` (`auth.go:200-216` at `8cf8ea2`, unchanged) then removes the key.
- The static handler already answers a wrong method with `Allow: GET, HEAD` and the text `method not allowed` (`internal/webui/static.go:20-21`); Task 2 uses the same header value for `/auth/state` and the same words.
- Go's `ServeMux` serves `HEAD` from a `GET` pattern: `TestRoutes` now asserts `HEAD /auth/state` answers 200 (run green on 2026-10-12).
- The toolchain is Go 1.26.1 and `go.mod` says `go 1.26.0` (`go version`, 2026-10-12), so `testing/synctest` is available without a new dependency. Nothing in the tree used it before (`rg -n synctest --glob '*.go'` at `8cf8ea2` printed nothing).
- Before Task 3 no call through `api.NewUnixClient` could outlive 30 seconds: `http.Client.Timeout` covered the whole exchange (`internal/api/client.go:31` at `8cf8ea2`). `ghr logs -f` polls one short `Log` call a second (`cmd/ghr/cli.go:238-262`), and `ghr setup github` polls `SetupState` (`cmd/ghr/setup.go:123-137`). No client method streams. So a 30-second limit per call keeps every caller's behaviour, and only `Kill` gets longer.
- `socketCall` (`internal/daemon/run_test.go:601-615` at `8cf8ea2`) is the only code that calls `.HTTP.Do` on a client from `NewUnixClient` (`rg -n 'NewUnixClient|HTTP\.Do' --glob '*.go'`, 2026-10-12: line 608 is the one hit with both). Its file is identical on `master` and at the Task 2 commit, so the lines Task 3 quotes are `master`'s. `context` and `time` are already imported there (lines 5 and 23). The two other raw clients in that file set their own 10-second `Timeout` (lines 310 and 638).
- `main.run` prints `ghr: <error>` and exits 1, or 2 with the usage for a `usageError` (`cmd/ghr/main.go:75-86` at `8cf8ea2`). `logs` returns nil when its context has ended (`cmd/ghr/cli.go:241`), so Ctrl-C in `ghr logs -f` still exits 0 without a message.
- The web listener's `WriteTimeout` is 60 seconds and the socket's server sets none: `internal/daemon/run.go:284` is `srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}`, and lines 286-287 give the web server `WriteTimeout: webWriteTimeout` (`run.go:42`, 60 s). Nothing between the web listener and the API handler wraps the `http.ResponseWriter` (`rg -n 'Unwrap\(\) http.ResponseWriter|http.ResponseWriter$' internal --glob '!*_test.go'` printed nothing, 2026-10-12), so `http.NewResponseController` reaches the connection.
- Every request a Go server hands to a handler carries that `*http.Server` in its context under `http.ServerContextKey` (documented on the key in `net/http`). On the web path the value survives the wrappers: `requireSession` derives its context from the request's (`internal/webui/handler.go:148` and `:175` at `8cf8ea2`) and `http.StripPrefix` keeps it. Read from the code; Task 5's test puts the server into the context itself and does not go through `internal/webui`.
- Go clears a connection's write deadline after every response: `c.rwc.SetWriteDeadline(time.Time{})` at `/usr/local/go/src/net/http/server.go:2081` (Go 1.26.1). The kill route's longer deadline therefore cannot reach a later request on the same connection.
- golang.org/x/term v0.46.0, read on 2026-10-12: `term.ReadPassword` changes the mode, reads and changes it back inside one call (`term_unix.go:74-91`), so a caller cannot order its own restore after that mode change. `term.MakeRaw` clears `ECHO` and `ISIG` among others (`term_unix.go:34`), so in raw mode nothing typed is shown and Ctrl-C is a byte. `term.Terminal` returns `io.EOF` for Ctrl-C and for Ctrl-D on an empty line (`terminal.go:827-833`), ends a line at a carriage return or a line feed (`terminal.go:582`) and holds lines of up to 4,096 characters (`terminal.go:366`). The package has no call that turns echo off and leaves signals on.
- `cmd/ghr` builds for darwin, freebsd and windows today (`GOOS=<os> go build ./cmd/ghr`, 2026-10-12, before and after Task 7). Turning echo off with the terminal's own ioctls would need golang.org/x/sys imported directly, which `go.mod` lists as indirect, and one file per system family; `term.MakeRaw` and `term.Restore` exist for all of them.
- The pseudo-terminal test ran on the dev host and did not skip (`go test ./cmd/ghr/ -run TestPasswordPromptOnARealTerminal -v`, 2026-10-12: three rows `PASS`). With `restore()` removed from `askPassword` both it and the table test failed (mutation run, 2026-10-12).
- `flag.FlagSet.Parse` stops at the first argument that is not a flag and may be called again on the rest; Task 6's `repo add` relies on both, and its tests cover flags before the name, after it, and on both sides (run green on 2026-10-12).
- No ghr daemon runs on the dev host (`ls /run/ghr` failed, 2026-10-12). The new CLI tests replace `newClient` anyway, so none of them can reach a real socket.
- `unverified`: that a runner stop really takes up to 90 seconds. The spec reads it from the code (no `TimeoutStopSec` on runner units). Timing one `ghr kill` of a busy runner on the runner LXC would settle it; that needs the owner's yes.
- `unverified`: the prompt at a real keyboard. On a pseudo-terminal the state after the prompt equals the state before it for a typed line, for Ctrl-C typed in raw mode and for a cancel before the read starts. Nobody typed at an interactive terminal: that nothing typed is shown, that Backspace works and that the shell prompt looks right afterwards. Running a built `ghr web set-password` in a terminal, typing, and pressing Ctrl-C at the second prompt would settle it.
- `unverified`: that CI's runner has `/dev/ptmx`. Without it the pseudo-terminal test skips and says so.
- `unverified`: whether the test binary's stdin can be a terminal under `go test`. The token and password tests replace `stdinIsTerminal`, so they do not depend on the answer.
- `Validate` already rejects `global_max > MaxRunners`, a repository `max` or `warm` above it (`internal/config/config.go`, lines near 340 to 380 at `8cf8ea2`); the label checks are added next to them and do not depend on them.
- `Parse` runs `dropBadLabels` after `clampRunners` and before `Validate`; `Validate` runs on the repaired config, so a repaired file saves and loads again (`TestParseDropsInvalidLabels`, ran green 2026-10-12).
- Every `Decode` of a document-less file returns `io.EOF`: `TestParseEmptyFileLoadsDefaults` failed with `parse config: EOF` for an empty, a blank and a comments-only input before the fix (2026-10-12).
- A second `Decode` after an `io.EOF` first one also returns `io.EOF`, so the "expected exactly one YAML document" check stays correct for an empty file (the three inputs load as defaults, test green 2026-10-12).
- `saveTarget` resolves the directory part with `filepath.EvalSymlinks` at every hop, so a `..` in a relative link is joined onto a real directory; a `path` that does not exist yet (the first save) is returned unchanged (`TestSaveRoundTrip` and `TestSaveRejectsInvalid` stay green; 2026-10-12).
- A dangling link, a chain of links (relative, then absolute) and an existing target all keep every link and write the file at the end; a link loop and a link into a missing directory return an error and leave the directory as it was. All four tests ran red against the old `Save` and green against `saveTarget` on 2026-10-12.
- A resolution failure other than "the last file does not exist" (for example a permission error from `Lstat`) takes the same error branches; I could not test a permission error because the dev host runs as root. Unverified; a test as a non-root user would settle it.
- The sync order and failure handling are proved through `syncFile`: the test reads `config.yaml` and the temporary file at the moment the stub runs (old content in the first, new content in the second), and a failing stub leaves the old file and only one directory entry (2026-10-12; red with the seam declared and not called: `sync ran 0 times, want 1`). That the real `(*os.File).Sync` is what runs is the variable's initial value, read in the code.
- Other packages' tests that use labels pass with the rules: `go test -count=1 -timeout 300s ./...` ran green on 2026-10-12 after Task 10 (list below).
- The 100-character limit is the web form's; GitHub's own limit is not verified (spec 3.1 and section 7).
- Every `download_url` in the Node manifest (1473) begins `https://github.com/actions/node-versions/releases/download/`, every one in the Python manifest (2392) begins `https://github.com/actions/python-versions/releases/download/`, and none contains `?`, `#`, `%`, `\`, `..` or a doubled slash: `curl` of both `versions-manifest.json` files with `jq`, run 2026-10-12.
- Adoptium's package link for JDK 8, 11, 17, 21, 24 and 25 begins `https://github.com/adoptium/temurin<major>-binaries/releases/download/`, where `<major>` equals the first part of `version_data.semver` (for example `8.0.504+1` and `temurin8-binaries`). The links for 11 to 25 contain `%2B`, an encoded `+`, which is why `httpsFrom` allows escapes other than `%2f`, `%2e` and `%5c`: `curl` of `api.adoptium.net/v3/assets/feature_releases/<major>/ga?...` for those six majors, run 2026-10-12.
- `Release.Version` of a Java release is `version_data.semver`, so its part before the first `.` is the major: `internal/toolchain/java.go:74-78` (as found).
- The Go and .NET download URLs are built from constants ghr sets, not from list data: `Sources.GoDownload + name` at `internal/toolchain/golang.go:29` and `URL: d.e.Sources.DotnetScript` at `internal/toolchain/dotnet.go:78` (as found); `DefaultSources` at `internal/toolchain/env.go:15-26`.
- .NET downloads exactly one file, the configured script, and nothing below or beside it: `Resolve` sets `URL: d.e.Sources.DotnetScript` for every release (`internal/toolchain/dotnet.go:78`, as found) and `Install` fetches only `rel.URL`. So its rule is an exact match, not a prefix. Every other installer's URL lies below a directory whose configured or constant URL ends in `/`: `nodeReleases`, `pythonReleases`, `javaReleases` (Task 11) and `GoDownload: "https://go.dev/dl/"` (`internal/toolchain/env.go:21`, as found; its field comment at `internal/toolchain/toolchain.go:32` already says "ending in "/"").
- A string prefix without its closing slash also matches sibling paths (`/dl` is a prefix of `/dlx/`, and `.../dotnet-install.sh` of `.../dotnet-install.sh.other`). Before the two rules were separated, the check passed `dotnet-install.sh.other`, `dotnet-install.sh/another-script` and, with `GoDownload` set to `https://go.dev/dl`, `https://go.dev/dlx/...`: the rows added for them failed with `got download: fetched, want it refused`. Run 2026-10-12.
- The version lists (`Sources.NodeManifest`, `GoManifest`, `PythonManifest`, `GoReleases`, `Adoptium`, `DotnetIndex`) are constants ghr sets and are read by `Env.Get`, never by `Env.Fetch`, so they need no allowlist. The no-plain-http-redirect rule of Task 12 covers `Get` too, through Task 13: `NewEnv` sets `Get: httpGet` (`internal/toolchain/env.go:38`, as found) and `httpGet` uses `getClient` after Task 13, pinned by `TestHTTPGetFollowsARedirectOnlyToHTTPS`.
- Redirect targets are left unpinned by host on purpose. The first URL is authenticated by TLS to the pinned origin, and an origin that can redirect anywhere can equally serve any content itself, so pinning the target adds nothing; and GitHub has changed its asset host name before, so a pinned name would break installs. Observed on 2026-10-12 with `curl -sIL`: `https://go.dev/dl/go1.20.linux-amd64.tar.gz` redirects once to `https://dl.google.com/go/...`; the Temurin 21 package link redirects once to `https://release-assets.githubusercontent.com/...`; `dotnet-install.sh` and the three kinds of version list answer without a redirect.
- `os.OpenRoot` follows a symlink at the path it is given ("It follows symbolic links in the directory name", `/usr/local/go/src/os/root.go:80`, Go 1.26.1), and `Root.OpenRoot` follows one that stays inside the root (`os/root_unix.go`, `openRootInRoot`). Hence `openOwned` compares `Root.Stat(".")` with `os.Lstat(path)`. `TestOpenOwnedOpensOnlyTheDirectoryItself` fails without the comparison (Task 17, Step 6).
- `Root.Chmod(".", mode)` and `Root.Stat(".")` act on the opened directory itself, and on Linux `Root.Chmod` uses the plain `fchmodat` call, so it needs no recent kernel: `/usr/local/go/src/internal/syscall/unix/at_sysnum_linux.go:14` and `os/root_unix.go:177-183`; `TestInstallArchiveTmpModes` passes with the new `opDir` (mode `0711` on `.tmp`, `0700` on the operation directory).
- The fixture's `OpID` is called by `opDir` after the look at `.tmp` and before `Mkdir`: `internal/toolchain/layout.go:104` (as found). The new `opDir` keeps that order.
- The tool cache is created `0755`, owned by the runner user, with no sticky bit: `deploy/setup.sh:83`. So a job can rename or replace `.tmp` and any tool folder.
- Storage operations run one at a time: `internal/storage/queue.go:55-56`. Two installs never share `.tmp` at once.
- The go-versions manifest lists `1.21.0`, `1.20.0` and `1.19.0`, and every first release down to `1.12.0`, with three parts; go.dev names `go1.21.0.linux-amd64.tar.gz`, `go1.20.linux-amd64.tar.gz` and `go1.19.linux-amd64.tar.gz`: `curl` of the manifest and of `https://go.dev/dl/?mode=json&include=all`, run 2026-10-12.
- `Host.Extract` in `internal/system` already runs `tar --no-same-owner -xzf`: `internal/system/host_test.go:24` (as found). Task 13 uses the same argument order, and `TestNewEnvExtractsWithRealTar` runs it with the real `tar`.
- No test in the tree relies on `system.Download` or `httpGet` following a redirect to `http`: `rg -n 'http\.Redirect|StatusFound' --type go -g '*_test.go'` finds only `internal/github/download_test.go`, which uses the GitHub client. Run 2026-10-12.
- No test in the tree calls `t.Parallel()` (same `rg`, 2026-10-12), so the two redirect tests may replace `http.DefaultTransport` for their duration.
- `httptest.NewTLSServer` servers share one built-in certificate, so the client of one trusts another; `TestDownloadFollowsARedirectOnlyToHTTPS` follows a redirect from one TLS server to a second one and passes. Run 2026-10-12.
- `syscall.Stat_t` has `Dev`, `Ino` and `Nlink` on every Unix target checked, with differing integer types that the code converts: `go vet ./internal/storage/` passes for `linux/amd64`, `linux/arm64`, `darwin` and `windows`. Run 2026-10-12.
- Unverified: whether a filesystem with transparent compression would let Task 18's test pass before the fix. The test writes random bytes to avoid that; running it on such a filesystem would settle it.
- Docker's apt key has the primary fingerprint `9DC858229FC7DD38854AE2D88D81803C0EBFCD88`, with the subkey `D3306A018370199E527AE7997EA0A9C3F273FCD8` as a second `fpr` line. Evidence: `curl -fsSL https://download.docker.com/linux/debian/gpg -o key.asc` then `gpg --show-keys --with-colons key.asc | rg '^fpr'`, 2026-10-12, printed exactly those two `fpr` records and one `pub` record.
- `gpg --import` into a fresh `--homedir` starts a `gpg-agent` for that directory, which stays after the script ends unless killed. Evidence: `pgrep -c gpg-agent` went from 0 to 1 after `gpg --homedir w/gnupg --batch --no-tty --quiet --import ...`, 2026-10-12; `pgrep -a` showed `gpg-agent --homedir .../w/gnupg --use-standard-socket --daemon`; `gpgconf --homedir w/gnupg --kill all` brought it back to 0.
- The real import, list and export path works on the real key and exports the same 3817 bytes. Evidence, 2026-10-12: `install_docker_key` sourced from `deploy/setup.sh` and called alone with `KEYRING_DIR` set to a directory under the scratchpad (real `curl` and `gpg`, nothing under `/etc`) installed a mode 0644 `docker.asc` that starts with `-----BEGIN PGP PUBLIC KEY BLOCK-----`, holds one `pub` record whose first `fpr` is the pinned value, and was byte-identical to the download (`cmp`); `pgrep -ac gpg-agent` was 0 afterwards.
- `gpg --export` exits 0 when it exports nothing (hence the header check in `export_docker_key`). Unverified here against the real binary; the fake `gpg` in the tests models it, and the header check does not depend on it. Running `gpg --homedir <empty dir> --armor --export 9DC8...` would settle it.
- `WantedBy=docker.service` plus enable starts a dependent unit again after Docker restarts. Evidence (Debian 13, systemd 257, two throwaway units, 2026-10-12): with `Requires=` and `After=` only, the dependent unit stays inactive after its required unit is stopped and started; with the dependent unit also linked into the required unit's `.wants` directory it starts again, also on a restart. Not re-run by this drafter.
- `systemctl --no-reload` exists and keeps `enable` and `disable` from reloading the manager. Evidence: `systemctl --help` on this host, 2026-10-12, prints `--no-reload         Don't reload daemon after en-/dis-abling unit files`. That a plain `reenable` reloads the manager on every run is the plan reviewer's finding and what that help text implies; I did not run it, because systemd changes on this host are not allowed. The tests replace `systemctl`, so they check only the arguments and the order of the calls (`--no-reload reenable`, then at most one `daemon-reload`, then `restart`). That `reenable` recreates the links from the unit's current `[Install]` section, and that a changed unit file that is not yet reloaded is enabled correctly, are unverified on a live host; the live check in spec section 8 (stop and start Docker on the runner LXC) settles it.
- Bash `read` returns non-zero both at end of input and when stdin is closed, and a partial last line comes back in the variable with status 1; an empty line returns 0. Evidence, 2026-10-12: `bash -c 'IFS= read -rs p </dev/null; echo "eof rc=$? p=[$p]"; IFS= read -rs p <&-; echo "closed rc=$? p=[$p]"; IFS= read -rs p < <(printf abc); echo "partial rc=$? p=[$p]"; IFS= read -rs p <<<""; echo "enter rc=$? p=[$p]"'` printed `eof rc=1 p=[]`, `read: read error: 0: Bad file descriptor` then `closed rc=1 p=[]`, `partial rc=1 p=[abc]`, `enter rc=0 p=[]`.
- An instance directory is a full copy of the runner files and nothing reads an old version directory after that: `internal/runner/lifecycle.go:93-101` (`dist, err := m.Host.ReadLink(m.Paths.Dist)`, `m.Host.CopyTree(ctx, dist, dir)`, `DistVersion: filepath.Base(dist)`), and `internal/daemon/run.go:91` (`Dist: "/opt/ghr/dist/current"`). `stop_ghr` stops the daemon before `install_runner` (`deploy/setup.sh`, `main`), so no copy is in progress while `gc_dist` runs.
- `web:` in `ghr setup` is `web.listen` from the config, or `-` when the web UI is off. Evidence: `cmd/ghr/setup.go:97` prints `web: %s` from `orDash(st.WebListen)`; `internal/daemon/run.go:332-335` sets `webListen = webCfg.Listen`; `deploy/config.example.yaml:15` is `listen: 0.0.0.0:8080`.
- An IPv6 `web.listen` has its host in brackets. Evidence: the address goes through `net.Listen("tcp", webCfg.Listen)` (`internal/daemon/run.go:302`) and is validated with `net.SplitHostPort` (`internal/config/config.go:430`), which requires brackets around an IPv6 host.
- `hostname -I` prints addresses separated by spaces, IPv4 or IPv6 first, unbracketed. Evidence: the existing fake in the tests (`192.168.0.99 fd00::99`) and `man hostname`; not run on the runner LXC.
- The release job needs no git credential after the checkout. Evidence: `.github/workflows/ci.yml` as found, lines 62 and 68 (`git tag --points-at HEAD --list ...`, `git tag --list ... --sort=-v:refname`) read local tags, which `fetch-depth: 0` has already fetched; line 111 publishes with `gh release create ... --target "$GITHUB_SHA"` and `GH_TOKEN: ${{ github.token }}`; no other `git` or `gh` call is in the file.
- Action pins, resolved 2026-10-12 with read-only calls. Newest release of the major in use: `gh api repos/actions/checkout/tags --paginate -q '.[].name' | rg '^v4\.[0-9]+\.[0-9]+$' | sort -V | tail -1` gave `v4.4.0`; the same for `setup-go` with `^v5\.` gave `v5.6.0`; for `setup-node` with `^v4\.` gave `v4.4.0`. Each is a published, non-pre-release release: `gh api repos/actions/checkout/releases/tags/v4.4.0` (published 2026-07-20), `repos/actions/setup-go/releases/tags/v5.6.0` (2025-12-16), `repos/actions/setup-node/releases/tags/v4.4.0` (2025-04-14). SHAs: `gh api repos/actions/checkout/git/ref/tags/v4.4.0 -q '.object.type + " " + .object.sha'` gave `commit 11d5960a326750d5838078e36cf38b85af677262`; `repos/actions/setup-go/git/ref/tags/v5.6.0` gave `commit 40f1582b2485089dde7abd97c1529aa768e1baff`; `repos/actions/setup-node/git/ref/tags/v4.4.0` gave `commit 49933ea5288caeca8642d1e84afbd3f7d6820020`. All three tags are lightweight (object type `commit`), so no dereference was needed. Task 23's text states these three SHAs as verified on 2026-10-12 and tells the implementer to change no pin and resolve nothing; these commands are evidence for reviewers only.
- The README line being replaced is the second item of "Requirements" (`README.md:18-19` as found). The per-repository setting is `allow_public` (`internal/config/config.go:84`, with the comment that a job in a public repository can come from anyone's pull request).
- `shellcheck` is not installed on this host (`which shellcheck` printed nothing), so the script is not verified clean under it. `bash -n deploy/setup.sh` passes. Running `shellcheck deploy/setup.sh deploy/tests/setup_test.sh` where it is available would settle it.
Line numbers are those of commit `76066ff` (local `master` on 2026-10-12), the commit the work started from.

- `Backend.update` turns every error that is not an `*api.Error` into a 400 with the error's text: `internal/daemon/backend.go:418-431`.
- `internal/api` has constructors for 400, 404 and 409 only, so the 500 is a literal: `internal/api/server.go:59-61`; `api.Error` is `{Status, Msg, RetryAt}`: `internal/api/server.go:25-29`.
- `Store.Update` validates before it saves, and `config.Save` validates again, so an error from `Save` inside `Update` is a write failure: `internal/daemon/store.go:112-118`, `internal/config/config.go:226-229`.
- A rename onto a directory fails for root too. Evidence: `TestSaveFailureIsA500` passes as root on this host (2026-10-12), and `TestStoreConfigureWriteFailures` at `internal/daemon/store_test.go:201-207` already relies on it.
- The runner-cap warnings come from `config.Parse` (`clampRunners`), not from `Validate`, with the text `<what> <n> is above 99; using 99`: `internal/config/config.go:186-191`, `:200`, `:37`. `Validate` has one warning of its own, about `cleanup_name_prefixes`: `internal/config/config.go:404`.
- There are two reload paths: `Backend.Reload` at `internal/daemon/backend.go:815`, and the not-configured loop in `Run` at `internal/daemon/run.go:411-420`. Start-up warnings are written at `internal/daemon/run.go:315-317`.
- The web listener is opened at `internal/daemon/run.go:302` and served at `:370`; the Unix socket's listener is a different variable, `ln`, at `:280`.
- **How long a silent connection can hold a slot on the web listener** (`internal/daemon/run.go:285-289`): `ReadHeaderTimeout` 10 s and `ReadTimeout` 30 s at line 286, `WriteTimeout` (`webWriteTimeout`, 60 s, line 42) and `IdleTimeout` 120 s at line 287. A connection that sends nothing is closed after 10 s; one that sends a request slowly after 30 s; a response is cut 60 s after its request was read; a kept-alive connection is closed after 120 s without a new request.
- **No route streams without a session.** The change stream is `GET /changes` in the API (`internal/api/server.go:194`), which the web listener serves only under `/api/`, behind `requireSession`: `internal/webui/handler.go:50`, `:140`. It moves its own write deadline on every write (`internal/api/server.go:499`, `:524`), so with a session it stays open as long as the session lives. The routes without a session are `/auth/*` (`internal/webui/handler.go:42-49`) and the static files (`:51`); each answers at once.
- `http.Server.Serve` returns `ErrServerClosed` when `Accept` fails during a shutdown, retries with a growing delay when the error is temporary, and otherwise stops serving: `/usr/local/go/src/net/http/server.go:3434-3450` (Go 1.26.1). This is why the listener must not return an error for a refused connection.
- The HTTP server closes a connection more than once: `Server.Close` closes every active connection (`/usr/local/go/src/net/http/server.go:3114-3117`) and the connection's own goroutine closes it again (`:1754-1756`).
- `testing/synctest` is in the standard library of the Go in use: `/usr/local/go/src/testing/synctest/synctest.go`, `go version go1.26.1 linux/amd64`, `go.mod:3` says `go 1.26.0`. No test in the tree used it before.
- Inference, not verified beyond this host: the kernel hands connections to `accept` in the order they arrived, so in `TestRunBoundsAWebClientsConnections` the 65th connection dialled is the one over the bound. Evidence: the test passed in every run on 2026-10-12 (at least 5 plain and 4 under `-race`). If the order could differ, the test would fail with `connection 65 of one client was not closed`.
- `finishedJob` reads the job, so the completion time is at hand: `internal/daemon/joblogs.go:248-256`; `github.Job.CompletedAt` is a `*time.Time` (`internal/github/client.go:89`), as is `model.RunJob.CompletedAt` (`internal/model/model.go:362`). The answers are stored in one place, `downloadLog`: `internal/daemon/joblogs.go:159`, and read in two: `internal/daemon/joblogs.go:275`, `internal/daemon/annotations.go:45`.
- `logJobOf` gives a job that completed at `actionsT0` plus one minute: `internal/daemon/joblogs_test.go:31`. `TestRunAnnotationsCountsLogOutcomes` froze its clock at `actionsT0`: `internal/daemon/annotations_test.go:149`.
- Unverified: whether GitHub can answer 404 for a log right after a job completes (spec section 7, row 213). It would be settled by requesting a log the moment a job finishes. The task is correct either way.
- `readJSON` is `os.ReadFile`: `internal/runner/manager.go:595-601`. The reads of job-writable files are `internal/runner/private.go:50` (`ghr.json`), `internal/runner/adopt.go:90`, `internal/runner/lifecycle.go:170` and `:431` (`job.json`). The root-owned reads that keep `readJSON` are `internal/runner/private.go:48`, `internal/runner/lifecycle.go:435`, `:546`, `:626` and `internal/runner/update.go:115`.
- `readFrom` checks the name with `Lstat`, opens with `O_NONBLOCK`, and accepts only a regular file, checked again on the open file: `internal/runner/cleanup.go:513-524`. It reads through an `os.Root` of the log directory, which `openLogs` opens: `internal/runner/cleanup.go:488-506`.
- The directory that holds the instance directories is root's, and each instance directory is given to the runner user, so a job controls only the last part of the path: `deploy/setup.sh:80-81` (`install -d -m 0755 ... "$STATE_DIR/instances"`), `internal/runner/lifecycle.go:114` (`ChownR(ctx, dir, RunnerUser)`).
- The tree type-checks for Windows and must keep doing so: `GOOS=windows go vet ./...` prints nothing at the finished branch (2026-10-12). `syscall.O_NONBLOCK` exists there (`internal/runner/cleanup.go:517` uses it untagged); `syscall.O_NOFOLLOW` does not. My first version of Task 29 used `O_NOFOLLOW` and was the only thing in the tree that failed that check, which is how I know the base passes it. `internal/runner/cleanup_test.go:202` skips a test on Windows, so someone runs these tests there. Test files that make FIFOs are tagged `//go:build unix`, like `internal/runner/runnerlog_unix_test.go`.
- Each run in GitHub's runs listing carries `repository.private`: `gh api 'repos/darkraise/ghr/actions/runs?per_page=1' --jq '.workflow_runs[0] | {id, private: .repository.private, repo: .repository.full_name}'` answered `{"id":38115999933,"private":false,"repo":"darkraise/ghr"}` on 2026-10-12.
- `repoDemand` lists the queued, in-progress and waiting runs on every poll (`internal/runner/tick.go:191-195`); `gatherDemand` calls `readVisibility` right after it and drops the queue of a repository that has just become blocked (`internal/runner/tick.go:129-153`); `readVisibility` makes no call while the answer is younger than 6 hours (`internal/runner/visibility.go:38-40`, `:14`). When a poll fails, `gatherDemand` goes on to the next repository without touching the queued jobs it copied from the last poll (`internal/runner/tick.go:100-104`, `:135-145`), and status counts a repository's queue from that record (`internal/runner/manager.go:618`).
- `readVisibility` copies the record under the manager's lock, releases the lock for the `GetRepo` call and then stores the answer without looking at the record again: `internal/runner/visibility.go:33-51`. `ClearDegraded`, which a reload and a new token call, sets `readAt` to zero on every record: `internal/runner/manager.go:740-750`.
- A tick takes the config once, at its start, and passes that copy to every step: `internal/runner/tick.go:18`. The manager reaches the config only through its `Config` func (`internal/runner/manager.go:164`), which the daemon sets to `store.Config` (`internal/daemon/run.go:173`), an atomic load that takes no lock (`internal/daemon/store.go:43`). `internal/runner` does not import `internal/daemon`, so the manager cannot call the store while it holds its own lock.
- `Store.Update` holds the store's lock while it runs the function it is given: `internal/daemon/store.go:105-109`. Task 25's tests use that to start reloads that are certain to run after the update.
- `Validate` warns about `cleanup_name_prefixes` only when the repository's effective `max` is not 1, and a repository without `max` counts as 1 outside mode `all`: `internal/config/config.go:403-405`, `:288-296`.
- Unverified: that a 304 from GitHub for a runs listing always means `repository.private` is unchanged, that is, that the listing's ETag changes when only the repository's visibility changes. Task 31 relies on it only in one direction: a cached listing that still said "public" after the repository was made private again would block the repository again at every poll, and `GetRepo`'s "private" would lift the block only in a poll whose own listing did not say public. It would be settled by making a test repository with a queued run private and comparing the listing's ETag before and after, which is a write to GitHub.
- The manager forgets a removed repository only at the start of a tick: `internal/runner/tick.go:25`, `internal/runner/visibility.go:137-150`. `AddRepo` reads the repository at `internal/daemon/backend.go:542` and saves at `:558`; the save wakes the run loop (`internal/daemon/backend.go:437-439`).
- Only `*runner.Manager` and the daemon's `fakeManager` implement the daemon's `Manager` interface: `rg 'func \(.*\) CancelUpdate\(\) error' internal cmd` on 2026-10-12 found `internal/runner/update.go:258` and `internal/daemon/backend_test.go`.
- `history.Store.Writes()` counts "the appends that wrote a line and the prunes that replaced the file" (`internal/history/history.go:40-46`), and a prune replaces the file only when a line went (`internal/history/history.go:235-237`).
- `forcedPrune` wrote the retention event whenever the step did not fail: `internal/runner/cleanup.go:363-370`. `prune` has two callers: `internal/runner/cleanup.go:364` and `internal/runner/tick.go:66`, which ignores the result.
- `metrics.Load` kept every rollup not dated after now: `internal/metrics/metrics.go:177-182`; `addToHour` drops the rollups that are not before the sample's hour on a step back: `internal/metrics/metrics.go:310-312`. `Hours()` returns the closed rollups followed by the open hour: `internal/metrics/metrics.go:256-264`.


## Task index

1. Two-tier login lockout
2. Auth request handling
3. Per-call timeouts in the API client
4. The CLI names why a call ended
5. The kill route's write deadline
6. Token prompt and argument checks
7. Ctrl-C at a password prompt
8. Label rules
9. An empty config file loads as the defaults
10. Save through a symlink, and sync
11. Downloads only from the tool's own releases
12. The download client refuses a redirect to plain http
13. Version lists follow the same rule, and tar keeps no owners
14. A failed Python install removes only what it made
15. Go file names before 1.21
16. Removing a Java folder whose name contains `+`
17. The `.tmp` directory without a check-then-use gap
18. Hard links counted once
19. Check Docker's apt key
20. The service unit comes back with Docker
21. Ask for the web password again after a failed first install
22. Old runner versions, the version prefix and the closing hint
23. CI permissions and pins, and the README
24. A failed save is a 500
25. A reload repeats no warning
26. Connection limits on the web listener
27. A panic in a token check
28. A log that is not there yet
29. Root reads of job-writable files
30. A listed run says whether its repository is private
31. A repository that turned public is blocked from the poll that shows it
32. The manager takes a visibility answer from outside
33. Adding a repository hands over what it just read
34. The retention event and an honest comment
35. One rollup per hour after a clock step back
36. Whole-tree verification

---

### Task 1: Two-tier login lockout

**Files:**
- Modify: `internal/webui/auth.go` (the constants, `login`, `admitLocked`, `ClientKey`; new `failLocked` and `prefixKey`)
- Test: `internal/webui/auth_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names. `ClientKey` keeps its name and signature, so `internal/webui/handler.go` is not edited.

**Items:** 47
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 3 = 4

A failed web login is counted under one key per client, the value of `ClientKey`: the IPv4 address, or for IPv6 the /64 the address lies in. Five failures within 15 minutes lock the key for 15 minutes. On a LAN every IPv6 host is in the same /64, so one host's wrong passwords lock every host out.

After this task an IPv4 client is counted exactly as before. An IPv6 client is counted under two keys: its own address, locked at 5 failures, and its /64, locked at 50. A login is refused while either key is locked, with the later of the two retry times. A successful login clears the address's failures and leaves the /64's. A locked client is refused before its password is checked, so while a /64 is locked nothing more is counted for it and the table of counters stays bounded. The window, the lockout and the sweep do not change. This is security code: type it exactly, and do not shorten the tests.

How it is built: `ClientKey` returns the address alone, without the port. `login` keeps taking that one key and derives the second one itself with the new `prefixKey`, which answers only for an IPv6 address. Both keys live in the same `fails` map (a /64 key contains `/`, an address never does), so `sweepLocked` handles both without a change.

- [ ] **Step 1: Write the failing tests**

All test edits are in `internal/webui/auth_test.go`. The tests name one new constant, so add it first: in `internal/webui/auth.go` replace

```go
	sessionTTL  = 30 * 24 * time.Hour
	maxFailures = 5
	failWindow  = 15 * time.Minute
	lockout     = 15 * time.Minute
```

with

```go
	sessionTTL  = 30 * 24 * time.Hour
	maxFailures = 5
	// maxPrefixFailures is the limit for a whole IPv6 /64. It is higher than
	// maxFailures because on a LAN every host shares one /64.
	maxPrefixFailures = 50
	failWindow        = 15 * time.Minute
	lockout           = 15 * time.Minute
```

In `internal/webui/auth_test.go`, add `"fmt"` to the imports. Replace

```go
	"errors"
	"io/fs"
```

with

```go
	"errors"
	"fmt"
	"io/fs"
```

Replace the whole `TestFailuresUseARollingWindow` function with these two functions:

```go
// These figures decide how many passwords a client may try, so a change to
// any of them must be a deliberate one.
func TestThrottleFigures(t *testing.T) {
	if maxFailures != 5 || maxPrefixFailures != 50 || failWindow != 15*time.Minute || lockout != 15*time.Minute {
		t.Fatalf("limits %d per address and %d per /64, window %v, lockout %v", maxFailures, maxPrefixFailures, failWindow, lockout)
	}
}

func TestFailuresUseARollingWindow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   func(i int) string
		limit int
	}{
		{"IPv4 address", func(int) string { return "10.0.0.1" }, maxFailures},
		{"IPv6 address", func(int) string { return v6(1) }, maxFailures},
		{"IPv6 /64", func(i int) string { return v6(i + 1) }, maxPrefixFailures},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, c := newTestAuth(t)
			if _, err := a.Setup(pw); err != nil {
				t.Fatal(err)
			}
			done := 0
			fail := func(n int) {
				t.Helper()
				for range n {
					if _, err := a.Login(tc.key(done), "wrong password!"); !errors.Is(err, ErrWrongPassword) {
						t.Fatalf("failure %d at %v: %v", done+1, c.now(), err)
					}
					done++
				}
			}
			fail(1)
			c.add(14 * time.Minute)
			fail(tc.limit - 2)
			c.add(2 * time.Minute)
			// The first failure has left the window, so the lock lands on the
			// second of these two and not on the first.
			fail(2)
			var te *ThrottledError
			if _, err := a.Login(tc.key(0), pw); !errors.As(err, &te) {
				t.Fatalf("%d failures within the last 15 minutes did not lock the client out: %v", tc.limit, err)
			}
		})
	}
}
```

Replace the whole `TestBurstCannotExceedTheLimit` function with:

```go
func TestBurstCannotExceedTheLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		n     int
		key   func(i int) string
		wrong int
	}{
		{"one address", 20, func(int) string { return "10.0.0.1" }, maxFailures},
		{"one /64", 60, func(i int) string { return v6(i + 1) }, maxPrefixFailures},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestAuth(t)
			if _, err := a.Setup(pw); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan error, tc.n)
			for i := range tc.n {
				go func() {
					<-start
					_, err := a.Login(tc.key(i), "wrong password!")
					results <- err
				}()
			}
			close(start)
			wrong, throttled := 0, 0
			for range tc.n {
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
			if wrong != tc.wrong || throttled != tc.n-tc.wrong {
				t.Fatalf("%d wrong-password answers and %d throttled, want %d and %d", wrong, throttled, tc.wrong, tc.n-tc.wrong)
			}
			var te *ThrottledError
			if _, err := a.Login(tc.key(0), pw); !errors.As(err, &te) {
				t.Fatalf("the right password after the burst: %v", err)
			}
		})
	}
}
```

Directly above `func TestFailuresExpireAndAreSwept`, add:

```go
// v6 is the throttle key of host n in the /64 v6Prefix, as the handler
// derives it.
func v6(n int) string { return ClientKey(fmt.Sprintf("[2001:db8:1:2::%x]:5555", n)) }

const v6Prefix = "2001:db8:1:2::/64"

func failureCounters(a *Auth) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.fails)
}

// A dual-stack listener reports an IPv4 client as ::ffff:a.b.c.d. That is
// the client a.b.c.d: one counter, locked at maxFailures.
func TestIPv4MappedAddressIsItsIPv4Client(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	keys := []string{ClientKey("10.0.0.1:5555"), ClientKey("[::ffff:10.0.0.1]:5555")}
	for i := range maxFailures {
		if _, err := a.Login(keys[i%2], "wrong password!"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	for _, key := range keys {
		var te *ThrottledError
		if _, err := a.Login(key, pw); !errors.As(err, &te) {
			t.Fatalf("%s after %d failures: %v", key, maxFailures, err)
		}
	}
	if n := failureCounters(a); n != 1 {
		t.Fatalf("%d failure counters, want 1", n)
	}
}

func TestOnlyAnIPv6AddressHasAPrefixCounter(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want int
	}{
		{"10.0.0.1", 1},
		{"::ffff:10.0.0.1", 1},
		{"not an address", 1},
		{v6(1), 2},
	} {
		a, _ := newTestAuth(t)
		if _, err := a.Setup(pw); err != nil {
			t.Fatal(err)
		}
		a.Login(tc.key, "wrong password!")
		if n := failureCounters(a); n != tc.want {
			t.Errorf("%s: %d failure counters after one failure, want %d", tc.key, n, tc.want)
		}
	}
}

// With every derivation slot taken and its context already done, a login
// that gets as far as the password check returns the context's error. A
// locked client must be refused before that: no derivation runs for it and
// no failure is counted, whatever password it sends, until the lockout ends.
func TestLockedClientIsRefusedBeforeThePasswordCheck(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures int
		key      func(i int) string
		refused  string
		admitted string
	}{
		{"IPv4 address", maxFailures, func(int) string { return "10.0.0.1" }, "10.0.0.1", "10.0.0.2"},
		{"IPv6 address", maxFailures, func(int) string { return v6(1) }, v6(1), v6(2)},
		{"IPv6 /64, an address never seen", maxPrefixFailures, func(i int) string { return v6(i + 1) }, v6(1000), ClientKey("[2001:db8:1:3::1]:5555")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, c := newTestAuth(t)
			if _, err := a.Setup(pw); err != nil {
				t.Fatal(err)
			}
			for i := range tc.failures {
				if _, err := a.Login(tc.key(i), "wrong password!"); !errors.Is(err, ErrWrongPassword) {
					t.Fatalf("failure %d: %v", i+1, err)
				}
			}
			until := c.now().Add(lockout)
			counters := failureCounters(a)
			fillSlots(a)
			defer func() { <-a.sem; <-a.sem }()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			refused := func(when string) {
				t.Helper()
				for _, password := range []string{pw, "wrong password!"} {
					var te *ThrottledError
					if _, err := a.login(ctx, tc.refused, password); !errors.As(err, &te) || !te.RetryAt.Equal(until) {
						t.Fatalf("%s %s: %v", tc.refused, when, err)
					}
				}
			}
			refused("at once")
			if _, err := a.login(ctx, tc.admitted, pw); !errors.Is(err, context.Canceled) {
				t.Fatalf("%s, which is not locked out: %v", tc.admitted, err)
			}
			if n := failureCounters(a); n != counters {
				t.Fatalf("%d failure counters after the refusals, want %d", n, counters)
			}
			c.add(lockout - time.Nanosecond)
			refused("just before the lockout ends")
			c.add(time.Nanosecond)
			if _, err := a.login(ctx, tc.refused, pw); !errors.Is(err, context.Canceled) {
				t.Fatalf("%s after the lockout: %v", tc.refused, err)
			}
			if n := failureCounters(a); n != 0 {
				t.Fatalf("%d failure counters left after the lockout", n)
			}
		})
	}
}

// login checks the lockout three times: on arrival, after waiting for a
// derivation slot, and after the derivation. A lock that lands before any of
// them must refuse the login and stay in place, whichever key it is on and
// even when the password is right. login reads the clock under a.mu right
// before each check, which is where this test plants the lock.
func TestALockLandingMidLoginRefusesIt(t *testing.T) {
	for _, tc := range []struct{ name, locked string }{{"address", v6(1)}, {"prefix", v6Prefix}} {
		for check := 1; check <= 3; check++ {
			t.Run(fmt.Sprintf("%s before check %d", tc.name, check), func(t *testing.T) {
				a, c := newTestAuth(t)
				if _, err := a.Setup(pw); err != nil {
					t.Fatal(err)
				}
				until := c.now().Add(lockout)
				reads := 0
				a.now = func() time.Time {
					if reads++; reads == check {
						a.fails[tc.locked] = &failures{times: []time.Time{c.now()}, lockedUntil: until}
					}
					return c.now()
				}
				var te *ThrottledError
				if id, err := a.Login(v6(1), pw); !errors.As(err, &te) || !te.RetryAt.Equal(until) || id != "" {
					t.Fatalf("login: id %q err %v", id, err)
				}
				if reads != check {
					t.Fatalf("login went on to clock read %d after the lock landed before check %d", reads, check)
				}
				if f := a.fails[tc.locked]; f == nil || !f.lockedUntil.Equal(until) || len(a.sessions) != 1 {
					t.Fatalf("the refused login changed the lock or started a session: %+v, %d sessions", f, len(a.sessions))
				}
			})
		}
	}
}

func TestRefusalNamesTheLaterRetryTime(t *testing.T) {
	a, c := newTestAuth(t)
	now := c.now()
	for _, tc := range []struct {
		name            string
		address, prefix time.Duration // how long after now each lock ends; 0 is no counter
		want            time.Duration // 0 is admitted
	}{
		{"no counter", 0, 0, 0},
		{"address locked", 3 * time.Minute, 0, 3 * time.Minute},
		{"/64 locked", 0, 4 * time.Minute, 4 * time.Minute},
		{"both locked, /64 for longer", 3 * time.Minute, 4 * time.Minute, 4 * time.Minute},
		{"both locked, address for longer", 4 * time.Minute, 3 * time.Minute, 4 * time.Minute},
		{"address lock ended, /64 locked", -time.Minute, 4 * time.Minute, 4 * time.Minute},
		{"/64 lock ended, address locked", 3 * time.Minute, -time.Minute, 3 * time.Minute},
		{"both locks ended", -time.Minute, -time.Second, 0},
	} {
		a.mu.Lock()
		clear(a.fails)
		for key, d := range map[string]time.Duration{v6(1): tc.address, v6Prefix: tc.prefix} {
			if d != 0 {
				a.fails[key] = &failures{times: []time.Time{now}, lockedUntil: now.Add(d)}
			}
		}
		err := a.admitLocked(v6(1), now)
		a.mu.Unlock()
		var te *ThrottledError
		switch {
		case tc.want == 0 && err != nil:
			t.Errorf("%s: refused: %v", tc.name, err)
		case tc.want != 0 && (!errors.As(err, &te) || !te.RetryAt.Equal(now.Add(tc.want))):
			t.Errorf("%s: %v, want a retry at %v", tc.name, err, now.Add(tc.want))
		}
	}
}

// On a LAN every IPv6 host shares one /64, so one host's wrong passwords
// must lock that host's address and nobody else's.
func TestIPv6AddressLocksAlone(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	for i := range maxFailures {
		if _, err := a.Login(v6(1), "wrong password!"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	var te *ThrottledError
	if _, err := a.Login(v6(1), pw); !errors.As(err, &te) || !te.RetryAt.Equal(c.now().Add(lockout)) {
		t.Fatalf("sixth attempt: %v", err)
	}
	if _, err := a.Login(v6(2), pw); err != nil {
		t.Fatalf("another address in the /64 was throttled: %v", err)
	}
	a.Login("10.0.0.1", "wrong password!")
	// The IPv6 address, its /64, and the IPv4 address, which has no second key.
	if n := failureCounters(a); n != 3 {
		t.Fatalf("%d failure counters, want 3", n)
	}
}

// A host that takes a new address inside its /64 for every attempt is held to
// maxPrefixFailures a window, and adds nothing to the table once the /64 is
// locked.
func TestIPv6PrefixLocksAtFifty(t *testing.T) {
	a, c := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	fail := func(key string) {
		t.Helper()
		if _, err := a.Login(key, "wrong password!"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("%s: %v", key, err)
		}
	}
	retryAt := func(key, password string) time.Time {
		t.Helper()
		var te *ThrottledError
		if _, err := a.Login(key, password); !errors.As(err, &te) {
			t.Fatalf("%s was not refused: %v", key, err)
		}
		return te.RetryAt
	}
	start := c.now()
	for range maxFailures {
		fail(v6(1))
	}
	c.add(5 * time.Minute)
	others := maxPrefixFailures - maxFailures
	for n := range others {
		fail(v6(n + 2))
	}
	// v6(1) is locked on its own until start+lockout; the /64's lock ends later.
	prefixUntil := start.Add(5*time.Minute + lockout)
	if got := retryAt(v6(1), pw); !got.Equal(prefixUntil) {
		t.Fatalf("an address locked before its /64: retry at %v, want %v", got, prefixUntil)
	}
	for n := range 10 {
		if got := retryAt(v6(1000+n), "wrong password!"); !got.Equal(prefixUntil) {
			t.Fatalf("a fresh address in a locked /64: retry at %v, want %v", got, prefixUntil)
		}
	}
	if n, want := failureCounters(a), others+2; n != want {
		t.Fatalf("%d failure counters while the /64 is locked, want %d", n, want)
	}
	if _, err := a.Login(ClientKey("[2001:db8:1:3::1]:5555"), pw); err != nil {
		t.Fatalf("another /64 was throttled: %v", err)
	}
	if _, err := a.Login("10.0.0.1", pw); err != nil {
		t.Fatalf("an IPv4 client was throttled: %v", err)
	}
	c.add(10 * time.Minute)
	if got := retryAt(v6(1), pw); !got.Equal(prefixUntil) {
		t.Fatalf("after the address's own lockout: retry at %v, want %v", got, prefixUntil)
	}
	c.add(5 * time.Minute)
	if _, err := a.Login(v6(1000), pw); err != nil {
		t.Fatalf("after the /64's lockout: %v", err)
	}
	if n := failureCounters(a); n != 0 {
		t.Fatalf("%d failure counters left after the lockout", n)
	}
}

// The owner logging in from one address must not hand a guesser beside it a
// fresh count for the /64.
func TestSuccessClearsTheAddressNotThePrefix(t *testing.T) {
	a, _ := newTestAuth(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		for range maxFailures - 1 {
			a.Login(v6(1), "wrong password!")
		}
		if _, err := a.Login(v6(1), pw); err != nil {
			t.Fatalf("failures before a success still counted for the address: %v", err)
		}
	}
	a.mu.Lock()
	_, address := a.fails[v6(1)]
	counted := 0
	if f := a.fails[v6Prefix]; f != nil {
		counted = len(f.times)
	}
	a.mu.Unlock()
	if want := 2 * (maxFailures - 1); address || counted != want {
		t.Fatalf("after two successes: address counter kept %v, %d failures counted for the /64, want %d", address, counted, want)
	}
	for n := range maxPrefixFailures - 2*(maxFailures-1) {
		if _, err := a.Login(v6(n+2), "wrong password!"); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("address %d: %v", n+2, err)
		}
	}
	var te *ThrottledError
	if _, err := a.Login(v6(1), pw); !errors.As(err, &te) {
		t.Fatalf("the successes cleared the /64's failures: %v", err)
	}
}
```

In `TestClientKey`, `ClientKey` used to return the /64 for an IPv6 address; it now returns the address. Replace

```go
		"[2001:db8:1:2:aaaa::1]:5555": "2001:db8:1:2::/64",
		"[2001:db8:1:2:bbbb::9]:6000": "2001:db8:1:2::/64",
		"[::ffff:10.0.0.1]:80":        "10.0.0.1",
		"[fe80::1%eth0]:80":           "fe80::/64",
```

with

```go
		"[2001:db8:1:2:aaaa::1]:5555": "2001:db8:1:2:aaaa::1",
		"[2001:db8:1:2:bbbb::9]:6000": "2001:db8:1:2:bbbb::9",
		"[::ffff:10.0.0.1]:80":        "10.0.0.1",
		"[fe80::1%eth0]:80":           "fe80::1",
```

The tests use the package's own helpers: `newTestAuth` (its clock is the returned `*clock`, moved with `c.add`), `fillSlots`, and the constant `pw`. `v6(n)` builds its key through `ClientKey`, as the handler does.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/webui/ -run 'Throttle|Rolling|Burst|IPv|Prefix|Locked|Lock|Refusal|SuccessClears|ClientKey'`
Expected: FAIL. The lines that show why:

- `TestIPv6AddressLocksAlone`: `another address in the /64 was throttled: too many failed logins; retry after 2026-10-05T12:15:00Z`
- `TestBurstCannotExceedTheLimit/one_/64`: `5 wrong-password answers and 55 throttled, want 50 and 10`
- `TestSuccessClearsTheAddressNotThePrefix`: `after two successes: address counter kept false, 0 failures counted for the /64, want 8`
- `TestClientKey`: `ClientKey("[2001:db8:1:2:aaaa::1]:5555") = "2001:db8:1:2::/64", want "2001:db8:1:2:aaaa::1"`

`TestFailuresUseARollingWindow/IPv6_/64`, `TestOnlyAnIPv6AddressHasAPrefixCounter`, `TestLockedClientIsRefusedBeforeThePasswordCheck` (two of its three rows), `TestRefusalNamesTheLaterRetryTime` and `TestIPv6PrefixLocksAtFifty` fail too. `TestThrottleFigures`, `TestIPv4MappedAddressIsItsIPv4Client`, `TestALockLandingMidLoginRefusesIt` and the IPv4 rows pass already: they guard what must not change.

- [ ] **Step 3: Write the implementation**

All edits are in `internal/webui/auth.go`.

In `login`, replace

```go
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
```

with

```go
	if err := a.admitLocked(key, now); err != nil {
		return "", err
	}
	if !match {
		a.failLocked(key, maxFailures, now)
		if p, ok := prefixKey(key); ok {
			a.failLocked(p, maxPrefixFailures, now)
		}
		return "", ErrWrongPassword
	}
```

Leave the rest of `login` as it is. In particular the line `delete(a.fails, key)` near its end stays: it clears the address and must never touch the /64's counter.

Replace the whole `admitLocked` method, including its comment,

```go
// admitLocked sweeps, then refuses a client that is locked out. a.mu is held.
func (a *Auth) admitLocked(key string, now time.Time) error {
	a.sweepLocked(now)
	if f := a.fails[key]; f != nil && now.Before(f.lockedUntil) {
		return &ThrottledError{RetryAt: f.lockedUntil}
	}
	return nil
}
```

with these two methods:

```go
// failLocked counts one failure under key and locks key out once it has
// limit of them within failWindow. a.mu is held.
func (a *Auth) failLocked(key string, limit int, now time.Time) {
	f := a.fails[key]
	if f == nil {
		f = &failures{}
		a.fails[key] = f
	}
	f.times = append(f.times, now)
	if len(f.times) >= limit {
		f.lockedUntil = now.Add(lockout)
	}
}

// admitLocked sweeps, then refuses a client whose address or IPv6 /64 is
// locked out, with the later of the two retry times. Every login passes here
// again just before a failure is counted, so nothing is counted for a locked
// /64 and one /64 never holds more than maxPrefixFailures address counters.
// a.mu is held.
func (a *Auth) admitLocked(key string, now time.Time) error {
	a.sweepLocked(now)
	var until time.Time
	if f := a.fails[key]; f != nil {
		until = f.lockedUntil
	}
	if p, ok := prefixKey(key); ok {
		if f := a.fails[p]; f != nil && f.lockedUntil.After(until) {
			until = f.lockedUntil
		}
	}
	if now.Before(until) {
		return &ThrottledError{RetryAt: until}
	}
	return nil
}
```

`sweepLocked`, directly below, is not changed.

At the end of the file, replace the whole `ClientKey` function, including its comment,

```go
// ClientKey is the throttle key for a request's remote address: the IPv4
// address, or the IPv6 /64, since one IPv6 host can use a whole /64.
func ClientKey(addr string) string {
	ip, ok := remoteAddr(addr)
	if !ok {
		if host, _, err := net.SplitHostPort(addr); err == nil {
			return host
		}
		return addr
	}
	if ip.Is6() {
		p, _ := ip.Prefix(64)
		return p.String()
	}
	return ip.String()
}
```

with these two functions:

```go
// ClientKey is the throttle key for a request's remote address: the address
// without its port. Login also counts an IPv6 address under its /64.
func ClientKey(addr string) string {
	ip, ok := remoteAddr(addr)
	if !ok {
		if host, _, err := net.SplitHostPort(addr); err == nil {
			return host
		}
		return addr
	}
	return ip.String()
}

// prefixKey is the second throttle key of an IPv6 client, its /64: one host
// can use every address in it. An IPv4 client has none, however its address
// is written: every IPv4-mapped address lies in the one /64 ::ffff:0:0/64.
func prefixKey(key string) (string, bool) {
	ip, err := netip.ParseAddr(key)
	if err != nil || !ip.Is6() || ip.Is4In6() {
		return "", false
	}
	p, _ := ip.Prefix(64)
	return p.String(), true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/webui/ ./internal/daemon/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/webui` and `ok  	github.com/darkraise/ghr/internal/daemon`)

Then run the lockout tests under the race detector: `PATH=/usr/local/go/bin:$PATH go test -race -count=5 ./internal/webui/ -run 'Throttle|Rolling|Burst|IPv|Prefix|Locked|Lock|Refusal|SuccessClears|ClientKey|Swept|StopsWaiting'`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/webui`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/webui/auth.go internal/webui/auth_test.go
git commit -m "fix(webui): lock IPv6 logins by address and /64"
```

### Task 2: Auth request handling

**Files:**
- Modify: `internal/webui/handler.go` (`authBodyLimit`, `Handler`, `checks`)
- Test: `internal/webui/handler_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 50, 51
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

Three things are wrong with the `/auth` routes of the web listener. The body limit is 4 KiB, which two passwords of the maximum 1,024 bytes do not fit in when a client writes every byte as a six-byte JSON escape. A known path called with the wrong method (`GET /auth/login`) answers 404 `not found`. A body over the limit answers 400 `invalid request body`, the same as malformed JSON.

After this task the limit is 16 KiB. A known `/auth` path with another method answers 405 `method not allowed` with an `Allow` header naming what the path accepts; an unknown `/auth` path still answers 404. A body over the limit answers 413 `request body too large`; any other body defect stays 400 `invalid request body`. `GET /auth/state` is also served for `HEAD` by Go's router, so its `Allow` header is `GET, HEAD`, as the static handler in the same package already writes it.

- [ ] **Step 1: Write the failing tests**

All edits are in `internal/webui/handler_test.go`. Three assertions in `TestRequestChecks` hold the old 400 for an oversized body; the block below replaces them and adds a body of exactly the limit. In `TestRequestChecks` replace

```go
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
```

with

```go
	big := body(strings.Repeat("x", 17<<10))
	if rec := do(h, http.MethodPost, "/auth/login", big, nil); rec.Code != http.StatusRequestEntityTooLarge ||
		!strings.Contains(rec.Body.String(), "request body too large") {
		t.Fatalf("oversized body: %d %s", rec.Code, rec.Body.String())
	}
	atCap := body(pw) + strings.Repeat(" ", authBodyLimit-len(body(pw)))
	if rec := do(h, http.MethodPost, "/auth/login", atCap, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("valid JSON padded to the cap: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/login", atCap+" ", nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("valid JSON padded past the cap: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/auth/login", body(pw)+` {"password":"x"}`, nil); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "invalid request body") {
		t.Fatalf("trailing JSON: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, "/auth/logout", strings.Repeat("x", authBodyLimit+1), withCookie(id)); rec.Code != http.StatusRequestEntityTooLarge || !a.Valid(id) {
		t.Fatalf("oversized logout: %d, session valid %v", rec.Code, a.Valid(id))
	}
```

In `TestRoutes`, directly above

```go
	if rec := do(h, http.MethodPost, "/", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /: %d", rec.Code)
	}
```

add

```go
	for _, tc := range []struct{ method, path, allow string }{
		{http.MethodPost, "/auth/state", "GET, HEAD"},
		{http.MethodGet, "/auth/setup", "POST"},
		{http.MethodGet, "/auth/login", "POST"},
		{http.MethodPut, "/auth/logout", "POST"},
		{http.MethodDelete, "/auth/password", "POST"},
	} {
		rec := do(h, tc.method, tc.path, "", withCookie(id))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != tc.allow ||
			!strings.Contains(rec.Body.String(), `"error":"method not allowed"`) {
			t.Errorf("%s %s: %d Allow %q %s", tc.method, tc.path, rec.Code, rec.Header().Get("Allow"), rec.Body.String())
		}
	}
	if rec := do(h, http.MethodHead, "/auth/state", "", nil); rec.Code != http.StatusOK {
		t.Fatalf("HEAD /auth/state: %d", rec.Code)
	}
	if !a.Valid(id) {
		t.Fatal("a refused method ended the session")
	}
```

Directly above `func TestLogoutAndPasswordChange`, add the test below. Type the escape exactly as shown, in two parts: the constant `bs` is one backslash, and `"u0078"` follows it.

```go
// A JSON encoder may write any byte as a six-byte \u escape, so the body
// limit has to hold two passwords of the maximum length written that way.
func TestLongestPasswordsFitTheBodyLimit(t *testing.T) {
	h, a, _ := newTestHandler(t)
	id, err := a.Setup(strings.Repeat("x", MaxPasswordLen))
	if err != nil {
		t.Fatal(err)
	}
	// "x" and "y" as JSON escapes. The backslash stands apart so that no
	// editor or tool turns the escape back into the letter.
	const bs = `\`
	req := `{"current":"` + strings.Repeat(bs+"u0078", MaxPasswordLen) + `","new":"` + strings.Repeat(bs+"u0079", MaxPasswordLen) + `"}`
	if len(req) <= 12*MaxPasswordLen {
		t.Fatalf("the request is %d bytes; its passwords are not escaped", len(req))
	}
	if rec := do(h, http.MethodPost, "/auth/password", req, withCookie(id)); rec.Code != http.StatusNoContent {
		t.Fatalf("a %d-byte password change: %d %s", len(req), rec.Code, rec.Body.String())
	}
	if _, err := a.Login("10.0.0.1", strings.Repeat("y", MaxPasswordLen)); err != nil {
		t.Fatalf("the new password: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/webui/ -run 'TestRequestChecks|TestRoutes|TestLongestPasswordsFitTheBodyLimit'`
Expected: FAIL with

- `TestRequestChecks`: `oversized body: 400 {"error":"invalid request body"}`
- `TestRoutes`: `POST /auth/state: 404 Allow "" {"error":"not found"}` and the same for the four other rows
- `TestLongestPasswordsFitTheBodyLimit`: `a 12311-byte password change: 400 {"error":"invalid request body"}`

- [ ] **Step 3: Write the implementation**

All edits are in `internal/webui/handler.go`. The `errors` package is already imported.

Replace

```go
	cookieName    = "ghr_session"
	authBodyLimit = 4 << 10
```

with

```go
	cookieName = "ghr_session"
	// authBodyLimit holds two passwords of MaxPasswordLen with every byte
	// written as a six-byte JSON escape.
	authBodyLimit = 16 << 10
```

The `csp` line below them stays as it is.

In `Handler`, replace

```go
	mux.HandleFunc("GET /auth/state", h.state)
	mux.HandleFunc("POST /auth/setup", h.setup)
	mux.HandleFunc("POST /auth/login", h.login)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.HandleFunc("POST /auth/password", h.password)
	mux.HandleFunc("/auth/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
```

with

```go
	allow := map[string]string{}
	route := func(method, path string, fn http.HandlerFunc) {
		mux.HandleFunc(method+" "+path, fn)
		allow[path] = method
		if method == http.MethodGet {
			// The mux serves HEAD from a GET pattern.
			allow[path] = "GET, HEAD"
		}
	}
	route(http.MethodGet, "/auth/state", h.state)
	route(http.MethodPost, "/auth/setup", h.setup)
	route(http.MethodPost, "/auth/login", h.login)
	route(http.MethodPost, "/auth/logout", h.logout)
	route(http.MethodPost, "/auth/password", h.password)
	// Without this catch-all the mux would hand a wrong method or an unknown
	// /auth path to the static handler, which answers with the app's page.
	mux.HandleFunc("/auth/", func(w http.ResponseWriter, r *http.Request) {
		if methods, ok := allow[r.URL.Path]; ok {
			w.Header().Set("Allow", methods)
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		writeError(w, http.StatusNotFound, "not found")
	})
```

In the comment above `checks`, replace

```go
// POST body is read in full here, so the 4 KB cap holds for routes that
// ignore their body.
```

with

```go
// POST body is read in full here, so authBodyLimit holds for routes that
// ignore their body.
```

Inside `checks`, replace

```go
			data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, authBodyLimit))
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
```

with

```go
			data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, authBodyLimit))
			var tooLarge *http.MaxBytesError
			switch {
			case errors.As(err, &tooLarge):
				writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			case err != nil:
				writeError(w, http.StatusBadRequest, "invalid request body")
				return
			}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/webui/ ./internal/daemon/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/webui` and `ok  	github.com/darkraise/ghr/internal/daemon`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/webui/handler.go internal/webui/handler_test.go
git commit -m "fix(webui): answer 405 and 413 on /auth routes"
```

### Task 3: Per-call timeouts in the API client

**Files:**
- Modify: `internal/api/client.go` (`NewUnixClient`, `call`, `Kill`; new `callWithin`)
- Modify: `internal/daemon/run_test.go` (the `socketCall` helper only)
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: for Task 4, the three ways a `Client` method fails without an answer. When the caller's context ends, the method returns that context's own error, unwrapped (`ctx.Err()`, so `context.Canceled` after a cancel). When the call outlives its limit, it returns a plain error, wrapping nothing, with the text `ghr daemon did not answer within <n>s; the request may still be running`, where `<n>` is 30 for every call and 100 for `Kill`. Any other transport failure returns `ghr daemon unreachable: <error>`, wrapping the cause. The first two also hold when the call is cut short while it reads a response, an error response included. For Task 5, the `"testing/synctest"` import in `internal/api/api_test.go`.

**Items:** 48
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

Every `ghr` subcommand but `daemon` talks to the daemon through `api.Client`. Its HTTP client has one 30-second timeout for every request, and `call` reports every transport error as `ghr daemon unreachable`. Stopping a runner can take 90 seconds (systemd's default stop timeout), so `Kill` gives up while the daemon is still working. A caller that cancels its context is also told the daemon is unreachable.

After this task the client's HTTP client has no timeout and every call sets its own limit: 30 seconds by default, 100 seconds for `Kill`. A call tells its caller why it ended without an answer, in the three forms the Interfaces section lists. Nothing in `cmd/ghr` changes here; Task 4 turns the caller's context error into the CLI's message.

The limit and the caller's context cover the whole call, the reading of the response included. That matters most for an error response: `call` answers a 404 or 405 whose body is not JSON with the advice to restart an older daemon, and a body that was cut short is not JSON either. A call cut short there must report the cut, not that advice and not the bare status.

One test helper outside the package relied on the timeout this task removes: `socketCall` in `internal/daemon/run_test.go` sends raw requests through `NewUnixClient(socket).HTTP`. Step 4 gives its request a 30-second deadline, so a daemon that hangs in one of those tests fails the test instead of holding it until `go test -timeout`.

The tests need no real waiting. They replace the transport with a fake and run inside `testing/synctest`, whose clock is fake: inside `synctest.Test` a `time.After(time.Hour)` and a 30-second context deadline pass at once, in order.

- [ ] **Step 1: Write the failing client tests**

All edits in this step are in `internal/api/api_test.go`. Add the `testing/synctest` import. Replace

```go
	"testing/iotest"
	"time"
```

with

```go
	"testing/iotest"
	"testing/synctest"
	"time"
```

Replace the whole `TestUnreachableDaemon` function with:

```go
func TestUnreachableDaemon(t *testing.T) {
	c := NewUnixClient("/nonexistent/ghr.sock")
	if _, err := c.Status(context.Background()); err == nil || !strings.HasPrefix(err.Error(), "ghr daemon unreachable: ") {
		t.Fatalf("err %v", err)
	}
	if c.HTTP.Timeout != 0 {
		t.Fatalf("the client cuts every call after %v, also the ones with a longer limit of their own", c.HTTP.Timeout)
	}
}

// slowDaemon answers every request with 204 after its delay, or gives up when
// the request's context ends first, as a transport does.
type slowDaemon time.Duration

func (d slowDaemon) RoundTrip(r *http.Request) (*http.Response, error) {
	select {
	case <-time.After(time.Duration(d)):
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
}

// The bubble's clock is fake, so the limits under test are the real ones.
func TestCallTimeLimits(t *testing.T) {
	status := func(ctx context.Context, c *Client) error {
		_, err := c.Status(ctx)
		return err
	}
	kill := func(ctx context.Context, c *Client) error { return c.Kill(ctx, "a3f9c1") }
	for _, tc := range []struct {
		name    string
		answer  time.Duration
		call    func(context.Context, *Client) error
		want    string
		elapsed time.Duration
	}{
		{"a call answered in time", 29 * time.Second, status, "", 29 * time.Second},
		{"a call not answered", time.Hour, status, "ghr daemon did not answer within 30s; the request may still be running", 30 * time.Second},
		{"a stop that takes systemd's 90 seconds", 95 * time.Second, kill, "", 95 * time.Second},
		{"a stop not answered", time.Hour, kill, "ghr daemon did not answer within 100s; the request may still be running", 100 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := &Client{Base: "http://ghr", HTTP: &http.Client{Transport: slowDaemon(tc.answer)}}
				start := time.Now()
				err := tc.call(context.Background(), c)
				if got := time.Since(start); got != tc.elapsed {
					t.Errorf("returned after %v, want %v", got, tc.elapsed)
				}
				switch {
				case tc.want == "" && err != nil:
					t.Fatalf("err %v", err)
				case tc.want != "" && (err == nil || err.Error() != tc.want):
					t.Fatalf("err %v, want %s", err, tc.want)
				}
			})
		})
	}
}

// stalledBody is a response body that never arrives; the read gives up when
// the request's context ends.
type stalledBody struct{ ctx context.Context }

func (b stalledBody) Read([]byte) (int, error) {
	select {
	case <-time.After(time.Hour):
		return 0, io.EOF
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	}
}

func (stalledBody) Close() error { return nil }

// stalledDaemon answers at once with its status and then never sends the body.
type stalledDaemon int

func (d stalledDaemon) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: int(d), Status: fmt.Sprintf("%d %s", int(d), http.StatusText(int(d))),
		Header: http.Header{}, Body: stalledBody{r.Context()}, Request: r,
	}, nil
}

// A call cut short while it reads the response is reported like one cut
// short before it: by its limit, or by its caller. That holds for an error
// response too. Its unread body must not become an API error, nor the advice
// to restart an older daemon that a 404 or 405 without JSON gets.
func TestCallCutShortWhileReadingTheResponse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusInternalServerError} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("status %d, cancelled %v", status, cancelled), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c := &Client{Base: "http://ghr", HTTP: &http.Client{Transport: stalledDaemon(status)}}
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					if cancelled {
						time.AfterFunc(10*time.Second, cancel)
					}
					_, err := c.Status(ctx)
					const limit = "ghr daemon did not answer within 30s; the request may still be running"
					switch {
					case cancelled && err != context.Canceled:
						t.Fatalf("err %v, want the caller's own context error", err)
					case !cancelled && (err == nil || err.Error() != limit):
						t.Fatalf("err %v, want %s", err, limit)
					}
				})
			})
		}
	}
}

// A caller that gives up (Ctrl-C in the CLI) gets its own context's error
// back, not a report that the daemon cannot be reached.
func TestCallEndedByItsCaller(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := &Client{Base: "http://ghr", HTTP: &http.Client{Transport: slowDaemon(time.Hour)}}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(10*time.Second, cancel)
		start := time.Now()
		err := c.Kill(ctx, "a3f9c1")
		if err != context.Canceled || time.Since(start) != 10*time.Second {
			t.Fatalf("err %v after %v", err, time.Since(start))
		}
	})
}
```

- [ ] **Step 2: Run the client tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/api/ -run 'TestUnreachableDaemon|TestCallTimeLimits|TestCallEndedByItsCaller|TestCallCutShortWhileReadingTheResponse'`
Expected: FAIL with

- `TestUnreachableDaemon`: `the client cuts every call after 30s, also the ones with a longer limit of their own`
- `TestCallTimeLimits/a_call_not_answered`: `err <nil>, want ghr daemon did not answer within 30s; the request may still be running`
- `TestCallTimeLimits/a_stop_not_answered`: `err <nil>, want ghr daemon did not answer within 100s; the request may still be running`
- `TestCallCutShortWhileReadingTheResponse/status_200,_cancelled_false`: `err EOF, want ghr daemon did not answer within 30s; the request may still be running`
- `TestCallCutShortWhileReadingTheResponse/status_404,_cancelled_true`: `err the running ghr daemon does not know GET /status; it is older than this ghr, restart it: systemctl restart ghr, want the caller's own context error`
- `TestCallCutShortWhileReadingTheResponse/status_500,_cancelled_false`: `err 500 Internal Server Error, want ghr daemon did not answer within 30s; the request may still be running`
- `TestCallEndedByItsCaller`: `err ghr daemon unreachable: Delete "http://ghr/runners/a3f9c1": context canceled after 10s`

Seven of the eight rows of `TestCallCutShortWhileReadingTheResponse` fail; `status 200, cancelled true` passes already.

- [ ] **Step 3: Write the client implementation**

All edits in this step are in `internal/api/client.go`. No import changes.

Replace

```go
// NewUnixClient talks to the daemon over its Unix socket.
func NewUnixClient(socket string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}
	return &Client{Base: "http://ghr", HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}
}

func (c *Client) call(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ghr daemon unreachable: %w", err)
	}
	defer resp.Body.Close()
```

with

```go
const (
	callLimit = 30 * time.Second
	// killLimit is above systemd's default stop timeout of 90 seconds: the
	// daemon answers a kill once the runner's unit has stopped.
	killLimit = 100 * time.Second
)

// NewUnixClient talks to the daemon over its Unix socket. The HTTP client
// has no timeout of its own: every call sets its limit.
func NewUnixClient(socket string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}
	return &Client{Base: "http://ghr", HTTP: &http.Client{Transport: tr}}
}

func (c *Client) call(ctx context.Context, method, path string, body io.Reader, out any) error {
	return c.callWithin(ctx, callLimit, method, path, body, out)
}

// callWithin gives the daemon limit to answer, response body included.
func (c *Client) callWithin(ctx context.Context, limit time.Duration, method, path string, body io.Reader, out any) error {
	callCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	// early names the reason when the call was cut short rather than failed:
	// the caller's context, returned as it is, or the call's own limit.
	early := func() error {
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case callCtx.Err() != nil:
			return fmt.Errorf("ghr daemon did not answer within %ds; the request may still be running", int(limit/time.Second))
		}
		return nil
	}
	req, err := http.NewRequestWithContext(callCtx, method, c.Base+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if e := early(); e != nil {
			return e
		}
		return fmt.Errorf("ghr daemon unreachable: %w", err)
	}
	defer resp.Body.Close()
```

In the block that handles `resp.StatusCode >= 300`, directly below, replace

```go
		derr := json.NewDecoder(resp.Body).Decode(&e)
```

with

```go
		derr := json.NewDecoder(resp.Body).Decode(&e)
		if derr != nil {
			// An error body cut short says nothing about the daemon.
			if cut := early(); cut != nil {
				return cut
			}
		}
```

The rest of that block stays: the comment about a plain-text 404 or 405 and what follows it. At the end of the same function replace

```go
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
```

with

```go
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		if e := early(); e != nil {
			return e
		}
		return err
	}
	return nil
}
```

Replace

```go
func (c *Client) Kill(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, "/runners/"+url.PathEscape(id), nil, nil)
}
```

with

```go
func (c *Client) Kill(ctx context.Context, id string) error {
	return c.callWithin(ctx, killLimit, http.MethodDelete, "/runners/"+url.PathEscape(id), nil, nil)
}
```

Every other method keeps calling `c.call`, which now applies the 30-second limit.

- [ ] **Step 4: Give the daemon tests' socket helper its own limit**

This step changes one test helper and has no test of its own. In `internal/daemon/run_test.go`, touch nothing but the top of `socketCall`. The `context` and `time` packages are already imported there. Replace

```go
// socketCall is a raw request over the Unix socket.
func socketCall(socket, method, path, body string) (int, string, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, "http://ghr"+path, rd)
	resp, err := api.NewUnixClient(socket).HTTP.Do(req)
```

with

```go
// socketCall is a raw request over the Unix socket. The client's HTTP client
// has no timeout, so the request carries its own.
func socketCall(socket, method, path, body string) (int, string, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, method, "http://ghr"+path, rd)
	resp, err := api.NewUnixClient(socket).HTTP.Do(req)
```

The rest of the function stays as it is.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/api/ ./cmd/ghr/ ./internal/daemon/`
Expected: PASS (`ok` for all three packages). `cmd/ghr` is not edited in this task; it is run because it builds on the client.

- [ ] **Step 6: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/api/client.go internal/api/api_test.go internal/daemon/run_test.go
git commit -m "fix(api): give each client call its own time limit"
```

### Task 4: The CLI names why a call ended

**Files:**
- Modify: `cmd/ghr/main.go` (the `newClient` block, `run`)
- Test: `cmd/ghr/main_test.go`

**Interfaces:**
- Consumes: from Task 3, what a `Client` method returns when it gets no answer. A cancelled caller gets `context.Canceled` itself, so `errors.Is(err, context.Canceled)` holds and the error's text is `context canceled`. A call that outlived its limit gets a plain error whose whole text is `ghr daemon did not answer within 30s; the request may still be running`, or `100s` for `Kill`. Every other transport failure gets `ghr daemon unreachable: <error>`. This task's test also sets `api.Client`'s two exported fields, `Base` and `HTTP`, to put a fake transport in.
- Produces: `run` in `cmd/ghr/main.go` prints `ghr: interrupted` and exits 1 for any error for which `errors.Is(err, context.Canceled)` holds; the variable `interruptContext` in `cmd/ghr/main.go`.

**Items:** 48
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`run` in `cmd/ghr/main.go` prints every error of a subcommand as `ghr: <error>` and exits 1. Since Task 3 the client says why a call got no answer, so two of the three cases already read well: `ghr: ghr daemon did not answer within 30s; the request may still be running` and `ghr: ghr daemon unreachable: <error>`. The third does not: Ctrl-C prints `ghr: context canceled`.

After this task Ctrl-C prints `ghr: interrupted`. The exit code stays 1 for all three. `ghr logs -f` still ends silently with exit 0 on Ctrl-C, because `logs` already returns nil then.

`run` builds its context from the interrupt signal, which a test cannot cancel, so the task first puts that behind a variable the test replaces. The test runs inside `testing/synctest`, whose clock is fake: the fake daemon's hour and the client's 30-second and 100-second limits pass at once.

- [ ] **Step 1: Write the failing test**

Add the variable first; this alone changes no behaviour. In `cmd/ghr/main.go` replace

```go
// newClient is replaced in tests.
var newClient = func() *api.Client { return api.NewUnixClient(socketPath()) }
```

with

```go
// newClient and interruptContext are replaced in tests. interruptContext
// returns the context Ctrl-C cancels.
var (
	newClient        = func() *api.Client { return api.NewUnixClient(socketPath()) }
	interruptContext = func() (context.Context, context.CancelFunc) {
		return signal.NotifyContext(context.Background(), os.Interrupt)
	}
)
```

and, in `run`, replace

```go
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
```

with

```go
	ctx, stop := interruptContext()
```

Leave the `daemon` case above it alone: its `signal.NotifyContext` call also takes `syscall.SIGTERM` and is a different line.

In `cmd/ghr/main_test.go` replace the import block

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

with

```go
import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/darkraise/ghr/internal/api"
)
```

and add, directly above `func TestBareGhrPrintsUsageWithoutTerminal`:

```go
// hungDaemon holds every request for an hour, or until the request's context
// ends, as a transport does. onRequest runs when a request arrives.
type hungDaemon struct{ onRequest func() }

func (d hungDaemon) RoundTrip(r *http.Request) (*http.Response, error) {
	d.onRequest()
	select {
	case <-time.After(time.Hour):
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
}

// The bubble's clock is fake, so the daemon's hour and the client's limits
// pass at once.
func TestRunNamesWhyACallEndedEarly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		ctrlC  bool
		stderr string
	}{
		{"Ctrl-C", []string{"kill", "a3f9c1"}, true, "ghr: interrupted\n"},
		{"no answer", []string{"status"}, false, "ghr: ghr daemon did not answer within 30s; the request may still be running\n"},
		{"no answer to a kill", []string{"kill", "a3f9c1"}, false, "ghr: ghr daemon did not answer within 100s; the request may still be running\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				oldClient, oldContext := newClient, interruptContext
				t.Cleanup(func() { newClient, interruptContext = oldClient, oldContext })
				interruptContext = func() (context.Context, context.CancelFunc) { return ctx, cancel }
				newClient = func() *api.Client {
					return &api.Client{Base: "http://ghr", HTTP: &http.Client{Transport: hungDaemon{func() {
						if tc.ctrlC {
							cancel()
						}
					}}}}
				}
				var out, errb bytes.Buffer
				if code := run(tc.args, strings.NewReader(""), &out, &errb); code != 1 || errb.String() != tc.stderr || out.Len() != 0 {
					t.Fatalf("exit %d stdout %q stderr %q, want exit 1 and stderr %q", code, out.String(), errb.String(), tc.stderr)
				}
			})
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./cmd/ghr/ -run 'TestRunNamesWhyACallEndedEarly'`
Expected: FAIL with `exit 1 stdout "" stderr "ghr: context canceled\n", want exit 1 and stderr "ghr: interrupted\n"`. Only the `Ctrl-C` row fails. The two `no answer` rows pass already: they pin the texts Task 3 produces as they reach the terminal.

- [ ] **Step 3: Write the implementation**

In `run` in `cmd/ghr/main.go` replace

```go
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		fmt.Fprintln(stderr, "ghr:", err)
```

with

```go
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return 0
		}
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(stderr, "ghr: interrupted")
			return 1
		}
		fmt.Fprintln(stderr, "ghr:", err)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/api/ ./cmd/ghr/ ./internal/daemon/`
Expected: PASS (`ok` for all three packages)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add cmd/ghr/main.go cmd/ghr/main_test.go
git commit -m "fix(cli): name why a daemon call ended early"
```

### Task 5: The kill route's write deadline

**Files:**
- Modify: `internal/api/server.go` (the `DELETE /runners/{id}` route; new `killWriteWindow`)
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: the `"testing/synctest"` import Task 3 added to `internal/api/api_test.go`. If it is not there, add it between `"testing/iotest"` and `"time"`.
- Produces: nothing a later task names

**Items:** 48
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The browser reaches the API through the web listener, whose `WriteTimeout` is 60 seconds (`internal/daemon/run.go`, not edited here). The route that stops a runner waits for the runner's systemd unit to stop, which can take 90 seconds, so the listener cuts the response and the browser sees a failure while the stop goes on.

After this task the route sets its own write deadline, 100 seconds from its start, before it begins the stop, but only on a server that has a write timeout. It does so through `http.NewResponseController`, the way `streamChanges` and `streamLog` in the same file already extend theirs, and ignores the error: a writer without deadline support (a test recorder) returns one, and then nothing needs doing.

The same handler also serves the Unix socket, whose server sets no `WriteTimeout`. A kill over the socket had no write deadline and must keep none, so the handler must not set one there. It tells the two apart without a new parameter: Go puts the `*http.Server` that accepted a connection into every request's context under `http.ServerContextKey`, and the handler extends the deadline only when that server's `WriteTimeout` is above zero. A request with no server in its context (a handler called directly, as most tests do) gets no deadline either.

- [ ] **Step 1: Write the failing test**

All edits in this step are in `internal/api/api_test.go`. The fake backend needs a hook that runs while the stop is under way. In the `fakeBackend` struct replace

```go
	killed        []string
	killErr       error
	pruneErr      error
```

with

```go
	killed        []string
	killErr       error
	onKill        func() // runs inside KillRunner
	pruneErr      error
```

and replace

```go
func (f *fakeBackend) KillRunner(ctx context.Context, id string) error {
	f.killed = append(f.killed, id)
	return f.killErr
}
```

with

```go
func (f *fakeBackend) KillRunner(ctx context.Context, id string) error {
	f.killed = append(f.killed, id)
	if f.onKill != nil {
		f.onKill()
	}
	return f.killErr
}
```

Directly above `func TestUnreachableDaemon`, add:

```go
// deadlineWriter records the write deadline a handler sets, as the web
// listener's connection would take it.
type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *deadlineWriter) SetWriteDeadline(t time.Time) error {
	w.deadline = t
	return nil
}

// servedBy is a request as srv's connection would hand it to a handler: the
// server is in its context. A nil srv leaves the context without one.
func servedBy(srv *http.Server, method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	if srv == nil {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), http.ServerContextKey, srv))
}

// A stop can take systemd's 90 seconds, longer than the web listener's
// WriteTimeout, so the kill route sets its own write deadline before it
// starts the stop. The Unix socket's server has no write timeout: its
// responses had no deadline and must keep none. The bubble's clock stands
// still, so the deadline is exact.
func TestKillRouteExtendsOnlyAServersWriteTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		web, socket := &http.Server{WriteTimeout: 60 * time.Second}, &http.Server{}
		const kill = "/runners/a3f9c1"
		for _, tc := range []struct {
			name string
			req  *http.Request
			want time.Duration // 0 is no deadline
		}{
			{"a kill on a server with a write timeout", servedBy(web, http.MethodDelete, kill), 100 * time.Second},
			{"a kill on a server without one", servedBy(socket, http.MethodDelete, kill), 0},
			{"a kill outside a server", servedBy(nil, http.MethodDelete, kill), 0},
			{"another route on a server with a write timeout", servedBy(web, http.MethodDelete, "/repos/old"), 0},
		} {
			b := &fakeBackend{}
			w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
			atKill := w.deadline
			b.onKill = func() { atKill = w.deadline }
			NewServer(b).ServeHTTP(w, tc.req)
			var want time.Time
			if tc.want != 0 {
				want = time.Now().Add(tc.want)
			}
			left := func(deadline time.Time) string {
				if deadline.IsZero() {
					return "none"
				}
				return time.Until(deadline).String()
			}
			if w.Code != http.StatusNoContent || !w.deadline.Equal(want) || !atKill.Equal(want) {
				t.Errorf("%s: status %d, write deadline %s (%s when the stop began), want %s",
					tc.name, w.Code, left(w.deadline), left(atKill), left(want))
			}
		}
		// A recorder cannot take a write deadline; the stop must still run.
		b := &fakeBackend{}
		plain := httptest.NewRecorder()
		NewServer(b).ServeHTTP(plain, servedBy(web, http.MethodDelete, kill))
		if plain.Code != http.StatusNoContent || len(b.killed) != 1 {
			t.Fatalf("a writer without deadlines: status %d, kills %v", plain.Code, b.killed)
		}
	})
}
```

`servedBy` builds the request a server's connection would hand to the handler. The test runs inside `synctest.Test`, where the clock stands still, so the deadline can be compared exactly.

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/api/ -run 'TestKillRouteExtendsOnlyAServersWriteTimeout'`
Expected: FAIL with `a kill on a server with a write timeout: status 204, write deadline none (none when the stop began), want 1m40s`. The three other rows pass already: they are the cases that must keep having no deadline, and they fail if Step 3 sets the deadline without looking at the server.

- [ ] **Step 3: Write the implementation**

All edits are in `internal/api/server.go`. In `NewServer` replace

```go
	mux.HandleFunc("DELETE /runners/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.KillRunner(r.Context(), r.PathValue("id")))
	})
```

with

```go
	mux.HandleFunc("DELETE /runners/{id}", func(w http.ResponseWriter, r *http.Request) {
		// Only a server that cuts its responses needs the longer deadline. The
		// Unix socket's server sets no WriteTimeout and keeps no deadline.
		if srv, ok := r.Context().Value(http.ServerContextKey).(*http.Server); ok && srv.WriteTimeout > 0 {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(killWriteWindow))
		}
		respond(w, nil, b.KillRunner(r.Context(), r.PathValue("id")))
	})
```

Directly above the comment that begins `// rawWriteWindow bounds a raw log request`, add (followed by one empty line):

```go
// killWriteWindow bounds the answer to a kill from the route's start, on a
// server that has a WriteTimeout. The daemon waits for the runner's unit to
// stop, which systemd allows 90 seconds by default, longer than the web
// listener's WriteTimeout.
const killWriteWindow = 100 * time.Second
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/api/ ./internal/daemon/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/api` and `ok  	github.com/darkraise/ghr/internal/daemon`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/api/server.go internal/api/api_test.go
git commit -m "fix(api): extend the kill route's write deadline"
```

### Task 6: Token prompt and argument checks

**Files:**
- Modify: `cmd/ghr/cli.go` (the `status` and `token` cases of `cli`, the `add` case of `repo`)
- Modify: `cmd/ghr/main.go` (one line of the usage text)
- Test: `cmd/ghr/cli_test.go`

**Interfaces:**
- Consumes: nothing from an earlier task. `readToken` already exists in `cmd/ghr/setup.go`.
- Produces: nothing a later task names

**Items:** 54, 55
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

Three small CLI defects. `ghr token set` always reads the token from stdin, so at a terminal the token is typed in the clear. `ghr repo add` takes its first argument as the repository name before it looks at flags, so `ghr repo add -h` tries to add a repository named `-h`. `ghr status` ignores further arguments.

After this task `ghr token set` at a terminal asks with `readToken`, the no-echo prompt `ghr setup github` already uses; with a pipe on stdin it reads stdin to the end, as before. `ghr repo add` parses its flags first, so `-h` prints the usage, flags may stand before or after the name, and a name that begins with `-` is a usage error. `ghr status` with any further argument is a usage error. A usage error exits 2, as before.

- [ ] **Step 1: Write the failing tests**

All edits in this step are in `cmd/ghr/cli_test.go`.

Replace the whole `TestRepoAddMaxFlag` function with:

```go
func TestRepoAddMaxFlag(t *testing.T) {
	reqs := fakeDaemon(t)
	for _, args := range [][]string{
		{"repo", "add", "r1"},
		{"repo", "add", "r2", "--max", "0"},
		{"repo", "add", "r3", "--max", "3"},
	} {
		if code, _, errOut := runCLI(t, "", args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
	}
	want := []string{
		`{"name":"r1","allow_public":false}`,
		`{"name":"r2","max":0,"allow_public":false}`,
		`{"name":"r3","max":3,"allow_public":false}`,
		`{"name":"r4","max":4,"labels":["a"],"allow_public":false}`,
		`{"name":"r5","max":5,"allow_public":true}`,
	}
	for _, args := range [][]string{
		{"repo", "add", "--max", "4", "--label", "a", "r4"},
		{"repo", "add", "--allow-public", "r5", "--max", "5"},
	} {
		if code, _, errOut := runCLI(t, "", args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
	}
	if len(*reqs) != len(want) {
		t.Fatalf("%d requests, want %d: %v", len(*reqs), len(want), *reqs)
	}
	for i, w := range want {
		if got := strings.TrimSpace((*reqs)[i].body); got != w {
			t.Errorf("request %d: got %s want %s", i, got, w)
		}
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"repo", "add", "r6", "--max", "-2"}, "--max must be >= 0"},
		{[]string{"repo", "add", "r7", "--max", "-1"}, "--max must be >= 0"},
		{[]string{"repo", "add", "r8", "extra"}, "unexpected argument extra"},
		{[]string{"repo", "add", "--max", "2"}, "usage: ghr repo add <name>"},
		{[]string{"repo", "add", "--", "-r9"}, "repo name must not begin with -: -r9"},
		{[]string{"repo", "add", "-"}, "repo name must not begin with -: -"},
		{[]string{"repo", "add", "--bogus"}, "flag provided but not defined: -bogus"},
	} {
		if code, _, errOut := runCLI(t, "", tc.args...); code != 2 || !strings.Contains(errOut, "ghr: "+tc.want) {
			first, _, _ := strings.Cut(errOut, "\n")
			t.Errorf("%v: exit %d stderr %q, want exit 2 and %q", tc.args, code, first, tc.want)
		}
	}
	if len(*reqs) != len(want) {
		t.Fatalf("rejected adds reached the daemon: %v", *reqs)
	}
	code, out, errOut := runCLI(t, "", "repo", "add", "-h")
	if code != 0 || !strings.Contains(out, "usage: ghr") || errOut != "" || len(*reqs) != len(want) {
		t.Fatalf("repo add -h: exit %d stdout %d bytes stderr %q requests %v", code, len(out), errOut, *reqs)
	}
}
```

Replace the whole `TestTokenSetReadsStdin` function with the two functions below. The first is the old test plus one line, `stubPasswordInput(t, false)`: the command now asks whether stdin is a terminal, and the test must not depend on how the test binary was started.

```go
func TestTokenSetReadsStdin(t *testing.T) {
	reqs := fakeDaemon(t)
	stubPasswordInput(t, false)
	if code, _, _ := runCLI(t, "github_pat_new\n", "token", "set"); code != 0 {
		t.Fatal("token set failed")
	}
	if r := (*reqs)[0]; r.method != "PUT" || r.path != "/token" || r.body != "github_pat_new" {
		t.Fatalf("request %+v", r)
	}
}

// At a terminal the token is asked for without echo, so it stays out of the
// screen and the scrollback.
func TestTokenSetPromptsAtATerminal(t *testing.T) {
	reqs := fakeDaemon(t)
	stubPasswordInput(t, true, " github_pat_typed ")
	code, out, errOut := runCLI(t, "not read\n", "token", "set")
	if code != 0 || out != "" || errOut != "GitHub token: \n" {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
	if r := (*reqs)[0]; r.method != "PUT" || r.path != "/token" || r.body != "github_pat_typed" {
		t.Fatalf("request %+v", r)
	}
}
```

In `TestDrainAndResumeAllTakeNoArguments` replace

```go
	for _, args := range [][]string{{"drain", "darkcloud"}, {"resume-all", "darkcloud"}} {
```

with

```go
	for _, args := range [][]string{{"drain", "darkcloud"}, {"resume-all", "darkcloud"}, {"status", "darkcloud"}} {
```

Every test here calls `fakeDaemon(t)`, so no command reaches a real daemon.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./cmd/ghr/ -run 'TestRepoAddMaxFlag|TestTokenSet|TestDrainAndResumeAllTakeNoArguments'`
Expected: FAIL with

- `TestRepoAddMaxFlag`: `[repo add --max 4 --label a r4]: exit 2: ghr: unexpected argument 4`, followed by the usage text
- `TestTokenSetPromptsAtATerminal`: `exit 0 out "" err ""`
- `TestDrainAndResumeAllTakeNoArguments`: `[status darkcloud]: exit 0, want 2`

- [ ] **Step 3: Write the implementation**

In `cmd/ghr/cli.go`, in `cli`, replace

```go
	case "status":
		st, err := c.Status(ctx)
```

with

```go
	case "status":
		if err := need(args, 1, "status"); err != nil {
			return err
		}
		st, err := c.Status(ctx)
```

In the `token` case of the same function replace

```go
		data, err := io.ReadAll(io.LimitReader(stdin, 64*1024))
		if err != nil {
			return err
		}
		return c.SetToken(ctx, strings.TrimSpace(string(data)))
```

with

```go
		token, err := readToken(ctx, stdin, errOut)
		if err != nil {
			return err
		}
		return c.SetToken(ctx, token)
```

`readToken` trims the token itself. The `io` and `strings` imports stay: other code in the file uses them.

In the `add` case of `repo` replace

```go
		fs.Var(&labels, "label", "")
		if err := fs.Parse(args[2:]); err != nil {
			return flagError(err)
		}
		if fs.NArg() > 0 {
			return usageError("unexpected argument " + fs.Arg(0))
		}
		req := model.AddRepoRequest{Name: args[1], Labels: labels, AllowPublic: *allowPublic}
```

with

```go
		fs.Var(&labels, "label", "")
		// The flag package stops at the first argument that is not a flag,
		// so the flags after the name need a second pass.
		if err := fs.Parse(args[1:]); err != nil {
			return flagError(err)
		}
		name := fs.Arg(0)
		switch {
		case name == "":
			return usageError("usage: ghr repo add <name> [--max n] [--label l]... [--allow-public]")
		case strings.HasPrefix(name, "-"):
			return usageError("repo name must not begin with -: " + name)
		}
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return flagError(err)
		}
		if fs.NArg() > 0 {
			return usageError("unexpected argument " + fs.Arg(0))
		}
		req := model.AddRepoRequest{Name: name, Labels: labels, AllowPublic: *allowPublic}
```

In the usage text in `cmd/ghr/main.go` replace

```go
  token set                       read a new PAT from stdin
```

with

```go
  token set                       set a new PAT (prompts, or reads stdin)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./cmd/ghr/`
Expected: PASS (`ok  	github.com/darkraise/ghr/cmd/ghr`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add cmd/ghr/cli.go cmd/ghr/cli_test.go cmd/ghr/main.go
git commit -m "fix(cli): prompt for the token, check arguments"
```

### Task 7: Ctrl-C at a password prompt

**Files:**
- Modify: `cmd/ghr/password.go` (the test-seam variables, `askPassword`; new `errInterrupted`, `hideTerminal`, `readHidden`)
- Create: `cmd/ghr/password_linux_test.go`
- Test: `cmd/ghr/cli_test.go`

**Interfaces:**
- Consumes: nothing a task must add first. With Task 4 in place, `run` prints `ghr: interrupted` when the command's context was cancelled from outside the prompt; without it that text is `ghr: context canceled`. Ctrl-C typed at the prompt prints `ghr: interrupted` either way.
- Produces: nothing a later task names

**Items:** 56
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 3 = 4

`askPassword` prints a prompt and reads one line from the terminal with echo off. It is used by `ghr web set-password`, `ghr setup github` and `ghr token set`. Today it calls `term.ReadPassword`, which turns echo off, blocks in the read and turns echo on again when the read returns. Ctrl-C cancels the command's context, but `askPassword` looks at the context only after the read has returned, so nothing happens until the user presses Enter.

After this task the read runs on its own goroutine, and `askPassword` returns as soon as the read or the context has ended. The blocked read is left behind: the process is about to exit.

The rule that makes this safe: **only `askPassword` changes the terminal's mode, on its own goroutine; the goroutine that reads changes none.** If the reading goroutine turned echo off itself, as `term.ReadPassword` does, a cancel could arrive before it had done so: the prompt would put the terminal back, the read would then turn echo off, and the process would exit with the terminal left that way. So `askPassword` hides the input before it starts the read and puts the terminal back after the read or the context has ended. Both happen in one function, in that order, on every path.

The golang.org/x/term package can hide input from the caller's side in one portable way, raw mode (`term.MakeRaw`, undone by `term.Restore`). Two things follow from raw mode. The line must be read by code that understands raw input (Enter arrives as a carriage return, Backspace as a byte): the package's own `term.Terminal` does that. And the terminal no longer turns Ctrl-C into a signal: Ctrl-C, like Ctrl-D on an empty line, arrives as a byte, and `term.Terminal` reports it as end of input. The new `readHidden` turns that into the error `errInterrupted`, whose text is `interrupted`, so `run` prints `ghr: interrupted` and exits 1. The context still ends the prompt when the interrupt comes from elsewhere (`kill -INT`).

Three seams let tests stand in for the terminal: `stdinIsTerminal` (unchanged), `readPassword` (now only reads) and the new `hideInput`.

- [ ] **Step 1: Write the failing tests of the reader**

In `cmd/ghr/cli_test.go`, directly above `func TestStatusWarnsWhileTheWebUIHasNoPassword`, add the two tests below. Type the escapes exactly: `\x7f` is Backspace, `\x03` is Ctrl-C, `\x04` is Ctrl-D.

```go
// readHidden reads what a terminal in raw mode delivers: Enter arrives as a
// carriage return, and Ctrl-C and Ctrl-D arrive as bytes, not as a signal.
func TestReadHidden(t *testing.T) {
	for _, tc := range []struct {
		name, typed, want string
		err               error
	}{
		{"a line ended by Enter", "a long secret\r", "a long secret", nil},
		{"a line ended by a line feed", "a long secret\n", "a long secret", nil},
		{"spaces are kept", " a long secret \r", " a long secret ", nil},
		{"backspace removes a character", "a lomg\x7f\x7fng secret\r", "a long secret", nil},
		{"Ctrl-C", "\x03", "", errInterrupted},
		{"Ctrl-C after some typing", "a long\x03", "", errInterrupted},
		{"Ctrl-D on an empty line", "\x04", "", errInterrupted},
		{"the terminal goes away", "", "", errInterrupted},
	} {
		got, err := readHidden(strings.NewReader(tc.typed))
		if string(got) != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("%s: read %q err %v, want %q and %v", tc.name, got, err, tc.want, tc.err)
		}
	}
}

func TestCtrlCAtAPromptPrintsInterrupted(t *testing.T) {
	reqs := fakeDaemon(t)
	stubPasswordInput(t, true)
	readPassword = func() ([]byte, error) { return readHidden(strings.NewReader("a long\x03")) }
	code, out, errOut := runCLI(t, "", "web", "set-password")
	if code != 1 || out != "" || errOut != "New web password: \nghr: interrupted\n" || len(*reqs) != 0 {
		t.Fatalf("exit %d out %q err %q requests %v", code, out, errOut, *reqs)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./cmd/ghr/ -run 'TestReadHidden|TestCtrlCAtAPromptPrintsInterrupted'`
Expected: FAIL. The package does not build: `undefined: errInterrupted` and `undefined: readHidden`, then `FAIL	github.com/darkraise/ghr/cmd/ghr [build failed]`.

- [ ] **Step 3: Write the reader**

In `cmd/ghr/password.go`, directly above the comment `// stdinIsTerminal and readPassword are replaced in tests.`, add (followed by one empty line):

```go
// errInterrupted reports Ctrl-C or Ctrl-D at a prompt. In raw mode the
// terminal hands both to the reader as bytes and raises no signal.
var errInterrupted = errors.New("interrupted")
```

and directly above the comment `// maxPipedPassword bounds a password read from a pipe`, add (followed by one empty line):

```go
// readHidden reads one line typed into a terminal that is in raw mode. It
// changes no terminal mode itself: askPassword must stay the only code that
// does, or a cancelled prompt could put the terminal back too early.
func readHidden(in io.Reader) ([]byte, error) {
	rw := struct {
		io.Reader
		io.Writer
	}{in, io.Discard}
	line, err := term.NewTerminal(rw, "").ReadPassword("")
	if errors.Is(err, io.EOF) {
		return nil, errInterrupted
	}
	return []byte(line), err
}
```

No import changes: the file already imports `errors`, `io` and `golang.org/x/term`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./cmd/ghr/`
Expected: PASS (`ok  	github.com/darkraise/ghr/cmd/ghr`)

- [ ] **Step 5: Write the failing test of the prompt**

Add the seam for hiding the input first, not yet used, so the test compiles. In `cmd/ghr/password.go` replace

```go
// stdinIsTerminal and readPassword are replaced in tests.
var (
	stdinIsTerminal = func() bool { return fdIsTerminal(os.Stdin.Fd()) }
	readPassword    = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }
)
```

with

```go
// stdinIsTerminal, hideInput and readPassword are replaced in tests.
var (
	stdinIsTerminal = func() bool { return fdIsTerminal(os.Stdin.Fd()) }
	hideInput       = func() (func(), error) { return hideTerminal(os.Stdin) }
	readPassword    = func() ([]byte, error) { return readHidden(os.Stdin) }
)
```

and add `hideTerminal` directly below that block, above `readHidden`:

```go
// hideTerminal puts f's terminal into raw mode, which echoes nothing, and
// returns the function that puts back the state it found.
func hideTerminal(f *os.File) (func(), error) {
	fd := int(f.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() { term.Restore(fd, state) }, nil
}
```

Between this step and Step 7 the real prompt would read without hiding the input. Nothing is committed in between, and the tests replace both variables.

The remaining edits of this step are in `cmd/ghr/cli_test.go`. In the import block replace

```go
	"net/http/httptest"
	"strings"
	"testing"
	"time"
```

with

```go
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
```

Replace the whole `stubPasswordInput` function, including its comment, with the version below. It now also replaces `hideInput`: under `go test` stdin is not a terminal, and the real one would fail.

```go
// stubPasswordInput makes stdin look like a terminal, or not, and feeds the
// no-echo reads from answers. There is no terminal whose input could be
// hidden.
func stubPasswordInput(t *testing.T, terminal bool, answers ...string) {
	t.Helper()
	oldTerm, oldRead, oldHide := stdinIsTerminal, readPassword, hideInput
	stdinIsTerminal = func() bool { return terminal }
	hideInput = func() (func(), error) { return func() {}, nil }
	readPassword = func() ([]byte, error) {
		if len(answers) == 0 {
			return nil, io.EOF
		}
		a := answers[0]
		answers = answers[1:]
		return []byte(a), nil
	}
	t.Cleanup(func() { stdinIsTerminal, readPassword, hideInput = oldTerm, oldRead, oldHide })
}
```

Directly above the comment that begins `// readHidden reads what a terminal in raw mode delivers`, add:

```go
// askPassword is the only code that changes the terminal's mode: it hides
// the input before the read starts and puts the terminal back when the read
// or the context has ended, whichever is first. Whenever the context ends,
// also before the read has started, the change back must be the last one,
// and the read left behind must change nothing after it.
func TestPasswordPromptAlwaysPutsTheTerminalBack(t *testing.T) {
	const beforePrompt, whileHidden, whileReading = "before the prompt", "while the input is hidden", "while the read waits"
	failed := errors.New("stdin closed")
	for _, tc := range []struct {
		name   string
		cancel string
		read   func() ([]byte, error) // nil waits for an Enter that comes too late
		want   string
		err    error
	}{
		{"the read returns", "", func() ([]byte, error) { return []byte("a long secret"), nil }, "a long secret", nil},
		{"the read fails", "", func() ([]byte, error) { return nil, failed }, "", failed},
		{"cancelled before the prompt", beforePrompt, nil, "", context.Canceled},
		{"cancelled before the read starts", whileHidden, nil, "", context.Canceled},
		{"cancelled while the read waits", whileReading, nil, "", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				stubPasswordInput(t, true)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				terminal, changes := "saved", []string(nil)
				hideInput = func() (func(), error) {
					terminal, changes = "raw", append(changes, "raw")
					if tc.cancel == whileHidden {
						cancel()
					}
					return func() { terminal, changes = "saved", append(changes, "saved") }, nil
				}
				enter := make(chan struct{})
				pressEnter := sync.OnceFunc(func() { close(enter) })
				defer pressEnter()
				if readPassword = tc.read; tc.read == nil {
					readPassword = func() ([]byte, error) {
						<-enter
						return []byte("typed too late"), nil
					}
				}
				if tc.cancel == beforePrompt {
					cancel()
				}
				var prompts bytes.Buffer
				var got string
				var err error
				returned := make(chan struct{})
				go func() {
					got, err = askPassword(ctx, &prompts, "New web password: ")
					close(returned)
				}()
				synctest.Wait()
				if tc.cancel == whileReading {
					if terminal != "raw" || prompts.String() != "New web password: " {
						t.Errorf("while the read waits: terminal %s, prompts %q", terminal, prompts.String())
					}
					cancel()
					synctest.Wait()
				}
				select {
				case <-returned:
				default:
					pressEnter()
					<-returned
					t.Fatal("the prompt still waits for the read after its context ended")
				}
				if got != tc.want || !errors.Is(err, tc.err) {
					t.Errorf("password %q err %v, want %q and %v", got, err, tc.want, tc.err)
				}
				pressEnter()
				synctest.Wait()
				if terminal != "saved" || !slices.Equal(changes, []string{"raw", "saved"}) || prompts.String() != "New web password: \n" {
					t.Fatalf("terminal %s after the changes %v, prompts %q", terminal, changes, prompts.String())
				}
			})
		})
	}
}
```

The test models the terminal as a string that only `hideInput` and the function it returns change. Its row `cancelled before the read starts` cancels the context from inside `hideInput`, before the reading goroutine exists. After every row the test lets the read that was left behind finish and then checks the terminal once more. `synctest.Wait()` returns once every other goroutine of the test is blocked, so the test needs no sleep to know that the prompt is waiting.

- [ ] **Step 6: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./cmd/ghr/`
Expected: FAIL in all five rows of `TestPasswordPromptAlwaysPutsTheTerminalBack`. The rows `the read returns` and `the read fails` print `terminal saved after the changes [], prompts "New web password: \n"`; the three cancelled rows print `the prompt still waits for the read after its context ended`. Every other test of the package passes.

- [ ] **Step 7: Write the implementation**

In `cmd/ghr/password.go` replace the whole `askPassword` function, including its comment,

```go
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

with

```go
// askPassword reads one line without echo. It alone changes the terminal's
// mode, on this goroutine: it hides the input before the read starts and puts
// the terminal back once the read or ctx has ended, whichever is first. The
// read blocks and does not see ctx, so it runs on its own goroutine and, when
// ctx ends first, is left behind for the exiting process.
func askPassword(ctx context.Context, prompts io.Writer, prompt string) (string, error) {
	restore, err := hideInput()
	if err != nil {
		return "", err
	}
	fmt.Fprint(prompts, prompt)
	type answer struct {
		b   []byte
		err error
	}
	// The goroutine calls the function read here: a prompt that was
	// cancelled must not read the variable later.
	read, done := readPassword, make(chan answer, 1)
	go func() {
		b, err := read()
		done <- answer{b, err}
	}()
	var a answer
	select {
	case a = <-done:
	case <-ctx.Done():
	}
	restore()
	fmt.Fprintln(prompts)
	if a.err != nil {
		return "", a.err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(a.b), nil
}
```

Two things in it must stay as written. `restore()` is called once, after the `select`, on every path: do not move it into one of the cases, and do not let `readPassword` or `readHidden` change a terminal mode. And the line `read, done := readPassword, make(chan answer, 1)` takes the function value before the goroutine starts: a goroutine left behind by a cancelled prompt must not read the package variable later, while a test puts the original back.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./cmd/ghr/`
Expected: PASS (`ok  	github.com/darkraise/ghr/cmd/ghr`)

- [ ] **Step 9: Add the test on a real terminal**

The tests so far only model a terminal. This one runs the real mode changes and the real read against a pseudo-terminal, which Linux provides without a new dependency, and compares the terminal's state before and after. It is added after the implementation because it cannot fail first in a useful way: against the old `askPassword` its last row would wait for input for ever. Create `cmd/ghr/password_linux_test.go` with exactly this content:

```go
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/term"
)

// openPTY returns both ends of a new pseudo-terminal. The second is a real
// terminal as far as the prompt can tell; what is written to the first
// arrives there as typed.
func openPTY(t *testing.T) (master, tty *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	var unlock int32
	var number uint32
	for _, call := range []struct {
		request uintptr
		arg     unsafe.Pointer
	}{{syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)}, {syscall.TIOCGPTN, unsafe.Pointer(&number)}} {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), call.request, uintptr(call.arg)); errno != 0 {
			t.Fatalf("pseudo-terminal ioctl %#x: %v", call.request, errno)
		}
	}
	tty, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tty.Close() })
	return master, tty
}

// The stubs of the other tests only model a terminal. Here the prompt runs
// its real mode changes and its real read against a pseudo-terminal, and the
// terminal must end in the state it was found in.
func TestPasswordPromptOnARealTerminal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typed string // typed once the input is hidden; "" cancels before the read starts
		want  string
		err   error
	}{
		{"a line is typed", "a long secret\r", "a long secret", nil},
		{"Ctrl-C is typed", "a long\x03", "", errInterrupted},
		{"cancelled before the read starts", "", "", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			master, tty := openPTY(t)
			stubPasswordInput(t, true)
			found, err := term.GetState(int(tty.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hideInput = func() (func(), error) {
				restore, err := hideTerminal(tty)
				if err != nil {
					return nil, err
				}
				if tc.typed == "" {
					cancel()
				} else if _, err := master.WriteString(tc.typed); err != nil {
					t.Error(err)
				}
				return restore, nil
			}
			readDone := make(chan struct{})
			readPassword = func() ([]byte, error) {
				defer close(readDone)
				return readHidden(tty)
			}
			var prompts bytes.Buffer
			got, err := askPassword(ctx, &prompts, "New web password: ")
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Errorf("password %q err %v, want %q and %v", got, err, tc.want, tc.err)
			}
			if tc.typed == "" {
				// The read left behind ends with the next line typed. It
				// must leave the terminal as the prompt put it back.
				if _, err := master.WriteString("typed too late\r"); err != nil {
					t.Fatal(err)
				}
			}
			<-readDone
			left, err := term.GetState(int(tty.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(left, found) {
				t.Fatalf("the terminal was left in another state than it was found in:\n%+v\n%+v", left, found)
			}
		})
	}
}
```

The file name ends in `_linux_test.go`, so other systems do not build it. The row `Ctrl-C is typed` types into the terminal once it is in raw mode, from inside `hideInput`; the row `cancelled before the read starts` cancels at the same point.

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./cmd/ghr/ -run 'TestPasswordPromptOnARealTerminal' -v`
Expected: PASS, with `--- PASS` for all three rows. A `--- SKIP` with `no pseudo-terminals here` means the machine has no `/dev/ptmx`; report that instead of continuing.

- [ ] **Step 10: Run the package under the race detector**

Run: `PATH=/usr/local/go/bin:$PATH go test -race -count=10 ./cmd/ghr/`
Expected: PASS (`ok  	github.com/darkraise/ghr/cmd/ghr`), with no `WARNING: DATA RACE`

- [ ] **Step 11: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add cmd/ghr/password.go cmd/ghr/cli_test.go cmd/ghr/password_linux_test.go
git commit -m "fix(cli): end a password prompt on Ctrl-C"
```

### Task 8: Label rules

**Files:**
- Modify: `internal/config/config.go` (imports, new constant `MaxLabelLength`, `Parse`, new `dropBadLabels`, `labelFault` and `labelProblem`, `Validate`)
- Test: `internal/config/config_test.go` (`TestValidateErrors`, `TestValidLabelsPass`, `TestValidateNamesOnlyTheFirstBadLabel`, `TestParseDropsInvalidLabels`)

**Interfaces:**
- Consumes: `clampRunners` and `Parse` as they stand (the runner-cap work), `SystemLabels`
- Produces: `config.MaxLabelLength = 100`; the `Validate` error texts and the `Parse` warning texts listed in Contracts

**Items:** 57
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

Nothing checks what a label holds today. A label with a space or a comma, an empty one, a very long one, or one of the system labels GitHub already gives every runner (`self-hosted`, `linux`, `x64`) is accepted and sent to GitHub, where a system label is then registered twice. This task adds the rule and applies it on the two paths that reach the config.

A label is valid when, after trimming, it has 1 to 100 characters, holds no comma and no whitespace, and is not a system label (compared ignoring case). A list is valid when every label is valid and no two are equal ignoring case. The global `labels` and each repository's `labels` are judged on their own: a repository label that repeats a global one is allowed.

- An API change reaches `Validate`, which refuses an invalid list and names the first offender in it. Nothing outside `internal/config` changes: the daemon already turns a `Validate` error into a 400.
- Loading `config.yaml` goes through `Parse`, which drops the invalid or repeated labels before it validates, and returns one warning for each, like `clampRunners` does for the runner cap. Loading never rewrites the file; the next save writes the repaired lists. The daemon already records every warning `Parse` returns as an event.

- [ ] **Step 1: Write the failing tests**

In `internal/config/config_test.go`, in `TestValidateErrors`, add rows to the `cases` map. Replace

```go
		"watch_repos: DarkMem is already a configured repo":    func(c *Config) { c.WatchRepos = []string{"DarkMem"} },
	}
	for want, mutate := range cases {
```

with

```go
		"watch_repos: DarkMem is already a configured repo":    func(c *Config) { c.WatchRepos = []string{"DarkMem"} },

		`labels: "a b" must not contain spaces or commas`:             func(c *Config) { c.Labels = []string{"a b"} },
		`labels: "a,b" must not contain spaces or commas`:             func(c *Config) { c.Labels = []string{"a,b"} },
		`labels: " " must not be empty`:                               func(c *Config) { c.Labels = []string{"homelab", " "} },
		`labels: "Docker" is listed twice`:                            func(c *Config) { c.Labels = []string{"docker", "Docker"} },
		`labels: "x64" is added by ghr`:                               func(c *Config) { c.Labels = []string{"x64"} },
		`darkcloud: labels: "Linux" is added by ghr`:                  func(c *Config) { c.Repos[0].Labels = []string{"darkcloud-linux", "Linux"} },
		`darkcloud: labels: "a\tb" must not contain spaces or commas`: func(c *Config) { c.Repos[0].Labels = []string{"a\tb"} },
		"labels: \"" + strings.Repeat("a", 101) + "\" must be at most 100 characters": func(c *Config) {
			c.Labels = []string{strings.Repeat("a", 101)}
		},
	}
	for want, mutate := range cases {
```

The blank line before the new rows is deliberate: it keeps `gofmt` from re-aligning the rows above.

Then add three tests directly above the comment `// The floors themselves are valid; only values below them are rejected.`:

```go
func TestValidLabelsPass(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c.Labels = []string{" docker ", strings.Repeat("é", 100), "Docker-2"}
	c.Repos[0].Labels = []string{"docker-2", "darkcloud-linux"}
	if _, err := c.Validate(); err != nil {
		t.Fatalf("valid labels rejected: %v", err)
	}
}

func TestValidateNamesOnlyTheFirstBadLabel(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c.Labels = []string{"a b", "linux", "c,d"}
	_, err = c.Validate()
	if want := `labels: "a b" must not contain spaces or commas`; err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %s", err, want)
	}
}

// A config.yaml written by hand with bad labels loses them on load, with one
// warning for each, and what was loaded saves and loads again unchanged.
func TestParseDropsInvalidLabels(t *testing.T) {
	c, warnings, err := Parse([]byte(`
owner: darkraise
labels: [homelab, "a b", Docker, docker, linux, ""]
repos:
  - name: darkcloud
    labels: [homelab, "x,y", self-hosted]
  - name: darkmem
`))
	if err != nil {
		t.Fatal(err)
	}
	want := `labels: "a b" is not a valid label; ignored|` +
		`labels: "docker" is listed twice; ignored|` +
		`labels: "linux" is not a valid label; ignored|` +
		`labels: "" is not a valid label; ignored|` +
		`darkcloud: labels: "x,y" is not a valid label; ignored|` +
		`darkcloud: labels: "self-hosted" is not a valid label; ignored`
	if got := strings.Join(warnings, "|"); got != want {
		t.Fatalf("warnings\n got %s\nwant %s", got, want)
	}
	if got := strings.Join(c.Labels, ","); got != "homelab,Docker" {
		t.Fatalf("labels = %s", got)
	}
	if got := strings.Join(c.Repo("darkcloud").Labels, ","); got != "homelab" {
		t.Fatalf("darkcloud labels = %s", got)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatalf("saving a repaired config: %v", err)
	}
	back, warnings, err := Load(path)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("loading what was saved: warnings %v, err %v", warnings, err)
	}
	if got := strings.Join(back.Labels, ","); got != "homelab,Docker" {
		t.Fatalf("saved labels = %s", got)
	}
}
```

`TestValidLabelsPass` passes before the implementation; it pins the cases the rule must keep allowing (surrounding spaces, 100 multi-byte characters, a repository label that repeats a global one).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/config/ -run 'TestValidateErrors|TestValidLabelsPass|TestValidateNamesOnlyTheFirstBadLabel|TestParseDropsInvalidLabels'`
Expected: FAIL with `error = <nil>, want labels: "a b" must not contain spaces or commas` (in `TestValidateNamesOnlyTheFirstBadLabel`), eight `want error containing "labels: ..." , got <nil>` lines in `TestValidateErrors`, and in `TestParseDropsInvalidLabels` an empty `got` under `warnings`. `TestValidLabelsPass` does not fail.

- [ ] **Step 3: Write the implementation**

All edits are in `internal/config/config.go`.

(a) In the import block, replace

```go
	"time"

	"gopkg.in/yaml.v3"
```

with

```go
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
```

(b) Replace

```go
var SystemLabels = []string{"self-hosted", "linux", "x64"}
```

with

```go
var SystemLabels = []string{"self-hosted", "linux", "x64"}

// MaxLabelLength is the longest a custom label may be, in characters.
const MaxLabelLength = 100
```

(c) In `Parse`, replace

```go
	clamped := c.clampRunners()
	warnings, err := c.Validate()
	if err != nil {
		return nil, nil, err
	}
	return c, append(clamped, warnings...), nil
}
```

with

```go
	repaired := c.clampRunners()
	repaired = append(repaired, c.dropBadLabels()...)
	warnings, err := c.Validate()
	if err != nil {
		return nil, nil, err
	}
	return c, append(repaired, warnings...), nil
}
```

(d) Directly above `func Load(path string) (*Config, []string, error) {`, insert:

```go
// dropBadLabels removes from the global list and from each repository's list
// every label that Validate would refuse, and returns one warning for each.
func (c *Config) dropBadLabels() []string {
	var warnings []string
	drop := func(prefix string, labels []string) []string {
		kept := make([]string, 0, len(labels))
		seen := map[string]bool{}
		for _, l := range labels {
			key := strings.ToLower(strings.TrimSpace(l))
			switch {
			case labelFault(l) != "":
				warnings = append(warnings, fmt.Sprintf("%slabels: %q is not a valid label; ignored", prefix, l))
			case seen[key]:
				warnings = append(warnings, fmt.Sprintf("%slabels: %q is listed twice; ignored", prefix, l))
			default:
				seen[key] = true
				kept = append(kept, l)
			}
		}
		if len(kept) == len(labels) {
			return labels
		}
		return kept
	}
	c.Labels = drop("", c.Labels)
	for i := range c.Repos {
		c.Repos[i].Labels = drop(c.Repos[i].Name+": ", c.Repos[i].Labels)
	}
	return warnings
}

// labelFault says why one label is not valid, or returns "" for a valid one.
func labelFault(l string) string {
	l = strings.TrimSpace(l)
	switch {
	case l == "":
		return "must not be empty"
	case utf8.RuneCountInString(l) > MaxLabelLength:
		return fmt.Sprintf("must be at most %d characters", MaxLabelLength)
	case strings.ContainsFunc(l, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }):
		return "must not contain spaces or commas"
	}
	for _, s := range SystemLabels {
		if strings.EqualFold(l, s) {
			return "is added by ghr"
		}
	}
	return ""
}

// labelProblem describes the first label in the list that is not valid or
// that repeats an earlier one ignoring case, and returns "" for a valid list.
func labelProblem(labels []string) string {
	seen := map[string]bool{}
	for _, l := range labels {
		if f := labelFault(l); f != "" {
			return fmt.Sprintf("labels: %q %s", l, f)
		}
		key := strings.ToLower(strings.TrimSpace(l))
		if seen[key] {
			return fmt.Sprintf("labels: %q is listed twice", l)
		}
		seen[key] = true
	}
	return ""
}

```

(e) In `Validate`, replace

```go
	if c.DiskHighWater < 1 || c.DiskHighWater > 100 {
```

with

```go
	if p := labelProblem(c.Labels); p != "" {
		errs = append(errs, p)
	}
	if c.DiskHighWater < 1 || c.DiskHighWater > 100 {
```

(f) In `Validate`, inside the loop over `c.Repos`, replace

```go
		if len(c.CustomLabels(r)) == 0 {
```

with

```go
		if p := labelProblem(r.Labels); p != "" {
			errs = append(errs, r.Name+": "+p)
		}
		if len(c.CustomLabels(r)) == 0 {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/config/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/config`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/config/` (must print nothing), then:

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "fix(config): refuse invalid labels"
```

### Task 9: An empty config file loads as the defaults

**Files:**
- Modify: `internal/config/config.go` (`Parse`)
- Test: `internal/config/config_test.go` (`TestParseEmptyFileLoadsDefaults`)

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 58
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

A `config.yaml` that holds no YAML document, either empty or with only comments, makes the first `Decode` in `Parse` return `io.EOF`, so `Parse` fails with `parse config: EOF` and the daemon does not start. After this task such a file loads as the defaults. The check for a second document is unchanged: it already treats `io.EOF` as the normal end.

- [ ] **Step 1: Write the failing test**

In `internal/config/config_test.go`, add this test directly above `func TestCloneIsDeep(t *testing.T) {`:

```go
func TestParseEmptyFileLoadsDefaults(t *testing.T) {
	for name, y := range map[string]string{
		"empty":         "",
		"blank":         "\n\n",
		"comments only": "# owner: darkraise\n# mode: all\n",
	} {
		c, warnings, err := Parse([]byte(y))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(warnings) != 0 || c.Mode != ModeQueue || c.GlobalMax != 2 || c.PollInterval.D() != 10*time.Second {
			t.Errorf("%s: warnings %v, config %+v", name, warnings, c)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/config/ -run TestParseEmptyFileLoadsDefaults`
Expected: FAIL with `empty: parse config: EOF` (and the same for `blank` and `comments only`, in map order)

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, in `Parse`, replace

```go
	if err := dec.Decode(c); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
```

with

```go
	if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/config/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/config`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/config/` (must print nothing), then:

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "fix(config): load an empty file as defaults"
```

### Task 10: Save through a symlink, and sync

**Files:**
- Modify: `internal/config/config.go` (`Save`, new `saveTarget`, new `maxLinkHops`, new `syncFile`)
- Test: `internal/config/config_test.go` (imports, helpers `saveOwner`, `isSymlink` and `stubSync`, `TestSaveThroughSymlinkReplacesTheTarget`, `TestSaveThroughDanglingSymlinkCreatesTheTarget`, `TestSaveThroughLinkChain`, `TestSaveWritesNothingWhenTheLinkCannotBeResolved`, `TestSaveSyncsBeforeTheRename`, `TestSaveKeepsTheFileWhenSyncFails`)

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names (`syncFile` and `saveTarget` are unexported and only this package's tests use them)

**Items:** 59, 206
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 2 = 3

`Save` writes a temporary file beside `path` and renames it over `path`. When `path` is a symbolic link (an administrator keeps `config.yaml` in another directory and links it), the rename replaces the link with a regular file, and the real file is never updated. `Save` also never syncs the temporary file, so a power loss after the rename can leave an empty `config.yaml`; `metrics.Save` and the history store already sync before they rename.

This task has two cycles, each with its own test.

- **The link.** `Save` first resolves where `path` leads: if `path` is a symbolic link it reads the link, resolves a relative target against the link's directory, and follows a chain of links, at most 8. The last file may not exist yet (the target can disappear after the config was loaded); it is then created. The temporary file is created beside that final file and renamed onto it, never onto a link, so every link in the chain stays. Any other resolution failure (a missing directory on the way, a loop, more than 8 links) is returned as an error and nothing is written. A `path` that does not exist yet (the first save) is used as it is.
- **The sync.** The temporary file is synced before the rename. The call goes through a package variable, `syncFile`, so a test can see when it runs and make it fail. If the sync fails, `Save` returns the error, `config.yaml` keeps its old content and the temporary file is removed (the existing `defer os.Remove` does that).

Mode 0600 stays: the directory is 0700 and only the daemon reads the file.

- [ ] **Step 1: Write the failing link tests**

In `internal/config/config_test.go`, add this code directly above `func TestSaveRejectsInvalid(t *testing.T) {`:

```go
func saveOwner(t *testing.T, path, owner string) error {
	t.Helper()
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	c.Owner = owner
	return Save(path, c)
}

func isSymlink(t *testing.T, path string) bool {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode()&os.ModeSymlink != 0
}

func TestSaveThroughSymlinkReplacesTheTarget(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "elsewhere")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "config.yaml")
	if err := os.WriteFile(target, []byte("owner: before\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(filepath.Join("elsewhere", "config.yaml"), link); err != nil {
		t.Fatal(err)
	}
	if err := saveOwner(t, link, "after"); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(t, link) {
		t.Fatal("the link was replaced by a file")
	}
	if got, _ := os.Readlink(link); got != filepath.Join("elsewhere", "config.yaml") {
		t.Fatalf("link now points at %q", got)
	}
	back, _, err := Load(target)
	if err != nil || back.Owner != "after" {
		t.Fatalf("target after save: owner %q, err %v", back.Owner, err)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o600 {
		t.Fatalf("target mode = %v, want 0600", fi.Mode().Perm())
	}
	for d, want := range map[string]int{dir: 2, targetDir: 1} {
		entries, _ := os.ReadDir(d)
		if len(entries) != want {
			t.Fatalf("%s holds %v, want %d entries (a temp file was left)", d, entries, want)
		}
	}
}

// The target can disappear after the file was loaded; the link must still
// stay a link and the target must be created.
func TestSaveThroughDanglingSymlinkCreatesTheTarget(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "elsewhere")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(filepath.Join("elsewhere", "config.yaml"), link); err != nil {
		t.Fatal(err)
	}
	if err := saveOwner(t, link, "after"); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(t, link) {
		t.Fatal("the link was replaced by a file")
	}
	back, _, err := Load(filepath.Join(targetDir, "config.yaml"))
	if err != nil || back.Owner != "after" {
		t.Fatalf("target after save: owner %q, err %v", back.Owner, err)
	}
}

func TestSaveThroughLinkChain(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realDir, "config.yaml")
	middle := filepath.Join(dir, "middle.yaml")
	if err := os.Symlink(target, middle); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.yaml")
	if err := os.Symlink("middle.yaml", link); err != nil {
		t.Fatal(err)
	}
	if err := saveOwner(t, link, "after"); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(t, link) || !isSymlink(t, middle) {
		t.Fatal("a link in the chain was replaced by a file")
	}
	back, _, err := Load(target)
	if err != nil || back.Owner != "after" {
		t.Fatalf("target after save: owner %q, err %v", back.Owner, err)
	}
}

func TestSaveWritesNothingWhenTheLinkCannotBeResolved(t *testing.T) {
	for name, build := range map[string]func(t *testing.T, dir string) string{
		"loop": func(t *testing.T, dir string) string {
			a, b := filepath.Join(dir, "a.yaml"), filepath.Join(dir, "b.yaml")
			if err := os.Symlink("b.yaml", a); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("a.yaml", b); err != nil {
				t.Fatal(err)
			}
			return a
		},
		"missing directory": func(t *testing.T, dir string) string {
			link := filepath.Join(dir, "config.yaml")
			if err := os.Symlink(filepath.Join("missing", "config.yaml"), link); err != nil {
				t.Fatal(err)
			}
			return link
		},
	} {
		dir := t.TempDir()
		link := build(t, dir)
		before, _ := os.ReadDir(dir)
		if err := saveOwner(t, link, "after"); err == nil {
			t.Errorf("%s: Save returned nil", name)
		}
		after, _ := os.ReadDir(dir)
		if len(after) != len(before) {
			t.Errorf("%s: directory held %v, now %v", name, before, after)
		}
		for _, e := range before {
			if fi, err := os.Lstat(filepath.Join(dir, e.Name())); err != nil || fi.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s: %s is no longer a link (%v)", name, e.Name(), err)
			}
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/config/ -run 'TestSaveThrough|TestSaveWritesNothing'`
Expected: FAIL with `the link was replaced by a file` (in `TestSaveThroughSymlinkReplacesTheTarget` and in `TestSaveThroughDanglingSymlinkCreatesTheTarget`), `a link in the chain was replaced by a file`, and `loop: Save returned nil` and `missing directory: Save returned nil`

- [ ] **Step 3: Write the link implementation**

In `internal/config/config.go`, replace the comment and the start of `Save`

```go
// Save validates c, then writes it atomically (temp file in the same dir + rename).
func Save(path string, c *Config) error {
	if _, err := c.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
```

with

```go
// maxLinkHops is how many symbolic links saveTarget follows before it gives up.
const maxLinkHops = 8

// saveTarget returns the file Save replaces: path itself, or the file at the
// end of the symbolic links that start there. That file may not exist yet.
// Anything else that stops the resolution is an error, so Save never writes
// over a link.
func saveTarget(path string) (string, error) {
	orig := path
	for hops := 0; ; hops++ {
		dir, err := filepath.EvalSymlinks(filepath.Dir(path))
		if err != nil {
			return "", err
		}
		path = filepath.Join(dir, filepath.Base(path))
		fi, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
		if hops == maxLinkHops {
			return "", fmt.Errorf("%s: more than %d symbolic links", orig, maxLinkHops)
		}
		dest, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(dir, dest)
		}
		path = dest
	}
}

// Save validates c, then writes it atomically (temp file beside the file, then
// renamed over it). When path is a symbolic link, the file at the end of the
// link is replaced and the link stays.
func Save(path string, c *Config) error {
	if _, err := c.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	path, err = saveTarget(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
```

`errors` and `fmt` are already imported in this file.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/config/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/config`)

- [ ] **Step 5: Add the sync seam**

In `internal/config/config.go`, directly above the line `// maxLinkHops is how many symbolic links saveTarget follows before it gives up.`, add:

```go
// syncFile is os.File.Sync; a test replaces it to see when the sync runs and to
// make it fail.
var syncFile = (*os.File).Sync

```

Nothing calls it yet, so behaviour is unchanged.

- [ ] **Step 6: Write the failing sync tests**

In `internal/config/config_test.go`, replace

```go
import (
	"encoding/json"
	"os"
```

with

```go
import (
	"encoding/json"
	"errors"
	"os"
```

Then add this code directly above `func TestSaveRejectsInvalid(t *testing.T) {`:

```go
func stubSync(t *testing.T, stub func(*os.File) error) {
	t.Helper()
	orig := syncFile
	syncFile = stub
	t.Cleanup(func() { syncFile = orig })
}

func TestSaveSyncsBeforeTheRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("owner: before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls int
	var targetAtSync, tempAtSync string
	stubSync(t, func(f *os.File) error {
		calls++
		b, _ := os.ReadFile(path)
		targetAtSync = string(b)
		b, _ = os.ReadFile(f.Name())
		tempAtSync = string(b)
		return nil
	})
	if err := saveOwner(t, path, "after"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("sync ran %d times, want 1", calls)
	}
	if targetAtSync != "owner: before\n" {
		t.Fatalf("config.yaml held %q when the temp file was synced, want the old content", targetAtSync)
	}
	if !strings.Contains(tempAtSync, "owner: after") {
		t.Fatalf("temp file held %q when synced, want the new config", tempAtSync)
	}
}

func TestSaveKeepsTheFileWhenSyncFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("owner: before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stubSync(t, func(*os.File) error { return errors.New("disk full") })
	if err := saveOwner(t, path, "after"); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Save returned %v, want the sync error", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "owner: before\n" {
		t.Fatalf("config.yaml now holds %q", b)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("directory holds %v, want only config.yaml", entries)
	}
}
```

- [ ] **Step 7: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/config/ -run 'TestSaveSyncs|TestSaveKeepsTheFile'`
Expected: FAIL with `sync ran 0 times, want 1` and `Save returned <nil>, want the sync error`

- [ ] **Step 8: Write the sync implementation**

In `internal/config/config.go`, in `Save`, replace

```go
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
```

with

```go
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := syncFile(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
```

- [ ] **Step 9: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/config/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/config`)

- [ ] **Step 10: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/config/` (must print nothing), then:

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "fix(config): save through a symlink and sync"
```

### Task 11: Downloads only from the tool's own releases

**Files:**
- Modify: `internal/toolchain/toolchain.go` (`Env`, new `Origin`)
- Modify: `internal/toolchain/env.go` (`NewEnv`, new `httpsFrom`)
- Modify: `internal/toolchain/layout.go` (new `fetch`, `installArchive`)
- Modify: `internal/toolchain/node.go`
- Modify: `internal/toolchain/python.go`
- Modify: `internal/toolchain/java.go`
- Modify: `internal/toolchain/golang.go`
- Modify: `internal/toolchain/dotnet.go`
- Create: `internal/toolchain/download_test.go`
- Test: `internal/toolchain/helpers_test.go` (`newFixture`)
- Test: `internal/toolchain/install_test.go` (the `installArchive` calls)
- Test: `internal/toolchain/hardening_test.go` (the `installArchive` calls)

**Interfaces:**
- Consumes: nothing
- Produces: `type Origin struct{ Dir, File string }`; `Env.Allow func(url string, from Origin) error`; `(*Env).fetch(ctx context.Context, from Origin, rawURL, dst string) error`; `(*Env).installArchive(ctx context.Context, toolDir string, from Origin, rel Release, progress func(string)) error`; `httpsFrom(rawURL string, from Origin) error`; `nodeReleases`, `pythonReleases`, `javaReleases(version string) string`; the error text `refusing to download <url>`

**Items:** 60
**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 3 = 6

The daemon runs as root. It downloads each toolchain from a URL that a version list names (Node, Python, Java) or that it builds from a configured source (Go, .NET), and then unpacks the archive or runs the script. Today any URL is fetched: a tampered list can point the download anywhere.

After this task every download goes through one function, `Env.fetch`. It first asks `Env.Allow` whether the URL comes from the `Origin` the installer itself passes (never anything read from the list), and only then hands that same string to `Env.Fetch`. An `Origin` is one of two rules. A directory rule (`Dir`: Node, Python, Java, Go) passes a URL below a directory whose URL ends in `/`; a directory written without that slash passes nothing, because `/dl` is also a prefix of `/dlx/`. An exact-file rule (`File`: .NET) passes only the configured script URL itself. `NewEnv` sets the strict check `httpsFrom`. A refused URL fails the install with exactly `refusing to download <url>`. The test fixture replaces `Allow`, so the other tests keep their local `httptest` servers.

- [ ] **Step 1: Write the failing tests**

Create `internal/toolchain/download_test.go` with exactly this content. Every test builds its `Env` with `NewEnv`, so it runs with the check the daemon uses, and names no identifier that does not exist yet.

```go
package toolchain

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

const (
	realNodeURL   = "https://github.com/actions/node-versions/releases/download/22.11.0-11593095476/node-22.11.0-linux-x64.tar.gz"
	realPythonURL = "https://github.com/actions/python-versions/releases/download/3.13.7-16980743123/python-3.13.7-linux-24.04-x64.tar.gz"
	realJDK21URL  = "https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz"
	realJDK8URL   = "https://github.com/adoptium/temurin8-binaries/releases/download/jdk8u504-b01/OpenJDK8U-jdk_x64_linux_hotspot_8u504b01.tar.gz"
	realGoURL     = "https://go.dev/dl/go1.25.1.linux-amd64.tar.gz"
	realDotnetURL = "https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh"
)

var errFetched = errors.New("fetched")

// strictInstall runs tool's installer on url in an Env straight from NewEnv,
// so with the download check the daemon uses, and returns what reached Fetch.
func strictInstall(t *testing.T, tool, version, url string, sources func(*Sources)) ([]string, error) {
	t.Helper()
	var fetched []string
	fetch := func(_ context.Context, url, _ string) error {
		fetched = append(fetched, url)
		return errFetched
	}
	e := NewEnv(t.TempDir(), "/home/ghrunner", "ghrunner", (&host{}).run, fetch)
	if sources != nil {
		sources(&e.Sources)
	}
	in, err := New(e).Get(tool)
	if err != nil {
		t.Fatal(err)
	}
	rel := Release{Tool: tool, Version: version, Folder: strings.Replace(version, "+", "-", 1), URL: url}
	err = in.Install(context.Background(), rel, func(string) {})
	if got := tmpEntries(t, e.Root); len(got) != 0 {
		t.Errorf("%s %s: .tmp holds %v", tool, url, got)
	}
	return fetched, err
}

func wantRefused(t *testing.T, what, url string, fetched []string, err error) {
	t.Helper()
	if err == nil || err.Error() != "refusing to download "+url {
		t.Errorf("%s %q: got %v, want it refused", what, url, err)
	}
	if len(fetched) != 0 {
		t.Errorf("%s %q: fetched %q", what, url, fetched)
	}
}

func TestInstallDownloadsOnlyFromItsOwnToolsReleases(t *testing.T) {
	cases := []struct {
		tool, version, url string
		refused            bool
	}{
		{"node", "22.11.0", realNodeURL, false},
		{"node", "22.11.0", "http://github.com/actions/node-versions/releases/download/22.11.0-11593095476/node-22.11.0-linux-x64.tar.gz", true},
		{"node", "22.11.0", "https://example.net/actions/node-versions/releases/download/22.11.0-11593095476/node-22.11.0-linux-x64.tar.gz", true},
		{"node", "22.11.0", "https://github.com/evil/node-versions/releases/download/22.11.0-11593095476/node-22.11.0-linux-x64.tar.gz", true},
		{"node", "22.11.0", "http://127.0.0.1:8080/node-22.11.0-linux-x64.tar.gz", true},
		{"node", "22.11.0", realPythonURL, true},
		{"node", "22.11.0", realJDK21URL, true},
		{"python", "3.13.7", realPythonURL, false},
		{"python", "3.13.7", "http://github.com/actions/python-versions/releases/download/3.13.7-16980743123/python-3.13.7-linux-24.04-x64.tar.gz", true},
		{"python", "3.13.7", "https://example.net/actions/python-versions/releases/download/3.13.7-16980743123/python-3.13.7-linux-24.04-x64.tar.gz", true},
		{"python", "3.13.7", realNodeURL, true},
		{"java", "21.0.12+101.0.LTS", realJDK21URL, false},
		{"java", "8.0.504+1", realJDK8URL, false},
		{"java", "21.0.12+101.0.LTS", "http://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz", true},
		{"java", "21.0.12+101.0.LTS", "https://api.adoptium.net/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz", true},
		{"java", "21.0.12+101.0.LTS", "https://github.com/evil/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_x64_linux_hotspot_21.0.12.1_1.tar.gz", true},
		{"java", "21.0.12+101.0.LTS", realJDK8URL, true},
		{"java", "x.0.1", "https://github.com/adoptium/temurinx-binaries/releases/download/jdk-x/OpenJDK.tar.gz", true},
		{"java", ".0.1", "https://github.com/adoptium/temurin-binaries/releases/download/jdk/OpenJDK.tar.gz", true},
		{"java", "21.0.12+101.0.LTS", realNodeURL, true},
		{"go", "1.25.1", realGoURL, false},
		{"go", "1.25.1", "http://go.dev/dl/go1.25.1.linux-amd64.tar.gz", true},
		{"go", "1.25.1", "https://github.com/actions/go-versions/releases/download/1.25.1-17451174567/go-1.25.1-linux-x64.tar.gz", true},
		{"go", "1.25.1", "https://go.dev/play/go1.25.1.linux-amd64.tar.gz", true},
		{"dotnet", "8.0.414", realDotnetURL, false},
		{"dotnet", "8.0.414", "http://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh", true},
		{"dotnet", "8.0.414", "https://github.com/dotnet/install-scripts/raw/main/src/dotnet-install.sh", true},
		{"dotnet", "8.0.414", "https://builds.dotnet.microsoft.com/dotnet/scripts/v2/dotnet-install.sh", true},
		{"dotnet", "8.0.414", realDotnetURL + ".other", true},
		{"dotnet", "8.0.414", realDotnetURL + "/another-script", true},
		{"dotnet", "8.0.414", realDotnetURL + "/", true},
		{"dotnet", "8.0.414", "https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install", true},
		{"dotnet", "8.0.414", "https://user@builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh", true},
		{"dotnet", "8.0.414", "https://builds.dotnet.microsoft.com:443/dotnet/scripts/v1/dotnet-install.sh", true},
		{"dotnet", "8.0.414", realDotnetURL + "?x=1", true},
		{"dotnet", "8.0.414", realDotnetURL + "#x", true},
	}
	for _, c := range cases {
		fetched, err := strictInstall(t, c.tool, c.version, c.url, nil)
		if c.refused {
			wantRefused(t, c.tool, c.url, fetched, err)
			continue
		}
		if !errors.Is(err, errFetched) || !slices.Equal(fetched, []string{c.url}) {
			t.Errorf("%s %s: got %v after fetching %q, want exactly that URL fetched", c.tool, c.url, err, fetched)
		}
	}
}

func TestInstallRefusesAURLThatIsNotPlain(t *testing.T) {
	// Adoptium's links carry an encoded "+", so an escape as such is fine.
	plain := "https://github.com/actions/node-versions/releases/download/a%2Bb/node.tar.gz"
	if fetched, err := strictInstall(t, "node", "22.11.0", plain, nil); !errors.Is(err, errFetched) || !slices.Equal(fetched, []string{plain}) {
		t.Errorf("%s: got %v after fetching %q, want exactly that URL fetched", plain, err, fetched)
	}
	for _, u := range []string{
		"https://GitHub.com/actions/node-versions/releases/download/1/node.tar.gz",
		"HTTPS://github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com@evil.example/actions/node-versions/releases/download/1/node.tar.gz",
		"https://user@github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"https://user:pw@github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com:443/actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com:/actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com./actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com.evil.example/actions/node-versions/releases/download/1/node.tar.gz",
		"https://evilgithub.com/actions/node-versions/releases/download/1/node.tar.gz",
		"https:///actions/node-versions/releases/download/1/node.tar.gz",
		"https:github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"//github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"https://github.com/actions/node-versions/releases/downloads/1/node.tar.gz",
		"https://github.com/actions/node-versions/releases/download",
		"https://github.com/actions/node-versions/releases/download/../../../evil/repo/releases/download/1/node.tar.gz",
		"https://github.com/actions/node-versions/releases/download/1/../node.tar.gz",
		"https://github.com/actions/node-versions/releases/download/1/./node.tar.gz",
		"https://github.com/actions/node-versions/releases/download//node.tar.gz",
		"https://github.com//actions/node-versions/releases/download/1/node.tar.gz",
		`https://github.com/actions/node-versions/releases/download/1\node.tar.gz`,
		"https://github.com/actions/node-versions/releases/download/1%5Cnode.tar.gz",
		"https://github.com/actions/node-versions/releases/download/%2e%2e/%2e%2e/node.tar.gz",
		"https://github.com/actions/node-versions/releases/download/%2E%2E/node.tar.gz",
		"https://github.com/actions/node-versions/releases/download/1/node%2Etar.gz",
		"https://github.com/actions/node-versions/releases/download/1%2fnode.tar.gz",
		"https://github.com/actions/node-versions/releases/download%2F1/node.tar.gz",
		"https://github.com/actions/node-versions/releases/download/1/node.tar.gz#x",
		"https://github.com/actions/node-versions/releases/download/1/node.tar.gz#",
		"https://github.com/actions/node-versions/releases/download/1/node.tar.gz?x=1",
		"https://github.com/actions/node-versions/releases/download/1/node.tar.gz?",
		" https://github.com/actions/node-versions/releases/download/1/node.tar.gz",
		"",
	} {
		fetched, err := strictInstall(t, "node", "22.11.0", u, nil)
		wantRefused(t, "node", u, fetched, err)
	}
}

// A source pointed at a local server, as this package's other tests do, must
// not pass the check NewEnv sets.
func TestNewEnvRefusesALocalSource(t *testing.T) {
	local := func(s *Sources) {
		s.GoDownload = "http://127.0.0.1:8080/dl/"
		s.DotnetScript = "http://127.0.0.1:8080/dotnet-install.sh"
	}
	fetched, err := strictInstall(t, "go", "1.25.1", "http://127.0.0.1:8080/dl/go1.25.1.linux-amd64.tar.gz", local)
	wantRefused(t, "go", "http://127.0.0.1:8080/dl/go1.25.1.linux-amd64.tar.gz", fetched, err)
	fetched, err = strictInstall(t, "dotnet", "8.0.414", "http://127.0.0.1:8080/dotnet-install.sh", local)
	wantRefused(t, "dotnet", "http://127.0.0.1:8080/dotnet-install.sh", fetched, err)
	tls := func(s *Sources) {
		s.GoDownload = "https://127.0.0.1:8080/dl/"
		s.DotnetScript = "https://127.0.0.1:8080/dotnet-install.sh"
	}
	fetched, err = strictInstall(t, "go", "1.25.1", "https://127.0.0.1:8080/dl/go1.25.1.linux-amd64.tar.gz", tls)
	wantRefused(t, "go", "https://127.0.0.1:8080/dl/go1.25.1.linux-amd64.tar.gz", fetched, err)
	fetched, err = strictInstall(t, "dotnet", "8.0.414", "https://127.0.0.1:8080/dotnet-install.sh", tls)
	wantRefused(t, "dotnet", "https://127.0.0.1:8080/dotnet-install.sh", fetched, err)
}

// A directory given without its closing slash would also cover its siblings
// ("/dl" is a prefix of "/dlx/"), so it is no directory at all.
func TestInstallRefusesADirectoryWithoutItsSlash(t *testing.T) {
	noSlash := func(s *Sources) { s.GoDownload = "https://go.dev/dl" }
	for _, u := range []string{
		"https://go.dev/dl/go1.25.1.linux-amd64.tar.gz",
		"https://go.dev/dlx/go1.25.1.linux-amd64.tar.gz",
		"https://go.dev/dl",
	} {
		fetched, err := strictInstall(t, "go", "1.25.1", u, noSlash)
		wantRefused(t, "go", u, fetched, err)
	}
}

// The one file an installer may fetch is held to the same shape rules as a
// URL below a directory, even when the configured URL breaks them itself.
func TestInstallRefusesAConfiguredFileThatIsNotPlain(t *testing.T) {
	for _, script := range []string{
		realDotnetURL + "?x=1",
		realDotnetURL + "#x",
		"https://user@builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh",
		"https://builds.dotnet.microsoft.com/dotnet/scripts/v1/../v1/dotnet-install.sh",
	} {
		fetched, err := strictInstall(t, "dotnet", "8.0.414", script, func(s *Sources) { s.DotnetScript = script })
		wantRefused(t, "dotnet", script, fetched, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestInstallDownloadsOnlyFromItsOwnToolsReleases|TestInstallRefuses|TestNewEnvRefusesALocalSource'`
Expected: FAIL in all five new tests. The first line is `download_test.go:102: node "http://github.com/actions/node-versions/releases/download/22.11.0-11593095476/node-22.11.0-linux-x64.tar.gz": got download: fetched, want it refused`

If your shell refuses a `|` inside a command, run the five tests one at a time instead. Each must FAIL:

- `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestInstallDownloadsOnlyFromItsOwnToolsReleases'`, first line as above
- `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestInstallRefusesAURLThatIsNotPlain'`, first line `download_test.go:153: node "https://GitHub.com/actions/node-versions/releases/download/1/node.tar.gz": got download: fetched, want it refused`
- `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestNewEnvRefusesALocalSource'`, first line `download_test.go:165: go "http://127.0.0.1:8080/dl/go1.25.1.linux-amd64.tar.gz": got download: fetched, want it refused`
- `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestInstallRefusesADirectoryWithoutItsSlash'`, first line `download_test.go:188: go "https://go.dev/dl/go1.25.1.linux-amd64.tar.gz": got download: fetched, want it refused`
- `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestInstallRefusesAConfiguredFileThatIsNotPlain'`, first line `download_test.go:202: dotnet "https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh?x=1": got download: fetched, want it refused`

- [ ] **Step 3: Write the implementation**

In `internal/toolchain/toolchain.go`, in the `Env` struct, replace

```go
	Fetch   func(ctx context.Context, url, dst string) error
	Run     func(ctx context.Context, name string, args ...string) ([]byte, error)
```

with

```go
	Fetch   func(ctx context.Context, url, dst string) error
	// Allow returns an error unless url may be downloaded for an installer
	// whose files come from from. A version list names the URL, and what it
	// downloads is unpacked or run on this machine.
	Allow   func(url string, from Origin) error
	Run     func(ctx context.Context, name string, args ...string) ([]byte, error)
```

In the same file, replace the line

```go
// Env is what the installers need from the host.
```

with

```go
// Origin is where an installer's downloads may come from: every URL below
// the directory Dir, which must end in "/", or the one URL File. Exactly one
// of the two is set.
type Origin struct{ Dir, File string }

// Env is what the installers need from the host.
```

In `internal/toolchain/env.go`, in the import block, replace

```go
	"net/http"
	"time"
```

with

```go
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
```

In `NewEnv`, replace

```go
		Fetch:   fetch,
		Run:     run,
```

with

```go
		Fetch:   fetch,
		Allow:   httpsFrom,
		Run:     run,
```

and add this function directly above the line `// GetTimeout bounds one read of a version list.`:

```go
// httpsFrom is the download check the daemon uses. It passes rawURL only when
// it is an https URL with no user, port, query or fragment, whose path holds
// nothing a server could resolve to another place (dot segments, doubled or
// encoded slashes, encoded dots, backslashes), and which is from's one file
// or lies below from's directory on the same host. The host must match byte
// for byte, because case folding maps some non-ASCII letters onto ASCII ones.
func httpsFrom(rawURL string, from Origin) error {
	refused := fmt.Errorf("refusing to download %s", rawURL)
	u, err := url.Parse(rawURL)
	// A URL that does not print back as written carries something the parsed
	// fields do not show, such as an empty fragment.
	if err != nil || u.String() != rawURL {
		return refused
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.Port() != "" {
		return refused
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return refused
	}
	escaped := strings.ToLower(u.EscapedPath())
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%2e") || strings.Contains(u.Path, `\`) {
		return refused
	}
	if path.Clean(u.Path) != u.Path {
		return refused
	}
	switch {
	case from.File != "" && from.Dir == "":
		if rawURL != from.File {
			return refused
		}
	case from.Dir != "" && from.File == "":
		// Without its closing slash a directory would also cover its
		// siblings: "/dl" is a prefix of "/dlx/".
		d, err := url.Parse(from.Dir)
		if err != nil || d.Scheme != "https" || d.Host != u.Host || !strings.HasSuffix(d.Path, "/") || !strings.HasPrefix(u.Path, d.Path) {
			return refused
		}
	default:
		return refused
	}
	return nil
}
```

In `internal/toolchain/layout.go`, replace

```go
// installArchive installs rel's archive at <Root>/<toolDir>/<rel.Folder>/x64,
// the layout @actions/tool-cache find() reads, writing the marker last.
func (e *Env) installArchive(ctx context.Context, toolDir string, rel Release, progress func(string)) error {
```

with

```go
// fetch is the one way an installer downloads: Allow judges rawURL against
// the installer's own origin, and Fetch then gets that same string.
func (e *Env) fetch(ctx context.Context, from Origin, rawURL, dst string) error {
	if err := e.Allow(rawURL, from); err != nil {
		return err
	}
	if err := e.Fetch(ctx, rawURL, dst); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	return nil
}

// installArchive installs rel's archive, which must come from from, at
// <Root>/<toolDir>/<rel.Folder>/x64, the layout @actions/tool-cache find()
// reads, writing the marker last.
func (e *Env) installArchive(ctx context.Context, toolDir string, from Origin, rel Release, progress func(string)) error {
```

and in the body of `installArchive` replace

```go
	if err := e.Fetch(ctx, rel.URL, archive); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if err := verify(archive, rel.SHA256); err != nil {
```

with

```go
	if err := e.fetch(ctx, from, rel.URL, archive); err != nil {
		return err
	}
	if err := verify(archive, rel.SHA256); err != nil {
```

In `internal/toolchain/node.go`, replace

```go
type node struct{ manifestTool }
```

with

```go
// nodeReleases is where every download_url of the node-versions manifest
// points.
const nodeReleases = "https://github.com/actions/node-versions/releases/download/"

type node struct{ manifestTool }
```

and replace

```go
	return n.e.installArchive(ctx, n.dir, rel, progress)
```

with

```go
	return n.e.installArchive(ctx, n.dir, Origin{Dir: nodeReleases}, rel, progress)
```

In `internal/toolchain/python.go`, replace

```go
type python struct{ manifestTool }
```

with

```go
// pythonReleases is where every download_url of the python-versions manifest
// points.
const pythonReleases = "https://github.com/actions/python-versions/releases/download/"

type python struct{ manifestTool }
```

and replace

```go
	if err := e.Fetch(ctx, rel.URL, archive); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	progress("extracting")
```

with

```go
	if err := e.fetch(ctx, Origin{Dir: pythonReleases}, rel.URL, archive); err != nil {
		return err
	}
	progress("extracting")
```

In `internal/toolchain/java.go`, replace

```go
type java struct{ e *Env }
```

with

```go
// javaReleases is where Adoptium publishes the builds of version's major:
// the temurin<major>-binaries repository. A major that is not a number has
// no such place, and the "" returned for it passes no URL.
func javaReleases(version string) string {
	major, _, _ := strings.Cut(version, ".")
	if major == "" || strings.Trim(major, "0123456789") != "" {
		return ""
	}
	return "https://github.com/adoptium/temurin" + major + "-binaries/releases/download/"
}

type java struct{ e *Env }
```

and replace

```go
	return j.e.installArchive(ctx, javaDir, rel, progress)
```

with

```go
	return j.e.installArchive(ctx, javaDir, Origin{Dir: javaReleases(rel.Version)}, rel, progress)
```

In `internal/toolchain/golang.go`, replace

```go
	return g.e.installArchive(ctx, g.dir, rel, progress)
```

with

```go
	return g.e.installArchive(ctx, g.dir, Origin{Dir: g.e.Sources.GoDownload}, rel, progress)
```

In `internal/toolchain/dotnet.go`, replace

```go
	if err := e.Fetch(ctx, rel.URL, filepath.Join(op, "dotnet-install.sh")); err != nil {
		return fmt.Errorf("download: %w", err)
	}
```

with

```go
	if err := e.fetch(ctx, Origin{File: e.Sources.DotnetScript}, rel.URL, filepath.Join(op, "dotnet-install.sh")); err != nil {
		return err
	}
```

Keep the `fmt` import in `python.go` and `dotnet.go`: both files still use it.

- [ ] **Step 4: Bring the existing tests to the new field and signature**

The fixture builds its `Env` by hand, so `Allow` would be nil there and every install test would panic. In `internal/toolchain/helpers_test.go`, in `newFixture`, replace

```go
		Fetch: system.Download,
		Run:   f.host.run,
```

with

```go
		Fetch: system.Download,
		Allow: func(string, Origin) error { return nil },
		Run:   f.host.run,
```

`installArchive` has a new third parameter. `internal/toolchain/install_test.go` calls it seven times and `internal/toolchain/hardening_test.go` twice, each time with `"demo", rel, func(`. Give every call the fixture server as its directory:

```bash
sed -i 's|"demo", rel, func(|"demo", Origin{Dir: f.url + "/"}, rel, func(|' internal/toolchain/install_test.go internal/toolchain/hardening_test.go
```

After it, a call reads for example

```go
	if err := f.installArchive(context.Background(), "demo", Origin{Dir: f.url + "/"}, rel, func(string) {}); err != nil {
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/toolchain`)

Then prove that no installer can download around the check. Run: `rg -n '\.Fetch\(' internal/toolchain -g '!*_test.go' | rg -v 'e\.Fetch\(ctx, rawURL, dst\)'`
Expected: no output. The one call it filters out is the line inside `Env.fetch` in `layout.go`.

If your shell refuses the pipe, run the first half alone: `rg -n '\.Fetch\(' internal/toolchain -g '!*_test.go'`
Expected: exactly one line, `internal/toolchain/layout.go:207:	if err := e.Fetch(ctx, rawURL, dst); err != nil {`

- [ ] **Step 6: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/toolchain` (must print nothing), then:

```bash
git add internal/toolchain
git commit -m "fix(toolchain): download only a tool's own releases"
```

### Task 12: The download client refuses a redirect to plain http

**Files:**
- Modify: `internal/system/system.go` (new `HTTPSRedirect`, new `downloadClient`, `download`)
- Test: `internal/system/host_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `system.HTTPSRedirect(req *http.Request, via []*http.Request) error`, whose refusal reads `refusing a redirect to <scheme>://<host>`

**Items:** 60
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

`download` in `internal/system/system.go` fetches everything the daemon later unpacks or runs as root: toolchain archives, install scripts and the runner tarball. It uses `http.DefaultClient`, which follows a redirect to plain `http`, where anyone on the path can replace the body.

After this task `download` refuses a redirect to a URL that is not `https`. A redirect to another `https` host is still followed: GitHub serves a release download from its asset host. The rule is an exported function, `HTTPSRedirect`, so that the client that reads the version lists can use the same one.

- [ ] **Step 1: Write the failing test**

In `internal/system/host_test.go`, add this test directly below `TestDownload`:

```go
func TestDownloadFollowsARedirectOnlyToHTTPS(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("sent over plain http"))
	}))
	t.Cleanup(plain.Close)
	// Another host, as GitHub serves a release download from its asset host.
	assets := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("tarball bytes"))
	}))
	t.Cleanup(assets.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/to-https" {
			http.Redirect(w, r, assets.URL+"/asset", http.StatusFound)
			return
		}
		http.Redirect(w, r, plain.URL+"/asset", http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	// The download client has no transport of its own, so it trusts the test
	// servers' certificate through the default one.
	old := http.DefaultTransport
	http.DefaultTransport = secure.Client().Transport
	defer func() { http.DefaultTransport = old }()

	dst := filepath.Join(t.TempDir(), "runner.tar.gz")
	if err := Download(context.Background(), secure.URL+"/to-https", dst); err != nil {
		t.Fatalf("a redirect to https: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "tarball bytes" {
		t.Fatalf("saved %q", b)
	}
	dst = filepath.Join(t.TempDir(), "runner.tar.gz")
	err := Download(context.Background(), secure.URL+"/to-http", dst)
	if err == nil || !strings.Contains(err.Error(), "refusing a redirect to "+plain.URL) {
		t.Fatalf("a redirect to plain http: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("what plain http sent was saved: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/system/ -run 'TestDownloadFollowsARedirectOnlyToHTTPS'`
Expected: FAIL with `host_test.go:141: a redirect to plain http: <nil>`

- [ ] **Step 3: Write the implementation**

In `internal/system/system.go`, replace

```go
// Download saves url to dst, following redirects. It sends no credentials:
// it fetches public release assets.
```

with

```go
// HTTPSRedirect is the redirect rule for what the daemon unpacks or runs: a
// redirect to anything but https is refused, so the body cannot be swapped on
// the way. It keeps http.Client's own limit of ten redirects, which setting a
// rule replaces.
func HTTPSRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refusing a redirect to %s://%s", req.URL.Scheme, req.URL.Host)
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
}

var downloadClient = &http.Client{CheckRedirect: HTTPSRedirect}

// Download saves url to dst, following redirects to https URLs. It sends no
// credentials: it fetches public release assets.
```

and in `download` replace

```go
	resp, err := http.DefaultClient.Do(req)
```

with

```go
	resp, err := downloadClient.Do(req)
```

`errors` and `fmt` are already imported there. `downloadClient` is given no transport, so it keeps using `http.DefaultTransport`; the test relies on that to trust its TLS servers.

- [ ] **Step 4: Run the tests to verify they pass**

The download client is shared, so run the packages that use it.

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/system/ ./internal/toolchain/ ./internal/runner/ ./internal/daemon/`
Expected: PASS (`ok` for `internal/system`, `internal/toolchain`, `internal/runner` and `internal/daemon`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/system` (must print nothing), then:

```bash
git add internal/system/system.go internal/system/host_test.go
git commit -m "fix(system): refuse a download redirect to http"
```

### Task 13: Version lists follow the same rule, and tar keeps no owners

**Files:**
- Modify: `internal/toolchain/env.go` (`NewEnv`, new `getClient`, `httpGet`)
- Test: `internal/toolchain/env_test.go`

**Interfaces:**
- Consumes: `system.HTTPSRedirect(req *http.Request, via []*http.Request) error` from Task 12, whose refusal reads `refusing a redirect to <scheme>://<host>`
- Produces: nothing a later task names

**Items:** 60
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`httpGet` in `internal/toolchain/env.go` reads the version lists: which versions exist, the download URL of each Node, Python and Java version, and the checksums of Go and Java. It uses `http.DefaultClient`, which follows a redirect to plain `http`, where anyone on the path can replace the list. After this task it follows a redirect only to `https`, by the rule `system.HTTPSRedirect` that the download client already uses.

The `tar` that `NewEnv` sets up to unpack an archive runs as root, and root's `tar` gives every file the owner the archive names. It gains `--no-same-owner`, as `Host.Extract` in `internal/system` already has.

- [ ] **Step 1: Write the failing tests**

In `internal/toolchain/env_test.go`, add this test directly below `TestHTTPGet`:

```go
func TestHTTPGetFollowsARedirectOnlyToHTTPS(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("sent over plain http"))
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list":
			w.Write([]byte("body"))
		case "/to-https":
			http.Redirect(w, r, "/list", http.StatusFound)
		default:
			http.Redirect(w, r, plain.URL+"/list", http.StatusFound)
		}
	}))
	defer secure.Close()
	// The version-list client has no transport of its own, so it trusts the
	// test server's certificate through the default one.
	old := http.DefaultTransport
	http.DefaultTransport = secure.Client().Transport
	defer func() { http.DefaultTransport = old }()

	if b, err := httpGet(context.Background(), secure.URL+"/to-https"); err != nil || string(b) != "body" {
		t.Fatalf("a redirect to https: %q, %v", b, err)
	}
	b, err := httpGet(context.Background(), secure.URL+"/to-http")
	if err == nil || !strings.Contains(err.Error(), "refusing a redirect to "+plain.URL) {
		t.Fatalf("a redirect to plain http: %q, %v", b, err)
	}
}
```

`TestNewEnvWiresTheRunnerAndFetch` in the same file asserts the old `tar` arguments, which this task changes. In it, replace

```go
	if want := []string{"tar -xzf a.tar.gz -C dir", "chown x", "fetch https://example.test/a.tar.gz dst"}; !slices.Equal(calls, want) {
```

with

```go
	if want := []string{"tar --no-same-owner -xzf a.tar.gz -C dir", "chown x", "fetch https://example.test/a.tar.gz dst"}; !slices.Equal(calls, want) {
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestHTTPGetFollowsARedirectOnlyToHTTPS'`
Expected: FAIL with `env_test.go:78: a redirect to plain http: "sent over plain http", <nil>`

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestNewEnvWiresTheRunnerAndFetch'`
Expected: FAIL with `env_test.go:106: calls ["tar -xzf a.tar.gz -C dir" "chown x" "fetch https://example.test/a.tar.gz dst"]`

- [ ] **Step 3: Write the implementation**

In `internal/toolchain/env.go`, in `NewEnv`, replace

```go
			_, err := run(ctx, "tar", "-xzf", archive, "-C", dir)
```

with

```go
			// As root, tar would otherwise give each file the owner the
			// archive names.
			_, err := run(ctx, "tar", "--no-same-owner", "-xzf", archive, "-C", dir)
```

Replace

```go
// httpGet reads a public version list; go.dev's full list is several MB,
// hence the generous cap.
```

with

```go
var getClient = &http.Client{CheckRedirect: system.HTTPSRedirect}

// httpGet reads a public version list; go.dev's full list is several MB,
// hence the generous cap.
```

and in `httpGet` replace

```go
	resp, err := http.DefaultClient.Do(req)
```

with

```go
	resp, err := getClient.Do(req)
```

The file already imports `internal/system`. `getClient` is given no transport, so it keeps using `http.DefaultTransport`; the test relies on that to trust its TLS server.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/toolchain`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/toolchain` (must print nothing), then:

```bash
git add internal/toolchain/env.go internal/toolchain/env_test.go
git commit -m "fix(toolchain): refuse list redirects and tar owners"
```

### Task 14: A failed Python install removes only what it made

**Files:**
- Modify: `internal/toolchain/python.go` (`Install`)
- Test: `internal/toolchain/python_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 61
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 3 = 4

The tool cache belongs to the runner user, so a job can write there and can swap a folder for a symlink. When Python's `setup.sh` fails, `Install` calls `dropFailed`, which removes the version folder's `x64` as root. It does so without the symlink check every other removal in this package makes, and also when the version folder was there before the install began, which may be a job's own `setup-python` writing it.

After this task `Install` notes whether the version folder exists just before `setup.sh` runs. After a failure it removes the version folder only when it did not exist, and only when `noSymlinks` passes for it.

- [ ] **Step 1: Write the failing tests**

Add these two tests at the end of `internal/toolchain/python_test.go`:

```go
func TestPythonFailedSetupKeepsAFolderThatWasThere(t *testing.T) {
	for name, fail := range map[string]bool{"script error": true, "no marker": false} {
		t.Run(name, func(t *testing.T) {
			f := pythonFixture(t)
			theirs := mkInstall(t, f.Root, "Python", "3.13.7", false)
			f.host.scripts["setup.sh"] = func(string, map[string]string, []string) error {
				if fail {
					return errors.New("ensurepip failed")
				}
				return nil
			}
			p := newPython(f.Env)
			rel, _ := p.Resolve(context.Background(), "3.13")
			if err := p.Install(context.Background(), rel, func(string) {}); err == nil {
				t.Fatal("no error")
			}
			if !exists(filepath.Join(theirs, "file")) {
				t.Fatal("a version folder that was there before the install was removed")
			}
		})
	}
}

func TestPythonFailedSetupRemovesNothingThroughASymlink(t *testing.T) {
	f := pythonFixture(t)
	outside := t.TempDir()
	theirs := mkInstall(t, outside, "", "3.13.7", false)
	symlinkOrSkip(t, outside, filepath.Join(t.TempDir(), "probe"))
	// <Root>/Python belongs to the runner user, so a job can swap it for a
	// link while setup.sh runs.
	f.host.scripts["setup.sh"] = func(string, map[string]string, []string) error {
		link := filepath.Join(f.Root, "Python")
		if err := os.Remove(link); err != nil {
			return err
		}
		if err := os.Symlink(outside, link); err != nil {
			return err
		}
		return errors.New("ensurepip failed")
	}
	p := newPython(f.Env)
	rel, _ := p.Resolve(context.Background(), "3.13")
	if err := p.Install(context.Background(), rel, func(string) {}); err == nil || !strings.Contains(err.Error(), "ensurepip failed") {
		t.Fatalf("got %v", err)
	}
	if !exists(filepath.Join(theirs, "file")) {
		t.Fatal("the failed install removed a folder outside the tool cache")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestPythonFailedSetup'`
Expected: FAIL with `python_test.go:154: a version folder that was there before the install was removed` (in both subtests) and `python_test.go:183: the failed install removed a folder outside the tool cache`. `TestPythonFailedSetupLeavesNoVersion`, which the pattern also runs, passes.

- [ ] **Step 3: Write the implementation**

In `internal/toolchain/python.go`, at the end of `Install`, replace

```go
	progress("running setup.sh")
	target := e.versionDir(p.dir, rel.Folder)
	env := []string{"RUNNER_TOOL_CACHE=" + e.Root, "LD_LIBRARY_PATH=" + filepath.Join(out, "lib"), "HOME=" + e.Home}
	if _, err := e.runAs(ctx, out, env, "setup.sh"); err != nil {
		dropFailed(target)
		return fmt.Errorf("setup.sh: %w", err)
	}
	if !e.has(p.dir, rel.Folder) {
		dropFailed(target)
		return errors.New("setup.sh finished without writing the .complete marker")
	}
	return nil
}
```

with

```go
	progress("running setup.sh")
	verDir := filepath.Join(e.Root, p.dir, rel.Folder)
	// A version folder that is already there may be a job's own setup-python
	// at work, and the runner user can swap any folder above it for a link.
	_, statErr := os.Lstat(verDir)
	existed := !os.IsNotExist(statErr)
	fail := func(err error) error {
		if !existed && noSymlinks(e.Root, verDir) == nil {
			os.RemoveAll(verDir)
		}
		return err
	}
	env := []string{"RUNNER_TOOL_CACHE=" + e.Root, "LD_LIBRARY_PATH=" + filepath.Join(out, "lib"), "HOME=" + e.Home}
	if _, err := e.runAs(ctx, out, env, "setup.sh"); err != nil {
		return fail(fmt.Errorf("setup.sh: %w", err))
	}
	if !e.has(p.dir, rel.Folder) {
		return fail(errors.New("setup.sh finished without writing the .complete marker"))
	}
	return nil
}
```

Leave `dropFailed` in `layout.go` as it is: `installArchive` still uses it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/toolchain`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/toolchain` (must print nothing), then:

```bash
git add internal/toolchain/python.go internal/toolchain/python_test.go
git commit -m "fix(toolchain): undo only what a Python install made"
```

### Task 15: Go file names before 1.21

**Files:**
- Modify: `internal/toolchain/golang.go` (`Resolve`, `sha256`, new `goArchive`)
- Test: `internal/toolchain/golang_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 62
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`Resolve` takes the version from the go-versions manifest and the archive with its checksum from go.dev. The manifest writes every release with three parts (`1.20.0`), and go.dev names the first release of a minor before 1.21 without the `.0` (`go1.20.linux-amd64.tar.gz`). So Go `1.20.0` and `1.19.0` fail today with `go.dev lists no SHA-256 for go1.20.0.linux-amd64.tar.gz`.

After this task, when go.dev lists no checksum under the manifest's spelling and the version is `x.y.0`, `Resolve` uses `go<x.y>.linux-amd64.tar.gz`. The version and the installed folder keep three parts.

- [ ] **Step 1: Write the failing test**

In `internal/toolchain/golang_test.go`, add this test directly below `TestGoResolvesFromTheManifestAndGoDev`:

```go
// The go-versions manifest writes every release with three parts; go.dev
// names the first release of a minor before 1.21 without its ".0".
func TestGoResolvesADotZeroReleaseUnderGoDevsName(t *testing.T) {
	f := newFixture(t)
	f.serve("/go.json", []byte(`[
 {"version":"1.21.0","stable":true,"files":[{"filename":"go-1.21.0-linux-x64.tar.gz","arch":"x64","platform":"linux","download_url":"https://example.invalid/a"}]},
 {"version":"1.20.0","stable":true,"files":[{"filename":"go-1.20.0-linux-x64.tar.gz","arch":"x64","platform":"linux","download_url":"https://example.invalid/b"}]},
 {"version":"1.19.0","stable":true,"files":[{"filename":"go-1.19.0-linux-x64.tar.gz","arch":"x64","platform":"linux","download_url":"https://example.invalid/c"}]},
 {"version":"1.18.10","stable":true,"files":[{"filename":"go-1.18.10-linux-x64.tar.gz","arch":"x64","platform":"linux","download_url":"https://example.invalid/d"}]}
]`))
	f.serve("/godl.json", goReleases(map[string]string{
		"go1.21.0.linux-amd64.tar.gz": "210",
		"go1.21.linux-amd64.tar.gz":   "21",
		"go1.20.linux-amd64.tar.gz":   "20",
		"go1.18.1.linux-amd64.tar.gz": "181",
	}))
	g := newGo(f.Env)
	rel, err := g.Resolve(context.Background(), "1.20.0")
	if want := (Release{Tool: "go", Version: "1.20.0", Folder: "1.20.0", URL: f.url + "/dl/go1.20.linux-amd64.tar.gz", SHA256: "20"}); err != nil || rel != want {
		t.Fatalf("1.20.0 resolved %+v, %v", rel, err)
	}
	rel, err = g.Resolve(context.Background(), "1.21.0")
	if want := (Release{Tool: "go", Version: "1.21.0", Folder: "1.21.0", URL: f.url + "/dl/go1.21.0.linux-amd64.tar.gz", SHA256: "210"}); err != nil || rel != want {
		t.Fatalf("1.21.0 resolved %+v, %v; the manifest's spelling comes first", rel, err)
	}
	if _, err := g.Resolve(context.Background(), "1.19.0"); err == nil || !strings.Contains(err.Error(), "go.dev lists no SHA-256 for go1.19.0.linux-amd64.tar.gz") {
		t.Fatalf("a release go.dev lists under neither name: %v", err)
	}
	if _, err := g.Resolve(context.Background(), "1.18.10"); err == nil {
		t.Fatal("1.18.10 is not a first release, so it has no shorter name")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestGoResolvesADotZeroReleaseUnderGoDevsName'`
Expected: FAIL with `golang_test.go:73: 1.20.0 resolved {Tool: Version: Folder: URL: SHA256:}, go.dev lists no SHA-256 for go1.20.0.linux-amd64.tar.gz`

- [ ] **Step 3: Write the implementation**

In `internal/toolchain/golang.go`, replace the import block

```go
import (
	"context"
	"encoding/json"
	"fmt"
)
```

with

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)
```

Then replace `Resolve` and `sha256`, from the comment above `Resolve` down to the closing brace of `sha256` (leave `Install` below them as it is), which read

```go
// Resolve picks the version from the go-versions manifest, which setup-go
// resolves "stable" from, and downloads the official go.dev archive, which
// publishes a SHA-256.
func (g golang) Resolve(ctx context.Context, spec string) (Release, error) {
	v, _, err := g.pick(ctx, spec)
	if err != nil {
		return Release{}, err
	}
	name := "go" + v + ".linux-amd64.tar.gz"
	sum, err := g.sha256(ctx, name)
	if err != nil {
		return Release{}, err
	}
	sv := makeSemver(v)
	return Release{Tool: "go", Version: sv, Folder: sv, URL: g.e.Sources.GoDownload + name, SHA256: sum}, nil
}

func (g golang) sha256(ctx context.Context, name string) (string, error) {
	b, err := g.e.Get(ctx, g.e.Sources.GoReleases)
	if err != nil {
		return "", fmt.Errorf("go.dev releases: %w", err)
	}
	var rs []struct {
		Files []struct {
			Filename string `json:"filename"`
			SHA256   string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &rs); err != nil {
		return "", fmt.Errorf("go.dev releases: %w", err)
	}
	for _, r := range rs {
		for _, f := range r.Files {
			if f.Filename == name && f.SHA256 != "" {
				return f.SHA256, nil
			}
		}
	}
	return "", fmt.Errorf("go.dev lists no SHA-256 for %s", name)
}
```

with

```go
// Resolve picks the version from the go-versions manifest, which setup-go
// resolves "stable" from, and downloads the official go.dev archive, which
// publishes a SHA-256.
func (g golang) Resolve(ctx context.Context, spec string) (Release, error) {
	v, _, err := g.pick(ctx, spec)
	if err != nil {
		return Release{}, err
	}
	names := []string{goArchive(v)}
	// The manifest writes every release with three parts; go.dev names the
	// first release of a minor before 1.21 without its ".0".
	if short, ok := strings.CutSuffix(v, ".0"); ok && strings.Count(v, ".") == 2 {
		names = append(names, goArchive(short))
	}
	name, sum, err := g.sha256(ctx, names)
	if err != nil {
		return Release{}, err
	}
	sv := makeSemver(v)
	return Release{Tool: "go", Version: sv, Folder: sv, URL: g.e.Sources.GoDownload + name, SHA256: sum}, nil
}

func goArchive(v string) string { return "go" + v + ".linux-amd64.tar.gz" }

// sha256 returns the first of names go.dev lists a SHA-256 for, with it.
func (g golang) sha256(ctx context.Context, names []string) (string, string, error) {
	b, err := g.e.Get(ctx, g.e.Sources.GoReleases)
	if err != nil {
		return "", "", fmt.Errorf("go.dev releases: %w", err)
	}
	var rs []struct {
		Files []struct {
			Filename string `json:"filename"`
			SHA256   string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &rs); err != nil {
		return "", "", fmt.Errorf("go.dev releases: %w", err)
	}
	for _, name := range names {
		for _, r := range rs {
			for _, f := range r.Files {
				if f.Filename == name && f.SHA256 != "" {
					return name, f.SHA256, nil
				}
			}
		}
	}
	return "", "", fmt.Errorf("go.dev lists no SHA-256 for %s", names[0])
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/toolchain`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/toolchain` (must print nothing), then:

```bash
git add internal/toolchain/golang.go internal/toolchain/golang_test.go
git commit -m "fix(toolchain): resolve Go x.y.0 by go.dev's name"
```

### Task 16: Removing a Java folder whose name contains `+`

**Files:**
- Modify: `internal/toolchain/java.go` (`Remove`)
- Test: `internal/toolchain/java_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 64
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`setup-java` names a JDK's folder after its version with the first `+` turned into `-`, and `Installed` shows a folder with its first `-` turned back into `+`. `Remove` always turns the `+` of the version it is given into `-`, so a folder that really has the `+` in its name is listed but can never be removed: `Remove` answers `toolchain version not installed`.

After this task `Remove` deletes the folder named exactly as the version when that folder is a complete install, and otherwise the folder with the first `+` turned into `-`, as today.

- [ ] **Step 1: Write the failing test**

Add this test at the end of `internal/toolchain/java_test.go`:

```go
// A folder that keeps the "+" in its name is listed under that name, so it
// must be removable under it.
func TestJavaRemovesAFolderNamedWithAPlus(t *testing.T) {
	const version = "21.0.12+101.0.LTS"
	f := newFixture(t)
	j := newJava(f.Env)
	plus := mkInstall(t, f.Root, javaDir, version, true)
	if got, err := j.Installed(); err != nil || len(got) != 1 || got[0].Version != version {
		t.Fatalf("installed %+v, %v", got, err)
	}
	if err := j.Remove(version); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if exists(plus) {
		t.Fatal("the folder named with a plus survived")
	}

	plus = mkInstall(t, f.Root, javaDir, version, true)
	dash := mkInstall(t, f.Root, javaDir, "21.0.12-101.0.LTS", true)
	if err := j.Remove(version); err != nil || exists(plus) || !exists(dash) {
		t.Fatalf("with both folders the exact name goes first: %v", err)
	}
	if err := j.Remove(version); err != nil || exists(dash) {
		t.Fatalf("then the folder setup-java names: %v", err)
	}
	if err := j.Remove(version); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("third remove: %v", err)
	}

	unfinished := mkInstall(t, f.Root, javaDir, version, false)
	dash = mkInstall(t, f.Root, javaDir, "21.0.12-101.0.LTS", true)
	if err := j.Remove(version); err != nil || exists(dash) || !exists(unfinished) {
		t.Fatalf("an unfinished folder under the exact name is not the installed version: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestJavaRemovesAFolderNamedWithAPlus'`
Expected: FAIL with `java_test.go:96: remove: toolchain version not installed`

- [ ] **Step 3: Write the implementation**

In `internal/toolchain/java.go`, replace

```go
func (j java) Remove(version string) error {
	return j.e.removeLayout(javaDir, strings.Replace(version, "+", "-", 1))
}
```

with

```go
// Remove takes a version as Installed shows it, which fits two folders: one
// that really has the "+" in its name, and the one setup-java names with "-".
// The exact name wins.
func (j java) Remove(version string) error {
	folder := strings.Replace(version, "+", "-", 1)
	if validFolder(version) && j.e.has(javaDir, version) {
		folder = version
	}
	return j.e.removeLayout(javaDir, folder)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/toolchain`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/toolchain` (must print nothing), then:

```bash
git add internal/toolchain/java.go internal/toolchain/java_test.go
git commit -m "fix(toolchain): remove a Java folder named with +"
```

### Task 17: The `.tmp` directory without a check-then-use gap

**Files:**
- Modify: `internal/toolchain/layout.go` (`opDir`, new `openOwned`)
- Test: `internal/toolchain/hardening_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `openOwned(path string) (*os.Root, error)`

**Items:** 65
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 3 = 4

Every install works in a fresh directory under `<Root>/.tmp`. `<Root>` belongs to the runner user, so a job can rename `.tmp` away and put a symlink in its place at any moment. `opDir`, running as root, looks at `.tmp` with `Lstat` and then calls `Chmod` and `Mkdir` by path. A swap between the look and the use makes root change the mode of the link's target and create a directory inside it.

After this task `opDir` still removes and remakes a `.tmp` that is a link or belongs to someone else, then opens the directory once and does the rest through that handle (`os.Root`): the mode change and the new directory can no longer land anywhere else. `os.OpenRoot` follows a symlink at the path it is given, so the new helper `openOwned` compares what it opened with what the path itself is and refuses a link. The task has two test-then-code cycles, one for each half.

- [ ] **Step 1: Write the first failing test**

In `internal/toolchain/hardening_test.go`, add this test directly below `TestOpDirDoesNotFollowASymlinkedTmp`. The fixture's `OpID` runs inside `opDir` after it has looked at `.tmp` and before it makes the operation directory, so the test swaps `.tmp` there.

```go
// The tool cache belongs to the runner user, so a job can swap .tmp for a
// link at any moment. OpID runs after opDir has looked at .tmp and before it
// makes the operation directory.
func TestOpDirDoesNotFollowATmpSwappedForALink(t *testing.T) {
	f := newFixture(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.Mkdir(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, victim, filepath.Join(t.TempDir(), "probe"))
	tmp, moved := filepath.Join(f.Root, ".tmp"), filepath.Join(f.Root, ".moved")
	f.OpID = func() string {
		if err := os.Rename(tmp, moved); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(victim, tmp); err != nil {
			t.Error(err)
		}
		return "op1"
	}
	f.opDir()
	if es, _ := os.ReadDir(victim); len(es) != 0 {
		t.Fatalf("an operation directory was created through the link: %v", es)
	}
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(victim); err != nil || st.Mode().Perm() != 0o700 {
			t.Fatalf("the link's target was chmodded: %v, %v", st, err)
		}
	}
	if !exists(filepath.Join(moved, "op1")) {
		t.Fatal("the operation directory was not made in the directory opDir had opened")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestOpDirDoesNotFollowATmpSwappedForALink'`
Expected: FAIL with `hardening_test.go:79: an operation directory was created through the link: [d op1/]`

- [ ] **Step 3: Use `.tmp` through one handle**

In `internal/toolchain/layout.go`, replace the body of `opDir` from its first comment to its closing brace, which reads

```go
	// A job can plant a symlink or its own directory here; Chmod would follow
	// the link as root and a foreign directory would let the job swap an
	// operation directory for a link.
	if fi, err := os.Lstat(tmp); err == nil && (!fi.IsDir() || !ownedByMe(fi)) {
		if err := os.RemoveAll(tmp); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(tmp, 0o711); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o711); err != nil {
		return "", err
	}
	dir := filepath.Join(tmp, e.OpID())
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
```

with

```go
	// A job can plant a symlink or its own directory here; a foreign directory
	// would let the job swap an operation directory for a link.
	if fi, err := os.Lstat(tmp); err == nil && (!fi.IsDir() || !ownedByMe(fi)) {
		if err := os.RemoveAll(tmp); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(tmp, 0o711); err != nil {
		return "", err
	}
	// The job can swap .tmp again after that look, so the rest goes through
	// one handle on the directory and never through its path.
	r, err := openOwned(tmp)
	if err != nil {
		return "", err
	}
	defer r.Close()
	if err := r.Chmod(".", 0o711); err != nil {
		return "", err
	}
	id := e.OpID()
	if err := r.Mkdir(id, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(tmp, id), nil
}

// openOwned opens the directory at path, and only one that belongs to this
// process.
func openOwned(path string) (*os.Root, error) {
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := r.Stat(".")
	if err == nil && !ownedByMe(opened) {
		err = fmt.Errorf("%s is a link or belongs to another user", path)
	}
	if err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}
```

Keep the call to `e.OpID()` between the `Chmod` and the `Mkdir`: the test of Step 1 swaps `.tmp` there.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestOpDir'`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/toolchain`)

- [ ] **Step 5: Write the second failing test**

`openOwned` does not yet tell a link from the directory it points to. In `internal/toolchain/hardening_test.go`, add this test directly below `TestOpDirDoesNotFollowATmpSwappedForALink`:

```go
func TestOpenOwnedOpensOnlyTheDirectoryItself(t *testing.T) {
	dir := t.TempDir()
	own := filepath.Join(dir, "own")
	if err := os.Mkdir(own, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := openOwned(own)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	symlinkOrSkip(t, own, link)
	for _, path := range []string{link, file, filepath.Join(dir, "missing")} {
		if r, err := openOwned(path); err == nil {
			r.Close()
			t.Errorf("%s was opened as a directory of this user", path)
		}
	}
}
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/ -run 'TestOpenOwnedOpensOnlyTheDirectoryItself'`
Expected: FAIL with `hardening_test.go:111: <a temporary path>/link was opened as a directory of this user`

- [ ] **Step 7: Refuse a link in `openOwned`**

In `internal/toolchain/layout.go`, replace

```go
// openOwned opens the directory at path, and only one that belongs to this
// process.
```

with

```go
// openOwned opens the directory at path, and only one that belongs to this
// process. os.OpenRoot follows a link at path, so what it opened is compared
// with what path itself is.
```

and in `openOwned` replace

```go
	opened, err := r.Stat(".")
	if err == nil && !ownedByMe(opened) {
		err = fmt.Errorf("%s is a link or belongs to another user", path)
	}
```

with

```go
	opened, err := r.Stat(".")
	if err == nil {
		var named os.FileInfo
		named, err = os.Lstat(path)
		if err == nil && (!os.SameFile(named, opened) || !ownedByMe(opened)) {
			err = fmt.Errorf("%s is a link or belongs to another user", path)
		}
	}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/toolchain/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/toolchain`)

- [ ] **Step 9: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/toolchain` (must print nothing), then:

```bash
git add internal/toolchain/layout.go internal/toolchain/hardening_test.go
git commit -m "fix(toolchain): use .tmp through one handle"
```

### Task 18: Hard links counted once

**Files:**
- Modify: `internal/storage/measure.go` (`walkCtx`, new `inode`)
- Modify: `internal/storage/own_unix.go` (new `hardLink`)
- Modify: `internal/storage/own_other.go` (new `hardLink`)
- Test: `internal/storage/measure_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `type inode struct{ dev, ino uint64 }`; `hardLink(fi os.FileInfo) (inode, bool)`

**Items:** 66
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The Storage page sizes each toolchain, folder and package cache with `walkCtx`, which adds the space of every directory entry. A file with several names (a hard link, as package stores use) is added once per name, so a cache can show more space than it takes on disk.

After this task a file with more than one link adds its space once per device and inode within one walk, as `du` does. Every name still counts as a file, and the newest time is unchanged. Device, inode and link count come from `syscall.Stat_t`, which exists only on Unix, so the lookup lives beside `allocated` in the build-tagged files and the other build never treats a file as linked.

- [ ] **Step 1: Write the failing test**

In `internal/storage/measure_test.go`, replace the start of the import block

```go
import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
```

with

```go
import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
```

and add this test directly below `TestWalkDoesNotFollowSymlinks`:

```go
// A package store keeps one file under several names. It takes its space
// once, as du reports it, while each name still counts as a file.
func TestWalkCountsAHardLinkedFileOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links are told apart by device and inode")
	}
	const size = 256 << 10
	// Random, so a compressing filesystem cannot shrink it.
	body := make([]byte, size)
	rand.Read(body)
	dir := t.TempDir()
	stored := filepath.Join(dir, "store", "a")
	writeFile(t, stored, string(body))
	if err := os.Mkdir(filepath.Join(dir, "links"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(stored, filepath.Join(dir, "links", "b")); err != nil {
		t.Skip("no hard links here:", err)
	}
	written := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(stored, written, written); err != nil {
		t.Fatal(err)
	}
	u, ok, err := walk(dir)
	if err != nil || !ok {
		t.Fatalf("ok %v, err %v", ok, err)
	}
	if u.Files != 2 || !u.Last.Equal(written) {
		t.Fatalf("usage %+v, want both names counted as files", u)
	}
	if u.Bytes < size || u.Bytes >= 2*size {
		t.Fatalf("%d bytes for one %d-byte file with two names", u.Bytes, size)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/storage/ -run 'TestWalkCountsAHardLinkedFileOnce'`
Expected: FAIL with `measure_test.go:111: 536576 bytes for one 262144-byte file with two names` (the first number is two copies of the file plus the three directories, so it varies a little with the filesystem)

- [ ] **Step 3: Write the implementation**

In `internal/storage/own_unix.go`, add at the end of the file:

```go
// hardLink identifies fi when it is a file with more than one name. A
// directory's link count is its subdirectories, not other names.
func hardLink(fi os.FileInfo) (inode, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || fi.IsDir() || st.Nlink < 2 {
		return inode{}, false
	}
	return inode{uint64(st.Dev), uint64(st.Ino)}, true
}
```

In `internal/storage/own_other.go`, add at the end of the file:

```go
func hardLink(os.FileInfo) (inode, bool) { return inode{}, false }
```

In `internal/storage/measure.go`, replace the comment above `walk`

```go
// walk sums the space taken under path (st_blocks × 512, as du counts),
// counts regular files and finds the newest file modification time. It
// never follows a symlink: WalkDir reports entries with Lstat. ok is false
// when path does not exist. Entries that vanish mid-walk, as a running job
// deletes them, are skipped; the first other error is returned with the
// partial sums.
```

with

```go
// inode names one file on one filesystem.
type inode struct{ dev, ino uint64 }

// walk sums the space taken under path as du counts it (st_blocks × 512, a
// file with several names once), counts regular files, every name as one,
// and finds the newest file modification time. It never follows a symlink:
// WalkDir reports entries with Lstat. ok is false when path does not exist.
// Entries that vanish mid-walk, as a running job deletes them, are skipped;
// the first other error is returned with the partial sums.
```

and in `walkCtx` replace

```go
	var first error
	walkErr := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err == nil {
			var fi fs.FileInfo
			if fi, err = d.Info(); err == nil {
				u.Bytes += allocated(fi)
				if fi.Mode().IsRegular() {
```

with

```go
	var first error
	seen := map[inode]bool{}
	walkErr := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if err == nil {
			var fi fs.FileInfo
			if fi, err = d.Info(); err == nil {
				if id, linked := hardLink(fi); !linked {
					u.Bytes += allocated(fi)
				} else if !seen[id] {
					seen[id] = true
					u.Bytes += allocated(fi)
				}
				if fi.Mode().IsRegular() {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/storage/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/storage`)

Then check the build without `syscall.Stat_t`. Run: `GOOS=windows PATH=/usr/local/go/bin:$PATH go vet ./internal/storage/`
Expected: no output

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/storage` (must print nothing), then:

```bash
git add internal/storage/measure.go internal/storage/measure_test.go internal/storage/own_unix.go internal/storage/own_other.go
git commit -m "fix(storage): count a hard-linked file once"
```

### Task 19: Check Docker's apt key

**Files:**
- Modify: `deploy/setup.sh` (`install_docker`, `write_docker_source`, new `export_docker_key` and `install_docker_key`)
- Test: `deploy/tests/setup_test.sh` (section `(f)`, `new_case`, section `(e)`)

**Interfaces:**
- Consumes: nothing
- Produces: `KEYRING_DIR` and `DOCKER_KEY_FINGERPRINT` in `deploy/setup.sh`; in the test script, `KEYRING_DIR` redirected into `$T` for the whole run (`DEFAULT_KEYRING_DIR` keeps the script's value), set per case by `new_case`, and checked by `run`

**Items:** 67
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 3 = 4

`install_docker` downloads Docker's apt key straight into `/etc/apt/keyrings/docker.asc` and trusts whatever arrives. apt trusts every key in a `Signed-By` file and may read a crafted file differently from `gpg`, so checking the downloaded file and then installing the same bytes is not enough. After this task the key is downloaded once into a fresh temporary directory, imported into a throwaway keyring (a `--homedir` of mode 0700 inside it), and only `gpg`'s own export of the pinned key (primary fingerprint `9DC858229FC7DD38854AE2D88D81803C0EBFCD88`, the full 40 characters) is installed to `docker.asc`. Any failure before the install (download, import, the pinned key missing, a different primary fingerprint, an empty or header-less export) stops with `Docker apt key has an unexpected fingerprint` and writes nothing to the keyring directory, so an existing `docker.asc` stays as it was. `gnupg` joins `ca-certificates` and `curl` in the first `apt-get install`. The key directory becomes a variable, `KEYRING_DIR`, so the tests can redirect it; the test script redirects it into its temporary tree for the whole run, and its `run` helper refuses to start a function while it points anywhere else.

Do not run `deploy/setup.sh` itself on a development host; it installs Docker. The steps below run only `deploy/tests/setup_test.sh`, which replaces every external command with a function and points every path into a temporary directory.

- [ ] **Step 1: Make the keyring directory a variable**

This changes no behaviour. It must come first: without it the failing test of Step 3 would run the old code against the real `/etc/apt/keyrings`.

In `deploy/setup.sh`, replace

```bash
APT_SOURCES_DIR=/etc/apt/sources.list.d
```

with

```bash
APT_SOURCES_DIR=/etc/apt/sources.list.d
KEYRING_DIR=/etc/apt/keyrings
```

In `deploy/setup.sh`, replace

```bash
Signed-By: /etc/apt/keyrings/docker.asc
EOF
```

with

```bash
Signed-By: $KEYRING_DIR/docker.asc
EOF
```

In `deploy/setup.sh`, replace

```bash
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
```

with

```bash
  install -m 0755 -d "$KEYRING_DIR"
  curl -fsSL https://download.docker.com/linux/debian/gpg -o "$KEYRING_DIR/docker.asc"
  chmod a+r "$KEYRING_DIR/docker.asc"
```

- [ ] **Step 2: Write the failing test**

Five edits in `deploy/tests/setup_test.sh`. First, the whole run is redirected: the keyring directory points into the temporary tree before any case runs, and `run`, which starts every `setup.sh` function under test, refuses to start while it points anywhere else. This keeps the tests away from the real `/etc/apt/keyrings`.

In `deploy/tests/setup_test.sh`, replace

```bash
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
```

with

```bash
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

# setup.sh installs Docker's key under /etc. The directory is redirected into $T
# before any case runs; run refuses to start a function while it points elsewhere.
DEFAULT_KEYRING_DIR=$KEYRING_DIR
KEYRING_DIR="$T/keyrings"
```

In `deploy/tests/setup_test.sh`, replace

```bash
# Runs a setup.sh function the way main runs: errexit on, die exits a subshell.
run() { (set -e; "$@"); }
```

with

```bash
# Runs a setup.sh function the way main runs: errexit on, die exits a subshell.
# It refuses to run while a redirected directory points outside the temporary tree.
run() {
  case "$KEYRING_DIR" in
    "$T"/*) ;;
    *) echo "refusing to run: KEYRING_DIR=$KEYRING_DIR is outside $T" >&2; return 99 ;;
  esac
  (set -e; "$@")
}
```

Second, every case gets its own keyring directory.

In `deploy/tests/setup_test.sh`, replace

```bash
  OPT_DIR="$T/$1/opt"
  mkdir -p "$DIST_DIR" "$MARK_DIR" "$STATE_DIR" "$OPT_DIR"
```

with

```bash
  OPT_DIR="$T/$1/opt"
  KEYRING_DIR="$T/$1/keyrings"
  mkdir -p "$DIST_DIR" "$MARK_DIR" "$STATE_DIR" "$OPT_DIR"
```

Third, section `(e)` expects the real default path in the `Signed-By` line. It now expects the redirected directory there, and checks the script's default separately.

In `deploy/tests/setup_test.sh`, replace

```bash
  "Architectures: amd64" "Signed-By: /etc/apt/keyrings/docker.asc"; do
  check "e: docker.sources has $line" contains "$src" "$line"
done
```

with

```bash
  "Architectures: amd64" "Signed-By: $KEYRING_DIR/docker.asc"; do
  check "e: docker.sources has $line" contains "$src" "$line"
done
check "e: Docker's key goes to /etc/apt/keyrings by default" [ "$DEFAULT_KEYRING_DIR" = /etc/apt/keyrings ]
```

Fourth, section `(f)` is rewritten. The existing `f-apt` case stubbed `curl`, `install` and `chmod` and checked only the apt line; it now also needs the key steps to succeed, so it runs against the fake `gpg` too. The fake `gpg` reads the "downloaded" file as lines of `KEY <primary fingerprint> [<subkey fingerprint> ...]`, imports, lists and exports those keys the way `gpg` does, and refuses to run (status 99) without a mode-0700 `--homedir`. `install` is not stubbed: the key is really installed into the per-case `KEYRING_DIR`.

In `deploy/tests/setup_test.sh`, replace

```bash
# (f) The apt step installs the libraries the python-versions builds link.
new_case f-apt
out=$( (
  apt-get() { echo "apt-get $*" >> "$MARK_DIR/apt"; }
  curl() { :; }
  install() { :; }
  chmod() { :; }
  write_docker_source() { :; }
  systemctl() { :; }
  docker() { :; }
  run install_docker
) 2>&1)
rc=$?
check "f: install_docker succeeds with stubs" [ "$rc" -eq 0 ]
apt=$(cat "$MARK_DIR/apt" 2>/dev/null)
for lib in libssl3t64 libffi8 libsqlite3-0 liblzma5 libbz2-1.0 libgdbm6t64 libncursesw6 libreadline8t64 libtcl8.6 libtk8.6; do
  check "f: apt installs $lib" contains "$apt" " $lib"
done
```

with

```bash
# (f) The apt step installs the libraries the python-versions builds link, and
# Docker's apt key only as gpg's own export of the pinned key. The downloaded
# bytes are never installed.
# fake_docker_key makes curl save $1 as the download. The fake gpg reads it as
# lines of "KEY <primary fpr> [<subkey fpr> ...]" and ignores any other line, then
# imports, lists and exports those keys the way gpg does: a key it does not hold
# fails to list but exports nothing with status 0. It refuses to run without a
# private, empty-at-start --homedir, so a run can never touch the real keyring.
# CURL_RC, GPG_IMPORT_RC and GPG_EXPORT (empty or noheader) inject failures.
DOCKER_FPR=9DC858229FC7DD38854AE2D88D81803C0EBFCD88
OTHER_FPR=D3306A018370199E527AE7997EA0A9C3F273FCD8
ZERO_FPR=0000000000000000000000000000000000000000
fake_docker_key() {
  KEY_DOWNLOAD="$1"
  curl() {
    local out=""
    while [ $# -gt 0 ]; do
      [ "$1" != -o ] || out="$2"
      shift
    done
    echo download >> "$MARK_DIR/curls"
    dirname "$out" > "$MARK_DIR/workdir"
    [ "${CURL_RC:-0}" = 0 ] || return "$CURL_RC"
    printf '%s\n' "$KEY_DOWNLOAD" > "$out"
  }
  gpg() {
    local home="" cmd="" want="" line
    echo "$*" >> "$MARK_DIR/gpg-calls"
    while [ $# -gt 0 ]; do
      case "$1" in
        --homedir) home="$2"; shift ;;
        --import | --list-keys | --export) cmd="$1"; want="$2"; shift ;;
      esac
      shift
    done
    [ -d "$home" ] && [ "$(stat -c %a "$home")" = 700 ] || return 99
    case "$cmd" in
      --import)
        [ "${GPG_IMPORT_RC:-0}" = 0 ] || return "$GPG_IMPORT_RC"
        cp "$want" "$home/keyring"
        ;;
      --list-keys | --export)
        line=$(awk -v f="$want" '$1 == "KEY" { for (i = 2; i <= NF; i++) if ($i == f) { print; exit } }' "$home/keyring")
        if [ "$cmd" = --list-keys ]; then
          [ -n "$line" ] || return 2
          read -ra k <<<"$line"
          printf 'pub:-:4096:1:8D81803C0EBFCD88:1487788586:::-:::scESCA::::::23::0:\nfpr:::::::::%s:\n' "${k[1]}"
          for sub in "${k[@]:2}"; do printf 'sub:-:4096:1:7EA0A9C3F273FCD8:1487788586::::::s::::::23:\nfpr:::::::::%s:\n' "$sub"; done
        elif [ -n "$line" ] && [ "${GPG_EXPORT:-}" != empty ]; then
          read -ra k <<<"$line"
          [ "${GPG_EXPORT:-}" = noheader ] || echo "-----BEGIN PGP PUBLIC KEY BLOCK-----"
          echo "EXPORT ${k[1]}"
        fi
        ;;
    esac
  }
  gpgconf() { echo "$*" >> "$MARK_DIR/gpgconf-calls"; }
}
install_docker_stubs() {
  apt-get() { echo "apt-get $*" >> "$MARK_DIR/apt"; }
  write_docker_source() { :; }
  systemctl() { :; }
  docker() { :; }
}
key_install() {
  out=$( (install_docker_stubs; fake_docker_key "$1"; run install_docker) 2>&1)
  rc=$?
}
refuses() {
  new_case "f-key-$1"
  key_install "$2"
  check "f: $1 stops the install" [ "$rc" -ne 0 ]
  check "f: $1 says the fingerprint is unexpected" contains "$out" "Docker apt key has an unexpected fingerprint"
  check "f: $1 writes nothing to the keyring directory" absent "$KEYRING_DIR"
  check "f: $1 stops before the Docker packages" lacks "$(cat "$MARK_DIR/apt")" "docker-ce"
  check "f: $1 removes its working directory" absent "$(cat "$MARK_DIR/workdir")"
}

new_case f-apt
key_install "KEY $DOCKER_FPR $OTHER_FPR"
check "f: install_docker succeeds with stubs" [ "$rc" -eq 0 ]
apt=$(cat "$MARK_DIR/apt" 2>/dev/null)
for lib in libssl3t64 libffi8 libsqlite3-0 liblzma5 libbz2-1.0 libgdbm6t64 libncursesw6 libreadline8t64 libtcl8.6 libtk8.6; do
  check "f: apt installs $lib" contains "$apt" " $lib"
done
check "f: apt installs gnupg with ca-certificates and curl" contains "$apt" "ca-certificates curl gnupg"
check "f: the key is downloaded once" [ "$(wc -l < "$MARK_DIR/curls")" -eq 1 ]
check "f: the installed key is gpg's export of the pinned key" [ "$(cat "$KEYRING_DIR/docker.asc")" = "$(printf '%s\n' '-----BEGIN PGP PUBLIC KEY BLOCK-----' "EXPORT $DOCKER_FPR")" ]
check "f: docker.asc is world-readable" [ "$(stat -c %a "$KEYRING_DIR/docker.asc")" = 644 ]
check "f: gpg ran three times: import, list, export" [ "$(wc -l < "$MARK_DIR/gpg-calls")" -eq 3 ]
check "f: every gpg call used a throwaway --homedir" [ "$(grep -vc '^--homedir /' "$MARK_DIR/gpg-calls")" -eq 0 ]
check "f: every gpg call ran in batch mode without a terminal" [ "$(grep -vc -e '--batch --no-tty' "$MARK_DIR/gpg-calls")" -eq 0 ]
check "f: the throwaway gpg-agent is stopped" contains "$(cat "$MARK_DIR/gpgconf-calls")" "--kill all"
check "f: and the working directory is removed" absent "$(cat "$MARK_DIR/workdir")"

new_case f-key-extra
key_install "KEY $DOCKER_FPR
KEY $ZERO_FPR $OTHER_FPR
TRAILING-DATA-AFTER-THE-KEY"
check "f: a download holding a second key installs" [ "$rc" -eq 0 ]
check "f: only the pinned key's export reaches the keyring" [ "$(cat "$KEYRING_DIR/docker.asc")" = "$(printf '%s\n' '-----BEGIN PGP PUBLIC KEY BLOCK-----' "EXPORT $DOCKER_FPR")" ]
check "f: the second key is not in docker.asc" lacks "$(cat "$KEYRING_DIR/docker.asc")" "$ZERO_FPR"
check "f: trailing data is not in docker.asc" lacks "$(cat "$KEYRING_DIR/docker.asc")" "TRAILING"
check "f: nothing else is in the keyring directory" [ "$(ls "$KEYRING_DIR")" = docker.asc ]

refuses missing-pinned-key "KEY $ZERO_FPR"
refuses empty-download ""
refuses pinned-only-as-subkey "KEY $ZERO_FPR $DOCKER_FPR"
refuses lowercase-primary "KEY ${DOCKER_FPR,,} $DOCKER_FPR"
refuses short-primary "KEY ${DOCKER_FPR:0:39} $DOCKER_FPR"
refuses long-primary "KEY ${DOCKER_FPR}0 $DOCKER_FPR"
CURL_RC=22 refuses failed-download "KEY $DOCKER_FPR"
GPG_IMPORT_RC=2 refuses failed-import "KEY $DOCKER_FPR"
GPG_EXPORT=empty refuses empty-export "KEY $DOCKER_FPR"
GPG_EXPORT=noheader refuses export-without-armor "KEY $DOCKER_FPR"

new_case f-key-existing
mkdir -p "$KEYRING_DIR"
echo "key from an earlier run" > "$KEYRING_DIR/docker.asc"
GPG_EXPORT=empty key_install "KEY $DOCKER_FPR"
check "f: a failed run stops the install over an existing docker.asc" [ "$rc" -ne 0 ]
check "f: and leaves the earlier docker.asc as it was" [ "$(cat "$KEYRING_DIR/docker.asc")" = "key from an earlier run" ]
check "f: and adds nothing next to it" [ "$(ls "$KEYRING_DIR")" = docker.asc ]
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `bash deploy/tests/setup_test.sh`
Expected: FAIL with `FAIL f: apt installs gnupg with ca-certificates and curl`, among other `FAIL f: ...` lines (the key is still installed unchecked, so `the installed key is gpg's export of the pinned key`, every `refuses` case and the `gpg ran three times` lines fail too), ending with `60 assertion(s) failed`

- [ ] **Step 4: Write the implementation**

In `deploy/setup.sh`, replace

```bash
KEYRING_DIR=/etc/apt/keyrings
```

with

```bash
KEYRING_DIR=/etc/apt/keyrings
DOCKER_KEY_FINGERPRINT=9DC858229FC7DD38854AE2D88D81803C0EBFCD88
```

In `deploy/setup.sh`, replace

```bash
install_docker() {
  log "installing or upgrading Docker Engine and base packages"
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl
  install -m 0755 -d "$KEYRING_DIR"
  curl -fsSL https://download.docker.com/linux/debian/gpg -o "$KEYRING_DIR/docker.asc"
  chmod a+r "$KEYRING_DIR/docker.asc"
  write_docker_source
```

with

```bash
# Downloads Docker's apt key into $1, imports it into a throwaway keyring and
# writes gpg's export of the pinned key to $1/docker.asc. Every step is in one
# && chain, because errexit does not apply inside a function called from `if`.
export_docker_key() {
  local work="$1" home="$1/gnupg" records fpr
  mkdir -m 0700 "$home" &&
    curl -fsSL https://download.docker.com/linux/debian/gpg -o "$work/download.asc" &&
    gpg --homedir "$home" --batch --no-tty --quiet --import "$work/download.asc" &&
    records=$(gpg --homedir "$home" --batch --no-tty --with-colons --list-keys "$DOCKER_KEY_FINGERPRINT") &&
    fpr=$(awk -F: 'prev == "pub" && $1 == "fpr" { print $10; exit } { prev = $1 }' <<<"$records") &&
    [ "$fpr" = "$DOCKER_KEY_FINGERPRINT" ] &&
    gpg --homedir "$home" --batch --no-tty --armor --export "$DOCKER_KEY_FINGERPRINT" > "$work/docker.asc" &&
    [ "$(head -n 1 "$work/docker.asc")" = "-----BEGIN PGP PUBLIC KEY BLOCK-----" ]
}

# apt trusts every key in a Signed-By file and may read a crafted file differently
# from gpg, so the downloaded bytes are never installed: the key is re-exported by
# gpg, which writes exactly the pinned key and nothing else.
install_docker_key() {
  local work status=0
  work=$(mktemp -d)
  export_docker_key "$work" || status=1
  # gpg --import starts a gpg-agent for the throwaway keyring.
  gpgconf --homedir "$work/gnupg" --kill all >/dev/null 2>&1 || true
  if [ "$status" = 0 ]; then
    install -m 0755 -d "$KEYRING_DIR" && install -m 0644 "$work/docker.asc" "$KEYRING_DIR/docker.asc" || status=2
  fi
  rm -rf "$work"
  [ "$status" != 1 ] || die "Docker apt key has an unexpected fingerprint"
  [ "$status" != 2 ] || die "could not install Docker's apt key"
}

install_docker() {
  log "installing or upgrading Docker Engine and base packages"
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl gnupg
  install_docker_key
  write_docker_source
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `bash deploy/tests/setup_test.sh`
Expected: PASS (the last line is `all assertions passed`, and no line starts with `FAIL`)

- [ ] **Step 6: Commit**

```bash
git add deploy/setup.sh deploy/tests/setup_test.sh
git commit -m "fix(deploy): install gpg's export of Docker's key"
```

### Task 20: The service unit comes back with Docker

**Files:**
- Modify: `deploy/setup.sh` (`install_service`, new `SYSTEMD_DIR`)
- Test: `deploy/tests/setup_test.sh` (new section `(n)`, before section `(c)`)

**Interfaces:**
- Consumes: nothing
- Produces: `SYSTEMD_DIR` in `deploy/setup.sh`; in the test script, `SYSTEMD_DIR` redirected into `$T` for the whole run (`DEFAULT_SYSTEMD_DIR` keeps the script's value), set per case by `new_case`, and checked by `run`

**Items:** 68
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The unit has `Requires=docker.service`. Stopping Docker therefore stops ghr, and starting Docker again does not start it. Adding the unit to `docker.service`'s `.wants` directory (what `WantedBy=docker.service` plus `systemctl enable` creates) makes the start of Docker start ghr after it. After this task `[Install]` reads `WantedBy=multi-user.target docker.service`, `Requires=docker.service` stays, and `install_service` runs `systemctl --no-reload reenable ghr.service` so an existing install gets the new link. Plain `reenable` reloads the system manager by itself on every run, which would reload an unchanged install too, so `--no-reload` is needed for the next rule. The unit file is written, and one explicit `systemctl daemon-reload` run after the file and the links are in place, only when something changed: the unit's content differed from what is on disk, or `docker.service.wants/ghr.service` did not exist before. An unchanged install reloads nothing. The restart still happens on every run, because the binary may be new.

- [ ] **Step 1: Make the unit directory a variable**

This changes no behaviour. It must come first: without it the failing test of Step 3 would run the old `install_service` and overwrite the real `/etc/systemd/system/ghr.service` of the machine running the tests.

In `deploy/setup.sh`, replace

```bash
APT_SOURCES_DIR=/etc/apt/sources.list.d
```

with

```bash
APT_SOURCES_DIR=/etc/apt/sources.list.d
SYSTEMD_DIR=/etc/systemd/system
```

In `deploy/setup.sh`, replace

```bash
  cat > /etc/systemd/system/ghr.service <<'EOF'
```

with

```bash
  cat > "$SYSTEMD_DIR/ghr.service" <<'EOF'
```

- [ ] **Step 2: Write the failing test**

Four edits in `deploy/tests/setup_test.sh`. First, the unit directory is redirected into the temporary tree for the whole run, as the keyring directory already is, and `run` checks both.

In `deploy/tests/setup_test.sh`, replace

```bash
DEFAULT_KEYRING_DIR=$KEYRING_DIR
KEYRING_DIR="$T/keyrings"
```

with

```bash
DEFAULT_KEYRING_DIR=$KEYRING_DIR
KEYRING_DIR="$T/keyrings"
DEFAULT_SYSTEMD_DIR=$SYSTEMD_DIR
SYSTEMD_DIR="$T/systemd"
```

In `deploy/tests/setup_test.sh`, replace

```bash
# It refuses to run while a redirected directory points outside the temporary tree.
run() {
  case "$KEYRING_DIR" in
    "$T"/*) ;;
    *) echo "refusing to run: KEYRING_DIR=$KEYRING_DIR is outside $T" >&2; return 99 ;;
  esac
  (set -e; "$@")
}
```

with

```bash
# It refuses to run while a redirected directory points outside the temporary tree.
run() {
  case "$KEYRING_DIR:$SYSTEMD_DIR" in
    "$T"/*:"$T"/*) ;;
    *) echo "refusing to run: KEYRING_DIR=$KEYRING_DIR or SYSTEMD_DIR=$SYSTEMD_DIR is outside $T" >&2; return 99 ;;
  esac
  (set -e; "$@")
}
```

Second, every case gets its own unit directory.

In `deploy/tests/setup_test.sh`, replace

```bash
  KEYRING_DIR="$T/$1/keyrings"
```

with

```bash
  KEYRING_DIR="$T/$1/keyrings"
  SYSTEMD_DIR="$T/$1/systemd"
```

Third, a new section before section `(c)`. The fake `systemctl` records every call and, like `reenable`, creates the `docker.service.wants` link; `--no-reload` is part of the recorded line, so a call without it does not match.

In `deploy/tests/setup_test.sh`, replace

```bash
# (c) Dist GC.
check_host() { :; }
```

with

```bash
# (n) The unit comes back with Docker. It is rewritten only when its content
# changes, and systemd is reloaded once, after reenable, only when the unit or the
# docker.service.wants link was not already in place. The restart happens on every run.
new_case n-service
mkdir -p "$SYSTEMD_DIR"
unit="$SYSTEMD_DIR/ghr.service"
link="$SYSTEMD_DIR/docker.service.wants/ghr.service"
fake_unit_systemctl() {
  systemctl() {
    echo "$*" >> "$MARK_DIR/systemctl"
    case " $* " in
      *" reenable "*) mkdir -p "$SYSTEMD_DIR/docker.service.wants" && : > "$link" ;;
    esac
  }
}
install_unit() {
  : > "$MARK_DIR/systemctl"
  (fake_unit_systemctl; run install_service) >/dev/null 2>&1
  rc=$?
}
reloaded=$'--no-reload reenable ghr.service\ndaemon-reload\nrestart ghr.service'
unchanged=$'--no-reload reenable ghr.service\nrestart ghr.service'
check "n: the unit goes to /etc/systemd/system by default" [ "$DEFAULT_SYSTEMD_DIR" = /etc/systemd/system ]
install_unit
check "n: install_service succeeds" [ "$rc" -eq 0 ]
check "n: the unit starts again with Docker" contains "$(cat "$unit")" "WantedBy=multi-user.target docker.service"
check "n: the unit still requires Docker" contains "$(cat "$unit")" "Requires=docker.service"
check "n: a first install enables without a reload, reloads once, then starts" [ "$(cat "$MARK_DIR/systemctl")" = "$reloaded" ]
check "n: and leaves the docker.service link" [ -e "$link" ]
touch -d '2020-01-01 00:00:00' "$unit"
install_unit
check "n: an unchanged unit is not rewritten" [ "$(date -r "$unit" +%Y)" = 2020 ]
check "n: an unchanged install reloads nothing, but still starts the unit" [ "$(cat "$MARK_DIR/systemctl")" = "$unchanged" ]
rm -rf "$SYSTEMD_DIR/docker.service.wants"
install_unit
check "n: an unchanged unit with no docker.service link reloads once, after reenable" [ "$(cat "$MARK_DIR/systemctl")" = "$reloaded" ]
printf '[Install]\nWantedBy=multi-user.target\n' > "$unit"
install_unit
check "n: an older unit is replaced" contains "$(cat "$unit")" "WantedBy=multi-user.target docker.service"
check "n: and, with its link already there, reloaded once after reenable" [ "$(cat "$MARK_DIR/systemctl")" = "$reloaded" ]
printf '[Install]\nWantedBy=multi-user.target\n' > "$unit"
rm -rf "$SYSTEMD_DIR/docker.service.wants"
install_unit
check "n: an upgrade from the old unit reloads exactly once, after reenable" [ "$(cat "$MARK_DIR/systemctl")" = "$reloaded" ]

# (c) Dist GC.
check_host() { :; }
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `bash deploy/tests/setup_test.sh`
Expected: FAIL with `FAIL n: the unit starts again with Docker`, followed by `FAIL n: a first install enables without a reload, reloads once, then starts`, `FAIL n: and leaves the docker.service link`, `FAIL n: an unchanged unit is not rewritten`, `FAIL n: an unchanged install reloads nothing, but still starts the unit` and the `FAIL n: ...` lines for the unit with no link, the older unit and the upgrade, ending with `9 assertion(s) failed`

- [ ] **Step 4: Write the implementation**

In `deploy/setup.sh`, replace

```bash
install_service() {
  cat > "$SYSTEMD_DIR/ghr.service" <<'EOF'
[Unit]
```

with

```bash
install_service() {
  local unit="$SYSTEMD_DIR/ghr.service" want reload=0
  want=$(cat <<'EOF'
[Unit]
```

In `deploy/setup.sh`, replace

```bash
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable ghr.service >/dev/null
  systemctl restart ghr.service
}
```

with

```bash
WantedBy=multi-user.target docker.service
EOF
  )
  if [ "$(cat "$unit" 2>/dev/null)" != "$want" ]; then
    printf '%s\n' "$want" > "$unit"
    reload=1
  fi
  [ -e "$SYSTEMD_DIR/docker.service.wants/ghr.service" ] || reload=1
  # Requires= alone stops ghr with Docker and never starts it again; the
  # docker.service.wants link made by WantedBy= does. Without --no-reload,
  # reenable reloads systemd itself on every run.
  systemctl --no-reload reenable ghr.service >/dev/null
  [ "$reload" = 0 ] || systemctl daemon-reload
  systemctl restart ghr.service
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `bash deploy/tests/setup_test.sh`
Expected: PASS (the last line is `all assertions passed`, and no line starts with `FAIL`)

- [ ] **Step 6: Commit**

```bash
git add deploy/setup.sh deploy/tests/setup_test.sh
git commit -m "fix(deploy): restart ghr when Docker starts again"
```

### Task 21: Ask for the web password again after a failed first install

**Files:**
- Modify: `deploy/setup.sh` (`install_config`, `set_web_password`, the `FIRST_INSTALL` variable)
- Test: `deploy/tests/setup_test.sh` (section `(h)`)

**Interfaces:**
- Consumes: nothing
- Produces: the marker file `$STATE_DIR/web-password-pending`

**Items:** 69
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`FIRST_INSTALL` is a shell variable, so a first install that dies after `install_config` never asks for the web password when it is run again: the second run finds an existing `config.yaml` and treats itself as an upgrade. After this task `install_config` also creates `$STATE_DIR/web-password-pending` (next to `toolchains-pending` and `setup-pending`), and `set_web_password` runs whenever that file exists. The file is removed once a password is set, or once the owner pressed Enter to skip at a terminal, which means a read that succeeded and returned an empty line. End of input or a failed read at the prompt is not a skip: it keeps the file, and whatever the read left in the buffer (a partial line) is not sent. With no terminal and no `GHR_WEB_PASSWORD` (a `curl | bash` run) the file stays, so a later run asks. A refused password keeps it too. `FIRST_INSTALL` was read nowhere else; it is removed from the script, and the tests that set it are rewritten around the marker file. Those tests asserted the old variable, so this task corrects them.

- [ ] **Step 1: Write the failing test**

Section `(h)` is replaced as a whole, from its first comment line down to its last check.

In `deploy/tests/setup_test.sh`, replace

```bash
# (h) A first install sets the web password; an upgrade leaves it alone.
new_case h-web-password
ETC_DIR="$T/h-web-password/etc"
mkdir -p "$ETC_DIR"
first=$( (set -e; install_config >/dev/null 2>&1; echo "$FIRST_INSTALL") )
check "h: writing config.yaml marks a first install" [ "$first" = 1 ]
again=$( (set -e; install_config >/dev/null 2>&1; echo "$FIRST_INSTALL") )
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

with

```bash
# (h) The web password is asked for while web-password-pending exists: after a
# first install, and again on each re-run until it is set or skipped at a terminal.
new_case h-web-password
ETC_DIR="$T/h-web-password/etc"
pending="$STATE_DIR/web-password-pending"
mkdir -p "$ETC_DIR"
(run install_config) >/dev/null 2>&1
check "h: writing config.yaml marks the web password pending" [ -f "$pending" ]
rm -f "$pending"
(run install_config) >/dev/null 2>&1
check "h: an existing config.yaml does not" absent "$pending"

web_ghr() {
  ghr() {
    echo "$*" >> "$MARK_DIR/ghr-calls"
    cat > "$MARK_DIR/ghr-stdin"
    [ ! -f "$MARK_DIR/fail-web" ] || { echo "ghr: password must be 12 to 1024 bytes" >&2; return 1; }
    echo "web password set; every browser was logged out"
  }
}
out=$( (web_ghr; GHR_WEB_PASSWORD='a long secret'; run set_web_password) </dev/null 2>&1)
check "h: without the marker the web password is left alone" absent "$MARK_DIR/ghr-calls"
check "h: and nothing is printed" [ -z "$out" ]

touch "$pending"
out=$( (web_ghr; GHR_WEB_PASSWORD='a long secret'; run set_web_password) </dev/null 2>&1)
check "h: GHR_WEB_PASSWORD goes to ghr web set-password" [ "$(cat "$MARK_DIR/ghr-calls")" = "web set-password" ]
check "h: the password is piped without a newline" [ "$(wc -c < "$MARK_DIR/ghr-stdin")" -eq 13 ]
check "h: success is reported" contains "$out" "web password set"
check "h: no reminder after success" lacks "$out" "has no password"
check "h: a password that is set clears the marker" absent "$pending"

rm -f "$MARK_DIR/ghr-calls"
touch "$pending" "$MARK_DIR/fail-web"
out=$( (web_ghr; GHR_WEB_PASSWORD='short'; run set_web_password) </dev/null 2>&1)
rc=$?
check "h: a refused password does not fail the install" [ "$rc" -eq 0 ]
check "h: the refusal is shown" contains "$out" "password must be 12 to 1024 bytes"
check "h: the reminder follows a refusal" contains "$out" "set it now with: ghr web set-password"
check "h: a refused password keeps the marker" [ -f "$pending" ]

rm -f "$MARK_DIR/ghr-calls" "$MARK_DIR/fail-web"
out=$( (web_ghr; GHR_WEB_PASSWORD=; run set_web_password) </dev/null 2>&1)
check "h: with no terminal and no GHR_WEB_PASSWORD nothing is sent" absent "$MARK_DIR/ghr-calls"
check "h: and the reminder is printed" contains "$out" "web UI on :8080 has no password; set it now with: ghr web set-password"
check "h: and the marker stays for the next run" [ -f "$pending" ]

(web_ghr; stdin_is_tty() { return 0; }; GHR_WEB_PASSWORD=; run set_web_password) <<<'a long secret' >/dev/null 2>&1
check "h: the next run at a terminal asks and sets the typed password" [ "$(cat "$MARK_DIR/ghr-stdin")" = "a long secret" ]
check "h: and clears the marker" absent "$pending"

touch "$pending"
rm -f "$MARK_DIR/ghr-calls"
out=$( (web_ghr; stdin_is_tty() { return 0; }; GHR_WEB_PASSWORD=; run set_web_password) <<<'' 2>&1)
check "h: Enter at the prompt sends nothing" absent "$MARK_DIR/ghr-calls"
check "h: Enter at the prompt clears the marker" absent "$pending"
check "h: Enter at the prompt still prints the reminder" contains "$out" "has no password"

# End of input and a failed read are not an Enter: the marker stays.
for ending in "end of input:/dev/null" "closed input:-" "a partial line:partial"; do
  touch "$pending"
  rm -f "$MARK_DIR/ghr-calls"
  case "${ending##*:}" in
    /dev/null) out=$( (web_ghr; stdin_is_tty() { return 0; }; GHR_WEB_PASSWORD=; run set_web_password) </dev/null 2>&1) ;;
    -) out=$( (web_ghr; stdin_is_tty() { return 0; }; GHR_WEB_PASSWORD=; run set_web_password) <&- 2>&1) ;;
    partial) out=$( (web_ghr; stdin_is_tty() { return 0; }; GHR_WEB_PASSWORD=; run set_web_password) < <(printf 'a long secret') 2>&1) ;;
  esac
  check "h: ${ending%%:*} at the prompt sends nothing" absent "$MARK_DIR/ghr-calls"
  check "h: ${ending%%:*} at the prompt keeps the marker" [ -f "$pending" ]
  check "h: ${ending%%:*} at the prompt still prints the reminder" contains "$out" "has no password"
done
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `bash deploy/tests/setup_test.sh`
Expected: FAIL with `FAIL h: writing config.yaml marks the web password pending`, followed by the `FAIL h: ...` lines for the password being sent, reported and cleared, and `FAIL h: end of input at the prompt still prints the reminder` (the old code never looks at the marker), ending with `15 assertion(s) failed`

- [ ] **Step 3: Write the implementation**

In `deploy/setup.sh`, replace

```bash
READY_TIMEOUT=60
FIRST_INSTALL=0
```

with

```bash
READY_TIMEOUT=60
```

In `deploy/setup.sh`, replace

```bash
    FIRST_INSTALL=1
    # Files, not variables: a first install that dies before preinstall_toolchains
    # or the setup wizard must still find them when re-run.
    touch "$STATE_DIR/toolchains-pending" "$STATE_DIR/setup-pending"
```

with

```bash
    # Files, not variables: a first install that dies before preinstall_toolchains,
    # the setup wizard or the web password prompt must still find them when re-run.
    touch "$STATE_DIR/toolchains-pending" "$STATE_DIR/setup-pending" "$STATE_DIR/web-password-pending"
```

In `deploy/setup.sh`, replace

```bash
# A first install turns the web UI on (config.example.yaml); until a password
# is set, the first visitor to the web UI sets it.
set_web_password() {
  [ "$FIRST_INSTALL" = 1 ] || return 0
  local pw="${GHR_WEB_PASSWORD:-}" out
  if [ -z "$pw" ] && [ -t 0 ]; then
    IFS= read -rsp "Web UI password (12 to 1024 bytes; Enter to skip): " pw || true
    echo
  fi
  if [ -n "$pw" ]; then
    if out=$(printf '%s' "$pw" | ghr web set-password 2>&1); then
      log "$out"
      return 0
    fi
```

with

```bash
# A first install turns the web UI on (config.example.yaml); until a password
# is set, the first visitor to the web UI sets it. The marker stays until a
# password is set or the owner skipped the prompt, so a re-run after a failed
# first install, or one without a terminal, asks again.
set_web_password() {
  local pending="$STATE_DIR/web-password-pending" pw="${GHR_WEB_PASSWORD:-}" out
  [ -f "$pending" ] || return 0
  if [ -z "$pw" ] && stdin_is_tty; then
    # Only a line that was read and is empty is a skip. End of input or a failed
    # read keeps the marker and drops whatever it left in $pw.
    if IFS= read -rsp "Web UI password (12 to 1024 bytes; Enter to skip): " pw; then
      [ -n "$pw" ] || rm -f "$pending"
    else
      pw=
    fi
    echo
  fi
  if [ -n "$pw" ]; then
    if out=$(printf '%s' "$pw" | ghr web set-password 2>&1); then
      rm -f "$pending"
      log "$out"
      return 0
    fi
```

`stdin_is_tty` is defined further down the script, which is fine: it is looked up when `set_web_password` runs.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `bash deploy/tests/setup_test.sh`
Expected: PASS (the last line is `all assertions passed`, and no line starts with `FAIL`)

- [ ] **Step 5: Commit**

```bash
git add deploy/setup.sh deploy/tests/setup_test.sh
git commit -m "fix(deploy): ask for the web password until set"
```

### Task 22: Old runner versions, the version prefix and the closing hint

**Files:**
- Modify: `deploy/setup.sh` (`main`, `gc_dist`, `stop_ghr`, `install_runner`, `print_next`)
- Test: `deploy/tests/setup_test.sh` (fake `fetch_runner`, sections `(a)`, `(k)` and `(c)`)

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 70, 71, 72
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 2 = 3

Three small fixes in `setup.sh`. First, `gc_dist` (which removes runner versions other than `current`) ran after the restarted daemon was ready, where a runner update queued in that moment could be installing into the directory it removes. It now runs right after `install_runner`, while `stop_ghr` still holds the daemon down. That is safe because an instance directory is a full copy of the runner files (`m.Host.CopyTree(ctx, dist, dir)` in `internal/runner/lifecycle.go`), so nothing reads an old version directory while the daemon is down, and the restarted daemon reads only `current`. Second, a leading `v` in `RUNNER_VERSION` (`v2.337.0`) is removed, because the download URL and the directory name add their own. Third, `print_next` told the owner to open `http://<first host address>:<port>/setup` whatever `web.listen` said; it now prints the host part of `web.listen` (the `web:` field of `ghr setup`) when that is a specific address, and the first host address only for a wildcard (`0.0.0.0`, `::`, `[::]`) or an empty host such as `:8080`. An IPv6 host stays in brackets.

Existing tests that asserted the old order change with it: "old versions still present at restart" becomes "already removed at restart", and "no GC when the service is not ready" becomes "gc already ran before the restart".

- [ ] **Step 1: Write the failing tests**

Four edits in `deploy/tests/setup_test.sh`. First, the fake `fetch_runner` records the version it is asked for.

In `deploy/tests/setup_test.sh`, replace

```bash
  bump fetches
  [ ! -f "$MARK_DIR/stopped" ] || touch "$MARK_DIR/fetched-after-stop"
```

with

```bash
  bump fetches
  echo "$1" >> "$MARK_DIR/fetched-versions"
  [ ! -f "$MARK_DIR/stopped" ] || touch "$MARK_DIR/fetched-after-stop"
```

Second, add the version-prefix case at the end of section `(a)`.

In `deploy/tests/setup_test.sh`, replace

```bash
check "a: the fetched-again version is made unwritable for group and others" [ -z "$(find "$DIST_DIR/$FIXTURE_VERSION" -perm /022 ! -type l -print -quit)" ]
```

with

```bash
check "a: the fetched-again version is made unwritable for group and others" [ -z "$(find "$DIST_DIR/$FIXTURE_VERSION" -perm /022 ! -type l -print -quit)" ]

# A leading v on RUNNER_VERSION is dropped: the download and the dist dir use the bare number.
new_case a-version-prefix
RUNNER_VERSION=v$FIXTURE_VERSION
out=$(run install_runner 2>&1)
rc=$?
check "a: a leading v in RUNNER_VERSION is accepted" [ "$rc" -eq 0 ]
check "a: the version is installed under its bare number" [ -x "$DIST_DIR/$FIXTURE_VERSION/run.sh" ]
check "a: no dist dir carries the v" absent "$DIST_DIR/v$FIXTURE_VERSION"
check "a: the runner is fetched by its bare number" [ "$(cat "$MARK_DIR/fetched-versions")" = "$FIXTURE_VERSION" ]
check "a: current points at the bare number" [ "$(target_of_current)" = "$FIXTURE_VERSION" ]
```

Third, add the listen-address cases to section `(k)`. The fake `hostname` there answers `192.168.0.99 fd00::99`.

In `deploy/tests/setup_test.sh`, replace

```bash
check "k: a finished setup prints nothing" [ -z "$out" ]
unset -f hostname
```

with

```bash
check "k: a finished setup prints nothing" [ -z "$out" ]
# A specific listen address is printed as it is; only a wildcard or an empty host
# is replaced by the host's first address.
for listen_url in "192.168.0.5:8080 http://192.168.0.5:8080/setup" \
  "127.0.0.1:9090 http://127.0.0.1:9090/setup" \
  "localhost:8080 http://localhost:8080/setup" \
  "[fd00::5]:8080 http://[fd00::5]:8080/setup" \
  "[::1]:8080 http://[::1]:8080/setup" \
  "0.0.0.0:8080 http://192.168.0.99:8080/setup" \
  "[::]:8080 http://192.168.0.99:8080/setup" \
  ":8080 http://192.168.0.99:8080/setup" \
  ":9090 http://192.168.0.99:9090/setup"; do
  setup_marks no pending - "${listen_url%% *}"
  out=$( (setup_ghr; run print_next) 2>&1)
  check "k: web ${listen_url%% *} gives ${listen_url##* }" contains "$out" "finish setup in the browser: ${listen_url##* }"
done
hostname() { echo "fd00::99 192.168.0.99"; }
setup_marks no pending - 0.0.0.0:8080
out=$( (setup_ghr; run print_next) 2>&1)
check "k: a wildcard with an IPv6 first address brackets it" contains "$out" "http://[fd00::99]:8080/setup"
hostname() { :; }
out=$( (setup_ghr; run print_next) 2>&1)
check "k: a wildcard with no host address says this host" contains "$out" "http://<this host>:8080/setup"
unset -f hostname
```

Fourth, the dist GC section. The restart stub now checks that the old versions are already gone when the service restarts, and the existing assertions follow.

In `deploy/tests/setup_test.sh`, replace

```bash
  bump restarts
  [ -d "$DIST_DIR/2.300.0" ] && [ -d "$DIST_DIR/2.336.0" ] && touch "$MARK_DIR/old-present-at-restart"
```

with

```bash
  bump restarts
  [ ! -e "$DIST_DIR/2.300.0" ] && [ ! -e "$DIST_DIR/2.336.0" ] && touch "$MARK_DIR/old-gone-at-restart"
```

In `deploy/tests/setup_test.sh`, replace

```bash
check "c: old versions still present at restart" [ -f "$MARK_DIR/old-present-at-restart" ]
check "c: current already switched at restart" [ -f "$MARK_DIR/current-new-at-restart" ]
check "c: old version 2.300.0 removed after ready" absent "$DIST_DIR/2.300.0"
check "c: old version 2.336.0 removed after ready" absent "$DIST_DIR/2.336.0"
```

with

```bash
check "c: old versions already removed at restart" [ -f "$MARK_DIR/old-gone-at-restart" ]
check "c: current already switched at restart" [ -f "$MARK_DIR/current-new-at-restart" ]
check "c: old version 2.300.0 removed" absent "$DIST_DIR/2.300.0"
check "c: old version 2.336.0 removed" absent "$DIST_DIR/2.336.0"
```

In `deploy/tests/setup_test.sh`, replace

```bash
check "c: ghr started again after the failure" [ "$(cat "$MARK_DIR/started" 2>/dev/null)" = ghr.service ]
```

with

```bash
check "c: ghr started again after the failure" [ "$(cat "$MARK_DIR/started" 2>/dev/null)" = ghr.service ]
check "c: no old version is removed when the download fails" [ -d "$DIST_DIR/2.336.0" ]
```

In `deploy/tests/setup_test.sh`, replace

```bash
check "c: no GC when the service is not ready" [ -d "$DIST_DIR/2.336.0" ]
```

with

```bash
check "c: gc already ran before the restart" absent "$DIST_DIR/2.336.0"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `bash deploy/tests/setup_test.sh`
Expected: FAIL with `FAIL a: the version is installed under its bare number`, followed by `FAIL a: no dist dir carries the v`, `FAIL a: the runner is fetched by its bare number`, `FAIL a: current points at the bare number`, five `FAIL k: web ...` lines for specific listen addresses, `FAIL k: a wildcard with an IPv6 first address brackets it`, `FAIL c: old versions already removed at restart` and `FAIL c: gc already ran before the restart`, ending with `12 assertion(s) failed`

- [ ] **Step 3: Write the implementation**

In `deploy/setup.sh`, replace

```bash
install_runner() {
  local version="$RUNNER_VERSION"
```

with

```bash
install_runner() {
  local version="${RUNNER_VERSION#v}"
```

In `deploy/setup.sh`, replace

```bash
  if [ -n "$web" ] && [ "$web" != - ]; then
    host=$(hostname -I 2>/dev/null | awk '{print $1}' || true)
    log "finish setup in the browser: http://${host:-<this host>}:${web##*:}/setup"
```

with

```bash
  if [ -n "$web" ] && [ "$web" != - ]; then
    host=${web%:*}
    case "$host" in
      "" | 0.0.0.0 | :: | "[::]")
        host=$(hostname -I 2>/dev/null | awk '{print $1}' || true)
        [[ "$host" != *:* ]] || host="[$host]"
        ;;
    esac
    log "finish setup in the browser: http://${host:-<this host>}:${web##*:}/setup"
```

In `deploy/setup.sh`, replace

```bash
# Must run only after the restarted daemon is ready: the old daemon may have been
# copying a non-current dist dir into a new instance until it stopped, and the new
# one only reads `current`.
gc_dist() {
```

with

```bash
# Runs while the daemon is stopped, after install_runner has switched `current`.
# An instance directory is a full copy of the runner files, so nothing reads an
# old version directory in the meantime, and no update can install into dist
# while it is removing from it.
gc_dist() {
```

The comment on `stop_ghr` gave the old position of `gc_dist` as the reason for dropping a queued update; that reason is gone.

In `deploy/setup.sh`, replace

```bash
# A queued update is dropped while the daemon is down: once it is back up it
# could start installing into dist while gc_dist is still removing from it.
```

with

```bash
# A queued update is dropped while the daemon is down, so the restarted daemon
# does not install a runner version over the one this script just installed.
```

In `deploy/setup.sh`, replace

```bash
  install_runner
  install_ghr
  install_config
  install_service
  trap - EXIT
  wait_ready
  configure_github
  set_web_password
  gc_dist
  preinstall_toolchains
```

with

```bash
  install_runner
  gc_dist
  install_ghr
  install_config
  install_service
  trap - EXIT
  wait_ready
  configure_github
  set_web_password
  preinstall_toolchains
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `bash deploy/tests/setup_test.sh`
Expected: PASS (the last line is `all assertions passed`, and no line starts with `FAIL`)

- [ ] **Step 5: Commit**

```bash
git add deploy/setup.sh deploy/tests/setup_test.sh
git commit -m "fix(deploy): clean dist early, name the listen host"
```

### Task 23: CI permissions and pins, and the README

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `README.md` (the Requirements list)

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 73, 74
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

This task has no failing test: neither a workflow file nor a README line can be exercised by a test in this repository, so the check is a set of `grep` commands in Step 4. Today the workflow runs with the repository's default token rights, its two checkouts leave the token in `.git/config`, and every action is named by a movable tag (`@v4`). After this task the workflow has a top-level `permissions: contents: read` (the `release` job keeps its own `contents: write`), both checkouts set `persist-credentials: false`, and every action is pinned to a full 40-character commit SHA with its tag in a trailing comment. The `release` job needs no git credential after the checkout: its `version` step only reads tags the checkout already fetched (`git tag --points-at`, `git tag --list`), and it publishes with `gh release create` using `GH_TOKEN`. The README's Requirements list says ghr serves private repositories, and a public one only with `allow_public` set for it, and keeps the warning about fork pull requests.

The pins below were resolved and verified on 2026-10-12. Each is the newest release of the major version the workflow uses today, and each SHA is the commit that the release's tag names in the `actions` organisation's own repository: `actions/checkout` v4.4.0 is `11d5960a326750d5838078e36cf38b85af677262`, `actions/setup-go` v5.6.0 is `40f1582b2485089dde7abd97c1529aa768e1baff`, and `actions/setup-node` v4.4.0 is `49933ea5288caeca8642d1e84afbd3f7d6820020`. Type them exactly as given in the steps below; change no pin and resolve nothing.

- [ ] **Step 1: Limit the token and pin the actions in the test job**

In `.github/workflows/ci.yml`, replace

```yaml
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26.x'
          check-latest: true
      - name: gofmt
```

with

```yaml
  pull_request:

permissions:
  contents: read

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4.4.0
        with:
          persist-credentials: false
      - uses: actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff # v5.6.0
        with:
          go-version: '1.26.x'
          check-latest: true
      - name: gofmt
```

In `.github/workflows/ci.yml`, replace

```yaml
      - uses: actions/setup-node@v4
        with:
          node-version-file: web/.nvmrc
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: npm ci
        working-directory: web
      - run: npm run lint
```

with

```yaml
      - uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020 # v4.4.0
        with:
          node-version-file: web/.nvmrc
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: npm ci
        working-directory: web
      - run: npm run lint
```

- [ ] **Step 2: Pin the actions and drop the stored credential in the release job**

In `.github/workflows/ci.yml`, replace

```yaml
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
```

with

```yaml
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4.4.0
        with:
          fetch-depth: 0
          persist-credentials: false
      - uses: actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff # v5.6.0
        with:
```

In `.github/workflows/ci.yml`, replace

```yaml
      - uses: actions/setup-node@v4
        if: env.VERSION != ''
```

with

```yaml
      - uses: actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020 # v4.4.0
        if: env.VERSION != ''
```

- [ ] **Step 3: Update the README requirement**

In `README.md`, replace

```markdown
- Every configured repo is **private** and does **not** enable "Run workflows from fork
  pull requests".
```

with

```markdown
- ghr serves private repositories. It serves a public one only when `allow_public` is set
  for it. Any repo it serves must **not** enable "Run workflows from fork pull requests",
  because a fork's pull request could then run its code on this host.
```

- [ ] **Step 4: Check the result**

Run: `grep -c 'uses:' .github/workflows/ci.yml`
Expected: `6`

Run: `grep -c 'uses: .*@[0-9a-f]\{40\} # v[0-9][0-9.]*$' .github/workflows/ci.yml`
Expected: `6`, the same number: every `uses:` line ends in `@`, 40 hex characters, a space and a `# v...` tag comment. A pin that is not 40 hex characters lowers this count.

Run: `grep -c 'persist-credentials: false' .github/workflows/ci.yml`
Expected: `2`

Run: `grep -n -A1 'permissions:' .github/workflows/ci.yml`
Expected: `8:permissions:` with `9-  contents: read` under it, then `51:    permissions:` with `52-      contents: write` (the release job)

Run: `grep -n 'allow_public' README.md`
Expected: one line, `18:- ghr serves private repositories. It serves a public one only when `allow_public` is set`

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(workflow): pin actions, limit token rights"
git add README.md
git commit -m "docs(readme): say when a public repo is served"
```

### Task 24: A failed save is a 500

**Files:**
- Modify: `internal/daemon/store.go` (`SaveError`, `Store.Update`)
- Modify: `internal/daemon/backend.go` (`Backend.update`)
- Test: `internal/daemon/backend_test.go` (`TestSaveFailureIsA500`)

**Interfaces:**
- Consumes: nothing
- Produces: `daemon.SaveError` (`type SaveError struct{ Err error }`, with `Error` and `Unwrap`), returned by `Store.Update` when `config.Save` fails; the answer `500 could not save config.yaml`

**Items:** 49
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

`Backend.update` is the one place every config change of the API goes through (PATCH config, add, watch, unwatch and remove a repository, pause all). It turns every error that is not an `*api.Error` into a 400 carrying the error's text. A full disk or a read-only directory therefore reads as a bad request, and the browser is shown the operating system's error with the path of a temporary file.

After this task `Store.Update` wraps a failure of `config.Save` in an exported error type, and `Backend.update` answers that type with 500 `could not save config.yaml` and writes the wrapped error to the daemon's log. A change the config refuses stays a 400 with its text. `internal/api` has no constructor for a 500, and that package is not this task's, so the `*api.Error` literal is built here.

- [ ] **Step 1: Write the failing test**

In `internal/daemon/backend_test.go`, add `log` to the imports: replace

```go
	"fmt"
	"net/http"
	"net/http/httptest"
```

with

```go
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
```

In the same file, add this test directly above the comment `// With several bad durations, the first in the order poll_interval,` (the comment of `TestPatchConfigReportsFirstBadDuration`):

```go
// A config.yaml that cannot be written is the daemon's failure, not the
// client's: the answer is a 500 that leaves the OS error out, and the log gets
// it. The valid change that was not saved does not go live.
func TestSaveFailureIsA500(t *testing.T) {
	b, _, _ := newBackend(t)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	// A rename onto a directory fails for root too.
	if err := os.Remove(b.Store.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b.Store.ConfigPath, 0o755); err != nil {
		t.Fatal(err)
	}
	three := 3
	err := b.PatchConfig(model.ConfigPatch{GlobalMax: &three})
	if apiStatus(err) != 500 || err.Error() != "could not save config.yaml" {
		t.Fatalf("status %d, error %v", apiStatus(err), err)
	}
	if b.Store.Config().GlobalMax != 2 {
		t.Fatal("a change that was not saved went live")
	}
	if got := logs.String(); !strings.Contains(got, "save config: ") || !strings.Contains(got, "config.yaml") {
		t.Fatalf("log %q", got)
	}
	var se *SaveError
	if _, err := b.Store.Update(func(c *config.Config) error { c.GlobalMax = 3; return nil }); !errors.As(err, &se) || se.Err == nil {
		t.Fatalf("Store.Update: %v", err)
	}
}
```

The test makes the save fail by putting a directory where `config.yaml` was: a rename onto a directory fails for every user, root included. `newBackend`, `apiStatus` and the config of the test store (`global_max: 2`) are the package's existing helpers.

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/ -run 'TestSaveFailureIsA500'`
Expected: FAIL with `undefined: SaveError` (the package's tests do not build yet)

- [ ] **Step 3: Write the implementation**

In `internal/daemon/store.go`, replace

```go
// Store owns the live config and token. Writes are validated and saved atomically.
type Store struct {
```

with

```go
// SaveError is a failure to write config.yaml, as opposed to a change the
// config refuses.
type SaveError struct{ Err error }

func (e *SaveError) Error() string { return "save config: " + e.Err.Error() }
func (e *SaveError) Unwrap() error { return e.Err }

// Store owns the live config and token. Writes are validated and saved atomically.
type Store struct {
```

In the same file, replace

```go
// Update applies fn to a copy of the config, validates, saves, then swaps it in.
func (s *Store) Update(fn func(c *config.Config) error) ([]string, error) {
```

with

```go
// Update applies fn to a copy of the config, validates, saves, then swaps it in.
// A failed save is returned as a *SaveError.
func (s *Store) Update(fn func(c *config.Config) error) ([]string, error) {
```

and, in `Update`, replace

```go
	if err := config.Save(s.ConfigPath, c); err != nil {
		return nil, err
	}
	s.cfg.Store(c)
	return warnings, nil
```

with

```go
	if err := config.Save(s.ConfigPath, c); err != nil {
		return nil, &SaveError{Err: err}
	}
	s.cfg.Store(c)
	return warnings, nil
```

`Update` has already validated the copy before it calls `config.Save`, so the validation `Save` repeats cannot fail here: whatever `Save` returns is a write failure. Leave `Store.Configure` as it is; it saves too, but its caller is the first-run setup, which this task does not change.

In `internal/daemon/backend.go`, in `update`, replace

```go
		if errors.As(err, &ae) {
			return err
		}
		return api.BadRequest(err.Error())
	}
	for _, w := range warnings {
```

with

```go
		if errors.As(err, &ae) {
			return err
		}
		var se *SaveError
		if errors.As(err, &se) {
			log.Printf("save config: %v", se.Err)
			return &api.Error{Status: http.StatusInternalServerError, Msg: "could not save config.yaml"}
		}
		return api.BadRequest(err.Error())
	}
	for _, w := range warnings {
```

`log` and `net/http` are already imported in `backend.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/ -run 'TestSaveFailureIsA500|TestPatchConfig|TestStore'`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/daemon`)

`TestPatchConfigRejectsInvalidSettings` is in that run: it shows that a change the config refuses is still a 400 with its text.

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/daemon` (must print nothing), then:

```bash
git add internal/daemon/store.go internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "fix(daemon): answer a failed config save with 500"
```

### Task 25: A reload repeats no warning

**Files:**
- Modify: `internal/daemon/store.go` (`Store`, `OpenStore`, `Reload`, `Update`, `Configure`)
- Modify: `internal/daemon/backend.go` (`Backend.Reload`)
- Modify: `internal/daemon/run.go` (the reload case of the not-configured loop in `Run`)
- Test: `internal/daemon/store_test.go`, `internal/daemon/backend_test.go` (new tests, and the fake manager's `cleared` flag), `internal/daemon/run_test.go`

**Interfaces:**
- Consumes: `daemon.SaveError` in `Store.Update` (Task 24), only as lines this task quotes
- Produces: `func (s *Store) Reload() (warnings, added []string, tokenChanged bool, err error)`, which has one more return value than before (`added`); every caller is in `internal/daemon` and is changed here

**Items:** 208
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

A load of `config.yaml` can produce warnings. One of them is the runner cap: `global_max: 500` is lowered to 99 in memory with the warning `global_max 500 is above 99; using 99`, and the file keeps saying 500 until something saves the config. A reload writes every warning of its load as an event, so that warning comes back on every reload.

After this task the store remembers the warnings of its last load (`OpenStore` and `Reload` both load), and `Reload` itself says which warnings of its load are new: `added` holds those that were neither in the load before nor in the config saved since. A save replaces the remembered list with the saved config's own warnings, because the file then holds the lowered value: if the owner edits it back to 500, that is new again. The lowered-value warnings come from `config.Parse`, not from `Config.Validate`, which is why a save drops them from the list. A reload writes an event only for what is in `added`; `Backend.Reload` still returns every warning of the load to its caller.

The comparison is made inside `Reload`, under the same hold of the store's lock as the load, and the save paths replace the list under that lock too. Two reloads at the same moment therefore report a new warning once, and a reload can never be compared with a list that a save has already replaced. That is why `Reload` returns the difference, and why there is no method that reads the remembered list.

There are two reload paths. `Backend.Reload` serves a configured daemon. Before ghr has an owner and a token, `Run` handles a reload itself. Both write only what is in `added`.

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/store_test.go`, add `slices` to the imports: replace

```go
	"path/filepath"
	"strconv"
```

with

```go
	"path/filepath"
	"slices"
	"strconv"
```

`Reload` gains a return value, so the ten existing calls in this file each take one more blank. Replace each of the 6 occurrences of `if _, _, err := s.Reload()` with `if _, _, _, err := s.Reload()`.

Replace each of the 2 occurrences of `if _, changed, err := s.Reload()` with `if _, _, changed, err := s.Reload()`.

Replace each of the 2 occurrences of `if _, changed, err := first.Reload()` with `if _, _, changed, err := first.Reload()`.

Then add these three tests directly above the comment `// unconfiguredStore opens a store whose config names owner (possibly empty)`:

```go
// Reload says which warnings of its load are new: neither in the load before
// nor in the config saved since. A rejected reload changes nothing. A save
// writes the lowered value to the file, so the same bad edit made again is
// new again.
func TestStoreReloadSaysWhichWarningsAreNew(t *testing.T) {
	const clamp = "global_max 500 is above 99; using 99"
	bad := []byte(strings.Replace(cfgYAML, "global_max: 2", "global_max: 500", 1))
	s := newStore(t)
	reload := func(s *Store, what string, wantAdded ...string) {
		t.Helper()
		os.WriteFile(s.ConfigPath, bad, 0o600)
		ws, added, _, err := s.Reload()
		if err != nil || !slices.Equal(ws, []string{clamp}) || !slices.Equal(added, wantAdded) {
			t.Fatalf("%s: warnings %v, new %v, err %v", what, ws, added, err)
		}
	}
	reload(s, "the bad edit", clamp)
	reload(s, "the same file again")
	os.WriteFile(s.ConfigPath, []byte("mode: fast\n"), 0o600)
	if _, _, _, err := s.Reload(); err == nil {
		t.Fatal("an invalid config was loaded")
	}
	reload(s, "after a rejected reload")
	if _, err := s.Update(func(c *config.Config) error { c.Mode = config.ModeAll; return nil }); err != nil {
		t.Fatal(err)
	}
	reload(s, "the bad edit made again after a save", clamp)

	first := unconfiguredStore(t, "darkraise", nil)
	reload(first, "before the first-run save", clamp)
	if err := first.Configure("DarkRaise", "tok1"); err != nil {
		t.Fatal(err)
	}
	bad = []byte(strings.Replace(string(bad), "owner: darkraise", "owner: DarkRaise", 1))
	reload(first, "the bad edit made again after the first-run save", clamp)
}

// Two reloads at once report a new warning once: the comparison with the
// remembered list is made under the same hold of the lock as the load. Both
// reloads are started while an update that saves nothing holds that lock.
func TestStoreReloadsAtOnceReportAWarningOnce(t *testing.T) {
	s := newStore(t)
	os.WriteFile(s.ConfigPath, []byte(strings.Replace(cfgYAML, "global_max: 2", "global_max: 500", 1)), 0o600)
	var wg sync.WaitGroup
	var added [2][]string
	var errs [2]error
	s.Update(func(*config.Config) error {
		for i := range 2 {
			wg.Go(func() { _, added[i], _, errs[i] = s.Reload() })
		}
		return errors.New("nothing to save")
	})
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || len(added[0])+len(added[1]) != 1 {
		t.Fatalf("new warnings %v and %v, errors %v and %v; want one new warning in all", added[0], added[1], errs[0], errs[1])
	}
}

// A reload that starts while a save is in progress is compared with what the
// save left. The save reports the warning it introduced, so the reload that
// follows it has nothing new.
func TestStoreReloadBehindASaveReportsNothingNew(t *testing.T) {
	s := newStore(t)
	const warning = "darkmem: cleanup_name_prefixes can remove a concurrent job's containers when max is not 1"
	var loaded, added []string
	var rerr error
	done := make(chan struct{})
	saved, err := s.Update(func(c *config.Config) error {
		go func() {
			defer close(done)
			loaded, added, _, rerr = s.Reload()
		}()
		two := 2
		c.Repo("darkmem").Max, c.Repo("darkmem").CleanupNamePrefixes = &two, []string{"dm-"}
		return nil
	})
	<-done
	if err != nil || !slices.Equal(saved, []string{warning}) {
		t.Fatalf("the save: warnings %v, err %v", saved, err)
	}
	if rerr != nil || !slices.Equal(loaded, []string{warning}) || len(added) != 0 {
		t.Fatalf("the reload behind it: warnings %v, new %v, err %v", loaded, added, rerr)
	}
}
```

The second and third tests control the order of events through `Store.Update`, which holds the store's lock while it runs the function it is given. A reload started from inside that function has to wait for the lock, so it is certain to run after the update. In the second test the update returns an error and saves nothing; in the third it saves a config with a warning of its own, which `Validate` gives for a repository that has `cleanup_name_prefixes` and a `max` other than 1.

In `internal/daemon/backend_test.go`, add these two tests directly above `func TestPatchCannotChangeWeb(t *testing.T) {`:

```go
// A reload writes a warning as an event when the load before did not have it,
// and still returns every warning of the load. A save writes the lowered value
// to config.yaml, so the same bad edit made again is reported again.
func TestReloadRepeatsNoWarning(t *testing.T) {
	b, _, _ := newBackend(t)
	const clamp = "global_max 500 is above 99; using 99"
	bad := []byte(strings.Replace(cfgYAML, "global_max: 2", "global_max: 500", 1))
	reload := func(wantEvents int, what string) {
		t.Helper()
		if err := os.WriteFile(b.Store.ConfigPath, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		ws, err := b.Reload()
		if err != nil || !slices.Contains(ws, clamp) {
			t.Fatalf("%s: warnings %v, err %v", what, ws, err)
		}
		n := 0
		for _, e := range b.Events.After(0) {
			if e.Level == "warn" && e.Msg == "config: "+clamp {
				n++
			}
		}
		if n != wantEvents {
			t.Fatalf("%s: %d warning events, want %d", what, n, wantEvents)
		}
	}
	reload(1, "the bad edit")
	reload(1, "the same file again")
	all := "all"
	if err := b.PatchConfig(model.ConfigPatch{Mode: &all}); err != nil {
		t.Fatal(err)
	}
	reload(2, "the bad edit made again after a save")
}

// Two reloads at once write a new warning as one event. Both are started
// while an update that saves nothing holds the store's lock.
func TestReloadsAtOnceWriteAWarningOnce(t *testing.T) {
	b, _, _ := newBackend(t)
	const clamp = "global_max 500 is above 99; using 99"
	if err := os.WriteFile(b.Store.ConfigPath, []byte(strings.Replace(cfgYAML, "global_max: 2", "global_max: 500", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	b.Store.Update(func(*config.Config) error {
		for range 2 {
			wg.Go(func() { b.Reload() })
		}
		return errors.New("nothing to save")
	})
	wg.Wait()
	n := 0
	for _, e := range b.Events.After(0) {
		if e.Level == "warn" && e.Msg == "config: "+clamp {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d warning events, want 1", n)
	}
}
```

`TestReloadsAtOnceWriteAWarningOnce` reloads from two goroutines, and every reload calls `ClearDegraded` on the daemon's fake manager, which sets the fake's `cleared` flag. Two unguarded writes of a `bool` are a data race, so the flag becomes an `atomic.Bool`, in the same file. `sync/atomic` is already imported there. In the `fakeManager` struct, replace

```go
	cleared   bool
```

with

```go
	cleared   atomic.Bool // set by ClearDegraded, which two reloads at once both call
```

Replace

```go
func (f *fakeManager) ClearDegraded() { f.cleared = true }
```

with

```go
func (f *fakeManager) ClearDegraded() { f.cleared.Store(true) }
```

In `TestReloadAppliesWakesAndRejects`, replace

```go
	m.cleared = false
```

with

```go
	m.cleared.Store(false)
```

The flag is read in nine places, in `TestPauseAllStepsKillToken`, `TestReloadAppliesWakesAndRejects` and `TestSetTokenJudgesEveryRepository`. Each read becomes a `Load()`; the four patterns below cover all nine. Replace each of the 4 occurrences of `!m.cleared` with `!m.cleared.Load()`.

Replace each of the 2 occurrences of `, m.cleared)` with `, m.cleared.Load())`.

Replace the one occurrence of `|| m.cleared ||` with `|| m.cleared.Load() ||`.

Replace each of the 2 occurrences of `, m.cleared, woke)` with `, m.cleared.Load(), woke)`.

After these, no `m.cleared` is left in the file that is not followed by `.Load()` or `.Store(`.

In `internal/daemon/run_test.go`, add this test directly above `func TestRunConfiguresOnceWhenReloadAndSetupRace(t *testing.T) {`:

```go
// Before ghr is configured a reload is handled by Run itself. It too writes a
// warning only when the load before did not have it; the start-up load did.
func TestRunUnconfiguredReloadRepeatsNoWarning(t *testing.T) {
	o, reload := unconfiguredOptions(t, "")
	cfg, err := os.ReadFile(o.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(o.ConfigPath, []byte(strings.Replace(string(cfg), "global_max: 2", "global_max: 500", 1)), 0o600)
	startRun(t, o)
	waitFor(t, "the setup phase", func() bool { _, ok := setupStateOver(o.Socket); return ok })
	reload <- os.Interrupt
	waitFor(t, "the reload", func() bool { return strings.Contains(eventMsgs(t, o.Socket), "config and token reloaded") })
	if n := strings.Count(eventMsgs(t, o.Socket), "global_max 500 is above 99; using 99"); n != 1 {
		t.Fatalf("the warning was written %d times:\n%s", n, eventMsgs(t, o.Socket))
	}
}
```

`cfgYAML`, `newStore`, `unconfiguredStore`, `newBackend`, `unconfiguredOptions`, `startRun`, `waitFor`, `setupStateOver` and `eventMsgs` are the package's existing test helpers.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/ -run 'TestStoreReload|TestReload|TestRunUnconfiguredReloadRepeatsNoWarning'`
Expected: FAIL with `assignment mismatch: 4 variables but s.Reload returns 3 values` (the package's tests do not build yet)

- [ ] **Step 3: Let the store say which warnings are new**

In `internal/daemon/store.go`, add `slices` to the imports: replace

```go
	"path/filepath"
	"strings"
```

with

```go
	"path/filepath"
	"slices"
	"strings"
```

In the `Store` struct, replace

```go
	mu    sync.Mutex
	cfg   atomic.Pointer[config.Config]
	token atomic.Pointer[string]
}
```

with

```go
	mu    sync.Mutex
	cfg   atomic.Pointer[config.Config]
	token atomic.Pointer[string]
	// warnings are those of the last load, or of the config last saved;
	// guarded by mu.
	warnings []string
}
```

In `OpenStore`, replace

```go
	warnings, _, err := s.Reload()
	return s, warnings, err
```

with

```go
	warnings, _, _, err := s.Reload()
	return s, warnings, err
```

Replace the whole of `Reload`, with its comment,

```go
// Reload re-reads config and token; on any error the previous values stay active.
// The owner cannot change while the daemon runs: the GitHub client and every
// live runner belong to the owner it started with. A first owner may be set.
// tokenChanged reports whether the token now in use differs from the one
// before the call, so a caller can keep what it fetched with an unchanged one.
func (s *Store) Reload() (warnings []string, tokenChanged bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, warnings, err := config.Load(s.ConfigPath)
	if err != nil {
		return nil, false, err
	}
	if cur := s.cfg.Load(); cur != nil && cur.Owner != "" && cur.Owner != cfg.Owner {
		return nil, false, fmt.Errorf("owner changed from %s to %s: restart ghr to switch owners", cur.Owner, cfg.Owner)
	}
	tok, err := s.readToken()
	if err != nil {
		return nil, false, err
	}
	s.cfg.Store(cfg)
	if tok != "" && tok != s.Token() {
		s.token.Store(&tok)
		tokenChanged = true
	}
	return warnings, tokenChanged, nil
}
```

with

```go
// Reload re-reads config and token; on any error the previous values stay active.
// The owner cannot change while the daemon runs: the GitHub client and every
// live runner belong to the owner it started with. A first owner may be set.
// tokenChanged reports whether the token now in use differs from the one
// before the call, so a caller can keep what it fetched with an unchanged one.
// added are the warnings of this load that the store did not have yet: those
// that were neither in the load before nor in the config saved since. They
// are worked out under the same hold of the lock as the load, so of two
// reloads at once only one reports a warning as new.
func (s *Store) Reload() (warnings, added []string, tokenChanged bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, warnings, err := config.Load(s.ConfigPath)
	if err != nil {
		return nil, nil, false, err
	}
	if cur := s.cfg.Load(); cur != nil && cur.Owner != "" && cur.Owner != cfg.Owner {
		return nil, nil, false, fmt.Errorf("owner changed from %s to %s: restart ghr to switch owners", cur.Owner, cfg.Owner)
	}
	tok, err := s.readToken()
	if err != nil {
		return nil, nil, false, err
	}
	s.cfg.Store(cfg)
	for _, w := range warnings {
		if !slices.Contains(s.warnings, w) {
			added = append(added, w)
		}
	}
	// A copy: the caller may append to what it gets.
	s.warnings = slices.Clone(warnings)
	if tok != "" && tok != s.Token() {
		s.token.Store(&tok)
		tokenChanged = true
	}
	return warnings, added, tokenChanged, nil
}
```

In `Update`, replace

```go
		return nil, &SaveError{Err: err}
	}
	s.cfg.Store(c)
	return warnings, nil
```

with

```go
		return nil, &SaveError{Err: err}
	}
	s.cfg.Store(c)
	s.warnings = slices.Clone(warnings)
	return warnings, nil
```

In `Configure`, which saves the config when it sets the first owner, replace

```go
			return err
		}
		s.cfg.Store(c)
	}
	s.token.Store(&token)
```

with

```go
			return err
		}
		s.cfg.Store(c)
		s.warnings, _ = c.Validate()
	}
	s.token.Store(&token)
```

A reload that is rejected returns before `s.cfg.Store(cfg)`, so it leaves the list alone, like the config. `Update` and `Configure` already hold `s.mu` for their whole length.

- [ ] **Step 4: Write only the new warnings as events**

In `internal/daemon/backend.go`, replace the start of `Reload`

```go
func (b *Backend) Reload() ([]string, error) {
	warnings, tokenChanged, err := b.Store.Reload()
	if err != nil {
		b.Events.Add("error", "", "reload rejected, keeping previous config: %v", err)
		return nil, api.BadRequest(err.Error())
	}
	if w := b.Store.Config().Web; w.Listen != b.WebApplied.Listen || !slices.Equal(w.Hosts, b.WebApplied.Hosts) {
		warnings = append(warnings, "web settings changed; restart ghr to apply")
	}
	for _, w := range warnings {
		b.Events.Add("warn", "", "config: %s", w)
	}
```

with

```go
func (b *Backend) Reload() ([]string, error) {
	warnings, added, tokenChanged, err := b.Store.Reload()
	if err != nil {
		b.Events.Add("error", "", "reload rejected, keeping previous config: %v", err)
		return nil, api.BadRequest(err.Error())
	}
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

In `internal/daemon/run.go`, in `Run`, replace

```go
			case <-reload:
				ws, _, err := store.Reload()
				if err != nil {
					ev.Add("error", "", "reload rejected, keeping previous config: %v", err)
					continue
				}
				for _, w := range ws {
					ev.Add("warn", "", "config: %s", w)
				}
```

with

```go
			case <-reload:
				_, added, _, err := store.Reload()
				if err != nil {
					ev.Add("error", "", "reload rejected, keeping previous config: %v", err)
					continue
				}
				for _, w := range added {
					ev.Add("warn", "", "config: %s", w)
				}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go vet ./... && PATH=/usr/local/go/bin:$PATH go test -race -count=1 -timeout 300s ./internal/daemon/`
Expected: no output from `go vet`, then PASS (`ok  	github.com/darkraise/ghr/internal/daemon`)

The whole package runs here because `TestReloadWarnsOnWebChange` and `TestReloadAppliesWakesAndRejects` cover the parts of `Reload` this task must not change, and under the race detector because two of the new tests reload from two goroutines.

Then run: `PATH=/usr/local/go/bin:$PATH go test -race -count=30 -run 'TestReloadsAtOnceWriteAWarningOnce' ./internal/daemon/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/daemon`)

- [ ] **Step 6: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/daemon` (must print nothing), then:

```bash
git add internal/daemon/store.go internal/daemon/store_test.go internal/daemon/backend.go internal/daemon/backend_test.go internal/daemon/run.go internal/daemon/run_test.go
git commit -m "fix(daemon): write a load warning once per load"
```

### Task 26: Connection limits on the web listener

**Files:**
- Create: `internal/daemon/limitlistener.go`
- Create: `internal/daemon/limitlistener_test.go`
- Modify: `internal/daemon/run.go` (where `Run` opens the web listener)
- Test: `internal/daemon/run_test.go` (`TestRunBoundsAWebClientsConnections`)

**Interfaces:**
- Consumes: nothing
- Produces: `limitConns(l net.Listener, total, perClient int) *limitListener`, `clientKey(remote net.Addr) string`, the constants `maxWebConns = 256` and `maxWebConnsPerClient = 64`, all unexported in package `daemon`

**Items:** 53
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 0 - risk 3 = 5

The web listener accepts without limit, and every open connection holds a file descriptor of the process that also starts runners. This task wraps the listener in a small type of its own, with no new dependency. The Unix socket is not wrapped.

The wrapper enforces two bounds:

- **256 connections in all.** With 256 open it does not call the inner `Accept`, so further clients wait in the kernel's accept queue.
- **64 connections per client.** Without this, one host on the LAN could hold all 256 and shut everyone else out of the web UI. A client is its remote address without the port: an IPv4 address as it is, an IPv4 address mapped into IPv6 (`::ffff:a.b.c.d`) as that IPv4 address, and an IPv6 address as its /64 with the zone removed, because one host can use every address of its /64. An address that does not parse counts under its own text. The key is computed here with `net/netip`; `internal/webui` has a similar function for the login lockout, and this task must neither import nor change that package.

Rules the code below implements, each pinned by a test:

- The order in `Accept` is: take a global slot, accept, compute the client's key. A connection of a client that already holds 64 is closed at once, its global slot is given back, and `Accept` goes on to the next connection. It does not return an error for it: `http.Server` backs off or stops on an error from `Accept`.
- A failed inner `Accept` gives its slot back and its error is returned as it is.
- A connection gives its global slot and its client's place back on its first `Close`, however often and from however many goroutines `Close` is called. The HTTP server does close a connection more than once.
- A client with nothing open has no entry in the table, so the table cannot grow without bound.
- `Close` on the wrapper closes the inner listener and then wakes an `Accept` that is waiting for a global slot; that `Accept` returns the inner listener's own closed error. `http.Server.Shutdown`, which `Run`'s teardown calls, closes the listener this way, so the serve loop does not hang at the cap.

The unit tests use `testing/synctest` (standard library; it is in the Go this repository uses, 1.26), which lets a test wait until every goroutine is blocked without sleeping. They drive a scripted inner listener that returns from `Accept` only what the test sends it.

- [ ] **Step 1: Write the failing unit tests**

Create `internal/daemon/limitlistener_test.go` with this content:

```go
package daemon

import (
	"errors"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

type textAddr string

func (a textAddr) Network() string { return "tcp" }
func (a textAddr) String() string  { return string(a) }

// scriptedConn is a connection with a chosen remote address that counts its
// Close calls. Nothing under test reads or writes it.
type scriptedConn struct {
	net.Conn
	remote textAddr
	closed atomic.Int32
}

func (c *scriptedConn) RemoteAddr() net.Addr { return c.remote }
func (c *scriptedConn) Close() error         { c.closed.Add(1); return nil }

var errScriptClosed = errors.New("scripted listener closed")

// scriptedListener returns from Accept only what the test sends it, so the
// test knows when the listener under test is inside Accept and when it is not.
type scriptedListener struct {
	arrive chan *scriptedConn
	fail   chan error
	closed chan struct{}
	once   sync.Once
	calls  atomic.Int32 // Accept calls, including the one still waiting
}

func newScriptedListener() *scriptedListener {
	return &scriptedListener{arrive: make(chan *scriptedConn), fail: make(chan error), closed: make(chan struct{})}
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	l.calls.Add(1)
	select {
	case c := <-l.arrive:
		return c, nil
	case err := <-l.fail:
		return nil, err
	case <-l.closed:
		return nil, errScriptClosed
	}
}

func (l *scriptedListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *scriptedListener) Addr() net.Addr { return textAddr("192.0.2.1:8080") }

// dial makes a connection from remote arrive at the listener and returns it.
func (l *scriptedListener) dial(remote string) *scriptedConn {
	c := &scriptedConn{remote: textAddr(remote)}
	l.arrive <- c
	return c
}

// acceptLoop calls Accept the way a server does. It sends every connection on
// the first channel and, when Accept fails, the error on the second.
func acceptLoop(ln net.Listener) (<-chan net.Conn, <-chan error) {
	conns, failed := make(chan net.Conn), make(chan error, 1)
	go func() {
		defer close(conns)
		for {
			c, err := ln.Accept()
			if err != nil {
				failed <- err
				return
			}
			conns <- c
		}
	}()
	return conns, failed
}

// endLoop closes ln and takes what its accept loop still holds. A bubble must
// not end with that loop blocked, also when an assertion has failed.
func endLoop(ln net.Listener, conns <-chan net.Conn) {
	ln.Close()
	for range conns {
	}
}

// closeTwiceAtOnce closes c from two goroutines and waits for both.
func closeTwiceAtOnce(c net.Conn) {
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { c.Close() })
	}
	wg.Wait()
}

func TestLimitListenerHoldsTheGlobalCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newScriptedListener()
		ln := limitConns(inner, 2, 2)
		conns, failed := acceptLoop(ln)
		defer endLoop(ln, conns)

		inner.dial("192.0.2.10:5000")
		first := <-conns
		inner.dial("192.0.2.11:5000")
		<-conns
		synctest.Wait()
		if n := inner.calls.Load(); n != 2 {
			t.Fatalf("with 2 connections open the inner Accept was called %d times", n)
		}

		// A connection closed twice, from two goroutines, frees one slot.
		closeTwiceAtOnce(first)
		inner.dial("192.0.2.12:5000")
		<-conns
		synctest.Wait()
		if n := inner.calls.Load(); n != 3 {
			t.Fatalf("after one connection closed the inner Accept was called %d times, want 3", n)
		}

		// Close ends the Accept that is waiting for a slot, with the inner
		// listener's own error.
		if err := ln.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-failed; !errors.Is(err, errScriptClosed) {
			t.Fatalf("Accept after Close: %v", err)
		}
	})
}

// A failed Accept of the inner listener is returned as it is and keeps no slot.
func TestLimitListenerFreesTheSlotOfAFailedAccept(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newScriptedListener()
		ln := limitConns(inner, 1, 1)
		boom := errors.New("accept: too many open files")
		_, failed := acceptLoop(ln)
		inner.fail <- boom
		if err := <-failed; err != boom {
			t.Fatalf("Accept returned %v", err)
		}
		conns, _ := acceptLoop(ln)
		defer endLoop(ln, conns)
		inner.dial("192.0.2.10:5000")
		<-conns
	})
}

func TestLimitListenerBoundsEachClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newScriptedListener()
		ln := limitConns(inner, 3, 2)
		conns, _ := acceptLoop(ln)
		defer endLoop(ln, conns)
		clients := func() int {
			ln.mu.Lock()
			defer ln.mu.Unlock()
			return len(ln.clients)
		}

		inner.dial("192.0.2.10:5000")
		first := <-conns
		inner.dial("192.0.2.10:5001")
		second := <-conns

		// A third connection of the same client is closed at once and Accept
		// goes on. Only one global slot is left, and the next client gets it,
		// so the refused connection gave its slot back.
		refused := inner.dial("192.0.2.10:5002")
		inner.dial("192.0.2.11:5000")
		other := <-conns
		if n := refused.closed.Load(); n != 1 {
			t.Fatalf("the connection over the client's bound was closed %d times", n)
		}
		if n := clients(); n != 2 {
			t.Fatalf("%d clients counted, want 2", n)
		}

		// A connection closed twice, from two goroutines, gives the client
		// one place back: its next connection is taken, the one after is not.
		closeTwiceAtOnce(first)
		inner.dial("192.0.2.10:5003")
		third := <-conns
		other.Close()
		if n := clients(); n != 1 {
			t.Fatalf("%d clients counted after the second one left, want 1", n)
		}
		again := inner.dial("192.0.2.10:5004")
		synctest.Wait()
		if n := again.closed.Load(); n != 1 {
			t.Fatalf("after one close the client holds 2 again, but its next connection was closed %d times", n)
		}

		// A client with nothing open is forgotten.
		second.Close()
		third.Close()
		if n := clients(); n != 0 {
			t.Fatalf("%d clients still counted with nothing open", n)
		}
	})
}

// A client at its bound that keeps connecting takes nothing from the others:
// every connection it is refused gives its global slot back, so a second
// client still gets all of its own.
func TestLimitListenerRefusalsCostOtherClientsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newScriptedListener()
		ln := limitConns(inner, 4, 2)
		conns, _ := acceptLoop(ln)
		defer endLoop(ln, conns)

		inner.dial("192.0.2.10:5000")
		<-conns
		inner.dial("192.0.2.10:5001")
		<-conns
		hammer := func() {
			t.Helper()
			for port := range 50 {
				c := inner.dial(net.JoinHostPort("192.0.2.10", strconv.Itoa(6000+port)))
				synctest.Wait()
				if n := c.closed.Load(); n != 1 {
					t.Fatalf("refused connection %d was closed %d times", port, n)
				}
			}
		}
		hammer()
		inner.dial("192.0.2.11:5000")
		<-conns
		hammer()
		inner.dial("192.0.2.11:5001")
		<-conns
		// Both clients are at their bound and every global slot is taken, so
		// the inner Accept is not called again: 4 kept, 100 refused.
		synctest.Wait()
		if n := inner.calls.Load(); n != 104 {
			t.Fatalf("the inner Accept was called %d times, want 104", n)
		}
	})
}

func TestClientKey(t *testing.T) {
	for remote, want := range map[string]string{
		"192.0.2.10:5000":                         "192.0.2.10",
		"[::ffff:192.0.2.10]:5000":                "192.0.2.10",
		"[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:5000": "2001:db8:1:2::/64",
		"[2001:db8:1:2::1]:6000":                  "2001:db8:1:2::/64",
		"[2001:db8:1:3::1]:6000":                  "2001:db8:1:3::/64",
		"[fe80::1%eth0]:5000":                     "fe80::/64",
		"@":                                       "@",
		"192.0.2.10":                              "192.0.2.10",
	} {
		if got := clientKey(textAddr(remote)); got != want {
			t.Errorf("clientKey(%q) = %q, want %q", remote, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the unit tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/ -run 'TestLimitListener|TestClientKey'`
Expected: FAIL with `undefined: limitConns` (the package's tests do not build yet)

- [ ] **Step 3: Write the listener**

Create `internal/daemon/limitlistener.go` with this content:

```go
package daemon

import (
	"net"
	"net/netip"
	"sync"
)

const (
	// maxWebConns bounds the connections open on the web listener at once.
	// Every one holds a file descriptor of the process that also starts runners.
	maxWebConns = 256
	// maxWebConnsPerClient bounds one client's share of them, so that one
	// host cannot hold every slot and shut the others out.
	maxWebConnsPerClient = 64
)

// limitListener keeps at most cap(slots) accepted connections open, and at
// most perClient of them from one client. With every slot taken it does not
// call the inner Accept, so further clients wait in the kernel's accept queue.
type limitListener struct {
	net.Listener
	slots     chan struct{}
	perClient int
	closed    chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex
	clients map[string]int // open connections by clientKey; no entry is 0
}

func limitConns(l net.Listener, total, perClient int) *limitListener {
	return &limitListener{
		Listener: l, slots: make(chan struct{}, total), perClient: perClient,
		closed: make(chan struct{}), clients: map[string]int{},
	}
}

// Accept returns the next connection of a client that is under its bound. A
// connection over the bound is closed here and is not an error: the HTTP
// server backs off or stops on an error from Accept.
func (l *limitListener) Accept() (net.Conn, error) {
	for {
		select {
		case l.slots <- struct{}{}:
		case <-l.closed:
			// Close closed the inner listener first, so this names its error.
			c, err := l.Listener.Accept()
			if err == nil {
				c.Close()
				err = net.ErrClosed
			}
			return nil, err
		}
		c, err := l.Listener.Accept()
		if err != nil {
			<-l.slots
			return nil, err
		}
		key := clientKey(c.RemoteAddr())
		if !l.admit(key) {
			c.Close()
			<-l.slots
			continue
		}
		return &limitConn{Conn: c, release: func() {
			l.leave(key)
			<-l.slots
		}}, nil
	}
}

func (l *limitListener) admit(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.clients[key] >= l.perClient {
		return false
	}
	l.clients[key]++
	return true
}

func (l *limitListener) leave(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.clients[key]--; l.clients[key] == 0 {
		delete(l.clients, key)
	}
}

// Close also ends an Accept that is waiting for a slot, which closing the
// inner listener alone would leave waiting until a connection closed.
func (l *limitListener) Close() error {
	err := l.Listener.Close()
	l.closeOnce.Do(func() { close(l.closed) })
	return err
}

// limitConn gives its slots back on the first Close. The HTTP server can
// close a connection more than once, and from more than one goroutine.
type limitConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

// clientKey names the host behind a remote address: an IPv4 address as it
// is, also when it arrives mapped into IPv6, and an IPv6 address as its /64,
// because one host can use every address of its /64. An address that does
// not parse counts under its own text.
func clientKey(remote net.Addr) string {
	text := remote.String()
	ap, err := netip.ParseAddrPort(text)
	if err != nil {
		return text
	}
	ip := ap.Addr().Unmap().WithZone("")
	if ip.Is4() {
		return ip.String()
	}
	prefix, _ := ip.Prefix(64)
	return prefix.String()
}
```

- [ ] **Step 4: Run the unit tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/ -run 'TestLimitListener|TestClientKey'`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/daemon`)

If a test fails with `panic: deadlock: all goroutines in bubble are blocked`, a slot was not given back or a waiting `Accept` was not woken: that is how `synctest` reports a goroutine that waits for ever.

- [ ] **Step 5: Write the failing test for the wiring**

In `internal/daemon/run_test.go`, add this test directly above `func TestRunWithoutTheWebListener(t *testing.T) {`:

```go
// Run serves the web UI through the limited listener: a client that holds
// maxWebConnsPerClient connections has its next one closed at once, and is
// served again once it closes one.
func TestRunBoundsAWebClientsConnections(t *testing.T) {
	addr := freeAddr(t)
	startRun(t, testOptions(t, cfgYAML+"web:\n  listen: "+addr+"\n"))
	var held []net.Conn
	t.Cleanup(func() {
		for _, c := range held {
			c.Close()
		}
	})
	waitFor(t, "the web listener", func() bool {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return false
		}
		held = append(held, c)
		return true
	})
	for len(held) < maxWebConnsPerClient+1 {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	// Connections are accepted in the order they arrived, so the last one
	// dialled is the one over the bound. The deadline only ends a failing
	// run, and it is shorter than the 10 s after which the server closes a
	// connection that sent no request.
	over := held[maxWebConnsPerClient]
	over.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := over.Read(make([]byte, 1)); err == nil || errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("connection %d of one client was not closed: %v", maxWebConnsPerClient+1, err)
	}
	held[0].Close()
	waitFor(t, "an answer once a connection closed", func() bool {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return false
		}
		defer c.Close()
		fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: %s\r\n\r\n", addr)
		c.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, err = c.Read(make([]byte, 1))
		return err == nil
	})
}
```

The test opens real TCP connections from 127.0.0.1, so they all count as one client. `freeAddr`, `testOptions`, `startRun` and `waitFor` are the package's existing helpers.

- [ ] **Step 6: Run the wiring test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/ -run 'TestRunBoundsAWebClientsConnections'`
Expected: FAIL after about 5 seconds with `connection 65 of one client was not closed: read tcp 127.0.0.1:<port>->127.0.0.1:<port>: i/o timeout`

- [ ] **Step 7: Wrap the web listener**

In `internal/daemon/run.go`, in `Run`, replace

```go
	if webCfg.Listen != "" {
		if webLn, err = net.Listen("tcp", webCfg.Listen); err != nil {
			return fmt.Errorf("web listener: %w", err)
		}
	}
```

with

```go
	if webCfg.Listen != "" {
		tcp, err := net.Listen("tcp", webCfg.Listen)
		if err != nil {
			return fmt.Errorf("web listener: %w", err)
		}
		webLn = limitConns(tcp, maxWebConns, maxWebConnsPerClient)
	}
```

Nothing else in `Run` changes: `webLn` stays a `net.Listener`, and `ln`, the Unix socket's listener, is not wrapped.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -race -count=1 -timeout 300s ./internal/daemon/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/daemon`)

The whole package runs under the race detector here: the listener is concurrency code, and every test that starts `Run` with a web listener now goes through it.

- [ ] **Step 9: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/daemon` (must print nothing), then:

```bash
git add internal/daemon/limitlistener.go internal/daemon/limitlistener_test.go internal/daemon/run.go internal/daemon/run_test.go
git commit -m "feat(daemon): limit open web connections"
```

### Task 27: A panic in a token check

**Files:**
- Modify: `internal/daemon/backend.go` (`checkTokenOn`, new `checkTokenSafely`)
- Test: `internal/daemon/backend_test.go` (`TestSetTokenJudgesEveryRepository`)

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names; the error text `could not check the token`

**Items:** 210
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`SetToken` checks a new token against every runner repository. `checkTokenOn` runs those checks on goroutines of its own, four at a time. A panic on such a goroutine is not recovered by `net/http`, which recovers only on the goroutine that serves the request, so it ends the daemon.

After this task each check recovers from a panic. The panic is written to the log and becomes that repository's error `could not check the token`, which `SetToken` then treats like any other failed check: with another repository passing, the token is saved and the failing repository gets a warning event.

- [ ] **Step 1: Write the failing test**

`TestSetTokenJudgesEveryRepository` in `internal/daemon/backend_test.go` is a table of check results per repository. Add a row whose check panics.

In the test, replace

```go
	const denied = hint + "github: 403 Resource not accessible by personal access token"
	for _, tc := range []struct {
```

with

```go
	const denied = hint + "github: 403 Resource not accessible by personal access token"
	panics := errors.New("the check panics instead of returning")
	for _, tc := range []struct {
```

replace

```go
		{"one of two fails", map[string]error{"darkmem": forbidden}, 0, "", []string{"new token: darkmem: " + denied}},
	} {
```

with

```go
		{"one of two fails", map[string]error{"darkmem": forbidden}, 0, "", []string{"new token: darkmem: " + denied}},
		{"one of two panics", map[string]error{"darkmem": panics}, 0, "", []string{"new token: darkmem: could not check the token"}},
	} {
```

and replace

```go
				checked = append(checked, repo)
				mu.Unlock()
				return tc.results[repo]
```

with

```go
				checked = append(checked, repo)
				mu.Unlock()
				if tc.results[repo] == panics {
					panic("boom")
				}
				return tc.results[repo]
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/ -run 'TestSetTokenJudgesEveryRepository'`
Expected: FAIL with `panic: boom` (the panic ends the test binary, as it would end the daemon)

- [ ] **Step 3: Write the implementation**

In `internal/daemon/backend.go`, in `checkTokenOn`, replace

```go
			errs[i] = b.CheckToken(ctx, token, repo)
		}()
	}
	wg.Wait()
	return errs
}
```

with

```go
			errs[i] = b.checkTokenSafely(ctx, token, repo)
		}()
	}
	wg.Wait()
	return errs
}

// checkTokenSafely turns a panic in a check into that repository's error. The
// check runs on its own goroutine, where a panic would end the daemon.
func (b *Backend) checkTokenSafely(ctx context.Context, token, repo string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("token check %s: panic: %v", repo, r)
			err = errors.New("could not check the token")
		}
	}()
	return b.CheckToken(ctx, token, repo)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/ -run 'TestSetToken|TestCheckTokenOn'`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/daemon`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/daemon` (must print nothing), then:

```bash
git add internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "fix(daemon): recover a panic in a token check"
```

### Task 28: A log that is not there yet

**Files:**
- Modify: `internal/daemon/joblogs.go` (constants, `logMiss`, `logJob`, `completionTime`, `downloadLog`, `finishedJob`, `JobLog`)
- Modify: `internal/daemon/annotations.go` (`RunAnnotations`)
- Test: `internal/daemon/joblogs_test.go`, `internal/daemon/annotations_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 213
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 0 - risk 2 = 4

The job page downloads a finished job's log from GitHub. When GitHub answers 404 for the log of a completed job, the daemon remembers "this job produced no log" for `logRetryTTL`, 10 minutes, and the page says so. Whether GitHub can answer 404 for a moment right after a job completes, before the log is stored, is not known. If it can, the page is wrong for 10 minutes.

After this task that answer is remembered for 15 seconds when the job completed less than 2 minutes before the answer, and for 10 minutes otherwise. The page says the same thing meanwhile. A job whose completion time GitHub left out counts as old.

Two routes start log downloads and share what they remember: the job log route (`JobLog`, through `finishedJob`) and the run page's annotations (`RunAnnotations`). Both know the job's completion time, so both pass it on. How long an answer is kept is decided once, when it is stored, and stored with it.

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/joblogs_test.go`, add this test directly above `func TestJobLogParsedCacheIsBoundedByLogSize(t *testing.T) {`:

```go
// GitHub may not have stored a log yet when its job has only just completed,
// so "no log" is kept 15 seconds when the job completed less than two minutes
// before the answer, and logRetryTTL otherwise.
func TestJobLogAsksAgainSoonAfterTheJobCompleted(t *testing.T) {
	for _, tc := range []struct {
		name  string
		since time.Duration // from the job's completion to the first request
		kept  time.Duration
	}{
		{"a minute ago", time.Minute, 15 * time.Second},
		{"two minutes ago", 2 * time.Minute, logRetryTTL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh := newBackend(t)
			j := logJobOf(gh, 1002, "Backend tests", "x")
			gh.logErr[1002] = ghNotFound()
			first := j.CompletedAt.Add(tc.since)
			clock := frozenClock(b, first)
			for _, step := range []struct {
				after     time.Duration
				downloads int
			}{{0, 1}, {tc.kept - time.Second, 1}, {tc.kept, 2}} {
				clock.Store(first.Add(step.after).UnixNano())
				l, err := b.JobLog(context.Background(), "darkmem", 1002)
				if err != nil || !l.NoLog {
					t.Fatalf("after %v: log %+v err %v", step.after, l, err)
				}
				if n := gh.count("JobLog"); n != step.downloads {
					t.Fatalf("after %v: %d downloads, want %d", step.after, n, step.downloads)
				}
			}
		})
	}
}
```

In `internal/daemon/annotations_test.go`, add this test directly above `func TestRunAnnotationsPassesRunErrors(t *testing.T) {`:

```go
// The annotations route starts log downloads too, and gives them the job's
// completion time: a log not found a minute after its job completed is asked
// for again 15 seconds later.
func TestRunAnnotationsAskAgainSoonAfterTheJobCompleted(t *testing.T) {
	b, _, gh := newBackend(t)
	j := logJobOf(gh, 2001, "a", "x")
	annotatedRun(gh, j)
	gh.logErr[2001] = ghNotFound()
	first := j.CompletedAt.Add(time.Minute)
	clock := frozenClock(b, first)
	for _, step := range []struct {
		after     time.Duration
		downloads int
	}{{0, 1}, {14 * time.Second, 1}, {15 * time.Second, 2}} {
		clock.Store(first.Add(step.after).UnixNano())
		if _, err := b.RunAnnotations(context.Background(), "darkmem", 102, 0); err != nil {
			t.Fatal(err)
		}
		if n := gh.count("JobLog"); n != step.downloads {
			t.Fatalf("after %v: %d downloads, want %d", step.after, n, step.downloads)
		}
	}
}
```

`logJobOf` gives the fake GitHub a job that completed at `actionsT0` plus one minute and returns it, `ghNotFound()` is a 404, `annotatedRun` puts jobs into run 102, and `frozenClock` sets the backend's clock and returns the value a test moves. All are the package's existing helpers.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/ -run 'TestJobLogAsksAgainSoonAfterTheJobCompleted|TestRunAnnotationsAskAgainSoonAfterTheJobCompleted'`
Expected: FAIL with `after 15s: 1 downloads, want 2` in both tests

- [ ] **Step 3: Write the implementation**

In `internal/daemon/joblogs.go`, replace

```go
	annotationEntries = 500
	logRetryTTL       = 10 * time.Minute
)
```

with

```go
	annotationEntries = 500
	logRetryTTL       = 10 * time.Minute
	// GitHub may not have stored a log yet when its job has only just
	// completed. "No log" for a job that completed less than logSettle ago
	// is therefore kept logSettleTTL, not logRetryTTL.
	logSettle    = 2 * time.Minute
	logSettleTTL = 15 * time.Second
)
```

Replace

```go
// logMiss is an expired, too large, absent or failed outcome, kept
// logRetryTTL.
type logMiss struct {
	at  time.Time
	res logResult
}
```

with

```go
// logMiss is an expired, too large, absent or failed outcome, kept until its
// time.
type logMiss struct {
	until time.Time
	res   logResult
}
```

Replace

```go
type logJob struct {
	repo  string
	id    int64
	name  string
	steps []model.Step
}
```

with

```go
type logJob struct {
	repo  string
	id    int64
	name  string
	steps []model.Step
	// completedAt is zero when GitHub gave no completion time.
	completedAt time.Time
}

// completionTime is a job's completed_at, which GitHub can leave out.
func completionTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
```

In `downloadLog`, replace

```go
		default:
			c.misses[j.id] = logMiss{at: b.now(), res: res}
		}
```

with

```go
		default:
			now, ttl := b.now(), logRetryTTL
			if res.outcome == logNone && now.Sub(j.completedAt) < logSettle {
				ttl = logSettleTTL
			}
			c.misses[j.id] = logMiss{until: now.Add(ttl), res: res}
		}
```

At the end of `finishedJob`, replace

```go
	return logJob{repo: name, id: jobID, name: job.Name, steps: modelSteps(job.Steps)}, nil
```

with

```go
	return logJob{repo: name, id: jobID, name: job.Name, steps: modelSteps(job.Steps), completedAt: completionTime(job.CompletedAt)}, nil
```

In `JobLog`, replace

```go
	if m, ok := c.misses[jobID]; ok && m.res.outcome != logFailed && b.now().Sub(m.at) < logRetryTTL {
```

with

```go
	if m, ok := c.misses[jobID]; ok && m.res.outcome != logFailed && b.now().Before(m.until) {
```

In `internal/daemon/annotations.go`, in `RunAnnotations`, replace

```go
		if m, ok := c.misses[j.ID]; ok && b.now().Sub(m.at) < logRetryTTL {
```

with

```go
		if m, ok := c.misses[j.ID]; ok && b.now().Before(m.until) {
```

and replace

```go
		calls[j.ID] = b.startLogLocked(logJob{repo: d.Repo, id: j.ID, name: j.Name, steps: j.Steps})
```

with

```go
		calls[j.ID] = b.startLogLocked(logJob{repo: d.Repo, id: j.ID, name: j.Name, steps: j.Steps, completedAt: completionTime(j.CompletedAt)})
```

`job.CompletedAt` (a `github.Job`) and `j.CompletedAt` (a `model.RunJob`) are both `*time.Time`.

- [ ] **Step 4: Run the package and fix the test of the old rule**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/`
Expected: FAIL in `TestRunAnnotationsCountsLogOutcomes` with `step 1: 5 downloads, want 4`

That test freezes the clock at `actionsT0`, a minute before its jobs complete, so its job without a log now counts as only just completed and is asked for again after 15 seconds. The test is about the 10-minute rule, so its jobs must be old. Move its clock an hour on.

In `internal/daemon/annotations_test.go`, in `TestRunAnnotationsCountsLogOutcomes`, replace

```go
	b, _, gh := newBackend(t)
	clock := frozenClock(b, actionsT0)
	annotatedRun(gh, logJobOf(gh, 2001, "a", "x"), logJobOf(gh, 2002, "b", "x"), logJobOf(gh, 2003, "c", "x"), logJobOf(gh, 2004, "d", "x"))
```

with

```go
	b, _, gh := newBackend(t)
	// The jobs completed an hour ago, so a log that is not found is kept
	// logRetryTTL like the other outcomes.
	start := actionsT0.Add(time.Hour)
	clock := frozenClock(b, start)
	annotatedRun(gh, logJobOf(gh, 2001, "a", "x"), logJobOf(gh, 2002, "b", "x"), logJobOf(gh, 2003, "c", "x"), logJobOf(gh, 2004, "d", "x"))
```

and, in the loop of the same test, replace

```go
		clock.Store(actionsT0.Add(step.at).UnixNano())
```

with

```go
		clock.Store(start.Add(step.at).UnixNano())
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/daemon`)

- [ ] **Step 6: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/daemon` (must print nothing), then:

```bash
git add internal/daemon/joblogs.go internal/daemon/joblogs_test.go internal/daemon/annotations.go internal/daemon/annotations_test.go
git commit -m "fix(daemon): ask again soon for a fresh job's log"
```

### Task 29: Root reads of job-writable files

**Files:**
- Modify: `internal/runner/manager.go` (`maxJobFile`, `readJobJSON`)
- Modify: `internal/runner/private.go` (`readMeta`), `internal/runner/adopt.go` (`Adopt`), `internal/runner/lifecycle.go` (`readJobFiles`, `finish`)
- Create: `internal/runner/jobfile_unix_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `readJobJSON(path string, v any) error` and `maxJobFile` in package `runner`

**Items:** 203
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 0 - risk 3 = 5

Every runner instance has a directory the job's user owns, so a job can put anything there. The daemon runs as root and reads two files in it with `readJSON`, which is `os.ReadFile`: `job.json`, which the job-started hook writes, and `ghr.json`, the metadata copy an older ghr left there. Opening a FIFO for reading blocks until someone opens it for writing. A job that replaces `job.json` with a FIFO therefore blocks the tick, which is the loop that manages every runner.

After this task those two files are read through a helper that makes the checks `readFrom` in `cleanup.go` already makes for runner logs: it does not follow a symbolic link, opens without blocking, accepts only a regular file, and reads at most 1 MiB. Like `readFrom`, it opens the directory as an `os.Root` and reads through it. The check for a regular file is made on the name and again on the open file, because the job can swap the name between the two; whatever it swaps in, the root lets the open reach only a file inside the instance directory. The files root alone can write (the private records, the pending records, the update state) keep `readJSON`.

Do not use `syscall.O_NOFOLLOW` for this: it does not exist on Windows, and the tree must keep building there.

There are four reads to change: the legacy `ghr.json` in `readMeta`, and `job.json` in `Adopt`, `readJobFiles` and `finish`.

- [ ] **Step 1: Write the failing tests**

Create `internal/runner/jobfile_unix_test.go` with this content:

```go
//go:build unix

package runner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// mustReturn fails the test when f has not returned after 5 s: opening a FIFO
// nobody writes to blocks for ever.
func mustReturn(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s blocked", what)
	}
}

func mkfifo(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A job can write its instance directory, and root reads job.json and the
// legacy ghr.json there: only a regular file of at most 1 MiB, and never
// through a link.
func TestReadJobJSONReadsOnlyASmallRegularFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const open, end = `{"job":"`, `"}`
	write("real.json", open+"e2e"+end)
	write("full.json", open+strings.Repeat("x", maxJobFile-len(open)-len(end))+end)
	write("big.json", open+strings.Repeat("x", maxJobFile-len(open)-len(end)+1)+end)
	if err := os.Symlink(filepath.Join(dir, "real.json"), filepath.Join(dir, "link.json")); err != nil {
		t.Fatal(err)
	}
	mkfifo(t, filepath.Join(dir, "fifo.json"))
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"real.json", true},
		{"full.json", true},
		{"big.json", false},
		{"link.json", false},
		{"fifo.json", false},
		{"none.json", false},
	} {
		var rec JobRecord
		var err error
		mustReturn(t, "reading "+tc.name, func() { err = readJobJSON(filepath.Join(dir, tc.name), &rec) })
		if (err == nil) != tc.ok || (tc.ok && rec.Job == "") {
			t.Errorf("%s: job of %d bytes, err %v", tc.name, len(rec.Job), err)
		}
		if tc.name == "none.json" && !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a missing file: %v", err)
		}
	}
}

// A FIFO named job.json blocks neither the tick that looks for a started job
// nor the cleanup that reads the job's record, and is not a job record.
func TestAFIFONamedJobFileBlocksNeitherTickNorCleanup(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.m.spawn(ctx, h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	h.gh.setRunner(101, "online", false)
	mkfifo(t, filepath.Join(h.m.instanceDir("aaaaaa"), JobFile))
	mustReturn(t, "the tick", func() { h.m.Tick(ctx) })
	if h.state("aaaaaa") != "idle" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
	h.sd.active["ghr-runner-aaaaaa"] = false
	mustReturn(t, "the cleanup", func() {
		h.m.Tick(ctx)
		h.m.Wait()
	})
	if h.state("aaaaaa") != "gone" {
		t.Fatalf("state after the unit stopped: %s\n%s", h.state("aaaaaa"), h.eventText())
	}
}

// Adoption reads both files of every instance directory. A FIFO in place of
// job.json leaves a runner adopted without a job; one in place of the legacy
// ghr.json is an instance with no readable metadata.
func TestAdoptIsNotBlockedByFIFOsInInstanceDirs(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkcloud", 1, false)
	mkfifo(t, filepath.Join(h.m.instanceDir("aaaaaa"), JobFile))
	makeInstance(t, h, "bbbbbb", "darkmem", 2, false)
	mkfifo(t, filepath.Join(h.m.instanceDir("bbbbbb"), MetaFile))
	h.sd.active["ghr-runner-aaaaaa"] = true
	h.sd.active["ghr-runner-bbbbbb"] = true
	mustReturn(t, "adoption", func() {
		if err := h.m.Adopt(context.Background()); err != nil {
			t.Error(err)
		}
		h.m.Wait()
	})
	if h.state("aaaaaa") != "starting" {
		t.Fatalf("aaaaaa %s", h.state("aaaaaa"))
	}
	if _, err := os.Stat(h.m.instanceDir("bbbbbb")); !os.IsNotExist(err) || !strings.Contains(h.eventText(), "instance bbbbbb had no readable ghr.json; stopped and removed") {
		t.Fatalf("bbbbbb: stat %v\n%s", err, h.eventText())
	}
}
```

The file is for Unix only because it makes FIFOs. `newHarness`, `makeInstance`, `h.state`, `h.eventText` and the fake GitHub's `setRunner` are the package's existing test helpers; the first runner the harness spawns has ID `aaaaaa` and GitHub runner ID 101. The 5-second wait in `mustReturn` only ends a failing run: a passing one returns at once.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/runner/ -run 'TestReadJobJSON|TestAFIFONamed|TestAdoptIsNotBlocked'`
Expected: FAIL with `undefined: readJobJSON` (the package's tests do not build yet)

- [ ] **Step 3: Write the helper**

In `internal/runner/manager.go`, add `io`, `io/fs` and `syscall` to the imports: replace

```go
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
```

with

```go
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
```

In the same file, replace

```go
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
```

with

```go
func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// maxJobFile bounds a file read from an instance directory.
const maxJobFile = 1 << 20

// readJobJSON reads a JSON file from an instance directory. The job can
// write that directory and root reads the file, so it gets the checks
// readFrom makes for logs: a link is not followed, only a regular file is
// read, and at most maxJobFile of it. A FIFO would block the caller, which is
// the tick, and a device would feed it for ever. The check is repeated on the
// open file, because the job can swap the name between the two; whatever it
// swaps in, the root lets the open reach only a file inside that directory.
func readJobJSON(path string, v any) error {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(path)
	fi, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fs.ErrInvalid
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return fs.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(f, maxJobFile+1))
	if err != nil {
		return err
	}
	if len(data) > maxJobFile {
		return fmt.Errorf("%s is larger than %d bytes", path, maxJobFile)
	}
	return json.Unmarshal(data, v)
}
```

The directory that holds the instance directories belongs to root, so a job cannot replace its own instance directory with something else; only what is inside it is the job's.

- [ ] **Step 4: Use it for the four reads**

In `internal/runner/private.go`, in `readMeta`, replace

```go
		err = readJSON(filepath.Join(m.instanceDir(id), MetaFile), &meta)
```

with

```go
		err = readJobJSON(filepath.Join(m.instanceDir(id), MetaFile), &meta)
```

Leave the line above it, `err := readJSON(m.privateMeta(id), &meta)`, as it is: the private record is root's.

In `internal/runner/adopt.go`, replace

```go
		if readJSON(filepath.Join(dir, JobFile), &rec) == nil {
```

with

```go
		if readJobJSON(filepath.Join(dir, JobFile), &rec) == nil {
```

In `internal/runner/lifecycle.go`, in `readJobFiles`, replace

```go
		if err := readJSON(filepath.Join(m.instanceDir(i.ID), JobFile), &rec); err != nil {
```

with

```go
		if err := readJobJSON(filepath.Join(m.instanceDir(i.ID), JobFile), &rec); err != nil {
```

and, in `finish`, replace

```go
	recOK := readJSON(filepath.Join(dir, JobFile), &rec) == nil
```

with

```go
	recOK := readJobJSON(filepath.Join(dir, JobFile), &rec) == nil
```

Every other `readJSON` call in the package stays: `m.pendingPath(id)`, `path` and the files under `m.Paths.Pending` in `lifecycle.go`, and `m.Paths.UpdateState` in `update.go`, are all outside the instance directories.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -race -count=1 -timeout 300s ./internal/runner/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/runner`)

- [ ] **Step 6: Check that the tree still builds for Windows**

Run: `GOOS=windows PATH=/usr/local/go/bin:$PATH go vet ./...`
Expected: no output

- [ ] **Step 7: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/runner` (must print nothing), then:

```bash
git add internal/runner/manager.go internal/runner/private.go internal/runner/adopt.go internal/runner/lifecycle.go internal/runner/jobfile_unix_test.go
git commit -m "fix(runner): read job-writable files safely as root"
```

### Task 30: A listed run says whether its repository is private

**Files:**
- Modify: `internal/github/client.go` (`Run`, new `RunRepository`, new `Run.RepoPublic`)
- Test: `internal/github/client_test.go` (`TestRunSaysWhetherItsRepositoryIsPublic`)

**Interfaces:**
- Consumes: nothing
- Produces: in package `github`, `type RunRepository struct { Private *bool }` (JSON `private`), the field `Repository RunRepository` (JSON `repository`) on `Run`, and `func (r Run) RepoPublic() bool`

**Items:** 214
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Every workflow run GitHub returns carries the repository it belongs to, and that object says whether the repository is private (checked with `gh api` on 2026-10-12: each run of the runs listing has `repository.private`). The client's `Run` type does not decode it. The next task needs it: the runner manager lists a repository's runs on every poll, and a run that says "public" is how it will notice that a repository was made public, hours before its own check is due.

This task only decodes the flag; nothing reads it yet. The one rule that matters here: a run decoded without the flag must not read as public. Many test fakes and fixtures across the tree build a `github.Run` with no repository at all, and none of them may suddenly look like a run of a public repository. So the flag is a pointer, and "public" means the flag is there and false.

- [ ] **Step 1: Write the failing test**

In `internal/github/client_test.go`, add this test directly above the comment `// setLimit changes one of the client's bounds for the length of a test.`:

```go
// A run says its repository is public only when it carries the private flag
// and the flag is false. A run without the flag says nothing.
func TestRunSaysWhetherItsRepositoryIsPublic(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"workflow_runs":[
			{"id":1,"repository":{"full_name":"darkraise/darkmem","private":false}},
			{"id":2,"repository":{"full_name":"darkraise/darkmem","private":true}},
			{"id":3,"repository":{"full_name":"darkraise/darkmem"}},
			{"id":4,"repository":null},
			{"id":5}]}`)
	})
	runs, err := c.ListRuns(context.Background(), "darkmem", "queued")
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, false, false, false, false}
	if len(runs) != len(want) {
		t.Fatalf("runs %+v", runs)
	}
	for i, r := range runs {
		if r.RepoPublic() != want[i] {
			t.Errorf("run %d: public %v, want %v", r.ID, r.RepoPublic(), want[i])
		}
	}
}
```

`newClient` is the file's existing helper: it starts a test server with the handler it is given and returns a client that talks to it. The five runs are a public one, a private one, one whose repository object has no flag, one whose repository is `null`, and one with no repository at all.

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/github/ -run 'TestRunSaysWhetherItsRepositoryIsPublic'`
Expected: FAIL with `r.RepoPublic undefined (type Run has no field or method RepoPublic)` (the package's tests do not build yet)

- [ ] **Step 3: Write the implementation**

In `internal/github/client.go`, at the end of the `Run` struct, replace

```go
	RunAttempt   int           `json:"run_attempt"`
	PullRequests []PullRequest `json:"pull_requests"`
}
```

with

```go
	RunAttempt   int           `json:"run_attempt"`
	PullRequests []PullRequest `json:"pull_requests"`
	Repository   RunRepository `json:"repository"`
}

// RunRepository is the repository object GitHub puts in every run. Private is
// nil when the answer did not carry the flag.
type RunRepository struct {
	Private *bool `json:"private"`
}

// RepoPublic reports whether the run says its repository is public. A run
// that came without the flag says nothing, so it does not say public.
func (r Run) RepoPublic() bool {
	return r.Repository.Private != nil && !*r.Repository.Private
}
```

Every call that decodes a `Run` (the runs listings, a single run, a run attempt) goes through this type, so nothing else changes.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go vet ./... && PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/github/ ./internal/runner/ ./internal/daemon/`
Expected: no output from `go vet`, then PASS for the three packages

The runner and the daemon run too because both build `github.Run` values in their tests; they must not notice the new field.

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/github` (must print nothing), then:

```bash
git add internal/github/client.go internal/github/client_test.go
git commit -m "feat(github): decode a run's repository visibility"
```

### Task 31: A repository that turned public is blocked from the poll that shows it

**Files:**
- Modify: `internal/runner/visibility.go` (`visibility`, `visibilityLocked`, `readVisibility`, new `sawPublic`)
- Modify: `internal/runner/tick.go` (`repoDemand`, `gatherDemand`)
- Test: `internal/runner/blocked_test.go`

**Interfaces:**
- Consumes: from Task 30, in package `github`: `type RunRepository struct { Private *bool }`, the field `Run.Repository RunRepository`, and `func (r Run) RepoPublic() bool`
- Produces: `func (m *Manager) sawPublic(repo string, now time.Time)` in `internal/runner/visibility.go`, placed directly above `blockedLocked`, and the field `listedAt time.Time` of the `visibility` struct; Task 32 adds to the same struct and places its method directly above `sawPublic`

**Items:** 214
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 3 = 5

ghr serves only private repositories unless a repository has `allow_public` in the config: a job in a public repository can come from anyone's pull request. The manager keeps GitHub's last answer on whether each repository is public and reads it again every 6 hours with `GetRepo`, and at the next poll after a reload. A repository whose answer is public and which has no `allow_public` is **blocked**: it gets no new runner, its queued jobs are not counted, and status says why. So a repository that is made public keeps being served for up to 6 hours.

GitHub already says more often than that. On every poll `repoDemand` lists the repository's queued, in-progress and waiting runs, and since Task 30 each `github.Run` can say whether its repository is public (`run.RepoPublic()`). After this task, when any listed run says so, the manager blocks the repository at once, in that tick, before anything is planned.

What a listing says is kept apart from what `GetRepo` said. The manager's record for a repository gets a second time, `listedAt`: the time of the tick in which a listing last said public. `GetRepo`'s answer and its time are not touched by a listing. The rules:

- A repository is blocked when `GetRepo` last said public, or when `listedAt` is set.
- A listing never makes `GetRepo`'s answer look fresh. The 6-hour read, and the read a reload asks for, still happen while public runs keep being listed.
- A listing never lifts a block and never gives an unread repository a "private" answer: a listing can come from the client's response cache. `GetRepo`'s answer is what clears `listedAt`.
- In a poll whose own listing says public, the block holds for that tick whatever `GetRepo` then says: `GetRepo` clears `listedAt` only when it is from an earlier tick.
- When a listing has blocked the repository and a later call of the same poll fails, the queued jobs an earlier poll counted for it are dropped all the same.

- [ ] **Step 1: Write the failing tests**

Add this to the end of `internal/runner/blocked_test.go`:

```go
// listedRun gives repo one queued run with jobs, as GitHub's runs listing
// names it: every run there says whether its repository is private.
func listedRun(h *harness, repo string, runID int64, private bool, jobs ...github.Job) {
	h.gh.mu.Lock()
	defer h.gh.mu.Unlock()
	h.gh.runs[repo+"/queued"] = []github.Run{{ID: runID, Status: "queued", Repository: github.RunRepository{Private: &private}}}
	h.gh.jobs[runID] = jobs
}

// The poll's own runs listing shows a repository that turned public, hours
// before the next read of the repository is due. It is blocked in that tick:
// no runner starts for the job the listing shows and the job is not counted.
// A later listing that says private lifts nothing; the repository's own read
// does, six hours after its last answer.
func TestRunListingBlocksAPublicRepositoryInTheSameTick(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.m.Tick(ctx) // darkmem is read: private
	tick := func(after time.Duration) {
		h.now = h.now.Add(after)
		h.m.Tick(ctx)
		h.m.Wait()
	}

	listedRun(h, "darkmem", 7, false, homelabJob(h, 71))
	queuedRun(h, "darkcloud", 8, homelabJob(h, 81))
	tick(10 * time.Second)
	if spawnsFor(h, "darkmem") != 0 || spawnsFor(h, "darkcloud") != 1 {
		t.Fatalf("spawns: darkmem %d, darkcloud %d\n%s", spawnsFor(h, "darkmem"), spawnsFor(h, "darkcloud"), h.eventText())
	}
	if r := h.m.Status().Repos[1]; r.Error != darkmemBlocked || r.Queued != 0 {
		t.Fatalf("status: error %q queued %d", r.Error, r.Queued)
	}
	if eventCount(h, "warn darkmem "+darkmemBlocked) != 1 || h.repoReads("darkmem") != 1 {
		t.Fatalf("%d reads of darkmem\n%s", h.repoReads("darkmem"), h.eventText())
	}

	listedRun(h, "darkmem", 7, true, homelabJob(h, 71))
	tick(10 * time.Second)
	if !h.m.blocked(h.cfg, h.cfg.Repos[1]) || spawnsFor(h, "darkmem") != 0 || h.repoReads("darkmem") != 1 {
		t.Fatalf("a listing that says private: blocked %v, %d spawns, %d reads",
			h.m.blocked(h.cfg, h.cfg.Repos[1]), spawnsFor(h, "darkmem"), h.repoReads("darkmem"))
	}

	tick(visibilityTTL - 20*time.Second)
	if h.m.blocked(h.cfg, h.cfg.Repos[1]) || spawnsFor(h, "darkmem") != 1 || h.repoReads("darkmem") != 2 {
		t.Fatalf("six hours after the repository's last read: blocked %v, %d spawns, %d reads\n%s",
			h.m.blocked(h.cfg, h.cfg.Repos[1]), spawnsFor(h, "darkmem"), h.repoReads("darkmem"), h.eventText())
	}
}

// A listing can come from the client's cache, so one that says private is
// not an answer: a repository GitHub has not been read for stays unread.
func TestRunListingThatSaysPrivateLeavesARepositoryUnread(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("GetRepo darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	listedRun(h, "darkmem", 7, true, homelabJob(h, 71))
	h.m.Tick(context.Background())
	h.m.Wait()
	if _, known := h.visibility("darkmem"); known || spawnsFor(h, "darkmem") != 0 {
		t.Fatalf("known %v, %d spawns", known, spawnsFor(h, "darkmem"))
	}
}

// A listing that keeps saying public is not the repository's own read. That
// read is still made six hours after its last answer, and once.
func TestPublicListingsDoNotPostponeTheRepositoryRead(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	start := h.now
	h.m.Tick(ctx) // darkmem is read: private
	h.gh.setPublic("darkmem", true)
	listedRun(h, "darkmem", 7, false, homelabJob(h, 71))
	for _, step := range []struct {
		at    time.Duration
		reads int
	}{{10 * time.Second, 1}, {3 * time.Hour, 1}, {visibilityTTL - time.Second, 1}, {visibilityTTL, 2}, {visibilityTTL + 10*time.Second, 2}} {
		h.now = start.Add(step.at)
		h.m.Tick(ctx)
		h.m.Wait()
		if h.repoReads("darkmem") != step.reads || !h.m.blocked(h.cfg, h.cfg.Repos[1]) || spawnsFor(h, "darkmem") != 0 {
			t.Fatalf("at %v: %d reads, want %d; blocked %v, %d spawns", step.at, h.repoReads("darkmem"), step.reads,
				h.m.blocked(h.cfg, h.cfg.Repos[1]), spawnsFor(h, "darkmem"))
		}
	}
}

// A reload asks for every repository to be read again, and a listing that
// says public in the next poll does not cancel that. The read's answer does
// not lift the block in a poll whose own listing says public; it does in one
// whose listing does not.
func TestPublicListingKeepsTheReadAReloadAskedFor(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.m.Tick(ctx) // darkmem is read: private
	tick := func() {
		h.now = h.now.Add(10 * time.Second)
		h.m.Tick(ctx)
		h.m.Wait()
	}
	check := func(when string, reads int, blocked bool, spawns int) {
		t.Helper()
		if h.repoReads("darkmem") != reads || h.m.blocked(h.cfg, h.cfg.Repos[1]) != blocked || spawnsFor(h, "darkmem") != spawns {
			t.Fatalf("%s: %d reads, blocked %v, %d spawns; want %d, %v, %d\n%s", when, h.repoReads("darkmem"),
				h.m.blocked(h.cfg, h.cfg.Repos[1]), spawnsFor(h, "darkmem"), reads, blocked, spawns, h.eventText())
		}
	}
	listedRun(h, "darkmem", 7, false, homelabJob(h, 71))
	tick()
	check("a public listing", 1, true, 0)

	h.m.ClearDegraded()
	tick()
	check("a reload, then another public listing", 2, true, 0)

	listedRun(h, "darkmem", 7, true, homelabJob(h, 71))
	tick()
	check("a private listing without a read", 2, true, 0)

	h.m.ClearDegraded()
	tick()
	check("a reload, then a private listing", 3, false, 1)
}

// The listing blocks before the runs' jobs are read. When that read then
// fails, the jobs an earlier poll counted for the repository go all the same.
func TestRunListingBlockDropsTheQueueWhenThePollThenFails(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 0 // nothing spawns, so the job stays queued
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Queued != 1 {
		t.Fatalf("setup: queued %d", r.Queued)
	}
	listedRun(h, "darkmem", 7, false, homelabJob(h, 71))
	h.gh.setErr("ListJobs darkmem", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "Bad Gateway"})
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Queued != 0 || r.OldestQueuedAt != nil || r.Error != darkmemBlocked {
		t.Fatalf("queued %d, oldest %v, error %q", r.Queued, r.OldestQueuedAt, r.Error)
	}
}
```

`homelabJob`, `queuedRun`, `spawnsFor`, `eventCount`, `darkmemBlocked`, `h.repoReads` and `h.visibility` are the package's existing test helpers; `h.repoReads` counts the `GetRepo` calls for a repository, and `h.m.ClearDegraded()` is what a reload calls. The fake GitHub answers `GetRepo` with "private" unless a test says otherwise. `darkmem` is `h.cfg.Repos[1]`.

`TestRunListingThatSaysPrivateLeavesARepositoryUnread` passes before and after this task. It pins the rule that a listing saying private is not an answer, so that a later change cannot break it unnoticed.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/runner/ -run 'TestRunListing|TestPublicListing'`
Expected: FAIL in four tests, because nothing blocks the repository yet:

- `TestRunListingBlocksAPublicRepositoryInTheSameTick`: `spawns: darkmem 1, darkcloud 1`
- `TestPublicListingsDoNotPostponeTheRepositoryRead`: `at 10s: 1 reads, want 1; blocked false, 1 spawns`
- `TestPublicListingKeepsTheReadAReloadAskedFor`: `a public listing: 1 reads, blocked false, 1 spawns; want 1, true, 0`
- `TestRunListingBlockDropsTheQueueWhenThePollThenFails`: `queued 1, oldest 2026-10-03 12:00:00 +0000 UTC, error "github: 502 Bad Gateway"`

- [ ] **Step 3: Keep what a listing said apart from GetRepo's answer**

In `internal/runner/visibility.go`, replace the `visibility` struct and `visibilityLocked`

```go
// visibility is GitHub's last answer on whether a repository is public. A
// zero readAt makes the next poll read it again whatever its age.
type visibility struct {
	public bool
	readAt time.Time
}

// visibilityLocked reports whether GitHub last said repo is public, and
// whether it has answered for repo at all; m.mu must be held.
func (m *Manager) visibilityLocked(repo string) (public, known bool) {
	v, known := m.vis[strings.ToLower(repo)]
	return v.public, known
}
```

with

```go
// visibility is what GitHub last said about whether a repository is public.
// public and readAt are GetRepo's answer and its time; a zero readAt makes
// the next poll ask GetRepo again whatever the answer's age. listedAt is the
// time of the tick in which a runs listing last said public, zero when none
// has since GetRepo cleared it. The two are kept apart so that a listing
// blocks without making GetRepo's answer look fresh.
type visibility struct {
	public   bool
	readAt   time.Time
	listedAt time.Time
}

// visibilityLocked reports whether GitHub last said repo is public, through
// GetRepo or through a runs listing, and whether it has said anything for
// repo at all; m.mu must be held.
func (m *Manager) visibilityLocked(repo string) (public, known bool) {
	v, known := m.vis[strings.ToLower(repo)]
	return v.public || !v.listedAt.IsZero(), known
}
```

At the end of `readVisibility`, replace

```go
	m.mu.Lock()
	nv := visibility{public: !r.Private, readAt: now}
	if m.visGen != gen {
		nv.readAt = time.Time{} // ClearDegraded ran during the read; the next poll reads again
	}
	m.vis[key] = nv
	m.mu.Unlock()
	return nil
}
```

with

```go
	m.mu.Lock()
	nv := visibility{public: !r.Private, readAt: now}
	if m.visGen != gen {
		nv.readAt = time.Time{} // ClearDegraded ran during the read; the next poll reads again
	}
	// A listing of this same poll said public: the block holds for this tick
	// whatever GetRepo says. An older one is what this answer replaces.
	if at := m.vis[key].listedAt; at.Equal(now) {
		nv.listedAt = at
	}
	m.vis[key] = nv
	m.mu.Unlock()
	return nil
}
```

Add this method directly above the comment `// blockedLocked is blocked with m.mu held.`:

```go
// sawPublic records that a run GitHub just listed for repo says its
// repository is public: the repository is blocked from this tick on, without
// waiting for readVisibility's next turn. GetRepo's answer and its time are
// left as they are, so that read still comes when it is due. A listing only
// ever blocks. One that says private is no answer, because a listing can come
// from the client's cache; lifting a block stays with readVisibility.
func (m *Manager) sawPublic(repo string, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.ToLower(repo)
	v := m.vis[key]
	v.listedAt = now
	m.vis[key] = v
}
```

`now` in `readVisibility` and in `sawPublic` is the same value within one tick: `gatherDemand` passes its own `now` to both. `ClearDegraded` in `manager.go` sets `readAt` to zero on every record and leaves the rest of the record alone, so it needs no change.

- [ ] **Step 4: Record a public listing, and drop the queue on the failure path**

In `internal/runner/tick.go`, add `slices` to the imports: replace

```go
	"context"
	"strings"
	"time"
```

with

```go
	"context"
	"slices"
	"strings"
	"time"
```

In `repoDemand`, replace

```go
		runs, err := m.GH.ListRuns(ctx, r.Name, status)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
```

with

```go
		runs, err := m.GH.ListRuns(ctx, r.Name, status)
		if err != nil {
			return nil, err
		}
		// Recorded before the runs' jobs are read, so a failure there does
		// not lose the answer.
		if slices.ContainsFunc(runs, github.Run.RepoPublic) {
			m.sawPublic(r.Name, now)
		}
		for _, run := range runs {
```

In `gatherDemand`, replace

```go
		if err != nil {
			missed(r)
			if tokenRejected(err) {
				authFailed = true
			}
```

with

```go
		if err != nil {
			missed(r)
			if !served(r) {
				// The listing blocked the repo before a later call of its poll
				// failed; what an earlier poll left for it goes all the same.
				delete(demand, r.Name)
			}
			if tokenRejected(err) {
				authFailed = true
			}
```

On the path without an error nothing else is needed: `gatherDemand` calls `readVisibility` right after `repoDemand`, and the code after it already drops the queued jobs of a repository that has just become blocked. `spawnPlanned` plans from a copy of the config in which a blocked repository is paused, and `noteBlocked` writes the event.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go vet ./... && PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/github/ ./internal/runner/ ./internal/daemon/`
Expected: no output from `go vet`, then PASS for the three packages

- [ ] **Step 6: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/runner` (must print nothing), then:

```bash
git add internal/runner/tick.go internal/runner/visibility.go internal/runner/blocked_test.go
git commit -m "fix(runner): block a public repo from its run listing"
```

### Task 32: The manager takes a visibility answer from outside

**Files:**
- Modify: `internal/runner/visibility.go` (`visibility`, `readVisibility`, new `RecordVisibility`, `forgetRemoved`)
- Test: `internal/runner/visibility_test.go`

**Interfaces:**
- Consumes: the `visibility` struct with its field `listedAt`, the end of `readVisibility`, and the position of `func (m *Manager) sawPublic(repo string, now time.Time)` in `internal/runner/visibility.go`, all as Task 31 left them
- Produces: `func (m *Manager) RecordVisibility(repo string, public bool)` on `*runner.Manager`, exported; Task 33 calls it from the daemon. Its contract: the caller hands an answer over only for a repository it is adding, and does so before the config that holds the repository can be seen

**Items:** 211
**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

The runner manager keeps, per repository, GitHub's last answer on whether the repository is public, and when it was read. Until now only the manager itself writes that record: from its own `GetRepo` read every 6 hours, and from the runs listing.

The daemon also reads a repository from GitHub, when the owner adds one. The next task hands that answer to the manager, so that a repository removed and added again never keeps the answer from before its removal. This task gives the manager the method that takes such an answer. It records the answer as a read made now, replacing whatever was there, in either direction. Nothing calls it yet.

Two things make the hand-over safe against the manager's own work, which runs on the tick's goroutine while the hand-over comes from a request's:

- **A read in flight.** `readVisibility` releases the manager's lock while it waits for `GetRepo` and then stores the answer. A read that began before a hand-over can end after it and would put its older answer back. So each record counts its hand-overs in `rev`, and a read stores its answer only when `rev` is what it was when the read began.
- **A tick before the repository is in the config.** `forgetRemoved` drops the record of every repository that is not in the config, at the start of each tick. An answer handed over for a repository that is being added is not in the config yet. So a handed-over record is marked `handed`, and `forgetRemoved` keeps it until the repository has been seen in the config once; from then on it is forgotten like any other when the repository is removed.

- [ ] **Step 1: Write the failing tests**

In `internal/runner/visibility_test.go`, add the `config` import: replace

```go
	"github.com/darkraise/ghr/internal/github"
)
```

with

```go
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
)
```

Then add this to the end of the file:

```go
// AddRepo reads a repository before it saves it and hands the answer over,
// as a read made now. Without it, a repository removed and added again before
// a tick would keep the answer from before its removal.
func TestRecordVisibilityReplacesAnOlderAnswer(t *testing.T) {
	h := newHarness(t)
	blockDarkmem(t, h)
	reads := h.repoReads("darkmem")
	h.m.RecordVisibility("DarkMem", false)
	if mem := h.cfg.Repos[1]; h.m.unread(mem) || h.m.blocked(h.cfg, mem) {
		t.Fatalf("after a private answer: unread %v blocked %v", h.m.unread(mem), h.m.blocked(h.cfg, mem))
	}
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	h.m.Wait()
	// GitHub still says public to a read, so a read would have blocked it again.
	if spawnsFor(h, "darkmem") != 1 || h.repoReads("darkmem") != reads {
		t.Fatalf("%d spawns, %d reads since the answer\n%s", spawnsFor(h, "darkmem"), h.repoReads("darkmem")-reads, h.eventText())
	}
	h.m.RecordVisibility("darkmem", true)
	if !h.m.blocked(h.cfg, h.cfg.Repos[1]) {
		t.Fatal("not blocked after a public answer")
	}
}

// A read of the repository that began before an answer was handed over ends
// after it. The handed-over answer is the newer one and stays.
func TestOlderRepositoryReadDoesNotReplaceAHandedOverAnswer(t *testing.T) {
	h := newHarness(t)
	h.gh.hook = func(method string) {
		if method == "GetRepo" && strings.HasSuffix(h.gh.callsText(), "GetRepo darkmem") {
			h.m.RecordVisibility("darkmem", true)
		}
	}
	h.m.Tick(context.Background()) // the read in flight will answer private
	h.gh.hook = nil
	if public, known := h.visibility("darkmem"); !public || !known || h.repoReads("darkmem") != 1 {
		t.Fatalf("public %v known %v after %d reads; the read that began first replaced the answer", public, known, h.repoReads("darkmem"))
	}
}

// An answer is handed over just before its repository shows up in the
// config. A tick that still sees the config without the repository keeps the
// answer, the tick that first sees the repository plans with it without a
// read of its own, and removing the repository later drops it.
func TestHandedOverAnswerWaitsForItsRepository(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	with := h.cfg
	without := h.cfg.Clone()
	without.Repos = without.Repos[:1]
	tick := func(cfg *config.Config) {
		h.cfg = cfg
		h.now = h.now.Add(10 * time.Second)
		h.m.Tick(ctx)
		h.m.Wait()
	}
	tick(without)
	h.m.RecordVisibility("darkmem", false)
	tick(without)
	if _, known := h.visibility("darkmem"); !known {
		t.Fatal("a tick before the repository was in the config dropped the answer")
	}
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	tick(with)
	if spawnsFor(h, "darkmem") != 1 || h.repoReads("darkmem") != 0 {
		t.Fatalf("%d spawns, %d reads; want a runner planned on the handed-over answer\n%s", spawnsFor(h, "darkmem"), h.repoReads("darkmem"), h.eventText())
	}
	tick(without)
	if _, known := h.visibility("darkmem"); known {
		t.Fatal("the answer outlived its repository")
	}
}
```

`blockDarkmem` makes the fake GitHub answer "public" for darkmem and runs the tick that finds out; the fake keeps answering "public" for the rest of that test, which is how the test shows that the handed-over answer counts as a read made now. The fake GitHub's `hook` runs during every call, after the call is recorded and before it answers, so the second test hands an answer over while the manager's own `GetRepo` for darkmem is in flight. `queuedRun`, `homelabJob`, `spawnsFor`, `h.repoReads` and `h.visibility` are the package's existing helpers.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/runner/ -run 'TestRecordVisibility|TestOlderRepositoryRead|TestHandedOverAnswer'`
Expected: FAIL with `h.m.RecordVisibility undefined (type *Manager has no field or method RecordVisibility)` (the package's tests do not build yet)

- [ ] **Step 3: Write the implementation**

In `internal/runner/visibility.go`, replace the `visibility` struct

```go
// visibility is what GitHub last said about whether a repository is public.
// public and readAt are GetRepo's answer and its time; a zero readAt makes
// the next poll ask GetRepo again whatever the answer's age. listedAt is the
// time of the tick in which a runs listing last said public, zero when none
// has since GetRepo cleared it. The two are kept apart so that a listing
// blocks without making GetRepo's answer look fresh.
type visibility struct {
	public   bool
	readAt   time.Time
	listedAt time.Time
}
```

with

```go
// visibility is what GitHub last said about whether a repository is public.
// public and readAt are GetRepo's answer and its time; a zero readAt makes
// the next poll ask GetRepo again whatever the answer's age. listedAt is the
// time of the tick in which a runs listing last said public, zero when none
// has since GetRepo cleared it. The two are kept apart so that a listing
// blocks without making GetRepo's answer look fresh.
type visibility struct {
	public   bool
	readAt   time.Time
	listedAt time.Time
	// rev counts the answers handed over through RecordVisibility. A GetRepo
	// read applies its answer only when rev is what it was as the read began.
	rev uint64
	// handed marks an answer handed over for a repository that is being
	// added: it is kept until that repository shows up in the config.
	handed bool
}
```

At the end of `readVisibility`, replace

```go
	m.mu.Lock()
	nv := visibility{public: !r.Private, readAt: now}
	if m.visGen != gen {
		nv.readAt = time.Time{} // ClearDegraded ran during the read; the next poll reads again
	}
	// A listing of this same poll said public: the block holds for this tick
	// whatever GetRepo says. An older one is what this answer replaces.
	if at := m.vis[key].listedAt; at.Equal(now) {
		nv.listedAt = at
	}
	m.vis[key] = nv
	m.mu.Unlock()
	return nil
}
```

with

```go
	m.mu.Lock()
	defer m.mu.Unlock()
	nv := m.vis[key]
	if nv.rev != v.rev {
		return nil // an answer was handed over during the read; it is the newer one
	}
	nv.public, nv.readAt = !r.Private, now
	if m.visGen != gen {
		nv.readAt = time.Time{} // ClearDegraded ran during the read; the next poll reads again
	}
	// A listing of this same poll said public: the block holds for this tick
	// whatever GetRepo says. An older one is what this answer replaces.
	if !nv.listedAt.Equal(now) {
		nv.listedAt = time.Time{}
	}
	m.vis[key] = nv
	return nil
}
```

`v` is the record `readVisibility` copied under the lock before it called `GetRepo`; for a repository with no record yet it is the zero record, whose `rev` is 0. The new code starts from the record as it is now and changes only what `GetRepo` answers, so `rev` and `handed` are carried over.

Add this method directly above the comment `// sawPublic records that a run GitHub just listed for repo says its`:

```go
// RecordVisibility takes an answer the caller has just read from GitHub for
// repo, as a read made now. Adding a repository reads it first; handing that
// answer over replaces whatever is left from before the repository was
// removed, and a read of the manager's own that is still in flight does not
// replace it. The caller hands over only for a repository it is adding: the
// answer is kept until that repository shows up in the config, so it must be
// handed over before the config with the repository can be seen.
func (m *Manager) RecordVisibility(repo string, public bool) {
	now := m.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.ToLower(repo)
	m.vis[key] = visibility{public: public, readAt: now, rev: m.vis[key].rev + 1, handed: true}
}
```

Replace the whole of `forgetRemoved`, with its comment,

```go
// forgetRemoved drops what is kept about visibility for repos that are no
// longer in the config, so one that is removed and added again starts unread
// and its removal is not reported as a block being lifted. It needs no API
// call, so it runs on every tick.
func (m *Manager) forgetRemoved(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.vis {
		if cfg.Repo(key) == nil {
			delete(m.vis, key)
		}
	}
	for key := range m.wasBlocked {
		if cfg.Repo(key) == nil {
			delete(m.wasBlocked, key)
		}
	}
}
```

with

```go
// forgetRemoved drops what is kept about visibility for repos that are no
// longer in the config, so one that is removed and added again starts unread
// and its removal is not reported as a block being lifted. An answer handed
// over for a repo that is being added is not in the config yet; it is kept
// until the repo has been seen there once. It needs no API call, so it runs
// on every tick.
func (m *Manager) forgetRemoved(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, v := range m.vis {
		switch listed := cfg.Repo(key) != nil; {
		case listed && v.handed:
			v.handed = false
			m.vis[key] = v
		case !listed && !v.handed:
			delete(m.vis, key)
		}
	}
	for key := range m.wasBlocked {
		if cfg.Repo(key) == nil {
			delete(m.wasBlocked, key)
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go vet ./... && PATH=/usr/local/go/bin:$PATH go test -race -count=1 -timeout 300s ./internal/runner/ && PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/github/ ./internal/daemon/`
Expected: no output from `go vet`, then PASS for the three packages

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/runner` (must print nothing), then:

```bash
git add internal/runner/visibility.go internal/runner/visibility_test.go
git commit -m "feat(runner): take a visibility answer from outside"
```

### Task 33: Adding a repository hands over what it just read

**Files:**
- Modify: `internal/daemon/store.go` (`Update`, new `UpdateThen`)
- Modify: `internal/daemon/backend.go` (the `Manager` interface, `update`, new `updateThen`, `AddRepo`)
- Test: `internal/daemon/backend_test.go` (`fakeManager`, `TestAddRepo`, `TestConcurrentAddRepoConflicts`)

**Interfaces:**
- Consumes: `func (m *Manager) RecordVisibility(repo string, public bool)` on `*runner.Manager` (Task 32), with its contract: hand over only for a repository being added, before the config that holds it can be seen
- Produces: `func (s *Store) UpdateThen(fn func(c *config.Config) error, saved func()) ([]string, error)`; `RecordVisibility(repo string, public bool)` as the last method of the daemon's `Manager` interface, and the same method on the test fake `fakeManager`

**Items:** 211
**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

The manager keeps GitHub's last answer on whether each repository is public, and drops the answer of a repository that is no longer in the config at the start of every tick. A repository that is removed and added again before a tick runs therefore keeps its old answer for up to 6 hours. The one wrong outcome is a repository that is private again and stays blocked.

`AddRepo` reads the repository from GitHub with `GetRepo` before it saves anything. After this task it hands that answer to the manager through `RecordVisibility`, which Task 32 added.

**When the answer is handed over, and why.** The hand-over happens inside the store's update, after `config.yaml` is written and before the new config is published. A refused add (a duplicate under the name GitHub reports, a concurrent add that lost, a failed validation or save) never reaches that step, so it hands over nothing and the answer of a repository that is already configured is left alone. Every reader of the config, the tick included, can see the repository only after its answer is in the manager, so the first tick that plans for the repository plans with that answer. A tick that runs between the hand-over and the publication still sees the old config; Task 32 makes the manager keep a handed-over answer until its repository has shown up in the config once, so that tick does not drop it.

For this the store gets `UpdateThen`: `Update` with a step that runs between the save and the swap, with the store's lock held. The daemon talks to the manager through its own `Manager` interface, and its tests use `fakeManager`; both get the method.

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/backend_test.go`, give `fakeManager` the method. At the end of the `fakeManager` struct, replace

```go
	now         time.Time    // Status().Now
	diskUsed    int64        // Status().DiskUsedBytes
	rate        int          // Status().RateRemaining
	statusCalls atomic.Int64 // Status calls; the change hub reads it from its own goroutine
}
```

with

```go
	now         time.Time    // Status().Now
	diskUsed    int64        // Status().DiskUsedBytes
	rate        int          // Status().RateRemaining
	statusCalls atomic.Int64 // Status calls; the change hub reads it from its own goroutine

	vmu          sync.Mutex
	visibility   []string          // RecordVisibility calls, as "<repo> public" or "<repo> private"; guarded by vmu
	onVisibility func(repo string) // runs during RecordVisibility, before the call is recorded
}

func (f *fakeManager) RecordVisibility(repo string, public bool) {
	if f.onVisibility != nil {
		f.onVisibility(repo)
	}
	answer := " private"
	if public {
		answer = " public"
	}
	f.vmu.Lock()
	f.visibility = append(f.visibility, repo+answer)
	f.vmu.Unlock()
}
```

Replace the whole of `TestAddRepo`

```go
func TestAddRepo(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "missing"}); apiStatus(err) != 400 || !strings.Contains(err.Error(), "repository access") {
		t.Fatalf("missing err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "public"}); apiStatus(err) != 409 {
		t.Fatalf("public err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "darkcloud"}); apiStatus(err) != 409 {
		t.Fatalf("duplicate err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "public", AllowPublic: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "newrepo", Labels: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if b.Store.Config().Repo("newrepo") == nil {
		t.Fatal("repo not saved")
	}
}
```

with

```go
func TestAddRepo(t *testing.T) {
	b, m, gh := newBackend(t)
	ctx := context.Background()
	// oldcloud is a former name of darkcloud, which is configured.
	gh.repos["oldcloud"] = &github.Repository{FullName: "darkraise/darkcloud", Private: true}
	var order, misplaced []string
	b.Wake = func() { order = append(order, "wake") }
	m.onVisibility = func(repo string) {
		order = append(order, "hand-over "+repo)
		saved, _, err := config.Load(b.Store.ConfigPath)
		if err != nil || saved.Repo(repo) == nil || b.Store.Config().Repo(repo) != nil {
			misplaced = append(misplaced, repo)
		}
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "missing"}); apiStatus(err) != 400 || !strings.Contains(err.Error(), "repository access") {
		t.Fatalf("missing err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "public"}); apiStatus(err) != 409 {
		t.Fatalf("public err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "darkcloud"}); apiStatus(err) != 409 {
		t.Fatalf("duplicate err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "oldcloud"}); apiStatus(err) != 409 {
		t.Fatalf("duplicate under a former name: err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "public", AllowPublic: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "newrepo", Labels: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if b.Store.Config().Repo("newrepo") == nil {
		t.Fatal("repo not saved")
	}
	// What AddRepo read goes to the manager only for an add that is saved. A
	// refused one hands over nothing, so the answer of the repository that
	// is already configured is left alone.
	if want := []string{"public public", "newrepo private"}; !reflect.DeepEqual(m.visibility, want) {
		t.Fatalf("handed over %v, want %v", m.visibility, want)
	}
	// The hand-over comes once config.yaml holds the repository and before
	// the new config can be seen, so before the loop is woken: a tick that
	// sees the repository finds its answer.
	if want := []string{"hand-over public", "wake", "hand-over newrepo", "wake"}; !reflect.DeepEqual(order, want) || len(misplaced) != 0 {
		t.Fatalf("order %v, want %v; handed over before the save or after the new config showed: %v", order, want, misplaced)
	}
}
```

At the start of `TestConcurrentAddRepoConflicts`, replace

```go
func TestConcurrentAddRepoConflicts(t *testing.T) {
	b, _, gh := newBackend(t)
```

with

```go
func TestConcurrentAddRepoConflicts(t *testing.T) {
	b, m, gh := newBackend(t)
```

and at its end, replace

```go
	if n != 1 {
		t.Fatalf("newrepo configured %d times", n)
	}
}
```

with

```go
	if n != 1 {
		t.Fatalf("newrepo configured %d times", n)
	}
	if !reflect.DeepEqual(m.visibility, []string{"newrepo private"}) {
		t.Fatalf("handed over %v, want one answer from the add that won", m.visibility)
	}
}
```

In `TestAddRepo` the fake's `onVisibility` runs at the moment of the hand-over. It checks that `config.yaml` on disk already holds the repository and that `b.Store.Config()`, which is what every reader sees, does not yet. `b.Wake` is what wakes the run loop after a saved change. The four refused adds are a repository the token cannot see, a public one without `AllowPublic`, one already configured, and `oldcloud`, which GitHub reports as `darkcloud`, already configured. The fake needs its mutex because `TestConcurrentAddRepoConflicts` adds from two goroutines.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/daemon/ -run 'TestAddRepo$|TestConcurrentAddRepoConflicts'`
Expected: FAIL in both tests with `handed over [], want` followed by the expected answers

- [ ] **Step 3: Give the store a step between the save and the swap**

In `internal/daemon/store.go`, replace

```go
// Update applies fn to a copy of the config, validates, saves, then swaps it in.
// A failed save is returned as a *SaveError.
func (s *Store) Update(fn func(c *config.Config) error) ([]string, error) {
	s.mu.Lock()
```

with

```go
// Update applies fn to a copy of the config, validates, saves, then swaps it in.
// A failed save is returned as a *SaveError.
func (s *Store) Update(fn func(c *config.Config) error) ([]string, error) {
	return s.UpdateThen(fn, nil)
}

// UpdateThen is Update with a step between the save and the swap: saved, when
// not nil, runs once config.yaml holds the change and before any reader of
// Config can see it. Whatever must be in place when the change shows is put
// in place there. It runs with the store's lock held and must not call the
// store.
func (s *Store) UpdateThen(fn func(c *config.Config) error, saved func()) ([]string, error) {
	s.mu.Lock()
```

and, in what is now the body of `UpdateThen`, replace

```go
		return nil, &SaveError{Err: err}
	}
	s.cfg.Store(c)
	s.warnings = slices.Clone(warnings)
```

with

```go
		return nil, &SaveError{Err: err}
	}
	if saved != nil {
		saved()
	}
	s.cfg.Store(c)
	s.warnings = slices.Clone(warnings)
```

- [ ] **Step 4: Hand the answer over from `AddRepo`**

In `internal/daemon/backend.go`, at the end of the `Manager` interface, replace

```go
	CancelUpdate() error
	Pending() []model.HistoryEntry
}
```

with

```go
	CancelUpdate() error
	Pending() []model.HistoryEntry
	RecordVisibility(repo string, public bool)
}
```

Replace the start of `update`

```go
func (b *Backend) update(fn func(c *config.Config) error) error {
	var before []string
	warnings, err := b.Store.Update(func(c *config.Config) error {
		// Until fn runs, c is a copy of the config this update replaces.
		before, _ = c.Validate()
		return fn(c)
	})
```

with

```go
func (b *Backend) update(fn func(c *config.Config) error) error {
	return b.updateThen(fn, nil)
}

// updateThen is update with a step that runs once the change is saved and
// before it can be seen, as Store.UpdateThen runs it.
func (b *Backend) updateThen(fn func(c *config.Config) error, saved func()) error {
	var before []string
	warnings, err := b.Store.UpdateThen(func(c *config.Config) error {
		// Until fn runs, c is a copy of the config this update replaces.
		before, _ = c.Validate()
		return fn(c)
	}, saved)
```

The comment above `update` stays where it is, above `func (b *Backend) update`.

In `AddRepo`, replace

```go
	if err := b.update(func(c *config.Config) error {
		if c.Repo(name) != nil {
			return api.Conflict("repo " + name + " is already configured")
		}
		c.WatchRepos = slices.DeleteFunc(c.WatchRepos, func(w string) bool { return strings.EqualFold(w, name) || strings.EqualFold(w, typed) })
		c.Repos = append(c.Repos, config.Repo{Name: name, Max: req.Max, Labels: req.Labels, AllowPublic: req.AllowPublic})
		return nil
	}); err != nil {
```

with

```go
	if err := b.updateThen(func(c *config.Config) error {
		if c.Repo(name) != nil {
			return api.Conflict("repo " + name + " is already configured")
		}
		c.WatchRepos = slices.DeleteFunc(c.WatchRepos, func(w string) bool { return strings.EqualFold(w, name) || strings.EqualFold(w, typed) })
		c.Repos = append(c.Repos, config.Repo{Name: name, Max: req.Max, Labels: req.Labels, AllowPublic: req.AllowPublic})
		return nil
	}, func() {
		// The answer just read goes to the manager only for an add that is
		// saved, and before the config with the repository can be seen: a
		// tick that sees the repository then finds this answer, not one
		// left from before the repository was removed.
		b.M.RecordVisibility(name, !repo.Private)
	}); err != nil {
```

`name` is the name GitHub reports for the repository, which is the one the config stores; `repo` is what `GetRepo` returned. The step runs with the store's lock held and takes the manager's lock inside `RecordVisibility`. That order is safe: the manager reads the config through `Store.Config`, which takes no lock, and never calls the store while it holds its own. `run.go` puts a `*runner.Manager` into `Backend.M`; it has the method since Task 32, so the daemon still builds.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go vet ./... && PATH=/usr/local/go/bin:$PATH go test -race -count=1 -timeout 300s ./internal/daemon/ && PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/github/ ./internal/runner/`
Expected: no output from `go vet`, then PASS for the three packages

- [ ] **Step 6: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/daemon` (must print nothing), then:

```bash
git add internal/daemon/store.go internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "fix(daemon): hand the manager what AddRepo read"
```

### Task 34: The retention event and an honest comment

**Files:**
- Modify: `internal/runner/cleanup.go` (`prune`, `forcedPrune`, the comments of `projects`)
- Test: `internal/runner/prune_scope_test.go`, `internal/runner/maintenance_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `func (m *Manager) prune(cfg *config.Config, now time.Time) (removed, ok bool)` (it returned one `bool` before)

**Items:** 207, 186
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

Two small things in one file, committed apart.

**The retention event (row 207).** A manual prune with the standard scope ends by removing history entries and log archives older than `history_retention`, and writes the event `prune: history and logs past retention removed` whenever that step did not fail, also when there was nothing to remove. After this task the event is written only when a history entry or a log archive was removed. The step is recorded in the prune's result either way, as before.

`prune` therefore reports whether it removed anything. For log archives it knows. For history it asks the store's write counter: `history.Store.Writes()` counts a prune only when the prune rewrote the file, which it does only when a line went. An entry appended by a finishing job during the prune is counted as well, so in that rare case the event is written although nothing was removed, which is what happens every time today. The history package is not changed.

**The comment on `projects()` (row 186).** No behaviour changes. Cleanup removes the compose projects of an instance as root. It finds them through the labels of containers and through a list an earlier cleanup recorded. That list was moved to a directory the job cannot write, and the comment says the job "cannot edit the names already remembered", which reads as more than it is: a job with Docker access can still label a container so that another compose project is removed. That gives the job nothing new, because the runner user is in the `docker` group and can remove any project itself. The comment is rewritten to say both.

- [ ] **Step 1: Write the failing test**

In `internal/runner/prune_scope_test.go`, add `os`, `path/filepath` and `time` to the imports: replace

```go
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
```

with

```go
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
```

In the same file, add this test directly above `func TestUnknownPruneScope(t *testing.T) {`:

```go
// The retention event says something was removed, so it is written only when
// a history entry or a log archive was. The step is in the prune's record
// either way.
func TestRetentionEventOnlyWhenSomethingWasRemoved(t *testing.T) {
	const event = "prune: history and logs past retention removed"
	for _, tc := range []struct {
		name   string
		setup  func(h *harness, old time.Time)
		events int
	}{
		{"nothing past retention", func(h *harness, _ time.Time) {
			h.m.History.Append(model.HistoryEntry{ID: "new", Repo: "darkmem", FinishedAt: h.now})
			os.MkdirAll(filepath.Join(h.m.Paths.Logs, "bbbbbb"), 0o755)
		}, 0},
		{"an old history entry", func(h *harness, old time.Time) {
			h.m.History.Append(model.HistoryEntry{ID: "old", Repo: "darkmem", FinishedAt: old})
		}, 1},
		{"an old log archive", func(h *harness, old time.Time) {
			dir := filepath.Join(h.m.Paths.Logs, "aaaaaa")
			os.MkdirAll(dir, 0o755)
			os.Chtimes(dir, old, old)
		}, 1},
	} {
		h := newHarness(t)
		tc.setup(h, h.now.Add(-40*24*time.Hour))
		if err := h.m.StartPruneScope(ScopeStandard); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		h.m.Wait()
		if n := strings.Count(h.eventText(), event); n != tc.events {
			t.Errorf("%s: %d retention events, want %d\n%s", tc.name, n, tc.events, h.eventText())
		}
		if lp := h.m.LastPrune(); lp.Outcome != "ok" || !strings.HasSuffix(stepNames(lp), ",history and logs past retention") {
			t.Errorf("%s: outcome %s, steps %s", tc.name, lp.Outcome, stepNames(lp))
		}
	}
}
```

The harness's clock is 2026-10-03 and `history_retention` is the default 30 days, so "old" is 40 days before the harness's clock. `stepNames` is the file's existing helper.

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/runner/ -run 'TestRetentionEventOnlyWhenSomethingWasRemoved'`
Expected: FAIL with `nothing past retention: 1 retention events, want 0`

- [ ] **Step 3: Write the implementation**

In `internal/runner/cleanup.go`, replace the start of `prune`

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
```

with

```go
// prune drops history lines and archived logs older than history_retention.
// It reports whether it removed anything and whether every step succeeded.
func (m *Manager) prune(cfg *config.Config, now time.Time) (removed, ok bool) {
	ok = true
	cutoff := now.Add(-cfg.HistoryRetention.D())
	// The store counts a prune only when it rewrote the file, which it does
	// only when a line went. An entry appended meanwhile is counted as well,
	// so removed can be true when nothing went; it is never false when
	// something did.
	writes := m.History.Writes()
	if err := m.History.Prune(cutoff); err != nil {
		ok = false
		m.Events.Add("warn", "", "history prune: %v", err)
	}
	removed = m.History.Writes() != writes
```

and its end

```go
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
```

with

```go
			if err := os.RemoveAll(filepath.Join(m.Paths.Logs, e.Name())); err != nil {
				ok = false
				m.Events.Add("warn", "", "log archive prune: %v", err)
			} else {
				removed = true
			}
		}
	}
	m.mu.Lock()
	m.lastPrune = now
	m.mu.Unlock()
	return removed, ok
}
```

In `forcedPrune`, replace

```go
		if m.prune(cfg, m.Now()) {
			m.recordStep("history and logs past retention", "0B", nil)
			m.Events.Add("info", "", "prune: history and logs past retention removed")
		} else {
```

with

```go
		if removed, ok := m.prune(cfg, m.Now()); ok {
			m.recordStep("history and logs past retention", "0B", nil)
			if removed {
				m.Events.Add("info", "", "prune: history and logs past retention removed")
			}
		} else {
```

The other caller of `prune`, the tick in `tick.go`, ignores what it returns and needs no change.

- [ ] **Step 4: Run the package and fix the test of the old rule**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/runner/`
Expected: FAIL in `TestStartPruneRunsForcedSequence` with `"prune: history and logs past retention removed" missing or out of order in events:`

That test checks the order of a standard prune's events and expects the retention event from a prune that had nothing to remove. Give it an entry past retention, so the event it orders is still written.

In `internal/runner/maintenance_test.go`, add the `model` import: replace

```go
	"testing"
	"time"
)
```

with

```go
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)
```

and, in `TestStartPruneRunsForcedSequence`, replace

```go
	h.docker.usage = []int{40} // below the high-water mark: a forced prune runs anyway
	if err := h.m.StartPrune(); err != nil {
```

with

```go
	h.docker.usage = []int{40} // below the high-water mark: a forced prune runs anyway
	// One entry past retention, so the retention step has something to report.
	h.m.History.Append(model.HistoryEntry{ID: "old", Repo: "darkmem", FinishedAt: h.now.Add(-40 * 24 * time.Hour)})
	if err := h.m.StartPrune(); err != nil {
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/runner/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/runner`)

- [ ] **Step 6: Commit the retention event**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/runner` (must print nothing), then:

```bash
git add internal/runner/cleanup.go internal/runner/prune_scope_test.go internal/runner/maintenance_test.go
git commit -m "fix(runner): report retention only when removed"
```

- [ ] **Step 7: Rewrite the comments of `projects`**

In `internal/runner/cleanup.go`, replace

```go
// projects returns the compose projects of an instance: ghr-<id> plus every
// project whose working_dir label lies inside the instance dir, sorted.
func (m *Manager) projects(ctx context.Context, id string) ([]string, error) {
```

with

```go
// projects returns the compose projects of an instance, sorted: ghr-<id>,
// every project whose working_dir label lies inside the instance dir, and
// those an earlier cleanup recorded.
//
// The recorded list is read from the private directory, where the job cannot
// write, so a job cannot forge it. The labels remain the job's word: a job
// with Docker access can label a container with another project's name and a
// working_dir inside its instance dir, and cleanup, as root, then removes
// that project's containers, networks and volumes. That gives the job nothing
// new: the runner user is in the docker group and can remove any project
// itself.
func (m *Manager) projects(ctx context.Context, id string) ([]string, error) {
```

and, inside the function, replace

```go
	// Cleanup remembers the projects it has seen: custom projects are found
	// only through their containers, so a retry after those are removed must
	// still reach the project's networks and volumes. The list is kept where
	// the job cannot write, so it cannot edit the names already remembered.
```

with

```go
	// Cleanup remembers the projects it has seen: custom projects are found
	// only through their containers, so a retry after those are removed must
	// still reach the project's networks and volumes.
```

No code line changes in this step.

- [ ] **Step 8: Check that nothing else changed**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/runner/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/runner`)

- [ ] **Step 9: Commit the comment**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/runner` (must print nothing), then:

```bash
git add internal/runner/cleanup.go
git commit -m "docs(runner): say what the project list protects"
```

### Task 35: One rollup per hour after a clock step back

**Files:**
- Modify: `internal/metrics/metrics.go` (`Load`)
- Test: `internal/metrics/metrics_test.go` (`TestLoadDropsTheRollupOfTheCurrentHour`)

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 205
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The metrics sampler keeps one rollup per closed UTC hour, plus the hour in progress (the open hour), and saves both to a file. `Load` reads the file at start-up. It already drops a rollup dated after now, which happens when the clock was set back before a restart.

One case is left. Say 12:00 was closed, 13:00 was open, and the clock is set back to 12:30 before a restart. The rollup of 12:00 is dated before now, so `Load` keeps it. The daemon then lives 12:30 to 13:00 again and closes a second rollup for 12:00. After this task `Load` drops every rollup dated at or after the current UTC hour: that hour will be lived again. This is the rule `addToHour` already applies when the clock steps back while the daemon runs.

- [ ] **Step 1: Write the failing test**

In `internal/metrics/metrics_test.go`, add this test directly above `func TestLoadMissingAndMalformed(t *testing.T) {`:

```go
// A clock set back before a restart can land inside an hour that was already
// closed. That hour is lived again, so its rollup goes, as it does on a step
// back while the daemon runs: an hour gets one rollup.
func TestLoadDropsTheRollupOfTheCurrentHour(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Path = filepath.Join(h.dir, "metrics.json")
	noon := h.now
	// 11:00, 12:00, 12:01 and 13:00: 11:00 and 12:00 are closed, 13:00 is open.
	for _, at := range []time.Duration{-time.Hour, 0, time.Minute, time.Hour} {
		h.now = noon.Add(at)
		h.s.Sample()
	}
	if err := h.s.Save(); err != nil {
		t.Fatal(err)
	}
	h.now = noon.Add(30 * time.Minute) // 12:30, inside the closed 12:00
	if err := h.s.Load(); err != nil {
		t.Fatal(err)
	}
	if hrs := h.s.Hours(); len(hrs) != 1 || hrs[0].At.Hour() != 11 {
		t.Fatalf("hours after the load %+v", hrs)
	}
	h.s.Sample()                // 12:30
	h.now = noon.Add(time.Hour) // 13:00: 12:00 closes again
	h.s.Sample()
	hrs := h.s.Hours()
	if len(hrs) != 3 || hrs[1].At.Hour() != 12 || hrs[1].Samples != 1 || hrs[2].At.Hour() != 13 {
		t.Fatalf("hours after 12:00 was lived again %+v", hrs)
	}
}
```

`newHost` is the package's helper: its clock is the `h.now` field and starts at 2026-10-05 12:00 UTC. `Hours()` returns the closed rollups followed by the open hour.

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/metrics/ -run 'TestLoadDropsTheRollupOfTheCurrentHour'`
Expected: FAIL with `hours after the load [{At:2026-10-05 11:00:00 +0000 UTC Samples:1` followed by a second rollup, `{At:2026-10-05 12:00:00 +0000 UTC Samples:2`

- [ ] **Step 3: Write the implementation**

In `internal/metrics/metrics.go`, in `Load`, replace

```go
	s.hours = nil
	for _, r := range st.Hours {
		if !r.At.After(now) && now.Sub(r.At) <= keepHours*time.Hour {
			s.hours = append(s.hours, r)
		}
	}
	s.open = nil
	if o := st.Open; o != nil {
		current := now.UTC().Truncate(time.Hour)
		switch {
```

with

```go
	current := now.UTC().Truncate(time.Hour)
	s.hours = nil
	for _, r := range st.Hours {
		if r.At.Before(current) && now.Sub(r.At) <= keepHours*time.Hour {
			s.hours = append(s.hours, r)
		}
	}
	s.open = nil
	if o := st.Open; o != nil {
		switch {
```

and replace the comment above `Load`

```go
// Load restores what Save wrote, dropping minutes older than three hours,
// rollups older than 30 days, and minutes and rollups dated after now (the
// clock was set back). A saved open hour resumes when it is the current UTC
// hour, is closed as it stands when it is an earlier one, and is dropped when
// it is a later one. A missing file is an empty start; a malformed one
// returns an error and changes nothing.
```

with

```go
// Load restores what Save wrote, dropping minutes older than three hours and
// rollups older than 30 days. After the clock was set back it also drops the
// minutes dated after now and the rollups of the current UTC hour and of
// later ones: those hours will be lived again, the rule addToHour applies to
// a step back while sampling. A saved open hour resumes when it is the
// current UTC hour, is closed as it stands when it is an earlier one, and is
// dropped when it is a later one. A missing file is an empty start; a
// malformed one returns an error and changes nothing.
```

A rollup dated before the current hour is never dated after now, so the new condition covers the old one.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -count=1 ./internal/metrics/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/metrics`)

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/metrics` (must print nothing), then:

```bash
git add internal/metrics/metrics.go internal/metrics/metrics_test.go
git commit -m "fix(metrics): drop the current hour's rollup on load"
```

### Task 36: Whole-tree verification

**Files:**
- Test: the whole tree; this task changes no file and makes no commit

**Interfaces:**
- Consumes: the finished state of Tasks 1 to 35
- Produces: nothing
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 0 - risk 0 = 0

The tasks before this one each ran the tests of the packages they touched. This task runs everything once on the finished branch, with the race detector, because the spec asks for the whole suite and the last tasks changed code after the last whole-tree run.

- [ ] **Step 1: Check formatting**

Run: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/`
Expected: no output.

- [ ] **Step 2: Vet the whole tree**

Run: `mkdir -p /root/.cache/ghr-plan-tmp && TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH go vet ./...`
Expected: no output, exit status 0.

- [ ] **Step 3: Vet the tree for Windows**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp GOOS=windows PATH=/usr/local/go/bin:$PATH go vet ./...`
Expected: no output, exit status 0. The CLI and several packages also build for Windows, and a Unix-only constant in shared code breaks that build without failing any Linux test.

- [ ] **Step 4: Run every Go test with the race detector**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH go test -race -count=1 -timeout 480s ./...`
Expected: one `ok` line for each of the 22 packages that have tests, no `FAIL` line and no `DATA RACE` text. Give the command at least nine minutes before treating it as hung.

- [ ] **Step 5: Run the installer tests**

Run: `bash deploy/tests/setup_test.sh`
Expected: the last line is `all assertions passed`.

- [ ] **Step 6: Confirm nothing under `web/` changed**

Run: `git diff --stat 8cf8ea2 HEAD -- web/`
Expected: no output. `8cf8ea2` is the commit this plan was written against; nothing under `web/` changed between it and the start of the work.

- [ ] **Step 7: Report**

Report the result of each step. When a step fails, report the failing package or assertion with its output and stop: this task fixes nothing and commits nothing.
