import {
  Background,
  Controls,
  MarkerType,
  Position,
  ReactFlow,
  ReactFlowProvider,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react'
import dagre from 'dagre'
import { useMemo, type CSSProperties } from 'react'

import { StatusTag } from '@/components/status-tag'
import { WorkflowCanvasSurface } from '@/components/workflow-canvas-surface'
import '@xyflow/react/dist/style.css'
import type { WorkbenchGraph, WorkbenchGraphNode } from '../types'

const GRAPH_NODE_WIDTH = 248
const GRAPH_NODE_HEIGHT = 104

type WorkbenchFlowNode = Node<WorkbenchGraphNode & Record<string, unknown>, 'workbenchGraphNode'>
type WorkbenchFlowEdge = Edge<{ relation: string; severity?: string }, 'smoothstep'>

function graphAccent(kind: string) {
  switch (kind) {
    case 'scope':
      return 'var(--soha-graph-scope)'
    case 'service':
      return 'var(--soha-graph-service)'
    case 'span':
      return 'var(--soha-graph-span)'
    case 'log_signature':
      return 'var(--soha-graph-log)'
    case 'metric_signal':
      return 'var(--soha-graph-metric)'
    case 'hypothesis':
      return 'var(--soha-graph-hypothesis)'
    case 'recommendation':
      return 'var(--soha-graph-recommendation)'
    default:
      return 'var(--soha-graph-muted)'
  }
}

function graphEdgeColor(severity?: string) {
  if (severity === 'critical') return 'var(--soha-workflow-edge-failure)'
  if (severity === 'warning') return 'var(--soha-warning)'
  return 'var(--soha-workflow-edge-default)'
}

function graphNodeLabel(kind: string) {
  switch (kind) {
    case 'scope':
      return '范围'
    case 'service':
      return '服务'
    case 'span':
      return 'Span'
    case 'log_signature':
      return '日志'
    case 'metric_signal':
      return '指标'
    case 'hypothesis':
      return '假设'
    case 'missing_source':
      return '缺失源'
    case 'recommendation':
      return '建议'
    default:
      return kind
  }
}

function graphEdges(graph: WorkbenchGraph, selectedNodeId: string | null): WorkbenchFlowEdge[] {
  return (graph.edges ?? []).map((item) => ({
    id: item.id,
    source: item.source,
    target: item.target,
    type: 'smoothstep',
    data: { relation: item.relation, severity: item.severity },
    markerEnd: {
      type: MarkerType.ArrowClosed,
      color: graphEdgeColor(item.severity),
    },
    label: item.relation,
    style: {
      stroke: graphEdgeColor(item.severity),
      strokeWidth:
        selectedNodeId && [item.source, item.target].includes(selectedNodeId)
          ? 2.4
          : item.relation === 'supports'
            ? 1.4
            : 1.8,
      opacity: selectedNodeId && ![item.source, item.target].includes(selectedNodeId) ? 0.25 : 1,
      strokeDasharray: item.relation === 'supports' ? '8 4' : undefined,
    },
    labelStyle: { fontSize: 11, fill: 'var(--soha-text-secondary)' },
  }))
}

function layoutWorkbenchGraph(nodes: WorkbenchFlowNode[], edges: WorkbenchFlowEdge[]) {
  const graph = new dagre.graphlib.Graph()
  graph.setDefaultEdgeLabel(() => ({}))
  graph.setGraph({ rankdir: 'LR', ranksep: 88, nodesep: 28 })

  nodes.forEach((node) =>
    graph.setNode(node.id, { width: GRAPH_NODE_WIDTH, height: GRAPH_NODE_HEIGHT }),
  )
  edges.forEach((edge) => graph.setEdge(edge.source, edge.target))
  dagre.layout(graph)

  return nodes.map((node) => {
    const position = graph.node(node.id) ?? { x: GRAPH_NODE_WIDTH / 2, y: GRAPH_NODE_HEIGHT / 2 }
    return {
      ...node,
      position: {
        x: position.x - GRAPH_NODE_WIDTH / 2,
        y: position.y - GRAPH_NODE_HEIGHT / 2,
      },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    }
  })
}

function WorkbenchGraphNodeCard({ data, selected }: NodeProps<WorkbenchFlowNode>) {
  const accentStyle = {
    '--soha-workbench-graph-accent': graphAccent(data.kind),
  } as CSSProperties
  return (
    <div className={`soha-workbench-graph-node ${selected ? 'is-selected' : ''}`}>
      <div className="soha-workbench-graph-node__card" style={accentStyle}>
        <div className="soha-workbench-graph-node__head">
          <span className="soha-workbench-graph-node__kind">{graphNodeLabel(data.kind)}</span>
          {data.severity ? <StatusTag value={data.severity} /> : null}
        </div>
        <div className="soha-workbench-graph-node__title">{data.title}</div>
        {data.subtitle ? (
          <div className="soha-workbench-graph-node__subtitle">{data.subtitle}</div>
        ) : null}
        {data.sourceRefs?.length ? (
          <div className="soha-workbench-graph-node__refs">
            {data.sourceRefs.slice(0, 2).join(' · ')}
          </div>
        ) : null}
      </div>
    </div>
  )
}

const WORKBENCH_GRAPH_NODE_TYPES = {
  workbenchGraphNode: WorkbenchGraphNodeCard,
} as const

function WorkbenchGraphCanvas({
  graph,
  selectedNodeId,
  onSelectNode,
}: {
  graph: WorkbenchGraph
  selectedNodeId: string | null
  onSelectNode: (nodeId: string | null) => void
}) {
  const edges = useMemo(() => graphEdges(graph, selectedNodeId), [graph, selectedNodeId])
  const nodes = useMemo(
    () =>
      layoutWorkbenchGraph(
        (graph.nodes ?? []).map((item) => ({
          id: item.id,
          type: 'workbenchGraphNode',
          selected: item.id === selectedNodeId,
          ariaRole: 'button',
          ariaLabel: `${graphNodeLabel(item.kind)} ${item.title}`,
          position: { x: 0, y: 0 },
          data: { ...item } as WorkbenchGraphNode & Record<string, unknown>,
        })),
        edges,
      ),
    [edges, graph, selectedNodeId],
  )

  return (
    <WorkflowCanvasSurface className="soha-workbench-graph-canvas">
      <ReactFlow<WorkbenchFlowNode, WorkbenchFlowEdge>
        nodes={nodes}
        edges={edges}
        nodeTypes={WORKBENCH_GRAPH_NODE_TYPES}
        fitView
        fitViewOptions={{ duration: 0 }}
        deleteKeyCode={null}
        multiSelectionKeyCode={null}
        onNodesChange={(changes) => {
          const selected = changes.find((change) => change.type === 'select' && change.selected)
          if (selected?.type === 'select') onSelectNode(selected.id)
          else if (
            changes.some(
              (change) =>
                change.type === 'select' && change.id === selectedNodeId && !change.selected,
            )
          )
            onSelectNode(null)
        }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable
        edgesFocusable={false}
        proOptions={{ hideAttribution: true }}
        onPaneClick={() => onSelectNode(null)}
        onNodeClick={(_, node) => onSelectNode(node.id)}
      >
        <Background gap={18} size={1} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </WorkflowCanvasSurface>
  )
}

export default function WorkbenchGraphView({
  fitKey,
  graph,
  selectedNodeId,
  onSelectNode,
}: {
  fitKey: string
  graph: WorkbenchGraph
  selectedNodeId: string | null
  onSelectNode: (nodeId: string | null) => void
}) {
  return (
    <ReactFlowProvider>
      <WorkbenchGraphCanvas
        key={fitKey}
        graph={graph}
        selectedNodeId={selectedNodeId}
        onSelectNode={onSelectNode}
      />
    </ReactFlowProvider>
  )
}
