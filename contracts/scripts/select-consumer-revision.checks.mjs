import assert from 'node:assert/strict'
import { test } from 'node:test'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { selectConsumerRepository, selectConsumerRevision } from './select-consumer-revision.mjs'

const sha = '1234567890abcdef1234567890abcdef12345678'
const revisions = { soha: sha, 'soha-web': sha }

test('pinned is the default and latest is explicit', () => {
  assert.equal(selectConsumerRevision(revisions, 'soha'), sha)
  assert.equal(selectConsumerRevision(revisions, 'soha-web', 'latest'), 'main')
})

test('App is included with an immutable baseline and explicit latest mode', () => {
  const manifest = JSON.parse(readFileSync(new URL('../compat/consumer-revisions.json', import.meta.url), 'utf8'))
  assert.match(selectConsumerRevision(manifest, 'soha-app'), /^[a-f0-9]{40}$/)
  assert.equal(selectConsumerRevision(manifest, 'soha-app', 'latest'), 'main')
})

test('missing consumers never silently fall back to main', () => {
  for (const mode of ['pinned', 'latest']) {
    assert.throws(() => selectConsumerRevision(revisions, 'soha-agent', mode), /Missing reviewed/)
    assert.throws(() => selectConsumerRevision(revisions, undefined, mode), /Missing reviewed/)
  }
})

test('rejects malformed manifests and modes', () => {
  for (const value of [null, [], 'main']) {
    assert.throws(() => selectConsumerRevision(value, 'soha'), /must be an object/)
  }
  assert.throws(() => selectConsumerRevision(revisions, 'soha', 'auto'), /Unsupported/)
})

test('all recorded revisions must be full immutable commits', () => {
  for (const ref of ['main', 'v0.1.18', '1234567', '0'.repeat(40), 'g'.repeat(40), null]) {
    assert.throws(() => selectConsumerRevision({ ...revisions, other: ref }, 'soha'), /commit SHA/)
  }
})

test('prototype keys are not consumers', () => {
  assert.throws(() => selectConsumerRevision(revisions, 'toString'), /Missing reviewed/)
})

test('external historical checkouts use Root without changing reviewed commit pins', () => {
  const manifest = JSON.parse(readFileSync(new URL('../compat/consumer-revisions.json', import.meta.url), 'utf8'))
  assert.deepEqual(manifest, {
    soha: 'a0885220969bc6a37ac75672214e6fe0b6d2926a',
    'soha-web': '83bcb548005e34ecf86bfc5b56804e680b0d5898',
    'soha-app': 'e51ef3fc0e46d89d62da79bf4cc1813ff797049b',
    'soha-cli': '71301b9fb9f9f2b4acd500483c08094a1b9be83a',
    'soha-agent': 'd8ce67c5b52b9b314e8fd6c1f42af9a0313e6db2',
    'soha-skills': 'e562189e1416a9e2f8c72f9018e2236dc8d23dd1',
  })
  for (const consumer of Object.keys(manifest)) {
    assert.equal(selectConsumerRepository(consumer), consumer === 'soha-app' ? 'opensoha/soha-app' : 'opensoha/soha')
    assert.equal(selectConsumerRevision(manifest, consumer), manifest[consumer])
  }
  assert.throws(() => selectConsumerRepository('other'), /Unknown consumer/)
})

test('trusted selector reads the original source pins and always emits App repository', (t) => {
  const directory = mkdtempSync(join(tmpdir(), 'consumer-original-pins-'))
  t.after(() => rmSync(directory, { recursive: true, force: true }))
  const manifest = join(directory, 'original-pins.json')
  const output = join(directory, 'output')
  const original = 'c'.repeat(40)
  writeFileSync(manifest, JSON.stringify({ 'soha-app': original }))
  const script = fileURLToPath(new URL('./select-consumer-revision.mjs', import.meta.url))
  const run = (args) => spawnSync(process.execPath, [script, ...args], { encoding: 'utf8',
    env: { ...process.env, CONSUMER: 'soha-app', CONSUMER_REVISION_MODE: 'pinned', GITHUB_OUTPUT: output } })
  const result = run(['--revisions-file', manifest])
  assert.equal(result.status, 0, result.stderr)
  assert.equal(readFileSync(output, 'utf8'), `ref=${original}\nrepository=opensoha/soha-app\n`)
  for (const args of [['--revisions-file'], ['--revisions-file', manifest, '--other'], ['--revisions-file', join(directory, 'missing')]]) {
    writeFileSync(output, '')
    assert.notEqual(run(args).status, 0)
    assert.equal(readFileSync(output, 'utf8'), '')
  }
  for (const pins of [{}, { 'soha-app': 'main' }, { 'soha-app': '0'.repeat(40) }]) {
    writeFileSync(manifest, JSON.stringify(pins))
    assert.notEqual(run(['--revisions-file', manifest]).status, 0)
    assert.equal(readFileSync(output, 'utf8'), '')
  }
})
