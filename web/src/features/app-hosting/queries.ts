import { queryOptions } from '@tanstack/react-query'
import { appHostingApi } from './api'
import {
  appHostingKeys,
  normalizeAudienceCandidatesParams,
  normalizeMyApprovalsParams,
  normalizeMyAppsParams,
  normalizePortalAppsParams,
  normalizeRevisionsParams,
} from './keys'
import type {
  AudienceCandidatesQueryParams,
  HostedAppRevisionsQueryParams,
  MyHostedAppApprovalsQueryParams,
  MyHostedAppsQueryParams,
  PortalHostedAppsQueryParams,
} from './types'

export const appHostingQueries = {
  portalApps: (params?: PortalHostedAppsQueryParams, enabled = true) => {
    const normalized = normalizePortalAppsParams(params)
    return queryOptions({
      queryKey: appHostingKeys.portalList(params),
      queryFn: () =>
        appHostingApi.listPortalApps({
          query: normalized.query || undefined,
          kind: normalized.kind || undefined,
          authorUserId: normalized.authorUserId || undefined,
          limit: normalized.limit,
          offset: normalized.offset || undefined,
        }),
      enabled,
    })
  },

  audienceCandidates: (params?: AudienceCandidatesQueryParams, enabled = true) => {
    const normalized = normalizeAudienceCandidatesParams(params)
    return queryOptions({
      queryKey: appHostingKeys.audienceCandidates(params),
      queryFn: () =>
        appHostingApi.searchAudienceCandidates({
          query: normalized.query || undefined,
          limit: normalized.limit,
        }),
      enabled,
    })
  },

  myApps: (params?: MyHostedAppsQueryParams, enabled = true) => {
    const normalized = normalizeMyAppsParams(params)
    return queryOptions({
      queryKey: appHostingKeys.myAppsList(params),
      queryFn: () =>
        appHostingApi.listMyApps({
          query: normalized.query || undefined,
          status: normalized.status || undefined,
          limit: normalized.limit,
          offset: normalized.offset || undefined,
        }),
      enabled,
    })
  },

  appDetail: (appId: string, enabled = true) => {
    const normalizedId = appId.trim()
    return queryOptions({
      queryKey: appHostingKeys.appDetail(normalizedId),
      queryFn: () => appHostingApi.getApp(normalizedId),
      enabled: enabled && Boolean(normalizedId),
    })
  },

  revisions: (appId: string, params?: HostedAppRevisionsQueryParams, enabled = true) => {
    const normalizedId = appId.trim()
    const normalized = normalizeRevisionsParams(params)
    return queryOptions({
      queryKey: appHostingKeys.revisionsList(normalizedId, params),
      queryFn: () => appHostingApi.listRevisions(normalizedId, normalized),
      enabled: enabled && Boolean(normalizedId),
    })
  },

  myApprovals: (params?: MyHostedAppApprovalsQueryParams, enabled = true) => {
    const normalized = normalizeMyApprovalsParams(params)
    return queryOptions({
      queryKey: appHostingKeys.myApprovals(params),
      queryFn: () =>
        appHostingApi.listMyApprovals({
          appId: normalized.appId || undefined,
          status: normalized.status || undefined,
          limit: normalized.limit,
          offset: normalized.offset,
        }),
      enabled,
    })
  },
}
