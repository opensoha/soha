/** @vitest-environment jsdom */

import type { AdminTableProps } from '@/components/admin-table'
import { useAuthStore } from '@/stores/auth-store'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { InternalWorkbenchAppHostingPage } from './page'
import type {
  AppHostingSettings,
  HostedApp,
  HostedAppRevision,
  HostedAppVisibilityApproval,
} from './types'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  getEnvelope: vi.fn(),
  put: vi.fn(),
  post: vi.fn(),
  patch: vi.fn(),
  delete: vi.fn(),
}))

const authState = vi.hoisted(() => ({
  loading: false,
  permissions: [] as string[],
}))

const tableState = vi.hoisted(() => ({
  propsList: [] as AdminTableProps[],
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

vi.mock('@/components/admin-table', () => ({
  AdminTable: (props: AdminTableProps) => {
    tableState.propsList.push(props)
    const { columns = [], dataSource = [], empty, headerExtra, pagination, rowKey } = props
    return (
      <div data-testid="admin-table">
        {headerExtra ? <div data-testid="admin-table-header-extra">{headerExtra}</div> : null}
        <table>
          <thead>
            <tr>
              {columns.map((column, colIdx) => (
                <th key={String(column?.key ?? column?.dataIndex ?? colIdx)}>
                  {typeof column?.title === 'string' ? column.title : null}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {dataSource.map((record, rowIdx) => {
              const resolvedKey =
                typeof rowKey === 'function' ? rowKey(record) : String(record?.[rowKey] ?? rowIdx)
              return (
                <tr key={resolvedKey} data-row-key={resolvedKey}>
                  {columns.map((column, colIdx) => {
                    const dataIndex = column?.dataIndex
                    const value = typeof dataIndex === 'string' ? record?.[dataIndex] : undefined
                    const rendered = column?.render
                      ? column.render(value, record, rowIdx)
                      : (value ?? null)
                    return (
                      <td key={String(column?.key ?? dataIndex ?? colIdx)}>
                        {rendered as ReactNode}
                      </td>
                    )
                  })}
                </tr>
              )
            })}
          </tbody>
        </table>
        {dataSource.length === 0 ? empty : null}
        {pagination && pagination !== false ? (
          <div data-testid="admin-table-pagination">
            {`total:${String(pagination.total ?? dataSource.length)}`}
          </div>
        ) : null}
      </div>
    )
  },
}))

vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({
    isLoading: authState.loading,
    data: authState.loading
      ? undefined
      : {
          data: {
            permissionKeys: authState.permissions,
            visibleMenuIds: ['identity-app-hosting'],
            visibleMenus: [],
          },
        },
  }),
  hasPermission: (snapshot: { permissionKeys?: string[] } | undefined, permissionKey?: string) => {
    if (!permissionKey) return true
    return snapshot?.permissionKeys?.includes(permissionKey) ?? false
  },
}))

const onlineApp: HostedApp = {
  id: 'app-online',
  name: '季度经营看板',
  description: '市场与财务经营指标汇总',
  kind: 'static',
  dnsLabel: 'q3-board-a1b2',
  host: 'q3-board-a1b2.apps.corp.example.com',
  url: 'https://q3-board-a1b2.apps.corp.example.com',
  spaFallback: true,
  ownerUserId: 'user-1',
  ownerProfile: {
    userId: 'user-1',
    displayName: '张三',
    teamNames: ['数据分析部'],
  },
  maintainerUserIds: ['user-3'],
  maintainers: [
    {
      userId: 'user-3',
      displayName: '王五',
      teamNames: ['数据分析部'],
    },
  ],
  visibility: {
    mode: 'selected',
    teamIds: ['team-mkt'],
    userIds: ['user-2'],
  },
  pendingVisibilityApproval: {
    id: 'apr-other',
    appId: 'app-online',
    appName: '季度经营看板',
    appDnsLabel: 'q3-board-a1b2',
    requesterUserId: 'user-1',
    currentVisibility: { mode: 'selected', teamIds: ['team-mkt'], userIds: ['user-2'] },
    requestedVisibility: { mode: 'organization' },
    baseRouteGeneration: 2,
    policyVersion: 3,
    status: 'pending',
    reason: '全员复盘会议使用',
    createdAt: '2026-11-21T02:00:00Z',
    updatedAt: '2026-11-21T02:00:00Z',
  },
  status: 'online',
  activeRevisionId: 'rev-2',
  activeRevisionNumber: 2,
  totalBytes: 2 * 1024 * 1024,
  routeGeneration: 2,
  createdAt: '2026-11-20T08:00:00Z',
  updatedAt: '2026-11-21T02:00:00Z',
}

const suspendedApp: HostedApp = {
  id: 'app-suspended',
  name: '异常轮询测试页',
  description: '高频请求测试页面',
  kind: 'static',
  dnsLabel: 'loop-test-c9d8',
  host: 'loop-test-c9d8.apps.corp.example.com',
  url: 'https://loop-test-c9d8.apps.corp.example.com',
  spaFallback: false,
  ownerUserId: 'user-2',
  ownerProfile: {
    userId: 'user-2',
    displayName: '李四',
    teamNames: ['研发平台部'],
  },
  maintainerUserIds: [],
  visibility: { mode: 'private' },
  status: 'suspended_by_admin',
  suspendedReason: '触发异常循环请求告警',
  activeRevisionId: 'rev-1',
  activeRevisionNumber: 1,
  totalBytes: 512 * 1024,
  routeGeneration: 5,
  createdAt: '2026-11-19T10:00:00Z',
  updatedAt: '2026-11-21T03:00:00Z',
}

const sampleRevisions: HostedAppRevision[] = [
  {
    id: 'rev-2',
    appId: 'app-online',
    revisionNumber: 2,
    description: '修复图表筛选',
    kind: 'static',
    status: 'ready',
    bundleSha256: 'abc123def456',
    fileCount: 12,
    totalBytes: 2 * 1024 * 1024,
    spaFallback: true,
    createdBy: 'user-1',
    createdAt: '2026-11-21T01:30:00Z',
    activatedAt: '2026-11-21T01:31:00Z',
  },
]

const pendingApprovals: HostedAppVisibilityApproval[] = [
  {
    id: 'apr-self',
    appId: 'app-admin-own',
    appName: '安全值班速查表',
    appDnsLabel: 'sec-duty-x1y2',
    requesterUserId: 'admin-self',
    requesterProfile: {
      userId: 'admin-self',
      displayName: '当前管理员',
      teamNames: ['安全治理组'],
    },
    currentVisibility: { mode: 'private' },
    requestedVisibility: { mode: 'organization' },
    baseRouteGeneration: 1,
    policyVersion: 3,
    status: 'pending',
    reason: '供全员查询值班接口人',
    createdAt: '2026-11-21T02:30:00Z',
    updatedAt: '2026-11-21T02:30:00Z',
  },
  {
    id: 'apr-other',
    appId: 'app-online',
    appName: '季度经营看板',
    appDnsLabel: 'q3-board-a1b2',
    requesterUserId: 'user-1',
    requesterProfile: {
      userId: 'user-1',
      displayName: '张三',
      teamNames: ['数据分析部'],
    },
    currentVisibility: { mode: 'selected', teamIds: ['team-mkt'], userIds: ['user-2'] },
    requestedVisibility: { mode: 'organization' },
    baseRouteGeneration: 2,
    policyVersion: 3,
    status: 'pending',
    reason: '全员复盘会议使用',
    createdAt: '2026-11-21T02:00:00Z',
    updatedAt: '2026-11-21T02:00:00Z',
  },
]

const invalidatedApprovals: HostedAppVisibilityApproval[] = [
  {
    id: 'apr-invalidated',
    appId: 'app-transferred',
    appName: '周报汇总助手',
    appDnsLabel: 'weekly-note-z9',
    requesterUserId: 'user-1',
    requesterProfile: {
      userId: 'user-1',
      displayName: '张三',
      teamNames: ['数据分析部'],
    },
    currentVisibility: { mode: 'private' },
    requestedVisibility: { mode: 'organization' },
    baseRouteGeneration: 1,
    policyVersion: 2,
    status: 'invalidated',
    invalidationReason: 'ownership_transferred',
    createdAt: '2026-11-20T09:00:00Z',
    updatedAt: '2026-11-21T01:00:00Z',
    decidedAt: '2026-11-21T01:00:00Z',
  },
]

const defaultSettings: AppHostingSettings = {
  enabled: true,
  appsDomain: 'apps.corp.example.com',
  publicScheme: 'https',
  storageIntegrationId: 'storage-minio-main',
  storagePrefix: 'app-hosting',
  gatewayTokenConfigured: true,
  allowOrganizationVisibility: true,
  organizationVisibilityPolicy: 'allow_direct',
  policyVersion: 3,
  maxBundleCompressedBytes: 50 * 1024 * 1024,
  maxBundleUncompressedBytes: 150 * 1024 * 1024,
  maxBundleFileCount: 2000,
  maxAppsPerOwner: 20,
  retainedRevisionsPerApp: 10,
  updatedBy: 'admin-self',
  updatedAt: '2026-11-20T12:00:00Z',
}

const roots: Root[] = []

beforeAll(() => {
  const getComputedStyle = window.getComputedStyle.bind(window)
  Object.defineProperty(window, 'getComputedStyle', {
    configurable: true,
    value: (element: Element) => getComputedStyle(element),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
})

beforeEach(() => {
  vi.clearAllMocks()
  tableState.propsList = []
  window.history.replaceState({}, '', '/internal-workbench/app-hosting')
  useAuthStore.setState({
    user: {
      userId: 'admin-self',
      userName: 'admin-self',
      displayName: '当前管理员',
      email: 'admin@example.com',
      roles: ['admin'],
      teams: ['sec'],
      projects: [],
      tags: null,
    },
    accessToken: 'test-token',
    sessionGeneration: 1,
  })
  authState.loading = false
  authState.permissions = ['apphosting.admin.view', 'apphosting.admin.manage', 'system.audit.view']

  apiMocks.get.mockImplementation((path: string) => {
    if (path === '/app-hosting/settings') {
      return Promise.resolve({ data: defaultSettings })
    }
    if (path.startsWith('/app-hosting/audience-candidates')) {
      return Promise.resolve({
        data: {
          users: [
            { id: 'user-1', username: 'zhangsan', displayName: '张三', department: '数据分析部' },
            { id: 'user-2', username: 'lisi', displayName: '李四', department: '研发平台部' },
          ],
          teams: [
            { id: 'team-mkt', name: '市场部', slug: 'marketing' },
            { id: 'team-rd', name: '研发平台部', slug: 'rd-platform' },
          ],
        },
      })
    }
    if (path.startsWith('/app-hosting/apps/app-online/revisions')) {
      return Promise.resolve({ data: { items: sampleRevisions, total: 1 } })
    }
    if (path === '/app-hosting/apps/app-online/visitor-attributes') {
      return Promise.resolve({ data: { granted: [] } })
    }
    if (path === '/app-hosting/apps/app-online') {
      return Promise.resolve({ data: onlineApp })
    }
    if (path.startsWith('/app-hosting/apps?')) {
      return Promise.resolve({ data: { items: [onlineApp, suspendedApp], total: 2 } })
    }
    if (path.startsWith('/app-hosting/approvals?')) {
      if (path.includes('status=invalidated')) {
        return Promise.resolve({ data: { items: invalidatedApprovals, total: 1 } })
      }
      return Promise.resolve({ data: { items: pendingApprovals, total: 2 } })
    }
    return Promise.resolve({ data: {} })
  })
  apiMocks.getEnvelope.mockImplementation((path: string) => {
    if (path.startsWith('/system-integrations')) {
      return Promise.resolve({
        items: [
          {
            id: 'storage-minio-main',
            category: 'storage',
            providerType: 's3',
            name: 'Corp MinIO',
            enabled: true,
            configuration: [],
            credentialKeys: [],
            healthStatus: 'healthy',
            version: 1,
            createdAt: '2026-11-01T00:00:00Z',
            updatedAt: '2026-11-01T00:00:00Z',
          },
        ],
      })
    }
    return Promise.resolve({ items: [] })
  })
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) {
      root.unmount()
    }
  })
  document.body.innerHTML = ''
})

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderPage(initialEntry = '/internal-workbench/app-hosting') {
  const url = new URL(initialEntry, 'https://console.corp.example.com')
  window.history.replaceState({}, '', `${url.pathname}${url.search}`)
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })

  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[initialEntry]}>
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <InternalWorkbenchAppHostingPage />
          </QueryClientProvider>
        </AntdApp>
      </MemoryRouter>,
    )
  })
  await flush()
  return { container, queryClient }
}

function setNativeValue(element: HTMLInputElement | HTMLTextAreaElement, value: string) {
  const prototype =
    element instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : HTMLInputElement.prototype
  const setter = Object.getOwnPropertyDescriptor(prototype, 'value')?.set
  setter?.call(element, value)
  element.dispatchEvent(new Event('input', { bubbles: true }))
  element.dispatchEvent(new Event('change', { bubbles: true }))
}

function findButtonByText(scope: ParentNode, text: string) {
  return Array.from(scope.querySelectorAll<HTMLButtonElement>('button')).find(
    (btn) => btn.textContent?.replace(/\s+/g, '') === text.replace(/\s+/g, ''),
  )
}

async function openRowActionMenu(scope: ParentNode, targetLabel: string) {
  const trigger = scope.querySelector<HTMLButtonElement>(
    `button[aria-label="更多操作：${targetLabel}"]`,
  )
  expect(trigger).toBeTruthy()
  await act(async () => {
    document.body.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
  })
  await flush()
  await act(async () => {
    trigger?.click()
  })
  await flush()
}

function findMenuItem(text: string) {
  const dropdowns = Array.from(
    document.body.querySelectorAll<HTMLElement>('.ant-dropdown:not(.ant-dropdown-hidden)'),
  )
  const activeDropdown = dropdowns[dropdowns.length - 1]
  if (!activeDropdown) return undefined
  return Array.from(activeDropdown.querySelectorAll<HTMLElement>('[role="menuitem"]')).find(
    (item) => item.textContent?.replace(/\s+/g, '') === text.replace(/\s+/g, ''),
  )
}

function isMenuItemDisabled(item: HTMLElement | undefined) {
  return (
    item?.getAttribute('aria-disabled') === 'true' ||
    item?.classList.contains('ant-dropdown-menu-item-disabled')
  )
}

async function clickRowAction(scope: ParentNode, targetLabel: string, actionLabel: string) {
  await openRowActionMenu(scope, targetLabel)
  const item = findMenuItem(actionLabel)
  expect(item).toBeTruthy()
  await act(async () => {
    item?.click()
  })
  await flush()
}

async function switchTab(container: HTMLElement, tabLabelFragment: string) {
  const tab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find((node) =>
    node.textContent?.includes(tabLabelFragment),
  )
  expect(tab).toBeTruthy()
  await act(async () => {
    tab?.click()
  })
  await flush()
}

async function selectDropdownOption(ariaLabel: string, optionTextFragment: string) {
  const select = document.body.querySelector<HTMLElement>(`[aria-label="${ariaLabel}"]`)
  expect(select).toBeTruthy()
  await act(async () => {
    select?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    select?.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }))
  })
  await flush()
  const option = Array.from(
    document.body.querySelectorAll<HTMLElement>('.ant-select-item-option'),
  ).find(
    (item) =>
      !item.closest('.ant-select-dropdown-hidden') &&
      item.textContent?.includes(optionTextFragment),
  )
  expect(option).toBeTruthy()
  await act(async () => {
    option?.click()
  })
  await flush()
}

describe('InternalWorkbenchAppHostingPage', () => {
  it('blocks access without apphosting.admin.view or apphosting.admin.manage', async () => {
    authState.permissions = ['identity.portal.view']
    const { container } = await renderPage()

    expect(container.textContent).toContain('无权限访问应用托管治理')
    expect(apiMocks.get).not.toHaveBeenCalled()
  })

  it('enforces read-only mode for apphosting.admin.view while keeping detail drawer and audit link accessible', async () => {
    authState.permissions = ['apphosting.admin.view', 'system.audit.view']
    const { container } = await renderPage()

    expect(container.textContent).toContain('当前为只读治理视角')
    expect(container.textContent).toContain('季度经营看板')
    expect(container.textContent).toContain('触发异常循环请求告警')
    expect(container.textContent).toContain('total:2')
    expect(tableState.propsList[0]?.rowKey).toBe('id')

    await openRowActionMenu(container, '季度经营看板')
    expect(isMenuItemDisabled(findMenuItem('详情'))).toBe(false)
    expect(isMenuItemDisabled(findMenuItem('强制封停'))).toBe(true)
    expect(isMenuItemDisabled(findMenuItem('转交'))).toBe(true)
    expect(isMenuItemDisabled(findMenuItem('删除'))).toBe(true)

    const detailItem = findMenuItem('详情')
    await act(async () => {
      detailItem?.click()
    })
    await flush()

    await openRowActionMenu(container, '异常轮询测试页')
    expect(isMenuItemDisabled(findMenuItem('解除封停'))).toBe(true)

    const drawer = document.body.querySelector('.ant-drawer')
    expect(drawer?.textContent).toContain('应用详情 · 季度经营看板')
    expect(drawer?.textContent).toContain('v2')
    const auditAnchor = Array.from(drawer?.querySelectorAll('a') ?? []).find((a) =>
      a.getAttribute('href')?.startsWith('/system/audit'),
    )
    expect(auditAnchor?.getAttribute('href')).toContain('metadataValue=app-online')

    await switchTab(container, '基础设施与策略设置')
    expect(apiMocks.getEnvelope).not.toHaveBeenCalled()
    const storageInput = container.querySelector<HTMLInputElement>(
      'input[aria-label="对象存储集成"]',
    )
    expect(storageInput).toBeTruthy()
    expect(storageInput?.disabled).toBe(true)
    expect(findButtonByText(container, '保存策略配置')?.disabled).toBe(true)
    expect(findButtonByText(container, '轮换网关令牌')?.disabled).toBe(true)
  })

  it('reviews a subset of visitor attributes for the selected app without reading environment configuration', async () => {
    const appPath = '/app-hosting/apps/app-online'
    const defaultGet = apiMocks.get.getMockImplementation()!
    const candidate = { ...onlineApp, kind: 'dynamic' }
    apiMocks.get.mockImplementation((path: string) => {
      if (path === appPath) return Promise.resolve({ data: candidate })
      if (path === `${appPath}/runtime-usage`) {
        return Promise.resolve({
          data: { status: 'unavailable', warningBytes: 200 * 1024 ** 2, warning: false },
        })
      }
      if (path === `${appPath}/visitor-attributes`) {
        return Promise.resolve({
          data: {
            granted: [],
            pending: {
              id: 'request-1',
              fields: ['email', 'department'],
              reason: '区分团队',
              requestedAt: onlineApp.updatedAt,
            },
          },
        })
      }
      return defaultGet(path)
    })
    apiMocks.put.mockResolvedValueOnce({ data: { granted: ['department'] } })

    const { container } = await renderPage()
    expect(apiMocks.get.mock.calls.some(([path]) => path.endsWith('/visitor-attributes'))).toBe(
      false,
    )
    await clickRowAction(container, '季度经营看板', '详情')
    const drawer = document.body.querySelector('.ant-drawer')!
    expect(drawer.textContent).toContain('暂时无法获取用量')
    expect(drawer.textContent).toContain('区分团队')
    expect(drawer.textContent).not.toContain('加密环境变量')
    expect(apiMocks.get.mock.calls.some(([path]) => path.endsWith('/runtime-config'))).toBe(false)
    const email = Array.from(drawer.querySelectorAll('label'))
      .find((label) => label.textContent === '邮箱')
      ?.querySelector('input')
    expect(email?.checked).toBe(true)
    await act(async () => email?.click())
    await act(async () => findButtonByText(drawer, '批准所选字段')?.click())
    await flush()
    expect(apiMocks.put).toHaveBeenCalledWith(`${appPath}/visitor-attributes`, {
      requestId: 'request-1',
      fields: ['department'],
    })
    expect(findButtonByText(drawer, '撤销授权')).toBeDefined()
  })

  it('filters global ledger and executes suspend and unsuspend with mandatory reason validation', async () => {
    apiMocks.post.mockResolvedValue({ data: { ...onlineApp, status: 'suspended_by_admin' } })
    const { container } = await renderPage()

    expect(apiMocks.get).toHaveBeenCalledWith('/app-hosting/apps?scope=all&limit=20&offset=0')
    expect(container.textContent).toContain('全公司待审')

    const keywordInput = container.querySelector<HTMLInputElement>(
      'input[aria-label="台账关键词"]',
    )!
    await act(async () => {
      setNativeValue(keywordInput, '看板')
    })
    await act(async () => {
      findButtonByText(container, '查询')?.click()
    })
    await flush()
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/apps?scope=all&query=%E7%9C%8B%E6%9D%BF&limit=20&offset=0',
    )
    await act(async () => {
      findButtonByText(container, '重置')?.click()
    })
    await flush()

    await clickRowAction(container, '季度经营看板', '强制封停')

    expect(document.body.textContent).toContain(
      '封停后立即切断所有网关访问，且作者无法自行恢复上线',
    )
    await act(async () => {
      findButtonByText(document.body, '确认强制封停')?.click()
    })
    await flush()
    expect(document.body.textContent).toContain('请填写强制封停原因')
    expect(apiMocks.post).not.toHaveBeenCalled()

    const suspendReasonInput = document.body.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="封停原因"]',
    )!
    await act(async () => {
      setNativeValue(suspendReasonInput, '  违规外发测试密钥  ')
    })
    await act(async () => {
      findButtonByText(document.body, '确认强制封停')?.click()
    })
    await flush()
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/apps/app-online/suspend', {
      reason: '违规外发测试密钥',
    })

    apiMocks.post.mockClear()
    apiMocks.post.mockResolvedValueOnce({ data: { ...suspendedApp, status: 'offline' } })
    await clickRowAction(container, '异常轮询测试页', '解除封停')
    await act(async () => {
      findButtonByText(document.body, '确认解除封停')?.click()
    })
    await flush()
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/apps/app-suspended/unsuspend', {})
  })

  it('transfers app ownership (excluding current owner) and requires exact app name match before permanent deletion', async () => {
    const { container } = await renderPage()

    apiMocks.post.mockResolvedValueOnce({ data: { ...onlineApp, ownerUserId: 'user-2' } })
    await clickRowAction(container, '季度经营看板', '转交')

    expect(document.body.textContent).toContain(
      '固定链接保持不变，原作者私有访问权与待审申请将立即重算/失效',
    )
    await selectDropdownOption('新归属员工', '李四')

    const transferReasonInput = document.body.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="转交原因"]',
    )!
    await act(async () => {
      setNativeValue(transferReasonInput, '员工调岗交接')
    })
    await act(async () => {
      findButtonByText(document.body, '确认转交归属')?.click()
    })
    await flush()
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/apps/app-online/transfer', {
      newOwnerUserId: 'user-2',
      reason: '员工调岗交接',
    })

    apiMocks.post.mockClear()
    apiMocks.post.mockResolvedValueOnce({
      data: { appId: 'app-online', deleted: true },
    })
    await clickRowAction(container, '季度经营看板', '删除')

    expect(findButtonByText(document.body, '确认彻底删除')?.disabled).toBe(true)

    const confirmNameInput = document.body.querySelector<HTMLInputElement>(
      'input[aria-label="确认应用名称"]',
    )!
    await act(async () => {
      setNativeValue(confirmNameInput, '季度经营看板-wrong')
    })
    expect(findButtonByText(document.body, '确认彻底删除')?.disabled).toBe(true)

    await act(async () => {
      setNativeValue(confirmNameInput, '季度经营看板')
    })
    expect(findButtonByText(document.body, '确认彻底删除')?.disabled).toBe(false)

    await act(async () => {
      findButtonByText(document.body, '确认彻底删除')?.click()
    })
    await flush()
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/apps/app-online/delete', {
      confirmationName: '季度经营看板',
    })
  })

  it('prevents self-approval on own request and supports approving/rejecting colleague requests plus viewing invalidated reasons', async () => {
    const { container } = await renderPage('/internal-workbench/app-hosting?tab=approvals')

    expect(apiMocks.get).toHaveBeenCalledWith(
      '/app-hosting/approvals?scope=all&status=pending&limit=20&offset=0',
    )
    expect(container.textContent).toContain('申请人本人需回避审批，请由其他管理员处理')

    const rows = Array.from(container.querySelectorAll<HTMLTableRowElement>('tbody tr'))
    const selfRow = rows.find((row) => row.textContent?.includes('安全值班速查表'))!
    const otherRow = rows.find((row) => row.textContent?.includes('季度经营看板'))!

    await openRowActionMenu(selfRow, '安全值班速查表')
    expect(isMenuItemDisabled(findMenuItem('批准'))).toBe(true)
    expect(isMenuItemDisabled(findMenuItem('拒绝'))).toBe(true)

    await openRowActionMenu(otherRow, '季度经营看板')
    expect(isMenuItemDisabled(findMenuItem('批准'))).toBe(false)
    expect(isMenuItemDisabled(findMenuItem('拒绝'))).toBe(false)

    apiMocks.post.mockResolvedValueOnce({
      data: {
        approval: { ...pendingApprovals[1], status: 'approved' },
        app: { ...onlineApp, visibility: { mode: 'organization' } },
      },
    })
    const approveItem = findMenuItem('批准')
    await act(async () => {
      approveItem?.click()
    })
    await flush()

    const approveComment = document.body.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="审批备注"]',
    )!
    await act(async () => {
      setNativeValue(approveComment, '同意面向全公司开放')
    })
    await act(async () => {
      findButtonByText(document.body, '确认批准')?.click()
    })
    await flush()
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/approvals/apr-other/approve', {
      comment: '同意面向全公司开放',
    })

    apiMocks.post.mockClear()
    apiMocks.post.mockResolvedValueOnce({
      data: {
        approval: { ...pendingApprovals[1], status: 'rejected' },
        app: onlineApp,
      },
    })
    const refreshedOtherRow = Array.from(
      container.querySelectorAll<HTMLTableRowElement>('tbody tr'),
    ).find((row) => row.textContent?.includes('季度经营看板'))!
    await clickRowAction(refreshedOtherRow, '季度经营看板', '拒绝')

    await act(async () => {
      findButtonByText(document.body, '确认拒绝')?.click()
    })
    await flush()
    expect(document.body.textContent).toContain('请填写拒绝原因')
    expect(apiMocks.post).not.toHaveBeenCalled()

    const rejectComment = document.body.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="拒绝原因"]',
    )!
    await act(async () => {
      setNativeValue(rejectComment, '包含内部预算明细，请限定在市场与财务部门')
    })
    await act(async () => {
      findButtonByText(document.body, '确认拒绝')?.click()
    })
    await flush()
    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/approvals/apr-other/reject', {
      comment: '包含内部预算明细，请限定在市场与财务部门',
    })

    const invalidatedScopeItem = Array.from(
      container.querySelectorAll<HTMLElement>('.ant-segmented-item'),
    ).find((item) => item.textContent?.trim() === '已失效')!
    await act(async () => {
      invalidatedScopeItem.click()
    })
    await flush()

    expect(container.textContent).toContain('周报汇总助手')
    expect(container.textContent).toContain('失效原因：归属已变更')
  })

  it('saves governance policy settings (with graceful storageIntegrationId input fallback) and wipes rotated gateway token on modal close', async () => {
    authState.permissions = ['apphosting.admin.view', 'apphosting.admin.manage']
    const { container } = await renderPage('/internal-workbench/app-hosting?tab=settings')

    expect(apiMocks.getEnvelope).not.toHaveBeenCalled()
    const domainInput = container.querySelector<HTMLInputElement>('input[aria-label="泛域名后缀"]')!
    const storageInput = container.querySelector<HTMLInputElement>(
      'input[aria-label="对象存储集成"]',
    )!
    expect(domainInput.value).toBe('apps.corp.example.com')
    expect(storageInput.value).toBe('storage-minio-main')

    await act(async () => {
      setNativeValue(domainInput, 'apps.internal.example.org')
      setNativeValue(storageInput, 'storage-minio-dedicated')
    })

    await selectDropdownOption('全公司公开策略', '须管理员审批后公开')

    expect(container.textContent).toContain(
      '即将变更全公司公开策略：现有待审申请将在保存后自动失效',
    )

    apiMocks.put.mockResolvedValueOnce({
      data: {
        ...defaultSettings,
        appsDomain: 'apps.internal.example.org',
        storageIntegrationId: 'storage-minio-dedicated',
        organizationVisibilityPolicy: 'require_approval',
        policyVersion: 4,
      },
    })

    await act(async () => {
      findButtonByText(container, '保存策略配置')?.click()
    })
    await flush()

    expect(apiMocks.put).toHaveBeenCalledWith(
      '/app-hosting/settings',
      expect.objectContaining({
        enabled: true,
        appsDomain: 'apps.internal.example.org',
        publicScheme: 'https',
        storageIntegrationId: 'storage-minio-dedicated',
        storagePrefix: 'app-hosting',
        organizationVisibilityPolicy: 'require_approval',
        allowOrganizationVisibility: true,
        maxBundleCompressedBytes: 50 * 1024 * 1024,
        maxBundleUncompressedBytes: 150 * 1024 * 1024,
        maxBundleFileCount: 2000,
        maxAppsPerOwner: 20,
        retainedRevisionsPerApp: 10,
      }),
    )

    apiMocks.post.mockResolvedValueOnce({
      data: {
        token: 'soha_agw_one_time_secret_token_999',
        rotatedAt: '2026-11-21T04:10:00Z',
      },
    })

    await act(async () => {
      findButtonByText(container, '轮换网关令牌')?.click()
    })
    await flush()

    const rotateReasonInput = document.body.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="轮换原因"]',
    )!
    await act(async () => {
      setNativeValue(rotateReasonInput, '季度例行轮换')
    })
    await act(async () => {
      findButtonByText(document.body, '确认轮换令牌')?.click()
    })
    await flush()

    expect(apiMocks.post).toHaveBeenCalledWith('/app-hosting/settings/gateway-token/rotate', {
      reason: '季度例行轮换',
    })
    expect(
      document.body.querySelector('[data-testid="rotated-gateway-token-value"]')?.textContent,
    ).toBe('soha_agw_one_time_secret_token_999')

    await act(async () => {
      findButtonByText(document.body, '我已保存并关闭')?.click()
    })
    await flush()

    expect(document.body.textContent).not.toContain('soha_agw_one_time_secret_token_999')
  })
})
