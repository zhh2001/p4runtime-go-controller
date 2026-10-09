# Example 02: L2 table

Installs the L2 pipeline and writes an entry forwarding destination `00:11:22:33:44:55` to port 1.

## Prerequisites

- Use Go 1.25 or newer and run commands from the repository root.
- Start BMv2 with device ID 1 and CPU port 255 using `./scripts/run-bmv2.sh`. It compiles the matching L2 files used below. See [script requirements](../../scripts/README.md#requirements).
- Binding a host-facing interface to port 1 enables forwarded traffic. See [the fixture guide](../testdata/README.md).

For an existing target, generate the matching files with `./scripts/compile-l2.sh` before running the example. Use both files from the same compilation.

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
