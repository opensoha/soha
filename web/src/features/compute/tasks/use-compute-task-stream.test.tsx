/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ComputeTaskStreamEvent } from '@opensoha/contracts/gen/ts/sohaapi'
import { computeKeys } from '../keys'
import { useComputeTaskStream } from './use-compute-task-stream'

const withStreamTicket = vi.fn(async (url: URL | string) => String(url))

vi.mock('@/features/auth', () => ({
  buildSameOriginStreamURL: (path: string) => new URL(path, 'http://localhost'),
  withStreamTicket: (url: URL | string) => withStreamTicket(url),
}))

class EventSourceMock {
  static instances: EventSourceMock[] = []
  onopen?: () => void
  onmessage?: (event: MessageEvent<string>) => void
  onerror?: () => void
  closed = false
  constructor(readonly url: string) {
    EventSourceMock.instances.push(this)
  }
  close() {
    this.closed = true
  }
}

function Probe({ enabled = true }: { enabled?: boolean }) {
  const stream = useComputeTaskStream({ domain: 'virtualization', taskId: 'task/one', enabled })
  return <span>{stream.status}</span>
}

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
let queryClient: QueryClient

describe('compute task stream', () => {
  beforeEach(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('EventSource', EventSourceMock)
    EventSourceMock.instances = []
    queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    queryClient.clear()
    container.remove()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('updates canonical task cache and closes on terminal events', async () => {
    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <Probe />
        </QueryClientProvider>,
      )
      await Promise.resolve()
    })

    const source = EventSourceMock.instances[0]
    expect(source?.url).toContain('/api/v1/compute/tasks/virtualization/task%2Fone/stream')
    await act(async () => source?.onopen?.())
    expect(container.textContent).toBe('live')

    const event: ComputeTaskStreamEvent = {
      type: 'terminal',
      observedAt: '2026-08-23T12:00:00Z',
      task: {
        id: 'task/one',
        domain: 'virtualization',
        sourceType: 'virtualization_task',
        sourceId: 'task/one',
        kind: 'vm_action',
        category: 'lifecycle',
        normalizedStatus: 'succeeded',
        rawStatus: 'completed',
        resources: [],
        attemptCount: 1,
        cancelable: false,
        retryable: false,
        availableActions: ['logs'],
        createdAt: '2026-08-23T11:59:00Z',
      },
    }
    await act(async () =>
      source?.onmessage?.({ data: JSON.stringify(event) } as MessageEvent<string>),
    )

    expect(container.textContent).toBe('done')
    expect(source?.closed).toBe(true)
    expect(
      queryClient.getQueryData<{ data: { normalizedStatus: string } }>(
        computeKeys.task('virtualization', 'task/one'),
      )?.data.normalizedStatus,
    ).toBe('succeeded')
  })

  it('falls back after repeated disconnects and closing the view stops subscriptions without canceling the task', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const render = (enabled: boolean) =>
      root.render(
        <QueryClientProvider client={queryClient}>
          <Probe enabled={enabled} />
        </QueryClientProvider>,
      )
    await act(async () => {
      render(true)
      await Promise.resolve()
    })
    await act(async () => EventSourceMock.instances[0].onerror?.())
    expect(container.textContent).toBe('reconnecting')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000)
    })
    await act(async () => EventSourceMock.instances[1].onerror?.())
    await act(async () => {
      await vi.advanceTimersByTimeAsync(4000)
    })
    await act(async () => EventSourceMock.instances[2].onerror?.())
    expect(container.textContent).toBe('polling')
    expect(EventSourceMock.instances.every((source) => source.closed)).toBe(true)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(9000)
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: computeKeys.task('virtualization', 'task/one'),
    })
    const connections = EventSourceMock.instances.length
    const refreshes = invalidate.mock.calls.length
    await act(async () => {
      render(false)
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(30000)
    })
    expect(container.textContent).toBe('idle')
    expect(EventSourceMock.instances.every((source) => source.closed)).toBe(true)
    expect(EventSourceMock.instances).toHaveLength(connections)
    expect(invalidate).toHaveBeenCalledTimes(refreshes)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
