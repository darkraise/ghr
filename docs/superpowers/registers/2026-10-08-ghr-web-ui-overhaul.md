# ghr web UI overhaul

**Source:** owner request in chat, 2026-10-08
**Covers:** docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | The UI looks AI slop, need rebrand and redesign it. | docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md | - | doing | - |
| 2 | pages are good but the layout is messy, make them more user friendly and graphically appeal. | docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md | - | doing | Dashboard only in this spec; other pages go to spec 2 |
| 3 | I also need to see which pipeline is running from the github, not only the one running on runner, I mean I want to adopt the Actions page of github, but for all available repo, not per-repo. | - | - | open | Spec 3; activity endpoint from spec 1 is reusable |
| 4 | Show a runner's just-finished job in the Dashboard lanes while its runner is still cleaning up; the backend drops cleaning instances until history lands (spec 4.2), so the job disappears for a while | - | - | open | - |
| 5 | Decide how a job with conclusion 'unknown' (no result after 30 minutes) shows in activity buckets and repo hours; it is counted as failed today | - | - | open | - |
| 6 | Activity: tolerate a history entry with a zero StartedAt (it would span year 1) | - | - | deferred | web UI backend plan 1, Task 6 deferred minor: speculative, normal paths always set StartedAt |
| 7 | Backend.Activity cache: eviction can drop an entry another caller just received (costs one duplicate build); cached slices are shared with callers (safe while GET /activity only marshals); key is loc.String(); tests discard errors | - | - | deferred | web UI backend plan 1, Task 7 deferred minors: none can affect results today |
| 8 | Metrics Load discards a saved open hour that lies in the future (clock stepped back across a restart) | - | - | deferred | web UI backend plan 1, Task 2 deferred minor: negligible |
