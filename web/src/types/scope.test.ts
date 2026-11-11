import { describe, expect, it } from 'vitest'
import { toScopeKey } from './scope'

describe('toScopeKey', () => {
  it('normalizes whitespace and preserves a selected scope', () => {
    expect(toScopeKey(' cluster-a ', ' namespace-a ')).toEqual({
      clusterId: 'cluster-a',
      namespace: 'namespace-a',
    })
  })

  it('uses null for missing values and clears namespace without a cluster', () => {
    expect(toScopeKey(undefined, 'namespace-a')).toEqual({
      clusterId: null,
      namespace: null,
    })
    expect(toScopeKey('cluster-a', ' ')).toEqual({
      clusterId: 'cluster-a',
      namespace: null,
    })
  })
})
