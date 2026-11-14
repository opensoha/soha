import type {
  ApiResponse,
  NetworkEndpointMaintenance,
  NetworkEndpointMaintenanceInput,
  NetworkEndpointPresence,
  NetworkEndpointResponseEvent,
  NetworkEndpointResponseEventEnvelope,
  NetworkEndpointResponsePolicy,
  NetworkEndpointResponsePolicyEnvelope,
  NetworkEndpointResponsePolicyInput,
  NetworkEndpointResponsePolicyRevision,
  NetworkEndpointResponsePreviewEnvelope,
  NetworkEndpointResponsePreviewInput,
  NetworkEndpointResponseRestoreInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

export interface EndpointResponseFilters {
  deviceId?: string
  siteId?: string
  policyId?: string
  status?: string
}

const base = '/network-access'
const policies = `${base}/endpoint-response-policies`
const events = `${base}/endpoint-response-events`
const maintenance = `${base}/endpoint-maintenance-windows`
const idPath = (path: string, id: string) => `${path}/${encodeURIComponent(id)}`
function query(filters: EndpointResponseFilters) {
  const params = new URLSearchParams({ limit: '200' })
  for (const [key, value] of Object.entries(filters)) {
    if (value?.trim()) params.set(key, value.trim())
  }
  return params.toString()
}

export async function listEndpointResponsePolicies(filters: EndpointResponseFilters = {}) {
  return (
    (await api.get<ApiResponse<NetworkEndpointResponsePolicy[]>>(`${policies}?${query(filters)}`))
      .data ?? []
  )
}
export async function saveEndpointResponsePolicy({
  policyId,
  input,
}: {
  policyId?: string
  input: NetworkEndpointResponsePolicyInput
}) {
  const response = policyId
    ? await api.put<NetworkEndpointResponsePolicyEnvelope>(idPath(policies, policyId), input)
    : await api.post<NetworkEndpointResponsePolicyEnvelope>(policies, input)
  return response.data
}
export async function publishEndpointResponsePolicy({
  policyId,
  expectedRevision,
}: {
  policyId: string
  expectedRevision: number
}) {
  return (
    await api.post<NetworkEndpointResponsePolicyEnvelope>(`${idPath(policies, policyId)}/publish`, {
      expectedRevision,
    })
  ).data
}
export async function rollbackEndpointResponsePolicy({
  policyId,
  expectedRevision,
  revision,
}: {
  policyId: string
  expectedRevision: number
  revision: number
}) {
  return (
    await api.post<NetworkEndpointResponsePolicyEnvelope>(
      `${idPath(policies, policyId)}/rollback`,
      { expectedRevision, revision },
    )
  ).data
}
export function deleteEndpointResponsePolicy({
  policyId,
  expectedRevision,
}: {
  policyId: string
  expectedRevision: number
}) {
  return api.delete(`${idPath(policies, policyId)}?expectedRevision=${expectedRevision}`)
}
export async function endpointResponseRevisions(policyId: string) {
  return (
    (
      await api.get<ApiResponse<NetworkEndpointResponsePolicyRevision[]>>(
        `${idPath(policies, policyId)}/revisions`,
      )
    ).data ?? []
  )
}
export async function previewEndpointResponse({
  policyId,
  input,
}: {
  policyId: string
  input: NetworkEndpointResponsePreviewInput
}) {
  return (
    await api.post<NetworkEndpointResponsePreviewEnvelope>(
      `${idPath(policies, policyId)}/preview`,
      input,
    )
  ).data
}
export async function listEndpointResponseEvents(filters: EndpointResponseFilters = {}) {
  return (
    (await api.get<ApiResponse<NetworkEndpointResponseEvent[]>>(`${events}?${query(filters)}`))
      .data ?? []
  )
}
export async function endpointResponseEvent(eventId: string) {
  return (await api.get<NetworkEndpointResponseEventEnvelope>(idPath(events, eventId))).data
}
export async function restoreEndpointResponse({
  eventId,
  input,
}: {
  eventId: string
  input: NetworkEndpointResponseRestoreInput
}) {
  return (
    await api.post<NetworkEndpointResponseEventEnvelope>(
      `${idPath(events, eventId)}/restore`,
      input,
    )
  ).data
}
export async function endpointPresence(deviceId: string) {
  return (
    await api.get<ApiResponse<NetworkEndpointPresence>>(
      `${base}/endpoint-devices/${encodeURIComponent(deviceId)}/presence`,
    )
  ).data
}
export async function listEndpointMaintenance(filters: EndpointResponseFilters = {}) {
  return (
    (await api.get<ApiResponse<NetworkEndpointMaintenance[]>>(`${maintenance}?${query(filters)}`))
      .data ?? []
  )
}
export async function createEndpointMaintenance(input: NetworkEndpointMaintenanceInput) {
  return (await api.post<ApiResponse<NetworkEndpointMaintenance>>(maintenance, input)).data
}
export function endEndpointMaintenance(windowId: string) {
  return api.delete(idPath(maintenance, windowId))
}
