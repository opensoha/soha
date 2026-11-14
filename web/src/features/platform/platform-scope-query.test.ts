import { describe, expect, it } from 'vitest'
import { buildClusterScopedPath } from './platform-scope-query'

describe('buildClusterScopedPath', () => {
  it('omits namespace when the scope targets all namespaces', () => {
    expect(buildClusterScopedPath('cluster-a', 'workloads/pods')).toBe('/clusters/cluster-a/workloads/pods')
    expect(buildClusterScopedPath('cluster-a', 'workloads/pods', '')).toBe('/clusters/cluster-a/workloads/pods')
    expect(buildClusterScopedPath('cluster-a', 'workloads/pods', '   ')).toBe('/clusters/cluster-a/workloads/pods')
  })

  it('includes namespace when a concrete namespace is selected', () => {
    expect(buildClusterScopedPath('cluster-a', 'workloads/pods', 'kube-system')).toBe(
      '/clusters/cluster-a/workloads/pods?namespace=kube-system',
    )
  })

  it('merges extra query params with the namespace filter', () => {
    expect(buildClusterScopedPath('cluster-a', 'events', 'prod', { limit: 200, source: ' live ' })).toBe(
      '/clusters/cluster-a/events?namespace=prod&limit=200&source=live',
    )
  })

  it('skips blank extra query params', () => {
    expect(buildClusterScopedPath('cluster-a', 'events', null, { limit: 200, source: '   ', empty: '' })).toBe(
      '/clusters/cluster-a/events?limit=200',
    )
  })
})
