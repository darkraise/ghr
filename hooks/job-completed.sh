#!/usr/bin/env bash
# Runner post-job hook. Must always exit 0. Adds finished_at to job.json; when
# job.json is missing or unreadable it records finished_at alone.
dir="${GHR_INSTANCE_DIR:-}"
[ -n "$dir" ] && [ -d "$dir" ] || exit 0
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
out="$(jq -c --arg f "$now" '. + {finished_at: $f}' "$dir/job.json" 2>/dev/null)" \
  || out="$(jq -nc --arg f "$now" '{finished_at: $f}' 2>/dev/null)" \
  || exit 0
printf '%s\n' "$out" > "$dir/job.json.tmp" 2>/dev/null \
  && mv -f "$dir/job.json.tmp" "$dir/job.json" 2>/dev/null
exit 0
