# Example 02: L2 table

Installs the L2 pipeline and writes an entry forwarding destination `00:11:22:33:44:55` to port 1.

## Prerequisites

- Compile the bundled P4 program with `./scripts/compile-l2.sh`.
- Start BMv2 with device ID 1 and CPU port 255 as described in [the fixture guide](../testdata/README.md).
- Bind a host-facing interface to port 1 to receive forwarded frames.

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
