import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import { buildNetworkItemPath, buildNetworkListPath, buildNetworkYAMLPath } from './paths'
import type { NetworkKind, NetworkTarget, NetworkYAML, UpdateNetworkYAMLVariables } from './types'

export async function listNetworkResources<T>(
  kind: NetworkKind,
  scope: NetworkTarget['scope'],
): Promise<T[]> {
  const response = await api.get<ApiResponse<T[]>>(buildNetworkListPath(kind, scope))
  return response.data ?? []
}

export async function getNetworkYAML(
  kind: NetworkKind,
  target: NetworkTarget,
): Promise<NetworkYAML> {
  const response = await api.get<ApiResponse<NetworkYAML>>(
    buildNetworkYAMLPath(kind, target.scope, target.name),
  )
  return response.data
}

export async function deleteNetworkResource(
  kind: NetworkKind,
  target: NetworkTarget,
): Promise<void> {
  await api.delete<unknown>(buildNetworkItemPath(kind, target.scope, target.name))
}

export async function updateNetworkYAML(
  kind: NetworkKind,
  variables: UpdateNetworkYAMLVariables,
): Promise<NetworkYAML> {
  const response = await api.put<ApiResponse<NetworkYAML>>(
    buildNetworkYAMLPath(kind, variables.scope, variables.name),
    {
      content: variables.content,
      ...(variables.requireObservedIdentity ? { requireObservedIdentity: true } : {}),
    },
  )
  return response.data
}
