# Fable review: the Actions view spec

**Date:** 2026-10-09
**Spec:** docs/superpowers/specs/2026-10-09-ghr-actions-view-design.md
**Verdict:** ready for planning after fixes; no blocker. Every finding below was applied to the spec the same day, with the owner's approval.

| # | Severity | Finding | Disposition |
|---|---|---|---|
| 1 | major | "·" chains in the summary and the row facts break spec 1 §1.7 | two-sentence summary; facts row spaced by gaps |
| 2 | major | a "ghr" kit Badge breaks spec 2 §1.2; anti-slop.test rejects Badge imports | the word "ghr" in muted mono |
| 3 | major | fetching under the request context, with no overall deadline, behind the entry lock | `WithoutCancel` plus a 20 s deadline, as `RunnerSteps` does |
| 4 | major | the repository select's option source was ambiguous | options from `/config`, as History |
| 5 | major | the cache-drop list was wrong (RemoveRepo only pauses; FinalizeRemovals missing) | rebuild when the store's config pointer changes |
| 6 | minor | the keep-previous-runs rule was best effort and inconsistent across paths | one rule over the entry's last successful build |
| 7 | minor | the axe sweep lists pages explicitly | Actions and Repositories cases added |
| 8 | minor | "the Repositories page's error text" is not derivable | explicit error table |
| 9 | minor | `run_started_at` can be absent | `*time.Time`, fallback to `created_at` |
| 10 | minor | `api.Backend` and its fakes were not named | named in §1.2 |
| 11 | minor | `groupByDay` is typed to history entries | made generic over a date accessor |
| 12 | minor | the picker needs extracting, and the dialogs' rules differed | shared `RepoPicker`; disabled "added" and "watched" entries |
| 13 | minor | `Section` has no `id` prop; hash scrolling is version dependent | `id` prop; the plan checks hash scrolling |
| 14 | minor | the config and available-repos fixtures and types change too | listed in §4 |
| 15 | minor | no empty state for zero covered repositories | added |
| 16 | minor | the row's accessible name had no carrier | a table per day; named title link |
| 17 | minor | cap versus filter order; summary under a filter | filter, then cap; "in darkmem" |
| 18 | nit | a static LoaderCircle reads as a frozen spinner | kit Spinner labelled Running (owner's choice) |
| 19 | nit | rate-limit wording differed from `errorText` | "Retry after HH:MM" |
| 20 | minor | the server-side `?repo=` filter is surplus | dropped; the browser filters (owner's choice) |
| nits | nit | Run.Status unread; API budget; KnownFields downgrade; failed history read; paused and removing coverage; focus-based polling; PATCH /config, Configure, unwatch and renames are safe | each written into the spec |
