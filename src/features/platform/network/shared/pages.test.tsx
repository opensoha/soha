/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { parse } from 'yaml'
import { IngressDetailPage } from '../ingresses/detail-page'
import { NetworkIngressesPage } from '../ingresses/list-page'
import { ServiceDetailPage } from '../services/detail-page'
import { NetworkServicesPage } from '../services/list-page'

const testState = vi.hoisted(() => ({
  responses: {} as Record<string, unknown>,
  scope: {
    clusterId: 'cluster-a' as string | null,
    namespace: 'team-a' as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn((path: string) => Promise.resolve({ data: testState.responses[path] ?? [] })),
  post: vi.fn((path: string, _body?: unknown) =>
    Promise.resolve({ data: testState.responses[path] ?? {} }),
  ),
  put: vi.fn((path: string, _body?: { content: string }) =>
    Promise.resolve({ data: testState.responses[path] ?? {} }),
  ),
}))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

vi.mock('@/features/copilot', () => ({
  encodeAIContextForElement: () => 'ai-context',
  useAIPageContext: () => undefined,
}))

vi.mock('@/components/resource-events-timeline', () => ({
  ResourceEventsTimeline: () => <div data-testid="events-timeline" />,
}))

vi.mock('@/components/resource-metrics-panel', () => ({
  ResourceMetricsPanel: () => <div data-testid="metrics-panel" />,
}))

vi.mock('@/components/k8s-yaml-editor', () => ({
  K8sYamlEditor: () => <div data-testid="yaml-editor" />,
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    empty,
    headerExtra,
    localSorting,
    pageSize,
    paginationSummary,
    title,
    viewportScroll,
    virtual,
  }: {
    columns: Array<Record<string, any>>
    dataSource: Array<Record<string, any>>
    empty?: ReactNode
    headerExtra?: ReactNode
    localSorting?: boolean
    pageSize?: number
    paginationSummary?: ReactNode
    title?: ReactNode
    viewportScroll?: boolean
    virtual?: boolean
  }) => (
    <div
      data-page-size={pageSize}
      data-testid="admin-table"
      data-local-sorting={localSorting}
      data-viewport-scroll={viewportScroll}
      data-virtual={virtual}
    >
      {title ? <div data-testid="table-title">{title}</div> : null}
      {headerExtra ? <div data-testid="header-extra">{headerExtra}</div> : null}
      {paginationSummary ? <div data-testid="pagination-summary">{paginationSummary}</div> : null}
      <div data-testid="row-count">{dataSource.length}</div>
      {dataSource.length === 0 ? <div data-testid="empty">{empty}</div> : null}
      {dataSource.map((record, rowIndex) => (
        <div key={`${record.name}-${rowIndex}`} data-testid={`row-${rowIndex}`}>
          {columns.map((column, columnIndex) => {
            const value =
              typeof column.dataIndex === 'string' ? record[column.dataIndex] : undefined
            const content =
              typeof column.render === 'function' ? column.render(value, record, rowIndex) : value
            return (
              <div key={`${String(column.dataIndex)}-${columnIndex}`}>
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

function installDomMocks() {
  const getComputedStyle = window.getComputedStyle.bind(window)

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
  Object.defineProperty(window, 'getComputedStyle', {
    writable: true,
    value: (element: Element) => getComputedStyle(element),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
}

async function flush() {
  await Promise.resolve()
  await Promise.resolve()
  await new Promise((resolve) => window.setTimeout(resolve, 0))
}

async function renderPage(
  node: ReactNode,
  route: string,
  routePath: string,
  extraRoutes?: ReactNode,
) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[route]}>
            <Routes>
              <Route path={routePath} element={node} />
              {extraRoutes}
            </Routes>
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })
  await act(async () => {
    await flush()
  })
  return container
}

async function clickTab(container: HTMLElement, label: string) {
  const tab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find((item) =>
    item.textContent?.includes(label),
  )
  if (!tab) throw new Error(`Tab not found: ${label}`)
  await act(async () => {
    tab.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flush()
  })
}

function setNativeInputValue(element: HTMLInputElement, value: string) {
  const descriptor = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(element), 'value')
  descriptor?.set?.call(element, value)
}

describe('network core pages', () => {
  beforeAll(installDomMocks)

  beforeEach(() => {
    testState.scope.clusterId = 'cluster-a'
    testState.scope.namespace = 'team-a'
    testState.responses = {}
    vi.clearAllMocks()
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) root.unmount()
    })
    roots = []
    for (const container of containers) container.remove()
    containers = []
  })

  it('preserves Service list search and API path with shared continuous browsing', async () => {
    const yamlPath = '/clusters/cluster-a/network/services/core-dns/yaml?namespace=team-a'
    const planPath = '/clusters/cluster-a/resources/update-plan'
    testState.responses['/clusters/cluster-a/network/services?namespace=team-a'] = [
      {
        name: 'core-dns',
        namespace: 'team-a',
        type: 'ClusterIP',
        clusterIp: '10.43.0.10',
        ports: ['53/UDP'],
        ageSeconds: 60,
        allowedActions: ['view', 'update'],
      },
      {
        name: 'web',
        namespace: 'team-a',
        type: 'NodePort',
        clusterIp: '10.43.1.20',
        ports: ['80/TCP'],
        portMappings: [
          {
            name: 'http',
            protocol: 'TCP',
            targetPort: '8080',
            port: 80,
            nodePort: 30001,
          },
        ],
        ageSeconds: 120,
        allowedActions: ['view'],
      },
    ]
    testState.responses[yamlPath] = {
      kind: 'Service',
      name: 'core-dns',
      namespace: 'team-a',
      content: JSON.stringify({
        apiVersion: 'v1',
        kind: 'Service',
        metadata: {
          labels: { 'k8s-app': 'kube-dns' },
          name: 'core-dns',
          namespace: 'team-a',
          resourceVersion: '12',
        },
        spec: {
          clusterIP: '10.43.0.10',
          clusterIPs: ['10.43.0.10'],
          ports: [{ name: 'dns', port: 53, protocol: 'UDP', targetPort: 'dns' }],
          selector: { 'k8s-app': 'kube-dns' },
          type: 'ClusterIP',
        },
      }),
    }
    testState.responses[planPath] = {
      capability: 'k8s.resources.update',
      target: 'cluster-a/team-a/Service/core-dns',
      ready: true,
      riskLevel: 'mutate',
      requiresApproval: false,
      changes: [{ action: 'update', resource: 'Service/core-dns', summary: 'dry-run passed' }],
      warnings: [],
    }
    const container = await renderPage(
      <NetworkServicesPage />,
      '/network/services',
      '/network/services',
    )

    expect(apiMocks.get).toHaveBeenCalledWith(
      '/clusters/cluster-a/network/services?namespace=team-a',
    )
    expect(
      container.querySelector('[data-testid="admin-table"]')?.getAttribute('data-page-size'),
    ).toBeNull()
    expect(
      container.querySelector('[data-testid="admin-table"]')?.getAttribute('data-local-sorting'),
    ).toBe('true')
    expect(
      container.querySelector('[data-testid="admin-table"]')?.getAttribute('data-viewport-scroll'),
    ).toBe('true')
    expect(container.querySelector('[data-testid="pagination-summary"]')).toBeNull()
    expect(
      container.querySelector('[data-testid="admin-table"]')?.getAttribute('data-virtual'),
    ).toBe('true')
    expect(container.textContent).toContain('NodePort: 30001')
    expect(container.textContent).toContain('Port: http · 80/TCP')
    expect(container.textContent).toContain('TargetPort: 8080')
    expect(container.querySelectorAll('.soha-service-port-arrow')).toHaveLength(2)
    expect(container.querySelectorAll('.soha-metadata-tag')).toHaveLength(5)
    expect(container.querySelectorAll('.ant-tag-gold')).toHaveLength(2)
    expect(container.querySelectorAll('.ant-tag-cyan')).toHaveLength(1)
    const input = container.querySelector(
      'input[placeholder="搜索 Service / namespace / type / port"]',
    ) as HTMLInputElement
    await act(async () => {
      setNativeInputValue(input, 'core')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
      await flush()
    })
    expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('1')
    const editButton = container.querySelector<HTMLButtonElement>(
      'button[aria-label="编辑 core-dns"]',
    )
    expect(editButton).not.toBeNull()
    await act(async () => {
      editButton?.click()
      await flush()
    })
    await act(flush)
    await act(flush)
    expect(apiMocks.get).toHaveBeenCalledWith(yamlPath)
    await act(async () => {
      await vi.waitFor(() => expect(document.querySelector('#name')).not.toBeNull())
    })
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.textContent).toContain('编辑 Service · core-dns')
    const nameInput = Array.from(dialog?.querySelectorAll<HTMLInputElement>('input') ?? []).find(
      (input) => input.value === 'core-dns',
    )
    expect(nameInput?.disabled).toBe(true)

    const nextButton = Array.from(dialog?.querySelectorAll<HTMLButtonElement>('button') ?? []).find(
      (button) => button.textContent?.includes('下一步'),
    )
    await act(async () => {
      nextButton?.click()
      await flush()
    })
    expect(dialog?.querySelector<HTMLInputElement>('input[aria-label="容器端口"]')?.value).toBe(
      'dns',
    )

    const saveButton = Array.from(dialog?.querySelectorAll<HTMLButtonElement>('button') ?? []).find(
      (button) => button.textContent?.includes('保存更改'),
    )
    await act(async () => {
      saveButton?.click()
      await flush()
    })
    await act(flush)
    expect(apiMocks.post).toHaveBeenCalledWith(
      planPath,
      expect.objectContaining({ kind: 'Service', name: 'core-dns' }),
    )
    const confirmButton = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.includes('确认更新'),
    )
    await act(async () => {
      confirmButton?.click()
      await flush()
    })
    await act(flush)
    const [, request] = apiMocks.put.mock.calls[apiMocks.put.mock.calls.length - 1] ?? []
    expect(apiMocks.put).toHaveBeenCalledWith(yamlPath, expect.any(Object))
    expect(parse(request?.content ?? '')).toMatchObject({
      metadata: { name: 'core-dns', namespace: 'team-a', resourceVersion: '12' },
      spec: {
        clusterIP: '10.43.0.10',
        clusterIPs: ['10.43.0.10'],
        ports: [{ name: 'dns', port: 53, protocol: 'UDP', targetPort: 'dns' }],
      },
    })
  })

  it('renders Ingress list from the unchanged endpoint', async () => {
    testState.responses['/clusters/cluster-a/network/ingresses?namespace=team-a'] = [
      {
        name: 'web',
        namespace: 'team-a',
        className: 'nginx',
        hosts: ['example.test'],
        address: '10.0.0.1',
        backendServices: ['web'],
        ageSeconds: 60,
        allowedActions: ['view'],
      },
    ]
    const container = await renderPage(
      <NetworkIngressesPage />,
      '/network/ingresses',
      '/network/ingresses',
    )

    expect(apiMocks.get).toHaveBeenCalledWith(
      '/clusters/cluster-a/network/ingresses?namespace=team-a',
    )
    expect(container.textContent).toContain('example.test')
    expect(container.textContent).toContain('web')
  })

  it('enables Service backend pods, metrics, events, and YAML only on their tabs', async () => {
    const detailPath = '/clusters/cluster-a/network/services/api/detail?namespace=team-a'
    const metricsPath = '/clusters/cluster-a/network/services/api/metrics?namespace=team-a'
    const eventsPath = '/clusters/cluster-a/events?namespace=team-a&limit=100'
    const yamlPath = '/clusters/cluster-a/network/services/api/yaml?namespace=team-a'
    testState.responses[detailPath] = {
      name: 'api',
      namespace: 'team-a',
      type: 'ClusterIP',
      clusterIp: '10.43.0.20',
      ports: ['80/TCP'],
      portMappings: [
        {
          name: 'http',
          protocol: 'TCP',
          targetPort: '8080',
          port: 80,
        },
      ],
      selector: { app: 'api' },
      ageSeconds: 60,
      backendPods: [
        {
          name: 'api-1',
          namespace: 'team-a',
          phase: 'Running',
          readyContainers: '1/1',
          restarts: 0,
          labels: { app: 'api' },
          ageSeconds: 30,
        },
      ],
      endpoints: [],
    }
    testState.responses[metricsPath] = { rangeMinutes: 60 }
    testState.responses[eventsPath] = []
    testState.responses[yamlPath] = {
      kind: 'Service',
      name: 'api',
      namespace: 'team-a',
      content: 'kind: Service',
    }
    const container = await renderPage(
      <ServiceDetailPage />,
      '/network/services/api?namespace=team-a',
      '/network/services/:serviceName',
    )
    const requested = () => apiMocks.get.mock.calls.map(([path]) => path as string)

    expect(requested()).toEqual([detailPath])
    expect(container.textContent).toContain('api-1')
    expect(container.textContent).toContain('Port: http · 80/TCP')
    expect(container.textContent).toContain('TargetPort: 8080')
    expect(requested()).not.toContain(metricsPath)
    expect(requested()).not.toContain(eventsPath)
    expect(requested()).not.toContain(yamlPath)

    await clickTab(container, '指标')
    expect(requested()).toContain(metricsPath)
    await clickTab(container, '事件')
    expect(requested()).toContain(eventsPath)
    await clickTab(container, 'YAML')
    expect(requested()).toContain(yamlPath)
    await act(async () => {
      await flush()
    })
    expect(container.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
  })

  it('keeps Ingress YAML request behind the YAML tab', async () => {
    const detailPath = '/clusters/cluster-a/network/ingresses/web/detail?namespace=team-a'
    const yamlPath = '/clusters/cluster-a/network/ingresses/web/yaml?namespace=team-a'
    testState.responses[detailPath] = {
      name: 'web',
      namespace: 'team-a',
      className: 'nginx',
      routes: [
        {
          host: 'example.test',
          path: '/',
          pathType: 'Prefix',
          tls: false,
          serviceName: 'web',
          servicePort: '80',
        },
      ],
      address: '10.0.0.1',
      backendServices: ['web'],
      ageSeconds: 60,
    }
    testState.responses[yamlPath] = {
      kind: 'Ingress',
      name: 'web',
      namespace: 'team-a',
      content: 'kind: Ingress',
    }
    const container = await renderPage(
      <IngressDetailPage />,
      '/network/ingresses/web?namespace=team-a',
      '/network/ingresses/:name',
    )
    const requested = () => apiMocks.get.mock.calls.map(([path]) => path as string)

    expect(requested()).toEqual([detailPath])
    await clickTab(container, 'YAML')
    expect(requested()).toContain(yamlPath)
  })
})
