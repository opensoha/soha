import { spawn, execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { setTimeout as delay } from 'node:timers/promises'
import { isolatedRuntimeEnv, prepareRun, runContext } from './e2e-run-context.mjs'
import { aiPreflight, validateTarget } from './quality-guards.mjs'

const [kind, ...inputArgs] = process.argv.slice(2)
const args = inputArgs.filter((value) => value !== '--')
let run
let child
let preview
let previewDone
let interrupted = false
let forced = false
let killTimer
const signalProcess = (owned, signal) => {
  if (!owned?.pid) return
  if (owned === preview && (owned.exitCode !== null || owned.signalCode !== null)) return
  if (signal === 'SIGKILL') forced = true
  try {
    if (process.platform !== 'win32') process.kill(-owned.pid, signal)
    else owned.kill(signal)
  } catch (error) {
    if (error.code !== 'ESRCH') throw error
  }
}
const forwardSignal = (signal) => {
  interrupted = true
  if (!child) {
    signalProcess(preview, 'SIGTERM')
    return
  }
  // Playwright handles SIGINT for teardown; SIGTERM would bypass its cleanup.
  signalProcess(child, signal === 'SIGTERM' ? 'SIGINT' : signal)
  killTimer ??= setTimeout(() => signalProcess(child, 'SIGKILL'), 10_000)
}
const onTerm = () => forwardSignal('SIGTERM')
const onInt = () => forwardSignal('SIGINT')
process.on('SIGTERM', onTerm)
process.on('SIGINT', onInt)

function finalizeRun(descendantsGone) {
  if (run && process.exitCode !== 0) {
    const manifest = JSON.parse(readFileSync(run.context.manifest, 'utf8'))
    if (manifest.executionId !== run.context.executionId)
      throw new Error('BLOCKED: refusing to update another execution report')
    if (manifest.result !== 'BLOCKED') {
      manifest.result = 'FAIL'
      manifest.interrupted = interrupted
      writeFileSync(run.context.manifest, JSON.stringify(manifest, null, 2), { mode: 0o600 })
    }
  }
  if (!descendantsGone || forced)
    throw new Error(
      "BLOCKED: E2E cleanup is uncertain after forced shutdown; keep this run's resource locks for recovery",
    )
  run?.release()
}

try {
  if (!['mock', 'real', 'ai'].includes(kind)) throw new Error('BLOCKED: unknown E2E mode')
  if (args.some((arg) => /^(?:--(?:config|output|reporter)(?:=|$)|-c)/.test(arg)))
    throw new Error(
      'BLOCKED: managed E2E owns config, reports and output; pass test paths or filters',
    )
  const env = { ...process.env }
  // Playwright gives these environment variables precedence over reporter.outputFile.
  for (const key of [
    'PLAYWRIGHT_JSON_OUTPUT_FILE',
    'PLAYWRIGHT_JSON_OUTPUT_DIR',
    'PLAYWRIGHT_JSON_OUTPUT_NAME',
  ])
    delete env[key]
  env.SOHA_E2E_EXECUTION_ID ??= randomUUID()
  env.SOHA_E2E_MANAGED_RUN = env.SOHA_E2E_EXECUTION_ID
  env.SOHA_E2E_LIST_ONLY = args.includes('--list') ? '1' : '0'
  const listOnly = env.SOHA_E2E_LIST_ONLY === '1'
  runContext(env) // Reject invalid user inputs before any resource is touched.
  const getTarget = () => {
    if (kind !== 'real' || !env.SOHA_E2E_TARGET_FILE) return undefined
    let target
    try {
      target = JSON.parse(readFileSync(env.SOHA_E2E_TARGET_FILE, 'utf8'))
    } catch {
      throw new Error('BLOCKED: cannot read dedicated real target')
    }
    const inspected = JSON.parse(
      execFileSync('docker', ['inspect', target.containerId], {
        encoding: 'utf8',
        stdio: ['ignore', 'pipe', 'pipe'],
      }),
    )[0]
    validateTarget(target, inspected)
    return target
  }
  if (!listOnly) run = await prepareRun(env, kind, { getTarget })
  if (interrupted) throw new Error('BLOCKED: interrupted before E2E startup')
  const needsPreview =
    !listOnly && (kind === 'mock' || (kind === 'ai' && aiPreflight(env).result === 'ENABLED'))
  if (needsPreview) {
    preview = spawn(process.execPath, ['scripts/e2e-preview.mjs'], {
      env: kind === 'ai' ? isolatedRuntimeEnv(env) : env,
      stdio: ['ignore', 'inherit', 'inherit', 'ipc'],
      detached: process.platform !== 'win32',
    })
    previewDone = new Promise((accept) => {
      preview.once('error', () => accept(2))
      preview.once('close', (code) => accept(code ?? 1))
    })
    let timer
    try {
      await new Promise((accept, reject) => {
        preview.once('error', reject)
        preview.once('close', () =>
          reject(new Error('BLOCKED: owned preview exited before readiness')),
        )
        preview.on('message', (message) => {
          if (message?.ready && message.executionId === env.SOHA_E2E_EXECUTION_ID) accept()
        })
        timer = setTimeout(
          () => reject(new Error('BLOCKED: owned preview startup timed out')),
          60_000,
        )
      })
    } finally {
      clearTimeout(timer)
    }
    if (interrupted) throw new Error('BLOCKED: interrupted during preview startup')
  }
  const command =
    kind === 'ai' && !listOnly
      ? ['scripts/run-ai.mjs', ...args]
      : [
          'node_modules/@playwright/test/cli.js',
          'test',
          '--config',
          kind === 'real'
            ? 'playwright.real.config.ts'
            : kind === 'ai'
              ? 'playwright.ai.config.ts'
              : 'playwright.config.ts',
          ...args,
        ]
  if (run)
    console.log(
      `E2E ${run.context.taskId ?? 'legacy'}/${run.context.executionId}: ${run.context.manifest}`,
    )
  child = spawn(process.execPath, command, {
    env,
    stdio: 'inherit',
    detached: process.platform !== 'win32',
  })
  const code = await new Promise((accept, reject) => {
    child.once('error', reject)
    child.once('close', (value) => accept(value ?? 1))
  })
  let accepted = listOnly
  if (run) {
    const report = JSON.parse(readFileSync(run.context.manifest, 'utf8'))
    if (report.taskId !== run.context.taskId || report.executionId !== run.context.executionId)
      throw new Error('BLOCKED: completion report does not belong to this invocation')
    accepted =
      report.result === 'PASS' ||
      (kind === 'ai' && env.SOHA_AI_ENABLED !== '1' && report.result === 'NOT_RUN')
  }
  process.exitCode = interrupted ? 130 : code
  if (!interrupted && code === 0 && ((preview && preview.exitCode !== null) || !accepted))
    process.exitCode = 1
} catch (error) {
  console.error(error.message)
  process.exitCode = interrupted ? 130 : 2
  if (run) {
    const manifest = JSON.parse(readFileSync(run.context.manifest, 'utf8'))
    if (manifest.executionId === run.context.executionId) {
      manifest.result = 'BLOCKED'
      manifest.reason = error.message
      writeFileSync(run.context.manifest, JSON.stringify(manifest, null, 2), { mode: 0o600 })
      if (kind === 'ai')
        writeFileSync(run.context.aiReport, JSON.stringify(manifest, null, 2), { mode: 0o600 })
    }
  }
} finally {
  let descendantsGone = true
  if (child?.pid && process.platform !== 'win32') {
    const alive = () => {
      try {
        process.kill(-child.pid, 0)
        return true
      } catch (error) {
        if (error.code === 'ESRCH') return false
        throw error
      }
    }
    if (alive()) {
      signalProcess(child, 'SIGINT')
      killTimer ??= setTimeout(() => signalProcess(child, 'SIGKILL'), 10_000)
    }
    const deadline = Date.now() + 12_000
    while (alive() && Date.now() < deadline) await delay(50)
    descendantsGone = !alive()
  }
  if (preview) {
    signalProcess(preview, 'SIGTERM')
    const timer = setTimeout(() => signalProcess(preview, 'SIGKILL'), 5_000)
    await previewDone
    clearTimeout(timer)
  }
  clearTimeout(killTimer)
  try {
    finalizeRun(descendantsGone)
  } catch (error) {
    console.error(error.message)
    process.exitCode = 2
  }
  process.off('SIGTERM', onTerm)
  process.off('SIGINT', onInt)
}
