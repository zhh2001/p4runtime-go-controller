# BMv2 integration tests

Use a BMv2 P4Runtime target with device ID 1. Tests install their pipeline on the target selected by `P4RT_TARGET`, which defaults to `127.0.0.1:9559`.

From the repository root, start the target in one terminal:

```bash
./scripts/run-bmv2.sh
```

In another terminal, run `make e2e` or `./scripts/test-bmv2.sh`. The test script compiles a fresh L2 pair into `build/integration/l2`, exports both pipeline paths and runs with race detection. L2 pipeline, arbitration and CPU port tests run without extra settings. The CPU test sends a frame back through port 255 and verifies the PacketIn payload and metadata, so it needs no network interfaces.

For custom ports, start with `./scripts/run-bmv2.sh -p 10559 -t 10090` and use `P4RT_TARGET=127.0.0.1:10559 make e2e`. To reuse L2 artifacts, set `P4RT_P4INFO` and `P4RT_DEVICE_CONFIG` together. These tests expect the bundled L2 program. With only one path set, the test script returns an error. Extra Go test flags can be passed directly, for example `./scripts/test-bmv2.sh -v -run '^TestBMv2_PacketCPUPort$'`.

Enable masks, CLI, raw Ethernet, replication, examples and the fresh-target test with the settings below.

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

`TestBMv2_ReadRole` installs an L2 entry with a primary client, then reads it through clients configured with the default role and a named role. It observes the actual gRPC request to check the role on a wildcard read. Run it with `./scripts/test-bmv2.sh -count=3 -v -run '^TestBMv2_ReadRole$'`. Some PI versions accept named roles but do not enforce role scopes. Passing this test verifies request transmission and reading, while SDK tests against a controlled server verify filtered results and rejection without retrying as the default role.

`TestBMv2_PipelineActions` saves the config from `testdata/masks.p4`, writes a TERNARY entry, then commits with no config. It reads back the saved config and entry. It also checks that `VERIFY` and `RECONCILE_AND_COMMIT` retain the entry, while `VERIFY_AND_COMMIT` clears it. This test uses `P4RT_MASK_P4INFO` and `P4RT_MASK_DEVICE_CONFIG`, which must come from the same compilation.

To check queries and writes before pipeline installation, start a fresh target and run these tests separately, before the rest of the suite:

```bash
P4RT_TEST_FRESH_TARGET=1 go test -race -tags=integration -count=1 \
  -run '^TestBMv2_(WriteWithoutPipeline|GetPipelineWithoutPipeline)$' ./test/integration/...
```

Do not set `P4RT_TEST_FRESH_TARGET` when running the full suite. Other tests install a pipeline.

`TestBMv2_GetPipelineWithoutPipeline` compares the raw target error with the SDK result. It checks `ErrPipelineNotSet`, a nil pipeline and preservation of the complete gRPC status.

On Linux, `TestBMv2_ConnectExample` runs the actual connection example and checks clean shutdown on SIGINT. With `P4RT_TEST_FRESH_TARGET=1`, it checks the no-pipeline message without installing a config. Otherwise it installs the bundled L2 config and checks the printed table and action counts. Build the binary from the repository root:

```bash
mkdir -p /tmp/p4runtime-integration
go build -o /tmp/p4runtime-integration/connect-example ./examples/01_connect
P4RT_CONNECT_EXAMPLE_BIN=/tmp/p4runtime-integration/connect-example \
./scripts/test-bmv2.sh -count=3 -v -run '^TestBMv2_ConnectExample$'
```

For the fresh-target case, add `P4RT_TEST_FRESH_TARGET=1` and run before any pipeline is installed. The example test is skipped when its binary path is unset. Without the fresh-target flag, both L2 pipeline paths are required and the test script supplies them when unset.

Some PI versions check LPM trailing zeros against the padded byte width. They reject these 9-bit LPM cases with `Invalid LPM value, incorrect number of trailing zeros`. These cases require PI to use the field width for that check. The TERNARY cases can be run separately with `-run '^TestBMv2_FieldWidthMasks$/ternary'`.

The RANGE cases can be run separately with `-run '^TestBMv2_FieldWidthMasks$/range'`.

## CLI configuration and output

`TestBMv2_CLIConfig` runs the actual CLI with settings from a file, environment variables and flags. It checks their priority, the legacy settings flag, default text, JSON and YAML output, and `--config-file` alongside the device `--config` during pipeline installation. It reads back P4Info, a forwarding entry and an indirect counter through both formats.

Build the CLI and run it against the bundled L2 target:

```bash
mkdir -p /tmp/p4runtime-integration
go build -o /tmp/p4runtime-integration/p4ctl ./cmd/p4ctl
P4RT_CLI_BIN=/tmp/p4runtime-integration/p4ctl \
./scripts/test-bmv2.sh -count=3 -v -run '^TestBMv2_CLIConfig$'
```

This test uses `P4RT_CLI_BIN`, `P4RT_P4INFO` and `P4RT_DEVICE_CONFIG`. The script supplies the pipeline pair when both paths are unset. The target must use device ID 1 and plaintext. The test installs its pipeline. TLS configuration, empty result arrays and full-width integer values are covered by the CLI's subprocess tests against a controlled server.

## CLI table operations

`TestBMv2_TableCLI` runs the actual CLI against EXACT, LPM, TERNARY, RANGE, OPTIONAL, and mixed-match tables from `testdata/table_keys.p4`. It inserts two entries, modifies one, deletes it without an action, and reads back the other entry. It also checks wildcard entries, an explicit full-domain RANGE, identical matches at different priorities, missing entries, and invalid keys. The LPM field is 32 bits, so this test can run on PI versions with the 9-bit LPM limitation above.

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

`TestBMv2_TableValidation` uses the same P4Info and config pair, without needing a CLI binary. The program includes action scopes, a table with idle timeout support, a constant default action, constant entries and an action profile. The test writes permitted actions, reads back default changes and checks the builder's rejection of forbidden writes. It receives an idle notification with the requested TTL and key, confirms the entry remains until explicitly deleted and verifies metadata through Read.

Run it with the compiled pair above:

```bash
P4RT_KEY_P4INFO=/tmp/p4runtime-integration/table-keys/table_keys.p4info.txtpb \
P4RT_KEY_DEVICE_CONFIG=/tmp/p4runtime-integration/table-keys/table_keys.json \
go test -race -tags=integration -count=3 \
  -run '^TestBMv2_TableValidation$' ./test/integration/...
```

Some PI versions omit `metadata` from idle notifications while returning it correctly through Read. The live test logs that omission and checks metadata when the notification contains it. A separate SDK test checks that a complete notification reaches the handler unchanged.

## CLI value parsing

`TestBMv2_TableValues` uses `testdata/table_values.p4`, with 128-bit keys and 129-bit action parameters. It runs the actual CLI through insert, read, modify and delete for EXACT, LPM, TERNARY, RANGE and OPTIONAL. It checks decimal values above 64 bits, IPv4, IPv6, mapped IPv6, MAC bytes, explicit hex and the ambiguity between IPv6 and eight colon-separated bytes. Invalid input must return an ordinary error and leave the table empty.

Compile the pipeline pair and CLI from the repository root:

```bash
mkdir -p /tmp/p4runtime-integration/table-values
p4c-bm2-ss --arch v1model \
  --p4runtime-files /tmp/p4runtime-integration/table-values/table_values.p4info.txtpb \
  -o /tmp/p4runtime-integration/table-values/table_values.json \
  test/integration/testdata/table_values.p4
go build -o /tmp/p4runtime-integration/p4ctl ./cmd/p4ctl
P4RT_CLI_BIN=/tmp/p4runtime-integration/p4ctl \
P4RT_VALUE_P4INFO=/tmp/p4runtime-integration/table-values/table_values.p4info.txtpb \
P4RT_VALUE_DEVICE_CONFIG=/tmp/p4runtime-integration/table-values/table_values.json \
go test -race -tags=integration -count=3 -v \
  -run '^TestBMv2_TableValues$' ./test/integration/...
```

Both pipeline paths and the CLI binary are required. With neither pipeline path set, the test is skipped. It installs its pipeline on the selected plaintext target with device ID 1. The 128-bit LPM field is byte-aligned and does not use the 9-bit LPM cases described above.

## Resource indexes

`TestBMv2_ResourceIndexes` uses four-entry counter, meter and register arrays from `testdata/resources.p4`. It checks that invalid indexes return locally without an RPC, writes and reads indexes 0 and 3, and verifies that `-1` reads every entry. Compile the matching pipeline pair from the repository root:

```bash
mkdir -p /tmp/p4runtime-integration/resources
p4c-bm2-ss --arch v1model \
  --p4runtime-files /tmp/p4runtime-integration/resources/resources.p4info.txtpb \
  -o /tmp/p4runtime-integration/resources/resources.json \
  test/integration/testdata/resources.p4
P4RT_RESOURCE_P4INFO=/tmp/p4runtime-integration/resources/resources.p4info.txtpb \
P4RT_RESOURCE_DEVICE_CONFIG=/tmp/p4runtime-integration/resources/resources.json \
go test -race -tags=integration -count=3 -v \
  -run '^TestBMv2_(ResourceIndexes|RegisterValues)$' ./test/integration/...
```

Set both pipeline paths to run this test. It installs its pipeline on the selected target. Some PI versions do not implement RegisterEntry RPCs. In that case, the test checks the target's unsupported response and skips valid register reads and writes. Invalid register indexes still undergo local validation. SDK tests against a controlled server cover valid register requests and preservation of named indexes above the physical array size. This live test does not establish support for index translation.

`TestBMv2_RegisterValues` uses the same pipeline pair. It checks local rejection of overflow and mismatched P4Data types, then inspects canonical zero, padded integer and maximum-value requests sent to the target. Targets with RegisterEntry support also undergo value readback. On targets without that support, the test verifies unsupported responses and reports that readback was unavailable. Controlled gRPC tests cover the other P4Data types and copying of caller values. See [Register arrays](../../register/README.md) for the write API.

## Counter values and units

`TestBMv2_CounterValues` writes and reads three four-entry arrays declared as PACKETS, BYTES and BOTH. It checks zero, ordinary counts, values above uint32 and both int64 boundaries. It inspects the Write RPC to verify that both fields and explicit zero data reach the target. Index 3 receives no data plane traffic. `TestBMv2_CounterCLI` checks signed decimal output and lossless JSON/YAML strings through the actual CLI.

`TestBMv2_CounterDataplane` sends three Ethernet frames from port 1 to port 2 and checks packet and byte increments for each declared unit. It then writes zeros and verifies that counting resumes from zero. Compile the matching fixture and CLI from the repository root:

```bash
mkdir -p /tmp/p4runtime-integration/counters
p4c-bm2-ss --arch v1model \
  --p4runtime-files /tmp/p4runtime-integration/counters/counters.p4info.txtpb \
  -o /tmp/p4runtime-integration/counters/counters.json \
  test/integration/testdata/counters.p4
go build -o /tmp/p4runtime-integration/p4ctl ./cmd/p4ctl
P4RT_CLI_BIN=/tmp/p4runtime-integration/p4ctl \
P4RT_COUNTER_P4INFO=/tmp/p4runtime-integration/counters/counters.p4info.txtpb \
P4RT_COUNTER_DEVICE_CONFIG=/tmp/p4runtime-integration/counters/counters.json \
go test -race -tags=integration -count=3 -v \
  -run '^TestBMv2_Counter(Values|CLI|Dataplane)$' ./test/integration/...
```

Set both pipeline paths to enable the tests. CLI checks also require `P4RT_CLI_BIN`. The Linux data plane test requires `P4RT_HOST_IFACE1`, `P4RT_HOST_IFACE2` and `CAP_NET_RAW`. Each test installs its pipeline on a dedicated target with device ID 1. BMv2's PI implementation preserves unsigned 64-bit counter bit patterns in the int64 wire fields. The negative-value cases check that target behavior and do not establish support on other implementations. Controlled gRPC tests check SDK value preservation independently of BMv2.

## Meter configuration

`TestBMv2_MeterConfigs` checks local rejection of negative values and invalid field relationships, then writes and reads configurations at indexes 0 and 3. It distinguishes an explicit zero Config from Reset and verifies that the target receives burst values above uint32 for its own validation.

`TestBMv2_MeterDataplane` runs on Linux with two host interfaces bound to switch ports 1 and 2. The fixture writes the meter color into the last byte of the source MAC and forwards the frame from port 1 to port 2. Zero rates prevent token refill, so finite bursts produce a deterministic GREEN, YELLOW, RED sequence. Explicit zeros produce RED, and Reset restores GREEN. Compile the fixture and run on a dedicated target:

```bash
mkdir -p /tmp/p4runtime-integration/meters
p4c-bm2-ss --arch v1model \
  --p4runtime-files /tmp/p4runtime-integration/meters/meters.p4info.txtpb \
  -o /tmp/p4runtime-integration/meters/meters.json \
  test/integration/testdata/meters.p4
P4RT_METER_P4INFO=/tmp/p4runtime-integration/meters/meters.p4info.txtpb \
P4RT_METER_DEVICE_CONFIG=/tmp/p4runtime-integration/meters/meters.json \
go test -race -tags=integration -count=3 -v \
  -run '^TestBMv2_Meter(Configs|Dataplane)$' ./test/integration/...
```

Set `P4RT_HOST_IFACE1` and `P4RT_HOST_IFACE2` and grant `CAP_NET_RAW` for the data plane test. Both tests install their pipeline. The V1Model fixture exercises two-rate three-color behavior. Controlled gRPC tests cover the single-rate field constraints and EBurst representation. This live fixture does not establish single-rate target support.

## Packet replication

`TestBMv2_PRE` uses the bundled L2 pipeline to insert, read, modify and delete multicast groups and clone sessions. It checks legacy ports, byte ports with leading zeros and mixed replica sets. It also modifies entries directly from their read results. Run it on a target with device ID 1:

```bash
./scripts/test-bmv2.sh -count=3 -v -run '^TestBMv2_PRE$'
```

On Linux, `TestBMv2_PREDataplane` uses `testdata/replication.p4` and two veth ports. It captures multicast and cloned frames, checks each replica's instance, changes the output port through Modify and verifies that Delete stops replication. The program places the instance in the last byte of the source MAC. It uses multicast group 19 and clone session 319.

Compile the pipeline pair from the repository root:

```bash
mkdir -p /tmp/p4runtime-integration/replication
p4c-bm2-ss --arch v1model \
  --p4runtime-files /tmp/p4runtime-integration/replication/replication.p4info.txtpb \
  -o /tmp/p4runtime-integration/replication/replication.json \
  test/integration/testdata/replication.p4
go test -c -race -tags=integration \
  -o /tmp/p4runtime-integration/integration.test ./test/integration
sudo env P4RT_TARGET=127.0.0.1:9559 \
  P4RT_PRE_P4INFO=/tmp/p4runtime-integration/replication/replication.p4info.txtpb \
  P4RT_PRE_DEVICE_CONFIG=/tmp/p4runtime-integration/replication/replication.json \
  P4RT_HOST_IFACE1=host1 P4RT_HOST_IFACE2=host2 \
  /tmp/p4runtime-integration/integration.test \
  -test.v -test.count=3 -test.run '^TestBMv2_PREDataplane$'
```

Bind target ports 1 and 2 to the switch ends of two veth pairs, then replace `host1` and `host2` with their host ends. The binary needs raw socket permission. With neither PRE pipeline path set, the data plane test is skipped. Setting only one path fails. Each test installs its pipeline.

The BMv2 tests use numeric ports and untruncated clones. Some BMv2/PI versions reject nonzero clone truncation lengths and do not support backup failover. SDK tests against a controlled server cover wide and string ports, backup order, truncation lengths and complete read-modify-write preservation. These protocol checks do not establish target support for those features.

## Digest subscriptions

On Linux, `TestBMv2_DigestSubscriptions` uses `testdata/digests.p4` and real Ethernet input. It checks named subscriptions, explicit subscription to all digests, invalid names, decoded test data, duplicate suppression before ACK and delivery after ACK. It also cancels a named subscription before acknowledging its received batch and checks that the canceled callback receives no later batches.

Compile the digest program from the repository root:

```bash
mkdir -p /tmp/p4runtime-integration/digests
p4c-bm2-ss --arch v1model \
  --p4runtime-files /tmp/p4runtime-integration/digests/digests.p4info.txtpb \
  -o /tmp/p4runtime-integration/digests/digests.json \
  test/integration/testdata/digests.p4
```

Use a target with data ports 1 and 2 bound to two veth pairs, as described below. Set `P4RT_DIGEST_P4INFO` and `P4RT_DIGEST_DEVICE_CONFIG` to the compiled paths, and `P4RT_HOST_IFACE1` and `P4RT_HOST_IFACE2` to the host ends. Run the integration test binary with raw socket permission:

```bash
go test -c -race -tags=integration \
  -o /tmp/p4runtime-integration/integration.test ./test/integration
sudo env P4RT_TARGET=127.0.0.1:9559 \
  P4RT_DIGEST_P4INFO=/tmp/p4runtime-integration/digests/digests.p4info.txtpb \
  P4RT_DIGEST_DEVICE_CONFIG=/tmp/p4runtime-integration/digests/digests.json \
  P4RT_HOST_IFACE1=host1 P4RT_HOST_IFACE2=host2 \
  /tmp/p4runtime-integration/integration.test \
  -test.v -test.count=3 -test.run '^TestBMv2_DigestSubscriptions$'
```

With neither digest pipeline path set, the test is skipped. With one path missing, it fails. Both host interface variables and raw socket permission are required to run the test. The test installs its digest pipeline on the selected target.

The test observes redelivery after each ACK before sending the next ACK. Some PI versions store an asynchronous ACK task's request by reference, allowing a later stream request to overwrite it. SDK tests verify consecutive ACKs against a server that records each request. The live test does not establish that an affected PI version handles consecutive ACKs correctly.

## L2 traffic and examples

On Linux, `TestBMv2_L2Dataplane` sends real Ethernet frames through two host-facing interfaces. It checks L2 forwarding, unmatched-frame PacketIn, PacketOut on each port, direct and indirect counters, and the effect of deleting a forwarding entry. `TestBMv2_L2Examples` runs examples 02, 03, and 04, captures their actual traffic, checks the counter output, and stops the Packet I/O example with SIGINT.

`TestBMv2_PacketOutMetadata` checks that incomplete, unknown and out-of-range metadata returns an error without emitting a frame. Complete packets with leading zero bytes and explicit nil or zero padding must reach their selected port with the original payload. `TestBMv2_PacketCLI` sends twenty frames across both ports and checks that an out-of-range port returns an ordinary error without emitting a frame.

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
  -test.v -test.run '^TestBMv2_(L2(Dataplane|Examples)|Packet(CLI|OutMetadata))$'
```

Replace `host1` and `host2` with the host interface names. With no host interfaces configured, the Ethernet tests are skipped. Example binary paths are also required for the example test.
