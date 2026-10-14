#!/usr/bin/env bash
# Runner pre-job hook. GitHub fails the job if this exits non-zero, so it never
# talks to the daemon. It exits 1 only to refuse a job whose event comes from
# another repository, such as a fork's pull request; otherwise it records the
# job and exits 0.
# A pull-request-type event whose payload cannot be read is refused too.
case "${GITHUB_EVENT_NAME:-}" in
  pull_request|pull_request_target|pull_request_review|pull_request_review_comment|workflow_run)
    head=$(jq -r '.pull_request.head.repo.full_name // .workflow_run.head_repository.full_name // empty' \
      "${GITHUB_EVENT_PATH:-/dev/null}" 2>/dev/null) || head=
    if [ -z "$head" ] || [ "${head,,}" != "${GITHUB_REPOSITORY,,}" ]; then
      echo "ghr: refusing a job started from another repository (${head:-unknown}); see SECURITY.md" >&2
      exit 1
    fi ;;
esac
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
