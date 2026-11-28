/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act, forwardRef, isValidElement, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ClusterNamespacesPage } from './namespaces-list-page'
import { ClusterNodesPage } from './nodes-list-page'
import { clusterResourceKeys } from './keys'
import { toClusterScope } from './scope'
import { monitoringKeys } from '../resource-monitoring/keys'

const testState = vi.hoisted(() => ({
  t: (_key: string, fallback?: string) => fallback ?? _key,
  permissions: [] as string[],
  responses: {} as Record<string, unknown>,
  scope: {
    clusterId: null as string | null,
    namespace: null as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))

const tablePropsMock = vi.hoisted(() => vi.fn())
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
    t: testState.t,
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
    ...bodyProps
  }: {
    columns?: Array<{
      dataIndex?: string
      key?: string
      render?: (value: unknown, record: Record<string, unknown>) => ReactNode
    }>
    dataSource?: Array<Record<string, unknown>>
    headerExtra?: ReactNode
    tableSize?: string
    [key: string]: unknown
  }) => {
    tablePropsMock({ columns, dataSource, headerExtra, tableSize, ...bodyProps })
    return (
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
    )
  },
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
  ManagementRefreshButton: ({
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
  ManagementState: ({ title }: { title?: ReactNode }) => <div>{title}</div>,
  ManagementTableToolbar: ({ children }: { children?: ReactNode }) => <>{children}</>,
}))

const mountedRoots: Root[] = []

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
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
  vi.useRealTimers()
})

let currentQueryClient: QueryClient
let renderCurrentPage: (page: ReactNode) => Promise<void>
async function flushQueries() {
  await act(async () => {
    if (vi.isFakeTimers()) await vi.advanceTimersByTimeAsync(1)
    else await new Promise((resolve) => setTimeout(resolve, 5))
  })
}

async function renderPage(page: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })

  renderCurrentPage = async (page: ReactNode) =>
    act(async () => {
      root.render(
        <AntdApp>
          <QueryClientProvider client={queryClient}>
            <MemoryRouter>{page}</MemoryRouter>
          </QueryClientProvider>
        </AntdApp>,
      )
    })
  currentQueryClient = queryClient
  await renderCurrentPage(page)
  await act(async () => {
    await Promise.resolve()
    if (vi.isFakeTimers()) await vi.advanceTimersByTimeAsync(1)
    else await new Promise((resolve) => setTimeout(resolve, 0))
  })
  return container
}

describe('cluster resource list pages', () => {
  it.each([
    ['nodes', ClusterNodesPage],
    ['namespaces', ClusterNamespacesPage],
  ])(
    'keeps %s body props stable on equal refreshes and binds updated permissions and scope',
    async (kind, Page) => {
      testState.scope.clusterId = 'cluster-a'
      const path =
        kind === 'nodes'
          ? '/clusters/cluster-a/infrastructure/nodes'
          : '/clusters/cluster-a/namespaces'
      const key =
        kind === 'nodes'
          ? clusterResourceKeys.nodeList(toClusterScope('cluster-a'))
          : clusterResourceKeys.namespaceList(toClusterScope('cluster-a'))
      const record = {
        name: 'resource-a',
        status: 'ready',
        unschedulable: false,
        roles: [],
        labels: { owner: 'team-a' },
        taints: [],
        ageSeconds: 60,
        podCount: 1,
        allowedActions: ['update', 'drain', 'delete'],
      }
      testState.responses[path] = [record]
      const container = await renderPage(<Page />)
      await flushQueries()
      const before = tablePropsMock.mock.calls.at(-1)![0]
      expect(before.dataSource).toHaveLength(1)
      if (kind === 'nodes') {
        expect(before.expandable).toBeUndefined()
        const link = container.querySelector('a.soha-node-name')!
        expect(link.getAttribute('href')).toContain('clusterId=cluster-a')
        expect(link.querySelector('.soha-table-cell-text')?.textContent).toBe('resource-a')
      }
      let complete!: (response: { data: unknown[] }) => void
      apiMocks.get.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            complete = resolve
          }),
      )
      await act(async () => {
        void currentQueryClient.invalidateQueries({ queryKey: key })
      })
      await flushQueries()
      const pending = tablePropsMock.mock.calls.at(-1)![0]
      for (const prop of ['columns', 'onRow', 'rowKey', 'empty', 'dataSource', 'expandable'])
        expect(pending[prop], prop).toBe(before[prop])
      await act(async () => complete({ data: [{ ...record, labels: { ...record.labels } }] }))
      await flushQueries()
      const same = tablePropsMock.mock.calls.at(-1)![0]
      expect(same.columns).toBe(before.columns)
      expect(same.dataSource).toBe(before.dataSource)
      await act(async () => currentQueryClient.setQueryData(key, [{ ...record, ageSeconds: 120 }]))
      await flushQueries()
      const changed = tablePropsMock.mock.calls.at(-1)![0]
      expect(changed.dataSource[0].ageSeconds).toBe(120)
      expect(changed.columns).toBe(before.columns)
      const action = before.columns.find((column: { key?: string }) => column.key === 'actions')
      const oldMenu = action.render(undefined, before.dataSource[0], 0)
      expect(isValidElement(oldMenu)).toBe(true)
      await act(async () =>
        currentQueryClient.setQueryData(key, [{ ...record, allowedActions: [] }]),
      )
      await flushQueries()
      if (!isValidElement<{ onSelect: (key: string) => void }>(oldMenu))
        throw new Error('missing menu')
      await act(async () => oldMenu.props.onSelect('delete'))
      expect(document.querySelector('[role="dialog"]')).toBeNull()
      expect(apiMocks.delete).not.toHaveBeenCalled()
      const nextPath = path.replace('cluster-a', 'cluster-b')
      testState.responses[nextPath] = [{ ...record, name: 'resource-b' }]
      testState.scope.clusterId = 'cluster-b'
      await renderCurrentPage(<Page />)
      await flushQueries()
      const nextScope = tablePropsMock.mock.calls.at(-1)![0]
      expect(nextScope.dataSource[0].name).toBe('resource-b')
      expect(nextScope.columns).not.toBe(before.columns)
      expect(apiMocks.get).toHaveBeenCalledWith(nextPath)
      if (kind === 'nodes')
        expect(container.querySelector('a.soha-node-name')?.getAttribute('href')).toContain(
          'clusterId=cluster-b',
        )
    },
  )

  it('keeps node fallback scoped, stops retrying errors and allows manual retry', async () => {
    vi.useFakeTimers()
    const visible = vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    try {
      testState.scope.clusterId = 'cluster-a'
      const path = '/clusters/cluster-a/infrastructure/nodes'
      testState.responses[path] = [{ name: 'node-a' }]
      const container = await renderPage(<ClusterNodesPage />)
      const invalidations = vi.spyOn(currentQueryClient, 'invalidateQueries')
      const queryKey = clusterResourceKeys.nodeList(toClusterScope('cluster-a'))
      await act(async () => vi.advanceTimersByTimeAsync(15_000))
      expect(invalidations).toHaveBeenCalledWith({ queryKey })
      apiMocks.get.mockRejectedValueOnce(new Error('nodes unavailable'))
      await act(async () =>
        container.querySelector<HTMLButtonElement>('[aria-label="刷新节点"]')!.click(),
      )
      await flushQueries()
      expect(currentQueryClient.getQueryState(queryKey)?.status).toBe('error')
      const reads = apiMocks.get.mock.calls.filter(([url]) => url === path).length
      invalidations.mockClear()
      await act(async () => vi.advanceTimersByTimeAsync(30_000))
      expect(invalidations).not.toHaveBeenCalled()
      expect(apiMocks.get.mock.calls.filter(([url]) => url === path)).toHaveLength(reads)
      await act(async () =>
        container.querySelector<HTMLButtonElement>('[aria-label="刷新节点"]')!.click(),
      )
      await flushQueries()
      expect(apiMocks.get.mock.calls.filter(([url]) => url === path)).toHaveLength(reads + 1)
      expect(currentQueryClient.getQueryState(queryKey)?.status).toBe('success')
    } finally {
      visible.mockRestore()
    }
  })

  it('updates node monitoring columns and sorters on metrics and permission changes', async () => {
    testState.scope.clusterId = 'cluster-a'
    testState.permissions = ['platform.nodes.view', 'platform.resource-monitoring.overview.view']
    testState.responses['/clusters/cluster-a/infrastructure/nodes'] = [
      { name: 'node-a' },
      { name: 'node-b' },
    ]
    const nodeRef = { clusterId: 'cluster-a', name: 'node-a' }
    const metric = {
      clusterId: 'cluster-a',
      nodes: [{ nodeRef, quality: 'valid', capacity: { cpuCores: 4 } }],
      panels: [
        {
          key: 'host.cpu.used',
          quality: 'valid',
          series: [{ nodeRef, quality: 'valid', latest: 1 }],
        },
      ],
    }
    testState.responses[
      '/clusters/cluster-a/resource-monitoring?view=overview&rangeMinutes=15&stepSeconds=60'
    ] = metric
    await renderPage(<ClusterNodesPage />)
    await flushQueries()
    const before = tablePropsMock.mock.calls.at(-1)![0]
    const cpu = (props: typeof before) =>
      props.columns.find((column: { key?: string }) => column.key === 'hostCpu')
    expect(cpu(before).render(undefined, { name: 'node-a' }).props.usage.value).toBe(1)
    const monitoringKey = monitoringKeys.view('', '@undefined', 'cluster-a', {
      view: 'overview',
      rangeMinutes: 15,
      stepSeconds: 60,
    })
    await act(async () =>
      currentQueryClient.setQueryData(monitoringKey, {
        ...metric,
        panels: [{ ...metric.panels[0], series: [{ ...metric.panels[0].series[0], latest: 2 }] }],
      }),
    )
    await flushQueries()
    const updated = tablePropsMock.mock.calls.at(-1)![0]
    expect(updated.columns).not.toBe(before.columns)
    expect(cpu(updated).render(undefined, { name: 'node-a' }).props.usage.value).toBe(2)
    expect(cpu(updated).sorter({ name: 'node-a' }, { name: 'node-b' })).toBeGreaterThan(0)
    testState.permissions = ['platform.nodes.view']
    await renderCurrentPage(<ClusterNodesPage />)
    await flushQueries()
    const denied = tablePropsMock.mock.calls.at(-1)![0]
    expect(denied.columns).not.toBe(updated.columns)
    const cell = cpu(denied).render(undefined, { name: 'node-a' })
    expect(cell.props.canRead).toBe(false)
    expect(cell.props.usage.value).toBeNull()
  })

  it('refreshes namespace resources and Pod counts independently and removes counts without permission', async () => {
    testState.scope.clusterId = 'cluster-a'
    testState.permissions = ['platform.namespaces.view', 'platform.pods.view']
    const namespacePath = '/clusters/cluster-a/namespaces'
    const podPath = '/clusters/cluster-a/workloads/pods'
    testState.responses[namespacePath] = [{ name: 'team-a', status: 'Active', labels: {} }]
    testState.responses[podPath] = [
      { name: 'pod-a', namespace: 'team-a', phase: 'Running', readyContainers: '1/1' },
    ]
    const container = await renderPage(<ClusterNamespacesPage />)
    await flushQueries()
    const before = tablePropsMock.mock.calls.at(-1)![0]
    expect(before.dataSource[0]).toMatchObject({ podCount: 1, attentionCount: 0 })
    testState.responses[podPath] = [
      { name: 'pod-a', namespace: 'team-a', phase: 'Failed', readyContainers: '0/1' },
    ]
    await act(async () =>
      container.querySelector<HTMLButtonElement>('[aria-label="刷新"]')!.click(),
    )
    await flushQueries()
    const updated = tablePropsMock.mock.calls.at(-1)![0]
    expect(updated.dataSource[0]).toMatchObject({ podCount: 1, attentionCount: 1 })
    expect(updated.columns).toBe(before.columns)
    expect(apiMocks.get.mock.calls.filter(([path]) => path === namespacePath)).toHaveLength(2)
    expect(apiMocks.get.mock.calls.filter(([path]) => path === podPath)).toHaveLength(2)
    testState.permissions = ['platform.namespaces.view']
    await renderCurrentPage(<ClusterNamespacesPage />)
    await flushQueries()
    const denied = tablePropsMock.mock.calls.at(-1)![0]
    expect(denied.dataSource[0].podCount).toBeUndefined()
    const count = denied.columns.find(
      (column: { dataIndex?: string }) => column.dataIndex === 'podCount',
    )
    expect(count.onHeaderCell().title).toBe('需要命名空间与 Pod 查看权限')
    expect(count.render(undefined, denied.dataSource[0]).props.title).toBe(
      '需要命名空间与 Pod 查看权限',
    )
  })

  it('keeps both list queries disabled until a cluster is selected', async () => {
    const nodes = await renderPage(<ClusterNodesPage />)
    expect(nodes.textContent).toContain('请选择集群')
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
      ;(nodes.querySelector('[aria-label="查看节点污点"]') as HTMLButtonElement).click()
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
