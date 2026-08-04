#!/usr/bin/env bash
set -euo pipefail

client_root=$1
git_root=$2
shift 2

# Bubblewrap injects only the four client contract variables. Bash and the
# runtime may also export their bookkeeping variables after startup.
for name in $(compgen -e); do
  case "$name" in
    REPOWOLF_ENDPOINT|REPOWOLF_TOKEN|REPOWOLF_CA_FILE|GIT_SSH_COMMAND|OLDPWD|PWD|SHLVL|TMPDIR|_)
      ;;
    *)
      printf 'unexpected jail environment variable: %s\n' "$name" >&2
      exit 20
      ;;
  esac
done
for name in HOME GH_TOKEN GITHUB_TOKEN SSH_AUTH_SOCK REPOWOLF_TOKEN_AGENT REPOWOLF_SERVER_NAME; do
  if [[ -v "$name" ]]; then
    printf 'forbidden jail environment variable: %s\n' "$name" >&2
    exit 21
  fi
done

shopt -s nullglob dotglob
home_entries=(/home/jail/*)
((${#home_entries[@]} == 0)) || { printf 'jail home is not empty\n' >&2; exit 22; }
[[ -r /proc/self/status && -e /dev/null ]] || { printf 'proc or dev is unavailable\n' >&2; exit 23; }
: > /tmp/repowolf-jail-write-test
[[ -r "$REPOWOLF_CA_FILE" ]] || { printf 'trusted CA is unavailable\n' >&2; exit 24; }

[[ -x "$client_root/bin/gh" && -x "$client_root/bin/repowolf-git-ssh" ]] || exit 25
[[ ! -e "$client_root/bin/repowolf" && ! -e "$client_root/bin/ssh" ]] || exit 26
command -v gh >/dev/null 2>&1 && { printf 'host gh is discoverable\n' >&2; exit 27; }
command -v ssh >/dev/null 2>&1 && { printf 'host ssh is discoverable\n' >&2; exit 28; }
[[ ! -e /etc/gh && ! -e /etc/ssh && ! -e /home/jail/.config/gh && ! -e /home/jail/.ssh ]] || exit 29
for path in "$@"; do
  [[ ! -e "$path" ]] || { printf 'forbidden host path is visible: %s\n' "$path" >&2; exit 30; }
done

[[ -t 0 ]] || { printf 'stdin is not a TTY\n' >&2; exit 31; }
IFS= read -r forwarded
[[ "$forwarded" == task15-forwarded-stdin-marker ]] || { printf 'stdin was not forwarded\n' >&2; exit 31; }

issue_output="$("$client_root"/bin/gh issue list --repo alpha/repo)"
[[ "$issue_output" == $'31\ttyped issue\topen\tsecurity\t2026-08-01T00:00:00Z' ]] || {
  printf 'restricted gh output mismatch: %s\n' "$issue_output" >&2
  exit 32
}

"$git_root/bin/git" -C /work/checkout fetch --no-write-fetch-head origin refs/heads/main \
  >/tmp/fetch.stdout 2>/tmp/fetch.stderr
candidate="$("$git_root"/bin/git -C /work/checkout rev-parse refs/heads/feature/candidate)"
remote_url=ssh://git@github.com:22/alpha/repo.git
"$git_root/bin/git" -C /work/checkout push "$remote_url" \
  "$candidate:refs/heads/feature/task15" >/tmp/allowed.stdout 2>/tmp/allowed.stderr
if "$git_root/bin/git" -C /work/checkout push "$remote_url" \
  "$candidate:refs/heads/main" >/tmp/denied.stdout 2>/tmp/denied.stderr; then
  printf 'exact-main push unexpectedly succeeded\n' >&2
  exit 33
fi

denial_seen=false
while IFS= read -r line; do
  [[ "$line" == *"repowolf git transport failed"* ]] && denial_seen=true
done < /tmp/denied.stderr
[[ "$denial_seen" == true ]] || { printf 'sanitized denial missing\n' >&2; exit 34; }
