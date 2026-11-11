import { resourceInsightQueries } from '@/features/platform/shared/resource-insights/queries'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback, useInlineApiError } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAIPageContext } from '@/features/copilot'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth-store'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Spin } from 'antd'
import { lazy, Suspense, useMemo, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useWorkloadDetailScope } from '../shared/detail-scope'
import { WorkloadDetailShell } from '../shared/detail-shell'
import { WorkloadDetailHeader } from '../shared/workload-detail-header'
import { WorkloadRefresh, useWorkloadRefresh } from '../shared/workload-refresh'
import { WorkloadDetailEvents } from '../shared/detail-events'
import { workloadKeys } from '../shared/keys'
import { deploymentQueries } from './queries'
import { usePodObservations } from '../shared/use-pod-observations'
import { buildDeploymentFacts } from './deployment-detail-model'
import { DeploymentDetailOverview, deploymentDetailState } from './deployment-detail-overview'
import { DeploymentDetailPods } from './deployment-detail-pods'
import { DeploymentDetailHistory } from './deployment-detail-history'
import { useDeploymentDetailActions } from './use-deployment-detail-actions'
import './deployment-detail.css'

const Metrics = lazy(async () => ({
  default: (await import('../shared/detail-metrics')).WorkloadDetailMetrics,
}))
const Resources = lazy(async () => ({
  default: (await import('./deployment-detail-resources')).DeploymentDetailResources,
}))
const Delivery = lazy(async () => ({
  default: (await import('./deployment-detail-delivery')).DeploymentDetailDelivery,
}))
const tabs = [
  'overview',
  'pods',
  'revisions',
  'events',
  'metrics',
  'related-resources',
  'delivery',
  'yaml',
]

export function DeploymentDetailPage() {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const { deploymentName = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const { clusterId, namespace } = useWorkloadDetailScope()
  const scope = useMemo(() => ({ clusterId, namespace }), [clusterId, namespace])
  const permissions = usePermissionSnapshot()
  const permissionData = readableQueryData(permissions)?.data
  const canRead = hasPermission(permissionData, 'platform.deployment.view')
  const requestedTab =
    params.get('tab') === 'relationships' || params.get('tab') === 'resources'
      ? 'related-resources'
      : params.get('tab') || 'overview'
  const active = tabs.includes(requestedTab) ? requestedTab : 'overview'
  const requestedRange = Number(params.get('range') || 60)
  const range = [15, 60, 360, 1440].includes(requestedRange) ? requestedRange : 60
  const changeTab = (key: string) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('tab', key)
      return next
    })
  const changeRange = (value: number) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        next.set('range', String(value))
        return next
      },
      { replace: true },
    )
  const detailRefresh = useWorkloadRefresh()
  const detailOptions = deploymentQueries.detail(scope, deploymentName)
  const detailQuery = useQuery({
    ...detailOptions,
    refetchInterval: detailRefresh.interval,
    refetchIntervalInBackground: false,
    enabled: Boolean(detailOptions.enabled && canRead),
  })
  const rawDetail = canRead ? readableQueryData(detailQuery) : undefined
  const detail =
    rawDetail?.name === deploymentName && rawDetail.namespace === namespace ? rawDetail : undefined
  const canReadPods = canRead && hasPermission(permissionData, 'platform.pods.view')
  const { query: podListQuery, pods: completePods } = usePodObservations(
    scope,
    detail?.pods || [],
    Boolean(detail && canReadPods && active === 'pods'),
  )
  const statusOptions = deploymentQueries.rolloutStatus(scope, deploymentName)
  const statusQuery = useQuery({
    ...statusOptions,
    refetchInterval: detailRefresh.interval,
    refetchIntervalInBackground: false,
    enabled: Boolean(statusOptions.enabled && canRead),
  })
  const status = canRead ? readableQueryData(statusQuery) : undefined
  const historyOptions = deploymentQueries.rollouts(scope, deploymentName)
  const historyQuery = useQuery({
    ...historyOptions,
    enabled: Boolean(historyOptions.enabled && canRead),
  })
  const history = canRead ? readableQueryData(historyQuery) : undefined
  // The Pods table needs ownership evidence even when opened directly.
  const sessionGeneration = useAuthStore((state) => state.sessionGeneration)
  const graphOptions = resourceInsightQueries.graph(
    scope,
    'Deployment',
    deploymentName,
    sessionGeneration,
  )
  const graphQuery = useQuery({
    ...graphOptions,
    enabled: Boolean(graphOptions.enabled && detail && canReadPods && active === 'pods'),
    staleTime: 30_000,
    retry: (failureCount, error) => !queryAccessDenied({ error }) && failureCount < 3,
  })
  const graph = canRead ? readableQueryData(graphQuery) : undefined
  const metricsOptions = deploymentQueries.metrics(scope, deploymentName, range)
  const metricsQuery = useQuery({
    ...metricsOptions,
    enabled: Boolean(metricsOptions.enabled && canRead && active === 'metrics'),
  })
  const metrics = canRead && active === 'metrics' ? readableQueryData(metricsQuery) : undefined
  const eventsOptions = deploymentQueries.events(scope, deploymentName)
  const eventsQuery = useQuery({
    ...eventsOptions,
    enabled: Boolean(eventsOptions.enabled && canRead && active === 'events'),
  })
  const events = canRead && active === 'events' ? readableQueryData(eventsQuery) : undefined
  useInlineApiError(active === 'metrics' ? metricsQuery.error : null)
  useInlineApiError(active === 'events' ? eventsQuery.error : null)
  useInlineApiError(active === 'revisions' ? historyQuery.error : null)
  const model = useMemo(
    () =>
      detail
        ? buildDeploymentFacts(detail, scope, history || [], status?.revision, graph)
        : undefined,
    [detail, scope, history, status?.revision, graph],
  )
  const actions = useDeploymentDetailActions(detail, scope, status?.revision)
  const client = useQueryClient()
  const [refreshToken, setRefreshToken] = useState(0)
  const identity = JSON.stringify([clusterId, namespace, deploymentName])
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: `Deployment ${deploymentName}`,
    entityKind: 'kubernetes.deployment',
    entityName: deploymentName,
    clusterId: clusterId || undefined,
    namespace: namespace || undefined,
    workload: deploymentName,
    timeRangeMinutes: metrics?.rangeMinutes || range,
    pinnedData: {
      pods: detail?.pods?.length,
      rolloutStatus: status?.status,
      desiredReplicas: detail?.desiredReplicas,
    },
  })
  const errorText = (error: unknown) =>
    error instanceof Error ? error.message : error ? String(error) : undefined
  if (permissions.isLoading) return <Spin />
  if (permissions.isError && !permissionData)
    return <ReadFeedback query={permissions} label={zh ? '权限' : 'Permissions'} />
  if (!canRead)
    return (
      <ManagementState
        compact
        kind="no-permission"
        title={zh ? '无权查看 Deployment' : 'No permission to view Deployment'}
      />
    )
  if (!detailOptions.enabled)
    return (
      <ManagementState
        compact
        kind="empty"
        title={
          zh
            ? '请选择集群和命名空间后查看 Deployment'
            : 'Select a cluster and namespace to view this Deployment'
        }
      />
    )
  if (!detail)
    return (
      <>
        <ReadFeedback
          canRead={canRead}
          query={detailQuery}
          label={zh ? 'Deployment 详情' : 'Deployment detail'}
        />
        {detailQuery.isLoading && <Spin />}
        {canRead && !detailQuery.isPending && !detailQuery.isError && (
          <ManagementState
            compact
            kind="not-found"
            title={zh ? 'Deployment 未找到' : 'Deployment not found'}
          />
        )}
      </>
    )
  return (
    <div className="soha-deployment-detail">
      {actions.feedback}
      <ReadFeedback canRead query={statusQuery} label={zh ? '滚动发布状态' : 'Rollout status'} />
      {active !== 'revisions' && (
        <ReadFeedback canRead query={historyQuery} label={zh ? '版本历史' : 'Revision history'} />
      )}
      {active === 'pods' && (
        <>
          <ReadFeedback
            canRead={canReadPods}
            query={podListQuery}
            label={zh ? 'Pod 用量' : 'Pod usage'}
          />
          <ReadFeedback
            canRead={canReadPods}
            query={graphQuery}
            label={zh ? 'Pod 版本归属' : 'Pod revision ownership'}
          />
        </>
      )}
      <WorkloadDetailShell
        title="Deployment"
        resource="deployments"
        paramKey="deploymentName"
        activeTabKey={active}
        onTabChange={changeTab}
        detailRefetchOnMount={false}
        hideMetadataOverview
        hideRelationshipsTab
        yamlLast
        header={
          <WorkloadDetailHeader
            kind="Deployment"
            name={detail.name}
            namespace={detail.namespace}
            backLabel={zh ? '返回 Deployments' : 'Back to Deployments'}
            backTo={`/workloads/deployments?clusterId=${encodeURIComponent(clusterId || '')}&namespace=${encodeURIComponent(namespace || '')}`}
            actions={
              <>
                <WorkloadRefresh
                  beforeRefresh={actions.buttons}
                  resourceLabel="Deployment"
                  settings={detailRefresh}
                  queries={[
                    detailQuery,
                    statusQuery,
                    historyQuery,
                    ...(active === 'pods' ? [podListQuery, graphQuery] : []),
                    ...(active === 'metrics' ? [metricsQuery] : []),
                    ...(active === 'events' ? [eventsQuery] : []),
                  ]}
                  onRefresh={() => {
                    void client.invalidateQueries({
                      queryKey: workloadKeys.detail('deployments', scope, deploymentName),
                    })
                    if (podListQuery.isEnabled) void podListQuery.refetch()
                    if (graphQuery.isEnabled) void graphQuery.refetch()
                    setRefreshToken((value) => value + 1)
                  }}
                />
                {actions.deleteButton}
              </>
            }
            status={
              <StatusTag
                value={deploymentDetailState(detail, status)}
                label={
                  deploymentDetailState(detail, status) === 'scaled-to-zero'
                    ? zh
                      ? '已缩容至 0'
                      : 'Scaled to zero'
                    : undefined
                }
              />
            }
            facts={
              <>
                <span>
                  {detail.readyReplicas} / {detail.desiredReplicas} {zh ? '就绪' : 'ready'}
                </span>
                <span>
                  {zh ? '版本' : 'Revision'} {status?.revision || '—'}
                </span>

                <span>{detail.strategy}</span>
              </>
            }
          />
        }
        extraOverview={
          model ? (
            <DeploymentDetailOverview
              detail={detail}
              status={status}
              scope={scope}
              model={model}
              onTabChange={changeTab}
            />
          ) : null
        }
        extraTabPanes={[
          {
            key: 'pods',
            label: 'Pods',
            children:
              active === 'pods' && model ? (
                <DeploymentDetailPods
                  key={identity}
                  pods={completePods}
                  scope={scope}
                  model={model}
                  currentRevision={status?.revision}
                />
              ) : null,
          },
          {
            key: 'revisions',
            label: zh ? '版本历史' : 'Revision history',
            children:
              active === 'revisions' ? (
                <DeploymentDetailHistory
                  key={identity}
                  scope={scope}
                  records={history || []}
                  currentRevision={status?.revision}
                  loading={historyQuery.isLoading}
                  error={errorText(historyQuery.error)}
                  canRollback={actions.canRollback}
                  blocked={actions.blocked}
                  onRollback={actions.openRollback}
                  onRefresh={() => void historyQuery.refetch()}
                />
              ) : null,
          },
          {
            key: 'events',
            label: zh ? '事件' : 'Events',
            children:
              active === 'events' ? (
                <WorkloadDetailEvents
                  key={identity}
                  resourceLabel="Deployment"
                  events={events || []}
                  observedAt={eventsQuery.dataUpdatedAt || Date.now()}
                  loading={eventsQuery.isLoading}
                  error={errorText(eventsQuery.error)}
                  onRefresh={() => void eventsQuery.refetch()}
                />
              ) : null,
          },
          {
            key: 'metrics',
            label: zh ? '指标' : 'Metrics',
            children:
              active === 'metrics' ? (
                <Suspense fallback={<Spin />}>
                  <Metrics
                    mode="workload"
                    scopeDescription={
                      zh
                        ? `Deployment 所属 Pods 聚合 · ${detail.namespace} / ${detail.name}`
                        : `Aggregate of Deployment Pods · ${detail.namespace} / ${detail.name}`
                    }
                    data={metrics}
                    rangeMinutes={range}
                    onRangeChange={changeRange}
                    loading={metricsQuery.isLoading}
                    errorMessage={errorText(metricsQuery.error)}
                    emptyDescription={
                      detail.desiredReplicas === 0 && !detail.pods?.length
                        ? zh
                          ? '当前没有 Pod，暂无指标'
                          : 'No Pods; metrics unavailable'
                        : undefined
                    }
                    onRefresh={() => void metricsQuery.refetch()}
                  />
                </Suspense>
              ) : null,
          },
          {
            key: 'related-resources',
            label: zh ? '关联资源' : 'Related resources',
            children:
              active === 'related-resources' && model ? (
                <Suspense fallback={<Spin />}>
                  <Resources
                    key={identity}
                    scope={scope}
                    name={deploymentName}
                    model={model}
                    onShowPods={() => changeTab('pods')}
                  />
                </Suspense>
              ) : null,
          },
          {
            key: 'delivery',
            label: zh ? '交付联动' : 'Delivery integration',
            children:
              active === 'delivery' ? (
                <Suspense fallback={<Spin />}>
                  <Delivery
                    key={identity}
                    scope={scope}
                    name={deploymentName}
                    refreshToken={refreshToken}
                  />
                </Suspense>
              ) : null,
          },
        ]}
      />
      {actions.confirmation}
    </div>
  )
}
