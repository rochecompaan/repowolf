#!/bin/sh
set -eu

: "${FAKE_SSH_REPOSITORY:?}"
: "${FAKE_SSH_ARGV_LOG:?}"
: "${FAKE_SSH_STDERR:?}"
{
  printf 'BEGIN\n'
  printf '%s\n' "$@"
  printf 'END\n'
} >> "$FAKE_SSH_ARGV_LOG"
printf '%s\n' "$FAKE_SSH_STDERR" >&2

remote=''
for argument in "$@"; do remote=$argument; done
case "$remote" in
  "git-upload-pack 'alpha/repo.git'")
    : "${FAKE_GIT_UPLOAD_PACK:?}"
    : "${FAKE_SSH_UPLOAD_INPUT:?}"
    : > "$FAKE_SSH_UPLOAD_INPUT"
    tee "$FAKE_SSH_UPLOAD_INPUT" | "$FAKE_GIT_UPLOAD_PACK" "$FAKE_SSH_REPOSITORY"
    ;;
  "git-receive-pack 'alpha/repo.git'")
    : "${FAKE_GIT_RECEIVE_PACK:?}"
    : "${FAKE_SSH_RECEIVE_INPUT:?}"
    : > "$FAKE_SSH_RECEIVE_INPUT"
    tee "$FAKE_SSH_RECEIVE_INPUT" | "$FAKE_GIT_RECEIVE_PACK" "$FAKE_SSH_REPOSITORY"
    ;;
  *)
    printf 'unexpected fake ssh command\n' >&2
    exit 98
    ;;
esac
