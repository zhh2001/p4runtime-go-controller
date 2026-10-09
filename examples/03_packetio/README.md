# Example 03: packet I/O

Subscribes to PacketIn messages and prints each packet's ingress port and payload. With `--send-port`, it sends a 60-byte Ethernet frame at startup, including both `egress_port` and zero `_pad` metadata.

Use Go 1.25 or newer and run commands from the repository root. Start BMv2 with `./scripts/run-bmv2.sh`, which compiles the L2 files, then run [example 02](../02_l2_switch/README.md) to install the pipeline. The command below sends to port 1, so bind a host-facing interface to that port. See [the fixture guide](../testdata/README.md) and [script requirements](../../scripts/README.md#requirements).

For an existing target, compile the L2 files with `./scripts/compile-l2.sh` and install that pair first. CPU loopback uses `--send-port 255` and works through the gRPC connection.

## Run

```sh
go run ./examples/03_packetio \
    --addr 127.0.0.1:9559 \
    --p4info ./examples/testdata/l2.p4info.txt \
    --send-port 1
```

Use Ctrl+C to exit.

Send an Ethernet frame with a destination absent from `MyIngress.t_l2` to a bound data port. The example prints a PacketIn with that ingress port and the original Ethernet frame, without the control header. Frames that match the table are forwarded to their configured data port.
