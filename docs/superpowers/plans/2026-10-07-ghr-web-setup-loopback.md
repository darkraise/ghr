# ghr Loopback-only Web Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Limit ghr's first-visitor web password setup to loopback clients, fix the timing-sensitive setup.sh test, and roll v0.1.15 out to the runner LXC with live checks.

**Architecture:** The `setup` handler in `internal/webui` refuses non-loopback or proxied requests with a new `ErrSetupRemote` (403) before `auth.Setup`; address parsing moves into a helper shared with `ClientKey`. Messages that said the next visitor sets the password are reworded in the CLI, the daemon event and the login page. homelab's setup_test.sh freezes `SECONDS` in the flaky case; the README and a setup.sh comment follow the new rule. The rollout runs spec §8.

**Tech Stack:** Go 1.26, React 19 + Vitest, bash.

**Spec:** docs/superpowers/specs/2026-10-07-ghr-tui-retirement-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 1 of 4 tasks is heavy (Task 1, risk 3) and is delegated; the rest top out at total 4 (impl-sonnet-medium), raised to high because a task is delegated.

**Plan review:** 2026-10-07 — dr-superpowers:judge-opus — executability 17 / coherence 18 / coverage 16 / assumptions 16 (round 1)

> codex off — plugin-not-enabled

## Global Constraints

- Code repo: github.com/darkraise/ghr. Tasks 1–2 run in the worktree `D:/Repositories/Personal/ghr-setup-loopback` on branch `fix/web-setup-loopback`, created from ghr master `e9d52bc`. Paths in those tasks are relative to that worktree.
- Task 3 runs in homelab, worktree `D:/Repositories/Personal/homelab-setup-loopback` on branch `fix/web-setup-loopback` from homelab master. Its paths are relative to that worktree.
- After Task 2, the ghr branch is finished with dr-superpowers:finishing-a-development-branch (fast-forward into ghr master, worktree removed); after Task 3, the homelab branch likewise. Task 4 runs only after both, and acts on the live LXC `root@192.168.0.99` (`ssh -i ~/.ssh/ghr_lxc`).
- Commits: `<type>(<scope>): <subject>`, subject ≤50 chars, imperative, no period; English only. Never `--no-verify`. Never push: the owner pushes (the agent's `git push` is blocked).
- Go checks: `timeout 900 go build ./... && timeout 900 go vet ./... && timeout 900 go test ./...`. `go test -race` does not run on this Windows box; CI runs it on Linux. Run `gofmt -l .` before each Go commit; it must print nothing.
- Web checks, from `web/`: if `web/node_modules` is missing run `npm ci` first; then `npm test`, `npm run lint`, `npm run build`.
- Comments: none unless the why is non-obvious; never reference this task, a register row or the review.
- Copy strings in Contracts are exact; tests assert them.
- Every outward step in Task 4 (push, release, deploy, password-file changes on the LXC) waits for the owner's explicit yes in chat.

## Contracts

- **C1** `var ErrSetupRemote = errors.New("set the first password over SSH: ghr web set-password (or open this page through an SSH tunnel to localhost)")` in `internal/webui/auth.go`; `handler.fail` maps it to 403 in the JSON error shape `{"error": "<message>"}`.
- **C2** `func remoteAddr(s string) (netip.Addr, bool)` in `internal/webui/auth.go`: `net.SplitHostPort` (whole string on error), `netip.ParseAddr`, then `Unmap().WithZone("")`; `false` when it does not parse. `ClientKey` uses it and keeps its output.
- **C3** `func fromLoopback(r *http.Request) bool` in `internal/webui/handler.go`: false when any of `X-Forwarded-For`, `X-Real-IP`, `Forwarded` is present (`len(r.Header.Values(name)) > 0`); otherwise `remoteAddr(r.RemoteAddr)` parses and `IsLoopback()`.
- **C4** Reworded strings:
  - `ghr web reset-password` stdout and the reset route's `warn` event: `web password removed; set a new one with: ghr web set-password`
  - status warning: `warning: the web UI has no password. Run: ghr web set-password`
  - setup-page hint: `No password is set. Run ghr web set-password over SSH, or set it here from the host itself.`

## Assumptions (evidence)

- `checks` (`internal/webui/handler.go:51-89`) runs before routing and refuses an `/auth` POST without `X-GHR: 1` with its own 403, and reads the body under a 4 KB cap; the `do` test helper sets `X-GHR` and `RemoteAddr = "192.168.0.10:5555"` (`internal/webui/handler_test.go:36-53`). Read 2026-10-07.
- Handler tests running setup through `do` from the default address: `handler_test.go:78`, `:97` (expects 409), `:108` (expects 400), `:139`, `:322` (expects 500). `grep -n auth/setup`, 2026-10-07. The daemon integration test posts setup to a `127.0.0.1` listener (`internal/daemon/run_test.go:233,332`).
- `TestClientKey` (`internal/webui/auth_test.go:447-460`) pins `ClientKey` output, including `"not an address"` → `"not an address"` and `"[fe80::1%eth0]:80"` → `"fe80::/64"`.
- Bash drops `SECONDS`' special meaning once it is unset: `bash -c 'x=$(unset SECONDS; SECONDS=0; /usr/bin/sleep 2; echo $SECONDS); echo $x'` printed `0` in Git Bash on 2026-10-07.
- shellcheck is not on this box (`command -v shellcheck` empty, 2026-10-07); `uv` is (`~/.local/bin/uv`). That `uvx --from shellcheck-py shellcheck` runs here is unverified — Task 3 verifies it.
- The live UI has a password: `GET /auth/state` on `http://192.168.0.99:8080` and `https://ghr.quangtc.dev` returned `setup_required:false` on 2026-10-07. `ghr.quangtc.dev` is public through nginx-proxy-manager, which sets `X-Forwarded-For` (darkmem, v0.1.13 and v0.1.14 deploy records).
- Pushing ghr master releases the next patch version: CI's release job tags `latest + 1` with `gh release --target` unless HEAD is already tagged (`.github/workflows/ci.yml:57-73`, read 2026-10-07); the latest tag is `v0.1.14` (`git tag --sort=-v:refname`, 2026-10-07), so the push releases `v0.1.15`.
- ghr master was 16 commits ahead of origin before this plan (`git rev-list --count origin/master..master`, 2026-10-07).
- Deploys copy homelab `github-runner/setup.sh` and `config.example.yaml` to `/root/github-runner/` on the LXC and run `GHR_VERSION=<tag> bash setup.sh`; setup.sh stops ghr, so it runs only when `ghr status` shows no busy runner (darkmem, v0.1.14 deploy record).

## Task index

1. Loopback-only setup guard
2. Reword the next-visitor messages
3. homelab test fix and docs
4. Rollout v0.1.15

---

### Task 1: Loopback-only setup guard

**Superseded:** by docs/superpowers/specs/2026-10-07-ghr-first-run-setup-design.md; the guard is reverted and the rollout continues as Task 14 of docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md.

**Files:**
- Modify: `internal/webui/auth.go` (error var block at lines 26-31; `ClientKey` at 328-345)
- Modify: `internal/webui/handler.go` (`setup` at 168; `fail` at 241)
- Test: `internal/webui/handler_test.go`

**Interfaces:**
- Consumes: none
- Produces: C1, C2, C3

**Items:** 12

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 3 = 5

- [ ] **Step 1: Write the failing tests**

In `internal/webui/handler_test.go`, add this helper directly below `func body(pw string) string`:

```go
// local sends the request from loopback, then applies mods.
func local(mods ...func(*http.Request)) func(*http.Request) {
	return func(r *http.Request) {
		r.RemoteAddr = "127.0.0.1:5555"
		for _, m := range mods {
			m(r)
		}
	}
}
```

Switch the existing setup calls to loopback (the 409, 400 and 500 they assert are reached only by a loopback client):

- line 78: `rec = do(h, http.MethodPost, "/auth/setup", body(pw), local())`
- line 97: `if rec := do(h, http.MethodPost, "/auth/setup", body(pw), local()); rec.Code != http.StatusConflict {`
- line 108: `if rec := do(h, http.MethodPost, "/auth/setup", body("short"), local()); rec.Code != http.StatusBadRequest ||`
- line 139: `rec := do(h, http.MethodPost, "/auth/setup", body(pw), local(func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "https") }))`
- line 322: `do(h, http.MethodPost, "/auth/setup", body(pw), local()),`

Append this test at the end of the file:

```go
func TestSetupOnlyFromLoopback(t *testing.T) {
	remote := map[string]func(*http.Request){
		"LAN address":           nil,
		"unparseable address":   func(r *http.Request) { r.RemoteAddr = "pipe" },
		"X-Forwarded-For":       local(func(r *http.Request) { r.Header.Set("X-Forwarded-For", "192.168.0.10") }),
		"empty X-Forwarded-For": local(func(r *http.Request) { r.Header["X-Forwarded-For"] = []string{""} }),
		"X-Real-IP":             local(func(r *http.Request) { r.Header.Set("X-Real-IP", "192.168.0.10") }),
		"Forwarded":             local(func(r *http.Request) { r.Header.Set("Forwarded", "for=192.168.0.10") }),
	}
	for name, mod := range remote {
		h, a, _ := newTestHandler(t)
		rec := do(h, http.MethodPost, "/auth/setup", body(pw), mod)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), ErrSetupRemote.Error()) ||
			len(rec.Result().Cookies()) != 0 {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		if req, err := a.SetupRequired(); !req || err != nil {
			t.Errorf("%s: setup required %v err %v", name, req, err)
		}
	}

	h, a, _ := newTestHandler(t)
	if _, err := a.Setup(pw); err != nil {
		t.Fatal(err)
	}
	if rec := do(h, http.MethodPost, "/auth/setup", body(pw), nil); rec.Code != http.StatusForbidden {
		t.Errorf("remote setup with a password set: %d %s", rec.Code, rec.Body.String())
	}

	for _, addr := range []string{"127.0.0.1:5555", "127.8.9.10:5555", "[::1]:5555", "[::ffff:127.0.0.1]:5555"} {
		h, _, _ := newTestHandler(t)
		rec := do(h, http.MethodPost, "/auth/setup", body(pw), func(r *http.Request) { r.RemoteAddr = addr })
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s: %d %s", addr, rec.Code, rec.Body.String())
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 900 go test ./internal/webui/`
Expected: build failure `undefined: ErrSetupRemote`.

- [ ] **Step 3: Write the implementation**

In `internal/webui/auth.go`, add to the error `var` block, after `ErrWrongPassword`:

```go
	ErrSetupRemote    = errors.New("set the first password over SSH: ghr web set-password (or open this page through an SSH tunnel to localhost)")
```

Replace `ClientKey` (lines 328-345, from its doc comment to its closing brace) with:

```go
// remoteAddr parses a request's RemoteAddr. Unmap matters: a RemoteAddr may
// carry an IPv4-mapped address, for which IsLoopback is false.
func remoteAddr(s string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		host = s
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap().WithZone(""), true
}

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

In `internal/webui/handler.go`, make `setup` start with the guard:

```go
func (h *handler) setup(w http.ResponseWriter, r *http.Request) {
	if !fromLoopback(r) {
		h.fail(w, ErrSetupRemote)
		return
	}
	var b passwordBody
```

(the rest of `setup` is unchanged). Add this function directly above `func (h *handler) setup`:

```go
// fromLoopback reports whether r comes straight from this host. A forwarding
// header means a proxy relayed it; a proxy on this host would otherwise make
// every visitor look local.
func fromLoopback(r *http.Request) bool {
	for _, name := range []string{"X-Forwarded-For", "X-Real-IP", "Forwarded"} {
		if len(r.Header.Values(name)) > 0 {
			return false
		}
	}
	ip, ok := remoteAddr(r.RemoteAddr)
	return ok && ip.IsLoopback()
}
```

In `fail`, add this case directly after the `ErrWrongPassword` case:

```go
	case errors.Is(err, ErrSetupRemote):
		writeError(w, http.StatusForbidden, err.Error())
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 900 go test ./internal/webui/ ./internal/daemon/`
Expected: `ok` for both packages (`TestClientKey` and `internal/daemon`'s web listener test, which sets up over `127.0.0.1`, included).

Then run the full Go checks from Global Constraints and `gofmt -l .`; all green, gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/auth.go internal/webui/handler.go internal/webui/handler_test.go
git commit -m "fix(webui): allow first-password setup only locally" -m "A LAN or proxied visitor could claim an unset web password, and a
session can replace the GitHub token, so /auth/setup now answers 403
unless the request comes straight from loopback."
```

### Task 2: Reword the next-visitor messages

**Files:**
- Modify: `cmd/ghr/cli.go:131,294`
- Modify: `internal/daemon/web.go:26`
- Modify: `internal/webui/auth.go:313-314` (the `Reset` doc comment)
- Modify: `web/src/pages/login.tsx:75`
- Test: `cmd/ghr/cli_test.go:232,325`, `internal/daemon/web_test.go:61`, `web/src/pages/login.test.tsx`

**Interfaces:**
- Consumes: none (independent of Task 1's code; runs after it on the same branch)
- Produces: C4

**Items:** 12

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 2 - spec 0 - coupling 0 - risk 0 = 2

- [ ] **Step 1: Update the tests to the new strings**

- `cmd/ghr/cli_test.go:232`: replace `"web password removed; the next visitor to the web UI sets a new one"` with `"web password removed; set a new one with: ghr web set-password"`.
- `cmd/ghr/cli_test.go:325`: `const warning = "warning: the web UI has no password. Run: ghr web set-password\n"`
- `internal/daemon/web_test.go:61`: replace `"web password reset; the next visitor to the web UI sets a new one"` with `"web password removed; set a new one with: ghr web set-password"`.
- `web/src/pages/login.test.tsx`, in the test `sets the first password, checking it first, and opens the dashboard`, directly after the line `const submit = screen.getByRole("button", { name: "Set password" })`, add:

```tsx
    expect(
      screen.getByText("No password is set. Run ghr web set-password over SSH, or set it here from the host itself."),
    ).toBeInTheDocument()
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 900 go test ./cmd/ghr/ ./internal/daemon/` — Expected: FAIL in `TestWebResetPassword`, `TestStatusWarnsWhileTheWebUIHasNoPassword` and the reset-route test in `web_test.go`.
Run, from `web/`: `npm test -- src/pages/login.test.tsx` — Expected: FAIL, unable to find the hint text.

- [ ] **Step 3: Change the strings**

- `cmd/ghr/cli.go:131`: `fmt.Fprintln(out, "web password removed; set a new one with: ghr web set-password")`
- `cmd/ghr/cli.go:294`: `fmt.Fprintln(out, "warning: the web UI has no password. Run: ghr web set-password")`
- `internal/daemon/web.go:26`: `ev.Add("warn", "", "web password removed; set a new one with: ghr web set-password")`
- `internal/webui/auth.go:313-314`, the `Reset` doc comment becomes:

```go
// Reset forgets the password and ends every session. Until a new one is set,
// only a loopback client can set it from the browser.
```

- `web/src/pages/login.tsx:75`: `? "No password is set. Run ghr web set-password over SSH, or set it here from the host itself."`

- [ ] **Step 4: Run the checks**

Run the full Go checks and `gofmt -l .`, then the web checks (`npm test`, `npm run lint`, `npm run build`). Expected: all green.
Then, from the worktree root, search for leftovers:
Run: `grep -rniE "next visitor|first visitor" --include=*.go --include=*.ts --include=*.tsx --include=*.md . | grep -v node_modules`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add cmd/ghr/cli.go cmd/ghr/cli_test.go internal/daemon/web.go internal/daemon/web_test.go internal/webui/auth.go web/src/pages/login.tsx web/src/pages/login.test.tsx
git commit -m "fix: point password messages at set-password"
```

### Task 3: homelab test fix and docs

**Files:**
- Modify: `github-runner/tests/setup_test.sh:29,164-173`
- Modify: `github-runner/setup.sh:208-209` (comment only)
- Modify: `github-runner/README.md:85`

**Interfaces:**
- Consumes: none
- Produces: none

**Items:** 9, 12

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

- [ ] **Step 1: Make the flaky case deterministic**

In `github-runner/tests/setup_test.sh`, case `b-still-starting`, replace

```bash
out=$(run wait_ready 2>&1)
```

(the one directly below `READY_TIMEOUT=5`) with

```bash
# Unset, SECONDS stops tracking real time, so only the stubbed sleep advances it.
out=$(unset SECONDS; SECONDS=0; run wait_ready 2>&1)
```

and replace the last two lines of the case

```bash
# 6 polls at simulated t=0..5; one fewer if a real second boundary passes mid-test.
check "b: polling is bounded by the timeout" between "$(count polls)" 5 6
```

with

```bash
check "b: polling is bounded by the timeout" [ "$(count polls)" -eq 6 ]
```

Delete line 29, `between() { [ "$1" -ge "$2" ] && [ "$1" -le "$3" ]; }`: this case was its only caller.

- [ ] **Step 2: Prove it is no longer timing-sensitive**

Run from `github-runner/`: `timeout 300 bash tests/setup_test.sh | grep -E "^FAIL|polling is bounded"` — Expected: only `ok   b: polling is bounded by the timeout`.

Then run 40 copies in parallel under load:

```bash
cd github-runner && d=$(mktemp -d) && for i in $(seq 40); do (timeout 600 bash tests/setup_test.sh > "$d/st-$i.log" 2>&1; echo "$i rc=$?") & done; wait; grep -l "^FAIL" "$d"/st-*.log; rm -rf "$d"
```

Expected: forty `rc=0` lines and no file listed by `grep -l`.

- [ ] **Step 3: Update the comment and the README**

`github-runner/setup.sh:208-209` becomes:

```bash
# A first install turns the web UI on (config.example.yaml); until a password
# is set, its setup page answers only from this host.
```

In `github-runner/README.md` line 85, replace

```text
Until then, the first visitor to reach the port sets it from the browser. `ghr web reset-password` deletes the password and logs every browser out, which reopens that window, so set a new one right after running it.
```

with

```text
Until then the browser setup page answers only from the host itself, for example through `ssh -L 8080:localhost:8080 root@<lxc>` and http://localhost:8080. `ghr web reset-password` deletes the password and logs every browser out, which reopens that loopback-only setup page, so set a new one right after running it.
```

- [ ] **Step 4: Lint**

Run from `github-runner/`: `bash -n setup.sh && bash -n tests/setup_test.sh`
Expected: no output.
Run: `timeout 300 uvx --from shellcheck-py shellcheck setup.sh tests/setup_test.sh`
Expected: no findings. If `uvx` cannot fetch shellcheck-py, record that in the task report and continue; this verifies the shellcheck Assumption either way. Report every finding; fix only findings in lines this task changed, and list the rest for the owner.

- [ ] **Step 5: Commit**

```bash
git add github-runner/tests/setup_test.sh github-runner/setup.sh github-runner/README.md
git commit -m "fix(github-runner): freeze SECONDS in readiness test" -m "Bash's SECONDS kept counting real time while the stubbed sleep added
to it, so a slow run polled one time fewer. The README and a setup.sh
comment now say the setup page answers only from the host itself."
```

### Task 4: Rollout v0.1.15

**Superseded:** by docs/superpowers/specs/2026-10-07-ghr-first-run-setup-design.md; the guard is reverted and the rollout continues as Task 14 of docs/superpowers/plans/2026-10-07-ghr-first-run-setup.md.

**Files:**
- Modify: `docs/superpowers/registers/2026-10-07-ghr-tui-retirement.md` (homelab master, row 8 state)

**Interfaces:**
- Consumes: C1, C4 (asserted live)
- Produces: none

**Items:** 8

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 0 - spec 1 - coupling 1 - risk 2 = 4

Run by the main session, not a subagent: steps 2 and 5 need the owner. Each step that changes the LXC or the ghr remote waits for the owner's explicit yes.

- [ ] **Step 1: Pre-flight**

Confirm ghr master and homelab master hold Tasks 1–3 (`git log --oneline -5` in each) and that `git -C D:/Repositories/Personal/ghr log --oneline origin/master..master` lists the TUI-retirement commits followed by Tasks 1–2's two commits on top.

- [ ] **Step 2: Owner pushes ghr master; CI releases v0.1.15**

Ask the owner to run `git -C D:/Repositories/Personal/ghr push origin master`. Then wait for CI: `gh run list -R darkraise/ghr -L 3` until the newest run on master completes, then `gh release view v0.1.15 -R darkraise/ghr`.
Expected: the run concluded `success` and release `v0.1.15` exists. A red run stops the rollout; report it.

- [ ] **Step 3: Deploy**

```bash
ssh -i ~/.ssh/ghr_lxc root@192.168.0.99 'ghr status'
```

Wait until no runner is busy. Then:

```bash
scp -i ~/.ssh/ghr_lxc D:/Repositories/Personal/homelab/github-runner/setup.sh D:/Repositories/Personal/homelab/github-runner/config.example.yaml root@192.168.0.99:/root/github-runner/
ssh -i ~/.ssh/ghr_lxc root@192.168.0.99 'cd /root/github-runner && GHR_VERSION=v0.1.15 timeout 1800 bash setup.sh; echo rc=$?; ghr version'
```

Expected: `rc=0` and `ghr version` prints `v0.1.15`. Then over `ssh -t`, run `ghr` with no arguments: it prints status, without the warning line (a password is set).

- [ ] **Step 4: Live password checks (spec §8)**

Every `/auth` POST below is this command, with `<url>` and `<pw>` filled in (throwaways come from `openssl rand -hex 12`; never print the owner's password file):

```bash
curl -s -m 10 -w '
%{http_code}
' -X POST -H 'X-GHR: 1' -H 'Content-Type: application/json' -d '{"password":"<pw>"}' <url>
```

The last output line is the status code; the line before it is the body.

1. On the LXC: `cp -p /etc/ghr/web-password /root/web-password.keep`. Keep it until item 6.
2. On the LXC: `printf '%s' '<throwaway A>' | ghr web set-password` prints `web password set; every browser was logged out`; `POST http://127.0.0.1:8080/auth/login` with throwaway A gets 204.
3. On the LXC: `ghr web reset-password` prints `web password removed; set a new one with: ghr web set-password`, and `ghr status` then prints the first line `warning: the web UI has no password. Run: ghr web set-password`. From the dev box: `POST http://192.168.0.99:8080/auth/setup` gets 403 with a body containing `set the first password over SSH`. `POST https://ghr.quangtc.dev/auth/setup` gets the same, or a refusal from nginx-proxy-manager itself (an access list answers 401 or 403 with its own body); record which. Either way `GET http://192.168.0.99:8080/auth/state` still reports `"setup_required":true`. On the LXC: the same POST to `http://127.0.0.1:8080/auth/setup` gets 204.
4. On the LXC: `ghr web reset-password` again. Ask the owner to run, from their own terminal: `ssh -t -i ~/.ssh/ghr_lxc root@192.168.0.99 "cd /root/github-runner && env -u GHR_WEB_PASSWORD bash -c '. ./setup.sh; FIRST_INSTALL=1; set_web_password'"` and type a throwaway containing a space.
5. Ask the owner to log in with that throwaway at `http://192.168.0.99:8080` and confirm it worked. While logged in, the owner checks the four web additions (status chips on every page, Dashboard row actions, Runners log preview, Copy URL).
6. On the LXC: `install -m 0600 /root/web-password.keep /etc/ghr/web-password && systemctl restart ghr && rm /root/web-password.keep`; then `GET http://192.168.0.99:8080/auth/state` reports `"setup_required":false`.

If `/auth/state` reported `setup_required:true` before item 1, skip items 1 and 6, and end with the owner running `ghr web set-password` with a password they choose.

- [ ] **Step 5: Close the register rows**

In homelab master:

```bash
R=D:/Repositories/Personal/darkraise-ai-plugins/plugins/dr-superpowers/scripts
REG=docs/superpowers/registers/2026-10-07-ghr-tui-retirement.md
bash $R/register set $REG 8 done --note "<CI run id, release, setup.sh rc, live checks passed>"
bash $R/register set $REG 9 done --note "b-still-starting freezes SECONDS; 40 parallel runs green"
bash $R/register set $REG 12 done --note "owner chose loopback-only setup 2026-10-07; TestSetupOnlyFromLoopback; live 403 from LAN, 204 from loopback"
git add docs/superpowers/registers/2026-10-07-ghr-tui-retirement.md
git commit -m "docs(registers): close ghr v0.1.15 rollout rows"
```

Expected: `scripts/register check` on the file passes.
