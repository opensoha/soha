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
import { useWorkloadDetailActions } from '../shared/use-workload-detail-actions'
import { workloadKeys } from '../shared/keys'
import { CronJobDetailOverview } from './cronjob-detail-overview'
import { CronJobJobsTable } from './cronjob-jobs-table'
import { retainedCronJobs } from './cronjob-detail-model'
import { cronJobQueries } from './queries'
import { cronJobMutations } from './mutations'
import './cronjob-detail.css'

const Metrics = lazy(async () => ({
  default: (await import('./cronjob-detail-metrics')).CronJobDetailMetrics,
}))
const Resources = lazy(async () => ({
  default: (await import('./cronjob-related-resources')).CronJobRelatedResources,
}))
const Delivery = lazy(async () => ({
  default: (await import('../shared/workload-detail-delivery')).WorkloadDetailDelivery,
}))
const tabs = ['overview', 'jobs', 'events', 'metrics', 'related-resources', 'delivery', 'yaml']

export function CronJobDetailPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const { cronJobName: name = '' } = useParams()
  const detailScope = useWorkloadDetailScope()
  const scope = useMemo(
    () => ({ clusterId: detailScope.clusterId, namespace: detailScope.namespace }),
    [detailScope.clusterId, detailScope.namespace],
  )
  const permissions = usePermissionSnapshot(),
    permissionData = readableQueryData(permissions)?.data
  const canRead = hasPermission(permissionData, 'platform.workloads.cron-jobs.view')
  const [params, setParams] = useSearchParams(),
    requested = params.get('tab') || 'overview'
  const normalized = ['relationships', 'resources'].includes(requested)
    ? 'related-resources'
    : requested
  const active = tabs.includes(normalized) ? normalized : 'overview'
  const requestedRange = Number(params.get('range') || 60),
    range = [15, 60, 360, 1440].includes(requestedRange) ? requestedRange : 60
  const changeTab = (tab: string) =>
    setParams((old) => {
      const next = new URLSearchParams(old)
      next.set('tab', tab)
      return next
    })
  const refresh = useWorkloadRefresh()
  const options = cronJobQueries.detail(scope, name)
  const query = useQuery({
    ...options,
    enabled: Boolean(canRead && options.enabled),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const raw = canRead ? readableQueryData(query) : undefined
  const detail = raw?.name === name && raw.namespace === scope.namespace ? raw : undefined
  const config = cronJobQueries.configuration(scope, name)
  const configuration = useQuery({
    ...config,
    enabled: Boolean(canRead && detail && config.enabled && active === 'overview'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const eo = cronJobQueries.events(scope, name)
  const events = useQuery({
    ...eo,
    enabled: Boolean(canRead && eo.enabled && active === 'events'),
    refetchInterval: refresh.interval,
    refetchIntervalInBackground: false,
  })
  const actions = useWorkloadDetailActions({
    detail,
    scope,
    resource: 'cronjobs',
    kind: 'CronJob',
    permission: 'platform.workloads.cron-jobs',
    mutations: { remove: cronJobMutations.remove, suspend: cronJobMutations.suspendDetail },
  })
  const client = useQueryClient(),
    [refreshToken, setRefreshToken] = useState(0)
  const identity = JSON.stringify([scope.clusterId, scope.namespace, name])
  if (permissions.isLoading) return <Spin />
  if (permissions.isError && !permissionData)
    return <ReadFeedback query={permissions} label={zh ? '权限' : 'Permissions'} />
  if (!canRead)
    return (
      <ManagementState
        compact
        kind="no-permission"
        title={zh ? '无权查看 CronJob' : 'No permission to view CronJob'}
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
        <ReadFeedback canRead query={query} label="CronJob" />
        {query.isLoading ? (
          <Spin />
        ) : !query.isError ? (
          <ManagementState
            compact
            kind="not-found"
            title={zh ? 'CronJob 未找到' : 'CronJob not found'}
          />
        ) : null}
      </>
    )
  const jobs = retainedCronJobs(detail)
  return (
    <div className="soha-cronjob-detail">
      {actions.feedback}
      <WorkloadDetailShell
        ownerManagedTabs
        title="CronJob"
        resource="cronjobs"
        paramKey="cronJobName"
        activeTabKey={active}
        onTabChange={changeTab}
        detailRefetchOnMount={false}
        hideMetadataOverview
        hideRelationshipsTab
        yamlLast
        header={
          <WorkloadDetailHeader
            kind="CronJob"
            name={name}
            namespace={detail.namespace}
            status={
              <StatusTag
                value={
                  detail.suspend ? 'suspended' : detail.suspend === false ? 'enabled' : 'unknown'
                }
                label={
                  detail.suspend
                    ? zh
                      ? '已暂停'
                      : 'Suspended'
                    : detail.suspend === false
                      ? zh
                        ? '调度启用'
                        : 'Scheduling enabled'
                      : zh
                        ? '未知'
                        : 'Unknown'
                }
              />
            }
            facts={
              <>
                <span>{detail.schedule}</span>
                <span>{detail.timeZone || (zh ? '控制器时区' : 'Controller time zone')}</span>
                <span>{detail.concurrencyPolicy}</span>
                <span>
                  {zh ? '活跃' : 'Active'} {detail.activeJobs ?? '—'}
                </span>
                <span>
                  {zh ? '已创建' : 'Created'}{' '}
                  {formatAgeSeconds(
                    detail.createdAt
                      ? Math.max(0, (Date.now() - Date.parse(detail.createdAt)) / 1000)
                      : detail.ageSeconds,
                  )}
                </span>
              </>
            }
            actions={
              <>
                <WorkloadRefresh
                  resourceLabel="CronJob"
                  settings={refresh}
                  queries={[query, configuration, events]}
                  onRefresh={() => {
                    void client.invalidateQueries({
                      queryKey: workloadKeys.detail('cronjobs', scope, name),
                    })
                    if (events.isEnabled) void events.refetch()
                    setRefreshToken((v) => v + 1)
                  }}
                />
                {actions.buttons}
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
              label={zh ? '调度配置' : 'Schedule configuration'}
            />
            <CronJobDetailOverview
              detail={detail}
              jobs={jobs}
              scope={scope}
              configuration={readableQueryData(configuration)}
              onTabChange={changeTab}
            />
          </>
        }
        extraTabPanes={[
          {
            key: 'jobs',
            label: 'Jobs',
            children:
              active === 'jobs' ? (
                <CronJobJobsTable key={identity} jobs={jobs} scope={scope} />
              ) : null,
          },
          {
            key: 'events',
            label: zh ? '事件' : 'Events',
            children:
              active === 'events' ? (
                <WorkloadDetailEvents
                  key={identity}
                  resourceLabel="CronJob"
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
                    jobs={jobs}
                    activeJobs={detail.activeJobs}
                    rangeMinutes={range}
                    observedAt={query.dataUpdatedAt || Date.now()}
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
                    jobs={jobs}
                    scope={scope}
                    refreshToken={refreshToken}
                    interval={refresh.interval}
                    onShowJobs={() => changeTab('jobs')}
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
                    kind="CronJob"
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
