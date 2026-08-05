#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
tools="$(mktemp -d)"
trap 'rm -rf "$tools"' EXIT
cd "$root"
go build -o "$tools/protoc-gen-go" google.golang.org/protobuf/cmd/protoc-gen-go
go build -o "$tools/protoc-gen-go-grpc" google.golang.org/grpc/cmd/protoc-gen-go-grpc
PATH="$tools:$PATH" go tool buf generate
