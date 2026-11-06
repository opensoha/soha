/** @vitest-environment jsdom */

import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import type { ReactNode } from 'react'
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/services/api-client'
import { PlatformAccessControlClusterRoleBindingDetailPage } from './clusterrolebindings/detail-page'
import { PlatformAccessControlClusterRoleDetailPage } from './clusterroles/detail-page'
import { PlatformAccessControlRoleBindingDetailPage } from './rolebindings/detail-page'
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
    post: vi.fn(async () => ({
      data: {
        allowed: !testState.capability.disabled,
        capability: {
          status: testState.capability.disabled ? 'unsupported' : 'available',
          reason: testState.capability.reason,
        },
      },
    })),
    put: vi.fn(async () => ({ data: { content: '' } })),
  },
}))

vi.mock('@/features/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: [
          'roles',
          'cluster-roles',
          'role-bindings',
          'cluster-role-bindings',
          'service-accounts',
        ].flatMap((resource) =>
          ['view', 'create', 'update', 'delete'].map(
            (action) => `platform.access-control.${resource}.${action}`,
          ),
        ),
        visibleMenuIds: [],
      },
    },
    error: null,
    isPending: false,
  }),
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
  K8sYamlEditor: ({
    readOnly,
    applyDisabled,
    value,
    onApply,
  }: {
    readOnly: boolean
    applyDisabled: boolean
    value: string
    onApply: () => void
  }) => (
    <div data-testid="yaml-editor" data-read-only={String(readOnly)}>
      <pre>{value}</pre>
      <button disabled={readOnly || applyDisabled} onClick={onApply}>
        应用 YAML
      </button>
    </div>
  ),
}))

vi.mock('@/features/platform/shared/resource-insights/workload-relationship-canvas', () => ({
  default: ({
    graph,
    onSelect,
  }: {
    graph: KubernetesResourceGraph
    onSelect: (id: string) => void
  }) => (
    <div>
      {graph.nodes.map(({ id, resource }) => (
        <button key={id} data-id={id} onClick={() => onSelect(id)}>
          {resource.name}
        </button>
      ))}
    </div>
  ),
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
  it.each([
    ['Role', 'roles', true, PlatformAccessControlRoleDetailPage],
    ['ClusterRole', 'clusterroles', false, PlatformAccessControlClusterRoleDetailPage],
    ['RoleBinding', 'rolebindings', true, PlatformAccessControlRoleBindingDetailPage],
    [
      'ClusterRoleBinding',
      'clusterrolebindings',
      false,
      PlatformAccessControlClusterRoleBindingDetailPage,
    ],
    ['ServiceAccount', 'serviceaccounts', true, PlatformAccessControlServiceAccountDetailPage],
  ] as const)(
    'mounts %s graph only on the relationships tab with exact scope',
    async (kind, routeSegment, namespaced, DetailPage) => {
      const clusterId = 'cluster-a'
      const name = 'reader'
      const namespace = namespaced ? 'team-b' : undefined
      const namespaceQuery = namespace ? `namespace=${namespace}` : ''
      const querySuffix = namespaceQuery ? `?${namespaceQuery}` : ''
      const apiPath = `/clusters/${clusterId}/access-control/${routeSegment}/${name}`
      testState.responses[`${apiPath}/detail${querySuffix}`] = {
        name,
        namespace,
        ageSeconds: 60,
        rules: 0,
        roleRef: 'Role/target-role',
        secrets: 0,
        imagePullSecrets: 0,
      }
      const apiVersion = kind === 'ServiceAccount' ? 'v1' : 'rbac.authorization.k8s.io/v1'
      testState.responses[`${apiPath}/yaml${querySuffix}`] = {
        content: [
          `apiVersion: ${apiVersion}`,
          `kind: ${kind}`,
          'metadata:',
          `  name: ${name}`,
          ...(namespace ? [`  namespace: ${namespace}`] : []),
        ].join('\n'),
      }
      const graphQuery = new URLSearchParams(namespaceQuery)
      graphQuery.set('kind', kind)
      graphQuery.set('name', name)
      const graphPath = `/clusters/${clusterId}/resources/graph?${graphQuery}`
      const graph: KubernetesResourceGraph = {
        clusterId,
        rootId: 'root',
        generatedAt: '',
        warnings: [],
        evidence: [],
        nodes: [
          {
            id: 'root',
            resource: {
              clusterId,
              namespace,
              kind,
              name,
              apiVersion,
              scopeMode: namespaced ? 'namespace' : 'cluster',
            },
          },
        ],
        edges: [],
      }
      if (kind === 'RoleBinding') {
        graph.nodes.push({
          id: 'target',
          resource: {
            clusterId,
            namespace,
            kind: 'Role',
            name: 'target-role',
            apiVersion,
            scopeMode: 'namespace',
          },
        })
        graph.edges.push({
          id: 'role-ref',
          sourceId: 'root',
          targetId: 'target',
          relation: 'binds-role',
          fieldPath: 'roleRef',
        })
      }
      testState.responses[graphPath] = graph

      const baseRoute = `/platform-access-control/${routeSegment}`
      const container = await renderPage(
        <DetailPage />,
        `${baseRoute}/${name}?clusterId=${clusterId}&namespace=team-b`,
        `${baseRoute}/:name`,
      )
      expect(apiGetMock.mock.calls.map(([path]) => path)).not.toContain(graphPath)
      await clickTab(container, '关系')
      await act(async () => {
        await import('@/features/platform/shared/resource-insights/workload-relationships')
        await import('@/features/platform/shared/resource-insights/workload-relationship-canvas')
      })
      await vi.waitFor(async () => {
        await flushAsyncWork()
        expect(apiGetMock).toHaveBeenCalledWith(graphPath)
        expect(container.querySelector('[data-id="root"]')).not.toBeNull()
      })
      if (kind === 'RoleBinding') {
        const target = container.querySelector<HTMLButtonElement>('[data-id="target"]')
        expect(target).not.toBeNull()
        await act(async () => target?.click())
        const inspector = container.querySelector('aside[aria-label="选中资源信息"]')
        expect(inspector?.textContent).toContain('被声明为绑定角色')
        expect(inspector?.textContent).toContain('roleRef')
        expect(
          [...(inspector?.querySelectorAll('button') ?? [])].find(
            (button) => button.textContent === '查看详情',
          )?.disabled,
        ).toBe(false)
      }
      await clickTab(container, '概览')
      expect(container.querySelector('.soha-workload-relationships')).toBeNull()
    },
  )

  it('does not request a Role graph for a detail URL without a namespace', async () => {
    const container = await renderPage(
      <PlatformAccessControlRoleDetailPage />,
      '/platform-access-control/roles/reader?clusterId=cluster-a&namespace=&tab=relationships',
      '/platform-access-control/roles/:name',
    )
    expect(container.textContent).toContain('请选择集群和命名空间')
    expect(apiGetMock.mock.calls.some(([path]) => path.includes('/resources/graph'))).toBe(false)
  })

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

  it('loads overview configuration but mounts bindings and YAML editor only on their tabs', async () => {
    testState.scope.namespace = 'list-team'
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
      { name: 'other-role', namespace: 'team-a', roleRef: 'Role/other', ageSeconds: 60 },
      { name: 'cluster-ref', namespace: 'team-a', roleRef: 'ClusterRole/reader', ageSeconds: 60 },
      { name: 'other-namespace', namespace: 'team-b', roleRef: 'Role/reader', ageSeconds: 60 },
    ]
    testState.responses['/clusters/cluster-a/access-control/clusterrolebindings'] = []
    testState.responses['/clusters/cluster-a/access-control/roles/reader/yaml?namespace=team-a'] = {
      content: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: reader
  namespace: team-a
rules:
  - apiGroups: ['']
    resources: [pods]
    verbs: [get]
`,
    }

    const container = await renderPage(
      <PlatformAccessControlRoleDetailPage />,
      '/platform-access-control/roles/reader?clusterId=cluster-a&namespace=team-a',
      '/platform-access-control/roles/:name',
    )
    const requestedPaths = () => apiGetMock.mock.calls.map(([path]) => String(path))

    expect(container.textContent).toContain('pods')
    expect(container.textContent).toContain('get')
    expect(requestedPaths()).toEqual([
      '/clusters/cluster-a/access-control/roles/reader/detail?namespace=team-a',
      '/clusters/cluster-a/access-control/roles/reader/yaml?namespace=team-a',
    ])
    expect(container.querySelector('[data-testid="yaml-editor"]')).toBeNull()
    await clickTab(container, '绑定')
    expect(container.textContent).toContain('reader-binding')
    expect(container.querySelector('[data-testid="row-count"]')?.textContent).toBe('1')
    expect(container.textContent).not.toContain('other-role')
    expect(container.textContent).not.toContain('cluster-ref')
    expect(container.textContent).not.toContain('other-namespace')
    expect(
      container.querySelector(
        'a[href="/platform-access-control/rolebindings/reader-binding?clusterId=cluster-a&namespace=team-a"]',
      ),
    ).not.toBeNull()
    expect(requestedPaths()).toContain(
      '/clusters/cluster-a/access-control/rolebindings?namespace=team-a',
    )
    expect(requestedPaths()).not.toContain('/clusters/cluster-a/access-control/clusterrolebindings')
    expect(container.querySelector('[data-testid="yaml-editor"]')).toBeNull()
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
      '/clusters/cluster-a/access-control/serviceaccounts/builder/yaml?namespace=team-a'
    ] = {
      content: `apiVersion: v1
kind: ServiceAccount
metadata:
  name: builder
  namespace: team-a
automountServiceAccountToken: false
`,
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
    expect(container.querySelector('[data-testid="yaml-editor"]')).toBeNull()
    expect(apiGetMock).not.toHaveBeenCalledWith(
      '/clusters/cluster-a/access-control/rolebindings?namespace=team-a&subjectKind=ServiceAccount&subjectName=builder&subjectNamespace=team-a',
    )
    await clickTab(container, '绑定')

    expect(container.textContent).toContain('builder-role')
    expect(container.textContent).toContain('builder-cluster-role')
    expect(
      container.querySelector(
        'a[href="/platform-access-control/rolebindings/builder-role?clusterId=cluster-a&namespace=team-a"]',
      ),
    ).not.toBeNull()
    expect(
      container.querySelector(
        'a[href="/platform-access-control/clusterrolebindings/builder-cluster-role?clusterId=cluster-a"]',
      ),
    ).not.toBeNull()
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/access-control/rolebindings?namespace=team-a&subjectKind=ServiceAccount&subjectName=builder&subjectNamespace=team-a',
    )
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/access-control/clusterrolebindings?subjectKind=ServiceAccount&subjectName=builder&subjectNamespace=team-a',
    )
  })

  it('blocks list mutations and YAML apply while retaining YAML read when unsupported', async () => {
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
    const actions = list.querySelector<HTMLButtonElement>('button[aria-label="更多操作：builder"]')
    expect(actions).not.toBeNull()
    await act(async () => actions?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()
    const deleteItem = Array.from(document.querySelectorAll('[role="menuitem"]')).find((item) =>
      item.textContent?.includes('删除'),
    )
    expect(deleteItem?.getAttribute('aria-disabled')).toBe('true')
    await act(async () => deleteItem?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    expect(api.delete).not.toHaveBeenCalled()

    testState.responses['/clusters/cluster-a/access-control/roles/reader/detail?namespace=team-a'] =
      {
        name: 'reader',
        namespace: 'team-a',
        rules: 1,
        ageSeconds: 60,
        allowedActions: ['update'],
      }
    testState.responses['/clusters/cluster-a/access-control/roles/reader/yaml?namespace=team-a'] = {
      content: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: reader
  namespace: team-a
rules: []
`,
    }
    const detail = await renderPage(
      <PlatformAccessControlRoleDetailPage />,
      '/platform-access-control/roles/reader',
      '/platform-access-control/roles/:name',
    )
    await clickTab(detail, 'YAML')

    expect(detail.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
    expect(
      detail.querySelector('[data-testid="yaml-editor"]')?.getAttribute('data-read-only'),
    ).toBe('false')
    const applyButton = Array.from(detail.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent === '应用 YAML',
    )
    expect(applyButton?.disabled).toBe(true)
    await act(async () => applyButton?.click())
    expect(api.put).not.toHaveBeenCalled()
    expect(apiGetMock.mock.calls.map(([path]) => path)).toContain(
      '/clusters/cluster-a/access-control/roles/reader/yaml?namespace=team-a',
    )
  })
})
