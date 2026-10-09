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
├── metrics                   — reserved for future metrics support
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

Failed stream creation and arbitration use backoff of `initial * 2^k` up to `max`, with ±20 % jitter applied to each sleep. An established stream that fails starts the next attempt immediately. Successful arbitration resets the backoff.

The supervisor logs each StreamChannel opening attempt and mastership change at INFO, failures at WARN, and the actual retry delay and normal shutdown at DEBUG. These attempts are separate from gRPC's internal transport connection retries. See [Observability](observability.md).

`BecomePrimary` reads the current state and its change notification under the same lock. A state transition wakes every waiter, which then checks the current state again. The public `Events` queue serves observers separately, so event consumption and buffer limits do not affect primary waits.

## Dispatcher

`Client.receiveHandler` receives every `StreamMessageResponse` from the supervisor. The dispatcher holds four maps keyed by an opaque ID:

- `PacketInHandler`
- `DigestListHandler`
- `IdleTimeoutHandler`
- `StreamMessageHandler`

Registration returns a closure that removes the corresponding subscription. The dispatcher copies the handlers for each message under the subscription lock and releases the lock before invoking any callback. Catch-all handlers run before the matching typed handlers. Ordering within either group is unspecified.

Handlers can register or cancel subscriptions, including their own. Changes apply to later messages. Cancellation returns without waiting for handlers already selected for the current message, which may still run. All handlers run in the stream receive goroutine and should return quickly.

## Digest subscriptions

`digest.Subscriber.Subscribe(name, handler)` resolves a P4Info name or alias and returns a cancellation function and an error. Unknown names, zero IDs and nil handlers return an error before registration. An empty name explicitly subscribes to all non-nil digest lists, including IDs absent from P4Info. Callbacks receive the raw `DigestList` without decoding its `P4Data`.

`OnDigest` keeps its cancellation-only signature. Invalid subscriptions register nothing and return a no-op cancellation function. Use `Subscribe` when the caller needs to report registration errors. Both APIs follow the dispatcher's cancellation semantics above.

`Ack` accepts only nonzero digest IDs declared in the subscriber's P4Info. It copies the batch's `digest_id` and `list_id` without changing the batch. It does not track received batches or require a live subscription, so processing can finish and acknowledge a batch after cancellation. `Client.SendDigestAck` remains available for raw acknowledgements. Sending an ACK waits for gRPC transmission and does not confirm target processing.

These identifiers follow [P4Runtime's digest acknowledgement rules](https://p4lang.github.io/p4runtime/spec/v1.3.0/P4Runtime-Spec.html#sec-digestentry).

## SetPipeline fallback

The default action is `VERIFY_AND_COMMIT`. When unsupported, it can fall back once to `RECONCILE_AND_COMMIT`. `NoFallback: true` disables this even when `Action` is omitted.

Fallback requires gRPC `Unimplemented` or an `InvalidArgument` message explicitly identifying an unsupported RPC action. Errors about parser features, action parameters, or preserving forwarding state stop the call. Messages outside the recognized forms also stop the call. Unsupported actions match `ErrTargetUnsupported` while preserving the underlying gRPC error.

Explicit `VERIFY`, `VERIFY_AND_SAVE`, `COMMIT`, and `RECONCILE_AND_COMMIT` run only the requested action. A failed verification or save never installs a config. A failed reconciliation never falls back to an action that clears entries.

`COMMIT` requires a nil pipeline and sends no `Config`. It commits the config already saved on the target by `VERIFY_AND_SAVE`. Other actions require a non-nil pipeline and send its P4Info and device config. Unknown action values are rejected before sending an RPC.

## Canonical bytes

P4Runtime's canonical encoding uses the shortest nonempty big-endian byte string for an unsigned integer. Zero is encoded as `00`. Integer encoders normalize nil, empty, and all-zero inputs to that single byte.

The table entry builder handles wildcard omission separately. `Optional(nil)`, an LPM prefix of zero, an all-zero ternary mask and a RANGE covering the field's entire domain omit the corresponding match field. A zero value with an explicit optional match, a nonzero LPM prefix or a nonzero ternary mask remains in the entry.

LPM and prefix-style ternary masks count from the field's highest bit, excluding byte padding. For a 9-bit field, a full mask is `01ff`, and an 8-bit prefix mask is `01fe`. Mask helpers validate the value and mask before applying them, so masking cannot hide an input that exceeds the field width. Redundant leading zero bytes are accepted and removed from encoded values and masks.

Range endpoints are normalized before comparison. Leading zero bytes do not affect their order, and the builder emits canonical low and high values. A range from zero through `2^bitwidth - 1` is omitted, including for fields whose width is not a multiple of eight. A narrower range, including `0..0`, remains a concrete match.

`Builder.BuildKey()` constructs the table ID, match fields, priority, and default-action flag without an action, metadata, or idle timeout. Use it to identify an ordinary entry for deletion. `Build()` uses the same key validation and also requires an action. Every EXACT field is required. Tables with TERNARY, RANGE, or OPTIONAL fields require a positive priority, even when those fields are wildcarded. Other tables require zero priority. `AsDefault()` omits matches and priority from both forms. Default entries cannot be deleted.

`Build()` resolves action names and aliases to their P4Info IDs, checks the table's `action_refs` and enforces `TABLE_ONLY` and `DEFAULT_ONLY` scope. It rejects action writes on constant table entries and constant default actions, even when the supplied action equals the original. A constant table may still have a mutable default action. Default entries use MODIFY updates. Indirect tables require action profile references, so they cannot use this direct-action builder. `BuildKey()` can still identify their entries for reads or direct resource requests.

`IdleTimeout(0)` disables expiration. `Build()` rejects negative values, nonzero values on default entries and nonzero values on tables without `NOTIFY_CONTROL` support. `BuildKey()` continues to ignore idle timeout, action and metadata, including values that would be invalid for `Build()`.

These checks follow [P4Runtime's table entry rules](https://p4lang.github.io/p4runtime/spec/v1.5.0/P4Runtime-Spec.html#sec-table-entry).

`internal/codec` provides these helpers:

- `EncodeUint(uint64, bits)` — integer → canonical bytes.
- `EncodeBytes(bytes, bits)` — strips leading zeros, validates bit width.
- `LPMMask(value, prefix, bits)` — applies a prefix-length mask to value and returns the canonical encoding.

## Packet I/O metadata

`packetio.Subscriber.Send` validates all metadata before submitting a PacketOut. Every field declared by `packet_out` must be supplied, including padding. Missing and unknown names return an error. An explicit nil, empty or all-zero value encodes `00`. Unsigned values must fit their declared bit widths. The outgoing fields follow P4Info declaration order.

Without a `packet_out` definition, the subscriber accepts payload-only packets with empty metadata. Supplying metadata in that case returns an error. These rules follow [P4Runtime's Packet I/O requirements](https://p4lang.github.io/p4runtime/spec/v1.5.0/P4Runtime-Spec.html#sec-packet-io).

`p4ctl packet send` reads the `egress_port` bit width from P4Info, includes port zero and supplies zero for the bundled `_pad` field when present. Missing header or port definitions and out-of-range ports return errors before connecting. Programs with additional metadata fields require an application that supplies them through the SDK.

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

Write RPC errors expose `*errors.WriteError` through `errors.As`. `Updates[i]` is the target's `p4.Error` for request update `i`, including `OK` results. `errors.Is` matches any failed update in the batch. `status.FromError` preserves the RPC's original code, message, and details, including the top-level `Unknown` used for per-update failures.

Results are exposed only when an `Unknown` response contains exactly one valid `p4.Error` per request update and at least one failure. Otherwise `Updates` is nil and the raw status remains available. Check the result codes and requested atomicity before retrying a batch. The SDK does not retry writes automatically.
