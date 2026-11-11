import type {
  ApiResponse,
  EndpointDevice,
  EndpointDeviceInput,
  EndpointDeviceStatus,
  NetworkAccessGrant,
  NetworkAccessGrantInput,
  NetworkAccessGrantSecret,
  NetworkAccessGrantStatus,
  NetworkWorkloadBinding,
  NetworkWorkloadBindingInput,
  NetworkAccessPolicy,
  NetworkAccessPolicyInput,
  NetworkAdmissionPreviewRequest,
  NetworkConflictAnalysisRequest,
  NetworkConflictAnalysisResult,
  NetworkGateway,
  NetworkGatewayInput,
  NetworkGatewayStatus,
  NetworkMihomoMode,
  NetworkMihomoProfile,
  NetworkMihomoProfileInput,
  NetworkMihomoProfileStatus,
  NetworkNASBinding,
  NetworkNASBindingInput,
  NetworkNASBindingStatus,
  NetworkAccessProfile,
  NetworkPolicyPreviewRequest,
  NetworkPolicyPreviewResult,
  NetworkAdmissionPreviewResult,
  NetworkPolicyEffect,
  NetworkPolicySnapshot,
  NetworkResource,
  NetworkResourceInput,
  NetworkResourceKind,
  NetworkRuntimeEnrollment,
  NetworkRuntimeEnrollmentInput,
  NetworkRuntimeEnrollmentSecret,
  NetworkSession,
  NetworkSessionActionExecuteInput,
  NetworkSessionActionInput,
  NetworkSessionActionPlan,
  NetworkSessionCommand,
  NetworkSessionStatus,
  NetworkSite,
  NetworkSiteInput,
  NetworkSiteProfileBinding,
  NetworkSiteProfileBindingInput,
  NetworkSiteStatus,
  NetworkSpace,
  NetworkSpaceInput,
  NetworkSpaceStatus,
  NetworkTelemetrySummary,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

interface BaseFilters {
  limit?: number
  search?: string
}

export interface EndpointDeviceFilters extends BaseFilters {
  ownerUserId?: string
  siteId?: string
  status?: EndpointDeviceStatus
}

export interface NetworkSiteFilters extends BaseFilters {
  status?: NetworkSiteStatus
}

export interface NetworkSpaceFilters extends BaseFilters {
  siteId?: string
  status?: NetworkSpaceStatus
}

export interface NetworkResourceFilters extends BaseFilters {
  kind?: NetworkResourceKind
  protected?: boolean
  spaceId?: string
}

export interface NetworkGatewayFilters extends BaseFilters {
  siteId?: string
  status?: NetworkGatewayStatus
}

export interface NetworkMihomoProfileFilters extends BaseFilters {
  deviceId?: string
  mode?: NetworkMihomoMode
  status?: NetworkMihomoProfileStatus
}

export interface NetworkTelemetrySummaryFilters {
  from?: string
  limit?: number
  producerId?: string
  to?: string
}

export interface NetworkNASBindingFilters {
  limit?: number
  runtimeId?: string
  siteId?: string
  status?: NetworkNASBindingStatus
}

export interface NetworkSiteProfileBindingFilters {
  accessProfile?: NetworkAccessProfile
  limit?: number
  siteId?: string
}

export interface NetworkSessionFilters {
  deviceId?: string
  limit?: number
  runtimeId?: string
  siteId?: string
  status?: NetworkSessionStatus
  subjectId?: string
}

export interface NetworkRuntimeEnrollmentFilters {
  limit?: number
}

export interface NetworkAccessGrantFilters {
  deviceId?: string
  limit?: number
  status?: NetworkAccessGrantStatus
  subjectId?: string
}

export interface NetworkAccessPolicyFilters extends BaseFilters {
  enabled?: boolean
  effect?: NetworkPolicyEffect
}

export interface UpdateEndpointDeviceVariables {
  deviceId: string
  input: EndpointDeviceInput
}

export interface UpdateNetworkSiteVariables {
  siteId: string
  input: NetworkSiteInput
}

export interface UpdateNetworkSpaceVariables {
  spaceId: string
  input: NetworkSpaceInput
}

export interface UpdateNetworkResourceVariables {
  resourceId: string
  input: NetworkResourceInput
}

export interface UpdateNetworkGatewayVariables {
  gatewayId: string
  input: NetworkGatewayInput
}

export interface UpdateNetworkMihomoProfileVariables {
  profileId: string
  input: NetworkMihomoProfileInput
}

export interface UpdateNetworkAccessPolicyVariables {
  policyId: string
  input: NetworkAccessPolicyInput
}

export interface UpdateNetworkNASBindingVariables {
  bindingId: string
  input: NetworkNASBindingInput
}

export interface UpdateNetworkSiteProfileBindingVariables {
  bindingId: string
  input: NetworkSiteProfileBindingInput
}

export interface PlanNetworkSessionActionVariables {
  sessionId: string
  input: NetworkSessionActionInput
}

export interface ExecuteNetworkSessionActionVariables {
  sessionId: string
  input: NetworkSessionActionExecuteInput
}

function queryString(entries: Array<[string, string | number | boolean | undefined]>) {
  const params = new URLSearchParams()
  for (const [key, raw] of entries) {
    const value = typeof raw === 'string' ? raw.trim() : raw
    if (value === undefined || value === '') continue
    params.set(key, String(value))
  }
  const suffix = params.toString()
  return suffix ? `?${suffix}` : ''
}

async function list<T>(path: string): Promise<T[]> {
  const response = await api.get<ApiResponse<T[]>>(path)
  return response.data ?? []
}

export function listEndpointDevices(filters: EndpointDeviceFilters): Promise<EndpointDevice[]> {
  return list(
    `/network-access/devices${queryString([
      ['search', filters.search],
      ['status', filters.status],
      ['ownerUserId', filters.ownerUserId],
      ['siteId', filters.siteId],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function updateEndpointDevice({
  deviceId,
  input,
}: UpdateEndpointDeviceVariables): Promise<EndpointDevice> {
  const response = await api.put<ApiResponse<EndpointDevice>>(
    `/network-access/devices/${encodeURIComponent(deviceId)}`,
    input,
  )
  return response.data
}

export function listNetworkSites(filters: NetworkSiteFilters): Promise<NetworkSite[]> {
  return list(
    `/network-access/sites${queryString([
      ['search', filters.search],
      ['status', filters.status],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function createNetworkSite(input: NetworkSiteInput): Promise<NetworkSite> {
  const response = await api.post<ApiResponse<NetworkSite>>('/network-access/sites', input)
  return response.data
}

export async function updateNetworkSite({
  siteId,
  input,
}: UpdateNetworkSiteVariables): Promise<NetworkSite> {
  const response = await api.put<ApiResponse<NetworkSite>>(
    `/network-access/sites/${encodeURIComponent(siteId)}`,
    input,
  )
  return response.data
}

export async function deleteNetworkSite(siteId: string): Promise<void> {
  await api.delete(`/network-access/sites/${encodeURIComponent(siteId)}`)
}

export function listNetworkSpaces(filters: NetworkSpaceFilters): Promise<NetworkSpace[]> {
  return list(
    `/network-access/spaces${queryString([
      ['search', filters.search],
      ['status', filters.status],
      ['siteId', filters.siteId],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function createNetworkSpace(input: NetworkSpaceInput): Promise<NetworkSpace> {
  const response = await api.post<ApiResponse<NetworkSpace>>('/network-access/spaces', input)
  return response.data
}

export async function updateNetworkSpace({
  spaceId,
  input,
}: UpdateNetworkSpaceVariables): Promise<NetworkSpace> {
  const response = await api.put<ApiResponse<NetworkSpace>>(
    `/network-access/spaces/${encodeURIComponent(spaceId)}`,
    input,
  )
  return response.data
}

export async function deleteNetworkSpace(spaceId: string): Promise<void> {
  await api.delete(`/network-access/spaces/${encodeURIComponent(spaceId)}`)
}

export function listNetworkResources(filters: NetworkResourceFilters): Promise<NetworkResource[]> {
  return list(
    `/network-access/resources${queryString([
      ['search', filters.search],
      ['kind', filters.kind],
      ['spaceId', filters.spaceId],
      ['protected', filters.protected],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function createNetworkResource(input: NetworkResourceInput): Promise<NetworkResource> {
  const response = await api.post<ApiResponse<NetworkResource>>('/network-access/resources', input)
  return response.data
}

export async function updateNetworkResource({
  resourceId,
  input,
}: UpdateNetworkResourceVariables): Promise<NetworkResource> {
  const response = await api.put<ApiResponse<NetworkResource>>(
    `/network-access/resources/${encodeURIComponent(resourceId)}`,
    input,
  )
  return response.data
}

export async function deleteNetworkResource(resourceId: string): Promise<void> {
  await api.delete(`/network-access/resources/${encodeURIComponent(resourceId)}`)
}

export function listNetworkGateways(filters: NetworkGatewayFilters): Promise<NetworkGateway[]> {
  return list(
    `/network-access/gateways${queryString([
      ['search', filters.search],
      ['status', filters.status],
      ['siteId', filters.siteId],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function createNetworkGateway(input: NetworkGatewayInput): Promise<NetworkGateway> {
  const response = await api.post<ApiResponse<NetworkGateway>>('/network-access/gateways', input)
  return response.data
}

export async function updateNetworkGateway({
  gatewayId,
  input,
}: UpdateNetworkGatewayVariables): Promise<NetworkGateway> {
  const response = await api.put<ApiResponse<NetworkGateway>>(
    `/network-access/gateways/${encodeURIComponent(gatewayId)}`,
    input,
  )
  return response.data
}

export function listNetworkMihomoProfiles(
  filters: NetworkMihomoProfileFilters,
): Promise<NetworkMihomoProfile[]> {
  return list(
    `/network-access/mihomo-profiles${queryString([
      ['search', filters.search],
      ['deviceId', filters.deviceId],
      ['mode', filters.mode],
      ['status', filters.status],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function createNetworkMihomoProfile(
  input: NetworkMihomoProfileInput,
): Promise<NetworkMihomoProfile> {
  const response = await api.post<ApiResponse<NetworkMihomoProfile>>(
    '/network-access/mihomo-profiles',
    input,
  )
  return response.data
}

export async function updateNetworkMihomoProfile({
  profileId,
  input,
}: UpdateNetworkMihomoProfileVariables): Promise<NetworkMihomoProfile> {
  const response = await api.put<ApiResponse<NetworkMihomoProfile>>(
    `/network-access/mihomo-profiles/${encodeURIComponent(profileId)}`,
    input,
  )
  return response.data
}

export async function deleteNetworkMihomoProfile(profileId: string): Promise<void> {
  await api.delete(`/network-access/mihomo-profiles/${encodeURIComponent(profileId)}`)
}

export async function getNetworkTelemetrySummary(
  filters: NetworkTelemetrySummaryFilters = {},
): Promise<NetworkTelemetrySummary> {
  const response = await api.get<ApiResponse<NetworkTelemetrySummary>>(
    `/network-access/telemetry/summary${queryString([
      ['from', filters.from],
      ['to', filters.to],
      ['producerId', filters.producerId],
      ['limit', filters.limit],
    ])}`,
  )
  return response.data
}

export function listNetworkNASBindings(
  filters: NetworkNASBindingFilters,
): Promise<NetworkNASBinding[]> {
  return list(
    `/network-access/nas-bindings${queryString([
      ['siteId', filters.siteId],
      ['runtimeId', filters.runtimeId],
      ['status', filters.status],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function createNetworkNASBinding(
  input: NetworkNASBindingInput,
): Promise<NetworkNASBinding> {
  const response = await api.post<ApiResponse<NetworkNASBinding>>(
    '/network-access/nas-bindings',
    input,
  )
  return response.data
}

export async function updateNetworkNASBinding({
  bindingId,
  input,
}: UpdateNetworkNASBindingVariables): Promise<NetworkNASBinding> {
  const response = await api.put<ApiResponse<NetworkNASBinding>>(
    `/network-access/nas-bindings/${encodeURIComponent(bindingId)}`,
    input,
  )
  return response.data
}

export async function deleteNetworkNASBinding(bindingId: string): Promise<void> {
  await api.delete(`/network-access/nas-bindings/${encodeURIComponent(bindingId)}`)
}

export function listNetworkSiteProfileBindings(
  filters: NetworkSiteProfileBindingFilters,
): Promise<NetworkSiteProfileBinding[]> {
  return list(
    `/network-access/site-profile-bindings${queryString([
      ['siteId', filters.siteId],
      ['accessProfile', filters.accessProfile],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function createNetworkSiteProfileBinding(
  input: NetworkSiteProfileBindingInput,
): Promise<NetworkSiteProfileBinding> {
  const response = await api.post<ApiResponse<NetworkSiteProfileBinding>>(
    '/network-access/site-profile-bindings',
    input,
  )
  return response.data
}

export async function updateNetworkSiteProfileBinding({
  bindingId,
  input,
}: UpdateNetworkSiteProfileBindingVariables): Promise<NetworkSiteProfileBinding> {
  const response = await api.put<ApiResponse<NetworkSiteProfileBinding>>(
    `/network-access/site-profile-bindings/${encodeURIComponent(bindingId)}`,
    input,
  )
  return response.data
}

export async function deleteNetworkSiteProfileBinding(bindingId: string): Promise<void> {
  await api.delete(`/network-access/site-profile-bindings/${encodeURIComponent(bindingId)}`)
}

export function listNetworkSessions(filters: NetworkSessionFilters): Promise<NetworkSession[]> {
  return list(
    `/network-access/sessions${queryString([
      ['siteId', filters.siteId],
      ['runtimeId', filters.runtimeId],
      ['subjectId', filters.subjectId],
      ['deviceId', filters.deviceId],
      ['status', filters.status],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function planNetworkSessionAction({
  sessionId,
  input,
}: PlanNetworkSessionActionVariables): Promise<NetworkSessionActionPlan> {
  const response = await api.post<ApiResponse<NetworkSessionActionPlan>>(
    `/network-access/sessions/${encodeURIComponent(sessionId)}/actions/plan`,
    input,
  )
  return response.data
}

export async function executeNetworkSessionAction({
  sessionId,
  input,
}: ExecuteNetworkSessionActionVariables): Promise<NetworkSessionCommand> {
  const response = await api.post<ApiResponse<NetworkSessionCommand>>(
    `/network-access/sessions/${encodeURIComponent(sessionId)}/actions/execute`,
    input,
  )
  return response.data
}

export function listNetworkRuntimeEnrollments(
  filters: NetworkRuntimeEnrollmentFilters,
): Promise<NetworkRuntimeEnrollment[]> {
  return list(`/network-access/enrollments${queryString([['limit', filters.limit]])}`)
}

export async function createNetworkRuntimeEnrollment(
  input: NetworkRuntimeEnrollmentInput,
): Promise<NetworkRuntimeEnrollmentSecret> {
  const response = await api.post<ApiResponse<NetworkRuntimeEnrollmentSecret>>(
    '/network-access/enrollments',
    input,
  )
  return response.data
}

export async function revokeNetworkRuntimeEnrollment(
  enrollmentId: string,
): Promise<NetworkRuntimeEnrollment> {
  const response = await api.post<ApiResponse<NetworkRuntimeEnrollment>>(
    `/network-access/enrollments/${encodeURIComponent(enrollmentId)}/revoke`,
  )
  return response.data
}

export function listNetworkAccessGrants(
  filters: NetworkAccessGrantFilters,
): Promise<NetworkAccessGrant[]> {
  return list(
    `/network-access/access-grants${queryString([
      ['subjectId', filters.subjectId],
      ['deviceId', filters.deviceId],
      ['status', filters.status],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function createNetworkAccessGrant(
  input: NetworkAccessGrantInput,
): Promise<NetworkAccessGrantSecret> {
  const response = await api.post<ApiResponse<NetworkAccessGrantSecret>>(
    '/network-access/access-grants',
    input,
  )
  return response.data
}

export async function revokeNetworkAccessGrant(grantId: string): Promise<NetworkAccessGrant> {
  const response = await api.post<ApiResponse<NetworkAccessGrant>>(
    `/network-access/access-grants/${encodeURIComponent(grantId)}/revoke`,
  )
  return response.data
}

export function listNetworkAccessPolicies(
  filters: NetworkAccessPolicyFilters,
): Promise<NetworkAccessPolicy[]> {
  return list(
    `/network-access/policies${queryString([
      ['search', filters.search],
      ['enabled', filters.enabled],
      ['effect', filters.effect],
      ['limit', filters.limit],
    ])}`,
  )
}

export async function createNetworkAccessPolicy(
  input: NetworkAccessPolicyInput,
): Promise<NetworkAccessPolicy> {
  const response = await api.post<ApiResponse<NetworkAccessPolicy>>(
    '/network-access/policies',
    input,
  )
  return response.data
}

export async function updateNetworkAccessPolicy({
  policyId,
  input,
}: UpdateNetworkAccessPolicyVariables): Promise<NetworkAccessPolicy> {
  const response = await api.put<ApiResponse<NetworkAccessPolicy>>(
    `/network-access/policies/${encodeURIComponent(policyId)}`,
    input,
  )
  return response.data
}

export async function deleteNetworkAccessPolicy(policyId: string): Promise<void> {
  await api.delete(`/network-access/policies/${encodeURIComponent(policyId)}`)
}

export async function getNetworkAccessPolicySnapshot(): Promise<NetworkPolicySnapshot> {
  const response = await api.get<ApiResponse<NetworkPolicySnapshot>>(
    '/network-access/policy/snapshot',
  )
  return response.data
}

export async function compileNetworkAccessPolicySnapshot(): Promise<NetworkPolicySnapshot> {
  const response = await api.post<ApiResponse<NetworkPolicySnapshot>>(
    '/network-access/policies/compile',
  )
  return response.data
}

export async function analyzeNetworkAccessConflicts(
  input: NetworkConflictAnalysisRequest,
): Promise<NetworkConflictAnalysisResult> {
  const response = await api.post<ApiResponse<NetworkConflictAnalysisResult>>(
    '/network-access/conflicts/analyze',
    input,
  )
  return response.data
}

export async function previewNetworkAccessPolicy(
  input: NetworkPolicyPreviewRequest,
): Promise<NetworkPolicyPreviewResult> {
  const response = await api.post<ApiResponse<NetworkPolicyPreviewResult>>(
    '/network-access/policy/preview',
    input,
  )
  return response.data
}

export async function previewNetworkAdmission(
  input: NetworkAdmissionPreviewRequest,
): Promise<NetworkAdmissionPreviewResult> {
  const response = await api.post<ApiResponse<NetworkAdmissionPreviewResult>>(
    '/network-access/admission/preview',
    input,
  )
  return response.data
}

export async function listNetworkWorkloadBindings(): Promise<NetworkWorkloadBinding[]> {
  const response = await api.get<ApiResponse<NetworkWorkloadBinding[]>>(
    '/network-access/workload-bindings',
  )
  return response.data ?? []
}
export async function putNetworkWorkloadBinding({
  id,
  input,
}: {
  id: string
  input: NetworkWorkloadBindingInput
}): Promise<NetworkWorkloadBinding> {
  const response = await api.put<ApiResponse<NetworkWorkloadBinding>>(
    `/network-access/workload-bindings/${encodeURIComponent(id)}`,
    input,
  )
  return response.data
}

export async function submitManagementProof(
  deviceId: string,
  input: import('@opensoha/contracts/gen/ts/sohaapi').NetworkManagementProofInput,
): Promise<EndpointDevice> {
  const result = await api.post<ApiResponse<EndpointDevice>>(
    `/network-access/devices/${encodeURIComponent(deviceId)}/management-proof`,
    input,
  )
  return result.data
}
