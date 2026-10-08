# ghr web UI redesign: identity, shell and Dashboard

**Date:** 2026-10-08
**Status:** draft for owner review. The visual direction was chosen in chat on 2026-10-08 from the mockups below.
**Register:** docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md (item 1 in full, item 2 for the Dashboard only)
**Mockups:** docs/superpowers/mockups/2026-10-08-ghr-web-ui-directions.html, tab "A, graphical" (also published at https://claude.ai/artifact/P1iocQEbpwNjEejkVcPsny)
**Builds on:** docs/superpowers/specs/2026-10-05-ghr-web-ui-design.md
**Code:** `web/`, `internal/metrics`, `internal/history`, `internal/daemon`, `internal/api`, `internal/model`, `internal/system`, `internal/runner`

## Goal

The web UI stops looking like the untouched darkraise-ui template. It gets its own identity, a shell built around the runner capacity, and a Dashboard that draws what ghr is doing as pictures of real data. This spec is the first of three:

1. **This spec:** the visual system, the app shell, and the Dashboard, plus the backend the Dashboard needs.
2. **Next:** every other page redesigned in this system (register item 2).
3. **Then:** the cross-repo Actions page (register item 3).

## Decisions

- **Direction: "Control Room, graphical".** Dark first, dense, IBM Plex type, one cyan signal colour. The owner picked it over a light "Precise" direction and an industrial "Workshop" direction after seeing all three.
- **The anti-slop rules are binding** (§1.7). They come from Impeccable (pbakaus/impeccable), taste-skill (Leonxlnx/taste-skill) and the VectorLab anti-slop skill. The owner's explicit choices win over them: the Dashboard's first row is stat cards at the owner's request, although the rules list big-number tiles as a common AI pattern.
- **We stay on darkraise-ui and restyle it.** ghr's palette overrides the theme engine's colour tokens from a ghr stylesheet. The engine writes those tokens as inline styles on `<html>` (`applyTokens` in the theme provider), so each override carries `!important`. Presets can't be registered from outside the kit (`presets` is a fixed map), so a ghr preset is not an option. An upstream "custom palette" escape hatch in darkraise-ui was rejected for now as a cross-repo release; it is a follow-up.
- **The theme switcher goes.** ghr has one look. The only theme choice left is light, dark or system, and the default is dark.
- **Icons stay Lucide.** darkraise-ui's own components draw Lucide icons, so a second library would mix stroke styles on one screen. Every icon uses one stroke width (§1.5). The taste-skill rule against Lucide loses to the rule that icons come from one consistent set.
- **Charts are hand-built SVG components**, not recharts. The lane view needs exact geometry that recharts doesn't model, and the other charts are small.
- **The daemon computes activity**, not the browser. A new `GET /activity` packs jobs into lanes and builds time buckets (§4.2), because 30 days of history is too much to ship to the browser on every poll.
- **Metrics gain rollups that survive restarts** (§4.1). Today ghr keeps 60 one-minute samples in memory only.

## 1. Visual system

### 1.1 Colour

Colour lives in the shell: a dark rail in both modes, a tinted ground, and one accent. Status colours appear only where they carry meaning, and always next to a word or an icon.

| Role | Dark | Light | Used for |
|---|---|---|---|
| ground | `#0d1218` | `#eef1f4` | page background |
| surface | `#121a23` | `#ffffff` | cards, the activity panel, inputs |
| sunk | `#19222d` | `#e5e9ee` | tracks, empty cells, meters |
| line | `#222d3a` | `#d4dae2` | borders, row dividers, grid lines |
| text | `#d9e0e8` | `#121a24` | body text |
| muted | `#808c9c` | `#586577` | secondary text, axis labels |
| accent | `#4cc3d6` | `#0a7f93` | primary actions, selection, focus, "running", CPU series |
| on-accent | `#062027` | `#ffffff` | text on accent fills |
| ok | `#4fb36a` | `#2f8a45` | succeeded |
| bad | `#ef6b5e` | `#c63a2f` | failed, errors |
| warn | `#e0a644` | `#a26700` | waiting jobs, deadlines, warnings |
| shell | `#090d12` | `#131a22` | sidebar background |
| shell-hi | `#17202b` | `#232d3a` | active nav item, capacity panel |
| shell-text | `#c9d2dd` | `#d5dce5` | sidebar text |
| shell-muted | `#6f7c8d` | `#8794a5` | sidebar secondary text |

- These map onto the engine's tokens (`--background`, `--card`, `--primary`, `--border`, `--muted-foreground`, the sidebar set, `--success`, `--warning`, `--destructive`, `--chart-1..5` and the rest). The plan lists every engine token and its ghr value. No component reads a raw hex value.
- `--chart-1..5` are redefined as tints of the accent and of muted (§3.5), never green, amber or red, so a chart series never looks like a status.
- Every text pair meets WCAG AA: 4.5:1 for body text, 3:1 for large text and controls. The plan records the measured ratios.

### 1.2 Type

- **IBM Plex Sans** for all text, and **IBM Plex Mono** for numbers, times, IDs, versions and code only. They come from `@fontsource/ibm-plex-sans` and `@fontsource/ibm-plex-mono`, so no font loads from outside (the CSP forbids it). Inter and JetBrains Mono are removed.
- Scale: page title 21px/600, section title 15px/600, body 13.5px/400, small 12.5px, axis 10.5px mono, stat number 26px mono/500.
- Tabular numerals everywhere digits line up. Sentence case everywhere. No uppercase letter-spaced labels.

### 1.3 Shape and depth

- Radius: 6px on controls (buttons, inputs, nav items, segmented controls) and 10px on surfaces (cards, the activity panel, dialogs). Nothing is pill-shaped except a switch.
- Borders are the only elevation: 1px `line`. No shadows on cards, and no glow, glass, gradients on surfaces, or coloured side borders.
- Density: the engine's `compact` axis.

### 1.4 Motion

There is one authored motion: a running job's leading edge, and a busy runner's light, pulse on a 1.8s ease-out loop. Anything else animates only `opacity` or `transform`, in 150ms or less. Under `prefers-reduced-motion` the pulse stops and transitions are instant.

### 1.5 Icons

Lucide, 16px in the sidebar and 15px inline, stroke width 1.75 everywhere. Unicode glyphs (✔ ✖ ⧗ ⊘) stop standing in for icons; `Glyph` is replaced by Lucide icons with an accessible label.

### 1.6 Browser surfaces

Text selection, focus rings (2px accent, 2px offset), scrollbars and the caret take their colours from the palette.

### 1.7 Anti-slop rules

Every redesigned component follows these:

- One accent colour. No purple and no gradients on buttons or text.
- No nested cards. Group by spacing and headings first; a card marks only an element that needs to stand apart.
- Status dots only for live state (runner slots, the capacity bar). Elsewhere state is a word, and outcomes get an icon.
- No pills around plain text such as state names, counts or errors. Small badges only for counts on navigation.
- No "·" chains as separators, and no dash characters in empty cells; empty cells stay blank.
- No em dashes or en dashes in UI copy. Buttons name their action, and errors say what happened and what to do.
- Monospace only for data. No decorative SVG, background patterns or illustrations.

## 2. App shell

```
┌────────────┬──────────────────────────────────────────┐
│ ▬ ghr lxc-99│ banners (reconnecting / degraded / setup)│
│ Runners    │                                          │
│ ▮▮▯░ 2 of 4│ page                                     │
│ Operate    │                                          │
│  Dashboard │                                          │
│  Runners 3 │                                          │
│  History   │                                          │
│ Configure  │                                          │
│  Repos   5 │                                          │
│  Toolchains│                                          │
│  Storage   │                                          │
│  Settings  │                                          │
│ ┌update──┐ │                                          │
│ └────────┘ │                                          │
│ Owner  ◐ ⏻ │                                          │
└────────────┴──────────────────────────────────────────┘
```

- **`SidebarLayout`** keeps its structure. ghr passes `showThemeSwitcher={false}`, `activeBar="ring"` (no 3px left bar) and `notificationSlot={null}`, and restyles the active item to a `shell-hi` fill with 600 weight.
- **Brand.** The logo mark is three lanes of shrinking length with a dot at the end of the last, like jobs moving through slots. It sits beside the wordmark "ghr" in Plex Mono 600 and the host name in muted mono. `public/logo.svg` and the favicon are replaced by the mark.
- **Capacity bar** (`navHeader`). This is the design's signature element. It shows "Runners" and "2 of 4 busy" above one segment per slot: filled for busy, outlined for warm, dim for free. It reads `status.instances` and `global_max`. In `all` mode there is no cap, so it shows one segment per live runner and the text "2 busy".
- **Navigation** in two groups:
  - Operate: Dashboard, Runners, History.
  - Configure: Repositories, Toolchains, Storage, Settings.
  - Counts appear as plain mono numbers on the right: live runners and configured repositories. Spec 3 adds Actions under Operate.
- **Runner update card** (`navFooter`). It appears only while `status.runner_update.deadline` is set, showing the version, the deadline and the days left, with **Queue update** (or **Cancel update** once queued). Its border and heading are `warn`, and turn `bad` within 7 days of the deadline. This replaces the runner-update chip.
- **Footer:** "Owner", a mode control and **Log out**. The mode control is three icon buttons in one segmented group (moon, sun, monitor) for Dark, Light and System, each with a tooltip, with the current one pressed.
- **`StatusChips` is removed** from every page. Its content moves to the capacity bar, the update card and the Dashboard's stat cards. Degraded, reconnecting and setup-pending stay as banners at the top of the page, restyled.
- **Login and setup** render outside the shell and take the palette, the type and a centred brand mark. Setup keeps its own scroll container.
- **Saved theme settings are cleared.** `public/theme-init.js` restores every theme axis from `localStorage`, so an owner who used the old switcher would keep its preset or density. The new script clears every `theme-*` key once (a `ghr-theme-v2` flag marks it done), keeps `mode`, and the theme config pins every axis.

## 3. Dashboard

Top to bottom: header, stat cards, Activity, then Repositories beside Disk and Events. This matches the mockup.

### 3.1 Header

- The title "Dashboard" with a one-sentence summary underneath, built from the status:
  - The first clause is "2 of 4 runners busy.", or "2 runners busy." in `all` mode, "No runners busy." when there are none, or "All repositories are paused." when they all are.
  - Waiting jobs are added as "3 jobs are waiting in darkmem", "in darkmem and ghr", or "across 3 repositories", and left out when nothing waits.
  - A repo in error adds "old-repo can't be reached on GitHub" (one repo) or "2 repositories have errors".
- Actions on the right: **Pause all** or **Resume all** (the existing behaviour) and **Add repository** (primary).

### 3.2 Stat cards

Four cards in a row. Each has a label, a number, and a picture of its data, with no description line.

| Card | Number | Picture |
|---|---|---|
| Runners | busy count, "of N busy" | one block per slot: busy, warm, free |
| Waiting jobs | sum of `queued`, "oldest N min" (from the oldest queued job's `created_at`, which needs a new `status` field, §4.3) | step chart of waiting jobs over the last hour, in `warn` |
| API budget | `rate_remaining`, "of 5,000" | remaining-budget bar |
| Host | CPU %, with memory used of total beside it | CPU over the last hour, in accent |

At 640px and narrower the cards form a 2 by 2 grid.

### 3.3 Activity

- A panel titled "Activity", with the range on the left ("13:05 to 14:05", "Sep 27 to Oct 3, per 6 hours") and a segmented control on the right: **1h, 3h, 24h, 7d, 30d**. The default is 1h, and the choice is remembered in `localStorage`.
- **1h and 3h show lanes:**
  - There is one lane per slot, labelled "Slot 1" to "Slot N". Each job is a bar from its start to its end. Succeeded bars are an `ok` tint and failed bars a `bad` tint, with cancelled bars in `sunk` with a `muted` outline. A running bar is solid accent with a pulsing leading edge that the browser extends to "now" every second. A warm runner shows as a dashed accent outline from when it went idle until now.
  - A bar shows "repo job #run" when the label fits, and always has a tooltip with the repo, workflow, job, run, result and duration. Clicking a running bar opens its runner page; clicking a finished bar opens its GitHub job (`html_url`).
  - Beneath the lanes, on the same time axis, are a step area of waiting jobs and a CPU line with a dashed memory line.
- **24h, 7d and 30d show buckets.** The 24h window has one column per hour, 7d one per 6 hours and 30d one per day. Each column has four tracks:
  - busy slot time as a percentage of capacity, or busy runner-minutes in `all` mode;
  - runs stacked as succeeded plus failed;
  - the most jobs waiting at once;
  - average CPU.
  Each column has a tooltip with its values.
- A vertical "now" line and time ticks are drawn to scale, and every axis label names a value the chart reaches.
- The legend changes with the view (lanes or buckets).
- When the history retention is shorter than the window, the uncovered part is shaded `sunk` and labelled "History is kept for 14 days" (or whatever the setting is).
- Gaps in metrics (the daemon was down) break the line rather than drawing it as zero.
- Below 1024px the panel's SVG keeps a 720px minimum width inside its own horizontal scroll container, so the page body never scrolls sideways.

### 3.4 Repositories

- A table with the columns Repository, State, Runners, Waiting, Last 24 hours and Last job.
  - **State** is a word: Running (accent), Idle, Paused (muted), Removing (muted) or Error (bad). An error's message shows under the name in `bad`.
  - **Runners** shows "1/2" with one small cell per allowed runner (unlimited shows the count only).
  - **Waiting** shows the number in `warn`, and stays blank at zero.
  - **Last 24 hours** has 24 hourly cells: `sunk` when nothing ran, three `ok` intensities by run count, `bad` when any run failed that hour, and the current hour outlined in accent.
  - **Last job** shows a result icon, "build #41" and a relative time.
- The section header has a **Manage** link to Repositories, and clicking a row opens that repository.
- **Changed from today:** the per-row max stepper and the Edit, Pause and Resume buttons move into a row menu (a "More actions" icon button with a tooltip) holding Pause or Resume, Edit and Remove. The max stepper lives on the repository page only. This keeps the table scannable; changing a cap is rare.

### 3.5 Disk

- The heading "Disk", with the Docker data root path beside it and a **Storage** link.
- The headline "61%" with "146 of 240 GB used, prunes at 80%".
- One stacked bar of what fills the disk: build cache, images, package caches, toolchains, volumes, and "other" (used minus the categories). The categories use accent tints and grey tints, never status colours. A `warn` marker sits at `disk_high_water`.
- A legend lists each category's size.
- Sources: `GET /storage` for the categories, and `disk_used_bytes` and `disk_total_bytes` for the total (§4.3).

### 3.6 Events

The existing event feed, restyled:
- each row is a time in mono and the message;
- an icon appears only for `ok` (check, ok colour), `warn` (triangle, warn colour) and `error` (x, bad colour);
- the last 8 events show, followed by a link to History.

### 3.7 Removed from the Dashboard

- The runners table: the lanes replace it, and the Runners page keeps the full table.
- The metric tiles.
- The per-repo queue table: the Waiting column replaces it.

## 4. Backend

### 4.1 Metrics retention

`internal/metrics` keeps two rings:

- **Minutes:** the last 180 one-minute samples, holding `at`, `live`, `queued`, `cpu` and `mem` as today.
- **Hours:** the last 720 hourly rollups, each with `at` (hour start), `samples`, `live_max`, `queued_max`, `cpu_avg` and `mem_avg`. A rollup closes when the first sample of a new hour arrives.

Persistence:
- Both rings are written atomically (temp file and rename, mode 0600) to `/var/lib/ghr/metrics.json` (new `daemon.Options.MetricsPath`) when an hour closes and at shutdown, and are read at start.
- A missing file starts empty. A malformed file is logged as a `warn` event and ignored.
- Samples older than their ring's span are dropped on load.
- `GET /metrics` keeps its current shape and returns the last 60 minutes, so nothing that reads it today changes.

### 4.2 `GET /activity?window=1h|3h|24h|7d|30d`

Any other `window` gets 400. The endpoint is mounted on the socket API, so it is reachable over the web as `/api/activity`. The response:

```json
{
  "window": "1h",
  "from": "2026-10-03T13:05:00Z",
  "to": "2026-10-03T14:05:00Z",
  "capacity": 4,
  "history_from": "2026-09-03T14:05:00Z",
  "lanes": [
    { "jobs": [
      { "repo": "darkmem", "workflow": "ci", "job": "test", "run_number": "42",
        "state": "running", "started_at": "...", "finished_at": null,
        "instance_id": "aaaaaa", "html_url": "..." }
    ] }
  ],
  "buckets": [],
  "waiting": [ { "at": "...", "value": 3 } ],
  "cpu": [ { "at": "...", "cpu": 23, "mem": 3328599654 } ],
  "repos": [ { "repo": "darkmem", "hours": [ { "succeeded": 2, "failed": 0, "cancelled": 0 } ] } ]
}
```

- **`capacity`** is `global_max` in `queue` mode and `null` in `all` mode.
- **`history_from`** is `now - history_retention`; the UI shades anything earlier.
- **1h and 3h fill `lanes`**, and `buckets` is empty:
  - Jobs are history entries that overlap the window, plus live instances. A busy instance is `running` from its job's `started_at`, and an idle one is `warm` from its `since`.
  - Jobs are packed greedily by start time into the first lane free at that start. There are `max(capacity, lanes needed)` lanes, or just the lanes needed when `capacity` is null.
  - `waiting` and `cpu` come from the minute ring.
- **24h, 7d and 30d fill `buckets`**, and `lanes` is empty. Bucket sizes are 1 hour, 6 hours and 1 day, aligned in local time: hours on the hour, 6-hour buckets at 00:00, 06:00, 12:00 and 18:00, days at midnight. Each bucket holds:
  - `start`;
  - `busy_minutes`: job time overlapping the bucket;
  - `busy_pct`: `busy_minutes / (capacity × bucket minutes)`, or null without a capacity. It uses today's `global_max`, and the tooltip says so.
  - `succeeded`, `failed` and `cancelled`, counted by finish time;
  - `waiting_max` and `cpu_avg`, from the hour ring, or null where no rollup exists.
- **`repos`** is always the last 24 hourly buckets per configured repo, counted by finish time, so the Dashboard's table needs no extra call.
- History is read once per request through `history.Store`. Results are cached per window for 5 seconds behind a mutex, because the Dashboard polls the endpoint every 5 seconds and several tabs may be open.
- **Local time:** bucket boundaries use the daemon's time zone (`time.Local`), and every timestamp is RFC 3339 with offset. The browser formats times in its own zone.

### 4.3 Status additions

- `system.Docker.DataRootUsage` returns used and total bytes along with the percentage. `model.Status` gains `disk_used_bytes` and `disk_total_bytes`.
- `model.RepoStatus` gains `oldest_queued_at`: the earliest `created_at` among that repo's queued jobs, which the scheduler already reads (`internal/runner/tick.go`, `repoDemand`). It is omitted when nothing is queued.

Each new field is added to the TypeScript mirrors and the shared JSON fixtures, so the existing fixture drift test covers it.

## 5. Frontend structure

- **`web/src/styles/ghr-theme.css`** holds the token overrides per `[data-mode]`, the radius and focus overrides, the selection and scrollbar styles, and the font families. It is imported after `darkraise-ui/styles.css`.
- **`theme.config.ts`** pins every axis (preset `default`, density `compact`, elevation `low`, radius `subtle`, `shellStyle` `classic`, glows `none`), sets `switcher.enabled: false`, and makes mode default to `dark`.
- **New components** in `web/src/components/`:
  - `capacity-bar`, `update-card`, `mode-control`;
  - `stat-card`;
  - `activity-panel`, with `lanes-chart`, `buckets-chart` and `window-control` inside it;
  - `repo-activity-strip`, `repo-table`, `disk-breakdown`, `event-list`, `result-icon`.
  - Each has one job, takes plain props, and is tested on its own.
- **New hook:** `useActivity(window)` polls `GET /activity` every 5 seconds while the Dashboard is open. `useNow` drives the running bars between polls.
- **Removed:** `status-chips`, `glyph` (replaced by `result-icon` and Lucide), and the Dashboard's tile and table code.

## 6. Error and empty states

- **`/activity` fails:** the panel keeps its last data, if it has any, and shows "Couldn't load activity: {error}" with **Retry**. The other sections are unaffected.
- **No jobs in the window:** the lanes stay drawn and empty, with "No jobs ran in the last 3 hours." centred.
- **No repositories:** the table area says "No repositories yet" with **Add repository**, and the activity panel still renders.
- **After a restart:** the hour ring starts from the file. Without a file, the bucket views show history-based tracks, and their metric tracks show "Collecting data since 14:05".
- **The daemon is unreachable:** the existing reconnecting banner shows, and every section keeps its last data.

## 7. Testing

### Go

- **Metrics:**
  - the minute ring keeps 180 samples;
  - an hour closes into a rollup with the right max and average;
  - the persisted file round-trips;
  - a malformed file is ignored with a warning;
  - stale samples are dropped on load;
  - the existing `GET /metrics` shape is unchanged.
- **Activity:**
  - lane packing: overlapping jobs take separate lanes, a job starting as another ends reuses its lane, there are `capacity` lanes when fewer are needed, and the lane count grows past `capacity` when needed;
  - running and warm instances are placed correctly;
  - bucket maths: overlap minutes split across bucket boundaries, `busy_pct` is null in `all` mode, and counts go by finish time;
  - local-time alignment, tested on a fixed zone with a DST transition;
  - `history_from` follows the retention setting;
  - a bad `window` gets 400;
  - the cache serves a second call within 5 seconds and refreshes after;
  - `repos` holds 24 buckets per configured repo;
  - the endpoint is reachable over the socket and over `/api` with a session.
- **Status:** `disk_used_bytes` and `disk_total_bytes` are parsed from the data-root usage, and `oldest_queued_at` is set from the earliest queued job and omitted otherwise.
- **Fixtures:** an `activity.json` fixture joins the drift test, for both a lanes window and a buckets window.

### Frontend

- **Summary sentence:** every case in §3.1.
- **Capacity bar:** queue mode, `all` mode, warm and free slots.
- **Update card:** hidden without a deadline, `warn` before the last 7 days and `bad` inside them, and Queue / Cancel calling the existing endpoints.
- **Stat cards** render from fixtures.
- **Activity panel:**
  - the window control switches views, legend and range, and is remembered;
  - lanes render bars per state with tooltips;
  - clicking a running bar navigates to the runner;
  - the bucket view renders null metrics as gaps;
  - the retention shading appears when `history_from` is later than `from`;
  - the error and empty states show.
- **Repositories table:** each state's word, a blank waiting cell at zero, the strip's cell classes, and the row menu actions.
- **`theme-init.js`:** clears `theme-*` keys once and keeps `mode`.
- **Anti-slop check:** a test fails when any component file under `web/src` contains an em dash or en dash in a string literal, or uses `backdrop-filter`, `background-clip: text` or a multi-colour gradient.

### Manual, before release

- On the runner LXC, in Chrome, at 1280px and 390px wide, in both modes:
  - the Dashboard against live jobs;
  - each activity window;
  - the update card while an update is pending;
  - no CSP errors in the console;
  - fonts load from the binary.
- Contrast is measured for every pair in §1.1.

## 8. Delivery

On the branch `feat/web-ui-overhaul`, in this order:

1. Backend: metrics rings and persistence, `/activity`, and the status fields.
2. Theme: tokens, fonts, theme config, `theme-init.js`, and the brand assets.
3. Shell: sidebar, capacity bar, update card, mode control, banners, and removing `StatusChips`.
4. Dashboard.

Every step leaves the suite green. Pushing master releases a version (the CI release job), so the branch merges only when the owner says so.

## Out of scope

- Redesigning the other pages beyond what the shell and theme change for free (spec 2).
- The Actions page (spec 3).
- An upstream darkraise-ui custom-palette feature.
- A history of the API budget. ghr stores only its current value, and the card shows that.
- Server-sent events. Polling stays.
