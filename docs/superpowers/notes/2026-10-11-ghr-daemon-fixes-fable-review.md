# Fable review: daemon fixes

**Date:** 2026-10-11
**Spec:** docs/superpowers/specs/2026-10-11-ghr-daemon-fixes-design.md (reviewed as first written, before any plan)
**Reviewer:** dr-superpowers:judge-fable
**Verdict:** approve with the listed changes. Every finding was confirmed against the code before the spec was amended. The owner ruled on findings 2, 4 and 6 on 2026-10-11; the other dispositions were proposed to the owner the same day and await the review of the amended spec.

| # | Severity | Finding | Disposition |
|---|---|---|---|
| 1 | major | §3 "treat a blocked repository exactly as a paused one" contradicts the manager: a paused repository is not polled and `forgetPaused` wipes its error, backoff and queue every tick; `sched` reads `Paused` from the config it is handed | Accepted: §3 rewritten with a separate blocked state, a derived config copy for planning, the idle stop and the update gate, and a message kept apart from `repoErr` |
| 2 | major | §3 start-up hole: "never read counts as not public" lets a public repository spawn after a restart when its first read fails; the tick order was not stated | Accepted, owner's ruling: an unread repository gets no new runners; the read sits in `gatherDemand`, before planning |
| 3 | major | §3 names no interface change: `runner.GitHub` has no `GetRepo`, nor does the package's fake | Accepted: §3 |
| 4 | major | §7.3 `"max": null` cannot be told from an absent key with `*int` | Accepted, owner's ruling: a three-case value type, and the repository page sends `null` for an emptied box in this spec |
| 5 | medium | §2.1 the refusal rules have no order, and paused repositories are skipped, so a token rotated under "Pause all" is checked for no permission | Accepted: fixed order (401, rate limit, all failed, save with warnings); paused repositories are checked |
| 6 | medium | §7.4 a 409 from `/setup/finish` breaks installs without `setup.sh`, which never have the markers | Accepted, owner's ruling: `configure` creates `setup-pending`; a finish without it answers 204 and does nothing |
| 7 | medium | §4.1 classifying post steps by name misfiles a regular step a person named `Post …`; `Initialize containers` and `Stop containers` print no header | Accepted: post steps are found by position; both container steps are excluded |
| 8 | medium | §4.1 "opened" against "current" is undefined once the time rule moves the pointer | Accepted: one pointer; today's "current step already has lines" test is kept |
| 9 | medium | §7.2 GitHub's `full_name` may name another owner after a transfer | Accepted: the add is refused |
| 10 | medium | §5.4 gating `RunnerSteps` first turns an unknown runner, a runner without a job and a cached answer into errors while degraded | Accepted: the gate sits just before the GitHub call |
| 11 | low | §6 a file holding only an oversized line is never rewritten (`all == nil` return); 0600 "at the next rewrite" becomes rare once a no-op prune writes nothing | Accepted: §6.2 |
| 12 | low | §6.3 dropping a future open hour is already today's behaviour | Accepted: reworded |
| 13 | low | §5.2 an undecodable workflow file is fetched on every page load | Accepted: remembered as bad |
| 14 | low | §4.2 the raw log route and the web fixtures were not covered | Accepted: §4.2 |
| 15 | low | §3 visibility state outlives a removed repository | Accepted: dropped each tick |
| 16 | low | §2.2 a caller that joins a fetch after the generation changed also gets the old answer; a failed read is kept 10 minutes only by the annotations route | Accepted: stated |
| 17 | low | §1.2 the `lru` test also reads the unexported `size` | Accepted: the test moves into the new package |
| 18 | low | §2.3 callers of `Store.Reload` not named | Accepted: named |
| 19 | low | §2.1 the test fake returns a plain error, so a 401 test would pass through the wrong rule; `CheckTokenOwner` needs wiring | Accepted: §2.1 |
| 20 | low | §1.2 the page cap reaches `ListUserRepos` and the tick's `ListRuns` | Accepted: stated, error and never a partial list |
| 21 | low | §7.1 `config.example.yaml` and the Activity lane padding are named by the row, not by the spec | Accepted: §7.1 |
| 22 | nit | §1.3 where the one-slot channel is created | Accepted: in `New` |
| 23 | nit | §4.1 `Complete job` prints lines before `Cleaning up orphan processes` | Accepted as a known limit |
| 24 | nit | §4.1 whether steps after a cancelled step are `skipped` is unverified | Accepted: added to §8 |
| 25 | nit | §9 tests missing for the 429 path, `warm: null`, sorted PATCH order, the skip log, the name exclusions and remove-and-re-add; `runDetailPoll` needs the attempt | Accepted: §9 and §5.1 |
| 26 | nit | Row 31 is only partly answered | Accepted: the row map says so; the row's acceptance text will too |

The review found sound, against the code: `record` as the single place the token verdict is set (§1.1); the lock structure of `mutate` (§1.3); the completeness of the cache list in §2.2 and its lock order; that an unchanged token generation makes `dropLabelChecks` drop only removed repositories, and that `ClearDegraded` leaves a rate-limit suspension alone (§2.3); the daemon side of §5.1; the three graph cases of §5.3; the read order in §5.4; and that clamping in `Parse` while refusing in `Validate` leaves no config that loads but cannot be saved (§7.1).

Not reviewed: `internal/runner/lifecycle.go` beyond `startFinish`, `Pending`, `finalizePending` and `stopIdle`; `adopt.go`; `update.go` outside the gate; the web test suites; the live checks recorded in §8; `internal/api` server tests; the change hub.
