import { describe, expect, it } from 'vitest'
import { providerPortalKeys } from './keys'

describe('providerPortalKeys', () => {
  it('keeps every query under the stable Provider Portal root', () => {
    expect(providerPortalKeys.bootstrap()).toEqual(['provider-portal', 'bootstrap'])
    expect(providerPortalKeys.applications()).toEqual(['provider-portal', 'applications'])
    expect(providerPortalKeys.security()).toEqual(['provider-portal', 'security'])
    expect(providerPortalKeys.recent(6)).toEqual(['provider-portal', 'recent', { limit: 6 }])
  })

  it('normalizes application identifiers before creating detail keys', () => {
    expect(providerPortalKeys.application(' app-1 ')).toEqual([
      'provider-portal',
      'applications',
      'detail',
      'app-1',
    ])
  })
})
