# Example 04: counter read

Reads the L2 program's indirect counter. Indexes correspond to egress ports. Run example 02 and send frames matching its entry before reading index 1. Both this counter and the table's direct counter count packets and bytes handled by the L2 forward action.

## Run

```sh
go run ./examples/04_counters \
    --addr 127.0.0.1:9559 \
    --p4info ./examples/testdata/l2.p4info.txt \
    --counter MyIngress.pkt_counter --index 1
```

Omit `--index` or use `--index -1` to read all 512 indexes.
