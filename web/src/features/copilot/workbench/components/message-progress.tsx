import type { ConversationMessage } from '../conversation'
import {
  metadataAgentStatus,
  metadataThinkingSummary,
  metadataToolExecutions,
  toolExecutionLabel,
} from '../stream-presentation'

export function MessageProgress({ message }: { message: ConversationMessage }) {
  if (!['loading', 'updating'].includes(message.deliveryStatus ?? '')) return null
  const phase = message.metadata?.agentPhase
  const activeTool = metadataToolExecutions(message.metadata)
    .reverse()
    .find((tool) => ['running', 'queued'].includes(tool.status))
  const label =
    message.metadata?.agentStopReason === 'legacy_reconciliation_required'
      ? '旧版任务结果待核对'
      : phase === 'cancelling'
        ? '正在停止'
        : phase === 'waiting_approval'
          ? '等待审批'
          : phase === 'paused'
            ? message.metadata?.agentStopReason === 'waiting_input'
              ? '等待补充信息'
              : '等待核对执行结果'
            : phase === 'preparing'
              ? '准备执行'
              : metadataAgentStatus(message.metadata)?.status === 'queued'
                ? '等待执行'
                : activeTool
                  ? toolExecutionLabel(activeTool.toolName)
                  : phase === 'waiting_task'
                    ? '等待任务结果'
                    : metadataThinkingSummary(message.metadata) || '正在思考'
  return (
    <div className="soha-ai-message-progress" role="status" aria-live="polite">
      <span className="soha-ai-message-progress__dots" aria-hidden="true">
        <span />
        <span />
        <span />
      </span>
      <span>{label}</span>
    </div>
  )
}
