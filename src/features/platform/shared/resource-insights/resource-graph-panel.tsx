import { ManagementRefreshButton } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { buildKubernetesResourcePath } from '@/features/platform/shared/resource-ref'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth-store'
import type { ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { CloseOutlined } from '@ant-design/icons'
import type { KubernetesResourceGraph } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import {
  Background,
  Controls,
  MarkerType,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
  useStore,
  type Edge,
  type Node,
  type OnMove,
  type Viewport,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Alert, Button, Empty, Space, Spin, Switch, Typography } from 'antd'
import dagre from 'dagre'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { resourceInsightQueries } from './queries'
import './resource-graph-panel.css'

const NODE_WIDTH = 240
const NODE_HEIGHT = 112

type ResourceGraphNode = Node<{
  label: ReactNode
  resource: KubernetesResourceGraph['nodes'][number]['resource']
}>

export function buildResourceGraphFlow(graph: KubernetesResourceGraph): {
  nodes: ResourceGraphNode[]
  edges: Edge[]
} {
  const layout = new dagre.graphlib.Graph()
  layout.setDefaultEdgeLabel(() => ({}))
  layout.setGraph({ rankdir: 'TB', ranksep: 64, nodesep: 34 })

  graph.nodes.forEach((node) => layout.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT }))
  graph.edges.forEach((edge) => layout.setEdge(edge.sourceId, edge.targetId))
  dagre.layout(layout)

  const nodes = graph.nodes.map<ResourceGraphNode>((node) => {
    const position = layout.node(node.id) ?? { x: NODE_WIDTH / 2, y: NODE_HEIGHT / 2 }
    const namespace = node.resource.namespace ? `${node.resource.namespace}/` : ''
    const state = node.health || node.status
    const root = node.id === graph.rootId
    return {
      id: node.id,
      // Fixed node geometry must also be available to XYFlow before observation.
      // Controlled selection updates can replace its measured dimensions.
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
      ariaRole: 'button',
      ariaLabel: `${node.resource.kind} ${namespace}${node.resource.name}${state ? ` ${state}` : ''}`,
      className: `soha-resource-graph-node${root ? ' is-root' : ''}`,
      data: {
        label: (
          <span className="soha-resource-graph-label">
            <span className="soha-resource-graph-kind">{node.resource.kind}</span>
            <span className="soha-resource-graph-name" title={`${namespace}${node.resource.name}`}>
              {namespace}
              {node.resource.name}
            </span>
            {state ? <span className="soha-resource-graph-state">{state}</span> : null}
          </span>
        ),
        resource: node.resource,
      },
      position: { x: position.x - NODE_WIDTH / 2, y: position.y - NODE_HEIGHT / 2 },
      sourcePosition: Position.Bottom,
      targetPosition: Position.Top,
      style: {
        width: NODE_WIDTH,
        height: NODE_HEIGHT,
      },
    }
  })

  const edges = graph.edges.map<Edge>((edge) => ({
    id: edge.id,
    source: edge.sourceId,
    target: edge.targetId,
    label: edge.relation,
    markerEnd: { type: MarkerType.ArrowClosed, color: 'var(--soha-text-tertiary)' },
    style: { stroke: 'var(--soha-text-tertiary)', strokeWidth: 1.5 },
    labelStyle: { fill: 'var(--soha-text-secondary)', fontSize: 11 },
    labelBgStyle: { fill: 'var(--soha-bg-surface)', fillOpacity: 1 },
  }))

  return { nodes, edges }
}

export function ResourceGraphCanvas({
  graph,
  flow: suppliedFlow,
  getResourcePath,
  fitViewKey,
  defaultViewport,
  onMove,
  selectedId: controlledSelection,
  onSelect,
  hideInspector = false,
}: {
  graph: KubernetesResourceGraph
  selectedId?: string | null
  onSelect?: (id: string | null) => void
  hideInspector?: boolean
  fitViewKey?: string | boolean
  defaultViewport?: Viewport
  onMove?: OnMove
  flow?: ReturnType<typeof buildResourceGraphFlow>
  getResourcePath?: (
    resource: KubernetesResourceGraph['nodes'][number]['resource'],
  ) => string | null
}) {
  const navigate = useNavigate()
  const { localeCode } = useI18n()
  const { fitView } = useReactFlow()
  const canvasWidth = useStore((state) => state.width)
  const canvasHeight = useStore((state) => state.height)
  const canvasRef = useRef<HTMLDivElement>(null)
  const restoredViewport = useRef(Boolean(defaultViewport))
  const [localSelection, setLocalSelection] = useState<string | null>(null)
  const selectedId = controlledSelection === undefined ? localSelection : controlledSelection
  const setSelectedId = (id: string | null) => {
    setLocalSelection(id)
    onSelect?.(id)
  }
  const flow = useMemo(() => suppliedFlow ?? buildResourceGraphFlow(graph), [graph, suppliedFlow])
  const selectedNode = graph.nodes.find((node) => node.id === selectedId)
  const selectedPath = selectedNode
    ? (getResourcePath ?? buildKubernetesResourcePath)(selectedNode.resource)
    : null
  const relatedEdges = graph.edges.filter(
    (edge) => edge.sourceId === selectedId || edge.targetId === selectedId,
  )
  const zh = localeCode === 'zh_CN'

  useEffect(() => {
    if (!canvasWidth || !canvasHeight) return
    const frame = requestAnimationFrame(() => {
      if (restoredViewport.current) {
        restoredViewport.current = false
        return
      }
      void fitView({ padding: 0.18, duration: 0 })
    })
    return () => cancelAnimationFrame(frame)
  }, [fitView, canvasWidth, canvasHeight, fitViewKey])

  return (
    <div className="soha-resource-graph-workspace">
      <div className="soha-resource-graph-canvas" ref={canvasRef}>
        <ReactFlow<ResourceGraphNode, Edge>
          aria-label={
            localeCode === 'zh_CN' ? 'Kubernetes 资源关系图' : 'Kubernetes resource relationships'
          }
          edges={flow.edges.map((edge) =>
            edge.source === selectedId || edge.target === selectedId
              ? {
                  ...edge,
                  style: { stroke: 'var(--soha-primary)', strokeWidth: 1.8 },
                  markerEnd: { type: MarkerType.ArrowClosed, color: 'var(--soha-primary)' },
                }
              : edge,
          )}
          nodes={flow.nodes.map((node) => ({
            ...node,
            selected: node.id === selectedNode?.id,
            domAttributes: { 'aria-expanded': node.id === selectedNode?.id },
          }))}
          edgesFocusable={false}
          defaultViewport={defaultViewport}
          onMove={onMove}
          nodesConnectable={false}
          nodesDraggable={false}
          deleteKeyCode={null}
          multiSelectionKeyCode={null}
          proOptions={{ hideAttribution: true }}
          onNodeClick={(_, node) => setSelectedId(node.id)}
          onNodesChange={(changes) => {
            const selected = changes.find((change) => change.type === 'select' && change.selected)
            if (selected?.type === 'select') setSelectedId(selected.id)
            else if (
              changes.some(
                (change) =>
                  change.type === 'select' && change.id === selectedId && !change.selected,
              )
            ) {
              setSelectedId(null)
            }
          }}
        >
          <Background gap={20} size={1} color="var(--soha-border-color-strong)" />
          <Controls showInteractive={false} />
        </ReactFlow>
      </div>
      {selectedNode && !hideInspector ? (
        <aside
          className="soha-resource-graph-details"
          aria-label={zh ? '选中资源信息' : 'Selected resource information'}
        >
          <div className="soha-resource-graph-details-heading">
            <Typography.Text type="secondary">{zh ? '已选中' : 'Selected'}</Typography.Text>
            <Button
              type="text"
              icon={<CloseOutlined />}
              aria-label={zh ? '关闭资源信息' : 'Close resource information'}
              onClick={() => {
                canvasRef.current?.querySelector<HTMLElement>('.react-flow__node.selected')?.focus()
                setSelectedId(null)
              }}
            />
          </div>
          <Typography.Title level={5}>{selectedNode.resource.kind}</Typography.Title>
          <Typography.Text className="soha-resource-graph-details-name">
            {selectedNode.resource.name}
          </Typography.Text>
          <dl className="soha-resource-graph-facts">
            <dt>{zh ? '集群' : 'Cluster'}</dt>
            <dd>{selectedNode.resource.clusterId}</dd>
            <dt>{zh ? '命名空间' : 'Namespace'}</dt>
            <dd>{selectedNode.resource.namespace || (zh ? '集群级' : 'Cluster scoped')}</dd>
            <dt>API</dt>
            <dd>{selectedNode.resource.apiVersion}</dd>
            <dt>{zh ? '状态' : 'Status'}</dt>
            <dd>
              <StatusTag value={selectedNode.health || selectedNode.status} />
            </dd>
          </dl>
          <Typography.Text strong>{zh ? '直接关系' : 'Direct relationships'}</Typography.Text>
          <ul className="soha-resource-graph-relations">
            {relatedEdges.map((edge) => {
              const outgoing = edge.sourceId === selectedId
              const related = graph.nodes.find(
                (node) => node.id === (outgoing ? edge.targetId : edge.sourceId),
              )
              if (!related) return null
              return (
                <li key={edge.id}>
                  <Typography.Text type="secondary">
                    {edge.relation} · {outgoing ? (zh ? '指向' : 'To') : zh ? '来自' : 'From'}{' '}
                    {related.resource.kind}
                  </Typography.Text>
                  <button
                    type="button"
                    className="soha-resource-graph-related-resource"
                    onClick={() => setSelectedId(related.id)}
                  >
                    {related.resource.name}
                  </button>
                </li>
              )
            })}
          </ul>
          {relatedEdges.length === 0 ? (
            <Typography.Text type="secondary">
              {zh ? '暂无直接关系' : 'No direct relationships'}
            </Typography.Text>
          ) : null}
          <div className="soha-resource-graph-details-action">
            <Button
              type="primary"
              disabled={!selectedPath}
              onClick={() => {
                if (selectedPath) navigate(selectedPath)
              }}
            >
              {zh ? '查看资源详情' : 'View resource details'}
            </Button>
            {!selectedPath ? (
              <Typography.Text type="secondary">
                {zh ? '该资源暂无详情入口' : 'No detail page for this resource'}
              </Typography.Text>
            ) : null}
          </div>
        </aside>
      ) : null}
    </div>
  )
}

export function focusResourceGraph(graph: KubernetesResourceGraph): KubernetesResourceGraph {
  if (!graph.rootId) return graph
  const keep = new Set([graph.rootId])
  const ancestors = [graph.rootId]
  while (ancestors.length) {
    const target = ancestors.pop()
    for (const edge of graph.edges) {
      if (edge.targetId !== target || keep.has(edge.sourceId)) continue
      keep.add(edge.sourceId)
      ancestors.push(edge.sourceId)
    }
  }
  for (const edge of graph.edges) {
    if (edge.sourceId === graph.rootId) keep.add(edge.targetId)
    if (edge.targetId === graph.rootId) keep.add(edge.sourceId)
  }
  return {
    ...graph,
    nodes: graph.nodes.filter((node) => keep.has(node.id)),
    edges: graph.edges.filter((edge) => keep.has(edge.sourceId) && keep.has(edge.targetId)),
  }
}

export function ResourceGraphPanel({
  kind,
  name,
  scope,
  focusPod = false,
}: {
  kind: string
  name: string
  scope: ScopeKey
  focusPod?: boolean
}) {
  const { localeCode } = useI18n()
  const sessionGeneration = useAuthStore((state) => state.sessionGeneration)
  const graphQuery = useQuery(resourceInsightQueries.graph(scope, kind, name, sessionGeneration))
  const graph = readableQueryData(graphQuery)
  const [includeOtherResources, setIncludeOtherResources] = useState(false)
  const focusedGraph = useMemo(
    () => (graph && focusPod && !includeOtherResources ? focusResourceGraph(graph) : graph),
    [graph, focusPod, includeOtherResources],
  )

  if (graphQuery.isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    )
  }
  if (!graph)
    return (
      <ReadFeedback
        label={localeCode === 'zh_CN' ? '资源关系' : 'Resource relationships'}
        query={graphQuery}
      />
    )
  if (graph.nodes.length === 0) {
    return (
      <>
        <ReadFeedback
          label={localeCode === 'zh_CN' ? '资源关系' : 'Resource relationships'}
          query={graphQuery}
        />
        <ManagementRefreshButton
          aria-label="刷新资源关系"
          tooltip="刷新资源关系"
          loading={graphQuery.isFetching}
          onClick={() => void graphQuery.refetch()}
        />
        <Empty description={localeCode === 'zh_CN' ? '未发现关联资源' : 'No related resources'} />
      </>
    )
  }

  return (
    <div className="soha-detail-stack">
      <ReadFeedback
        label={localeCode === 'zh_CN' ? '资源关系' : 'Resource relationships'}
        query={graphQuery}
      />
      <Space wrap>
        {focusPod && (
          <label>
            <Switch
              size="small"
              checked={includeOtherResources}
              onChange={setIncludeOtherResources}
            />{' '}
            {localeCode === 'zh_CN' ? '包含其他关联资源' : 'Include other related resources'}
          </label>
        )}
        <ManagementRefreshButton
          aria-label="刷新资源关系"
          tooltip="刷新资源关系"
          loading={graphQuery.isFetching}
          onClick={() => void graphQuery.refetch()}
        />
        <Typography.Text strong>
          {localeCode === 'zh_CN' ? '资源关系与影响范围' : 'Resource relationships and impact'}
        </Typography.Text>
        <Typography.Text type="secondary">
          {new Date(graph.generatedAt).toLocaleString()}
        </Typography.Text>
      </Space>
      {graph.warnings.length ? (
        <Alert showIcon type="warning" title={graph.warnings.join(' ')} />
      ) : null}
      <ReactFlowProvider>
        <ResourceGraphCanvas
          key={`${graph.clusterId}/${graph.rootId ?? `${kind}/${name}`}`}
          graph={focusedGraph ?? graph}
        />
      </ReactFlowProvider>
      {graph.evidence.length ? (
        <section aria-labelledby="resource-evidence-title">
          <Typography.Text id="resource-evidence-title" strong>
            {localeCode === 'zh_CN' ? '最近证据' : 'Recent evidence'}
          </Typography.Text>
          <ul className="m-0 mt-2 list-none p-0">
            {graph.evidence.slice(0, 20).map((item, index) => (
              <li
                className="flex flex-wrap items-center justify-between gap-2 border-0 border-b border-solid border-[var(--ant-color-split)] py-2 last:border-b-0"
                key={`${item.observedAt}-${item.summary}-${index}`}
              >
                <Space>
                  <StatusTag value={item.severity} />
                  <Typography.Text>{item.summary}</Typography.Text>
                </Space>
                <Typography.Text type="secondary">
                  <time dateTime={item.observedAt}>
                    {new Date(item.observedAt).toLocaleString()}
                  </time>
                </Typography.Text>
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </div>
  )
}
