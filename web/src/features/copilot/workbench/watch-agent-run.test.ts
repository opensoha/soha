import { afterEach, expect, it, vi } from 'vitest'
import { workbenchApi } from './api'
import { watchAgentRun } from './watch-agent-run'
import type { WorkbenchAgentRun } from './types'

vi.mock('./api', () => ({ workbenchApi: { agentRuns: { session: vi.fn() } } }))
const run = (status: string): WorkbenchAgentRun => ({
  id: 'run-1',
  providerId: 'hermes',
  providerKind: 'hermes-api',
  capabilityId: 'general',
  status,
})
afterEach(() => {
  vi.useRealTimers()
  vi.resetAllMocks()
})
it('polls queued and running tasks through the terminal result', async () => {
  vi.useFakeTimers()
  vi.mocked(workbenchApi.agentRuns.session)
    .mockResolvedValueOnce({ data: [run('queued')] })
    .mockResolvedValueOnce({ data: [run('running')] })
    .mockResolvedValueOnce({ data: [run('completed')] })
  const snapshot = vi.fn()
  const pending = watchAgentRun('session-1', 'run-1', new AbortController().signal, snapshot)
  await vi.advanceTimersByTimeAsync(2000)
  expect((await pending).status).toBe('completed')
  expect(snapshot.mock.calls.map(([item]) => item.status)).toEqual([
    'queued',
    'running',
    'completed',
  ])
  expect(vi.getTimerCount()).toBe(0)
})
it('aborts the wait without polling again', async () => {
  vi.useFakeTimers()
  vi.mocked(workbenchApi.agentRuns.session).mockResolvedValue({ data: [run('running')] })
  const controller = new AbortController()
  const pending = watchAgentRun('session-1', 'run-1', controller.signal, vi.fn())
  const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })
  await vi.advanceTimersByTimeAsync(0)
  controller.abort()
  await rejected
  expect(workbenchApi.agentRuns.session).toHaveBeenCalledTimes(1)
  expect(vi.getTimerCount()).toBe(0)
})
it('reports a missing task instead of displaying completion', async () => {
  vi.mocked(workbenchApi.agentRuns.session).mockResolvedValue({ data: [] })
  await expect(
    watchAgentRun('session-1', 'run-1', new AbortController().signal, vi.fn()),
  ).rejects.toThrow('找不到助手任务')
})

it('keeps polling cancellation until the runner acknowledges stopping', async () => {
  vi.useFakeTimers()
  vi.mocked(workbenchApi.agentRuns.session)
    .mockResolvedValueOnce({
      data: [{ ...run('canceled'), output: { cancellationPending: true } }],
    })
    .mockResolvedValueOnce({
      data: [{ ...run('canceled'), output: { cancellationPending: false } }],
    })
  const snapshot = vi.fn()
  const pending = watchAgentRun('session-1', 'run-1', new AbortController().signal, snapshot)
  await vi.advanceTimersByTimeAsync(2000)
  expect((await pending).output?.cancellationPending).toBe(false)
  expect(snapshot).toHaveBeenCalledTimes(2)
  expect(vi.getTimerCount()).toBe(0)
})
