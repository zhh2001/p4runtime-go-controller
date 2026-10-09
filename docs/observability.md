# Observability

## Logging

The SDK uses `log/slog`. `client.WithLogger` supplies the logger used by the stream supervisor and pipeline fallback. Without this option, the client uses `slog.Default`. Passing nil leaves the previously selected logger unchanged.

Configure a JSON logger with DEBUG enabled:

```go
logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))
c, err := client.Dial(ctx, "127.0.0.1:9559",
    client.WithDeviceID(1),
    client.WithElectionID(client.ElectionID{Low: 1}),
    client.WithInsecure(),
    client.WithLogger(logger),
)
```

This snippet uses `context`, `log/slog`, `os` and the SDK's `client` package. Check the Dial error and close the client when the session ends.

| Level | Records                                                                                      |
| ----- | -------------------------------------------------------------------------------------------- |
| INFO  | Each StreamChannel opening attempt, primary or backup changes, pipeline fallback             |
| WARN  | Stream opening, arbitration, receive, send or graceful-close failures                        |
| DEBUG | State transitions, unchanged arbitration responses, actual retry delays, supervisor shutdown |

Stream records include the configured `device_id`, `role`, `election_id_high` and `election_id_low`. Arbitration records include `state` and `status_code`. Failure records include `stage` and `error`. Retry records include `delay` as a `time.Duration`, encoded in nanoseconds by slog's JSON handler. Attributes and groups already attached to the supplied logger are preserved.

Client shutdown does not produce a failure warning. State and shutdown records remain available at DEBUG. Packet payloads and pipeline configuration are not logged by the supervisor. Errors include the transport or target's error text.

Opening attempts refer to the P4Runtime StreamChannel RPC. gRPC can retry a transport connection within one attempt. Failed stream creation or arbitration uses exponential backoff with jitter. A failure after arbitration starts the next attempt immediately. The retry log records the same delay used by the timer.

Log handlers run synchronously. They must return promptly and must not call `Close` or `CloseGracefully` on the same client. They can inspect `State` and `IsPrimary`. Send shutdown requests to another goroutine if a handler needs to end a session.

## Metrics and tracing

The `metrics` package is reserved for future support. It has no collector interface, emitted counters or histograms, no-op collector, Prometheus adapter or OpenTelemetry adapter. These integrations are planned.

Applications can use `client.WithUnaryInterceptor` to measure RPC latency and status. `client.WithStreamInterceptor` observes stream creation. Measuring streamed messages and final RPC errors requires wrapping the returned `grpc.ClientStream` and instrumenting `SendMsg` and `RecvMsg`. The same hooks can connect application tracing without adding a tracing dependency to the SDK.

`Client.State` provides the latest session state. `Client.Events` delivers best-effort notifications through bounded queues. Full queues drop notifications, so an event consumer cannot count every reconnect or arbitration failure. Events also compete between readers. Use one reader to distribute events within an application.
