# ghr web UI: realtime updates

**Date:** 2026-10-10
**Status:** approved in chat on 2026-10-10 (scope, approach and the full design).
**Register:** docs/superpowers/registers/2026-10-10-ghr-ui-polish-realtime.md (item 3)
**Code:** `internal/changes` (new), `internal/api`, `internal/daemon`, `internal/events`, `internal/history`, `internal/webui`, `web/src`

## Goal

The web UI learns about daemon state only from timers: status and events every second, config, metrics, activity, history, token and storage every 5 seconds. The owner wants the UI to update the moment the daemon's state changes. This spec adds a push channel from the daemon to the browser that says what changed, so the browser refetches it at once.

## Decisions

- **Daemon state only.** GitHub-backed data (the Actions page, run and job pages) keeps its 15-second polling and cache; nothing here spends GitHub API budget.
- **Notify, then refetch.** The stream names topics that changed; the browser refetches through the existing endpoints. It does not carry the data, so every page keeps its one data path. Chosen over pushing payloads and over publish calls at every mutation site.
- **One watcher detects changes** by fingerprinting each topic every 250 ms, woken early by new events and history rows. Chosen over instrumenting the runner manager, config store, storage service and sampler.
- **Server-sent events read with `fetch`.** Every `/api` request must carry `X-GHR: 1`, which `EventSource` cannot send.
- **Polling stays as the fallback.** While the stream is up, daemon-state queries poll every 60 s as a safety net; while it is down, they poll at today's intervals.

## 1. Topics

| Topic | Fingerprint | Early wake |
|---|---|---|
| `status` | FNV-64a of the JSON of `Backend.Status()` with `Now` zeroed | - |
| `events` | `events.Ring.Last()`, the last sequence number | `Ring.Add` |
| `config` | FNV-64a of the JSON of `Backend.Config()` | - |
| `token` | FNV-64a of the JSON of `Backend.Token()` | - |
| `storage` | FNV-64a of the JSON of `Backend.Storage()` | - |
| `history` | `history.Store.Writes()`, a counter bumped by `Append` and `Prune` | `Append`, `Prune` |
| `metrics` | FNV-64a of the JSON of `Backend.Metrics()` | - |

- Topic names are lowercase ASCII and are the wire values.
- A fingerprint read that panics or errors is skipped for that tick and logged once per topic until it succeeds again; it never stops the watcher.
- `Ring.Last()` and `Store.Writes()` are new methods. `Ring` and `Store` gain an `OnChange func()` field, called after the lock is released, nil-safe.

## 2. Daemon

### 2.1 Watcher and hub (`internal/changes`)

- `changes.Hub` holds subscribers and runs the watcher.
  - `Subscribe() (*Sub, error)` returns `ErrFull` when 32 subscribers are open. `Sub.C` is a `<-chan []string` buffered to 1, each value a sorted, de-duplicated topic set; `Sub.Close()` unsubscribes.
  - A subscriber that has not drained its channel gets its pending set merged with the new one rather than blocking the watcher, so a slow browser loses no topic.
  - `Wake()` asks for an immediate check; non-blocking, coalescing.
  - `Close()` ends every subscription (their channels close) and stops the watcher. Safe to call twice.
- The watcher goroutine runs only while at least one subscriber is open. On the first subscribe it records every fingerprint as the baseline without publishing; it stops when the last subscriber closes.
- It checks every 250 ms and on `Wake()`. Topics whose fingerprints differ from the last check are published together as one set.
- Sources are injected as a `map[string]func() (uint64, error)`, so the package does not import the daemon and tests use fakes. The clock and tick are injectable.
- Before publishing a set containing `status`, `history` or `metrics`, the hub calls an injected `BeforePublish(topics)`; the daemon uses it to mark every `Backend` activity cache entry stale, so the refetch does not return a 5-second-old activity view.

### 2.2 `GET /changes`

Added to `api.NewServer` through a new `Backend.Subscribe() (*changes.Sub, error)`.

- `ErrFull` answers 503 `{"error":"too many live connections"}`.
- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-store`, `X-Accel-Buffering: no`. Status 200, flushed at once.
- First message: `event: hello` with `data: {"epoch":"<status epoch>"}`.
- Each set: `event: change` with `data: {"topics":["config","status"]}`, topics sorted.
- Every 15 s without a change: the comment line `: ping`.
- Before every write the handler extends the write deadline by 30 s through `http.ResponseController`; the Unix socket listener has none, and the web listener's 60 s `WriteTimeout` would otherwise end the stream. A failed write or flush ends the handler.
- The handler ends when the request context ends, when the subscription channel closes, or on a write error, and always calls `Sub.Close()`.

### 2.3 Wiring and shutdown

- `daemon.Run` builds the hub with the sources in §1 and passes it to `Backend`. `events.Ring.OnChange` and `history.Store.OnChange` call `hub.Wake`.
- Both `http.Server`s register `hub.Close` with `RegisterOnShutdown`, so open streams end and `Shutdown` is not held to its deadline.

### 2.4 Sessions on long requests (`internal/webui`)

- `requireSession` checks the session at the start, as today. While the request is still running it re-checks `Auth.Valid` every 5 s; on a failure it cancels the request's context, which ends the stream (and the raw log download, the other long `/api` request).
- Logout, a password change or reset, and session expiry therefore end an open stream within 5 s. The re-check stops when the handler returns.

## 3. Browser

### 3.1 Stream client (`web/src/api/live.ts`)

- `connectChanges(onHello, onChange, signal)` calls `fetch("/api/changes", { headers: { "X-GHR": "1" }, credentials: "same-origin", signal })` and parses the body as server-sent events: lines split across chunks are joined, comment lines are ignored, an event ends at a blank line, unknown event names are ignored.
- A 401 calls the existing unauthorized handler and stops reconnecting.
- Any other end (network drop, 503, daemon restart) reconnects after 1 s, doubling to at most 30 s, and resets to 1 s after a `hello`.

### 3.2 Live state and invalidation

- A module store exposes `useLive(): boolean`, true from a `hello` until the stream ends.
- `Shell` mounts `useChangeStream()` once, next to `useMetrics` and `useEvents`.
- On `change`, it invalidates by topic:
  - `status`: `keys.status` and every `activity` query
  - `events`: the current `events` query
  - `config`: `keys.config`
  - `token`: `keys.token`
  - `storage`: `keys.storage`
  - `history`: every `history` and `activity` query
  - `metrics`: `keys.metrics` and every `activity` query
- On every `hello` it invalidates all of the queries above, because changes may have been missed while disconnected. A changed epoch is already handled by `Shell`.

### 3.3 Polling

- `useStatus`, `useEvents`, `useConfig`, `useMetrics`, `useToken`, `useActivity`, `useHistory` and `useStorage` poll every 60 s (`POLL_SAFETY`) while `useLive()` is true, and at their current intervals otherwise. `useStorage`'s 1-second busy polling also yields to the stream while live.
- Unchanged: `useActions`, `useRunDetail`, `useRunAnnotations`, `useJobLog`, `useLogTail`, `useSteps`, `useContainers`, `useLabelCheck`, `useRegistrations`, `useSetupState`. Steps and containers come from Docker and the runner log from disk, not from daemon memory.

## 4. Testing

### Go

- `internal/changes`: a changed fingerprint publishes its topic; an unchanged one does not; several topics in one check form one set; the first check after subscribe publishes nothing; `Wake` checks before the tick; a source error skips that topic only; the 33rd subscribe gets `ErrFull`; a full subscriber channel merges sets; `Close` closes every channel and is idempotent; the watcher stops with the last subscriber; `BeforePublish` runs for `status`, `history` and `metrics` sets only.
- `internal/api`: `GET /changes` sends the headers and the `hello` frame, then a `change` frame for a published set, a `: ping` after the heartbeat interval (shortened in tests), 503 on `ErrFull`, and returns and closes the subscription when the client disconnects or the hub closes.
- `internal/events` and `internal/history`: `Last`, `Writes` and `OnChange`.
- `internal/daemon`: `Status` fingerprints ignore `Now`; the activity cache is rebuilt after `BeforePublish`.
- `internal/webui`: a long `/api` request is cancelled within one re-check after logout, and after the session expires; a short request starts no re-check after it returns.

### Frontend

- The parser: one event split across chunks, two events in one chunk, comment lines, CRLF line endings, an unknown event name.
- Reconnect backoff doubles to 30 s and resets after `hello`; a 401 stops reconnecting.
- Each topic invalidates the queries in §3.2; `hello` invalidates all of them.
- Polling intervals switch between live and not live for every hook in §3.3, and the hooks left unchanged keep their intervals.

### Manual, before release

- With the daemon running and the Dashboard open, stopping a runner and pausing a repository show in the UI in under a second; the browser network panel shows one long `changes` request and no 1-second polling.
- Logging out in another tab ends the stream within 5 seconds.

## Out of scope

- Pushing GitHub-backed data (Actions list, run and job pages).
- Pushing a runner's live log, steps or containers.
- A multi-daemon or multi-process fan-out; ghr runs one daemon.
