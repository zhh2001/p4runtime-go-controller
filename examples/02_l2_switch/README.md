# Example 02: L2 learning switch seed

Pushes a pipeline onto the target and seeds a single L2 MAC → port entry.

## Prerequisites

- A running P4Runtime target (typically BMv2 started via
  [`scripts/run-bmv2.sh`](../../scripts/run-bmv2.sh)).
- A P4 program that exposes an `ingress.t_l2` table with an EXACT match on
  `hdr.eth.dst` and an `ingress.forward(port)` action (9-bit port). The
  `examples/testdata/` directory ships a compatible `l2.p4info.txt` and
  `l2.bmv2.json`.

## Run

```sh
go run ./examples/02_l2_switch \
    --addr 127.0.0.1:9559 \
    --p4info ./examples/testdata/l2.p4info.txt \
    --config ./examples/testdata/l2.bmv2.json
```

Expected output:

```
pipeline installed via VERIFY_AND_COMMIT
wrote 1 entry
```
