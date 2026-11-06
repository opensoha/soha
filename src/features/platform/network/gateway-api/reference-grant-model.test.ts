import { describe, expect, it } from 'vitest'
import { grantSummaryLabel, grantSummaryParts } from './reference-grant-model'

describe('ReferenceGrant list summary', () => {
  it('keeps fields separate for tags without guessing identities for legacy values', () => {
    expect(grantSummaryParts('routes:HTTPRoute.gateway.networking.k8s.io')).toEqual({
      namespace: 'routes',
      kind: 'HTTPRoute',
      group: 'gateway.networking.k8s.io',
      name: undefined,
    })
    expect(grantSummaryParts('backends:Service/api')).toEqual({
      namespace: 'backends',
      kind: 'Service',
      group: 'core',
      name: 'api',
    })
    expect(grantSummaryParts('legacy reference')).toBeUndefined()
  })
  it('keeps the actual source namespace, kind and API group', () => {
    expect(
      grantSummaryLabel('routes:HTTPRoute.gateway.networking.k8s.io', 'from', 'All names'),
    ).toBe('routes · HTTPRoute · gateway.networking.k8s.io')
  })
  it('distinguishes named and unrestricted targets in the grant namespace', () => {
    expect(grantSummaryLabel('backends:Service/api', 'to', '全部名称')).toBe('Service · api · core')
    expect(grantSummaryLabel('backends:Service', 'to', '全部名称')).toBe(
      'Service · 全部名称 · core',
    )
  })
  it('preserves unknown groups and unrecognized DTO values without guessed identities', () => {
    expect(grantSummaryLabel('apps:Backend.custom.example/test', 'to', 'All names')).toBe(
      'Backend · test · custom.example',
    )
    expect(grantSummaryLabel('legacy reference', 'from', 'All names')).toBe('legacy reference')
  })
})
