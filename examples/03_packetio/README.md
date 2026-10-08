# Example 03: packet I/O

Subscribes to PacketIn messages and prints each packet's ingress port and payload. With `--send-port`, it sends a 60-byte Ethernet frame at startup, including both `egress_port` and zero `_pad` metadata.

First compile the L2 fixtures and run example 02 to install the pipeline. Start BMv2 with CPU port 255 and a host-facing interface on the chosen output port. See [the fixture guide](../testdata/README.md).

## Run

```sh
go run ./examples/03_packetio \
    --addr 127.0.0.1:9559 \
    --p4info ./examples/testdata/l2.p4info.txt \
    --send-port 1
```

Use Ctrl+C to exit.

Send an Ethernet frame with a destination absent from `MyIngress.t_l2` to a bound data port. The example prints a PacketIn with that ingress port and the original Ethernet frame, without the control header. Frames that match the table are forwarded to their configured data port.
