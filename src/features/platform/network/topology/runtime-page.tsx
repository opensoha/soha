import { localeText, useI18n } from '@/i18n'
import { AdminTable } from '@/components/admin-table'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import {
  Background,
  Controls,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  type Node,
  type NodeProps,
  type Edge,
  type Viewport,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import {
  Alert,
  Button,
  Card,
  Drawer,
  Grid,
  Input,
  Segmented,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
  Tooltip,
  type TableColumnsType,
} from 'antd'
import dagre from 'dagre'
import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  connectedPath,
  edgeLabel,
  filterTopology,
  resolutionLabels,
  topologyEntrySummary,
} from './model'
import { topologyQueries } from './queries'
import type { TopologyNode, TopologyEdge } from './types'
import { NodeEvidence, Resolution } from './node-evidence'
import { TopologyCoveragePanel } from './coverage-panel'
import './topology.css'

const { Text } = Typography
const colors: Record<string, string> = {
  entry: 'var(--soha-graph-scope)',
  rule: 'var(--soha-graph-service)',
  processor: 'var(--soha-warning)',
  service: 'var(--soha-graph-metric)',
  pod: 'var(--soha-success)',
  external: 'var(--soha-graph-muted)',
}
type FlowNode = Node<
  { item: TopologyNode; entrySummary: string; onSelect: (id: string) => void },
  'topology'
>
function TopologyFlowNode({ data, selected }: NodeProps<FlowNode>) {
  const { localeCode, t } = useI18n()
  const { item } = data
  return (
    <div
      className={`soha-topology-node ${selected ? 'is-selected' : ''} ${data.entrySummary ? 'has-entry' : ''}`}
    >
      <Handle type="target" position={Position.Left} />
      <button
        type="button"
        className="soha-topology-node-card"
        onClick={() => data.onSelect(item.id)}
        style={{ borderLeftColor: colors[item.type] ?? 'var(--soha-border-color)' }}
      >
        <span className="soha-topology-node-head">
          <span>
            {item.provider} · {item.protocol || item.type}
          </span>
          <span>
            {t(
              `network.topology.resolution.${item.resolutionState}`,
              resolutionLabels[item.resolutionState],
            )}
          </span>
        </span>
        <span className="soha-topology-node-title">{item.label}</span>
        {data.entrySummary && (
          <span className="soha-topology-node-entry" title={data.entrySummary}>
            {data.entrySummary}
          </span>
        )}
        <span
          className="soha-topology-node-subtitle"
          title={`${item.resource?.namespace || localeText(localeCode, '集群', 'Cluster')} · ${item.resource?.kind || item.type}`}
        >
          {item.resource?.namespace || localeText(localeCode, '集群', 'Cluster')} ·{' '}
          {item.resource?.kind || item.type}
        </span>
      </button>
      <Handle type="source" position={Position.Right} />
    </div>
  )
}
const nodeTypes = { topology: TopologyFlowNode }

function TopologyCanvas({
  nodes,
  edges,
  onSelect,
  selected,
  viewport,
}: {
  nodes: TopologyNode[]
  edges: TopologyEdge[]
  onSelect: (id: string) => void
  selected: string
  viewport: { current: Viewport | undefined }
}) {
  const { localeCode } = useI18n()
  const entrySummaries = useMemo(
    () =>
      new Map(nodes.map((node) => [node.id, topologyEntrySummary(node, nodes, edges, localeCode)])),
    [nodes, edges, localeCode],
  )
  const shape = JSON.stringify([
    nodes.map((node) => [node.id, entrySummaries.get(node.id) ? 128 : 112]),
    edges.map((edge) => [edge.source, edge.target]),
  ])
  const positions = useMemo(() => {
    const [sizes, links] = JSON.parse(shape) as [[string, number][], [string, string][]]
    const graph = new dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}))
    graph.setGraph({ rankdir: 'LR', ranksep: 220, nodesep: 36 })
    sizes.forEach(([id, height]) => graph.setNode(id, { width: 252, height }))
    links.forEach(([source, target]) => graph.setEdge(source, target))
    dagre.layout(graph)
    return new Map(
      sizes.map(([id, height]) => {
        const point = graph.node(id)
        return [id, { x: point.x - 126, y: point.y - height / 2 }]
      }),
    )
  }, [shape])
  const path = selected ? connectedPath(new Set([selected]), edges) : undefined
  const flowNodes: FlowNode[] = nodes.map((item) => ({
    id: item.id,
    type: 'topology',
    position: positions.get(item.id) ?? { x: 0, y: 0 },
    selected: item.id === selected,
    data: { item, entrySummary: entrySummaries.get(item.id) ?? '', onSelect },
    style: { opacity: path && !path.has(item.id) ? 0.4 : 1 },
  }))
  const flowEdges: Edge[] = edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    label: edgeLabel(edge, localeCode),
    type: 'smoothstep',
    animated: false,
    markerEnd: { type: MarkerType.ArrowClosed },
    style: {
      stroke: 'var(--soha-text-tertiary)',
      opacity: path && (!path.has(edge.source) || !path.has(edge.target)) ? 0.3 : 1,
      strokeDasharray: edge.resolutionState === 'resolved' ? undefined : '6 4',
    },
    labelStyle: { fill: 'var(--soha-text-secondary)', fontSize: 11 },
    labelBgStyle: { fill: 'var(--soha-bg-surface)' },
  }))
  return (
    <div
      className="soha-topology-canvas"
      aria-label={localeText(
        localeCode,
        '配置关系图，可切换关系表使用键盘查看',
        'Configuration graph. Switch to the relationship table for keyboard access.',
      )}
    >
      <ReactFlow
        nodes={flowNodes}
        edges={flowEdges}
        nodeTypes={nodeTypes}
        nodesDraggable={false}
        nodesConnectable={false}
        defaultViewport={viewport.current}
        fitView={!viewport.current}
        fitViewOptions={{ maxZoom: 1, padding: 0.18 }}
        onMoveEnd={(_, value) => {
          viewport.current = value
        }}
        minZoom={0.15}
        maxZoom={1.8}
        onNodesChange={(changes) => {
          const selection = changes.find((change) => change.type === 'select' && change.selected)
          if (selection?.type === 'select') onSelect(selection.id)
          else if (
            changes.some(
              (change) => change.type === 'select' && change.id === selected && !change.selected,
            )
          )
            onSelect('')
        }}
        onNodeClick={(_, node) => onSelect(node.id)}
        onPaneClick={() => onSelect('')}
        onEdgeClick={(_, edge) => onSelect(edge.target)}
      >
        <Background />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}

export function NetworkTopologyRuntimePage() {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const permissionQuery = usePermissionSnapshot()
  const snapshot = readableQueryData(permissionQuery)?.data
  const canRead = hasPermission(snapshot, 'platform.network.topology.view')
  const principal = useAuthStore((state) => state.user?.userId || '')
  const permissionSignature = `${(snapshot?.permissionKeys ?? []).slice().sort().join(',')}@${permissionQuery.dataUpdatedAt}`
  const query = useQuery({
    ...topologyQueries.detail(toScopeKey(clusterId, namespace), principal, permissionSignature),
    enabled: Boolean(clusterId) && canRead,
  })
  const readableData = canRead ? readableQueryData(query) : undefined
  const data = readableData?.clusterId === clusterId ? readableData : undefined
  const [params, setParams] = useSearchParams()
  const loadedScope = useRef('')
  const currentScope = JSON.stringify([clusterId, namespace || null])
  const selectionScope = JSON.stringify([
    principal,
    clusterId,
    namespace || null,
    snapshot?.permissionKeys?.slice().sort(),
  ])
  const [selection, setSelection] = useState({ scope: '', id: '' })
  const selected = selection.scope === selectionScope ? selection.id : ''
  const selectionOrigin = useRef<HTMLElement | null>(null)
  const coverageButton = useRef<HTMLButtonElement>(null)
  const setSelected = (id: string) => {
    if (id && !selected) selectionOrigin.current = document.activeElement as HTMLElement | null
    setSelection({ scope: selectionScope, id })
  }
  const closeEvidence = () => {
    setSelected('')
    const origin = selectionOrigin.current
    if (origin?.isConnected) origin.focus()
    else coverageButton.current?.focus()
  }
  const viewports = useRef(new Map<string, { current: Viewport | undefined }>())
  if (!viewports.current.has(selectionScope))
    viewports.current.set(selectionScope, { current: undefined })
  const viewport = viewports.current.get(selectionScope)!
  const [coverageOpen, setCoverageOpen] = useState(false)
  const screens = Grid.useBreakpoint()
  const wide = Boolean(screens.xl)
  const mode = ['graph', 'table'].includes(params.get('view') || '')
    ? params.get('view')!
    : screens.md
      ? 'graph'
      : 'table'
  const provider = params.get('provider') || ''
  const protocol = params.get('protocol') || ''
  const keyword = useDeferredValue(params.get('q') || '')
  const issues = params.get('issues') === '1'
  const focus = params.get('focus') || ''
  useEffect(() => {
    if (!query.isSuccess || !data) return
    const previousScope = loadedScope.current
    loadedScope.current = currentScope
    if (
      !focus ||
      !previousScope ||
      previousScope === currentScope ||
      data.nodes?.some((node) => node.id === focus)
    )
      return
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        next.delete('focus')
        return next
      },
      { replace: true },
    )
  }, [data, focus, query.isSuccess, currentScope, setParams])
  const update = (key: string, value: string) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        if (value) next.set(key, value)
        else next.delete(key)
        return next
      },
      { replace: true },
    )
  const nodes = data?.nodes ?? []
  const edges = data?.edges ?? []
  const filtered = useMemo(
    () => filterTopology(nodes, edges, { provider, protocol, keyword, issues, focus }),
    [nodes, edges, provider, protocol, keyword, issues, focus],
  )
  const unassociatedNodes = useMemo(() => {
    const linkedIDs = new Set(filtered.edges.flatMap((edge) => [edge.source, edge.target]))
    return filtered.nodes.filter((node) => !linkedIDs.has(node.id))
  }, [filtered.nodes, filtered.edges])
  const showPods = params.get('pods') === '1' || Boolean(keyword.trim())
  const graphNodes = showPods
    ? filtered.nodes
    : filtered.nodes.filter((node) => node.type !== 'pod')
  const graphIDs = new Set(graphNodes.map((node) => node.id))
  const graphEdges = filtered.edges.filter(
    (edge) => graphIDs.has(edge.source) && graphIDs.has(edge.target),
  )
  const selectedNode = filtered.nodes.find((node) => node.id === selected)
  const failures = (data?.coverage ?? []).filter((item) => item.state !== 'available')
  const hasFilters = Boolean(provider || protocol || keyword.trim() || issues || focus)
  const resetFilters = () =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        for (const key of ['provider', 'protocol', 'q', 'issues', 'focus']) next.delete(key)
        return next
      },
      { replace: true },
    )
  const refreshState = query.isFetching
    ? 'loading'
    : query.isError
      ? 'error'
      : !data
        ? 'unknown'
        : failures.length || data.truncated || !data.graphVersion
          ? 'partial'
          : 'success'
  const refreshLabel = localeText(
    localeCode,
    query.isFetching
      ? '正在读取拓扑'
      : query.isError
        ? '读取失败，请重试'
        : !data
          ? '尚未读取拓扑'
          : refreshState === 'partial'
            ? '已读取，覆盖不完整'
            : '拓扑读取正常',
    query.isFetching
      ? 'Reading topology'
      : query.isError
        ? 'Read failed. Retry.'
        : !data
          ? 'Topology not loaded'
          : refreshState === 'partial'
            ? 'Loaded with incomplete coverage'
            : 'Topology read succeeded',
  )
  const refreshHint = `${refreshLabel} · ${localeText(localeCode, '手动刷新', 'Manual refresh')}${data ? ` · ${formatDateTime(data.generatedAt)}` : ''}`
  useEffect(() => {
    if (data && selected && !filtered.nodes.some((node) => node.id === selected)) {
      setSelection({ scope: selectionScope, id: '' })
    }
  }, [data, selected, filtered.nodes, selectionScope])
  const permissions: Record<string, string> = {
    Service: 'platform.network.services.view',
    Pod: 'platform.pods.view',
    Ingress: 'platform.network.ingresses.view',
    HTTPRoute: 'platform.network.http-routes.view',
    GRPCRoute: 'platform.network.grpc-routes.view',
    Gateway: 'platform.network.gateways.view',
    EndpointSlice: 'platform.network.endpoint-slices.view',
  }
  const evidence = selectedNode && (
    <NodeEvidence
      key={selectedNode.id}
      node={selectedNode}
      edges={edges}
      nodes={nodes}
      clusterId={clusterId || ''}
      canOpen={hasPermission(snapshot, permissions[selectedNode.resource?.kind || ''] || '')}
      canBrowseCRD={hasPermission(snapshot, 'platform.extensions.view')}
      canEdit={hasPermission(
        snapshot,
        (permissions[selectedNode.resource?.kind || ''] || '').replace(/\.view$/, '.update'),
      )}
      onFocus={() => update('focus', selectedNode.id)}
      onSelect={setSelected}
    />
  )
  const columns: TableColumnsType<TopologyEdge> = [
    {
      title: localeText(localeCode, '来源', 'Source'),
      key: 'source',
      width: 230,
      render: (_, edge) => (
        <Button type="link" onClick={() => setSelected(edge.source)}>
          {nodes.find((node) => node.id === edge.source)?.label ||
            localeText(localeCode, '不可见', 'Not visible')}
        </Button>
      ),
    },
    {
      title: localeText(localeCode, '目标', 'Target'),
      key: 'target',
      width: 230,
      render: (_, edge) => (
        <Button type="link" onClick={() => setSelected(edge.target)}>
          {nodes.find((node) => node.id === edge.target)?.label ||
            localeText(localeCode, '不可见', 'Not visible')}
        </Button>
      ),
    },
    {
      title: localeText(
        localeCode,
        '关系 / 协议 / 端口 / 权重',
        'Relationship / Protocol / Port / Weight',
      ),
      key: 'relation',
      width: 300,
      render: (_, edge) => edgeLabel(edge, localeCode),
    },
    {
      title: localeText(localeCode, '解析状态', 'Resolution'),
      dataIndex: 'resolutionState',
      width: 160,
      render: (value: string) => <Resolution value={value} />,
    },
  ]
  return (
    <div className="soha-page soha-topology-page">
      <ReadFeedback
        label={localeText(localeCode, '网络拓扑', 'Network topology')}
        query={query}
        canRead={canRead && Boolean(clusterId)}
      />
      {!clusterId ? (
        <ManagementState
          kind="select-scope"
          title={localeText(
            localeCode,
            '选择集群查看网络拓扑',
            'Select a cluster to view network topology',
          )}
        />
      ) : !canRead && !permissionQuery.isLoading ? (
        <ManagementState
          kind="error"
          title={localeText(
            localeCode,
            '没有网络拓扑查看权限',
            'Network topology permission is required',
          )}
        />
      ) : null}
      <h1 className="soha-topology-sr-only">
        {localeText(localeCode, '网络拓扑', 'Network topology')}
      </h1>
      <Card className="soha-detail-card soha-topology-filter-card">
        <div className="soha-topology-filters">
          <Input
            aria-label={localeText(localeCode, '搜索已加载节点', 'Search loaded nodes')}
            placeholder={localeText(
              localeCode,
              '搜索已加载的资源、命名空间或规则',
              'Search loaded resources, namespaces or rules',
            )}
            allowClear
            value={params.get('q') || ''}
            onChange={(event) => update('q', event.target.value)}
          />
          <Select
            aria-label="Provider"
            value={provider}
            onChange={(value) => update('provider', value)}
            options={[
              { value: '', label: localeText(localeCode, '全部 Provider', 'All providers') },
              ...['kubernetes', 'gateway', 'traefik', 'apisix'].map((value) => ({
                value,
                label: value,
              })),
            ]}
          />
          <Select
            aria-label={localeText(localeCode, '协议', 'Protocol')}
            value={protocol}
            onChange={(value) => update('protocol', value)}
            options={[
              { value: '', label: localeText(localeCode, '全部协议', 'All protocols') },
              ...['HTTP', 'GRPC', 'TCP', 'UDP', 'TLS'].map((value) => ({ value, label: value })),
            ]}
          />
          <Button onClick={resetFilters} disabled={!hasFilters}>
            {localeText(localeCode, '重置', 'Reset')}
          </Button>
        </div>
        {focus && (
          <Space>
            <Tag closable onClose={() => update('focus', '')}>
              {localeText(localeCode, '已聚焦路径', 'Focused path')}
            </Tag>
            <Text type="secondary">
              {nodes.find((node) => node.id === focus)?.label ||
                localeText(
                  localeCode,
                  '所选节点已不在当前快照中',
                  'The selected node is no longer in this snapshot',
                )}
            </Text>
          </Space>
        )}
      </Card>
      <div className={`soha-topology-workspace ${wide && selectedNode ? 'has-inspector' : ''}`}>
        <Card className="soha-detail-card soha-topology-main-card">
          <div className="soha-topology-toolbar">
            <span className="soha-topology-count" role="status">
              {localeText(localeCode, '已加载 ', 'Loaded ')}
              {nodes.length} {localeText(localeCode, '个资源', 'resources')} · {edges.length}{' '}
              {localeText(localeCode, '条关系', 'relationships')}
              {hasFilters && (
                <Text type="secondary">
                  {' '}
                  ·{' '}
                  {localeText(
                    localeCode,
                    `筛选后 ${filtered.nodes.length} 个资源`,
                    `${filtered.nodes.length} resources after filtering`,
                  )}
                </Text>
              )}
            </span>
            <Space wrap size={12}>
              <Button ref={coverageButton} onClick={() => setCoverageOpen(true)} disabled={!data}>
                {localeText(localeCode, '数据覆盖', 'Data coverage')}
                {failures.length ? ` · ${failures.length}` : ''}
              </Button>
              <Tooltip title={refreshHint} trigger={['hover', 'focus']}>
                <span
                  role="status"
                  tabIndex={0}
                  aria-label={refreshHint}
                  className={`soha-topology-read-status is-${refreshState}`}
                />
              </Tooltip>
              <ManagementRefreshButton
                aria-label={localeText(localeCode, '刷新网络拓扑', 'Refresh network topology')}
                tooltip={localeText(localeCode, '刷新网络拓扑', 'Refresh network topology')}
                disabled={!clusterId || !canRead}
                loading={query.isFetching}
                onClick={() => void query.refetch()}
              />
            </Space>
          </div>
          <div className="soha-topology-view-controls">
            <Space wrap size={16}>
              <Segmented
                aria-label={localeText(localeCode, '拓扑显示方式', 'Topology display')}
                value={mode}
                options={[
                  { label: localeText(localeCode, '关系图', 'Graph'), value: 'graph' },
                  { label: localeText(localeCode, '关系表', 'Table'), value: 'table' },
                ]}
                onChange={(value) => update('view', String(value))}
              />
              <Space size={6}>
                <Switch
                  size="small"
                  checked={issues}
                  onChange={(value) => update('issues', value ? '1' : '')}
                  aria-label={localeText(localeCode, '只看未解析节点', 'Show unresolved nodes')}
                />
                <Text>{localeText(localeCode, '未解析', 'Unresolved')}</Text>
              </Space>
              {mode === 'graph' && (
                <Space size={6}>
                  <Switch
                    size="small"
                    aria-label={localeText(localeCode, '展开 Pod 节点', 'Expand Pod nodes')}
                    checked={showPods}
                    onChange={(value) => update('pods', value ? '1' : '')}
                  />
                  <Text>{localeText(localeCode, '展开 Pod', 'Expand Pods')}</Text>
                </Space>
              )}
            </Space>
            <Tooltip
              title={localeText(
                localeCode,
                '筛选包含匹配资源的上下游；图中关系不表示实时流量。',
                'Filters include upstream and downstream context; relationships do not represent live traffic.',
              )}
            >
              <Text type="secondary" className="soha-topology-context">
                {localeText(localeCode, '配置关系', 'Configuration relationships')}
              </Text>
            </Tooltip>
          </div>
          {data && (!data.graphVersion || data.truncated || failures.length > 0) && (
            <Alert
              className="soha-topology-coverage-alert"
              type="warning"
              showIcon
              title={
                !data.graphVersion
                  ? localeText(
                      localeCode,
                      '旧版 Agent 仅提供资源摘要，请升级以查看规则级拓扑',
                      'This older Agent only provides summaries. Upgrade for rule-level topology.',
                    )
                  : data.truncated
                    ? localeText(
                        localeCode,
                        '已达到数据预算，请缩小命名空间范围',
                        'Data budget reached. Narrow the namespace scope.',
                      )
                    : localeText(
                        localeCode,
                        `${failures.length} 项来源不可完整读取，计数仅包含已加载内容`,
                        `${failures.length} sources have incomplete coverage. Counts cover loaded content only.`,
                      )
              }
              action={
                <Button size="small" onClick={() => setCoverageOpen(true)}>
                  {localeText(localeCode, '查看覆盖', 'View coverage')}
                </Button>
              }
            />
          )}
          {!clusterId ||
          (!canRead && !permissionQuery.isLoading) ? null : permissionQuery.isLoading ||
            (query.isPending && !data) ? (
            <ManagementState
              kind="loading"
              bordered={false}
              title={localeText(
                localeCode,
                '正在读取配置关系',
                'Reading configuration relationships',
              )}
            />
          ) : queryAccessDenied(query) ? (
            <ManagementState
              kind="error"
              title={localeText(
                localeCode,
                '当前范围的拓扑读取被拒绝',
                'Topology access was denied for this scope',
              )}
            />
          ) : query.isError && !data ? (
            <ManagementState
              kind="error"
              title={localeText(localeCode, '拓扑读取失败', 'Topology read failed')}
              description={localeText(
                localeCode,
                '请重试读取当前集群与命名空间。',
                'Retry reading this cluster and namespace.',
              )}
              actions={
                <Button onClick={() => void query.refetch()}>
                  {localeText(localeCode, '重试', 'Retry')}
                </Button>
              }
            />
          ) : !data || !data.graphVersion ? (
            <ManagementState
              bordered={false}
              title={localeText(localeCode, '没有规则级拓扑数据', 'No rule-level topology data')}
              description={localeText(
                localeCode,
                '请确认当前集群与 Agent 的拓扑能力。',
                'Check topology support for this cluster and Agent.',
              )}
            />
          ) : nodes.length === 0 && failures.length > 0 ? (
            <ManagementState
              bordered={false}
              title={localeText(
                localeCode,
                '尚未读取到拓扑资源',
                'Topology resources could not be loaded',
              )}
              description={localeText(
                localeCode,
                '部分来源读取失败或不可访问，当前结果无法确认资源是否为空。',
                'Some sources failed or are inaccessible. This result cannot establish an empty inventory.',
              )}
              actions={
                <Button onClick={() => setCoverageOpen(true)}>
                  {localeText(localeCode, '检查数据覆盖', 'Check data coverage')}
                </Button>
              }
            />
          ) : filtered.nodes.length === 0 ? (
            <ManagementState
              bordered={false}
              title={
                hasFilters
                  ? localeText(
                      localeCode,
                      '没有匹配的已加载资源',
                      'No loaded resources match these filters',
                    )
                  : localeText(
                      localeCode,
                      '当前范围没有拓扑资源',
                      'No topology resources in this scope',
                    )
              }
              description={
                hasFilters
                  ? localeText(
                      localeCode,
                      '调整筛选可查看其他已加载关系。',
                      'Adjust filters to inspect other loaded relationships.',
                    )
                  : localeText(
                      localeCode,
                      '已读取的来源未返回资源，可切换命名空间或刷新。',
                      'Loaded sources returned no resources. Change namespace or refresh.',
                    )
              }
              actions={
                hasFilters && (
                  <Button onClick={resetFilters}>
                    {localeText(localeCode, '清除筛选', 'Clear filters')}
                  </Button>
                )
              }
            />
          ) : mode === 'graph' && graphNodes.length === 0 ? (
            <ManagementState
              bordered={false}
              title={localeText(
                localeCode,
                '当前资源均为已收起的 Pod',
                'All matching resources are collapsed Pods',
              )}
              description=""
              actions={
                <Button onClick={() => update('pods', '1')}>
                  {localeText(localeCode, '展开 Pod', 'Expand Pods')}
                </Button>
              }
            />
          ) : mode === 'graph' ? (
            <TopologyCanvas
              key={selectionScope}
              viewport={viewport}
              nodes={graphNodes}
              edges={graphEdges}
              onSelect={setSelected}
              selected={selected}
            />
          ) : (
            <AdminTable
              rowKey="id"
              columns={columns}
              dataSource={filtered.edges}
              enableColumnSelection={false}
              pagination={false}
              virtual
              scroll={{ x: 920, y: 520 }}
            />
          )}
          {mode === 'table' && unassociatedNodes.length > 0 && (
            <details className="soha-topology-isolated" key={`${clusterId}:${namespace}`}>
              <summary>
                {localeText(
                  localeCode,
                  `未关联资源（${unassociatedNodes.length}）`,
                  `Unassociated resources (${unassociatedNodes.length})`,
                )}
              </summary>
              <div className="soha-topology-isolated-list">
                {unassociatedNodes.map((node) => (
                  <Button key={node.id} size="small" onClick={() => setSelected(node.id)}>
                    {node.label}
                  </Button>
                ))}
              </div>
            </details>
          )}
        </Card>
        {wide && selectedNode && (
          <Card
            className="soha-detail-card soha-topology-inspector"
            title={localeText(localeCode, '关系证据', 'Relationship evidence')}
            extra={
              <Button type="text" onClick={closeEvidence}>
                {localeText(localeCode, '关闭', 'Close')}
              </Button>
            }
          >
            {evidence}
          </Card>
        )}
      </div>
      <Drawer
        title={localeText(localeCode, '关系证据', 'Relationship evidence')}
        open={!wide && Boolean(selectedNode)}
        size="min(400px, 100vw)"
        onClose={closeEvidence}
      >
        {evidence}
      </Drawer>
      <Drawer
        title={localeText(localeCode, '数据覆盖', 'Data coverage')}
        open={coverageOpen}
        onClose={() => setCoverageOpen(false)}
        size="min(680px, 100vw)"
      >
        <TopologyCoveragePanel
          key={selectionScope}
          coverage={data?.coverage ?? []}
          generatedAt={data?.generatedAt}
          observedAt={data?.observedAt}
        />
      </Drawer>
    </div>
  )
}
