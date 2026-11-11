import { describe, expect, it } from 'vitest'
import {
  hasNamespacedWorkloadScope,
  hasWorkloadCluster,
  normalizeWorkloadScope,
  requireWorkloadClusterId,
  requireWorkloadNamespace,
} from './scope'

describe('workload scope', () => {
  it('uses the canonical ScopeKey normalization', () => {
    expect(normalizeWorkloadScope({ clusterId: ' cluster-a ', namespace: ' team-a ' })).toEqual({
      clusterId: 'cluster-a',
      namespace: 'team-a',
    })
    expect(normalizeWorkloadScope({ clusterId: null, namespace: 'ignored' })).toEqual({
      clusterId: null,
      namespace: null,
    })
  })

  it('distinguishes cluster-wide lists from namespaced details', () => {
    expect(hasWorkloadCluster({ clusterId: 'cluster-a', namespace: null })).toBe(true)
    expect(hasNamespacedWorkloadScope({ clusterId: 'cluster-a', namespace: null })).toBe(false)
    expect(hasNamespacedWorkloadScope({ clusterId: 'cluster-a', namespace: 'team-a' })).toBe(true)
  })

  it('rejects missing required scope parts', () => {
    expect(() => requireWorkloadClusterId({ clusterId: null, namespace: null })).toThrow(
      'A cluster is required',
    )
    expect(() => requireWorkloadNamespace({ clusterId: 'cluster-a', namespace: null })).toThrow(
      'A namespace is required',
    )
  })
})
