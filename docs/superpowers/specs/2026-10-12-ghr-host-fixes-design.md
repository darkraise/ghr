# ghr: host fixes

**Date:** 2026-10-12
**Status:** design presented in chat and approved by the owner on 2026-10-12, with the rulings on public visibility (row 214), download integrity (rows 60 and 67), the login lockout (row 47), the unscheduled rows, the packaging and the execution strategy made the same day. Amended the same day while the plan was drafted (§1.2, §1.4, §1.5, §3.1, §4.1, §4.4, §4.5, §4.6, §5.1 and §6.4): three checks were tightened after a security review of the drafts, and the rest records what the code had to decide. The owner was told of each.
**Execution strategy:** sequential — the fixes are small and split by package, but the security parts (lockout, root-owned toolchain paths, root reads of job-writable files) must run alone anyway, and the dev host is the limit: `/tmp` is a nearly full tmpfs and both test suites time out under load (register rows 224 and 225).
**Register:** docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md (rows 47 to 74, 186, 199, 203, 205 to 208 and 210 to 214). This is sub-project 3 of 6.
**Code:** `internal/webui`, `internal/api`, `cmd/ghr`, `internal/config`, `internal/toolchain`, `internal/storage`, `internal/system` (the download client), `internal/daemon`, `internal/runner`, `internal/github` (the `Run` type), `internal/metrics`, `deploy/setup.sh` with `deploy/tests/setup_test.sh`, `.github/workflows/ci.yml`, `README.md`. No web file changes.

## Goal

The review of build `forge-ui-930381d` found no critical or high defect in the web server, the CLI, the config loader, the toolchain installers, the installer script or CI. It found a login lockout one LAN host can hold against another, commands that report "daemon unreachable" while the daemon is working, root-owned code paths that trust what a job can write, downloads that are not pinned to their host, and a set of small inconsistencies. This spec closes those, together with the leftovers of sub-projects 1 and 2 that touch the same packages.

## Decisions

- **Targeted fixes.** Nothing is restructured.
- **A repository that turned public is blocked from the poll that shows it** (owner's ruling, 2026-10-12). GitHub's runs listing already says whether the repository is private, so no call is added and the 6-hour read stays as a backstop.
- **Downloads are pinned to their host, not to a checksum** (owner's ruling, 2026-10-12). Node, Python and `dotnet-install.sh` have no checksum published anywhere but the origin they are downloaded from, and the ghr tarball's checksum ships in the release that holds the tarball. A checksum from the same origin detects a damaged download and nothing else. The official `setup-node`, `setup-python` and `setup-dotnet` actions rely on TLS to the same hosts. Go and Java keep the checksums they already have.
- **The login lockout has two tiers for IPv6** (owner's ruling, 2026-10-12): the exact address, and its /64 at a higher count.
- **An invalid label in `config.yaml` is dropped with a warning; one sent through the API is refused.** This follows the owner's ruling of 2026-10-11 on the runner cap: a file that loaded yesterday must not stop the daemon after an upgrade.
- **The eight rows found while planning sub-project 2 belong here** (owner's ruling, 2026-10-12): rows 205 to 208 and 210 to 213.

## 1. Web server

### 1.1 Login lockout (row 47)

Today a failed login is counted under `ClientKey`: the IPv4 address, or the IPv6 /64. Five failures within 15 minutes lock that key for 15 minutes. On a LAN every IPv6 host shares one /64, so one host's wrong passwords lock out all of them.

- An IPv4 client is counted and locked by its address, as today.
- An IPv6 client is counted under two keys: its exact address and its /64. The address locks after 5 failures within `failWindow`, the /64 after 50. Both use today's 15-minute window and 15-minute lockout.
- A login is refused while either key is locked, with the later of the two retry times.
- A successful login clears the address's failures and leaves the /64's.
- A host that changes its address inside the /64 for every attempt is held to 50 attempts per window. While the /64 is locked no failure is recorded, so the failure table stays bounded.
- An IPv4 address written in IPv6 form (`::ffff:a.b.c.d`) is that IPv4 client and has no /64.
- Known limits: a login already checking its password when a lock lands finishes that check and its result is discarded, as with today's limit; and a client that owns several /64s gets 50 attempts per window in each.

Not closed here: behind a reverse proxy every client reaches ghr from the proxy's address, so they all share one key. ghr reads no `X-Forwarded-For`. This is its own register row and needs an owner ruling on which proxies to trust.

### 1.2 Slow stops (row 48)

`Manager.Kill` runs `systemctl stop` on the runner's unit and waits for it. Runner units set no stop timeout, so systemd's default of 90 seconds applies. The CLI's HTTP client gives up after 30 seconds and the web listener cuts a response after 60, and both report a failure while the stop continues. The CLI also prints `ghr daemon unreachable` for any transport error, including Ctrl-C.

- `api.Client` drops its client-wide 30-second timeout for a per-call one: 30 seconds by default, 100 seconds for `Kill`.
- The CLI tells three failures apart. Ctrl-C prints `interrupted`, for any command that ends because its context was cancelled, so also for `ghr setup github` interrupted while it waits. A call that ran out of time prints `ghr daemon did not answer within <n>s; the request may still be running`. Anything else keeps `ghr daemon unreachable: <error>`.
- The kill route sets its own write deadline of 100 seconds through `http.ResponseController`, so the web listener's 60-second `WriteTimeout` does not cut it. On the socket server, which has no write timeout, the call changes nothing.

### 1.3 Save failures (row 49)

`Backend.update` turns every error that is not an `*api.Error` into a 400 carrying the error's text, so a full disk reads as a bad request and shows the OS error to the browser.

- `Store.Update` wraps a failure of `config.Save` in an exported error type. `Backend.update` answers it with 500 `could not save config.yaml` and logs the wrapped error. A validation error stays a 400 with its text.

### 1.4 Request handling (rows 50 and 51)

- `authBodyLimit` becomes 16 KiB, the limit the socket route uses. Two 1,024-byte passwords in JSON, with every byte escaped, do not fit in 4 KiB.
- A request to a known `/auth` path with another method answers 405 `method not allowed` with an `Allow` header naming the path's method: `POST` for the four POST paths, and `GET, HEAD` for `/auth/state`, because Go's router serves `HEAD` there. An unknown `/auth` path still answers 404.
- A body over the limit answers 413 `request body too large`. Any other body defect stays 400.

### 1.5 Connection limit (row 53)

The web listener accepts without limit, and every open connection holds a file descriptor of the process that also starts runners.

- The web listener is wrapped so that at most 256 connections are open at once. Further clients wait in the kernel's accept queue. The wrapper lives in `internal/daemon` and adds no dependency. The Unix socket is not limited.
- One client holds at most 64 of them. A client is an IPv4 address, or an IPv6 /64. A connection from a client already at 64 is closed at once and costs no other client a slot. Without this bound one LAN host could hold every slot.
- Known limits: behind a reverse proxy every browser reaches ghr from the proxy's address and shares its 64, the caveat register row 226 records for the lockout; every IPv6 host of one /64 shares 64; a host with four IPv4 addresses can still fill all 256; and a client that sends one cheap request every two minutes can probably keep a connection open past the idle timeout (inferred from how keep-alive works, not measured).

## 2. CLI

### 2.1 Token prompt (row 54)

- `ghr token set` with a terminal on stdin reads it with `readToken`, the no-echo prompt `ghr setup github` uses. With a pipe on stdin it reads stdin to the end, as today.

### 2.2 Arguments (row 55)

- `ghr repo add` parses its flags before it takes the name, so `ghr repo add -h` prints the usage. A name that begins with `-` is a usage error.
- `ghr status` with any further argument is a usage error.

### 2.3 Ctrl-C at a password prompt (row 56)

`askPassword` checks its context only after the blocked read returns, so Ctrl-C takes effect at the next Enter.

- The read runs on its own goroutine. When the context ends first, `askPassword` restores the terminal state it saved before the read, ends the prompt's line and returns the context's error. The blocked read is left behind; the process is exiting.

## 3. Config

### 3.1 Labels (row 57)

`CustomLabels` already trims, lower-cases and removes duplicates when labels are used, but nothing checks what a label holds, and a system label written as a custom one is sent to GitHub twice.

A label is **valid** when, after trimming, it has 1 to 100 characters, holds no comma and no whitespace, and is not one of the system labels `self-hosted`, `linux` and `x64`. A list is valid when every label is valid and no two are equal ignoring case. The rule covers the global `labels` and every repository's `labels`, each list on its own: a repository label that repeats a global one is allowed, and `CustomLabels` merges them as today.

- `Validate` refuses an invalid list, naming the first offender, for example `labels: "a b" must not contain spaces or commas` or `<repo>: labels: "linux" is added by ghr`. Only an API change reaches this: PATCH and add repository answer 400.
- `Parse` repairs the lists before it validates, as it does for the runner cap: an invalid or repeated label is dropped and a warning says so, `labels: "a b" is not a valid label; ignored` for an invalid one and `labels: "docker" is listed twice; ignored` for a repeat. The file is not rewritten by loading; the next save writes the repaired lists.
- The other refusals of `Validate` read `must not be empty`, `must be at most 100 characters` and `is listed twice`.
- Accepted consequence: a file whose only labels are system labels (for example `labels: [linux]` and a repository with no labels of its own) loaded before and no longer does. The repair drops the system label, and the existing rule `needs at least one label in labels or repo labels` then stops the daemon. Such a file was already running runners with no label of their own, which that rule exists to prevent. The owner was asked on 2026-10-12 whether it should load with a warning instead; this stands unless they say so.
- The 100-character limit is the web form's. GitHub's own limit is not verified (§7).

### 3.2 Empty file (row 58)

- A `config.yaml` that holds no YAML document (empty, or only comments) loads as the defaults. Today the first `Decode` returns `EOF` and the daemon does not start.

### 3.3 Saving (rows 59 and 206)

- `Save` resolves `path` when it is a symbolic link and replaces the file the link names, so the link stays. The temporary file is created beside that file.
- `Save` syncs the temporary file before the rename, as `metrics.Save` and the history store do.
- Mode 0600 stays. It is deliberate: the directory is 0700 and nothing reads the file but the daemon.

### 3.4 Repeated load warnings (row 208)

A reload writes every warning of the load as an event, so `global_max 500 is above 99; using 99` repeats on every reload until a save rewrites the file.

- The store remembers the warnings of its last load. `Backend.Reload` writes an event only for a warning that load did not have. A save replaces the remembered list with the saved config's warnings, so editing the file back to an out-of-range value is reported again.

## 4. Toolchains and storage

### 4.1 Downloads (row 60)

Checked on 2026-10-12: every `download_url` in the Node and Python manifests, and the link Adoptium returns for a Temurin JDK, is on `https://github.com`.

- Before an archive or script is fetched, its URL must be `https`, on the host its tool expects and below that tool's own release path: `https://github.com/actions/node-versions/releases/download/` for Node, `https://github.com/actions/python-versions/releases/download/` for Python, `https://github.com/adoptium/temurin<major>-binaries/releases/download/` for Java with the major being installed, and the configured `Sources.GoDownload` and `Sources.DotnetScript` for Go and .NET. A host alone would let a tampered version list name any repository on GitHub. Anything else fails the install with `refusing to download <url>`.
- The URL is judged by its parsed fields: no user, port, query or fragment, the host equal byte for byte, and a path with no dot segment, doubled or encoded slash, encoded dot or backslash.
- Every download goes through one function on `Env`, which applies the rule of the installer that called it and then fetches that same string. The check is a field of `Env` whose strict form is the default, so tests keep their local servers.
- Where a redirect leads is not pinned by host: the first URL is authenticated by TLS, an origin that can redirect anywhere can serve anything itself, and GitHub has changed its asset host before.
- The download client and the version-list client refuse a redirect to a URL that is not `https`.
- The `tar` that unpacks an archive as root gains `--no-same-owner`.
- No checksum is added for Node, Python or `dotnet-install.sh` (Decisions). `dotnet-install.sh` and Python's `setup.sh` already run as the runner user, not as root.

### 4.2 A failed Python install (row 61)

`dropFailed` removes the version folder as root after a failed `setup.sh`, without the symlink check every other removal makes, and also when the folder was there before the install began: a job's own `setup-python` may be writing it.

- `Install` notes whether the version folder exists before `setup.sh` runs. After a failure it removes the folder only when it did not, and only when `noSymlinks` passes for it.

### 4.3 Go file names (row 62)

Checked on 2026-10-12: the manifest lists `1.20.0` and `1.19.0`, and go.dev names those files `go1.20.linux-amd64.tar.gz` and `go1.19.linux-amd64.tar.gz`. From 1.21 on the names agree.

- When go.dev lists no checksum for the manifest's spelling and the version is `x.y.0`, `Resolve` tries `go<x.y>.linux-amd64.tar.gz`. The installed folder keeps the three-part version.

### 4.4 Java folders (row 64)

- `Remove` deletes the folder named exactly as the version when it is a complete install, which is what `Installed` lists, and otherwise the folder with the first `+` turned into `-`, as today.

### 4.5 The `.tmp` directory (row 65)

`opDir` checks `.tmp` with `Lstat` and then calls `Chmod` and `Mkdir` by path. A job can swap the directory for a link between the two.

- `opDir` opens `.tmp` as an `os.Root`, refuses it when what it opened is not what `Lstat` of the path names (`os.OpenRoot` follows a link at its own path), checks the opened directory's owner, changes its mode through the handle and creates the operation directory relative to it. A `.tmp` that is a link or belongs to someone else is still removed and made again first.
- Not closed here: after `opDir` returns, the install uses the operation directory by path again. The random operation ID and the unlistable `.tmp` limit that; closing it means carrying the handle through the install, which is a restructuring.

### 4.6 Hard links (row 66)

- `measure` counts the allocated size of a file with more than one link once per device and inode, as `du` does, within each row it reports (one toolchain, folder or cache path), so a row equals `du -s` of that row. Its file count and newest time are unchanged.

## 5. Installer, CI and docs

### 5.1 Docker's apt key (row 67)

- `install_docker` installs `gnupg` with `ca-certificates` and `curl`, downloads the key once into a temporary directory and imports it into a throwaway gpg keyring. That keyring must hold a key whose primary fingerprint is exactly `9DC858229FC7DD38854AE2D88D81803C0EBFCD88` (checked against the published key on 2026-10-12).
- What is installed to `/etc/apt/keyrings/docker.asc` is gpg's own armored export of that one key, never the downloaded bytes: apt trusts every key in a `Signed-By` file and may read a crafted file differently from gpg.
- A failed download, import, lookup or export stops with `Docker apt key has an unexpected fingerprint`, writes nothing to `/etc/apt/keyrings` and leaves an existing key file as it was.
- The ghr tarball check is unchanged (Decisions).

### 5.2 The service unit (row 68)

With `Requires=docker.service`, stopping Docker stops ghr, and starting Docker again does not start it.

- `[Install]` becomes `WantedBy=multi-user.target docker.service`, so a start of Docker starts ghr after it. `Requires=` stays: ghr must not start runners without Docker. `install_service` uses `systemctl reenable` so an existing install gets the new link.
- The unit file is written, and `daemon-reload` run, only when its content differs. The restart still happens on every run, because the binary may be new.
- The start-after-Docker behaviour was checked on systemd 257 (§7).

### 5.3 The password prompt after a failed first install (row 69)

`FIRST_INSTALL` lives in the script's memory, so a first install that dies after `install_config` never asks for the web password on the re-run.

- `install_config` also creates `$STATE_DIR/web-password-pending`. `set_web_password` runs when that file exists and removes it once a password is set, or once the owner pressed Enter to skip at a terminal. Without a terminal and without `GHR_WEB_PASSWORD` the file stays, so a later run asks.
- `FIRST_INSTALL` is read nowhere else and is removed, in the script and in the tests that set it.

### 5.4 Old runner versions (row 70)

`gc_dist` runs after the restarted daemon is ready, where a runner update queued in that moment could be installing into the directory it removes.

- `gc_dist` runs right after `install_runner`, while the daemon is stopped by `stop_ghr`. Its comment is replaced. Nothing reads an old version while the daemon is down (§7).

### 5.5 Small fixes (rows 71 and 72)

- A leading `v` in `RUNNER_VERSION` is removed.
- `print_next` prints the host part of `web.listen` when it is a specific address, and the first host address only for a wildcard or an empty host.

### 5.6 CI (row 73)

- The workflow gets a top-level `permissions: contents: read`; the release job keeps its `contents: write`.
- Both checkouts set `persist-credentials: false`. The release step publishes through `GH_TOKEN` and reads tags only.
- Every action is pinned to a full commit SHA, with its tag in a comment.

### 5.7 README (row 74)

- The Requirements line says ghr serves private repositories, and a public one only with `allow_public` set for it, with the warning about fork pull requests kept.

## 6. Runner and daemon leftovers

### 6.1 Root reads of job-writable files (row 203)

`readJSON` uses `os.ReadFile`. For `job.json` and the legacy `ghr.json`, both inside the instance directory a job can write, a FIFO planted under either name blocks the tick.

- Those two files are read through a helper that opens without blocking and without following a link, accepts only a regular file and reads at most 1 MiB, the checks `readFrom` already makes for logs. Root-owned files (private records, pending records, the update state) keep `readJSON`.

### 6.2 A repository that becomes public (rows 214 and 211)

Checked on 2026-10-12 with `gh api`: each run in GitHub's runs listing carries `repository.private`.

- `github.Run` gains the repository's `private` flag. `repoDemand` already lists a repository's queued, in-progress and waiting runs on every poll. When any of them says the repository is public, the manager records that answer at once, as a read made now. The block then applies in the same tick, before anything is planned: its queued jobs are discarded and no runner starts.
- The listing never lifts a block and never marks an unread repository as private: a listing may be served from the ETag cache. Only `GetRepo` does, on its 6-hour schedule or after a reload.
- A repository with no run in the three listings gives no answer, and needs none: without a queued run nothing is started for it.
- `AddRepo` has just read the repository from GitHub. It hands that answer to the manager, so a repository removed and added again never keeps an older one (row 211).
- The known limits of the daemon-fixes spec §3 still hold: a runner already starting is not stopped, and during an API pause nothing is polled.

### 6.3 A log that is not there yet (row 213)

A completed job whose log answers 404 is remembered as "produced no log" for 10 minutes. Whether GitHub's blob store can answer 404 for a moment after a job completes is not verified.

- When the job completed less than 2 minutes ago, that answer is remembered for 15 seconds. The page says the same thing meanwhile.

### 6.4 Small fixes (rows 205, 207 and 210)

- `metrics.Load` drops a rollup dated at or after the current UTC hour, the rule `addToHour` applies on a backwards clock step, so an hour lived twice gets one rollup.
- `forcedPrune` writes `history and logs past retention removed` only when a history entry or a log archive was removed. The step is recorded in the prune's result either way. Whether history lost an entry is read from the store's write counter, so a job's entry appended during the prune also counts as a removal; an exact answer needs the history store to return a count, which is not worth a change there.
- Each per-repository check in `checkTokenOn` recovers from a panic, which becomes that repository's error `could not check the token`.

### 6.5 Rows with no behaviour change

- **Row 52:** by design. No browser scopes a cookie by port, and the Origin check compares host names only because the reverse proxy forwards `Host` without the browser's port, as the comment on `sameHost` says.
- **Row 186:** by design. The runner user is in the `docker` group, so a job can remove any compose project's networks and volumes itself; root doing it on the job's word gives the job nothing new. The comment on `projects()` is rewritten to say what moving the file closed and what it did not.
- **Row 199:** already fixed by commit `90d7ca4`: `install_runner` fetches a version again when any of its files is not owned by root, and removes group and other write permission.
- **Row 63:** deferred as a known limit. A .NET install cut off by a daemon stop can leave an SDK that counts as installed; removing it and installing it again recovers. Cleaning partly copied runtimes and packs after a failure could delete what a running job's own `setup-dotnet` is writing.
- **Row 212:** deferred as a known limit. Finding a `watch_repos` entry under a repository's old name needs one API call per watched repository on every add; the owner can remove the old entry.

## 7. Verify before fixing

- **Row 48:** read from the code, not timed: runner units set no `TimeoutStopSec`. The plan times one stop of a busy runner if the owner allows a deploy; the fix does not depend on the exact figure.
- **Row 57:** GitHub's maximum label length is not verified: its documentation names none, and trying one means registering a runner, which is a write to GitHub. The rule keeps the web form's 100. Should GitHub refuse a shorter label, the spawn fails with GitHub's own error, as it does today, and the limit is lowered then.
- **Row 68:** verified on 2026-10-12 on Debian 13 with systemd 257, with two throwaway units shaped like `docker.service` and `ghr.service`. With `Requires=` and `After=` alone, the second stays inactive after the first is stopped and started. With the second also wanted by the first, it starts again, also on a restart of the first.
- **Row 70:** verified on 2026-10-12 by reading `spawn` in `internal/runner/lifecycle.go`: an instance directory is a full copy of the version `dist/current` names (`CopyTree`), so no running runner reads a version directory, and `stop_ghr` stops the daemon before `install_runner`.
- **Row 213:** not verifiable without a job finishing under observation; the fix is safe either way.

## 8. Testing

Every fix starts from a failing test. Go tests use the packages' existing fakes and clocks, and no new test calls `time.Sleep`. `setup.sh` changes are tested in `deploy/tests/setup_test.sh`, which sources the script and replaces its commands.

- **Web server:** an IPv4 address locks at 5 failures; an IPv6 address locks at 5 while another address in its /64 still logs in; the /64 locks at 50 failures spread over addresses and then refuses a fresh address; a success clears the address and not the /64. A wrong method on `/auth/setup` answers 405 with `Allow`; a 17 KiB body answers 413; two maximum-length passwords pass. A save failure answers 500 without the OS text. The limited listener holds the 257th connection until one closes.
- **CLI:** `Kill` waits past 30 seconds on a slow fake daemon; a cancelled call prints `interrupted`; a timed-out call prints the did-not-answer text. `token set` at a terminal uses the no-echo read. `repo add -h` prints usage; `status extra` is a usage error. A context ended during the password read returns at once and restores the terminal.
- **Config:** each label rule refuses through `Validate`; `Parse` drops the same labels with a warning and the result saves and loads; an empty and a comment-only file load as defaults; `Save` through a symlink keeps the link and changes its target. A reload repeats no warning; a save followed by a bad edit reports it again.
- **Toolchains and storage:** an `http` URL and a URL on another host are refused for each tool; a redirect to `http` fails; a failed Python install leaves a folder that existed and removes one it made; Go `1.20.0` resolves to `go1.20`; a Java folder containing `+` is removed; `opDir` with `.tmp` as a symlink does not change the link's target; a file with two links is counted once.
- **Installer:** a key with another fingerprint stops the install and writes no keyring; the unit is not rewritten when unchanged; a re-run with `web-password-pending` asks for the password; `gc_dist` runs before the service restart; `RUNNER_VERSION=v2.x` installs `2.x`; `print_next` names the listen address.
- **Runner and daemon:** a FIFO named `job.json` does not block a tick; a queued run that says its repository is public blocks it in that tick, and a listing that says private does not lift a block; a repository added again starts with the answer `AddRepo` read; a not-found log of a job completed a minute ago is asked for again after 15 seconds; `Load` drops a rollup of the current hour; a prune that removed nothing writes no retention event; a panicking token check becomes that repository's error.
- **Suites:** `go vet ./...` and `go test -race ./...` with `TMPDIR` and `GOTMPDIR` on the root disk (Go is at `/usr/local/go/bin`), and `bash deploy/tests/setup_test.sh`. The CI and README changes have no test.
- **Live check:** deploying to the runner LXC needs the owner's yes each time. With a yes: a fresh `setup.sh` run, one job to completion, `ghr kill` of a busy runner, and a stop and start of Docker.

## Out of scope

- A trusted-proxy setting so the lockout sees the browser's address behind a reverse proxy: its own register row, waiting for an owner ruling.
- Checksums or signatures for Node, Python, `dotnet-install.sh` and the ghr tarball (Decisions).
- The web form's label rules and messages (row 103): sub-project 4.
- Rows 204 and 224: the owner's live check on the runner LXC, and `/tmp` on the dev host.

## Row map

| Row | Section |
|---|---|
| 47 | §1.1 |
| 48 | §1.2 |
| 49 | §1.3 |
| 50, 51 | §1.4 |
| 52 | §6.5, by design |
| 53 | §1.5 |
| 54 | §2.1 |
| 55 | §2.2 |
| 56 | §2.3 |
| 57 | §3.1 |
| 58 | §3.2 |
| 59, 206 | §3.3 |
| 60 | §4.1 |
| 61 | §4.2 |
| 62 | §4.3 |
| 63 | §6.5, deferred |
| 64 | §4.4 |
| 65 | §4.5 |
| 66 | §4.6 |
| 67 | §5.1 |
| 68 | §5.2 |
| 69 | §5.3 |
| 70 | §5.4 |
| 71, 72 | §5.5 |
| 73 | §5.6 |
| 74 | §5.7 |
| 186 | §6.5, comment only |
| 199 | §6.5, already done |
| 203 | §6.1 |
| 205, 207, 210 | §6.4 |
| 208 | §3.4 |
| 211, 214 | §6.2 |
| 212 | §6.5, deferred |
| 213 | §6.3 |
