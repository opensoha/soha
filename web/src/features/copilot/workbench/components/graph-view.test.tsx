/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, expect, it, vi } from 'vitest'
import WorkbenchGraphView from './graph-view'
import type { WorkbenchGraph } from '../types'
const state = vi.hoisted(() => ({ props: {} as Record<string, any> }))
vi.mock('@xyflow/react', async (original) => ({
  ...(await original<object>()),
  ReactFlowProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  ReactFlow: (props: Record<string, any>) => {
    state.props = props
    return <div />
  },
  Background: () => null,
  Controls: () => null,
}))
let root: ReturnType<typeof createRoot>
let host: HTMLDivElement

afterEach(async () => {
  await act(async () => root?.unmount())
  host?.remove()
  vi.unstubAllGlobals()
})
it('links keyboard selection to the owner, highlights adjacent edges and keeps exploration read only', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  const graph: WorkbenchGraph = {
    nodes: [
      { id: 'service', title: 'Checkout', kind: 'service' },
      { id: 'signal', title: 'Latency', kind: 'metric_signal' },
      { id: 'other', title: 'Other', kind: 'service' },
    ],
    edges: [
      { id: 'related', source: 'service', target: 'signal', relation: 'supports' },
      { id: 'unrelated', source: 'other', target: 'signal', relation: 'supports' },
    ],
  }
  const selected = vi.fn()
  await act(async () =>
    root.render(
      <WorkbenchGraphView
        fitKey="session-a"
        graph={graph}
        selectedNodeId="service"
        onSelectNode={selected}
      />,
    ),
  )
  expect(state.props.nodes.find((node: any) => node.id === 'service')).toMatchObject({
    selected: true,
    ariaRole: 'button',
    ariaLabel: '服务 Checkout',
  })
  expect(state.props.edges.find((edge: any) => edge.id === 'related').style.opacity).toBe(1)
  expect(state.props.edges.find((edge: any) => edge.id === 'unrelated').style.opacity).toBeLessThan(
    1,
  )
  state.props.onNodesChange([
    { type: 'select', id: 'signal', selected: true },
    { type: 'select', id: 'service', selected: false },
  ])
  expect(selected).toHaveBeenLastCalledWith('signal')
  state.props.onNodesChange([{ type: 'select', id: 'service', selected: false }])
  expect(selected).toHaveBeenLastCalledWith(null)
  expect(state.props.nodesDraggable).toBe(false)
  expect(state.props.nodesConnectable).toBe(false)
  expect(state.props.deleteKeyCode).toBeNull()
  expect(state.props.fitViewOptions.duration).toBe(0)
})
