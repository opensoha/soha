/** @vitest-environment jsdom */

import { act, type ComponentType, type MutableRefObject, type ReactNode, type Ref } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PodLogViewer } from './pod-log-viewer'

interface MockListAPI {
  readonly element: HTMLDivElement | null
  scrollToRow: (options: { index: number }) => void
}

interface MockListProps {
  children?: ReactNode
  className?: string
  listRef?: Ref<MockListAPI>
  onScroll?: (event: { currentTarget: HTMLDivElement }) => void
  rowComponent: ComponentType<Record<string, unknown>>
  rowCount: number
  rowProps: Record<string, unknown>
}

const mocks = vi.hoisted(() => ({
  apiGet: vi.fn(),
  downloadText: vi.fn(),
  scrollToRow: vi.fn(),
}))

vi.mock('react-window', async () => {
  const React = await import('react')
  return {
    List: ({
      children,
      className,
      listRef,
      onScroll,
      rowComponent: Row,
      rowCount,
      rowProps,
    }: MockListProps) => {
      const elementRef = React.useRef<HTMLDivElement>(null)

      React.useLayoutEffect(() => {
        const api: MockListAPI = {
          get element() {
            return elementRef.current
          },
          scrollToRow: mocks.scrollToRow,
        }
        if (typeof listRef === 'function') {
          listRef(api)
          return () => listRef(null)
        }
        if (listRef) {
          ;(listRef as MutableRefObject<MockListAPI | null>).current = api
          return () => {
            ;(listRef as MutableRefObject<MockListAPI | null>).current = null
          }
        }
      }, [listRef])

      return (
        <div ref={elementRef} className={className} onScroll={onScroll}>
          {children}
          {Array.from({ length: Math.min(rowCount, 12) }, (_, index) => (
            <Row
              key={index}
              {...rowProps}
              index={index}
              style={{ height: 32 }}
              ariaAttributes={{
                role: 'listitem',
                'aria-posinset': index + 1,
                'aria-setsize': rowCount,
              }}
            />
          ))}
        </div>
      )
    },
  }
})

vi.mock('@/features/auth/stream-ticket', () => ({
  buildSameOriginStreamURL: (path: string) => new URL(path, 'http://localhost'),
  withStreamTicket: async (url: string) => url,
}))

vi.mock('@/i18n', () => {
  const t = (_key: string, fallback?: string) => fallback ?? _key
  return { useI18n: () => ({ localeCode: 'zh_CN' as const, t }) }
})

vi.mock('@/services/api-client', () => ({ api: { get: mocks.apiGet } }))
vi.mock('@/utils/download', () => ({ downloadText: mocks.downloadText }))

class WebSocketMock {
  static readonly OPEN = 1
  static instances: WebSocketMock[] = []

  readyState = 0
  onopen: (() => void) | null = null
  onmessage: ((event: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  onclose: (() => void) | null = null

  constructor(readonly url: string) {
    WebSocketMock.instances.push(this)
  }

  send() {}

  close() {
    this.readyState = 3
  }
}

const roots: Root[] = []

async function renderViewer() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)

  await act(async () => {
    root.render(
      <PodLogViewer clusterId="cluster-a" namespace="default" podName="pod-a" container="main" />,
    )
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
  })

  return container
}

beforeEach(() => {
  WebSocketMock.instances = []
  mocks.apiGet.mockReset()
  mocks.downloadText.mockReset()
  mocks.scrollToRow.mockReset()
  mocks.apiGet.mockImplementation(async (path: string) => {
    const tailLines = Number(new URL(path, 'http://localhost').searchParams.get('tailLines'))
    return {
      data: {
        content: Array.from({ length: tailLines }, (_, index) => `line-${index}`).join('\n'),
      },
    }
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('WebSocket', WebSocketMock)
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
    callback(0)
    return 1
  })
})

afterEach(async () => {
  await act(async () => {
    roots.splice(0).forEach((root) => root.unmount())
  })
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
})

describe('PodLogViewer', () => {
  it('starts from 100 lines without mounting every log row', async () => {
    await renderViewer()

    expect(String(mocks.apiGet.mock.calls[0]?.[0])).toContain('tailLines=100')
    expect(document.querySelectorAll('.soha-log-row').length).toBeLessThan(100)
  })

  it('loads one older page and stops following after the user scrolls up', async () => {
    const container = await renderViewer()
    const shell = container.querySelector<HTMLDivElement>('.soha-log-shell')
    expect(shell).not.toBeNull()
    Object.defineProperties(shell, {
      clientHeight: { configurable: true, value: 320 },
      scrollHeight: { configurable: true, value: 3200 },
    })
    if (!shell) return
    shell.scrollTop = 0
    mocks.scrollToRow.mockClear()

    await act(async () => {
      shell.dispatchEvent(new Event('scroll', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
      await Promise.resolve()
      await Promise.resolve()
    })

    const historyCalls = mocks.apiGet.mock.calls.filter(([path]) =>
      String(path).includes('tailLines=200'),
    )
    expect(historyCalls).toHaveLength(1)
    expect(container.querySelectorAll('[role="switch"]')[0]?.getAttribute('aria-checked')).toBe(
      'false',
    )
    expect(mocks.scrollToRow).not.toHaveBeenCalled()
  })

  it('bounds a single oversized live log line before export', async () => {
    const container = await renderViewer()
    const socket = WebSocketMock.instances[WebSocketMock.instances.length - 1]
    expect(socket).toBeDefined()

    await act(async () => {
      socket?.onmessage?.(
        new MessageEvent('message', {
          data: JSON.stringify({ type: 'log', data: 'x'.repeat(3 * 1024 * 1024) }),
        }),
      )
    })

    const exportButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('导出日志'),
    )
    expect(exportButton).toBeDefined()
    await act(async () => {
      exportButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(mocks.downloadText).toHaveBeenCalledOnce()
    expect(String(mocks.downloadText.mock.calls[0]?.[1]).length).toBeLessThan(70_000)
  })
  it('does not reconnect after a pending snapshot resolves following unmount', async () => {
    let resolve!: (value: { data: { content: string } }) => void
    mocks.apiGet.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done
        }),
    )
    await renderViewer()
    await act(async () => roots.pop()!.unmount())
    await act(async () => {
      resolve({ data: { content: 'late output' } })
      await Promise.resolve()
    })
    expect(WebSocketMock.instances).toHaveLength(0)
  })
})
