import type {
  DeliveryApplicationBindingSummary,
  DeliveryApplicationDetail,
  ExecutionArtifact,
  ExecutionTask,
  ReleaseBoardEntry,
  ReleaseBundle,
  WorkflowRun,
  WorkflowTemplate,
} from '@/types'

type DeliveryStatusCarrier = {
  latestBuild?: { status?: string }
  latestWorkflow?: { status?: string }
  latestRelease?: { status?: string }
  latestBundle?: { status?: string }
  latestExecutionTask?: { status?: string }
}

type DeliverySignal = {
  color: string
  label: string
  value?: string
}

export const DELIVERY_READY_STATUSES = new Set(['completed', 'success', 'succeeded', 'ready', 'deployed', 'approved'])
export const DELIVERY_FAILURE_STATUSES = new Set(['failed', 'error', 'canceled', 'cancelled', 'callback_timeout', 'rejected'])
export const DELIVERY_ACTIVE_STATUSES = new Set(['queued', 'pending', 'dispatching', 'running', 'waiting_approval', 'pending_approval'])
export const DELIVERY_VALIDATION_NODE_TYPES = new Set(['check', 'check_http', 'check_k8s_event', 'smoke_test', 'verify', 'verification', 'test', 'quality_gate'])
export const EXECUTION_TASK_ACTIVE_STATUSES = new Set(['queued', 'dispatching', 'running'])

export function normalizeDeliveryStatus(value?: null | string) {
  return String(value || '').trim().toLowerCase()
}

export function isDeliveryReadyStatus(value?: null | string) {
  return DELIVERY_READY_STATUSES.has(normalizeDeliveryStatus(value))
}

export function isDeliveryFailureStatus(value?: null | string) {
  return DELIVERY_FAILURE_STATUSES.has(normalizeDeliveryStatus(value))
}

export function isDeliveryActiveStatus(value?: null | string) {
  return DELIVERY_ACTIVE_STATUSES.has(normalizeDeliveryStatus(value))
}

export function deliveryStatusesFromCarrier(record?: DeliveryStatusCarrier | null) {
  if (!record) return []
  return [
    record.latestBuild?.status,
    record.latestWorkflow?.status,
    record.latestRelease?.status,
    record.latestBundle?.status,
    record.latestExecutionTask?.status,
  ].map(normalizeDeliveryStatus).filter(Boolean)
}

export function collectDeliveryStatuses(detail?: DeliveryApplicationDetail, bindings: DeliveryStatusCarrier[] = []) {
  return [
    ...deliveryStatusesFromCarrier(detail),
    ...bindings.flatMap(deliveryStatusesFromCarrier),
  ]
}

export function summarizeDeliveryBuildSignal(
  records: DeliveryStatusCarrier[],
  options: { completedLabel?: string; configuredLabel?: string; emptyLabel?: string } = {},
): DeliverySignal {
  const statuses = records.flatMap(deliveryStatusesFromCarrier)
  if (statuses.some((status) => DELIVERY_FAILURE_STATUSES.has(status))) return { color: 'red', label: '失败待处理' }
  if (statuses.some((status) => DELIVERY_ACTIVE_STATUSES.has(status))) return { color: 'blue', label: '执行中' }
  if (records.some((record) => record.latestBuild)) return { color: 'green', label: options.completedLabel ?? '构建完成' }
  return { color: 'default', label: records.length > 0 ? (options.configuredLabel ?? '待构建') : (options.emptyLabel ?? '待接入') }
}

export function summarizeDeliveryValidationSignal(
  records: DeliveryStatusCarrier[],
  options: {
    activeLabel?: string
    emptyLabel?: string
    noValidationLabel?: string
    pendingLabel?: string
    readyLabel?: string
    validationNodes?: number
  } = {},
): DeliverySignal {
  const statuses = records.flatMap(deliveryStatusesFromCarrier)
  if (!records.length) return { color: 'default', label: options.emptyLabel ?? '未绑定环境' }
  if (statuses.some((status) => DELIVERY_FAILURE_STATUSES.has(status))) return { color: 'red', label: '阻塞' }
  if (statuses.some((status) => DELIVERY_ACTIVE_STATUSES.has(status))) return { color: 'blue', label: options.activeLabel ?? '等待执行' }
  if (options.validationNodes === 0) return { color: 'default', label: options.noValidationLabel ?? '未配置验证' }
  if (statuses.some((status) => DELIVERY_READY_STATUSES.has(status))) return { color: 'green', label: options.readyLabel ?? '可验证' }
  return { color: 'gold', label: options.pendingLabel ?? '待验证' }
}

export function countWorkflowValidationNodes(run?: WorkflowRun | null) {
  return run?.nodeRuns?.filter((item) => DELIVERY_VALIDATION_NODE_TYPES.has(normalizeDeliveryStatus(item.type))).length ?? 0
}

export function workflowTemplateValidationNodeCount(template?: WorkflowTemplate | null) {
  const nodes = template?.definition?.nodes
  if (!Array.isArray(nodes)) return 0
  return nodes.filter((node) => {
    if (!node || typeof node !== 'object') return false
    const type = String((node as { type?: unknown }).type ?? '')
    return DELIVERY_VALIDATION_NODE_TYPES.has(normalizeDeliveryStatus(type))
  }).length
}

export function releaseBoardValidationCount(entry: ReleaseBoardEntry) {
  return countWorkflowValidationNodes(entry.latestWorkflow)
}

export function releaseBoardArtifactCount(entry: ReleaseBoardEntry) {
  const taskArtifacts = entry.latestExecutionTask?.artifacts?.length ?? 0
  const bundleArtifact = entry.latestBundle?.artifactRef || entry.latestBundle?.artifactDigest ? 1 : 0
  return taskArtifacts + bundleArtifact
}

export function releaseBoardQualitySignal(entry: ReleaseBoardEntry): Required<DeliverySignal> {
  const statuses = deliveryStatusesFromCarrier(entry)
  if (statuses.some((status) => DELIVERY_FAILURE_STATUSES.has(status))) {
    return { color: 'red', label: '阻塞', value: 'blocked' }
  }
  if (entry.requiresApproval && normalizeDeliveryStatus(entry.latestWorkflow?.status).includes('approval')) {
    return { color: 'gold', label: '待审批', value: 'approval' }
  }
  if (!entry.targets?.length) {
    return { color: 'orange', label: '缺少目标', value: 'target' }
  }
  if (statuses.some((status) => DELIVERY_ACTIVE_STATUSES.has(status))) {
    return { color: 'blue', label: '执行中', value: 'running' }
  }
  if (isDeliveryReadyStatus(entry.latestRelease?.status) || isDeliveryReadyStatus(entry.latestBundle?.status)) {
    return { color: 'green', label: '可推广', value: 'ready' }
  }
  if (releaseBoardValidationCount(entry) === 0) {
    return { color: 'default', label: '待验证', value: 'validation' }
  }
  return { color: 'default', label: '待执行', value: 'pending' }
}

export function summarizeReleaseBoard(rows: ReleaseBoardEntry[]) {
  return rows.reduce(
    (summary, entry) => {
      const signal = releaseBoardQualitySignal(entry).value
      summary.total += 1
      summary.targets += entry.targets?.length ?? 0
      if (signal === 'ready') summary.ready += 1
      if (signal === 'blocked') summary.blocked += 1
      if (signal === 'running') summary.running += 1
      if (signal === 'approval') summary.approval += 1
      return summary
    },
    { approval: 0, blocked: 0, ready: 0, running: 0, targets: 0, total: 0 },
  )
}

export function canCancelExecutionTask(task?: ExecutionTask | null) {
  return task ? ['queued', 'dispatching', 'running'].includes(normalizeDeliveryStatus(task.status)) : false
}

export function canRetryExecutionTask(task?: ExecutionTask | null) {
  return task ? ['failed', 'callback_timeout', 'canceled', 'cancelled'].includes(normalizeDeliveryStatus(task.status)) : false
}

export function summarizeExecutionTaskStatus(tasks: ExecutionTask[]) {
  return tasks.reduce(
    (summary, task) => {
      const status = normalizeDeliveryStatus(task.status)
      summary.total += 1
      summary.artifacts += task.artifacts?.length ?? 0
      if (EXECUTION_TASK_ACTIVE_STATUSES.has(status)) summary.active += 1
      if (DELIVERY_FAILURE_STATUSES.has(status)) summary.blocked += 1
      if (canRetryExecutionTask(task)) summary.retryable += 1
      if (task.callbackToken && !DELIVERY_FAILURE_STATUSES.has(status)) summary.callbackReady += 1
      return summary
    },
    { active: 0, artifacts: 0, blocked: 0, callbackReady: 0, retryable: 0, total: 0 },
  )
}

export function summarizeExecutionTaskArtifacts(artifacts?: ExecutionArtifact[]) {
  if (!artifacts?.length) return '-'
  const labels = artifacts.slice(0, 2).map((item) => item.name || item.ref || item.path || item.kind).filter(Boolean)
  return `${artifacts.length} · ${labels.join(' / ')}`
}

export function summarizeReleaseBundleStatus(bundles: ReleaseBundle[]) {
  return bundles.reduce(
    (summary, bundle) => {
      const status = normalizeDeliveryStatus(bundle.status)
      summary.total += 1
      if (DELIVERY_READY_STATUSES.has(status)) summary.ready += 1
      if (DELIVERY_FAILURE_STATUSES.has(status)) summary.blocked += 1
      if (bundle.artifactRef || bundle.artifactDigest) {
        summary.artifacts += 1
      } else {
        summary.missingArtifacts += 1
      }
      return summary
    },
    { artifacts: 0, blocked: 0, missingArtifacts: 0, ready: 0, total: 0 },
  )
}

export function summarizeReleaseBundleArtifact(bundle: ReleaseBundle) {
  if (bundle.artifactRef) return bundle.artifactRef
  if (bundle.artifactDigest) return bundle.artifactDigest
  return '-'
}

export function countRuntimeArtifacts(detail?: DeliveryApplicationDetail, bundleArtifacts?: ExecutionArtifact[], taskArtifacts?: ExecutionArtifact[]) {
  const bundleArtifact = detail?.latestBundle?.artifactRef || detail?.latestBundle?.artifactDigest ? 1 : 0
  const taskEmbeddedArtifacts = detail?.latestExecutionTask?.artifacts?.length ?? 0
  return bundleArtifact + taskEmbeddedArtifacts + (bundleArtifacts?.length ?? 0) + (taskArtifacts?.length ?? 0)
}

export function runtimeValidationNodeCount(bindings: DeliveryApplicationBindingSummary[]) {
  return bindings.reduce((sum, binding) => sum + workflowTemplateValidationNodeCount(binding.workflowTemplate), 0)
}
