#!/usr/bin/env bash
# Runner pre-job hook. GitHub fails the job if this exits non-zero, so it must
# never talk to the daemon and must always exit 0. It only records the job.
# jq does the JSON encoding: workflow names may hold quotes or control characters.
dir="${GHR_INSTANCE_DIR:-}"
[ -n "$dir" ] && [ -d "$dir" ] || exit 0
# The runner runs .sh hooks as `bash -e`: a failing last command in an && list
# exits the shell, so the rename chain ends in `|| true`.
jq -nc \
  --arg run_id "${GITHUB_RUN_ID:-}" --arg run_attempt "${GITHUB_RUN_ATTEMPT:-}" \
  --arg run_number "${GITHUB_RUN_NUMBER:-}" --arg workflow "${GITHUB_WORKFLOW:-}" \
  --arg job "${GITHUB_JOB:-}" --arg runner_name "${RUNNER_NAME:-}" \
  --arg started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '$ARGS.named' > "$dir/job.json.tmp" 2>/dev/null \
  && mv -f "$dir/job.json.tmp" "$dir/job.json" 2>/dev/null || true
exit 0
