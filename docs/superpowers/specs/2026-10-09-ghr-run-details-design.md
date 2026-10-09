# ghr web UI: workflow run details

**Date:** 2026-10-09
**Status:** approved section by section in chat on 2026-10-09; mockup at https://claude.ai/artifact/FurnXQ2U5Z22fF2vqb9HFy (example data, approved as the layout). Revised the same day for the Fable review (docs/superpowers/notes/2026-10-09-ghr-run-details-fable-review.md, findings 1 to 40), all dispositions approved by the owner.
**Register:** docs/superpowers/registers/2026-10-09-ghr-run-details.md
**Builds on:** docs/superpowers/specs/2026-10-09-ghr-actions-view-design.md (the Actions view), and through it spec 1's visual system and anti-slop rules (§1.7) and spec 2's page kit and page-wide rules (§1). They bind the pages here; this spec does not repeat them.
**Code:** `web/`, `internal/api`, `internal/daemon`, `internal/model`, `internal/github`, new `internal/joblog`, new `internal/wfgraph`

## Goal

The Actions page lists runs but sends the owner to github.com for everything else. This spec adds GitHub's run page to ghr: a run summary, the job graph, annotations, artifacts, attempts, and each job's steps and logs, for every repository ghr covers (configured or watched).

## Decisions

- **The daemon does the work** (§2): it fetches from GitHub, builds the graph, parses logs and extracts annotations, and caches the results; the browser renders. Chosen over relaying raw data to the browser and over annotations only inside an opened log.
- **Graph from the workflow file, timeline otherwise** (§3). GitHub's API has no job dependency data, so the graph reads the workflow YAML at the run's commit, which needs the PAT's **Contents: read**. Without it the page draws a job timeline and names the missing permission. Contents: read stays optional; the README lists it as optional.
- **Annotations come from job logs** (§4.3). GitHub's annotations endpoint is part of the Checks API, and fine-grained PATs have no Checks permission (it exists for GitHub Apps only: checked against GitHub's two permission tables on 2026-10-09). Log lines carry `##[error]`, `##[warning]` and `##[notice]` with the message only; GitHub's file and line fields are not in the log. On a darkcloud failure the two parsed lines matched the two API annotations' messages exactly.
- **First version scope:** summary, graph or timeline, annotations, artifacts, attempts, job page with step logs and search.
- **Read-only.** No re-run or cancel, which would need Actions: write.
- **Downloads go through JSON or a fetch, never a navigation to `/api/`**, because the web handler refuses `/api/` requests without `X-GHR: 1` (`internal/webui/handler.go:68`) and a navigation cannot send it (§2.1).

## 1. Pages

### 1.1 Routes

- Run page: `/actions/$repo/runs/$id`, search `attempt?: number`.
- Job page: `/actions/$repo/runs/$id/jobs/$jobId`, search `attempt?: number`, `step?: number`, `line?: number`. `step` and `line` come from an annotation link; the page opens that step and scrolls to and marks that line.
- `$repo` is a bare repository name under `owner`, as on the Actions page. `$id`, `$jobId`, `attempt`, `step` and `line` must be positive integers; anything else shows the not-found line of §1.6 without calling the API.
- Both routes sit under the app layout and the Actions nav item stays active; the plan checks how the kit's sidebar matches nested paths and sets prefix matching if it needs to.
- On the Actions page a run's title links to its run page instead of `html_url`; the GitHub link moves to the run page header.

### 1.2 Shared frame

- `PageHeader` as on the repository page (`web/src/pages/repository.tsx`): breadcrumbs Actions, repository, workflow, `#run_number` (mono), and on the job page the job name; title is the run title; description is one sentence naming workflow, `#run_number`, repository and branch, with numbers and the branch in mono. The status icon sits before the title.
- Header actions: the attempt picker, shown only when the run has more than one attempt ("Attempt 2 (latest)", "Attempt 1"), and "Open on GitHub" (the run's `html_url`; GitHub's attempt object carries the same URL, with no attempt in it).
- Status icons come from a new shared `RunStatusIcon({ status, conclusion })` extracted from the Actions page's private `StatusIcon` (`web/src/pages/actions.tsx:36-40`); the Actions page uses it too.
- Left rail, sticky on wide screens: "Summary", a "Jobs" label, one entry per job (status icon, name, duration in mono), then "Run details" with a "Workflow file" link. Below 800 px the rail stacks above the content.
- GitHub links (commit, pull request, workflow file) are built from `config.owner`: `https://github.com/{owner}/{repo}/…`; the workflow file link is `blob/{head_sha}/{path}`.
- While the shown attempt is not completed, the run query refetches every `POLL_ACTIONS` (15 seconds) and stops once it completes.

### 1.3 Run page (summary)

Four blocks, each a `Section` (spec 2 §1.1), not cards, so the graph's job tiles are the only bordered elements inside them (spec 1 §1.7):

1. **Summary:** "Triggered via {event}" with actor, short `head_sha` (mono, linked) and pull request numbers (mono, linked); Status (icon and word); Total duration (mono, from the attempt's start to its last job's completion, or to now while active); Artifacts (count, mono).
2. **Graph:** titled with the workflow file's base name and the event, or "Jobs timeline" when §3.4 applies, with its one-line reason under it.
3. **Annotations:** heading with error, warning and notice counts (each shown only when non-zero); one row per annotation with level icon, message (mono, wraps), "{job} / {step}" and a "Log line {n}" link to the job page carrying `attempt`, `step` and `line`. Footer: "Taken from the job logs, so file and line come only from the message text." Status lines, each only when its count is non-zero: "Reading logs for {n} jobs" (`unread`), "Logs for {n} jobs have expired" (`expired_logs`), "{n} job logs are too large to read here" (`too_large`), "{n} job logs could not be read" (`failed`). After the per-job cap: "{n} more in the log" under that job's last row. With no annotations and every count zero: "No errors, warnings or notices in the finished jobs' logs."
   The annotations query refetches every `POLL_SLOW` (5 seconds) while `unread` is above zero, every `POLL_ACTIONS` while the run is active, and stops when both are false.
4. **Artifacts:** a table of name (mono), size (mono, human units, right-aligned), expires (mono date, right-aligned), and a "Download" button (§2.1's flow). An expired artifact shows "Expired" in muted text instead of the button. Hidden when the run has no artifacts. Artifacts belong to the run, so every attempt shows the same list, as GitHub does.

### 1.4 Job page

- Section heading: status icon, job name, duration (mono). Below it: runner name (mono), labels as plain mono words separated by gaps, the word "ghr" in muted mono when ghr ran the job (§2.4), and start time (mono).
- Toolbar: a search box ("Search logs") and a "Raw log" button (§2.1's flow).
- Steps: one row per step (chevron, status icon, name, duration in mono) that expands to its log. The first failed step starts expanded; with `step` in the URL, that step does instead.
- Running or queued job: the step list without logs, using the existing step timeline, and the note "Logs appear when the job finishes."
- A log too large to parse (§4.1) shows "This log is too large to show here." with the Raw log button. An expired log shows "GitHub no longer keeps this job's log." and no Raw log button.
- A job ID that is not in the shown attempt's jobs: "This job is not part of attempt {n}." with a link to the run page.

### 1.5 Log viewer

- Lines render only inside expanded steps. Each line: per-step line number (muted, not selectable), then text in mono.
- `group` lines render as collapsed disclosure rows (`aria-expanded`) titled with the group name; their lines up to the matching `endgroup` render when opened. `endgroup` lines are not shown.
- `error` and `warning` lines get a tinted background and colour and the prefix "Error: " or "Warning: "; `notice` gets "Notice: " with no tint; `command` lines use the primary colour; `debug` lines are muted.
- Text rendering is a tokenizer that returns React elements; no HTML strings and no `dangerouslySetInnerHTML`. ANSI SGR codes for the 8 and bright 8 foreground colours and bold become spans; every other escape sequence (other CSI, OSC such as hyperlinks) is removed, as are C0 control characters other than tab. A line containing `\r` shows only its last segment. Colours map to log palette tokens so both themes read.
- A step with more than 5,000 lines renders its last 5,000 and a "Show {n} earlier lines" button above them. When `line` or the current search match falls above the cut, the earlier lines render.
- Search: case-insensitive substring over every line of every step, run on the loaded data. The box shows "{i} of {n}"; Enter and Shift+Enter move to the next and previous match. Only the step and groups holding the current match open, and the match scrolls into view and is marked; other matches in rendered lines are marked too.
- The log viewer's background is dark in both themes, like GitHub's, with its own tokens defined for light and dark.

### 1.6 Errors on the pages

All use the existing `ErrorLine` or `Alert`:

- 404 from ghr for an uncovered repository: "{repo} is not configured or watched in ghr." with a link to the Repositories page.
- 404 from GitHub for the run or job, or an invalid route parameter: "This run no longer exists on GitHub."
- 403: the daemon's wording, which names the missing permission (§2.4). 429: today's "Retry after HH:MM". Data already shown stays on screen.

## 2. Backend

### 2.1 Routes

Every route first resolves `{repo}` against the configured and watched names (case-insensitive, `coveredRepos`) to its configured spelling, used from then on for GitHub URLs and cache keys; otherwise it answers 404 "repo {repo} is not configured or watched". `{id}`, `{jobId}` and `attempt` must parse as positive integers, otherwise 400 "{name} must be a positive integer". Every route answers through `respond`.

| Route | Returns |
|---|---|
| `GET /actions/{repo}/runs/{id}?attempt=N` | `model.RunDetail` |
| `GET /actions/{repo}/runs/{id}/annotations?attempt=N` | `model.RunAnnotations` |
| `GET /actions/{repo}/jobs/{jobId}/log` | `model.JobLog` |
| `GET /actions/{repo}/jobs/{jobId}/log/raw` | `model.RawLog` |
| `GET /actions/{repo}/artifacts/{id}/download` | `model.DownloadURL` |

- `attempt` absent means the latest attempt. An `attempt` above the run's `run_attempt` answers 400.
- The log routes answer 409 "job has not finished" for a job whose status is not `completed`.
- **Artifact download:** the route asks GitHub for the artifact's zip without following the redirect and answers `{url}` with the `Location`, after checking it is `https` on a host ending in `.githubusercontent.com` or `.blob.core.windows.net` (otherwise 502 "GitHub returned an unexpected download address"). The browser's Download button fetches the route with `X-GHR` and then sets `window.location` to `url`; GitHub's signed URL needs no credentials and expires after about a minute, so the button fetches it on each click. 410 from GitHub answers 410 "artifact has expired".
- **Raw log:** the route returns `{text}` (the log body, at most 32 MiB, otherwise 413 "log is larger than 32 MiB; open it on GitHub"); it is not cached. The Raw log button fetches it with `X-GHR`, turns it into a Blob, and saves it through a temporary object URL with the file name `{repo}-job-{jobId}.log`. No route sets `Content-Disposition`.

### 2.2 Model

Go types in `internal/model`, JSON names in brackets.

- `RunDetail`: `Repo` (repo), `Watched` (watched), `ID` (id), `RunNumber` (run_number), `Attempt` (attempt), `Attempts` (attempts, the run's latest `run_attempt`), `Workflow` (workflow), `Title` (title), `Branch` (branch), `Event` (event), `Actor` (actor), `Status` (status), `Conclusion` (conclusion), `HeadSHA` (head_sha), `Path` (path), `PullRequests []int` (pull_requests), `StartedAt` (started_at), `UpdatedAt` (updated_at), `HTMLURL` (html_url), `Jobs []RunJob` (jobs), `Graph *RunGraph` (graph, omitted when §3.4 applies), `GraphNote string` (graph_note, omitempty), `Artifacts []RunArtifact` (artifacts), `FetchedAt` (fetched_at).
- `RunJob`: `ID`, `Name`, `Status`, `Conclusion`, `StartedAt *time.Time`, `CompletedAt *time.Time`, `RunnerName` (runner_name), `Labels []string`, `HTMLURL`, `GHR bool` (ghr), `Steps []Step` (the existing `model.Step`).
- `RunGraph`: `Nodes []GraphNode` (nodes). `GraphNode`: `Key` (key, the YAML job ID), `Name` (name, the YAML `name` with expressions kept, or the key), `Column int` (column), `Needs []string` (needs, keys), `JobIDs []int64` (job_ids, in the jobs API's order), `Group bool` (group: a matrix or a reusable-workflow caller).
- `RunArtifact`: `ID`, `Name`, `SizeBytes int64` (size_bytes), `Expired bool`, `ExpiresAt *time.Time` (expires_at).
- `RunAnnotations`: `Items []Annotation` (items), `Omitted map[int64]int` (omitted, job ID to the count beyond the per-job cap; TypeScript `Record<string, number>`), `Unread int` (unread, finished jobs whose logs were not ready within the wait), `ExpiredLogs int` (expired_logs), `TooLarge int` (too_large), `Failed int` (failed).
- `Annotation`: `Level` (level: error, warning or notice), `JobID`, `JobName`, `Step int`, `StepName`, `Line int` (per-step line number), `Message`.
- `JobLog`: `JobID`, `TooLarge bool` (too_large), `Expired bool` (expired), `Steps []LogStep`. `LogStep`: `Number`, `Name`, `Lines []LogLine`. `LogLine`: `N int` (n), `Kind string` (kind: "", group, endgroup, error, warning, notice, command, debug), `Text` (text, with the `##[…]` prefix removed and ANSI kept).
- `RawLog`: `Text` (text). `DownloadURL`: `URL` (url).

### 2.3 GitHub client

`internal/github` gains:

- `Run` fields `HeadSHA` (head_sha), `Path` (path), `RunAttempt` (run_attempt), `PullRequests []struct{ Number int }` (pull_requests).
- `ErrGone`, the `ErrKind` `classify` gives a 410 (verified: an expired job log answers `410 Gone`).
- Through the existing `do` path: `GetRun(ctx, repo, id)`, `GetRunAttempt(ctx, repo, id, attempt)`, `ListAttemptJobs(ctx, repo, id, attempt)` (`/attempts/{n}/jobs?per_page=100`, all pages), `GetJob(ctx, repo, jobID)` (`/actions/jobs/{job_id}`), `ListArtifacts(ctx, repo, runID)` (all pages), `GetContent(ctx, repo, path, ref) ([]byte, error)` (escapes `path` segment by segment, keeping `/`; decodes base64).
- A new low-level path for redirected downloads, because `do` reads whole bodies, treats any status of 300 or above as an error and keeps ETag bodies (`client.go:286-326`):
  - `request(ctx, method, u) (*http.Response, error)` does what `do` does before and after the round trip (suspension check, headers, `record`, `classify` for statuses of 400 and above) but uses a copy of the client whose `CheckRedirect` returns `http.ErrUseLastResponse`, returns the response unread, and never touches the ETag cache.
  - `redirectURL(ctx, u) (string, error)` calls `request`, expects a 302, and returns `Location`.
  - `JobLog(ctx, repo, jobID, max int64) ([]byte, error)` takes `redirectURL` for `/actions/jobs/{id}/logs`, then fetches that URL with a separate plain `http.Client` (no `Authorization`, dial and response-header timeouts of 10 seconds, no whole-request timeout, the body read through a 30 second idle deadline) and returns `ErrTooLarge` past `max`.
  - `ArtifactURL(ctx, repo, id) (string, error)` is `redirectURL` for `/actions/artifacts/{id}/zip`.
- Because the second request is built without the token, nothing depends on Go's cross-host header policy.

### 2.4 Assembly

- Every new backend method calls `b.githubErr()` first, like the other GitHub-calling routes (`internal/daemon/backend.go:791`), and turns GitHub errors into `api.Error` with `actionsErr`'s wording (`internal/daemon/actions.go`), so a 403 names the missing permission and a 429 carries `RetryAt`. 404 stays 404 and 410 stays 410.
- Latest attempt: `GetRun` and `ListJobs` (`filter=latest`, which returns the latest attempt's jobs). Earlier attempt: `GetRun` for `Attempts`, then `GetRunAttempt` and `ListAttemptJobs`. A re-run attempt lists every job of the workflow: jobs that were not re-run keep attempt 1's times and conclusions under new job IDs, and their logs are available (checked on darkmem run 37498148517 and darkrouter run 33861987112).
- Artifacts: `ListArtifacts`, the same list for every attempt.
- Graph: §3, from `Path` at `HeadSHA`.
- The log routes call `GetJob` first for the job's status, run and steps, check that the job's run belongs to `{repo}` (the job URL already names it), and answer 409 when it has not completed.
- A job is marked `GHR` when (repo, run ID, job name) appears in ghr's instances, pending jobs or history, the same sources `ghrRuns` reads. Two attempts with the same job name are both marked; history has no job ID to tell them apart.
- `StartedAt` follows `actionsRun`'s fallback (`run_started_at`, else `created_at`).
- Interfaces gain the new calls: the daemon's `GitHub` interface (`internal/daemon/backend.go:45`) and `fakeGH` (`internal/daemon/backend_test.go:93`); `api.Backend` (`internal/api/server.go`) and `fakeBackend` (`internal/api/api_test.go:21`).

### 2.5 Caches

All in memory, on `Backend`. Callers for the same key share one fetch (the pattern of the runner steps cache), detached from the request with `context.WithoutCancel`.

| Cache | Key | Holds | Lifetime and bound |
|---|---|---|---|
| Run details | repo (lower case), run ID, attempt number ("latest" is resolved to the number after `GetRun`) | `RunDetail` | 15 s while the attempt is not completed, 60 s after; at most 64 entries, expired entries dropped on insert, then the oldest |
| Workflow file | repo, `head_sha`, path | the file's bytes, or "not found" | bytes: no expiry; "not found": 10 minutes; 403, 429 and 5xx are never cached; 64 entries, least recently used evicted. The graph is built per request from the bytes and the attempt's jobs |
| Job annotations | job ID | annotations and counts of one finished job | no expiry; 500 entries, LRU |
| Parsed job log | job ID | `JobLog` | no expiry; at most 32 MiB in total, measured as the raw log size, LRU |

- Detail fetches use a 20 second deadline.
- **Log downloads** for annotations and the log route share one process-wide limit of 4 at a time. Each runs on its own detached context with a 60 second deadline and fills the annotation and parsed-log caches when it finishes.
- **The annotations route** starts downloads for every finished, non-skipped job of the attempt not already cached, waits at most 20 seconds, and answers with what it has. Jobs still downloading count in `Unread`. A job whose log answered 410 counts in `ExpiredLogs`, one above the parse cap in `TooLarge`, and one that failed otherwise in `Failed` (the error is logged); these three outcomes are cached for 10 minutes, after which a call downloads the log again.

### 2.6 API budget

With one tab on an active run: the run, its jobs and its artifacts every 15 seconds, about 720 calls an hour at most. Logs cost two requests per finished job (one counted against the rate limit), once. Nothing is fetched while no page is open.

## 3. The graph (`internal/wfgraph`)

### 3.1 Reading the workflow

`yaml.v3` decodes `jobs` into, per job ID in file order: `name`, `needs` (a string or a list), whether `strategy.matrix` is set, and whether `uses` is set. Everything else is ignored.

### 3.2 Matching jobs

Each YAML job gets a pattern from its `name`, or its ID when there is no name:

- each `${{ … }}` matches any text (non-greedy), so `Frontend tests (${{ matrix.shard }}/2)` matches `Frontend tests (1/2)` and also the unexpanded name GitHub reports for a skipped matrix job;
- with a matrix and no expression in the name, ` (…)` may follow;
- with `uses`, ` / …` may follow.

Each API job goes to the YAML job whose pattern matches it exactly as literal text first, otherwise to the matching pattern with the longest literal prefix, ties to the earlier YAML job. A YAML job with more than one API job, a matrix, or `uses` is a group node.

If any API job matches no YAML job, the file is not the one that ran (for example an event that runs the default branch's copy of the workflow), and §3.4 applies.

### 3.3 Layout

- A node's column is the length of its longest `needs` chain; jobs without `needs` are in column 0, so a workflow can start with several jobs (darkmem's CI starts with three).
- Nodes keep YAML order within a column. YAML jobs with no API job are dropped, and their `needs` pass through to what they need.
- The browser draws tiles absolutely positioned per column with an SVG layer of cubic curves from a node's right edge to each dependent's left edge, without a new dependency. Tile width 220 px, column gap 70 px. The drawing scrolls sideways inside its own region, which is focusable (`tabIndex={0}`) with `aria-label="Job graph"`.
- A group tile lists its jobs by their full names (status icon, name, duration), the first five and a "{n} more" toggle with `aria-expanded`.
- Every tile and group row is a link to its job page; a tile's accessible name adds "needs {names}". Lines are `aria-hidden`; the left rail is the full non-graphical list.

### 3.4 Timeline instead

`Graph` is omitted and `GraphNote` says why:

- 403 on the file: "Add Contents: read to the token to see which jobs depend on which."
- 404, or a path that is not under `.github/workflows/` (dynamic workflows such as Dependabot's): "This run has no workflow file to read."
- An API job that matches no YAML job (§3.2): "The workflow file at this commit does not match the run's jobs."
- YAML that does not decode: "ghr could not read {path}." A `needs` naming an unknown job or forming a cycle gets the same note; GitHub refuses such files before a run exists, so this is defensive only.

The timeline draws one row per job, sorted by start: name, a bar from its start to its completion against minutes since the run's start (axis ticks at whole minutes, mono, `aria-hidden`), and the duration as text (mono); a job that never started shows "skipped" or "queued" in place of a bar and duration. A 429 or 503 is not a reason: the route fails as a whole.

## 4. Logs (`internal/joblog`)

### 4.1 Fetching

Only for jobs whose status is `completed`. Logs above 4 MiB are not parsed: `JobLog` comes back with `TooLarge`, and the Raw log button still works up to 32 MiB. A 410 sets `Expired`.

### 4.2 Splitting into steps

Each line is `{RFC 3339 timestamp} {text}`; a leading byte-order mark is removed, and a line without a timestamp belongs with the line before it. One pointer walks the steps that have a start time, only forwards, starting at the first:

- a `##[group]Run ` line, other than the first line the current step received, moves the pointer to the next step whose window (start to completion, widened by one second each side) contains the line's time; when no later step's window contains it, the pointer stays;
- otherwise a line more than one second after the current step's completion moves it to the next step whose completion plus one second is not before the line's time; when there is none, the pointer stays.

Measured on a real 2,615-line darkmem job log, timestamps alone put "Create test databases" and "Lint" lines into "Test" because the three started in the same second; this rule gives each step exactly its own lines. Steps that complete in the same second can still trade boundary lines (a post-checkout step got 0 lines there); the raw log is exact.

### 4.3 Kinds and annotations

`##[group]`, `##[endgroup]`, `##[error]`, `##[warning]`, `##[notice]`, `##[command]` and `##[debug]` set `Kind` and are removed from `Text`. Other `##[…]` lines stay as text.

Annotations are the error, warning and notice lines of a job, with duplicates (same level, step and message) merged. They are sorted by level (errors, then warnings, then notices), then by job in the jobs API's order, then by step and line. At most 50 per job; the rest count in `Omitted`.

## 5. Testing

### Go

- `internal/joblog`: table tests on fixtures trimmed from real logs, with the darkmem steps that share a second, a byte-order mark, groups, every kind, ANSI kept in `Text`, a line without a timestamp, the pointer staying when nothing matches, the per-job cap and duplicate merging.
- `internal/wfgraph`: expression names, the matrix suffix, `uses` callers, several roots, a dropped YAML job passing its `needs` through, an unmatched API job giving the mismatch note, a cycle, an unknown `needs`, invalid YAML.
- `internal/github`: `ErrGone` for 410; `GetContent`'s path escaping; with an `httptest` API server and a second `httptest` blob server, `JobLog` sends no `Authorization` to the blob server, stops at `max`, and records rate headers from the 302; `ArtifactURL` returns `Location` without following it; neither touches the ETag cache.
- `internal/daemon` with `fakeGH`: the 404 for uncovered repositories and the resolved spelling, the attempt bounds, "latest" and its number sharing a cache entry, each cache's lifetime, bound and shared fetch, the workflow file cache not keeping 403, each `GraphNote`, `Unread` after the 20 second wait (with a shortened test deadline), `ExpiredLogs`, `TooLarge`, `Failed`, the process-wide download limit, the 409 for an unfinished job, the `GHR` marker, a 403 naming the permission.
- `internal/api`: each route's status codes, including 400 for non-integer parameters, 409, 410, 413, and the download host check.

### Frontend

Fixtures for a run detail (with a group node), its annotations and a job log, generated from Go models in `web/fixtures_test.go` like `actions.json`.

- Run page: summary facts, graph tiles linking to job pages, the timeline and each note, annotation counts and status lines, the link carrying `attempt`, `step` and `line`, artifacts with an expired row and the download flow (fetch, then navigation), the attempt picker updating `attempt`, the run and annotation polls stopping.
- Job page: the failed step open, the `step` and `line` parameters including a line above the 5,000-line cut, groups opening, ANSI spans and stripped controls, a `\r` line, search count and navigation opening only the current match's step, the too-large and expired states, a running job's note, a job missing from the attempt, the Raw log Blob save.
- The Actions page links to the run page and uses `RunStatusIcon`.
- Invalid route parameters show the not-found line without an API call.
- axe on both pages; the narrow layout with the stacked rail; the anti-slop test passes.

### Manual, before release

On the LXC with a watched and a configured repository: a failed run with annotations, a matrix workflow, a "re-run failed jobs" run with two attempts, an artifact download, the raw log save, a token with and without Contents: read, the Actions nav item staying active on both pages.

## 6. Delivery

One plan. The README's PAT section adds Contents: read as optional, for the graph.

## Out of scope

- Re-run and cancel.
- Job summaries (`$GITHUB_STEP_SUMMARY`): GitHub has no REST endpoint for them.
- Live logs for running jobs.
- File and line fields on annotations.
- Billable time and usage.
- 256-colour and true-colour ANSI.
- Drawing a reusable workflow's own jobs as a graph; they are listed inside the caller's tile.
- Repositories of owners other than `owner`.
