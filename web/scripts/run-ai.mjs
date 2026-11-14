import { spawn } from 'node:child_process'
import { createRequire } from 'node:module'
import { readFileSync, writeFileSync } from 'node:fs'
import { aiPreflight } from './quality-guards.mjs'
import { createModelProxy } from './model-proxy.mjs'
import { randomUUID } from 'node:crypto'
import { isolatedRuntimeEnv, runContext, runIdentity } from './e2e-run-context.mjs'

const run = runContext()
if (!run.executionId || process.env.SOHA_E2E_MANAGED_RUN !== run.executionId)
  throw new Error('BLOCKED: use npm run test:e2e:ai to acquire run resources')
let result = { result: 'NOT_RUN' }
let proxy
let timer
try {
  const approval = aiPreflight(process.env)
  if (approval.result === 'NOT_RUN') {
    result.reason = 'SOHA_AI_ENABLED=1 was not explicitly requested'
  } else {
    const capability = 'soha-proxy-' + randomUUID().replace(/-/g, '')
    proxy = createModelProxy(
      process.env.MIDSCENE_MODEL_BASE_URL,
      process.env.MIDSCENE_MODEL_API_KEY,
      approval.maxCalls,
      fetch,
      capability,
    )
    await new Promise((resolve) => proxy.server.listen(0, '127.0.0.1', resolve))
    const env = isolatedRuntimeEnv(process.env)
    // Only this fixed endpoint/key reaches the SDK. Real credentials remain in the proxy.
    Object.assign(env, {
      MIDSCENE_MODEL_BASE_URL: `http://127.0.0.1:${proxy.server.address().port}/v1`,
      MIDSCENE_MODEL_API_KEY: capability,
      SOHA_AI_GUARDED_RUN: '1',
      SOHA_AI_PROXY_PORT: String(proxy.server.address().port),
      MIDSCENE_MODEL_NAME: process.env.MIDSCENE_MODEL_NAME,
      MIDSCENE_MODEL_FAMILY: process.env.MIDSCENE_MODEL_FAMILY,
      SOHA_TEST_MODE: 'ai-ui-mock',
      SOHA_E2E_EXECUTION_ID: run.executionId,
      SOHA_E2E_MANAGED_RUN: run.executionId,
      SOHA_E2E_PORT: String(run.port),
      MIDSCENE_RUN_DIR: run.midscene,
    })
    if (run.taskId) env.SOHA_E2E_TASK_ID = run.taskId
    const child = spawn(
      process.execPath,
      [
        createRequire(import.meta.url).resolve('@playwright/test/cli'),
        'test',
        '--config',
        'playwright.ai.config.ts',
        ...process.argv.slice(2),
      ],
      { env, stdio: 'inherit' },
    )
    timer = setTimeout(() => child.kill('SIGTERM'), 930_000)
    const code = await new Promise((resolve, reject) => {
      child.once('error', reject)
      child.once('exit', resolve)
    })
    clearTimeout(timer)
    const manifest = JSON.parse(readFileSync(run.manifest, 'utf8'))
    result = {
      result:
        code === 0 &&
        proxy.summary().rejected === 0 &&
        manifest.executionId === run.executionId &&
        manifest.result === 'PASS'
          ? 'PASS'
          : 'FAIL',
      ...proxy.summary(),
      approvalId: process.env.SOHA_AI_APPROVAL_ID,
    }
    process.exitCode = result.result === 'PASS' ? 0 : 1
  }
} catch (error) {
  result = { result: 'BLOCKED', reason: error.message }
  process.exitCode = 2
} finally {
  clearTimeout(timer)
  if (proxy) {
    proxy.server.closeAllConnections()
    await new Promise((resolve) => proxy.server.close(resolve))
  }
  result = { ...runIdentity(run), ...result }
  writeFileSync(run.aiReport, JSON.stringify(result, null, 2), { mode: 0o600 })
  if (result.result === 'NOT_RUN' || result.result === 'BLOCKED')
    writeFileSync(
      run.manifest,
      JSON.stringify({ ...result, mode: 'ai-ui-mock', tests: [] }, null, 2),
      { mode: 0o600 },
    )
  console.log(JSON.stringify(result))
}
