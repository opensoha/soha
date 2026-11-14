/** @vitest-environment jsdom */

import { ApiError } from '@/services/api-error'
import type { AlertEventStreamSignal } from '@opensoha/contracts/gen/ts/sohaapi'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAlertEventStream } from './use-alert-event-stream'

const withStreamTicket = vi.fn(async (url: URL | string) => String(url))

vi.mock('@/features/auth', () => ({
  buildSameOriginStreamURL: (path: string) => new URL(path, 'ws://localhost'),
  withStreamTicket: (url: URL | string) => withStreamTicket(url),
}))

class WebSocketMock {
  static instances: WebSocketMock[] = []
  static OPEN = 1
  readyState = WebSocketMock.OPEN
  onopen?: () => void
  onmessage?: (event: MessageEvent<string>) => void
  onclose?: () => void
  onerror?: () => void
  constructor(readonly url: string) {
    WebSocketMock.instances.push(this)
  }
  close() {}
}

function Probe({
  onSignal,
  onFallback,
}: {
  onSignal: (signal: AlertEventStreamSignal) => void
  onFallback?: () => void
}) {
  const stream = useAlertEventStream({ clusterId: 'cluster-a', onSignal, onFallback })
  return (
    <>
      <span>{stream.status}</span>
      {stream.error ? <button onClick={stream.reconnect}>恢复连接</button> : null}
    </>
  )
}

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

describe('alert event stream', () => {
  beforeEach(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('WebSocket', WebSocketMock)
    WebSocketMock.instances = []
    withStreamTicket.mockReset().mockImplementation(async (url) => String(url))
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('binds cluster scope and treats the initial reset as a live resync signal', async () => {
    const onSignal = vi.fn()
    await act(async () => root.render(<Probe onSignal={onSignal} />))
    await act(async () => Promise.resolve())
    const socket = WebSocketMock.instances[0]
    expect(socket?.url).toContain('/api/v1/alert-events/stream')
    expect(socket?.url).toContain('clusterId=cluster-a')

    await act(async () => socket?.onopen?.())
    expect(container.textContent).toBe('connecting')
    await act(async () =>
      socket?.onmessage?.({
        data: JSON.stringify({
          type: 'reset',
          observedAt: '2026-08-23T08:00:00Z',
          clusterId: 'cluster-a',
          resyncRequired: true,
        }),
      } as MessageEvent<string>),
    )
    expect(onSignal).toHaveBeenCalledWith(expect.objectContaining({ type: 'reset' }))
    expect(container.textContent).toBe('live')
  })
  it('stops reconnect and fallback after denied tickets, then resumes only on explicit reconnect', async () => {
    vi.useFakeTimers()
    withStreamTicket.mockRejectedValueOnce(new ApiError(403, 'ticket refused', { method: 'POST' }))
    const onSignal = vi.fn()
    const onFallback = vi.fn()
    await act(async () => root.render(<Probe onSignal={onSignal} onFallback={onFallback} />))
    expect(container.querySelector('span')?.textContent).toBe('degraded')
    await act(async () => vi.advanceTimersByTimeAsync(60_000))
    expect(withStreamTicket).toHaveBeenCalledTimes(1)
    expect(WebSocketMock.instances).toHaveLength(0)
    expect(onFallback).not.toHaveBeenCalled()
    await act(async () => container.querySelector('button')!.click())
    expect(withStreamTicket).toHaveBeenCalledTimes(2)
    expect(WebSocketMock.instances).toHaveLength(1)
    expect(container.querySelector('button')).toBeNull()
    await act(async () =>
      WebSocketMock.instances[0].onmessage?.({
        data: JSON.stringify({
          type: 'reset',
          clusterId: 'cluster-a',
          observedAt: '2026-10-16T00:00:00Z',
        }),
      } as MessageEvent<string>),
    )
    expect(container.textContent).toBe('live')
    expect(onSignal).toHaveBeenCalledOnce()
  })
})
