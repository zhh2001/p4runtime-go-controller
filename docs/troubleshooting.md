# Troubleshooting

## `ErrArbitrationFailed` on Dial

The target did not respond to the initial `MasterArbitrationUpdate` within the arbitration timeout. Common causes:

- The target is still warming up (BMv2 takes a few hundred ms to start the gRPC server).
- Another controller owns the primary election with a higher election ID — you are connected but backup; inspect `c.State()` to confirm.
- TLS mismatch: you dialed with `WithInsecure()` but the server requires TLS, or vice versa.

Bump the timeout with `client.WithArbitrationTimeout(30 * time.Second)` and re-check logs at `INFO` level — the supervisor logs every connection attempt.

## `ErrNotPrimary` on Write / SetPipeline

The client lost primary status (stream drop + re-arbitration, or another controller took over with a higher election ID). Call `c.BecomePrimary(ctx)` to wait for primary to return, or bump the election ID.

## CLI TLS connection fails

`p4ctl --insecure=false` enables TLS and verifies the target certificate. Use `--tls-ca` for a private CA bundle and `--tls-server-name` when the certificate's DNS name differs from the address used to connect. For mutual TLS, both `--tls-cert` and `--tls-key` are required.

Check that the target serves TLS, the certificate is valid for the requested name, and its issuer is trusted. Authentication failures can appear as a connection timeout while the SDK waits for arbitration. The CLI never falls back to plaintext. Use `--insecure=true` only when a plaintext connection is intended, and omit TLS-specific flags in that mode.

## SetForwardingPipelineConfig keeps failing

The default call tries `VERIFY_AND_COMMIT`, then `RECONCILE_AND_COMMIT` only if the first action is explicitly unsupported. If both fail, the final error is returned. `NoFallback: true` disables the second attempt. Typical root causes:

- P4Info bytes do not match the compiled pipeline blob. Re-run `p4c` to produce a matching pair.
- The target cannot preserve existing entries for the supplied config. An explicit `RECONCILE_AND_COMMIT` does not fall back to clearing those entries. Choose `VERIFY_AND_COMMIT` separately if clearing them is intended.

For a separate save and commit, first save the config, check the result, then commit with a nil pipeline:

```go
if _, err := c.SetPipeline(ctx, p, client.SetPipelineOptions{
    Action: client.PipelineVerifyAndSave,
}); err != nil {
    return err
}
_, err := c.SetPipeline(ctx, nil, client.SetPipelineOptions{
    Action: client.PipelineCommit,
})
```

`COMMIT` uses the last saved config on the target. It sends no new config and requires a successful prior save. Passing a non-nil pipeline with `COMMIT` is rejected locally. `VERIFY` and `VERIFY_AND_SAVE` never fall back to an installation action.

BMv2 1.15.0-2bdd0b7b with PI 5689c91 cannot reconcile the bundled L2 pipeline's indirect counters. PI restores them using `INSERT`, and the target logs `INSERT update type not supported for counters`. The RPC returns `Error when reconciling config` and can leave the device in an unfinished update. Restart that target before installing with `VERIFY_AND_COMMIT`. Restarting and fresh installation clear forwarding state. Initial installation and counter reads work with this build.

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

## Ending a session after PacketOut

`SendPacketOut` and `packetio.Subscriber.Send` wait for gRPC to accept the request for transmission. They do not wait for target receipt. For a session that sends a packet and then exits, call `CloseGracefully(ctx)` with a deadline. It half-closes the send direction, waits for the target's final RPC status, and releases the client even if the wait fails. `p4ctl packet send` uses this sequence.

Some targets keep the response stream open after the send direction closes. For those targets, the graceful wait returns the context error at its deadline. Do not retry automatically after a send or close error, since the target may already have received the request. Stream completion does not acknowledge individual packet forwarding. Stream handlers should use `Close` when they need to abort, and leave graceful shutdown to the caller.

## Packet-in decode drops metadata

The SDK only decodes metadata fields it can resolve through the active `Pipeline`. If a metadata field is missing, verify that the P4Info you loaded actually declares `controller_packet_metadata` with the expected name and field IDs.

## High CPU in the stream goroutine

If you registered a slow `PacketInHandler`, it runs inline on the supervisor goroutine and blocks the next receive. Push work to a buffered channel or a worker pool.

## BMv2 on Apple Silicon

Pin the image to an arm64 build:

```sh
docker pull p4lang/behavioral-model:latest-arm64
./scripts/run-bmv2.sh --docker -i p4lang/behavioral-model:latest-arm64
```

## Running integration tests

```sh
./scripts/run-bmv2.sh
make e2e
```

The integration suite uses the `integration` build tag; it is excluded from the default `make test` run.

The launcher runs a native target in the foreground by default. Keep it running in another terminal, or use `--docker` for a detached container. It compiles the L2 artifacts and enables CPU port 255. `make e2e` generates its own matching L2 pair and passes both paths to the tests. See [the integration guide](../test/integration/README.md) for optional suites and existing PI version limits.
