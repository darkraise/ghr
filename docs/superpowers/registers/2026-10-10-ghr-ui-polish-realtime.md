# ghr UI polish and realtime updates

**Source:** owner request in chat, 2026-10-10
**Covers:** docs/superpowers/specs/2026-10-10-ghr-realtime-ui-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | Do not allow to collapse sidebar | commit a697071 | Rail has no collapse toggle; collapsed-rail styles and markup removed | done | toggle hidden by CSS; kit has no prop |
| 2 | Increase font size of entire system by one more level | commit a657e48 | Theme font-size axis is large; no new overlap or truncation versus medium | done | rail widened to 18rem; chart SVG text held at 0.75rem |
| 3 | Realtime update for UI | docs/superpowers/plans/2026-10-10-ghr-realtime-ui.md | Daemon pushes changes; UI refreshes without waiting for poll timers | verify | Owner runs spec section 4 manual checks: runner stop shows in under 1 s, no 1 s polling when idle, logout ends the stream within 5 s, six tabs open |
| 4 | Realtime: end-to-end test of /api/changes through the web listener under a short WriteTimeout | - | - | open | final review Minor 3; a middleware hiding SetWriteDeadline would end streams at 60 s unnoticed |
| 5 | Realtime: README note on the stream behind a proxy (X-Accel-Buffering, 15 s ping) | - | - | open | final review Minor 8 |
| 6 | Realtime: unexplained 'Errors 1 error' in one full web run after Task 9 | - | - | open | not reproduced in 15 full and 40 isolated runs; save the output if it recurs, vitest fails CI on unhandled errors |
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
