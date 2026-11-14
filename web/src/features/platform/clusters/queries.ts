import type { ScopeKey } from '@/types'
import { queryOptions } from '@tanstack/react-query'
import {
  getAgentUpgradeStatus,
  getClusterDetail,
  getClusterLogCollection,
  listClusterCapabilities,
  listClusterLogDataSources,
  listClusterNodes,
  listClusters,
} from './api'
import { clusterKeys } from './keys'
import type { Cluster, ClusterDetail, Node } from './types'

function hasCluster(scope: ScopeKey) {
  return Boolean(scope.clusterId?.trim())
}

export const clusterQueries = {
  agentUpgrade: (scope: ScopeKey) =>
    queryOptions({
      queryKey: clusterKeys.agentUpgrade(scope),
      queryFn: () => getAgentUpgradeStatus({ scope }),
      enabled: hasCluster(scope),
      staleTime: 0,
      retry: false,
    }),
  list: (enabled = true) =>
    queryOptions<Cluster[]>({
      queryKey: clusterKeys.list(),
      queryFn: listClusters,
      enabled,
    }),
  capabilities: () =>
    queryOptions({
      queryKey: clusterKeys.capabilities(),
      queryFn: listClusterCapabilities,
    }),
  detail: (scope: ScopeKey) =>
    queryOptions<ClusterDetail>({
      queryKey: clusterKeys.detail(scope),
      queryFn: () => getClusterDetail({ scope }),
      enabled: hasCluster(scope),
    }),
  nodes: (scope: ScopeKey) =>
    queryOptions<Node[]>({
      queryKey: clusterKeys.nodes(scope),
      queryFn: () => listClusterNodes({ scope }),
      enabled: hasCluster(scope),
    }),
  logCollection: (scope: ScopeKey) =>
    queryOptions({
      queryKey: clusterKeys.logCollection(scope),
      queryFn: () => getClusterLogCollection(scope),
      enabled: hasCluster(scope),
      refetchInterval: (query) =>
        query.state.status === 'error'
          ? false
          : ['installing', 'stopping'].includes(query.state.data?.status ?? '')
            ? 2_000
            : false,
    }),
  logDataSources: (enabled = true) =>
    queryOptions({
      queryKey: clusterKeys.logDataSources(),
      queryFn: listClusterLogDataSources,
      enabled,
    }),
}
