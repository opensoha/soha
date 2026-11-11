import { describe, expect, it } from 'vitest'
import {
  formatJobDuration,
  jobCompletionTarget,
  jobDuration,
  jobState,
  readJobTemplate,
} from './job-detail-model'

describe('native Job execution facts', () => {
  it.each([
    [{ failed: 3, active: 1 }, 'running'],
    [{ failed: 3, active: 0 }, 'failures-recorded'],
    [{ succeeded: 1, completions: 1, active: 0 }, 'awaiting-result'],
    [{ conditions: [{ type: 'Complete', status: 'True' }] }, 'completed'],
    [{ conditions: [{ type: 'Failed', status: 'True' }] }, 'failed'],
    [{ conditions: [{ type: 'FailureTarget', status: 'True' }] }, 'terminating'],
    [{ conditions: [{ type: 'SuccessCriteriaMet', status: 'True' }] }, 'finishing'],
    [{ suspend: true }, 'suspended'],
    [{ conditions: [{ type: 'Failed', status: 'False' }] }, 'unknown'],
  ])('does not turn counters into terminal conditions: %j', (job, state) =>
    expect(jobState(job)).toBe(state),
  )
  it('freezes failed duration at its terminal condition and keeps missing timestamps unknown', () => {
    const job = {
      startTime: '2026-11-01T00:00:00Z',
      conditions: [{ type: 'Failed', status: 'True', lastTransitionTime: '2026-11-01T00:00:42Z' }],
    }
    expect(jobDuration(job, Date.parse('2026-11-02T00:00:00Z'))).toBe(42)
    expect(
      jobDuration({ ...job, conditions: [{ type: 'Failed', status: 'True' }] }),
    ).toBeUndefined()
    expect(jobDuration({ ...job, suspend: true, conditions: [] })).toBeUndefined()
    expect(jobCompletionTarget({ completions: 0 })).toBeUndefined()
    expect(jobCompletionTarget({ completions: 0, completionTarget: 0 })).toBe(0)
  })
  it('checks exact YAML identity and reads template references without resolving Secret data', () => {
    const yaml =
      'apiVersion: batch/v1\nkind: Job\nmetadata: {name: job, namespace: demo}\nspec: {template: {spec: {containers: [{name: worker, image: busybox, command: [sh, -c], args: [echo done]}]}}}\n'
    expect(readJobTemplate(yaml, 'job', 'demo')[0]).toMatchObject({
      name: 'worker',
      image: 'busybox',
      command: ['sh', '-c'],
      args: ['echo done'],
    })
    expect(() => readJobTemplate(yaml, 'other', 'demo')).toThrow(/identity/)
    expect(() => readJobTemplate(yaml, 'job', 'other')).toThrow(/identity/)
    expect(() => readJobTemplate('x'.repeat(4 * 1024 * 1024 + 1), 'job', 'demo')).toThrow(/size/)
  })
})

it('retains seconds for short Jobs', () => {
  expect(formatJobDuration(42)).toBe('42s')
  expect(formatJobDuration(125)).toBe('2m 5s')
})
