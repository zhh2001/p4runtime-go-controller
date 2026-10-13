import { defineConfig } from 'vitepress'
import { existsSync, statSync } from 'node:fs'
import { cp } from 'node:fs/promises'
import { dirname, posix, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { base, pages, repository, version } from './site.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const sources = Object.fromEntries(Object.entries(pages).map(([source, page]) => [page, source]))

function repositoryLinks(md) {
  md.core.ruler.after('inline', 'repository-links', (state) => {
    const source = sources[state.env.relativePath]
    if (!source) throw new Error(`Unknown documentation page: ${state.env.relativePath}`)
    function rewrite(value) {
      if (!value || /^(?:[a-z][a-z\d+.-]*:|\/\/|\/)/i.test(value)) return value
      // VitePress prefixes numeric heading anchors with an underscore.
      if (value.startsWith('#')) return value.replace(/^#(\d)/, '#_$1')
      const [, pathname, suffix] = value.match(/^([^?#]*)(.*)$/)
      const target = posix.normalize(posix.join(posix.dirname(source), decodeURI(pathname)))
      const page = pages[target] || pages[posix.join(target, 'README.md')]
      if (page) return '/' + page + suffix.replace(/#(\d)/, '#_$1')
      const location = resolve(root, target)
      if (!existsSync(location)) throw new Error(`Missing link in ${source}: ${value}`)
      const kind = statSync(location).isDirectory() ? 'tree' : 'blob'
      return `${repository}/${kind}/main/${target}${suffix}`
    }
    function visit(tokens) {
      for (const token of tokens) {
        if (token.type === 'link_open') token.attrSet('href', rewrite(token.attrGet('href')))
        if (token.type === 'image') token.attrSet('src', rewrite(token.attrGet('src')))
        if (token.children) visit(token.children)
      }
    }
    visit(state.tokens)
  })
}

function sourceUpdates() {
  return {
    name: 'documentation-source-updates',
    apply: 'serve',
    configureServer(server) {
      const files = new Map(Object.entries(pages).map(([source, page]) => [
        resolve(root, source), resolve(root, 'docs/.vitepress/site', page),
      ]))
      server.watcher.add([...files.keys()])
      server.watcher.on('change', async (file) => {
        const destination = files.get(file)
        if (destination) await cp(file, destination)
      })
    },
  }
}

const sidebar = [
  { text: 'Start here', items: [
    { text: 'Overview', link: '/overview' },
    { text: 'Quickstart', link: '/quickstart' },
    { text: 'Target support', link: '/target-support' },
    { text: 'BMv2 setup', link: '/bmv2' },
  ] },
  { text: 'Examples', items: [
    { text: 'Examples overview', link: '/examples/' },
    { text: 'Connect', link: '/examples/connect' },
    { text: 'L2 forwarding', link: '/examples/l2-forwarding' },
    { text: 'Packet I/O', link: '/examples/packetio' },
    { text: 'Counters', link: '/examples/counters' },
    { text: 'L2 P4 program and interfaces', link: '/examples/l2-fixture' },
  ] },
  { text: 'SDK and CLI', items: [
    { text: 'API reference', link: '/api' },
    { text: 'p4ctl', link: '/cli' },
    { text: 'Pipeline ownership', link: '/guide/pipeline' },
    { text: 'Counters', link: '/guide/counters' },
    { text: 'Meters', link: '/guide/meters' },
    { text: 'Registers', link: '/guide/registers' },
    { text: 'Packet replication', link: '/guide/replication' },
  ] },
  { text: 'Reference', items: [
    { text: 'Troubleshooting', link: '/troubleshooting' },
    { text: 'Observability', link: '/observability' },
    { text: 'Performance', link: '/performance' },
    { text: 'Architecture', link: '/architecture' },
    { text: 'Glossary', link: '/glossary' },
    { text: 'Integration tests', link: '/integration' },
  ] },
  { text: 'Project', items: [
    { text: 'Migrating from v1', link: '/migration-v2' },
    { text: 'Releases', link: '/releases' },
    { text: 'Changelog', link: '/changelog' },
    { text: 'Contributing', link: '/contributing' },
    { text: 'Support', link: '/support' },
    { text: 'Security', link: '/security' },
  ] },
]

export default defineConfig({
  title: 'p4runtime-go-controller',
  description: 'Go SDK and p4ctl CLI for P4Runtime controllers, with BMv2 examples.',
  base,
  srcDir: '.vitepress/site',
  cleanUrls: false,
  sitemap: { hostname: `https://zhh2001.github.io${base}` },
  transformPageData(page) {
    page.frontmatter.sourceURL = `${repository}/edit/main/${sources[page.relativePath]}`
  },
  locales: {
    root: { label: 'English', lang: 'en', link: '/' },
    zh: { label: '简体中文', lang: 'zh-CN', link: '/zh/', title: 'p4runtime-go-controller',
      description: '用于编写 P4Runtime 控制器的 Go SDK 和 p4ctl CLI，提供 BMv2 示例。',
      themeConfig: {
        nav: [
          { text: '快速上手', link: '/zh/quickstart' },
          { text: '项目介绍', link: '/zh/overview' },
          { text: '完整文档（英文）', link: '/quickstart' },
          { text: version, link: '/release-notes/v2.0.1' },
        ],
        sidebar: [
          { text: '入门', items: [
            { text: '首页', link: '/zh/' },
            { text: '快速上手', link: '/zh/quickstart' },
            { text: '项目介绍', link: '/zh/overview' },
          ] },
          { text: '英文文档', items: [
            { text: '目标支持', link: '/target-support' },
            { text: '示例', link: '/examples/' },
            { text: 'CLI', link: '/cli' },
            { text: 'API 参考', link: '/api' },
            { text: '排错', link: '/troubleshooting' },
            { text: 'v2 升级', link: '/migration-v2' },
          ] },
        ],
        outlineTitle: '本页目录',
        docFooter: { prev: '上一页', next: '下一页' },
        returnToTopLabel: '返回顶部',
        sidebarMenuLabel: '目录',
        darkModeSwitchLabel: '切换主题',
        editLink: { pattern: ({ frontmatter }) => frontmatter.sourceURL, text: '在 GitHub 上编辑此页' },
        footer: { message: 'Apache License 2.0', copyright: 'Copyright © 2026 The p4runtime-go-controller Authors' },
      },
    },
  },
  markdown: { config: repositoryLinks },
  vite: { plugins: [sourceUpdates()] },
  themeConfig: {
    i18nRouting: false,
    nav: [
      { text: 'Guide', link: '/quickstart' },
      { text: 'Examples', link: '/examples/' },
      { text: 'API', link: '/api' },
      { text: 'CLI', link: '/cli' },
      { text: version, link: '/release-notes/v2.0.1' },
    ],
    sidebar: { '/zh/': [], '/': sidebar },
    socialLinks: [{ icon: 'github', link: repository }],
    editLink: { pattern: ({ frontmatter }) => frontmatter.sourceURL, text: 'Edit this page on GitHub' },
    search: { provider: 'local', options: { locales: { zh: { translations: {
      button: { buttonText: '搜索', buttonAriaLabel: '搜索文档' },
      modal: { noResultsText: '没有找到结果', resetButtonTitle: '清除搜索',
        displayDetails: '显示详细内容', backButtonTitle: '关闭搜索',
        footer: { selectText: '选择', navigateText: '切换', closeText: '关闭' } },
    } } } } },
    outline: [2, 3],
    footer: { message: 'Released under the Apache License 2.0.', copyright: 'Copyright © 2026 The p4runtime-go-controller Authors' },
  },
})
