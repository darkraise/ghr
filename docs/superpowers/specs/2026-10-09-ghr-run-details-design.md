# ghr web UI: workflow run details

**Date:** 2026-10-09
**Status:** approved section by section in chat on 2026-10-09; mockup at https://claude.ai/artifact/FurnXQ2U5Z22fF2vqb9HFy (example data, approved as the layout).
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

## 1. Pages

### 1.1 Routes

- Run page: `/actions/$repo/runs/$id`, search `attempt?: number`.
- Job page: `/actions/$repo/runs/$id/jobs/$jobId`, search `attempt?: number`, `step?: number`, `line?: number`. `step` and `line` come from an annotation link; the page opens that step and scrolls to and marks that line.
- `$repo` is a bare repository name under `owner`, as on the Actions page. Both routes sit under the app layout and the Actions nav item stays active.
- On the Actions page a run's title links to its run page instead of `html_url`; the GitHub link moves to the run page header.

### 1.2 Shared frame

- Breadcrumb: Actions, repository, workflow, `#run_number` (mono).
- Header: status icon (the Actions page's `StatusIcon`), run title, then workflow, `#run_number`, repository and branch in a muted line. Right side: the attempt picker, shown only when the run has more than one attempt ("Attempt 2 (latest)", "Attempt 1"), and "Open on GitHub" (`html_url` of the shown attempt).
- Left rail, sticky on wide screens: "Summary", a "Jobs" label, one entry per job (status icon, name, duration in mono), then "Run details" with a "Workflow file" link to the file on GitHub at `head_sha`. Below 800 px the rail stacks above the content.
- While the shown attempt is not completed, the page refetches every 15 seconds and stops once it completes.

### 1.3 Run page (summary)

In order:

1. **Summary card:** "Triggered via {event}" with actor, short `head_sha` (mono, linked to the commit on GitHub) and pull request numbers (linked); Status (icon and word); Total duration (mono, from the attempt's start to its last job's completion, or to now while active); Artifacts (count, mono).
2. **Graph card** titled with the workflow file's base name and `on:` event, or "Jobs timeline" when §3.4 applies, with its one-line reason under it.
3. **Annotations card:** heading with error, warning and notice counts (each shown only when non-zero); one row per annotation with level icon, message (mono, wraps), "{job} / {step}" and a "Log line {n}" link to the job page. Footer note: "Taken from the job logs, so file and line come only from the message text." While `unread` is above zero (§2.2), a line reads "Reading logs for {n} jobs" and the card refetches every 5 seconds. With no annotations and nothing unread: "No errors, warnings or notices in the finished jobs' logs."
4. **Artifacts card:** table of name (mono), size (mono, human units), expires (mono date), and a "Download" button that navigates to the download route (§2.1). An expired artifact shows "Expired" instead of the button. Hidden when the run has no artifacts.

### 1.4 Job page

- Card header: status icon, job name, duration (mono). Below it: runner name (mono), labels (mono chips as plain spans; the kit `Badge` stays confined to `src/components/tag-field.tsx`), a "ghr" marker when ghr ran the job (§2.4), and start time (mono).
- Toolbar: a search box ("Search logs") and a "Raw log" link to the raw route (§2.1).
- Steps: one row per step (chevron, status icon, name, duration in mono) that expands to its log. The first failed step starts expanded; with `step` in the URL, that step does instead.
- Running or queued job: the step list without logs, using the existing step timeline, and the note "Logs appear when the job finishes."
- A log the daemon refused as too large (§4.1) shows "This log is too large to show here." with the Raw log link. An expired log (410) shows "GitHub no longer keeps this job's log."

### 1.5 Log viewer

- Lines render only inside expanded steps. Each line: per-step line number (muted, not selectable), then text in mono.
- `group` lines render as collapsed disclosure rows titled with the group name; their lines up to the matching `endgroup` render when opened. `endgroup` lines are not shown.
- `error` and `warning` lines get a tinted background and colour and the prefix "Error: " or "Warning: "; `notice` gets "Notice: " with no tint; `command` lines use the primary colour; `debug` lines are muted.
- ANSI SGR codes become spans: the 8 and bright 8 colours, 256-colour and true-colour foregrounds, and bold. Every other escape sequence is removed. Colours map to log palette tokens so both themes read.
- A step with more than 5,000 lines renders its last 5,000 and a "Show {n} earlier lines" button above them.
- Search: case-insensitive substring over every line of every step. Steps and groups holding a match open; matches are marked; the box shows "{i} of {n}"; Enter and Shift+Enter move to the next and previous match and scroll it into view.
- The log viewer's background is dark in both themes, like GitHub's, with its own tokens defined for light and dark.

### 1.6 Errors on the pages

All use the existing `ErrorLine` or `Alert`:

- 404 from ghr for an uncovered repository: "{repo} is not configured or watched in ghr." with a link to the Repositories page.
- 404 from GitHub for the run or job: "This run no longer exists on GitHub."
- 403 and 429 keep today's messages (the missing permission named; "API calls are paused until {time}"). Data already shown stays on screen.

## 2. Backend

### 2.1 Routes

Every route first checks that `{repo}` is configured or watched (case-insensitive, `coveredRepos`) and otherwise answers 404 "repo {repo} is not configured or watched". The three JSON routes answer through `respond`; the raw log and artifact routes write their own responses and use `respond` only for errors.

| Route | Returns |
|---|---|
| `GET /actions/{repo}/runs/{id}?attempt=N` | `model.RunDetail` |
| `GET /actions/{repo}/runs/{id}/annotations?attempt=N` | `model.RunAnnotations` |
| `GET /actions/{repo}/jobs/{jobId}/log` | `model.JobLog` |
| `GET /actions/{repo}/jobs/{jobId}/log/raw` | `text/plain; charset=utf-8`, streamed from GitHub, with `Content-Disposition: attachment; filename="{repo}-job-{jobId}.log"` |
| `GET /actions/{repo}/artifacts/{id}/zip` | 302 to the `Location` GitHub returns; 410 when GitHub says the artifact expired |

`attempt` absent means the latest attempt. An `attempt` below 1 or above the run's `run_attempt` answers 400.

### 2.2 Model

Go types in `internal/model`, JSON names in brackets.

- `RunDetail`: `Repo` (repo), `Watched` (watched), `ID` (id), `RunNumber` (run_number), `Attempt` (attempt), `Attempts` (attempts, the run's latest `run_attempt`), `Workflow` (workflow), `Title` (title), `Branch` (branch), `Event` (event), `Actor` (actor), `Status` (status), `Conclusion` (conclusion), `HeadSHA` (head_sha), `Path` (path), `PullRequests []int` (pull_requests), `StartedAt` (started_at), `UpdatedAt` (updated_at), `HTMLURL` (html_url), `Jobs []RunJob` (jobs), `Graph *RunGraph` (graph, omitted when §3.4 applies), `GraphNote string` (graph_note, omitempty), `Artifacts []RunArtifact` (artifacts), `FetchedAt` (fetched_at).
- `RunJob`: `ID`, `Name`, `Status`, `Conclusion`, `StartedAt *time.Time`, `CompletedAt *time.Time`, `RunnerName` (runner_name), `Labels []string`, `HTMLURL`, `GHR bool` (ghr), `Steps []Step` (the existing `model.Step`).
- `RunGraph`: `Nodes []GraphNode` (nodes). `GraphNode`: `Key` (key, the YAML job ID, or `unmatched:{jobId}`), `Name` (name, the YAML `name` with expressions kept, or the key), `Column int` (column), `Needs []string` (needs, keys), `JobIDs []int64` (job_ids, in the jobs API's order), `Group bool` (group: a matrix or a reusable-workflow caller).
- `RunArtifact`: `ID`, `Name`, `SizeBytes int64` (size_bytes), `Expired bool`, `ExpiresAt *time.Time` (expires_at).
- `RunAnnotations`: `Items []Annotation` (items), `Omitted map[int64]int` (omitted, job ID to count beyond the per-job cap), `Unread int` (unread, finished jobs whose logs were not read before the deadline), `ExpiredLogs int` (expired_logs).
- `Annotation`: `Level` (level: error, warning or notice), `JobID`, `JobName`, `Step int`, `StepName`, `Line int` (per-step line number), `Message`.
- `JobLog`: `JobID`, `TooLarge bool` (too_large), `Expired bool` (expired), `Steps []LogStep`. `LogStep`: `Number`, `Name`, `Lines []LogLine`. `LogLine`: `N int` (n), `Kind string` (kind: "", group, endgroup, error, warning, notice, command, debug), `Text` (text, with the `##[…]` prefix removed and ANSI kept).

### 2.3 GitHub client

`internal/github` gains:

- `Run` fields `HeadSHA` (head_sha), `Path` (path), `RunAttempt` (run_attempt), `PullRequests []struct{ Number int }` (pull_requests).
- `GetRun(ctx, repo, id)`, `GetRunAttempt(ctx, repo, id, attempt)`, `ListAttemptJobs(ctx, repo, id, attempt)` (`/attempts/{n}/jobs?per_page=100`, all pages), `ListArtifacts(ctx, repo, runID)` (all pages), `GetContent(ctx, repo, path, ref) ([]byte, error)` (decodes base64), `JobLog(ctx, repo, jobID, max int64) ([]byte, error)` (follows the redirect; returns `ErrTooLarge` past `max`), `JobLogStream(ctx, repo, jobID) (io.ReadCloser, error)`, `ArtifactURL(ctx, repo, id) (string, error)` (one request with redirects disabled; returns `Location`).
- Log and artifact redirects go to another host. Go's client drops `Authorization` on a cross-host redirect, and a test proves the token never reaches the second host.
- Every call goes through the existing request path, so rate limit tracking, suspension and the 403 permission naming apply.

### 2.4 Assembly

- Latest attempt: `GetRun` and `ListJobs`. Earlier attempt: `GetRunAttempt` and `ListAttemptJobs`. `Attempts` comes from `GetRun` in both cases.
- Artifacts: `ListArtifacts`, the same list for every attempt.
- Graph: §3, from `Path` at `HeadSHA`.
- A job is marked `GHR` when (repo, run ID, job name) appears in ghr's instances, pending jobs or history, the same sources `ghrRuns` reads. Two attempts with the same job name are both marked; history has no job ID to tell them apart.
- `StartedAt` follows `actionsRun`'s fallback (`run_started_at`, else `created_at`).

### 2.5 Caches

All in memory, on `Backend`, with the shared-fetch pattern of the runner steps cache: callers for the same key wait on one GitHub fetch, detached from the request with `context.WithoutCancel` and a 20 second deadline.

| Cache | Key | Lifetime |
|---|---|---|
| Run details | repo (lower case), run ID, attempt | 15 s while the attempt is not completed, 60 s after |
| Graph | repo, `head_sha`, path | no expiry; 64 entries, least recently used evicted |
| Job annotations | job ID | no expiry (finished jobs only); 500 entries, LRU |
| Parsed job log | job ID | no expiry (finished jobs only); 8 entries, LRU |

The annotations route reads finished, non-skipped jobs four at a time within one 20 second deadline. Jobs not read by then count in `Unread`; their fetches keep running and fill the cache for the next call.

### 2.6 API budget

With one tab on an active run: the run, its jobs and its artifacts every 15 seconds, about 720 calls an hour at most. Logs cost one call per finished job, once. Nothing is fetched while no page is open.

## 3. The graph (`internal/wfgraph`)

### 3.1 Reading the workflow

`yaml.v3` decodes `jobs` into, per job ID in file order: `name`, `needs` (a string or a list), whether `strategy.matrix` is set, and whether `uses` is set. Everything else is ignored.

### 3.2 Matching jobs

Each YAML job gets a pattern from its `name`, or its ID when there is no name:

- each `${{ … }}` matches any text (non-greedy), so `Frontend tests (${{ matrix.shard }}/2)` matches `Frontend tests (1/2)` and also the unexpanded name GitHub reports for a skipped matrix job;
- with a matrix and no expression in the name, ` (…)` may follow;
- with `uses`, ` / …` may follow.

Each API job goes to the YAML job whose pattern matches it exactly as literal text first, otherwise to the matching pattern with the longest literal prefix, ties to the earlier YAML job. A YAML job with more than one API job, a matrix, or `uses` is a group node. An API job that matches nothing becomes its own node `unmatched:{id}` in a final column, with no edges.

### 3.3 Layout

- A node's column is the length of its longest `needs` chain; jobs without `needs` are in column 0, so a workflow can start with several jobs (darkmem's CI starts with three).
- Nodes keep YAML order within a column. YAML jobs with no API job are dropped, and their `needs` pass through to what they need.
- The browser draws cards absolutely positioned per column with an SVG layer of cubic curves from a node's right edge to each dependent's left edge, without a new dependency. Card width 220 px, column gap 70 px; the card scrolls sideways inside its own container.
- A group card lists its jobs (status icon, name without the group prefix, duration), the first five and a "{n} more" toggle.
- Every card and group row is a link to its job page; a card's accessible name adds "needs {names}". Lines are `aria-hidden`; the left rail is the full non-graphical list.

### 3.4 Timeline instead

`Graph` is omitted and `GraphNote` says why:

- 403 on the file: "Add Contents: read to the token to see which jobs depend on which."
- 404, or a path that is not under `.github/workflows/` (dynamic workflows such as Dependabot's): "This run has no workflow file to read."
- YAML that does not decode, a `needs` naming an unknown job, or a cycle: "ghr could not read {path}."

The timeline draws one bar per job from its start to its completion against minutes since the run's start (axis ticks at whole minutes, mono), sorted by start; a job that never started shows "skipped" or "queued" in place of a bar. A 429 or 503 is not a reason: the route fails as a whole.

## 4. Logs (`internal/joblog`)

### 4.1 Fetching

Only for jobs whose status is `completed`. Logs above 16 MiB are not parsed: `JobLog` comes back with `TooLarge`. A 410 sets `Expired`; for annotations it counts in `ExpiredLogs`.

### 4.2 Splitting into steps

Each line is `{RFC 3339 timestamp} {text}`; a leading byte-order mark is removed, and a line without a timestamp belongs with the line before it. One pointer walks the steps that have a start time, only forwards, starting at the first:

- a `##[group]Run ` line, other than the first line the current step received, moves the pointer to the next step whose window (start to completion, widened by one second each side) contains the line's time;
- otherwise a line more than one second after the current step's completion moves it to the next step whose completion plus one second is not before the line's time.

Measured on a real 2,615-line darkmem job log, timestamps alone put "Create test databases" and "Lint" lines into "Test" because the three started in the same second; this rule gives each step exactly its own lines. Steps that complete in the same second can still trade boundary lines (a post-checkout step got 0 lines there); the raw log is exact.

### 4.3 Kinds and annotations

`##[group]`, `##[endgroup]`, `##[error]`, `##[warning]`, `##[notice]`, `##[command]` and `##[debug]` set `Kind` and are removed from `Text`. Other `##[…]` lines stay as text.

Annotations are the error, warning and notice lines of a job, with duplicates (same level, step and message) merged. They are sorted by level (errors, then warnings, then notices), then by job in the jobs API's order, then by step and line. At most 50 per job; the rest count in `Omitted`.

## 5. Testing

### Go

- `internal/joblog`: table tests on fixtures trimmed from real logs, with the darkmem steps that share a second, a byte-order mark, groups, every kind, ANSI kept in `Text`, a line without a timestamp, the per-job cap and duplicate merging.
- `internal/wfgraph`: expression names, the matrix suffix, `uses` callers, unmatched jobs, several roots, a dropped YAML job passing its `needs` through, a cycle, an unknown `needs`, invalid YAML.
- `internal/github`: an `httptest` pair proving the log and artifact redirects do not forward `Authorization`, that `ArtifactURL` does not follow, and that `JobLog` stops at `max`.
- `internal/daemon` with the existing fake GitHub client: the 404 for uncovered repositories, the attempt bounds, the cache lifetimes and shared fetch, each `GraphNote`, `Unread` after the deadline, `ExpiredLogs`, the `GHR` marker.
- `internal/api`: each route's status codes and the raw log's headers.

### Frontend

Fixtures for a run detail, its annotations and a job log, in the style of `web/src/api/fixtures/actions.json`.

- Run page: summary facts, graph cards linking to job pages, the timeline and its note, annotation counts and the link to step and line, artifacts with an expired row, the attempt picker updating `attempt`, polling stopping at completion.
- Job page: the failed step open, the `step` and `line` search parameters, groups opening, ANSI spans, search count and navigation, the 5,000-line cut, the too-large and expired states, a running job's note.
- The Actions page links to the run page.
- axe on both pages; the narrow layout with the stacked rail; the anti-slop test passes.

### Manual, before release

On the LXC with a watched and a configured repository: a failed run with annotations, a matrix workflow, a re-run with two attempts, an artifact download, a token with and without Contents: read.

## 6. Delivery

One plan. The README's PAT section adds Contents: read as optional, for the graph.

## Out of scope

- Re-run and cancel.
- Job summaries (`$GITHUB_STEP_SUMMARY`): GitHub has no REST endpoint for them.
- Live logs for running jobs.
- File and line fields on annotations.
- Billable time and usage.
- Drawing a reusable workflow's own jobs as a graph; they are listed inside the caller's card.
- Repositories of owners other than `owner`.
