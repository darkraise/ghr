# Fable review: web UI identity, shell and Dashboard spec

**Date:** 2026-10-08
**Reviewer:** Fable 5.1 (judge-fable), read-only, adversarial
**Spec:** docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md at 2e6b3b4
**Verdict:** not ready for planning as is. Blocking: 7, 39, 16, 17, 19, 13, 15, 12, 43. The rest are one-line clarifications.

Severity and confidence follow each title. Evidence paths are as the reviewer gave them.

## Factual claims about existing code

1. **Verified claims, no action.** `applyTokens` writes tokens inline on `<html>` (ThemeProvider.tsx:164-177); `presets` is a fixed map (presets/index.ts:5-9); `SidebarLayout` accepts `navHeader`, `navFooter`, `showThemeSwitcher`, `notificationSlot`, `activeBar` with `"ring"`; `theme-init.js` restores every axis (17-48); `switcher.enabled` exists; metrics keep 60 one-minute samples in memory (metrics.go:18,86-88; run.go:206); history reads the whole file per call; `history_retention` defaults to 30d; `global_max >= 1`; `repoDemand` records `CreatedAt` (tick.go:145); `DataRootUsage` returns only a percent via `df --output=pcent` (system.go:272-286); event levels are `info|ok|warn|error` (model.go:99); `RunnerUpdate` has `deadline`, `queued`, `running`, `check_error` (model.go:51-65); the fixture drift test exists; `useNow` applies the daemon clock offset; CSP `default-src 'self'` covers self-hosted fonts (handler.go:20).
2. **No separate per-repo queue table exists.** Minor, high. dashboard.tsx:86-160 has one repo table with a Queue column. §3.7 should say "the Queue column and its hourglass glyph".
3. **The Dashboard header also has a mode switch and a global-max stepper; the spec drops them silently.** Minor, high. dashboard.tsx:233-247. They exist on Settings; list them in §3.7.
4. **`DataRootUsage` is a `runner.Manager` interface method.** Minor, high. manager.go:68, fakes_test.go:315, run_test.go:71, three call sites in cleanup.go, `setDisk(pct int)`. Name the interface change.
5. **`theme-init.js` falls back to `"system"`.** Minor, high. theme-init.js:2. The fallback must become `"dark"`, or the first paint is system mode.
6. **Only two readers of `GET /metrics` remain** (hooks.ts:52, internal/api/client.go:173). Minor, high. Compatibility holds only if `Metrics()` slices to 60; today it returns every sample.

## Theming

7. **Engine tokens are HSL component triplets; hex overrides would be invalid.** Critical, high. generateTokens.ts:521,548 writes `"0 0% 100%"`; the kit reads `hsl(var(--border))` and `hsl(var(--primary) / 0.5)`. Write the palette as `H S% L%` triplets.
8. **The engine writes many unlisted tokens, several contradicting §1.3.** Important, high. generateTokens.ts:505-626: `--primary-fill`, `--ring`, `--focus-ring`, `--surface-tint`, `--legend`, `--info(-foreground)`, `--surface-base/raised/overlay/sunken/sidebar/header`, `--control-well-subtle/deep`, the sidebar set, `--border-subtle/default/strong`, `--shadow-card`, `--shadow-dropdown`, `--bg-style`, `--noise-opacity`, `--content-gradient-overlay`, `--sf-hue(-2/-3)`. Shadows must be `none`, the gradient overlay and noise zeroed, and the `sidebar-gradient-overlay` / `header-gradient-overlay` classes (SidebarLayout.tsx:182,243) checked. Say which of `--background` and `--surface-base` paints the canvas.
9. **Fonts are engine tokens.** Minor, high. styles.css:11489-11491 defines `--font-sans` (Inter) and `--font-mono` (JetBrains Mono). Override these two.
10. **The §1.2 pixel scale fights the font-size and density axes.** Minor, medium. Decide: override the kit's size tokens, or accept the kit's scale and drop the pixel table.
11. **Clearing `theme-*` and keeping `mode` matches the real keys.** Verified. ThemeProvider.tsx:63-83; storage is read at mount; `setMode` writes only `mode`.
12. **The kit header bar stays and always renders a `UserMenu`.** Important, high. SidebarLayout.tsx:230-253, LayoutHeader.tsx:88-93, UserMenu.tsx:28. Decide whether to hide it on desktop, and whether `user` / `onLogout` are still passed.

## Shell and Dashboard gaps

13. **`starting` and `cleaning` instances are not placed.** Important, high. sched.go:14-19,108-116: `starting` counts toward `global_max`, `cleaning` does not.
14. **Live instances can exceed `global_max`; no overflow rule for the bar or the Runners card.** Minor, high.
15. **Packing by start time splits a runner's job from its warm tail, and "Slot N" is a fiction.** Important, high. `HistoryEntry.ID` and `InstanceStatus.ID` are the instance ID (model.go:79,105). Pack by instance chain or rename lanes; define ties.
16. **Bucket count and partial buckets undefined.** Important, high. Snap `from` to a boundary; 24/28/30 closed buckets plus the open one; the open bucket's denominator is elapsed time.
17. **Daemon `time.Local` vs browser zone.** Important, high. Accept a `tz` query parameter (IANA name), load with `time.LoadLocation`, import `time/tzdata`, include in the cache key, inject the location into the bucket builder.
18. **DST makes 6-hour and day buckets 5/7 or 23/25 hours.** Minor, high. Compute bucket minutes from real boundaries.
19. **`repos[].hours` has no start, order or alignment.** Important, high. Add `start`, state oldest-first.
20. **`html_url` is `omitempty`.** Minor, high. model.go:114. Define the fallback when absent.
21. **The update card ignores `running`, `check_error` and the last outcome.** Important, high. model.go:51-65.
22. **Stat-card sparklines name no data source.** Minor, high. State that they read `GET /metrics`, already polled in the shell (shell.tsx:35).
23. **Runners card in `all` mode undefined; "of 5,000" hard-coded.** Minor, high. `rate_limit` exists on `GET /token` (model.go:188) and is 15,000 for GitHub Apps.
24. **Summary sentence precedence and error copy unspecified.** Minor, high. "All repositories are paused" can hold while runners drain; the error clause rewrites `RepoStatus.Error`.
25. **Disk card: units, negative "other", the `Containers` row, the unmeasured state.** Minor, high. `docker system df` is decimal (disk.go:32), `df` binary; `disk_used_bytes` is 0 until the first `checkDisk` (tick.go:48).
26. **"Link to History" from Events is the wrong page.** Minor, high. History is job history; events live only in the ring.
27. **`oldest_queued_at` is per repo and refreshes once per poll.** Minor, high. The UI takes the minimum; freshness is the last successful poll (manager.go:451).
28. **SVG chart accessibility not designed.** Important, high. Focusable bars with `aria-label`, `role="img"` plus a hidden table or summary, legend not by colour alone.
29. **On mobile, the capacity bar and update card live only in the drawer.** Minor, high. LayoutHeader.tsx:69-74, MobileDrawer.tsx:73,78, sidebar-layout.css:3.
30. **`history_from` assumes exact pruning.** Minor, high. Pruning runs every 24h by `finished_at` (tick.go:50); shading is a guide.

## Backend

31. **The open hour is lost on restart after downtime.** Important, high. Persist the open rollup or close it at shutdown and mark it partial; crash loss is acceptable.
32. **Hour close needs rules for clock jumps; key rollups on UTC.** Minor, high.
33. **The sampler has no events reference.** Minor, high. metrics.go:23-38. The daemon logs a malformed file.
34. **`/activity` cache staleness and lock scope.** Minor, high. Per-window entries with their own lock (singleflight); state the 5-second staleness; whole-file reads match what `Append` already pays (history.go:26).
35. **`repos` scans 24h even for a 1h request.** Minor, high. Built from the same read, keyed by `status.repos` at request time.
36. **`live_max` is never consumed.** Minor, high. Drop it or use it.
37. **Three different nulls (`capacity`, `busy_pct`, `cpu_avg` / `waiting_max`).** Minor, medium. Type them `number | null` and state how each renders.
38. **`GET /activity` needs an `api.Backend` interface change.** Minor, high. server.go:72, backend_test.go fakes.

## Contrast

39. **Several §1.1 pairs fail AA.** Important, high (WCAG 2.x, ±0.02).

| Pair | Dark | Light |
|---|---|---|
| text on ground | 14.13 | 15.45 |
| text on surface | 13.17 | 17.52 |
| muted on ground | 5.51 | 5.23 |
| muted on surface | 5.13 | 5.93 |
| accent on ground | 9.03 | **4.14** |
| accent on surface | 8.42 | 4.70 |
| ok on ground | 7.15 | **3.83** |
| ok on surface | 6.67 | **4.34** |
| bad on ground | 6.21 | 4.58 |
| bad on surface | 5.79 | 5.19 |
| warn on ground | 8.69 | **4.14** |
| warn on surface | 8.10 | 4.69 |
| on-accent on accent | 8.11 | 4.70 |
| shell-text on shell | 12.75 | 12.68 |
| shell-text on shell-hi | 10.76 | 10.08 |
| shell-muted on shell | 4.59 | 5.68 |
| shell-muted on shell-hi | **3.87** | 4.52 |

Suggested: light accent `#086e80`, light ok `#237a3a`, light warn `#8a5700`, dark shell-muted `#7d8a9b`. Record tight passes. The warm outline and the current-hour outline must use accent, not line, to reach 3:1.

## Contradictions

40. **Nav counts: §1.7 says badges, §2 says plain numbers; the mockup shows Toolchains 9 and Actions.** Minor, high.
41. **The mockup footer lacks the mode control and has no header bar.** Minor, high.
42. **`useMetrics` stays mounted in the shell only if the stat cards use it.** Minor, high. shell.tsx:35.

## Testing and delivery

43. **The anti-slop lint fails on 26 existing files** (64 dash occurrences, e.g. runners.tsx:20, runners-table.tsx:43,95,121, runner-detail.tsx:37, toolchains.ts:17, query.ts:7, storage.tsx:68). Important, high. Scope it to this spec's files with an allowlist spec 2 shrinks; use the TypeScript parser to find string literals; also match Tailwind `backdrop-blur-*`; give the gradient check a concrete regex.
44. **`theme-init.js` test needs a loading strategy.** Minor, high. Run it in jsdom with seeded `localStorage` via `new Function` or `vm`.
45. **Delivery seams.** Minor, medium. Step 1 regenerates fixtures (`GHR_UPDATE_FIXTURES=1`); step 3 changes banner copy (shell.tsx:66,72) with shell.test.tsx:21,29; step 4 deletes `web/src/lib/activity.ts` and its test.
46. **Missing tests** for the open-bucket denominator, `tz`, instances over capacity, `starting` / `cleaning`, absent `html_url`, the update card while running, the header decision, and an accessibility pass on the SVG. Minor, high.
