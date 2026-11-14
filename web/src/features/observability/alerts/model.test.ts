import { describe, expect, it } from 'vitest'
import { alertDiagnosticPaths } from './model'
import type { AlertEvent } from './types'

describe('alert diagnostic paths', () => {
  it('preserves the trigger scope, time range, and correlation identifiers', () => {
    const event = {
      id: 'evt-1',
      sourceType: 'internal_rule',
      fingerprint: 'fp-1',
      title: 'CPU high',
      summary: 'CPU > 90%',
      severity: 'critical',
      status: 'firing',
      clusterId: 'fallback-cluster',
      namespace: 'fallback-namespace',
      labels: { app: 'api' },
      createdAt: '2026-08-13T10:00:00Z',
      updatedAt: '2026-08-13T10:05:00Z',
    } satisfies AlertEvent
    const paths = alertDiagnosticPaths(event, {
      version: 'v1',
      signal: 'metrics',
      dataSourceId: 'prom-main',
      metricKey: 'cpu_usage',
      traceId: 'trace-1',
      context: {
        version: 'v1',
        scope: {
          clusterId: 'cluster-a',
          environment: 'production',
          namespace: 'production',
          service: 'api',
          workload: 'api-v2',
        },
        timeRange: { from: '2026-08-13T09:00:00Z', to: '2026-08-13T10:00:00Z' },
        filter: { spanId: 'span-1' },
      },
    })

    for (const path of [paths.metrics, paths.traces, paths.dashboards]) {
      const params = new URL(path, 'http://soha.local').searchParams
      expect(params.get('cluster')).toBe('cluster-a')
      expect(params.get('namespace')).toBe('production')
      expect(params.get('service')).toBe('api')
      expect(params.get('workload')).toBe('api-v2')
      expect(params.get('environment')).toBe('production')
      expect(params.get('dataSourceId')).toBe('prom-main')
      expect(params.get('from')).toBe('2026-08-13T09:00:00Z')
      expect(params.get('to')).toBe('2026-08-13T10:00:00Z')
    }
    const logParams = new URL(paths.logs, 'http://soha.local').searchParams
    expect(logParams.get('cluster')).toBe('cluster-a')
    expect(logParams.get('namespace')).toBe('production')
    expect(logParams.get('service')).toBe('api')
    expect(logParams.get('workload')).toBe('api-v2')
    expect(logParams.get('environment')).toBe('production')
    expect(logParams.get('traceId')).toBe('trace-1')
    expect(logParams.get('spanId')).toBe('span-1')
    expect(logParams.get('from')).toBe('2026-08-13T09:00:00.000Z')
    expect(logParams.get('to')).toBe('2026-08-13T10:00:00.000Z')
  })
})
