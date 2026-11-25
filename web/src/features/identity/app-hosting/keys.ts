import type {
  AdminHostedAppListParams,
  AdminHostedAppRevisionListParams,
  AdminVisibilityApprovalListParams,
} from './types'

function normalizeText(value?: string) {
  const trimmed = value?.trim()
  return trimmed ? trimmed : undefined
}

function normalizePagination(limit?: number, offset?: number) {
  return {
    limit: typeof limit === 'number' && Number.isFinite(limit) ? limit : 20,
    offset: typeof offset === 'number' && Number.isFinite(offset) ? Math.max(0, offset) : 0,
  }
}

export function normalizeAdminAppListParams(params: AdminHostedAppListParams = {}) {
  return {
    query: normalizeText(params.query),
    status: params.status || undefined,
    ownerUserId: normalizeText(params.ownerUserId),
    teamId: normalizeText(params.teamId),
    ...normalizePagination(params.limit, params.offset),
  }
}

export function normalizeAdminApprovalListParams(params: AdminVisibilityApprovalListParams = {}) {
  return {
    status: params.status || undefined,
    appId: normalizeText(params.appId),
    ...normalizePagination(params.limit, params.offset),
  }
}

export function normalizeRevisionListParams(params: AdminHostedAppRevisionListParams = {}) {
  return normalizePagination(params.limit, params.offset)
}

export const appHostingAdminKeys = {
  all: ['identity-app-hosting'] as const,
  settings: ['identity-app-hosting', 'settings'] as const,
  apps: ['identity-app-hosting', 'apps'] as const,
  appLists: ['identity-app-hosting', 'apps', 'list'] as const,
  appList: (params: AdminHostedAppListParams = {}) =>
    ['identity-app-hosting', 'apps', 'list', normalizeAdminAppListParams(params)] as const,
  appDetails: ['identity-app-hosting', 'apps', 'detail'] as const,
  appDetail: (appId: string) => ['identity-app-hosting', 'apps', 'detail', appId.trim()] as const,
  appRevisions: (appId: string, params: AdminHostedAppRevisionListParams = {}) =>
    [
      'identity-app-hosting',
      'apps',
      'revisions',
      appId.trim(),
      normalizeRevisionListParams(params),
    ] as const,
  approvals: ['identity-app-hosting', 'approvals'] as const,
  approvalLists: ['identity-app-hosting', 'approvals', 'list'] as const,
  approvalList: (params: AdminVisibilityApprovalListParams = {}) =>
    [
      'identity-app-hosting',
      'approvals',
      'list',
      normalizeAdminApprovalListParams(params),
    ] as const,
  audienceCandidates: ['identity-app-hosting', 'audience-candidates'] as const,
  audienceCandidateSearch: (query = '', limit = 20) =>
    ['identity-app-hosting', 'audience-candidates', query.trim(), limit] as const,
}

export const appHostingAdminMutationKeys = {
  updateSettings: ['identity-app-hosting', 'settings', 'update'] as const,
  rotateGatewayToken: ['identity-app-hosting', 'settings', 'rotate-gateway-token'] as const,
  suspendApp: ['identity-app-hosting', 'apps', 'suspend'] as const,
  unsuspendApp: ['identity-app-hosting', 'apps', 'unsuspend'] as const,
  transferApp: ['identity-app-hosting', 'apps', 'transfer'] as const,
  deleteApp: ['identity-app-hosting', 'apps', 'delete'] as const,
  approveVisibility: ['identity-app-hosting', 'approvals', 'approve'] as const,
  rejectVisibility: ['identity-app-hosting', 'approvals', 'reject'] as const,
}
