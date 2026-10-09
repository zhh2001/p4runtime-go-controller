#!/usr/bin/env bash
set -euo pipefail

# gen-docs.sh regenerates the API index and package pages using gomarkdoc.
# Install with
#   go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@v1.1.0
# Rendering and checking arguments are passed to gomarkdoc, such as --check.
# Output paths and file templates are set below.

if ! command -v gomarkdoc >/dev/null 2>&1; then
  echo "gomarkdoc not found; install with: go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@v1.1.0" >&2
  exit 1
fi

cd "$(dirname "$0")/.."

package_template=$(<scripts/templates/api-package.gotxt)
index_template=$(<scripts/templates/api-index.gotxt)

packages=(
  ./client
  ./pipeline
  ./tableentry
  ./codec
  ./packetio
  ./digest
  ./counter
  ./meter
  ./register
  ./pre
  ./errors
  ./metrics
)

# Check one package per invocation so every stale page fails the command.
for package in "${packages[@]}"; do
  gomarkdoc \
    "$@" \
    --output "docs/api/${package#./}.md" \
    --template "file=$package_template" \
    "$package"
done

gomarkdoc \
  "$@" \
  --output docs/api-reference.md \
  --template "file=$index_template" \
  "${packages[@]}"
