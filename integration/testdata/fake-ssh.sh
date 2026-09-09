#!/bin/sh
set -eu

: "${FAKE_SSH_GITHUB_REPOSITORY:?}"
: "${FAKE_SSH_GITEA_REPOSITORY:?}"
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
  printf 'GH_TOKEN=%s\n' "${GH_TOKEN-unset}"
  printf 'GITHUB_TOKEN=%s\n' "${GITHUB_TOKEN-unset}"
  printf 'GH_PROMPT_DISABLED=%s\n' "${GH_PROMPT_DISABLED-unset}"
  printf 'GH_NO_UPDATE_NOTIFIER=%s\n' "${GH_NO_UPDATE_NOTIFIER-unset}"
  printf 'NO_COLOR=%s\n' "${NO_COLOR-unset}"
  printf 'REPOWOLF_TOKEN_AGENT=%s\n' "${REPOWOLF_TOKEN_AGENT-unset}"
  printf 'REPOWOLF_TOKEN_GITHUB=%s\n' "${REPOWOLF_TOKEN_GITHUB-unset}"
  printf 'REPOWOLF_TOKEN_GITEA=%s\n' "${REPOWOLF_TOKEN_GITEA-unset}"
  printf 'SSH_AUTH_SOCK=%s\n' "${SSH_AUTH_SOCK-unset}"
  printf 'GIT_PROTOCOL=%s\n' "${GIT_PROTOCOL-unset}"
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
    : "${FAKE_SSH_GITHUB_UPLOAD_INPUT:?}"
    : > "$FAKE_SSH_GITHUB_UPLOAD_INPUT"
    "$FAKE_TEE" "$FAKE_SSH_GITHUB_UPLOAD_INPUT" | "$FAKE_GIT_UPLOAD_PACK" "$FAKE_SSH_GITHUB_REPOSITORY"
    ;;
  "git-receive-pack 'alpha/repo.git'")
    : "${FAKE_GIT_RECEIVE_PACK:?}"
    : "${FAKE_SSH_GITHUB_RECEIVE_INPUT:?}"
    : > "$FAKE_SSH_GITHUB_RECEIVE_INPUT"
    "$FAKE_TEE" "$FAKE_SSH_GITHUB_RECEIVE_INPUT" | "$FAKE_GIT_RECEIVE_PACK" "$FAKE_SSH_GITHUB_REPOSITORY"
    ;;
  "git-upload-pack 'Team_Name/Repo.One.git'")
    : "${FAKE_GIT_UPLOAD_PACK:?}"
    : "${FAKE_SSH_GITEA_UPLOAD_INPUT:?}"
    : > "$FAKE_SSH_GITEA_UPLOAD_INPUT"
    "$FAKE_TEE" "$FAKE_SSH_GITEA_UPLOAD_INPUT" | "$FAKE_GIT_UPLOAD_PACK" "$FAKE_SSH_GITEA_REPOSITORY"
    ;;
  "git-receive-pack 'Team_Name/Repo.One.git'")
    : "${FAKE_GIT_RECEIVE_PACK:?}"
    : "${FAKE_SSH_GITEA_RECEIVE_INPUT:?}"
    : > "$FAKE_SSH_GITEA_RECEIVE_INPUT"
    "$FAKE_TEE" "$FAKE_SSH_GITEA_RECEIVE_INPUT" | "$FAKE_GIT_RECEIVE_PACK" "$FAKE_SSH_GITEA_REPOSITORY"
    ;;
  *)
    printf 'unexpected fake ssh command\n' >&2
    exit 98
    ;;
esac
