# Architecture

This document describes the layered design of `p4runtime-go-controller`, the
main data flows, and the key sequence diagrams. See
[`DESIGN_NOTES.md`](DESIGN_NOTES.md) for the rationale behind each choice.

## Layered View

```mermaid
flowchart TD
    subgraph User[Controller process]
        app[Application logic]
        app --> api[Public API: client / pipeline / tableentry / packetio / digest / counter / meter / register]
    end

    api --> obs[Observability: metrics / tracing / log/slog]
    api --> codec[internal/codec: canonical bytes]
    api --> stream[internal/stream: StreamChannel supervisor]

    stream -->|bidi gRPC| target[P4Runtime target]
    api -->|unary gRPC| target
```

The layering is strict: package `client` imports `internal/stream`, never the
reverse. The public `errors` package is the only one that is safe to import
from every other package.

## Session Lifecycle

```mermaid
sequenceDiagram
    autonumber
    participant App as Application
    participant C as client.Client
    participant S as internal/stream.Supervisor
    participant T as P4Runtime target

    App->>C: Dial(ctx, addr, opts)
    C->>T: gRPC Dial (TLS + keepalive)
    C->>S: Start(ctx, electionID)
    S->>T: StreamChannel() -> MasterArbitrationUpdate
    T-->>S: MasterArbitrationUpdate(primary=true/false)
    S-->>C: event(primary|backup)
    App->>C: BecomePrimary(ctx)
    C-->>App: nil | ErrNotPrimary

    Note over S,T: Reconnect path
    T--xS: stream disconnected
    S->>T: Dial + re-arbitrate after backoff
```

## Pipeline Push

```mermaid
sequenceDiagram
    autonumber
    participant App as Application
    participant C as client.Client
    participant P as pipeline.Pipeline
    participant T as P4Runtime target

    App->>P: Load(p4infoBytes, deviceConfigBytes)
    App->>C: SetPipeline(ctx, P)
    C->>T: SetForwardingPipelineConfig(VERIFY_AND_COMMIT)
    alt target supports VERIFY_AND_COMMIT
        T-->>C: OK
    else fallback
        T-->>C: UNIMPLEMENTED | INVALID_ARGUMENT
        C->>T: SetForwardingPipelineConfig(RECONCILE_AND_COMMIT)
        T-->>C: OK | fallback to COMMIT
    end
    C-->>App: PipelineResult{action, info}
```

## Table Write

```mermaid
sequenceDiagram
    autonumber
    participant App as Application
    participant B as tableentry.Builder
    participant E as TableEntry (proto)
    participant C as client.Client
    participant T as P4Runtime target

    App->>B: NewEntry("ingress.t_l2").Match(...).Action(...).Build()
    B->>E: validated proto
    App->>C: Write(ctx, INSERT, E)
    C->>T: WriteRequest{election_id, updates=[...]}
    T-->>C: WriteResponse | error (with per-update status)
    C-->>App: nil | ErrEntryExists | ErrNotPrimary
```

## Packet-In Round Trip

```mermaid
sequenceDiagram
    autonumber
    participant T as P4Runtime target
    participant S as internal/stream.Supervisor
    participant P as packetio.Subscriber
    participant App as Application

    T-->>S: StreamMessageResponse{packet}
    S->>P: deliver(packetIn)
    P->>P: decode metadata via P4Info
    P-->>App: OnPacket(PacketIn)
```

## Concurrency Model

- `client.Client` is safe for concurrent use. The gRPC stub is already
  goroutine-safe. State that is not (mastership flag, last-known election ID,
  stream handle) lives behind a `sync.RWMutex` with the narrowest possible
  critical section.
- `internal/stream.Supervisor` owns exactly one goroutine plus a receive
  goroutine per connected stream. Both terminate within a bounded deadline when
  the caller-provided context is cancelled.
- No `time.Sleep` without a select on `ctx.Done()`. No unbuffered goroutine
  leaks under `-race`.

## Error Classification

Sentinel errors in the public `errors` package let callers react
programmatically:

- `ErrNotPrimary` — operation requires primary mastership.
- `ErrPipelineNotSet` — target has no active pipeline yet.
- `ErrEntryExists` / `ErrEntryNotFound` — write-path idempotency helpers.
- `ErrUnsupportedMatchKind` — match kind not supported by the target pipeline.
- `ErrTargetUnsupported` — target does not support the attempted feature (e.g.,
  `VERIFY_AND_COMMIT`).
- `ErrStreamClosed` — stream was closed by the target or by `Client.Close`.

The concrete error type wraps the gRPC status so callers can still use
`status.FromError`.
