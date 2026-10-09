# Releases

## Version policy

Published versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and the [Go 1 source compatibility policy](https://go.dev/doc/go1compat). Public API additions belong in a minor version when existing supported code remains compatible. Incompatible public API changes require a new major version.

The current development branch is preparing the next major release. `pre.Replica` now contains slices, so existing comparisons and map keys using that type no longer compile. The branch also contains the behavior changes listed under [Unreleased](../CHANGELOG.md). It must not be released as another v1 version.

The module path is still `github.com/zhh2001/p4runtime-go-controller`. Before publishing v2, migrate it to `github.com/zhh2001/p4runtime-go-controller/v2`, update internal imports and external examples, and verify that consumers can import the new module. Go requires the major-version suffix for v2 and later. See [Go modules: v2 and beyond](https://go.dev/blog/v2-go-modules). This migration is a separate change.

Use a published v1 tag when an application needs the existing v1 API. `@latest` selects a published module version and does not install the current development branch.

## Release preparation

Before creating a release tag:

1. Choose a version that matches the public API changes and the module path. Complete the v2 migration before tagging the current branch.
2. Move the relevant Unreleased notes into a dated version section in CHANGELOG. Include migration instructions for incompatible changes.
3. Complete the build, vet, race, lint and module checks in [CONTRIBUTING](../CONTRIBUTING.md), including applicable BMv2 tests.
4. Validate the release configuration and build a local snapshot. Check every configured platform, the version output, archives, SBOMs and checksums.

Local release validation needs Go 1.26 or newer, GoReleaser v2 and Syft on PATH. Run it from a disposable checkout because the release hook runs `go mod tidy` and the build writes to `dist`:

```sh
goreleaser check
goreleaser release --snapshot --skip=publish,sign --clean
```

The snapshot command builds local artifacts without publishing a release. Its version label is based on the latest tag and does not select the next release version. It does not validate GitHub OIDC signing. The tag workflow installs Cosign for that step.

## Tagged release workflow

The [release workflow](../.github/workflows/release.yml) runs for tags matching `v*`. GoReleaser builds `p4ctl` for Linux and macOS on amd64 and arm64, plus Windows amd64. Linux and macOS archives use tar.gz. Windows uses zip. Each archive includes LICENSE, NOTICE and README.

The release configuration creates an SPDX JSON SBOM for each archive and a SHA-256 checksum file covering the archives and SBOMs. Cosign signs the checksum file using the workflow's GitHub OIDC identity. GoReleaser creates a draft GitHub release, and prerelease tags are marked as prereleases. See [.goreleaser.yaml](../.goreleaser.yaml).

Release notes group `feat`, `fix` and `docs` subjects with or without a scope. Subjects carrying the Conventional Commits `!` marker stay in their corresponding group. Review the Unreleased compatibility notes when choosing the version, since grouping does not decide the version number.

Review the draft assets and release notes before publishing it. A local snapshot does not verify upload permissions, OIDC credentials or the final GitHub release.
