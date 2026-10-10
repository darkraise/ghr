# Fable review: the stop starts the cleanup itself (register row 33)

**Date:** 2026-10-10
**Spec:** docs/superpowers/specs/2026-10-10-ghr-runner-stopping-state-design.md (the amendment for register item 33; reviewed as a design in chat, before it was written)
**Reviewer:** dr-superpowers:judge-fable
**Verdict:** approve with the listed changes. The owner approved every disposition below on 2026-10-10.

The review confirmed against the code that every writer of an instance's state holds `m.mu` and refuses a `cleaning` instance, that `beginFinish` cannot start two finishes at once, that the tick's steps tolerate an instance turning `cleaning` or disappearing under them, that `SD.Active` after a successful `SD.Stop` is a trustworthy exit answer, and that adoption cleans up a runner whose finish was refused at shutdown. It preferred this design to checking for the exit inside a tick.

| # | Severity | Finding | Disposition |
|---|---|---|---|
| 1 | major | `beginFinish` calls `wg.Add` after releasing `m.mu`, so a `Kill` that outlives the 5 s HTTP shutdown can race `Wait` | Accepted: §1.1, `closed` check and `wg.Add` inside the `m.mu` section |
| 2 | medium | `TestKillMarksTheRunnerStoppingWhileItsUnitStops` asserts the flag after the stop; the instance is `cleaning` by then | Accepted: §4 bullet reworded, test amended |
| 3 | medium | Tests that kill and never wait leak a finish that races the temporary directory's removal | Accepted: §4 |
| 4 | low | A `Kill` of a `cleaning` instance would retry a failed cleanup at once, past `retryFinish`, and could spend the ten-failure allowance in seconds | Accepted: §1.1, `Kill` never calls `beginFinish` for a `cleaning` instance |
| 5 | low | A `Kill` of a `cleaning` instance still adds "stopped by user" | Deferred, register row 34: existed before |
| 6 | low | When `SD.Stop` fails but the unit is gone, the runner reads as running until the next tick | Deferred, register row 35: existed before; how `systemctl stop` answers for an unloaded transient unit is not verified |
| 7 | low | `stopIdle` looks IDs from one snapshot up in a map built from another | Deferred, register row 36: the two cannot differ while instances are added only on the loop goroutine |
| 8 | low | Three comments describe the wake as what cleans a stopped runner up (`Manager.Kill`, `Backend.Wake`, `beginFinish`) | Accepted: corrected with the code |
| 9 | low | Register rows 25 and 33 need the outcome, and the 16.5 s poll period stays unaddressed | Accepted: row 33 carries the decision, row 37 the poll period |

Tests added at the review's request (§4): two concurrent kills start one finish; a unit systemd still reports active starts no finish; a kill racing `Close` and `Wait` under the race detector; a kill of a `cleaning` instance with a retry pending; a kill before any tick keeps the job's run number and workflow.

Not reviewed: the web UI, `internal/activity`, the change hub's fingerprints, `internal/api`, `cmd/ghr`, and real `systemctl` exit codes. The manual checks in §4 cover the first three end to end.
