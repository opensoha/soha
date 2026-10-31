import { localeText, useI18n } from '@/i18n'
import { StatusTag } from '@/components/status-tag'
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
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import {
  Alert,
  Button,
  Card,
  Drawer,
  Empty,
  Grid,
  Input,
  Segmented,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
  type TableColumnsType,
} from 'antd'
import dagre from 'dagre'
import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  connectedPath,
  coverageLabels,
  edgeLabel,
  filterTopology,
  resolutionLabels,
  topologyEntrySummary,
} from './model'
import { topologyQueries } from './queries'
import type { TopologyNode, TopologyEdge } from './types'
import { NodeEvidence, Resolution } from './node-evidence'
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
}: {
  nodes: TopologyNode[]
  edges: TopologyEdge[]
  onSelect: (id: string) => void
  selected: string
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
    graph.setGraph({ rankdir: 'LR', ranksep: 90, nodesep: 36 })
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
        fitView
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
  const { localeCode, t } = useI18n()
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
  const data = canRead ? readableQueryData(query) : undefined
  const [params, setParams] = useSearchParams()
  const loadedScope = useRef('')
  const currentScope = JSON.stringify([clusterId, namespace || null])
  const [selected, setSelected] = useState('')
  const [coverageOpen, setCoverageOpen] = useState(false)
  const screens = Grid.useBreakpoint()
  const wide = Boolean(screens.xl)
  const mode = params.get('view') || (screens.md ? 'graph' : 'table')
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
  const selectedNode = nodes.find((node) => node.id === selected)
  const failures = (data?.coverage ?? []).filter((item) => item.state !== 'available')
  const ready = clusterId && canRead && !query.isLoading
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
      render: (_, edge) => edgeLabel(edge, localeCode),
    },
    {
      title: localeText(localeCode, '解析状态', 'Resolution'),
      dataIndex: 'resolutionState',
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
      <Card className="soha-detail-card" styles={{ body: { padding: 16 } }}>
        <div className="soha-topology-toolbar">
          <div>
            <Typography.Title level={4} style={{ margin: 0 }}>
              {localeText(localeCode, '网络拓扑', 'Network topology')}
            </Typography.Title>
            <Text type="secondary">
              {localeText(
                localeCode,
                '配置关系与控制器观测 · 已加载',
                'Configuration and controller observations · Loaded ',
              )}
              {nodes.length} {localeText(localeCode, '节点 /', ' nodes / ')}
              {edges.length} {localeText(localeCode, '关系', ' relationships')}
            </Text>
          </div>
          <Space wrap>
            <Button onClick={() => setCoverageOpen(true)} disabled={!data}>
              {localeText(localeCode, '数据覆盖', 'Data coverage')}
              {failures.length
                ? ` · ${failures.length} ${localeText(localeCode, '项需关注', 'need attention')}`
                : ''}
            </Button>
            <ManagementRefreshButton
              aria-label={localeText(localeCode, '刷新网络拓扑', 'Refresh network topology')}
              tooltip={localeText(localeCode, '刷新网络拓扑', 'Refresh network topology')}
              disabled={!clusterId || !canRead}
              loading={query.isFetching}
              onClick={() => void query.refetch()}
            />
          </Space>
        </div>
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
          <Space>
            <Switch
              checked={issues}
              onChange={(value) => update('issues', value ? '1' : '')}
              aria-label={localeText(localeCode, '只看未解析节点', 'Show unresolved nodes')}
            />
            <Text>{localeText(localeCode, '未解析', 'Unresolved')}</Text>
          </Space>
          {mode === 'graph' && (
            <Space>
              <Switch
                aria-label={localeText(localeCode, '展开 Pod 节点', 'Expand Pod nodes')}
                checked={showPods}
                onChange={(value) => update('pods', value ? '1' : '')}
              />
              <Text>{localeText(localeCode, '展开 Pod', 'Expand Pods')}</Text>
            </Space>
          )}
          <Segmented
            aria-label={localeText(localeCode, '拓扑显示方式', 'Topology display')}
            value={mode}
            options={[
              { label: localeText(localeCode, '关系图', 'Graph'), value: 'graph' },
              { label: localeText(localeCode, '关系表', 'Table'), value: 'table' },
            ]}
            onChange={(value) => update('view', String(value))}
          />
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
        {data && (!data.graphVersion || data.truncated || failures.length > 0) && (
          <Alert
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
                      '当前结果已达到预算，请缩小命名空间范围',
                      'The result reached its budget. Narrow the namespace scope.',
                    )
                  : localeText(
                      localeCode,
                      '部分来源不可读取，请查看数据覆盖',
                      'Some sources could not be read. Check data coverage.',
                    )
            }
            description={localeText(
              localeCode,
              '计数仅包含当前已加载内容；未观测到的引用不会判定为不存在。',
              'Counts cover loaded content only. Unobserved references are not treated as absent.',
            )}
          />
        )}
        {data && (
          <Text type="secondary" className="soha-topology-time">
            {localeText(localeCode, '生成于', 'Generated ')}
            {formatDateTime(data.generatedAt)}{' '}
            {localeText(localeCode, '· 来源采样', ' · Source observed ')}
            {data.observedAt
              ? formatDateTime(data.observedAt)
              : localeText(localeCode, '未提供', 'Unavailable')}{' '}
            {localeText(
              localeCode,
              '· 图中关系不表示实时流量',
              ' · Relationships do not represent live traffic',
            )}
          </Text>
        )}
      </Card>
      <div className={`soha-topology-workspace ${wide && selectedNode ? 'has-inspector' : ''}`}>
        <Card className="soha-detail-card" styles={{ body: { padding: 12 } }}>
          {queryAccessDenied(query) ? (
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
              title={localeText(
                localeCode,
                '拓扑读取失败，请查看请求反馈后重试',
                'Topology read failed. Check request feedback and retry.',
              )}
            />
          ) : ready && filtered.nodes.length === 0 ? (
            <Empty
              description={
                data?.graphVersion
                  ? localeText(
                      localeCode,
                      '当前筛选范围没有已加载节点',
                      'No loaded nodes match these filters',
                    )
                  : localeText(localeCode, '没有规则级拓扑数据', 'No rule-level topology data')
              }
            />
          ) : mode === 'graph' ? (
            <TopologyCanvas
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
              scroll={{ x: 1200, y: 520 }}
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
            className="soha-detail-card"
            title={localeText(localeCode, '关系证据', 'Relationship evidence')}
            extra={
              <Button type="text" onClick={() => setSelected('')}>
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
        onClose={() => setSelected('')}
      >
        {evidence}
      </Drawer>
      <Drawer
        title={localeText(localeCode, '数据覆盖', 'Data coverage')}
        open={coverageOpen}
        onClose={() => setCoverageOpen(false)}
        size="min(500px, 100vw)"
      >
        <Text type="secondary">
          {localeText(
            localeCode,
            '读取权限、API 安装和数据预算分别影响覆盖范围。插件读取需要 Agent 动作、已保存的自定义资源授权及 Kubernetes RBAC。',
            'Read permissions, installed APIs and data budgets determine coverage. Provider reads require the Agent action, saved custom-resource grants and Kubernetes RBAC.',
          )}
        </Text>
        <Typography.Paragraph style={{ marginTop: 12 }}>
          Traefik：<Text code>traefik.io</Text>{' '}
          {localeText(
            localeCode,
            '的 ingressroutes、ingressroutetcps、ingressrouteudps、traefikservices、middlewares、middlewaretcps。APISIX：',
            ' resources: ingressroutes, ingressroutetcps, ingressrouteudps, traefikservices, middlewares, middlewaretcps. APISIX: ',
          )}
          <Text code>apisix.apache.org</Text>{' '}
          {localeText(
            localeCode,
            '的 apisixroutes、apisixupstreams、apisixtls。拓扑所需最小资源动作为 list，Agent 还需',
            ' resources: apisixroutes, apisixupstreams, apisixtls. The minimum resource verb is list; Agent also requires ',
          )}{' '}
          <Text code>platform.custom_resources.list</Text>
          {localeText(
            localeCode,
            '。先在集群保存指定 namespace 的授权，再审查并应用生成的 RBAC；保存配置不会自动扩大运行中 Agent 权限。',
            '. Save the required namespace grants in cluster settings, then review and apply generated RBAC. Saving settings does not expand the running Agent permissions.',
          )}
        </Typography.Paragraph>
        {(data?.coverage ?? []).map((item, index) => (
          <div
            className="soha-topology-coverage"
            key={`${item.sourceId}:${item.namespace}:${index}`}
          >
            <Space wrap>
              <Text strong>{item.sourceId}</Text>
              <StatusTag
                value={item.state === 'available' ? 'healthy' : 'warning'}
                label={t(
                  `network.topology.coverage.${item.state}`,
                  coverageLabels[item.state] || item.state,
                )}
              />
            </Space>
            <Text>
              {item.namespace ||
                localeText(localeCode, '查询范围全部命名空间', 'All namespaces in scope')}
            </Text>
            <Text type="secondary">
              {item.reasonCode || localeText(localeCode, '已完成读取', 'Read completed')}
              {item.truncated ? localeText(localeCode, '· 已截断', ' · Truncated') : ''}
            </Text>
            <Text type="secondary">
              {item.observedAt || item.checkedAt
                ? formatDateTime(item.observedAt || item.checkedAt)
                : localeText(localeCode, '采样时间未知', 'Observation time unknown')}
            </Text>
          </div>
        ))}
      </Drawer>
    </div>
  )
}
