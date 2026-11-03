import assert from 'node:assert/strict'
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { tmpdir } from 'node:os'
import { setTimeout as delay } from 'node:timers/promises'
import { runParallel } from './run-e2e-parallel.mjs'

const directory = mkdtempSync(join(tmpdir(), 'soha-parallel-proof-'))
const barrier = join(directory, 'ready')
const overriddenReport = join(directory, 'unrelated.json')
writeFileSync(overriddenReport, 'preserve unrelated output')
const runs = []
const done = runParallel(
  ['task119-parallel-a', 'task119-parallel-b'].map((taskId) => ({
    taskId,
    args: ['e2e/ui-mock/parallel-isolation.spec.ts'],
  })),
  {
    env: {
      ...process.env,
      SOHA_E2E_PROBE_BARRIER: barrier,
      PLAYWRIGHT_JSON_OUTPUT_FILE: overriddenReport,
      PLAYWRIGHT_JSON_OUTPUT_DIR: directory,
      PLAYWRIGHT_JSON_OUTPUT_NAME: 'inherited.json',
    },
    onStart: (run) => runs.push(run),
  },
)
try {
  const deadline = Date.now() + 90_000
  while (
    runs.length !== 2 ||
    !runs.every((run) => existsSync(join(run.root, 'parallel-ready.json')))
  ) {
    if (Date.now() > deadline || runs.some((run) => run.exitCode !== null))
      throw new Error(runs.map((run) => run.output).join('\n'))
    await delay(100)
  }
  const proofs = runs.map((run) =>
    JSON.parse(readFileSync(join(run.root, 'parallel-ready.json'), 'utf8')),
  )
  assert.notEqual(proofs[0].baseURL, proofs[1].baseURL)
  assert.notEqual(proofs[0].marker, proofs[1].marker)
  assert.equal(proofs[0].resourceId, proofs[1].resourceId)
  assert.ok(
    runs.every((run) => run.exitCode === null),
    'both browser processes must overlap',
  )
  writeFileSync(barrier, 'both tasks reached their independent mutation')
  await done
  assert.equal(readFileSync(overriddenReport, 'utf8'), 'preserve unrelated output')
  assert.equal(existsSync(join(directory, 'inherited.json')), false)
  for (const run of runs) {
    assert.equal(run.exitCode, 0, run.output)
    const manifest = JSON.parse(readFileSync(join(run.root, 'run-manifest.json'), 'utf8'))
    assert.equal(manifest.result, 'PASS')
    assert.equal(manifest.taskId, run.taskId)
    assert.equal(manifest.executionId, run.executionId)
    assert.equal(manifest.tests.length, 1)
    assert.ok(existsSync(join(run.root, 'playwright.json')))
    console.log(`PASS parallel isolation: ${run.root}`)
  }
} finally {
  for (const run of runs) if (run.exitCode === null) run.child.kill('SIGTERM')
  await done
  rmSync(directory, { recursive: true, force: true })
}
