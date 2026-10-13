# Fable review: GitHub storage spec

**Reviewer:** dr-superpowers:judge-fable, 2026-10-13
**Spec:** docs/superpowers/specs/2026-10-13-ghr-github-storage-design.md at 1a4635b

Review complete. Outcome first: the spec is coherent with the codebase in most places, but it has two data-loss hazards in the cleanup rule, a rate-limit interaction that can pause the runner poller, a synchronous bulk-delete API that cannot finish inside the web server's write timeout, and several type and math ambiguities that two implementers would resolve differently. One more short design pass on sections 2.3, 3.1 and 3.2 is needed before planning; the rest are amendments.

Spec reviewed: /root/repositories/ghr/docs/superpowers/specs/2026-10-13-ghr-github-storage-design.md

FINDINGS

F1. Critical. Section 2.3 (cleanup, in-progress skip).
What is wrong: the skip lists only runs with status `queued` and `in_progress`. GitHub's status filter also has `waiting`, `requested` and `pending` (docs: https://docs.github.com/en/rest/actions/workflow-runs?apiVersion=2022-11-28, "List workflow runs for a repository", `status` values). A run whose build job uploaded an artifact and whose deploy job is `waiting` for an environment approval is not in the protected set.
Scenario: `cleanup_max_age: 6h`; a release workflow builds at 09:00, waits for approval; approver clicks at 16:00; the deploy job's `download-artifact` fails because the artifact was deleted at 15:00.
Evidence: spec line 110; `ListRuns` takes one status per call (/root/repositories/ghr/internal/github/client.go:498); docs list 14 values and say nothing about combining them.
Fix: include `waiting` at minimum, and `pending`/`requested` for safety (five calls per repository per cleanup), or replace the per-status listing with one `ListRecentRuns`-style page filtered client-side on `status != "completed"`. State explicitly in §2.3 which statuses are protected.

F2. Critical. Section 2.3 (cleanup rule) and 3.1 (1h minimum).
What is wrong: the age rule only protects the producing run. Artifacts consumed by a different run (a `workflow_run`-triggered workflow downloading the triggering run's artifact, `actions/download-artifact` with `run-id`, `gh run download`, a nightly job reading the last build) are unprotected, and the 1h floor makes that routine rather than rare. The in-progress check cannot see cross-run consumers because the consumer has its own run ID.
Scenario: a PR-comment bot triggered on `workflow_run: completed` is queued behind other jobs for 70 minutes; `cleanup_max_age: 1h` deletes the artifact it was about to download. The failure the feature exists to prevent (a red CI run) is replaced by another one.
Evidence: spec lines 109-116, 137, 144; `Artifact.WorkflowRun.ID` (spec line 47) is the only linkage available.
Fix: raise the floor (24h is a defensible minimum for a global rule; keep 1h only as a per-repository override with an explicit warning), state in §2.3 and the README that cross-run consumers are not protected, and show that sentence in the save-preview dialog. Optionally protect artifacts whose `WorkflowRun.ID` matches a run that any queued/in-progress/waiting run's `triggering_workflow_run` references; that is extra calls, so documenting the limit is the cheaper fix.

F3. Important. Section 2.1 (rate budget) and 2.3.
What is wrong: the 500-calls-left guard addresses the primary limit only. Deletes are content-generating requests and GitHub's secondary limit is "no more than 80 content-generating requests per minute" and "no more than 500 content-generating requests per hour" (https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api?apiVersion=2022-11-28). A secondary-limit 403/429 is classified `ErrRateLimit` and suspends the shared client (client.go:329-340, 418-435); every manager call then fast-fails with `ErrRateLimit`, which `Manager.apiErr` turns into a runner-wide pause (/root/repositories/ghr/internal/runner/manager.go:851-859). Runner starts also share the 1-per-second `mutate` slot with deletes (client.go:454-476), and waiters are served in order, so a burst of deletes delays `GenerateJITConfig`.
Scenario: the first cleanup pass on a repository with 90 days of artifacts (several hundred) deletes at 1/s; after ~500 deletes in the hour GitHub answers 403 secondary; the client is suspended, the runner poller pauses, and a queued job waits. Repeats on the next pass.
Fix: cap deletes per pass (for example 100, oldest first, so a capped pass still removes what costs most), keep a rolling per-hour mutation count in the client or in `ghstorage` with a budget well under 500 that reserves room for JIT configs, and say in §2.1 that cleanup yields the mutation slot to runner starts (or that the manager's starts take priority). Also correct the pass-cost estimate: with cleanup on, each cleaned repository adds 2-5 `ListRuns` calls, so 36 repositories cost ~150-250 calls per pass, not 75.

F4. Important. Section 3.2 (`POST .../artifacts/delete`, `DELETE .../caches`).
What is wrong: the bulk routes answer 200 synchronously after deleting every item, but `mutate` spaces calls 1 s apart, and the web listener has `WriteTimeout` 60 s (/root/repositories/ghr/internal/daemon/run.go:43, 303). The kill route needed a special 100 s window for this reason (/root/repositories/ghr/internal/api/server.go:242-249, 474-478).
Scenario: the user ticks 150 artifacts or clicks "delete all caches" on darkmem (6 GB, many entries); the browser request dies at 60 s, deletes continue server-side with no result reported, and the user retries, doubling the work.
Fix: make bulk deletes asynchronous like prune and toolchain operations: 202, progress and result in the snapshot (an `Operation`-like entry), with the change stream refreshing the page. Keep the single-item `DELETE` routes synchronous.

F5. Important. Section 3.1 (config types).
What is wrong: the spec uses `""` for "off" globally and `"off"` per repository, but `config.Duration.UnmarshalYAML` calls `ParseDuration`, which rejects both (`time.ParseDuration("")` errors; "off" is not a duration), and `Duration.String()` writes `0s` for zero, which `Validate` as specified ("at least 1h") would then refuse on the next load. The Go types are not named, so one implementer will use `Duration` with 0 = off, another a string.
Evidence: /root/repositories/ghr/internal/config/config.go:103-150, spec lines 133-144.
Fix: specify the types. Suggested: `CleanupMaxAge Duration` with `omitempty` and 0 meaning off (so `""`/absent is off and nothing is ever written for off), and per repository `Cleanup string` with the values `off` or a duration (absent = inherit); `Validate` rejects a non-zero global or override under the floor. State the same encoding for `GitHubStoragePatch` and for `RepoStorage.CleanupMaxAge`.

F6. Important. Sections 2.4 and 3.3 (status alert vs CLI line).
What is wrong: `StorageAlert {Level, UsedPct, RunsOutAt, ResetsAt}` does not carry used or included GB-hours, yet the `ghr status` line prints "373 / 372 GB-h". `printStatus` reads only `model.Status` (/root/repositories/ghr/cmd/ghr/cli.go:309). Also the browser's banner depends on the `status` change topic, whose fingerprint is built from the manager's status with decorations added by hand (/root/repositories/ghr/internal/daemon/changes.go:70-76); if the alert is only added in `Backend.Status()` (backend.go:278-294) the banner will not refresh on a level change.
Fix: add `UsedGBHours`, `IncludedGBHours` and `Month` to `StorageAlert`, and state that the alert is added in both `Backend.Status` and the `status` fingerprint.

F7. Important. Section 2.2 (`IncludedGBHours`) and 5 (live check).
What is wrong: the spec assumes 10^9-byte GB and defers verification, but the billing page already states "GitHub Actions measures storage in binary gigabytes (GB)", 1 GB = 2^30 bytes (https://docs.github.com/en/billing/managing-billing-for-your-products/about-billing-for-github-actions). With binary units, 500 MB over 744 h is 363.3 GB-h, not 372. The October data cannot settle it: 373.3 exceeds both and the failures began on Oct 6 when the cumulative sum crossed both thresholds the same day.
Fix: compute with 2^20/2^30 per the docs, keep the live check as a sanity check rather than the decider, and convert `ArtifactBytes` to GB with the same divisor in the projection.

F8. Important. Section 2.2 (`RunsOutAt`, `ProjectedGBHours`).
What is wrong: "current hourly rate" is undefined (stored bytes now as GB per hour? yesterday's billed GB-h / 24? either plus Packages?). The rate can be 0 (division by zero) and the field is meaningless once used >= included. If `RunsOutAt` is computed from `now` at read time, the `github-storage` fingerprint changes on every tick. Also `IncludedGBHours` depends on hours in the month, which changes at UTC midnight on the 1st while the billing pass is hourly, so for up to an hour the card shows last month's quota with `ResetsAt` in the past.
Fix: define rate = (private artifact bytes now in GB) + (last full UTC day's Packages GB-h / 24); `RunsOutAt` nil when rate = 0, when used >= included, or when it falls after `ResetsAt`; computed at pass time and stored in the snapshot; a billing read is triggered at `ResetsAt`; levels use `now < ResetsAt`.

F9. Important. Section 3.2 (change stream topic).
What is wrong: the hub calls `sources()` four times a second (/root/repositories/ghr/internal/changes/hub.go:63-74, 196-200) and `Fingerprint` JSON-marshals the value (hub.go:77-85). Hashing a snapshot that lists every artifact and cache of 36 repositories, with timestamps stripped on each call, is a few hundred KB of marshalling four times a second for the life of every open tab.
Fix: use the counter pattern the `events` and `history` topics use (changes.go:66-69, 84, 92): a generation incremented on every snapshot change. Also add the topic to `TOPIC_QUERIES` (/root/repositories/ghr/web/src/api/use-change-stream.ts:8-17); the spec does not mention the web side of the mapping.

F10. Important. Section 4 (artifact row "link to the producing run's page in ghr").
What is wrong: the run page resolves the repository through `coveredName`, which 404s for anything not configured or watched (/root/repositories/ghr/internal/daemon/rundetail.go:69-80, 166-170). Coverage of the Artifacts page is every repository the token reaches (spec line 26), so the link is dead for most rows.
Fix: link to ghr's run page only when the repository is configured or watched (the snapshot can carry `Covered bool`), otherwise to GitHub's run URL (`https://github.com/{owner}/{repo}/actions/runs/{id}`).

F11. Important. Section 3.2 (`PUT .../retention`) and 4.
What is wrong, three parts. (a) `RetentionMaxDays` is 0 "while unknown" and retention is read at most once a day, so the 1..`RetentionMaxDays` check rejects every value for a repository whose retention has not been read yet. (b) After a successful PUT the snapshot keeps the old `RetentionDays` for up to a day; the row shows the old value and the user retries. (c) The retention endpoint's `days` is documented as the number of days to retain "checks, workflow runs, commit statuses, artifacts, and logs" (https://docs.github.com/en/rest/actions/permissions?apiVersion=2022-11-28); lowering it to a few days to save storage also shortens what ghr's own Actions, run and job-log pages can show, and the spec's confirmation dialog does not say so.
Fix: read retention on demand before the PUT when unknown (or validate 1..400 and let GitHub's 422 answer); write the new value into the snapshot on success; word the dialog with the logs/runs consequence.

F12. Important. Decisions ("Public repositories are not cleaned by the global age") and 2.2 ("stored bytes now" over private repositories only).
What is wrong: the claim that public repositories' artifacts do not accrue against the allowance is unverified. The billing page says the quota applies "for private repositories" and that Actions minutes are free in public repositories, but it has no sentence saying public-repository artifact storage is free (same page as F7). The author's own data can settle it: the October `usageItems` carry `repositoryName`; if any public repository appears with Actions storage, the decision and the projection are wrong.
Fix: check the usage items against the visibility list before planning; if public storage does accrue, drop the private-only rules (the projection, the global-age exemption) and simplify.

F13. Important. Section 2.2 (`ResetsAt`, `Month`) and 3.3 ("exhausted until Nov 1").
What is wrong: the spec assumes the allowance resets on the calendar month in UTC. The docs say storage charges "reset to zero at the start of each billing cycle", and the usage endpoint is queried by calendar year/month (https://docs.github.com/en/rest/billing/usage?apiVersion=2022-11-28), but nothing on either page says a Free user's cycle is the calendar month. Unverified; if the cycle is anchored to the account's billing day, every number in the quota card is off by a partial month and the "until Nov 1" promise is wrong.
Fix: state the assumption in §2.2 and in the card's help text, and add to the live check: watch for the day the billing read drops to zero.

F14. Minor. Section 2.1 ("The owner type is read once at start and after an owner change").
What is wrong: the owner cannot change at runtime. `Reload` refuses a changed owner (/root/repositories/ghr/internal/daemon/store.go:86) and `ErrConfigured` says it changes only by editing config.yaml and restarting (store.go:19); the client is built with the owner once in `start` (run.go:178-179).
Fix: "read once at start".

F15. Minor. Section 5 (billing errors) and 1 (`StorageUsage`).
What is wrong: the error wording "token lacks <permission> on <owner>/<repo>" (/root/repositories/ghr/internal/daemon/actions.go:89-90) names a repository; billing has none. `GET /users/{username}/settings/billing/usage` is a user-account permission ("Plan: read", https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens?apiVersion=2022-11-28), which means it reads the token owner's own billing: when the PAT belongs to a different user than `owner` (a collaborator's token), the read fails for good even though the repositories' artifacts count against `owner`. The org endpoint additionally requires the enhanced billing platform and an admin token. Whether these 403s carry `X-Accepted-GitHub-Permissions` is unverified.
Fix: a billing-specific wording ("the token's user needs Plan: read; the quota is read for the token's own account"), and a note that the quota is only meaningful when the token's user is `owner`.

F16. Minor. Sections 1 and 2.1 ("lists non-expired artifacts").
What is wrong: the list endpoint has no expiry filter (query parameters are `per_page`, `page`, `name`; https://docs.github.com/en/rest/actions/artifacts?apiVersion=2022-11-28), so `expired == true` items must be dropped client-side, and the spec does not say whether they are excluded from `ArtifactCount`/`ArtifactBytes` and from the expandable list. Deleting an expired artifact is also pointless.
Fix: state that expired artifacts are dropped from the snapshot entirely and never deleted.

F17. Minor. Sections 1 (404 as success) and 3.2 (manual deletes record `By: "manual"`).
What is wrong: when a manual delete and a cleanup pass hit the same artifact, both succeed (one by 404) and both append a `Deletion` with its bytes, double-counting the recent-deletions list.
Fix: record a deletion only on a 204, or only when the artifact was still in the snapshot when the delete started.

F18. Minor. Section 3.2 (manual delete "updates the snapshot without a full pass") and 2.1.
What is wrong: an inventory pass that listed the repository before the manual delete and finishes after it writes the old listing back, resurrecting the deleted artifact until the next pass.
Fix: a per-repository generation bumped by manual deletes; a pass that started under an older generation re-applies the deletions it missed (or drops its listing for that repository).

F19. Minor. Sections 2.1 and 5 (token change, permission grant).
What is wrong: the spec says nothing about the token generation that every daemon cache honours (/root/repositories/ghr/internal/daemon/backend.go:265-273, 817; manager.go:796). After `SetToken` or `Reload`, per-repository permission errors stay on the rows for up to 15 minutes, and a pass in flight under the old token writes its result as if it were the new token's.
Fix: `SetToken` and `Reload` call `ghstorage.Refresh()` and clear row errors; a pass records the generation it started under and drops its result when it changed (the existing pattern).

F20. Important. Section 3.2 (`POST .../artifacts/delete` body and semantics).
What is wrong: unspecified: both `ids` and `older_than` given, neither given, an empty `ids`, ids not in the snapshot for that repository (delete anyway, or refuse?), the status when every id fails (200 with `failed`, or 502?), whether `older_than` applies the in-progress skip (the user named no ids, so "deletes what it is asked to" does not settle it), and whether `{repo}` matches ignoring case like every other route (`config.Repo` uses `EqualFold`, config.go:401).
Fix: exactly one of `ids`/`older_than`, non-empty, else 400; ids must be in the snapshot, else listed in `failed` with "not in the last inventory"; always 200 with the counts once the request is valid; `older_than` applies the in-progress skip and the preview shows the count; repository match ignores case.

F21. Minor. Section 3.2 (`GET /github-storage/preview`).
What is wrong: missing `cleanup_max_age` (400?), the value `off` (empty list?), and whether `repo` given means "that repository's override" and absent means "the global age on every private repository without an override" are not stated; the preview cannot apply the in-progress skip without calls, so it over-counts.
Fix: define the three cases and label the dialog "up to N artifacts".

F22. Minor. Sections 2.1 and 3.2 (`Refresh` while running "does nothing" / 202).
What is wrong: the existing storage service answers `ErrMeasuring` → 409 in that case (/root/repositories/ghr/internal/storage/service.go:126-138, backend.go:998-999). Either pattern works; the spec picks the other without saying so.
Fix: pick one and state it (409 keeps the UI's existing refresh-button handling).

F23. Minor. Section 2.2 (`RepoStorage.CleanupMaxAge`, `Inherited`).
What is wrong: for a public repository with a global age set, the effective value is off but "inherited" from what? `""` vs `"off"` with `Inherited true` is left to the implementer.
Fix: replace the pair with `Cleanup string` ("off" or a duration) and `CleanupSource string` ("global", "override", "public", "none").

F24. Minor. Section 3.1 (`repos:` overrides).
What is wrong: the override map is keyed by repository name but nothing says keys are matched ignoring case (GitHub names are), nor that `Validate` rejects empty keys, keys with a slash, or two keys differing only by case, as it does for `repos` and `watch_repos` (config.go:502-511, 547-567).
Fix: add those rules to §3.1.

F25. Minor. README section.
What is wrong: `Parse` uses `KnownFields(true)` (config.go:180), so a config.yaml with a `github_storage` block is refused by any older ghr. The README has a "Downgrading" note for `watch_repos` for this reason (/root/repositories/ghr/README.md:57) and the spec's README section does not extend it.
Fix: extend the downgrade note.

F26. Minor. Section 2.2 (`CheckedAt time.Time // zero before the first`).
What is wrong: the model's convention for "absent until known" is `*time.Time` with `omitempty` (model.go:84-97, 109, 253); a zero `time.Time` serialises as `0001-01-01T00:00:00Z` and the web code would need a special case.
Fix: `CheckedAt *time.Time`, same for `Quota.ReadAt` if ever absent.

F27. Minor. Section 1 (`StorageUsage` fields).
What is wrong: the usage report schema declares `quantity` as an integer (https://docs.github.com/en/rest/billing/usage?apiVersion=2022-11-28) while the measured values are fractional (373.3). An implementer following the schema would decode into an `int` and fail or truncate.
Fix: state `float64` and cite the measurement.

F28. Minor. Sections 2.3 and 3.2 (deletion logging).
What is wrong: cleanup deletions go to the daemon log only; every other user-initiated action in the backend writes to the events ring (`b.Events.Add("info", name, "deleted runner registration %s", ...)`, backend.go:1122), which is what the UI's event feed and `ghr events` show.
Fix: one event per manual delete request (with the count and bytes) and one per cleanup pass per repository; the per-artifact lines stay in the log.

F29. Minor. Section 2.1 (inventory pagination).
What is wrong: `getAll` stops at 50 pages and returns `errTooManyPages`, dropping everything collected (client.go:149-155, 478-495). A repository with more than 5,000 live artifacts, which is exactly the one that needs cleaning, can never be inventoried or cleaned.
Fix: either accept that limit and say so on the row, or let the inventory keep the pages read and clean oldest-first from them.

F30. Minor. Section 2.1 (config read during a pass).
What is wrong: a `PATCH /config` that changes an age while a pass runs can be seen by half the repositories.
Fix: a pass reads the config once at its start.

F31. Minor. Section 3.2 (manual routes and the GitHub gate).
What is wrong: every backend method that calls GitHub first checks `githubErr()` (degraded token, rate-limit pause; backend.go:1056-1066); the spec's manual delete and retention routes do not say they do.
Fix: state they go through the same gate and answer 503/429 like the other routes.

F32. Minor. Section 2.3 (ages against the host clock).
What is wrong: age = daemon `now` minus GitHub's `created_at`; a host with a skewed clock shifts every age, in the deleting direction if the host runs fast.
Fix: compare against the `Date` header of the listing response, or note the risk; at the suggested 24h floor it matters less.

F33. Minor. Section 4 (banner on every page).
What is wrong: `projected` and `warn` can be true for most of a month; a non-dismissible banner on every page for weeks will be ignored by the time it says `critical`. The existing banners are for conditions the user can act on once.
Fix: allow dismissing `projected`/`warn` per level per month (local storage), keep `critical`/`exhausted` always on.

F34. Minor. Section 2.4 (thresholds vs the 6-12 h lag).
What is wrong: `used` is up to 12 h stale, and accrual on the heavy days was 5 GB-h per hour (133 GB-h on Oct 5), so `warn` at 70% can appear when real usage is already past 90%.
Fix: compare the thresholds against `used + rate × 12h` (the lag bound), or at least say in the help text that the level trails.

F35. Minor. Scope (goal: prevent CI failing from the quota).
What is wrong: out of the box nothing changes: cleanup is off, retention stays at 90 days, and the quota card only reports. The root cause measured on Oct 13 is a 90-day retention on artifacts that are used within minutes. Setting repository retention to a few days (the lever this spec adds under "Retention + caches") removes most of the exposure with no cleanup pass at all, but the spec never recommends it and the README section only lists permissions.
Fix: a "what to set" paragraph in the README and a hint on the Artifacts page when retention is at the default and the level is above none.

Checked and found consistent with the code (no finding): `ListUserRepos` and the `AvailableRepos` filter (backend.go:1157-1177); `Artifact` gaining `created_at` and `workflow_run{id, head_branch}` (docs confirm the fields); permissions for every new call (docs confirm Actions read/write, Administration read/write for retention, Plan: read for the user billing endpoint, Administration: read at the org); 404-as-success following `DeleteRunner`; the error classification path through `actionsErr`; the shell banner pattern (`border-warning`/`border-destructive` in /root/repositories/ghr/web/src/components/restart-banner.tsx:9, 39, 53) and the tab union in /root/repositories/ghr/web/src/components/shell.tsx:26 needing `/artifacts`; the unconfigured daemon running no passes (run.go:288-291).

VERDICT

Needs another design round, but a short one: §2.3 (protected statuses, the age floor and the cross-run caveat: F1, F2), §2.1/§3.2 (mutation budget and asynchronous bulk deletes: F3, F4, F20) and §3.1/§2.2 (the config types, the alert fields, the binary-GB and rate definitions: F5-F8). Those change behaviour the owner ruled on (the 1h minimum and the synchronous routes), so they need the owner's answer rather than an implementer's. F12 and F13 should be checked against the October usage items before that round. Everything else is an amendment that can land in the same edit; after it the spec is ready for planning.
