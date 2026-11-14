import { useState } from 'react'
import { ExperimentOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Popover, Select, Space, Typography, type TableColumnsType } from 'antd'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { DeliveryTable } from '../delivery-table'
import { deliveryQueries } from '../queries'
import type { ReleaseBundle } from '../types'
import {
  deliveryEvidenceContext,
  DeliveryAssistDrawer,
  EvidencePanelHeader,
} from './evidence-layout'
import { summarizeDeliveryGovernance, type GovernanceDecision } from './governance'
import { releaseBundleUpdatedAt, sortByLatest, workflowValidationCount } from './shared'

const { Text } = Typography

type Judgment = GovernanceDecision | 'unknown'
const judgmentLabels: Record<Judgment, { value: string; label: string }> = {
  pending: { value: 'checking', label: '验证中' },
  blocked: { value: 'failed', label: '禁止晋级' },
  passed: { value: 'success', label: '验证通过' },
  not_run: { value: 'unknown', label: '缺少证据' },
  unknown: { value: 'unknown', label: '证据待核对' },
}
const judgmentFilters = [
  { value: 'all', label: '全部判断' },
  ...Object.entries(judgmentLabels).map(([value, { label }]) => ({ value, label })),
]

function EvidenceRefs({ label, refs }: { label: string; refs: string[] }) {
  return (
    <Popover
      title={label + '引用'}
      trigger="click"
      content={
        <Space orientation="vertical">
          {refs.map((ref) => (
            <Text key={ref} code copyable className="soha-delivery-evidence-reference">
              {ref}
            </Text>
          ))}
        </Space>
      }
    >
      <Button size="small">
        {label} {refs.length}
      </Button>
    </Popover>
  )
}

function sourceWorkflowId(bundle: ReleaseBundle) {
  const runId = bundle.metadata?.workflowRunId
  return typeof runId === 'string' && runId.trim()
    ? runId.trim()
    : bundle.artifacts?.find((artifact) => artifact.workflowRunId)?.workflowRunId
}

export function DeliveryTestingPage() {
  const navigate = useNavigate()
  const [search, setSearch] = useSearchParams()
  const [assistOpen, setAssistOpen] = useState(false)
  const snapshot = usePermissionSnapshot().data?.data
  const canViewBundles = hasPermission(snapshot, 'delivery.release-bundles.view')
  const canViewTasks = hasPermission(snapshot, 'delivery.execution-tasks.view')
  const canViewReleaseBoard = hasPermission(snapshot, 'delivery.release-board.view')
  const canViewWorkflow = hasPermission(snapshot, 'delivery.workflows.view')
  const canViewWorkflowCenter =
    canViewWorkflow || hasPermission(snapshot, 'delivery.applications.view')
  const bundlesQuery = useQuery(deliveryQueries.releaseBundles.list({ enabled: canViewBundles }))
  const tasksQuery = useQuery(deliveryQueries.executionTasks.list({ enabled: canViewTasks }))
  const boardQuery = useQuery(deliveryQueries.releaseBoard.list({ enabled: canViewReleaseBoard }))

  const bundleData = canViewBundles ? readableQueryData(bundlesQuery) : undefined
  const taskData = canViewTasks ? readableQueryData(tasksQuery) : undefined
  const board = (canViewReleaseBoard ? readableQueryData(boardQuery) : undefined) ?? []
  const candidates = sortByLatest(bundleData ?? [], releaseBundleUpdatedAt).map((bundle) => {
    const evidence = summarizeDeliveryGovernance(bundle, taskData ?? [])
    const evidenceUnavailable =
      tasksQuery.isError ||
      (canViewTasks && tasksQuery.isLoading) ||
      (!taskData && evidence.decision === 'not_run')
    const judgment: Judgment = evidenceUnavailable ? 'unknown' : evidence.decision
    return { bundle, evidence, judgment, runId: sourceWorkflowId(bundle) }
  })
  const filter =
    judgmentFilters.find((item) => item.value === search.get('judgment'))?.value ?? 'all'
  const visible = candidates.filter(({ judgment }) => filter === 'all' || judgment === filter)
  const requestedId = search.get('releaseBundleId')
  const selected = visible.find(({ bundle }) => bundle.id === requestedId) ?? visible[0]
  const missingRequested =
    requestedId && bundleData && !candidates.some(({ bundle }) => bundle.id === requestedId)
  const bundle = selected?.bundle
  const detail = bundle
    ? deliveryEvidenceContext(board, bundle.applicationId, bundle.applicationEnvironmentId)
    : undefined
  const runId = selected?.runId
  const workflowRecord = runId ? board.find((row) => row.latestWorkflow?.id === runId) : undefined
  const tasksPath =
    '/delivery/execution-tasks' +
    (bundle ? '?' + new URLSearchParams({ releaseBundleId: bundle.id }) : '')

  function changeFilter(value: string) {
    const next = new URLSearchParams(search)
    if (value === 'all') next.delete('judgment')
    else next.set('judgment', value)
    next.delete('releaseBundleId')
    setSearch(next, { replace: true })
  }

  const columns: TableColumnsType<(typeof candidates)[number]> = [
    {
      title: '候选版本',
      key: 'version',
      render: (_, { bundle: item }) => {
        const context = deliveryEvidenceContext(
          board,
          item.applicationId,
          item.applicationEnvironmentId,
        )
        return (
          <div className="soha-delivery-evidence-identity">
            <Button
              className="soha-delivery-evidence-name"
              type="link"
              aria-pressed={item.id === bundle?.id}
              onClick={() => {
                const next = new URLSearchParams(search)
                next.set('releaseBundleId', item.id)
                setSearch(next, { replace: true })
              }}
            >
              {item.version || item.id}
            </Button>
            <span>{[context.application, context.environment].filter(Boolean).join(' / ')}</span>
          </div>
        )
      },
    },
    {
      title: '来源',
      key: 'source',
      render: (_, { bundle: item, runId }) => (
        <div className="soha-delivery-evidence-identity">
          {item.sourceType || '—'}
          {runId ? (
            <Text type="secondary" code>
              {runId}
            </Text>
          ) : null}
        </div>
      ),
    },
    {
      title: '验证判断',
      key: 'judgment',
      render: (_, { judgment }) => <StatusTag {...judgmentLabels[judgment]} />,
    },
    {
      ...tableColumnPresets.datetime,
      title: '更新',
      key: 'updatedAt',
      render: (_, { bundle: item }) => formatDateTime(releaseBundleUpdatedAt(item)),
    },
  ]

  return (
    <div className="soha-page soha-delivery-workbench-page">
      <h1 className="soha-delivery-evidence-sr-only">测试验证</h1>
      <ReadFeedback query={tasksQuery} label="验证任务" canRead={canViewTasks} />
      <ReadFeedback query={boardQuery} label="发布看板" canRead={canViewReleaseBoard} />
      {missingRequested ? <Text type="secondary">当前可见记录中没有找到指定版本包。</Text> : null}
      <div className={selected ? 'soha-delivery-evidence-grid is-testing' : undefined}>
        <section className="soha-delivery-evidence-panel" aria-label="候选版本">
          <EvidencePanelHeader
            title="候选版本"
            description={bundleData ? visible.length + ' 条记录' : undefined}
            actions={
              <>
                <Select
                  aria-label="按验证判断筛选"
                  value={filter}
                  options={judgmentFilters}
                  onChange={changeFilter}
                />
                {canViewTasks ? (
                  <Button icon={<ExperimentOutlined />} onClick={() => navigate(tasksPath)}>
                    验证任务
                  </Button>
                ) : null}
                <Button onClick={() => setAssistOpen(true)}>验证辅助</Button>
                {canViewBundles || canViewTasks || canViewReleaseBoard ? (
                  <ManagementRefreshButton
                    aria-label="刷新验证证据"
                    tooltip="刷新验证证据"
                    loading={
                      bundlesQuery.isFetching || tasksQuery.isFetching || boardQuery.isFetching
                    }
                    onClick={() => {
                      if (canViewBundles) void bundlesQuery.refetch()
                      if (canViewTasks) void tasksQuery.refetch()
                      if (canViewReleaseBoard) void boardQuery.refetch()
                    }}
                  />
                ) : null}
              </>
            }
          />
          <DeliveryTable
            aria-label="候选版本与验证判断"
            rowKey={(candidate) => candidate.bundle.id}
            rowClassName={(candidate: (typeof candidates)[number]) =>
              candidate.bundle.id === bundle?.id ? 'soha-delivery-evidence-row-selected' : ''
            }
            dataSource={visible}
            empty={
              <ManagementState
                bordered={false}
                compact
                kind={canViewBundles ? 'empty' : 'no-permission'}
                title={
                  !canViewBundles
                    ? '无版本包查看权限'
                    : candidates.length
                      ? '没有符合当前判断的候选版本'
                      : '暂无候选版本'
                }
                description={
                  canViewBundles && !candidates.length
                    ? '工作流生成版本包后，可在这里查看验证结果。'
                    : undefined
                }
                actions={
                  canViewBundles && candidates.length ? (
                    <Button type="link" onClick={() => changeFilter('all')}>
                      清除筛选
                    </Button>
                  ) : canViewWorkflowCenter ? (
                    <Link to="/release-board">前往工作流</Link>
                  ) : undefined
                }
              />
            }
            error={canViewBundles ? bundlesQuery.error : null}
            hasReadData={bundleData !== undefined}
            onRetry={() => void bundlesQuery.refetch()}
            loading={canViewBundles && bundlesQuery.isLoading}
            columns={columns}
            pagination={false}
            enableDensity={false}
            showColumnSettings={false}
            shellClassName="soha-delivery-evidence-table"
          />
        </section>
        {selected && bundle && detail ? (
          <section className="soha-delivery-evidence-panel" aria-label="晋级判断">
            <EvidencePanelHeader
              title="晋级判断"
              actions={<StatusTag {...judgmentLabels[selected.judgment]} />}
            />
            <div className="soha-delivery-evidence-body">
              <dl className="soha-delivery-evidence-context">
                <div>
                  <dt>应用</dt>
                  <dd>{detail.application}</dd>
                </div>
                <div>
                  <dt>环境</dt>
                  <dd>{detail.environment || '未绑定'}</dd>
                </div>
                <div>
                  <dt>版本</dt>
                  <dd>{bundle.version || bundle.id}</dd>
                </div>
                <div>
                  <dt>版本包状态</dt>
                  <dd>
                    <StatusTag value={bundle.status} />
                  </dd>
                </div>
              </dl>
              <dl className="soha-delivery-evidence-subject">
                <div>
                  <dt>交付物</dt>
                  <dd>{bundle.artifactRef ? <code>{bundle.artifactRef}</code> : '未记录'}</dd>
                </div>
                <div>
                  <dt>镜像摘要</dt>
                  <dd>{bundle.artifactDigest ? <code>{bundle.artifactDigest}</code> : '未记录'}</dd>
                </div>
              </dl>
              <ul className="soha-delivery-evidence-facts">
                <li>
                  <strong>验证任务</strong>
                  <span>
                    {!canViewTasks
                      ? '无查看权限'
                      : tasksQuery.isLoading
                        ? '读取中'
                        : tasksQuery.isError
                          ? '读取结果待核对'
                          : '关联 ' + selected.evidence.verificationTaskCount + ' 个任务'}
                  </span>
                </li>
                {workflowRecord ? (
                  <li>
                    <strong>工作流验证节点</strong>
                    <span>{workflowValidationCount(workflowRecord)} 个</span>
                  </li>
                ) : null}
                <li>
                  <strong>审批</strong>
                  <span>
                    {[selected.evidence.approvalStatus, selected.evidence.approvalRequestId]
                      .filter(Boolean)
                      .join(' / ') || '未记录'}
                  </span>
                </li>
                {selected.evidence.rollbackTaskCount ? (
                  <li>
                    <strong>回滚任务</strong>
                    <span>{selected.evidence.rollbackTaskCount} 个</span>
                  </li>
                ) : null}
                <li>
                  <strong>证据引用</strong>
                  {selected.evidence.evidenceRefs.length || selected.evidence.aiAuditRefs.length ? (
                    <div className="soha-delivery-evidence-actions">
                      {selected.evidence.evidenceRefs.length ? (
                        <EvidenceRefs label="报告" refs={selected.evidence.evidenceRefs} />
                      ) : null}
                      {selected.evidence.aiAuditRefs.length ? (
                        <EvidenceRefs label="AI 审计" refs={selected.evidence.aiAuditRefs} />
                      ) : null}
                    </div>
                  ) : (
                    <span>未记录</span>
                  )}
                </li>
              </ul>
              <div className="soha-delivery-evidence-note">
                {selected.evidence.reason ? <p>{selected.evidence.reason}</p> : null}
                <p>
                  {selected.judgment === 'passed'
                    ? '当前记录显示验证通过，晋级仍按目标环境规则和审批流程执行。'
                    : selected.judgment === 'blocked'
                      ? '当前验证记录显示阻塞，请核对相关任务和报告。'
                      : '当前证据尚不足以确认晋级。'}
                </p>
              </div>
              <div className="soha-delivery-evidence-actions">
                <Button
                  onClick={() =>
                    navigate('/delivery/release-bundles/' + encodeURIComponent(bundle.id))
                  }
                >
                  查看版本包
                </Button>
                {runId && canViewWorkflow ? (
                  <Button onClick={() => navigate('/workflows/' + encodeURIComponent(runId))}>
                    查看来源工作流
                  </Button>
                ) : null}
                {selected.judgment === 'blocked' ? (
                  <Button
                    onClick={() =>
                      navigate(
                        '/delivery/analysis?' + new URLSearchParams({ releaseBundleId: bundle.id }),
                      )
                    }
                  >
                    查看阻塞原因
                  </Button>
                ) : null}
              </div>
            </div>
          </section>
        ) : null}
      </div>
      <DeliveryAssistDrawer
        open={assistOpen}
        onClose={() => setAssistOpen(false)}
        title="AI Gateway 验证辅助"
        description="可以汇总版本、任务日志、diff 和验证证据，输出是否可晋级的建议；最终晋级仍由常规流程和审批决定。"
        skillId="delivery-tester"
        manualPath={canViewTasks ? tasksPath : undefined}
        manualTitle="手工验证"
        capabilities={[
          'delivery.release.plan',
          'delivery.release_context.diff',
          'delivery.release_bundles.list',
          'delivery.execution_tasks.list',
          'delivery.execution_logs.list',
        ]}
      />
    </div>
  )
}
