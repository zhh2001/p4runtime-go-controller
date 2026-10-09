# Contributing

Contributions are welcome. This document describes the workflow.

## Prerequisites

- Go 1.25 or newer. `go.mod` declares the minimum version and preferred toolchain.
- Git and Make for the commands below.
- `golangci-lint` v2.14.0, `govulncheck`, and `shellcheck` for lint gates. Install the linter from the [official releases](https://github.com/golangci/golangci-lint/releases/tag/v2.14.0).

Unit tests and tests against controlled gRPC servers run with the Go toolchain. The optional BMv2 tests also need Bash, `p4c-bm2-ss` and a running target. Native mode uses local `simple_switch_grpc`. Docker mode uses an accessible Docker daemon and an image containing `simple_switch_grpc`, while compilation still uses local p4c. See [script requirements](scripts/README.md#requirements).

## Local Setup

```sh
git clone https://github.com/zhh2001/p4runtime-go-controller.git
cd p4runtime-go-controller
make tidy
make lint
make test
```

`make lint` uses Go 1.27.1, matching the lint job in CI. Go downloads that toolchain when needed. Update the Go and linter versions together when changing the lint configuration.

## Go toolchain checks

The CI build matrix runs Go 1.25 and `stable` on Ubuntu, macOS and Windows. The `1.25` selector chooses a patch release in that series, which can be newer than the minimum `1.25.0` declared in `go.mod`. `stable` follows the latest stable release.

`actions/setup-go@v6` sets `GOTOOLCHAIN=local`. Each CI job uses the installed Go toolchain without switching to the preferred toolchain in `go.mod`. The setup step prints the actual version and Go environment. Check those logs when comparing results across runs. See [Go toolchain selection](https://go.dev/doc/toolchain#go-toolchain-selection).

To check the declared minimum locally, use an explicit toolchain name:

```sh
GOTOOLCHAIN=go1.25.0 go build ./...
GOTOOLCHAIN=go1.25.0 go vet ./...
GOTOOLCHAIN=go1.25.0 go test -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
```

Replace `go1.25.0` with the version shown in a CI run to reproduce that run's toolchain. A bare `go` command can switch versions because of the preferred toolchain. Adding `+auto` to an explicit name also allows switching.

## BMv2 integration tests

From the repository root, start a native target in one terminal:

```sh
./scripts/run-bmv2.sh
```

In another terminal, run:

```sh
make e2e
```

The launcher and test script each compile a matching L2 P4Info and device config. The test script runs with race detection, which requires CGO and a C compiler supported by the Go toolchain. Use a dedicated target because tests install their pipeline. Docker startup, custom ports and compiler overrides are described in [the script guide](scripts/README.md). Linux Ethernet tests also need bound data interfaces and raw socket permissions. See [the integration guide](test/integration/README.md) for those tests and their settings.

## Workflow

1. Open an issue before starting non-trivial work so the design can be agreed on first.
2. Create a branch named `feature/<topic>` or `fix/<topic>` from `main`.
3. Commit using [Conventional Commits](https://www.conventionalcommits.org/) (`feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`, `build`, `perf`).
4. Sign each commit (`git commit -s`) — the project uses the [Developer Certificate of Origin](https://developercertificate.org/).
5. Open a pull request against `main`. Rebase before requesting review. We squash-merge to keep history linear.

## Coding Standards

- `gofmt`, `goimports`, and `golangci-lint run` must be clean. CI enforces this.
- Every exported symbol has a godoc comment beginning with the symbol name.
- Every blocking exported function takes `ctx context.Context` first.
- No new package-level mutable state. No side effects inside `init`.
- New tests use `testify/require` for fatal assertions and `testify/assert` for soft ones.

## Pull Request Checklist

- [ ] `make lint test` passes locally.
- [ ] New behavior has tests; public API changes add an entry under `[Unreleased]` in `CHANGELOG.md`.
- [ ] Exported symbols have godoc; major additions ship with an `Example*` function.
- [ ] Commits are signed and follow Conventional Commits.

See the [Code of Conduct](CODE_OF_CONDUCT.md) for expected behavior in all project spaces.
