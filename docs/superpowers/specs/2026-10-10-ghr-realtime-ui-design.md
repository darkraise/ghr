# ghr web UI: realtime updates

**Date:** 2026-10-10
**Status:** approved in chat on 2026-10-10 (scope, approach and the full design). Revised the same day for the Fable review (findings 1 to 19, docs/superpowers/notes/2026-10-10-ghr-realtime-ui-fable-review.md).
**Register:** docs/superpowers/registers/2026-10-10-ghr-ui-polish-realtime.md (item 3)
**Code:** `internal/changes` (new), `internal/api`, `internal/daemon`, `internal/events`, `internal/history`, `internal/webui`, `web/src`

## Goal

The web UI learns about daemon state only from timers: status and events every second, config, metrics, activity, history, token and storage every 5 seconds. The owner wants the UI to update the moment the daemon's state changes. This spec adds a push channel from the daemon to the browser that says what changed, so the browser refetches it at once.

## Decisions

- **Daemon state only.** GitHub-backed data (the Actions page, run and job pages) keeps its 15-second polling and cache; nothing here spends GitHub API budget.
- **Notify, then refetch.** The stream names topics that changed; the browser refetches through the existing endpoints. It does not carry the data, so every page keeps its one data path. Chosen over pushing payloads and over publish calls at every mutation site.
- **One watcher detects changes** by fingerprinting each topic every 250 ms, woken early by new events and history rows. Chosen over instrumenting the runner manager, config store, storage service and sampler.
- **Server-sent events read with `fetch`.** Every `/api` request must carry `X-GHR: 1`, which `EventSource` cannot send.
- **Polling stays as the fallback.** While the stream is up and the tab is visible, daemon-state queries poll every 60 s as a safety net; otherwise they poll at today's intervals.
- **Fast-moving counters are their own topic.** The GitHub rate counters and disk byte counts change on almost every tick, so they publish `gauges`, which refetches only status and token, never the activity views.

## 1. Topics

The daemon computes all fingerprints in one pass per check (§2.3). Each fingerprint is FNV-64a of the JSON of a projection, except where a counter is named.

| Topic | Fingerprint | Early wake |
|---|---|---|
| `status` | `Status` with `Now`, `RateRemaining`, `RateLimit`, `DiskUsedBytes` and `DiskTotalBytes` zeroed | - |
| `gauges` | `Status.RateRemaining`, `RateLimit`, `DiskUsedBytes`, `DiskTotalBytes`, and `TokenStatus.RateRemaining`, `RateLimit`, `RateReset` | - |
| `events` | counter: `events.Ring.Last()`, the last sequence number | `Ring.Add` |
| `config` | `Backend.Config()` | - |
| `token` | `TokenStatus` with `RateRemaining`, `RateLimit`, `RateReset` and `CheckedAt` set to nil | - |
| `storage` | `Backend.Storage()` | - |
| `history` | counter: `history.Store.Writes()` | `Append`, `Prune` |
| `metrics` | `Backend.Metrics()` | - |

- `TokenStatus.CheckedAt` is in no fingerprint. Every GitHub response moves it, about 15 times per poll cycle on an idle daemon, and each `gauges` notice refetches `keys.status`; the 60 s safety poll (§3.3) keeps the token's check time fresh instead. Amended 2026-10-10 after the manual checks found an idle daemon refetching status about once a second.
- Topic names are lowercase ASCII and are the wire values.
- A topic whose fingerprint cannot be computed in a check (an error, or a panic recovered in the daemon's source function) is left out of that check's map; the hub skips it and logs once per topic until it is computed again. It never stops the watcher.
- New methods: `events.Ring.Last() int64`; `history.Store.Writes() int64`, bumped only when `Append` writes a line or `Prune` replaces the file, not on a duplicate append or a prune with no file.
- New hooks: `events.Ring.SetOnChange(func())` and `history.Store.SetOnChange(func())`, stored and read under each type's own mutex and called after the mutex is released. A nil hook is a no-op.

## 2. Daemon

### 2.1 Watcher and hub (`internal/changes`)

- `changes.New(opts)` returns a `*Hub`. Options: the tick (250 ms), the wake coalescing window (50 ms), a clock, a logger, and `BeforePublish func(topics []string)`.
- `Hub.SetSources(func() map[string]uint64)` installs the function that computes every fingerprint. It is guarded by the hub mutex.
- `Subscribe() (*Sub, error)`:
  - returns `ErrNotReady` before `SetSources`, `ErrFull` when 32 subscribers are open, and `ErrClosed` after `Close`;
  - when it opens the first subscriber, it computes the baseline fingerprints synchronously, before returning, then starts the watcher goroutine;
  - `Sub.C` is a `<-chan []string` buffered to 1; each value is a sorted, de-duplicated topic set; `Sub.Close()` unsubscribes and is safe to call twice.
- Concurrency rules:
  - One hub mutex serialises publishing, `Sub.Close`, `Subscribe` and `Hub.Close`. The hub is the only goroutine that sends on or closes a subscriber channel, and only while holding the mutex, so a send on a closed channel cannot happen.
  - Publishing to a subscriber whose buffer is full merges instead of blocking: a non-blocking receive of the pending set, the union with the new set, then a send. A slow browser loses no topic.
  - `Wake()` takes no lock: it is a non-blocking send on a channel buffered to 1. Calling it from inside `Ring.Add` or `history.Store.Append` therefore cannot block their callers or deadlock.
- The watcher checks every tick and on a wake. A wake runs a check at most once per 50 ms, so a burst of events causes at most one check per window. Topics whose fingerprints differ from the previous check are published together as one set.
- When the last subscriber closes, the watcher stops and the hub discards its fingerprints, so the next first subscriber takes a fresh baseline.
- `BeforePublish` runs before a set is sent when the set contains `status`, `config`, `history` or `metrics`; never for a set holding only `gauges`, `events`, `token` or `storage` topics.
- `Close()` ends every subscription (their channels close), stops the watcher and waits for it to return, so no source runs after `Close` returns. Safe to call twice.

### 2.2 `GET /changes`

Added to `api.NewServer` through a new `Backend.Subscribe() (*changes.Sub, error)`.

- `ErrNotReady`, `ErrFull` and `ErrClosed` answer 503 with `{"error":"not ready"}`, `{"error":"too many live connections"}` and `{"error":"shutting down"}`.
- Headers: `Content-Type: text/event-stream`, `Cache-Control: no-store`, `X-Accel-Buffering: no`. Status 200, flushed at once.
- First message: `event: hello` with `data: {}`.
- Each set: `event: change` with `data: {"topics":["config","status"]}`, topics as the hub sent them.
- Every 15 s without a change: the comment line `: ping`.
- Before every write the handler extends the write deadline by 30 s through `http.ResponseController`. An error from `SetWriteDeadline` is ignored (the Unix socket has no deadline, and test recorders do not support it); an error from a write or a flush ends the handler.
- The handler ends when the request context ends, when the subscription channel closes, or on a write error, and always calls `Sub.Close()`.

### 2.3 Wiring and shutdown

- `daemon.Run` builds the hub before any listener serves, and in the same place, still single-threaded:
  - sets `ev.SetOnChange(hub.Wake)`;
  - registers `hub.Close` with `RegisterOnShutdown` on both `http.Server`s, so a teardown during first-run setup also ends open streams.
- `start()` sets `hist.SetOnChange(hub.Wake)`, gives the hub to `Backend`, and calls `hub.SetSources` with the daemon's fingerprint function.
- During first-run setup there are no sources, so `GET /changes` answers 503 and the browser keeps today's polling, retrying with backoff; within 30 s of `start()` a retry goes live.
- The fingerprint function, in `internal/daemon`:
  - calls `M.Status()` once per check, and derives the `status` and `gauges` projections, the storage snapshot's `DiskPct` and the token's degraded state from that one result instead of calling `Backend.Status()`, `Backend.Storage()` and `Backend.Token()`;
  - reads `WebSetupRequired` and `SetupPending`, which read the password file and stat the setup marker, at most once per second, reusing the last values in between;
  - recovers a panic from any one topic and leaves that topic out of the map.
- `BeforePublish` bumps an activity generation counter. `Backend.Activity` captures the generation before it builds and stores it on the cache entry. An entry built under an older generation is stale whatever its age, so a build that started before a change is never reused after it.

### 2.4 Sessions on long requests (`internal/webui`)

- `requireSession` captures the session cookie's value when the request starts and checks it as today. It then wraps the request context with a cancel function and arms a `time.AfterFunc` at `sessionRecheck` (a package variable, 5 s, which tests shorten). The timer function calls `Auth.Valid` with the captured value; if the session is gone it cancels the context, and otherwise it re-arms itself.
- The handler runs as `next.ServeHTTP(w, r.WithContext(ctx))`; when it returns, the timer is stopped and the context cancelled.
- Logout, a password change or reset, and session expiry therefore end an open stream, or a raw log download, within 5 s. A short request starts one timer and stops it on return.

## 3. Browser

### 3.1 Stream client (`web/src/api/live.ts`)

- `connectChanges({ onHello, onChange, onAlive, signal })` calls `fetch("/api/changes", { headers: { "X-GHR": "1" }, credentials: "same-origin", signal })` and parses the body as server-sent events:
  - bytes go through a `TextDecoder` with `stream: true`, so a character split across chunks decodes correctly;
  - lines end at LF or CRLF, and a line split across chunks is joined;
  - a line starting with `:` is a comment and calls `onAlive`;
  - `event:` sets the event name; `data:` lines, with one leading space stripped, are joined with LF; a blank line dispatches the event;
  - `hello` calls `onHello`; `change` parses the data as JSON and calls `onChange(topics)`; unknown names and unparseable data are ignored.
- A 401 calls the existing unauthorized handler and does not reconnect.
- An abort through `signal` ends quietly: no reconnect, the reader is cancelled.
- Any other end (network drop, 503, daemon restart, a stream that ends without error) reconnects after 1 s, doubling to at most 30 s. The delay goes back to 1 s only after the connection has delivered a `change` or a `: ping`, so a proxy that accepts and drops right after `hello` cannot make a 1 s loop.

### 3.2 Live state and invalidation

- A module store exposes `useLive(): boolean`. It is true from a `hello` until the stream ends or the tab is hidden.
- `Shell` mounts `useChangeStream()` once, next to `useMetrics` and `useEvents`.
- Hidden tabs: while `document.visibilityState` is `"hidden"` the stream is aborted and `useLive()` is false. When the tab becomes visible again the stream reconnects. This keeps a background tab from refetching on every change, and keeps several open tabs from tying up the browser's six HTTP/1.1 connections to the host.
- On a `change` event, each topic invalidates these queries:

  | Topic | Queries invalidated |
  |---|---|
  | `status` | `keys.status`, and every query with the `["activity"]` prefix |
  | `gauges` | `keys.status`, `keys.token` |
  | `events` | every query with the `["events"]` prefix |
  | `config` | `keys.config`, and every `["activity"]` query |
  | `token` | `keys.token` |
  | `storage` | `keys.storage` |
  | `history` | every `["history"]` and `["activity"]` query |
  | `metrics` | `keys.metrics`, and every `["activity"]` query |

- On a `hello` after a previous connection (a reconnect, or the tab becoming visible again), the stream invalidates every query listed above, because changes may have been missed. The first `hello` after mount invalidates nothing; the queries mounted with it are already fetching.

### 3.3 Polling

- These hooks poll every 60 s (`POLL_SAFETY`) while `useLive()` is true, and at their current intervals otherwise: `useStatus`, `useEvents`, `useConfig`, `useMetrics`, `useToken`, `useActivity`, `useHistory`, `useStorage`. `useStorage`'s 1-second busy polling also gives way to the stream while live.
- These hooks are unchanged: `useActions`, `useRunDetail`, `useRunAnnotations`, `useJobLog`, `useLogTail`, `useSteps`, `useContainers`, `useLabelCheck`, `useRegistrations`, `useSetupState`. Steps and containers come from Docker and the runner log from disk, not from daemon memory.

## 4. Testing

### Go

- `internal/changes`:
  - a changed fingerprint publishes its topic; an unchanged one does not;
  - several topics changed in one check form one set;
  - `Subscribe` takes the baseline before it returns, so a change made right after `Subscribe` returns is published;
  - after the last subscriber closes and a new one subscribes, a fresh baseline is taken;
  - a wake checks before the tick, and a burst of wakes inside 50 ms causes one check;
  - a topic missing from the sources' map is skipped while the others publish;
  - `Subscribe` before `SetSources` gets `ErrNotReady`;
  - the 33rd subscribe gets `ErrFull`; a subscribe after `Close` gets `ErrClosed`;
  - a full subscriber channel merges sets;
  - `Close` during a publish, and `Wake` during `Close`, neither panic nor deadlock (run with `-race`);
  - `Close` closes every channel, waits for the watcher, and is idempotent;
  - `BeforePublish` runs for sets holding `status`, `config`, `history` or `metrics`, and not for a set of only `gauges`.
- `internal/api`, using `httptest.NewServer` so disconnects and flushes are real:
  - `GET /changes` sends the headers and the `hello` frame;
  - a published set becomes a `change` frame;
  - a `: ping` arrives after the heartbeat interval (shortened in tests);
  - `ErrNotReady`, `ErrFull` and `ErrClosed` answer 503;
  - the handler returns and closes the subscription when the client disconnects and when the hub closes;
  - a `ResponseWriter` without write-deadline support still streams.
- `internal/events` and `internal/history`: `Last`, `Writes` (not bumped by a duplicate append or a prune with no file) and `SetOnChange`, under `-race`.
- `internal/daemon`:
  - an idle daemon whose rate counter and disk bytes change publishes only `gauges` across several checks;
  - a token check that moves only `CheckedAt` publishes nothing;
  - `status` ignores `Now`;
  - the fingerprint function calls `M.Status()` once per check;
  - an activity build that began before `BeforePublish` is not reused after it.
- `internal/webui`:
  - a long `/api` request is cancelled within one re-check after logout, and after the session expires;
  - a request that returns stops its timer.

### Frontend

- The parser: one event split across chunks, a multi-byte character split across chunks, two events in one chunk, comment lines calling `onAlive`, CRLF line endings, an unknown event name, unparseable `change` data.
- Reconnects:
  - backoff doubles to 30 s;
  - the delay resets only after a `change` or a ping, not after a bare `hello`;
  - a 401 stops reconnecting;
  - an abort does not reconnect.
- Hidden tabs: hiding the tab aborts the stream and makes `useLive()` false; showing it reconnects, and the second `hello` invalidates every daemon-state query.
- Each topic invalidates exactly the queries in §3.2; the first `hello` invalidates none.
- Polling switches between live and not live for every hook in §3.3, and the hooks left unchanged keep their intervals.

### Manual, before release

- With the daemon running and the Dashboard open:
  - stopping a runner and pausing a repository show in the UI in under a second;
  - the browser network panel shows one long `changes` request, and no 1-second polling while the daemon is idle.
- Logging out in another tab ends the stream within 5 seconds.
- Six tabs open on ghr, one of them visible: the visible tab still loads pages normally.

## Out of scope

- Pushing GitHub-backed data (Actions list, run and job pages).
- Pushing a runner's live log, steps or containers.
- A multi-daemon or multi-process fan-out; ghr runs one daemon.
