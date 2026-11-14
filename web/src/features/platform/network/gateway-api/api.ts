import type { ScopeKey } from '@/types'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import { listNetworkResources } from '../shared/api'
import { buildNetworkDetailPath } from '../shared/paths'
import type { GatewayAPIKind } from './types'

export function listGatewayAPIResources<T>(kind: GatewayAPIKind, scope: ScopeKey) {
  return listNetworkResources<T>(kind, scope)
}

export async function getGatewayAPIResource<T>(
  kind: GatewayAPIKind,
  scope: ScopeKey,
  name: string,
) {
  const response = await api.get<ApiResponse<T>>(buildNetworkDetailPath(kind, scope, name))
  return response.data
}
