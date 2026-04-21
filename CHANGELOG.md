# Changelog

All notable changes to this project are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Starting with `v1.0.0` the public API follows the Go 1 compatibility promise;
every breaking change requires a major version bump.

## [Unreleased]

## [1.1.0] - 2026-04-21

### Added
- `pre` package wrapping the P4Runtime Packet Replication Engine:
  `MulticastGroup` + `CloneSession` with `Insert` / `Modify` / `Delete` /
  `Read` helpers, plus a `Replica` type carrying egress port and instance.
  Tests at 92 % statement coverage.

## [1.0.0] - 2026-04-20

Initial public release.

### Added
- Initial project scaffolding: module metadata, LICENSE, NOTICE, editor and
  ignore files.
- Governance documents: CONTRIBUTING, CODE_OF_CONDUCT, SECURITY, SUPPORT,
  MAINTAINERS.
- Architecture and design notes (`ARCHITECTURE.md`, `docs/architecture.md`,
  `DESIGN_NOTES.md`).
- Sentinel errors in the `errors` package (`ErrNotPrimary`,
  `ErrPipelineNotSet`, `ErrEntryExists`, `ErrEntryNotFound`,
  `ErrUnsupportedMatchKind`, `ErrTargetUnsupported`, `ErrStreamClosed`,
  `ErrArbitrationFailed`, `ErrElectionIDZero`, `ErrInvalidBitWidth`,
  `ErrInvalidMatchField`, `ErrInvalidActionParam`).
- `client` package: `Dial`, `Close`, `BecomePrimary`, `State`, `Events`,
  `OnPacketIn`, `OnDigestList`, `OnIdleTimeout`, `OnStreamMessage`,
  `SendPacketOut`, `SendDigestAck`, `SetPipeline`, `GetPipeline`, `Write`,
  `WriteTableEntry`, `Read`, `ReadTableEntries`.
- 128-bit `ElectionID` with `Less`, `Equal`, `Cmp`, `Increment`, `IsZero`,
  `String`, and `BigInt`.
- Functional options: `WithDeviceID`, `WithElectionID`, `WithRole`,
  `WithTLS`, `WithInsecure`, `WithCredentials`, `WithKeepalive`,
  `WithReconnectBackoff`, `WithArbitrationTimeout`, `WithMaxMessageSize`,
  `WithDialOptions`, `WithUnaryInterceptor`, `WithStreamInterceptor`,
  `WithLogger`.
- `pipeline` package with `Load`, `LoadText`, `New`, and by-name /
  by-ID indexes for tables, actions, action parameters, counters, direct
  counters, meters, direct meters, registers, digests, and controller
  packet metadata.
- `tableentry` package with the fluent `Builder` and `Exact`, `LPM`,
  `Ternary`, `Range`, `Optional` match constructors.
- `internal/codec` package for canonical byte encoding of integers, MAC,
  IPv4, IPv6, arbitrary bitstrings, LPM prefixes, TERNARY masks, and
  RANGE validation.
- `internal/stream` state-machine supervisor with exponential backoff and
  re-arbitration on reconnect.
- `internal/testutil` bufconn-backed mock P4Runtime server.
- `packetio` subscriber with metadata encode/decode via P4Info.
- `digest` subscriber with name-scoped filtering and `Ack`.
- `counter`, `meter`, `register` typed read/write wrappers.
- Reference CLI (`cmd/p4ctl`) built on cobra + viper with `connect`,
  `pipeline set|get`, `table insert|modify|delete|read`, `packet send|sniff`,
  `counter read`, and `version` subcommands.
- Example programs: `examples/01_connect`, `examples/02_l2_switch`,
  `examples/03_packetio`, `examples/04_counters`.
- User documentation: `docs/quickstart.md`, `docs/architecture.md`,
  `docs/troubleshooting.md`, `docs/performance.md`, `docs/glossary.md`,
  `docs/i18n/README.zh-CN.md`.
- GitHub workflows: `ci.yml`, `release.yml`, `codeql.yml`,
  `govulncheck.yml`, `stale.yml`.
- Release tooling: `.goreleaser.yaml`, `Makefile`, `Taskfile.yml`,
  `.golangci.yml`, `scripts/run-bmv2.sh`, `scripts/gen-docs.sh`,
  `scripts/check-shell.sh`.
- Integration test harness under `test/integration/` gated by the
  `integration` build tag.
