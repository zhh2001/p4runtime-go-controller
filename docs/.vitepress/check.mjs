import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import { dirname, extname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { base, pages, siteURL } from './site.mjs'

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
  return value.replace(/&(?:amp|quot|apos|lt|gt|#[xX][\da-fA-F]+|#\d+);/g, (entity) => {
    if (entity.startsWith('&#')) {
      const code = entity.slice(2, -1)
      return String.fromCodePoint(code[0].toLowerCase() === 'x' ? parseInt(code.slice(1), 16) : Number(code))
    }
    return { '&amp;': '&', '&quot;': '"', '&apos;': "'", '&lt;': '<', '&gt;': '>' }[entity]
  })
}
function attributes(tag) {
  return Object.fromEntries([...tag.matchAll(/([\w:-]+)="([^"]*)"/g)].map((match) => [match[1], decode(match[2])]))
}
function single(tags, key, value, attribute, label) {
  const matches = tags.filter((tag) => tag[key] === value)
  assert.equal(matches.length, 1, `${label}: expected one ${value}`)
  assert(matches[0][attribute]?.trim(), `${label}: empty ${value}`)
  return matches[0][attribute]
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

const urls = new Map()
const titles = new Set()
const descriptions = new Set()
for (const page of Object.values(pages)) {
  const { content } = html.get(resolve(output, page.replace(/\.md$/, '.html')))
  const head = content.match(/<head>([\s\S]*?)<\/head>/)[1]
  const meta = [...head.matchAll(/<meta\b[^>]*>/g)].map((match) => attributes(match[0]))
  const links = [...head.matchAll(/<link\b[^>]*>/g)].map((match) => attributes(match[0]))
  const title = decode(head.match(/<title>([^<]+)<\/title>/)[1])
  const description = single(meta, 'name', 'description', 'content', page)
  assert(!titles.has(title), `${page}: duplicate page title`)
  assert(!descriptions.has(description), `${page}: duplicate page description`)
  titles.add(title)
  descriptions.add(description)
  const canonical = single(links, 'rel', 'canonical', 'href', page)
  const route = page.replace(/(^|\/)index\.md$/, '$1').replace(/\.md$/, '.html')
  assert.equal(canonical, new URL(route, siteURL).href, `${page}: canonical URL`)
  const language = attributes(content.match(/<html\b[^>]*>/)[0]).lang
  urls.set(canonical, { page, language })
  assert(!meta.some((tag) => tag.name === 'robots' && /noindex/i.test(tag.content)), `${page}: noindex`)
  for (const [property, value] of Object.entries({
    'og:type': 'website', 'og:site_name': 'p4runtime-go-controller',
    'og:title': title, 'og:description': description, 'og:url': canonical,
    'og:locale': language === 'zh-CN' ? 'zh_CN' : 'en_US',
    'og:image': new URL('social-card.png', siteURL).href,
    'og:image:type': 'image/png', 'og:image:width': '1200', 'og:image:height': '630',
  })) assert.equal(single(meta, 'property', property, 'content', page), value, `${page}: ${property}`)
  const alt = single(meta, 'property', 'og:image:alt', 'content', page)
  for (const [name, value] of Object.entries({
    'twitter:card': 'summary_large_image', 'twitter:title': title,
    'twitter:description': description, 'twitter:image': new URL('social-card.png', siteURL).href,
    'twitter:image:alt': alt,
  })) assert.equal(single(meta, 'name', name, 'content', page), value, `${page}: ${name}`)
}

const image = await readFile(resolve(output, 'social-card.png'))
assert(image.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10])), 'Share image must be PNG')
assert.equal(image.readUInt32BE(16), 1200, 'Share image width')
assert.equal(image.readUInt32BE(20), 630, 'Share image height')

const sitemap = await readFile(resolve(output, 'sitemap.xml'), 'utf8')
const entries = new Map()
for (const match of sitemap.matchAll(/<url>([\s\S]*?)<\/url>/g)) {
  const url = decode(match[1].match(/<loc>([^<]+)<\/loc>/)[1])
  assert(urls.has(url), `Sitemap URL has no canonical page: ${url}`)
  assert(!entries.has(url), `Duplicate sitemap URL: ${url}`)
  const alternatives = [...match[1].matchAll(/<xhtml:link\b[^>]*>/g)].map((link) => attributes(link[0]))
  entries.set(url, alternatives)
}
assert.equal(entries.size, urls.size, 'Sitemap must contain every canonical page')
let translations = 0
for (const [url, alternatives] of entries) {
  const { page } = urls.get(url)
  const counterpart = page.startsWith('zh/') ? page.slice(3) : `zh/${page}`
  const translated = Object.values(pages).includes(counterpart)
  assert.equal(alternatives.length, translated ? 2 : 0, `${page}: language alternatives`)
  for (const alternative of alternatives) {
    assert.equal(alternative.rel, 'alternate', `${page}: language link relation`)
    assert.equal(urls.get(alternative.href)?.language, alternative.hreflang, `${page}: language link target`)
    assert(entries.get(alternative.href)?.some((link) => link.href === url), `${page}: reciprocal language link`)
  }
  if (translated) {
    assert(alternatives.some((link) => link.href === url), `${page}: language link to self`)
    assert(alternatives.some((link) => urls.get(link.href)?.page === counterpart), `${page}: translated page link`)
    translations++
  }
}
const notFound = html.get(resolve(output, '404.html')).content
assert(!/<link\b[^>]*rel="canonical"/.test(notFound), '404 must not have a canonical URL')
console.log(`Checked metadata and sitemap for ${urls.size} pages, ${translations / 2} language pairs and the share image`)
