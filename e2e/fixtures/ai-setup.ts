import setup from './run-manifest'
import { writeFileSync } from 'node:fs'
import { runContext, runIdentity } from '../../scripts/e2e-run-context.mjs'

export default function aiSetup() {
  setup()
  const endpoint = new URL(process.env.MIDSCENE_MODEL_BASE_URL || 'http://invalid')
  if (
    process.env.SOHA_AI_GUARDED_RUN !== '1' ||
    endpoint.hostname !== '127.0.0.1' ||
    endpoint.port !== process.env.SOHA_AI_PROXY_PORT ||
    !/^soha-proxy-[a-f0-9]{32}$/.test(process.env.MIDSCENE_MODEL_API_KEY || '')
  ) {
    writeFileSync(
      runContext().manifest,
      JSON.stringify({
        ...runIdentity(),
        mode: 'ai-ui-mock',
        result: 'BLOCKED',
        reason: 'Use the guarded run-ai entry; direct SDK endpoints are forbidden',
      }),
    )
    throw new Error('BLOCKED: guarded model proxy required')
  }
}
