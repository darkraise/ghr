# ghr GitHub artifact storage management

**Source:** owner request in chat, 2026-10-13: "Allow to mange artifacts storage and some other things of github on ghr. I faced a problem where CI failed too frequently because of artifact storage full."; scope answers the same day
**Covers:** docs/superpowers/specs/2026-10-13-ghr-github-storage-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | Quota monitor + warning | docs/superpowers/plans/2026-10-13-ghr-github-storage.md | Status and the banner show the quota level from the billing read; ghr status prints the line | verify | LXC 2026-10-13, build gh-storage-592d434: Step 5 checks 1 to 4 passed (quota read after Plan: read granted; 3 repos match gh exactly; used 384.634 vs gh 384.63; included 363.28125 = 744 h x 500/1024); check 5 FAILED: discount (384.63 of 384.63 gross) did not stop near the prorated 500 MB allowance, so ghr shows level exhausted while GitHub's net over-allowance is 0.0016; owner's plan allowance is larger than the 500 MB default; the billing cycle check is due on 2026-11-01 |
| 2 | Auto-cleanup policy | docs/superpowers/plans/2026-10-13-ghr-github-storage.md | Cleanup deletes artifacts older than the effective age, skipping runs not completed, within the write budget | verify | cleanup not run live: needs the owner's go-ahead, starting with one repository |
| 3 | Artifacts page | docs/superpowers/plans/2026-10-13-ghr-github-storage.md | The Artifacts tab shows quota, policy, repositories, operation progress and recent deletions | verify | LXC 2026-10-13, build gh-storage-592d434: owner looks at the Artifacts page themselves (Step 6 not driven by the session) |
| 4 | Retention + caches | docs/superpowers/plans/2026-10-13-ghr-github-storage.md | Retention can be set per repository and on all; caches can be deleted one at a time or all | verify | retention and cache deletes not run live: need the owner's go-ahead, starting with one repository |
