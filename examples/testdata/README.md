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

Start a local target with device ID 1 and CPU port 255. The launcher compiles both artifacts first:

```sh
./scripts/run-bmv2.sh
```

For traffic, bind existing host-facing interfaces with `--interface 1@veth1 --interface 2@veth2`. BMv2 needs permission to open raw sockets on them. Examples 02 and 03 use port 1 by default in their documented commands. Docker mode provides a control plane target through `--docker`; host interface bindings use native mode. See [the script guide](../../scripts/README.md).
