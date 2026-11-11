import { describe, expect, it } from 'vitest'
import {
  dashboardPanelAlertRulePath,
  dashboardPanelQueryInput,
  dashboardPlaybackParams,
  dashboardVariableValues,
  readDashboardPlayback,
  shiftDashboardPlayback,
} from './model'

describe('dashboard playback model', () => {
  it('keeps absolute windows and Grafana variable values shareable', () => {
    const params = new URLSearchParams(
      'from=2026-08-08T00%3A00%3A00Z&to=2026-08-08T01%3A00%3A00Z&var-namespace=production',
    )
    const window = readDashboardPlayback(params)
    const variables = dashboardVariableValues(
      [
        {
          name: 'namespace',
          type: 'custom',
          current: 'default',
          options: ['default', 'production'],
        },
      ],
      params,
    )
    expect(window).toEqual({
      from: '2026-08-08T00:00:00.000Z',
      rangeMinutes: 60,
      to: '2026-08-08T01:00:00.000Z',
    })
    expect(variables).toEqual({ namespace: 'production' })
    expect(dashboardPlaybackParams(params, window, variables).get('var-namespace')).toBe(
      'production',
    )
  })

  it('steps one fixed window and validates panel query input', () => {
    const window = {
      from: '2026-08-08T00:00:00.000Z',
      rangeMinutes: 60,
      to: '2026-08-08T01:00:00.000Z',
    }
    expect(shiftDashboardPlayback(window, 1, Date.parse('2026-08-08T03:00:00Z'))).toEqual({
      from: '2026-08-08T01:00:00.000Z',
      rangeMinutes: 60,
      to: '2026-08-08T02:00:00.000Z',
    })
    expect(dashboardPanelQueryInput(window, {}, 30).stepSeconds).toBe(30)
    expect(dashboardPanelQueryInput(window, {}, 0).stepSeconds).toBe(60)
  })

  it('builds an advanced alert draft with resolved custom variables', () => {
    const path = dashboardPanelAlertRulePath(
      'Kubernetes',
      'prom-main',
      {
        id: '1',
        title: 'CPU',
        type: 'timeseries',
        layout: { x: 0, y: 0, w: 12, h: 8 },
        queryable: true,
        targets: [
          {
            refId: 'A',
            expression: 'sum(rate(cpu{namespace="$namespace"}[$__rate_interval])) / $__range_s',
          },
        ],
      },
      {
        timeFrom: '2026-08-08T00:00:00Z',
        timeTo: '2026-08-08T01:00:00Z',
        stepSeconds: 60,
        variables: { namespace: 'production' },
      },
    )
    const params = new URL(path, 'http://localhost').searchParams
    expect(params.get('query')).toBe('sum(rate(cpu{namespace="production"}[4m])) / 3600')
    expect(params.get('dataSourceId')).toBe('prom-main')
    expect(params.get('windowMinutes')).toBe('60')
  })
})
