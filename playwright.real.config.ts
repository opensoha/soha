import { defineConfig } from '@playwright/test'
import { join } from 'node:path'
import { runContext } from './scripts/e2e-run-context.mjs'

const run = runContext()

// Playwright 1.63's failure ARIA snapshot includes password input values.
// Disable it before workers start; real auth must never enter error-context artifacts.
process.env.PLAYWRIGHT_NO_COPY_PROMPT = '1'

export default defineConfig({
  testDir: './e2e',
  testMatch: /(?:api|flows)\/.*\.spec\.ts$/,
  globalSetup: './e2e/fixtures/real-target.ts',
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  timeout: 60_000,
  reporter: run.listOnly ? [['list']] : [['list'], ['./e2e/fixtures/result-reporter.ts']],
  outputDir: join(run.root, 'real-private'),
  use: {
    browserName: 'chromium',
    serviceWorkers: 'block',
    screenshot: 'off',
    trace: 'off',
    locale: 'zh-CN',
  },
})
