/** @vitest-environment jsdom */

import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/i18n'
import { useAuthStore } from '@/stores/auth-store'
import { PortalColleagueAuthorPage } from './author-page'
import { PortalColleagueWorksPage } from './list-page'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  patch: vi.fn(),
}))

const permissionState = vi.hoisted(() => ({
  keys: ['workbench.home.view', 'identity.portal.view', 'observe.ai.chat'] as string[],
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))
vi.mock('@/features/auth', () => ({
  hasPermission: (_snapshot: unknown, key?: string) => !key || permissionState.keys.includes(key),
  logoutAuthSession: vi.fn(() => Promise.resolve()),
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: permissionState.keys,
        visibleMenuIds: ['home-workbench'],
        visibleMenus: [],
      },
    },
  }),
}))

const mountedRoots: Root[] = []

beforeAll(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class ResizeObserver {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => ({
      matches: false,
      media: '',
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

beforeEach(() => {
  vi.clearAllMocks()
  permissionState.keys = ['workbench.home.view', 'identity.portal.view', 'observe.ai.chat']
  useAuthStore.setState({
    user: {
      userId: 'usr-visitor',
      userName: 'visitor',
      email: 'visitor@example.test',
      roles: ['employee'],
      teams: ['team-growth'],
      projects: [],
      tags: [],
    },
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  useAuthStore.getState().clearAuth()
  document.body.innerHTML = ''
})

async function flushAsync() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 15))
  })
}

async function renderColleagueRoutes(initialEntry = '/portal/colleague-works') {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[initialEntry]}>
            <I18nProvider>
              <Routes>
                <Route path="/portal/colleague-works" element={<PortalColleagueWorksPage />} />
                <Route
                  path="/portal/colleague-works/authors/:authorId"
                  element={<PortalColleagueAuthorPage />}
                />
              </Routes>
            </I18nProvider>
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await flushAsync()
  return container
}

describe('Portal Colleague Works pages', () => {
  it('supports unified search, author cards with department tags, author drill-down, and safe links', async () => {
    apiMocks.get.mockImplementation(async (path: string) => {
      if (path.includes('authorUserId=usr-alice-fin')) {
        return {
          data: {
            items: [
              {
                id: 'app-fin-1',
                name: '财务月度核销表',
                description: '财务部报销统计看板',
                kind: 'static',
                dnsLabel: 'fin-board-1',
                host: 'fin-board-1.apps.soha.internal',
                url: 'https://fin-board-1.apps.soha.internal',
                author: {
                  userId: 'usr-alice-fin',
                  displayName: '王敏',
                  teamNames: ['财务部'],
                },
                updatedAt: '2026-11-21T03:00:00Z',
              },
            ],
            authors: [],
            selectedAuthor: {
              userId: 'usr-alice-fin',
              displayName: '王敏',
              teamNames: ['财务部'],
              visibleAppCount: 1,
            },
            myAppsCount: 2,
            total: 1,
          },
        }
      }

      return {
        data: {
          items: [
            {
              id: 'app-fin-1',
              name: '财务月度核销表',
              description: '财务部报销统计看板',
              kind: 'static',
              dnsLabel: 'fin-board-1',
              host: 'fin-board-1.apps.soha.internal',
              url: 'https://fin-board-1.apps.soha.internal',
              author: {
                userId: 'usr-alice-fin',
                displayName: '王敏',
                teamNames: ['财务部'],
              },
              updatedAt: '2026-11-21T03:00:00Z',
            },
            {
              id: 'app-unsafe',
              name: '异常链接作品',
              description: '',
              kind: 'dynamic',
              dnsLabel: 'unsafe-1',
              host: 'unsafe-1.apps.soha.internal',
              url: 'javascript:alert(1)',
              author: {
                userId: 'usr-alice-hr',
                displayName: '王敏',
                teamNames: ['人力资源部'],
              },
              updatedAt: '2026-11-21T03:10:00Z',
            },
          ],
          authors: [
            {
              userId: 'usr-alice-fin',
              displayName: '王敏',
              teamNames: ['财务部'],
              visibleAppCount: 1,
            },
            {
              userId: 'usr-alice-hr',
              displayName: '王敏',
              teamNames: ['人力资源部'],
              visibleAppCount: 1,
            },
          ],
          myAppsCount: 2,
          total: 2,
        },
      }
    })

    const container = await renderColleagueRoutes()
    expect(apiMocks.get).toHaveBeenCalledWith('/app-hosting/portal/apps?limit=60')
    expect(container.textContent).toContain('财务月度核销表')
    expect(container.textContent).toContain('财务部')
    expect(container.textContent).toContain('人力资源部')

    const safeLink = container.querySelector<HTMLAnchorElement>(
      'a[aria-label="打开应用 财务月度核销表"]',
    )
    expect(safeLink?.getAttribute('href')).toBe('https://fin-board-1.apps.soha.internal/')
    expect(safeLink?.getAttribute('target')).toBe('_blank')
    expect(safeLink?.getAttribute('rel')).toBe('noopener noreferrer')

    const disabledUnsafeButton = Array.from(container.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('链接不可用'),
    )
    expect(disabledUnsafeButton?.disabled).toBe(true)

    const searchInput = container.querySelector<HTMLInputElement>('input[aria-label="搜索同事作品"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        searchInput,
        '王敏',
      )
      searchInput.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await flushAsync()
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/portal/apps?query=%E7%8E%8B%E6%95%8F&limit=60',
    )

    const authorCard = container.querySelectorAll<HTMLButtonElement>('.soha-portal-author-card')[0]
    expect(authorCard).toBeDefined()
    await act(async () => {
      authorCard?.click()
    })
    await flushAsync()

    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/portal/apps?authorUserId=usr-alice-fin&limit=60',
    )
    expect(container.textContent).toContain('王敏公开的作品')
    expect(container.textContent).toContain('当前对您开放 1 个在线作品')

    const backButton = Array.from(container.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('返回全部同事作品'),
    )
    expect(backButton).toBeDefined()
    await act(async () => {
      backButton?.click()
    })
    await flushAsync()
    expect(container.textContent).toContain('发现并一键打开同事分享给您')
  })

  it('renders empty state publish guidance and no-match filter reset state', async () => {
    apiMocks.get.mockResolvedValue({
      data: { items: [], authors: [], myAppsCount: 0, total: 0 },
    })

    const container = await renderColleagueRoutes()
    expect(container.textContent).toContain('当前暂无对您开放的同事作品')
    expect(container.textContent).toContain('soha host deploy ./dist --name "季度看板"')
    expect(container.querySelector('a[href="/heya"]')).not.toBeNull()

    const searchInput = container.querySelector<HTMLInputElement>('input[aria-label="搜索同事作品"]')!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        searchInput,
        '不存在的工具',
      )
      searchInput.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await flushAsync()

    expect(container.textContent).toContain('未找到匹配的同事作品')
    const clearBtn = Array.from(container.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('清空筛选条件'),
    )
    expect(clearBtn).toBeDefined()
  })
})
