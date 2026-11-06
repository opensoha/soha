import { api } from '@/services/api-client'
import type { ApiResponse, ResourceMetrics, ScopeKey } from '@/types'
import {
  buildNetworkEventsPath,
  buildNetworkDetailPath,
  buildServiceMetricsPath,
} from '../shared/paths'
import { listNetworkResources } from '../shared/api'
import type { Service, ServiceEvent } from './types'

export function listServices(scope: ScopeKey) {
  return listNetworkResources<Service>('services', scope)
}

export async function getService(scope: ScopeKey, name: string) {
  const response = await api.get<ApiResponse<Service>>(
    buildNetworkDetailPath('services', scope, name),
  )
  return response.data
}

export async function getServiceMetrics(scope: ScopeKey, name: string): Promise<ResourceMetrics> {
  const response = await api.get<ApiResponse<ResourceMetrics>>(buildServiceMetricsPath(scope, name))
  return response.data
}

export async function listServiceEvents(
  scope: ScopeKey,
  name: string,
  limit = 100,
): Promise<ServiceEvent[]> {
  const response = await api.get<ApiResponse<ServiceEvent[]>>(buildNetworkEventsPath(scope, limit))
  const normalizedName = name.trim()
  return (response.data ?? []).filter(
    (event) =>
      event.involvedName === normalizedName &&
      event.namespace === scope.namespace &&
      event.involvedKind?.toLowerCase() === 'service',
  )
}
