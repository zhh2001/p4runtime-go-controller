#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GO_BIN="${GO:-go}"

if ! command -v "${GO_BIN}" >/dev/null 2>&1; then
  echo "${GO_BIN} is required to run the integration tests" >&2
  exit 1
fi

if [[ -z "${P4RT_P4INFO:-}" && -z "${P4RT_DEVICE_CONFIG:-}" ]]; then
  OUTPUT="${ROOT}/build/integration/l2"
  "${ROOT}/scripts/compile-l2.sh" "${OUTPUT}"
  P4RT_P4INFO="${OUTPUT}/l2.p4info.txt"
  P4RT_DEVICE_CONFIG="${OUTPUT}/l2.bmv2.json"
elif [[ -z "${P4RT_P4INFO:-}" || -z "${P4RT_DEVICE_CONFIG:-}" ]]; then
  echo "Set P4RT_P4INFO and P4RT_DEVICE_CONFIG together" >&2
  exit 2
fi

for FILE in "${P4RT_P4INFO}" "${P4RT_DEVICE_CONFIG}"; do
  if [[ ! -s "${FILE}" ]]; then
    echo "Pipeline file is missing or empty: ${FILE}" >&2
    exit 1
  fi
done

P4RT_P4INFO="$(cd "$(dirname "${P4RT_P4INFO}")" && pwd)/$(basename "${P4RT_P4INFO}")"
P4RT_DEVICE_CONFIG="$(cd "$(dirname "${P4RT_DEVICE_CONFIG}")" && pwd)/$(basename "${P4RT_DEVICE_CONFIG}")"
P4RT_TARGET="${P4RT_TARGET:-127.0.0.1:9559}"
export P4RT_P4INFO P4RT_DEVICE_CONFIG P4RT_TARGET

cd "${ROOT}"
echo "Running BMv2 integration tests against ${P4RT_TARGET}"
exec "${GO_BIN}" test -race -tags=integration -count=1 "$@" ./test/integration/...
