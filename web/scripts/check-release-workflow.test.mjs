import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'vitest'

test('release accepts only the reviewed, registry-backed contracts lock', () => {
  const workflow = readFileSync(
    new URL('../.github/workflows/release.yml', import.meta.url),
    'utf8',
  )
  const script = workflow.match(/node -e "(const pkg=require\('\.\/package.json'\).*?)"/)?.[1]
  assert.ok(script)
  const root = mkdtempSync(join(tmpdir(), 'soha-release-lock-'))
  const version = '0.1.19'
  const entry = {
    version,
    resolved: `https://registry.npmjs.org/@opensoha/contracts/-/contracts-${version}.tgz`,
    integrity: 'sha512-reviewed',
  }
  const run = (dependency = version, lock = entry, requested = '') => {
    writeFileSync(
      join(root, 'package.json'),
      JSON.stringify({ dependencies: { '@opensoha/contracts': dependency } }),
    )
    writeFileSync(
      join(root, 'package-lock.json'),
      JSON.stringify({ packages: { 'node_modules/@opensoha/contracts': lock } }),
    )
    return spawnSync(process.execPath, ['-e', script], {
      cwd: root,
      env: { ...process.env, CONTRACTS_VERSION: requested },
      encoding: 'utf8',
    }).status
  }
  try {
    assert.equal(run(), 0)
    assert.equal(run(version, entry, version), 0)
    assert.notEqual(run(version, entry, '0.1.18'), 0)
    assert.notEqual(run('^0.1.19'), 0)
    assert.notEqual(run(version, { ...entry, version: '0.1.18' }), 0)
    assert.notEqual(run(version, { ...entry, resolved: 'file:../soha-contracts' }), 0)
    assert.notEqual(run(version, { ...entry, integrity: undefined }), 0)
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})
