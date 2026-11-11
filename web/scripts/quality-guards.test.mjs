import { describe, expect, it } from 'vitest'
import { execFileSync } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import {
  aiPreflight,
  redact,
  validateCleanup,
  validateTarget,
  validateVersions,
  assertTrustedDispatch,
  sourceEvidence,
} from './quality-guards.mjs'

it('records source evidence for a dirty diff larger than 1 MiB', () => {
  const repo = mkdtempSync(join(tmpdir(), 'soha-source-evidence-'))
  const git = (...args) => execFileSync('git', ['-C', repo, ...args])
  try {
    git('init', '--quiet')
    writeFileSync(join(repo, 'large.txt'), '')
    git('add', 'large.txt')
    git(
      '-c',
      'user.name=Test',
      '-c',
      'user.email=test@example.invalid',
      'commit',
      '--quiet',
      '-m',
      'fixture',
    )
    writeFileSync(join(repo, 'large.txt'), 'x'.repeat(2 * 1024 * 1024) + '\n')
    const before = sourceEvidence(repo)
    expect(before.commit).toMatch(/^[a-f0-9]{40}$/)
    expect(before.dirtyPatchDigest).toMatch(/^[a-f0-9]{64}$/)
    writeFileSync(join(repo, 'untracked.txt'), 'new source')
    expect(sourceEvidence(repo).dirtyPatchDigest).not.toBe(before.dirtyPatchDigest)
  } finally {
    rmSync(repo, { recursive: true, force: true })
  }
})

it('S07/C03: dispatch refuses forks, arbitrary refs and unapproved SHA', () => {
  const sha = 'a'.repeat(40)
  expect(() => assertTrustedDispatch('refs/heads/main', sha, sha)).not.toThrow()
  for (const [ref, actual, approved] of [
    ['refs/pull/1/merge', sha, sha],
    ['refs/heads/main', 'main', 'main'],
    ['refs/heads/main', sha, 'b'.repeat(40)],
    ['refs/heads/main', sha, undefined],
  ])
    expect(() => assertTrustedDispatch(ref, actual, approved)).toThrow('BLOCKED')
})

const sources = Object.fromEntries(
  ['web', 'core', 'contracts', 'agent'].map((name) => [
    name,
    { commit: 'a'.repeat(40), artifactDigest: 'e'.repeat(64) },
  ]),
)
const target = {
  sources,
  mode: 'api-real',
  baseURL: 'http://127.0.0.1:18432',
  disposable: true,
  runId: 'test-run',
  containerId: 'b'.repeat(64),
  imageDigest: 'sha256:' + 'c'.repeat(64),
  role: 'readonly',
}
const inspected = {
  Id: target.containerId,
  Image: target.imageDigest,
  Config: {
    Labels: {
      'soha.test.run': target.runId,
      ...Object.fromEntries(
        Object.entries(sources).flatMap(([name, value]) => [
          [`soha.test.${name}-sha`, value.commit],
          [`soha.test.${name}-artifact`, value.artifactDigest],
        ]),
      ),
    },
  },
  NetworkSettings: { Ports: { '8080/tcp': [{ HostIp: '127.0.0.1', HostPort: '18432' }] } },
}

describe('S01–S04/C01 target, cleanup and privacy boundaries', () => {
  it('accepts owned target and rejects fake localhost, unknown/shared or wrong port', () => {
    expect(validateTarget(target, inspected)).toBe(target)
    for (const change of [
      { baseURL: 'http://localhost:18433' },
      { baseURL: 'https://other.invalid' },
      { disposable: false },
      { runId: 'another-run' },
      { containerId: 'd'.repeat(64) },
      { imageDigest: 'wrong' },
      { role: 'administrator' },
    ])
      expect(() => validateTarget({ ...target, ...change }, inspected)).toThrow()
  })
  it('rejects missing/invalid SHA and no implicit latest fallback', () => {
    for (const commit of ['', 'main', 'latest', 'abc', '; touch /tmp/unwanted'])
      expect(() => validateVersions({ ...sources, core: { commit } }, 'api-real')).toThrow()
    expect(() => validateVersions({ ...sources, agent: undefined }, 'e2e-agent')).toThrow()
  })
  it('C02: refuses missing digests, wrong artifacts and mismatched contracts', () => {
    for (const artifactDigest of [undefined, '', 'latest', 'f'.repeat(64)])
      expect(() =>
        validateTarget(
          {
            ...target,
            sources: { ...sources, contracts: { ...sources.contracts, artifactDigest } },
          },
          inspected,
        ),
      ).toThrow('BLOCKED')
    expect(() =>
      validateTarget(target, {
        ...inspected,
        Config: {
          Labels: { ...inspected.Config.Labels, 'soha.test.contracts-sha': 'f'.repeat(40) },
        },
      }),
    ).toThrow('BLOCKED')
  })
  it('cleanup and residual recovery reject other runs and stale UIDs', () => {
    const lease = { runId: 'run', id: 'resource', uid: 'uid' }
    expect(validateCleanup(lease, lease, 'run')).toEqual({ id: 'resource', uid: 'uid' })
    for (const change of [{ runId: 'other' }, { uid: 'recreated' }, { id: 'resource-other' }])
      expect(() => validateCleanup(lease, { ...lease, ...change }, 'run')).toThrow()
    expect(() => validateCleanup({ ...lease, uid: '' }, lease, 'run')).toThrow()
  })
  it('synthetic secret canary never enters shareable summary', () => {
    const canary = 'synthetic-secret-canary'
    const output = JSON.stringify(
      redact(
        {
          headers: { Authorization: `Bearer ${canary}`, Cookie: canary },
          state: { accessToken: canary },
          yaml: `token: ${canary}`,
          message: `failed ${canary}`,
        },
        [canary],
      ),
    )
    expect(output).not.toContain(canary)
  })
})

describe('M01/M02 opt-in and model budget', () => {
  const env = {
    SOHA_AI_ENABLED: '1',
    MIDSCENE_MODEL_BASE_URL: 'https://approved.invalid/v1',
    MIDSCENE_MODEL_API_KEY: 'synthetic',
    MIDSCENE_MODEL_NAME: 'configured-model',
    MIDSCENE_MODEL_FAMILY: 'configured-family',
    SOHA_AI_ALLOWED_HOST: 'approved.invalid',
    SOHA_AI_APPROVAL_ID: 'approval',
    SOHA_AI_MAX_CALLS: '12',
  }
  it('disabled is NOT_RUN, explicit missing credentials or authorization is BLOCKED', () => {
    expect(aiPreflight({})).toEqual({ result: 'NOT_RUN' })
    for (const key of Object.keys(env).filter((key) => key !== 'SOHA_AI_ENABLED'))
      expect(() => aiPreflight({ ...env, [key]: '' })).toThrow(/BLOCKED/)
  })
  it('host mismatch, URL credentials, insecure protocol and budget rejected', () => {
    expect(aiPreflight(env).maxCalls).toBe(12)
    for (const change of [
      { SOHA_AI_ALLOWED_HOST: 'other.invalid' },
      { MIDSCENE_MODEL_BASE_URL: 'https://user:pass@approved.invalid' },
      { MIDSCENE_MODEL_BASE_URL: 'http://approved.invalid' },
      { SOHA_AI_MAX_CALLS: '13' },
    ])
      expect(() => aiPreflight({ ...env, ...change })).toThrow(/BLOCKED/)
  })
})
