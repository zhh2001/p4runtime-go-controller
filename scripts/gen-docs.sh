#!/usr/bin/env bash
set -euo pipefail

# gen-docs.sh regenerates docs/api-reference.md using gomarkdoc. Install with
#   go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@v1.1.0
# Additional arguments are passed to gomarkdoc, such as --check.

if ! command -v gomarkdoc >/dev/null 2>&1; then
  echo "gomarkdoc not found; install with: go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@v1.1.0" >&2
  exit 1
fi

cd "$(dirname "$0")/.."

gomarkdoc \
  --output docs/api-reference.md \
  "$@" \
  ./client \
  ./pipeline \
  ./tableentry \
  ./codec \
  ./packetio \
  ./digest \
  ./counter \
  ./meter \
  ./register \
  ./pre \
  ./errors \
  ./metrics
