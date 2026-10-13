# ghr GitHub artifact storage management

**Source:** owner request in chat, 2026-10-13: "Allow to mange artifacts storage and some other things of github on ghr. I faced a problem where CI failed too frequently because of artifact storage full."; scope answers the same day
**Covers:** docs/superpowers/specs/2026-10-13-ghr-github-storage-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | Quota monitor + warning | docs/superpowers/plans/2026-10-13-ghr-github-storage.md | Status and the banner show the quota level from the billing read; ghr status prints the line | planned | - |
| 2 | Auto-cleanup policy | docs/superpowers/plans/2026-10-13-ghr-github-storage.md | Cleanup deletes artifacts older than the effective age, skipping runs not completed, within the write budget | planned | - |
| 3 | Artifacts page | docs/superpowers/plans/2026-10-13-ghr-github-storage.md | The Artifacts tab shows quota, policy, repositories, operation progress and recent deletions | planned | - |
| 4 | Retention + caches | docs/superpowers/plans/2026-10-13-ghr-github-storage.md | Retention can be set per repository and on all; caches can be deleted one at a time or all | planned | - |
