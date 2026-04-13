import { Card, Spin, Empty, Typography } from '@douyinfe/semi-ui'
import { IconServer, IconGridView, IconAlertTriangle, IconTick } from '@douyinfe/semi-icons'
import { useQuery } from '@tanstack/react-query'
import { PageHeader } from '@/components/page-header'
import { StatGrid } from '@/components/stat-grid'
import { StatusTag } from '@/components/status-tag'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatDateTime } from '@/utils/time'
import type { Cluster, ApiResponse } from '@/types'

const { Text } = Typography

interface AlertSummary {
  totalCount: number
  firingCount: number
  resolvedCount: number
  criticalCount: number
  warningCount: number
  infoCount: number
  channelCount: number
  lastReceivedAt?: string
}

interface PodOverview {
  name: string
  namespace: string
  phase: string
  readyContainers: string
  restarts: number
  nodeName: string
  podIp?: string
}

export function OverviewPage() {
  const { t } = useI18n()
  const preferredClusterId = usePlatformScopeStore((state) => state.clusterId)
  const clustersQuery = useQuery({
    queryKey: ['clusters'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
  })

  const summaryQuery = useQuery({
    queryKey: ['monitoring-summary'],
    queryFn: () => api.get<ApiResponse<AlertSummary>>('/monitoring/summary'),
  })

  const clusters = clustersQuery.data?.data ?? []
  const summary = summaryQuery.data?.data
  const healthyClusters = clusters.filter((cluster) => cluster.health?.status === 'healthy').length
  const effectiveClusterId = clusters.some((cluster) => cluster.id === preferredClusterId)
    ? preferredClusterId
    : clusters[0]?.id
  const effectiveCluster = clusters.find((cluster) => cluster.id === effectiveClusterId)

  const podsQuery = useQuery({
    queryKey: ['overview-pods', effectiveClusterId],
    queryFn: () => api.get<ApiResponse<PodOverview[]>>(buildClusterScopedPath(effectiveClusterId!, 'workloads/pods')),
    enabled: !!effectiveClusterId,
  })

  const isLoading = clustersQuery.isLoading || summaryQuery.isLoading

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    )
  }

  const stats = [
    { label: '集群总数', value: clusters.length, icon: <IconServer size="extra-large" /> },
    { label: '健康集群', value: healthyClusters, icon: <IconTick size="extra-large" /> },
    { label: '活跃告警', value: summary?.firingCount ?? 0, icon: <IconAlertTriangle size="extra-large" /> },
    { label: '通知渠道', value: summary?.channelCount ?? 0, icon: <IconGridView size="extra-large" /> },
  ]

  const pods = podsQuery.data?.data ?? []
  const podStats = [
    { label: 'Pod 总数', value: pods.length, icon: <IconGridView size="extra-large" /> },
    { label: 'Running', value: pods.filter((item) => item.phase === 'Running').length, icon: <IconTick size="extra-large" /> },
    { label: 'Pending', value: pods.filter((item) => item.phase === 'Pending').length, icon: <IconAlertTriangle size="extra-large" /> },
    { label: '异常 Pod', value: pods.filter((item) => item.phase !== 'Running' || item.restarts > 0).length, icon: <IconAlertTriangle size="extra-large" /> },
    { label: '发生重启', value: pods.filter((item) => item.restarts > 0).length, icon: <IconAlertTriangle size="extra-large" /> },
  ]
  const problematicPods = pods
    .filter((item) => item.phase !== 'Running' || item.restarts > 0)
    .sort((a, b) => b.restarts - a.restarts)
    .slice(0, 8)

  return (
    <div className="kc-page">
      <PageHeader title={t('page.overview.title', 'Platform Overview')} description={t('page.overview.desc', 'Inspect fleet size, cluster health, and alert pressure from one console view.')} />

      <StatGrid items={stats} />

      <Card title={t('page.overview.alerts', 'Alert Summary')}>
        {summary ? (
          <div className="kc-list-panel">
            <div className="kc-list-row">
              <div className="kc-list-row-meta">
                <Text strong>{t('page.overview.alertDist', 'Alert Distribution')}</Text>
              </div>
              <div className="kc-list-row-extra">
                <Text type="tertiary" size="small">总数: {summary.totalCount}</Text>
                <Text type="tertiary" size="small">活跃: {summary.firingCount}</Text>
                <Text type="tertiary" size="small">已恢复: {summary.resolvedCount}</Text>
                <Text type="tertiary" size="small">Critical: {summary.criticalCount}</Text>
                <Text type="tertiary" size="small">Warning: {summary.warningCount}</Text>
                <Text type="tertiary" size="small">最近接收: {formatDateTime(summary.lastReceivedAt)}</Text>
              </div>
            </div>
          </div>
        ) : (
          <Empty description={t('page.overview.noAlerts', 'No alert summary')} />
        )}
      </Card>

      <Card title={t('page.overview.clusterHealth', 'Cluster Health')}>
        {clusters.length === 0 ? (
          <Empty description={t('page.overview.noClusters', 'No clusters')} />
        ) : (
          <div className="kc-list-panel">
            {clusters.map((c) => (
              <div key={c.id} className="kc-list-row">
                <div className="kc-list-row-meta">
                  <Text strong>{c.name}</Text>
                  <StatusTag value={c.health?.status ?? 'unknown'} />
                </div>
                <div className="kc-list-row-extra">
                  <Text type="tertiary" size="small">Region: {c.region || '-'}</Text>
                  <Text type="tertiary" size="small">Env: {c.environment || '-'}</Text>
                  <Text type="tertiary" size="small">Mode: {c.connectionMode || '-'}</Text>
                  <Text type="tertiary" size="small">Version: {c.version || '-'}</Text>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card title={effectiveCluster ? `Pod 运行态势 / ${effectiveCluster.name}` : 'Pod 运行态势'}>
        {!effectiveCluster ? (
          <Empty description="暂无可用集群" />
        ) : podsQuery.isLoading ? (
          <Spin size="large" />
        ) : (
          <div className="kc-page-section">
            <StatGrid items={podStats} />
            <Card className="kc-detail-card" title="异常 Pod">
              {problematicPods.length === 0 ? (
                <Empty description="当前集群下暂无异常 Pod" />
              ) : (
                <div className="kc-list-panel">
                  {problematicPods.map((item) => (
                    <div key={`${item.namespace}/${item.name}`} className="kc-list-row">
                      <div className="kc-list-row-meta">
                        <Text strong>{item.name}</Text>
                        <StatusTag value={item.phase} />
                      </div>
                      <div className="kc-list-row-extra">
                        <Text type="tertiary" size="small">NS: {item.namespace}</Text>
                        <Text type="tertiary" size="small">Node: {item.nodeName || '-'}</Text>
                        <Text type="tertiary" size="small">Ready: {item.readyContainers}</Text>
                        <Text type="tertiary" size="small">Restarts: {item.restarts}</Text>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </Card>
          </div>
        )}
      </Card>
    </div>
  )
}
