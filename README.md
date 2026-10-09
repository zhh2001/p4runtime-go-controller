# p4runtime-go-controller

[![CI](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/ci.yml/badge.svg)](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/ci.yml)
[![CodeQL](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/codeql.yml/badge.svg)](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/codeql.yml)
[![codecov](https://codecov.io/gh/zhh2001/p4runtime-go-controller/branch/main/graph/badge.svg)](https://codecov.io/gh/zhh2001/p4runtime-go-controller)
[![Go Reference](https://pkg.go.dev/badge/github.com/zhh2001/p4runtime-go-controller/v2.svg)](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller/v2)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zhh2001/p4runtime-go-controller)](go.mod)
[![Latest Release](https://img.shields.io/github/v/release/zhh2001/p4runtime-go-controller?sort=semver)](https://github.com/zhh2001/p4runtime-go-controller/releases/latest)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://www.conventionalcommits.org)

A production-grade Go SDK for writing P4Runtime controllers.

- Works against any P4Runtime 1.3.0+ target (BMv2, Stratum, Tofino-based switches, custom ASIC agents).
- Zero hard dependency beyond `google.golang.org/grpc`, `google.golang.org/protobuf`, and the official P4Runtime proto stubs.
- Structured logging through `log/slog` and gRPC interceptor hooks for application metrics and tracing. Built-in metrics and adapters are planned. See [Observability](docs/observability.md).

> Published releases preserve source compatibility within a major version under the [Go 1 compatibility policy](https://go.dev/doc/go1compat). This branch develops v2 and is not source-compatible with `v1.1.1`. See the [CHANGELOG](CHANGELOG.md) and [release guide](docs/releases.md).

## Install

Requires Go 1.26 or newer. Before the first v2 release, use a local checkout as described in the [Quickstart](docs/quickstart.md#1-install).

After a v2 release is published, run this command from your application's Go module:

```sh
go get github.com/zhh2001/p4runtime-go-controller/v2@latest
```

## Quickstart

To start the bundled BMv2 target, follow [the Quickstart guide](docs/quickstart.md). It covers repository setup and the tools required for native and Docker modes.

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/zhh2001/p4runtime-go-controller/v2/client"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    c, err := client.Dial(ctx, "127.0.0.1:9559",
        client.WithDeviceID(1),
        client.WithElectionID(client.ElectionID{High: 0, Low: 1}),
    )
    if err != nil {
        log.Fatalf("dial: %v", err)
    }
    defer c.Close()

    if err := c.BecomePrimary(ctx); err != nil {
        log.Fatalf("arbitration: %v", err)
    }
    log.Println("primary controller for device 1")
}
```

See [`examples/`](examples/) for full end-to-end walkthroughs, including connection, pipeline push, L2 learning switch, packet I/O, and counter reads.

## Feature Matrix

| Capability                                                                      | Status       |
| ------------------------------------------------------------------------------- | ------------ |
| Connection management (TLS, keepalive, reconnect)                               | ready        |
| Mastership / arbitration (128-bit election ID)                                  | ready        |
| Pipeline configuration (VERIFY / RECONCILE / COMMIT with fallback)              | ready        |
| P4Info by-name / by-ID index                                                    | ready        |
| Table entry insert / modify / delete (EXACT / LPM / TERNARY / RANGE / OPTIONAL) | ready        |
| Indirect counters and meters, typed register arrays                             | ready        |
| PacketIn / PacketOut with metadata encode/decode                                | ready        |
| Digest subscribe and ack                                                        | ready        |
| Packet Replication Engine (multicast groups, clone sessions)                    | ready (v1.1) |
| Reference CLI (`p4ctl`)                                                         | ready        |
| Structured stream lifecycle logging (`log/slog`)                                | ready        |
| Built-in metrics collection                                                     | planned      |
| Prometheus adapter                                                              | planned      |
| OpenTelemetry gRPC interceptors                                                 | planned      |

## P4Runtime Compatibility

| Controller version    | P4Runtime spec |
| --------------------- | -------------- |
| `v1.x`                | 1.3.0+         |
| v2 development branch | 1.3.0+         |

This is the protocol baseline. Optional resources and newer fields require target support. Byte-based PRE ports require P4Runtime 1.4 or later, and backup replicas require 1.5 or later. See [PRE](pre/README.md).

## Documentation

- [`ARCHITECTURE.md`](ARCHITECTURE.md) — layered design and data-flow.
- [`docs/quickstart.md`](docs/quickstart.md) — run your first controller.
- [Migrating from v1 to v2](docs/migration-v2.md).
- [`docs/troubleshooting.md`](docs/troubleshooting.md) — common issues.
- [`docs/observability.md`](docs/observability.md) — logging and instrumentation hooks.
- [`docs/glossary.md`](docs/glossary.md) — P4, P4Runtime, PDPI, pipeline, etc.
- [`docs/i18n/README.zh-CN.md`](docs/i18n/README.zh-CN.md) — 中文版本。

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Please read the [Code of Conduct](CODE_OF_CONDUCT.md) before opening a pull request.

## Security

To report a vulnerability, follow the instructions in [SECURITY.md](SECURITY.md). Do not open a public issue for anything that could affect deployed controllers.

## License

Licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE) for third-party attribution.
