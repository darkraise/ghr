#!/usr/bin/env bash
# Installs or upgrades ghr and native GitHub Actions runners on a Debian 13 or
# Ubuntu 22.04, 24.04 or 26.04 LXC.
# Idempotent: re-run to upgrade Docker, the runner and ghr. Never overwrites config or token.
#   bash setup.sh   (asks for the web password, the GitHub owner and the PAT; Enter skips any)
#   curl -fsSL https://github.com/darkraise/ghr/releases/latest/download/setup.sh | bash
#     (no prompts: finish in the browser with the setup code printed at the end; only
#     stdin is the pipe, so the output still reaches the terminal;
#     bash <(curl -fsSL ...) keeps the prompts)
# Optional: GHR_VERSION=v0.1.1 RUNNER_VERSION=2.337.0
#   GHR_TOOLCHAINS=popular|none (default: popular on a first install, none on an upgrade)
#   GHR_OWNER=... and GHR_TOKEN=... configure GitHub for scripts that cannot answer the
#     prompts; GHR_TOKEN is ignored without an owner. Whatever is skipped is finished in
#     the web UI at /setup, or with: ghr setup github --owner <owner>
#   GHR_WEB_PASSWORD=... sets the web UI password on a first install (else it asks; Enter skips),
#     so the browser does not ask for the setup code.
set -euo pipefail

GHR_REPO="darkraise/ghr"
GHR_VERSION="${GHR_VERSION:-latest}"
RUNNER_VERSION="${RUNNER_VERSION:-latest}"
GHR_TOOLCHAINS="${GHR_TOOLCHAINS:-}"
# The python-versions builds setup-python installs link these at run time.
PYTHON_RUNTIME_LIBS=(libssl3t64 libffi8 libsqlite3-0 liblzma5 libbz2-1.0 libgdbm6t64 libncursesw6 libreadline8t64 libtcl8.6 libtk8.6)
# Docker's repository for this distribution; check_host sets it from os-release.
DOCKER_DISTRO=debian
RUNNER_USER=ghrunner
# The daemon hard-codes these paths; they are variables only so tests can redirect them.
OPT_DIR=/opt/ghr
DIST_DIR="$OPT_DIR/dist"
HOOKS_DIR="$OPT_DIR/hooks"
BIN_DIR=/usr/local/bin
STATE_DIR=/var/lib/ghr
ETC_DIR=/etc/ghr
APT_SOURCES_DIR=/etc/apt/sources.list.d
SYSTEMD_DIR=/etc/systemd/system
KEYRING_DIR=/etc/apt/keyrings
OS_RELEASE=/etc/os-release
DOCKER_KEY_FINGERPRINT=9DC858229FC7DD38854AE2D88D81803C0EBFCD88
READY_TIMEOUT=60
# Temporary directories the exit trap removes if the script dies before it can.
CLEANUP=()
# set_web_password sets it when it leaves the web UI without a password.
WEB_UNCLAIMED=

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }

check_host() {
  [ "$(id -u)" -eq 0 ] || die "run as root"
  # shellcheck disable=SC1090,SC1091
  . "$OS_RELEASE"
  case "${ID:-}:${VERSION_ID:-}" in
    debian:13 | ubuntu:24.04 | ubuntu:26.04) ;;
    # 22.04 predates the 64-bit time_t renames.
    ubuntu:22.04) PYTHON_RUNTIME_LIBS=(libssl3 libffi8 libsqlite3-0 liblzma5 libbz2-1.0 libgdbm6 libncursesw6 libreadline8 libtcl8.6 libtk8.6) ;;
    *) die "Debian 13 or Ubuntu 22.04, 24.04 or 26.04 required (found ${PRETTY_NAME:-unknown})" ;;
  esac
  [ "$(dpkg --print-architecture)" = amd64 ] ||
    die "ghr supports amd64 (x86_64) hosts only (found $(dpkg --print-architecture))"
  DOCKER_DISTRO=$ID
}

# write_docker_source writes Docker's repository in the deb822 form Docker's
# install guide uses. Earlier runs wrote a one-line docker.list; keeping both
# makes apt warn that every Docker target is configured twice.
write_docker_source() {
  cat > "$APT_SOURCES_DIR/docker.sources" <<EOF
Types: deb
URIs: https://download.docker.com/linux/${DOCKER_DISTRO}
Suites: ${VERSION_CODENAME}
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: $KEYRING_DIR/docker.asc
EOF
  rm -f "$APT_SOURCES_DIR/docker.list"
}

# Downloads Docker's apt key into $1, imports it into a throwaway keyring and
# writes gpg's export of the pinned key to $1/docker.asc. Every step is in one
# && chain, because errexit does not apply inside a function called from `if`.
export_docker_key() {
  local work="$1" home="$1/gnupg" records fpr
  mkdir -m 0700 "$home" &&
    curl -fsSL "https://download.docker.com/linux/$DOCKER_DISTRO/gpg" -o "$work/download.asc" &&
    gpg --homedir "$home" --batch --no-tty --quiet --import "$work/download.asc" &&
    records=$(gpg --homedir "$home" --batch --no-tty --with-colons --list-keys "$DOCKER_KEY_FINGERPRINT") &&
    fpr=$(awk -F: '$1 == "pub" { n++ } prev == "pub" && $1 == "fpr" { f = $10 } { prev = $1 } END { if (n == 1) print f }' <<<"$records") &&
    [ "$fpr" = "$DOCKER_KEY_FINGERPRINT" ] &&
    gpg --homedir "$home" --batch --no-tty --armor --export "$DOCKER_KEY_FINGERPRINT" > "$work/docker.asc" &&
    [ "$(head -n 1 "$work/docker.asc")" = "-----BEGIN PGP PUBLIC KEY BLOCK-----" ]
}

# apt trusts every key in a Signed-By file and may read a crafted file differently
# from gpg, so the downloaded bytes are never installed: the key is re-exported by
# gpg, which writes exactly the pinned key and nothing else.
install_docker_key() {
  local work status=0
  work=$(mktemp -d) || die "could not create a temporary directory"
  export_docker_key "$work" || status=1
  # gpg --import starts a gpg-agent for the throwaway keyring.
  gpgconf --homedir "$work/gnupg" --kill all >/dev/null 2>&1 || true
  if [ "$status" = 0 ]; then
    install -m 0755 -d "$KEYRING_DIR" && install -m 0644 "$work/docker.asc" "$KEYRING_DIR/docker.asc" || status=2
  fi
  rm -rf "$work"
  [ "$status" != 1 ] || die "Docker apt key has an unexpected fingerprint"
  [ "$status" != 2 ] || die "could not install Docker's apt key"
}

install_docker() {
  log "installing or upgrading Docker Engine and base packages"
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl gnupg
  install_docker_key
  write_docker_source
  apt-get update -qq
  # Re-runs upgrade installed packages; keep locally edited conffiles (e.g.
  # /etc/containerd/config.toml) instead of stopping at a dpkg prompt.
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
    -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold \
    docker-ce docker-ce-cli containerd.io docker-compose-plugin docker-buildx-plugin \
    git curl jq unzip build-essential ca-certificates "${PYTHON_RUNTIME_LIBS[@]}"
  systemctl enable --now docker >/dev/null
  docker info >/dev/null 2>&1 || die "docker is not working; the LXC needs features: nesting=1,keyctl=1"
}

create_user_and_dirs() {
  id "$RUNNER_USER" >/dev/null 2>&1 || useradd --create-home --shell /bin/bash "$RUNNER_USER"
  usermod -aG docker "$RUNNER_USER"
  install -d -m 0755 "$OPT_DIR" "$DIST_DIR" "$HOOKS_DIR" \
    "$STATE_DIR" "$STATE_DIR/instances" "$STATE_DIR/logs" "$STATE_DIR/pending"
  install -d -m 0700 "$ETC_DIR"
  install -d -m 0755 -o "$RUNNER_USER" -g "$RUNNER_USER" "$STATE_DIR/toolcache"
}

# Downloads the actions-runner tarball for $1 to $2 and verifies its SHA-256.
fetch_runner() {
  local version="$1" out="$2" tgz="actions-runner-linux-x64-$1.tar.gz" want
  curl -fsSL -o "$out" "https://github.com/actions/runner/releases/download/v$version/$tgz"
  want=$(curl -fsSL "https://api.github.com/repos/actions/runner/releases/tags/v$version" | jq -r .body |
    grep -oP '(?<=<!-- BEGIN SHA linux-x64 -->)[0-9a-f]{64}(?=<!-- END SHA linux-x64 -->)') || true
  [ -n "$want" ] || die "no linux-x64 checksum in the v$version release notes"
  echo "$want  $out" | sha256sum -c - >/dev/null || die "actions-runner checksum mismatch"
}

# The daemon installs queued runner updates into the same dist dir, so it must
# not run while this script installs one. Runner units outlive it, and
# install_service starts it again; on a first install there is no unit yet.
# If a later step fails, the exit trap starts the old daemon again.
# A queued update is dropped while the daemon is down, so the restarted daemon
# does not install a runner version over the one this script just installed.
# The version it already warned about is kept, or it would warn again.
stop_ghr() {
  trap 'rm -rf "${CLEANUP[@]}"; systemctl start ghr.service 2>/dev/null || true' EXIT
  if systemctl cat ghr.service >/dev/null 2>&1; then
    systemctl stop ghr.service || die "could not stop ghr.service; stop it and re-run"
    case "$(systemctl is-active ghr.service || true)" in
      active | deactivating) die "could not stop ghr.service; stop it and re-run" ;;
    esac
  fi
  local update="$STATE_DIR/runner-update.json"
  if [ -e "$update" ]; then
    jq 'del(.queued_at)' "$update" > "$update.tmp" && mv -f "$update.tmp" "$update" || rm -f "$update" "$update.tmp"
  fi
}

# A version an older setup.sh extracted kept the tarball's owner ids (1001),
# which may be a real user here, so whatever that user could write is not
# vouched for by its run.sh.
tree_is_root_owned() {
  [ -z "$(find "$1" ! -uid 0 -print -quit)" ]
}

install_runner() {
  local version="${RUNNER_VERSION#v}"
  if [ "$version" = latest ]; then
    version=$(curl -fsSL https://api.github.com/repos/actions/runner/releases/latest | jq -r .tag_name)
    version="${version#v}"
  fi
  local dir="$DIST_DIR/$version" staging="$DIST_DIR/$version.tmp"
  # A dist dir is published only after installdependencies.sh succeeded, so an
  # executable run.sh means the version is complete.
  if [ -d "$dir" ] && ! tree_is_root_owned "$dir"; then
    log "actions-runner $version has files not owned by root; fetching it again"
    rm -rf "$dir"
  fi
  if [ ! -x "$dir/run.sh" ]; then
    log "installing actions-runner $version"
    rm -rf "$staging"
    mkdir -p "$staging"
    fetch_runner "$version" "$staging/runner.tar.gz"
    tar --no-same-owner -xzf "$staging/runner.tar.gz" -C "$staging"
    rm -f "$staging/runner.tar.gz"
    "$staging/bin/installdependencies.sh" >/dev/null || die "actions-runner $version: bin/installdependencies.sh failed"
    rm -rf "$dir"
    mv -T "$staging" "$dir"
  fi
  chown -R root:root "$dir"
  chmod -R go-w "$dir"
  ln -sfn "$dir" "$DIST_DIR/current.tmp"
  mv -Tf "$DIST_DIR/current.tmp" "$DIST_DIR/current"
}

install_ghr() {
  local base tmp hook
  if [ "$GHR_VERSION" = latest ]; then
    base="https://github.com/$GHR_REPO/releases/latest/download"
  else
    base="https://github.com/$GHR_REPO/releases/download/$GHR_VERSION"
  fi
  log "installing ghr ($GHR_VERSION)"
  tmp=$(mktemp -d)
  CLEANUP+=("$tmp")
  curl -fsSL -o "$tmp/ghr_linux_amd64.tar.gz" "$base/ghr_linux_amd64.tar.gz"
  curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"
  (cd "$tmp" && sha256sum -c checksums.txt >/dev/null) || die "ghr checksum mismatch"
  tar -xzf "$tmp/ghr_linux_amd64.tar.gz" -C "$tmp"
  # Written beside the target and renamed over it, so an interrupted run leaves
  # the old file or the complete new one, never half of it.
  install -m 0755 "$tmp/ghr/ghr" "$BIN_DIR/.ghr.new"
  mv -f "$BIN_DIR/.ghr.new" "$BIN_DIR/ghr"
  for hook in job-started.sh job-completed.sh; do
    install -m 0755 "$tmp/ghr/$hook" "$HOOKS_DIR/.$hook.new"
    mv -f "$HOOKS_DIR/.$hook.new" "$HOOKS_DIR/$hook"
  done
  if [ -f "$tmp/ghr/config.example.yaml" ]; then
    install -m 0644 "$tmp/ghr/config.example.yaml" "$OPT_DIR/config.example.yaml"
  fi
  rm -rf "$tmp"
}

install_config() {
  if [ ! -f "$ETC_DIR/config.yaml" ]; then
    [ -f "$OPT_DIR/config.example.yaml" ] ||
      die "the ghr $GHR_VERSION release has no config.example.yaml; install a newer GHR_VERSION"
    install -m 0600 "$OPT_DIR/config.example.yaml" "$ETC_DIR/config.yaml"
    log "wrote $ETC_DIR/config.yaml from config.example.yaml"
    # Files, not variables: a first install that dies before preinstall_toolchains,
    # the setup wizard or the web password prompt must still find them when re-run.
    touch "$STATE_DIR/toolchains-pending" "$STATE_DIR/setup-pending" "$STATE_DIR/web-password-pending"
  fi
}

install_service() {
  local unit="$SYSTEMD_DIR/ghr.service" want reload=0
  want=$(cat <<'EOF'
[Unit]
Description=ghr GitHub Actions runner manager
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
ExecStart=/usr/local/bin/ghr daemon
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target docker.service
EOF
  )
  if [ "$(cat "$unit" 2>/dev/null)" != "$want" ]; then
    printf '%s\n' "$want" > "$unit"
    reload=1
  fi
  [ -e "$SYSTEMD_DIR/docker.service.wants/ghr.service" ] || reload=1
  # Requires= alone stops ghr with Docker and never starts it again; the
  # docker.service.wants link made by WantedBy= does. Without --no-reload,
  # reenable reloads systemd itself on every run.
  systemctl --no-reload reenable ghr.service >/dev/null
  [ "$reload" = 0 ] || systemctl daemon-reload
  systemctl restart ghr.service
}

# Polls `ghr status` until the API answers. A configured daemon adopts its runners
# and reconciles with GitHub before it serves, so this can take several seconds.
wait_ready() {
  local deadline=$((SECONDS + READY_TIMEOUT)) out state
  log "waiting for ghr to become ready (up to ${READY_TIMEOUT}s)"
  while :; do
    if out=$(timeout 5 ghr status 2>&1); then
      printf '%s\n' "$out"
      return 0
    fi
    # Restart=always keeps a crashing daemon in "activating" (auto-restart), never
    # "failed", so with Type=simple anything but "active" means it exited.
    state=$(systemctl is-active ghr.service || true)
    [ "$state" = active ] || die "ghr service failed (systemd state: $state); see: journalctl -u ghr -n 50"
    [ "$SECONDS" -lt "$deadline" ] ||
      die "ghr service still starting after ${READY_TIMEOUT}s; check: systemctl status ghr; journalctl -u ghr -f"
    sleep 1
  done
}

# A first install turns the web UI on (config.example.yaml); until a password
# is set, the browser asks for the setup code print_next shows. The marker stays
# until a password is set or the owner skipped the prompt, so a re-run after a
# failed first install, or one without a terminal, asks again.
set_web_password() {
  local pending="$STATE_DIR/web-password-pending" pw="${GHR_WEB_PASSWORD:-}" out
  [ -f "$pending" ] || return 0
  if [ -z "$pw" ] && stdin_is_tty; then
    # Only a line that was read and is empty is a skip. End of input or a failed
    # read keeps the marker and drops whatever it left in $pw.
    if IFS= read -rsp "Web UI password (12 to 1024 characters; Enter to skip): " pw; then
      [ -n "$pw" ] || rm -f "$pending"
    else
      pw=
    fi
    echo
  fi
  if [ -n "$pw" ]; then
    if out=$(printf '%s' "$pw" | ghr web set-password 2>&1); then
      rm -f "$pending"
      log "$out"
      return 0
    fi
    warn "${out#ghr: }"
  fi
  WEB_UNCLAIMED=1
}

stdin_is_tty() { [ -t 0 ]; }

# Prints one field of `ghr setup` (configured, wizard, owner, web), or nothing
# when the daemon does not answer.
setup_field() {
  timeout 10 ghr setup 2>/dev/null | sed -n "s/^$1: //p" || true
}

# Asks for the GitHub owner and token while ghr is not configured, on every run,
# so an answer skipped or refused earlier can be given later. Enter skips either.
configure_github() {
  [ "$(setup_field configured)" = no ] || return 0
  local owner="${GHR_OWNER:-}" tok="${GHR_TOKEN:-}" out
  if [ -z "$owner" ]; then
    owner=$(setup_field owner)
    [ "$owner" != - ] || owner=
  fi
  if [ -z "$owner" ] && stdin_is_tty; then
    read -rp "GitHub owner (user or org; Enter to finish in the browser): " owner || true
  fi
  if [ -z "$owner" ]; then
    [ -z "$tok" ] || warn "GHR_TOKEN is ignored without an owner; set GHR_OWNER"
    return 0
  fi
  if [ -z "$tok" ] && stdin_is_tty; then
    read -rsp "GitHub fine-grained PAT (Administration: read/write, Actions: read; Enter to finish in the browser): " tok || true
    echo
  fi
  [ -n "$tok" ] || return 0
  if out=$(printf '%s' "$tok" | timeout 90 ghr setup github --owner "$owner" 2>&1); then
    log "$out"
  else
    warn "${out#ghr: }"
  fi
}

# Says what is left of first-run setup and, while the web UI has no password,
# ends with the setup code its browser asks for.
print_next() {
  local configured wizard web host code
  configured=$(setup_field configured)
  [ -n "$configured" ] || return 0
  wizard=$(setup_field wizard)
  web=$(setup_field web)
  [ "$web" != - ] || web=
  if [ "$configured" != yes ] || [ "$wizard" = pending ]; then
    if [ -n "$web" ]; then
      host=${web%:*}
      case "$host" in
        "" | 0.0.0.0 | :: | "[::]")
          host=$(hostname -I 2>/dev/null | awk '{print $1}' || true)
          [[ "$host" != *:* ]] || host="[$host]"
          ;;
      esac
      log "finish setup in the browser: http://${host:-<this host>}:${web##*:}/setup"
    elif [ "$configured" != yes ]; then
      log "finish setup with: ghr setup github --owner <owner>"
    else
      log "end first-run setup with: ghr setup finish"
    fi
  fi
  [ -n "$web" ] || return 0
  # ghr code also fails once a password is set, so only a password this run
  # left unset makes the failure worth a reminder.
  code=$(timeout 10 ghr code 2>/dev/null) || code=
  if [ -n "$code" ]; then
    log "web UI setup code: $code (the browser asks for it once; or set a password now with: ghr web set-password)"
  elif [ -n "$WEB_UNCLAIMED" ]; then
    log "the web UI has no password; set one with: ghr web set-password"
  fi
}

# Runs while the daemon is stopped, after install_runner has switched `current`.
# An instance directory is a full copy of the runner files, so nothing reads an
# old version directory in the meantime, and no update can install into dist
# while it is removing from it.
gc_dist() {
  local keep old
  keep=$(readlink -e "$DIST_DIR/current") || return 0
  keep=$(basename "$keep")
  for old in "$DIST_DIR"/*/; do
    old=$(basename "$old")
    if [ "$old" != "$keep" ] && [ "$old" != current ]; then
      log "removing old runner $old"
      rm -rf "${DIST_DIR:?}/$old"
    fi
  done
}

check_toolchains() {
  case "$GHR_TOOLCHAINS" in
    "" | popular | none) ;;
    *) die "GHR_TOOLCHAINS must be popular or none (got: $GHR_TOOLCHAINS)" ;;
  esac
}

# Queues the popular toolchains on a first install (until one queue
# succeeds), or when GHR_TOOLCHAINS asks, and returns at once: the daemon
# installs them in the background. A toolchain the owner removed is not
# queued again by an upgrade.
preinstall_toolchains() {
  local pending="$STATE_DIR/toolchains-pending" mode="$GHR_TOOLCHAINS"
  if [ -z "$mode" ]; then
    mode=none
    [ ! -f "$pending" ] || mode=popular
  fi
  if [ "$mode" != popular ]; then
    rm -f "$pending"
    return 0
  fi
  case "$(setup_field configured)" in
    no | starting)
      log "toolchains can be chosen in the setup wizard, or later with: ghr toolchain install --preset popular"
      return 0
      ;;
  esac
  if timeout 30 ghr toolchain install --preset popular; then
    rm -f "$pending"
    log "popular toolchains queued; they install in the background"
  else
    warn "could not queue the popular toolchains; queue them later with: ghr toolchain install --preset popular"
  fi
}

main() {
  check_host
  check_toolchains
  install_docker
  create_user_and_dirs
  stop_ghr
  install_runner
  gc_dist
  install_ghr
  install_config
  install_service
  trap - EXIT
  wait_ready
  set_web_password
  configure_github
  preinstall_toolchains
  print_next
}

# Piped into bash (curl ... | bash), BASH_SOURCE is empty; sourced by the tests, it
# differs from $0. main runs last, so a truncated download runs nothing.
if [[ -z "${BASH_SOURCE[0]:-}" || "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
