/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/i18n'
import { CRDPage } from './extensions-pages'

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
      const payload = testState.responses[path]
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

vi.mock('@/components/page-header', () => ({
  PageHeader: () => <div data-testid="page-header">page-header</div>,
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    title,
    toolbar,
  }: {
    columns: Array<Record<string, any>>
    dataSource: Array<Record<string, any>>
    title?: ReactNode
    toolbar?: ReactNode
  }) => (
    <div data-testid="admin-table">
      {title ? <div data-testid="table-title">{title}</div> : null}
      {toolbar ? <div data-testid="table-toolbar">{toolbar}</div> : null}
      <div data-testid="column-titles">
        {columns.map((column, index) => (
          <div key={`title-${index}`} data-testid={`column-title-${index}`}>
            {column.title}
          </div>
        ))}
      </div>
      {dataSource.map((record, rowIndex) => (
        <div key={`${record.group}-${rowIndex}`} data-testid={`row-${rowIndex}`}>
          {columns.map((column, columnIndex) => {
            const dataIndex = typeof column.dataIndex === 'string' ? column.dataIndex : undefined
            const value = dataIndex ? record[dataIndex] : undefined
            const content = typeof column.render === 'function' ? column.render(value, record, rowIndex) : value
            return (
              <div key={`${record.group}-${columnIndex}`} data-testid={`cell-${rowIndex}-${columnIndex}`}>
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

  it('renders the CRD catalog title inside the table shell and separates api group, crd names, and kind count columns', async () => {
    const container = await renderWithProviders(<CRDPage />)

    expect(container.querySelector('[data-testid="page-header"]')).toBeNull()
    expect(container.textContent).toContain('CustomResourceDefinitions')
    expect(container.textContent).toContain('scope-toolbar')
    expect(container.textContent).toContain('scope-hint')

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
})
