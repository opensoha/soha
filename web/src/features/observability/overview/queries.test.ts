import { describe, expect, it } from 'vitest'
import { observabilityKeys } from '../keys'
import { observabilityOverviewQueries } from './queries'

describe('observability overview queries', () => {
  it('uses the canonical monitoring key', () => {
    expect(observabilityOverviewQueries.summary().queryKey).toEqual(
      observabilityKeys.monitoring.summary(),
    )
  })
})
