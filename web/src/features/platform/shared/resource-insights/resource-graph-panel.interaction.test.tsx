/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  ResourceGraphPanel,
  ResourceGraphCanvas,
  buildResourceGraphFlow,
} from './resource-graph-panel'

const state = vi.hoisted(() => ({
  graph: null as KubernetesResourceGraph | null,
  navigate: vi.fn(),
  fitView: vi.fn(),
}))

vi.mock('react-router-dom', () => ({ useNavigate: () => state.navigate }))
vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: 'zh_CN' }) }))
vi.mock('@tanstack/react-query', () => ({
  useQuery: () => ({ data: state.graph }),
  queryOptions: (options: unknown) => options,
}))
vi.mock('@xyflow/react', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  ReactFlowProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  Background: () => null,
  Controls: () => null,
  useReactFlow: () => ({ fitView: state.fitView }),
  useStore: (selector: (state: { width: number; height: number }) => number) =>
    selector({ width: 800, height: 600 }),
  ReactFlow: ({
    nodes,
    onNodeClick,
    onNodesChange,
  }: {
    nodes: { id: string; ariaLabel: string }[]
    onNodeClick: (event: unknown, node: { id: string }) => void
    onNodesChange: (changes: { type: string; id: string; selected: boolean }[]) => void
  }) => (
    <div>
      {nodes.map((node) => (
        <button
          key={node.id}
          data-node={node.id}
          onClick={(e) => onNodeClick(e, node)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') onNodesChange([{ type: 'select', id: node.id, selected: true }])
            if (e.key === 'Escape')
              onNodesChange([{ type: 'select', id: node.id, selected: false }])
          }}
        >
          {node.ariaLabel}
        </button>
      ))}
    </div>
  ),
}))

describe('resource graph inspection', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal(
      'requestAnimationFrame',
      vi.fn(() => 1),
    )
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
    state.navigate.mockClear()
    state.graph = {
      clusterId: 'cluster-a',
      generatedAt: '2026-09-21T10:00:00Z',
      rootId: 'deployment',
      nodes: [
        {
          id: 'deployment',
          resource: {
            clusterId: 'cluster-a',
            apiVersion: 'apps/v1',
            kind: 'Deployment',
            name: 'api',
            namespace: 'team-a',
            scopeMode: 'namespace',
          },
        },
        {
          id: 'pod',
          resource: {
            clusterId: 'cluster-a',
            apiVersion: 'v1',
            kind: 'Pod',
            name: 'api-1',
            namespace: 'team-a',
            scopeMode: 'namespace',
          },
          status: 'running',
        },
        {
          id: 'unknown',
          resource: {
            clusterId: 'cluster-a',
            apiVersion: 'example/v1',
            kind: 'UnknownKind',
            name: 'external',
            scopeMode: 'cluster',
          },
        },
      ],
      edges: [{ id: 'owns', sourceId: 'deployment', targetId: 'pod', relation: 'owns' }],
      warnings: [],
      evidence: [],
    }
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)
  })

  afterEach(async () => {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  })

  async function render() {
    await act(async () =>
      root.render(
        <ResourceGraphPanel
          kind="Deployment"
          name="api"
          scope={{ clusterId: state.graph!.clusterId, namespace: 'team-a' }}
        />,
      ),
    )
  }
  async function click(selector: string) {
    await act(async () => container.querySelector<HTMLElement>(selector)!.click())
  }

  it('inspects a node first and only navigates from the explicit scoped detail action', async () => {
    await render()
    expect(container.querySelector('aside')).toBeNull()
    await click('[data-node="pod"]')
    expect(state.navigate).not.toHaveBeenCalled()
    expect(container.querySelector('aside')?.textContent).toContain('api-1')
    expect(container.querySelector('aside')?.textContent).toContain('team-a')
    expect(container.querySelector('aside')?.textContent).toContain('来自 Deployment')
    await click('.soha-resource-graph-details-action button')
    expect(state.navigate).toHaveBeenCalledWith(
      '/workloads/pods/api-1?clusterId=cluster-a&namespace=team-a',
    )
    await click('[aria-label="关闭资源信息"]')
    expect(container.querySelector('aside')).toBeNull()
  })

  it('supports keyboard selection and leaves unsupported resource types inspectable', async () => {
    await render()
    await act(async () =>
      container
        .querySelector('[data-node="pod"]')!
        .dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })),
    )
    expect(container.querySelector('aside')?.textContent).toContain('api-1')
    await act(async () =>
      container
        .querySelector('[data-node="pod"]')!
        .dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })),
    )
    expect(container.querySelector('aside')).toBeNull()
    await click('[data-node="unknown"]')
    expect(container.querySelector('aside')?.textContent).toContain('external')
    expect(
      container.querySelector<HTMLButtonElement>('.soha-resource-graph-details-action button')
        ?.disabled,
    ).toBe(true)
    expect(state.navigate).not.toHaveBeenCalled()
  })

  it('allows inspecting a neighbor and resets selection when cluster scope changes', async () => {
    await render()
    await click('[data-node="pod"]')
    await click('.soha-resource-graph-related-resource')
    expect(container.querySelector('.soha-resource-graph-details-name')?.textContent).toBe('api')
    expect(state.navigate).not.toHaveBeenCalled()
    state.graph = { ...state.graph!, clusterId: 'cluster-b' }
    await render()
    expect(container.querySelector('aside')).toBeNull()
  })
  it('retains supplied flow output and supports external selection without the legacy inspector', async () => {
    const graph = state.graph!
    const flow = buildResourceGraphFlow(graph)
    flow.nodes[0].ariaLabel = 'custom supplied root'
    const onSelect = vi.fn()
    await act(async () =>
      root.render(
        <ResourceGraphCanvas
          graph={graph}
          flow={flow}
          selectedId="pod"
          onSelect={onSelect}
          hideInspector
        />,
      ),
    )
    expect(container.textContent).toContain('custom supplied root')
    expect(container.querySelector('aside')).toBeNull()
    await click('[data-node="deployment"]')
    expect(onSelect).toHaveBeenCalledWith('deployment')
  })
})
