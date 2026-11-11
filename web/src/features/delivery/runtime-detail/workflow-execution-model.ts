import { normalizeDeliveryStatus } from '../delivery-status'
import type { WorkflowRun } from '../types'

export function workflowExecutionState(status?: string) {
  const value = normalizeDeliveryStatus(status)
  switch (value) {
    case 'completed':
    case 'success':
    case 'succeeded':
      return { tone: 'success', label: '已完成', tag: 'succeeded' } as const
    case 'running':
      return { tone: 'running', label: '执行中', tag: 'running_task' } as const
    case 'preparing':
    case 'dispatching':
      return { tone: 'running', label: '准备执行', tag: 'initializing' } as const
    case 'cancelling':
    case 'canceling':
    case 'stopping':
      return { tone: 'waiting', label: '正在停止', tag: 'progressing' } as const
    case 'waiting_approval':
    case 'pending_approval':
      return { tone: 'waiting', label: '等待审批', tag: 'approval' } as const
    case 'paused':
      return { tone: 'waiting', label: '已暂停', tag: 'waiting' } as const
    case 'rejected':
      return { tone: 'error', label: '审批已拒绝', tag: 'denied' } as const
    case 'failed':
    case 'error':
    case 'timeout':
    case 'callback_timeout':
      return { tone: 'error', label: '执行失败', tag: 'failed' } as const
    case 'canceled':
    case 'cancelled':
      return { tone: 'idle', label: '已取消', tag: 'cancelled' } as const
    case 'skipped':
      return { tone: 'idle', label: '已跳过', tag: 'skipped' } as const
    case 'queued':
    case 'pending':
      return { tone: 'idle', label: '等待执行', tag: 'inactive' } as const
    default:
      return {
        tone: 'idle',
        label: value ? `状态未知 · ${status}` : '状态未知',
        tag: 'unknown',
      } as const
  }
}

export function workflowExecutionStages(run: WorkflowRun) {
  const nodes = run.nodeRuns?.length ? run.nodeRuns : undefined
  return (nodes ?? run.steps).map((item, index) => {
    const node = nodes?.[index]
    return {
      key: node?.nodeId || `step:${index}:${item.name}`,
      name: item.name,
      status: item.status,
      summary: item.summary,
      startedAt: node?.startedAt,
      finishedAt: node?.finishedAt,
      executionTaskId: node?.executionTaskId,
      buildRecordId: node?.buildRecordId,
      releaseBundleId: node?.releaseBundleId,
      state: workflowExecutionState(item.status),
    }
  })
}

export function workflowExecutionSummary(run: WorkflowRun) {
  const stages = workflowExecutionStages(run)
  const current =
    stages.find((stage) => stage.state.tone === 'running') ??
    stages.find((stage) => stage.state.tone === 'waiting') ??
    stages.find((stage) => stage.state.tone === 'error') ??
    stages.find(
      (stage) =>
        stage.state.tone === 'idle' &&
        ['queued', 'pending'].includes(normalizeDeliveryStatus(stage.status)),
    ) ??
    stages[stages.length - 1]
  return {
    stages,
    current,
    successful: stages.filter((stage) => stage.state.tone === 'success').length,
    failed: stages.filter((stage) => stage.state.tone === 'error').length,
  }
}

export function workflowExecutionIsLive(status?: string) {
  return [
    'pending',
    'queued',
    'preparing',
    'dispatching',
    'running',
    'waiting_approval',
    'pending_approval',
    'paused',
    'cancelling',
    'canceling',
    'stopping',
  ].includes(normalizeDeliveryStatus(status))
}

export function workflowCanReview(run: WorkflowRun) {
  return (
    normalizeDeliveryStatus(run.status) === 'waiting_approval' && run.scope !== 'delivery_batch'
  )
}
