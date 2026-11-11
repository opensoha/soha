import { defineConfig } from '@playwright/test'
import basic from './playwright.config'
import { join } from 'node:path'
import { runContext } from './scripts/e2e-run-context.mjs'

export default defineConfig({
  ...basic,
  globalSetup: './e2e/fixtures/ai-setup.ts',
  testMatch: /ai\/.*\.spec\.ts$/,
  testIgnore: [],
  timeout: 180_000,
  globalTimeout: 900_000,
  workers: 1,
  retries: 0,
  outputDir: join(runContext().root, 'ai-browser'),
})
