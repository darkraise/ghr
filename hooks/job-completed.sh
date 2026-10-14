#!/usr/bin/env bash
# Runner post-job hook. Must always exit 0. Adds finished_at to job.json; when
# job.json is missing or unreadable it records finished_at alone.
# The runner runs it as `bash -e`, so the rename chain ends in `|| true`.
dir="${GHR_INSTANCE_DIR:-}"
[ -n "$dir" ] && [ -d "$dir" ] || exit 0
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || exit 0
# jq -e exits 4 when it produces no output (an empty job.json), falling through.
# The job owns the instance dir: job.json may be a FIFO or a link, so only a
# regular file is read, and the timeout covers a swap after the test.
[ -f "$dir/job.json" ] && [ ! -L "$dir/job.json" ] \
  && out="$(timeout 10 jq -ce --arg f "$now" '. + {finished_at: $f}' "$dir/job.json" 2>/dev/null)" \
  || out="$(jq -nc --arg f "$now" '{finished_at: $f}' 2>/dev/null)" \
  || exit 0
printf '%s\n' "$out" > "$dir/job.json.tmp" 2>/dev/null \
  && mv -f "$dir/job.json.tmp" "$dir/job.json" 2>/dev/null || true
exit 0
