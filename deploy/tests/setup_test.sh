#!/usr/bin/env bash
# Offline tests for setup.sh: no network, root, apt or systemd. External commands are
# replaced by shell functions and every path points into a temporary directory.
#   bash tests/setup_test.sh
# shellcheck disable=SC2016,SC2317,SC2329
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR/.. source=setup.sh
. "$HERE/../setup.sh" || { echo "cannot source setup.sh" >&2; exit 1; }
set +e

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

FAILS=0
check() {
  local name="$1"
  shift
  if "$@"; then
    printf 'ok   %s\n' "$name"
  else
    printf 'FAIL %s\n' "$name"
    FAILS=$((FAILS + 1))
  fi
}
contains() { [[ "$1" == *"$2"* ]]; }
lacks() { [[ "$1" != *"$2"* ]]; }
absent() { [ ! -e "$1" ] && [ ! -L "$1" ]; }
count() { cat "$MARK_DIR/$1"; }
bump() { echo $(($(count "$1") + 1)) > "$MARK_DIR/$1"; }
# Runs a setup.sh function the way main runs: errexit on, die exits a subshell.
run() { (set -e; "$@"); }

# Git Bash without Windows symlink rights silently copies on `ln -s`; there the
# `current` link is emulated with a pointer file so the same assertions apply.
touch "$T/probe-target"
if ! { ln -s "$T/probe-target" "$T/probe-link" 2>/dev/null && [ -L "$T/probe-link" ]; }; then
  echo "note: no symlink support; emulating ln -sfn and readlink -e with pointer files"
  ln() {
    [ "$1" = -sfn ] || { echo "unexpected: ln $*" >&2; return 1; }
    rm -rf "$3" && printf '%s\n' "$2" > "$3"
  }
  readlink() {
    [ "$1" = -e ] && [ -f "$2" ] || return 1
    local target
    target=$(cat "$2")
    [ -e "$target" ] && printf '%s\n' "$target"
  }
fi
target_of_current() {
  local t
  t=$(readlink -e "$DIST_DIR/current") || return 1
  basename "$t"
}

FIXTURE_VERSION=2.337.0
FIX="$T/fixture"
mkdir -p "$FIX/bin"
printf '%s\n' '#!/usr/bin/env bash' 'echo runner' > "$FIX/run.sh"
printf '%s\n' '#!/usr/bin/env bash' \
  '[ ! -e "$DIST_DIR/$FIXTURE_VERSION" ] || touch "$MARK_DIR/published-before-deps"' \
  '[ ! -f "$MARK_DIR/fail-deps" ] || exit 1' \
  'touch "$(dirname "$0")/../deps-installed"' > "$FIX/bin/installdependencies.sh"
chmod +x "$FIX/run.sh" "$FIX/bin/installdependencies.sh"
tar -czf "$T/runner.tar.gz" -C "$FIX" .

fetch_runner() {
  bump fetches
  [ ! -f "$MARK_DIR/stopped" ] || touch "$MARK_DIR/fetched-after-stop"
  cp "$T/runner.tar.gz" "$2"
}
READY_AFTER=1
FAKE_STATE=active
ghr() {
  if [ "$1" = toolchain ]; then
    echo "$*" >> "$MARK_DIR/ghr-calls"
    [ ! -f "$MARK_DIR/fail-queue" ] || return 1
    echo "queued — follow with: ghr storage"
    return
  fi
  bump polls
  if [ "$1" = status ] && [ "$(count polls)" -ge "$READY_AFTER" ]; then
    echo "ghr status: ready"
    return 0
  fi
  echo "daemon unreachable" >&2
  return 1
}
systemctl() {
  [ "$1" != stop ] || { echo "$2" > "$MARK_DIR/stopped"; return 0; }
  [ "$1" != start ] || { echo "$2" > "$MARK_DIR/started"; return 0; }
  [ "$1" = is-active ] || return 0
  echo "$FAKE_STATE"
  [ "$FAKE_STATE" = active ]
}
timeout() { shift; "$@"; }
sleep() { SECONDS=$((SECONDS + $1)); }

new_case() {
  DIST_DIR="$T/$1/dist"
  MARK_DIR="$T/$1/mark"
  STATE_DIR="$T/$1/state"
  mkdir -p "$DIST_DIR" "$MARK_DIR" "$STATE_DIR"
  echo 0 > "$MARK_DIR/fetches"
  echo 0 > "$MARK_DIR/polls"
  READY_AFTER=1
  FAKE_STATE=active
  READY_TIMEOUT=60
  RUNNER_VERSION=$FIXTURE_VERSION
  export DIST_DIR MARK_DIR FIXTURE_VERSION
  printf '\n# %s\n' "$1"
}
old_version() {
  mkdir -p "$DIST_DIR/$1"
  cp "$FIX/run.sh" "$DIST_DIR/$1/run.sh"
}

# (a) A failed installdependencies.sh publishes nothing; a re-run installs and publishes.
new_case a-install-runner
touch "$MARK_DIR/fail-deps"
out=$(run install_runner 2>&1)
rc=$?
check "a: failing installdependencies makes install_runner fail" [ "$rc" -ne 0 ]
check "a: error names installdependencies" contains "$out" "installdependencies.sh failed"
check "a: no published dist dir after failure" absent "$DIST_DIR/$FIXTURE_VERSION"
check "a: current not created after failure" absent "$DIST_DIR/current"
touch "$DIST_DIR/$FIXTURE_VERSION.tmp/junk-from-failed-run"
rm "$MARK_DIR/fail-deps"
out=$(run install_runner 2>&1)
rc=$?
check "a: re-run succeeds" [ "$rc" -eq 0 ]
check "a: re-run publishes an executable run.sh" [ -x "$DIST_DIR/$FIXTURE_VERSION/run.sh" ]
check "a: published dir has dependencies installed" [ -f "$DIST_DIR/$FIXTURE_VERSION/deps-installed" ]
check "a: dependencies ran before publishing" absent "$MARK_DIR/published-before-deps"
check "a: leftover temp dir from the failed run was discarded" absent "$DIST_DIR/$FIXTURE_VERSION/junk-from-failed-run"
check "a: no temp dir left after success" absent "$DIST_DIR/$FIXTURE_VERSION.tmp"
check "a: tarball not kept in the dist dir" absent "$DIST_DIR/$FIXTURE_VERSION/runner.tar.gz"
check "a: current points at the new version" [ "$(target_of_current)" = "$FIXTURE_VERSION" ]
run install_runner >/dev/null 2>&1
check "a: third run is a no-op for an installed version" [ "$(count fetches)" -eq 2 ]

# (b) Readiness loop.
new_case b-ready-after-polls
READY_AFTER=3
out=$(run wait_ready 2>&1)
rc=$?
check "b: ready after 3 polls succeeds" [ "$rc" -eq 0 ]
check "b: polled exactly 3 times" [ "$(count polls)" -eq 3 ]
check "b: prints ghr status" contains "$out" "ghr status: ready"

new_case b-service-failed
READY_AFTER=1000
FAKE_STATE=activating
out=$(run wait_ready 2>&1)
rc=$?
check "b: crashed service fails readiness" [ "$rc" -ne 0 ]
check "b: reports service failed" contains "$out" "ghr service failed (systemd state: activating)"
check "b: points at journalctl" contains "$out" "journalctl -u ghr"
check "b: does not report still starting" lacks "$out" "still starting"
check "b: fails on the first poll, without waiting" [ "$(count polls)" -eq 1 ]

new_case b-still-starting
READY_AFTER=1000
READY_TIMEOUT=5
# Unset, SECONDS stops tracking real time, so only the stubbed sleep advances it.
out=$(unset SECONDS; SECONDS=0; run wait_ready 2>&1)
rc=$?
check "b: never-ready service fails readiness" [ "$rc" -ne 0 ]
check "b: reports still starting after the timeout" contains "$out" "ghr service still starting after 5s"
check "b: does not report service failed" lacks "$out" "service failed"
check "b: polling is bounded by the timeout" [ "$(count polls)" -eq 6 ]

# (d) install_config never asks for the token; a first install marks setup pending.
new_case d-no-token-prompt
ETC_DIR="$T/d-no-token-prompt/etc"
mkdir -p "$ETC_DIR"
out=$(GHR_TOKEN='' run install_config </dev/null 2>&1)
rc=$?
check "d: install_config succeeds without a token" [ "$rc" -eq 0 ]
check "d: no token prompt" lacks "$out" "PAT"
check "d: no token file written" absent "$ETC_DIR/token"
check "d: a first install marks setup pending" [ -f "$STATE_DIR/setup-pending" ]
rm -f "$STATE_DIR/setup-pending"
run install_config >/dev/null 2>&1
check "d: an upgrade does not mark setup pending" absent "$STATE_DIR/setup-pending"

# (f) The apt step installs the libraries the python-versions builds link.
new_case f-apt
out=$( (
  apt-get() { echo "apt-get $*" >> "$MARK_DIR/apt"; }
  curl() { :; }
  install() { :; }
  chmod() { :; }
  write_docker_source() { :; }
  systemctl() { :; }
  docker() { :; }
  run install_docker
) 2>&1)
rc=$?
check "f: install_docker succeeds with stubs" [ "$rc" -eq 0 ]
apt=$(cat "$MARK_DIR/apt" 2>/dev/null)
for lib in libssl3t64 libffi8 libsqlite3-0 liblzma5 libbz2-1.0 libgdbm6t64 libncursesw6 libreadline8t64 libtcl8.6 libtk8.6; do
  check "f: apt installs $lib" contains "$apt" " $lib"
done

# (g) A first install queues the popular toolchains; an upgrade does not.
new_case g-first-install
ETC_DIR="$T/g-first-install/etc"
mkdir -p "$ETC_DIR"
(run install_config) >/dev/null 2>&1
check "g: a missing config.yaml marks the preset pending" [ -f "$STATE_DIR/toolchains-pending" ]
rm -f "$STATE_DIR/toolchains-pending"
(run install_config) >/dev/null 2>&1
check "g: an existing config.yaml is an upgrade" absent "$STATE_DIR/toolchains-pending"

new_case g-preinstall
pending="$STATE_DIR/toolchains-pending"
: > "$MARK_DIR/ghr-calls"
touch "$pending"
out=$( (GHR_TOOLCHAINS=; run preinstall_toolchains) 2>&1)
check "g: a first install queues the popular preset" [ "$(cat "$MARK_DIR/ghr-calls")" = "toolchain install --preset popular" ]
check "g: says once how to follow the installs" [ "$(grep -c 'follow with: ghr storage' <<<"$out")" -eq 1 ]
check "g: a queued preset is no longer pending" absent "$pending"
: > "$MARK_DIR/ghr-calls"
(GHR_TOOLCHAINS=; run preinstall_toolchains) >/dev/null 2>&1
check "g: an upgrade queues nothing" [ ! -s "$MARK_DIR/ghr-calls" ]
: > "$MARK_DIR/ghr-calls"
(GHR_TOOLCHAINS=popular; run preinstall_toolchains) >/dev/null 2>&1
check "g: GHR_TOOLCHAINS=popular queues on an upgrade" [ -s "$MARK_DIR/ghr-calls" ]
: > "$MARK_DIR/ghr-calls"
touch "$pending"
(GHR_TOOLCHAINS=none; run preinstall_toolchains) >/dev/null 2>&1
check "g: GHR_TOOLCHAINS=none skips a first install" [ ! -s "$MARK_DIR/ghr-calls" ]
check "g: GHR_TOOLCHAINS=none drops the pending preset" absent "$pending"
touch "$pending" "$MARK_DIR/fail-queue"
out=$( (GHR_TOOLCHAINS=; run preinstall_toolchains) 2>&1)
rc=$?
check "g: a failed queue does not fail setup" [ "$rc" -eq 0 ]
check "g: a failed queue warns" contains "$out" "warning:"
check "g: a failed queue keeps the preset pending" [ -f "$pending" ]
rm -f "$MARK_DIR/fail-queue"
: > "$MARK_DIR/ghr-calls"
(GHR_TOOLCHAINS=; run preinstall_toolchains) >/dev/null 2>&1
check "g: the next run queues a preset still pending" [ -s "$MARK_DIR/ghr-calls" ]
out=$( (GHR_TOOLCHAINS=all; run check_toolchains) 2>&1)
rc=$?
check "g: an unknown GHR_TOOLCHAINS is rejected" [ "$rc" -ne 0 ]
check "g: names the allowed values" contains "$out" "popular or none"
out=$( (GHR_TOOLCHAINS=none; run check_toolchains) 2>&1)
rc=$?
check "g: GHR_TOOLCHAINS=none is accepted" [ "$rc" -eq 0 ]

# (h) A first install sets the web password; an upgrade leaves it alone.
new_case h-web-password
ETC_DIR="$T/h-web-password/etc"
mkdir -p "$ETC_DIR"
first=$( (set -e; install_config >/dev/null 2>&1; echo "$FIRST_INSTALL") )
check "h: writing config.yaml marks a first install" [ "$first" = 1 ]
again=$( (set -e; install_config >/dev/null 2>&1; echo "$FIRST_INSTALL") )
check "h: an existing config.yaml is not a first install" [ "$again" = 0 ]

web_ghr() {
  ghr() {
    echo "$*" >> "$MARK_DIR/ghr-calls"
    cat > "$MARK_DIR/ghr-stdin"
    [ ! -f "$MARK_DIR/fail-web" ] || { echo "ghr: password must be 12 to 1024 bytes" >&2; return 1; }
    echo "web password set; every browser was logged out"
  }
}
out=$( (web_ghr; FIRST_INSTALL=0; GHR_WEB_PASSWORD='a long secret'; run set_web_password) </dev/null 2>&1)
check "h: an upgrade does not touch the web password" absent "$MARK_DIR/ghr-calls"
check "h: an upgrade prints nothing" [ -z "$out" ]

out=$( (web_ghr; FIRST_INSTALL=1; GHR_WEB_PASSWORD='a long secret'; run set_web_password) </dev/null 2>&1)
check "h: GHR_WEB_PASSWORD goes to ghr web set-password" [ "$(cat "$MARK_DIR/ghr-calls")" = "web set-password" ]
check "h: the password is piped without a newline" [ "$(wc -c < "$MARK_DIR/ghr-stdin")" -eq 13 ]
check "h: success is reported" contains "$out" "web password set"
check "h: no reminder after success" lacks "$out" "has no password"

rm -f "$MARK_DIR/ghr-calls"
touch "$MARK_DIR/fail-web"
out=$( (web_ghr; FIRST_INSTALL=1; GHR_WEB_PASSWORD='short'; run set_web_password) </dev/null 2>&1)
rc=$?
check "h: a refused password does not fail the install" [ "$rc" -eq 0 ]
check "h: the refusal is shown" contains "$out" "password must be 12 to 1024 bytes"
check "h: the reminder follows a refusal" contains "$out" "set it now with: ghr web set-password"

rm -f "$MARK_DIR/ghr-calls" "$MARK_DIR/fail-web"
out=$( (web_ghr; FIRST_INSTALL=1; GHR_WEB_PASSWORD=; run set_web_password) </dev/null 2>&1)
check "h: with no terminal and no GHR_WEB_PASSWORD nothing is sent" absent "$MARK_DIR/ghr-calls"
check "h: and the reminder is printed" contains "$out" "web UI on :8080 has no password; set it now with: ghr web set-password"

# setup_ghr answers `ghr setup` from the mark files configured, wizard, owner
# and web, and records `ghr setup github` and `ghr toolchain` calls.
setup_ghr() {
  ghr() {
    case "$1 ${2:-}" in
      "setup ")
        printf 'configured: %s\nwizard: %s\nowner: %s\nweb: %s\n' "$(cat "$MARK_DIR/configured")" \
          "$(cat "$MARK_DIR/wizard")" "$(cat "$MARK_DIR/owner")" "$(cat "$MARK_DIR/web")"
        ;;
      "setup github")
        echo "$*" >> "$MARK_DIR/ghr-calls"
        cat > "$MARK_DIR/ghr-stdin"
        [ ! -f "$MARK_DIR/fail-github" ] || { echo "ghr: token rejected by GitHub: github: 401 Bad credentials" >&2; return 1; }
        echo "GitHub owner and token set; ghr is running"
        ;;
      "toolchain install")
        echo "$*" >> "$MARK_DIR/ghr-calls"
        echo "queued — follow with: ghr storage"
        ;;
      *) return 1 ;;
    esac
  }
}
setup_marks() {
  echo "$1" > "$MARK_DIR/configured"
  echo "$2" > "$MARK_DIR/wizard"
  echo "$3" > "$MARK_DIR/owner"
  echo "$4" > "$MARK_DIR/web"
  rm -f "$MARK_DIR/ghr-calls" "$MARK_DIR/ghr-stdin"
}

# (i) configure_github asks for owner and token while ghr is not configured.
new_case i-configure
setup_marks no pending - 0.0.0.0:8080
out=$( (setup_ghr; GHR_OWNER=DarkRaise; GHR_TOKEN=tok; run configure_github) </dev/null 2>&1)
check "i: GHR_OWNER and GHR_TOKEN configure ghr" [ "$(cat "$MARK_DIR/ghr-calls")" = "setup github --owner DarkRaise" ]
check "i: the token is piped" [ "$(cat "$MARK_DIR/ghr-stdin")" = tok ]
check "i: without a newline" [ "$(wc -c < "$MARK_DIR/ghr-stdin")" -eq 3 ]
check "i: success is reported" contains "$out" "ghr is running"

setup_marks yes "done" DarkRaise 0.0.0.0:8080
out=$( (setup_ghr; GHR_OWNER=DarkRaise; GHR_TOKEN=tok; run configure_github) </dev/null 2>&1)
check "i: a configured ghr is left alone" absent "$MARK_DIR/ghr-calls"
check "i: and nothing is printed" [ -z "$out" ]

setup_marks no pending DarkRaise 0.0.0.0:8080
(setup_ghr; GHR_OWNER=; GHR_TOKEN=tok; run configure_github) </dev/null >/dev/null 2>&1
check "i: config.yaml's owner is used" [ "$(cat "$MARK_DIR/ghr-calls")" = "setup github --owner DarkRaise" ]

setup_marks no pending - 0.0.0.0:8080
(setup_ghr; GHR_OWNER=; GHR_TOKEN=; run configure_github) </dev/null >/dev/null 2>&1
check "i: without a terminal or env vars nothing is sent" absent "$MARK_DIR/ghr-calls"
out=$( (setup_ghr; GHR_OWNER=; GHR_TOKEN=tok; run configure_github) </dev/null 2>&1)
check "i: GHR_TOKEN without an owner warns" contains "$out" "GHR_TOKEN is ignored without an owner; set GHR_OWNER"
check "i: and sends nothing" absent "$MARK_DIR/ghr-calls"

touch "$MARK_DIR/fail-github"
out=$( (setup_ghr; GHR_OWNER=DarkRaise; GHR_TOKEN=bad; run configure_github) </dev/null 2>&1)
rc=$?
check "i: a refused token does not fail the install" [ "$rc" -eq 0 ]
check "i: the refusal is shown" contains "$out" "token rejected by GitHub"
rm -f "$MARK_DIR/fail-github"

setup_marks no pending - 0.0.0.0:8080
(setup_ghr; stdin_is_tty() { return 0; }; GHR_OWNER=; GHR_TOKEN=; run configure_github) <<<$'DarkRaise\ntok' >/dev/null 2>&1
check "i: the prompts configure ghr" [ "$(cat "$MARK_DIR/ghr-calls")" = "setup github --owner DarkRaise" ]
check "i: with the typed token" [ "$(cat "$MARK_DIR/ghr-stdin")" = tok ]
setup_marks no pending - 0.0.0.0:8080
(setup_ghr; stdin_is_tty() { return 0; }; GHR_OWNER=; GHR_TOKEN=; run configure_github) <<<'' >/dev/null 2>&1
check "i: Enter at the owner prompt skips" absent "$MARK_DIR/ghr-calls"
(setup_ghr; stdin_is_tty() { return 0; }; GHR_OWNER=; GHR_TOKEN=; run configure_github) <<<$'DarkRaise\n' >/dev/null 2>&1
check "i: Enter at the token prompt skips" absent "$MARK_DIR/ghr-calls"

# (j) Toolchains wait for GitHub to be configured.
new_case j-toolchains-unconfigured
setup_marks no pending - 0.0.0.0:8080
touch "$STATE_DIR/toolchains-pending"
out=$( (setup_ghr; GHR_TOOLCHAINS=; run preinstall_toolchains) 2>&1)
check "j: nothing is queued before GitHub is configured" absent "$MARK_DIR/ghr-calls"
check "j: the preset stays pending" [ -f "$STATE_DIR/toolchains-pending" ]
check "j: says where to choose toolchains" contains "$out" "toolchains can be chosen in the setup wizard, or later with: ghr toolchain install --preset popular"
setup_marks yes pending DarkRaise 0.0.0.0:8080
(setup_ghr; GHR_TOOLCHAINS=; run preinstall_toolchains) >/dev/null 2>&1
check "j: once configured the pending preset is queued" [ "$(cat "$MARK_DIR/ghr-calls")" = "toolchain install --preset popular" ]

# (k) print_next says what is left of first-run setup.
new_case k-print-next
hostname() { echo "192.168.0.99 fd00::99"; }
setup_marks no pending - 0.0.0.0:8080
out=$( (setup_ghr; run print_next) 2>&1)
check "k: unconfigured with the web UI points at the browser" contains "$out" "finish setup in the browser: http://192.168.0.99:8080/setup"
setup_marks yes pending DarkRaise 0.0.0.0:8080
out=$( (setup_ghr; run print_next) 2>&1)
check "k: a pending wizard points at the browser" contains "$out" "finish setup in the browser: http://192.168.0.99:8080/setup"
setup_marks no pending - -
out=$( (setup_ghr; run print_next) 2>&1)
check "k: without the web UI it names ghr setup github" contains "$out" "finish setup with: ghr setup github --owner <owner>"
setup_marks yes pending DarkRaise -
out=$( (setup_ghr; run print_next) 2>&1)
check "k: a pending wizard without the web UI names ghr setup finish" contains "$out" "end first-run setup with: ghr setup finish"
setup_marks yes "done" DarkRaise 0.0.0.0:8080
out=$( (setup_ghr; run print_next) 2>&1)
check "k: a finished setup prints nothing" [ -z "$out" ]
unset -f hostname

# (c) Dist GC.
check_host() { :; }
install_docker() { :; }
create_user_and_dirs() { :; }
install_ghr() { :; }
install_config() { :; }
install_service() {
  bump restarts
  [ -d "$DIST_DIR/2.300.0" ] && [ -d "$DIST_DIR/2.336.0" ] && touch "$MARK_DIR/old-present-at-restart"
  [ "$(target_of_current)" = "$FIXTURE_VERSION" ] && touch "$MARK_DIR/current-new-at-restart"
}

new_case c-main-upgrade
echo 0 > "$MARK_DIR/restarts"
old_version 2.300.0
old_version 2.336.0
ln -sfn "$DIST_DIR/2.336.0" "$DIST_DIR/current"
out=$(run main 2>&1)
rc=$?
check "c: upgrade via main succeeds" [ "$rc" -eq 0 ]
check "c: service restarted once" [ "$(count restarts)" -eq 1 ]
check "c: old versions still present at restart" [ -f "$MARK_DIR/old-present-at-restart" ]
check "c: current already switched at restart" [ -f "$MARK_DIR/current-new-at-restart" ]
check "c: old version 2.300.0 removed after ready" absent "$DIST_DIR/2.300.0"
check "c: old version 2.336.0 removed after ready" absent "$DIST_DIR/2.336.0"
check "c: new version kept" [ -x "$DIST_DIR/$FIXTURE_VERSION/run.sh" ]
check "c: current kept and points at the new version" [ "$(target_of_current)" = "$FIXTURE_VERSION" ]
check "c: ghr stopped before the runner download" [ -f "$MARK_DIR/fetched-after-stop" ]
check "c: the stopped unit is ghr.service" [ "$(cat "$MARK_DIR/stopped")" = ghr.service ]

new_case c-first-install
out=$( (systemctl() { return 5; }; run stop_ghr) 2>&1)
rc=$?
check "c: stop_ghr succeeds when ghr is not installed yet" [ "$rc" -eq 0 ]

new_case c-main-fetch-fails
old_version 2.336.0
ln -sfn "$DIST_DIR/2.336.0" "$DIST_DIR/current"
out=$( (fetch_runner() { return 1; }; run main) 2>&1)
rc=$?
check "c: main fails when the runner download fails" [ "$rc" -ne 0 ]
check "c: ghr started again after the failure" [ "$(cat "$MARK_DIR/started" 2>/dev/null)" = ghr.service ]

new_case c-queued-update-dropped
mkdir -p "$STATE_DIR"
echo '{"queued_at":"2026-10-05T12:00:00Z","warned_version":"2.338.0"}' > "$STATE_DIR/runner-update.json"
out=$( (run stop_ghr) 2>&1)
rc=$?
check "c: stop_ghr succeeds with a queued runner update" [ "$rc" -eq 0 ]
check "c: stop_ghr drops the queued runner update" [ ! -e "$STATE_DIR/runner-update.json" ]

new_case c-main-not-ready
echo 0 > "$MARK_DIR/restarts"
old_version 2.336.0
ln -sfn "$DIST_DIR/2.336.0" "$DIST_DIR/current"
READY_AFTER=1000
FAKE_STATE=activating
out=$(run main 2>&1)
rc=$?
check "c: main fails when the service is not ready" [ "$rc" -ne 0 ]
check "c: no ghr start after the service restarted" [ ! -f "$MARK_DIR/started" ]
check "c: no GC when the service is not ready" [ -d "$DIST_DIR/2.336.0" ]

new_case c-gc-direct
old_version 2.300.0
old_version 2.336.0
old_version 2.337.0
mkdir -p "$DIST_DIR/2.338.0.tmp"
ln -sfn "$DIST_DIR/2.336.0" "$DIST_DIR/current"
out=$(run gc_dist 2>&1)
rc=$?
check "c: gc_dist succeeds" [ "$rc" -eq 0 ]
check "c: gc keeps current's target" [ -d "$DIST_DIR/2.336.0" ]
check "c: gc keeps current" [ "$(target_of_current)" = 2.336.0 ]
check "c: gc removes an older version" absent "$DIST_DIR/2.300.0"
check "c: gc removes a newer non-current version" absent "$DIST_DIR/2.337.0"
check "c: gc removes a leftover temp dir" absent "$DIST_DIR/2.338.0.tmp"

# (e) Docker apt source: one deb822 docker.sources; a one-line docker.list
# from an earlier run is removed, so apt never sees the repository twice.
dpkg() { [ "$1" = --print-architecture ] && echo amd64; }
VERSION_CODENAME=trixie
APT_SOURCES_DIR="$T/apt-sources"
mkdir -p "$APT_SOURCES_DIR"
echo "deb [arch=amd64] https://download.docker.com/linux/debian trixie stable" > "$APT_SOURCES_DIR/docker.list"
for pass in first second; do
  out=$(run write_docker_source 2>&1)
  rc=$?
  check "e: $pass write succeeds" [ "$rc" -eq 0 ]
  check "e: $pass leaves only docker.sources" [ "$(ls "$APT_SOURCES_DIR")" = docker.sources ]
done
src=$(cat "$APT_SOURCES_DIR/docker.sources" 2>/dev/null)
for line in "Types: deb" "URIs: https://download.docker.com/linux/debian" "Suites: trixie" "Components: stable" \
  "Architectures: amd64" "Signed-By: /etc/apt/keyrings/docker.asc"; do
  check "e: docker.sources has $line" contains "$src" "$line"
done

echo
if [ "$FAILS" -gt 0 ]; then
  echo "$FAILS assertion(s) failed"
  exit 1
fi
echo "all assertions passed"
