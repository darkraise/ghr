# ghr web UI overhaul

**Source:** owner request in chat, 2026-10-08
**Covers:** docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | The UI looks AI slop, need rebrand and redesign it. | docs/superpowers/plans/2026-10-08-ghr-web-ui-2-identity-dashboard.md | Every task of plan 2 passes; npm test, lint and typecheck are green; the owner accepts the new look on the runner LXC | planned | - |
| 2 | pages are good but the layout is messy, make them more user friendly and graphically appeal. | docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md | - | doing | Dashboard only in this spec; other pages go to spec 2 |
| 3 | I also need to see which pipeline is running from the github, not only the one running on runner, I mean I want to adopt the Actions page of github, but for all available repo, not per-repo. | - | - | open | Spec 3; activity endpoint from spec 1 is reusable |
| 4 | Show a runner's just-finished job in the Dashboard lanes while its runner is still cleaning up; the backend drops cleaning instances until history lands (spec 4.2), so the job disappears for a while | - | - | open | - |
| 5 | Decide how a job with conclusion 'unknown' (no result after 30 minutes) shows in activity buckets and repo hours; it is counted as failed today | - | - | open | - |
| 6 | Activity: tolerate a history entry with a zero StartedAt (it would span year 1) | - | - | deferred | web UI backend plan 1, Task 6 deferred minor: speculative, normal paths always set StartedAt |
| 7 | Backend.Activity cache: eviction can drop an entry another caller just received (costs one duplicate build); cached slices are shared with callers (safe while GET /activity only marshals); key is loc.String(); tests discard errors | - | - | deferred | web UI backend plan 1, Task 7 deferred minors: none can affect results today |
| 8 | Metrics Load discards a saved open hour that lies in the future (clock stepped back across a restart) | - | - | deferred | web UI backend plan 1, Task 2 deferred minor: negligible |
| 9 | Redesign the Dashboard layout (item 2, the Dashboard part): stat cards, activity lanes and buckets, repository table, disk, events | docs/superpowers/plans/2026-10-08-ghr-web-ui-2-identity-dashboard.md | Dashboard tasks of plan 2 pass, including the axe and anti-slop tests | planned | - |
| 10 | Delete web/src/lib/activity.ts and its test once the Repositories and Repository pages stop using ActivitySummary | - | - | open | spec 1 section 5 lists it; plan 2 keeps it because repo-summary.tsx still imports it; spec 2 redesigns those pages |
| 11 | Show the Docker data root path beside the Disk heading on the Dashboard | - | - | open | spec 1 section 3.5; no API field exposes the path, so plan 2 omits it |
| 12 | errorText in src/query.ts adds an em dash ('— retry after') to banner and toast copy | - | - | open | anti-slop rule, spec 1 section 1.7; query.ts is outside plan 2's files; spec 2 |
| 13 | Manual pre-release check on the runner LXC (spec 1 section 7): Chrome at 1280px and 390px, both modes, every activity window, the update card, no CSP errors, fonts from the binary, chart keyboard navigation | - | - | open | after plan 2 merges, before a release |
