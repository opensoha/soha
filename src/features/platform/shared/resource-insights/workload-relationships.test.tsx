/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { ApiError } from '@/services/api-error'
import { WorkloadRelationships } from './workload-relationships'
import {
  workloadKinds,
  networkRelationshipKinds,
  relationshipResourceDescriptors,
  type WorkloadKind,
  type RelationshipRootKind,
} from './relationship-model'
const state = vi.hoisted(() => ({
  graph: undefined as KubernetesResourceGraph | undefined,
  permissions: ['*'],
  enabled: false,
  error: null as Error | null,
  navigate: vi.fn(),
  removeQueries: vi.fn(),
  refetch: vi.fn(),
}))
vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: 'zh_CN' }) }))
vi.mock('react-router-dom', () => ({ useNavigate: () => state.navigate }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: state.permissions }, error: null }),
  hasPermission: (permissions: string[] | undefined, key: string) =>
    Boolean(permissions?.includes('*') || permissions?.includes(key)),
}))
vi.mock('@tanstack/react-query', () => ({
  queryOptions: (o: unknown) => o,
  useQuery: (o: { enabled: boolean }) => {
    state.enabled = o.enabled
    return {
      data: state.graph,
      error: state.error,
      isError: Boolean(state.error),
      isLoading: false,
      isFetching: false,
      isEnabled: o.enabled,
      refetch: state.refetch,
    }
  },
  useQueryClient: () => ({ removeQueries: state.removeQueries }),
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
    'data-resource-id'?: string
  }) => (
    <button
      aria-label={props['aria-label']}
      aria-pressed={props['aria-pressed']}
      data-resource-id={props['data-resource-id']}
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
const workloadApiVersion = (kind: string) =>
  ['Deployment', 'StatefulSet', 'DaemonSet', 'ReplicaSet'].includes(kind)
    ? 'apps/v1'
    : ['Job', 'CronJob'].includes(kind)
      ? 'batch/v1'
      : 'v1'
function fixture(kind: WorkloadKind = 'Pod', name = 'api'): KubernetesResourceGraph {
  const resource = {
    clusterId: 'a',
    namespace: 'ns',
    scopeMode: 'namespace' as const,
    apiVersion: 'v1',
  }
  return {
    clusterId: 'a',
    rootId: 'root',
    generatedAt: '',
    warnings: [],
    evidence: [],
    nodes: [
      { id: 'root', resource: { ...resource, kind, name, apiVersion: workloadApiVersion(kind) } },
      { id: 'service', resource: { ...resource, kind: 'Service', name: 'front' } },
      {
        id: 'secret',
        status: 'unresolved',
        resource: { ...resource, kind: 'Secret', name: 'credential' },
      },
      { id: 'unknown', resource: { ...resource, kind: 'UnknownKind', name: 'external' } },
    ],
    edges: [
      { id: 'selection', sourceId: 'service', targetId: 'root', relation: 'selects' },
      { id: 'custom', sourceId: 'root', targetId: 'unknown', relation: 'future-token' },
    ],
  }
}
describe('workload relationship browser', () => {
  let container: HTMLDivElement, root: Root
  beforeEach(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    state.graph = fixture()
    state.permissions = ['*']
    state.error = null
    state.navigate.mockClear()
    state.removeQueries.mockClear()
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  })
  async function render(kind: RelationshipRootKind = 'Pod', active = true, name = 'api') {
    await act(async () =>
      root.render(
        <WorkloadRelationships
          kind={kind}
          name={name}
          scope={{
            clusterId: 'a',
            namespace: relationshipResourceDescriptors[kind].scope === 'namespace' ? 'ns' : null,
          }}
          active={active}
        />,
      ),
    )
  }
  async function click(text: string) {
    const button = [...container.querySelectorAll('button')].find(
      (button) => button.textContent === text,
    )!
    expect(button).toBeTruthy()
    await act(async () => button.click())
    return button
  }
  it('routes actual ReferenceGrant v1 nodes and declaration sources with their namespace', async () => {
    state.graph = fixture()
    state.graph.nodes[0].resource = {
      clusterId: 'a',
      namespace: 'ns',
      scopeMode: 'namespace',
      apiVersion: 'gateway.networking.k8s.io/v1',
      kind: 'ReferenceGrant',
      name: 'api',
    }
    state.graph.edges[0].sourceRef = {
      clusterId: 'a',
      namespace: 'other',
      scopeMode: 'namespace',
      apiVersion: 'gateway.networking.k8s.io/v1',
      kind: 'ReferenceGrant',
      name: 'declaration',
    }
    state.permissions = ['platform.network.reference-grants.view']
    await render('ReferenceGrant')
    await click('ReferenceGrant api')
    await click('查看详情')
    expect(state.navigate).toHaveBeenCalledWith(
      '/network/gateway-api/referencegrants/api?namespace=ns&clusterId=a',
    )
    const source = [...container.querySelector('aside')!.querySelectorAll('button')].find(
      (button) => button.textContent === 'ReferenceGrant other / declaration',
    )!
    expect(source).toBeTruthy()
    await act(async () => source.click())
    expect(state.navigate).toHaveBeenCalledWith(
      '/network/gateway-api/referencegrants/declaration?namespace=other&clusterId=a',
    )
  })
  it.each(networkRelationshipKinds)('reads the %s root only on its active tab', async (kind) => {
    const descriptor = relationshipResourceDescriptors[kind]
    state.graph = fixture()
    state.graph.nodes[0].resource = {
      ...state.graph.nodes[0].resource,
      kind,
      apiVersion: descriptor.apiVersions[0],
      scopeMode: descriptor.scope,
      namespace: descriptor.scope === 'namespace' ? 'ns' : undefined,
    }
    await render(kind, false)
    expect(state.enabled).toBe(false)
    await render(kind)
    expect(state.enabled).toBe(true)
    await click(`${kind} api`)
    const inspector = container.querySelector('aside')
    expect(inspector?.textContent).toContain('api')
    await act(async () =>
      inspector?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    expect(container.querySelector('aside')).toBeNull()
    state.permissions = []
    await render(kind)
    expect(state.enabled).toBe(false)
    expect(container.textContent).toContain('无资源查看权限')
  })
  it.each([
    ['gateway.networking.k8s.io/v1', ['*'], true],
    ['custom.example/v1', ['*'], false],
    ['gateway.networking.k8s.io/v1', ['platform.network.services.view'], false],
  ] as const)(
    'shows structured declarations and gates sourceRef %s navigation',
    async (apiVersion, permissions, linkable) => {
      state.graph = fixture()
      state.graph.nodes[0] = {
        ...state.graph.nodes[0],
        status: 'observed',
        health: 'Ready',
        resource: { ...state.graph.nodes[0].resource, kind: 'Service', uid: 'uid-current' },
      }
      state.graph.edges[0] = {
        ...state.graph.edges[0],
        resolutionState: 'no_ready_endpoints',
        protocol: 'TCP',
        port: '443',
        sectionName: 'tls',
        fieldPath: 'spec.parentRefs[0]',
        note: 'UID does not establish readiness',
        sourceRef: {
          clusterId: 'a',
          namespace: 'other',
          scopeMode: 'namespace',
          apiVersion,
          kind: 'Gateway',
          name: 'edge',
        },
      }
      state.permissions = [...permissions]
      await render('Service')
      await click('Service api')
      const inspector = container.querySelector('aside')!
      expect(inspector.textContent).toContain('uid-current')
      expect(inspector.textContent).toContain('资源观测状态Ready')
      expect(inspector.textContent).toContain('关系解析未观测到就绪端点')
      expect(inspector.textContent).toContain('声明字段spec.parentRefs[0]')
      expect(inspector.textContent).toContain('SectionNametls')
      const source = [...inspector.querySelectorAll('button')].find(
        (button) => button.textContent === 'Gateway other / edge',
      )
      expect(Boolean(source)).toBe(linkable)
      if (source) {
        await act(async () => source.click())
        expect(state.navigate).toHaveBeenCalledWith(
          '/network/gateway-api/gateways/edge?namespace=other&clusterId=a',
        )
      }
      await click('列表')
      expect(
        container.querySelector('.soha-workload-relationships-edge-facts')?.textContent,
      ).toContain('spec.parentRefs[0]')
    },
  )
  it.each(workloadKinds)(
    'reads the exact %s root only when visible and permitted',
    async (kind) => {
      state.graph = fixture(kind)
      await render(kind)
      expect(state.enabled).toBe(true)
      await render(kind, false)
      expect(state.enabled).toBe(false)
      state.permissions = []
      await render(kind)
      expect(state.enabled).toBe(false)
      expect(container.textContent).toContain('无资源查看权限')
      expect(container.querySelector('[data-id]')).toBeNull()
    },
  )
  it('uses one selection across graph/list, preserves refresh selection and clears on identity change', async () => {
    await render()
    const node = await click('Service front')
    expect(container.querySelector('aside')?.textContent).toContain('选择器匹配 Pod api')
    expect(state.navigate).not.toHaveBeenCalled()
    await click('列表')
    expect(container.querySelector('aside')?.textContent).toContain('front')
    state.graph = { ...fixture(), generatedAt: '2026-11-05T00:00:00Z' }
    await render()
    expect(container.querySelector('aside')).not.toBeNull()
    await click('关系图')
    await act(async () =>
      container
        .querySelector('aside')!
        .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    expect(container.querySelector('aside')).toBeNull()
    // Switching views unmounts the previous node: focus falls back to the view control.
    expect(document.activeElement).not.toBe(node)
    await click('Pod api')
    expect(container.querySelector('aside')?.textContent).toContain('被选择器匹配 Service front')
    expect(container.querySelector('aside')?.textContent).toContain('future-token')
    state.graph = fixture('Pod', 'second')
    await render('Pod', true, 'second')
    expect(container.querySelector('aside')).toBeNull()
  })
  it.each(['close', 'Escape'])(
    'restores the recreated list resource button after view changes and refresh via %s',
    async (method) => {
      await render()
      await click('列表')
      const original = await click('front')
      expect(original.dataset.resourceId).toBe('service')
      await click('关系图')
      expect(original.isConnected).toBe(false)
      state.refetch.mockImplementationOnce(async () => {
        state.graph = { ...fixture(), generatedAt: '2026-11-05T01:00:00Z' }
        return { isSuccess: true, error: null, data: state.graph }
      })
      await act(async () =>
        container.querySelector<HTMLButtonElement>('[aria-label="刷新资源关系"]')!.click(),
      )
      await click('列表')
      const replacement = container.querySelector<HTMLButtonElement>(
        '[data-resource-id="service"]',
      )!
      expect(replacement).not.toBe(original)
      expect(replacement.isConnected).toBe(true)
      expect(container.querySelector('aside')?.textContent).toContain('front')
      if (method === 'close') {
        await act(async () =>
          container.querySelector<HTMLButtonElement>('[aria-label="关闭资源信息"]')!.click(),
        )
      } else {
        await act(async () =>
          container
            .querySelector('aside')!
            .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
        )
      }
      expect(container.querySelector('aside')).toBeNull()
      expect(document.activeElement).toBe(replacement)
    },
  )
  it('returns focus to the selected graph node after following a relationship inside the inspector', async () => {
    await render()
    await click('Pod api')
    const neighbor = [...container.querySelectorAll<HTMLButtonElement>('aside button')].find(
      (button) => button.textContent === 'Service front',
    )!
    await act(async () => neighbor.click())
    expect(container.querySelector('aside')?.textContent).toContain('选择器匹配 Pod api')
    await act(async () =>
      container
        .querySelector('aside')!
        .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    expect(container.querySelector('aside')).toBeNull()
    expect(document.activeElement).toBe(container.querySelector('[data-id="service"]'))
  })
  it('returns focus on Escape and keeps unresolved/unsupported/unauthorized targets non-navigable', async () => {
    await render()
    const node = await click('Secret credential')
    expect(container.querySelector('aside')?.textContent).toContain('未解析')
    expect(
      [...container.querySelectorAll('button')].find((b) => b.textContent === '查看详情')?.disabled,
    ).toBe(true)
    await act(async () =>
      container
        .querySelector('aside')!
        .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    expect(document.activeElement).toBe(node)
    await click('UnknownKind external')
    expect(
      [...container.querySelectorAll('button')].find((b) => b.textContent === '查看详情')?.disabled,
    ).toBe(true)
    state.permissions = ['platform.pods.view']
    await render()
    await click('Service front')
    expect(
      [...container.querySelectorAll('button')].find((b) => b.textContent === '查看详情')?.disabled,
    ).toBe(true)
  })
  it.each([
    { kind: 'Pod', apiVersion: 'custom.example/v1' },
    { kind: 'Service', apiVersion: 'custom.example/v1' },
    { kind: 'Service', apiVersion: 'v1', scopeMode: 'cluster' as const },
    { kind: 'Service', apiVersion: 'v1', namespace: undefined },
    { kind: 'Service', apiVersion: 'v1', scopeMode: 'unknown' },
    { kind: 'Node', apiVersion: 'v1', scopeMode: 'namespace' as const },
    { kind: 'Service', apiVersion: 'v1', clusterId: 'different' },
  ])('keeps $kind $apiVersion inspectable without using a wrong built-in route', async (patch) => {
    const node = state.graph!.nodes[1]
    node.resource = { ...node.resource, ...patch } as typeof node.resource
    await render()
    await click(`${patch.kind} front`)
    expect(container.querySelector('aside')?.textContent).toContain('front')
    const action = [...container.querySelectorAll('button')].find(
      (button) => button.textContent === '查看详情',
    )!
    expect(action.disabled).toBe(true)
    await act(async () => action.click())
    expect(state.navigate).not.toHaveBeenCalled()
  })
  it('still navigates a supported GVK using its exact resource scope', async () => {
    await render()
    await click('Service front')
    const action = [...container.querySelectorAll('button')].find(
      (button) => button.textContent === '查看详情',
    )!
    expect(action.disabled).toBe(false)
    await act(async () => action.click())
    expect(state.navigate).toHaveBeenCalledWith('/network/services/front?namespace=ns&clusterId=a')
  })
  it.each(['gvk', 'scope', 'cluster'])(
    'rejects a mismatched root %s without showing it as current',
    async (mismatch) => {
      if (mismatch === 'gvk') state.graph!.nodes[0].resource.apiVersion = 'custom.example/v1'
      if (mismatch === 'scope') state.graph!.nodes[0].resource.scopeMode = 'cluster'
      if (mismatch === 'cluster') state.graph!.clusterId = 'different'
      await render()
      expect(container.querySelector('[data-id]')).toBeNull()
      expect(container.textContent).toContain('资源关系数据未返回')
    },
  )
  it('clears selection when a same-name root is recreated with a new UID', async () => {
    state.graph!.nodes[0].resource.uid = 'first'
    await render()
    await click('Service front')
    expect(container.querySelector('aside')).not.toBeNull()
    state.graph = fixture()
    state.graph.nodes[0].resource.uid = 'replacement'
    await render()
    expect(container.querySelector('aside')).toBeNull()
  })

  it.each([401, 403])('retains ordinary-failure data and clears cache on %s', async (status) => {
    await render()
    state.error = new Error('temporary failure')
    await render()
    expect(container.querySelector('[data-id="root"]')).not.toBeNull()
    expect(container.textContent).toContain('temporary failure')
    state.error = new ApiError(status, 'denied')
    await render()
    expect(container.querySelector('[data-id]')).toBeNull()
    expect(state.removeQueries).toHaveBeenCalled()
    expect(container.textContent).toContain('denied')
  })
  it('does not present a stale graph from a different resource as empty or current', async () => {
    state.graph = fixture('Deployment')
    await render()
    expect(container.querySelector('[data-id]')).toBeNull()
    expect(container.textContent).toContain('资源关系数据未返回')
  })
})
