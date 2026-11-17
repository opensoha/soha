/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useKubernetesResourceStream } from './resource-stream'

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
  onEvent,
  onResyncRequired,
  namespace = 'team-a',
  eventDebounceMs = 0,
  fallbackIntervalMs,
  onFallback,
}: {
  onEvent: (event: { type: string }) => void
  onResyncRequired: () => void
  namespace?: string
  eventDebounceMs?: number
  fallbackIntervalMs?: number | false
  onFallback?: () => void
}) {
  const stream = useKubernetesResourceStream({
    clusterId: 'cluster-a',
    namespace,
    eventDebounceMs,
    fallbackIntervalMs,
    onFallback,
    kinds: ['Pod'],
    onEvent,
    onResyncRequired,
  })
  return <span>{stream.status}</span>
}

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

describe('Kubernetes resource stream', () => {
  beforeEach(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('WebSocket', WebSocketMock)
    WebSocketMock.instances = []
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('waits for informer readiness and forwards reset events', async () => {
    const onEvent = vi.fn()
    const onResyncRequired = vi.fn()
    await act(async () =>
      root.render(<Probe onEvent={onEvent} onResyncRequired={onResyncRequired} />),
    )
    await act(async () => Promise.resolve())
    const socket = WebSocketMock.instances[0]
    expect(socket?.url).toContain('/api/v1/clusters/cluster-a/resources/stream')
    expect(socket?.url).toContain('namespace=team-a')

    await act(async () => socket?.onopen?.())
    expect(container.textContent).toBe('connecting')
    await act(async () =>
      socket?.onmessage?.({
        data: JSON.stringify({
          type: 'status',
          clusterId: 'cluster-a',
          observedAt: '2026-08-23T07:59:59Z',
          cacheStatus: 'live',
          resyncRequired: false,
        }),
      } as MessageEvent<string>),
    )
    expect(container.textContent).toBe('live')
    await act(async () =>
      socket?.onmessage?.({
        data: JSON.stringify({
          type: 'reset',
          clusterId: 'cluster-a',
          observedAt: '2026-08-23T08:00:00Z',
          resyncRequired: true,
        }),
      } as MessageEvent<string>),
    )
    expect(onEvent).not.toHaveBeenCalled()
    expect(onResyncRequired).toHaveBeenCalledOnce()
    expect(container.textContent).toBe('live')
  })

  it('coalesces collection refresh bursts into a bounded window and retains the latest event', async () => {
    vi.useFakeTimers()
    const onEvent = vi.fn()
    await act(async () =>
      root.render(<Probe onEvent={onEvent} onResyncRequired={vi.fn()} eventDebounceMs={250} />),
    )
    const socket = WebSocketMock.instances[0]
    for (let i = 0; i < 300; i += 1)
      await act(async () =>
        socket.onmessage?.({
          data: JSON.stringify({ type: 'modified', observedAt: String(i) }),
        } as MessageEvent<string>),
      )
    expect(onEvent).not.toHaveBeenCalled()
    await act(async () => vi.advanceTimersByTimeAsync(250))
    expect(onEvent).toHaveBeenCalledOnce()
    expect(onEvent).toHaveBeenLastCalledWith(expect.objectContaining({ observedAt: '299' }))
    await act(async () =>
      socket.onmessage?.({
        data: JSON.stringify({ type: 'deleted', observedAt: '300' }),
      } as MessageEvent<string>),
    )
    await act(async () => vi.advanceTimersByTimeAsync(250))
    expect(onEvent).toHaveBeenCalledTimes(2)
  })

  it('resyncs immediately, cancels obsolete refreshes and ignores old-scope socket messages', async () => {
    vi.useFakeTimers()
    const onEvent = vi.fn()
    const resync = vi.fn()
    const render = (namespace: string) =>
      act(async () =>
        root.render(
          <Probe
            namespace={namespace}
            onEvent={onEvent}
            onResyncRequired={resync}
            eventDebounceMs={250}
          />,
        ),
      )
    await render('a')
    const first = WebSocketMock.instances[0]
    const send = (socket: WebSocketMock, type: string) =>
      act(async () =>
        socket.onmessage?.({
          data: JSON.stringify({ type, observedAt: type }),
        } as MessageEvent<string>),
      )
    await send(first, 'modified')
    await send(first, 'reset')
    expect(resync).toHaveBeenCalledOnce()
    await act(async () => vi.advanceTimersByTimeAsync(250))
    expect(onEvent).not.toHaveBeenCalled()
    await send(first, 'modified')
    await render('b')
    await send(first, 'modified')
    await act(async () => vi.advanceTimersByTimeAsync(250))
    expect(onEvent).not.toHaveBeenCalled()
    const second = WebSocketMock.instances[1]
    await send(second, 'error')
    expect(resync).toHaveBeenCalledTimes(2)
    await send(second, 'modified')
    await act(async () => root.render(null))
    await act(async () => vi.advanceTimersByTimeAsync(250))
    expect(onEvent).not.toHaveBeenCalled()
  })

  it('retains unbatched event delivery and lets the query own the polling fallback', async () => {
    vi.useFakeTimers()
    const onEvent = vi.fn()
    const onFallback = vi.fn()
    await act(async () =>
      root.render(
        <Probe
          onEvent={onEvent}
          onResyncRequired={vi.fn()}
          onFallback={onFallback}
          fallbackIntervalMs={false}
        />,
      ),
    )
    const socket = WebSocketMock.instances[0]
    for (let i = 0; i < 3; i += 1)
      await act(async () =>
        socket.onmessage?.({
          data: JSON.stringify({ type: 'modified', observedAt: String(i) }),
        } as MessageEvent<string>),
      )
    expect(onEvent).toHaveBeenCalledTimes(3)
    await act(async () => vi.advanceTimersByTimeAsync(30_000))
    expect(onFallback).not.toHaveBeenCalled()
  })

  it('pauses fallback reads while hidden and resumes the same timer without delaying resync', async () => {
    vi.useFakeTimers()
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    const fallback = vi.fn()
    const resync = vi.fn()
    try {
      await act(async () =>
        root.render(<Probe onEvent={vi.fn()} onResyncRequired={resync} onFallback={fallback} />),
      )
      await act(async () => vi.advanceTimersByTimeAsync(15_000))
      expect(fallback).toHaveBeenCalledOnce()
      hidden.mockReturnValue(true)
      await act(async () => vi.advanceTimersByTimeAsync(30_000))
      expect(fallback).toHaveBeenCalledOnce()
      await act(async () =>
        WebSocketMock.instances[0].onmessage?.({
          data: JSON.stringify({ type: 'error', observedAt: 'now' }),
        } as MessageEvent<string>),
      )
      expect(resync).toHaveBeenCalledOnce()
      hidden.mockReturnValue(false)
      await act(async () => vi.advanceTimersByTimeAsync(15_000))
      expect(fallback).toHaveBeenCalledTimes(2)
    } finally {
      hidden.mockRestore()
    }
  })
})
