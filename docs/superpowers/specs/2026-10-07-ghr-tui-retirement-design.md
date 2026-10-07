# ghr TUI retirement

**Date:** 2026-10-07
**Status:** approved in chat (2026-10-07); revised the same day after an independent review (Fable, 18 findings: 17 accepted, the live-check half of #16 declined; the owner chose to close the four web UI deviations and to prompt for the web password in setup.sh): docs/superpowers/notes/2026-10-07-ghr-tui-retirement-fable-review.md; amended the same day after implementation (register rows 9 and 12): first-visitor setup is limited to loopback (§2), and a timing-sensitive setup.sh test is fixed (§7); the amendment revised after a second Fable review (11 findings: 10 accepted, the setup-state UX nit declined): docs/superpowers/notes/2026-10-07-ghr-web-setup-loopback-fable-review.md; the loopback-only first-visitor rule was withdrawn the same day by docs/superpowers/specs/2026-10-07-ghr-first-run-setup-design.md (owner ruling: the first visitor may claim the password)
**Register:** docs/superpowers/registers/2026-10-07-ghr-tui-retirement.md
**Code:** github.com/darkraise/ghr (delete `internal/tui`; changes in `cmd/ghr`, `internal/webui`, `internal/daemon`, `internal/model`, `web/src`, `go.mod`); homelab `github-runner/`
**Builds on:** docs/superpowers/specs/2026-10-05-ghr-web-ui-design.md (the web UI's pages; §5 here adds to them)

## Goal

Delete the terminal UI and keep the CLI, so every interactive feature is built once, in the web UI. Close the four places where the web UI still falls short of the TUI. Close the gap a fresh install opens, where the web UI's password is claimed by its first visitor, by setting the password from SSH: during setup.sh, or later with a new CLI command; the browser setup page remains as a fallback.

## Decisions

- **The web UI already offers every TUI action; four presentation gaps remain and are closed here (§5).** The TUI reaches the daemon only through the 35-method `tui.Client` interface (`internal/tui/model.go:20-56`), and `web/src/api/client.ts` calls every endpoint behind those methods. Every client-side derivation the TUI made has a web counterpart: label classification and quick-add (`manage.go` → `web/src/lib/labels.ts`), activity statistics (`manage.go` → `lib/activity.ts`), effective labels (`repos.go` → `pages/repository.tsx`), the last .NET major warning (`storage.go` → `lib/toolchains.ts`), disk warn/critical and the runner-update badge (`shell.go` → `pages/dashboard.tsx`, `components/maintenance-card.tsx`). The four gaps, found in review: Dashboard row actions, the Runners log preview, status chips on every page, and copying a run's URL. The keyboard shortcuts and help overlay are not ported; the web UI uses buttons.
- **The CLI is kept as it is,** apart from `ghr web set-password` and the status warning line (§2, §3). Commands that exist only in the web UI (token status, the runner registration list, the label check, runner steps and containers) are not added. Config reload stays `systemctl reload ghr` (SIGHUP).
- **The Go API client keeps all its methods.** Twelve are no longer called from `cmd/`, but `internal/api/api_test.go`, `internal/api/update_test.go` and `internal/daemon/run_test.go` use them to test the daemon routes the web UI depends on.
- **The daemon sets the password, over the Unix socket**, as `ghr web reset-password` already does (`internal/daemon/web.go`). The CLI writing `/etc/ghr/web-password` itself was rejected: it would race with `Auth.fileMu`, and sessions started under the old password would stay valid.
- **Bare `ghr` in a terminal runs `ghr status`.** Scripts and pipes keep today's behaviour.
- **The first-visitor setup page stays** as a fallback (owner's choice). A loopback-only rule added the same day was withdrawn by docs/superpowers/specs/2026-10-07-ghr-first-run-setup-design.md.
- **Specs, plans and registers that mention the TUI are historical records** and are not edited.

## 1. Removing the TUI

- Delete `internal/tui` (all files, the `ui` subpackage and `testdata`).
- `cmd/ghr/main.go`:
  - Drop the `tui` import, `runTUI`, `openTUI` and the `case "tui"`.
  - With no arguments: when `isTerminal()` (stdin and stdout both terminals) holds, run the CLI with the arguments `["status"]` and return its result as for any other command (exit 1 with `ghr: ...` when the daemon is unreachable); otherwise print usage to stderr and exit 2, as today.
  - `ghr tui` falls through to the CLI, which reports `unknown command tui` with usage and exit 2.
  - Usage: remove the `(no command)` dashboard line and the `tui` line; add `(no command)  same as status, in a terminal` and `web set-password  set the web UI password (prompts, or reads stdin)`, next to `web reset-password`.
- Terminal detection moves from `github.com/charmbracelet/x/term` to `golang.org/x/term`. `fdIsTerminal` keeps its `func(uintptr) bool` type and wraps `term.IsTerminal(int(fd))`, so `TestStdioIsTerminalNeedsBoth` stands unchanged.
- `go.mod`: remove the direct requirements `charmbracelet/bubbles`, `bubbletea`, `lipgloss`, `x/ansi`, `x/exp/golden`, `x/term`, `lrstanley/bubblezone` and `muesli/termenv`, all used only under `internal/tui`; add `golang.org/x/term`; `go mod tidy` drops their indirect dependencies.
- Comments and test names that point at the TUI are reworded to state the behaviour itself:
  - `internal/daemon/labelcheck.go:34`: classification happens in the web UI.
  - `internal/daemon/backend.go:141`: "however many browser tabs poll its detail view".
  - `web/src/query.ts:11`, `web/src/api/hooks.ts:122`, `web/src/components/shell.tsx:31`, `web/src/components/toolchains-card.tsx:16`.
  - Test names in `web/src/lib/format.test.ts:25`, `web/src/lib/duration.test.ts:28`, `web/src/components/components.test.tsx:18`.
  - Acceptance: a case-insensitive whole-word search of the ghr tree for `TUI` and for `internal/tui`, excluding `web/node_modules` and `cmd/ghr/main_test.go` (whose test proves `ghr tui` is gone), returns nothing.

## 2. Setting the web password from SSH

### Auth

`internal/webui/auth.go` gains `func (a *Auth) Set(password string) error`:

- Returns `ErrPasswordLength` unless `validLength(password)`, without touching the file.
- Takes `fileMu`, derives a new hash through `derive` (lock order `fileMu` → `sem` → `mu`, as `Setup` and `ChangePassword`), and writes it with `writeHash` (temporary file and rename), whether or not a password exists. It never reads the old file, so it also replaces a corrupt one.
- While still holding `fileMu`, takes `mu`, increments `gen` and clears `sessions`, as `Reset` does. A login that minted a session from the old hash before this point loses it to the clear; one that finishes after it is refused by its `gen` check.
- The login throttle (`fails`) is left as it is.
- `ErrUnreadable` (`internal/webui/password.go:35`) changes its advice from `ghr web reset-password` to `ghr web set-password`, which repairs the file without reopening the first-visitor window.

### Socket route

`socketHandler` (`internal/daemon/web.go`) gains `POST /web/set-password`, served only on the Unix socket like `/web/reset-password` (the web listener mounts the API handler alone, `run.go:242`):

- Body: JSON `{"password": "<string>"}`, read with `http.MaxBytesReader` capped at 16 KiB of encoded body (JSON escaping can grow a 1024-byte password past 6 KiB), then `io.ReadAll` and `json.Unmarshal`, as `webui.decodeBody` does, so trailing data is rejected. Any read or decode failure is 400 `{"error": "invalid request body"}`.
- `ErrPasswordLength` is 400 with its message (`password must be 12 to 1024 bytes`); any other error is 500 with its message, in the reset route's JSON error shape.
- Success is 204 and adds an `info` event: `web password set from the command line; every browser was logged out`.

### Client and CLI

- `internal/api/client.go` gains `SetWebPassword(ctx, password string) error`, posting the JSON body to `/web/set-password`.
- `cli` (`cmd/ghr/cli.go:54`) gains a `stderr io.Writer` parameter after `out`; `run` passes its own stderr, and the test helper `runCLI` is updated.
- `case "web"` accepts `reset-password` (unchanged) or `set-password`; anything else is `usage: ghr web <reset-password|set-password>`.
- `set-password` reads the password one of two ways:
  - **stdin is a terminal** (a new stdin-only check, `stdinIsTerminal`, a package variable over `fdIsTerminal(os.Stdin.Fd())`): write `New web password: ` to stderr, read with the package variable `readPassword` (defaulting to `term.ReadPassword` on stdin's descriptor), write a newline to stderr, then `Repeat: ` and read again. After each read, a read error is returned as is, and `ctx.Err()` is returned if set, so a Ctrl-C during the prompt aborts after the next Enter without sending anything. Different entries fail with `passwords do not match`.
  - **otherwise:** read stdin up to 4 KiB and strip exactly one trailing `\n` or `\r\n`; other whitespace is kept.
- Length is checked by the daemon only, so the CLI cannot drift from it; its message is printed as `ghr: password must be 12 to 1024 bytes` with exit 1.
- On success print `web password set; every browser was logged out` to stdout.

## 3. Warning while the web UI has no password

- `model.Status` gains `WebSetupRequired bool` (`json:"web_setup_required"`): true when the web listener is running and `auth.SetupRequired()` reports no password. `daemon.Run` gives the backend a function that computes it; a read error of the password file counts as false here (the existing start-up event already reports an unreadable file).
- At start-up, when the listener is up and no password is set, `daemon.Run` adds a `warn` event: `web UI on <addr> has no password; run: ghr web set-password`.
- `printStatus` (`cmd/ghr/cli.go:277`) prints `warning: the web UI has no password. Run: ghr web set-password` as its first line when the field is true. Because bare `ghr` and setup.sh's `wait_ready` both print status, the owner sees it on every install and every time they type `ghr`.
- The web UI ignores the field (a logged-in browser can only see it false). `web/src/api/types.ts` gains it for type completeness.

## 4. Homelab `github-runner/`

### config.example.yaml

Uncomment `web:` and `listen: 0.0.0.0:8080`, with the block comment `# browser UI; setup.sh asks for its password, or run: ghr web set-password`. `hosts` stays commented out. Only fresh installs see this: setup.sh copies the example only when `/etc/ghr/config.yaml` is absent (`setup.sh:145-147`).

### setup.sh

- `install_config` sets `FIRST_INSTALL=1` when it writes the config.
- A new step `set_web_password`, run in `main` right after `wait_ready`, acts only when `FIRST_INSTALL=1`:
  - Password source: `GHR_WEB_PASSWORD` if set; otherwise, when stdin is a terminal, `read -rsp "Web UI password (12 to 1024 bytes; Enter to skip): "`; otherwise none.
  - A non-empty password is piped to `ghr web set-password` with `printf '%s'`. On failure (for example too short) it warns with ghr's message and continues: the install itself succeeded.
  - With no password, or after a failure, it logs `web UI on :8080 has no password; set it now with: ghr web set-password`.
- The header comment documents `GHR_WEB_PASSWORD` next to `GHR_TOKEN`.

### README.md

- Use section: remove the `ghr tui` line, the Shift-drag/tmux paragraph, and "or TUI" from the config-rewrite sentence; add `ghr` (status, in a terminal) and `ghr web set-password` to the command block.
- Web UI section:
  - Drop "with the same pages and actions as `ghr tui`" and "It stays off until `web.listen` is set"; say the example config turns it on at `0.0.0.0:8080` and that a first install asks for the password (`GHR_WEB_PASSWORD` for scripts).
  - Step 1: if the install skipped the password, run `ghr web set-password` before adding a proxy; `ghr status` warns until a password is set.
  - Keep the first-visitor setup page and `ghr web reset-password` as the fallback, and say the setup page works only from the host itself (for example through `ssh -L 8080:localhost:8080 root@<lxc>`); replace the sentence that the first visitor to reach the port sets the password, and say `ghr web reset-password` reopens the loopback-only setup page rather than "that window".
  - Point 4 gains: change `listen` to a LAN-only address if the LXC has more than one network.
  - Upgrading an older config still means adding the `web:` block by hand and restarting.

## 5. Web UI additions

### 5.1 Status chips on every page

`StatusChips` moves from `pages/dashboard.tsx` into its own component, rendered by `Shell` (`components/shell.tsx`) above the `Outlet`, after the reconnect and degraded alerts, whenever status data exists. Its content is unchanged: mode, runners, API remaining, disk (coloured by `disk_high_water` from the config, which `Shell` already polls), degraded, and the runner-update badge. The Dashboard stops rendering its own copy.

### 5.2 Dashboard actions

- **Scheduling controls**, next to Pause all / Resume all:
  - A mode toggle labelled with the mode it switches to (`Switch to ALL` / `Switch to QUEUE`; the current mode shows in the status chips), which sends `PATCH /config {mode}` with that mode.
  - A global max stepper (− value +). Each press sends `PATCH /config {global_max: n±1}`; − is disabled at 1. It stays enabled in `all` mode, with the hint "applies in queue mode".
- **Repositories card:**
  - An `Add repository` button in the card header opens the existing `AddRepoDialog`. The empty-state text becomes "no repos configured" with the same button.
  - Each row gains an actions cell:
    - a repo max stepper sending `PATCH /config {repos: {<name>: {max: n±1}}}`, disabled when max is 0 (unlimited, titled "unlimited: change it on the repository page") and − disabled at 1, as the TUI's `repoCap`;
    - `Edit`, a link to `/repositories/<name>`;
    - `Pause` / `Resume`;
    - `Remove`, which opens the confirmation `Remove repo <name>? Its running jobs finish first.`
  - The pause, resume and remove handling (calls, toasts, confirmation) is extracted from `pages/repositories.tsx` into a shared component used by both pages, so the two cannot drift.
- **Runners card:** `RunnersTable` with `actions` on (Logs, Copy ID, Stop), as on the Runners page.
- Every repo and scheduling action is disabled while the daemon is unreachable; the runner row actions behave exactly as on the Runners page. Failures show the same error toasts as the same action elsewhere. Each successful change invalidates status (and config for `PATCH /config`).

### 5.3 Runners page log preview

- Rows of the Runners table become selectable by click and by arrow keys on the focused table, with `aria-selected` on the selected row. While nothing is selected and runners exist (the page has just opened, or the first runner has just appeared), the first row is selected. A selected runner that leaves the list stays selected, shown as ended below, with no row highlighted, until the owner selects another; the down arrow then selects the first row. Clicks on the row's links and action buttons do not change the selection.
- Below the table, a card `Log preview — <id> (following)` shows the selected runner's log through `useLogTail(id, true)` and `LogView` with follow on. It is 16 lines tall, and the table above it scrolls.
- When the previewed runner leaves the instance list, its last text stays and the title becomes `Log preview — <id> (ended)`, no longer polled, until the owner selects another runner. With no runners the card reads `no runner selected`.
- The Dashboard's runners card has no preview, as in the TUI.

### 5.4 Copy run URL

History rows and the runner detail header gain a `Copy URL` button beside `Open run`, shown when `html_url` exists. It calls `copyText(html_url)` and shows the same "copied" toast as Copy ID.

## 6. Error handling

- A newer CLI against a daemon that has not restarted onto the new binary gets the client's existing message: `the running ghr daemon does not know POST /web/set-password; it is older than this ghr, restart it: systemctl restart ghr`. setup.sh restarts the daemon on every upgrade.
- A daemon that cannot write the password file returns 500 with the OS error; the old file is untouched because `writeHash` writes a temporary file and renames it.
- A web action that fails (for example a `PATCH /config` refused by validation) shows the daemon's error in a toast and leaves the displayed values to the next poll.

## 7. Testing

- **Auth** (`internal/webui`): `Set` sets a password when none exists; replaces an existing one, so the old password fails and the new one logs in; ends existing sessions; replaces a corrupt file; rejects 11- and 1025-byte passwords without touching the file.
- **Socket route** (`internal/daemon`): 204 and the event on success; 400 for a body that does not decode, for trailing data, for a body over 16 KiB, and for a bad length; a 1024-byte password of `<` characters is accepted; the route is unreachable through the web handler (template: `TestResetRouteIsSocketOnly`).
- **Status** (`internal/daemon`): `web_setup_required` is true with a listener and no password, false after `Set`, false without a listener; the start-up `warn` event.
- **Loopback setup** (`internal/webui`): setup from `192.168.0.10` is 403 with `ErrSetupRemote`'s message and leaves `setup_required` true; from `127.0.0.1`, `::1` and `::ffff:127.0.0.1` it is 204; from `127.0.0.1` with `X-Forwarded-For` or with `Forwarded` it is 403. An unparseable `RemoteAddr` is 403. Handler tests that run setup through the `do` helper's default `192.168.0.10` (`internal/webui/handler_test.go:43`) switch to a loopback address: `TestSetupThenAPI` (whose second setup must still get 409, which only a loopback client reaches), `TestLoginResponses`, `TestSecureCookie` and `TestUnreadablePasswordFile` (whose 500 likewise needs a loopback client). The daemon integration tests already reach the listener over `127.0.0.1`.
- **Wording tests**: `cmd/ghr/cli_test.go:325` (status warning), `cmd/ghr/cli_test.go:232` (reset-password output) and `internal/daemon/web_test.go:61` (reset event) pin the new texts.
- **Client** (`internal/api`): `SetWebPassword` posts the JSON body to `/web/set-password`.
- **CLI** (`cmd/ghr`):
  - Piped input strips one trailing newline and keeps other whitespace.
  - The terminal path with matching and mismatched entries, prompts asserted on stderr (replaced `stdinIsTerminal` and `readPassword`); a cancelled context after a read sends nothing.
  - `web` with an unknown subcommand is a usage error; usage contains `web set-password` and no longer `tui` (extend `TestWebResetPassword`).
  - `printStatus` prints the warning line only when the field is true.
- **main** (`cmd/ghr`): bare `ghr` with terminals runs status against its own fake daemon (`fakeDaemon` does not restore `newClient`); bare `ghr` with terminals and an unreachable daemon exits 1; without terminals prints usage and exits 2; `ghr tui` is an unknown command.
- **Web** (vitest):
  - The chips render on a non-Dashboard page.
  - Dashboard: the mode toggle and both steppers send the right patches; the bounds and the unlimited case disable them; Add opens the dialog; Remove confirms before sending.
  - Runners preview: the first row is selected; clicking another row switches the preview; an action-button click does not; a runner that leaves shows `(ended)` and stops polling.
  - Copy URL calls the clipboard with `html_url` on both pages.
  - The setup page shows the new hint.
- `go build ./... && go vet ./... && go test ./...` on the dev box; CI runs `go test -race` on Linux. Web: `npm test`, `npm run lint` (already `--max-warnings 0`), `npm run build`.
- setup.sh: `bash -n setup.sh` and `shellcheck` if available; the prompt path is exercised during rollout only if the owner reinstalls, so its logic stays a few lines.
- `tests/setup_test.sh` case `b-still-starting` (register row 9): Bash's `SECONDS` keeps counting real time after the stubbed `sleep` adds to it, so a real second passing during the loop cut the poll count, about 1 run in 20 under load. The case runs `wait_ready` in a subshell that first runs `unset SECONDS; SECONDS=0`, which makes `SECONDS` an ordinary variable that only the stubbed `sleep` advances, and asserts exactly 6 polls: `deadline` is 5, and the loop polls at simulated t=0 to 5 before `die`. The comment about a real second boundary goes with the old `between 5 6` assertion. setup.sh is unchanged.

## 8. Rollout

- Release v0.1.15 after the owner pushes ghr master (the agent cannot push); CI tags it.
- Run `GHR_VERSION=v0.1.15 bash setup.sh` on 192.168.0.99 while no jobs run: a Docker upgrade during setup.sh restarts the Docker daemon, which stops job containers. (Stopping ghr itself does not stop runners; they are separate units.)
- Check that `ghr` with no arguments over SSH prints status, with the warning line while no password is set.
- Live checks of the password paths, as one sequence on the LXC so the owner's password is restored exactly once. Every `/auth` POST below sends `-H 'X-GHR: 1'` and a JSON body `{"password":"<throwaway of 12+ bytes>"}`, because `checks` refuses an `/auth` POST without the header with its own 403.
  1. Copy `/etc/ghr/web-password` aside with `cp -p`, and keep the copy until step 6.
  2. Pipe a throwaway to `ghr web set-password`, then log in over HTTP with it.
  3. Run `ghr web reset-password`. From the dev box, `POST http://192.168.0.99:8080/auth/setup` gets 403 with `ErrSetupRemote`'s message, and `/auth/state` still reports `setup_required:true`. On the LXC, the same request to `http://127.0.0.1:8080/auth/setup` gets 204.
  4. Run `ghr web reset-password` again. The owner runs the prompt step by hand over `ssh -t`, from the directory setup.sh was copied to for this rollout: `env -u GHR_WEB_PASSWORD bash -c '. ./setup.sh; FIRST_INSTALL=1; set_web_password'`. Sourcing does not run `main`, and it resets `FIRST_INSTALL` to 0 (`setup.sh:27`), so the assignment comes after it. The owner types a throwaway containing a space.
  5. The owner logs in with that throwaway from the browser.
  6. Restore the file with `install -m 0600`, run `systemctl restart ghr` (the restart resets the in-memory sessions and `gen`), and confirm `/auth/state` reports `setup_required:false`.
  If the owner has not set a password yet, skip steps 1 and 6 and end with `ghr web set-password` and a password the owner chooses.
- Check the four web additions in the browser over `http://192.168.0.99:8080`.

## Out of scope

- New CLI commands for features only the web UI has.
- Removing the first-visitor setup page (it is limited to loopback instead).
- Porting the TUI's keyboard shortcuts or help overlay.
- Editing earlier specs, plans or registers that mention the TUI.
