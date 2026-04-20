# Example 04: counter read

Reads every index of the named indirect counter and prints one line per entry.

## Run

```sh
go run ./examples/04_counters \
    --addr 127.0.0.1:9559 \
    --p4info ./examples/testdata/l2.p4info.txt \
    --counter ingress.pkt_counter
```
