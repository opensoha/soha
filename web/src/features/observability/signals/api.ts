import type {
  ObservabilityMetricDefinition,
  ObservabilityMetricQueryInput,
  ObservabilityMetricQueryResult,
  ObservabilityQueryResultMeta,
  ObservabilityService,
  ObservabilityServiceListEnvelope,
  ObservabilityTopologyEnvelope,
  ObservabilityTraceQueryInput,
  ObservabilityTraceQueryResult,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'

export async function queryMetrics(input: ObservabilityMetricQueryInput) {
  const response = await api.post<ApiResponse<ObservabilityMetricQueryResult>>(
    '/observability/metrics/query',
    input,
  )
  return response.data
}

export async function queryTraces(input: ObservabilityTraceQueryInput) {
  const response = await api.post<ApiResponse<ObservabilityTraceQueryResult>>(
    '/observability/traces/query',
    input,
  )
  return response.data
}

export interface ObservabilityServiceQuery {
  clusterId?: string
  dataSourceId?: string
  environment?: string
  namespace?: string
  service?: string
  timeFrom: string
  timeTo: string
}

type ObservabilityServiceResponse = {
  data: ObservabilityService
  meta: ObservabilityQueryResultMeta
}

function serviceQueryPath(path: string, query: ObservabilityServiceQuery) {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value?.trim()) params.set(key, value.trim())
  }
  return `${path}?${params.toString()}`
}

export async function listMetricCatalog() {
  const response = await api.get<ApiResponse<ObservabilityMetricDefinition[]>>(
    '/observability/metrics/catalog',
  )
  return response.data ?? []
}

export function listServices(query: ObservabilityServiceQuery) {
  return api.getEnvelope<ObservabilityServiceListEnvelope>(
    serviceQueryPath('/observability/services', query),
  )
}

export function getService(serviceId: string, query: ObservabilityServiceQuery) {
  return api.getEnvelope<ObservabilityServiceResponse>(
    serviceQueryPath(`/observability/services/${encodeURIComponent(serviceId)}`, query),
  )
}

export function getServiceTopology(serviceId: string, query: ObservabilityServiceQuery) {
  return api.getEnvelope<ObservabilityTopologyEnvelope>(
    serviceQueryPath(`/observability/services/${encodeURIComponent(serviceId)}/topology`, query),
  )
}
