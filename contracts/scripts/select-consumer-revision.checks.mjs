import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import { selectConsumerRevision } from './select-consumer-revision.mjs'

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
