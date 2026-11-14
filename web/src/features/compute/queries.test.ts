import { describe, expect, it } from 'vitest'
import { computeQueries } from './queries'

describe('compute polling boundaries', () => {
  it('stops automatic reads after failure and resumes after a successful manual read', () => {
    for (const options of [
      computeQueries.overview(),
      computeQueries.providerInstances(),
      computeQueries.tasks(),
      computeQueries.taskLogs('virtualization', 'task'),
    ]) {
      const interval = options.refetchInterval
      expect(typeof interval).toBe('function')
      if (typeof interval !== 'function') throw new Error('Expected an interval callback')
      expect(interval({ state: { status: 'error' } } as never)).toBe(false)
      expect(interval({ state: { status: 'success' } } as never)).toBeGreaterThan(0)
    }
  })
  it('polls active tasks and stops for terminal or unknown task outcomes', () => {
    const interval = computeQueries.task('virtualization', 'task').refetchInterval
    if (typeof interval !== 'function') throw new Error('Expected a task interval callback')
    for (const status of [
      'queued',
      'running',
      'succeeded',
      'failed',
      'canceled',
      'timeout',
      'unknown',
    ]) {
      expect(
        interval({
          state: { status: 'success', data: { data: { normalizedStatus: status } } },
        } as never),
      ).toBe(['queued', 'running'].includes(status) ? 15_000 : false)
    }
  })
})
