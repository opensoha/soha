import { useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Card, Spin, Statistic, Typography } from 'antd'
import {
  AppstoreOutlined,
  ArrowRightOutlined,
  CheckCircleOutlined,
  ClusterOutlined,
  FireOutlined,
  ReloadOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import type { ApiResponse, Cluster } from '@/types'
import './platform-pages.css'

const { Text } = Typography

const clusterTypeLabels: Record<string, { zh: string; en: string }> = {
  standard_kubernetes: { zh: '标准 Kubernetes', en: 'Standard Kubernetes' },
  gke: { zh: 'GKE', en: 'GKE' },
  ack: { zh: 'ACK', en: 'ACK' },
  tke: { zh: 'TKE', en: 'TKE' },
  aks: { zh: 'AKS', en: 'AKS' },
}

function clusterTypeOf(cluster: Cluster) {
  const provider = cluster.labels?.provider
  return typeof provider === 'string' && provider.trim() !== '' ? provider.trim() : cluster.region
}

function formatClusterType(cluster: Cluster, localeCode: string) {
  const value = clusterTypeOf(cluster)
  const item = clusterTypeLabels[value]
  if (!item) return value || '-'
  return localeCode === 'zh_CN' ? item.zh : item.en
}

function formatWorkloadSource(source: string | undefined, localeCode: string) {
  switch ((source || '').toLowerCase()) {
    case 'cache':
      return localeCode === 'zh_CN' ? '缓存' : 'Cache'
    case 'live':
      return localeCode === 'zh_CN' ? '实时' : 'Live'
    case 'agent':
      return 'Agent'
    default:
      return source || '-'
  }
}

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

interface AggregatedNamespaceBreakdown extends WorkloadOverviewNamespace {
  clusterId: string
  clusterName: string
}

interface AggregatedProblematicPod extends WorkloadOverviewPod {
  clusterId: string
  clusterName: string
}

interface AggregatedWorkloadOverview extends Omit<WorkloadOverview, 'clusterId' | 'namespace' | 'generatedAt' | 'source' | 'namespaceBreakdown' | 'problematicPods'> {
  generatedAt: string
  source: string
  namespaceBreakdown: AggregatedNamespaceBreakdown[]
  problematicPods: AggregatedProblematicPod[]
}

export function OverviewPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const { clusterId } = usePlatformScopeStore()

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
  const currentCluster = clusters.find((cluster) => cluster.id === clusterId) ?? null

  const workloadOverviewQuery = useQuery({
    queryKey: ['overview-workload', clusterId, '__all__'],
    queryFn: () =>
      api.get<ApiResponse<WorkloadOverview>>(
        buildClusterScopedPath(clusterId!, 'workloads/overview', null),
      ),
    enabled: !!clusterId,
  })

  const workloadOverviewLoading = workloadOverviewQuery.isLoading
  const workloadOverviewData = workloadOverviewQuery.data?.data ?? null

  const workloadOverview = useMemo<AggregatedWorkloadOverview | null>(() => {
    if (!workloadOverviewData) return null
    const clusterName = currentCluster?.name ?? clusterId ?? '-'

    return {
      totalPods: workloadOverviewData.totalPods,
      runningPods: workloadOverviewData.runningPods,
      pendingPods: workloadOverviewData.pendingPods,
      succeededPods: workloadOverviewData.succeededPods,
      failedPods: workloadOverviewData.failedPods,
      unknownPods: workloadOverviewData.unknownPods,
      restartingPods: workloadOverviewData.restartingPods,
      atRiskPods: workloadOverviewData.atRiskPods,
      generatedAt: workloadOverviewData.generatedAt,
      source: workloadOverviewData.source,
      namespaceBreakdown: (workloadOverviewData.namespaceBreakdown ?? [])
        .map((item) => ({
          ...item,
          clusterId: workloadOverviewData.clusterId,
          clusterName,
        }))
        .sort((left, right) => {
          if (left.atRiskPods !== right.atRiskPods) return right.atRiskPods - left.atRiskPods
          if (left.restartingPods !== right.restartingPods) return right.restartingPods - left.restartingPods
          if (left.totalPods !== right.totalPods) return right.totalPods - left.totalPods
          return left.namespace.localeCompare(right.namespace)
        })
        .slice(0, 6),
      problematicPods: (workloadOverviewData.problematicPods ?? [])
        .map((item) => ({
          ...item,
          clusterId: workloadOverviewData.clusterId,
          clusterName,
        }))
        .sort((left, right) => {
          if (left.restarts !== right.restarts) return right.restarts - left.restarts
          return left.namespace.localeCompare(right.namespace) || left.name.localeCompare(right.name)
        })
        .slice(0, 8),
    }
  }, [clusterId, currentCluster?.name, workloadOverviewData])

  const namespaceBreakdown = workloadOverview?.namespaceBreakdown ?? []
  const problematicPods = workloadOverview?.problematicPods ?? []
  const updatedAt = workloadOverview?.generatedAt ? formatDateTime(workloadOverview.generatedAt) : '-'
  const isLoading = clustersQuery.isLoading || summaryQuery.isLoading

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    )
  }

  const overviewStats = [
    {
      key: 'clusters',
      label: localeCode === 'zh_CN' ? '集群总数' : 'Clusters',
      helper: localeCode === 'zh_CN' ? '已登记到控制台的集群' : 'Registered in the console',
      value: clusters.length,
      icon: <ClusterOutlined />,
      tone: 'default',
    },
    {
      key: 'healthy',
      label: localeCode === 'zh_CN' ? '健康集群' : 'Healthy',
      helper: localeCode === 'zh_CN' ? '当前健康状态正常' : 'Reporting healthy status',
      value: healthyClusters,
      icon: <CheckCircleOutlined />,
      tone: 'success',
    },
    {
      key: 'alerts',
      label: localeCode === 'zh_CN' ? '活跃告警' : 'Firing Alerts',
      helper: localeCode === 'zh_CN' ? '需要值守的告警压力' : 'Current alert pressure',
      value: summary?.firingCount ?? 0,
      icon: <WarningOutlined />,
      tone: (summary?.firingCount ?? 0) > 0 ? 'warning' : 'default',
    },
    {
      key: 'channels',
      label: localeCode === 'zh_CN' ? '通知渠道' : 'Channels',
      helper: localeCode === 'zh_CN' ? '可用通知投递入口' : 'Delivery paths configured',
      value: summary?.channelCount ?? 0,
      icon: <AppstoreOutlined />,
      tone: 'default',
    },
  ]

  const alertChips = [
    { key: 'total', label: localeCode === 'zh_CN' ? '总数' : 'Total', value: summary?.totalCount ?? 0, tone: 'default' },
    { key: 'firing', label: localeCode === 'zh_CN' ? '活跃' : 'Firing', value: summary?.firingCount ?? 0, tone: 'warning' },
    { key: 'resolved', label: localeCode === 'zh_CN' ? '已恢复' : 'Resolved', value: summary?.resolvedCount ?? 0, tone: 'success' },
    { key: 'critical', label: 'Critical', value: summary?.criticalCount ?? 0, tone: 'danger' },
    { key: 'warning', label: 'Warning', value: summary?.warningCount ?? 0, tone: 'warning' },
  ]

  const podStats = [
    {
      key: 'pods',
      label: localeCode === 'zh_CN' ? 'Pod 总数' : 'Pods',
      helper: localeCode === 'zh_CN' ? '当前平台纳管 Pod 存量' : 'Total managed pods',
      value: workloadOverview?.totalPods ?? 0,
      icon: <AppstoreOutlined />,
      tone: 'default',
    },
    {
      key: 'running',
      label: 'Running',
      helper: localeCode === 'zh_CN' ? '正常运行中的 Pod' : 'Pods serving traffic',
      value: workloadOverview?.runningPods ?? 0,
      icon: <CheckCircleOutlined />,
      tone: 'success',
    },
    {
      key: 'pending',
      label: 'Pending',
      helper: localeCode === 'zh_CN' ? '等待调度或启动' : 'Waiting for scheduling or startup',
      value: workloadOverview?.pendingPods ?? 0,
      icon: <WarningOutlined />,
      tone: (workloadOverview?.pendingPods ?? 0) > 0 ? 'warning' : 'default',
    },
    {
      key: 'completed',
      label: localeCode === 'zh_CN' ? '已完成' : 'Completed',
      helper: localeCode === 'zh_CN' ? '已结束的工作负载' : 'Completed workload runs',
      value: workloadOverview?.succeededPods ?? 0,
      icon: <CheckCircleOutlined />,
      tone: 'default',
    },
    {
      key: 'risk',
      label: localeCode === 'zh_CN' ? '需关注 Pod' : 'At-risk Pods',
      helper: localeCode === 'zh_CN' ? '需要继续排查的实例' : 'Pods needing follow-up',
      value: workloadOverview?.atRiskPods ?? 0,
      icon: <FireOutlined />,
      tone: (workloadOverview?.atRiskPods ?? 0) > 0 ? 'danger' : 'default',
    },
    {
      key: 'restart',
      label: localeCode === 'zh_CN' ? '发生重启' : 'Restarts',
      helper: localeCode === 'zh_CN' ? '近期有重启痕迹' : 'Pods with restart activity',
      value: workloadOverview?.restartingPods ?? 0,
      icon: <ReloadOutlined />,
      tone: (workloadOverview?.restartingPods ?? 0) > 0 ? 'warning' : 'default',
    },
  ]

  return (
    <div className="soha-page soha-overview-page soha-platform-overview-page">
      <div className="soha-overview-metric-grid">
        {overviewStats.map((item) => (
          <Card key={item.key} size="small" variant="outlined" className={`soha-overview-metric-card is-${item.tone}`}>
            <div className="soha-overview-metric-card-head">
              <div className="soha-overview-metric-copy">
                <Text className="soha-overview-metric-label">{item.label}</Text>
                <Statistic value={item.value} />
              </div>
              <span className="soha-overview-metric-icon">{item.icon}</span>
            </div>
            <Text className="soha-overview-metric-helper">{item.helper}</Text>
          </Card>
        ))}
      </div>

      <div className="soha-overview-summary-grid">
        <Card
          className="soha-overview-panel-card"
          title={localeCode === 'zh_CN' ? '告警摘要' : 'Alert Summary'}
          extra={
            <Text type="secondary" className="text-xs">
              {localeCode === 'zh_CN' ? '最近接收' : 'Last received'}: {formatDateTime(summary?.lastReceivedAt)}
            </Text>
          }
        >
          {summary ? (
            <div className="soha-overview-alert-stack">
              <div className="soha-overview-section-bar">
                <div>
                  <Text strong>{localeCode === 'zh_CN' ? '告警分布' : 'Alert Distribution'}</Text>
                  <div className="soha-overview-inline-caption">
                    {summary.firingCount > 0
                      ? (localeCode === 'zh_CN' ? '当前仍有活跃告警，优先看 Critical 和 Warning。' : 'Active alerts remain. Start with Critical and Warning.')
                      : (localeCode === 'zh_CN' ? '当前没有活跃告警，保持通道与规则可用。' : 'No active alerts right now. Keep rules and channels healthy.')}
                  </div>
                </div>
                <Button type="text" icon={<ArrowRightOutlined />} onClick={() => navigate('/monitoring-workbench/alerts')}>
                  {localeCode === 'zh_CN' ? '查看监控工作台' : 'Open Monitoring Workbench'}
                </Button>
              </div>
              <div className="soha-overview-chip-grid">
                {alertChips.map((item) => (
                  <div key={item.key} className={`soha-overview-chip is-${item.tone}`}>
                    <span className="soha-overview-chip-label">{item.label}</span>
                    <span className="soha-overview-chip-value">{item.value}</span>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <ManagementState bordered={false} compact title={t('page.overview.noAlerts', 'No alert summary')} />
          )}
        </Card>

        <Card
          className="soha-overview-panel-card"
          title={localeCode === 'zh_CN' ? '集群健康状态' : 'Cluster Health'}
          extra={
            <Text type="secondary" className="text-xs">
              {localeCode === 'zh_CN' ? '健康 / 总数' : 'Healthy / Total'}: {healthyClusters}/{clusters.length}
            </Text>
          }
        >
          {clusters.length === 0 ? (
            <ManagementState bordered={false} compact title={t('page.overview.noClusters', 'No clusters')} />
          ) : (
            <div className="soha-overview-cluster-list">
              {clusters.map((cluster) => (
                <div key={cluster.id} className="soha-overview-cluster-row">
                  <div className="soha-overview-cluster-main">
                    <div className="soha-overview-cluster-title-row">
                      <Text strong>{cluster.name}</Text>
                      <StatusTag value={cluster.health?.status ?? 'unknown'} />
                    </div>
                    <div className="soha-overview-cluster-caption">
                      {localeCode === 'zh_CN' ? '类型' : 'Type'}: {formatClusterType(cluster, localeCode)}
                    </div>
                  </div>
                  <div className="soha-overview-cluster-meta">
                    <span>{`Env: ${cluster.environment || '-'}`}</span>
                    <span>{`Mode: ${cluster.connectionMode || '-'}`}</span>
                    <span>{`Version: ${cluster.version || '-'}`}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>

      <Card
        className="soha-overview-runtime-card"
        title={localeCode === 'zh_CN' ? 'Pod 运行态势' : 'Pod Runtime'}
        extra={clusters.length > 0 ? (
          <div className="soha-overview-runtime-card-extra">
            <Button type="text" icon={<ArrowRightOutlined />} onClick={() => navigate('/workloads/pods')}>
              {localeCode === 'zh_CN' ? '查看 Pod 列表' : 'Open Pods'}
            </Button>
          </div>
        ) : null}
      >
        {clusters.length === 0 ? (
          <ManagementState bordered={false} compact kind="select-scope" title={localeCode === 'zh_CN' ? '暂无可用集群' : 'No cluster available'} />
        ) : workloadOverviewLoading ? (
          <div className="flex items-center justify-center h-56">
            <Spin size="large" />
          </div>
        ) : !workloadOverview ? (
          <ManagementState bordered={false} compact title={localeCode === 'zh_CN' ? '当前平台暂无运行态势摘要' : 'No workload runtime summary for the platform'} />
        ) : (
          <div className="soha-overview-runtime-layout">
            <div className="soha-overview-runtime-main">
              <div className="soha-overview-section-bar">
                <div>
                  <div className="soha-overview-section-kicker">
                    {localeCode === 'zh_CN' ? '运行面信号' : 'Runtime Signal'}
                  </div>
                  <Text strong className="soha-overview-runtime-scope">
                    {currentCluster?.name || (localeCode === 'zh_CN' ? '当前集群' : 'Current Cluster')}
                  </Text>
                </div>
                <div className="soha-overview-meta-pills">
                  <span className="soha-overview-pill">
                    <span className="soha-overview-pill-label">{localeCode === 'zh_CN' ? '数据来源' : 'Source'}</span>
                    <span className="soha-overview-pill-value">{formatWorkloadSource(workloadOverview.source, localeCode)}</span>
                  </span>
                  <span className="soha-overview-pill">
                    <span className="soha-overview-pill-label">{localeCode === 'zh_CN' ? '更新时间' : 'Updated'}</span>
                    <span className="soha-overview-pill-value">{updatedAt}</span>
                  </span>
                </div>
              </div>

              <div className="soha-overview-pod-grid">
                {podStats.map((item) => (
                  <Card key={item.key} size="small" variant="outlined" className={`soha-overview-pod-card is-${item.tone}`}>
                    <div className="soha-overview-pod-card-head">
                      <div className="soha-overview-metric-copy">
                        <Text className="soha-overview-metric-label">{item.label}</Text>
                        <Statistic value={item.value} />
                      </div>
                      <span className="soha-overview-metric-icon">{item.icon}</span>
                    </div>
                    <Text className="soha-overview-metric-helper">{item.helper}</Text>
                  </Card>
                ))}
              </div>

              <div className="soha-overview-subpanel">
                <div className="soha-overview-subpanel-head">
                  <div>
                    <Text strong>{localeCode === 'zh_CN' ? '需关注的 Pod' : 'Pods Requiring Attention'}</Text>
                    <div className="soha-overview-inline-caption">
                      {localeCode === 'zh_CN' ? '先看异常实例，再下钻到详情页定位节点、重启与就绪状态。' : 'Start with the exceptions, then drill into pod details for node, restart, and readiness context.'}
                    </div>
                  </div>
                  <Text type="secondary" className="text-xs">
                    {localeCode === 'zh_CN' ? '更新时间' : 'Updated'}: {updatedAt}
                  </Text>
                </div>
                {problematicPods.length === 0 ? (
                  <ManagementState bordered={false} compact title={localeCode === 'zh_CN' ? '当前平台没有需要关注的 Pod' : 'No pods require attention in the platform scope'} />
                ) : (
                  <div className="soha-overview-attention-list">
                    {problematicPods.map((item) => (
                      <div key={`${item.namespace}/${item.name}`} className="soha-overview-attention-row">
                        <div className="soha-overview-attention-main">
                          <Text strong>{item.name}</Text>
                          <StatusTag value={item.phase} />
                        </div>
                        <div className="soha-overview-attention-meta">
                          <span>{`Cluster: ${item.clusterName}`}</span>
                          <span>{`NS: ${item.namespace}`}</span>
                          <span>{`Node: ${item.nodeName || '-'}`}</span>
                          <span>{`Ready: ${item.readyContainers}`}</span>
                          <span>{`Restarts: ${item.restarts}`}</span>
                          <span>{`Age: ${formatAgeSeconds(item.ageSeconds)}`}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>

            <div className="soha-overview-runtime-side">
              <div className="soha-overview-subpanel">
                <div className="soha-overview-subpanel-head">
                  <div>
                    <Text strong>
                      {localeCode === 'zh_CN' ? '命名空间热点' : 'Namespace Hotspots'}
                    </Text>
                    <div className="soha-overview-inline-caption">
                      {localeCode === 'zh_CN' ? '先看哪些命名空间承载了更多 Pod 和风险信号。' : 'Use this to spot which namespaces carry most of the pod volume and risk pressure.'}
                    </div>
                  </div>
                </div>
                {namespaceBreakdown.length === 0 ? (
                  <ManagementState bordered={false} compact title={localeCode === 'zh_CN' ? '当前平台暂无 Pod 分布数据' : 'No namespace distribution in the platform scope'} />
                ) : (
                  <div className="soha-overview-namespace-list">
                    {namespaceBreakdown.map((item) => (
                      <div key={`${item.clusterId}:${item.namespace}`} className="soha-overview-namespace-row">
                        <div className="soha-overview-namespace-main">
                          <Text strong>{item.namespace}</Text>
                          <div className="soha-overview-cluster-caption">
                            {`Cluster: ${item.clusterName}`}
                          </div>
                        </div>
                        <div className="soha-overview-namespace-meta">
                          <span>{`Pods: ${item.totalPods}`}</span>
                          <span>{`Running: ${item.runningPods}`}</span>
                          <span>{`${localeCode === 'zh_CN' ? '需关注' : 'At-risk'}: ${item.atRiskPods}`}</span>
                          <span>{`${localeCode === 'zh_CN' ? '重启' : 'Restarts'}: ${item.restartingPods}`}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </div>
        )}
      </Card>
    </div>
  )
}
