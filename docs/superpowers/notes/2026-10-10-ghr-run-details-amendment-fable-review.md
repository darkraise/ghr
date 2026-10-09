# Fable review: the run details spec amendment for register rows 15, 21 and 32

**Date:** 2026-10-10
**Spec:** docs/superpowers/specs/2026-10-09-ghr-run-details-design.md (amendment commit dcb4e3d)
**Reviewer:** dr-superpowers:judge-fable
**Verdict:** rework. The owner approved every disposition below on 2026-10-10 and they were applied to the spec the same day; findings 1 and 8 were reversed later that day (see the first check below), with the owner's approval.

## Checks run after the review

- Finding 1 refuted on 2026-10-10 after the plan's Codex review: `startBackgroundRead` clears the read deadline before the handler runs (`/usr/local/go/src/net/http/server.go:697`, called at `:2060-2062` for a request without a body), and a probe server with `ReadTimeout: 1s` kept a GET handler's context live for 3 s and answered 200. Neither ghr server sets a base context, and `Shutdown` does not cancel handler contexts, so a request's context ends only when its client disconnects. An earlier reading of `:729-733` and `:769-773` had wrongly confirmed the finding.
- Finding 4's premise that nginx-proxy-manager speaks HTTP/1.0 upstream without the websockets toggle is unverified; the disposition removes the dependency either way.

| # | Severity | Finding | Disposition |
|---|---|---|---|
| 1 | high | `ReadTimeout` 30 s cancels request contexts at 30 s, so the 50 s waits never hold on the web listener; the log routes answer 500 "context canceled" today | Refuted: no change. `ReadTimeout` stays 30 s, no 504 mapping, row 15 stays n/a |
| 2 | medium | `RawJobLog` leaks the slot on its own error returns once the slot moves into the reader | `RawJobLog` releases slot, body and deadline on every error return, panics included |
| 3 | medium | `Close` not idempotent; a second release blocks or raises the limit to 5 | `Close` safe to call more than once |
| 4 | medium | A body without a length is close-delimited, so an abort looks complete through an HTTP/1.0 proxy | No length declared: buffer up to 32 MiB and send with `Content-Length`; stream only declared lengths |
| 5 | medium | Spec did not say whether the daemon or the route owns the 32 MiB cap and the 413 | The daemon owns both; the API test checks only error-before-body, 502 and abort |
| 6 | medium | A raw request that gets its slot late has seconds left for the copy | Slot within 25 s of the request's start, otherwise 503 |
| 7 | low | The deadline does not bound a write blocked on a stalled client; the unix socket has no `WriteTimeout` | Route sets its write deadline with `http.ResponseController`; abort on page unmount not added |
| 8 | low | Row 15's stated reason is wrong; the waiter is safe only by a 10 s margin | Refuted with 1: the original reason holds |
| 9 | low | Spec claims a 30 s idle deadline on blob reads that the code lacks | Claim deleted; the request deadlines bound the read |
| 10 | low | A copy error on the first read would drop the connection instead of answering a status | First chunk read before headers; such an error answers 502 |
| 11 | low | Busy has no home in `downloadLog` or the annotations tally; the slot flag the log route needs is unspecified | Spec names both: no miss, call removed, a channel closed when the slot is taken |
| 12 | low | Spec did not list what the change removes | Removal list added under §2.1 |
| 13 | low | HEAD on the raw route streams the log into nothing | Accepted as is; noted in §2.1 |
| 14 | low | Missing tests for the amendment's behaviour | Added to §5 |
| 15 | low | Three separate 50 s values must stay aligned | One constant, `logRequestWindow` |
