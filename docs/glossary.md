# Glossary

Domain terms used in P4 and P4Runtime. Definitions are deliberately short —
pointers to the canonical spec are at the bottom.

**P4** — A domain-specific language for describing the packet processing
behavior of a forwarding element (ASIC, SmartNIC, software switch).

**P4Runtime** — A gRPC-based control-plane API that a P4 program exposes to
its controller. See the [specification](https://p4.org/p4-spec/p4runtime/main/P4Runtime-Spec.html).

**P4Info** — The Protobuf metadata describing a compiled P4 program: tables,
actions, action parameters, counters, meters, registers, digests, and
controller packet metadata. Produced by `p4c` via `--p4runtime-files`.

**Pipeline** — In this SDK, the pairing of a parsed P4Info and the opaque
device-configuration blob that the target expects (e.g., `bmv2.json` for
BMv2, a binary artifact for Tofino).

**PDPI** — "PD-programmable Device Pipeline Interface", an older term some
vendors use for a similar concept. Not used in this project.

**Table entry** — A single (match, action) row installed into a P4 table.
EXACT entries are keyed by value, LPM entries carry a prefix length,
TERNARY entries carry a mask, RANGE entries carry `[low, high]`, OPTIONAL
entries are don't-care when the value is absent.

**Match kind** — EXACT / LPM / TERNARY / RANGE / OPTIONAL. Defined per
match field in the P4 source.

**Action** — A named function that a matched table entry invokes. May take
parameters; parameter bit widths are declared in P4Info.

**Action profile** — A container for actions shared across table entries,
typically used for ECMP or WCMP load-sharing. Out of scope for the current
SDK version.

**Counter** — A packet/byte counter attached either to a table (direct
counter) or declared independently with an indexed array (indirect
counter). Read through the `counter` package.

**Meter** — A token-bucket rate limiter, analogous to counters. Configurable
CIR/CBurst/PIR/PBurst. Read/write through the `meter` package.

**Register** — A P4-accessible array of storage slots. Useful for storing
flow-scoped state readable by the controller.

**Digest** — A target-initiated notification describing a collection of
learned entries (classic use: MAC learning).

**StreamChannel** — The bidirectional P4Runtime gRPC stream carrying
`MasterArbitrationUpdate`, `PacketIn`, `PacketOut`, `DigestList`, and
`DigestListAck` messages.

**Election ID** — The 128-bit value the controller supplies during
`MasterArbitrationUpdate`. The highest election ID wins the primary role.

**Primary / Backup controller** — Only the primary may write, push
pipelines, or send packet-outs. Backups may read.

**Mastership** — Shorthand for the primary/backup state machine.

**Canonical bytes** — P4Runtime 1.3.0 convention requiring byte-string
integer values to be stored with no leading zero bytes. The zero value is
encoded as an empty byte slice.
