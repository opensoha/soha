import { ResourceDetailHeader } from '@/features/platform/shared/resource-detail-header'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useAIPageContext } from '@/features/copilot'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Card, Descriptions, Spin, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { LogCollectionCard } from './log-collection-card'
import { AgentActionsCard } from './agent-actions-card'
import {
  clusterTypeOf,
  formatClusterRuntimeValue,
  formatClusterType,
  formatConnectionMode,
} from './presentation'
import { clusterQueries } from './queries'
import './styles.css'
import type { Node } from './types'

const { Text } = Typography

export function ClusterDetailPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = useParams()
  const setClusterId = usePlatformScopeStore((state) => state.setClusterId)

  const detailScope = toScopeKey(clusterId, null)
  const clusterDetailQuery = useQuery(clusterQueries.detail(detailScope))
  const visibleClusterDetailQueryData = readableQueryData(clusterDetailQuery)
  const nodesQuery = useQuery(clusterQueries.nodes(detailScope))
  const visibleNodesQueryData = readableQueryData(nodesQuery)

  const detail = visibleClusterDetailQueryData
  const summary = detail?.summary
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: localeCode === 'zh_CN' ? '集群详情' : 'Cluster Detail',
    entityKind: 'Cluster',
    entityName: summary?.name || clusterId,
    clusterId,
  })
  const nodeColumns: TableColumnsType<Node> = [
    {
      title: localeCode === 'zh_CN' ? '节点' : 'Node',
      dataIndex: 'name',
      render: (value: string) => (
        <Button
          type="text"
          onClick={() => {
            setClusterId(summary?.id ?? null)
            navigate(
              `/cluster-resources/nodes/${encodeURIComponent(value)}?clusterId=${encodeURIComponent(summary?.id ?? '')}`,
            )
          }}
        >
          {value}
        </Button>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'status',
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '角色' : 'Roles',
      dataIndex: 'roles',
      render: (roles: string[]) => (roles?.length ? roles.join(', ') : '-'),
    },
    {
      title: localeCode === 'zh_CN' ? '版本' : 'Version',
      dataIndex: 'version',
      render: (value: string) => value || '-',
    },
    {
      title: 'IP',
      dataIndex: 'internalIp',
      render: (value: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? 'Pod 数量' : 'Pods',
      dataIndex: 'podCount',
    },
  ]

  if (clusterDetailQuery.isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    )
  }

  if (!detail || !summary) {
    return (
      <div className="soha-page">
        <ManagementState kind="not-found" description={t('common.notFound', 'Not found')} />
      </div>
    )
  }

  return (
    <div className="soha-page soha-cluster-detail-page soha-k8s-detail-page">
      <ResourceDetailHeader
        kind="Cluster"
        name={summary.name}
        status={summary.health?.status}
        queryKey={clusterQueries.detail(detailScope).queryKey}
        onRefresh={() => Promise.all([clusterDetailQuery.refetch(), nodesQuery.refetch()])}
      />
      <ReadFeedback
        canRead={clusterDetailQuery.isEnabled}
        query={clusterDetailQuery}
        label="集群详情"
      />
      <ReadFeedback canRead={nodesQuery.isEnabled} query={nodesQuery} label="节点" />

      <div className="soha-cluster-detail-grid">
        <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '基础信息' : 'Summary'}>
          <Descriptions
            size="small"
            items={[
              {
                key: localeCode === 'zh_CN' ? '名称' : 'Name',
                label: localeCode === 'zh_CN' ? '名称' : 'Name',
                children: summary.name,
              },
              {
                key: localeCode === 'zh_CN' ? '状态' : 'Status',
                label: localeCode === 'zh_CN' ? '状态' : 'Status',
                children: <StatusTag value={summary.health?.status ?? 'unknown'} />,
              },
              {
                key: localeCode === 'zh_CN' ? '版本' : 'Version',
                label: localeCode === 'zh_CN' ? '版本' : 'Version',
                children: summary.version || '-',
              },
              {
                key: localeCode === 'zh_CN' ? '类型' : 'Type',
                label: localeCode === 'zh_CN' ? '类型' : 'Type',
                children: formatClusterType(clusterTypeOf(summary), localeCode),
              },
              { key: 'Environment', label: 'Environment', children: summary.environment || '-' },
              {
                key: localeCode === 'zh_CN' ? '连接方式' : 'Mode',
                label: localeCode === 'zh_CN' ? '连接方式' : 'Mode',
                children: formatConnectionMode(summary.connectionMode, localeCode),
              },
              {
                key: localeCode === 'zh_CN' ? '最近检查' : 'Last Checked',
                label: localeCode === 'zh_CN' ? '最近检查' : 'Last Checked',
                children: summary.health?.lastChecked
                  ? formatDateTime(summary.health.lastChecked)
                  : '-',
              },
              {
                key: localeCode === 'zh_CN' ? '状态信息' : 'Message',
                label: localeCode === 'zh_CN' ? '状态信息' : 'Message',
                children: summary.health?.message || '-',
              },
            ]}
          />
          <div className="soha-detail-meta">
            <Text strong>{localeCode === 'zh_CN' ? '集群 Labels:' : 'Cluster Labels:'}</Text>
            {Object.keys(summary.labels || {}).length === 0 ? (
              <Text type="secondary" className="text-xs">
                {localeCode === 'zh_CN' ? '未配置标签' : 'No labels configured'}
              </Text>
            ) : (
              <div className="soha-tag-list">
                {Object.entries(summary.labels || {}).map(([key, value]) => (
                  <Tag key={key}>
                    {key}={value}
                  </Tag>
                ))}
              </div>
            )}
          </div>
          <div className="soha-detail-meta">
            <Text strong>{localeCode === 'zh_CN' ? '能力:' : 'Capabilities:'}</Text>
            {summary.capabilities?.length ? (
              <div className="soha-tag-list">
                {summary.capabilities.map((item) => (
                  <Tag key={item}>{item}</Tag>
                ))}
              </div>
            ) : (
              <Text type="secondary" className="text-xs">
                {localeCode === 'zh_CN' ? '无额外能力声明' : 'No explicit capabilities reported'}
              </Text>
            )}
          </div>
        </Card>

        <Card
          className="soha-detail-card"
          title={localeCode === 'zh_CN' ? '连接与诊断' : 'Connection & Diagnostics'}
        >
          <Descriptions
            size="small"
            items={[
              {
                key: localeCode === 'zh_CN' ? '连接模式' : 'Connection Mode',
                label: localeCode === 'zh_CN' ? '连接模式' : 'Connection Mode',
                children: formatConnectionMode(detail.connection.mode, localeCode),
              },
              {
                key: localeCode === 'zh_CN' ? '凭据类型' : 'Credential Type',
                label: localeCode === 'zh_CN' ? '凭据类型' : 'Credential Type',
                children: formatClusterRuntimeValue(detail.connection.credentialType, localeCode),
              },
              {
                key: localeCode === 'zh_CN' ? '来源类型' : 'Source Type',
                label: localeCode === 'zh_CN' ? '来源类型' : 'Source Type',
                children: formatClusterRuntimeValue(detail.connection.sourceType, localeCode),
              },
              {
                key: localeCode === 'zh_CN' ? 'Context' : 'Context',
                label: localeCode === 'zh_CN' ? 'Context' : 'Context',
                children: detail.connection.context || '-',
              },
              {
                key: localeCode === 'zh_CN' ? 'Endpoint' : 'Endpoint',
                label: localeCode === 'zh_CN' ? 'Endpoint' : 'Endpoint',
                children: detail.connection.endpoint || '-',
              },
              {
                key: localeCode === 'zh_CN' ? 'Informer Cache' : 'Informer Cache',
                label: localeCode === 'zh_CN' ? 'Informer Cache' : 'Informer Cache',
                children: detail.connection.usesInformerCache
                  ? localeCode === 'zh_CN'
                    ? '是'
                    : 'Yes'
                  : localeCode === 'zh_CN'
                    ? '否'
                    : 'No',
              },
              {
                key: localeCode === 'zh_CN' ? '同步策略' : 'Sync Strategy',
                label: localeCode === 'zh_CN' ? '同步策略' : 'Sync Strategy',
                children: formatClusterRuntimeValue(detail.diagnostics.syncStrategy, localeCode),
              },
              {
                key: localeCode === 'zh_CN' ? '缓存状态' : 'Cache Status',
                label: localeCode === 'zh_CN' ? '缓存状态' : 'Cache Status',
                children: formatClusterRuntimeValue(detail.diagnostics.cacheStatus, localeCode),
              },
              {
                key: localeCode === 'zh_CN' ? '连接状态' : 'Connection State',
                label: localeCode === 'zh_CN' ? '连接状态' : 'Connection State',
                children: formatClusterRuntimeValue(detail.diagnostics.connectionState, localeCode),
              },
              {
                key: localeCode === 'zh_CN' ? '诊断信息' : 'Diagnostic Message',
                label: localeCode === 'zh_CN' ? '诊断信息' : 'Diagnostic Message',
                children: detail.diagnostics.message || '-',
              },
            ]}
          />
        </Card>
      </div>

      {summary.connectionMode === 'agent' ? (
        <AgentActionsCard checks={detail.diagnostics.actionChecks} />
      ) : null}

      <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '监控配置' : 'Monitoring'}>
        <Descriptions
          size="small"
          items={[
            {
              key: localeCode === 'zh_CN' ? 'Prometheus URL' : 'Prometheus URL',
              label: localeCode === 'zh_CN' ? 'Prometheus URL' : 'Prometheus URL',
              children: detail.monitoring.prometheus.baseUrl || '-',
            },
            {
              key: localeCode === 'zh_CN' ? 'Prometheus Cluster Label' : 'Prometheus Cluster Label',
              label:
                localeCode === 'zh_CN' ? 'Prometheus Cluster Label' : 'Prometheus Cluster Label',
              children: detail.monitoring.prometheus.clusterLabel || '-',
            },
            {
              key: localeCode === 'zh_CN' ? 'Bearer Token' : 'Bearer Token',
              label: localeCode === 'zh_CN' ? 'Bearer Token' : 'Bearer Token',
              children: detail.monitoring.prometheus.hasBearerToken
                ? localeCode === 'zh_CN'
                  ? '已配置'
                  : 'Configured'
                : localeCode === 'zh_CN'
                  ? '未配置'
                  : 'Not configured',
            },
            {
              key: localeCode === 'zh_CN' ? 'Grafana URL' : 'Grafana URL',
              label: localeCode === 'zh_CN' ? 'Grafana URL' : 'Grafana URL',
              children: detail.monitoring.prometheus.grafanaBaseUrl || '-',
            },
          ]}
        />
      </Card>

      <Card
        className="soha-detail-card"
        title={localeCode === 'zh_CN' ? '日志采集' : 'Log Collection'}
      >
        <LogCollectionCard scope={detailScope} localeCode={localeCode} />
      </Card>

      <Card
        className="soha-detail-card"
        title={localeCode === 'zh_CN' ? '节点快照' : 'Node Snapshot'}
      >
        {nodesQuery.isError ? (
          <ManagementState
            compact
            description={
              (nodesQuery.error as Error)?.message ||
              (localeCode === 'zh_CN' ? '节点快照加载失败' : 'Failed to load node snapshot')
            }
            kind="error"
            title={localeCode === 'zh_CN' ? '节点快照加载失败' : 'Failed to load node snapshot'}
          />
        ) : (
          <AdminTable
            shellClassName="soha-management-table-shell"
            columns={nodeColumns}
            dataSource={visibleNodesQueryData ?? []}
            rowKey="name"
            loading={nodesQuery.isLoading}
            pagination={false}
            scroll={{ x: 800, y: 520 }}
            enableColumnSelection={false}
          />
        )}
      </Card>
    </div>
  )
}
