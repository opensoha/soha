import { spawn } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { runContext } from './e2e-run-context.mjs'

// One verification/cache lease owns the batch. At most two browser runs consume it at once.
export async function runParallel(jobs, { env = process.env, onStart = () => {} } = {}) {
  if (!jobs.length || new Set(jobs.map((job) => job.taskId)).size !== jobs.length)
    throw new Error('Each parallel job needs a distinct task ID')
  for (const job of jobs) {
    runContext({ SOHA_E2E_TASK_ID: job.taskId, SOHA_E2E_EXECUTION_ID: 'validate' })
    if (!Array.isArray(job.args) || !job.args.length || job.args[0].startsWith('-'))
      throw new Error('Each parallel job needs an explicit test path')
  }
  const running = new Set()
  const results = []
  let index = 0
  let interrupted = false
  const stop = () => {
    interrupted = true
    for (const child of running) child.kill('SIGTERM')
  }
  process.on('SIGINT', stop)
  process.on('SIGTERM', stop)
  try {
    await Promise.all(
      Array.from({ length: Math.min(2, jobs.length) }, async () => {
        while (index < jobs.length && !interrupted) {
          const job = jobs[index++]
          const executionId = randomUUID()
          const childEnv = {
            ...env,
            SOHA_E2E_TASK_ID: job.taskId,
            SOHA_E2E_EXECUTION_ID: executionId,
          }
          const context = runContext(childEnv)
          const child = spawn(process.execPath, ['scripts/run-e2e.mjs', 'mock', ...job.args], {
            env: childEnv,
            stdio: ['ignore', 'pipe', 'pipe'],
          })
          running.add(child)
          const record = {
            taskId: job.taskId,
            executionId,
            root: context.root,
            child,
            output: '',
            exitCode: null,
          }
          child.stdout.on('data', (data) => {
            record.output += data
          })
          child.stderr.on('data', (data) => {
            record.output += data
          })
          const done = new Promise((accept) => {
            child.once('error', (error) => {
              record.output += error.message
              accept(2)
            })
            child.once('close', (code) => accept(code ?? 1))
          })
          onStart(record)
          record.exitCode = await done
          running.delete(child)
          results.push(record)
        }
      }),
    )
    return { results, interrupted }
  } finally {
    process.off('SIGINT', stop)
    process.off('SIGTERM', stop)
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const jobs = process.argv.slice(2).map((value) => {
      const split = value.indexOf('=')
      if (split < 1 || split === value.length - 1)
        throw new Error(
          'Usage: npm run test:e2e:parallel -- task-a=e2e/path.spec.ts task-b=e2e/other.spec.ts',
        )
      return { taskId: value.slice(0, split), args: [value.slice(split + 1)] }
    })
    const batch = await runParallel(jobs)
    for (const result of batch.results) {
      if (result.exitCode !== 0) console.error(result.output)
      console.log(`${result.exitCode === 0 ? 'PASS' : 'FAIL'} ${result.taskId}: ${result.root}`)
    }
    process.exitCode = batch.interrupted
      ? 130
      : batch.results.some((run) => run.exitCode !== 0)
        ? 1
        : 0
  } catch (error) {
    console.error(error.message)
    process.exitCode = 2
  }
}
