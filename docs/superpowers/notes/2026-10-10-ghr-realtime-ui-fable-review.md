# Fable review: realtime UI spec

**Date:** 2026-10-10
**Reviewed:** docs/superpowers/specs/2026-10-10-ghr-realtime-ui-design.md as committed in 105faa2
**Verdict:** ready after the listed fixes. All 19 findings were applied to the spec the same day.

Claims the reviewer verified as correct: the web listener's 60 s `WriteTimeout` and no timeouts on the socket server; per-write deadline extension through `http.ResponseController` is needed because Go sets the deadline once per request; the 30 s `ReadTimeout` does not cancel a bodyless GET's context; nothing wraps the `ResponseWriter` between `webui.Handler` and the API mux; every storage and prune busy signal is inside the `Storage()` or `Status()` JSON; React Query v5 `invalidateQueries` skips disabled queries but ignores tab focus.

| # | Severity | Section | Finding | Resolution in the spec |
|---|---|---|---|---|
| 1 | major | §1 | `status` and `token` fingerprints change on almost every tick: `RateRemaining` (every GitHub response) and `DiskUsedBytes` are in them, so pushes degrade to ~10 s polling of the heaviest endpoints | Stable projections; new `rate` topic for the volatile fields that invalidates only status and token and skips `BeforePublish` |
| 2 | major | §3 | Hidden tabs: invalidation fetches regardless of focus, and six tabs on HTTP/1.1 use all six connections per host | Stream closes while the tab is hidden; `useLive` false while hidden |
| 3 | major | §2.1 | A baseline taken after `Subscribe` returns can swallow a change until the 60 s poll | `Subscribe` takes the baseline synchronously; a stopped watcher discards fingerprints |
| 4 | major | §2.3 | Setting `Ring.OnChange` in `start()` races with handlers already calling `ev.Add`; `Backend` is built in `start()`, not `Run` | Hub built in `Run` before `serve()`; sources attached later through `SetSources`; `OnChange` set before concurrency |
| 5 | major | §2.1 | Channel close versus publish unspecified: send-on-closed panic; a locking `Wake` can block `Ring.Add` callers | One hub mutex serialises publish and close; the hub is the only sender; `Wake` is a lock-free non-blocking send; `Subscribe` after `Close` errors |
| 6 | minor | §2.1 | Marking activity entries stale needs each entry's lock and races with an in-flight build | Generation counter on the activity cache |
| 7 | minor | §3.2 | `config` changes (`history_retention`) affect activity but do not invalidate it | `config` joins `BeforePublish` and invalidates activity |
| 8 | minor | §3.2 | The first `hello` double-fetches every query | Invalidate on `hello` only after a previous connection |
| 9 | minor | §2.2 | `hello` epoch unused; "handled by Shell" is partial | Epoch dropped from `hello` |
| 10 | minor | §3.1 | Abort through `signal` reads as a reconnect; parser details missing | `AbortError` ends without reconnecting; decoder and `data:` rules stated |
| 11 | minor | §3.1 | Backoff reset on every `hello` allows a 1 s reconnect loop | Reset only after the first `change` or `ping` |
| 12 | minor | §2.2 | `SetWriteDeadline` errors must be ignored; recorder tests cannot cover disconnects | Stated; tests use `httptest.NewServer` |
| 13 | minor | §2.4 | Session re-check shape and test seam | `time.AfterFunc` chain, cookie captured at entry, interval a package variable |
| 14 | minor | §1 | `Writes()` would bump on no-op appends and prunes | Bump only when a line is written or the file replaced |
| 15 | minor | §1 | Status sources cost at 250 ms (password file read, setup marker stat, `M.Status()` three times) | One `M.Status()` per check; setup flags cached for 1 s |
| 16 | minor | §2.1 | Event bursts cause a check per `Wake` | Wakes coalesced to one check per 50 ms |
| 17 | nit | §3.2 | `events` invalidation key | Prefix `["events"]` |
| 18 | nit | §2.3 | `Hub.Close` should join the watcher | Stated |
| 19 | nit | §4 | Missing tests for findings 1, 2, 3, 5, 6, 10, 12 | Added |

Security: nothing beyond the above. The 503 cap is reached only after `requireSession`; the stream carries topic names only; `X-Accel-Buffering: no` and the 15 s ping cover a buffering proxy.
