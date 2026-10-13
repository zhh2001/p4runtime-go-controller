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

## Known BMv2 and PI limits

The following limits apply to the BMv2 1.15.0-2bdd0b7b and PI 5689c91 build used for local validation:

- Some valid 9-bit LPM values are rejected because PI checks trailing zeros against padded byte width. The 32-bit LPM fixture, and 9-bit TERNARY and RANGE fixtures, can be tested separately.
- RegisterEntry RPCs are unsupported in this build. Controlled-server tests cover register encoding and request behavior. Live register tests check the unsupported response.
- Byte-based PRE ports and backup replicas require newer target support. Live replication fixtures use legacy port fields.
- Named read roles can be accepted without enforcing role scopes. Controlled-server tests check that the SDK sends the configured role and preserves target rejection.
- Reconciliation of the bundled L2 pipeline's indirect counters can fail because PI restores counters using INSERT. See [pipeline troubleshooting](troubleshooting.md#setforwardingpipelineconfig-keeps-failing).

See the digest section of the [integration guide](../test/integration/README.md) for the target's acknowledgement lifetime behavior. SDK tests cover named subscriptions, cancellation and ACK validation independently.
