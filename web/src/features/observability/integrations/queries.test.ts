import { describe, expect, it } from 'vitest'
import { observabilityKeys } from '../keys'
import { observabilityIntegrationQueries } from './queries'

describe('alert integration queries', () => {
  it('uses the canonical integration list key', () => {
    expect(observabilityIntegrationQueries.list().queryKey).toEqual(
      observabilityKeys.integrations.list(),
    )
  })
})
