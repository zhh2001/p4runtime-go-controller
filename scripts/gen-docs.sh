#!/usr/bin/env bash
set -euo pipefail

# gen-docs.sh regenerates docs/api-reference.md using gomarkdoc. Install with
#   go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest

if ! command -v gomarkdoc >/dev/null 2>&1; then
  echo "gomarkdoc not found; install with: go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest" >&2
  exit 1
fi

cd "$(dirname "$0")/.."

gomarkdoc \
  --output docs/api-reference.md \
  ./client \
  ./pipeline \
  ./tableentry \
  ./packetio \
  ./digest \
  ./counter \
  ./meter \
  ./register \
  ./errors \
  ./metrics
