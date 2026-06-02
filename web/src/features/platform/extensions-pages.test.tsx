/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/i18n'
import { CRDPage, HelmChartsPage, HelmReleasesPage } from './extensions-pages'

const testState = vi.hoisted(() => ({
  normalizePath: (path: string) => {
    const [pathname, rawQuery] = path.split('?')
    if (!rawQuery) return path
    const params = new URLSearchParams(rawQuery)
    params.sort()
    const query = params.toString()
    return query ? `${pathname}?${query}` : pathname
  },
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
      const responseKey = path in testState.responses ? path : testState.normalizePath(path)
      if (!(responseKey in testState.responses)) {
        return Promise.resolve({ data: [] })
      }
      const payload = testState.responses[responseKey]
      if (payload instanceof Error) {
        return Promise.reject(payload)
      }
      return Promise.resolve({ data: payload })
    }),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

vi.mock('@/components/platform-scope-toolbar', () => ({
  PlatformScopeToolbar: () => <div data-testid="scope-toolbar">scope-toolbar</div>,
}))

vi.mock('@/components/platform-cluster-scope-hint', () => ({
  PlatformClusterScopeHint: () => <div data-testid="scope-hint">scope-hint</div>,
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    empty,
    headerExtra,
    onRow,
    paginationSummary,
    title,
    toolbar,
    toolbarExtra,
  }: {
    columns: Array<Record<string, any>>
    dataSource: Array<Record<string, any>>
    empty?: ReactNode
    headerExtra?: ReactNode
    onRow?: (record: Record<string, any>, index: number) => Record<string, any>
    paginationSummary?: ReactNode | ((total: number, range: [number, number]) => ReactNode)
    title?: ReactNode
    toolbar?: ReactNode
    toolbarExtra?: ReactNode
  }) => (
    <div data-testid="admin-table">
      {title ? <div data-testid="table-title">{title}</div> : null}
      {headerExtra ? <div data-testid="header-extra">{headerExtra}</div> : null}
      {toolbar ? <div data-testid="table-toolbar">{toolbar}</div> : null}
      {toolbarExtra ? <div data-testid="toolbar-extra">{toolbarExtra}</div> : null}
      {paginationSummary ? (
        <div data-testid="pagination-summary">
          {typeof paginationSummary === 'function' ? paginationSummary(dataSource.length, [1, dataSource.length]) : paginationSummary}
        </div>
      ) : null}
      <div data-testid="row-count">{dataSource.length}</div>
      {dataSource.length === 0 ? <div data-testid="empty">{empty}</div> : null}
      <div data-testid="column-titles">
        {columns.map((column, index) => (
          <div key={`title-${index}`} data-testid={`column-title-${index}`}>
            {column.title}
          </div>
        ))}
      </div>
      {dataSource.map((record, rowIndex) => (
        <div
          key={`${record.group || record.name || 'row'}-${rowIndex}`}
          data-testid={`row-${rowIndex}`}
          onClick={onRow?.(record, rowIndex)?.onClick}
        >
          {columns.map((column, columnIndex) => {
            const dataIndex = typeof column.dataIndex === 'string' ? column.dataIndex : undefined
            const value = dataIndex ? record[dataIndex] : undefined
            const content = typeof column.render === 'function' ? column.render(value, record, rowIndex) : value
            return (
              <div key={`${record.group || record.name || 'row'}-${columnIndex}`} data-testid={`cell-${rowIndex}-${columnIndex}`}>
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
  testState.responses = { ...responses }
  Object.entries(responses).forEach(([path, payload]) => {
    testState.responses[testState.normalizePath(path)] = payload
  })
}

function setNativeInputValue(element: HTMLInputElement, value: string) {
  const prototype = Object.getPrototypeOf(element)
  const descriptor = Object.getOwnPropertyDescriptor(prototype, 'value')
  descriptor?.set?.call(element, value)
}

async function renderWithProviders(node: ReactNode, route = '/extensions') {
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
          <I18nProvider>
            <MemoryRouter
              initialEntries={[route]}
              future={{
                v7_startTransition: true,
                v7_relativeSplatPath: true,
              }}
            >
              {node}
            </MemoryRouter>
          </I18nProvider>
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

describe('CRD catalog page', () => {
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
    Object.defineProperty(window, 'getComputedStyle', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        getPropertyValue: vi.fn(() => ''),
      })),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  beforeEach(() => {
    setResponses({
      '/clusters/cluster-a/extensions/crds': [
        {
          name: 'challenges.acme.cert-manager.io',
          group: 'acme.cert-manager.io',
          kind: 'Challenge',
          plural: 'challenges',
          version: 'v1',
          versions: ['v1'],
          scope: 'Namespaced',
        },
        {
          name: 'orders.acme.cert-manager.io',
          group: 'acme.cert-manager.io',
          kind: 'Order',
          plural: 'orders',
          version: 'v1',
          versions: ['v1'],
          scope: 'Namespaced',
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
    for (const container of containers) {
      container.remove()
    }
    containers = []
    vi.clearAllMocks()
  })

  it('keeps the CRD catalog query controls outside the table and summarizes pagination on the left', async () => {
    const container = await renderWithProviders(<CRDPage />)

    expect(container.querySelector('[data-testid="page-header"]')).toBeNull()
    expect(container.querySelector('[data-testid="table-title"]')).toBeNull()
    expect(container.querySelector('[data-testid="table-toolbar"]')).toBeNull()
    expect(container.querySelector('input[placeholder="搜索 API Group / CRD / Kind / Version"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain('当前 1 / 1 条')

    const headerButtons = Array.from(container.querySelectorAll('[data-testid="header-extra"] button')).map((button) =>
      button.textContent?.trim() || button.getAttribute('aria-label'),
    )
    expect(headerButtons).toEqual(['切换表格密度', '刷新'])

    expect(container.textContent).toContain('API Group')
    expect(container.textContent).toContain('CRD Names')
    expect(container.textContent).toContain('Kinds 数量')

    expect(container.querySelector('[data-testid="cell-0-0"]')?.textContent).toContain('acme.cert-manager.io')
    expect(container.querySelector('[data-testid="cell-0-0"]')?.textContent).not.toContain('2 个 kinds')

    const crdNamesCell = container.querySelector('[data-testid="cell-0-1"]')?.textContent ?? ''
    expect(crdNamesCell).toContain('challenges.acme.cert-manager.io')
    expect(crdNamesCell).toContain('orders.acme.cert-manager.io')

    expect(container.querySelector('[data-testid="cell-0-2"]')?.textContent).toContain('2 个')
  })

  it('keeps Helm release list filters in the query card and search results in pagination summary', async () => {
    setResponses({
      '/clusters/cluster-a/helm/releases?namespace=team-a': [
        { name: 'ingress-nginx', namespace: 'team-a', chart: 'ingress-nginx-4.12.0', revision: '3', status: 'deployed', appVersion: '1.12.0', ageSeconds: 60 },
        { name: 'cert-manager', namespace: 'cert-manager', chart: 'cert-manager-v1.16.0', revision: '1', status: 'failed', appVersion: 'v1.16.0', ageSeconds: 120 },
      ],
    })

    const container = await renderWithProviders(<HelmReleasesPage />, '/helm/releases')

    expect(container.querySelector('[data-testid="table-title"]')).toBeNull()
    expect(container.querySelector('[data-testid="table-toolbar"]')).toBeNull()
    expect(container.querySelector('input[placeholder="搜索 Release / Namespace / Chart / 状态 / 版本"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain('当前 2 / 2 条')

    const headerButtons = Array.from(container.querySelectorAll('[data-testid="header-extra"] button')).map((button) =>
      button.textContent?.trim() || button.getAttribute('aria-label'),
    )
    expect(headerButtons).toEqual(['切换表格密度', '刷新'])

    const input = container.querySelector('input[placeholder="搜索 Release / Namespace / Chart / 状态 / 版本"]') as HTMLInputElement | null
    if (!input) {
      throw new Error('helm search input not found')
    }

    await act(async () => {
      setNativeInputValue(input, 'cert')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
      await new Promise((resolve) => window.setTimeout(resolve, 0))
      await new Promise((resolve) => window.setTimeout(resolve, 20))
    })

    expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('1')
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain('当前 1 / 2 条')
    expect(container.textContent).toContain('cert-manager')
    expect(container.textContent).not.toContain('ingress-nginx')
  })

  it('renders Helm charts from the backend catalog and filters within the table shell', async () => {
    const catalog = {
      repository: {
        id: 'artifacthub',
        name: 'Artifact Hub',
        displayName: 'Artifact Hub',
        url: 'https://artifacthub.io',
      },
      source: 'artifacthub',
      refreshedAt: '2026-06-02T06:00:00Z',
      totalCount: 17043,
      loadedCount: 2,
      chartCount: 2,
      versionCount: 5,
      charts: [
        {
          packageId: 'pkg-nginx',
          name: 'nginx',
          repositoryName: 'bitnami',
          repositoryDisplay: 'Bitnami',
          repositoryUrl: 'https://charts.bitnami.com/bitnami',
          artifactHubUrl: 'https://artifacthub.io/packages/helm/bitnami/nginx',
          latestVersion: '1.2.3',
          appVersion: '1.25.0',
          description: 'nginx ingress chart',
          keywords: ['ingress', 'proxy'],
          versions: ['1.2.3', '1.2.2'],
          versionCount: 2,
          stars: 40,
          official: true,
          verifiedPublisher: true,
        },
        {
          packageId: 'pkg-prometheus',
          name: 'prometheus',
          repositoryName: 'prometheus-community',
          repositoryDisplay: 'prometheus-community',
          repositoryUrl: 'https://prometheus-community.github.io/helm-charts',
          artifactHubUrl: 'https://artifacthub.io/packages/helm/prometheus-community/prometheus',
          latestVersion: '15.0.0',
          appVersion: '2.45.0',
          description: 'monitoring chart',
          keywords: ['metrics'],
          versions: ['15.0.0', '14.0.0', '13.0.0'],
          versionCount: 3,
          stars: 100,
        },
      ],
    }
    setResponses({
      '/clusters/cluster-a/helm/charts?limit=20&offset=0': catalog,
      '/clusters/cluster-a/helm/charts?keyword=metrics&limit=20&offset=0': {
        ...catalog,
        query: 'metrics',
        totalCount: 1,
        loadedCount: 1,
        chartCount: 1,
        charts: [catalog.charts[1]],
      },
      '/clusters/cluster-a/helm/charts?limit=20&offset=0&keyword=metrics': {
        ...catalog,
        query: 'metrics',
        totalCount: 1,
        loadedCount: 1,
        chartCount: 1,
        charts: [catalog.charts[1]],
      },
      '/clusters/cluster-a/helm/charts/bitnami/nginx?version=1.2.3': {
        ...catalog.charts[0],
        readme: '# NGINX\n\nA web server chart.',
        availableVersions: [
          { version: '1.2.3', appVersion: '1.25.0' },
          { version: '1.2.2', appVersion: '1.24.0' },
        ],
        links: [{ name: 'Home', url: 'https://nginx.org' }],
      },
      '/clusters/cluster-a/helm/charts/values?packageId=pkg-nginx&name=nginx&version=1.2.3': {
        packageId: 'pkg-nginx',
        name: 'nginx',
        version: '1.2.3',
        content: 'replicaCount: 2\n',
      },
    })

    const container = await renderWithProviders(<HelmChartsPage />, '/helm/charts')

    expect(container.querySelector('[data-testid="table-title"]')).toBeNull()
    expect(container.querySelector('[data-testid="table-toolbar"]')).not.toBeNull()
    expect(container.textContent).toContain('Artifact Hub')
    expect(container.textContent).toContain('仅 Helm packages')
    expect(container.textContent).toContain('总计 17,043 个')
    expect(container.textContent).not.toContain('当前页 2 个')
    expect(container.textContent).not.toContain('版本 5 个')
    expect(container.querySelector('input[placeholder="搜索 Chart / 版本 / 描述 / 关键词 / 维护者"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain('当前 1-2 / 总计 17,043 条')

    expect(container.querySelector('[data-testid="header-extra"]')).toBeNull()
    const toolbarButtons = Array.from(container.querySelectorAll('[data-testid="toolbar-extra"] button')).map((button) =>
      button.textContent?.trim() || button.getAttribute('aria-label'),
    )
    expect(toolbarButtons).toEqual(['切换表格密度', '刷新'])

    await act(async () => {
      container.querySelector('[data-testid="row-0"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
      await new Promise((resolve) => window.setTimeout(resolve, 0))
    })

    expect(document.body.textContent).toContain('Chart: bitnami/nginx')
    const valuesTab = Array.from(document.body.querySelectorAll('[role="tab"]')).find((item) => item.textContent?.includes('Values'))
    await act(async () => {
      valuesTab?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(document.body.textContent).toContain('replicaCount: 2')

    const input = container.querySelector('input[placeholder="搜索 Chart / 版本 / 描述 / 关键词 / 维护者"]') as HTMLInputElement | null
    if (!input) {
      throw new Error('helm charts search input not found')
    }

    await act(async () => {
      setNativeInputValue(input, 'metrics')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
      await new Promise((resolve) => window.setTimeout(resolve, 0))
      await new Promise((resolve) => window.setTimeout(resolve, 20))
    })

    await act(async () => {
      await Promise.resolve()
      await new Promise((resolve) => window.setTimeout(resolve, 20))
    })

    expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('1')
    expect(container.querySelector('[data-testid="pagination-summary"]')?.textContent).toContain('当前 1-1 / 总计 1 条')
    expect(container.textContent).toContain('prometheus')
    expect(container.textContent).not.toContain('nginx ingress chart')
  })
})
