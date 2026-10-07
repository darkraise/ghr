# Fable review: ghr toolchains and caches spec

**Reviewed:** `docs/superpowers/specs/2026-10-06-ghr-toolchains-caches-design.md` @ 460eebf
**Reviewer:** judge-fable (Fable 5.1), 2026-10-06
**Legend:** [V] verified against cited source; [I] inferred.
**Disposition:** all 19 findings accepted; the spec revision lists how each was applied.

## Verdict

Not ready. Findings 1–3 block: each makes an LXC acceptance row (§9 row 1 or 2) fail as written — Java's folder naming is invisible to setup-java for the release Adoptium currently returns, the Python install cannot complete without `LD_LIBRARY_PATH`, and the .NET flag decision rests on a misreading that turns later installs into live-binary overwrites. Findings 4–5 are high: the "a running restore never loses files" decision is not delivered by the busy check and deletion order as specified. The rest are medium/low plan-level corrections.

## Findings

1. **Java folder name must come from `version_data.semver`, not the release name** — blocking, high, [V]. setup-java caches `item.version_data.semver` (temurin/installer.ts `findPackageForDownload`) through `getToolcacheVersionName` (`+`→`-`) and reads back with `tc.findAllVersions`, which lists only folders where `semver.valid(semver.clean(child))` holds. Adoptium's newest GA 21 is `jdk-21.0.12.1+1` (four components; `version_data.semver` = `21.0.12+101.0.LTS`); the spec's rule yields `21.0.12.1-1`, not valid semver. A three-part `21.0.8-9` is a semver prerelease that setup-java rescues by converting `-`→`+` before `isVersionSatisfies`, but a port of tool-cache `find()` rejects it. Fix: folder = `version_data.semver` with the first `+` → `-`; the Java layout test ports setup-java's `findInToolcache` + `isVersionSatisfies`.
2. **.NET: do not omit `--skip-non-versioned-files`** — blocking, high, [V]. dotnet-install.sh: the flag "skips non-versioned files if they already exist", so the first install writes the host anyway; without it, later installs overwrite the live `dotnet` muxer (`Text file busy`, possible muxer downgrade). setup-dotnet finds local SDKs by reading `DOTNET_INSTALL_DIR/sdk/*` and checking `dotnet.dll` (`src/installer.ts` `getInstalledSdkVersions`), and always passes the flag. Fix: always pass it; install with `--version <latest-sdk>`. `https://dot.net/v1/dotnet-install.sh` is a 301 to `https://builds.dotnet.microsoft.com/dotnet/scripts/v1/dotnet-install.sh`.
3. **Python `setup.sh` needs `LD_LIBRARY_PATH=<extracted>/lib`** — blocking, high, [V] mechanism / [I] rpath value. Builds use `--enable-shared` with an rpath to the build machine's tool cache; the bundled `setup.sh` runs `./python` (import pip, ensurepip, `pip install --upgrade --force-reinstall pip`). setup-python runs it with `LD_LIBRARY_PATH=<extracted>/lib` (`src/install-python.ts`) and exports `LD_LIBRARY_PATH` at job time (`src/find-python.ts`). Also: unset `AGENT_TOOLSDIRECTORY`, set `HOME=/home/ghrunner`, PyPI access required. Optional: symlink `/opt/hostedtoolcache`.
4. **`busy()` counting confirmed jobs misses running jobs** — high, high, [V]. `JobConfirmed` is set only on GitHub confirmation (`internal/runner/tick.go:158–182`); state is `Busy` earlier from `job.json` (`lifecycle.go:155–185`) or `GetRunner().Busy`. Fix: count instances in state Busy, after `readJobFiles()`; state the residual window.
5. **Clear and remove are not atomic for a job; removal deletes folder before marker** — high, high, [V]/[I]. Fix: remove the marker first; clear caches by renaming the directory aside, recreating it empty and owned by `ghrunner`, then deleting the renamed tree.
6. **Marker-less leftovers block re-install; do not sweep them at startup** — medium, high, [V]. Mirror tool-cache `_createToolPath` (remove a marker-less target, then install); never sweep marker-less folders globally (could be a job's in-progress `cacheDir`); keep the `.tmp` sweep.
7. **Go versions must resolve from the actions/go-versions manifest** — medium, high, [V]. setup-go resolves `stable` from the manifest before `tc.find`; go.dev is a fallback. Download the matching go.dev tarball with its `sha256`; name the folder with setup-go's `makeSemver` mapping (`1.25` → `1.25.0`).
8. **Drop the `gomod` chmod walk; root bypasses permissions** — medium, high, [I]. Removes the only root mutation of a job-controlled tree. Make `.tmp` root-owned 0700 and chown before rename. State the trust assumption (jobs are root-equivalent through the docker group), so these are hardening.
9. **Docker: use `--format json`, not `-v --format '{{json .}}'`** — medium-low, medium, [I]. Verbose template output wraps rows in section headers; `--format json` in verbose mode writes one document with `Images/Containers/Volumes/BuildCache` arrays. Non-verbose rows have humanized string sizes (`"1.2GB"`, `"1.2GB (50%)"`). Run `docker system df` last and tolerate failure.
10. **Page renumbering changes every golden and hard-coded tables** — medium-low, high, [V]. `shell.go:203` `short` slice indexed by page; help text "1-5" (`dialogs.go:541,592`); prune tests (`manage_test.go:156`, `settings_test.go:163–167`); `input.go:66`.
11. **.NET removal list incomplete; name the runtime consequence** — low-medium, medium, [I]. Add `host/fxr/<ver>`, `metadata/workloads/<band>`, `library-packs/`; the confirmation must say the major's runtimes go too (`net8.0` tests built with SDK 10 need them).
12. **`POST /prune` with a body on an older daemon runs a standard prune** — low-medium, high, [V]. Old handler ignores the body (`server.go:201–207`). Use `POST /prune/{scope}` (plain-text 404 on old daemons). `decode` 400s on an empty body.
13. **Unknown tool-cache folders are invisible** — low, high, [I]. PyPy, Zulu, Ruby etc.; add "other" rows (size, no Remove); skip `.tmp`.
14. **Python `setup.sh` deletes an existing `Python/<ver>/x64`** — low, high, [V]. Only bites a marker-less folder; qualify "installs only add files".
15. **Ambiguities** — low, high, [I]. `operations.queued` count or list; install-time source (marker mtime); Java `Available` across majors; which .NET `support-phase` values count; full .NET version mapping; whether Settings keeps the Disk/Prune lines; `ghr prune` is a new command.
16. **Python runtime libraries and smoke test** — low, medium, [I]. Build links libssl3, libffi8, libsqlite3-0, liblzma5, libbz2, libgdbm6, libncursesw6, libreadline8; acceptance should `import ssl, sqlite3, ctypes, lzma, bz2, zlib`.
17. **Register acceptance cells empty** — low, high, [V]. Write §9's LXC rows back; note Docker rows beyond item 4 are a deliberate extension.
18. **Web UI fixture list misses `model.Storage`** — low, high, [V].
19. **Measurer cost** — low, medium, [I]. 10⁵–10⁶ files in some caches; consider a 30-minute idle cadence and Lstat-only traversal.

## Claims verified correct

- tool-cache `find()` layout and marker, `semver.clean`, `RUNNER_TOOL_CACHE`; `cacheDir` copies source contents.
- setup-node: cache first, `tar xz --strip 1`, `node/<ver>/<arch>`.
- setup-go: `tc.find('go', …)`; go.dev JSON with per-file `sha256`; top `go/` stripped for dist archives.
- setup-python: cache first; manifest linux files all carry `platform_version` (22.04/24.04/26.04, no checksums), so Debian `VERSION_ID=13` matches nothing; bundled `setup.sh` needs no sudo, writes only under the tool cache, writes the marker.
- setup-java: `Java_Temurin-Hotspot_jdk`, `+`→`-`, extracted top folder cached into `<arch>`; Adoptium `feature_releases/<n>/ga` returns `binaries[].package.checksum`.
- setup-dotnet: `DOTNET_INSTALL_DIR`, skips download when a local SDK satisfies the request with `check-latest: false`; `releases-index.json` fields `channel-version`, `latest-sdk`, `support-phase`, `release-type`.
- `docker builder prune -af` removes all unused build cache.
- Existing ghr behavior: `forcedPrune` steps, prune/update reservation, `/status` composition, older-daemon detection, `/api/*` auth mounting, `setup.sh` ordering.
