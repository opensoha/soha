import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds } from '@/utils/time'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Spin } from 'antd'
import { lazy, Suspense, useMemo, useState } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { useWorkloadDetailScope } from '../shared/detail-scope'
import { WorkloadDetailShell } from '../shared/detail-shell'
import { WorkloadDetailHeader } from '../shared/workload-detail-header'
import { WorkloadRefresh, useWorkloadRefresh } from '../shared/workload-refresh'
import { WorkloadDetailEvents } from '../shared/detail-events'
import { WorkloadPodsTable } from '../shared/workload-pods-table'
import { usePodObservations } from '../shared/use-pod-observations'
import { useWorkloadDetailActions } from '../shared/use-workload-detail-actions'
import { workloadKeys } from '../shared/keys'
import { JobDetailOverview } from './job-detail-overview'
import {
  formatJobDuration,
  jobCompletionTarget,
  jobDuration,
  jobState,
  jobStateLabel,
} from './job-detail-model'
import { jobQueries } from './queries'
import { jobMutations } from './mutations'
import './job-detail.css'

const Metrics = lazy(async () => ({
  default: (await import('../shared/detail-metrics')).WorkloadDetailMetrics,
}))
const Resources = lazy(async () => ({
  default: (await import('../shared/controller-related-resources')).ControllerRelatedResources,
}))
const Delivery = lazy(async () => ({
  default: (await import('../shared/workload-detail-delivery')).WorkloadDetailDelivery,
}))
const Logs = lazy(async () => ({ default: (await import('./job-detail-logs')).JobDetailLogs }))
const tabs = [
  'overview',
  'pods',
  'logs',
  'events',
  'metrics',
  'related-resources',
  'delivery',
  'yaml',
]

export function JobDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { jobName: name = '' } = useParams()
  const detailScope = useWorkloadDetailScope()
  const scope = useMemo(
    () => ({ clusterId: detailScope.clusterId, namespace: detailScope.namespace }),
    [detailScope.clusterId, detailScope.namespace],
  )
  const permissions = usePermissionSnapshot(),
    permissionData = readableQueryData(permissions)?.data
  const canRead = hasPermission(permissionData, 'platform.workloads.jobs.view')
  const [params, setParams] = useSearchParams(),
    requested = params.get('tab') || 'overview'
  const normalized = ['relationships', 'resources'].includes(requested)
    ? 'related-resources'
    : requested
  const active = tabs.includes(normalized) ? normalized : 'overview'
  const rv = Number(params.get('range') || 60),
    range = [15, 60, 360, 1440].includes(rv) ? rv : 60
  const changeTab = (tab: string) =>
    setParams((previous) => {
      const next = new URLSearchParams(previous)
      next.set('tab', tab)
      return next
    })
  const refresh = useWorkloadRefresh()
  const options = jobQueries.detail(scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(canRead && options.enabled),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const raw = canRead ? readableQueryData(query) : undefined
  const detail = raw?.name === name && raw.namespace === scope.namespace ? raw : undefined
  const tpl = jobQueries.template(scope, name)
  const template = useQuery({
    ...tpl,
    enabled: Boolean(canRead && detail && tpl.enabled && active === 'overview'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const mo = jobQueries.metrics(scope, name, range)
  const metrics = useQuery({
    ...mo,
    enabled: Boolean(canRead && mo.enabled && active === 'metrics'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const eo = jobQueries.events(scope, name)
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
  const actions = useWorkloadDetailActions({
    detail,
    scope,
    resource: 'jobs',
    kind: 'Job',
    permission: 'platform.workloads.jobs',
    mutations: jobMutations,
  })
  const client = useQueryClient(),
    [refreshToken, setRefreshToken] = useState(0)
  const identity = JSON.stringify([scope.clusterId, scope.namespace, name])
  const errorText = (e: unknown) => (e instanceof Error ? e.message : undefined)
  if (permissions.isLoading) return <Spin />
  if (permissions.isError && !permissionData)
    return <ReadFeedback query={permissions} label={zh ? '权限' : 'Permissions'} />
  if (!canRead)
    return (
      <ManagementState
        compact
        kind="no-permission"
        title={zh ? '无权查看 Job' : 'No permission to view Job'}
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
        <ReadFeedback canRead query={query} label="Job" />
        {query.isLoading ? (
          <Spin />
        ) : !query.isError ? (
          <ManagementState compact kind="not-found" title={zh ? 'Job 未找到' : 'Job not found'} />
        ) : null}
      </>
    )
  const state = jobState(detail),
    duration = jobDuration(detail),
    target = jobCompletionTarget(detail)
  const sources = (detail.relatedResources || []).filter(
    (r) => r.kind === 'CronJob' && r.relation === 'owner' && r.namespace === detail.namespace,
  )
  return (
    <div className="soha-job-detail">
      {actions.feedback}
      <WorkloadDetailShell
        ownerManagedTabs
        title="Job"
        resource="jobs"
        paramKey="jobName"
        activeTabKey={active}
        onTabChange={changeTab}
        detailRefetchOnMount={false}
        hideMetadataOverview
        hideRelationshipsTab
        yamlLast
        header={
          <WorkloadDetailHeader
            kind="Job"
            name={name}
            namespace={detail.namespace}
            backTo={`/workloads/jobs?clusterId=${encodeURIComponent(scope.clusterId || '')}&namespace=${encodeURIComponent(scope.namespace || '')}`}
            status={
              <StatusTag
                value={
                  state === 'running'
                    ? 'progressing'
                    : state === 'completed'
                      ? 'completed'
                      : state === 'failed'
                        ? 'failed'
                        : state === 'failures-recorded'
                          ? 'warning'
                          : state
                }
                label={jobStateLabel(state, zh)}
              />
            }
            facts={
              <>
                <span>
                  {zh ? '成功' : 'Succeeded'} {detail.succeeded} / {target ?? '—'}
                </span>
                <span>
                  {zh ? '活跃' : 'Active'} {detail.active}
                </span>
                <span>
                  {zh ? '失败 Pod' : 'Failed Pods'} {detail.failed}
                </span>
                <span>{detail.completionMode}</span>
                <span>
                  {zh ? '已创建' : 'Created'}{' '}
                  {detail.createdAt
                    ? formatAgeSeconds(
                        Math.max(0, (Date.now() - Date.parse(detail.createdAt)) / 1000),
                      )
                    : '—'}
                </span>
                <span>
                  {zh ? '耗时' : 'Elapsed'}{' '}
                  {duration === undefined ? '—' : formatJobDuration(duration)}
                </span>
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  resourceLabel="Job"
                  settings={refresh}
                  queries={[query, template, metrics, events, observed.query]}
                  onRefresh={() => {
                    void client.invalidateQueries({
                      queryKey: workloadKeys.detail('jobs', scope, name),
                    })
                    if (metrics.isEnabled) void metrics.refetch()
                    if (events.isEnabled) void events.refetch()
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
          <>
            <ReadFeedback
              canRead={template.isEnabled}
              query={template}
              label={zh ? 'Job 模板' : 'Job template'}
            />
            <JobDetailOverview
              detail={detail}
              scope={scope}
              template={readableQueryData(template)}
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
            key: 'logs',
            label: zh ? '日志' : 'Logs',
            children:
              active === 'logs' ? (
                <Suspense fallback={<Spin />}>
                  <Logs key={identity} pods={detail.pods || []} scope={scope} />
                </Suspense>
              ) : null,
          },
          {
            key: 'events',
            label: zh ? '事件' : 'Events',
            children:
              active === 'events' ? (
                <WorkloadDetailEvents
                  key={identity}
                  resourceLabel="Job"
                  events={readableQueryData(events) || []}
                  observedAt={events.dataUpdatedAt || Date.now()}
                  loading={events.isLoading}
                  error={errorText(events.error)}
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
                    scopeDescription={`${zh ? '本次 Job 已保留 Pods 聚合' : 'Retained Pods for this Job'} · ${detail.namespace} / ${name} · ${zh ? '执行耗时' : 'Elapsed'} ${duration === undefined ? '—' : formatJobDuration(duration)} · ${zh ? '成功/失败 Pod' : 'Succeeded/failed Pods'} ${detail.succeeded}/${detail.failed}`}
                    data={readableQueryData(metrics)}
                    loading={metrics.isLoading}
                    errorMessage={errorText(metrics.error)}
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
                    kind="Job"
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
                    kind="Job"
                    scope={scope}
                    name={name}
                    sources={sources.map((r) => ({
                      kind: r.kind,
                      name: r.name,
                      namespace: detail.namespace,
                    }))}
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
