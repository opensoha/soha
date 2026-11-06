import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Spin } from 'antd'
import { lazy, Suspense, useCallback, useMemo, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useWorkloadDetailScope } from '../shared/detail-scope'
import { WorkloadDetailShell } from '../shared/detail-shell'
import { WorkloadDetailHeader } from '../shared/workload-detail-header'
import { WorkloadRefresh, useWorkloadRefresh } from '../shared/workload-refresh'
import { WorkloadDetailEvents } from '../shared/detail-events'
import { workloadKeys } from '../shared/keys'
import { WorkloadPodsTable } from '../shared/workload-pods-table'
import { usePodObservations } from '../shared/use-pod-observations'
import { useWorkloadDetailActions } from '../shared/use-workload-detail-actions'
import { WorkloadRevisionHistory } from '../shared/workload-revision-history'
import { statefulSetQueries } from './queries'
import { statefulSetMutations } from './mutations'
import { StatefulSetDetailOverview } from './statefulset-detail-overview'
import { statefulSetState } from './statefulset-detail-model'
import './statefulset-detail.css'
const Metrics = lazy(async () => ({
  default: (await import('../shared/detail-metrics')).WorkloadDetailMetrics,
}))
const Storage = lazy(async () => ({
  default: (await import('./statefulset-detail-storage')).StatefulSetDetailStorage,
}))
const Resources = lazy(async () => ({
  default: (await import('./statefulset-detail-resources')).StatefulSetDetailResources,
}))
const Delivery = lazy(async () => ({
  default: (await import('../shared/workload-detail-delivery')).WorkloadDetailDelivery,
}))
const Revision = lazy(async () => ({
  default: (await import('./statefulset-detail-revision')).StatefulSetDetailRevision,
}))
const tabs = [
  'overview',
  'pods',
  'revisions',
  'events',
  'metrics',
  'storage',
  'related-resources',
  'delivery',
  'yaml',
]
export function StatefulSetDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { statefulSetName: name = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const detailScope = useWorkloadDetailScope()
  const scope = useMemo(
    () => ({ clusterId: detailScope.clusterId, namespace: detailScope.namespace }),
    [detailScope.clusterId, detailScope.namespace],
  )
  const permissions = usePermissionSnapshot(),
    permissionData = readableQueryData(permissions)?.data
  const canRead = hasPermission(permissionData, 'platform.workloads.stateful-sets.view')
  const requested = params.get('tab') || 'overview'
  const normalized = ['relationships', 'resources'].includes(requested)
    ? 'related-resources'
    : requested === 'volumes'
      ? 'storage'
      : requested
  const active = tabs.includes(normalized) ? normalized : 'overview'
  const rangeValue = Number(params.get('range') || 60),
    range = [15, 60, 360, 1440].includes(rangeValue) ? rangeValue : 60
  const changeTab = (tab: string) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('tab', tab)
      return next
    })
  const refresh = useWorkloadRefresh()
  const detailOptions = statefulSetQueries.detail(scope, name)
  const detailQuery = useQuery({
    ...detailOptions,
    enabled: Boolean(canRead && detailOptions.enabled),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const raw = canRead ? readableQueryData(detailQuery) : undefined
  const detail = raw?.name === name && raw.namespace === scope.namespace ? raw : undefined
  const configurationOptions = statefulSetQueries.configuration(scope, name)
  const configurationQuery = useQuery({
    ...configurationOptions,
    enabled: Boolean(canRead && configurationOptions.enabled && detail),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const configuration = canRead ? readableQueryData(configurationQuery) : undefined
  const historyOptions = statefulSetQueries.rollouts(scope, name)
  const historyQuery = useQuery({
    ...historyOptions,
    enabled: Boolean(canRead && historyOptions.enabled && active === 'revisions'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const history = canRead ? readableQueryData(historyQuery) : undefined
  const metricsOptions = statefulSetQueries.metrics(scope, name, range)
  const metricsQuery = useQuery({
    ...metricsOptions,
    enabled: Boolean(canRead && metricsOptions.enabled && active === 'metrics'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const eventOptions = statefulSetQueries.events(scope, name)
  const eventsQuery = useQuery({
    ...eventOptions,
    enabled: Boolean(canRead && eventOptions.enabled && active === 'events'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const observed = usePodObservations(
    scope,
    detail?.pods || [],
    Boolean(detail?.pods?.length && active === 'pods'),
  )
  const actions = useWorkloadDetailActions({
    detail,
    scope,
    resource: 'statefulsets',
    kind: 'StatefulSet',
    permission: 'platform.workloads.stateful-sets',
    mutations: statefulSetMutations,
  })
  const client = useQueryClient(),
    [refreshToken, setRefreshToken] = useState(0)
  const identity = JSON.stringify([scope.clusterId, scope.namespace, name])
  const [storageState, setStorageState] = useState({
    identity: '',
    isFetching: false,
    isError: false,
    isEnabled: false,
  })
  const onStorageReadState = useCallback(
    (state: { isFetching: boolean; isError: boolean; isEnabled: boolean }) =>
      setStorageState({ identity, ...state }),
    [identity],
  )
  const podModel = useMemo(
    () => ({
      podOwners: new Map(
        (detail?.pods || [])
          .filter((p) => p.labels?.['controller-revision-hash'])
          .map((p) => [
            `${p.namespace}/${p.name}`,
            { record: { revision: p.labels!['controller-revision-hash'] } },
          ]),
      ),
    }),
    [detail?.pods],
  )
  const errorText = (error: unknown) => (error instanceof Error ? error.message : undefined)
  if (permissions.isLoading) return <Spin />
  if (permissions.isError && !permissionData)
    return <ReadFeedback query={permissions} label={zh ? '权限' : 'Permissions'} />
  if (!canRead)
    return (
      <ManagementState
        compact
        kind="no-permission"
        title={zh ? '无权查看 StatefulSet' : 'No permission to view StatefulSet'}
      />
    )
  if (!detailOptions.enabled)
    return (
      <ManagementState
        compact
        title={zh ? '请选择集群和命名空间后查看 StatefulSet' : 'Select a cluster and namespace'}
      />
    )
  if (!detail)
    return (
      <>
        <ReadFeedback canRead query={detailQuery} label="StatefulSet" />
        {detailQuery.isLoading && <Spin />}
        {!detailQuery.isPending && !detailQuery.isError && (
          <ManagementState
            compact
            kind="not-found"
            title={zh ? 'StatefulSet 未找到' : 'StatefulSet not found'}
          />
        )}
      </>
    )
  const state = statefulSetState(detail, configuration)
  return (
    <div className="soha-statefulset-detail">
      {actions.feedback}
      {
        <ReadFeedback
          canRead
          query={configurationQuery}
          label={zh ? 'StatefulSet 配置' : 'StatefulSet configuration'}
        />
      }
      <WorkloadDetailShell
        ownerManagedTabs
        title="StatefulSet"
        resource="statefulsets"
        paramKey="statefulSetName"
        activeTabKey={active}
        onTabChange={changeTab}
        detailRefetchOnMount={false}
        hideMetadataOverview
        hideRelationshipsTab
        yamlLast
        header={
          <WorkloadDetailHeader
            kind="StatefulSet"
            name={name}
            namespace={detail.namespace}
            backTo={`/workloads/statefulsets?clusterId=${encodeURIComponent(scope.clusterId || '')}&namespace=${encodeURIComponent(scope.namespace || '')}`}
            status={
              <StatusTag
                value={state}
                label={
                  state === 'scaled-to-zero'
                    ? zh
                      ? '已缩容至 0'
                      : 'Scaled to zero'
                    : state === 'progressing'
                      ? zh
                        ? '更新中'
                        : 'Updating'
                      : state === 'degraded'
                        ? zh
                          ? '未就绪'
                          : 'Not ready'
                        : state === 'unknown'
                          ? zh
                            ? '未知'
                            : 'Unknown'
                          : undefined
                }
              />
            }
            facts={
              <>
                <span>
                  {detail.readyReplicas ?? '—'} / {detail.desiredReplicas ?? '—'}{' '}
                  {zh ? '就绪' : 'ready'}
                </span>
                <span className="soha-controller-header-revision" title={detail.currentRevision}>
                  {zh ? '当前版本' : 'Current revision'}{' '}
                  <span>{detail.currentRevision || '—'}</span>
                </span>
                {detail.updateRevision !== detail.currentRevision && (
                  <span className="soha-controller-header-revision" title={detail.updateRevision}>
                    {zh ? '目标版本' : 'Target revision'}{' '}
                    <span>{detail.updateRevision || '—'}</span>
                  </span>
                )}
                <span>{detail.updateStrategy}</span>
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  resourceLabel="StatefulSet"
                  settings={refresh}
                  beforeRefresh={actions.buttons}
                  queries={[
                    detailQuery,
                    configurationQuery,
                    historyQuery,
                    metricsQuery,
                    eventsQuery,
                    observed.query,
                    ...(active === 'storage' && storageState.identity === identity
                      ? [storageState]
                      : []),
                  ]}
                  onRefresh={() => {
                    void client.invalidateQueries({
                      queryKey: workloadKeys.detail('statefulsets', scope, name),
                    })
                    if (observed.query.isEnabled) void observed.query.refetch()
                    setRefreshToken((v) => v + 1)
                  }}
                />
                {actions.deleteButton}
              </>
            }
          />
        }
        extraOverview={
          <StatefulSetDetailOverview
            detail={detail}
            configuration={configuration}
            scope={scope}
            onTabChange={changeTab}
          />
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
                  <WorkloadPodsTable
                    key={identity}
                    pods={observed.pods}
                    scope={scope}
                    model={podModel}
                    currentRevision={detail.currentRevision}
                    revisionWidth={200}
                    factColumns={[
                      {
                        title: zh ? '序号' : 'Ordinal',
                        key: 'ordinal',
                        width: 80,
                        render: (_, pod) => pod.labels?.['apps.kubernetes.io/pod-index'] ?? '—',
                      },
                    ]}
                  />
                </>
              ) : null,
          },
          {
            key: 'revisions',
            label: zh ? '版本历史' : 'Revision history',
            children:
              active === 'revisions' ? (
                <>
                  <ReadFeedback
                    canRead
                    query={historyQuery}
                    label={zh ? '版本历史' : 'Revision history'}
                  />
                  <WorkloadRevisionHistory
                    key={identity}
                    records={history || []}
                    currentName={detail.currentRevision}
                    updateName={detail.updateRevision}
                    sourceLabel="ControllerRevision"
                    loading={historyQuery.isLoading}
                    error={errorText(historyQuery.error)}
                    onRefresh={() => void historyQuery.refetch()}
                    renderRevision={(record) => (
                      <Suspense fallback={<Spin />}>
                        <Revision
                          key={record.name}
                          scope={scope}
                          name={name}
                          record={record}
                          records={history || []}
                          currentName={detail.currentRevision}
                        />
                      </Suspense>
                    )}
                  />
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
                  resourceLabel="StatefulSet"
                  events={readableQueryData(eventsQuery) || []}
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
                        ? `StatefulSet 所属 Pods 聚合 · ${detail.namespace} / ${detail.name}`
                        : `Aggregate of StatefulSet Pods · ${detail.namespace} / ${detail.name}`
                    }
                    data={readableQueryData(metricsQuery)}
                    loading={metricsQuery.isLoading}
                    errorMessage={errorText(metricsQuery.error)}
                    rangeMinutes={range}
                    onRangeChange={(value) =>
                      setParams(
                        (previous) => {
                          const next = new URLSearchParams(previous)
                          next.set('range', String(value))
                          return next
                        },
                        { replace: true },
                      )
                    }
                    onRefresh={() => void metricsQuery.refetch()}
                  />
                </Suspense>
              ) : null,
          },
          {
            key: 'storage',
            label: zh ? '存储与 PVC' : 'Storage and PVCs',
            children:
              active === 'storage' ? (
                <Suspense fallback={<Spin />}>
                  <Storage
                    key={identity}
                    scope={scope}
                    detail={detail}
                    configuration={configuration}
                    refreshToken={refreshToken}
                    interval={refresh.interval}
                    onReadStateChange={onStorageReadState}
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
                    detail={detail}
                    scope={scope}
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
                    kind="StatefulSet"
                    scope={scope}
                    name={name}
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
