# Troubleshooting

## `ErrArbitrationFailed` on Dial

The target did not respond to the initial `MasterArbitrationUpdate` within the arbitration timeout. Common causes:

- The target is still warming up (BMv2 takes a few hundred ms to start the gRPC server).
- Another controller owns the primary election with a higher election ID — you are connected but backup; inspect `c.State()` to confirm.
- TLS mismatch: you dialed with `WithInsecure()` but the server requires TLS, or vice versa.

Bump the timeout with `client.WithArbitrationTimeout(30 * time.Second)` and re-check logs at `INFO` level — the supervisor logs every connection attempt.

## `ErrNotPrimary` on Write / SetPipeline

The client lost primary status (stream drop + re-arbitration, or another controller took over with a higher election ID). Call `c.BecomePrimary(ctx)` to wait for primary to return, or bump the election ID.

## SetForwardingPipelineConfig keeps failing

The SDK walks `VERIFY_AND_COMMIT → RECONCILE_AND_COMMIT → COMMIT`. If every step fails, the final error is returned. Typical root causes:

- P4Info bytes do not match the compiled pipeline blob. Re-run `p4c` to produce a matching pair.
- The target dislikes `RECONCILE_AND_COMMIT` on first boot — pass `client.SetPipelineOptions{Action: client.PipelineCommit}` once to seed the pipeline, then switch back to the default.

## Write returns `Unknown`

P4Runtime reports per-update failures with RPC code `Unknown` and ordered `p4.Error` details. Use `errors.Is(err, errs.ErrEntryExists)` or `errors.Is(err, errs.ErrEntryNotFound)` to classify known failures. In a batch, either match can refer to just one update.

With the standard library `errors` package and the SDK error package imported as `errs`, inspect results before deciding which updates to retry:

```go
var writeErr *errs.WriteError
if errors.As(err, &writeErr) {
    for i, result := range writeErr.Updates {
        if result.GetCanonicalCode() != int32(codes.OK) {
            log.Printf("update %d: %s", i, result.GetMessage())
        }
    }
}
```

`Updates` includes successful results at their original indices. It is nil for RPC-wide errors or incomplete or malformed details. `status.FromError(err)` still exposes the original response. For `CONTINUE_ON_ERROR`, successful updates may already be stored. For rollback or atomic writes, account for that atomicity when deciding what to retry.

A `PermissionDenied` response can also indicate a role restriction. Only RPC-wide messages explicitly identifying lost primary status map to `ErrNotPrimary`.

## Leading zeros in match fields

P4Runtime's canonical integer encoding uses the shortest nonempty byte string. Zero is `[]byte{0x00}`. Extra leading zero bytes are accepted when the value fits the field width, but canonical values preserve read-write symmetry. Use `codec.EncodeBytes`, `codec.MAC`, `codec.IPv4`, or `codec.IPv6` to normalize values.

For an OPTIONAL match, use `tableentry.Optional(nil)` for a wildcard and `tableentry.Optional(codec.MustEncodeUint(0, width))` to match zero. A zero value remains an explicit match.

## Packet-in decode drops metadata

The SDK only decodes metadata fields it can resolve through the active `Pipeline`. If a metadata field is missing, verify that the P4Info you loaded actually declares `controller_packet_metadata` with the expected name and field IDs.

## High CPU in the stream goroutine

If you registered a slow `PacketInHandler`, it runs inline on the supervisor goroutine and blocks the next receive. Push work to a buffered channel or a worker pool.

## BMv2 on Apple Silicon

Pin the image to an arm64 build:

```sh
docker pull p4lang/behavioral-model:latest-arm64
./scripts/run-bmv2.sh -i p4lang/behavioral-model:latest-arm64
```

## Running integration tests

```sh
./scripts/run-bmv2.sh
make e2e
```

The integration suite uses the `integration` build tag; it is excluded from the default `make test` run.
