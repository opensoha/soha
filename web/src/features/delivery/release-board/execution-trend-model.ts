import {
  isDeliveryActiveStatus,
  isDeliveryFailureStatus,
  isDeliveryReadyStatus,
  normalizeDeliveryStatus,
} from '../delivery-status'
import type { BuildRecord, DeliveryBatch, WorkflowRun } from '../types'

export type ExecutionPoint = {
  id: string
  status: string
  createdAt: string
  href: string
  version?: string
  duration: number | null
}

export function executionTone(status: string) {
  const value = normalizeDeliveryStatus(status)
  if (value === 'canceled' || value === 'cancelled') return 'neutral'
  if (value.includes('approval') || value === 'partially_completed') return 'warning'
  if (isDeliveryFailureStatus(value)) return 'danger'
  if (isDeliveryReadyStatus(value)) return 'success'
  if (isDeliveryActiveStatus(value) || value === 'waiting_execution' || value === 'canceling')
    return 'primary'
  return 'neutral'
}

function timestamp(value?: string) {
  const time = Date.parse(value || '')
  return Number.isFinite(time) && time > 0 ? time : null
}

export function executionDuration(
  record: BuildRecord | WorkflowRun | DeliveryBatch,
  now: number,
): number | null {
  let start: number | null
  let end: number | null = null
  const active =
    isDeliveryActiveStatus(record.status) ||
    record.status === 'waiting_execution' ||
    record.status === 'canceling'
  if ('sourceSystem' in record) {
    start = timestamp(record.startedAt)
    end = active ? now : timestamp(record.finishedAt)
  } else {
    // A partial authorized projection cannot describe the whole workflow's duration.
    if ('rootRunId' in record && record.partialView) return null
    const nodes = 'rootRunId' in record ? record.nodes : (record.nodeRuns ?? [])
    const started = nodes.filter((node) => timestamp(node.startedAt) !== null)
    if (!started.length) return null
    start = Math.min(...started.map((node) => timestamp(node.startedAt)!))
    if (active) end = now
    else if (started.every((node) => timestamp(node.finishedAt) !== null)) {
      end = Math.max(...started.map((node) => timestamp(node.finishedAt)!))
    }
  }
  return start !== null && end !== null && end >= start ? (end - start) / 1000 : null
}

export function durationLabel(seconds: number | null, english = false) {
  if (seconds === null) return english ? 'Duration unavailable' : '耗时未知'
  if (seconds < 1) return english ? '<1 s' : '<1 秒'
  if (seconds < 60) return `${Math.round(seconds)}${english ? ' s' : ' 秒'}`
  return `${(seconds / 60).toFixed(1).replace(/\.0$/, '')}${english ? ' min' : ' 分钟'}`
}

export function executionPoints(
  records: (BuildRecord | WorkflowRun | DeliveryBatch)[],
  now: number,
): ExecutionPoint[] {
  return [...records]
    .sort((a, b) => Date.parse(a.createdAt) - Date.parse(b.createdAt) || a.id.localeCompare(b.id))
    .slice(-10)
    .map((record) => ({
      id: record.id,
      status: record.status,
      createdAt: record.createdAt,
      href: `${'sourceSystem' in record ? '/builds' : 'rootRunId' in record ? '/delivery/batches' : '/workflows'}/${encodeURIComponent(record.id)}`,
      version:
        'metadata' in record && typeof record.metadata?.imageTag === 'string'
          ? record.metadata.imageTag
          : undefined,
      duration: executionDuration(record, now),
    }))
}
