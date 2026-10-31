import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementState, ManagementTableToolbar } from '@/components/management-list'
import { OverviewMetricCard, type OverviewMetricItem } from '@/components/overview-visuals'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { SecurityPosturePanel } from '@/features/platform/shared/resource-insights/security-posture-panel'
import { buildKubernetesEventResourcePath } from '@/features/platform/shared/resource-ref'
import { useKubernetesResourceStream } from '@/features/platform/shared/resource-stream'
import { ResourceStreamStatus } from '@/features/platform/shared/resource-stream-status'
import { buildWorkloadListPagePath } from '@/features/platform/workloads-model'
import '@/features/platform/workloads/styles.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatAgeSeconds } from '@/utils/time'
import {
  AppstoreOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  ClusterOutlined,
  ScheduleOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Link } from 'react-router-dom'
import { Segmented } from 'antd'
import { useState } from 'react'
import {
  useWorkloadTableDensity,
  WorkloadRefreshButton,
  WorkloadTableEmpty,
} from '../shared/list-controls'
import { workloadQueries } from '../shared/queries'
import type { WorkloadEvent } from '../shared/types'
import { workloadOverviewQueries } from './queries'

type WorkloadOverviewMetricItem = OverviewMetricItem & { path: string }

export function WorkloadsOverviewPage() {
  const { t, localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const scope = toScopeKey(clusterId, namespace)
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const workloadPermissions: Record<string, boolean> = {
    deployments: hasPermission(permissionSnapshot, 'platform.deployment.view'),
    pods: hasPermission(permissionSnapshot, 'platform.pods.view'),
    statefulsets: hasPermission(permissionSnapshot, 'platform.workloads.stateful-sets.view'),
    daemonsets: hasPermission(permissionSnapshot, 'platform.workloads.daemon-sets.view'),
    jobs: hasPermission(permissionSnapshot, 'platform.workloads.jobs.view'),
    cronjobs: hasPermission(permissionSnapshot, 'platform.workloads.cron-jobs.view'),
  }
  const deploymentsQuery = useQuery({
    ...workloadQueries.list<unknown>('deployments', scope),
    enabled: Boolean(clusterId) && workloadPermissions.deployments,
  })
  const visibleDeploymentsQueryData =
    Boolean(clusterId) && workloadPermissions.deployments
      ? readableQueryData(deploymentsQuery)
      : undefined
  const podsQuery = useQuery({
    ...workloadQueries.list<unknown>('pods', scope),
    enabled: Boolean(clusterId) && workloadPermissions.pods,
  })
  const visiblePodsQueryData =
    Boolean(clusterId) && workloadPermissions.pods ? readableQueryData(podsQuery) : undefined
  const statefulSetsQuery = useQuery({
    ...workloadQueries.list<unknown>('statefulsets', scope),
    enabled: Boolean(clusterId) && workloadPermissions.statefulsets,
  })
  const visibleStatefulSetsQueryData =
    Boolean(clusterId) && workloadPermissions.statefulsets
      ? readableQueryData(statefulSetsQuery)
      : undefined
  const daemonSetsQuery = useQuery({
    ...workloadQueries.list<unknown>('daemonsets', scope),
    enabled: Boolean(clusterId) && workloadPermissions.daemonsets,
  })
  const visibleDaemonSetsQueryData =
    Boolean(clusterId) && workloadPermissions.daemonsets
      ? readableQueryData(daemonSetsQuery)
      : undefined
  const jobsQuery = useQuery({
    ...workloadQueries.list<unknown>('jobs', scope),
    enabled: Boolean(clusterId) && workloadPermissions.jobs,
  })
  const visibleJobsQueryData =
    Boolean(clusterId) && workloadPermissions.jobs ? readableQueryData(jobsQuery) : undefined
  const cronJobsQuery = useQuery({
    ...workloadQueries.list<unknown>('cronjobs', scope),
    enabled: Boolean(clusterId) && workloadPermissions.cronjobs,
  })
  const visibleCronJobsQueryData =
    Boolean(clusterId) && workloadPermissions.cronjobs
      ? readableQueryData(cronJobsQuery)
      : undefined
  const eventsQuery = useQuery({
    ...workloadOverviewQueries.events(scope),
    enabled:
      Boolean(clusterId) && hasPermission(permissionSnapshot, 'platform.workloads.overview.view'),
  })
  const visibleEventsQueryData =
    Boolean(clusterId) && hasPermission(permissionSnapshot, 'platform.workloads.overview.view')
      ? readableQueryData(eventsQuery)
      : undefined
  const overviewReads = [
    deploymentsQuery,
    podsQuery,
    statefulSetsQuery,
    daemonSetsQuery,
    jobsQuery,
    cronJobsQuery,
    eventsQuery,
  ]
  const refreshOverview = () =>
    Promise.allSettled(
      overviewReads.filter((query) => query.isEnabled).map((query) => query.refetch()),
    )
  const refreshingOverview = overviewReads.some((query) => query.isEnabled && query.isFetching)
  const metricQueries: Record<string, typeof deploymentsQuery> = {
    deployments: deploymentsQuery,
    pods: podsQuery,
    statefulsets: statefulSetsQuery,
    daemonsets: daemonSetsQuery,
    jobs: jobsQuery,
    cronjobs: cronJobsQuery,
  }
  const [eventType, setEventType] = useState('all')
  const workloadStream = useKubernetesResourceStream({
    clusterId,
    namespace,
    enabled:
      Boolean(clusterId) && hasPermission(permissionSnapshot, 'platform.workloads.overview.view'),
    kinds: ['Deployment', 'StatefulSet', 'DaemonSet', 'Job', 'CronJob', 'Pod', 'Event'],
    onEvent: refreshOverview,
    onFallback: refreshOverview,
    onResyncRequired: refreshOverview,
  })
  const { densityButton, tableSize } = useWorkloadTableDensity(localeCode)

  if (!clusterId) {
    return (
      <div className="soha-page soha-overview-page soha-workloads-overview-page">
        <ManagementState
          compact
          kind="select-scope"
          title={t('common.pleaseSelectClusterShort', 'Select a cluster')}
        />
      </div>
    )
  }

  const stats = (
    [
      {
        key: 'deployments',
        label: 'Deployments',
        helper:
          localeCode === 'zh_CN' ? '无状态应用副本控制面' : 'Stateless application controllers',
        value: visibleDeploymentsQueryData?.length ?? '—',
        icon: <AppstoreOutlined />,
        tone: 'default',
        path: buildWorkloadListPagePath('deployments', namespace, clusterId),
      },
      {
        key: 'pods',
        label: 'Pods',
        helper: localeCode === 'zh_CN' ? '当前范围的 Pod 资源' : 'Pod resources in scope',
        value: visiblePodsQueryData?.length ?? '—',
        icon: <ClusterOutlined />,
        tone: 'success',
        path: buildWorkloadListPagePath('pods', namespace, clusterId),
      },
      {
        key: 'statefulsets',
        label: 'StatefulSets',
        helper: localeCode === 'zh_CN' ? '有状态服务控制面' : 'Stateful service controllers',
        value: visibleStatefulSetsQueryData?.length ?? '—',
        icon: <CheckCircleOutlined />,
        tone: 'default',
        path: buildWorkloadListPagePath('statefulsets', namespace, clusterId),
      },
      {
        key: 'daemonsets',
        label: 'DaemonSets',
        helper: localeCode === 'zh_CN' ? '节点级守护进程' : 'Node-level daemon workloads',
        value: visibleDaemonSetsQueryData?.length ?? '—',
        icon: <ClusterOutlined />,
        tone: 'default',
        path: buildWorkloadListPagePath('daemonsets', namespace, clusterId),
      },
      {
        key: 'jobs',
        label: 'Jobs',
        helper: localeCode === 'zh_CN' ? '一次性任务资源' : 'One-off workload runs',
        value: visibleJobsQueryData?.length ?? '—',
        icon: <ClockCircleOutlined />,
        tone: 'default',
        path: buildWorkloadListPagePath('jobs', namespace, clusterId),
      },
      {
        key: 'cronjobs',
        label: 'CronJobs',
        helper: localeCode === 'zh_CN' ? '周期调度任务' : 'Scheduled workload runs',
        value: visibleCronJobsQueryData?.length ?? '—',
        icon: <ScheduleOutlined />,
        tone: 'default',
        path: buildWorkloadListPagePath('cronjobs', namespace, clusterId),
      },
    ] satisfies WorkloadOverviewMetricItem[]
  )
    .filter((item) => workloadPermissions[item.key])
    .map((item) => {
      const query = metricQueries[item.key]
      return {
        ...item,
        helper: query.isError
          ? readableQueryData(query) === undefined
            ? localeCode === 'zh_CN'
              ? '读取失败'
              : 'Read failed'
            : localeCode === 'zh_CN'
              ? '未更新，显示上次结果'
              : 'Showing the previous result'
          : query.isLoading
            ? localeCode === 'zh_CN'
              ? '正在加载'
              : 'Loading'
            : query.isFetching
              ? localeCode === 'zh_CN'
                ? '正在刷新'
                : 'Refreshing'
              : item.helper,
      }
    })

  const eventColumns: TableColumnsType<WorkloadEvent> = [
    {
      title: t('common.namespace', 'Namespace'),
      dataIndex: 'namespace',
      render: (value: string) => value || '-',
    },
    {
      title: localeCode === 'zh_CN' ? '类型' : 'Type',
      dataIndex: 'type',
      render: (value: string) => <StatusTag value={value} />,
    },
    { title: localeCode === 'zh_CN' ? '原因' : 'Reason', dataIndex: 'reason' },
    {
      title: localeCode === 'zh_CN' ? '对象' : 'Object',
      dataIndex: 'involvedName',
      render: (_: string, record: WorkloadEvent) => {
        const label = `${record.involvedKind || '-'} / ${record.involvedName || '-'}`
        const path = buildKubernetesEventResourcePath(record, clusterId)
        return path ? (
          <Link className="soha-workload-overview-event-link" to={path}>
            {label}
          </Link>
        ) : (
          label
        )
      },
    },
    {
      title: localeCode === 'zh_CN' ? '消息' : 'Message',
      dataIndex: 'message',
      ellipsis: { showTitle: false },
      render: (value?: string) => <TableCellText value={value} />,
    },
    { title: localeCode === 'zh_CN' ? '次数' : 'Count', dataIndex: 'count' },
    {
      ...tableColumnPresets.datetime,
      title: localeCode === 'zh_CN' ? '时长' : 'Age',
      dataIndex: 'ageSeconds',
      render: (value: number) => formatAgeSeconds(value),
    },
  ]
  const events = visibleEventsQueryData ?? []
  const filteredEvents =
    eventType === 'warning'
      ? events.filter((event) => event.type?.toLowerCase() === 'warning')
      : events

  return (
    <div className="soha-page soha-overview-page soha-workloads-overview-page">
      <ReadFeedback
        canRead={deploymentsQuery.isEnabled}
        query={deploymentsQuery}
        label="Deployment"
      />
      <ReadFeedback canRead={podsQuery.isEnabled} query={podsQuery} label="Pods" />
      <ReadFeedback
        canRead={statefulSetsQuery.isEnabled}
        query={statefulSetsQuery}
        label="StatefulSet"
      />
      <ReadFeedback canRead={daemonSetsQuery.isEnabled} query={daemonSetsQuery} label="DaemonSet" />
      <ReadFeedback canRead={jobsQuery.isEnabled} query={jobsQuery} label="Job" />
      <ReadFeedback canRead={cronJobsQuery.isEnabled} query={cronJobsQuery} label="CronJob" />
      <ReadFeedback canRead={eventsQuery.isEnabled} query={eventsQuery} label="资源事件" />

      <div className="soha-workload-overview-toolbar">
        <span className="soha-kubernetes-resource-count">
          {localeCode === 'zh_CN' ? `${stats.length} 类工作负载` : `${stats.length} workload types`}
        </span>
        <ManagementTableToolbar>
          <ResourceStreamStatus
            status={workloadStream.status}
            refreshing={refreshingOverview}
            refreshFailed={overviewReads.some((query) => query.isEnabled && query.isError)}
            lastEventAt={workloadStream.lastEventAt}
            localeCode={localeCode}
          />
          <WorkloadRefreshButton
            label={localeCode === 'zh_CN' ? '刷新概览' : 'Refresh overview'}
            loading={refreshingOverview}
            disabled={refreshingOverview}
            onRefresh={() => void refreshOverview()}
          />
        </ManagementTableToolbar>
      </div>
      <div className="soha-overview-metric-grid soha-workload-overview-metric-grid">
        {stats.map((item) => (
          <Link
            key={item.key}
            aria-label={`${localeCode === 'zh_CN' ? '查看' : 'Open'} ${item.label}`}
            className="soha-workload-overview-metric-link"
            to={item.path}
          >
            <OverviewMetricCard
              label={item.label}
              value={item.value}
              helper={item.helper}
              icon={item.icon}
              tone={item.tone}
            />
          </Link>
        ))}
      </div>
      <SecurityPosturePanel scope={scope} />
      <KubernetesResourceTable
        resourceKey="workload-events"
        resourceLabel={localeCode === 'zh_CN' ? '当前返回的事件' : 'returned events'}
        allRows={events}
        filterKey={eventType}
        error={eventsQuery.isEnabled === false ? null : eventsQuery.error}
        hasReadData={eventsQuery.data !== undefined}
        onRetry={eventsQuery.isEnabled === false ? undefined : () => eventsQuery.refetch()}
        title={
          <span className="soha-kubernetes-resource-count" role="status" aria-live="polite">
            {visibleEventsQueryData === undefined
              ? eventsQuery.isError
                ? localeCode === 'zh_CN'
                  ? '事件未加载'
                  : 'Events unavailable'
                : localeCode === 'zh_CN'
                  ? '正在加载事件'
                  : 'Loading events'
              : localeCode === 'zh_CN'
                ? `事件 ${filteredEvents.length} / 当前返回 ${events.length} 条`
                : `${filteredEvents.length} events / ${events.length} returned`}
          </span>
        }
        className="soha-workload-overview-events soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={
          <ManagementTableToolbar>
            <Segmented
              aria-label={localeCode === 'zh_CN' ? '事件类型' : 'Event type'}
              size="small"
              value={eventType}
              onChange={(value) => setEventType(String(value))}
              options={[
                { value: 'all', label: localeCode === 'zh_CN' ? '全部事件' : 'All events' },
                { value: 'warning', label: localeCode === 'zh_CN' ? '警告事件' : 'Warnings' },
              ]}
            />
            {densityButton}
            <WorkloadRefreshButton
              disabled={!clusterId}
              label={localeCode === 'zh_CN' ? '刷新事件' : 'Refresh events'}
              loading={eventsQuery.isFetching}
              onRefresh={() => void eventsQuery.refetch()}
            />
          </ManagementTableToolbar>
        }
        columns={eventColumns}
        dataSource={filteredEvents}
        localSorting
        rowKey={(record) => `${record.namespace || ''}/${record.name}`}
        loading={eventsQuery.isLoading}
        empty={
          <WorkloadTableEmpty
            clusterId={clusterId}
            filteredCount={filteredEvents.length}
            localeCode={localeCode}
            resourceLabel={localeCode === 'zh_CN' ? '事件记录' : 'event records'}
            totalCount={events.length}
          />
        }
        tableSize={tableSize}
        scroll={{ x: 'max-content' }}
      />
    </div>
  )
}
