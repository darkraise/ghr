# ghr: config items editable in the UI

**Date:** 2026-10-13
**Status:** design presented in chat in four sections and approved by the owner on 2026-10-13, with the rulings on scope, busy restarts, lockout protection, the restart mechanism and the execution strategy made the same day.
**Execution strategy:** sequential — the UI tasks consume the new API fields (the status restart block, the patch keys), and both sides touch the shared model types, so the work is one chain.
**Register:** docs/superpowers/registers/2026-10-13-ghr-ui-config-items.md (rows 1 to 3); docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md (rows 232 and 263).
**Code:** `internal/model`, `internal/config`, `internal/daemon`, `internal/api`, `internal/webui`, `internal/system`, `internal/runner` (one message), `web/src` (API types, Settings, the repository page, the shell banners, a restart screen), `deploy/config.example.yaml`, `README.md`.

## Goal

Every `config.yaml` key except `owner` is editable in the web UI, and the keys that only a restart applies can be applied from the UI.

An audit of `config.yaml` against `PATCH /api/config` and the UI on 2026-10-13 found four keys the UI cannot change: `repos[].allow_public` (settable only when a repository is added), `web.listen`, `web.hosts` and `owner`. Every other key is already editable. The paths the daemon takes from its command line are packaging, not settings.

## Decisions

- **Owner stays read-only** (owner's ruling, 2026-10-13). A new owner orphans every registered runner and the repository list, and the token must belong to it; it stays a deliberate `config.yaml` edit.
- **A restart is refused while the daemon's own work is in flight** (owner's ruling, 2026-10-13): a storage operation running or queued, a prune, or a runner software update. Running jobs never block it: runners are their own systemd units and the daemon adopts them at start.
- **Web settings are checked when saved, and a failed bind at start falls back** (owner's ruling, 2026-10-13), so a bad value cannot crash-loop the daemon or lock the owner out of the UI.
- **The daemon restarts through `systemctl restart --no-block ghr.service`** (owner's ruling, 2026-10-13). systemd stops it with the normal graceful shutdown and starts it at once.
- **No `ghr restart` command.** `systemctl restart ghr` already does that; the endpoint is reachable over the socket for anyone who wants it.

## 1. Daemon and API

### 1.1 The config patch

`model.ConfigPatch` gains `Web *WebPatch` with `Listen *string` and `Hosts *[]string`; `model.RepoPatch` gains `AllowPublic *bool`. `PatchConfig` applies them. The section 2.1 checks run inside the patch on the new values, parsing `host:port` themselves; the store's `config.Validate` still runs on the result, as for every patch, so its `web.listen` and `web.hosts` syntax checks hold too.

- `allow_public` takes effect on the next tick, as a hand edit and reload does today: `planConfig` reads it from the current config.
- `web.listen` and `web.hosts` are written to `config.yaml` and take effect at the next start.
- `owner` is not in the patch.

### 1.2 Restart state in the status

`model.Status` gains:

```go
Restart RestartStatus `json:"restart"`

type RestartStatus struct {
	// Pending names the settings whose saved value differs from the one the
	// running daemon applied: "web.listen", "web.hosts".
	Pending []string `json:"pending,omitempty"`
	// BlockedBy names the daemon work that refuses a restart now.
	BlockedBy []string `json:"blocked_by,omitempty"`
	// Available is false when the daemon was not started by systemd.
	Available bool `json:"available"`
}
```

- `Pending` compares `Store.Config().Web` with `Backend.WebApplied`, the block the listener runs with (section 2.2 sets its `Listen` to the address actually bound). Hosts compare as sets, ignoring case and order. A hand edit to `config.yaml` followed by a reload shows here too.
- `BlockedBy`, in this order:
  - the storage service's current operation, as its verb and target (`"installing node 24"`, the wording the UI's `queueText` uses);
  - `"N queued"` for queued storage operations;
  - `"pruning"` while maintenance runs;
  - `"updating the runner software"` while `RunnerUpdate.Running` is true.
- `Available` is true when the environment has `INVOCATION_ID`, which systemd sets for every service it starts.

### 1.3 `POST /api/restart`

- 409 with `"ghr was not started by systemd; restart it by hand"` when `Available` is false.
- 409 with `"waits for: " + strings.Join(BlockedBy, ", ")` while `BlockedBy` is not empty.
- Otherwise it writes the event `restart requested` (with `from the web UI` when the request came through the web listener, section 2.1), answers 202, and after the response is written runs `systemctl restart --no-block ghr.service` through `internal/system`. A failure of that command is written as an error event; the daemon keeps running.
- The route is on the socket API, so the web listener serves it under `/api/restart` with its session, `X-GHR` and Origin checks.
- The CLI client gains no method for it.

### 1.4 Events

- The reload warning `web settings changed; restart ghr to apply` is written once each time `Restart.Pending` changes to a different non-empty list, not on every reload. This closes comprehensive-review rows 232 and 263; `TestReloadWarnsOnWebChange` changes with it.
- A patch that changes `web.listen` or `web.hosts` writes the same warning under the same rule.
- `blockedText` in `internal/runner/visibility.go` reads: `"<repo> is public; ghr serves only private repositories. Turn on Serve when public on its repository page to serve it"`.

## 2. Lockout protection

### 2.1 When a web setting is saved

`PatchConfig` refuses, with a 400 naming the field, in this order:

1. **An empty `web.listen`:** `"the web UI cannot turn itself off; clear web.listen in config.yaml to turn it off"`. A patch from the socket is refused the same way; `config.yaml` stays the way to turn the UI off.
2. **A listen host that is not usable:** the host must be empty, `0.0.0.0`, `::`, `localhost`, or an IP address assigned to this host. Anything else: `"web.listen must use an address of this host, 0.0.0.0, :: or localhost"`. Addresses come from `net.InterfaceAddrs` through a `Backend` seam.
3. **A port that is taken:** unless the new port equals the port of `WebApplied.Listen`, the daemon binds the new address with `net.Listen` and closes it at once, through a `Backend` seam. On failure: `"web.listen: <host:port> is not free: <reason>"`. When the port equals the one the listener holds, no test is made: the listener releases it at the restart, and section 2.2 covers what else might hold it.
4. **A loopback address for a browser elsewhere:** when the request came through the web listener from a client address that is not loopback, a new listen host of `localhost` or a loopback IP is refused: `"you are not on this host; listening on <host> would lock this browser out"`.
5. **Removing the browser's own hostname:** when the request came through the web listener and its `Host` header names a hostname (not an IP literal and not `localhost`), the new `web.hosts` must still contain it, ignoring case: `"you are using <name>; removing it from web.hosts would lock this browser out"`.

The web handler marks a request that came through it by putting its `Host` value and remote address into the request's context before handing it to the API, through a pair of functions in `internal/api` (`WithBrowserHost`, `BrowserHost`). It records the request's remote address with it, for check 4. A request over the socket carries no mark and skips checks 4 and 5.

### 2.2 When the daemon starts

- **The last working address.** After the web listener binds, the daemon writes its address to `Options.WebListenPath`, default `/var/lib/ghr/web-listen`, atomically, mode 0600.
- **Fallback.** When `web.listen` fails to bind, the daemon tries the address in that file when it differs. When that binds, the daemon serves the UI on it, sets `WebApplied.Listen` to it, and writes an error event: `"web.listen <addr>: <reason>; serving the web UI on <fallback>"`.
- **No address works.** When there is no file, it names the same address, or it fails too, the daemon starts without the web listener and writes `"web.listen <addr>: <reason>; the web UI is off until it is fixed"`, instead of exiting. The socket API and the runners work as usual.
- **Status.** `model.Status` gains `WebListenError *WebListenError` (`json:"web_listen_error,omitempty"`) with `Address`, `Reason` and `ServingOn` (empty when no listener runs). It stays until the next start.
- `web.hosts` cannot fail to apply; the handler always uses the configured list.
- An empty `web.listen` in `config.yaml` still means no web UI, with no fallback and no error.

## 3. UI

### 3.1 The repository page

- The settings form gains a **Serve when public** switch, saved with the existing save bar as `repos.<name>.allow_public`. Help: "A job in a public repository can come from anyone's pull request and runs on this host."
- Saving it **on** first asks "Serve <repo> while it is public?" with the help text as the body, action "Serve when public". The question covers the whole save: confirming sends every changed field in one patch, and Cancel sends nothing and keeps the draft.
- Saving it **off** does not ask. When GitHub reports the repository public, the field's note reads "Idle runners stop; running jobs finish."
- The draft, changed marker and rejection behave like the form's other fields.

### 3.2 Settings → General: Web access

A `FieldGroup` titled "Web access" after "Runner defaults", note "Changes apply after a restart.":

- **Listen address**: an `Input` in mono, value `web.listen`.
- **Allowed hostnames**: a `TagField`, value `web.hosts`, help "IP addresses and localhost always work."
- Both are in the existing `SaveBar` draft. The client checks the shape first: `host:port` with a port from 1 to 65535, and bare hostnames. A 400 from the daemon shows under the field it names.
- When `Restart.Pending` is not empty, the group shows the restart control of section 3.3 under its fields.

### 3.3 Restart

- **Banner.** While `restart.pending` is not empty and `web_listen_error` is absent, a warning banner in the shell, in the Runner update banner's place and style, on every page: "Restart needed: web.listen changed." (each pending key named), with a **Restart ghr** button. When `available` is false the button is absent and the text adds "Restart ghr with systemctl restart ghr."
- **Blocked.** While `restart.blocked_by` is not empty the button is disabled and the banner adds "Waits for: installing node 24, 1 queued".
- **Confirmation.** "Restart ghr?" Body: "Running jobs keep running. The web UI is down for a few seconds and you will need to log in again." When `web.listen` is pending, it adds "The web UI moves to <url>." Action "Restart".
- **The new URL.** The scheme and hostname are the page's own unless the new listen host is a specific IP, which then replaces the hostname; the port is the new listen port.
- **Restarting screen.** After the 202 the app shows a full-page "Restarting ghr…" screen.
  - When the address does not change, it polls `GET /auth/state` every second and, once it answers, loads `/login`.
  - When it changes, the page cannot reach the new address (the CSP's `connect-src 'self'`), so it waits 10 seconds and then loads `<url>/login`.
  - After 60 seconds without an answer it reads "ghr has not answered yet. Try <url>." with the link.
- **Listener error.** While `web_listen_error` is present, an error banner in the same place: "ghr could not listen on 10.0.0.5:8080: cannot assign requested address. Serving on 0.0.0.0:8080." with a link to Settings → Web access.

## 4. Docs

- `deploy/config.example.yaml`: the `web` and `allow_public` comments say the UI can change them.
- `README.md`: one line on restarting from the UI and what blocks it.
- The Owner field's help on Settings stays as it is.

## 5. Testing

### 5.1 Go

- Each check of section 2.1, with fake interface addresses and a fake binder: a wildcard, an own IP, a foreign IP, a hostname, an empty listen, a free port, a taken port, the held port; a loopback listen from a remote browser, from a local one and over the socket; the hostname check for a browser request through a hostname, through an IP, and over the socket.
- `PatchConfig` with `allow_public` on and off and with the web keys; `allow_public` unblocks a public repository on the next tick.
- `Restart.Pending` after a patch and after a hand edit and reload, hosts compared as sets; `BlockedBy` for each of its four sources; `Available` with and without `INVOCATION_ID`.
- `POST /api/restart`: 202 then the fake systemctl call after the response; 409 while blocked; 409 without systemd.
- The restart-to-apply warning is written once per change of the pending list.
- Start: the address is recorded after a bind; a failing `web.listen` falls back and sets `web_listen_error`; with no fallback the daemon runs without a listener and the socket API answers.

### 5.2 Web

- The Serve when public switch: saving on asks first and sends the patch once confirmed; saving off sends it at once; the public note.
- The Web access group: its values, the client shape checks, a daemon refusal under its field.
- The banners: pending, blocked, unavailable and listener error.
- The confirmation's text and the new URL for a wildcard, a specific IP and an unchanged address.
- The restarting screen for an unchanged and a changed address, and its 60-second message.

### 5.3 On the runner LXC, with the owner's yes

- Add a hostname, restart from the banner, log in again.
- Move the port to 9090 through the UI, follow the move, and move it back.
- Turn Serve when public on and off for a private repository.
- Put an unassigned IP in `web.listen` by hand, restart with `systemctl restart ghr`, and see the listener error banner on the fallback address.
