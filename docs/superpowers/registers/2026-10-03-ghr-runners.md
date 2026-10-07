# Register: native GitHub runner manager

**Source:** user request in chat, 2026-10-03
**Covers:** docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | setup script that will setup multiple github runners for multiple repo | docs/superpowers/plans/2026-10-03-ghr-runner-manager.md | setup.sh installs ghr on a fresh Debian 13 LXC and serves every configured repo (Task 23 E2E 1-2) | done | - |
| 2 | allow to configure to run all or as a queue | docs/superpowers/plans/2026-10-03-ghr-runner-manager.md | queue and all modes behave per spec in Task 23 E2E checks 1 and 5 | done | - |
| 3 | can configure number of concurrent runners | docs/superpowers/plans/2026-10-03-ghr-runner-manager.md | global_max and per-repo max cap concurrent jobs (sched tests + Task 23 E2E check 1) | done | - |
| 4 | setup a TUI app that allow to manage the runners | docs/superpowers/plans/2026-10-03-ghr-runner-manager.md | ghr tui shows repos, runners, events, history, config and performs actions (Task 21 tests + Task 23 check 7) | done | owner ruled done 2026-10-05 |
| 5 | can it be rich UI? | docs/superpowers/plans/2026-10-03-ghr-runner-manager.md | rich TUI: colors, tabs, spinners, gauges, mouse clicks/scroll, detail view (Task 21 goldens + Task 23 check 7) | done | owner ruled done 2026-10-05 |
| 6 | fr-1: stop counting Docker-unreachable failures toward the cleanup give-up (the give-up added for C2 leaks a job's containers and volumes when Docker is down about five minutes) | ghr aa85036 (bounded, inline; no plan) | TestDockerUnreachableKeepsCleaning RED at attempt 10, then GREEN; existing give-up test and go test ./... pass | done | Listing failures (errListProjects) no longer count toward the give-up; ghr v0.1.2 deployed to the LXC 2026-10-04 (setup.sh GHR_VERSION=v0.1.2, binary sha256 matches the release asset, daemon active, all repos active) |
| 7 | Owner: rotate the MaxMind licence key and the NPMplus admin password (both were committed in nginx-proxy-manager-plus/compose.yml and remain in git history) | - | - | done | owner ruled done 2026-10-05 |
| 8 | Owner: replace the all-repositories PAT (expires 2026-10-11) with one scoped to the repos ghr manages, then ghr token set | - | - | done | owner ruled done 2026-10-05 |
| 9 | Owner: decide how the runner version on the LXC is kept current (finding C4: nothing warns when it goes stale; pin GHR_VERSION on routine setup.sh re-runs) | docs/superpowers/specs/2026-10-05-ghr-runner-update-design.md | - | planned | owner ruled 2026-10-05: warning plus a queued update ghr runs when free; continued as row 1 of docs/superpowers/registers/2026-10-05-ghr-runner-update.md |
| 10 | Owner: clean up the LXC leftovers (/root/.pat, /root/ghr.v0.1.0.bak, /root/ghr-labelfix) and remove the ghr_lxc key from authorized_keys and from the Windows machine | - | - | done | owner ruled done 2026-10-05 |
| 11 | 54 confirmed Minor findings from the final whole-branch review | - | - | deferred | Minor findings never entered the fix loop; listed one line each in docs/superpowers/notes/2026-10-04-ghr-final-review-deferred.md |
| 12 | Parked findings, deferred minors and per-task discovered items from tasks 1-24 (69 parked, 51 deferred minors) | - | - | deferred | Each parked finding carries its ruling; verbatim in docs/superpowers/notes/2026-10-04-ghr-run-record.md |
| 13 | setup.sh writes /etc/apt/sources.list.d/docker.list while the LXC already has docker.sources (2026-06-18), so apt warns that every Docker target is configured twice | homelab master 0fc4817 | setup_test.sh: one docker.sources with Docker's deb822 fields, docker.list removed, idempotent | done | fixed 2026-10-05; deployed with ghr v0.1.8: the LXC now has only docker.sources and apt update reports no duplicates |
