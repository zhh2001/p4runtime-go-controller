# Migrating from v1 to v2

This guide covers applications using the published v1.1.1 SDK. The v2 development branch changes the module path and several API behaviors. Review the [v2.0.0-rc.1 release notes](release-notes/v2.0.0-rc.1.md) alongside your application's tests.

## Update the module and imports

Use Go 1.26 or newer. The minimum is 1.26.0 and the preferred toolchain is 1.26.9. See [toolchain checks](../CONTRIBUTING.md#go-toolchain-checks).

Before the first v2 release, use a local SDK checkout whose `go.mod` declares `github.com/zhh2001/p4runtime-go-controller/v2`. Run these commands in your application's module, replacing the checkout path:

```sh
go mod edit -replace=github.com/zhh2001/p4runtime-go-controller/v2=/path/to/p4runtime-go-controller
go get github.com/zhh2001/p4runtime-go-controller/v2/client
```

Change each SDK import from `github.com/zhh2001/p4runtime-go-controller/<package>` to `github.com/zhh2001/p4runtime-go-controller/v2/<package>`. This includes the SDK `errors` package. Imports of P4Runtime protobufs, gRPC and the standard library keep their existing paths. The repository clone URL also stays unchanged. Your application does not need a `/v2` module suffix merely because it depends on this SDK.

After a v2 tag is published, remove the local replacement and select a published version:

```sh
go mod edit -dropreplace=github.com/zhh2001/p4runtime-go-controller/v2
go get github.com/zhh2001/p4runtime-go-controller/v2@latest
```

Use an exact published v2 tag instead of `@latest` for a reproducible upgrade or a prerelease trial. Run `go mod tidy` after updating imports and review `go.mod` and `go.sum`. The v1 module may remain if another dependency still uses it. Go supports [both major versions in one application](https://go.dev/blog/v2-go-modules), but their SDK types and error sentinels are distinct. Keep a client's wrappers and SDK error checks on the same major version.

## Replace positional literals and Replica comparisons

`pre.Replica` and `meter.Config` have additional fields. Convert positional literals to keyed literals:

```go
replica := pre.Replica{EgressPort: 7, Instance: 1}
config := meter.Config{
    CIR: 1000, CBurst: 2000,
    PIR: 1500, PBurst: 3000,
}
```

The meter example uses a two-rate meter. Single-rate meters require matching committed and peak fields. See [meter configuration](../meter/README.md).

`Replica` contains slices and cannot be compared with `==` or used as a map key. Compare the selected port field, instance and ordered backups explicitly:

```go
package controller

import (
    "bytes"

    "github.com/zhh2001/p4runtime-go-controller/v2/pre"
)

func sameReplica(a, b pre.Replica) bool {
    if a.EgressPort != b.EgressPort || a.Instance != b.Instance ||
        (a.Port == nil) != (b.Port == nil) || !bytes.Equal(a.Port, b.Port) ||
        len(a.BackupReplicas) != len(b.BackupReplicas) {
        return false
    }
    for i, backup := range a.BackupReplicas {
        other := b.BackupReplicas[i]
        if backup.Instance != other.Instance || !bytes.Equal(backup.Port, other.Port) {
            return false
        }
    }
    return true
}
```

For a map indexed by the primary destination, use a comparable key and keep the Replica as the value:

```go
package controller

import "github.com/zhh2001/p4runtime-go-controller/v2/pre"

type primaryReplicaKey struct {
    BytePort   bool
    Port       string
    EgressPort uint32
    Instance   uint32
}

func primaryKey(r pre.Replica) primaryReplicaKey {
    return primaryReplicaKey{
        BytePort: r.Port != nil, Port: string(r.Port),
        EgressPort: r.EgressPort, Instance: r.Instance,
    }
}
```

This key identifies the primary destination only. Replicas with different backup lists share it, so compare or store those lists according to your application's needs. String conversion preserves opaque port bytes, including leading and embedded zeros. Legacy and byte ports remain distinct.

Keep `EgressPort` for targets supporting only the legacy field. Set `Port` for supported P4Runtime 1.4+ targets, leaving `EgressPort` zero. Backup replicas require target support for P4Runtime 1.5. There is no automatic conversion between port fields. See [PRE](../pre/README.md).

## Review behavior changes

| Area                     | Changes to check in existing code                                                                                                                                                                                                                                                              |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Numeric encoding         | Zero is `00`, including an LPM mask with prefix zero. Use the prefix length to identify an LPM wildcard. `Optional(nil)` is a wildcard, while `Optional([]byte{})` matches zero. Update byte expectations and tests that treat an empty result as numeric zero.                                |
| Table entries            | Supply required EXACT fields, valid action parameters and allowed actions. Priority and idle timeout must match the table's capabilities. Use `BuildKey` for DELETE. Full-range RANGE, zero-prefix LPM and zero-mask ternary are omitted as wildcards.                                         |
| Pipeline ownership       | Constructor inputs and query results are copied. Compare resource IDs across queries. To change P4Info, edit a copy and construct a new Pipeline with matching device configuration. See [ownership](../pipeline/README.md).                                                                   |
| Pipeline actions         | `PipelineCommit` requires a nil pipeline and commits a previously saved config. Other actions require a pipeline. Only VERIFY_AND_COMMIT can fall back to RECONCILE_AND_COMMIT for an unsupported action. See [save and commit](troubleshooting.md#setforwardingpipelineconfig-keeps-failing). |
| Stream sends             | Send methods wait for gRPC send completion. Success does not confirm target processing or packet forwarding. Use a separate deadline for `CloseGracefully` outside stream handlers. `Close` still aborts immediately. See [shutdown](troubleshooting.md#ending-a-session-after-packetout).     |
| Mastership and callbacks | Disconnect and close revoke primary status. Use `BecomePrimary` to wait and `IsPrimary` to inspect state. `Events` is an observation channel. Canceling a callback prevents future dispatches, but callbacks selected for the current message may still run.                                   |
| PacketOut                | Supply every metadata field declared by P4Info, including padding. Values must fit the declared width. The SDK sends fields in declaration order.                                                                                                                                              |
| Digests                  | Prefer `Subscribe` to receive name and handler validation errors. Invalid `OnDigest` subscriptions register nothing. ACK requires a nonzero ID declared in P4Info and remains valid after subscription cancellation.                                                                           |
| Resource indexes         | Only `-1` selects an entire counter, meter or register array on read. Writes require a non-negative index. Ordinary indexes must be below the declared size. Named indexes are translated and checked by the target.                                                                           |
| Registers                | `Write` accepts only `bit<W>` and `int<W>`. Use two's complement for signed values and `WriteData` for other types. Varbits retain their length and require an explicit bitwidth. Valid header varbits remain unsupported. See [register values](../register/README.md).                       |
| Meters                   | Rates and bursts must be non-negative and satisfy the declared meter type. Replace the legacy `-1` default convention with `Reset`. `Write(Config{})` is an explicit zero configuration. See [meters](../meter/README.md).                                                                     |
| Client reads             | Handle `GetPipeline` errors such as `ErrPipelineNotSet` with `errors.Is`. `Client.Read` includes the configured role and does not require primary status. A returned P4Info can be usable without device configuration.                                                                        |
| Observability            | Use `WithLogger` and gRPC interceptor options. Built-in metrics collectors and adapters remain planned. See [observability](observability.md).                                                                                                                                                 |

## Handle partially successful writes

With the default CONTINUE_ON_ERROR atomicity, a sentinel match can describe one failed update in an otherwise successful batch. Check `errors.Is` for classification and `errors.As` for complete per-update results. Those results retain original request indexes, including successful updates.

The following helper reports failed indexes when complete results are available:

```go
package controller

import (
    "errors"

    "google.golang.org/grpc/codes"

    errs "github.com/zhh2001/p4runtime-go-controller/v2/errors"
)

func failedUpdateIndexes(err error) ([]int, bool) {
    var batch *errs.WriteError
    if !errors.As(err, &batch) || batch.Updates == nil {
        return nil, false
    }
    var failed []int
    for i, result := range batch.Updates {
        if result.GetCanonicalCode() != int32(codes.OK) {
            failed = append(failed, i)
        }
    }
    return failed, true
}
```

Use the per-update codes and your operation's retry policy before resending. When the helper returns false, the response does not identify a complete set of update outcomes. Do not assume every update failed. Other atomicity modes require their own rollback handling. The original gRPC status and details remain available through `status.FromError`. See [Write errors](troubleshooting.md#write-returns-unknown).

## Update CLI automation and bundled targets

Install the v2 CLI from the checkout with `go install ./cmd/p4ctl`. Once published, use `go install github.com/zhh2001/p4runtime-go-controller/v2/cmd/p4ctl@latest`, or select an exact tag. The executable name remains `p4ctl`, so check which installation your scripts invoke. Source installations normally report `dev` in `p4ctl version`. Use `go version -m /absolute/path/to/p4ctl` to inspect the module version. Release archives include version metadata supplied by GoReleaser.

Use `--config-file` or `P4CTL_CONFIG_FILE` for CLI settings. Keep `pipeline set --config` for the compiled device configuration:

```sh
p4ctl --config-file ./controller.yaml pipeline set \
    --p4info ./l2.p4info.txt \
    --config ./l2.bmv2.json
```

Explicit flags override nonempty environment variables, which override the settings file and defaults. Missing or invalid explicitly selected settings files return an error before connecting.

`--insecure=false` now enables certificate-verified TLS. A plaintext target needs `--insecure=true`. Private CAs and mutual TLS use the [TLS flags](../cmd/p4ctl/README.md#tls). JSON and YAML outputs are implemented for connect, pipeline set/get, table read and counter read. Update scripts to parse the selected format, including `[]` for empty reads. Protobuf bytes use base64 and 64-bit values use strings. Valid IPv6 literals take precedence over colon-separated bytes; prefix ambiguous raw bytes with `0x`.

`scripts/run-bmv2.sh` starts a native target by default. Use `--docker` for Docker. Recompile P4Info and device configuration together rather than combining an old generated file with a new one. The bundled target uses device ID 1 and CPU port 255. The L2 program sends misses to PacketIn and indexes `MyIngress.pkt_counter` by egress port. See [scripts](../scripts/README.md) and [integration tests](../test/integration/README.md).

## Validate the upgraded application

Run these commands from your application module after updating code:

```sh
go mod tidy
go list -m all
go build ./...
go vet ./...
go test -race ./...
```

Review remaining v1 imports and dependencies, saved byte encodings, CLI output consumers and retry behavior. Then run against a dedicated target with the features your application uses. A newer SDK does not add missing target capabilities. Use [release preparation](releases.md) for SDK release checks.
