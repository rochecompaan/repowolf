#!/usr/bin/env bash
set -euo pipefail

host=${1:-127.0.0.1}
port=${2:-8443}
attempts=${3:-30}
if [[ ! $port =~ ^[0-9]+$ ]] || [ "$port" -eq 0 ] || [ "$port" -gt 65535 ] || \
   [[ ! $attempts =~ ^[1-9][0-9]*$ ]] || [ "$attempts" -gt 300 ]; then
  echo "usage: wait-for-broker.sh [host] [port] [attempts]" >&2
  exit 2
fi

for ((attempt = 1; attempt <= attempts; attempt++)); do
  if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then
    exit 0
  fi
  sleep 1
done

echo "wait-for-broker: $host:$port not ready after $attempts attempts" >&2
exit 1
