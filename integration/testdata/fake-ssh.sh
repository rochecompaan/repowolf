#!/bin/sh
set -eu

: "${FAKE_SSH_REPOSITORY:?}"
: "${FAKE_SSH_ARGV_LOG:?}"
: "${FAKE_SSH_ENV_LOG:?}"
: "${FAKE_SSH_STDERR:?}"
: "${FAKE_TEE:?}"
{
  printf 'BEGIN\n'
  printf '%s\n' "$@"
  printf 'END\n'
} >> "$FAKE_SSH_ARGV_LOG"
{
  printf 'GH_TOKEN=%s\n' "${GH_TOKEN-}"
  printf 'TASK13_ENV_MARKER=%s\n' "${TASK13_ENV_MARKER-}"
  printf 'REPOWOLF_TOKEN_AGENT=%s\n' "${REPOWOLF_TOKEN_AGENT-unset}"
  printf 'REPOWOLF_ENDPOINT=%s\n' "${REPOWOLF_ENDPOINT-unset}"
  printf 'FAKE_GIT_UPLOAD_PACK=%s\n' "$FAKE_GIT_UPLOAD_PACK"
  printf 'FAKE_GIT_RECEIVE_PACK=%s\n' "$FAKE_GIT_RECEIVE_PACK"
  printf 'FAKE_TEE=%s\n' "$FAKE_TEE"
} >> "$FAKE_SSH_ENV_LOG"
printf '%s\n' "$FAKE_SSH_STDERR" >&2

remote=''
for argument in "$@"; do remote=$argument; done
case "$remote" in
  "git-upload-pack 'alpha/repo.git'")
    : "${FAKE_GIT_UPLOAD_PACK:?}"
    : "${FAKE_SSH_UPLOAD_INPUT:?}"
    : > "$FAKE_SSH_UPLOAD_INPUT"
    "$FAKE_TEE" "$FAKE_SSH_UPLOAD_INPUT" | "$FAKE_GIT_UPLOAD_PACK" "$FAKE_SSH_REPOSITORY"
    ;;
  "git-receive-pack 'alpha/repo.git'")
    : "${FAKE_GIT_RECEIVE_PACK:?}"
    : "${FAKE_SSH_RECEIVE_INPUT:?}"
    : > "$FAKE_SSH_RECEIVE_INPUT"
    "$FAKE_TEE" "$FAKE_SSH_RECEIVE_INPUT" | "$FAKE_GIT_RECEIVE_PACK" "$FAKE_SSH_REPOSITORY"
    ;;
  *)
    printf 'unexpected fake ssh command\n' >&2
    exit 98
    ;;
esac
