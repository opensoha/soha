import { useState } from 'react'
import { FileSearchOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Select, Spin, Typography } from 'antd'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { formatDateTime } from '@/utils/time'
import { readableQueryData } from '@/utils/query-feedback'
import { deliveryQueries } from '../queries'
import type { ExecutionTask, ReleaseBoardEntry, ReleaseBundle } from '../types'
import {
  DeliveryAssistDrawer,
  EvidencePanelHeader,
  deliveryEvidenceContext,
} from './evidence-layout'
import { summarizeDeliveryGovernance } from './governance'
import {
  executionTaskUpdatedAt,
  isBlockedStatus,
  releaseBundleUpdatedAt,
  sortByLatest,
} from './shared'

const { Text } = Typography

const issueFilters = [
  { value: 'all', label: '全部状态' },
  { value: 'investigate', label: '待排查' },
  { value: 'blocked', label: '阻塞' },
]
const issueKeys = ['executionTaskId', 'releaseBundleId', 'applicationEnvironmentId'] as const

type Issue = {
  id: string
  applicationId: string
  applicationEnvironmentId?: string
  updatedAt?: string
} & (
  | { key: 'executionTaskId'; task: ExecutionTask }
  | { key: 'releaseBundleId'; bundle: ReleaseBundle; reason?: string }
  | { key: 'applicationEnvironmentId'; entry: ReleaseBoardEntry }
)

function hasFailure(status?: string) {
  return isBlockedStatus(status) || status?.trim().toLowerCase() === 'blocked'
}

function environmentEvidence(entry: ReleaseBoardEntry) {
  return [
    {
      title: '构建记录',
      kind: 'build',
      path: '/builds',
      record: entry.latestBuild,
      at: entry.latestBuild?.finishedAt || entry.latestBuild?.startedAt,
    },
    {
      title: '工作流运行',
      kind: 'workflow',
      path: '/workflows',
      record: entry.latestWorkflow,
      at: entry.latestWorkflow?.updatedAt,
    },
    {
      title: '执行任务',
      kind: 'task',
      path: '/delivery/execution-tasks',
      record: entry.latestExecutionTask,
      at: entry.latestExecutionTask && executionTaskUpdatedAt(entry.latestExecutionTask),
    },
    {
      title: '发布记录',
      kind: 'release',
      path: '/releases',
      record: entry.latestRelease,
      at: entry.latestRelease?.deployedAt,
    },
    {
      title: '版本包',
      kind: 'bundle',
      path: '/delivery/release-bundles',
      record: entry.latestBundle,
      at: entry.latestBundle && releaseBundleUpdatedAt(entry.latestBundle),
    },
  ].flatMap(({ record, at, ...source }) =>
    record && hasFailure(record.status)
      ? [
          {
            ...source,
            status: record.status,
            id: record.id,
            at: at || record.createdAt,
          },
        ]
      : [],
  )
}

function issueTitle(issue: Issue) {
  switch (issue.key) {
    case 'executionTaskId':
      return '异常任务 · ' + (issue.task.taskKind || issue.id)
    case 'releaseBundleId':
      return '版本阻塞 · ' + (issue.bundle.version || issue.id)
    case 'applicationEnvironmentId':
      return '环境阻塞 · ' + (issue.entry.environmentName || issue.id)
  }
}

function issueStatus(issue: Issue) {
  return issue.key === 'executionTaskId'
    ? { value: 'warning', label: '待排查' }
    : { value: 'failed', label: '阻塞' }
}

export function DeliveryAnalysisPage() {
  const navigate = useNavigate()
  const [search, setSearch] = useSearchParams()
  const [assistOpen, setAssistOpen] = useState(false)
  const snapshot = usePermissionSnapshot().data?.data
  const canViewTasks = hasPermission(snapshot, 'delivery.execution-tasks.view')
  const canViewBoard = hasPermission(snapshot, 'delivery.release-board.view')
  const canViewBundles = hasPermission(snapshot, 'delivery.release-bundles.view')
  const canViewApplications = hasPermission(snapshot, 'delivery.applications.view')
  const canViewWorkflows = hasPermission(snapshot, 'delivery.workflows.view')
  const canViewReleases = hasPermission(snapshot, 'delivery.releases.view')
  const canViewHistory = canViewApplications || canViewWorkflows
  const tasksQuery = useQuery(
    deliveryQueries.executionTasks.list({ enabled: canViewTasks, refetchInterval: 5000 }),
  )
  const boardQuery = useQuery(
    deliveryQueries.releaseBoard.list({ enabled: canViewBoard, refetchInterval: 5000 }),
  )
  const bundlesQuery = useQuery(deliveryQueries.releaseBundles.list({ enabled: canViewBundles }))

  const taskData = canViewTasks ? readableQueryData(tasksQuery) : undefined
  const boardData = canViewBoard ? readableQueryData(boardQuery) : undefined
  const bundleData = canViewBundles ? readableQueryData(bundlesQuery) : undefined
  const board = boardData ?? []
  const issues: Issue[] = [
    ...(taskData ?? [])
      .filter((task) => hasFailure(task.status))
      .map(
        (task): Issue => ({
          key: 'executionTaskId',
          id: task.id,
          applicationId: task.applicationId,
          applicationEnvironmentId: task.applicationEnvironmentId,
          updatedAt: executionTaskUpdatedAt(task),
          task,
        }),
      ),
    ...(bundleData ?? []).flatMap((bundle): Issue[] => {
      const governance = summarizeDeliveryGovernance(
        bundle,
        tasksQuery.isError ? [] : (taskData ?? []),
      )
      return hasFailure(bundle.status) || governance.decision === 'blocked'
        ? [
            {
              key: 'releaseBundleId',
              id: bundle.id,
              applicationId: bundle.applicationId,
              applicationEnvironmentId: bundle.applicationEnvironmentId,
              updatedAt: releaseBundleUpdatedAt(bundle),
              bundle,
              reason: governance.reason,
            },
          ]
        : []
    }),
    ...board.flatMap((entry): Issue[] => {
      const latest = sortByLatest(environmentEvidence(entry), (item) => item.at)[0]
      return latest
        ? [
            {
              key: 'applicationEnvironmentId',
              id: entry.applicationEnvironmentId,
              applicationId: entry.applicationId,
              applicationEnvironmentId: entry.applicationEnvironmentId,
              updatedAt: latest.at,
              entry,
            },
          ]
        : []
    }),
  ]
  const filter = issueFilters.find((item) => item.value === search.get('status'))?.value ?? 'all'
  const requestedKey = issueKeys.find((key) => search.get(key))
  const requestedId = requestedKey ? search.get(requestedKey) : null
  const visible = sortByLatest(issues, (issue) => issue.updatedAt).filter(
    (issue) =>
      filter === 'all' ||
      (issue.key === 'executionTaskId' ? filter === 'investigate' : filter === 'blocked'),
  )
  const selected =
    visible.find((issue) => issue.key === requestedKey && issue.id === requestedId) ?? visible[0]
  const context = selected
    ? deliveryEvidenceContext(board, selected.applicationId, selected.applicationEnvironmentId)
    : undefined
  const task = selected?.key === 'executionTaskId' ? selected.task : undefined
  const bundleId = selected?.key === 'releaseBundleId' ? selected.id : task?.releaseBundleId
  const bundle = bundleId ? bundleData?.find((item) => item.id === bundleId) : undefined
  const logsPath = task ? '/delivery/execution-tasks/' + encodeURIComponent(task.id) : undefined
  const manualPath = canViewTasks ? (logsPath ?? '/delivery/execution-tasks') : undefined
  const impactPath =
    selected?.applicationEnvironmentId && canViewApplications
      ? '/applications/' +
        encodeURIComponent(selected.applicationId) +
        '?' +
        new URLSearchParams({
          tab: 'services',
          applicationEnvironmentId: selected.applicationEnvironmentId,
        })
      : canViewHistory
        ? '/release-board'
        : undefined
  const loading =
    (canViewTasks && tasksQuery.isLoading) ||
    (canViewBoard && boardQuery.isLoading) ||
    (canViewBundles && bundlesQuery.isLoading)
  const canRead = canViewTasks || canViewBoard || canViewBundles
  const incomplete =
    (canViewTasks && tasksQuery.isError) ||
    (canViewBoard && boardQuery.isError) ||
    (canViewBundles && bundlesQuery.isError)
  const missingRequested =
    requestedKey &&
    !loading &&
    !issues.some((issue) => issue.key === requestedKey && issue.id === requestedId)
  const detailPermissions: Record<string, boolean> = {
    build: canViewApplications,
    workflow: canViewWorkflows,
    task: canViewTasks,
    release: canViewReleases,
    bundle: canViewBundles,
  }
  const failureMessage = task?.operationState?.failureMessage
  const observations =
    selected?.key === 'applicationEnvironmentId' ? environmentEvidence(selected.entry) : []
  const analysisActions = (
    <>
      {manualPath ? (
        <Button onClick={() => navigate(manualPath)} icon={<FileSearchOutlined />}>
          手工排查
        </Button>
      ) : null}
      <Button onClick={() => setAssistOpen(true)}>分析辅助</Button>
    </>
  )

  function changeSelection(issue?: Issue, status?: string) {
    const next = new URLSearchParams(search)
    issueKeys.forEach((key) => next.delete(key))
    if (issue) next.set(issue.key, issue.id)
    if (status === 'all') next.delete('status')
    else if (status) next.set('status', status)
    setSearch(next, { replace: true })
  }

  return (
    <div className="soha-page soha-delivery-workbench-page">
      <h1 className="soha-delivery-evidence-sr-only">问题分析</h1>
      <ReadFeedback query={tasksQuery} label="执行任务" canRead={canViewTasks} />
      <ReadFeedback query={boardQuery} label="发布看板" canRead={canViewBoard} />
      <ReadFeedback query={bundlesQuery} label="版本包" canRead={canViewBundles} />
      {missingRequested ? <Text type="secondary">当前可见线索中没有找到指定记录。</Text> : null}
      <div className={selected ? 'soha-delivery-evidence-grid is-analysis' : undefined}>
        <section className="soha-delivery-evidence-panel" aria-label="待处理线索">
          <EvidencePanelHeader
            title="待处理线索"
            description={
              taskData || bundleData || boardData ? visible.length + ' 条记录' : undefined
            }
            actions={
              <>
                <Select
                  aria-label="按问题状态筛选"
                  value={filter}
                  options={issueFilters}
                  onChange={(value) => changeSelection(undefined, value)}
                />
                {canRead ? (
                  <ManagementRefreshButton
                    aria-label="刷新问题线索"
                    tooltip="刷新问题线索"
                    loading={
                      tasksQuery.isFetching || boardQuery.isFetching || bundlesQuery.isFetching
                    }
                    onClick={() => {
                      if (canViewTasks) void tasksQuery.refetch()
                      if (canViewBoard) void boardQuery.refetch()
                      if (canViewBundles) void bundlesQuery.refetch()
                    }}
                  />
                ) : null}
              </>
            }
          />
          {visible.length ? (
            <ul className="soha-delivery-evidence-queue">
              {visible.map((issue) => {
                const item = deliveryEvidenceContext(
                  board,
                  issue.applicationId,
                  issue.applicationEnvironmentId,
                )
                return (
                  <li key={issue.key + ':' + issue.id}>
                    <button
                      type="button"
                      aria-pressed={issue === selected}
                      onClick={() => changeSelection(issue)}
                    >
                      <span>
                        <strong>{issueTitle(issue)}</strong>
                        <StatusTag {...issueStatus(issue)} />
                      </span>
                      <span>
                        {[
                          item.application,
                          item.environment,
                          issue.updatedAt && formatDateTime(issue.updatedAt),
                        ]
                          .filter(Boolean)
                          .join(' / ')}
                      </span>
                    </button>
                  </li>
                )
              })}
            </ul>
          ) : loading ? (
            <div className="soha-delivery-evidence-loading" role="status">
              <Spin size="small" /> 正在读取问题线索
            </div>
          ) : (
            <ManagementState
              bordered={false}
              compact
              kind={canRead ? 'empty' : 'no-permission'}
              title={
                !canRead
                  ? '无问题线索查看权限'
                  : issues.length
                    ? '没有符合当前状态的线索'
                    : incomplete
                      ? '当前读取结果无法确认问题线索'
                      : '暂无失败或阻塞线索'
              }
              actions={
                <div className="soha-delivery-evidence-actions">
                  {issues.length ? (
                    <Button type="link" onClick={() => changeSelection(undefined, 'all')}>
                      清除筛选
                    </Button>
                  ) : canViewHistory ? (
                    <Link to="/execution-history">查看最近执行</Link>
                  ) : null}
                  {analysisActions}
                </div>
              }
            />
          )}
        </section>
        {selected && context ? (
          <section className="soha-delivery-evidence-panel" aria-label="问题证据">
            <EvidencePanelHeader
              title={issueTitle(selected)}
              actions={<StatusTag {...issueStatus(selected)} />}
            />
            <div className="soha-delivery-evidence-body">
              <dl className="soha-delivery-evidence-context">
                <div>
                  <dt>应用</dt>
                  <dd>{context.application}</dd>
                </div>
                <div>
                  <dt>环境</dt>
                  <dd>{context.environment || '未绑定'}</dd>
                </div>
                <div>
                  <dt>记录</dt>
                  <dd>
                    <code>{selected.id}</code>
                  </dd>
                </div>
                {bundleId ? (
                  <div>
                    <dt>版本包</dt>
                    <dd>{bundle?.version || bundleId}</dd>
                  </div>
                ) : null}
              </dl>
              {context.binding?.targets?.length ? (
                <dl className="soha-delivery-evidence-subject">
                  <div>
                    <dt>环境登记目标</dt>
                    <dd>
                      {context.binding.targets
                        .map((target) =>
                          [target.namespace, target.workloadName].filter(Boolean).join('/'),
                        )
                        .filter(Boolean)
                        .join('、')}
                    </dd>
                  </div>
                </dl>
              ) : null}
              <h3>已观察到的事实</h3>
              <ol className="soha-delivery-evidence-timeline">
                {task ? (
                  <>
                    {task.createdAt ? (
                      <li>
                        <time dateTime={task.createdAt}>{formatDateTime(task.createdAt)}</time>
                        <strong>任务创建</strong>
                      </li>
                    ) : null}
                    {task.startedAt ? (
                      <li>
                        <time dateTime={task.startedAt}>{formatDateTime(task.startedAt)}</time>
                        <strong>任务开始</strong>
                      </li>
                    ) : null}
                    <li>
                      {selected.updatedAt ? (
                        <time dateTime={selected.updatedAt}>
                          {formatDateTime(selected.updatedAt)}
                        </time>
                      ) : null}
                      <div>
                        <strong>任务状态</strong> <StatusTag value={task.status} />
                        {failureMessage ? <p>{failureMessage}</p> : null}
                        <p>
                          {task.providerKind} / {task.targetKind} · 重试 {task.attemptCount}/
                          {task.maxRetries}
                        </p>
                      </div>
                      {logsPath ? <Link to={logsPath}>查看任务日志</Link> : null}
                    </li>
                  </>
                ) : selected.key === 'releaseBundleId' ? (
                  <li>
                    {selected.updatedAt ? (
                      <time dateTime={selected.updatedAt}>
                        {formatDateTime(selected.updatedAt)}
                      </time>
                    ) : null}
                    <div>
                      <strong>版本包状态</strong> <StatusTag value={selected.bundle.status} />
                      {selected.reason ? <p>{selected.reason}</p> : null}
                    </div>
                    <Link to={'/delivery/release-bundles/' + encodeURIComponent(selected.id)}>
                      查看版本包
                    </Link>
                  </li>
                ) : (
                  sortByLatest(observations, (item) => item.at)
                    .reverse()
                    .map((item) => (
                      <li key={item.kind + ':' + item.id}>
                        {item.at ? <time dateTime={item.at}>{formatDateTime(item.at)}</time> : null}
                        <div>
                          <strong>{item.title}</strong> <StatusTag value={item.status} />
                        </div>
                        {detailPermissions[item.kind] ? (
                          <Link to={item.path + '/' + encodeURIComponent(item.id)}>
                            查看{item.title}
                          </Link>
                        ) : null}
                      </li>
                    ))
                )}
              </ol>
              <div className="soha-delivery-evidence-note">
                <h3>待确认</h3>
                <p>
                  {task
                    ? '结合任务日志和关联发布核对首次失败位置及影响范围。'
                    : selected.key === 'releaseBundleId'
                      ? '结合版本和任务证据核对验证阻塞原因。'
                      : '环境状态来自发布看板，实际影响范围需结合原始运行记录核对。'}
                </p>
              </div>
              <div className="soha-delivery-evidence-actions">
                {bundleId && canViewBundles ? (
                  <Button
                    type="primary"
                    onClick={() =>
                      navigate(
                        '/delivery/testing?' + new URLSearchParams({ releaseBundleId: bundleId }),
                      )
                    }
                  >
                    查看候选版本
                  </Button>
                ) : null}
                {impactPath ? (
                  <Button onClick={() => navigate(impactPath)}>查看影响面</Button>
                ) : null}
                {analysisActions}
              </div>
            </div>
          </section>
        ) : null}
      </div>
      <DeliveryAssistDrawer
        open={assistOpen}
        onClose={() => setAssistOpen(false)}
        title="AI Gateway 故障分析"
        description="可在常规证据基础上汇总失败原因、影响范围和修复建议，适合发布失败、验证失败和 K8s 运行态问题。"
        skillId="delivery-tester"
        manualPath={manualPath}
        manualTitle="手工排查"
        capabilities={[
          'diagnosis.release_failure.analyze',
          'delivery.rollback.context',
          'delivery.release_context.diff',
          'k8s.pods.logs',
          'k8s.deployments.events',
        ]}
      />
    </div>
  )
}
