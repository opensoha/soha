import { api } from '@/services/api-client'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import type { ApiResponse, ScopeKey } from '@/types'
import { normalizeNetworkScope, requireNetworkClusterId } from '../shared/scope'
import type { NetworkTopologyView } from './types'

export async function getNetworkTopology(
  scope: ScopeKey,
  signal?: AbortSignal,
): Promise<NetworkTopologyView> {
  const normalized = normalizeNetworkScope(scope)
  const response = await api.get<ApiResponse<NetworkTopologyView>>(
    buildClusterScopedPath(
      requireNetworkClusterId(normalized),
      'network/topology',
      normalized.namespace,
    ),
    { signal },
  )
  return response.data
}
