# Performance Notes

This document describes the SDK's encoding, table-entry construction and stream dispatch paths, along with the benchmarks used to measure them.

## RPC comparison data

The [controller evaluation repository](https://github.com/zhh2001/p4runtime-controller-evaluation) contains the RPC performance dataset for SDK v2.0.1 and five comparison configurations. It provides 1,680 measurement rows across seven parameter points, with scripts that recompute 2,184 metric summaries and 56 paired throughput summaries, including 95% bootstrap intervals.

The runs use BMv2 over loopback on WSL2 with an 8 GiB memory limit and 2 GiB swap. They vary table population, write batch size and worker count. The evaluation repository documents the pinned software versions, workload and CSV units. Its reanalysis needs Python 3.12 and NumPy, without running a switch.

The repository also provides [benchmark runners and setup instructions](https://github.com/zhh2001/p4runtime-controller-evaluation/blob/main/docs/running-rpc.md). The installer prepares pinned client dependencies and builds the Go drivers and P4 fixture. The launcher supports a short validation of all seven parameter points and a full ten-round run. Each run records request outcomes, full table readbacks, source snapshots and environment information.

## Micro-benchmarks

Run the library's benchmarks with:

```sh
make bench
```

The current suite includes:

- `BenchmarkEncodeUint`: zero, a 9-bit port, and full-width 32-bit and 64-bit integers.
- `BenchmarkEncodeBytes`: canonical, zero-padded and zero inputs for a 32-bit field.
- `BenchmarkLPMMask`: applying a 24-bit prefix to a 32-bit IPv4 value.
- `BenchmarkTableEntryBuild`: constructing an EXACT entry with a direct action, using a new or reused builder.

For repeated measurements of these paths:

```sh
go version
go test -run '^$' -bench 'Benchmark(EncodeUint|EncodeBytes|LPMMask|TableEntryBuild)$' -benchmem -count=5 ./internal/codec ./tableentry
```

The benchmarks use `testing.B.Loop` to exclude setup and retain measured calls, and report allocations. Both builder cases set the same match and action on each iteration. The `new` case includes builder creation and the table lookup. The `reused` case creates the builder before timing.

Record the Go version, CPU, command and results when comparing runs. Timing and allocation counts can change with the toolchain, compiler and hardware. See [Go toolchain checks](../CONTRIBUTING.md#go-toolchain-checks) for choosing a specific version. The suite measures local SDK work without a target RPC, so its results do not measure switch throughput or forwarding latency.

## Encoding and builder reuse

`codec.EncodeBytes` returns an independent byte slice even when the input is already canonical. Nil, empty and all-zero inputs produce a single zero byte. Copying the result keeps changes to the input and output separate. Include this copy when estimating allocation costs.

`Build` and `BuildKey` create fresh protobuf messages. They retain the builder's settings for later calls. `Match(field, value)` replaces the constraint for that field, and `Action` replaces the action and its parameters. Other matches, priority, metadata, idle timeout and the default-entry flag remain set until changed. Use a new builder when you need to discard prior settings or return from a default entry to an ordinary entry.

Match values and action parameter slices remain borrowed by the builder. Keep them unchanged while building, or supply your own copies. `Metadata` copies its input. Built entries have independent encoded bytes, so later builder changes do not edit an earlier result. A builder has no synchronization. Use separate builders per goroutine or protect access with a lock.

## Stream callbacks

The supervisor has a main loop for sends and connection state, plus a receive goroutine for each active stream. Packet, digest and idle-timeout callbacks run inline in that receive goroutine. Catch-all callbacks run before typed callbacks for each message. Ordering among callbacks in the same group is unspecified.

A slow callback delays the next receive, including later packets, arbitration responses and receive errors. The send loop runs separately. Move slow work to a worker pool or an application queue, and decide how to handle a full queue so it does not stall reception. A callback already in progress can outlive its stream, so synchronize shared application state.

## Throughput tips

- **Batch writes**. Send multiple updates in one `Client.Write(ctx, opts, updates...)` call to reduce RPC overhead. Account for target limits, atomicity and partial failures when choosing a batch size.
- **Raise the gRPC message size** if you hit `ResourceExhausted`. The defaults are 32 MiB for both send and receive; use `client.WithMaxMessageSize(64<<20)` for larger bulk pipelines.
- **Tune `WithReconnectBackoff`** for failed stream creation or arbitration. A failure after successful arbitration starts the next attempt immediately. See [Observability](observability.md) for retry logs.

## Metrics

Built-in metrics and adapters are planned. The `metrics` package has no collector API or emitted measurements. Applications can measure unary RPC latency through `client.WithUnaryInterceptor` and stream creation through `client.WithStreamInterceptor`. To measure stream messages and final errors, wrap the returned `grpc.ClientStream` and instrument `SendMsg` and `RecvMsg`.

`Client.State` gives the current session state. `Client.Events` provides best-effort notifications and can drop events when its buffers fill, so it cannot provide an exact reconnect or arbitration-failure count. See [Observability](observability.md) for logging and instrumentation scope.
