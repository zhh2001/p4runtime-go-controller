#!/usr/bin/env bash
set -euo pipefail

# check-shell.sh runs shellcheck over every tracked shell script.

cd "$(dirname "$0")/.."

if ! command -v shellcheck >/dev/null 2>&1; then
  echo "shellcheck not found; install from https://www.shellcheck.net/" >&2
  exit 1
fi

find scripts -type f -name "*.sh" -print0 | xargs -0 shellcheck
