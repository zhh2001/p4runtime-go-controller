# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Published releases follow the Go 1 source compatibility policy within a major version. Incompatible public API changes require a major version bump. The current Unreleased changes are planned for the next major release. See [Release preparation](docs/releases.md).

## [Unreleased]

### Added

- A [v1 to v2 migration guide](docs/migration-v2.md) covering application dependencies, source changes, behavior checks and CLI automation. The release guide also distinguishes SDK tags, draft releases and CLI archive installations.
- `client.Client.CloseGracefully(ctx)` drains accepted stream sends, half-closes StreamChannel and waits for the target's final RPC status. It releases the connection on success, error or timeout. Use a deadline and call it outside stream handlers.
- `errors.WriteError` preserves the original gRPC status and complete per-update results in request order, including successful updates. Use the standard library's `errors.As` to inspect `Updates` before retrying a partially successful batch. Its `errors.Is` matches any failed update. `Updates` is nil when the response lacks valid, complete results.
- `tableentry.Builder.BuildKey()` builds table keys without requiring an action. It includes table ID, matches, priority and the default-entry flag. CLI table deletion uses this key without an action.
- `digest.Subscriber.Subscribe(name, handler)` returns a cancellation function and a validation error. `OnDigest` remains available and returns a no-op cancellation function for invalid subscriptions. An empty name subscribes to every non-nil digest notification.
- `pre.Replica.Port`, `pre.Replica.BackupReplicas` and `pre.BackupReplica` preserve byte ports and ordered backups. Byte ports retain their original bytes, including leading zeros and target-specific string encodings. Set either `Port` or the legacy `EgressPort`.
- `register.Reader.WriteData(ctx, name, index, data)` validates typed P4Data against P4Info and copies it before sending. It supports compound values and translated types. Varbits preserve their bytes and require an explicit bitwidth with matching length. Varbit fields in valid headers are unsupported because P4Header does not carry their bitwidth.
- `meter.Config.EBurst` supports single-rate three-color meters. `meter.Reader.Reset(ctx, name, index)` omits the configuration to restore default GREEN behavior. `Write(Config{})` continues to send an explicit zero configuration.
- CLI TLS configuration with `--insecure=false`, system CAs by default, `--tls-ca`, `--tls-server-name` and paired `--tls-cert` / `--tls-key` options for mutual authentication. TLS options are rejected when insecure transport is selected.
- Global CLI configuration through `--config-file` and `P4CTL_CONFIG_FILE`. The legacy global `--config` remains available outside `pipeline set`, where `--config` selects the device configuration.
- CLI JSON and YAML output for `connect`, `pipeline set|get`, `table read` and `counter read`. Read results are arrays, including empty results. Protobuf values use the official JSON mapping, and diagnostics go to stderr.
- StreamChannel logging through the configured `slog.Logger`, covering opening attempts, mastership changes, failures, retry delays and shutdown. See [Observability](docs/observability.md).

### Changed

- Security maintenance covers the two most recent published minor lines within the latest published major. Prereleases count toward that window, so the first published v2 prerelease ends v1 security maintenance. Development branches and draft releases do not advance it. Issue reports now request the v2 module path and the CLI version subcommand, with Go build information for source installations.
- The module path, SDK imports, tests and examples now use `github.com/zhh2001/p4runtime-go-controller/v2`. Published v1 tags retain the unsuffixed path. Before the first v2 release, applications can use a local checkout through a replace directive. See [Quickstart](docs/quickstart.md#1-install).
- Release tags use the v2 series, and local snapshots use a `2.0.0-SNAPSHOT` version label even while the latest existing tag is v1.
- GoReleaser keeps scoped and unscoped `feat!`, `fix!` and `docs!` subjects in their corresponding release-note groups.
- Updated grpc-go to 1.83.2 and x/net to 0.60.0. Go 1.26 is now the minimum version, and Go 1.26.9 is the preferred toolchain.
- `SendStreamRequest`, `SendPacketOut`, `SendDigestAck` and `packetio.Subscriber.Send` wait for gRPC send completion and return send errors to the caller. Completion does not acknowledge target processing or packet forwarding. CLI `packet send` uses graceful shutdown after sending. `Close` retains immediate shutdown and can be called from a stream handler.
- Mastership becomes non-primary on disconnection or shutdown. `BecomePrimary` observes current state independently of `Events` and supports concurrent waiters. Stream shutdown coordinates queue closure with active senders.
- Stream callbacks run outside the registration lock and can register or cancel subscriptions. Cancellation affects future dispatches. Callbacks already selected for the current message may still run.
- `SetPipeline` requires a nil pipeline for `PipelineCommit` and a non-nil pipeline for other actions. Only VERIFY_AND_COMMIT may fall back to RECONCILE_AND_COMMIT when the target reports an unsupported action. Explicit VERIFY, VERIFY_AND_SAVE, COMMIT and RECONCILE_AND_COMMIT actions stay within their requested scope. `NoFallback` also applies to the default action.
- Read requests include the configured role without requiring primary status. `GetPipeline` reports `ErrPipelineNotSet` for an absent configuration while preserving the target's gRPC status and details when present. A configuration containing only P4Info remains usable.
- Table entry validation checks required exact matches, field widths, action membership and scope, priority, idle timeout support and constant-table restrictions. LPM and ternary masks use the declared field width. Range endpoints are compared after normalization. Full-range, zero-prefix LPM, zero-mask ternary and nil optional matches are omitted as wildcards.
- PacketOut metadata must supply every field declared by P4Info, including padding fields. Metadata is validated and encoded in declaration order. CLI packet sending derives the egress-port width from P4Info and fills padding fields with zero.
- Digest acknowledgements require a nonzero digest ID declared in P4Info. They may be sent after subscription cancellation and do not require an active subscription.
- Counter, meter and register reads use `-1` to select all entries and reject other negative indexes. Writes require a non-negative index. Ordinary indexes are checked against the declared array size. Named index types are left to target-side translation and validation.
- Meter writes validate the declared meter type and require non-negative rates and bursts. Two-rate meters require PIR >= CIR. Single-rate meters require CIR = PIR and CBurst = PBurst. EBurst is zero except for single-rate three-color meters. Target-specific numeric limits remain with the target.
- CLI configuration precedence is flags, non-empty environment variables, configuration file, then defaults. Explicit missing, unreadable or invalid configuration files return errors before dialing.
- CLI table values, masks, ranges and action parameters share P4Info width validation. Valid IPv6 takes precedence over colon-separated bytes. Use a `0x` prefix for unambiguous raw bytes, such as `0x01:02:03:04:05:06:07:08`. MAC addresses remain supported, and malformed values return errors.
- `scripts/run-bmv2.sh` uses native `simple_switch_grpc` by default and retains an explicit Docker mode. The launcher and integration harness compile paired L2 artifacts from source. The default device ID is 1 and the CPU port is 255. See [Scripts](scripts/README.md).
- The L2 example program retains its direct counter and adds the indirect `MyIngress.pkt_counter`, indexed by egress port. L2 misses produce PacketIn messages through CPU port 255. PacketOut uses its egress metadata and removes the control header before forwarding. Examples and integration tests use the compiled P4Info names.
- Pipeline constructors copy P4Info and device configuration. Info, Raw and resource queries return independent copies. Compare resource IDs across queries and construct a new Pipeline to use an edited P4Info. See [Pipeline ownership](pipeline/README.md).
- Pipeline construction reports pointer cycles and malformed nil message values before copying. Ordinary type and resource validation remains with the corresponding APIs and target.
- Metrics documentation describes the current interceptor hooks and planned built-in support. The `metrics` package remains a reserved namespace without a collector API or adapters.

### Compatibility notes

- `pre.Replica` now contains slices and cannot be compared with `==` or used as a map key. Compare fields and byte slices explicitly. Use keyed struct literals for `Replica` and `meter.Config`, whose field sets have grown. See [PRE](pre/README.md).
- Canonical numeric zero is now a single `00` byte, including `codec.LPMMask` with prefix length zero. Test the prefix length to identify an LPM wildcard. `Optional(nil)` is a wildcard, while `Optional([]byte{})` explicitly matches zero.
- `register.Reader.Write([]byte)` accepts only `bit<W>` and `int<W>`, including named types resolving to them. Signed values use big-endian two's complement. Use `WriteData` for other types. See [Register values](register/README.md).
- Negative meter values, including the target-specific `-1` convention, are rejected. Use `Reset` for default GREEN behavior. An explicit zero configuration remains distinct from reset. See [Meter configuration](meter/README.md).

## [1.1.1] - 2026-04-24

### Added

- Public `codec` package re-exporting the canonical integer, byte string, MAC, IPv4, IPv6, mask, range and hexadecimal helpers for use outside the module.

## [1.1.0] - 2026-04-21

### Added
- `pre` package wrapping the P4Runtime Packet Replication Engine: `MulticastGroup` + `CloneSession` with `Insert` / `Modify` / `Delete` / `Read` helpers, plus a `Replica` type carrying egress port and instance. Tests at 92 % statement coverage.

## [1.0.0] - 2026-04-20

Initial public release.

### Added
- Initial project scaffolding: module metadata, LICENSE, NOTICE, editor and ignore files.
- Governance documents: CONTRIBUTING, CODE_OF_CONDUCT, SECURITY, SUPPORT, MAINTAINERS.
- Architecture documentation (`ARCHITECTURE.md`, `docs/architecture.md`).
- Sentinel errors in the `errors` package (`ErrNotPrimary`, `ErrPipelineNotSet`, `ErrEntryExists`, `ErrEntryNotFound`, `ErrUnsupportedMatchKind`, `ErrTargetUnsupported`, `ErrStreamClosed`, `ErrArbitrationFailed`, `ErrElectionIDZero`, `ErrInvalidBitWidth`, `ErrInvalidMatchField`, `ErrInvalidActionParam`).
- `client` package: `Dial`, `Close`, `BecomePrimary`, `State`, `Events`, `OnPacketIn`, `OnDigestList`, `OnIdleTimeout`, `OnStreamMessage`, `SendPacketOut`, `SendDigestAck`, `SetPipeline`, `GetPipeline`, `Write`, `WriteTableEntry`, `Read`, `ReadTableEntries`.
- 128-bit `ElectionID` with `Less`, `Equal`, `Cmp`, `Increment`, `IsZero`, `String`, and `BigInt`.
- Functional options: `WithDeviceID`, `WithElectionID`, `WithRole`, `WithTLS`, `WithInsecure`, `WithCredentials`, `WithKeepalive`, `WithReconnectBackoff`, `WithArbitrationTimeout`, `WithMaxMessageSize`, `WithDialOptions`, `WithUnaryInterceptor`, `WithStreamInterceptor`, `WithLogger`.
- `pipeline` package with `Load`, `LoadText`, `New`, and by-name / by-ID indexes for tables, actions, action parameters, counters, direct counters, meters, direct meters, registers, digests, and controller packet metadata.
- `tableentry` package with the fluent `Builder` and `Exact`, `LPM`, `Ternary`, `Range`, `Optional` match constructors.
- `internal/codec` package for canonical byte encoding of integers, MAC, IPv4, IPv6, arbitrary bitstrings, LPM prefixes, TERNARY masks, and RANGE validation.
- `internal/stream` state-machine supervisor with exponential backoff and re-arbitration on reconnect.
- `internal/testutil` bufconn-backed mock P4Runtime server.
- `packetio` subscriber with metadata encode/decode via P4Info.
- `digest` subscriber with name-scoped filtering and `Ack`.
- `counter`, `meter`, `register` typed read/write wrappers.
- Reference CLI (`cmd/p4ctl`) built on cobra + viper with `connect`, `pipeline set|get`, `table insert|modify|delete|read`, `packet send|sniff`, `counter read`, and `version` subcommands.
- Example programs: `examples/01_connect`, `examples/02_l2_switch`, `examples/03_packetio`, `examples/04_counters`.
- User documentation: `docs/quickstart.md`, `docs/architecture.md`, `docs/troubleshooting.md`, `docs/performance.md`, `docs/glossary.md`, `docs/i18n/README.zh-CN.md`.
- GitHub workflows: `ci.yml`, `release.yml`, `codeql.yml`, `govulncheck.yml`, `stale.yml`.
- Release tooling: `.goreleaser.yaml`, `Makefile`, `Taskfile.yml`, `.golangci.yml`, `scripts/run-bmv2.sh`, `scripts/gen-docs.sh`, `scripts/check-shell.sh`.
- Integration test harness under `test/integration/` gated by the `integration` build tag.
