# ghr UI polish and realtime updates

**Source:** owner request in chat, 2026-10-10
**Covers:** docs/superpowers/specs/2026-10-10-ghr-realtime-ui-design.md, docs/superpowers/specs/2026-10-10-ghr-runner-stopping-state-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | Do not allow to collapse sidebar | commit a697071 | Rail has no collapse toggle; collapsed-rail styles and markup removed | done | toggle hidden by CSS; kit has no prop |
| 2 | Increase font size of entire system by one more level | commit a657e48 | Theme font-size axis is large; no new overlap or truncation versus medium | done | rail widened to 18rem; chart SVG text held at 0.75rem |
| 3 | Realtime update for UI | docs/superpowers/plans/2026-10-10-ghr-realtime-ui.md | Daemon pushes changes; UI refreshes without waiting for poll timers | verify | Checked 2026-10-10 on the LXC with build 7714ed3. Pass: pause and resume show in 240 ms; idle makes 6 API requests in 60 s with one changes request; logout ends the stream in 3.5 s; six tabs with five marked hidden load pages. Fail: a runner stop took 16 s to show, see row 25 |
| 4 | Realtime: end-to-end test of /api/changes through the web listener under a short WriteTimeout | commit 735f180 | A daemon test holds /api/changes open through the web listener for three shortened WriteTimeouts and still receives a change frame | done | final review Minor 3; fails when SetWriteDeadline is removed from streamChanges (checked once, not committed) |
| 5 | Realtime: README note on the stream behind a proxy (X-Accel-Buffering, 15 s ping) | commit 78fb4af | README Web UI section says what a proxy must do for /api/changes and that the UI falls back to polling | done | final review Minor 8; nginx behaviour taken from its documented defaults, not tested behind a proxy |
| 6 | Realtime: unexplained 'Errors 1 error' in one full web run after Task 9 | - | - | deferred | not reproduced in 56 full runs (15 at the time, 41 on 2026-10-10) and 40 isolated runs; cause unknown; save the output if it recurs, vitest fails CI on unhandled errors |
| 7 | Realtime: log a topic missing from the hub baseline once (spec section 1) | - | - | deferred | Task 3 ruling parked it: logging needs the topic list, which the spec leaves to the daemon |
| 8 | Realtime: plan Assumptions line 'Spec gap, section 1' is stale after d243c17 | - | - | deferred | plan text changes only via plan-amend; the ledger ruling recorded the supersession |
| 9 | Realtime: storage topic can publish up to 4x per second while an install reports progress | - | - | deferred | final review Minor 4; matches spec fingerprint of the full snapshot |
| 10 | Realtime: hooks-live.test.tsx reads TanStack observer internals | - | - | deferred | final review Minor 5; plan pins the library version |
| 11 | Realtime: doc comments on changes.Options, Hub, New and the three errors | - | - | deferred | Task 3 minor and final review Minor 7; comment policy is none by default |
| 12 | Realtime: changes.Subscribe calls sources() under the hub lock | - | - | deferred | Task 3 minor; contract-mandated baseline, keep fingerprints cheap |
| 13 | Realtime: a Wake sent while no watcher runs costs the next watcher one extra check | - | - | deferred | Task 3 minor |
| 14 | Realtime: hub tests TestSubscribeRefusals and TestCloseDuringPublishAndWakeDuringClose lack t.Cleanup(h.Close) | - | - | deferred | Task 3 minor; leaks only when a test fails |
| 15 | Realtime: Task 4 report quoted no gofmt or go vet output | - | - | deferred | Task 4 minor; evidence nit only |
| 16 | Realtime: hist.SetOnChange(hub.Wake) in run.go has no test | - | - | deferred | Task 7 minor; a lost wake delays history by at most 250 ms |
| 17 | Realtime: the two RegisterOnShutdown hooks are not tested individually | - | - | deferred | Task 7 minor; each covers the other |
| 18 | Realtime: the explicit hub.Close after teardown is not tested | - | - | deferred | Task 7 minor; documented by its comment |
| 19 | Realtime: run_test.go reader goroutine can block on a full frames channel after a failure | - | - | deferred | Task 7 minor |
| 20 | Realtime: handler_test.go request goroutine leaks if the session test fails early | - | - | deferred | Task 8 minor |
| 21 | Realtime: handler_test.go timer-capture hook appends without a lock | - | - | deferred | Task 8 minor; fragile only if sessionRecheck is shortened there |
| 22 | Realtime: password change and reset session ends are not tested for the stream | - | - | deferred | Task 8 minor; they end sessions through the same Valid check as logout |
| 23 | Realtime: history.Store.Append double-closes its file (defer plus explicit Close) | - | - | deferred | Task 2 discovered; pre-existing and harmless |
| 24 | Realtime: an idle daemon refetches /api/status about once a second in bursts | commit 721df16, spec 7714ed3 | A token check that moves only CheckedAt publishes no topic; an idle Dashboard makes no status refetch bursts | done | found by the row 3 checks; rechecked on the LXC with build 7714ed3: 1 change notice and 6 API requests in 60 s idle; the token's check time now refreshes on the 60 s safety poll |
| 25 | Realtime: a stopped runner stays running in the UI until the manager's next tick | docs/superpowers/specs/2026-10-10-ghr-runner-stopping-state-design.md | A runner stopped with ghr kill or the Stop button reads stopping in the UI in under a second and is cleaned up as soon as its unit is down | doing | found by the row 3 checks; busy runner b6f7e2 took 16.2 s from ghr kill to the UI: 8.4 s for systemd to stop the unit, then 7.8 s until the next tick; Manager.Kill leaves the exit to the next tick, KillRunner does not wake the loop, and there is no stopping state; the stream itself delivered the job result 63 ms after the daemon logged it |
| 26 | Realtime: the gauges fingerprint omits Status.RateLimit, which spec section 1 lists | - | - | deferred | noticed while fixing row 24; the status topic zeroes it too, so a changed limit alone publishes nothing; the limit rarely changes |
