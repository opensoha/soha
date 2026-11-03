import { createHash, randomUUID } from 'node:crypto'
import { mkdirSync, writeFileSync } from 'node:fs'
import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { sourceEvidence } from '../../scripts/quality-guards.mjs'
import { runContext, runIdentity } from '../../scripts/e2e-run-context.mjs'

export default function setup() {
  const run = runContext()
  mkdirSync(run.root, { recursive: true })
  const manifest = {
    ...runIdentity(run),
    runId: randomUUID(),
    mode: process.env.SOHA_TEST_MODE || 'ui-mock',
    sources: {
      web: sourceEvidence('.'),
      contracts: existsSync('../soha-contracts/.git') ? sourceEvidence('../soha-contracts') : null,
    },
    environment: {
      id: 'synthetic-mocks',
      disposable: true,
      baseURL: run.baseURL,
      dataNamespace: run.dataNamespace,
    },
    artifact: {
      directory: run.dist,
      indexDigest: createHash('sha256')
        .update(readFileSync(join(run.dist, 'index.html')))
        .digest('hex'),
      manifestDigest: createHash('sha256')
        .update(readFileSync(join(run.dist, '.vite/manifest.json')))
        .digest('hex'),
    },
    toolchain: { node: process.version, playwright: '1.63.0' },
    result: 'RUNNING',
  }
  writeFileSync(run.manifest, JSON.stringify(manifest, null, 2), { mode: 0o600 })
}
