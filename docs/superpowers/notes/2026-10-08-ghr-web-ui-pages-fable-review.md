# Fable review: spec 2 (the other pages)

**Spec:** docs/superpowers/specs/2026-10-08-ghr-web-ui-pages-design.md, read against the working tree at 0599ec5.
**Reviewer:** dr-superpowers:judge-fable, 2026-10-08. 38 findings: 1 Critical, 14 Important, 23 Minor.

## Verified claims, no action

The spec is right about: the Dashboard's private `Section` (web/src/pages/dashboard.tsx:20-30, `aria-label`, h2, 10px radius); `Row` in repository.tsx:49 and `Field` in settings-form.tsx:27 with the amber "●"; `stateVariant` and the `BadgeVariant` import in lib/status.ts:1,23; `running()` excluding cleaning (status.ts:34); `Glyph` used only by runner-detail.tsx; the finished-runner `Alert` and back button (runner-detail.tsx:157-166); Stop disabled `!live || status.isError` (:151); the Logs/Copy ID/Stop row buttons and the "⧗ waiting" badge (runners-table.tsx:97-131); today's waiting rule (:40); History keeping an unconfigured repo selectable (history.tsx:24); `RESULTS` = success/failure/cancelled; `limit` 200 today (hooks.ts:78); `errorText`'s em dash (query.ts:7); `queueText`'s em dash (lib/toolchains.ts:17); the "see Activity" reload copy (maintenance-card.tsx:17); `EffectiveMax` 1 in queue mode and unlimited in all mode, `EffectiveWarm` 1 (config.go:240-257); `github.Step` without times and `fetchSteps` copying four fields (github/client.go:48-53, daemon/backend.go:319); `DataRootBytes` already reading `docker info` (system.go:288); the manager storing the whole `DiskUsage` (manager.go:173,446); `history.Store.Query(repo, conclusion, limit)` (history.go:89), `Backend.History` (server.go:63), `Client.History` (client.go:89), `historyCmd` (cli.go:265-274); the `activity` cache keyed `window|loc` (backend.go:128); `useRepoActivity`'s test (hooks-pages.test.tsx:58) and `ACTIVITY_LIMIT` (hooks.ts:149); `stepper.tsx` with no callers; the Lucide icons named (`CircleAlert`, `TriangleAlert`, `Trash2`, `Eraser`, `ExternalLink`, `Copy`, `Circle` all exported); `Spinner`'s `label` prop; `Button` `size="icon"` and `loading`; the shell footer's Log out (shell.tsx:102). The remaining em/en dashes outside tests are all in files this spec rewrites or deletes, so the §8 directory walk is feasible.

## Critical

**F1. SectionNav's hash links trip `UnsavedGuard` and can reload the page.** Critical, medium-high. §1.1, §5.2, §7. `UnsavedGuard` blocks every navigation while `count > 0 || saving` (unsaved-guard.tsx:26) with no location comparison. A fragment navigation fires `popstate`; TanStack's `onPushPopEvent` runs the blockers on every pop (node_modules/@tanstack/history/dist/esm/index.js:223-257), and the state-index delta of a hash jump is `NaN`, so a blocked jump calls `win.history.go(-NaN)` (:250), which browsers treat as `go(0)`: a reload with unsaved edits. Ruling: SectionNav items are buttons that call `scrollIntoView` and move focus to the section heading; the URL does not change. Also make `shouldBlockFn` return false when `next.pathname === current.pathname`, and test that a section jump with unsaved changes opens no dialog.

## Important

**F2. The History table's `since` is undefined and churns the query key.** High. §4. For 7d the daemon snaps `from` to a 6-hour boundary 28 closed buckets back (activity/buckets.go:59-66, 97-99), so a table bounded at `now - 7d` disagrees with the chart, and `now` ticks every second, so a key built from it refetches constantly. Ruling: `since` is the activity response's `from`, used verbatim; the table query waits for the activity query.

**F3. `/activity?repo=` 400 for an unconfigured name breaks pages that need removed repositories.** High. §2.1. History keeps an unconfigured repo selectable, and the repository page stays open while a repo is removed (backend.go:488-529). The daemon's other paths answer 404 "unknown repo" (backend.go:401,467,709); `repoHours` matches with `EqualFold` (buckets.go:165) while `Store.Query` compares exactly (history.go:99). Ruling: accept any name; filter with `EqualFold`; `repos` holds the configured entry or is empty; no 400.

**F4. Bucket picking on a `role="img"` element.** High. §4. Buckets are `<g tabIndex={0} role="img">` (buckets-chart.tsx:138-142); an image cannot be an interactive control, and Space is missing. Ruling: in `"jobs"` mode each bucket is `role="button"` with `aria-pressed`, activates on Enter and Space; `"all"` mode unchanged; History in the axe run with a bucket picked.

**F5. "The chart always shows every outcome, stacked" changes the Dashboard.** High. §4 vs spec 1 §3.3: the code draws succeeded and failed only (buckets-chart.tsx:40,148-152). The legend lives in `ActivityPanel` (activity-panel.tsx:29-48), not `BucketsChart`. Ruling: keep succeeded plus failed in both modes, or add cancelled to both and list it as a Dashboard change; History gets its own legend.

**F6. Pending steps would draw as Failed.** High. §3. `ResultIcon` maps an unknown or empty conclusion to Failed (result-icon.tsx:9,20); GitHub also reports `neutral`, `timed_out`, `action_required`. Ruling: pending is `status` neither `completed` nor `in_progress`, drawn as a muted `Circle` labelled "Pending"; completed steps use `ResultIcon`, so other conclusions read as Failed, stated in the spec.

**F7. The Runners selection model is ambiguous once selection is the URL.** High. §3. Today selection is local state on a focusable container (runners-table.tsx:47-66). URL selection with arrows either pollutes Back or leaves focus behind, and with no `$id` the implicit selection jumps when the first runner finishes, unlike today (runners.tsx:15-18). Ruling: rows are links with roving tabindex; arrows move focus and navigate with `replace: true`; the implicit selection, once shown, is written to the URL with `replace`.

**F8. The setup wizard loses the Install buttons.** High. §6.1 moves them to the page header, but the wizard renders `ToolchainsCard`, which carries them (setup.tsx:211, toolchains-card.tsx:94-105). Ruling: a `ToolchainActions` component both render, or the wizard keeps its own row.

**F9. The kit has no "destructive outline" button.** High. §6.2. `ButtonVariant` is `default | destructive | outline | secondary | ghost | link` (darkraise-ui Button.d.ts:2). Ruling: `destructive` for the two irreversible prunes, `outline` for the others.

**F10. Width switching "through `useWidth`" cannot work.** High. §1.1, §8. `useWidth` observes an element and clamps to at least 720 (use-width.ts:5,14); the test `matchMedia` stub always returns `matches: false` (test/setup.ts:7-22). Ruling: add `useMediaQuery(query)` on `window.matchMedia` for 1024 and 1280; tests override the stub per case; column hiding stays CSS.

**F11. The repository chart does not fit beside the facts at 1024px.** Medium. §5.2. The chart keeps a 720px minimum; at 1024px the content area is about 780px. Ruling: facts beside the chart at 1280px and wider, or a lower minimum for `"jobs"` mode.

**F12. The new `errorText` copy double-punctuates the banner and toasts.** High. §1.2. The shell appends ". Retrying." (shell.tsx:115) and toasts interpolate it mid-sentence (settings-form.ts:122,134; repository.tsx:126,138). Ruling: "{message without trailing period}. Retry after 14:05" with no final period; test the banner string.

**F13. Tooltips on table cells are unreachable by keyboard.** High. §5.1, §6.2. `TooltipTrigger` needs a focusable element (Tooltip.d.ts:25-29). Ruling: pick one accessible pattern and apply it to 7 days, Labels and Paths.

**F14. Settings states are incomplete.** High. §7. Runner version also has no `installed` (maintenance-card.tsx:24), checking with no `checked_at` (:66-67), overdue (:55), and `last_outcome: "failed"` with `last_error` (model.go:67-68). Token `state` is `ok | rejected | unverified` (model.go:192); "4,812 of 5,000" needs a rule without `rate_limit`. Ruling: enumerate each line; `ok` reads "Valid"; without a limit show "4,812, resets 14:30".

**F15. `since` semantics for empty values and the fourth caller.** High. §2.3, §8. The web client sends empty parameters (client.ts:79-97), and an empty string is unparsable; `runner/manager.go:207` also calls `History.Query`. Ruling: absent or empty `since` means no bound; `Client.History` omits it for the zero time; list manager.go.

## Minor

- **F16.** §1.2: the install dialog's badge says "lts" (install-dialog.tsx:122); "added" is already a muted word; "private" is undefined. Ruling: "public" and "lts" become muted words; "private" shows nothing.
- **F17.** `ago()` gives "2h ago" / "5m ago" and `dur()` gives "3m10s" (format.ts:5-20), not the spec's "2 h ago", "5 min ago", "3m 10s". Ruling: use the existing output, or change the formatters and list the Dashboard.
- **F18.** Header counts undefined: "2 jobs waiting" (all queued, or only capped repos?), "5 configured" (with removing repos?), "4.2 GB" (with `other_tool_cache`?). Ruling: define each.
- **F19.** `queueText` with no current operation but `queued > 0` (today "2 queued") is not covered; verbs become sentence case. Ruling: "2 queued" stays; verbs capitalised.
- **F20.** §5.2 omits what stays on Workflow labels (Check now, Not checked yet / Checking, `lc.error`, the degraded line, the label-change note; label-check-card.tsx:49-68,103-107) and registrations (Refresh, Delete for offline non-ghr runners, the empty text, the `ghr` badge; registrations-card.tsx:38-77). Ruling: list them; the `ghr` badge becomes a muted word.
- **F21.** §6.2 has no "Not measured yet" state (storage-cards.tsx:42), and the header would read "Docker uses 0%". Ruling: "Not measured yet" until measured.
- **F22.** Dead code not deleted: `repo-actions.tsx`, `repoState`, `maxText`, `glyph.test.tsx`, `state-badge.tsx`, and `unsavedText`'s "●" (draft.ts:27, five tests). Ruling: list them.
- **F23.** Group headings inside tables have no markup rule. Ruling: one `<tbody>` per group with a `<th scope="rowgroup" colSpan>` row.
- **F24.** A tooltip is not an accessible name. Ruling: each icon button carries an `aria-label` naming the row.
- **F25.** `Field` rows with no control would render a `<label for>` with no target. Ruling: without `htmlFor` the label renders as text.
- **F26.** setup.tsx:142 keeps "+ Add repository". Ruling: the wizard's buttons follow §1.2 too, or exempt them.
- **F27.** §3 Containers drops the Project column silently. Ruling: keep it muted, or state the drop.
- **F28.** §3 Steps lacks the empty state, and steps are fetched only while live (hooks.ts:83-90). Ruling: "No steps reported yet"; a never-seen runner shows "Steps are not kept after a runner finishes".
- **F29.** Reload toast with 0 warnings and plurals; SaveBar "1 unsaved change". Medium.
- **F30.** Offline is not stated page-wide. Ruling: every control that changes the daemon is disabled while it is unreachable.
- **F31.** A `ToggleGroup type="single"` deselects to "" on a second click. Ruling: empty reads as All.
- **F32.** A bucket pick after the 500 cap may be incomplete. Ruling: the cap line stays visible while a bucket is picked.
- **F33.** §8 "leaving out test files": say whether `src/test/*` helpers count; vitest's 5 s timeout stays above the 3 s `asyncUtilTimeout`. Medium.
- **F34.** `FormSection` collides with the kit's `forms/components/form-section/FormSection`. Ruling: rename. Low.
- **F35.** `useRepoActions` needs a `RepoStatus` (use-repo-actions.ts:11), undefined before the first status and after removal. Ruling: actions disabled until it exists. Medium.
- **F36.** `idle` is a muted word but an accent-outline light; `online` becomes accent though it is a registration status. Ruling: accept, and say the light and word differ on purpose. Low.
- **F37.** `disk_root` is filled only on the `DataRootBytes` path (cleanup.go:279-285). Ruling: the fallback leaves it empty; update the fake in §8.
- **F38.** The frontend plan is roughly double plan 2's scope. Ruling: split into kit + Runners + History, then Repositories + Toolchains + Storage + Settings + the walk. Low.

## Verdict

Not ready for planning as written. Blocking: F1, F2, F3, F5, F6, F7, F8, F9, F10, F12. F4, F11, F13, F14 and F15 need a sentence each before the frontend plan; the rest are one-line clarifications. Register rows 10, 11, 12, 14, 15 and 16 are discharged by the sections named; row 2 is discharged for the eight pages once the ambiguities are closed.
