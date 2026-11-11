import { describe, expect, it } from 'vitest'
import { observabilityKeys } from '../keys'
import { observabilityRuleQueries } from './queries'

describe('alert rule queries', () => {
  it('uses canonical list, detail and runs keys', () => {
    expect(observabilityRuleQueries.list().queryKey).toEqual(observabilityKeys.rules.list())
    expect(observabilityRuleQueries.detail('rule-1').queryKey).toEqual(
      observabilityKeys.rules.detail('rule-1'),
    )
    expect(observabilityRuleQueries.runs('rule-1').queryKey).toEqual(
      observabilityKeys.rules.runs('rule-1'),
    )
  })
})
