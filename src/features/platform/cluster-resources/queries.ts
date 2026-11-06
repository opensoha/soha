import { queryOptions } from '@tanstack/react-query'
import { listWorkloadEvents } from '../workloads/shared/api'
import { getNodeDetail, getNodeYAML, listNamespaces, listNodes } from './api'
import { clusterResourceKeys } from './keys'
import { hasClusterScope } from './scope'
import type {
  ClusterNamespace,
  ClusterNode,
  ClusterNodeDetail,
  ClusterScope,
  NodeYAMLView,
} from './types'

function hasTarget(scope: ClusterScope, name: string) {
  return hasClusterScope(scope) && Boolean(name.trim())
}

export const nodeQueries = {
  list: (scope: ClusterScope) =>
    queryOptions<ClusterNode[]>({
      queryKey: clusterResourceKeys.nodeList(scope),
      queryFn: () => listNodes(scope),
      enabled: hasClusterScope(scope),
    }),
  detail: (scope: ClusterScope, name: string) =>
    queryOptions<ClusterNodeDetail>({
      queryKey: clusterResourceKeys.nodeDetail(scope, name),
      queryFn: () => getNodeDetail({ scope, name }),
      enabled: hasTarget(scope, name),
    }),
  events: (scope: ClusterScope, name: string, limit = 100) =>
    queryOptions({
      queryKey: clusterResourceKeys.nodeEvents(scope, name, limit),
      queryFn: async () =>
        (await listWorkloadEvents(scope, limit)).filter(
          (event) => event.involvedKind === 'Node' && event.involvedName === name,
        ),
      enabled: hasTarget(scope, name),
    }),
  yaml: (scope: ClusterScope, name: string, load = true) =>
    queryOptions<NodeYAMLView>({
      queryKey: clusterResourceKeys.nodeYAML(scope, name),
      queryFn: () => getNodeYAML({ scope, name }),
      enabled: load && hasTarget(scope, name),
    }),
}

export const namespaceQueries = {
  list: (scope: ClusterScope) =>
    queryOptions<ClusterNamespace[]>({
      queryKey: clusterResourceKeys.namespaceList(scope),
      queryFn: () => listNamespaces(scope),
      enabled: hasClusterScope(scope),
    }),
}
