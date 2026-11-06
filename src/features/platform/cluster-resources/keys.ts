import { normalizeClusterScope } from './scope'
import type { ClusterScope } from './types'

function normalizeName(name: string) {
  return name.trim()
}

export const clusterResourceKeys = {
  all: ['platform', 'cluster-resources'] as const,
  nodes: () => [...clusterResourceKeys.all, 'nodes'] as const,
  nodeLists: () => [...clusterResourceKeys.nodes(), 'list'] as const,
  nodeList: (scope: ClusterScope) =>
    [...clusterResourceKeys.nodeLists(), normalizeClusterScope(scope)] as const,
  nodeDetails: () => [...clusterResourceKeys.nodes(), 'detail'] as const,
  nodeDetail: (scope: ClusterScope, name: string) =>
    [
      ...clusterResourceKeys.nodeDetails(),
      normalizeClusterScope(scope),
      normalizeName(name),
    ] as const,
  nodeEvents: (scope: ClusterScope, name: string, limit = 100) =>
    [...clusterResourceKeys.nodeDetail(scope, name), 'events', limit] as const,
  nodeYAML: (scope: ClusterScope, name: string) =>
    [...clusterResourceKeys.nodeDetail(scope, name), 'yaml'] as const,
  namespaces: () => [...clusterResourceKeys.all, 'namespaces'] as const,
  namespaceLists: () => [...clusterResourceKeys.namespaces(), 'list'] as const,
  namespaceList: (scope: ClusterScope) =>
    [...clusterResourceKeys.namespaceLists(), normalizeClusterScope(scope)] as const,
}
