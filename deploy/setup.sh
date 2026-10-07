#!/usr/bin/env bash
# Installs or upgrades ghr and native GitHub Actions runners on a Debian 13 LXC.
# Idempotent: re-run to upgrade Docker, the runner and ghr. Never overwrites config or token.
#   bash setup.sh   (asks for the GitHub owner, the PAT and the web password; Enter skips any)
#   curl -fsSL https://github.com/darkraise/ghr/releases/latest/download/setup.sh | bash
#     (no prompts: finish in the browser; bash <(curl -fsSL ...) keeps the prompts)
# Optional: GHR_VERSION=v0.1.1 RUNNER_VERSION=2.337.0
#   GHR_TOOLCHAINS=popular|none (default: popular on a first install, none on an upgrade)
#   GHR_OWNER=... and GHR_TOKEN=... configure GitHub for scripts that cannot answer the
#     prompts; GHR_TOKEN is ignored without an owner. Whatever is skipped is finished in
#     the web UI at /setup, or with: ghr setup github --owner <owner>
#   GHR_WEB_PASSWORD=... sets the web UI password on a first install (else it asks; Enter skips).
set -euo pipefail

GHR_REPO="darkraise/ghr"
GHR_VERSION="${GHR_VERSION:-latest}"
RUNNER_VERSION="${RUNNER_VERSION:-latest}"
GHR_TOOLCHAINS="${GHR_TOOLCHAINS:-}"
# The python-versions builds setup-python installs link these at run time.
PYTHON_RUNTIME_LIBS=(libssl3t64 libffi8 libsqlite3-0 liblzma5 libbz2-1.0 libgdbm6t64 libncursesw6 libreadline8t64 libtcl8.6 libtk8.6)
RUNNER_USER=ghrunner
# The daemon hard-codes these paths; they are variables only so tests can redirect them.
OPT_DIR=/opt/ghr
DIST_DIR="$OPT_DIR/dist"
HOOKS_DIR="$OPT_DIR/hooks"
STATE_DIR=/var/lib/ghr
ETC_DIR=/etc/ghr
APT_SOURCES_DIR=/etc/apt/sources.list.d
READY_TIMEOUT=60
FIRST_INSTALL=0

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }

check_host() {
  [ "$(id -u)" -eq 0 ] || die "run as root"
  # shellcheck disable=SC1091
  . /etc/os-release
  { [ "${ID:-}" = debian ] && [ "${VERSION_ID:-}" = 13 ]; } || die "Debian 13 required (found ${PRETTY_NAME:-unknown})"
}

# write_docker_source writes Docker's repository in the deb822 form Docker's
# install guide uses. Earlier runs wrote a one-line docker.list; keeping both
# makes apt warn that every Docker target is configured twice.
write_docker_source() {
  cat > "$APT_SOURCES_DIR/docker.sources" <<EOF
Types: deb
URIs: https://download.docker.com/linux/debian
Suites: ${VERSION_CODENAME}
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
  rm -f "$APT_SOURCES_DIR/docker.list"
}

install_docker() {
  log "installing or upgrading Docker Engine and base packages"
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
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
# A queued update is dropped while the daemon is down: once it is back up it
# could start installing into dist while gc_dist is still removing from it.
stop_ghr() {
  systemctl stop ghr.service 2>/dev/null || true
  rm -f "$STATE_DIR/runner-update.json"
  trap 'systemctl start ghr.service 2>/dev/null || true' EXIT
}

install_runner() {
  local version="$RUNNER_VERSION"
  if [ "$version" = latest ]; then
    version=$(curl -fsSL https://api.github.com/repos/actions/runner/releases/latest | jq -r .tag_name)
    version="${version#v}"
  fi
  local dir="$DIST_DIR/$version" staging="$DIST_DIR/$version.tmp"
  # A dist dir is published only after installdependencies.sh succeeded, so an
  # executable run.sh means the version is complete.
  if [ ! -x "$dir/run.sh" ]; then
    log "installing actions-runner $version"
    rm -rf "$staging"
    mkdir -p "$staging"
    fetch_runner "$version" "$staging/runner.tar.gz"
    tar -xzf "$staging/runner.tar.gz" -C "$staging"
    rm -f "$staging/runner.tar.gz"
    "$staging/bin/installdependencies.sh" >/dev/null || die "actions-runner $version: bin/installdependencies.sh failed"
    mv -T "$staging" "$dir"
  fi
  ln -sfn "$dir" "$DIST_DIR/current.tmp"
  mv -Tf "$DIST_DIR/current.tmp" "$DIST_DIR/current"
}

install_ghr() {
  local base tmp
  if [ "$GHR_VERSION" = latest ]; then
    base="https://github.com/$GHR_REPO/releases/latest/download"
  else
    base="https://github.com/$GHR_REPO/releases/download/$GHR_VERSION"
  fi
  log "installing ghr ($GHR_VERSION)"
  tmp=$(mktemp -d)
  curl -fsSL -o "$tmp/ghr_linux_amd64.tar.gz" "$base/ghr_linux_amd64.tar.gz"
  curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"
  (cd "$tmp" && sha256sum -c checksums.txt >/dev/null) || die "ghr checksum mismatch"
  tar -xzf "$tmp/ghr_linux_amd64.tar.gz" -C "$tmp"
  install -m 0755 "$tmp/ghr/ghr" /usr/local/bin/ghr
  install -m 0755 "$tmp/ghr/job-started.sh" "$tmp/ghr/job-completed.sh" "$HOOKS_DIR/"
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
    FIRST_INSTALL=1
    # Files, not variables: a first install that dies before preinstall_toolchains
    # or the setup wizard must still find them when re-run.
    touch "$STATE_DIR/toolchains-pending" "$STATE_DIR/setup-pending"
  fi
}

install_service() {
  cat > /etc/systemd/system/ghr.service <<'EOF'
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
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable ghr.service >/dev/null
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
# is set, the first visitor to the web UI sets it.
set_web_password() {
  [ "$FIRST_INSTALL" = 1 ] || return 0
  local pw="${GHR_WEB_PASSWORD:-}" out
  if [ -z "$pw" ] && [ -t 0 ]; then
    IFS= read -rsp "Web UI password (12 to 1024 bytes; Enter to skip): " pw || true
    echo
  fi
  if [ -n "$pw" ]; then
    if out=$(printf '%s' "$pw" | ghr web set-password 2>&1); then
      log "$out"
      return 0
    fi
    warn "${out#ghr: }"
  fi
  log "web UI on :8080 has no password; set it now with: ghr web set-password"
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

# Says what is left of first-run setup.
print_next() {
  local configured wizard web host
  configured=$(setup_field configured)
  [ -n "$configured" ] || return 0
  wizard=$(setup_field wizard)
  web=$(setup_field web)
  [ "$configured" != yes ] || [ "$wizard" = pending ] || return 0
  if [ -n "$web" ] && [ "$web" != - ]; then
    host=$(hostname -I 2>/dev/null | awk '{print $1}' || true)
    log "finish setup in the browser: http://${host:-<this host>}:${web##*:}/setup"
  elif [ "$configured" != yes ]; then
    log "finish setup with: ghr setup github --owner <owner>"
  else
    log "end first-run setup with: ghr setup finish"
  fi
}

# Must run only after the restarted daemon is ready: the old daemon may have been
# copying a non-current dist dir into a new instance until it stopped, and the new
# one only reads `current`.
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
  install_ghr
  install_config
  install_service
  trap - EXIT
  wait_ready
  configure_github
  set_web_password
  gc_dist
  preinstall_toolchains
  print_next
}

# Piped into bash (curl ... | bash), BASH_SOURCE is empty; sourced by the tests, it
# differs from $0. main runs last, so a truncated download runs nothing.
if [[ -z "${BASH_SOURCE[0]:-}" || "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
