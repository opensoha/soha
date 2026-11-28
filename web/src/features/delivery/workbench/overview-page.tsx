import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import {
  AppstoreOutlined,
  ArrowRightOutlined,
  ClusterOutlined,
  DeploymentUnitOutlined,
  RocketOutlined,
} from '@ant-design/icons'
import { useQueries, useQuery, type UseQueryResult } from '@tanstack/react-query'
import { Button, Card } from 'antd'
import { ManagementState } from '@/components/management-list'
import { OverviewHeroCard } from '@/components/overview-visuals'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { deliveryStageLabels, deliveryStatusLabels } from '../batches/model'
import { isDeliveryReadyStatus, releaseBoardQualitySignal } from '../delivery-status'
import { deliveryQueries } from '../queries'
import type { DeliveryExecutionHistoryEntry, ExecutionTask } from '../domain-types'
import './overview.css'

const ACTIVE_HISTORY_STATUSES = new Set([
  'pending',
  'queued',
  'running',
  'waiting_execution',
  'canceling',
  'waiting_approval',
  'pending_approval',
  'awaiting_approval',
])
const APPROVAL_HISTORY_STATUSES = new Set([
  'waiting_approval',
  'pending_approval',
  'awaiting_approval',
])
const ACTIVE_TASK_STATUSES = new Set(['queued', 'dispatching', 'running'])

interface OverviewRecord {
  key: string
  title: string
  description?: string
  stage?: string
  status: string
  statusLabel?: string
  path?: string
}

function historyRecord(
  entry: DeliveryExecutionHistoryEntry,
  names: Map<string, string>,
): OverviewRecord | undefined {
  const key = entry.kind + '/' + entry.id
  if (entry.batch) {
    const batch = entry.batch
    const completed = batch.nodes.filter((node) => isDeliveryReadyStatus(node.status)).length
    const applications = new Set(batch.targets.map(({ target }) => target.applicationId)).size
    return {
      key,
      title: batch.definition.name,
      description: [
        `${applications} 个应用 / ${batch.serviceCount} 个服务`,
        batch.nodes.length ? `${completed}/${batch.nodes.length} 节点完成` : '',
        batch.partialView ? '仅可见目标' : '',
      ]
        .filter(Boolean)
        .join(' · '),
      stage: [
        ...new Set(
          batch.nodes
            .filter(
              (node) =>
                node.status !== 'pending' &&
                node.status !== 'queued' &&
                ACTIVE_HISTORY_STATUSES.has(node.status),
            )
            .map(({ stage, name }) =>
              stage && stage !== 'barrier' ? (deliveryStageLabels[stage] ?? name) : name,
            )
            .filter(Boolean),
        ),
      ].join(' · '),
      status: batch.status,
      path: `/delivery/batches/${encodeURIComponent(batch.id)}`,
    }
  }
  if (entry.application) {
    const run = entry.application
    const nodes = run.nodeRuns?.length ? run.nodeRuns : run.steps
    const current = nodes.find(
      (node) => node.status !== 'pending' && ACTIVE_HISTORY_STATUSES.has(node.status),
    )
    const application =
      names.get(run.applicationId) ||
      (typeof run.metadata?.applicationName === 'string'
        ? run.metadata.applicationName
        : run.applicationId)
    return {
      key,
      title: run.workflowName,
      description: [application, run.deploymentName].filter(Boolean).join(' · '),
      stage: current?.name,
      status: run.status,
      path: run.deliveryBatchId
        ? `/delivery/batches/${encodeURIComponent(run.deliveryBatchId)}`
        : `/workflows/${encodeURIComponent(run.id)}`,
    }
  }
  if (entry.build) {
    const build = entry.build
    return {
      key,
      title: names.get(build.applicationId) || build.applicationId,
      description: ['构建', build.sourceSystem].filter(Boolean).join(' · '),
      status: build.status,
      path: `/builds/${encodeURIComponent(build.id)}`,
    }
  }
}

function taskRecord(task: ExecutionTask): OverviewRecord {
  return {
    key: task.id,
    title: task.taskKind,
    description: [task.providerKind, task.targetKind].filter(Boolean).join(' · '),
    status: task.status,
    path: `/delivery/execution-tasks/${encodeURIComponent(task.id)}`,
  }
}

function OverviewRecords({
  records,
  active = false,
}: {
  records: OverviewRecord[]
  active?: boolean
}) {
  return (
    <div className="soha-delivery-overview-records" data-active={active}>
      {records.map(({ key, title, description, stage, status, statusLabel, path }) => {
        const context = [description, active ? undefined : stage].filter(Boolean).join(' · ')
        const waitingForRunner = status === 'waiting_execution'
        const content = (
          <>
            <span className="soha-delivery-overview-record-main">
              <strong title={active ? title : undefined}>{title}</strong>
              {context ? <small>{context}</small> : null}
              {active && stage ? (
                <small className="soha-delivery-overview-stage" title={stage}>
                  <span>当前阶段</span> {stage}
                </small>
              ) : null}
            </span>
            <StatusTag
              value={
                waitingForRunner
                  ? 'checking'
                  : APPROVAL_HISTORY_STATUSES.has(status)
                    ? 'warning'
                    : status
              }
              label={
                statusLabel ??
                (waitingForRunner
                  ? deliveryStatusLabels.waiting_execution
                  : APPROVAL_HISTORY_STATUSES.has(status)
                    ? '待审批'
                    : undefined)
              }
            />
          </>
        )
        return path ? (
          <Link className="soha-delivery-overview-record" key={key} to={path}>
            {content}
          </Link>
        ) : (
          <div className="soha-delivery-overview-record" key={key}>
            {content}
          </div>
        )
      })}
    </div>
  )
}

function QueryState({
  queries,
  label,
}: {
  queries: Array<Pick<UseQueryResult, 'isLoading' | 'isError' | 'refetch'>>
  label: string
}) {
  const failed = queries.filter((query) => query.isError)
  if (failed.length) {
    return (
      <ManagementState
        bordered={false}
        compact
        kind="error"
        title={label + '加载失败'}
        actions={
          <Button onClick={() => failed.forEach((query) => void query.refetch())}>重试</Button>
        }
      />
    )
  }
  return queries.some((query) => query.isLoading) ? (
    <ManagementState bordered={false} compact kind="loading" />
  ) : null
}

function SectionLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link to={to}>
      {children} <ArrowRightOutlined aria-hidden="true" />
    </Link>
  )
}

export function DeliveryOverviewPage() {
  const permissionQuery = usePermissionSnapshot()
  const snapshot = permissionQuery.data?.data
  const canViewApplications = hasPermission(snapshot, 'delivery.applications.view')
  const canViewServices = hasPermission(snapshot, 'delivery.application-services.view')
  const canViewEnvironments = hasPermission(snapshot, 'delivery.application-environments.view')
  const canViewBoard = hasPermission(snapshot, 'delivery.release-board.view')
  const canViewBundles = hasPermission(snapshot, 'delivery.release-bundles.view')
  const canViewTasks = hasPermission(snapshot, 'delivery.execution-tasks.view')
  const canViewWorkflows = hasPermission(snapshot, 'delivery.workflows.view')
  const canViewHistory = canViewWorkflows || canViewApplications
  const useTaskHistory = !canViewHistory && canViewTasks

  const applications = useQuery(deliveryQueries.applications.list(canViewApplications))
  const visibleApps = canViewApplications ? (readableQueryData(applications) ?? []) : []
  const services = useQueries({
    queries:
      canViewServices && !applications.isError
        ? visibleApps.map((app) => deliveryQueries.applications.services(app.id))
        : [],
  })
  const environments = useQuery(
    deliveryQueries.environments.list(canViewEnvironments && !canViewBoard),
  )
  const board = useQuery(deliveryQueries.releaseBoard.list({ enabled: canViewBoard }))
  const bundles = useQuery(
    deliveryQueries.releaseBundles.list({ enabled: canViewBundles && !canViewApplications }),
  )
  const tasks = useQuery(deliveryQueries.executionTasks.list({ enabled: useTaskHistory }))
  const active = useQuery(
    deliveryQueries.executionHistory({ status: 'running', limit: 6 }, canViewHistory),
  )
  const approval = useQuery(
    deliveryQueries.executionHistory({ status: 'approval', limit: 6 }, canViewWorkflows),
  )
  const recent = useQuery(
    deliveryQueries.executionHistory({ status: 'all', limit: 24 }, canViewHistory),
  )

  const names = new Map(visibleApps.map(({ id, name }) => [id, name]))
  const visibleTasks = useTaskHistory ? (readableQueryData(tasks) ?? []) : []
  const activeRecords = canViewHistory
    ? (readableQueryData(active)?.items ?? []).flatMap((entry) => historyRecord(entry, names) ?? [])
    : visibleTasks
        .filter((task) => ACTIVE_TASK_STATUSES.has(task.status))
        .slice(0, 6)
        .map(taskRecord)
  const blocked = canViewBoard
    ? (readableQueryData(board) ?? []).filter(
        (entry) => releaseBoardQualitySignal(entry).value === 'blocked',
      )
    : []
  const waitingRecords: OverviewRecord[] = [
    ...(canViewWorkflows
      ? (readableQueryData(approval)?.items ?? []).flatMap(
          (entry) => historyRecord(entry, names) ?? [],
        )
      : []),
    ...blocked.slice(0, 6).map((entry) => ({
      key: entry.applicationEnvironmentId,
      title: entry.applicationName,
      description: entry.environmentName || entry.environmentKey || entry.environmentId,
      status: 'failed',
      statusLabel: '阻塞',
      path: `/delivery/analysis?applicationEnvironmentId=${encodeURIComponent(entry.applicationEnvironmentId)}`,
    })),
  ]
  const ongoingKeys = new Set([...activeRecords, ...waitingRecords].map(({ key }) => key))
  const recentRecords = canViewHistory
    ? (readableQueryData(recent)?.items ?? [])
        .flatMap((entry) => {
          const record = historyRecord(entry, names)
          return record &&
            !ongoingKeys.has(record.key) &&
            !ACTIVE_HISTORY_STATUSES.has(record.status)
            ? [
                {
                  ...record,
                  description: [record.description, formatDateTime(entry.createdAt)]
                    .filter(Boolean)
                    .join(' · '),
                },
              ]
            : []
        })
        .slice(0, 6)
    : visibleTasks
        .filter(
          (task) =>
            !ACTIVE_TASK_STATUSES.has(task.status) && !APPROVAL_HISTORY_STATUSES.has(task.status),
        )
        .sort((a, b) => (b.updatedAt || b.createdAt).localeCompare(a.updatedAt || a.createdAt))
        .slice(0, 6)
        .map(taskRecord)
  const metrics: Array<{ label: string; value: number | undefined }> = []
  const metricQueries = []
  if (canViewApplications) {
    metrics.push({ label: '应用', value: applications.isSuccess ? visibleApps.length : undefined })
    metricQueries.push(applications)
    if (canViewServices) {
      metrics.push({
        label: '服务',
        value:
          applications.isSuccess && services.every((query) => query.isSuccess)
            ? services.reduce((count, query) => count + (readableQueryData(query)?.length ?? 0), 0)
            : undefined,
      })
      metricQueries.push(...services)
    }
  }
  if (canViewBoard) {
    const clusters = (board.data ?? []).flatMap((entry) =>
      (entry.targets ?? []).map((target) => target.clusterId).filter(Boolean),
    )
    metrics.push({
      label: '发布目标涉及集群',
      value: board.isSuccess ? new Set(clusters).size : undefined,
    })
    metricQueries.push(board)
  }
  if (canViewEnvironments && !canViewBoard) {
    metrics.push({
      label: '环境绑定',
      value: environments.isSuccess ? environments.data.length : undefined,
    })
    metricQueries.push(environments)
  }
  if (canViewBundles && !canViewApplications) {
    metrics.push({ label: '版本包', value: bundles.isSuccess ? bundles.data.length : undefined })
    metricQueries.push(bundles)
  }

  const state = permissionQuery.isError ? (
    <ManagementState
      kind="error"
      actions={<Button onClick={() => void permissionQuery.refetch()}>重试</Button>}
    />
  ) : permissionQuery.isLoading ? (
    <ManagementState kind="loading" />
  ) : null
  const showExecutions = canViewHistory || useTaskHistory
  const showWaiting = canViewWorkflows || canViewBoard
  const waitingQueries = [...(canViewWorkflows ? [approval] : []), ...(canViewBoard ? [board] : [])]
  const activeQuery = canViewHistory ? active : tasks
  const recentQuery = canViewHistory ? recent : tasks

  return (
    <div className="soha-page soha-overview-page soha-delivery-overview-page">
      <h1 className="soha-delivery-overview-heading">持续交付总览</h1>
      {state ??
        (!showExecutions && !showWaiting && !metrics.length ? (
          <ManagementState kind="no-permission" />
        ) : (
          <>
            {activeQuery.isError ? <QueryState queries={[activeQuery]} label="运行记录" /> : null}
            {waitingQueries.some((q) => q.isError) ? (
              <QueryState queries={waitingQueries} label="待处理记录" />
            ) : null}
            <div className="soha-overview-bento-top">
              <OverviewHeroCard
                variant="delivery"
                eyebrowIcon={<RocketOutlined />}
                eyebrowLabel="持续交付流水线脉冲"
                value={activeRecords.length + recentRecords.length}
                valueSuffix="条近期流水线与批次活动"
                kpis={metrics.slice(0, 2).map((m) => ({
                  key: m.label,
                  label: m.label,
                  value: m.value ?? '—',
                }))}
                telemetryNodes={[
                  {
                    id: 'git-source',
                    label: `APPS · ${visibleApps.length}`,
                    title: '受管应用与仓库触发',
                    metric: `${visibleApps.length} 个应用接入流水线`,
                    status: 'primary',
                    path: canViewApplications ? '/applications' : undefined,
                  },
                  {
                    id: 'ci-build',
                    label: `RUNNING · ${activeRecords.length}`,
                    title: '正在执行的流水线与批次',
                    metric: activeRecords[0]?.title || `${activeRecords.length} 条运行记录`,
                    status: activeRecords.length > 0 ? 'primary' : 'healthy',
                    path: canViewBoard ? '/release-board?view=active' : '/delivery/execution-tasks',
                  },
                  {
                    id: 'canary-gate',
                    label:
                      waitingRecords.length > 0
                        ? `APPROVAL · ${waitingRecords.length}`
                        : 'CANARY · OK',
                    title: '审批与灰度放量门禁',
                    metric:
                      waitingRecords.length > 0
                        ? `${waitingRecords.length} 项待审批或受阻`
                        : '暂无阻塞审批项',
                    status: waitingRecords.length > 0 ? 'warning' : 'healthy',
                    path: canViewBoard ? '/release-board' : undefined,
                  },
                  {
                    id: 'prod-live',
                    label: `RECENT · ${recentRecords.length}`,
                    title: '近期已完成生产交付',
                    metric: `${recentRecords.length} 条近期执行记录已收敛`,
                    status: 'healthy',
                    path: canViewBoard ? '/release-board?view=recent' : '/delivery/execution-tasks',
                  },
                ]}
                embeddedPods={(metrics.length
                  ? metrics
                  : [
                      { label: '正在运行', value: activeRecords.length },
                      { label: '待处理', value: waitingRecords.length },
                      { label: '最近执行', value: recentRecords.length },
                    ]
                )
                  .slice(0, 3)
                  .map(({ label, value }, idx) => ({
                    key: label,
                    label,
                    value: value ?? '—',
                    helper:
                      idx === 0
                        ? '当前可见交付范围'
                        : idx === 1
                          ? '关联受管服务拓扑'
                          : '多环境目标覆盖',
                    icon:
                      idx === 0 ? (
                        <AppstoreOutlined />
                      ) : idx === 1 ? (
                        <DeploymentUnitOutlined />
                      ) : (
                        <ClusterOutlined />
                      ),
                    to:
                      idx === 0 && canViewApplications
                        ? '/applications'
                        : canViewBoard
                          ? '/release-board'
                          : undefined,
                  }))}
                segments={[
                  {
                    key: 'running',
                    label: '正在运行',
                    value: activeRecords.length,
                    tone: 'primary',
                  },
                  {
                    key: 'waiting',
                    label: '待审批 / 阻塞',
                    value: waitingRecords.length,
                    tone: 'warning',
                  },
                  {
                    key: 'recent',
                    label: '近期已结束',
                    value: recentRecords.length,
                    tone: 'success',
                  },
                ]}
                ctaLabel={canViewBoard ? '进入发布看板' : '查看执行任务'}
                ctaTo={canViewBoard ? '/release-board' : '/delivery/execution-tasks'}
                detailDrawerTitle="持续交付流水线与批次明细"
                graphicDeck={[
                  {
                    key: 'delivery-pass-rings',
                    kind: 'rings',
                    title: '交付批次通行光谱环',
                    subtitle: '构建 · 门禁 · 生产切流',
                    actionLabel: '发布看板',
                    actionTo: canViewBoard ? '/release-board' : '/delivery/execution-tasks',
                    centerValue: `${activeRecords.length + recentRecords.length}`,
                    centerLabel: '活跃与完成',
                    rings: [
                      {
                        key: 'recent',
                        label: '近期已收敛',
                        value: recentRecords.length,
                        ratio:
                          recentRecords.length /
                          Math.max(
                            1,
                            activeRecords.length + waitingRecords.length + recentRecords.length,
                          ),
                        tone: 'success',
                        to: canViewBoard ? '/release-board?view=recent' : undefined,
                      },
                      {
                        key: 'running',
                        label: '执行中流水线',
                        value: activeRecords.length,
                        ratio:
                          activeRecords.length /
                          Math.max(
                            1,
                            activeRecords.length + waitingRecords.length + recentRecords.length,
                          ),
                        tone: 'primary',
                        to: canViewBoard ? '/release-board?view=active' : undefined,
                      },
                      {
                        key: 'waiting',
                        label: '待审批与阻塞',
                        value: waitingRecords.length,
                        ratio:
                          waitingRecords.length /
                          Math.max(
                            1,
                            activeRecords.length + waitingRecords.length + recentRecords.length,
                          ),
                        tone: waitingRecords.length > 0 ? 'warning' : 'neutral',
                        to: canViewBoard ? '/release-board' : undefined,
                      },
                    ],
                  },
                  {
                    key: 'delivery-stage-spectrum',
                    kind: 'spectrum',
                    title: '应用拓扑与阶段吞吐频谱',
                    subtitle: '全链路交付漏斗脉冲',
                    actionLabel: '应用列表',
                    actionTo: canViewApplications ? '/applications' : undefined,
                    bars: [
                      {
                        key: 'apps',
                        label: '受管应用',
                        value: visibleApps.length,
                        tone: 'primary',
                        to: canViewApplications ? '/applications' : undefined,
                      },
                      {
                        key: 'services',
                        label: '微服务',
                        value: metrics.find((m) => m.label === '服务')?.value ?? 0,
                        tone: 'primary',
                      },
                      {
                        key: 'active',
                        label: '运行中',
                        value: activeRecords.length,
                        tone: 'primary',
                      },
                      {
                        key: 'waiting',
                        label: '灰度审批',
                        value: waitingRecords.length,
                        tone: waitingRecords.length > 0 ? 'warning' : 'neutral',
                      },
                      {
                        key: 'done',
                        label: '近期完成',
                        value: recentRecords.length,
                        tone: 'success',
                      },
                    ],
                  },
                  {
                    key: 'delivery-gate-radar',
                    kind: 'radar',
                    title: '灰度审批与运行批次雷达',
                    subtitle:
                      waitingRecords.length > 0
                        ? `${waitingRecords.length} 项待放行`
                        : '流水线顺畅',
                    actionLabel: '任务流',
                    actionTo: canViewTasks ? '/delivery/execution-tasks' : undefined,
                    emptyLabel: '暂无正在运行的记录',
                    blips: [
                      ...waitingRecords.slice(0, 2).map((rec) => ({
                        key: `wait-${rec.key}`,
                        label: rec.title,
                        badge: rec.statusLabel || '待审批',
                        tone:
                          rec.status === 'failed'
                            ? ('danger' as const)
                            : ('warning' as const),
                      })),
                      ...activeRecords.slice(0, 2).map((rec) => ({
                        key: `act-${rec.key}`,
                        label: rec.title,
                        badge:
                          [
                            rec.status === 'waiting_execution'
                              ? deliveryStatusLabels.waiting_execution
                              : rec.statusLabel,
                            rec.stage ? `当前阶段 ${rec.stage}` : undefined,
                          ]
                            .filter(Boolean)
                            .join(' · ') || '执行中',
                        tone: 'primary' as const,
                      })),
                      ...recentRecords.slice(0, 1).map((rec) => ({
                        key: `rec-${rec.key}`,
                        label: rec.title,
                        badge: '已收敛',
                        tone: 'success' as const,
                      })),
                    ].slice(0, 5),
                  },
                ]}
              />
              {showExecutions || showWaiting ? (
                <div
                  className="soha-delivery-overview-panels"
                  data-paired={showExecutions && showWaiting}
                >
                  {showExecutions ? (
                    <Card
                      size="small"
                      className="soha-overview-panel-card"
                      title={<h2 className="soha-delivery-overview-panel-heading">正在运行</h2>}
                      extra={
                        <SectionLink
                          to={
                            canViewHistory
                              ? '/release-board?view=active'
                              : '/delivery/execution-tasks'
                          }
                        >
                          查看进行中
                        </SectionLink>
                      }
                    >
                      <QueryState queries={[activeQuery]} label="运行记录" />
                      {activeRecords.length ? (
                        <OverviewRecords records={activeRecords} active />
                      ) : activeQuery.isSuccess ? (
                        <ManagementState
                          bordered={false}
                          compact
                          kind="empty"
                          title="暂无正在运行的记录"
                        />
                      ) : null}
                    </Card>
                  ) : null}
                  {showWaiting ? (
                    <Card
                      size="small"
                      className="soha-overview-panel-card"
                      title={<h2 className="soha-delivery-overview-panel-heading">待处理</h2>}
                    >
                      <QueryState queries={waitingQueries} label="待处理记录" />
                      {waitingRecords.length ? (
                        <OverviewRecords records={waitingRecords} />
                      ) : waitingQueries.every((query) => query.isSuccess) ? (
                        <ManagementState
                          bordered={false}
                          compact
                          kind="empty"
                          title="暂无审批或阻塞待处理"
                        />
                      ) : null}
                    </Card>
                  ) : null}
                </div>
              ) : null}
              {showExecutions ? (
                <Card
                  size="small"
                  className="soha-overview-panel-card"
                  title={<h2 className="soha-delivery-overview-panel-heading">最近执行</h2>}
                  extra={
                    <SectionLink
                      to={canViewHistory ? '/release-board?view=recent' : '/delivery/execution-tasks'}
                    >
                      查看执行记录
                    </SectionLink>
                  }
                >
                  <QueryState queries={[recentQuery]} label="执行记录" />
                  {recentRecords.length ? (
                    <OverviewRecords records={recentRecords} />
                  ) : recentQuery.isSuccess ? (
                    <ManagementState
                      bordered={false}
                      compact
                      kind="empty"
                      title="近期记录中暂无已结束的执行"
                    />
                  ) : null}
                </Card>
              ) : null}
              {metricQueries.some((query) => query.isError) ? (
                <QueryState queries={metricQueries} label="部分统计" />
              ) : null}
            </div>
          </>
        ))}
    </div>
  )
}
