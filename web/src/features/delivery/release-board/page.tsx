import { useEffect, useRef, useState } from 'react'
import { App, Button, Card, Form, Popconfirm, Progress, Space, Typography } from 'antd'
import { ReloadOutlined, RightOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import {
  ManagementIconButton,
  ManagementQueryScope,
  ManagementState,
  ManagementKeywordField,
  ManagementQueryGrid,
} from '@/components/management-list'
import { MutationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import {
  isDeliveryActiveStatus,
  isDeliveryFailureStatus,
  isDeliveryReadyStatus,
  normalizeDeliveryStatus,
} from '../delivery-status'
import { deliveryMutations } from '../mutations'
import { deliveryQueries } from '../queries'
import type {
  BuildRecord,
  DeliveryBatch,
  DeliveryExecutionHistoryParams,
  WorkflowRun,
} from '../types'
import { deliveryStatusLabels } from '../batches/model'
import { HistoryScopeFilters } from './history-scope-filters'
import { durationLabel, executionDuration } from './execution-trend-model'
import './styles.css'

const { Text } = Typography
function workflowMetadataText(run: WorkflowRun, key: string) {
  const value = run.metadata?.[key]
  return typeof value === 'string' && value.trim() ? value.trim() : ''
}

function workflowApplicationName(run: WorkflowRun) {
  return workflowMetadataText(run, 'applicationName') || run.applicationId
}

function workflowTarget(run: WorkflowRun) {
  return [run.clusterId, run.namespace, run.deploymentName].filter(Boolean).join(' / ')
}

function workflowNodes(run: WorkflowRun) {
  return run.nodeRuns?.length ? run.nodeRuns : run.steps
}

function workflowProgress(run: WorkflowRun) {
  const nodes = workflowNodes(run)
  const progressed = nodes.filter(
    (node) => normalizeDeliveryStatus(node.status) !== 'pending',
  ).length
  return {
    progressed,
    total: nodes.length,
    percent: nodes.length ? Math.round((progressed / nodes.length) * 100) : 0,
  }
}

function workflowCurrentNode(run: WorkflowRun) {
  const nodes = workflowNodes(run)
  return (
    nodes.find((node) => isDeliveryActiveStatus(node.status)) ??
    nodes.find((node) => isDeliveryFailureStatus(node.status)) ??
    nodes[nodes.length - 1]
  )
}

function WorkflowRunCard({
  run,
  pendingDecision,
  decisionDisabled,
  onApprove,
  onReject,
}: {
  run: WorkflowRun
  pendingDecision?: 'approve' | 'reject'
  decisionDisabled?: boolean
  onApprove?: () => void
  onReject?: () => void
}) {
  const { localeCode } = useI18n()
  const progress = workflowProgress(run)
  const currentNode = workflowCurrentNode(run)
  const applicationName = workflowApplicationName(run)
  const target = workflowTarget(run)
  const active = isDeliveryActiveStatus(run.status)
  const nodeLabel = active
    ? localeCode === 'zh_CN'
      ? '当前节点'
      : 'Current node'
    : isDeliveryFailureStatus(run.status)
      ? localeCode === 'zh_CN'
        ? '失败节点'
        : 'Failed node'
      : localeCode === 'zh_CN'
        ? '最后节点'
        : 'Last node'

  return (
    <Card className="soha-release-run-card" role="listitem" size="small">
      <div className="soha-release-run-card__header">
        <div className="soha-release-run-card__identity">
          <Link
            aria-label={`${localeCode === 'zh_CN' ? '查看工作流' : 'View workflow'} ${run.workflowName}`}
            className="soha-release-run-card__link"
            to={
              run.deliveryBatchId
                ? `/delivery/batches/${encodeURIComponent(run.deliveryBatchId)}`
                : `/workflows/${encodeURIComponent(run.id)}`
            }
          >
            <Text strong ellipsis title={run.workflowName}>
              {run.workflowName}
            </Text>
            <RightOutlined aria-hidden="true" />
          </Link>
          <Text type="secondary" ellipsis title={run.id}>
            {run.id}
          </Text>
        </div>
        <StatusTag value={run.status} />
      </div>

      <div className="soha-release-run-card__details">
        <div>
          <Text type="secondary">{localeCode === 'zh_CN' ? '应用' : 'Application'}</Text>
          <Text strong ellipsis title={applicationName}>
            {applicationName}
          </Text>
          {applicationName !== run.applicationId ? (
            <Text type="secondary" ellipsis title={run.applicationId}>
              {run.applicationId}
            </Text>
          ) : null}
        </div>
        <div>
          <Text type="secondary">{localeCode === 'zh_CN' ? '目标' : 'Target'}</Text>
          <Text ellipsis title={target || '-'}>
            {target || '-'}
          </Text>
        </div>
      </div>

      <div className="soha-release-run-card__progress">
        <div className="soha-release-run-card__progress-label">
          <Text type="secondary">{localeCode === 'zh_CN' ? '节点进度' : 'Node progress'}</Text>
          <Text>
            {progress.progressed}/{progress.total} {localeCode === 'zh_CN' ? '节点' : 'nodes'}
          </Text>
        </div>
        <Progress
          aria-label={localeCode === 'zh_CN' ? '工作流节点进度' : 'Workflow node progress'}
          percent={progress.percent}
          showInfo={false}
          size="small"
          status={
            isDeliveryFailureStatus(run.status)
              ? 'exception'
              : isDeliveryReadyStatus(run.status)
                ? 'success'
                : 'normal'
          }
        />
      </div>

      <div className="soha-release-run-card__footer">
        <Text ellipsis title={`${nodeLabel}：${currentNode?.name || '-'}`}>
          {nodeLabel}：{currentNode?.name || '-'}
        </Text>
        <Space size={8} wrap>
          {onApprove ? (
            <Popconfirm
              cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
              okText={localeCode === 'zh_CN' ? '批准' : 'Approve'}
              title={localeCode === 'zh_CN' ? '确认批准这个工作流？' : 'Approve this workflow?'}
              onConfirm={onApprove}
            >
              <Button
                aria-label={`${localeCode === 'zh_CN' ? '批准工作流' : 'Approve workflow'} ${run.workflowName}`}
                disabled={decisionDisabled || Boolean(pendingDecision)}
                loading={pendingDecision === 'approve'}
                size="small"
                type="primary"
              >
                {localeCode === 'zh_CN' ? '批准' : 'Approve'}
              </Button>
            </Popconfirm>
          ) : null}
          {onReject ? (
            <Popconfirm
              cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
              okButtonProps={{ danger: true }}
              okText={localeCode === 'zh_CN' ? '拒绝' : 'Reject'}
              title={localeCode === 'zh_CN' ? '确认拒绝这个工作流？' : 'Reject this workflow?'}
              onConfirm={onReject}
            >
              <Button
                aria-label={`${localeCode === 'zh_CN' ? '拒绝工作流' : 'Reject workflow'} ${run.workflowName}`}
                danger
                disabled={decisionDisabled || Boolean(pendingDecision)}
                loading={pendingDecision === 'reject'}
                size="small"
              >
                {localeCode === 'zh_CN' ? '拒绝' : 'Reject'}
              </Button>
            </Popconfirm>
          ) : null}
          <Text type="secondary">{formatDateTime(run.updatedAt)}</Text>
        </Space>
      </div>
    </Card>
  )
}

function BatchRunCard({ batch }: { batch: DeliveryBatch }) {
  const { localeCode } = useI18n()
  const english = localeCode === 'en_US'
  const completed = batch.nodes.filter((node) => isDeliveryReadyStatus(node.status)).length
  return (
    <Card className="soha-release-run-card" role="listitem" size="small">
      <div className="soha-release-run-card__header">
        <div className="soha-release-run-card__identity">
          <Link
            className="soha-release-run-card__link"
            to={`/delivery/batches/${encodeURIComponent(batch.id)}`}
          >
            <Text strong ellipsis title={batch.definition.name}>
              {batch.definition.name}
            </Text>
            <RightOutlined aria-hidden="true" />
          </Link>
          <Text type="secondary" ellipsis title={batch.id}>
            {batch.id}
          </Text>
        </div>
        <StatusTag
          value={batch.status === 'partially_completed' ? 'warning' : batch.status}
          label={english ? batch.status : deliveryStatusLabels[batch.status]}
        />
      </div>
      <div className="soha-release-run-card__summary">
        {batch.partialView ? (
          <MetadataTag label={english ? 'Accessible targets only' : '仅显示可见目标'} />
        ) : null}
        <Text>
          {Array.from(
            new Set(
              batch.targets.map((target) => `${target.applicationName} / ${target.serviceName}`),
            ),
          ).join(' · ')}
        </Text>
        <Text type="secondary">
          {english
            ? `${batch.serviceCount} services · ${batch.targetCount} targets · ${batch.buildCount} builds`
            : `${batch.serviceCount} 个服务 · ${batch.targetCount} 个环境目标 · ${batch.buildCount} 次构建`}
        </Text>
      </div>
      <div className="soha-release-run-card__progress">
        <div className="soha-release-run-card__progress-label">
          <Text type="secondary">{english ? 'Completed nodes' : '已完成节点'}</Text>
          <Text>
            {completed}/{batch.nodes.length}
          </Text>
        </div>
        <Progress
          percent={batch.nodes.length ? Math.round((completed / batch.nodes.length) * 100) : 0}
          showInfo={false}
          size="small"
          status={batch.status === 'failed' ? 'exception' : undefined}
        />
        {batch.stopSummary ? <Text type="danger">{batch.stopSummary}</Text> : null}
      </div>
      <div className="soha-release-run-card__footer">
        <Text type="secondary" ellipsis title={batch.createdBy}>
          {batch.createdBy}
        </Text>
        <Text type="secondary">{formatDateTime(batch.updatedAt)}</Text>
      </div>
    </Card>
  )
}

function BuildRunCard({ build }: { build: BuildRecord }) {
  const english = useI18n().localeCode === 'en_US'
  const label = (key: string) =>
    typeof build.metadata?.[key] === 'string' ? String(build.metadata[key]) : ''
  return (
    <Card className="soha-release-run-card" role="listitem" size="small">
      <div className="soha-release-run-card__header">
        <div className="soha-release-run-card__identity">
          <Link
            className="soha-release-run-card__link"
            to={`/builds/${encodeURIComponent(build.id)}`}
          >
            <Text strong>
              {label('buildSourceName') ||
                label('applicationName') ||
                (english ? 'Build execution' : '构建执行')}
            </Text>
            <RightOutlined aria-hidden="true" />
          </Link>
          <Text type="secondary" ellipsis title={build.id}>
            {build.id}
          </Text>
        </div>
        <StatusTag value={build.status} />
      </div>
      <div className="soha-release-run-card__summary">
        <Text>
          {[label('applicationName') || build.applicationId, label('serviceName')]
            .filter(Boolean)
            .join(' / ')}
        </Text>
        <Space wrap>
          <MetadataTag label={english ? 'Build only' : '仅构建'} />
          <Text type="secondary">{label('refName') || build.sourceSystem}</Text>
        </Space>
        {label('imageTag') ? (
          <Text ellipsis title={label('imageTag')}>
            {label('imageTag')}
          </Text>
        ) : null}
      </div>
      <div className="soha-release-run-card__footer">
        <Text type="secondary">{durationLabel(executionDuration(build, Date.now()), english)}</Text>
        <Text type="secondary">{formatDateTime(build.createdAt)}</Text>
      </div>
    </Card>
  )
}

export function ExecutionHistoryPage() {
  const { localeCode } = useI18n()
  const english = localeCode === 'en_US'
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionQuery = usePermissionSnapshot()
  const permissions = permissionQuery.data?.data
  const canView =
    hasPermission(permissions, 'delivery.workflows.view') ||
    hasPermission(permissions, 'delivery.applications.view')
  const canReview = hasPermission(permissions, 'delivery.application-environments.approve')
  const [search, setSearch] = useSearchParams()
  const location = useLocation()
  const params: DeliveryExecutionHistoryParams = {
    applicationId: search.get('applicationId') || undefined,
    serviceId: search.get('serviceId') || undefined,
    workflowId: search.get('workflowId') || undefined,
    applicationEnvironmentId: search.get('applicationEnvironmentId') || undefined,
    buildSourceId: search.get('buildSourceId') || undefined,
    status: (search.get('status') || 'all') as DeliveryExecutionHistoryParams['status'],
    search: search.get('search') || undefined,
    cursor: search.get('cursor') || undefined,
    limit: 12,
  }
  const history = useQuery(deliveryQueries.executionHistory(params, canView))
  const approve = useMutation(deliveryMutations.workflows.approve(queryClient))
  const reject = useMutation(deliveryMutations.workflows.reject(queryClient))
  const historyData = readableQueryData(history)
  const submitting = useRef(false)
  const [acknowledged, setAcknowledged] = useState<string[]>([])
  const locked =
    approve.isPending ||
    reject.isPending ||
    writeOutcomeUnknown(approve.error) ||
    writeOutcomeUnknown(reject.error)
  const previousCursors: string[] = Array.isArray(location.state?.historyCursors)
    ? location.state.historyCursors
    : []
  useEffect(() => {
    if (!search.has('tab')) return
    setSearch(
      (current) => {
        const next = new URLSearchParams(current)
        next.delete('tab')
        return next
      },
      { replace: true, state: location.state },
    )
  }, [search, setSearch, location.state])
  const updateFilter = (key: string, value: string) =>
    setSearch(
      (current) => {
        const next = new URLSearchParams(current)
        next.delete('cursor')
        if (value && value !== 'all') next.set(key, value)
        else next.delete(key)
        return next
      },
      { replace: true, state: null },
    )
  const movePage = (cursor: string | undefined, cursors: string[]) =>
    setSearch(
      (current) => {
        const next = new URLSearchParams(current)
        if (cursor) next.set('cursor', cursor)
        else next.delete('cursor')
        return next
      },
      { state: { historyCursors: cursors } },
    )
  const decide = async (id: string, action: 'approve' | 'reject') => {
    if (submitting.current || locked || history.isError || !canReview || acknowledged.includes(id))
      return
    const run = historyData?.items.find((entry) => entry.id === id)?.application
    if (!run || run.scope === 'delivery_batch' || run.status !== 'waiting_approval') return
    submitting.current = true
    try {
      await (action === 'approve' ? approve : reject).mutateAsync({
        id,
        comment:
          action === 'approve'
            ? 'Approved from execution history'
            : 'Rejected from execution history',
      })
      setAcknowledged((current) => [...current, id])
      void message.success(english ? 'Decision saved' : '审批结果已保存')
      void history.refetch()
    } catch {
      /* Keep the original decision visible while its outcome is checked. */
    } finally {
      submitting.current = false
    }
  }
  if (permissionQuery.isLoading) return <ManagementState kind="loading" />
  if (permissionQuery.isError)
    return (
      <ManagementState
        kind="error"
        title={english ? 'Permissions unavailable' : '权限加载失败'}
        actions={
          <Button onClick={() => void permissionQuery.refetch()}>
            {english ? 'Retry' : '重试'}
          </Button>
        }
      />
    )
  if (!canView)
    return (
      <ManagementState
        kind="no-permission"
        title={english ? 'No execution history access' : '无权查看执行记录'}
      />
    )
  return (
    <div className="soha-page soha-release-board">
      <section
        className="soha-release-board__runs"
        aria-label={english ? 'Execution history' : '执行记录'}
      >
        <Form className="soha-management-query-form" layout="horizontal">
          <ManagementQueryGrid
            actions={
              <>
                <Button onClick={() => setSearch({}, { replace: true, state: null })}>
                  {english ? 'Reset' : '重置'}
                </Button>
                <ManagementIconButton
                  aria-label={english ? 'Refresh executions' : '刷新执行记录'}
                  icon={<ReloadOutlined />}
                  loading={history.isFetching}
                  tooltip={english ? 'Refresh' : '刷新'}
                  onClick={() => void history.refetch()}
                />
              </>
            }
          >
            <ManagementKeywordField
              inputProps={{ 'aria-label': english ? 'Search executions' : '搜索执行记录' }}
              placeholder={
                english
                  ? 'Search application, service, workflow or build'
                  : '搜索应用、服务、工作流或构建'
              }
              value={params.search || ''}
              onChange={(value) => updateFilter('search', value)}
            />
            <ManagementQueryScope
              aria-label={english ? 'Filter executions by status' : '按状态筛选执行记录'}
              label={english ? 'Status' : '状态'}
              value={params.status || 'all'}
              options={[
                { value: 'all', label: english ? 'All' : '全部' },
                { value: 'running', label: english ? 'Running' : '运行中' },
                { value: 'approval', label: english ? 'Approval' : '待审批' },
                { value: 'succeeded', label: english ? 'Succeeded' : '成功' },
                { value: 'failed', label: english ? 'Failed' : '失败' },
                { value: 'canceled', label: english ? 'Canceled' : '已取消' },
              ]}
              onChange={(value) => updateFilter('status', String(value))}
            />
            <HistoryScopeFilters />
          </ManagementQueryGrid>
        </Form>

        <ReadFeedback query={history} label={english ? 'Execution history' : '执行记录'} />
        <MutationFeedback
          operations={[
            {
              mutation: approve,
              label: english ? 'Approve workflow' : '批准工作流',
              target: approve.variables?.id,
              receipt: {
                message: english
                  ? 'Decision accepted; check the execution record.'
                  : '审批请求已受理，以执行记录的后续状态为准。',
              },
            },
            {
              mutation: reject,
              label: english ? 'Reject workflow' : '拒绝工作流',
              target: reject.variables?.id,
              receipt: {
                message: english
                  ? 'Decision accepted; check the execution record.'
                  : '审批请求已受理，以执行记录的后续状态为准。',
              },
            },
          ]}
        />
        {history.isPending ? (
          <ManagementState
            bordered={false}
            kind="loading"
            title={english ? 'Loading executions' : '正在加载执行记录'}
          />
        ) : historyData?.items.length ? (
          <div className="soha-release-board__list" role="list">
            {historyData.items.map((entry) => {
              if (entry.kind === 'batch' && entry.batch)
                return <BatchRunCard key={`batch/${entry.id}`} batch={entry.batch} />
              if (entry.kind === 'build' && entry.build)
                return <BuildRunCard key={`build/${entry.id}`} build={entry.build} />
              if (entry.kind !== 'application' || !entry.application) return null
              const run = entry.application
              const reviewable =
                canReview && run.scope !== 'delivery_batch' && run.status === 'waiting_approval'
              return (
                <WorkflowRunCard
                  key={`application/${entry.id}`}
                  run={run}
                  decisionDisabled={locked || history.isError || acknowledged.includes(run.id)}
                  pendingDecision={
                    approve.isPending ? 'approve' : reject.isPending ? 'reject' : undefined
                  }
                  onApprove={reviewable ? () => void decide(run.id, 'approve') : undefined}
                  onReject={reviewable ? () => void decide(run.id, 'reject') : undefined}
                />
              )
            })}
          </div>
        ) : !history.isError ? (
          <ManagementState
            bordered={false}
            compact
            title={english ? 'No matching executions' : '暂无匹配的执行记录'}
          />
        ) : null}
        {params.cursor || historyData?.nextCursor ? (
          <nav
            className="soha-release-board__pagination"
            aria-label={english ? 'Execution pagination' : '执行记录分页'}
          >
            <Space wrap>
              {params.cursor ? (
                <Button onClick={() => movePage(undefined, [])}>
                  {english ? 'Latest executions' : '最新记录'}
                </Button>
              ) : null}
              {previousCursors.length ? (
                <Button
                  onClick={() =>
                    movePage(
                      previousCursors[previousCursors.length - 1],
                      previousCursors.slice(0, -1),
                    )
                  }
                >
                  {english ? 'Previous' : '上一页'}
                </Button>
              ) : null}
              <Button
                disabled={!historyData?.nextCursor || history.isFetching || history.isError}
                onClick={() =>
                  movePage(historyData?.nextCursor, [...previousCursors, params.cursor || ''])
                }
              >
                {english ? 'Next' : '下一页'}
              </Button>
            </Space>
          </nav>
        ) : null}
      </section>
    </div>
  )
}
