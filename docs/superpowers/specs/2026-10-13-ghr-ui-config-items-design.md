# ghr: config items editable in the UI

**Date:** 2026-10-13
**Status:** design presented in chat in four sections and approved by the owner on 2026-10-13, with the rulings on scope, busy restarts, lockout protection, the restart mechanism and the execution strategy made the same day. Amended the same day after a review of the first draft (notes/2026-10-13-ghr-ui-config-items-spec-review.md): the lockout check now compares against the address the browser reached (§2.1), the fallback records an address only once a browser has used it (§2.2), the systemd test checks the main PID (§1.2), and the rest records what the code needs. The owner was told of each.
**Execution strategy:** sequential — the UI tasks consume the new API fields (the status restart block, the patch keys), and both sides touch the shared model types, so the work is one chain.
**Register:** docs/superpowers/registers/2026-10-13-ghr-ui-config-items.md (rows 1 to 3); docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md (rows 232 and 263).
**Code:** `internal/model`, `internal/config`, `internal/daemon`, `internal/api`, `internal/webui`, `internal/runner` (a prune accessor, visibility in the status, one message), `web/src` (API types, Settings, the repository page, the shell banners, a restart screen), `deploy/config.example.yaml`, `README.md`.

## Goal

Every `config.yaml` key except `owner` is editable in the web UI, and the keys that only a restart applies can be applied from the UI.

An audit of `config.yaml` against `PATCH /api/config` and the UI on 2026-10-13 found four keys the UI cannot change: `repos[].allow_public` (settable only when a repository is added), `web.listen`, `web.hosts` and `owner`. Every other key is already editable. The paths the daemon takes from its command line are packaging, not settings.

## Decisions

- **Owner stays read-only** (owner's ruling, 2026-10-13). A new owner orphans every registered runner and the repository list, and the token must belong to it; it stays a deliberate `config.yaml` edit.
- **A restart is refused while the daemon's own work is in flight** (owner's ruling, 2026-10-13): a storage operation running or queued, a prune of either kind, or a runner software update. Running jobs never block it: runners are their own systemd units, the daemon adopts them at start, and finish and cleanup work resumes there (`run.go`'s shutdown waits `ShutdownWait` for it).
- **Web settings are checked when saved, and a failed bind at start falls back** (owner's ruling, 2026-10-13), so a bad value cannot crash-loop the daemon or lock the owner out of the UI.
- **The daemon restarts through `systemctl restart --no-block ghr.service`** (owner's ruling, 2026-10-13). systemd stops it with the normal graceful shutdown and starts it at once.
- **No `ghr restart` command.** `systemctl restart ghr` already does that; the endpoint is reachable over the socket for anyone who wants it.
- **Accepted interruptions.** A restart also cuts, as any `systemctl restart ghr` does today: a runner stop in flight (the unit still stops; its finish runs at the next start's adoption), a spawn in the middle of a tick (a leftover registration is removed by the periodic reconcile), a label scan (it shows as unchecked and can be run again), and a storage measurement (the next start measures). None of these block a restart.

## 1. Daemon and API

### 1.1 The config patch

- `model.ConfigPatch` gains `Web *WebPatch` with `Listen *string` and `Hosts *[]string`; `model.RepoPatch` gains `AllowPublic *bool`.
- `api.Backend.PatchConfig` gains a context: `PatchConfig(ctx context.Context, p model.ConfigPatch) error`. The `PATCH /config` and `POST /repos/{name}/pause|resume` handlers pass `r.Context()`, and the fakes in `internal/api` tests change with it. Section 2.1 reads the browser mark from that context.
- `PatchConfig` applies the new fields. The section 2.1 checks run inside the patch function on the new values; the store's `config.Validate` still runs on the result, as for every patch.
- `allow_public` takes effect at once: `updateThen` wakes the loop, and `planConfig` reads it from the current config on that tick.
- `web.listen` and `web.hosts` are written to `config.yaml` and take effect at the next start.
- `owner` is not in the patch.

### 1.2 Restart state in the status

`model.Status` gains:

```go
Restart RestartStatus `json:"restart"`
// WebListenError is set when the configured web.listen failed to bind at
// start (section 2.2).
WebListenError *WebListenError `json:"web_listen_error,omitempty"`

type RestartStatus struct {
	// Pending names the settings whose saved value differs from the one the
	// running daemon applied: "web.listen", "web.hosts".
	Pending []string `json:"pending,omitempty"`
	// BlockedBy names the daemon work that refuses a restart now.
	BlockedBy []string `json:"blocked_by,omitempty"`
	// Available is false when this process is not ghr.service's main process.
	Available bool `json:"available"`
	// WebListen is the listen value the running web listener serves,
	// WebApplied.Listen; empty when none runs.
	WebListen string `json:"web_listen,omitempty"`
	// Failed says why the last accepted restart did not happen; a new
	// accepted request clears it.
	Failed string `json:"failed,omitempty"`
}

type WebListenError struct {
	Address   string `json:"address"`
	Reason    string `json:"reason"`
	ServingOn string `json:"serving_on,omitempty"`
}
```

- **Pending** compares `Store.Config().Web` with `Backend.WebApplied`. `WebApplied.Listen` is the configured string that was bound (the `web.listen` value, or the fallback file's value of section 2.2), never `Addr().String()`, which reports `[::]` for a wildcard on a dual-stack host. Listen values compare as strings; hosts compare as sets, ignoring case, order and duplicates. A hand edit to `config.yaml` followed by a reload shows here too.
- **BlockedBy**, in this order:
  - the storage service's current operation, as its verb and target (`"installing node 24"`, the wording the UI's `queueText` uses);
  - `"N queued"` for queued storage operations;
  - `"pruning"` while a prune of either kind runs. The automatic disk prune sets only the manager's `pruning` flag, not `maint.Running`, so the `daemon.Manager` interface gains `Pruning() bool`, true while `m.pruning` is set and `m.upd.running` is not;
  - `"updating the runner software"` while `RunnerUpdate.Running` is true.
- **Available** is worked out once at start through an `Options.SelfUnit` seam. The default is true when `INVOCATION_ID` is set and `systemctl show -p MainPID --value ghr.service` prints this process's PID. `INVOCATION_ID` alone is not enough: every process a systemd unit starts inherits it, including a job's processes on a runner unit.
- **Failed** is kept by `Backend` in memory, so a new process starts without it. Section 1.3 sets and clears it (register row 4, 2026-10-13).
- **Live updates.** `Backend.fingerprints()` hashes the manager's status for the `status` topic. It also hashes `Restart` and `WebListenError`, so a change in either publishes a `status` change at once rather than at the next 60-second poll.

### 1.3 `POST /api/restart`

- 409 with `"ghr is not running as ghr.service; restart it by hand"` when `Available` is false.
- 409 with `"waits for: " + strings.Join(BlockedBy, ", ")` while `BlockedBy` is not empty.
- Otherwise:
  1. Clear `Restart.Failed`, then write the event `restart requested`, with `from the web UI` when the request carries the browser mark of section 2.1.
  2. `w.WriteHeader(http.StatusAccepted)`, then `http.NewResponseController(w).Flush()`.
  3. Check the blockers again; when work started since the first check, write `restart cancelled: <blockers> started` as a warning, set `Restart.Failed` to `<blockers> started`, and stop. Otherwise run `Options.Restart(ctx)` with `context.WithoutCancel(r.Context())`. The default runs `systemctl restart --no-block ghr.service` through `system.Exec`. It is its own seam, not a method on `runner.Systemd`, whose fakes stay as they are.
  4. A failure of the command is written as an error event `restart failed: <error>` and sets `Restart.Failed` to the error's text; the daemon keeps running.
- The route is on the socket API, so the web listener serves it under `/api/restart` with its session, `X-GHR` and Origin checks. The CLI client gains no method for it.

### 1.4 Events and messages

- **The reload response** (`POST /reload`, printed by `ghr reload`) lists `web settings changed; restart ghr to apply` on every reload while `Restart.Pending` is not empty, as today; `TestRunServesTheWebUI` keeps pinning that.
- **The event** with the same text is written when `Restart.Pending` becomes non-empty, and again when it changes while non-empty. `Backend` keeps the last pending list it wrote an event for; an empty list resets it. A patch that changes `web.listen` or `web.hosts` follows the same rule. This closes comprehensive-review rows 232 and 263; `TestReloadWarnsOnWebChange` changes with it.
- **`blockedText`** in `internal/runner/visibility.go` reads: `"<repo> is public; ghr serves only private repositories. Turn on Serve when public on its repository page to serve it"`. `internal/runner/blocked_test.go` pins it and changes with it.

### 1.5 Visibility in the status

`model.RepoStatus` gains `Public *bool` (`json:"public,omitempty"`), filled in `Manager.Status()` from the visibility GitHub last reported; nil until GitHub has answered. The repository page needs it for section 3.1's note, which shows whether or not the repository is blocked.

## 2. Lockout protection

### 2.1 When a web setting is saved

**The browser mark.** `internal/api` gains `WithBrowser(ctx, host string) context.Context` and `Browser(ctx) (host string, ok bool)`. The web handler wraps each `/api/` request's context with the request's `Host` value before handing it to the API. A request over the socket carries no mark. The connection's local address comes from `http.LocalAddrContextKey`, which `net/http` sets for every request; an IPv4-mapped IPv6 address is unmapped first.

`PatchConfig` refuses, with a 400, in this order. Every message starts with the field key and a colon, so the UI can place it (section 3.2):

1. **An empty `web.listen`:** `"web.listen: the web UI cannot turn itself off; clear it in config.yaml to turn the UI off"`. A patch over the socket is refused the same way.
2. **A listen value that does not parse:** `config.Validate`'s wording, `"web.listen must be host:port with a port from 1 to 65535, such as 0.0.0.0:8080"`, prefixed `web.listen: `.
3. **A listen host this browser could not reach.** For a request with the browser mark, let L be the connection's local IP. The new host is accepted when it is empty, `::`, `0.0.0.0` with L an IPv4 address, L itself, or `localhost` or a loopback IP with L a loopback address. Anything else: `"web.listen: you reached ghr on <L>; listening on <host> would lock this browser out"`. This covers a docker bridge or second-NIC address, an IPv4 wildcard for a browser on IPv6, a LAN address for a proxy on this host that connects over loopback, and a loopback address for a browser elsewhere.
4. **A listen host that is not this host's, over the socket:** without the browser mark the host must be empty, `0.0.0.0`, `::`, `localhost`, or an IP address from `net.InterfaceAddrs` (through a `Backend` seam): `"web.listen: <host> is not an address of this host"`.
5. **A port that is taken:** unless the new port equals the port of `WebApplied.Listen`, the daemon binds the new address with `net.Listen` and closes it at once, through a `Backend` seam: `"web.listen: <host:port> is not free: <reason>"`. When the port is the one the listener holds, no test is made: the listener releases it at the restart, and section 2.2 covers what else might hold it.
6. **Removing the browser's own hostname:** for a request with the browser mark whose `Host` names a hostname (not an IP literal and not `localhost`), the new `web.hosts` must still contain it, ignoring case: `"web.hosts: you are using <name>; removing it would lock this browser out"`.

Checks 3 to 5 call the network while `Store.mu` is held, as the patch function runs under it; they are quick local calls.

### 2.2 When the daemon starts

- **The last working address.** `Options.WebListenPath`, default `/var/lib/ghr/web-listen`, holds the last `web.listen` value a browser used. The web handler writes the served listen string there, atomically, mode 0600, on the first request with a valid session that it serves, when the file does not already hold that value. A bind alone does not prove a browser can reach the address. An empty `WebListenPath`, as in tests, records nothing and offers no fallback. An unreadable or malformed file is treated as absent.
- **Fallback.** When `web.listen` fails to bind and the file holds a different value, the daemon binds that value. On success it serves the UI there, sets `WebApplied.Listen` to it, and writes an error event: `"web.listen <addr>: <reason>; serving the web UI on <fallback>"`.
- **No address works.** When there is no fallback or it fails too, the daemon starts without the web listener and writes `"web.listen <addr>: <reason>; the web UI is off until it is fixed"`, instead of exiting. The socket API and the runners work as usual.
- **The fallback listener** goes through `limitConns(tcp, maxWebConns, maxWebConnsPerClient)` like the configured one. `notConfigured` and `SetupState.WebListen` use the address actually served, or none.
- **Status.** `WebListenError` (section 1.2) carries the failed address, the reason, and the address served, empty when no listener runs. It stays until the next start.
- `web.hosts` cannot fail to apply; the handler always uses the configured list.
- An empty `web.listen` in `config.yaml` still means no web UI, with no fallback and no error.
- `TestRunFailsWhenTheWebPortIsTaken` and `TestRunClosesTheWebListenerWhenStartFails` pin an exit on a bind failure; they are rewritten for the fallback and the run without a listener, keeping what they check about closing the socket when start fails for another reason.

## 3. UI

### 3.1 The repository page

- `web/src/api/types.ts`: `RepoConfig` gains `allow_public?: boolean`, `RepoPatch` gains `allow_public?: boolean`, `RepoStatus` gains `public?: boolean`.
- The settings form gains a **Serve when public** switch, saved with the existing save bar as `repos.<name>.allow_public`. Help: "A job in a public repository can come from anyone's pull request and runs on this host."
- **Saving it on** first asks "Serve <repo> while it is public?" with the help text as the body, action "Serve when public". The question covers the whole save: confirming sends every changed field in one patch, and Cancel sends nothing and keeps the draft. The save that `UnsavedGuard` runs when leaving the page asks the same question.
- **Saving it off** does not ask.
- **The note.** When `public` is true, the field's note reads "GitHub reports this repository public." and, with the switch off, adds "Idle runners stop; running jobs finish."
- The draft, changed marker and rejection behave like the form's other fields.

### 3.2 Settings → General: Web access

A `FieldGroup` titled "Web access" after "Runner defaults", note "Changes apply after a restart." The setup wizard, which reuses these sections, does not show it:

- **Listen address**: an `Input` in mono, value `web.listen`. When the saved value is empty (a hand edit), the field shows it empty with the note "The web UI turns off at the next restart."
- **Allowed hostnames**: a `TagField`, value `web.hosts`, help "IP addresses and localhost always work."
- Both are in the existing `SaveBar` draft. `lib/settings-form.ts` carries them as `web_listen` and `web_hosts` in `loadedValues`, `withPatch` and `fieldErrors`, and `toPatch` nests them under `web`, as it nests `runner_limits`. Hosts compare as sets in `equal`, so reordering is no change, as the daemon sees it.
- The client checks the shape first: `host:port` with a port from 1 to 65535, and bare hostnames.
- **Rejections.** A 400 whose message starts with `web.listen: ` or `web.hosts: ` shows under that field, with the prefix removed; any other rejection stays in the form's `RejectedAlert`.
- When `restart.pending` is not empty, the group shows the restart control of section 3.3 under its fields.

### 3.3 Restart

- **Banner.** The Restart ghr button stays disabled until the config has loaded, since the confirmation needs the saved `web.listen`. While `restart.pending` is not empty and `web_listen_error` is absent, a warning banner in the shell, in the Runner update banner's place and style, on every page: "Restart needed: web.listen changed." (each pending key named), with a **Restart ghr** button. When `available` is false the button is absent and the text adds "Restart ghr with systemctl restart ghr."
- **Blocked.** While `restart.blocked_by` is not empty the button is disabled and the banner adds "Waits for: installing node 24, 1 queued".
- **Confirmation.** "Restart ghr?" Body: "Running jobs keep running. The web UI is down for a few seconds and you will need to log in again." Then, when `web.listen` is pending:
  - saved empty: "The web UI turns off.";
  - otherwise, the page reached ghr directly (its port is the port of `restart.web_listen`): "The web UI moves to <url>.";
  - otherwise (a proxy in front): "If a proxy forwards to ghr, point it at port <new port>."
  Action "Restart".
- **The new URL**, for a direct page: the scheme and hostname are the page's own unless the new listen host is a specific IP, which then replaces the hostname; the port is the new listen port. Behind a proxy, or with `web.listen` not pending, the URL is the page's own origin.
- **Restarting screen.** On the 202 the app clears its query cache (`queryClient.clear()`), which aborts its polls and change stream, and renders a full-page "Restarting ghr…" screen from the root route, outside `appRoute` and its guards. A late 401 from a request already in flight may still move the route to `/login`; the restarting screen stays in place.
  - When the URL is the page's own origin, it polls `GET /auth/state` every second until it answers `authenticated: false`, then loads `<url>/login` with `window.location.assign`. Sessions live in memory, so that answer comes from the new process; an error or `authenticated: true` (the old process, not yet stopped) keeps it polling.
  - When it differs, the page cannot reach it (the CSP's `default-src 'self'` limits fetches to the page's own origin; navigation is not limited), so it waits 10 seconds and then loads `<url>/login`. Meanwhile it polls its own origin as above, for the failed state only.
  - **A restart that did not happen.** Whenever its own origin answers `authenticated: true`, the screen also reads `GET /api/status`. When `restart.failed` is set it stops polling, cancels the 10-second move, and replaces the spinner with "ghr did not restart: <failed>" and a **Back to ghr** button that reloads the page. No 60-second message follows.
  - After 60 seconds without an answer on the page's own origin it reads "ghr has not answered yet." with a link to it: a failed bind serves the UI on the last working address instead (section 2.2). For another origin the page has already been replaced; when that address does not answer, the browser shows its own error page.
- **Listener error.** While `web_listen_error` is present, an error banner in the same place: "ghr could not listen on 10.0.0.5:8080: cannot assign requested address. Serving on 0.0.0.0:8080." with a link to Settings → Web access.

## 4. Docs

- `deploy/config.example.yaml`: the `web` and `allow_public` comments say the UI can change them.
- `README.md`: the web UI list says the listen address and hostnames can be changed in Settings → Web access and applied with Restart ghr, what blocks a restart, and that the UI refuses an address the browser could not reach.
- The Owner field's help on Settings stays as it is.

## 5. Testing

### 5.1 Go

- **Save checks** (section 2.1), with fake interface addresses and a fake binder:
  - empty listen (with the empty-listen message, not the parse one) and a parse failure;
  - a browser on an IPv4 LAN address: `0.0.0.0`, `::`, its own address and an empty host pass; a docker bridge address, another NIC's address and `localhost` fail;
  - a browser on IPv6 (and on an IPv4-mapped address, unmapped): `0.0.0.0` fails for the former;
  - a browser over loopback: `localhost` passes, a LAN address fails;
  - over the socket: an own address passes, a foreign one and a hostname fail;
  - a free port, a taken port, the held port;
  - the hostname check for a browser through a hostname, through an IP, and over the socket;
  - every message starts with its field key.
- `PatchConfig` with `allow_public` on and off; a public repository is served on the tick the patch wakes.
- `RepoStatus.Public` nil before GitHub answers, then true or false.
- `Restart.Pending` after a patch and after a hand edit and reload; hosts compared as sets; a wildcard listen is not pending against itself.
- `BlockedBy` for a storage operation, queued operations, a manual prune, an automatic prune and a runner update.
- `Available` through the `SelfUnit` seam.
- The `status` fingerprint changes when `Restart` or `WebListenError` does.
- `POST /api/restart`: 202 is flushed before the `Restart` seam runs, with an uncancelled context; 409 while blocked; 409 when not available; a seam failure writes an error event; a cancel and a seam failure each set `Restart.Failed`, and the next accepted request clears it.
- Reload: the response lists the warning on every reload while pending; the event is written once per change of the pending list and again after it empties and returns.
- Start: the address is recorded after the first authenticated request, not after the bind; a failing `web.listen` falls back, serves through `limitConns`, and sets `WebListenError`; without a fallback the daemon runs without a listener and the socket API answers; an unreadable fallback file is absent; an empty `WebListenPath` records nothing.

### 5.2 Web

- Serve when public: saving on asks first and sends the patch once confirmed; Cancel sends nothing; the guard's save on leaving asks too; saving off sends at once; the public note with the switch on and off.
- Web access: its values, the client shape checks, hosts reordered is no change, a `web.listen:` rejection under its field, another rejection in the alert, the empty-listen note.
- The banners: pending, blocked, unavailable and listener error.
- The confirmation's text and URL: a wildcard, a specific IP, a proxy in front, an empty listen, nothing pending for listen.
- The restarting screen: the cache is cleared, the stream is aborted, and a late 401 does not replace the screen; polling for the page's origin; the 10-second wait for another origin; the 60-second message; `restart.failed` from the old process shows the reason and the reload button, stops polling, and cancels a move.

### 5.3 On the runner LXC, with the owner's yes

- Add a hostname, restart from the banner, log in again.
- Move the port to 9090 through the UI, follow the move, log in, and move it back.
- Try a docker bridge address in Web access and see it refused.
- Turn Serve when public on and off for a private repository.
- Put an unassigned IP in `web.listen` by hand, restart with `systemctl restart ghr`, and see the listener error banner on the fallback address.
