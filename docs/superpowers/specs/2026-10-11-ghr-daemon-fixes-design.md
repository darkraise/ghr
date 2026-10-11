# ghr: daemon fixes

**Date:** 2026-10-11
**Status:** design presented in chat and approved by the owner on 2026-10-11, with the rulings on row grouping, public repositories, the runner cap and the execution strategy made the same day. Amended the same day after the review in docs/superpowers/notes/2026-10-11-ghr-daemon-fixes-fable-review.md, approved by the owner, and amended once more while the plan was drafted (§1.2, §2.1, §2.2, §3, §4.1, §4.2, §5.2, §5.4, §7.1 to §7.5 and §8), with the owner's rulings of that day on §4.1, §4.2, the Max help text and the token wording.
**Execution strategy:** sequential — 15 of the 26 rows edit `internal/daemon`, most of them `backend.go`, so they form one chain; the other 11 are small fixes in leaf packages.
**Register:** docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md (rows 19 to 45 except 33). This is sub-project 2 of 6; row 33 moved to sub-project 4.
**Code:** `internal/github`, `internal/history`, `internal/joblog`, `internal/wfgraph`, `internal/metrics`, `internal/config`, `internal/daemon`, `internal/runner` (§3 only), `internal/model`, `cmd/ghr/cli.go`, a new `internal/lru`, `deploy/config.example.yaml`, and five web files (`web/src/api/hooks.ts`, `web/src/api/types.ts`, `web/src/pages/run.tsx`, `web/src/pages/repository.tsx`, `web/src/lib/settings-form.ts`) with their tests and fixtures

## Goal

The review of build `forge-ui-930381d` found no critical or high defect in the daemon outside the runner manager. It found one way the history file can stop working for good, a log splitter that puts output under the wrong step, caches that outlive the token they were filled with, several values with no upper bound, and a set of small inconsistencies. This spec closes those.

## Decisions

- **Targeted fixes.** Nothing is restructured except the cache type, which moves to its own package because two packages now need it (§1.2).
- **Only a 401 rejects the token** in the GitHub client too. This applies the owner's ruling of 2026-10-11 on row 2 (docs/superpowers/specs/2026-10-11-ghr-runner-lifecycle-fixes-design.md §2.1) to the client's own token verdict.
- **A public repository needs `allow_public`** (owner's ruling, 2026-10-11). One without it gets no new runners, and neither does a repository whose visibility has not been read yet. The cost is accepted: a public repository configured before this change needs the flag added once.
- **The runner cap is 99, and `config.yaml` is clamped, not refused** (owner's ruling, 2026-10-11). A change through the API above the cap is refused.
- **Setup is pending from the moment ghr is configured until it is finished** (owner's ruling, 2026-10-11), on every install path, so a repeated finish has something to check.
- **A cleared `max` or `warm` reaches the repository page in this spec** (owner's ruling, 2026-10-11), not only the API.
- **The log splitter keeps timestamps and adds the runner's own markers.** A parser driven only by marker order was rejected: several step kinds (set up job, container start and stop) print no marker. Leaving the splitter alone was rejected: the fault reproduces on an ordinary job (§8).

## 1. GitHub client

### 1.1 Token verdict (rows 27 and 44)

Today `record` marks the token rejected on any `ErrAuth`, which includes every 403 that is not a rate limit, except on a Contents path. One repository refusing one call flips the token page to "rejected". `record` is the one place the verdict is set, for API calls and downloads alike.

- Only a 401 sets the verdict to rejected. A 403 leaves `CheckedAt` and `OK` as they were, like any failure that is not a verdict on the token.
- The Contents exemption and `contentsPathRe` are deleted. Row 44 (the exemption misses after a rename redirect) has nothing left to fix.

### 1.2 Bounds (rows 28 and 45)

Today the ETag cache holds up to 1,000 response bodies of any size and is emptied whole at the cap; a response body is read with no limit; a listing follows `next` links with no limit.

- The `lru` type in `internal/daemon/rungraph.go` moves to a new package `internal/lru` with exported `New`, `Get`, `Add` and `Remove`. Its `len()` method, used only by tests, is dropped. Its test moves with it from `rungraph_test.go` into the new package, where it may read the type's unexported fields as it reads `size` today. Callers that change: `joblogs.go` and `rungraph.go`.
- The ETag cache becomes an `lru` bounded at 1,000 entries and 32 MiB of body bytes.
- An API response body is read up to 8 MiB. A larger one fails the call with `github: response larger than 8 MiB`. The largest legitimate bodies are a 1 MB file in base64 and a page of 100 jobs with their steps, both well under that. Log and artifact downloads (`download.go`) have their own limits and are unchanged.
- The limit is checked before the response is classified or recorded, so an oversized response's status and rate headers are not used.
- `getAll` stops after 50 pages (5,000 items) with `github: listing has more than 50 pages`. The caller gets the error, never a partial list: the six listing methods return a nil list with any error, where today they return what they had gathered. Two callers are worth knowing: `ListUserRepos`, behind setup and the Add dialog's repository list, and `ListRuns`, which the tick calls per repository, where the error becomes that repository's poll failure with its backoff.

### 1.3 Cancellable writes (row 36)

`mutate` waits for its mutex and sleeps the 1-second spacing without looking at its context, so a request that was cancelled, or a daemon that is closing, still waits.

- The mutex becomes a one-slot channel, created in `New`, taken in a `select` with `ctx.Done()`. The spacing wait also ends when the context ends. Both return the context's error.
- `Client.Sleep` becomes `func(ctx context.Context, d time.Duration) error` so tests keep a fake clock. It is set in `New` and in six places in `client_test.go`.

## 2. Token replacement and reload

### 2.1 Checking a new token (row 23)

Today `SetToken` checks nothing when no runner repository is configured, checks only `Repos[0]` otherwise, and does not wake the loop. If `Repos[0]` no longer exists on GitHub, no token can be accepted.

- With at least one runner repository that is not being removed, `SetToken` runs the existing `CheckToken` on each of them, four at a time, within the request's context. Paused repositories are included: a token rotated while everything is paused must still be checked for the permissions the runners need.
- The results are judged in this order, and the first rule that applies decides:
  1. a 401 from any repository refuses the token: `new token rejected: ` followed by the API error's own text, for example `new token rejected: github: 401 Bad credentials`, the form ghr prints today;
  2. a rate limit from any repository refuses with 429 and its retry time;
  3. every repository failing refuses the token with a 400 naming the first repository in config order: `new token rejected: <repo>: <failure>`;
  4. otherwise the token is saved, and each failing repository gets a warning event `new token: <repo>: <failure>`. The next tick then shows the same problem on that repository through the per-repository error of the lifecycle spec §2.1.
- A 401 is recognised with `errors.As` on `*github.APIError`, because `checkToken` wraps its errors. The test fake in `backend_test.go` returns a plain error today and changes with it.
- With no such repository, the check is the one the setup wizard uses: the token must list at least one repository of the owner. `Backend` gains an injected `CheckTokenOwner func(ctx context.Context, token string) error`, built from `setup.check`'s listing step and wired where `CheckToken` is, in `start`.
- Nothing is saved when the token is refused. After saving, `SetToken` calls `Wake`.

### 2.2 Data fetched with the old token (row 24)

Today a token change clears the client's ETag cache and the label checks, but the daemon keeps its own copies: the Actions list with its per-repository errors, run details, parsed logs, annotations, log misses (the annotations route remembers a failed read for 10 minutes), "workflow file not found" marks, and runner steps.

- Each of those caches remembers the client's token generation (`TokenMeta().Generation`) it was filled under and empties itself when the generation differs, the way `labelChecks.refresh` already does. `ForgetCache` bumps the generation, so `SetToken` and a token-changing reload need no extra call.
- A fetch that began under an older generation is never served afterwards. For run details, logs and runner steps its result is not stored, and a caller that is waiting on it, or joins it before it ends, still gets its answer. The Actions list holds its lock for a whole build, so a build the token changed during is stored under the old generation and discarded by the next request, which builds again with the new token.
- Workflow file bytes and "not found" marks share one cache that cannot be emptied by kind, so each mark carries the generation it was made under, and a mark from another generation counts as absent.
- The activity cache, the setup-state cache and workflow file bytes hold nothing that depends on the token and are left alone.

### 2.3 Reload with the same token (row 25)

Today every reload calls `ForgetCache`, which drops the ETag cache, the token metadata, the label checks (through the generation) and any rate-limit suspension, also when only `config.yaml` changed.

- `Store.Reload` also reports whether the token changed. `Backend.Reload` calls `ForgetCache` only then. The other callers of `Store.Reload` take the extra result and ignore it: `OpenStore`, the SIGHUP handler of the unconfigured phase in `run.go`, and `store_test.go`.
- `ClearDegraded` still runs on every reload, so a reload remains the way to say "try the token again now".

## 3. Public repositories (row 32)

Today visibility is read once, in `AddRepo`. A repository written into `config.yaml` by hand is never checked, a repository made public later is never noticed, and the answer to "was public allowed?" is not stored anywhere.

A paused repository cannot serve as the model for a blocked one: the manager does not poll a paused repository and wipes its error, backoff and queue on every tick. A blocked repository must keep being polled and must keep showing why it is blocked. So blocking is its own state.

**Config**

- `config.Repo` gains `AllowPublic bool` (`allow_public`, omitted when false). `AddRepo` stores the request's value. `deploy/config.example.yaml` documents the key.

**Reading visibility**

- `runner.GitHub` gains `GetRepo`, and the package's test fake implements it.
- The manager keeps, per repository, whether GitHub last reported it public and when it was read. Entries for repositories no longer in the config are dropped on every tick, so a repository removed and added again starts unread.
- `gatherDemand` reads `GetRepo` for a repository right after that repository's poll succeeded, when the answer is missing or older than 6 hours. This is before the tick plans anything. `ClearDegraded` (token replaced, reload) marks every answer as due without discarding it.
- If `GetRepo` fails, the failure goes through `apiErr` as a poll failure does, and the previous answer, if any, stands.

**What blocked means**

- A repository is **blocked** when its last answer is public and its live config has no `allow_public`. A repository is **unread** while it has no answer yet.
- `spawnPlanned`, `stopIdle` and the runner-update gate work from a copy of the config in which every blocked repository has `Paused` set. The copy is never saved and never shown. So a blocked repository gets no new runners, its idle runners stop, and its running jobs finish. A runner that is still starting when its repository becomes blocked is not stopped: once it has registered it is idle and stops at the next tick, unless it has already taken a queued job. The window is at most `start_timeout`.
- An unread repository is only left out of `spawnPlanned`. Its runners are not stopped. In the normal case this costs nothing, because the read follows the first successful poll in the same tick; it delays spawning by a tick only when that read fails.
- A blocked repository is polled like any other, so a busy runner still gets its job's details, but its queued jobs are discarded from the manager's demand: status shows `Queued 0` for it, and its queue does not hold back a runner update. A failed poll of a blocked repository does not count toward "a repository did not answer".

**What the owner sees**

- `Status` shows, for a blocked repository, `<repo> is public; ghr serves only private repositories. Set allow_public: true for it in config.yaml and reload to serve it`. This text takes precedence over the repository's poll error and spawn error, and is kept apart from them, so a successful poll does not clear it. Its `Paused` field stays what the config says.
- One event is written when a repository becomes blocked (level `warn`, the status text) and one when it stops being blocked (level `ok`, `no longer blocked`). Removing a repository writes neither.
- Known limits: a repository removed and added again before the next tick keeps its previous answer until the next read; the only wrong outcome is a repository that is private again staying blocked, because adding a public one without the flag is refused. And while API calls are paused, a repository blocked by a config change keeps its last queued count until calls resume; the message and the event are immediate.
- Accepted consequence: a public repository configured before this change is blocked from the first tick after the upgrade until the flag is added.
- No web control for the flag in this spec. It is set by adding the repository with the switch, or in `config.yaml` followed by a reload.

## 4. Job logs

### 4.1 Splitting a log into steps (rows 20 and 31)

`joblog.Parse` assigns a line to a step by the step's start and end times, which GitHub rounds to whole seconds, plus one rule: a `##[group]Run ` line, when the current step already has lines, moves to the next step whose window contains it. Two faults follow.

- A skipped step carries equal start and end times and prints nothing. It is a candidate window like any other, so it can take the next step's header and first second of output.
- Post steps and `Complete job` usually all start in the second the last regular step ended. Nothing moves the parser on, so their output stays under that step and they show no lines.

Two definitions:

- **The runner's post steps** are found by position, not by name alone: starting from the last step and skipping `Complete job`, `Complete runner` and `Stop containers`, they are the unbroken run of steps named `Post ` followed by the name of an earlier step, which is how the runner names them (`Post Run actions/checkout@v4`, `Post Build image`). A step a person named `Post coverage comment` is a regular step, in the middle of a job or last, unless an earlier step is named `coverage comment`.
- **A regular step** is any step that is not `Set up job`, `Initialize containers`, `Stop containers`, `Complete job` or one of the runner's post steps. Those print no `##[group]Run ` header of their own. `Set up runner` and `Complete runner`, the steps of the job-started and job-completed hooks ghr sets on every runner it manages, print one (`##[group]Run '/opt/ghr/hooks/job-started.sh'` and its `job-completed.sh` twin), so they are regular steps and rule 2 opens them. `Complete runner` is skipped only in the walk that finds the post steps.

There is one pointer, the current step, as today. The rules, in order:

1. A step whose conclusion is `skipped` takes no lines and is never a candidate. `model.Step` already carries the conclusion. When every step is skipped, no step gets a line.
2. A `##[group]Run …` line, when the current step already has lines, moves to the first later regular step whose window contains the line's time. With no such step it stays with the current one, as today.
3. A `Post job cleanup.` line moves to the first later runner's post step whose window contains the line's time, unless the current step is one of the runner's post steps that has no lines yet; then it stays. With no such step it stays with the current one.
4. A `Cleaning up orphan processes` line, when the last step with times is named `Complete job` and the line's time falls in that step's window, moves to that step. A script that prints the same text earlier does not move the parser.
5. Any other line stays with the current step, unless its time is more than one second past the current step's end; then it moves to the next step that has not ended, as today.

Compared with today these rules add candidates the parser could not reach and remove two kinds of wrong candidate (skipped steps and steps that print no header); for any other line they decide as today.

Known limits, not fixed:

- An inner step of a composite action prints its own `##[group]Run ` line. When it starts within the last second of its outer step, rule 2 can move to the following step early. Telling the two apart needs the action's definition, which the log does not carry. This is the composite part of row 31.
- Lines the runner prints for `Complete job` before `Cleaning up orphan processes` (job outputs) stay with the last post step.
- A step that is not skipped, prints nothing and lasts under a second can still take the next step's header. No such step has been seen.
- The line the runner prints ahead of each hook's header (`A job started hook has been configured by the self-hosted runner administrator` and its `completed` twin) stays with the step before it.
- `Initialize containers` prints no marker, so its first second of output stays with the step before it, as today.

### 4.2 A job with no log (row 26)

`RunAnnotations` excludes skipped jobs but counts every other completed job whose log cannot be read as a failed read. A job cancelled before it started has no log.

- For a job that never ran, `api.github.com` answers the log request with a redirect and the blob store behind it answers 404 (§8). `OpenJobLog` in `internal/github/download.go` reports that 404 as an `*APIError` of kind `ErrNotFound`, where today it is a plain error that the routes turn into a 502.
- When the log of a completed job is not found, the outcome is a new `logNone`, remembered for 10 minutes like an expired log. `finishedJob` has already read the job by then, so the 404 is about the log, not the repository.
- Not verified: whether the blob store can answer 404 for a moment right after a job completes. If it can, that job's page says it produced no log for up to 10 minutes.
- `model.JobLog` gains `NoLog bool` (`no_log`). `JobLog` returns it flagged, as it does for an expired or too large log. `RunAnnotations` counts such a job nowhere.
- The raw log route answers the same 404 with `this job produced no log` instead of "not found on GitHub".
- Web: `JobLog` in `types.ts` gains `no_log`, the job page shows `This job produced no log.`, and offers no raw log download for it. Fixtures that build a `JobLog` gain the field: `web/src/api/fixtures/job-log.json`, `log-viewer.test.tsx`, `log-view.test.ts`.

## 5. Run page data

### 5.1 A re-run of a finished run (row 22)

A completed attempt is cached 60 seconds, also under the "latest attempt" key, and the page stops polling once the shown attempt has completed. A re-run stays invisible until a reload after the cache ends.

- The entry stored under the latest-attempt key (attempt 0) lives 15 seconds whatever its status. An entry under a numbered attempt keeps 60 seconds once completed.
- `runDetailPoll` in `web/src/api/hooks.ts` takes the requested attempt as a second argument. It returns `POLL_SAFETY` (60 s) for a completed run requested as the latest attempt, and still `false` for a numbered attempt. Each such poll is three conditional GitHub requests. `useRunDetail` and `client-pages.test.ts` change with it.

### 5.2 A workflow file that cannot be decoded (row 34)

`GetContent` returns a plain error when GitHub answers `encoding: none` (a file over 1 MB) or bad base64, and `runGraph` fails the whole run page on any error that is not a 404 or a 403.

- `GetContent` wraps those two defects in a new `github.ErrBadContent`, whose text is `github: file content cannot be decoded`. `runGraph` answers it with the existing "ghr could not read <path>" note. Network, rate-limit and server errors still fail the call.
- `workflowFile` remembers such a file as bad, without expiry, as it remembers bytes: a file at a commit never changes, so it is not fetched again on every page load.

### 5.3 Graph matching (row 41)

- When two patterns match a job name with equal literal prefixes, the one with more literal text in total wins; a remaining tie goes to the earlier job.
- A `strategy` written as one expression (`strategy: ${{ … }}`) no longer fails the file. The job counts as a matrix job.
- A run with no jobs yet returns no graph with the note `This run has no jobs yet.`, before the workflow file is read.

### 5.4 Runner steps and activity (rows 29 and 30)

- `RunnerSteps` checks `githubErr` immediately before its GitHub call, not first. An unknown runner still answers 404, a runner without a job still answers empty, and a cached answer is still served, all without GitHub and so also while the token is rejected or calls are paused.
- Its fetch recovers from a panic the way `fetchDetailSafely` does, so the shared call always completes, with the error `could not read the runner's steps`.
- `Activity` reads instances, then pending records, then history, the order `ghrRuns` documents: a job moves through them in that order. The comment that argues for the current order is replaced.

## 6. History and metrics

### 6.1 An oversized history line (row 19)

`readAll` uses a scanner with a 1 MiB line limit. One longer line makes every `Append`, `Query` and `Prune` fail with `token too long`, and `Prune` cannot remove it because it reads first.

- `readAll` reads with a `bufio.Reader` and skips a line longer than 1 MiB the way it already skips one that is not valid JSON. It reports how many lines it skipped, counting both kinds. The store logs once when that number changes to a value other than zero: `history: <path>: skipped <n> line(s) longer than 1 MiB or not valid JSON`. A missing file counts as a read that skipped nothing, so a bad line in a file written afterwards is logged again.
- `Append` is not given a size check: an entry ghr writes is a few hundred bytes, and a larger one is now harmless on read.

### 6.2 When a prune rewrites, file mode and durability (rows 19 and 35)

- `prune` rewrites the file when at least one entry is older than the cutoff or its read skipped at least one line. Otherwise it writes nothing and does not count as a write, so it publishes no `history` change. Today's early return for a file with no readable entry is removed, so a file holding only an oversized line is rewritten empty.
- `history.jsonl` is created 0600, the mode a prune already leaves it with. `append` also sets 0600 on a file it opens with a wider mode, because rewrites are now rare.
- `append` syncs the file before closing it; `prune` syncs the temporary file before the rename.

### 6.3 Clock steps (row 42)

- `Load` drops samples and rollups dated after now. A saved open hour dated after the current hour is already dropped today; that stays.
- `addToHour`, given a sample from an hour before the open one (the clock stepped back), drops the open hour and every rollup that is not before the sample's hour, then opens the sample's hour. Samples of the new time are no longer left out. Minute samples dated after the new sample are dropped on any backwards step, also one inside the open hour, which matches what `Load` does.
- Not closed here: after a restart with the clock set back inside the current hour, `Load` keeps a closed rollup of that hour and a second one is made later. This exists today and is recorded as its own register row.

## 7. Config and setup

### 7.1 Runner cap (row 21)

- `config.MaxRunners = 99`, the limit the Add repository dialog already applies.
- `Parse` lowers a `global_max`, a repository `max` or a repository `warm` above 99 to 99 and returns a warning such as `global_max 500 is above 99; using 99`, or `<repo>: max 120 is above 99; using 99` for a repository's value. The file is not rewritten by loading; the next save writes 99.
- `Validate` refuses a value above 99 (`global_max must be <= 99`, `<repo>: max must be <= 99`, `<repo>: warm must be <= 99`). `Parse` clamps before it validates, and `Store.Update` validates a copy of an already clamped config, so only an API change (PATCH, add repository) can reach the refusal, and it gets a 400.
- `Activity` pads its lanes up to `global_max`; the cap bounds that at 99 and nothing else changes there. `deploy/config.example.yaml` names the cap beside `global_max`.
- Web: the settings form's `global_max` check becomes 1 to 99. How capacity is drawn (row 117) and the Add dialog's own clamping (row 76) belong to sub-projects 5 and 4.

### 7.2 Adding a repository (row 37)

- "Already configured" is checked inside the update, so a concurrent duplicate gets 409. The early check outside stays as a fast path.
- The repository is stored under GitHub's spelling, the name part of `full_name`, which is also its new name when GitHub redirected a renamed one. The duplicate check inside the update, the removal from `watch_repos`, `forgetLabelCheck` and the event all use that name.
- When the owner part of `full_name` differs from the configured owner, ignoring case, the repository was transferred: the add is refused with 400: `<typed name> now lives at <full_name>; ghr serves only repositories of <owner>`.

### 7.3 Patching config (row 38)

- `update` writes a config warning as an event only when the config before the update did not already have it.
- `RepoPatch.Max` and `RepoPatch.Warm` change from `*int` to a small value type that tells three cases apart: absent (leave the value), `null` (clear it, so it is inherited again) and a number. It implements `UnmarshalJSON`, which the JSON decoder calls for `null`, and is written with `omitzero`, so an absent value is still left out. `cmd/ghr/cli.go` and `backend_test.go` build the new type.
- Web: `RepoPatch` in `types.ts` allows `null` for both. On a repository page, emptying the Max or Warm box becomes a change that sends `null`, where it is an error today; the comment in `repository.tsx` that explains the error goes with it. The Max box's help text loses its last sentence, which would no longer be true, and reads `Leave empty for the default: 1 in queue mode, no limit in all mode.`
- Repository names in a PATCH are applied in sorted order, so the unknown repository reported is always the same one.

### 7.4 Setup (rows 39 and 40)

Today `POST /setup/finish` can be repeated after setup and queues the popular toolchain set each time. Only `deploy/setup.sh` creates the `setup-pending` marker, so on an install without it nothing records that setup is unfinished.

- `configure` creates the `setup-pending` marker before it saves the owner and token, and fails if it cannot. Every install path now has the marker from the moment ghr is configured. A marker left behind by a failed configure is harmless: an unconfigured daemon shows the wizard anyway.
- `finish` acts only while `setup-pending` exists: it queues the popular set when asked, removes both markers and writes its event. Without the marker it answers 204 and does nothing. A daemon that is not configured yet still answers 409 first, as today, so it is never told that setup finished.
- `finish` checks the marker and acts on it under a lock of its own, so several finishes arriving together queue the popular set once and write one event.
- Consequences: after `ghr setup github`, the web UI shows its "setup is not finished" notice until `ghr setup finish`, the flow the README describes. A wizard on an install without `setup.sh` now survives a page reload between configuring and finishing.
- The comment on `configure` says what happens: a second request waits for the first and then gets the 409.

### 7.5 Detached goroutines (row 43)

- `scanLabels` and the per-repository fetch in the Actions build recover from a panic. A scan ends as a partial result with the error `could not finish the check` and no groups; a fetch ends as that repository's error, `could not read the runs`.

## 8. Verify before fixing

- **Row 20:** verified on 2026-10-11. In job 114375054549 of `darkraise/darkraise-ai-plugins`, step 9 is `skipped` with start and end both 03:10:26Z.
- **Row 31:** verified on 2026-10-11 by running `joblog.Parse` over the log of job 114400898499 of `darkraise/ghr`: steps 26 to 29 got no lines, and three `Post job cleanup.` blocks and `Cleaning up orphan processes` were filed under step 14. Also verified on two jobs run by ghr's own runners, jobs 111871024678 and 111870951263 of `darkraise/darkmem`, which end `Post …, Complete runner, Complete job`: with `Complete runner` skipped in the walk, every post step, `Complete runner` and `Complete job` get their own lines. In the second of them, today's parser files the next step's header and 11 lines under a skipped step, a live instance of row 20.
- **Row 26:** verified on 2026-10-11 on two jobs cancelled before they started, job 114404210515 of `darkraise/darkrouter` and job 111363230492 of `darkraise/ghr-e2e`: `api.github.com` answers 302 and the blob store answers `404 The specified blob does not exist`. An expired log differs: `api.github.com` itself answers 410.
- **Steps after a cancelled step:** verified on 2026-10-11. In job 114404096068 of `darkraise/darkrouter` only the interrupted step is `cancelled`; the steps after it are `skipped` with equal times, so rule 1 covers them.
- **Row 41, the `strategy` expression:** not verified against a real workflow. The fix does not depend on it: a scalar `strategy` stops being an error.

## 9. Testing

Every fix starts from a failing test. Go tests use the packages' existing fakes and clocks, and no new test calls `time.Sleep`; where a result arrives on a detached goroutine, the daemon tests' existing polling helpers are used.

- **Client:** a 403 leaves the token verdict; a 401 sets it rejected; the cache evicts the least recently used entry by count and by bytes; a body over the limit and a listing over 50 pages fail with no partial result; `mutate` returns when its context ends while queued and while spacing.
- **Token and reload:** a new token is refused on a 401 from one repository while another passes; refused with 429 when one is rate-limited and another passes; refused when every repository fails; saved with warning events when some fail; checked against a paused repository; saved through the listing check when no runner repository exists; `Wake` is called. After a token change the Actions list, a run detail, a parsed log, a log miss and runner steps are fetched again, and a fetch that straddles the change is not stored. A reload with the same token keeps the ETag cache and a rate-limit suspension.
- **Public repositories:** a public repository without the flag spawns nothing, stops its idle runner, shows the message after a later successful poll, and shows `Queued 0`; with the flag it is served; an unread repository spawns nothing and keeps its idle runner; a failing read keeps the last answer; the read repeats after 6 hours and after `ClearDegraded`; a blocked repository's queue, and its failed poll, do not hold back a runner update; a repository removed while blocked and added again starts unread and writes no "no longer blocked" event.
- **Job logs:** fixtures shaped like the two logs in §8: a skipped step takes no lines and the next step keeps its header; each post step and `Complete job` get their own lines. Also: a regular step a person named `Post …`, in the middle of a job or last, keeps its own header and takes no `Post job cleanup.` line; `Cleaning up orphan processes` printed by a script before `Complete job` leaves the parser where it is; a `##[group]Run ` line never moves to `Initialize containers` or `Stop containers`; a `##[group]Run ` line with no fitting later step stays put. A completed job whose log is 404 returns `no_log`, is not counted as failed, and its raw log answers the no-log message.
- **Run page:** a completed latest attempt is refetched after 15 s and a numbered one after 60 s (the second already holds today and guards it); a bad workflow file gives the note and the page and is fetched once; the three graph cases of §5.3; `RunnerSteps` answers 404 for an unknown runner and serves a cached answer while the token is rejected, and completes after a panic; a job moving from instance to history during `Activity` is in the result.
- **History and metrics:** a file with a 2 MiB line still appends and queries, the skip is logged once, and a prune removes the line, also when it is the only line; the file is 0600 when created and after an append to a 0644 file; a prune that drops nothing leaves the file and the write count alone; future-dated samples are dropped on load; a sample after a backwards step is counted.
- **Config and setup:** `Parse` clamps and warns; `Validate` refuses above 99; a clamped config saves and loads again; a concurrent duplicate add gets 409, the stored name is GitHub's, and a transferred repository is refused; `null` clears `max` and `warm` while an absent key leaves them; an unknown repository in a PATCH is reported in sorted order; an unchanged warning is not written again; `configure` creates the marker; a second `/setup/finish` answers 204 and queues nothing; a finish with no marker queues nothing; eight finishes at once queue the popular set once.
- **Web:** unit tests for `runDetailPoll` with and without an attempt, the settings form's bound, the job page's no-log sentence and missing download, and a repository page that sends `null` for an emptied Max.
- **Suites:** `go vet ./...` and `go test -race ./...` (Go is at `/usr/local/go/bin`); in `web`, lint, typecheck and the unit suite.
- **Live check:** deploying to the runner LXC needs the owner's yes each time. With a yes: add `allow_public: true` to the public repository first, then one job to completion, its log page with a post step, and a token replacement.

## Out of scope

- A change missed between a page's first fetch and its stream opening (row 33): sub-project 4, with rows 85 and 86.
- README wording about private repositories (row 74) and labels validation (row 57): sub-project 3.
- The job-log 404 wording for other causes (row 92): sub-project 4.
- A web control for `allow_public`, and a CLI form for clearing `max` or `warm`.

## Row map

| Row | Section |
|---|---|
| 19 | §6.1, §6.2 |
| 20 | §4.1 |
| 21 | §7.1 |
| 22 | §5.1 |
| 23 | §2.1 |
| 24 | §2.2 |
| 25 | §2.3 |
| 26 | §4.2 |
| 27, 44 | §1.1 |
| 28, 45 | §1.2 |
| 29, 30 | §5.4 |
| 31 | §4.1; its composite part is a known limit there |
| 32 | §3 |
| 34 | §5.2 |
| 35 | §6.2 |
| 36 | §1.3 |
| 37 | §7.2 |
| 38 | §7.3 |
| 39, 40 | §7.4 |
| 41 | §5.3 |
| 42 | §6.3 |
| 43 | §7.5 |
