# ghr web UI: the Actions view

**Date:** 2026-10-09
**Status:** approved section by section in chat on 2026-10-09.
**Register:** docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md (item 3)
**Builds on:** docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md (spec 1) and docs/superpowers/specs/2026-10-08-ghr-web-ui-pages-design.md (spec 2). Spec 1's visual system, anti-slop rules and shell, and spec 2's page kit and page-wide rules, bind the page here; this spec does not repeat them.
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
- **Delivery is one plan** (§5).

## 1. Watched repositories

### 1.1 Config

- A new top-level key `watch_repos: [name, …]` in `config.yaml`, JSON `watch_repos`, omitted when empty.
- Names are bare repository names under `owner`, like `repos[].name`. GitHub calls go through the same `repoURL`, so a repository of another owner cannot be watched.
- Validation, in `config.Validate`: every name is non-empty, contains no `/`, appears once ignoring case, and is not a configured repository's name ignoring case.
- A watched repository gets no runners, no demand polling, no label check and no activity rows. Only `GET /actions` reads the list.

### 1.2 API

- `POST /watch {name}`: trims the name; 400 when it is empty; 409 when it is already configured or already watched; then `GetRepo`, mapping not found to the same 400 that `AddRepo` gives ("token cannot see owner/name: add it to the PAT's repository access first"). Public repositories are allowed, because no runner ever serves them. On success it appends the name, records the event "watching" for that repository, and drops the actions cache (§2.3).
- `DELETE /watch/{name}`: removes the name, matching case-insensitively; 404 when it is not watched; records the event "stopped watching"; drops the actions cache.
- `POST /repos` (AddRepo) also removes the name from `watch_repos` in the same config write, so a repository is never in both lists.
- `GET /repos/available` gains `watched: bool` on each entry.
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
| `RunStartedAt` | `run_started_at` |
| `UpdatedAt` | `updated_at` |
| `HTMLURL` | `html_url` |

The tick's demand poll and the label check read only `ID` and `Status`. Their behaviour does not change.

### 2.2 `GET /actions?repo=`

- **Covered repositories** are the configured ones, in config order, then the watched ones in config order. With `repo` set, only that repository, matched case-insensitively; 404 when it is neither configured nor watched.
- **Fetching:** one `ListRecentRuns(repo, 50)` per covered repository, at most four at a time, under the request's context. The client's ETag revalidation already applies to these GETs.
- **The `ghr` mark:** a run is marked `ghr` when its run ID appears in ghr's history, in its pending records, or in a live instance's job. Repository names are compared case-insensitively.
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
      "html_url": "https://github.com/…", "ghr": true, "watched": false
    }
  ],
  "repos": [
    { "repo": "darkmem", "watched": false },
    { "repo": "darkcloud", "watched": true, "error": "GitHub rate limit; API calls are paused", "retry_at": "2026-10-09T14:30:00Z" }
  ]
}
```

- **Order:** runs whose status is not `completed` come first, newest `started_at` first. Completed runs follow, newest `started_at` first. Ties go by repository, then ID.
- **Errors:** a repository whose call fails keeps its row in `repos` with `error` set to the same text the Repositories page shows for GitHub errors, and contributes no runs. The endpoint itself fails only on a bad `repo` parameter.
- **Degraded or rate limited:** when the backend's `githubErr()` reports an error (the token was rejected, or API calls are paused until `PausedUntil()`), no call is made. Each repository keeps the runs it had in the previous cached response for the same filter, and every repository gets that error's message as `error`, plus `retry_at` when calls are paused. The page formats `retry_at` in local time ("rate limited until 14:30"), as `errorText` does for banners. A rate-limit or auth error returned by a call itself is reported on its repository the same way. The manager's tick sees the same suspension on its next GitHub call, so the Dashboard's rate figures and degraded state need nothing new.

### 2.3 Cache

- One entry per `repo` filter (empty for all), each with its own lock. This is the `/activity` cache pattern: an entry built less than 15 seconds ago is returned as is, and creating an entry drops expired ones nobody holds.
- `POST /watch`, `DELETE /watch/{name}`, `POST /repos`, `DELETE /repos/{name}` and a config reload drop every entry, so the next request sees the new set of repositories.

## 3. The Actions page

### 3.1 Placement

- Route `/actions`, nav item "Actions" under Operate between Runners and History, with a lucide `Workflow` icon.
- The query polls every 15 seconds while the page is visible, matching the cache.

### 3.2 Layout

- **Header:** the page title, and a summary such as "2 running, 1 queued across 7 repositories · updated 14:05" (`fetched_at` in local time). Statuses `queued`, `waiting`, `pending` and `requested` count as queued; `in_progress` counts as running.
- **Filters, in the URL like History's:**
  - `repo`: a select of All, then the configured repositories, then the watched ones, each watched entry suffixed "(watched)". Choosing one asks the server with `?repo=`.
  - `status`: a segmented control of All, Active, Failed and Succeeded, filtered in the browser. Active is any run that is not completed. Failed and Succeeded are completed runs whose `resultWord(conclusion)` is "Failed" or "Succeeded", so the filter always agrees with the row's icon.
  - Unknown values are dropped, so the page falls back to its defaults.
- **Errors:** one alert above the lists, one line per repository with an error, for example "darkcloud: rate limited until 14:30" (`retry_at` in local time) or "darkcloud: GitHub: not found". The other repositories' runs still show.
- **In progress:** the runs that are not completed. Elapsed time is `now − started_at`, ticking each second like the Dashboard's running bars. The section is hidden when empty, unless the status filter is Active; then it shows "Nothing is running or queued."
- **Recent:** completed runs grouped by local day with History's `groupByDay`, at most 100 rows. Duration is `updated_at − started_at`.
- **A row:**
  - A result icon: `ResultIcon` for completed runs, plus a queued variant (lucide `Clock`, muted) and a running variant (lucide `LoaderCircle`, primary, not spinning, to meet the reduced-motion rules).
  - The title, `display_title` falling back to the workflow name, linking to `html_url` in a new tab.
  - A muted second line: workflow · repository · #run number · branch (mono) · event · actor.
  - On the right: the duration (mono, `dur()`), the start time (`hhmm`, or `monthDay hhmm` before today), and a "ghr" badge when `ghr` is set.
  - The row's accessible name joins the title, repository, run number, a status word (Queued, Running, or `resultWord(conclusion)`) and the duration.
- **Empty states:** "No workflow runs in the covered repositories yet." when there are no runs at all, and "No runs match these filters." with a "Clear filters" button when filters hide everything.
- **Under 768px:** the right-hand column moves below the second line, and the filters wrap.
- A link "Watch more repositories" in the header goes to `/repositories#watched`.

### 3.3 Watched repositories on the Repositories page

- A section "Watched repositories" with `id="watched"` below the configured table. Its description reads "Their workflow runs show on the Actions page. ghr runs no runners for them."
- Each watched name (mono) has a Remove button. There is no confirmation because nothing is lost, and the toast reads "Stopped watching {name}".
- A "Watch a repository" button opens a dialog built on the add-repository picker. It lists `/repos/available` without the configured and watched entries; choosing one posts `/watch` and toasts "Watching {name}". Public repositories are listed without the public warning.
- When nothing is watched, the section shows its description and the button only.

## 4. Testing

### Go

- `config`: `watch_repos` validation (empty, slash, duplicate in any case, overlap with a configured repository) and the YAML round trip.
- `daemon`: `POST /watch` covers not found, conflicts and success; `DELETE /watch` covers 404 and success; AddRepo removes a watched name; `AvailableRepos` sets `watched`.
- `daemon`, actions:
  - the order (not completed first, ties by repository then ID) and the merging of configured and watched repositories;
  - a failing repository keeps its row and the others' runs;
  - `ghr` matching against history, pending and live instances, in any case;
  - the 404 for an uncovered `repo`;
  - the cache TTL, and the drop on watch, add and remove;
  - the degraded and paused paths, which keep the previous runs, make no call, and set each error and `retry_at`;
  - concurrency is at most four.
- `github`: the new `Run` fields decode from a recorded API page.
- `web/fixtures_test.go`: an `actions` fixture generated from `model.Actions`.

### Frontend

- The Actions page: the summary line; both sections; the status and repository filters and their URL round trip; the per-repository error alert; both empty states; the `ghr` badge; row names; the ticking elapsed time; the narrow layout.
- The Repositories page: the watched section, adding through the dialog, and removing with its toast.
- The nav item, and the existing axe and anti-slop sweeps, which pick up the new files.

### Manual, before release

Register row 13's check gains the Actions page at 1280px and 390px in both modes, with one watched repository.

## 5. Delivery

One plan: config and API first (watch list, `Run` fields, `GET /actions`, fixtures), then the Actions page, then the watched section on the Repositories page.

## Out of scope

- Cancel and re-run.
- Expanding a run into its jobs, and telling other self-hosted runners from GitHub-hosted ones. Both need a jobs call per run.
- Repositories of owners other than `owner`.
- A Dashboard panel, notifications and a background collector.
- Workflow and branch filters.
