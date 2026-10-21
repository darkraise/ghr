#!/usr/bin/env bash
# Tests for detect.sh. Run them as an unprivileged user: root reads every file,
# so the unreadable cases cannot be tested as root.
#   bash actions/lib/detect_test.sh
set -uo pipefail

if [ "$(id -u)" -eq 0 ]; then
  echo "detect_test.sh refuses to run as root: root can read every file, so the unreadable cases would pass for the wrong reason; run it as an unprivileged user" >&2
  exit 1
fi

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
T=$(mktemp -d)
trap 'chmod -R u+rwX "$T"; rm -rf "$T"' EXIT

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

WARNING="::warning::ghr's artifact store settings may exist but this job's user cannot read them, so this job uses GitHub's artifact storage; run the job as root in its container or on the host"
WRITE_WARNING="::warning::this job's user cannot write the step's outputs to GITHUB_OUTPUT, so this job uses GitHub's artifact storage; run the job as root in its container or on the host"

# instance <name> makes a runner instance whose job workspace is
# <name>/_work/repo, and prints that workspace.
instance() {
  mkdir -p "$T/$1/_work/repo"
  printf '%s\n' "$T/$1/_work/repo"
}

# settings <name> writes the settings file of instance <name> with ghr's modes.
settings() {
  mkdir -p -m 0700 "$T/$1/_work/_ghr"
  printf '{"v":1}\n' > "$T/$1/_work/_ghr/artifact-store.json"
  chmod 0600 "$T/$1/_work/_ghr/artifact-store.json"
}

# detect [VAR=value...] runs detect.sh without RUNNER_WORKSPACE unless given,
# and sets out, err, rc and outputs (what it appended to GITHUB_OUTPUT).
detect() {
  printf 'earlier=kept\n' > "$T/output"
  out=$(env -u RUNNER_WORKSPACE GITHUB_OUTPUT="$T/output" "$@" sh "$HERE/detect.sh" 2> "$T/stderr")
  rc=$?
  err=$(cat "$T/stderr")
  outputs=$(cat "$T/output")
}

ws=$(instance a)
settings a
detect RUNNER_WORKSPACE="$ws"
check "a: a readable settings file succeeds" [ "$rc" -eq 0 ]
check "a: GITHUB_OUTPUT gets on=true and the file's path" [ "$outputs" = "$(printf 'earlier=kept\non=true\nsettings=%s' "$T/a/_work/_ghr/artifact-store.json")" ]
check "a: nothing is printed" [ -z "$out$err" ]

ws=$(instance b)
detect RUNNER_WORKSPACE="$ws"
check "b: no settings file succeeds" [ "$rc" -eq 0 ]
check "b: GITHUB_OUTPUT gets on=false and an empty path" [ "$outputs" = "$(printf 'earlier=kept\non=false\nsettings=')" ]
check "b: no warning without the file" [ -z "$out$err" ]

ws=$(instance c)
mkdir -m 0700 "$T/c/_work/_ghr"
detect RUNNER_WORKSPACE="$ws"
check "c: an empty _ghr is off" [ "$outputs" = "$(printf 'earlier=kept\non=false\nsettings=')" ]
check "c: an empty _ghr gives no warning" [ -z "$out$err" ]

detect
check "d: no RUNNER_WORKSPACE succeeds" [ "$rc" -eq 0 ]
check "d: no RUNNER_WORKSPACE is off" [ "$outputs" = "$(printf 'earlier=kept\non=false\nsettings=')" ]
check "d: no RUNNER_WORKSPACE gives no warning" [ -z "$out$err" ]

ws=$(instance e)
settings e
chmod 000 "$T/e/_work/_ghr/artifact-store.json"
detect RUNNER_WORKSPACE="$ws"
check "e: an unreadable settings file succeeds" [ "$rc" -eq 0 ]
check "e: an unreadable settings file is off" [ "$outputs" = "$(printf 'earlier=kept\non=false\nsettings=')" ]
check "e: an unreadable settings file warns" [ "$out" = "$WARNING" ]

ws=$(instance f)
settings f
chmod 000 "$T/f/_work/_ghr"
detect RUNNER_WORKSPACE="$ws"
check "f: an unreachable _ghr succeeds" [ "$rc" -eq 0 ]
check "f: an unreachable _ghr is off" [ "$outputs" = "$(printf 'earlier=kept\non=false\nsettings=')" ]
check "f: an unreachable _ghr warns" [ "$out" = "$WARNING" ]

ws=$(instance g)
settings g
printf 'earlier=kept\n' > "$T/readonly-output"
chmod 0444 "$T/readonly-output"
detect RUNNER_WORKSPACE="$ws" GITHUB_OUTPUT="$T/readonly-output"
check "g: an unwritable GITHUB_OUTPUT succeeds" [ "$rc" -eq 0 ]
check "g: an unwritable GITHUB_OUTPUT warns" [ "$out" = "$WRITE_WARNING" ]
check "g: an unwritable GITHUB_OUTPUT prints no shell error" [ -z "$err" ]
check "g: an unwritable GITHUB_OUTPUT is left unchanged" [ "$(cat "$T/readonly-output")" = "earlier=kept" ]

ws=$(instance h)
settings h
detect RUNNER_WORKSPACE="$ws" GITHUB_OUTPUT=
check "h: no GITHUB_OUTPUT fails" [ "$rc" -ne 0 ]
check "h: the failure says why" contains "$err" "GITHUB_OUTPUT is not set"

echo
if [ "$FAILS" -gt 0 ]; then
  echo "$FAILS assertion(s) failed"
  exit 1
fi
echo "all assertions passed"
