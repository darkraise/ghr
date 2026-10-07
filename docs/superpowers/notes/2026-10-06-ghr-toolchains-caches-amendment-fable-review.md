# Fable review of the toolchains spec amendment (66e8737)

**Date:** 2026-10-06
**Reviewer:** dr-superpowers:judge-fable (read-only), at the owner's request
**Verdict:** approve with fixes
**Spec:** docs/superpowers/specs/2026-10-06-ghr-toolchains-caches-design.md

| # | Finding | Severity / confidence | Disposition |
|---|---|---|---|
| 1 | `unused-volumes` can delete a running job's unnamed volume (`docker volume create` then `docker run --rm -v $VOL:…` steps): between steps it is anonymous and unreferenced | medium / high | Owner chose: refuse while any runner is busy (409 at request time); Decisions, §3, §4, §5, §6, §9 and register row 5 updated |
| 2 | The decision bullet misattributed the orphans: cleanup's `rm -f -v` removes the anonymous volumes of the containers it removes; orphans come from a job's own `docker rm` or `compose down` without `-v` | low / high | Reworded |
| 3 | Nothing said how a prune triggers a measurement | medium / high | `runner.Manager.PruneDone func()`, set by the daemon to the measurer's trigger; fires on every `forcedPrune` exit and after a pruning `checkDisk`; triggers coalesce to one follow-up measurement |
| 4 | Type of `last_prune.steps[].freed` unspecified | low / medium | Bytes, parsed with the §3 parser; clients format |
| 5 | The Build Cache row's `Reclaimable` never carries a percentage; units run past GB | low / high | Parser text lists B–PB and the bare form; fixture gains a non-zero bare build-cache row and a TB size |
| 6 | `NewEnv` signature change breaks two test calls | low / high | Stated in §2; plan item |
| 7 | The 60-minute limit is per step, so one install may run ~3 h | low / high | Stated explicitly; no operation-level limit |
| 8 | Python libraries may miss `libgdbm-compat4t64` | low / low | Acceptance row 2 gains an `ldd` check over `lib-dynload`; a missing library joins the apt line |

Claims the review verified correct: volume prune is anonymous-only without `--all` (API 1.42+); a volume referenced by any container, running or stopped, is in use; df JSON field names and string types match the fixtures and the v29.8.2 formatter; base-1000 units everywhere, prune output included; 68/67/8.65 GB match the fixtures; `POST /prune/{scope}` gets a plain-text mux 404 from an older daemon; `standard` and `checkDisk` exclude volumes consistently.
