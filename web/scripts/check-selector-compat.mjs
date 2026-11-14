import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { after, test } from 'node:test'
import { pathToFileURL } from 'node:url'
import { dependencyPatchPlan as plan } from './apply-dependency-patches.mjs'

const require = createRequire(import.meta.url)
const sha256 = (content) => createHash('sha256').update(content).digest('hex')
const output = mkdtempSync(
  join(process.env.OPENSOHA_VERIFY_DIR || tmpdir(), 'soha-selector-compat-'),
)
after(() => {
  rmSync(output, { recursive: true })
  assert.equal(existsSync(output), false)
  console.log(JSON.stringify({ selectorOutput: output, cleanup: 'PASS' }))
})
const entry = plan.files.find((file) => file.path === 'node_modules/nwsapi/src/nwsapi.js')
const installed = readFileSync(require.resolve('nwsapi'), 'utf8')
assert([entry.before, entry.after].includes(sha256(installed)), 'pinned selector source required')
const edit = plan.edits.selectorCacheCapacity
const candidate =
  sha256(installed) === entry.after ? installed : installed.replace(edit.before, edit.after)
assert.equal(sha256(candidate), entry.after)
const source = join(output, 'nwsapi.cjs')
writeFileSync(source, candidate, { mode: 0o600 })
const fixtures = new URL('./fixtures/nwsapi-2.2.28/', import.meta.url)
for (const [name, identity] of Object.entries(plan.nwsapiCompatibility.fixtures))
  assert.equal(sha256(readFileSync(new URL(name, fixtures))), identity.sha256)

test('published parser and semantic regressions retain all 18 upstream assertions', () => {
  const original = readFileSync(new URL('maintenance.test.mts.txt', fixtures), 'utf8')
  const replace = (content, before, after) => {
    assert.equal(content.split(before).length, 2, 'exact upstream test plumbing required')
    return content.replace(before, after)
  }
  // Only fixture location and module resolution change; the pinned assertions, including
  // the malformed-selector child process and its 3000ms deadline, remain upstream bytes.
  let suite = replace(
    original,
    'const require = createRequire(import.meta.url)',
    `const require = createRequire(${JSON.stringify(import.meta.url)})`,
  )
  suite = replace(
    suite,
    "const root = new URL('../../../', import.meta.url)",
    `const root = new URL(${JSON.stringify(new URL('../..', import.meta.url).href)})`,
  )
  suite = replace(
    suite,
    "const source =\n  process.env['NWSAPI_TEST_SOURCE'] ||\n  fileURLToPath(new URL('src/nwsapi.js', root))",
    `const source = fileURLToPath(${JSON.stringify(pathToFileURL(source).href)})`,
  )
  const suiteFile = join(output, 'maintenance.test.mts')
  writeFileSync(suiteFile, suite, { mode: 0o600 })
  mkdirSync(join(output, 'fixture'))
  writeFileSync(
    join(output, 'fixture/parser-stall.txt'),
    readFileSync(new URL('parser-stall.txt', fixtures)),
    { mode: 0o600 },
  )
  // This is a separate test controller, not a worker of the outer node:test run.
  const env = { ...process.env }
  delete env.NODE_TEST_CONTEXT
  const result = spawnSync(process.execPath, ['--experimental-strip-types', '--test', suiteFile], {
    encoding: 'utf8',
    maxBuffer: 4 * 1024 * 1024,
    env,
  })
  assert.ifError(result.error)
  assert.equal(result.status, 0, result.stdout + result.stderr)
  assert.match(result.stdout, /# tests 18\b/)
  assert.match(result.stdout, /# pass 18\b/)
  console.log(result.stdout)
})

test('all four caches evict beyond 4096 entries and retain live DOM results', () => {
  const { JSDOM } = require('jsdom')
  const dom = new JSDOM('<!doctype html><div></div>')
  try {
    const engine = require(source)(dom.window)
    const node = dom.window.document.querySelector('div')
    const selector = (value) => `div[data-cache-probe="${value}"]`
    for (let i = 0; i < 4096 + 64; i++) {
      node.setAttribute('data-cache-probe', String(i))
      assert.equal(engine.match(selector(i), node), true)
      assert.deepEqual(engine.select(selector(i)), [node])
    }
    for (const name of ['matchResolvers', 'matchLambdas', 'selectResolvers', 'selectLambdas']) {
      assert.equal(engine[name].size(), 4096, name)
      const first = name.endsWith('Lambdas') ? `selector:${selector(0)}` : selector(0)
      assert.equal(engine[name].has(first), false, `${name} evicts the oldest selector`)
    }
    assert.equal(engine.match(selector(0), node), false)
    assert.deepEqual(engine.select(selector(0)), [])
    node.setAttribute('data-cache-probe', '0')
    assert.equal(engine.match(selector(0), node), true)
    assert.deepEqual(engine.select(selector(0)), [node])
    node.remove()
    assert.deepEqual(engine.select(selector(0)), [])
  } finally {
    dom.window.close()
  }
})
