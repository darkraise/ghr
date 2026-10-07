# ghr toolchains, package caches and Docker disk

**Date:** 2026-10-06
**Status:** approved in chat section by section (2026-10-06); revised the same day after an independent review (Fable, 19 findings, all accepted: docs/superpowers/notes/2026-10-06-ghr-toolchains-caches-fable-review.md); amended 2026-10-06 while planning part 2 (owner-approved in chat): the `unused-volumes` prune scope, 60-minute install limits, Docker fixture provenance, the Debian package names; that amendment revised after a Fable review (8 findings, all accepted; `unused-volumes` refused while runners are busy by the owner's choice): docs/superpowers/notes/2026-10-06-ghr-toolchains-caches-amendment-fable-review.md; amended 2026-10-06 after the TUI final review (owner-approved in chat): a running prune also makes Storage poll every second (§6, §7)
**Register:** docs/superpowers/registers/2026-10-06-ghr-toolchains-caches.md
**Code:** github.com/darkraise/ghr (new `internal/toolchain`, `internal/storage`; changes in `internal/runner`, `internal/system`, `internal/daemon`, `internal/api`, `internal/model`, `internal/tui`, `cmd/ghr`); homelab `github-runner/`
**Builds on:** docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md (on-disk layout, Disk), docs/superpowers/specs/2026-10-05-ghr-web-ui-design.md (amended by this spec, §7)

## Goal

Let the owner see and manage what the runner LXC keeps on disk between jobs: the toolchains in the shared tool cache, the package-manager caches in the shared runner home, and Docker's images, volumes and build cache. Install the popular toolchains on a fresh install, so the `setup-*` actions hit the cache instead of downloading on every job.

## Decisions

- **The daemon installs toolchains itself, in Go**, writing the exact layout each `setup-*` action looks for. Pre-installing from `setup.sh` in bash was rejected because the UI could not install and the installer logic would later be duplicated in Go. A warm-up workflow that calls each `setup-*` action was rejected because it ties up a runner, needs a host repo, and still fails for Python (below).
- **Python must come from the tool cache on this LXC.** `setup-python` checks the tool cache first (`tc.find('Python', …)`) and otherwise picks a build from the actions/python-versions manifest by matching `platform_version` against `VERSION_ID` from `/etc/os-release` (`@actions/tool-cache` `manifest.ts`). Every Linux file in the manifest carries an Ubuntu `platform_version` (22.04, 24.04, 26.04), so on Debian 13 (`VERSION_ID="13"`) an uncached version finds no match [verified against the action sources and the manifest, 2026-10-06; not yet observed on the LXC]. Pre-installing the Ubuntu 24.04 build fixes this.
- **Trust assumption.** Jobs are root-equivalent on the LXC through the docker group (runner-manager spec). The rules below that keep the root daemon from following job-controlled paths are hardening, not a security boundary.
- **Removing toolchains and clearing package caches are refused while any runner is busy.** "Busy" is the runner state ghr already tracks, set as soon as the job hook writes `job.json`, before GitHub confirms the job (§2). A job can still be handed to an idle runner while a clear runs; the clear's rename-aside (§2) makes that window harmless, because the job sees either the old tree or an empty directory, never a half-deleted one.
- **Installs are always allowed.** They add a new version folder and never overwrite a file a job may be executing; the only thing an install replaces is a version folder without a `.complete` marker, which the `setup-*` actions ignore (§2). Build-cache prunes are always allowed: BuildKit does not prune cache held by a running build.
- **Clears and removals are checked when they run, not when requested**, because they wait in a queue (§2).
- **Package caches are found at their default paths only.** A workflow that relocates a cache (for example `NUGET_PACKAGES`) is not seen.
- **Toolchains show their install time, not a last-used time.** The `setup-*` actions do not write to a cached toolchain when they use it, so last use cannot be known.
- **Storage data is served by its own endpoint**, not inside `GET /status`, which clients poll every second and which must stay cheap.
- **The last prune result lives in daemon memory**, like the existing maintenance status, and resets on restart.
- **Pre-install runs only on a first install** (or when asked), so a toolchain the owner removed does not come back on the next `setup.sh` upgrade.
- **Docker disk detail goes beyond the build cache on purpose** (images, containers and volumes, and a `dangling-images` prune), as the owner chose, so the Docker disk picture is complete.
- **Unused anonymous volumes get their own prune scope, `unused-volumes`**, as the owner chose on 2026-10-06. The LXC then held 68 anonymous volumes (8.65 GB), 67 of them attached to no container. ghr's per-job cleanup removes the volumes carrying its labels and the anonymous volumes of the containers it removes (`docker rm -f -v`), but a volume a job orphans itself, through `docker rm` or `docker compose down` without `-v`, is reached by nothing. The scope is manual only: neither `standard` nor the automatic `checkDisk` prune runs it. It is refused while any runner is busy, like Remove and Clear: a job that creates an unnamed volume (`docker volume create`) and mounts it only in short `docker run --rm` steps leaves it unreferenced between steps, and a prune in that window would delete it.

## 1. Tool cache layout

The runner's `RUNNER_TOOL_CACHE` is `/var/lib/ghr/toolcache` and `DOTNET_INSTALL_DIR` is `/var/lib/ghr/toolcache/dotnet` (set per runner in `internal/runner/lifecycle.go`).

`@actions/tool-cache` `find(tool, version, arch)` accepts `<cache>/<tool>/<version>/<arch>/` only when the sibling marker file `<arch>.complete` exists and `semver.clean(<version>)` is valid semver. A version spec that is not explicit (`22`, `3.13`) matches the highest cached version satisfying it.

| Tool | Versions from | Archive from | Installed at |
|---|---|---|---|
| Node | actions/node-versions `versions-manifest.json` | the manifest's linux x64 `download_url` | `node/<x.y.z>/x64`, the archive's top folder stripped (setup-node extracts with `--strip 1`) |
| Go | actions/go-versions `versions-manifest.json`, stable entries (setup-go resolves `stable` from it before looking in the cache) | `https://go.dev/dl/go<ver>.linux-amd64.tar.gz`, checked against the `sha256` in `https://go.dev/dl/?mode=json&include=all` | `go/<semver>/x64`, the top `go/` folder stripped; `<semver>` follows setup-go's `makeSemver` (`1.25` → `1.25.0`) |
| Python | actions/python-versions `versions-manifest.json`, stable entries with a `linux` / `24.04` / `x64` file | that file's `download_url` | `Python/<x.y.z>/x64`, written by the archive's own `setup.sh` (§2) |
| Java | Adoptium `https://api.adoptium.net/v3`, Temurin (`vendor=eclipse`), HotSpot, JDK, GA | the binary's `package.link`, checked against `package.checksum` | `Java_Temurin-Hotspot_jdk/<ver>/x64`, where `<ver>` is the release's `version_data.semver` with its first `+` replaced by `-` (for example `21.0.12+101.0.LTS` → `21.0.12-101.0.LTS`), as setup-java's `getToolcacheVersionName` does; the archive's top folder (`jdk-21.0.12.1+1`) becomes `x64` |
| .NET | `https://dotnetcli.blob.core.windows.net/dotnet/release-metadata/releases-index.json` | `https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh` (`dot.net/v1/dotnet-install.sh` redirects there) | `dotnet/sdk/<ver>`; setup-dotnet reads `DOTNET_INSTALL_DIR/sdk/*` (a folder counts when it holds `dotnet.dll`) and skips its download when one satisfies the request and `check-latest` is false |

Java folders are not found by `tc.find()` for every version (a four-part release such as `21.0.12.1` exists, and `21.0.8-9` is a semver prerelease); setup-java reads them back with its own `findInToolcache`, converting `-` back to `+` before matching. Its layout test therefore ports that code (§9).

## 2. Daemon

### Packages

- **`internal/toolchain`** holds one installer per tool behind one interface:
  - `Available(ctx) ([]Choice, error)`: what the Install dialog offers, newest first (below).
  - `Resolve(ctx, spec string) (Release, error)`: turns a spec into one release.
  - `Install(ctx, rel Release) error`.
  - `Installed() ([]Installed, error)`: tool, version, arch, path, install time.
  - `Remove(version string) error`.
- It also holds the `popular` preset: Node `22` and `24`, .NET `8.0` and `10.0`, Python `3.13` and `3.14`, Go `latest`, Java `21` and `25`. Each entry resolves when its install runs.
- **`internal/storage`** holds the operation queue, the measurer and the snapshot. It receives `busy func() int` from `runner.Manager` (below).

`runner.Manager` keeps prune (§4); the Backend merges its prune state into the storage snapshot. `runner.Manager` gains a nil-safe `PruneDone func()` field, which the daemon sets to the measurer's trigger (§2 Measurer, §4).

### Versions per tool

| Tool | `Available` lists | `Resolve` accepts |
|---|---|---|
| Node | manifest versions, stable only | `x.y.z`, `x`, `x.y`, `latest` (the newest stable): newest match |
| Go | manifest stable versions | same |
| Python | manifest stable versions with a 24.04 x64 file | same |
| Java | the majors in `/v3/info/available_releases` `available_releases`, LTS ones marked | a major only (`21`): newest GA from `/v3/assets/feature_releases/<major>/ga?architecture=x64&os=linux&image_type=jdk&jvm_impl=hotspot&vendor=eclipse` |
| .NET | channels whose `support-phase` is `active` or `maintenance`, each with its `latest-sdk` | a channel (`8.0`) → its `latest-sdk`; a full SDK version (`8.0.414`) → itself |

`Available` results are cached per tool for 1 hour. "Stable" excludes prereleases (`-rc`, `-beta`, `stable: false` in the manifests).

### Busy count

`runner.Manager.BusyCount()` takes the manager's lock, runs `readJobFiles()` so a `job.json` the hook has just written is seen, and counts instances in state `Busy`. It does not use `JobConfirmed`, which waits at least one poll interval for GitHub.

### Operation queue

- Toolchain installs, toolchain removals and cache clears join one first-in-first-out queue and run one at a time.
- A queued remove or clear calls `busy()` when it reaches the front. A non-zero count fails it with `refused: N jobs running`; it is not retried.
- Prune is not in the queue. It keeps its own one-at-a-time flag and may run concurrently with the queue.
- There is no cancel. Daemon shutdown interrupts the running operation and drops the rest of the queue; each dropped item gets a `warn` event.
- The last 10 finished operations are kept in memory with their outcome (`ok`, `failed`, `refused`, `interrupted`, `skipped`) and message.
- Each operation adds an `info` event when it starts and an `info` or `warn` event when it ends, naming what it installed or freed, so the Dashboard feed shows it.

### Install mechanics

1. Resolve the version. If it is already installed (folder and marker present; for .NET, `sdk/<ver>/dotnet.dll`), finish as `skipped` with an `info` event.
2. Create `/var/lib/ghr/toolcache/.tmp/` root-owned, mode 0711, and the operation's directory `.tmp/<op-id>/` root-owned, mode 0700. Download into it over HTTPS and verify the checksum where the source publishes one (Go, Java). Node and Python archives come from the same GitHub releases the actions download, with no checksum, which is the trust the actions already extend.
3. Extract inside `.tmp/<op-id>/`, then `chown -R -h ghrunner:ghrunner` the extracted tree (`-h`: never follow a symlink in the archive).
4. If the target `<tool>/<ver>/x64` exists without a marker (a killed earlier install), remove it, as `@actions/tool-cache` `_createToolPath` does. Then rename the tree into place and write the `.complete` marker last, owned by `ghrunner`.
5. On any failure, delete `.tmp/<op-id>/` and the target folder if this operation created it. No marker exists, so the actions ignore anything left behind.
6. On daemon start, delete `.tmp/` and any `.ghr-clearing-*` directories (below). The queue worker does this before its first operation, so the API is not held up and no install races it. Version folders without a marker are never swept at start: one may be a running job's own `setup-*` download in progress.

Each command an install runs (`tar`, `chown`, `runuser` for `setup.sh` and `dotnet-install.sh`) and each archive download is limited to 60 minutes, not the 10 minutes ghr's other commands get: a .NET SDK, a JDK, or Python with its pip step can take longer on a slow link. `internal/system` gains `ExecGroupFor(d)`, a process-group-killing runner with timeout `d`, and `DownloadFor(d)`, a download with timeout `d`; `Exec`, `ExecGroup` and `Download` keep reading `CommandTimeout` and `DownloadTimeout` on every call. `toolchain.NewEnv` takes the download function as a parameter, so the daemon cannot forget the limit: `NewEnv(root, home, user, system.ExecGroupFor(time.Hour), system.DownloadFor(time.Hour))`; its two existing test calls gain the argument. Version lists keep their 1-minute limit. The limit is per step, not per operation: one Python install (download, extraction, `setup.sh`) may legitimately run about three hours before every step times out, and the queue behind it waits. There is no operation-level limit; daemon shutdown still cancels the running step and kills its process group.

Per-tool steps:

- **Python:** the python-versions archive contains a `setup.sh` that deletes any existing `Python/<ver>/x64`, copies the build there, runs `./python -m ensurepip` and `pip install --upgrade --force-reinstall pip`, and writes the marker. ghr chowns the operation directory to `ghrunner` and runs that script as `ghrunner` (`runuser -u ghrunner`) with this environment, mirroring setup-python's `installPython`:
  - `RUNNER_TOOL_CACHE=/var/lib/ghr/toolcache`;
  - `LD_LIBRARY_PATH=<extracted>/lib`, because the builds are shared-library builds whose rpath points at the build machine's tool cache;
  - `HOME=/home/ghrunner`;
  - `AGENT_TOOLSDIRECTORY` unset (the script prefers it over `RUNNER_TOOL_CACHE`).
  ghr then checks that the marker exists. The pip step needs outbound access to PyPI. The build targets Ubuntu 24.04 (glibc 2.39); Debian 13 ships glibc 2.41, and the runtime libraries it links are installed by `setup.sh` (§8) [compatibility inferred; the LXC acceptance check confirms it]. At job time setup-python itself exports `LD_LIBRARY_PATH`, so nothing else is needed.
- **.NET:** run `dotnet-install.sh --version <sdk> --install-dir /var/lib/ghr/toolcache/dotnet --skip-non-versioned-files` as `ghrunner`, from a copy of the script in the operation directory (chowned to `ghrunner`, as for Python). Steps 3–4 do not apply: the script writes into `dotnet/` directly, and on failure ghr deletes `sdk/<ver>` if it did not exist before the operation. The flag skips non-versioned files only when they already exist, so the first install still writes the `dotnet` host and later installs never overwrite it while a job may be running it. setup-dotnet passes the same flag.
- **Java:** the archive's top folder becomes `x64` (§1).

### Removal

- Node, Go, Python, Java: delete `<tool>/<ver>/x64.complete` **first**, so a job starting during the removal no longer finds the version, then `<tool>/<ver>/x64`, then `<tool>/<ver>` when empty.
- .NET: delete `dotnet/sdk/<ver>`. When no other SDK of the same major version remains, also delete that major's folders (names starting with `<major>.`) under `shared/*/`, `packs/*/`, `host/fxr/`, `templates/`, `sdk-manifests/`, `metadata/workloads/` and `library-packs/`. This removes that major's runtimes too, so a job that builds with a newer SDK but runs `net<major>.0` tests loses them; the confirmation says so (§6). The `dotnet` host stays.
- Deletion uses `os.RemoveAll`, which does not follow symlinks.
- An unknown tool or a version that is not installed is a 404.

### Package caches

A static table, paths relative to `/home/ghrunner` (`Paths.Home`):

| Name | Label | Paths |
|---|---|---|
| `nuget` | NuGet | `.nuget/packages` |
| `npm` | npm | `.npm` |
| `pnpm` | pnpm | `.local/share/pnpm/store`, `.cache/pnpm` |
| `yarn` | Yarn | `.cache/yarn`, `.yarn/berry/cache` |
| `pip` | pip | `.cache/pip` |
| `gomod` | Go modules | `go/pkg/mod` |
| `gobuild` | Go build | `.cache/go-build` |
| `maven` | Maven | `.m2/repository` |
| `gradle` | Gradle | `.gradle/caches`, `.gradle/wrapper/dists` |
| `cargo` | Cargo | `.cargo/registry`, `.cargo/git` |

- Clearing handles each existing path in three steps: rename it to `<parent>/.<base>.ghr-clearing-<op-id>` (atomic), recreate the empty directory owned by `ghrunner` with the old mode, then delete the renamed tree. A job sees either the old tree or an empty directory. Every step goes through one `os.Root` opened at the runner home, so a symlinked directory that stays inside the home is followed and one leading out of it is refused, even if a job swaps it in mid-clear.
- No permission changes are needed: the daemon runs as root, so Go's read-only module files do not block deletion, and ghr never chmods a job-controlled tree.
- A cache none of whose paths exist is reported `present: false` and cannot be cleared (409).

### Other tool-cache folders

Top-level folders in the tool cache that no installer owns (for example `PyPy`, `Java_Zulu_jdk`, `Ruby`, written by jobs' own `setup-*` steps) are listed as "other" with their size and no Remove. `.tmp` is skipped.

### Measurer

- One goroutine measures every 30 minutes, after every queued operation and after every prune (through `runner.Manager.PruneDone`, §4), and on `POST /storage/refresh`. A trigger during a measurement starts one more measurement after it, never more than one. Only one measurement runs at a time; a refresh during one is a 409.
- It walks each package cache path, each toolchain folder and each other tool-cache folder with `Lstat` only (symlinks are counted as entries, never followed; a package cache path that resolves out of the runner home through a symlinked directory is skipped and named in `measure_error`), summing allocated size (`st_blocks × 512`, as `du` does), counting regular files, and taking the newest file modification time as "last written".
- Install time is the modification time of the `.complete` marker (for .NET, of `sdk/<ver>`); both ghr and the actions write the marker last.
- It reads Docker disk usage (§3) last; a Docker failure keeps the filesystem results.
- Results go into a new snapshot that replaces the old one under a mutex only when the whole measurement finishes, so readers never see half of one. A failed part keeps the rest and records `measure_error`.

## 3. Docker disk

New `system.Docker` methods:

- `DiskUsage(ctx)`: `docker system df --format json`, one JSON object per line with string fields `Type` (`Images`, `Containers`, `Local Volumes`, `Build Cache`), `TotalCount`, `Active`, `Size` and `Reclaimable` (humanized, for example `"1.2GB (50%)"`).
- `BuildCacheUsage(ctx)`: `docker system df -v --format json`, one document whose `BuildCache` array is grouped by `CacheType` (`regular`, `source.local`, `exec.cachemount`, `frontend`, others as they come) into count, size, and reclaimable size (records not `InUse`).
- `PruneAllBuildCache(ctx)`: `docker builder prune -af`.
- `PruneUnusedVolumes(ctx)`: `docker volume prune -f`. Without `--all` it removes only anonymous volumes no container uses; named volumes and volumes in use stay (`docker volume prune --help` on the LXC, Docker 29.8.2, 2026-10-06). A volume referenced by any container, running or stopped, is in use and survives. An unnamed volume created with `docker volume create` is anonymous too, and between the steps that mount it nothing references it, so the scope is refused while runners are busy (§4).

Sizes are parsed from Docker's humanized strings into bytes: a number with a decimal unit (`B`, `kB`, `MB`, `GB`, `TB`, `PB`, base 1000, as go-units `HumanSize` prints them), optionally followed by a percentage (`"5.627GB (85%)"`). The Images, Containers and Local Volumes rows carry the percentage only when their total is non-zero; the Build Cache row's `Reclaimable` never carries one (`"1.2GB"`, `"0B"`). The same parser reads the prune commands' "Total reclaimed space" values (§4).

Fixture provenance (captured 2026-10-06 while planning part 2, Docker 29.8.2 on the LXC):

- `docker system df --format json` is real LXC output: one object per line, every field a string. Its Build Cache row was empty (`"0B"`), so the fixture adds a variant with a non-zero bare `Reclaimable` and a `TB` size.
- `docker system df -v --format json` is one real document with the keys `BuildCache`, `Containers`, `Images` and `Volumes`. The LXC had no build cache then, so `BuildCache` was empty. The build-cache record fixture is therefore written from the Docker CLI formatter at tag v29.8.2 (`cli/command/formatter/buildcache.go`): every field is a string; `ID` gains a trailing `*` when the record is in use; `Size` is humanized with three significant digits; `InUse` and `Shared` are `"true"` or `"false"`; the other fields are `Parent`, `CacheType`, `Description`, `CreatedAt`, `CreatedSince`, `LastUsedAt`, `LastUsedSince` and `UsageCount`. The LXC acceptance check (§9) confirms them against real records.

## 4. Prune

- `forcedPrune` takes a scope:
  - `standard` (today's behavior): build cache down to `build_cache_keep`, dangling images, history and logs past retention, then a disk reading.
  - `build-cache-keep`: build cache down to `build_cache_keep`.
  - `build-cache-all`: `docker builder prune -af`.
  - `dangling-images`: `docker image prune -f`.
  - `unused-volumes`: `docker volume prune -f` (§3); manual only, refused with 409 `refused: N jobs running` when `BusyCount()` is non-zero at request time (manual prunes start at once, so request time is run time).
- Every scope ends with a disk reading and triggers a measurement: `forcedPrune` calls `PruneDone` on every exit path, interrupted included. The automatic `checkDisk` prune calls it only after it pruned, not when the disk was under `disk_high_water`.
- Both `forcedPrune` and the automatic `checkDisk` prune record `last_prune`: trigger (`auto` or `manual`), scope (`auto` for `checkDisk`), start and finish times, outcome, and one entry per step with the space it freed or its error. `freed` is bytes, parsed with the §3 parser from the step's "Total reclaimed space" (0 when the output has none); clients format it. `checkDisk` keeps its existing event.

## 5. Data model and API

### `model.Storage` (served by `GET /storage`)

- `toolchains`: `[]{tool, version, arch, path, bytes, installed_at}`. Java's `version` shows the folder name with its first `-` turned back into `+`.
- `other_tool_cache`: `[]{name, bytes}`.
- `package_caches`: `[]{name, label, paths, present, bytes, files, last_written}`.
- `docker`: `{rows: []{type, count, active, bytes, reclaimable}, build_cache_types: []{type, count, bytes, reclaimable}, disk_pct}`.
- `measured_at`, `measuring`, `measure_error`.
- `operations`: `{current: {id, kind, target, started_at, progress} | null, queued: <count>, recent: []{id, kind, target, started_at, finished_at, outcome, message}}`. `kind` is `install`, `remove` or `clear`; `progress` is the latest step (`downloading`, `extracting`, `running setup.sh`, …).
- `last_prune`: `{trigger, scope, started_at, finished_at, outcome, steps: []{name, freed, error}} | null`.

### Endpoints

| Endpoint | Behavior |
|---|---|
| `GET /storage` | The current snapshot; never walks a directory during the request. |
| `POST /storage/refresh` | 202; 409 while a measurement runs. |
| `GET /toolchains/available?tool=<tool>` | `[]{spec, version, lts?}`: `spec` is what `POST /toolchains` takes (a full version, a Java major, a .NET channel), `version` what the picker shows; 400 for an unknown tool; a source failure is a 502 with its message. |
| `POST /toolchains` | Body `{"tool": "node", "version": "22"}` or `{"preset": "popular"}`. 202 once queued; 400 for an unknown tool or preset or a missing version. Resolution happens when the install runs, so an unresolvable version fails that operation, not the request. |
| `DELETE /toolchains/{tool}/{version}` | 202 once queued; 400 unknown tool; 404 not installed. |
| `POST /caches/{name}/clear` | 202 once queued; 404 unknown name; 409 not present. |
| `POST /prune` | Unchanged: the `standard` scope; the request body is not read. |
| `POST /prune/{scope}` | `build-cache-keep`, `build-cache-all`, `dangling-images`, `unused-volumes` or `standard`; 400 for an unknown scope; the same 409s as `POST /prune`, plus `refused: N jobs running` for `unused-volumes` while runners are busy. A separate route, so an older daemon answers a plain-text 404 instead of silently running a standard prune. |

The client's older-daemon detection (a plain-text 404 or 405 reads as "restart ghr") covers the new routes.

## 6. TUI

### Pages

The sidebar becomes Dashboard, Repositories, Runners, History, **Storage**, Settings: Storage is key `5` and Settings moves to key `6`. Every table indexed by page follows: `pageNames`, the tab row's short names (`internal/tui/shell.go`), the number keys in `input.go`, and the help overlay's "1-5" texts (`dialogs.go`), which become "1-6".

The Storage page polls `GET /storage` every 5 seconds, and every second while an operation runs or waits, a measurement is in progress, or a prune runs (the status's maintenance state).

### Cards, top to bottom

1. **Docker disk**
   - The disk bar against `disk_high_water`.
   - A table of Images, Containers, Volumes and Build cache: count, active, size, reclaimable. Under Build cache, indented rows per cache type.
   - A last-prune line, for example `auto · 14:05 · ok — build cache >72h 1.2 GB, to 20GB 4.1 GB, dangling 300 MB`; a failed step shows its error in red; `pruning…` while one runs; `no prune since start` when absent.
   - Buttons **Prune**, **Build cache to <build_cache_keep>**, **All build cache**, **Dangling images**, **Unused volumes**, each behind a confirmation dialog that names what it removes.
2. **Toolchains**
   - One row per installed version: tool, version, size, installed date, and **Remove**. The confirmation names the size; for the last .NET SDK of a major it also says `also removes the <major>.0 runtimes and packs`.
   - "Other" rows for unowned tool-cache folders: name and size, no button.
   - While the queue works: `installing node 24.9.0 — extracting (2 queued)`.
   - **Install…** opens a dialog: a tool select, then the existing filterable picker over `/toolchains/available` (typing `22` filters to Node 22 releases; a partial version may be entered as is), then Install. Errors stay in the dialog with Retry.
   - **Install popular set** opens a confirmation listing the nine entries.
3. **Package caches**
   - One row per cache: label, path, size, files, last written, **Clear** (confirmation naming the size). `not present` and no button when absent.
   - Footer: `measured 14:02` and **Refresh**, or `measuring…`.
4. **Recent operations**: the last 10, each with time, kind, target and outcome.

### Behavior

- When the latest status shows busy runners, Remove, Clear and **Unused volumes** stay enabled and their hint reads `refused while N jobs run`. The daemon's refusal arrives as the operation's `refused` outcome and a toast; for **Unused volumes**, which is not queued, as the 409's message in a toast.
- At 80 columns the tables drop the reclaimable and files columns.
- Settings: the Maintenance card loses its **Disk** row, its **Prune** row and the **Prune now** button, all of which the Storage page now shows. It keeps the runner rows and **Reload config.yaml**. `disk_high_water` and `build_cache_keep` stay in Settings' fields.

## 7. Web UI spec amendment

`docs/superpowers/specs/2026-10-05-ghr-web-ui-design.md` changes as follows; the frontend plan builds them with the rest of the UI:

- **Pages:** the sidebar becomes Dashboard, Repositories, Runners, History, Storage, Settings. A Storage page with the four cards of §6, the same actions and confirmations.
- **Settings:** "Prune and Reload config" becomes "Reload config".
- **Polling table:** Storage, `GET /storage`, 5 s on Storage and 1 s while an operation, a measurement or a prune runs; available versions, `GET /toolchains/available?tool=`, fetched when the Install dialog picks a tool, not polled.
- **Confirmation dialogs:** add remove toolchain, clear cache, install popular set and each prune scope (`unused-volumes` included).
- **Types and fixtures:** `types.ts` mirrors `model.Storage`, and the type-fixture tests include it.

The new routes sit under `/api/*`, so they inherit login, the `X-GHR` header and the Origin check unchanged.

## 8. CLI and `setup.sh`

### CLI

| Command | Calls |
|---|---|
| `ghr storage` | `GET /storage`, printed as Docker disk, last prune, toolchains, other tool-cache folders, package caches, current operation |
| `ghr storage refresh` | `POST /storage/refresh` |
| `ghr toolchain list` | `GET /storage` (toolchains only) |
| `ghr toolchain available <tool>` | `GET /toolchains/available` |
| `ghr toolchain install <tool> <version>` | `POST /toolchains` |
| `ghr toolchain install --preset popular` | `POST /toolchains` |
| `ghr toolchain rm <tool> <version>` | `DELETE /toolchains/{tool}/{version}` |
| `ghr cache list` | `GET /storage` (caches only) |
| `ghr cache clear <name>` | `POST /caches/{name}/clear` |
| `ghr prune [--scope <scope>]` | new command: `POST /prune` without `--scope`, `POST /prune/{scope}` with it |

Queuing commands print `queued — follow with: ghr storage`. None prompts, matching `ghr repo rm`.

### `setup.sh` (homelab `github-runner/`)

- The apt step also installs the shared libraries the python-versions builds link: `libssl3t64`, `libffi8`, `libsqlite3-0`, `liblzma5`, `libbz2-1.0`, `libgdbm6t64`, `libncursesw6` and `libreadline8t64` (names confirmed with `apt-cache policy` on the LXC, Debian 13, 2026-10-06; all eight were already installed there, so the line keeps a fresh LXC equal to it).
- `install_config` records whether `config.yaml` was missing (a first install).
- A new step after `wait_ready`, `preinstall_toolchains`, runs `ghr toolchain install --preset popular` when `GHR_TOOLCHAINS` is `popular`, then logs that the installs continue in the background and that `ghr storage` shows progress. It does not wait.
- `GHR_TOOLCHAINS` defaults to `popular` on a first install and `none` on an upgrade; either value may be set explicitly.
- A failure to queue (for example an older daemon) logs a warning and does not fail `setup.sh`.
- The README gains a "Toolchains and caches" section: the commands, `GHR_TOOLCHAINS`, the refusal while jobs run, why Python must come from the tool cache on Debian, and that removing the last .NET SDK of a major removes its runtimes.

## 9. Testing

### Go

- **Installers**, each against an `httptest` server serving a small manifest or index and archive:
  - the layout of §1, including Go's `makeSemver`, Java's `version_data.semver` folder (with a four-part release such as `21.0.12.1+1`) and .NET's `sdk/<ver>`;
  - each `Resolve` row of §2, including Go `latest` resolving from the go-versions manifest when go.dev lists a newer version;
  - Python's `setup.sh` runs as `ghrunner` with `RUNNER_TOOL_CACHE`, `LD_LIBRARY_PATH=<extracted>/lib` and `HOME` set and `AGENT_TOOLSDIRECTORY` unset (a fake script records its environment);
  - .NET passes `--version <sdk>` and `--skip-non-versioned-files`;
  - a checksum mismatch, a download error, an extraction failure and a Python `setup.sh` that writes no marker each leave no version folder, no marker and nothing under `.tmp/`;
  - an installed version is `skipped`; a marker-less target is replaced;
  - start-up clears `.tmp/` and `.ghr-clearing-*` without delaying `Start`, before the first queued operation, and leaves marker-less version folders alone;
  - `chown` does not follow a symlink in the archive.
- **Layout compatibility**, against code ported from each action: Node, Go and Python through `@actions/tool-cache` `find()` with both an explicit and a partial spec; Java through setup-java's `findInToolcache` and `isVersionSatisfies`; .NET through setup-dotnet's `getInstalledSdkVersions`.
- **Removal:** the marker goes before the folder; the last .NET SDK of a major also removes every folder listed in §2 for that major; one of two SDKs of a major removes only that SDK.
- **Busy count:** a runner whose `job.json` exists but whose job is not yet confirmed counts as busy.
- **Queue:** FIFO, one at a time; remove and clear check `busy()` at run time (a fake whose count changes between enqueue and run); shutdown interrupts and drops with events; the recent list keeps 10; `-race`.
- **Clear:** rename-aside leaves an empty directory owned by `ghrunner` with the old mode, and a clear whose user cannot be looked up fails untouched; read-only Go module files are deleted without chmod; a symlink inside a cache is removed, not followed; a symlinked parent inside the home is followed and one leading out of it is refused, by clears and the start-up sweep.
- **Measurer:** sizes, file counts and last written on a fixture tree; missing paths report `present: false`; a cache behind a symlinked parent leading out of the home is skipped with an error, one inside the home is measured; other tool-cache folders are listed and `.tmp` is not; refresh during a measurement is a 409; a Docker failure keeps the filesystem results; readers never see a partial snapshot (`-race`).
- **Install limits:** `ExecGroupFor` and `DownloadFor` time out at their own `d`; `Exec`, `ExecGroup` and `Download` still follow a changed `CommandTimeout` and `DownloadTimeout`; `NewEnv` stores the download function it is given.
- **Docker parsing:** the fixtures of §3, including humanized sizes.
- **Prune:** each scope runs only its steps, and `standard` does not prune volumes; `unused-volumes` with a busy runner is a 409 and runs no Docker command; `PruneDone` fires once per `forcedPrune` (interrupted included) and per pruning `checkDisk`, never for a `checkDisk` under the high-water mark; `freed` is parsed to bytes; `last_prune` records trigger, steps and a failed step's error for both `checkDisk` and `forcedPrune`; bare `POST /prune` is `standard`.
- **API:** every status code in §5, over HTTP and at the Backend; older-daemon detection for the new routes, including `POST /prune/{scope}`.
- **CLI:** each command's request and output.

### TUI

- New goldens at 80 and 120 columns: Storage idle, Storage with an install running and two queued, Storage with busy runners, the Install dialog, a confirmation dialog (including the .NET runtime warning).
- Every existing golden is regenerated (the sidebar and tab row change) and each diff reviewed; the Settings goldens lose the Disk and Prune rows and the button.
- The tests that assert `[ Prune now ]` or focus on `setPrune` (`manage_test.go`, `settings_test.go`) move to the Storage page.
- Input: `5` opens Storage and `6` Settings; mouse reaches every button.

### `setup.sh`

`tests/setup_test.sh`: a first install queues the preset; an upgrade does not; `GHR_TOOLCHAINS` overrides both ways; a failed queue warns and the script succeeds; the apt step lists the Python runtime libraries.

### LXC acceptance (register rows)

1. `ghr toolchain install --preset popular` finishes with nine toolchains listed.
2. A workflow in one private repo runs `setup-node` 22, `setup-python` 3.13, `setup-dotnet` 8.0.x, `setup-go` (`stable`) and `setup-java` 21 (temurin); each log shows the cached toolchain used without a download, and `python -c "import ssl, sqlite3, ctypes, lzma, bz2, zlib"` succeeds. On the LXC, `ldd` over each installed Python's `lib/python3.*/lib-dynload/*.so` (with `LD_LIBRARY_PATH` set to its `lib`) reports no `not found`; a missing library (for example `libgdbm_compat.so.4`, package `libgdbm-compat4t64`) is added to the `setup.sh` apt line.
3. The Storage page shows Docker disk rows, build cache types and package cache sizes; **All build cache** frees space and `last_prune` shows it.
4. Clearing NuGet while a job runs ends `refused`; it succeeds once no runner is busy.
5. **Unused volumes** while a job runs is refused; once no runner is busy it frees the anonymous volumes no container uses, the Local Volumes row shrinks, and `last_prune` shows the space freed.

## 10. Delivery

One ghr feature branch, merged and released through CI (the next patch version), deployed to the LXC with `GHR_VERSION` pinned after the owner approves the release. The homelab `setup.sh`, its test and README changes ship with it and are copied to the LXC's `/root/github-runner`. The existing LXC is not a first install: the owner runs `ghr toolchain install --preset popular` (or `GHR_TOOLCHAINS=popular bash setup.sh`) to pre-install there.

## Out of scope

- Last-used times for toolchains, and automatic removal of unused toolchains.
- Size caps or automatic trimming of package caches.
- Caches at non-default paths, and caches outside `/home/ghrunner` other than the tool cache.
- Architectures other than x64, installers beyond Node, Python, Go, Java (Temurin) and .NET, and removing "other" tool-cache folders.
- Installing an exact Java release (Java installs by major).
- Cancelling a queued or running operation.
- Persisting `last_prune` or the operations list across restarts.
- A `/opt/hostedtoolcache` symlink to match the Python builds' rpath (rejected: `LD_LIBRARY_PATH`, which setup-python already sets at job time, is enough).
