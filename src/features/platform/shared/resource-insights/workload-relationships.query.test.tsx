/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider, focusManager } from '@tanstack/react-query'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { ApiError } from '@/services/api-error'
import { useAuthStore } from '@/stores/auth-store'
import type { User } from '@/types'
import { WorkloadRelationships } from './workload-relationships'
import { resourceInsightKeys } from './keys'
const state = vi.hoisted(() => ({
  status: 403,
  response: 'valid',
  reads: [] as string[],
  storageRead: vi.fn<() => Promise<KubernetesResourceGraph>>(),
  navigate: vi.fn(),
}))
vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: 'zh_CN' }) }))
vi.mock('react-router-dom', () => ({ useNavigate: () => state.navigate }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({
    data: {
      data: { permissionKeys: ['platform.pods.view', 'platform.storage.storage-classes.view'] },
    },
    error: null,
  }),
  hasPermission: (snapshot: { permissionKeys: string[] }, permission: string) =>
    snapshot.permissionKeys.includes(permission),
}))
vi.mock('./api', () => ({
  getKubernetesResourceGraph: async (
    scope: { clusterId: string; namespace: string },
    kind: string,
    name: string,
  ) => {
    state.reads.push(`${scope.clusterId}/${scope.namespace}/${kind}/${name}`)
    if (kind === 'StorageClass') return state.storageRead()
    if (state.status) throw new ApiError(state.status, `rejected ${state.status}`)
    return {
      clusterId: state.response === 'wrong-cluster' ? 'different' : scope.clusterId,
      rootId: 'root',
      generatedAt: '',
      warnings: [],
      evidence: [],
      edges: [],
      nodes: [
        {
          id: 'root',
          resource: {
            clusterId: scope.clusterId,
            namespace: scope.namespace,
            kind,
            name,
            apiVersion:
              state.response === 'wrong-gvk'
                ? 'custom.example/v1'
                : ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'].includes(kind)
                  ? 'apps/v1'
                  : ['Job', 'CronJob'].includes(kind)
                    ? 'batch/v1'
                    : 'v1',
            scopeMode: state.response === 'wrong-scope' ? 'cluster' : 'namespace',
          },
        },
      ],
    }
  },
  getKubernetesSecurityPosture: vi.fn(),
}))
vi.mock('@/components/request-feedback', () => ({
  ReadFeedback: ({ query }: { query: { error: Error | null } }) =>
    query.error ? <div role="alert">{query.error.message}</div> : null,
}))
vi.mock('@/components/management-list', () => ({
  ManagementState: ({ title }: { title: string }) => <div>{title}</div>,
}))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    dataSource,
    columns,
  }: {
    dataSource: { id: string }[]
    columns: { key: string; render: (_: unknown, node: unknown) => ReactNode }[]
  }) => (
    <div>
      {dataSource.map((node) => (
        <div key={node.id}>
          {columns.map((column) => (
            <span key={column.key}>{column.render(null, node)}</span>
          ))}
        </div>
      ))}
    </div>
  ),
}))
vi.mock('antd', () => ({
  Card: ({
    children,
    title,
    extra,
  }: {
    children: ReactNode
    title: ReactNode
    extra: ReactNode
  }) => (
    <section>
      <h2>{title}</h2>
      {extra}
      {children}
    </section>
  ),
  Typography: { Text: ({ children }: { children: ReactNode }) => <span>{children}</span> },
  Button: ({
    children,
    onClick,
    disabled,
    ...props
  }: {
    children: ReactNode
    onClick: () => void
    disabled: boolean
    'aria-label'?: string
    'aria-pressed'?: boolean
  }) => (
    <button
      aria-label={props['aria-label']}
      aria-pressed={props['aria-pressed']}
      disabled={disabled}
      onClick={onClick}
    >
      {children}
    </button>
  ),
  Segmented: ({
    options,
    onChange,
  }: {
    options: { value: string; label: string }[]
    onChange: (value: string) => void
  }) => (
    <div>
      {options.map((option) => (
        <button key={option.value} onClick={() => onChange(option.value)}>
          {option.label}
        </button>
      ))}
    </div>
  ),
  Switch: () => null,
  Spin: () => null,
  Alert: ({ title }: { title: string }) => <div role="alert">{title}</div>,
}))
vi.mock('./workload-relationship-canvas', () => ({
  default: ({
    graph,
    onSelect,
  }: {
    graph: KubernetesResourceGraph
    onSelect: (id: string) => void
  }) => (
    <div>
      {graph.nodes.map((node) => (
        <button key={node.id} data-id={node.id} onClick={() => onSelect(node.id)}>
          {node.resource.kind} {node.resource.name}
        </button>
      ))}
    </div>
  ),
}))

const scope = { clusterId: 'a', namespace: 'ns' }
const storageScope = { clusterId: 'a', namespace: null }
const cached: KubernetesResourceGraph = {
  clusterId: 'a',
  rootId: 'root',
  generatedAt: '',
  warnings: [],
  evidence: [],
  edges: [],
  nodes: [
    {
      id: 'root',
      resource: {
        clusterId: 'a',
        namespace: 'ns',
        kind: 'Pod',
        name: 'api',
        apiVersion: 'v1',
        scopeMode: 'namespace',
      },
    },
  ],
}

function storageGraph(visibleClaim: string): KubernetesResourceGraph {
  return {
    ...cached,
    nodes: [
      {
        id: 'root',
        resource: {
          clusterId: 'a',
          kind: 'StorageClass',
          name: 'fast',
          apiVersion: 'storage.k8s.io/v1',
          scopeMode: 'cluster',
        },
      },
      {
        id: 'claim',
        resource: {
          clusterId: 'a',
          namespace: 'ns',
          kind: 'PersistentVolumeClaim',
          name: visibleClaim,
          apiVersion: 'v1',
          scopeMode: 'namespace',
        },
      },
    ],
    edges: [
      { id: 'claim-class', sourceId: 'claim', targetId: 'root', relation: 'uses-storage-class' },
    ],
  }
}

function establishSession(userId: string) {
  const user: User = {
    userId,
    userName: userId,
    email: '',
    roles: [],
    teams: [],
    projects: [],
    tags: null,
  }
  const auth = useAuthStore.getState()
  auth.setTokens(`test-token-${userId}`)
  auth.setUser(user)
}

describe('relationship reads with a real QueryClient', () => {
  let client: QueryClient, container: HTMLDivElement, root: Root
  beforeEach(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    state.status = 403
    state.response = 'valid'
    state.reads = []
    state.storageRead.mockReset()
    client = new QueryClient({ defaultOptions: { queries: { retryDelay: 0, gcTime: Infinity } } })
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    useAuthStore.getState().clearAuth()
    client.clear()
    focusManager.setFocused(undefined)
    container.remove()
    vi.unstubAllGlobals()
  })
  async function settle() {
    for (let i = 0; i < 8; i++)
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
  }
  async function render(name = 'api', clusterId = 'a') {
    await act(async () =>
      root.render(
        <QueryClientProvider client={client}>
          <WorkloadRelationships kind="Pod" name={name} scope={{ ...scope, clusterId }} />
        </QueryClientProvider>,
      ),
    )
    await settle()
  }
  async function refresh() {
    await act(async () =>
      container.querySelector<HTMLButtonElement>('[aria-label="刷新资源关系"]')!.click(),
    )
    await settle()
  }
  const podKey = (rootScope = scope, name = 'api') =>
    resourceInsightKeys.graph(rootScope, 'Pod', name, useAuthStore.getState().sessionGeneration)

  it.each([401, 403])(
    'latches %s, clears private data, avoids automatic retries and supports explicit recovery',
    async (status) => {
      state.status = status
      client.setQueryData(podKey(), cached, { updatedAt: 1 })
      await render()
      expect(state.reads).toHaveLength(1)
      expect(container.querySelector('[data-id]')).toBeNull()
      expect(container.textContent).toContain(`rejected ${status}`)
      expect(client.getQueryData(podKey())).toBeUndefined()
      await act(async () => {
        focusManager.setFocused(false)
        focusManager.setFocused(true)
        await client.invalidateQueries({ queryKey: resourceInsightKeys.all })
      })
      await render()
      await settle()
      expect(state.reads).toHaveLength(1)
      // A repeated explicit rejection is one request, and cannot restart an automatic loop.
      await refresh()
      expect(state.reads).toHaveLength(2)
      await render()
      await settle()
      expect(state.reads).toHaveLength(2)
      state.status = 0
      await refresh()
      expect(state.reads).toHaveLength(3)
      expect(container.querySelector('[data-id="root"]')?.textContent).toBe('Pod api')
      expect(container.textContent).not.toContain(`rejected ${status}`)
      await settle()
      expect(state.reads).toHaveLength(3)
      // Manual refresh bypasses the local 30-second freshness window.
      await refresh()
      expect(state.reads).toHaveLength(4)
      // Recovery restores automatic reads: invalidation must fetch again.
      await act(async () => {
        await client.invalidateQueries({ queryKey: podKey() })
      })
      await settle()
      expect(state.reads).toHaveLength(5)
    },
  )
  it.each(['wrong-gvk', 'wrong-scope', 'wrong-cluster'])(
    'does not display a successful API response with %s as the current root',
    async (response) => {
      state.status = 0
      state.response = response
      await render()
      expect(state.reads).toEqual(['a/ns/Pod/api'])
      expect(container.querySelector('[data-id]')).toBeNull()
      expect(container.textContent).toContain('资源关系数据未返回')
    },
  )
  it('isolates a denial from a new root and cluster without rendering old nodes', async () => {
    await render()
    expect(state.reads).toEqual(['a/ns/Pod/api'])
    state.status = 0
    await render('other', 'b')
    expect(state.reads).toEqual(['a/ns/Pod/api', 'b/ns/Pod/other'])
    expect(container.querySelector('[data-id="root"]')?.textContent).toBe('Pod other')
    expect(container.textContent).not.toContain('rejected')
    expect(client.getQueryData(podKey())).toBeUndefined()
    expect(client.getQueryData(podKey({ ...scope, clusterId: 'b' }, 'other'))).toMatchObject({
      clusterId: 'b',
    })
  })

  it('isolates the same StorageClass root and an in-flight result across sessions', async () => {
    establishSession('account-a')
    const storageKey = () =>
      resourceInsightKeys.graph(
        storageScope,
        'StorageClass',
        'fast',
        useAuthStore.getState().sessionGeneration,
      )
    const firstKey = storageKey()
    let finishA!: (graph: KubernetesResourceGraph) => void
    let finishB!: (graph: KubernetesResourceGraph) => void
    state.storageRead
      .mockResolvedValueOnce(storageGraph('private-a'))
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finishA = resolve
          }),
      )
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finishB = resolve
          }),
      )
    await act(async () =>
      root.render(
        <QueryClientProvider client={client}>
          <WorkloadRelationships kind="StorageClass" name="fast" scope={storageScope} />
        </QueryClientProvider>,
      ),
    )
    await settle()
    expect(container.textContent).toContain('private-a')
    await refresh()
    expect(state.storageRead).toHaveBeenCalledTimes(2)

    await act(async () => {
      useAuthStore.getState().clearAuth()
      establishSession('account-b')
    })
    await settle()
    const secondKey = storageKey()
    expect(secondKey).not.toEqual(firstKey)
    expect(state.storageRead).toHaveBeenCalledTimes(3)
    expect(container.textContent).not.toContain('private-a')

    await act(async () => finishB(storageGraph('visible-b')))
    await settle()
    expect(container.textContent).toContain('visible-b')
    state.storageRead.mockResolvedValueOnce(storageGraph('refreshed-b'))
    await refresh()
    expect(state.storageRead).toHaveBeenCalledTimes(4)
    expect(container.textContent).toContain('refreshed-b')
    await act(async () => finishA(storageGraph('late-private-a')))
    await settle()
    expect(container.textContent).toContain('refreshed-b')
    expect(container.textContent).not.toContain('private-a')
    expect(client.getQueryData(firstKey)).toEqual(storageGraph('late-private-a'))
    expect(client.getQueryData(secondKey)).toEqual(storageGraph('refreshed-b'))
  })
})
