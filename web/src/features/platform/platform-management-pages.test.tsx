/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  PlatformAccessControlClusterRoleBindingsPage,
  PlatformAccessControlClusterRolesPage,
  PlatformAccessControlRoleBindingsPage,
  PlatformAccessControlServiceAccountsPage,
} from './platform-management-pages'

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

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    empty,
    title,
    toolbar,
    toolbarExtra,
  }: {
    columns: Array<Record<string, any>>
    dataSource: Array<Record<string, any>>
    empty?: ReactNode
    title?: ReactNode
    toolbar?: ReactNode
    toolbarExtra?: ReactNode
  }) => (
    <div data-testid="admin-table">
      {title ? <div data-testid="table-title">{title}</div> : null}
      {toolbar ? <div data-testid="toolbar">{toolbar}</div> : null}
      {toolbarExtra ? <div data-testid="toolbar-extra">{toolbarExtra}</div> : null}
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

async function renderWithProviders(node: ReactNode, route = '/platform-access-control/serviceaccounts') {
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

describe('platform RBAC list pages', () => {
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

  it('shows the select-cluster empty state when no cluster is selected', async () => {
    testState.scope.clusterId = null
    testState.scope.namespace = null

    const container = await renderWithProviders(<PlatformAccessControlServiceAccountsPage />)

    expect(container.textContent).toContain('请选择集群查看 RBAC 资源。')
  })

  it('does not render the cluster-scoped namespace hint for cluster role pages', async () => {
    setResponses({
      '/clusters/cluster-a/access-control/clusterroles?namespace=team-a': [
        { name: 'viewer', rules: 3, aggregationRules: 0, ageSeconds: 60, allowedActions: ['view'] },
      ],
    })

    const container = await renderWithProviders(<PlatformAccessControlClusterRolesPage />, '/platform-access-control/clusterroles')

    expect(container.textContent).not.toContain('命名空间筛选不会影响结果')
  })

  it('filters role bindings by local search input', async () => {
    setResponses({
      '/clusters/cluster-a/access-control/rolebindings?namespace=team-a': [
        { name: 'viewer-binding', namespace: 'team-a', roleRef: 'Role/viewer', subjects: ['User:alice'], ageSeconds: 60, allowedActions: ['view'] },
        { name: 'editor-binding', namespace: 'team-a', roleRef: 'Role/editor', subjects: ['User:bob'], ageSeconds: 120, allowedActions: ['view'] },
      ],
    })

    const container = await renderWithProviders(<PlatformAccessControlRoleBindingsPage />, '/platform-access-control/rolebindings')
    const input = container.querySelector('input[placeholder="搜索 RoleBindings"]') as HTMLInputElement | null
    if (!input) {
      throw new Error('search input not found')
    }

    await act(async () => {
      setNativeInputValue(input, 'viewer')
      input.dispatchEvent(new Event('input', { bubbles: true }))
      input.dispatchEvent(new Event('change', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('1')
    expect(container.textContent).toContain('viewer-binding')
    expect(container.textContent).not.toContain('editor-binding')
  })

  it('renders the RBAC table title inside the panel header', async () => {
    const container = await renderWithProviders(<PlatformAccessControlServiceAccountsPage />)

    expect(container.querySelector('[data-testid="table-title"]')?.textContent).toContain('ServiceAccounts')
    expect(container.textContent).toContain('按当前集群与命名空间范围审查 ServiceAccounts 资源关系。')
  })

  it('shows subject chips with overflow summary', async () => {
    setResponses({
      '/clusters/cluster-a/access-control/rolebindings?namespace=team-a': [
        {
          name: 'viewer-binding',
          namespace: 'team-a',
          roleRef: 'Role/viewer',
          subjects: ['User:alice', 'Group:platform', 'ServiceAccount:team-a/viewer'],
          ageSeconds: 60,
          allowedActions: ['view'],
        },
      ],
    })

    const container = await renderWithProviders(<PlatformAccessControlRoleBindingsPage />, '/platform-access-control/rolebindings')

    expect(container.textContent).toContain('User alice')
    expect(container.textContent).toContain('Group platform')
    expect(container.textContent).toContain('+1')
  })

  it('hides destructive actions when delete is not allowed', async () => {
    setResponses({
      '/clusters/cluster-a/access-control/clusterrolebindings?namespace=team-a': [
        { name: 'viewer-binding', roleRef: 'ClusterRole/viewer', subjects: ['User:alice'], ageSeconds: 60, allowedActions: ['view'] },
      ],
    })

    const container = await renderWithProviders(<PlatformAccessControlClusterRoleBindingsPage />, '/platform-access-control/clusterrolebindings')

    expect(container.textContent).not.toContain('删除')
    expect(container.querySelector('button[aria-label="删除"]')).toBeNull()
  })
})
