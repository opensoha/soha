import { api } from '@/services/api-client'
import type {
  AppHostingAudienceCandidatesResponse,
  DeleteHostedAppResponse,
  HostedAppDeploymentResponse,
  HostedAppListResponse,
  HostedAppResponse,
  HostedAppRevisionListResponse,
  HostedAppVisibilityApprovalListResponse,
  PortalHostedAppCatalogResponse,
  WorkspaceHostedAppDeploymentResponse,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type {
  AudienceCandidatesQueryParams,
  CreateHostedAppRequest,
  DeleteHostedAppRequest,
  HostedAppLifecycleActionRequest,
  HostedAppRevisionsQueryParams,
  HostedAppRollbackRequest,
  MyHostedAppApprovalsQueryParams,
  MyHostedAppsQueryParams,
  PortalHostedAppsQueryParams,
  UpdateHostedAppMaintainersRequest,
  UpdateHostedAppRequest,
  UpdateHostedAppVisibilityRequest,
  WorkspaceHostedAppDeploymentRequest,
} from './types'

const APP_HOSTING_BASE = '/app-hosting'

function withQuery(path: string, entries: Record<string, string | number | undefined>) {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(entries)) {
    if (value === undefined || value === '') continue
    search.set(key, String(value))
  }
  const encoded = search.toString()
  return encoded ? `${path}?${encoded}` : path
}

function encodeAppId(appId: string) {
  return encodeURIComponent(appId.trim())
}

export const appHostingApi = {
  listPortalApps: async (params?: PortalHostedAppsQueryParams) => {
    const response = await api.get<PortalHostedAppCatalogResponse>(
      withQuery(`${APP_HOSTING_BASE}/portal/apps`, {
        query: params?.query?.trim() || undefined,
        kind: params?.kind || undefined,
        authorUserId: params?.authorUserId?.trim() || undefined,
        limit: params?.limit,
        offset: params?.offset,
      }),
    )
    return response.data
  },

  searchAudienceCandidates: async (params?: AudienceCandidatesQueryParams) => {
    const response = await api.get<AppHostingAudienceCandidatesResponse>(
      withQuery(`${APP_HOSTING_BASE}/audience-candidates`, {
        query: params?.query?.trim() || undefined,
        limit: params?.limit,
      }),
    )
    return response.data
  },

  listMyApps: async (params?: MyHostedAppsQueryParams) => {
    const response = await api.get<HostedAppListResponse>(
      withQuery(`${APP_HOSTING_BASE}/apps`, {
        scope: 'mine',
        query: params?.query?.trim() || undefined,
        status: params?.status || undefined,
        limit: params?.limit,
        offset: params?.offset,
      }),
    )
    return response.data
  },

  createApp: async (input: CreateHostedAppRequest) => {
    const response = await api.post<HostedAppResponse>(`${APP_HOSTING_BASE}/apps`, input)
    return response.data
  },

  getApp: async (appId: string) => {
    const response = await api.get<HostedAppResponse>(
      `${APP_HOSTING_BASE}/apps/${encodeAppId(appId)}`,
    )
    return response.data
  },

  updateApp: async (appId: string, input: UpdateHostedAppRequest) => {
    const response = await api.patch<HostedAppResponse>(
      `${APP_HOSTING_BASE}/apps/${encodeAppId(appId)}`,
      input,
    )
    return response.data
  },

  updateVisibility: async (appId: string, input: UpdateHostedAppVisibilityRequest) => {
    const response = await api.put<HostedAppResponse>(
      `${APP_HOSTING_BASE}/apps/${encodeAppId(appId)}/visibility`,
      input,
    )
    return response.data
  },

  withdrawVisibilityApproval: async (appId: string, input?: HostedAppLifecycleActionRequest) => {
    const response = await api.post<HostedAppResponse>(
      `${APP_HOSTING_BASE}/apps/${encodeAppId(appId)}/visibility-approval/withdraw`,
      input ?? {},
    )
    return response.data
  },

  updateMaintainers: async (appId: string, input: UpdateHostedAppMaintainersRequest) => {
    const response = await api.put<HostedAppResponse>(
      `${APP_HOSTING_BASE}/apps/${encodeAppId(appId)}/maintainers`,
      input,
    )
    return response.data
  },

  takeOffline: async (appId: string, input?: HostedAppLifecycleActionRequest) => {
    const response = await api.post<HostedAppResponse>(
      `${APP_HOSTING_BASE}/apps/${encodeAppId(appId)}/offline`,
      input ?? {},
    )
    return response.data
  },

  bringOnline: async (appId: string, input?: HostedAppLifecycleActionRequest) => {
    const response = await api.post<HostedAppResponse>(
      `${APP_HOSTING_BASE}/apps/${encodeAppId(appId)}/online`,
      input ?? {},
    )
    return response.data
  },

  deleteApp: async (appId: string, input: DeleteHostedAppRequest) => {
    const response = await api.post<DeleteHostedAppResponse>(
      `${APP_HOSTING_BASE}/apps/${encodeAppId(appId)}/delete`,
      input,
    )
    return response.data
  },

  listRevisions: async (appId: string, params?: HostedAppRevisionsQueryParams) => {
    const response = await api.get<HostedAppRevisionListResponse>(
      withQuery(`${APP_HOSTING_BASE}/apps/${encodeAppId(appId)}/revisions`, {
        limit: params?.limit,
        offset: params?.offset,
      }),
    )
    return response.data
  },

  rollbackApp: async (appId: string, input: HostedAppRollbackRequest) => {
    const response = await api.post<HostedAppDeploymentResponse>(
      `${APP_HOSTING_BASE}/apps/${encodeAppId(appId)}/rollback`,
      input,
    )
    return response.data
  },

  deployFromWorkspace: async (input: WorkspaceHostedAppDeploymentRequest) => {
    const response = await api.post<WorkspaceHostedAppDeploymentResponse>(
      `${APP_HOSTING_BASE}/workspace-deployments`,
      input,
    )
    return response.data
  },

  listMyApprovals: async (params?: MyHostedAppApprovalsQueryParams) => {
    const response = await api.get<HostedAppVisibilityApprovalListResponse>(
      withQuery(`${APP_HOSTING_BASE}/approvals`, {
        scope: 'mine',
        appId: params?.appId?.trim() || undefined,
        status: params?.status || undefined,
        limit: params?.limit,
        offset: params?.offset,
      }),
    )
    return response.data
  },
}
