import { afterEach, describe, expect, it } from 'vitest'
import { mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { createServer } from 'node:net'
import { acquireResource, isolatedRuntimeEnv, prepareRun, runContext } from './e2e-run-context.mjs'

const cleanups = []
afterEach(async () => {
  for (const cleanup of cleanups.splice(0).reverse()) await cleanup()
})

function fixture(task = 'task-a', execution = 'run-a') {
  const cwd = mkdtempSync(join(tmpdir(), 'soha-e2e-isolation-'))
  cleanups.push(() => rmSync(cwd, { recursive: true, force: true }))
  return {
    cwd,
    lockRoot: join(cwd, 'locks'),
    env: { SOHA_E2E_TASK_ID: task, SOHA_E2E_EXECUTION_ID: execution },
  }
}

async function start(f, kind = 'mock', target) {
  const run = await prepareRun(f.env, kind, { ...f, target })
  cleanups.push(run.release)
  return run
}

describe('task output and runtime resource isolation', () => {
  it('keeps task routing but never forwards personal/model credentials to AI runtimes', () => {
    const env = isolatedRuntimeEnv({
      PATH: '/usr/bin',
      TMPDIR: '/tmp/run',
      SOHA_E2E_TASK_ID: 'task-a',
      SOHA_E2E_EXECUTION_ID: 'exec-a',
      SOHA_E2E_MANAGED_RUN: 'exec-a',
      SOHA_E2E_PORT: '45123',
      MIDSCENE_MODEL_API_KEY: 'secret-canary',
      OPENAI_API_KEY: 'secret-canary',
      MIDSCENE_RUN_DIR: '/unrelated',
      PLAYWRIGHT_JSON_OUTPUT_FILE: '/unrelated',
    })
    expect(env.SOHA_E2E_PORT).toBe('45123')
    expect(env.SOHA_E2E_EXECUTION_ID).toBe('exec-a')
    expect(JSON.stringify(env)).not.toContain('secret-canary')
    expect(env).not.toHaveProperty('MIDSCENE_RUN_DIR')
    expect(env).not.toHaveProperty('PLAYWRIGHT_JSON_OUTPUT_FILE')
  })
  it('keeps legacy paths and derives task paths without changing resource runId', () => {
    expect(runContext({}, '/repo').manifest).toBe('/repo/test-results/run-manifest.json')
    const run = runContext(
      { SOHA_E2E_TASK_ID: 'Task_119', SOHA_E2E_EXECUTION_ID: 'exec-1', SOHA_E2E_PORT: '45123' },
      '/repo',
    )
    expect(run.manifest).toBe('/repo/test-results/tasks/Task_119/exec-1/run-manifest.json')
    expect(run.baseURL).toBe('http://127.0.0.1:45123')
    expect(run.dataNamespace).toBe('Task_119-exec-1')
  })

  it('rejects invalid IDs and ports before filesystem mutations', () => {
    for (const value of ['', '../other', '/tmp/other', 'a/b', 'a.b', 'a'.repeat(65), 'x;touch y']) {
      expect(() => runContext({ SOHA_E2E_TASK_ID: value, SOHA_E2E_EXECUTION_ID: 'run' })).toThrow(
        'BLOCKED',
      )
      expect(() => runContext({ SOHA_E2E_EXECUTION_ID: value })).toThrow('BLOCKED')
    }
    for (const value of ['', '0', '80', '65536', '4179;echo', 'https://example.invalid'])
      expect(() => runContext({ SOHA_E2E_PORT: value })).toThrow('BLOCKED')
    expect(() => runContext({ SOHA_E2E_TASK_ID: 'task' })).toThrow('managed')
  })

  it('isolates simultaneous task executions and preserves completed evidence', async () => {
    const f = fixture()
    const a = await start(f)
    const b = await start({
      ...f,
      env: { SOHA_E2E_TASK_ID: 'task-b', SOHA_E2E_EXECUTION_ID: 'run-b' },
    })
    expect(a.context.port).not.toBe(b.context.port)
    expect(a.context.root).not.toBe(b.context.root)
    writeFileSync(a.context.manifest, 'owned evidence')
    await expect(prepareRun(f.env, 'mock', f)).rejects.toThrow()
    expect(readFileSync(a.context.manifest, 'utf8')).toBe('owned evidence')
    expect(JSON.parse(readFileSync(b.context.manifest, 'utf8')).executionId).toBe('run-b')
    a.release()
    await expect(prepareRun(f.env, 'mock', f)).rejects.toThrow()
    expect(readFileSync(a.context.manifest, 'utf8')).toBe('owned evidence')
  })

  it('rejects symlinked report parents without touching their targets', async () => {
    const f = fixture()
    const outside = mkdtempSync(join(tmpdir(), 'soha-e2e-outside-'))
    cleanups.push(() => rmSync(outside, { recursive: true, force: true }))
    writeFileSync(join(outside, 'keep'), 'untouched')
    symlinkSync(outside, join(f.cwd, 'test-results'), 'dir')
    await expect(prepareRun(f.env, 'mock', f)).rejects.toThrow('real directory')
    expect(readdirSync(outside)).toEqual(['keep'])
  })

  it('preserves an existing legacy manifest symlink target on rejection', async () => {
    const f = fixture()
    f.env = { SOHA_E2E_EXECUTION_ID: 'legacy' }
    const first = await start(f, 'real')
    first.release()
    const outside = join(f.cwd, 'preserve.json')
    writeFileSync(outside, 'untouched')
    rmSync(first.context.manifest)
    symlinkSync(outside, first.context.manifest)
    await expect(prepareRun(f.env, 'real', f)).rejects.toThrow('report path')
    expect(readFileSync(outside, 'utf8')).toBe('untouched')
    expect(readdirSync(f.lockRoot)).toEqual([])
  })

  it('records failed real preflight only in this execution and releases its ownership', async () => {
    const f = fixture()
    await expect(
      prepareRun(f.env, 'real', {
        ...f,
        getTarget() {
          throw new Error('BLOCKED: rejected target')
        },
      }),
    ).rejects.toThrow('rejected target')
    const report = JSON.parse(readFileSync(runContext(f.env, f.cwd).manifest, 'utf8'))
    expect(report).toMatchObject({ taskId: 'task-a', executionId: 'run-a', result: 'BLOCKED' })
  })

  it('refuses a busy explicit port without stopping or replacing its owner', async () => {
    const server = createServer()
    await new Promise((accept) => server.listen(0, '127.0.0.1', accept))
    cleanups.push(() => new Promise((accept) => server.close(accept)))
    const f = fixture()
    f.env.SOHA_E2E_PORT = String(server.address().port)
    await expect(prepareRun(f.env, 'mock', f)).rejects.toThrow(/EADDRINUSE/)
    expect(server.listening).toBe(true)
    const manifest = JSON.parse(readFileSync(runContext(f.env, f.cwd).manifest, 'utf8'))
    expect(manifest.result).toBe('BLOCKED')
  })

  it('serializes legacy report writers, independent of their browser port', async () => {
    const f = fixture()
    f.env = { SOHA_E2E_EXECUTION_ID: 'legacy-a' }
    const a = await start(f, 'real')
    writeFileSync(a.context.manifest, 'keep previous writer')
    await expect(prepareRun({ SOHA_E2E_EXECUTION_ID: 'legacy-b' }, 'real', f)).rejects.toThrow(
      'occupied',
    )
    expect(readFileSync(a.context.manifest, 'utf8')).toBe('keep previous writer')
  })

  it('locks real environments across task/target aliases, roles and checkouts', async () => {
    const a = fixture('task-a', 'exec-a')
    const b = fixture('task-b', 'exec-b')
    b.lockRoot = a.lockRoot
    const target = { runId: 'provider-owned-run', containerId: 'a'.repeat(64) }
    await start(a, 'real', target)
    await expect(prepareRun(b.env, 'real', { ...b, target })).rejects.toThrow('occupied')
    b.env.SOHA_E2E_EXECUTION_ID = 'exec-c'
    await expect(
      prepareRun(b.env, 'real', { ...b, target: { ...target, containerId: 'b'.repeat(64) } }),
    ).rejects.toThrow('occupied')
    b.env.SOHA_E2E_EXECUTION_ID = 'exec-d'
    await start(b, 'real', { runId: 'independent-run', containerId: 'b'.repeat(64) })
    expect(a.env.SOHA_E2E_REAL_RUN_ID).toBe(target.runId)
    expect(a.env.SOHA_E2E_REAL_CONTAINER_ID).toBe(target.containerId)
  })

  it('releases only owned locks and refuses to steal an unknown owner', () => {
    const f = fixture()
    const release = acquireResource('owned', { executionId: 'run-a' }, f.lockRoot)
    expect(() => acquireResource('owned', { executionId: 'run-b' }, f.lockRoot)).toThrow('occupied')
    release()
    const replacement = acquireResource('owned', { executionId: 'run-c' }, f.lockRoot)
    const path = join(f.lockRoot, readdirSync(f.lockRoot)[0], 'owner.json')
    writeFileSync(path, JSON.stringify({ token: 'another-owner' }))
    expect(replacement).toThrow('ownership changed')
    expect(JSON.parse(readFileSync(path, 'utf8')).token).toBe('another-owner')
  })
})
