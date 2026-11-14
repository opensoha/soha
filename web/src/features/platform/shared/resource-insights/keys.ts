import type { ScopeKey } from '@/types'
import type { ResourceGraphRootAPI } from './api'

function scopeKey(scope: ScopeKey) {
  return { clusterId: scope.clusterId?.trim() ?? '', namespace: scope.namespace?.trim() ?? '' }
}

export const resourceInsightKeys = {
  all: ['platform-resource-insights'] as const,
  graph: (
    scope: ScopeKey,
    kind: string,
    name: string,
    sessionGeneration: number,
    rootAPI?: ResourceGraphRootAPI,
  ) =>
    [
      ...resourceInsightKeys.all,
      'graph',
      scopeKey(scope),
      kind.trim(),
      name.trim(),
      sessionGeneration,
      ...(rootAPI ? [rootAPI.apiVersion, rootAPI.resource] : []),
    ] as const,
  posture: (scope: ScopeKey, limit: number) =>
    [...resourceInsightKeys.all, 'security-posture', scopeKey(scope), limit] as const,
}
