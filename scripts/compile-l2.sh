#!/usr/bin/env bash
set -euo pipefail

# Compile the example's P4Info and BMv2 configuration together.
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT="${1:-${ROOT}/examples/testdata}"
COMPILER="${P4C_BM2_SS:-p4c-bm2-ss}"

if [[ ${1:-} == "-h" || ${1:-} == "--help" ]]; then
  echo "Usage: $(basename "$0") [output-directory]"
  exit 0
fi
if [[ $# -gt 1 ]]; then
  echo "Usage: $(basename "$0") [output-directory]" >&2
  exit 2
fi
if ! command -v "${COMPILER}" >/dev/null 2>&1; then
  echo "${COMPILER} is required to compile the L2 example" >&2
  exit 1
fi

mkdir -p -- "${OUTPUT}"
OUTPUT="$(cd "${OUTPUT}" && pwd)"
STAGING="$(mktemp -d "${OUTPUT}/.l2-build.XXXXXX")"
trap 'rm -rf "${STAGING}"' EXIT

"${COMPILER}" --arch v1model \
  --p4runtime-files "${STAGING}/l2.p4info.txtpb" \
  -o "${STAGING}/l2.bmv2.json" \
  "${ROOT}/examples/testdata/l2.p4"

mv "${STAGING}/l2.bmv2.json" "${OUTPUT}/l2.bmv2.json"
mv "${STAGING}/l2.p4info.txtpb" "${OUTPUT}/l2.p4info.txt"
echo "Compiled L2 fixtures in ${OUTPUT}"
