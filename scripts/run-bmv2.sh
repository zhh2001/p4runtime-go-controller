#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BACKEND="native"
PORT="9559"
THRIFT_PORT="9090"
IMAGE="p4lang/behavioral-model:latest"
NAME="p4runtime-go-controller-bmv2"
OUTPUT="${ROOT}/examples/testdata"
SWITCH="${SIMPLE_SWITCH_GRPC:-simple_switch_grpc}"
INTERFACES=()

usage() {
  cat <<EOF
Usage: $(basename "$0") [--native | --docker] [options]
  -p, --port PORT          gRPC port (default 9559)
  -t, --thrift-port PORT   Thrift port (default 9090)
  -i, --image IMAGE        Docker image, selects Docker mode
  -n, --name NAME          Docker container name
      --interface PORT@IFACE  Bind a native data port, repeatable
      --output DIRECTORY  Directory for compiled L2 artifacts
  -h, --help              Show this help

Device ID is 1 and CPU port is 255 for the bundled L2 program.
Native mode is the default and runs in the foreground.
Docker mode starts a detached container.
EOF
}

fail() {
  echo "$*" >&2
  exit 2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --native) BACKEND="native"; shift ;;
    --docker) BACKEND="docker"; shift ;;
    -p?*|-t?*|-i?*|-n?*) set -- "${1:0:2}" "${1:2}" "${@:2}" ;;
    -p|--port|-t|--thrift-port|-i|--image|-n|--name|--output|--interface)
      [[ $# -ge 2 && -n "$2" ]] || fail "Missing value for $1"
      case "$1" in
        -p|--port) PORT="$2" ;;
        -t|--thrift-port) THRIFT_PORT="$2" ;;
        -i|--image) IMAGE="$2"; BACKEND="docker" ;;
        -n|--name) NAME="$2" ;;
        --output) OUTPUT="$2" ;;
        --interface) INTERFACES+=("$2") ;;
      esac
      shift 2
      ;;
    -h|--help) usage; exit 0 ;;
    *) fail "Unknown argument: $1" ;;
  esac
done

valid_port() {
  [[ "$1" =~ ^[0-9]{1,5}$ ]] && (( 10#$1 >= 1 && 10#$1 <= 65535 ))
}
valid_port "${PORT}" || fail "Invalid gRPC port: ${PORT}"
valid_port "${THRIFT_PORT}" || fail "Invalid Thrift port: ${THRIFT_PORT}"
PORT="$((10#${PORT}))"
THRIFT_PORT="$((10#${THRIFT_PORT}))"
[[ "${PORT}" != "${THRIFT_PORT}" ]] || fail "gRPC and Thrift ports must differ"

# Bash 3.2 treats an empty array as unset under nounset.
for BINDING in ${INTERFACES[@]+"${INTERFACES[@]}"}; do
  [[ "${BINDING}" =~ ^([0-9]{1,3})@([a-zA-Z0-9_.:-]+)$ ]] || fail "Invalid interface binding: ${BINDING}"
  DATA_PORT="$((10#${BASH_REMATCH[1]}))"
  (( DATA_PORT < 511 && DATA_PORT != 255 )) || fail "Data ports must be 0..510 except CPU port 255"
done

if [[ "${BACKEND}" == "native" ]]; then
  command -v "${SWITCH}" >/dev/null 2>&1 || fail "${SWITCH} is required for native mode"
else
  [[ ${#INTERFACES[@]} -eq 0 ]] || fail "Use --native to bind host interfaces"
  [[ "${NAME}" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]] || fail "Invalid Docker container name: ${NAME}"
  [[ "${IMAGE}" != -* ]] || fail "Invalid Docker image: ${IMAGE}"
  command -v docker >/dev/null 2>&1 || fail "docker is required for Docker mode"
  docker info >/dev/null 2>&1 || fail "Docker daemon is unavailable or access is denied"
  if docker container inspect "${NAME}" >/dev/null 2>&1; then
    fail "Container ${NAME} already exists. Choose another --name or stop and remove it first"
  fi
fi

"${ROOT}/scripts/compile-l2.sh" "${OUTPUT}"
OUTPUT="$(cd "${OUTPUT}" && pwd)"
SWITCH_ARGS=(--no-p4 --device-id 1 --thrift-port "${THRIFT_PORT}" --log-console)
for BINDING in ${INTERFACES[@]+"${INTERFACES[@]}"}; do
  SWITCH_ARGS+=(-i "${BINDING}")
done

echo "Starting BMv2 with device ID 1 and CPU port 255"
echo "Integration tests: P4RT_TARGET=127.0.0.1:${PORT} make e2e"
if [[ "${BACKEND}" == "native" ]]; then
  SWITCH_ARGS+=(--notifications-addr "ipc:///tmp/p4runtime-bmv2-${PORT}-notifications.ipc")
  exec "${SWITCH}" "${SWITCH_ARGS[@]}" -- --grpc-server-addr "127.0.0.1:${PORT}" --cpu-port 255
fi

CONTAINER_ID="$(docker create --name "${NAME}" \
  -p "127.0.0.1:${PORT}:${PORT}" \
  -v "${OUTPUT}:/testdata:ro" \
  --entrypoint simple_switch_grpc \
  "${IMAGE}" "${SWITCH_ARGS[@]}" -- --grpc-server-addr "0.0.0.0:${PORT}" --cpu-port 255)"

trap 'docker rm -f "${CONTAINER_ID}" >/dev/null 2>&1 || true' EXIT
if ! docker start "${CONTAINER_ID}" >/dev/null; then
  docker logs "${CONTAINER_ID}" >&2
  exit 1
fi
for ((ATTEMPT=0; ATTEMPT<100; ATTEMPT++)); do
  if [[ "$(docker container inspect --format '{{.State.Running}}' "${CONTAINER_ID}")" != "true" ]]; then
    docker logs "${CONTAINER_ID}" >&2
    echo "BMv2 container exited during startup" >&2
    exit 1
  fi
  if (exec 3<>"/dev/tcp/127.0.0.1/${PORT}") 2>/dev/null; then
    trap - EXIT
    echo "Container: ${NAME}"
    echo "Logs: docker logs -f ${NAME}"
    echo "Stop: docker stop ${NAME} && docker rm ${NAME}"
    exit 0
  fi
  sleep 0.1
done
docker logs "${CONTAINER_ID}" >&2
echo "BMv2 did not listen on port ${PORT} within 10 seconds" >&2
exit 1
