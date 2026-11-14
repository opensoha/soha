import './styles.css'
import type { ExecutionTask, ReleaseBoardEntry, ReleaseBundle } from '../types'

const BLOCKED_STATUSES = new Set([
  'failed',
  'error',
  'canceled',
  'cancelled',
  'timeout',
  'rejected',
])
const ACTIVE_STATUSES = new Set([
  'running',
  'queued',
  'pending',
  'building',
  'dispatching',
  'waiting_approval',
  'pending_approval',
])
const READY_STATUSES = new Set([
  'completed',
  'success',
  'succeeded',
  'ready',
  'verified',
  'published',
])

export const VERIFY_TASK_KINDS = new Set([
  'verify',
  'validation',
  'smoke_test',
  'check',
  'check_http',
  'check_k8s_event',
])

function normalizeStatus(value?: string) {
  return String(value || '')
    .trim()
    .toLowerCase()
}

export function isBlockedStatus(value?: string) {
  return BLOCKED_STATUSES.has(normalizeStatus(value))
}

export function isActiveStatus(value?: string) {
  return ACTIVE_STATUSES.has(normalizeStatus(value))
}

export function isReadyStatus(value?: string) {
  return READY_STATUSES.has(normalizeStatus(value))
}

export function workflowValidationCount(entry: ReleaseBoardEntry) {
  return (
    entry.latestWorkflow?.nodeRuns?.filter((node) => {
      const type = String(node.type || '').toLowerCase()
      return (
        VERIFY_TASK_KINDS.has(type) ||
        type.includes('verify') ||
        type.includes('check') ||
        type.includes('smoke')
      )
    }).length ?? 0
  )
}

export function executionTaskUpdatedAt(task: ExecutionTask) {
  return (
    task.updatedAt || task.lastHeartbeatAt || task.finishedAt || task.startedAt || task.createdAt
  )
}

export function releaseBundleUpdatedAt(bundle: ReleaseBundle) {
  return bundle.updatedAt || bundle.createdAt
}

export function sortByLatest<T>(items: T[], timeSelector: (item: T) => string | undefined) {
  return [...items].sort(
    (left, right) =>
      new Date(timeSelector(right) || 0).getTime() - new Date(timeSelector(left) || 0).getTime(),
  )
}
