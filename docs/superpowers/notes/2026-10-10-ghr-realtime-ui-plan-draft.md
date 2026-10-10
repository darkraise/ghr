# Realtime UI plan draft

**Date:** 2026-10-10
**Spec:** docs/superpowers/specs/2026-10-10-ghr-realtime-ui-design.md (approved by the owner, Fable review applied)
**Register:** docs/superpowers/registers/2026-10-10-ghr-ui-polish-realtime.md row 3 (rows 1 and 2 done)
**Target plan:** docs/superpowers/plans/2026-10-10-ghr-realtime-ui.md (not written yet)

Decisions taken while exploring the code for the plan. The plan still needs full code per task, scoring lines, lint and review.

## Tasks (scored against ladder.md, Rule S clean)

| # | Task | Files | Score | Implementer |
|---|---|---|---|---|
| 1 | Event ring change hook | `internal/events/events.go`, `events_test.go` | 1-0-2-1 = 4 | sonnet-medium |
| 2 | History write counter | `internal/history/history.go`, `history_test.go` | 1-0-2-1 = 4 | sonnet-medium |
| 3 | Change hub | `internal/changes/hub.go`, `hub_test.go` (new) | 1-0-2-3 = 6 | opus-high |
| 4 | Backend helpers: activity generation, `tokenStatus`/`storageFrom` split, fake manager fields | `internal/daemon/backend.go`, `backend_test.go` | 1-0-2-2 = 5 | sonnet-high |
| 5 | Daemon fingerprints and `Subscribe` | `internal/daemon/changes.go`, `changes_test.go` (new) | 1-0-2-1 = 4 | sonnet-medium |
| 6 | `GET /changes` route | `internal/api/server.go`, `api_test.go` (fake), `changes_test.go` (new) | 1-0-2-1 = 4 | sonnet-medium |
| 7 | Daemon wiring and shutdown | `internal/daemon/run.go`, `run_test.go` | 1-0-2-3 = 6 | opus-high |
| 8 | Session re-check on long requests | `internal/webui/handler.go`, `handler_test.go` | 1-0-0-3 = 4 | sonnet-medium (heavy: risk 3) |
| 9 | Browser stream client and live store | `web/src/api/live.ts`, `live.test.ts`, `client.ts` | 1-0-2-1 = 4 | sonnet-medium |
| 10 | Change stream hook, invalidation, Shell mount, test switch | `web/src/api/use-change-stream.ts`, its test, `shell.tsx`, `shell.test.tsx`, `src/test/setup.ts` | 2-0-1-1 = 4 | sonnet-medium |
| 11 | Polling gives way to the stream | `web/src/api/hooks.ts`, a new hooks test | 1-0-1-2 = 4 | sonnet-medium |

Heavy tasks: 3, 4, 7, 8 (4 of 11), so the Execution line is `inline — claude --model sonnet --effort high`. Executor lane: `codex-gate usable=false reason=plugin-not-enabled lane=false`, so there are no Executor lines. Every task carries `**Items:** 3`. Branch: `feat/realtime-ui` from `master`. Copy the Global Constraints block from docs/superpowers/plans/2026-10-10-ghr-run-details-log-fixes.md (Go/npm check commands, timeouts, no `go test ./...`, commit format), minus its log-specific copy lines.

Order matters for compiling: Task 5 adds `Backend.Changes *changes.Hub` and `(b *Backend) Subscribe() (*changes.Sub, error)` (nil hub returns `changes.ErrNotReady`) before Task 6 adds `Subscribe` to `api.Backend`.

## Contracts settled

- `events.Ring.SetOnChange(func())`, `Ring.Last() int64`. `Add` reads the hook under `r.mu` and calls it after unlocking and logging.
- `history.Store.SetOnChange(func())`, `Store.Writes() int64`. `Append` and `Prune` become wrappers over `append`/`prune` returning `(wrote bool, err error)`; they bump `writes` and call the hook only when a line was written or the file renamed.
- `changes` package: `MaxSubscribers = 32`; `ErrNotReady`, `ErrFull`, `ErrClosed`; `Options{Tick (250ms), WakeWindow (50ms), Logf (log.Printf)}`; `New(Options) *Hub`; `Fingerprint(v any) (uint64, error)` (FNV-64a of JSON); `(h *Hub) SetSources(sources func() map[string]uint64, before func(topics []string))`. **Deviation from spec §2.1:** `BeforePublish` is passed to `SetSources`, not to `New`, because the hub is built in `Run` before `Backend` exists; record this in the plan's Assumptions. Also `Wake()`, `Subscribe() (*Sub, error)`, `Close()`; `Sub{C <-chan []string}` with `Close()`. There is no fake clock: tests use short Tick and WakeWindow values.
- Hub internals: one `mu`. `Subscribe` computes the baseline under `mu` when `h.stop == nil`, then `wg.Add(1); go watch(stop)`. `check` computes outside the lock, then under the lock returns early if its own `stop` is closed. It diffs only topics present in both maps, keeps missing topics' old fingerprints (logging once per topic until back), sorts, calls `before` when any of status/config/history/metrics changed, and sends to every subscriber. `send` merges by non-blocking receive, union, then send (it cannot block because the hub is the only sender and holds `mu`). `Sub.Close` closes the stop channel and nils `prev`/`missing` when the last subscriber leaves. `Close` sets `closed`, closes all channels and stop, then calls `wg.Wait()` outside the lock. `watch`: ticker, or wake followed by waiting out the rest of WakeWindow since the last check.
- `api`: `var heartbeat = 15 * time.Second`, `var changesWriteWindow = 30 * time.Second`. `streamChanges` sets the headers, writes `event: hello\ndata: {}\n\n`, then `event: change\ndata: {"topics":[...]}\n\n` per set and `: ping\n\n` per heartbeat. It ignores the `SetWriteDeadline` error, ends on a write or `Flush` error, and closes the subscription via `defer sub.Close()`. Refusal messages: "not ready", "too many live connections", "shutting down" (503 via `respond`). `fakeBackend` gains `hub *changes.Hub` and a mutex-guarded `subs []*changes.Sub`.
- `daemon`: `Backend.activityGen atomic.Int64`, `activityEntry.gen int64`. A cached entry is reused only if `e.gen == gen` (gen loaded after `e.mu.Lock()`). `(b *Backend) staleActivity([]string)` increments the generation. Free functions `tokenStatus(meta github.TokenMeta, st model.Status) model.TokenStatus` and `(b *Backend) storageFrom(st model.Status) model.Storage`. `setupFlags()` caches `WebSetupRequired`/`SetupPending` for 1 s in a mutex-guarded struct field. `fingerprints()` reads `M.Status()` and `GH.TokenMeta()` once (GH may be nil) and wraps each topic in a recover. Topic projections are as in spec §1. `fakeManager` gains `now`, `diskUsed`, `rate` and `statusCalls atomic.Int64` (Status is called concurrently in the activity tests).
- `run.go`: right after `ev := events.New()` it builds `hub := changes.New(changes.Options{})`, then calls `ev.SetOnChange(hub.Wake)`, `srv.RegisterOnShutdown(hub.Close)` and `webSrv.RegisterOnShutdown(hub.Close)`. `start(ctx, o, store, ev, hub, epoch, webCfg, webSetupRequired)` sets `hist.SetOnChange(hub.Wake)`, sets `Changes: hub` in the Backend literal, then calls `hub.SetSources(b.fingerprints, b.staleActivity)`. The configured shutdown branch calls `hub.Close()` right after `teardown()`, because Shutdown runs hooks in goroutines without waiting. During setup, `su.unconfigured()` already answers `/changes` with 503 (`setup.go:74-76`).
- `webui`: `var sessionRecheck = 5 * time.Second` and `var afterFunc = time.AfterFunc` (test seam). `requireSession` captures the session id, wraps the context with a cancel, and arms a self-re-arming timer guarded by a mutex and a `done` flag; it stops the timer on return.
- Web: `liveConfig = { enabled: true }` (the test setup sets it false); `SseParser` (LF/CRLF, comment calls `onAlive`, one leading space stripped, data lines joined by LF); `readChanges(h, signal): Promise<"ended" | "unauthorized">` cancels its reader when `signal` aborts; `runChanges(h, signal, wait = sleep)` with backoff from `RETRY_MIN` 1000 to `RETRY_MAX` 30000, reset only after a change or ping; `setLive`/`useLive` via `useSyncExternalStore`. `client.ts` exports `notifyUnauthorized()`. `use-change-stream.ts` holds `TOPIC_QUERIES`, `invalidateTopics`, `invalidateAll` and `useChangeStream`, whose handlers act only while their controller is the current one (an old loop's `onEnd` must not clear a newer connection's live flag). `hooks.ts` adds `POLL_SAFETY = 60_000` and `livePoll(live, interval)` for the eight hooks in spec §3.3. Tests read intervals from `query.observers[0].options.refetchInterval`.

## Test notes

- Daemon end-to-end (Task 7): use `testOptions(t, cfgYAML)` with `o.Reload = reload` (a `chan os.Signal`), an `http.Client` dialing the unix socket, a `GET http://ghr/changes` read with `bufio`, `reload <- os.Interrupt` (`Backend.Reload` adds events), then expect a `change` frame; after `cancel()`, `Run` returns within 10 s and the body ends.
- Unverified: jsdom's `TextEncoder`/`ReadableStream` feeding `TextDecoder` in Vitest (Task 9's tests verify it).
- Manual checks from spec §4 run at finishing on the owner's machine.
