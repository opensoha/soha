import { defineConfig } from '@playwright/test'
import { join } from 'node:path'
import { runContext } from './scripts/e2e-run-context.mjs'

const run = runContext()

export default defineConfig({
  testDir: './e2e',
  globalSetup: './e2e/fixtures/run-manifest.ts',
  testMatch: /(?:ui-mock|visual)\/.*\.spec\.ts$/,
  testIgnore: ['**/ai/**', '**/api/**', '**/flows/**'],
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  timeout: 30_000,
  reporter: run.listOnly
    ? [['list']]
    : [['list'], ['./e2e/fixtures/result-reporter.ts'], ['json', { outputFile: run.jsonReport }]],
  outputDir: join(run.root, 'browser'),
  use: {
    browserName: 'chromium',
    baseURL: run.baseURL,
    viewport: { width: 1440, height: 1000 },
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    serviceWorkers: 'block',
    trace: 'off',
    screenshot: 'only-on-failure',
  },
  // The managed entry owns preview directly, including its IPC readiness and shutdown.
  // It never probes/reuses someone else's HTTP server; Vite still enforces strictPort.
})
