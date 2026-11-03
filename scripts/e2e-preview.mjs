import { preview } from 'vite'
import { runContext } from './e2e-run-context.mjs'

const context = runContext()
if (process.env.SOHA_E2E_MANAGED_RUN !== context.executionId || !process.send)
  throw new Error('BLOCKED: preview requires its managed parent')
const server = await preview({
  build: { outDir: context.dist },
  preview: { host: '127.0.0.1', port: context.port, strictPort: true },
})
let stopping = false
const stop = () => {
  if (stopping) return
  stopping = true
  server.httpServer.closeAllConnections()
  server.httpServer.close(() => process.exit(0))
}
process.on('SIGINT', stop)
process.on('SIGTERM', stop)
process.send({ ready: true, executionId: context.executionId })
