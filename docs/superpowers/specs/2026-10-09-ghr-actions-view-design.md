# ghr web UI: the Actions view

**Date:** 2026-10-09
**Status:** approved section by section in chat on 2026-10-09. Revised the same day for the Fable review (findings 1 to 20 and the nits), with the owner choosing a browser-side repository filter and the kit spinner for running runs.
**Register:** docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md (item 3)
**Builds on:** docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md (spec 1) and docs/superpowers/specs/2026-10-08-ghr-web-ui-pages-design.md (spec 2). Spec 1's visual system, anti-slop rules (§1.7) and shell, and spec 2's page kit and page-wide rules (§1), bind the page here; this spec does not repeat them.
**Code:** `web/`, `internal/api`, `internal/daemon`, `internal/model`, `internal/github`, `internal/config`

## Goal

The owner wants to see which workflows are running on GitHub, not only the jobs ghr's runners take: GitHub's Actions page, but across repositories rather than one at a time. This spec adds an Actions page that lists the active and recent workflow runs of every repository ghr covers, whoever ran them, and lets the owner watch extra repositories that get no runners.

This is spec 3 of the web UI overhaul.

## Decisions

- **Coverage is the configured repositories plus opt-in watched ones** (§1), chosen over configured only and over every repository the token can see.
- **Active and recent runs**, as GitHub's Actions page lists them (§3), chosen over active runs only and over runs expanded into their jobs.
- **Read-only.** Each run links to its GitHub page; there is no cancel or re-run, so the token needs no new permission.
- **A new Actions page under Operate** (§3), chosen over a tab inside History and over a Dashboard panel. History keeps ghr's own jobs.
- **Fetched when the page asks, cached for 15 seconds** (§2), chosen over a background collector and over reusing the tick's poll. Nothing is spent while nobody has the page open.
- **One response for all covered repositories, filtered in the browser** (§2.2, §3.2), chosen over a server-side `?repo=` filter.
- **Delivery is one plan** (§5).

## 1. Watched repositories

### 1.1 Config

- A new top-level key `watch_repos: [name, …]` in `config.yaml`, JSON `watch_repos`, omitted when empty.
- Names are bare repository names under `owner`, like `repos[].name`. GitHub calls go through the same `repoURL`, so a repository of another owner cannot be watched.
- Validation, in `config.Validate`: every name is non-empty, contains no `/`, appears once ignoring case, and is not a configured repository's name ignoring case.
- A watched repository gets no runners, no demand polling, no label check and no activity rows. Only `GET /actions` reads the list.
- `config.Parse` decodes with `KnownFields(true)`, so a binary older than this change refuses a config that has `watch_repos`. A downgrade needs the key removed first; the release notes say so.
- Nothing else writes the key: `PATCH /config` cannot touch it (`model.ConfigPatch` has no such field), and `Store.Update` and the first-run `Configure` work on a clone of the current config, so they keep it.

### 1.2 API

- `POST /watch {name}`:
  - trims the name, and answers 400 when it is empty;
  - answers 409 when the name is a configured repository (including one being removed, until `FinalizeRemovals` deletes it) or is already watched;
  - calls `GetRepo`, mapping not found to the same 400 that `AddRepo` gives ("token cannot see {owner}/{name}: add it to the PAT's repository access first");
  - allows public repositories, because no runner ever serves them;
  - on success appends the name and records the event "watching" for that repository.
- `DELETE /watch/{name}`: removes the name, matching case-insensitively; 404 when it is not watched; records the event "stopped watching". It never calls GitHub, so a repository GitHub no longer shows can always be unwatched.
- `POST /repos` (AddRepo) also removes the name from `watch_repos` in the same config write, so a repository is never in both lists.
- `GET /repos/available` gains `watched: bool` on each entry.
- `api.Backend` (`internal/api/server.go`) gains `Actions(ctx) (model.Actions, error)`, `WatchRepo(ctx, name string) error` and `UnwatchRepo(name string) error`, with the matching routes and the `fakeBackend` in `internal/api/api_test.go`. The daemon's `GitHub` interface already has `ListRecentRuns`.
- A renamed repository keeps answering through GitHub's redirect, so its watch entry keeps working under the old name.
- The local-socket CLI is not extended.

## 2. Backend

### 2.1 GitHub client

`github.Run` grows from `{id, status}` to the fields the page shows:

| Field | JSON |
|---|---|
| `ID` | `id` |
| `Name` (the workflow's name) | `name` |
| `DisplayTitle` | `display_title` |
| `RunNumber` | `run_number` (an integer) |
| `HeadBranch` | `head_branch` |
| `Event` | `event` |
| `Actor.Login` | `actor.login` |
| `Status` | `status` |
| `Conclusion` | `conclusion` (null while not completed) |
| `CreatedAt` | `created_at` |
| `RunStartedAt` (`*time.Time`) | `run_started_at`, which GitHub may leave out |
| `UpdatedAt` | `updated_at` |
| `HTMLURL` | `html_url` |

The existing callers, the tick's demand poll and the label check, read only `ID`. Their behaviour does not change.

### 2.2 `GET /actions`

- **Covered repositories** are the configured ones in config order, including paused ones and ones being removed, then the watched ones in config order. The endpoint takes no parameters.
- **Fetching:**
  - one `ListRecentRuns(repo, 50)` per covered repository, at most four at a time;
  - under `context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)`, as `RunnerSteps` does, because callers share the build and one closing its tab must not cancel it for the others;
  - a repository that has not answered by the deadline gets the error "GitHub did not answer in time" and no runs.
- **Start time:** a run starts at `run_started_at`, or at `created_at` when GitHub left it out. Sorting, elapsed time and duration use this time.
- **The `ghr` mark:** a run is marked `ghr` when its run ID appears in ghr's history, in its pending records (`Manager.Pending`), or in a live instance's job, with repository names compared case-insensitively. If the history read fails, no run is marked and the endpoint still answers.
- **Response** (`model.Actions`):

```json
{
  "fetched_at": "2026-10-09T14:05:00Z",
  "runs": [
    {
      "repo": "darkmem", "id": 102, "run_number": 42, "workflow": "ci",
      "title": "Fix the cache key", "branch": "master", "event": "push",
      "actor": "darkraise", "status": "in_progress", "conclusion": "",
      "started_at": "2026-10-09T14:01:00Z", "updated_at": "2026-10-09T14:04:00Z",
      "html_url": "https://github.com/darkraise/darkmem/actions/runs/102", "ghr": true, "watched": false
    }
  ],
  "repos": [
    { "repo": "darkmem", "watched": false },
    { "repo": "darkcloud", "watched": true, "error": "GitHub rate limit; API calls are paused", "retry_at": "2026-10-09T14:30:00Z" }
  ]
}
```

- **Order:** runs whose status is not `completed` come first, newest start first. Completed runs follow, newest start first. Ties go by repository, then ID.
- **Errors:** a repository whose call fails keeps its row in `repos` with `error` set, and contributes no runs except as the next rule allows. The endpoint itself does not fail on a repository's error.

| GitHub error | `error` | `retry_at` |
|---|---|---|
| not found | "token cannot see {owner}/{name}: add it to the PAT's repository access first" (AddRepo's text) | none |
| rate limit | "GitHub rate limit; API calls are paused" | the client's retry time |
| auth | "GitHub rejected the token: {err}" | none |
| deadline | "GitHub did not answer in time" | none |
| anything else | `err.Error()` | none |

- **Degraded or rate limited:** when the backend's `githubErr()` reports an error (the token was rejected, or calls are paused until `PausedUntil()`), no call is made, and every repository gets that error's message, plus `retry_at` when calls are paused. One rule then covers both this path and a call that fails with a rate-limit or auth error itself: such a repository keeps the runs it had in the cache entry's last successful build, if the entry still holds one. `fetched_at` stays that build's time while every repository is served this way. A daemon restart or a config change (§2.3) loses the kept runs.
- **The tick** meets the same suspension on its next GitHub call, so the Dashboard's rate figures and degraded state need nothing new.

### 2.3 Cache

- One entry with one lock, holding the last response, the time it was built, and the `*config.Config` it was built from.
- An entry built less than 15 seconds ago from the same config pointer is returned as is. Otherwise it is rebuilt.
- The store installs a new config pointer on every `Update`, `Reload` and `Configure` (`internal/daemon/store.go`), and `spawnPlanned` already relies on that identity. So watching, unwatching, adding, removing, `FinalizeRemovals`, a reload and setup all invalidate the entry with no hooks.
- **Memory:** one response, at most 50 runs per covered repository. The GitHub client's ETag cache holds one 50-run body per repository, roughly 150 to 300 KB each.

### 2.4 API budget

- While the page is open: at most one conditional GET per covered repository every 15 seconds, so at most 240 per repository per hour. A 304 answer does not count against GitHub's rate limit, and it still passes through the client's `record()`, so the rate figures follow GitHub's headers.
- The client's ETag cache keys on the full URL, so the page's `per_page=50` and the label check's `per_page=20` are separate entries. The cache resets as a whole at 1000 entries, and on a reload or token change; after a reset each repository pays one full GET.
- This is beside the tick's three conditional `ListRuns` per configured repository every `poll_interval`.

## 3. The Actions page

### 3.1 Placement

- Route `/actions`, nav item "Actions" under Operate between Runners and History, with a lucide `Workflow` icon.
- The query polls every 15 seconds, matching the cache, and like the other polls it pauses while the window is not focused.

### 3.2 Header and filters

- **Summary:** two sentences, with no separator characters. With no repository filter: "2 running, 1 queued across 7 repositories. Updated 14:05." With one: "2 running, 1 queued in darkmem. Updated 14:05." `fetched_at` is shown in local time. Statuses `queued`, `waiting`, `pending` and `requested` count as queued; `in_progress` counts as running.
- **Filters, in the URL like History's, both applied in the browser:**
  - `repo`: a select of All, then the configured repositories, then the watched ones, each watched entry suffixed "(watched)". Its options come from `/config` (`repos[].name` plus `watch_repos`), as History's select does, and a `repo` value not in that list is dropped.
  - `status`: a segmented control of All, Active, Failed and Succeeded. Active is any run that is not completed. Failed and Succeeded are completed runs whose `resultWord(conclusion)` is "Failed" or "Succeeded", so the filter always agrees with the row's icon. An unknown value is dropped.
- A link "Watch more repositories" goes to `/repositories#watched`. The plan confirms that the app's TanStack Router version scrolls to the hash on an in-app link, and scrolls the section into view on mount if it does not.

### 3.3 Lists

- **Errors:** one alert above the lists, one line per repository with an error. It uses `errorText`'s form: "darkcloud: GitHub rate limit; API calls are paused. Retry after 14:30", with `retry_at` in local time. The other repositories' runs still show.
- **In progress:** the runs that are not completed, as one table. Elapsed time is `now − start`, ticking each second like the Dashboard's running bars. The section is hidden when empty, unless the status filter is Active; then it reads "Nothing is running or queued."
- **Recent:** completed runs, filtered first and then capped at 100 rows, grouped by local day of their start. This uses `groupByDay`, made generic over a date accessor, `groupByDay<T>(rows: T[], at: (row: T) => string)`. History passes `finished_at`. As on History, each day is one `<tbody>` whose first row is a `<th scope="rowgroup">`.
- **A row:**
  - A status cell. Completed runs use `ResultIcon`, whose `aria-label` supplies the result word. Queued runs use a muted lucide `Clock` labelled "Queued". Running runs use the kit `Spinner` labelled "Running", as spec 2 does for an in-progress step; the plan checks that it stops under reduced motion.
  - A title cell: `display_title`, falling back to the workflow name, as a link to `html_url` in a new tab. Visually hidden text completes its name as "{title}, {repo} #{run number}, opens in a new tab".
  - Under the title, a wrapping row of facts spaced by gaps, with no separator characters: workflow, repository, #run number, branch (mono), event, actor.
  - Right-aligned cells: the duration (mono, `dur()`: `updated_at − start` when completed, elapsed when not), the start time (mono, `hhmm`, or `monthDay hhmm` before today), and the word "ghr" in muted mono when `ghr` is set. No kit `Badge`.
- **Empty states:**
  - no covered repositories: "No repositories are covered yet." with links "Add a repository" (`/repositories`) and "Watch a repository" (`/repositories#watched`);
  - covered repositories but no runs: "No workflow runs in the covered repositories yet.";
  - filters hide everything: "No runs match these filters." with a "Clear filters" button.
- **Under 768px:** the right-hand cells move below the facts row, the table scrolls inside its `Section` and never the page, and the filters wrap.

### 3.4 Watched repositories on the Repositories page

- A section "Watched repositories" below the configured table. `Section` gains an optional `id` prop, and this one uses `id="watched"`. Its description reads "Their workflow runs show on the Actions page. ghr runs no runners for them."
- Each watched name (mono) has a Remove icon button labelled "Stop watching {name}". There is no confirmation because nothing is lost, and the toast reads "Stopped watching {name}".
- A "Watch a repository" button opens a dialog. The add-repository dialog's listbox, filter and keyboard handling are extracted into a shared `RepoPicker` that both dialogs use. In the picker, configured entries are disabled with the word "added" (as today) and watched ones with the word "watched". Public repositories are listed without the public warning. Choosing one posts `/watch` and toasts "Watching {name}".
- When nothing is watched, the section shows its description and the button only.

## 4. Testing

### Go

- `config`: `watch_repos` validation (empty, slash, duplicate in any case, overlap with a configured repository), the YAML round trip, and that `PATCH /config` keeps the list.
- `github`: the new `Run` fields decode from a recorded API page, including a run without `run_started_at`.
- `daemon`, watch:
  - `POST /watch` answers not found, the conflicts (including a repository being removed) and success;
  - `DELETE /watch` answers 404 and success without calling GitHub;
  - AddRepo removes a watched name;
  - `AvailableRepos` sets `watched`.
- `daemon`, actions:
  - the order, with the not-completed runs first and ties by repository then ID, and the start-time fallback;
  - the merging of configured (paused included) and watched repositories;
  - each row of the error table;
  - a failing repository keeps its row and the others' runs;
  - the 20-second deadline (with a fake that blocks), and that a cancelled request context does not cancel the shared build;
  - `ghr` matching against history, pending records and live instances, in any case, and a failed history read;
  - the TTL, and rebuilding on a new config pointer;
  - the degraded and paused paths make no call, keep the last good runs, and set each error and `retry_at`;
  - a rate-limit error from a call keeps that repository's last good runs;
  - at most four calls run at once.
- `web/fixtures_test.go`: a new `actions` fixture from `model.Actions`; the `config` fixture gains one watched repository; `available-repos` gains `watched`.

### Frontend

- Types: `Config.watch_repos`, `AvailableRepo.watched`, and `Actions`, `ActionsRun` and `ActionsRepo` in `web/src/api/types.ts`.
- `groupByDay` works with both accessors; History's tests keep passing.
- The Actions page: the summary line in both forms; both sections; the status and repository filters and their URL round trip; the error alert; all three empty states; the "ghr" word; the row link names; the ticking elapsed time; the narrow layout.
- The Repositories page: the watched section, watching through the dialog, removing with its toast, and the `RepoPicker` in both dialogs.
- The nav item.
- `web/src/a11y.test.tsx` lists pages explicitly, so it gains an Actions case (both sections, an error line, a "ghr" mark) and a Repositories case with a watched repository. The anti-slop walk covers the new files on its own.

### Manual, before release

Register row 13's check gains the Actions page at 1280px and 390px in both modes, with one watched repository.

## 5. Delivery

One plan, in this order: config, the watch API and the `Run` fields; `GET /actions` and the fixtures; the Actions page; the `RepoPicker` extraction and the watched section on the Repositories page.

## Out of scope

- Cancel and re-run.
- Expanding a run into its jobs, and telling other self-hosted runners from GitHub-hosted ones. Both need a jobs call per run.
- Repositories of owners other than `owner`.
- A Dashboard panel, notifications and a background collector.
- Workflow and branch filters.
