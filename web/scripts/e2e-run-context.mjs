import { createHash, randomUUID } from 'node:crypto'
import { createServer } from 'node:net'
import {
  mkdirSync,
  lstatSync,
  readFileSync,
  realpathSync,
  rmSync,
  writeFileSync,
  existsSync,
} from 'node:fs'
import { join, resolve } from 'node:path'
import { tmpdir } from 'node:os'

const identifier = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/

export function isolatedRuntimeEnv(env) {
  return Object.fromEntries(
    [
      'PATH',
      'TMPDIR',
      'USERPROFILE',
      'LOCALAPPDATA',
      'SystemRoot',
      'CI',
      'SOHA_E2E_TASK_ID',
      'SOHA_E2E_EXECUTION_ID',
      'SOHA_E2E_MANAGED_RUN',
      'SOHA_E2E_PORT',
      'SOHA_E2E_LIST_ONLY',
    ]
      .filter((key) => env[key])
      .map((key) => [key, env[key]]),
  )
}

function validId(value, name) {
  if (!identifier.test(value ?? '')) throw new Error(`BLOCKED: invalid ${name}`)
  return value
}

export function runContext(env = process.env, cwd = process.cwd()) {
  const taskId = env.SOHA_E2E_TASK_ID
  if (taskId !== undefined) validId(taskId, 'SOHA_E2E_TASK_ID')
  const executionId = env.SOHA_E2E_EXECUTION_ID
  if (executionId !== undefined) validId(executionId, 'SOHA_E2E_EXECUTION_ID')
  if (taskId && !executionId) throw new Error('BLOCKED: use the managed E2E entry for task runs')
  const rawPort = env.SOHA_E2E_PORT ?? '4179'
  if (!/^\d{4,5}$/.test(rawPort) || +rawPort < 1024 || +rawPort > 65535)
    throw new Error('BLOCKED: SOHA_E2E_PORT must be 1024..65535')
  const root = taskId
    ? resolve(cwd, 'test-results', 'tasks', taskId, executionId)
    : resolve(cwd, 'test-results')
  return {
    taskId: taskId ?? null,
    executionId: executionId ?? null,
    dataNamespace: executionId ? `${taskId ?? 'legacy'}-${executionId}` : null,
    port: +rawPort,
    baseURL: `http://127.0.0.1:${rawPort}`,
    root,
    manifest: join(root, 'run-manifest.json'),
    jsonReport: join(root, 'playwright.json'),
    aiReport: join(root, 'ai-run.json'),
    midscene: join(root, 'midscene'),
    dist: resolve(cwd, 'dist'),
    listOnly: env.SOHA_E2E_LIST_ONLY === '1',
  }
}

export function runIdentity(context = runContext()) {
  return {
    taskId: context.taskId,
    executionId: context.executionId,
    dataNamespace: context.dataNamespace,
  }
}

// Only create known directories; a symlink must never redirect cleanup or report writes.
export function privateDirectory(path) {
  try {
    mkdirSync(path, { mode: 0o700 })
  } catch (error) {
    if (error.code !== 'EEXIST') throw error
  }
  const stat = lstatSync(path)
  if (!stat.isDirectory() || stat.isSymbolicLink())
    throw new Error('BLOCKED: E2E directory is not a real directory')
}

export function defaultLockRoot() {
  // TMPDIR may be different for each verification task; host resource locks must be shared.
  return join(
    realpathSync(process.platform === 'win32' ? tmpdir() : '/tmp'),
    `soha-e2e-${process.getuid?.() ?? 'user'}`,
  )
}

export function acquireResource(key, owner, lockRoot = defaultLockRoot()) {
  privateDirectory(lockRoot)
  const path = join(lockRoot, createHash('sha256').update(key).digest('hex'))
  try {
    mkdirSync(path, { mode: 0o700 })
  } catch (error) {
    if (error.code === 'EEXIST')
      throw new Error(`BLOCKED: E2E resource is occupied; inspect owner at ${path}`, {
        cause: error,
      })
    throw error
  }
  const token = randomUUID()
  const receipt = join(path, 'owner.json')
  writeFileSync(receipt, JSON.stringify({ ...owner, pid: process.pid, token }), {
    mode: 0o600,
    flag: 'wx',
  })
  return () => {
    // A replaced/unknown lock is someone else's responsibility, never force-remove it.
    if (
      lstatSync(path).isSymbolicLink() ||
      JSON.parse(readFileSync(receipt, 'utf8')).token !== token
    )
      throw new Error(`BLOCKED: E2E lock ownership changed at ${path}`)
    rmSync(path, { recursive: true })
  }
}

export async function reservePreviewPort(requested, owner, lockRoot) {
  const server = createServer()
  await new Promise((accept, reject) => {
    server.once('error', reject)
    server.listen(requested ?? 0, '127.0.0.1', accept)
  })
  try {
    const port = server.address().port
    const release = acquireResource(`port:${port}`, owner, lockRoot)
    return { port, release }
  } finally {
    await new Promise((accept) => server.close(accept))
  }
}

export async function prepareRun(
  env,
  kind,
  { cwd = process.cwd(), lockRoot, target, getTarget } = {},
) {
  const releases = []
  let ownsReports = false
  let context = runContext(env, cwd)
  const owner = { ...runIdentity(context), kind }
  const release = () => {
    const failures = []
    for (const fn of releases.splice(0).reverse()) {
      try {
        fn()
      } catch (error) {
        failures.push(error.message)
      }
    }
    if (failures.length) throw new Error(failures.join('\n'))
  }
  try {
    privateDirectory(resolve(cwd, 'test-results'))
    if (context.taskId) {
      privateDirectory(resolve(cwd, 'test-results/tasks'))
      privateDirectory(resolve(cwd, 'test-results/tasks', context.taskId))
      // Persistent evidence is never silently reused, including completed executions.
      mkdirSync(context.root, { mode: 0o700 })
    } else {
      releases.push(acquireResource(`legacy:${realpathSync(cwd)}`, owner, lockRoot))
    }
    for (const path of [context.manifest, context.jsonReport, context.aiReport]) {
      if (existsSync(path) && !lstatSync(path).isFile())
        throw new Error('BLOCKED: report path must be a regular file')
      // lstat also rejects symlinks to existing files; dangling symlinks are checked separately.
      try {
        if (lstatSync(path).isSymbolicLink()) throw new Error('BLOCKED: report path is a symlink')
      } catch (error) {
        if (error.code !== 'ENOENT') throw error
      }
    }
    ownsReports = true
    if (getTarget) target = getTarget()
    if (kind === 'real' && target) {
      if (!target.runId || !/^[a-f0-9]{64}$/.test(target.containerId ?? ''))
        throw new Error('BLOCKED: invalid real resource identity')
      for (const key of [`real-run:${target.runId}`, `real-container:${target.containerId}`])
        releases.push(acquireResource(key, owner, lockRoot))
      env.SOHA_E2E_REAL_RUN_ID = target.runId
      env.SOHA_E2E_REAL_CONTAINER_ID = target.containerId
    }
    if (kind !== 'real') {
      const requested = env.SOHA_E2E_PORT ? context.port : context.taskId ? undefined : 4179
      const port = await reservePreviewPort(requested, owner, lockRoot)
      releases.push(port.release)
      env.SOHA_E2E_PORT = String(port.port)
      context = runContext(env, cwd)
    }
    for (const name of ['browser', 'ai-browser', 'real-private', 'midscene'])
      privateDirectory(join(context.root, name))
    writeFileSync(
      context.manifest,
      JSON.stringify({ ...runIdentity(context), result: 'RUNNING', tests: [] }, null, 2),
      { mode: 0o600 },
    )
    return { context, release }
  } catch (error) {
    if (ownsReports)
      writeFileSync(
        context.manifest,
        JSON.stringify(
          { ...runIdentity(context), result: 'BLOCKED', reason: error.message },
          null,
          2,
        ),
        { mode: 0o600 },
      )
    release()
    throw error
  }
}
