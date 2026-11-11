import { describe, expect, it } from 'vitest'
import { metricInput, traceInput, type SignalFilters } from './shared'

const filters: SignalFilters = {
  dataSourceId: ' prom-main ',
  limit: 100,
  metricKey: 'error_rate',
  minDurationMs: 50,
  rangeMinutes: 15,
  service: ' checkout ',
  timeFrom: '2026-08-30T00:00:00Z',
  timeTo: '2026-08-30T00:15:00Z',
  traceId: ' trace-1 ',
  workload: ' api ',
}

describe('observability signal inputs', () => {
  it('preserves the exact datasource, scope, metric and absolute window', () => {
    expect(metricInput(filters, ' cluster-a ', ' apps ')).toEqual({
      dataSourceId: 'prom-main',
      metricKey: 'error_rate',
      scope: {
        clusterId: 'cluster-a',
        namespace: 'apps',
        service: 'checkout',
        workload: 'api',
      },
      stepSeconds: 60,
      timeFrom: '2026-08-30T00:00:00.000Z',
      timeTo: '2026-08-30T00:15:00.000Z',
    })
  })

  it('preserves the exact datasource, scope, trace and absolute window', () => {
    expect(traceInput(filters, ' cluster-a ', ' apps ')).toEqual({
      dataSourceId: 'prom-main',
      limit: 100,
      minDurationMs: 50,
      scope: {
        clusterId: 'cluster-a',
        namespace: 'apps',
        service: 'checkout',
        workload: 'api',
      },
      timeFrom: '2026-08-30T00:00:00.000Z',
      timeTo: '2026-08-30T00:15:00.000Z',
      traceId: 'trace-1',
    })
  })
})
