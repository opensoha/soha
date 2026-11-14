import { readFileSync, writeFileSync } from 'node:fs'
import type { FullResult, Reporter, TestCase, TestResult } from '@playwright/test/reporter'
import { runContext } from '../../scripts/e2e-run-context.mjs'

export default class ResultReporter implements Reporter {
  constructor() {
    if (
      !process.env.SOHA_E2E_MANAGED_RUN ||
      process.env.SOHA_E2E_MANAGED_RUN !== runContext().executionId
    )
      throw new Error(
        'BLOCKED: use npm run test:e2e (or the real/AI entry) to acquire run resources',
      )
  }
  tests: { name: string; result: string }[] = []
  onTestEnd(test: TestCase, result: TestResult) {
    this.tests.push({ name: test.title, result: result.status })
  }
  onEnd(result: FullResult) {
    const run = runContext()
    const path = run.manifest
    const manifest = JSON.parse(readFileSync(path, 'utf8'))
    if (manifest.executionId !== run.executionId || manifest.taskId !== run.taskId)
      throw new Error('BLOCKED: report identity changed during execution')
    if (manifest.result === 'BLOCKED') return
    Object.assign(manifest, {
      result:
        result.status === 'passed' &&
        this.tests.length > 0 &&
        this.tests.every((test) => test.result === 'passed')
          ? 'PASS'
          : 'FAIL',
      elapsedMs: result.duration,
      tests: this.tests,
    })
    writeFileSync(path, JSON.stringify(manifest, null, 2))
  }
}
