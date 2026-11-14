import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Spin, Typography, Tooltip } from 'antd'
import type { TableColumnsType } from 'antd'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { TableCellLink, TableCellText } from '@/components/table-cell-content'
import { ManagementToolbarSearch } from '@/components/management-list'
import { ClusterConnectionPanel } from './connection-panel'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAIPageContext } from '@/features/copilot'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { WorkloadDetailHeader } from '../workloads/shared/workload-detail-header'
import { ResourceDetailTabs, useResourceDetailTab } from '../shared/resource-detail-header'
import { WorkloadRefresh, useWorkloadRefresh } from '../workloads/shared/workload-refresh'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewFacts,
  WorkloadOverviewMetadata,
} from '../workloads/shared/workload-overview'
import { KubernetesResourceTable } from '../shared/kubernetes-resource-table'
import { ClusterEditor } from './cluster-editor'
import { PrometheusCheckResult, useClusterPrometheusCheck } from './prometheus-check'
import { LogCollectionCard } from './log-collection-card'
import { AgentActionsCard } from './agent-actions-card'
import {
  clusterTypeOf,
  formatClusterHealth,
  formatClusterType,
  formatConnectionMode,
} from './presentation'
import { clusterQueries } from './queries'
import type { Node } from './types'
import './styles.css'

const { Text } = Typography
const tabs = ['overview', 'nodes', 'monitoring', 'connection', 'logging']

export function ClusterDetailPage() {
  const { clusterId = '' } = useParams()
  return <ClusterDetailContent key={clusterId} clusterId={clusterId} />
}
function ClusterDetailContent({ clusterId }: { clusterId: string }) {
  const { t, localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const navigate = useNavigate()
  const setClusterId = usePlatformScopeStore((s) => s.setClusterId)
  const [tab, setTab] = useResourceDetailTab()
  const activeTab = tabs.includes(tab) ? tab : 'overview'
  const [editingTarget, setEditingTarget] = useState<string>()
  const [checkingTarget, setCheckingTarget] = useState<string>()
  const editing = editingTarget === clusterId
  const checking = checkingTarget === clusterId
  const permissionQuery = usePermissionSnapshot()
  const permissions = readableQueryData(permissionQuery)?.data
  const canRead = hasPermission(permissions, 'platform.clusters.view')
  const canUpdate = hasPermission(permissions, 'platform.clusters.update')
  const canReadNodes = canRead && hasPermission(permissions, 'platform.nodes.view')
  const refresh = useWorkloadRefresh()
  const detailScope = toScopeKey(clusterId, null)
  const clusterDetailQuery = useQuery({
    ...clusterQueries.detail(detailScope),
    enabled: Boolean(clusterId && canRead),
    refetchInterval: refresh.interval,
  })
  const nodesQuery = useQuery({
    ...clusterQueries.nodes(detailScope),
    enabled: Boolean(clusterId && canReadNodes && ['overview', 'nodes'].includes(activeTab)),
    refetchInterval: refresh.interval,
  })
  const candidate = canRead ? readableQueryData(clusterDetailQuery) : undefined
  const identityMismatch = Boolean(candidate && candidate.summary.id !== clusterId)
  const detail = !identityMismatch ? candidate : undefined
  const [nodeSearch, setNodeSearch] = useState('')
  const summary = detail?.summary
  const nodes = canReadNodes && !nodesQuery.isError ? readableQueryData(nodesQuery) : undefined
  const monitoring = useClusterPrometheusCheck(
    clusterId,
    activeTab === 'monitoring' && checking && canRead,
  )
  useEffect(() => {
    if (clusterId) setClusterId(clusterId)
    setCheckingTarget(undefined)
    setEditingTarget(undefined)
  }, [clusterId, setClusterId])
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: zh ? '集群详情' : 'Cluster detail',
    entityKind: 'Cluster',
    entityName: summary?.name || clusterId,
    clusterId,
  })
  const nodeColumns: TableColumnsType<Node> = [
    {
      title: localeCode === 'zh_CN' ? '节点' : 'Node',
      dataIndex: 'name',
      key: 'name',
      width: 260,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (value: string) => (
        <TableCellLink
          label={value}
          onClick={() =>
            navigate(
              `/cluster-resources/nodes/${encodeURIComponent(value)}?${new URLSearchParams({ clusterId })}`,
            )
          }
        />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'status',
      key: 'status',
      width: 120,
      sorter: (a, b) => (a.status || '').localeCompare(b.status || ''),
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '角色' : 'Roles',
      dataIndex: 'roles',
      key: 'roles',
      width: 160,
      sorter: (a, b) => (a.roles || []).join(',').localeCompare((b.roles || []).join(',')),
      render: (roles: string[]) => <TableCellText value={roles?.join(', ')} />,
    },
    {
      title: localeCode === 'zh_CN' ? '版本' : 'Version',
      dataIndex: 'version',
      key: 'version',
      width: 150,
      sorter: (a, b) => (a.version || '').localeCompare(b.version || ''),
      render: (value: string) => value || '-',
    },
    {
      title: 'IP',
      dataIndex: 'internalIp',
      key: 'internalIp',
      width: 170,
      sorter: (a, b) => (a.internalIp || '').localeCompare(b.internalIp || ''),
      render: (value: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? 'Pod 数量' : 'Pods',
      dataIndex: 'podCount',
      key: 'podCount',
      width: 100,
      sorter: (a, b) => (a.podCount ?? 0) - (b.podCount ?? 0),
    },
  ]

  if (permissionQuery.isPending || clusterDetailQuery.isLoading) return <Spin size="large" />
  if (!canRead)
    return (
      <ManagementState
        compact
        kind="error"
        title={zh ? '需要集群查看权限' : 'Cluster view permission required'}
      />
    )
  if (identityMismatch)
    return (
      <ManagementState
        kind="error"
        title={zh ? '集群详情身份不匹配' : 'Cluster identity mismatch'}
        actions={
          <Button onClick={() => void clusterDetailQuery.refetch()}>{zh ? '重试' : 'Retry'}</Button>
        }
      />
    )
  if (!detail || !summary)
    return (
      <>
        <ReadFeedback
          canRead
          query={clusterDetailQuery}
          label={zh ? '集群详情' : 'Cluster detail'}
        />
        {!clusterDetailQuery.isError && (
          <ManagementState kind="not-found" description={t('common.notFound', 'Not found')} />
        )}
      </>
    )
  const activeQueries = [clusterDetailQuery, ...(nodesQuery.isEnabled ? [nodesQuery] : [])]
  const reload = () => Promise.all(activeQueries.map((q) => q.refetch()))
  const monitoringConfig = detail.monitoring.prometheus
  const overview = (
    <WorkloadOverview
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '集群信息' : 'Cluster information'}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '集群 ID' : 'Cluster ID',
                  children: <Text copyable>{summary.id}</Text>,
                },
                { label: zh ? '版本' : 'Version', children: summary.version || '—' },
                {
                  label: zh ? '类型' : 'Type',
                  children: formatClusterType(clusterTypeOf(summary), localeCode),
                },
                { label: zh ? '环境' : 'Environment', children: summary.environment || '—' },
                {
                  label: zh ? '连接方式' : 'Connection mode',
                  children: formatConnectionMode(summary.connectionMode, localeCode),
                },
              ]}
            />
          </Card>
          <Card
            className="soha-detail-card"
            title={zh ? '监控接入' : 'Monitoring'}
            extra={
              <Button type="link" onClick={() => setTab('monitoring')}>
                {zh ? '查看配置' : 'View configuration'}
              </Button>
            }
          >
            <Text>
              {monitoringConfig.baseUrl || (zh ? '未配置 Prometheus' : 'Prometheus not configured')}
            </Text>
          </Card>
        </>
      }
    >
      <Alert
        showIcon
        type={summary.health?.status === 'healthy' ? 'success' : 'info'}
        title={formatClusterHealth(summary.health?.status || 'unknown', localeCode)}
        description={summary.health?.message || undefined}
        action={
          <Button type="link" onClick={() => setTab('connection')}>
            {zh ? '连接诊断' : 'Diagnostics'}
          </Button>
        }
      />
      <Card
        className="soha-detail-card"
        title={zh ? '节点' : 'Nodes'}
        extra={
          <Button type="link" onClick={() => setTab('nodes')}>
            {zh ? '查看全部' : 'View all'}
          </Button>
        }
      >
        <ReadFeedback canRead={canReadNodes} query={nodesQuery} label={zh ? '节点' : 'Nodes'} />
        {canReadNodes ? (
          <>
            <WorkloadOverviewFacts
              facts={[
                { key: 'total', label: zh ? '全部节点' : 'All nodes', value: nodes?.length },
                {
                  key: 'ready',
                  label: zh ? '就绪' : 'Ready',
                  value: nodes?.filter((n) => n.status?.toLowerCase() === 'ready').length,
                },
              ]}
            />
            {nodes && (
              <AdminTable
                tableSize="small"
                columns={nodeColumns.slice(0, 3)}
                dataSource={nodes.slice(0, 5)}
                rowKey="name"
                pagination={false}
                enableDensity={false}
                enableColumnSelection={false}
                scroll={{ x: 600 }}
              />
            )}
          </>
        ) : (
          <Text type="secondary">{zh ? '需要节点查看权限' : 'Node view permission required'}</Text>
        )}
      </Card>
      <Card className="soha-detail-card" title={zh ? '能力' : 'Capabilities'}>
        <div className="soha-tag-list">
          {summary.capabilities?.length ? (
            summary.capabilities.map((capability) => (
              <MetadataTag key={capability} label={capability} />
            ))
          ) : (
            <Text type="secondary">
              {zh ? '无额外能力声明' : 'No explicit capabilities reported'}
            </Text>
          )}
        </div>
      </Card>
      <WorkloadOverviewMetadata labels={summary.labels} />
    </WorkloadOverview>
  )

  return (
    <div className="soha-page soha-workload-detail-page soha-workload-detail-frame soha-cluster-detail-page">
      <ReadFeedback canRead query={clusterDetailQuery} label={zh ? '集群详情' : 'Cluster detail'} />
      <WorkloadDetailHeader
        kind="Cluster"
        name={summary.name}
        backTo="/clusters"
        status={
          <StatusTag
            label={formatClusterHealth(summary.health?.status || 'unknown', localeCode)}
            value={summary.health?.status || 'unknown'}
          />
        }
        facts={
          <>
            <MetadataTag label={formatConnectionMode(summary.connectionMode, localeCode)} />
            <span>{summary.version || '—'}</span>
            <span>{summary.environment || '—'}</span>
          </>
        }
        actions={
          <WorkloadRefresh
            settings={refresh}
            queries={activeQueries}
            resourceLabel="Cluster"
            onRefresh={reload}
            beforeRefresh={
              <>
                {canReadNodes && (
                  <Button
                    type="primary"
                    onClick={() =>
                      navigate(
                        `/cluster-resources/nodes?clusterId=${encodeURIComponent(clusterId)}`,
                      )
                    }
                  >
                    {zh ? '进入工作台' : 'Open workbench'}
                  </Button>
                )}
                {canUpdate && !clusterDetailQuery.error && (
                  <Button onClick={() => setEditingTarget(clusterId)}>
                    {zh ? '编辑集群' : 'Edit cluster'}
                  </Button>
                )}
              </>
            }
          />
        }
      />
      <ResourceDetailTabs
        activeKey={activeTab}
        onChange={setTab}
        items={[
          { key: 'overview', label: zh ? '概览' : 'Overview', children: overview },
          {
            key: 'nodes',
            label: zh ? '节点' : 'Nodes',
            children: (
              <>
                {canReadNodes ? (
                  <KubernetesResourceTable
                    resourceKey="cluster-detail-nodes"
                    resourceLabel={zh ? 'Nodes' : 'Nodes'}
                    allRows={nodes ?? []}
                    dataSource={(nodes ?? []).filter((n) =>
                      [n.name, n.internalIp, n.version, ...(n.roles ?? [])]
                        .join(' ')
                        .toLowerCase()
                        .includes(nodeSearch.trim().toLowerCase()),
                    )}
                    filterKey={nodeSearch}
                    columns={nodeColumns}
                    rowKey="name"
                    loading={nodesQuery.isLoading}
                    error={nodesQuery.error}
                    hasReadData={nodesQuery.data !== undefined}
                    onRetry={() => nodesQuery.refetch()}
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    localSorting
                    scroll={{ x: 900 }}
                    toolbar={
                      <div className="soha-cluster-node-search">
                        <ManagementToolbarSearch
                          value={nodeSearch}
                          onChange={setNodeSearch}
                          placeholder={
                            zh ? '搜索节点、IP、角色、版本' : 'Search node, IP, role, version'
                          }
                        />
                      </div>
                    }
                    headerExtra={
                      <Button onClick={() => nodesQuery.refetch()}>
                        {zh ? '刷新节点' : 'Refresh nodes'}
                      </Button>
                    }
                  />
                ) : (
                  <ManagementState
                    compact
                    title={zh ? '需要节点查看权限' : 'Node view permission required'}
                  />
                )}
              </>
            ),
          },
          {
            key: 'monitoring',
            label: zh ? '监控接入' : 'Monitoring',
            children: (
              <Card
                className="soha-detail-card"
                title="Prometheus"
                extra={
                  <div className="soha-cluster-monitoring-actions">
                    {canUpdate && !clusterDetailQuery.error ? (
                      <Button onClick={() => setEditingTarget(clusterId)}>
                        {zh ? '编辑监控配置' : 'Edit monitoring'}
                      </Button>
                    ) : null}
                    <Button
                      type="primary"
                      loading={checking && monitoring.query.isFetching}
                      disabled={!monitoring.canCheck}
                      onClick={() => {
                        if (!monitoring.canCheck) return
                        if (checking) void monitoring.query.refetch()
                        else setCheckingTarget(clusterId)
                      }}
                    >
                      {zh ? '校验接入' : 'Check connection'}
                    </Button>
                  </div>
                }
              >
                <WorkloadOverviewConfiguration
                  items={[
                    { label: 'Prometheus URL', children: monitoringConfig.baseUrl || '—' },
                    {
                      label: zh ? '访问方式' : 'Transport',
                      children: monitoringConfig.transport === 'agent' ? 'Agent' : 'Core',
                    },
                    {
                      label: zh ? '集群标签名' : 'Cluster label name',
                      children: monitoringConfig.clusterLabel || '—',
                    },
                    {
                      label: zh ? '集群标签值' : 'Cluster label value',
                      children: monitoringConfig.clusterLabel
                        ? monitoringConfig.clusterLabelValue || (
                            <Text type="secondary">
                              {summary.id} {zh ? '（默认集群 ID）' : '(default cluster ID)'}
                            </Text>
                          )
                        : '—',
                    },
                    {
                      label: 'Bearer Token',
                      children: monitoringConfig.hasBearerToken
                        ? zh
                          ? '已配置'
                          : 'Configured'
                        : zh
                          ? '未配置'
                          : 'Not configured',
                    },
                    { label: 'Grafana URL', children: monitoringConfig.grafanaBaseUrl || '—' },
                  ]}
                />
                <div className="soha-cluster-check-result">
                  {checking ? (
                    <PrometheusCheckResult check={monitoring} clusterId={clusterId} />
                  ) : (
                    <Tooltip
                      trigger={['hover', 'focus', 'click']}
                      title={
                        monitoring.canCheck
                          ? zh
                            ? '校验当前保存配置的节点 CPU、内存及身份匹配。'
                            : 'Check saved node CPU, memory and identity matching.'
                          : zh
                            ? '需要主机监控校验权限'
                            : 'Host monitoring permission required'
                      }
                    >
                      <Button
                        type="text"
                        size="small"
                        icon={<QuestionCircleOutlined />}
                        aria-label={zh ? '监控校验说明' : 'Monitoring check information'}
                      />
                    </Tooltip>
                  )}
                </div>
              </Card>
            ),
          },
          {
            key: 'connection',
            label: zh ? '连接与 Agent' : 'Connection & Agent',
            children: (
              <div className="soha-detail-stack">
                <ClusterConnectionPanel detail={detail} localeCode={localeCode} />
                {summary.connectionMode === 'agent' && (
                  <AgentActionsCard checks={detail.diagnostics.actionChecks} />
                )}
              </div>
            ),
          },
          {
            key: 'logging',
            label: zh ? '日志采集' : 'Log collection',
            children: (
              <Card className="soha-detail-card" title={zh ? '日志采集' : 'Log collection'}>
                <LogCollectionCard scope={detailScope} localeCode={localeCode} />
              </Card>
            ),
          },
        ]}
      />
      {editing && (
        <ClusterEditor
          key={summary.id}
          cluster={summary}
          onClose={() => {
            setEditingTarget(undefined)
            setCheckingTarget(undefined)
          }}
        />
      )}
    </div>
  )
}
