# Plan draft: run details

**Spec:** docs/superpowers/specs/2026-10-09-ghr-run-details-design.md (revised for the Fable review, commit 1abe44c)
**Status:** research done, task breakdown drafted; the plan file `docs/superpowers/plans/2026-10-09-ghr-run-details.md` is not written yet. Write it with dr-superpowers:writing-plans from this draft, with complete code in every step (the previous plan, `docs/superpowers/plans/2026-10-09-ghr-actions-view.md`, is the format model: same Global Constraints block, Step 0 environment check, `timeout 300` on every test command).

## Task breakdown (Rule S: files + spec + coupling <= 3 each)

| # | Task | Files | Score f-s-c-r = total |
|---|---|---|---|
| 1 | GitHub run reads | `internal/github/client.go` (Run fields, `ErrGone` after `ErrServer`, classify 410), new `runs.go` (PullRequest, Artifact, GetRun, GetRunAttempt, ListAttemptJobs, GetJob, ListArtifacts, GetContent), `runs_test.go` | 1-0-2-1 = 4 |
| 2 | GitHub redirected downloads | `client.go` (`Blob *http.Client` set in `New`; extract the rate-limit suspension in `do` into a helper), new `download.go` (`request`, `redirectURL`, `JobLog`, `ArtifactURL`, `ErrTooLarge`), `download_test.go` | 1-0-2-3 = 6 |
| 3 | Model types and job log parsing | `internal/model/model.go`, new `internal/joblog/joblog.go` (`Parse(raw, []model.Step) []model.LogStep`, `Annotations(jobID, name, steps) ([]model.Annotation, omitted int)`), test | 1-0-2-1 = 4 |
| 4 | Workflow graph | new `internal/wfgraph/wfgraph.go` (`Parse` via yaml.Node to keep order, resolve aliases; `Build(w, []APIJob) ([]Node, error)`, `ErrMismatch`, `ErrInvalid`), test | 1-0-2-0 = 3 |
| 5 | Daemon GitHub interface and fake | `internal/daemon/backend.go` (GitHub interface gains the Task 1-2 methods), `backend_test.go` (embed `fakeRuns` in `fakeGH`, one line), new `runs_fake_test.go` | 1-0-2-1 = 4 |
| 6 | Workflow file cache and graph note | new `internal/daemon/rungraph.go` (`runGraph(ctx, repo, sha, path, jobs) (*model.RunGraph, note string, err)`), test | 1-0-2-2 = 5 |
| 7 | Run details and cache | new `rundetail.go` (`coveredName`, `detailErr`, `ghrJobs`, `runJob`, `modelSteps`, `RunDetail`), test | 1-0-2-3 = 6 |
| 8 | Job logs | new `joblogs.go` (`logCall` shared per job, process-wide semaphore 4, parsed-log cache by bytes (var so tests shrink it), annotation cache 500, failure outcomes 10 min; `JobLog`, `RawJobLog`), test | 1-0-2-3 = 6 |
| 9 | Annotations | new `annotations.go` (`RunAnnotations`; `annotationsWait` var 20 s), test | 1-0-2-3 = 6 |
| 10 | Artifact download | new `artifacts.go` (`ArtifactDownload`, host allowlist), test | 1-0-2-3 = 6 |
| 11 | API routes | `internal/api/server.go` (5 routes, `positive`, `attemptParam`, `FromGitHub` maps `ErrGone` to 410), `api_test.go` (fakeBackend) | 1-0-2-1 = 4 |
| 12 | Fixtures | `web/fixtures_test.go` (+ generated `run-detail`, `run-annotations`, `job-log` JSON) | 1-0-2-0 = 3 |
| 13 | TS types and test fixtures | `web/src/api/types.ts`, `types.test.ts`, `src/test/fixtures.ts` | 1-0-2-0 = 3 |
| 14 | API client and hooks | `client.ts`, `hooks.ts`, `client-pages.test.ts` | 1-0-2-1 = 4 |
| 15 | Run helpers and status icon | new `src/lib/run-details.ts` (+test), new `src/components/run-status-icon.tsx` | 1-0-2-0 = 3 |
| 16 | Run frame | new `src/components/run-frame.tsx` (+test) | 1-0-2-0 = 3 |
| 17 | Run and job routes | `src/router.tsx`, new `src/pages/run.tsx` (exports RunPage and JobPage), `run.test.tsx` | 1-0-2-1 = 4 |
| 18 | Actions page links to the run page | `pages/actions.tsx` (use RunStatusIcon, drop now-unused `isActive`/`isQueued` imports), `actions.test.tsx` | 1-0-1-1 = 3 |
| 19 | Artifacts section | new `components/run-artifacts.tsx` (+test), mount in `pages/run.tsx` | 1-0-2-0 = 3 |
| 20 | Annotations section | new `components/run-annotations.tsx` (+test), mount | 1-0-2-0 = 3 |
| 21 | Job graph | new `components/run-graph.tsx` (+test), mount | 1-0-2-0 = 3 |
| 22 | Jobs timeline | new `components/run-timeline.tsx` (+test), mount | 1-0-2-0 = 3 |
| 23 | ANSI tokenizer | new `src/lib/ansi.tsx` (+test) | 1-0-2-0 = 3 |
| 24 | Log view model | new `src/lib/log-view.ts` (+test): groups, search matches, 5,000-line window | 1-0-2-0 = 3 |
| 25 | Log viewer | new `components/log-viewer.tsx` (+test) | 1-0-2-0 = 3 |
| 26 | Job page logs | `pages/run.tsx` (JobPage mounts viewer, Raw log Blob save, step/line params), `run.test.tsx` | 1-0-2-1 = 4 |
| 27 | Accessibility and README | `src/a11y.test.tsx` (axe on both pages), `README.md` (Contents: read optional) | 1-0-1-0 = 2 |

Heavy: 2, 6, 7, 8, 9, 10 (6 of 27) → `**Execution:** inline — claude --model sonnet --effort high`.

## Rulings to record in the plan's Assumptions

- Adding methods to an interface whose implementers the same or an earlier task supplies is scored coupling 2, not 3 (Tasks 5, 11).
- `PageHeader` takes a string `title` and `description` (`web/node_modules/darkraise-ui/dist/layout/types.d.ts:56-62`), so the status icon cannot sit before the title: it shows in the rail and the Summary's Status fact, and the description is plain text. Amends spec §1.2.
- The frame and section components take link render functions (`JobLinkFn = (jobId, children, {step, line, className, label, current}) => ReactNode`, plus `summaryLink`), and `pages/run.tsx` supplies typed TanStack `Link`s; components test without a router. Both routes are registered in Task 17, because a typed `Link` needs its target route registered.
- TanStack `Link` sets `aria-current="page"` and `data-status="active"` itself when active, unless `activeProps` is given (`web/node_modules/@tanstack/react-router/dist/esm/link.js:219-240`). Use `activeOptions={{ includeSearch: false }}` for rail links.
- The kit sidebar matches nested paths (`currentPath.startsWith(href + "/")`, `darkraise-ui/dist/chunk-4S4GXBTH.js:385`), so the Actions nav item stays active; no change needed (review finding 28).
- The blob download has no idle deadline; the caller's detached context bounds it (60 s for parsed logs, 120 s for the raw log). Amends spec §2.3.
- Workflow file cache needs no shared fetch: the run details fetch that calls it is already shared per key.
- "Latest" and its number share a run-detail entry by storing the result under both keys when the shown attempt is the latest.
- 404 handling in the browser: an uncovered repository is detected from `/api/config` names before any call; any 404 from the run API then means "This run no longer exists on GitHub."
- Go must emit non-nil slices and maps in the new models (labels, steps, needs, job_ids, items, lines, omitted), so TS types need no `| null`. TS `kind` and `level` stay `string` so JSON fixtures assign.
- Log redirects and artifact redirects both answer `302` to `*.blob.core.windows.net` (checked 2026-10-09 on darkmem job 112607627142 and a darkmem artifact); allowlist `.blob.core.windows.net` and `.githubusercontent.com`.

## Code facts gathered

- `c.do` (`internal/github/client.go:246-328`) reads whole bodies, classifies any status >= 300, caches ETag bodies; `classify` (`:330-368`) maps 410 to `ErrOther`.
- Client tests: `newClient(t, handler)` in `internal/github/client_test.go:24`, `TestClassify` table at `:91`.
- Daemon: GitHub interface `internal/daemon/backend.go:45-56`; shared fetch pattern `RunnerSteps` `:258-300`; `githubErr` `:791`; `repoName` only resolves configured repos (`:773`), so add `coveredName`; `actionsErr` wording `internal/daemon/actions.go:64-100`; `ghrRuns` `:245-269`; `fakeGH` `internal/daemon/backend_test.go:93`; `newBackend` `:254`; test config has repos darkcloud and darkmem, owner darkraise (`store_test.go:17-26`); watch a repo in tests via `b.Store.Update(func(c *config.Config) error { c.WatchRepos = []string{"docs"}; return nil })`.
- History entries key jobs by `Repo`, `RunID`, `JobName`; instances by `Job.RunID`, `Job.Name` (`internal/model/model.go:96-125`).
- API: `respond`, `FromGitHub`, `BadRequest` in `internal/api/server.go`; id parsing pattern `:251-256`; `setup(t)` and `c.call` in `api_test.go:178` and `client.go:34`.
- Web: `request` sends `X-GHR` (`web/src/api/client.ts:45`); `POLL_SLOW` 5 s, `POLL_ACTIONS` 15 s (`hooks.ts:5-8`); fixtures from `web/fixtures_test.go` (`GHR_UPDATE_FIXTURES=1 go test ./web/`), typed in `types.test.ts`, exported in `src/test/fixtures.ts`; `renderApp`, `renderRoutes`, `mockApi`, `setViewport` helpers in `src/test/`; route ids are `/app/<path>`; `Section` is itself a bordered card (`components/page/section.tsx`); `dur` gives `4m10s`; `humanBytes`, `clock`, `dateTime` in `lib/format.ts`; `resultWord` in `lib/history.ts`; `RouterLink` takes a string `to`.
- Fixture plan: run 102 darkmem "Fix the cache key", attempt 2 of 2, completed failure, head_sha `a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9`, PR 87, start 13:55:00; jobs 1001 Pick runner, 1002 Backend tests (failed step 3 "Lint (ruff)", step 4 skipped), 1003 and 1004 Frontend tests (1/2) and (2/2), 1005 E2E (api); graph nodes pick-runner c0, backend c1, frontend-tests c1 group, e2e c0 group; artifacts 11 frontend-dist 2,400,000 B, 12 coverage-backend expired. Total duration 4m10s.
- joblog test fixture: steps 1 Set up job 02:52:41-44, 2 Set up runner 44-44, 3 Initialize containers 44-54, 4 Create test databases 54-54, 5 Lint 54-54, 6 Test 52:54-53:31, 7 Run tests (no times), 8 Complete job 53:32-53:33, with the last line at 53:33; expected line counts 1, 3, 1, 3, 5, 6, 0, 1.
