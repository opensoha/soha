import { isLegacyAgentRun, isReadOnlyAgentRunMessage } from '../agent-run-replay'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  EditOutlined,
  ExclamationCircleOutlined,
  FileTextOutlined,
  MinusCircleOutlined,
  PictureOutlined,
  SafetyCertificateOutlined,
  UnorderedListOutlined,
  UserOutlined,
} from '@ant-design/icons'
import type {
  PersonalEnvironment,
  PersonalEnvironmentActionRequest,
  PersonalEnvironmentDataDeletionRequest,
  PersonalEnvironmentTemplate,
  WorkbenchSource,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useMutation, useQuery } from '@tanstack/react-query'
import {
  App,
  Badge,
  Button,
  Dropdown,
  Input,
  Modal,
  Segmented,
  Select,
  Space,
  Typography,
} from 'antd'
import type { ReactNode } from 'react'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { observeQueries } from '../../observe/queries'
import { agentConnectionUnavailableReason } from '../agent-selection'
import type { PersonalEnvironmentDataExportRequest } from '../api'
import { workbenchApi } from '../api'
import type { WorkbenchArtifactEntry } from '../artifacts'
import {
  activityDateLabel,
  activitySummary,
  buildAssistantActivity,
  conciseActivityText,
  type AssistantActivity,
} from '../assistant-activity'
import { taskUsageSummary } from '../context-selection'
import type { ConversationMessage } from '../conversation'
import { modeLabel } from '../mode'
import {
  metadataSources,
  metadataToolExecutions,
  toolApprovalId,
  toolExecutionLabel,
  toolExecutionSummary,
} from '../stream-presentation'
import type {
  PersonalArtifact,
  PersonalAssistant,
  WorkbenchAgentProvider,
  WorkbenchAgentRun,
  WorkbenchSession,
} from '../types'
import { AssistantAvatar } from './assistant-avatar'
import { ContextRows } from './context-usage'
import { WorkbenchMarkdown } from './markdown'
import { MessageApproval } from './message-approval'
import { PersonalWorkspace } from './personal-workspace'

export type AssistantTab = 'activity' | 'results' | 'approvals' | 'upcoming' | 'identity'

const environmentNames: Record<PersonalEnvironment['state'], string> = {
  unprovisioned: '未开通',
  pending: '正在处理',
  ready: '就绪',
  sleeping: '休眠',
  missing: '环境缺失',
  expired: '租约已到期',
  unknown: '状态未确认',
  quota_exceeded: '配额已耗尽',
  deleting: '正在删除数据',
  deletion_unknown: '数据删除结果未确认',
  deleted: '持久数据已删除',
}

function executionIcon(status: string) {
  if (['failed', 'error'].includes(status)) return <ExclamationCircleOutlined aria-hidden="true" />
  if (['cancelled', 'canceled'].includes(status)) return <MinusCircleOutlined aria-hidden="true" />
  if (['completed', 'success', 'succeeded'].includes(status))
    return <CheckCircleOutlined aria-hidden="true" />
  return <ClockCircleOutlined aria-hidden="true" />
}

export function AssistantPanel({
  tab,
  onTabChange,
  provider,
  session,
  messages,
  runs,
  artifacts,
  loading,
  onArtifact,
  onSettings,
  onTools,
  assistant,
  connectionState,
  environment,
  workspaceOwner,
  environmentError,
  environmentLoading,
  onRetryEnvironment,
  environmentTemplates = [],
  environmentTemplatesError,
  environmentTemplatesLoading,
  onRetryEnvironmentTemplates,
  environmentChanging,
  environmentUncertain,
  environmentFeedback,
  onChangeEnvironment,
  canRetryDeletion,
  onDeleteEnvironment,
  environmentExporting,
  onExportEnvironment,
  onCancelEnvironmentExport,
  personalRuns,
  tasksError,
  onRetryTasks,
  onSession,
  onProfilePrompt,
  avatarBusy,
  renaming,
  personalArtifacts,
  artifactsError,
  artifactsLoading,
  onRetryArtifacts,
  onPersonalArtifact,
  onSource,
}: {
  tab: AssistantTab
  onTabChange: (tab: AssistantTab) => void
  provider?: WorkbenchAgentProvider
  session?: WorkbenchSession
  messages: ConversationMessage[]
  runs: WorkbenchAgentRun[]
  artifacts: WorkbenchArtifactEntry[]
  loading: boolean
  onArtifact: (key: string) => void
  onSettings: () => void
  onTools: () => void
  assistant?: PersonalAssistant
  connectionState: 'checking' | 'connected' | 'unknown'
  environment?: PersonalEnvironment
  workspaceOwner?: string
  environmentError?: boolean
  environmentLoading?: boolean
  onRetryEnvironment?: () => void
  environmentTemplates?: PersonalEnvironmentTemplate[]
  environmentTemplatesError?: boolean
  environmentTemplatesLoading?: boolean
  onRetryEnvironmentTemplates?: () => void
  environmentUncertain?: boolean
  environmentFeedback?: ReactNode
  environmentChanging?: boolean
  onChangeEnvironment?: (input: PersonalEnvironmentActionRequest) => void
  canRetryDeletion?: boolean
  onDeleteEnvironment?: (input: PersonalEnvironmentDataDeletionRequest) => Promise<unknown>
  environmentExporting?: boolean
  onExportEnvironment?: (input: PersonalEnvironmentDataExportRequest) => void
  onCancelEnvironmentExport?: () => void
  personalRuns: WorkbenchAgentRun[]
  tasksError: boolean
  onRetryTasks: () => void
  onSession: (id: string) => void
  onProfilePrompt: (kind: 'name' | 'avatar') => void
  avatarBusy: boolean
  renaming: boolean
  personalArtifacts: PersonalArtifact[]
  artifactsError: boolean
  artifactsLoading: boolean
  onRetryArtifacts: () => void
  onPersonalArtifact: (artifact: PersonalArtifact) => void
  onSource: (source: WorkbenchSource) => void
}) {
  const { message } = App.useApp()
  const personalVMPending = environment?.backend === 'vm' && environment.state === 'pending'
  const [environmentTemplate, setEnvironmentTemplate] = useState<string>()
  const [deletionTarget, setDeletionTarget] = useState<{
    id: string
    name: string
    version: number
  }>()
  const [deletionName, setDeletionName] = useState('')
  const canDeleteData = Boolean(
    onDeleteEnvironment &&
    assistant &&
    environment?.assistantId === assistant.id &&
    !environmentError &&
    !environmentLoading &&
    !environmentChanging &&
    !environmentUncertain &&
    !renaming &&
    environment.dataInitialized &&
    (environment.state === 'sleeping' ||
      (environment.state === 'deletion_unknown' && canRetryDeletion)),
  )
  const deletionMatches = Boolean(
    canDeleteData &&
    deletionTarget &&
    deletionTarget.id === assistant?.id &&
    deletionTarget.name === assistant?.name &&
    deletionTarget.version === environment?.version &&
    deletionName === deletionTarget.name,
  )
  const selectedEnvironmentTemplate = environmentTemplates.find(
    (item) => item.id === environmentTemplate,
  )
  const [resumeInputs, setResumeInputs] = useState<Record<string, string>>({})
  const [activityId, setActivityId] = useState<string | null>(null)
  const [stepId, setStepId] = useState('overview')
  const cancel = useMutation({
    mutationFn: workbenchApi.agentRuns.cancel,
    onSuccess: () => {
      onRetryTasks()
      void message.success('已请求取消，等待执行端确认')
    },
    onError: () => void message.error('任务取消失败，请检查最新状态'),
  })
  const resume = useMutation({
    mutationFn: ({ runId, input }: { runId: string; input: string }) =>
      workbenchApi.agentRuns.resume(runId, { input }),
    onSuccess: (_, { runId }) => {
      setResumeInputs((inputs) => ({ ...inputs, [runId]: '' }))
      onRetryTasks()
      void message.success('已提交补充信息，任务将继续执行')
    },
    onError: (error: Error) => void message.error(error.message),
  })
  const snapshot = usePermissionSnapshot().data?.data
  const canReadPlans = hasPermission(snapshot, 'observe.ai.inspection.view')
  const plans = useQuery({
    ...observeQueries.operations.tasks(),
    enabled: tab === 'upcoming' && canReadPlans,
  })
  const tools = [
    ...new Map(
      [
        ...messages.flatMap((message) => metadataToolExecutions(message.metadata)),
        ...runs.flatMap((run) => run.toolExecutions ?? []),
        ...personalRuns.flatMap((run) => run.toolExecutions ?? []),
      ].map((tool) => [tool.id, tool]),
    ).values(),
  ].sort((a, b) => b.startedAt.localeCompare(a.startedAt))
  const activityRuns = [...runs, ...personalRuns]
  const taskApprovalIds = new Set<string>()
  const readOnlyApprovalIds = new Set(
    [
      ...messages
        .filter((item) => isReadOnlyAgentRunMessage(item))
        .flatMap((item) => metadataToolExecutions(item.metadata)),
      ...activityRuns.filter(isLegacyAgentRun).flatMap((run) => run.toolExecutions ?? []),
    ].map(toolApprovalId),
  )
  for (const run of activityRuns) {
    const readOnly = isLegacyAgentRun(run)
    const ids = run.output?.approvalRequestIds
    if ((!readOnly && run.providerKind !== 'dsh') || !Array.isArray(ids)) continue
    for (const id of ids
      .filter((id): id is string => typeof id === 'string' && id.length > 0)
      .slice(0, 8)) {
      taskApprovalIds.add(id)
      if (readOnly) readOnlyApprovalIds.add(id)
    }
  }
  const approvalIds = [
    ...new Set([
      ...tools.map(toolApprovalId).filter((id): id is string => Boolean(id)),
      ...taskApprovalIds,
    ]),
  ]
  const reason = agentConnectionUnavailableReason(provider)
  const connectionText =
    connectionState === 'checking'
      ? '连接中…'
      : connectionState === 'unknown'
        ? '状态未确认'
        : reason
          ? '未连接'
          : '已连接'
  const enabledPlans = (plans.data ?? []).filter((plan) => plan.enabled)
  const activity = buildAssistantActivity({
    personalRuns,
    runs,
    messages,
    artifacts,
    personalArtifacts,
    session,
  })
  const dates = new Map<string, AssistantActivity[]>()
  for (const item of activity) {
    const label = activityDateLabel(item.timestamp)
    dates.set(label, [...(dates.get(label) ?? []), item])
  }
  const groups = Array.from(dates, ([label, items]) => ({ label, items }))
  const selectedActivity = activity.find((item) => item.id === activityId)
  const selectedTool = selectedActivity?.tools.find((tool) => `tool:${tool.id}` === stepId)
  const toolSummary = selectedTool ? toolExecutionSummary(selectedTool) : ''
  const selectedChild = selectedActivity?.children.find((child) => `child:${child.id}` === stepId)
  const selectedArtifact = selectedActivity?.artifacts.find(
    (artifact) => `artifact:${artifact.id}` === stepId,
  )
  const overview = !selectedTool && !selectedChild && !selectedArtifact
  const selectedRun = selectedChild || selectedActivity?.run
  const detailTimestamp = selectedTool
    ? selectedTool.completedAt || selectedTool.startedAt
    : selectedChild
      ? selectedChild.completedAt || selectedChild.updatedAt
      : selectedArtifact
        ? selectedArtifact.createdAt
        : selectedActivity?.timestamp
  const detailSources = [
    ...new Map(
      [
        ...metadataSources(selectedRun?.output),
        ...metadataSources(selectedTool?.output),
        ...metadataSources(selectedArtifact?.current?.message.metadata),
        ...messages
          .filter((item) => selectedRun && item.metadata?.agentRunId === selectedRun.id)
          .flatMap((item) => metadataSources(item.metadata)),
      ].map((source) => [source.id, source]),
    ).values(),
  ]
  const renderDetailText = (content: string) => (
    <WorkbenchMarkdown
      content={content}
      sources={detailSources}
      onSource={(source) => {
        setActivityId(null)
        onSource(source)
      }}
    />
  )
  const openActivityArtifact = (artifact: AssistantActivity['artifacts'][number]) => {
    if (artifact.current) onArtifact(artifact.current.key)
    else onPersonalArtifact(artifact)
  }
  const renderActivity = (item: AssistantActivity) => (
    <li className="soha-ai-activity-row" data-activity-id={item.id} key={item.id}>
      <Button
        type="text"
        className="soha-ai-activity-icon"
        aria-label={`查看动态：${item.title}`}
        icon={
          item.kind === 'artifact' ||
          (!item.run && item.artifacts.length) ||
          (item.run && isLegacyAgentRun(item.run)) ? (
            <FileTextOutlined />
          ) : (
            executionIcon(item.run?.stopReason === 'unknown_outcome' ? 'failed' : item.status)
          )
        }
        onClick={() => {
          setStepId('overview')
          setActivityId(item.id)
        }}
      />
      <div className="soha-ai-activity-body">
        <strong className="soha-ai-activity-title">{item.title}</strong>
        <p className="soha-ai-activity-summary">{activitySummary(item)}</p>
        {item.timestamp ? (
          <time dateTime={item.timestamp} title={new Date(item.timestamp).toLocaleString()}>
            {new Intl.DateTimeFormat('zh-CN', { hour: 'numeric', minute: '2-digit' }).format(
              new Date(item.timestamp),
            )}
          </time>
        ) : (
          <small>时间待同步</small>
        )}
      </div>
    </li>
  )
  const renderActivityActions = (item: AssistantActivity) => {
    const run = item.run
    const artifact = selectedArtifact || item.artifacts[0]
    return (
      <div className="soha-ai-activity-actions">
        {artifact ? (
          <Button
            size="small"
            type="link"
            onClick={() => {
              setActivityId(null)
              openActivityArtifact(artifact)
            }}
          >
            查看成果
            {!selectedArtifact && item.artifacts.length > 1 ? ` · ${item.artifacts.length}` : ''}
          </Button>
        ) : null}
        {item.sessionId ? (
          <Button
            size="small"
            type="link"
            onClick={() => {
              setActivityId(null)
              onSession(item.sessionId!)
            }}
          >
            查看对话
          </Button>
        ) : null}
        {run?.phase === 'waiting_approval' ? (
          <Button
            size="small"
            type="link"
            onClick={() => {
              setActivityId(null)
              onTabChange('approvals')
            }}
          >
            查看批准
          </Button>
        ) : null}
        {run && !isLegacyAgentRun(run) && ['queued', 'running'].includes(run.status) ? (
          <Button
            size="small"
            type="text"
            disabled={cancel.isPending}
            loading={cancel.isPending && cancel.variables === run.id}
            onClick={() => cancel.mutate(run.id)}
          >
            取消任务
          </Button>
        ) : null}
      </div>
    )
  }
  const renderActivityOverview = (item: AssistantActivity) => {
    const run = item.run
    const taskUsage = taskUsageSummary(run?.output?.usage)
    const waitingInput =
      run && !isLegacyAgentRun(run) && run.phase === 'paused' && run.stopReason === 'waiting_input'
    const question = typeof run?.input?.question === 'string' ? run.input.question : ''
    const outcome =
      typeof run?.output?.summary === 'string'
        ? run.output.summary
        : typeof run?.output?.answer === 'string'
          ? run.output.answer
          : ''
    return (
      <>
        {item.analysisRuns?.length ? (
          <section>
            <h4>关联运行</h4>
            {item.analysisRuns.map((ref) => (
              <p key={`${ref.kind}:${ref.id}`}>
                {modeLabel(ref.kind)} <StatusTag value={ref.status || 'unknown'} />
              </p>
            ))}
          </section>
        ) : null}
        {taskUsage.length > 0 && (
          <section className="soha-ai-task-usage">
            <h4>任务用量</h4>
            <ContextRows items={taskUsage} />
            <p>多次模型调用的累计输入与输出，不代表上下文窗口占用。</p>
          </section>
        )}
        {run && isLegacyAgentRun(run) ? (
          <p role="status">
            旧版任务记录，仅供查看；不能继续旧执行检查点。
            {run.output?.stopReason === 'legacy_reconciliation_required' ? '执行结果待核对。' : ''}
          </p>
        ) : null}
        <h3>执行结果</h3>
        {renderDetailText(outcome || item.artifacts[0]?.artifact.summary || activitySummary(item))}
        {run?.errorMessage ? (
          <section>
            <h4>执行反馈</h4>
            {renderDetailText(run.errorMessage)}
          </section>
        ) : null}
        {question ? (
          <details className="soha-ai-activity-record">
            <summary>任务请求</summary>
            <p className="soha-ai-activity-request">{question}</p>
          </details>
        ) : null}
        {waitingInput && run ? (
          <div className="soha-ai-activity-resume">
            <p>
              {typeof run.output?.pendingQuestion === 'string'
                ? run.output.pendingQuestion
                : '等待你的补充信息'}
            </p>
            <label htmlFor={`activity-input-${run.id}`}>补充信息</label>
            <Input.TextArea
              id={`activity-input-${run.id}`}
              aria-label={`任务 ${run.id} 的补充信息`}
              value={resumeInputs[run.id] ?? ''}
              maxLength={16384}
              autoSize={{ minRows: 2, maxRows: 6 }}
              onChange={(event) =>
                setResumeInputs((inputs) => ({ ...inputs, [run.id]: event.target.value }))
              }
            />
            <Button
              size="small"
              loading={resume.isPending && resume.variables?.runId === run.id}
              disabled={resume.isPending || !resumeInputs[run.id]?.trim()}
              onClick={() => resume.mutate({ runId: run.id, input: resumeInputs[run.id] })}
            >
              补充并继续
            </Button>
          </div>
        ) : null}
      </>
    )
  }
  return (
    <div className="soha-ai-assistant-panel">
      <div className="soha-ai-assistant-profile">
        <div className="soha-ai-assistant-avatar">
          <AssistantAvatar assistant={assistant} busy={avatarBusy} />
          <Dropdown
            trigger={['click']}
            placement="bottomRight"
            disabled={!assistant || renaming}
            menu={{
              items: [
                { key: 'avatar', label: '更改形象', icon: <PictureOutlined /> },
                { key: 'name', label: '编辑昵称', icon: <EditOutlined /> },
              ],
              onClick: ({ key }) => onProfilePrompt(key === 'avatar' ? 'avatar' : 'name'),
            }}
          >
            <Button
              type="text"
              shape="circle"
              size="small"
              className="soha-ai-assistant-profile-edit"
              aria-label="编辑助手资料"
              title="编辑助手资料"
              icon={<EditOutlined />}
              disabled={!assistant || renaming}
            />
          </Dropdown>
        </div>
        <div className="soha-ai-assistant-name">
          <Typography.Title level={4}>{assistant?.name || 'heya'}</Typography.Title>
        </div>
        <Badge
          className="soha-ai-assistant-connection"
          status={connectionState === 'connected' && !reason ? 'success' : 'default'}
          text={connectionText}
          title={
            connectionState === 'checking'
              ? '正在确认助手服务连接'
              : connectionState === 'unknown'
                ? '暂时无法确认助手服务的连接状态'
                : reason || '助手服务可访问；个人工作空间状态请在「身份」查看。'
          }
        />
      </div>
      <Segmented
        block
        name="assistant-panel"
        value={tab}
        onChange={(value) => onTabChange(value as AssistantTab)}
        options={[
          {
            value: 'activity',
            icon: <UnorderedListOutlined />,
            label: <span className="soha-ai-sr-only">任务活动</span>,
            tooltip: '任务活动',
          },
          {
            value: 'results',
            icon: <FileTextOutlined />,
            label: <span className="soha-ai-sr-only">成果</span>,
            tooltip: '成果',
          },
          {
            value: 'approvals',
            icon: <SafetyCertificateOutlined />,
            label: <span className="soha-ai-sr-only">批准</span>,
            tooltip: '批准',
          },
          {
            value: 'upcoming',
            icon: <ClockCircleOutlined />,
            label: <span className="soha-ai-sr-only">即将到来</span>,
            tooltip: '即将到来',
          },
          {
            value: 'identity',
            icon: <UserOutlined />,
            label: <span className="soha-ai-sr-only">身份</span>,
            tooltip: '身份',
          },
        ]}
      />
      <div className="soha-ai-assistant-content">
        {tab === 'activity' ? (
          <div className="soha-ai-activity-feed">
            {loading ? (
              <Typography.Text type="secondary" role="status">
                正在同步任务…
              </Typography.Text>
            ) : null}
            {tasksError ? <Button onClick={onRetryTasks}>任务动态读取失败，重试</Button> : null}
            {artifactsError ? (
              <Button onClick={onRetryArtifacts}>成果动态读取失败，重试</Button>
            ) : null}
            {!loading && !activity.length && !tasksError && !artifactsError ? (
              <ManagementState
                compact
                bordered={false}
                title="还没有任务活动"
                description="主聊天与旁聊的执行进展、工具步骤和成果会汇集在这里。"
              />
            ) : null}
            {groups.map((group) => (
              <section className="soha-ai-activity-group" key={group.label}>
                <h3>{group.label}</h3>
                <ol>{group.items.map(renderActivity)}</ol>
              </section>
            ))}
          </div>
        ) : null}
        {tab === 'results' ? (
          <>
            <Typography.Text className="soha-ai-assistant-section" strong>
              你的成果
            </Typography.Text>
            {artifactsError ? <Button onClick={onRetryArtifacts}>成果读取失败，重试</Button> : null}
            {artifactsLoading ? (
              <Typography.Text type="secondary">正在同步成果…</Typography.Text>
            ) : null}
            {!artifactsError && !artifactsLoading && !personalArtifacts.length ? (
              <ManagementState
                compact
                bordered={false}
                title="暂时没有成果"
                description="任务生成的报告、配置草稿和分析结果会保留在原对话中。"
              />
            ) : null}
            {personalArtifacts.map((item) => (
              <div className="soha-ai-assistant-event" key={item.id}>
                <FileTextOutlined />
                <span>
                  <strong>{item.artifact.title || item.artifact.kind}</strong>
                  <small>{item.artifact.summary}</small>
                  <Button size="small" type="link" onClick={() => onPersonalArtifact(item)}>
                    查看成果
                  </Button>
                  {item.sessionId ? (
                    <Button size="small" type="link" onClick={() => onSession(item.sessionId!)}>
                      打开对话
                    </Button>
                  ) : null}
                </span>
              </div>
            ))}
          </>
        ) : null}
        {tab === 'approvals' ? (
          <>
            <Typography.Text className="soha-ai-assistant-section" strong>
              你的变更申请
            </Typography.Text>
            {approvalIds.length ? (
              approvalIds.map((id) => (
                <MessageApproval
                  key={id}
                  id={id}
                  continuesTask={taskApprovalIds.has(id)}
                  readOnly={readOnlyApprovalIds.has(id)}
                />
              ))
            ) : (
              <ManagementState
                compact
                bordered={false}
                title="没有待处理的申请"
                description="助手发起变更申请后，你可以在这里核对参数并决定是否批准。"
              />
            )}
          </>
        ) : null}
        {tab === 'upcoming' ? (
          <>
            <Typography.Text className="soha-ai-assistant-section" strong>
              已启用的计划
            </Typography.Text>
            {!canReadPlans ? (
              <ManagementState
                compact
                bordered={false}
                title="暂无计划查看权限"
                description="需要巡检任务读取权限。"
              />
            ) : plans.isError ? (
              <Button onClick={() => void plans.refetch()}>计划读取失败，重试</Button>
            ) : plans.isPending ? (
              <Typography.Text type="secondary">正在读取计划…</Typography.Text>
            ) : !enabledPlans.length ? (
              <ManagementState
                compact
                bordered={false}
                title="暂时没有计划"
                description="在 AI 工作台安排巡检后，启用的计划会显示在这里。"
              />
            ) : (
              enabledPlans.map((plan) => (
                <div className="soha-ai-assistant-event" key={plan.id}>
                  <ClockCircleOutlined />
                  <span>
                    <strong>{plan.title}</strong>
                    <small>
                      {plan.trigger?.kind === 'alert'
                        ? '按告警触发'
                        : `每 ${plan.intervalMinutes} 分钟`}
                    </small>
                  </span>
                </div>
              ))
            )}
            {canReadPlans ? <Link to="/ai-workbench/inspection">管理巡检计划</Link> : null}
          </>
        ) : null}
        {tab === 'identity' ? (
          <>
            <Typography.Text className="soha-ai-assistant-section" strong>
              你的助手
            </Typography.Text>
            <Typography.Paragraph type="secondary">
              助手名字独立于你的账号名称。主聊天与旁聊共享同一助手，各自保存消息和背景。
            </Typography.Paragraph>
            {workspaceOwner &&
            !environmentError &&
            environment?.backend === 'vm' &&
            environment.dataInitialized &&
            environment.assistantId === assistant?.id ? (
              <PersonalWorkspace
                key={`${workspaceOwner}:${environment.assistantId}`}
                owner={workspaceOwner}
                environment={environment}
                onRefresh={() => onRetryEnvironment?.()}
              />
            ) : null}
            <Typography.Paragraph>{assistant?.name || 'heya'}</Typography.Paragraph>
            <Button type="text" onClick={() => onProfilePrompt('name')}>
              在聊天中改名
            </Button>
            <dl className="soha-ai-assistant-facts">
              <dt>个人工作空间</dt>
              <dd>
                {environmentError
                  ? '读取失败'
                  : environmentLoading
                    ? '正在读取…'
                    : environment
                      ? personalVMPending
                        ? 'VM 待就绪'
                        : environmentNames[environment.state] || '状态未确认'
                      : '尚未读取'}
              </dd>
              {!environmentError && environment?.observedAt ? (
                <>
                  <dt>环境观察时间</dt>
                  <dd>{new Date(environment.observedAt).toLocaleString()}</dd>
                </>
              ) : null}
              <dt>执行器</dt>
              <dd>{provider?.name || '尚未配置'}</dd>
              <dt>运行方式</dt>
              <dd>{provider?.supportsAsync ? '后台任务' : '当前对话内执行'}</dd>
              <dt>当前对话</dt>
              <dd>
                {['main', 'personal_main'].includes(session?.metadata?.conversationKind || '')
                  ? '主聊天'
                  : session?.title || '准备中'}
              </dd>
            </dl>
            <Typography.Paragraph type="secondary">
              {environmentLoading
                ? '正在读取个人环境的最近回执。'
                : environmentError
                  ? '无法确认环境状态，请重新读取。'
                  : environment?.state === 'unprovisioned'
                    ? '个人工作空间尚未开通。后台任务由已配置的运行器执行。'
                    : environment?.state === 'deleted'
                      ? '持久数据已删除，已保留防重建记录。'
                      : ['deleting', 'deletion_unknown'].includes(environment?.state || '')
                        ? '持久数据删除尚未确认，请先核对环境。不会自动重试删除。'
                        : environment?.state === 'unknown'
                          ? '上次操作结果未确认，需核对原租约后再恢复。'
                          : personalVMPending
                            ? 'VM 已绑定，等待初始化与就绪验证。就绪前个人环境操作不可用，聊天仍可继续。'
                            : '环境状态来自服务端最近的实际回执。休眠保留持久数据。'}
            </Typography.Paragraph>
            {onRetryEnvironment ? (
              <Button
                className="soha-ai-environment-refresh"
                size="small"
                loading={environmentLoading}
                onClick={onRetryEnvironment}
              >
                刷新环境状态
              </Button>
            ) : null}
            {environment?.dataInitialized && environment.state !== 'deleted' ? (
              <Typography.Paragraph type="secondary">
                持久文件保留至你显式删除。休眠、到期和取消不会删除文件；休眠后可导出 TAR 归档，最大
                256 MiB。
              </Typography.Paragraph>
            ) : null}
            {environmentExporting ? (
              <Button onClick={onCancelEnvironmentExport}>取消导出</Button>
            ) : null}
            {!environmentError && !environmentLoading && environment && onChangeEnvironment ? (
              <div className="soha-ai-environment-controls">
                {environment.state === 'unprovisioned' ? (
                  <>
                    <Select
                      aria-label="个人环境模板"
                      placeholder="选择个人环境模板"
                      value={selectedEnvironmentTemplate?.id}
                      loading={environmentTemplatesLoading}
                      disabled={
                        environmentChanging || environmentUncertain || environmentTemplatesError
                      }
                      options={environmentTemplates.map((item) => ({
                        value: item.id,
                        label: `${item.name}（${item.backend === 'vm' ? `VM · ${item.provider?.toUpperCase() || '提供方未确认'}` : 'Docker'}）`,
                      }))}
                      onChange={setEnvironmentTemplate}
                    />
                    <Typography.Text type="secondary">
                      {environmentTemplatesError
                        ? '模板读取失败，请重新读取。'
                        : environmentTemplatesLoading
                          ? '正在读取可用模板…'
                          : !environmentTemplates.length
                            ? '暂无获准的个人环境模板，请联系管理员配置。'
                            : '模板是配置，开通后仍需节点回执确认。'}
                    </Typography.Text>
                    {selectedEnvironmentTemplate ? (
                      <Typography.Text type="secondary">
                        {selectedEnvironmentTemplate.mode === 'observe-only'
                          ? '持久数据只读'
                          : '持久数据可写'}{' '}
                        · 磁盘 {Math.round(selectedEnvironmentTemplate.quota.diskBytes / 1048576)}{' '}
                        MiB · 内存{' '}
                        {Math.round(selectedEnvironmentTemplate.quota.memoryBytes / 1048576)} MiB ·
                        租约 {selectedEnvironmentTemplate.leaseSeconds} 秒
                      </Typography.Text>
                    ) : null}
                    {environmentTemplatesError ? (
                      <Button onClick={onRetryEnvironmentTemplates}>重新读取模板</Button>
                    ) : null}
                    <Button
                      loading={environmentChanging}
                      disabled={
                        environmentUncertain ||
                        !selectedEnvironmentTemplate ||
                        environmentTemplatesError ||
                        environmentTemplatesLoading
                      }
                      onClick={() =>
                        onChangeEnvironment({
                          action: 'wake',
                          templateId: selectedEnvironmentTemplate!.id,
                        })
                      }
                    >
                      开通个人环境
                    </Button>
                  </>
                ) : !['pending', 'deleting', 'deleted'].includes(environment.state) ? (
                  <Space wrap>
                    {onExportEnvironment &&
                    environment.state === 'sleeping' &&
                    environment.dataInitialized ? (
                      <Button
                        disabled={
                          environmentChanging ||
                          environmentUncertain ||
                          environmentError ||
                          environmentLoading ||
                          environment.assistantId !== assistant?.id
                        }
                        loading={environmentExporting}
                        aria-label="导出持久文件"
                        onClick={() =>
                          onExportEnvironment({
                            assistantId: environment.assistantId,
                            environmentVersion: environment.version,
                          })
                        }
                      >
                        导出持久文件
                      </Button>
                    ) : null}
                    {['ready', 'quota_exceeded'].includes(environment.state) ? (
                      <Button
                        loading={environmentChanging}
                        disabled={environmentUncertain}
                        onClick={() => onChangeEnvironment({ action: 'sleep' })}
                      >
                        休眠环境
                      </Button>
                    ) : null}
                    {['sleeping', 'expired', 'missing'].includes(environment.state) ? (
                      <Button
                        loading={environmentChanging}
                        disabled={environmentUncertain}
                        onClick={() => onChangeEnvironment({ action: 'wake' })}
                      >
                        {environment.state === 'missing' ? '恢复环境' : '唤醒环境'}
                      </Button>
                    ) : null}
                    <Button
                      disabled={environmentChanging}
                      onClick={() => onChangeEnvironment({ action: 'reconcile' })}
                    >
                      核对环境
                    </Button>
                    {onDeleteEnvironment &&
                    ['sleeping', 'deletion_unknown'].includes(environment.state) ? (
                      <Button
                        danger
                        disabled={!canDeleteData}
                        onClick={() => {
                          if (!canDeleteData || !assistant) return
                          setDeletionName('')
                          setDeletionTarget({
                            id: assistant.id,
                            name: assistant.name,
                            version: environment.version,
                          })
                        }}
                      >
                        删除持久数据
                      </Button>
                    ) : null}
                  </Space>
                ) : null}
                {environment.state === 'missing' ? (
                  <Typography.Text type="secondary">
                    恢复需要已有持久卷，不会重新创建空数据。
                  </Typography.Text>
                ) : null}
              </div>
            ) : null}
            {!deletionTarget ? environmentFeedback : null}
            <Modal
              title="删除个人持久数据"
              open={Boolean(deletionTarget)}
              okText="确认删除数据"
              cancelText="取消"
              confirmLoading={environmentChanging}
              okButtonProps={{
                danger: true,
                disabled: !deletionMatches,
                'aria-label': '确认删除数据',
              }}
              onCancel={() => {
                if (environmentChanging) return
                setDeletionTarget(undefined)
                setDeletionName('')
              }}
              onOk={async () => {
                if (!deletionMatches || !deletionTarget || !onDeleteEnvironment) return
                try {
                  await onDeleteEnvironment({
                    assistantId: deletionTarget.id,
                    environmentVersion: deletionTarget.version,
                    confirmationName: deletionName,
                  })
                  setDeletionTarget(undefined)
                  setDeletionName('')
                } catch {
                  // Keep the selection visible; the refreshed receipt gates another explicit request.
                }
              }}
            >
              {environmentFeedback}
              <p>将永久删除「{deletionTarget?.name}」的持久文件。请先自行保留所需文件。</p>
              <p>输入助手完整名称「{deletionTarget?.name}」确认；取消不会发送删除请求。</p>
              <Input
                aria-label="确认删除数据的助手全名"
                value={deletionName}
                maxLength={60}
                disabled={environmentChanging || environmentUncertain}
                onChange={(event) => setDeletionName(event.target.value)}
              />
              {deletionTarget && !canDeleteData ? (
                <p role="status">环境或助手资料已变化，请取消并重新核对。</p>
              ) : null}
            </Modal>
            <Space wrap>
              <Button size="small" onClick={onSettings}>
                会话设置
              </Button>
              <Button size="small" onClick={onTools}>
                工具与 Skills
              </Button>
              <Link className="soha-ai-mobile-workbench" to="/ai-workbench/overview">
                AI 工作台
              </Link>
            </Space>
          </>
        ) : null}
      </div>
      <Modal
        title={
          <div className="soha-ai-activity-dialog-title">
            {selectedActivity?.status ? <StatusTag value={selectedActivity.status} /> : null}
            <span>{selectedActivity?.title || '动态详情'}</span>
          </div>
        }
        open={Boolean(selectedActivity)}
        onCancel={() => setActivityId(null)}
        centered
        width={800}
        destroyOnHidden
        closable={{ 'aria-label': '关闭动态详情' }}
        styles={{
          container: { padding: 0, borderRadius: 'var(--soha-radius-lg)' },
          close: { width: 44, height: 44 },
        }}
        classNames={{
          container: 'soha-ai-activity-dialog',
          header: 'soha-ai-activity-dialog-header',
          body: 'soha-ai-activity-dialog-body',
          footer: 'soha-ai-activity-dialog-footer',
        }}
        footer={selectedActivity ? renderActivityActions(selectedActivity) : null}
      >
        {selectedActivity ? (
          <div className="soha-ai-activity-detail-layout">
            <nav className="soha-ai-activity-steps" aria-label="动态步骤">
              <button
                type="button"
                aria-current={overview ? 'step' : undefined}
                onClick={() => setStepId('overview')}
              >
                {executionIcon(selectedActivity.status)}
                <span>任务概览</span>
              </button>
              {[...selectedActivity.tools].reverse().map((tool) => (
                <button
                  type="button"
                  key={tool.id}
                  aria-current={selectedTool?.id === tool.id ? 'step' : undefined}
                  onClick={() => setStepId(`tool:${tool.id}`)}
                >
                  {executionIcon(tool.status)}
                  <span>{toolExecutionLabel(tool.toolName)}</span>
                </button>
              ))}
              {selectedActivity.children.map((child) => (
                <button
                  type="button"
                  key={child.id}
                  aria-current={selectedChild?.id === child.id ? 'step' : undefined}
                  onClick={() => setStepId(`child:${child.id}`)}
                >
                  {executionIcon(child.status)}
                  <span>委派专业助手</span>
                </button>
              ))}
              {selectedActivity.artifacts.map((artifact) => (
                <button
                  type="button"
                  key={artifact.id}
                  aria-current={selectedArtifact?.id === artifact.id ? 'step' : undefined}
                  onClick={() => setStepId(`artifact:${artifact.id}`)}
                >
                  <FileTextOutlined aria-hidden="true" />
                  <span>{conciseActivityText(artifact.artifact.title || '生成成果', 24)}</span>
                </button>
              ))}
            </nav>
            <div className="soha-ai-activity-detail-content" key={stepId}>
              <div className="soha-ai-activity-detail-meta">
                {selectedTool || selectedChild ? (
                  <StatusTag value={(selectedTool || selectedChild)!.status} />
                ) : null}
                {detailTimestamp && Number.isFinite(Date.parse(detailTimestamp)) ? (
                  <time dateTime={detailTimestamp}>
                    {new Date(detailTimestamp).toLocaleString()}
                  </time>
                ) : (
                  <span>时间待同步</span>
                )}
              </div>
              {selectedActivity.run?.stopReason === 'unknown_outcome' ? (
                <p className="soha-ai-activity-note">执行结果未知，请先核对原任务回执。</p>
              ) : null}
              {overview ? renderActivityOverview(selectedActivity) : null}
              {selectedTool ? (
                <>
                  <h3>{toolExecutionLabel(selectedTool.toolName)}</h3>
                  {toolSummary ? renderDetailText(toolSummary) : null}
                  <details className="soha-ai-activity-record" open={!toolSummary}>
                    <summary>调用记录</summary>
                    <pre>
                      {JSON.stringify(
                        {
                          tool: selectedTool.toolName,
                          input: selectedTool.input,
                          output: selectedTool.output,
                        },
                        null,
                        2,
                      )}
                    </pre>
                  </details>
                </>
              ) : null}
              {selectedChild ? (
                <>
                  <h3>委派专业助手</h3>
                  {renderDetailText(
                    typeof selectedChild.output?.summary === 'string'
                      ? selectedChild.output.summary
                      : '未提供委派任务结果。',
                  )}
                  {selectedChild.errorMessage ? renderDetailText(selectedChild.errorMessage) : null}
                  {typeof selectedChild.input?.question === 'string' ? (
                    <details className="soha-ai-activity-record">
                      <summary>委派请求</summary>
                      <p className="soha-ai-activity-request">{selectedChild.input.question}</p>
                    </details>
                  ) : null}
                </>
              ) : null}
              {selectedArtifact ? (
                <>
                  <h3>{selectedArtifact.artifact.title || '生成成果'}</h3>
                  {renderDetailText(selectedArtifact.artifact.summary)}
                </>
              ) : null}
            </div>
          </div>
        ) : null}
      </Modal>
    </div>
  )
}
