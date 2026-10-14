#!/usr/bin/env bash
# Runner post-job hook. Must always exit 0. Adds finished_at to job.json; when
# job.json is missing or unreadable it records finished_at alone.
# The runner runs it as `bash -e`, so the rename chain ends in `|| true`.
dir="${GHR_INSTANCE_DIR:-}"
[ -n "$dir" ] && [ -d "$dir" ] || exit 0
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || exit 0
# jq -e exits 4 when it produces no output (an empty job.json), falling through.
# The job owns the instance dir: job.json may be a FIFO, a link or huge, so only
# a regular file of at most 64 KiB is read, and the timeout covers a swap after
# the tests. stat, unlike wc, never opens the file, so a FIFO cannot block it.
[ -f "$dir/job.json" ] && [ ! -L "$dir/job.json" ] \
  && [ "$(stat -c %s -- "$dir/job.json" 2>/dev/null)" -le 65536 ] 2>/dev/null \
  && out="$(timeout 10 jq -ce --arg f "$now" '. + {finished_at: $f}' "$dir/job.json" 2>/dev/null)" \
  || out="$(jq -nc --arg f "$now" '{finished_at: $f}' 2>/dev/null)" \
  || exit 0
# job.json.tmp may be a FIFO nobody reads, which blocks the open of the redirect.
timeout 5 bash -c 'printf "%s\n" "$1" > "$2" && mv -f "$2" "$3"' _ "$out" "$dir/job.json.tmp" "$dir/job.json" 2>/dev/null || true
exit 0
