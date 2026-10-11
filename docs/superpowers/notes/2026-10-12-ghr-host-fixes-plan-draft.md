# Host fixes plan draft

**Date:** 2026-10-12
**Spec:** docs/superpowers/specs/2026-10-12-ghr-host-fixes-design.md (approved by the owner on 2026-10-12)
**Register:** docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md (rows 47 to 74, 186, 199, 203, 205 to 208, 210 to 214; sub-project 3 of 6)
**Target plan:** docs/superpowers/plans/2026-10-12-ghr-host-fixes.md (not written yet)

The plan is drafted the way the two earlier plans were: each task is carried out red-then-green in a worktree first, and its text is written from what was done. Five drafters did this, one per group of packages that share no file. Their task texts are the fragments below. What is left is to assemble, score, lint and review the plan.

## Fragments and worktrees

Fragments are kept in `.superpowers/plan-draft/host-fixes/` in the primary checkout (untracked): `BRIEF.md` (the rules every drafter followed), `header-top.md` (the plan header down to the Spec line) and one `chain-N.md` per drafter. Each fragment holds its tasks in the plan's task format, then Contracts, Assumptions and Notes for the assembler with the drafter's proposed scores.

| Chain | Tasks | Rows | Files it owns | Worktree branch |
|---|---|---|---|---|
| 1 web server and CLI | 1 to 6 | 47, 50, 51, 48, 54, 55, 56 | `internal/webui`, `internal/api`, `cmd/ghr` | `worktree-agent-a195375cd8b6d9062` |
| 2 config | 7 to 9 | 57, 58, 59, 206 | `internal/config` | `worktree-agent-a100dbedb589e34d3` |
| 3 toolchains and storage | 10 to 16 | 60, 61, 62, 64, 65, 66 | `internal/toolchain`, `internal/storage`, `download` in `internal/system` | `worktree-agent-ab69eab930b98d827` |
| 4 installer, CI, docs | 17 to 21 | 67 to 74 | `deploy/`, `.github/workflows/ci.yml`, `README.md` | `worktree-agent-adc0b20048c043f79` |
| 5 daemon and runner | 22 to 31 | 49, 208, 53, 210, 213, 203, 214, 211, 207, 186, 205 | `internal/daemon`, `internal/runner`, `internal/metrics`, the `Run` type in `internal/github` | `worktree-agent-a00aadc4fa2fbf449` |

Worktrees are under `.claude/worktrees/agent-<id>` in the primary checkout. Each branch has one commit per task on top of `master`. The status of each chain when this note was last updated is in the section "Chain status" at the end.

Task titles: 1 two-tier login lockout; 2 auth request handling; 3 per-call timeouts and clearer CLI failures; 4 the kill route's write deadline; 5 token prompt and argument checks; 6 Ctrl-C at a password prompt; 7 label rules; 8 an empty config file; 9 saving through a symlink, with a sync; 10 downloads only from the expected place; 11 no redirect to plain http, no tarball owners; 12 a failed Python install; 13 Go file names before 1.21; 14 removing a Java folder with `+`; 15 the `.tmp` directory; 16 hard links counted once; 17 Docker's apt key; 18 the service unit comes back with Docker; 19 the web password after a failed first install; 20 old runner versions, the version prefix and the closing hint; 21 CI permissions and pins, README; 22 a failed save is a 500; 23 a reload repeats no warning; 24 a connection limit on the web listener; 25 a panic in a token check; 26 a log that is not there yet; 27 root reads of job-writable files; 28 a repository that turned public; 29 adding a repository hands over what it read; 30 the retention event and the `projects()` comment; 31 one rollup per hour.

## Spec amendments owed

A background security check flagged four drafts while they were written. Each was tightened by a message to its drafter. The spec still has the earlier wording and must be amended to match the task texts, and the owner told what changed:

- **§5.1 (row 67, Task 17):** the script does not install the downloaded key file. It imports the download into a throwaway keyring, requires the pinned fingerprint `9DC858229FC7DD38854AE2D88D81803C0EBFCD88` there, and installs only gpg's own armored export of that key. Reason: apt trusts every key in a `Signed-By` file and may read a crafted file differently from gpg.
- **§4.1 (row 60, Task 10):** downloads are pinned to the release path, not only the host: `github.com/actions/node-versions/releases/download/`, `github.com/actions/python-versions/releases/download/`, `github.com/adoptium/temurin<N>-binaries/releases/download/` (checked against the live lists on 2026-10-12). URLs are compared as parsed fields, with credentials, ports, look-alike hosts and `..` segments refused; every download goes through one function on `Env`; the strict check is the default and a test says so. Redirect hosts stay unpinned on purpose (the first URL is authenticated by TLS, and GitHub has changed its asset host before).
- **§1.5 (row 53, Task 24):** besides the global cap of 256, at most 64 connections per client (IPv4 address, or IPv6 /64), computed inside `internal/daemon`; an over-limit connection is closed at once and never costs another client a slot. Behind a reverse proxy all browsers share the proxy's allowance, the same caveat as register row 226.
- **§1.1 (row 47, Task 1):** no design change. The move to 5 per address and 50 per /64 is the owner's ruling. The drafter was told to pin eight invariants with tests (IPv4 unchanged, a mapped IPv4 address is IPv4, at most 50 password checks per window per /64, a success never clears the /64, both keys checked after waiting for a derivation slot, the sweep cannot shorten a lock, same window and lockout, the later retry time is returned).

- **§5.6 (row 73, Task 21):** flagged as "unverified action SHA pin". No design change, but each pinned SHA in `.github/workflows/ci.yml` must be checked by the assembler: `gh api repos/actions/<name>/git/ref/tags/<tag>` (dereference an annotated tag with `gh api repos/actions/<name>/git/tags/<sha>`), and `gh api repos/actions/<name>/commits/<sha>` must answer from the `actions/<name>` repository itself. Put the commands and their output in the plan's Assumptions.

The key check in `deploy/setup.sh` was flagged four times, on successive commits; the later flags may be about commits made before the drafter applied the re-export design. Read the final function.

Every flag arrived as a title with no detail. Read the four pieces of code yourself when assembling (the lockout, the download check, the listener, the key check) rather than trusting that the messages were followed.

## Open question for the owner

- **A config whose only labels are system labels no longer starts** (found by the config drafter). Example: `labels: [linux]` and a repository with no labels of its own. Today it loads. With Task 7 the loader drops `linux`, and the existing rule `needs at least one label in labels or repo labels` stops the daemon. This contradicts the spec's line that a file that loaded yesterday must not stop the daemon. Asked on 2026-10-12, no answer yet. Default taken: keep the refusal and record it in spec §3.1 as an accepted consequence. If the owner wants it to load with a warning, Task 7 changes.

## Decisions taken while drafting

- Row 205 is fixed in `metrics.Load` (drop a rollup dated at or after the current UTC hour), not at hour close. The owner was told.
- Row 57: the config drafter uses `labels: "docker" is listed twice; ignored` for a repeated label, because "not a valid label" is wrong for a repeat. The other `Validate` texts it chose are `must not be empty`, `must be at most 100 characters`, `is listed twice`. Keep them unless the owner objects.
- GitHub's maximum label length is unverified and cannot be checked read-only; the rule keeps 100 (spec §7).
- Rows 68 and 70 are verified (spec §7).
- The installer drafter's worktree was created from `origin/master` (`f5cd90d`), far behind local `master`. It was told to rebase onto local `master` and rewrite Task 17 from the rebased file. Check `git merge-base --is-ancestor master <branch>` for every chain before trusting its quoted lines.

## What is left

1. Confirm each chain finished: its fragment exists, its branch has one commit per task, and its Notes report a green whole-tree run.
2. Merge the five branches into one scratch branch from `master` (they share no file) and run `go vet ./...`, `go test -race -count=1 ./...` and `bash deploy/tests/setup_test.sh` once, with `TMPDIR` and `GOTMPDIR` on the root disk (`/root/.cache/ghr-plan-tmp`). Go is at `/usr/local/go/bin`.
3. Amend the spec as listed above, in one `docs(specs):` commit, and tell the owner.
4. Assemble the plan: `header-top.md`, then Execution, Parallelism (`sequential`, see the spec's Execution strategy line), Global Constraints (copy from `docs/superpowers/plans/2026-10-11-ghr-daemon-fixes.md`, minus its web lines, plus: `TMPDIR`/`GOTMPDIR` on the root disk for every Go command; `bash deploy/tests/setup_test.sh` for installer tasks and never `deploy/setup.sh` itself; no file under `web/`), Contracts and Assumptions merged from the fragments, the task index, then the task blocks without each fragment's `### Tasks` line.
5. Score every task against `reference/ladder.md` (the drafters' proposals are in their Notes), apply Rule S, and write the Implementer and Evaluation lines.
6. The Codex executor lane is available (`codex-gate usable=true lane=true`, 2026-10-12). The lane question is owed to the owner once per plan, as the timed plain-text question in `reference/executor-lane.md` §Planning, after scoring and before any assignment line is written.
7. Set the register rows to `planned` with the plan's path and an acceptance each, and put `**Items:**` on the tasks (the fragments already carry them).
8. Self-review, `scripts/plan-lint`, the plan review rounds, the `**Plan review:**` line, then the execution handoff.
9. When the plan is saved and the branches are no longer needed, remove the five worktrees and their branches.

## Chain status

- Chain 2 (config): finished. Commits `fc9fe6f`, `9d7276c`, `3ae8a44` on `8cf8ea2`; whole-tree `go vet` and `go test` green; only `internal/config/config.go` and its test touched.
- Chain 4 (installer, CI, docs): finished. Rebased onto local `master` (`76066ff`). Commits `fe7ab38`, `597d33e`, `c916664`, `9f041ae`, `2d1575f`, `991dbc9`, `3831560`, `63bf7e4`: Task 17 has three commits because its design changed twice (the fragment describes the final export design of `991dbc9`), and Task 21 is two commits (CI, README). `bash deploy/tests/setup_test.sh` and the whole Go tree green; the drafter replayed every task's edits onto a copy of master's files and got the worktree's bytes. `shellcheck` is not installed on this host.
  - Its notes raise three points to settle at assembly: the comment on `stop_ghr` was changed because it named `gc_dist`'s old position as the reason for dropping a queued runner update (whether the drop is still needed is not checked); `web-password-pending` stays when the password is set in the browser, so a later run at a terminal asks again; the "exactly one `pub` record" rule was dropped as unnecessary once only the export is installed.
- Chain 1 (web server and CLI): finished. One commit per task on `8cf8ea2`: `cbc3f32` (1), `504a19a` (2), `8e11467` (3), `68b3a17` (4), `6156a08` (5), `d91020a` (6). Whole-tree `go vet` and `go test` green. The fragment's code blocks were extracted from the commits by script and each quoted block checked against the commit before its task. Its "Lockout invariants" section maps the eight points to tests; the review led to one code change (an IPv4-mapped address gets no /64 key).
  - To settle at assembly: the `Allow` header for `/auth/state` is `GET, HEAD` (Go's router serves HEAD there), where the spec says "the path's method"; `interrupted` prints as `ghr: interrupted` for any cancelled context, so it also replaces `ghr: context canceled` when `ghr setup github` is interrupted; `socketCall` in `internal/daemon/run_test.go` relied on the client's 30-second timeout that Task 3 removes (chain 5 owns that file: give that helper its own timeout in a chain 5 task, or a small extra task); the terminal restore after Ctrl-C is proven only as "restore is called".
  - Adjacent findings it left alone, to add as register rows: `ghr repo rm -h` and `ghr logs -h` still treat `-h` as a name; the socket password route answers an oversized body with 400 while `/auth` now answers 413.
  - Known limit to state in the plan: a login already deriving its key when a lock lands finishes that derivation and its result is discarded, as with the per-address limit today.

## Incident: a drafter's test run wrote to this host

While running tests red-first, the installer drafter's old code ran against real paths on the dev host, before the task text made those paths variables:

- It created `/etc/systemd/system/ghr.service`. It removed the file; the unit was never loaded or enabled. Checked on 2026-10-12: the file is absent and no `ghr*` unit file is listed.
- It overwrote `/etc/apt/keyrings/docker.asc`, which this host's Docker apt source uses. It restored the file from a download it had fingerprint-checked. Checked on 2026-10-12: the file holds one key with the primary fingerprint `9DC858229FC7DD38854AE2D88D81803C0EBFCD88`, 3817 bytes. The original bytes are not known. The owner may want to run `apt-get update` once to see the Docker repository verify.

For the plan: Tasks 17 and 18 must make the keyring directory and the unit path variables in their FIRST step, before any test runs, and the Global Constraints must say that a test run may never write under `/etc`. Read both task texts for this at assembly.
