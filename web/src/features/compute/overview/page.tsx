import { ContextFeedback } from '@/components/context-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import {
  AppstoreOutlined,
  ArrowRightOutlined,
  ClockCircleOutlined,
  ClusterOutlined,
  DesktopOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons'
import { lazy, Suspense, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Popover, Space, Typography } from 'antd'
import { Link, useNavigate } from 'react-router-dom'
import type {
  ComputeHealthStatus,
  ComputeResourceRef,
  ComputeSectionStatus,
  ComputeWarning,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useAIPageContext } from '@/features/copilot'
import {
  OverviewHeroCard,
  OverviewNodeMatrix,
  type OverviewMetricItem,
} from '@/components/overview-visuals'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { localeText, useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { canAccessRoute, getRouteMeta } from '@/routes/meta'
import type { PermissionSnapshot } from '@/types'

import { computeQueries } from '../queries'
import { ProviderInstancesPanel } from './provider-instances-panel'
import type { SummaryChartProps } from './summary-chart'
import '../compute.css'

const { Text } = Typography
const SummaryChart = lazy(() => import('./summary-chart'))

type NavigableMetricItem = OverviewMetricItem & { path: string }

function OverviewLink({
  snapshot,
  to,
  className,
  children,
  ...label
}: {
  snapshot?: PermissionSnapshot
  to: string
  className: string
  children: ReactNode
  'aria-label': string
}) {
  if (!canAccessRoute(getRouteMeta(to.split('?')[0]), snapshot)) {
    return <div className={`${className} is-readonly`}>{children}</div>
  }
  return (
    <Link to={to} className={className} {...label}>
      {children}
    </Link>
  )
}

function readableSummary<T>(
  section: { status: ComputeSectionStatus; summary?: T; warnings?: ComputeWarning[] } | undefined,
  field?: string,
): T | undefined {
  if (!section || section.status === 'unavailable') return undefined
  if (
    field &&
    section.warnings?.some(
      ({ code }) => code === `${field}_redacted` || code === `${field}_read_failed`,
    )
  )
    return undefined
  return section.summary
}

function providerDomainLabel(domain: string, localeCode: 'zh_CN' | 'en_US') {
  return domain === 'container_runtime'
    ? localeText(localeCode, '容器运行时', 'Container runtime')
    : localeText(localeCode, '虚拟化', 'Virtualization')
}

const ATTENTION_LABELS: Record<string, [string, string]> = {
  runtime_host_unavailable: ['主机状态异常', 'Host status is unhealthy'],
  virtualization_connection_unavailable: [
    '虚拟化连接不可用',
    'Virtualization connection unavailable',
  ],
}

function attentionLabel(code: string, summary: string, localeCode: 'zh_CN' | 'en_US') {
  return ATTENTION_LABELS[code]?.[localeCode === 'zh_CN' ? 0 : 1] ?? summary
}

function attentionTarget(code: string, resource?: ComputeResourceRef) {
  if (resource?.kind === 'runtime_host') {
    return `/compute/runtimes/hosts/${encodeURIComponent(resource.id)}`
  }
  if (resource?.kind === 'vm') {
    return `/compute/virtualization/vms/${encodeURIComponent(resource.id)}`
  }
  if (resource?.kind === 'project') {
    return `/compute/runtimes/projects/${encodeURIComponent(resource.id)}`
  }
  return code === 'virtualization_connection_unavailable'
    ? '/compute/virtualization/clusters'
    : '/compute/runtimes/hosts'
}

function resourceKindLabel(kind: string, localeCode: 'zh_CN' | 'en_US') {
  if (kind === 'runtime_host') return localeText(localeCode, '运行时主机', 'Runtime host')
  if (kind === 'connection')
    return localeText(localeCode, '虚拟化连接', 'Virtualization connection')
  if (kind === 'vm') return localeText(localeCode, '虚拟机', 'Virtual machine')
  return localeText(localeCode, '计算资源', 'Compute resource')
}

function providerHealthSummary(status: ComputeHealthStatus, localeCode: 'zh_CN' | 'en_US') {
  if (status === 'healthy') return localeText(localeCode, '汇总状态健康', 'Summary reports healthy')
  if (status === 'degraded') {
    return localeText(localeCode, '服务降级，请检查连接状态', 'Degraded; check connection status')
  }
  if (status === 'unavailable') {
    return localeText(
      localeCode,
      '状态不可用，请查看读取说明或接入状态',
      'Status unavailable; review data availability or access status',
    )
  }
  if (status === 'pending') {
    return localeText(localeCode, '等待状态更新', 'Waiting for a status update')
  }
  return localeText(localeCode, '尚无可用状态', 'Status not available yet')
}

export function ComputeOverviewPage() {
  const { localeCode } = useI18n()
  const [dataNotesOpen, setDataNotesOpen] = useState(false)
  const navigate = useNavigate()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewVirtualization = [
    'virtualization.overview.view',
    'virtualization.vms.view',
    'virtualization.clusters.view',
    'virtualization.images.view',
    'virtualization.storage.view',
    'virtualization.flavors.view',
    'virtualization.operations.view',
    'virtualization.sync.view',
  ].some((permission) => hasPermission(snapshot, permission))
  const canViewDocker = [
    'docker.overview.view',
    'docker.hosts.view',
    'docker.projects.view',
    'docker.services.view',
    'docker.ports.view',
    'docker.templates.view',
  ].some((permission) => hasPermission(snapshot, permission))
  const canViewTasks =
    hasPermission(snapshot, 'virtualization.operations.view') ||
    hasPermission(snapshot, 'virtualization.sync.view') ||
    hasPermission(snapshot, 'docker.operations.view')
  const canTestProvider = hasPermission(snapshot, 'virtualization.clusters.test')
  const canDiscoverProvider = hasPermission(snapshot, 'virtualization.sync.sync')
  const overviewQuery = useQuery(
    computeQueries.overview(canViewVirtualization || canViewDocker || canViewTasks),
  )
  const overview = readableQueryData(overviewQuery)?.data
  const virtualization = overview?.virtualization
  const agents = overview?.agents
  const runtimes = overview?.runtimes
  const workloads = overview?.runtimeWorkloads
  const tasks = overview?.tasks

  const vmSummary = readableSummary(virtualization, 'virtualization_vms')
  const connectionSummary = readableSummary(virtualization, 'virtualization_connections')
  const agentSummary = readableSummary(agents)
  const runtimeSummary = readableSummary(runtimes)
  const serviceSummary = readableSummary(workloads, 'runtime_services')
  const projectSummary = readableSummary(workloads, 'runtime_projects')
  const taskSummary = readableSummary(tasks)
  const warnings = [
    ...new Map(
      [
        ...(overview?.warnings ?? []),
        ...[virtualization, agents, runtimes, workloads, tasks].flatMap(
          (section) => section?.warnings ?? [],
        ),
      ].map((warning) => [warning.code, warning]),
    ).values(),
  ]
  const attention = [...(overview?.attention ?? [])].sort(
    (a, b) =>
      ({ critical: 0, warning: 1, info: 2 })[a.severity] -
      { critical: 0, warning: 1, info: 2 }[b.severity],
  )

  useAIPageContext({
    sourceWorkbench: 'compute',
    sourceTitle: localeText(localeCode, '计算资源工作台', 'Compute Resources'),
    entityKind: 'compute.overview',
    entityName: localeText(localeCode, '计算资源总览', 'Compute overview'),
    pinnedData: overview
      ? {
          partial: overview.partial,
          attentionCount: overview.attention.length,
          resourceRefs: overview.attention
            .flatMap((item) => item.resources ?? [])
            .slice(0, 20)
            .map(({ domain, kind, id, displayName }) => ({ domain, kind, id, displayName })),
          toolRefs: [
            'compute.overview.read',
            'compute.resources.read',
            'compute.resource_relations.list',
            'compute.tasks.list',
          ],
        }
      : undefined,
  })

  const overviewStats = [
    {
      key: 'virtual-machines',
      className: 'soha-compute-metric-vms',
      label: localeText(localeCode, '虚拟机', 'Virtual machines'),
      value: vmSummary?.vmsTotal ?? '—',
      helper: vmSummary ? (
        <>
          {localeText(
            localeCode,
            `${vmSummary.vmsRunning ?? '—'} 台运行中 / ${vmSummary.vmsStopped ?? '—'} 台已停止 / ${vmSummary.vmsError ?? '—'} 台异常`,
            `${vmSummary.vmsRunning ?? '—'} running / ${vmSummary.vmsStopped ?? '—'} stopped / ${vmSummary.vmsError ?? '—'} unhealthy`,
          )}
        </>
      ) : (
        localeText(localeCode, '未取得虚拟机统计', 'Virtual machine counts unavailable')
      ),
      visual: {
        total: vmSummary?.vmsTotal,
        items: [
          {
            label: localeText(localeCode, '运行中', 'Running'),
            value: vmSummary?.vmsRunning,
            tone: 'success',
          },
          {
            label: localeText(localeCode, '已停止', 'Stopped'),
            value: vmSummary?.vmsStopped,
            tone: 'neutral',
          },
          {
            label: localeText(localeCode, '异常', 'Unhealthy'),
            value: vmSummary?.vmsError,
            tone: 'danger',
          },
        ],
      },
      icon: <DesktopOutlined />,
      path: '/compute/virtualization/vms',
    },
    {
      key: 'runtime-hosts',
      className: 'soha-compute-metric-hosts',
      label: localeText(localeCode, '运行时主机', 'Runtime hosts'),
      value: runtimeSummary?.total ?? '—',
      helper: runtimeSummary ? (
        <>
          {localeText(
            localeCode,
            `${runtimeSummary.available ?? '—'} 台可用 / ${runtimeSummary.error ?? '—'} 台异常 / ${runtimeSummary.waitingAgent ?? '—'} 台等待 Agent`,
            `${runtimeSummary.available ?? '—'} available / ${runtimeSummary.error ?? '—'} unhealthy / ${runtimeSummary.waitingAgent ?? '—'} awaiting agent`,
          )}
        </>
      ) : (
        localeText(localeCode, '未取得主机统计', 'Host counts unavailable')
      ),
      visual: {
        total: runtimeSummary?.total,
        items: [
          {
            label: localeText(localeCode, '可用', 'Available'),
            value: runtimeSummary?.available,
            tone: 'success',
          },
          {
            label: localeText(localeCode, '异常', 'Unhealthy'),
            value: runtimeSummary?.error,
            tone: 'danger',
          },
          {
            label: localeText(localeCode, '等待 Agent', 'Awaiting agent'),
            value: runtimeSummary?.waitingAgent,
            tone: 'neutral',
          },
        ],
      },
      icon: <ClusterOutlined />,
      path: '/compute/runtimes/hosts',
    },
    {
      key: 'services',
      className: 'soha-compute-metric-services',
      label: localeText(localeCode, '运行时服务', 'Runtime services'),
      value: serviceSummary?.services ?? '—',
      helper: serviceSummary
        ? localeText(
            localeCode,
            `${projectSummary?.projects ?? '—'} 个项目`,
            `${projectSummary?.projects ?? '—'} projects`,
          )
        : localeText(localeCode, '未取得服务统计', 'Service counts unavailable'),
      visual: {
        kind: 'bars',
        items: [
          {
            label: localeText(localeCode, '项目', 'Projects'),
            value: projectSummary?.projects,
            tone: 'neutral',
          },
          {
            label: localeText(localeCode, '服务', 'Services'),
            value: serviceSummary?.services,
            tone: 'violet',
          },
        ],
      },
      icon: <AppstoreOutlined />,
      path: '/compute/runtimes/projects',
    },
    {
      key: 'active-tasks',
      className: 'soha-compute-metric-tasks',
      label: localeText(localeCode, '活跃任务', 'Active tasks'),
      value:
        taskSummary?.queued !== undefined && taskSummary?.running !== undefined
          ? taskSummary.queued + taskSummary.running
          : '—',
      helper: taskSummary ? (
        <>
          {localeText(
            localeCode,
            `${taskSummary.queued ?? '—'} 个排队 / ${taskSummary.running ?? '—'} 个执行中`,
            `${taskSummary.queued ?? '—'} queued / ${taskSummary.running ?? '—'} running`,
          )}
        </>
      ) : (
        localeText(localeCode, '未取得任务统计', 'Task counts unavailable')
      ),
      visual: {
        total:
          taskSummary?.queued !== undefined && taskSummary?.running !== undefined
            ? taskSummary.queued + taskSummary.running
            : undefined,
        items: [
          {
            label: localeText(localeCode, '排队', 'Queued'),
            value: taskSummary?.queued,
            tone: 'neutral',
          },
          {
            label: localeText(localeCode, '执行中', 'Running'),
            value: taskSummary?.running,
            tone: 'active',
          },
        ],
      },
      icon: <ClockCircleOutlined />,
      path: '/compute/tasks/operations',
    },
  ] satisfies (NavigableMetricItem & { className: string; visual: SummaryChartProps })[]
  const visibleOverviewStats = overviewStats.filter(({ key }) => {
    if (key === 'virtual-machines') return canViewVirtualization
    if (key === 'active-tasks') return canViewTasks
    return canViewDocker
  })

  const lowerPanels = (
    <>
      <div className="soha-compute-priority-grid">
        <Card
          classNames={{ header: 'soha-compute-panel-header', body: 'soha-compute-panel-body' }}
          className="soha-overview-runtime-card soha-compute-attention-panel"
          title={
            <h2 className="soha-compute-section-heading">
              {localeText(localeCode, '需要关注', 'Needs attention')}
            </h2>
          }
          extra={
            <Space size={4}>
              <Popover
                trigger="click"
                placement="bottomRight"
                open={dataNotesOpen}
                onOpenChange={setDataNotesOpen}
                title={localeText(localeCode, '数据说明', 'About this data')}
                content={
                  <div className="soha-compute-data-notes">
                    {overview?.generatedAt ? (
                      <p>
                        {localeText(localeCode, '汇总于', 'Aggregated at')}{' '}
                        {formatDateTime(overview.generatedAt)}
                      </p>
                    ) : null}
                    <p>
                      {localeText(
                        localeCode,
                        '仅统计当前权限下已读取的记录，每类资源或任务最多读取 1,000 条。汇总时间不代表资源的最新观测时间，读取成功也不代表资源健康。',
                        'Counts cover records read within your permissions, up to 1,000 per resource or task source. Aggregation time is not the latest observation of each resource. A successful read does not indicate resource health.',
                      )}
                    </p>
                    <p>
                      {localeText(
                        localeCode,
                        '运行时服务按服务记录统计；活跃任务为排队与执行中之和；失败包含超时。不可读取或不可见的统计显示为 —。提供方健康汇总已有连接或主机状态，刷新总览不会发起连接健康检查。',
                        'Runtime services count service records. Active tasks combine queued and running tasks; failures include timeouts. Unavailable or hidden counts display —. Provider health summarizes existing connection or host states; refreshing this overview does not run connection health checks.',
                      )}
                    </p>
                    {overview?.providerHealth.length ? (
                      <ul>
                        {overview.providerHealth.map((provider) => (
                          <li key={`${provider.domain}:${provider.providerKey}`}>
                            {provider.providerKey} ·{' '}
                            {providerHealthSummary(provider.status, localeCode)}
                            {provider.checkedAt ? (
                              <>
                                {' '}
                                · {localeText(
                                  localeCode,
                                  '状态汇总于',
                                  'Status aggregated at',
                                )}{' '}
                                {formatDateTime(provider.checkedAt)}
                              </>
                            ) : null}
                          </li>
                        ))}
                      </ul>
                    ) : null}
                    {warnings.length ? (
                      <ul>
                        {warnings.map((warning) => (
                          <li key={warning.code}>{warning.message || warning.code}</li>
                        ))}
                      </ul>
                    ) : null}
                  </div>
                }
              >
                <Button
                  type="text"
                  icon={<QuestionCircleOutlined />}
                  aria-label={localeText(localeCode, '查看数据说明', 'About this data')}
                  aria-expanded={dataNotesOpen}
                  onKeyDown={(event) => {
                    if (event.key === 'Escape') setDataNotesOpen(false)
                  }}
                />
              </Popover>
              <ManagementRefreshButton
                aria-label={localeText(localeCode, '刷新计算资源总览', 'Refresh compute overview')}
                tooltip={localeText(localeCode, '刷新总览', 'Refresh overview')}
                loading={overviewQuery.isFetching}
                onClick={() => void overviewQuery.refetch()}
              />
            </Space>
          }
        >
          {overview?.partial ? (
            <ContextFeedback
              showIcon
              type="warning"
              title={localeText(
                localeCode,
                '部分资源暂不可用，其余已授权数据仍可查看',
                'Some resources are unavailable. Other authorized data remains visible.',
              )}
            />
          ) : null}

          {overview?.freshness?.status === 'unknown' || overview?.freshness?.status === 'stale' ? (
            <p className="soha-compute-section-note">
              {overview.freshness.status === 'stale'
                ? localeText(
                    localeCode,
                    '数据可能已过期，请刷新重试',
                    'Data may be stale; refresh to retry',
                  )
                : localeText(localeCode, '数据时效未知', 'Freshness unknown')}
            </p>
          ) : null}
          {overviewQuery.isLoading ? (
            <ManagementState bordered={false} compact kind="loading" />
          ) : attention.length ? (
            <div className="soha-overview-attention-list">
              {attention.map((item, index) => (
                <OverviewLink
                  snapshot={snapshot}
                  aria-label={localeText(
                    localeCode,
                    `查看${item.resources?.[0]?.displayName || attentionLabel(item.code, item.summary, localeCode)}`,
                    `View ${item.resources?.[0]?.displayName || attentionLabel(item.code, item.summary, localeCode)}`,
                  )}
                  className={`soha-overview-attention-row soha-compute-attention-row soha-compute-overview-link-row is-${item.severity}`}
                  key={`${item.code}:${item.resources?.map((resource) => resource.id).join(',') || index}`}
                  to={attentionTarget(
                    item.code,
                    item.resources?.length === 1 ? item.resources[0] : undefined,
                  )}
                >
                  <div className="soha-overview-attention-main">
                    <Space size={6} wrap>
                      <Text strong>
                        {item.resources?.[0]?.displayName ||
                          attentionLabel(item.code, item.summary, localeCode)}
                      </Text>
                    </Space>
                    <div className="soha-overview-inline-caption soha-compute-attention-reason">
                      <StatusTag value={item.severity} />
                      <span>
                        {item.resources?.length
                          ? attentionLabel(item.code, item.summary, localeCode)
                          : localeText(
                              localeCode,
                              '查看受影响资源并处理',
                              'Review and resolve the affected resources',
                            )}
                      </span>
                      {item.resources?.[0] ? (
                        <span className="soha-compute-resource-kind">
                          {resourceKindLabel(item.resources[0].kind, localeCode)}
                        </span>
                      ) : null}
                    </div>
                  </div>
                  <div className="soha-overview-attention-meta">
                    {item.resources && item.resources.length > 1 ? (
                      <span>
                        {localeText(
                          localeCode,
                          `共 ${item.resources.length} 个资源`,
                          `${item.resources.length} resources`,
                        )}
                      </span>
                    ) : null}
                    <span className="soha-compute-overview-row-action">
                      {localeText(localeCode, '查看详情', 'View details')}
                      <ArrowRightOutlined />
                    </span>
                  </div>
                </OverviewLink>
              ))}
            </div>
          ) : (
            <ManagementState
              bordered={false}
              compact
              kind="empty"
              title={
                overview
                  ? localeText(
                      localeCode,
                      '已读取范围内暂无待处置风险',
                      'No risks in the available records',
                    )
                  : localeText(localeCode, '尚未取得风险信息', 'Risk data unavailable')
              }
            />
          )}
          {canViewTasks ? (
            <>
              <div className="soha-compute-task-footer">
                <Button
                  type={(taskSummary?.failed ?? 0) > 0 ? 'primary' : 'default'}
                  size="small"
                  icon={<ArrowRightOutlined />}
                  iconPlacement="end"
                  onClick={() =>
                    navigate(
                      (taskSummary?.failed ?? 0) > 0
                        ? '/compute/tasks/operations?status=failed'
                        : '/compute/tasks/operations',
                    )
                  }
                >
                  {(taskSummary?.failed ?? 0) > 0
                    ? localeText(localeCode, '查看失败任务', 'View failed tasks')
                    : localeText(localeCode, '查看任务', 'View tasks')}
                </Button>
                {(taskSummary?.failed ?? 0) > 0 ? (
                  <Space wrap>
                    <Link to="/compute/tasks/operations?status=timeout">
                      {localeText(localeCode, '查看超时任务', 'View timed-out tasks')}
                    </Link>
                    <Link to="/compute/tasks/operations">
                      {localeText(localeCode, '所有任务', 'All tasks')}
                    </Link>
                  </Space>
                ) : null}
              </div>
              {tasks?.status === 'degraded' ? (
                <p className="soha-compute-section-note">
                  {localeText(
                    localeCode,
                    '部分任务来源读取失败，当前数字仅含已读取记录。',
                    'Some task sources could not be read; counts cover available records only.',
                  )}
                </p>
              ) : null}
            </>
          ) : null}
        </Card>

        <Card
          classNames={{ header: 'soha-compute-panel-header', body: 'soha-compute-panel-body' }}
          className="soha-overview-runtime-card soha-compute-provider-health"
          title={
            <h2 className="soha-compute-section-heading">
              {localeText(localeCode, '提供方概况', 'Provider overview')}
            </h2>
          }
        >
          {overviewQuery.isLoading ? (
            <ManagementState bordered={false} compact kind="loading" />
          ) : overview?.providerHealth.length ? (
            <div className="soha-overview-attention-list">
              {overview.providerHealth.map((provider) => (
                <OverviewLink
                  snapshot={snapshot}
                  aria-label={localeText(
                    localeCode,
                    `查看 ${provider.providerKey} 提供方`,
                    `View ${provider.providerKey} provider`,
                  )}
                  className="soha-overview-attention-row soha-compute-provider-summary-row soha-compute-overview-link-row"
                  key={`${provider.domain}:${provider.providerKey}`}
                  to={
                    provider.domain === 'container_runtime'
                      ? '/compute/runtimes/hosts'
                      : '/compute/virtualization/clusters'
                  }
                >
                  <div className="soha-overview-attention-main">
                    <Space size={6} wrap>
                      <Text strong>{provider.providerKey}</Text>
                      <StatusTag value={provider.status} />
                    </Space>
                    <div className="soha-overview-inline-caption">
                      {providerDomainLabel(provider.domain, localeCode)}
                    </div>
                  </div>
                  <div className="soha-overview-attention-meta">
                    <span className="soha-compute-overview-row-action">
                      {provider.domain === 'container_runtime'
                        ? localeText(localeCode, '查看主机', 'View hosts')
                        : localeText(localeCode, '查看连接', 'View connections')}
                      <ArrowRightOutlined />
                    </span>
                  </div>
                </OverviewLink>
              ))}
            </div>
          ) : (
            <ManagementState
              bordered={false}
              compact
              kind="empty"
              title={localeText(localeCode, '暂无提供方状态', 'No provider status')}
            />
          )}
        </Card>
      </div>

      <div className="soha-compute-infrastructure-grid">
        <ProviderInstancesPanel
          canViewTask={
            hasPermission(snapshot, 'virtualization.operations.view') ||
            hasPermission(snapshot, 'virtualization.sync.view')
          }
          canDiscover={canDiscoverProvider}
          canTest={canTestProvider}
          enabled={canViewVirtualization || canViewDocker}
          localeCode={localeCode}
        />
      </div>
    </>
  )

  return (
    <div className="soha-page soha-overview-page soha-compute-page soha-compute-overview-page">
      <ReadFeedback
        query={overviewQuery}
        label={localeText(localeCode, '计算资源总览', 'Compute overview')}
      />
      <h1 className="soha-compute-page-heading">
        {localeText(localeCode, '计算资源总览', 'Compute overview')}
      </h1>
      <section
        className="soha-compute-snapshot"
        aria-label={localeText(localeCode, '资源统计', 'Resource counts')}
      >
        {visibleOverviewStats.length === 4 ? (
          <OverviewHeroCard
            variant="compute"
            heroMetricClassName={`soha-overview-metric-card ${visibleOverviewStats[0].className}`}
            eyebrowIcon={visibleOverviewStats[0].icon}
            eyebrowLabel={visibleOverviewStats[0].label}
            value={visibleOverviewStats[0].value}
            valueSuffix={localeText(localeCode, '台受管实例', 'managed VMs')}
            caption={!vmSummary ? visibleOverviewStats[0].helper : undefined}
            detailDrawerTitle={localeText(localeCode, '计算资源与提供方明细', 'Compute & Provider Details')}
            graphicDeck={[
              {
                key: 'compute-orbit-rings',
                kind: 'rings',
                title: localeText(localeCode, '异构算力在线光谱环', 'Compute Readiness Orbit'),
                subtitle: localeText(localeCode, '虚拟机与容器运行时', 'VM & Container Runtime'),
                actionLabel: localeText(localeCode, '虚拟机', 'VMs'),
                actionTo: canViewVirtualization ? '/compute/virtualization/vms' : undefined,
                centerValue:
                  vmSummary?.vmsTotal && vmSummary.vmsTotal > 0
                    ? `${Math.round(((vmSummary.vmsRunning ?? 0) / vmSummary.vmsTotal) * 100)}%`
                    : '—',
                centerLabel: localeText(localeCode, '在线率', 'Online'),
                rings: [
                  {
                    key: 'vm-running',
                    label: localeText(localeCode, '运行中虚拟机', 'Running VMs'),
                    value: vmSummary?.vmsRunning ?? '—',
                    ratio:
                      vmSummary?.vmsTotal && vmSummary.vmsTotal > 0
                        ? (vmSummary.vmsRunning ?? 0) / vmSummary.vmsTotal
                        : 0,
                    tone: 'success',
                    to: canViewVirtualization ? '/compute/virtualization/vms' : undefined,
                  },
                  {
                    key: 'hosts-ready',
                    label: localeText(localeCode, '可用运行时主机', 'Ready Hosts'),
                    value: runtimeSummary
                      ? `${runtimeSummary.available ?? 0}/${runtimeSummary.total ?? 0}`
                      : '—',
                    ratio:
                      runtimeSummary?.total && runtimeSummary.total > 0
                        ? (runtimeSummary.available ?? 0) / runtimeSummary.total
                        : 0,
                    tone: 'primary',
                    to: canViewDocker ? '/compute/runtimes/hosts' : undefined,
                  },
                  {
                    key: 'agents-online',
                    label: localeText(localeCode, '在线 Agent 通道', 'Online Agents'),
                    value: agentSummary
                      ? `${agentSummary.online ?? 0}/${agentSummary.total ?? 0}`
                      : '—',
                    ratio:
                      agentSummary?.total && agentSummary.total > 0
                        ? (agentSummary.online ?? 0) / agentSummary.total
                        : 0,
                    tone: 'primary',
                  },
                  {
                    key: 'vm-error',
                    label: localeText(localeCode, '异常受管实例', 'Unhealthy VMs'),
                    value: vmSummary?.vmsError ?? 0,
                    ratio:
                      vmSummary?.vmsTotal && vmSummary.vmsTotal > 0
                        ? (vmSummary.vmsError ?? 0) / vmSummary.vmsTotal
                        : 0,
                    tone: (vmSummary?.vmsError ?? 0) > 0 ? 'danger' : 'neutral',
                  },
                ],
              },
              {
                key: 'compute-provider-spectrum',
                kind: 'spectrum',
                title: localeText(localeCode, '多源算力与任务吞吐频谱', 'Multi-Provider Capacity Spectrum'),
                subtitle: localeText(localeCode, '跨虚拟化与容器编排栈', 'Across virtualization & runtime'),
                actionLabel: localeText(localeCode, '集群连接', 'Clusters'),
                actionTo: canViewVirtualization ? '/compute/virtualization/clusters' : undefined,
                bars: [
                  {
                    key: 'vms',
                    label: localeText(localeCode, '虚拟机', 'VMs'),
                    value: vmSummary?.vmsTotal ?? 0,
                    tone: 'success',
                    to: canViewVirtualization ? '/compute/virtualization/vms' : undefined,
                  },
                  {
                    key: 'services',
                    label: localeText(localeCode, '容器服务', 'Services'),
                    value: serviceSummary?.services ?? 0,
                    tone: 'primary',
                    to: canViewDocker ? '/compute/runtimes/projects' : undefined,
                  },
                  {
                    key: 'hosts',
                    label: localeText(localeCode, '宿主机', 'Hosts'),
                    value: runtimeSummary?.total ?? 0,
                    tone: 'primary',
                    to: canViewDocker ? '/compute/runtimes/hosts' : undefined,
                  },
                  {
                    key: 'projects',
                    label: localeText(localeCode, '项目栈', 'Projects'),
                    value: projectSummary?.projects ?? 0,
                    tone: 'success',
                  },
                  {
                    key: 'tasks',
                    label: localeText(localeCode, '活跃任务', 'Tasks'),
                    value: (taskSummary?.queued ?? 0) + (taskSummary?.running ?? 0),
                    tone: (taskSummary?.failed ?? 0) > 0 ? 'warning' : 'primary',
                    to: canViewTasks ? '/compute/tasks/operations' : undefined,
                  },
                ],
              },
              {
                key: 'compute-risk-radar',
                kind: 'radar',
                title: localeText(localeCode, '连接健康与故障态势雷达', 'Provider & Risk Radar'),
                subtitle: localeText(
                  localeCode,
                  `${overview?.providerHealth.length ?? 0} 个提供方接入`,
                  `${overview?.providerHealth.length ?? 0} providers`,
                ),
                actionLabel: localeText(localeCode, '任务中心', 'Tasks'),
                actionTo: canViewTasks ? '/compute/tasks/operations' : undefined,
                emptyLabel: localeText(localeCode, '全部虚拟化连接与宿主机健康', 'All compute nodes healthy'),
                blips: [
                  ...attention.slice(0, 2).map((item, idx) => ({
                    key: `attn-${item.code}-${idx}`,
                    label:
                      item.resources?.[0]?.displayName ||
                      attentionLabel(item.code, item.summary, localeCode),
                    badge: item.severity.toUpperCase(),
                    tone:
                      item.severity === 'critical'
                        ? ('danger' as const)
                        : ('warning' as const),
                    to: attentionTarget(
                      item.code,
                      item.resources?.length === 1 ? item.resources[0] : undefined,
                    ),
                  })),
                  ...(overview?.providerHealth ?? []).slice(0, 2).map((prov) => ({
                    key: `prov-${prov.domain}-${prov.providerKey}`,
                    label: prov.providerKey,
                    badge: prov.status === 'healthy' ? 'HEALTHY' : prov.status.toUpperCase(),
                    tone:
                      prov.status === 'healthy'
                        ? ('success' as const)
                        : prov.status === 'degraded'
                          ? ('warning' as const)
                          : ('danger' as const),
                    to:
                      prov.domain === 'container_runtime'
                        ? '/compute/runtimes/hosts'
                        : '/compute/virtualization/clusters',
                  })),
                ],
              },
            ]}
            kpis={[
              ...(connectionSummary
                ? [
                    {
                      key: 'conn',
                      label: localeText(localeCode, '虚拟化集群', 'Connections'),
                      value: `${connectionSummary.connectionsHealthy ?? 0}/${connectionSummary.connectionsTotal ?? 0}`,
                    },
                  ]
                : []),
              ...(agentSummary
                ? [
                    {
                      key: 'agent',
                      label: localeText(localeCode, 'Agent 在线', 'Agents online'),
                      value: `${agentSummary.online ?? 0}/${agentSummary.total ?? 0}`,
                    },
                  ]
                : []),
            ]}
            telemetryNodes={[
              {
                id: 'vm-fleet',
                label: `VMS · ${vmSummary?.vmsTotal ?? '—'}`,
                title: localeText(localeCode, '虚拟机实例舰队', 'Virtual Machine Fleet'),
                metric: localeText(
                  localeCode,
                  `${vmSummary?.vmsRunning ?? 0} 台运行中 / ${vmSummary?.vmsStopped ?? 0} 台已停止`,
                  `${vmSummary?.vmsRunning ?? 0} running / ${vmSummary?.vmsStopped ?? 0} stopped`,
                ),
                status: 'primary',
                path: canAccessRoute(
                  getRouteMeta('/compute/virtualization/vms'),
                  snapshot,
                )
                  ? '/compute/virtualization/vms'
                  : undefined,
              },
              {
                id: 'vm-alert',
                label:
                  (vmSummary?.vmsError ?? 0) > 0
                    ? `VM-ERR · ${vmSummary?.vmsError}`
                    : 'GPU-A100',
                title: localeText(localeCode, '异常虚拟机巡检', 'VM Health Signal'),
                metric:
                  (vmSummary?.vmsError ?? 0) > 0
                    ? localeText(
                        localeCode,
                        `${vmSummary?.vmsError} 台实例状态异常`,
                        `${vmSummary?.vmsError} unhealthy instances`,
                      )
                    : localeText(localeCode, '实例探针健康', 'Instance probes healthy'),
                status: (vmSummary?.vmsError ?? 0) > 0 ? 'critical' : 'healthy',
                path: canAccessRoute(
                  getRouteMeta('/compute/virtualization/vms'),
                  snapshot,
                )
                  ? '/compute/virtualization/vms'
                  : undefined,
              },
              {
                id: 'virt-conn',
                label: `CONN · ${connectionSummary?.connectionsHealthy ?? 0}/${connectionSummary?.connectionsTotal ?? 0}`,
                title: localeText(localeCode, '虚拟化连接池', 'Hypervisor Connections'),
                metric:
                  (connectionSummary?.connectionsDegraded ?? 0) > 0
                    ? localeText(
                        localeCode,
                        `${connectionSummary?.connectionsDegraded} 个连接降级`,
                        `${connectionSummary?.connectionsDegraded} connections degraded`,
                      )
                    : localeText(localeCode, '全部连接正常', 'All connections healthy'),
                status:
                  (connectionSummary?.connectionsDegraded ?? 0) > 0 ? 'warning' : 'healthy',
                path: canAccessRoute(
                  getRouteMeta('/compute/virtualization/clusters'),
                  snapshot,
                )
                  ? '/compute/virtualization/clusters'
                  : undefined,
              },
              {
                id: 'runtime-svc',
                label: `SERVICES · ${serviceSummary?.services ?? '—'}`,
                title: localeText(localeCode, '容器运行时服务', 'Container Services'),
                metric: localeText(
                  localeCode,
                  `${projectSummary?.projects ?? 0} 个编排项目运行中`,
                  `${projectSummary?.projects ?? 0} projects running`,
                ),
                status: 'primary',
                path: canAccessRoute(
                  getRouteMeta('/compute/runtimes/projects'),
                  snapshot,
                )
                  ? '/compute/runtimes/projects'
                  : undefined,
              },
              {
                id: 'runtime-hosts',
                label: `HOSTS · ${runtimeSummary?.available ?? 0}/${runtimeSummary?.total ?? 0}`,
                title: localeText(localeCode, '宿主机与 Agent 节点', 'Runtime Hosts'),
                metric:
                  (runtimeSummary?.error ?? 0) > 0
                    ? localeText(
                        localeCode,
                        `${runtimeSummary?.error} 台主机异常`,
                        `${runtimeSummary?.error} hosts unhealthy`,
                      )
                    : localeText(
                        localeCode,
                        `${agentSummary?.online ?? 0} 个 Agent 在线`,
                        `${agentSummary?.online ?? 0} agents online`,
                      ),
                status: (runtimeSummary?.error ?? 0) > 0 ? 'critical' : 'healthy',
                path: canAccessRoute(
                  getRouteMeta('/compute/runtimes/hosts'),
                  snapshot,
                )
                  ? '/compute/runtimes/hosts'
                  : undefined,
              },
              {
                id: 'tasks-node',
                label:
                  (taskSummary?.failed ?? 0) > 0
                    ? `TASK-FAIL · ${taskSummary?.failed}`
                    : `TASKS · ${(taskSummary?.queued ?? 0) + (taskSummary?.running ?? 0)}`,
                title: localeText(localeCode, '计算异步任务队列', 'Compute Task Queue'),
                metric: localeText(
                  localeCode,
                  `${taskSummary?.running ?? 0} 执行中 / ${taskSummary?.failed ?? 0} 失败`,
                  `${taskSummary?.running ?? 0} running / ${taskSummary?.failed ?? 0} failed`,
                ),
                status: (taskSummary?.failed ?? 0) > 0 ? 'warning' : 'healthy',
                path: canAccessRoute(
                  getRouteMeta('/compute/tasks/operations'),
                  snapshot,
                )
                  ? '/compute/tasks/operations'
                  : undefined,
              },
            ]}
            embeddedPods={visibleOverviewStats.slice(1).map((item) => {
              const allowed = canAccessRoute(getRouteMeta(item.path.split('?')[0]), snapshot)
              return {
                key: item.key,
                className: `soha-overview-metric-card ${item.className}`,
                label: (
                  <>
                    {item.icon}
                    <span>{item.label}</span>
                  </>
                ),
                value: item.value,
                helper: item.helper,
                tone:
                  item.key === 'runtime-hosts' && (runtimeSummary?.error ?? 0) > 0
                    ? 'danger'
                    : 'default',
                to: item.path,
                readonly: !allowed,
                ariaLabel: localeText(
                  localeCode,
                  `查看${String(item.label)}`,
                  `View ${String(item.label)}`,
                ),
                visual:
                  item.key === 'runtime-hosts' && runtimeSummary ? (
                    <OverviewNodeMatrix
                      items={[
                        { count: runtimeSummary.available, tone: 'success' },
                        { count: runtimeSummary.error, tone: 'danger' },
                        { count: runtimeSummary.waitingAgent, tone: 'warning' },
                      ]}
                    />
                  ) : (
                    <Suspense fallback={<div className="soha-compute-chart-placeholder" />}>
                      <SummaryChart {...item.visual} surface="hero" />
                    </Suspense>
                  ),
              }
            })}
            segments={
              vmSummary
                ? [
                    {
                      key: 'running',
                      label: localeText(localeCode, '运行中', 'Running'),
                      value: vmSummary.vmsRunning,
                      tone: 'success',
                    },
                    {
                      key: 'stopped',
                      label: localeText(localeCode, '已停止', 'Stopped'),
                      value: vmSummary.vmsStopped,
                      tone: 'neutral',
                    },
                    {
                      key: 'error',
                      label: localeText(localeCode, '异常', 'Unhealthy'),
                      value: vmSummary.vmsError,
                      tone: 'danger',
                    },
                  ]
                : undefined
            }
            interactiveHint={localeText(
              localeCode,
              '拖拽旋转 · 点击 3D 节点下钻',
              'Drag to rotate · Click 3D nodes to drill down',
            )}
            ctaLabel={localeText(localeCode, '浏览实例', 'Explore Fleet')}
            ctaTo={visibleOverviewStats[0].path}
            ctaAriaLabel={localeText(
              localeCode,
              `查看${String(visibleOverviewStats[0].label)}`,
              `View ${String(visibleOverviewStats[0].label)}`,
            )}
            ctaReadonly={
              !canAccessRoute(getRouteMeta(visibleOverviewStats[0].path.split('?')[0]), snapshot)
            }
          />
        ) : (
          <div className="soha-overview-metric-grid">
            {visibleOverviewStats.map(({ key, path, ...item }) => (
              <OverviewLink
                snapshot={snapshot}
                aria-label={localeText(
                  localeCode,
                  `查看${String(item.label)}`,
                  `View ${String(item.label)}`,
                )}
                className="soha-overview-card-link is-bento-tile"
                key={key}
                to={path}
              >
                <Card
                  loading={overviewQuery.isLoading}
                  className={`soha-overview-metric-card ${item.className}`}
                  classNames={{ body: 'soha-compute-metric-body' }}
                >
                  <div className="soha-compute-metric-content">
                    <div>
                      <span className="soha-compute-metric-label">
                        {item.icon}
                        {item.label}
                      </span>
                      <strong className="soha-compute-metric-value">{item.value}</strong>
                    </div>
                    <Suspense fallback={<div className="soha-compute-chart-placeholder" />}>
                      <SummaryChart {...item.visual} surface="default" />
                    </Suspense>
                  </div>
                  <div className="soha-compute-metric-footer">
                    <div className="soha-compute-metric-caption">{item.helper}</div>
                  </div>
                </Card>
              </OverviewLink>
            ))}
          </div>
        )}
      </section>
      {lowerPanels}
    </div>
  )
}
