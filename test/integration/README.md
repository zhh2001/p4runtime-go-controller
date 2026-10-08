# BMv2 integration tests

Use a BMv2 P4Runtime target with device ID 1. Tests install their pipeline on the target selected by `P4RT_TARGET`, which defaults to `127.0.0.1:9559`.

From the repository root, start the target in one terminal:

```bash
./scripts/run-bmv2.sh
```

In another terminal, run `make e2e` or `./scripts/test-bmv2.sh`. The test script compiles a fresh L2 pair into `build/integration/l2`, exports both pipeline paths and runs with race detection. L2 pipeline, arbitration and CPU port tests run without extra settings. The CPU test sends a frame back through port 255 and verifies the PacketIn payload and metadata, so it needs no network interfaces.

For custom ports, start with `./scripts/run-bmv2.sh -p 10559 -t 10090` and use `P4RT_TARGET=127.0.0.1:10559 make e2e`. To reuse L2 artifacts, set `P4RT_P4INFO` and `P4RT_DEVICE_CONFIG` together. These tests expect the bundled L2 program. With only one path set, the test script returns an error. Extra Go test flags can be passed directly, for example `./scripts/test-bmv2.sh -v -run '^TestBMv2_PacketCPUPort$'`.

Enable masks, table CLI, raw Ethernet, examples and the fresh-target test with the settings below.

The 9-bit LPM, TERNARY, and RANGE tests use `testdata/masks.p4`. From the repository root, compile its P4Info and device configuration together:

```bash
mkdir -p /tmp/p4runtime-integration/masks
p4c --target bmv2 --arch v1model \
  --p4runtime-files /tmp/p4runtime-integration/masks/masks.p4info.txtpb \
  -o /tmp/p4runtime-integration/masks test/integration/testdata/masks.p4
P4RT_MASK_P4INFO=/tmp/p4runtime-integration/masks/masks.p4info.txtpb \
P4RT_MASK_DEVICE_CONFIG=/tmp/p4runtime-integration/masks/masks.json \
go test -race -tags=integration -count=1 -run '^TestBMv2_FieldWidthMasks$' ./test/integration/...
```

The tests cover full and partial prefixes, short masks, and padded inputs. Range cases include padded low and high endpoints, equal and zero endpoints, and values on either side of a byte boundary. Each entry is inserted, read back, and deleted. Both mask environment variables must be set to run this test. When neither is set, it is skipped.

The L2 pipeline and arbitration tests use `P4RT_P4INFO` and `P4RT_DEVICE_CONFIG`. Generate a matching pair with `./scripts/compile-l2.sh`, then set them to `examples/testdata/l2.p4info.txt` and `examples/testdata/l2.bmv2.json`. Set both sets of pipeline variables to run the full integration suite against one target.

The L2 tests also check duplicate inserts, missing-entry modifications and deletions, and a partially successful batch. They verify the per-update error indices and read back the batch's successful insert. A separate test submits a stale election ID and checks that the target's permission response remains available.

`TestBMv2_PipelineActions` saves the config from `testdata/masks.p4`, writes a TERNARY entry, then commits with no config. It reads back the saved config and entry. It also checks that `VERIFY` and `RECONCILE_AND_COMMIT` retain the entry, while `VERIFY_AND_COMMIT` clears it. This test uses `P4RT_MASK_P4INFO` and `P4RT_MASK_DEVICE_CONFIG`, which must come from the same compilation.

To check writes before pipeline installation, start a fresh target and run this test separately, before the rest of the suite:

```bash
P4RT_TEST_FRESH_TARGET=1 go test -race -tags=integration -count=1 \
  -run '^TestBMv2_WriteWithoutPipeline$' ./test/integration/...
```

Do not set `P4RT_TEST_FRESH_TARGET` when running the full suite. Other tests install a pipeline.

Some PI versions check LPM trailing zeros against the padded byte width. They reject these 9-bit LPM cases with `Invalid LPM value, incorrect number of trailing zeros`. These cases require PI to use the field width for that check. The TERNARY cases can be run separately with `-run '^TestBMv2_FieldWidthMasks$/ternary'`.

The RANGE cases can be run separately with `-run '^TestBMv2_FieldWidthMasks$/range'`.

## CLI table operations

`TestBMv2_TableCLI` runs the actual CLI against EXACT, LPM, TERNARY, RANGE, OPTIONAL, and mixed-match tables from `testdata/table_keys.p4`. It inserts two entries, modifies one, deletes it without an action, and reads back the other entry. It also checks wildcard entries, identical matches at different priorities, missing entries, and invalid keys. The LPM field is 32 bits, so this test can run on PI versions with the 9-bit LPM limitation above.

From the repository root, with a running BMv2 target:

```bash
mkdir -p /tmp/p4runtime-integration/table-keys
p4c --target bmv2 --arch v1model \
  --p4runtime-files /tmp/p4runtime-integration/table-keys/table_keys.p4info.txtpb \
  -o /tmp/p4runtime-integration/table-keys test/integration/testdata/table_keys.p4
go build -o /tmp/p4runtime-integration/p4ctl ./cmd/p4ctl
P4RT_CLI_BIN=/tmp/p4runtime-integration/p4ctl \
P4RT_KEY_P4INFO=/tmp/p4runtime-integration/table-keys/table_keys.p4info.txtpb \
P4RT_KEY_DEVICE_CONFIG=/tmp/p4runtime-integration/table-keys/table_keys.json \
go test -race -tags=integration -count=1 -run '^TestBMv2_TableCLI$' ./test/integration/...
```

Set all three variables to include this test in the full suite. With none set, the CLI test is skipped. The test installs its pipeline and uses plaintext on a target with device ID 1.

## L2 traffic and examples

On Linux, `TestBMv2_L2Dataplane` sends real Ethernet frames through two host-facing interfaces. It checks L2 forwarding, unmatched-frame PacketIn, PacketOut on each port, direct and indirect counters, and the effect of deleting a forwarding entry. `TestBMv2_L2Examples` runs examples 02, 03, and 04, captures their actual traffic, checks the counter output, and stops the Packet I/O example with SIGINT.

Start BMv2 with device ID 1, CPU port 255, and data ports 1 and 2 bound to the switch ends of two veth pairs. Set `P4RT_HOST_IFACE1` and `P4RT_HOST_IFACE2` to their host ends. The tests need permission to open raw Ethernet sockets. They leave interface and target creation to the caller.

`TestBMv2_PacketCLI` runs the actual `p4ctl packet send` command ten times for each port. After every command exits, it captures the complete Ethernet frame on the selected interface. Set `P4RT_CLI_BIN` to the CLI binary.

From the repository root:

```bash
./scripts/compile-l2.sh
mkdir -p /tmp/p4runtime-integration
go build -o /tmp/p4runtime-integration/l2-example ./examples/02_l2_switch
go build -o /tmp/p4runtime-integration/packet-example ./examples/03_packetio
go build -o /tmp/p4runtime-integration/counter-example ./examples/04_counters
go build -o /tmp/p4runtime-integration/p4ctl ./cmd/p4ctl
go test -c -race -tags=integration \
  -o /tmp/p4runtime-integration/integration.test ./test/integration
sudo env P4RT_TARGET=127.0.0.1:9559 \
  P4RT_P4INFO="$PWD/examples/testdata/l2.p4info.txt" \
  P4RT_DEVICE_CONFIG="$PWD/examples/testdata/l2.bmv2.json" \
  P4RT_HOST_IFACE1=host1 P4RT_HOST_IFACE2=host2 \
  P4RT_L2_EXAMPLE_BIN=/tmp/p4runtime-integration/l2-example \
  P4RT_PACKET_EXAMPLE_BIN=/tmp/p4runtime-integration/packet-example \
  P4RT_COUNTER_EXAMPLE_BIN=/tmp/p4runtime-integration/counter-example \
  P4RT_CLI_BIN=/tmp/p4runtime-integration/p4ctl \
  /tmp/p4runtime-integration/integration.test \
  -test.v -test.run '^TestBMv2_(L2(Dataplane|Examples)|PacketCLI)$'
```

Replace `host1` and `host2` with the host interface names. With no host interfaces configured, the Ethernet tests are skipped. Example binary paths are also required for the example test.
