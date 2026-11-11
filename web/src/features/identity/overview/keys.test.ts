import { describe, expect, it } from 'vitest'
import { IDENTITY_OVERVIEW_AUDIT_LIMIT } from './api'
import { identityOverviewKeys } from './keys'

describe('identity overview query keys', () => {
  it('uses one stable hierarchical capability root', () => {
    expect(identityOverviewKeys.all).toEqual(['identity', 'overview'])
    expect(identityOverviewKeys.sessions()).toEqual(['identity', 'overview', 'sessions'])
    expect(identityOverviewKeys.audit()).toEqual([
      'identity',
      'overview',
      'audit',
      { limit: IDENTITY_OVERVIEW_AUDIT_LIMIT },
    ])
  })
})
