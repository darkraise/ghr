# ghr final review: deferred and owner findings (2026-10-04)

Source: whole-branch review of ghr 73672af and homelab e5a23a2 by two reviewers (a Claude judge and Codex), merged and verified against the code by a third. 61 findings were confirmed (0 Critical, 7 Important, 54 Minor) and 2 rejected. Eight were fixed in a final wave (see the ledger and git log). This file lists the rest. Ids are the verifier's: `C` Claude, `X` Codex, `CM` Claude Minor.

## Owner actions (not code)

- **C5 (Important)** Rotate the MaxMind licence key and change the NPMplus admin password if it was ever left at `12345678`. Both were committed earlier in `nginx-proxy-manager-plus/compose.yml` and remain in git history. Rewrite history only if homelab is or will become public.
- **X15 (Important)** The deployed PAT is "all repositories", wider than the spec's exact-repositories rule, and expires 2026-10-11. Replace it with one limited to darkmem, darkcloud, darkagents, ghr-e2e (if kept) with Administration read/write and Actions read, with a longer expiry, then `ghr token set` (the new token check fixes the silent wrong-permission case).
- **C4 (Important, design)** Nothing notices when the runner version on the LXC goes stale. Either schedule a `setup.sh` re-run or have the daemon warn when `DistVersion` is older than the latest actions/runner release. Pin `GHR_VERSION` on routine `setup.sh` re-runs because every master push publishes a release.
- **LXC leftovers:** `/root/.pat`, `/root/ghr.v0.1.0.bak`, `/root/ghr-labelfix`, and the `ghr_lxc` key line in `authorized_keys` (and the key file on the Windows machine).

## Open before first real use: one residual from the fix wave (fr-1, CONFIRMED-GAP)

Fixed 2026-10-04 in ghr aa85036 as described below (test `TestDockerUnreachableKeepsCleaning`).

The fix for C2 (a cleanup that can never succeed holds a repo slot) gives up after 10 consecutive failed Docker cleanups (about five minutes). The scoped re-review found that this also fires when the Docker daemon itself is unreachable: `projects()` fails on `ComposeContainers`, the counter reaches 10, `finish` deletes `ghr-projects` and the instance directory, and the job's containers and volumes then leak permanently because nothing sweeps Docker resources later. The ruling seat confirmed it and named the smallest change, not yet applied because the review procedure allows only one fix wave:

- In `internal/runner/cleanup.go` add a sentinel `errListProjects` and make `projects()` wrap the `ComposeContainers` error with it.
- In `internal/runner/lifecycle.go` (around lines 364-372) raise `ip.cleanupFails` only when `err != nil && !errors.Is(err, errListProjects)`; leave the counter unchanged for that error (no increment, no reset) and keep returning `docker cleanup: ...` so `finish` retries every 30 s.
- Add one test: an instance whose `ComposeContainers` fails more than `maxCleanupFailures` times stays `cleaning`, keeps its `ghr-projects` file and is not deleted from GitHub; keep the existing give-up test (listing succeeds, a removal fails 10 times). Prove it RED then GREEN.

Related minors from the same re-review, not fixed: a failed `ghr-projects` write counts toward the limit after removing nothing; the failure counter is in memory only; `ghr token set` blames Administration or Actions for any list error (including 5xx and rate limits) and never checks Administration write; log archive and chown failures still retry without a limit; the JIT credential remains visible in the process command line (`ps`, `systemctl status`).

## Deferred Minor findings (all confirmed)

Daemon and runner
- X1 Socket probe/remove/listen is not an exclusive lock; shutdown frees the socket before cleanups finish. Fix: flock a lock file for the process lifetime.
- X2 A `systemd-run` error rolls back (deletes dir and registration) without confirming the unit is inactive. Fix: `SD.Stop` before `RemoveAll`.
- X4 Runners registered by v0.1.0 (no system labels) are not seen as stale after upgrade; only `all`-mode warm runners get stuck.
- X6 `ForgetCache` does not invalidate in-flight old-token requests (self-heals in about 60 s).
- X7/CM5 Registrations of a removed repo are never cleaned if the last DELETE fails.
- X8 A corrupt or missing `ghr.json` removes the instance without Docker cleanup.
- X9 Prefix cleanup uses the current config, not the job's. Fix: snapshot prefixes into `Meta`.
- X10 "Restart to switch owners" adopts old-owner runners; `Meta` has no owner.
- X11 History finalisation queries `filter=latest`, so a rerun loses the conclusion. Fix: use the attempt endpoint.
- X14 Timed-out helper commands leave grandchildren running (no process-group kill).
- X16 Adopting a completion-only `job.json` can give a negative duration.
- X17 A failed HTTP `Serve` is only logged.
- X18 Per-unit `Active` and pending-file errors are silent.
- X19 `lastPrune` resets at every start, so retention never runs if restarts are under 24 h apart.
- X20/CM6 Case-sensitive history filter and registration prefix.
- X21 Log read errors are discarded; any `Stat` error reads as unknown runner.
- CM4 Paused repos always show Queued 0.
- CM7 Repeated disk and spawn-failed events can fill the 1000-entry event ring.
- CM9 `ghr.json` is writable by the runner user (written before `chown -R`).
- CM10 Config and token renames lack `fsync`.
- X32 API decoder accepts unknown fields and trailing data.
- X33 `logs` and `history` accept extra arguments; last argument wins.
- X34 `printStatus` discards write and flush errors.
- X13 CLI output is not sanitised against terminal escape sequences.
- X35 `hooks/job-completed.sh` `date` assignment is unguarded under `bash -e`.

TUI
- X23 Quick repeated `+`/`-` lose increments. X24 No ordering guard on overlapping status responses. X25 Selection kept by index, not identity. X26 Config, event and log fetch errors are not shown. X27 Details dialog has no height limit or scrolling. X28 Overflow on small terminals. X29/CM1 Sanitising incomplete; newlines, tabs and bidi characters survive. X30/CM16 Log trim can split a UTF-8 character. X31 Config prompt cannot pin an inherited value. CM2 5 s timeout on actions. CM3 OSC 52 clipboard write bypasses Bubble Tea.

Installer, CI and docs
- X37 Readiness probe honours an exported `GHR_SOCKET`. X38 Temp download directory leaks on failure. CM11 Every master push publishes a release (consider path filters). CM13 `docker.list` is written even when `docker.sources` exists (duplicate apt source seen on the LXC). CM14 `.gitattributes` pins only `*.sh` to LF, so `config.example.yaml` reaches the LXC with CRLF (it parses). CM15 `e2e-waiting.yml` is still in the public repo and references the deleted environment; delete it. X46 Existing NPMplus deployments fail compose until `.env` is filled in; document the upgrade.

Tests that cannot fail or are missing
- X39 ETag tests pass without conditional requests. X40 History conclusion-filter assertion cannot fail. X41 `Store.Update` test never reopens the store and no test asserts file modes. X42 `setup_test.sh` discards the third `install_runner` exit status. X43 TUI golden clock masking is time-zone dependent. X44 API mutation tests do not check pause-all values, `GlobalMax` or labels.

## Rejected

- X5 Token swap leaves backoff deadlines: harmless, the replacement shares the same per-user limit and per-repo backoff is capped at 2 minutes.
- CM8 Idle runner holds a global slot until `idle_timeout` in queue mode: this is the specified behaviour.

## Earlier rulings the verifier judged wrong

- Task 14 pc-1 (cleanup failures only produce warnings): the instance holds its repo slot forever. Fixed in the final wave (C2).
- Task 8 pc-2 (credential not echoed by `systemd-run`): the default unit description is the full `--jitconfig` command line. Fixed in the final wave (C6).
