# p4runtime-go-controller

[![CI](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/ci.yml/badge.svg)](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/ci.yml)
[![CodeQL](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/codeql.yml/badge.svg)](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/codeql.yml)
[![govulncheck](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/govulncheck.yml/badge.svg)](https://github.com/zhh2001/p4runtime-go-controller/actions/workflows/govulncheck.yml)
[![codecov](https://codecov.io/gh/zhh2001/p4runtime-go-controller/branch/main/graph/badge.svg)](https://codecov.io/gh/zhh2001/p4runtime-go-controller)
[![Go Reference](https://pkg.go.dev/badge/github.com/zhh2001/p4runtime-go-controller.svg)](https://pkg.go.dev/github.com/zhh2001/p4runtime-go-controller)
[![Go Report Card](https://goreportcard.com/badge/github.com/zhh2001/p4runtime-go-controller)](https://goreportcard.com/report/github.com/zhh2001/p4runtime-go-controller)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zhh2001/p4runtime-go-controller)](go.mod)
[![Latest Release](https://img.shields.io/github/v/release/zhh2001/p4runtime-go-controller?sort=semver)](https://github.com/zhh2001/p4runtime-go-controller/releases/latest)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Conventional Commits](https://img.shields.io/badge/Conventional%20Commits-1.0.0-yellow.svg)](https://www.conventionalcommits.org)

A production-grade Go SDK for writing P4Runtime controllers.

- Works against any P4Runtime 1.3.0+ target (BMv2, Stratum, Tofino-based
  switches, custom ASIC agents).
- Zero hard dependency beyond `google.golang.org/grpc`,
  `google.golang.org/protobuf`, and the official P4Runtime proto stubs.
- Structured logging through `log/slog`, pluggable metrics, pluggable tracing.

> The public API is stable as of `v1.0.0` and follows the
> [Go 1 compatibility promise](https://go.dev/doc/go1compat). Every change is
> documented in the [CHANGELOG](CHANGELOG.md).

## Install

```sh
go get github.com/zhh2001/p4runtime-go-controller@latest
```

## Quickstart

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/zhh2001/p4runtime-go-controller/client"
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

See [`examples/`](examples/) for full end-to-end walkthroughs, including
connection, pipeline push, L2 learning switch, packet I/O, and counter reads.

## Feature Matrix

| Capability | Status |
| --- | --- |
| Connection management (TLS, keepalive, reconnect) | ready |
| Mastership / arbitration (128-bit election ID) | ready |
| Pipeline configuration (VERIFY / RECONCILE / COMMIT with fallback) | ready |
| P4Info by-name / by-ID index | ready |
| Table entry insert / modify / delete (EXACT / LPM / TERNARY / RANGE / OPTIONAL) | ready |
| Counters, Meters, Registers (direct and indirect) | ready |
| PacketIn / PacketOut with metadata encode/decode | ready |
| Digest subscribe and ack | ready |
| Reference CLI (`p4ctl`) | ready |
| Prometheus adapter | planned |
| OpenTelemetry gRPC interceptors | planned |

## P4Runtime Compatibility

| Controller version | P4Runtime spec |
| --- | --- |
| `v1.x` | 1.3.0+ |

## Documentation

- [`ARCHITECTURE.md`](ARCHITECTURE.md) — layered design and data-flow.
- [`DESIGN_NOTES.md`](DESIGN_NOTES.md) — architectural decisions and open
  questions.
- [`docs/quickstart.md`](docs/quickstart.md) — run your first controller.
- [`docs/troubleshooting.md`](docs/troubleshooting.md) — common issues.
- [`docs/glossary.md`](docs/glossary.md) — P4, P4Runtime, PDPI, pipeline, etc.
- [`docs/i18n/README.zh-CN.md`](docs/i18n/README.zh-CN.md) — 中文版本.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Please read the
[Code of Conduct](CODE_OF_CONDUCT.md) before opening a pull request.

## Security

To report a vulnerability, follow the instructions in [SECURITY.md](SECURITY.md).
Do not open a public issue for anything that could affect deployed controllers.

## License

Licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE)
for third-party attribution.
