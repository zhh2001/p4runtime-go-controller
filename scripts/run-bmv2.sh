#!/usr/bin/env bash
set -euo pipefail

# run-bmv2.sh — launch BMv2 (simple_switch_grpc) in a Docker container, bound
# to the local testdata fixtures. Useful for running the example programs and
# the integration test suite against a P4Runtime target.
#
# Usage:
#   scripts/run-bmv2.sh [-p 9559] [-i p4lang/p4c:stable]

PORT="9559"
IMAGE="p4lang/behavioral-model:latest"
NAME="p4runtime-go-controller-bmv2"
TESTDATA="$(cd "$(dirname "$0")/.." && pwd)/examples/testdata"

while getopts "p:i:h" opt; do
  case "${opt}" in
    p) PORT="${OPTARG}" ;;
    i) IMAGE="${OPTARG}" ;;
    h)
      echo "Usage: $(basename "$0") [-p port] [-i image]"
      exit 0
      ;;
    *)
      echo "Unknown option"
      exit 2
      ;;
  esac
done

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required to run this script" >&2
  exit 1
fi

if [[ ! -f "${TESTDATA}/l2.bmv2.json" ]]; then
  cat >&2 <<EOF
Expected compiled device config at ${TESTDATA}/l2.bmv2.json.
Run p4c locally to produce it, for example:
  p4c --target bmv2 --arch v1model \\
      --p4runtime-files ${TESTDATA}/l2.p4info.txt \\
      -o ${TESTDATA} \\
      ${TESTDATA}/l2.p4
EOF
  exit 1
fi

echo "starting BMv2 container '${NAME}' on port ${PORT} using image ${IMAGE}"
docker rm -f "${NAME}" >/dev/null 2>&1 || true

docker run -d \
  --name "${NAME}" \
  -p "${PORT}:${PORT}" \
  -v "${TESTDATA}:/testdata:ro" \
  "${IMAGE}" \
  simple_switch_grpc \
    --no-p4 \
    --log-console \
    -- \
    --grpc-server-addr "0.0.0.0:${PORT}" >/dev/null

echo "BMv2 running. Attach with:"
echo "  docker logs -f ${NAME}"
echo "Stop with:"
echo "  docker rm -f ${NAME}"
