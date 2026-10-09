# ghr run details

**Source:** owner request in chat, 2026-10-09, plus the scope chosen in brainstorming
**Covers:** docs/superpowers/specs/2026-10-09-ghr-run-details-design.md

| # | Item | Assigned | Acceptance | State | Note |
|---|---|---|---|---|---|
| 1 | Action details view same as github | docs/superpowers/plans/2026-10-09-ghr-run-details.md | Run and job pages under /actions/{repo}/runs/{id} show summary, rail, header and errors per spec §1; Actions page links to them | done | - |
| 2 | graph | docs/superpowers/plans/2026-10-09-ghr-run-details.md | Graph from the workflow file at head_sha, or a jobs timeline with the §3.4 note | done | - |
| 3 | annotations | docs/superpowers/plans/2026-10-09-ghr-run-details.md | Annotations from job logs with counts, status lines and log-line links (spec §1.3, §4.3) | done | - |
| 4 | Job logs | docs/superpowers/plans/2026-10-09-ghr-run-details.md | Job page step logs with groups, ANSI, search, 5,000-line window and raw log save (spec §1.4-§1.5, §4) | done | - |
| 5 | Artifacts | docs/superpowers/plans/2026-10-09-ghr-run-details.md | Artifacts table with size, expiry and a Download button through a checked GitHub URL (spec §1.3, §2.1) | done | - |
| 6 | Attempts | docs/superpowers/plans/2026-10-09-ghr-run-details.md | Attempt picker; any attempt's jobs, annotations and logs (spec §1.2, §2.4) | done | - |
| 7 | Run details Task 7: ghrJobs repeats ghrRuns' walk | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 8 | Run details Task 8: logCache.misses never pruned | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 9 | Run details Task 2: redirectURL ignores the io.Copy drain error (download.go:62) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 10 | Run details Task 2: JobLog/ArtifactURL panic on a Client built without New, because only New sets Blob | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 11 | Run details Task 2: redirectURL accepts only 302, not 301/307/308 (matches the brief and GitHub's documented status) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 12 | Run details Task 6: workflowFile has no single-flight, so concurrent callers for an uncached file each call GitHub (the plan's Assumptions say the details fetch is already shared) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 13 | Run details Task 6: rungraph_test.go setup calls ignore runGraph errors; no test for a non-ErrMismatch Build error (noteInvalid) or for 5xx not being cached | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 14 | Run details Task 6: newLRU with maxEntries <= 0 drops every entry rather than meaning no limit; Tasks 8 and 9 reuse lru | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 15 | Run details Task 7: a waiter on another caller's fetch returns raw ctx.Err(), which respond maps to 500 instead of 504 (rundetail.go:253-254) | - | - | open | - |
| 16 | Run details Task 7: evictOldest breaks ties by map order, so equal timestamps can evict a sibling key just stored (rundetail.go:297-306) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 17 | Run details Task 7: the latest attempt is stored under two keys, so 64 entries hold about 32 runs (rundetail.go:278-281) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 18 | Run details Task 7: ghrJobs reads the whole history on every uncached fetch (rundetail.go:186), mirroring ghrRuns (parked in preflight) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 19 | Run details Task 7: ghr is marked per job name, so a hosted attempt-1 job shares the flag with attempt 2 (documented limitation, rundetail.go:180-182) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 20 | Run details Task 7: the shared-fetch test can pass from cache if the second goroutine runs late (rundetail_test.go:619-644) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 21 | Run details Task 8: queued log downloads wait for a slot with no deadline; a caller timing out while queued is told "GitHub is still sending this log" (joblogs.go:121, :210) | - | - | open | - |
| 22 | Run details Task 8: the parsed-log cache is sized by raw bytes, not parsed memory (joblogs.go:91, :145; documented at :26-27) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 23 | Run details Task 8: misses map holds failed outcomes that JobLog never reads (parked in preflight); no test for the raw route's generic-failure path or for an uncovered repo while the token is degraded | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 24 | Run details Task 8: hideURL matches Go's "failed to parse Location header" prefix (download.go:110); a Go release that rewords it re-opens the leak, and the malformed subtest fails first as the tripwire | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 25 | Run details Task 8: hideURL's "log download failed" wording also covers the API redirect request | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 26 | Run details Task 9: a failed log download counts as Failed for the whole 10-minute retry window while the job log route retries at once (annotations.go:46-49; plan-mandated by the logMiss comment and TestRunAnnotationsCountsLogOutcomes) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 27 | Run details Task 9: a cancelled request returns a partial answer with a nil error instead of ctx.Err() (annotations.go:62-63, 104) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 28 | Run details Task 9: no test for a context cancelled during the wait, for attempt > 0 through this route, or for joining a download the job log route started | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 29 | Run details Task 10: the download host test still lacks a daemon-level 504 case and a malformed-Location case | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 30 | Run details Task 10: the allowlist accepts any subdomain of the two suffixes (plan-mandated by the Contracts) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 31 | Run details Task 10: hideURL's "artifact download failed" wording also covers the API redirect request | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 32 | Run details Final review: RawJobLog copies up to 32 MiB to a string and JSON-escapes ESC bytes, so one response can hold 3 to 4 times its size (joblogs.go:252); the fix is a streaming response | - | - | open | - |
| 33 | Run details Final review: the cache-fill section of RunDetail and the post-download section of downloadLog still run unprotected by recover (rundetail.go:219-234, joblogs.go:107-119); they only touch in-memory maps | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 34 | Run details Final review: rowsFrom groups the whole step on every render of an open step (log-viewer.tsx), O(total lines) per search keystroke | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
| 35 | Run details Final review: the duplicate-job-key check also refuses two merge keys in a jobs mapping (wfgraph.go:504) | - | - | deferred | Deferred minor from the run-details plan ledger; no failure reaches a user today |
