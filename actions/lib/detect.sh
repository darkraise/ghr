#!/bin/sh
# Tells the upload and download actions whether this job runs on a ghr runner
# that ghr gave its artifact store settings. ghr writes them to
# _ghr/artifact-store.json in the runner's work folder, the parent of
# RUNNER_WORKSPACE. Appends on=true|false and settings=<path or empty> to
# GITHUB_OUTPUT. POSIX sh, not bash: every Linux job that uses the actions
# runs it, container jobs whose image has no bash included.
set -eu

[ -n "${GITHUB_OUTPUT:-}" ] || {
  echo "::error::GITHUB_OUTPUT is not set; run this inside a GitHub Actions job" >&2
  exit 1
}

on=false
settings=
if [ -n "${RUNNER_WORKSPACE:-}" ]; then
  dir="$(dirname "$RUNNER_WORKSPACE")/_ghr"
  f="$dir/artifact-store.json"
  if [ -f "$f" ] && [ -r "$f" ]; then
    on=true
    settings=$f
  # Without search permission on _ghr the job cannot even see whether the file
  # exists; an empty _ghr is what ghr leaves after it removes the file.
  elif [ -e "$f" ] || { [ -d "$dir" ] && ! [ -x "$dir" ]; }; then
    echo "::warning::ghr's artifact store settings exist but this job's user cannot read them, so this job uses GitHub's artifact storage; run the job as root in its container or on the host"
  fi
fi
printf 'on=%s\nsettings=%s\n' "$on" "$settings" >> "$GITHUB_OUTPUT"
