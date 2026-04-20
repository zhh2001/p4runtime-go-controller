# Security Policy

## Supported Versions

During the `v0.x` development line, only the latest minor release is supported
for security fixes. Once the project reaches `v1.0.0`, the two most recent
minor releases will receive patches.

| Version | Supported |
| --- | --- |
| latest `v0.x` | yes |
| older `v0.x` | no |

## Reporting a Vulnerability

Please do not file public issues for security-relevant bugs. Use one of the
following private channels instead:

- GitHub Security Advisories: open a private advisory from the repository's
  **Security** tab. This is the preferred channel.
- Email: send a report to the addresses listed in
  [MAINTAINERS.md](MAINTAINERS.md). Encrypt with the maintainer's PGP key when
  one is published.

A maintainer will acknowledge the report within five business days, work with
the reporter on a coordinated disclosure timeline, and publish a patched
release before the public advisory.

## Handling Process

1. The maintainer confirms the report and opens a private fix branch.
2. A regression test is added alongside the fix.
3. A patched version is tagged and released.
4. A public advisory is published once the patched release is available.

## Scope

The policy covers the library and the `p4ctl` reference CLI. Example programs
under `examples/` are illustrative only and are not considered
production-grade.
