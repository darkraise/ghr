# ghr Run Details Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GitHub's workflow run page to ghr: a run summary, the job graph or a jobs timeline, annotations, artifacts, attempts, and a job page with step logs and search, for every configured or watched repository.

**Architecture:** The GitHub client gains run, attempt, job, artifact and content reads plus a redirect path for log and artifact downloads; two new pure packages parse job logs (`internal/joblog`) and workflow files (`internal/wfgraph`); the daemon assembles `RunDetail`, `RunAnnotations` and `JobLog` behind shared, bounded in-memory caches and serves five new API routes. The browser adds two routes under `/actions/$repo/runs/$id`, a shared run frame, four summary sections, and a log viewer built on an ANSI tokenizer and a log view model.

**Tech Stack:** Go 1.26 standard library (`net/http`, `sync`, `context`, `encoding/base64`), `gopkg.in/yaml.v3` (already in `go.mod`), React 19 + TypeScript, TanStack Router and Query, darkraise-ui, lucide-react, Vitest + Testing Library, axe-core.

**Spec:** docs/superpowers/specs/2026-10-09-ghr-run-details-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 21 of 27 tasks total 4 or less; Tasks 2, 6, 7, 8, 9 and 10 (total 5 or more) are delegated.

**Plan review:** 2026-10-09 — dr-superpowers:judge-opus — executability 18 / coherence 18 / coverage 18 / assumptions 16 (round 1)

## Global Constraints

- Execution environment: the owner's Linux machine (bash, GNU coreutils `timeout`, Go 1.26, Node 24 from `web/.nvmrc`). Task 1 Step 0 checks it; on any other shell, translate `timeout N cmd` and `VAR=1 cmd` to that shell's equivalents.
- Work on the branch `feat/run-details`, never on `master`. Do not push `master`: CI turns every push to `master` into a release. Do not commit `.superpowers/`.
- Go checks, from the repository root: `gofmt -l internal cmd web/*.go` prints nothing; `go vet ./internal/... ./cmd/... ./web/` passes. Never run `go vet ./...` or `go test ./...`: they walk Go code under `web/node_modules`.
- Web checks, from `web/`: `npm test`, `npm run typecheck`, `npm run lint` (eslint with `--max-warnings 0`). Web tests run with `TZ=UTC` (`web/vite.config.ts:23`), and the app's clock follows the status fixture's `now`, 2026-10-03T14:05:00Z.
- Give every test command an explicit timeout (`timeout 300 …`).
- Spec 1 §1.7 and spec 2 §1.2 copy rules bind every page: no "·" separator chains, no dash characters in empty cells, no kit `Badge` import outside `src/components/tag-field.tsx` (`web/src/anti-slop.test.ts` enforces it), mono for IDs, numbers, times, SHAs and branches, sentence-case toasts, icon buttons carry an `aria-label` plus a tooltip. The run page's four blocks are `Section`s; the graph's job tiles are the only bordered elements inside a section.
- Log text renders as React elements from a tokenizer: no HTML strings and no `dangerouslySetInnerHTML` anywhere in this plan.
- The browser never navigates to `/api/`: the web handler refuses `/api/` requests without `X-GHR: 1` (`internal/webui/handler.go:68`). Downloads go through `request` (which sends `X-GHR`) and then a navigation to GitHub's URL or a Blob object URL.
- GitHub links are built from `config.owner`: `https://github.com/{owner}/{repo}/…`.
- Repository names are bare names under `owner`, compared case-insensitively; the daemon resolves them to their configured spelling before any GitHub URL or cache key.
- English only. Comments only where the why is not obvious.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.

## Contracts

**Go — GitHub client (`internal/github`)**

- `github.Run` gains (Task 1): `HeadSHA string json:"head_sha"`, `Path string json:"path"`, `RunAttempt int json:"run_attempt"`, `PullRequests []PullRequest json:"pull_requests"`. `type PullRequest struct { Number int json:"number" }`.
- `github.ErrGone` (Task 1): a new `ErrKind` declared after `ErrServer`; `classify` gives it to a 410.
- `type Artifact struct` (Task 1): `ID int64 id`, `Name string name`, `SizeInBytes int64 size_in_bytes`, `Expired bool expired`, `ExpiresAt *time.Time expires_at`.
- Methods on `*Client` (Task 1), each through `c.do` or `c.getAll`:
  - `GetRun(ctx context.Context, repo string, id int64) (*Run, error)` — `GET /repos/{owner}/{repo}/actions/runs/{id}`.
  - `GetRunAttempt(ctx context.Context, repo string, id int64, attempt int) (*Run, error)` — `…/actions/runs/{id}/attempts/{attempt}`.
  - `ListAttemptJobs(ctx context.Context, repo string, id int64, attempt int) ([]Job, error)` — `…/actions/runs/{id}/attempts/{attempt}/jobs?per_page=100`, every page.
  - `GetJob(ctx context.Context, repo string, jobID int64) (*Job, error)` — `…/actions/jobs/{jobID}`.
  - `ListArtifacts(ctx context.Context, repo string, runID int64) ([]Artifact, error)` — `…/actions/runs/{runID}/artifacts?per_page=100`, every page.
  - `GetContent(ctx context.Context, repo, path, ref string) ([]byte, error)` — `…/contents/{path}?ref={ref}` with `path` escaped segment by segment (`/` kept), body `{"content","encoding"}`, base64 decoded after removing newlines; any other encoding is an error.
- Task 2: `Client.Blob *http.Client`, set in `New` (dial, TLS handshake and response-header timeouts of 10 s, no whole-request timeout); `var ErrTooLarge = errors.New("github: download is larger than the limit")`; `func (c *Client) JobLog(ctx context.Context, repo string, jobID int64, max int64) ([]byte, error)` (redirect from `…/actions/jobs/{jobID}/logs`, then a plain GET on `Blob` with no `Authorization`; `ErrTooLarge` past `max`); `func (c *Client) ArtifactURL(ctx context.Context, repo string, id int64) (string, error)` (the `Location` of `…/actions/artifacts/{id}/zip`, not followed). Neither touches the ETag cache.

**Go — model (`internal/model/model.go`, Task 3)**

Field, type, json. Every slice and map in these types is non-nil when the daemon emits it.

- `RunDetail`: `Repo string repo`, `Watched bool watched`, `ID int64 id`, `RunNumber int64 run_number`, `Attempt int attempt`, `Attempts int attempts`, `Workflow string workflow`, `Title string title`, `Branch string branch`, `Event string event`, `Actor string actor`, `Status string status`, `Conclusion string conclusion`, `HeadSHA string head_sha`, `Path string path`, `PullRequests []int pull_requests`, `StartedAt time.Time started_at`, `UpdatedAt time.Time updated_at`, `HTMLURL string html_url`, `Jobs []RunJob jobs`, `Graph *RunGraph graph,omitempty`, `GraphNote string graph_note,omitempty`, `Artifacts []RunArtifact artifacts`, `FetchedAt time.Time fetched_at`.
- `RunJob`: `ID int64 id`, `Name string name`, `Status string status`, `Conclusion string conclusion`, `StartedAt *time.Time started_at,omitempty`, `CompletedAt *time.Time completed_at,omitempty`, `RunnerName string runner_name`, `Labels []string labels`, `HTMLURL string html_url`, `GHR bool ghr`, `Steps []Step steps` (the existing `model.Step`).
- `RunGraph`: `Nodes []GraphNode nodes`. `GraphNode`: `Key string key`, `Name string name`, `Column int column`, `Needs []string needs`, `JobIDs []int64 job_ids`, `Group bool group`.
- `RunArtifact`: `ID int64 id`, `Name string name`, `SizeBytes int64 size_bytes`, `Expired bool expired`, `ExpiresAt *time.Time expires_at,omitempty`.
- `RunAnnotations`: `Items []Annotation items`, `Omitted map[int64]int omitted`, `Unread int unread`, `ExpiredLogs int expired_logs`, `TooLarge int too_large`, `Failed int failed`.
- `Annotation`: `Level string level` ("error", "warning" or "notice"), `JobID int64 job_id`, `JobName string job_name`, `Step int step`, `StepName string step_name`, `Line int line`, `Message string message`.
- `JobLog`: `JobID int64 job_id`, `TooLarge bool too_large`, `Expired bool expired`, `Steps []LogStep steps`. `LogStep`: `Number int number`, `Name string name`, `Lines []LogLine lines`. `LogLine`: `N int n`, `Kind string kind` ("", "group", "endgroup", "error", "warning", "notice", "command", "debug"; never omitted), `Text string text` (the `##[…]` prefix removed, ANSI kept).
- `RawLog`: `Text string text`. `DownloadURL`: `URL string url`.

**Go — log parsing (`internal/joblog`, Task 3)**

- `func Parse(raw []byte, steps []model.Step) []model.LogStep` — one `LogStep` per input step in input order, `Lines` non-nil, `N` counting from 1 within each step (spec §4.2, §4.3).
- `const PerJob = 50`.
- `func LevelRank(level string) int` — error 0, warning 1, notice 2, anything else 3.
- `func Annotations(jobID int64, jobName string, steps []model.LogStep) (items []model.Annotation, omitted int)` — one job's error, warning and notice lines, duplicates (same level, step and message) merged, sorted by `LevelRank`, then step, then line; the first `PerJob` kept, the rest counted in `omitted`. `items` is non-nil.

**Go — workflow graph (`internal/wfgraph`, Task 4)**

- `type Job struct { Key, Name string; Needs []string; Matrix, Uses bool }` (`Name` is the YAML `name`, "" when absent); `type Workflow struct { Jobs []Job }` in file order.
- `var ErrInvalid = errors.New("wfgraph: invalid workflow")`; `var ErrMismatch = errors.New("wfgraph: a job matches no workflow job")`.
- `func Parse(data []byte) (*Workflow, error)` — errors wrap `ErrInvalid`: YAML that does not decode, no `jobs` mapping, a `needs` naming an unknown job, a cycle. Aliases resolve.
- `type APIJob struct { ID int64; Name string }`.
- `func Build(w *Workflow, jobs []APIJob) ([]model.GraphNode, error)` — spec §3.2 and §3.3; `ErrMismatch` when any API job matches no YAML job. Nodes come sorted by column, then YAML order; `Needs` and `JobIDs` are non-nil.

**Go — daemon (`internal/daemon`)**

- `GitHub` interface (Task 5) gains, with the client's exact signatures: `GetRun`, `GetRunAttempt`, `ListAttemptJobs`, `GetJob`, `ListArtifacts`, `GetContent`, `JobLog`, `ArtifactURL`.
- Public methods on `*Backend`, each calling `b.githubErr()` first and resolving `repo` through `coveredName` (Task 7):
  - `RunDetail(ctx context.Context, repo string, id int64, attempt int) (model.RunDetail, error)` (Task 7; `attempt` 0 means the latest).
  - `JobLog(ctx context.Context, repo string, jobID int64) (model.JobLog, error)` and `RawJobLog(ctx context.Context, repo string, jobID int64) (model.RawLog, error)` (Task 8).
  - `RunAnnotations(ctx context.Context, repo string, id int64, attempt int) (model.RunAnnotations, error)` (Task 9).
  - `ArtifactDownload(ctx context.Context, repo string, id int64) (model.DownloadURL, error)` (Task 10).
- Errors the routes answer with (`api.Error` status and message): uncovered repository 404 `repo {repo} is not configured or watched`; an attempt above the run's `run_attempt` 400 `run {id} has no attempt {n}`; an unfinished job on either log route 409 `job has not finished`; an expired artifact 410 `artifact has expired`; an expired log on the raw route 410 `GitHub no longer keeps this job's log`; a raw log over 32 MiB 413 `log is larger than 32 MiB; open it on GitHub`; a download address that is not `https` on a host ending in `.githubusercontent.com` or `.blob.core.windows.net` 502 `GitHub returned an unexpected download address`.

**Go — API (`internal/api`, Task 11)**

- `api.Backend` gains the five methods above with the same signatures.
- Routes, each answering through `respond`: `GET /actions/{repo}/runs/{id}` (query `attempt`), `GET /actions/{repo}/runs/{id}/annotations` (query `attempt`), `GET /actions/{repo}/jobs/{jobId}/log`, `GET /actions/{repo}/jobs/{jobId}/log/raw`, `GET /actions/{repo}/artifacts/{id}/download`. A path or query integer that is not positive answers 400 `{name} must be a positive integer`, with names `run id`, `job id`, `artifact id` and `attempt`; an absent `attempt` passes 0.
- `FromGitHub` maps `github.ErrGone` to 410.
- The browser reaches each route under `/api`, for example `GET /api/actions/darkmem/runs/102?attempt=1`.

**Fixtures (Task 12, generated from Go models in `web/fixtures_test.go`)**

Times are on 2026-10-03, UTC. Every job's `html_url` is `https://github.com/darkraise/darkmem/actions/runs/102/job/{id}`.

- `web/src/api/fixtures/run-detail.json`: repo `darkmem`, watched false, id 102, run_number 42, attempt 2, attempts 2, workflow `ci`, title `Fix the cache key`, branch `fix/cache-key`, event `pull_request`, actor `octo-dev`, status `completed`, conclusion `failure`, head_sha `a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9`, path `.github/workflows/ci.yml`, pull_requests `[87]`, started_at 13:55:00, updated_at 13:59:10, html_url `https://github.com/darkraise/darkmem/actions/runs/102`, fetched_at 14:05:00. Total duration 4m10s.
  - Jobs, in this order (all `completed`):
    - 1001 `Pick runner`, success, 13:55:00 to 13:55:20 (0m20s), runner `ghr-darkmem-a1b2c3`, labels `self-hosted`, `homelab`, ghr true. Steps: 1 `Set up job` 13:55:00-13:55:05, 2 `Pick` 13:55:05-13:55:19, 3 `Complete job` 13:55:19-13:55:20, all success.
    - 1002 `Backend tests`, failure, 13:55:25 to 13:57:40 (2m15s), runner `ghr-darkmem-d4e5f6`, labels `self-hosted`, `homelab`, ghr true. Steps: 1 `Set up job` 13:55:25-13:55:30 success, 2 `Install` 13:55:30-13:56:10 success, 3 `Lint (ruff)` 13:56:10-13:57:35 failure, 4 `Test` skipped with no times, 5 `Complete job` 13:57:35-13:57:40 success.
    - 1003 `Frontend tests (1/2)`, success, 13:55:25 to 13:58:00 (2m35s), runner `GitHub Actions 2`, labels `ubuntu-latest`, ghr false. Steps: 1 `Set up job` 13:55:25-13:55:30, 2 `Test` 13:55:30-13:57:55, 3 `Complete job` 13:57:55-13:58:00, all success.
    - 1004 `Frontend tests (2/2)`, success, 13:55:26 to 13:58:30 (3m04s), runner `GitHub Actions 3`, labels `ubuntu-latest`, ghr false. Steps: 1 `Set up job` 13:55:26-13:55:31, 2 `Test` 13:55:31-13:58:25, 3 `Complete job` 13:58:25-13:58:30, all success.
    - 1005 `E2E (api)`, success, 13:55:30 to 13:59:10 (3m40s), runner `GitHub Actions 4`, labels `ubuntu-latest`, ghr false. Steps: 1 `Set up job` 13:55:30-13:55:35, 2 `Test` 13:55:35-13:59:05, 3 `Complete job` 13:59:05-13:59:10, all success.
  - Graph nodes, in this order: `pick-runner` (name `Pick runner`, column 0, needs `[]`, job_ids `[1001]`, group false); `e2e` (name `E2E (${{ matrix.suite }})`, column 0, needs `[]`, job_ids `[1005]`, group true); `backend` (name `Backend tests`, column 1, needs `["pick-runner"]`, job_ids `[1002]`, group false); `frontend-tests` (name `Frontend tests (${{ matrix.shard }}/2)`, column 1, needs `["pick-runner"]`, job_ids `[1003, 1004]`, group true). No `graph_note`.
  - Artifacts: 11 `frontend-dist`, size_bytes 2400000 (`humanBytes` gives `2.4 MB`), expired false, expires_at 2026-10-17T13:59:00Z; 12 `coverage-backend`, size_bytes 51200, expired true, expires_at 2026-10-02T13:59:00Z.
- `web/src/api/fixtures/run-annotations.json`: items in this order: error, job 1002 `Backend tests`, step 3 `Lint (ruff)`, line 5, `src/cache.py:12:5: F401 'os' imported but unused`; error, job 1002, step 3 `Lint (ruff)`, line 7, `Process completed with exit code 1.`; warning, job 1003 `Frontend tests (1/2)`, step 2 `Test`, line 2, `Node.js 16 actions are deprecated.`; notice, job 1005 `E2E (api)`, step 2 `Test`, line 3, `Retried 1 flaky test`. omitted `{"1002": 3}`; unread, expired_logs, too_large and failed 0.
- `web/src/api/fixtures/job-log.json`: job_id 1002, too_large false, expired false, steps (number, name, then lines as n kind text, "" for the plain kind):
  - 1 `Set up job`: 1 "" `Current runner version: '2.338.0'`; 2 group `Runner Image`; 3 "" `Image: ubuntu-24.04`; 4 endgroup "".
  - 2 `Install`: 1 command `pip install -r requirements.txt`; 2 "" `Successfully installed ruff-0.6.9`.
  - 3 `Lint (ruff)`: 1 group `Run ruff check .`; 2 "" `ruff check .`; 3 endgroup ""; 4 "" `\u001b[31mFound 1 error.\u001b[0m`; 5 error `src/cache.py:12:5: F401 'os' imported but unused`; 6 debug `Finishing: Lint (ruff)`; 7 error `Process completed with exit code 1.`.
  - 4 `Test`: no lines.
  - 5 `Complete job`: 1 "" `Cleaning up orphan processes`.

**TypeScript — types and test fixtures (Task 13)**

- `web/src/api/types.ts`: `interface RunJob { id: number; name: string; status: string; conclusion: string; started_at?: string; completed_at?: string; runner_name: string; labels: string[]; html_url: string; ghr: boolean; steps: Step[] }`; `interface GraphNode { key: string; name: string; column: number; needs: string[]; job_ids: number[]; group: boolean }`; `interface RunGraph { nodes: GraphNode[] }`; `interface RunArtifact { id: number; name: string; size_bytes: number; expired: boolean; expires_at?: string }`; `interface RunDetail { repo: string; watched: boolean; id: number; run_number: number; attempt: number; attempts: number; workflow: string; title: string; branch: string; event: string; actor: string; status: string; conclusion: string; head_sha: string; path: string; pull_requests: number[]; started_at: string; updated_at: string; html_url: string; jobs: RunJob[]; graph?: RunGraph; graph_note?: string; artifacts: RunArtifact[]; fetched_at: string }`; `interface Annotation { level: string; job_id: number; job_name: string; step: number; step_name: string; line: number; message: string }`; `interface RunAnnotations { items: Annotation[]; omitted: Record<string, number>; unread: number; expired_logs: number; too_large: number; failed: number }`; `interface LogLine { n: number; kind: string; text: string }`; `interface LogStep { number: number; name: string; lines: LogLine[] }`; `interface JobLog { job_id: number; too_large: boolean; expired: boolean; steps: LogStep[] }`; `interface RawLog { text: string }`; `interface DownloadURL { url: string }`.
- `web/src/test/fixtures.ts`: `fixtures.runDetail: RunDetail`, `fixtures.runAnnotations: RunAnnotations`, `fixtures.jobLog: JobLog`; `export function runRoutes(over: Record<string, unknown> = {}): Record<string, unknown>` returns `authedRoutes` plus `"GET /api/actions/darkmem/runs/102": fixtures.runDetail`, `"GET /api/actions/darkmem/runs/102/annotations": fixtures.runAnnotations`, `"GET /api/actions/darkmem/jobs/1002/log": fixtures.jobLog`, with `over` applied last.

**TypeScript — API client and hooks (Task 14)**

- `web/src/api/client.ts` `api` gains: `runDetail(repo: string, id: number, attempt: number | undefined, signal?: AbortSignal): Promise<RunDetail>` (`GET /api/actions/{repo}/runs/{id}`, `?attempt=N` only when `attempt` is set); `runAnnotations(repo: string, id: number, attempt: number | undefined, signal?: AbortSignal): Promise<RunAnnotations>`; `jobLog(repo: string, jobId: number, signal?: AbortSignal): Promise<JobLog>`; `rawJobLog(repo: string, jobId: number): Promise<RawLog>`; `artifactDownload(repo: string, id: number): Promise<DownloadURL>`. `repo` goes through `seg`.
- `web/src/api/hooks.ts`: `keys.runDetail(repo: string, id: number, attempt: number | undefined)` = `["run-detail", repo.toLowerCase(), id, attempt ?? 0] as const`; `keys.runAnnotations(repo, id, attempt)` = `["run-annotations", repo.toLowerCase(), id, attempt ?? 0] as const`; `keys.jobLog(repo: string, jobId: number)` = `["job-log", repo.toLowerCase(), jobId] as const`.
- `useRunDetail(repo: string, id: number, attempt: number | undefined, enabled: boolean)` — refetches every `POLL_ACTIONS` while `data.status !== "completed"`, otherwise not.
- `useRunAnnotations(repo: string, id: number, attempt: number | undefined, active: boolean, enabled: boolean)` — refetches every `POLL_SLOW` while `data.unread > 0`, else every `POLL_ACTIONS` while `active`, else not.
- `useJobLog(repo: string, jobId: number, enabled: boolean)` — no refetch interval.

**TypeScript — run helpers and status icon (Task 15)**

- `web/src/lib/run-details.ts`:
  - `interface RunSearch { attempt?: number }`; `interface JobSearch { attempt?: number; step?: number; line?: number }`.
  - `positiveInt(v: unknown): number | undefined` — a number or a decimal string holding a positive safe integer, otherwise `undefined`.
  - `parseRunSearch(search: Record<string, unknown>): RunSearch`; `parseJobSearch(search: Record<string, unknown>): JobSearch` — keep only the keys `positiveInt` accepts.
  - `isRunActive(status: string): boolean` — `status !== "completed"`.
  - `statusText(status: string, conclusion: string): string` — `resultWord(conclusion)` when completed, `"Running"` for `in_progress`, otherwise `"Queued"`.
  - `jobMs(job: RunJob, now: number): number | undefined` — `completed_at` (or `now` when absent) minus `started_at`; `undefined` without `started_at`.
  - `runTotalMs(run: RunDetail, now: number): number` — from `started_at` to the latest job `completed_at`, or to `now` while `isRunActive(run.status)`.
  - `runDescription(run: RunDetail): string` — `` `${workflow} #${run_number} in ${repo} on ${branch}.` ``, or without ` on {branch}` when the branch is empty. The fixture gives `ci #42 in darkmem on fix/cache-key.`
  - `githubLinks(owner: string, run: RunDetail): { commit: string; workflowFile: string | undefined; pull: (n: number) => string }` — `https://github.com/{owner}/{repo}/commit/{head_sha}`, `…/blob/{head_sha}/{path}` only when `path` starts with `.github/workflows/` (otherwise `undefined`), `…/pull/{n}`.
  - `attemptLabel(n: number, attempts: number): string` — `"Attempt 2 (latest)"` when `n === attempts`, else `"Attempt 1"`.
  - `interface AnnotationCounts { error: number; warning: number; notice: number }`; `annotationCounts(items: Annotation[]): AnnotationCounts`.
  - `firstFailedStep(steps: Step[]): number | undefined` — the number of the first step whose conclusion is `failure`.
  - `rawLogFileName(repo: string, jobId: number): string` — `` `${repo}-job-${jobId}.log` ``.
- `web/src/components/run-status-icon.tsx`: `RunStatusIcon({ status, conclusion }: { status: string; conclusion: string })` — `ResultIcon` when completed (img names `Succeeded`, `Failed`, `Cancelled`, `Skipped`, `Unknown`), a small `Spinner` labelled `Running` (screen-reader text) for `in_progress`, otherwise a lucide `Clock` with `role="img"` and `aria-label="Queued"`.

**TypeScript — run frame (Task 16, `web/src/components/run-frame.tsx`)**

- `interface JobLinkOptions { step?: number; line?: number; className?: string; label?: string; current?: boolean }`; `type JobLinkFn = (jobId: number, children: ReactNode, opts?: JobLinkOptions) => ReactNode`; `type SummaryLinkFn = (children: ReactNode, opts: { className?: string; current?: boolean }) => ReactNode`. `label` becomes the link's `aria-label`; `current` lets a test stub set `aria-current="page"` (TanStack `Link` sets it itself).
- `RunFrame({ run, owner, now, current, job, jobLink, summaryLink, onAttempt, children }: { run: RunDetail; owner: string; now: number; current: "summary" | number; job?: RunJob; jobLink: JobLinkFn; summaryLink: SummaryLinkFn; onAttempt: (attempt: number) => void; children: ReactNode })`:
  - `PageHeader` with `title={run.title}`, `description={runDescription(run)}`, breadcrumbs `Actions` (`/actions`), `{repo}` (`/actions?repo={repo}`), `{workflow}` (no href), `#{run_number}` (no href on the run page; the run page's path with `?attempt=` when `job` is set), and `{job.name}` when `job` is set.
  - Header actions: a kit `Select` with `aria-label="Attempt"` and options `attemptLabel(n, run.attempts)` for `n` from `run.attempts` down to 1, shown only when `run.attempts > 1`, calling `onAttempt(n)`; then an anchor `Open on GitHub` to `run.html_url` (`target="_blank"`, `rel="noreferrer"`).
  - Rail: `<nav aria-label="Run">` holding `summaryLink("Summary", …)`, the label `Jobs`, one `jobLink(job.id, …)` per job (`RunStatusIcon`, name, `dur(jobMs)` in mono when defined) with `current` set for `current === job.id`, the label `Run details`, and an anchor `Workflow file` to `githubLinks(owner, run).workflowFile` when defined. Sticky on wide screens; below 800 px (`min-[800px]:` variants) the rail stacks above the content.
  - `children` render in the content column.

**TypeScript — log viewer (Tasks 23 to 25)**

- `web/src/components/log-viewer.tsx` (Task 25): `LogViewer({ log, steps, step, line, toolbar }: { log: JobLog; steps: Step[]; step?: number; line?: number; toolbar?: ReactNode })` — renders a search box (`aria-label="Search logs"`, placeholder `Search logs`), the text `{i} of {n}` while the query is non-empty, then `toolbar`; then one row per step of `log.steps` (a button with `aria-expanded`, the step's status icon from `steps`, its name and duration in mono) that expands to its lines. The step `step` starts expanded, otherwise the first failed step; with `line`, that line is marked with `data-target="true"` and scrolled into view. The caller renders the too-large and expired states; `LogViewer` is only given a log with neither flag.
- Task 23 adds the log palette tokens (`--log-*`, defined in both `:root[data-mode="dark"]` and `:root[data-mode="light"]`, dark in both) and the `ghr-log-*` classes to `web/src/styles/ghr-theme.css`.

**Names shared between tasks of one layer**

- `internal/github` (Task 2): unexported `(*Client).newRequest(ctx context.Context, method, u string, body io.Reader) (*http.Request, uint64, error)`, `(*Client).suspendedErr() error`, `(*Client).suspend(gen uint64, e *APIError)` in `client.go`; `newBlobClient()`, `request`, `redirectURL` in `download.go`.
- `wfgraph.Build` returns each node's `Needs` sorted by key without duplicates, and returns `ErrInvalid` for an unknown `needs` or a cycle too. `joblog.Parse` strips a byte-order mark and one trailing `\r` per line; with no timed steps every line goes to the first step. `joblog.Annotations` merges duplicates only within one step.
- `internal/daemon/runs_fake_test.go` (Task 5): `type fakeRuns struct`, embedded as the first field of `fakeGH`, with fields `rmu sync.Mutex`; `runByID map[int64]github.Run`, `runErr error`, `runWait chan struct{}`; `attemptRuns map[string]github.Run`, `attemptJobs map[string][]github.Job`; `jobByID map[int64]github.Job`; `artifacts map[int64][]github.Artifact`; `content map[string][]byte`, `contentErr error`; `logs map[int64][]byte`, `logErr map[int64]error`, `logWait chan struct{}`; `artURL string`, `artErr error`; `calls []string` (each `"<Method> <repo> <id>"`), `logInFlight int`, `logPeak int`. Helpers `attemptKey(id int64, attempt int) string` (`"id/n"`), `contentKey(repo, path, ref string) string`, `ghNotFound() error`, `(*fakeRuns).count(prefix string) int`. A missing entry answers 404; the fake `JobLog` returns `github.ErrTooLarge` past its limit. Latest-attempt jobs still come from `fakeGH.jobs` through `ListJobs`.
- `internal/daemon/rungraph.go` (Task 6): `type lru[K comparable, V any]`, `newLRU[K, V](maxEntries int, maxSize int64) *lru[K, V]` with `get`, `add(k K, v V, size int64)`, `remove`, `len`; `wfCache`, field `Backend.wfFiles`; `(*Backend).workflowFile(ctx, repo, sha, path string) ([]byte, error)`; `(*Backend).runGraph(ctx context.Context, repo, sha, path string, jobs []github.Job) (*model.RunGraph, string, error)`; constants `noteNoPermission`, `noteNoFile`, `noteMismatch`, `noteInvalid`, `wfFileEntries = 64`, `wfMissingTTL = 10 * time.Minute`; test helpers `ciPath`, `ciYAML`, `ciJobs()`.
- `internal/daemon/rundetail.go` (Task 7): `(*Backend).coveredName(name string) (string, bool, error)`; `detailErr(err error, owner, repo string) error` (404 "not found on GitHub", 410 "GitHub no longer keeps it", deadline 504 "GitHub did not answer in time", 403 with `actionsErr`'s wording, 429 with `RetryAt`, otherwise 502); `ghrJobKey(repo string, runID int64, name string) string`; `(*Backend).ghrJobs() (map[string]bool, error)`; `modelSteps([]github.Step) []model.Step` (`fetchSteps` uses it); `runJob(repo string, runID int64, j github.Job, ours map[string]bool) model.RunJob`; `detailCache`, field `Backend.details`; `detailActiveTTL`, `detailDoneTTL`, `detailEntries`; `var detailDeadline = 20 * time.Second`; test helpers `detailRun(gh, status)`, `setDetailDeadline`.
- `internal/daemon/joblogs.go` (Task 8): `logCache`, field `Backend.logs`; `logCall`, `logJob`, `logResult`, `logMiss`, `logOutcome` (`logParsed`, `logExpired`, `logTooLarge`, `logFailed`), `jobNotes`; `(*Backend).startLogLocked(logJob) *logCall`, `downloadLog`, `finishedJob`, `flaggedLog`; constants `parseLimit`, `rawLimit`, `logSlots = 4`, `annotationEntries = 500`, `logRetryTTL = 10 * time.Minute`; vars `parsedLogBytes`, `logDeadline = 60 * time.Second`, `logRouteWait = 50 * time.Second`, `rawLogDeadline = 50 * time.Second` (each log request's whole deadline from its start); test helpers `logLines`, `logJobOf(gh, id, name, lines...) github.Job`, `setParsedLogBytes`, `waitCalls`.
- Task 9: `var annotationsWait = 20 * time.Second`; test helpers `setAnnotationsWait`, `annotatedRun`. Task 10: `downloadHost(u string) bool`. Task 11 (package `api`): `positive(v, name string) (int64, error)`, `attemptParam(*http.Request) (int, error)`, `runParams`; `fakeBackend.runCalls`, `fakeBackend.runErr`.
- `web/src/api/hooks.ts` (Task 14): `runDetailPoll(data: RunDetail | undefined): number | false`; `annotationsPoll(data: RunAnnotations | undefined, active: boolean): number | false`.
- `web/src/lib/ansi.tsx` (Task 23): `interface AnsiSegment { text: string; color?: string; bold: boolean }` (`color` such as `"red"` or `"bright-red"`); `ansiSegments(raw: string): AnsiSegment[]`; `plainText(raw: string): string`; `matchRanges(text: string, query: string): [number, number][]`; `renderAnsi(raw: string, query?: string, current?: number): ReactNode[]` (matches as `<mark class="ghr-log-match">`, the current one with `data-current="true"`). Tokens `--log-bg`, `--log-fg`, `--log-muted`, `--log-command`, `--log-error-bg`, `--log-error-fg`, `--log-warning-bg`, `--log-warning-fg`, `--log-match-bg`, `--log-current`, `--log-target-bg`, `--log-ansi-{black,red,green,yellow,blue,magenta,cyan,white}` and their `bright-` forms; classes `ghr-log`, `ghr-log-muted`, `ghr-log-command`, `ghr-log-error`, `ghr-log-warning`, `ghr-log-target`, `ghr-log-match`, `ghr-log-ansi-<colour>`.
- `web/src/lib/log-view.ts` (Task 24): `LOG_WINDOW = 5000`; `type LogRow = { type: "line"; line: LogLine } | { type: "group"; line: LogLine; lines: LogLine[] }`; `groupRows(lines: LogLine[]): LogRow[]`; `containingGroup(lines: LogLine[], n: number): number | undefined`; `windowStart(lines: LogLine[], keep?: number): number`; `interface LogMatch { step: number; line: number; index: number }`; `findMatches(log: JobLog, query: string): LogMatch[]`.
- `LogViewer` (Task 25) reads `step` and `line` only on mount, so the job page gives it a `key` built from the job ID, step and line; each line row carries `data-line={n}`; a step with no lines shows "This step wrote no log lines."; only the expanded lines sit in the dark `ghr-log` panel. `RunFrame` (Task 16) styles rail links from `aria-current`; on the run page `#42` in the breadcrumbs is plain text.
- `web/src/pages/run.tsx` (Task 17): `RunPage()`, `JobPage()`, `RunNotFound(props: ErrorComponentProps)` (the routes' `errorComponent`: the not-found line for a `SearchParamError`, otherwise `RouteError`). `web/src/router.tsx`: `runRoute` (`/actions/$repo/runs/$id`, id `/app/actions/$repo/runs/$id`), `jobRoute` (`/actions/$repo/runs/$id/jobs/$jobId`, id `/app/actions/$repo/runs/$id/jobs/$jobId`), and a local `strictSearch<T extends object>(raw: Record<string, unknown>, parsed: T, names: string[]): T` that throws when `parseRunSearch` or `parseJobSearch` refused a present key, so a bad value shows the not-found line instead of silently being dropped.
- Section components, each taking `JobLinkFn` where it links to jobs and testing without a router: `ArtifactsSection({ repo, artifacts }: { repo: string; artifacts: RunArtifact[] })` (Task 19, renders nothing for an empty list); `AnnotationsSection({ data, error, onRetry, jobLink }: { data: RunAnnotations | undefined; error: unknown; onRetry: () => void; jobLink: JobLinkFn })` (Task 20, link names `Log line {n}, {job} / {step}`); `JobGraph({ graph, jobs, path, event, now, jobLink }: { graph: RunGraph; jobs: RunJob[]; path: string; event: string; now: number; jobLink: JobLinkFn })` (Task 21, title the file's base name with `on: {event}` beside it, region `role="group"` `aria-label="Job graph"`, link names `{job}, {status word}[, needs {names}]`); `JobsTimeline({ run, now, jobLink }: { run: RunDetail; now: number; jobLink: JobLinkFn })` (Task 22, title "Jobs timeline").

## Assumptions (evidence)

- Execution runs on the owner's Linux machine, as for the previous plan (`docs/superpowers/plans/2026-10-09-ghr-actions-view.md`, Assumptions); Task 1 Step 0 checks it.
- Ruling (draft, 2026-10-09): adding methods to an interface whose implementers the same or an earlier task supplies is scored coupling 2, not 3 (Tasks 5 and 11).
- Ruling, amends spec §1.2: `PageHeader` takes a string `title` and `description` (`web/node_modules/darkraise-ui/dist/layout/types.d.ts:56-62`), so the status icon cannot sit before the title and the description cannot carry mono spans. The status shows in the rail and in the Summary's Status fact; the description is plain text.
- Ruling: `RunFrame` and the section components take link render functions (`JobLinkFn`, `SummaryLinkFn`), and `pages/run.tsx` supplies typed TanStack `Link`s, so components test without a router. Both routes are registered in Task 17, because a typed `Link` needs its target route registered.
- TanStack `Link` sets `aria-current="page"` and `data-status="active"` whenever it is active, with or without `activeProps` (`applyLinkState`, `web/node_modules/@tanstack/react-router/dist/esm/link.js:219-240`; checked 2026-10-09, correcting the draft's "unless `activeProps` is given"). Rail links use `activeOptions={{ includeSearch: false }}`; the Summary link adds `exact: true` so it is not active on job pages.
- The kit sidebar matches nested paths (`currentPath.startsWith(href + "/")`, `web/node_modules/darkraise-ui/dist/chunk-4S4GXBTH.js:385`), so the Actions nav item stays active on both new pages with no change (spec §1.1; Fable review finding 28).
- Ruling, amends spec §2.3: the blob download has no idle deadline; the caller's detached context bounds it (60 s for parsed logs, 120 s for the raw log).
- Ruling: the workflow file cache needs no shared fetch of its own: the run details fetch that calls it is already shared per key.
- Ruling: "latest" and its number share a run-detail entry by storing the result under both keys when the shown attempt is the latest.
- Ruling: an uncovered repository is detected in the browser from `/api/config` names before any call; any 404 from the run API then means "This run no longer exists on GitHub."
- Ruling: Go emits non-nil slices and maps in the new models (labels, steps, needs, job_ids, items, lines, omitted), so the TypeScript types carry no `| null`. TypeScript `kind` and `level` stay `string` so JSON fixtures assign to them.
- Log and artifact redirects both answer `302` to `*.blob.core.windows.net` (checked 2026-10-09 on darkmem job 112607627142 and a darkmem artifact), so the allowlist is `.blob.core.windows.net` and `.githubusercontent.com`.
- The client's `do` reads whole bodies, classifies any status of 300 or above and caches ETag bodies (`internal/github/client.go:246-328`), so downloads take the new `request` path (Task 2).
- jsdom implements no `scrollIntoView`; `web/src/test/setup.ts:23` stubs it on `Element.prototype`, so the log viewer may call it unguarded in tests.
- `errorText` already appends "Retry after HH:MM" for an `ApiError` with `retryAt` (`web/src/query.ts:8-12`), so the pages' 429 wording needs no new code.
- Ruling (plan writing, 2026-10-09), amends spec §2.5 and the draft's 120 s raw-log bound: the web listener has `WriteTimeout: 60 * time.Second` (`internal/daemon/run.go:268`), so each log request gets one 50 s deadline from its start that covers the job read, the slot wait and the download (`logRouteWait`, `rawLogDeadline`). The log route then answers 503 "GitHub is still sending this log; try again shortly" while its shared download goes on and fills the cache; the raw route answers 503 "ghr is reading other logs; try again shortly" when no download slot frees in time. The web handler forwards `/api/…` through `http.StripPrefix("/api", api)` with no response size cap (`internal/webui/handler.go:41`).
- `go` and `gofmt` live in `/usr/local/go/bin`, which is not on `PATH` on the machine this plan was written on (checked 2026-10-09); Task 1 Step 0 tells the implementer to `export PATH=/usr/local/go/bin:$PATH` when `go` is missing.
- `yaml.v3`'s `Node.Decode` resolves aliases and `<<` merge keys (Task 4's test passes a merged job). GitHub answers 410 on `/actions/jobs/{id}/logs` itself, before any redirect (Fable review finding 5), so an expired log comes back as `ErrGone` through `request`.
- The run-details cache keeps only successful results; a 400 or a GitHub error is shared between waiting callers but not stored. Run details resolve the repository name before calling `githubErr`, so an uncovered repository answers 404 even while the token is degraded. The process-wide limit of 4 log downloads is a semaphore on `Backend.logs` (the daemon builds one `Backend`, `internal/daemon/run.go:214`); the raw log route takes a slot too.
- `PageHeader` throws "useRouterAdapter must be used inside a <RouterAdapterProvider>" without an adapter (darkraise-ui `router/routerAdapterContext.ts:10`), so `RunFrame` tests wrap it in a stub adapter whose `Link` is a plain anchor. TanStack `Link` accepts a query inside a string `to` (`/actions?repo=darkmem` navigated with `{repo: "darkmem"}`, probed 2026-10-09).
- `@eslint/js` recommended enables `no-control-regex`, so `ansi.tsx` carries one `eslint-disable-next-line` with its reason. `theme.test.ts` requires `!important` only on tokens the kit writes, so `--log-*` tokens go without it. Testing Library joins a step button's name parts without spaces (`FailedLint (ruff)1m25s`, observed in the plan's validation worktree on 2026-10-09), so tests match step buttons with regular expressions.
- Rule S file counts: the `RunStatusIcon` test lives in `src/lib/run-details.test.tsx` (Task 15) and the log palette contrast test in `theme.test.ts` is added by Task 25, not Task 23.
- Ruling (plan writing): the current search match decides which step and groups are open, so typing a query does not open a step per keystroke; the trade-off is that the step holding the current match cannot be collapsed while it is current.
- TanStack replaces the address with the validated search, so a dropped `attempt=0` is invisible to the page (`location.searchStr` was `""`, probed 2026-10-09); `validateSearch` therefore throws through `strictSearch`, which the router wraps in a `SearchParamError` (`@tanstack/router-core/dist/esm/router.js:694`).
- jsdom refuses to redefine `window.location.assign`; tests use `vi.stubGlobal("location", { assign })` (both observed in the plan's validation worktree on 2026-10-09, where Tasks 19 and 26 passed). jsdom has no `URL.createObjectURL`; the Raw log test stubs it with a `URL` subclass, since `mockApi` still calls `new URL`. Mutation errors toast app-wide through the query client's `MutationCache` (`web/src/query.ts:19`), so the Download and Raw log buttons need no error UI of their own.
- The README's PAT permissions sit under "## Requirements" (`README.md:20-21`). The run and annotation polls stopping (spec §5) are tested on the hooks in Task 14, not in page tests.
- Ruling, reads spec §1.5: `command` lines use the log panel's own `--log-command` token, set to the dark theme's primary value (188.3 62.7% 56.9%) in both themes, because the panel is dark in both and the light theme's `--primary` is tuned for a light background.
- Ruling, reads spec §1.5: when `line` or the current match falls above the 5,000-line cut, the window starts at that line (`windowStart`), and the "Show {n} earlier lines" button stays for anything above it, so a target near the top of a 200,000-line step does not render every line.
- Task 25's log palette contrast row in `theme.test.ts` passes at its red step because Task 23 added the tokens; it is a regression guard, not a red-first test.

## Task index

1. GitHub run reads
2. GitHub redirected downloads
3. Model types and job log parsing
4. Workflow graph
5. Daemon GitHub interface and fake
6. Workflow file cache and graph note
7. Run details and cache
8. Job logs
9. Annotations
10. Artifact download
11. API routes
12. Fixtures
13. TS types and test fixtures
14. API client and hooks
15. Run helpers and status icon
16. Run frame
17. Run and job routes
18. Actions page links to the run page
19. Artifacts section
20. Annotations section
21. Job graph
22. Jobs timeline
23. ANSI tokenizer
24. Log view model
25. Log viewer
26. Job page logs
27. Accessibility and README

---

### Task 1: GitHub run reads

**Files:**
- Modify: `internal/github/client.go` (the `ErrKind` constants at lines 22-29, the `Run` struct and its comment at lines 43-59, `classify`'s switch at lines 361-366)
- Create: `internal/github/runs.go`
- Test: `internal/github/runs_test.go` (new), `internal/github/client_test.go` (the `TestClassify` table at line 105)

**Interfaces:**
- Consumes: none
- Produces: `github.Run` fields `HeadSHA`, `Path`, `RunAttempt`, `PullRequests`; `github.PullRequest`; `github.ErrGone`; `github.Artifact`; `GetRun`, `GetRunAttempt`, `ListAttemptJobs`, `GetJob`, `ListArtifacts`, `GetContent` (Contracts, Go — GitHub client)

**Items:** 1, 2, 5, 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 0: Check the environment**

Run: `uname -s && go version && node --version && git status -sb | head -1`
Expected: `Linux`, Go 1.26.x, Node v24.x, and the branch `feat/run-details` (switch to it with `git switch feat/run-details` if needed; never work on `master`). If `go` is not found, run `export PATH=/usr/local/go/bin:$PATH` first: Go is installed there and may not be on `PATH`. If the machine is not Linux, stop and tell the owner before any task runs: Tasks 2 and 8 run the race detector.

- [ ] **Step 1: Write the failing tests**

In `internal/github/client_test.go`, add a 410 row to the `TestClassify` table, after the `"404"` row (line 105):

```go
		{"410", 410, nil, ErrGone, time.Time{}},
```

Create `internal/github/runs_test.go`:

```go
package github

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRunDetailFieldsDecode(t *testing.T) {
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":102,"name":"ci","run_attempt":2,"head_sha":"a1f9c3e","path":".github/workflows/ci.yml",
			"pull_requests":[{"number":87,"head":{"ref":"x"}}]}`)
	})
	r, err := c.GetRun(context.Background(), "darkmem", 102)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != 102 || r.RunAttempt != 2 || r.HeadSHA != "a1f9c3e" || r.Path != ".github/workflows/ci.yml" ||
		len(r.PullRequests) != 1 || r.PullRequests[0].Number != 87 {
		t.Fatalf("run %+v", r)
	}
	if f.requests[0] != "GET /repos/darkraise/darkmem/actions/runs/102" {
		t.Fatalf("request %s", f.requests[0])
	}
}

func TestRunReadsAskTheRightPaths(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(c *Client) error
		want string
	}{
		{"attempt", func(c *Client) error { _, err := c.GetRunAttempt(ctx, "darkmem", 102, 1); return err },
			"GET /repos/darkraise/darkmem/actions/runs/102/attempts/1"},
		{"attempt jobs", func(c *Client) error { _, err := c.ListAttemptJobs(ctx, "darkmem", 102, 1); return err },
			"GET /repos/darkraise/darkmem/actions/runs/102/attempts/1/jobs?per_page=100"},
		{"job", func(c *Client) error { _, err := c.GetJob(ctx, "darkmem", 1002); return err },
			"GET /repos/darkraise/darkmem/actions/jobs/1002"},
		{"artifacts", func(c *Client) error { _, err := c.ListArtifacts(ctx, "darkmem", 102); return err },
			"GET /repos/darkraise/darkmem/actions/runs/102/artifacts?per_page=100"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) })
			if err := tc.call(c); err != nil {
				t.Fatal(err)
			}
			if len(f.requests) != 1 || f.requests[0] != tc.want {
				t.Fatalf("requests %v", f.requests)
			}
		})
	}
}

func TestAttemptJobsAndArtifactsPaginate(t *testing.T) {
	var base string
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		next := ""
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, base, r.URL.RequestURI()))
		} else {
			next = "2"
		}
		if strings.Contains(r.URL.Path, "/artifacts") {
			fmt.Fprintf(w, `{"artifacts":[{"id":1%s,"name":"dist","size_in_bytes":2400000,"expired":true,"expires_at":"2026-10-17T13:59:00Z"}]}`, next)
			return
		}
		fmt.Fprintf(w, `{"jobs":[{"id":1%s,"name":"build"}]}`, next)
	})
	base = c.BaseURL
	jobs, err := c.ListAttemptJobs(context.Background(), "darkmem", 102, 1)
	if err != nil || len(jobs) != 2 || jobs[1].ID != 12 {
		t.Fatalf("jobs %+v err %v", jobs, err)
	}
	arts, err := c.ListArtifacts(context.Background(), "darkmem", 102)
	if err != nil || len(arts) != 2 || arts[1].ID != 12 {
		t.Fatalf("artifacts %+v err %v", arts, err)
	}
	a := arts[0]
	if a.Name != "dist" || a.SizeInBytes != 2400000 || !a.Expired || a.ExpiresAt == nil ||
		!a.ExpiresAt.Equal(time.Date(2026, 10, 17, 13, 59, 0, 0, time.UTC)) {
		t.Fatalf("artifact %+v", a)
	}
}

func TestGetContent(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, want, wantURI, wantErr string
	}{
		{name: "base64 with line breaks", path: ".github/workflows/ci.yml",
			body:    `{"content":"am9iczoK\nICBi\nOiB7fQo=\n","encoding":"base64"}`,
			want:    "jobs:\n  b: {}\n",
			wantURI: "GET /repos/darkraise/darkmem/contents/.github/workflows/ci.yml?ref=a1f9c3e"},
		{name: "segments escaped, slashes kept", path: ".github/workflows/a b#1.yml",
			body:    `{"content":"","encoding":"base64"}`,
			wantURI: "GET /repos/darkraise/darkmem/contents/.github/workflows/a%20b%231.yml?ref=a1f9c3e"},
		{name: "too large for the API", path: "big.yml",
			body:    `{"content":"","encoding":"none"}`,
			wantURI: "GET /repos/darkraise/darkmem/contents/big.yml?ref=a1f9c3e",
			wantErr: `github: big.yml has encoding "none", not base64`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			got, err := c.GetContent(context.Background(), "darkmem", tc.path, "a1f9c3e")
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err %v", err)
				}
			} else if err != nil || string(got) != tc.want {
				t.Fatalf("got %q err %v", got, err)
			}
			if f.requests[0] != tc.wantURI {
				t.Fatalf("request %s", f.requests[0])
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/github/ -count=1`
Expected: FAIL to compile with `undefined: ErrGone` and `c.GetRun undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/github/client.go`, add `ErrGone` as the last `ErrKind` constant:

```go
	ErrServer
	ErrGone
)
```

Replace the `Run` struct and its comment (lines 43-59) with:

```go
// Run is a workflow run. The tick and the label check read only ID; the
// Actions page and the run page read the rest. GitHub may leave
// run_started_at out.
type Run struct {
	ID           int64         `json:"id"`
	Name         string        `json:"name"`
	DisplayTitle string        `json:"display_title"`
	RunNumber    int64         `json:"run_number"`
	HeadBranch   string        `json:"head_branch"`
	Event        string        `json:"event"`
	Actor        Account       `json:"actor"`
	Status       string        `json:"status"`
	Conclusion   string        `json:"conclusion"`
	CreatedAt    time.Time     `json:"created_at"`
	RunStartedAt *time.Time    `json:"run_started_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	HTMLURL      string        `json:"html_url"`
	HeadSHA      string        `json:"head_sha"`
	Path         string        `json:"path"`
	RunAttempt   int           `json:"run_attempt"`
	PullRequests []PullRequest `json:"pull_requests"`
}
```

In `classify`, add a 410 case after the 404 case (line 361):

```go
	case resp.StatusCode == 404:
		e.Kind = ErrNotFound
	case resp.StatusCode == 410:
		e.Kind = ErrGone
```

Create `internal/github/runs.go`:

```go
package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type PullRequest struct {
	Number int `json:"number"`
}

type Artifact struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	SizeInBytes int64      `json:"size_in_bytes"`
	Expired     bool       `json:"expired"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	data, _, err := c.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (c *Client) GetRun(ctx context.Context, repo string, id int64) (*Run, error) {
	var r Run
	if err := c.getJSON(ctx, c.repoURL(repo, fmt.Sprintf("/actions/runs/%d", id)), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (c *Client) GetRunAttempt(ctx context.Context, repo string, id int64, attempt int) (*Run, error) {
	var r Run
	if err := c.getJSON(ctx, c.repoURL(repo, fmt.Sprintf("/actions/runs/%d/attempts/%d", id, attempt)), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListAttemptJobs lists every job of one attempt. A re-run attempt lists the
// jobs it did not re-run too, under new IDs.
func (c *Client) ListAttemptJobs(ctx context.Context, repo string, id int64, attempt int) ([]Job, error) {
	var out []Job
	err := c.getAll(ctx, c.repoURL(repo, fmt.Sprintf("/actions/runs/%d/attempts/%d/jobs?per_page=100", id, attempt)), func(b []byte) error {
		var page struct {
			Jobs []Job `json:"jobs"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Jobs...)
		return nil
	})
	return out, err
}

func (c *Client) GetJob(ctx context.Context, repo string, jobID int64) (*Job, error) {
	var j Job
	if err := c.getJSON(ctx, c.repoURL(repo, fmt.Sprintf("/actions/jobs/%d", jobID)), &j); err != nil {
		return nil, err
	}
	return &j, nil
}

func (c *Client) ListArtifacts(ctx context.Context, repo string, runID int64) ([]Artifact, error) {
	var out []Artifact
	err := c.getAll(ctx, c.repoURL(repo, fmt.Sprintf("/actions/runs/%d/artifacts?per_page=100", runID)), func(b []byte) error {
		var page struct {
			Artifacts []Artifact `json:"artifacts"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Artifacts...)
		return nil
	})
	return out, err
}

// GetContent reads one file at ref. GitHub answers base64 with line breaks;
// a file over 1 MB comes back with encoding "none", which is an error here.
func (c *Client) GetContent(ctx context.Context, repo, path, ref string) ([]byte, error) {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	u := c.repoURL(repo, "/contents/"+strings.Join(segs, "/")+"?ref="+url.QueryEscape(ref))
	var body struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := c.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	if body.Encoding != "base64" {
		return nil, fmt.Errorf("github: %s has encoding %q, not base64", path, body.Encoding)
	}
	return base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(body.Content))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/github/ -count=1`
Expected: PASS.

- [ ] **Step 5: Check formatting and vet**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/`
Expected: no output from `gofmt`, and `go vet` passes.

- [ ] **Step 6: Commit**

```bash
git add internal/github/client.go internal/github/client_test.go internal/github/runs.go internal/github/runs_test.go
git commit -m "feat(github): read runs, attempts, jobs, artifacts"
```

---

### Task 2: GitHub redirected downloads

**Files:**
- Modify: `internal/github/client.go` (the `Client` struct at lines 128-144, `New` at lines 146-152, `do` at lines 245-328)
- Create: `internal/github/download.go`
- Test: `internal/github/download_test.go`

**Interfaces:**
- Consumes: `github.ErrGone` (Task 1)
- Produces: `Client.Blob`, `github.ErrTooLarge`, `(*Client).JobLog`, `(*Client).ArtifactURL` (Contracts, Go — GitHub client)

**Items:** 4, 5

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

The existing `do` path cannot carry these calls: it reads whole bodies, treats any status of 300 or above as an error and keeps ETag bodies (spec §2.3). `request` shares `do`'s header setup, suspension check, `record`, `classify` and suspension update, through three helpers this task extracts from `do`, so the existing suspension tests (`TestRateLimitSuspendsEveryRequest`, `TestOlderGenerationLeavesCacheAndSuspension`) prove the extraction.

- [ ] **Step 1: Write the failing tests**

Create `internal/github/download_test.go`:

```go
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// newBlobServer serves body and records the Authorization header of each request.
func newBlobServer(t *testing.T, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &auth
}

func TestJobLogFollowsTheRedirectWithoutTheToken(t *testing.T) {
	blob, auth := newBlobServer(t, "2026-10-03T13:56:10.0000000Z hello\n")
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("API call without the token")
		}
		w.Header().Set("Location", blob.URL+"/log?sig=1")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("X-RateLimit-Remaining", "4321")
		w.WriteHeader(http.StatusFound)
	})
	for i := 0; i < 2; i++ {
		got, err := c.JobLog(context.Background(), "darkmem", 1002, 1<<20)
		if err != nil || string(got) != "2026-10-03T13:56:10.0000000Z hello\n" {
			t.Fatalf("pass %d: log %q err %v", i, got, err)
		}
	}
	if len(*auth) != 2 || (*auth)[0] != "" || (*auth)[1] != "" {
		t.Fatalf("blob server saw Authorization %q", *auth)
	}
	if c.RateRemaining() != 4321 {
		t.Fatalf("rate remaining %d", c.RateRemaining())
	}
	if len(f.requests) != 2 || f.requests[0] != "GET /repos/darkraise/darkmem/actions/jobs/1002/logs" {
		t.Fatalf("requests %v", f.requests)
	}
	if len(c.cache) != 0 {
		t.Fatalf("redirect kept in the ETag cache: %v", c.cache)
	}
}

func TestJobLogStopsAtMax(t *testing.T) {
	blob, _ := newBlobServer(t, "0123456789")
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, blob.URL+"/log", http.StatusFound)
	})
	if got, err := c.JobLog(context.Background(), "darkmem", 1, 10); err != nil || string(got) != "0123456789" {
		t.Fatalf("at max: %q %v", got, err)
	}
	if _, err := c.JobLog(context.Background(), "darkmem", 1, 9); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over max: %v", err)
	}
}

func TestDownloadErrors(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		status  int
		headers map[string]string
		call    func(c *Client) error
		kind    ErrKind
		plain   string
	}{
		{name: "expired log", status: 410, kind: ErrGone,
			call: func(c *Client) error { _, err := c.JobLog(ctx, "darkmem", 1, 10); return err }},
		{name: "expired artifact", status: 410, kind: ErrGone,
			call: func(c *Client) error { _, err := c.ArtifactURL(ctx, "darkmem", 11); return err }},
		{name: "missing permission", status: 403, headers: map[string]string{"X-Accepted-GitHub-Permissions": "actions=read"}, kind: ErrAuth,
			call: func(c *Client) error { _, err := c.ArtifactURL(ctx, "darkmem", 11); return err }},
		{name: "no redirect", status: 200,
			call:  func(c *Client) error { _, err := c.ArtifactURL(ctx, "darkmem", 11); return err },
			plain: "github: /repos/darkraise/darkmem/actions/artifacts/11/zip answered 200 without a download address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"message":"nope"}`)
			})
			err := tc.call(c)
			if tc.plain != "" {
				if err == nil || err.Error() != tc.plain {
					t.Fatalf("err %v", err)
				}
				return
			}
			if !IsKind(err, tc.kind) {
				t.Fatalf("err %#v, want kind %d", err, tc.kind)
			}
		})
	}
}

func TestArtifactURLDoesNotFollow(t *testing.T) {
	const signed = "https://productionresultssa1.blob.core.windows.net/actions-results/zip?sig=abc"
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", signed)
		w.Header().Set("ETag", `"v1"`)
		w.WriteHeader(http.StatusFound)
	})
	got, err := c.ArtifactURL(context.Background(), "darkmem", 11)
	if err != nil || got != signed {
		t.Fatalf("url %q err %v", got, err)
	}
	if len(f.requests) != 1 || f.requests[0] != "GET /repos/darkraise/darkmem/actions/artifacts/11/zip" {
		t.Fatalf("requests %v", f.requests)
	}
	if len(c.cache) != 0 {
		t.Fatalf("redirect kept in the ETag cache: %v", c.cache)
	}
}

func TestDownloadsShareTheRateLimitSuspension(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(now.Add(time.Minute).Unix()))
		w.WriteHeader(403)
	})
	c.Now = func() time.Time { return now }
	if _, err := c.ArtifactURL(context.Background(), "darkmem", 11); !IsKind(err, ErrRateLimit) {
		t.Fatalf("first call: %v", err)
	}
	if got := c.SuspendedUntil(); !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("SuspendedUntil = %v", got)
	}
	if _, err := c.JobLog(context.Background(), "darkmem", 1, 10); !IsKind(err, ErrRateLimit) {
		t.Fatalf("during suspension: %v", err)
	}
	if _, err := c.GetRepo(context.Background(), "darkmem"); !IsKind(err, ErrRateLimit) {
		t.Fatalf("API call during suspension: %v", err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("requests sent during suspension: %v", f.requests)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/github/ -run 'Download|JobLog|ArtifactURL' -count=1`
Expected: FAIL to compile with `c.JobLog undefined` and `undefined: ErrTooLarge`.

- [ ] **Step 3: Extract the request helpers from `do`**

In `internal/github/client.go`, replace the `Client` struct with:

```go
type Client struct {
	BaseURL string // https://api.github.com
	Owner   string
	Token   func() string
	HTTP    *http.Client
	// Blob fetches the signed download URLs GitHub redirects to. It sends no
	// token, and the caller's context bounds the whole download.
	Blob  *http.Client
	Now   func() time.Time
	Sleep func(time.Duration)

	mu         sync.Mutex
	cache      map[string]cached
	remaining  int
	retryAt    time.Time
	gen        uint64 // bumped when the token changes; older responses are not recorded
	meta       TokenMeta
	mutMu      sync.Mutex
	lastMutate time.Time
}
```

Replace `New` with:

```go
func New(owner string, token func() string) *Client {
	return &Client{
		BaseURL: "https://api.github.com", Owner: owner, Token: token,
		HTTP: &http.Client{Timeout: 30 * time.Second}, Blob: newBlobClient(), Now: time.Now, Sleep: time.Sleep,
		remaining: -1,
	}
}
```

Insert these three helpers directly above the `// do performs one request` comment:

```go
// newRequest builds an authenticated API request and returns the token
// generation it was built under.
func (c *Client) newRequest(ctx context.Context, method, u string, body io.Reader) (*http.Request, uint64, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	// SetToken and Reload store the new token before ForgetCache bumps the
	// generation, so reading the generation first means a request may carry
	// a newer token than its generation (its response is then dropped) but
	// never an older one.
	c.mu.Lock()
	gen := c.gen
	c.mu.Unlock()
	req.Header.Set("Authorization", "Bearer "+c.Token())
	return req, gen, nil
}

// suspendedErr fails a request fast while a rate limit suspends the API.
func (c *Client) suspendedErr() error {
	if until := c.SuspendedUntil(); !until.IsZero() {
		return &APIError{Status: http.StatusTooManyRequests, Kind: ErrRateLimit, Message: "rate limited until " + until.Format(time.RFC3339), RetryAt: until}
	}
	return nil
}

// suspend extends the suspension to a rate-limit error's retry time, unless
// the token changed while the request was in flight.
func (c *Client) suspend(gen uint64, e *APIError) {
	if e.Kind != ErrRateLimit {
		return
	}
	c.mu.Lock()
	if gen == c.gen && e.RetryAt.After(c.retryAt) {
		c.retryAt = e.RetryAt
	}
	c.mu.Unlock()
}
```

In `do`, replace everything from `req, err := http.NewRequestWithContext(ctx, method, u, rd)` down to and including the suspension check's closing brace (the `if c.Now().Before(retryAt) { … }` block) with:

```go
	req, gen, err := c.newRequest(ctx, method, u, rd)
	if err != nil {
		return nil, "", err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if err := c.suspendedErr(); err != nil {
		return nil, "", err
	}
	c.mu.Lock()
	prev, hasPrev := c.cache[u]
	c.mu.Unlock()
```

Further down in `do`, replace the error branch

```go
	if apiErr != nil {
		err := apiErr
		if err.Kind == ErrRateLimit {
			c.mu.Lock()
			if gen == c.gen && err.RetryAt.After(c.retryAt) {
				c.retryAt = err.RetryAt
			}
			c.mu.Unlock()
		}
		return nil, "", err
	}
```

with:

```go
	if apiErr != nil {
		c.suspend(gen, apiErr)
		return nil, "", apiErr
	}
```

Leave `mutate` unchanged.

- [ ] **Step 4: Write the download path**

Create `internal/github/download.go`:

```go
package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

var ErrTooLarge = errors.New("github: download is larger than the limit")

func newBlobClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ForceAttemptHTTP2:     true,
	}}
}

// request performs one API call that may answer with a redirect, which it
// returns instead of following. The response comes back unread and is never
// kept in the ETag cache; statuses of 400 and above become an *APIError.
func (c *Client) request(ctx context.Context, method, u string) (*http.Response, error) {
	req, gen, err := c.newRequest(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	if err := c.suspendedErr(); err != nil {
		return nil, err
	}
	hc := *c.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	var apiErr *APIError
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		apiErr = c.classify(resp, data)
	}
	c.record(gen, resp.Header, apiErr)
	if apiErr != nil {
		c.suspend(gen, apiErr)
		return nil, apiErr
	}
	return resp, nil
}

// redirectURL returns where GitHub redirects a download endpoint to.
func (c *Client) redirectURL(ctx context.Context, u string) (string, error) {
	resp, err := c.request(ctx, http.MethodGet, u)
	if err != nil {
		return "", err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || loc == "" {
		return "", fmt.Errorf("github: %s answered %d without a download address", resp.Request.URL.Path, resp.StatusCode)
	}
	return loc, nil
}

// JobLog downloads a finished job's log, at most max bytes. The signed URL
// GitHub redirects to is fetched without the token.
func (c *Client) JobLog(ctx context.Context, repo string, jobID int64, max int64) ([]byte, error) {
	loc, err := c.redirectURL(ctx, c.repoURL(repo, fmt.Sprintf("/actions/jobs/%d/logs", jobID)))
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Blob.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: log download answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, ErrTooLarge
	}
	return data, nil
}

// ArtifactURL returns the signed address of an artifact's zip without
// following it; it expires after about a minute.
func (c *Client) ArtifactURL(ctx context.Context, repo string, id int64) (string, error) {
	return c.redirectURL(ctx, c.repoURL(repo, fmt.Sprintf("/actions/artifacts/%d/zip", id)))
}
```

- [ ] **Step 5: Run the package tests, with the race detector**

Run: `timeout 300 go test ./internal/github/ -count=1 && timeout 300 go test -race ./internal/github/ -count=1`
Expected: PASS twice; the existing suspension and generation tests pass unchanged.

- [ ] **Step 6: Check formatting and vet**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/`
Expected: no output from `gofmt`, and `go vet` passes.

- [ ] **Step 7: Commit**

```bash
git add internal/github/client.go internal/github/download.go internal/github/download_test.go
git commit -m "feat(github): download job logs and artifact URLs"
```

---

### Task 3: Model types and job log parsing

**Files:**
- Modify: `internal/model/model.go` (insert after the `ActionsRepo` struct, before the `// Activity is GET /activity` comment at line 322)
- Create: `internal/joblog/joblog.go`
- Test: `internal/joblog/joblog_test.go`

**Interfaces:**
- Consumes: `model.Step` (existing, `internal/model/model.go:138-146`)
- Produces: `model.RunDetail`, `RunJob`, `RunGraph`, `GraphNode`, `RunArtifact`, `RunAnnotations`, `Annotation`, `JobLog`, `LogStep`, `LogLine`, `RawLog`, `DownloadURL` (Contracts, Go — model); `joblog.Parse`, `joblog.PerJob`, `joblog.LevelRank`, `joblog.Annotations` (Contracts, Go — log parsing)

**Items:** 3, 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The fixture is trimmed from a darkmem job whose "Create test databases", "Lint" and "Test" steps start in the same second. The pointer rule of spec §4.2 gives the eight steps 1, 3, 1, 3, 5, 6, 0 and 1 lines; timestamps alone would put the Lint lines into Test.

- [ ] **Step 1: Write the failing tests**

Create `internal/joblog/joblog_test.go`:

```go
package joblog

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func at(hms string) *time.Time {
	t, err := time.Parse(time.RFC3339, "2026-10-03T"+hms+"Z")
	if err != nil {
		panic(err)
	}
	return &t
}

// Trimmed from a darkmem job log: "Create test databases", "Lint" and "Test"
// start in the same second, which timestamps alone cannot split.
var darkmemSteps = []model.Step{
	{Number: 1, Name: "Set up job", StartedAt: at("02:52:41"), CompletedAt: at("02:52:44")},
	{Number: 2, Name: "Set up runner", StartedAt: at("02:52:44"), CompletedAt: at("02:52:44")},
	{Number: 3, Name: "Initialize containers", StartedAt: at("02:52:44"), CompletedAt: at("02:52:54")},
	{Number: 4, Name: "Create test databases", StartedAt: at("02:52:54"), CompletedAt: at("02:52:54")},
	{Number: 5, Name: "Lint", StartedAt: at("02:52:54"), CompletedAt: at("02:52:54")},
	{Number: 6, Name: "Test", StartedAt: at("02:52:54"), CompletedAt: at("02:53:31")},
	{Number: 7, Name: "Run tests"},
	{Number: 8, Name: "Complete job", StartedAt: at("02:53:32"), CompletedAt: at("02:53:33")},
}

const darkmemLog = "\xef\xbb\xbf2026-10-03T02:52:41.5104326Z Current runner version: '2.338.0'\r\n" +
	"2026-10-03T02:52:44.0101010Z ##[group]Run ./setup-runner\r\n" +
	"2026-10-03T02:52:44.0202020Z runner ready\n" +
	"2026-10-03T02:52:44.0303030Z ##[endgroup]\n" +
	"2026-10-03T02:52:44.9000000Z ##[group]Run docker compose up -d\n" +
	"2026-10-03T02:52:54.1000000Z ##[group]Run createdb test_a test_b\n" +
	"  continued without a timestamp\n" +
	"2026-10-03T02:52:54.1200000Z ##[endgroup]\n" +
	"2026-10-03T02:52:54.2000000Z ##[group]Run ruff check .\n" +
	"2026-10-03T02:52:54.2100000Z ##[endgroup]\n" +
	"2026-10-03T02:52:54.3000000Z \x1b[31mFound 1 error.\x1b[0m\n" +
	"2026-10-03T02:52:54.3100000Z ##[error]src/cache.py:12:5: F401 'os' imported but unused\n" +
	"2026-10-03T02:52:54.3200000Z ##[warning]ruff 0.6 is outdated\n" +
	"2026-10-03T02:52:54.4000000Z ##[group]Run pytest -q\n" +
	"2026-10-03T02:52:55.0000000Z ##[command]/usr/bin/pytest -q\n" +
	"2026-10-03T02:53:00.0000000Z ##[debug]collected 12 items\n" +
	"2026-10-03T02:53:20.0000000Z ##[notice]Retried 1 flaky test\n" +
	"2026-10-03T02:53:25.0000000Z ##[group]Run echo done\n" +
	"2026-10-03T02:53:30.0000000Z ##[endgroup]\n" +
	"2026-10-03T02:53:33.0000000Z Cleaning up orphan processes\n"

func TestParseSplitsTheDarkmemLog(t *testing.T) {
	got := Parse([]byte(darkmemLog), darkmemSteps)
	var counts []int
	for i, s := range got {
		if s.Number != darkmemSteps[i].Number || s.Name != darkmemSteps[i].Name || s.Lines == nil {
			t.Fatalf("step %d: %+v", i, s)
		}
		for j, l := range s.Lines {
			if l.N != j+1 {
				t.Fatalf("step %d line %d numbered %d", s.Number, j+1, l.N)
			}
		}
		counts = append(counts, len(s.Lines))
	}
	if want := []int{1, 3, 1, 3, 5, 6, 0, 1}; !reflect.DeepEqual(counts, want) {
		t.Fatalf("line counts %v, want %v", counts, want)
	}
	for _, tc := range []struct {
		step, n    int
		kind, text string
	}{
		{1, 1, "", "Current runner version: '2.338.0'"},
		{2, 1, "group", "Run ./setup-runner"},
		{2, 3, "endgroup", ""},
		{3, 1, "group", "Run docker compose up -d"},
		{4, 2, "", "  continued without a timestamp"},
		{5, 3, "", "\x1b[31mFound 1 error.\x1b[0m"},
		{5, 4, "error", "src/cache.py:12:5: F401 'os' imported but unused"},
		{5, 5, "warning", "ruff 0.6 is outdated"},
		{6, 2, "command", "/usr/bin/pytest -q"},
		{6, 3, "debug", "collected 12 items"},
		{6, 4, "notice", "Retried 1 flaky test"},
		// No later step's window holds 02:53:25, so the pointer stays on Test.
		{6, 5, "group", "Run echo done"},
		{8, 1, "", "Cleaning up orphan processes"},
	} {
		l := got[tc.step-1].Lines[tc.n-1]
		if l.Kind != tc.kind || l.Text != tc.text {
			t.Errorf("step %d line %d = %q %q, want %q %q", tc.step, tc.n, l.Kind, l.Text, tc.kind, tc.text)
		}
	}
}

func TestParseEdges(t *testing.T) {
	one := []model.Step{{Number: 1, Name: "Only", StartedAt: at("10:00:00"), CompletedAt: at("10:00:01")}}
	untimed := []model.Step{{Number: 1, Name: "A"}, {Number: 2, Name: "B"}}
	for _, tc := range []struct {
		name  string
		raw   string
		steps []model.Step
		want  [][]model.LogLine
	}{
		{"no steps", "2026-10-03T10:00:00Z x\n", nil, [][]model.LogLine{}},
		{"unknown prefix stays text", "2026-10-03T10:00:00Z ##[section]Starting: Only\n", one,
			[][]model.LogLine{{{N: 1, Text: "##[section]Starting: Only"}}}},
		{"late line with no later step stays", "2026-10-03T10:00:00Z a\n2026-10-03T10:00:30Z b\n", one,
			[][]model.LogLine{{{N: 1, Text: "a"}, {N: 2, Text: "b"}}}},
		{"first line without a timestamp", "orphan\n2026-10-03T10:00:00Z a", one,
			[][]model.LogLine{{{N: 1, Text: "orphan"}, {N: 2, Text: "a"}}}},
		{"no step has times", "2026-10-03T10:00:00Z a\n", untimed,
			[][]model.LogLine{{{N: 1, Text: "a"}}, {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse([]byte(tc.raw), tc.steps)
			lines := [][]model.LogLine{}
			for _, s := range got {
				lines = append(lines, s.Lines)
			}
			if !reflect.DeepEqual(lines, tc.want) {
				t.Fatalf("lines %+v, want %+v", lines, tc.want)
			}
		})
	}
}

func TestAnnotationsMergeSortAndCap(t *testing.T) {
	steps := []model.LogStep{
		{Number: 2, Name: "Lint", Lines: []model.LogLine{
			{N: 1, Kind: "notice", Text: "cache hit"},
			{N: 2, Kind: "error", Text: "F401 'os' imported but unused"},
			{N: 3, Kind: "error", Text: "F401 'os' imported but unused"},
			{N: 4, Kind: "warning", Text: "F401 'os' imported but unused"},
			{N: 5, Kind: "debug", Text: "not an annotation"},
			{N: 6, Kind: "group", Text: "Run ruff"},
		}},
		{Number: 3, Name: "Test", Lines: []model.LogLine{
			{N: 1, Kind: "error", Text: "F401 'os' imported but unused"},
		}},
	}
	items, omitted := Annotations(1002, "Backend tests", steps)
	want := []model.Annotation{
		{Level: "error", JobID: 1002, JobName: "Backend tests", Step: 2, StepName: "Lint", Line: 2, Message: "F401 'os' imported but unused"},
		{Level: "error", JobID: 1002, JobName: "Backend tests", Step: 3, StepName: "Test", Line: 1, Message: "F401 'os' imported but unused"},
		{Level: "warning", JobID: 1002, JobName: "Backend tests", Step: 2, StepName: "Lint", Line: 4, Message: "F401 'os' imported but unused"},
		{Level: "notice", JobID: 1002, JobName: "Backend tests", Step: 2, StepName: "Lint", Line: 1, Message: "cache hit"},
	}
	if !reflect.DeepEqual(items, want) || omitted != 0 {
		t.Fatalf("items %+v omitted %d", items, omitted)
	}

	var many []model.LogLine
	for i := 1; i <= PerJob+3; i++ {
		many = append(many, model.LogLine{N: i, Kind: "error", Text: fmt.Sprintf("error %d", i)})
	}
	many = append(many, model.LogLine{N: PerJob + 4, Kind: "warning", Text: "late warning"})
	items, omitted = Annotations(7, "big", []model.LogStep{{Number: 1, Name: "s", Lines: many}})
	if len(items) != PerJob || omitted != 4 || items[PerJob-1].Message != fmt.Sprintf("error %d", PerJob) {
		t.Fatalf("%d items, omitted %d, last %+v", len(items), omitted, items[len(items)-1])
	}

	if items, omitted := Annotations(1, "clean", []model.LogStep{{Number: 1, Lines: []model.LogLine{}}}); items == nil || len(items) != 0 || omitted != 0 {
		t.Fatalf("clean job: %#v %d", items, omitted)
	}
}

func TestLevelRank(t *testing.T) {
	for level, want := range map[string]int{"error": 0, "warning": 1, "notice": 2, "debug": 3, "": 3} {
		if got := LevelRank(level); got != want {
			t.Errorf("LevelRank(%q) = %d, want %d", level, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/joblog/ -count=1`
Expected: FAIL to compile: `undefined: Parse` (and `model.LogStep` undefined).

- [ ] **Step 3: Add the model types**

In `internal/model/model.go`, insert after the `ActionsRepo` struct (before the `// Activity is GET /activity` comment):

```go
// RunDetail is GET /actions/{repo}/runs/{id}: one attempt of a workflow run.
// Attempts is the run's latest attempt number. Graph is nil when GraphNote
// says why the page draws a timeline instead.
type RunDetail struct {
	Repo         string        `json:"repo"`
	Watched      bool          `json:"watched"`
	ID           int64         `json:"id"`
	RunNumber    int64         `json:"run_number"`
	Attempt      int           `json:"attempt"`
	Attempts     int           `json:"attempts"`
	Workflow     string        `json:"workflow"`
	Title        string        `json:"title"`
	Branch       string        `json:"branch"`
	Event        string        `json:"event"`
	Actor        string        `json:"actor"`
	Status       string        `json:"status"`
	Conclusion   string        `json:"conclusion"`
	HeadSHA      string        `json:"head_sha"`
	Path         string        `json:"path"`
	PullRequests []int         `json:"pull_requests"`
	StartedAt    time.Time     `json:"started_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	HTMLURL      string        `json:"html_url"`
	Jobs         []RunJob      `json:"jobs"`
	Graph        *RunGraph     `json:"graph,omitempty"`
	GraphNote    string        `json:"graph_note,omitempty"`
	Artifacts    []RunArtifact `json:"artifacts"`
	FetchedAt    time.Time     `json:"fetched_at"`
}

// RunJob is one job of a run attempt; GHR is set when ghr's runners ran a
// job of that name in the run.
type RunJob struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	RunnerName  string     `json:"runner_name"`
	Labels      []string   `json:"labels"`
	HTMLURL     string     `json:"html_url"`
	GHR         bool       `json:"ghr"`
	Steps       []Step     `json:"steps"`
}

type RunGraph struct {
	Nodes []GraphNode `json:"nodes"`
}

// GraphNode is one workflow job: Key is its YAML ID, Column the length of
// its longest needs chain, JobIDs its jobs in the jobs API's order. Group is
// set for a matrix or a reusable-workflow caller.
type GraphNode struct {
	Key    string   `json:"key"`
	Name   string   `json:"name"`
	Column int      `json:"column"`
	Needs  []string `json:"needs"`
	JobIDs []int64  `json:"job_ids"`
	Group  bool     `json:"group"`
}

type RunArtifact struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	SizeBytes int64      `json:"size_bytes"`
	Expired   bool       `json:"expired"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// RunAnnotations is GET /actions/{repo}/runs/{id}/annotations. Omitted counts,
// per job ID, the annotations beyond the per-job cap; Unread counts finished
// jobs whose logs were not ready within the wait.
type RunAnnotations struct {
	Items       []Annotation  `json:"items"`
	Omitted     map[int64]int `json:"omitted"`
	Unread      int           `json:"unread"`
	ExpiredLogs int           `json:"expired_logs"`
	TooLarge    int           `json:"too_large"`
	Failed      int           `json:"failed"`
}

// Annotation is an error, warning or notice line of a job log; Line counts
// within its step.
type Annotation struct {
	Level    string `json:"level"`
	JobID    int64  `json:"job_id"`
	JobName  string `json:"job_name"`
	Step     int    `json:"step"`
	StepName string `json:"step_name"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

// JobLog is GET /actions/{repo}/jobs/{jobId}/log: a finished job's log split
// into its steps.
type JobLog struct {
	JobID    int64     `json:"job_id"`
	TooLarge bool      `json:"too_large"`
	Expired  bool      `json:"expired"`
	Steps    []LogStep `json:"steps"`
}

type LogStep struct {
	Number int       `json:"number"`
	Name   string    `json:"name"`
	Lines  []LogLine `json:"lines"`
}

// LogLine is one log line. Kind is "", group, endgroup, error, warning,
// notice, command or debug; Text has the ##[kind] prefix removed and keeps
// ANSI escapes.
type LogLine struct {
	N    int    `json:"n"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// RawLog is GET /actions/{repo}/jobs/{jobId}/log/raw.
type RawLog struct {
	Text string `json:"text"`
}

// DownloadURL is GET /actions/{repo}/artifacts/{id}/download: GitHub's
// signed address, valid for about a minute.
type DownloadURL struct {
	URL string `json:"url"`
}
```

- [ ] **Step 4: Write the parser**

Create `internal/joblog/joblog.go`. The byte-order mark is written as the escape `"\uFEFF"`; type the six characters, not the invisible character itself, or the compiler refuses the file with "illegal byte order mark".

```go
// Package joblog splits a GitHub Actions job log into its steps and reads
// the annotations out of it.
package joblog

import (
	"sort"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// PerJob caps the annotations one job contributes.
const PerJob = 50

// slack widens each step's window: log timestamps and step times are
// rounded differently.
const slack = time.Second

var kinds = []string{"group", "endgroup", "error", "warning", "notice", "command", "debug"}

// split reads one log line's timestamp; ok is false for a line without one.
func split(line string) (at time.Time, text string, ok bool) {
	ts, rest, found := strings.Cut(line, " ")
	if !found {
		ts, rest = line, ""
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, line, false
	}
	return t, rest, true
}

// kind removes a known ##[kind] prefix.
func kind(text string) (string, string) {
	if !strings.HasPrefix(text, "##[") {
		return "", text
	}
	for _, k := range kinds {
		if p := "##[" + k + "]"; strings.HasPrefix(text, p) {
			return k, text[len(p):]
		}
	}
	return "", text
}

// Parse splits raw into one LogStep per step, in the order given. Steps that
// never started get no lines. Lines without a timestamp stay with the line
// before them.
func Parse(raw []byte, steps []model.Step) []model.LogStep {
	out := make([]model.LogStep, len(steps))
	for i, s := range steps {
		out[i] = model.LogStep{Number: s.Number, Name: s.Name, Lines: []model.LogLine{}}
	}
	if len(steps) == 0 {
		return out
	}
	var timed []int
	for i, s := range steps {
		if s.StartedAt != nil {
			timed = append(timed, i)
		}
	}
	// end is a step's completion plus slack, or no bound while it has none.
	end := func(k int) (time.Time, bool) {
		c := steps[timed[k]].CompletedAt
		if c == nil {
			return time.Time{}, false
		}
		return c.Add(slack), true
	}
	contains := func(k int, t time.Time) bool {
		if t.Before(steps[timed[k]].StartedAt.Add(-slack)) {
			return false
		}
		e, ok := end(k)
		return !ok || !t.After(e)
	}

	text := strings.TrimPrefix(string(raw), "\uFEFF")
	text = strings.TrimSuffix(text, "\n")
	p := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		at, body, ok := split(line)
		if ok && len(timed) > 0 {
			cur := &out[timed[p]]
			if strings.HasPrefix(body, "##[group]Run ") && len(cur.Lines) > 0 {
				for k := p + 1; k < len(timed); k++ {
					if contains(k, at) {
						p = k
						break
					}
				}
			} else if e, bounded := end(p); bounded && at.After(e) {
				for k := p + 1; k < len(timed); k++ {
					if e2, b2 := end(k); !b2 || !e2.Before(at) {
						p = k
						break
					}
				}
			}
		}
		idx := 0
		if len(timed) > 0 {
			idx = timed[p]
		}
		k, t := kind(body)
		out[idx].Lines = append(out[idx].Lines, model.LogLine{N: len(out[idx].Lines) + 1, Kind: k, Text: t})
	}
	return out
}

// LevelRank orders annotation levels: errors, then warnings, then notices.
func LevelRank(level string) int {
	switch level {
	case "error":
		return 0
	case "warning":
		return 1
	case "notice":
		return 2
	}
	return 3
}

// Annotations returns one job's error, warning and notice lines, duplicates
// (same level, step and message) merged, sorted by level, step and line, and
// capped at PerJob; omitted counts the rest.
func Annotations(jobID int64, jobName string, steps []model.LogStep) (items []model.Annotation, omitted int) {
	items = []model.Annotation{}
	seen := map[string]bool{}
	for _, s := range steps {
		for _, l := range s.Lines {
			if LevelRank(l.Kind) > 2 {
				continue
			}
			key := l.Kind + "\x00" + l.Text
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, model.Annotation{Level: l.Kind, JobID: jobID, JobName: jobName,
				Step: s.Number, StepName: s.Name, Line: l.N, Message: l.Text})
		}
		clear(seen)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if ra, rb := LevelRank(a.Level), LevelRank(b.Level); ra != rb {
			return ra < rb
		}
		if a.Step != b.Step {
			return a.Step < b.Step
		}
		return a.Line < b.Line
	})
	if len(items) > PerJob {
		omitted = len(items) - PerJob
		items = items[:PerJob]
	}
	return items, omitted
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/joblog/ ./internal/model/ -count=1`
Expected: PASS.

- [ ] **Step 6: Check formatting and vet**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/`
Expected: no output from `gofmt`, and `go vet` passes.

- [ ] **Step 7: Commit**

```bash
git add internal/model/model.go internal/joblog/joblog.go internal/joblog/joblog_test.go
git commit -m "feat(joblog): split job logs into steps"
```

---

### Task 4: Workflow graph

**Files:**
- Create: `internal/wfgraph/wfgraph.go`
- Test: `internal/wfgraph/wfgraph_test.go`

**Interfaces:**
- Consumes: `model.GraphNode` (Task 3)
- Produces: `wfgraph.Job`, `wfgraph.Workflow`, `wfgraph.APIJob`, `wfgraph.ErrInvalid`, `wfgraph.ErrMismatch`, `wfgraph.Parse`, `wfgraph.Build` (Contracts, Go — workflow graph)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

`Parse` keeps file order by reading `jobs` as a `yaml.Node` and decodes each job body on its own, which resolves aliases and `<<` merge keys. `Build` sorts each node's `Needs` by key.

- [ ] **Step 1: Write the failing tests**

Create `internal/wfgraph/wfgraph_test.go`:

```go
package wfgraph

import (
	"errors"
	"reflect"
	"testing"

	"github.com/darkraise/ghr/internal/model"
)

const ciYAML = `
name: ci
on: [push, pull_request]
jobs:
  pick-runner:
    name: Pick runner
    runs-on: ubuntu-latest
  backend: &backend
    name: Backend tests
    needs: pick-runner
    runs-on: ubuntu-latest
  frontend-tests:
    name: Frontend tests (${{ matrix.shard }}/2)
    needs: [pick-runner]
    strategy:
      matrix:
        shard: [1, 2]
  e2e:
    name: E2E
    strategy: { matrix: { suite: [api] } }
  lint:
    <<: *backend
    name: Lint
  docs:
    needs: [backend]
    if: false
  release:
    needs: [docs, frontend-tests]
    uses: ./.github/workflows/release.yml
`

func TestParse(t *testing.T) {
	w, err := Parse([]byte(ciYAML))
	if err != nil {
		t.Fatal(err)
	}
	want := []Job{
		{Key: "pick-runner", Name: "Pick runner"},
		{Key: "backend", Name: "Backend tests", Needs: []string{"pick-runner"}},
		{Key: "frontend-tests", Name: "Frontend tests (${{ matrix.shard }}/2)", Needs: []string{"pick-runner"}, Matrix: true},
		{Key: "e2e", Name: "E2E", Matrix: true},
		{Key: "lint", Name: "Lint", Needs: []string{"pick-runner"}},
		{Key: "docs", Needs: []string{"backend"}},
		{Key: "release", Needs: []string{"docs", "frontend-tests"}, Uses: true},
	}
	if !reflect.DeepEqual(w.Jobs, want) {
		t.Fatalf("jobs\n%+v\nwant\n%+v", w.Jobs, want)
	}
}

func TestParseRefusesInvalidFiles(t *testing.T) {
	for name, src := range map[string]string{
		"not YAML":      "jobs: [",
		"no jobs":       "name: ci\non: push\n",
		"jobs a list":   "jobs: [a, b]\n",
		"unknown needs": "jobs:\n  a:\n    needs: b\n",
		"cycle":         "jobs:\n  a:\n    needs: c\n  b:\n    needs: a\n  c:\n    needs: [b]\n",
		"self cycle":    "jobs:\n  a:\n    needs: a\n",
		"bad needs":     "jobs:\n  a:\n    needs: {x: 1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(src)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err %v", err)
			}
		})
	}
}

func TestBuildLaysOutTheRun(t *testing.T) {
	w, err := Parse([]byte(ciYAML))
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := Build(w, []APIJob{
		{1001, "Pick runner"},
		{1002, "Backend tests"},
		{1003, "Frontend tests (1/2)"},
		{1004, "Frontend tests (2/2)"},
		{1005, "E2E (api)"},
		{1006, "release / publish"},
		{1007, "Lint"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.GraphNode{
		{Key: "pick-runner", Name: "Pick runner", Column: 0, Needs: []string{}, JobIDs: []int64{1001}},
		{Key: "e2e", Name: "E2E", Column: 0, Needs: []string{}, JobIDs: []int64{1005}, Group: true},
		{Key: "backend", Name: "Backend tests", Column: 1, Needs: []string{"pick-runner"}, JobIDs: []int64{1002}},
		{Key: "frontend-tests", Name: "Frontend tests (${{ matrix.shard }}/2)", Column: 1, Needs: []string{"pick-runner"}, JobIDs: []int64{1003, 1004}, Group: true},
		{Key: "lint", Name: "Lint", Column: 1, Needs: []string{"pick-runner"}, JobIDs: []int64{1007}},
		// docs never ran, so release needs what docs needed.
		{Key: "release", Name: "release", Column: 2, Needs: []string{"backend", "frontend-tests"}, JobIDs: []int64{1006}, Group: true},
	}
	if !reflect.DeepEqual(nodes, want) {
		t.Fatalf("nodes\n%+v\nwant\n%+v", nodes, want)
	}
}

func TestBuildMatching(t *testing.T) {
	for _, tc := range []struct {
		name string
		jobs []Job
		api  string
		want string // the key the API job lands on; "" means ErrMismatch
	}{
		{"expression stands for any text", []Job{{Key: "t", Name: "Test (${{ matrix.os }}, ${{ matrix.v }})"}}, "Test (linux, 1.26)", "t"},
		{"unexpanded name of a skipped matrix job", []Job{{Key: "t", Name: "Test (${{ matrix.os }})"}}, "Test (${{ matrix.os }})", "t"},
		{"matrix suffix", []Job{{Key: "e2e", Name: "E2E", Matrix: true}}, "E2E (api, chrome)", "e2e"},
		{"no suffix without a matrix", []Job{{Key: "b", Name: "Build"}}, "Build (x)", ""},
		{"reusable workflow caller", []Job{{Key: "rel", Uses: true}}, "rel / publish", "rel"},
		{"key when there is no name", []Job{{Key: "lint"}}, "lint", "lint"},
		{"longest literal prefix wins", []Job{{Key: "any", Name: "Test ${{ matrix.os }}"}, {Key: "linux", Name: "Test linux ${{ matrix.v }}"}}, "Test linux 1.26", "linux"},
		{"ties go to the earlier job", []Job{{Key: "a", Name: "Test ${{ matrix.x }}"}, {Key: "b", Name: "Test ${{ matrix.y }}"}}, "Test 1", "a"},
		{"exact text wins over a longer prefix", []Job{{Key: "a", Name: "Deploy ${{ matrix.env }}"}, {Key: "b", Name: "Deploy $${{ matrix.n }}"}}, "Deploy ${{ matrix.env }}", "a"},
		{"no workflow job matches", []Job{{Key: "a", Name: "Build"}}, "Deploy", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := Build(&Workflow{Jobs: tc.jobs}, []APIJob{{ID: 1, Name: tc.api}})
			if tc.want == "" {
				if !errors.Is(err, ErrMismatch) {
					t.Fatalf("err %v nodes %+v", err, nodes)
				}
				return
			}
			if err != nil || len(nodes) != 1 || nodes[0].Key != tc.want {
				t.Fatalf("nodes %+v err %v", nodes, err)
			}
		})
	}
}

func TestBuildRefusesAnInvalidWorkflow(t *testing.T) {
	w := &Workflow{Jobs: []Job{{Key: "a", Needs: []string{"b"}}, {Key: "b", Needs: []string{"a"}}}}
	if _, err := Build(w, []APIJob{{1, "a"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/wfgraph/ -count=1`
Expected: FAIL to compile: `undefined: Parse`.

- [ ] **Step 3: Write the implementation**

Create `internal/wfgraph/wfgraph.go`:

```go
// Package wfgraph reads a workflow file's job dependencies and lays the
// run's jobs out as a graph.
package wfgraph

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/darkraise/ghr/internal/model"
)

var (
	ErrInvalid  = errors.New("wfgraph: invalid workflow")
	ErrMismatch = errors.New("wfgraph: a job matches no workflow job")
)

// Job is one workflow job: Name is its YAML name, "" when it has none.
type Job struct {
	Key, Name    string
	Needs        []string
	Matrix, Uses bool
}

type Workflow struct {
	Jobs []Job
}

type APIJob struct {
	ID   int64
	Name string
}

// needsList takes needs as one job ID or a list of them.
type needsList []string

func (n *needsList) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind == yaml.ScalarNode {
		*n = []string{v.Value}
		return nil
	}
	var list []string
	if err := v.Decode(&list); err != nil {
		return err
	}
	*n = list
	return nil
}

type rawJob struct {
	Name     string    `yaml:"name"`
	Needs    needsList `yaml:"needs"`
	Strategy struct {
		Matrix any `yaml:"matrix"`
	} `yaml:"strategy"`
	Uses string `yaml:"uses"`
}

// Parse reads the jobs of a workflow file in file order. Each job body is
// decoded on its own, which resolves aliases and merge keys.
func Parse(data []byte) (*Workflow, error) {
	var doc struct {
		Jobs yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	jobs := &doc.Jobs
	for jobs.Kind == yaml.AliasNode {
		jobs = jobs.Alias
	}
	if jobs.Kind != yaml.MappingNode || len(jobs.Content) == 0 {
		return nil, fmt.Errorf("%w: no jobs", ErrInvalid)
	}
	w := &Workflow{}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		var raw rawJob
		if err := jobs.Content[i+1].Decode(&raw); err != nil {
			return nil, fmt.Errorf("%w: job %s: %v", ErrInvalid, jobs.Content[i].Value, err)
		}
		w.Jobs = append(w.Jobs, Job{Key: jobs.Content[i].Value, Name: raw.Name, Needs: raw.Needs,
			Matrix: raw.Strategy.Matrix != nil, Uses: raw.Uses != ""})
	}
	if err := validate(w); err != nil {
		return nil, err
	}
	return w, nil
}

// validate refuses a needs that names an unknown job or forms a cycle;
// GitHub refuses such files before a run exists.
func validate(w *Workflow) error {
	index := map[string]int{}
	for i, j := range w.Jobs {
		index[j.Key] = i
	}
	for _, j := range w.Jobs {
		for _, n := range j.Needs {
			if _, ok := index[n]; !ok {
				return fmt.Errorf("%w: job %s needs unknown job %s", ErrInvalid, j.Key, n)
			}
		}
	}
	const (
		unseen = iota
		visiting
		done
	)
	state := make([]int, len(w.Jobs))
	var visit func(i int) error
	visit = func(i int) error {
		switch state[i] {
		case visiting:
			return fmt.Errorf("%w: needs cycle through job %s", ErrInvalid, w.Jobs[i].Key)
		case done:
			return nil
		}
		state[i] = visiting
		for _, n := range w.Jobs[i].Needs {
			if err := visit(index[n]); err != nil {
				return err
			}
		}
		state[i] = done
		return nil
	}
	for i := range w.Jobs {
		if err := visit(i); err != nil {
			return err
		}
	}
	return nil
}

var exprRe = regexp.MustCompile(`\$\{\{.*?\}\}`)

type pattern struct {
	label  string
	prefix string
	re     *regexp.Regexp
}

// newPattern matches the names GitHub gives a workflow job's runs: each
// expression stands for any text, a matrix job without an expression in its
// name may get " (…)", and a reusable-workflow caller " / …".
func newPattern(j Job) pattern {
	label := j.Name
	if label == "" {
		label = j.Key
	}
	lits := exprRe.Split(label, -1)
	quoted := make([]string, len(lits))
	for i, l := range lits {
		quoted[i] = regexp.QuoteMeta(l)
	}
	expr := strings.Join(quoted, ".*?")
	if j.Matrix && len(lits) == 1 {
		expr += `(?: \(.*\))?`
	}
	if j.Uses {
		expr += `(?: / .*)?`
	}
	return pattern{label: label, prefix: lits[0], re: regexp.MustCompile(`(?s)^` + expr + `$`)}
}

// Build lays the run's jobs out per workflow job. A job goes to the workflow
// job whose name is its exact text, otherwise to the matching pattern with
// the longest literal prefix, ties to the earlier job. Workflow jobs with no
// run are dropped and their needs pass through to what they need.
func Build(w *Workflow, jobs []APIJob) ([]model.GraphNode, error) {
	if err := validate(w); err != nil {
		return nil, err
	}
	pats := make([]pattern, len(w.Jobs))
	for i, j := range w.Jobs {
		pats[i] = newPattern(j)
	}
	ids := make([][]int64, len(w.Jobs))
	for _, a := range jobs {
		best := -1
		for i, p := range pats {
			if p.label == a.Name {
				best = i
				break
			}
		}
		if best < 0 {
			for i, p := range pats {
				if p.re.MatchString(a.Name) && (best < 0 || len(p.prefix) > len(pats[best].prefix)) {
					best = i
				}
			}
		}
		if best < 0 {
			return nil, fmt.Errorf("%w: %s", ErrMismatch, a.Name)
		}
		ids[best] = append(ids[best], a.ID)
	}

	index := map[string]int{}
	for i, j := range w.Jobs {
		index[j.Key] = i
	}
	needs := make([][]string, len(w.Jobs))
	resolved := make([]bool, len(w.Jobs))
	var effective func(i int) []string
	effective = func(i int) []string {
		if resolved[i] {
			return needs[i]
		}
		out := []string{}
		for _, n := range w.Jobs[i].Needs {
			k := index[n]
			if len(ids[k]) > 0 {
				out = append(out, n)
			} else {
				out = append(out, effective(k)...)
			}
		}
		slices.Sort(out)
		out = slices.Compact(out)
		needs[i], resolved[i] = out, true
		return out
	}
	cols := map[string]int{}
	var column func(i int) int
	column = func(i int) int {
		if c, ok := cols[w.Jobs[i].Key]; ok {
			return c
		}
		c := 0
		for _, n := range effective(i) {
			c = max(c, column(index[n])+1)
		}
		cols[w.Jobs[i].Key] = c
		return c
	}

	nodes := []model.GraphNode{}
	for i, j := range w.Jobs {
		if len(ids[i]) == 0 {
			continue
		}
		name := j.Name
		if name == "" {
			name = j.Key
		}
		nodes = append(nodes, model.GraphNode{Key: j.Key, Name: name, Column: column(i), Needs: effective(i),
			JobIDs: ids[i], Group: len(ids[i]) > 1 || j.Matrix || j.Uses})
	}
	sort.SliceStable(nodes, func(a, b int) bool { return nodes[a].Column < nodes[b].Column })
	return nodes, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/wfgraph/ -count=1`
Expected: PASS.

- [ ] **Step 5: Check formatting and vet**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/`
Expected: no output from `gofmt`, and `go vet` passes.

- [ ] **Step 6: Commit**

```bash
git add internal/wfgraph/wfgraph.go internal/wfgraph/wfgraph_test.go
git commit -m "feat(wfgraph): lay out run jobs from the workflow"
```

---

### Task 5: Daemon GitHub interface and fake

**Files:**
- Modify: `internal/daemon/backend.go:45-56` (the `GitHub` interface)
- Modify: `internal/daemon/backend_test.go:93` (embed `fakeRuns` in `fakeGH`)
- Create: `internal/daemon/runs_fake_test.go`

**Interfaces:**
- Consumes: the Task 1 and Task 2 client methods and `github.Artifact`, `github.ErrTooLarge` (Contracts, Go — GitHub client)
- Produces: the daemon `GitHub` interface additions (Contracts, Go — daemon); `fakeRuns` and its helpers (Contracts, Go — daemon test fakes)

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Add the run details calls to the interface**

In `internal/daemon/backend.go`, add these eight lines to the `GitHub` interface after `ListUserRepos(ctx context.Context) ([]github.UserRepo, error)` (line 55), so the closing brace follows them:

```go
	GetRun(ctx context.Context, repo string, id int64) (*github.Run, error)
	GetRunAttempt(ctx context.Context, repo string, id int64, attempt int) (*github.Run, error)
	ListAttemptJobs(ctx context.Context, repo string, id int64, attempt int) ([]github.Job, error)
	GetJob(ctx context.Context, repo string, jobID int64) (*github.Job, error)
	ListArtifacts(ctx context.Context, repo string, runID int64) ([]github.Artifact, error)
	GetContent(ctx context.Context, repo, path, ref string) ([]byte, error)
	JobLog(ctx context.Context, repo string, jobID int64, max int64) ([]byte, error)
	ArtifactURL(ctx context.Context, repo string, id int64) (string, error)
```

- [ ] **Step 2: Run the daemon checks to verify the fake no longer fits**

Run: `go vet ./internal/daemon/`
Expected: FAIL with `*fakeGH does not implement GitHub (missing method ArtifactURL)` (the real `*github.Client` already has the methods from Tasks 1 and 2, so only the test build fails).

- [ ] **Step 3: Write the fake and embed it**

Create `internal/daemon/runs_fake_test.go`:

```go
package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/darkraise/ghr/internal/github"
)

// fakeRuns answers the run details calls; fakeGH embeds it. Each call is
// recorded as "<Method> <repo> <id>", and a missing entry answers 404.
type fakeRuns struct {
	rmu         sync.Mutex
	runByID     map[int64]github.Run        // GetRun
	runErr      error                       // returned by GetRun when set
	runWait     chan struct{}               // when set, GetRun waits for it or the context
	attemptRuns map[string]github.Run       // GetRunAttempt, by attemptKey
	attemptJobs map[string][]github.Job     // ListAttemptJobs, by attemptKey
	jobByID     map[int64]github.Job        // GetJob
	artifacts   map[int64][]github.Artifact // ListArtifacts, by run ID
	content     map[string][]byte           // GetContent, by contentKey
	contentErr  error                       // returned by GetContent when set
	logs        map[int64][]byte            // JobLog, by job ID
	logErr      map[int64]error             // returned by JobLog for that job
	logWait     chan struct{}               // when set, JobLog waits for it or the context
	artURL      string                      // returned by ArtifactURL
	artErr      error                       // returned by ArtifactURL when set
	calls       []string
	logInFlight int
	logPeak     int
}

func attemptKey(id int64, attempt int) string { return fmt.Sprintf("%d/%d", id, attempt) }

func contentKey(repo, path, ref string) string { return repo + ":" + ref + ":" + path }

func ghNotFound() error {
	return &github.APIError{Status: 404, Kind: github.ErrNotFound, Message: "Not Found"}
}

func (f *fakeRuns) record(method, repo string, id any) {
	f.rmu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("%s %s %v", method, repo, id))
	f.rmu.Unlock()
}

// count is how many recorded calls are prefix or start with prefix and a
// space, so "GetRun" does not count GetRunAttempt calls.
func (f *fakeRuns) count(prefix string) int {
	f.rmu.Lock()
	defer f.rmu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == prefix || strings.HasPrefix(c, prefix+" ") {
			n++
		}
	}
	return n
}

func (f *fakeRuns) GetRun(ctx context.Context, repo string, id int64) (*github.Run, error) {
	f.record("GetRun", repo, id)
	if f.runWait != nil {
		select {
		case <-f.runWait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.rmu.Lock()
	defer f.rmu.Unlock()
	if f.runErr != nil {
		return nil, f.runErr
	}
	r, ok := f.runByID[id]
	if !ok {
		return nil, ghNotFound()
	}
	return &r, nil
}

func (f *fakeRuns) GetRunAttempt(ctx context.Context, repo string, id int64, attempt int) (*github.Run, error) {
	f.record("GetRunAttempt", repo, attemptKey(id, attempt))
	f.rmu.Lock()
	defer f.rmu.Unlock()
	r, ok := f.attemptRuns[attemptKey(id, attempt)]
	if !ok {
		return nil, ghNotFound()
	}
	return &r, nil
}

func (f *fakeRuns) ListAttemptJobs(ctx context.Context, repo string, id int64, attempt int) ([]github.Job, error) {
	f.record("ListAttemptJobs", repo, attemptKey(id, attempt))
	f.rmu.Lock()
	defer f.rmu.Unlock()
	return f.attemptJobs[attemptKey(id, attempt)], nil
}

func (f *fakeRuns) GetJob(ctx context.Context, repo string, jobID int64) (*github.Job, error) {
	f.record("GetJob", repo, jobID)
	f.rmu.Lock()
	defer f.rmu.Unlock()
	j, ok := f.jobByID[jobID]
	if !ok {
		return nil, ghNotFound()
	}
	return &j, nil
}

func (f *fakeRuns) ListArtifacts(ctx context.Context, repo string, runID int64) ([]github.Artifact, error) {
	f.record("ListArtifacts", repo, runID)
	f.rmu.Lock()
	defer f.rmu.Unlock()
	return f.artifacts[runID], nil
}

func (f *fakeRuns) GetContent(ctx context.Context, repo, path, ref string) ([]byte, error) {
	f.record("GetContent", repo, ref+":"+path)
	f.rmu.Lock()
	defer f.rmu.Unlock()
	if f.contentErr != nil {
		return nil, f.contentErr
	}
	data, ok := f.content[contentKey(repo, path, ref)]
	if !ok {
		return nil, ghNotFound()
	}
	return data, nil
}

func (f *fakeRuns) JobLog(ctx context.Context, repo string, jobID int64, limit int64) ([]byte, error) {
	f.record("JobLog", repo, jobID)
	f.rmu.Lock()
	f.logInFlight++
	f.logPeak = max(f.logPeak, f.logInFlight)
	wait := f.logWait
	f.rmu.Unlock()
	defer func() {
		f.rmu.Lock()
		f.logInFlight--
		f.rmu.Unlock()
	}()
	if wait != nil {
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.rmu.Lock()
	defer f.rmu.Unlock()
	if err := f.logErr[jobID]; err != nil {
		return nil, err
	}
	data, ok := f.logs[jobID]
	if !ok {
		return nil, ghNotFound()
	}
	if int64(len(data)) > limit {
		return nil, github.ErrTooLarge
	}
	return data, nil
}

func (f *fakeRuns) ArtifactURL(ctx context.Context, repo string, id int64) (string, error) {
	f.record("ArtifactURL", repo, id)
	f.rmu.Lock()
	defer f.rmu.Unlock()
	return f.artURL, f.artErr
}
```

In `internal/daemon/backend_test.go`, make `fakeRuns` the first field of `fakeGH` (line 93):

```go
type fakeGH struct {
	fakeRuns
	repos    map[string]*github.Repository
```

`fakeRuns`' methods have pointer receivers, so `*fakeGH` gains them through the embedded value; none of its field or method names collide with `fakeGH`'s.

- [ ] **Step 4: Run the daemon tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ -count=1`
Expected: PASS (`ok  github.com/darkraise/ghr/internal/daemon`).

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/`
Expected: no output from `gofmt`, `go vet` passes.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/backend.go internal/daemon/backend_test.go internal/daemon/runs_fake_test.go
git commit -m "test(daemon): fake the run details GitHub calls"
```

---

### Task 6: Workflow file cache and graph note

**Files:**
- Create: `internal/daemon/rungraph.go` (also holds `lru`, the bounded map Tasks 8 and 9 reuse)
- Modify: `internal/daemon/backend.go:111` (a `wfFiles wfCache` field after `actions actionsCache`; line 103 before Task 5)
- Test: `internal/daemon/rungraph_test.go`

**Interfaces:**
- Consumes: `wfgraph.Parse`, `wfgraph.Build`, `wfgraph.APIJob`, `wfgraph.ErrMismatch` (Contracts, Go — workflow graph); `GetContent` on the daemon `GitHub` interface and `fakeRuns.content`, `contentErr`, `contentKey`, `count` (Task 5)
- Produces: `lru`, `newLRU`, `wfCache`, `runGraph`, the note constants and `ciPath`, `ciYAML`, `ciJobs` test helpers (Contracts, Go — daemon internals)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/rungraph_test.go`:

```go
package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
)

const ciPath = ".github/workflows/ci.yml"

const ciYAML = `
jobs:
  build:
    name: Build
  test:
    needs: build
`

func ciJobs() []github.Job {
	return []github.Job{{ID: 1, Name: "Build"}, {ID: 2, Name: "test"}}
}

func TestLRUBoundsEntriesAndSize(t *testing.T) {
	c := newLRU[string, int](2, 10)
	c.add("a", 1, 4)
	c.add("b", 2, 4)
	c.get("a")
	c.add("c", 3, 4)
	if _, ok := c.get("b"); ok || c.len() != 2 {
		t.Fatalf("b should be evicted as least recently used, len %d", c.len())
	}
	c.add("d", 4, 8)
	if _, ok := c.get("a"); ok || c.len() != 1 || c.size != 8 {
		t.Fatalf("size bound: len %d size %d", c.len(), c.size)
	}
	c.add("e", 5, 11)
	if _, ok := c.get("e"); ok || c.len() != 1 {
		t.Fatalf("an entry above maxSize must not be kept, len %d", c.len())
	}
}

func TestRunGraphNotes(t *testing.T) {
	for _, tc := range []struct {
		name, path, yaml string
		err              error
		jobs             []github.Job
		note             string
	}{
		{"dynamic workflow", "dynamic/dependabot/dependabot-updates", ciYAML, nil, ciJobs(), "This run has no workflow file to read."},
		{"missing file", ciPath, "", nil, ciJobs(), "This run has no workflow file to read."},
		{"no Contents permission", ciPath, "", &github.APIError{Status: 403, Kind: github.ErrAuth, Permissions: "contents=read"}, ciJobs(),
			"Add Contents: read to the token to see which jobs depend on which."},
		{"job not in the file", ciPath, ciYAML, nil, append(ciJobs(), github.Job{ID: 3, Name: "Deploy"}),
			"The workflow file at this commit does not match the run's jobs."},
		{"invalid YAML", ciPath, "jobs: [", nil, ciJobs(), "ghr could not read .github/workflows/ci.yml."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh := newBackend(t)
			gh.contentErr = tc.err
			if tc.yaml != "" {
				gh.content = map[string][]byte{contentKey("darkmem", tc.path, "abc"): []byte(tc.yaml)}
			}
			g, note, err := b.runGraph(context.Background(), "darkmem", "abc", tc.path, tc.jobs)
			if err != nil || g != nil || note != tc.note {
				t.Fatalf("graph %+v note %q err %v", g, note, err)
			}
		})
	}
}

func TestRunGraphBuildsFromTheFile(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
	g, note, err := b.runGraph(context.Background(), "darkmem", "abc", ciPath, ciJobs())
	if err != nil || note != "" || g == nil || len(g.Nodes) != 2 {
		t.Fatalf("graph %+v note %q err %v", g, note, err)
	}
	if n := g.Nodes[1]; n.Key != "test" || n.Column != 1 || fmt.Sprint(n.Needs) != "[build]" || fmt.Sprint(n.JobIDs) != "[2]" {
		t.Fatalf("node %+v", n)
	}
}

func TestRunGraphFailsOnRateLimit(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.contentErr = &github.APIError{Status: 429, Kind: github.ErrRateLimit}
	if _, _, err := b.runGraph(context.Background(), "darkmem", "abc", ciPath, ciJobs()); !github.IsKind(err, github.ErrRateLimit) {
		t.Fatalf("err %v", err)
	}
}

func TestWorkflowFileCache(t *testing.T) {
	ctx := context.Background()
	t.Run("keeps bytes", func(t *testing.T) {
		b, _, gh := newBackend(t)
		gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
		for range 2 {
			if _, note, _ := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs()); note != "" {
				t.Fatalf("note %q", note)
			}
		}
		if n := gh.count("GetContent"); n != 1 {
			t.Fatalf("%d GetContent calls, want 1", n)
		}
	})
	t.Run("keeps not found for 10 minutes", func(t *testing.T) {
		b, _, gh := newBackend(t)
		clock := frozenClock(b, actionsT0)
		b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs())
		gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
		clock.Store(actionsT0.Add(wfMissingTTL - time.Second).UnixNano())
		if _, note, _ := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs()); note != noteNoFile || gh.count("GetContent") != 1 {
			t.Fatalf("note %q, %d calls", note, gh.count("GetContent"))
		}
		clock.Store(actionsT0.Add(wfMissingTTL).UnixNano())
		if _, note, _ := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs()); note != "" || gh.count("GetContent") != 2 {
			t.Fatalf("note %q, %d calls", note, gh.count("GetContent"))
		}
	})
	t.Run("never keeps a 403", func(t *testing.T) {
		b, _, gh := newBackend(t)
		gh.contentErr = &github.APIError{Status: 403, Kind: github.ErrAuth}
		b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs())
		gh.contentErr = nil
		gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
		if g, note, _ := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs()); g == nil || note != "" {
			t.Fatalf("graph %+v note %q", g, note)
		}
	})
	t.Run("holds 64 files", func(t *testing.T) {
		b, _, gh := newBackend(t)
		gh.content = map[string][]byte{}
		for i := range wfFileEntries + 1 {
			sha := fmt.Sprintf("sha%d", i)
			gh.content[contentKey("darkmem", ciPath, sha)] = []byte(ciYAML)
			b.runGraph(ctx, "darkmem", sha, ciPath, ciJobs())
		}
		b.runGraph(ctx, "darkmem", "sha0", ciPath, ciJobs())
		if n := gh.count("GetContent"); n != wfFileEntries+2 {
			t.Fatalf("%d GetContent calls, want %d: the oldest file should have been evicted", n, wfFileEntries+2)
		}
	})
}
```

`TestRunGraphBuildsFromTheFile` relies on Task 4's `Build`: `test` needs `build`, so it sits in column 1 with needs `[build]` and the API job 2.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run 'LRU|RunGraph|WorkflowFile' -count=1`
Expected: FAIL to build with `undefined: newLRU` and `b.runGraph undefined`.

- [ ] **Step 3: Write the cache and the graph builder**

Create `internal/daemon/rungraph.go`:

```go
package daemon

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/wfgraph"
)

const (
	noteNoPermission = "Add Contents: read to the token to see which jobs depend on which."
	noteNoFile       = "This run has no workflow file to read."
	noteMismatch     = "The workflow file at this commit does not match the run's jobs."
	noteInvalid      = "ghr could not read %s."

	wfFileEntries = 64
	wfMissingTTL  = 10 * time.Minute
)

// wfFile is a workflow file's bytes, or the time GitHub answered 404 for it.
type wfFile struct {
	data      []byte
	missingAt time.Time
}

// wfCache keeps workflow files by repository, commit and path. A file at a
// commit never changes, so bytes do not expire; only "not found" does, and
// a 403, 429 or 5xx is never kept, so granting the permission shows the
// graph on the next call.
type wfCache struct {
	mu    sync.Mutex
	files *lru[string, wfFile]
}

func (b *Backend) workflowFile(ctx context.Context, repo, sha, path string) ([]byte, error) {
	c := &b.wfFiles
	key := strings.ToLower(repo) + "\x00" + sha + "\x00" + path
	c.mu.Lock()
	if c.files == nil {
		c.files = newLRU[string, wfFile](wfFileEntries, 0)
	}
	f, ok := c.files.get(key)
	c.mu.Unlock()
	if ok && f.missingAt.IsZero() {
		return f.data, nil
	}
	if ok && b.now().Sub(f.missingAt) < wfMissingTTL {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound, Message: "Not Found"}
	}
	data, err := b.GH.GetContent(ctx, repo, path, sha)
	switch {
	case err == nil:
		f = wfFile{data: data}
	case github.IsKind(err, github.ErrNotFound):
		f = wfFile{missingAt: b.now()}
	default:
		return nil, err
	}
	c.mu.Lock()
	c.files.add(key, f, 0)
	c.mu.Unlock()
	return data, err
}

// runGraph builds the job graph of a run from its workflow file at sha. When
// no graph can be drawn it returns the note that says why; an error means
// the run details call fails as a whole.
func (b *Backend) runGraph(ctx context.Context, repo, sha, path string, jobs []github.Job) (*model.RunGraph, string, error) {
	if !strings.HasPrefix(path, ".github/workflows/") {
		return nil, noteNoFile, nil
	}
	data, err := b.workflowFile(ctx, repo, sha, path)
	var ge *github.APIError
	switch {
	case github.IsKind(err, github.ErrNotFound):
		return nil, noteNoFile, nil
	case errors.As(err, &ge) && ge.Kind == github.ErrAuth && ge.Status == 403:
		return nil, noteNoPermission, nil
	case err != nil:
		return nil, "", err
	}
	w, err := wfgraph.Parse(data)
	if err != nil {
		return nil, fmt.Sprintf(noteInvalid, path), nil
	}
	in := make([]wfgraph.APIJob, 0, len(jobs))
	for _, j := range jobs {
		in = append(in, wfgraph.APIJob{ID: j.ID, Name: j.Name})
	}
	nodes, err := wfgraph.Build(w, in)
	switch {
	case errors.Is(err, wfgraph.ErrMismatch):
		return nil, noteMismatch, nil
	case err != nil:
		return nil, fmt.Sprintf(noteInvalid, path), nil
	}
	return &model.RunGraph{Nodes: nodes}, "", nil
}

// lru is a map bounded by entry count and, when maxSize is above 0, by the
// total of the sizes given to add; the least recently used entries go
// first. Callers hold their own lock.
type lru[K comparable, V any] struct {
	maxEntries int
	maxSize    int64
	size       int64
	order      *list.List // front is the most recently used
	items      map[K]*list.Element
}

type lruItem[K comparable, V any] struct {
	key  K
	val  V
	size int64
}

func newLRU[K comparable, V any](maxEntries int, maxSize int64) *lru[K, V] {
	return &lru[K, V]{maxEntries: maxEntries, maxSize: maxSize, order: list.New(), items: map[K]*list.Element{}}
}

func (c *lru[K, V]) get(k K) (V, bool) {
	e, ok := c.items[k]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*lruItem[K, V]).val, true
}

// add stores v under k. An entry larger than maxSize on its own is not kept.
func (c *lru[K, V]) add(k K, v V, size int64) {
	c.remove(k)
	if c.maxSize > 0 && size > c.maxSize {
		return
	}
	c.items[k] = c.order.PushFront(&lruItem[K, V]{key: k, val: v, size: size})
	c.size += size
	for c.order.Len() > c.maxEntries || (c.maxSize > 0 && c.size > c.maxSize) {
		c.remove(c.order.Back().Value.(*lruItem[K, V]).key)
	}
}

func (c *lru[K, V]) remove(k K) {
	e, ok := c.items[k]
	if !ok {
		return
	}
	c.order.Remove(e)
	delete(c.items, k)
	c.size -= e.Value.(*lruItem[K, V]).size
}

func (c *lru[K, V]) len() int { return c.order.Len() }
```

In `internal/daemon/backend.go`, add the field after `actions actionsCache` (line 111 once Task 5 has run), so the struct ends:

```go
	actions actionsCache
	wfFiles wfCache
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/ -run 'LRU|RunGraph|WorkflowFile' -count=1 -v`
Expected: PASS for `TestLRUBoundsEntriesAndSize`, `TestRunGraphNotes` (five rows), `TestRunGraphBuildsFromTheFile`, `TestRunGraphFailsOnRateLimit`, `TestWorkflowFileCache` (four subtests).

- [ ] **Step 5: Format, vet and run the package**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/ && timeout 300 go test ./internal/daemon/ -count=1`
Expected: no `gofmt` output; vet and tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/rungraph.go internal/daemon/rungraph_test.go internal/daemon/backend.go
git commit -m "feat(daemon): build the run graph from the workflow"
```

---

### Task 7: Run details and cache

**Files:**
- Create: `internal/daemon/rundetail.go`
- Modify: `internal/daemon/backend.go:112` (a `details detailCache` field after `wfFiles wfCache`) and `:337-342` (`fetchSteps` uses `modelSteps`; lines 328-333 before Tasks 5 and 6)
- Test: `internal/daemon/rundetail_test.go`

**Interfaces:**
- Consumes: `github.Run`'s new fields and `github.PullRequest` (Task 1); the model types (Task 3); `runGraph`, `ciPath`, `ciYAML` (Task 6); `fakeRuns` (Task 5); `actionsErr`, `githubErr`, `frozenClock`, `actionsT0`, `apiStatus` (existing)
- Produces: `(*Backend).RunDetail` (Contracts, Go — daemon); `coveredName`, `detailErr`, `ghrJobKey`, `ghrJobs`, `modelSteps`, `runJob`, `detailDeadline`, `detailRun` (Contracts, Go — daemon internals)

**Items:** 1, 6

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/rundetail_test.go`:

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func setDetailDeadline(t *testing.T, d time.Duration) {
	old := detailDeadline
	detailDeadline = d
	t.Cleanup(func() { detailDeadline = old })
}

// detailRun gives gh run 102 of darkmem: attempt 2 of 2, two jobs per
// attempt, one artifact and the ci.yml of ciYAML.
func detailRun(gh *fakeGH, status string) {
	start := actionsT0.Add(-10 * time.Minute)
	run := github.Run{ID: 102, Name: "ci", DisplayTitle: "Fix the cache key", RunNumber: 42, HeadBranch: "fix/cache-key",
		Event: "pull_request", Actor: github.Account{Login: "octo-dev"}, Status: status, Conclusion: "failure",
		CreatedAt: start.Add(-time.Minute), RunStartedAt: &start, UpdatedAt: start.Add(4 * time.Minute),
		HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/102", HeadSHA: "abc", Path: ciPath, RunAttempt: 2,
		PullRequests: []github.PullRequest{{Number: 87}}}
	first := run
	first.RunAttempt, first.Conclusion = 1, "success"
	jobs := func(base int64) []github.Job {
		return []github.Job{
			{ID: base + 1, RunID: 102, Name: "Build", Status: "completed", Conclusion: "success", RunnerName: "ghr-darkmem-a1b2c3",
				Labels: []string{"self-hosted"}, Steps: []github.Step{{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success"}}},
			{ID: base + 2, RunID: 102, Name: "test", Status: "completed", Conclusion: "failure"},
		}
	}
	gh.runByID = map[int64]github.Run{102: run}
	gh.jobs = map[int64][]github.Job{102: jobs(2000)}
	gh.attemptRuns = map[string]github.Run{attemptKey(102, 1): first}
	gh.attemptJobs = map[string][]github.Job{attemptKey(102, 1): jobs(1000)}
	gh.artifacts = map[int64][]github.Artifact{102: {{ID: 11, Name: "dist", SizeInBytes: 2400000}}}
	gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
}

func TestRunDetailAssemblesTheLatestAttempt(t *testing.T) {
	b, m, gh := newBackend(t)
	frozenClock(b, actionsT0)
	detailRun(gh, "completed")
	if err := b.Hist.Append(model.HistoryEntry{ID: "h", Repo: "DarkMem", RunID: 102, JobName: "Build", Conclusion: "success",
		StartedAt: actionsT0.Add(-9 * time.Minute), FinishedAt: actionsT0.Add(-8 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	m.pending = []model.HistoryEntry{{ID: "p", Repo: "darkmem", RunID: 999, JobName: "test"}}

	d, err := b.RunDetail(context.Background(), "DarkMem", 102, 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Repo != "darkmem" || d.Watched || d.Attempt != 2 || d.Attempts != 2 || d.Title != "Fix the cache key" || d.Workflow != "ci" ||
		d.Branch != "fix/cache-key" || d.Actor != "octo-dev" || d.HeadSHA != "abc" || fmt.Sprint(d.PullRequests) != "[87]" ||
		!d.StartedAt.Equal(actionsT0.Add(-10*time.Minute)) || !d.FetchedAt.Equal(actionsT0) {
		t.Fatalf("detail %+v", d)
	}
	if len(d.Jobs) != 2 || d.Jobs[0].ID != 2001 || !d.Jobs[0].GHR || d.Jobs[1].GHR || d.Jobs[1].Labels == nil || d.Jobs[1].Steps == nil ||
		len(d.Jobs[0].Steps) != 1 {
		t.Fatalf("jobs %+v", d.Jobs)
	}
	if d.Graph == nil || len(d.Graph.Nodes) != 2 || d.GraphNote != "" {
		t.Fatalf("graph %+v note %q", d.Graph, d.GraphNote)
	}
	if len(d.Artifacts) != 1 || d.Artifacts[0].SizeBytes != 2400000 {
		t.Fatalf("artifacts %+v", d.Artifacts)
	}
	if gh.count("GetRunAttempt") != 0 || len(gh.jobCalls) != 1 {
		t.Fatalf("calls %v, ListJobs %v", gh.calls, gh.jobCalls)
	}
}

func TestRunDetailEarlierAttempt(t *testing.T) {
	b, _, gh := newBackend(t)
	detailRun(gh, "completed")
	d, err := b.RunDetail(context.Background(), "darkmem", 102, 1)
	if err != nil {
		t.Fatal(err)
	}
	if d.Attempt != 1 || d.Attempts != 2 || d.Conclusion != "success" || d.Jobs[0].ID != 1001 || len(gh.jobCalls) != 0 {
		t.Fatalf("detail %+v, ListJobs %v", d, gh.jobCalls)
	}
	if gh.count("GetRunAttempt darkmem 102/1") != 1 || gh.count("ListAttemptJobs darkmem 102/1") != 1 {
		t.Fatalf("calls %v", gh.calls)
	}
}

func TestRunDetailRepoAndAttemptBounds(t *testing.T) {
	b, _, gh := newBackend(t)
	detailRun(gh, "completed")
	ctx := context.Background()
	var ae *api.Error
	if _, err := b.RunDetail(ctx, "nope", 102, 0); !errors.As(err, &ae) || ae.Status != 404 || ae.Msg != "repo nope is not configured or watched" {
		t.Fatalf("uncovered: %v", err)
	}
	if len(gh.calls) != 0 {
		t.Fatalf("an uncovered repository called GitHub: %v", gh.calls)
	}
	if _, err := b.RunDetail(ctx, "darkmem", 102, 3); !errors.As(err, &ae) || ae.Status != 400 || ae.Msg != "run 102 has no attempt 3" {
		t.Fatalf("attempt 3: %v", err)
	}
	if _, err := b.Store.Update(func(c *config.Config) error { c.WatchRepos = []string{"Docs"}; return nil }); err != nil {
		t.Fatal(err)
	}
	gh.content = map[string][]byte{contentKey("Docs", ciPath, "abc"): []byte(ciYAML)}
	d, err := b.RunDetail(ctx, "docs", 102, 0)
	if err != nil || d.Repo != "Docs" || !d.Watched || gh.count("GetRun Docs 102") != 1 {
		t.Fatalf("watched: %+v err %v calls %v", d, err, gh.calls)
	}
}

func TestRunDetailErrors(t *testing.T) {
	retry := actionsT0.Add(30 * time.Minute)
	for _, tc := range []struct {
		name   string
		err    error
		status int
		msg    string
	}{
		{"missing permission", &github.APIError{Status: 403, Kind: github.ErrAuth, Permissions: "actions=read"}, 403,
			"token lacks actions=read on darkraise/darkmem: grant it in the PAT settings"},
		{"not found", &github.APIError{Status: 404, Kind: github.ErrNotFound}, 404, "not found on GitHub"},
		{"rate limit", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: retry}, 429, "GitHub rate limit; API calls are paused"},
		{"server", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "Bad Gateway"}, 502, "github: 502 Bad Gateway"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh := newBackend(t)
			gh.runErr = tc.err
			_, err := b.RunDetail(context.Background(), "darkmem", 102, 0)
			var ae *api.Error
			if !errors.As(err, &ae) || ae.Status != tc.status || ae.Msg != tc.msg {
				t.Fatalf("got %#v", err)
			}
			if tc.status == 429 && !ae.RetryAt.Equal(retry) {
				t.Fatalf("retry at %v", ae.RetryAt)
			}
		})
	}
}

func TestRunDetailRefusesWhileDegraded(t *testing.T) {
	b, m, gh := newBackend(t)
	m.degraded = "401 Bad credentials"
	_, err := b.RunDetail(context.Background(), "darkmem", 102, 0)
	if apiStatus(err) != 503 || len(gh.calls) != 0 {
		t.Fatalf("err %v calls %v", err, gh.calls)
	}
}

func TestRunDetailDeadlineOutlivesTheRequest(t *testing.T) {
	setDetailDeadline(t, 50*time.Millisecond)
	b, _, gh := newBackend(t)
	detailRun(gh, "completed")
	gh.runWait = make(chan struct{}) // never closed
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := b.RunDetail(ctx, "darkmem", 102, 0)
	var ae *api.Error
	if !errors.As(err, &ae) || ae.Status != 504 || ae.Msg != "GitHub did not answer in time" {
		t.Fatalf("err %#v", err)
	}
}

func TestRunDetailLatestSharesAnEntryWithItsNumber(t *testing.T) {
	for _, order := range [][]int{{0, 2}, {2, 0}} {
		b, _, gh := newBackend(t)
		detailRun(gh, "completed")
		for _, a := range order {
			if _, err := b.RunDetail(context.Background(), "darkmem", 102, a); err != nil {
				t.Fatal(err)
			}
		}
		if n := gh.count("GetRun"); n != 1 {
			t.Fatalf("order %v: %d GetRun calls, want 1", order, n)
		}
	}
}

func TestRunDetailCacheLifetime(t *testing.T) {
	for _, tc := range []struct {
		status string
		ttl    time.Duration
	}{{"in_progress", detailActiveTTL}, {"completed", detailDoneTTL}} {
		b, _, gh := newBackend(t)
		clock := frozenClock(b, actionsT0)
		detailRun(gh, tc.status)
		for i, step := range []struct {
			at   time.Duration
			want int
		}{{0, 1}, {tc.ttl - time.Millisecond, 1}, {tc.ttl, 2}} {
			clock.Store(actionsT0.Add(step.at).UnixNano())
			if _, err := b.RunDetail(context.Background(), "darkmem", 102, 0); err != nil || gh.count("GetRun") != step.want {
				t.Fatalf("%s step %d: %d calls, want %d, err %v", tc.status, i, gh.count("GetRun"), step.want, err)
			}
		}
	}
}

func TestRunDetailCacheHolds64Entries(t *testing.T) {
	b, _, gh := newBackend(t)
	clock := frozenClock(b, actionsT0)
	detailRun(gh, "completed")
	run := gh.runByID[102]
	run.RunAttempt = 1
	for id := int64(1); id <= detailEntries; id++ {
		r := run
		r.ID = id
		gh.runByID[id] = r
	}
	ctx := context.Background()
	for id := int64(1); id <= detailEntries; id++ {
		clock.Store(actionsT0.Add(time.Duration(id) * time.Millisecond).UnixNano())
		if _, err := b.RunDetail(ctx, "darkmem", id, 1); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(b.details.entries); n != detailEntries {
		t.Fatalf("%d entries, want %d", n, detailEntries)
	}
	if _, ok := b.details.entries[detailKey{repo: "darkmem", id: 1, attempt: 1}]; ok {
		t.Fatal("the oldest entry was kept")
	}
	if _, err := b.RunDetail(ctx, "darkmem", detailEntries, 1); err != nil || gh.count("GetRun darkmem 64") != 1 {
		t.Fatalf("the newest entry was not kept: err %v, calls %d", err, gh.count("GetRun darkmem 64"))
	}
}

func TestRunDetailCallersShareOneFetch(t *testing.T) {
	b, _, gh := newBackend(t)
	detailRun(gh, "completed")
	gh.runWait = make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := b.RunDetail(context.Background(), "darkmem", 102, 0)
			done <- err
		}()
	}
	deadline := time.Now().Add(3 * time.Second)
	for gh.count("GetRun") < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(gh.runWait)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if n := gh.count("GetRun"); n != 1 {
		t.Fatalf("%d GetRun calls, want 1", n)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run RunDetail -count=1`
Expected: FAIL to build with `b.RunDetail undefined` and `undefined: detailDeadline`.

- [ ] **Step 3: Write the run details**

Create `internal/daemon/rundetail.go`:

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

const (
	detailActiveTTL = 15 * time.Second
	detailDoneTTL   = 60 * time.Second
	detailEntries   = 64
)

// detailDeadline bounds one run details fetch; tests shorten it.
var detailDeadline = 20 * time.Second

// detailKey's attempt is 0 for "the latest".
type detailKey struct {
	repo    string
	id      int64
	attempt int
}

type detailEntry struct {
	at  time.Time
	val model.RunDetail
}

// detailCall is one fetch that concurrent callers for a key share.
type detailCall struct {
	done chan struct{}
	val  model.RunDetail
	err  error
}

type detailCache struct {
	mu      sync.Mutex
	entries map[detailKey]detailEntry
	calls   map[detailKey]*detailCall
	// histFailing holds whether the last fetch failed to read history, so a
	// read that keeps failing warns once.
	histFailing bool
}

func detailTTL(d model.RunDetail) time.Duration {
	if d.Status == "completed" {
		return detailDoneTTL
	}
	return detailActiveTTL
}

// coveredName resolves a configured or watched repository, ignoring case, to
// its configured spelling, and reports whether it is watched.
func (b *Backend) coveredName(name string) (string, bool, error) {
	cfg := b.Store.Config()
	if r := cfg.Repo(name); r != nil {
		return r.Name, false, nil
	}
	for _, w := range cfg.WatchRepos {
		if strings.EqualFold(w, name) {
			return w, true, nil
		}
	}
	return "", false, api.NotFound("repo " + name + " is not configured or watched")
}

// detailErr gives an error from a run details call the status the routes
// answer with: 404 and 410 keep theirs, and the rest read as on the Actions
// page, so a 403 names the missing permission.
func detailErr(err error, owner, repo string) error {
	var ae *api.Error
	if err == nil || errors.As(err, &ae) {
		return err
	}
	switch {
	case github.IsKind(err, github.ErrNotFound):
		return api.NotFound("not found on GitHub")
	case github.IsKind(err, github.ErrGone):
		return &api.Error{Status: http.StatusGone, Msg: "GitHub no longer keeps it"}
	}
	msg, retry := actionsErr(err, owner, repo)
	switch {
	case retry != nil:
		return &api.Error{Status: http.StatusTooManyRequests, Msg: msg, RetryAt: *retry}
	case errors.Is(err, context.DeadlineExceeded):
		return &api.Error{Status: http.StatusGatewayTimeout, Msg: msg}
	case github.IsKind(err, github.ErrAuth):
		return &api.Error{Status: http.StatusForbidden, Msg: msg}
	}
	return &api.Error{Status: http.StatusBadGateway, Msg: msg}
}

func ghrJobKey(repo string, runID int64, name string) string {
	return strings.ToLower(repo) + "#" + strconv.FormatInt(runID, 10) + "#" + name
}

// ghrJobs reads the same sources as ghrRuns, in the same order, keyed by
// job. History has no job ID, so two attempts of one job name are both
// marked.
func (b *Backend) ghrJobs() (map[string]bool, error) {
	insts := b.M.Status().Instances
	pend := b.M.Pending()
	hist, err := b.Hist.Query("", "", time.Time{}, 0)
	ours := map[string]bool{}
	if err != nil {
		return ours, err
	}
	for _, i := range insts {
		if i.Job != nil && i.Job.RunID != 0 {
			ours[ghrJobKey(i.Repo, i.Job.RunID, i.Job.Name)] = true
		}
	}
	for _, p := range pend {
		if p.RunID != 0 {
			ours[ghrJobKey(p.Repo, p.RunID, p.JobName)] = true
		}
	}
	for _, h := range hist {
		if h.RunID != 0 {
			ours[ghrJobKey(h.Repo, h.RunID, h.JobName)] = true
		}
	}
	return ours, nil
}

func modelSteps(steps []github.Step) []model.Step {
	out := make([]model.Step, 0, len(steps))
	for _, s := range steps {
		out = append(out, model.Step{Number: s.Number, Name: s.Name, Status: s.Status, Conclusion: s.Conclusion,
			StartedAt: s.StartedAt, CompletedAt: s.CompletedAt})
	}
	return out
}

func runJob(repo string, runID int64, j github.Job, ours map[string]bool) model.RunJob {
	labels := j.Labels
	if labels == nil {
		labels = []string{}
	}
	return model.RunJob{
		ID: j.ID, Name: j.Name, Status: j.Status, Conclusion: j.Conclusion,
		StartedAt: j.StartedAt, CompletedAt: j.CompletedAt, RunnerName: j.RunnerName, Labels: labels,
		HTMLURL: j.HTMLURL, GHR: ours[ghrJobKey(repo, runID, j.Name)], Steps: modelSteps(j.Steps),
	}
}

// RunDetail returns one attempt of a run, 0 meaning the latest. Callers for
// the same key share one fetch, detached from any one request; the result is
// kept 15 s while the attempt runs and 60 s once it has completed.
func (b *Backend) RunDetail(ctx context.Context, repo string, id int64, attempt int) (model.RunDetail, error) {
	name, watched, err := b.coveredName(repo)
	if err != nil {
		return model.RunDetail{}, err
	}
	if err := b.githubErr(); err != nil {
		return model.RunDetail{}, err
	}
	c := &b.details
	key := detailKey{repo: strings.ToLower(name), id: id, attempt: attempt}
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && b.now().Sub(e.at) < detailTTL(e.val) {
		c.mu.Unlock()
		return e.val, nil
	}
	if call, ok := c.calls[key]; ok {
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.val, call.err
		case <-ctx.Done():
			return model.RunDetail{}, ctx.Err()
		}
	}
	call := &detailCall{done: make(chan struct{})}
	if c.calls == nil {
		c.calls = map[detailKey]*detailCall{}
	}
	c.calls[key] = call
	c.mu.Unlock()

	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detailDeadline)
	call.val, call.err = b.fetchDetail(fctx, name, watched, id, attempt)
	cancel()
	call.err = detailErr(call.err, b.Store.Config().Owner, name)

	c.mu.Lock()
	delete(c.calls, key)
	if call.err == nil {
		now := b.now()
		for k, e := range c.entries {
			if now.Sub(e.at) >= detailTTL(e.val) {
				delete(c.entries, k)
			}
		}
		keys := []detailKey{key, {repo: key.repo, id: id, attempt: call.val.Attempt}}
		if call.val.Attempt == call.val.Attempts {
			keys = append(keys, detailKey{repo: key.repo, id: id})
		}
		for _, k := range keys {
			if _, ok := c.entries[k]; !ok && len(c.entries) >= detailEntries {
				c.evictOldest()
			}
			if c.entries == nil {
				c.entries = map[detailKey]detailEntry{}
			}
			c.entries[k] = detailEntry{at: now, val: call.val}
		}
	}
	c.mu.Unlock()
	close(call.done)
	return call.val, call.err
}

func (c *detailCache) evictOldest() {
	var oldest detailKey
	found := false
	for k, e := range c.entries {
		if !found || e.at.Before(c.entries[oldest].at) {
			oldest, found = k, true
		}
	}
	delete(c.entries, oldest)
}

func (b *Backend) fetchDetail(ctx context.Context, repo string, watched bool, id int64, attempt int) (model.RunDetail, error) {
	run, err := b.GH.GetRun(ctx, repo, id)
	if err != nil {
		return model.RunDetail{}, err
	}
	attempts := max(run.RunAttempt, 1)
	if attempt > attempts {
		return model.RunDetail{}, api.BadRequest(fmt.Sprintf("run %d has no attempt %d", id, attempt))
	}
	shown := run
	var jobs []github.Job
	if attempt == 0 || attempt == attempts {
		jobs, err = b.GH.ListJobs(ctx, repo, id)
	} else {
		shown, err = b.GH.GetRunAttempt(ctx, repo, id, attempt)
		if err == nil {
			jobs, err = b.GH.ListAttemptJobs(ctx, repo, id, attempt)
		}
	}
	if err != nil {
		return model.RunDetail{}, err
	}
	arts, err := b.GH.ListArtifacts(ctx, repo, id)
	if err != nil {
		return model.RunDetail{}, err
	}
	graph, note, err := b.runGraph(ctx, repo, shown.HeadSHA, shown.Path, jobs)
	if err != nil {
		return model.RunDetail{}, err
	}

	ours, herr := b.ghrJobs()
	b.details.mu.Lock()
	if herr != nil && !b.details.histFailing {
		log.Printf("run details: cannot read history, so no job is marked as ghr's: %v", herr)
	}
	b.details.histFailing = herr != nil
	b.details.mu.Unlock()

	start := shown.CreatedAt
	if shown.RunStartedAt != nil && !shown.RunStartedAt.IsZero() {
		start = *shown.RunStartedAt
	}
	title := shown.DisplayTitle
	if title == "" {
		title = shown.Name
	}
	d := model.RunDetail{
		Repo: repo, Watched: watched, ID: id, RunNumber: shown.RunNumber, Attempt: attempts, Attempts: attempts,
		Workflow: shown.Name, Title: title, Branch: shown.HeadBranch, Event: shown.Event, Actor: shown.Actor.Login,
		Status: shown.Status, Conclusion: shown.Conclusion, HeadSHA: shown.HeadSHA, Path: shown.Path,
		PullRequests: make([]int, 0, len(shown.PullRequests)), StartedAt: start, UpdatedAt: shown.UpdatedAt,
		HTMLURL: shown.HTMLURL, Jobs: make([]model.RunJob, 0, len(jobs)), Graph: graph, GraphNote: note,
		Artifacts: make([]model.RunArtifact, 0, len(arts)), FetchedAt: b.now(),
	}
	if attempt != 0 {
		d.Attempt = attempt
	}
	for _, p := range shown.PullRequests {
		d.PullRequests = append(d.PullRequests, p.Number)
	}
	for _, j := range jobs {
		d.Jobs = append(d.Jobs, runJob(repo, id, j, ours))
	}
	for _, a := range arts {
		d.Artifacts = append(d.Artifacts, model.RunArtifact{ID: a.ID, Name: a.Name, SizeBytes: a.SizeInBytes, Expired: a.Expired, ExpiresAt: a.ExpiresAt})
	}
	return d, nil
}
```

In `internal/daemon/backend.go`, add the field after `wfFiles wfCache`:

```go
	actions actionsCache
	wfFiles wfCache
	details detailCache
}
```

and in `fetchSteps` (lines 337-342) replace the loop that builds `steps` and its `return steps, nil` with:

```go
		return modelSteps(j.Steps), nil
```

- [ ] **Step 4: Run the tests with the race detector**

Run: `timeout 300 go test -race ./internal/daemon/ -run RunDetail -count=1`
Expected: PASS. If the race detector is unavailable (`-race requires cgo`), run without `-race` and tell the owner.

- [ ] **Step 5: Format, vet and run the package**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/ && timeout 300 go test ./internal/daemon/ -count=1`
Expected: no `gofmt` output; vet and tests pass (the runner steps tests still pass with `modelSteps`).

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/rundetail.go internal/daemon/rundetail_test.go internal/daemon/backend.go
git commit -m "feat(daemon): assemble and cache run details"
```

---

### Task 8: Job logs

**Files:**
- Create: `internal/daemon/joblogs.go`
- Modify: `internal/daemon/backend.go:113` (a `logs logCache` field after `details detailCache`)
- Test: `internal/daemon/joblogs_test.go`

**Interfaces:**
- Consumes: `joblog.Parse`, `joblog.Annotations` (Task 3); `github.ErrTooLarge`, `github.ErrGone` (Tasks 1-2); `lru`, `newLRU` (Task 6); `coveredName`, `detailErr`, `modelSteps`, `detailDeadline` (Task 7); `fakeRuns` (Task 5)
- Produces: `(*Backend).JobLog`, `(*Backend).RawJobLog` (Contracts, Go — daemon); `logCache`, `logCall`, `logJob`, `logOutcome`, `jobNotes`, `startLogLocked`, `parsedLogBytes`, `logRouteWait`, `annotationEntries`, `logRetryTTL`, `logJobOf`, `logLines`, `waitCalls` (Contracts, Go — daemon internals)

**Items:** 4

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/joblogs_test.go`:

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// logLines formats lines as GitHub writes a job log, one second apart from
// actionsT0.
func logLines(lines ...string) []byte {
	var sb strings.Builder
	for i, l := range lines {
		fmt.Fprintf(&sb, "%s %s\n", actionsT0.Add(time.Duration(i)*time.Second).Format("2006-01-02T15:04:05.0000000Z"), l)
	}
	return []byte(sb.String())
}

// logJobOf gives gh a completed darkmem job of run 102 with one step,
// "Lint", whose log holds lines.
func logJobOf(gh *fakeGH, id int64, name string, lines ...string) github.Job {
	start, end := actionsT0, actionsT0.Add(time.Minute)
	j := github.Job{ID: id, RunID: 102, Name: name, Status: "completed", Conclusion: "failure", StartedAt: &start, CompletedAt: &end,
		Steps: []github.Step{{Number: 1, Name: "Lint", Status: "completed", Conclusion: "failure", StartedAt: &start, CompletedAt: &end}}}
	gh.rmu.Lock()
	defer gh.rmu.Unlock()
	if gh.jobByID == nil {
		gh.jobByID = map[int64]github.Job{}
		gh.logs = map[int64][]byte{}
		gh.logErr = map[int64]error{}
	}
	gh.jobByID[id] = j
	gh.logs[id] = logLines(lines...)
	return j
}

func setParsedLogBytes(t *testing.T, n int64) {
	old := parsedLogBytes
	parsedLogBytes = n
	t.Cleanup(func() { parsedLogBytes = old })
}

// waitCalls waits up to 3 s for n calls starting with prefix.
func waitCalls(t *testing.T, gh *fakeGH, prefix string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for gh.count(prefix) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d %s calls, want %d", gh.count(prefix), prefix, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestJobLogParsesAndKeepsTheLog(t *testing.T) {
	b, _, gh := newBackend(t)
	logJobOf(gh, 1002, "Backend tests", "##[group]Run ruff check .", "ruff check .", "##[endgroup]", "##[error]F401 unused")
	ctx := context.Background()
	for range 2 {
		l, err := b.JobLog(ctx, "DarkMem", 1002)
		if err != nil {
			t.Fatal(err)
		}
		if l.JobID != 1002 || l.TooLarge || l.Expired || len(l.Steps) != 1 || len(l.Steps[0].Lines) != 4 || l.Steps[0].Lines[3].Kind != "error" {
			t.Fatalf("log %+v", l)
		}
	}
	if gh.count("GetJob darkmem 1002") != 2 || gh.count("JobLog darkmem 1002") != 1 {
		t.Fatalf("calls %v", gh.calls)
	}
	if b.logs.notes.maxEntries != annotationEntries || b.logs.parsed.maxSize != parsedLogBytes {
		t.Fatalf("bounds %d entries, %d bytes", b.logs.notes.maxEntries, b.logs.parsed.maxSize)
	}
}

func TestJobLogRefusals(t *testing.T) {
	b, m, gh := newBackend(t)
	j := logJobOf(gh, 1002, "Backend tests", "x")
	j.Status = "in_progress"
	gh.jobByID[1002] = j
	ctx := context.Background()
	var ae *api.Error
	for name, call := range map[string]func(repo string) error{
		"log": func(repo string) error { _, err := b.JobLog(ctx, repo, 1002); return err },
		"raw": func(repo string) error { _, err := b.RawJobLog(ctx, repo, 1002); return err },
	} {
		if err := call("darkmem"); !errors.As(err, &ae) || ae.Status != 409 || ae.Msg != "job has not finished" {
			t.Fatalf("%s unfinished: %v", name, err)
		}
		if err := call("nope"); !errors.As(err, &ae) || ae.Status != 404 || ae.Msg != "repo nope is not configured or watched" {
			t.Fatalf("%s uncovered: %v", name, err)
		}
	}
	if gh.count("JobLog") != 0 {
		t.Fatalf("an unfinished job's log was downloaded: %v", gh.calls)
	}
	m.degraded = "401 Bad credentials"
	if _, err := b.JobLog(ctx, "darkmem", 1002); apiStatus(err) != 503 {
		t.Fatalf("degraded: %v", err)
	}
	if _, err := b.JobLog(ctx, "darkmem", 9); apiStatus(err) != 503 || gh.count("GetJob darkmem 9") != 0 {
		t.Fatalf("degraded made calls: %v", gh.calls)
	}
}

func TestJobLogOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		want     model.JobLog
		status   int
		retained bool
	}{
		{"expired", &github.APIError{Status: 410, Kind: github.ErrGone}, model.JobLog{JobID: 1002, Expired: true}, 0, true},
		{"too large", github.ErrTooLarge, model.JobLog{JobID: 1002, TooLarge: true}, 0, true},
		{"failed", errors.New("boom"), model.JobLog{}, 502, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh := newBackend(t)
			logJobOf(gh, 1002, "Backend tests", "x")
			gh.logErr[1002] = tc.err
			for i := range 2 {
				l, err := b.JobLog(context.Background(), "darkmem", 1002)
				if apiStatus(err) != tc.status || l.JobID != tc.want.JobID || l.Expired != tc.want.Expired || l.TooLarge != tc.want.TooLarge {
					t.Fatalf("call %d: log %+v err %v", i, l, err)
				}
			}
			want := 2
			if tc.retained {
				want = 1
			}
			if n := gh.count("JobLog"); n != want {
				t.Fatalf("%d downloads, want %d", n, want)
			}
		})
	}
}

func TestJobLogParsedCacheIsBoundedByLogSize(t *testing.T) {
	setParsedLogBytes(t, int64(len(logLines("a long enough line")))+10)
	b, _, gh := newBackend(t)
	logJobOf(gh, 1, "one", "a long enough line")
	logJobOf(gh, 2, "two", "a long enough line")
	ctx := context.Background()
	for _, id := range []int64{1, 2, 1} {
		if _, err := b.JobLog(ctx, "darkmem", id); err != nil {
			t.Fatal(err)
		}
	}
	if n := gh.count("JobLog darkmem 1"); n != 2 {
		t.Fatalf("job 1 downloaded %d times, want 2: only one log fits", n)
	}
}

func TestJobLogCallersShareOneDownload(t *testing.T) {
	b, _, gh := newBackend(t)
	logJobOf(gh, 1002, "Backend tests", "x")
	gh.logWait = make(chan struct{})
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := b.JobLog(context.Background(), "darkmem", 1002)
			done <- err
		}()
	}
	waitCalls(t, gh, "GetJob", 2)
	waitCalls(t, gh, "JobLog", 1)
	time.Sleep(50 * time.Millisecond)
	close(gh.logWait)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if n := gh.count("JobLog"); n != 1 {
		t.Fatalf("%d downloads, want 1", n)
	}
}

func TestJobLogStopsWaitingButKeepsTheDownload(t *testing.T) {
	old := logRouteWait
	logRouteWait = 20 * time.Millisecond
	t.Cleanup(func() { logRouteWait = old })
	b, _, gh := newBackend(t)
	logJobOf(gh, 1002, "Backend tests", "x")
	gh.logWait = make(chan struct{})
	ctx := context.Background()
	var ae *api.Error
	if _, err := b.JobLog(ctx, "darkmem", 1002); !errors.As(err, &ae) || ae.Status != 503 || ae.Msg != "GitHub is still sending this log; try again shortly" {
		t.Fatalf("slow download: %v", err)
	}
	close(gh.logWait)
	deadline := time.Now().Add(3 * time.Second)
	for {
		b.logs.mu.Lock()
		_, ok := b.logs.parsed.get(1002)
		b.logs.mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the download never filled the cache")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if l, err := b.JobLog(ctx, "darkmem", 1002); err != nil || len(l.Steps) != 1 || gh.count("JobLog") != 1 {
		t.Fatalf("after the download: %+v err %v, %d downloads", l, err, gh.count("JobLog"))
	}
}

func TestLogDownloadsRunFourAtOnce(t *testing.T) {
	b, _, gh := newBackend(t)
	for id := int64(1); id <= 6; id++ {
		logJobOf(gh, id, fmt.Sprintf("job %d", id), "x")
	}
	gh.logWait = make(chan struct{})
	done := make(chan error, 6)
	for id := int64(1); id <= 6; id++ {
		go func() {
			_, err := b.JobLog(context.Background(), "darkmem", id)
			done <- err
		}()
	}
	waitCalls(t, gh, "JobLog", 4)
	time.Sleep(50 * time.Millisecond)
	if n := gh.count("JobLog"); n != 4 {
		t.Fatalf("%d downloads started, want 4", n)
	}
	close(gh.logWait)
	for range 6 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	gh.rmu.Lock()
	peak := gh.logPeak
	gh.rmu.Unlock()
	if peak != 4 || gh.count("JobLog") != 6 {
		t.Fatalf("peak %d, %d downloads", peak, gh.count("JobLog"))
	}
}

func TestRawJobLogGivesUpWhenEverySlotIsBusy(t *testing.T) {
	old := rawLogDeadline
	rawLogDeadline = 50 * time.Millisecond
	t.Cleanup(func() { rawLogDeadline = old })
	b, _, gh := newBackend(t)
	for id := int64(1); id <= 5; id++ {
		logJobOf(gh, id, fmt.Sprintf("job %d", id), "x")
	}
	gh.logWait = make(chan struct{})
	done := make(chan error, 4)
	for id := int64(1); id <= 4; id++ {
		go func() {
			_, err := b.JobLog(context.Background(), "darkmem", id)
			done <- err
		}()
	}
	waitCalls(t, gh, "JobLog", 4)
	var ae *api.Error
	if _, err := b.RawJobLog(context.Background(), "darkmem", 5); !errors.As(err, &ae) || ae.Status != 503 || ae.Msg != "ghr is reading other logs; try again shortly" {
		t.Fatalf("busy slots: %v", err)
	}
	if n := gh.count("JobLog darkmem 5"); n != 0 {
		t.Fatalf("job 5 was downloaded %d times while every slot was busy", n)
	}
	close(gh.logWait)
	for range 4 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestRawJobLog(t *testing.T) {
	b, _, gh := newBackend(t)
	big := strings.Repeat("x", parseLimit)
	logJobOf(gh, 1002, "Backend tests", big)
	ctx := context.Background()
	if l, err := b.JobLog(ctx, "darkmem", 1002); err != nil || !l.TooLarge {
		t.Fatalf("parsed: %+v err %v", l, err)
	}
	for range 2 {
		raw, err := b.RawJobLog(ctx, "darkmem", 1002)
		if err != nil || !strings.Contains(raw.Text, big) {
			t.Fatalf("raw: %d bytes, err %v", len(raw.Text), err)
		}
	}
	if n := gh.count("JobLog"); n != 3 {
		t.Fatalf("%d downloads, want 3: the raw log is not cached", n)
	}
	var ae *api.Error
	for _, tc := range []struct {
		err    error
		status int
		msg    string
	}{
		{github.ErrTooLarge, 413, "log is larger than 32 MiB; open it on GitHub"},
		{&github.APIError{Status: 410, Kind: github.ErrGone}, 410, "GitHub no longer keeps this job's log"},
	} {
		gh.rmu.Lock()
		gh.logErr[1002] = tc.err
		gh.rmu.Unlock()
		if _, err := b.RawJobLog(ctx, "darkmem", 1002); !errors.As(err, &ae) || ae.Status != tc.status || ae.Msg != tc.msg {
			t.Fatalf("%v: got %v", tc.err, err)
		}
	}
}
```

The logs are single-step, so the tests do not depend on how Task 3's `Parse` splits steps: every line belongs to "Lint", numbered from 1.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run 'JobLog|LogDownloads' -count=1`
Expected: FAIL to build with `b.JobLog undefined`, `undefined: parsedLogBytes` and `undefined: logRouteWait`.

- [ ] **Step 3: Write the log downloads and caches**

Create `internal/daemon/joblogs.go`:

```go
package daemon

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/joblog"
	"github.com/darkraise/ghr/internal/model"
)

const (
	parseLimit        = 4 << 20
	rawLimit          = 32 << 20
	logSlots          = 4
	annotationEntries = 500
	logRetryTTL       = 10 * time.Minute
)

var (
	// parsedLogBytes bounds the parsed-log cache by the raw logs' total size;
	// tests shrink it before the cache is first used.
	parsedLogBytes int64 = 32 << 20
	logDeadline          = 60 * time.Second
	// logRouteWait and rawLogDeadline bound each log request from its start
	// (job read, slot wait and download), keeping the answer under the web
	// listener's 60 s WriteTimeout; a parsed download that outlasts the wait
	// still finishes and fills the cache for the next call.
	logRouteWait   = 50 * time.Second
	rawLogDeadline = 50 * time.Second
)

type logOutcome int

const (
	logParsed logOutcome = iota
	logExpired
	logTooLarge
	logFailed
)

// jobNotes is one finished job's annotations and the count beyond the cap.
type jobNotes struct {
	items   []model.Annotation
	omitted int
}

type logResult struct {
	outcome logOutcome
	log     model.JobLog
	notes   jobNotes
	err     error // set for logFailed
}

// logCall is one log download that the log route and the annotations route
// share.
type logCall struct {
	done chan struct{}
	res  logResult
}

// logMiss is an expired, too large or failed outcome, kept logRetryTTL.
type logMiss struct {
	at  time.Time
	res logResult
}

type logCache struct {
	mu sync.Mutex
	// slots limits log downloads across the whole daemon, which runs one
	// Backend.
	slots  chan struct{}
	calls  map[int64]*logCall
	parsed *lru[int64, model.JobLog]
	notes  *lru[int64, jobNotes]
	misses map[int64]logMiss
}

// initLocked creates the cache on first use; the caller holds mu.
func (c *logCache) initLocked() {
	if c.slots != nil {
		return
	}
	c.slots = make(chan struct{}, logSlots)
	c.calls = map[int64]*logCall{}
	c.parsed = newLRU[int64, model.JobLog](1<<30, parsedLogBytes)
	c.notes = newLRU[int64, jobNotes](annotationEntries, 0)
	c.misses = map[int64]logMiss{}
}

// logJob is what a download needs to parse a job's log.
type logJob struct {
	repo  string
	id    int64
	name  string
	steps []model.Step
}

// startLogLocked joins the job's download in flight or starts one; the
// caller holds mu.
func (b *Backend) startLogLocked(j logJob) *logCall {
	c := &b.logs
	if call, ok := c.calls[j.id]; ok {
		return call
	}
	call := &logCall{done: make(chan struct{})}
	c.calls[j.id] = call
	go b.downloadLog(j, call)
	return call
}

// downloadLog runs detached from every request, so a caller that stops
// waiting still leaves the result in the caches.
func (b *Backend) downloadLog(j logJob, call *logCall) {
	c := &b.logs
	c.slots <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), logDeadline)
	data, err := b.GH.JobLog(ctx, j.repo, j.id, parseLimit)
	cancel()
	<-c.slots

	var res logResult
	switch {
	case err == nil:
		steps := joblog.Parse(data, j.steps)
		items, omitted := joblog.Annotations(j.id, j.name, steps)
		res = logResult{outcome: logParsed, log: model.JobLog{JobID: j.id, Steps: steps}, notes: jobNotes{items: items, omitted: omitted}}
	case errors.Is(err, github.ErrTooLarge):
		res.outcome = logTooLarge
	case github.IsKind(err, github.ErrGone):
		res.outcome = logExpired
	default:
		res.outcome, res.err = logFailed, err
		log.Printf("job %d log: %v", j.id, err)
	}

	c.mu.Lock()
	delete(c.calls, j.id)
	if res.outcome == logParsed {
		c.parsed.add(j.id, res.log, int64(len(data)))
		c.notes.add(j.id, res.notes, 0)
		delete(c.misses, j.id)
	} else {
		c.misses[j.id] = logMiss{at: b.now(), res: res}
	}
	c.mu.Unlock()
	call.res = res
	close(call.done)
}

func flaggedLog(id int64, o logOutcome) model.JobLog {
	return model.JobLog{JobID: id, TooLarge: o == logTooLarge, Expired: o == logExpired, Steps: []model.LogStep{}}
}

// finishedJob reads a job of a covered repository and refuses one that has
// not completed.
func (b *Backend) finishedJob(ctx context.Context, repo string, jobID int64) (logJob, error) {
	name, _, err := b.coveredName(repo)
	if err != nil {
		return logJob{}, err
	}
	if err := b.githubErr(); err != nil {
		return logJob{}, err
	}
	gctx, cancel := context.WithTimeout(ctx, detailDeadline)
	job, err := b.GH.GetJob(gctx, name, jobID)
	cancel()
	if err != nil {
		return logJob{}, detailErr(err, b.Store.Config().Owner, name)
	}
	if job.Status != "completed" {
		return logJob{}, api.Conflict("job has not finished")
	}
	return logJob{repo: name, id: jobID, name: job.Name, steps: modelSteps(job.Steps)}, nil
}

// JobLog returns a finished job's log split into steps. An expired or too
// large log comes back flagged; a failed download is tried again.
func (b *Backend) JobLog(ctx context.Context, repo string, jobID int64) (model.JobLog, error) {
	wctx, cancel := context.WithTimeout(ctx, logRouteWait)
	defer cancel()
	j, err := b.finishedJob(wctx, repo, jobID)
	if err != nil {
		return model.JobLog{}, err
	}
	c := &b.logs
	c.mu.Lock()
	c.initLocked()
	if l, ok := c.parsed.get(jobID); ok {
		c.mu.Unlock()
		return l, nil
	}
	if m, ok := c.misses[jobID]; ok && m.res.outcome != logFailed && b.now().Sub(m.at) < logRetryTTL {
		c.mu.Unlock()
		return flaggedLog(jobID, m.res.outcome), nil
	}
	call := b.startLogLocked(j)
	c.mu.Unlock()
	select {
	case <-call.done:
	case <-wctx.Done():
		if err := ctx.Err(); err != nil {
			return model.JobLog{}, err
		}
		return model.JobLog{}, &api.Error{Status: http.StatusServiceUnavailable, Msg: "GitHub is still sending this log; try again shortly"}
	}
	switch call.res.outcome {
	case logParsed:
		return call.res.log, nil
	case logFailed:
		return model.JobLog{}, detailErr(call.res.err, b.Store.Config().Owner, j.repo)
	}
	return flaggedLog(jobID, call.res.outcome), nil
}

// RawJobLog returns a finished job's whole log, up to 32 MiB, uncached.
func (b *Backend) RawJobLog(ctx context.Context, repo string, jobID int64) (model.RawLog, error) {
	rctx, cancel := context.WithTimeout(ctx, rawLogDeadline)
	defer cancel()
	j, err := b.finishedJob(rctx, repo, jobID)
	if err != nil {
		return model.RawLog{}, err
	}
	c := &b.logs
	c.mu.Lock()
	c.initLocked()
	slots := c.slots
	c.mu.Unlock()
	select {
	case slots <- struct{}{}:
	case <-rctx.Done():
		if err := ctx.Err(); err != nil {
			return model.RawLog{}, err
		}
		return model.RawLog{}, &api.Error{Status: http.StatusServiceUnavailable, Msg: "ghr is reading other logs; try again shortly"}
	}
	defer func() { <-slots }()
	data, err := b.GH.JobLog(rctx, j.repo, jobID, rawLimit)
	switch {
	case errors.Is(err, github.ErrTooLarge):
		return model.RawLog{}, &api.Error{Status: http.StatusRequestEntityTooLarge, Msg: "log is larger than 32 MiB; open it on GitHub"}
	case github.IsKind(err, github.ErrGone):
		return model.RawLog{}, &api.Error{Status: http.StatusGone, Msg: "GitHub no longer keeps this job's log"}
	case err != nil:
		return model.RawLog{}, detailErr(err, b.Store.Config().Owner, j.repo)
	}
	return model.RawLog{Text: string(data)}, nil
}
```

In `internal/daemon/backend.go`, add the field after `details detailCache`:

```go
	actions actionsCache
	wfFiles wfCache
	details detailCache
	logs    logCache
}
```

- [ ] **Step 4: Run the tests with the race detector**

Run: `timeout 300 go test -race ./internal/daemon/ -run 'JobLog|LogDownloads' -count=1`
Expected: PASS. If the race detector is unavailable, run without `-race` and tell the owner.

- [ ] **Step 5: Format, vet and run the package**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/ && timeout 300 go test ./internal/daemon/ -count=1`
Expected: no `gofmt` output; vet and tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/joblogs.go internal/daemon/joblogs_test.go internal/daemon/backend.go
git commit -m "feat(daemon): download, parse and cache job logs"
```

---

### Task 9: Annotations

**Files:**
- Create: `internal/daemon/annotations.go`
- Test: `internal/daemon/annotations_test.go`

**Interfaces:**
- Consumes: `joblog.Annotations`, `joblog.LevelRank` (Task 3); `RunDetail`, `detailRun` (Task 7); `logCache`, `startLogLocked`, `logJob`, `jobNotes`, `logRetryTTL`, `logJobOf` (Task 8); `fakeRuns` (Task 5)
- Produces: `(*Backend).RunAnnotations` (Contracts, Go — daemon); `annotationsWait` (Contracts, Go — daemon internals)

**Items:** 3

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/annotations_test.go`:

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func setAnnotationsWait(t *testing.T, d time.Duration) {
	old := annotationsWait
	annotationsWait = d
	t.Cleanup(func() { annotationsWait = old })
}

// annotatedRun gives gh run 102 of darkmem with the given jobs as its latest
// attempt's jobs.
func annotatedRun(gh *fakeGH, jobs ...github.Job) {
	detailRun(gh, "completed")
	gh.jobs = map[int64][]github.Job{102: jobs}
	gh.content = nil // the graph is not under test: the note path is enough
}

func annotationText(items []model.Annotation) string {
	var out []string
	for _, a := range items {
		out = append(out, fmt.Sprintf("%s %d %d:%d %s", a.Level, a.JobID, a.Step, a.Line, a.Message))
	}
	return fmt.Sprint(out)
}

func TestRunAnnotationsCollectsFinishedJobs(t *testing.T) {
	b, _, gh := newBackend(t)
	build := logJobOf(gh, 2001, "Build", "##[warning]old node", "##[error]build broke")
	test := logJobOf(gh, 2002, "test", "##[notice]flaky", "##[error]test broke")
	skipped := logJobOf(gh, 2003, "lint", "##[error]never read")
	skipped.Conclusion = "skipped"
	running := github.Job{ID: 2004, Name: "deploy", Status: "in_progress"}
	annotatedRun(gh, build, test, skipped, running)

	a, err := b.RunAnnotations(context.Background(), "darkmem", 102, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := "[error 2001 1:2 build broke error 2002 1:2 test broke warning 2001 1:1 old node notice 2002 1:1 flaky]"
	if got := annotationText(a.Items); got != want {
		t.Fatalf("items %s", got)
	}
	if a.Items[0].JobName != "Build" || a.Items[0].StepName != "Lint" || a.Omitted == nil || len(a.Omitted) != 0 ||
		a.Unread != 0 || a.ExpiredLogs != 0 || a.TooLarge != 0 || a.Failed != 0 {
		t.Fatalf("annotations %+v", a)
	}
	if gh.count("JobLog") != 2 {
		t.Fatalf("downloads %v", gh.calls)
	}
	if _, err := b.RunAnnotations(context.Background(), "darkmem", 102, 0); err != nil || gh.count("JobLog") != 2 {
		t.Fatalf("second call: %d downloads, err %v", gh.count("JobLog"), err)
	}
}

func TestRunAnnotationsCapsEachJob(t *testing.T) {
	b, _, gh := newBackend(t)
	var lines []string
	for i := range 52 {
		lines = append(lines, fmt.Sprintf("##[error]problem %d", i))
	}
	annotatedRun(gh, logJobOf(gh, 2001, "Build", lines...))
	a, err := b.RunAnnotations(context.Background(), "darkmem", 102, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Items) != 50 || a.Omitted[2001] != 2 {
		t.Fatalf("%d items, omitted %v", len(a.Items), a.Omitted)
	}
}

func TestRunAnnotationsReusesTheParsedLog(t *testing.T) {
	b, _, gh := newBackend(t)
	annotatedRun(gh, logJobOf(gh, 2001, "Build", "##[error]build broke"))
	ctx := context.Background()
	if _, err := b.JobLog(ctx, "darkmem", 2001); err != nil {
		t.Fatal(err)
	}
	b.logs.mu.Lock()
	b.logs.notes.remove(2001)
	b.logs.mu.Unlock()
	a, err := b.RunAnnotations(ctx, "darkmem", 102, 0)
	if err != nil || len(a.Items) != 1 || gh.count("JobLog") != 1 {
		t.Fatalf("items %+v, %d downloads, err %v", a.Items, gh.count("JobLog"), err)
	}
}

func TestRunAnnotationsCountsUnreadAfterTheWait(t *testing.T) {
	setAnnotationsWait(t, 50*time.Millisecond)
	b, _, gh := newBackend(t)
	annotatedRun(gh, logJobOf(gh, 2001, "Build", "##[error]a"), logJobOf(gh, 2002, "test", "##[error]b"))
	gh.logWait = make(chan struct{})
	t.Cleanup(func() { close(gh.logWait) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	a, err := b.RunAnnotations(ctx, "darkmem", 102, 0)
	if err != nil || a.Unread != 2 || len(a.Items) != 0 {
		t.Fatalf("annotations %+v err %v", a, err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("waited %v", took)
	}
}

func TestRunAnnotationsCountsLogOutcomes(t *testing.T) {
	b, _, gh := newBackend(t)
	clock := frozenClock(b, actionsT0)
	annotatedRun(gh, logJobOf(gh, 2001, "a", "x"), logJobOf(gh, 2002, "b", "x"), logJobOf(gh, 2003, "c", "x"))
	gh.logErr[2001] = &github.APIError{Status: 410, Kind: github.ErrGone}
	gh.logErr[2002] = github.ErrTooLarge
	gh.logErr[2003] = errors.New("boom")
	ctx := context.Background()
	for i, step := range []struct {
		at        time.Duration
		downloads int
	}{{0, 3}, {logRetryTTL - time.Second, 3}, {logRetryTTL, 6}} {
		clock.Store(actionsT0.Add(step.at).UnixNano())
		a, err := b.RunAnnotations(ctx, "darkmem", 102, 0)
		if err != nil || a.ExpiredLogs != 1 || a.TooLarge != 1 || a.Failed != 1 || a.Unread != 0 || len(a.Items) != 0 {
			t.Fatalf("step %d: %+v err %v", i, a, err)
		}
		if n := gh.count("JobLog"); n != step.downloads {
			t.Fatalf("step %d: %d downloads, want %d", i, n, step.downloads)
		}
	}
}

func TestRunAnnotationsPassesRunErrors(t *testing.T) {
	b, _, _ := newBackend(t)
	if _, err := b.RunAnnotations(context.Background(), "nope", 102, 0); apiStatus(err) != 404 {
		t.Fatalf("err %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run RunAnnotations -count=1`
Expected: FAIL to build with `b.RunAnnotations undefined` and `undefined: annotationsWait`.

- [ ] **Step 3: Write the annotations route**

Create `internal/daemon/annotations.go`:

```go
package daemon

import (
	"context"
	"sort"
	"time"

	"github.com/darkraise/ghr/internal/joblog"
	"github.com/darkraise/ghr/internal/model"
)

// annotationsWait bounds how long the annotations route waits for log
// downloads before it answers with what it has; tests shorten it.
var annotationsWait = 20 * time.Second

// RunAnnotations collects the annotations of an attempt's finished jobs from
// their logs. Downloads it starts keep running after it answers; the jobs
// still downloading count in Unread.
func (b *Backend) RunAnnotations(ctx context.Context, repo string, id int64, attempt int) (model.RunAnnotations, error) {
	d, err := b.RunDetail(ctx, repo, id, attempt)
	if err != nil {
		return model.RunAnnotations{}, err
	}
	notes := map[int64]jobNotes{}
	outcomes := map[int64]logOutcome{}
	calls := map[int64]*logCall{}
	c := &b.logs
	c.mu.Lock()
	c.initLocked()
	for _, j := range d.Jobs {
		if j.Status != "completed" || j.Conclusion == "skipped" {
			continue
		}
		if n, ok := c.notes.get(j.ID); ok {
			notes[j.ID] = n
			continue
		}
		if l, ok := c.parsed.get(j.ID); ok {
			items, omitted := joblog.Annotations(j.ID, j.Name, l.Steps)
			n := jobNotes{items: items, omitted: omitted}
			c.notes.add(j.ID, n, 0)
			notes[j.ID] = n
			continue
		}
		if m, ok := c.misses[j.ID]; ok && b.now().Sub(m.at) < logRetryTTL {
			outcomes[j.ID] = m.res.outcome
			continue
		}
		calls[j.ID] = b.startLogLocked(logJob{repo: d.Repo, id: j.ID, name: j.Name, steps: j.Steps})
	}
	c.mu.Unlock()

	timer := time.NewTimer(annotationsWait)
	defer timer.Stop()
wait:
	for _, call := range calls {
		select {
		case <-call.done:
		case <-timer.C:
			break wait
		case <-ctx.Done():
			break wait
		}
	}

	out := model.RunAnnotations{Items: []model.Annotation{}, Omitted: map[int64]int{}}
	for _, j := range d.Jobs {
		if call, ok := calls[j.ID]; ok {
			select {
			case <-call.done:
			default:
				out.Unread++
				continue
			}
			if call.res.outcome == logParsed {
				notes[j.ID] = call.res.notes
			} else {
				outcomes[j.ID] = call.res.outcome
			}
		}
		if n, ok := notes[j.ID]; ok {
			out.Items = append(out.Items, n.items...)
			if n.omitted > 0 {
				out.Omitted[j.ID] = n.omitted
			}
		}
		if o, ok := outcomes[j.ID]; ok {
			switch o {
			case logExpired:
				out.ExpiredLogs++
			case logTooLarge:
				out.TooLarge++
			case logFailed:
				out.Failed++
			}
		}
	}
	// Items are in job order and each job's are sorted, so a stable sort by
	// level gives level, then job, then step and line.
	sort.SliceStable(out.Items, func(i, k int) bool {
		return joblog.LevelRank(out.Items[i].Level) < joblog.LevelRank(out.Items[k].Level)
	})
	return out, nil
}
```

- [ ] **Step 4: Run the tests with the race detector**

Run: `timeout 300 go test -race ./internal/daemon/ -run RunAnnotations -count=1`
Expected: PASS. If the race detector is unavailable, run without `-race` and tell the owner.

- [ ] **Step 5: Format, vet and run the package**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/ && timeout 300 go test ./internal/daemon/ -count=1`
Expected: no `gofmt` output; vet and tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/annotations.go internal/daemon/annotations_test.go
git commit -m "feat(daemon): collect annotations from job logs"
```

---

### Task 10: Artifact download

**Files:**
- Create: `internal/daemon/artifacts.go`
- Test: `internal/daemon/artifacts_test.go`

**Interfaces:**
- Consumes: `ArtifactURL` on the daemon `GitHub` interface and `fakeRuns.artURL`, `artErr` (Task 5); `github.ErrGone` (Task 1); `coveredName`, `detailErr`, `detailDeadline` (Task 7)
- Produces: `(*Backend).ArtifactDownload` (Contracts, Go — daemon); `downloadHost` (Contracts, Go — daemon internals)

**Items:** 5

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/artifacts_test.go`:

```go
package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
)

func TestDownloadHost(t *testing.T) {
	for u, want := range map[string]bool{
		"https://productionresultssa1.blob.core.windows.net/actions-results/x.zip?sig=1": true,
		"https://pipelines.actions.githubusercontent.com/x":                              true,
		"https://PIPELINES.ACTIONS.GITHUBUSERCONTENT.COM/x":                              true,
		"http://productionresultssa1.blob.core.windows.net/x":                            false,
		"https://blob.core.windows.net.example.com/x":                                    false,
		"https://evilblob.core.windows.net/x":                                            false,
		"https://example.com/x":                                                          false,
		"/relative":                                                                      false,
		"":                                                                               false,
	} {
		if got := downloadHost(u); got != want {
			t.Errorf("%q: got %v", u, got)
		}
	}
}

func TestArtifactDownload(t *testing.T) {
	const signed = "https://productionresultssa1.blob.core.windows.net/actions-results/x.zip?sig=1"
	for _, tc := range []struct {
		name   string
		url    string
		err    error
		status int
		msg    string
	}{
		{"signed address", signed, nil, 0, ""},
		{"expired", "", &github.APIError{Status: 410, Kind: github.ErrGone}, 410, "artifact has expired"},
		{"unknown host", "https://example.com/x.zip", nil, 502, "GitHub returned an unexpected download address"},
		{"not found", "", &github.APIError{Status: 404, Kind: github.ErrNotFound}, 404, "not found on GitHub"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _, gh := newBackend(t)
			gh.artURL, gh.artErr = tc.url, tc.err
			d, err := b.ArtifactDownload(context.Background(), "DarkMem", 11)
			var ae *api.Error
			if tc.status == 0 {
				if err != nil || d.URL != signed || gh.count("ArtifactURL darkmem 11") != 1 {
					t.Fatalf("url %q err %v calls %v", d.URL, err, gh.calls)
				}
				return
			}
			if !errors.As(err, &ae) || ae.Status != tc.status || ae.Msg != tc.msg || d.URL != "" {
				t.Fatalf("url %q err %#v", d.URL, err)
			}
		})
	}
}

func TestArtifactDownloadRefusals(t *testing.T) {
	b, m, gh := newBackend(t)
	if _, err := b.ArtifactDownload(context.Background(), "nope", 11); apiStatus(err) != 404 {
		t.Fatalf("uncovered: %v", err)
	}
	m.degraded = "401 Bad credentials"
	if _, err := b.ArtifactDownload(context.Background(), "darkmem", 11); apiStatus(err) != 503 || gh.count("ArtifactURL") != 0 {
		t.Fatalf("degraded: %v, calls %v", err, gh.calls)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/ -run 'Artifact|DownloadHost' -count=1`
Expected: FAIL to build with `undefined: downloadHost` and `b.ArtifactDownload undefined`.

- [ ] **Step 3: Write the download route's backend**

Create `internal/daemon/artifacts.go`:

```go
package daemon

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// downloadHost reports whether u is an address GitHub sends artifact and log
// downloads to: https on a host under githubusercontent.com or Azure blob
// storage.
func downloadHost(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" {
		return false
	}
	host := strings.ToLower(p.Hostname())
	return strings.HasSuffix(host, ".githubusercontent.com") || strings.HasSuffix(host, ".blob.core.windows.net")
}

// ArtifactDownload returns GitHub's short-lived address for an artifact's
// zip; the browser navigates to it, so it is checked first.
func (b *Backend) ArtifactDownload(ctx context.Context, repo string, id int64) (model.DownloadURL, error) {
	name, _, err := b.coveredName(repo)
	if err != nil {
		return model.DownloadURL{}, err
	}
	if err := b.githubErr(); err != nil {
		return model.DownloadURL{}, err
	}
	actx, cancel := context.WithTimeout(ctx, detailDeadline)
	defer cancel()
	u, err := b.GH.ArtifactURL(actx, name, id)
	switch {
	case github.IsKind(err, github.ErrGone):
		return model.DownloadURL{}, &api.Error{Status: http.StatusGone, Msg: "artifact has expired"}
	case err != nil:
		return model.DownloadURL{}, detailErr(err, b.Store.Config().Owner, name)
	case !downloadHost(u):
		return model.DownloadURL{}, &api.Error{Status: http.StatusBadGateway, Msg: "GitHub returned an unexpected download address"}
	}
	return model.DownloadURL{URL: u}, nil
}
```

The host check is the only thing between GitHub's answer and a browser navigation, so it compares the parsed host's suffix with its leading dot; `evilblob.core.windows.net` and `blob.core.windows.net.example.com` both fail.

- [ ] **Step 4: Run the tests with the race detector**

Run: `timeout 300 go test -race ./internal/daemon/ -run 'Artifact|DownloadHost' -count=1`
Expected: PASS. If the race detector is unavailable, run without `-race` and tell the owner.

- [ ] **Step 5: Format, vet and run the package**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/ && timeout 300 go test ./internal/daemon/ -count=1`
Expected: no `gofmt` output; vet and tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/artifacts.go internal/daemon/artifacts_test.go
git commit -m "feat(daemon): hand out checked artifact downloads"
```

---

### Task 11: API routes

**Files:**
- Modify: `internal/api/server.go` (imports at lines 4-19, `FromGitHub` at 49-50, the `Backend` interface after line 80, the routes before line 124, helpers before `pausePatch` at line 302)
- Test: `internal/api/api_test.go` (`fakeBackend` fields at line 59, methods before line 76, a row in `TestGitHubErrorsMapToStatuses` after line 305, new tests at the end)

**Interfaces:**
- Consumes: the daemon methods of Tasks 7 to 10 and the model types (Contracts, Go — daemon, model); `github.ErrGone` (Task 1)
- Produces: the `api.Backend` additions and the five routes (Contracts, Go — API)

**Items:** 1, 3, 4, 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/api/api_test.go`, add two fields at the end of `fakeBackend` (after `actionsCalls  int`, line 59):

```go
	runCalls      []string // "<method> <repo> <id>", plus the attempt for run calls
	runErr        error    // returned by the five run details methods
```

Add these methods before `func (f *fakeBackend) Actions` (line 76):

```go
func (f *fakeBackend) RunDetail(ctx context.Context, repo string, id int64, attempt int) (model.RunDetail, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("RunDetail %s %d %d", repo, id, attempt))
	return model.RunDetail{Repo: repo, ID: id, Attempt: attempt}, f.runErr
}

func (f *fakeBackend) RunAnnotations(ctx context.Context, repo string, id int64, attempt int) (model.RunAnnotations, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("RunAnnotations %s %d %d", repo, id, attempt))
	return model.RunAnnotations{Items: []model.Annotation{}, Omitted: map[int64]int{id: 1}}, f.runErr
}

func (f *fakeBackend) JobLog(ctx context.Context, repo string, jobID int64) (model.JobLog, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("JobLog %s %d", repo, jobID))
	return model.JobLog{JobID: jobID, Steps: []model.LogStep{}}, f.runErr
}

func (f *fakeBackend) RawJobLog(ctx context.Context, repo string, jobID int64) (model.RawLog, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("RawJobLog %s %d", repo, jobID))
	return model.RawLog{Text: "raw log"}, f.runErr
}

func (f *fakeBackend) ArtifactDownload(ctx context.Context, repo string, id int64) (model.DownloadURL, error) {
	f.runCalls = append(f.runCalls, fmt.Sprintf("ArtifactDownload %s %d", repo, id))
	return model.DownloadURL{URL: "https://example.blob.core.windows.net/a.zip"}, f.runErr
}
```

In `TestGitHubErrorsMapToStatuses`, add a row after the `ErrUnprocessable` row (line 305):

```go
		{&github.APIError{Status: 410, Kind: github.ErrGone, Message: "Gone"}, 410, false},
```

Append to the end of the file:

```go
func TestRunDetailRoutes(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	var d model.RunDetail
	if err := c.call(ctx, http.MethodGet, "/actions/darkmem/runs/102?attempt=2", nil, &d); err != nil || d.ID != 102 || d.Attempt != 2 {
		t.Fatalf("run %+v err %v", d, err)
	}
	var a model.RunAnnotations
	if err := c.call(ctx, http.MethodGet, "/actions/darkmem/runs/102/annotations", nil, &a); err != nil || a.Omitted[102] != 1 {
		t.Fatalf("annotations %+v err %v", a, err)
	}
	var l model.JobLog
	if err := c.call(ctx, http.MethodGet, "/actions/darkmem/jobs/1002/log", nil, &l); err != nil || l.JobID != 1002 {
		t.Fatalf("log %+v err %v", l, err)
	}
	var raw model.RawLog
	if err := c.call(ctx, http.MethodGet, "/actions/darkmem/jobs/1002/log/raw", nil, &raw); err != nil || raw.Text != "raw log" {
		t.Fatalf("raw %+v err %v", raw, err)
	}
	var u model.DownloadURL
	if err := c.call(ctx, http.MethodGet, "/actions/darkmem/artifacts/11/download", nil, &u); err != nil || u.URL == "" {
		t.Fatalf("download %+v err %v", u, err)
	}
	want := []string{"RunDetail darkmem 102 2", "RunAnnotations darkmem 102 0", "JobLog darkmem 1002", "RawJobLog darkmem 1002",
		"ArtifactDownload darkmem 11"}
	if !reflect.DeepEqual(b.runCalls, want) {
		t.Fatalf("calls %v", b.runCalls)
	}
}

func TestRunDetailRoutesRefuseBadNumbers(t *testing.T) {
	c, b := setup(t)
	for path, msg := range map[string]string{
		"/actions/darkmem/runs/abc":                       "run id must be a positive integer",
		"/actions/darkmem/runs/0/annotations":             "run id must be a positive integer",
		"/actions/darkmem/runs/102?attempt=0":             "attempt must be a positive integer",
		"/actions/darkmem/runs/102/annotations?attempt=x": "attempt must be a positive integer",
		"/actions/darkmem/runs/102?attempt=99999999999":   "attempt must be a positive integer",
		"/actions/darkmem/jobs/-4/log":                    "job id must be a positive integer",
		"/actions/darkmem/jobs/1.5/log/raw":               "job id must be a positive integer",
		"/actions/darkmem/artifacts/x/download":           "artifact id must be a positive integer",
	} {
		var ae *Error
		if err := c.call(context.Background(), http.MethodGet, path, nil, nil); !errors.As(err, &ae) || ae.Status != 400 || ae.Msg != msg {
			t.Errorf("%s: %v", path, err)
		}
	}
	if len(b.runCalls) != 0 {
		t.Fatalf("a bad number reached the backend: %v", b.runCalls)
	}
}

func TestRunDetailRoutesPassBackendStatuses(t *testing.T) {
	c, b := setup(t)
	for _, tc := range []struct {
		path string
		err  error
	}{
		{"/actions/nope/runs/1", NotFound("repo nope is not configured or watched")},
		{"/actions/darkmem/runs/1?attempt=3", BadRequest("run 1 has no attempt 3")},
		{"/actions/darkmem/jobs/1/log", Conflict("job has not finished")},
		{"/actions/darkmem/jobs/1/log/raw", &Error{Status: http.StatusRequestEntityTooLarge, Msg: "log is larger than 32 MiB; open it on GitHub"}},
		{"/actions/darkmem/artifacts/1/download", &Error{Status: http.StatusGone, Msg: "artifact has expired"}},
		{"/actions/darkmem/artifacts/1/download", &Error{Status: http.StatusBadGateway, Msg: "GitHub returned an unexpected download address"}},
	} {
		b.runErr = tc.err
		var want, ae *Error
		errors.As(tc.err, &want)
		if err := c.call(context.Background(), http.MethodGet, tc.path, nil, nil); !errors.As(err, &ae) || ae.Status != want.Status || ae.Msg != want.Msg {
			t.Errorf("%s: %v", tc.path, err)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/api/ -count=1`
Expected: FAIL: `TestGitHubErrorsMapToStatuses` reports the 410 row as 502, and the three new tests fail because the routes do not exist (`the running ghr daemon does not know GET /actions/darkmem/runs/102?attempt=2…`).

- [ ] **Step 3: Add the routes**

In `internal/api/server.go`:

1. Add `"math"` to the imports, between `"io"` and `"net/http"`.
2. In `FromGitHub`, add a case after the `github.ErrUnprocessable` case:

```go
	case github.ErrGone:
		return &Error{Status: http.StatusGone, Msg: err.Error()}
```

3. In the `Backend` interface, after `Actions(ctx context.Context) (model.Actions, error)` (line 80):

```go
	RunDetail(ctx context.Context, repo string, id int64, attempt int) (model.RunDetail, error)
	RunAnnotations(ctx context.Context, repo string, id int64, attempt int) (model.RunAnnotations, error)
	JobLog(ctx context.Context, repo string, jobID int64) (model.JobLog, error)
	RawJobLog(ctx context.Context, repo string, jobID int64) (model.RawLog, error)
	ArtifactDownload(ctx context.Context, repo string, id int64) (model.DownloadURL, error)
```

4. In `NewServer`, after the `GET /actions` handler and before `mux.HandleFunc("GET /status", …)` (line 124):

```go
	mux.HandleFunc("GET /actions/{repo}/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, attempt, err := runParams(r)
		if err != nil {
			respond(w, nil, err)
			return
		}
		d, err := b.RunDetail(r.Context(), r.PathValue("repo"), id, attempt)
		respond(w, d, err)
	})
	mux.HandleFunc("GET /actions/{repo}/runs/{id}/annotations", func(w http.ResponseWriter, r *http.Request) {
		id, attempt, err := runParams(r)
		if err != nil {
			respond(w, nil, err)
			return
		}
		a, err := b.RunAnnotations(r.Context(), r.PathValue("repo"), id, attempt)
		respond(w, a, err)
	})
	mux.HandleFunc("GET /actions/{repo}/jobs/{jobId}/log", func(w http.ResponseWriter, r *http.Request) {
		id, err := positive(r.PathValue("jobId"), "job id")
		if err != nil {
			respond(w, nil, err)
			return
		}
		l, err := b.JobLog(r.Context(), r.PathValue("repo"), id)
		respond(w, l, err)
	})
	mux.HandleFunc("GET /actions/{repo}/jobs/{jobId}/log/raw", func(w http.ResponseWriter, r *http.Request) {
		id, err := positive(r.PathValue("jobId"), "job id")
		if err != nil {
			respond(w, nil, err)
			return
		}
		l, err := b.RawJobLog(r.Context(), r.PathValue("repo"), id)
		respond(w, l, err)
	})
	mux.HandleFunc("GET /actions/{repo}/artifacts/{id}/download", func(w http.ResponseWriter, r *http.Request) {
		id, err := positive(r.PathValue("id"), "artifact id")
		if err != nil {
			respond(w, nil, err)
			return
		}
		d, err := b.ArtifactDownload(r.Context(), r.PathValue("repo"), id)
		respond(w, d, err)
	})
```

5. Before `func pausePatch` (line 302):

```go
// positive parses a path or query value that must be a positive integer.
func positive(v, name string) (int64, error) {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, BadRequest(name + " must be a positive integer")
	}
	return n, nil
}

// attemptParam reads ?attempt=; absent means 0, the latest attempt.
func attemptParam(r *http.Request) (int, error) {
	v := r.URL.Query().Get("attempt")
	if v == "" {
		return 0, nil
	}
	n, err := positive(v, "attempt")
	if err != nil || n > math.MaxInt32 {
		return 0, BadRequest("attempt must be a positive integer")
	}
	return int(n), nil
}

func runParams(r *http.Request) (int64, int, error) {
	id, err := positive(r.PathValue("id"), "run id")
	if err != nil {
		return 0, 0, err
	}
	attempt, err := attemptParam(r)
	return id, attempt, err
}
```

`*daemon.Backend` already has the five methods (Tasks 7 to 10), so `var _ api.Backend = (*Backend)(nil)` in `internal/daemon/backend.go` keeps compiling.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/api/ -count=1 -v -run 'RunDetailRoutes|GitHubErrorsMapToStatuses'`
Expected: PASS for `TestGitHubErrorsMapToStatuses`, `TestRunDetailRoutes`, `TestRunDetailRoutesRefuseBadNumbers`, `TestRunDetailRoutesPassBackendStatuses`.

- [ ] **Step 5: Format, vet and run both packages**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/ && timeout 300 go test ./internal/api/ ./internal/daemon/ -count=1`
Expected: no `gofmt` output; vet and tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/api/server.go internal/api/api_test.go
git commit -m "feat(api): serve run details, logs and downloads"
```

---

### Task 12: Fixtures

**Files:**
- Modify: `web/fixtures_test.go` (`"fmt"` import after line 5, three entries after the `toolchain-choices` entry at line 225, helper functions after `fixtureValues`)
- Create (generated): `web/src/api/fixtures/run-detail.json`, `web/src/api/fixtures/run-annotations.json`, `web/src/api/fixtures/job-log.json`

**Interfaces:**
- Consumes: the model types (Task 3)
- Produces: the three fixture files with exactly the values in Contracts, Fixtures; Task 13 types them and the web tasks assert on them

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Add the fixture values**

In `web/fixtures_test.go`, add `"fmt"` to the imports after `"encoding/json"`. In `fixtureValues`, add three entries after the `"toolchain-choices"` entry, so the map ends:

```go
		"toolchain-choices": []model.ToolchainChoice{{Spec: "22.11.0", Version: "22.11.0", LTS: true}, {Spec: "24.9.0", Version: "24.9.0"}},
		"run-detail":        runDetailFixture(),
		"run-annotations":   runAnnotationsFixture(),
		"job-log":           jobLogFixture(),
	}
}
```

Append after `fixtureValues`:

```go
// runAt is a time of the run-detail fixture's run, on the fixture day.
func runAt(min, sec int) *time.Time {
	t := time.Date(2026, 10, 3, 13, min, sec, 0, time.UTC)
	return &t
}

// fixtureStep is a completed step; a nil start leaves both times out.
func fixtureStep(n int, name, conclusion string, start, end *time.Time) model.Step {
	return model.Step{Number: n, Name: name, Status: "completed", Conclusion: conclusion, StartedAt: start, CompletedAt: end}
}

func fixtureJob(id int64, name, conclusion string, start, end *time.Time, runner string, labels []string, ghr bool, steps ...model.Step) model.RunJob {
	return model.RunJob{ID: id, Name: name, Status: "completed", Conclusion: conclusion, StartedAt: start, CompletedAt: end,
		RunnerName: runner, Labels: labels, HTMLURL: fmt.Sprintf("https://github.com/darkraise/darkmem/actions/runs/102/job/%d", id),
		GHR: ghr, Steps: steps}
}

func runDetailFixture() model.RunDetail {
	ghrLabels := []string{"self-hosted", "homelab"}
	hosted := []string{"ubuntu-latest"}
	return model.RunDetail{
		Repo: "darkmem", ID: 102, RunNumber: 42, Attempt: 2, Attempts: 2, Workflow: "ci", Title: "Fix the cache key",
		Branch: "fix/cache-key", Event: "pull_request", Actor: "octo-dev", Status: "completed", Conclusion: "failure",
		HeadSHA: "a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9", Path: ".github/workflows/ci.yml", PullRequests: []int{87},
		StartedAt: *runAt(55, 0), UpdatedAt: *runAt(59, 10), HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/102",
		Jobs: []model.RunJob{
			fixtureJob(1001, "Pick runner", "success", runAt(55, 0), runAt(55, 20), "ghr-darkmem-a1b2c3", ghrLabels, true,
				fixtureStep(1, "Set up job", "success", runAt(55, 0), runAt(55, 5)),
				fixtureStep(2, "Pick", "success", runAt(55, 5), runAt(55, 19)),
				fixtureStep(3, "Complete job", "success", runAt(55, 19), runAt(55, 20))),
			fixtureJob(1002, "Backend tests", "failure", runAt(55, 25), runAt(57, 40), "ghr-darkmem-d4e5f6", ghrLabels, true,
				fixtureStep(1, "Set up job", "success", runAt(55, 25), runAt(55, 30)),
				fixtureStep(2, "Install", "success", runAt(55, 30), runAt(56, 10)),
				fixtureStep(3, "Lint (ruff)", "failure", runAt(56, 10), runAt(57, 35)),
				fixtureStep(4, "Test", "skipped", nil, nil),
				fixtureStep(5, "Complete job", "success", runAt(57, 35), runAt(57, 40))),
			fixtureJob(1003, "Frontend tests (1/2)", "success", runAt(55, 25), runAt(58, 0), "GitHub Actions 2", hosted, false,
				fixtureStep(1, "Set up job", "success", runAt(55, 25), runAt(55, 30)),
				fixtureStep(2, "Test", "success", runAt(55, 30), runAt(57, 55)),
				fixtureStep(3, "Complete job", "success", runAt(57, 55), runAt(58, 0))),
			fixtureJob(1004, "Frontend tests (2/2)", "success", runAt(55, 26), runAt(58, 30), "GitHub Actions 3", hosted, false,
				fixtureStep(1, "Set up job", "success", runAt(55, 26), runAt(55, 31)),
				fixtureStep(2, "Test", "success", runAt(55, 31), runAt(58, 25)),
				fixtureStep(3, "Complete job", "success", runAt(58, 25), runAt(58, 30))),
			fixtureJob(1005, "E2E (api)", "success", runAt(55, 30), runAt(59, 10), "GitHub Actions 4", hosted, false,
				fixtureStep(1, "Set up job", "success", runAt(55, 30), runAt(55, 35)),
				fixtureStep(2, "Test", "success", runAt(55, 35), runAt(59, 5)),
				fixtureStep(3, "Complete job", "success", runAt(59, 5), runAt(59, 10))),
		},
		Graph: &model.RunGraph{Nodes: []model.GraphNode{
			{Key: "pick-runner", Name: "Pick runner", Column: 0, Needs: []string{}, JobIDs: []int64{1001}},
			{Key: "e2e", Name: "E2E (${{ matrix.suite }})", Column: 0, Needs: []string{}, JobIDs: []int64{1005}, Group: true},
			{Key: "backend", Name: "Backend tests", Column: 1, Needs: []string{"pick-runner"}, JobIDs: []int64{1002}},
			{Key: "frontend-tests", Name: "Frontend tests (${{ matrix.shard }}/2)", Column: 1, Needs: []string{"pick-runner"},
				JobIDs: []int64{1003, 1004}, Group: true},
		}},
		Artifacts: []model.RunArtifact{
			{ID: 11, Name: "frontend-dist", SizeBytes: 2_400_000, ExpiresAt: ptr(time.Date(2026, 10, 17, 13, 59, 0, 0, time.UTC))},
			{ID: 12, Name: "coverage-backend", SizeBytes: 51_200, Expired: true, ExpiresAt: ptr(time.Date(2026, 10, 2, 13, 59, 0, 0, time.UTC))},
		},
		FetchedAt: fixtureTime,
	}
}

func runAnnotationsFixture() model.RunAnnotations {
	return model.RunAnnotations{
		Items: []model.Annotation{
			{Level: "error", JobID: 1002, JobName: "Backend tests", Step: 3, StepName: "Lint (ruff)", Line: 5,
				Message: "src/cache.py:12:5: F401 'os' imported but unused"},
			{Level: "error", JobID: 1002, JobName: "Backend tests", Step: 3, StepName: "Lint (ruff)", Line: 7,
				Message: "Process completed with exit code 1."},
			{Level: "warning", JobID: 1003, JobName: "Frontend tests (1/2)", Step: 2, StepName: "Test", Line: 2,
				Message: "Node.js 16 actions are deprecated."},
			{Level: "notice", JobID: 1005, JobName: "E2E (api)", Step: 2, StepName: "Test", Line: 3, Message: "Retried 1 flaky test"},
		},
		Omitted: map[int64]int{1002: 3},
	}
}

func jobLogFixture() model.JobLog {
	line := func(n int, kind, text string) model.LogLine { return model.LogLine{N: n, Kind: kind, Text: text} }
	return model.JobLog{JobID: 1002, Steps: []model.LogStep{
		{Number: 1, Name: "Set up job", Lines: []model.LogLine{
			line(1, "", "Current runner version: '2.338.0'"), line(2, "group", "Runner Image"),
			line(3, "", "Image: ubuntu-24.04"), line(4, "endgroup", ""),
		}},
		{Number: 2, Name: "Install", Lines: []model.LogLine{
			line(1, "command", "pip install -r requirements.txt"), line(2, "", "Successfully installed ruff-0.6.9"),
		}},
		{Number: 3, Name: "Lint (ruff)", Lines: []model.LogLine{
			line(1, "group", "Run ruff check ."), line(2, "", "ruff check ."), line(3, "endgroup", ""),
			line(4, "", "\x1b[31mFound 1 error.\x1b[0m"),
			line(5, "error", "src/cache.py:12:5: F401 'os' imported but unused"),
			line(6, "debug", "Finishing: Lint (ruff)"),
			line(7, "error", "Process completed with exit code 1."),
		}},
		{Number: 4, Name: "Test", Lines: []model.LogLine{}},
		{Number: 5, Name: "Complete job", Lines: []model.LogLine{line(1, "", "Cleaning up orphan processes")}},
	}}
}
```

The `"\x1b[31mFound 1 error.\x1b[0m"` literal is Go's escape for the ESC byte; the JSON encoder writes it as `\u001b`.

- [ ] **Step 2: Run the fixture test to verify it fails**

Run: `timeout 300 go test ./web/ -count=1`
Expected: FAIL with `web/src/api/fixtures/run-detail.json is out of date with the Go types` (and the same for `run-annotations.json` and `job-log.json`).

- [ ] **Step 3: Generate the fixtures**

Run: `GHR_UPDATE_FIXTURES=1 timeout 300 go test ./web/ -count=1`
Expected: PASS, and the three JSON files exist under `web/src/api/fixtures/`.

- [ ] **Step 4: Run the fixture test and spot-check the files**

Run: `timeout 300 go test ./web/ -count=1 && grep -c '"kind": ""' web/src/api/fixtures/job-log.json && grep -n 'u001b\|"omitted"\|"graph_note"' web/src/api/fixtures/*.json`
Expected: PASS; `6` empty kinds (the plain lines keep `"kind": ""`); the ANSI line reads `"text": "\u001b[31mFound 1 error.\u001b[0m"`; `"omitted"` appears in `run-annotations.json` with `"1002": 3`; no `graph_note` anywhere (the fixture has a graph).

- [ ] **Step 5: Format and vet**

Run: `gofmt -l internal cmd web/*.go && go vet ./internal/... ./cmd/... ./web/`
Expected: no output from `gofmt`, `go vet` passes.

- [ ] **Step 6: Commit**

```bash
git add web/fixtures_test.go web/src/api/fixtures/run-detail.json web/src/api/fixtures/run-annotations.json web/src/api/fixtures/job-log.json
git commit -m "test(web): add run detail, annotation and log fixtures"
```

---

### Task 13: TS types and test fixtures

**Files:**
- Modify: `web/src/api/types.ts` (append after `interface Actions` at the end of the file)
- Modify: `web/src/test/fixtures.ts` (imports, the typed constants, the `fixtures` object, a new `runRoutes` after `authedRoutes`)
- Test: `web/src/api/types.test.ts`

**Interfaces:**
- Consumes: `web/src/api/fixtures/run-detail.json`, `run-annotations.json`, `job-log.json` generated by Task 12 (Contracts, Fixtures)
- Produces: the run detail types, `fixtures.runDetail`, `fixtures.runAnnotations`, `fixtures.jobLog` and `runRoutes` (Contracts, TypeScript — types and test fixtures)

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

In `web/src/api/types.test.ts`, change the fixtures import to `import { fixtures, runRoutes } from "@/test/fixtures"`, add three JSON imports in alphabetical place (after `historyJson`, and after `registrationsJson`):

```ts
import jobLogJson from "./fixtures/job-log.json"
```

```ts
import runAnnotationsJson from "./fixtures/run-annotations.json"
import runDetailJson from "./fixtures/run-detail.json"
```

add `JobLog` after `HistoryEntry`, and `RunAnnotations`, `RunDetail` after `Registration`, in the `import type { … } from "./types"` list, then append at the end of the file:

```ts
describe("run detail fixtures", () => {
  const run: RunDetail = runDetailJson
  const annotations: RunAnnotations = runAnnotationsJson
  const jobLog: JobLog = jobLogJson

  it("the run carries its attempts, jobs, graph and artifacts", () => {
    expect([run.id, run.run_number, run.attempt, run.attempts]).toEqual([102, 42, 2, 2])
    expect(run.pull_requests).toEqual([87])
    expect(run.jobs.map((j) => j.id)).toEqual([1001, 1002, 1003, 1004, 1005])
    expect(run.jobs[1]?.steps[3]).toEqual({ number: 4, name: "Test", status: "completed", conclusion: "skipped" })
    expect(run.graph?.nodes.map((n) => [n.key, n.column, n.group])).toEqual([
      ["pick-runner", 0, false],
      ["e2e", 0, true],
      ["backend", 1, false],
      ["frontend-tests", 1, true],
    ])
    expect(run.graph?.nodes[3]?.job_ids).toEqual([1003, 1004])
    expect(run).not.toHaveProperty("graph_note")
    expect(run.artifacts.map((a) => [a.name, a.expired])).toEqual([
      ["frontend-dist", false],
      ["coverage-backend", true],
    ])
  })

  it("annotations key the omitted counts by job ID", () => {
    expect(annotations.items.map((a) => a.level)).toEqual(["error", "error", "warning", "notice"])
    expect(annotations.omitted).toEqual({ "1002": 3 })
    expect([annotations.unread, annotations.expired_logs, annotations.too_large, annotations.failed]).toEqual([0, 0, 0, 0])
  })

  it("the job log keeps every kind and ANSI in the text", () => {
    expect(jobLog.steps.map((s) => s.lines.length)).toEqual([4, 2, 7, 0, 1])
    expect(jobLog.steps[2]?.lines[3]?.text).toBe("\u001b[31mFound 1 error.\u001b[0m")
    expect(jobLog.steps[2]?.lines.map((l) => l.kind)).toEqual(["group", "", "endgroup", "", "error", "debug", "error"])
  })

  it("runRoutes serves run 102 and lets a test replace one route", () => {
    expect(runRoutes()["GET /api/actions/darkmem/jobs/1002/log"]).toBe(fixtures.jobLog)
    expect(runRoutes()["GET /api/status"]).toBe(fixtures.status)
    expect(runRoutes({ "GET /api/actions/darkmem/runs/102": 1 })["GET /api/actions/darkmem/runs/102"]).toBe(1)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && timeout 300 npx vitest run src/api/types.test.ts`
Expected: FAIL in "run detail fixtures" with `runRoutes is not a function`. `timeout 300 npm run typecheck` also fails: `Module "./types" has no exported member 'RunDetail'`.

- [ ] **Step 3: Add the types and the test fixtures**

Append to `web/src/api/types.ts`:

```ts
export interface RunJob {
  id: number
  name: string
  status: string
  conclusion: string
  started_at?: string
  completed_at?: string
  runner_name: string
  labels: string[]
  html_url: string
  ghr: boolean
  steps: Step[]
}

export interface GraphNode {
  key: string
  name: string
  column: number
  needs: string[]
  job_ids: number[]
  group: boolean
}

export interface RunGraph {
  nodes: GraphNode[]
}

export interface RunArtifact {
  id: number
  name: string
  size_bytes: number
  expired: boolean
  expires_at?: string
}

export interface RunDetail {
  repo: string
  watched: boolean
  id: number
  run_number: number
  attempt: number
  attempts: number
  workflow: string
  title: string
  branch: string
  event: string
  actor: string
  status: string
  conclusion: string
  head_sha: string
  path: string
  pull_requests: number[]
  started_at: string
  updated_at: string
  html_url: string
  jobs: RunJob[]
  graph?: RunGraph
  graph_note?: string
  artifacts: RunArtifact[]
  fetched_at: string
}

export interface Annotation {
  level: string
  job_id: number
  job_name: string
  step: number
  step_name: string
  line: number
  message: string
}

// omitted maps a job ID, as a JSON object key, to the annotations beyond the
// per-job cap.
export interface RunAnnotations {
  items: Annotation[]
  omitted: Record<string, number>
  unread: number
  expired_logs: number
  too_large: number
  failed: number
}

export interface LogLine {
  n: number
  kind: string
  text: string
}

export interface LogStep {
  number: number
  name: string
  lines: LogLine[]
}

export interface JobLog {
  job_id: number
  too_large: boolean
  expired: boolean
  steps: LogStep[]
}

export interface RawLog {
  text: string
}

export interface DownloadURL {
  url: string
}
```

In `web/src/test/fixtures.ts`, add the JSON imports (after `historyJson`, and after `registrationsJson`):

```ts
import jobLogJson from "@/api/fixtures/job-log.json"
```

```ts
import runAnnotationsJson from "@/api/fixtures/run-annotations.json"
import runDetailJson from "@/api/fixtures/run-detail.json"
```

add `JobLog` after `HistoryEntry`, and `RunAnnotations`, `RunDetail` after `Registration`, in the type import list; after `const actions: Actions = actionsJson` add:

```ts
const runDetail: RunDetail = runDetailJson
const runAnnotations: RunAnnotations = runAnnotationsJson
const jobLog: JobLog = jobLogJson
```

add `runDetail,`, `runAnnotations,` and `jobLog,` after `actions,` in the `fixtures` object, and append at the end of the file:

```ts
// The run page's routes for run 102 of darkmem and its failed job 1002.
export function runRoutes(over: Record<string, unknown> = {}): Record<string, unknown> {
  return authedRoutes({
    "GET /api/actions/darkmem/runs/102": fixtures.runDetail,
    "GET /api/actions/darkmem/runs/102/annotations": fixtures.runAnnotations,
    "GET /api/actions/darkmem/jobs/1002/log": fixtures.jobLog,
    ...over,
  })
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && timeout 300 npx vitest run src/api/types.test.ts`
Expected: PASS (14 tests).

- [ ] **Step 5: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both pass. Assigning each JSON fixture to its type is what proves the Go JSON names match.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/types.ts web/src/api/types.test.ts web/src/test/fixtures.ts
git commit -m "feat(web): add run detail types and fixtures"
```

---

### Task 14: API client and hooks

**Files:**
- Modify: `web/src/api/client.ts` (the type import list; the `api` object after `actions`)
- Modify: `web/src/api/hooks.ts` (the type import; `keys` after `actions`; new functions after `useActions`)
- Test: `web/src/api/client-pages.test.ts`

**Interfaces:**
- Consumes: the run detail types and `fixtures` (Task 13; Contracts, TypeScript — types and test fixtures)
- Produces: `api.runDetail`, `api.runAnnotations`, `api.jobLog`, `api.rawJobLog`, `api.artifactDownload`, `keys.runDetail`, `keys.runAnnotations`, `keys.jobLog`, `useRunDetail`, `useRunAnnotations`, `useJobLog` (Contracts, TypeScript — API client and hooks), plus `runDetailPoll` and `annotationsPoll` (Contracts, same section)

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Replace the imports at the top of `web/src/api/client-pages.test.ts` with:

```ts
import { renderHook } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { api } from "./client"
import { annotationsPoll, keys, POLL_ACTIONS, POLL_SLOW, runDetailPoll, useJobLog, useRunAnnotations, useRunDetail } from "./hooks"
```

and append at the end of the file:

```ts
describe("api for the run pages", () => {
  it.each([
    ["runDetail latest", () => api.runDetail("dark mem", 102, undefined), "/api/actions/dark%20mem/runs/102", ""],
    ["runDetail attempt", () => api.runDetail("darkmem", 102, 1), "/api/actions/darkmem/runs/102", "?attempt=1"],
    ["runAnnotations latest", () => api.runAnnotations("darkmem", 102, undefined), "/api/actions/darkmem/runs/102/annotations", ""],
    ["runAnnotations attempt", () => api.runAnnotations("darkmem", 102, 2), "/api/actions/darkmem/runs/102/annotations", "?attempt=2"],
    ["jobLog", () => api.jobLog("darkmem", 1002), "/api/actions/darkmem/jobs/1002/log", ""],
    ["rawJobLog", () => api.rawJobLog("darkmem", 1002), "/api/actions/darkmem/jobs/1002/log/raw", ""],
    ["artifactDownload", () => api.artifactDownload("darkmem", 11), "/api/actions/darkmem/artifacts/11/download", ""],
  ])("%s reads its route", async (_name, call, path, search) => {
    const { calls } = mockApi({ [`GET ${path}`]: { ok: true } })
    await call()
    expect(calls).toHaveLength(1)
    expect(calls[0]).toMatchObject({ method: "GET", path, search })
  })

  it("keys a run by its lower-case repository, ID and attempt", () => {
    expect(keys.runDetail("DarkMem", 102, undefined)).toEqual(["run-detail", "darkmem", 102, 0])
    expect(keys.runAnnotations("darkmem", 102, 1)).toEqual(["run-annotations", "darkmem", 102, 1])
    expect(keys.jobLog("DarkMem", 1002)).toEqual(["job-log", "darkmem", 1002])
  })

  it("polls a run only while its attempt is not completed", () => {
    expect(runDetailPoll(undefined)).toBe(false)
    expect(runDetailPoll(fixtures.runDetail)).toBe(false)
    expect(runDetailPoll({ ...fixtures.runDetail, status: "in_progress" })).toBe(POLL_ACTIONS)
  })

  it.each([
    ["no data on an active run", undefined, true, POLL_ACTIONS],
    ["no data on a finished run", undefined, false, false],
    ["unread logs", { ...fixtures.runAnnotations, unread: 2 }, false, POLL_SLOW],
    ["every log read on an active run", fixtures.runAnnotations, true, POLL_ACTIONS],
    ["every log read on a finished run", fixtures.runAnnotations, false, false],
  ])("polls annotations with %s", (_why, data, active, want) => {
    expect(annotationsPoll(data, active)).toBe(want)
  })

  it("reads nothing while the hooks are disabled", async () => {
    const { calls } = mockApi({})
    const { wrapper } = withQuery()
    renderHook(() => useRunDetail("darkmem", 102, undefined, false), { wrapper })
    renderHook(() => useRunAnnotations("darkmem", 102, undefined, true, false), { wrapper })
    renderHook(() => useJobLog("darkmem", 1002, false), { wrapper })
    await new Promise((r) => setTimeout(r, 100))
    expect(calls).toHaveLength(0)
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/api/client-pages.test.ts`
Expected: FAIL in "api for the run pages" (`api.runDetail is not a function`, `keys.runDetail is not a function`, `runDetailPoll is not a function`).

- [ ] **Step 3: Add the API calls**

In `web/src/api/client.ts`, add `DownloadURL` after `Container`, `JobLog` after `HistoryEntry`, `RawLog` after `PruneScope`, and `RunAnnotations`, `RunDetail` after `Registration` in the `import type { … } from "./types"` list. In the `api` object, directly after the `actions:` entry, add:

```ts
  runDetail: (repo: string, id: number, attempt: number | undefined, signal?: AbortSignal) =>
    request<RunDetail>("GET", `/api/actions/${seg(repo)}/runs/${id}${attempt ? `?${query({ attempt })}` : ""}`, undefined, signal),
  runAnnotations: (repo: string, id: number, attempt: number | undefined, signal?: AbortSignal) =>
    request<RunAnnotations>(
      "GET",
      `/api/actions/${seg(repo)}/runs/${id}/annotations${attempt ? `?${query({ attempt })}` : ""}`,
      undefined,
      signal,
    ),
  jobLog: (repo: string, jobId: number, signal?: AbortSignal) =>
    request<JobLog>("GET", `/api/actions/${seg(repo)}/jobs/${jobId}/log`, undefined, signal),
  rawJobLog: (repo: string, jobId: number) => request<RawLog>("GET", `/api/actions/${seg(repo)}/jobs/${jobId}/log/raw`),
  artifactDownload: (repo: string, id: number) => request<DownloadURL>("GET", `/api/actions/${seg(repo)}/artifacts/${id}/download`),
```

- [ ] **Step 4: Add the keys and hooks**

In `web/src/api/hooks.ts`, change the type import to:

```ts
import type { ActivityWindow, GhrEvent, LogChunk, RunAnnotations, RunDetail, Status, Storage } from "./types"
```

add to `keys`, directly after `actions: ["actions"] as const,`:

```ts
  runDetail: (repo: string, id: number, attempt: number | undefined) =>
    ["run-detail", repo.toLowerCase(), id, attempt ?? 0] as const,
  runAnnotations: (repo: string, id: number, attempt: number | undefined) =>
    ["run-annotations", repo.toLowerCase(), id, attempt ?? 0] as const,
  jobLog: (repo: string, jobId: number) => ["job-log", repo.toLowerCase(), jobId] as const,
```

and insert between `useActions` and `browserZone`:

```ts
// A run's details change only while the shown attempt runs.
export function runDetailPoll(data: RunDetail | undefined): number | false {
  return data && data.status !== "completed" ? POLL_ACTIONS : false
}

// The daemon answers within 20 seconds with the logs it has read, so jobs it
// is still reading are asked for again soon; new jobs finish only while the
// run is active.
export function annotationsPoll(data: RunAnnotations | undefined, active: boolean): number | false {
  if (data && data.unread > 0) return POLL_SLOW
  return active ? POLL_ACTIONS : false
}

export function useRunDetail(repo: string, id: number, attempt: number | undefined, enabled: boolean) {
  return useQuery({
    queryKey: keys.runDetail(repo, id, attempt),
    queryFn: ({ signal }) => api.runDetail(repo, id, attempt, signal),
    enabled,
    refetchInterval: (q) => runDetailPoll(q.state.data),
  })
}

export function useRunAnnotations(repo: string, id: number, attempt: number | undefined, active: boolean, enabled: boolean) {
  return useQuery({
    queryKey: keys.runAnnotations(repo, id, attempt),
    queryFn: ({ signal }) => api.runAnnotations(repo, id, attempt, signal),
    enabled,
    refetchInterval: (q) => annotationsPoll(q.state.data, active),
  })
}

export function useJobLog(repo: string, jobId: number, enabled: boolean) {
  return useQuery({ queryKey: keys.jobLog(repo, jobId), queryFn: ({ signal }) => api.jobLog(repo, jobId, signal), enabled })
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/api/client-pages.test.ts`
Expected: PASS (35 tests).

- [ ] **Step 6: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both pass.

- [ ] **Step 7: Commit**

```bash
git add web/src/api/client.ts web/src/api/hooks.ts web/src/api/client-pages.test.ts
git commit -m "feat(web): add run detail API calls and hooks"
```

---

### Task 15: Run helpers and status icon

**Files:**
- Create: `web/src/lib/run-details.ts`
- Create: `web/src/components/run-status-icon.tsx`
- Test: `web/src/lib/run-details.test.tsx` (helpers and the icon; `.tsx` because it renders the icon)

**Interfaces:**
- Consumes: the run detail types and `fixtures` (Task 13); `resultWord` (`web/src/lib/history.ts:81`); `ResultIcon` (`web/src/components/result-icon.tsx`)
- Produces: everything in Contracts, TypeScript — run helpers and status icon

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/run-details.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RunDetail } from "@/api/types"
import { RunStatusIcon } from "@/components/run-status-icon"
import { fixtures } from "@/test/fixtures"
import {
  annotationCounts,
  attemptLabel,
  firstFailedStep,
  githubLinks,
  isRunActive,
  jobMs,
  parseJobSearch,
  parseRunSearch,
  positiveInt,
  rawLogFileName,
  runDescription,
  runTotalMs,
  statusText,
} from "./run-details"

const run = fixtures.runDetail
const now = Date.parse("2026-10-03T14:05:00Z")

describe("route parameters", () => {
  it.each([
    [1, 1],
    ["42", 42],
    ["01", 1],
    [0, undefined],
    [-3, undefined],
    [1.5, undefined],
    ["1.5", undefined],
    ["abc", undefined],
    ["", undefined],
    ["99999999999999999999", undefined],
    [undefined, undefined],
    [null, undefined],
  ])("positiveInt(%j) is %j", (v, want) => {
    expect(positiveInt(v)).toBe(want)
  })

  it("keeps only positive integers in the search", () => {
    expect(parseRunSearch({ attempt: "2", step: 3 })).toEqual({ attempt: 2 })
    expect(parseRunSearch({ attempt: "x" })).toEqual({})
    expect(parseJobSearch({ attempt: 1, step: "3", line: 0, other: 5 })).toEqual({ attempt: 1, step: 3 })
  })
})

describe("run facts", () => {
  it("names the status of a run or job", () => {
    expect(isRunActive("completed")).toBe(false)
    expect(isRunActive("queued")).toBe(true)
    expect(statusText("completed", "failure")).toBe("Failed")
    expect(statusText("completed", "success")).toBe("Succeeded")
    expect(statusText("in_progress", "")).toBe("Running")
    expect(statusText("waiting", "")).toBe("Queued")
  })

  it("times a job from its start to its completion, or to now", () => {
    const backend = run.jobs[1]
    if (!backend) throw new Error("no job")
    expect(jobMs(backend, now)).toBe(135_000)
    expect(jobMs({ ...backend, completed_at: undefined }, Date.parse("2026-10-03T13:56:25Z"))).toBe(60_000)
    expect(jobMs({ ...backend, started_at: undefined, completed_at: undefined }, now)).toBeUndefined()
  })

  it("times the run to its last job's completion, or to now while active", () => {
    expect(runTotalMs(run, now)).toBe(250_000)
    const active: RunDetail = { ...run, status: "in_progress" }
    expect(runTotalMs(active, Date.parse("2026-10-03T13:57:00Z"))).toBe(120_000)
  })

  it("describes the run in one sentence", () => {
    expect(runDescription(run)).toBe("ci #42 in darkmem on fix/cache-key.")
    expect(runDescription({ ...run, branch: "" })).toBe("ci #42 in darkmem.")
  })

  it("builds GitHub links from the owner", () => {
    const links = githubLinks("darkraise", run)
    expect(links.commit).toBe("https://github.com/darkraise/darkmem/commit/a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9")
    expect(links.workflowFile).toBe(
      "https://github.com/darkraise/darkmem/blob/a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9/.github/workflows/ci.yml",
    )
    expect(links.pull(87)).toBe("https://github.com/darkraise/darkmem/pull/87")
    expect(githubLinks("darkraise", { ...run, path: "dynamic/dependabot/dependabot-updates" }).workflowFile).toBeUndefined()
  })

  it("labels attempts", () => {
    expect(attemptLabel(2, 2)).toBe("Attempt 2 (latest)")
    expect(attemptLabel(1, 2)).toBe("Attempt 1")
  })

  it("counts annotations by level", () => {
    expect(annotationCounts(fixtures.runAnnotations.items)).toEqual({ error: 2, warning: 1, notice: 1 })
    expect(annotationCounts([])).toEqual({ error: 0, warning: 0, notice: 0 })
  })

  it("finds the first failed step", () => {
    expect(firstFailedStep(run.jobs[1]?.steps ?? [])).toBe(3)
    expect(firstFailedStep(run.jobs[0]?.steps ?? [])).toBeUndefined()
  })

  it("names the raw log file", () => {
    expect(rawLogFileName("darkmem", 1002)).toBe("darkmem-job-1002.log")
  })
})

describe("RunStatusIcon", () => {
  it.each([
    ["completed", "success", "Succeeded"],
    ["completed", "failure", "Failed"],
    ["completed", "cancelled", "Cancelled"],
    ["completed", "skipped", "Skipped"],
    ["queued", "", "Queued"],
    ["waiting", "", "Queued"],
  ])("shows %s/%s as %s", (status, conclusion, name) => {
    render(<RunStatusIcon status={status} conclusion={conclusion} />)
    expect(screen.getByRole("img", { name })).toBeInTheDocument()
  })

  it("shows a running job as a spinner", () => {
    render(<RunStatusIcon status="in_progress" conclusion="" />)
    expect(screen.getByText("Running")).toHaveClass("sr-only")
    expect(screen.queryByRole("img", { name: "Queued" })).toBeNull()
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/lib/run-details.test.tsx`
Expected: FAIL: `Failed to resolve import "./run-details"`.

- [ ] **Step 3: Write the helpers**

Create `web/src/lib/run-details.ts`:

```ts
import type { Annotation, RunDetail, RunJob, Step } from "@/api/types"
import { resultWord } from "@/lib/history"

export interface RunSearch {
  attempt?: number
}

export interface JobSearch {
  attempt?: number
  step?: number
  line?: number
}

export function positiveInt(v: unknown): number | undefined {
  const n = typeof v === "number" ? v : typeof v === "string" && /^\d+$/.test(v) ? Number(v) : NaN
  return Number.isSafeInteger(n) && n > 0 ? n : undefined
}

function pick<K extends string>(search: Record<string, unknown>, names: K[]): Partial<Record<K, number>> {
  const out: Partial<Record<K, number>> = {}
  for (const name of names) {
    const n = positiveInt(search[name])
    if (n !== undefined) out[name] = n
  }
  return out
}

export function parseRunSearch(search: Record<string, unknown>): RunSearch {
  return pick(search, ["attempt"])
}

export function parseJobSearch(search: Record<string, unknown>): JobSearch {
  return pick(search, ["attempt", "step", "line"])
}

export const isRunActive = (status: string): boolean => status !== "completed"

export function statusText(status: string, conclusion: string): string {
  if (!isRunActive(status)) return resultWord(conclusion)
  return status === "in_progress" ? "Running" : "Queued"
}

export function jobMs(job: RunJob, now: number): number | undefined {
  if (!job.started_at) return undefined
  const end = job.completed_at ? Date.parse(job.completed_at) : now
  return end - Date.parse(job.started_at)
}

export function runTotalMs(run: RunDetail, now: number): number {
  const start = Date.parse(run.started_at)
  if (isRunActive(run.status)) return now - start
  let end = start
  for (const j of run.jobs) if (j.completed_at) end = Math.max(end, Date.parse(j.completed_at))
  return end - start
}

export function runDescription(run: RunDetail): string {
  const where = `${run.workflow} #${run.run_number} in ${run.repo}`
  return run.branch ? `${where} on ${run.branch}.` : `${where}.`
}

export function githubLinks(owner: string, run: RunDetail): { commit: string; workflowFile: string | undefined; pull: (n: number) => string } {
  const base = `https://github.com/${owner}/${run.repo}`
  return {
    commit: `${base}/commit/${run.head_sha}`,
    workflowFile: run.path.startsWith(".github/workflows/") ? `${base}/blob/${run.head_sha}/${run.path}` : undefined,
    pull: (n) => `${base}/pull/${n}`,
  }
}

export function attemptLabel(n: number, attempts: number): string {
  return n === attempts ? `Attempt ${n} (latest)` : `Attempt ${n}`
}

export interface AnnotationCounts {
  error: number
  warning: number
  notice: number
}

export function annotationCounts(items: Annotation[]): AnnotationCounts {
  const out: AnnotationCounts = { error: 0, warning: 0, notice: 0 }
  for (const a of items) if (a.level === "error" || a.level === "warning" || a.level === "notice") out[a.level]++
  return out
}

export function firstFailedStep(steps: Step[]): number | undefined {
  return steps.find((s) => s.conclusion === "failure")?.number
}

export function rawLogFileName(repo: string, jobId: number): string {
  return `${repo}-job-${jobId}.log`
}
```

- [ ] **Step 4: Write the status icon**

Create `web/src/components/run-status-icon.tsx`:

```tsx
import { Spinner } from "darkraise-ui/components/spinner"
import { Clock } from "lucide-react"
import { ResultIcon } from "@/components/result-icon"
import { isRunActive } from "@/lib/run-details"

export function RunStatusIcon({ status, conclusion }: { status: string; conclusion: string }) {
  if (!isRunActive(status)) return <ResultIcon conclusion={conclusion} />
  if (status === "in_progress") return <Spinner size="sm" label={<span className="sr-only">Running</span>} />
  return <Clock role="img" aria-label="Queued" size={15} className="shrink-0 text-muted-foreground" />
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/lib/run-details.test.tsx`
Expected: PASS (29 tests).

- [ ] **Step 6: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both pass.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/run-details.ts web/src/lib/run-details.test.tsx web/src/components/run-status-icon.tsx
git commit -m "feat(web): add run helpers and RunStatusIcon"
```

---

### Task 16: Run frame

**Files:**
- Create: `web/src/components/run-frame.tsx`
- Test: `web/src/components/run-frame.test.tsx`

**Interfaces:**
- Consumes: `RunDetail`, `RunJob`, `fixtures.runDetail` (Task 13); `attemptLabel`, `githubLinks`, `jobMs`, `runDescription` and `RunStatusIcon` (Task 15); `dur` (`web/src/lib/format.ts:5`)
- Produces: `RunFrame`, `JobLinkFn`, `JobLinkOptions`, `SummaryLinkFn` (Contracts, TypeScript — run frame)

**Items:** 1, 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

`PageHeader` reads the kit's router adapter and throws `useRouterAdapter must be used inside a <RouterAdapterProvider>` without one, so the test wraps the frame in a `RouterAdapterProvider` whose `Link` is a plain anchor; the job and summary links are stub anchors too, so the frame tests need no router.

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/run-frame.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { RouterAdapterProvider, type RouterAdapter } from "darkraise-ui/router"
import { describe, expect, it, vi } from "vitest"
import type { RunDetail } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { RunFrame, type JobLinkFn, type SummaryLinkFn } from "./run-frame"

// PageHeader's breadcrumbs link through the kit's router adapter; plain
// anchors stand in for the router here.
const adapter: RouterAdapter = {
  Link: ({ to, className, children }) => (
    <a href={to} className={className}>
      {children}
    </a>
  ),
  useNavigate: () => () => {},
  usePathname: () => "/",
  useBack: () => () => {},
  useInvalidate: () => () => {},
}

const jobLink: JobLinkFn = (jobId, children, opts) => (
  <a href={`#job-${jobId}`} className={opts?.className} aria-label={opts?.label} aria-current={opts?.current ? "page" : undefined}>
    {children}
  </a>
)
const summaryLink: SummaryLinkFn = (children, opts) => (
  <a href="#summary" className={opts.className} aria-current={opts.current ? "page" : undefined}>
    {children}
  </a>
)

const now = Date.parse("2026-10-03T14:05:00Z")

function renderFrame(over: Partial<Parameters<typeof RunFrame>[0]> = {}, run: RunDetail = fixtures.runDetail) {
  const onAttempt = vi.fn()
  const user = userEvent.setup()
  render(
    <RouterAdapterProvider value={adapter}>
      <RunFrame run={run} owner="darkraise" now={now} current="summary" jobLink={jobLink} summaryLink={summaryLink} onAttempt={onAttempt} {...over}>
        <p>Run content</p>
      </RunFrame>
    </RouterAdapterProvider>,
  )
  return { onAttempt, user }
}

describe("RunFrame", () => {
  it("heads the page with the run title, its description and breadcrumbs", () => {
    renderFrame()
    expect(screen.getByRole("heading", { name: "Fix the cache key" })).toBeInTheDocument()
    expect(screen.getByText("ci #42 in darkmem on fix/cache-key.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Actions" })).toHaveAttribute("href", "/actions")
    expect(screen.getByRole("link", { name: "darkmem" })).toHaveAttribute("href", "/actions?repo=darkmem")
    expect(screen.getByText("ci")).toBeInTheDocument()
    expect(screen.getByText("#42")).not.toHaveAttribute("href")
    expect(screen.getByText("Run content")).toBeInTheDocument()
  })

  it("adds the job to the breadcrumbs and links the run number on a job page", () => {
    renderFrame({ job: fixtures.runDetail.jobs[1], current: 1002 })
    expect(screen.getByRole("link", { name: "#42" })).toHaveAttribute("href", "/actions/darkmem/runs/102?attempt=2")
    expect(screen.getAllByText("Backend tests").length).toBeGreaterThan(1)
  })

  it("opens the run on GitHub in a new tab", () => {
    renderFrame()
    const link = screen.getByRole("link", { name: "Open on GitHub" })
    expect(link).toHaveAttribute("href", "https://github.com/darkraise/darkmem/actions/runs/102")
    expect(link).toHaveAttribute("target", "_blank")
  })

  it("picks an attempt, latest first", async () => {
    const { onAttempt, user } = renderFrame()
    const picker = screen.getByRole("combobox", { name: "Attempt" })
    expect(picker).toHaveTextContent("Attempt 2 (latest)")
    await user.click(picker)
    expect(screen.getAllByRole("option").map((o) => o.textContent)).toEqual(["Attempt 2 (latest)", "Attempt 1"])
    await user.click(screen.getByRole("option", { name: "Attempt 1" }))
    expect(onAttempt).toHaveBeenCalledWith(1)
  })

  it("hides the attempt picker for a run with one attempt", () => {
    renderFrame({}, { ...fixtures.runDetail, attempt: 1, attempts: 1 })
    expect(screen.queryByRole("combobox", { name: "Attempt" })).toBeNull()
  })

  it("lists the summary, every job with its status and duration, and the workflow file in the rail", () => {
    renderFrame({ current: 1002 })
    const rail = within(screen.getByRole("navigation", { name: "Run" }))
    expect(rail.getByRole("link", { name: "Summary" })).not.toHaveAttribute("aria-current")
    const links = rail.getAllByRole("link").filter((a) => a.getAttribute("href")?.startsWith("#job-"))
    expect(links.map((a) => a.getAttribute("href"))).toEqual(["#job-1001", "#job-1002", "#job-1003", "#job-1004", "#job-1005"])
    const backend = within(rail.getByRole("link", { name: /Backend tests/ }))
    expect(backend.getByRole("img", { name: "Failed" })).toBeInTheDocument()
    expect(backend.getByText("2m15s")).toHaveClass("font-mono")
    expect(rail.getByRole("link", { name: /Backend tests/ })).toHaveAttribute("aria-current", "page")
    expect(rail.getByText("Jobs")).toBeInTheDocument()
    expect(rail.getByText("Run details")).toBeInTheDocument()
    expect(rail.getByRole("link", { name: /Workflow file/ })).toHaveAttribute(
      "href",
      "https://github.com/darkraise/darkmem/blob/a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9/.github/workflows/ci.yml",
    )
  })

  it("marks the summary as current on the run page", () => {
    renderFrame()
    const rail = within(screen.getByRole("navigation", { name: "Run" }))
    expect(rail.getByRole("link", { name: "Summary" })).toHaveAttribute("aria-current", "page")
  })

  it("leaves out the workflow file link for a run without a workflow file", () => {
    renderFrame({}, { ...fixtures.runDetail, path: "dynamic/dependabot/dependabot-updates" })
    expect(screen.queryByRole("link", { name: /Workflow file/ })).toBeNull()
  })

  it("stacks the rail above the content below 800 px", () => {
    renderFrame()
    const rail = screen.getByRole("navigation", { name: "Run" })
    expect(rail).toHaveClass("min-[800px]:sticky")
    expect(rail.parentElement).toHaveClass("min-[800px]:grid-cols-[15rem_minmax(0,1fr)]")
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/components/run-frame.test.tsx`
Expected: FAIL: `Failed to resolve import "./run-frame"`.

- [ ] **Step 3: Write the frame**

Create `web/src/components/run-frame.tsx`:

```tsx
import { Button } from "darkraise-ui/components/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { PageHeader } from "darkraise-ui/layout"
import type { ReactNode } from "react"
import type { RunDetail, RunJob } from "@/api/types"
import { RunStatusIcon } from "@/components/run-status-icon"
import { dur } from "@/lib/format"
import { attemptLabel, githubLinks, jobMs, runDescription } from "@/lib/run-details"

export interface JobLinkOptions {
  step?: number
  line?: number
  className?: string
  label?: string
  current?: boolean
}

// The pages pass typed router links; tests pass plain anchors, so the frame
// and the sections render without a router.
export type JobLinkFn = (jobId: number, children: ReactNode, opts?: JobLinkOptions) => ReactNode
export type SummaryLinkFn = (children: ReactNode, opts: { className?: string; current?: boolean }) => ReactNode

// The router marks the current link with aria-current, so the style follows it.
const RAIL_LINK =
  "flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-muted aria-[current=page]:bg-muted aria-[current=page]:font-medium"
const RAIL_LABEL = "mt-3 px-2 pb-1 text-xs font-medium text-muted-foreground"

export function RunFrame({
  run,
  owner,
  now,
  current,
  job,
  jobLink,
  summaryLink,
  onAttempt,
  children,
}: {
  run: RunDetail
  owner: string
  now: number
  current: "summary" | number
  job?: RunJob
  jobLink: JobLinkFn
  summaryLink: SummaryLinkFn
  onAttempt: (attempt: number) => void
  children: ReactNode
}) {
  const runPath = `/actions/${encodeURIComponent(run.repo)}/runs/${run.id}`
  const breadcrumbs = [
    { label: "Actions", href: "/actions" },
    { label: run.repo, href: `/actions?repo=${encodeURIComponent(run.repo)}` },
    { label: run.workflow },
    job ? { label: `#${run.run_number}`, href: `${runPath}?attempt=${run.attempt}` } : { label: `#${run.run_number}` },
    ...(job ? [{ label: job.name }] : []),
  ]
  const attempts = Array.from({ length: run.attempts }, (_, i) => run.attempts - i)
  const workflowFile = githubLinks(owner, run).workflowFile
  const actions = (
    <div className="flex flex-wrap items-center gap-2">
      {run.attempts > 1 && (
        <Select value={String(run.attempt)} onValueChange={(v) => onAttempt(Number(v))}>
          <SelectTrigger aria-label="Attempt" className="w-48">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {attempts.map((n) => (
              <SelectItem key={n} value={String(n)}>
                {attemptLabel(n, run.attempts)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      <Button variant="outline" asChild>
        <a href={run.html_url} target="_blank" rel="noreferrer">
          Open on GitHub
        </a>
      </Button>
    </div>
  )

  return (
    <div className="flex flex-col gap-4">
      <PageHeader breadcrumbs={breadcrumbs} title={run.title} description={runDescription(run)} actions={actions} />
      <div className="grid gap-4 min-[800px]:grid-cols-[15rem_minmax(0,1fr)] min-[800px]:items-start">
        <nav aria-label="Run" className="flex flex-col min-[800px]:sticky min-[800px]:top-4">
          {summaryLink("Summary", { className: RAIL_LINK, current: current === "summary" })}
          <p className={RAIL_LABEL}>Jobs</p>
          <ul className="flex flex-col">
            {run.jobs.map((j) => {
              const ms = jobMs(j, now)
              return (
                <li key={j.id}>
                  {jobLink(
                    j.id,
                    <>
                      <RunStatusIcon status={j.status} conclusion={j.conclusion} />
                      <span className="min-w-0 flex-1 break-words">{j.name}</span>
                      {ms !== undefined && <span className="font-mono text-xs text-muted-foreground">{dur(ms)}</span>}
                    </>,
                    { className: RAIL_LINK, current: current === j.id },
                  )}
                </li>
              )
            })}
          </ul>
          <p className={RAIL_LABEL}>Run details</p>
          {workflowFile && (
            <a href={workflowFile} target="_blank" rel="noreferrer" className={RAIL_LINK}>
              Workflow file
              <span className="sr-only">, opens in a new tab</span>
            </a>
          )}
        </nav>
        <div className="flex min-w-0 flex-col gap-4">{children}</div>
      </div>
    </div>
  )
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/components/run-frame.test.tsx`
Expected: PASS (9 tests).

- [ ] **Step 5: Typecheck, lint and the anti-slop test**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint && timeout 300 npx vitest run src/anti-slop.test.ts`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/run-frame.tsx web/src/components/run-frame.test.tsx
git commit -m "feat(web): add the shared run frame"
```

---

### Task 17: Run and job routes

**Files:**
- Modify: `web/src/router.tsx` (imports at lines 18 and 24, a new block after `actionsRoute` ends at line 123, the route tree at line 137)
- Create: `web/src/pages/run.tsx`
- Test: `web/src/pages/run.test.tsx`

**Interfaces:**
- Consumes: `fixtures.runDetail`, `runRoutes` (Contracts, TypeScript — types and test fixtures); `useConfig`, `useRunDetail` (Contracts, API client and hooks); `positiveInt`, `parseRunSearch`, `parseJobSearch`, `RunSearch`, `JobSearch`, `jobMs`, `runTotalMs`, `statusText`, `githubLinks` (Contracts, run helpers); `RunStatusIcon`; `RunFrame`, `JobLinkFn`, `SummaryLinkFn` (Contracts, run frame).
- Produces: the routes `/app/actions/$repo/runs/$id` and `/app/actions/$repo/runs/$id/jobs/$jobId`; `RunPage`, `JobPage`, `RunNotFound` in `web/src/pages/run.tsx` (Contracts additions, pages). Tasks 18 to 22, 26 and 27 build on them.

**Items:** 1, 6

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

Notes for this task:
- The router replaces the address with its validated search, so a refused `attempt`, `step` or `line` would silently vanish. `strictSearch` makes `validateSearch` throw for a refused value of the route's own keys; TanStack wraps the throw in a `SearchParamError` (`web/node_modules/@tanstack/router-core/dist/esm/router.js:694`) and renders the route's `errorComponent`, `RunNotFound`, which shows the not-found line. Any other error falls through to the app's `RouteError`.
- Rail links pass `activeOptions={{ includeSearch: false }}`; the Summary link also passes `exact: true`, because TanStack's prefix match (`web/node_modules/@tanstack/react-router/dist/esm/link.js:34-43`) would mark it current on every job page. TanStack sets `aria-current="page"` itself on the active link (`link.js:237-240`).
- Every link carries `attempt: run.attempt`, so a job page and an annotation link always name the attempt they belong to.

- [ ] **Step 1: Write the failing tests**

Create `web/src/pages/run.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, type Call } from "@/test/api"
import { fixtures, runRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const runCalls = (calls: Call[]) => calls.filter((c) => c.path.startsWith("/api/actions/"))

describe("Run page", () => {
  it("shows the run's header and summary facts", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    expect(await screen.findByRole("heading", { name: "Fix the cache key", level: 1 })).toBeInTheDocument()
    expect(screen.getByText("ci #42 in darkmem on fix/cache-key.")).toBeInTheDocument()
    const summary = within(screen.getByRole("region", { name: "Summary" }))
    expect(summary.getByText("Triggered via pull_request")).toBeInTheDocument()
    expect(summary.getByText("octo-dev")).toBeInTheDocument()
    expect(summary.getByRole("link", { name: "a1f9c3e" })).toHaveAttribute(
      "href",
      "https://github.com/darkraise/darkmem/commit/a1f9c3e0d4b5c6a7b8c9d0e1f2a3b4c5d6e7f8a9",
    )
    expect(summary.getByRole("link", { name: "#87" })).toHaveAttribute("href", "https://github.com/darkraise/darkmem/pull/87")
    expect(summary.getByText("Failed")).toBeInTheDocument()
    expect(summary.getByText("4m10s")).toHaveClass("font-mono")
    expect(summary.getByText("2")).toHaveClass("font-mono")
  })

  it("links the rail to the summary and to each job of the shown attempt", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const rail = within(await screen.findByRole("navigation", { name: "Run" }))
    expect(rail.getByRole("link", { name: "Summary" })).toHaveAttribute("aria-current", "page")
    expect(rail.getByRole("link", { name: /Backend tests/ })).toHaveAttribute("href", "/actions/darkmem/runs/102/jobs/1002?attempt=2")
    expect(rail.getByRole("link", { name: /Backend tests/ })).not.toHaveAttribute("aria-current")
  })

  it("asks for the picked attempt through the URL", async () => {
    const { calls } = mockApi(runRoutes())
    const { user, router } = renderApp("/actions/darkmem/runs/102")
    await screen.findByRole("heading", { name: "Fix the cache key", level: 1 })
    await user.click(screen.getByRole("combobox", { name: "Attempt" }))
    await user.click(await screen.findByRole("option", { name: "Attempt 1" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ attempt: 1 }))
    await waitFor(() => expect(runCalls(calls).some((c) => c.path === "/api/actions/darkmem/runs/102" && c.search === "?attempt=1")).toBe(true))
  })

  it.each([
    ["a run ID that is not a number", "/actions/darkmem/runs/abc"],
    ["an attempt of zero", "/actions/darkmem/runs/102?attempt=0"],
    ["a job ID that is not a number", "/actions/darkmem/runs/102/jobs/x"],
    ["a line that is not a number", "/actions/darkmem/runs/102/jobs/1002?line=top"],
  ])("shows the not-found line for %s without asking the API", async (_, path) => {
    const { calls } = mockApi(runRoutes())
    renderApp(path)
    expect(await screen.findByText("This run no longer exists on GitHub.")).toBeInTheDocument()
    expect(runCalls(calls)).toEqual([])
  })

  it("names a repository ghr does not cover without asking the API", async () => {
    const { calls } = mockApi(runRoutes())
    renderApp("/actions/other/runs/102")
    expect(await screen.findByText("other is not configured or watched in ghr.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Open Repositories" })).toHaveAttribute("href", "/repositories")
    expect(runCalls(calls)).toEqual([])
  })

  it("reads a watched repository's run", async () => {
    mockApi(runRoutes({ "GET /api/actions/docs/runs/300": { ...fixtures.runDetail, repo: "docs", id: 300, watched: true } }))
    renderApp("/actions/docs/runs/300")
    expect(await screen.findByRole("heading", { name: "Fix the cache key", level: 1 })).toBeInTheDocument()
  })

  it("says the run is gone when GitHub no longer has it", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/runs/102": () => json({ error: "not found on GitHub" }, 404) }))
    renderApp("/actions/darkmem/runs/102")
    expect(await screen.findByText("This run no longer exists on GitHub.")).toBeInTheDocument()
  })

  it("shows the daemon's wording for a missing permission", async () => {
    mockApi(
      runRoutes({
        "GET /api/actions/darkmem/runs/102": () =>
          json({ error: "token lacks actions=read on darkraise/darkmem: grant it in the PAT settings" }, 403),
      }),
    )
    renderApp("/actions/darkmem/runs/102")
    expect(await screen.findByText("token lacks actions=read on darkraise/darkmem: grant it in the PAT settings")).toBeInTheDocument()
  })
})

describe("Job page", () => {
  it("shows the job's heading, runner, labels and steps", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    const heading = await screen.findByRole("heading", { name: "Backend tests", level: 2 })
    const job = within(heading.closest("section") as HTMLElement)
    expect(job.getByText("2m15s")).toHaveClass("font-mono")
    expect(job.getByText("ghr-darkmem-d4e5f6")).toHaveClass("font-mono")
    expect(job.getByText("homelab")).toBeInTheDocument()
    expect(job.getByText("ghr")).toHaveClass("font-mono")
    expect(job.getByText("2026-10-03 13:55:25")).toHaveClass("font-mono")
    expect(job.getAllByText("Lint (ruff)").length).toBeGreaterThan(0)
    const rail = within(screen.getByRole("navigation", { name: "Run" }))
    expect(rail.getByRole("link", { name: /Backend tests/ })).toHaveAttribute("aria-current", "page")
    expect(rail.getByRole("link", { name: "Summary" })).not.toHaveAttribute("aria-current")
  })

  it("says logs wait for a running job and lists its steps", async () => {
    const running = {
      ...fixtures.runDetail,
      status: "in_progress",
      conclusion: "",
      jobs: fixtures.runDetail.jobs.map((j) => (j.id === 1002 ? { ...j, status: "in_progress", conclusion: "", completed_at: undefined } : j)),
    }
    const { calls } = mockApi(runRoutes({ "GET /api/actions/darkmem/runs/102": running }))
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("Logs appear when the job finishes.")).toBeInTheDocument()
    expect(screen.getByRole("list", { name: "Steps" })).toBeInTheDocument()
    expect(calls.some((c) => c.path.endsWith("/log"))).toBe(false)
  })

  it("says when the job is not part of the shown attempt", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/999")
    expect(await screen.findByText("This job is not part of attempt 2.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Open the run" })).toHaveAttribute("href", "/actions/darkmem/runs/102?attempt=2")
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/pages/run.test.tsx`
Expected: FAIL — the routes do not exist yet, so `findByRole("heading", { name: "Fix the cache key" })` and the not-found texts time out.

- [ ] **Step 3: Register the two routes**

In `web/src/router.tsx`, add after the line `import { loginSearch, safeRedirect } from "./lib/redirect"`:

```tsx
import { parseJobSearch, parseRunSearch, type JobSearch, type RunSearch } from "./lib/run-details"
```

Add after the line `import { RepositoryPage } from "./pages/repository"`:

```tsx
import { JobPage, RunNotFound, RunPage } from "./pages/run"
```

Insert after the `actionsRoute` definition (the block ending `  component: ActionsPage,\n})`):

```tsx
// A value the parser refused fails the route, so a bad attempt, step or line
// shows the not-found line instead of quietly falling back to the latest
// attempt; the router would otherwise drop it from the address.
function strictSearch<T extends object>(raw: Record<string, unknown>, parsed: T, names: string[]): T {
  const kept = new Map(Object.entries(parsed))
  for (const name of names) {
    if (raw[name] !== undefined && kept.get(name) === undefined) throw new Error(`${name} must be a positive integer`)
  }
  return parsed
}

const runRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/actions/$repo/runs/$id",
  validateSearch: (search: Record<string, unknown>): RunSearch => ({
    attempt: undefined,
    ...strictSearch(search, parseRunSearch(search), ["attempt"]),
  }),
  component: RunPage,
  errorComponent: RunNotFound,
})
const jobRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/actions/$repo/runs/$id/jobs/$jobId",
  validateSearch: (search: Record<string, unknown>): JobSearch => ({
    attempt: undefined,
    step: undefined,
    line: undefined,
    ...strictSearch(search, parseJobSearch(search), ["attempt", "step", "line"]),
  }),
  component: JobPage,
  errorComponent: RunNotFound,
})
```

In `routeTree`, replace the line `    actionsRoute,` with:

```tsx
    actionsRoute,
    runRoute,
    jobRoute,
```

- [ ] **Step 4: Create the pages**

Create `web/src/pages/run.tsx`:

```tsx
import { Link, SearchParamError, useNavigate, useParams, useSearch, type ErrorComponentProps } from "@tanstack/react-router"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useId, type ReactNode } from "react"
import { ApiError } from "@/api/client"
import { useConfig, useRunDetail } from "@/api/hooks"
import type { RunDetail, RunJob } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { RouteError } from "@/components/route-error"
import { RunFrame, type JobLinkFn, type SummaryLinkFn } from "@/components/run-frame"
import { RunStatusIcon } from "@/components/run-status-icon"
import { StepList } from "@/components/step-list"
import { dateTimeSec, dur } from "@/lib/format"
import { githubLinks, jobMs, positiveInt, runTotalMs, statusText } from "@/lib/run-details"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const GONE = "This run no longer exists on GitHub."

function runError(err: unknown): string {
  return err instanceof ApiError && err.status === 404 ? GONE : errorText(err)
}

function Bare({ repo, children }: { repo: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-4">
      <PageHeader breadcrumbs={[{ label: "Actions", href: "/actions" }, { label: repo }]} title="Workflow run" />
      {children}
    </div>
  )
}

// The routes' error view: their search parser refuses a bad attempt, step or
// line. Any other error is the app's usual one.
export function RunNotFound(props: ErrorComponentProps) {
  const params = useParams({ strict: false })
  if (!(props.error instanceof SearchParamError)) return <RouteError {...props} />
  return (
    <Bare repo={params.repo ?? ""}>
      <ErrorLine>{GONE}</ErrorLine>
    </Bare>
  )
}

function RunGate({
  repo,
  idParam,
  attempt,
  invalid = false,
  children,
}: {
  repo: string
  idParam: string
  attempt: number | undefined
  invalid?: boolean
  children: (run: RunDetail, owner: string) => ReactNode
}) {
  const config = useConfig()
  const id = positiveInt(idParam)
  const valid = id !== undefined && !invalid
  const names = [...(config.data?.repos ?? []).map((r) => r.name), ...(config.data?.watch_repos ?? [])]
  const covered = names.some((n) => n.toLowerCase() === repo.toLowerCase())
  const run = useRunDetail(repo, id ?? 0, attempt, valid && covered)

  if (!valid) {
    return (
      <Bare repo={repo}>
        <ErrorLine>{GONE}</ErrorLine>
      </Bare>
    )
  }
  if (!config.data) {
    return (
      <Bare repo={repo}>
        {config.isError ? <ErrorLine onRetry={() => void config.refetch()}>{errorText(config.error)}</ErrorLine> : <Spinner label="Loading" />}
      </Bare>
    )
  }
  if (!covered) {
    return (
      <Bare repo={repo}>
        <div className="flex flex-col items-start gap-2">
          <ErrorLine>{`${repo} is not configured or watched in ghr.`}</ErrorLine>
          <Link to="/repositories" className="text-sm text-primary hover:underline">
            Open Repositories
          </Link>
        </div>
      </Bare>
    )
  }
  if (!run.data) {
    return (
      <Bare repo={repo}>
        {run.isError ? (
          <ErrorLine onRetry={() => void run.refetch()}>{runError(run.error)}</ErrorLine>
        ) : (
          <Spinner label="Loading" />
        )}
      </Bare>
    )
  }
  return (
    <>
      {run.isError && <ErrorLine onRetry={() => void run.refetch()}>{runError(run.error)}</ErrorLine>}
      {children(run.data, config.data.owner)}
    </>
  )
}

function linksFor(run: RunDetail): { jobLink: JobLinkFn; summaryLink: SummaryLinkFn } {
  const params = { repo: run.repo, id: String(run.id) }
  return {
    jobLink: (jobId, children, opts = {}) => (
      <Link
        to="/actions/$repo/runs/$id/jobs/$jobId"
        params={{ ...params, jobId: String(jobId) }}
        search={{ attempt: run.attempt, step: opts.step, line: opts.line }}
        activeOptions={{ includeSearch: false }}
        className={opts.className}
        aria-label={opts.label}
      >
        {children}
      </Link>
    ),
    // exact: a job page's path starts with the run page's, and would
    // otherwise mark Summary as the current page too.
    summaryLink: (children, opts) => (
      <Link
        to="/actions/$repo/runs/$id"
        params={params}
        search={{ attempt: run.attempt }}
        activeOptions={{ exact: true, includeSearch: false }}
        className={opts.className}
      >
        {children}
      </Link>
    ),
  }
}

function useAttemptNavigate(run: RunDetail): (attempt: number) => void {
  const navigate = useNavigate()
  return (attempt) => void navigate({ to: "/actions/$repo/runs/$id", params: { repo: run.repo, id: String(run.id) }, search: { attempt } })
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-sm">{children}</dd>
    </div>
  )
}

function SummaryFacts({ run, owner, now }: { run: RunDetail; owner: string; now: number }) {
  const links = githubLinks(owner, run)
  return (
    <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
      <Fact label={run.event ? `Triggered via ${run.event}` : "Triggered"}>
        {run.actor && <span>{run.actor}</span>}
        <a href={links.commit} target="_blank" rel="noreferrer" className="font-mono text-primary hover:underline">
          {run.head_sha.slice(0, 7)}
        </a>
        {run.pull_requests.map((n) => (
          <a key={n} href={links.pull(n)} target="_blank" rel="noreferrer" className="font-mono text-primary hover:underline">
            #{n}
          </a>
        ))}
      </Fact>
      <Fact label="Status">
        <RunStatusIcon status={run.status} conclusion={run.conclusion} />
        <span>{statusText(run.status, run.conclusion)}</span>
      </Fact>
      <Fact label="Total duration">
        <span className="font-mono">{dur(runTotalMs(run, now))}</span>
      </Fact>
      <Fact label="Artifacts">
        <span className="font-mono">{run.artifacts.length}</span>
      </Fact>
    </dl>
  )
}

function RunSummary({ run, owner }: { run: RunDetail; owner: string }) {
  const now = useNow()
  const onAttempt = useAttemptNavigate(run)
  const { jobLink, summaryLink } = linksFor(run)
  return (
    <RunFrame run={run} owner={owner} now={now} current="summary" jobLink={jobLink} summaryLink={summaryLink} onAttempt={onAttempt}>
      <Section title="Summary">
        <SummaryFacts run={run} owner={owner} now={now} />
      </Section>
    </RunFrame>
  )
}

export function RunPage() {
  const params = useParams({ from: "/app/actions/$repo/runs/$id" })
  const search = useSearch({ from: "/app/actions/$repo/runs/$id" })
  return (
    <RunGate repo={params.repo} idParam={params.id} attempt={search.attempt}>
      {(run, owner) => <RunSummary run={run} owner={owner} />}
    </RunGate>
  )
}

function JobMeta({ job }: { job: RunJob }) {
  return (
    <div className="mt-1 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
      {job.runner_name && <span className="font-mono">{job.runner_name}</span>}
      {job.labels.length > 0 && (
        <span className="flex flex-wrap gap-x-2 font-mono">
          {job.labels.map((l) => (
            <span key={l}>{l}</span>
          ))}
        </span>
      )}
      {job.ghr && <span className="font-mono">ghr</span>}
      {job.started_at && <span className="font-mono">{dateTimeSec(job.started_at)}</span>}
    </div>
  )
}

function JobView({ run, owner, jobId }: { run: RunDetail; owner: string; jobId: number }) {
  const now = useNow()
  const onAttempt = useAttemptNavigate(run)
  const headingId = useId()
  const { jobLink, summaryLink } = linksFor(run)
  const job = run.jobs.find((j) => j.id === jobId)
  if (!job) {
    return (
      <RunFrame run={run} owner={owner} now={now} current={jobId} jobLink={jobLink} summaryLink={summaryLink} onAttempt={onAttempt}>
        <div className="flex flex-col items-start gap-2">
          <p className="text-sm text-muted-foreground">{`This job is not part of attempt ${run.attempt}.`}</p>
          {summaryLink("Open the run", { className: "text-sm text-primary hover:underline" })}
        </div>
      </RunFrame>
    )
  }
  const ms = jobMs(job, now)
  return (
    <RunFrame run={run} owner={owner} now={now} current={job.id} job={job} jobLink={jobLink} summaryLink={summaryLink} onAttempt={onAttempt}>
      <section aria-labelledby={headingId} className="min-w-0 rounded-[10px] border border-border bg-card p-4">
        <div className="flex flex-wrap items-center gap-2">
          <RunStatusIcon status={job.status} conclusion={job.conclusion} />
          <h2 id={headingId} className="text-base font-semibold">
            {job.name}
          </h2>
          {ms !== undefined && <span className="font-mono text-sm text-muted-foreground">{dur(ms)}</span>}
        </div>
        <JobMeta job={job} />
        <div className="mt-4">
          {job.status === "completed" ? (
            <StepList steps={job.steps} now={now} live={false} />
          ) : (
            <div className="flex flex-col gap-2">
              <p className="text-sm text-muted-foreground">Logs appear when the job finishes.</p>
              <StepList steps={job.steps} now={now} live />
            </div>
          )}
        </div>
      </section>
    </RunFrame>
  )
}

export function JobPage() {
  const params = useParams({ from: "/app/actions/$repo/runs/$id/jobs/$jobId" })
  const search = useSearch({ from: "/app/actions/$repo/runs/$id/jobs/$jobId" })
  const jobId = positiveInt(params.jobId)
  return (
    <RunGate repo={params.repo} idParam={params.id} attempt={search.attempt} invalid={jobId === undefined}>
      {(run, owner) => <JobView run={run} owner={owner} jobId={jobId ?? 0} />}
    </RunGate>
  )
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/pages/run.test.tsx src/router.test.tsx`
Expected: PASS (14 tests in `run.test.tsx`).

- [ ] **Step 6: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both exit 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/router.tsx web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "feat(web): add the run and job pages"
```

---

### Task 18: Actions page links to the run page

**Files:**
- Modify: `web/src/pages/actions.tsx` (imports at lines 9, 15, 18-19; `StatusIcon` at lines 36-40; its use at line 48; the title link at lines 51-54)
- Test: `web/src/pages/actions.test.tsx` (the import at line 4, the test at lines 20-31, a new test before line 64)

**Interfaces:**
- Consumes: `RunStatusIcon` (Contracts, run helpers and status icon); the run route from Task 17; `runRoutes` (Contracts, types and test fixtures).
- Produces: none.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Update the tests**

In `web/src/pages/actions.test.tsx`, replace the line `import { authedRoutes, fixtures } from "@/test/fixtures"` with:

```tsx
import { authedRoutes, fixtures, runRoutes } from "@/test/fixtures"
```

In the test "shows the runs in progress with their elapsed time", replace these three lines:

```tsx
    const link = progress.getByRole("link", { name: "Fix the cache key, darkmem #42, opens in a new tab" })
    expect(link).toHaveAttribute("href", "https://github.com/darkraise/darkmem/actions/runs/102")
    expect(link).toHaveAttribute("target", "_blank")
```

with:

```tsx
    const link = progress.getByRole("link", { name: "Fix the cache key, darkmem #42" })
    expect(link).toHaveAttribute("href", "/actions/darkmem/runs/102")
    expect(link).not.toHaveAttribute("target")
```

Insert before the line `  it("lists each repository that could not be read", async () => {`:

```tsx
  it("opens a run's page from its title", async () => {
    mockApi(runRoutes())
    const { user, router } = renderApp("/actions")
    await screen.findByRole("region", { name: "In progress" })
    await user.click(region("In progress").getByRole("link", { name: "Fix the cache key, darkmem #42" }))
    expect(await screen.findByRole("heading", { name: "Fix the cache key", level: 1 })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/actions/darkmem/runs/102")
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/pages/actions.test.tsx`
Expected: FAIL — "Unable to find an accessible element with the role "link" and name "Fix the cache key, darkmem #42"".

- [ ] **Step 3: Link the title and use the shared status icon**

In `web/src/pages/actions.tsx`:

1. Delete the line `import { Clock } from "lucide-react"`.
2. Replace `import { ResultIcon } from "@/components/result-icon"` with `import { RunStatusIcon } from "@/components/run-status-icon"`.
3. In the `@/lib/actions` import, delete the two lines `  isActive,` and `  isQueued,` (nothing else in the file uses them).
4. Delete the whole `StatusIcon` function (lines 36-40) and the blank line after it.
5. Replace `        <StatusIcon run={run} />` with `        <RunStatusIcon status={run.status} conclusion={run.conclusion} />`.
6. Replace the title anchor:

```tsx
        <a href={run.html_url} target="_blank" rel="noreferrer" className="font-medium hover:underline">
          {run.title}
          <span className="sr-only">{`, ${run.repo} #${run.run_number}, opens in a new tab`}</span>
        </a>
```

with:

```tsx
        <Link
          to="/actions/$repo/runs/$id"
          params={{ repo: run.repo, id: String(run.id) }}
          className="font-medium hover:underline"
        >
          {run.title}
          <span className="sr-only">{`, ${run.repo} #${run.run_number}`}</span>
        </Link>
```

`Link` is already imported from `@tanstack/react-router` (line 1), and `Spinner` stays imported for the loading state.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/pages/actions.test.tsx`
Expected: PASS.

- [ ] **Step 5: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both exit 0 (lint fails if an unused import is left behind).

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/actions.tsx web/src/pages/actions.test.tsx
git commit -m "feat(web): link Actions runs to their run page"
```

---

### Task 19: Artifacts section

**Files:**
- Create: `web/src/components/run-artifacts.tsx`
- Test: `web/src/components/run-artifacts.test.tsx`
- Modify: `web/src/pages/run.tsx` (an import and the mount inside `RunSummary`), `web/src/pages/run.test.tsx` (one test)

**Interfaces:**
- Consumes: `api.artifactDownload` (Contracts, API client and hooks); `RunArtifact`, `fixtures.runDetail` (Contracts, types and test fixtures); `RunSummary` in `web/src/pages/run.tsx` (Task 17).
- Produces: `ArtifactsSection` (Contracts additions, sections).

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/run-artifacts.test.tsx`:

```tsx
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { ArtifactsSection } from "./run-artifacts"

const row = (name: string) => within(screen.getByText(name).closest("tr") as HTMLElement)

describe("ArtifactsSection", () => {
  it("lists each artifact's size and expiry", () => {
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" artifacts={fixtures.runDetail.artifacts} />, { wrapper })
    expect(screen.getByRole("region", { name: "Artifacts" })).toBeInTheDocument()
    expect(screen.getByText("frontend-dist")).toHaveClass("font-mono")
    expect(row("frontend-dist").getByText("2.4 MB")).toHaveClass("font-mono", "text-right")
    expect(row("frontend-dist").getByText("2026-10-17")).toHaveClass("font-mono", "text-right")
    expect(row("frontend-dist").getByRole("button", { name: "Download frontend-dist" })).toBeInTheDocument()
  })

  it("shows an expired artifact without a download button", () => {
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" artifacts={fixtures.runDetail.artifacts} />, { wrapper })
    expect(row("coverage-backend").getByText("Expired")).toHaveClass("text-muted-foreground")
    expect(row("coverage-backend").queryByRole("button")).toBeNull()
  })

  it("asks the daemon for the address, then goes to it", async () => {
    const url = "https://productionresultssa1.blob.core.windows.net/actions-results/a.zip?sig=x"
    const { calls } = mockApi({ "GET /api/actions/darkmem/artifacts/11/download": { url } })
    const assign = vi.fn()
    vi.stubGlobal("location", { assign })
    const { wrapper } = withQuery()
    render(<ArtifactsSection repo="darkmem" artifacts={fixtures.runDetail.artifacts} />, { wrapper })
    await userEvent.setup().click(screen.getByRole("button", { name: "Download frontend-dist" }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith(url))
    expect(calls.map((c) => c.path)).toEqual(["/api/actions/darkmem/artifacts/11/download"])
  })

  it("renders nothing for a run without artifacts", () => {
    const { wrapper } = withQuery()
    const { container } = render(<ArtifactsSection repo="darkmem" artifacts={[]} />, { wrapper })
    expect(container).toBeEmptyDOMElement()
  })
})
```

In `web/src/pages/run.test.tsx`, insert before the line `  it("links the rail to the summary and to each job of the shown attempt", async () => {`:

```tsx
  it("lists the run's artifacts", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const artifacts = within(await screen.findByRole("region", { name: "Artifacts" }))
    expect(artifacts.getByRole("button", { name: "Download frontend-dist" })).toBeInTheDocument()
    expect(artifacts.getByText("Expired")).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/components/run-artifacts.test.tsx src/pages/run.test.tsx`
Expected: FAIL — `run-artifacts.test.tsx` cannot resolve `./run-artifacts`, and "lists the run's artifacts" finds no "Artifacts" region.

- [ ] **Step 3: Write the section**

Create `web/src/components/run-artifacts.tsx`:

```tsx
import { useMutation } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { api } from "@/api/client"
import type { RunArtifact } from "@/api/types"
import { Section } from "@/components/page/section"
import { dateTime, humanBytes } from "@/lib/format"

function DownloadButton({ repo, artifact }: { repo: string; artifact: RunArtifact }) {
  // GitHub's signed address expires after about a minute, so every click asks
  // the daemon for a new one rather than keeping it.
  const download = useMutation({
    mutationFn: () => api.artifactDownload(repo, artifact.id),
    onSuccess: (d) => window.location.assign(d.url),
  })
  return (
    <Button size="sm" variant="outline" loading={download.isPending} aria-label={`Download ${artifact.name}`} onClick={() => download.mutate()}>
      Download
    </Button>
  )
}

export function ArtifactsSection({ repo, artifacts }: { repo: string; artifacts: RunArtifact[] }) {
  if (artifacts.length === 0) return null
  return (
    <Section title="Artifacts">
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead className="text-right">Expires</TableHead>
              <TableHead>
                <span className="sr-only">Download</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {artifacts.map((a) => (
              <TableRow key={a.id}>
                <TableCell className="font-mono">{a.name}</TableCell>
                <TableCell className="text-right font-mono">{humanBytes(a.size_bytes)}</TableCell>
                <TableCell className="text-right font-mono">{a.expires_at ? dateTime(a.expires_at).slice(0, 10) : ""}</TableCell>
                <TableCell className="text-right">
                  {a.expired ? <span className="text-sm text-muted-foreground">Expired</span> : <DownloadButton repo={repo} artifact={a} />}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </Section>
  )
}
```

- [ ] **Step 4: Mount it on the run page**

In `web/src/pages/run.tsx`, make these replacements in order; each `Replace` text occurs exactly once in the file.

1. Replace:

```tsx
import { RouteError } from "@/components/route-error"
```

   with:

```tsx
import { RouteError } from "@/components/route-error"
import { ArtifactsSection } from "@/components/run-artifacts"
```

2. Replace:

```tsx
        <SummaryFacts run={run} owner={owner} now={now} />
      </Section>
    </RunFrame>
```

   with:

```tsx
        <SummaryFacts run={run} owner={owner} now={now} />
      </Section>
      <ArtifactsSection repo={run.repo} artifacts={run.artifacts} />
    </RunFrame>
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/components/run-artifacts.test.tsx src/pages/run.test.tsx`
Expected: PASS.

- [ ] **Step 6: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both exit 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/run-artifacts.tsx web/src/components/run-artifacts.test.tsx web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "feat(web): list a run's artifacts with downloads"
```

---

### Task 20: Annotations section

**Files:**
- Create: `web/src/components/run-annotations.tsx`
- Test: `web/src/components/run-annotations.test.tsx`
- Modify: `web/src/pages/run.tsx` (imports, the annotations query and the mount inside `RunSummary`), `web/src/pages/run.test.tsx` (one test)

**Interfaces:**
- Consumes: `useRunAnnotations` (Contracts, API client and hooks); `RunAnnotations`, `Annotation`, `fixtures.runAnnotations` (Contracts, types and test fixtures); `annotationCounts`, `isRunActive` (Contracts, run helpers); `JobLinkFn` (Contracts, run frame); `RunSummary` (Task 17, as Task 19 left it).
- Produces: `AnnotationsSection` (Contracts additions, sections).

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Notes for this task: the annotations query passes `run.attempt` and `active = isRunActive(run.status)`, so it polls every `POLL_SLOW` while logs are unread, every `POLL_ACTIONS` while the run is active, and stops otherwise; Task 14's hook tests cover those intervals. Rows arrive sorted by level first, so one job's rows need not be adjacent: its "{n} more in the log" line goes under the last of them.

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/run-annotations.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RunAnnotations } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import type { JobLinkFn } from "./run-frame"
import { AnnotationsSection } from "./run-annotations"

const jobLink: JobLinkFn = (jobId, children, opts = {}) => (
  <a href={`#job-${jobId}-${opts.step}-${opts.line}`} aria-label={opts.label} className={opts.className}>
    {children}
  </a>
)

function show(data: RunAnnotations | undefined, error: unknown = null) {
  render(<AnnotationsSection data={data} error={error} onRetry={() => {}} jobLink={jobLink} />)
  return within(screen.getByRole("region", { name: "Annotations" }))
}

const none: RunAnnotations = { items: [], omitted: {}, unread: 0, expired_logs: 0, too_large: 0, failed: 0 }

describe("AnnotationsSection", () => {
  it("counts each level and lists the annotations", () => {
    const s = show(fixtures.runAnnotations)
    expect(s.getByText("2 errors")).toHaveClass("font-mono")
    expect(s.getByText("1 warning")).toBeInTheDocument()
    expect(s.getByText("1 notice")).toBeInTheDocument()
    expect(s.getByText("src/cache.py:12:5: F401 'os' imported but unused")).toHaveClass("font-mono")
    expect(s.getAllByRole("img", { name: "Error" })).toHaveLength(2)
    expect(s.getByRole("img", { name: "Warning" })).toBeInTheDocument()
    expect(s.getByRole("img", { name: "Notice" })).toBeInTheDocument()
    expect(s.getAllByText("Backend tests / Lint (ruff)")).toHaveLength(2)
    expect(s.getByText("Taken from the job logs, so file and line come only from the message text.")).toBeInTheDocument()
  })

  it("links each annotation to its log line", () => {
    const s = show(fixtures.runAnnotations)
    const link = s.getByRole("link", { name: "Log line 5, Backend tests / Lint (ruff)" })
    expect(link).toHaveTextContent("Log line 5")
    expect(link).toHaveAttribute("href", "#job-1002-3-5")
  })

  it("says how many more a job's log holds under its last row", () => {
    const s = show(fixtures.runAnnotations)
    const more = s.getByText("3 more in the log")
    const row = more.closest("li") as HTMLElement
    expect(within(row).getByText("Process completed with exit code 1.")).toBeInTheDocument()
  })

  it("hides the zero counts", () => {
    const s = show({ ...fixtures.runAnnotations, items: fixtures.runAnnotations.items.filter((a) => a.level === "error") })
    expect(s.getByText("2 errors")).toBeInTheDocument()
    expect(s.queryByText(/warning/)).toBeNull()
    expect(s.queryByText(/notice/)).toBeNull()
  })

  it.each([
    [{ unread: 2 }, "Reading logs for 2 jobs"],
    [{ expired_logs: 3 }, "Logs for 3 jobs have expired"],
    [{ expired_logs: 1 }, "The log for 1 job has expired"],
    [{ too_large: 2 }, "2 job logs are too large to read here"],
    [{ failed: 1 }, "1 job log could not be read"],
  ])("shows the status line for %o", (counts, line) => {
    const s = show({ ...none, ...counts })
    expect(s.getByText(line)).toBeInTheDocument()
    expect(s.queryByText("No errors, warnings or notices in the finished jobs' logs.")).toBeNull()
  })

  it("says the finished jobs' logs are clean", () => {
    const s = show(none)
    expect(s.getByText("No errors, warnings or notices in the finished jobs' logs.")).toBeInTheDocument()
  })

  it("shows the error while nothing has loaded", () => {
    const s = show(undefined, new Error("GitHub did not answer in time"))
    expect(s.getByText("GitHub did not answer in time")).toBeInTheDocument()
  })
})
```

In `web/src/pages/run.test.tsx`, insert before the line `  it("links the rail to the summary and to each job of the shown attempt", async () => {`:

```tsx
  it("links each annotation to its line in the job's log", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const annotations = within(await screen.findByRole("region", { name: "Annotations" }))
    expect(await annotations.findByRole("link", { name: "Log line 5, Backend tests / Lint (ruff)" })).toHaveAttribute(
      "href",
      "/actions/darkmem/runs/102/jobs/1002?attempt=2&step=3&line=5",
    )
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/components/run-annotations.test.tsx src/pages/run.test.tsx`
Expected: FAIL — `./run-annotations` cannot be resolved, and the page has no "Annotations" region.

- [ ] **Step 3: Write the section**

Create `web/src/components/run-annotations.tsx`:

```tsx
import { Spinner } from "darkraise-ui/components/spinner"
import { CircleX, Info, TriangleAlert, type LucideIcon } from "lucide-react"
import type { Annotation, RunAnnotations } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import type { JobLinkFn } from "@/components/run-frame"
import { plural } from "@/lib/format"
import { annotationCounts } from "@/lib/run-details"
import { errorText } from "@/query"

const LEVELS: Record<string, { label: string; Icon: LucideIcon; className: string } | undefined> = {
  error: { label: "Error", Icon: CircleX, className: "text-destructive" },
  warning: { label: "Warning", Icon: TriangleAlert, className: "text-warning" },
  notice: { label: "Notice", Icon: Info, className: "text-muted-foreground" },
}

function LevelIcon({ level }: { level: string }) {
  const { label, Icon, className } = LEVELS[level] ?? { label: "Notice", Icon: Info, className: "text-muted-foreground" }
  return <Icon role="img" aria-label={label} size={15} className={`mt-0.5 shrink-0 ${className}`} />
}

// Each line shows only when its count is above zero.
function statusLines(data: RunAnnotations): string[] {
  const out: string[] = []
  if (data.unread > 0) out.push(`Reading logs for ${plural(data.unread, "job")}`)
  if (data.expired_logs > 0) {
    out.push(data.expired_logs === 1 ? "The log for 1 job has expired" : `Logs for ${data.expired_logs} jobs have expired`)
  }
  if (data.too_large > 0) {
    out.push(data.too_large === 1 ? "1 job log is too large to read here" : `${data.too_large} job logs are too large to read here`)
  }
  if (data.failed > 0) out.push(data.failed === 1 ? "1 job log could not be read" : `${data.failed} job logs could not be read`)
  return out
}

// Rows are sorted by level first, so a job's rows need not be adjacent; its
// "more" line goes under the last of them.
function isLastOfJob(items: Annotation[], i: number): boolean {
  const id = items[i]?.job_id
  return !items.slice(i + 1).some((a) => a.job_id === id)
}

function Counts({ items }: { items: Annotation[] }) {
  const c = annotationCounts(items)
  const parts = [
    c.error > 0 && plural(c.error, "error"),
    c.warning > 0 && plural(c.warning, "warning"),
    c.notice > 0 && plural(c.notice, "notice"),
  ].filter((p): p is string => typeof p === "string")
  if (parts.length === 0) return null
  return (
    <span className="flex flex-wrap gap-x-3 text-sm text-muted-foreground">
      {parts.map((p) => (
        <span key={p} className="font-mono">
          {p}
        </span>
      ))}
    </span>
  )
}

export function AnnotationsSection({
  data,
  error,
  onRetry,
  jobLink,
}: {
  data: RunAnnotations | undefined
  error: unknown
  onRetry: () => void
  jobLink: JobLinkFn
}) {
  if (!data) {
    return (
      <Section title="Annotations">
        {error ? <ErrorLine onRetry={onRetry}>{errorText(error)}</ErrorLine> : <Spinner label="Loading" />}
      </Section>
    )
  }
  const lines = statusLines(data)
  return (
    <Section title="Annotations" aside={<Counts items={data.items} />}>
      {error ? <ErrorLine onRetry={onRetry}>{errorText(error)}</ErrorLine> : null}
      {data.items.length === 0 && lines.length === 0 ? (
        <p className="text-sm text-muted-foreground">No errors, warnings or notices in the finished jobs&apos; logs.</p>
      ) : (
        <>
          {data.items.length > 0 && (
            <ul className="flex flex-col">
              {data.items.map((a, i) => {
                const more = isLastOfJob(data.items, i) ? (data.omitted[String(a.job_id)] ?? 0) : 0
                return (
                  <li key={`${a.job_id}-${a.step}-${a.line}-${a.level}`} className="border-b border-border py-2 last:border-b-0">
                    <div className="flex gap-2">
                      <LevelIcon level={a.level} />
                      <div className="flex min-w-0 flex-col gap-1">
                        <p className="font-mono text-sm break-words whitespace-pre-wrap">{a.message}</p>
                        <p className="flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                          <span>{`${a.job_name} / ${a.step_name}`}</span>
                          {jobLink(a.job_id, `Log line ${a.line}`, {
                            step: a.step,
                            line: a.line,
                            className: "text-primary hover:underline",
                            label: `Log line ${a.line}, ${a.job_name} / ${a.step_name}`,
                          })}
                        </p>
                        {more > 0 && <p className="text-xs text-muted-foreground">{`${more} more in the log`}</p>}
                      </div>
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
          {lines.map((l) => (
            <p key={l} className="mt-2 text-sm text-muted-foreground">
              {l}
            </p>
          ))}
          <p className="mt-3 text-xs text-muted-foreground">Taken from the job logs, so file and line come only from the message text.</p>
        </>
      )}
    </Section>
  )
}
```

- [ ] **Step 4: Query and mount it on the run page**

In `web/src/pages/run.tsx`, make these replacements in order; each `Replace` text occurs exactly once in the file.

1. Replace:

```tsx
import { useConfig, useRunDetail } from "@/api/hooks"
```

   with:

```tsx
import { useConfig, useRunAnnotations, useRunDetail } from "@/api/hooks"
```

2. Replace:

```tsx
import { RouteError } from "@/components/route-error"
```

   with:

```tsx
import { RouteError } from "@/components/route-error"
import { AnnotationsSection } from "@/components/run-annotations"
```

3. Replace:

```tsx
import { githubLinks, jobMs, positiveInt, runTotalMs, statusText } from "@/lib/run-details"
```

   with:

```tsx
import { githubLinks, isRunActive, jobMs, positiveInt, runTotalMs, statusText } from "@/lib/run-details"
```

4. Replace:

```tsx
  const onAttempt = useAttemptNavigate(run)
  const { jobLink, summaryLink } = linksFor(run)
  return (
    <RunFrame run={run} owner={owner} now={now} current="summary"
```

   with:

```tsx
  const onAttempt = useAttemptNavigate(run)
  const annotations = useRunAnnotations(run.repo, run.id, run.attempt, isRunActive(run.status), true)
  const { jobLink, summaryLink } = linksFor(run)
  return (
    <RunFrame run={run} owner={owner} now={now} current="summary"
```

5. Replace:

```tsx
      <ArtifactsSection repo={run.repo} artifacts={run.artifacts} />
```

   with:

```tsx
      <AnnotationsSection data={annotations.data} error={annotations.error} onRetry={() => void annotations.refetch()} jobLink={jobLink} />
      <ArtifactsSection repo={run.repo} artifacts={run.artifacts} />
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/components/run-annotations.test.tsx src/pages/run.test.tsx`
Expected: PASS.

- [ ] **Step 6: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both exit 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/run-annotations.tsx web/src/components/run-annotations.test.tsx web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "feat(web): show a run's annotations"
```

---

### Task 21: Job graph

**Files:**
- Create: `web/src/components/run-graph.tsx`
- Test: `web/src/components/run-graph.test.tsx`
- Modify: `web/src/pages/run.tsx` (an import and the mount inside `RunSummary`), `web/src/pages/run.test.tsx` (one test)

**Interfaces:**
- Consumes: `RunGraph`, `GraphNode`, `RunJob`, `fixtures.runDetail` (Contracts, types and test fixtures); `jobMs`, `statusText` (Contracts, run helpers); `RunStatusIcon`; `JobLinkFn` (Contracts, run frame); `RunSummary` (as Task 20 left it).
- Produces: `JobGraph` (Contracts additions, sections).

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Notes for this task: spec §3.3 fixes the drawing. Tiles are 220 px wide with a 70 px column gap, absolutely positioned per column in the order the daemon sends (column, then file order); a single job's tile is one link, a group tile lists its jobs as links (first five, then a "{n} more" toggle with `aria-expanded`). Lines are one `aria-hidden` SVG of cubic curves from a node's right edge to each dependent's left edge, at the middle of the tile's first row so they stay put when a group opens. The scroll region is `role="group"` with `aria-label="Job graph"` and `tabIndex={0}` (axe flags `aria-label` on an element with no role). Each link's accessible name is the job name, its status word, and "needs {names}" when the node has needs.

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/run-graph.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import type { RunGraph, RunJob } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import type { JobLinkFn } from "./run-frame"
import { JobGraph } from "./run-graph"

const NOW = Date.parse("2026-10-03T14:05:00Z")
const run = fixtures.runDetail
const graph = run.graph as RunGraph

const jobLink: JobLinkFn = (jobId, children, opts = {}) => (
  <a href={`#job-${jobId}`} aria-label={opts.label} className={opts.className}>
    {children}
  </a>
)

function show(g: RunGraph = graph, jobs: RunJob[] = run.jobs) {
  const view = render(<JobGraph graph={g} jobs={jobs} path={run.path} event={run.event} now={NOW} jobLink={jobLink} />)
  return { ...view, section: within(screen.getByRole("region", { name: "ci.yml" })) }
}

describe("JobGraph", () => {
  it("is titled with the workflow file and the event", () => {
    const { section } = show()
    expect(section.getByText("pull_request")).toHaveClass("font-mono")
  })

  it("draws a focusable, scrollable region with one tile link per job", () => {
    const { section } = show()
    const region = section.getByRole("group", { name: "Job graph" })
    expect(region).toHaveAttribute("tabindex", "0")
    expect(region).toHaveClass("overflow-x-auto")
    expect(section.getByRole("link", { name: "Pick runner, Succeeded" })).toHaveAttribute("href", "#job-1001")
    expect(section.getByRole("link", { name: "Backend tests, Failed, needs Pick runner" })).toHaveAttribute("href", "#job-1002")
    expect(section.getByRole("link", { name: "Backend tests, Failed, needs Pick runner" })).toHaveTextContent("2m15s")
  })

  it("places tiles by column and keeps the lines out of the accessibility tree", () => {
    const { container } = show()
    const tile = (id: number) => (container.querySelector(`a[href="#job-${id}"]`)?.parentElement as HTMLElement).style
    expect(tile(1001).left).toBe("0px")
    expect(tile(1001).width).toBe("220px")
    expect(tile(1002).left).toBe("290px")
    const svg = container.querySelector("svg") as SVGElement
    expect(svg).toHaveAttribute("aria-hidden", "true")
    expect(svg.querySelectorAll("path")).toHaveLength(2)
  })

  it("lists a group's jobs by their full names", () => {
    const { section } = show()
    expect(section.getByText("Frontend tests (${{ matrix.shard }}/2)")).toBeInTheDocument()
    expect(section.getByRole("link", { name: "Frontend tests (1/2), Succeeded, needs Pick runner" })).toHaveTextContent("2m35s")
    expect(section.getByRole("link", { name: "Frontend tests (2/2), Succeeded, needs Pick runner" })).toBeInTheDocument()
    expect(section.getByRole("link", { name: "E2E (api), Succeeded" })).toBeInTheDocument()
  })

  it("shows a large group's first five jobs and the rest on request", async () => {
    const base = run.jobs[2] as RunJob
    const shards = Array.from({ length: 7 }, (_, i) => ({ ...base, id: 2000 + i, name: `Shard ${i + 1}` }))
    const g: RunGraph = { nodes: [{ key: "shard", name: "Shard", column: 0, needs: [], job_ids: shards.map((s) => s.id), group: true }] }
    const { section } = show(g, shards)
    expect(section.getAllByRole("link")).toHaveLength(5)
    const more = section.getByRole("button", { name: "2 more" })
    expect(more).toHaveAttribute("aria-expanded", "false")
    await userEvent.setup().click(more)
    expect(section.getAllByRole("link")).toHaveLength(7)
    expect(section.getByRole("button", { name: "Show fewer" })).toHaveAttribute("aria-expanded", "true")
  })
})
```

In `web/src/pages/run.test.tsx`, insert before the line `  it("links each annotation to its line in the job's log", async () => {`:

```tsx
  it("draws the job graph with tiles linking to the job pages", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102")
    const graph = within(await screen.findByRole("region", { name: "ci.yml" }))
    expect(graph.getByRole("link", { name: "Backend tests, Failed, needs Pick runner" })).toHaveAttribute(
      "href",
      "/actions/darkmem/runs/102/jobs/1002?attempt=2",
    )
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/components/run-graph.test.tsx src/pages/run.test.tsx`
Expected: FAIL — `./run-graph` cannot be resolved, and the page has no "ci.yml" region.

- [ ] **Step 3: Write the graph**

Create `web/src/components/run-graph.tsx`:

```tsx
import { useState } from "react"
import type { GraphNode, RunGraph, RunJob } from "@/api/types"
import { Section } from "@/components/page/section"
import type { JobLinkFn } from "@/components/run-frame"
import { RunStatusIcon } from "@/components/run-status-icon"
import { dur } from "@/lib/format"
import { jobMs, statusText } from "@/lib/run-details"

const TILE_W = 220
const GAP_X = 70
const GAP_Y = 16
const HEAD_H = 40
const ROW_H = 28
const GROUP_SHOWN = 5

interface Placed {
  node: GraphNode
  x: number
  y: number
  h: number
}

function tileHeight(node: GraphNode, open: boolean): number {
  if (!node.group) return HEAD_H
  const rows = open ? node.job_ids.length : Math.min(node.job_ids.length, GROUP_SHOWN)
  const toggle = node.job_ids.length > GROUP_SHOWN ? ROW_H : 0
  return HEAD_H + rows * ROW_H + toggle + 8
}

// Nodes arrive sorted by column, then by their order in the workflow file.
function layout(nodes: GraphNode[], open: Set<string>): { placed: Placed[]; width: number; height: number } {
  const colY: number[] = []
  const placed = nodes.map((node) => {
    const y = colY[node.column] ?? 0
    const h = tileHeight(node, open.has(node.key))
    colY[node.column] = y + h + GAP_Y
    return { node, x: node.column * (TILE_W + GAP_X), y, h }
  })
  const columns = Math.max(1, ...nodes.map((n) => n.column + 1))
  return { placed, width: columns * TILE_W + (columns - 1) * GAP_X, height: Math.max(HEAD_H, ...colY.map((y) => y - GAP_Y)) }
}

// Lines leave a node's right edge and enter each dependent's left edge, both
// at the middle of the tile's first row.
function edges(placed: Placed[]): string[] {
  const at = new Map(placed.map((p) => [p.node.key, p]))
  return placed.flatMap((to) =>
    to.node.needs.flatMap((key) => {
      const from = at.get(key)
      if (!from) return []
      const x1 = from.x + TILE_W
      const y1 = from.y + HEAD_H / 2
      const x2 = to.x
      const y2 = to.y + HEAD_H / 2
      const bend = GAP_X / 2
      return [`M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}`]
    }),
  )
}

function JobLine({ job, now }: { job: RunJob; now: number }) {
  const ms = jobMs(job, now)
  return (
    <>
      <RunStatusIcon status={job.status} conclusion={job.conclusion} />
      <span className="min-w-0 flex-1 truncate">{job.name}</span>
      {ms !== undefined && <span className="font-mono text-xs text-muted-foreground">{dur(ms)}</span>}
    </>
  )
}

export function JobGraph({
  graph,
  jobs,
  path,
  event,
  now,
  jobLink,
}: {
  graph: RunGraph
  jobs: RunJob[]
  path: string
  event: string
  now: number
  jobLink: JobLinkFn
}) {
  const [open, setOpen] = useState<Set<string>>(new Set())
  const byId = new Map(jobs.map((j) => [j.id, j]))
  const names = new Map(graph.nodes.map((n) => [n.key, n.name]))
  const { placed, width, height } = layout(graph.nodes, open)

  function label(job: RunJob, node: GraphNode): string {
    const needs = node.needs.map((k) => names.get(k) ?? k)
    return `${job.name}, ${statusText(job.status, job.conclusion)}${needs.length > 0 ? `, needs ${needs.join(", ")}` : ""}`
  }

  function toggle(key: string) {
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  return (
    <Section
      title={path.split("/").pop() ?? path}
      aside={
        event ? (
          <span className="text-sm text-muted-foreground">
            on: <span className="font-mono">{event}</span>
          </span>
        ) : undefined
      }
    >
      <div role="group" aria-label="Job graph" tabIndex={0} className="overflow-x-auto pb-2">
        <div className="relative" style={{ width, height }}>
          <svg aria-hidden="true" width={width} height={height} className="absolute inset-0 text-border">
            {edges(placed).map((d) => (
              <path key={d} d={d} fill="none" stroke="currentColor" strokeWidth={1.5} />
            ))}
          </svg>
          {placed.map(({ node, x, y, h }) => {
            const style = { left: x, top: y, width: TILE_W, height: h }
            const first = node.job_ids[0]
            const single = first !== undefined ? byId.get(first) : undefined
            if (!node.group && single) {
              return (
                <div key={node.key} className="absolute" style={style}>
                  {jobLink(single.id, <JobLine job={single} now={now} />, {
                    className: "flex h-full items-center gap-2 rounded-[6px] border border-border bg-card px-3 text-sm hover:bg-muted",
                    label: label(single, node),
                  })}
                </div>
              )
            }
            const isOpen = open.has(node.key)
            const members = node.job_ids.flatMap((id) => {
              const j = byId.get(id)
              return j ? [j] : []
            })
            const shown = isOpen ? members : members.slice(0, GROUP_SHOWN)
            const hidden = members.length - GROUP_SHOWN
            return (
              <div key={node.key} className="absolute flex flex-col rounded-[6px] border border-border bg-card px-3 py-1" style={style}>
                <p className="flex h-8 items-center gap-2 text-sm font-medium">
                  <span className="min-w-0 flex-1 truncate">{node.name}</span>
                  <span className="font-mono text-xs text-muted-foreground">{members.length}</span>
                </p>
                <ul className="flex flex-col">
                  {shown.map((j) => (
                    <li key={j.id} className="h-7">
                      {jobLink(j.id, <JobLine job={j} now={now} />, {
                        className: "flex h-full items-center gap-2 rounded-[4px] px-1 text-sm hover:bg-muted",
                        label: label(j, node),
                      })}
                    </li>
                  ))}
                </ul>
                {hidden > 0 && (
                  <button
                    type="button"
                    aria-expanded={isOpen}
                    onClick={() => toggle(node.key)}
                    className="h-7 px-1 text-left text-sm text-primary hover:underline"
                  >
                    {isOpen ? "Show fewer" : `${hidden} more`}
                  </button>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </Section>
  )
}
```

- [ ] **Step 4: Mount it on the run page**

In `web/src/pages/run.tsx`, make these replacements in order; each `Replace` text occurs exactly once in the file.

1. Replace:

```tsx
import { RunFrame, type JobLinkFn, type SummaryLinkFn } from "@/components/run-frame"
```

   with:

```tsx
import { RunFrame, type JobLinkFn, type SummaryLinkFn } from "@/components/run-frame"
import { JobGraph } from "@/components/run-graph"
```

2. Replace:

```tsx
      <AnnotationsSection data=
```

   with:

```tsx
      {run.graph && <JobGraph graph={run.graph} jobs={run.jobs} path={run.path} event={run.event} now={now} jobLink={jobLink} />}
      <AnnotationsSection data=
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/components/run-graph.test.tsx src/pages/run.test.tsx`
Expected: PASS.

- [ ] **Step 6: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both exit 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/run-graph.tsx web/src/components/run-graph.test.tsx web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "feat(web): draw a run's job graph"
```

---

### Task 22: Jobs timeline

**Files:**
- Create: `web/src/components/run-timeline.tsx`
- Test: `web/src/components/run-timeline.test.tsx`
- Modify: `web/src/pages/run.tsx` (an import and the graph mount inside `RunSummary`), `web/src/pages/run.test.tsx` (one test)

**Interfaces:**
- Consumes: `RunDetail`, `RunJob`, `fixtures.runDetail` (Contracts, types and test fixtures); `isRunActive` (Contracts, run helpers); `RunStatusIcon`; `JobLinkFn` (Contracts, run frame); `RunSummary` (as Task 21 left it).
- Produces: `JobsTimeline` (Contracts additions, sections).

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Notes for this task: spec §3.4. One row per job sorted by start (jobs that never started last, in API order); a bar from start to completion (or now while running) against minutes since the run's start, axis ticks at whole minutes in mono and `aria-hidden`; the duration as mono text; "skipped" or "queued" in place of bar and duration for a job that never started. The note under the title is the daemon's `graph_note`, shown as given.

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/run-timeline.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RunDetail } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import type { JobLinkFn } from "./run-frame"
import { JobsTimeline } from "./run-timeline"

const NOW = Date.parse("2026-10-03T14:05:00Z")

const jobLink: JobLinkFn = (jobId, children, opts = {}) => (
  <a href={`#job-${jobId}`} className={opts.className}>
    {children}
  </a>
)

function show(over: Partial<RunDetail> = {}) {
  const run: RunDetail = { ...fixtures.runDetail, graph: undefined, graph_note: "This run has no workflow file to read.", ...over }
  const view = render(<JobsTimeline run={run} now={NOW} jobLink={jobLink} />)
  return { ...view, section: within(screen.getByRole("region", { name: "Jobs timeline" })) }
}

describe("JobsTimeline", () => {
  it.each([
    "Add Contents: read to the token to see which jobs depend on which.",
    "This run has no workflow file to read.",
    "The workflow file at this commit does not match the run's jobs.",
    "ghr could not read .github/workflows/ci.yml.",
  ])("says why there is no graph: %s", (note) => {
    const { section } = show({ graph_note: note })
    expect(section.getByText(note)).toBeInTheDocument()
  })

  it("lists the jobs by start with their durations as text", () => {
    const { section } = show()
    const links = section.getAllByRole("link")
    expect(links.map((l) => l.textContent)).toEqual([
      "Pick runner",
      "Backend tests",
      "Frontend tests (1/2)",
      "Frontend tests (2/2)",
      "E2E (api)",
    ])
    expect(links[1]).toHaveAttribute("href", "#job-1002")
    const row = within(links[1]?.closest("li") as HTMLElement)
    expect(row.getByText("2m15s")).toHaveClass("font-mono")
  })

  it("draws each bar against minutes since the run started", () => {
    const { container } = show()
    const ticks = container.querySelector('[aria-hidden="true"].font-mono') as HTMLElement
    expect(ticks.textContent).toBe("0m1m2m3m4m")
    const bars = container.querySelectorAll<HTMLElement>("[data-bar]")
    expect(bars).toHaveLength(5)
    expect(bars[0]?.style.left).toBe("0%")
  })

  it("says skipped or queued for a job that never started", () => {
    const [first, second, ...rest] = fixtures.runDetail.jobs
    if (!first || !second) throw new Error("fixture has too few jobs")
    const { section } = show({
      jobs: [
        { ...first, status: "completed", conclusion: "skipped", started_at: undefined, completed_at: undefined },
        { ...second, status: "queued", conclusion: "", started_at: undefined, completed_at: undefined },
        ...rest,
      ],
    })
    expect(section.getByText("skipped")).toBeInTheDocument()
    expect(section.getByText("queued")).toBeInTheDocument()
    expect(section.getAllByRole("link").at(-1)).toHaveTextContent("Backend tests")
  })
})
```

In `web/src/pages/run.test.tsx`, insert before the line `  it("links each annotation to its line in the job's log", async () => {`:

```tsx
  it("draws the jobs timeline with the reason when there is no graph", async () => {
    const note = "Add Contents: read to the token to see which jobs depend on which."
    mockApi(runRoutes({ "GET /api/actions/darkmem/runs/102": { ...fixtures.runDetail, graph: undefined, graph_note: note } }))
    renderApp("/actions/darkmem/runs/102")
    const timeline = within(await screen.findByRole("region", { name: "Jobs timeline" }))
    expect(timeline.getByText(note)).toBeInTheDocument()
    expect(timeline.getByRole("link", { name: /Backend tests/ })).toHaveAttribute("href", "/actions/darkmem/runs/102/jobs/1002?attempt=2")
    expect(screen.queryByRole("group", { name: "Job graph" })).toBeNull()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/components/run-timeline.test.tsx src/pages/run.test.tsx`
Expected: FAIL — `./run-timeline` cannot be resolved, and the page has no "Jobs timeline" region.

- [ ] **Step 3: Write the timeline**

Create `web/src/components/run-timeline.tsx`:

```tsx
import type { RunDetail, RunJob } from "@/api/types"
import { Section } from "@/components/page/section"
import type { JobLinkFn } from "@/components/run-frame"
import { RunStatusIcon } from "@/components/run-status-icon"
import { dur } from "@/lib/format"
import { isRunActive } from "@/lib/run-details"

const MINUTE = 60_000

function jobEnd(job: RunJob, now: number): number | undefined {
  if (!job.started_at) return undefined
  return job.completed_at ? Date.parse(job.completed_at) : now
}

// Started jobs by start time, then the ones that never started, each group in
// the jobs API's order.
function byStart(jobs: RunJob[]): RunJob[] {
  const started = jobs.filter((j) => j.started_at)
  const rest = jobs.filter((j) => !j.started_at)
  return [...started.sort((a, b) => Date.parse(a.started_at ?? "") - Date.parse(b.started_at ?? "")), ...rest]
}

export function JobsTimeline({ run, now, jobLink }: { run: RunDetail; now: number; jobLink: JobLinkFn }) {
  const from = Date.parse(run.started_at)
  const ends = run.jobs.flatMap((j) => {
    const e = jobEnd(j, now)
    return e === undefined ? [] : [e]
  })
  const to = Math.max(from + MINUTE, isRunActive(run.status) ? now : from, ...ends)
  const span = to - from
  const ticks = Array.from({ length: Math.floor(span / MINUTE) + 1 }, (_, i) => i)
  const pct = (t: number) => `${(Math.max(0, t - from) / span) * 100}%`

  return (
    <Section title="Jobs timeline">
      {run.graph_note && <p className="-mt-2 mb-3 text-sm text-muted-foreground">{run.graph_note}</p>}
      <div className="grid grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)_4.5rem] items-center gap-x-3">
        <span />
        <div aria-hidden="true" className="relative h-5 font-mono text-xs text-muted-foreground">
          {ticks.map((m) => (
            <span key={m} className="absolute -translate-x-1/2" style={{ left: `${((m * MINUTE) / span) * 100}%` }}>
              {m}m
            </span>
          ))}
        </div>
        <span />
      </div>
      <ul className="flex flex-col">
        {byStart(run.jobs).map((job) => {
          const end = jobEnd(job, now)
          const start = job.started_at ? Date.parse(job.started_at) : undefined
          return (
            <li
              key={job.id}
              className="grid grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)_4.5rem] items-center gap-x-3 border-b border-border py-1.5 text-sm last:border-b-0"
            >
              {jobLink(
                job.id,
                <>
                  <RunStatusIcon status={job.status} conclusion={job.conclusion} />
                  <span className="min-w-0 truncate">{job.name}</span>
                </>,
                { className: "flex min-w-0 items-center gap-2 hover:underline" },
              )}
              {start !== undefined && end !== undefined ? (
                <>
                  <span aria-hidden="true" className="relative h-2 rounded-[2px] bg-muted">
                    <span
                      data-bar="true"
                      className={`absolute inset-y-0 rounded-[2px] ${isRunActive(job.status) ? "bg-primary" : "bg-primary/60"}`}
                      style={{ left: pct(start), width: `max(2px, ${((end - Math.max(start, from)) / span) * 100}%)` }}
                    />
                  </span>
                  <span className="text-right font-mono text-xs text-muted-foreground">{dur(end - start)}</span>
                </>
              ) : (
                <>
                  <span className="text-xs text-muted-foreground">{job.conclusion === "skipped" ? "skipped" : "queued"}</span>
                  <span />
                </>
              )}
            </li>
          )
        })}
      </ul>
    </Section>
  )
}
```

- [ ] **Step 4: Show it when there is no graph**

In `web/src/pages/run.tsx`, make these replacements in order; each `Replace` text occurs exactly once in the file.

1. Replace:

```tsx
import { RunStatusIcon } from "@/components/run-status-icon"
```

   with:

```tsx
import { RunStatusIcon } from "@/components/run-status-icon"
import { JobsTimeline } from "@/components/run-timeline"
```

2. Replace:

```tsx
      {run.graph && <JobGraph graph={run.graph} jobs={run.jobs} path={run.path} event={run.event} now={now} jobLink={jobLink} />}
```

   with:

```tsx
      {run.graph ? (
        <JobGraph graph={run.graph} jobs={run.jobs} path={run.path} event={run.event} now={now} jobLink={jobLink} />
      ) : (
        <JobsTimeline run={run} now={now} jobLink={jobLink} />
      )}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/components/run-timeline.test.tsx src/pages/run.test.tsx`
Expected: PASS.

- [ ] **Step 6: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both exit 0.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/run-timeline.tsx web/src/components/run-timeline.test.tsx web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "feat(web): draw a jobs timeline without a graph"
```

---

### Task 23: ANSI tokenizer

**Files:**
- Create: `web/src/lib/ansi.tsx`
- Modify: `web/src/styles/ghr-theme.css` (log tokens at the end of the `:root[data-mode="dark"]` block, after `--chart-5` at line 76, and of the `:root[data-mode="light"]` block, after `--chart-5` at line 128; classes before `.ghr-mark:focus-visible .ghr-mark-box`)
- Test: `web/src/lib/ansi.test.tsx`

**Interfaces:**
- Consumes: none
- Produces: `AnsiSegment`, `ansiSegments`, `plainText`, `matchRanges`, `renderAnsi` (Contracts, TypeScript — log viewer); the `--log-*` tokens and `ghr-log-*` classes

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/ansi.test.tsx`:

```tsx
import { render } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { ansiSegments, matchRanges, plainText, renderAnsi } from "./ansi"

function html(raw: string, query?: string, current?: number): HTMLElement {
  return render(<p>{renderAnsi(raw, query, current)}</p>).container.firstElementChild as HTMLElement
}

describe("ansiSegments", () => {
  it("keeps plain text as one segment", () => {
    expect(ansiSegments("ruff check .")).toEqual([{ text: "ruff check .", color: undefined, bold: false }])
    expect(ansiSegments("")).toEqual([])
  })

  it("reads the 8 and bright 8 foreground colours, bold and resets", () => {
    expect(ansiSegments("\u001b[31mFound 1 error.\u001b[0m done")).toEqual([
      { text: "Found 1 error.", color: "red", bold: false },
      { text: " done", color: undefined, bold: false },
    ])
    expect(ansiSegments("\u001b[1;92mok\u001b[22m still green\u001b[39m plain")).toEqual([
      { text: "ok", color: "bright-green", bold: true },
      { text: " still green", color: "bright-green", bold: false },
      { text: " plain", color: undefined, bold: false },
    ])
    expect(ansiSegments("\u001b[1mbold\u001b[mreset")).toEqual([
      { text: "bold", color: undefined, bold: true },
      { text: "reset", color: undefined, bold: false },
    ])
  })

  it("skips 256-colour and true-colour arguments", () => {
    expect(ansiSegments("\u001b[38;5;31ma\u001b[38;2;31;32;33mb\u001b[48;5;1mc")).toEqual([
      { text: "abc", color: undefined, bold: false },
    ])
  })

  it.each([
    ["other CSI sequences", "\u001b[2K\u001b[1Gprogress\u001b[?25l", "progress"],
    ["an OSC hyperlink ended by ST", "\u001b]8;;https://example.com\u001b\\link\u001b]8;;\u001b\\", "link"],
    ["an OSC title ended by BEL", "\u001b]0;title\u0007text", "text"],
    ["an unterminated OSC", "text\u001b]0;title", "text"],
    ["other escapes", "a\u001b(Bb\u001b=c", "abc"],
    ["C0 controls other than tab", "a\u0000b\tc\u0007d\u0008e\u001b", "ab\tcde"],
  ])("removes %s", (_name, raw, want) => {
    expect(plainText(raw)).toBe(want)
  })

  it("shows only the last carriage-return segment of a line", () => {
    expect(plainText("10%\r50%\r100%")).toBe("100%")
    expect(plainText("done\r")).toBe("done")
    expect(plainText("\u001b[32m10%\r\u001b[31m100%")).toBe("100%")
  })
})

describe("matchRanges", () => {
  it("finds case-insensitive, non-overlapping occurrences", () => {
    expect(matchRanges("Error: error", "ERROR")).toEqual([
      [0, 5],
      [7, 12],
    ])
    expect(matchRanges("aaaa", "aa")).toEqual([
      [0, 2],
      [2, 4],
    ])
    expect(matchRanges("text", "")).toEqual([])
  })
})

describe("renderAnsi", () => {
  it("renders colours and bold as classed spans", () => {
    const p = html("\u001b[1;31mFAIL\u001b[0m test")
    const span = p.querySelector("span")
    expect(span).toHaveTextContent("FAIL")
    expect(span).toHaveClass("ghr-log-ansi-red", "font-semibold")
    expect(p).toHaveTextContent("FAIL test")
  })

  it("renders markup in the log as text", () => {
    const p = html("<img src=x onerror=alert(1)>")
    expect(p.querySelector("img")).toBeNull()
    expect(p).toHaveTextContent("<img src=x onerror=alert(1)>")
  })

  it("marks every occurrence of the query and flags the current one", () => {
    const p = html("Error: error", "error", 1)
    const marks = p.querySelectorAll("mark")
    expect([...marks].map((m) => m.textContent)).toEqual(["Error", "error"])
    expect(marks[0]).not.toHaveAttribute("data-current")
    expect(marks[1]).toHaveAttribute("data-current", "true")
    expect(marks[1]).toHaveClass("ghr-log-match")
  })

  it("marks an occurrence that crosses a colour change", () => {
    const p = html("\u001b[31mFou\u001b[0mnd it", "found", 0)
    const marks = p.querySelectorAll("mark")
    expect([...marks].map((m) => m.textContent)).toEqual(["Fou", "nd"])
    expect(marks[0]?.querySelector("span")).toHaveClass("ghr-log-ansi-red")
    expect(p).toHaveTextContent("Found it")
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/lib/ansi.test.tsx`
Expected: FAIL: `Failed to resolve import "./ansi"`.

- [ ] **Step 3: Write the tokenizer**

Create `web/src/lib/ansi.tsx`. The `eslint-disable-next-line no-control-regex` line is required: `no-control-regex` (from `@eslint/js` recommended) rejects the control bytes the tokenizer exists to match.

```tsx
import type { ReactNode } from "react"

export interface AnsiSegment {
  text: string
  // "red" or "bright-red"; absent for the default colour.
  color?: string
  bold: boolean
}

const COLORS = ["black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"]

// One alternative per thing the tokenizer acts on: a CSI sequence (SGR when
// its final byte is "m"), an OSC sequence ended by BEL, ST or the end of the
// line, any other escape, and the C0 controls other than tab.
// eslint-disable-next-line no-control-regex -- log lines carry raw control bytes
const TOKEN = /\x1b\[([0-?]*)[ -/]*([@-~])|\x1b\][\s\S]*?(?:\x07|\x1b\\|$)|\x1b[ -/]*[0-~]?|[\x00-\x08\x0a-\x1f]/g

// A line rewritten with carriage returns (a progress bar) shows its last
// state; a trailing CR from a CRLF line ending is not a rewrite.
function lastSegment(raw: string): string {
  const line = raw.replace(/\r+$/, "")
  return line.slice(line.lastIndexOf("\r") + 1)
}

function applySgr(params: string, style: { color?: string; bold: boolean }) {
  const codes = params === "" ? [0] : params.split(";").map((p) => (p === "" ? 0 : Number(p)))
  for (let i = 0; i < codes.length; i++) {
    const c = codes[i] ?? 0
    if (c === 0) {
      style.color = undefined
      style.bold = false
    } else if (c === 1) style.bold = true
    else if (c === 22) style.bold = false
    else if (c === 39) style.color = undefined
    else if (c >= 30 && c <= 37) style.color = COLORS[c - 30]
    else if (c >= 90 && c <= 97) style.color = `bright-${COLORS[c - 90]}`
    else if (c === 38 || c === 48) {
      // 256-colour and true-colour arguments are skipped, not read as codes.
      if (codes[i + 1] === 5) i += 2
      else if (codes[i + 1] === 2) i += 4
    }
  }
}

export function ansiSegments(raw: string): AnsiSegment[] {
  const line = lastSegment(raw)
  const out: AnsiSegment[] = []
  const style: { color?: string; bold: boolean } = { bold: false }
  const push = (text: string) => {
    if (!text) return
    const last = out[out.length - 1]
    if (last && last.color === style.color && last.bold === style.bold) last.text += text
    else out.push({ text, color: style.color, bold: style.bold })
  }
  let at = 0
  for (const m of line.matchAll(TOKEN)) {
    push(line.slice(at, m.index))
    at = m.index + m[0].length
    if (m[2] === "m") applySgr(m[1] ?? "", style)
  }
  push(line.slice(at))
  return out
}

export function plainText(raw: string): string {
  return ansiSegments(raw)
    .map((s) => s.text)
    .join("")
}

// Case-insensitive, non-overlapping occurrences of query in text, as
// [start, end) offsets.
export function matchRanges(text: string, query: string): [number, number][] {
  if (!query) return []
  const hay = text.toLowerCase()
  const needle = query.toLowerCase()
  const out: [number, number][] = []
  for (let i = hay.indexOf(needle); i >= 0; i = hay.indexOf(needle, i + needle.length)) out.push([i, i + needle.length])
  return out
}

function segmentClass(s: AnsiSegment): string | undefined {
  const names = [s.color ? `ghr-log-ansi-${s.color}` : "", s.bold ? "font-semibold" : ""].filter(Boolean)
  return names.length > 0 ? names.join(" ") : undefined
}

// Renders one log line as React elements: coloured and bold spans, and a
// <mark> on each occurrence of query, the one numbered current marked as the
// current match.
export function renderAnsi(raw: string, query = "", current?: number): ReactNode[] {
  const segments = ansiSegments(raw)
  const ranges = matchRanges(segments.map((s) => s.text).join(""), query)
  const out: ReactNode[] = []
  let offset = 0
  let r = 0
  for (const s of segments) {
    const cls = segmentClass(s)
    let i = 0
    while (i < s.text.length) {
      const pos = offset + i
      while (r < ranges.length && (ranges[r]?.[1] ?? 0) <= pos) r++
      const range = ranges[r]
      const inside = range !== undefined && range[0] <= pos
      const end = Math.min(s.text.length, (inside ? range[1] : (range?.[0] ?? Infinity)) - offset)
      const text = s.text.slice(i, end)
      const key = out.length
      const piece = cls ? (
        <span key={key} className={cls}>
          {text}
        </span>
      ) : (
        text
      )
      out.push(
        inside ? (
          <mark key={key} className="ghr-log-match" data-current={r === current ? "true" : undefined}>
            {piece}
          </mark>
        ) : (
          piece
        ),
      )
      i = end
    }
    offset += s.text.length
  }
  return out
}
```

- [ ] **Step 4: Add the log palette**

In `web/src/styles/ghr-theme.css`, insert these lines before the closing `}` of the `:root[data-mode="dark"]` block (after `  --chart-5: hsl(214.3 12.4% 55.7%) !important;`) and again, identical, before the closing `}` of the `:root[data-mode="light"]` block (after `  --chart-5: hsl(214.8 15.0% 40.6%) !important;`). They carry no `!important`: the kit's engine does not write them.

```css
  /* The log viewer is dark in both modes, like GitHub's. */
  --log-bg: 213.3 33.3% 5.3%;
  --log-fg: 212.0 24.6% 88.0%;
  --log-muted: 214.3 12.4% 58.0%;
  --log-command: 188.3 62.7% 56.9%;
  --log-error-bg: 5.4 50.0% 14.0%;
  --log-error-fg: 5.4 81.9% 72.0%;
  --log-warning-bg: 37.7 50.0% 12.0%;
  --log-warning-fg: 37.7 71.6% 62.0%;
  --log-match-bg: 45.0 70.0% 24.0%;
  --log-current: 45.0 95.0% 60.0%;
  --log-target-bg: 213.0 28.6% 13.7%;
  --log-ansi-black: 212.0 9.0% 57.0%;
  --log-ansi-red: 3.0 100.0% 72.0%;
  --log-ansi-green: 128.0 49.0% 52.0%;
  --log-ansi-yellow: 40.0 72.0% 52.0%;
  --log-ansi-blue: 212.0 100.0% 67.0%;
  --log-ansi-magenta: 266.0 100.0% 77.0%;
  --log-ansi-cyan: 184.0 60.0% 52.0%;
  --log-ansi-white: 212.0 13.0% 73.0%;
  --log-ansi-bright-black: 212.0 9.0% 62.0%;
  --log-ansi-bright-red: 3.0 100.0% 80.0%;
  --log-ansi-bright-green: 128.0 60.0% 62.0%;
  --log-ansi-bright-yellow: 44.0 87.0% 60.0%;
  --log-ansi-bright-blue: 212.0 100.0% 78.0%;
  --log-ansi-bright-magenta: 270.0 100.0% 85.0%;
  --log-ansi-bright-cyan: 184.0 58.0% 62.0%;
  --log-ansi-bright-white: 210.0 60.0% 98.0%;
```

Then insert these classes directly before `.ghr-mark:focus-visible .ghr-mark-box {`:

```css
.ghr-log {
  background: hsl(var(--log-bg));
  color: hsl(var(--log-fg));
}

.ghr-log-muted {
  color: hsl(var(--log-muted));
}

.ghr-log-command {
  color: hsl(var(--log-command));
}

.ghr-log-error {
  background: hsl(var(--log-error-bg));
  color: hsl(var(--log-error-fg));
}

.ghr-log-warning {
  background: hsl(var(--log-warning-bg));
  color: hsl(var(--log-warning-fg));
}

.ghr-log-target {
  background: hsl(var(--log-target-bg));
}

.ghr-log-match {
  background: hsl(var(--log-match-bg));
  color: inherit;
}

.ghr-log-match[data-current="true"] {
  outline: 2px solid hsl(var(--log-current));
}

.ghr-log-ansi-black {
  color: hsl(var(--log-ansi-black));
}
.ghr-log-ansi-red {
  color: hsl(var(--log-ansi-red));
}
.ghr-log-ansi-green {
  color: hsl(var(--log-ansi-green));
}
.ghr-log-ansi-yellow {
  color: hsl(var(--log-ansi-yellow));
}
.ghr-log-ansi-blue {
  color: hsl(var(--log-ansi-blue));
}
.ghr-log-ansi-magenta {
  color: hsl(var(--log-ansi-magenta));
}
.ghr-log-ansi-cyan {
  color: hsl(var(--log-ansi-cyan));
}
.ghr-log-ansi-white {
  color: hsl(var(--log-ansi-white));
}
.ghr-log-ansi-bright-black {
  color: hsl(var(--log-ansi-bright-black));
}
.ghr-log-ansi-bright-red {
  color: hsl(var(--log-ansi-bright-red));
}
.ghr-log-ansi-bright-green {
  color: hsl(var(--log-ansi-bright-green));
}
.ghr-log-ansi-bright-yellow {
  color: hsl(var(--log-ansi-bright-yellow));
}
.ghr-log-ansi-bright-blue {
  color: hsl(var(--log-ansi-bright-blue));
}
.ghr-log-ansi-bright-magenta {
  color: hsl(var(--log-ansi-bright-magenta));
}
.ghr-log-ansi-bright-cyan {
  color: hsl(var(--log-ansi-bright-cyan));
}
.ghr-log-ansi-bright-white {
  color: hsl(var(--log-ansi-bright-white));
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/lib/ansi.test.tsx src/styles/theme.test.ts`
Expected: PASS (15 ANSI tests; the theme tests still pass, since the new tokens add keys the engine does not write).

- [ ] **Step 6: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both pass.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/ansi.tsx web/src/lib/ansi.test.tsx web/src/styles/ghr-theme.css
git commit -m "feat(web): add the ANSI tokenizer and log palette"
```

---

### Task 24: Log view model

**Files:**
- Create: `web/src/lib/log-view.ts`
- Test: `web/src/lib/log-view.test.ts`

**Interfaces:**
- Consumes: `JobLog`, `LogLine`, `fixtures.jobLog` (Task 13); `matchRanges`, `plainText` (Task 23)
- Produces: `LOG_WINDOW`, `LogRow`, `groupRows`, `containingGroup`, `windowStart`, `LogMatch`, `findMatches` (Contracts, TypeScript — log viewer)

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/log-view.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { JobLog, LogLine } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { containingGroup, findMatches, groupRows, LOG_WINDOW, windowStart } from "./log-view"

const line = (n: number, kind = "", text = `line ${n}`): LogLine => ({ n, kind, text })
const many = (count: number): LogLine[] => Array.from({ length: count }, (_, i) => line(i + 1))

describe("groupRows", () => {
  it("folds a group's lines under it and hides endgroup", () => {
    const lint = fixtures.jobLog.steps[2]?.lines ?? []
    const rows = groupRows(lint)
    expect(rows.map((r) => [r.type, r.line.n])).toEqual([
      ["group", 1],
      ["line", 4],
      ["line", 5],
      ["line", 6],
      ["line", 7],
    ])
    const group = rows[0]
    expect(group?.type === "group" ? group.lines.map((l) => l.n) : []).toEqual([2])
  })

  it("runs a group without endgroup to the end and lets a new group close the last", () => {
    const rows = groupRows([line(1, "group", "a"), line(2), line(3, "group", "b"), line(4), line(5)])
    expect(rows.map((r) => (r.type === "group" ? [r.line.n, r.lines.map((l) => l.n)] : [r.line.n]))).toEqual([
      [1, [2]],
      [3, [4, 5]],
    ])
  })
})

describe("containingGroup", () => {
  const lines = [line(1, "group", "a"), line(2), line(3, "endgroup", ""), line(4)]
  it.each([
    [2, 1],
    [1, undefined],
    [4, undefined],
    [9, undefined],
  ])("line %i is inside group %j", (n, want) => {
    expect(containingGroup(lines, n)).toBe(want)
  })
})

describe("windowStart", () => {
  it("renders every line of a step under the window", () => {
    expect(windowStart(many(LOG_WINDOW))).toBe(0)
  })

  it("renders the last 5,000 lines of a longer step", () => {
    expect(windowStart(many(6000))).toBe(1000)
    expect(windowStart(many(6000), 5500)).toBe(1000)
  })

  it("reaches back to a line above the cut", () => {
    expect(windowStart(many(6000), 10)).toBe(9)
    expect(windowStart(many(6000), 99999)).toBe(1000)
  })
})

describe("findMatches", () => {
  it("finds every occurrence, case-insensitively, without ANSI codes, in step order", () => {
    expect(findMatches(fixtures.jobLog, "FOUND")).toEqual([{ step: 3, line: 4, index: 0 }])
    expect(findMatches(fixtures.jobLog, "ruff").map((m) => [m.step, m.line, m.index])).toEqual([
      [2, 2, 0],
      [3, 1, 0],
      [3, 2, 0],
      [3, 6, 0],
    ])
  })

  it("counts several occurrences in one line and skips endgroup lines", () => {
    const log: JobLog = {
      job_id: 1,
      too_large: false,
      expired: false,
      steps: [{ number: 1, name: "s", lines: [line(1, "", "aa aa"), line(2, "endgroup", "aa")] }],
    }
    expect(findMatches(log, "aa")).toEqual([
      { step: 1, line: 1, index: 0 },
      { step: 1, line: 1, index: 1 },
    ])
    expect(findMatches(log, "")).toEqual([])
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/lib/log-view.test.ts`
Expected: FAIL: `Failed to resolve import "./log-view"`.

- [ ] **Step 3: Write the model**

Create `web/src/lib/log-view.ts`:

```ts
import type { JobLog, LogLine } from "@/api/types"
import { matchRanges, plainText } from "@/lib/ansi"

// A step renders at most this many of its last lines until asked for more.
export const LOG_WINDOW = 5000

export type LogRow = { type: "line"; line: LogLine } | { type: "group"; line: LogLine; lines: LogLine[] }

// GitHub's groups do not nest: a group line closes any open group. endgroup
// lines are not shown, and a group without one runs to the end of the step.
export function groupRows(lines: LogLine[]): LogRow[] {
  const rows: LogRow[] = []
  let open: { type: "group"; line: LogLine; lines: LogLine[] } | undefined
  for (const line of lines) {
    if (line.kind === "endgroup") {
      open = undefined
    } else if (line.kind === "group") {
      open = { type: "group", line, lines: [] }
      rows.push(open)
    } else if (open) {
      open.lines.push(line)
    } else {
      rows.push({ type: "line", line })
    }
  }
  return rows
}

// The number of the group line holding line n, or undefined when n is not
// inside a group (a group line itself is always shown).
export function containingGroup(lines: LogLine[], n: number): number | undefined {
  let group: number | undefined
  for (const line of lines) {
    if (line.n === n) return line.kind === "group" ? undefined : group
    if (line.kind === "group") group = line.n
    else if (line.kind === "endgroup") group = undefined
  }
  return undefined
}

// The index of the first line to render: the last LOG_WINDOW lines, reaching
// back to line keep when it falls above the cut.
export function windowStart(lines: LogLine[], keep?: number): number {
  const cut = Math.max(0, lines.length - LOG_WINDOW)
  if (keep === undefined) return cut
  const at = lines.findIndex((l) => l.n === keep)
  return at >= 0 ? Math.min(cut, at) : cut
}

export interface LogMatch {
  step: number
  line: number
  // which occurrence within the line, counting from 0
  index: number
}

// Case-insensitive matches over the text every shown line displays, ANSI
// codes removed, in step and line order.
export function findMatches(log: JobLog, query: string): LogMatch[] {
  if (!query) return []
  const out: LogMatch[] = []
  for (const step of log.steps) {
    for (const line of step.lines) {
      if (line.kind === "endgroup") continue
      matchRanges(plainText(line.text), query).forEach((_, index) => out.push({ step: step.number, line: line.n, index }))
    }
  }
  return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/lib/log-view.test.ts`
Expected: PASS (11 tests).

- [ ] **Step 5: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/log-view.ts web/src/lib/log-view.test.ts
git commit -m "feat(web): add the log view model"
```

---

### Task 25: Log viewer

**Files:**
- Create: `web/src/components/log-viewer.tsx`
- Test: `web/src/components/log-viewer.test.tsx`
- Test: `web/src/styles/theme.test.ts` (a log palette check, inserted before `describe("ghr chrome styles"`)

**Interfaces:**
- Consumes: `JobLog`, `LogLine`, `LogStep`, `Step`, `fixtures` (Task 13); `firstFailedStep`, `RunStatusIcon` (Task 15); `renderAnsi` and the `ghr-log-*` classes (Task 23); `containingGroup`, `findMatches`, `groupRows`, `windowStart`, `LogMatch` (Task 24)
- Produces: `LogViewer` (Contracts, TypeScript — log viewer)

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Behaviour the tests pin: the step rows sit on the page's own background, so their status icons keep the page's contrast; each expanded step's lines sit in a `ghr-log` panel, dark in both modes. The open step and group of the current search match are derived from it rather than stored, so typing a query letter by letter opens no step on the way. `step` and `line` are read once, on mount: the job page keys `LogViewer` on them so a new annotation link re-opens. `Element.prototype.scrollIntoView` is a no-op in tests (`web/src/test/setup.ts:23`). Testing Library joins a step button's parts without spaces (`SkippedTest`, `FailedLint (ruff)1m25s`), so the tests match step buttons by regular expression.

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/log-viewer.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import type { JobLog, LogLine, Step } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { LogViewer } from "./log-viewer"

const steps: Step[] = fixtures.runDetail.jobs[1]?.steps ?? []

function renderViewer(props: Partial<Parameters<typeof LogViewer>[0]> = {}) {
  const user = userEvent.setup()
  const view = render(<LogViewer log={fixtures.jobLog} steps={steps} {...props} />)
  return { ...view, user }
}

const stepButton = (name: RegExp) => screen.getByRole("button", { name })
const lineRow = (n: number, step: string) => {
  const item = screen.getByRole("button", { name: new RegExp(step) }).closest("li") as HTMLElement
  return item.querySelector(`[data-line="${n}"]`) as HTMLElement | null
}

function bigLog(count: number, special: Record<number, string> = {}): JobLog {
  const lines: LogLine[] = Array.from({ length: count }, (_, i) => ({ n: i + 1, kind: "", text: special[i + 1] ?? `row ${i + 1}` }))
  return { job_id: 9, too_large: false, expired: false, steps: [{ number: 1, name: "Build", lines }] }
}
const bigSteps: Step[] = [{ number: 1, name: "Build", status: "completed", conclusion: "failure" }]

describe("LogViewer", () => {
  it("lists every step with its status and duration, the failed one open", () => {
    renderViewer()
    expect(screen.getAllByRole("button", { expanded: false }).length).toBeGreaterThan(0)
    const lint = stepButton(/Lint \(ruff\)/)
    expect(lint).toHaveAttribute("aria-expanded", "true")
    expect(within(lint).getByRole("img", { name: "Failed" })).toBeInTheDocument()
    expect(within(lint).getByText("1m25s")).toHaveClass("font-mono")
    expect(stepButton(/Set up job/)).toHaveAttribute("aria-expanded", "false")
    expect(within(stepButton(/Test$/)).getByRole("img", { name: "Skipped" })).toBeInTheDocument()
    expect(screen.queryByText("Current runner version: '2.338.0'")).toBeNull()
  })

  it("numbers lines per step and styles each kind", () => {
    const { container } = renderViewer()
    const error = lineRow(5, "Lint")
    expect(error).toHaveClass("ghr-log-error")
    expect(error).toHaveTextContent("5Error: src/cache.py:12:5: F401 'os' imported but unused")
    expect(error?.querySelector("span")).toHaveClass("select-none", "ghr-log-muted")
    expect(lineRow(6, "Lint")?.querySelector(".ghr-log-muted.min-w-0")).toHaveTextContent("Finishing: Lint (ruff)")
    expect(lineRow(3, "Lint")).toBeNull()
    const ansi = lineRow(4, "Lint")?.querySelector(".ghr-log-ansi-red")
    expect(ansi).toHaveTextContent("Found 1 error.")
    expect(container.querySelector(".ghr-log")).not.toBeNull()
  })

  it("shows commands in the primary colour and opens steps on click", async () => {
    const { user } = renderViewer()
    await user.click(stepButton(/Install/))
    expect(stepButton(/Install/)).toHaveAttribute("aria-expanded", "true")
    expect(lineRow(1, "Install")?.querySelector(".ghr-log-command")).toHaveTextContent("pip install -r requirements.txt")
    await user.click(stepButton(/Install/))
    expect(lineRow(1, "Install")).toBeNull()
  })

  it("folds groups until opened", async () => {
    const { user } = renderViewer()
    const group = screen.getByRole("button", { name: "Run ruff check ." })
    expect(group).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByText("ruff check .")).toBeNull()
    await user.click(group)
    expect(group).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByText("ruff check .")).toBeInTheDocument()
  })

  it("prefixes notices without a tint and warnings with one", () => {
    const log: JobLog = {
      job_id: 9,
      too_large: false,
      expired: false,
      steps: [
        {
          number: 1,
          name: "Build",
          lines: [
            { n: 1, kind: "notice", text: "Retried 1 flaky test" },
            { n: 2, kind: "warning", text: "Node.js 16 actions are deprecated." },
          ],
        },
      ],
    }
    const { container } = render(<LogViewer log={log} steps={bigSteps} />)
    const notice = container.querySelector('[data-line="1"]')
    expect(notice).toHaveTextContent("Notice: Retried 1 flaky test")
    expect(notice?.className).not.toMatch(/ghr-log-(error|warning)/)
    expect(container.querySelector('[data-line="2"]')).toHaveClass("ghr-log-warning")
    expect(container.querySelector('[data-line="2"]')).toHaveTextContent("Warning: Node.js 16 actions are deprecated.")
  })

  it("opens the step and group of the line in the URL and marks the line", () => {
    renderViewer({ step: 3, line: 2 })
    expect(screen.getByRole("button", { name: "Run ruff check ." })).toHaveAttribute("aria-expanded", "true")
    const row = lineRow(2, "Lint")
    expect(row).toHaveAttribute("data-target", "true")
    expect(row).toHaveClass("ghr-log-target")
  })

  it("opens the step in the URL instead of the failed one", () => {
    renderViewer({ step: 1 })
    expect(stepButton(/Set up job/)).toHaveAttribute("aria-expanded", "true")
    expect(stepButton(/Lint \(ruff\)/)).toHaveAttribute("aria-expanded", "false")
  })

  it("shows the toolbar after the search box", () => {
    renderViewer({ toolbar: <button type="button">Raw log</button> })
    expect(screen.getByRole("searchbox", { name: "Search logs" })).toHaveAttribute("placeholder", "Search logs")
    expect(screen.getByRole("button", { name: "Raw log" })).toBeInTheDocument()
  })

  it("counts matches and opens only the current match's step and group", async () => {
    const { user } = renderViewer()
    const box = screen.getByRole("searchbox", { name: "Search logs" })
    await user.type(box, "RUFF")
    expect(screen.getByText("1 of 4")).toBeInTheDocument()
    expect(stepButton(/Install/)).toHaveAttribute("aria-expanded", "true")
    expect(stepButton(/Set up job/)).toHaveAttribute("aria-expanded", "false")
    const first = lineRow(2, "Install")?.querySelector("mark")
    expect(first).toHaveAttribute("data-current", "true")
    expect(first).toHaveTextContent("ruff")

    await user.keyboard("{Enter}")
    expect(screen.getByText("2 of 4")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Run ruff check ." }).querySelector("mark")).toHaveAttribute("data-current", "true")
    expect(screen.getByRole("button", { name: "Run ruff check ." })).toHaveAttribute("aria-expanded", "false")

    await user.keyboard("{Enter}")
    expect(screen.getByText("3 of 4")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Run ruff check ." })).toHaveAttribute("aria-expanded", "true")
    expect(lineRow(2, "Lint")?.querySelector("mark")).toHaveAttribute("data-current", "true")
    expect(lineRow(6, "Lint")?.querySelector("mark")).not.toHaveAttribute("data-current")

    await user.keyboard("{Shift>}{Enter}{/Shift}")
    expect(screen.getByText("2 of 4")).toBeInTheDocument()
    await user.keyboard("{Shift>}{Enter}{/Shift}{Shift>}{Enter}{/Shift}")
    expect(screen.getByText("4 of 4")).toBeInTheDocument()
  })

  it("says when nothing matches", async () => {
    const { user } = renderViewer()
    expect(screen.queryByText(/ of /)).toBeNull()
    await user.type(screen.getByRole("searchbox", { name: "Search logs" }), "nothing like this")
    expect(screen.getByText("0 of 0")).toBeInTheDocument()
  })

  it("renders the last 5,000 lines of a long step until asked for more", { timeout: 20_000 }, async () => {
    const user = userEvent.setup()
    const { container } = render(<LogViewer log={bigLog(6000)} steps={bigSteps} />)
    expect(container.querySelector('[data-line="1000"]')).toBeNull()
    expect(container.querySelector('[data-line="1001"]')).not.toBeNull()
    await user.click(screen.getByRole("button", { name: "Show 1000 earlier lines" }))
    expect(container.querySelector('[data-line="1"]')).not.toBeNull()
    expect(screen.queryByRole("button", { name: /earlier lines/ })).toBeNull()
  })

  it("renders back to the URL's line when it is above the cut", { timeout: 20_000 }, () => {
    const { container } = render(<LogViewer log={bigLog(6000)} steps={bigSteps} step={1} line={10} />)
    expect(container.querySelector('[data-line="10"]')).toHaveAttribute("data-target", "true")
    expect(container.querySelector('[data-line="9"]')).toBeNull()
    expect(screen.getByRole("button", { name: "Show 9 earlier lines" })).toBeInTheDocument()
  })

  it("renders back to the current match when it is above the cut", { timeout: 20_000 }, async () => {
    const user = userEvent.setup()
    const { container } = render(<LogViewer log={bigLog(6000, { 3: "the needle is here" })} steps={bigSteps} />)
    expect(container.querySelector('[data-line="3"]')).toBeNull()
    await user.type(screen.getByRole("searchbox", { name: "Search logs" }), "needle")
    expect(screen.getByText("1 of 1")).toBeInTheDocument()
    expect(container.querySelector('[data-line="3"] mark')).toHaveAttribute("data-current", "true")
  })
})
```

In `web/src/styles/theme.test.ts`, insert before `describe("ghr chrome styles", () => {`:

```ts
const LOG_COLOURS = ["black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"].flatMap((c) => [
  `--log-ansi-${c}`,
  `--log-ansi-bright-${c}`,
])

describe("log palette", () => {
  it.each(["dark", "light"] as const)("is dark and readable in %s mode", (mode) => {
    const t = tokens(mode)
    expect(luminance(t["--log-bg"] ?? "")).toBeLessThan(0.02)
    for (const fg of ["--log-fg", "--log-muted", "--log-command", ...LOG_COLOURS]) {
      expect(contrast(t[fg], t["--log-bg"]), `${fg} on --log-bg`).toBeGreaterThanOrEqual(4.5)
      expect(contrast(t[fg], t["--log-target-bg"]), `${fg} on --log-target-bg`).toBeGreaterThanOrEqual(4.5)
    }
    expect(contrast(t["--log-error-fg"], t["--log-error-bg"])).toBeGreaterThanOrEqual(4.5)
    expect(contrast(t["--log-warning-fg"], t["--log-warning-bg"])).toBeGreaterThanOrEqual(4.5)
    expect(contrast(t["--log-fg"], t["--log-match-bg"])).toBeGreaterThanOrEqual(4.5)
    expect(contrast(t["--log-current"], t["--log-bg"])).toBeGreaterThanOrEqual(3)
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/components/log-viewer.test.tsx src/styles/theme.test.ts`
Expected: FAIL: `Failed to resolve import "./log-viewer"`. The log palette tests already pass (Task 23 defined the tokens); they guard the contrast from here on.

- [ ] **Step 3: Write the viewer**

Create `web/src/components/log-viewer.tsx`:

```tsx
import { Input } from "darkraise-ui/components/input"
import { ChevronRight } from "lucide-react"
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
import type { JobLog, LogLine, LogStep, Step } from "@/api/types"
import { RunStatusIcon } from "@/components/run-status-icon"
import { renderAnsi } from "@/lib/ansi"
import { dur } from "@/lib/format"
import { containingGroup, findMatches, groupRows, windowStart, type LogMatch } from "@/lib/log-view"
import { firstFailedStep } from "@/lib/run-details"

const PREFIX: Record<string, string | undefined> = { error: "Error: ", warning: "Warning: ", notice: "Notice: " }
const ROW_CLASS: Record<string, string | undefined> = { error: "ghr-log-error", warning: "ghr-log-warning" }
const TEXT_CLASS: Record<string, string | undefined> = { command: "ghr-log-command", debug: "ghr-log-muted" }

const groupKey = (step: number, n: number) => `${step}:${n}`

function stepMs(step: Step | undefined): number | undefined {
  if (!step?.started_at || !step.completed_at) return undefined
  return Date.parse(step.completed_at) - Date.parse(step.started_at)
}

function Line({ line, query, current, target }: { line: LogLine; query: string; current?: number; target: boolean }) {
  return (
    <div
      data-line={line.n}
      data-target={target ? "true" : undefined}
      className={`flex gap-3 px-2 ${ROW_CLASS[line.kind] ?? ""} ${target ? "ghr-log-target" : ""}`}
    >
      <span className="ghr-log-muted w-12 shrink-0 text-right select-none">{line.n}</span>
      <span className={`min-w-0 break-all whitespace-pre-wrap ${TEXT_CLASS[line.kind] ?? ""}`}>
        {PREFIX[line.kind]}
        {renderAnsi(line.text, query, current)}
      </span>
    </div>
  )
}

export function LogViewer({
  log,
  steps,
  step,
  line,
  toolbar,
}: {
  log: JobLog
  steps: Step[]
  step?: number
  line?: number
  toolbar?: ReactNode
}) {
  const target = step !== undefined && line !== undefined ? { step, line } : undefined
  const linesOf = (n: number): LogLine[] => log.steps.find((s) => s.number === n)?.lines ?? []

  const [openSteps, setOpenSteps] = useState<Set<number>>(() => {
    const first = step ?? firstFailedStep(steps)
    return new Set(first === undefined ? [] : [first])
  })
  const [openGroups, setOpenGroups] = useState<Set<string>>(() => {
    if (!target) return new Set()
    const g = containingGroup(linesOf(target.step), target.line)
    return new Set(g === undefined ? [] : [groupKey(target.step, g)])
  })
  const [showAll, setShowAll] = useState<Set<number>>(() => new Set())
  const [query, setQuery] = useState("")
  const [cur, setCur] = useState(0)
  const ref = useRef<HTMLDivElement>(null)

  const matches = useMemo(() => findMatches(log, query), [log, query])
  const current: LogMatch | undefined = matches[cur]

  // The current match's step and group show however the owner left them,
  // so a search opens nothing else.
  const currentGroup = current ? containingGroup(linesOf(current.step), current.line) : undefined
  const stepOpen = (n: number) => openSteps.has(n) || current?.step === n
  const groupOpen = (stepN: number, n: number) =>
    openGroups.has(groupKey(stepN, n)) || (current?.step === stepN && currentGroup === n)

  function onQuery(next: string) {
    setQuery(next)
    setCur(0)
  }

  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key !== "Enter" || matches.length === 0) return
    e.preventDefault()
    const next = (cur + (e.shiftKey ? matches.length - 1 : 1)) % matches.length
    setCur(next)
  }

  function toggle<T>(set: (f: (s: Set<T>) => Set<T>) => void, key: T) {
    set((s) => {
      const out = new Set(s)
      if (!out.delete(key)) out.add(key)
      return out
    })
  }

  useEffect(() => {
    ref.current?.querySelector('[data-target="true"]')?.scrollIntoView({ block: "center" })
  }, [])

  useEffect(() => {
    if (query) ref.current?.querySelector('mark[data-current="true"]')?.scrollIntoView({ block: "center" })
  }, [query, cur])

  function renderLines(s: LogStep) {
    const keep = current?.step === s.number ? current.line : target?.step === s.number ? target.line : undefined
    const start = showAll.has(s.number) ? 0 : windowStart(s.lines, keep)
    const lineProps = (l: LogLine) => ({
      line: l,
      query,
      current: current && current.step === s.number && current.line === l.n ? current.index : undefined,
      target: target?.step === s.number && target.line === l.n,
    })
    return (
      <div className="ghr-log overflow-x-auto rounded-md py-2 font-mono text-xs leading-5">
        {start > 0 && (
          <button
            type="button"
            className="ghr-log-command px-2 pb-1 hover:underline"
            onClick={() => setShowAll((all) => new Set(all).add(s.number))}
          >
            {`Show ${start} earlier lines`}
          </button>
        )}
        {s.lines.length === 0 && <p className="ghr-log-muted px-2">This step wrote no log lines.</p>}
        {groupRows(s.lines.slice(start)).map((row) => {
          if (row.type === "line") return <Line key={row.line.n} {...lineProps(row.line)} />
          const key = groupKey(s.number, row.line.n)
          const open = groupOpen(s.number, row.line.n)
          return (
            <div key={row.line.n}>
              <button
                type="button"
                aria-expanded={open}
                onClick={() => toggle(setOpenGroups, key)}
                className="flex w-full items-start gap-1 text-left hover:underline"
              >
                <ChevronRight aria-hidden="true" size={14} className={`mt-0.5 ml-12 shrink-0 ${open ? "rotate-90" : ""}`} />
                <span className="min-w-0 break-all whitespace-pre-wrap">
                  {renderAnsi(
                    row.line.text,
                    query,
                    current && current.step === s.number && current.line === row.line.n ? current.index : undefined,
                  )}
                </span>
              </button>
              {open && row.lines.map((l) => <Line key={l.n} {...lineProps(l)} />)}
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <div ref={ref} className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <Input
          type="search"
          aria-label="Search logs"
          placeholder="Search logs"
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          onKeyDown={onKey}
          className="w-64"
        />
        {query && (
          <span aria-live="polite" className="font-mono text-sm text-muted-foreground">
            {matches.length === 0 ? "0 of 0" : `${cur + 1} of ${matches.length}`}
          </span>
        )}
        {toolbar}
      </div>
      <ol aria-label="Steps" className="flex flex-col">
        {log.steps.map((s) => {
          const info = steps.find((x) => x.number === s.number)
          const open = stepOpen(s.number)
          const ms = stepMs(info)
          return (
            <li key={s.number} className="border-b border-border last:border-b-0">
              <button
                type="button"
                aria-expanded={open}
                onClick={() => toggle(setOpenSteps, s.number)}
                className="flex w-full items-center gap-2 py-1.5 text-left text-sm hover:bg-muted"
              >
                <ChevronRight aria-hidden="true" size={15} className={`shrink-0 text-muted-foreground ${open ? "rotate-90" : ""}`} />
                {info && <RunStatusIcon status={info.status} conclusion={info.conclusion} />}
                <span className="min-w-0 flex-1 break-words">{s.name}</span>
                {ms !== undefined && <span className="font-mono text-xs text-muted-foreground">{dur(ms)}</span>}
              </button>
              {open && <div className="pb-2">{renderLines(s)}</div>}
            </li>
          )
        })}
      </ol>
    </div>
  )
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/components/log-viewer.test.tsx src/styles/theme.test.ts`
Expected: PASS (13 viewer tests, 16 theme tests). The three 6,000-line tests take a few seconds each.

- [ ] **Step 5: Typecheck, lint and the whole web suite**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint && timeout 600 npm test`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/log-viewer.tsx web/src/components/log-viewer.test.tsx web/src/styles/theme.test.ts
git commit -m "feat(web): add the log viewer"
```

---

### Task 26: Job page logs

**Files:**
- Modify: `web/src/pages/run.tsx` (imports, a new `saveText` and `JobLogs` before `JobView`, `JobView`'s signature and completed branch, `JobPage`)
- Test: `web/src/pages/run.test.tsx` (the vitest import, seven tests in the "Job page" block)

**Interfaces:**
- Consumes: `useJobLog`, `api.rawJobLog` (Contracts, API client and hooks); `JobSearch`, `rawLogFileName` (Contracts, run helpers); `LogViewer` (Contracts, log viewer); `fixtures.jobLog`, `runRoutes` (Contracts, types and test fixtures); `JobView` and `JobPage` (Task 17).
- Produces: none.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

Notes for this task: a completed job's steps come from `LogViewer`, given `step` and `line` from the URL; a running or queued job keeps Task 17's step timeline and note, and asks for no log. The page owns the too-large and expired states (spec §1.4) because `LogViewer` is only given a log with neither flag. The Raw log button fetches `{text}` through `request` (it sends `X-GHR`), wraps it in a Blob and clicks a temporary `<a download>` on an object URL, revoked on the next tick so the download has started. The test stubs `URL` with a subclass (jsdom has no object URLs, and `mockApi` still calls `new URL`) and `HTMLAnchorElement.prototype.click` (jsdom cannot navigate).

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/run.test.tsx`, replace `import { describe, expect, it } from "vitest"` with:

```tsx
import { describe, expect, it, vi } from "vitest"
```

Insert before the line `  it("says logs wait for a running job and lists its steps", async () => {`:

```tsx
  it("shows a finished job's log with search and the raw log button", async () => {
    const { calls } = mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByLabelText("Search logs")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Raw log" })).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/api/actions/darkmem/jobs/1002/log")).toBe(true)
  })

  it("opens the step and marks the line an annotation links to", async () => {
    mockApi(runRoutes())
    renderApp("/actions/darkmem/runs/102/jobs/1002?attempt=2&step=3&line=5")
    await screen.findByLabelText("Search logs")
    await waitFor(() => expect(document.querySelector('[data-target="true"]')).toHaveTextContent("src/cache.py:12:5: F401 'os' imported but unused"))
  })

  it("marks the new line when an annotation link changes step and line", async () => {
    mockApi(runRoutes())
    const { router } = renderApp("/actions/darkmem/runs/102/jobs/1002?attempt=2&step=3&line=5")
    await waitFor(() => expect(document.querySelector('[data-target="true"]')).toHaveTextContent("src/cache.py:12:5"))
    await router.navigate({
      to: "/actions/$repo/runs/$id/jobs/$jobId",
      params: { repo: "darkmem", id: "102", jobId: "1002" },
      search: { attempt: 2, step: 2, line: 2 },
    })
    await waitFor(() => expect(document.querySelector('[data-target="true"]')).toHaveTextContent("Successfully installed ruff-0.6.9"))
  })

  it("shows why the log could not be read and reads it again on Try again", async () => {
    let busy = true
    mockApi(
      runRoutes({
        "GET /api/actions/darkmem/jobs/1002/log": () =>
          busy ? json({ error: "GitHub is still sending this log; try again shortly" }, 503) : fixtures.jobLog,
      }),
    )
    const { user } = renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("GitHub is still sending this log; try again shortly")).toBeInTheDocument()
    busy = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    expect(await screen.findByLabelText("Search logs")).toBeInTheDocument()
  })

  it("offers only the raw log for a log too large to show", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log": { ...fixtures.jobLog, too_large: true, steps: [] } }))
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("This log is too large to show here.")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Raw log" })).toBeInTheDocument()
    expect(screen.queryByLabelText("Search logs")).toBeNull()
  })

  it("says when GitHub no longer keeps the log", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log": { ...fixtures.jobLog, expired: true, steps: [] } }))
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("GitHub no longer keeps this job's log.")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Raw log" })).toBeNull()
  })

  it("saves the raw log through a Blob", async () => {
    const { calls } = mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log/raw": { text: "2026-10-03T13:55:25.0000000Z Set up job\n" } }))
    const create = vi.fn(() => "blob:http://localhost/raw")
    const revoke = vi.fn()
    // jsdom has no object URLs; a subclass keeps new URL(...) working.
    class FakeURL extends URL {
      static createObjectURL = create
      static revokeObjectURL = revoke
    }
    vi.stubGlobal("URL", FakeURL)
    const saved: string[] = []
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      saved.push(`${this.download} ${this.href}`)
    })
    const { user } = renderApp("/actions/darkmem/runs/102/jobs/1002")
    await user.click(await screen.findByRole("button", { name: "Raw log" }))
    await waitFor(() => expect(saved).toEqual(["darkmem-job-1002.log blob:http://localhost/raw"]))
    expect(create).toHaveBeenCalledWith(expect.any(Blob))
    expect(calls.some((c) => c.path === "/api/actions/darkmem/jobs/1002/log/raw")).toBe(true)
    await waitFor(() => expect(revoke).toHaveBeenCalledWith("blob:http://localhost/raw"))
    click.mockRestore()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && timeout 300 npx vitest run src/pages/run.test.tsx`
Expected: FAIL — the seven new tests find no "Search logs" box, no "Raw log" button, no marked line, no "Try again" button and none of the two state texts.

- [ ] **Step 3: Show the log on the job page**

In `web/src/pages/run.tsx`, make these replacements in order; each `Replace` text occurs exactly once in the file.

1. Replace:

```tsx
import { Link, SearchParamError,
```

   with:

```tsx
import { useMutation } from "@tanstack/react-query"
import { Link, SearchParamError,
```

2. Replace:

```tsx
import { Spinner } from "darkraise-ui/components/spinner"
```

   with:

```tsx
import { Button } from "darkraise-ui/components/button"
import { Spinner } from "darkraise-ui/components/spinner"
```

3. Replace:

```tsx
import { ApiError } from "@/api/client"
```

   with:

```tsx
import { ApiError, api } from "@/api/client"
```

4. Replace:

```tsx
import { useConfig, useRunAnnotations, useRunDetail } from "@/api/hooks"
```

   with:

```tsx
import { useConfig, useJobLog, useRunAnnotations, useRunDetail } from "@/api/hooks"
```

5. Replace:

```tsx
import { ErrorLine } from "@/components/page/error-line"
```

   with:

```tsx
import { LogViewer } from "@/components/log-viewer"
import { ErrorLine } from "@/components/page/error-line"
```

6. Replace:

```tsx
positiveInt, runTotalMs, statusText } from "@/lib/run-details"
```

   with:

```tsx
positiveInt, rawLogFileName, runTotalMs, statusText, type JobSearch } from "@/lib/run-details"
```

7. Replace:

```tsx
function JobView({ run, owner, jobId }: { run: RunDetail; owner: string; jobId: number }) {
```

   with:

```tsx
// The Blob goes through a temporary object URL because a navigation to the
// API cannot send X-GHR. The URL is revoked after the click has started the
// download, not during it.
function saveText(text: string, fileName: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }))
  const a = document.createElement("a")
  a.href = url
  a.download = fileName
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

function JobLogs({ repo, job, search }: { repo: string; job: RunJob; search: JobSearch }) {
  const log = useJobLog(repo, job.id, true)
  const raw = useMutation({
    mutationFn: () => api.rawJobLog(repo, job.id),
    onSuccess: (r) => saveText(r.text, rawLogFileName(repo, job.id)),
  })
  const rawButton = (
    <Button size="sm" variant="outline" loading={raw.isPending} onClick={() => raw.mutate()}>
      Raw log
    </Button>
  )
  if (!log.data) {
    return log.isError ? <ErrorLine onRetry={() => void log.refetch()}>{errorText(log.error)}</ErrorLine> : <Spinner label="Loading" />
  }
  if (log.data.expired) return <p className="text-sm text-muted-foreground">GitHub no longer keeps this job&apos;s log.</p>
  if (log.data.too_large) {
    return (
      <div className="flex flex-col items-start gap-2">
        <p className="text-sm text-muted-foreground">This log is too large to show here.</p>
        {rawButton}
      </div>
    )
  }
  // LogViewer reads step and line once, when it mounts; a new annotation
  // link on the same job page must mount it again.
  return (
    <LogViewer
      key={`${job.id}:${search.step ?? ""}:${search.line ?? ""}`}
      log={log.data}
      steps={job.steps}
      step={search.step}
      line={search.line}
      toolbar={rawButton}
    />
  )
}

function JobView({ run, owner, jobId, search }: { run: RunDetail; owner: string; jobId: number; search: JobSearch }) {
```

8. Replace:

```tsx
            <StepList steps={job.steps} now={now} live={false} />
```

   with:

```tsx
            <JobLogs repo={run.repo} job={job} search={search} />
```

9. Replace:

```tsx
<JobView run={run} owner={owner} jobId={jobId ?? 0} />
```

   with:

```tsx
<JobView run={run} owner={owner} jobId={jobId ?? 0} search={search} />
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && timeout 300 npx vitest run src/pages/run.test.tsx`
Expected: PASS (25 tests).

- [ ] **Step 5: Typecheck and lint**

Run: `cd web && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: both exit 0.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "feat(web): show a finished job's logs"
```

---

### Task 27: Accessibility and README

**Files:**
- Modify: `web/src/a11y.test.tsx` (the fixtures import at line 5, a new block appended after line 169)
- Modify: `README.md` (the "Requirements" section, lines 20-21)

**Interfaces:**
- Consumes: `runRoutes` (Contracts, types and test fixtures); the run and job pages (Tasks 17 to 22 and 26); the `LogViewer` search box (Contracts, log viewer).
- Produces: none.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the accessibility tests**

In `web/src/a11y.test.tsx`, replace `import { authedRoutes, fixtures } from "@/test/fixtures"` with:

```tsx
import { authedRoutes, fixtures, runRoutes } from "@/test/fixtures"
```

Append at the end of the file:

```tsx
describe("Run details accessibility", () => {
  it.each([
    [1280, "the side rail"],
    [390, "the stacked rail"],
  ])(
    "has no axe violations on the run page at %ipx (%s)",
    async (width) => {
      setViewport(width)
      mockApi(runRoutes())
      renderApp("/actions/darkmem/runs/102")
      await screen.findByRole("group", { name: "Job graph" })
      await screen.findByRole("link", { name: "Log line 5, Backend tests / Lint (ruff)" })
      expect(await violations()).toEqual([])
    },
    30_000,
  )

  it(
    "has no axe violations on the job page",
    async () => {
      mockApi(runRoutes())
      renderApp("/actions/darkmem/runs/102/jobs/1002?attempt=2&step=3&line=5")
      await screen.findByLabelText("Search logs")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
```

- [ ] **Step 2: Run them**

Run: `cd web && timeout 300 npx vitest run src/a11y.test.tsx`
Expected: PASS. A violation names the rule and the element (for example `aria-prohibited-attr: …`): fix the component that renders it, not the test.

- [ ] **Step 3: Name Contents: read in the README**

In `README.md`, under "## Requirements", replace:

```markdown
- A fine-grained PAT scoped to exactly those repos, with **Administration: read/write** and
  **Actions: read**, and an expiry.
```

with:

```markdown
- A fine-grained PAT scoped to exactly those repos, with **Administration: read/write** and
  **Actions: read**, and an expiry. **Contents: read** is optional: with it, a run's page in
  the web UI draws the job graph from the workflow file; without it, a jobs timeline.
```

- [ ] **Step 4: Run every web check**

Run: `cd web && timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: all pass, `src/anti-slop.test.ts` included.

- [ ] **Step 5: Commit**

```bash
git add web/src/a11y.test.tsx README.md
git commit -m "test(web): check the run pages with axe"
```
