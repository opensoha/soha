import { expect, it } from 'vitest'
import { resourceCollectionPath } from './resource-collection-path'

it.each([
  {
    name: 'all namespaces removes the detail identity namespace',
    collection: '/workloads/pods',
    search: '?clusterId=a&namespace=apps',
    namespace: null,
    expected: '/workloads/pods?clusterId=a',
  },
  {
    name: 'selected namespace survives a linked resource in another namespace',
    collection: '/workloads/pods',
    search: '?clusterId=a&namespace=other&sort=name',
    namespace: 'apps',
    expected: '/workloads/pods?clusterId=a&namespace=apps&sort=name',
  },
  {
    name: 'explicit owner query is normalized without merging detail parameters',
    collection: '/workloads/jobs?clusterId=b&namespace=other&sort=name&tab=logs',
    search: '?clusterId=a&namespace=other&detailOnly=value',
    namespace: 'apps',
    expected: '/workloads/jobs?clusterId=b&namespace=apps&sort=name',
  },
  {
    name: 'explicit empty query overrides detail query',
    collection: '/workloads/jobs?',
    search: '?clusterId=a&namespace=other',
    namespace: null,
    expected: '/workloads/jobs',
  },
  {
    name: 'detail leaf parameters are removed',
    collection: '/configuration/configmaps',
    search: '?clusterId=a&namespace=apps&tab=yaml&range=60&container=init&edit=true&webhook=hook',
    namespace: 'apps',
    expected: '/configuration/configmaps?clusterId=a&namespace=apps',
  },
  {
    name: 'selected namespace is added when detail search has none',
    collection: '/workloads/pods',
    search: '?clusterId=a',
    namespace: 'apps',
    expected: '/workloads/pods?clusterId=a&namespace=apps',
  },
  {
    name: 'empty scope produces no trailing query marker',
    collection: '/cluster-resources/nodes',
    search: '?namespace=stale&tab=yaml',
    namespace: null,
    expected: '/cluster-resources/nodes',
  },
])('$name', ({ collection, search, namespace, expected }) => {
  expect(resourceCollectionPath(collection, search, namespace)).toBe(expected)
})
