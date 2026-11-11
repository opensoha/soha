import { readFileSync } from 'node:fs'
import { assertTrustedDispatch } from './quality-guards.mjs'

if (process.argv.includes('--dispatch')) {
  assertTrustedDispatch(
    process.env.GITHUB_REF,
    process.env.GITHUB_SHA,
    process.env.SOHA_APPROVED_AI_SHA,
  )
  if (!/^[a-f0-9]{40}$/.test(process.env.SOHA_AI_CONTRACTS_SHA ?? ''))
    throw new Error('BLOCKED: exact contracts SHA required')
} else {
  const YAML = (await import('yaml')).default
  const file = YAML.parse(readFileSync('.github/workflows/quality-ai.yml', 'utf8'))
  if (
    Object.keys(file.on).join() !== 'workflow_dispatch' ||
    file.permissions?.contents !== 'read' ||
    Object.keys(file.permissions).length !== 1
  )
    throw new Error('Unsafe enhanced trigger/permissions')
  for (const job of Object.values(file.jobs)) {
    if (!job.environment || !job['timeout-minutes'])
      throw new Error('Protected environment and job timeout required')
    if (
      job.steps[0].with?.ref !== '${{ vars.SOHA_APPROVED_AI_SHA }}' ||
      !job.if.includes("vars.SOHA_APPROVED_AI_SHA != ''")
    )
      throw new Error('Approved source checkout required before executing scripts')
    for (const step of job.steps)
      if (step.uses && !/@[a-f0-9]{40}$/.test(step.uses))
        throw new Error('Exact action SHA required')
  }
  console.log('Enhanced workflow: manual, read-only, protected environment, fixed actions')
}
