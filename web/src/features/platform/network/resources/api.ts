import type { ScopeKey } from '@/types'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import { listNetworkResources } from '../shared/api'
import { buildNetworkDetailPath } from '../shared/paths'
import type { NetworkCoreKind } from './types'

export function listNetworkCoreResources<T>(kind: NetworkCoreKind, scope: ScopeKey) {
  return listNetworkResources<T>(kind, scope)
}

export async function getNetworkCoreResource<T extends { name: string }>(
  kind: NetworkCoreKind,
  scope: ScopeKey,
  name: string,
) {
  const response = await api.get<ApiResponse<T>>(buildNetworkDetailPath(kind, scope, name))
  return response.data
}
