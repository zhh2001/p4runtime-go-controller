# examples/testdata

The L2 program and its P4Info for the examples and integration tests.

| File            | Purpose                                                           |
| --------------- | ----------------------------------------------------------------- |
| `l2.p4`         | Source for the minimal L2 switch program.                         |
| `l2.p4info.txt` | Generated P4Info in text format, checked in for metadata lookups. |
| `l2.bmv2.json`  | Generated BMv2 device config, kept out of Git.                    |

## Regenerate

```sh
./scripts/compile-l2.sh
```

The script requires `p4c-bm2-ss`, included with p4c. It generates both files from the same source in one compilation. Pass an output directory to keep a separate pair, for example `./scripts/compile-l2.sh /tmp/l2`. Use both files from that directory when installing the pipeline. Do not combine a new JSON with an older P4Info.

## Resources and packet flow

`MyIngress.t_l2` matches `hdr.eth.dst`. Its `MyIngress.forward(port)` action sends a matching Ethernet frame to the specified 9-bit port. An unmatched frame goes to CPU port 255 as a PacketIn.

`MyIngress.pkt_counter` has 512 entries. The forward action counts packets and bytes at the egress port's index. `MyIngress.pkt_counter_direct` counts the same traffic for each L2 table entry. PacketOut bypasses the L2 table.

PacketIn metadata contains `ingress_port` and `_pad`. PacketOut metadata contains `egress_port` and `_pad`. Both port fields are 9 bits, and `_pad` is 7 bits with value zero. The payload is a complete Ethernet frame. The P4 parser removes the PacketOut control header, and the deparser prepends a PacketIn control header only for frames sent to port 255.

## Native BMv2

Start a target with device ID 1 and CPU port 255:

```sh
simple_switch_grpc --no-p4 --device-id 1 \
    -- --grpc-server-addr 127.0.0.1:9559 --cpu-port 255
```

For traffic, bind host-facing interfaces before `--`, for example `-i 1@veth1 -i 2@veth2`. These interfaces must already exist, and BMv2 needs permission to open raw sockets on them. Examples 02 and 03 use port 1 by default in their documented commands.
