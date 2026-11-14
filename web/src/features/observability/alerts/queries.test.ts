import { describe, expect, it } from 'vitest'
import { observabilityKeys } from '../keys'
import { observabilityAlertQueries } from './queries'

describe('alert queries', () => {
  it('uses canonical list and detail fan-out keys', () => {
    expect(observabilityAlertQueries.list().queryKey).toEqual(observabilityKeys.alerts.list())
    expect(observabilityAlertQueries.recent(8).queryKey).toEqual(observabilityKeys.alerts.recent(8))
    expect(observabilityAlertQueries.recent(8, 'cluster-a').queryKey).toEqual(
      observabilityKeys.alerts.recent(8, 'cluster-a'),
    )
    for (const options of [observabilityAlertQueries.list(), observabilityAlertQueries.recent(8)]) {
      expect(typeof options.refetchInterval).toBe('function')
      const interval = options.refetchInterval as (query: {
        state: { status: string }
      }) => number | false
      expect(interval({ state: { status: 'success' } })).toBe(15_000)
      expect(interval({ state: { status: 'error' } })).toBe(false)
      expect(interval({ state: { status: 'success' } })).toBe(15_000)
    }
    expect(observabilityAlertQueries.detail('event-1').queryKey).toEqual(
      observabilityKeys.alerts.detail('event-1'),
    )
    expect(observabilityAlertQueries.preview('event-1', 'policy-1').queryKey).toEqual(
      observabilityKeys.alerts.preview('event-1', 'policy-1'),
    )
  })
})
