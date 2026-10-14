# Toolchains and caches

ghr installs toolchains into the runners' shared tool cache, `/var/lib/ghr/toolcache`, in the layout each `setup-*` action reads. `setup-node`, `setup-python`, `setup-go`, `setup-java` (Temurin) and `setup-dotnet` then use the cached version instead of downloading it on every job.

## Commands

| Command | Effect |
|---|---|
| `ghr toolchain list` | installed toolchains |
| `ghr toolchain available <tool>` | versions available for `node`, `go`, `python`, `java` or `dotnet` |
| `ghr toolchain install <tool> <version>` | a full version, a partial one (`22`, `3.13`), `latest`, a Java major (`21`) or a .NET channel (`8.0`) |
| `ghr toolchain install --preset popular` | Node 22 and 24, .NET 8.0 and 10.0, Python 3.13 and 3.14, Go latest, Java 21 and 25 |
| `ghr toolchain rm <tool> <version>` | remove a toolchain |
| `ghr cache list` | package caches |
| `ghr cache clear <name>` | `nuget`, `npm`, `pnpm`, `yarn`, `pip`, `gomod`, `gobuild`, `maven`, `gradle` or `cargo` |
| `ghr storage [refresh]` | Docker's disk (images, containers, volumes, build cache by type), the last prune, toolchains, other tool-cache folders, package caches and the running operation; `refresh` measures now |
| `ghr prune [--scope <scope>]` | `standard` (the default), `build-cache-keep`, `build-cache-all`, `dangling-images` or `unused-volumes` (anonymous volumes no container uses) |

Installs, removals and clears queue and run one at a time; follow them with `ghr storage`. Removing a toolchain, clearing a cache and `--scope unused-volumes` are refused while any runner has a job or is starting. While one runs, idle runners are stopped and no runner starts; they come back on the next tick after it ends. The same actions are under Settings → Toolchains and Settings → Storage in the web UI.

ghr also prunes Docker on its own when the disk use of Docker's data root passes `disk_high_water`, keeping `build_cache_keep` of build cache.

## The popular preset at install

`setup.sh` queues the popular preset on a first install once ghr is configured, and returns without waiting. While the owner and token are not set yet, the preset stays pending and is offered in the setup wizard, or can be queued with `ghr toolchain install --preset popular`. If the first run fails before the preset is queued, the next run queues it. `GHR_TOOLCHAINS=popular` or `GHR_TOOLCHAINS=none` overrides this either way.

## Python on Debian and Ubuntu

The Python builds `setup-python` uses are made for Ubuntu. ghr installs the builds made for the host's Ubuntu release; on Debian it installs Ubuntu 24.04's, and `setup.sh` installs the libraries they link.

On Debian, Python must come from the tool cache: without a cached version, `setup-python` matches its download against `VERSION_ID` in `/etc/os-release` and finds none for Debian 13.

## .NET

Removing the last .NET SDK of a major version also removes that major's runtimes and packs, so a job that builds with a newer SDK but runs `net8.0` tests loses them.
