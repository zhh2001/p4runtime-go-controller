# Releases

## Version policy

Published versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and the [Go 1 source compatibility policy](https://go.dev/doc/go1compat). Public API additions belong in a minor version when existing supported code remains compatible. Incompatible public API changes require a new major version.

The current development branch develops v2. `pre.Replica` now contains slices, so existing comparisons and map keys using that type no longer compile. The branch also contains the behavior changes listed under [Unreleased](../CHANGELOG.md). Release tags for this branch must be in the v2 series.

The module path is `github.com/zhh2001/p4runtime-go-controller/v2`, declared at the repository root. SDK imports, tests and examples use the `/v2` prefix. Published v1 tags retain `github.com/zhh2001/p4runtime-go-controller`. Go treats them as separate modules, so an application can use both versions with distinct import aliases. See [Go modules: v2 and beyond](https://go.dev/blog/v2-go-modules).

Use a published v1 tag when an application needs the existing v1 API. `@latest` selects a published version of the requested module path. Before the first v2 release, follow the [local checkout instructions](quickstart.md#1-install).

## Release preparation

Before creating a release tag:

1. Choose a v2 version that matches the public API changes. Check that `go.mod`, imports and external consumers use the `/v2` module path.
2. Move the relevant Unreleased notes into a dated version section in CHANGELOG. Include migration instructions for incompatible changes.
3. Complete the build, vet, race, lint and module checks in [CONTRIBUTING](../CONTRIBUTING.md), including applicable BMv2 tests.
4. Validate the release configuration and build a local snapshot. Check every configured platform, the version output, archives, SBOMs and checksums.

Local release validation needs Go 1.26 or newer, GoReleaser v2 and Syft on PATH. Run it from a disposable checkout because the release hook runs `go mod tidy` and the build writes to `dist`:

```sh
goreleaser check
goreleaser release --snapshot --skip=publish,sign --clean
```

The snapshot command builds local artifacts without publishing a release. Its version label is `2.0.0-SNAPSHOT-<short commit>`, including when the latest existing tag is v1. This label does not publish or reserve a release version. The snapshot does not validate GitHub OIDC signing. The tag workflow installs Cosign for that step.

## Tagged release workflow

The [release workflow](../.github/workflows/release.yml) runs for tags matching `v2.*`. GoReleaser builds `p4ctl` for Linux and macOS on amd64 and arm64, plus Windows amd64. Linux and macOS archives use tar.gz. Windows uses zip. Each archive includes LICENSE, NOTICE and README.

The release configuration creates an SPDX JSON SBOM for each archive and a SHA-256 checksum file covering the archives and SBOMs. Cosign signs the checksum file using the workflow's GitHub OIDC identity. GoReleaser creates a draft GitHub release, and prerelease tags are marked as prereleases. See [.goreleaser.yaml](../.goreleaser.yaml).

Release notes group `feat`, `fix` and `docs` subjects with or without a scope. Subjects carrying the Conventional Commits `!` marker stay in their corresponding group. Review the Unreleased compatibility notes when choosing the version, since grouping does not decide the version number.

Review the draft assets and release notes before publishing it. A local snapshot does not verify upload permissions, OIDC credentials or the final GitHub release.
