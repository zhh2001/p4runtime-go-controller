# Architecture (detailed)

For the high-level overview see the repository-root [`ARCHITECTURE.md`](../ARCHITECTURE.md). This document captures the implementation-level detail that the top-level file keeps short.

## Layering

```
client                       — public long-lived session
├── pipeline                  — parsed P4Info, by-name indexes
├── tableentry                — fluent builder
├── packetio                  — PacketIn/PacketOut wrapper
├── digest                    — digest subscribe + ack
├── counter / meter / register — typed data-plane read/write
├── metrics                   — pluggable observability surface
├── errors                    — sentinel errors
└── internal/
    ├── codec                 — canonical bytes + codec helpers
    ├── stream                — StreamChannel supervisor
    └── testutil              — bufconn mock P4Runtime server
```

Dependencies flow top-down. `internal/*` never imports anything higher in the stack; `client` imports `internal/stream` and `pipeline` (for `GetPipeline` round-tripping); `tableentry` imports `pipeline` and `internal/codec`; the data-plane packages import `client` and `pipeline`.

## Dial sequence

1. `client.Dial` collects options, validates device ID and election ID.
2. gRPC `NewClient` creates the connection lazily.
3. An internal context is spun up and the stream supervisor is started with a pointer to the `Client.dial` helper and `Client.receiveHandler`.
4. `Dial` busy-waits (25 ms ticks) for the supervisor to transition out of `connecting`. On success it returns; on timeout it calls `Close` and surfaces `ErrArbitrationFailed`.

## Stream supervisor state machine

```
    +-------+   Dialer(ctx) fails    +--------------+
    | disc. | <--------------------- | connecting   |
    +---+---+                        +------+-------+
        |                                   |
        | supervisor.Start                  | stream established + arbitrate
        v                                   v
    +-------+   arb status==AlreadyExists  +---------+
    | conn. | ---------------------------> | backup  |
    +---+---+                              +----+----+
        |                                       |
        | arb status==OK                         | new arbitration w/ OK
        v                                       v
    +----------+                           +---------+
    | primary  |  <------------------------+         |
    +-----+----+                                     |
          |                                          |
          | transport error                          |
          v                                          |
      reconnect (exponential backoff)  ------------->+
```

Reconnect backoff is `initial * 2^k` up to `max`, with ±20 % jitter applied to each sleep so concurrent supervisors de-synchronize.

`BecomePrimary` reads the current state and its change notification under the same lock. A state transition wakes every waiter, which then checks the current state again. The public `Events` queue serves observers separately, so event consumption and buffer limits do not affect primary waits.

## Dispatcher

`Client.receiveHandler` receives every `StreamMessageResponse` from the supervisor. The dispatcher holds four maps keyed by an opaque ID:

- `PacketInHandler`
- `DigestListHandler`
- `IdleTimeoutHandler`
- `StreamMessageHandler`

Registration returns a closure that removes the corresponding subscription. The dispatcher copies the handlers for each message under the subscription lock and releases the lock before invoking any callback. Catch-all handlers run before the matching typed handlers. Ordering within either group is unspecified.

Handlers can register or cancel subscriptions, including their own. Changes apply to later messages. Cancellation returns without waiting for handlers already selected for the current message, which may still run. All handlers run in the stream receive goroutine and should return quickly.

## SetPipeline fallback

The fallback chain mirrors the spec recommendation:

1. `VERIFY_AND_COMMIT` (strict — verify + install)
2. `RECONCILE_AND_COMMIT` (reconcile existing entries)
3. `COMMIT` (non-verifying; last resort)

Triggers that cause a fall-through:

- gRPC `Unimplemented`.
- gRPC `InvalidArgument` with a message containing `not supported` or `unsupported action` (case-insensitive).

All other errors bubble up unchanged so callers can tell the difference between "target doesn't know the action" and "pipeline blob is broken".

## Canonical bytes

P4Runtime's canonical encoding uses the shortest nonempty big-endian byte string for an unsigned integer. Zero is encoded as `00`. Integer encoders normalize nil, empty, and all-zero inputs to that single byte.

The table entry builder handles wildcard omission separately. `Optional(nil)`, an LPM prefix of zero, and an all-zero ternary mask omit the corresponding match field. A zero value with an explicit optional match, a nonzero LPM prefix, or a nonzero ternary mask remains in the entry.

LPM and prefix-style ternary masks count from the field's highest bit, excluding byte padding. For a 9-bit field, a full mask is `01ff`, and an 8-bit prefix mask is `01fe`. Mask helpers validate the value and mask before applying them, so masking cannot hide an input that exceeds the field width. Redundant leading zero bytes are accepted and removed from encoded values and masks.

Range endpoints are normalized before comparison. Leading zero bytes do not affect their order, and the builder emits canonical low and high values.

`internal/codec` provides these helpers:

- `EncodeUint(uint64, bits)` — integer → canonical bytes.
- `EncodeBytes(bytes, bits)` — strips leading zeros, validates bit width.
- `LPMMask(value, prefix, bits)` — applies a prefix-length mask to value and returns the canonical encoding.

## Error taxonomy

Every sentinel in the public `errors` package has a narrow, well-defined meaning. Wrapping with `fmt.Errorf("%w", errs.Err...)` is the preferred pattern; callers use `errors.Is`.

| Sentinel                                         | Meaning                                      |
| ------------------------------------------------ | -------------------------------------------- |
| `ErrNotPrimary`                                  | Operation requires primary mastership.       |
| `ErrPipelineNotSet`                              | Target has no active pipeline.               |
| `ErrEntryExists` / `ErrEntryNotFound`            | Write-path idempotency helpers.              |
| `ErrUnsupportedMatchKind`                        | Match kind not declared on the field.        |
| `ErrTargetUnsupported`                           | Target refused the feature.                  |
| `ErrStreamClosed`                                | Stream was closed.                           |
| `ErrArbitrationFailed`                           | First arbitration timed out or was rejected. |
| `ErrElectionIDZero`                              | Election ID was the reserved zero value.     |
| `ErrInvalidBitWidth`                             | Value too large for the declared bit width.  |
| `ErrInvalidMatchField` / `ErrInvalidActionParam` | Unknown name at build time.                  |
