# Design Notes

This document is a living record of design decisions (ADR-style), the rationale
behind each decision, and open questions that are deliberately deferred.

The audience is future maintainers and contributors. Every non-trivial trade-off
that cannot be inferred from reading the code should be captured here.

## Key Abstractions

| Type | Package | Responsibility |
| --- | --- | --- |
| `Client` | `client` | Long-lived session to a single P4Runtime target. Owns the gRPC connection, mastership state, and stream supervisor. Safe for concurrent use. |
| `Pipeline` | `pipeline` | Parsed P4Info plus (optionally) the opaque device-config blob. Exposes by-name lookup for tables, actions, counters, meters, registers, digests, and controller packet metadata. |
| `TableEntry` | `tableentry` | Fluent builder that produces `p4.TableEntry` protos. Enforces canonical bytes and bit-width correctness at construction time. |
| `Stream` | `internal/stream` | Supervisor goroutine for the bidirectional `StreamChannel`. State machine: `disconnected → connecting → connected → primary | backup`. Emits events on a channel. |
| `Codec` | `internal/codec` | Canonical byte encoding/decoding for integers, MAC, IPv4, IPv6, arbitrary bitstrings, LPM prefixes, and ternary masks. |
| `Metrics` | `metrics` | Plug-in interface. No-op default. Prometheus and OpenTelemetry adapters live in sub-packages so core has zero hard dependency on them. |

## Architectural Decisions

### ADR-001 — Module path

Status: accepted.

The module path is `github.com/zhh2001/p4runtime-go-controller`. Staying on
`v1.x.y` keeps this path; a future `v2` will require the `/v2` suffix per the
Go module rules.

### ADR-002 — Canonical bytes at encode time, not at write time

Status: accepted.

P4Runtime 1.3.0 mandates canonical byte encoding for integer-typed match fields
(no leading zeros). We normalize at `tableentry.Build()` rather than inside the
write path so the produced proto is already wire-legal and can be inspected or
logged without surprise.

### ADR-003 — Pluggable metrics and tracing, not a hard dependency

Status: accepted.

`metrics.Metrics` is an interface with a no-op default. Prometheus lives in
`metrics/prometheus/`. OpenTelemetry tracing is wired via user-supplied gRPC
interceptors. This keeps the core dependency set to proto/grpc/stdlib.

### ADR-004 — Stream supervisor owns reconnect, not the caller

Status: accepted.

Reconnect with exponential backoff is implemented inside `internal/stream`. On
reconnect the supervisor re-sends the arbitration update and waits for the
primary-election result before emitting the `primary`/`backup` event. Callers
observe events, not transport failures.

### ADR-005 — `SetForwardingPipelineConfig` action fallback

Status: accepted.

We attempt `VERIFY_AND_COMMIT` first. If the target returns `UNIMPLEMENTED` or
`INVALID_ARGUMENT` with a message indicating the action is unsupported, we fall
back to `RECONCILE_AND_COMMIT`, then `COMMIT`. Each attempt is logged at INFO.
The chosen action is exposed on the returned result so callers can record it.

### ADR-006 — Election ID is an immutable struct, not two uint64s

Status: accepted.

```go
type ElectionID struct { High, Low uint64 }
```

All comparison helpers (`Less`, `Equal`, `Cmp`) treat the pair as a 128-bit
unsigned integer. `Increment` saturates at `max` (returns `false` instead of
wrapping) to avoid collisions that a buggy caller could otherwise produce.

### ADR-007 — No global state in the library core

Status: accepted.

No package-level mutable variables, no `init()` side effects beyond proto
registration (which we do not trigger — that belongs to the proto stub
packages). This matches the stdlib and kubernetes style and is necessary for
concurrent multi-target controllers.

### ADR-008 — Context is mandatory on every blocking call

Status: accepted.

Every exported function that does I/O takes `ctx context.Context` as its first
parameter. The stream supervisor also honors `ctx` — `Close()` calls
`cancel()` and the supervisor tears down within a bounded deadline.

## Filter-Blocked Items to Revisit

- `CONTRIBUTING.md` — the first drafted version was rejected by a content
  filter during authoring. Resolved with a shorter, more neutral rewrite on
  2026-04-20.

## Open Questions

These are decisions we have deliberately deferred. Picking a reasonable default
and documenting the question is OK; getting stuck is not.

- **Role support**: left as an explicit TODO. `Client.WithRole(...)` is the
  planned extension point. No timeline.
- **Multi-controller leader election**: out of scope. Users can layer etcd /
  Consul on top. We may offer a reference integration later.
- **`WriteBatch` atomicity guarantees**: `atomicity=DATAPLANE_ATOMIC` is not
  universally supported. We expose the knob and document per-target behavior
  rather than pretend the abstraction is uniform.
- **Packet-in backpressure**: the initial implementation uses an unbounded
  delivery channel scoped by a user-supplied buffer size. We may later offer a
  blocking-drop strategy — tracked as a future design iteration.
