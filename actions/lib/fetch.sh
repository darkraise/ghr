#!/usr/bin/env bash
# Prints the path of a ghr-artifact binary for this runner, checked against its
# release's ghr-artifact-checksums.txt. The binary is cached under
# RUNNER_TOOL_CACHE; the checksums are fetched fresh and the cached binary is
# checked on every run, because ghr's runners share one tool cache.
#
# Inputs: GHR_VERSION (the action's version input), GHR_ACTION_REF (github.action_ref),
# RUNNER_OS, RUNNER_ARCH, RUNNER_TOOL_CACHE; GHR_RELEASE_BASE_URL replaces the
# GitHub release URL in tests.
set -euo pipefail

die() {
  echo "::error::$*" >&2
  exit 1
}

version=${GHR_VERSION:-${GHR_ACTION_REF:-}}
if ! [[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  die 'reference a ghr release tag such as @vX.Y.Z, or set `version`'
fi

case "${RUNNER_OS:-}/${RUNNER_ARCH:-}" in
  Linux/X64) asset=ghr-artifact_linux_amd64 ;;
  Linux/ARM64) asset=ghr-artifact_linux_arm64 ;;
  Windows/X64) asset=ghr-artifact_windows_amd64.exe ;;
  *) die "ghr-artifact has no build for ${RUNNER_OS:-?} ${RUNNER_ARCH:-?}; it runs on Linux X64 or ARM64 and Windows X64" ;;
esac

base="${GHR_RELEASE_BASE_URL:-https://github.com/darkraise/ghr/releases/download}/$version"
[ -n "${RUNNER_TOOL_CACHE:-}" ] || die "RUNNER_TOOL_CACHE is not set; run this inside a GitHub Actions job"
# Forward slashes keep Git Bash on Windows treating the path as a path.
dir="${RUNNER_TOOL_CACHE//\\//}/ghr-artifact/$version"
bin="$dir/$asset"

fetch() {
  curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 20 --max-time 600 -o "$2" "$1"
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fetch "$base/ghr-artifact-checksums.txt" "$work/sums" || die "cannot download ghr-artifact-checksums.txt from $base"
want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1; exit }' "$work/sums")
[[ $want =~ ^[0-9a-f]{64}$ ]] || die "ghr-artifact-checksums.txt of $version does not list $asset"

verified() {
  [ -f "$1" ] && [ "$(sha256sum < "$1" | awk '{ print $1 }')" = "$want" ]
}

if [ -e "$bin" ] && ! verified "$bin"; then
  echo "the cached $asset does not match $version's checksum; downloading it again" >&2
fi
if ! verified "$bin"; then
  mkdir -p "$dir"
  tmp=$(mktemp "$dir/.$asset.XXXXXX")
  if ! fetch "$base/$asset" "$tmp"; then
    rm -f "$tmp"
    die "cannot download $asset from $base"
  fi
  if ! verified "$tmp"; then
    rm -f "$tmp"
    die "$asset from $base does not match ghr-artifact-checksums.txt; the download is damaged or was changed"
  fi
  chmod 0755 "$tmp"
  mv -f "$tmp" "$bin"
fi
printf '%s\n' "$bin"
