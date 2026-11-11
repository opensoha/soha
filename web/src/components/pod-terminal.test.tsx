/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PodTerminal } from './pod-terminal'

interface TerminalMockInstance {
  cols: number
  rows: number
}

const terminalMocks = vi.hoisted(() => ({
  fit: vi.fn(),
  instances: [] as TerminalMockInstance[],
}))

vi.mock('@xterm/xterm', () => ({
  Terminal: class {
    cols = 80
    rows = 24

    constructor() {
      terminalMocks.instances.push(this)
    }

    loadAddon() {}
    open() {}
    focus() {}
    write() {}
    writeln() {}
    dispose() {}
    onData() {}
  },
}))

vi.mock('@xterm/addon-fit', () => ({
  FitAddon: class {
    fit() {
      terminalMocks.fit()
    }
  },
}))

vi.mock('@/features/auth/stream-ticket', () => ({
  buildSameOriginStreamURL: (path: string) => new URL(path, 'http://localhost'),
  withStreamTicket: async (url: string) => url,
}))

vi.mock('@/i18n', () => {
  const t = (_key: string, fallback?: string) => fallback ?? _key
  return { useI18n: () => ({ localeCode: 'zh_CN' as const, t }) }
})

vi.mock('@/theme/app-theme', () => ({
  readTerminalThemeColors: () => ({}),
}))

class WebSocketMock {
  static readonly OPEN = 1
  static instances: WebSocketMock[] = []

  readyState = 0
  sent: string[] = []
  onopen: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null

  constructor(readonly url: string) {
    WebSocketMock.instances.push(this)
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    this.readyState = 3
  }
}

class ResizeObserverMock {
  static instances: ResizeObserverMock[] = []

  target: Element | null = null

  constructor(readonly callback: ResizeObserverCallback) {
    ResizeObserverMock.instances.push(this)
  }

  observe(target: Element) {
    this.target = target
  }

  unobserve() {}
  disconnect() {}
}

const roots: Root[] = []

beforeEach(() => {
  vi.useFakeTimers()
  WebSocketMock.instances = []
  ResizeObserverMock.instances = []
  terminalMocks.instances = []
  terminalMocks.fit.mockReset()
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('WebSocket', WebSocketMock)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    callback(0)
    return 1
  })
  vi.stubGlobal('cancelAnimationFrame', vi.fn())
})

afterEach(async () => {
  await act(async () => {
    roots.splice(0).forEach((root) => root.unmount())
  })
  document.body.innerHTML = ''
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('PodTerminal', () => {
  it('sends row-only changes and coalesces resize events', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    roots.push(root)

    await act(async () => {
      root.render(<PodTerminal clusterId="cluster-a" namespace="default" podName="pod-a" />)
      await Promise.resolve()
      await Promise.resolve()
    })

    const socket = WebSocketMock.instances[0]
    expect(socket).toBeDefined()
    await act(async () => {
      socket.readyState = WebSocketMock.OPEN
      socket.onopen?.()
    })

    const messages = () => socket.sent.map((message) => JSON.parse(message))
    expect(messages().filter((message) => message.type === 'resize')).toEqual([
      { type: 'resize', cols: 80, rows: 24 },
    ])

    const terminal = terminalMocks.instances[0]
    const observer = ResizeObserverMock.instances.find((instance) =>
      instance.target?.classList.contains('soha-terminal-shell-inner'),
    )
    expect(observer).toBeDefined()

    await act(async () => {
      for (let rows = 25; rows <= 30; rows += 1) {
        terminal.rows = rows
        observer?.callback([], observer as unknown as ResizeObserver)
      }
    })

    expect(messages().filter((message) => message.type === 'resize')).toHaveLength(1)
    await act(async () => {
      vi.runOnlyPendingTimers()
    })
    expect(messages().filter((message) => message.type === 'resize')).toEqual([
      { type: 'resize', cols: 80, rows: 24 },
      { type: 'resize', cols: 80, rows: 30 },
    ])

    await act(async () => {
      for (let cols = 81; cols <= 85; cols += 1) {
        terminal.cols = cols
        observer?.callback([], observer as unknown as ResizeObserver)
      }
    })
    expect(messages().filter((message) => message.type === 'resize')).toHaveLength(2)
    await act(async () => {
      vi.runOnlyPendingTimers()
    })

    const resizeMessages = messages().filter((message) => message.type === 'resize')
    expect(resizeMessages).toHaveLength(3)
    expect(resizeMessages[resizeMessages.length - 1]).toMatchObject({ cols: 85, rows: 30 })
    expect(messages().filter((message) => message.type === 'input')).toHaveLength(0)
  })
  it('pins UID, revalidates every reconnect, and closes without injecting commands', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    roots.push(root)
    const guard = vi.fn().mockResolvedValue(true)
    await act(async () => {
      root.render(
        <PodTerminal
          clusterId="cluster-a"
          namespace="test"
          podName="api"
          expectedUid="uid-a"
          beforeConnect={guard}
        />,
      )
    })
    const first = WebSocketMock.instances[0]
    expect(new URL(first.url).searchParams.get('expectedUid')).toBe('uid-a')
    first.readyState = WebSocketMock.OPEN
    guard.mockResolvedValue(false)
    await act(async () => container.querySelector('button')?.click())
    expect(guard).toHaveBeenCalledTimes(2)
    expect(WebSocketMock.instances).toHaveLength(1)
    expect(first.readyState).toBe(3)
    expect(first.sent.map((value) => JSON.parse(value).type)).toEqual(['close'])
  })
})
