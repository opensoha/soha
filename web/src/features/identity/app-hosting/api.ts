import { api } from '@/services/api-client'
import {
  normalizeAdminAppListParams,
  normalizeAdminApprovalListParams,
  normalizeRevisionListParams,
} from './keys'
import type {
  AdminHostedAppListParams,
  AdminHostedAppRevisionListParams,
  AdminVisibilityApprovalListParams,
  AppHostingAudienceCandidates,
  AppHostingGatewayTokenRotateRequest,
  AppHostingGatewayTokenSecret,
  AppHostingSettings,
  AppHostingSettingsUpdateRequest,
  ApproveVisibilityApprovalInput,
  DeleteHostedAppInput,
  DeleteHostedAppResult,
  HostedApp,
  HostedAppList,
  HostedAppRevisionList,
  HostedAppVisibilityApprovalDecisionResult,
  HostedAppVisibilityApprovalList,
  RejectVisibilityApprovalInput,
  SuspendHostedAppInput,
  TransferHostedAppInput,
  UnsuspendHostedAppInput,
} from './types'

export async function getAppHostingSettings(): Promise<AppHostingSettings> {
  const response = await api.get<{ data: AppHostingSettings }>('/app-hosting/settings')
  return response.data
}

export async function updateAppHostingSettings(
  input: AppHostingSettingsUpdateRequest,
): Promise<AppHostingSettings> {
  const response = await api.put<{ data: AppHostingSettings }>('/app-hosting/settings', input)
  return response.data
}

export async function rotateAppHostingGatewayToken(
  input?: AppHostingGatewayTokenRotateRequest,
): Promise<AppHostingGatewayTokenSecret> {
  const payload = input?.reason?.trim() ? { reason: input.reason.trim() } : {}
  const response = await api.post<{ data: AppHostingGatewayTokenSecret }>(
    '/app-hosting/settings/gateway-token/rotate',
    payload,
  )
  return response.data
}

export async function listAppHostingAudienceCandidates(
  query = '',
  limit = 20,
): Promise<AppHostingAudienceCandidates> {
  const params = new URLSearchParams()
  if (query.trim()) params.set('query', query.trim())
  params.set('limit', String(limit))
  const response = await api.get<{ data: AppHostingAudienceCandidates }>(
    `/app-hosting/audience-candidates?${params.toString()}`,
  )
  return response.data
}

export async function listAdminHostedApps(
  params: AdminHostedAppListParams = {},
): Promise<HostedAppList> {
  const normalized = normalizeAdminAppListParams(params)
  const search = new URLSearchParams()
  search.set('scope', 'all')
  if (normalized.query) search.set('query', normalized.query)
  if (normalized.status) search.set('status', normalized.status)
  if (normalized.ownerUserId) search.set('ownerUserId', normalized.ownerUserId)
  if (normalized.teamId) search.set('teamId', normalized.teamId)
  search.set('limit', String(normalized.limit))
  search.set('offset', String(normalized.offset))
  const response = await api.get<{ data: HostedAppList }>(`/app-hosting/apps?${search.toString()}`)
  return response.data
}

export async function getAdminHostedApp(appId: string): Promise<HostedApp> {
  const response = await api.get<{ data: HostedApp }>(
    `/app-hosting/apps/${encodeURIComponent(appId.trim())}`,
  )
  return response.data
}

export async function listAdminHostedAppRevisions(
  appId: string,
  params: AdminHostedAppRevisionListParams = {},
): Promise<HostedAppRevisionList> {
  const normalized = normalizeRevisionListParams(params)
  const search = new URLSearchParams()
  search.set('limit', String(normalized.limit))
  search.set('offset', String(normalized.offset))
  const response = await api.get<{ data: HostedAppRevisionList }>(
    `/app-hosting/apps/${encodeURIComponent(appId.trim())}/revisions?${search.toString()}`,
  )
  return response.data
}

export async function suspendAdminHostedApp(input: SuspendHostedAppInput): Promise<HostedApp> {
  const response = await api.post<{ data: HostedApp }>(
    `/app-hosting/apps/${encodeURIComponent(input.appId.trim())}/suspend`,
    { reason: input.reason.trim() },
  )
  return response.data
}

export async function unsuspendAdminHostedApp(input: UnsuspendHostedAppInput): Promise<HostedApp> {
  const payload = input.reason?.trim() ? { reason: input.reason.trim() } : {}
  const response = await api.post<{ data: HostedApp }>(
    `/app-hosting/apps/${encodeURIComponent(input.appId.trim())}/unsuspend`,
    payload,
  )
  return response.data
}

export async function transferAdminHostedAppOwnership(
  input: TransferHostedAppInput,
): Promise<HostedApp> {
  const payload: { newOwnerUserId: string; reason?: string } = {
    newOwnerUserId: input.newOwnerUserId.trim(),
  }
  if (input.reason?.trim()) payload.reason = input.reason.trim()
  const response = await api.post<{ data: HostedApp }>(
    `/app-hosting/apps/${encodeURIComponent(input.appId.trim())}/transfer`,
    payload,
  )
  return response.data
}

export async function deleteAdminHostedApp(
  input: DeleteHostedAppInput,
): Promise<DeleteHostedAppResult> {
  const payload: { confirmationName: string; reason?: string } = {
    confirmationName: input.confirmationName,
  }
  if (input.reason?.trim()) payload.reason = input.reason.trim()
  const response = await api.post<{ data: DeleteHostedAppResult }>(
    `/app-hosting/apps/${encodeURIComponent(input.appId.trim())}/delete`,
    payload,
  )
  return response.data
}

export async function listAdminVisibilityApprovals(
  params: AdminVisibilityApprovalListParams = {},
): Promise<HostedAppVisibilityApprovalList> {
  const normalized = normalizeAdminApprovalListParams(params)
  const search = new URLSearchParams()
  search.set('scope', 'all')
  if (normalized.status) search.set('status', normalized.status)
  if (normalized.appId) search.set('appId', normalized.appId)
  search.set('limit', String(normalized.limit))
  search.set('offset', String(normalized.offset))
  const response = await api.get<{ data: HostedAppVisibilityApprovalList }>(
    `/app-hosting/approvals?${search.toString()}`,
  )
  return response.data
}

export async function approveAdminVisibilityApproval(
  input: ApproveVisibilityApprovalInput,
): Promise<HostedAppVisibilityApprovalDecisionResult> {
  const payload = input.comment?.trim() ? { comment: input.comment.trim() } : {}
  const response = await api.post<{ data: HostedAppVisibilityApprovalDecisionResult }>(
    `/app-hosting/approvals/${encodeURIComponent(input.approvalId.trim())}/approve`,
    payload,
  )
  return response.data
}

export async function rejectAdminVisibilityApproval(
  input: RejectVisibilityApprovalInput,
): Promise<HostedAppVisibilityApprovalDecisionResult> {
  const response = await api.post<{ data: HostedAppVisibilityApprovalDecisionResult }>(
    `/app-hosting/approvals/${encodeURIComponent(input.approvalId.trim())}/reject`,
    { comment: input.comment.trim() },
  )
  return response.data
}
