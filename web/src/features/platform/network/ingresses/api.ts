import type { ScopeKey } from '@/types'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import { listNetworkResources } from '../shared/api'
import { buildNetworkDetailPath } from '../shared/paths'
import type { Ingress } from './types'
import { requireNetworkClusterId } from '../shared/scope'

export function listIngresses(scope: ScopeKey) {
  return listNetworkResources<Ingress>('ingresses', scope)
}

export async function getIngress(scope: ScopeKey, name: string) {
  const response = await api.get<ApiResponse<Ingress>>(
    buildNetworkDetailPath('ingresses', scope, name),
  )
  return response.data
}

export async function getIngressEvents(scope: ScopeKey, name: string) {
  const response = await api.get<
    ApiResponse<import('../../workloads/shared/types').WorkloadEvent[]>
  >(
    `/clusters/${encodeURIComponent(requireNetworkClusterId(scope))}/events?${new URLSearchParams({ namespace: scope.namespace || '', limit: '100' })}`,
  )
  return (response.data || []).filter(
    (event) =>
      event.namespace === scope.namespace &&
      event.involvedKind?.toLowerCase() === 'ingress' &&
      event.involvedName === name,
  )
}
