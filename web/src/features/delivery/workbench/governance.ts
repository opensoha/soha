import type { ExecutionTask, ReleaseBundle } from '../types'

export type GovernanceDecision = 'passed' | 'blocked' | 'pending' | 'not_run'

export interface DeliveryGovernance {
  decision: GovernanceDecision
  label: string
  reason?: string
  verificationTaskCount: number
  approvalStatus?: string
  approvalRequestId?: string
  rollbackTaskCount: number
  aiAuditRefs: string[]
  evidenceRefs: string[]
}

const failureStatuses = new Set(['failed', 'error', 'canceled', 'cancelled', 'rejected', 'blocked'])
const activeStatuses = new Set([
  'queued',
  'pending',
  'dispatching',
  'running',
  'waiting_approval',
  'pending_approval',
])
const successStatuses = new Set([
  'completed',
  'complete',
  'success',
  'succeeded',
  'passed',
  'approved',
  'ready',
])
const verificationKinds = [
  'verify',
  'test',
  'check',
  'quality_gate',
  'quality-gate',
  'validation',
  'smoke',
]

function recordValue(record: Record<string, unknown> | undefined, names: string[]) {
  if (!record) return undefined
  for (const name of names) {
    const value = record[name]
    if (typeof value === 'string' && value.trim()) return value.trim()
    if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  }
  return undefined
}

function values(record: Record<string, unknown> | undefined, names: string[]) {
  if (!record) return []
  return names.flatMap((name) => {
    const value = record[name]
    if (!Array.isArray(value)) return []
    return value
      .filter((item): item is string => typeof item === 'string' && item.trim().length > 0)
      .map((item) => item.trim())
  })
}

function isVerificationTask(task: ExecutionTask) {
  const kind = String(task.taskKind || '').toLowerCase()
  return verificationKinds.some((value) => kind.includes(value))
}

function statusOf(task: ExecutionTask) {
  return String(task.status || task.operationState?.status || '')
    .trim()
    .toLowerCase()
}

export function summarizeDeliveryGovernance(
  bundle: ReleaseBundle,
  tasks: ExecutionTask[],
): DeliveryGovernance {
  const metadata = bundle.metadata
  const relatedTasks = tasks.filter((task) => task.releaseBundleId === bundle.id)
  const verificationTasks = relatedTasks.filter(isVerificationTask)
  const taskRecords = verificationTasks
    .flatMap((task) => [task.result, task.payload])
    .filter(Boolean) as Record<string, unknown>[]
  const records = [metadata, ...taskRecords]
  const explicitConclusion = records
    .map((record) =>
      recordValue(record, [
        'testConclusion',
        'validationConclusion',
        'qualityGate',
        'conclusion',
        'gate',
      ]),
    )
    .find(Boolean)
  const explicitStatus = explicitConclusion?.toLowerCase()
  const failedTask = verificationTasks.find(
    (task) =>
      failureStatuses.has(statusOf(task)) ||
      failureStatuses.has(String(task.operationState?.failureReason || '').toLowerCase()),
  )
  const activeTask = verificationTasks.find((task) => activeStatuses.has(statusOf(task)))
  const passedTask = verificationTasks.find((task) => successStatuses.has(statusOf(task)))
  const bundleStatus = String(bundle.status || '').toLowerCase()
  const decision: GovernanceDecision =
    failedTask || ['failed', 'error', 'rejected', 'blocked'].includes(explicitStatus || '')
      ? 'blocked'
      : activeTask ||
          ['pending', 'running', 'waiting_approval', 'in_progress'].includes(explicitStatus || '')
        ? 'pending'
        : passedTask ||
            ['passed', 'pass', 'success', 'succeeded', 'approved', 'ready'].includes(
              explicitStatus || '',
            )
          ? 'passed'
          : verificationTasks.length || explicitConclusion
            ? 'pending'
            : ['failed', 'error', 'rejected', 'blocked'].includes(bundleStatus)
              ? 'blocked'
              : 'not_run'
  const approvalStatus = records
    .map((record) => recordValue(record, ['approvalStatus', 'approval_state', 'approvalState']))
    .find(Boolean)
  const approvalRequestId = records
    .map((record) =>
      recordValue(record, [
        'approvalRequestId',
        'aiGatewayApprovalRequestId',
        'gatewayApprovalRequestId',
      ]),
    )
    .find(Boolean)
  const rollbackTaskCount = relatedTasks.filter((task) =>
    String(task.taskKind || '')
      .toLowerCase()
      .includes('rollback'),
  ).length
  const aiAuditRefs = records.flatMap((record) => [
    ...values(record, ['aiAuditRefs', 'aiGatewayAuditRefs']),
    ...(['aiGatewayAuditLogId', 'aiGatewayRunId', 'aiGatewayToolName']
      .map((name) => recordValue(record, [name]))
      .filter(Boolean) as string[]),
  ])
  const evidenceRefs = records.flatMap((record) => [
    ...values(record, ['evidenceRefs', 'evidenceIds', 'reportIds']),
    ...(['testReportId', 'validationReportId', 'reportId']
      .map((name) => recordValue(record, [name]))
      .filter(Boolean) as string[]),
  ])
  const reason =
    failedTask?.operationState?.failureMessage ||
    (failedTask?.result?.failureMessage as string | undefined) ||
    (explicitStatus && ['failed', 'rejected', 'blocked'].includes(explicitStatus)
      ? explicitConclusion
      : undefined)
  return {
    decision,
    label:
      decision === 'passed'
        ? '验证通过'
        : decision === 'blocked'
          ? '禁止晋级'
          : decision === 'pending'
            ? '验证中'
            : '未验证',
    reason,
    verificationTaskCount: verificationTasks.length,
    approvalStatus,
    approvalRequestId,
    rollbackTaskCount,
    aiAuditRefs: [...new Set(aiAuditRefs)],
    evidenceRefs: [...new Set(evidenceRefs)],
  }
}
