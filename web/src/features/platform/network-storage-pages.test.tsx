/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { NetworkGatewayClassesPage, NetworkServicesPage, StoragePvcPage } from './network-storage-pages'

const testState = vi.hoisted(() => ({
  responses: {} as Record<string, unknown>,
  scope: {
    clusterId: 'cluster-a' as string | null,
    namespace: 'team-a' as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: vi.fn((path: string) => {
      if (!(path in testState.responses)) {
        return Promise.resolve({ data: [] })
      }
      return Promise.resolve({ data: testState.responses[path] })
    }),
  },
}))

vi.mock('@/components/resource-actions', () => ({
  useResourceActions: () => ({
    column: { title: '操作', dataIndex: '__actions', render: () => null },
    modalNode: null,
  }),
}))

vi.mock('@/components/resource-events-timeline', () => ({
  ResourceEventsTimeline: () => <div data-testid="events-timeline" />,
}))

vi.mock('@/components/resource-metrics-panel', () => ({
  ResourceMetricsPanel: () => <div data-testid="metrics-panel" />,
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    empty,
    headerExtra,
    paginationSummary,
    title,
    toolbar,
    toolbarExtra,
  }: {
    columns: Array<Record<string, any>>
    dataSource: Array<Record<string, any>>
    empty?: ReactNode
    headerExtra?: ReactNode
    paginationSummary?: ReactNode | ((total: number, range: [number, number]) => ReactNode)
    title?: ReactNode
    toolbar?: ReactNode
    toolbarExtra?: ReactNode
  }) => (
    <div data-testid="admin-table">
      {title ? <div data-testid="table-title">{title}</div> : null}
      {headerExtra ? <div data-testid="header-extra">{headerExtra}</div> : null}
      {toolbar ? <div data-testid="toolbar">{toolbar}</div> : null}
      {toolbarExtra ? <div data-testid="toolbar-extra">{toolbarExtra}</div> : null}
      {paginationSummary ? (
        <div data-testid="pagination-summary">
          {typeof paginationSummary === 'function' ? paginationSummary(dataSource.length, [1, dataSource.length]) : paginationSummary}
        </div>
      ) : null}
      <div data-testid="row-count">{dataSource.length}</div>
      {dataSource.length === 0 ? <div data-testid="empty">{empty}</div> : null}
      {dataSource.map((record, rowIndex) => (
        <div key={`${record.name || 'row'}-${rowIndex}`} data-testid={`row-${rowIndex}`}>
          {columns.map((column, columnIndex) => {
            const key = typeof column.dataIndex === 'string' ? column.dataIndex : `col-${columnIndex}`
            const value = typeof column.dataIndex === 'string' ? record[column.dataIndex] : undefined
            const content = typeof column.render === 'function' ? column.render(value, record, rowIndex) : value
            return (
              <div key={`${key}-${columnIndex}`} data-testid={`cell-${rowIndex}-${columnIndex}`}>
                {content == null ? '' : content}
              </div>
            )
          })}
        </div>
      ))}
    </div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setResponses(responses: Record<string, unknown>) {
  testState.responses = responses
}

function setNativeInputValue(element: HTMLInputElement, value: string) {
  const prototype = Object.getPrototypeOf(element)
  const descriptor = Object.getOwnPropertyDescriptor(prototype, 'value')
  descriptor?.set?.call(element, value)
}

function installDomMocks() {
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
}

async function renderWithProviders(node: ReactNode, route = '/network/services') {
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
          <MemoryRouter
            initialEntries={[route]}
            future={{
              v7_startTransition: true,
              v7_relativeSplatPath: true,
            }}
          >
            {node}
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })

  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
    await new Promise((resolve) => window.setTimeout(resolve, 0))
  })

  return container
}

describe('network resource list pages', () => {
  beforeAll(() => {
    installDomMocks()
  })

  beforeEach(() => {
    testState.scope.clusterId = 'cluster-a'
    testState.scope.namespace = 'team-a'
    setResponses({})
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

  it('keeps service search controls in a separate card and summarizes results in pagination', async () => {
    setResponses({
      '/clusters/cluster-a/network/services?namespace=team-a': [
        { name: 'core-dns', namespace: 'kube-system', type: 'ClusterIP', clusterIp: '10.43.0.10', ports: ['53/UDP'], ageSeconds: 60, allowedActions: ['view'] },
        { name: 'web', namespace: 'team-a', type: 'NodePort', clusterIp: '10.43.1.20', ports: ['80/TCP'], ageSeconds: 120, allowedActions: ['view'] },
      ],
    })

    const container = await renderWithProviders(<NetworkServicesPage />)

    expect(container.querySelector('[data-testid="table-title"]')).toBeNull()
    expect(container.querySelector('[data-testid="toolbar"]')).toBeNull()
    expect(container.querySelector('input[placeholder="搜索 Service / namespace / type / port"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain('当前 2 / 2 条')

    const headerButtons = Array.from(container.querySelectorAll('[data-testid="header-extra"] button')).map((button) =>
      button.textContent?.trim() || button.getAttribute('aria-label'),
    )
    expect(headerButtons).toEqual(['切换表格密度', '刷新'])

    const input = container.querySelector('input[placeholder="搜索 Service / namespace / type / port"]') as HTMLInputElement | null
    if (!input) {
      throw new Error('service search input not found')
    }

    await act(async () => {
      setNativeInputValue(input, 'core')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('1')
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain('当前 1 / 2 条')
    expect(container.textContent).toContain('core-dns')
    expect(container.textContent).not.toContain('web')
  })

  it('keeps cluster-scoped gateway classes on the same network table pattern', async () => {
    setResponses({
      '/clusters/cluster-a/network/gatewayclasses': [
        { name: 'traefik', controllerName: 'traefik.io/gateway-controller', accepted: 'True', parametersRef: '', ageSeconds: 60, allowedActions: ['view'] },
      ],
    })

    const container = await renderWithProviders(<NetworkGatewayClassesPage />, '/network/gateway-api/gatewayclasses')

    expect(container.querySelector('[data-testid="table-title"]')).toBeNull()
    expect(container.querySelector('[data-testid="toolbar"]')).toBeNull()
    expect(container.querySelector('input[placeholder="搜索 GatewayClass / controller"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain('当前 1 / 1 条')
    expect(container.textContent).not.toContain('集群级资源')
  })

  it('keeps storage PVC filters in a separate query card with create before table utilities', async () => {
    setResponses({
      '/clusters/cluster-a/storage/persistentvolumeclaims?namespace=team-a': [
        { name: 'data-core', namespace: 'team-a', status: 'Bound', volumeName: 'pv-core', requested: '10Gi', storageClass: 'local-path', accessModes: ['ReadWriteOnce'], ageSeconds: 60, allowedActions: ['view'] },
        { name: 'cache', namespace: 'team-a', status: 'Pending', volumeName: '', requested: '1Gi', storageClass: 'fast', accessModes: ['ReadWriteOnce'], ageSeconds: 120, allowedActions: ['view'] },
      ],
    })

    const container = await renderWithProviders(<StoragePvcPage />, '/storage/persistentvolumeclaims')

    expect(container.querySelector('[data-testid="table-title"]')).toBeNull()
    expect(container.querySelector('[data-testid="toolbar"]')).toBeNull()
    expect(container.querySelector('input[placeholder="搜索 PVC / namespace / storageClass"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain('当前 2 / 2 条')

    const headerButtons = Array.from(container.querySelectorAll('[data-testid="header-extra"] button')).map((button) =>
      button.textContent?.trim() || button.getAttribute('aria-label'),
    )
    expect(headerButtons).toEqual(['新增', '切换表格密度', '刷新'])

    const input = container.querySelector('input[placeholder="搜索 PVC / namespace / storageClass"]') as HTMLInputElement | null
    if (!input) {
      throw new Error('pvc search input not found')
    }

    await act(async () => {
      setNativeInputValue(input, 'core')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('1')
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain('当前 1 / 2 条')
    expect(container.textContent).toContain('data-core')
    expect(container.textContent).not.toContain('cache')
  })
})
