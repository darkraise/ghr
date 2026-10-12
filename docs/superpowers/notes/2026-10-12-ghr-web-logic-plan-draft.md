# Web-logic plan draft

**Date:** 2026-10-12
**Spec:** docs/superpowers/specs/2026-10-12-ghr-web-logic-fixes-design.md (approved by the owner on 2026-10-12)
**Register:** docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md (rows 33, 75 to 109, 225, 279 to 284; sub-project 4 of 7)
**Target plan:** docs/superpowers/plans/2026-10-12-ghr-web-logic-fixes.md (written and assembled on 2026-10-12)

**Status:** the plan is assembled (84 tasks) and its first review round is answered in part; a second session on 2026-10-12 ended on its budget. "What is left now" below is the work list. The sections after it record how the plan was drafted; their "What is left" list is done except its item 8.

## What is left now

State on 2026-10-12: the plan is at `docs/superpowers/plans/2026-10-12-ghr-web-logic-fixes.md`, linted (0 errors) and committed without a Plan review line. The register rows are planned, rows 285 to 308 are added, and the spec is amended. The reference branch `plan/web-logic-order` (worktree `.claude/worktrees/order-web-logic`, HEAD `6f7f105`) holds the 83 task commits in plan order; `plan/web-logic-order-r1` is its state before the review fixes. On `plan/web-logic-order`: `npm test` 97 files and 1384 tests, `npm run test:tz` 5 tests, typecheck, lint and build clean; the Go checks (22 packages with `-race`) were run before the review fixes, which touch no Go.

Review round 1 ran on the Codex seat (`codex gpt-6-astra / xhigh`): executability 17, coherence 17, coverage 18, assumptions 18, one Critical and nine Important findings, in `.superpowers/sdd/2026-10-12-ghr-web-logic-fixes/plan-review-round-1.json`. `plan-round-1.md` beside it is the plan as reviewed.

Fixed, each carried out on `plan/web-logic-order` and written into its fragment:

- Critical, Task 21: the settings form counts an unsaved change by the stored values (`changedKeys(draft, loaded)` and `settle(d, sent)` without `equal`), as spec section 2.3 says. Two tests from 2026-10-06 that pinned the opposite are rewritten in the task: `120s` over `2m0s`, and a space typed after a value, now count as one unsaved change.
- Task 28: a tag that is added clears the refusal line; one test added.
- Task 39: the stored match is reset when the deferred search text changes; one guard test added.
- Task 76: the test also asserts that the chart is drawn.
- Header: the one-test-file rule now reads "only the test files its commands name"; the Contracts line on `@/query` excepts test files.

Still to do, in order:

1. **Add a last task, a browser check gated on the owner's yes** (finding: unverified assumptions have no verifying task). Write it as Task 3 of `chain-7.md` with a `(7, 3)` entry in `scores.txt`: deploy to the runner LXC only with a yes, then the checks of spec section 8 with expected results, and "pending" in the report without a yes. The memory notes on the LXC say how to deploy and drive the browser. Point the `unverified` bullets of `header-assumptions.md` at it.
2. **Record the four declined findings** in `header-assumptions.md`, one line each, and in the register:
   - Task 14, a second navigation blocked while a dismissed save still runs: beyond spec section 2.2; it is register row 286.
   - Tasks 18 and 19, incomplete numbers such as `1e` that a number input reports as empty: on `master` already, the spec keeps the meaning of an emptied box; add it to row 289's note.
   - Task 25, an edit made while the wizard's "Save your changes?" save runs is lost on leaving: `saveAndNext` on `master` does the same; add a new row.
   - Task 39, the urgent render still regroups the open steps: the line re-render and the matching are deferred and the parse is cached; it is row 303.
3. **Tell the owner about the Task 21 reading**: add it to row 308 (its note) and to the "Readings" bullet of `header-assumptions.md`, and add spec section 2.3 to the amendments if its wording should say "counts as an unsaved change" outright.
4. **Reassemble and lint**: `python3 .superpowers/plan-draft/web-logic/assemble.py docs/superpowers/plans/2026-10-12-ghr-web-logic-fixes.md 1,2,3,5,4,6,7`, then `plan-lint`. The block checker `ordercheck2.py` was in the session scratchpad and is gone; `git diff plan/web-logic-order-r1 plan/web-logic-order` shows the review fixes.
5. **Review round 2** (cap 2): `review-route PLAN --plan-round 2` names `dr-superpowers:judge-opus`; pass it `diff -u <workspace>/plan-round-1.md PLAN` as the delta and the round 1 JSON as the prior findings. Then the `**Plan review:**` line, and commit.
6. **Clean up**: remove the six drafter worktrees and branches and the `plan/web-logic-order-r1` branch; keep `plan/web-logic-merge` and `plan/web-logic-order`, which the plan's Assumptions cite.

The plan is drafted the way the three earlier plans were: each task is carried out red-then-green in a worktree first, and its text is written from what was done. Six drafters did this, one per group of files. Their task texts are the fragments below. What is left is to check the task order, write the last two tasks, score, assemble, lint and review the plan.

## Fragments and worktrees

Fragments are in `.superpowers/plan-draft/web-logic/` in the primary checkout (untracked): `BRIEF.md` (the rules every drafter followed), `header-top.md`, `header-mid.md`, `assemble.py`, and one `chain-N.md` per drafter. Each fragment numbers its tasks from 1 and holds its tasks, then Contracts, Assumptions and Notes for the assembler with the drafter's proposed scores and adjacent findings. Worktrees are under `.claude/worktrees/`.

| Chain | Tasks | Rows | Worktree branch |
|---|---|---|---|
| 1 suite and housekeeping | 8 | 109, 225, 100, 99, 101, 108 | `worktree-agent-af9859d24181ef893` |
| 2 session and routing | 9 | 75, 106, 88, 89, 98, 79, 83, 281, 108 | `worktree-agent-a05b0a27e1fd9b4f8` |
| 3 forms and drafts | 17 | 76, 279, 80, 97, 82, 103, 107, 101, 108 | `worktree-agent-ae8b61b4f91d03de2` |
| 4 API client and live data | 11 | 81, 102, 85, 84, 33, 86, 87, 93, 106, 108 | `worktree-agent-a732011984c5c5c14` |
| 5 run page and job log | 13 | 77, 280, 104, 91, 92, 108 | `worktree-agent-a470b640ac0f8e638` |
| 6 results, links, History, token, Actions | 24 | 78, 105, 90, 84, 96, 95, 282, 106, 283, 284, 101 | `worktree-agent-a3f0128278bda1baa` |

Each branch sits on local `master` at `d95cb45` with one commit per task, in task order. Each drafter reported its own whole web suite, `tsc --noEmit` and `eslint src --max-warnings 0` green, except chain 4, whose last task (one shared clock) makes one test in `web/src/pages/run.test.tsx` fail; chain 5's Task 2 corrects that test.

**Plan order of the chains: 1, 2, 3, 5, 4, 6, then chain 7.** Chain 5 goes before chain 4 because of that test.

**Merge check.** The branch `plan/web-logic-merge` (worktree `.claude/worktrees/merge-web-logic`, HEAD `81c1da8`) holds all six branches merged onto `master`. One conflict, resolved by taking both sides: the import block at the top of `web/src/api/hooks-pages.test.tsx` (chains 4 and 5). On that branch, on 2026-10-12: `npx vitest run` gave 97 files and 1382 tests passed; `npm run test:tz` 2 files and 5 tests; `tsc --noEmit` and `eslint src --max-warnings 0` exit 0; `go test -count=1 ./internal/model/ ./internal/history/ ./internal/activity/` ok. `go test -race ./...` and `npm run build` were not run. The branch is a reference, not the plan's execution.

## What is left

1. **Check the quoted blocks in plan order.** Each drafter checked its "replace these lines" blocks against its own chain only. Cherry-pick the task commits onto a fresh branch from `master` in plan order and, before each one, check that the task's quoted blocks are in the files as they then stand. Known mismatch: the import block of `web/src/api/hooks-pages.test.tsx` quoted by chain 4's Task 10 (and Task 5, if it quotes it), which chain 5's Task 8 changes first. Files edited by more than one chain: `web/src/api/hooks.ts` (4, 5), `web/src/api/hooks-pages.test.tsx` (4, 5), `web/src/lib/history.ts` and its test (1, 6), `web/src/components/runner-panel.tsx` and its test (4, 6), `web/src/lib/format.test.ts` (4, 6), `web/src/components/shell.tsx` (2 only, but see chain 2's note on `count()`), `web/src/test/render.tsx` (3 adds `renderAppInBrowser`). Correct the fragment text where a block no longer matches.
2. **Write chain 7** (`chain-7.md`), two tasks:
   - Per-test timeouts below the new default. Nineteen places in eleven test files pass `{ timeout: 10_000 }`, which overrides the 15 seconds of chain 1's Task 1 and gives those tests less time than every other test (`rg -n 'timeout: ?10_000' web/src --glob '*.test.*'`; one of them, `web/src/pages/setup.test.tsx:48`, is a `findByRole` wait, not a test timeout, and stays). Remove the overrides, and correct the comment at `web/src/test/setup.ts:32-33` that names "Vitest's 5-second test timeout". This is an addition to spec section 5.1; amend the spec.
   - Whole-tree verification, changing nothing: `npm test`, `npm run test:tz`, `npm run typecheck`, `npm run lint`, `npm run build`, then `gofmt -l`, `go vet ./...` and `go test -race -count=1 -timeout 480s ./...`.
3. **Scores.** Write `scores.txt` as a Python dict `{(chain, local task): (files, spec, coupling, risk)}` from the tables in each fragment's Notes, checking each against `reference/ladder.md`. Proposed heavy tasks so far (total 5): chain 2 Task 1, chain 4 Task 5, chain 6 Task 5; chain 3's scores were not read.
4. **Header.** Write `header-assumptions.md` (the base commit, the merge check above, the stale-base note below, Claude-only). `header-mid.md` already carries the Global Constraints; chain 2's tasks each end with a "Lint and type-check" step, which the constraints also cover.
5. **Assemble:** `python3 .superpowers/plan-draft/web-logic/assemble.py docs/superpowers/plans/2026-10-12-ghr-web-logic-fixes.md 1,2,3,5,4,6,7`. The script renumbers tasks and every "Task N" reference inside a fragment; check the result for references it could not know (another plan's task numbers, cross-chain references).
6. **Register.** Set rows 33, 75 to 93, 95 to 109, 225 and 279 to 284 to `planned` with the plan's path and an acceptance each; rows 84, 101, 106 and 108 are discharged by several tasks. Add the drafters' adjacent findings as new rows (each fragment's Notes lists them; several belong to sub-project 5 or 6).
7. **Spec amendments** from the drafting, each recorded in a fragment's Notes: containers on the runner panel also keep their data (3.3); the focus and reconnect refetch is turned off in the query client's defaults, not on the hooks (3.4); the stream watchdog also covers the wait for the response's headers (3.2); the request timeout is a plain `Error` reading `the daemon did not answer` (3.2); `JobLogs` and `Line` are memoised, not `LogViewer`, and the intrinsic size is `auto 1.25rem` (4.1); row 96 is fixed in the page and keeps rows only when the chart's start slides (3.6); pruning a draft pauses while a save is in flight (2.3); the wizard's reload warning uses `UnsavedGuard` unchanged (2.5); the per-test timeouts (5.1).
8. **Lint, self-review, plan review, record, commit**, as dr-superpowers:writing-plans says. Then remove the six drafter worktrees and branches and keep `plan/web-logic-merge`.

## Rulings made while drafting

- The external executor question was asked on 2026-10-12 and not answered within 15 minutes: the plan is Claude-only (no `External executors` line), although the Codex gate printed `usable=true lane=true`.
- The plan is sequential (the spec's execution strategy).

## Open with the owner

None blocks the plan; each drafter chose the reading named here.

- Row 96: the History rows stay only when the chart's start slides to its next bucket. After a click on 24h, 7d or 30d the old rows are hidden while loading, as today. Keeping them across a window change too is a one-line change (chain 6, Task 16).
- Row 76: `1.9` in Max reads `Max must be >= 0`, the settings form's shared rule for anything that is not a whole number (chain 3, Task 1).
- Row 82: the wizard's guard also stops the browser's Back button out of the wizard, with a dialog worded "on the Settings page"; the retry button reads "Try again", where the spec says "Retry" (chain 3, Tasks 8 and 10).
- Row 107: the toast after Cancel removal still reads "Resumed <name>" (chain 3).
- Row 89: when `/auth/state` cannot be read after a `401`, the Account form shows that request's own error (chain 2, Task 4).
- Row 83 and 281 wording: `Log out and discard 1 unsaved change?`, the button `Log out`, the toast `Log out failed: <error>` (chain 2).
- Spec section 4.2: the three new results fall into the charts' "cancelled" count, as skipped does today; History's Cancelled filter matches `cancelled` exactly, so chart and filter can disagree (chain 6's Notes).

## Incident

The Agent tool's worktrees were created at `f5cd90d`, the last merged pull request, 128 commits behind local `master`. Every drafter rebased or fast-forwarded onto `d95cb45` and re-checked its quoted blocks there. An isolated agent also cannot write into the primary checkout, so each fragment was written inside its worktree and copied out.
