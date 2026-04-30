import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Card, Empty, Spin, Typography } from 'antd'
import { AppstoreOutlined, CheckCircleOutlined, ClusterOutlined, WarningOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { StatGrid } from '@/components/stat-grid'
import { StatusTag } from '@/components/status-tag'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import type { ApiResponse, Cluster } from '@/types'

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

interface WorkloadOverviewNamespace {
  namespace: string
  totalPods: number
  runningPods: number
  atRiskPods: number
  restartingPods: number
}

interface WorkloadOverviewPod {
  name: string
  namespace: string
  phase: string
  readyContainers: string
  restarts: number
  nodeName?: string
  ageSeconds: number
}

interface WorkloadOverview {
  clusterId: string
  namespace?: string
  source: string
  generatedAt: string
  totalPods: number
  runningPods: number
  pendingPods: number
  succeededPods: number
  failedPods: number
  unknownPods: number
  restartingPods: number
  atRiskPods: number
  namespaceBreakdown?: WorkloadOverviewNamespace[]
  problematicPods?: WorkloadOverviewPod[]
}

function buildPodDetailPath(name: string, namespace: string) {
  const params = new URLSearchParams()
  if (namespace) {
    params.set('namespace', namespace)
  }
  const query = params.toString()
  return query ? `/workloads/pods/${name}?${query}` : `/workloads/pods/${name}`
}

export function OverviewPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const {
    clusterId: preferredClusterId,
    namespace,
    setClusterId,
    setNamespace,
  } = usePlatformScopeStore()

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

  useEffect(() => {
    if (!clusters.length) return
    if (preferredClusterId && clusters.some((cluster) => cluster.id === preferredClusterId)) return
    setClusterId(clusters[0].id)
  }, [clusters, preferredClusterId, setClusterId])

  const workloadOverviewQuery = useQuery({
    queryKey: ['overview-workload', effectiveClusterId, namespace],
    queryFn: () =>
      api.get<ApiResponse<WorkloadOverview>>(
        buildClusterScopedPath(effectiveClusterId!, 'workloads/overview', namespace),
      ),
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
    { label: '集群总数', value: clusters.length, icon: <ClusterOutlined style={{ fontSize: 28 }} /> },
    { label: '健康集群', value: healthyClusters, icon: <CheckCircleOutlined style={{ fontSize: 28 }} /> },
    { label: '活跃告警', value: summary?.firingCount ?? 0, icon: <WarningOutlined style={{ fontSize: 28 }} /> },
    { label: '通知渠道', value: summary?.channelCount ?? 0, icon: <AppstoreOutlined style={{ fontSize: 28 }} /> },
  ]

  const workloadOverview = workloadOverviewQuery.data?.data
  const namespaceBreakdown = workloadOverview?.namespaceBreakdown ?? []
  const problematicPods = workloadOverview?.problematicPods ?? []
  const scopeLabel = effectiveCluster
    ? `${effectiveCluster.name} / ${namespace && namespace !== '' ? namespace : t('platformScope.allNamespaces', 'All namespaces')}`
    : '-'
  const podStats = [
    { label: localeCode === 'zh_CN' ? 'Pod 总数' : 'Pods', value: workloadOverview?.totalPods ?? 0, icon: <AppstoreOutlined style={{ fontSize: 28 }} /> },
    { label: 'Running', value: workloadOverview?.runningPods ?? 0, icon: <CheckCircleOutlined style={{ fontSize: 28 }} /> },
    { label: 'Pending', value: workloadOverview?.pendingPods ?? 0, icon: <WarningOutlined style={{ fontSize: 28 }} /> },
    { label: localeCode === 'zh_CN' ? '已完成' : 'Completed', value: workloadOverview?.succeededPods ?? 0, icon: <CheckCircleOutlined style={{ fontSize: 28 }} /> },
    { label: localeCode === 'zh_CN' ? '需关注 Pod' : 'At-risk Pods', value: workloadOverview?.atRiskPods ?? 0, icon: <WarningOutlined style={{ fontSize: 28 }} /> },
    { label: localeCode === 'zh_CN' ? '发生重启' : 'Restarting', value: workloadOverview?.restartingPods ?? 0, icon: <WarningOutlined style={{ fontSize: 28 }} /> },
  ]

  return (
    <div className="kc-page">
      <PlatformScopeToolbar />

      <StatGrid items={stats} />

      <Card title={t('page.overview.alerts', 'Alert Summary')}>
        {summary ? (
          <div className="kc-list-panel">
            <div className="kc-list-row">
              <div className="kc-list-row-meta">
                <Text strong>{t('page.overview.alertDist', 'Alert Distribution')}</Text>
              </div>
              <div className="kc-list-row-extra">
                <Text type="secondary" className="text-xs">总数: {summary.totalCount}</Text>
                <Text type="secondary" className="text-xs">活跃: {summary.firingCount}</Text>
                <Text type="secondary" className="text-xs">已恢复: {summary.resolvedCount}</Text>
                <Text type="secondary" className="text-xs">Critical: {summary.criticalCount}</Text>
                <Text type="secondary" className="text-xs">Warning: {summary.warningCount}</Text>
                <Text type="secondary" className="text-xs">最近接收: {formatDateTime(summary.lastReceivedAt)}</Text>
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
            {clusters.map((cluster) => (
              <div key={cluster.id} className="kc-list-row">
                <div className="kc-list-row-meta">
                  <Text strong>{cluster.name}</Text>
                  <StatusTag value={cluster.health?.status ?? 'unknown'} />
                </div>
                <div className="kc-list-row-extra">
                  <Text type="secondary" className="text-xs">Region: {cluster.region || '-'}</Text>
                  <Text type="secondary" className="text-xs">Env: {cluster.environment || '-'}</Text>
                  <Text type="secondary" className="text-xs">Mode: {cluster.connectionMode || '-'}</Text>
                  <Text type="secondary" className="text-xs">Version: {cluster.version || '-'}</Text>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card
        title={localeCode === 'zh_CN' ? 'Pod 运行态势' : 'Pod Runtime'}
        extra={effectiveCluster ? (
          <div className="flex items-center gap-3">
            <Text type="secondary" className="text-xs">
              {localeCode === 'zh_CN' ? '当前范围' : 'Scope'}: {scopeLabel}
            </Text>
            <Button type="text" onClick={() => navigate('/workloads/pods')}>
              {localeCode === 'zh_CN' ? '查看 Pod 列表' : 'Open Pods'}
            </Button>
          </div>
        ) : null}
      >
        {!effectiveCluster ? (
          <Empty description={localeCode === 'zh_CN' ? '暂无可用集群' : 'No cluster available'} />
        ) : workloadOverviewQuery.isLoading ? (
          <Spin size="large" />
        ) : !workloadOverview ? (
          <Empty description={localeCode === 'zh_CN' ? '当前范围暂无运行态势摘要' : 'No workload runtime summary for the current scope'} />
        ) : (
          <div className="kc-page-section">
            <StatGrid items={podStats} />

            <div className="grid gap-4 xl:grid-cols-2">
              <Card
                className="kc-detail-card"
                title={
                  namespace && namespace !== ''
                    ? (localeCode === 'zh_CN' ? '当前命名空间摘要' : 'Namespace Snapshot')
                    : (localeCode === 'zh_CN' ? '命名空间热点' : 'Namespace Hotspots')
                }
                extra={
                  namespace && namespace !== '' ? (
                    <Button type="text" onClick={() => setNamespace(null)}>
                      {localeCode === 'zh_CN' ? '查看全部命名空间' : 'All Namespaces'}
                    </Button>
                  ) : null
                }
              >
                {namespaceBreakdown.length === 0 ? (
                  <Empty description={localeCode === 'zh_CN' ? '当前范围暂无 Pod 分布数据' : 'No namespace distribution in the current scope'} />
                ) : (
                  <div className="kc-list-panel">
                    {namespaceBreakdown.map((item) => (
                      <div key={item.namespace} className="kc-list-row">
                        <div className="kc-list-row-meta">
                          <Text strong>{item.namespace}</Text>
                        </div>
                        <div className="kc-list-row-extra">
                          <Text type="secondary" className="text-xs">Pods: {item.totalPods}</Text>
                          <Text type="secondary" className="text-xs">Running: {item.runningPods}</Text>
                          <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '需关注' : 'At-risk'}: {item.atRiskPods}</Text>
                          <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '重启' : 'Restarts'}: {item.restartingPods}</Text>
                          {namespace && namespace !== '' ? null : (
                            <Button
                              type="text"
                              onClick={() => {
                                setNamespace(item.namespace)
                                navigate('/workloads/pods')
                              }}
                            >
                              {localeCode === 'zh_CN' ? '查看 Pod' : 'Open Pods'}
                            </Button>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </Card>

              <Card
                className="kc-detail-card"
                title={localeCode === 'zh_CN' ? '需关注的 Pod' : 'Pods Requiring Attention'}
                extra={
                  <Text type="secondary" className="text-xs">
                    {localeCode === 'zh_CN' ? '更新时间' : 'Updated'}: {formatDateTime(workloadOverview.generatedAt)}
                  </Text>
                }
              >
                {problematicPods.length === 0 ? (
                  <Empty description={localeCode === 'zh_CN' ? '当前范围内没有需要关注的 Pod' : 'No pods require attention in the current scope'} />
                ) : (
                  <div className="kc-list-panel">
                    {problematicPods.map((item) => (
                      <div key={`${item.namespace}/${item.name}`} className="kc-list-row">
                        <div className="kc-list-row-meta">
                          <Button
                            type="text"
                            onClick={() => navigate(buildPodDetailPath(item.name, item.namespace))}
                          >
                            {item.name}
                          </Button>
                          <StatusTag value={item.phase} />
                        </div>
                        <div className="kc-list-row-extra">
                          <Text type="secondary" className="text-xs">NS: {item.namespace}</Text>
                          <Text type="secondary" className="text-xs">Node: {item.nodeName || '-'}</Text>
                          <Text type="secondary" className="text-xs">Ready: {item.readyContainers}</Text>
                          <Text type="secondary" className="text-xs">Restarts: {item.restarts}</Text>
                          <Text type="secondary" className="text-xs">Age: {formatAgeSeconds(item.ageSeconds)}</Text>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </Card>
            </div>
          </div>
        )}
      </Card>
    </div>
  )
}
