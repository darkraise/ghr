#!/usr/bin/env bash
# Offline tests for fetch.sh: the release is a local directory served through
# file:// URLs and the tool cache is a temporary directory.
#   bash actions/lib/fetch_test.sh
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
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

# release <version> writes the three assets and their checksums file.
release() {
  local d="$T/release/$1"
  mkdir -p "$d"
  for a in ghr-artifact_linux_amd64 ghr-artifact_linux_arm64 ghr-artifact_windows_amd64.exe; do
    printf 'binary %s %s\n' "$a" "$1" > "$d/$a"
  done
  (cd "$d" && sha256sum ghr-artifact_linux_amd64 ghr-artifact_linux_arm64 ghr-artifact_windows_amd64.exe > ghr-artifact-checksums.txt)
}

# fetch [VAR=value...] runs fetch.sh with a clean set of inputs and sets out, err and rc.
fetch() {
  out=$(env -u GHR_VERSION -u GHR_ACTION_REF \
    GHR_RELEASE_BASE_URL="file://$T/release" RUNNER_TOOL_CACHE="$T/cache" RUNNER_OS=Linux RUNNER_ARCH=X64 \
    "$@" bash "$HERE/fetch.sh" 2> "$T/stderr")
  rc=$?
  err=$(cat "$T/stderr")
}

release v1.2.3
CACHED="$T/cache/ghr-artifact/v1.2.3/ghr-artifact_linux_amd64"

fetch GHR_ACTION_REF=v1.2.3
check "a: the action's tag is fetched" [ "$rc" -eq 0 ]
check "a: the cached path is printed" [ "$out" = "$CACHED" ]
check "a: the cached binary is the release's" cmp -s "$CACHED" "$T/release/v1.2.3/ghr-artifact_linux_amd64"
check "a: the cached binary is executable" [ -x "$CACHED" ]
check "a: nothing but the binary is left in the cache" [ "$(ls -A "$T/cache/ghr-artifact/v1.2.3")" = ghr-artifact_linux_amd64 ]

fetch GHR_VERSION=v1.2.3 GHR_ACTION_REF=main
check "b: the version input wins over a branch reference" [ "$rc" -eq 0 ]
check "b: the version input's binary is printed" [ "$out" = "$CACHED" ]

for ref in main 0123456789abcdef0123456789abcdef01234567 v1.2 1.2.3 v1.2.3-rc1 ""; do
  fetch GHR_ACTION_REF="$ref"
  check "c: reference '$ref' is refused" [ "$rc" -ne 0 ]
  check "c: reference '$ref' names the fix" contains "$err" 'reference a ghr release tag such as @vX.Y.Z, or set `version`'
  check "c: reference '$ref' prints no path" [ -z "$out" ]
done
fetch GHR_VERSION=1.2.3 GHR_ACTION_REF=v1.2.3
check "c: a version input without the v is refused" [ "$rc" -ne 0 ]

release v1.2.4
printf 'tampered\n' > "$T/release/v1.2.4/ghr-artifact_linux_amd64"
fetch GHR_ACTION_REF=v1.2.4
check "d: a download that does not match its checksum fails" [ "$rc" -ne 0 ]
check "d: the failure says so" contains "$err" "does not match ghr-artifact-checksums.txt"
check "d: nothing is cached" [ -z "$(ls -A "$T/cache/ghr-artifact/v1.2.4" 2> /dev/null)" ]

printf 'changed by a job\n' > "$CACHED"
fetch GHR_ACTION_REF=v1.2.3
check "e: a changed cached binary is fetched again" [ "$rc" -eq 0 ]
check "e: the cached binary is the release's again" cmp -s "$CACHED" "$T/release/v1.2.3/ghr-artifact_linux_amd64"
check "e: the replacement is reported" contains "$err" "does not match v1.2.3's checksum; downloading it again"

cp "$T/release/v1.2.3/ghr-artifact-checksums.txt" "$T/sums.good"
sed -i 's/^[0-9a-f]\{8\}/00000000/' "$T/release/v1.2.3/ghr-artifact-checksums.txt"
fetch GHR_ACTION_REF=v1.2.3
check "f: a cached binary is checked against the release on every run" [ "$rc" -ne 0 ]
check "f: no path is printed for a binary that fails the check" [ -z "$out" ]
cp "$T/sums.good" "$T/release/v1.2.3/ghr-artifact-checksums.txt"

for row in "Linux ARM64 ghr-artifact_linux_arm64" "Windows X64 ghr-artifact_windows_amd64.exe"; do
  read -r os arch asset <<< "$row"
  fetch GHR_ACTION_REF=v1.2.3 RUNNER_OS="$os" RUNNER_ARCH="$arch"
  check "g: $os $arch succeeds" [ "$rc" -eq 0 ]
  check "g: $os $arch fetches $asset" [ "$out" = "$T/cache/ghr-artifact/v1.2.3/$asset" ]
done
for row in "macOS ARM64" "Windows ARM64" "Linux X86"; do
  read -r os arch <<< "$row"
  fetch GHR_ACTION_REF=v1.2.3 RUNNER_OS="$os" RUNNER_ARCH="$arch"
  check "g: $os $arch is refused" [ "$rc" -ne 0 ]
  check "g: $os $arch names the supported runners" contains "$err" "ghr-artifact has no build for $os $arch"
done

fetch GHR_ACTION_REF=v9.9.9
check "h: a release that does not exist fails" [ "$rc" -ne 0 ]
check "h: the failure names the release URL" contains "$err" "cannot download ghr-artifact-checksums.txt from file://$T/release/v9.9.9"

fetch GHR_ACTION_REF=v1.2.3 RUNNER_TOOL_CACHE="$T\\wincache"
check "i: backslashes in the tool cache path become forward slashes" [ "$out" = "$T/wincache/ghr-artifact/v1.2.3/ghr-artifact_linux_amd64" ]

echo
if [ "$FAILS" -gt 0 ]; then
  echo "$FAILS assertion(s) failed"
  exit 1
fi
echo "all assertions passed"
