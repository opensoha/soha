import { expect, it } from 'vitest'
import { resolveNetworkDetailScope } from './scope'

it('prefers the URL resource scope and uses the selected scope for legacy links', () => {
  const selected = { clusterId: 'cluster-a', namespace: 'team-a' }
  const scope = (query: string) => resolveNetworkDetailScope(selected, new URLSearchParams(query))
  expect(scope('clusterId=cluster-b&namespace=team-b')).toEqual({
    clusterId: 'cluster-b',
    namespace: 'team-b',
  })
  expect(scope('clusterId=cluster-b')).toEqual({ clusterId: 'cluster-b', namespace: null })
  expect(scope('clusterId=cluster-a&namespace=')).toEqual({
    clusterId: 'cluster-a',
    namespace: null,
  })
  expect(scope('namespace=team-b')).toEqual({ clusterId: 'cluster-a', namespace: 'team-b' })
  expect(scope('')).toEqual(selected)
})
