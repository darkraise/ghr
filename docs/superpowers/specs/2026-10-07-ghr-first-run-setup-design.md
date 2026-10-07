# ghr first-run setup from the CLI or the web UI

**Date:** 2026-10-07
**Status:** approved in chat, section by section (2026-10-07); revised the same day after an independent review (Fable, 28 findings, all accepted): docs/superpowers/notes/2026-10-07-ghr-first-run-setup-fable-review.md; amended while planning: v0.1.16 instead of v0.1.15, the Repositories step reuses the Add repository dialog, a failed setup-state read does not redirect
**Register:** docs/superpowers/registers/2026-10-07-ghr-first-run-setup.md
**Code:** github.com/darkraise/ghr (`cmd/ghr`, `internal/config`, `internal/daemon`, `internal/runner`, `internal/model`, `internal/api`, `internal/webui`, `web/src`); homelab `github-runner/`
**Supersedes:** the loopback-only first-visitor setup in docs/superpowers/specs/2026-10-07-ghr-tui-retirement-design.md §2, and Tasks 1 and 4 of docs/superpowers/plans/2026-10-07-ghr-web-setup-loopback.md

## Goal

A fresh install can be finished from either side. setup.sh (or the new `ghr setup` command) can supply the GitHub owner, the token and the web password; whatever it skips, a first-run wizard in the web UI completes: claim the password, enter owner and token, pick repos, adjust every setting, choose toolchains. The daemon therefore starts and serves before it has an owner or a token.

## Decisions

- **The first web visitor may claim the password** (owner's ruling, register row 2: only the owner who sets up the runner host reaches the web UI first). The loopback-only guard (ghr `e87935d`) is reverted. The first visitor can then also set the owner and token; the ruling accepts that. On the web port the setup routes sit under `/api/`, behind `requireSession` (`internal/webui/handler.go:41`); on the socket they need no session, as every socket route, because the socket is mode 0600 (`internal/daemon/run.go:98`).
- **Two-phase start, no restart** (owner's choice of approach A). Unconfigured, the daemon serves a setup API; once owner and token are valid it runs today's start-up path in the same process and swaps the HTTP handler. Restarting after setup (B) was rejected because sessions live in memory and the owner would log in again mid-wizard; making the owner changeable at any time (C) was rejected because live runners belong to the owner the daemon started with.
- **The wizard covers everything** (owner's choice): password, GitHub, repositories, every setting, toolchains.
- **setup.sh prompts and any prompt can be skipped** (owner's choice). Settings, repos and toolchains stay on the existing CLI commands; `ghr setup` adds only what the CLI cannot do today: set owner and token on an unconfigured daemon, show the setup state, and end the wizard.
- **One writer.** Owner and token are validated and written by the daemon, whichever side supplies them; setup.sh no longer writes the token file.
- **The setup.sh password prompt stays optional** (register row 3), as built in the TUI-retirement work.

## 1. Setup state

- **Store configured:** `config.yaml` has a non-empty `owner` and `/etc/ghr/token` holds a non-empty token after trimming. `Store.Configured() bool` reports it.
- **Serving:** the full API is in the swap handler (§3). Clients wait for this, not for the store: between the two, the start-up path runs (manager `Init`, `Adopt`, `Reconcile`, storage), which takes seconds.
- **Wizard pending:** `/var/lib/ghr/setup-pending` exists. setup.sh creates it on a first install; `POST /setup/finish` deletes it. Existing installs never have it, so an upgrade shows no wizard.
- **Toolchains pending:** the existing `/var/lib/ghr/toolchains-pending`, created by setup.sh on a first install.
- Both markers are read with `os.Stat` on every request that reports them; nothing caches them.
- `daemon.Options` gains `SetupPendingPath` and `ToolchainsPendingPath` (defaults above), so tests use temporary paths.

## 2. Config and store

- `config.example.yaml`: `owner: ""` with the comment `# GitHub user or org; set by setup.sh, ghr setup github, or the web UI`, and `repos: []`. The owner's three repos are removed; the other defaults stay. The `web:` comment becomes `# browser UI; the first visitor sets its password (setup.sh can set it first)`.
- `config.Validate` no longer reports `owner is required` (`internal/config/config.go:282-284`). Every other rule is unchanged.
- `OpenStore` and `Store.Reload` accept a missing token file, or one that is empty after trimming, while no token has been loaded yet (`Token()` returns `""`). Once a token is loaded, a reload that finds it missing or empty keeps today's error and the previous values.
- The `owner changed` guard in `Reload` (`internal/daemon/store.go:51-53`) applies only when the current owner is non-empty, so a reload may set the first owner but never change one.
- `Store.Configure(owner, token string) error` holds `s.mu` once for the whole operation, using unlocked internals of `SetToken` and `Update`:
  1. Already configured → `ErrConfigured` (route: 409).
  2. The config names a different owner (case-insensitive) → `ErrOwnerMismatch` carrying it (route: 400 `config.yaml names owner <owner>; edit config.yaml to change it`).
  3. Write the token (temporary file, 0600, rename, as `SetToken`).
  4. Set the owner, validate and save the config (as `Update`). If this fails the store keeps the new token but stays unconfigured; a retry overwrites the token. A token write failure leaves both untouched.
  Holding the lock across check and writes is what keeps a concurrent SIGHUP reload from configuring a different owner in between.

## 3. Daemon

### Start-up order

`Run` (`internal/daemon/run.go:121`) becomes:

1. `OpenStore`, bind the socket and the web listener, build `Auth` and the event ring, as today.
2. Mint the status epoch here (today `Manager.Init`, `internal/runner/manager.go:199`) and pass it to the manager through a new `Manager.Epoch` field that `Init` uses when set, so both phases report one epoch. `model.Status`'s epoch comment becomes: changes on every daemon start; event sequence numbers restart with it.
3. Register `signal.Notify` for SIGHUP (or use `o.Reload`) before any wait, not after start-up as today (`run.go:270-276`).
4. Build one `swapHandler` (`atomic.Pointer[http.Handler]` behind `ServeHTTP`) and pass it where `apiHandler` goes today: to `socketHandler` and to `webui.Handler`. The socket-only password routes and the web `/auth/*` routes stay outside it, so they work in both phases and sessions survive the swap.
5. Start serving both listeners and set `served = true`. From here every exit from `Run` goes through one `teardown` that shuts down both servers (today's 5-second `Shutdown`, then `Close`), so no path returns with a listener still bound.
6. Store configured: go to step 8.
7. Unconfigured: put the setup-phase handler in the swap handler; add the `warn` event `ghr is not configured; finish setup at http://<this host>:<port>/setup or run: ghr setup github --owner <owner>` (port from the applied web listener; without the URL part when the web UI is off). Then wait in a `select` on:
   - ctx cancelled → `teardown`, return nil. Nothing else exists to close.
   - the `ready` channel, closed by a `sync.Once` when `Store.Configure` succeeds → step 8.
   - SIGHUP → `Store.Reload`. An error adds `reload rejected, keeping previous config: <err>`; success adds `config and token reloaded`; when `Store.Configured()` now holds, close `ready` through the same `sync.Once` → step 8. A successful reload that does not configure keeps its config.
8. Run today's start-up path from `owner := store.Config().Owner` (`run.go:161`) to `api.NewServer`, reading the owner once, after configuration. Then put the full handler in the swap handler and add the events `ghr configured for owner <owner>; starting` (only when step 7 ran) and today's `ghr daemon started (owner %s, mode %s)`. A failure on this path runs `teardown` and returns the error; systemd restarts the daemon, which then starts configured and serves the full API.
9. The run loop and its ctx branch are today's, with `teardown` in place of the inline server shutdown.

### Setup routes

A daemon-level `setupRoutes(next http.Handler, s *setup) http.Handler` mounts in front of the API in both phases, so `api.Backend` (`internal/api/server.go:56-90`) does not change. `setup` holds the store, the `ready` once, the event ring, `auth`, `o.GitHubURL`, the two marker paths, the applied web listen address, and an `atomic.Pointer` to the backend that step 8 sets just before the swap; a non-nil backend is what `configured` (serving) reports, and `POST /setup/finish` calls its `InstallPreset`. In the setup phase `next` is the setup-phase fallback below; in the serving phase it is `api.NewServer(b)`.

- `GET /setup` → `model.SetupState` (JSON): `configured` (serving, §1), `starting` (store configured, not yet serving), `setup_pending`, `toolchains_pending`, `owner` (config's owner as it stands, possibly set while the token is missing), `web_listen` (the applied listener's address, empty when the web UI is off).
- `POST /setup/github`, body `{"owner": "<string>", "token": "<string>"}`, read with a 16 KiB `MaxBytesReader` and `json.Unmarshal` (trailing data rejected; failure 400 `invalid request body`):
  - `owner` empty and config names one → use config's. Then `owner` must match a GitHub login (1 to 39 letters, digits or single hyphens, not starting or ending with a hyphen), else 400 `owner must be a GitHub user or organisation name`. `token` trimmed must be non-empty, else 400 `token is required`.
  - Store already configured → 409 `already configured; the owner changes only by editing config.yaml and restarting ghr, and the token with: ghr token set`.
  - Validation uses a client built with the candidate owner and token (`github.New`, `o.GitHubURL` as `BaseURL`): `ListUserRepos`, keeping repos whose owner login matches case-insensitively; none → 400 `the token cannot see any repository of <owner>; grant it access to at least one`. Then `checkToken` on the first match in case-insensitive name order. `ListUserRepos` walks every page; for a classic PAT that is every repo the user can reach, which costs one request per 100 repos. Fine-grained PATs, which the README asks for, list only the selected repos.
  - GitHub errors, by `github` kind (`internal/github/client.go:315-354`), mapped here rather than through `api.FromGitHub`: `ErrAuth` → 400 `token rejected by GitHub: <message>`; `ErrNotFound` from `checkToken` → 400 `the token cannot see <owner>/<repo>`; `ErrRateLimit` → 429 with `retry_at`; anything else, including network errors → 502 `cannot reach GitHub: <message>`.
  - The owner stored is the login GitHub returns on the matched repo.
  - Then `Store.Configure`: `ErrConfigured` → 409 as above; `ErrOwnerMismatch` → 400; other errors → 500 with the OS error. Success → close `ready`, 204.
- `POST /setup/finish`, body `{"toolchains": "popular" | "none"}` (anything else 400): not serving → 409 `ghr is not configured yet; finish GitHub setup first`. `popular` queues the preset through the backend's `InstallPreset("popular")`, its errors mapped as `POST /toolchains` maps them (`internal/daemon/backend.go:578`); then `toolchains-pending` and `setup-pending` are deleted (missing is not an error); 204 and the `info` event `first-run setup finished`.

### Setup-phase fallback

- `GET /status` → `model.Status` with `now`, the shared `epoch`, `mode`, `global_max`, `web_setup_required`, `unconfigured: true`, `setup_pending`, empty `repos` and `instances`, zero elsewhere.
- `GET /events` as today: the ring exists before the manager.
- Anything else → 503 `{"error": "ghr is not configured yet; finish setup first"}`.

### Status fields

`model.Status` gains `Unconfigured bool` (`unconfigured`) and `SetupPending bool` (`setup_pending`). The negative name makes an older daemon, which omits it, read as configured, so a newer CLI never hides an old daemon's tables.

## 4. CLI

- `ghr setup` with no argument prints the state, one `key: value` per line: `configured: yes|starting|no`, `wizard: pending|done`, `owner: <owner or ->`, `web: <web_listen or ->`; exit 0.
- `ghr setup github [--owner <owner>]`:
  - `--owner` may be omitted when the state's owner is set; otherwise its absence is a usage error (exit 2).
  - The token comes from a no-echo prompt `GitHub token: ` on stderr when stdin is a terminal (one entry, no repeat), otherwise from stdin, trimmed. The reading code is shared with `ghr web set-password` (`stdinIsTerminal`, `readPassword`, the context check after a read).
  - It posts to `/setup/github`; a daemon error prints `ghr: <message>`, exit 1.
  - After the 204 it polls `GET /setup` once a second for up to 60 seconds until `configured` is true, treating connection errors as still starting (the daemon may be restarting after a start-up failure). Success prints `GitHub owner and token set; ghr is running`; the timeout prints `ghr: owner and token saved, but ghr is still starting; check: journalctl -u ghr -n 50`, exit 1.
- `ghr setup finish [--toolchains popular|none]` (default `none`) posts to `/setup/finish` and prints `first-run setup finished`.
- Anything else under `setup` → `usage: ghr setup [github [--owner <owner>] | finish [--toolchains popular|none]]`, exit 2.
- `internal/api` client gains `SetupState`, `SetupGitHub` and `SetupFinish`.
- `printStatus` (`cmd/ghr/cli.go:292`): while `unconfigured`, it prints only `warning: ghr is not configured. Run: ghr setup github --owner <owner>, or open the web UI` and, when set, the existing password warning, nothing else.
- Usage lists the three `setup` forms.

## 5. Web UI

### Routing

- The session check in `appRoute.beforeLoad` (`web/src/router.tsx:44-53`) moves into a shared function. After it, the function fetches `GET /api/setup` with `staleTime: 0`, as `authState` does.
- `appRoute` (component `Shell`): not `configured` (including `starting`) → redirect to `/setup`.
- A new layout route `setupLayout` (id `setup`, under the root, no `Shell`) runs the same function and holds `/setup`. Configured with the wizard done → redirect to `/`. The `Shell` is never mounted on `/setup`, so its config, metrics and events queries never meet the setup phase's 503.
- The `Shell` shows a banner `First-run setup is not finished.` with a `Finish setup` link to `/setup` while the polled `/status` reports `setup_pending`; a CLI `ghr setup finish` therefore clears it within one poll.
- After login or claim, `/login` redirects as today; `appRoute` then sends an unconfigured daemon to `/setup`.
- A failed `GET /api/setup` in either `beforeLoad` does not redirect: the page loads, and the Shell's status poll or the wizard's error state reports the failure.
- The claim form's hint becomes `No password is set. Choose one to claim this ghr.`

### Wizard

One page, a step indicator, steps saved as they go. It opens at the first step not done: GitHub while not `configured`, otherwise Repositories (the password is always done by then: the page needs a session).

1. **Password**: shown as done; claiming happens on `/login`.
2. **GitHub**: owner and token fields, the owner prefilled from the state and read-only when config names one; the required PAT access (repository access to the repos ghr serves; Administration read/write, Actions read). Submit posts `/setup/github` and shows the daemon's message inline. On 204 the step shows `Starting ghr…` and polls `GET /setup` once a second until `configured`, then invalidates the setup state and status and moves on; after 60 seconds it shows `ghr is still starting; check journalctl -u ghr on the host` with a retry button that resumes polling. Shown as done when configured, including by setup.sh.
3. **Repositories**: the configured repos (from `GET /config`) and an `Add repository` button that opens the existing `AddRepoDialog` (`web/src/components/add-repo-dialog.tsx`), which lists `GET /repos/available`, takes max and labels, and confirms a public repo before `POST /repos`. Zero repos is allowed.
4. **Settings**: the settings form, extracted from `pages/settings.tsx` into `components/settings-form.tsx`, which exposes its unsaved field list and its save to both pages; save uses `PATCH /config`. The Token, Maintenance and Account cards stay on the Settings page, whose router-level `UnsavedGuard` stays. In the wizard, `Next` with unsaved fields opens a confirm dialog (save, discard, or stay).
5. **Toolchains**: a `Popular set` switch, on when `toolchains_pending`, and the existing toolchains card for single installs.
6. **Finish**: posts `/setup/finish` with the switch's value, invalidates the setup state and goes to the Dashboard.

`Skip the rest` on steps 3 to 5 posts `/setup/finish` with `none`.

## 6. Homelab `github-runner/`

### setup.sh

- `install_config` (`setup.sh:146-164`): on a first install copies the example, sets `FIRST_INSTALL=1`, touches `toolchains-pending` and `setup-pending`. The token prompt and `die "a token is required"` are removed; the token file is no longer written here.
- The comment above `wait_ready` (`setup.sh:188-189`) becomes: the daemon serves once its listeners are up; when configured, adoption and GitHub reconciliation run first, so this can take several seconds.
- New `configure_github`, run in `main` right after `wait_ready` and before `set_web_password`. Unless `ghr setup` prints `configured: no`, it returns. Otherwise:
  - Owner: `GHR_OWNER`; else the state's owner when set; else, when stdin is a terminal, `read -rp "GitHub owner (user or org; Enter to finish in the browser): "`.
  - Token, only with an owner: `GHR_TOKEN`; else, when stdin is a terminal, `read -rsp "GitHub fine-grained PAT (Administration: read/write, Actions: read; Enter to finish in the browser): "`.
  - Both present → `printf '%s' "$tok" | ghr setup github --owner "$owner"`, which returns once ghr serves (§4); on failure warn with ghr's message and continue.
  - `GHR_TOKEN` set but no owner obtained → warn `GHR_TOKEN is ignored without an owner; set GHR_OWNER`.
  - It runs on every setup.sh run while unconfigured, not only on a first install.
- `preinstall_toolchains` first runs `ghr setup`: while it prints `configured: no` or `configured: starting`, it keeps `toolchains-pending` and logs `toolchains can be chosen in the setup wizard, or later with: ghr toolchain install --preset popular`. Otherwise its logic is today's.
- A final step `print_next`, from `ghr setup`'s output:
  - not `configured: yes`, web on → `finish setup in the browser: http://<first address of hostname -I>:<port of web>/setup`;
  - not `configured: yes`, web off → `finish setup with: ghr setup github --owner <owner>`;
  - `configured: yes`, `wizard: pending`, web on → the browser line;
  - `configured: yes`, `wizard: pending`, web off → `end first-run setup with: ghr setup finish`;
  - otherwise nothing.
- The header comment documents `GHR_OWNER` next to `GHR_TOKEN` and `GHR_WEB_PASSWORD`, and that `GHR_TOKEN` now needs an owner; `set_web_password`'s comment drops the loopback wording.

### README.md

The install section describes both paths: answer the prompts (or pass `GHR_OWNER`, `GHR_TOKEN`, `GHR_WEB_PASSWORD`, then `ghr setup finish`), or press Enter and finish at `http://<host>:8080/setup`; the first visitor claims the password. It notes that `GHR_TOKEN` alone no longer configures ghr. The loopback and SSH-tunnel wording from the TUI-retirement amendment goes.

## 7. Reverting the loopback-only setup (register row 3)

- ghr: revert the guard from `e87935d` (`ErrSetupRemote`, the loopback check in the `setup` handler and its `fail` case, their tests), keeping the shared `remoteAddr` helper that `ClientKey` uses (`internal/webui/auth.go:329-358`). Keep `b9560e6` except the login hint, replaced by §5's wording.
- TUI-retirement spec: remove the `Loopback-only first-visitor setup` subsection and the Decisions bullet's loopback rule, and add to its Status line that this spec supersedes them.
- Loopback plan: Tasks 1 and 4 marked superseded by this spec; Tasks 2 and 3 stand as done.
- TUI-retirement register: row 12 → done with the owner's ruling; row 9 → done once its acceptance is checked against homelab `2689225`; row 8 reassigned to this spec's plan, its acceptance carried into §10.

## 8. Error handling

- The swap: a request that entered the setup handler finishes there; the next one sees the full API. The swap happens after the backend is built, so no request reaches a half-built backend. Clients that act after configuring wait for `configured` (§4, §5, §6).
- Concurrent configuring (two browsers, a browser and the CLI, a POST and a SIGHUP): `Store.Configure` checks and writes under one lock and `ready` closes once, so one wins and the owner read at step 8 is final.
- A web session in a second tab while unconfigured: `appRoute` redirects to `/setup`; a request that slipped through gets the 503 and the page's error state.
- A newer CLI against an older daemon: unknown routes get the client's existing message (`internal/api/client.go:55-57`); `unconfigured` absent reads as configured.

## 9. Testing

- **config**: an empty owner validates; the rest of `Validate` unchanged.
- **store**: opening with no token file and with an empty one; `Configured()`; `Configure` writes both, refuses when configured (`ErrConfigured`) and on a different config owner (`ErrOwnerMismatch`), accepts the config owner, leaves both files untouched on a token write failure and stays unconfigured on an owner write failure; `Reload` sets a first owner and a first token but refuses an owner change; once configured, the existing reload errors stand.
- **daemon**, against the fake GitHub server the daemon tests use:
  - `POST /setup/github`: bad owner syntax, empty token, omitted owner taken from config, owner mismatch, token sees no repo of the owner, 401, 404 from `checkToken`, rate limit (429 with `retry_at`), GitHub down (502), success (204, files written, canonical owner case), configured (409), concurrent requests (one 204, one 409).
  - Setup phase: 503 for an API route; the reduced `/status` (`unconfigured`, `web_setup_required`, epoch); `/events`; `POST /setup/finish` 409; `GET /setup` reporting `starting` between configure and swap; `/auth/setup` on the web port and `/web/set-password` on the socket work; `GET /api/setup` and `POST /api/setup/github` without a session get 401.
  - Lifecycle: `Run` unconfigured, log in on the web listener, `POST /api/setup/github`, poll until `configured`, then the same session gets the full `/api/status`, with the same epoch as before; the SIGHUP path reaches the same state; a reload and a POST racing configure once; ctx cancelled while unconfigured returns nil with the socket and port free; a start-up failure after configuring (an unreadable history path) returns the error with the socket and port free (template: `TestRunClosesTheWebListenerWhenStartFails`).
  - `POST /setup/finish`: both markers deleted, `popular` queues the preset, a bad value is 400.
- **CLI**: `setup` state output for each `configured` value; `setup github` from stdin and from the prompt, omitted owner, daemon error, the wait for `configured` and its timeout; `setup finish`; usage; `printStatus` while unconfigured prints only the warnings; an old daemon's status (no `unconfigured`) prints tables.
- **web (vitest)**: redirect to `/setup` while unconfigured; `/setup` renders without the Shell; redirect away when configured and done; the banner from `/status`; each step (owner prefill read-only, GitHub error inline, the starting poll and its timeout, repo checklist adds, settings save and the Next confirm, toolchain switch default, finish and skip post the right body); the Settings page's existing tests pass on `SettingsForm`; the claim hint.
- **setup.sh** (`tests/setup_test.sh`, stubbed `ghr`):
  - Rewritten: `d-token`, `g-first-install` and `h-web-password` (`tests/setup_test.sh:174-182, 204-211, 254-257`), which rely on the token prompt or on `GHR_TOKEN` writing the token file.
  - New: `configure_github` with env vars, with prompts, with Enter at owner and at token, with the state's owner, skipped when configured, the `GHR_TOKEN`-without-owner warning, warns on failure; `install_config` no longer prompts and touches `setup-pending`; `preinstall_toolchains` while unconfigured; each `print_next` branch.
  - `bash -n` and `shellcheck` (installed if possible) on setup.sh.

## 10. Rollout

- v0.1.15 (the TUI-retirement work with the loopback guard) was released and deployed to the runner LXC on 2026-10-07; this spec ships as v0.1.16.
- That rollout paused with the owner's web password removed and backed up at `/root/web-password.keep` on the LXC (TUI-retirement spec §8 steps 4-6 pending). First, with the owner's yes, restore it as §8 step 6 does: `install -m 0600 /root/web-password.keep /etc/ghr/web-password`, `systemctl restart ghr`, `/auth/state` reports `setup_required:false`.
- Run shellcheck on setup.sh first if it can be installed.
- Upgrade the runner LXC (192.168.0.99) with `GHR_VERSION=v0.1.16 bash setup.sh` while no jobs run: it is configured, so no prompts and no wizard. Check `ghr`, `ghr setup` (`configured: yes`, `wizard: done`), the web UI login with the owner's password, and `ghr web set-password` with the copy-aside and restore sequence of the TUI-retirement spec §8 (carried from its register row 8).
- Fresh-install check on a throwaway LXC the owner creates: run setup.sh pressing Enter at every prompt, then in a browser claim the password and walk the wizard to the Dashboard; then a second fresh run with `GHR_OWNER`, `GHR_TOKEN` and `GHR_WEB_PASSWORD` set, a login from the browser with that password, and `ghr setup finish`. Without a throwaway LXC the fresh-install check is skipped and recorded as skipped.

## Out of scope

- Changing the owner of a configured daemon without a restart.
- Persisting web sessions across restarts.
- A CLI wizard for repos, settings and toolchains (the existing commands cover them).
