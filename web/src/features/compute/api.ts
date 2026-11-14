import type {
  ConnectionCheckResultEnvelope,
  ComputeDomain,
  ComputeOverviewEnvelope,
  ComputeProviderDiscoverRequest,
  ComputeProviderDomain,
  ComputeProviderInstanceEnvelope,
  ComputeProviderInstanceListEnvelope,
  ComputeProviderReadRequest,
  ComputeResourceActionRequest,
  ComputeResourceKind,
  ComputeResourceRelationListEnvelope,
  ComputeTaskCategory,
  ComputeTaskDomain,
  ComputeTaskEnvelope,
  ComputeTaskLogListEnvelope,
  ComputeTaskListEnvelope,
  ComputeTaskStatus,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

interface ComputeTaskFilters {
  domain?: ComputeTaskDomain
  providerKey?: string
  status?: ComputeTaskStatus
  category?: ComputeTaskCategory
  resourceKind?: string
  resourceId?: string
  sortBy?: 'createdAt' | 'kind' | 'domain' | 'status'
  sortOrder?: 'asc' | 'desc'
  cursor?: string
  limit?: number
}

interface ComputeProviderInstanceFilters {
  domain?: ComputeProviderDomain
  providerKey?: string
  cursor?: string
  limit?: number
}

function queryString(filters: object) {
  const params = new URLSearchParams()
  Object.entries(filters).forEach(([key, value]) => {
    if (value !== undefined && value !== '') params.set(key, String(value))
  })
  const query = params.toString()
  return query ? `?${query}` : ''
}

export const computeApi = {
  overview: () => api.getEnvelope<ComputeOverviewEnvelope>('/compute/overview'),
  providerInstances: (filters: ComputeProviderInstanceFilters = {}) =>
    api.getEnvelope<ComputeProviderInstanceListEnvelope>(
      `/compute/provider-instances${queryString(filters)}`,
    ),
  providerInstance: (domain: ComputeProviderDomain, providerKey: string, instanceRef: string) =>
    api.getEnvelope<ComputeProviderInstanceEnvelope>(
      providerInstancePath(domain, providerKey, instanceRef),
    ),
  checkProviderHealth: async (
    domain: ComputeProviderDomain,
    providerKey: string,
    instanceRef: string,
    input: ComputeProviderReadRequest,
  ) => {
    const response = await postIdempotent<ConnectionCheckResultEnvelope>(
      `${providerInstancePath(domain, providerKey, instanceRef)}/health-checks`,
      input,
      'provider-health',
    )
    if (!response.data?.checkedAt || !response.data.status?.trim()) {
      throw new Error('Connection checks require a server upgrade to return synchronous results.')
    }
    return response
  },
  discoverProvider: (
    domain: ComputeProviderDomain,
    providerKey: string,
    instanceRef: string,
    input: ComputeProviderDiscoverRequest,
  ) =>
    postIdempotent<ComputeTaskEnvelope>(
      `${providerInstancePath(domain, providerKey, instanceRef)}/discoveries`,
      input,
      'provider-discovery',
    ),
  resourceRelations: (
    domain: ComputeDomain,
    kind: ComputeResourceKind,
    id: string,
    filters: { cursor?: string; limit?: number } = {},
  ) =>
    api.getEnvelope<ComputeResourceRelationListEnvelope>(
      `${resourcePath(domain, kind, id)}/relations${queryString(filters)}`,
    ),
  executeResourceAction: (
    domain: ComputeDomain,
    kind: ComputeResourceKind,
    id: string,
    action: string,
    input: ComputeResourceActionRequest = {},
  ) =>
    postIdempotent<ComputeTaskEnvelope>(
      `${resourcePath(domain, kind, id)}/actions/${encodeURIComponent(action)}`,
      input,
      'resource-action',
    ),
  tasks: (filters: ComputeTaskFilters = {}) =>
    api.getEnvelope<ComputeTaskListEnvelope>(`/compute/tasks${queryString(filters)}`),
  task: (domain: ComputeTaskDomain, taskId: string) =>
    api.getEnvelope<ComputeTaskEnvelope>(taskPath(domain, taskId)),
  taskLogs: (domain: ComputeTaskDomain, taskId: string) =>
    api.getEnvelope<ComputeTaskLogListEnvelope>(`${taskPath(domain, taskId)}/logs`),
  cancelTask: (domain: ComputeTaskDomain, taskId: string) =>
    postIdempotent<ComputeTaskEnvelope>(`${taskPath(domain, taskId)}/cancel`, {}, 'cancel'),
  retryTask: (domain: ComputeTaskDomain, taskId: string) =>
    postIdempotent<ComputeTaskEnvelope>(`${taskPath(domain, taskId)}/retry`, {}, 'retry'),
}

function postIdempotent<T>(path: string, body: unknown, action: string) {
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`
  return api.postWithHeaders<T>(path, body, {
    'Idempotency-Key': `compute-${action}-${random}`,
  })
}

function providerInstancePath(
  domain: ComputeProviderDomain,
  providerKey: string,
  instanceRef: string,
) {
  return `/compute/provider-instances/${encodeURIComponent(domain)}/${encodeURIComponent(providerKey)}/${encodeURIComponent(instanceRef)}`
}

function resourcePath(domain: ComputeDomain, kind: ComputeResourceKind, id: string) {
  return `/compute/resources/${encodeURIComponent(domain)}/${encodeURIComponent(kind)}/${encodeURIComponent(id)}`
}

function taskPath(domain: ComputeTaskDomain, taskId: string) {
  return `/compute/tasks/${encodeURIComponent(domain)}/${encodeURIComponent(taskId)}`
}

export { taskPath }
export type { ComputeProviderInstanceFilters, ComputeTaskFilters }
