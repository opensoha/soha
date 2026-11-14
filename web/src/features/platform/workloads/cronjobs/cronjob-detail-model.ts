import { parseDocument } from 'yaml'
import { jobDuration, jobEndTime, jobState } from '../shared/job-execution'
import type { CronJobDetail, CronJobChildJob } from './types'

export interface CronJobConfiguration {
  lastSuccessfulTime?: string
  startingDeadlineSeconds?: number
  successfulJobsHistoryLimit?: number
  failedJobsHistoryLimit?: number
  parallelism?: number
  completions?: number
  backoffLimit?: number
  activeDeadlineSeconds?: number
  containers: { name: string; image: string; init: boolean; command: string[]; args: string[] }[]
}

export function readCronJobConfiguration(
  content: string,
  name: string,
  namespace: string,
): CronJobConfiguration {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('CronJob YAML is unavailable or exceeds the size limit')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid CronJob YAML')
  const value = doc.toJS({ maxAliasCount: 100 })
  if (
    value?.kind !== 'CronJob' ||
    value?.metadata?.name !== name ||
    value?.metadata?.namespace !== namespace ||
    !value?.spec?.jobTemplate?.spec?.template?.spec
  )
    throw new Error('CronJob YAML identity does not match the selected resource')
  const spec = value.spec,
    job = spec.jobTemplate.spec,
    pod = job.template.spec
  const number = (v: unknown) =>
    typeof v === 'number' && Number.isFinite(v) && v >= 0 ? v : undefined
  const strings = (v: unknown) =>
    Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
  return {
    lastSuccessfulTime:
      typeof value.status?.lastSuccessfulTime === 'string'
        ? value.status.lastSuccessfulTime
        : undefined,
    startingDeadlineSeconds: number(spec.startingDeadlineSeconds),
    successfulJobsHistoryLimit: number(spec.successfulJobsHistoryLimit),
    failedJobsHistoryLimit: number(spec.failedJobsHistoryLimit),
    parallelism: number(job.parallelism),
    completions: number(job.completions),
    backoffLimit: number(job.backoffLimit),
    activeDeadlineSeconds: number(job.activeDeadlineSeconds),
    containers: [true, false].flatMap((init) => {
      const list = init ? pod.initContainers : pod.containers
      return Array.isArray(list)
        ? list.map((c) => ({
            name: typeof c?.name === 'string' ? c.name : '—',
            image: typeof c?.image === 'string' ? c.image : '—',
            init,
            command: strings(c?.command),
            args: strings(c?.args),
          }))
        : []
    }),
  }
}

/** Successful detail reads include all current-UID children; Go omits an empty Jobs slice. */
export function retainedCronJobs(detail: CronJobDetail) {
  return (detail.jobs || [])
    .filter((job) => job.namespace === detail.namespace)
    .sort(
      (a, b) =>
        (Date.parse(b.startTime || '') || 0) - (Date.parse(a.startTime || '') || 0) ||
        b.name.localeCompare(a.name),
    )
}

export function cronExecutionMetrics(
  jobs: CronJobChildJob[] | undefined,
  rangeMinutes: number,
  now: number,
) {
  const from = now - rangeMinutes * 60_000
  const inWindow = (v: number) => Number.isFinite(v) && v >= from && v <= now
  const completeStarts = jobs?.every((j) => Number.isFinite(Date.parse(j.startTime || '')))
  const starts = jobs?.filter((j) => inWindow(Date.parse(j.startTime || '')))
  const terminalJobs = jobs?.flatMap((job) => {
    const state = jobState(job)
    return state === 'failed' || state === 'completed'
      ? [{ job, state, end: Date.parse(jobEndTime(job) || '') }]
      : []
  })
  const completeOutcomes = completeStarts && terminalJobs?.every(({ end }) => Number.isFinite(end))
  const ends = completeOutcomes ? terminalJobs?.filter(({ end }) => inWindow(end)) : undefined
  const succeeded = ends?.filter((j) => j.state === 'completed').length
  const failed = ends?.filter((j) => j.state === 'failed').length
  const durations = ends
    ?.flatMap(({ job, end }) => {
      const value = jobDuration(job, now)
      return value === undefined ? [] : [{ timestamp: new Date(end).toISOString(), value }]
    })
    .sort((a, b) => a.timestamp.localeCompare(b.timestamp))
  const meanDuration = durations?.length
    ? durations.reduce((sum, p) => sum + p.value, 0) / durations.length
    : undefined
  const count = ends?.length
  const points = (key: 'started' | 'succeeded' | 'failed' | 'rate') =>
    Array.from({ length: 13 }, (_, i) => {
      const end = from + ((now - from) * i) / 12
      const terminal = ends?.filter((j) => j.end <= end)
      const wins = terminal?.filter((j) => j.state === 'completed').length || 0
      const value =
        key === 'started'
          ? starts?.filter((j) => Date.parse(j.startTime!) <= end).length
          : key === 'succeeded'
            ? wins
            : key === 'failed'
              ? terminal?.filter((j) => j.state === 'failed').length
              : terminal?.length
                ? (wins / terminal.length) * 100
                : undefined
      return { timestamp: new Date(end).toISOString(), value }
    }).filter(
      (p): p is { timestamp: string; value: number } =>
        p.value !== undefined && Boolean(key === 'started' ? completeStarts : completeOutcomes),
    )
  return {
    started: completeStarts ? starts?.length : undefined,
    succeeded,
    failed,
    meanDuration,
    durations,
    rate: count ? (succeeded! / count) * 100 : undefined,
    points,
    unknown: jobs?.filter((job) => {
      const state = jobState(job)
      return (
        !Number.isFinite(Date.parse(job.startTime || '')) ||
        state === 'unknown' ||
        ((state === 'failed' || state === 'completed') &&
          !Number.isFinite(Date.parse(jobEndTime(job) || '')))
      )
    }).length,
  }
}
