import { describe, expect, it } from 'vitest'
import { requireClusterId, toClusterScope } from './scope'

describe('cluster resource scope', () => {
  it('creates a canonical cluster-only scope', () => {
    expect(toClusterScope(' cluster-a ')).toEqual({ clusterId: 'cluster-a', namespace: null })
    expect(toClusterScope(null)).toEqual({ clusterId: null, namespace: null })
  })

  it('rejects requests without a cluster', () => {
    expect(() => requireClusterId(toClusterScope(null))).toThrow('A cluster is required')
  })
})
