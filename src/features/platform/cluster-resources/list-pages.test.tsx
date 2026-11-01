/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act, forwardRef, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ClusterNamespacesPage } from './namespaces-list-page'
import { ClusterNodesPage } from './nodes-list-page'

const testState = vi.hoisted(() => ({
  permissions: [] as string[],
  responses: {} as Record<string, unknown>,
  scope: {
    clusterId: null as string | null,
    namespace: null as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(async (path: string) => ({ data: testState.responses[path] ?? [] })),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))
vi.mock('@/features/copilot', () => ({ useAIPageContext: vi.fn() }))
vi.mock('@/features/auth', () => ({
  hasAllowedAction: (actions: string[] | undefined, action: string) =>
    actions?.includes(action) ?? false,
  hasPermission: (_snapshot: unknown, permission: string) =>
    testState.permissions.includes(permission),
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))
vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))
vi.mock('@/features/platform/node-resource-utils', () => ({
  NodeResourcePanel: () => <div>node-resource-panel</div>,
  parseStringMap: () => ({}),
  parseTaints: () => [],
  stringifyMap: () => '{}',
  stringifyTaints: () => '[]',
}))
vi.mock('@/components/status-tag', () => ({
  MetadataTag: ({ label, tone }: { label: ReactNode; tone?: string }) => (
    <span data-tone={tone}>{label}</span>
  ),
  StatusTag: ({ value, label }: { value?: string; label?: string }) => (
    <span>{label || value}</span>
  ),
}))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns = [],
    dataSource = [],
    headerExtra,
    tableSize,
  }: {
    columns?: Array<{
      dataIndex?: string
      key?: string
      render?: (value: unknown, record: Record<string, unknown>) => ReactNode
    }>
    dataSource?: Array<Record<string, unknown>>
    headerExtra?: ReactNode
    tableSize?: string
  }) => (
    <div data-count={dataSource.length} data-table-size={tableSize} data-testid="admin-table">
      {headerExtra}
      {dataSource.length}
      {dataSource.flatMap((record) =>
        columns.map((column, index) => (
          <div key={`${String(record.name)}-${column.key ?? column.dataIndex}-${index}`}>
            {column.render
              ? column.render(record[column.dataIndex ?? ''], record)
              : (record[column.dataIndex ?? ''] as ReactNode)}
          </div>
        )),
      )}
    </div>
  ),
}))
vi.mock('@/components/management-list', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/components/management-list')>()),
  ManagementDensityButton: ({
    'aria-label': ariaLabel,
    onClick,
  }: {
    'aria-label': string
    onClick?: () => void
  }) => (
    <button aria-label={ariaLabel} onClick={onClick}>
      {ariaLabel}
    </button>
  ),
  ManagementDetailHeader: ({ title }: { title?: ReactNode }) => <h1>{title}</h1>,
  ManagementIconButton: forwardRef<
    HTMLButtonElement,
    { 'aria-label': string; onClick?: () => void }
  >(({ 'aria-label': ariaLabel, onClick }, ref) => (
    <button ref={ref} aria-label={ariaLabel} onClick={onClick} />
  )),
  ManagementRefreshButton: ({ 'aria-label': ariaLabel }: { 'aria-label': string }) => (
    <button aria-label={ariaLabel}>{ariaLabel}</button>
  ),
  ManagementState: ({ title }: { title?: ReactNode }) => <div>{title}</div>,
  ManagementTableToolbar: ({ children }: { children?: ReactNode }) => <>{children}</>,
}))

const mountedRoots: Root[] = []

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
})

beforeEach(() => {
  vi.clearAllMocks()
  testState.permissions = []
  testState.responses = {}
  testState.scope.clusterId = null
  testState.scope.namespace = null
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function renderPage(page: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>{page}</MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
  return container
}

describe('cluster resource list pages', () => {
  it('keeps both list queries disabled until a cluster is selected', async () => {
    const nodes = await renderPage(<ClusterNodesPage />)
    expect(nodes.textContent).toContain('Please select a cluster')
    expect(apiMocks.get).not.toHaveBeenCalled()

    const namespaces = await renderPage(<ClusterNamespacesPage />)
    expect(namespaces.textContent).toContain('Select a cluster')
    expect(apiMocks.get).not.toHaveBeenCalled()
  })

  it('renders unwrapped node and namespace data from canonical cluster paths', async () => {
    testState.scope.clusterId = 'cluster-a'
    testState.responses['/clusters/cluster-a/infrastructure/nodes'] = [{ name: 'node-a' }]
    testState.responses['/clusters/cluster-a/namespaces'] = [{ name: 'team-a' }]

    const nodes = await renderPage(<ClusterNodesPage />)
    const namespaces = await renderPage(<ClusterNamespacesPage />)

    expect(namespaces.querySelector('h1')).toBeNull()
    expect(nodes.querySelector('[data-testid="admin-table"]')?.getAttribute('data-count')).toBe('1')
    expect(
      namespaces.querySelector('[data-testid="admin-table"]')?.getAttribute('data-count'),
    ).toBe('1')
    expect(apiMocks.get).toHaveBeenCalledWith('/clusters/cluster-a/infrastructure/nodes')
    expect(apiMocks.get).toHaveBeenCalledWith('/clusters/cluster-a/namespaces')
  })

  it('shows Ready independently of cordon, pressure and actual taints', async () => {
    testState.scope.clusterId = 'cluster-a'
    testState.responses['/clusters/cluster-a/infrastructure/nodes'] = [
      {
        name: 'maintenance-node',
        status: 'ready',
        unschedulable: true,
        taints: [{ key: 'maintenance', value: 'task113', effect: 'NoSchedule' }],
        conditions: [
          { type: 'Ready', status: 'True' },
          { type: 'DiskPressure', status: 'True' },
        ],
      },
      { name: 'legacy-node', status: 'unknown', unschedulable: false },
    ]
    const nodes = await renderPage(<ClusterNodesPage />)
    expect(nodes.textContent).toContain('ready')
    expect(nodes.textContent).toContain('禁止调度')
    expect(nodes.textContent).toContain('磁盘压力')
    expect(nodes.querySelector('.soha-node-taints')?.textContent).toContain('maintenance=task113')
    expect(nodes.querySelector('[data-tone="purple"]')).not.toBeNull()
    expect(nodes.textContent).toContain('未提供')
    expect(nodes.textContent).not.toContain('排水中')
    await act(async () => {
      ;(
        nodes.querySelector(
          '[aria-label="查看污点 maintenance=task113:NoSchedule"]',
        ) as HTMLButtonElement
      ).click()
    })
    expect(document.body.textContent).toContain('maintenance=task113:NoSchedule')
  })

  it('provides density and refresh through the shared namespace table toolbar', async () => {
    testState.scope.clusterId = 'cluster-a'
    const namespaces = await renderPage(<ClusterNamespacesPage />)

    expect(namespaces.querySelector('[aria-label="切换表格密度"]')).not.toBeNull()
    expect(namespaces.querySelector('[aria-label="刷新"]')).not.toBeNull()
    expect(namespaces.querySelector('[data-table-size="small"]')).not.toBeNull()

    await act(async () => {
      ;(namespaces.querySelector('[aria-label="切换表格密度"]') as HTMLButtonElement).click()
    })
    expect(namespaces.querySelector('[data-table-size="middle"]')).not.toBeNull()
  })

  it('renders node and namespace mutations only when their permissions are allowed', async () => {
    testState.scope.clusterId = 'cluster-a'
    testState.responses['/clusters/cluster-a/infrastructure/nodes'] = [
      { name: 'node-a', allowedActions: ['view'] },
    ]
    testState.responses['/clusters/cluster-a/namespaces'] = [
      { name: 'team-a', allowedActions: ['view'] },
    ]

    const readonlyNodes = await renderPage(<ClusterNodesPage />)
    const readonlyNamespaces = await renderPage(<ClusterNamespacesPage />)
    async function menu(container: HTMLElement, name: string, resource: string) {
      await act(async () =>
        container.querySelector<HTMLButtonElement>(`[aria-label="更多操作：${name}"]`)!.click(),
      )
      return document.querySelector(`[role="menu"][aria-label="${resource} 操作：${name}"]`)!
    }
    expect((await menu(readonlyNodes, 'node-a', '节点')).textContent).toBe('详情')
    expect((await menu(readonlyNamespaces, 'team-a', '命名空间')).textContent).toBe('查看资源Helm')
    expect(readonlyNamespaces.textContent).not.toContain('Create')

    testState.permissions = ['platform.namespaces.create']
    testState.responses['/clusters/cluster-a/infrastructure/nodes'] = [
      { name: 'node-b', allowedActions: ['view', 'update', 'drain', 'delete'] },
    ]
    testState.responses['/clusters/cluster-a/namespaces'] = [
      { name: 'team-b', allowedActions: ['view', 'update', 'delete'] },
    ]
    const writableNodes = await renderPage(<ClusterNodesPage />)
    const writableNamespaces = await renderPage(<ClusterNamespacesPage />)
    const nodeMenu = await menu(writableNodes, 'node-b', '节点')
    expect(nodeMenu.textContent).toContain('编辑节点')
    expect(nodeMenu.textContent).toContain('排空节点')
    expect(nodeMenu.textContent).toContain('删除')
    const namespaceMenu = await menu(writableNamespaces, 'team-b', '命名空间')
    expect(namespaceMenu.textContent).toContain('编辑')
    expect(namespaceMenu.textContent).toContain('删除')
    await act(async () => {
      ;[...namespaceMenu.querySelectorAll<HTMLElement>('[role="menuitem"]')]
        .find((item) => item.textContent === '删除')!
        .click()
    })
    expect(document.querySelector('[role="dialog"]')?.textContent).toContain(
      '该命名空间下的资源会一并回收',
    )
    expect(apiMocks.delete).not.toHaveBeenCalled()
    await act(async () =>
      document.querySelector<HTMLButtonElement>('.ant-modal-footer .ant-btn-default')!.click(),
    )
    expect(apiMocks.delete).not.toHaveBeenCalled()
    expect(writableNamespaces.textContent).toContain('Create')
  })
})
