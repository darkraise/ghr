# Fable review: ghr runner manager spec

**Reviewed:** `docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md` @ 22ae6db
**Reviewer:** judge-fable (Fable 5.1), 2026-10-03
**Legend:** [V] verified against cited source; [I] inferred.

## Verdict

Not ready for planning yet. Blocking: 1–6 (ownership/privilege model, hook failing
jobs, trust model, idle_timeout vs warm). 7–15 should become explicit plan tasks and
test cases. Both open questions are answered: a shared dist dir does not work (the
runner writes `.runner`/`.credentials`/`.credentials_rsaparams` into its root on
`--jitconfig` [V] actions/runner `src/Runner.Listener/Runner.cs`), and polkit vs
sudoers dissolves if the daemon runs as root.

## Findings

1. **Instance-dir creation impossible as specified** — high/high. Runner writes into
   its install dir; Debian `fs.protected_hardlinks=1` [V systemd 50-default.conf]
   blocks `cp -al` of root-owned files by ghrunner; root-made copies leave dirs
   ghrunner can't write. Fix: daemon as root, copy then chown dirs, spawn units as
   ghrunner (or plain `cp -a`). Drop the shared-dir clause.
2. **Config write-back impossible** — high/high. Temp+rename into root-owned
   `/etc/ghr/` fails as ghrunner. Fix: daemon as root, or mutable config under
   `/var/lib/ghr/`.
3. **polkit/sudoers are both root grants** — medium/high. polkit can't inspect unit
   properties (`--uid root`) [I]; docker group is root-equivalent anyway. Fix: run
   daemon as root; delete open question 2.
4. **`started` hook fails jobs when daemon unreachable** — high/high. Pre-job script
   non-zero exit fails the job, no timeout [V docs: running-scripts-before-or-after-a-job].
   Fix: hook writes `<instance>/job.json`, best-effort socket notify with short
   timeout, always exit 0; hook must be a `.sh` path [I] under e.g. `/opt/ghr/hooks/`.
5. **Trust model unstated** — high/high. Jobs run as the daemon user: can read PAT,
   drive the socket, `docker run -v /:/host`. Self-hosted runners should almost never
   serve public repos [V security-hardening doc]; fork-PR workflows can be enabled on
   private repos [V]. Fix: Trust-model section (private repos only, no fork-PR
   workflows, LXC is blast radius, PAT scoped + expiry + rotation path); `ghr repo
   add` warns on public repos.
6. **`idle_timeout` churns warm runners** — medium-high/high. Warm runners killed and
   respawned every 5 min. Fix: in `all` mode timeout applies only to idle instances
   beyond `warm`.
7. **Over-spawn race** — medium-high/high. Busy runner's job still listed `queued`
   for seconds → spawn extra runner that idles, holding a global slot. Fix: exclude
   jobs attributed to busy instances (`runner_name`), or keep covering until API
   shows `in_progress`.
8. **No `starting` detection/timeout** — medium-high/high. Fix: poll runners API
   (`status`, `busy`) for starting instances; add `start_timeout` (~2m).
9. **`cleaning` excluded from caps → prefix cleanup races next job even at max 1** —
   medium/high. Fix: count cleaning toward repo cap (not global), or no spawn for a
   repo while cleaning.
10. **`GITHUB_JOB` can't map to jobs API** — medium/high. It's the YAML key; API has
    `name`, `runner_name`, `runner_id` [V REST workflow-jobs]. Fix: attribute by
    `runner_name`.
11. **Cleanup misses networks/volumes; compose project-name collisions** — medium/
    medium-high. `working_dir` label is on containers [V compose labels.go], not
    networks/volumes [I]; same checkout basename → same project across instances.
    Fix: resolve project names then `compose -p <name> down -v --remove-orphans`;
    set `COMPOSE_PROJECT_NAME=ghr-<id>` per runner [I inherits]; document what
    cleanup doesn't cover.
12. **403 ambiguity** — medium/high. Rate limits return 403/429 [V rate-limits doc];
    don't go degraded on those; 404 = per-repo access error [I]. Serialize mutating
    calls ≥1s apart [V best-practices].
13. **Label matching** — medium. GitHub auto-adds `self-hosted`, OS, `X64` [V JIT
    example]; matching is case-insensitive [I]; old compose used `docker` label.
    Fix: effective labels = system ∪ config ∪ repo, case-insensitive; migration note.
14. **`all` mode vs `max` ambiguous** — medium/high. Fix: `max: 0` = unlimited;
    default unlimited in `all` mode.
15. **`setup-dotnet` needs `DOTNET_INSTALL_DIR`** — medium/high [V setup-dotnet
    README]. Fix: set it into the tool cache; add compose/buildx plugins to setup.
16. **Runs in `waiting` may hide queued jobs** — low-medium/low. Also poll
    `status=waiting` or test it.
17. **`_diag` logs discarded; not shipped to Loki** — low-medium/high [V autoscaling
    doc]. Fix: move to `/var/lib/ghr/logs/<id>/`, prune by retention; optional Alloy
    `loki.source.file`.
18. **Idle stop races assignment; JIT deletion semantics** — low/medium. Check
    `busy==false` before stop; 404 after job = success; reconcile periodically.
19. **Queue loop: skip or stop at capped FIFO head?** — low/high. Fix: skip
    ineligible repos; define "oldest uncovered job" by `created_at` ordering.
20. **Unfiltered `builder prune` wipes useful cache** — low/high. Use
    `--keep-storage`/`--max-used-space` and/or `until=72h`; measure Docker data-root fs.
21. **Pause semantics undefined** — low/high. Suggest: no new spawns, idle stopped,
    busy finish.
22. **Shared tool cache / `$HOME` concurrency** — low/medium. Document tolerated races.
23. **Startup reconciliation mapping undefined** — low/high. Define `<id>` once
    across unit, dir, runner name; handle missing `ghr.json`.
24. **Testing gaps** — low/high. Add cases for 4, 6, 7, 8, 12, 13; manual E2E: job
    starting during daemon restart, two concurrent compose jobs of one repo.
25. **Nits** — paginate jobs `per_page=100`; `runner_group_id: 1` OK [V partial];
    PAT permissions in spec correct [V]; `ghr repo add` needs PAT already covering
    the repo; lowercase `x-ratelimit-*`; 24h queue expiry [V]; OSC 52 primary over
    `xdg-open` on headless LXC; `darkraise/ghr` must be public for tokenless
    downloads; YAGNI candidates: sparklines, median-scaled bars, SSE; migration note
    for old `/opt/gh-runners/work/*` and the three `homelab-*` runners.
