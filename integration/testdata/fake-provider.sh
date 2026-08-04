#!/bin/sh
set -eu

: "${FAKE_PROVIDER_ARGV_LOG:?}"
: "${FAKE_PROVIDER_STDIN_LOG:?}"
: "${FAKE_PROVIDER_OUTPUT_LOG:?}"
: "${FAKE_PROVIDER_ENV_LOG:?}"
: "${FAKE_PROVIDER_STDERR_LOG:?}"
: "${FAKE_PROVIDER_STDERR:?}"

{
  printf 'BEGIN\n'
  printf '%s\n' "$@"
  printf 'END\n'
} >> "$FAKE_PROVIDER_ARGV_LOG"
{
  printf 'GH_TOKEN=%s\n' "${GH_TOKEN-}"
  printf 'TASK13_ENV_MARKER=%s\n' "${TASK13_ENV_MARKER-}"
  printf 'REPOWOLF_TOKEN_AGENT=%s\n' "${REPOWOLF_TOKEN_AGENT-unset}"
  printf 'REPOWOLF_ENDPOINT=%s\n' "${REPOWOLF_ENDPOINT-unset}"
} >> "$FAKE_PROVIDER_ENV_LOG"
printf '%s\n' "$FAKE_PROVIDER_STDERR" >> "$FAKE_PROVIDER_STDERR_LOG"
printf '%s\n' "$FAKE_PROVIDER_STDERR" >&2

emit() {
  printf '%s\n' "$1" >> "$FAKE_PROVIDER_OUTPUT_LOG"
  printf '%s\n' "$1"
}

method=''
endpoint=''
needs_input=false
previous=''
for argument in "$@"; do
  if [ "$previous" = '--method' ]; then method=$argument; fi
  if [ "$previous" = '--input' ] && [ "$argument" = '-' ]; then needs_input=true; fi
  endpoint=$argument
  previous=$argument
done
if $needs_input; then
  input=$(cat)
  printf 'BEGIN\n%s\nEND\n' "$input" >> "$FAKE_PROVIDER_STDIN_LOG"
fi

issue='{"number":31,"title":"typed issue","body":"task13-issue-body-marker","state":"open","user":{"login":"agent"},"assignees":[],"labels":[{"name":"security"}],"html_url":"https://safe.invalid/issue/31","created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-01T00:00:00Z"}'
case "$method $endpoint" in
  'GET /search/issues?'*)
    emit "{\"items\":[$issue]}"
    ;;
  'GET /repos/alpha/repo/pulls/7')
    emit '{"head":{"sha":"0123456789012345678901234567890123456789"}}'
    ;;
  'GET /repos/alpha/repo/commits/'*'/check-runs?'*)
    emit '{"total_count":1,"check_runs":[{"name":"unit","status":"completed","conclusion":"success","details_url":"https://safe.invalid/check","output":{"title":"unit"},"started_at":"2026-08-01T00:00:00Z","completed_at":"2026-08-01T00:01:00Z"}]}'
    ;;
  'GET /repos/alpha/repo/commits/'*'/status?'*)
    emit '{"total_count":1,"statuses":[{"context":"policy","state":"success","description":"safe","target_url":"https://safe.invalid/status","created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-01T00:01:00Z"}]}'
    ;;
  'POST /repos/alpha/repo/issues')
    emit '{"number":32,"title":"typed write","body":"task13-issue-body-marker","state":"open","user":{"login":"agent"},"assignees":[],"labels":[],"html_url":"https://safe.invalid/issue/32","created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-01T00:00:00Z"}'
    ;;
  'GET /repos/alpha/repo/issues/31')
    emit '{"number":31}'
    ;;
  'POST /repos/alpha/repo/issues/31/comments')
    emit '{"id":99,"user":{"login":"agent"},"body":"task13-comment-marker","html_url":"https://safe.invalid/comment/99","created_at":"2026-08-01T00:00:00Z","updated_at":"2026-08-01T00:00:00Z"}'
    ;;
  *)
    printf 'unexpected fake provider command\n' >&2
    exit 97
    ;;
esac
