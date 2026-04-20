# Example 03: packet I/O

Subscribes to PacketIn messages, optionally sends a demo PacketOut at startup,
and prints a one-line summary of every packet received.

## Run

```sh
go run ./examples/03_packetio \
    --addr 127.0.0.1:9559 \
    --p4info ./examples/testdata/l2.p4info.txt \
    --send-port 1
```

Use Ctrl+C to exit.
