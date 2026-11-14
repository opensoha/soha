import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { createServer } from 'node:net'
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { setTimeout as delay } from 'node:timers/promises'
import { acquireResource, runContext } from './e2e-run-context.mjs'
import { runParallel } from './run-e2e-parallel.mjs'

const directory = mkdtempSync(join(tmpdir(), 'soha-e2e-lifecycle-'))
const children = []
const promises = []
const spec = 'e2e/ui-mock/parallel-isolation.spec.ts'

function launch(kind, args, overrides = {}) {
  const env = {
    ...process.env,
    SOHA_E2E_TASK_ID: 'task119-lifecycle',
    SOHA_E2E_EXECUTION_ID: randomUUID(),
    ...overrides,
  }
  const context = runContext(env)
  const child = spawn(process.execPath, ['scripts/run-e2e.mjs', kind, ...args], {
    env,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  children.push(child)
  const run = { ...context, child, output: '', done: null }
  child.stdout.on('data', (value) => {
    run.output += value
  })
  child.stderr.on('data', (value) => {
    run.output += value
  })
  run.done = new Promise((accept, reject) => {
    child.once('error', reject)
    child.once('close', accept)
  })
  promises.push(run.done)
  return run
}

async function ready(root, child) {
  const path = join(root, 'parallel-ready.json')
  const deadline = Date.now() + 60_000
  while (!existsSync(path)) {
    assert.ok(
      Date.now() < deadline && child.exitCode === null,
      `browser did not reach probe: ${root}`,
    )
    await delay(100)
  }
  return JSON.parse(readFileSync(path, 'utf8'))
}

async function reclaimed(proof) {
  const port = Number(new URL(proof.baseURL).port)
  const server = createServer()
  await new Promise((accept, reject) => {
    server.once('error', reject)
    server.listen(port, '127.0.0.1', accept)
  })
  await new Promise((accept) => server.close(accept))
  const release = acquireResource(`port:${port}`, { executionId: 'lifecycle-check' })
  release()
}

try {
  const listing = launch('mock', [spec, '--list'])
  assert.equal(await listing.done, 0, listing.output)
  assert.equal(existsSync(listing.root), false, 'listing must not create evidence')

  const empty = launch('mock', [spec, '--grep', '^deliberately-no-match$', '--pass-with-no-tests'])
  assert.equal(await empty.done, 1, empty.output)
  assert.equal(JSON.parse(readFileSync(empty.manifest, 'utf8')).result, 'FAIL')

  const canary = 'synthetic-never-send-' + randomUUID()
  const disabled = launch('ai', [], { SOHA_AI_ENABLED: '0', MIDSCENE_MODEL_API_KEY: canary })
  assert.equal(await disabled.done, 0, disabled.output)
  assert.equal(JSON.parse(readFileSync(disabled.manifest, 'utf8')).result, 'NOT_RUN')
  assert.ok(!disabled.output.includes(canary))
  const blocked = launch('ai', [], {
    SOHA_AI_ENABLED: '1',
    MIDSCENE_MODEL_BASE_URL: undefined,
    MIDSCENE_MODEL_API_KEY: undefined,
  })
  assert.equal(await blocked.done, 2, blocked.output)
  assert.equal(JSON.parse(readFileSync(blocked.manifest, 'utf8')).result, 'BLOCKED')
  assert.equal(JSON.parse(readFileSync(blocked.aiReport, 'utf8')).result, 'BLOCKED')

  const cancelled = launch('mock', [spec], {
    SOHA_E2E_PROBE_BARRIER: join(directory, 'never-created'),
  })
  const proof = await ready(cancelled.root, cancelled.child)
  cancelled.child.kill('SIGTERM')
  assert.equal(await cancelled.done, 130, cancelled.output)
  assert.equal(JSON.parse(readFileSync(cancelled.manifest, 'utf8')).result, 'FAIL')
  await reclaimed(proof)
  const preserved = readFileSync(cancelled.manifest, 'utf8')
  const duplicate = launch('mock', [spec], {
    SOHA_E2E_TASK_ID: cancelled.taskId,
    SOHA_E2E_EXECUTION_ID: cancelled.executionId,
  })
  assert.equal(await duplicate.done, 2, duplicate.output)
  assert.equal(readFileSync(cancelled.manifest, 'utf8'), preserved)

  const runs = []
  const batchPromise = runParallel(
    ['task119-cancel-a', 'task119-cancel-b'].map((taskId) => ({ taskId, args: [spec] })),
    {
      env: { ...process.env, SOHA_E2E_PROBE_BARRIER: join(directory, 'batch-never-created') },
      onStart: (run) => {
        runs.push(run)
        children.push(run.child)
      },
    },
  )
  promises.push(batchPromise)
  while (runs.length < 2) await delay(25)
  const proofs = await Promise.all(runs.map((run) => ready(run.root, run.child)))
  process.kill(process.pid, 'SIGINT')
  const batch = await batchPromise
  assert.equal(batch.interrupted, true)
  for (const run of runs) assert.equal(run.exitCode, 130, run.output)
  for (const value of proofs) await reclaimed(value)
  console.log(
    'PASS: list, AI opt-out/missing approval, duplicate evidence, SIGTERM and batch SIGINT reclaim owned previews and locks',
  )
  for (const root of [disabled.root, blocked.root, cancelled.root, ...runs.map((run) => run.root)])
    console.log(root)
} finally {
  for (const child of children)
    if (child.exitCode === null && child.signalCode === null) child.kill('SIGTERM')
  await Promise.allSettled(promises)
  rmSync(directory, { recursive: true, force: true })
}
