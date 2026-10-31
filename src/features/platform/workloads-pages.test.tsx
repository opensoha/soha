/** @vitest-environment jsdom */

import { PodLogViewer } from '@/components/pod-log-viewer'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import type { Key, ReactNode } from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { WorkloadsDaemonSetsPage } from './workloads/daemonsets/list-page'
import { WorkloadsDeploymentsPage } from './workloads/deployments/list-page'
import { PodDetailPage } from './workloads/pods/detail-page'
import { WorkloadsPodsPage } from './workloads/pods/list-page'
import { WorkloadsStatefulSetsPage } from './workloads/statefulsets/list-page'

const testState = vi.hoisted(() => ({
  permissionKeys: [
    'platform.pods.delete',
    'platform.pods.exec',
    'platform.pods.logs',
    'platform.pods.update',
    'platform.pods.view',
  ],
  responses: {} as Record<string, unknown>,
  scope: {
    clusterId: 'cluster-a' as string | null,
    namespace: 'monitoring' as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn((path: string) => Promise.resolve({ data: testState.responses[path] ?? [] })),
)
const apiDeleteMock = vi.hoisted(() => vi.fn(() => Promise.resolve({ data: null })))
const apiPostMock = vi.hoisted(() => vi.fn(() => Promise.resolve({ data: null })))
const withStreamTicketMock = vi.hoisted(() => vi.fn(async (url: string) => url))
const openSessionMock = vi.hoisted(() => vi.fn())

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: apiGetMock,
    delete: apiDeleteMock,
    post: apiPostMock,
    put: vi.fn(),
  },
}))

vi.mock('@/features/auth/stream-ticket', () => ({
  buildSameOriginStreamURL: (path: string, protocol: string) =>
    new URL(path, `${protocol}://localhost`).toString(),
  withStreamTicket: withStreamTicketMock,
}))

vi.mock('@/components/platform-scope-toolbar', () => ({
  PlatformScopeToolbar: () => <div data-testid="scope-toolbar">scope-toolbar</div>,
}))

vi.mock('@/components/resource-events-timeline', () => ({
  ResourceEventsTimeline: () => (
    <div data-testid="resource-events-timeline">resource-events-timeline</div>
  ),
}))

vi.mock('@/components/status-tag', () => ({
  BooleanTag: ({ value }: { value: boolean }) => <span>{String(value)}</span>,
  StatusTag: ({ value }: { value: string }) => <span>{value}</span>,
}))

vi.mock('@/components/resource-metrics-panel', () => ({
  ResourceMetricsPanel: () => (
    <div data-testid="resource-metrics-panel">resource-metrics-panel</div>
  ),
}))

vi.mock('@/features/platform/node-resource-utils', () => ({
  ResourceProgressCell: () => (
    <div data-testid="resource-progress-cell">resource-progress-cell</div>
  ),
  formatBytesAsG: () => '-',
  formatCpu: () => '-',
}))

vi.mock('@/features/platform/session-dock', () => ({
  useRealtimeSessionDock: () => ({ openSession: openSessionMock }),
}))

vi.mock('@/components/stat-grid', () => ({
  StatGrid: () => <div data-testid="stat-grid">stat-grid</div>,
}))

vi.mock('@/components/resource-actions', () => ({
  TABLE_ACTIONS_COLUMN_CLASS_NAME: 'soha-actions',
  useResourceActions: () => ({
    column: { title: '', dataIndex: '__actions__', render: () => null },
    modalNode: null,
  }),
}))

vi.mock('@/features/auth/permission-snapshot', () => ({
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: testState.permissionKeys,
        visibleMenuIds: [],
      },
    },
    isLoading: false,
  }),
  hasPermission: (snapshot: { permissionKeys: string[] } | undefined, permissionKey: string) =>
    snapshot?.permissionKeys.includes(permissionKey) ?? false,
  hasAllowedAction: (allowedActions: string[] | undefined, action: string) =>
    allowedActions?.includes(action) ?? false,
}))

vi.mock('@/utils/download', () => ({
  downloadJSON: vi.fn(),
  downloadText: vi.fn(),
}))

type MockRecord = Record<string, unknown>
type MockColumn = {
  dataIndex?: string | string[]
  key?: Key
  render?: (value: unknown, record: MockRecord, index: number) => ReactNode
  title?: ReactNode
}

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    headerExtra,
    loading,
    rowKey,
    rowSelection,
    title,
    toolbar,
    toolbarExtra,
  }: {
    columns?: MockColumn[]
    dataSource?: MockRecord[]
    headerExtra?: ReactNode
    loading?: boolean
    rowKey?: string | ((record: MockRecord) => Key)
    rowSelection?: {
      onChange?: (keys: string[]) => void
      selectedRowKeys?: string[]
    }
    title?: ReactNode
    toolbar?: ReactNode
    toolbarExtra?: ReactNode
  }) => {
    function mockCellValue(record: MockRecord, dataIndex?: string | string[]) {
      if (!dataIndex) return undefined
      if (Array.isArray(dataIndex)) {
        return dataIndex.reduce<unknown>(
          (value, key) =>
            value && typeof value === 'object' ? (value as MockRecord)[key] : undefined,
          record,
        )
      }
      return record[dataIndex]
    }

    function mockRowKey(record: MockRecord) {
      if (typeof rowKey === 'function') return rowKey(record)
      if (typeof rowKey === 'string') return String(record[rowKey] ?? '')
      return String(record.id ?? record.name ?? '')
    }

    const rows = dataSource ?? []
    const selectedRowKeys = rowSelection?.selectedRowKeys ?? []
    return (
      <div data-testid="admin-table">
        {title ? <div data-testid="table-title">{title}</div> : null}
        {headerExtra ? <div data-testid="table-header-extra">{headerExtra}</div> : null}
        {toolbar ? <div data-testid="table-toolbar">{toolbar}</div> : null}
        {toolbarExtra ? <div data-testid="table-toolbar-extra">{toolbarExtra}</div> : null}
        <div data-testid="table-loading">{String(Boolean(loading))}</div>
        <div data-testid="table-rows">{rows.length}</div>
        {rows.map((record, rowIndex) => {
          const key = String(mockRowKey(record))
          return (
            <div key={key || rowIndex} data-testid={`row-${rowIndex}`}>
              {rowSelection ? (
                <input
                  aria-label={`select-${key}`}
                  checked={selectedRowKeys.includes(key)}
                  onChange={(event) => {
                    const nextKeys = event.currentTarget.checked
                      ? Array.from(new Set([...selectedRowKeys, key]))
                      : selectedRowKeys.filter((item) => item !== key)
                    rowSelection.onChange?.(nextKeys)
                  }}
                  type="checkbox"
                />
              ) : null}
              {(columns ?? []).map((column, columnIndex) => {
                const value = mockCellValue(record, column.dataIndex)
                const content =
                  typeof column.render === 'function'
                    ? column.render(value, record, rowIndex)
                    : value
                return (
                  <div
                    key={`${String(column.key ?? column.dataIndex ?? columnIndex)}-${columnIndex}`}
                    data-testid={`cell-${rowIndex}-${columnIndex}`}
                    data-column={
                      Array.isArray(column.dataIndex)
                        ? column.dataIndex.join('.')
                        : column.dataIndex
                    }
                  >
                    {content as ReactNode}
                  </div>
                )
              })}
            </div>
          )
        })}
      </div>
    )
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []
let currentQueryClient: QueryClient | null = null

function setResponses(responses: Record<string, unknown>) {
  testState.responses = responses
}

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
  })
}

async function renderWithProviders(node: ReactNode, route = '/workloads/pods') {
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
  currentQueryClient = queryClient

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[route]}>{node}</MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })

  await flushAsyncWork()

  return container
}

async function rerenderWithProviders(node: ReactNode, route = '/workloads/pods') {
  const root = roots[roots.length - 1]
  const queryClient = currentQueryClient
  if (!root || !queryClient) throw new Error('renderWithProviders must run first')
  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[route]}>{node}</MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await flushAsyncWork()
}

describe('workloads pods page refresh controls', () => {
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
    const getComputedStyle = window.getComputedStyle.bind(window)
    Object.defineProperty(window, 'getComputedStyle', {
      writable: true,
      value: (element: Element) => getComputedStyle(element),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  beforeEach(() => {
    vi.useFakeTimers()
    testState.permissionKeys = [
      'platform.pods.delete',
      'platform.pods.exec',
      'platform.pods.logs',
      'platform.pods.update',
      'platform.pods.view',
    ]
    testState.scope.clusterId = 'cluster-a'
    testState.scope.namespace = 'monitoring'
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Direct Cluster',
          connectionMode: 'direct',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'workload.mutations',
          label: 'Workload mutations',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial' },
        },
      ],
      '/clusters/cluster-a/workloads/pods?namespace=monitoring': [
        {
          name: 'prometheus-0',
          namespace: 'monitoring',
          phase: 'Running',
          readyContainers: '1/1',
          restarts: 0,
          podIp: '10.0.0.10',
          nodeName: 'node-a',
          cpu: '10m',
          memory: '64Mi',
          ageSeconds: 60,
          allowedActions: ['delete', 'exec', 'logs', 'update'],
        },
      ],
    })
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) {
        root.unmount()
      }
    })
    roots = []
    currentQueryClient = null
    for (const container of containers) {
      container.remove()
    }
    containers = []
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it('manually refetches pod data from the refresh button', async () => {
    const container = await renderWithProviders(<WorkloadsPodsPage />)
    const podListPath = '/clusters/cluster-a/workloads/pods?namespace=monitoring'
    expect(apiGetMock.mock.calls.filter(([path]) => path === podListPath)).toHaveLength(1)

    const refreshButton = container.querySelector('button[aria-label="Refresh"]')
    if (!refreshButton) {
      throw new Error('refresh button not found')
    }

    await act(async () => {
      refreshButton.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushAsyncWork()

    expect(apiGetMock.mock.calls.filter(([path]) => path === podListPath)).toHaveLength(2)
  })

  it('polls pod data automatically when auto refresh is enabled', async () => {
    await renderWithProviders(<WorkloadsPodsPage />)
    const podListPath = '/clusters/cluster-a/workloads/pods?namespace=monitoring'
    expect(apiGetMock.mock.calls.filter(([path]) => path === podListPath)).toHaveLength(1)

    await act(async () => {
      vi.advanceTimersByTime(15000)
    })
    await flushAsyncWork()

    expect(apiGetMock.mock.calls.filter(([path]) => path === podListPath)).toHaveLength(2)
  })

  it('renders pod phase and ready container count in separate columns', async () => {
    const container = await renderWithProviders(<WorkloadsPodsPage />)
    await act(async () => {
      vi.advanceTimersByTime(0)
      await Promise.resolve()
    })
    await flushAsyncWork()

    expect(container.querySelector('[data-column="phase"]')?.textContent).toBe('Running')
    expect(container.querySelector('[data-column="readyContainers"]')?.textContent).toBe('就绪 1/1')
  })

  it('clears the previous cluster node filter when the cluster changes', async () => {
    const container = await renderWithProviders(<WorkloadsPodsPage />)
    const nodeSelect = Array.from(container.querySelectorAll('.ant-select')).find((item) =>
      item.textContent?.includes('全部节点'),
    )
    expect(nodeSelect).toBeDefined()
    await act(async () => {
      nodeSelect
        ?.querySelector('[role="combobox"]')
        ?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
      vi.runOnlyPendingTimers()
    })
    await flushAsyncWork()
    const nodeOption = Array.from(document.querySelectorAll('[role="option"]')).find(
      (item) => item.textContent === 'node-a',
    )
    expect(nodeOption).toBeDefined()
    await act(async () => nodeOption?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    expect(container.querySelector('[data-testid="table-rows"]')?.textContent).toBe('1')

    testState.scope.clusterId = 'cluster-b'
    testState.scope.namespace = null
    setResponses({
      '/clusters/cluster-b/workloads/pods': [
        {
          name: 'worker-b',
          namespace: 'default',
          phase: 'Running',
          readyContainers: '1/1',
          restarts: 0,
          podIp: '10.0.1.10',
          nodeName: 'node-b',
          ageSeconds: 60,
        },
      ],
    })
    await rerenderWithProviders(<WorkloadsPodsPage />)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    await flushAsyncWork()

    expect(container.querySelector('[data-testid="table-rows"]')?.textContent).toBe('1')
    expect(container.textContent).toContain('worker-b')
  })

  it('keeps pod usage values inside resource bars with request and limit markers', async () => {
    setResponses({
      '/clusters/cluster-a/workloads/pods?namespace=monitoring': [
        {
          name: 'resource-demo',
          namespace: 'monitoring',
          phase: 'Running',
          readyContainers: '1/1',
          restarts: 0,
          podIp: '10.0.0.11',
          nodeName: 'node-a',
          cpu: '50m',
          memory: '64Mi',
          requests: { cpu: '100m', memory: '128Mi' },
          limits: { cpu: '200m', memory: '256Mi' },
          ageSeconds: 60,
        },
      ],
    })

    const container = await renderWithProviders(<WorkloadsPodsPage />)
    await act(async () => {
      vi.advanceTimersByTime(0)
      await Promise.resolve()
    })
    await flushAsyncWork()

    const resourceCells = Array.from(container.querySelectorAll('.soha-pod-resource-limit-cell'))

    expect(resourceCells).toHaveLength(2)
    for (const [index, cell] of resourceCells.entries()) {
      expect(cell.querySelector('.soha-pod-resource-value')?.textContent).toBe(
        index === 0 ? '50m' : '64Mi',
      )
      expect(cell.tagName).toBe('BUTTON')
      expect(cell.querySelectorAll('.ant-progress')).toHaveLength(1)
      expect(cell.querySelector('.ant-progress')?.getAttribute('aria-hidden')).toBe('true')
      expect(cell.querySelectorAll('.soha-pod-resource-marker.is-request')).toHaveLength(1)
      expect(cell.querySelectorAll('.soha-pod-resource-marker.is-limit')).toHaveLength(1)
      expect(cell.getAttribute('aria-label')).toContain('使用')
      expect(cell.getAttribute('aria-label')).toContain('请求以内')
    }
  })

  it('uses actual request and limit thresholds without turning missing metrics into zero', async () => {
    const cases = [
      {
        name: 'zero',
        cpu: '0',
        request: '100m',
        limit: '500m',
        tone: 'success',
        usage: '0m',
        percent: 0,
      },
      {
        name: 'at-request',
        cpu: '100m',
        request: '100m',
        limit: '500m',
        tone: 'success',
        usage: '100m',
        percent: 20,
      },
      {
        name: 'above-request',
        cpu: '101m',
        request: '100m',
        limit: '500m',
        tone: 'warning',
        usage: '101m',
        percent: 20.2,
      },
      {
        name: 'below-danger',
        cpu: '449m',
        request: '100m',
        limit: '500m',
        tone: 'warning',
        usage: '449m',
        percent: 89.8,
      },
      {
        name: 'at-danger',
        cpu: '450m',
        request: '100m',
        limit: '500m',
        tone: 'danger',
        usage: '450m',
        percent: 90,
      },
      {
        name: 'above-limit',
        cpu: '600m',
        request: '100m',
        limit: '500m',
        tone: 'danger',
        usage: '600m',
        percent: 100,
      },
      {
        name: 'no-limit',
        cpu: '150m',
        request: '100m',
        tone: 'warning',
        usage: '150m',
        percent: 100,
      },
      {
        name: 'zero-request',
        cpu: '10m',
        request: '0',
        limit: '500m',
        tone: 'warning',
        usage: '10m',
        percent: 2,
      },
      { name: 'no-request', cpu: '50m', limit: '500m', tone: 'success', usage: '50m', percent: 10 },
      { name: 'no-baseline', cpu: '50m', tone: 'unknown', usage: '50m', percent: 0 },
      {
        name: 'missing',
        cpu: '-',
        request: '100m',
        limit: '500m',
        tone: 'unknown',
        usage: '—',
        percent: 0,
      },
      {
        name: 'invalid',
        cpu: 'Infinity',
        request: '100m',
        limit: '500m',
        tone: 'unknown',
        usage: '—',
        percent: 0,
      },
    ]
    setResponses({
      '/clusters/cluster-a/workloads/pods?namespace=monitoring': cases.map((item) => ({
        name: item.name,
        namespace: 'monitoring',
        phase: 'Running',
        readyContainers: '1/1',
        restarts: 0,
        ageSeconds: 60,
        cpu: item.cpu,
        requests: { cpu: item.request },
        limits: { cpu: item.limit },
      })),
    })
    const container = await renderWithProviders(<WorkloadsPodsPage />)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
    await flushAsyncWork()

    for (const item of cases) {
      const cell = Array.from(container.querySelectorAll('.soha-pod-resource-limit-cell')).find(
        (node) => node.getAttribute('aria-label')?.startsWith(`${item.name} · CPU`),
      )!
      expect(cell, item.name).toBeDefined()
      expect(cell.classList.contains(`is-${item.tone}`), item.name).toBe(true)
      expect(cell.querySelector('.soha-pod-resource-value')?.textContent, item.name).toBe(
        item.usage,
      )
      const renderedPercent = (
        cell.querySelector('.soha-pod-resource-progress-wrap') as HTMLElement
      ).style.getPropertyValue('--soha-pod-resource-usage-percent')
      expect(Number.parseFloat(renderedPercent), item.name).toBeCloseTo(item.percent)
      expect(cell.querySelectorAll('.is-limit'), item.name).toHaveLength(item.limit ? 1 : 0)
      expect(cell.querySelectorAll('.is-request'), item.name).toHaveLength(item.request ? 1 : 0)
    }
    const missing = container.querySelector('[aria-label^="missing · CPU"]')!
    expect(missing.getAttribute('aria-label')).toContain('暂无指标')
    const unbounded = container.querySelector('[aria-label^="no-baseline · CPU"]')!
    expect(unbounded.getAttribute('aria-label')).toContain('无比例基准')
    const noLimit = container.querySelector('[aria-label^="no-limit · CPU"]')!
    expect(noLimit.getAttribute('aria-label')).toContain('以请求为比例基准')
    expect(
      container.querySelector('[aria-label^="above-limit · CPU"] .is-limit.is-passed'),
    ).not.toBeNull()
  })

  it('opens pod logs and terminal from row actions in the realtime dock', async () => {
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Direct Cluster',
          connectionMode: 'direct',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'workload.mutations',
          label: 'Workload mutations',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial' },
        },
        {
          key: 'pod.logs',
          label: 'Pod logs',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial' },
        },
        {
          key: 'pod.exec',
          label: 'Pod exec',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial' },
        },
      ],
      '/clusters/cluster-a/workloads/pods?namespace=monitoring': [
        {
          name: 'prometheus-0',
          namespace: 'monitoring',
          phase: 'Running',
          readyContainers: '1/1',
          restarts: 0,
          podIp: '10.0.0.10',
          nodeName: 'node-a',
          cpu: '10m',
          memory: '64Mi',
          ageSeconds: 60,
          allowedActions: ['delete', 'exec', 'logs', 'update'],
        },
      ],
    })

    const container = await renderWithProviders(<WorkloadsPodsPage />)
    await act(async () => {
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()
    const logsButton = container.querySelector('button[aria-label="打开日志会话"]')
    const terminalButton = container.querySelector('button[aria-label="打开终端会话"]')
    const deleteButton = container.querySelector('button[aria-label="删除 Pod"]')

    expect(logsButton).toBeInstanceOf(HTMLButtonElement)
    expect(terminalButton).toBeInstanceOf(HTMLButtonElement)
    expect(deleteButton).toBeInstanceOf(HTMLButtonElement)
    await act(async () => logsButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await act(async () => terminalButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await act(async () => deleteButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()

    expect(openSessionMock).toHaveBeenNthCalledWith(1, {
      clusterId: 'cluster-a',
      kind: 'logs',
      namespace: 'monitoring',
      podName: 'prometheus-0',
      streamingDisabledReason: undefined,
    })
    expect(openSessionMock).toHaveBeenNthCalledWith(2, {
      clusterId: 'cluster-a',
      kind: 'terminal',
      namespace: 'monitoring',
      podName: 'prometheus-0',
      shell: '/bin/sh',
    })

    expect(document.body.textContent).toContain('确认删除 Pod prometheus-0？')
    expect(container.querySelector('button[aria-label="更多 Pod 操作"]')).toBeNull()
  })

  it('disables pod delete when agent mode does not support it', async () => {
    const partialReason = 'pod deletion remains direct-only'
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Agent Cluster',
          connectionMode: 'agent',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'workload.mutations',
          label: 'Workload mutations',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial', notes: [partialReason] },
        },
      ],
      '/clusters/cluster-a/workloads/pods?namespace=monitoring': [
        {
          name: 'failed-pod',
          namespace: 'monitoring',
          phase: 'Failed',
          readyContainers: '0/1',
          restarts: 0,
          podIp: '',
          nodeName: 'node-a',
          cpu: '0',
          memory: '0',
          ageSeconds: 60,
          allowedActions: ['delete'],
        },
      ],
    })

    const container = await renderWithProviders(<WorkloadsPodsPage />)
    await act(async () => {
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    expect(
      (container.querySelector('button[aria-label="删除 Pod"]') as HTMLButtonElement | null)
        ?.disabled,
    ).toBe(true)
    expect(container.querySelector('input[aria-label="select-monitoring/failed-pod"]')).toBeNull()
    expect(container.textContent).not.toContain('批量删除')
    expect(apiDeleteMock).not.toHaveBeenCalled()
  })

  it('hides pod delete controls when the role lacks the delete permission', async () => {
    testState.permissionKeys = ['platform.pods.view']

    const container = await renderWithProviders(<WorkloadsPodsPage />)
    await act(async () => {
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    expect(container.querySelector('button[aria-label="删除 Pod"]')).toBeNull()

    const checkbox = container.querySelector('input[aria-label="select-monitoring/prometheus-0"]')
    await act(async () => {
      checkbox?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushAsyncWork()

    expect(container.textContent).not.toContain('批量删除')
  })

  it('hides pod delete controls when the current policy denies delete', async () => {
    setResponses({
      '/clusters/cluster-a/workloads/pods?namespace=monitoring': [
        {
          name: 'production-pod',
          namespace: 'monitoring',
          phase: 'Running',
          readyContainers: '1/1',
          restarts: 0,
          podIp: '10.0.0.12',
          nodeName: 'node-a',
          cpu: '10m',
          memory: '64Mi',
          ageSeconds: 60,
          allowedActions: [],
        },
      ],
    })

    const container = await renderWithProviders(<WorkloadsPodsPage />)
    await act(async () => {
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    expect(container.querySelector('button[aria-label="删除 Pod"]')).toBeNull()
  })

  it('limits concurrent pod batch delete requests', async () => {
    const podCount = 12
    const releaseDeleteRequests: Array<() => void> = []
    let activeDeletes = 0
    let maxActiveDeletes = 0
    apiDeleteMock.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          activeDeletes += 1
          maxActiveDeletes = Math.max(maxActiveDeletes, activeDeletes)
          releaseDeleteRequests.push(() => {
            activeDeletes -= 1
            resolve({ data: null })
          })
        }),
    )
    for (let index = 1; index < podCount; index += 1) {
      apiDeleteMock.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            activeDeletes += 1
            maxActiveDeletes = Math.max(maxActiveDeletes, activeDeletes)
            releaseDeleteRequests.push(() => {
              activeDeletes -= 1
              resolve({ data: null })
            })
          }),
      )
    }
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          connectionMode: 'direct_kubeconfig',
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'workload.mutations',
          label: 'Workload mutations',
          category: 'workloads',
          riskLevel: 'mutate',
          requiresApproval: true,
          direct: { status: 'available' },
          agent: { status: 'partial' },
        },
      ],
      '/clusters/cluster-a/workloads/pods?namespace=monitoring': Array.from(
        { length: podCount },
        (_, index) => ({
          name: `failed-pod-${index}`,
          namespace: 'monitoring',
          phase: 'Failed',
          readyContainers: '0/1',
          restarts: 0,
          podIp: '',
          nodeName: 'node-a',
          cpu: '0',
          memory: '0',
          ageSeconds: 60,
          allowedActions: ['delete'],
        }),
      ),
    })

    const container = await renderWithProviders(<WorkloadsPodsPage />)
    await act(async () => {
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    for (let index = 0; index < podCount; index += 1) {
      const checkbox = container.querySelector(
        `input[aria-label="select-monitoring/failed-pod-${index}"]`,
      )
      expect(checkbox).toBeInstanceOf(HTMLInputElement)
      await act(async () => {
        checkbox?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      })
      await flushAsyncWork()
    }

    const batchDeleteButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.trim() === '批量删除',
    )
    expect(batchDeleteButton).toBeInstanceOf(HTMLButtonElement)
    await act(async () => {
      batchDeleteButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    const confirmButton = document.body.querySelector('.ant-popconfirm-buttons .ant-btn-primary')
    expect(confirmButton).toBeInstanceOf(HTMLButtonElement)
    await act(async () => {
      confirmButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })
    await flushAsyncWork()

    expect(apiDeleteMock).toHaveBeenCalledTimes(8)
    expect(maxActiveDeletes).toBeLessThanOrEqual(8)

    while (releaseDeleteRequests.length > 0) {
      const release = releaseDeleteRequests.shift()
      await act(async () => {
        release?.()
        await Promise.resolve()
      })
      await flushAsyncWork()
    }

    expect(apiDeleteMock).toHaveBeenCalledTimes(podCount)
    expect(maxActiveDeletes).toBeLessThanOrEqual(8)
  }, 15_000)

  it('hides the deployment action column when the current cluster capability is unsupported', async () => {
    const unsupportedReason = 'workload mutations are disabled for agent-connected clusters'
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Agent Cluster',
          region: 'dev',
          environment: 'test',
          labels: {},
          connectionMode: 'agent',
          version: 'v1.30.0',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'workload.mutations',
          label: 'Workload mutations',
          category: 'workloads',
          direct: { status: 'available' },
          agent: {
            status: 'unsupported',
            notes: [unsupportedReason],
          },
        },
      ],
      '/clusters/cluster-a/workloads/deployments?namespace=monitoring': [
        {
          name: 'prometheus',
          namespace: 'monitoring',
          desiredReplicas: 1,
          readyReplicas: 1,
          updatedReplicas: 1,
          available: 1,
          ageSeconds: 300,
          allowedActions: ['restart', 'scale', 'update', 'delete'],
        },
      ],
    })

    const container = await renderWithProviders(
      <WorkloadsDeploymentsPage />,
      '/workloads/deployments',
    )
    await act(async () => {
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    expect(apiGetMock).toHaveBeenCalledWith('/clusters/capabilities')
    expect(container.textContent).toContain('当前集群连接模式暂不支持该操作')
    expect(container.textContent).not.toContain(unsupportedReason)
    for (const label of ['重启', '扩缩', '删除', '回滚']) {
      expect(container.querySelector(`button[aria-label="${label}"]`)).toBeNull()
    }
    expect(container.querySelector('input[aria-label="select-monitoring/prometheus"]')).toBeNull()
  })

  it('renders statefulset restart, scale, and delete actions using workload permissions', async () => {
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Direct Cluster',
          connectionMode: 'direct',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'workload.mutations',
          label: 'Workload mutations',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial' },
        },
      ],
      '/clusters/cluster-a/workloads/statefulsets?namespace=monitoring': [
        {
          name: 'prometheus',
          namespace: 'monitoring',
          serviceName: 'prometheus',
          desiredReplicas: 2,
          readyReplicas: 1,
          currentReplicas: 2,
          ageSeconds: 300,
          allowedActions: ['restart', 'scale', 'delete'],
        },
      ],
    })

    const container = await renderWithProviders(
      <WorkloadsStatefulSetsPage />,
      '/workloads/statefulsets',
    )
    await act(async () => {
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    const restartButton = container.querySelector('button[aria-label="重启"]')
    const scaleButton = container.querySelector('button[aria-label="扩缩"]')
    const deleteButton = container.querySelector('button[aria-label="删除"]')
    expect(restartButton).toBeInstanceOf(HTMLButtonElement)
    expect(scaleButton).toBeInstanceOf(HTMLButtonElement)
    expect(deleteButton).toBeInstanceOf(HTMLButtonElement)

    await act(async () => {
      restartButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushAsyncWork()
    expect(apiPostMock).toHaveBeenCalledWith('/clusters/cluster-a/workloads/statefulsets/restart', {
      namespace: 'monitoring',
      name: 'prometheus',
    })
  })

  it('opens the statefulset scale modal and submits replicas to the scale endpoint', async () => {
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Direct Cluster',
          connectionMode: 'direct',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'workload.mutations',
          label: 'Workload mutations',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial' },
        },
      ],
      '/clusters/cluster-a/workloads/statefulsets?namespace=monitoring': [
        {
          name: 'prometheus',
          namespace: 'monitoring',
          serviceName: 'prometheus',
          desiredReplicas: 2,
          readyReplicas: 1,
          currentReplicas: 2,
          ageSeconds: 300,
          allowedActions: ['scale'],
        },
      ],
    })

    const container = await renderWithProviders(
      <WorkloadsStatefulSetsPage />,
      '/workloads/statefulsets',
    )
    await act(async () => {
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    const scaleButton = container.querySelector('button[aria-label="扩缩"]')
    await act(async () => {
      scaleButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    const modalOkButton = document.body.querySelector('.ant-modal-footer .ant-btn-primary')
    expect(document.body.textContent).toContain('StatefulSet 扩缩容')
    expect(modalOkButton).toBeInstanceOf(HTMLButtonElement)

    await act(async () => {
      modalOkButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushAsyncWork()
    expect(apiPostMock).toHaveBeenCalledWith('/clusters/cluster-a/workloads/statefulsets/scale', {
      namespace: 'monitoring',
      name: 'prometheus',
      replicas: 2,
    })
  })

  it('renders daemonset restart and delete actions', async () => {
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Direct Cluster',
          connectionMode: 'direct',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'workload.mutations',
          label: 'Workload mutations',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial' },
        },
      ],
      '/clusters/cluster-a/workloads/daemonsets?namespace=monitoring': [
        {
          name: 'node-exporter',
          namespace: 'monitoring',
          desiredNumber: 3,
          currentNumber: 3,
          readyNumber: 3,
          availableNumber: 3,
          updatedNumber: 3,
          ageSeconds: 300,
          allowedActions: ['restart', 'delete'],
        },
      ],
    })

    const container = await renderWithProviders(
      <WorkloadsDaemonSetsPage />,
      '/workloads/daemonsets',
    )
    await act(async () => {
      vi.runOnlyPendingTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    const restartButton = container.querySelector('button[aria-label="重启"]')
    const deleteButton = container.querySelector('button[aria-label="删除"]')
    expect(restartButton).toBeInstanceOf(HTMLButtonElement)
    expect(deleteButton).toBeInstanceOf(HTMLButtonElement)

    await act(async () => {
      restartButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushAsyncWork()
    expect(apiPostMock).toHaveBeenCalledWith('/clusters/cluster-a/workloads/daemonsets/restart', {
      namespace: 'monitoring',
      name: 'node-exporter',
    })
  })

  it('renders container, volume, and related resource tabs on pod detail page', async () => {
    setResponses({
      '/clusters/cluster-a/workloads/pods/demo-pod/detail?namespace=monitoring': {
        name: 'demo-pod',
        namespace: 'monitoring',
        phase: 'Running',
        createdAt: '2026-05-07T12:00:00Z',
        containers: [
          {
            name: 'app',
            image: 'demo:1.0',
            ready: true,
            restartCount: 0,
            state: 'running',
          },
        ],
        volumes: [
          {
            name: 'config-volume',
            type: 'ConfigMap',
            sourceName: 'demo-config',
            readOnly: false,
            details: ['ConfigMap: demo-config'],
            volumeMounts: [{ name: 'app', mountPath: '/etc/config', readOnly: false }],
            referencedConfigMaps: ['demo-config'],
          },
        ],
        relatedResources: [
          {
            kind: 'Service',
            name: 'demo-svc',
            namespace: 'monitoring',
            relations: ['selected-by-service'],
            details: ['Type: ClusterIP'],
          },
        ],
        allowedActions: ['logs', 'exec'],
      },
    })

    const container = await renderWithProviders(
      <Routes>
        <Route path="/workloads/pods/:podName" element={<PodDetailPage />} />
      </Routes>,
      '/workloads/pods/demo-pod?namespace=monitoring',
    )

    await act(async () => {
      vi.runAllTimers()
      await Promise.resolve()
    })

    expect(container.textContent).toContain('容器')
    expect(container.textContent).toContain('卷')
    expect(container.textContent).toContain('相关资源')
  })

  it('hides pod logs and terminal tabs when the role lacks their permissions', async () => {
    testState.permissionKeys = ['platform.pods.view']
    setResponses({
      '/clusters/cluster-a/workloads/pods/demo-pod/detail?namespace=monitoring': {
        name: 'demo-pod',
        namespace: 'monitoring',
        phase: 'Running',
        createdAt: '2026-05-07T12:00:00Z',
        containers: [],
        allowedActions: ['logs', 'exec'],
      },
    })

    const container = await renderWithProviders(
      <Routes>
        <Route path="/workloads/pods/:podName" element={<PodDetailPage />} />
      </Routes>,
      '/workloads/pods/demo-pod?namespace=monitoring',
    )
    await act(async () => {
      vi.runAllTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    const tabLabels = Array.from(container.querySelectorAll('[role="tab"]')).map((tab) =>
      tab.textContent?.replace(/\s/g, ''),
    )
    expect(tabLabels).not.toContain('日志')
    expect(tabLabels).not.toContain('终端')
  })

  it('disables pod logs and terminal tabs when the current policy denies them', async () => {
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Direct Cluster',
          connectionMode: 'direct',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'pod.logs',
          label: 'Pod logs',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial' },
        },
        {
          key: 'pod.exec',
          label: 'Pod exec',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial' },
        },
      ],
      '/clusters/cluster-a/workloads/pods/demo-pod/detail?namespace=monitoring': {
        name: 'demo-pod',
        namespace: 'monitoring',
        phase: 'Running',
        createdAt: '2026-05-07T12:00:00Z',
        containers: [],
        allowedActions: [],
      },
    })

    const container = await renderWithProviders(
      <Routes>
        <Route path="/workloads/pods/:podName" element={<PodDetailPage />} />
      </Routes>,
      '/workloads/pods/demo-pod?namespace=monitoring',
    )
    await act(async () => {
      vi.runAllTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    for (const label of ['日志', '终端']) {
      const tab = Array.from(container.querySelectorAll('[role="tab"]')).find(
        (item) => item.textContent?.replace(/\s/g, '') === label,
      )
      expect(tab).toBeInstanceOf(HTMLElement)
      expect(tab?.getAttribute('aria-disabled')).toBe('true')
    }
  })

  it('keeps partial pod logs on snapshot mode and disables interactive terminal', async () => {
    const logsReason = 'agent connection supports snapshot logs only'
    const execReason = 'agent connection supports non-interactive exec only'
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Agent Cluster',
          connectionMode: 'agent',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'pod.logs',
          label: 'Pod logs',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial', notes: [logsReason] },
        },
        {
          key: 'pod.exec',
          label: 'Pod exec',
          category: 'workloads',
          direct: { status: 'available' },
          agent: { status: 'partial', notes: [execReason] },
        },
      ],
      '/clusters/cluster-a/workloads/pods/demo-pod/detail?namespace=monitoring': {
        name: 'demo-pod',
        namespace: 'monitoring',
        phase: 'Running',
        createdAt: '2026-05-07T12:00:00Z',
        containers: [
          {
            name: 'app',
            image: 'demo:1.0',
            ready: true,
            restartCount: 0,
            state: 'running',
          },
        ],
        allowedActions: ['logs', 'exec'],
      },
      '/clusters/cluster-a/workloads/pods/demo-pod/logs?namespace=monitoring&tailLines=100&container=app':
        {
          content: 'line one\nline two\n',
        },
    })

    const container = await renderWithProviders(
      <Routes>
        <Route path="/workloads/pods/:podName" element={<PodDetailPage />} />
      </Routes>,
      '/workloads/pods/demo-pod?namespace=monitoring',
    )
    await flushAsyncWork()
    await act(async () => {
      vi.runAllTimers()
      await Promise.resolve()
    })
    await flushAsyncWork()

    const terminalTab = Array.from(container.querySelectorAll('[role="tab"]')).find((tab) =>
      tab.textContent?.replace(/\s/g, '').includes('终端'),
    )
    expect(terminalTab).toBeInstanceOf(HTMLElement)
    expect(terminalTab?.getAttribute('aria-disabled')).toBe('true')

    const logContainer = await renderWithProviders(
      <PodLogViewer
        active
        clusterId="cluster-a"
        namespace="monitoring"
        podName="demo-pod"
        container="app"
        streamingDisabledReason={logsReason}
      />,
    )
    await flushAsyncWork()

    expect(logContainer.textContent).toContain(logsReason)
    expect(logContainer.textContent).toContain('轮询')
    expect(
      apiGetMock.mock.calls.some(([path]) =>
        String(path).includes('/clusters/cluster-a/workloads/pods/demo-pod/logs?'),
      ),
    ).toBe(true)
    expect(withStreamTicketMock).not.toHaveBeenCalled()
  })
})
