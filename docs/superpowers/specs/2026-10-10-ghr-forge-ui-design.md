# ghr web UI: the Forge overhaul

**Date:** 2026-10-10
**Status:** draft for owner review. The direction was chosen in chat on 2026-10-10 from ten previews, and the four design sections below were approved one by one the same day.
**Execution strategy:** parallel — after a foundation part, five page groups own separate page files and share nothing at run time.
**Register:** docs/superpowers/registers/2026-10-10-ghr-forge-ui.md
**Mockups:** docs/superpowers/mockups/2026-10-10-ghr-style-directions.html, tab "1 · Forge" (also published at https://claude.ai/artifact/LsS7sjfSXfeZKfENDrcBoo). Where the mockup and this spec differ, this spec wins: the mockup shows a search box, a "Re-run failed jobs" button, "Timestamps" and "Wrap" toggles and eight tabs, none of which ghr gets here.
**Replaces:** §1 (visual system) and §2 (shell) of docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md, and the page layouts in the specs of 2026-10-08 (pages), 2026-10-09 (Actions view, run details) where this spec names a change. Behaviour those specs define stays unless this spec says otherwise.
**Code:** `web/` only.

## Goal

ghr's web UI looks and reads like a part of GitHub: the boxes, status pills, run rows and settings layout an Actions user already knows. The sidebar becomes a top bar with tabs, every page is redrawn in one visual system, and four page groups are restructured toward GitHub's own patterns.

## Decisions

- **Direction: Forge**, chosen by the owner over nine other previews.
- **As close to GitHub as the preview**, chosen over giving ghr its own accent and over keeping IBM Plex. GitHub's blue, green, amber and red, the coral active-tab line, and Mona Sans.
- **Depth: restyle, new shell and page rework**, chosen over a restyle alone.
- **Reworked page groups:** Actions and History; the run and job pages; Repositories and the repository page; Settings, Toolchains and Storage as one settings area. The Dashboard, Runners, the setup wizard and Login keep their structure and are restyled.
- **Colour mode follows the system by default**, chosen over opening light or dark. The dark, light and system control stays.
- **One spec and one plan**, chosen by the owner over three specs.
- **We stay on darkraise-ui** and keep overriding its tokens from `web/src/styles/ghr-theme.css`, as today.
- **No daemon or API change.** Every reworked page uses data it already loads. A gap found during the plan is reported, not filled with a new endpoint.
- **Type sizes and density stay as pinned today** (the kit's `compact` density and `large` font size, the owner's choice of 2026-10-10). Only the families change.
- **Icons stay Lucide.**
- **The repository page's unsaved guard covers leaving the repository only**, the owner's ruling of 2026-10-11 on register row 9, replacing "leaving Capacity and labels by any route": the draft and the save bar already carry across the four sections.
- **Not included,** because ghr has no such behaviour today: search, re-running jobs, log timestamp and wrap toggles.

## 1. Visual system

### 1.1 Colour

| Role | Light | Dark | Used for |
|---|---|---|---|
| ground | `#f6f8fa` | `#0d1117` | page background, box header bands, table header rows |
| surface | `#ffffff` | `#151b23` | boxes, inputs, menus, the top bar in light mode |
| top bar | `#ffffff` | `#010409` | top bar and tab row |
| sunk | `#eaeef2` | `#262c36` | tracks, empty meter cells, count badges, selected list entries |
| line | `#d1d9e0` | `#3d444d` | borders and dividers |
| text | `#1f2328` | `#f0f6fc` | body text |
| muted | `#59636e` | `#9198a1` | secondary text, axis labels |
| accent | `#0969da` | `#4493f8` | links, primary buttons, focus, selection markers |
| on-accent | `#ffffff` | `#0d1117` | text on accent fills |
| active tab | `#fd8c73` | `#f78166` | the 2px line under the current tab; nothing else |
| succeeded | `#1a7f37` | `#3fb950` | succeeded |
| failed | `#cf222e` | `#f85149` | failed, errors, destructive actions |
| running | `#9a6700` | `#d29922` | running, in progress |
| waiting | `#bc4c00` | `#db6d28` | queued and waiting jobs, warnings, deadlines |

- The overrides are written as today: HSL triplets with `!important` under `:root[data-mode="dark"]` and `:root[data-mode="light"]`, one decimal place, computed from these hex values. The plan maps every key the kit's `generateTokens` writes, as spec 1 §1.1 required.
- No component reads a raw colour.
- Shadows, noise, gradient overlays and surface tint stay neutralised, as today.
- **Status colours in charts.** The lanes chart and the finished-jobs tracks draw succeeded, failed, running and cancelled (muted) in the status colours, because there the colour is the state. Every other series (CPU, memory, busy time, the tool-cache bar, size bars) uses the text colour, muted, or the accent mixed toward sunk, never a status colour.
- **The log viewer stays dark in both modes**, with its own tokens, as today.

### 1.2 Type

- **Mona Sans** for the interface and headings, from `@fontsource-variable/mona-sans` (OFL-1.1).
- **JetBrains Mono** for names, IDs, numbers that line up, durations and logs, from `@fontsource/jetbrains-mono`, weights 400 and 600.
- `@fontsource/ibm-plex-sans` and `@fontsource/ibm-plex-mono` are removed.
- Two weights carry the interface: 400 and 600. Tabular numerals wherever digits line up.

### 1.3 Shape

- Radius 6px on boxes, buttons, inputs and menus; fully round only on status pills, count badges and the avatar-sized marks.
- 1px `line` borders separate surfaces. No shadows.
- **Box.** `Section` (`web/src/components/page/section.tsx`) becomes the Forge box: `surface` fill, 1px border, 6px radius, and a header band on `ground` holding the `h2`, the optional `aside`, and a bottom border. Its props and `aria-labelledby` wiring stay.
- `FieldGroup` and `Field` keep their structure and take the new tokens.

### 1.4 Status

- A new shared `StatusPill({ state, label? })` replaces `StateText` on every page: a Lucide icon in the state's colour, a word in the text colour, and the state's colour at low opacity as the fill. The word is not coloured: light-mode Running on its own tint measures about 4.3:1, under the 4.5:1 floor. `StateText` is deleted once nothing uses it.
- One vocabulary and one mapping, in one module:

| State | Word | Colour | Icon |
|---|---|---|---|
| succeeded | Succeeded | succeeded | `Check` |
| failed, error | Failed, Error | failed | `X` |
| running, busy | Running | running | `CircleDot` |
| queued, waiting | Waiting | waiting | `Clock` |
| cancelled | Cancelled | muted | `Ban` |
| skipped | Skipped | muted | `Minus` |
| idle, paused, removing, unknown | their own word | muted | `Minus` |

- Where a row has no room for a pill (run rows, job lists, step rows), the status shows as the icon alone in its colour, with the word as its accessible name. `ResultIcon` and `RunStatusIcon` keep their props and draw from the same mapping.
- A spinner still marks a step or run that is in progress, and stops under reduced motion, as today.
- Every text and status colour in the table of §1.1 meets 4.5:1 on `surface` and on `ground` in its mode; a pill's icon meets 3:1 on its own tinted fill (§7).

## 2. Shell

- The sidebar layout is replaced by a top bar and a tab row in ghr's own markup. The kit's `TopNavLayout` is not used: it always renders a search box and a user menu, renders no badges, and hides its tabs below 768px.
- **Top bar, left to right:** the ghr mark; the host name in mono; a spacer; the capacity segments with their text ("3 of 4 busy" in queue mode, "3 busy" in all mode, from `capacity()`); the colour-mode control; Log out.
- **Tabs:** Dashboard, Runners (badge: live runners), Actions, History, a divider, Repositories (badge: configured count), Settings. The current tab has the 2px active-tab line and weight 600. A tab stays current on its nested pages: Runners on `/runners/$id`, Actions on run and job pages, Repositories on `/repositories/$name/…`, Settings on `/settings/…`.
- **Content:** a centred column, at most 1216px wide, with 24px side padding.
- **Banners,** in this order above the page content: Reconnecting, Degraded, First-run setup, then the runner update. `UpdateCard` leaves the navigation footer and renders as a banner with the same states, wording and actions.
- **Narrow screens:** the tab row scrolls sideways inside itself and the page never does. Below 640px the host name hides and the capacity text hides, leaving the segments with the text as their accessible name.
- **Removed with the sidebar:** the `Operate` and `Configure` group labels, the sidebar capacity panel, the header's user menu (Log out is a button in the top bar), the widened rail and the CSS that hides the collapse toggle.

### 2.1 Colour mode

- A browser with no saved choice opens in the system's mode. `web/public/theme-init.js` treats a missing `mode` as `system`, and `theme.config.ts` sets `mode: "system"`.
- A saved `dark`, `light` or `system` keeps working. `ModeControl` is unchanged apart from its place in the top bar.

### 2.2 Routes

| Route | Shows |
|---|---|
| `/settings` | Settings, General |
| `/settings/token`, `/settings/runner`, `/settings/account` | the other settings sections |
| `/settings/toolchains`, `/settings/storage` | Toolchains and Storage |
| `/toolchains`, `/storage` | redirect to the two above, keeping any hash |
| `/repositories/$name` | repository, Overview |
| `/repositories/$name/settings`, `/labels`, `/registrations` | the other repository sections |

- An unknown section under `/settings/` or `/repositories/$name/` shows the route's not-found line.
- Every other route and its search parameters stay as they are.
- In-app links change with the routes: the Dashboard's "Storage" link, the row menu's "Edit" (to `/repositories/$name/settings`), and toasts or help text that name a page.

## 3. Dashboard and Runners

- **Dashboard.** Same content and order as today, drawn as the Forge preview:
  - the four stats share one box, split by vertical dividers, in place of four cards;
  - Activity, Repositories, Disk and Events are boxes with header bands;
  - Repositories spans the full width, with Disk and Events side by side below it and stacked under 1280px (the owner's ruling of 2026-10-11 on register row 15: beside Disk and Events the table did not fit its box);
  - the lanes chart uses the status colours (§1.1), and the repository table shows `StatusPill`s.
- **Runners and the runner page.** The split view, list, panel, steps, log and stop dialog keep their behaviour. They take the box, the pill, and for the selected list entry the `sunk` fill with a 2px accent marker on its left edge.

## 4. Run lists

### 4.1 Shared run row

A new `RunRow` in `web/src/components/page/` draws one list row for Actions and History:

- a status icon;
- a title in weight 600, as a link;
- under it one line of facts in muted text, wrapping, with no separator characters between facts;
- on the right, two stacked mono values, each with a small Lucide icon (`Calendar` for a time, `Clock` for a duration);
- optional trailing actions.

Rows sit in one box, divided by 1px lines, and highlight on hover. Group headings are rows on `ground` spanning the box.

### 4.2 Actions

- The "In progress" and "Recent" tables become one box. Its header band holds the run count for the current filters ("12 workflow runs") and the two existing filters, `repo` and `status`.
- "In progress" and each day of "Recent" become group headings inside the box.
- **A row:** the status icon; the run title, linking to the run page; then the facts "{workflow} #{run number}", the repository, "{event} by {actor}"; the branch as a mono label in accent text on a low-opacity accent fill; a "ghr" label on `sunk` when ghr ran it. On the right: the start time and the duration.
- Unchanged: the summary sentences, filter rules, URL parameters, the 100-row cap, day grouping, elapsed-time ticking, the error alert, the empty states and the "Watch more repositories" link.
- The markup stays a table, so the row groups keep their `<th scope="rowgroup">` headings.
- **Under 768px:** the right-hand values move under the facts line.

### 4.3 History

- Still the jobs ghr's own runners finished, with the same three controls in the URL and the "Jobs" chart box above the list.
- The seven-column table becomes `RunRow`s in one box whose header band holds the description ("48 jobs, 2 failed, median 3m10s").
- **A row:** the result icon; the job name, linking to `/actions/{repo}/runs/{run_id}` when `run_id` is above 0, and plain text otherwise; the facts workflow, repository, "#{run number}"; on the right the finish time and the duration, with the relative duration bar under the duration; then the two icon buttons "Open run" and "Copy run URL", shown when the entry has a URL.
- Unchanged: bucket picking and its "Showing …" line, day headings, the 500-row note, the empty states and the header summary's rules.

## 5. Run and job pages

- **Frame.** The breadcrumbs, the run title and its description sentence stay. The run's state shows as a `StatusPill` first in the header's actions, ahead of the attempt picker and "Open on GitHub". The kit's page header takes its title and description as plain text, so the pill cannot sit inside the title.
- **Left list.** Summary, the jobs, and Run details stay. The current entry has the `sunk` fill and the accent left marker. Each job entry is a status icon, the name and the duration in mono.
- **Summary** becomes one box holding a row of label-over-value pairs: Triggered via (event, actor, short SHA, pull requests), Status, Total duration, Artifacts. The pairs wrap below 800px.
- **Graph.** The box body is a `ground` canvas. Each job is a `surface` card with a 1px border: status icon, name, and the duration right-aligned in mono; a hovered or focused card takes the accent border. Connectors are 1px `muted` lines. Layout, the timeline fallback and its reason line come from `internal/wfgraph` and the existing component unchanged.
- **Annotations and Artifacts** are boxes with header bands. Their rows, counts, status lines and download flow are unchanged.
- **Job page.** The heading box keeps the job facts. Steps and log become one dark panel:
  - its header holds the job's status icon, name and duration, the "Search logs" box and "Raw log";
  - a running or queued job's step list sits in the same dark panel, with the note "Logs appear when the job finishes.";
  - each step is a row inside the dark panel (chevron, status icon, name, duration in mono) that expands to its lines;
  - an `error` line keeps its tint and gains a 2px `failed` marker on its left edge; `warning` gets the same in `waiting`.
- Unchanged: which step opens first, `step` and `line` links, groups, ANSI handling, the 5,000-line cut, search, the running-job note and every error state of run-details §1.6.

## 6. Configuration pages

### 6.1 Shared section layout

A new `SideLayout` in `web/src/components/page/` gives the GitHub-settings frame used by the settings area and the repository page:

- a left list of links, 240px wide, with optional group labels and dividers; the current link has the `sunk` fill and the accent left marker and `aria-current="page"`;
- the section's content on the right;
- below 800px the list stacks above the content.

`SectionNav` is deleted once Settings and the repository page stop using it.

### 6.2 Settings area

| Left list | Content | Route |
|---|---|---|
| General | today's General, Timing, Disk and retention, and Runner defaults groups as one form, with the `SaveBar` | `/settings` |
| GitHub token | today's token section | `/settings/token` |
| Runner software | today's "Runner and config" section: runner version, update actions, Reload config.yaml | `/settings/runner` |
| Account | Change password | `/settings/account` |
| *Host* (group label) | | |
| Toolchains | today's Toolchains page | `/settings/toolchains` |
| Storage | today's Storage page | `/settings/storage` |

- Each section's page header is its own title, description and actions, as its page has today (Toolchains: Install and Install popular set; Storage: Prune).
- Field wording, help, validation, dialogs, confirmations, toasts and polling are unchanged. Tables and bars take the box and the tokens.
- `UnsavedGuard` covers leaving General by any route, including a click in the left list.
- The setup wizard keeps rendering the config sections and the toolchain list without the left list.

### 6.3 Repositories

- The full-width table becomes rows in one box. The header band holds the summary ("5 configured, 2 running, 1 paused") and **Add repository**.
- **A row, left:** the repository name in weight 600 as a link to its page, with its state as a `StatusPill` beside it. Under it, wrapping: the repository's labels as mono labels on `sunk`, "Warm {n}" in all mode, and "{rate}% succeeded in 7 days" with the existing visually hidden counts. A repository being removed shows "Removing. Running jobs finish first." in this line's place.
- **A row, right:** the runner meter with "2/3", the waiting count when above 0, the 24-hour strip, and the row menu (Pause or Resume, Edit, Remove).
- **Under 768px:** the 24-hour strip hides and the right-hand group moves under the facts.
- "Watched repositories" stays a box below with the same content and `id="watched"`.
- The Dashboard keeps `RepoTable` in its compact form; the `"full"` columns are deleted with their tests.

### 6.4 Repository page

The header stays above the frame: breadcrumb, name, the state sentence, and the actions, which open with the state as a `StatusPill` ahead of Pause or Resume, View history and the More actions menu.

| Left list | Content | Route |
|---|---|---|
| Overview | the "Last 24 hours" chart and the facts list | `/repositories/$name` |
| Capacity and labels | the Capacity, Labels and Cleanup groups, with the `SaveBar` | `/repositories/$name/settings` |
| Workflow labels | the label check | `/repositories/$name/labels` |
| GitHub registrations | the registration list | `/repositories/$name/registrations` |

- `UnsavedGuard` covers leaving the repository page. Moving between its sections keeps the draft and the `SaveBar`, with no guard.
- Unchanged: every field, check, row, button and message, the "being removed" and "daemon unreachable" locked states, and the not-found line.

### 6.5 Setup wizard and Login

Restyled only: the tokens, the box, the pill and the fonts. Steps, fields, wording and flow are unchanged.

## 7. Testing

**Frontend.**
- Every reworked page's tests are updated to the new structure; assertions about behaviour that does not change stay. New shared pieces (`StatusPill`, `RunRow`, `SideLayout`, the top bar, the update banner) get tests sized like their neighbours in `web/src/components/page/`.
- The router test covers the new settings and repository section routes, the two redirects, and the unknown-section case.
- The existing axe checks stay on every page.
- A new contrast test reads the palette from one module and checks, in both modes: text, muted, accent and each status colour on `surface` and on `ground` at 4.5:1; each pill's icon on its tinted fill at 3:1.
- The theme-init test covers a missing saved mode resolving to the system's mode.

**Whole app.** `npm run build`, `npm run lint`, `npm run typecheck` and `npm test` in `web/`, then the Go tests for `./internal/...`, `./cmd/...` and `./web/` (never `./...`, which walks `web/node_modules`), because the Go side embeds the built UI.

**Manual, before release.** On the runner LXC, with the owner's yes for each deploy:
- every page in light and in dark, at 1440px and at 400px wide, with no sideways page scroll;
- a real run in progress on the Dashboard, Actions, its run page and its job page;
- a failed run's job page: the first failed step open, the error line marked;
- General: the unsaved guard on a left-list click; "Capacity and labels": a left-list click keeps the draft with no guard, and leaving the repository opens it;
- `/toolchains` and `/storage` land on the new sections;
- a new browser profile opens in the system's mode.

## Parallel decomposition

A file imported by more than one part belongs to the foundation. The plan settles the full lists from the import graph; the lists below are the known owners.

| Part | Owns | Consumes from the foundation | May run beside |
|---|---|---|---|
| **F. Foundation** | `web/package.json`, `web/package-lock.json`, `web/index.html`, `web/public/theme-init.js`, `web/src/main.tsx`, `web/src/theme.config.ts`, `web/src/styles/*`, `web/src/router.tsx` and its test, `web/src/components/shell.tsx`, `root-layout.tsx`, `brand.tsx`, `capacity-bar.tsx`, `mode-control.tsx`, `update-card.tsx`, all of `web/src/components/page/` (including the new `status-pill`, `run-row`, `side-layout`), `result-icon.tsx`, `run-status-icon.tsx`, `buckets-chart.tsx`, `sparkline.tsx`, `size-bar.tsx`, `window-control.tsx`, `runner-meter.tsx`, `repo-activity-strip.tsx`, `disk-breakdown.tsx`, `step-list.tsx`, `save-bar.tsx`, `unsaved-guard.tsx`, `web/src/lib/status.ts`, `web/src/test/*` | — | nothing |
| **A. Dashboard and Runners** | `pages/dashboard.tsx`, `pages/runners.tsx`, `stat-card.tsx`, `activity-panel.tsx`, `lanes-chart.tsx`, `event-list.tsx`, `repo-table.tsx`, `runner-list.tsx`, `runner-panel.tsx`, `runner-section.tsx`, `stop-runner-dialog.tsx`, `log-view.tsx` | box, pill, tokens | B, C, D, E |
| **B. Run lists** | `pages/actions.tsx`, `pages/history.tsx`, `lib/actions.ts`, `lib/history.ts` | box, `RunRow`, status icons | A, C, D, E |
| **C. Run pages** | `pages/run.tsx`, `run-frame.tsx`, `run-graph.tsx`, `run-timeline.tsx`, `run-annotations.tsx`, `run-artifacts.tsx`, `log-viewer.tsx`, `lib/run-details.ts`, `lib/log-view.ts`, `lib/ansi.tsx` | box, pill, status icons | A, B, D, E |
| **D. Repositories** | `pages/repositories.tsx`, `pages/repository.tsx`, `repo-activity.tsx`, `label-check-group.tsx`, `registrations-group.tsx`, `add-repo-dialog.tsx`, `watch-repo-dialog.tsx`, `repo-picker.tsx`, `lib/repos.ts` | box, pill, `SideLayout`, the repository section routes | A, B, C, E |
| **E. Settings area** | `pages/settings.tsx`, `pages/toolchains.tsx`, `pages/storage.tsx`, `pages/setup.tsx`, `pages/login.tsx`, `settings-form.tsx`, `token-section.tsx`, `account-section.tsx`, `toolchain-actions.tsx`, `toolchain-sections.tsx`, `install-dialog.tsx`, `storage-disk.tsx`, `storage-tables.tsx`, `prune-section.tsx`, `refused-hint.tsx`, `tag-field.tsx` | box, pill, `SideLayout`, the settings routes | A, B, C, D |
| **Z. Closing** | deletions (`state-text`, `section-nav`, the `"full"` table columns, the Plex packages if any import remains), the contrast test's final palette check, the whole-app and manual checks | everything | nothing |

- Each part owns the test files of the files it owns.
- **F settles the shared things first:** the palette, the fonts, the shell, the whole route table with every new section route pointing at the existing page components, and the shared components with their final props. After F the app builds, passes its tests and shows the Forge look on every page with the old page structures.
- **A page part never edits a foundation file.** If it needs a change to one, it stops and reports, and the change is made once on the integration branch.
- **Tests share nothing at run time:** vitest runs on fixtures in jsdom, with no ports, databases or fixed paths. Each worktree runs `npm ci` in its own `web/`.
- No part carries security, data-loss, migration or concurrency risk.

## Out of scope

- Daemon, API and CLI changes.
- Search, re-running jobs, log timestamp and wrap toggles.
- An upstream palette hook in darkraise-ui; ghr keeps overriding tokens from its own sheet.
- New pages, and any change to what the Dashboard or Runners show.
