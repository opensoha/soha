import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'vitest'
import { createHash } from 'node:crypto'
import YAML from 'yaml'

test('active release accepts exact Core versions and rejects prefixed or unrelated tags', () => {
  const workflow = YAML.parse(
    readFileSync(new URL('../../.github/workflows/release.yml', import.meta.url), 'utf8'),
  )
  const steps = workflow.jobs.release.steps
  const script = steps.find((step) => step.name === 'Verify release version').run
  const root = mkdtempSync(join(tmpdir(), 'soha-core-version-'))
  mkdirSync(join(root, 'deploy'))
  writeFileSync(
    join(root, 'deploy/kustomization.yaml'),
    '  - name: ghcr.io/opensoha/soha\n    newTag: v1.2.3\n',
  )
  writeFileSync(
    join(root, 'deploy/deployment.yaml'),
    '          image: ghcr.io/opensoha/soha:v1.2.3\n',
  )
  const run = (version, ref = 'refs/tags/v1.2.3') =>
    spawnSync('bash', ['-e', '-o', 'pipefail', '-c', script], {
      cwd: root,
      env: {
        ...process.env,
        RELEASE_VERSION: version,
        GITHUB_REF: ref,
        GITHUB_REF_NAME: ref.replace(/^refs\/(?:heads|tags)\//, ''),
      },
    }).status
  try {
    assert.equal(run('v1.2.3'), 0)
    assert.equal(run('v1.2.3', 'refs/heads/main'), 0)
    for (const value of ['contracts/v1.2.3', '1.2.3', 'v1.2.3-rc.1', 'v1.2.4', 'v1.2.3\n'])
      assert.notEqual(run(value), 0)
    assert.notEqual(run('v1.2.3', 'refs/tags/contracts/v1.2.3'), 0)
    assert.equal(
      steps.find((step) => step.name === 'Resolve runtime version').run.includes('git describe'),
      false,
    )
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

test('active root release builds same-source Web and blocks absent or different public SDKs', () => {
  const workflow = readFileSync(
    new URL('../../.github/workflows/release.yml', import.meta.url),
    'utf8',
  )
  assert.ok(workflow.includes('npm run build:web'))
  assert.ok(workflow.includes('"webRef": "${GITHUB_SHA}"'))
  assert.ok(!workflow.includes('--repo opensoha/soha-web'))
  assert.ok(!workflow.includes('git describe --tags'))
  const script = workflow.match(/node - "\$package_dir" <<'NODE'\n([\s\S]*?)\n          NODE/)?.[1]
  assert.ok(script)
  const root = mkdtempSync(join(tmpdir(), 'soha-release-source-'))
  const content = Buffer.from('same source package')
  const integrity = 'sha512-' + createHash('sha512').update(content).digest('base64')
  writeFileSync(join(root, 'sdk.tgz'), content)
  writeFileSync(join(root, 'local.json'), JSON.stringify([{ filename: 'sdk.tgz' }]))
  const run = (dist) => {
    writeFileSync(join(root, 'registry.json'), JSON.stringify(dist))
    return spawnSync(process.execPath, ['-', root], { input: script, encoding: 'utf8' }).status
  }
  try {
    const dist = {
      tarball: 'https://registry.npmjs.org/@opensoha/contracts/-/contracts-0.1.29.tgz',
      integrity,
    }
    assert.equal(run(dist), 0)
    assert.notEqual(run({ ...dist, integrity: undefined }), 0)
    assert.notEqual(run({ ...dist, tarball: 'file:../contracts' }), 0)
    assert.notEqual(run({ ...dist, integrity: 'sha512-other-source' }), 0)
  } finally {
    rmSync(root, { recursive: true, force: true })
  }
})

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
