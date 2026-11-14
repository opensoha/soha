import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { deliveryQueries } from '@/features/delivery'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Spin } from 'antd'
import { lazy, Suspense, useMemo } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useWorkloadDetailScope } from '../shared/detail-scope'
import { WorkloadDetailShell } from '../shared/detail-shell'
import { WorkloadDetailHeader } from '../shared/workload-detail-header'
import { WorkloadRefresh, useWorkloadRefresh } from '../shared/workload-refresh'
import { WorkloadDetailEvents } from '../shared/detail-events'
import { WorkloadPodsTable } from '../shared/workload-pods-table'
import { usePodObservations } from '../shared/use-pod-observations'
import { useWorkloadDetailActions } from '../shared/use-workload-detail-actions'
import { workloadMutations } from '../shared/mutations'
import { workloadKeys } from '../shared/keys'
import { ReplicaControllerOverview } from '../shared/replica-controller-overview'
import { ReplicationControllerSources } from './replicationcontroller-sources'
import { replicaControllerHealth } from '../shared/replica-controller-model'
import { replicationControllerQueries } from './queries'

const Metrics = lazy(async () => ({
  default: (await import('../shared/detail-metrics')).WorkloadDetailMetrics,
}))
const Resources = lazy(async () => ({
  default: (await import('../shared/controller-related-resources')).ControllerRelatedResources,
}))
const tabs = ['overview', 'pods', 'events', 'metrics', 'related-resources', 'yaml']
export function ReplicationControllerDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { replicationControllerName: name = '' } = useParams()
  const detailScope = useWorkloadDetailScope()
  const scope = useMemo(
    () => ({ clusterId: detailScope.clusterId, namespace: detailScope.namespace }),
    [detailScope.clusterId, detailScope.namespace],
  )
  const permissions = usePermissionSnapshot(),
    permissionData = readableQueryData(permissions)?.data
  const canRead = hasPermission(permissionData, 'platform.workloads.replication-controllers.view')
  const [params, setParams] = useSearchParams(),
    requested = params.get('tab') || 'overview'
  const normalized = ['relationships', 'resources'].includes(requested)
    ? 'related-resources'
    : requested
  const active = tabs.includes(normalized) ? normalized : 'overview'
  const rv = Number(params.get('range') || 60),
    range = [15, 60, 360, 1440].includes(rv) ? rv : 60
  const changeTab = (tab: string) =>
    setParams((old) => {
      const next = new URLSearchParams(old)
      next.set('tab', tab)
      return next
    })
  const refresh = useWorkloadRefresh(),
    options = replicationControllerQueries.detail(scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(canRead && options.enabled),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const raw = canRead ? readableQueryData(query) : undefined
  const detail = raw?.name === name && raw.namespace === scope.namespace ? raw : undefined
  const co = replicationControllerQueries.configuration(scope, name)
  const configuration = useQuery({
    ...co,
    enabled: Boolean(canRead && detail && co.enabled && active === 'overview'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const mo = replicationControllerQueries.metrics(scope, name, range)
  const metrics = useQuery({
    ...mo,
    enabled: Boolean(canRead && mo.enabled && active === 'metrics'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const eo = replicationControllerQueries.events(scope, name)
  const events = useQuery({
    ...eo,
    enabled: Boolean(canRead && eo.enabled && active === 'events'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const canReadDelivery = canRead && hasPermission(permissionData, 'delivery.applications.view')
  const bindings = useQuery({
    ...deliveryQueries.environments.list(),
    enabled: Boolean(canReadDelivery && detail && active === 'overview'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const observed = usePodObservations(
    scope,
    detail?.pods || [],
    Boolean(detail?.pods?.length && active === 'pods'),
  )
  const client = useQueryClient()
  const actions = useWorkloadDetailActions({
    detail,
    scope,
    resource: 'replicationcontrollers',
    kind: 'ReplicationController',
    permission: 'platform.workloads.replication-controllers',
    mutations: { remove: (client) => workloadMutations.remove('replicationcontrollers', client) },
  })
  const identity = JSON.stringify([scope.clusterId, scope.namespace, name])
  if (permissions.isLoading) return <Spin />
  if (permissions.isError && !permissionData)
    return <ReadFeedback query={permissions} label={zh ? '权限' : 'Permissions'} />
  if (!canRead)
    return (
      <ManagementState
        compact
        kind="no-permission"
        title={
          zh ? '无权查看 ReplicationController' : 'No permission to view ReplicationController'
        }
      />
    )
  if (!options.enabled)
    return (
      <ManagementState
        compact
        title={zh ? '请选择集群和命名空间' : 'Select a cluster and namespace'}
      />
    )
  if (!detail)
    return (
      <>
        <ReadFeedback canRead query={query} label="ReplicationController" />
        {query.isLoading ? (
          <Spin />
        ) : !query.isError ? (
          <ManagementState
            compact
            kind="not-found"
            title={zh ? 'ReplicationController 未找到' : 'ReplicationController not found'}
          />
        ) : null}
      </>
    )
  const health = replicaControllerHealth(detail, readableQueryData(configuration), true)
  return (
    <div className="soha-replicationcontroller-detail">
      {actions.feedback}
      <WorkloadDetailShell
        ownerManagedTabs
        title="ReplicationController"
        resource="replicationcontrollers"
        paramKey="replicationControllerName"
        activeTabKey={active}
        onTabChange={changeTab}
        detailRefetchOnMount={false}
        hideMetadataOverview
        hideRelationshipsTab
        yamlLast
        header={
          <WorkloadDetailHeader
            kind="ReplicationController"
            name={name}
            namespace={detail.namespace}
            status={
              <StatusTag
                value={health}
                label={
                  health === 'healthy'
                    ? zh
                      ? '健康'
                      : 'Healthy'
                    : health === 'inactive'
                      ? zh
                        ? '期望为 0'
                        : 'Zero desired'
                      : health === 'unknown'
                        ? zh
                          ? '状态未知'
                          : 'Unknown'
                        : health === 'failed'
                          ? zh
                            ? '副本失败'
                            : 'Replica failure'
                          : zh
                            ? '副本未就绪或不可用'
                            : 'Not ready or available'
                }
              />
            }
            facts={
              <>
                <span>
                  {detail.readyReplicas ?? '—'} / {detail.desiredReplicas ?? '—'}{' '}
                  {zh ? '就绪' : 'ready'}
                </span>
                <span>
                  {zh ? '可用' : 'Available'} {detail.availableReplicas ?? '—'}
                </span>
                <span>
                  {zh ? '已创建' : 'Created'}{' '}
                  {detail.createdAt
                    ? formatAgeSeconds(
                        Math.max(0, (Date.now() - Date.parse(detail.createdAt)) / 1000),
                      )
                    : '—'}
                </span>
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  resourceLabel="ReplicationController"
                  settings={refresh}
                  queries={[query, bindings, configuration, events, metrics, observed.query]}
                  onRefresh={() => {
                    void client.invalidateQueries({
                      queryKey: workloadKeys.detail('replicationcontrollers', scope, name),
                    })
                    if (bindings.isEnabled) void bindings.refetch()
                    if (observed.query.isEnabled) void observed.query.refetch()
                  }}
                />
                {actions.deleteButton}
              </>
            }
          />
        }
        extraOverview={
          <>
            <ReadFeedback
              canRead={configuration.isEnabled}
              query={configuration}
              label={zh ? 'ReplicationController 模板' : 'ReplicationController template'}
            />
            <ReplicaControllerOverview
              kind="ReplicationController"
              currentReplicas={detail.currentReplicas}
              asideExtra={
                <ReplicationControllerSources
                  detail={detail}
                  scope={scope}
                  bindings={bindings}
                  canRead={canReadDelivery}
                />
              }
              detail={detail}
              scope={scope}
              configuration={readableQueryData(configuration)}
              onTabChange={changeTab}
            />
          </>
        }
        extraTabPanes={[
          {
            key: 'pods',
            label: 'Pods',
            children:
              active === 'pods' ? (
                <>
                  <ReadFeedback
                    canRead={observed.query.isEnabled}
                    query={observed.query}
                    label={zh ? 'Pod 用量' : 'Pod observations'}
                  />
                  <WorkloadPodsTable key={identity} pods={observed.pods} scope={scope} />
                </>
              ) : null,
          },
          {
            key: 'events',
            label: zh ? '事件' : 'Events',
            children:
              active === 'events' ? (
                <WorkloadDetailEvents
                  key={identity}
                  resourceLabel="ReplicationController"
                  events={readableQueryData(events) || []}
                  observedAt={events.dataUpdatedAt || Date.now()}
                  loading={events.isLoading}
                  error={events.error?.message}
                  onRefresh={() => void events.refetch()}
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
                    data={readableQueryData(metrics)}
                    loading={metrics.isLoading}
                    errorMessage={metrics.error?.message}
                    scopeDescription={`${zh ? '当前 ReplicationController UID 所属 Pods 聚合' : 'Pods owned by the current ReplicationController UID'} · ${detail.namespace} / ${name}`}
                    rangeMinutes={range}
                    onRangeChange={(v) =>
                      setParams(
                        (old) => {
                          const next = new URLSearchParams(old)
                          next.set('range', String(v))
                          return next
                        },
                        { replace: true },
                      )
                    }
                    onRefresh={() => void metrics.refetch()}
                  />
                </Suspense>
              ) : null,
          },
          {
            key: 'related-resources',
            label: zh ? '关联资源' : 'Related resources',
            children:
              active === 'related-resources' ? (
                <Suspense fallback={<Spin />}>
                  <Resources
                    key={identity}
                    kind="ReplicationController"
                    detail={{
                      ...detail,
                      relatedResources: detail.relatedResources?.filter(
                        (r) => r.relation !== 'owner' || r.namespace === detail.namespace,
                      ),
                    }}
                    scope={scope}
                    onShowPods={() => changeTab('pods')}
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
