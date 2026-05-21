/** @vitest-environment jsdom */

import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { AppLayout } from './app-layout'
import type { PermissionSnapshot } from '@/types'

const testState = vi.hoisted(() => ({
  auth: {
    clearAuth: vi.fn(),
    user: { userName: 'Admin', email: 'admin@example.com', roles: ['ops'] },
  },
  prefs: {
    currentWorkspace: 'resource' as string | null,
    localeCode: 'zh_CN',
    setCurrentWorkspace: vi.fn((workspace: string | null) => {
      testState.prefs.currentWorkspace = workspace
    }),
    setLocaleCode: vi.fn(),
    setSidebarCollapsed: vi.fn(),
    setThemeMode: vi.fn(),
    sidebarCollapsed: false,
    themeMode: 'light',
  },
  snapshot: {
    permissionKeys: [],
    visibleMenuIds: [],
    visibleMenus: [],
  } as PermissionSnapshot,
}))

vi.mock('@/features/auth/permission-snapshot', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/permission-snapshot')>('@/features/auth/permission-snapshot')
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: { data: testState.snapshot },
      isLoading: false,
    }),
  }
})

vi.mock('@/features/settings/use-branding-settings', () => ({
  useBrandingSettings: () => ({
    data: {
      data: {
        appTitle: 'KubeCrux',
        sidebarTitle: 'KubeCrux',
        loginLogoUrl: '',
        expandedLogoUrl: '',
        collapsedLogoUrl: '',
        faviconUrl: '',
      },
    },
  }),
  getNormalizedBranding: (value: any) => ({
    appTitle: value?.appTitle || 'KubeCrux',
    sidebarTitle: value?.sidebarTitle || 'KubeCrux',
    loginLogoUrl: value?.loginLogoUrl || '',
    expandedLogoUrl: value?.expandedLogoUrl || '',
    collapsedLogoUrl: value?.collapsedLogoUrl || '',
    faviconUrl: value?.faviconUrl || '',
  }),
}))

vi.mock('@/features/announcements/announcement-center', () => ({
  AnnouncementBell: () => <div data-testid="announcement-bell">bell</div>,
}))

vi.mock('@/components/header-preference-button', () => ({
  HeaderPreferenceButton: ({ label, title }: { label?: string; title?: string }) => <button>{label || title}</button>,
}))

vi.mock('@/features/system/menu-icons', () => ({
  resolveMenuIcon: () => null,
}))

vi.mock('@/features/system/menu-schema', async () => {
  const actual = await vi.importActual<typeof import('@/features/system/menu-schema')>('@/features/system/menu-schema')
  return {
    ...actual,
    resolveMenuSectionLabel: (key: string) => key,
  }
})

vi.mock('@/stores/auth-store', () => ({
  useAuthStore: (selector?: (state: any) => unknown) => {
    const state = {
      clearAuth: testState.auth.clearAuth,
      isAuthenticated: () => true,
      user: testState.auth.user,
    }
    return selector ? selector(state) : state
  },
}))

vi.mock('@/stores/preferences-store', () => ({
  usePreferencesStore: (selector: (state: any) => unknown) => selector({
    currentWorkspace: testState.prefs.currentWorkspace,
    localeCode: testState.prefs.localeCode,
    setCurrentWorkspace: testState.prefs.setCurrentWorkspace,
    setLocaleCode: testState.prefs.setLocaleCode,
    setSidebarCollapsed: testState.prefs.setSidebarCollapsed,
    setThemeMode: testState.prefs.setThemeMode,
    sidebarCollapsed: testState.prefs.sidebarCollapsed,
    themeMode: testState.prefs.themeMode,
  }),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderWithProviders(route: string, snapshotOverrides?: Partial<PermissionSnapshot>) {
  testState.snapshot = {
    ...testState.snapshot,
    ...snapshotOverrides,
  }

  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[route]}>
          <Routes>
            <Route element={<AppLayout />}>
              <Route path="*" element={<div data-testid="page">page</div>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })

  return container
}

describe('app layout workspace navigation', () => {
  beforeAll(() => {
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: false,
        media: '',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  beforeEach(() => {
    testState.auth.clearAuth.mockClear()
    testState.auth.user = { userName: 'Admin', email: 'admin@example.com', roles: ['ops'] }
    testState.prefs.currentWorkspace = 'resource'
    testState.prefs.localeCode = 'zh_CN'
    testState.prefs.sidebarCollapsed = false
    testState.prefs.setCurrentWorkspace.mockClear()
    testState.snapshot = {
      permissionKeys: ['workspace.resource.view', 'workspace.application.view', 'overview.view', 'delivery.applications.view', 'system.menus.view'],
      visibleMenuIds: ['dashboard', 'builds', 'system', 'menus'],
      visibleMenus: [
        { id: 'dashboard', path: '/', labelZh: '概览', labelEn: 'Overview', iconKey: 'gauge', section: 'platform', sortOrder: 1, enabled: true },
        { id: 'builds', path: '/applications', labelZh: '应用中心', labelEn: 'Applications', iconKey: 'blocks', section: 'deliver', sortOrder: 2, enabled: true },
        { id: 'system', path: '/system', labelZh: '系统管理', labelEn: 'System', iconKey: 'panels-top-left', section: 'admin', sortOrder: 3, enabled: true },
        { id: 'menus', parentId: 'system', path: '/system/menus', labelZh: '菜单管理', labelEn: 'Menus', iconKey: 'menu-square', section: 'admin', sortOrder: 4, enabled: true },
      ],
    }
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) {
        root.unmount()
      }
    })
    roots = []
    for (const container of containers) {
      container.remove()
    }
    containers = []
    vi.clearAllMocks()
  })

  it('shows only the workbench switcher when only one business workspace is available', async () => {
    const container = await renderWithProviders('/', {
      permissionKeys: ['workspace.resource.view', 'overview.view', 'system.menus.view'],
      visibleMenuIds: ['dashboard', 'system', 'menus'],
      visibleMenus: [
        { id: 'dashboard', path: '/', labelZh: '概览', labelEn: 'Overview', iconKey: 'gauge', section: 'platform', sortOrder: 1, enabled: true },
        { id: 'system', path: '/system', labelZh: '系统管理', labelEn: 'System', iconKey: 'panels-top-left', section: 'admin', sortOrder: 3, enabled: true },
        { id: 'menus', parentId: 'system', path: '/system/menus', labelZh: '菜单管理', labelEn: 'Menus', iconKey: 'menu-square', section: 'admin', sortOrder: 4, enabled: true },
      ],
    })

    expect(container.querySelector('.kc-workbench-switcher__label')?.textContent).toBe('平台工作台')
    expect(container.querySelector('.kc-workspace-switcher-shell')).toBeNull()
  })

  it('filters the business menu by the current application workspace', async () => {
    const container = await renderWithProviders('/applications')

    expect(container.querySelector('.kc-workbench-switcher__label')?.textContent).toBe('应用交付工作台')
    expect(container.textContent).toContain('应用中心')
    expect(container.textContent).not.toContain('概览')
    expect(container.querySelector('.kc-nav-system')).toBeNull()
    expect(testState.prefs.setCurrentWorkspace).toHaveBeenCalledWith('application')
  })

  it('switches the left nav into system workspace mode while visiting system pages', async () => {
    const container = await renderWithProviders('/system/menus')

    expect(container.querySelector('.kc-workbench-switcher-shell')).not.toBeNull()
    expect(container.querySelector('.kc-workbench-switcher__label')?.textContent).toBe('平台工作台')
    expect(container.textContent).toContain('菜单管理')
    expect(container.textContent).not.toContain('概览')
    expect(container.querySelector('.kc-nav-system')).toBeNull()
    expect(container.querySelector('.kc-nav-business.is-system')).not.toBeNull()
    expect(container.querySelector('.kc-sider-topbar > button.kc-sider-brand')).not.toBeNull()
    expect(container.querySelector('button[aria-label="系统设置"]')?.className).not.toContain('is-active')
    expect(testState.prefs.setCurrentWorkspace).not.toHaveBeenCalled()
  })

  it('renders the workbench switcher below the brand bar and above the business menu', async () => {
    const container = await renderWithProviders('/')
    const brandBar = container.querySelector('.kc-sider-topbar')
    const workbenchShell = container.querySelector('.kc-workbench-switcher-shell')
    const businessNav = container.querySelector('.kc-nav-business')

    expect(brandBar).not.toBeNull()
    expect(workbenchShell).not.toBeNull()
    expect(businessNav).not.toBeNull()
    expect(container.querySelector('.kc-workspace-switcher-shell')).toBeNull()
    expect(brandBar?.nextElementSibling).toBe(workbenchShell)
    expect(workbenchShell?.nextElementSibling).toBe(businessNav)
  })

  it('syncs the persisted workspace when navigating directly to resource pages', async () => {
    testState.prefs.currentWorkspace = 'application'

    await renderWithProviders('/workloads/pods', {
      permissionKeys: ['workspace.resource.view', 'platform.workloads.view', 'system.menus.view'],
      visibleMenuIds: ['workloads', 'system', 'menus'],
      visibleMenus: [
        { id: 'workloads', path: '/workloads', labelZh: '工作负载', labelEn: 'Workloads', iconKey: 'boxes', section: 'platform', sortOrder: 1, enabled: true },
        { id: 'system', path: '/system', labelZh: '系统管理', labelEn: 'System', iconKey: 'panels-top-left', section: 'admin', sortOrder: 3, enabled: true },
        { id: 'menus', parentId: 'system', path: '/system/menus', labelZh: '菜单管理', labelEn: 'Menus', iconKey: 'menu-square', section: 'admin', sortOrder: 4, enabled: true },
      ],
    })

    expect(testState.prefs.setCurrentWorkspace).toHaveBeenCalledWith('resource')
  })

  it('renders ungrouped workbench menus without a group heading', async () => {
    const container = await renderWithProviders('/', {
      permissionKeys: ['workspace.resource.view', 'overview.view', 'platform.workloads.view'],
      visibleMenuIds: ['dashboard', 'workloads'],
      visibleMenus: [
        { id: 'dashboard', path: '/', labelZh: '概览', labelEn: 'Overview', iconKey: 'gauge', section: '', sortOrder: 1, enabled: true },
        { id: 'workloads', path: '/workloads', labelZh: '工作负载', labelEn: 'Workloads', iconKey: 'boxes', section: '', sortOrder: 2, enabled: true },
      ],
    })

    expect(container.textContent).toContain('概览')
    expect(container.textContent).toContain('工作负载')
    expect(container.textContent).not.toContain('platform')
    expect(container.querySelector('.ant-menu-item-group-title')).toBeNull()
  })

  it('shows AI workbench menus in the standard business sidebar when the AI workbench is active', async () => {
    const container = await renderWithProviders('/ai-workbench/chat', {
      permissionKeys: [
        'workspace.resource.view',
        'observe.ai.view',
        'observe.ai.chat',
        'observe.monitoring.view',
        'overview.view',
        'system.menus.view',
      ],
      visibleMenuIds: [
        'dashboard',
        'ai-workbench',
        'ai-workbench-chat',
        'ai-workbench-inspection',
        'ai-workbench-model-settings',
        'monitoring-workbench',
        'monitoring-workbench-overview',
        'system',
        'menus',
      ],
      visibleMenus: [
        { id: 'dashboard', path: '/', labelZh: '概览', labelEn: 'Overview', iconKey: 'gauge', section: 'platform', sortOrder: 1, enabled: true },
        { id: 'ai-workbench', path: '/ai-workbench', labelZh: 'AI工作台', labelEn: 'AI Workbench', iconKey: 'bot', section: 'ops', sortOrder: 15, enabled: true },
        { id: 'ai-workbench-chat', parentId: 'ai-workbench', path: '/ai-workbench/chat', labelZh: '通用聊天', labelEn: 'Chat', iconKey: 'bot', section: 'ops', sortOrder: 16, enabled: true },
        { id: 'ai-workbench-inspection', parentId: 'ai-workbench', path: '/ai-workbench/inspection', labelZh: '巡检', labelEn: 'Inspection', iconKey: 'bot', section: 'ops', sortOrder: 19, enabled: true },
        { id: 'ai-workbench-model-settings', parentId: 'ai-workbench', path: '/ai-workbench/model-settings', labelZh: '模型设置', labelEn: 'Model Settings', iconKey: 'bot', section: 'ops', sortOrder: 20, enabled: true },
        { id: 'monitoring-workbench', path: '/monitoring-workbench', labelZh: '监控工作台', labelEn: 'Monitoring Workbench', iconKey: 'gauge', section: 'ops', sortOrder: 60, enabled: true },
        { id: 'monitoring-workbench-overview', parentId: 'monitoring-workbench', path: '/monitoring-workbench/overview', labelZh: '总览', labelEn: 'Overview', iconKey: 'gauge', section: 'ops', sortOrder: 61, enabled: true },
        { id: 'system', path: '/system', labelZh: '系统管理', labelEn: 'System', iconKey: 'panels-top-left', section: 'admin', sortOrder: 99, enabled: true },
        { id: 'menus', parentId: 'system', path: '/system/menus', labelZh: '菜单管理', labelEn: 'Menus', iconKey: 'menu-square', section: 'admin', sortOrder: 100, enabled: true },
      ],
    })

    expect(container.querySelector('.kc-workbench-switcher__label')?.textContent).toBe('AI工作台')
    expect(container.querySelector('.kc-nav-business')).not.toBeNull()
    expect(container.querySelector('.kc-nav-system')).toBeNull()
    expect(container.textContent).toContain('通用聊天')
    expect(container.textContent).toContain('巡检')
    expect(container.textContent).not.toContain('监控工作台')
    expect(container.textContent).not.toContain('系统管理')
  })

  it('shows virtualization workbench menus directly in the business sidebar', async () => {
    const container = await renderWithProviders('/virtualization/vms', {
      permissionKeys: [
        'workspace.resource.view',
        'overview.view',
        'virtualization.overview.view',
        'virtualization.vms.view',
        'virtualization.operations.view',
        'virtualization.sync.manage',
        'observe.monitoring.view',
        'system.menus.view',
      ],
      visibleMenuIds: [
        'dashboard',
        'virtualization-workbench',
        'virtualization-workbench-overview',
        'virtualization-workbench-vms',
        'virtualization-workbench-operations',
        'virtualization-workbench-sync',
        'monitoring-workbench',
        'monitoring-workbench-overview',
        'system',
        'menus',
      ],
      visibleMenus: [
        { id: 'dashboard', path: '/', labelZh: '概览', labelEn: 'Overview', iconKey: 'gauge', section: 'platform', sortOrder: 1, enabled: true },
        { id: 'virtualization-workbench', path: '/virtualization', labelZh: '虚拟化管理工作台', labelEn: 'Virtualization Workbench', iconKey: 'server', section: 'ops', sortOrder: 10, enabled: true },
        { id: 'virtualization-workbench-overview', parentId: 'virtualization-workbench', path: '/virtualization/overview', labelZh: '总览', labelEn: 'Overview', iconKey: 'server', section: 'ops', sortOrder: 11, enabled: true },
        { id: 'virtualization-workbench-vms', parentId: 'virtualization-workbench', path: '/virtualization/vms', labelZh: '虚拟机', labelEn: 'Virtual Machines', iconKey: 'server', section: 'ops', sortOrder: 12, enabled: true },
        { id: 'virtualization-workbench-operations', parentId: 'virtualization-workbench', path: '/virtualization/operations', labelZh: '操作记录', labelEn: 'Operations', iconKey: 'file-clock', section: 'ops', sortOrder: 13, enabled: true },
        { id: 'virtualization-workbench-sync', parentId: 'virtualization-workbench', path: '/virtualization/sync', labelZh: '同步任务', labelEn: 'Sync', iconKey: 'activity', section: 'ops', sortOrder: 14, enabled: true },
        { id: 'monitoring-workbench', path: '/monitoring-workbench', labelZh: '监控工作台', labelEn: 'Monitoring Workbench', iconKey: 'gauge', section: 'ops', sortOrder: 60, enabled: true },
        { id: 'monitoring-workbench-overview', parentId: 'monitoring-workbench', path: '/monitoring-workbench/overview', labelZh: '总览', labelEn: 'Overview', iconKey: 'gauge', section: 'ops', sortOrder: 61, enabled: true },
        { id: 'system', path: '/system', labelZh: '系统管理', labelEn: 'System', iconKey: 'panels-top-left', section: 'admin', sortOrder: 99, enabled: true },
        { id: 'menus', parentId: 'system', path: '/system/menus', labelZh: '菜单管理', labelEn: 'Menus', iconKey: 'menu-square', section: 'admin', sortOrder: 100, enabled: true },
      ],
    })

    expect(container.querySelector('.kc-workbench-switcher__label')?.textContent).toBe('虚拟化管理工作台')
    expect(container.querySelector('.kc-nav-business')).not.toBeNull()
    expect(container.textContent).toContain('虚拟机')
    expect(container.textContent).toContain('操作记录')
    expect(container.textContent).toContain('同步任务')
    const businessNavText = container.querySelector('.kc-nav-business')?.textContent ?? ''
    expect((businessNavText.match(/虚拟化/g) ?? [])).toHaveLength(0)
    expect(container.textContent).not.toContain('监控工作台')
    expect(container.textContent).not.toContain('系统管理')
  })

  it('shows a settings entry in the header and routes system navigation through the main sidebar', async () => {
    const container = await renderWithProviders('/', {
      permissionKeys: ['workspace.resource.view', 'overview.view', 'settings.identity.view', 'system.menus.view'],
      visibleMenuIds: ['dashboard', 'settings', 'system', 'menus'],
      visibleMenus: [
        { id: 'dashboard', path: '/', labelZh: '概览', labelEn: 'Overview', iconKey: 'gauge', section: 'platform', sortOrder: 1, enabled: true },
        { id: 'settings', path: '/settings', labelZh: '设置中心', labelEn: 'Settings', iconKey: 'settings', section: 'admin', sortOrder: 2, enabled: true },
        { id: 'system', path: '/system', labelZh: '系统管理', labelEn: 'System', iconKey: 'panels-top-left', section: 'admin', sortOrder: 3, enabled: true },
        { id: 'menus', parentId: 'system', path: '/system/menus', labelZh: '菜单管理', labelEn: 'Menus', iconKey: 'menu-square', section: 'admin', sortOrder: 4, enabled: true },
      ],
    })

    const settingsButton = container.querySelector('button[aria-label="系统设置"]')
    expect(settingsButton).not.toBeNull()
    expect(container.querySelector('.kc-nav-system')).toBeNull()
  })
})
