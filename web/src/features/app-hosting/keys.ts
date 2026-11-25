import type {
  AudienceCandidatesQueryParams,
  HostedAppRevisionsQueryParams,
  MyHostedAppApprovalsQueryParams,
  MyHostedAppsQueryParams,
  PortalHostedAppsQueryParams,
} from './types'

function normalizeText(value?: string) {
  return value?.trim() || ''
}

function normalizeLimit(value?: number, fallback = 50) {
  return typeof value === 'number' && Number.isFinite(value) && value > 0 ? Math.floor(value) : fallback
}

function normalizeOffset(value?: number) {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? Math.floor(value) : 0
}

export function normalizePortalAppsParams(params?: PortalHostedAppsQueryParams) {
  return {
    query: normalizeText(params?.query),
    kind: params?.kind ?? '',
    authorUserId: normalizeText(params?.authorUserId),
    limit: normalizeLimit(params?.limit, 60),
    offset: normalizeOffset(params?.offset),
  } as const
}

export function normalizeAudienceCandidatesParams(params?: AudienceCandidatesQueryParams) {
  return {
    query: normalizeText(params?.query),
    limit: normalizeLimit(params?.limit, 20),
  } as const
}

export function normalizeMyAppsParams(params?: MyHostedAppsQueryParams) {
  return {
    query: normalizeText(params?.query),
    status: params?.status ?? '',
    limit: normalizeLimit(params?.limit, 100),
    offset: normalizeOffset(params?.offset),
  } as const
}

export function normalizeRevisionsParams(params?: HostedAppRevisionsQueryParams) {
  return {
    limit: normalizeLimit(params?.limit, 30),
    offset: normalizeOffset(params?.offset),
  } as const
}

export function normalizeMyApprovalsParams(params?: MyHostedAppApprovalsQueryParams) {
  return {
    appId: normalizeText(params?.appId),
    status: params?.status ?? '',
    limit: normalizeLimit(params?.limit, 10),
    offset: normalizeOffset(params?.offset),
  } as const
}

export const appHostingKeys = {
  all: ['app-hosting'] as const,
  portalAll: () => ['app-hosting', 'portal'] as const,
  portalList: (params?: PortalHostedAppsQueryParams) =>
    ['app-hosting', 'portal', 'list', normalizePortalAppsParams(params)] as const,
  audienceCandidatesAll: () => ['app-hosting', 'audience-candidates'] as const,
  audienceCandidates: (params?: AudienceCandidatesQueryParams) =>
    ['app-hosting', 'audience-candidates', normalizeAudienceCandidatesParams(params)] as const,
  appsAll: () => ['app-hosting', 'apps'] as const,
  myAppsLists: () => ['app-hosting', 'apps', 'mine'] as const,
  myAppsList: (params?: MyHostedAppsQueryParams) =>
    ['app-hosting', 'apps', 'mine', normalizeMyAppsParams(params)] as const,
  appDetails: () => ['app-hosting', 'apps', 'detail'] as const,
  appDetail: (appId: string) => ['app-hosting', 'apps', 'detail', normalizeText(appId)] as const,
  revisionsAll: (appId: string) =>
    ['app-hosting', 'apps', 'revisions', normalizeText(appId)] as const,
  revisionsList: (appId: string, params?: HostedAppRevisionsQueryParams) =>
    [
      'app-hosting',
      'apps',
      'revisions',
      normalizeText(appId),
      normalizeRevisionsParams(params),
    ] as const,
  approvalsAll: () => ['app-hosting', 'approvals'] as const,
  myApprovals: (params?: MyHostedAppApprovalsQueryParams) =>
    ['app-hosting', 'approvals', 'mine', normalizeMyApprovalsParams(params)] as const,
}
