import { api } from '@/services/api-client'
import { buildWorkloadItemPath } from '@/features/platform/workloads/shared/paths'
import {
  getWorkloadDetail,
  getWorkloadMetrics,
  getWorkloadYAML,
  listWorkloadEvents,
  listWorkloads,
} from '@/features/platform/workloads/shared/api'
import type { WorkloadEvent } from '@/features/platform/workloads/shared/types'
import type { Job, JobDetail, JobTarget } from './types'

const jobKind = 'jobs' as const

function normalizedJobName(name: string) {
  const normalized = name.trim()
  if (!normalized) throw new Error('A job name is required')
  return normalized
}

export function listJobs(scope: JobTarget['scope']): Promise<Job[]> {
  return listWorkloads<Job>(jobKind, scope)
}

export function getJobDetail(target: JobTarget): Promise<JobDetail> {
  return getWorkloadDetail<JobDetail>(jobKind, target.scope, target.name)
}

export async function listJobEvents(target: JobTarget): Promise<WorkloadEvent[]> {
  const name = normalizedJobName(target.name)
  const events = await listWorkloadEvents(target.scope, 100)
  return events.filter(
    (event) =>
      event.involvedName === name &&
      (!event.involvedKind || event.involvedKind.toLowerCase() === 'job'),
  )
}

export async function deleteJob(target: JobTarget): Promise<void> {
  await api.delete<unknown>(buildWorkloadItemPath(jobKind, target.scope, target.name))
}

export function getJobMetrics(target: JobTarget, rangeMinutes: number) {
  return getWorkloadMetrics(jobKind, target.scope, target.name, rangeMinutes)
}
export async function getJobTemplate(target: JobTarget) {
  const yaml = await getWorkloadYAML(jobKind, target.scope, target.name)
  const { readJobTemplate } = await import('./job-detail-model')
  return readJobTemplate(yaml.content, target.name, target.scope.namespace || 'default')
}
