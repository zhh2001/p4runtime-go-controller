---
layout: home
hero:
  name: p4runtime-go-controller
  text: P4Runtime controllers in Go
  tagline: A Go SDK and p4ctl CLI, with runnable BMv2 examples.
  actions:
    - theme: brand
      text: Get started
      link: /quickstart
    - theme: alt
      text: Examples
      link: /examples/
    - theme: alt
      text: GitHub
      link: https://github.com/zhh2001/p4runtime-go-controller
features:
  - title: Go SDK
    details: Build table entries, manage pipelines and handle Packet I/O through named P4Info resources.
  - title: p4ctl CLI
    details: Connect to a target, install a pipeline and inspect tables and counters from the terminal.
  - title: BMv2 examples
    details: Follow connection, L2 forwarding, Packet I/O and counter examples using the bundled P4 program.
---

## Start with a local target

Use Go 1.26 or newer. The [quickstart](quickstart.md) covers installing v2.0.1, starting BMv2 and writing your first controller. The [examples](examples.md) show individual operations, and the [CLI guide](../cmd/p4ctl/README.md) covers terminal workflows.

## Protocol and target support

The SDK's protocol baseline is P4Runtime 1.3.0. Optional resources and newer fields depend on the target. BMv2 is used for integration testing. See [target support](target-support.md) for the tested paths and target-specific limits.
