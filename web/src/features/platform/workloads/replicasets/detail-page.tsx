import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
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
import { ReplicaSetDetailOverview } from './replicaset-detail-overview'
import { replicaSetHealth, replicaSetOwner, replicaSetRevision } from './replicaset-detail-model'
import { replicaSetQueries } from './queries'
import './replicaset-detail.css'

const Metrics = lazy(async () => ({
  default: (await import('../shared/detail-metrics')).WorkloadDetailMetrics,
}))
const Resources = lazy(async () => ({
  default: (await import('../shared/controller-related-resources')).ControllerRelatedResources,
}))
const tabs = ['overview', 'pods', 'events', 'metrics', 'related-resources', 'yaml']
export function ReplicaSetDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { replicaSetName: name = '' } = useParams()
  const detailScope = useWorkloadDetailScope()
  const scope = useMemo(
    () => ({ clusterId: detailScope.clusterId, namespace: detailScope.namespace }),
    [detailScope.clusterId, detailScope.namespace],
  )
  const permissions = usePermissionSnapshot(),
    permissionData = readableQueryData(permissions)?.data
  const canRead = hasPermission(permissionData, 'platform.workloads.replica-sets.view')
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
    options = replicaSetQueries.detail(scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(canRead && options.enabled),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const raw = canRead ? readableQueryData(query) : undefined
  const detail = raw?.name === name && raw.namespace === scope.namespace ? raw : undefined
  const owner = detail ? replicaSetOwner(detail) : undefined
  const canReadParent = canRead && hasPermission(permissionData, 'platform.deployment.view')
  const co = replicaSetQueries.configuration(scope, name)
  const configuration = useQuery({
    ...co,
    enabled: Boolean(canRead && detail && co.enabled && active === 'overview'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const ho = replicaSetQueries.ownerHistory(scope, owner?.name || '')
  const ownerHistory = useQuery({
    ...ho,
    enabled: Boolean(canReadParent && owner && ho.enabled && active === 'overview'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const so = replicaSetQueries.ownerStatus(scope, owner?.name || '')
  const ownerStatus = useQuery({
    ...so,
    enabled: Boolean(canReadParent && owner && so.enabled && active === 'overview'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const mo = replicaSetQueries.metrics(scope, name, range)
  const metrics = useQuery({
    ...mo,
    enabled: Boolean(canRead && mo.enabled && active === 'metrics'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const eo = replicaSetQueries.events(scope, name)
  const events = useQuery({
    ...eo,
    enabled: Boolean(canRead && eo.enabled && active === 'events'),
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
    resource: 'replicasets',
    kind: 'ReplicaSet',
    permission: 'platform.workloads.replica-sets',
    mutations: { remove: (client) => workloadMutations.remove('replicasets', client) },
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
        title={zh ? '无权查看 ReplicaSet' : 'No permission to view ReplicaSet'}
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
        <ReadFeedback canRead query={query} label="ReplicaSet" />
        {query.isLoading ? (
          <Spin />
        ) : !query.isError ? (
          <ManagementState
            compact
            kind="not-found"
            title={zh ? 'ReplicaSet 未找到' : 'ReplicaSet not found'}
          />
        ) : null}
      </>
    )
  const health = replicaSetHealth(detail, readableQueryData(configuration)),
    revision = replicaSetRevision(detail)
  return (
    <div className="soha-replicaset-detail">
      {actions.feedback}
      <WorkloadDetailShell
        ownerManagedTabs
        title="ReplicaSet"
        resource="replicasets"
        paramKey="replicaSetName"
        activeTabKey={active}
        onTabChange={changeTab}
        detailRefetchOnMount={false}
        hideMetadataOverview
        hideRelationshipsTab
        yamlLast
        header={
          <WorkloadDetailHeader
            kind="ReplicaSet"
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
                {revision && (
                  <span title="deployment.kubernetes.io/revision">
                    {zh ? '版本' : 'Revision'} {revision}
                  </span>
                )}
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
                  resourceLabel="ReplicaSet"
                  settings={refresh}
                  queries={[
                    query,
                    configuration,
                    ownerHistory,
                    ownerStatus,
                    events,
                    metrics,
                    observed.query,
                  ]}
                  onRefresh={() => {
                    void client.invalidateQueries({
                      queryKey: workloadKeys.detail('replicasets', scope, name),
                    })
                    if (ownerHistory.isEnabled) void ownerHistory.refetch()
                    if (ownerStatus.isEnabled) void ownerStatus.refetch()
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
              label={zh ? 'ReplicaSet 模板' : 'ReplicaSet template'}
            />
            <ReadFeedback
              canRead={ownerHistory.isEnabled}
              query={ownerHistory}
              label={zh ? '所属 Deployment 版本' : 'Owning Deployment revisions'}
            />
            <ReadFeedback
              canRead={ownerStatus.isEnabled}
              query={ownerStatus}
              label={zh ? '所属 Deployment 修订' : 'Owning Deployment revision'}
            />
            <ReplicaSetDetailOverview
              detail={detail}
              scope={scope}
              configuration={readableQueryData(configuration)}
              ownerHistory={canReadParent ? readableQueryData(ownerHistory) : undefined}
              parentStatus={canReadParent ? readableQueryData(ownerStatus) : undefined}
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
                  resourceLabel="ReplicaSet"
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
                    scopeDescription={`${zh ? '当前 ReplicaSet UID 所属 Pods 聚合' : 'Pods owned by the current ReplicaSet UID'} · ${detail.namespace} / ${name}`}
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
                    kind="ReplicaSet"
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
