# ghr web UI part 1 (server): open findings (2026-10-06)

Source: the execution ledger of `docs/superpowers/plans/2026-10-05-ghr-web-ui-1-server.md`. ghr `feat/web-server` was merged into `master` at `9216e09`. Every task review passed; a whole-branch review (one Claude judge, Codex off for quota) found 0 Critical and 1 Important, and a four-finding fix wave landed in `9216e09`. This file lists what was left open.

## Owner decisions: a spec revision settles all three

- **Audit events (Important, parked by the ruling seat).** Setting the first password, changing it and a 5-failure lockout leave no trace in `/events` or the journal. The spec has no such rule. Smallest version: a `warn` event for each, through an optional setter on `Auth` so the C3 and C5 signatures stay unchanged.
- **Change-password throttle (Task 3, parked).** `POST /auth/password` does not count or throttle a wrong `current` password. The spec throttles logins only. Fix after a spec revision adds a 429: send the route through the per-client admit and record path that login uses, in Task 5's handler.
- **Raw 500 text (Task 5, parked).** `handler.fail`'s default branch returns `err.Error()`, so `/auth/setup` can echo an OS write error with the password-file path while setup is open. Fix: a generic body plus a logging hook (same hook as the audit events).

## Before or with part 2

- Frontend scaffold needs its own `node_modules/` ignore: the root `.gitignore` is now `/dist/` (Task 8), so a nested `dist/` is no longer ignored.
- Task 4 marks every file under `assets/` immutable and answers a missing path (even a missing hashed asset) with `index.html` and status 200. Confirm the Vite build puts nothing unhashed under `assets/`; consider a 404 for missing `assets/` paths.
- README or config docs: remove the `web:` block before downgrading ghr, because older binaries parse config strictly and refuse to start.
- Plan Assumptions line 102 ("waiting for a derivation slot does not watch the request context") is stale for login after the fix wave; it still holds for setup and change-password.
- CI: the first run of `go test -race` covers `TestWriteHashReplacesAtomically` (skips on Windows), the Auth race tests (they use 50 ms sleeps, run them with `-count=3` once), and the steps-cache tests.

## Deferred minors by task (all low severity)

- **Task 1:** the `web.hosts` error does not name the offending entry; the host part of `web.listen` is validated only at bind; a trailing-dot FQDN is rejected; the clone test does not prove a deep copy.
- **Task 2:** `writeHash` fails if `/etc/ghr` does not exist (it always exists in an install); `newHash` accepts iterations that `parseHash` rejects; test robustness (nil `fi.Mode()` on a Stat failure, an ignored `WriteFile` error, a reader goroutine that outlives `t.Fatal`).
- **Task 3:** `ChangePassword` does not check that `keep` is a live session (the handler always passes the authenticated cookie); the `fails` map has no size cap; a login racing a password change can report the correct new password as wrong once; `readHash` runs under `a.mu` in the generation-mismatch branch; `Setup` can leave a password set but return an error if the session random read fails; two race tests can pass without exercising the race; some test errors are ignored.
- **Task 4:** `fs.ReadFile` copies each file per request; any `index.html` read error is reported as "not built"; no `Content-Type` assertion on the fallback; no 405 header check.
- **Task 5:** `GET /api` (no trailing slash) gets a bare 307 without the `X-GHR` check or `Cache-Control: no-store`; logout with a stale cookie returns 401 and leaves the cookie; no tests for a bare `[::1]` host, an empty `Host`, an empty `hosts` list or uppercase `X-Forwarded-Proto`.
- **Task 6:** a panic inside `fetchSteps` would leave its in-flight entry and block that runner's later callers (fix: a `defer`); the caller that starts a fetch ignores its own cancellation for up to 30 s; the 50 ms sleep in the shared-fetch test makes the joining path likely, not certain.
- **Task 7:** `slices.Equal` is order-sensitive, so reordering `web.hosts` warns; `TestPatchCannotChangeWeb` asserts only `Listen`.
- **Task 9:** the 500 body of a failed reset carries the password-file path (socket-local); ignored `Encode` and `NewRequest` errors in the test.
- **Task 10:** only the 204 path of `ResetWebPassword` is tested.
- **Task 11:** if `fs.Sub` failed, `Run` would return without closing the manager (cannot happen with the embedded literal; fix is to compute it right after the bind); `freeAddr` has a small port-reuse window; ignored setup errors in the tests.
- **Fix wave:** a cancelled login answers 500 `{"error":"context canceled"}` through the same raw-500 branch; the two new tests leak a blocked goroutine only when they fail.

## Discovered by implementers

- No directory fsync after the password-file rename was reported in Task 2 and fixed in the fix wave; nothing else outstanding beyond the items above.
