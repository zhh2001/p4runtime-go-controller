# Indirect counters

`counter.Reader` reads and writes indexed counter arrays by P4Info name or alias. Direct counters attached to table entries require the raw client API.

```go
r, err := counter.NewReader(c, p)
if err != nil {
    return err
}
samples, err := r.Read(ctx, "stats", -1)
```

Pass `-1` to read every index. Other reads and all writes require a non-negative index below the P4Info array size. Named index types retain their numeric value for target translation and validation. Each sample contains the canonical counter name, ID, index and counts returned by the target.

| P4Info unit | Count used by the counter |
| ----------- | ------------------------- |
| `PACKETS`   | Packets                   |
| `BYTES`     | Bytes                     |
| `BOTH`      | Packets and bytes         |

`Write(ctx, name, index, packets, bytes)` sends both fields in a `MODIFY` request. P4Runtime permits simultaneous packet and byte updates regardless of the declared unit. The target applies that unit and its own limits. For a single-unit counter, only the corresponding field has a defined counter meaning. Read preserves either field if the target returns it.

Counts use the protocol's `int64` fields. The SDK sends and returns these values unchanged, including values above uint32 and negative wire values. It does not impose a shared target range or convert negative values to unsigned decimal numbers. A target may reject values it cannot represent. BMv2 stores unsigned 64-bit counters internally, so its PI implementation can return negative int64 wire values when the high bit is set. That behavior does not establish negative-value support for other targets.

`Write(ctx, name, index, 0, 0)` includes an explicit `CounterData` message and clears the counts selected by the target's unit. Omitting `CounterData` in a raw request leaves the counts unchanged. Unsupported writes return `ErrTargetUnsupported` without an automatic retry.

`p4ctl counter read` displays the returned values as signed decimal numbers. JSON and YAML encode the int64 index and counts as strings to preserve precision. The CLI provides counter reads. Use the SDK for writes.

The request and zero-value semantics follow [P4Runtime counter entries](https://p4lang.github.io/p4runtime/spec/v1.5.0/P4Runtime-Spec.html#_counterentry_directcounterentry).
