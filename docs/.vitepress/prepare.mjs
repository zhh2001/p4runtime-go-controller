import { cp, mkdir, rm } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { pages } from './site.mjs'

const directory = dirname(fileURLToPath(import.meta.url))
const root = resolve(directory, '../..')
const output = resolve(directory, 'site')

await rm(output, { recursive: true, force: true })
for (const [source, page] of Object.entries(pages)) {
  const destination = resolve(output, page)
  await mkdir(dirname(destination), { recursive: true })
  await cp(resolve(root, source), destination)
}
console.log(`Prepared ${Object.keys(pages).length} documentation pages`)
