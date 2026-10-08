# ghr web UI redesign: the other pages

**Date:** 2026-10-08
**Status:** approved section by section in chat on 2026-10-08; awaiting the owner's review of this file.
**Register:** docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md (the rest of item 2, and items 10, 11, 12, 14, 15, 16)
**Builds on:** docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md (spec 1). Its visual system (§1), anti-slop rules (§1.7) and shell (§2) bind every page here; this spec does not repeat them.
**Code:** `web/`, `internal/activity`, `internal/history`, `internal/api`, `internal/daemon`, `internal/model`, `internal/github`, `internal/system`, `internal/runner`, `cmd/ghr`

## Goal

Spec 1 redesigned the shell and the Dashboard. The other eight pages (Runners, the runner page, History, Repositories, the repository page, Toolchains, Storage and Settings) still use the old card stacks, pills, glyphs and dot chains. This spec rebuilds them in the Control Room system, makes each layout easier to use, and draws pictures of real data where a page has some to show.

This is the second of three specs. The cross-repo Actions page is spec 3.

## Decisions

- **Small API additions are allowed** (§2), chosen by the owner over a frontend-only redesign. All are optional fields or parameters, so the CLI and older callers keep working.
- **The page set stays.** All eight pages and spec 1's navigation stay; no page merges.
- **The runner page joins the Runners page** as a split view, chosen over lanes above a table (§3).
- **Settings becomes one scrolling page of sections with a side index**, chosen over tabs and over restyled cards (§7).
- **History gets a bucket chart above its table**, chosen over a tidied table or a summary line (§4).
- **Repositories becomes a full-width table**, the Dashboard's table with more columns, chosen over redesigned cards (§5).
- **A shared page kit comes first** (§1), and the pages are rebuilt on it, chosen over rebuilding each page with its own markup.
- **Delivery is two plans:** the backend additions, then the frontend.

## 1. Shared page kit and page-wide rules

### 1.1 Components

The kit lives in `web/src/components/page/`. Redesigned pages lay out with it and stop using the kit's `Card` for page layout.

- **`Section`**: the Dashboard's bordered panel, moved out of `dashboard.tsx` unchanged in look. It has a 10px radius, a 1px `line` border, an `h2` heading and an optional `aside`. It labels itself with `aria-labelledby` pointing at its heading, not `aria-label`. The Dashboard uses it from the new file.
- **`FormSection`**: a heading with an `id` (the `SectionNav` anchor), an optional one-line note, then its rows separated by 1px `line` dividers. It has no card around it.
- **`Field`**: one form row, used by Settings, the repository page and the setup wizard's settings step.
  - The label sits in a 12rem left column at 640px and wider, and above the control below 640px.
  - Under the control come one help line in `muted`, then an error line in `bad` with a Lucide `CircleAlert` icon.
  - A changed field shows the word "Changed" in `warn` beside the control, replacing the amber dot.
  - It replaces the private `Row` in `repository.tsx` and `Field` in `settings-form.tsx`.
- **`SplitView`**: a list column 22rem wide and a panel filling the rest, at 1024px and wider. Below 1024px it renders only what the page passes for the narrow layout.
- **`StateText`**: a state as a coloured word, never a pill.
  - Accent: busy, running, active, online, matched.
  - `warn`: waiting, starting, queued, expires soon, unverified, refused, interrupted.
  - `bad`: error, failed, failure, offline, unmatched, rejected.
  - `ok`: ok, success, valid, up to date.
  - `muted`: paused, removing, idle, cancelled, and anything else, matching the Dashboard's repository states.
  - The word is shown in sentence case. It replaces `StateBadge`, and `stateVariant` and the `BadgeVariant` import leave `lib/status.ts`.
- **`ResultIcon`** (spec 1) is the only way a job or step outcome is drawn. `Glyph` and `components/glyph.tsx` are deleted.
- **`SectionNav`**: a sticky index of section links on the right, at 1280px and wider. It uses plain hash links, and marks the section nearest the top of the viewport with `aria-current="true"` from a scroll listener. It needs no router change.
- **`SaveBar`** (restyled): it sticks to the bottom on `surface` with a 1px `line` border and no shadow. It reads "2 unsaved changes" in `warn`, with **Discard** (outline) and **Save changes**. Settings and the repository page share it.
- **`RejectedAlert`** (restyled): a Lucide icon in place of each "✖", and "and 3 more" in place of "… and 3 more".

### 1.2 Rules for every page

- **Header.** `PageHeader` with a one-sentence `description` that sums up live state, like the Dashboard's. Actions sit on the right: the primary one filled, the rest outline.
- **Loading** uses the kit's `Spinner` with a sentence-case label ("Loading", "Waiting for the daemon"), with no ellipsis character.
- **Errors** read `errorText(err)` in `bad` with a Lucide icon, plus a **Try again** button where a retry makes sense. No "✖" or "⚠" glyphs.
- **Empty states** are one muted sentence and, where one exists, the button that fixes it.
- **Copy.** No "–" placeholders in cells (empty cells stay blank), no "·" chains, and no em or en dashes. Button labels lose their "+" and "…" ("Add repository", "Install"). A busy button uses the kit's `loading` state instead of changing its label to "Adding…" or "Queuing…".
- **`errorText` (item 12)** in `src/query.ts` reads "{message}. Retry after 14:05." with no dash.
- **Badges.** No kit `Badge` around plain text on any page or dialog. `TagField`'s removable chips stay: they are controls, not labels.
- **Tables.** Mono for IDs, numbers, times, sizes and versions. Numbers right-aligned. A row with more than one action gets the Dashboard's single **More actions** menu. A table scrolls inside its `Section` and never the page.
- **Confirmations.** Each `ConfirmDialog` gets a short question as its title, naming the thing ("Remove python 3.13?"), and the consequence in `body` ("Frees 210 MB. Jobs that need it install it again."). Today's one-sentence titles are split this way, and the action button repeats the verb.
- **Dialogs** (Add repository, Install, Replace token) follow the same copy and badge rules. "public" and "installed" become muted words, and the public-repository warning gets a `TriangleAlert` icon in `warn`.

## 2. Backend additions

All five are optional, and each regenerates its frontend fixtures in the same commit.

1. **`GET /activity?repo=<name>`.**
   - When set, `activity.Build` keeps only that repository's history entries and live instances, and `repos` holds only that repository.
   - Lanes are not padded to capacity. `capacity` is `null`, so each bucket's `busy_pct` is `null` and `busy_minutes` counts that repository's runs only.
   - Each bucket's `waiting_max` and `cpu_avg` are `null`, and `waiting` and `cpu` are empty: host load does not belong to one repository.
   - A name not in the config returns 400 "unknown repository {name}".
   - `Backend.Activity` takes the repo and adds it to its cache key.
2. **7-day counts.** Each `repos[]` entry on `/activity` gains `week: {succeeded, failed, cancelled}` over the rolling 7 days ending now, whatever the window. Conclusions map as the buckets map them today (`finishedState`), so anything that is not success, cancelled or skipped counts as failed; item 5 still owns that rule, and skipped is counted in none of the three.
3. **`GET /history?since=<RFC 3339 time>`.**
   - It returns entries that finished at or after `since`. An unparsable value returns 400 "since must be an RFC 3339 time".
   - `history.Store.Query`, `Backend.History` and `api.Client.History` gain a `since time.Time` argument, where the zero time means no bound.
   - `ghr history` in `cmd/ghr/cli.go` passes the zero time, so its output is unchanged.
4. **Step times.**
   - `github.Step` decodes `started_at` and `completed_at`.
   - `model.Step` gains `StartedAt` and `CompletedAt` as `*time.Time` with `omitempty`, and `fetchSteps` copies them.
   - A pending step has neither; a running step has only `started_at`.
5. **Docker root path (item 11).**
   - `system.DiskUsage` gains `Root string`, set by `DataRootBytes` from the `docker info` output it already reads.
   - `model.Status` gains `disk_root`, which stays empty until the first measurement.
   - The Dashboard's Disk heading and the Storage page show it in muted mono.

The TypeScript types in `web/src/api/types.ts` mirror each field. `api.activity` and `api.history` take the new parameters, and the query keys include them.

## 3. Runners and the runner page

**One page, two routes.**
- `/runners` and `/runners/$id` both render `RunnersPage`, and `$id` is the selection.
- `/runners/$id` keeps its `tab` search parameter (`steps`, `log`, `containers`), so links from the Dashboard's lanes work unchanged.
- `pages/runner-detail.tsx` becomes `components/runner-panel.tsx`, `components/runners-table.tsx` becomes `components/runner-list.tsx`, and `StopRunnerDialog` moves to `components/stop-runner-dialog.tsx`.

**At 1024px and wider** the page is a `SplitView`.
- **The list.** A row per live runner holds:
  - a state light: accent for busy, pulsing per spec 1 §1.4; an accent outline for idle; accent at 45% for starting; `muted` for cleaning;
  - the runner ID in mono;
  - the repository;
  - the job name with its run number in mono;
  - the elapsed time in mono.
- **Waiting.** Under the runners, a **Waiting** group lists each unpaused repository at its cap with jobs queued: "darkrouter, 2 queued, cap 2". It uses the same rule as today's waiting rows.
- **Selection.** Arrow Up and Arrow Down move the selection as they do today, and each row is also a link to `/runners/$id` that keeps the current tab. With no `$id`, the first live runner shows in the panel without changing the URL. The selected row is marked with `aria-current="true"` and a `sunk` fill.
- **The panel.**
  - **Header:** the runner ID, its `StateText`, and a fact row with labels: Repository, Job, Running for, Started. It does not use "·".
  - **Actions:** **Open run** and **Stop runner** stay visible when they apply. **Copy run URL** and **Copy runner ID** sit in a **More actions** menu. The Logs, Copy ID and Stop row buttons leave the list.
  - **Tabs:** Steps, Log, Containers.

**Below 1024px** `/runners` shows only the list, and `/runners/$id` shows only the panel, with a "Runners" back link above its header.

**Steps (graphical).**
- One row per step: `ResultIcon` (or the kit's `Spinner` while in progress), the name, the duration in mono, and a thin bar on a shared time axis.
- The axis runs from the first step's start to the last step's end, or to now while the job runs. A running step's bar grows to now.
- Pending steps are `muted` and have no bar or duration.
- A pure helper `lib/steps.ts` turns steps and `now` into bar offsets and widths as fractions of the axis.

**Log.**
- The log fills the panel's remaining height.
- The Follow switch sits in the tab's header row, and the scroll-away pause stays.
- The text is mono on `sunk` with a 6px radius.
- "No log output yet" shows as a muted line outside the `<pre>`.

**Containers.** A table with the state as `StateText` and the name and image in mono. When empty: "No containers in this runner's compose projects."

**Finished runners.**
- They keep today's behaviour: the panel holds the last snapshot it saw for that ID, steps and containers stop polling, and a muted line reads "This runner has finished". The warning `Alert` and its back button go.
- A runner never seen live (opened from a Dashboard lane) shows its ID and the word "Finished", and the Log tab still fetches its log.
- **Stop runner** is disabled for a finished runner and while the daemon is unreachable.

**Header and empty state.**
- **Header:** "Runners", described as "3 live, 2 jobs waiting". The counts leave out cleaning runners, matching `running()` in `lib/status.ts`.
- **No runners and no waiting jobs:** "No runners. They start when a job is queued." with a link to Repositories.

## 4. History

**Controls.**
- They sit in one row under the header, and wrap below 640px.
- All three live in the URL through the route's `validateSearch`: `/history?repo=&result=&window=`.
  - `repo`: a select of "All repositories" then each configured name. A name in the URL that is no longer configured stays selectable, as today.
  - `result`: a segmented control of All, Succeeded, Failed and Cancelled, mapped to `success`, `failure` and `cancelled`.
  - `window`: `WindowControl` limited to `24h`, `7d` and `30d`, default `7d`. `WindowControl` gains an `options` prop for this.
- An unknown value in the URL falls back to its default.

**Chart.**
- A `Section` titled "Jobs" holds `BucketsChart` for `/activity?window=…&repo=…`.
- `BucketsChart` gains `tracks: "all" | "jobs"`. `"all"` is the Dashboard's four tracks. `"jobs"` draws only the busy and finished-jobs tracks, at the same track geometry. The Dashboard passes `"all"`.
- The chart always shows every outcome, stacked, and its legend says "All results". The result filter applies to the table only.
- **Picking a bucket.** Clicking a bucket, or pressing Enter on the focused one, narrows the table to that bucket's start and end. A line above the table reads "Showing 14:00 to 15:00, Oct 8", with a **Show whole window** button. Changing any control clears the pick. The narrowing filters the rows already loaded, in the browser.

**Table.**
- Rows come from `/history?since=<window start>&repo=&conclusion=&limit=500`. `useHistory` takes `since`, and its key includes it.
- Rows are grouped under day headings in the browser's zone: "Today", "Yesterday", then "Mon, Oct 6".
- **Columns:**
  - Finished: a mono time;
  - Repository;
  - Job: the job name, with the workflow under it in `muted`;
  - Run: a mono `#n`;
  - Result: `ResultIcon` and its word;
  - Duration: mono, with a relative bar on `sunk` in accent against the longest row in view;
  - two icon buttons with tooltips: **Open run** (`ExternalLink`) and **Copy run URL** (`Copy`), shown when the entry has a URL.
- When 500 rows come back, a muted line reads "Showing the newest 500 jobs in this window."
- **Empty:** "No finished jobs in the last 7 days" (naming the window). With a repo or result filter set: "No jobs match these filters" with **Clear filters**.

**Header.** The description sums up the rows in view after every filter: "48 jobs, 2 failed, median 3m 10s". The pure helpers in `lib/history.ts` group rows by day, compute the median, and filter to a picked bucket.

## 5. Repositories and the repository page

### 5.1 Repositories

- **Table.** `RepoTable` gains `columns: "compact" | "full"`. The Dashboard passes `"compact"` and keeps today's columns. `"full"` adds:
  - **Warm**: shown in all mode only, from `/config`;
  - **7 days**: the success rate, `succeeded / (succeeded + failed)`, from `week`, as a whole percent in mono. It is blank when both are 0, and its tooltip reads "46 succeeded, 2 failed, 1 cancelled";
  - **Labels**: the repository's own labels from `/config`, in `muted` mono, truncated, with a tooltip holding all of them.
- **Data.** The page polls `/activity?window=24h` for the 24-hour strip and the week counts.
- **Rows.** The row menu (Pause or Resume, Edit, Remove) and click-to-open stay as on the Dashboard. A repository being removed shows "Removing. Running jobs finish first." under its name in `muted`.
- **Header.** "Repositories", described as "5 configured, 2 running, 1 paused". The action is **Add repository**.
- **Below 768px** the Warm, 7 days and Labels columns hide.
- **Empty:** "No repositories yet" with **Add repository**.
- **Deleted (item 10):** `components/repo-summary.tsx`, `lib/activity.ts` and its test, `useRepoActivity` with its `repoActivity` query key and its test in `api/hooks-pages.test.tsx`, and `ACTIVITY_LIMIT`.

### 5.2 The repository page

- **Header.**
  - The breadcrumb goes back to Repositories, then the name.
  - The description is the state as a sentence: "Running 2 of 3 runners, 1 job waiting", "Idle", "Paused", or the repository's error.
  - Actions: **Pause** or **Resume** (outline), **View history** (a link to `/history?repo=<name>`), and a **More actions** menu holding **Remove**. All run through `useRepoActions`.
- **Activity.** A `Section` titled "Last 24 hours" holds `BucketsChart` in `"jobs"` mode for `/activity?window=24h&repo=<name>`. Beside it at 1024px and wider, and under it below, is a facts list:
  - Runners: the repository's segment meter and "2 of 3";
  - Waiting;
  - Last 7 days: "46 succeeded, 2 failed, 96%";
  - Last job: `ResultIcon`, the job name with its run number, and its age.
- **Settings.** Below the activity come `FormSection`s, with a `SectionNav` at 1280px and wider:
  - **Capacity:** Max and Warm. The defaults move from placeholders to the help line. Max: "Leave empty for the default: 1 in queue mode, no limit in all mode. Once set, it stays explicit." Warm: "Runners kept ready in all mode. Leave empty for the default of 1." The blank and warm-above-max errors stay as today.
  - **Labels:** the repository labels field, then "Runners get". This shows the system labels, the global labels marked "global", and the repository labels, as plain mono words, with the existing note that running runners keep their labels.
  - **Cleanup:** the prefixes field.
  - **Workflow labels:** today's `LabelCheckCard`, rebuilt as a `FormSection`. Each workflow group is a row with its `StateText`, job count, "last seen 2 h ago", and the missing labels, each with **Add label**. The OS and architecture hint gets a `TriangleAlert` icon. The header line reads "Checked 5 min ago, partial" without "·".
  - **GitHub registrations:** today's `RegistrationsCard`, rebuilt as a `FormSection`. Each registration's status is `StateText`, and its labels are plain mono words.
- **Locked states.**
  - While the repository is being removed, a muted line under the header reads "Being removed. Running jobs finish first, and settings are read-only." The form is disabled.
  - While the daemon is unreachable, the form is disabled, and the shell's banner says why.
- **Not found:** "Repository not found. It may have been removed." with a link to Repositories.
- **Save.** The `SaveBar` and `UnsavedGuard` behave as today.

## 6. Toolchains and Storage

### 6.1 Toolchains

- **Header.** "Toolchains", described as "9 installed, 4.2 GB". Actions: **Install** (filled) and **Install popular set** (outline). Both keep today's dialogs and confirmation.
- **Size.**
  - A `Section` titled "Tool cache" holds one horizontal stacked bar of the cache by tool, with a legend listing each tool and its total in mono.
  - Each bar part has a `role="img"` label.
  - The four largest tools take `--chart-1` to `--chart-4`, and the rest plus folders installed by jobs share `--chart-5` as "Other". The bar uses no status colours.
  - A pure helper in `lib/toolchains.ts` builds the parts.
- **Installed.**
  - A `Section` holds a table grouped by tool, with the tool as a group heading.
  - Each row: the version in mono, the arch in `muted` mono, the size in mono with a relative bar against the largest row, the install date in mono, and **Remove** as an icon button (`Trash2`) with a tooltip naming the version.
  - The `.NET` last-major warning stays in the confirmation body.
- **Installed by jobs.** Folders from `other_tool_cache` follow under the heading "Installed by jobs", each with its size. A muted note reads "A job's own setup step installed these. ghr does not manage them." They have no actions.
- **In progress.**
  - While an operation runs or is queued, a line above the table shows a `Spinner` and "Installing node 24, 40%, 2 more queued".
  - `queueText` drops its em dash and returns this form: the verb and target, then the progress after a comma when present, then ", N more queued" when N is above 0.
- **Refused.** `RefusedHint` becomes a muted line with a `TriangleAlert` icon in `warn`.
- **Empty:** "The tool cache is empty" with **Install popular set**.
- **Setup.** The setup wizard's toolchain step renders the same components, without the page header.

### 6.2 Storage

- **Header.**
  - "Storage", described as "Docker uses 62% of /var/lib/docker and prunes above 80%". The path is `disk_root`; until it is known, the description leaves out "of …".
  - The action is **Prune** (filled), which runs the standard prune after its confirmation. It is disabled while maintenance runs or the daemon is unreachable.
- **Layout.** Below 1280px the sections stack in this order. At 1280px and wider they form a 3:2 grid: Disk and Package caches on the left, Prune and Recent operations on the right.
- **Disk.**
  - The Dashboard's `DiskBreakdown`, unchanged, with the high-water marker.
  - Under it is the Docker table: Type, Count, Active, Size, Reclaimable, with numbers in mono right-aligned. The build cache types are indented `muted` rows under Build Cache.
  - The last prune is a facts list: "Last prune: manual standard at 14:02, ok", then one line per step with what it freed, and failed steps in `bad`. "Pruning" shows with a `Spinner` while one runs, and "No prune since the daemon started" when there is none.
- **Prune.** A `Section` with one row per targeted prune:
  - Each row has a sentence saying what goes, how much it can free where Docker reports it, and a button.
  - The rows: "Build cache beyond 20GB" with **Trim build cache**; "All build cache, up to 6.1 GB" with **Remove build cache**; "Dangling images" with **Remove dangling images**; "Unused volumes, up to 1.2 GB" with **Remove unused volumes**.
  - The two that cannot be undone cheaply (all build cache and unused volumes) use destructive outline buttons. Each keeps today's confirmation, split into a title and a body.
  - The volume refusal hint stays under its row.
- **Package caches.**
  - A table: Cache, Paths (`muted` mono, truncated, with a tooltip), Size (mono with a relative bar), Files (mono), Last written ("2 h ago", or "Never written"), and **Clear** as an icon button (`Eraser`) with a tooltip naming the cache.
  - Caches that are not present leave the table for one muted line: "Not present: Gradle, Maven".
  - The `Section`'s aside reads "Measured 14:02" (or a `Spinner` with "Measuring"), with **Refresh** as an outline button.
  - A measurement error shows in `bad` under the table.
- **Recent operations.** A table: the time in mono, the kind, the target, the outcome as `StateText`, and the message in `muted`. When empty: "No operations since the daemon started."

## 7. Settings

**Layout.**
- One scrolling page of `FormSection`s with a `SectionNav` at 1280px and wider.
- The `SaveBar` counts only config fields. The GitHub token, Runner and config, and Account sections act at once with their own buttons, and each says so in its note.

**Config sections** (today's `SettingsSections`, rebuilt on `Field`):
- **General:** Mode (select, with the mode's meaning as help), Global max, Owner (read-only mono, help "Change it in config.yaml and restart the daemon").
- **Timing:** Poll interval, Start timeout, Idle timeout.
- **Disk and retention:** Disk high-water, Build cache keep, History retention.
- **Runner defaults:** Global labels, Memory max, CPU quota, with the note "Limits apply to newly started runners."
- Field help text keeps today's wording, in sentence case. Example values move from placeholders to the help line ("For example 20GB").

**GitHub token:**
- Status: `StateText` ("Valid", "Expires soon", or the state), then "Expires Oct 30, in 22 days" or "Expiry unknown".
- Checked: "2 min ago".
- Rate limit: "4,812 of 5,000, resets 14:30" in mono. It is blank until known, never "–".
- The reason, in `bad`, when the token is not valid.
- **Replace token**, with today's dialog and confirmation. The section note keeps today's sentence about what replacing checks.

**Runner and config** (today's Maintenance card):
- Runner version, using `StateText` instead of badges: "2.328.0, up to date, checked 3 h ago"; "2.328.0, 2.329.0 required by Oct 14, in 6 days"; "Queued: runs when no job is running or queued"; or "Updating".
- The deadline word turns `bad` within 7 days, as the shell's update card does.
- **Queue update** or **Cancel update** when they apply. The last check error stays as a muted line.
- **Reload config.yaml**, with the help "Reads config.yaml again. Warnings appear under Events on the Dashboard." Its toast says "Config reloaded, 1 warning. See Events on the Dashboard." Today's copy points at an "Activity" page that no longer exists.

**Account:**
- Current, new and confirm password as `Field`s, then **Change password**, with today's checks and messages in sentence case.
- **Log out** leaves this section, because the shell's footer already has it.

**Setup.** The setup wizard's settings step renders the same config sections without `SectionNav`, and keeps its own step layout.

## 8. Testing

### Go

- `internal/activity`: table rows for the repo filter (another repo's runs and instances left out, no lane padding, null host figures, empty `waiting` and `cpu`), and for week counts on both sides of the 7-day edge and for each conclusion.
- `internal/history`: rows for `since` at, just before and just after the boundary, combined with `repo`, `conclusion` and `limit`.
- `internal/api`: the `repo` and `since` parameters reach the backend; an unknown repo and an unparsable `since` return 400.
- `internal/daemon`: the activity cache key separates repos.
- `internal/github` and `internal/daemon`: step times decode, and are copied when present and absent.
- `internal/system`: the root path is returned from the `docker info` output.
- `cmd/ghr`: `ghr history` still sends no `since`.
- CI runs `go test -race` as before.

### Frontend

- Each page's existing test is updated to the new structure and copy, and keeps covering today's behaviour: Stop with its finished and busy wording, follow and scroll pause, save and discard, the unsaved guard, refused hints, and the confirmation flows.
- New unit tests cover the pure helpers: `lib/steps.ts` (axis, running and pending steps), `lib/history.ts` (day grouping in a fixed zone, median, bucket pick), the tool cache parts in `lib/toolchains.ts`, `queueText`, the repository success rate, and the `StateText` mapping.
- Component tests cover `SectionNav`'s current section, `SplitView` at both widths through `useWidth`, the runner list's arrow keys and links, and the History controls round-tripping through the URL.
- `a11y.test.tsx` adds each redesigned page to its axe run.
- **Anti-slop (item 16).**
  - `anti-slop.test.ts` adds each redesigned file as its page lands, and also rejects Tailwind gradient utilities (`bg-linear-*`, `bg-gradient-to-*`, `bg-radial-*`, `bg-conic-*`).
  - When the last page lands, the file list becomes a walk of every `.ts`, `.tsx` and `.css` file under `web/src` plus `public/theme-init.js`, leaving out test files.
  - The same task deletes `components/stepper.tsx` (item 15) and checks that nothing imports `Glyph`, `StateBadge` or the kit's `Badge` outside `TagField`.
- **Flaky waits (item 14).** `web/src/test/setup.ts` sets Testing Library's `asyncUtilTimeout` to 3000 ms, so `findBy` queries survive a loaded machine.
- `npm test`, `npm run lint` and `npm run typecheck` pass after every task.

### Manual, before release

In Chrome at 1280px and 390px, in dark and light mode, check every page here:
- the Runners split and narrow views with a busy runner;
- History's bucket pick;
- a repository page with a label check;
- a toolchain install in progress;
- a prune;
- a settings save.

Also confirm there are no CSP errors in the console. This runs with register item 13 on the runner LXC.

## 9. Delivery

On a branch `feat/web-ui-pages`, as two plans:

1. **Backend:** §2, with fixtures regenerated in the same commits.
2. **Frontend:**
   - the kit and page-wide rules (§1) first, including the Dashboard's move to the shared `Section` and the `errorText` copy;
   - then Runners, History, Repositories with the repository page, Toolchains, Storage and Settings;
   - then the anti-slop directory walk and the deletions.

Every task leaves the suite green. Pushing master cuts a release through CI, so the branch merges only when the owner says so.

## Out of scope

- The cross-repo Actions page (spec 3).
- Redesigning login and setup beyond the shared components they render (spec 1 restyled both).
- Dashboard changes beyond moving to the shared `Section`, the `"all"` tracks prop, and the Docker root path.
- Register items 4, 5, 17, 18 (apart from `Section`'s `aria-labelledby`) and 19.
- An upstream darkraise-ui custom-palette feature.
- Server-sent events. Polling stays.
