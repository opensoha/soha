import { KubernetesResourceTable } from '../shared/kubernetes-resource-table'
import {
  ManagementQueryField,
  ManagementState,
  ManagementTableToolbar,
  ManagementRefreshButton,
} from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { TableCellLink } from '@/components/table-cell-content'
import { NodeStateTags } from '../shared/node-state-tags'
import { NodeResourcePanel } from '../node-resource-utils'
import { useAIPageContext } from '@/features/copilot'
import { useKubernetesResourceStream } from '../shared/resource-stream'
import { ResourceStreamStatus } from '../shared/resource-stream-status'
import {
  WorkloadQueryPanel,
  WorkloadSearchInput,
  useWorkloadTableDensity,
} from '../workloads/shared/list-controls'
import '../styles/base.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Grid, Select, Space, Tooltip, type TableColumnsType } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { nodeQueries } from './queries'
import { toClusterScope } from './scope'
import type { ClusterNode } from './types'
import { useNodeActions } from './use-node-actions'
import { useNodeMonitoring } from './use-node-monitoring'
import { filterNodes, nodeUsage } from './node-list-model'
import { NodeTaints, NodeUsageCell } from './node-cells'
import './nodes-list-page.css'
import './node-detail.css'

export function ClusterNodesPage() {
  const { t, localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const navigate = useNavigate()
  const pinColumns = Boolean(Grid.useBreakpoint().md)
  const queryClient = useQueryClient()
  const { clusterId } = usePlatformScopeStore()
  const scope = toClusterScope(clusterId)
  const [keyword, setKeyword] = useState('')
  const [ready, setReady] = useState('')
  const [scheduling, setScheduling] = useState('')
  const [role, setRole] = useState('')
  const reset = () => {
    setKeyword('')
    setReady('')
    setScheduling('')
    setRole('')
  }
  useEffect(() => {
    setKeyword('')
    setReady('')
    setScheduling('')
    setRole('')
  }, [clusterId])
  const { tableSize, densityButton } = useWorkloadTableDensity(localeCode)
  const nodeListQuery = nodeQueries.list(scope)
  const nodeStream = useKubernetesResourceStream({
    clusterId,
    kinds: ['Node', 'Pod', 'Event'],
    onEvent: () => void queryClient.invalidateQueries({ queryKey: nodeListQuery.queryKey }),
    onFallback: () => void queryClient.invalidateQueries({ queryKey: nodeListQuery.queryKey }),
    onResyncRequired: () => queryClient.invalidateQueries({ queryKey: nodeListQuery.queryKey }),
  })
  const nodesQuery = useQuery(nodeListQuery)
  const nodes = readableQueryData(nodesQuery) ?? []
  const actions = useNodeActions({ scope, nodes })
  const monitoring = useNodeMonitoring(clusterId)
  const rows = useMemo(
    () => filterNodes(nodes, keyword, ready, scheduling, role),
    [nodes, keyword, ready, scheduling, role],
  )
  const nameCollator = useMemo(
    () => new Intl.Collator(zh ? 'zh-CN' : 'en-US', { numeric: true, sensitivity: 'base' }),
    [zh],
  )
  const detailPath = (name: string, tab = 'overview') =>
    `/cluster-resources/nodes/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId: clusterId ?? '', tab })}`
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: zh ? '集群节点' : 'Cluster nodes',
    entityKind: 'kubernetes.node-list',
    entityName: clusterId || 'nodes',
    clusterId: clusterId ?? undefined,
    node: actions.editingNodeName ?? undefined,
    visibleFilters: { keyword, ready, scheduling, role },
    pinnedData: { nodeCount: nodes.length },
  })
  const columns: TableColumnsType<ClusterNode> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 300,
      fixed: pinColumns ? 'left' : undefined,
      ellipsis: { showTitle: false },
      defaultSortOrder: 'ascend',
      sorter: (a, b) => nameCollator.compare(a.name, b.name),
      render: (name: string) => (
        <Tooltip title={name} trigger={['hover', 'focus']}>
          <Link className="soha-node-name" to={detailPath(name)}>
            {name}
          </Link>
        </Tooltip>
      ),
    },
    {
      title: zh ? '状态' : 'Status',
      dataIndex: 'status',
      width: 160,
      render: (_, n) => (
        <NodeStateTags
          status={n.status}
          cordoned={n.unschedulable}
          conditions={n.conditions}
          draining={actions.isDraining(n.name)}
        />
      ),
    },
    {
      title: 'CPU',
      key: 'hostCpu',
      width: 148,
      sorter: (a, b) =>
        (nodeUsage(monitoring.data, clusterId, a.name, 'cpu').value ?? -1) -
        (nodeUsage(monitoring.data, clusterId, b.name, 'cpu').value ?? -1),
      render: (_, n) => (
        <NodeUsageCell
          kind="cpu"
          usage={nodeUsage(monitoring.data, clusterId, n.name, 'cpu')}
          canRead={monitoring.canRead}
          failed={monitoring.query.isError}
        />
      ),
    },
    {
      title: zh ? '内存' : 'Memory',
      key: 'hostMemory',
      width: 148,
      sorter: (a, b) =>
        (nodeUsage(monitoring.data, clusterId, a.name, 'memory').value ?? -1) -
        (nodeUsage(monitoring.data, clusterId, b.name, 'memory').value ?? -1),
      render: (_, n) => (
        <NodeUsageCell
          kind="memory"
          usage={nodeUsage(monitoring.data, clusterId, n.name, 'memory')}
          canRead={monitoring.canRead}
          failed={monitoring.query.isError}
        />
      ),
    },
    {
      title: 'Pods',
      dataIndex: 'podCount',
      width: 80,
      render: (count: number, n) => (
        <TableCellLink label={String(count)} onClick={() => navigate(detailPath(n.name, 'pods'))} />
      ),
    },
    {
      title: zh ? '角色' : 'Roles',
      dataIndex: 'roles',
      width: 160,
      render: (roles: string[]) => (
        <Space size={4} wrap>
          {roles?.length ? roles.map((r) => <MetadataTag key={r} label={r} />) : '—'}
        </Space>
      ),
    },
    { title: 'IP', dataIndex: 'internalIp', width: 144, render: (v: string) => v || '—' },
    {
      title: zh ? '版本' : 'Version',
      dataIndex: 'version',
      width: 130,
      render: (v: string) => v || '—',
    },
    {
      title: zh ? '污点' : 'Taints',
      key: 'taints',
      width: 180,
      render: (_, n) => <NodeTaints taints={n.taints} compact />,
    },
    {
      title: zh ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      width: 68,
      render: (v: number) => formatAgeSeconds(v),
    },
    actions.column,
  ]
  return (
    <div className="soha-page soha-cluster-nodes-page">
      <WorkloadQueryPanel
        localeCode={localeCode}
        hasActiveFilters={Boolean(keyword || ready || scheduling || role)}
        onReset={reset}
      >
        <WorkloadSearchInput
          label={zh ? '关键词' : 'Keyword'}
          value={keyword}
          onChange={setKeyword}
          placeholder={zh ? '搜索名称 / IP / 标签 / 污点' : 'Name / IP / labels / taints'}
          width={260}
        />
        <ManagementQueryField label={zh ? '状态' : 'Status'}>
          <Select
            aria-label={zh ? '节点状态' : 'Node status'}
            value={ready}
            onChange={setReady}
            options={[
              { value: '', label: zh ? '全部状态' : 'All states' },
              { value: 'ready', label: 'Ready' },
              { value: 'not-ready', label: zh ? '未就绪 / 未知' : 'Not ready / unknown' },
            ]}
          />
        </ManagementQueryField>
        <ManagementQueryField label={zh ? '调度' : 'Scheduling'}>
          <Select
            aria-label={zh ? '节点调度' : 'Node scheduling'}
            value={scheduling}
            onChange={setScheduling}
            options={[
              { value: '', label: zh ? '全部调度状态' : 'All scheduling states' },
              { value: 'schedulable', label: zh ? '允许调度' : 'Schedulable' },
              { value: 'cordoned', label: zh ? '禁止调度' : 'Cordoned' },
            ]}
          />
        </ManagementQueryField>
        <ManagementQueryField label={zh ? '角色' : 'Role'}>
          <Select
            aria-label={zh ? '节点角色' : 'Node role'}
            value={role}
            onChange={setRole}
            options={[
              { value: '', label: zh ? '全部角色' : 'All roles' },
              ...[...new Set(nodes.flatMap((n) => n.roles ?? []))]
                .sort()
                .map((r) => ({ value: r, label: r })),
            ]}
          />
        </ManagementQueryField>
      </WorkloadQueryPanel>
      {actions.feedback}
      {!clusterId ? (
        <ManagementState
          compact
          kind="select-scope"
          title={t('common.pleaseSelectCluster', '请选择集群')}
        />
      ) : (
        <KubernetesResourceTable
          resourceKey="nodes"
          resourceLabel={t('common.nodes', 'Nodes')}
          allRows={nodes}
          filterKey={JSON.stringify([keyword, ready, scheduling, role])}
          scopeMode="cluster"
          error={nodesQuery.error}
          hasReadData={nodesQuery.data !== undefined}
          onRetry={() => nodesQuery.refetch()}
          columnSettingIconOnly
          columnSettingPlacement="header"
          shellClassName="soha-management-table-shell"
          headerExtra={
            <ManagementTableToolbar>
              <ResourceStreamStatus
                status={nodeStream.status}
                lastEventAt={nodeStream.lastEventAt}
                localeCode={localeCode}
                refreshing={
                  nodesQuery.isFetching || (monitoring.canRead && monitoring.query.isFetching)
                }
                refreshFailed={
                  nodesQuery.isError || (monitoring.canRead && monitoring.query.isError)
                }
              />
              {densityButton}
              <ManagementRefreshButton
                aria-label={zh ? '刷新节点' : 'Refresh nodes'}
                loading={nodesQuery.isFetching}
                tooltip={zh ? '刷新' : 'Refresh'}
                onClick={() =>
                  void Promise.all([
                    nodesQuery.refetch(),
                    ...(monitoring.canRead ? [monitoring.query.refetch()] : []),
                  ])
                }
              />
            </ManagementTableToolbar>
          }
          columns={columns}
          dataSource={rows}
          rowKey="name"
          loading={nodesQuery.isLoading}
          localSorting
          tableSize={tableSize}
          tableLayout="fixed"
          scroll={{ x: 1700 }}
          expandable={{
            columnWidth: 48,
            fixed: pinColumns ? 'left' : undefined,
            expandedRowRender: (node: ClusterNode) => <NodeResourcePanel node={node} />,
          }}
        />
      )}
      {actions.dialogs}
    </div>
  )
}
