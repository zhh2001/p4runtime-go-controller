# Performance Notes

The SDK is designed for production controller deployments. This document
records the shape of the hot paths, the benchmarks we run, and tips for
keeping throughput and latency predictable.

## Micro-benchmarks

Run the library's benchmarks with:

```sh
make bench
```

The current suite includes:

- `BenchmarkEncodeBytesUint` — canonical byte encoding for fixed-width
  integers.
- `BenchmarkLPMMask` — prefix masking for LPM match fields.
- `BenchmarkTableEntryBuild` — building a single `TableEntry` proto through
  the fluent builder.

Benchmarks are reproducible on any machine; there is no reference hardware
lock-in. When you add features that touch these paths, include a benchmark
alongside the change and record the delta in `CHANGELOG.md`.

## Hot-path principles

- **No allocations in match-field encoders for values already in canonical
  form.** `codec.EncodeBytes` checks the input and only allocates when it
  must strip leading zeros.
- **Builder is reusable**. `tableentry.NewBuilder` + `.Build()` creates a
  fresh proto every time the builder is executed, so callers can keep one
  builder per flow-table and only vary the match values.
- **Stream supervisor is single-goroutine**. All dispatch runs on one
  goroutine. If you register a handler that blocks, you block the receive
  loop. Push slow work onto a buffered channel or a worker pool.

## Throughput tips

- **Batch writes**. A single `Client.Write(ctx, opts, updates...)` with
  dozens or hundreds of updates is dramatically cheaper than one call per
  update.
- **Raise the gRPC message size** if you hit `ResourceExhausted`. The
  defaults are 32 MiB for both send and receive; use
  `client.WithMaxMessageSize(64<<20)` for larger bulk pipelines.
- **Use `WithReconnectBackoff`** to shorten recovery in controlled
  environments (labs, CI) where the target is known to come back quickly.

## Metrics

The SDK does not depend on any metrics backend by default. Wire your own
collector through the `metrics` package and expose the counters you care
about (write latency, reconnect count, arbitration failures).
