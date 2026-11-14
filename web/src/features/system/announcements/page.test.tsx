/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { AnnouncementsPage } from './page'

Object.defineProperty(globalThis, 'IS_REACT_ACT_ENVIRONMENT', { value: true, writable: true })

const testState = vi.hoisted(() => ({
  get: vi.fn(),
  snapshot: {
    permissionKeys: [
      'system.announcements.view',
      'system.announcements.create',
      'system.announcements.update',
      'system.announcements.delete',
      'system.announcements.publish',
      'system.announcements.withdraw',
    ],
    visibleMenuIds: ['system-announcements'],
    visibleMenus: [],
  } as PermissionSnapshot,
}))

vi.mock('@/services/api-client', () => ({
  api: {
    delete: vi.fn(),
    get: testState.get,
    post: vi.fn(),
    put: vi.fn(),
  },
}))

vi.mock('@/features/auth', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth')>('@/features/auth')
  return {
    ...actual,
    usePermissionSnapshot: () => ({ data: { data: testState.snapshot }, isLoading: false }),
  }
})

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    toolbar,
  }: {
    columns: any[]
    dataSource: any[]
    toolbar?: ReactNode
  }) => (
    <div data-testid="receipt-table">
      {toolbar}
      {dataSource.map((record) => (
        <div key={record.userId}>
          {columns.map((column) => (
            <div key={String(column.key ?? column.dataIndex)}>
              {column.render
                ? column.render(record[column.dataIndex], record)
                : String(record[column.dataIndex] ?? '')}
            </div>
          ))}
        </div>
      ))}
    </div>
  ),
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

async function settle() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function find(selector: string) {
  for (let attempt = 0; attempt < 10; attempt += 1) {
    const element = document.querySelector<HTMLElement>(selector)
    if (element) return element
    await settle()
  }
  throw new Error(`Missing element: ${selector}`)
}

describe('AnnouncementsPage receipt drawer', () => {
  beforeAll(() => {
    const getComputedStyle = window.getComputedStyle.bind(window)
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element))
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: false,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    })
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    globalThis.ResizeObserver = ResizeObserverMock
  })

  beforeEach(async () => {
    testState.get.mockImplementation((path: string) => {
      if (path === '/announcements') {
        return Promise.resolve({
          data: [
            {
              id: 'announcement-1',
              title: 'Maintenance',
              content: 'Tonight',
              level: 'info',
              status: 'published',
              audience: 'all',
              sticky: false,
              publishedAt: '2026-09-04T08:00:00Z',
              createdAt: '2026-09-04T08:00:00Z',
              updatedAt: '2026-09-04T08:00:00Z',
            },
          ],
        })
      }
      if (path.startsWith('/announcements/announcement-1/receipts')) {
        return Promise.resolve({
          data: {
            items: [
              {
                userId: 'user-1',
                username: 'ada',
                displayName: 'Ada',
                email: 'ada@example.test',
                teamNames: ['研发中心'],
                isRead: true,
                readAt: '2026-09-04T09:00:00Z',
              },
            ],
            total: 1,
            page: 1,
            pageSize: 15,
            readCount: 1,
            unreadCount: 0,
          },
        })
      }
      throw new Error(`Unexpected GET ${path}`)
    })
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    await act(async () => {
      root.render(
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <AnnouncementsPage />
          </QueryClientProvider>
        </AntdApp>,
      )
    })
    await settle()
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.clearAllMocks()
  })

  it('opens the read-status drawer from the card action', async () => {
    const button = await find('button[aria-label="查看阅读情况"]')
    await act(async () => button.click())
    await settle()

    expect(document.body.textContent).toContain('阅读情况')
    const drawer = document.body.querySelector('.ant-drawer')
    expect(drawer?.querySelector('.soha-system-announcement-receipt-identity')?.textContent).toBe(
      'ada',
    )
    expect(drawer?.querySelector('.soha-system-announcement-receipt-details')?.textContent).toBe(
      '研发中心',
    )
    expect(drawer?.querySelector('.soha-system-announcement-receipt-status')?.textContent).toBe(
      '已读',
    )
    expect(drawer?.textContent).not.toContain('组织：')
    expect(drawer?.textContent).not.toContain('Ada')
    expect(drawer?.textContent).not.toContain('ada@example.test')
    expect(drawer?.textContent).not.toContain('Maintenance')
    expect(drawer?.querySelector('.soha-system-announcement-receipt-list')).not.toBeNull()
    expect(drawer?.querySelector('.soha-system-announcement-receipt-details')).not.toBeNull()
    expect(drawer?.querySelector('table')).toBeNull()
    expect(testState.get).toHaveBeenCalledWith(
      '/announcements/announcement-1/receipts?state=all&page=1&pageSize=15',
    )
  })
})
