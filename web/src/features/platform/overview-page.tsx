import { ManagementState } from '@/components/management-list'
import {
  OverviewChip,
  OverviewMetricCard,
  OverviewSectionBar,
  type OverviewChipItem,
  type OverviewMetricItem,
} from '@/components/overview-visuals'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { observabilityAlertQueries, useAlertEventStream } from '@/features/observability'
import { ResourceStreamStatus } from '@/features/platform/shared/resource-stream-status'
import { buildWorkloadDetailPath } from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type { Cluster } from '@/types'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import {
  AppstoreOutlined,
  ArrowRightOutlined,
  FireOutlined,
  HddOutlined,
  QuestionCircleOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Space, Spin, Tooltip, Typography } from 'antd'
import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import './overview.css'
import {
  filterAlertsForCluster,
  formatPlatformOverviewAlertScope,
  formatPlatformOverviewText,
  selectActiveAlerts,
  summarizeFleetReadiness,
} from './overview/model'
import { PlatformOperationsPanel } from './overview/operations-panel'
import { platformOverviewQueries } from './overview/queries'
import { ResourceFinder, type OverviewResourceKindOption } from './overview/resource-finder'
import type { AggregatedWorkloadOverview } from './overview/types'
import { nodeQueries } from './cluster-resources/queries'
import { WorkloadRefresh, useWorkloadRefresh } from './workloads/shared/workload-refresh'

const { Text } = Typography

function OverviewHint({ title, label }: { title: string; label: string }) {
  return (
    <Tooltip title={title} trigger={['hover', 'focus']}>
      <Button
        type="text"
        size="small"
        className="soha-platform-overview-hint"
        aria-label={label}
        icon={<QuestionCircleOutlined />}
      />
    </Tooltip>
  )
}

const clusterTypeLabels: Record<string, { zh: string; en: string }> = {
  standard_kubernetes: { zh: '标准 Kubernetes', en: 'Standard Kubernetes' },
  k3s: { zh: 'K3s', en: 'K3s' },
  gke: { zh: 'GKE', en: 'GKE' },
  eks: { zh: 'EKS', en: 'EKS' },
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

export function OverviewPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  // Overview is deliberately cluster-wide, irrespective of the namespace selector.
  const clusterId = usePlatformScopeStore((state) => state.clusterId)
  const setNamespace = usePlatformScopeStore((state) => state.setNamespace)
  const refresh = useWorkloadRefresh()
  const [refreshToken, setRefreshToken] = useState(0)
  const [operationsRefreshState, setOperationsRefreshState] = useState({
    isFetching: false,
    isError: false,
  })
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canViewClusters = hasPermission(permissionSnapshot, 'platform.clusters.view')
  const canViewMonitoring = hasPermission(permissionSnapshot, 'observe.monitoring.view')
  const canViewAlerts = hasPermission(permissionSnapshot, 'observe.alerts.view')
  const canViewWorkloads = hasPermission(permissionSnapshot, 'platform.pods.view')
  const canViewNodes = hasPermission(permissionSnapshot, 'platform.nodes.view')
  const nodesQuery = useQuery({
    ...nodeQueries.list({ clusterId, namespace: null }),
    enabled: canViewNodes && Boolean(clusterId),
    refetchInterval: refresh.interval,
  })
  const nodes = canViewNodes && clusterId ? readableQueryData(nodesQuery) : undefined

  const clustersQuery = useQuery({
    ...platformOverviewQueries.clusters(canViewClusters),
    refetchInterval: refresh.interval,
  })
  const visibleClustersQueryData = readableQueryData(clustersQuery)

  const summaryQuery = useQuery({
    ...platformOverviewQueries.monitoringSummary(canViewMonitoring),
    refetchInterval: refresh.interval,
  })
  const visibleSummaryQueryData = readableQueryData(summaryQuery)
  const alertEventsQuery = useQuery({
    ...observabilityAlertQueries.recent(50, clusterId ?? undefined),
    refetchInterval: refresh.interval,
    enabled: canViewAlerts && Boolean(clusterId),
  })
  const visibleAlertEventsQueryData =
    canViewAlerts && Boolean(clusterId) ? readableQueryData(alertEventsQuery) : undefined
  const refreshAlertOverview = () => {
    if (!refresh.enabled || refresh.hidden) return
    if (alertEventsQuery.isEnabled && !alertEventsQuery.isError) {
      void alertEventsQuery.refetch()
    }
    if (summaryQuery.isEnabled && !summaryQuery.isError) void summaryQuery.refetch()
  }
  const alertStream = useAlertEventStream({
    clusterId,
    enabled:
      canViewAlerts &&
      Boolean(clusterId) &&
      refresh.enabled &&
      !refresh.hidden &&
      !queryAccessDenied(alertEventsQuery),
    onSignal: refreshAlertOverview,
    onFallback: refreshAlertOverview,
  })

  const clusters = canViewClusters ? (visibleClustersQueryData ?? []) : []
  const summary = canViewMonitoring ? visibleSummaryQueryData?.data : undefined
  const healthyClusters = clusters.filter((cluster) => cluster.health?.status === 'healthy').length
  const currentCluster = clusters.find((cluster) => cluster.id === clusterId) ?? null
  const fleetReadiness = summarizeFleetReadiness(clusters)
  const scopedAlertEvents = useMemo(
    () => filterAlertsForCluster(visibleAlertEventsQueryData ?? [], clusterId),
    [visibleAlertEventsQueryData, clusterId],
  )
  const activeAlertEvents = useMemo(
    () => selectActiveAlerts(scopedAlertEvents, 50),
    [scopedAlertEvents],
  )
  const activeAlerts = activeAlertEvents.slice(0, 2)
  const alertEventSeverityCounts = useMemo(
    () =>
      activeAlertEvents.reduce(
        (counts, alert) => {
          const severity = alert.severity.toLowerCase()
          if (severity === 'critical' || severity === 'warning' || severity === 'info') {
            counts[severity] += 1
          }
          return counts
        },
        { critical: 0, warning: 0, info: 0 },
      ),
    [activeAlertEvents],
  )
  const effectiveAlertCount = scopedAlertEvents.length
  const effectiveFiringCount = activeAlertEvents.length
  const resolvedAlertCount = scopedAlertEvents.filter(
    (alert) => (alert.currentState || alert.status).toLowerCase() === 'resolved',
  ).length
  const resourceSearchOptions = useMemo(
    () =>
      [
        {
          value: 'pods',
          label: 'Pods',
          allowed: hasPermission(permissionSnapshot, 'platform.pods.view'),
        },
        {
          value: 'deployments',
          label: 'Deployments',
          allowed: hasPermission(permissionSnapshot, 'platform.deployment.view'),
        },
        {
          value: 'services',
          label: 'Services',
          allowed: hasPermission(permissionSnapshot, 'platform.network.services.view'),
        },
        {
          value: 'configmaps',
          label: 'ConfigMaps',
          allowed: hasPermission(permissionSnapshot, 'platform.configuration.config-maps.view'),
        },
        {
          value: 'namespaces',
          label: localeCode === 'zh_CN' ? '命名空间' : 'Namespaces',
          allowed: hasPermission(permissionSnapshot, 'platform.namespaces.view'),
        },
        {
          value: 'nodes',
          label: localeCode === 'zh_CN' ? '节点' : 'Nodes',
          allowed: hasPermission(permissionSnapshot, 'platform.nodes.view'),
        },
        {
          value: 'hpas',
          label: 'HPAs',
          allowed: hasPermission(
            permissionSnapshot,
            'platform.configuration.horizontal-pod-autoscalers.view',
          ),
        },
        {
          value: 'networkpolicies',
          label: 'NetworkPolicies',
          allowed: hasPermission(permissionSnapshot, 'platform.network.network-policies.view'),
        },
      ]
        .filter((item) => item.allowed)
        .map(({ value, label }) => ({ value, label })) as OverviewResourceKindOption[],
    [localeCode, permissionSnapshot],
  )

  const workloadOverviewQuery = useQuery({
    ...platformOverviewQueries.workload(clusterId, canViewWorkloads),
    refetchInterval: refresh.interval,
  })
  const visibleWorkloadOverviewQueryData = readableQueryData(workloadOverviewQuery)

  const workloadOverviewLoading = canViewWorkloads && workloadOverviewQuery.isLoading
  const workloadOverviewData = canViewWorkloads
    ? (visibleWorkloadOverviewQueryData?.data ?? null)
    : null

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
          if (left.restartingPods !== right.restartingPods)
            return right.restartingPods - left.restartingPods
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
          return (
            left.namespace.localeCompare(right.namespace) || left.name.localeCompare(right.name)
          )
        })
        .slice(0, 8),
    }
  }, [clusterId, currentCluster?.name, workloadOverviewData])

  const namespaceBreakdown = workloadOverview?.namespaceBreakdown ?? []
  const problematicPods = workloadOverview?.problematicPods ?? []
  const updatedAt = workloadOverview?.generatedAt
    ? formatDateTime(workloadOverview.generatedAt)
    : '-'
  const isLoading = permissionSnapshotQuery.isLoading
  const latestAlertEventAt = scopedAlertEvents.reduce<string | undefined>((latest, alert) => {
    const candidate = alert.lastSeenAt || alert.updatedAt
    return !latest || Date.parse(candidate) > Date.parse(latest) ? candidate : latest
  }, undefined)
  const lastReceivedAt = formatDateTime(latestAlertEventAt)
  const lastReceivedText =
    lastReceivedAt === '-'
      ? localeCode === 'zh_CN'
        ? '暂无接收记录'
        : 'No received alerts'
      : lastReceivedAt

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    )
  }

  if (permissionSnapshotQuery.isError) {
    return (
      <div className="soha-page soha-overview-page soha-platform-overview-page">
        <ManagementState
          kind="error"
          title={localeCode === 'zh_CN' ? '权限信息加载失败' : 'Failed to load permissions'}
        />
      </div>
    )
  }

  const workloadValue = (value: number | undefined) =>
    !clusterId || !canViewWorkloads || workloadOverviewQuery.isError || workloadOverviewLoading
      ? '-'
      : (value ?? '-')
  const overviewStats = [
    {
      key: 'nodes',
      label: localeCode === 'zh_CN' ? '就绪节点' : 'Ready nodes',
      helper:
        localeCode === 'zh_CN'
          ? '当前集群的就绪节点 / 已读取节点总数'
          : 'Ready / returned nodes in the current cluster',
      value: nodes
        ? `${nodes.filter((node) => node.status.toLowerCase() === 'ready').length} / ${nodes.length}`
        : '—',
      icon: <HddOutlined />,
      tone: nodes?.some((node) => node.status.toLowerCase() !== 'ready') ? 'warning' : 'default',
    },
    {
      key: 'pods',
      label: localeCode === 'zh_CN' ? 'Pod 总数' : 'Pods',
      helper:
        localeCode === 'zh_CN' ? '当前集群 · 全部命名空间' : 'Current cluster · All namespaces',
      value: workloadValue(workloadOverview?.totalPods),
      icon: <AppstoreOutlined />,
      tone: 'default',
    },
    {
      key: 'risk',
      label: localeCode === 'zh_CN' ? '需关注 Pod' : 'At-risk Pods',
      helper: localeCode === 'zh_CN' ? '从下方异常实例开始排查' : 'Start with the exceptions below',
      value: workloadValue(workloadOverview?.atRiskPods),
      icon: <FireOutlined />,
      tone: (workloadOverview?.atRiskPods ?? 0) > 0 ? 'danger' : 'default',
    },
    {
      key: 'alerts',
      label: localeCode === 'zh_CN' ? '近期活跃告警' : 'Recent firing alerts',
      helper:
        localeCode === 'zh_CN'
          ? '最近 50 条告警记录中的活跃项'
          : 'Active in the latest 50 alert records',
      value:
        !clusterId || !canViewAlerts || alertEventsQuery.isError || alertEventsQuery.isLoading
          ? '-'
          : effectiveFiringCount,
      icon: <WarningOutlined />,
      tone: effectiveFiringCount > 0 ? 'warning' : 'default',
    },
  ] satisfies OverviewMetricItem[]

  const alertChips = [
    {
      key: 'firing',
      label: localeCode === 'zh_CN' ? '活跃' : 'Firing',
      value: effectiveFiringCount,
      tone: 'warning',
    },
    {
      key: 'resolved',
      label: localeCode === 'zh_CN' ? '已恢复' : 'Resolved',
      value: resolvedAlertCount,
      tone: 'success',
    },
    {
      key: 'critical',
      label: formatPlatformOverviewText('critical', localeCode),
      value: alertEventSeverityCounts.critical,
      tone: 'danger',
    },
    {
      key: 'warning',
      label: formatPlatformOverviewText('warning', localeCode),
      value: alertEventSeverityCounts.warning,
      tone: 'warning',
    },
    {
      key: 'info',
      label: formatPlatformOverviewText('info', localeCode),
      value: alertEventSeverityCounts.info,
      tone: 'default',
    },
  ] satisfies OverviewChipItem[]

  const podStats = [
    {
      key: 'running',
      label: formatPlatformOverviewText('running', localeCode),
      value: workloadOverview?.runningPods ?? 0,
      tone: 'success',
    },
    {
      key: 'pending',
      label: formatPlatformOverviewText('pending', localeCode),
      value: workloadOverview?.pendingPods ?? 0,
      tone: 'warning',
    },
    {
      key: 'completed',
      label: localeCode === 'zh_CN' ? '已完成' : 'Completed',
      value: workloadOverview?.succeededPods ?? 0,
      tone: 'default',
    },
    {
      key: 'failed',
      label: localeCode === 'zh_CN' ? '失败' : 'Failed',
      value: workloadOverview?.failedPods ?? 0,
      tone: 'danger',
    },
    {
      key: 'unknown',
      label: localeCode === 'zh_CN' ? '未知' : 'Unknown',
      value: workloadOverview?.unknownPods ?? 0,
      tone: 'default',
    },
  ] satisfies OverviewChipItem[]

  return (
    <div className="soha-page soha-overview-page soha-platform-overview-page">
      <ReadFeedback canRead={clustersQuery.isEnabled} query={clustersQuery} label="集群目录" />
      <ReadFeedback canRead={summaryQuery.isEnabled} query={summaryQuery} label="监控汇总" />
      <ReadFeedback
        canRead={alertEventsQuery.isEnabled}
        query={alertEventsQuery}
        label="告警事件"
      />
      <ReadFeedback
        canRead={workloadOverviewQuery.isEnabled}
        query={workloadOverviewQuery}
        label="工作负载总览"
      />

      <h1 className="soha-platform-overview-heading">
        {localeCode === 'zh_CN' ? 'Kubernetes 总览' : 'Kubernetes overview'}
      </h1>
      <div className="soha-platform-overview-toolbar">
        <Space size={8} wrap>
          <Text strong>
            {currentCluster?.name || (localeCode === 'zh_CN' ? '当前集群' : 'Current cluster')}
          </Text>
          <Text type="secondary">{localeCode === 'zh_CN' ? '全部命名空间' : 'All namespaces'}</Text>
        </Space>
        <WorkloadRefresh
          settings={refresh}
          resourceLabel={localeCode === 'zh_CN' ? '集群总览' : 'Cluster overview'}
          disabled={!clusterId}
          queries={[
            clustersQuery,
            summaryQuery,
            alertEventsQuery,
            workloadOverviewQuery,
            nodesQuery,
            operationsRefreshState,
          ]}
          onRefresh={() => {
            setRefreshToken((value) => value + 1)
            return Promise.allSettled(
              [clustersQuery, summaryQuery, alertEventsQuery, workloadOverviewQuery]
                .filter((query) => query.isEnabled)
                .map((query) => query.refetch()),
            )
          }}
        />
      </div>
      <div className="soha-overview-metric-grid">
        {overviewStats.map((item) => (
          <OverviewMetricCard
            key={item.key}
            label={
              <Space size={2}>
                {item.label}
                <OverviewHint
                  title={item.helper}
                  label={
                    localeCode === 'zh_CN' ? `${item.label}统计说明` : `${item.label} information`
                  }
                />
              </Space>
            }
            value={item.value}
            icon={item.icon}
            tone={item.tone}
          />
        ))}
      </div>

      <div className="soha-platform-focus-grid">
        <Card
          className="soha-overview-runtime-card"
          title={localeCode === 'zh_CN' ? 'Pod 运行状态' : 'Pod runtime'}
          extra={
            canViewWorkloads && clusterId ? (
              <div className="soha-overview-runtime-card-extra">
                <Button
                  type="text"
                  icon={<ArrowRightOutlined />}
                  onClick={() => navigate('/workloads/pods')}
                >
                  {localeCode === 'zh_CN' ? '查看 Pod 列表' : 'Open Pods'}
                </Button>
              </div>
            ) : null
          }
        >
          {!canViewWorkloads ? (
            <ManagementState
              bordered={false}
              compact
              kind="no-permission"
              title={
                localeCode === 'zh_CN'
                  ? '无权限查看 Pod 运行态势'
                  : 'No permission to view pod runtime'
              }
            />
          ) : !clusterId ? (
            <ManagementState
              bordered={false}
              compact
              kind="select-scope"
              title={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'}
            />
          ) : workloadOverviewLoading ? (
            <div className="flex items-center justify-center h-56">
              <Spin size="large" />
            </div>
          ) : workloadOverviewQuery.isError ? (
            <ManagementState
              bordered={false}
              compact
              kind="error"
              title={localeCode === 'zh_CN' ? 'Pod 运行态势加载失败' : 'Failed to load pod runtime'}
            />
          ) : !workloadOverview ? (
            <ManagementState
              bordered={false}
              compact
              title={
                localeCode === 'zh_CN'
                  ? '当前平台暂无运行态势摘要'
                  : 'No workload runtime summary for the platform'
              }
            />
          ) : (
            <div className="soha-overview-runtime-layout">
              <div className="soha-overview-runtime-main">
                <div className="soha-platform-runtime-source">
                  <Text type="secondary">
                    {localeCode === 'zh_CN'
                      ? '当前集群 · 全部命名空间'
                      : 'Current cluster · All namespaces'}
                  </Text>
                  <OverviewHint
                    label={localeCode === 'zh_CN' ? '运行状态数据来源' : 'Runtime data source'}
                    title={`${localeCode === 'zh_CN' ? '来源' : 'Source'}: ${formatWorkloadSource(workloadOverview.source, localeCode)} · ${localeCode === 'zh_CN' ? '更新于' : 'Updated'} ${updatedAt}`}
                  />
                </div>

                <div className="soha-platform-pod-phases">
                  {podStats.map((item) => (
                    <OverviewChip
                      key={item.key}
                      label={item.label}
                      value={item.value}
                      tone={item.tone}
                    />
                  ))}
                </div>

                <div className="soha-overview-subpanel">
                  <div className="soha-overview-subpanel-head">
                    <div>
                      <Text strong>
                        {localeCode === 'zh_CN' ? '需关注的 Pod' : 'Pods Requiring Attention'}
                      </Text>
                    </div>
                  </div>
                  {problematicPods.length === 0 ? (
                    <ManagementState
                      bordered={false}
                      compact
                      title={
                        localeCode === 'zh_CN'
                          ? '当前集群没有需要关注的 Pod'
                          : 'No pods require attention in this cluster'
                      }
                    />
                  ) : (
                    <div
                      className="soha-overview-attention-list"
                      role="region"
                      aria-label={
                        localeCode === 'zh_CN' ? '需关注 Pod 列表' : 'Pods requiring attention list'
                      }
                      tabIndex={problematicPods.length > 3 ? 0 : undefined}
                    >
                      {problematicPods.map((item) => (
                        <div
                          key={`${item.namespace}/${item.name}`}
                          className="soha-overview-attention-row"
                        >
                          <div className="soha-overview-attention-main">
                            <Link
                              title={item.name}
                              to={buildWorkloadDetailPath(
                                'pods',
                                item.name,
                                null,
                                item.namespace,
                                item.clusterId,
                              )}
                            >
                              {item.name}
                            </Link>
                            <StatusTag value={item.phase} />
                          </div>
                          <div className="soha-overview-attention-meta">
                            <span>{`${t('common.namespace', '命名空间')}: ${item.namespace}`}</span>
                            <span>{`${t('common.node', '节点')}: ${item.nodeName || '-'}`}</span>
                            <span>{`${t('common.ready', '就绪')}: ${item.readyContainers}`}</span>
                            <span>{`${t('common.restarts', '重启次数')}: ${item.restarts}`}</span>
                            <span>{`${t('common.age', '时长')}: ${formatAgeSeconds(item.ageSeconds)}`}</span>
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
        <Card
          className="soha-overview-panel-card"
          title={localeCode === 'zh_CN' ? '当前集群告警' : 'Current Cluster Alerts'}
          extra={
            canViewAlerts && clusterId && !alertEventsQuery.isError ? (
              <Space size={6} wrap>
                <OverviewHint
                  label={localeCode === 'zh_CN' ? '告警接收信息' : 'Alert reception information'}
                  title={`${localeCode === 'zh_CN' ? '最近记录' : 'Recent records'} ${effectiveAlertCount} · ${localeCode === 'zh_CN' ? '最近接收' : 'Last received'}: ${lastReceivedText}`}
                />
                <ReadFeedback
                  label="告警事件流"
                  query={{
                    error: alertStream.error ?? null,
                    isError: Boolean(alertStream.error),
                    isFetching:
                      alertStream.status === 'connecting' || alertStream.status === 'reconnecting',
                  }}
                />
                {alertStream.error ? (
                  <Button size="small" onClick={alertStream.reconnect}>
                    重新连接事件流
                  </Button>
                ) : null}
                <ResourceStreamStatus
                  status={alertStream.status}
                  lastEventAt={alertStream.lastEventAt}
                  localeCode={localeCode}
                />
              </Space>
            ) : null
          }
        >
          {!clusterId ? (
            <ManagementState
              bordered={false}
              compact
              kind="select-scope"
              title={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'}
            />
          ) : !canViewAlerts ? (
            <ManagementState
              bordered={false}
              compact
              kind="no-permission"
              title={
                localeCode === 'zh_CN'
                  ? '无权限查看告警摘要'
                  : 'No permission to view alert summary'
              }
            />
          ) : alertEventsQuery.isError ? (
            <ManagementState
              bordered={false}
              compact
              kind="error"
              title={localeCode === 'zh_CN' ? '集群告警加载失败' : 'Failed to load cluster alerts'}
            />
          ) : alertEventsQuery.isLoading ? (
            <div className="soha-platform-ops-loading">
              <Spin size="small" />
            </div>
          ) : (
            <div className="soha-overview-alert-stack">
              <OverviewSectionBar
                title={
                  <Space size={2}>
                    {localeCode === 'zh_CN' ? '告警分布' : 'Alert distribution'}
                    <OverviewHint
                      label={localeCode === 'zh_CN' ? '告警统计说明' : 'Alert count information'}
                      title={
                        localeCode === 'zh_CN'
                          ? '仅统计当前集群最近 50 条告警记录中的活跃项；不是全量活跃告警总数。'
                          : 'Active alerts among the latest 50 records in this cluster; not the total active alert count.'
                      }
                    />
                  </Space>
                }
                extra={
                  <Button
                    type="text"
                    icon={<ArrowRightOutlined />}
                    onClick={() => navigate('/monitoring-workbench/alerts')}
                  >
                    {localeCode === 'zh_CN' ? '查看全部告警' : 'Open All Alerts'}
                  </Button>
                }
              />
              <div className="soha-overview-chip-grid">
                {alertChips.map((item) => (
                  <OverviewChip
                    key={item.key}
                    label={item.label}
                    value={item.value}
                    tone={item.tone}
                  />
                ))}
              </div>
              {activeAlerts.length > 0 ? (
                <div className="soha-platform-active-alert-list">
                  {activeAlerts.map((alert) => (
                    <button
                      key={alert.id}
                      type="button"
                      className="soha-platform-active-alert-row"
                      onClick={() =>
                        navigate(`/monitoring-workbench/alerts/${encodeURIComponent(alert.id)}`)
                      }
                    >
                      <span className="soha-platform-active-alert-copy">
                        <Text strong ellipsis>
                          {formatPlatformOverviewText(alert.title, localeCode)}
                        </Text>
                        <span>
                          {formatPlatformOverviewAlertScope(
                            alert,
                            currentCluster?.name,
                            localeCode,
                          )}
                        </span>
                      </span>
                      <span className="soha-platform-active-alert-meta">
                        <StatusTag
                          label={formatPlatformOverviewText(alert.severity, localeCode)}
                          value={alert.severity}
                        />
                        <span>{formatDateTime(alert.lastSeenAt || alert.updatedAt)}</span>
                      </span>
                    </button>
                  ))}
                </div>
              ) : null}
            </div>
          )}
        </Card>
      </div>

      <PlatformOperationsPanel
        key={clusterId || 'no-cluster'}
        clusterId={clusterId}
        refresh={refresh}
        refreshToken={refreshToken}
        onRefreshStateChange={setOperationsRefreshState}
        connectionMode={currentCluster?.connectionMode}
        localeCode={localeCode}
        permissions={{
          audit: hasPermission(permissionSnapshot, 'system.audit.view'),
          events: hasPermission(permissionSnapshot, 'platform.workloads.overview.view'),
          hpas: hasPermission(
            permissionSnapshot,
            'platform.configuration.horizontal-pod-autoscalers.view',
          ),
          mutatingWebhooks: hasPermission(
            permissionSnapshot,
            'platform.configuration.mutating-webhook-configurations.view',
          ),
          networkPolicies: hasPermission(
            permissionSnapshot,
            'platform.network.network-policies.view',
          ),
          nodes: hasPermission(permissionSnapshot, 'platform.nodes.view'),
          operations: hasPermission(permissionSnapshot, 'system.operations.view'),
          podDisruptionBudgets: hasPermission(
            permissionSnapshot,
            'platform.configuration.pod-disruption-budgets.view',
          ),
          validatingWebhooks: hasPermission(
            permissionSnapshot,
            'platform.configuration.validating-webhook-configurations.view',
          ),
        }}
      />

      <Card
        className="soha-overview-panel-card"
        title={localeCode === 'zh_CN' ? '命名空间分布' : 'Namespace distribution'}
        extra={
          <OverviewHint
            label={
              localeCode === 'zh_CN' ? '命名空间分布说明' : 'Namespace distribution information'
            }
            title={
              localeCode === 'zh_CN'
                ? '按需关注 Pod、重启记录与 Pod 数量排序，展示前 6 个命名空间。点击名称查看该命名空间的 Pods。'
                : 'Top six namespaces ordered by at-risk Pods, restart records and Pod count. Open a name to view its Pods.'
            }
          />
        }
      >
        <div className="soha-platform-namespace-distribution">
          {!canViewWorkloads ? (
            <ManagementState
              bordered={false}
              compact
              kind="no-permission"
              title={
                localeCode === 'zh_CN'
                  ? '无权限查看命名空间分布'
                  : 'No namespace distribution access'
              }
            />
          ) : !clusterId ? (
            <ManagementState
              bordered={false}
              compact
              kind="select-scope"
              title={localeCode === 'zh_CN' ? '请选择集群' : 'Select a cluster'}
            />
          ) : workloadOverviewLoading ? (
            <div className="soha-platform-ops-loading">
              <Spin size="small" />
            </div>
          ) : workloadOverviewQuery.isError ? (
            <ManagementState
              bordered={false}
              compact
              kind="error"
              title={
                localeCode === 'zh_CN'
                  ? '命名空间分布加载失败'
                  : 'Namespace distribution failed to load'
              }
            />
          ) : namespaceBreakdown.length === 0 ? (
            <ManagementState
              bordered={false}
              compact
              title={
                localeCode === 'zh_CN'
                  ? '当前平台暂无 Pod 分布数据'
                  : 'No namespace distribution in the platform scope'
              }
            />
          ) : (
            <div
              className="soha-overview-namespace-list"
              role="region"
              aria-label={localeCode === 'zh_CN' ? '命名空间热点列表' : 'Namespace hotspots list'}
              tabIndex={namespaceBreakdown.length > 3 ? 0 : undefined}
            >
              {namespaceBreakdown.map((item) => (
                <div
                  key={`${item.clusterId}:${item.namespace}`}
                  className="soha-overview-namespace-row"
                >
                  <div className="soha-overview-namespace-main">
                    <Link
                      to={`/workloads/pods?clusterId=${encodeURIComponent(item.clusterId)}&namespace=${encodeURIComponent(item.namespace)}`}
                      title={item.namespace}
                      onClick={() => setNamespace(item.namespace)}
                    >
                      {item.namespace}
                    </Link>
                  </div>
                  <div className="soha-overview-namespace-meta">
                    <span>{`Pods: ${item.totalPods}`}</span>
                    <span>{`${localeCode === 'zh_CN' ? '运行中' : 'Running'}: ${item.runningPods}`}</span>
                    <span>{`${localeCode === 'zh_CN' ? '需关注' : 'At-risk'}: ${item.atRiskPods}`}</span>
                    <span>{`${localeCode === 'zh_CN' ? '重启' : 'Restarts'}: ${item.restartingPods}`}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>
      <div className="soha-overview-summary-grid">
        <Card
          className="soha-overview-panel-card"
          title={localeCode === 'zh_CN' ? '集群目录' : 'Cluster directory'}
          extra={
            <Text type="secondary" className="text-xs">
              {localeCode === 'zh_CN' ? '健康 / 总数' : 'Healthy / Total'}: {healthyClusters}/
              {clusters.length}
            </Text>
          }
        >
          {!canViewClusters ? (
            <ManagementState
              bordered={false}
              compact
              kind="no-permission"
              title={
                localeCode === 'zh_CN'
                  ? '无权限查看集群健康状态'
                  : 'No permission to view cluster health'
              }
            />
          ) : clustersQuery.isError ? (
            <ManagementState
              bordered={false}
              compact
              kind="error"
              title={
                localeCode === 'zh_CN' ? '集群概览加载失败' : 'Failed to load cluster overview'
              }
            />
          ) : clusters.length === 0 ? (
            <ManagementState
              bordered={false}
              compact
              title={t('page.overview.noClusters', 'No clusters')}
            />
          ) : (
            <>
              {canViewMonitoring ? (
                <Text type="secondary" className="soha-platform-fleet-caption">
                  {localeCode === 'zh_CN' ? '全平台通知渠道' : 'Platform notification channels'}:{' '}
                  {summaryQuery.isError ? '-' : (summary?.channelCount ?? '-')}
                </Text>
              ) : null}
              <div className="soha-platform-fleet-summary">
                <span>
                  {localeCode === 'zh_CN' ? '健康' : 'Healthy'}{' '}
                  <strong>{fleetReadiness.healthy}</strong>
                </span>
                <span>
                  {localeCode === 'zh_CN' ? '异常' : 'Unhealthy'}{' '}
                  <strong>{fleetReadiness.unhealthy}</strong>
                </span>
                <span>
                  {localeCode === 'zh_CN' ? '超时' : 'Stale'}{' '}
                  <strong>{fleetReadiness.stale}</strong>
                </span>
                <span>
                  {localeCode === 'zh_CN' ? '版本轨道' : 'Versions'}{' '}
                  <strong>{fleetReadiness.versions}</strong>
                </span>
              </div>
              <div
                className="soha-overview-cluster-list"
                role="region"
                aria-label={localeCode === 'zh_CN' ? '集群健康列表' : 'Cluster health list'}
                tabIndex={clusters.length > 3 ? 0 : undefined}
              >
                {clusters.map((cluster) => (
                  <div key={cluster.id} className="soha-overview-cluster-row">
                    <div className="soha-overview-cluster-main">
                      <div className="soha-overview-cluster-title-row">
                        <Link to={`/clusters/${encodeURIComponent(cluster.id)}`}>
                          {cluster.name}
                        </Link>
                        <StatusTag
                          label={formatPlatformOverviewText(
                            cluster.health?.status ?? 'unknown',
                            localeCode,
                          )}
                          value={cluster.health?.status ?? 'unknown'}
                        />
                      </div>
                      <div className="soha-overview-cluster-caption">
                        {localeCode === 'zh_CN' ? '类型' : 'Type'}:{' '}
                        {formatClusterType(cluster, localeCode)}
                      </div>
                    </div>
                    <div className="soha-overview-cluster-meta">
                      <span>
                        {localeCode === 'zh_CN' ? '环境' : 'Env'}: {cluster.environment || '-'}
                      </span>
                      <span>
                        {localeCode === 'zh_CN' ? '连接方式' : 'Mode'}:{' '}
                        {formatPlatformOverviewText(cluster.connectionMode || '-', localeCode)}
                      </span>
                      <span>
                        {localeCode === 'zh_CN' ? '版本' : 'Version'}: {cluster.version || '-'}
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            </>
          )}
        </Card>
        <ResourceFinder
          clusterId={clusterId}
          clusterName={currentCluster?.name}
          localeCode={localeCode}
          options={resourceSearchOptions}
        />
      </div>
    </div>
  )
}
