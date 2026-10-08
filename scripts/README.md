# BMv2 scripts

Run these commands from the repository root.

## Start a target

```bash
./scripts/run-bmv2.sh
```

The default uses the local `simple_switch_grpc` in the foreground, with device ID 1, gRPC port 9559, Thrift port 9090 and CPU port 255. It requires `p4c-bm2-ss` and compiles both L2 artifacts before starting. The target starts without a pipeline. Run example 02, the pipeline CLI or the integration tests to install one.

Choose other ports when those ports are already in use:

```bash
./scripts/run-bmv2.sh -p 10559 -t 10090
P4RT_TARGET=127.0.0.1:10559 make e2e
```

Native mode can bind existing data interfaces. Opening raw sockets requires the corresponding permissions:

```bash
sudo ./scripts/run-bmv2.sh --interface 1@veth1 --interface 2@veth2
```

CPU port 255 and drop port 511 cannot be data bindings. Interfaces must already exist. The script leaves their creation and removal to the caller. Stop the foreground target with Ctrl+C.

## Docker

```bash
./scripts/run-bmv2.sh --docker --name l2-test
```

Docker mode requires an accessible Docker daemon and local p4c. It publishes the gRPC port on `127.0.0.1`, waits for the container's listener and leaves the target running in the background. The default image is `p4lang/behavioral-model:latest`; `-i IMAGE` selects another image and also selects Docker mode. The image must provide `simple_switch_grpc`.

The script refuses an existing container name. It never removes that container. If its own new container fails during startup, it prints the logs and removes only that container. Use another name or stop the existing target yourself:

```bash
docker logs -f l2-test
docker stop l2-test
docker rm l2-test
```

Host interface bindings are available in native mode. Docker mode provides the control plane and CPU Packet I/O tests without binding host data interfaces.

## Artifacts and tests

`--output DIRECTORY` changes the launcher's compilation directory. Its default is `examples/testdata`, matching the examples' default paths. P4Info and JSON are always generated together with `compile-l2.sh`. For the [BMv2 command-line options](https://github.com/p4lang/behavioral-model/blob/main/targets/simple_switch_grpc/README.md), use the upstream guide.

`make e2e`, `task e2e` and `./scripts/test-bmv2.sh` use the same test entry point. With no pipeline paths set, it compiles the L2 pair into `build/integration/l2`. To supply an existing pair:

```bash
P4RT_P4INFO=/tmp/l2/l2.p4info.txt \
P4RT_DEVICE_CONFIG=/tmp/l2/l2.bmv2.json \
./scripts/test-bmv2.sh -v -count=3
```

Both paths must be set and nonempty. The default test run covers L2 pipeline operations, arbitration and CPU PacketOut/PacketIn loopback. Other suites require the settings in the [integration guide](../test/integration/README.md). Tests install their pipeline on the selected target, so use a target dedicated to testing.
