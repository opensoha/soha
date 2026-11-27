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
  useAnnouncementInbox: vi.fn(() => ({
    data: { data: { items: [], unreadCount: 0 } },
    error: null,
    isEnabled: true,
    isError: false,
    isFetching: false,
    refetch: vi.fn(),
  })),
}))

const permissionMocks = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: ['identity.applications.view', 'identity.portal.view', 'workbench.home.view'],
    visibleMenuIds: ['home-workbench'],
    visibleMenus: [
      {
        id: 'home-workbench',
        path: '/portal',
        labelEn: 'Home',
        labelZh: '首页',
        iconKey: 'home',
        sortOrder: 0,
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
      roles: ['admin'],
      teams: [],
      projects: [],
      tags: [],
    },
  })
  Object.assign(permissionMocks.snapshot, structuredClone(defaultPermissionSnapshot))
  usePreferencesStore.setState({ localeCode: 'en_US' })
  apiMocks.get.mockResolvedValue({ data: bootstrap })
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

async function renderPage(initialEntry = '/portal') {
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

describe('Provider Portal catalog page (unified AppLayout stage)', () => {
  it('renders unified compact 3D application cards without inline announcement or legacy portal chrome', async () => {
    const container = await renderPage()

    expect(apiMocks.get).toHaveBeenCalledWith('/portal/bootstrap')
    expect(announcementMocks.useAnnouncementInbox).toHaveBeenCalledWith()
    expect(container.textContent).toContain('Operations Console')
    const cards = container.querySelector('.soha-portal-app-grid')
    expect(cards?.textContent).not.toContain('Available')
    expect(cards?.textContent).not.toContain('Link')
    expect(cards?.querySelector('.soha-portal-app-tags')).toBeNull()
    expect(cards?.textContent).toContain('Featured')
    expect(container.querySelector('.soha-portal-header')).toBeNull()
    expect(container.querySelector('.soha-portal-side')).toBeNull()
    expect(container.querySelector('.soha-portal-announcements')).toBeNull()
    expect(container.querySelector('.soha-portal-app-grid.is-small')).not.toBeNull()
    expect(container.querySelectorAll('.soha-portal-app-card.is-small')).toHaveLength(2)
    expect(container.querySelector('.soha-portal-app-description')).toBeNull()
    expect(
      container.querySelector('.soha-portal-expandable-search')?.classList.contains('is-expanded'),
    ).toBe(false)
    expect(container.querySelector('.soha-portal-search-trigger')).not.toBeNull()
  })

  it('filters applications by tag query parameter from Dual-Rail sub-navigation', async () => {
    const container = await renderPage('/portal?tag=internal')

    expect(container.querySelector('.soha-portal-stage-head__title')?.textContent).toContain(
      'internal',
    )
    const visibleCards = [...container.querySelectorAll('.soha-portal-app-card')]
    expect(visibleCards).toHaveLength(1)
    expect(visibleCards[0]?.textContent).toContain('Build Dashboard')
    expect(visibleCards[0]?.textContent).not.toContain('Operations Console')
  })

  it('filters favorites via ?group=favorites and refreshes empty state after unfavoriting', async () => {
    const container = await renderPage('/portal?group=favorites')
    expect(container.querySelectorAll('.soha-portal-app-card')).toHaveLength(1)
    expect(container.querySelector('.soha-portal-app-grid')?.textContent).toContain(
      'Operations Console',
    )

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
    await act(async () => unfavorite?.click())
    await vi.waitFor(async () => {
      await flushAsyncWork()
      expect(container.querySelectorAll('.soha-portal-app-card')).toHaveLength(0)
    })
    expect(apiMocks.delete).toHaveBeenCalledWith('/portal/applications/app-1/favorite')
    expect(container.textContent).toContain('No favorite applications')
  })

  it('expands search on icon click, searches within favorites, and collapses when empty on blur', async () => {
    apiMocks.get.mockResolvedValue({
      data: {
        ...bootstrap,
        applications: [{ ...application, status: 'maintenance' }, secondaryApplication],
      },
    })
    const container = await renderPage('/portal?group=favorites')
    expect(container.querySelector('.soha-portal-app-grid')?.textContent).toContain('Maintenance')
    expect(container.querySelector<HTMLButtonElement>('.soha-portal-app-launch')?.disabled).toBe(
      true,
    )

    const searchTrigger = container.querySelector<HTMLButtonElement>('.soha-portal-search-trigger')!
    expect(searchTrigger).not.toBeNull()
    await act(async () => searchTrigger.click())
    expect(
      container.querySelector('.soha-portal-expandable-search')?.classList.contains('is-expanded'),
    ).toBe(true)

    const input = container.querySelector<HTMLInputElement>(
      '.soha-portal-app-search input, input.soha-portal-app-search',
    )!
    await act(async () => {
      input.focus()
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(
        input,
        'Build',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(container.querySelectorAll('.soha-portal-app-card')).toHaveLength(0)
    expect(container.textContent).toContain('No matching applications')

    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, '')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.blur()
    })
    expect(
      container.querySelector('.soha-portal-expandable-search')?.classList.contains('is-expanded'),
    ).toBe(false)
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
  })

  it('shows Colleague Works tab by default and gates My Apps tab on apphosting permissions', async () => {
    usePreferencesStore.setState({ localeCode: 'zh_CN' })
    const defaultContainer = await renderPage()
    const defaultNav = defaultContainer.querySelector('.soha-portal-view-nav')
    expect(defaultNav?.textContent).toContain('企业应用')
    expect(defaultNav?.textContent).toContain('同事作品')
    expect(defaultNav?.textContent).not.toContain('我的作品')

    permissionMocks.snapshot.permissionKeys = ['identity.portal.view', 'apphosting.apps.publish']
    const publisherContainer = await renderPage()
    const publisherNav = publisherContainer.querySelector('.soha-portal-view-nav')
    const myAppsLink = publisherNav?.querySelector<HTMLAnchorElement>('a[href="/portal/my-apps"]')
    expect(myAppsLink).not.toBeNull()
    expect(myAppsLink?.textContent).toContain('我的作品')
  })
})
