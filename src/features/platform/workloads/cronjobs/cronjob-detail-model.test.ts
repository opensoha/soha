import { describe, expect, it } from 'vitest'
import {
  cronExecutionMetrics,
  readCronJobConfiguration,
  retainedCronJobs,
} from './cronjob-detail-model'
import type { CronJobChildJob, CronJobDetail } from './types'

const now = Date.parse('2026-11-01T10:00:00Z')
const execution = (name: string, state: string, start: string, end?: string): CronJobChildJob => ({
  name,
  namespace: 'team',
  active: state === 'running' ? 1 : 0,
  failed: state === 'failed' ? 1 : 0,
  succeeded: state === 'completed' ? 1 : 0,
  ageSeconds: 100,
  startTime: start,
  conditions: ['completed', 'failed'].includes(state)
    ? [
        {
          type: state === 'completed' ? 'Complete' : 'Failed',
          status: 'True',
          lastTransitionTime: end,
        },
      ]
    : undefined,
})

describe('CronJob retained executions', () => {
  it('uses native terminal conditions and time window, not failed Pod counters', () => {
    const complete = execution('done', 'completed', '2026-11-01T09:30:00Z', '2026-11-01T09:30:20Z')
    const failed = execution('failed', 'failed', '2026-11-01T09:40:00Z', '2026-11-01T09:40:40Z')
    const retry = { ...execution('retry', 'running', '2026-11-01T09:50:00Z'), failed: 5 }
    const old = execution('old', 'completed', '2026-11-01T07:30:00Z', '2026-11-01T07:30:10Z')
    const metrics = cronExecutionMetrics([complete, failed, retry, old], 60, now)
    expect(metrics).toMatchObject({
      started: 3,
      succeeded: 1,
      failed: 1,
      meanDuration: 30,
      rate: 50,
    })
    expect(metrics.points('failed').at(-1)?.value).toBe(1)
    expect(metrics.points('started').at(-1)?.value).toBe(3)
  })
  it('keeps unavailable, empty and missing native times distinct', () => {
    expect(cronExecutionMetrics(undefined, 60, now).started).toBeUndefined()
    expect(cronExecutionMetrics([], 60, now)).toMatchObject({ started: 0, succeeded: 0, failed: 0 })
    expect(cronExecutionMetrics([], 60, now).rate).toBeUndefined()
    const metrics = cronExecutionMetrics(
      [{ name: 'legacy', namespace: 'team', active: 0, failed: 2, succeeded: 0, ageSeconds: 2 }],
      60,
      now,
    )
    expect(metrics.started).toBeUndefined()
    expect(metrics.failed).toBeUndefined()
    expect(metrics.points('failed')).toEqual([])
  })
  it('leaves terminal metrics unknown when a known outcome has no terminal timestamp', () => {
    const complete = execution('done', 'completed', '2026-11-01T09:30:00Z', '2026-11-01T09:30:20Z')
    const failed = execution('failed', 'failed', '2026-11-01T09:40:00Z')
    const metrics = cronExecutionMetrics([complete, failed], 60, now)
    expect(metrics.started).toBe(2)
    expect(metrics.points('started').at(-1)?.value).toBe(2)
    expect(metrics.succeeded).toBeUndefined()
    expect(metrics.failed).toBeUndefined()
    expect(metrics.meanDuration).toBeUndefined()
    expect(metrics.rate).toBeUndefined()
    expect(metrics.points('succeeded')).toEqual([])
    expect(metrics.points('failed')).toEqual([])
    expect(metrics.points('rate')).toEqual([])
    expect(metrics.unknown).toBe(1)
  })
  it('filters wrong namespaces without using a job-name prefix', () => {
    const detail = {
      namespace: 'team',
      jobs: [
        { ...execution('unrelated-name', 'running', '2026-11-01T09:30:00Z') },
        { ...execution('cron-123', 'running', '2026-11-01T09:31:00Z'), namespace: 'other' },
      ],
    } as CronJobDetail
    expect(retainedCronJobs(detail)?.map((j) => j.name)).toEqual(['unrelated-name'])
    expect(retainedCronJobs({ namespace: 'team' } as CronJobDetail)).toEqual([])
  })
})

describe('CronJob native configuration', () => {
  const yaml =
    'apiVersion: batch/v1\nkind: CronJob\nmetadata: {name: report, namespace: team}\nstatus: {lastSuccessfulTime: "2026-11-01T09:30:20Z"}\nspec: {successfulJobsHistoryLimit: 0, failedJobsHistoryLimit: 1, startingDeadlineSeconds: 0, jobTemplate: {spec: {backoffLimit: 0, template: {spec: {containers: [{name: app, image: busybox, command: [sh, -c], args: [echo done]}]}}}}}'
  it('preserves explicit zero and native last-success time without resolving secrets', () => {
    expect(readCronJobConfiguration(yaml, 'report', 'team')).toMatchObject({
      successfulJobsHistoryLimit: 0,
      startingDeadlineSeconds: 0,
      backoffLimit: 0,
      lastSuccessfulTime: '2026-11-01T09:30:20Z',
      containers: [{ name: 'app', image: 'busybox' }],
    })
  })
  it('rejects wrong kind, name, namespace, invalid document and oversize content', () => {
    for (const content of [
      yaml.replace('kind: CronJob', 'kind: Job'),
      yaml.replace('name: report', 'name: other'),
      yaml.replace('namespace: team', 'namespace: other'),
      'spec: [invalid',
      ' '.repeat(4 * 1024 * 1024 + 1),
    ])
      expect(() => readCronJobConfiguration(content, 'report', 'team')).toThrow()
  })
})
