import { execFileSync } from 'node:child_process'
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { pages, version } from '../docs/.vitepress/site.mjs'

const repository = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const temporary = await mkdtemp(join(tmpdir(), 'p4runtime-doc-examples-'))
const module = 'github.com/zhh2001/p4runtime-go-controller/v2'
const environment = { ...process.env, GOWORK: 'off' }
let checked = 0
try {
  for (const source of Object.keys(pages)) {
    const content = await readFile(resolve(repository, source), 'utf8')
    for (const match of content.matchAll(/^```go\s*\n([\s\S]*?)^```\s*$/gm)) {
      if (!/^package\s+\w+/m.test(match[1])) continue
      const application = join(temporary, String(checked++))
      await mkdir(application)
      await writeFile(join(application, 'go.mod'), `module example.com/doc-example\n\ngo 1.26.0\n\nrequire ${module} ${version}\n`)
      await writeFile(join(application, 'example.go'), match[1])
      for (const args of [['mod', 'tidy'], ['build', './...'], ['vet', './...']]) {
        execFileSync('go', args, { cwd: application, env: environment, stdio: 'pipe', timeout: 180000 })
      }
      const selected = execFileSync('go', ['list', '-m', '-f', '{{.Version}} {{.Replace}}', module], {
        cwd: application, env: environment, encoding: 'utf8', timeout: 180000,
      }).trim()
      if (selected !== `${version} <nil>`) throw new Error(`Unexpected SDK dependency: ${selected}`)
      console.log(`${source}: complete Go example builds against ${version}`)
    }
  }
  if (!checked) throw new Error('No complete Go examples found')
  console.log(`Checked ${checked} complete Go examples`)
} catch (error) {
  if (error.stdout) process.stderr.write(error.stdout)
  if (error.stderr) process.stderr.write(error.stderr)
  throw error
} finally {
  await rm(temporary, { recursive: true, force: true })
}
