import {
  BookOutlined,
  BranchesOutlined,
  CopyOutlined,
  DislikeOutlined,
  LikeOutlined,
  RollbackOutlined,
  SmileOutlined,
} from '@ant-design/icons'
import type { WorkbenchSource } from '@opensoha/contracts/gen/ts/sohaapi'
import { App, Button, Popover, Space, Tag, Typography } from 'antd'
import { useState } from 'react'
import { isReadOnlyAgentRunMessage } from '../agent-run-replay'
import type { ConversationMessage } from '../conversation'
import {
  metadataAgentStatus,
  metadataSources,
  metadataToolExecutions,
  toolApprovalId,
  toolExecutionLabel,
  toolExecutionSummary,
} from '../stream-presentation'
import { MessageApproval } from './message-approval'
import { MessageSpecialist } from './message-specialist'

export function MessageActivity({
  message: item,
  hasArtifact,
  onSource,
  onBranch,
  branchBlocked,
  onSaveMemory,
  onFeedback,
  feedbackBlocked,
  onReply,
  reaction,
  onReaction,
}: {
  message: ConversationMessage
  hasArtifact: boolean
  onSource?: (source: WorkbenchSource) => void
  branchBlocked?: boolean
  onBranch?: () => void
  onSaveMemory?: () => void
  feedbackBlocked?: boolean
  onFeedback?: (disposition: 'accepted' | 'rejected') => void
  onReply?: () => void
  reaction?: string
  onReaction?: (emoji: string) => void
}) {
  const { message } = App.useApp()
  const [view, setView] = useState<'sources' | null>(null)
  const [reactionOpen, setReactionOpen] = useState(false)
  const sources = metadataSources(item.metadata)
  const tools = metadataToolExecutions(item.metadata)
  const readOnly = isReadOnlyAgentRunMessage(item)
  const pending = ['loading', 'updating'].includes(item.deliveryStatus ?? '')
  const terminal = ['succeeded', 'failed', 'cancelled'].includes(
    metadataAgentStatus(item.metadata)?.status ?? '',
  )
  if (item.role !== 'assistant' || item.metadata?.source === 'agent-runtime-queued') return null
  if (pending && tools.length === 0) return null
  return (
    <div className="soha-ai-workbench__message-activity">
      {readOnly ? <Typography.Text type="secondary">旧版任务记录，仅供查看</Typography.Text> : null}
      {!pending ? (
        <Space size={4} wrap>
          <Button
            size="small"
            type="text"
            icon={<CopyOutlined />}
            aria-label="复制回复"
            onClick={async () => {
              try {
                await navigator.clipboard.writeText(item.content)
                void message.success('已复制回复')
              } catch {
                void message.error('复制失败，请手动选择正文。')
              }
            }}
          />
          {onReply && !readOnly ? (
            <Button
              size="small"
              type="text"
              icon={<RollbackOutlined />}
              aria-label="引用回复"
              onClick={onReply}
            />
          ) : null}
          {onReaction && !readOnly ? (
            <Popover
              trigger="click"
              open={reactionOpen}
              onOpenChange={setReactionOpen}
              content={
                <Space size={2}>
                  {['❤️', '👍', '🎉', '💡', '😂'].map((emoji) => (
                    <Button
                      key={emoji}
                      type="text"
                      aria-label={`标记 ${emoji}`}
                      aria-pressed={reaction === emoji}
                      onClick={() => {
                        onReaction(emoji)
                        setReactionOpen(false)
                      }}
                    >
                      {emoji}
                    </Button>
                  ))}
                </Space>
              }
            >
              <Button
                size="small"
                type="text"
                icon={reaction ? undefined : <SmileOutlined />}
                aria-label="回复表情"
              >
                {reaction}
              </Button>
            </Popover>
          ) : null}
          {onFeedback && !readOnly ? (
            <>
              <Button
                size="small"
                type="text"
                aria-label="有帮助"
                disabled={feedbackBlocked}
                icon={<LikeOutlined />}
                onClick={() => onFeedback('accepted')}
              />
              <Button
                size="small"
                type="text"
                aria-label="有问题"
                disabled={feedbackBlocked}
                icon={<DislikeOutlined />}
                onClick={() => onFeedback('rejected')}
              />
            </>
          ) : null}
          {onSaveMemory && !readOnly ? (
            <Button
              size="small"
              type="text"
              aria-label="保存为个人记忆"
              icon={<BookOutlined />}
              onClick={onSaveMemory}
            />
          ) : null}
          {onBranch && !readOnly ? (
            <Button
              size="small"
              type="text"
              aria-label="从此处开旁聊"
              disabled={branchBlocked}
              icon={<BranchesOutlined />}
              onClick={onBranch}
            />
          ) : null}
          {sources.length > 0 ? (
            <Button
              size="small"
              type="text"
              onClick={() => setView(view === 'sources' ? null : 'sources')}
            >
              来源
            </Button>
          ) : null}
        </Space>
      ) : null}
      {view === 'sources' ? (
        <div>
          {sources.map((source) => (
            <div key={source.id}>
              <Button type="link" size="small" onClick={() => onSource?.(source)}>
                {source.title}
              </Button>
              <Typography.Paragraph type="secondary">
                {source.summary || source.kind}
              </Typography.Paragraph>
            </div>
          ))}
        </div>
      ) : null}
      {tools.length > 0 ? (
        <details
          className="soha-ai-tool-activity"
          open={tools.some(
            (tool) =>
              tool.toolName === 'change.request' ||
              ['running', 'failed', 'error'].includes(tool.status),
          )}
        >
          <summary>
            执行记录 {tools.length}
            {hasArtifact ? ' · 含生成成果' : ''}
          </summary>
          {tools.map((tool) => {
            const related = tool.output?.relatedIds as Record<string, unknown> | undefined
            const approvalId = toolApprovalId(tool)
            const interrupted = terminal && tool.status === 'running'
            const failed = interrupted || ['failed', 'error'].includes(tool.status)
            const inProgress = !terminal && tool.status === 'running'
            const duration =
              tool.startedAt && tool.completedAt
                ? Math.max(
                    0,
                    new Date(tool.completedAt).getTime() - new Date(tool.startedAt).getTime(),
                  )
                : undefined
            const input =
              tool.input?.input && typeof tool.input.input === 'object'
                ? (tool.input.input as Record<string, unknown>)
                : tool.input
            const target = [
              input?.clusterId,
              input?.namespace,
              input?.nodeName,
              input?.serviceName,
              input?.query,
              input?.title,
            ]
              .filter((value): value is string => typeof value === 'string' && !!value)
              .join(' / ')
            const label = toolExecutionLabel(tool.toolName)
            return (
              <details key={tool.id} open={inProgress || failed || !!approvalId}>
                <summary>
                  <Space size={8} wrap>
                    <span>{label}</span>
                    <Tag
                      color={
                        failed ? 'error' : inProgress ? (readOnly ? 'default' : 'processing') : 'success'
                      }
                    >
                      {failed
                        ? '失败'
                        : inProgress
                          ? (readOnly ? '历史状态 · 进行中' : '进行中')
                          : approvalId
                            ? '已提交'
                            : tool.toolName === 'agent.delegate'
                              ? '已委派'
                              : '已完成'}
                    </Tag>
                    {duration !== undefined && Number.isFinite(duration) ? (
                      <Typography.Text type="secondary">
                        {(duration / 1000).toFixed(1)} 秒
                      </Typography.Text>
                    ) : null}
                  </Space>
                </summary>
                {target ? (
                  <Typography.Paragraph type="secondary">{target}</Typography.Paragraph>
                ) : null}
                {!approvalId && (interrupted || toolExecutionSummary(tool)) ? (
                  <Typography.Paragraph type={failed ? 'danger' : 'secondary'}>
                    {interrupted ? '任务已结束，未收到该步骤的结果。' : toolExecutionSummary(tool)}
                  </Typography.Paragraph>
                ) : null}
                {approvalId ? <MessageApproval id={approvalId} readOnly={readOnly} /> : null}
                {tool.toolName === 'agent.delegate' &&
                typeof related?.childRunId === 'string' &&
                typeof related?.sessionId === 'string' ? (
                  <MessageSpecialist
                    id={related.childRunId}
                    sessionId={related.sessionId}
                    readOnly={readOnly}
                  />
                ) : null}
              </details>
            )
          })}
        </details>
      ) : null}
    </div>
  )
}
