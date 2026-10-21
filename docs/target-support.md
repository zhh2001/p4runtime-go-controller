# Target support

## Protocol baseline

v2 uses the official P4Runtime protobuf definitions and a P4Runtime 1.3.0 protocol baseline. A target must implement the resources and RPC behavior an application uses.

| Feature                  | Protocol requirement                                                    |
| ------------------------ | ----------------------------------------------------------------------- |
| Legacy PRE `egress_port` | Included in the 1.3.0 baseline.                                         |
| PRE byte-based `port`    | P4Runtime 1.4 or newer, with target support.                            |
| Role-filtered Read       | P4Runtime 1.4 or newer. The target defines and enforces the role scope. |
| PRE backup replicas      | P4Runtime 1.5 or newer, with target support.                            |

The SDK does not negotiate these optional capabilities. Select the representation and operation supported by the target. The [PRE guide](../pre/README.md) describes port and backup fields.

## Validation scope

SDK tests use controlled gRPC servers to check request fields, validation, returned values and stream behavior. BMv2 integration fixtures cover arbitration, pipeline operations, table matches, Packet I/O, counters, meters and legacy packet replication. Linux Ethernet tests check forwarding, counter increments, meter colors and replicated frames.

The [integration guide](../test/integration/README.md) lists the fixtures, required settings and individual tests. Optional suites require those settings. A skipped suite does not establish target support.

Stratum, Tofino-based hardware and other ASIC targets have not been tested in this repository.

## Input comparison data

The [input-validation archive](https://github.com/zhh2001/p4runtime-controller-evaluation/blob/main/docs/inputs.md) records 6,984 trials across 382 fixed inputs, six profiles and two repetitions, with controlled-service and BMv2 results kept separately. The corpora cover five table-match types, numerical widths from 1 to 129 bits, resource indices, typed register data, default entries, action scope, indirect references and sparse IDs. Typed registers and translated-index mappings use the controlled service.

The archive includes selected requests, complete saved target snapshots, P4Info fixtures and offline verification scripts. Verification checks input coverage, request association, repeated classifications and semantic values. Results distinguish local errors, target errors and inputs outside the selected high-level API.

The [live runner guide](https://github.com/zhh2001/p4runtime-controller-evaluation/blob/main/docs/running-inputs.md) provides pinned client setup, P4 compilation and smoke, full and Go race modes. Setup checks generated inputs and declarations against the archive. A full run executes 6,984 new trials and verifies recorded requests, classifications and target state. These functional tests use loopback services and owned BMv2 processes without Mininet or root privileges.

## Known BMv2 and PI limits

The following limits apply to the BMv2 1.15.0-2bdd0b7b and PI 5689c91 build used for local validation:

- Some valid 9-bit LPM values are rejected because PI checks trailing zeros against padded byte width. The 32-bit LPM fixture, and 9-bit TERNARY and RANGE fixtures, can be tested separately.
- RegisterEntry RPCs are unsupported in this build. Controlled-server tests cover register encoding and request behavior. Live register tests check the unsupported response.
- Byte-based PRE ports and backup replicas require newer target support. Live replication fixtures use legacy port fields.
- Named read roles can be accepted without enforcing role scopes. Controlled-server tests check that the SDK sends the configured role and preserves target rejection.
- Reconciliation of the bundled L2 pipeline's indirect counters can fail because PI restores counters using INSERT. See [pipeline troubleshooting](troubleshooting.md#setforwardingpipelineconfig-keeps-failing).
- Read can return an explicit full-range RANGE for an omitted wildcard match. The archived input verifier compares its wildcard semantics.
- An EXACT MODIFY with `priority=-1`, or a default MODIFY with `idle_timeout_ns=1`, can be accepted. The recorded readbacks set priority or TTL to zero. The [table-constraint results](https://github.com/zhh2001/p4runtime-controller-evaluation/blob/main/docs/inputs.md#table-entry-constraints) retain the requests, state changes and per-item status differences.

See the digest section of the [integration guide](../test/integration/README.md) for the target's acknowledgement lifetime behavior. SDK tests cover named subscriptions, cancellation and ACK validation independently.
