import { describe, expect, it } from 'vitest'
import {
  observabilityScope,
  signalSearchParams,
  traceWaterfallRows,
} from './model'

describe('observability signal model', () => {
  it('normalizes optional scope and preserves shared query context', () => {
    expect(observabilityScope(' cluster ', '', ' checkout ', '')).toEqual({
      clusterId: 'cluster',
      namespace: undefined,
      service: 'checkout',
      workload: undefined,
    })
    expect(observabilityScope(null, null)).toBeUndefined()
    expect(
      signalSearchParams(new URLSearchParams('signal=metrics&service=old'), {
        service: ' checkout ',
        workload: undefined,
        from: '2026-08-03T00:00:00Z',
      }).toString(),
    ).toBe('signal=metrics&service=checkout&from=2026-08-03T00%3A00%3A00Z')
  })

  it('builds stable trace waterfall offsets from span timestamps', () => {
    const rows = traceWaterfallRows(
      [
        {
          traceId: 'trace-1',
          spanId: 'span-1',
          operation: 'GET',
          service: 'checkout',
          durationMs: 120,
          startTime: '2026-08-03T00:00:00Z',
          tags: {},
          error: false,
        },
        {
          traceId: 'trace-1',
          spanId: 'span-2',
          parentSpanId: 'span-1',
          operation: 'POST',
          service: 'checkout',
          durationMs: 20,
          startTime: '2026-08-03T00:00:00.050Z',
          tags: {},
          error: false,
        },
      ],
      'trace-1',
    )
    expect(rows).toHaveLength(2)
    expect(rows[0]?.leftPercent).toBe(0)
    expect(rows[0]?.widthPercent).toBe(100)
    expect(rows[1]?.leftPercent).toBeCloseTo(41.67, 1)
    expect(rows[1]?.span.parentSpanId).toBe('span-1')
  })

})
