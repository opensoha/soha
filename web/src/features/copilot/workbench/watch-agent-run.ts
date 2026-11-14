import { workbenchApi } from './api'
import { isRunningExternalAgentRun } from './agent-run-replay'
import type { WorkbenchAgentRun } from './types'

function waitForPoll(signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const abort = () => {
      clearTimeout(timer)
      reject(new DOMException('The operation was aborted.', 'AbortError'))
    }
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', abort)
      resolve()
    }, 1000)
    signal.addEventListener('abort', abort, { once: true })
    if (signal.aborted) abort()
  })
}

export async function watchAgentRun(
  sessionId: string,
  runId: string,
  signal: AbortSignal,
  onSnapshot: (run: WorkbenchAgentRun) => void,
) {
  while (!signal.aborted) {
    const response = await workbenchApi.agentRuns.session(sessionId)
    if (signal.aborted) break
    const run = response.data.find((item) => item.id === runId)
    if (!run) throw new Error('找不到助手任务，请刷新会话。')
    onSnapshot(run)
    if (!isRunningExternalAgentRun(run)) return run
    await waitForPoll(signal)
  }
  throw new DOMException('The operation was aborted.', 'AbortError')
}
