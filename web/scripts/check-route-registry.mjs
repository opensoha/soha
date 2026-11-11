#!/usr/bin/env node

import { execFileSync } from 'node:child_process'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDirectory = dirname(fileURLToPath(import.meta.url))
const projectRoot = resolve(scriptDirectory, '..')
const vitestEntry = resolve(projectRoot, 'node_modules/vitest/vitest.mjs')

try {
  execFileSync(process.execPath, [vitestEntry, 'run', 'src/routes/route-validation.test.ts'], {
    cwd: projectRoot,
    env: { ...process.env, CI: '1' },
    stdio: 'inherit',
  })
} catch (error) {
  process.exit(typeof error.status === 'number' ? error.status : 1)
}
