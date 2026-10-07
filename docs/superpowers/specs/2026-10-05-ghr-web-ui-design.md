# ghr web UI

**Date:** 2026-10-05
**Status:** approved in chat section by section (2026-10-05); revised the same day after an independent review (Fable, 29 findings, all accepted) and the owner's ruling to add a Host allowlist; amended 2026-10-06 to add the Storage page (docs/superpowers/specs/2026-10-06-ghr-toolchains-caches-design.md §7)
**Code:** github.com/darkraise/ghr (new `internal/webui`, new `web/`, `internal/config`, `internal/daemon`, `internal/api`, `cmd/ghr`, `.github/workflows/ci.yml`); homelab `github-runner/`
**Builds on:** docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md, docs/superpowers/specs/2026-10-04-ghr-tui-revamp-design.md, docs/superpowers/specs/2026-10-05-ghr-repositories-management-design.md, docs/superpowers/specs/2026-10-05-ghr-runner-update-design.md

## Goal

A browser UI for ghr with full parity with the TUI: everything the TUI shows and every action it offers. It is reachable from the LAN or through nginx-proxy-manager, and protected by a password the owner chooses on first visit.

## Decisions

- **The daemon serves the web UI itself, on a second, opt-in TCP listener.** It serves the embedded SPA and mounts the existing `api.NewServer` handler unchanged under `/api/`, behind an auth middleware. A separate `ghr web` process proxying to the socket was rejected: the socket is root-only (0600), so the proxy would run as root anyway, adding a unit and a hop for no isolation.
- **Live data is polled, on the TUI's schedule.** Every live feed already has a pollable endpoint, most with a cursor (§5). Server-sent events were rejected for now: new API surface the TUI would not use, while one-second polling is adequate. Adding SSE later for `/events` and `/runners/{id}/log` is a thin wrapper over their existing cursors.
- **Authentication is one owner-chosen password, set on first visit (trust on first use).** Until it is set, the first visitor to reach the port becomes the admin. The owner accepted this window over a one-time setup code; the README tells them to log in immediately after enabling the listener. `ghr web reset-password` reopens the window.
- **A Host allowlist blocks DNS rebinding.** Without it, any website the owner visits during the setup window could rebind its own hostname to ghr's IP and run setup from a same-origin page, passing both the Origin and `X-GHR` checks. Requests are accepted only for IP-literal hosts, `localhost`, or names listed in `web.hosts` (§2).
- **The password is hashed with PBKDF2-HMAC-SHA256 from the Go standard library** (`crypto/pbkdf2`, in the standard library since Go 1.24; ghr is on 1.26). Argon2id was rejected because it adds `golang.org/x/crypto` for a single-user LAN tool.
- **ghr does not terminate TLS.** nginx-proxy-manager does. On plain HTTP within the LAN, the password and cookie travel in the clear; the README says so.
- **The frontend is a darkraise-ui SPA** scaffolded with `create-darkraise-ui` and embedded in the binary with `go:embed`. Server-rendered Go templates with htmx were rejected in favour of the house UI kit.
- **TypeScript types are hand-written** from `internal/model`, guarded against drift by shared JSON fixtures (§8). Generating them was rejected as tooling the size of the model does not justify.

## 1. Configuration

`config.Config` gains:

```go
Web Web `yaml:"web,omitempty" json:"web,omitzero"`

type Web struct {
	Listen string   `yaml:"listen,omitempty" json:"listen,omitempty"`
	Hosts  []string `yaml:"hosts,omitempty" json:"hosts,omitempty"`
}
```

- Empty `web.listen` (the default) means no TCP listener.
- Validation:
  - `web.listen` must split with `net.SplitHostPort` into a host and a numeric port from 1 to 65535.
  - Each `web.hosts` entry must be a bare hostname: no port, no scheme, not empty.
- `web.listen` and `web.hosts` are read once, at daemon start. A reload that changes either leaves the listener and the allowlist as they are. The warning `web settings changed; restart ghr to apply` is then both appended to the `[]string` that `Backend.Reload` returns (so the CLI and TUI show it) and added as a `warn` event. The comparison is against the values the running listener was started with, so changing and then reverting the file warns once and then stops.
- `model.ConfigPatch` gains no `web` field, so `PATCH /config` cannot change it. `decode` does not reject unknown keys, so a `web` key in a patch body is ignored. `config.Save` marshals the whole `Config`, and `Clone` round-trips through YAML, so the block survives every CLI/TUI/web rewrite of `config.yaml`. `Parse` uses `KnownFields(true)`, so the field must exist before any config file carries it.
- The password hash lives in `/etc/ghr/web-password` (new `daemon.Options.WebPasswordPath`), mode 0600, written atomically (temp file in the same directory + rename).
  - Format: `pbkdf2-sha256$<iterations>$<salt base64>$<hash base64>`.
  - Parameters: 600000 iterations, a 16-byte random salt, a 32-byte key.

## 2. Server (`internal/webui`)

The package has two constructors, so password reset works whether or not the listener is on.

- **`webui.NewAuth(path string, now func() time.Time, rand io.Reader) *Auth`.** `daemon.Run` always builds it. It owns the password file, the session store and the login throttle, and exposes `Reset()` for the socket route (§3).
- **`webui.Handler(a *Auth, api http.Handler, static fs.FS, hosts []string) http.Handler`.** It is built only when `web.listen` is set.
  - The daemon passes `fs.Sub(web.Dist, "dist")` as `static`; tests pass `fstest.MapFS` with and without `index.html`.
  - The daemon binds `net.Listen("tcp", listen)` right after the socket and before any runner is touched. A bind failure fails startup, as a socket bind failure does.
  - The `served`/defer pattern in `run.go` extends to the TCP listener, so an `Init`/`Adopt` failure closes it.
  - On shutdown, the socket server and the TCP server are shut down concurrently, each with the existing 5-second limit.

### The TCP `http.Server`

`ReadHeaderTimeout: 10s`, `ReadTimeout: 30s`, `WriteTimeout: 60s`, `IdleTimeout: 120s`, `MaxHeaderBytes: 64 << 10`.

### Checks, in order, on every request

1. **Host.** The `Host` hostname, with its port stripped, must be an IP literal, `localhost`, or a case-insensitive match for an entry in `web.hosts`. Otherwise the response is 421 with plain text `unknown host; add it to web.hosts`.
2. **`X-GHR` header.** Every `POST` to `/auth/*` and every request to `/api/*` must carry `X-GHR: 1`, else 403. A browser cannot send a custom header cross-origin without a CORS preflight, which ghr never answers with permission.
3. **Origin.** Every request other than `GET`/`HEAD` gets 403 when an `Origin` header is present and its hostname differs from the `Host` hostname. Ports are ignored on both sides, because nginx-proxy-manager forwards `Host $host`, which drops the port the browser used. A reverse proxy must pass the original hostname through.
4. **Session.** Every `/api/*` request needs a valid session, else 401.

### Routes on the TCP listener

| Route | Auth | Purpose |
|---|---|---|
| `GET /auth/state` | none | `{"setup_required": bool, "authenticated": bool}` |
| `POST /auth/setup` `{password}` | none | Only while no password file exists: sets the password and starts a session (204 + cookie). 409 once a password exists; 400 outside 12–1024 bytes. |
| `POST /auth/login` `{password}` | none | 204 + cookie. 401 on a wrong password; 429 with `retry_at` while throttled; 409 while setup is required. |
| `POST /auth/logout` | session | Ends this session and clears the cookie. 204. |
| `POST /auth/password` `{current, new}` | session | Changes the password, ending every other session but keeping this one. 401 on a wrong `current`; 400 outside 12–1024 bytes. |
| any other `/auth/*` | none | JSON 404. |
| `/api/*` | session | `http.StripPrefix("/api", apiHandler)`: the existing handler, unchanged. Unknown paths get the API mux's 404. |
| everything else, `GET`/`HEAD` | none | Static SPA (below). |
| everything else, other methods | none | 405. |

- `/auth/*` request bodies are capped at 4 KB with `http.MaxBytesReader`; `/api/*` keeps the existing caps.
- All `/auth/*` and `/api/*` responses use the existing error body `{"error": ..., "retry_at"?: ...}` and carry `Cache-Control: no-store`.

### Sessions

- A session ID is 32 random bytes, base64url, held in an in-memory map behind a mutex, with a fixed 30-day expiry. Expired sessions are swept on each login and setup.
- Every daemon restart ends every session. That includes each `setup.sh` upgrade and any restart needed to apply `web.*` changes.
- The cookie:
  - Named `ghr_session`, with `Path=/`, `HttpOnly`, `SameSite=Strict` and `Max-Age=2592000`.
  - With `Secure` when `r.TLS != nil` or `X-Forwarded-Proto` is `https`. Trusting that header can only make the cookie stricter.
- Because of `SameSite=Strict`, following a link into ghr from another site (for example a GitHub job page) shows the login page until the page is reloaded. This is accepted.

### Login throttling

- Failed logins are counted per client from `r.RemoteAddr`. `X-Forwarded-For` is not trusted (it is spoofable). IPv4 addresses count individually; IPv6 addresses count by /64.
- After 5 failures within 15 minutes, that client gets 429 with `retry_at` for 15 minutes, even with the right password. A successful login clears its counter.
- Counters older than 15 minutes are swept on each login attempt.
- Behind nginx-proxy-manager, all clients share the proxy's counter, so anyone can lock the owner out with five wrong guesses. The README recommends an nginx-proxy-manager access list in front of the host, or a LAN-only `web.listen`.
- At most 2 PBKDF2 derivations run at once, using a semaphore shared by setup, login and password change. Each one takes roughly half a second of CPU, and the cap keeps a flood from starving the daemon's tick loop.
- Password checks compare derived keys with `subtle.ConstantTimeCompare`.
- Setup and password writes hold a mutex, so two concurrent setups yield one 204 and one 409.

### Static SPA

- Files come from the `static` `fs.FS` through a custom handler, not `http.FileServer`, so there are no directory listings and no `/index.html` → `/` redirect.
- A path naming an existing file is served as is. Files under `/assets/` (hashed names) get `Cache-Control: public, max-age=31536000, immutable`; every other file gets `no-cache`.
- Any other path gets `index.html` with `no-cache`, so client routes survive a reload.
- When `static` holds no `index.html` (a Go-only build), every SPA path gets a plain-text 503: `ghr web UI was not built into this binary`.
- Every response carries:
  - `Content-Security-Policy: default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'`. Inline styles are allowed because darkraise-ui components set style attributes. `data:` images are needed because darkraise-ui's `theme.css` uses them. Scripts are not allowed inline.
  - `X-Content-Type-Options: nosniff` and `Referrer-Policy: same-origin`.

## 3. Password reset (socket only)

- `ghr web reset-password` calls the new `api.Client.ResetWebPassword(ctx)`, which posts to `/web/reset-password` on the Unix socket. `cmd/ghr/cli.go` gains a `web` case, and the usage text in `main.go` gains a line.
- The route calls `Auth.Reset()`, which deletes the password file and ends every web session; the next visit shows setup again. It returns 204 even when no password was set, and it works whether or not the listener is enabled.
- The route lives on a socket-only mux that wraps `api.NewServer(b)`. It is **not** in the handler mounted under `/api/`, so `POST /api/web/reset-password` over TCP returns 404.

## 4. Steps cache

`Backend.RunnerSteps` caches its result per runner ID for 5 seconds, behind a mutex, because socket and TCP clients call it concurrently.
- Errors are not cached.
- Entries for runners no longer in `Status().Instances` are dropped on the next call.

`github.Client.do` already revalidates GETs with `If-None-Match`, and GitHub does not count 304 responses against the rate limit. So the cache's value is fewer round-trips to GitHub when several browser tabs or TUIs watch the same runner, not rate-limit savings.

## 5. Frontend (`web/`)

### Scaffold and additions

- Created by `npx create-darkraise-ui web --layout sidebar -y --no-git` in the ghr repo root. Through `npm create`, the flags would need a `--` separator.
- The scaffold provides darkraise-ui, React and React DOM, Vite, Tailwind 4, TypeScript and ESLint, plus a single welcome page. The plan adds:
  - **Dependencies:** `@tanstack/react-router` (code-based routes), `@tanstack/react-query`, `@fontsource/inter`, `@fontsource/jetbrains-mono`.
  - **Dev dependencies:** `vitest`, `jsdom`, `@testing-library/react`, `@testing-library/user-event`, `@testing-library/jest-dom`.
  - **Config and scripts:** an `eslint.config.js` copied from darkraise-web-template's root config (ESLint 9 will not run without one), a `test` script (`vitest run`), and `web/.nvmrc` pinned to the Node major CI uses.
- `package-lock.json` is committed. There is no `--multilingual`.
- `index.html`:
  - The inline theme-bootstrap script moves to `public/theme-init.js`, loaded with `<script src="/theme-init.js">`, because the CSP forbids inline scripts.
  - The Google Fonts `<link>`s are removed; the fonts come from the `@fontsource` packages, so the UI makes no outside request.
  - The title is `ghr`.
- `vite.config.ts` proxies `/api` and `/auth` to `GHR_DEV_URL` (default `http://localhost:8080`) with `changeOrigin` left false, so the daemon sees `Host: localhost` and the Origin check passes.

### Build output and the Go embed

- `web/embed.go` (`package web`) declares `//go:embed all:dist` `var Dist embed.FS`.
- `web/dist/.gitkeep` is committed so a fresh clone builds and tests without Node. Vite empties `dist/` on build, so the `build` script ends by recreating it: `vite build && tsc --noEmit && node -e "require('fs').writeFileSync('dist/.gitkeep','')"`.
- `web/.gitignore` ignores `dist/*` except `!dist/.gitkeep`, replacing the scaffold's bare `dist` line. The root `.gitignore` entry `dist/` becomes `/dist/` (the release staging folder).

### API layer (`web/src/api/`)

- **`client.ts`** is a `fetch` wrapper. It sends `X-GHR: 1` and `credentials: "same-origin"`.
  - A 401 from an `/api/*` request clears the query cache and navigates to `/login`.
  - A 401 from `/auth/*` (wrong password at login, wrong current password) is not a redirect: like every other non-2xx response, it throws an `ApiError {status, message, retryAt?}` parsed from the error body. Forms show it inline; mutations show it as a toast.
- **`types.ts`** holds hand-written mirrors of the `internal/model` types the UI reads (including `model.Storage`), plus `config.Config`.
- **One TanStack Query hook per endpoint.** Polling pauses while the tab is hidden (TanStack's default). The cadence follows the TUI (`internal/tui/model.go`, `tick` and `slowPoll`):

| Data | Endpoint | Interval |
|---|---|---|
| Status (runners, queue, repos, degraded, disk, rate, runner update, maintenance) | `GET /status` | 1 s, always |
| Events | `GET /events?after=<seq>` | 1 s, always |
| Log tail | `GET /runners/{id}/log?cursor=` | 1 s while a log view is open |
| Label check | `GET /repos/{name}/label-check` | 1 s while a check is running |
| Steps, containers | `/runners/{id}/steps`, `/containers` | 5 s on runner detail while the runner is live |
| Config, metrics | `GET /config`, `GET /metrics` | 5 s, always |
| Storage | `GET /storage` | 5 s on Storage; 1 s while an operation or measurement runs |
| Installable versions | `GET /toolchains/available?tool=` | fetched when the Install dialog picks a tool; not polled |
| History | `GET /history?repo=&conclusion=&limit=200` | 5 s on History (200, as the TUI; `limit=0` would return the whole file) |
| Token status | `GET /token` | 5 s on Settings |
| Repository activity | `GET /history?repo=<name>&limit=500` per repo card (the TUI's `fetchActivity` call; history is a local file, so one call per repo is cheap) | 5 s on Repositories |

- **`useEvents`** keeps `lastSeq` and appends new events, capped at the last 200. When `status.epoch` changes (the daemon restarted, so sequence numbers start over), it clears the list, resets the cursor to 0, and refetches config.
- **`useLogTail(id)`** keeps the `next` cursor and appends `data`. A follow toggle keeps the view scrolled to the end.
- **A failed status poll** shows a "reconnecting" banner, as the TUI's page header does, until a poll succeeds.

### Pages

The sidebar mirrors the TUI's pages (`pageNames` in `internal/tui/model.go`): Dashboard, Repositories, Runners, History, Storage, Settings.

- **Setup / Login** (`/login`). Shows setup (password plus confirmation) while `/auth/state` reports setup required, otherwise login. Wrong-password and 429 errors show inline, the latter with when to retry.
- **Dashboard**:
  - Status chips: mode, global max, degraded with its reason, rate limit remaining, disk, runner-update badge.
  - The metrics tiles, matching `internal/tui/dashboard.go`: live and queued sparklines, CPU, memory.
  - Runners in flight, the per-repo queue, and the live event feed.
  - Pause all / Resume all.
- **Repositories**. A card per repo with its recent activity. Actions:
  - Add: a filterable picker over `/repos/available`, then max and labels, as in the TUI dialog.
  - Edit max, warm, labels and cleanup prefixes.
  - Pause/resume and remove.
  - Label check with live progress, including the TUI's quick-add of missing labels (`manage.go`, `dialogs.go`).
  - The GitHub runner registration list, with delete.
- **Runners**. A table of instances with row actions: logs, stop, copy ID.
- **Runner detail** (`/runners/$id`). Instance facts, job steps, containers, the live log tail with follow, and Stop. After the runner ends, the page keeps its last snapshot, as the TUI's `detailSnap` does.
- **History**. A table filtered by repo and by conclusion.
- **Storage**: the four cards of the TUI's Storage page (docs/superpowers/specs/2026-10-06-ghr-toolchains-caches-design.md §6): Docker disk with its four prune actions and the last prune, Toolchains with Install (tool select, then a searchable version list), Install popular set and Remove, Package caches with Clear and Refresh, and Recent operations.
- **Settings**:
  - Every field the TUI's Settings page edits, saved through `PATCH /config`.
  - Token status, and Replace token (`PUT /token`).
  - Runner version, with Queue update / Cancel update.
  - Reload config.
  - Change password and Log out.

Stop runner, remove repo, delete registration, replace token, each prune scope, remove toolchain, clear cache and install popular set each open a confirmation dialog before the request is sent.

## 6. Error handling

- A bind failure on `web.listen` (port in use, bad address) fails daemon start with the error, like a socket bind failure; `journalctl -u ghr` shows it.
- An unreadable or malformed password file: login and setup return 500 with `web password file is unreadable; run ghr web reset-password`, and a `warn` event records it at start. A missing password file is the setup state, not an error.
- API errors pass through `/api` unchanged. The SPA shows them as toasts, and a 429 from GitHub shows its `retry_at`.

## 7. CLI

`ghr web reset-password` is the only new subcommand (§3). No subcommand enables the listener; the owner edits `web.listen` (and `web.hosts`) and restarts ghr.

## 8. Testing

### Go (`internal/webui`, `internal/daemon`, `internal/api`, `cmd/ghr`)

- **Password store:** hash round-trip; malformed file; atomic write leaves mode 0600.
- **Setup:** allowed only without a password file; 409 afterwards; 400 under 12 or over 1024 bytes; concurrent setups yield one 204 and one 409.
- **Login:**
  - correct → 204 with cookie; wrong → 401;
  - the 6th failure inside 15 minutes → 429 with `retry_at`, even with the right password;
  - the counter clears after a success and after the window (fake clock);
  - stale counters are swept;
  - two IPv6 addresses in one /64 share a counter.
- **Sessions:** expiry after 30 days (fake clock); logout ends only that session; a password change ends others but keeps the caller's; a wrong `current` gets 401 and keeps the session; `Reset()` ends all.
- **Cookie:** `HttpOnly`, `SameSite=Strict`, `Max-Age`; `Secure` with TLS or `X-Forwarded-Proto: https`, and without it otherwise.
- **Request checks:**
  - an unlisted hostname → 421; an IP literal, `localhost` and a listed name pass;
  - `/api/status` without `X-GHR` → 403, before any session check;
  - without a session → 401;
  - a `POST` with a foreign `Origin` → 403;
  - a `POST` whose `Origin` and `Host` share a hostname but differ in port passes;
  - a valid request reaches the API handler with the prefix stripped;
  - an `/auth/*` body over 4 KB → 400.
- **Static** (against `fstest.MapFS`):
  - an existing file is served, with the right cache headers inside and outside `/assets/`;
  - an unknown client path → `index.html`;
  - `/index.html` is served without a redirect;
  - a directory path → `index.html`, not a listing;
  - unknown `/api/x` → the API's 404; unknown `/auth/x` → JSON 404;
  - a `POST` to an SPA path → 405;
  - an FS without `index.html` → 503;
  - CSP and the other headers are present.
- **Socket-only route:** `POST /web/reset-password` works on the socket handler with the listener disabled; `POST /api/web/reset-password` over the TCP handler → 404; `api.Client.ResetWebPassword` posts to it.
- **Daemon:**
  - no TCP listener when `web.listen` is empty;
  - the TCP listener is closed when `Init` fails and after `ctx` is cancelled;
  - a reload that changes `web.*` returns the warning and keeps the listener, while change-then-revert warns once;
  - a patch body carrying `web` leaves it unchanged;
  - `config.Save` round-trips the `web` block;
  - `GET /config` omits an empty `web`;
  - validation rejects a non-numeric port and a host entry with a port.
- **Steps cache:** two calls within 5 seconds make one GitHub call; an error is not cached; a call after 5 seconds refetches; concurrent calls are race-free under `-race`.
- **CLI:** `ghr web reset-password` posts to the socket route.
- **Type fixtures:** a Go test marshals one representative value of each model type the UI reads (and `config.Config`) into `web/src/api/fixtures/*.json`. It fails, with instructions to regenerate, when a committed fixture differs from what the Go types produce.

### Frontend (vitest + Testing Library, in `web/`)

- **`client.ts`:**
  - sends `X-GHR`;
  - an `/api` 401 navigates to `/login`;
  - an `/auth/login` 401 and an `/auth/password` 401 throw `ApiError` without navigating;
  - error bodies become `ApiError` with `retryAt`.
- **Hooks:** `useEvents` appends by cursor, resets on an epoch change and caps at 200. `useLogTail` advances the cursor, and follow keeps the view at the end.
- **Type fixtures:** each fixture parses into its `types.ts` type, and the fields the UI reads are present. `model.Storage` is among the fixtures.
- **Confirmations:** each destructive action opens its confirmation dialog and sends nothing until it is confirmed.
- **Pages:** each page renders against mocked responses, including the empty and disconnected states. The login page shows a wrong-password error inline.

### Manual, before release

On the runner LXC (192.168.0.99):
1. Set `web.listen` (and `web.hosts` if a proxy hostname is used), restart ghr, and set the password.
2. Watch a real job's runner, steps and log tail update live.
3. Pause and resume a repo; change the password, then reset it.
4. Confirm the 503 page does not appear (the release embeds the UI) and the browser console shows no CSP violations.
5. Repeat the login and one write through nginx-proxy-manager on its hostname.

## 9. Delivery

### ghr repo, on a feature branch

- `.github/workflows/ci.yml`:
  - **`test` job:** the Go steps (gofmt, vet, `go test -race`) run first. Then `actions/setup-node` with `node-version-file: web/.nvmrc`, and in `web/`: `npm ci`, `npm run lint`, `npm run typecheck`, `npm test`. Running Go first keeps Go tooling from walking `web/node_modules`.
  - **`release` job:** also runs `actions/setup-node`, then `npm ci && npm run build` in `web/` before `go build`, so release binaries embed the UI.
- The Go `test` job does not need the UI built, because `.gitkeep` keeps the embed valid and the static tests use `fstest.MapFS`.

### homelab `github-runner/`

- `config.example.yaml`: a commented `web:` block with `listen: 0.0.0.0:8080` and `hosts: [ghr.lan]`.
- `README.md`: a "Web UI" section covering:
  - enabling the listener and restarting;
  - logging in immediately, because the first visitor sets the password, and that `ghr web reset-password` reopens that window;
  - adding any proxy hostname to `web.hosts`;
  - that HTTP inside the LAN is unencrypted, so use nginx-proxy-manager for TLS;
  - putting an nginx-proxy-manager access list in front of the host, because behind the proxy all clients share one login throttle;
  - that restarts and upgrades log you out.
- `setup.sh` needs no change.

## Out of scope

- Server-sent events or WebSockets.
- Built-in TLS.
- Multiple users or roles.
- Translations.
- A mobile-specific layout (the template's responsive behaviour only).
- The TUI's keyboard help overlay.
- Changing `web.*` settings without a restart.
