/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ClusterNamespacesPage, ClusterNodesPage } from './cluster-resources-pages'

const testState = vi.hoisted(() => ({
  responses: {} as Record<string, unknown>,
  scope: {
    clusterId: null as string | null,
    namespace: null as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/i18n', () => ({
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: vi.fn((path: string) => Promise.resolve({ data: testState.responses[path] ?? [] })),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

vi.mock('@/components/platform-cluster-scope-hint', () => ({
  PlatformClusterScopeHint: () => <div data-testid="scope-hint">scope-hint</div>,
}))

vi.mock('@/features/platform/node-resource-utils', () => ({
  NodeResourcePanel: () => <div data-testid="node-resource-panel">node-resource-panel</div>,
  parseStringMap: () => ({}),
  parseTaints: () => [],
  stringifyMap: () => '{}',
  stringifyTaints: () => '[]',
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({ dataSource }: { dataSource?: unknown[] }) => (
    <div data-testid="admin-table">{dataSource?.length ?? 0}</div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

async function renderWithProviders(node: ReactNode) {
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
  })

  return container
}

describe('cluster resource pages', () => {
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
    testState.scope.clusterId = null
    testState.scope.namespace = null
    testState.responses = {}
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

  it('shows the select-cluster state on the nodes page without a page-level scope bar', async () => {
    const container = await renderWithProviders(<ClusterNodesPage />)

    expect(container.querySelector('[data-testid="resource-workspace-scopebar"]')).toBeNull()
    expect(container.textContent).toContain('Please select a cluster')
  })

  it('shows the select-cluster state on the namespaces page without a page-level scope bar', async () => {
    const container = await renderWithProviders(<ClusterNamespacesPage />)

    expect(container.querySelector('[data-testid="resource-workspace-scopebar"]')).toBeNull()
    expect(container.textContent).toContain('Select a cluster')
  })
})
