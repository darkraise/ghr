# ghr web UI redesign: identity, shell and Dashboard

**Date:** 2026-10-08
**Status:** draft for owner review. The visual direction was chosen in chat on 2026-10-08 from the mockups below. Revised the same day for all 46 findings of the Fable review (docs/superpowers/notes/2026-10-08-ghr-web-ui-identity-dashboard-fable-review.md), with the owner's approval of the proposed rulings.
**Register:** docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md (item 1 in full, item 2 for the Dashboard only)
**Mockups:** docs/superpowers/mockups/2026-10-08-ghr-web-ui-directions.html, tab "A, graphical" (also published at https://claude.ai/artifact/P1iocQEbpwNjEejkVcPsny). Where the mockup and this spec differ, this spec wins: the mockup has no mode control, shows "Slot" lane labels and an Actions entry, and gives the palette before the contrast fixes.
**Builds on:** docs/superpowers/specs/2026-10-05-ghr-web-ui-design.md
**Code:** `web/`, `internal/metrics`, `internal/history`, `internal/daemon`, `internal/api`, `internal/model`, `internal/system`, `internal/runner`, `internal/github`

## Goal

The web UI stops looking like the untouched darkraise-ui template. It gets its own identity, a shell built around the runner capacity, and a Dashboard that draws what ghr is doing as pictures of real data. This spec is the first of three:

1. **This spec:** the visual system, the app shell, and the Dashboard, plus the backend the Dashboard needs.
2. **Next:** every other page redesigned in this system (register item 2).
3. **Then:** the cross-repo Actions page (register item 3).

## Decisions

- **Direction: "Control Room, graphical".** Dark first, dense, IBM Plex type, one cyan signal colour. The owner picked it over a light "Precise" direction and an industrial "Workshop" direction after seeing all three.
- **The anti-slop rules are binding** (§1.7). They come from Impeccable (pbakaus/impeccable), taste-skill (Leonxlnx/taste-skill) and the VectorLab anti-slop skill. The owner's explicit choices win over them: the Dashboard's first row is stat cards at the owner's request, although the rules list big-number tiles as a common AI pattern.
- **We stay on darkraise-ui and restyle it.**
  - ghr's palette overrides the theme engine's tokens from a ghr stylesheet. The engine writes them as inline styles on `<html>` (`applyTokens` in `ThemeProvider.tsx`), so each override carries `!important`. The values are HSL component triplets, because the kit reads them as `hsl(var(--token))` (§1.1).
  - Presets can't be registered from outside the kit (`presets` is a fixed map), so a ghr preset is not an option.
  - An upstream "custom palette" escape hatch in darkraise-ui was rejected for now as a cross-repo release; it is a follow-up.
- **The theme switcher goes.** ghr has one look. The only theme choice left is dark, light or system, and the default is dark.
- **Type sizes follow the kit.** ghr pins the kit's `compact` density and `medium` font size and uses the kit's size tokens, instead of a separate pixel scale that would clash with kit components. ghr changes only the font families.
- **Icons stay Lucide.** darkraise-ui's own components draw Lucide icons, so a second library would mix stroke styles on one screen. Every icon uses one stroke width (§1.5).
- **Charts are hand-built SVG components**, not recharts. The lane view needs exact geometry that recharts doesn't model, and the other charts are small.
- **The daemon computes activity**, not the browser. A new `GET /activity` packs runners into lanes and builds time buckets (§4.2), because 30 days of history is too much to ship to the browser on every poll.
- **Metrics gain hourly rollups that survive restarts** (§4.1). Today ghr keeps 60 one-minute samples in memory only.

## 1. Visual system

### 1.1 Colour

Colour lives in the shell: a dark rail in both modes, a tinted ground, and one accent. Status colours appear only where they carry meaning, and always next to a word or an icon.

| Role | Dark | Dark HSL | Light | Light HSL | Used for |
|---|---|---|---|---|---|
| ground | `#0d1218` | `213 30% 7%` | `#eef1f4` | `210 21% 95%` | page background (`--background`) |
| surface | `#121a23` | `212 32% 10%` | `#ffffff` | `0 0% 100%` | cards, the activity panel, inputs, menus |
| sunk | `#19222d` | `213 29% 14%` | `#e5e9ee` | `213 21% 92%` | tracks, empty cells, meters |
| line | `#222d3a` | `212 26% 18%` | `#d4dae2` | `214 19% 86%` | borders, row dividers, grid lines |
| text | `#d9e0e8` | `212 25% 88%` | `#121a24` | `213 33% 11%` | body text |
| muted | `#808c9c` | `214 12% 56%` | `#586577` | `215 15% 41%` | secondary text, axis labels |
| accent | `#4cc3d6` | `188 63% 57%` | `#086e80` | `189 88% 27%` | primary actions, selection, focus, "running", CPU series |
| on-accent | `#062027` | `193 73% 9%` | `#ffffff` | `0 0% 100%` | text on accent fills |
| ok | `#4fb36a` | `136 40% 51%` | `#237a3a` | `136 55% 31%` | succeeded |
| bad | `#ef6b5e` | `5 82% 65%` | `#c63a2f` | `4 62% 48%` | failed, errors |
| warn | `#e0a644` | `38 72% 57%` | `#8a5700` | `38 100% 27%` | waiting jobs, deadlines, warnings |
| shell | `#090d12` | `213 33% 5%` | `#131a22` | `212 28% 10%` | sidebar background |
| shell-hi | `#17202b` | `213 30% 13%` | `#232d3a` | `214 25% 18%` | active nav item, capacity panel |
| shell-text | `#c9d2dd` | `213 23% 83%` | `#d5dce5` | `214 24% 87%` | sidebar text |
| shell-muted | `#7d8a9b` | `214 13% 55%` | `#8794a5` | `214 14% 59%` | sidebar secondary text |

**How the overrides are written.**
- `web/src/styles/ghr-theme.css` sets each token under `:root[data-mode="dark"]` and `:root[data-mode="light"]` as an HSL triplet with `!important`. The triplets carry one decimal place, computed from the hex values and not the rounded figures above.
- No component reads a raw hex value; ghr's own components use the same tokens.

**Mandatory tokens.** The plan maps every key `generateTokens` writes. At minimum:
- **ground:** `--background`, `--surface-base`.
- **surface:** `--card`, `--popover`, `--surface-raised`, `--surface-overlay`.
- **sunk:** `--muted`, `--surface-sunken`, `--control-well-subtle`, `--control-well-deep`.
- **line:** `--border`, `--input`, `--border-default`, with `--border-subtle` and `--border-strong` as one step lighter and darker.
- **text:** `--foreground`, `--card-foreground`, `--popover-foreground`.
- **muted:** `--muted-foreground`, `--legend`.
- **accent:** `--primary`, `--primary-fill`, `--ring`, `--focus-ring`. **on-accent:** `--primary-foreground`.
- **Status:** `--success`, `--warning`, `--destructive`, `--info` (accent) and their foregrounds.
- **Sidebar:** the shell values on `--surface-sidebar` and the sidebar set (`--sidebar-*`, including `--sidebar-hover-bg` as shell-hi and `--sidebar-border` as shell-hi).
- **Charts:** `--chart-1` to `--chart-5` as accent, then accent mixed with sunk at 62%, 40% and 24%, then muted. Never green, amber or red, so a chart series never looks like a status.
- **Fonts:** `--font-sans` and `--font-mono` (§1.2).

**Neutralised tokens.**
- `--shadow-card` and `--shadow-dropdown` are `none`.
- `--noise-opacity` is `0`.
- `--content-gradient-overlay` and `--surface-tint` are `none`.
- The `sidebar-gradient-overlay` and `header-gradient-overlay` classes render nothing.
- The plan confirms each in the kit's CSS.

**Contrast.** Measured with the WCAG 2.x formula; every text pair meets 4.5:1 and every outline 3:1.
- Lowest text pairs: light `bad` on ground 4.58, light `shell-muted` on `shell-hi` 4.52, dark `shell-muted` on `shell-hi` 4.68, light `ok` on ground 4.73. These are tight, and any later tint change re-measures them.
- Outlines: the warm-runner outline and the current-hour outline use `accent`, at 4.85 light and 7.71 dark against `sunk`.

### 1.2 Type

- **IBM Plex Sans** for all text, and **IBM Plex Mono** for numbers, times, IDs, versions and code only.
  - They come from `@fontsource/ibm-plex-sans` (400, 500, 600) and `@fontsource/ibm-plex-mono` (400, 500), so no font loads from outside. The CSP's `default-src 'self'` covers them.
  - `--font-sans` and `--font-mono` are overridden so kit components use them too.
  - The `@fontsource/inter` and `@fontsource/jetbrains-mono` packages are removed.
- Sizes come from the kit's tokens at `compact` density and `medium` font size. ghr adds only three roles the kit lacks: the stat number (the kit's 2xl size, mono, weight 500), chart axis labels (the kit's xs size, mono), and the page summary (the kit's base size).
- Tabular numerals everywhere digits line up. Sentence case everywhere. No uppercase letter-spaced labels.

### 1.3 Shape and depth

- Radius: 6px on controls (buttons, inputs, nav items, segmented controls) and 10px on surfaces (cards, the activity panel, dialogs). Nothing is pill-shaped except a switch. The kit's `subtle` radius axis is pinned, and ghr overrides its radius tokens to these two values.
- Borders are the only elevation: 1px `line`. No shadows, no glow, no glass, no gradients on surfaces, and no coloured side borders.

### 1.4 Motion

There is one authored motion: a running bar's leading edge, and a busy runner's light, pulse on a 1.8s ease-out loop. Anything else animates only `opacity` or `transform`, in 150ms or less. Under `prefers-reduced-motion` the pulse stops and transitions are instant.

### 1.5 Icons

Lucide, 16px in the sidebar and 15px inline, stroke width 1.75 everywhere. Unicode glyphs (✔ ✖ ⧗ ⊘) stop standing in for icons in the files this spec touches: a new `result-icon` component draws Lucide icons with an accessible label. `Glyph` stays for pages that spec 2 has not yet redesigned.

### 1.6 Browser surfaces

Text selection, focus rings (2px accent, 2px offset), scrollbars and the caret take their colours from the palette.

### 1.7 Anti-slop rules

Every component this spec creates or touches follows these:

- One accent colour. No purple and no gradients on buttons or text.
- No nested cards. Group by spacing and headings first; a card marks only an element that needs to stand apart.
- Status dots only for live state (the capacity bar). Elsewhere state is a word, and outcomes get an icon.
- No pills around plain text such as state names, counts or errors. Navigation counts are plain mono numbers.
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
│ Owner ◐ ⏻  │                                          │
└────────────┴──────────────────────────────────────────┘
```

**Layout props.**
- `SidebarLayout` keeps its structure. ghr passes `showThemeSwitcher={false}`, `activeBar="ring"` (no 3px left bar) and `notificationSlot={null}`.
- The active item is restyled to a `shell-hi` fill with 600 weight.

**Header bar.**
- The kit always mounts its top header bar with a user menu. On screens 768px and wider it is hidden with CSS; the rail holds everything it held.
- Below 768px it stays, because it carries the drawer trigger. ghr passes `user` and `onLogout`, so its user menu offers Log out there.
- ghr's `headerSlot` puts a compact capacity readout ("2 of 4 busy") in that bar, so the main fact is visible without opening the drawer.

**Brand.**
- The logo mark is three lanes of shrinking length with a dot at the end of the last, like jobs moving through runners.
- It sits beside the wordmark "ghr" in Plex Mono 600 and the host name in muted mono.
- `public/logo.svg` and the favicon are replaced by the mark.

**Capacity bar** (`navHeader`). This is the design's signature element: the label "Runners", a count, and one segment per runner.
- **Segments by state:**
  - `busy`: filled accent;
  - `starting`: accent at 45% opacity;
  - `idle` (warm): accent outline;
  - free capacity: `sunk`;
  - `cleaning`: left out, because it no longer counts against `global_max` (`sched.go`).
- **Queue mode.** The bar has `max(global_max, busy + starting + idle)` segments. When live runners exceed `global_max` (after the cap was lowered), the extra segments follow a 2px gap, and the text reads "5 busy, max 4". Otherwise it reads "2 of 4 busy".
- **All mode.** One segment per counted runner, and the text reads "2 busy".

**Navigation** in two groups:
- Operate: Dashboard, Runners, History.
- Configure: Repositories, Toolchains, Storage, Settings.
- Runners shows the live runner count and Repositories the configured count, as plain mono numbers on the right.
- Spec 3 adds Actions under Operate.

**Runner update card** (`navFooter`). It replaces the runner-update chip and reads `status.runner_update`:

| State | Shows | Action |
|---|---|---|
| `deadline` set, not queued, not running | "2.329.0 required by Oct 14, 6 days left" | **Queue update** |
| `queued` | "Queued since 14:02. Runners update between jobs." | **Cancel update** |
| `running` | "Updating runners" with an inline spinner | none |
| `last_outcome` is `failed` and `deadline` is still set | the deadline line, then `last_error` in `bad` | **Try again** (queues) |

- The card shows only in these states. A `check_error` stays on Settings, where it shows today.
- Its border and heading are `warn`, and turn `bad` within 7 days of the deadline.

**Footer:** "Owner", a mode control and **Log out**. The mode control is three icon buttons in one segmented group (moon, sun, monitor) for Dark, Light and System, each with a tooltip, with the current one pressed.

**Banners and chips.**
- `StatusChips` is removed from every page. Its content moves to the capacity bar, the update card and the Dashboard's stat cards.
- Degraded, reconnecting and setup-pending stay as banners at the top of the page. Their copy is rewritten without dashes, for example "Daemon unreachable: {error}. Retrying."

**Login and setup** render outside the shell and take the palette, the type and a centred brand mark. Setup keeps its own scroll container.

**Saved theme settings.** `public/theme-init.js` today restores every theme axis from `localStorage` and falls back to `system`. The new script:
- clears every `theme-*` key once, with a `ghr-theme-v2` flag marking it done;
- keeps `mode`, and falls back to `dark` when `mode` is absent.

The theme config pins every axis, and `setMode` writes only `mode`, so the cleared keys stay clear.

## 3. Dashboard

Top to bottom: header, stat cards, Activity, then Repositories beside Disk and Events. This matches the mockup.

### 3.1 Header

The title "Dashboard" with a one-sentence summary built from the status, in this order:

1. **Runners.**
   - "2 of 4 runners busy." in queue mode, or "2 runners busy." in all mode. "Busy" counts runners in state `busy`.
   - "No runners busy." when none are.
   - When every repository is paused (ignoring ones being removed), this clause is "All repositories are paused." with nothing busy, or "All repositories are paused, and 2 runners are finishing." otherwise.
2. **Waiting**, when any job waits:
   - "3 jobs are waiting in darkmem." for one repository;
   - "in darkmem and ghr" for two;
   - "across 3 repositories" for more.
3. **Errors**, when any repository has `error` set:
   - one repository shows its raw error: "old-repo: GitHub: not found.";
   - more show "2 repositories have errors."

A degraded daemon has its own banner, so the sentence doesn't repeat it.

**Actions** on the right: **Pause all** or **Resume all** (the existing behaviour) and **Add repository** (primary).

### 3.2 Stat cards

Four cards in a row. Each has a label, a number, and a picture of its data, with no description line. They read `GET /status` and `GET /metrics`; the shell already polls both.

| Card | Number | Picture |
|---|---|---|
| Runners | busy count with "of N busy", or "busy" in all mode | the capacity segments, larger |
| Waiting jobs | sum of `queued`, with "oldest N min" from the earliest `oldest_queued_at` across repositories | step chart of `queued` over the last hour from `/metrics`, in `warn` |
| API budget | `rate_remaining` with "of {rate_limit}" | remaining-budget bar |
| Host | CPU %, with memory used of total beside it | CPU over the last hour from `/metrics`, in accent |

- The waiting age is as fresh as the scheduler's last successful poll, so it moves in `poll_interval` steps.
- Until GitHub has answered once, the API budget card shows "Not measured yet".
- At 640px and narrower the cards form a 2 by 2 grid.

### 3.3 Activity

**Panel.** Titled "Activity", with the range on the left ("13:05 to 14:05", "Sep 27 to Oct 3, per 6 hours") and a segmented control on the right: **1h, 3h, 24h, 7d, 30d**.
- The default is 1h, and the choice is remembered in `localStorage`.
- The panel polls `GET /activity?window=…&tz=…` every 5 seconds, sending the browser's IANA time zone.

**1h and 3h show lanes.** Each lane holds runners one after another; a lane is not a fixed slot, and lanes are labelled "Lane 1" to "Lane N". Each runner is one chain of bars:
- `starting`: a muted outline;
- warm: a dashed accent outline from when it went idle;
- its job: succeeded in an `ok` tint, failed in a `bad` tint, cancelled or skipped in `sunk` with a `muted` outline;
- a running job: solid accent with a pulsing leading edge, extended to "now" every second with `useNow` between polls.

Labels and links:
- A bar shows "repo job #run" when the label fits.
- Clicking a running bar opens its runner page. Clicking a finished bar opens its GitHub job (`html_url`). Without `html_url` the bar is not a link.

Beneath the lanes, on the same time axis, are a step area of waiting jobs and a CPU line with a dashed memory line.

**24h, 7d and 30d show buckets.** The 24h window has one column per hour, 7d one per 6 hours and 30d one per day. Each column has four tracks:
- busy slot time as a percentage of capacity, or busy runner-minutes in all mode, with the axis labelled to match;
- runs stacked as succeeded plus failed;
- the most jobs waiting at once;
- average CPU.

The last column is the bucket still in progress, drawn lighter.

**Nulls render differently:**
- a null `capacity` switches the busy track to runner-minutes;
- a null `waiting_max` or `cpu_avg` is a gap in its line, never a zero;
- a run of nulls back to the start of the window reads "Collecting data since 14:05" on that track.

**Scale and context.**
- A vertical "now" line and time ticks are drawn to scale. Every axis label names a value the chart reaches. The legend changes with the view, and shows each state by shape as well as colour (solid, dashed outline, plain outline, tint).
- When `history_from` is later than the window's start, the part before it is shaded `sunk` and labelled "History is kept for 14 days" (or whatever the setting is). Pruning runs once a day, so the shading is a guide, not an exact edge.

**Keyboard and screen readers.**
- Each bar and column is focusable, an `<a>` when it links and a `<g tabindex="0">` otherwise. Its `aria-label` carries the tooltip text: repo, workflow, job, run, result and duration, or the bucket's values.
- Focus shows the accent ring.
- The chart has a text summary for screen readers ("8 jobs in the last hour, 1 failed, 2 running") and a visually hidden table of the same data.

**Narrow screens.** Below 1024px the panel's SVG keeps a 720px minimum width inside its own horizontal scroll container, so the page body never scrolls sideways.

### 3.4 Repositories

A table with the columns Repository, State, Runners, Waiting, Last 24 hours and Last job:
- **State** is a word: Running (accent), Idle, Paused (muted), Removing (muted) or Error (bad). An error's raw message shows under the name in `bad`.
- **Runners** shows "1/2" with one small cell per allowed runner. Unlimited shows the count only.
- **Waiting** shows the number in `warn`, and stays blank at zero.
- **Last 24 hours** has 24 hourly cells from `activity.repos`, oldest first, the last being the hour in progress. A cell is `sunk` when nothing ran, one of three `ok` intensities by run count, or `bad` when any run failed that hour. The hour in progress is outlined in accent.
- **Last job** shows a result icon, "build #41" and a relative time.

The section header has a **Manage** link to Repositories, and clicking a row opens that repository.

**Changed from today:** the per-row max stepper and the Edit, Pause and Resume buttons move into a row menu: a "More actions" icon button with a tooltip, holding Pause or Resume, Edit and Remove. The max stepper lives on the repository page only. This keeps the table scannable; changing a cap is rare.

### 3.5 Disk

- The heading "Disk", with the Docker data root path beside it and a **Storage** link.
- The headline "61%" with "146 of 240 GB used, prunes at 80%".
- One stacked bar of what fills the disk: build cache, images, containers, local volumes, package caches, toolchains, and "other".
  - Other is used bytes minus the categories, clamped at zero.
  - The categories use accent and grey tints, never status colours.
  - A `warn` marker sits at `disk_high_water`.
- A legend lists each category's size.
- **Units:** sizes are bytes everywhere in the API. `docker system df` reports decimal units, and `ParseSize` already turns them into bytes. The UI formats every size with the existing `humanBytes`.
- **Sources:** `GET /storage` for the categories, and `disk_used_bytes` and `disk_total_bytes` for the total (§4.3).
- **Before the first measurement**, when the total is 0, the section reads "Not measured yet".

### 3.6 Events

The existing event feed, restyled:
- each row is a time in mono and the message;
- an icon appears only for `ok` (check, ok colour), `warn` (triangle, warn colour) and `error` (x, bad colour);
- `info` has none.

The last 8 events show, with no link, because ghr has no separate events page.

### 3.7 Removed from the Dashboard

- The runners table. The lanes replace it, and the Runners page keeps the full table.
- The metric tiles.
- The Queue column and its hourglass glyph. The Waiting column replaces them.
- The mode switch and the global-max stepper in the header. Both stay on Settings.
- The per-row max stepper and buttons, which move into the row menu (§3.4).

## 4. Backend

### 4.1 Metrics retention

`internal/metrics` keeps two rings:

- **Minutes:** the last 180 one-minute samples, holding `at`, `live`, `queued`, `cpu` and `mem` as today.
- **Hours:** the last 720 hourly rollups, each with:
  - `at`: the hour start, keyed on UTC so every rollup is 60 minutes long;
  - `samples`;
  - `queued_max`, `cpu_avg` and `mem_avg`.
  - Local alignment happens only in the bucket builder (§4.2).

Hour-close rules:
- The open rollup closes when a sample from a later UTC hour arrives.
- If the clock jumps forward past whole hours, those hours stay absent.
- A sample from an earlier hour than the open rollup (a backwards clock step) goes into the minute ring but not into a rollup.

Persistence:
- Both rings and the open rollup are written atomically (temp file in the same directory, then rename, mode 0600) to `/var/lib/ghr/metrics.json` (new `daemon.Options.MetricsPath`). Writes happen when an hour closes, every 15 minutes, and at shutdown, where the open rollup is saved as it stands.
- A crash loses at most 15 minutes.
- **On start the daemon loads the file:**
  - a missing file starts empty;
  - a malformed file is ignored and the daemon (not the sampler) adds a `warn` event;
  - samples and rollups older than their ring's span are dropped;
  - a saved open rollup for the current UTC hour resumes, and an older one is closed as it stands (its `samples` shows it is partial).
- **`GET /metrics` keeps its shape.** `Sampler.Metrics()` returns only the last 60 minutes, so `internal/api/client.go` and the web hook see what they see today.

### 4.2 `GET /activity?window=…&tz=…`

**Parameters.**
- `window` is `1h`, `3h`, `24h`, `7d` or `30d`; anything else gets 400.
- `tz` is an IANA zone name, loaded with `time.LoadLocation`. `time/tzdata` is imported so the binary works without a system zone database. A missing `tz` means the daemon's zone, and an unknown one gets 400.
- The endpoint is on the socket API, so it is reachable over the web as `/api/activity`.
- The `api.Backend` interface gains `Activity(ctx, window string, loc *time.Location) (model.Activity, error)`, and the test fakes implement it.

**Response:**

```json
{
  "window": "1h",
  "tz": "Asia/Ho_Chi_Minh",
  "from": "2026-10-03T21:05:00+07:00",
  "to": "2026-10-03T22:05:00+07:00",
  "capacity": 4,
  "history_from": "2026-09-03T22:05:00+07:00",
  "lanes": [
    { "runs": [
      { "instance_id": "aaaaaa", "repo": "darkmem", "workflow": "ci", "job": "test", "run_number": "42",
        "segments": [
          { "state": "warm", "from": "...", "to": "..." },
          { "state": "running", "from": "...", "to": null }
        ],
        "html_url": "..." }
    ] }
  ],
  "buckets": [],
  "waiting": [ { "at": "...", "value": 3 } ],
  "cpu": [ { "at": "...", "cpu": 23, "mem": 3328599654 } ],
  "repos": [ { "repo": "darkmem", "hours": [ { "start": "...", "succeeded": 2, "failed": 0, "cancelled": 0 } ] } ]
}
```

**Top-level fields.**
- **`capacity`** is `global_max` in queue mode and `null` in all mode.
- **`history_from`** is `now - history_retention`.
- `to` is the request time. Timestamps are RFC 3339 in the requested zone.

**Lanes (1h and 3h; `buckets` is empty).**
- A run is one runner instance: its history entry (`HistoryEntry.ID` is the instance ID), plus its live state when it is still alive. Live segments use the instance's states: `starting` from `since`, `warm` from `since` while idle, and `running` from the job's `started_at` while busy.
- A finished run is one segment whose state is the job's conclusion: `succeeded`, `failed`, `cancelled` or `skipped`. Instances in `cleaning` that have no history entry yet are left out.
- **Packing:**
  - Runs whose span overlaps the window are sorted by start, then instance ID, and each goes into the first lane free at its start. A lane is free when its last run ended at or before that moment.
  - There are `max(capacity, lanes needed)` lanes, or just the lanes needed when `capacity` is null.
- `waiting` and `cpu` come from the minute ring.

**Buckets (24h, 7d and 30d; `lanes` is empty).**
- **Size and alignment** (in `tz`): 1 hour on the hour, 6 hours at 00:00, 06:00, 12:00 and 18:00, or 1 day at midnight.
- **Range:** `from` snaps back to the bucket boundary that gives 24, 28 or 30 closed buckets, followed by the open bucket that contains `to`. Bucket lengths come from the actual boundaries, so DST days are 23 or 25 hours.
- **Each bucket has:**
  - `start` and `end`;
  - `busy_minutes`: the minutes of every run's `running` or finished segment that fall inside the bucket, split at its boundaries;
  - `busy_pct`: `busy_minutes / (capacity × minutes elapsed in the bucket)`, where elapsed is the full length for closed buckets and `to - start` for the open one. It is null without a capacity, and it uses today's `global_max`, which the tooltip says.
  - `succeeded`, `failed` and `cancelled`, counted by finish time;
  - `waiting_max` and `cpu_avg`, from the hour rollups whose UTC hour falls inside the bucket. They are null where no rollup exists.

**Repos.**
- `repos` holds one entry per repository in the current config, and repositories that are no longer configured are absent.
- Each holds 24 hourly buckets in `tz`, oldest first: 23 closed hours and the hour in progress, counted by finish time.
- It is built from the same history read as the rest of the response.

**Caching and cost.**
- Each `(window, tz)` pair has its own cache entry with its own lock, so building a 30d response never blocks a 1h poll. An entry lives 5 seconds, so a job that just finished may show as running for up to 5 seconds.
- Every response reads the history file once. That is the same whole-file read `Append` already does, and fine at ghr's scale.

**Types.** In the TypeScript types, `capacity`, `busy_pct`, `waiting_max`, `cpu_avg`, a segment's `to` and `html_url` are typed as possibly null or absent.

### 4.3 Status additions

- **Disk bytes.** `DataRootUsage` on the `runner.Manager` interface (`internal/runner/manager.go`) returns `system.DiskUsage{Pct, Used, Total}` instead of an `int`.
  - `system.Docker` implements it from `df --output=pcent,used,size`.
  - The runner and daemon test fakes, the three call sites in `cleanup.go` and `setDisk` change with it.
  - `model.Status` gains `disk_used_bytes` and `disk_total_bytes`, which are 0 until the first measurement.
- **Rate limit.** `model.Status` gains `rate_limit`, from the GitHub client's last seen `X-RateLimit-Limit`, which the client already records. It is omitted until known.
- **Oldest waiting job.** `model.RepoStatus` gains `oldest_queued_at`: the earliest `CreatedAt` among that repository's queued jobs, which `repoDemand` already reads. It is omitted when nothing is queued.

Each new field is added to the TypeScript mirrors and the shared JSON fixtures, so the existing fixture drift test covers it.

## 5. Frontend structure

- **`web/src/styles/ghr-theme.css`** is imported after `darkraise-ui/styles.css`. It holds:
  - the token overrides and the neutralised tokens (§1.1);
  - the radius overrides;
  - the header hiding at 768px and wider;
  - the selection, focus and scrollbar styles.
- **`theme.config.ts`** pins every axis and sets `switcher.enabled: false`. The pinned values: preset `default`, density `compact`, font size `medium`, elevation `low`, button elevation `flat`, radius `subtle`, control depth `flush`, `shellStyle` `classic`, background `solid`, glows `none`. Mode defaults to `dark`.
- **New components** in `web/src/components/`:
  - `capacity-bar`, `update-card`, `mode-control`;
  - `stat-card`;
  - `activity-panel`, with `lanes-chart`, `buckets-chart` and `window-control` inside it;
  - `repo-activity-strip`, `repo-table`, `disk-breakdown`, `event-list`, `result-icon`.
  - Each has one job, takes plain props, and is tested on its own.
- **New hook:** `useActivity(window)` polls `GET /activity` every 5 seconds while the Dashboard is open. `useNow` drives the running bars between polls. `useMetrics` stays mounted in the shell, because the stat cards read it.
- **Removed:**
  - `status-chips` and its test;
  - the Dashboard's tile, table and queue code;
  - `web/src/lib/activity.ts` and its test (the old per-repo summary).

## 6. Error and empty states

- **`/activity` fails:** the panel keeps its last data, if it has any, and shows "Couldn't load activity: {error}" with **Retry**. The other sections are unaffected.
- **No runs in the window:** the lanes stay drawn and empty, with "No jobs ran in the last 3 hours." centred.
- **No repositories:** the table area says "No repositories yet" with **Add repository**, and the activity panel still renders.
- **After a restart without a metrics file:** the bucket views show the history-based tracks, and the metric tracks read "Collecting data since 14:05".
- **The daemon is unreachable:** the reconnecting banner shows, and every section keeps its last data.

## 7. Testing

### Go

- **Metrics:**
  - the minute ring keeps 180 samples;
  - an hour closes into a rollup with the right max and averages;
  - a forward clock jump leaves absent hours, and a backwards sample skips the rollup;
  - the file round-trips, including the open rollup;
  - a malformed file is ignored and the daemon adds a warning;
  - stale entries are dropped on load, and an old open rollup closes as partial;
  - `Metrics()` returns only the last 60 minutes, so the `GET /metrics` shape is unchanged.
- **Activity, lanes:**
  - overlapping runs take separate lanes;
  - a run starting as another ends reuses its lane;
  - ties order by instance ID;
  - there are `capacity` lanes when fewer are needed, and more when needed;
  - a live instance's starting, warm and running segments are placed correctly;
  - a `cleaning` instance without history is left out;
  - a run without `html_url` serializes without it.
- **Activity, buckets:**
  - minutes split across bucket boundaries;
  - the open bucket's `busy_pct` divides by elapsed minutes;
  - `busy_pct` is null in all mode;
  - counts go by finish time;
  - `from` snaps to give exactly 24, 28 or 30 closed buckets plus the open one;
  - a DST transition in a fixed zone gives a 23- or 25-hour day;
  - `tz` changes the alignment, and an unknown `tz` gets 400;
  - `history_from` follows the retention setting.
- **Activity, other:**
  - `repos` holds 24 oldest-first hours per configured repository, the last one open, with removed repositories absent;
  - a bad `window` gets 400;
  - each `(window, tz)` cache entry serves a second call within 5 seconds and refreshes after, and a slow 30d build does not block a 1h call;
  - the endpoint is reachable over the socket and over `/api` with a session.
- **Status:**
  - `disk_used_bytes` and `disk_total_bytes` parse from the `df` output, with fakes updated;
  - `rate_limit` appears once known;
  - `oldest_queued_at` is the earliest queued job, and is omitted when nothing is queued.
- **Fixtures:** an `activity.json` fixture joins the drift test for a lanes window and a buckets window. Fixtures are regenerated with `GHR_UPDATE_FIXTURES=1` in the same commit as each model change.

### Frontend

- **Summary sentence:** every clause and precedence case in §3.1, including paused with runners finishing.
- **Capacity bar:** queue mode, all mode, starting, warm, free, cleaning excluded, and runners above `global_max`.
- **Update card:** hidden outside its states, each state's text and action, `bad` within 7 days, and the buttons calling the existing endpoints.
- **Stat cards:** render from fixtures, including all mode and an unknown rate limit.
- **Activity panel:**
  - the window control switches views, legend and range, and is remembered;
  - each segment state renders;
  - bars and columns are focusable, with labels;
  - a running bar links to its runner, and a run without `html_url` is not a link;
  - nulls render as gaps and "Collecting data since";
  - the retention shading appears when `history_from` is later than `from`;
  - the error and empty states show.
- **Repositories table:** each state's word, the raw error, a blank waiting cell at zero, the strip's cell classes with the open hour outlined, and the row menu actions.
- **Header:** hidden at 768px and wider, present with the capacity readout and Log out below it.
- **`theme-init.js`:** the test reads the file and runs it with `new Function` in jsdom with a seeded `localStorage`. It checks that the `theme-*` keys are cleared once, `mode` is kept, and the fallback is `dark`.
- **Accessibility:** an axe pass over the rendered Dashboard with fixtures reports no violations. `axe-core` joins the dev dependencies for it.
- **Anti-slop check:** a test parses the files this spec creates or touches with the TypeScript compiler API. It fails on:
  - an em dash or en dash in a string literal or JSX text;
  - `backdrop-filter` or Tailwind `backdrop-blur-*`;
  - `background-clip: text`;
  - a `linear-gradient(` or `radial-gradient(` with more than one colour stop.

  The test holds an explicit file list; spec 2 adds each page it redesigns, until the list is all of `web/src`.

### Manual, before release

On the runner LXC, in Chrome, at 1280px and 390px wide, in both modes:
- the Dashboard against live jobs;
- each activity window;
- the update card while an update is pending;
- no CSP errors in the console;
- fonts load from the binary;
- keyboard navigation through the chart.

## 8. Delivery

On the branch `feat/web-ui-overhaul`, in this order:

1. **Backend:** metrics rings and persistence, `/activity`, and the status fields, with fixtures regenerated in the same commits.
2. **Theme:** tokens, fonts, theme config, `theme-init.js`, and the brand assets.
3. **Shell:** sidebar, capacity bar, update card, mode control, header handling, banner copy (with `shell.test.tsx` in the same commit), and removing `StatusChips`.
4. **Dashboard:** the new sections, deleting `lib/activity.ts`, and the anti-slop and accessibility tests.

Every step leaves the suite green. Pushing master releases a version (the CI release job), so the branch merges only when the owner says so.

## Out of scope

- Redesigning the other pages beyond what the shell and theme change for free (spec 2).
- The Actions page (spec 3).
- An upstream darkraise-ui custom-palette feature.
- A history of the API budget. ghr stores only its current value, and the card shows that.
- A separate events page.
- Server-sent events. Polling stays.
