import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { ArrowRightOutlined } from '@ant-design/icons'
import { useQueries, useQuery, type UseQueryResult } from '@tanstack/react-query'
import { Button, Card } from 'antd'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
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
      description: [application, run.deploymentName, current?.name].filter(Boolean).join(' · '),
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
  className,
}: {
  records: OverviewRecord[]
  className?: string
}) {
  return (
    <div className={`soha-delivery-overview-records ${className ?? ''}`}>
      {records.map(({ key, title, description, status, statusLabel, path }) => {
        const content = (
          <>
            <span className="soha-delivery-overview-record-main">
              <strong>{title}</strong>
              {description ? <small>{description}</small> : null}
            </span>
            <StatusTag value={status} label={statusLabel} />
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
                      <OverviewRecords
                        records={activeRecords}
                        className="soha-delivery-overview-active"
                      />
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
            {metrics.length ? (
              <Card
                size="small"
                className="soha-overview-panel-card"
                title={<h2 className="soha-delivery-overview-panel-heading">数据概览</h2>}
                extra={<span className="soha-delivery-overview-scope">当前可见记录</span>}
              >
                <QueryState queries={metricQueries} label="部分统计" />
                <dl className="soha-delivery-overview-metrics">
                  {metrics.map(({ label, value }) => (
                    <div key={label}>
                      <dt>{label}</dt>
                      <dd>{value ?? '—'}</dd>
                    </div>
                  ))}
                </dl>
              </Card>
            ) : null}
          </>
        ))}
    </div>
  )
}
