import { siteURL, version } from './site.mjs'

const descriptions = {
  'index.md': 'Build P4Runtime controllers with a Go SDK and p4ctl CLI. Start with runnable BMv2 examples for tables, Packet I/O and counters.',
  'overview.md': 'Explore the Go SDK and p4ctl CLI, their resource APIs, installation steps and P4Runtime compatibility.',
  'quickstart.md': 'Install the Go SDK and p4ctl, start BMv2 and write your first P4Runtime table entry with the bundled L2 program.',
  'examples/index.md': 'Run four Go examples for P4Runtime connections, L2 forwarding, Packet I/O and indirect counter reads on BMv2.',
  'examples/connect.md': 'Connect to a P4Runtime target, participate in primary election and inspect the installed pipeline with a Go example.',
  'examples/l2-forwarding.md': 'Install the bundled L2 pipeline and write a destination MAC entry that forwards matching Ethernet frames to port 1.',
  'examples/packetio.md': 'Receive PacketIn messages and send an Ethernet frame with PacketOut metadata using the Go SDK and L2 pipeline.',
  'examples/counters.md': 'Read the L2 program\'s indirect counter by egress port and inspect packet and byte counts after forwarding traffic.',
  'examples/l2-fixture.md': 'Compile the L2 P4 program, inspect its resources and bind BMv2 data interfaces for forwarding and CPU packet tests.',
  'cli.md': 'Use p4ctl to connect, install pipelines, manage table entries, send packets and read counters, with TLS and JSON or YAML output.',
  'api.md': `Browse the Go API reference for the published ${version} SDK, including client, pipeline, tableentry, packetio and resource packages.`,
  'guide/pipeline.md': 'Understand how Pipeline owns P4Info and device configuration, copies query results and identifies resources by name or ID.',
  'guide/counters.md': 'Read and write indirect counter arrays by P4Info name, with index validation and packet or byte unit checks.',
  'guide/meters.md': 'Read, configure and reset indirect meter arrays, with P4Info meter types, rates, bursts and index validation.',
  'guide/registers.md': 'Read register arrays and write integer or structured P4Data values with type, bit width and index validation.',
  'guide/replication.md': 'Manage multicast groups and clone sessions, including legacy ports, byte-based ports and ordered backup replicas.',
  'target-support.md': 'Review the P4Runtime 1.3.0 baseline, tested BMv2 paths and target requirements for optional resources and newer fields.',
  'bmv2.md': 'Compile the L2 program and start simple_switch_grpc locally or in Docker using the repository\'s BMv2 scripts.',
  'integration.md': 'Run BMv2 integration tests for P4Runtime resources, CLI workflows, packet traffic and the bundled Go examples.',
  'troubleshooting.md': 'Diagnose P4Runtime arbitration, TLS, pipeline installation, write errors, packet metadata and stream shutdown.',
  'observability.md': 'Configure log/slog for connection, arbitration, retry and pipeline logs, and review the planned metrics and tracing scope.',
  'performance.md': 'Reanalyze BMv2 RPC comparison data, run the pinned controller workloads and review SDK microbenchmarks, encoding and stream callbacks.',
  'architecture.md': 'Explore SDK layering, connection and stream supervision, dispatch, pipeline fallback, encoding and error handling.',
  'glossary.md': 'Look up P4 and P4Runtime terms, including P4Info, table matches, meters, digests, StreamChannel and canonical bytes.',
  'migration-v2.md': 'Migrate from v1.1.1 to the /v2 module, update Replica usage and check SDK behavior changes and CLI automation.',
  'releases.md': 'Follow version policy, release preparation, local rehearsals and the tagged workflow for SDK and p4ctl releases.',
  'release-notes/v2.0.1.md': 'Read v2.0.1 release notes for the Apache License 2.0 text update and SDK or CLI installation.',
  'release-notes/v2.0.0.md': 'Read the first stable v2 release notes, covering compatibility, resource APIs, CLI changes and target support.',
  'release-notes/v2.0.0-rc.1.md': 'Read the first v2 release candidate notes, including the /v2 module path, resource APIs and compatibility changes.',
  'changelog.md': 'Review published release history and upcoming changes for the Go SDK and p4ctl CLI.',
  'contributing.md': 'Set up the Go toolchain, run tests and lint, build the documentation site and follow the contribution workflow.',
  'support.md': 'Find where to ask usage questions, report issues and request features for p4runtime-go-controller.',
  'security.md': 'Review supported version lines and the process for privately reporting and handling security vulnerabilities.',
  'code-of-conduct.md': 'Read the project\'s Contributor Covenant scope and reporting process for community conduct concerns.',
  'maintainers.md': 'Find project maintainer contacts and the process for joining or leaving the maintainer team.',
  'zh/index.md': '使用 Go SDK 和 p4ctl CLI 编写 P4Runtime 控制器，通过 BMv2 示例学习表项、Packet I/O 和计数器操作。',
  'zh/overview.md': '了解 Go SDK 和 p4ctl CLI 的安装方式、资源 API、功能范围及 P4Runtime 版本兼容情况。',
  'zh/quickstart.md': '安装 Go SDK 和 p4ctl，启动 BMv2，使用 L2 P4 程序写入表项、收发 CPU 数据包并读取计数器。',
}

const titles = {
  'index.md': 'P4Runtime controllers in Go',
  'overview.md': 'Project overview',
  'examples/l2-fixture.md': 'L2 P4 program and interfaces',
  'zh/index.md': '用 Go 编写 P4Runtime 控制器',
  'zh/overview.md': '项目介绍',
}

export function pageURL(path) {
  return new URL(path.replace(/(^|\/)index\.md$/, '$1').replace(/\.md$/, '.html'), siteURL).href
}

export function applyMetadata(page) {
  const frontmatter = page.frontmatter
  const head = frontmatter.head ||= []
  const description = frontmatter.description || descriptions[page.relativePath]
  if (!description) throw new Error(`Missing page description: ${page.relativePath}`)
  page.description = frontmatter.description = description
  if (titles[page.relativePath] && !frontmatter.title) {
    page.title = frontmatter.title = titles[page.relativePath]
  }
  const title = `${page.title} | p4runtime-go-controller`
  const url = pageURL(page.relativePath)
  const image = new URL('social-card.png', siteURL).href
  const alt = 'p4runtime-go-controller: Go SDK, p4ctl CLI and BMv2 examples'
  head.push(
    ['link', { rel: 'canonical', href: url }],
    ...Object.entries({
      'og:type': 'website',
      'og:site_name': 'p4runtime-go-controller',
      'og:title': title,
      'og:description': description,
      'og:url': url,
      'og:locale': page.relativePath.startsWith('zh/') ? 'zh_CN' : 'en_US',
      'og:image': image,
      'og:image:type': 'image/png',
      'og:image:width': '1200',
      'og:image:height': '630',
      'og:image:alt': alt,
    }).map(([property, content]) => ['meta', { property, content }]),
    ...Object.entries({
      'twitter:card': 'summary_large_image',
      'twitter:title': title,
      'twitter:description': description,
      'twitter:image': image,
      'twitter:image:alt': alt,
    }).map(([name, content]) => ['meta', { name, content }]),
  )
}
