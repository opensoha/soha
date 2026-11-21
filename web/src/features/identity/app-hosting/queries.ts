import { queryOptions } from '@tanstack/react-query'
import {
  getAdminHostedApp,
  getAppHostingSettings,
  listAdminHostedAppRevisions,
  listAdminHostedApps,
  listAdminVisibilityApprovals,
  listAppHostingAudienceCandidates,
} from './api'
import { appHostingAdminKeys } from './keys'
import type {
  AdminHostedAppListParams,
  AdminHostedAppRevisionListParams,
  AdminVisibilityApprovalListParams,
} from './types'

export const appHostingAdminQueries = {
  settings: (enabled = true) =>
    queryOptions({
      queryKey: appHostingAdminKeys.settings,
      queryFn: getAppHostingSettings,
      enabled,
    }),
  apps: (params: AdminHostedAppListParams = {}, enabled = true) =>
    queryOptions({
      queryKey: appHostingAdminKeys.appList(params),
      queryFn: () => listAdminHostedApps(params),
      enabled,
    }),
  appDetail: (appId: string, enabled = true) =>
    queryOptions({
      queryKey: appHostingAdminKeys.appDetail(appId),
      queryFn: () => getAdminHostedApp(appId),
      enabled: enabled && Boolean(appId.trim()),
    }),
  appRevisions: (appId: string, params: AdminHostedAppRevisionListParams = {}, enabled = true) =>
    queryOptions({
      queryKey: appHostingAdminKeys.appRevisions(appId, params),
      queryFn: () => listAdminHostedAppRevisions(appId, params),
      enabled: enabled && Boolean(appId.trim()),
    }),
  approvals: (params: AdminVisibilityApprovalListParams = {}, enabled = true) =>
    queryOptions({
      queryKey: appHostingAdminKeys.approvalList(params),
      queryFn: () => listAdminVisibilityApprovals(params),
      enabled,
    }),
  audienceCandidates: (query = '', limit = 20, enabled = true) =>
    queryOptions({
      queryKey: appHostingAdminKeys.audienceCandidateSearch(query, limit),
      queryFn: () => listAppHostingAudienceCandidates(query, limit),
      enabled,
    }),
}
