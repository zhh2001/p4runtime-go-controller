# Releases

## Version policy

Published versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html) and the [Go 1 source compatibility policy](https://go.dev/doc/go1compat). Public API additions belong in a minor version when existing supported code remains compatible. Incompatible public API changes require a new major version.

The current development branch develops v2. `pre.Replica` now contains slices, so existing comparisons and map keys using that type no longer compile. The branch also contains the behavior changes listed in [CHANGELOG](../CHANGELOG.md). Release tags for this branch must be in the v2 series.

The module path is `github.com/zhh2001/p4runtime-go-controller/v2`, declared at the repository root. SDK imports, tests and examples use the `/v2` prefix. Published v1 tags retain `github.com/zhh2001/p4runtime-go-controller`. Go treats them as separate modules, so an application can use both versions with distinct import aliases. See [Go modules: v2 and beyond](https://go.dev/blog/v2-go-modules).

Use a published v1 tag when an application needs the existing v1 API. `@latest` selects a published version of the requested module path. Before the first v2 release, follow the [local checkout instructions](quickstart.md#1-install).

Applications upgrading from v1.1.1 should follow the [v2 migration guide](migration-v2.md), including source changes, behavior checks and CLI automation.

## Planned prerelease

The first v2 release candidate, `v2.0.0-rc.1`, is scheduled for 2026-10-09. Its [release notes](release-notes/v2.0.0-rc.1.md) cover compatibility, SDK and CLI changes, installation and target support. CHANGELOG contains the dated `2.0.0-rc.1` section and retains Unreleased for later changes.

Commit the prepared files before creating the tag. Review the draft assets and use the prepared notes as the public release body. The first published v2 prerelease also changes the security maintenance window described below.

## Release preparation

Before creating a release tag:

1. Choose a v2 version that matches the public API changes. Check that `go.mod`, imports and external consumers use the `/v2` module path.
2. Move the relevant Unreleased notes into a dated version section in CHANGELOG. Include migration instructions for incompatible changes and check the [security support policy](../SECURITY.md#supported-versions). Publishing a prerelease in a new major version also advances the security support window.
3. Complete the build, vet, race, lint and module checks in [CONTRIBUTING](../CONTRIBUTING.md), including applicable BMv2 tests. Verify the [migration guide](migration-v2.md) with an external application and check its documented CLI commands.
4. Validate the release configuration and build a local snapshot. Check every configured platform, the version output, archives, SBOMs and checksums.

Local release validation needs Go 1.26 or newer, GoReleaser v2 and Syft on PATH. Run it from a disposable checkout because the release hook runs `go mod tidy` and the build writes to `dist`:

```sh
goreleaser check
goreleaser release --snapshot --skip=publish,sign --clean
```

The snapshot command builds local artifacts without publishing a release. Its version label is `2.0.0-SNAPSHOT-<short commit>`, including when the latest existing tag is v1. This label does not publish or reserve a release version. The snapshot does not validate GitHub OIDC signing. The tag workflow installs Cosign for that step.

## Local candidate rehearsal

To check the selected candidate's archive names and CLI version before its tag exists, run this command in a disposable checkout:

```sh
GORELEASER_CURRENT_TAG=v2.0.0-rc.1 \
GORELEASER_PREVIOUS_TAG=v1.1.1 \
goreleaser release --clean --skip=validate,publish,sign \
    --release-notes docs/release-notes/v2.0.0-rc.1.md
```

The environment selects the candidate version locally, and the notes file replaces generated commit notes. The command creates local artifacts without creating a tag or uploading a release. It skips tag validation and signing to rehearse an unpublished version. Run the regular preparation checks before creating the real tag. Upload permissions and GitHub OIDC signing are checked by the tagged workflow.

## Tagged release workflow

The [release workflow](../.github/workflows/release.yml) runs for tags matching `v2.*`. GoReleaser builds `p4ctl` for Linux and macOS on amd64 and arm64, plus Windows amd64. Linux and macOS archives use tar.gz. Windows uses zip. Each archive includes LICENSE, NOTICE and README.

The release configuration creates an SPDX JSON SBOM for each archive and a SHA-256 checksum file covering the archives and SBOMs. Cosign signs the checksum file using the workflow's GitHub OIDC identity. GoReleaser creates a draft GitHub release, and prerelease tags are marked as prereleases. See [.goreleaser.yaml](../.goreleaser.yaml).

The SDK version is selected by its Git tag. A public tag can be fetched by Go or its module proxies while the GitHub release is still a draft. Complete the SDK and migration checks before making the tag public. See [Publishing a Go module](https://go.dev/doc/modules/publishing).

Release notes group `feat`, `fix` and `docs` subjects with or without a scope. Subjects carrying the Conventional Commits `!` marker stay in their corresponding group. Review the compatibility notes in CHANGELOG when choosing the version, since grouping does not decide the version number.

Review the draft assets and release notes before publishing it. A local snapshot does not verify upload permissions, OIDC credentials or the final GitHub release.

## Using CLI release archives

Choose the archive matching the operating system and architecture where `p4ctl` will run. Verify its SHA-256 digest against the signed checksum file before extracting it. The corresponding SPDX file describes the archive's dependencies.

Each archive contains the executable, LICENSE, NOTICE and README. It does not contain the Go SDK, P4 fixtures or BMv2 scripts. Use a repository checkout for those workflows and the `/v2` module for application dependencies. README links refer to documentation in the repository.

On Linux or macOS, run these commands from the extracted directory:

```sh
./p4ctl version
go version -m ./p4ctl
```

On Windows, the executable is `p4ctl.exe`. The version output identifies the release version, short commit and build date. Go build information identifies the `/v2` main module and platform. The `go version -m` check requires an installed Go toolchain, while the release binary itself does not.

Installing with `go install .../v2/cmd/p4ctl@<tag>` builds from source and does not apply GoReleaser's version flags. That binary normally reports `dev`, even when its Go build information identifies a published tag. Use the archive when you need an executable with release version metadata.
