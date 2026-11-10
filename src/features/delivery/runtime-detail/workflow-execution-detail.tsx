import { useDocumentHidden } from '@/utils/use-document-hidden'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Alert, App, Button, Card, Collapse, Descriptions, Space, Typography, theme } from 'antd'
import {
  ArrowLeftOutlined,
  CheckOutlined,
  ClockCircleOutlined,
  CloseOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { StatusTag } from '@/components/status-tag'
import { requestErrorMessage, useInlineApiError } from '@/components/request-feedback'
import { writeOutcomeUnknown } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { deliveryKeys } from '../keys'
import { deliveryMutations } from '../mutations'
import type { DeliveryRuntimeKind, WorkflowRun } from '../types'
import {
  workflowCanReview,
  workflowExecutionIsLive,
  workflowExecutionState,
  workflowExecutionSummary,
} from './workflow-execution-model'
import './workflow-execution-detail.css'

const { Text } = Typography

export function WorkflowExecutionDetail({
  record,
  applicationName,
  environmentName,
  sourceName,
  refreshing,
  refreshError,
  onRefresh,
  onBack,
  onEvidence,
  relatedInfo,
  artifacts,
  artifactCount,
  initialStageKey,
}: {
  record: WorkflowRun
  applicationName: string
  environmentName: string
  sourceName: string
  refreshing: boolean
  refreshError?: unknown
  onRefresh: () => Promise<WorkflowRun | undefined>
  onBack: () => void
  onEvidence?: (kind: DeliveryRuntimeKind, id: string) => void
  relatedInfo: ReactNode
  artifacts: ReactNode
  artifactCount?: number
  initialStageKey?: string
}) {
  const { token } = theme.useToken()
  const { modal } = App.useApp()
  const queryClient = useQueryClient()
  const snapshot = usePermissionSnapshot().data?.data
  const approve = useMutation(deliveryMutations.workflows.approve(queryClient))
  const reject = useMutation(deliveryMutations.workflows.reject(queryClient))
  const [selectedKey, setSelectedKey] = useState<string | null>(initialStageKey ?? null)
  const hidden = useDocumentHidden()
  const [confirming, setConfirming] = useState(false)
  const [submitting, setSubmitting] = useState<'approve' | 'reject' | null>(null)
  const [acknowledged, setAcknowledged] = useState(false)
  const [actionError, setActionError] = useState('')
  const [decisionError, setDecisionError] = useState<Error | null>(null)
  useInlineApiError(actionError ? decisionError : null)
  const decisionDialog = useRef<ReturnType<typeof modal.confirm> | null>(null)
  const state = workflowExecutionState(record.status)
  const summary = workflowExecutionSummary(record)
  const selected = summary.stages.find((stage) => stage.key === selectedKey) ?? summary.current
  const canReview =
    hasPermission(snapshot, 'delivery.application-environments.approve') &&
    workflowCanReview(record)
  const live = workflowExecutionIsLive(record.status)
  const unknown = writeOutcomeUnknown(approve.error) || writeOutcomeUnknown(reject.error)
  const submittingRequest = useRef(false)
  const actionDisabled = confirming || refreshing || !!refreshError || acknowledged || unknown

  useEffect(() => {
    setSelectedKey(initialStageKey ?? null)
  }, [initialStageKey])

  useEffect(
    () => () => {
      decisionDialog.current?.destroy()
    },
    [],
  )

  useEffect(() => {
    if (!workflowCanReview(record)) setAcknowledged(false)
  }, [record])

  async function decide(action: 'approve' | 'reject') {
    if (submittingRequest.current || unknown) return
    submittingRequest.current = true
    let saved = false
    let writeStarted = false
    setSubmitting(action)
    setActionError('')
    setDecisionError(null)
    try {
      const fresh = await onRefresh()
      if (!fresh || !workflowCanReview(fresh))
        throw new Error('执行状态已变化，请核对最新记录后再操作。')
      writeStarted = true
      await (action === 'approve' ? approve : reject).mutateAsync({
        id: record.id,
        comment:
          action === 'approve' ? 'Approved from workflow detail' : 'Rejected from workflow detail',
      })
      saved = true
      setAcknowledged(true)
      await queryClient.invalidateQueries({
        queryKey: deliveryKeys.executionHistory().slice(0, -1),
      })
      await onRefresh()
    } catch (error) {
      setDecisionError(error instanceof Error ? error : null)
      const uncertain =
        writeStarted && !saved && writeOutcomeUnknown(error instanceof Error ? error : null)
      const message = `${saved ? '审批已提交，执行记录尚未更新。' : uncertain ? '审批请求结果不明，请先核对执行记录，不要重复提交。' : '审批未提交。'}${error instanceof Error ? requestErrorMessage(error, 'zh_CN') : '请重试。'}`
      setActionError(message)
      if (!saved) {
        decisionDialog.current?.update({
          content: <Alert type={uncertain ? 'warning' : 'error'} showIcon title={message} />,
          okButtonProps: {
            'aria-label': action === 'approve' ? '确认批准' : '确认拒绝',
            loading: false,
            disabled: uncertain,
            danger: action === 'reject',
          },
        })
        throw error
      }
    } finally {
      submittingRequest.current = false
      setSubmitting(null)
    }
  }

  async function confirmDecision(action: 'approve' | 'reject') {
    if (decisionDialog.current || unknown) return
    setConfirming(true)
    decisionDialog.current = modal.confirm({
      title: action === 'approve' ? '批准本次执行？' : '拒绝本次执行？',
      content: `工作流「${record.workflowName}」 · ${environmentName}。${action === 'approve' ? '批准后工作流可以继续执行。' : '拒绝将结束本次审批。'}`,
      okText: action === 'approve' ? '确认批准' : '确认拒绝',
      cancelText: '返回',
      okButtonProps: {
        'aria-label': action === 'approve' ? '确认批准' : '确认拒绝',
        danger: action === 'reject',
      },
      onOk: () => decide(action),
      afterClose: () => {
        setConfirming(false)
        decisionDialog.current = null
      },
    })
    // Await mode keeps rejected actions in the dialog without an unhandled rejection.
    await decisionDialog.current
  }

  const feedback =
    state.tone === 'running'
      ? summary.current?.state.tone === 'running'
        ? `正在处理「${summary.current.name}」`
        : '执行状态持续更新，可选择节点查看证据。'
      : state.label === '等待审批'
        ? '工作流等待审批，审批结果确认后继续更新执行记录。'
        : state.tone === 'error'
          ? '查看失败节点及其证据，确认原因后再采取下一步操作。'
          : state.label === '已取消'
            ? '本次工作流已取消，关联任务的停止状态以任务证据为准。'
            : state.tone === 'success'
              ? '本次运行已结束，节点结果和制品保留在下方。'
              : '以下内容来自本次运行记录。'

  return (
    <div
      className="soha-page soha-workflow-execution"
      data-motion-disabled={!token.motion}
      data-motion-paused={hidden ? 'true' : 'false'}
    >
      <header className="soha-execution-identity">
        <div className="soha-execution-identity-main">
          <div className="soha-execution-heading">
            <h1>{record.workflowName || '工作流执行'}</h1>
            <StatusTag value={state.tag} label={state.label} />
          </div>
          <Space wrap size={[16, 4]}>
            <Text type="secondary">
              {applicationName} / {environmentName}
            </Text>
            <Text type="secondary">来源 · {sourceName}</Text>
          </Space>
          <Text className="soha-execution-id" type="secondary" copyable>
            {record.id}
          </Text>
          <Text type="secondary">
            记录更新于 {formatDateTime(record.updatedAt || record.createdAt)}
          </Text>
        </div>
        <Space wrap>
          <Button icon={<ArrowLeftOutlined />} onClick={onBack}>
            返回列表
          </Button>
          <Button
            icon={<ReloadOutlined />}
            loading={refreshing}
            onClick={() => void onRefresh().catch(() => {})}
          >
            刷新证据
          </Button>
        </Space>
      </header>

      {refreshError ? (
        <Alert
          type="warning"
          showIcon
          title="证据未更新，正在展示上次记录"
          description={
            refreshError instanceof Error
              ? requestErrorMessage(refreshError, 'zh_CN')
              : '刷新失败，请重试。'
          }
          action={
            <Button size="small" onClick={() => void onRefresh().catch(() => {})}>
              重试
            </Button>
          }
        />
      ) : null}
      {actionError ? <Alert type="error" showIcon title={actionError} /> : null}
      {acknowledged && workflowCanReview(record) ? (
        <Alert
          type="info"
          showIcon
          title="审批已提交，等待执行记录确认"
          description="请求受理后，执行状态仍以服务端记录为准。"
        />
      ) : null}

      <section className={`soha-execution-feedback is-${state.tone}`} aria-label="执行状态">
        <div className="soha-execution-feedback-main" role="status" aria-live="polite">
          <span className={`soha-execution-state-mark is-${state.tone}`} aria-hidden="true">
            {state.tone === 'success' ? (
              <CheckOutlined />
            ) : state.tone === 'error' ? (
              <CloseOutlined />
            ) : (
              <ClockCircleOutlined />
            )}
          </span>
          <div>
            <strong>{state.label}</strong>
            <p>{feedback}</p>
          </div>
        </div>
        <div className="soha-execution-counts">
          {summary.stages.length ? (
            <span>
              <b>{summary.successful}</b> / {summary.stages.length} 节点成功
            </span>
          ) : (
            <span>暂无节点记录</span>
          )}
          {summary.failed ? (
            <span className="soha-execution-failures">{summary.failed} 节点失败</span>
          ) : null}
          <small>
            {refreshError ? '证据更新已暂停' : live ? '运行状态自动更新' : '本次运行记录'}
          </small>
        </div>
        {canReview ? (
          <Space wrap>
            <Button
              aria-label="拒绝执行"
              disabled={actionDisabled}
              loading={submitting === 'reject'}
              danger
              onClick={() => void confirmDecision('reject')}
            >
              拒绝执行
            </Button>
            <Button
              aria-label="批准执行"
              disabled={actionDisabled}
              loading={submitting === 'approve'}
              type="primary"
              onClick={() => void confirmDecision('approve')}
            >
              批准执行
            </Button>
          </Space>
        ) : null}
      </section>

      <Card className="soha-execution-stages-panel" title="执行节点" size="small">
        {selectedKey && !summary.stages.some((stage) => stage.key === selectedKey) ? (
          <Alert
            type="warning"
            showIcon
            title="所选节点不在本次执行记录中"
            description="请从节点列表重新选择；下方保留当前执行节点的已有记录。"
          />
        ) : null}
        {summary.stages.length ? (
          <div className="soha-execution-workspace">
            <ol className="soha-execution-nodes" aria-label="本次执行节点">
              {summary.stages.map((stage, index) => (
                <li key={stage.key}>
                  <Button
                    type="text"
                    className={`soha-execution-node is-${stage.state.tone}${selected?.key === stage.key ? ' is-selected' : ''}`}
                    aria-label={`${stage.name}，${stage.state.label}`}
                    aria-pressed={selected?.key === stage.key}
                    onClick={() => setSelectedKey(stage.key)}
                  >
                    <span className="soha-execution-node-marker" aria-hidden="true">
                      {stage.state.tone === 'success' ? (
                        <CheckOutlined />
                      ) : stage.state.tone === 'error' ? (
                        <CloseOutlined />
                      ) : (
                        index + 1
                      )}
                    </span>
                    <span className="soha-execution-node-copy">
                      <strong>{stage.name}</strong>
                      <small>{stage.state.label}</small>
                    </span>
                    {state.tone === 'running' && stage.state.tone === 'running' && token.motion ? (
                      <span className="soha-execution-live-dot" aria-hidden="true" />
                    ) : null}
                  </Button>
                </li>
              ))}
            </ol>
            {selected ? (
              <section className="soha-execution-inspection" aria-label="节点证据">
                <div className="soha-execution-inspection-heading">
                  <h2>{selected.name}</h2>
                  <StatusTag value={selected.state.tag} label={selected.state.label} />
                </div>
                <p className="soha-execution-node-summary">
                  {selected.summary || '当前节点没有附加执行说明。'}
                </p>
                <Descriptions
                  column={{ xs: 1, sm: 2 }}
                  size="small"
                  items={[
                    {
                      key: 'started',
                      label: '开始时间',
                      children: selected.startedAt ? formatDateTime(selected.startedAt) : '—',
                    },
                    {
                      key: 'finished',
                      label: '结束时间',
                      children: selected.finishedAt ? formatDateTime(selected.finishedAt) : '—',
                    },
                  ]}
                />
                <Space wrap className="soha-execution-evidence-actions">
                  {selected.executionTaskId ? (
                    <Button
                      onClick={() => onEvidence?.('execution_task', selected.executionTaskId!)}
                    >
                      任务日志与结果
                    </Button>
                  ) : null}
                  {selected.buildRecordId ? (
                    <Button onClick={() => onEvidence?.('build', selected.buildRecordId!)}>
                      构建明细
                    </Button>
                  ) : null}
                  {selected.releaseBundleId ? (
                    <Button
                      onClick={() => onEvidence?.('release_bundle', selected.releaseBundleId!)}
                    >
                      产物
                    </Button>
                  ) : null}
                </Space>
              </section>
            ) : null}
          </div>
        ) : (
          <Text type="secondary">本次运行尚未返回节点记录。</Text>
        )}
      </Card>
      <Collapse
        className="soha-execution-support"
        defaultActiveKey={['artifacts']}
        items={[
          {
            key: 'artifacts',
            label: `制品与结果${artifactCount === undefined ? '' : ` · ${artifactCount}`}`,
            children: artifacts,
          },
          { key: 'related', label: '关联信息与审计', children: relatedInfo },
        ]}
      />
    </div>
  )
}
