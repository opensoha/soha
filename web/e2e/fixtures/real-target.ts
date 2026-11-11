import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { validateTarget } from '../../scripts/quality-guards.mjs'
import { runContext, runIdentity } from '../../scripts/e2e-run-context.mjs'

export function loadTarget() {
  if (!process.env.SOHA_E2E_TARGET_FILE)
    throw new Error('BLOCKED: SOHA_E2E_TARGET_FILE required; no development/default target')
  const target = JSON.parse(readFileSync(process.env.SOHA_E2E_TARGET_FILE, 'utf8'))
  const inspected = JSON.parse(
    execFileSync('docker', ['inspect', target.containerId], { encoding: 'utf8' }),
  )[0]
  validateTarget(target, inspected)
  if (
    target.runId !== process.env.SOHA_E2E_REAL_RUN_ID ||
    target.containerId !== process.env.SOHA_E2E_REAL_CONTAINER_ID
  )
    throw new Error('BLOCKED: target changed after acquiring exclusive environment ownership')
  if (
    !['e2e-direct', 'e2e-agent'].includes(target.mode) ||
    !target.clusterId ||
    !target.namespace ||
    !target.approvalId
  )
    throw new Error('BLOCKED: dedicated scope/mode/approval required')
  for (const name of ['loginEnv', 'passwordEnv'])
    if (!/^SOHA_E2E_[A-Z_]+$/.test(target[name] ?? '') || !process.env[target[name]])
      throw new Error(`BLOCKED: dedicated ${name} credential reference required`)
  return target
}

export default function setup() {
  const run = runContext()
  mkdirSync(run.root, { recursive: true })
  const path = run.manifest
  try {
    const target = loadTarget()
    writeFileSync(
      path,
      JSON.stringify(
        {
          ...runIdentity(run),
          runId: target.runId,
          mode: target.mode,
          sources: target.sources,
          environment: {
            disposable: true,
            containerId: target.containerId,
            imageDigest: target.imageDigest,
          },
          result: 'RUNNING',
        },
        null,
        2,
      ),
    )
  } catch (error) {
    writeFileSync(
      path,
      JSON.stringify(
        {
          ...runIdentity(run),
          mode: 'e2e-real',
          result: 'BLOCKED',
          reason: (error as Error).message,
        },
        null,
        2,
      ),
    )
    throw error
  }
}
