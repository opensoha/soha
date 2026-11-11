/** @vitest-environment jsdom */

import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/i18n'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { SohaProviderPortalPage } from './page'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  post: vi.fn(),
}))

const announcementMocks = vi.hoisted(() => ({
  useAnnouncementInbox: vi.fn(),
}))

const permissionMocks = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: [
      'identity.applications.view',
      'identity.portal.view',
      'observe.monitoring.view',
      'workbench.home.view',
      'workbench.monitoring.view',
      'workspace.resource.view',
    ],
    visibleMenuIds: ['home-workbench', 'monitoring-workbench', 'monitoring-workbench-overview'],
    visibleMenus: [
      {
        id: 'home-workbench',
        path: '/portal',
        labelEn: 'Home',
        labelZh: '首页',
        iconKey: 'home',
        sortOrder: 0,
      },
      {
        id: 'monitoring-workbench',
        path: '/monitoring-workbench',
        labelEn: 'Observability Workbench',
        labelZh: '可观测性工作台',
        iconKey: 'gauge',
        sortOrder: 50,
      },
      {
        id: 'monitoring-workbench-overview',
        parentId: 'monitoring-workbench',
        path: '/monitoring-workbench/overview',
        labelEn: 'Overview',
        labelZh: '总览',
        iconKey: 'gauge',
        sortOrder: 51,
      },
    ],
  },
}))
const defaultPermissionSnapshot = structuredClone(permissionMocks.snapshot)

vi.mock('@/services/api-client', () => ({ api: apiMocks }))
vi.mock('@/features/announcements', () => ({
  useAnnouncementInbox: announcementMocks.useAnnouncementInbox,
}))
vi.mock('@/features/auth', () => ({
  hasPermission: (_snapshot: unknown, permissionKey?: string) =>
    !permissionKey || permissionMocks.snapshot.permissionKeys.includes(permissionKey),
  usePermissionSnapshot: () => ({
    data: { data: permissionMocks.snapshot },
  }),
}))

const application = {
  id: 'app-1',
  slug: 'console',
  name: 'Operations Console',
  description: 'Manage production services',
  tags: ['production'],
  providerType: 'link',
  portalVisible: true,
  featured: true,
  favorite: true,
  sortOrder: 1,
  status: 'enabled',
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
}

const secondaryApplication = {
  ...application,
  id: 'app-2',
  slug: 'builds',
  name: 'Build Dashboard',
  description: 'Review build activity',
  tags: ['internal'],
  featured: false,
  favorite: false,
  sortOrder: 2,
}

const bootstrap = {
  principal: {
    userId: 'user-1',
    userName: 'admin',
    email: 'admin@example.test',
    roles: ['admin'],
    teams: [],
    projects: [],
    tags: [],
  },
  applications: [application, secondaryApplication],
  favorites: [application],
  recent: [],
  security: {
    principal: {
      userId: 'user-1',
      userName: 'admin',
      email: 'admin@example.test',
      roles: ['admin'],
      teams: [],
      projects: [],
      tags: [],
    },
    mfaEnabled: true,
    linkedSources: ['oidc'],
    activeSession: 1,
  },
}

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
  window.localStorage.clear()
  useAuthStore.setState({
    user: {
      userId: 'user-1',
      userName: 'admin',
      email: 'admin@example.test',
      avatarUrl: 'https://example.test/avatar.png',
      avatarFit: 'contain',
      roles: ['admin'],
      teams: [],
      projects: [],
      tags: [],
    },
  })
  Object.assign(permissionMocks.snapshot, structuredClone(defaultPermissionSnapshot))
  usePreferencesStore.setState({ localeCode: 'en_US' })
  apiMocks.get.mockResolvedValue({ data: bootstrap })
  announcementMocks.useAnnouncementInbox.mockReturnValue({
    data: { data: { items: [], unreadCount: 0 } },
    isLoading: false,
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  useAuthStore.getState().clearAuth()
  usePreferencesStore.setState({ localeCode: 'zh_CN' })
  window.localStorage.clear()
  document.body.innerHTML = ''
})

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderPage() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={['/portal']}>
            <I18nProvider>
              <SohaProviderPortalPage />
            </I18nProvider>
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await flushAsyncWork()
  return container
}

async function openApplicationMenu(container: HTMLElement, name = application.name) {
  const button = container.querySelector<HTMLButtonElement>(`button[aria-label="Actions ${name}"]`)
  expect(button).not.toBeNull()
  await act(async () => button?.click())
  await flushAsyncWork()
  const menu = document.querySelector('.ant-dropdown:not(.ant-dropdown-hidden) [role="menu"]')!
  expect(menu).not.toBeNull()
  return menu
}

describe('Provider Portal catalog page', () => {
  it.each([true, false])(
    'keeps the Heya shortcut separate and permission gated (chat=%s)',
    async (chat) => {
      if (chat) permissionMocks.snapshot.permissionKeys.push('observe.ai.chat')
      const container = await renderPage()
      const link = container.querySelector<HTMLAnchorElement>('a[aria-label="Open Heya"]')
      if (chat) {
        expect(link?.getAttribute('href')).toBe('/heya')
        expect(link?.textContent).toBe('Heya')
      } else expect(link).toBeNull()
    },
  )
  it('renders the application workspace without legacy portal chrome', async () => {
    const container = await renderPage()

    expect(apiMocks.get).toHaveBeenCalledWith('/portal/bootstrap')
    expect(announcementMocks.useAnnouncementInbox).toHaveBeenCalledWith(10, true)
    expect(container.textContent).toContain('Operations Console')
    const cards = container.querySelector('.soha-portal-app-grid')
    expect(cards?.textContent).not.toContain('Available')
    expect(cards?.textContent).not.toContain('Link')
    expect(cards?.querySelector('.soha-portal-app-tags')).toBeNull()
    expect(cards?.textContent).toContain('Featured')
    expect(container.querySelector('.soha-portal-header')).toBeNull()
    expect(container.querySelector('.soha-portal-shortcuts')).toBeNull()
    expect(container.querySelector('.soha-portal-toolbar')).toBeNull()
    expect(container.querySelector('.soha-portal-apps-toolbar')).toBeNull()
    expect(container.querySelector('.soha-portal-app-tag-filter')).toBeNull()
    expect(container.querySelector('.soha-portal-app-toolbar')).not.toBeNull()
    expect(container.querySelector('.soha-portal-app-toolbar .soha-portal-search')).not.toBeNull()
    expect(
      container.querySelector('button[aria-label="Switch application card size"]'),
    ).not.toBeNull()
    expect(container.querySelector('.soha-portal-category')).toBeNull()
    expect(container.querySelector('.soha-portal-mode')).toBeNull()
    expect(container.querySelector('.soha-portal-announcements')).toBeNull()
    expect(
      container
        .querySelector('.soha-portal-principal .soha-portal-user-avatar img')
        ?.getAttribute('src'),
    ).toBe('https://example.test/avatar.png')
    expect(
      container
        .querySelector('.soha-portal-principal .soha-portal-user-avatar')
        ?.getAttribute('style'),
    ).toContain('--soha-avatar-fit: contain')
    expect(container.querySelectorAll('.soha-portal-side-panel')).toHaveLength(2)
    expect(container.querySelector('button[aria-label="Collapse sidebar"]')).not.toBeNull()
  })

  it('renders accessible workbench links below the user panel', async () => {
    const container = await renderPage()

    await vi.waitFor(() => {
      expect(container.querySelector('.soha-portal-workbenches')).not.toBeNull()
    })
    const workbenches = container.querySelector('.soha-portal-workbenches')
    const sidePanels = [...container.querySelectorAll('.soha-portal-side-panel')]
    const observabilityLink = container.querySelector(
      'a[aria-label="Open Observability Workbench"]',
    )

    expect(workbenches).not.toBeNull()
    expect(container.querySelector('.soha-portal-recent-list')).toBeNull()
    expect(container.textContent).not.toContain('No recent launches')
    expect(sidePanels).toHaveLength(2)
    expect(sidePanels[1]).toBe(workbenches)
    expect(observabilityLink?.getAttribute('href')).toBe('/monitoring-workbench/overview')
    expect(container.textContent).not.toContain('Settings Center')
  })

  it('uses the admin workbench order instead of individual menu sort orders', async () => {
    permissionMocks.snapshot.permissionKeys.push(
      'workbench.compute.view',
      'workbench.settings.view',
      'settings.identity.view',
    )
    permissionMocks.snapshot.visibleMenus.push(
      {
        id: 'settings',
        path: '/settings',
        labelEn: 'Settings Center',
        labelZh: '设置中心',
        iconKey: 'settings',
        sortOrder: 1,
      },
      {
        id: 'compute-workbench',
        path: '/compute',
        labelEn: 'Compute Resources',
        labelZh: '计算资源工作台',
        iconKey: 'server',
        sortOrder: 2,
      },
      {
        id: 'identity',
        path: '/internal-workbench',
        labelEn: 'Internal Workbench',
        labelZh: '内网工作台',
        iconKey: 'shield',
        sortOrder: 3,
      },
    )
    permissionMocks.snapshot.visibleMenuIds = permissionMocks.snapshot.visibleMenus.map(
      (menu) => menu.id,
    )
    const container = await renderPage()

    expect(
      [...container.querySelectorAll('.soha-portal-workbench-link')].map((link) => ({
        label: link.textContent,
        path: link.getAttribute('href'),
      })),
    ).toEqual([
      { label: 'Observability Workbench', path: '/monitoring-workbench/overview' },
      { label: 'Compute Resources', path: '/compute/overview' },
      { label: 'Settings Center', path: '/settings/overview' },
    ])
  })

  it.each(['workbench.monitoring.view', 'observe.monitoring.view'])(
    'hides the workbench panel without %s despite a visible menu',
    async (permission) => {
      permissionMocks.snapshot.permissionKeys = permissionMocks.snapshot.permissionKeys.filter(
        (key) => key !== permission,
      )
      const container = await renderPage()
      expect(container.querySelector('.soha-portal-workbenches')).toBeNull()
    },
  )

  it('renders autoplay announcements and a horizontally collapsible sidebar', async () => {
    const intervalSpy = vi.spyOn(window, 'setInterval')
    announcementMocks.useAnnouncementInbox.mockReturnValue({
      data: {
        data: {
          items: Array.from({ length: 5 }, (_, index) => ({
            id: `notice-${index + 1}`,
            title: `Notice ${index + 1}`,
            content: `Announcement ${index + 1} content`,
            level: 'info',
            status: 'published',
            audience: 'all',
            sticky: false,
            createdAt: '2026-01-01T00:00:00Z',
            updatedAt: '2026-01-01T00:00:00Z',
            isRead: index > 0,
          })),
          unreadCount: 1,
        },
      },
      isLoading: false,
    })
    const container = await renderPage()

    expect(container.querySelectorAll('.soha-portal-announcement-item')).toHaveLength(5)
    expect(
      container.querySelector('.soha-portal-announcement-track')?.getAttribute('data-active-index'),
    ).toBe('0')
    expect(container.querySelector('.soha-portal-announcement-controls')).toBeNull()
    expect(container.querySelector('.soha-portal-announcement-page-indicator')).toBeNull()
    expect(container.textContent).toContain('Notice 5')
    expect(container.textContent).toContain('Announcement 5 content')

    const intervalCall = intervalSpy.mock.calls.find(([, delay]) => delay === 6000)
    expect(intervalCall).toBeDefined()
    await act(async () => (intervalCall?.[0] as () => void)())
    expect(
      container.querySelector('.soha-portal-announcement-track')?.getAttribute('data-active-index'),
    ).toBe('1')
    intervalSpy.mockRestore()

    const collapseSidebarButton = container.querySelector('button[aria-label="Collapse sidebar"]')
    await act(async () =>
      collapseSidebarButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )
    expect(container.querySelector('.soha-portal-workspace')?.className).toContain(
      'is-side-collapsed',
    )
    expect(container.querySelector('.soha-portal-side')).toBeNull()
    expect(container.querySelectorAll('.soha-portal-side-panel')).toHaveLength(0)

    const expandSidebarButton = container.querySelector(
      '.soha-portal-view-actions button[aria-label="Expand sidebar"]',
    )
    await act(async () =>
      expandSidebarButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )
    expect(container.querySelector('.soha-portal-workspace')?.className).not.toContain(
      'is-side-collapsed',
    )
    expect(container.querySelector('.soha-portal-side')).not.toBeNull()
    expect(container.querySelectorAll('.soha-portal-side-panel')).toHaveLength(2)
  })

  it('switches application card density and filters by tag', async () => {
    const container = await renderPage()

    const densityButton = container.querySelector(
      'button[aria-label="Switch application card size"]',
    )
    expect(container.querySelector('.soha-portal-app-grid')?.className).toContain('is-medium')
    await act(async () => densityButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    expect(container.querySelector('.soha-portal-app-grid')?.className).toContain('is-small')
    expect(container.querySelectorAll('.soha-portal-app-card.is-small')).toHaveLength(2)
    expect(container.querySelector('.soha-portal-app-description')).toBeNull()
    expect(container.querySelectorAll('.soha-portal-app-launch')).toHaveLength(2)
    await act(async () => densityButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    expect(container.querySelector('.soha-portal-app-grid')?.className).toContain('is-medium')
    expect(container.querySelector('.soha-portal-app-grid')?.className).not.toContain('is-large')

    const internalGroup = [
      ...container.querySelectorAll('.soha-portal-group-menu .ant-menu-item'),
    ].find((item) => item.textContent?.trim() === 'internal')
    await act(async () => internalGroup?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()

    const visibleCards = [...container.querySelectorAll('.soha-portal-app-card')]
    expect(visibleCards).toHaveLength(1)
    expect(visibleCards[0]?.textContent).toContain('Build Dashboard')
    expect(visibleCards[0]?.textContent).not.toContain('Operations Console')
  })

  it('filters favorites and refreshes the empty state after unfavoriting', async () => {
    const container = await renderPage()
    const selectGroup = async (label: string) => {
      const item = [...container.querySelectorAll('.soha-portal-group-menu .ant-menu-item')].find(
        (entry) => entry.textContent?.trim() === label,
      )
      expect(item).toBeDefined()
      await act(async () => item?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    }
    await selectGroup('internal')
    await selectGroup('Favorites')
    expect(container.querySelectorAll('.soha-portal-app-card')).toHaveLength(1)
    expect(container.querySelector('.soha-portal-app-grid')?.textContent).toContain(
      'Operations Console',
    )
    expect(container.querySelector('.ant-menu-item-selected')?.textContent).toContain('Favorites')

    apiMocks.delete.mockResolvedValue({ data: {} })
    apiMocks.get.mockResolvedValue({
      data: {
        ...bootstrap,
        applications: [{ ...application, favorite: false }, secondaryApplication],
        favorites: [],
      },
    })
    const menu = await openApplicationMenu(container)
    const unfavorite = [...menu.querySelectorAll<HTMLElement>('[role="menuitem"]')].find(
      (item) => item.textContent?.trim() === 'Unfavorite',
    )
    expect(unfavorite).toBeDefined()
    expect(menu.textContent).toContain('Details')
    expect(apiMocks.post).not.toHaveBeenCalled()
    await act(async () => unfavorite?.click())
    await vi.waitFor(async () => {
      await flushAsyncWork()
      expect(container.querySelectorAll('.soha-portal-app-card')).toHaveLength(0)
    })
    expect(apiMocks.delete).toHaveBeenCalledWith('/portal/applications/app-1/favorite')
    expect(container.textContent).toContain('No favorite applications')
    expect(apiMocks.post).not.toHaveBeenCalled()

    await selectGroup('All')
    expect(container.querySelectorAll('.soha-portal-app-card')).toHaveLength(2)
  })

  it('searches within favorites and keeps unavailable status visible', async () => {
    apiMocks.get.mockResolvedValue({
      data: {
        ...bootstrap,
        applications: [{ ...application, status: 'maintenance' }, secondaryApplication],
      },
    })
    const container = await renderPage()
    const favorites = [
      ...container.querySelectorAll('.soha-portal-group-menu .ant-menu-item'),
    ].find((item) => item.textContent?.trim() === 'Favorites')
    await act(async () => favorites?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    expect(container.querySelector('.soha-portal-app-grid')?.textContent).toContain('Maintenance')
    expect(container.querySelector<HTMLButtonElement>('.soha-portal-app-launch')?.disabled).toBe(
      true,
    )
    await act(async () =>
      container.querySelector<HTMLButtonElement>('.soha-portal-app-launch')?.click(),
    )
    expect(apiMocks.post).not.toHaveBeenCalled()

    const input = container.querySelector<HTMLInputElement>(
      '.soha-portal-app-search input, input.soha-portal-app-search',
    )!
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        'Build',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.querySelectorAll('.soha-portal-app-card')).toHaveLength(0)
    expect(container.textContent).toContain('No matching applications')
  })

  it('restores portal layout preferences for the same user without sharing them', async () => {
    const container = await renderPage()

    await act(async () =>
      container
        .querySelector('button[aria-label="Collapse application groups"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )
    await act(async () =>
      container
        .querySelector('button[aria-label="Collapse sidebar"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )
    await act(async () =>
      container
        .querySelector('button[aria-label="Switch application card size"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )

    expect(
      JSON.parse(window.localStorage.getItem('soha-provider-portal-layout:v1:user-1') ?? 'null'),
    ).toEqual({
      applicationView: 'small',
      isGroupCollapsed: true,
      isSideCollapsed: true,
    })

    const restoredContainer = await renderPage()
    expect(restoredContainer.querySelector('.soha-portal-workspace')?.className).toContain(
      'is-group-collapsed',
    )
    expect(restoredContainer.querySelector('.soha-portal-workspace')?.className).toContain(
      'is-side-collapsed',
    )
    expect(restoredContainer.querySelector('.soha-portal-app-grid')?.className).toContain(
      'is-small',
    )

    await act(async () => {
      useAuthStore.setState({
        user: { ...useAuthStore.getState().user!, userId: 'user-2' },
      })
    })
    const otherUserContainer = await renderPage()
    expect(otherUserContainer.querySelector('.soha-portal-workspace')?.className).not.toContain(
      'is-group-collapsed',
    )
    expect(otherUserContainer.querySelector('.soha-portal-workspace')?.className).not.toContain(
      'is-side-collapsed',
    )
    expect(otherUserContainer.querySelector('.soha-portal-app-grid')?.className).toContain(
      'is-medium',
    )
  })

  it('renders application groups as the fixed first workspace column', async () => {
    const container = await renderPage()

    const workspace = container.querySelector('.soha-portal-workspace')
    const groupNavigation = container.querySelector('.soha-portal-group-nav')
    expect(workspace?.children[0]).toBe(groupNavigation)
    expect(workspace?.children[1]).toBe(container.querySelector('.soha-portal-apps'))
    expect(workspace?.children[2]).toBe(container.querySelector('.soha-portal-side'))
    expect(container.textContent).toContain('Application groups')
    expect(container.querySelector('.soha-portal-tag-filter-tabs')).toBeNull()
    expect(container.querySelector('button[aria-label="Switch to grouped view"]')).toBeNull()
    expect(container.querySelector('button[aria-label="Switch to tab view"]')).toBeNull()

    await act(async () =>
      container
        .querySelector('button[aria-label="Collapse application groups"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )
    expect(workspace?.className).toContain('is-group-collapsed')
    expect(container.querySelector('.soha-portal-group-nav')).toBeNull()
    expect(container.querySelector('.soha-portal-group-menu')).toBeNull()
    expect(
      container.querySelector(
        '.soha-portal-app-toolbar button[aria-label="Expand application groups"]',
      ),
    ).not.toBeNull()
    expect(workspace?.children[0]).toBe(container.querySelector('.soha-portal-apps'))

    await act(async () =>
      container
        .querySelector('button[aria-label="Expand application groups"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )
    expect(workspace?.className).not.toContain('is-group-collapsed')
    expect(workspace?.children[0]).toBe(container.querySelector('.soha-portal-group-nav'))
    expect(container.querySelector('.soha-portal-group-menu')).not.toBeNull()

    const internalGroup = [
      ...container.querySelectorAll('.soha-portal-group-menu .ant-menu-item'),
    ].find((item) => item.textContent?.trim() === 'internal')
    await act(async () => internalGroup?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()

    const visibleCards = [...container.querySelectorAll('.soha-portal-app-card')]
    expect(visibleCards).toHaveLength(1)
    expect(visibleCards[0]?.textContent).toContain('Build Dashboard')
  })

  it('launches an enabled application when its card is clicked', async () => {
    apiMocks.post.mockImplementation(() => new Promise(() => {}))
    const container = await renderPage()

    await act(async () =>
      container
        .querySelector('.soha-portal-app-card .soha-portal-app-title')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )

    expect(apiMocks.post).toHaveBeenCalledWith('/portal/applications/app-1/launch')
    await flushAsyncWork()
    expect(container.querySelector('.soha-portal-app-launch')?.getAttribute('aria-busy')).toBe(
      'true',
    )
    await act(async () =>
      container.querySelector<HTMLButtonElement>('.soha-portal-app-launch')?.click(),
    )
    expect(apiMocks.post).toHaveBeenCalledTimes(1)
  })

  it('hides application details without the identity application permission', async () => {
    permissionMocks.snapshot.permissionKeys = []
    const container = await renderPage()

    const menu = await openApplicationMenu(container)
    expect(menu.textContent).not.toContain('Details')
    expect(menu.textContent).toContain('Unfavorite')
    expect(container.querySelectorAll('.soha-portal-app-launch')).toHaveLength(2)
    expect(apiMocks.post).not.toHaveBeenCalled()
  })

  it('renders the portal home from the global Chinese dictionary', async () => {
    usePreferencesStore.setState({ localeCode: 'zh_CN' })
    const container = await renderPage()

    expect(container.textContent).not.toContain('标签')
    expect(container.textContent).toContain('全部')
    expect(container.textContent).not.toContain('全部标签')
    expect(container.querySelector('button[aria-label="切换应用卡片尺寸"]')).not.toBeNull()
  })
})
