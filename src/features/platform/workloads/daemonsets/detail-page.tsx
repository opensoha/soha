import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { formatAgeSeconds } from '@/utils/time'
import { readableQueryData } from '@/utils/query-feedback'
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
import { WorkloadPodsTable } from '../shared/workload-pods-table'
import { usePodObservations } from '../shared/use-pod-observations'
import { useWorkloadDetailActions } from '../shared/use-workload-detail-actions'
import { WorkloadRevisionHistory } from '../shared/workload-revision-history'
import { daemonSetQueries } from './queries'
import { daemonSetMutations } from './mutations'
import { DaemonSetDetailOverview } from './daemonset-detail-overview'
import {
  daemonSetState,
  daemonSetPodModel,
  daemonSetCurrentTemplate,
} from './daemonset-detail-model'
import './daemonset-detail.css'
const Metrics = lazy(async () => ({
  default: (await import('../shared/detail-metrics')).WorkloadDetailMetrics,
}))
const Resources = lazy(async () => ({
  default: (await import('./daemonset-detail-resources')).DaemonSetDetailResources,
}))
const Delivery = lazy(async () => ({
  default: (await import('../shared/workload-detail-delivery')).WorkloadDetailDelivery,
}))
const Revision = lazy(async () => ({
  default: (await import('./daemonset-detail-revision')).DaemonSetDetailRevision,
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
export function DaemonSetDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { daemonSetName: name = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const detailScope = useWorkloadDetailScope()
  const scope = useMemo(
    () => ({ clusterId: detailScope.clusterId, namespace: detailScope.namespace }),
    [detailScope.clusterId, detailScope.namespace],
  )
  const permissions = usePermissionSnapshot(),
    permissionData = readableQueryData(permissions)?.data
  const canRead = hasPermission(permissionData, 'platform.workloads.daemon-sets.view')
  const requested = params.get('tab') || 'overview'
  const normalized = ['relationships', 'resources'].includes(requested)
    ? 'related-resources'
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
  const detailOptions = daemonSetQueries.detail(scope, name)
  const detailQuery = useQuery({
    ...detailOptions,
    enabled: Boolean(canRead && detailOptions.enabled),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const raw = canRead ? readableQueryData(detailQuery) : undefined
  const detail = raw?.name === name && raw.namespace === scope.namespace ? raw : undefined
  const configurationOptions = daemonSetQueries.configuration(scope, name)
  const configurationQuery = useQuery({
    ...configurationOptions,
    enabled: Boolean(canRead && configurationOptions.enabled && detail),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const configuration = canRead ? readableQueryData(configurationQuery) : undefined
  const historyOptions = daemonSetQueries.rollouts(scope, name)
  const historyQuery = useQuery({
    ...historyOptions,
    enabled: Boolean(canRead && historyOptions.enabled),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const history = canRead ? readableQueryData(historyQuery) : undefined
  const metricsOptions = daemonSetQueries.metrics(scope, name, range)
  const metricsQuery = useQuery({
    ...metricsOptions,
    enabled: Boolean(canRead && metricsOptions.enabled && active === 'metrics'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const eventOptions = daemonSetQueries.events(scope, name)
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
    resource: 'daemonsets',
    kind: 'DaemonSet',
    permission: 'platform.workloads.daemon-sets',
    mutations: daemonSetMutations,
  })
  const client = useQueryClient(),
    [refreshToken, setRefreshToken] = useState(0)
  const identity = JSON.stringify([scope.clusterId, scope.namespace, name])
  const podModel = useMemo(
    () =>
      daemonSetPodModel(
        detail || { name, namespace: scope.namespace || '', createdAt: '' },
        history || [],
      ),
    [detail, history, name, scope.namespace],
  )
  const currentTemplate = daemonSetCurrentTemplate(history || [])
  const errorText = (error: unknown) => (error instanceof Error ? error.message : undefined)
  if (permissions.isLoading) return <Spin />
  if (permissions.isError && !permissionData)
    return <ReadFeedback query={permissions} label={zh ? '权限' : 'Permissions'} />
  if (!canRead)
    return (
      <ManagementState
        compact
        kind="no-permission"
        title={zh ? '无权查看 DaemonSet' : 'No permission to view DaemonSet'}
      />
    )
  if (!detailOptions.enabled)
    return (
      <ManagementState
        compact
        title={zh ? '请选择集群和命名空间后查看 DaemonSet' : 'Select a cluster and namespace'}
      />
    )
  if (!detail)
    return (
      <>
        <ReadFeedback canRead query={detailQuery} label="DaemonSet" />
        {detailQuery.isLoading && <Spin />}
        {!detailQuery.isPending && !detailQuery.isError && (
          <ManagementState
            compact
            kind="not-found"
            title={zh ? 'DaemonSet 未找到' : 'DaemonSet not found'}
          />
        )}
      </>
    )
  const state = daemonSetState(detail, configuration)
  return (
    <div className="soha-daemonset-detail">
      {actions.feedback}
      {active !== 'revisions' && (
        <ReadFeedback
          canRead
          query={historyQuery}
          label={zh ? 'DaemonSet 版本历史' : 'DaemonSet revision history'}
        />
      )}
      {
        <ReadFeedback
          canRead
          query={configurationQuery}
          label={zh ? 'DaemonSet 配置' : 'DaemonSet configuration'}
        />
      }
      <WorkloadDetailShell
        ownerManagedTabs
        title="DaemonSet"
        resource="daemonsets"
        paramKey="daemonSetName"
        activeTabKey={active}
        onTabChange={changeTab}
        detailRefetchOnMount={false}
        hideMetadataOverview
        hideRelationshipsTab
        yamlLast
        header={
          <WorkloadDetailHeader
            kind="DaemonSet"
            name={name}
            namespace={detail.namespace}
            backTo={`/workloads/daemonsets?clusterId=${encodeURIComponent(scope.clusterId || '')}&namespace=${encodeURIComponent(scope.namespace || '')}`}
            status={
              <StatusTag
                value={state}
                label={
                  state === 'no-targets'
                    ? zh
                      ? '无匹配目标'
                      : 'No matching targets'
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
                  {detail.readyNumber ?? '—'} / {detail.desiredNumber ?? '—'}{' '}
                  {zh ? '就绪' : 'ready'}
                </span>
                <span>
                  {zh ? '已更新' : 'Updated'} {detail.updatedNumber ?? '—'} /{' '}
                  {detail.desiredNumber ?? '—'}
                </span>
                <span>
                  {zh ? '模板版本' : 'Template revision'} {currentTemplate?.revision || '—'}
                </span>
                <span>{configuration?.strategy || detail.updateStrategy}</span>
                <span>
                  {zh ? '已创建' : 'Created'}{' '}
                  {detail.createdAt && Number.isFinite(Date.parse(detail.createdAt))
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
                  resourceLabel="DaemonSet"
                  settings={refresh}
                  beforeRefresh={actions.buttons}
                  queries={[
                    detailQuery,
                    configurationQuery,
                    historyQuery,
                    metricsQuery,
                    eventsQuery,
                    observed.query,
                  ]}
                  onRefresh={() => {
                    void client.invalidateQueries({
                      queryKey: workloadKeys.detail('daemonsets', scope, name),
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
          <DaemonSetDetailOverview
            detail={detail}
            configuration={configuration}
            currentTemplate={currentTemplate}
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
                    currentRevision={currentTemplate?.revision}
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
                    currentName={currentTemplate?.name}
                    sourceLabel="ControllerRevision"
                    countsKnown={(record) => Boolean(record.podRevisionHash)}
                    currentLabel={zh ? '当前模板' : 'Current template'}
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
                          currentName={currentTemplate?.name}
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
                  resourceLabel="DaemonSet"
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
                        ? `DaemonSet 所属 Pods 聚合 · ${detail.namespace} / ${detail.name}`
                        : `Aggregate of DaemonSet Pods · ${detail.namespace} / ${detail.name}`
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
                    kind="DaemonSet"
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
