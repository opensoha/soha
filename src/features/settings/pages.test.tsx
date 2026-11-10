/** @vitest-environment jsdom */

import type { PermissionSnapshot } from '@/types'
import { useAuthStore } from '@/stores/auth-store'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import type { ReactNode } from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { AISettingsPage } from './ai/page'
import { BrandingSettingsPage } from './branding/page'
import { LoginSettingsPage } from './identity/page'
import { SettingsOverviewPage } from './overview/page'

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: [
      'access.users.view',
      'access.roles.view',
      'access.groups.view',
      'access.policies.view',
      'settings.identity.view',
      'settings.identity.update',
      'settings.branding.view',
      'settings.branding.create',
      'settings.branding.update',
      'settings.ai.view',
      'settings.ai.update',
      'settings.system-integrations.view',
      'settings.runtime-config.view',
      'observe.ai.view',
      'observe.ai.chat',
      'plugin.view',
    ],
    visibleMenuIds: [
      'settings',
      'account-profile',
      'settings-about',
      'settings-login',
      'settings-branding',
    ],
    visibleMenus: [
      { id: 'settings', path: '/settings', labelZh: '设置中心' },
      {
        id: 'account-profile',
        parentId: 'settings',
        path: '/account/profile',
        labelZh: '个人中心',
        sortOrder: 10,
      },
      {
        id: 'settings-about',
        parentId: 'settings',
        path: '/settings/about',
        labelZh: '关于',
        sortOrder: 20,
      },
      {
        id: 'settings-login',
        parentId: 'settings',
        path: '/settings/login',
        labelZh: '登录设置',
        sortOrder: 261,
      },
      {
        id: 'settings-branding',
        parentId: 'settings',
        path: '/settings/branding',
        labelZh: '品牌设置',
        sortOrder: 262,
      },
    ],
  } as PermissionSnapshot,
  responses: {} as Record<string, unknown>,
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn(
    (path: string): Promise<{ data: unknown }> =>
      Promise.resolve({ data: testState.responses[path] ?? {} }),
  ),
)
const apiPostMock = vi.hoisted(() => vi.fn(() => Promise.resolve({ data: {} })))
const apiPutMock = vi.hoisted(() => vi.fn(() => Promise.resolve({ data: {} })))

vi.mock('@/features/auth/permission-snapshot', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/permission-snapshot')>(
    '@/features/auth/permission-snapshot',
  )
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: { data: testState.snapshot },
      isLoading: false,
    }),
  }
})

vi.mock('@/services/api-client', () => ({
  api: {
    get: apiGetMock,
    put: apiPutMock,
    post: apiPostMock,
    delete: vi.fn(),
    upload: vi.fn(),
  },
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    title,
    headerExtra,
    toolbar,
    toolbarExtra,
    dataSource,
    columns,
  }: {
    title?: ReactNode
    headerExtra?: ReactNode
    toolbar?: ReactNode
    toolbarExtra?: ReactNode
    dataSource: unknown[]
    columns?: Array<{
      dataIndex?: string
      key?: string
      render?: (value: unknown, record: unknown, index: number) => ReactNode
    }>
  }) => (
    <div data-testid="admin-table">
      {title ? <div>{title}</div> : null}
      {headerExtra ? <div data-testid="admin-table-header-extra">{headerExtra}</div> : null}
      {toolbar ? <div data-testid="admin-table-toolbar">{toolbar}</div> : null}
      {toolbarExtra ? <div data-testid="admin-table-toolbar-extra">{toolbarExtra}</div> : null}
      <div>{`rows:${dataSource.length}`}</div>
      {dataSource.map((item, index) => (
        <div key={index}>
          <div>{JSON.stringify(item)}</div>
          {columns?.map((column, columnIndex) => (
            <span key={`${column.key || column.dataIndex || 'column'}-${columnIndex}`}>
              {column.render
                ? column.render(
                    column.dataIndex
                      ? (item as Record<string, unknown>)[column.dataIndex]
                      : undefined,
                    item,
                    index,
                  )
                : null}
            </span>
          ))}
        </div>
      ))}
    </div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setDefaultResponses() {
  testState.responses = {
    '/settings/identity': {
      providers: [
        {
          id: 'corp-oidc',
          name: 'OIDC',
          type: 'oidc',
          enabled: true,
          issuer: 'https://accounts.example.com',
          clientId: 'client',
          clientSecret: 'secret',
          redirectUrl: 'http://127.0.0.1:8080/api/v1/auth/login/corp-oidc/callback',
          frontendRedirectUrl: 'http://127.0.0.1:5173/login/callback',
          scopes: ['openid', 'profile', 'email'],
          defaultRoles: ['readonly'],
          userIdField: 'sub',
          userNameField: 'name',
          emailField: 'email',
        },
      ],
      defaultProviderId: 'corp-oidc',
    },
    '/access/roles': [
      {
        id: 'readonly',
        name: '只读用户',
        scope: 'global',
        capabilities: [],
        userCount: 0,
      },
      {
        id: 'admin',
        name: '管理员',
        scope: 'global',
        capabilities: [],
        userCount: 1,
      },
    ],
    '/access/users': [
      {
        id: 'user-1',
        username: 'admin',
        email: 'admin@example.com',
        displayName: 'Admin',
        status: 'active',
        tags: [],
        roles: ['admin'],
        teams: ['team-1'],
        projects: [],
        loginSources: [],
      },
      {
        id: 'user-2',
        username: 'disabled',
        email: 'disabled@example.com',
        displayName: 'Disabled',
        status: 'disabled',
        tags: [],
        roles: [],
        teams: [],
        projects: [],
        loginSources: [],
      },
    ],
    '/access/teams': [
      {
        id: 'team-1',
        name: '平台组',
        slug: 'platform',
        metadata: {},
        userCount: 1,
      },
    ],
    '/access/policies': [{ id: 'policy-1', name: '管理员策略', effect: 'allow', priority: 100 }],
    '/auth/sessions': [
      {
        id: 'session-1',
        userId: 'user-1',
        userName: 'admin',
        email: 'admin@example.com',
        providerType: 'local',
        status: 'active',
        createdAt: '2026-08-13T08:00:00Z',
        lastSeenAt: '2026-08-13T09:00:00Z',
        expiresAt: '2026-08-14T08:00:00Z',
      },
      {
        id: 'session-2',
        userId: 'user-1',
        userName: 'admin',
        email: 'admin@example.com',
        providerType: 'oidc',
        status: 'active',
        createdAt: '2026-08-13T08:30:00Z',
        lastSeenAt: '2026-08-13T09:00:00Z',
        expiresAt: '2026-08-14T08:30:00Z',
      },
      {
        id: 'session-3',
        userId: 'user-2',
        userName: 'operator',
        email: 'operator@example.com',
        providerType: 'oidc',
        status: 'active',
        createdAt: '2026-08-13T08:45:00Z',
        lastSeenAt: '2026-08-13T09:00:00Z',
        expiresAt: '2026-08-14T08:45:00Z',
      },
    ],
    '/operations/summary': {
      total: 8,
      failureCount: 1,
      retentionDays: 30,
      expiredEntryCount: 0,
      exportRecommended: false,
    },
    '/audit/summary': {
      total: 12,
      retentionDays: 90,
      expiredEntryCount: 0,
      exportRecommended: false,
    },
    '/settings/branding': {
      appTitle: 'Soha',
      sidebarTitle: 'Soha',
      slogan: 'Soha 是一种能力！',
      loginLogoUrl: 'https://cdn.example.com/legacy-login.svg',
      expandedLogoUrl: 'https://cdn.example.com/logo.svg',
      darkExpandedLogoUrl: 'https://cdn.example.com/dark-logo.svg',
      darkCollapsedLogoUrl: '',
      collapsedLogoUrl: '',
      faviconUrl: '',
    },
    '/settings/ai': {
      workbenchModel: {
        enabled: true,
        defaultPublicModel: 'gpt-public',
        defaultRouteId: 'route-openai',
        defaultEndpoint: 'chat/completions',
      },
      skillsRegistry: [
        {
          id: 'skill-1',
          name: 'Skill One',
          category: 'observability',
          enabled: true,
        },
      ],
    },
    '/settings/ai/workbench-runtime-assets': [],
    '/settings/ai/skills': {
      skillsRegistry: [
        {
          id: 'skill-1',
          name: 'Skill One',
          category: 'observability',
          enabled: true,
        },
      ],
    },
    '/ai-gateway/relay/model-routes?includeDisabled=true': [
      {
        id: 'route-openai',
        publicModel: 'gpt-public',
        upstreamId: 'upstream-openai',
        upstreamModel: 'gpt-4.1-mini',
        endpoint: 'chat/completions',
        enabled: true,
      },
      {
        id: 'route-disabled',
        publicModel: 'gpt-disabled',
        upstreamId: 'upstream-disabled',
        upstreamModel: 'gpt-disabled',
        endpoint: 'chat/completions',
        enabled: false,
      },
    ],
    '/copilot/data-sources': [],
    '/copilot/analysis-profiles': [],
    '/copilot/automation-policies': [],
    '/copilot/data-source-capabilities': [],
    '/copilot/workbench/catalog': {
      agentProviders: [
        {
          id: 'internal',
          kind: 'internal',
          name: 'soha 内置分析',
          enabled: true,
          default: true,
          capabilities: ['root_cause'],
          supportsAsync: false,
          supportsSkills: true,
          supportsToolsets: true,
        },
        {
          id: 'hermes',
          kind: 'hermes',
          name: 'Hermes Agent',
          description: '通过 soha agent runner 调用 Hermes CLI。',
          enabled: true,
          capabilities: ['root_cause', 'delivery_failure'],
          supportsAsync: true,
          supportsSkills: true,
          supportsToolsets: true,
        },
      ],
      capabilities: [
        { id: 'root_cause', name: '根因分析' },
        { id: 'delivery_failure', name: '发布失败分析' },
      ],
    },
    '/copilot/agent-runs': [
      {
        id: 'agent-run-1',
        providerId: 'hermes',
        providerKind: 'hermes',
        capabilityId: 'root_cause',
        status: 'running',
        claimedByAgentId: 'hermes-agent-runner',
        queuedAt: '2026-06-04T10:00:00Z',
        startedAt: '2026-06-04T10:01:00Z',
        lastHeartbeatAt: '2026-06-04T10:02:00Z',
        createdAt: '2026-06-04T10:00:00Z',
        updatedAt: '2026-06-04T10:02:00Z',
      },
    ],
  }
}

async function renderWithProviders(node: ReactNode, route: string) {
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
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[route]}>{node}</MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })

  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })

  return container
}

describe('settings ai page rendering', () => {
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
    Object.defineProperty(window, 'getComputedStyle', {
      writable: true,
      value: vi.fn().mockReturnValue({
        getPropertyValue: () => '',
        overflow: 'auto',
        overflowX: 'auto',
        overflowY: 'auto',
      }),
    })
  })

  beforeEach(() => {
    apiGetMock.mockImplementation((path: string) =>
      Promise.resolve({ data: testState.responses[path] ?? {} }),
    )
    apiPostMock.mockResolvedValue({ data: {} })
    apiPutMock.mockResolvedValue({ data: {} })
    testState.snapshot = {
      permissionKeys: [
        'access.users.view',
        'access.roles.view',
        'access.groups.view',
        'access.policies.view',
        'settings.identity.view',
        'settings.identity.update',
        'settings.branding.view',
        'settings.branding.create',
        'settings.branding.update',
        'settings.ai.view',
        'settings.ai.update',
        'settings.system-integrations.view',
        'settings.runtime-config.view',
        'system.online-users.view',
        'system.audit.view',
        'system.operations.view',
        'observe.ai.view',
        'observe.ai.chat',
        'plugin.view',
      ],
      visibleMenuIds: [
        'settings',
        'account-profile',
        'settings-about',
        'settings-login',
        'settings-branding',
      ],
      visibleMenus: [
        { id: 'settings', path: '/settings', labelZh: '设置中心' },
        {
          id: 'account-profile',
          parentId: 'settings',
          path: '/account/profile',
          labelZh: '个人中心',
          sortOrder: 10,
        },
        {
          id: 'settings-about',
          parentId: 'settings',
          path: '/settings/about',
          labelZh: '关于',
          sortOrder: 20,
        },
        {
          id: 'settings-login',
          parentId: 'settings',
          path: '/settings/login',
          labelZh: '登录设置',
          sortOrder: 261,
        },
        {
          id: 'settings-branding',
          parentId: 'settings',
          path: '/settings/branding',
          labelZh: '品牌设置',
          sortOrder: 262,
        },
      ],
    }
    setDefaultResponses()
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

  it('renders access and governance metrics without repeated navigation shortcuts', async () => {
    const container = await renderWithProviders(<SettingsOverviewPage />, '/settings/overview')

    expect(container.querySelector('.soha-management-detail-header')).toBeNull()
    expect(container.textContent).toContain('用户总数')
    expect(container.textContent).toContain('角色数')
    expect(container.textContent).toContain('组织数')
    expect(container.textContent).toContain('访问策略')
    expect(container.textContent).toContain('当前可正常登录')
    expect(container.textContent).toContain('当前已禁止登录')
    expect(container.textContent).toContain('尚未获得角色权限')
    expect(container.textContent).toContain('尚未加入任何组织')
    expect(container.textContent).toContain('系统活动')
    expect(container.textContent).toContain('在线用户2')
    expect(container.textContent).toContain('3 个活跃会话')
    expect(container.textContent).toContain('账户与权限')
    expect(container.textContent).toContain('操作记录8')
    expect(container.textContent).toContain('失败 1')
    expect(container.textContent).toContain('审计记录12')
    expect(container.textContent).toContain('保留 90 天')
    expect(container.querySelectorAll('.soha-overview-section-bar')).toHaveLength(0)
    expect(container.querySelectorAll('.soha-overview-panel-card > .ant-card-head')).toHaveLength(2)
    expect(container.querySelector('a[href="/access/users"]')?.textContent).toContain('查看用户')
    expect(container.querySelector('a[href="/system/online-users"]')?.textContent).toContain(
      '查看会话',
    )
    expect(container.querySelector('a[href="/system/operations"]')?.textContent).toContain(
      '查看操作',
    )
    expect(container.querySelector('a[href="/system/audit"]')?.textContent).toContain('查看审计')
    expect(container.querySelectorAll('.soha-overview-metric-card.is-default')).toHaveLength(4)
    expect(container.querySelector('.soha-settings-overview-chip-grid')).toBeNull()
    expect(container.textContent).not.toContain('常用入口')
    expect(container.textContent).not.toContain('品牌配置')
    expect(container.textContent).not.toContain('认证与品牌')
  })

  it('only loads overview data allowed by access permissions', async () => {
    testState.snapshot = {
      permissionKeys: ['access.users.view'],
      visibleMenuIds: ['settings', 'settings-overview'],
      visibleMenus: [
        { id: 'settings', path: '/settings', labelZh: '设置中心' },
        {
          id: 'settings-overview',
          parentId: 'settings',
          path: '/settings/overview',
          labelZh: '总览',
          sortOrder: 1,
        },
      ],
    }

    const container = await renderWithProviders(<SettingsOverviewPage />, '/settings/overview')

    expect(container.textContent).toContain('用户总数')
    expect(apiGetMock).toHaveBeenCalledWith('/access/users')
    expect(apiGetMock).not.toHaveBeenCalledWith('/access/roles')
    expect(apiGetMock).not.toHaveBeenCalledWith('/access/teams')
    expect(apiGetMock).not.toHaveBeenCalledWith('/access/policies')
    expect(apiGetMock).not.toHaveBeenCalledWith('/auth/sessions')
    expect(apiGetMock).not.toHaveBeenCalledWith('/audit/summary')
    expect(apiGetMock).not.toHaveBeenCalledWith('/operations/summary')
    expect(container.querySelector('a[href="/system/online-users"]')).toBeNull()
    expect(container.querySelector('a[href="/system/operations"]')).toBeNull()
    expect(container.querySelector('a[href="/system/audit"]')).toBeNull()
  })

  it('shows retryable errors instead of empty settings data', async () => {
    const failingPaths = new Set([
      '/access/users',
      '/settings/identity',
      '/settings/branding',
      '/settings/ai',
    ])
    apiGetMock.mockImplementation((path: string) =>
      failingPaths.has(path)
        ? Promise.reject(new Error('request failed'))
        : Promise.resolve({ data: testState.responses[path] ?? {} }),
    )

    const overview = await renderWithProviders(<SettingsOverviewPage />, '/settings/overview')
    const login = await renderWithProviders(<LoginSettingsPage />, '/settings/login')
    const branding = await renderWithProviders(<BrandingSettingsPage />, '/settings/branding')
    const ai = await renderWithProviders(<AISettingsPage />, '/ai-workbench/model-settings')

    for (const container of [overview, login, branding, ai]) {
      expect(container.textContent).toMatch(/读取失败|加载失败/)
      expect(container.textContent).toContain('重试')
    }
  })

  it.each(['/auth/sessions', '/audit/summary', '/operations/summary'])(
    'keeps primary overview data visible when optional activity query %s fails',
    async (failingPath) => {
      apiGetMock.mockImplementation((path: string) =>
        path === failingPath
          ? Promise.reject(new Error('request failed'))
          : Promise.resolve({ data: testState.responses[path] ?? {} }),
      )

      const overview = await renderWithProviders(<SettingsOverviewPage />, '/settings/overview')

      expect(overview.textContent).toContain('用户总数')
      expect(overview.textContent).toContain('角色数')
      expect(overview.textContent).toContain(
        ({
          '/auth/sessions': '在线会话',
          '/audit/summary': '审计汇总',
          '/operations/summary': '操作汇总',
        }[failingPath] ?? '') + '读取失败',
      )
      expect(overview.querySelector('button[aria-label^="重试读取"]')).not.toBeNull()
    },
  )

  it.each(['/access/users', '/access/roles', '/access/teams', '/access/policies'])(
    'keeps successful access metrics visible when %s fails',
    async (failingPath) => {
      apiGetMock.mockImplementation((path: string) =>
        path === failingPath
          ? Promise.reject(new Error('request failed'))
          : Promise.resolve({ data: testState.responses[path] ?? {} }),
      )

      const overview = await renderWithProviders(<SettingsOverviewPage />, '/settings/overview')

      expect(overview.textContent).toContain('用户总数')
      expect(overview.textContent).toContain('角色数')
      expect(overview.textContent).toContain('组织数')
      expect(overview.textContent).toContain('访问策略')
      expect(overview.textContent).toContain(
        ({
          '/access/users': '用户',
          '/access/roles': '角色',
          '/access/teams': '组织',
          '/access/policies': '访问策略',
        }[failingPath] ?? '') + '读取失败',
      )
      expect(overview.textContent).toContain('加载失败')
      expect(overview.querySelector('button[aria-label^="重试读取"]')).not.toBeNull()
    },
  )

  it('does not request protected settings data without view permissions', async () => {
    testState.snapshot = {
      permissionKeys: [],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const login = await renderWithProviders(<LoginSettingsPage />, '/settings/login')
    const branding = await renderWithProviders(<BrandingSettingsPage />, '/settings/branding')
    const ai = await renderWithProviders(<AISettingsPage />, '/ai-workbench/model-settings')

    expect(login.textContent).toContain('当前账号没有查看登录设置的权限。')
    expect(branding.textContent).toContain('当前账号没有查看品牌设置的权限。')
    expect(ai.textContent).toContain('当前账号没有查看 AI 设置的权限。')
    for (const path of [
      '/settings/identity',
      '/settings/branding',
      '/settings/ai',
      '/ai-gateway/relay/model-routes?includeDisabled=true',
      '/copilot/data-sources',
      '/copilot/analysis-profiles',
      '/copilot/data-source-capabilities',
      '/copilot/workbench/catalog',
      '/copilot/agent-runs',
      '/plugins/installed',
    ]) {
      expect(apiGetMock).not.toHaveBeenCalledWith(path)
    }
  })

  it('does not load login role options without the role view permission', async () => {
    testState.snapshot = {
      permissionKeys: ['settings.identity.view'],
      visibleMenuIds: [],
      visibleMenus: [],
    }

    const login = await renderWithProviders(<LoginSettingsPage />, '/settings/login')

    expect(login.textContent).toContain('本地账号密码登录')
    expect(apiGetMock).toHaveBeenCalledWith('/settings/identity')
    expect(apiGetMock).not.toHaveBeenCalledWith('/access/roles')
  })

  it('renders login settings on /settings/login', async () => {
    const container = await renderWithProviders(<LoginSettingsPage />, '/settings/login')

    expect(container.textContent).toContain('新增登录源')
    expect(container.textContent).toContain('OIDC')
    expect(container.querySelector('button[aria-label="本地账号密码登录"]')).not.toBeNull()
    expect(container.querySelector('button[aria-label="切换表格密度"]')).not.toBeNull()
    expect(container.querySelector('button[aria-label="刷新登录源"]')).not.toBeNull()
    expect(container.textContent).not.toContain(
      '配置 OIDC、飞书、钉钉、企业微信、OAuth2 与 SAML 登录源。',
    )
  })

  it('renders the saved branding preview and saves through the branding contract', async () => {
    const container = await renderWithProviders(<BrandingSettingsPage />, '/settings/branding')

    expect(container.querySelector('img[src="https://cdn.example.com/logo.svg"]')).not.toBeNull()
    expect((container.querySelector('#slogan') as HTMLInputElement).value).toBe('Soha 是一种能力！')
    expect(
      Array.from(container.querySelectorAll('.soha-branding-upload-hint')).map(
        (element) => element.textContent,
      ),
    ).toEqual([
      '用于登录页左侧主视觉和展开侧边栏；支持 JPG、PNG、SVG、ICO、WebP；建议尺寸 200 × 60 px；单个文件不超过 2 MB',
      '用于登录卡片和收起侧边栏；支持 JPG、PNG、SVG、ICO、WebP；建议尺寸 60 × 60 px；单个文件不超过 2 MB',
      '支持 JPG、PNG、SVG、ICO、WebP；建议尺寸 16 × 16、32 × 32 或 64 × 64 px；单个文件不超过 2 MB',
      '用于暗色侧边栏和登录页主视觉；未上传时沿用原图。建议使用透明底 PNG、SVG 或 WebP，200 × 60 px，不超过 2 MB',
      '用于暗色收起侧边栏和登录卡片；未上传时沿用原图。建议使用透明底 PNG、SVG 或 WebP，60 × 60 px，不超过 2 MB',
    ])
    expect(
      Array.from(container.querySelectorAll('button.soha-branding-upload-area')).map((button) =>
        button.getAttribute('aria-label'),
      ),
    ).toEqual([
      '替换登录页主视觉与展开侧边栏 Logo',
      '上传登录卡片与收起侧边栏图标',
      '上传Favicon 图标',
      '替换暗色展开 Logo',
      '上传暗色收起图标',
    ])

    const saveButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('保存设置'),
    )
    await act(async () => {
      saveButton?.click()
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiPutMock).toHaveBeenCalledWith(
      '/settings/branding',
      expect.objectContaining({
        appTitle: 'Soha',
        sidebarTitle: 'Soha',
        slogan: 'Soha 是一种能力！',
        loginLogoUrl: 'https://cdn.example.com/legacy-login.svg',
        darkExpandedLogoUrl: 'https://cdn.example.com/dark-logo.svg',
        darkCollapsedLogoUrl: '',
      }),
    )
  })

  it('does not overwrite an edited branding form during background refresh', async () => {
    let brandingReads = 0
    apiGetMock.mockImplementation((path: string) => {
      if (path === '/settings/branding') {
        brandingReads += 1
        return Promise.resolve({
          data: {
            ...(testState.responses[path] as Record<string, unknown>),
            appTitle: brandingReads === 1 ? 'Initial title' : 'Server title',
          },
        })
      }
      return Promise.resolve({ data: testState.responses[path] ?? {} })
    })

    const container = await renderWithProviders(<BrandingSettingsPage />, '/settings/branding')
    const appTitle = container.querySelector('#appTitle') as HTMLInputElement
    const valueSetter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set

    await act(async () => {
      valueSetter?.call(appTitle, 'Local draft')
      appTitle.dispatchEvent(new Event('input', { bubbles: true }))
      appTitle.dispatchEvent(new Event('change', { bubbles: true }))
    })

    const saveButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('保存设置'),
    )
    await act(async () => {
      saveButton?.click()
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(brandingReads).toBeGreaterThan(1)
    expect(appTitle.value).toBe('Local draft')
  })

  it('renders empty login settings when providers are empty', async () => {
    testState.responses['/settings/identity'] = {
      providers: [],
      defaultProviderId: '',
    }

    const container = await renderWithProviders(<LoginSettingsPage />, '/settings/login')

    expect(container.textContent).toContain('rows:0')
    expect(container.textContent).not.toContain('corp-oidc')
  })

  it('saves login source enabled state from the table switch', async () => {
    const container = await renderWithProviders(<LoginSettingsPage />, '/settings/login')
    const enabledSwitch = container.querySelector(
      'button[role="switch"][aria-label="启用 OIDC"]',
    ) as HTMLButtonElement | null

    expect(enabledSwitch).toBeTruthy()

    await act(async () => {
      enabledSwitch?.click()
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiPutMock).toHaveBeenCalledWith('/settings/identity/providers', {
      providers: [
        expect.objectContaining({
          id: 'corp-oidc',
          enabled: false,
        }),
      ],
      defaultProviderId: 'corp-oidc',
      localPasswordLoginEnabled: true,
    })
  })

  it('hides disabled login role and organization mapping fields', async () => {
    const container = await renderWithProviders(<LoginSettingsPage />, '/settings/login')
    const addButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('新增登录源'),
    ) as HTMLButtonElement | undefined

    expect(addButton).toBeTruthy()

    await act(async () => {
      addButton?.click()
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('登录补充角色')
    expect(document.body.textContent).toContain('登录补充组织')
    expect(document.body.textContent).not.toContain('角色字段')
    expect(document.body.textContent).not.toContain('组织字段')
    expect(document.body.textContent).toContain('显示名字段')
    expect(document.body.textContent).not.toContain('用户名字段')
    expect(document.body.textContent).toContain('手机号字段')
    expect(document.body.textContent).toContain('头像字段')
  })

  it('advances the login source step form without saving', async () => {
    await renderWithProviders(<LoginSettingsPage />, '/settings/login')
    const editButton = document.body.querySelector(
      'button[aria-label="编辑登录源"]',
    ) as HTMLButtonElement | null

    expect(editButton).toBeTruthy()

    await act(async () => {
      editButton?.click()
      await Promise.resolve()
    })

    const nextButton = Array.from(document.body.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('下一步'),
    ) as HTMLButtonElement | undefined

    expect(nextButton).toBeTruthy()

    await act(async () => {
      nextButton?.click()
      await Promise.resolve()
    })

    expect(apiPutMock).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('上一步')
  })

  it('offers system roles in the login source default role selector', async () => {
    await renderWithProviders(<LoginSettingsPage />, '/settings/login')
    const editButton = document.body.querySelector(
      'button[aria-label="编辑登录源"]',
    ) as HTMLButtonElement | null

    await act(async () => {
      editButton?.click()
      await Promise.resolve()
    })

    const nextButton = Array.from(document.body.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('下一步'),
    ) as HTMLButtonElement | undefined

    await act(async () => {
      nextButton?.click()
      await Promise.resolve()
    })

    const defaultRoleSelect = document.body.querySelector('#defaultRoles') as HTMLElement | null

    expect(defaultRoleSelect).toBeTruthy()

    await act(async () => {
      defaultRoleSelect?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('只读用户')
    expect(document.body.textContent).toContain('管理员')
  })

  it('keeps the default model page focused on model routing', async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      '/ai-workbench/model-settings',
    )

    expect(container.querySelector('[data-testid="ai-workbench-model-section"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="ai-companion-section"]')).toBeNull()
    expect(container.querySelector('[data-testid="ai-agent-runtime-section"]')).toBeNull()
    expect(container.textContent).toContain('Workbench 默认模型')
    expect(container.textContent).toContain('模型 Provider 在 AI Gateway 管理')
    expect(container.textContent).toContain('gpt-public')
    expect(container.textContent).not.toContain('导入模型目录')
    expect(container.textContent).not.toContain('保存 Skills')
    expect(apiGetMock).not.toHaveBeenCalledWith('/copilot/data-sources')
    expect(apiGetMock).not.toHaveBeenCalledWith('/copilot/analysis-profiles')
    expect(apiGetMock).not.toHaveBeenCalledWith('/copilot/workbench/catalog')
    expect(apiGetMock).not.toHaveBeenCalledWith('/copilot/agent-runs')
    expect(apiGetMock).not.toHaveBeenCalledWith('/plugins/installed')
  })

  it('waits for the settings form to mount before updating its values', async () => {
    let resolveModelRoutes: ((value: { data: unknown }) => void) | undefined
    apiGetMock.mockImplementation((path: string) =>
      path === '/ai-gateway/relay/model-routes?includeDisabled=true'
        ? new Promise((resolve) => {
            resolveModelRoutes = resolve
          })
        : Promise.resolve({ data: testState.responses[path] ?? {} }),
    )
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)

    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      '/ai-workbench/model-settings',
    )
    expect(container.querySelector('[data-testid="ai-workbench-model-form"]')).toBeNull()

    await act(async () => {
      resolveModelRoutes?.({
        data: testState.responses['/ai-gateway/relay/model-routes?includeDisabled=true'],
      })
      await Promise.resolve()
    })
    await vi.waitFor(() => {
      expect(container.querySelector('[data-testid="ai-workbench-model-form"]')).not.toBeNull()
    })

    expect(consoleError.mock.calls.flat().join(' ')).not.toContain(
      'Instance created by `useForm` is not connected',
    )
    consoleError.mockRestore()
  })

  it('renders Companion independently from AI settings administration', async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded section="companion" />,
      '/ai-workbench/companion',
    )

    expect(container.querySelector('[data-testid="ai-companion-section"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="ai-workbench-model-section"]')).toBeNull()
    expect(container.textContent).toContain('导入模型目录')
    expect(apiGetMock).toHaveBeenCalledWith('/plugins/installed')
    expect(apiGetMock).not.toHaveBeenCalledWith('/settings/ai')
  })

  it('does not render legacy provider connection controls', async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      '/ai-workbench/model-settings',
    )

    expect(container.querySelector('[data-testid="ai-provider-connections-section"]')).toBeNull()
    expect(container.querySelector('[data-testid="ai-provider-add"]')).toBeNull()
    expect(document.body.querySelector('[data-testid="ai-provider-modal"]')).toBeNull()
  })

  it('saves workbench model settings through the converged settings endpoint', async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded />,
      '/ai-workbench/model-settings',
    )
    const saveButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('保存默认模型'),
    ) as HTMLButtonElement | undefined

    expect(saveButton).toBeTruthy()

    await act(async () => {
      saveButton?.click()
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiPutMock).toHaveBeenCalledWith('/settings/ai/workbench-model', {
      workbenchModel: expect.objectContaining({
        enabled: true,
        defaultPublicModel: 'gpt-public',
        defaultRouteId: 'route-openai',
        defaultEndpoint: 'chat/completions',
      }),
    })
  })

  it('deep-links to immutable runtime profile history without offering publication to a viewer', async () => {
    testState.snapshot.permissionKeys = testState.snapshot.permissionKeys.filter(
      (permission) => permission !== 'settings.ai.update',
    )
    const path = '/settings/ai/workbench-runtime-profiles/heya-default'
    const assetPath = '/settings/ai/workbench-runtime-assets'
    testState.responses[assetPath] = [
      {
        kind: 'mcp',
        id: 'private-mcp',
        version: '1',
        transport: 'stdio',
        executionPosition: 'personal-docker',
        enabled: true,
      },
    ]
    testState.responses[path] = [
      {
        id: 'heya-default',
        name: '个人排障',
        scope: 'heya',
        revision: 3,
        enabled: true,
        assets: [{ kind: 'hook', id: 'tool-audit', version: '1', required: true }],
      },
    ]
    const previous = useAuthStore.getState().user
    useAuthStore.getState().setUser({
      userId: 'profile-viewer',
      userName: 'Profile viewer',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    try {
      const container = await renderWithProviders(
        <AISettingsPage embedded />,
        '/ai-workbench/model-settings?runtimeProfile=heya-default',
      )
      for (
        let attempt = 0;
        attempt < 20 && !container.textContent?.includes('v3 · 个人排障');
        attempt++
      ) {
        await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
      }
      expect(container.textContent).toContain('v3 · 个人排障')
      expect(apiGetMock).toHaveBeenCalledWith(path)
      expect(container.textContent).toContain('tool-audit @ 1 · 必需')
      expect(apiGetMock).toHaveBeenCalledWith(assetPath)
      expect(container.textContent).toContain('需绑定个人环境')
      expect(container.querySelector('button[aria-label="停用 private-mcp @ 1"]')).toBeNull()
      expect(container.textContent).not.toContain('编辑并发布新版本')
      expect(apiPutMock).not.toHaveBeenCalled()
    } finally {
      await act(async () => roots.pop()?.unmount())
      await act(async () => {
        useAuthStore.setState({ user: previous })
        await new Promise((resolve) => setTimeout(resolve, 20))
      })
    }
  })

  it('limits profile reasoning choices to the effective model and rejects an unsupported saved value', async () => {
    testState.responses['/copilot/workbench/catalog'] = {
      defaultPublicModel: 'gpt-public',
      modelOptions: [{ publicModel: 'gpt-public', reasoningEfforts: ['low'] }],
    }
    testState.responses['/settings/ai/workbench-runtime-profiles/chat-default'] = [{
      id: 'chat-default', name: '排障配置', scope: 'chat', revision: 2, enabled: true, assets: [],
      modelPreferences: { reasoningEffort: 'high' },
    }]
    const previous = useAuthStore.getState().user
    useAuthStore.getState().setUser({
      userId: 'profile-editor', userName: 'Profile editor', email: '', roles: [], teams: [], projects: [], tags: [],
    })
    try {
      const container = await renderWithProviders(
        <AISettingsPage embedded />,
        '/ai-workbench/model-settings?runtimeProfile=chat-default',
      )
      const button = (root: ParentNode, label: string) =>
        Array.from(root.querySelectorAll('button')).find((item) => item.textContent === label)
      const settle = () => act(async () => new Promise((resolve) => setTimeout(resolve, 20)))
      for (let attempt = 0; attempt < 20 && !button(container, '编辑并发布新版本'); attempt++)
        await settle()
      const edit = button(container, '编辑并发布新版本')
      expect(edit).toBeTruthy()
      await act(async () => edit?.click())
      await settle()
      const dialog = document.querySelector('[role="dialog"]')!
      const effort = dialog.querySelector('[id="modelPreferences_reasoningEffort"]')!
      await act(async () => {
        effort.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
        effort.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
      })
      await settle()
      const choices = Array.from(document.querySelectorAll(
        '.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option-content',
      )).map((item) => item.textContent)
      expect(choices).toEqual(['自动', '轻度'])
      await act(async () => button(dialog, '发布新版本')?.click())
      await settle()
      expect(dialog.textContent).toContain('当前模型未确认支持此推理强度')
      expect(apiPutMock).not.toHaveBeenCalled()
    } finally {
      await act(async () => roots.pop()?.unmount())
      useAuthStore.setState({ user: previous })
    }
  })

  it('probes an exact MCP version and fences retries when cleanup is unconfirmed', async () => {
    const path = '/settings/ai/workbench-runtime-assets'
    const asset = { kind: 'mcp', id: 'evidence', version: '1.2', transport: 'http', enabled: false }
    testState.responses[path] = [asset]
    apiPutMock.mockImplementationOnce(async () => {
      const updated = { ...asset, enabled: true }
      testState.responses[path] = [updated]
      return { data: updated }
    })
    apiPostMock.mockResolvedValueOnce({ data: { status: 'ready', capabilities: ['tool:read'] } })
    apiPostMock.mockResolvedValueOnce({ data: { status: 'cleanup_unknown', capabilities: [] } })

    const previous = useAuthStore.getState().user
    useAuthStore.getState().setUser({
      userId: 'asset-manager',
      userName: 'Asset manager',
      email: '',
      roles: [],
      teams: [],
      projects: [],
      tags: [],
    })
    try {
      const container = await renderWithProviders(
        <AISettingsPage embedded />,
        '/ai-workbench/model-settings',
      )
      const button = (label: string) =>
        container.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)
      const waitFor = async (ready: () => boolean) => {
        for (let attempt = 0; attempt < 20 && !ready(); attempt++) {
          await act(async () => new Promise((resolve) => setTimeout(resolve, 10)))
        }
      }
      const click = async (label: string) => {
        const target = button(label)
        expect(target?.disabled).toBe(false)
        await act(async () => target?.click())
      }
      await waitFor(() => Boolean(button('启用 evidence @ 1.2')))
      await click('启用 evidence @ 1.2')
      await waitFor(() =>
        Boolean(button('停用 evidence @ 1.2') && !button('停用 evidence @ 1.2')?.disabled),
      )
      expect(apiPutMock).toHaveBeenCalledWith(`${path}/mcp/evidence/1.2`, { enabled: true })
      await click('探测 evidence @ 1.2 · Chat')
      await waitFor(() => Boolean(container.textContent?.includes('探测就绪')))
      expect(container.textContent).toContain('tool:read')
      expect(apiPostMock).toHaveBeenCalledWith(`${path}/mcp/evidence/1.2/probe`, { scope: 'chat' })
      await click('探测 evidence @ 1.2 · Heya')
      await waitFor(() => Boolean(container.textContent?.includes('探测进程清理未确认')))
      expect(apiPostMock).toHaveBeenCalledWith(`${path}/mcp/evidence/1.2/probe`, { scope: 'heya' })
      expect(container.textContent).toContain('探测进程清理未确认')
      expect(button('探测 evidence @ 1.2 · Heya')?.disabled).toBe(true)
    } finally {
      await act(async () => roots.pop()?.unmount())
      await act(async () => {
        useAuthStore.setState({ user: previous })
        await new Promise((resolve) => setTimeout(resolve, 20))
      })
    }
  })

  it('saves skills registry without provider connection payloads', async () => {
    const container = await renderWithProviders(
      <AISettingsPage embedded section="skills" />,
      '/ai-workbench/skills',
    )
    const saveButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('保存 Skills'),
    ) as HTMLButtonElement | undefined

    expect(saveButton).toBeTruthy()
    expect(apiGetMock).toHaveBeenCalledWith('/settings/ai/skills')
    expect(apiGetMock).not.toHaveBeenCalledWith('/settings/ai')

    await act(async () => {
      saveButton?.click()
      await Promise.resolve()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    expect(apiPutMock).toHaveBeenCalledWith('/settings/ai/skills', {
      skillsRegistry: [
        expect.objectContaining({
          id: 'skill-1',
          name: 'Skill One',
          enabled: true,
        }),
      ],
    })
    const serializedCalls = JSON.stringify(apiPutMock.mock.calls)
    expect(serializedCalls).not.toContain('apiKey')
    expect(serializedCalls).not.toContain('baseUrl')
  })

  it('offers Loki by default and lets an administrator disable a retired AI data source', async () => {
    testState.responses['/copilot/data-sources'] = [
      {
        id: 'old-traces',
        name: 'Old traces',
        sourceKind: 'traces',
        backendType: 'skywalking',
        mcpAdapter: 'traces.v1',
        enabled: true,
        validationStatus: 'success',
      },
    ]
    const page = await renderWithProviders(
      <AISettingsPage section="data-sources" />,
      '/ai-workbench/data-sources',
    )
    expect(page.textContent).toContain('后端已退役')
    expect(page.querySelector('[aria-label="编辑数据源"]')).toBeNull()
    expect(page.querySelector('[aria-label="校验数据源连接"]')).toBeNull()
    const disable = page.querySelector<HTMLButtonElement>('[aria-label="停用旧 AI 数据源"]')!
    await act(async () => disable.click())
    expect(apiPutMock).not.toHaveBeenCalled()
    const confirmation = Array.from(document.body.querySelectorAll<HTMLButtonElement>('button'))
      .find((button) => button.textContent?.replace(/\s/g, '') === '停用')!
    await act(async () => confirmation.click())
    expect(apiPutMock).toHaveBeenCalledWith('/copilot/data-sources/old-traces', {
      name: 'Old traces',
      sourceKind: 'traces',
      backendType: 'skywalking',
      enabled: false,
    })

    const add = Array.from(page.querySelectorAll('button')).find(
      (button) => button.textContent?.replace(/\s/g, '') === '新增',
    )!
    await act(async () => add.click())
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.textContent).toContain('Loki Cluster Label')
    const sourceKind = dialog.querySelector('#sourceKind')!
    await act(async () => {
      sourceKind.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
      sourceKind.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
    })
    const choices = Array.from(document.querySelectorAll(
      '.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option-content',
    )).map((item) => item.textContent)
    expect(choices).toEqual(['logs', 'metrics', 'platform-native'])
  })
})
