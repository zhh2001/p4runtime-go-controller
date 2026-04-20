# Contributing

Contributions are welcome. This document describes the workflow.

## Prerequisites

- Go 1.25 or newer (matches the `go` directive in `go.mod`; the `toolchain` directive auto-downloads `go1.25.3` when needed).
- `golangci-lint`, `govulncheck`, `shellcheck` for lint gates.
- Docker for the optional BMv2 integration tests.

## Local Setup

```sh
git clone https://github.com/zhh2001/p4runtime-go-controller.git
cd p4runtime-go-controller
make tidy
make lint
make test
```

## Workflow

1. Open an issue before starting non-trivial work so the design can be agreed
   on first.
2. Create a branch named `feature/<topic>` or `fix/<topic>` from `main`.
3. Commit using [Conventional Commits](https://www.conventionalcommits.org/)
   (`feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`, `build`, `perf`).
4. Sign each commit (`git commit -s`) — the project uses the
   [Developer Certificate of Origin](https://developercertificate.org/).
5. Open a pull request against `main`. Rebase before requesting review. We
   squash-merge to keep history linear.

## Coding Standards

- `gofmt`, `goimports`, and `golangci-lint run` must be clean. CI enforces this.
- Every exported symbol has a godoc comment beginning with the symbol name.
- Every blocking exported function takes `ctx context.Context` first.
- No new package-level mutable state. No side effects inside `init`.
- New tests use `testify/require` for fatal assertions and `testify/assert`
  for soft ones.

## Pull Request Checklist

- [ ] `make lint test` passes locally.
- [ ] New behavior has tests; public API changes add an entry under
      `[Unreleased]` in `CHANGELOG.md`.
- [ ] Exported symbols have godoc; major additions ship with an `Example*`
      function.
- [ ] Commits are signed and follow Conventional Commits.

See the [Code of Conduct](CODE_OF_CONDUCT.md) for expected behavior in all
project spaces.
