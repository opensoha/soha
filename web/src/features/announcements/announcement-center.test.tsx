/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { AnnouncementBell } from './announcement-center'

const testState = vi.hoisted(() => ({
  items: [] as Array<Record<string, unknown>>,
  unreadCount: 0,
}))

vi.mock('@/i18n', () => ({
  localeText: (localeCode: string, chinese: string, english: string) =>
    localeCode === 'zh_CN' ? chinese : english,
  useI18n: () => ({ localeCode: 'en_US' as const }),
}))
vi.mock('@/features/auth', () => ({
  hasPermission: () => true,
  usePermissionSnapshot: () => ({ data: { data: { permissionKeys: [] } } }),
}))
vi.mock('./use-inbox', () => ({
  useAnnouncementInbox: () => ({
    data: { data: testState },
  }),
}))

describe('AnnouncementBell i18n', () => {
  beforeAll(() => {
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
    window.matchMedia = () =>
      ({
        addEventListener: () => undefined,
        addListener: () => undefined,
        dispatchEvent: () => false,
        matches: false,
        media: '',
        onchange: null,
        removeEventListener: () => undefined,
        removeListener: () => undefined,
      }) as MediaQueryList
  })

  beforeEach(() => {
    testState.items = []
    testState.unreadCount = 0
  })

  afterEach(() => document.body.replaceChildren())

  it('renders the bell and popover in English', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } })

    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <AntdApp>
            <AnnouncementBell />
          </AntdApp>
        </QueryClientProvider>,
      )
    })

    const bell = container.querySelector('button[aria-label="Announcements"]')
    expect(bell).toBeInstanceOf(HTMLButtonElement)
    await act(async () => {
      bell?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('Announcements')
    expect(document.body.textContent).toContain('Unread (0)')
    expect(document.body.textContent).toContain('All (0)')
    expect(document.body.textContent).toContain('No announcements')
    expect(document.body.textContent).not.toContain('公告中心')

    await act(async () => root.unmount())
  })

  it('preserves non-empty announcement content and actions', async () => {
    testState.items = [
      {
        id: 'notice-1',
        title: 'Maintenance window',
        content: 'Expected duration: 30 minutes',
        level: 'critical',
        sticky: true,
        isRead: false,
        publishedAt: '2026-08-24T00:00:00Z',
      },
    ]
    testState.unreadCount = 1
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const queryClient = new QueryClient({ defaultOptions: { mutations: { retry: false } } })

    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <AntdApp>
            <AnnouncementBell />
          </AntdApp>
        </QueryClientProvider>,
      )
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('Maintenance window')
    expect(document.body.textContent).toContain('Pinned')
    expect(document.body.textContent).toContain('Critical')
    expect(document.body.textContent).toContain('Later')
    expect(document.body.textContent).toContain('Mark as read')

    const bell = container.querySelector('button[aria-label="Announcements, 1 unread"]')
    await act(async () => {
      bell?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(document.body.textContent).toContain('View')
    expect(document.body.textContent).toContain('Expected duration: 30 minutes')

    await act(async () => root.unmount())
  })
})
