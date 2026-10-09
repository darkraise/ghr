# Fable review: the run details spec

**Date:** 2026-10-09
**Spec:** docs/superpowers/specs/2026-10-09-ghr-run-details-design.md
**Reviewer:** dr-superpowers:judge-fable
**Verdict:** needs rework (findings 1 to 3, 8 to 10). The owner approved every disposition below on 2026-10-09 and they were applied to the spec the same day; the raw log cap was later lowered from 64 to 32 MiB in the self-review.

## Checks run after the review

- Findings 1 and 2 confirmed in code: `internal/webui/handler.go:68` refuses any `/api/` request without `X-GHR: 1`; `internal/github/client.go:286-326` reads whole bodies, treats any status of 300 or above as an error, and caches GET bodies that carry an ETag.
- Finding 5: an expired job log answers `410 Gone` (darkraise/flashpoint-web job 62332128361, run from 2026-02-03).
- Finding 12: in "re-run failed jobs" attempts (darkmem run 37498148517, darkrouter run 33861987112) attempt 2 lists every job; jobs not re-run keep attempt 1's times but get new job IDs, and their logs answer 200. `filter=latest` returns attempt 2's jobs. No design change needed.
- Finding 31: the attempt endpoint's `html_url` is the run URL with no attempt in it; none of the sampled runs had artifacts, so the per-attempt artifact question stays unverified.
- Finding 11: no `pull_request_target` or `workflow_run` runs exist in the owner's repositories to test against; unverified.

| # | Severity | Finding | Proposed disposition |
|---|---|---|---|
| 1 | blocker | Raw log link and artifact download cannot carry `X-GHR`; a fetch that follows the artifact 302 gets an opaque cross-origin answer | Artifact route returns JSON `{url}`, the browser navigates to it (also removes the redirect surface, #18); raw log is fetched with `X-GHR` and saved through a Blob object URL |
| 2 | blocker | The new calls cannot use `c.do`: whole-body reads, shared redirect policy, 3xx as error, ETag body cache, rate headers on the discarded 302 | New low-level `request` path (suspension check, headers, `record`, `classify`, unread response) with a no-redirect client; the blob request is a second plain request without `Authorization`; these calls bypass the ETag cache |
| 3 | blocker | The log route has no source for the job's steps or status | Add `GetJob`; the log route calls it first; a job not completed answers 409 "job has not finished" |
| 4 | major | Two `httptest` servers on 127.0.0.1 share a host, so Go forwards `Authorization` | Moot with #2: assert the blob server saw no `Authorization` |
| 5 | major | 410 is not an `ErrKind`; it becomes 502 | Add `ErrGone` for 410 (verified for logs, above) |
| 6 | major | Routes through `respond` show GitHub's raw 403 text, not the named permission | New methods call `b.githubErr()` first and map errors through `actionsErr`'s wording |
| 7 | major | Graph card holding job cards breaks spec 1 §1.7 (no nested cards) | Page blocks become `Section`s; only graph nodes are bordered tiles |
| 8 | major | Annotation fetches under the 20 s deadline cannot "keep running"; per-request concurrency; failures and too-large logs uncounted | Per-job fetches on their own detached 60 s context, route waits at most 20 s; one process-wide limit of 4 log downloads; new `Failed` and `TooLarge` counts shown on the card |
| 9 | major | Memory bounded by entry count: about 200 MB worst case, and a 16 MiB JSON log for the browser | Parse cap 4 MiB; parsed-log cache bounded at 32 MiB total |
| 10 | major | Graph cache contents and error caching undefined; a cached 403 would hide the graph after the permission is granted | Cache the workflow bytes only, build the graph per request; cache 404 for 10 minutes; never cache 403, 429 or 5xx |
| 11 | major | `head_sha` may hold the wrong workflow file for events that run the default branch's copy | Draw the graph only when every API job matches a YAML job; otherwise the timeline with "The workflow file at this commit does not match the run's jobs." |
| 12 | major | Re-run attempts might hold only the re-run jobs | Verified: attempts list every job. Spec states it |
| 13 | major | Run details cache has no size bound | 64 entries, expired entries evicted on insert |
| 14 | major | Raw stream inherits the 30 s whole-request timeout | Moot with #1 (fetched as a Blob through the API); the blob request uses dial and header timeouts and a 64 MiB cap |
| 15 | minor | `StatusIcon` is private and typed to `ActionsRun` | Extract a shared component taking `{status, conclusion}` |
| 16 | minor | Workflow path needs per-segment escaping | Stated |
| 17 | minor | Repository names flow into `Content-Disposition` | Moot with #1; the repository is resolved to its configured spelling for URLs and cache keys |
| 18 | minor | 302 passes GitHub's `Location` unchecked | Moot with #1; the URL must be https on a GitHub or Azure blob host |
| 19 | minor | ANSI renderer safety and control characters undefined | React elements from a tokenizer, no HTML strings; strip C0 except tab, drop non-SGR escapes; for `\r` the last segment wins |
| 20 | minor | "Name without the group prefix" undefined for mid-name expressions | Group rows show the full job name |
| 21 | minor | Annotation links do not carry `attempt` | They carry it |
| 22 | minor | `line` above the 5,000-line cut is not rendered | The page renders the earlier lines when `line` needs them |
| 23 | minor | Non-integer IDs undefined | 400 in the API; the not-found line in the browser |
| 24 | minor | "Chips" reads as pills | Labels as plain mono words separated by gaps |
| 25 | minor | Size alignment and the download control's element unstated | Size right-aligned; Download is a button |
| 26 | minor | Timeline and graph scroll region accessibility | Timeline rows carry durations as text; scroll region focusable with `aria-label="Job graph"`; "{n} more" has `aria-expanded` |
| 27 | minor | Header departs from spec 2's `PageHeader` rule | Use `PageHeader` with the muted line as its description; breadcrumb as on the repository page |
| 28 | minor | Nav active state for nested paths unverified | The plan checks the kit's matching |
| 29 | minor | Commit, PR and workflow links need `owner` | Built from `config.owner` |
| 30 | minor | "latest" and `attempt=N` cache twice | Resolve to the number after `GetRun`, key on it |
| 31 | minor | Artifacts per attempt and the attempt URL unverified | Attempt URL verified (no attempt in it); artifacts listed for the run as GitHub does |
| 32 | minor | Fixtures come from `web/fixtures_test.go` | Stated |
| 33 | minor | Interfaces and fakes not named | Listed: daemon `GitHub`, `fakeGH`, `api.Backend`, `fakeBackend` |
| 34 | minor | Annotation polling stop conditions | Stops when `unread` is 0 and the run is completed |
| 35 | minor | Search opening every matching step can render 200,000 rows | Search opens only the step holding the current match |
| 36 | nit | Cycles and unknown `needs` cannot reach a run | Kept as defensive handling, removed from the named reasons |
| 37 | nit | Job page breadcrumb lacks the job name | Added |
| 38 | nit | 256-colour and true-colour ANSI unneeded | 16 colours and bold only |
| 39 | nit | `Omitted` has string keys in JSON | TypeScript type `Record<string, number>` |
| 40 | nit | The step pointer's no-match case | "Otherwise it stays" |
