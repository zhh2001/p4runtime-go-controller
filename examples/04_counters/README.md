# Example 04: counter read

Reads the L2 program's indirect counter. Indexes correspond to egress ports. Run example 02 and send frames matching its entry before reading index 1. Both this counter and the table's direct counter count packets and bytes handled by the L2 forward action.

## Run

Use Go 1.26 or newer and run from the repository root. Follow [example 02](../02_l2_switch/README.md) to start BMv2, generate the matching L2 files and install the pipeline. Counter reads use the gRPC connection. Nonzero counts require matching traffic on bound data ports.

```sh
go run ./examples/04_counters \
    --addr 127.0.0.1:9559 \
    --p4info ./examples/testdata/l2.p4info.txt \
    --counter MyIngress.pkt_counter --index 1
```

Omit `--index` or use `--index -1` to read all 512 indexes.
