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
}: {
  onEvent: (event: { type: string }) => void
  onResyncRequired: () => void
}) {
  const stream = useKubernetesResourceStream({
    clusterId: 'cluster-a',
    namespace: 'team-a',
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
})
