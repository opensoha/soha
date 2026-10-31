/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import type { ReactNode } from 'react'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { PlatformAccessControlClusterRolesPage } from './clusterroles/list-page'
import { PlatformAccessControlRoleBindingsPage } from './rolebindings/list-page'
import { PlatformAccessControlRoleDetailPage } from './roles/detail-page'
import { PlatformAccessControlServiceAccountDetailPage } from './serviceaccounts/detail-page'
import { PlatformAccessControlServiceAccountsPage } from './serviceaccounts/list-page'

const testState = vi.hoisted(() => ({
  capability: {
    disabled: false,
    isLoading: false,
    reason: '',
  },
  responses: {} as Record<string, unknown>,
  scope: {
    clusterId: 'cluster-a' as string | null,
    namespace: 'team-a' as string | null,
  },
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => ({ data: testState.responses[path] ?? [] })),
)

vi.mock('@/services/api-client', () => ({
  api: {
    delete: vi.fn(async () => ({ data: null })),
    get: apiGetMock,
    post: vi.fn(async () => ({ data: { content: '' } })),
    put: vi.fn(async () => ({ data: { content: '' } })),
  },
}))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/features/platform/cluster-capabilities', () => ({
  useClusterCapability: () => ({
    ...testState.capability,
    notes: [],
    requiredScopes: [],
    requiresApproval: false,
    status: testState.capability.disabled ? 'unsupported' : 'available',
  }),
}))

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  localeText: (localeCode: string, chinese: string, english: string) =>
    localeCode === 'zh_CN' ? chinese : english,
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))

vi.mock('@/components/status-tag', async (original) => ({
  ...(await original<typeof import('@/components/status-tag')>()),
  BooleanTag: ({ value }: { value: boolean }) => <span>{String(value)}</span>,
}))

vi.mock('@/components/k8s-yaml-editor', () => ({
  K8sYamlEditor: () => <div data-testid="yaml-editor">yaml-editor</div>,
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns,
    dataSource,
    empty,
    headerExtra,
    paginationSummary,
  }: {
    columns: Array<Record<string, any>>
    dataSource: Array<Record<string, any>>
    empty?: ReactNode
    headerExtra?: ReactNode
    paginationSummary?: ReactNode
  }) => (
    <div data-testid="admin-table">
      {headerExtra ? <div data-testid="header-extra">{headerExtra}</div> : null}
      {paginationSummary ? <div data-testid="pagination-summary">{paginationSummary}</div> : null}
      <div data-testid="row-count">{dataSource.length}</div>
      {dataSource.length === 0 ? <div>{empty}</div> : null}
      {dataSource.map((record, rowIndex) => (
        <div key={`${record.namespace ?? ''}/${record.name}`} data-testid={`row-${rowIndex}`}>
          {columns.map((column, columnIndex) => {
            const value =
              typeof column.dataIndex === 'string' ? record[column.dataIndex] : undefined
            const content =
              typeof column.render === 'function' ? column.render(value, record, rowIndex) : value
            return <div key={columnIndex}>{content == null ? '' : content}</div>
          })}
        </div>
      ))}
    </div>
  ),
}))

const mountedRoots: Root[] = []

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
  vi.clearAllMocks()
  testState.scope.clusterId = 'cluster-a'
  testState.scope.namespace = 'team-a'
  testState.responses = {}
  testState.capability.disabled = false
  testState.capability.isLoading = false
  testState.capability.reason = ''
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderPage(node: ReactNode, route: string, routePath?: string) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <AntdApp>
          <MemoryRouter initialEntries={[route]}>
            {routePath ? (
              <Routes>
                <Route path={routePath} element={node} />
              </Routes>
            ) : (
              node
            )}
          </MemoryRouter>
        </AntdApp>
      </QueryClientProvider>,
    )
  })
  await flushAsyncWork()
  await flushAsyncWork()
  return container
}

async function clickTab(container: HTMLElement, label: string) {
  const tab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find((item) =>
    item.textContent?.includes(label),
  )
  expect(tab).toBeDefined()
  await act(async () => tab?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
  await flushAsyncWork()
}

describe('platform access-control leaf pages', () => {
  it('keeps namespaced list controls and canonical wire paths', async () => {
    testState.responses['/clusters/cluster-a/access-control/serviceaccounts?namespace=team-a'] = [
      {
        name: 'builder',
        namespace: 'team-a',
        secrets: 2,
        imagePullSecrets: 1,
        automountServiceAccountToken: true,
        ageSeconds: 60,
        allowedActions: ['delete'],
      },
    ]

    const container = await renderPage(
      <PlatformAccessControlServiceAccountsPage />,
      '/platform-access-control/serviceaccounts',
    )

    expect(container.textContent).toContain('builder')
    expect(container.querySelector('input[placeholder="搜索 ServiceAccounts"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="pagination-summary"]')).toBeNull()
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/access-control/serviceaccounts?namespace=team-a',
    )
  })

  it('removes namespace from cluster-scoped list wires', async () => {
    testState.responses['/clusters/cluster-a/access-control/clusterroles'] = [
      { name: 'viewer', rules: 2, aggregationRules: 0, ageSeconds: 60 },
    ]

    const container = await renderPage(
      <PlatformAccessControlClusterRolesPage />,
      '/platform-access-control/clusterroles',
    )

    expect(container.textContent).toContain('viewer')
    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/access-control/clusterroles')
    expect(apiGetMock).not.toHaveBeenCalledWith(
      '/clusters/cluster-a/access-control/clusterroles?namespace=team-a',
    )
  })

  it('renders RoleBinding role without list-only subject details', async () => {
    testState.responses['/clusters/cluster-a/access-control/rolebindings?namespace=team-a'] = [
      {
        name: 'readers',
        namespace: 'team-a',
        roleRef: 'Role/reader',
        ageSeconds: 60,
      },
    ]

    const container = await renderPage(
      <PlatformAccessControlRoleBindingsPage />,
      '/platform-access-control/rolebindings',
    )

    expect(container.textContent).toContain('Role/reader')
    expect(container.textContent).not.toContain('Subjects')
  })

  it('loads detail, reverse relationships, and YAML only from their active tabs', async () => {
    testState.responses['/clusters/cluster-a/access-control/roles/reader/detail?namespace=team-a'] =
      {
        name: 'reader',
        namespace: 'team-a',
        rules: 1,
        ruleSummaries: ['get pods'],
        ageSeconds: 60,
      }
    testState.responses['/clusters/cluster-a/access-control/rolebindings?namespace=team-a'] = [
      {
        name: 'reader-binding',
        namespace: 'team-a',
        roleRef: 'Role/reader',
        subjects: ['User:alice'],
        ageSeconds: 60,
      },
    ]
    testState.responses['/clusters/cluster-a/access-control/clusterrolebindings'] = []
    testState.responses['/clusters/cluster-a/access-control/roles/reader/yaml?namespace=team-a'] = {
      content: 'kind: Role',
    }

    const container = await renderPage(
      <PlatformAccessControlRoleDetailPage />,
      '/platform-access-control/roles/reader?namespace=url-team',
      '/platform-access-control/roles/:name',
    )
    const requestedPaths = () => apiGetMock.mock.calls.map(([path]) => String(path))

    expect(container.textContent).toContain('get pods')
    expect(requestedPaths()).toEqual([
      '/clusters/cluster-a/access-control/roles/reader/detail?namespace=team-a',
    ])
    await clickTab(container, '关联资源')
    expect(container.textContent).toContain('RoleBinding team-a/reader-binding')
    expect(requestedPaths()).toContain(
      '/clusters/cluster-a/access-control/rolebindings?namespace=team-a',
    )
    expect(requestedPaths()).toContain('/clusters/cluster-a/access-control/clusterrolebindings')
    expect(requestedPaths()).not.toContain(
      '/clusters/cluster-a/access-control/roles/reader/yaml?namespace=team-a',
    )
    await clickTab(container, 'YAML')
    expect(requestedPaths()).toContain(
      '/clusters/cluster-a/access-control/roles/reader/yaml?namespace=team-a',
    )
    expect(container.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
  })

  it('loads ServiceAccount references through server-side subject filters', async () => {
    testState.responses[
      '/clusters/cluster-a/access-control/serviceaccounts/builder/detail?namespace=team-a'
    ] = {
      name: 'builder',
      namespace: 'team-a',
      ageSeconds: 60,
    }
    testState.responses[
      '/clusters/cluster-a/access-control/rolebindings?namespace=team-a&subjectKind=ServiceAccount&subjectName=builder&subjectNamespace=team-a'
    ] = [{ name: 'builder-role', namespace: 'team-a', roleRef: 'Role/editor', ageSeconds: 60 }]
    testState.responses[
      '/clusters/cluster-a/access-control/clusterrolebindings?subjectKind=ServiceAccount&subjectName=builder&subjectNamespace=team-a'
    ] = [{ name: 'builder-cluster-role', roleRef: 'ClusterRole/viewer', ageSeconds: 60 }]

    const container = await renderPage(
      <PlatformAccessControlServiceAccountDetailPage />,
      '/platform-access-control/serviceaccounts/builder',
      '/platform-access-control/serviceaccounts/:name',
    )
    await clickTab(container, '关联资源')

    expect(container.textContent).toContain('RoleBinding team-a/builder-role')
    expect(container.textContent).toContain('ClusterRoleBinding builder-cluster-role')
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/access-control/rolebindings?namespace=team-a&subjectKind=ServiceAccount&subjectName=builder&subjectNamespace=team-a',
    )
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/access-control/clusterrolebindings?subjectKind=ServiceAccount&subjectName=builder&subjectNamespace=team-a',
    )
  })

  it('blocks list mutations and YAML fetch when the capability is unsupported', async () => {
    testState.capability.disabled = true
    testState.capability.reason = 'Agent mode cannot apply RBAC YAML'
    testState.responses['/clusters/cluster-a/access-control/serviceaccounts?namespace=team-a'] = [
      {
        name: 'builder',
        namespace: 'team-a',
        secrets: 0,
        imagePullSecrets: 0,
        automountServiceAccountToken: true,
        ageSeconds: 60,
        allowedActions: ['delete'],
      },
    ]
    const list = await renderPage(
      <PlatformAccessControlServiceAccountsPage />,
      '/platform-access-control/serviceaccounts',
    )
    const createButton = Array.from(list.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.includes('新增'),
    )
    expect(list.textContent).toContain('Agent mode cannot apply RBAC YAML')
    expect(createButton?.disabled).toBe(true)
    expect(list.querySelector('button[aria-label="删除"]')).toBeNull()

    testState.responses['/clusters/cluster-a/access-control/roles/reader/detail?namespace=team-a'] =
      {
        name: 'reader',
        namespace: 'team-a',
        rules: 1,
        ageSeconds: 60,
      }
    const detail = await renderPage(
      <PlatformAccessControlRoleDetailPage />,
      '/platform-access-control/roles/reader',
      '/platform-access-control/roles/:name',
    )
    await clickTab(detail, 'YAML')

    expect(detail.textContent).toContain('Agent mode cannot apply RBAC YAML')
    expect(apiGetMock.mock.calls.map(([path]) => path)).not.toContain(
      '/clusters/cluster-a/access-control/roles/reader/yaml?namespace=team-a',
    )
  })
})
