import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { dirname, extname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { base, pages } from './site.mjs'

const output = resolve(dirname(fileURLToPath(import.meta.url)), 'dist')
async function files(directory) {
  const entries = await readdir(directory, { withFileTypes: true })
  const nested = await Promise.all(entries.map((entry) => {
    const path = resolve(directory, entry.name)
    return entry.isDirectory() ? files(path) : [path]
  }))
  return nested.flat()
}
function decode(value) {
  return value.replace(/&(?:amp|quot|apos|lt|gt);/g, (entity) => ({
    '&amp;': '&', '&quot;': '"', '&apos;': "'", '&lt;': '<', '&gt;': '>',
  })[entity])
}

const all = new Set(await files(output))
const html = new Map()
for (const path of all) {
  if (extname(path) !== '.html') continue
  const content = await readFile(path, 'utf8')
  const ids = new Set([...content.matchAll(/\bid="([^"]*)"/g)].map((match) => decode(match[1])))
  html.set(path, { content, ids })
}
for (const page of Object.values(pages)) {
  assert(html.has(resolve(output, page.replace(/\.md$/, '.html'))), `Missing page: ${page}`)
}

let checked = 0
for (const [path, { content }] of html) {
  const relative = path.slice(output.length + 1).split('\\').join('/')
  const current = new URL(base + relative, 'https://docs.example')
  for (const match of content.matchAll(/\b(?:href|src)="([^"]*)"/g)) {
    const value = decode(match[1])
    if (!value || /^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(value)) continue
    const target = new URL(value, current)
    assert(target.pathname.startsWith(base), `${relative}: link outside project base: ${value}`)
    let name = decodeURIComponent(target.pathname.slice(base.length))
    if (!name || name.endsWith('/')) name += 'index.html'
    if (!extname(name)) name += '.html'
    const destination = resolve(output, name)
    assert(all.has(destination), `${relative}: missing target: ${value}`)
    if (target.hash && html.has(destination)) {
      const id = decodeURIComponent(target.hash.slice(1))
      assert(html.get(destination).ids.has(id), `${relative}: missing anchor: ${value}`)
    }
    checked++
  }
}
console.log(`Checked ${html.size} HTML pages and ${checked} internal links, anchors and assets`)
