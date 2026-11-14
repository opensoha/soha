import { describe, expect, it } from 'vitest'
import { clusterResourceKeys } from './keys'

describe('cluster resource keys', () => {
  it('keeps node details and YAML under one normalized hierarchy', () => {
    const scope = { clusterId: ' cluster-a ', namespace: null } as const
    const detail = clusterResourceKeys.nodeDetail(scope, ' node-a ')

    expect(detail).toEqual([
      'platform',
      'cluster-resources',
      'nodes',
      'detail',
      { clusterId: 'cluster-a', namespace: null },
      'node-a',
    ])
    expect(clusterResourceKeys.nodeYAML(scope, ' node-a ').slice(0, detail.length)).toEqual(detail)
  })

  it('uses a separate namespace list prefix', () => {
    expect(clusterResourceKeys.namespaceLists()).toEqual([
      'platform',
      'cluster-resources',
      'namespaces',
      'list',
    ])
  })
})
