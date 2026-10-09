# Security Policy

## Supported Versions

Security updates cover the two most recent published minor version lines within the latest published major version. If that major has only one published minor line, only that line is supported.

Published prereleases count toward both the major version and the minor lines. The first published v2 prerelease ends security maintenance for v1. Unreleased source branches and draft GitHub releases do not change the support window. See [published releases](https://github.com/zhh2001/p4runtime-go-controller/releases).

For example, when v1.1 is the latest published minor line, v1.1.x and v1.0.x receive security updates. After the first v2.0 prerelease is published, v2.0 is the supported line and v1 is outside the support window. Once v2.1 is published, v2.1 and v2.0 are supported.

| Version                                         | Supported                     |
| ----------------------------------------------- | ----------------------------- |
| Latest minor line in the latest published major | yes                           |
| Previous published minor line in that major     | yes, when available           |
| Older minor lines or earlier major versions     | no                            |
| Unreleased development branches                 | no published security patches |

## Reporting a Vulnerability

Please do not file public issues for security-relevant bugs. Use one of the following private channels instead:

- GitHub Security Advisories: open a private advisory from the repository's **Security** tab. This is the preferred channel.
- Email: send a report to the addresses listed in [MAINTAINERS.md](MAINTAINERS.md). Encrypt with the maintainer's PGP key when one is published.

A maintainer will acknowledge the report within five business days, work with the reporter on a coordinated disclosure timeline, and publish a patched release before the public advisory.

## Handling Process

1. The maintainer confirms the report and opens a private fix branch.
2. A regression test is added alongside the fix.
3. A patched version is tagged and released.
4. A public advisory is published once the patched release is available.

## Scope

The policy covers the library and the `p4ctl` reference CLI. Example programs under `examples/` are illustrative only and are not considered production-grade.
