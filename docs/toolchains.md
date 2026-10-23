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

Installs, removals and clears queue and run one at a time; follow them with `ghr storage`. Removing a toolchain, clearing a cache and `--scope unused-volumes` are refused while any runner has a job or is starting. While one runs, idle runners are stopped and no runner starts; they come back on the next tick after it ends. These actions are refused when GitHub does not answer and idle runners exist. The same actions are under Settings → Toolchains and Settings → Storage in the web UI.

ghr also prunes Docker on its own when the disk use of Docker's data root passes `disk_high_water`, keeping `build_cache_keep` of build cache.

## System packages

ghr also manages command-line tools that jobs need and that come as apt packages, such as `ripgrep`, `shellcheck` or `zstd`. It installs them from the apt sources the host already has; it never adds a source or downloads a binary itself. The managed list is `packages:` in `config.yaml`, so a copied config, or a re-run of `setup.sh` on a new host, installs the same packages.

| Command | Effect |
|---|---|
| `ghr package list` | managed packages with their version, available upgrade, size and last error; the packages `setup.sh` installs (base); and packages installed by hand that ghr can adopt (detected) |
| `ghr package search <term>` | up to 50 packages whose name matches, each marked managed or installed |
| `ghr package install <name>...` | installs each name that is not installed and adds it to the list; an installed name is adopted: added to the list, nothing installed |
| `ghr package upgrade <name>...` or `--all` | refreshes the apt index, then upgrades the named managed packages, or all of them, that have a newer version |
| `ghr package uninstall <name>` | removes the package with `dpkg --remove`, then drops it from the list |
| `ghr package forget <name>` | drops the package from the list and leaves it installed |
| `ghr package refresh` | refreshes the apt index (`apt-get update`); fails, naming the source, when any apt source cannot be downloaded |

Package work joins the toolchain queue and runs one operation at a time; follow it with `ghr storage`. The web UI has the same actions in the System packages section of Settings → Toolchains. On a host without apt, every package command answers `apt is not available on this host`.

**Runners.** Upgrade and uninstall are refused while any runner has a job or is starting, and stop idle runners while they run, as removing a toolchain does. An install does the same only when it would also upgrade a package that is already installed; an install that only adds packages runs beside jobs. Adopt, forget and refresh never touch the runners.

**What ghr refuses.**

- A name must be 2 to 63 characters of lowercase letters, digits, `+`, `.` and `-`, starting with a letter or digit, without an architecture suffix.
- ghr does not install, upgrade or uninstall a base package. A base package in the list, such as one from a config copied from another release, shows as base and can only be forgotten.
- Adopting takes installed packages of priority `optional` or `extra` outside the `libs` and `oldlibs` sections, so the libraries the runner needs and the base system cannot be adopted and then uninstalled.
- Install and upgrade never remove a package. When apt would have to remove one, it stops with `Packages need to be removed but remove is disabled`, and `config.yaml` is unchanged.
- dpkg refuses an uninstall while another installed package depends on the package (`dependency problems - not removing`); the package stays in the list. ghr never purges a package and never runs autoremove.
- An install or upgrade waits up to 120 seconds for dpkg's lock, held for instance by unattended-upgrades. An index refresh fails at once while another index update runs, and an uninstall fails at once with dpkg's lock message; the next refresh or a retry succeeds once the other update ends. An adopt is not affected by the lock.

**Missing packages.** At start, after each config reload and after each `package_index` run, ghr queues an install of every managed package that is not installed, one operation per package. A package with no installation candidate in the host's apt sources gets an error in `ghr package list` and a warning in Events. A failed install, or one refused because runners are busy, is tried again at the next reload or `package_index` run: up to 24 hours later on the default schedule. The `package_index` background task refreshes the apt index every 24 hours (see [Background tasks](configuration.md#background-tasks)), and an install refreshes it first when ghr has not refreshed it in the last hour.

**Access.** The web UI installs packages as root without a further switch. Its password already lets its holder add repositories whose jobs run as `ghrunner`, a member of the `docker` group and so equivalent to root on the host; installing a package from the apt sources the host's administrator configured grants nothing beyond that.

## The popular preset at install

`setup.sh` queues the popular preset on a first install once ghr is configured, and returns without waiting. While the owner and token are not set yet, the preset stays pending and is offered in the setup wizard, or can be queued with `ghr toolchain install --preset popular`. If the first run fails before the preset is queued, the next run queues it. `GHR_TOOLCHAINS=popular` or `GHR_TOOLCHAINS=none` overrides this either way.

## Download integrity

What ghr checks before it installs a toolchain depends on the tool:

- **Go:** the SHA-256 that go.dev lists for the archive.
- **Java:** the SHA-256 that Adoptium's API lists for the package.
- **Node and Python:** none. The build comes from a release of `actions/node-versions` or `actions/python-versions` on GitHub over HTTPS. Python's own `setup.sh` runs as `ghrunner`.
- **.NET:** none. ghr fetches Microsoft's `dotnet-install.sh` over HTTPS and runs it as `ghrunner`; the script downloads the SDK itself.

A checksum from the same provider over the same connection catches a damaged download, not a compromised provider.

## Python on Debian and Ubuntu

The Python builds `setup-python` uses are made for Ubuntu. ghr installs the builds made for the host's Ubuntu release; on Debian it installs Ubuntu 24.04's, and `setup.sh` installs the libraries they link.

On Debian, Python must come from the tool cache: without a cached version, `setup-python` matches its download against `VERSION_ID` in `/etc/os-release` and finds none for Debian 13.

## .NET

Removing the last .NET SDK of a major version also removes that major's runtimes and packs, so a job that builds with a newer SDK but runs `net8.0` tests loses them.
