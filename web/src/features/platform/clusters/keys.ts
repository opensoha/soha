import { toScopeKey, type ScopeKey } from '@/types'

function normalizeClusterScope(scope: ScopeKey) {
  return toScopeKey(scope.clusterId, null)
}

export const clusterKeys = {
  all: ['platform', 'clusters'] as const,
  lists: () => [...clusterKeys.all, 'list'] as const,
  list: () => [...clusterKeys.lists()] as const,
  capabilities: () => [...clusterKeys.all, 'capabilities'] as const,
  details: () => [...clusterKeys.all, 'detail'] as const,
  detail: (scope: ScopeKey) => [...clusterKeys.details(), normalizeClusterScope(scope)] as const,
  nodes: (scope: ScopeKey) => [...clusterKeys.detail(scope), 'nodes'] as const,
  agentUpgrade: (scope: ScopeKey) => [...clusterKeys.detail(scope), 'agent-upgrade'] as const,
  logCollection: (scope: ScopeKey) => [...clusterKeys.detail(scope), 'log-collection'] as const,
  logDataSources: () => [...clusterKeys.all, 'log-data-sources'] as const,
}
